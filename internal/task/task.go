// Package task reads a task directory: task.json, instruction.md and the
// files the proposers will see. A task names a git repository and a
// revision; candidates are built from that revision, never from the
// working tree, so a run is reproducible from the trace alone.
//
// task.json has three versions. Version 1 is the propose/select manifest.
// Version 2 adds what a verifier doctor needs: a reference solution, a set
// of mutants, and the thresholds a task considers healthy. Version 3 adds
// the face: a version 3 coding task carries exactly the version 2 fields,
// and a chat task carries a conversation, an optional reference answer and
// an optional rubric instead of a repository. Older files keep their exact
// meaning; a field a version does not have is refused in it rather than
// ignored.
package task

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
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

// Face is which half of CMoA a task belongs to. It is a closed
// enumeration; add a constant, and the exhaustive linter finds every switch
// that must learn it.
type Face string

const (
	// FaceCoding: the proposers answer with a unified diff and a verifier
	// selects. It is what a version 1 or 2 task.json means.
	FaceCoding Face = "coding"
	// FaceChat: the proposers answer a conversation in prose and a judge
	// selects.
	FaceChat Face = "chat"
)

// Chat is everything the chat face adds: the conversation the proposers
// answer, and the two documents only the judge sees.
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

// VerifyKind is how a verifier's answer is read. It is a closed
// enumeration; add a constant, and the exhaustive linter finds every switch
// that must learn it.
type VerifyKind string

const (
	// KindExitCode: the service passes when it exits 0.
	KindExitCode VerifyKind = "exit-code"
	// KindBand: the service reports a measurement that must fall inside a
	// band. Reserved: task.json accepts the word, nothing runs it yet.
	KindBand VerifyKind = "band"
)

// Reference is the task's own solution: a unified diff against Rev that the
// verifier is expected to pass. It measures the verifier's false positives.
//
// The diff may be empty. An empty reference says the tree at Rev already is
// the reference solution, which is what a task built around an existing
// repository looks like: nothing has been broken, and the verifier is
// measured for false positives against the code as it stands.
type Reference struct {
	Path string // as written in task.json, relative to the task dir
	Diff string
}

// Mutant is one deliberate defect. Its diff is written against the tree
// with the reference diff applied, not against Rev, so a doctor applies the
// reference first and the mutant second.
type Mutant struct {
	Path     string // as written in task.json, relative to the task dir
	Diff     string
	Expect   MutantExpect
	Origin   MutantOrigin
	Operator string // the mutation operator, empty for a hand-written mutant
	Note     string
}

// MutantExpect is what a healthy verifier does with a mutant.
type MutantExpect string

const (
	// ExpectKilled: the verifier must fail on this mutant.
	ExpectKilled MutantExpect = "killed"
	// ExpectEquivalent: the mutant does not change behaviour, so passing is
	// not a defect. Such mutants are reported but not counted.
	ExpectEquivalent MutantExpect = "equivalent"
)

// MutantOrigin says who wrote the mutant.
type MutantOrigin string

