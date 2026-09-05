package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Kaikei-e/CMoA/internal/band"
	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/patch"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
	"github.com/Kaikei-e/CMoA/internal/verify"
	"github.com/Kaikei-e/CMoA/internal/worktree"
)

// verify's exit codes. They reuse the numbers main.go declares; the names
// say what the number means for one verification.
const (
	exitVerifyNo     = exitRuntime // the verifier answered no: fail, apply_failed, timeout
	exitVerifyRunner = exitInvalid // the verifier could not be run at all
)

// runnerErrorTail bounds how much of the container's stderr is folded into
// the JSON when there is no --out directory to write it to.
const runnerErrorTail = 4096

// labelPattern is the alphabet a --label may use. The label becomes part of
// the compose project name, which admits only these characters; rejecting a
// label rather than rewriting it keeps two different labels two different
// projects, so concurrent verifications cannot tear down each other's
// containers.
var labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// cmdVerify verifies one diff against the task's revision and prints a
// single trace.Verification on stdout. It is select's inner loop without a
// run: nothing is written under <task>/runs, no proposer is asked, and no
// cmoa.json is read unless --config names one. Both verify kinds are run
// here; only verify reads a band, because only verify is asked about a diff
// somebody else chose.
func cmdVerify(ctx context.Context, args []string, stdout, stderr io.Writer, logf func(string, ...any)) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	taskDir := fs.String("task", "", "task directory")
	diffPath := fs.String("diff", "", "unified diff to verify, - for stdin (an empty diff verifies the revision unchanged)")
	outDir := fs.String("out", "", "directory to write result.json, stdout.txt and stderr.txt into")
	timeout := fs.Duration("timeout", 0, "verifier timeout (default: the task's, then the config's, then none)")
	label := fs.String("label", "", "name for this verification, "+labelPattern.String()+" (default: 8 hex digits)")
	cfgPath := fs.String("config", "", "cmoa.json to take verify.timeout_seconds from; the whole file must be valid")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *taskDir == "" || *diffPath == "" {
		fmt.Fprintln(stderr, "cmoa: verify needs --task and --diff")
		return exitUsage
	}
	if *timeout < 0 {
		fmt.Fprintln(stderr, "cmoa: --timeout must not be negative")
		return exitUsage
	}
	if *label != "" && !labelPattern.MatchString(*label) {
		fmt.Fprintf(stderr, "cmoa: --label %q must match %s\n", *label, labelPattern)
		return exitUsage
	}
	t, err := task.Load(*taskDir)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitUsage
	}
	to, code := verifyTimeout(*timeout, t, *cfgPath, stderr)
	if code != exitOK {
		return code
	}
	// Prove the output directory before spending a container on a result
	// that could not be written.
	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			fmt.Fprintln(stderr, "cmoa:", err)
			return exitUsage
		}
		if _, err := os.Stat(trace.VerificationFile(*outDir)); err == nil {
			fmt.Fprintf(stderr, "cmoa: %s already exists; a verification directory records one verification\n", trace.VerificationFile(*outDir))
			return exitUsage
		}
	}
	diff, err := readDiff(*diffPath)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitUsage
	}
	rev, err := t.ResolveRev(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitUsage
	}
	lbl := *label
	if lbl == "" {
		lbl = label8()
	}

	sum := sha256.Sum256(diff)
	v := &trace.Verification{
		SchemaVersion: trace.SchemaVersion,
		Task:          string(t.ID),
		Rev:           rev,
		DiffSHA256:    hex.EncodeToString(sum[:]),
		Label:         lbl,
		CMoAVersion:   version(),
	}
	res, outBytes, errBytes := verifyDiff(ctx, t, rev, string(diff), lbl, to, logf)
	v.VerifyResult = *res

	// A result that cannot be written is a runner error like any other: the
	// caller still gets the one JSON object it was promised.
	if *outDir != "" {
		if err := trace.WriteVerification(*outDir, v, outBytes, errBytes); err != nil {
			fmt.Fprintln(stderr, "cmoa:", err)
			v.Status = trace.VerifyRunnerError
			v.Error = strings.TrimSpace(v.Error + "\n" + err.Error())
		}
	} else if v.Status == trace.VerifyRunnerError && len(errBytes) > 0 {
		v.Error = strings.TrimSpace(v.Error + "\n" + tail(errBytes, runnerErrorTail))
	}
	enc := json.NewEncoder(stdout)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitVerifyRunner
	}
	return verifyExit(v.Status)
}

