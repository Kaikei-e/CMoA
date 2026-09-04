// Package verify runs the task's verifier against one candidate worktree in
// a container: `docker compose run`. CMoA does not build a sandbox of its
// own; the compose file the task ships is the sandbox, and the candidate
// directory reaches it through the CMOA_CANDIDATE_DIR variable.
package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Spec is one verification.
type Spec struct {
	ComposeFile  string        // absolute path
	Service      string        // service to run
	ProjectName  string        // unique per candidate; see ProjectName
	CandidateDir string        // absolute worktree path, exported as CMOA_CANDIDATE_DIR
	Timeout      time.Duration // 0: no timeout
}

// Result is what the container did.
type Result struct {
	ExitCode int
	Duration time.Duration
	Stdout   []byte
	Stderr   []byte
	TimedOut bool
	Command  []string // the run command, for the trace
}

// Runner verifies a candidate. Selection depends on this interface so tests
// can substitute a fake.
type Runner interface {
	Run(ctx context.Context, s Spec) (*Result, error)
}

// RunnerError means the verifier could not be run at all (docker missing,
// compose file unreadable). It is distinct from a candidate failing.
type RunnerError struct {
	Stage  string
	Stderr string
	Err    error
}

func (e *RunnerError) Error() string {
	return fmt.Sprintf("verify: %s: %v: %s", e.Stage, e.Err, strings.TrimSpace(e.Stderr))
}

func (e *RunnerError) Unwrap() error { return e.Err }

// ComposeRunner runs `docker compose`. Docker is the binary name or path;
// empty means "docker" on PATH.
type ComposeRunner struct {
	Docker string
	// KillAfter bounds how long docker gets to stop cleanly after a timeout
	// before it is killed. Zero means 15 seconds.
	KillAfter time.Duration
}

// EnvCandidateDir is the variable compose files read the candidate from.
const EnvCandidateDir = "CMOA_CANDIDATE_DIR"

var projectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func removeByLabel(ctx context.Context, bin, project string) {
	ps := exec.CommandContext(ctx, bin, "ps", "-aq", "--filter", "label=com.docker.compose.project="+project)
	out, err := ps.Output()
	if err != nil {
		return
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return
	}
	_ = exec.CommandContext(ctx, bin, append([]string{"rm", "-f"}, ids...)...).Run()
}

// ProjectName derives a compose project name that is unique per candidate,
// so concurrent verifications of one task do not share containers or
// volumes. Compose requires lowercase alphanumerics, hyphen, underscore.
func ProjectName(taskID, runID, candidateID string) string {
	s := strings.ToLower("cmoa-" + taskID + "-" + runID + "-" + candidateID)
	s = regexp.MustCompile(`[^a-z0-9_-]`).ReplaceAllString(s, "-")
	return s
}

// Run executes the service once and always tears the project down.
func (r *ComposeRunner) Run(ctx context.Context, s Spec) (*Result, error) {
	if !projectPattern.MatchString(s.ProjectName) {
		return nil, &RunnerError{Stage: "project name", Err: fmt.Errorf("%q is not a compose project name", s.ProjectName)}
	}
	if _, err := os.Stat(s.ComposeFile); err != nil {
		return nil, &RunnerError{Stage: "compose file", Err: err}
	}
	docker := r.Docker
	if docker == "" {
		docker = "docker"
	}
	bin, err := exec.LookPath(docker)
	if err != nil {
		return nil, &RunnerError{Stage: "docker binary", Err: err}
	}
	env := append(os.Environ(), EnvCandidateDir+"="+s.CandidateDir)
	base := []string{"compose", "-f", s.ComposeFile, "-p", s.ProjectName}

	runCtx := ctx
	cancel := context.CancelFunc(func() {})
	if s.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, s.Timeout)
	}
	defer cancel()

	args := append(append([]string{}, base...), "run", "--rm", "--no-deps", "-T", "--quiet-pull", s.Service)
	cmd := exec.CommandContext(runCtx, bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// On timeout, ask docker to stop cleanly before the process is killed.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = r.KillAfter
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = 15 * time.Second
	}

	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start)

	// Teardown uses the parent ctx so it still runs after a timeout; give it
	// its own bound so a hung daemon cannot wedge select.
	downCtx, downCancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer downCancel()
	down := exec.CommandContext(downCtx, bin, append(append([]string{}, base...), "down", "-v", "--remove-orphans")...)
	down.Env = env
	_ = down.Run()
	// `down` has been observed (Compose v5.4, 2026-09) to leave a container
	// behind after the client was interrupted. Remove whatever still carries
	// the project label; the label, not the name, is stable.
	removeByLabel(downCtx, bin, s.ProjectName)

	res := &Result{Duration: dur, Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), Command: append([]string{docker}, args...)}
	if runCtx.Err() != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		res.TimedOut = true
		res.ExitCode = -1
		return res, nil
	}
	if runErr == nil {
		return res, nil
	}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		res.ExitCode = exit.ExitCode()
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		return res, nil
	}
	return nil, &RunnerError{Stage: "run", Stderr: stderr.String(), Err: runErr}
}
