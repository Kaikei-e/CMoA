package trace

import (
	"encoding/json"
	"os"
	"time"
)

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
	// ReasoningBytes is how much of the completion was reasoning rather
	// than answer: the server's reasoning_content, or the <think> block
	// CMoA stripped out of the content. A model that spent its whole
	// budget thinking answers `empty` with a large number here, which is a
	// different failure from a model that answered nothing — and
	// completion_tokens alone cannot tell the two apart.
	ReasoningBytes int        `json:"reasoning_bytes,omitempty"`
	Timings        Timings    `json:"timings"`
	Diff           *DiffStats `json:"diff,omitempty"` // coding face, only when Status == ok
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
// ReasoningTokens is part of CompletionTokens, not additional to it.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
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
func (d Dir) WritePrompt(p *Prompt) error { return writeJSON(d.PromptFile(p.ProposerID), p) }

// WriteCandidate writes candidates/<id>.json, the raw response, and the
// diff when there is one. An empty diff writes no .diff file.
func (d Dir) WriteCandidate(c *Candidate, raw []byte, diff string) error {
	return d.writeCandidate(c, raw, d.CandidateDiff(c.ProposerID), diff)
}

// WriteChatCandidate writes candidates/<id>.json, the raw response, and the
// answer as candidates/<id>.txt. An empty answer writes no .txt file, the
// way an empty diff writes no .diff.
func (d Dir) WriteChatCandidate(c *Candidate, raw []byte, answer string) error {
	return d.writeCandidate(c, raw, d.CandidateAnswer(c.ProposerID), answer)
}

// writeCandidate writes the files that precede a candidate record. The
// record remains last so it is only visible after its accompanying files.
func (d Dir) writeCandidate(c *Candidate, raw []byte, contentPath, content string) error {
	if err := writeFileAtomic(d.CandidateRaw(c.ProposerID), raw); err != nil {
		return err
	}
	if content != "" {
		if err := writeFileAtomic(contentPath, []byte(content)); err != nil {
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

func (d Dir) ReadCandidate(id string) (*Candidate, error) {
	var c Candidate
	return &c, readJSON(d.CandidateFile(id), &c)
}

// ReadCandidateDiff reads candidates/<id>.diff.
func (d Dir) ReadCandidateDiff(id string) (string, error) {
	b, err := os.ReadFile(d.CandidateDiff(id))
	return string(b), err
}
