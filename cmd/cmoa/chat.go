package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/harness"
	"github.com/Kaikei-e/CMoA/internal/judge"
	"github.com/Kaikei-e/CMoA/internal/prompt"
	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/selection"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// outcome is what select and judge print on the chat face: one JSON object
// on one line. The coding face prints a text line, because a diff path is
// what a caller does something with; a chat outcome is read by uzushio,
// which wants the sub-reason and the ranking, not prose.
type outcome struct {
	Kind        string   `json:"kind"`
	CandidateID string   `json:"candidate_id,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	Answer      string   `json:"answer,omitempty"` // path to the selected answer
	Ranked      []string `json:"ranked,omitempty"`
	Run         string   `json:"run"`
	Judge       string   `json:"judge"` // path to judge.json
}

func printOutcome(w io.Writer, dir trace.Dir, sel selection.Selection) error {
	rec := selection.Record(sel)
	out := outcome{
		Kind: string(rec.Kind), CandidateID: rec.CandidateID, Reason: rec.Reason,
		Run: string(dir), Judge: dir.JudgeFile(),
	}
	if rec.Error != "" {
		out.Reason = rec.Error
	}
	if s, err := dir.ReadSelect(); err == nil {
		out.Ranked = s.Ranked
	}
	if _, ok := sel.(selection.Selected); ok {
		out.Answer = dir.CandidateAnswer(rec.CandidateID)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, string(b))
	return nil
}

// cmdSelectChat runs the judge over a chat run propose already wrote.
func cmdSelectChat(ctx context.Context, cfg *config.Config, t *task.Task, dir trace.Dir, stdout, stderr io.Writer, logf func(string, ...any)) int {
	sel, err := selection.RunChat(ctx, cfg, t, dir, selection.ChatOptions{Log: logf})
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		if errors.Is(err, selection.ErrNoJudge) {
			return exitInvalid
		}
		return exitRuntime
	}
	if err := printOutcome(stdout, dir, sel); err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return exitRuntime
	}
	return exitOK
}

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
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
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
	if code := loadHarnessFlag(set, *harnessDir, &opt, stderr); code != exitOK {
		return code
	}
	cfg, t, code := loadAll(*cfgPath, *taskDir, stderr, logf)
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

// openReplay reads the run a replay is to be made from and refuses every
// way it could be the wrong one.
//
// The prompt version is the refusal that matters. A recorded answer is an
// answer to the question the source asked, and re-aggregating it under a
// prompt that has since changed would produce a report about a judge nobody
// ran. The task is checked for the same reason at a coarser grain.
func openReplay(src string, t *task.Task, cfg *config.Config, stderr io.Writer) (*judge.Replayer, int) {
	dir := trace.Dir(src)
	run, err := dir.ReadRun()
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return nil, exitInvalid
	}
	if run.Task.ID != string(t.ID) {
		fmt.Fprintf(stderr, "cmoa: run %s judged task %q, not %q\n", dir.ID(), run.Task.ID, t.ID)
		return nil, exitInvalid
	}
	replay, err := judge.OpenReplay(dir)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return nil, exitInvalid
	}
	if got, want := replay.PromptVersion(), prompt.Version(); got != want {
		fmt.Fprintf(stderr, "cmoa: run %s was judged at prompt version %s and this binary is %s; "+
			"the recorded answers are answers to another question\n", dir.ID(), got, want)
		return nil, exitInvalid
	}
	cfg.Judge = replay.Params(cfg.Judge)
	return replay, exitOK
}

// replayCandidates reads the answers the source run judged, out of the run
// itself rather than out of wherever they were read from that day. The files
// the source names may have moved; what it judged has not.
func replayCandidates(src string, stderr io.Writer) ([]propose.ExternalAnswer, int) {
	dir := trace.Dir(src)
	run, err := dir.ReadRun()
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return nil, exitInvalid
	}
	var out []propose.ExternalAnswer
	for _, p := range run.Proposers {
		text, err := dir.ReadCandidateAnswer(p.ID)
		if err != nil {
			fmt.Fprintln(stderr, "cmoa:", err)
			return nil, exitInvalid
		}
		out = append(out, propose.ExternalAnswer{File: filepath.ToSlash(dir.CandidateAnswer(p.ID)), Text: text})
	}
	return out, exitOK
}
