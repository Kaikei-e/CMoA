package cmoa_test

import (
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
	bin := filepath.Join(t.TempDir(), "cmoa")
	build := exec.Command("go", "build", "-o", bin, "./cmd/cmoa")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	taskDir := filepath.Join(root, "examples", "task-hello")
	if out, err := exec.Command("sh", filepath.Join(taskDir, "setup.sh")).CombinedOutput(); err != nil {
		t.Fatalf("setup: %v: %s", err, out)
	}
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
