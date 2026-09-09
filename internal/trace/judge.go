package trace

import (
	"encoding/json"
	"time"
)

type JudgeReport struct {
	SchemaVersion int   `json:"schema_version"`
	RunID         RunID `json:"run_id"`
	// Normalisation versions both consensus and tie-break agreement.
	// Older reports may only record it inside Consensus, or omit it.
	Normalisation string         `json:"normalisation,omitempty"`
	Judge         JudgeParams    `json:"judge"`
	Candidates    []string       `json:"candidates"`   // in the order the caller gave them
	Presentation  Presentation   `json:"presentation"` // how they were shown to the judge
	Pairs         []JudgePair    `json:"pairs"`
	Wins          map[string]int `json:"wins"`
	// Scores is the Copeland score of every candidate: a win is 1, a draw
	// the judge did answer (tie or disagree) is 0.5 to each side, and a
	// draw nobody could measure is 0. Wins is left as it was, because a
	// count of clean sweeps and a score are different questions.
	Scores map[string]float64 `json:"scores"`
	// Consensus is present, and Pairs empty, when the candidates agreed on
	// the normalised answer and the judge was never asked.
	Consensus *Consensus `json:"consensus,omitempty"`
	// TieBreak is present when more than one candidate was still in
	// contention after the score — inside a consensus group, or at the top
	// of the Copeland ranking — and says which key parted them.
	TieBreak *TieBreak    `json:"tie_break,omitempty"`
	Outcome  JudgeOutcome `json:"outcome"`
	Ranked   []string     `json:"ranked"`
	// DrawReasons counts the pairs by DrawReason. A decided pair is not in
	// it, so the values sum to the number of draws.
	DrawReasons          map[DrawReason]int  `json:"draw_reasons"`
	SwapConsistentPairs  int                 `json:"swap_consistent_pairs"`
	InvalidOutputRetries int                 `json:"invalid_output_retries"`
	Sanitized            []Sanitized         `json:"sanitized"`
	InjectionFlags       map[string][]string `json:"injection_flags"`
	Usage                Usage               `json:"usage"`
	LatencyMS            int64               `json:"latency_ms"`
	FinishedAt           time.Time           `json:"finished_at"`
}

// JudgeParams is what the judge endpoint was asked with, minus any key.
type JudgeParams struct {
	Model         string  `json:"model"`
	BaseURL       string  `json:"base_url"`
	Temperature   float64 `json:"temperature"`
	Seed          *int64  `json:"seed"`
	MaxTokens     int     `json:"max_tokens"`
	OutputFormat  string  `json:"output_format"`
	Parallel      int     `json:"parallel"`
	AllowTie      bool    `json:"allow_tie"`
	PromptVersion string  `json:"prompt_version"`
	// ExtraBody is the server-specific part of the request as it was
	// configured — a reasoning effort, say. It is in every call file too;
	// recording it here means the whole judge configuration can be read
	// out of one document.
	ExtraBody map[string]json.RawMessage `json:"extra_body,omitempty"`
}

// Presentation is how the candidates were fenced, and where that came
// from. There is no permutation: every pair is asked in both orders, so
// shuffling the candidates only renumbers the pairs and cannot change one
// byte the judge reads. What can change is the nonce, and it is derived
// from Seed — so a re-run under another seed is a real perturbation of the
// prompt, an irrelevant token the answer ought to be invariant to, rather
// than the same six requests sent twice.
type Presentation struct {
	Seed       int64  `json:"seed"`
	SeedSource string `json:"seed_source"` // run_id, or flag
	Nonce      string `json:"nonce"`
}

// JudgePair is one unordered pair of candidates, asked in both orders.
// Verdict is a candidate id, or "draw": a pair is won only when both orders
// name the same candidate. DrawReason says why a draw was a draw, which the
// outcome's own vocabulary cannot: `all_draws` is a union of four different
// findings, and this field is where the split lives.
type JudgePair struct {
	Pair       []string     `json:"pair"`
	Orders     []JudgeOrder `json:"orders"`
	Verdict    string       `json:"verdict"`
	DrawReason DrawReason   `json:"draw_reason,omitempty"`
}

