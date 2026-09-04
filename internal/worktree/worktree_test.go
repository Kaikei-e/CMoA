package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return repo
}

func TestAddAndRemove(t *testing.T) {
	repo := initRepo(t)
	dir := filepath.Join(t.TempDir(), "wt")
	ctx := context.Background()
	if err := Add(ctx, repo, "HEAD", dir); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "a.txt")); err != nil || string(b) != "one\n" {
		t.Fatalf("worktree content: %q, %v", b, err)
	}
	// Dirty the worktree; Remove must still succeed.
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	if err := Remove(ctx, repo, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worktree dir should be gone")
	}
	// A second Add at the same path works after removal (prune happened).
	if err := Add(ctx, repo, "HEAD", dir); err != nil {
		t.Fatal(err)
	}
	_ = Remove(ctx, repo, dir)
}

func TestAddBadRev(t *testing.T) {
	repo := initRepo(t)
	err := Add(context.Background(), repo, "no-such-rev", filepath.Join(t.TempDir(), "wt"))
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Stderr == "" {
		t.Fatalf("want *Error with stderr, got %v", err)
	}
}
