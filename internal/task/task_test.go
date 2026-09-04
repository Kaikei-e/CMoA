package task

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "task.json"), manifest)
	writeFile(t, filepath.Join(dir, "instruction.md"), "Fix add.\n")
	writeFile(t, filepath.Join(dir, "compose.yaml"), "services: {verify: {image: x}}\n")
	writeFile(t, filepath.Join(dir, "repo", "add.go"), "package add\n")
	writeFile(t, filepath.Join(dir, "repo", "sub", "b.go"), "package sub\n")
	return dir
}

const good = `{"version":1,"id":"hello","repo":"repo","files":["add.go","./sub/b.go"]}`

func TestLoad(t *testing.T) {
	dir := fixture(t, good)
	tk, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tk.ID != "hello" || tk.Rev != "HEAD" || tk.MaxContextBytes != DefaultMaxContextBytes {
		t.Fatalf("%+v", tk)
	}
	if tk.Repo != filepath.Join(dir, "repo") || tk.Verify.ComposeFile != filepath.Join(dir, "compose.yaml") || tk.Verify.Service != "verify" {
		t.Fatalf("%+v", tk)
	}
	if got := tk.FilePaths(); len(got) != 2 || got[1] != "sub/b.go" {
		t.Fatalf("files = %v", got)
	}
	if tk.Files[0].Content != "package add\n" {
		t.Fatalf("content = %q", tk.Files[0].Content)
	}
	if len(tk.InstructionSHA256()) != 64 {
		t.Fatal("sha")
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"version":             `{"version":0,"id":"h","repo":"repo","files":["add.go"]}`,
		"id":                  `{"version":1,"id":"H!","repo":"repo","files":["add.go"]}`,
		"repo":                `{"version":1,"id":"h","repo":"nope","files":["add.go"]}`,
		"files":               `{"version":1,"id":"h","repo":"repo","files":[]}`,
		"files[0]":            `{"version":1,"id":"h","repo":"repo","files":["../task.json"]}`,
		"files[1]":            `{"version":1,"id":"h","repo":"repo","files":["add.go","add.go"]}`,
		"verify.compose_file": `{"version":1,"id":"h","repo":"repo","files":["add.go"],"verify":{"compose_file":"missing.yaml"}}`,
	}
	for path, m := range cases {
		_, err := Load(fixture(t, m))
		ve, ok := errors.AsType[*ValidationError](err)
		if !ok {
			t.Errorf("%s: want ValidationError, got %v", path, err)
			continue
		}
		if ve.Path != path {
			t.Errorf("want %s, got %s: %s", path, ve.Path, ve.Msg)
		}
	}
}

// withFiles writes extra files (task-dir relative, forward slashes) into a
// fixture, so a version 2 manifest can declare diffs that exist.
func withFiles(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	dir := fixture(t, manifest)
	for p, c := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(p)), c)
	}
	return dir
}

var v2Diffs = map[string]string{
	"reference.diff":     "--- a/add.go\n+++ b/add.go\n",
	"mutants/0001.diff":  "--- a/add.go\n+++ b/add.go\n",
	"mutants/0002.diff":  "--- a/add.go\n+++ b/add.go\n",
	"mutants/empty.diff": "  \n",
}

func TestLoadV2(t *testing.T) {
	const m = `{"version":2,"id":"hello","repo":"repo","files":["add.go"],
	"verify":{"kind":"band","timeout_seconds":90},
	"reference":{"diff":"reference.diff"},
	"mutants":[{"diff":"mutants/0001.diff","note":"Add subtracts"},
	           {"diff":"./mutants/0002.diff","expect":"equivalent","origin":"generated","operator":"arith"}],
	"doctor":{"kill_rate_min":0.5,"reference_runs":1}}`
	tk, err := Load(withFiles(t, m, v2Diffs))
	if err != nil {
		t.Fatal(err)
	}
	if tk.Verify.Kind != KindBand || tk.Verify.TimeoutSeconds != 90 {
		t.Fatalf("verify = %+v", tk.Verify)
	}
	if tk.Reference == nil || tk.Reference.Path != "reference.diff" || tk.Reference.Diff != v2Diffs["reference.diff"] {
		t.Fatalf("reference = %+v", tk.Reference)
	}
	if len(tk.Mutants) != 2 {
		t.Fatalf("mutants = %+v", tk.Mutants)
	}
	first := Mutant{Path: "mutants/0001.diff", Diff: v2Diffs["mutants/0001.diff"], Expect: ExpectKilled, Origin: OriginHand, Note: "Add subtracts"}
	if tk.Mutants[0] != first {
		t.Fatalf("mutants[0] = %+v", tk.Mutants[0])
	}
	second := tk.Mutants[1]
	if second.Path != "mutants/0002.diff" || second.Expect != ExpectEquivalent || second.Origin != OriginGenerated || second.Operator != "arith" {
		t.Fatalf("mutants[1] = %+v", second)
	}
	if tk.Doctor != (DoctorSpec{KillRateMin: 0.5, ReferenceRuns: 1}) {
		t.Fatalf("doctor = %+v", tk.Doctor)
	}
}

