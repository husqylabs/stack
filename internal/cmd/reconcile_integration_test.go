package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/husqylabs/stack/internal/stack"
)

// These tests exercise the crash/abort reconciliation against a REAL git repo by
// driving a real rebase to a conflict, then taking the two user paths
// (--continue and --abort) and asserting our metadata heals to match git.

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupConflictingStack builds: main, p1 (off main, edits file), feat (off main,
// edits the SAME line differently). A stack.json tracks feat+p1 on main. The
// returned dir is the repo; main's tip is recorded as both branches' base.
func setupConflictingStack(t *testing.T) (dir, mainTip, p1Tip string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir = t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	gitCmd(t, dir, "config", "user.email", "t@t.test")
	gitCmd(t, dir, "config", "user.name", "Tester")

	write(t, dir, "f.txt", "base\n")
	gitCmd(t, dir, "add", "f.txt")
	gitCmd(t, dir, "commit", "-qm", "base")
	mainTip = gitCmd(t, dir, "rev-parse", "main")

	// p1 off main, edits line 1.
	gitCmd(t, dir, "checkout", "-q", "-b", "p1", "main")
	write(t, dir, "f.txt", "p1 change\n")
	gitCmd(t, dir, "commit", "-qam", "p1 edit")
	p1Tip = gitCmd(t, dir, "rev-parse", "p1")

	// feat off main, edits the SAME line -> guarantees a conflict on rebase onto p1.
	gitCmd(t, dir, "checkout", "-q", "-b", "feat", "main")
	write(t, dir, "f.txt", "feat change\n")
	gitCmd(t, dir, "commit", "-qam", "feat edit")

	// Track the stack locally.
	s := stack.New("main")
	s.Add("p1", "main", mainTip)
	s.Add("feat", "main", mainTip)
	store := stack.NewStore(filepath.Join(dir, ".git"))
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	return dir, mainTip, p1Tip
}

// reparentFeatOntoP1 runs the cobra command and returns its (expected) error.
func reparentFeatOntoP1(t *testing.T, dir string) error {
	t.Helper()
	t.Chdir(dir)
	root := NewRoot()
	root.SetArgs([]string{"reparent", "feat", "--onto", "p1"})
	return root.Execute()
}

func runSync(t *testing.T, dir string) error {
	t.Helper()
	t.Chdir(dir)
	root := NewRoot()
	root.SetArgs([]string{"sync"})
	return root.Execute()
}

func loadStack(t *testing.T, dir string) *stack.Stack {
	t.Helper()
	s, err := stack.NewStore(filepath.Join(dir, ".git")).Load()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func pendingExists(t *testing.T, dir string) bool {
	t.Helper()
	_, err := stack.NewStore(filepath.Join(dir, ".git")).LoadPending()
	return err == nil
}

// Path 1: conflict, then the user ABORTS -> we must roll back to main.
func TestReparentConflict_AbortRollsBack(t *testing.T) {
	dir, mainTip, _ := setupConflictingStack(t)

	err := reparentFeatOntoP1(t, dir)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected a conflict error, got %v", err)
	}
	if !pendingExists(t, dir) {
		t.Fatal("journal should persist while the rebase is unresolved")
	}

	// User backs out.
	gitCmd(t, dir, "rebase", "--abort")

	// Any subsequent command reconciles. Sync should succeed and roll back.
	if err := runSync(t, dir); err != nil {
		t.Fatalf("sync after abort should heal cleanly, got %v", err)
	}
	if pendingExists(t, dir) {
		t.Fatal("journal must be cleared after reconciliation")
	}
	s := loadStack(t, dir)
	if got := s.Branches["feat"]; got.Parent != "main" || got.ParentCommit != mainTip {
		t.Fatalf("rollback failed: feat.Parent=%q ParentCommit=%q (want main/%s)",
			got.Parent, got.ParentCommit, mainTip)
	}
}

// Path 2: conflict, then the user RESOLVES + --continue -> we must move feat onto p1.
func TestReparentConflict_ContinueCommitsForward(t *testing.T) {
	dir, _, p1Tip := setupConflictingStack(t)
	// `git rebase --continue` may open an editor for the commit message.
	t.Setenv("GIT_EDITOR", "true")

	err := reparentFeatOntoP1(t, dir)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected a conflict error, got %v", err)
	}

	// Resolve the conflict and finish the rebase.
	write(t, dir, "f.txt", "resolved\n")
	gitCmd(t, dir, "add", "f.txt")
	gitCmd(t, dir, "rebase", "--continue")

	// Reconciliation on the next command commits the move forward.
	if err := runSync(t, dir); err != nil {
		t.Fatalf("sync after continue should heal cleanly, got %v", err)
	}
	if pendingExists(t, dir) {
		t.Fatal("journal must be cleared after reconciliation")
	}
	s := loadStack(t, dir)
	if got := s.Branches["feat"]; got.Parent != "p1" || got.ParentCommit != p1Tip {
		t.Fatalf("forward commit failed: feat.Parent=%q ParentCommit=%q (want p1/%s)",
			got.Parent, got.ParentCommit, p1Tip)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
