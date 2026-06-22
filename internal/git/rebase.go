package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/husqylabs/stack/internal/stack"
)

// gitBin is the git executable. Configurable so a future build can pin a path or
// a vendored binary; nothing else hard-codes "git".
var gitBin = "git"

// RebaseResult reports the outcome of cascading one branch.
type RebaseResult struct {
	Branch     string
	OldBase    string // the recorded parent commit we rebased away from
	NewBase    string // the parent's current tip we rebased onto
	NewTip     string // branch tip after a successful rebase
	Skipped    bool   // base unchanged -> nothing to do
	Conflicted bool   // git stopped on conflicts; human intervention required
	Detail     string // git's stderr/stdout on failure, for surfacing to the user
}

// Cascade rebases an entire stack so every child sits on its parent's *current*
// tip. This is the whole point of the tool.
//
// THE GIT MATH
// ------------
// When a parent branch P is amended or grows new commits, every child C that was
// forked from P's old tip is now based on a stale commit. The fix per child is:
//
//	git rebase --onto <P's new tip> <C's recorded old base> <C>
//
// The three arguments mean, precisely:
//
//	--onto <newbase>  : graft replayed commits here (P's current tip)
//	<oldbase>         : replay the commits in (oldbase, C], i.e. EXCLUDING oldbase.
//	                    This is C.ParentCommit — where C was last synced onto P.
//	<C>               : the branch whose commits we replay; git checks it out.
//
// Using the *recorded* old base (not a live merge-base) is what makes this exact:
// it names precisely the commits that are uniquely C's, so we never duplicate or
// drop the parent's commits. After P itself is rebased, P's tip changes — which
// is why we must process strictly parent-before-child (topological order) and
// feed each child its parent's freshly-updated tip as <newbase>.
//
// Example, stack: main <- A <- B <- C, and A gets new commits:
//
//	rebase A onto main (A's recorded base) -> A'         (A.ParentCommit was main's old tip)
//	rebase --onto A' (B's recorded old base = A's old tip) B  -> B'
//	rebase --onto B' (C's recorded old base = B's old tip) C  -> C'
//
// Each step both moves the branch AND tells us the new tip to feed the next child.
//
// On conflict, git leaves the worktree mid-rebase; we stop the cascade and return
// a Conflicted result so the caller can tell the user to resolve and re-run.
//
// Crash-safety is delegated to the caller via CascadeHooks: BeforeRebase records
// a write-ahead journal of (branch, oldBase, newBase) *before* git is touched, so
// any interruption is recoverable; AfterRebase persists the advanced state and
// clears the journal once a branch lands cleanly. On conflict the cascade stops
// with the conflicted branch's journal intact, ready for reconciliation.
func (r *Repo) Cascade(ctx context.Context, s *stack.Stack, h CascadeHooks) ([]RebaseResult, error) {
	order, err := s.TopoOrder()
	if err != nil {
		return nil, err
	}

	var results []RebaseResult
	for _, b := range order {
		if b.Parent == "" {
			continue // trunk-rooted base branch: nothing above it to track
		}

		// Resolve the parent's CURRENT tip. For a tracked parent this reflects any
		// rebase we just performed earlier in this same cascade.
		newBaseHash, err := r.Tip(b.Parent)
		if err != nil {
			return results, fmt.Errorf("branch %q: %w", b.Name, err)
		}
		newBase := newBaseHash.String()
		oldBase := b.ParentCommit

		// Fast path: the parent hasn't moved since we last synced -> skip.
		if oldBase == newBase {
			res := RebaseResult{Branch: b.Name, OldBase: oldBase, NewBase: newBase, Skipped: true}
			results = append(results, res)
			continue
		}

		// Journal BEFORE mutating git, so an interruption here is recoverable.
		if h.BeforeRebase != nil {
			if err := h.BeforeRebase(b.Name, oldBase, newBase); err != nil {
				return results, fmt.Errorf("journal rebase of %q: %w", b.Name, err)
			}
		}

		res := r.rebaseOnto(ctx, newBase, oldBase, b.Name)
		results = append(results, res)
		if res.Conflicted {
			// Stop the cascade with the journal intact; reconciliation on the next
			// command heals this branch once the user resolves or aborts.
			return results, fmt.Errorf("rebase of %q stopped on conflicts; resolve, run `git rebase --continue`, then re-run sync", b.Name)
		}
		if res.NewTip == "" {
			// Hard failure (bad ref/args), not a conflict: git started nothing.
			return results, fmt.Errorf("rebase of %q failed: %s", b.Name, res.Detail)
		}

		// Success: advance recorded base + tip; AfterRebase persists and clears.
		b.ParentCommit = res.NewBase
		if h.AfterRebase != nil {
			if err := h.AfterRebase(b, res); err != nil {
				return results, err
			}
		}
	}
	return results, nil
}