// verifyDiff applies diff to a fresh worktree at rev and runs the task's
// verifier over it, exactly as select does for a candidate. The worktree is
// removed whatever happens.
func verifyDiff(ctx context.Context, t *task.Task, rev, diff, label string, to time.Duration, logf func(string, ...any)) (res *trace.VerifyResult, stdout, stderr []byte) {
	res = &trace.VerifyResult{StartedAt: time.Now().UTC()}
	defer func() {
		res.FinishedAt = time.Now().UTC()
		res.DurationMS = res.FinishedAt.Sub(res.StartedAt).Milliseconds()
	}()
	root, err := os.MkdirTemp("", "cmoa-wt-")
	if err != nil {
		res.Status = trace.VerifyRunnerError
		res.Error = err.Error()
		return res, nil, nil
	}
	defer func() { _ = os.RemoveAll(root) }()
	wt := filepath.Join(root, "candidate")
	if err := worktree.Add(ctx, t.Repo, rev, wt); err != nil {
		res.Status = trace.VerifyRunnerError
		res.Error = err.Error()
		return res, nil, nil
	}
	defer func() {
		if err := worktree.Remove(context.WithoutCancel(ctx), t.Repo, wt); err != nil {
			logf("warning: remove worktree %s: %v", wt, err)
		}
	}()
	// An empty diff is not an error: it verifies the revision unchanged,
	// which is how a task's seed state is shown to fail.
	if strings.TrimSpace(diff) != "" {
		if err := patch.Apply(ctx, wt, diff); err != nil {
			res.Status = trace.VerifyApplyFailed
			res.ApplyError = err.Error()
			return res, nil, nil
		}
	}
	spec := verify.Spec{
		ComposeFile:  t.Verify.ComposeFile,
		Service:      t.Verify.Service,
		ProjectName:  verify.ProjectName(string(t.ID), "verify", label),
		CandidateDir: wt,
		Timeout:      to,
	}
	res.ProjectName = spec.ProjectName
	logf("%s: verifying in %s", label, spec.ProjectName)
	out, err := (&verify.ComposeRunner{}).Run(ctx, spec)
	if err != nil {
		res.Status = trace.VerifyRunnerError
		res.Error = err.Error()
		if out != nil {
			res.Command = out.Command
			return res, out.Stdout, out.Stderr
		}
		return res, nil, nil
	}
	res.Command = out.Command
	res.ExitCode = out.ExitCode
	if out.TimedOut {
		res.Status = trace.VerifyTimeout
	} else {
		judge(t.Verify.Kind, res, out.Stdout)
	}
	logf("%s: %s (exit %d, %s)", label, res.Status, out.ExitCode, out.Duration.Round(time.Millisecond))
	if res.Band != nil {
		logf("%s: band judged %d, failed %d, skipped %d", label, res.Band.Judged, len(res.Band.Failed), len(res.Band.Skipped))
	}
	return res, out.Stdout, out.Stderr
}

// judge reads the verifier's answer in the vocabulary of its kind and sets
// res.Status. An exit-code verifier answers with its exit status. A band
// verifier answers with the gate CSV it printed: any invariant outside its
// band is this candidate's `fail`, while a container that exited non-zero
// with every band held is a broken harness — a `runner_error`, which says
// nothing about the code under test (ADR-0005's distinction).
func judge(kind task.VerifyKind, res *trace.VerifyResult, stdout []byte) {
	switch kind {
	case task.KindExitCode:
		if res.ExitCode == 0 {
			res.Status = trace.VerifyPass
			return
		}
		res.Status = trace.VerifyFail
	case task.KindBand:
		b, err := band.Parse(stdout)
		if err != nil {
			res.Status = trace.VerifyRunnerError
			res.Error = err.Error()
			return
		}
		res.Band = b
		switch {
		case len(b.Failed) > 0:
			res.Status = trace.VerifyFail
		case res.ExitCode != 0:
			res.Status = trace.VerifyRunnerError
			res.Error = fmt.Sprintf("band verifier exited %d with no failing invariant", res.ExitCode)
		default:
			// Skipped invariants do not withhold a pass: an invariant the
			// gate could not measure is the gate's business, not the
			// candidate's.
			res.Status = trace.VerifyPass
		}
	}
}

// verifyTimeout resolves the verifier timeout: the flag, then the task's
// verify.timeout_seconds, then the config's. cmoa.json is read only when
// --config names it; verify needs nothing else from it.
func verifyTimeout(flagValue time.Duration, t *task.Task, cfgPath string, stderr io.Writer) (time.Duration, int) {
	var cfg *config.Config
	if cfgPath != "" {
		var err error
		if cfg, err = config.Load(cfgPath); err != nil {
			fmt.Fprintln(stderr, "cmoa:", err)
			return 0, exitUsage
		}
	}
	switch {
	case flagValue > 0:
		return flagValue, exitOK
	case t.Verify.TimeoutSeconds > 0:
		return time.Duration(t.Verify.TimeoutSeconds) * time.Second, exitOK
	case cfg != nil:
		return time.Duration(cfg.Verify.TimeoutSeconds) * time.Second, exitOK
	}
	return 0, exitOK
}

// verifyExit maps a status to the process exit code: 0 the verifier passed,
// 1 it answered no, 3 it could not be run.
func verifyExit(s trace.VerifyStatus) int {
	switch s {
	case trace.VerifyPass:
		return exitOK
	case trace.VerifyFail, trace.VerifyApplyFailed, trace.VerifyTimeout:
		return exitVerifyNo
	case trace.VerifyRunnerError:
		return exitVerifyRunner
	case trace.VerifySkipped:
		// Unreachable: nothing is skipped when the diff is named on the
		// command line. Falling through beats a panic, which would follow a
		// JSON object the caller has already been handed.
	}
	return exitVerifyRunner
}

// readDiff reads the diff to verify; "-" is stdin.
func readDiff(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// label8 is the default label: 8 hex digits, enough to keep concurrent
// verifications of one task in separate compose projects.
func label8() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("cmoa: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// tail returns at most n trailing bytes of b, whole lines where it can.
func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
		if i := strings.IndexByte(string(b), '\n'); i >= 0 && i+1 < len(b) {
			b = b[i+1:]
		}
	}
	return strings.TrimSpace(string(b))
}
