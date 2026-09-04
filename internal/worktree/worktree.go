// Package worktree gives each candidate its own checkout of the task
// repository at the task revision, via git worktree. A worktree is cheap,
// shares the object store, and is removed after verification whatever the
// outcome.
package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Add checks out rev from repo into dir as a detached worktree. dir must not
// exist yet.
func Add(ctx context.Context, repo, rev, dir string) error {
	return git(ctx, repo, "worktree", "add", "--detach", "--quiet", dir, rev)
}

// Remove deletes the worktree at dir and prunes git's record of it. It is
// safe to call on a worktree whose files were modified or deleted.
func Remove(ctx context.Context, repo, dir string) error {
	if err := git(ctx, repo, "worktree", "remove", "--force", dir); err != nil {
		// A partially removed worktree still needs pruning.
		_ = git(ctx, repo, "worktree", "prune")
		return err
	}
	return git(ctx, repo, "worktree", "prune")
}

// Error carries git's stderr.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.Args, " "), e.Err, strings.TrimSpace(e.Stderr))
}

func (e *Error) Unwrap() error { return e.Err }

func git(ctx context.Context, repo string, args ...string) error {
	full := append([]string{"-C", repo}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &Error{Args: args, Stderr: stderr.String(), Err: err}
	}
	return nil
}
