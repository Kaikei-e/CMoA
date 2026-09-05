// Command cmoa is the CMoA runtime: a coding face selected by a verifier
// and a chat face selected by a judge.
//
//	cmoa propose --task <dir> [--config <file>] [--as-of YYYY-MM-DD] [--run-id <id>]
//	             [--harness <dir>] [--seed <int>] [--temperature <float>]
//	cmoa select  --task <dir> [--config <file>] [--run <run-dir>]
//	cmoa verify  --task <dir> --diff <file> [--out <dir>] [--timeout <dur>] [--label <name>] [--config <file>]
//	cmoa judge   --task <dir> --candidate <file> --candidate <file> [--candidate <file>]
//	             [--config <file>] [--run-id <id>] [--seed <int>] [--judge-seed <int>]
//	             [--as-of YYYY-MM-DD] [--harness <dir>]
//	cmoa serve   [--config <file>] [--listen <addr>] [--harness <dir>] [--as-of YYYY-MM-DD]
//	             [--allow-remote]
//	cmoa surfaces [--format text|json]
//	cmoa version
//
// propose and select work on both faces; which face a run is on comes from
// task.json. judge is the chat face without the proposers: it takes answers
// somebody else produced and runs the pairwise protocol over them, which is
// what a calibration needs. serve is the chat face behind an
// OpenAI-compatible endpoint.
//
// Exit codes: 0 success, 1 runtime error, 2 usage, 3 configuration or task
// validation error. select and judge exit 0 whatever the Selection is; the
// outcome is in select.json and on stdout. verify answers about one diff,
// so it spends the codes differently: 0 the verifier passed, 1 it answered
// no (fail, apply_failed, timeout), 2 usage or task error, 3 the verifier
// could not be run. A --harness directory that is not there is a usage
// error (2); one that is there and malformed is an input error (3).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/Kaikei-e/CMoA"
	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/harness"
	"github.com/Kaikei-e/CMoA/internal/harnessdir"
	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/selection"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

