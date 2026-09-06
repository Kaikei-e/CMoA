// Package judge is the chat face's aggregation: the candidates are first
// compared with each other, and only when they disagree is a single model
// asked to compare them two at a time, in both orders. There is no panel,
// no synthesis and no vote among proposers.
//
// Stage 1 is consensus. Every answer is normalised — see Normalize — and
// when more than half the candidates say the same thing, one of them is
// returned and the judge is never called. Agreement between independent
// proposers is evidence in its own right, and it is the cheapest evidence
// in the system: it costs nothing and it arrives first.
//
// Stage 2 is the judge, round-robin pairwise with an order swap: three
// candidates make three pairs and six calls. A pair is won only when both
// orders name the same candidate; a tie in either order, or a disagreement
// between the orders, is a draw. Draws are not discarded — a win scores 1
// and a draw the judge answered scores 0.5 to each side, which is the
// Copeland score, and the swap protocol's own reading of an inconsistent
// pair as a tie. A candidate that wins every pair is still named the
// Condorcet winner; otherwise the highest score is selected, and candidates
// the score cannot part go to the deterministic chain in tieBreak. A draw
// nobody could measure scores nothing, and a judge that timed out or could
// not be reached is still a failure rather than a score.
//
// So no_candidate now means only that there was nothing to select between,
// or that the judge could not be read: too_few_candidates and
// invalid_output. There is no re-ask beyond one retry for malformed JSON.
//
// Everything the judge saw is reconstructible from the trace: the
// permutation and the nonce, the exact request and response of every call,
// what the sanitiser rewrote, and which candidates carried injection-shaped
// text.
package judge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/prompt"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// Candidate is one answer to compare, by the id the run knows it as.
type Candidate struct {
	ID     string
	Answer string
}

// Input is one selection: the task as the judge sees it, and the answers.
type Input struct {
	RunID        trace.RunID
	Conversation []task.ConvMessage
	Reference    string // the task's reference answer; the proposers never saw it
	Rubric       string // the task's rubric; empty means the generic one
	AllowTie     bool
	Candidates   []Candidate // in the order the caller considers canonical
	// Seed overrides the presentation permutation and the nonce, so a
	// re-run can be asked for a different presentation of the same
	// candidates. It never touches the judge's own sampling seed.
	Seed *int64
}

// Judge asks one endpoint and writes what it answered.
type Judge struct {
	Cfg *config.Judge
	// Client performs the calls. Live speaks HTTP; a Replayer answers from
	// a run already made.
	Client Completer
	Dir    trace.Dir
	Now    func() time.Time
	// Log is called from the goroutine that made each call, so it must be
	// safe for concurrent use.
	Log func(format string, args ...any)
}

// Run performs the protocol and writes judge/<pair>-<order>.json and
// judge.json. It returns the report; an error means the trace could not be
// written, never that the judge disagreed with itself.
func (j *Judge) Run(ctx context.Context, in Input) (*trace.JudgeReport, error) {
	if j.Cfg == nil {
		return nil, errors.New("judge: no judge is configured")
	}
	now := j.Now
	if now == nil {
		now = time.Now
	}
	logf := j.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	started := now()

	// Refuse before spending, not after. judge.json is write-once and is
	// written last, so a run that already has one would otherwise pay for
	// all six calls and then fail at the final write.
	if _, err := os.Stat(j.Dir.JudgeFile()); err == nil {
		return nil, fmt.Errorf("%w: %s; a run is judged once", trace.ErrExists, j.Dir.JudgeFile())
	}
	// A previous attempt that died between the calls and judge.json leaves
	// call files behind. They belong to a selection that never happened, and
	// a different seed makes different pairs, so they are cleared rather
	// than left to be read as part of this one.
	if n, err := clearCalls(j.Dir); err != nil {
		return nil, err
	} else if n > 0 {
		logf("judge: cleared %d call file(s) from an attempt that left no judge.json", n)
	}

	rep := j.newReport(in)
	texts, tx := prepareCandidates(rep, in.Candidates)
	seed, seedSource := PresentationSeed(in.RunID, in.Seed)
	nonce := Nonce(seed)
	rep.Presentation = trace.Presentation{Seed: seed, SeedSource: seedSource, Nonce: nonce}

	if len(in.Candidates) < 2 {
		Aggregate(rep, nil)
		return rep, j.finish(rep, started, now)
	}

	// Stage 1, before the call list exists: a unanimous answer costs no
	// judge call at all. The sanitised text is what is compared, so the
	// candidates are read exactly as the judge would have read them.
	if cons, tb := consensus(rep.Candidates, tx); cons != nil {
		rep.Consensus, rep.TieBreak = cons, tb
		logf("consensus: %d of %d agree (%s), no judge call", len(tb.Among), len(rep.Candidates), cons.Agreement)
		Aggregate(rep, tx)
		return rep, j.finish(rep, started, now)
	}

	if err := j.runPairs(ctx, in, rep, texts, nonce, now, logf); err != nil {
		return nil, err
	}
	Aggregate(rep, tx)
	return rep, j.finish(rep, started, now)
}

// newReport creates the trace skeleton before candidate-specific data is
// added. Keeping its complete initial state in one place makes each protocol
// exit (too few candidates, consensus, and pairwise judging) write the same
// report shape.
func (j *Judge) newReport(in Input) *trace.JudgeReport {
	return &trace.JudgeReport{
		SchemaVersion: trace.SchemaVersion,
		RunID:         in.RunID,
		Judge: trace.JudgeParams{
			Model: j.Cfg.Model, BaseURL: j.Cfg.BaseURL, Temperature: *j.Cfg.Temperature,
			Seed: j.Cfg.Seed, MaxTokens: j.Cfg.MaxTokens, OutputFormat: string(j.Cfg.OutputFormat),
			Parallel: j.Cfg.Parallel, AllowTie: in.AllowTie, PromptVersion: prompt.Version(),
			ExtraBody: j.Cfg.ExtraBody,
		},
		Candidates:     []string{},
		Wins:           map[string]int{},
		Pairs:          []trace.JudgePair{},
		Ranked:         []string{},
		Sanitized:      []trace.Sanitized{},
		InjectionFlags: map[string][]string{},
	}
}

// prepareCandidates records the candidate data that affects a selection and
// returns exactly the sanitised text used by consensus and pairwise prompts.
func prepareCandidates(rep *trace.JudgeReport, candidates []Candidate) ([]string, Texts) {
	texts := make([]string, len(candidates))
	tx := Texts{}
	for i, c := range candidates {
		rep.Candidates = append(rep.Candidates, c.ID)
		rep.Wins[c.ID] = 0
		clean, rewrites := Sanitize(c.Answer)
		texts[i] = clean
		tx[c.ID] = Text{Norm: Normalize(clean), Raw: clean}
		for _, r := range rewrites {
			rep.Sanitized = append(rep.Sanitized, trace.Sanitized{Candidate: c.ID, What: r.What, Count: r.Count})
		}
		rep.InjectionFlags[c.ID] = InjectionFlags(c.Answer)
	}
	return texts, tx
}
func (j *Judge) finish(rep *trace.JudgeReport, started time.Time, now func() time.Time) error {
	rep.FinishedAt = now().UTC()
	rep.LatencyMS = rep.FinishedAt.Sub(started).Milliseconds()
	return j.Dir.WriteJudge(rep)
}

// clearCalls removes the judge/ call files of an attempt that left no
// judge.json, and returns how many there were.
func clearCalls(dir trace.Dir) (int, error) {
	entries, err := os.ReadDir(dir.JudgeDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(dir.JudgeDir(), e.Name())); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
