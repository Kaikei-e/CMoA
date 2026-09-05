package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/task"
)

func fixture() *task.Task {
	return &task.Task{
		Instruction: "Fix Add.",
		Files: []task.File{
			{Path: "add.go", Content: "package add\n"},
			{Path: "sub/b.go", Content: "package sub"},
		},
	}
}

func TestBuild(t *testing.T) {
	tk := fixture()
	msgs, err := Build(tk, Harness{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("%+v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "unified diff") || strings.HasSuffix(msgs[0].Content, "\n") {
		t.Fatalf("system: %q", msgs[0].Content)
	}
	u := msgs[1].Content
	for _, want := range []string{"# Task\n\nFix Add.\n", "## add.go\n\n```\npackage add\n```", "## sub/b.go\n\n```\npackage sub\n```", "# Output\n\nOne ```diff block"} {
		if !strings.Contains(u, want) {
			t.Errorf("user prompt missing %q:\n%s", want, u)
		}
	}
	// Determinism.
	again, _ := Build(tk, Harness{})
	if again[1].Content != u {
		t.Fatal("prompt not deterministic")
	}
	if len(Version()) != 16 {
		t.Fatal("version")
	}
}

// An empty harness must render the prompt a run without one renders, byte
// for byte: an edit that adds nothing must measure as nothing.
func TestEmptyHarnessChangesNothing(t *testing.T) {
	tk := fixture()
	none, err := Build(tk, Harness{})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := Build(tk, Harness{Notes: []Note{}, Skills: []Skill{}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range none {
		if none[i].Content != empty[i].Content {
			t.Fatalf("%s message differs:\n%q\n%q", none[i].Role, none[i].Content, empty[i].Content)
		}
	}
	if strings.Contains(none[0].Content, "HARNESS") || strings.Contains(none[1].Content, "# Harness") {
		t.Fatal("an empty harness must not print a heading")
	}
	if !strings.HasPrefix(none[1].Content, "# Task\n") {
		t.Fatalf("user prompt starts %q", none[1].Content[:20])
	}
	if !(Harness{}).Empty() || (Harness{}).HasContext() || (Harness{}).Bytes() != 0 {
		t.Fatal("Empty/HasContext/Bytes")
	}
}

// The golden files were rendered by the templates as they stood before the
// harness reached them. A run given no harness must still send those exact
// bytes: the baseline arm of a paired measurement is the prompt CMoA
// already sent, so changing it silently invalidates every cached baseline.
// Editing a template is allowed; editing it without editing this golden is
// not.
func TestNoHarnessGolden(t *testing.T) {
	msgs, err := Build(fixture(), Harness{})
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"no-harness.system.txt", "no-harness.user.txt"} {
		want, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if msgs[i].Content != string(want) {
			t.Errorf("%s message differs from testdata/%s\n got: %q\nwant: %q", msgs[i].Role, name, msgs[i].Content, want)
		}
	}
}

func TestHarnessBytes(t *testing.T) {
	h := Harness{
		SystemAppendix: "12345\n",
		Notes:          []Note{{Path: "memory/a.md", Body: "123\n"}, {Path: "memory/b.md", Body: "12"}},
		Skills:         []Skill{{Name: "ab", Description: "cde"}},
	}
	// 5 appendix + 3 + 2 note bodies + 2 name + 3 description; the trailing
	// newlines the templates supply themselves do not count.
	if got := h.Bytes(); got != 15 {
		t.Fatalf("Bytes() = %d, want 15", got)
	}
}

func TestBuildWithEverySurface(t *testing.T) {
	h := Harness{
		SystemAppendix: "Prefer table-driven tests.\n",
		Notes: []Note{
			{Path: "memory/00-first.md", Body: "First note.\n"},
			{Path: "memory/10-second.md", Body: "Second note."},
		},
		Skills: []Skill{
			{Name: "emit-unified-diff", Description: "How to write a diff that applies."},
			{Name: "read-before-edit", Description: "Read the file before changing it."},
		},
	}
	msgs, err := Build(fixture(), h)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(msgs[0].Content, "\nHARNESS\n\nPrefer table-driven tests.") {
		t.Fatalf("system tail: %q", msgs[0].Content[len(msgs[0].Content)-80:])
	}
	// The contract still comes first and whole.
	if !strings.HasPrefix(msgs[0].Content, "You are a code-editing engine.") || !strings.Contains(msgs[0].Content, "OUTPUT CONTRACT") {
		t.Fatal("the system contract must be rendered first and verbatim")
	}
	want := "# Harness\n\n## Notes\n\nFirst note.\n\nSecond note.\n\n## Available skills\n\n" +
		"- emit-unified-diff: How to write a diff that applies.\n- read-before-edit: Read the file before changing it.\n\n# Task\n\nFix Add.\n\n# Files\n"
	if !strings.HasPrefix(msgs[1].Content, want) {
		t.Fatalf("user prompt:\n%s", msgs[1].Content)
	}
}

func TestBuildWithOneSurfaceAtATime(t *testing.T) {
	for _, tc := range []struct {
		name       string
		h          Harness
		system     string
		userHas    []string
		userHasNot []string
	}{
		{
			name:       "system prompt only",
			h:          Harness{SystemAppendix: "Be terse."},
			system:     "\nHARNESS\n\nBe terse.",
			userHasNot: []string{"# Harness"},
		},
		{
			name:       "notes only",
			h:          Harness{Notes: []Note{{Path: "memory/a.md", Body: "Only note."}}},
			userHas:    []string{"# Harness\n\n## Notes\n\nOnly note.\n\n# Task\n"},
			userHasNot: []string{"Available skills"},
		},
		{
			name:       "skills only",
			h:          Harness{Skills: []Skill{{Name: "s", Description: "d"}}},
			userHas:    []string{"# Harness\n\n## Available skills\n\n- s: d\n\n# Task\n"},
			userHasNot: []string{"## Notes"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msgs, err := Build(fixture(), tc.h)
			if err != nil {
				t.Fatal(err)
			}
			if tc.system != "" && !strings.HasSuffix(msgs[0].Content, tc.system) {
				t.Errorf("system: %q", msgs[0].Content)
			}
			if tc.system == "" && strings.Contains(msgs[0].Content, "HARNESS") {
				t.Error("system message must not mention the harness")
			}
			for _, w := range tc.userHas {
				if !strings.Contains(msgs[1].Content, w) {
					t.Errorf("user prompt missing %q:\n%s", w, msgs[1].Content)
				}
			}
			for _, w := range tc.userHasNot {
				if strings.Contains(msgs[1].Content, w) {
					t.Errorf("user prompt must not contain %q:\n%s", w, msgs[1].Content)
				}
			}
		})
	}
}
