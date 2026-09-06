package judge

import (
	"context"
	"sync"
	"time"

	"github.com/Kaikei-e/CMoA/internal/prompt"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

type pairCall struct {
	pair          int
	order         string
	first, second int // indices into candidates
}

// planPairs adds the round-robin pairs in the caller's order. Both orders
// are scheduled, so no shuffle is needed; the seeded nonce is the rerun
// perturbation.
func planPairs(rep *trace.JudgeReport, candidates []Candidate) []pairCall {
	calls := make([]pairCall, 0, len(candidates)*(len(candidates)-1))
	for i := range candidates {
		for k := i + 1; k < len(candidates); k++ {
			pair := len(rep.Pairs)
			rep.Pairs = append(rep.Pairs, trace.JudgePair{
				Pair:    []string{candidates[i].ID, candidates[k].ID},
				Orders:  []trace.JudgeOrder{{}, {}},
				Verdict: trace.VerdictDraw,
			})
			calls = append(calls,
				pairCall{pair: pair, order: "ab", first: i, second: k},
				pairCall{pair: pair, order: "ba", first: k, second: i},
			)
		}
	}
	return calls
}

// runPairs executes the plan and serialises shared report and trace writes
// after each independent completion returns.
func (j *Judge) runPairs(ctx context.Context, in Input, rep *trace.JudgeReport, texts []string, nonce string, now func() time.Time, logf func(string, ...any)) error {
	calls := planPairs(rep, in.Candidates)
	base := prompt.JudgeInput{
		Conversation: in.Conversation, Reference: in.Reference, Rubric: in.Rubric,
		Nonce: nonce, AllowTie: in.AllowTie,
	}
	var mu sync.Mutex
	var writeErr error
	sem := make(chan struct{}, j.Cfg.Parallel)
	var wg sync.WaitGroup
	for _, call := range calls {
		wg.Add(1)
		go func(call pairCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			input := base
			input.Candidates = []prompt.JudgeCandidate{
				{Label: trace.ChoiceA, Text: texts[call.first]},
				{Label: trace.ChoiceB, Text: texts[call.second]},
			}
			rec, order := j.ask(ctx, in, input, call.pair, call.order, in.Candidates[call.first].ID, in.Candidates[call.second].ID, now)
			logf("judge pair %d %s: %s %s (%s)", call.pair, call.order, order.Status, order.ChoiceCandidate,
				time.Duration(order.LatencyMS)*time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			orderIndex := 0
			if call.order == "ba" {
				orderIndex = 1
			}
			rep.Pairs[call.pair].Orders[orderIndex] = order
			for _, attempt := range rec.Attempts {
				rep.Usage.PromptTokens += attempt.Usage.PromptTokens
				rep.Usage.CompletionTokens += attempt.Usage.CompletionTokens
			}
			rep.InvalidOutputRetries += order.Retries
			if err := j.Dir.WriteJudgeCall(rec); err != nil && writeErr == nil {
				writeErr = err
			}
		}(call)
	}
	wg.Wait()
	return writeErr
}
