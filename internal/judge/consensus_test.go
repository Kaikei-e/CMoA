package judge

import (
	"fmt"
	"sort"
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
		{"underscores stay: they are word characters", "user_id", "user_id"},
		{"a bullet at the start of a line goes", "- 101\n- 102", "101 102"},
		{"a numbered marker goes too", "1. alpha\n2. beta", "alpha beta"},
		{"and a parenthesised one", "1) alpha\n2) beta", "alpha beta"},
		{"every marker in a one-line list goes", "1. りんご 2. みかん 3. ぶどう", "りんご みかん ぶどう"},
		{"a full-width marker is one too", "１．りんご ２．みかん", "りんご みかん"},
		{"a hyphen inside a word is not a marker", "well-known answer", "well-known answer"},
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
		{"3、000円", "3000", true},
		{"候補は 1、2、3 です", "3", true},
		{"1,2345 is not grouped", "2345", true},
		{"1e5", "1e5", true},
		{"2.5e-3", "2.5e-3", true},
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
		{name: "an enumeration is not a number", a: "1. りんご 2. みかん 3. ぶどう", b: "1. 犬 2. 猫 3. 鳥"},
		{name: "a comma-separated list is not one number", a: "候補は 1、2、3 です", b: "123 です"},
		{name: "but a grouped thousand is", a: "3、000円", b: "3000円", how: trace.AgreementNumeric, want: true},
		{name: "an exponent is not its mantissa's tail", a: "1e5", b: "答えは5"},
		{name: "nor is it the number it stands for", a: "1e5", b: "100000"},
		{name: "a negation is not the same answer", a: "3つあります。", b: "3つではありません"},
		{name: "yes is not no", a: "はい、答えは長さ0のスライスです", b: "いいえ、それは長さ0のスライスではありません"},
		{name: "two denials still agree", a: "3つはありません", b: "3つではない", how: trace.AgreementNumeric, want: true},
		{name: "an identifier is not another identifier", a: "the field is user_id", b: "the field is userid"},
		{name: "knowing is not denying", a: "i know it is 5", b: "the answer is 5", how: trace.AgreementNumeric, want: true},
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
			// Two of four can write the same answer and still not be a
			// consensus, so this is reachable. No key can part them, and
			// saying "hash" would claim one did.
			name: "identical answers resolve without claiming a key", among: []string{"c", "b"},
			norm:   map[string]string{"a": "no", "b": "yes", "c": "yes"},
			chosen: "b", key: trace.TieBreakIdentical,
		},
		{
			name: "with no answers at all it still decides", among: []string{"c", "b"},
			chosen: "b", key: trace.TieBreakIdentical,
		},
		{
			// An answer that normalises to nothing does not win a tie, and
			// does not switch off the key that punishes it.
			name: "a blank answer loses to one that says something", among: []string{"a", "b"},
			norm:   map[string]string{"a": "", "b": "answer 2"},
			chosen: "b", key: trace.TieBreakLength,
		},
		{
			name: "one candidate is not a tie", among: []string{"c"},
			chosen: "c", key: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chosen, key := tieBreak(tc.among, all, texts(tc.norm))
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
			won("a", "b", "b"), drew("a", "c"), won("b", "c", "b"),
		},
	}
	// b wins both its pairs; a and c drew, which is half a win each, so the
	// shorter of the two ranks above the other.
	Aggregate(rep, texts(map[string]string{"a": "aa", "b": "bb", "c": "c"}))
	if got := strings.Join(rep.Ranked, ","); got != "b,c,a" {
		t.Errorf("ranked %q, want b,c,a (scores %v)", got, rep.Scores)
	}
}

// A verdict is recomputed from the orders, never inherited. Aggregate is
// exported, so a report can arrive with a verdict on it; scoring that as a
// win while also counting the pair as a draw would pay a candidate for a
// pair the judge never decided.
func TestAggregateRecomputesTheVerdict(t *testing.T) {
	stale := drew("a", "b")
	stale.Verdict, stale.DrawReason = "b", trace.DrawInvalid
	rep := &trace.JudgeReport{Candidates: []string{"a", "b"}, Wins: map[string]int{}, Pairs: []trace.JudgePair{stale}}
	Aggregate(rep, texts(map[string]string{"a": "alpha", "b": "beta"}))
	if p := rep.Pairs[0]; p.Verdict != trace.VerdictDraw || p.DrawReason != trace.DrawTie {
		t.Fatalf("pair %+v", p)
	}
	if rep.Scores["a"] != 0.5 || rep.Scores["b"] != 0.5 {
		t.Errorf("scores %v", rep.Scores)
	}
}

