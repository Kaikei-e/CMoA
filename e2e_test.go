package cmoa_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E runs propose and select on examples/task-hello against a real
// proposer fleet, a real DocDag vault and real Docker. It is skipped unless
// CMOA_E2E=1; CMOA_CONFIG must point at a cmoa.json for the fleet.
func TestE2E(t *testing.T) {
	if os.Getenv("CMOA_E2E") != "1" {
		t.Skip("set CMOA_E2E=1 (and CMOA_CONFIG) to run against a live fleet")
	}
	root, _ := os.Getwd()
	bin := build(t, root)
	taskDir := setupExample(t, root)
	run := func(args ...string) string {
		cmd := exec.Command(bin, args...)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("cmoa %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	runDir := run("propose", "--task", taskDir)
	t.Logf("run: %s", runDir)
	result := run("select", "--task", taskDir, "--run", runDir)
	t.Logf("select: %s", result)
	if !strings.HasPrefix(result, "selected ") {
		t.Fatalf("expected a selected candidate, got %q", result)
	}
}

// TestE2EVerify runs cmoa verify on examples/task-hello against real Docker:
// the reference diff must pass, and every mutant, composed onto the
// reference the way a doctor composes it, must fail. No proposer fleet is
// needed. Skipped unless CMOA_E2E=1.
func TestE2EVerify(t *testing.T) {
	if os.Getenv("CMOA_E2E") != "1" {
		t.Skip("set CMOA_E2E=1 to run against real docker")
	}
	root, _ := os.Getwd()
	bin := build(t, root)
	taskDir := setupExample(t, root)

	// The JSON contract, as a reader outside the module sees it.
	type verification struct {
		SchemaVersion int    `json:"schema_version"`
		Task          string `json:"task"`
		Rev           string `json:"rev"`
		Label         string `json:"label"`
		Status        string `json:"status"`
		ExitCode      int    `json:"exit_code"`
	}
	verify := func(diff, label string) (int, verification) {
		t.Helper()
		cmd := exec.Command(bin, "verify", "--task", taskDir, "--diff", diff, "--label", label)
		cmd.Stderr = os.Stderr
		out, _ := cmd.Output()
		var v verification
		if err := json.Unmarshal(out, &v); err != nil {
			t.Fatalf("verify %s: %v: %s", label, err, out)
		}
		return cmd.ProcessState.ExitCode(), v
	}

	if code, v := verify(filepath.Join(taskDir, "reference.diff"), "reference-1"); code != 0 || v.Status != "pass" {
		t.Fatalf("reference: exit %d, %+v", code, v)
	}
	var manifest struct {
		Mutants []struct {
			Diff   string `json:"diff"`
			Expect string `json:"expect"`
		} `json:"mutants"`
	}
	b, err := os.ReadFile(filepath.Join(taskDir, "task.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Mutants) == 0 {
		t.Fatal("task.json declares no mutants")
	}
	for i, m := range manifest.Mutants {
		if m.Expect != "killed" {
			continue
		}
		label := fmt.Sprintf("mutant-%d", i+1)
		code, v := verify(composeMutant(t, taskDir, m.Diff), label)
		if code != 1 || v.Status != "fail" {
			t.Errorf("%s (%s) survived: exit %d, %+v", label, m.Diff, code, v)
		}
	}
}

func build(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "cmoa")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/cmoa")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	return bin
}

func setupExample(t *testing.T, root string) string {
	t.Helper()
	taskDir := filepath.Join(root, "examples", "task-hello")
	if out, err := exec.Command("sh", filepath.Join(taskDir, "setup.sh")).CombinedOutput(); err != nil {
		t.Fatalf("setup: %v: %s", err, out)
	}
	return taskDir
}

// composeMutant materialises reference.diff followed by one mutant as a
// single diff against rev. A mutant is written against the reference-applied
// tree, so the two cannot simply be concatenated.
func composeMutant(t *testing.T, taskDir, mutant string) string {
	t.Helper()
	repo := filepath.Join(taskDir, "repo")
	rev := strings.TrimSpace(string(git(t, repo, "rev-parse", "HEAD")))
	wt := filepath.Join(t.TempDir(), "compose")
	git(t, repo, "worktree", "add", "--detach", "--quiet", wt, rev)
	defer git(t, repo, "worktree", "remove", "--force", wt)
	git(t, wt, "apply", filepath.Join(taskDir, "reference.diff"))
	git(t, wt, "apply", filepath.Join(taskDir, filepath.FromSlash(mutant)))
	combined := filepath.Join(t.TempDir(), "combined.diff")
	if err := os.WriteFile(combined, git(t, wt, "diff"), 0o644); err != nil {
		t.Fatal(err)
	}
	return combined
}

func git(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}
	return out
}
