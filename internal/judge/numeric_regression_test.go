package judge

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

func TestNumericAgreementRegression(t *testing.T) {
	body, err := os.ReadFile("testdata/numeric-agreement.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, A, B string
		Agreement  trace.ConsensusAgreement
	}
	if err := json.Unmarshal(body, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			for _, pair := range [][2]string{{tc.A, tc.B}, {tc.B, tc.A}} {
				how, ok := agree(Normalize(pair[0]), Normalize(pair[1]))
				if ok != (tc.Agreement != "") || how != tc.Agreement {
					t.Fatalf("agree(%q, %q) = %q, %v; want %q", pair[0], pair[1], how, ok, tc.Agreement)
				}
			}
		})
	}
}

// A false match also gave a tied candidate centrality credit against a
// losing answer. Exercise the complete score and tie-break path.
func TestNumericMismatchDoesNotBreakTieByConsensus(t *testing.T) {
	tx := texts(map[string]string{"a": "3 kilograms", "b": "2 m", "c": "3 m"})
	for _, ids := range permutations([]string{"a", "b", "c"}) {
		rep := &trace.JudgeReport{Candidates: ids, Wins: map[string]int{}, Pairs: []trace.JudgePair{
			drew("a", "b"), won("a", "c", "a"), won("b", "c", "b"),
		}}
		Aggregate(rep, tx)
		if rep.Outcome.CandidateID != "b" || rep.TieBreak == nil || rep.TieBreak.Key != trace.TieBreakLength {
			t.Fatalf("order %v: outcome %+v, tie %+v", ids, rep.Outcome, rep.TieBreak)
		}
	}
}
