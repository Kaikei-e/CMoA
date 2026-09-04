// Package selection is the coding face's aggregation: every candidate that
// propose recorded as ok is applied to its own worktree and verified in a
// container; the first passing candidate in configured order is selected.
// Nothing is merged, no judge is asked. The outcome is the sealed Selection
// type, mirrored into select.json.
package selection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/patch"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
	"github.com/Kaikei-e/CMoA/internal/verify"
	"github.com/Kaikei-e/CMoA/internal/worktree"
)

// Selection is the result of select. It is sealed: the four variants below
// are the only implementations, and gochecksumtype checks that every type
// switch over it names all four.
//
//sumtype:decl
type Selection interface{ sealed() }

// Selected: one candidate passed the verifier.
type Selected struct {
	CandidateID config.ProposerID
	Reason      string
}

// NoCandidate: every candidate failed, or none was usable.
type NoCandidate struct{ Tried int }

// JudgeTimeout: the chat face's judge did not answer in time. Never
// produced on the coding face; declared so the type is complete.
type JudgeTimeout struct{ After time.Duration }

// VerifierFailed: the verifier itself could not run (docker absent, compose
// file broken). Says nothing about any candidate.
type VerifierFailed struct{ Err error }

func (Selected) sealed()       {}
func (NoCandidate) sealed()    {}
func (JudgeTimeout) sealed()   {}
func (VerifierFailed) sealed() {}

// Record converts a Selection into its trace form.
func Record(s Selection) trace.SelectionRecord {
	switch v := s.(type) {
	case Selected:
		return trace.SelectionRecord{Kind: trace.SelectionSelected, CandidateID: string(v.CandidateID), Reason: v.Reason}
	case NoCandidate:
		return trace.SelectionRecord{Kind: trace.SelectionNoCandidate, Tried: v.Tried}
	case JudgeTimeout:
		return trace.SelectionRecord{Kind: trace.SelectionJudgeTimeout, AfterMS: v.After.Milliseconds()}
	case VerifierFailed:
		return trace.SelectionRecord{Kind: trace.SelectionVerifierFailed, Error: v.Err.Error()}
	}
	panic("selection: unreachable")
}

// Options tune a run.
type Options struct {
	// Runner verifies candidates; nil means ComposeRunner with "docker".
	Runner verify.Runner
	// WorktreeRoot is where candidate checkouts are made; empty means a
	// temporary directory that is removed afterwards.
	WorktreeRoot string
	// Log receives one line per event; nil discards.
	Log func(format string, args ...any)
}

// Run verifies the candidates in runDir and writes select.json. It returns
// the Selection and an error only when the trace could not be written or
// the run directory is not usable; a VerifierFailed is a Selection, not an
// error.
func Run(ctx context.Context, cfg *config.Config, t *task.Task, dir trace.Dir, opt Options) (Selection, error) {
	logf := opt.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	runner := opt.Runner
	if runner == nil {
		runner = &verify.ComposeRunner{}
	}
	if _, err := os.Stat(dir.SelectFile()); err == nil {
		return nil, fmt.Errorf("selection: %s already has select.json; a run is selected once", dir)
	}
	run, err := dir.ReadRun()
	if err != nil {
		return nil, err
	}
	if run.Task.ID != string(t.ID) {
		return nil, fmt.Errorf("selection: run %s is for task %q, not %q", dir.ID(), run.Task.ID, t.ID)
	}

	root := opt.WorktreeRoot
	if root == "" {
		root, err = os.MkdirTemp("", "cmoa-wt-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(root) }()
	}

	// Candidates in configured order; only the ok ones are verified.
	var order []string
	type job struct {
		id   config.ProposerID
		diff string
	}
	var jobs []job
	for _, p := range cfg.Proposers {
		order = append(order, string(p.ID))
		c, err := dir.ReadCandidate(string(p.ID))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // proposer absent from this run
			}
			return nil, err
		}
		if c.Status != trace.CandidateOK {
			if err := dir.WriteVerify(&trace.VerifyResult{CandidateID: string(p.ID), Status: trace.VerifySkipped, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()}, nil, nil); err != nil {
				return nil, err
			}
			continue
		}
		d, err := dir.ReadCandidateDiff(string(p.ID))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job{id: p.ID, diff: d})
	}

	results := make(map[config.ProposerID]*trace.VerifyResult, len(jobs))
	var runnerErr error
	var mu sync.Mutex
	sem := make(chan struct{}, cfg.Verify.MaxParallel)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, stdout, stderr, rerr := verifyOne(ctx, cfg, t, run.Task.ResolvedRev, dir, runner, root, j.id, j.diff, logf)
			mu.Lock()
			defer mu.Unlock()
			results[j.id] = res
			if rerr != nil && runnerErr == nil {
				runnerErr = rerr
			}
			if werr := dir.WriteVerify(res, stdout, stderr); werr != nil && runnerErr == nil {
				runnerErr = werr
			}
		}(j)
	}
	wg.Wait()

	var sel Selection
	var passed []string
	switch {
	case runnerErr != nil:
		sel = VerifierFailed{Err: runnerErr}
	default:
		for _, j := range jobs {
			if r := results[j.id]; r != nil && r.Status == trace.VerifyPass {
				passed = append(passed, string(j.id))
			}
		}
		if len(passed) == 0 {
			sel = NoCandidate{Tried: len(order)}
		} else {
			switch cfg.Selection.Rule {
			case config.RuleFirst:
				sel = Selected{CandidateID: config.ProposerID(passed[0]), Reason: "first passing candidate in configured order"}
			}
		}
	}
	if passed == nil {
		passed = []string{}
	}
	rec := &trace.Select{
		SchemaVersion: trace.SchemaVersion,
		RunID:         dir.ID(),
		Rule:          string(cfg.Selection.Rule),
		Order:         order,
		Selection:     Record(sel),
		AlsoPassed:    alsoPassed(sel, passed),
		MaxParallel:   cfg.Verify.MaxParallel,
		FinishedAt:    time.Now().UTC(),
	}
	if err := dir.WriteSelect(rec); err != nil {
		return nil, err
	}
	return sel, nil
}

