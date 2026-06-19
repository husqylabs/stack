package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Regression: CommitsBetween must return the commits UNIQUE to the branch
// (base..branch), not skip them. A prior inverted ancestor check dropped them
// all, which silently defaulted PR titles to the branch name.
func TestCommitsBetweenAndFirstSubject(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "user.email", "t@t.test")
	run(t, dir, "config", "user.name", "Tester")

	os.WriteFile(filepath.Join(dir, "a"), []byte("1"), 0o644)
	run(t, dir, "add", "a")
	run(t, dir, "commit", "-qm", "base commit")
	baseTip := run(t, dir, "rev-parse", "main")

	run(t, dir, "checkout", "-q", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "b"), []byte("2"), 0o644)
	run(t, dir, "add", "b")
	run(t, dir, "commit", "-qm", "first feat commit")
	os.WriteFile(filepath.Join(dir, "c"), []byte("3"), 0o644)
	run(t, dir, "add", "c")
	run(t, dir, "commit", "-qm", "second feat commit")

	repo, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	commits, err := repo.CommitsBetween(baseTip, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("want 2 unique commits, got %d", len(commits))
	}
	// Oldest-first ordering (replay order).
	if !strings.HasPrefix(commits[0].Message, "first feat commit") {
		t.Fatalf("expected oldest-first; got %q first", commits[0].Message)
	}

	if got, ok := repo.FirstCommitSubject(baseTip, "feat"); !ok || got != "first feat commit" {
		t.Fatalf("FirstCommitSubject = %q,%v want %q,true", got, ok, "first feat commit")
	}
	// No unique commits -> not derivable, must report false so --update-titles
	// never clobbers a good title with the branch name.
	if got, ok := repo.FirstCommitSubject(baseTip, "main"); ok {
		t.Fatalf("expected ok=false for empty range, got %q,true", got)
	}
}
