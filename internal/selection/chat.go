package selection

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/judge"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// RuleJudgePairwise is what select.json records on the chat face.
const RuleJudgePairwise = "judge-pairwise"

// ChatOptions tune a chat-face selection.
type ChatOptions struct {
	// Client sends the judge's requests; nil means a default client.
	Client *llm.Client
	// Seed overrides the presentation permutation and the nonce, never the
	// judge's own sampling seed.
	Seed *int64
	// Log receives one line per event; nil discards.
	Log func(format string, args ...any)
	Now func() time.Time
}

// RunChat asks the judge about the answers in dir and writes judge/*.json,
// judge.json and select.json. It returns the Selection and an error only
// when the trace could not be written; a judge that failed or timed out is
// a Selection, not an error, so the run still records what happened.
func RunChat(ctx context.Context, cfg *config.Config, t *task.Task, dir trace.Dir, opt ChatOptions) (Selection, error) {
	logf := opt.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	if t.Chat == nil {
		return nil, fmt.Errorf("selection: task %s is not a chat task", t.ID)
	}
	if cfg.Judge == nil {
		return nil, ErrNoJudge
	}
	// Both write-once files are checked before a single judge call is made.
	// judge.json lands first, so guarding only on select.json would let an
	// interrupted run pay for the whole protocol a second time and then die
	// at the write.
	for _, f := range []string{dir.JudgeFile(), dir.SelectFile()} {
		if _, err := os.Stat(f); err == nil {
			return nil, fmt.Errorf("selection: %s already has %s; a run is selected once", dir, filepath.Base(f))
		}
	}
	run, err := dir.ReadRun()
	if err != nil {
		return nil, err
	}
	if run.Task.ID != string(t.ID) {
		return nil, fmt.Errorf("selection: run %s is for task %q, not %q", dir.ID(), run.Task.ID, t.ID)
	}
	if run.Face != trace.FaceChat {
		return nil, fmt.Errorf("selection: run %s is a %s run; RunChat selects on the chat face", dir.ID(), run.Face)
	}

	// The run's own proposer list is the canonical order: it holds the
	// external ids `cmoa judge` invented as readily as the configured pool.
	var order []string
	var cands []judge.Candidate
	for _, p := range run.Proposers {
		order = append(order, p.ID)
		c, err := dir.ReadCandidate(p.ID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // proposer absent from this run
			}
			return nil, err
		}
		if c.Status != trace.CandidateOK {
			logf("%s: %s, not judged", p.ID, c.Status)
			continue
		}
		answer, err := dir.ReadCandidateAnswer(p.ID)
		if err != nil {
			return nil, err
		}
		cands = append(cands, judge.Candidate{ID: p.ID, Answer: answer})
	}
	logf("judging %d of %d answers", len(cands), len(order))

	client := opt.Client
	if client == nil {
		client = &llm.Client{HTTP: &http.Client{}}
	}
	j := &judge.Judge{Cfg: cfg.Judge, Client: client, Dir: dir, Now: now, Log: logf}
	rep, err := j.Run(ctx, judge.Input{
		RunID:        dir.ID(),
		Conversation: t.Chat.Conversation,
		Reference:    t.Chat.ReferenceAnswer,
		Rubric:       t.Chat.Rubric,
		AllowTie:     t.Chat.AllowTie,
		Candidates:   cands,
		Seed:         opt.Seed,
	})
	if err != nil {
		return nil, err
	}
	sel := FromJudge(rep, len(order))
	rec := &trace.Select{
		SchemaVersion: trace.SchemaVersion,
		RunID:         dir.ID(),
		Rule:          RuleJudgePairwise,
		Order:         order,
		Selection:     Record(sel),
		AlsoPassed:    []string{},
		Ranked:        rep.Ranked,
		MaxParallel:   cfg.Judge.Parallel,
		FinishedAt:    now().UTC(),
	}
	if err := dir.WriteSelect(rec); err != nil {
		return nil, err
	}
	return sel, nil
}

// ErrNoJudge is returned when a chat run is selected against a
// configuration that declares no judge.
var ErrNoJudge = errors.New("selection: the chat face needs a judge: cmoa.json declares none (version 2 adds the judge block)")

// FromJudge turns a judge report into the sealed Selection. tried is how
// many candidates the run asked for, which is not how many the judge saw:
// a proposer that failed never reached it.
func FromJudge(rep *trace.JudgeReport, tried int) Selection {
	switch rep.Outcome.Kind {
	case trace.SelectionSelected:
		return Selected{CandidateID: config.ProposerID(rep.Outcome.CandidateID), Reason: rep.Outcome.Reason}
	case trace.SelectionJudgeTimeout:
		return JudgeTimeout{After: time.Duration(rep.LatencyMS) * time.Millisecond}
	case trace.SelectionJudgeFailed:
		return JudgeFailed{Err: errors.New(rep.Outcome.Reason)}
	case trace.SelectionNoCandidate:
		return NoCandidate{Tried: tried, Reason: trace.NoCandidateReason(rep.Outcome.Reason)}
	case trace.SelectionVerifierFailed:
	}
	return NoCandidate{Tried: tried, Reason: trace.ReasonNoMajority}
}
