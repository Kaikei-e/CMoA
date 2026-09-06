package judge

import (
	"strings"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

// The normaliser is the whole of stage 1: two answers agree exactly when it
// folds them to the same text, so every case here is a claim about what
// counts as the same answer.
func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"full-width digits become ascii", "１０１", "101"},
		{"full-width punctuation too", "答え：１０１！", "答え:101"},
		{"the ideographic space is a space", "答え　101", "答え 101"},
		{"emphasis markers are removed", "**101** and `101`", "101 and 101"},
		{"underscores go with them", "_the_ answer", "the answer"},
		{"a bullet at the start of a line goes", "- 101\n- 102", "101 102"},
		{"a numbered marker goes too", "1. alpha\n2. beta", "alpha beta"},
		{"and a parenthesised one", "1) alpha\n2) beta", "alpha beta"},
		{"a bare number is not a list marker", "101.", "101"},
		{"nor is a decimal point", "1.101", "1.101"},
		{"a hyphen with no space is a sign", "-5", "-5"},
		{"whitespace collapses", "a  \n\t b", "a b"},
		{"trailing punctuation goes", "101です。", "101です"},
		{"however much of it there is", "101!?。", "101"},
		{"case is folded", "The Answer", "the answer"},
		{"separators are left in the text", "276,416", "276,416"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Normalize(tc.in); got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The last number is the answer, and two spellings of one quantity are one
// number.
func TestLastNumber(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"1 + 100 = 101", "101", true},
		{"answer: 276,416", "276416", true},
		{"答えは 276、416 です", "276416", true},
		{"3.50", "3.5", true},
		{"007", "7", true},
		{"the balance is -99", "-99", true},
		{"utf-8 is a name, 3 is a number", "3", true},
		{"no digits here", "", false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := lastNumber(Normalize(tc.in))
			if ok != tc.ok || got != tc.want {
				t.Errorf("lastNumber(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// Agreement is conservative in both directions: it must find the agreement
// two proposers really reached, and it must not invent one.
func TestAgree(t *testing.T) {
	long := strings.Repeat("あ", 170) + " 101"
	for _, tc := range []struct {
		name, a, b string
		how        trace.ConsensusAgreement
		want       bool
	}{
		{name: "the same text", a: "101です。", b: "**１０１です**", how: trace.AgreementExact, want: true},
		{name: "the same number, different words", a: "101です。", b: "1 + 100 = 101", how: trace.AgreementNumeric, want: true},
		{name: "separators do not matter", a: "答え: 276,416", b: "276416", how: trace.AgreementNumeric, want: true},
		{name: "different numbers", a: "101", b: "102"},
		{name: "no number at all", a: "青いから", b: "赤いから"},
		{name: "an enumeration is not a number", a: "1. あ\n2. い", b: "1. う\n2. え"},
		{name: "prose that happens to end in the same figure", a: long, b: "101"},
		{name: "two empty answers agree on nothing", a: "", b: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			how, ok := agree(Normalize(tc.a), Normalize(tc.b))
			if ok != tc.want || (ok && how != tc.how) {
				t.Errorf("agree(%q, %q) = %q, %v; want %q, %v", tc.a, tc.b, how, ok, tc.how, tc.want)
			}
		})
	}
}

// Two of three agreeing is a consensus, and a consensus costs no judge
// call: the fake judge here fails the test if it is ever asked.
func TestConsensusSkipsTheJudge(t *testing.T) {
	f := &fakeJudge{t: t, script: map[order]string{}}
	j, dir := fixture(t, f)
	rep, err := j.Run(t.Context(), answers(
		"granite", "1 + 100 = **101**",
		"qwen", "１０１です。",
		"gemma", "たぶん 55 でしょう",
	))
	if err != nil {
		t.Fatal(err)
	}
	if n := f.calls.Load(); n != 0 {
		t.Errorf("a consensus spent %d judge calls", n)
	}
	if rep.Outcome.Kind != trace.SelectionSelected || rep.Outcome.CandidateID != "qwen" {
		t.Fatalf("outcome %+v", rep.Outcome)
	}
	if want := "consensus: 2 of 3 agree on the normalised answer (numeric)"; rep.Outcome.Reason != want {
		t.Errorf("reason %q, want %q", rep.Outcome.Reason, want)
	}
	c := rep.Consensus
	if c == nil || c.Normalisation != Normalisation || c.Chosen != "qwen" || c.Agreement != trace.AgreementNumeric {
		t.Fatalf("consensus %+v", c)
	}
	if got := groupsString(c.Groups); got != "granite,qwen|gemma" {
		t.Errorf("groups %v", c.Groups)
	}
	// The shorter of the two that agreed is the one returned, and the
	// tie-break says so.
	if rep.TieBreak == nil || rep.TieBreak.Key != trace.TieBreakLength || rep.TieBreak.Chosen != "qwen" {
		t.Fatalf("tie break %+v", rep.TieBreak)
	}
	// Nothing was asked, so nothing was spent and no pair exists.
	if len(rep.Pairs) != 0 || rep.Usage.PromptTokens != 0 || rep.Wins["qwen"] != 0 || rep.Scores["qwen"] != 0 {
		t.Errorf("report %+v", rep)
	}
	// The whole finding is on disk, judged once.
	got, err := dir.ReadJudge()
	if err != nil {
		t.Fatal(err)
	}
	if got.Consensus == nil || got.Consensus.Chosen != "qwen" {
		t.Errorf("judge.json %+v", got.Consensus)
	}
}

// Three answers that do not agree are three answers for the judge: stage 1
// is a short cut, never a substitute.
func TestNoConsensusRunsTheJudge(t *testing.T) {
	f := &fakeJudge{t: t, script: map[order]string{
		{"a", "b"}: trace.ChoiceA, {"b", "a"}: trace.ChoiceB,
		{"a", "c"}: trace.ChoiceA, {"c", "a"}: trace.ChoiceB,
		{"b", "c"}: trace.ChoiceA, {"c", "b"}: trace.ChoiceB,
	}}
	j, _ := fixture(t, f)
	// The fixtures write the candidate id on the first line, which is how
	// the fake scripts its answers; the second line is what differs.
	rep, err := j.Run(t.Context(), answers("a", "a\nthe answer is 101", "b", "b\nthe answer is 102", "c", "c\nthe answer is 103"))
	if err != nil {
		t.Fatal(err)
	}
	if n := f.calls.Load(); n != 6 {
		t.Errorf("want 6 calls, got %d", n)
	}
	if rep.Consensus != nil || rep.Outcome.CandidateID != "a" {
		t.Errorf("consensus %+v outcome %+v", rep.Consensus, rep.Outcome)
	}
}

// The chain is the design, so it is tested as one: each row names the key
// that must decide.
func TestTieBreak(t *testing.T) {
	all := []string{"a", "b", "c"}
	for _, tc := range []struct {
		name   string
		among  []string
		norm   map[string]string
		chosen string
		key    trace.TieBreakKey
	}{
		{
			name: "the most central answer wins", among: []string{"a", "b"},
			norm:   map[string]string{"a": "101", "b": "42", "c": "the answer is 101"},
			chosen: "a", key: trace.TieBreakConsensus,
		},
		{
			name: "then the shortest", among: []string{"a", "b"},
			norm:   map[string]string{"a": "the answer is 101", "b": "101", "c": "no idea"},
			chosen: "b", key: trace.TieBreakLength,
		},
		{
			// 4 runes against 3 is not a verbosity difference, so the
			// length key declines and the digest decides.
			name: "lengths within the gate fall through to the hash", among: []string{"b", "c"},
			norm:   map[string]string{"a": "no", "b": "red", "c": "blue"},
			chosen: "c", key: trace.TieBreakHash,
		},
		{
			// The shortest is not unique, so that key does not fire either.
			name: "no unique shortest falls through too", among: []string{"a", "b", "c"},
			norm:   map[string]string{"a": "one", "b": "two", "c": "three"},
			chosen: "b", key: trace.TieBreakHash,
		},
		{
			name: "the answer decides, not the order it is given in", among: []string{"c", "b"},
			norm:   map[string]string{"a": "no", "b": "red", "c": "blue"},
			chosen: "c", key: trace.TieBreakHash,
		},
		{
			// Identical text would have been a consensus, never a tie; the
			// chain still returns one candidate rather than none.
			name: "identical answers still resolve", among: []string{"b", "c"},
			norm:   map[string]string{"a": "no", "b": "yes", "c": "yes"},
			chosen: "b", key: trace.TieBreakHash,
		},
		{
			name: "with no answers at all it still decides", among: []string{"b", "c"},
			chosen: "b", key: trace.TieBreakHash,
		},
		{
			name: "one candidate is not a tie", among: []string{"c"},
			chosen: "c", key: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chosen, key := tieBreak(tc.among, all, tc.norm)
			if chosen != tc.chosen || key != tc.key {
				t.Errorf("tieBreak(%v) = %q by %q, want %q by %q", tc.among, chosen, key, tc.chosen, tc.key)
			}
		})
	}
}

// ranked is by score, not by proposer order: an all-draw run used to rank
// in the order the pool happened to be configured in, which said nothing.
func TestRankedFollowsTheScore(t *testing.T) {
	rep := &trace.JudgeReport{
		Candidates: []string{"a", "b", "c"},
		Wins:       map[string]int{},
		Pairs: []trace.JudgePair{
			{Pair: []string{"a", "b"}, Orders: []trace.JudgeOrder{{}, {}}, Verdict: "b"},
			{Pair: []string{"a", "c"}, Orders: []trace.JudgeOrder{{}, {}}, Verdict: trace.VerdictDraw, DrawReason: trace.DrawTie},
			{Pair: []string{"b", "c"}, Orders: []trace.JudgeOrder{{}, {}}, Verdict: "b"},
		},
	}
	Aggregate(rep, map[string]string{"a": "aa", "b": "bb", "c": "c"})
	if got := strings.Join(rep.Ranked, ","); got != "b,c,a" {
		t.Errorf("ranked %q, want b,c,a (scores %v)", got, rep.Scores)
	}
}

// answers builds an Input from id, answer pairs.
func answers(pairs ...string) Input {
	in := input()
	for i := 0; i+1 < len(pairs); i += 2 {
		in.Candidates = append(in.Candidates, Candidate{ID: pairs[i], Answer: pairs[i+1]})
	}
	return in
}

func groupsString(groups [][]string) string {
	var out []string
	for _, g := range groups {
		out = append(out, strings.Join(g, ","))
	}
	return strings.Join(out, "|")
}
