package propose

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/harness"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// External creates a run for candidates that were handed to CMoA rather
// than proposed: `cmoa judge` reads answers off the command line, so no
// proposer is asked and the run records where each answer came from. The
// run is a chat run in every other respect, and select judges it the same
// way.
func External(ctx context.Context, cfg *config.Config, t *task.Task, answers []ExternalAnswer, opt Options) (trace.Dir, error) {
	logf, now := externalDependencies(opt)
	if err := validateExternal(t, cfg, answers); err != nil {
		return "", err
	}
	snap, err := harness.Take(ctx, cfg.Harness.Vault, cfg.Harness.Docdag, opt.AsOf)
	if err != nil {
		return "", fmt.Errorf("propose: harness snapshot: %w", err)
	}
	dir, run, err := createExternalRun(cfg, t, answers, opt, snap, now)
	if err != nil {
		return "", err
	}
	if err := dir.WriteRun(run); err != nil {
		return "", err
	}
	return writeExternalCandidates(dir, answers, now, logf)
}

// ExternalAnswer is one candidate read off the command line.
type ExternalAnswer struct {
	File string // as written on the command line, for the record
	Text string
}

func externalDependencies(opt Options) (func(string, ...any), func() time.Time) {
	logf := opt.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	return logf, now
}

func validateExternal(t *task.Task, cfg *config.Config, answers []ExternalAnswer) error {
	if t.Face != task.FaceChat {
		return fmt.Errorf("propose: external candidates are the chat face's; task %s is %s", t.ID, t.Face)
	}
	if cfg.Judge == nil {
		return ErrNoJudge
	}
	if len(answers) < 1 {
		return errors.New("propose: at least one candidate file is required")
	}
	return nil
}

func createExternalRun(cfg *config.Config, t *task.Task, answers []ExternalAnswer, opt Options, snap *harness.Snapshot, now func() time.Time) (trace.Dir, *trace.Run, error) {
	id := opt.RunID
	if id == "" {
		id = trace.NewRunID(now())
	}
	dir, err := trace.Create(t.Dir, id)
	if err != nil {
		return "", nil, err
	}
	run, err := newRun(cfg, t, id, "", snap, opt, now)
	if err != nil {
		return "", nil, err
	}
	run.CandidatesOrigin = trace.OriginExternal
	if opt.Replayed != nil {
		run.CandidatesOrigin = trace.OriginReplay
		run.Replayed = opt.Replayed
	}
	run.Proposers = nil
	run.Byzantine = trace.Byzantine{N: len(answers), F: (len(answers) - 1) / 3}
	for i, a := range answers {
		cid := fmt.Sprintf("c%d", i+1)
		run.Proposers = append(run.Proposers, trace.ProposerRef{ID: cid})
		run.ExternalCandidates = append(run.ExternalCandidates, trace.ExternalCandidate{
			ID: cid, File: a.File, SHA256: llm.SHA256([]byte(a.Text)),
		})
	}
	return dir, run, nil
}

func writeExternalCandidates(dir trace.Dir, answers []ExternalAnswer, now func() time.Time, logf func(string, ...any)) (trace.Dir, error) {
	at := now().UTC()
	for i, answer := range answers {
		cid := fmt.Sprintf("c%d", i+1)
		cand := &trace.Candidate{
			ProposerID: cid,
			Face:       string(task.FaceChat),
			Origin:     trace.OriginExternal,
			StartedAt:  at,
			FinishedAt: at,
		}
		body := classifyChat(cand, answer.Text)
		logf("%s: %s (%s, %d bytes)", cid, cand.Status, answer.File, len(body))
		if err := dir.WriteChatCandidate(cand, []byte(answer.Text), body); err != nil {
			return "", err
		}
	}
	return dir, nil
}
