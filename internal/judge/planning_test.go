package judge

import (
	"strings"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

func TestPlanPairsPreservesCanonicalOrder(t *testing.T) {
	rep := &trace.JudgeReport{Pairs: []trace.JudgePair{}}
	candidates := []Candidate{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	calls := planPairs(rep, candidates)

	wantPairs := [][]string{{"a", "b"}, {"a", "c"}, {"b", "c"}}
	if len(rep.Pairs) != len(wantPairs) {
		t.Fatalf("want %d pairs, got %d", len(wantPairs), len(rep.Pairs))
	}
	for i, want := range wantPairs {
		if got := rep.Pairs[i].Pair; strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("pair %d: want %v, got %v", i, want, got)
		}
	}
	wantCalls := []pairCall{
		{pair: 0, order: "ab", first: 0, second: 1}, {pair: 0, order: "ba", first: 1, second: 0},
		{pair: 1, order: "ab", first: 0, second: 2}, {pair: 1, order: "ba", first: 2, second: 0},
		{pair: 2, order: "ab", first: 1, second: 2}, {pair: 2, order: "ba", first: 2, second: 1},
	}
	if len(calls) != len(wantCalls) {
		t.Fatalf("want %d calls, got %d", len(wantCalls), len(calls))
	}
	for i, want := range wantCalls {
		if calls[i] != want {
			t.Errorf("call %d: want %+v, got %+v", i, want, calls[i])
		}
	}
}

// The whole protocol, end to end against a scripted judge: six calls, three
// pairs, and a candidate that wins both orders of both its pairs.
