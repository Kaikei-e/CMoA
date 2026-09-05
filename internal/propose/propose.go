// Package propose is the router: it takes a harness snapshot, sends every
// configured proposer the same prompt at the same time, and records what
// came back as candidates. It never retries and never judges; a bad answer
// is a candidate with a status, so the layer above can mine it.
package propose

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/harness"
	"github.com/Kaikei-e/CMoA/internal/harnessdir"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/patch"
	"github.com/Kaikei-e/CMoA/internal/prompt"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// ErrContextBudget is returned when the task and the rendered harness
// together exceed the task's max_context_bytes. Nothing is written: the run
// would have sent a prompt the task refused to allow.
var ErrContextBudget = errors.New("propose: context budget exceeded")

// Options tune a run.
type Options struct {
	AsOf        string          // YYYY-MM-DD; empty means today
	RunID       trace.RunID     // empty means generate
	Client      *llm.Client     // nil means a default client
	Version     string          // cmoa version string for run.json
	Harness     *harnessdir.Dir // nil means no harness directory
	Seed        *int64          // overrides every proposer's seed
	Temperature *float64        // overrides every proposer's temperature
	Log         func(format string, args ...any)
	Now         func() time.Time
}

// Run performs propose for t and returns the run directory. The harness
// snapshot is taken first; if it fails, nothing is written. Proposer
// failures are recorded, not returned.
func Run(ctx context.Context, cfg *config.Config, t *task.Task, opt Options) (trace.Dir, error) {
	logf := opt.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	client := opt.Client
	if client == nil {
		client = &llm.Client{HTTP: &http.Client{}}
	}

	// The overrides are applied before the effective config is recorded, so
	// run.json says what was sent, not what the file said.
	for i := range cfg.Proposers {
		if opt.Seed != nil {
			seed := *opt.Seed
			cfg.Proposers[i].Seed = &seed
		}
		if opt.Temperature != nil {
			temp := *opt.Temperature
			cfg.Proposers[i].Temperature = &temp
		}
	}

	rev, err := t.ResolveRev(ctx)
	if err != nil {
		return "", err
	}
	snap, err := harness.Take(ctx, cfg.Harness.Vault, cfg.Harness.Docdag, opt.AsOf)
	if err != nil {
		return "", fmt.Errorf("propose: harness snapshot: %w", err)
	}
	logf("harness: %s at %s as of %s (%d binding)", snap.Vault, snap.At, snap.AsOf, len(snap.Binding))

	var rendered prompt.Harness
	if opt.Harness != nil {
		rendered = opt.Harness.Harness
		logf("harness dir: %s (%d files, tree %s, %d notes, %d skills, %d bytes)", opt.Harness.Path,
			len(opt.Harness.Files), opt.Harness.TreeSHA256[:12], len(rendered.Notes), len(rendered.Skills), rendered.Bytes())
		// A Notes section is as much of the model's context as a file is,
		// and memory and skills are the auto-accepted surfaces: an unbounded
		// harness would silently overrun the server's context and be scored
		// as a harness regression when it is a budget bug.
		if taskBytes, harnessBytes := t.ContextBytes(), rendered.Bytes(); taskBytes+harnessBytes > t.MaxContextBytes {
			return "", fmt.Errorf("%w: instruction and files %d bytes plus harness %d bytes total %d, over max_context_bytes %d",
				ErrContextBudget, taskBytes, harnessBytes, taskBytes+harnessBytes, t.MaxContextBytes)
		}
	}
	messages, err := prompt.Build(t, rendered)
	if err != nil {
		return "", err
	}

	id := opt.RunID
	if id == "" {
		id = trace.NewRunID(now())
	}
	dir, err := trace.Create(t.Dir, id)
	if err != nil {
		return "", err
	}
	effective, err := cfg.Redacted()
	if err != nil {
		return "", err
	}
	n, f := cfg.ByzantineTolerance()
	run := &trace.Run{
		SchemaVersion: trace.SchemaVersion,
		RunID:         id,
		CreatedAt:     now().UTC(),
		CMoAVersion:   opt.Version,
		PromptVersion: prompt.Version(),
		Task: trace.TaskRef{
			ID: string(t.ID), Dir: t.Dir, Repo: t.Repo, Rev: t.Rev, ResolvedRev: rev,
			Files: t.FilePaths(), InstructionSHA256: t.InstructionSHA256(),
		},
		Config:    effective,
		Harness:   harnessRecord(snap, opt.Harness),
		Byzantine: trace.Byzantine{N: n, F: f},
	}
	for _, p := range cfg.Proposers {
		run.Proposers = append(run.Proposers, trace.ProposerRef{ID: string(p.ID), Model: p.Model, BaseURL: p.BaseURL})
	}
	if err := dir.WriteRun(run); err != nil {
		return "", err
	}
	logf("run %s: %d proposers (byzantine f=%d)", id, n, f)

	var wg sync.WaitGroup
	errs := make([]error, len(cfg.Proposers))
	for i := range cfg.Proposers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = ask(ctx, client, &cfg.Proposers[i], messages, dir, now, logf)
		}(i)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return dir, fmt.Errorf("propose: writing candidates: %w", err)
	}
	return dir, nil
}

