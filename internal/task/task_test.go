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
