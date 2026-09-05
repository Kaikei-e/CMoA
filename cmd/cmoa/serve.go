package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/serve"
)

// cmdServe runs the chat face behind an OpenAI-compatible endpoint until
// the process is interrupted. It has no --task: every request is its own
// task, written under serve.runs_dir as it arrives.
func cmdServe(ctx context.Context, args []string, stderr io.Writer, logf func(string, ...any)) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", "", "cmoa.json (default: $CMOA_CONFIG, ./cmoa.json)")
	listen := fs.String("listen", "", "override serve.listen")
	harnessDir := fs.String("harness", "", "rendered harness directory (default: none)")
	asOf := fs.String("as-of", "", "day the harness is read for, YYYY-MM-DD (default today)")
	allowRemote := fs.Bool("allow-remote", false, "allow binding an address that is not loopback; cmoa serve has no auth")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	var opt propose.Options
	if code := loadHarnessFlag(set, *harnessDir, &opt, stderr); code != exitOK {
		return code
	}
	path, err := config.Discover(*cfgPath, "")
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitInvalid
	}
	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitInvalid
	}
	if cfg.Serve == nil {
		fmt.Fprintln(stderr, "cmoa: cmoa.json declares no serve block (version 2 adds it)")
		return exitInvalid
	}
	if *listen != "" {
		cfg.Serve.Listen = *listen
	}
	if err := serve.CheckListen(cfg.Serve.Listen, *allowRemote); err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitUsage
	}
	srv, err := serve.New(cfg, serve.Options{
		AsOf: *asOf, Version: version(), Harness: opt.Harness, Log: logf,
	})
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitInvalid
	}
	if err := srv.ListenAndServe(ctx); err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitRuntime
	}
	return exitOK
}
