package task

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// readTaskFile reads a non-empty UTF-8 document named relative to the task.
func (t *Task) readTaskFile(p string) (string, string, error) {
	clean, b, err := t.readTaskPath(p)
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

// readTaskPath performs the shared task-relative path check and read.
func (t *Task) readTaskPath(p string) (string, []byte, error) {
	clean, err := cleanTaskPath(p)
	if err != nil {
		return "", nil, err
	}
	b, err := os.ReadFile(filepath.Join(t.Dir, filepath.FromSlash(clean)))
	if err != nil {
		return "", nil, err
	}
	return clean, b, nil
}

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