const (
	// OriginHand: written by a person, and therefore held to a stricter
	// standard than a generated one.
	OriginHand MutantOrigin = "hand"
	// OriginGenerated: produced by a mutation operator.
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

// manifest mirrors task.json. Fields a version 1 file may not carry are
// pointers or have a zero value that means "absent", so Load can refuse
// them instead of silently accepting a version 2 field in a version 1 file.
type manifest struct {
	Version         int      `json:"version"`
	ID              string   `json:"id"`
	Face            string   `json:"face"`
	Repo            string   `json:"repo"`
	Rev             string   `json:"rev"`
	Files           []string `json:"files"`
	Conversation    string   `json:"conversation"`
	Rubric          string   `json:"rubric"`
	MaxContextBytes int      `json:"max_context_bytes"`
	Judge           *struct {
		// A pointer: a written false must be told apart from "absent,
		// take the default", which is true.
		AllowTie *bool `json:"allow_tie"`
	} `json:"judge"`
	Verify struct {
		ComposeFile    string `json:"compose_file"`
		Service        string `json:"service"`
		Kind           string `json:"kind"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	} `json:"verify"`
	Reference *struct {
		Diff   string `json:"diff"`
		Answer string `json:"answer"`
	} `json:"reference"`
	Mutants []manifestMutant `json:"mutants"`
	Doctor  *struct {
		// Pointers: a written 0 is out of range for both, and must be
		// refused rather than read as "absent, take the default".
		KillRateMin   *float64 `json:"kill_rate_min"`
		ReferenceRuns *int     `json:"reference_runs"`
	} `json:"doctor"`
}

type manifestMutant struct {
	Diff     string `json:"diff"`
	Expect   string `json:"expect"`
	Origin   string `json:"origin"`
	Operator string `json:"operator"`
	Note     string `json:"note"`
}

// Option tunes Load.
type Option func(*options)

type options struct{ log func(string, ...any) }

// WithLog gives Load somewhere to say what a task declared and CMoA
// ignored. There is one such line — an instruction.md a chat task does not
// use — and it is a line, not an error: a task directory converted from the
// coding face keeps the file, and refusing it would be refusing history.
func WithLog(logf func(format string, args ...any)) Option {
	return func(o *options) { o.log = logf }
}

// Load reads dir/task.json and everything the manifest names: on the coding
// face instruction.md and every listed repository file, on the chat face
// the conversation and the judge's own documents.
func Load(dir string, opts ...Option) (*Task, error) {
	var o options
	for _, f := range opts {
		f(&o)
	}
	if o.log == nil {
		o.log = func(string, ...any) {}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(abs, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("task: %w", err)
	}
	var m manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("task: decode task.json: %w", err)
	}
	if m.Version < 1 || m.Version > MaxVersion {
		return nil, &ValidationError{"version", fmt.Sprintf("must be between 1 and %d, got %d", MaxVersion, m.Version)}
	}
	if m.Version < 3 {
		if err := rejectV3Fields(&m); err != nil {
			return nil, err
		}
		m.Face = string(FaceCoding)
	}
	if m.Version == 1 {
		if err := rejectV2Fields(&m); err != nil {
			return nil, err
		}
	}
	switch Face(m.Face) {
	case FaceCoding:
	case FaceChat:
	case "":
		return nil, &ValidationError{"face", fmt.Sprintf("is required in version 3; one of [%s %s]", FaceCoding, FaceChat)}
	default:
		return nil, &ValidationError{"face", fmt.Sprintf("%q is not a face; one of [%s %s]", m.Face, FaceCoding, FaceChat)}
	}
	id, err := ParseTaskID(m.ID)
	if err != nil {
		return nil, &ValidationError{"id", err.Error()}
	}
	if m.MaxContextBytes == 0 {
		m.MaxContextBytes = DefaultMaxContextBytes
	}
	if m.MaxContextBytes < 1 {
		return nil, &ValidationError{"max_context_bytes", "must be positive"}
	}
	if Face(m.Face) == FaceChat {
		return loadChat(abs, id, &m, o.log)
	}
	if err := rejectChatFields(&m); err != nil {
		return nil, err
	}
	if m.Repo == "" {
		return nil, &ValidationError{"repo", "is required"}
	}
	repo := m.Repo
	if !filepath.IsAbs(repo) {
		repo = filepath.Join(abs, repo)
	}
	repo = filepath.Clean(repo)
	if st, err := os.Stat(repo); err != nil || !st.IsDir() {
		return nil, &ValidationError{"repo", fmt.Sprintf("%s is not a directory", repo)}
	}
	if m.Rev == "" {
		m.Rev = DefaultRev
	}
	if len(m.Files) == 0 {
		return nil, &ValidationError{"files", "at least one file is required: the proposers see nothing else"}
	}
	if m.Verify.ComposeFile == "" {
		m.Verify.ComposeFile = DefaultComposeFile
	}
	if m.Verify.Service == "" {
		m.Verify.Service = DefaultService
	}
	compose := m.Verify.ComposeFile
	if !filepath.IsAbs(compose) {
		compose = filepath.Join(abs, compose)
	}
	if _, err := os.Stat(compose); err != nil {
		return nil, &ValidationError{"verify.compose_file", fmt.Sprintf("%s: %v", compose, err)}
	}
	if m.Verify.Kind == "" {
		m.Verify.Kind = string(KindExitCode)
	}
	switch VerifyKind(m.Verify.Kind) {
	case KindExitCode, KindBand:
	default:
		return nil, &ValidationError{"verify.kind", fmt.Sprintf("%q is not a verify kind; one of [%s %s]", m.Verify.Kind, KindExitCode, KindBand)}
	}
	if m.Verify.TimeoutSeconds < 0 {
		// 0 is "unset": the caller falls back to cmoa.json, or to no timeout.
		return nil, &ValidationError{"verify.timeout_seconds", "must not be negative"}
	}

	inst, err := os.ReadFile(filepath.Join(abs, InstructionFile))
	if err != nil {
		return nil, fmt.Errorf("task: %w", err)
	}
	if strings.TrimSpace(string(inst)) == "" {
		return nil, &ValidationError{InstructionFile, "must not be empty"}
	}

	t := &Task{
		ID:              id,
		Dir:             abs,
		Face:            FaceCoding,
		Repo:            repo,
		Rev:             m.Rev,
		Instruction:     string(inst),
		MaxContextBytes: m.MaxContextBytes,
		Verify: VerifySpec{
			ComposeFile:    compose,
			Service:        m.Verify.Service,
			Kind:           VerifyKind(m.Verify.Kind),
			TimeoutSeconds: m.Verify.TimeoutSeconds,
		},
	}
	if err := loadDoctor(t, &m); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i, p := range m.Files {
		at := fmt.Sprintf("files[%d]", i)
		clean, err := cleanRepoPath(p)
		if err != nil {
			return nil, &ValidationError{at, err.Error()}
		}
		if seen[clean] {
			return nil, &ValidationError{at, "duplicate path " + clean}
		}
		seen[clean] = true
		b, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(clean)))
		if err != nil {
			return nil, &ValidationError{at, err.Error()}
		}
		if !utf8.Valid(b) {
			return nil, &ValidationError{at, clean + " is not valid UTF-8; proposers only see text"}
		}
		t.Files = append(t.Files, File{Path: clean, Content: string(b)})
	}
	if total := t.ContextBytes(); total > t.MaxContextBytes {
		return nil, &ValidationError{"files", fmt.Sprintf("instruction and files total %d bytes, over max_context_bytes %d", total, t.MaxContextBytes)}
	}
	return t, nil
}

// loadChat builds a chat task: the conversation the proposers answer, plus
// the reference answer and the rubric, which only the judge ever sees.
func loadChat(abs string, id TaskID, m *manifest, logf func(string, ...any)) (*Task, error) {
	if err := rejectCodingFields(m); err != nil {
		return nil, err
	}
	t := &Task{
		ID:              id,
		Dir:             abs,
		Face:            FaceChat,
		MaxContextBytes: m.MaxContextBytes,
		Chat:            &Chat{AllowTie: true},
	}
	if m.Judge != nil && m.Judge.AllowTie != nil {
		t.Chat.AllowTie = *m.Judge.AllowTie
	}
	if m.Conversation == "" {
		m.Conversation = ConversationFile
	}
	conv, err := cleanTaskPath(m.Conversation)
	if err != nil {
		return nil, &ValidationError{"conversation", err.Error()}
	}
	t.Chat.ConversationPath = conv
	b, err := os.ReadFile(filepath.Join(abs, filepath.FromSlash(conv)))
	if err != nil {
		return nil, &ValidationError{"conversation", err.Error()}
	}
	if t.Chat.Conversation, err = parseConversation(b); err != nil {
		return nil, &ValidationError{"conversation", err.Error()}
	}
	if m.Reference != nil && m.Reference.Answer != "" {
		p, body, err := t.readTaskFile(m.Reference.Answer)
		if err != nil {
			return nil, &ValidationError{"reference.answer", err.Error()}
		}
		t.Chat.ReferencePath, t.Chat.ReferenceAnswer = p, body
	}
	if m.Rubric != "" {
		p, body, err := t.readTaskFile(m.Rubric)
		if err != nil {
			return nil, &ValidationError{"rubric", err.Error()}
		}
		t.Chat.RubricPath, t.Chat.Rubric = p, body
	}
	if _, err := os.Stat(filepath.Join(abs, InstructionFile)); err == nil {
		logf("task %s: %s is not used on the chat face; the conversation is the task", id, InstructionFile)
	}
	if total := t.ContextBytes(); total > t.MaxContextBytes {
		return nil, &ValidationError{"conversation", fmt.Sprintf("the conversation totals %d bytes, over max_context_bytes %d", total, t.MaxContextBytes)}
	}
	return t, nil
}

// parseConversation decodes and validates the message array a chat task
// hands the proposers. It must end with a user message: a conversation
// whose last turn is the assistant's has nothing to answer.
func parseConversation(b []byte) ([]ConvMessage, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("is not valid UTF-8; proposers only see text")
	}
	var msgs []ConvMessage
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&msgs); err != nil {
		return nil, err
	}
	if err := ValidateConversation(msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

// ValidateConversation checks the shape a chat task and `cmoa serve` both
// require: at least one message, a known role and non-empty content on
// every one, and a user message last.
func ValidateConversation(msgs []ConvMessage) error {
	if len(msgs) == 0 {
		return errors.New("at least one message is required")
	}
	for i, msg := range msgs {
		switch msg.Role {
		case RoleSystem, RoleUser, RoleAssistant:
		default:
			return fmt.Errorf("messages[%d].role: %q is not a role; one of [%s %s %s]", i, msg.Role, RoleSystem, RoleUser, RoleAssistant)
		}
		if strings.TrimSpace(msg.Content) == "" {
			return fmt.Errorf("messages[%d].content: must not be empty", i)
		}
	}
	if last := msgs[len(msgs)-1]; last.Role != RoleUser {
		return fmt.Errorf("the last message is %q; a conversation ends with a %s message, which is what the proposers answer", last.Role, RoleUser)
	}
	return nil
}

// readTaskFile reads one document named by task.json, relative to the task
// directory. It returns the cleaned path and the body.
func (t *Task) readTaskFile(p string) (string, string, error) {
	clean, err := cleanTaskPath(p)
	if err != nil {
		return "", "", err
	}
	b, err := os.ReadFile(filepath.Join(t.Dir, filepath.FromSlash(clean)))
	if err != nil {
		return "", "", err
	}
	if !utf8.Valid(b) {
		return "", "", errors.New(clean + " is not valid UTF-8")
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", "", errors.New(clean + " is empty")
	}
	return clean, string(b), nil
}

// rejectV3Fields refuses a version 3 field in an older file, so version 1
// and version 2 keep meaning exactly what they meant when they were
// written. A version 2 file has no face; it is the coding face by
// definition, and writing the word would make the two spellings differ.
func rejectV3Fields(m *manifest) error {
	const msg = "requires version 3"
	switch {
	case m.Face != "":
		return &ValidationError{"face", msg}
	case m.Conversation != "":
		return &ValidationError{"conversation", msg}
	case m.Rubric != "":
		return &ValidationError{"rubric", msg}
	case m.Judge != nil:
		return &ValidationError{"judge", msg}
	case m.Reference != nil && m.Reference.Answer != "":
		return &ValidationError{"reference.answer", msg}
	}
	return nil
}

// rejectChatFields refuses a chat field in a coding task. A version 3
// coding task carries exactly the version 2 fields.
func rejectChatFields(m *manifest) error {
	const msg = "belongs to the chat face"
	switch {
	case m.Conversation != "":
		return &ValidationError{"conversation", msg}
	case m.Rubric != "":
		return &ValidationError{"rubric", msg}
	case m.Judge != nil:
		return &ValidationError{"judge", msg}
	case m.Reference != nil && m.Reference.Answer != "":
		return &ValidationError{"reference.answer", msg}
	}
	return nil
}

// rejectCodingFields refuses a coding field in a chat task. A chat task
// names no repository, so a field that describes one is a task written for
// the wrong face rather than a field that happens to be unused.
func rejectCodingFields(m *manifest) error {
	const msg = "belongs to the coding face"
	switch {
	case m.Repo != "":
		return &ValidationError{"repo", msg}
	case m.Rev != "":
		return &ValidationError{"rev", msg}
	case m.Files != nil:
		return &ValidationError{"files", msg}
	case m.Verify != (manifest{}).Verify:
		return &ValidationError{"verify", msg}
	case m.Mutants != nil:
		return &ValidationError{"mutants", msg}
	case m.Doctor != nil:
		return &ValidationError{"doctor", msg}
	case m.Reference != nil && m.Reference.Diff != "":
		return &ValidationError{"reference.diff", msg}
	}
	return nil
}

// rejectV2Fields refuses a version 2 field in a version 1 file. The decoder
// cannot do it: the field is known to the manifest, so DisallowUnknownFields
// lets it through. Refusing it keeps version 1 meaning exactly what it meant
// when it was written.
func rejectV2Fields(m *manifest) error {
	const msg = "requires version 2"
	switch {
	case m.Verify.Kind != "":
		return &ValidationError{"verify.kind", msg}
	case m.Verify.TimeoutSeconds != 0:
		return &ValidationError{"verify.timeout_seconds", msg}
	case m.Reference != nil:
		return &ValidationError{"reference", msg}
	case m.Mutants != nil:
		return &ValidationError{"mutants", msg}
	case m.Doctor != nil:
		return &ValidationError{"doctor", msg}
	}
	return nil
}

// loadDoctor fills Reference, Mutants and Doctor, reading every declared
// diff. A declared diff that is missing is a validation error: a doctor
// that silently skips half its mutants measures nothing. An empty mutant is
// no defect and is refused too; an empty reference is allowed, and means
// the tree at rev is the reference solution.
func loadDoctor(t *Task, m *manifest) error {
	t.Doctor = DoctorSpec{KillRateMin: DefaultKillRateMin, ReferenceRuns: DefaultReferenceRuns}
	if d := m.Doctor; d != nil {
		if d.KillRateMin != nil {
			if *d.KillRateMin <= 0 || *d.KillRateMin > 1 {
				return &ValidationError{"doctor.kill_rate_min", fmt.Sprintf("%v is outside (0, 1]", *d.KillRateMin)}
			}
			t.Doctor.KillRateMin = *d.KillRateMin
		}
		if d.ReferenceRuns != nil {
			if *d.ReferenceRuns < 1 {
				return &ValidationError{"doctor.reference_runs", "must be at least 1"}
			}
			t.Doctor.ReferenceRuns = *d.ReferenceRuns
		}
	}
	if r := m.Reference; r != nil {
		diff, err := t.readDiff(r.Diff, allowEmpty)
		if err != nil {
			return &ValidationError{"reference.diff", err.Error()}
		}
		t.Reference = &Reference{Path: filepath.ToSlash(filepath.Clean(r.Diff)), Diff: diff}
	}
	seen := map[string]bool{}
	for i, mm := range m.Mutants {
		at := fmt.Sprintf("mutants[%d]", i)
		diff, err := t.readDiff(mm.Diff, requireDiff)
		if err != nil {
			return &ValidationError{at + ".diff", err.Error()}
		}
		path := filepath.ToSlash(filepath.Clean(mm.Diff))
		if seen[path] {
			return &ValidationError{at + ".diff", "duplicate path " + path}
		}
		seen[path] = true
		expect := MutantExpect(mm.Expect)
		if expect == "" {
			expect = ExpectKilled
		}
		switch expect {
		case ExpectKilled, ExpectEquivalent:
		default:
			return &ValidationError{at + ".expect", fmt.Sprintf("%q is not an expectation; one of [%s %s]", mm.Expect, ExpectKilled, ExpectEquivalent)}
		}
		origin := MutantOrigin(mm.Origin)
		if origin == "" {
			origin = OriginHand
		}
		switch origin {
		case OriginHand, OriginGenerated:
		default:
			return &ValidationError{at + ".origin", fmt.Sprintf("%q is not an origin; one of [%s %s]", mm.Origin, OriginHand, OriginGenerated)}
		}
		t.Mutants = append(t.Mutants, Mutant{
			Path:     path,
			Diff:     diff,
			Expect:   expect,
			Origin:   origin,
			Operator: mm.Operator,
			Note:     mm.Note,
		})
	}
	return nil
}

// emptyDiff says whether a declared diff may hold nothing. It is a named
// type so the two call sites read as what they mean rather than as true and
// false.
type emptyDiff bool

const (
	requireDiff emptyDiff = false // a mutant with no edit is not a defect
	allowEmpty  emptyDiff = true  // an empty reference is the tree at rev
)

// readDiff reads a diff declared in task.json. The path is relative to the
// task directory and may not leave it; the file must exist either way.
func (t *Task) readDiff(p string, empty emptyDiff) (string, error) {
	clean, err := cleanTaskPath(p)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(t.Dir, filepath.FromSlash(clean)))
	if err != nil {
		return "", err
	}
	if !empty && strings.TrimSpace(string(b)) == "" {
		return "", errors.New(clean + " is empty")
	}
	return string(b), nil
}

// cleanTaskPath rejects absolute paths and anything escaping the task dir.
func cleanTaskPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("is required")
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", fmt.Errorf("%q must be relative to the task directory", p)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%q escapes the task directory", p)
	}
	return clean, nil
}

// cleanRepoPath rejects absolute paths and anything escaping the repo.
func cleanRepoPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", fmt.Errorf("%q must be relative to the repository root", p)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%q escapes the repository", p)
	}
	return clean, nil
}

// ContextBytes is what the task itself pours into the prompt: on the coding
// face the instruction and the full text of every file it lists, on the
// chat face every message of the conversation. It is the number
// max_context_bytes bounds, and the number a caller adds its own additions
// to before deciding whether the prompt still fits. The reference answer
// and the rubric are not in it: the judge sees them, the proposers do not.
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

// ConversationSHA256 is the hex digest of the chat task's conversation as
// CMoA parsed it, for the trace. It is empty on the coding face.
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

// FilePaths lists the file paths in order.
func (t *Task) FilePaths() []string {
	out := make([]string, len(t.Files))
	for i, f := range t.Files {
		out[i] = f.Path
	}
	return out
}

// InstructionSHA256 is the hex digest of instruction.md, for the trace. It
// is the digest of the empty string on the chat face, which has no
// instruction.md.
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
