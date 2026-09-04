package prompt

import (
	"strings"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/task"
)

func TestBuild(t *testing.T) {
	tk := &task.Task{
		Instruction: "Fix Add.",
		Files: []task.File{
			{Path: "add.go", Content: "package add\n"},
			{Path: "sub/b.go", Content: "package sub"},
		},
	}
	msgs, err := Build(tk)
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
	again, _ := Build(tk)
	if again[1].Content != u {
		t.Fatal("prompt not deterministic")
	}
	if len(Version()) != 16 {
		t.Fatal("version")
	}
}
