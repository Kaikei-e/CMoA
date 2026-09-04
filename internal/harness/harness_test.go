package harness

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func vault(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "docdag.yaml"), []byte("preset: adr\n"), 0o644)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func fakeDocdag(t *testing.T) string {
	t.Helper()
	abs, _ := filepath.Abs("testdata/bin/docdag")
	t.Setenv("FAKE_DOCDAG_LOG", filepath.Join(t.TempDir(), "log"))
	return abs
}

func TestTake(t *testing.T) {
	v := vault(t)
	bin := fakeDocdag(t)
	s, err := Take(context.Background(), v, bin, "2026-09-04")
	if err != nil {
		t.Fatal(err)
	}
	if s.AsOf != "2026-09-04" || len(s.At) != 40 || s.DocdagVersion != "v0.3.0-test" {
		t.Fatalf("%+v", s)
	}
	if len(s.Binding) != 1 || s.Binding[0].ID != "0001" || s.Binding[0].Path != "docs/adr/0001-t.md" {
		t.Fatalf("binding = %+v", s.Binding)
	}
	log, _ := os.ReadFile(os.Getenv("FAKE_DOCDAG_LOG"))
	if !strings.Contains(string(log), "query --binding --fields id,title,status,path --format json --as-of 2026-09-04") {
		t.Fatalf("unexpected docdag argv: %s", log)
	}
}

func TestDirtyVault(t *testing.T) {
	v := vault(t)
	bin := fakeDocdag(t)
	os.WriteFile(filepath.Join(v, "x.md"), []byte("x"), 0o644)
	s, err := Take(context.Background(), v, bin, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(s.At, "-dirty") {
		t.Fatalf("At = %q, want -dirty suffix", s.At)
	}
	if len(s.AsOf) != 10 {
		t.Fatalf("AsOf defaulted to %q", s.AsOf)
	}
}

func TestErrors(t *testing.T) {
	bin := fakeDocdag(t)
	ctx := context.Background()
	if _, err := Take(ctx, t.TempDir(), bin, ""); !errors.Is(err, ErrNoVault) {
		t.Fatalf("missing docdag.yaml: %v", err)
	}
	if _, err := Take(ctx, filepath.Join(t.TempDir(), "nope"), bin, ""); !errors.Is(err, ErrNoVault) {
		t.Fatalf("missing dir: %v", err)
	}
	v := vault(t)
	if _, err := Take(ctx, v, bin, "yesterday"); err == nil {
		t.Fatal("bad as_of must fail")
	}
	if _, err := Take(ctx, v, "/no/such/docdag", ""); err == nil {
		t.Fatal("missing binary must fail")
	}
	t.Setenv("FAKE_DOCDAG_FAIL", "1")
	_, err := Take(ctx, v, bin, "")
	e, ok := errors.AsType[*Error](err)
	if !ok || !strings.Contains(e.Stderr, "boom") {
		t.Fatalf("want *Error with stderr, got %v", err)
	}
}

// TestRealDocdag runs against the uzushio vault when both are present.
func TestRealDocdag(t *testing.T) {
	v := os.Getenv("CMOA_TEST_VAULT")
	if v == "" {
		t.Skip("set CMOA_TEST_VAULT to a DocDag vault")
	}
	s, err := Take(context.Background(), v, "docdag", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("docdag %s at %s: %d binding docs", s.DocdagVersion, s.At, len(s.Binding))
}
