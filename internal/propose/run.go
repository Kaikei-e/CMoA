// Package propose is the router: it takes a harness snapshot, sends every
// configured proposer the same prompt at the same time, and records what
// came back as candidates. It never retries and never judges; a bad answer
// is a candidate with a status, so the layer above can mine it.
//
// Both faces go through the same router. The coding face sends the
// instruction and the files and keeps the unified diff it finds; the chat
// face sends the task's conversation and keeps the answer, with the style
// metadata a later calibration needs recorded at the time it is written.
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
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/prompt"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// Run performs propose for t and returns the run directory. The harness
// snapshot is taken first; if it fails, nothing is written. Proposer
// failures are recorded, not returned.
func Run(ctx context.Context, cfg *config.Config, t *task.Task, opt Options) (trace.Dir, error) {
	logf, now, client := runDependencies(opt)
	if t.Face == task.FaceChat && cfg.Judge == nil {
		return "", ErrNoJudge
	}
	applyOverrides(cfg, opt)

	rev, err := resolveRevision(ctx, t)
	if err != nil {
		return "", err
	}
	snap, err := harness.Take(ctx, cfg.Harness.Vault, cfg.Harness.Docdag, opt.AsOf)
	if err != nil {
		return "", fmt.Errorf("propose: harness snapshot: %w", err)
	}
	logf("harness: %s at %s as of %s (%d binding)", snap.Vault, snap.At, snap.AsOf, len(snap.Binding))

	messages, err := proposerMessages(t, opt, logf)
	if err != nil {
		return "", err
	}
	dir, run, err := createRun(cfg, t, opt, rev, snap, now)
	if err != nil {
		return "", err
	}
	logf("run %s: %d proposers (byzantine f=%d)", dir.ID(), run.Byzantine.N, run.Byzantine.F)
	if err := requestCandidates(ctx, client, cfg.Proposers, t.Face, messages, dir, now, logf); err != nil {
		return dir, fmt.Errorf("propose: writing candidates: %w", err)
	}
	return dir, nil
}

func runDependencies(opt Options) (func(string, ...any), func() time.Time, *llm.Client) {
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
	return logf, now, client
}

func applyOverrides(cfg *config.Config, opt Options) {
	// Apply overrides before recording the effective config so run.json says
	// what was sent, not what the file said.
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
}

func resolveRevision(ctx context.Context, t *task.Task) (string, error) {
	if t.Face != task.FaceCoding {
		return "", nil
	}
	return t.ResolveRev(ctx)
}

func proposerMessages(t *task.Task, opt Options, logf func(string, ...any)) ([]llm.Message, error) {
	var rendered prompt.Harness
	if opt.Harness != nil {
		rendered = opt.Harness.Harness
		logf("harness dir: %s (%d files, tree %s, %d notes, %d skills, %d bytes)", opt.Harness.Path,
			len(opt.Harness.Files), opt.Harness.TreeSHA256[:12], len(rendered.Notes), len(rendered.Skills), rendered.Bytes())
		if err := checkContextBudget(t, rendered); err != nil {
			return nil, err
		}
	}
	switch t.Face {
	case task.FaceCoding:
		return prompt.Build(t, rendered)
	case task.FaceChat:
		return prompt.BuildChat(t, rendered)
	}
	return nil, nil
}

func checkContextBudget(t *task.Task, rendered prompt.Harness) error {
	// A Notes section is as much of the model's context as a file is, and
	// memory and skills are auto-accepted surfaces. An unbounded harness
	// would silently overrun the server's context.
	what := "instruction and files"
	if t.Face == task.FaceChat {
		what = "conversation"
	}
	taskBytes, harnessBytes := t.ContextBytes(), rendered.Bytes()
	if taskBytes+harnessBytes <= t.MaxContextBytes {
		return nil
	}
	return fmt.Errorf("%w: %s %d bytes plus harness %d bytes total %d, over max_context_bytes %d",
		ErrContextBudget, what, taskBytes, harnessBytes, taskBytes+harnessBytes, t.MaxContextBytes)
}

func requestCandidates(ctx context.Context, client *llm.Client, proposers []config.Proposer, face task.Face, messages []llm.Message, dir trace.Dir, now func() time.Time, logf func(string, ...any)) error {
	var wg sync.WaitGroup
	errs := make([]error, len(proposers))
	for i := range proposers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = ask(ctx, client, &proposers[i], face, messages, dir, now, logf)
		}(i)
	}
	wg.Wait()
	return errors.Join(errs...)
}