// VerdictDraw is the Verdict of a pair no candidate won.
const VerdictDraw = "draw"

// DrawReason is why one pair produced no winner. A judge that abstained, a
// judge that contradicted itself under swap, a judge that could not hold a
// format and a judge that never answered are four different findings about
// the judge, and folding them into one word is exactly the conflation an
// agreement metric must not make.
type DrawReason string

const (
	// DrawTie: both orders answered, and at least one called it a tie.
	DrawTie DrawReason = "tie"
	// DrawDisagree: both orders named a candidate, and not the same one.
	// The position spoke, not the quality.
	DrawDisagree DrawReason = "disagree"
	// DrawInvalid: an order never produced usable JSON, retry included.
	DrawInvalid DrawReason = "invalid"
	// DrawUnmeasured: an order timed out or could not be sent, so the pair
	// was never judged at all — which is not the same as judged
	// inconclusive.
	DrawUnmeasured DrawReason = "unmeasured"
)

// JudgeOrder is one call: the pair in one order, and what came back.
type JudgeOrder struct {
	First           string          `json:"first"`
	Second          string          `json:"second"`
	Choice          string          `json:"choice,omitempty"` // A, B or tie, as the judge answered
	ChoiceCandidate string          `json:"choice_candidate,omitempty"`
	Status          JudgeCallStatus `json:"status"`
	Error           string          `json:"error,omitempty"`
	Retries         int             `json:"retries"`
	LatencyMS       int64           `json:"latency_ms"`
	RequestSHA256   string          `json:"request_sha256,omitempty"`
	ResponseSHA256  string          `json:"response_sha256,omitempty"`
	File            string          `json:"file"` // judge/<pair>-<ab|ba>.json
}

// JudgeCallStatus is what one judge call ended as.
type JudgeCallStatus string

const (
	JudgeCallOK            JudgeCallStatus = "ok"             // valid JSON with a choice in the enum
	JudgeCallInvalidOutput JudgeCallStatus = "invalid_output" // still unparsable after the one retry
	JudgeCallTimeout       JudgeCallStatus = "timeout"        // the judge's own timeout elapsed
	JudgeCallError         JudgeCallStatus = "error"          // HTTP or decode failure
)

// The three answers the judge may give inside one call. The labels are
// positional: which candidate A is is only in the trace.
const (
	ChoiceA   = "A"
	ChoiceB   = "B"
	ChoiceTie = "tie"
)

// JudgeOutcome is judge.json's verdict, in the same vocabulary select.json
// uses.
type JudgeOutcome struct {
	Kind        SelectionKind `json:"kind"`
	CandidateID string        `json:"candidate_id,omitempty"`
	Reason      string        `json:"reason"`
}

// Consensus is the stage that runs before the judge: the candidates'
// answers are normalised and compared to each other, and when more than
// half of them say the same thing the judge is never asked. It records the
// normalisation it used, so a trace written under one version of the
// normaliser is not silently compared with another.
type Consensus struct {
	Normalisation string `json:"normalisation"`
	// Groups partitions the candidates into sets that agree, in the order
	// the candidates were given. A candidate that agrees with nobody is a
	// group of one.
	Groups    [][]string         `json:"groups"`
	Chosen    string             `json:"chosen"`
	Agreement ConsensusAgreement `json:"agreement"`
}

// ConsensusAgreement is how the members of the chosen group agreed. Both
// tests are conservative, and which one fired is worth recording: an exact
// agreement is a stronger finding than one number matching.
type ConsensusAgreement string

const (
	// AgreementExact: the normalised answers are the same text.
	AgreementExact ConsensusAgreement = "exact"
	// AgreementNumeric: short answers whose last number is the same.
	AgreementNumeric ConsensusAgreement = "numeric"
)

