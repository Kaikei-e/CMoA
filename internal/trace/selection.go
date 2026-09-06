package trace

import "time"

type SelectionKind string

const (
	SelectionSelected       SelectionKind = "selected"
	SelectionNoCandidate    SelectionKind = "no_candidate"
	SelectionJudgeTimeout   SelectionKind = "judge_timeout"
	SelectionJudgeFailed    SelectionKind = "judge_failed"
	SelectionVerifierFailed SelectionKind = "verifier_failed"
)

// NoCandidateReason sub-classifies a no_candidate on the chat face. The
// distribution of these words over a calibration set is itself a measure of
// the judge, which is why they are recorded rather than folded into one.
//
// Three of them are historical. Since the chat face settles an undecided
// ranking with a Copeland score and a deterministic tie-break, a run whose
// pairs all drew, whose wins ran in a circle or whose leader was blocked by
// one draw is selected rather than refused. They stay declared because
// traces written before that change still carry the words.
type NoCandidateReason string

const (
	// ReasonCycle: every pair was decided and the wins run in a circle.
	// Historical: not produced since the score settled it.
	ReasonCycle NoCandidateReason = "cycle"
	// ReasonNoMajority: some pair was decided, but no candidate beat all
	// the others. Historical: not produced since the score settled it.
	ReasonNoMajority NoCandidateReason = "no_majority"
	// ReasonAllDraws: no pair was decided at all. Historical: not produced
	// since the score settled it.
	ReasonAllDraws NoCandidateReason = "all_draws"
	// ReasonInvalidOutput: the judge never returned usable JSON for a call
	// the outcome needed, retry included.
	ReasonInvalidOutput NoCandidateReason = "invalid_output"
	// ReasonTooFewCandidates: fewer than two answers to compare. A single
	// answer is not a selection; the caller can still read it.
	ReasonTooFewCandidates NoCandidateReason = "too_few_candidates"
)

// Run is run.json.
type Select struct {
	SchemaVersion int             `json:"schema_version"`
	RunID         RunID           `json:"run_id"`
	Rule          string          `json:"rule"`
	Order         []string        `json:"order"` // candidate ids in the order they were considered
	Selection     SelectionRecord `json:"selection"`
	AlsoPassed    []string        `json:"also_passed"`
	// Ranked is the chat face's candidate ids by Copeland score, ties
	// broken by the chain judge.json's tie_break names. It is
	// informational: only the Selection decides.
	Ranked      []string  `json:"ranked,omitempty"`
	MaxParallel int       `json:"max_parallel"`
	FinishedAt  time.Time `json:"finished_at"`
}

// SelectionRecord is the JSON shape of the sealed Selection type. Only the
// fields for Kind are populated.
type SelectionRecord struct {
	Kind        SelectionKind `json:"kind"`
	CandidateID string        `json:"candidate_id,omitempty"` // selected
	Reason      string        `json:"reason,omitempty"`       // selected; the sub-reason for no_candidate
	Tried       int           `json:"tried,omitempty"`        // no_candidate
	AfterMS     int64         `json:"after_ms,omitempty"`     // judge_timeout
	Error       string        `json:"error,omitempty"`        // verifier_failed, judge_failed
}

// JudgeReport is judge.json: the whole pairwise protocol for one selection,
// written once by select or judge on the chat face. It is the record a
// calibration reads, so everything the judge saw and every quantity that
// could explain the outcome is in it or in the file it names.

// WriteSelect writes select.json once.
func (d Dir) WriteSelect(s *Select) error { return writeJSONOnce(d.SelectFile(), s) }

// ReadSelect reads select.json.
func (d Dir) ReadSelect() (*Select, error) {
	var s Select
	return &s, readJSON(d.SelectFile(), &s)
}
