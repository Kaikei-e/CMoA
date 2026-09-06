package trace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

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
