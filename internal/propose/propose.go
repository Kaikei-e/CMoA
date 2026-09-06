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
	"errors"
	"time"

	"github.com/Kaikei-e/CMoA/internal/harnessdir"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// ErrContextBudget is returned when the task and the rendered harness
// together exceed the task's max_context_bytes. Nothing is written: the run
// would have sent a prompt the task refused to allow.
var ErrContextBudget = errors.New("propose: context budget exceeded")

// ErrNoJudge is returned when a chat task is proposed against a
// configuration that has no judge. Nothing is written: the answers would
// have nowhere to be selected, and a run that cannot be selected is a run
// that spent the fleet for nothing.
var ErrNoJudge = errors.New("propose: the chat face needs a judge: cmoa.json declares none (version 2 adds the judge block)")

// Options tune a run.
type Options struct {
	AsOf        string          // YYYY-MM-DD; empty means today
	RunID       trace.RunID     // empty means generate
	Client      *llm.Client     // nil means a default client
	Version     string          // cmoa version string for run.json
	Harness     *harnessdir.Dir // nil means no harness directory
	Seed        *int64          // overrides every proposer's seed
	Temperature *float64        // overrides every proposer's temperature
	// Replayed marks a chat run whose candidates and judge answers come
	// from a run already made rather than from anything asked today. It is
	// External's alone; a proposer pool cannot be replayed.
	Replayed *trace.Replayed
	Log      func(format string, args ...any)
	Now      func() time.Time
}
