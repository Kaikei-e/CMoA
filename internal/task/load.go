package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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

// WithLog gives Load somewhere to say what a task declared and CMoA ignored.
func WithLog(logf func(format string, args ...any)) Option {
	return func(o *options) { o.log = logf }
}

// Load reads dir/task.json and everything the manifest names.
func Load(dir string, opts ...Option) (*Task, error) {
	abs, m, logf, err := readManifest(dir, opts)
	if err != nil {
		return nil, err
	}
	id, err := validateManifest(&m)
	if err != nil {
		return nil, err
	}
	if Face(m.Face) == FaceChat {
		return loadChat(abs, id, &m, logf)
	}
	return loadCoding(abs, id, &m)
}

func readManifest(dir string, opts []Option) (string, manifest, func(string, ...any), error) {
	var o options
	for _, f := range opts {
		f(&o)
	}
	if o.log == nil {
		o.log = func(string, ...any) {}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", manifest{}, nil, err
	}
	raw, err := os.ReadFile(filepath.Join(abs, ManifestFile))
	if err != nil {
		return "", manifest{}, nil, fmt.Errorf("task: %w", err)
	}
	var m manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return "", manifest{}, nil, fmt.Errorf("task: decode task.json: %w", err)
	}
	return abs, m, o.log, nil
}

// validateManifest checks version-independent fields and normalizes defaults
// that every face needs before its own loader begins.
func validateManifest(m *manifest) (TaskID, error) {
	if m.Version < 1 || m.Version > MaxVersion {
		return "", &ValidationError{"version", fmt.Sprintf("must be between 1 and %d, got %d", MaxVersion, m.Version)}
	}
	if m.Version < 3 {
		if err := rejectV3Fields(m); err != nil {
			return "", err
		}
		m.Face = string(FaceCoding)
	}
	if m.Version == 1 {
		if err := rejectV2Fields(m); err != nil {
			return "", err
		}
	}
	switch Face(m.Face) {
	case FaceCoding, FaceChat:
	case "":
		return "", &ValidationError{"face", fmt.Sprintf("is required in version 3; one of [%s %s]", FaceCoding, FaceChat)}
	default:
		return "", &ValidationError{"face", fmt.Sprintf("%q is not a face; one of [%s %s]", m.Face, FaceCoding, FaceChat)}
	}
	id, err := ParseTaskID(m.ID)
	if err != nil {
		return "", &ValidationError{"id", err.Error()}
	}
	if m.MaxContextBytes == 0 {
		m.MaxContextBytes = DefaultMaxContextBytes
	}
	if m.MaxContextBytes < 1 {
		return "", &ValidationError{"max_context_bytes", "must be positive"}
	}
	return id, nil
}

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