func alsoPassed(sel Selection, passed []string) []string {
	if s, ok := sel.(Selected); ok {
		out := []string{}
		for _, p := range passed {
			if p != string(s.CandidateID) {
				out = append(out, p)
			}
		}
		return out
	}
	return []string{}
}

func verifyOne(ctx context.Context, cfg *config.Config, t *task.Task, rev string, dir trace.Dir, runner verify.Runner, root string, id config.ProposerID, diff string, logf func(string, ...any)) (res *trace.VerifyResult, stdout, stderr []byte, runnerErr error) {
	res = &trace.VerifyResult{CandidateID: string(id), StartedAt: time.Now().UTC()}
	defer func() {
		res.FinishedAt = time.Now().UTC()
		res.DurationMS = res.FinishedAt.Sub(res.StartedAt).Milliseconds()
	}()
	wt := filepath.Join(root, string(id))
	if err := worktree.Add(ctx, t.Repo, rev, wt); err != nil {
		res.Status = trace.VerifyRunnerError
		res.Error = err.Error()
		return res, nil, nil, err
	}
	defer func() {
		if err := worktree.Remove(context.WithoutCancel(ctx), t.Repo, wt); err != nil {
			logf("warning: remove worktree %s: %v", wt, err)
		}
	}()
	if err := patch.Apply(ctx, wt, diff); err != nil {
		logf("%s: apply failed", id)
		res.Status = trace.VerifyApplyFailed
		res.ApplyError = err.Error()
		return res, nil, nil, nil
	}
	spec := verify.Spec{
		ComposeFile:  t.Verify.ComposeFile,
		Service:      t.Verify.Service,
		ProjectName:  verify.ProjectName(string(t.ID), string(dir.ID()), string(id)),
		CandidateDir: wt,
		Timeout:      time.Duration(cfg.Verify.TimeoutSeconds) * time.Second,
	}
	res.ProjectName = spec.ProjectName
	logf("%s: verifying in %s", id, spec.ProjectName)
	out, err := runner.Run(ctx, spec)
	if err != nil {
		res.Status = trace.VerifyRunnerError
		res.Error = err.Error()
		if out != nil {
			res.Command = out.Command
			return res, out.Stdout, out.Stderr, err
		}
		return res, nil, nil, err
	}
	res.Command = out.Command
	res.ExitCode = out.ExitCode
	switch {
	case out.TimedOut:
		res.Status = trace.VerifyTimeout
	case out.ExitCode == 0:
		res.Status = trace.VerifyPass
	default:
		res.Status = trace.VerifyFail
	}
	logf("%s: %s (exit %d, %s)", id, res.Status, out.ExitCode, out.Duration.Round(time.Millisecond))
	return res, out.Stdout, out.Stderr, nil
}