// TieBreak is how one candidate was chosen from several that the score
// could not part. The chain is deterministic and its key is recorded,
// because a tie broken silently is a decision nobody can audit. No key in
// it reads the presentation order or the proposer order: those are the
// biases the swap exists to detect, not tie-breakers.
type TieBreak struct {
	// Among is the tied set, by id in ascending order — the same list
	// whatever order the run presented the candidates in.
	Among  []string    `json:"among"`
	Key    TieBreakKey `json:"key"`
	Chosen string      `json:"chosen"`
}

// TieBreakKey names the key that decided.
type TieBreakKey string

const (
	// TieBreakConsensus: the candidate whose normalised answer agrees with
	// the most others in the run.
	TieBreakConsensus TieBreakKey = "consensus"
	// TieBreakLength: the shortest normalised answer, when the tied
	// answers differ in length enough for brevity to mean something. The
	// counter-lever to the judges' documented verbosity bias.
	TieBreakLength TieBreakKey = "length"
	// TieBreakHash: the lowest SHA-256 digest of the answer — of the
	// normalised text, or of the raw text when the normalised texts are
	// equal. Arbitrary, but arbitrary about the answer rather than about
	// the position it was shown in or the proposer that wrote it.
	TieBreakHash TieBreakKey = "hash"
	// TieBreakIdentical: no key decided, because the candidates wrote the
	// same answer to the byte. The lowest candidate id is returned — a
	// property of the configuration, not of a position, chosen between
	// answers that are indistinguishable.
	TieBreakIdentical TieBreakKey = "identical"
)

// Sanitized is one rewrite the judge's fencing made to a candidate's text.
// A rewrite changes what is judged, so it is recorded rather than done
// quietly.
type Sanitized struct {
	Candidate string `json:"candidate"`
	What      string `json:"what"`
	Count     int    `json:"count"`
}

// JudgeCall is judge/<pair>-<ab|ba>.json: everything one call sent and
// everything that came back, including the attempt that failed to parse.
type JudgeCall struct {
	SchemaVersion int             `json:"schema_version"`
	RunID         RunID           `json:"run_id"`
	Pair          int             `json:"pair"`
	Order         string          `json:"order"` // ab or ba
	First         string          `json:"first"`
	Second        string          `json:"second"`
	Model         string          `json:"model"`
	BaseURL       string          `json:"base_url"`
	Attempts      []JudgeAttempt  `json:"attempts"`
	Status        JudgeCallStatus `json:"status"`
	Choice        string          `json:"choice,omitempty"`
	LatencyMS     int64           `json:"latency_ms"`
}

// JudgeAttempt is one HTTP round trip of a judge call. The second attempt
// exists only when the first did not parse.
type JudgeAttempt struct {
	Messages       []Message       `json:"messages"`
	Request        json.RawMessage `json:"request"`  // the full body, minus Authorization
	Response       json.RawMessage `json:"response"` // the full body as it came back
	RequestSHA256  string          `json:"request_sha256,omitempty"`
	ResponseSHA256 string          `json:"response_sha256,omitempty"`
	Content        string          `json:"content,omitempty"` // the completion text, reasoning stripped
	Parsed         *JudgeAnswer    `json:"parsed,omitempty"`
	ParseError     string          `json:"parse_error,omitempty"`
	Error          string          `json:"error,omitempty"`
	Usage          Usage           `json:"usage"`
	LatencyMS      int64           `json:"latency_ms"`
}

// JudgeAnswer is the object the judge is asked to return, in the key order
// the grammar fixes: the reason is written before the choice, so the choice
// cannot be reached without passing through it.
type JudgeAnswer struct {
	Reason string `json:"reason"`
	Choice string `json:"choice"`
}

// Dir is the run directory. All paths are derived from it, so nothing else
// in CMoA spells a trace file name.
func (d Dir) WriteJudgeCall(c *JudgeCall) error {
	return writeJSON(d.JudgeCallFile(c.Pair, c.Order), c)
}

// WriteJudge writes judge.json once.
func (d Dir) WriteJudge(r *JudgeReport) error { return writeJSONOnce(d.JudgeFile(), r) }

// ReadJudge reads judge.json.
func (d Dir) ReadJudge() (*JudgeReport, error) {
	var r JudgeReport
	return &r, readJSON(d.JudgeFile(), &r)
}
