package harnessdir

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/prompt"
)

// write lays out a harness directory from a path -> content map.
func write(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, c := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadEmptyDirectory(t *testing.T) {
	d, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !d.Harness.Empty() || len(d.Files) != 0 {
		t.Fatalf("%+v", d)
	}
	// The digest of nothing is still a digest, and it is stable.
	if d.TreeSHA256 != hex.EncodeToString(sha256.New().Sum(nil)) || len(d.TreeSHA256) != 64 {
		t.Fatalf("tree_sha256 = %q", d.TreeSHA256)
	}
}

func TestLoadMissingDirectory(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(f); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a file is not a harness directory: %v", err)
	}
}

func TestLoadEverySurface(t *testing.T) {
	dir := write(t, map[string]string{
		"system-prompt.md":                 "Prefer table-driven tests.\n",
		"memory/10-second.md":              "Second.\n",
		"memory/00-first.md":               "First.\n",
		"memory/nested/05-in-between.md":   "Nested.\n",
		"memory/.gitkeep":                  "",
		"memory/notes.txt":                 "not markdown, not a note\n",
		"skills/read-before-edit/SKILL.md": "---\nname: read-before-edit\ndescription: Read the file first.\n---\n\n# Body\n\nIgnored.\n",
		"skills/emit-diff/SKILL.md":        "# Emit a diff\n\nEmit exactly one unified diff.\n",
		"hooks.json":                       "{}\n",
	})
	d, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Harness.SystemAppendix != "Prefer table-driven tests." {
		t.Errorf("appendix = %q", d.Harness.SystemAppendix)
	}
	// Sorted by path, and only .md files under memory/ are notes.
	var got []string
	for _, n := range d.Harness.Notes {
		got = append(got, n.Path+"="+n.Body)
	}
	want := []string{"memory/00-first.md=First.", "memory/10-second.md=Second.", "memory/nested/05-in-between.md=Nested."}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("notes = %v, want %v", got, want)
	}
	// Skills are named by their directory, in name order, description only.
	if len(d.Harness.Skills) != 2 ||
		d.Harness.Skills[0] != (prompt.Skill{Name: "emit-diff", Description: "Emit exactly one unified diff."}) ||
		d.Harness.Skills[1] != (prompt.Skill{Name: "read-before-edit", Description: "Read the file first."}) {
		t.Errorf("skills = %+v", d.Harness.Skills)
	}
	// Every file is hashed, rendered or not: hooks.json is a distinct
	// harness state even though CMoA v0 cannot inject it.
	if len(d.Files) != 9 || d.Files[0].Path != "hooks.json" {
		t.Fatalf("files = %+v", d.Files)
	}
	sum := sha256.Sum256([]byte("{}\n"))
	if d.Files[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("hooks.json sha256 = %s", d.Files[0].SHA256)
	}
}