func TestV2Defaults(t *testing.T) {
	const m = `{"version":2,"id":"hello","repo":"repo","files":["add.go"],
	"reference":{"diff":"reference.diff"},"mutants":[{"diff":"mutants/0001.diff"}],"doctor":{}}`
	tk, err := Load(withFiles(t, m, v2Diffs))
	if err != nil {
		t.Fatal(err)
	}
	if tk.Verify.Kind != KindExitCode || tk.Verify.TimeoutSeconds != 0 {
		t.Fatalf("verify = %+v", tk.Verify)
	}
	if tk.Mutants[0].Expect != ExpectKilled || tk.Mutants[0].Origin != OriginHand {
		t.Fatalf("mutants[0] = %+v", tk.Mutants[0])
	}
	if tk.Doctor != (DoctorSpec{KillRateMin: DefaultKillRateMin, ReferenceRuns: DefaultReferenceRuns}) {
		t.Fatalf("doctor = %+v", tk.Doctor)
	}
	// A version 2 task that declares nothing loads, with the doctor defaults.
	bare, err := Load(fixture(t, `{"version":2,"id":"hello","repo":"repo","files":["add.go"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if bare.Reference != nil || len(bare.Mutants) != 0 || bare.Doctor.ReferenceRuns != DefaultReferenceRuns {
		t.Fatalf("%+v", bare)
	}
	// A version 1 task carries the same defaults.
	v1, err := Load(fixture(t, good))
	if err != nil {
		t.Fatal(err)
	}
	if v1.Verify.Kind != KindExitCode || v1.Doctor.KillRateMin != DefaultKillRateMin {
		t.Fatalf("%+v", v1)
	}
}

func TestLoadV2Errors(t *testing.T) {
	cases := map[string]string{
		"version":                `{"version":3,"id":"h","repo":"repo","files":["add.go"]}`,
		"verify.kind":            `{"version":2,"id":"h","repo":"repo","files":["add.go"],"verify":{"kind":"exitcode"}}`,
		"verify.timeout_seconds": `{"version":2,"id":"h","repo":"repo","files":["add.go"],"verify":{"timeout_seconds":-1}}`,
		"reference.diff":         `{"version":2,"id":"h","repo":"repo","files":["add.go"],"reference":{"diff":"mutants/empty.diff"}}`,
		"mutants[0].diff":        `{"version":2,"id":"h","repo":"repo","files":["add.go"],"mutants":[{"diff":"../escape.diff"}]}`,
		"mutants[1].diff":        `{"version":2,"id":"h","repo":"repo","files":["add.go"],"mutants":[{"diff":"mutants/0001.diff"},{"diff":"mutants/0001.diff"}]}`,
		"mutants[0].expect":      `{"version":2,"id":"h","repo":"repo","files":["add.go"],"mutants":[{"diff":"mutants/0001.diff","expect":"dead"}]}`,
		"mutants[1].origin":      `{"version":2,"id":"h","repo":"repo","files":["add.go"],"mutants":[{"diff":"mutants/0001.diff"},{"diff":"mutants/0002.diff","origin":"llm"}]}`,
		"doctor.kill_rate_min":   `{"version":2,"id":"h","repo":"repo","files":["add.go"],"doctor":{"kill_rate_min":1.5}}`,
		"doctor.reference_runs":  `{"version":2,"id":"h","repo":"repo","files":["add.go"],"doctor":{"reference_runs":-2}}`,
	}
	for path, m := range cases {
		_, err := Load(withFiles(t, m, v2Diffs))
		ve, ok := errors.AsType[*ValidationError](err)
		if !ok {
			t.Errorf("%s: want ValidationError, got %v", path, err)
			continue
		}
		if ve.Path != path {
			t.Errorf("want %s, got %s: %s", path, ve.Path, ve.Msg)
		}
	}
	// Several manifests fail at the same path, so they cannot key a map.
	more := []struct{ path, manifest string }{
		// A written 0 is out of range for both thresholds, not "absent".
		{"doctor.kill_rate_min", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"doctor":{"kill_rate_min":0}}`},
		{"doctor.reference_runs", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"doctor":{"reference_runs":0}}`},
		// A missing declared diff is an error, not a skipped mutant.
		{"mutants[0].diff", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"mutants":[{"diff":"mutants/nope.diff"}]}`},
		{"reference.diff", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"reference":{"diff":"../reference.diff"}}`},
		{"reference.diff", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"reference":{"diff":"/etc/passwd"}}`},
		{"reference.diff", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"reference":{"diff":""}}`},
		{"mutants[0].diff", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"mutants":[{"diff":"/etc/passwd"}]}`},
	}
	for _, c := range more {
		_, err := Load(withFiles(t, c.manifest, v2Diffs))
		ve, ok := errors.AsType[*ValidationError](err)
		if !ok || ve.Path != c.path {
			t.Errorf("%s: want ValidationError at %s, got %v", c.manifest, c.path, err)
		}
	}
}