const (
	exitOK      = 0
	exitRuntime = 1
	exitUsage   = 2
	exitInvalid = 3
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logf := func(format string, a ...any) { fmt.Fprintf(stderr, format+"\n", a...) }
	switch args[0] {
	case "propose":
		return cmdPropose(ctx, args[1:], stdout, stderr, logf)
	case "select":
		return cmdSelect(ctx, args[1:], stdout, stderr, logf)
	case "verify":
		return cmdVerify(ctx, args[1:], stdout, stderr, logf)
	case "judge":
		return cmdJudge(ctx, args[1:], stdout, stderr, logf)
	case "serve":
		return cmdServe(ctx, args[1:], stderr, logf)
	case "surfaces":
		return cmdSurfaces(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, version())
		return exitOK
	case "-h", "--help", "help":
		usage(stdout)
		return exitOK
	}
	fmt.Fprintf(stderr, "cmoa: unknown command %q\n", args[0])
	usage(stderr)
	return exitUsage
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage:
  cmoa propose --task <dir> [--config <file>] [--as-of YYYY-MM-DD] [--run-id <id>]
               [--harness <dir>] [--seed <int>] [--temperature <float>]
  cmoa select  --task <dir> [--config <file>] [--run <run-dir>]
  cmoa verify  --task <dir> --diff <file> [--out <dir>] [--timeout <dur>] [--label <name>] [--config <file>]
  cmoa judge   --task <dir> --candidate <file> --candidate <file> [--candidate <file>]
               [--config <file>] [--run-id <id>] [--seed <int>] [--judge-seed <int>]
               [--as-of YYYY-MM-DD] [--harness <dir>]
  cmoa serve   [--config <file>] [--listen <addr>] [--harness <dir>] [--as-of YYYY-MM-DD]
               [--allow-remote]
  cmoa surfaces [--format text|json]
  cmoa version

propose and select work on both faces; task.json says which. judge runs the
chat face's pairwise protocol over answers produced elsewhere. serve is the
chat face behind an OpenAI-compatible endpoint on loopback.
`)
}

func version() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 12 {
				return "dev-" + s.Value[:12]
			}
		}
	}
	return "dev"
}

func loadAll(flagConfig, taskDir string, stderr io.Writer, logf func(string, ...any)) (*config.Config, *task.Task, int) {
	if taskDir == "" {
		fmt.Fprintln(stderr, "cmoa: --task is required")
		return nil, nil, exitUsage
	}
	t, err := task.Load(taskDir, task.WithLog(logf))
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		if _, ok := errors.AsType[*task.ValidationError](err); ok {
			return nil, nil, exitInvalid
		}
		return nil, nil, exitInvalid
	}
	path, err := config.Discover(flagConfig, t.Dir)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return nil, nil, exitInvalid
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return nil, nil, exitInvalid
	}
	return cfg, t, exitOK
}

func cmdPropose(ctx context.Context, args []string, stdout, stderr io.Writer, logf func(string, ...any)) int {
	fs := flag.NewFlagSet("propose", flag.ContinueOnError)
	fs.SetOutput(stderr)
	taskDir := fs.String("task", "", "task directory")
	cfgPath := fs.String("config", "", "cmoa.json (default: $CMOA_CONFIG, <task>/cmoa.json, ./cmoa.json)")
	asOf := fs.String("as-of", "", "day the harness is read for, YYYY-MM-DD (default today)")
	runID := fs.String("run-id", "", "run id to create (default: generated)")
	harnessDir := fs.String("harness", "", "rendered harness directory (default: none)")
	seed := fs.Int64("seed", 0, "override every proposer's seed (pair with --temperature)")
	temperature := fs.Float64("temperature", 0, "override every proposer's temperature (pair with --seed)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	opt := propose.Options{AsOf: *asOf, Version: version(), Log: logf}
	if set["seed"] {
		opt.Seed = seed
	}
	if set["temperature"] {
		if math.IsNaN(*temperature) || math.IsInf(*temperature, 0) || *temperature < 0 || *temperature > 2 {
			fmt.Fprintf(stderr, "cmoa: --temperature %v is outside [0, 2]\n", *temperature)
			return exitUsage
		}
		opt.Temperature = temperature
	}
	if code := loadHarnessFlag(set, *harnessDir, &opt, stderr); code != exitOK {
		return code
	}
	cfg, t, code := loadAll(*cfgPath, *taskDir, stderr, logf)
	if code != exitOK {
		return code
	}
	if *runID != "" {
		id, err := trace.ParseRunID(*runID)
		if err != nil {
			fmt.Fprintln(stderr, "cmoa:", err)
			return exitUsage
		}
		opt.RunID = id
	}
	dir, err := propose.Run(ctx, cfg, t, opt)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		if errors.Is(err, harness.ErrNoVault) || errors.Is(err, propose.ErrContextBudget) || errors.Is(err, propose.ErrNoJudge) {
			return exitInvalid
		}
		return exitRuntime
	}
	fmt.Fprintln(stdout, string(dir))
	return exitOK
}

// loadHarnessFlag reads --harness into opt.Harness. It is shared by every
// command that takes one, so the flag means the same thing everywhere: an
// absent directory is a usage error, a malformed one an input error, and
// an empty string is neither "no harness" nor allowed.
func loadHarnessFlag(set map[string]bool, dir string, opt *propose.Options, stderr io.Writer) int {
	if !set["harness"] {
		return exitOK
	}
	if dir == "" {
		fmt.Fprintln(stderr, "cmoa: --harness needs a directory; omit the flag to run without a harness")
		return exitUsage
	}
	h, err := harnessdir.Load(dir)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		if errors.Is(err, harnessdir.ErrNotFound) {
			return exitUsage
		}
		return exitInvalid
	}
	opt.Harness = h
	return exitOK
}

func cmdSelect(ctx context.Context, args []string, stdout, stderr io.Writer, logf func(string, ...any)) int {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	fs.SetOutput(stderr)
	taskDir := fs.String("task", "", "task directory")
	cfgPath := fs.String("config", "", "cmoa.json (default: $CMOA_CONFIG, <task>/cmoa.json, ./cmoa.json)")
	runDir := fs.String("run", "", "run directory (default: the latest under <task>/runs)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	cfg, t, code := loadAll(*cfgPath, *taskDir, stderr, logf)
	if code != exitOK {
		return code
	}
	// A band verifier measures; it does not answer yes or no about a diff
	// nobody asked it about. Proposing candidates for one is a task design
	// that does not exist yet, so select refuses it rather than reading the
	// container's exit code as a verdict it does not carry.
	if t.Face == task.FaceCoding {
		switch t.Verify.Kind {
		case task.KindExitCode:
		case task.KindBand:
			fmt.Fprintf(stderr, "cmoa: task %s declares verify.kind band; select judges candidates on exit-code verifiers only. Use cmoa verify for one diff.\n", t.ID)
			return exitInvalid
		}
	}
	var dir trace.Dir
	var err error
	if *runDir != "" {
		dir, err = trace.Open(*runDir)
	} else {
		dir, err = trace.Latest(t.Dir)
	}
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitInvalid
	}
	if t.Face == task.FaceChat {
		return cmdSelectChat(ctx, cfg, t, dir, stdout, stderr, logf)
	}
	sel, err := selection.Run(ctx, cfg, t, dir, selection.Options{Log: logf})
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitRuntime
	}
	rec := selection.Record(sel)
	switch v := sel.(type) {
	case selection.Selected:
		fmt.Fprintf(stdout, "%s %s %s\n", rec.Kind, v.CandidateID, dir.CandidateDiff(string(v.CandidateID)))
	case selection.NoCandidate:
		fmt.Fprintf(stdout, "%s tried=%d\n", rec.Kind, v.Tried)
	case selection.JudgeTimeout:
		fmt.Fprintf(stdout, "%s after=%s\n", rec.Kind, v.After)
	case selection.JudgeFailed:
		fmt.Fprintf(stdout, "%s %s\n", rec.Kind, v.Err)
	case selection.VerifierFailed:
		fmt.Fprintf(stdout, "%s %s\n", rec.Kind, v.Err)
	}
	return exitOK
}

func cmdSurfaces(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("surfaces", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "text or json")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	type row struct {
		Surface  cmoa.Surface  `json:"surface"`
		Autonomy cmoa.Autonomy `json:"autonomy"`
	}
	var rows []row
	for _, s := range cmoa.AllSurfaces() {
		rows = append(rows, row{s, s.Autonomy()})
	}
	switch *format {
	case "json":
		out := struct {
			Surfaces []row                    `json:"surfaces"`
			ReadOnly []cmoa.ReadOnlyComponent `json:"read_only"`
		}{rows, cmoa.ReadOnlyComponents()}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return exitRuntime
		}
	case "text":
		for _, r := range rows {
			fmt.Fprintf(stdout, "%-20s %s\n", r.Surface, r.Autonomy)
		}
		for _, c := range cmoa.ReadOnlyComponents() {
			fmt.Fprintf(stdout, "%-20s read-only\n", c)
		}
	default:
		fmt.Fprintf(stderr, "cmoa: --format must be text or json\n")
		return exitUsage
	}
	return exitOK
}
