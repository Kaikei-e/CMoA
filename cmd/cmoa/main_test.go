package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSurfaces(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"surfaces"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "tool-implementation  propose-only") || !strings.Contains(out.String(), "verifier             read-only") {
		t.Fatalf("%s", out.String())
	}
	out.Reset()
	if code := run([]string{"surfaces", "--format", "json"}, &out, &errb); code != 0 {
		t.Fatal(code)
	}
	var v struct {
		Surfaces []struct{ Surface, Autonomy string } `json:"surfaces"`
		ReadOnly []string                             `json:"read_only"`
	}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || len(v.Surfaces) != 7 || len(v.ReadOnly) != 3 {
		t.Fatalf("%v %s", err, out.String())
	}
}

func TestUsageErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != exitUsage {
		t.Fatal(code)
	}
	if code := run([]string{"nope"}, &out, &errb); code != exitUsage {
		t.Fatal(code)
	}
	if code := run([]string{"propose"}, &out, &errb); code != exitUsage {
		t.Fatalf("propose without --task: %d", code)
	}
	if code := run([]string{"select", "--task", t.TempDir()}, &out, &errb); code != exitInvalid {
		t.Fatalf("select on empty task dir: %d", code)
	}
	if code := run([]string{"version"}, &out, &errb); code != 0 || out.Len() == 0 {
		t.Fatal("version")
	}
}
