package trace

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
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
	// OriginReplay: the candidates and every judge answer were read back
	// from a run that had already been made. Nothing was asked of any
	// server, and Run.Replayed says which run was read.
	OriginReplay = "replay"
)

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
	// Replayed is present only when the judge was not asked anything: the
	// answers came from the run it names. It is the one field that tells a
	// re-aggregation from a measurement, so it is written into run.json
	// rather than left to be inferred from a latency of nothing.
	Replayed *Replayed `json:"replayed_from,omitempty"`
}

// Replayed pins the run whose recorded answers were re-aggregated.
//
// The digests are of the source's own files as they were read. A replay is
// only as good as the record it read, and a record that changed under it is
// the one failure that would otherwise be invisible: the outcome would
// still look like arithmetic over calls nobody can now see.
type Replayed struct {
	RunID RunID `json:"run_id"`
	// Dir is the source run directory as it was named on the command line.
	Dir string `json:"dir"`
	// PromptVersion is the source's, and a replay refuses unless it is the
	// binary's own: the same recorded answer to a different question is
	// not the same measurement.
	PromptVersion string `json:"prompt_version"`
	// JudgeSHA256 is the digest of the source judge.json, and Calls the
	// digest of every call file that was replayed, in file-name order.
	JudgeSHA256 string         `json:"judge_sha256"`
	Calls       []ReplayedCall `json:"calls"`
}

// ReplayedCall is one source call file and the digest it was read at.
type ReplayedCall struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
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

// WriteRun writes run.json once.
func (d Dir) WriteRun(r *Run) error { return writeJSONOnce(d.RunFile(), r) }

// ReadRun reads run.json.
func (d Dir) ReadRun() (*Run, error) {
	var r Run
	return &r, readJSON(d.RunFile(), &r)
}
