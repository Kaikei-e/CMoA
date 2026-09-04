// Package task reads a task directory: task.json, instruction.md and the
// files the proposers will see. A task names a git repository and a
// revision; candidates are built from that revision, never from the
// working tree, so a run is reproducible from the trace alone.
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
	Repo            string // absolute
	Rev             string // as written; ResolveRev turns it into a SHA
	Files           []File
	Instruction     string
	MaxContextBytes int
	Verify          VerifySpec
}

// File is one repository file the proposers see in full.
type File struct {
	Path    string // relative to repo root, forward slashes
	Content string
}

// VerifySpec names the compose service that verifies a candidate.
type VerifySpec struct {
	ComposeFile string // absolute
	Service     string
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
	InstructionFile        = "instruction.md"
	ManifestFile           = "task.json"
)

// ValidationError reports one field of task.json that failed validation.
type ValidationError struct {
	Path string
	Msg  string
}

func (e *ValidationError) Error() string { return "task.json: " + e.Path + ": " + e.Msg }

type manifest struct {
	Version         int      `json:"version"`
	ID              string   `json:"id"`
	Repo            string   `json:"repo"`
	Rev             string   `json:"rev"`
	Files           []string `json:"files"`
	MaxContextBytes int      `json:"max_context_bytes"`
	Verify          struct {
		ComposeFile string `json:"compose_file"`
		Service     string `json:"service"`
	} `json:"verify"`
}

// Load reads dir/task.json, dir/instruction.md and every listed file.
func Load(dir string) (*Task, error) {
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
	if m.Version != 1 {
		return nil, &ValidationError{"version", fmt.Sprintf("must be 1, got %d", m.Version)}
	}
	id, err := ParseTaskID(m.ID)
	if err != nil {
		return nil, &ValidationError{"id", err.Error()}
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
	if m.MaxContextBytes == 0 {
		m.MaxContextBytes = DefaultMaxContextBytes
	}
	if m.MaxContextBytes < 1 {
		return nil, &ValidationError{"max_context_bytes", "must be positive"}
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
		Repo:            repo,
		Rev:             m.Rev,
		Instruction:     string(inst),
		MaxContextBytes: m.MaxContextBytes,
		Verify:          VerifySpec{ComposeFile: compose, Service: m.Verify.Service},
	}
	total := len(inst)
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
		total += len(b)
		t.Files = append(t.Files, File{Path: clean, Content: string(b)})
	}
	if total > t.MaxContextBytes {
		return nil, &ValidationError{"files", fmt.Sprintf("instruction and files total %d bytes, over max_context_bytes %d", total, t.MaxContextBytes)}
	}
	return t, nil
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

// FilePaths lists the file paths in order.
func (t *Task) FilePaths() []string {
	out := make([]string, len(t.Files))
	for i, f := range t.Files {
		out[i] = f.Path
	}
	return out
}

// InstructionSHA256 is the hex digest of instruction.md, for the trace.
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
