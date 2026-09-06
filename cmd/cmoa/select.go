package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/selection"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

func cmdSelect(ctx context.Context, args []string, stdout, stderr io.Writer, logf func(string, ...any)) int {
	fs := flag.NewFlagSet("select", flag.ContinueOnError)
	fs.SetOutput(stderr)
	taskDir := fs.String("task", "", "task directory")
	cfgPath := fs.String("config", "", "cmoa.json (default: $CMOA_CONFIG, <task>/cmoa.json, ./cmoa.json)")
	runDir := fs.String("run", "", "run directory (default: the latest under <task>/runs)")
	if code, done := parseArgs(fs, args); done {
		return code
	}
	cfg, t, code := loadTaskConfig(*cfgPath, *taskDir, stderr, logf)
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
