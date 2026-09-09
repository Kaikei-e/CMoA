package judge

import (
	"fmt"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

func TestAggregate(t *testing.T) {
	ok := func(choice string) trace.JudgeOrder {
		return trace.JudgeOrder{Status: trace.JudgeCallOK, Choice: choice}
	}
	pair := func(a, b string, first, second trace.JudgeOrder) trace.JudgePair {
		first.First, first.Second = a, b
		second.First, second.Second = b, a
		first.ChoiceCandidate = candidateOf(first.Choice, a, b)
		second.ChoiceCandidate = candidateOf(second.Choice, b, a)
		return trace.JudgePair{Pair: []string{a, b}, Orders: []trace.JudgeOrder{first, second}, Verdict: trace.VerdictDraw}
	}
	for _, tc := range []struct {
		name   string
		pairs  []trace.JudgePair
		norm   map[string]string // normalised answers; nil means the chain has none
		kind   trace.SelectionKind
		id     string
		reason string
		key    trace.TieBreakKey // the tie-break key, empty when none was needed
		scores map[string]float64
		swap   int
		draws  map[trace.DrawReason]int
	}{
		{
			name: "both orders agree on every pair",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("a", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("b", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
			},
			kind: trace.SelectionSelected, id: "a", swap: 3,
		},
		{
			// Position bias is half a win each, not a null: the pair the
			// swap did not survive leaves a and b level, and the score
			// decides instead of refusing.
			name: "one pair disagrees under swap and scores a half to each",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceA), ok(trace.ChoiceA)),
				pair("a", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("b", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
			},
			norm: map[string]string{"a": "alpha", "b": "beta", "c": "gamma"},
			kind: trace.SelectionSelected, id: "a", key: trace.TieBreakHash, swap: 2,
			scores: map[string]float64{"a": 1.5, "b": 1.5, "c": 0},
			reason: "copeland tie, score 1.5 of 2, tie broken by hash among [a b]",
		},
		{
			// The same top set, with answers: neither earns centrality
			// credit, so the shorter one wins.
			name: "a top set is parted by the shortest answer",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceA), ok(trace.ChoiceA)),
				pair("a", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("b", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
			},
			norm: map[string]string{"a": "the answer is 101", "b": "101", "c": "no idea"},
			kind: trace.SelectionSelected, id: "b", key: trace.TieBreakLength,
			reason: "copeland tie, score 1.5 of 2, tie broken by length among [a b]",
		},
		{
			// c is not in the top set; a agrees with it, so a is the
			// more central of the two that are tied.
			name: "a top set is parted by agreement with the rest of the run",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceA), ok(trace.ChoiceA)),
				pair("a", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("b", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
			},
			norm: map[string]string{"a": "101.0", "b": "42", "c": "101"},
			kind: trace.SelectionSelected, id: "a", key: trace.TieBreakConsensus,
		},
		{
			name: "a tie in one order is a draw for the pair",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceTie), ok(trace.ChoiceB)),
				pair("a", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("b", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
			},
			norm: map[string]string{"a": "alpha", "b": "beta", "c": "gamma"},
			kind: trace.SelectionSelected, id: "a", key: trace.TieBreakHash, swap: 2,
			scores: map[string]float64{"a": 1.5, "b": 1.5, "c": 0},
		},
		{
			// A cycle is a property of the judge, not an error: every
			// candidate scores one win and one loss, and the chain decides
			// — here past a length gate that refuses to separate "one"
			// from "two", and on the digest of the answers themselves.
			name: "the wins run in a circle",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("b", "c", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("a", "c", ok(trace.ChoiceB), ok(trace.ChoiceA)),
			},
			norm: map[string]string{"a": "one", "b": "two", "c": "three"},
			kind: trace.SelectionSelected, id: "b", key: trace.TieBreakHash, swap: 3,
			scores: map[string]float64{"a": 1, "b": 1, "c": 1},
		},
		{
			// Three candidates the judge called equal are three candidates
			// worth half a win each: the answer is one of them, not none.
			name: "nothing was decided at all",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceTie), ok(trace.ChoiceTie)),
				pair("a", "c", ok(trace.ChoiceTie), ok(trace.ChoiceTie)),
				pair("b", "c", ok(trace.ChoiceTie), ok(trace.ChoiceTie)),
			},
			norm: map[string]string{"a": "a longer answer", "b": "short", "c": "a third answer"},
			kind: trace.SelectionSelected, id: "b", key: trace.TieBreakLength, swap: 3,
			scores: map[string]float64{"a": 1, "b": 1, "c": 1},
			reason: "copeland tie, score 1 of 2, tie broken by length among [a b c]",
		},
		{
			name: "an unparsable answer is its own reason",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceA), trace.JudgeOrder{Status: trace.JudgeCallInvalidOutput}),
				pair("a", "c", ok(trace.ChoiceTie), ok(trace.ChoiceTie)),
				pair("b", "c", ok(trace.ChoiceTie), ok(trace.ChoiceTie)),
			},
			kind: trace.SelectionNoCandidate, reason: string(trace.ReasonInvalidOutput), swap: 2,
		},
		{
			name: "a timeout is the judge's failure, not the candidates'",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceA), trace.JudgeOrder{Status: trace.JudgeCallTimeout}),
			},
			kind: trace.SelectionJudgeTimeout,
		},
		{
			name: "an HTTP error is the judge's failure too",
			pairs: []trace.JudgePair{
				pair("a", "b", ok(trace.ChoiceA), trace.JudgeOrder{Status: trace.JudgeCallError, Error: "HTTP 500"}),
			},
			kind: trace.SelectionJudgeFailed, reason: "HTTP 500",
		},
		{
			name:  "two candidates, one pair, a draw is half a win each",
			pairs: []trace.JudgePair{pair("a", "b", ok(trace.ChoiceA), ok(trace.ChoiceA))},
			norm:  map[string]string{"a": "alpha", "b": "beta"},
			kind:  trace.SelectionSelected, id: "a", key: trace.TieBreakHash,
			scores: map[string]float64{"a": 0.5, "b": 0.5},
		},
		{
			name:  "two candidates, one pair, both orders agree",
			pairs: []trace.JudgePair{pair("a", "b", ok(trace.ChoiceA), ok(trace.ChoiceB))},
			kind:  trace.SelectionSelected, id: "a", swap: 1,
		},
		{
			name: "a pair between two losers times out, and the winner stands",
			pairs: []trace.JudgePair{
				pair("x", "y", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("x", "z", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("y", "z", trace.JudgeOrder{Status: trace.JudgeCallTimeout}, trace.JudgeOrder{Status: trace.JudgeCallTimeout}),
			},
			kind: trace.SelectionSelected, id: "x", swap: 2,
			draws: map[trace.DrawReason]int{trace.DrawUnmeasured: 1},
		},
		{
			name: "an HTTP error between two losers does not discard the winner either",
			pairs: []trace.JudgePair{
				pair("x", "y", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("x", "z", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("y", "z", ok(trace.ChoiceA), trace.JudgeOrder{Status: trace.JudgeCallError, Error: "HTTP 500"}),
			},
			kind: trace.SelectionSelected, id: "x", swap: 2,
			draws: map[trace.DrawReason]int{trace.DrawUnmeasured: 1},
		},
		{
			name: "a timeout in a pair the winner is in escalates",
			pairs: []trace.JudgePair{
				pair("x", "y", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("x", "z", trace.JudgeOrder{Status: trace.JudgeCallTimeout}, ok(trace.ChoiceB)),
				pair("y", "z", ok(trace.ChoiceA), ok(trace.ChoiceB)),
			},
			kind:  trace.SelectionJudgeTimeout,
			draws: map[trace.DrawReason]int{trace.DrawUnmeasured: 1},
		},
		{
			// x leads on two halves, but the pair nobody measured is worth
			// a whole point to y or z — enough to overtake it. Under the
			// score the question is not "could someone still sweep" but
			// "could the answer still change", and here it could.
			name: "an unmeasured pair that could still overtake the leader escalates",
			pairs: []trace.JudgePair{
				pair("x", "y", ok(trace.ChoiceTie), ok(trace.ChoiceTie)),
				pair("x", "z", ok(trace.ChoiceTie), ok(trace.ChoiceTie)),
				pair("y", "z", trace.JudgeOrder{Status: trace.JudgeCallTimeout}, trace.JudgeOrder{Status: trace.JudgeCallTimeout}),
			},
			kind: trace.SelectionJudgeTimeout, swap: 2,
			scores: map[string]float64{"x": 1, "y": 0.5, "z": 0.5},
			draws:  map[trace.DrawReason]int{trace.DrawTie: 2, trace.DrawUnmeasured: 1},
		},
		{
			// The same shape, with the leader far enough ahead that the
			// missing point cannot reach it: the answer stands.
			name: "an unmeasured pair that cannot reach the leader is only a draw",
			pairs: []trace.JudgePair{
				pair("w", "x", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("w", "y", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("w", "z", ok(trace.ChoiceTie), ok(trace.ChoiceTie)),
				pair("x", "y", ok(trace.ChoiceTie), ok(trace.ChoiceTie)),
				pair("x", "z", ok(trace.ChoiceA), ok(trace.ChoiceB)),
				pair("y", "z", trace.JudgeOrder{Status: trace.JudgeCallTimeout}, trace.JudgeOrder{Status: trace.JudgeCallTimeout}),
			},
			kind: trace.SelectionSelected, id: "w",
			scores: map[string]float64{"w": 2.5, "x": 1.5, "y": 0.5, "z": 0.5},
			reason: "copeland winner, score 2.5 of 3 (no condorcet winner)",
			draws:  map[trace.DrawReason]int{trace.DrawTie: 2, trace.DrawUnmeasured: 1},
		},
		{
			name:  "two candidates, the only pair times out",
			pairs: []trace.JudgePair{pair("x", "y", trace.JudgeOrder{Status: trace.JudgeCallTimeout}, ok(trace.ChoiceB))},
			kind:  trace.SelectionJudgeTimeout,
			draws: map[trace.DrawReason]int{trace.DrawUnmeasured: 1},
		},
		{
			name: "a timeout outranks an error when both could still decide",
			pairs: []trace.JudgePair{
				pair("x", "y", trace.JudgeOrder{Status: trace.JudgeCallError, Error: "HTTP 500"}, ok(trace.ChoiceB)),
				pair("x", "z", trace.JudgeOrder{Status: trace.JudgeCallTimeout}, ok(trace.ChoiceB)),
				pair("y", "z", ok(trace.ChoiceA), ok(trace.ChoiceB)),
			},
			kind:  trace.SelectionJudgeTimeout,
			draws: map[trace.DrawReason]int{trace.DrawUnmeasured: 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep := &trace.JudgeReport{Candidates: candidateIDs(tc.pairs), Wins: map[string]int{}, Pairs: tc.pairs}
			for _, id := range rep.Candidates {
				rep.Wins[id] = 0
			}
			Aggregate(rep, texts(tc.norm))
			if rep.Outcome.Kind != tc.kind {
				t.Fatalf("kind %q, want %q (%s)", rep.Outcome.Kind, tc.kind, rep.Outcome.Reason)
			}
			if rep.Outcome.CandidateID != tc.id {
				t.Errorf("candidate %q, want %q", rep.Outcome.CandidateID, tc.id)
			}
			if tc.reason != "" && rep.Outcome.Reason != tc.reason {
				t.Errorf("reason %q, want %q", rep.Outcome.Reason, tc.reason)
			}
			if tc.swap != 0 && rep.SwapConsistentPairs != tc.swap {
				t.Errorf("swap consistent %d, want %d", rep.SwapConsistentPairs, tc.swap)
			}
			if tc.draws != nil && fmt.Sprint(rep.DrawReasons) != fmt.Sprint(tc.draws) {
				t.Errorf("draw reasons %v, want %v", rep.DrawReasons, tc.draws)
			}
			if tc.scores != nil && fmt.Sprint(rep.Scores) != fmt.Sprint(tc.scores) {
				t.Errorf("scores %v, want %v", rep.Scores, tc.scores)
			}
			// A tie-break is recorded exactly when one was needed, and it
			// names the candidate the outcome selected.
			switch {
			case tc.key == "" && rep.TieBreak != nil:
				t.Errorf("unwanted tie break %+v", rep.TieBreak)
			case tc.key != "" && rep.TieBreak == nil:
				t.Errorf("no tie break recorded, want key %q", tc.key)
			case tc.key != "":
				if rep.TieBreak.Key != tc.key || rep.TieBreak.Chosen != tc.id {
					t.Errorf("tie break %+v, want key %q chosen %q", rep.TieBreak, tc.key, tc.id)
				}
			}
			// The same input twice is the same choice: nothing in the chain
			// reads a map's iteration order or a clock.
			again := &trace.JudgeReport{Candidates: candidateIDs(tc.pairs), Wins: map[string]int{}, Pairs: tc.pairs}
			Aggregate(again, texts(tc.norm))
			if fmt.Sprint(again.Outcome) != fmt.Sprint(rep.Outcome) || fmt.Sprint(again.Ranked) != fmt.Sprint(rep.Ranked) {
				t.Errorf("not deterministic: %v %v then %v %v", rep.Outcome, rep.Ranked, again.Outcome, again.Ranked)
			}
			// Every draw names why, and every decided pair names nothing.
			for _, p := range rep.Pairs {
				if (p.Verdict == trace.VerdictDraw) != (p.DrawReason != "") {
					t.Errorf("pair %v: verdict %q with draw_reason %q", p.Pair, p.Verdict, p.DrawReason)
				}
			}
		})
	}
}

func candidateIDs(pairs []trace.JudgePair) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range pairs {
		for _, id := range p.Pair {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// A single answer is not a selection, and neither is none.
