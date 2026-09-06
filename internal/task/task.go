// Package task reads a task directory: task.json, instruction.md and the
// files the proposers will see. A task names a git repository and a revision;
// candidates are built from that revision, never from the working tree.
package task

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Task is a loaded, validated task.
type Task struct {
	ID              TaskID
	Dir             string // absolute
	Face            Face   // coding in a version 1 or 2 file
	Repo            string // absolute; empty on the chat face
	Rev             string // as written; ResolveRev turns it into a SHA
	Files           []File
	Instruction     string
	MaxContextBytes int
	Verify          VerifySpec
	Reference       *Reference // version 2, nil when the task declares none
	Mutants         []Mutant   // version 2, empty when the task declares none
	Doctor          DoctorSpec // defaults even in a version 1 task
	Chat            *Chat      // the chat face, nil on the coding face
}

// Face is which half of CMoA a task belongs to. It is a closed enumeration.
type Face string

const (
	// FaceCoding: proposers answer with a unified diff and a verifier selects.
	FaceCoding Face = "coding"
	// FaceChat: proposers answer a conversation in prose and a judge selects.
	FaceChat Face = "chat"
)

// Chat is everything the chat face adds, including documents only the judge sees.
type Chat struct {
	ConversationPath string // as written in task.json
	Conversation     []ConvMessage
	ReferencePath    string // as written; empty when the task declares none
	ReferenceAnswer  string
	RubricPath       string // as written; empty when the task declares none
	Rubric           string
	AllowTie         bool
}

// ConvMessage is one message of a chat task's conversation.
type ConvMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// The roles a conversation message may carry.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// File is one repository file the proposers see in full.
type File struct {
	Path    string // relative to repo root, forward slashes
	Content string
}

// VerifySpec names the compose service that verifies a candidate.
type VerifySpec struct {
	ComposeFile    string // absolute
	Service        string
	Kind           VerifyKind
	TimeoutSeconds int // 0: not set by the task; the caller decides
}

// VerifyKind is how a verifier's answer is read. It is a closed enumeration.
type VerifyKind string

const (
	// KindExitCode: the service passes when it exits 0.
	KindExitCode VerifyKind = "exit-code"
	// KindBand reports a measurement which must fall inside a band. task.json
	// accepts it; execution support is reserved for a future implementation.
	KindBand VerifyKind = "band"
)

// Reference is the task's solution: a unified diff against Rev expected to pass.
// Its diff may be empty, meaning the tree at Rev already is the solution.
type Reference struct {
	Path string // as written in task.json, relative to task dir
	Diff string
}

// Mutant is a deliberate defect applied after the reference diff.
type Mutant struct {
	Path     string // as written in task.json, relative to task dir
	Diff     string
	Expect   MutantExpect
	Origin   MutantOrigin
	Operator string // mutation operator, empty for a hand-written mutant
	Note     string
}

// MutantExpect is what a healthy verifier does with a mutant.
type MutantExpect string

const (
	// ExpectKilled: the verifier must fail on this mutant.
	ExpectKilled MutantExpect = "killed"
	// ExpectEquivalent: the mutant is reported but not counted.
	ExpectEquivalent MutantExpect = "equivalent"
)

// MutantOrigin says who wrote the mutant.
type MutantOrigin string

const (
	// OriginHand is held to a stricter standard than a generated mutant.
	OriginHand MutantOrigin = "hand"
	// OriginGenerated was produced by a mutation operator.
	OriginGenerated MutantOrigin = "generated"
)

// DoctorSpec holds the thresholds a doctor judges the verifier against.
type DoctorSpec struct {
	KillRateMin   float64 // 0 < x <= 1
	ReferenceRuns int     // >= 1
}

// TaskID is a validated identifier; it becomes part of compose project names.
type TaskID string

var taskIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ParseTaskID validates s.
func ParseTaskID(s string) (TaskID, error) {
	if !taskIDPattern.MatchString(s) {
		return "", fmt.Errorf("task id %q must match %s", s, taskIDPattern)
	}
	return TaskID(s), nil
}

// Defaults.
const (
	DefaultRev             = "HEAD"
	DefaultMaxContextBytes = 65536
	DefaultComposeFile     = "compose.yaml"
	DefaultService         = "verify"
	DefaultKillRateMin     = 0.8
	DefaultReferenceRuns   = 3
	InstructionFile        = "instruction.md"
	ManifestFile           = "task.json"
	ConversationFile       = "conversation.json"
)

// MaxVersion is the newest task.json this build understands.
const MaxVersion = 3

// ValidationError reports one field of task.json that failed validation.
type ValidationError struct {
	Path string
	Msg  string
}

func (e *ValidationError) Error() string { return "task.json: " + e.Path + ": " + e.Msg }

// ContextBytes is the prompt content: coding instructions/files or chat messages.
// Reference answers and rubrics are excluded because only the judge sees them.
func (t *Task) ContextBytes() int {
	if t.Chat != nil {
		n := 0
		for _, m := range t.Chat.Conversation {
			n += len(m.Content)
		}
		return n
	}
	n := len(t.Instruction)
	for _, f := range t.Files {
		n += len(f.Content)
	}
	return n
}

// ConversationSHA256 is the digest of the parsed chat conversation; it is empty on coding tasks.
func (t *Task) ConversationSHA256() string {
	if t.Chat == nil {
		return ""
	}
	b, err := json.Marshal(t.Chat.Conversation)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// FilePaths lists file paths in order.
func (t *Task) FilePaths() []string {
	out := make([]string, len(t.Files))
	for i, f := range t.Files {
		out[i] = f.Path
	}
	return out
}

// InstructionSHA256 is the instruction digest; chat tasks hash the empty string.
func (t *Task) InstructionSHA256() string {
	sum := sha256.Sum256([]byte(t.Instruction))
	return hex.EncodeToString(sum[:])
}

// ResolveRev turns Rev into a full commit SHA with git rev-parse.
func (t *Task) ResolveRev(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", t.Repo, "rev-parse", "--verify", t.Rev+"^{commit}")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("task: resolve %q in %s: %w: %s", t.Rev, t.Repo, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
