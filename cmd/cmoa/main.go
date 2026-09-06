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
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
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
