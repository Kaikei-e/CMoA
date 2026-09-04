package patch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const simple = "diff --git a/add.go b/add.go\n--- a/add.go\n+++ b/add.go\n@@ -1,3 +1,3 @@\n package add\n \n-func Add(a, b int) int { return a - b }\n+func Add(a, b int) int { return a + b }\n"

func TestExtract(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
		err  error
	}{
		"bare":            {simple, simple, nil},
		"fenced diff":     {"Here you go:\n```diff\n" + simple + "```\nHope this helps.", simple, nil},
		"fenced plain":    {"```\n" + simple + "```", simple, nil},
		"crlf":            {strings.ReplaceAll(simple, "\n", "\r\n"), simple, nil},
		"no trailing nl":  {strings.TrimRight(simple, "\n"), simple, nil},
		"prose before":    {"I changed the sign.\n\n" + simple, simple, nil},
		"prose after":     {simple + "\nThat should fix it.\n", simple, nil},
		"unterminated":    {"```diff\n" + simple, simple, nil},
		"two fences":      {"```diff\n" + simple + "```\ntext\n```diff\n" + simple + "```", simple + "\n" + simple, nil},
		"think then diff": {"<think>x</think>\n```diff\n" + simple + "```", simple, nil},
		"no prefixes":     {"--- add.go\n+++ add.go\n@@ -1,3 +1,3 @@\n package add\n \n-func Add(a, b int) int { return a - b }\n+func Add(a, b int) int { return a + b }\n", simple, nil},
		"no git header":   {"--- a/add.go\n+++ b/add.go\n@@ -1,3 +1,3 @@\n package add\n \n-func Add(a, b int) int { return a - b }\n+func Add(a, b int) int { return a + b }\n", simple, nil},
		"timestamps":      {"--- a/add.go\t2026-09-04\n+++ b/add.go\t2026-09-04\n@@ -1,3 +1,3 @@\n package add\n \n-func Add(a, b int) int { return a - b }\n+func Add(a, b int) int { return a + b }\n", simple, nil},
		"new file no hdr": {"--- /dev/null\n+++ new.go\n@@ -0,0 +1 @@\n+package add\n", "diff --git a/new.go b/new.go\nnew file mode 100644\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1 @@\n+package add\n", nil},
		"nothing":         {"I cannot do that.", "", ErrNoDiff},
		"fence no diff":   {"```go\npackage x\n```", "", ErrNoDiff},
	}
	for name, c := range cases {
		got, err := Extract(c.in)
		if !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, want %v", name, err, c.err)
			continue
		}
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", name, got, c.want)
		}
	}
}

func TestComputeStats(t *testing.T) {
	diff := simple +
		"diff --git a/new.go b/new.go\nnew file mode 100644\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,2 @@\n+package add\n+var X = 1\n" +
		"diff --git a/old.go b/old.go\ndeleted file mode 100644\n--- a/old.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-package add\n"
	st, err := ComputeStats(diff)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(st.Files, ",") != "add.go,new.go,old.go" {
		t.Fatalf("files = %v", st.Files)
	}
	if st.Additions != 3 || st.Deletions != 2 || len(st.SHA256) != 64 {
		t.Fatalf("%+v", st)
	}
	// Without diff --git headers (plain unified diff) files still resolve.
	st, err = ComputeStats("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n")
	if err != nil || len(st.Files) != 1 || st.Files[0] != "x.go" {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := ComputeStats("@@ -1 +1 @@\n-a\n+b\n"); !errors.Is(err, ErrNoDiff) {
		t.Fatalf("headerless: %v", err)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "add.go"), []byte("package add\n\nfunc Add(a, b int) int { return a - b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestApply(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	if err := Apply(ctx, dir, simple); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "add.go"))
	if !strings.Contains(string(b), "a + b") {
		t.Fatalf("not applied: %s", b)
	}
	// Wrong hunk counts are forgiven by --recount.
	dir = repo(t)
	bad := strings.Replace(simple, "@@ -1,3 +1,3 @@", "@@ -1,9 +1,9 @@", 1)
	if err := Apply(ctx, dir, bad); err != nil {
		t.Fatalf("recount: %v", err)
	}
	// Context that does not match is an ApplyError with stderr.
	dir = repo(t)
	err := Apply(ctx, dir, strings.Replace(simple, "return a - b", "return a * b", 1))
	ae, ok := errors.AsType[*ApplyError](err)
	if !ok || ae.Stderr == "" {
		t.Fatalf("want ApplyError with stderr, got %v", err)
	}
	// Escaping the directory is refused.
	dir = repo(t)
	esc := "diff --git a/../evil b/../evil\nnew file mode 100644\n--- /dev/null\n+++ b/../evil\n@@ -0,0 +1 @@\n+x\n"
	if err := Apply(ctx, dir, esc); err == nil {
		t.Fatal("path escape must fail")
	}
	if err := Apply(ctx, dir, "  \n"); err == nil {
		t.Fatal("empty diff must fail")
	}
}

func TestApplyWithoutPrefixes(t *testing.T) {
	dir := repo(t)
	d, err := Extract("--- add.go\n+++ add.go\n@@ -1,3 +1,3 @@\n package add\n \n-func Add(a, b int) int { return a - b }\n+func Add(a, b int) int { return a + b }\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), dir, d); err != nil {
		t.Fatal(err)
	}
}

func TestApplyNewAndDelete(t *testing.T) {
	dir := repo(t)
	diff := "diff --git a/new.go b/new.go\nnew file mode 100644\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1 @@\n+package add\n" +
		"diff --git a/add.go b/add.go\ndeleted file mode 100644\n--- a/add.go\n+++ /dev/null\n@@ -1,3 +0,0 @@\n-package add\n-\n-func Add(a, b int) int { return a - b }\n"
	if err := Apply(context.Background(), dir, diff); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.go")); err != nil {
		t.Fatal("new file missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "add.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted file still present")
	}
}
