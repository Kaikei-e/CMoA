// Package trace defines the on-disk record of one CMoA run and the writer
// that produces it. CMoA writes traces and never reads them back; the one
// exception is select, which reads the candidates propose left in the same
// run directory. uzushio and people read everything else.
//
// A run is one directory, <task>/runs/<run-id>/, laid out as:
//
//	run.json                        written once by propose
//	prompt/<proposer-id>.json       the exact request sent to each proposer
//	candidates/<proposer-id>.json   what came back, with a status
//	candidates/<proposer-id>.raw.txt
//	candidates/<proposer-id>.diff   the extracted diff, coding face only
//	candidates/<proposer-id>.txt    the answer, chat face only
//	verify/<proposer-id>/result.json  written by select, coding face only
//	verify/<proposer-id>/stdout.txt
//	verify/<proposer-id>/stderr.txt
//	judge/<pair>-<ab|ba>.json       one judge call, chat face only
//	judge.json                      written once by select or judge
//	select.json                     written once by select
//
// Every JSON file is written atomically (temp file, then rename). run.json
// and select.json refuse to overwrite an existing file: a run is appended
// to, never rewritten.
package trace

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// SchemaVersion is written into run.json and select.json. Bump it when a
// field changes meaning; adding an optional field does not bump it.
const SchemaVersion = 1

// RunID names one run. It is time-ordered so the lexicographically largest
// entry in runs/ is the most recent.
type RunID string

var runIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

// NewRunID returns a fresh RunID for now (UTC): YYYYMMDDTHHMMSSZ-xxxxxxxx.
func NewRunID(now time.Time) RunID {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("trace: crypto/rand failed: " + err.Error())
	}
	return RunID(now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:]))
}

// ParseRunID validates a RunID supplied from outside.
func ParseRunID(s string) (RunID, error) {
	if !runIDPattern.MatchString(s) {
		return "", fmt.Errorf("trace: %q is not a run id (want YYYYMMDDTHHMMSSZ-8hex)", s)
	}
	return RunID(s), nil
}

// CandidateStatus is what propose concluded about one proposer's answer.
type CandidateStatus string

const (
	CandidateOK        CandidateStatus = "ok"         // a diff was extracted, or an answer arrived
	CandidateHTTPError CandidateStatus = "http_error" // non-2xx, connection refused, or undecodable body
	CandidateTimeout   CandidateStatus = "timeout"    // the proposer's own timeout elapsed
	CandidateMalformed CandidateStatus = "malformed"  // 2xx but the body was not a chat completion
	CandidateNoDiff    CandidateStatus = "no_diff"    // a completion arrived but held no unified diff
	CandidateEmpty     CandidateStatus = "empty"      // chat face: a completion arrived with nothing in it
)

// Face is which half of CMoA a run belongs to; it mirrors task.Face.
const (
	FaceCoding = "coding"
	FaceChat   = "chat"
)

// CandidatesOrigin says where a run's candidates came from.
const (
	// OriginProposers: the configured pool answered. What propose writes.
	OriginProposers = "proposers"
	// OriginExternal: the candidates were named on the command line by
	// `cmoa judge`, and no proposer was asked.
	OriginExternal = "external"
)

// VerifyStatus is what select concluded about one candidate.
type VerifyStatus string

const (
	VerifyPass        VerifyStatus = "pass"         // exit code 0
	VerifyFail        VerifyStatus = "fail"         // non-zero exit code
	VerifyApplyFailed VerifyStatus = "apply_failed" // the diff did not apply to the worktree
	VerifyTimeout     VerifyStatus = "timeout"      // killed by verify.timeout_seconds
	VerifyRunnerError VerifyStatus = "runner_error" // docker itself failed; see select.json
	VerifySkipped     VerifyStatus = "skipped"      // candidate status was not ok
)

// BandVerdict is what a banded verifier said about one invariant. The
// vocabulary is the verifier's, not CMoA's: CMoA reads the words and maps
// them onto a VerifyStatus.
type BandVerdict string

