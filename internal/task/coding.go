package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// loadCoding reads the repository-backed face after common manifest checks.
func loadCoding(abs string, id TaskID, m *manifest) (*Task, error) {
	if err := rejectChatFields(m); err != nil {
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
		ID: id, Dir: abs, Face: FaceCoding, Repo: repo, Rev: m.Rev,
		Instruction: string(inst), MaxContextBytes: m.MaxContextBytes,
		Verify: VerifySpec{ComposeFile: compose, Service: m.Verify.Service, Kind: VerifyKind(m.Verify.Kind), TimeoutSeconds: m.Verify.TimeoutSeconds},
	}
	if err := loadDoctor(t, m); err != nil {
		return nil, err
	}
	if err := loadFiles(t, m.Files); err != nil {
		return nil, err
	}
	if total := t.ContextBytes(); total > t.MaxContextBytes {
		return nil, &ValidationError{"files", fmt.Sprintf("instruction and files total %d bytes, over max_context_bytes %d", total, t.MaxContextBytes)}
	}
	return t, nil
}

func loadFiles(t *Task, paths []string) error {
	seen := map[string]bool{}
	for i, p := range paths {
		at := fmt.Sprintf("files[%d]", i)
		clean, err := cleanRepoPath(p)
		if err != nil {
			return &ValidationError{at, err.Error()}
		}
		if seen[clean] {
			return &ValidationError{at, "duplicate path " + clean}
		}
		seen[clean] = true
		b, err := os.ReadFile(filepath.Join(t.Repo, filepath.FromSlash(clean)))
		if err != nil {
			return &ValidationError{at, err.Error()}
		}
		if !utf8.Valid(b) {
			return &ValidationError{at, clean + " is not valid UTF-8; proposers only see text"}
		}
		t.Files = append(t.Files, File{Path: clean, Content: string(b)})
	}
	return nil
}

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
