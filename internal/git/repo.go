// Package git wraps the two ways we talk to git:
//
//   - repo.go : READ-ONLY inspection via go-git/v6. Resolving refs, reading the
//     commit DAG, and computing merge-bases. go-git is excellent at this.
//
//   - rebase.go : MUTATING operations via os/exec of the real `git` binary.
//     We deliberately do NOT use go-git for rebase/cherry-pick: go-git/v6 has no
//     rebase command and its line-level three-way merge is incomplete, so a
//     conflicting cascade could silently corrupt trees. The system git binary
//     does this correctly, including honoring the user's mergetool config.
//
// Keeping the split explicit means the dangerous half (rebase) is small,
// auditable, and isolated.
package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Repo is a read-only handle over a local repository.
type Repo struct {
	repo   *gogit.Repository
	workIn string // worktree root, where os/exec git commands run
}

// Open discovers the repository containing path.
func Open(path string) (*Repo, error) {
	r, err := gogit.PlainOpenWithOptions(path, &gogit.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("open repo at %q: %w", path, err)
	}
	wt, err := r.Worktree()
	if err != nil {
		return nil, fmt.Errorf("resolve worktree: %w", err)
	}
	return &Repo{repo: r, workIn: wt.Filesystem().Root()}, nil
}

// GitDir returns the absolute path to the .git directory (for the state store).
func (r *Repo) GitDir() (string, error) {
	return filepath.Join(r.workIn, ".git"), nil
}

// Tip resolves a branch name to its current commit hash.
func (r *Repo) Tip(branch string) (plumbing.Hash, error) {
	ref, err := r.repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve branch %q: %w", branch, err)
	}
	return ref.Hash(), nil
}

// MergeBase computes the best common ancestor of two branches. This is the core
// "git math": it tells us where two lines of history diverged. We use it to
// detect drift (has the parent moved since the child was forked?) and as a
// sanity check on the rebase range.
func (r *Repo) MergeBase(a, b string) (plumbing.Hash, error) {
	ca, err := r.commit(a)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	cb, err := r.commit(b)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	bases, err := ca.MergeBase(cb)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("merge-base %s..%s: %w", a, b, err)
	}
	if len(bases) == 0 {
		return plumbing.ZeroHash, fmt.Errorf("no common ancestor between %q and %q", a, b)
	}
	// For our DAG (single trunk) the first base is the one we want.
	return bases[0].Hash, nil
}

// CommitsBetween returns the commits unique to `branch` relative to `base`
// (i.e. base..branch), oldest first. This is exactly the set of commits a
// `rebase --onto` will replay, so it's useful for previews and dry-runs.
func (r *Repo) CommitsBetween(base, branch string) ([]*object.Commit, error) {
	baseHash, err := r.Tip(base)
	if err != nil {
		// base may be a raw hash rather than a branch name (recorded ParentCommit)
		baseHash = plumbing.NewHash(base)
	}
	head, err := r.commit(branch)
	if err != nil {
		return nil, err
	}
	baseCommit, err := r.repo.CommitObject(baseHash)
	if err != nil {
		return nil, fmt.Errorf("resolve base commit %s: %w", baseHash, err)
	}

	iter, err := r.repo.Log(&gogit.LogOptions{From: head.Hash})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var out []*object.Commit
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Hash == baseCommit.Hash {
			return storerStop
		}
		isAncestorOfBase, _ := baseCommit.IsAncestor(c)
		if isAncestorOfBase {
			return nil // shared history, not unique to branch
		}
		out = append(out, c)
		return nil
	})
	if err != nil && err != storerStop {
		return nil, err
	}
	// History() yields newest-first; reverse to oldest-first (replay order).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// FirstCommitSubject returns the subject line of the oldest commit unique to
// `branch` relative to `base`, for use as a default PR title. Falls back to the
// branch name if the range is empty.
func (r *Repo) FirstCommitSubject(base, branch string) string {
	commits, err := r.CommitsBetween(base, branch)
	if err != nil || len(commits) == 0 {
		return branch
	}
	msg := commits[0].Message
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if msg = strings.TrimSpace(msg); msg == "" {
		return branch
	}
	return msg
}

// RebaseInProgress reports whether git has a rebase paused mid-flight (the
// standard markers git itself uses). If true, our metadata must not be touched
// until the user finishes or aborts the rebase.
func (r *Repo) RebaseInProgress() bool {
	gitDir := filepath.Join(r.workIn, ".git")
	for _, marker := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(gitDir, marker)); err == nil {
			return true
		}
	}
	return false
}

// IsAncestor reports whether commit `ancestor` (a hash string) is an ancestor of
// the tip of `branch`. This is how reconciliation reads git's ground truth: after
// an interrupted rebase, whether the branch landed on the new base or fell back
// to the old one.
func (r *Repo) IsAncestor(ancestor, branch string) (bool, error) {
	anc, err := r.repo.CommitObject(plumbing.NewHash(ancestor))
	if err != nil {
		return false, fmt.Errorf("resolve ancestor %s: %w", ancestor, err)
	}
	tip, err := r.commit(branch)
	if err != nil {
		return false, err
	}
	return anc.IsAncestor(tip)
}

// LocalBranchExists reports whether a local branch ref is present.
func (r *Repo) LocalBranchExists(name string) bool {
	_, err := r.repo.Reference(plumbing.NewBranchReferenceName(name), false)
	return err == nil
}

// CurrentBranch returns the short name of the currently checked-out branch.
func (r *Repo) CurrentBranch() (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	if !head.Name().IsBranch() {
		return "", errors.New("HEAD is detached; check out a branch first")
	}
	return head.Name().Short(), nil
}

func (r *Repo) commit(branch string) (*object.Commit, error) {
	h, err := r.Tip(branch)
	if err != nil {
		// allow raw hashes too
		if h2 := plumbing.NewHash(branch); !h2.IsZero() {
			return r.repo.CommitObject(h2)
		}
		return nil, err
	}
	return r.repo.CommitObject(h)
}

var ErrNotFound = errors.New("not found")

// storerStop is a sentinel to break out of a Log ForEach early.
var storerStop = errors.New("stop")