const (
	BandPass    BandVerdict = "pass"    // the measurement fell inside the band
	BandFail    BandVerdict = "fail"    // it fell outside
	BandSkipped BandVerdict = "skipped" // the invariant was not measured
	BandInfo    BandVerdict = "info"    // measured, but no band was declared
)

// BandRow is one invariant as the gate CSV reported it. The four numbers
// are null when the verifier left the field empty, which a skipped or info
// row does: null is "not measured", 0 is a measurement of zero.
type BandRow struct {
	Invariant string      `json:"invariant"`
	Value     *float64    `json:"value"`
	CIHalf    *float64    `json:"ci_half"`
	BandLo    *float64    `json:"band_lo"`
	BandHi    *float64    `json:"band_hi"`
	Verdict   BandVerdict `json:"verdict"`
}

// Band is a banded verifier's answer, parsed from the gate CSV it printed.
// Judged counts the rows a band was actually applied to (pass and fail);
// Failed and Skipped name those rows, and Rows keeps every row in the order
// it was printed, info rows included.
type Band struct {
	Judged  int       `json:"judged"`
	Failed  []string  `json:"failed"`
	Skipped []string  `json:"skipped"`
	Rows    []BandRow `json:"rows"`
}

// SelectionKind mirrors the sealed Selection type in internal/selection.
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
type NoCandidateReason string

const (
	// ReasonCycle: every pair was decided and the wins run in a circle.
	ReasonCycle NoCandidateReason = "cycle"
	// ReasonNoMajority: some pair was decided, but no candidate beat all
	// the others.
	ReasonNoMajority NoCandidateReason = "no_majority"
	// ReasonAllDraws: no pair was decided at all.
	ReasonAllDraws NoCandidateReason = "all_draws"
	// ReasonInvalidOutput: the judge never returned usable JSON for a call
	// the outcome needed, retry included.
	ReasonInvalidOutput NoCandidateReason = "invalid_output"
	// ReasonTooFewCandidates: fewer than two answers to compare. A single
	// answer is not a selection; the caller can still read it.
	ReasonTooFewCandidates NoCandidateReason = "too_few_candidates"
)

// Run is run.json.
type Run struct {
	SchemaVersion int             `json:"schema_version"`
	RunID         RunID           `json:"run_id"`
	CreatedAt     time.Time       `json:"created_at"`
	CMoAVersion   string          `json:"cmoa_version"`
	PromptVersion string          `json:"prompt_version"`
	Face          string          `json:"face"`
	Task          TaskRef         `json:"task"`
	Config        json.RawMessage `json:"config"` // effective config, secrets stripped
	Harness       Harness         `json:"harness"`
	Proposers     []ProposerRef   `json:"proposers"`
	Byzantine     Byzantine       `json:"byzantine"`
	// The chat face only.
	ConversationSHA256 string              `json:"conversation_sha256,omitempty"`
	CandidatesOrigin   string              `json:"candidates_origin,omitempty"`
	ExternalCandidates []ExternalCandidate `json:"external_candidates,omitempty"`
}

