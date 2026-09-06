package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/harnessdir"
	"github.com/Kaikei-e/CMoA/internal/task"
)

// parseArgs parses a subcommand's flags. --help prints the usage and exits 0,
// the convention every caller of a CLI relies on to ask "does this command
// exist?"; any other parse error is a usage error (2).
func parseArgs(fs *flag.FlagSet, args []string) (code int, done bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK, true
		}
		return exitUsage, true
	}
	return exitOK, false
}

func loadTaskConfig(flagConfig, taskDir string, stderr io.Writer, logf func(string, ...any)) (*config.Config, *task.Task, int) {
	if taskDir == "" {
		fmt.Fprintln(stderr, "cmoa: --task is required")
		return nil, nil, exitUsage
	}
	t, err := task.Load(taskDir, task.WithLog(logf))
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
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

// loadHarnessFlag loads the harness without depending on a command's options.
// A missing directory is a usage error; a malformed one is an input error.
// An explicitly empty value is invalid, rather than equivalent to omission.
func loadHarnessFlag(set map[string]bool, dir string, stderr io.Writer) (*harnessdir.Dir, int) {
	if !set["harness"] {
		return nil, exitOK
	}
	if dir == "" {
		fmt.Fprintln(stderr, "cmoa: --harness needs a directory; omit the flag to run without a harness")
		return nil, exitUsage
	}
	h, err := harnessdir.Load(dir)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		if errors.Is(err, harnessdir.ErrNotFound) {
			return nil, exitUsage
		}
		return nil, exitInvalid
	}
	return h, exitOK
}

// explicitFlags distinguishes an omitted flag from an explicitly supplied zero.
func explicitFlags(fs *flag.FlagSet) map[string]bool {
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}
