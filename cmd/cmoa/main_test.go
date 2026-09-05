package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestProposeHarnessFlag(t *testing.T) {
	var out, errb bytes.Buffer
	// A harness directory that is not there is a usage error; the run
	// never starts, so nothing is written anywhere.
	if code := run([]string{"propose", "--task", t.TempDir(), "--harness", filepath.Join(t.TempDir(), "gone")}, &out, &errb); code != exitUsage {
		t.Fatalf("missing --harness dir: %d (%s)", code, errb.String())
	}
	// One that is there but malformed is an input error.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills", "half-done"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "half-done", "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"propose", "--task", t.TempDir(), "--harness", dir}, &out, &errb); code != exitInvalid {
		t.Fatalf("malformed --harness dir: %d (%s)", code, errb.String())
	}
	// An explicitly empty --harness is a usage error, not "no harness": a
	// shell loop expanding an unset variable would otherwise run the
	// baseline arm while the caller believed it ran the candidate.
	if code := run([]string{"propose", "--task", t.TempDir(), "--harness", ""}, &out, &errb); code != exitUsage {
		t.Fatalf("--harness '': %d", code)
	}
	// A temperature outside the range config accepts is refused here too,
	// NaN included: it passes both comparisons and fails at the encoder.
	for _, v := range []string{"3", "-1", "NaN", "Inf"} {
		if code := run([]string{"propose", "--task", t.TempDir(), "--temperature", v}, &out, &errb); code != exitUsage {
			t.Fatalf("--temperature %s: %d", v, code)
		}
	}
	// A temperature of zero is a value, not an absence: it must get past
	// the flag check and reach the task loading below it.
	if code := run([]string{"propose", "--task", t.TempDir(), "--temperature", "0"}, &out, &errb); code != exitInvalid {
		t.Fatalf("--temperature 0 must be accepted as a value: %d", code)
	}
}