// ExternalCandidate is one answer `cmoa judge` was handed on the command
// line: which id it was given, which file it was read from, and the digest
// of the bytes as read, so a caller can pin what was judged.
type ExternalCandidate struct {
	ID     string `json:"id"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

// TaskRef pins the task a run read.
type TaskRef struct {
	ID                string   `json:"id"`
	Dir               string   `json:"dir"`
	Repo              string   `json:"repo"`
	Rev               string   `json:"rev"`          // as written in task.json
	ResolvedRev       string   `json:"resolved_rev"` // git rev-parse of Rev
	Files             []string `json:"files"`
	InstructionSHA256 string   `json:"instruction_sha256"`
}

// Harness is the DocDag snapshot the run read: which vault, on which day
// (valid time) and at which revision (transaction time). With these three,
// `docdag --as-of <as_of> --at <at> query --binding` reconstructs it.
// Render is the rendered harness directory the run was given, absent when
// it was given none.
type Harness struct {
	Vault         string         `json:"vault"`
	AsOf          string         `json:"as_of"`
	At            string         `json:"at"`
	DocdagVersion string         `json:"docdag_version"`
	Binding       []HarnessDoc   `json:"binding"`
	Render        *HarnessRender `json:"render,omitempty"`
}

// HarnessRender is the harness directory `--harness` named, as CMoA read
// it: every file it holds and one digest over the lot. CMoA hashes the tree
// itself rather than copying the renderer's manifest, so the two can be
// compared. TreeSHA256 is sha256 over "<path>\n<sha256>\n" per file in
// path order.
type HarnessRender struct {
	Dir           string        `json:"dir"` // absolute, as the vault path is
	TreeSHA256    string        `json:"tree_sha256"`
	RenderedBytes int           `json:"rendered_bytes"` // harness bytes in the two messages
	Files         []HarnessFile `json:"files"`
}

// HarnessFile is one file of the rendered tree, by its slash-separated path
// relative to the directory root.
type HarnessFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// HarnessDoc is one binding document as query --binding lists it.
type HarnessDoc struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Path   string `json:"path"`
}

// ProposerRef identifies a proposer without its request parameters (those
// are in prompt/<id>.json).
type ProposerRef struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	BaseURL string `json:"base_url"`
}

// Byzantine records how many deceptive proposers the pool tolerates:
// n proposers tolerate f = floor((n-1)/3). Three proposers tolerate none.
type Byzantine struct {
	N int `json:"n"`
	F int `json:"f"`
}

// Prompt is prompt/<proposer-id>.json: exactly what was sent.
type Prompt struct {
	ProposerID string          `json:"proposer_id"`
	Messages   []Message       `json:"messages"`
	Request    json.RawMessage `json:"request"` // the full HTTP body, minus Authorization
	SHA256     string          `json:"sha256"`  // of Request
}

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Candidate is candidates/<proposer-id>.json.
type Candidate struct {
	ProposerID   string          `json:"proposer_id"`
	Model        string          `json:"model"`
	Face         string          `json:"face,omitempty"`
	Origin       string          `json:"origin,omitempty"` // external, for a candidate cmoa judge was handed
	Status       CandidateStatus `json:"status"`
	Error        string          `json:"error,omitempty"`
	FinishReason string          `json:"finish_reason,omitempty"`
	Usage        Usage           `json:"usage"`
	Timings      Timings         `json:"timings"`
	Diff         *DiffStats      `json:"diff,omitempty"` // coding face, only when Status == ok
	// The chat face, only when Status == ok.
	AnswerSHA256   string             `json:"answer_sha256,omitempty"`
	AnswerBytes    int                `json:"answer_bytes,omitempty"`
	Metadata       *CandidateMetadata `json:"metadata,omitempty"`
	RequestSHA256  string             `json:"request_sha256"`
	ResponseSHA256 string             `json:"response_sha256,omitempty"`
	StartedAt      time.Time          `json:"started_at"`
	FinishedAt     time.Time          `json:"finished_at"`
}

// CandidateMetadata is the style-control accounting a preference harness
// records for every answer. None of it reaches the judge's prompt: it exists so a later
// analysis can ask whether the judge was buying length and decoration, and
// that question cannot be answered by numbers nobody wrote down at the
// time. TokenLen is the server's completion_tokens, or -1 when it reported
// none.
type CandidateMetadata struct {
	TokenLen       int `json:"token_len"`
	Chars          int `json:"chars"`
	HeaderCount    int `json:"header_count"`
	ListCount      int `json:"list_count"`
	BoldCount      int `json:"bold_count"`
	CodeFenceCount int `json:"code_fence_count"`
}

// Usage is the token accounting the server reported (zero when absent).
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Timings: RequestMS is measured by CMoA; the rest come from llama-server's
// `timings` object and are zero on servers that do not send it.
type Timings struct {
	RequestMS         int64   `json:"request_ms"`
	ServerPromptMS    float64 `json:"server_prompt_ms,omitempty"`
	ServerPredictedMS float64 `json:"server_predicted_ms,omitempty"`
	TokensPerSecond   float64 `json:"tokens_per_second,omitempty"`
}

// DiffStats summarises an extracted diff.
type DiffStats struct {
	Files     []string `json:"files"`
	Additions int      `json:"additions"`
	Deletions int      `json:"deletions"`
	SHA256    string   `json:"sha256"`
}

// VerifyResult is verify/<proposer-id>/result.json. It is also embedded in
// Verification, which has no candidate; the id is omitted when empty for
// that reason only. Inside a run it is always set.
type VerifyResult struct {
	CandidateID string       `json:"candidate_id,omitempty"`
	Status      VerifyStatus `json:"status"`
	ExitCode    int          `json:"exit_code"`
	DurationMS  int64        `json:"duration_ms"`
	Command     []string     `json:"command,omitempty"`
	ProjectName string       `json:"project_name,omitempty"`
	ApplyError  string       `json:"apply_error,omitempty"`
	Error       string       `json:"error,omitempty"`
	Band        *Band        `json:"band,omitempty"` // only a verify.kind band verifier
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
}

// Verification is one verification outside a run: what `cmoa verify` prints
// on stdout and writes as result.json with --out. The embedded VerifyResult
// says what the verifier did, in the same vocabulary select uses (`skipped`
// excepted: nothing is skipped when a diff is named on the command line);
// the surrounding fields say what was verified.
type Verification struct {
	SchemaVersion int    `json:"schema_version"`
	Task          string `json:"task"`
	Rev           string `json:"rev"`         // the resolved commit SHA
	DiffSHA256    string `json:"diff_sha256"` // of the diff bytes as read
	Label         string `json:"label"`
	VerifyResult
	CMoAVersion string `json:"cmoa_version"`
}

// Select is select.json.
type Select struct {
	SchemaVersion int             `json:"schema_version"`
	RunID         RunID           `json:"run_id"`
	Rule          string          `json:"rule"`
	Order         []string        `json:"order"` // candidate ids in the order they were considered
	Selection     SelectionRecord `json:"selection"`
	AlsoPassed    []string        `json:"also_passed"`
	// Ranked is the chat face's candidate ids by wins, ties broken by the
	// order above. It is informational: only the Selection decides.
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
type JudgeReport struct {
	SchemaVersion        int                 `json:"schema_version"`
	RunID                RunID               `json:"run_id"`
	Judge                JudgeParams         `json:"judge"`
	Candidates           []string            `json:"candidates"`   // in the order the caller gave them
	Presentation         Presentation        `json:"presentation"` // how they were shown to the judge
	Pairs                []JudgePair         `json:"pairs"`
	Wins                 map[string]int      `json:"wins"`
	Outcome              JudgeOutcome        `json:"outcome"`
	Ranked               []string            `json:"ranked"`
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

// Presentation is how the candidates were shuffled and fenced. Permutation
// holds indices into Candidates, in the order the judge saw them; without
// it a re-run cannot be compared with this one.
type Presentation struct {
	Permutation []int  `json:"permutation"`
	Nonce       string `json:"nonce"`
	SeedSource  string `json:"seed_source"` // run_id, or flag
}

// JudgePair is one unordered pair of candidates, asked in both orders.
// Verdict is a candidate id, or "draw": a pair is won only when both orders
// name the same candidate.
type JudgePair struct {
	Pair    []string     `json:"pair"`
	Orders  []JudgeOrder `json:"orders"`
	Verdict string       `json:"verdict"`
}

// VerdictDraw is the Verdict of a pair no candidate won.
const VerdictDraw = "draw"

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
type Dir string

// RunsRoot returns <taskDir>/runs.
func RunsRoot(taskDir string) string { return filepath.Join(taskDir, "runs") }

// Create makes <taskDir>/runs/<id> and its subdirectories. It fails if the
// directory already exists.
func Create(taskDir string, id RunID) (Dir, error) {
	d := filepath.Join(RunsRoot(taskDir), string(id))
	if _, err := os.Stat(d); err == nil {
		return "", fmt.Errorf("trace: run %s already exists at %s", id, d)
	}
	for _, sub := range []string{"prompt", "candidates", "verify"} {
		if err := os.MkdirAll(filepath.Join(d, sub), 0o755); err != nil {
			return "", fmt.Errorf("trace: create %s: %w", d, err)
		}
	}
	return Dir(d), nil
}

// Open returns an existing run directory, checking that run.json is there.
func Open(runDir string) (Dir, error) {
	if _, err := os.Stat(filepath.Join(runDir, "run.json")); err != nil {
		return "", fmt.Errorf("trace: %s is not a run directory: %w", runDir, err)
	}
	if _, err := ParseRunID(filepath.Base(runDir)); err != nil {
		return "", err
	}
	return Dir(runDir), nil
}

// Latest returns the most recent run directory under <taskDir>/runs, or
// ErrNoRuns.
func Latest(taskDir string) (Dir, error) {
	entries, err := os.ReadDir(RunsRoot(taskDir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNoRuns
		}
		return "", err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && runIDPattern.MatchString(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 {
		return "", ErrNoRuns
	}
	sort.Strings(ids)
	return Dir(filepath.Join(RunsRoot(taskDir), ids[len(ids)-1])), nil
}

// ErrNoRuns is returned by Latest when runs/ is absent or empty.
var ErrNoRuns = errors.New("trace: no runs")

// ID is the run id of the directory.
func (d Dir) ID() RunID { return RunID(filepath.Base(string(d))) }

// Path helpers. These are the only place file names are spelled.
func (d Dir) RunFile() string             { return filepath.Join(string(d), "run.json") }
func (d Dir) SelectFile() string          { return filepath.Join(string(d), "select.json") }
func (d Dir) PromptFile(id string) string { return filepath.Join(string(d), "prompt", id+".json") }
func (d Dir) CandidateFile(id string) string {
	return filepath.Join(string(d), "candidates", id+".json")
}
func (d Dir) CandidateRaw(id string) string {
	return filepath.Join(string(d), "candidates", id+".raw.txt")
}
func (d Dir) CandidateDiff(id string) string {
	return filepath.Join(string(d), "candidates", id+".diff")
}
func (d Dir) CandidateAnswer(id string) string {
	return filepath.Join(string(d), "candidates", id+".txt")
}
func (d Dir) JudgeDir() string  { return filepath.Join(string(d), "judge") }
func (d Dir) JudgeFile() string { return filepath.Join(string(d), "judge.json") }

// JudgeCallFile names one call of the pairwise protocol. The name is the
// pair index and the order, so the six files of a three-candidate selection
// sort into the order they were built in.
func (d Dir) JudgeCallFile(pair int, order string) string {
	return filepath.Join(d.JudgeDir(), fmt.Sprintf("%d-%s.json", pair, order))
}

// JudgeCallName is JudgeCallFile relative to the run directory, which is
// how judge.json refers to it.
func JudgeCallName(pair int, order string) string {
	return fmt.Sprintf("judge/%d-%s.json", pair, order)
}

func (d Dir) VerifyDir(id string) string    { return filepath.Join(string(d), "verify", id) }
func (d Dir) VerifyResult(id string) string { return filepath.Join(d.VerifyDir(id), "result.json") }
func (d Dir) VerifyStdout(id string) string { return filepath.Join(d.VerifyDir(id), "stdout.txt") }
func (d Dir) VerifyStderr(id string) string { return filepath.Join(d.VerifyDir(id), "stderr.txt") }

// WriteRun writes run.json once.
func (d Dir) WriteRun(r *Run) error { return writeJSONOnce(d.RunFile(), r) }

// WriteSelect writes select.json once.
func (d Dir) WriteSelect(s *Select) error { return writeJSONOnce(d.SelectFile(), s) }

// WritePrompt writes prompt/<id>.json.
func (d Dir) WritePrompt(p *Prompt) error { return writeJSON(d.PromptFile(p.ProposerID), p) }

// WriteCandidate writes candidates/<id>.json, the raw response, and the
// diff when there is one. An empty diff writes no .diff file.
func (d Dir) WriteCandidate(c *Candidate, raw []byte, diff string) error {
	if err := writeFileAtomic(d.CandidateRaw(c.ProposerID), raw); err != nil {
		return err
	}
	if diff != "" {
		if err := writeFileAtomic(d.CandidateDiff(c.ProposerID), []byte(diff)); err != nil {
			return err
		}
	}
	return writeJSON(d.CandidateFile(c.ProposerID), c)
}

// WriteChatCandidate writes candidates/<id>.json, the raw response, and the
// answer as candidates/<id>.txt. An empty answer writes no .txt file, the
// way an empty diff writes no .diff.
func (d Dir) WriteChatCandidate(c *Candidate, raw []byte, answer string) error {
	if err := writeFileAtomic(d.CandidateRaw(c.ProposerID), raw); err != nil {
		return err
	}
	if answer != "" {
		if err := writeFileAtomic(d.CandidateAnswer(c.ProposerID), []byte(answer)); err != nil {
			return err
		}
	}
	return writeJSON(d.CandidateFile(c.ProposerID), c)
}

// ReadCandidateAnswer reads candidates/<id>.txt.
func (d Dir) ReadCandidateAnswer(id string) (string, error) {
	b, err := os.ReadFile(d.CandidateAnswer(id))
	return string(b), err
}

// WriteJudgeCall writes judge/<pair>-<order>.json.
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

// WriteVerify writes verify/<id>/{result.json,stdout.txt,stderr.txt}.
func (d Dir) WriteVerify(r *VerifyResult, stdout, stderr []byte) error {
	if err := os.MkdirAll(d.VerifyDir(r.CandidateID), 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(d.VerifyStdout(r.CandidateID), stdout); err != nil {
		return err
	}
	if err := writeFileAtomic(d.VerifyStderr(r.CandidateID), stderr); err != nil {
		return err
	}
	return writeJSON(d.VerifyResult(r.CandidateID), r)
}

// WriteVerification writes result.json, stdout.txt and stderr.txt into dir,
// which is created if it does not exist. result.json is write-once: a
// verification directory records one verification.
func WriteVerification(dir string, v *Verification, stdout, stderr []byte) error {
	if _, err := os.Stat(VerificationFile(dir)); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, VerificationFile(dir))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "stdout.txt"), stdout); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "stderr.txt"), stderr); err != nil {
		return err
	}
	return writeJSONOnce(filepath.Join(dir, "result.json"), v)
}

// VerificationFile is the result.json WriteVerification writes into dir.
func VerificationFile(dir string) string { return filepath.Join(dir, "result.json") }

// ReadRun reads run.json.
func (d Dir) ReadRun() (*Run, error) {
	var r Run
	return &r, readJSON(d.RunFile(), &r)
}

// ReadCandidate reads candidates/<id>.json.
func (d Dir) ReadCandidate(id string) (*Candidate, error) {
	var c Candidate
	return &c, readJSON(d.CandidateFile(id), &c)
}

// ReadCandidateDiff reads candidates/<id>.diff.
func (d Dir) ReadCandidateDiff(id string) (string, error) {
	b, err := os.ReadFile(d.CandidateDiff(id))
	return string(b), err
}

// ReadSelect reads select.json.
func (d Dir) ReadSelect() (*Select, error) {
	var s Select
	return &s, readJSON(d.SelectFile(), &s)
}

// ErrExists is wrapped when a write-once file is already present.
var ErrExists = errors.New("trace: file already exists")

func writeJSONOnce(path string, v any) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, path)
	}
	return writeJSON(path, v)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("trace: encode %s: %w", path, err)
	}
	return writeFileAtomic(path, append(b, '\n'))
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("trace: decode %s: %w", path, err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
