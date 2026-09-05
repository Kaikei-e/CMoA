package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	"empty.diff":         "",
	"mutants/0001.diff":  "--- a/add.go\n+++ b/add.go\n",
	"mutants/0002.diff":  "--- a/add.go\n+++ b/add.go\n",
	"mutants/empty.diff": "  \n",
}

// The reference diff may be an empty file: it says the tree at rev already
// is the reference solution, which is what a task built around an existing
// repository looks like. The file must still exist.
func TestEmptyReference(t *testing.T) {
	const m = `{"version":2,"id":"h","repo":"repo","files":["add.go"],"reference":{"diff":"empty.diff"}}`
	tk, err := Load(withFiles(t, m, v2Diffs))
	if err != nil {
		t.Fatal(err)
	}
	if tk.Reference == nil || tk.Reference.Path != "empty.diff" || tk.Reference.Diff != "" {
		t.Fatalf("reference = %+v", tk.Reference)
	}
	// Whitespace is as empty as nothing at all.
	const ws = `{"version":2,"id":"h","repo":"repo","files":["add.go"],"reference":{"diff":"mutants/empty.diff"}}`
	if _, err := Load(withFiles(t, ws, v2Diffs)); err != nil {
		t.Fatal(err)
	}
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
		"version":                `{"version":4,"id":"h","repo":"repo","files":["add.go"]}`,
		"verify.kind":            `{"version":2,"id":"h","repo":"repo","files":["add.go"],"verify":{"kind":"exitcode"}}`,
		"verify.timeout_seconds": `{"version":2,"id":"h","repo":"repo","files":["add.go"],"verify":{"timeout_seconds":-1}}`,
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
		// A mutant that changes nothing is no defect, so an empty one is
		// still refused; only the reference may be empty.
		{"mutants[0].diff", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"mutants":[{"diff":"mutants/empty.diff"}]}`},
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

// chatDir writes a chat task directory and returns it.
func chatDir(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "task.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const chatManifest = `{"version":3,"id":"c","face":"chat","conversation":"conversation.json",
 "reference":{"answer":"reference.md"},"rubric":"rubric.md","judge":{"allow_tie":false}}`

var chatFiles = map[string]string{
	"conversation.json": `[{"role":"system","content":"be brief"},{"role":"user","content":"hi"},
	                       {"role":"assistant","content":"hello"},{"role":"user","content":"why?"}]`,
	"reference.md": "because\n",
	"rubric.md":    "- is right\n",
}

func TestLoadChat(t *testing.T) {
	var logged []string
	dir := chatDir(t, chatManifest, chatFiles)
	// An instruction.md left over from the coding face is ignored, not
	// refused: a converted task keeps its history.
	if err := os.WriteFile(filepath.Join(dir, "instruction.md"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tk, err := Load(dir, WithLog(func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }))
	if err != nil {
		t.Fatal(err)
	}
	if tk.Face != FaceChat || tk.Repo != "" || tk.Instruction != "" {
		t.Fatalf("%+v", tk)
	}
	if len(tk.Chat.Conversation) != 4 || tk.Chat.Conversation[3].Content != "why?" {
		t.Fatalf("%+v", tk.Chat.Conversation)
	}
	if tk.Chat.ReferenceAnswer != "because\n" || tk.Chat.Rubric != "- is right\n" || tk.Chat.AllowTie {
		t.Errorf("%+v", tk.Chat)
	}
	// The judge's documents are not the proposers' context.
	if got := tk.ContextBytes(); got != len("be brief")+len("hi")+len("hello")+len("why?") {
		t.Errorf("ContextBytes = %d", got)
	}
	if tk.ConversationSHA256() == "" || len(logged) != 1 || !strings.Contains(logged[0], "instruction.md") {
		t.Errorf("log %v", logged)
	}
	// allow_tie defaults to true, and conversation.json is the default path.
	plain, err := Load(chatDir(t, `{"version":3,"id":"c","face":"chat"}`, chatFiles))
	if err != nil {
		t.Fatal(err)
	}
	if !plain.Chat.AllowTie || plain.Chat.ConversationPath != ConversationFile || plain.Chat.Rubric != "" {
		t.Errorf("%+v", plain.Chat)
	}
}

// Each version refuses the fields that do not belong to it, and each face
// refuses the other's. A field that is silently ignored is a task that
// measures something other than what it says.
func TestFaceAndVersionFields(t *testing.T) {
	for _, tc := range []struct{ path, manifest string }{
		{"face", `{"version":2,"id":"h","face":"coding","repo":"repo","files":["add.go"]}`},
		{"conversation", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"conversation":"c.json"}`},
		{"rubric", `{"version":1,"id":"h","repo":"repo","files":["add.go"],"rubric":"r.md"}`},
		{"judge", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"judge":{"allow_tie":true}}`},
		{"reference.answer", `{"version":2,"id":"h","repo":"repo","files":["add.go"],"reference":{"answer":"a.md"}}`},
		{"face", `{"version":3,"id":"h","repo":"repo","files":["add.go"]}`},
		{"face", `{"version":3,"id":"h","face":"chatting"}`},
		{"conversation", `{"version":3,"id":"h","face":"coding","repo":"repo","files":["add.go"],"conversation":"c.json"}`},
		{"judge", `{"version":3,"id":"h","face":"coding","repo":"repo","files":["add.go"],"judge":{}}`},
	} {
		_, err := Load(withFiles(t, tc.manifest, nil))
		ve, ok := errors.AsType[*ValidationError](err)
		if !ok {
			t.Errorf("%s: want ValidationError, got %v", tc.path, err)
			continue
		}
		if ve.Path != tc.path {
			t.Errorf("want %s, got %s: %s", tc.path, ve.Path, ve.Msg)
		}
	}
	// A chat task carries no field that describes a repository.
	for _, tc := range []struct{ path, manifest string }{
		{"repo", `{"version":3,"id":"c","face":"chat","repo":"repo"}`},
		{"rev", `{"version":3,"id":"c","face":"chat","rev":"HEAD"}`},
		{"files", `{"version":3,"id":"c","face":"chat","files":["a.go"]}`},
		{"verify", `{"version":3,"id":"c","face":"chat","verify":{"service":"v"}}`},
		{"mutants", `{"version":3,"id":"c","face":"chat","mutants":[{"diff":"m.diff"}]}`},
		{"doctor", `{"version":3,"id":"c","face":"chat","doctor":{"reference_runs":1}}`},
		{"reference.diff", `{"version":3,"id":"c","face":"chat","reference":{"diff":"r.diff"}}`},
	} {
		_, err := Load(chatDir(t, tc.manifest, chatFiles))
		ve, ok := errors.AsType[*ValidationError](err)
		if !ok {
			t.Errorf("%s: want ValidationError, got %v", tc.path, err)
			continue
		}
		if ve.Path != tc.path {
			t.Errorf("want %s, got %s: %s", tc.path, ve.Path, ve.Msg)
		}
	}
}

func TestConversationErrors(t *testing.T) {
	for _, tc := range []struct{ name, conv string }{
		{"empty array", `[]`},
		{"not an array", `{"role":"user","content":"hi"}`},
		{"an unknown role", `[{"role":"tool","content":"hi"}]`},
		{"empty content", `[{"role":"user","content":"  "}]`},
		{"an unknown field", `[{"role":"user","content":"hi","name":"n"}]`},
		{"the assistant speaks last", `[{"role":"user","content":"hi"},{"role":"assistant","content":"ho"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(chatDir(t, `{"version":3,"id":"c","face":"chat"}`, map[string]string{"conversation.json": tc.conv}))
			ve, ok := errors.AsType[*ValidationError](err)
			if !ok {
				t.Fatalf("want ValidationError, got %v", err)
			}
			if ve.Path != "conversation" {
				t.Errorf("path %s: %s", ve.Path, ve.Msg)
			}
		})
	}
	// A missing conversation, a missing rubric and an empty reference are
	// errors too: a task that names a document must have it.
	for _, tc := range []struct {
		path, manifest string
		files          map[string]string
	}{
		{"conversation", `{"version":3,"id":"c","face":"chat"}`, nil},
		{"rubric", `{"version":3,"id":"c","face":"chat","rubric":"nope.md"}`, chatFiles},
		{"reference.answer", `{"version":3,"id":"c","face":"chat","reference":{"answer":"empty.md"}}`,
			map[string]string{"conversation.json": chatFiles["conversation.json"], "empty.md": "  \n"}},
		{"conversation", `{"version":3,"id":"c","face":"chat","conversation":"../escape.json"}`, chatFiles},
	} {
		_, err := Load(chatDir(t, tc.manifest, tc.files))
		ve, ok := errors.AsType[*ValidationError](err)
		if !ok {
			t.Errorf("%s: want ValidationError, got %v", tc.path, err)
			continue
		}
		if ve.Path != tc.path {
			t.Errorf("want %s, got %s: %s", tc.path, ve.Path, ve.Msg)
		}
	}
}

func TestChatContextBudget(t *testing.T) {
	_, err := Load(chatDir(t, `{"version":3,"id":"c","face":"chat","max_context_bytes":4}`, chatFiles))
	ve, ok := errors.AsType[*ValidationError](err)
	if !ok || ve.Path != "conversation" {
		t.Fatalf("%v", err)
	}
}
