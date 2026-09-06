package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"

	"github.com/Kaikei-e/CMoA/internal/harness"
	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

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
	if code, done := parseArgs(fs, args); done {
		return code
	}
	set := explicitFlags(fs)
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
	h, code := loadHarnessFlag(set, *harnessDir, stderr)
	if code != exitOK {
		return code
	}
	opt.Harness = h
	cfg, t, code := loadTaskConfig(*cfgPath, *taskDir, stderr, logf)
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