// A version 1 file may not carry version 2 fields. The decoder cannot say
// so (the fields are known to the manifest), so Load refuses them by name.
func TestV1RejectsV2Fields(t *testing.T) {
	cases := map[string]string{
		"verify.kind":            `{"version":1,"id":"h","repo":"repo","files":["add.go"],"verify":{"kind":"exit-code"}}`,
		"verify.timeout_seconds": `{"version":1,"id":"h","repo":"repo","files":["add.go"],"verify":{"timeout_seconds":60}}`,
		"reference":              `{"version":1,"id":"h","repo":"repo","files":["add.go"],"reference":{"diff":"reference.diff"}}`,
		"mutants":                `{"version":1,"id":"h","repo":"repo","files":["add.go"],"mutants":[]}`,
		"doctor":                 `{"version":1,"id":"h","repo":"repo","files":["add.go"],"doctor":{}}`,
	}
	for path, m := range cases {
		_, err := Load(withFiles(t, m, v2Diffs))
		ve, ok := errors.AsType[*ValidationError](err)
		if !ok || ve.Path != path || ve.Msg != "requires version 2" {
			t.Errorf("%s: want %q requires version 2, got %v", path, path, err)
		}
	}
	// An unknown field is still a decode error at any version.
	if _, err := Load(fixture(t, `{"version":2,"id":"h","repo":"repo","files":["add.go"],"mutant":[]}`)); err == nil {
		t.Error("unknown field must fail")
	}
}

func TestContextBudget(t *testing.T) {
	dir := fixture(t, `{"version":1,"id":"h","repo":"repo","files":["add.go"],"max_context_bytes":10}`)
	_, err := Load(dir)
	ve, ok := errors.AsType[*ValidationError](err)
	if !ok || ve.Path != "files" {
		t.Fatalf("want budget error at files, got %v", err)
	}
}

func TestEmptyInstruction(t *testing.T) {
	dir := fixture(t, good)
	writeFile(t, filepath.Join(dir, "instruction.md"), "  \n")
	if _, err := Load(dir); err == nil {
		t.Fatal("empty instruction must fail")
	}
}

func TestResolveRev(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := fixture(t, good)
	repo := filepath.Join(dir, "repo")
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	tk, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := tk.ResolveRev(context.Background())
	if err != nil || len(sha) != 40 {
		t.Fatalf("ResolveRev = %q, %v", sha, err)
	}
	tk.Rev = "nope"
	if _, err := tk.ResolveRev(context.Background()); err == nil {
		t.Fatal("bad rev must fail")
	}
}