// won and drew build a pair as the judge would have left it: both orders
// answered, and Aggregate recomputes the verdict from them.
func won(a, b, winner string) trace.JudgePair {
	first, second := trace.ChoiceA, trace.ChoiceB
	if winner == b {
		first, second = trace.ChoiceB, trace.ChoiceA
	}
	return trace.JudgePair{Pair: []string{a, b}, Orders: []trace.JudgeOrder{
		{First: a, Second: b, Choice: first, ChoiceCandidate: winner, Status: trace.JudgeCallOK},
		{First: b, Second: a, Choice: second, ChoiceCandidate: winner, Status: trace.JudgeCallOK},
	}}
}

func drew(a, b string) trace.JudgePair {
	return trace.JudgePair{Pair: []string{a, b}, Orders: []trace.JudgeOrder{
		{First: a, Second: b, Choice: trace.ChoiceTie, Status: trace.JudgeCallOK},
		{First: b, Second: a, Choice: trace.ChoiceTie, Status: trace.JudgeCallOK},
	}}
}

// Nothing the run decides may depend on the order the candidates were given
// in. This runs the whole aggregation over every permutation of that order
// and demands the same answer, the same scores, the same ranking and the
// same tie-break record each time — the property the earlier determinism
// check could not see, because it re-ran one order twice.
func TestPermutationInvariance(t *testing.T) {
	for _, tc := range []struct {
		name  string
		norm  map[string]string
		pairs []trace.JudgePair
	}{
		{
			name:  "a tie the chain parts",
			norm:  map[string]string{"a": "alpha", "b": "beta", "c": "gamma"},
			pairs: []trace.JudgePair{drew("a", "b"), drew("a", "c"), drew("b", "c")},
		},
		{
			name:  "two candidates that wrote the same answer",
			norm:  map[string]string{"a": "same", "b": "same", "c": "other"},
			pairs: []trace.JudgePair{drew("a", "b"), won("a", "c", "a"), won("b", "c", "b")},
		},
		{
			name:  "a clear winner",
			norm:  map[string]string{"a": "alpha", "b": "beta", "c": "gamma"},
			pairs: []trace.JudgePair{won("a", "b", "a"), won("a", "c", "a"), drew("b", "c")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var want string
			for _, ids := range permutations([]string{"a", "b", "c"}) {
				rep := &trace.JudgeReport{Candidates: ids, Wins: map[string]int{}, Pairs: clonePairs(tc.pairs)}
				Aggregate(rep, texts(tc.norm))
				got := fmt.Sprintf("outcome=%v scores=%v ranked=%v tie=%+v",
					rep.Outcome, sortedScores(rep.Scores), rep.Ranked, rep.TieBreak)
				if want == "" {
					want = got
				}
				if got != want {
					t.Errorf("order %v: got %s, want %s", ids, got, want)
				}
			}
		})
	}
}

// The consensus stage is order-invariant too: which member of the group is
// returned cannot depend on which proposer happened to be asked first.
func TestConsensusIsOrderInvariant(t *testing.T) {
	answers := map[string]string{"a": "1 + 100 = 101", "b": "１０１です。", "c": "たぶん 55"}
	var want string
	for _, ids := range permutations([]string{"a", "b", "c"}) {
		tx := Texts{}
		for id, ans := range answers {
			tx[id] = Text{Norm: Normalize(ans), Raw: ans}
		}
		cons, tb := consensus(ids, tx)
		if cons == nil {
			t.Fatalf("order %v found no consensus", ids)
		}
		got := fmt.Sprintf("chosen=%s agreement=%s tie=%+v", cons.Chosen, cons.Agreement, tb)
		if want == "" {
			want = got
		}
		if got != want {
			t.Errorf("order %v: got %s, want %s", ids, got, want)
		}
	}
}

func permutations(ids []string) [][]string {
	if len(ids) <= 1 {
		return [][]string{ids}
	}
	var out [][]string
	for i := range ids {
		rest := append(append([]string{}, ids[:i]...), ids[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]string{ids[i]}, p...))
		}
	}
	return out
}

func clonePairs(pairs []trace.JudgePair) []trace.JudgePair {
	out := make([]trace.JudgePair, len(pairs))
	for i, p := range pairs {
		out[i] = p
		out[i].Orders = append([]trace.JudgeOrder{}, p.Orders...)
	}
	return out
}

// sortedScores prints a score map in a fixed order, so comparing two of
// them compares the scores and not Go's map iteration.
func sortedScores(sc map[string]float64) string {
	ids := make([]string, 0, len(sc))
	for id := range sc {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&b, "%s=%v ", id, sc[id])
	}
	return b.String()
}

// texts turns a table's normalised answers into what the chain reads. The
// raw text is the normalised text: only a run whose candidates wrote the
// same normalised answer needs them to differ.
func texts(norm map[string]string) Texts {
	tx := Texts{}
	for id, n := range norm {
		tx[id] = Text{Norm: n, Raw: n}
	}
	return tx
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
