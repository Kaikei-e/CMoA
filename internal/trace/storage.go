package trace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrExists = errors.New("trace: file already exists")

// writeOutputFiles keeps stdout and stderr in the same order in every
// persistence path. Callers write their result JSON only after this returns.
func writeOutputFiles(stdoutPath, stderrPath string, stdout, stderr []byte) error {
	if err := writeFileAtomic(stdoutPath, stdout); err != nil {
		return err
	}
	return writeFileAtomic(stderrPath, stderr)
}

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
