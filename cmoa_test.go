package cmoa

import (
	"slices"
	"testing"
)

func TestAllSurfacesHaveAutonomy(t *testing.T) {
	seen := map[Surface]bool{}
	for _, s := range AllSurfaces() {
		if seen[s] {
			t.Fatalf("duplicate surface %q", s)
		}
		seen[s] = true
		_ = s.Autonomy() // must not panic
	}
	if len(seen) != len(autonomyOf) {
		t.Fatalf("AllSurfaces has %d entries, autonomy table has %d", len(seen), len(autonomyOf))
	}
}

func TestAutonomyTable(t *testing.T) {
	want := map[Surface]Autonomy{
		SurfaceMemory:             AutonomyAutoAccept,
		SurfaceSkill:              AutonomyAutoAccept,
		SurfaceToolDescription:    AutonomyHumanApproval,
		SurfaceMiddleware:         AutonomyHumanApproval,
		SurfaceSubagentConfig:     AutonomyHumanApproval,
		SurfaceSystemPrompt:       AutonomyHumanApproval,
		SurfaceToolImplementation: AutonomyProposeOnly,
	}
	for s, a := range want {
		if got := s.Autonomy(); got != a {
			t.Errorf("%s: autonomy %q, want %q", s, got, a)
		}
	}
}

func TestEditableExcludesProposeOnly(t *testing.T) {
	ed := EditableSurfaces()
	if slices.Contains(ed, SurfaceToolImplementation) {
		t.Fatal("tool-implementation must not be editable")
	}
	if len(ed) != 6 {
		t.Fatalf("want 6 editable surfaces, got %d", len(ed))
	}
}

func TestParseSurface(t *testing.T) {
	if s, err := ParseSurface("skill"); err != nil || s != SurfaceSkill {
		t.Fatalf("ParseSurface(skill) = %q, %v", s, err)
	}
	if _, err := ParseSurface("verifier"); err == nil {
		t.Fatal("verifier is read-only, must not parse as a Surface")
	}
}

func TestAllSurfacesIsACopy(t *testing.T) {
	a := AllSurfaces()
	a[0] = "mutated"
	if AllSurfaces()[0] == "mutated" {
		t.Fatal("AllSurfaces must return a copy")
	}
}

func TestAutonomyPanicsOnUnknown(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = Surface("nope").Autonomy()
}