func TestTreeSHA256(t *testing.T) {
	files := map[string]string{"memory/a.md": "A\n", "skills/s/SKILL.md": "d\n"}
	a, err := Load(write(t, files))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(write(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if a.TreeSHA256 != b.TreeSHA256 {
		t.Fatalf("the same tree in two places must hash the same: %s %s", a.TreeSHA256, b.TreeSHA256)
	}
	// The digest is the manifest, so it can be recomputed by hand.
	h := sha256.New()
	for _, f := range a.Files {
		h.Write([]byte(f.Path + "\n" + f.SHA256 + "\n"))
	}
	if a.TreeSHA256 != hex.EncodeToString(h.Sum(nil)) {
		t.Fatal("tree_sha256 is not sha256 over <path>\\n<sha256>\\n per file in path order")
	}
	// Content moves it.
	files["memory/a.md"] = "B\n"
	c, err := Load(write(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if c.TreeSHA256 == a.TreeSHA256 {
		t.Fatal("a changed file must change the digest")
	}
	// A path moves it even when the bytes do not.
	d, err := Load(write(t, map[string]string{"memory/b.md": "A\n", "skills/s/SKILL.md": "d\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if d.TreeSHA256 == a.TreeSHA256 {
		t.Fatal("a renamed file must change the digest")
	}
}

func TestExcludedFromTheTree(t *testing.T) {
	plain, err := Load(write(t, map[string]string{"memory/a.md": "A\n"}))
	if err != nil {
		t.Fatal(err)
	}
	dir := write(t, map[string]string{
		"memory/a.md":     "A\n",
		"render.json":     `{"tree_sha256":"whatever"}`,
		".git/config":     "[core]\n",
		"memory/.git/bad": "x",
	})
	with, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if with.TreeSHA256 != plain.TreeSHA256 || len(with.Files) != 1 {
		t.Fatalf("the renderer's own manifest and .git are not harness content: %+v", with.Files)
	}
}

func TestSkillWithoutSkillFile(t *testing.T) {
	_, err := Load(write(t, map[string]string{"skills/half-done/README.md": "no SKILL.md here\n"}))
	if err == nil || !strings.Contains(err.Error(), "has no SKILL.md") {
		t.Fatalf("err = %v", err)
	}
	// A file directly under skills/ is not a skill and is not refused.
	if _, err := Load(write(t, map[string]string{"skills/.gitkeep": ""})); err != nil {
		t.Fatal(err)
	}
}

func TestSkillWithoutDescription(t *testing.T) {
	_, err := Load(write(t, map[string]string{"skills/mute/SKILL.md": "---\nname: mute\n---\n\n# Mute\n"}))
	if err == nil || !strings.Contains(err.Error(), "no description") {
		t.Fatalf("err = %v", err)
	}
}

func TestDescription(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"frontmatter", "---\nname: a\ndescription: Does a thing.\n---\nbody\n", "Does a thing."},
		{"frontmatter wins over body", "---\ndescription: From frontmatter.\n---\nFrom body.\n", "From frontmatter."},
		{"double quoted", "---\ndescription: \"Quoted: with a colon.\"\n---\n", "Quoted: with a colon."},
		{"single quoted", "---\ndescription: 'It''s quoted.'\n---\n", "It's quoted."},
		{"folded block", "---\ndescription: >\n  One line\n  and another.\nname: a\n---\n", "One line and another."},
		{"literal block", "---\ndescription: |\n  One line\n  and another.\n---\n", "One line and another."},
		{"continued value", "---\ndescription:\n  Wrapped over\n  two lines.\n---\n", "Wrapped over two lines."},
		{"nested key is not the description", "---\nmeta:\n  description: Nested.\n---\nBody line.\n", "Body line."},
		{"no frontmatter", "# Heading\n\n## Another\n\nFirst real line.\nSecond.\n", "First real line."},
		{"body after frontmatter", "---\nname: a\n---\n\n# Heading\n\nThe line.\n", "The line."},
		{"whitespace folded", "---\ndescription:   spaced    out   \n---\n", "spaced out"},
		{"empty", "", ""},
		{"headings only", "# One\n\n## Two\n", ""},
		{"unterminated frontmatter is a body", "---\ndescription: Not closed.\n", "description: Not closed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Description(tc.in); got != tc.want {
				t.Errorf("Description(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The digest is a cross-implementation contract: uzushio's renderer must
// reproduce it from a different codebase. A literal makes it one.
func TestTreeSHA256Golden(t *testing.T) {
	dir := write(t, map[string]string{
		"system-prompt.md":         "Prefer the Go standard library. When a change is small, keep the diff small.\n",
		"memory/00-conventions.md": "Tabs, not spaces: this repository is gofmt-clean.\n",
		"memory/10-verifier.md":    "The verifier runs `go test ./...` in a container. It does not run a formatter.\n",
		"skills/emit-diff/SKILL.md": "---\nname: emit-diff\ndescription: Emit exactly one unified diff, with an @@ line on every hunk.\n---\n\n" +
			"# Emit a diff\n\nThis body is not rendered into the prompt.\n",
		"skills/read-before-edit/SKILL.md": "# Read before edit\n\nReproduce context lines byte for byte from the files you were given.\n",
		"render.json":                      "{\"renderer_version\":\"fixture\",\"as_of\":\"2026-09-05\"}\n",
	})
	d, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	const golden = "433d75fe25fa1df9266fe6f228bf8c51dc4f3a0c3fd18bf3a82e7498f9256fb4"
	if d.TreeSHA256 != golden {
		t.Fatalf("tree_sha256 = %s, want %s (the manifest format is a contract; changing it is a schema change)", d.TreeSHA256, golden)
	}
	// 76 appendix + 49 + 78 note bodies + 154 of skill names and
	// descriptions: what run.json records as harness.render.rendered_bytes.
	if d.Harness.Bytes() != 357 {
		t.Errorf("rendered bytes = %d, want 357", d.Harness.Bytes())
	}
	// Load absolutises the path, as config.Load does for the vault.
	if !filepath.IsAbs(d.Path) {
		t.Errorf("Path = %q, want absolute", d.Path)
	}
}

func TestRelativePathIsAbsolutised(t *testing.T) {
	dir := write(t, map[string]string{"memory/a.md": "A\n"})
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, dir)
	if err != nil {
		t.Skip("no relative path to the temp dir")
	}
	d, err := Load(rel)
	if err != nil {
		t.Fatal(err)
	}
	if d.Path != dir {
		t.Fatalf("Path = %q, want %q", d.Path, dir)
	}
}

func TestNonUTF8IsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory", "bin.md"), []byte{0xff, 0xfe, '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}
	// The digest commits to bytes the request body could not carry: the
	// JSON encoder would replace them, so two trees would send one prompt.
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptySkillDirectoryIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, dir string }{
		{"empty skill directory", "skills/lonely"},
		{"SKILL.md is an empty directory", "skills/odd/SKILL.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(tc.dir)), 0o755); err != nil {
				t.Fatal(err)
			}
			// An empty directory never reaches the digest, so the refusal
			// cannot be left to the renderer comparison.
			if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "has no SKILL.md") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestSkillNameIsValidated(t *testing.T) {
	// A directory name may hold a newline; a rendered listing is one line
	// per skill, so a name that can write its own lines is refused.
	_, err := Load(write(t, map[string]string{"skills/x\n- fake: injected/SKILL.md": "description here\n"}))
	if err == nil || !strings.Contains(err.Error(), "skill name") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Load(write(t, map[string]string{"skills/Upper/SKILL.md": "d\n"})); err == nil {
		t.Fatal("an upper-case skill name must be refused")
	}
	for _, ok := range []string{"a", "emit-diff", "read.before_edit", "s9"} {
		if _, err := Load(write(t, map[string]string{"skills/" + ok + "/SKILL.md": "d\n"})); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

func TestSymlinksAreRefusedNotFollowed(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("not harness content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "memory", "link.md")); err != nil {
		t.Skip("symlinks not supported here")
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a symlink must be refused, not followed: %v", err)
	}
}

func TestGitIsNotHarnessContent(t *testing.T) {
	plain, err := Load(write(t, map[string]string{"memory/a.md": "A\n"}))
	if err != nil {
		t.Fatal(err)
	}
	// A worktree or submodule leaves .git as a one-line file, not a
	// directory; neither is harness content.
	with, err := Load(write(t, map[string]string{"memory/a.md": "A\n", ".git": "gitdir: elsewhere\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if with.TreeSHA256 != plain.TreeSHA256 || len(with.Files) != 1 {
		t.Fatalf("files = %+v", with.Files)
	}
}

// The renderer owns line endings: CRLF reaches the prompt as it was written.
func TestCRLFIsRenderedVerbatim(t *testing.T) {
	d, err := Load(write(t, map[string]string{"memory/a.md": "one\r\ntwo\r\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Harness.Notes) != 1 || d.Harness.Notes[0].Body != "one\r\ntwo\r" {
		t.Fatalf("note = %q", d.Harness.Notes)
	}
}