func harnessRecord(s *harness.Snapshot, dir *harnessdir.Dir) trace.Harness {
	h := trace.Harness{Vault: s.Vault, AsOf: s.AsOf, At: s.At, DocdagVersion: s.DocdagVersion, Binding: []trace.HarnessDoc{}}
	for _, d := range s.Binding {
		h.Binding = append(h.Binding, trace.HarnessDoc{ID: d.ID, Title: d.Title, Status: d.Status, Path: d.Path})
	}
	if dir != nil {
		r := &trace.HarnessRender{
			Dir: dir.Path, TreeSHA256: dir.TreeSHA256,
			RenderedBytes: dir.Harness.Bytes(), Files: []trace.HarnessFile{},
		}
		for _, f := range dir.Files {
			r.Files = append(r.Files, trace.HarnessFile{Path: f.Path, SHA256: f.SHA256})
		}
		h.Render = r
	}
	return h
}

// ask sends one request and writes prompt/<id>.json and candidates/<id>.*.
// Only trace write failures are returned.
func ask(ctx context.Context, client *llm.Client, p *config.Proposer, messages []llm.Message, dir trace.Dir, now func() time.Time, logf func(string, ...any)) error {
	key, err := p.APIKey()
	if err != nil {
		return err
	}
	req := llm.Request{
		BaseURL: p.BaseURL, APIKey: key, Model: p.Model, Messages: messages,
		Temperature: *p.Temperature, MaxTokens: p.MaxTokens, Seed: p.Seed, ExtraBody: p.ExtraBody,
	}
	cand := &trace.Candidate{ProposerID: string(p.ID), Model: p.Model, StartedAt: now().UTC()}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSeconds)*time.Second)
	defer cancel()
	resp, callErr := client.ChatCompletion(reqCtx, req)
	cand.FinishedAt = now().UTC()
	cand.Timings.RequestMS = cand.FinishedAt.Sub(cand.StartedAt).Milliseconds()

	var raw []byte
	var diff string
	if callErr == nil {
		raw = []byte(resp.RawContent)
		cand.FinishReason = resp.FinishReason
		cand.Usage = trace.Usage{PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens}
		cand.Timings.ServerPromptMS = resp.Timings.PromptMS
		cand.Timings.ServerPredictedMS = resp.Timings.PredictedMS
		cand.Timings.TokensPerSecond = resp.Timings.PredictedPerSecond
		cand.RequestSHA256 = llm.SHA256(resp.RequestBody)
		cand.ResponseSHA256 = llm.SHA256(resp.ResponseBody)
		if err := dir.WritePrompt(&trace.Prompt{ProposerID: string(p.ID), Messages: toTraceMessages(messages), Request: resp.RequestBody, SHA256: cand.RequestSHA256}); err != nil {
			return err
		}
		diff = classify(cand, resp.Content)
	} else {
		cand.Error = callErr.Error()
		var he *llm.HTTPError
		var de *llm.DecodeError
		switch {
		case errors.Is(callErr, context.DeadlineExceeded) && ctx.Err() == nil:
			cand.Status = trace.CandidateTimeout
		case errors.As(callErr, &he):
			cand.Status = trace.CandidateHTTPError
			raw = he.Body
		case errors.As(callErr, &de):
			cand.Status = trace.CandidateMalformed
			raw = de.Body
		default:
			cand.Status = trace.CandidateHTTPError
		}
		if err := dir.WritePrompt(&trace.Prompt{ProposerID: string(p.ID), Messages: toTraceMessages(messages)}); err != nil {
			return err
		}
	}
	logf("%s: %s (%s, %d tokens)", p.ID, cand.Status, time.Duration(cand.Timings.RequestMS)*time.Millisecond, cand.Usage.CompletionTokens)
	return dir.WriteCandidate(cand, raw, diff)
}

// classify sets the candidate's status from the completion text and returns
// the extracted diff (empty when there is none).
func classify(cand *trace.Candidate, content string) string {
	d, err := patch.Extract(content)
	if err != nil {
		cand.Status = trace.CandidateNoDiff
		cand.Error = err.Error()
		return ""
	}
	st, err := patch.ComputeStats(d)
	if err != nil {
		cand.Status = trace.CandidateNoDiff
		cand.Error = err.Error()
		return ""
	}
	cand.Status = trace.CandidateOK
	cand.Diff = &trace.DiffStats{Files: st.Files, Additions: st.Additions, Deletions: st.Deletions, SHA256: st.SHA256}
	return d
}

func toTraceMessages(ms []llm.Message) []trace.Message {
	out := make([]trace.Message, len(ms))
	for i, m := range ms {
		out[i] = trace.Message{Role: m.Role, Content: m.Content}
	}
	return out
}
