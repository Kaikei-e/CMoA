package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Kaikei-e/CMoA/internal/harness"
	"github.com/Kaikei-e/CMoA/internal/judge"
	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/selection"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// candidateFiles collects a repeated --candidate flag in the order it was
// given. The order is the caller's, and the trace records it: the judge's
// own presentation order is a permutation of it, chosen from the run id.
type candidateFiles []string

func (c *candidateFiles) String() string { return strings.Join(*c, ",") }

func (c *candidateFiles) Set(v string) error {
	if v == "" {
		return errors.New("needs a file")
	}
	*c = append(*c, v)
	return nil
}

func cmdJudge(ctx context.Context, args []string, stdout, stderr io.Writer, logf func(string, ...any)) int {
	fs := flag.NewFlagSet("judge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	taskDir := fs.String("task", "", "chat task directory")
	cfgPath := fs.String("config", "", "cmoa.json (default: $CMOA_CONFIG, <task>/cmoa.json, ./cmoa.json)")
	var files candidateFiles
	fs.Var(&files, "candidate", "a file holding one candidate answer; repeat for each candidate")
	runID := fs.String("run-id", "", "run id to create (default: generated)")
	asOf := fs.String("as-of", "", "day the harness is read for, YYYY-MM-DD (default today)")
	harnessDir := fs.String("harness", "", "rendered harness directory (default: none)")
	seed := fs.Int64("seed", 0, "presentation seed: it permutes the candidates, never the judge's sampling")
	judgeSeed := fs.Int64("judge-seed", 0, "override the judge's own sampling seed")
	replayFrom := fs.String("replay-from", "",
		"re-aggregate this run directory's recorded judge answers instead of asking the judge")
	if code, done := parseArgs(fs, args); done {
		return code
	}
	set := explicitFlags(fs)
	if set["replay-from"] {
		for _, f := range []string{"candidate", "seed", "judge-seed"} {
			if set[f] {
				fmt.Fprintf(stderr, "cmoa: --replay-from reads the candidates and the seed "+
					"from the run it replays; --%s would ask a different question\n", f)
				return exitUsage
			}
		}
	} else if len(files) == 0 {
		fmt.Fprintln(stderr, "cmoa: judge needs at least one --candidate")
		return exitUsage
	}

	opt := propose.Options{AsOf: *asOf, Version: version(), Log: logf}
	h, code := loadHarnessFlag(set, *harnessDir, stderr)
	if code != exitOK {
		return code
	}
	opt.Harness = h
	cfg, t, code := loadTaskConfig(*cfgPath, *taskDir, stderr, logf)
	if code != exitOK {
		return code
	}
	if t.Face != task.FaceChat {
		fmt.Fprintf(stderr, "cmoa: task %s is a %s task; judge compares chat answers\n", t.ID, t.Face)
		return exitInvalid
	}
	if cfg.Judge == nil {
		fmt.Fprintln(stderr, "cmoa:", selection.ErrNoJudge)
		return exitInvalid
	}
	if set["judge-seed"] {
		cfg.Judge.Seed = judgeSeed
	}
	var replay *judge.Replayer
	if set["replay-from"] {
		var code int
		if replay, code = openReplay(*replayFrom, t, cfg, stderr); code != exitOK {
			return code
		}
		files = nil
	}
	if *runID != "" {
		id, err := trace.ParseRunID(*runID)
		if err != nil {
			fmt.Fprintln(stderr, "cmoa:", err)
			return exitUsage
		}
		opt.RunID = id
	}
	answers, code := readCandidates(files, stderr)
	if code != exitOK {
		return code
	}
	if replay != nil {
		if answers, code = replayCandidates(*replayFrom, stderr); code != exitOK {
			return code
		}
		opt.Replayed = replay.Record()
	}

	dir, err := propose.External(ctx, cfg, t, answers, opt)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		if errors.Is(err, harness.ErrNoVault) || errors.Is(err, propose.ErrNoJudge) {
			return exitInvalid
		}
		return exitRuntime
	}
	chatOpt := selection.ChatOptions{Log: logf}
	if set["seed"] {
		chatOpt.Seed = seed
	}
	if replay != nil {
		// The recorded seed, not the one this run id would derive: the
		// nonce inside the candidate fences is part of every request the
		// replayer is about to answer.
		recorded := replay.Seed()
		chatOpt.Client, chatOpt.Seed = replay, &recorded
	}
	sel, err := selection.RunChat(ctx, cfg, t, dir, chatOpt)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitRuntime
	}
	if err := printOutcome(stdout, dir, sel); err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitRuntime
	}
	fmt.Fprintln(stdout, string(dir))
	return exitOK
}

// readCandidates reads the answers named on the command line. An unreadable
// or empty file is an input error, not a candidate with a status: nobody
// asked a model, so there is nothing to record.
func readCandidates(files []string, stderr io.Writer) ([]propose.ExternalAnswer, int) {
	var out []propose.ExternalAnswer
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(stderr, "cmoa:", err)
			return nil, exitInvalid
		}
		if !utf8.Valid(b) {
			fmt.Fprintf(stderr, "cmoa: %s is not valid UTF-8; the judge only sees text\n", f)
			return nil, exitInvalid
		}
		if strings.TrimSpace(string(b)) == "" {
			fmt.Fprintf(stderr, "cmoa: %s is empty; a candidate with no answer is not a candidate\n", f)
			return nil, exitInvalid
		}
		out = append(out, propose.ExternalAnswer{File: filepath.ToSlash(f), Text: string(b)})
	}
	return out, exitOK
}