// CascadeHooks lets the caller (which owns persistence) journal and commit each
// rebase step without the git layer depending on the store.
type CascadeHooks struct {
	// BeforeRebase is called right before a branch is rebased. Use it to write the
	// write-ahead journal. Returning an error aborts the cascade before any git
	// mutation for that branch.
	BeforeRebase func(branch, oldBase, newBase string) error
	// AfterRebase is called after a branch rebases cleanly (b.ParentCommit already
	// advanced). Use it to persist state and clear the journal.
	AfterRebase func(b *stack.Branch, res RebaseResult) error
}

// RebaseOnto exposes a single `git rebase --onto` for callers outside the
// cascade (e.g. reparenting a branch onto a different parent). Same semantics as
// the per-branch step inside Cascade.
func (r *Repo) RebaseOnto(ctx context.Context, newbase, oldbase, branch string) RebaseResult {
	return r.rebaseOnto(ctx, newbase, oldbase, branch)
}

// rebaseOnto runs a single `git rebase --onto newbase oldbase branch` and reports
// the result without throwing on the expected "conflict" exit.
func (r *Repo) rebaseOnto(ctx context.Context, newbase, oldbase, branch string) RebaseResult {
	res := RebaseResult{Branch: branch, OldBase: oldbase, NewBase: newbase}

	// --onto <newbase> <oldbase> <branch>
	// We pass the branch explicitly so git checks it out for us; no manual checkout.
	out, err := r.run(ctx, "rebase", "--onto", newbase, oldbase, branch)
	if err != nil {
		// Distinguish a merge conflict (recoverable, user resolves) from a hard
		// failure (bad args, missing ref). git writes "CONFLICT" / "could not apply".
		lc := strings.ToLower(out)
		if strings.Contains(lc, "conflict") || strings.Contains(lc, "could not apply") {
			res.Conflicted = true
			res.Detail = out
			return res
		}
		res.Detail = fmt.Sprintf("%v: %s", err, out)
		return res
	}

	tip, terr := r.Tip(branch)
	if terr != nil {
		res.Detail = terr.Error()
		return res
	}
	res.NewTip = tip.String()
	return res
}

// run executes a git subcommand in the worktree and returns combined output.
func (r *Repo) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Dir = r.workIn
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// CreateBranch creates `name` at the tip of `parent` and checks it out. Uses the
// git binary (mutation) for parity with the rest of the worktree-changing ops.
func (r *Repo) CreateBranch(ctx context.Context, name, parent string) error {
	out, err := r.run(ctx, "checkout", "-b", name, parent)
	if err != nil {
		return fmt.Errorf("create branch %q on %q: %s", name, parent, out)
	}
	return nil
}

// Fetch updates remote-tracking refs from `remote`.
func (r *Repo) Fetch(ctx context.Context, remote string) (string, error) {
	return r.run(ctx, "fetch", remote)
}

// Checkout switches to an existing branch.
func (r *Repo) Checkout(ctx context.Context, name string) (string, error) {
	return r.run(ctx, "checkout", name)
}

// DeleteLocalBranch force-deletes a local branch.
func (r *Repo) DeleteLocalBranch(ctx context.Context, name string) (string, error) {
	return r.run(ctx, "branch", "-D", name)
}

// DeleteRemoteBranch deletes a branch on the remote.
func (r *Repo) DeleteRemoteBranch(ctx context.Context, remote, name string) (string, error) {
	return r.run(ctx, "push", remote, "--delete", name)
}

// TrackRemoteBranch creates a local branch `name` tracking `remote/name`, without
// checking it out. Used by adopt to materialize a teammate's stack locally.
func (r *Repo) TrackRemoteBranch(ctx context.Context, remote, name string) error {
	out, err := r.run(ctx, "branch", "--track", name, remote+"/"+name)
	if err != nil {
		return fmt.Errorf("track %s/%s: %s", remote, name, out)
	}
	return nil
}

// Push pushes a branch with --force-with-lease, the safe force needed after a
// rebase rewrites history. force-with-lease refuses to clobber commits we haven't
// seen, protecting against trampling a teammate's push.
func (r *Repo) Push(ctx context.Context, remote, branch string) (string, error) {
	return r.run(ctx, "push", "--force-with-lease", remote, branch)
}
