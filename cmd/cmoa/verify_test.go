package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

const (
	addGo = "package hello\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	// fixDiff turns the seed bug into the sum; it applies to the fixture.
	fixDiff = `--- a/add.go
+++ b/add.go
@@ -1,5 +1,5 @@
 package hello

 func Add(a, b int) int {
-	return a - b
+	return a + b
 }
`
	// staleDiff has context the revision does not carry.
	staleDiff = `--- a/add.go
+++ b/add.go
@@ -1,3 +1,3 @@
 package hello
-	return a * b
+	return a + b
`
	manifestV2 = `{"version":2,"id":"hello","repo":"repo","files":["add.go"],
	"verify":{"compose_file":"compose.yaml","service":"verify","kind":"exit-code"}}`
	manifestV1     = `{"version":1,"id":"hello","repo":"repo","files":["add.go"]}`
	manifestV2Band = `{"version":2,"id":"hello","repo":"repo","files":["add.go"],
	"verify":{"kind":"band"}}`
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// verifyFixture is a task whose repository is a real git repository with one
// commit, plus a fake docker on PATH: `go test ./...` needs no daemon.
func verifyFixture(t *testing.T, manifest string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "task.json"), manifest)
	write(t, filepath.Join(dir, "instruction.md"), "Fix Add.\n")
	write(t, filepath.Join(dir, "compose.yaml"), "services: {verify: {image: x}}\n")
	write(t, filepath.Join(dir, "repo", "add.go"), addGo)
	write(t, filepath.Join(dir, "fix.diff"), fixDiff)
	write(t, filepath.Join(dir, "stale.diff"), staleDiff)
	write(t, filepath.Join(dir, "empty.diff"), "")
	repo := filepath.Join(dir, "repo")
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "seed"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	bin, err := filepath.Abs(filepath.Join("..", "..", "internal", "verify", "testdata", "bin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_DOCKER_LOG", filepath.Join(t.TempDir(), "docker.log"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// runVerify runs the command and decodes the one JSON object it prints.
func runVerify(t *testing.T, args ...string) (int, *trace.Verification, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(append([]string{"verify"}, args...), &out, &errb)
	if out.Len() == 0 {
		return code, nil, errb.String()
	}
	if n := strings.Count(strings.TrimSpace(out.String()), "\n"); n != 0 {
		t.Fatalf("stdout must hold one JSON object, got:\n%s", out.String())
	}
	var v trace.Verification
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", out.String(), err)
	}
	return code, &v, errb.String()
}

func TestVerifyPass(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--label", "reference-1")
	if code != exitOK || v == nil {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if v.SchemaVersion != trace.SchemaVersion || v.Task != "hello" || len(v.Rev) != 40 || len(v.DiffSHA256) != 64 {
		t.Fatalf("%+v", v)
	}
	if v.Status != trace.VerifyPass || v.ExitCode != 0 || v.Label != "reference-1" {
		t.Fatalf("%+v", v)
	}
	if v.ProjectName != "cmoa-hello-verify-reference-1" || len(v.Command) == 0 || v.CMoAVersion == "" {
		t.Fatalf("%+v", v)
	}
	if v.CandidateID != "" || v.FinishedAt.Before(v.StartedAt) {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyFail(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	t.Setenv("FAKE_DOCKER_EXIT", "3")
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"))
	if code != exitVerifyNo || v == nil {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if v.Status != trace.VerifyFail || v.ExitCode != 3 {
		t.Fatalf("%+v", v)
	}
	if len(v.Label) != 8 {
		t.Fatalf("default label = %q", v.Label)
	}
}

func TestVerifyApplyFailed(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "stale.diff"))
	if code != exitVerifyNo || v == nil {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if v.Status != trace.VerifyApplyFailed || v.ApplyError == "" || v.ProjectName != "" {
		t.Fatalf("%+v", v)
	}
	if b, err := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG")); err == nil && len(b) > 0 {
		t.Fatalf("docker ran for a diff that did not apply: %s", b)
	}
}

// An empty diff verifies the revision unchanged: the seed state, which for
// a real task fails. The fake docker passes, which is all this asserts.
func TestVerifyEmptyDiff(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "empty.diff"))
	if code != exitOK || v == nil || v.Status != trace.VerifyPass {
		t.Fatalf("exit %d: %+v: %s", code, v, errb)
	}
}

func TestVerifyOut(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	out := filepath.Join(t.TempDir(), "verification")
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--out", out, "--label", "mutant-1")
	if code != exitOK || v == nil {
		t.Fatalf("exit %d: %s", code, errb)
	}
	var onDisk trace.Verification
	b, err := os.ReadFile(trace.VerificationFile(out))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.Label != v.Label || onDisk.Status != v.Status || onDisk.DiffSHA256 != v.DiffSHA256 {
		t.Fatalf("result.json %+v differs from stdout %+v", onDisk, v)
	}
	for _, name := range []string{"stdout.txt", "stderr.txt"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Error(err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(out, "stdout.txt")); err != nil || string(b) != "test output\n" {
		t.Fatalf("stdout.txt = %q, %v", b, err)
	}
	// A verification directory records one verification.
	code, v, errb = runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--out", out)
	if code != exitUsage || v != nil {
		t.Fatalf("second run into the same --out: exit %d, %+v", code, v)
	}
	if !strings.Contains(errb, "already exists") {
		t.Fatalf("stderr = %q", errb)
	}
}

// The --out pre-flight creates the directory and proves it usable before a
// container is spent on a result that could not be written.
func TestVerifyOutPreflight(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	file := filepath.Join(t.TempDir(), "notadir")
	write(t, file, "")
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--out", file)
	if code != exitUsage || v != nil {
		t.Fatalf("--out on a regular file: exit %d, %+v", code, v)
	}
	if !strings.Contains(errb, "not a directory") {
		t.Fatalf("stderr = %q", errb)
	}
	if b, err := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG")); err == nil && len(b) > 0 {
		t.Fatalf("docker ran before the output directory was proven: %s", b)
	}
	// A directory two levels down is created, not demanded.
	out := filepath.Join(t.TempDir(), "a", "b")
	if code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--out", out); code != exitOK || v == nil {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if _, err := os.Stat(trace.VerificationFile(out)); err != nil {
		t.Fatal(err)
	}
}

// A result that cannot be written is a runner error, and the caller still
// gets its one JSON object.
func TestVerifyOutWriteFails(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	out := filepath.Join(t.TempDir(), "verification")
	// stdout.txt is a non-empty directory, so the atomic rename onto it fails
	// after the verifier has already answered.
	write(t, filepath.Join(out, "stdout.txt", "blocker"), "x")
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--label", "blocked")
	if code != exitOK || v == nil {
		t.Fatalf("control run: exit %d: %s", code, errb)
	}
	code, v, errb = runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--out", out, "--label", "blocked")
	if code != exitVerifyRunner || v == nil {
		t.Fatalf("exit %d, %+v: %s", code, v, errb)
	}
	if v.Status != trace.VerifyRunnerError || v.Error == "" {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyRunnerError(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	// git but no docker: the verifier cannot be run at all.
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	onlyGit := t.TempDir()
	if err := os.Symlink(git, filepath.Join(onlyGit, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", onlyGit)
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--label", "no-docker")
	if code != exitVerifyRunner || v == nil {
		t.Fatalf("exit %d, %+v: %s", code, v, errb)
	}
	if v.Status != trace.VerifyRunnerError || !strings.Contains(v.Error, "docker") {
		t.Fatalf("%+v", v)
	}
}

// --diff - reads stdin. os.Stdin is swapped here rather than plumbed through
// run(), which every other command would have to grow a parameter for.
func TestVerifyDiffFromStdin(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	f, err := os.Open(filepath.Join(dir, "fix.diff"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old }()
	code, v, errb := runVerify(t, "--task", dir, "--diff", "-", "--label", "from-stdin")
	if code != exitOK || v == nil || v.Status != trace.VerifyPass {
		t.Fatalf("exit %d: %+v: %s", code, v, errb)
	}
	// The digest is over the bytes as read, whatever the source.
	sum := sha256.Sum256([]byte(fixDiff))
	if v.DiffSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("diff_sha256 = %s", v.DiffSHA256)
	}
}

// The timeout is the flag, then the task's, then the config's, then none.
func TestVerifyTimeout(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "cmoa.json")
	write(t, cfg, `{"version":1,"proposers":[{"id":"p1","base_url":"http://x","model":"m"}],
	"harness":{"vault":"v"},"verify":{"timeout_seconds":30}}`)
	timed := &task.Task{Verify: task.VerifySpec{TimeoutSeconds: 90}}
	bare := &task.Task{}
	cases := []struct {
		name string
		flag time.Duration
		task *task.Task
		cfg  string
		want time.Duration
	}{
		{"flag over everything", 5 * time.Second, timed, cfg, 5 * time.Second},
		{"task over config", 0, timed, cfg, 90 * time.Second},
		{"config", 0, bare, cfg, 30 * time.Second},
		{"none", 0, bare, "", 0},
		{"task without config", 0, timed, "", 90 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var errb bytes.Buffer
			got, code := verifyTimeout(c.flag, c.task, c.cfg, &errb)
			if code != exitOK || got != c.want {
				t.Fatalf("got %s (exit %d), want %s: %s", got, code, c.want, errb.String())
			}
		})
	}
	// An unreadable or invalid config is a usage error, even with --timeout.
	var errb bytes.Buffer
	if _, code := verifyTimeout(time.Second, bare, filepath.Join(dir, "nope.json"), &errb); code != exitUsage {
		t.Fatalf("missing config: exit %d", code)
	}
}

func TestVerifyBandNotImplemented(t *testing.T) {
	dir := verifyFixture(t, manifestV2Band)
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"))
	if code != exitUsage || v != nil {
		t.Fatalf("exit %d, %+v", code, v)
	}
	if !strings.Contains(errb, "verify.kind band is not implemented") {
		t.Fatalf("stderr = %q", errb)
	}
	if b, err := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG")); err == nil && len(b) > 0 {
		t.Fatalf("docker ran for a band verifier: %s", b)
	}
}

func TestVerifyV1Task(t *testing.T) {
	dir := verifyFixture(t, manifestV1)
	code, v, errb := runVerify(t, "--task", dir, "--diff", filepath.Join(dir, "fix.diff"))
	if code != exitOK || v == nil || v.Status != trace.VerifyPass {
		t.Fatalf("exit %d: %+v: %s", code, v, errb)
	}
}

func TestVerifyUsage(t *testing.T) {
	dir := verifyFixture(t, manifestV2)
	cases := [][]string{
		{},
		{"--task", dir},
		{"--diff", filepath.Join(dir, "fix.diff")},
		{"--task", dir, "--diff", filepath.Join(dir, "nope.diff")},
		{"--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--timeout", "-1s"},
		{"--task", t.TempDir(), "--diff", filepath.Join(dir, "fix.diff")},
		{"--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--config", filepath.Join(dir, "nope.json")},
		// A label names a compose project: it is rejected, never rewritten.
		{"--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--label", "Reference One!"},
		{"--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--label", "../../etc/passwd"},
		{"--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--label", "-leading-dash"},
		{"--task", dir, "--diff", filepath.Join(dir, "fix.diff"), "--label", strings.Repeat("a", 65)},
	}
	for _, args := range cases {
		if code, v, _ := runVerify(t, args...); code != exitUsage || v != nil {
			t.Errorf("%v: exit %d, %+v", args, code, v)
		}
	}
}
