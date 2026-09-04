package trace

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunIDRoundTrip(t *testing.T) {
	id := NewRunID(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	if _, err := ParseRunID(string(id)); err != nil {
		t.Fatal(err)
	}
	if string(id)[:16] != "20260904T120000Z" {
		t.Fatalf("unexpected prefix in %s", id)
	}
	if _, err := ParseRunID("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateWriteReadLatest(t *testing.T) {
	task := t.TempDir()
	older := RunID("20260904T110000Z-00000000")
	newer := RunID("20260904T120000Z-00000000")
	for _, id := range []RunID{newer, older} { // create out of order on purpose
		d, err := Create(task, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.WriteRun(&Run{SchemaVersion: SchemaVersion, RunID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Create(task, older); err == nil {
		t.Fatal("Create must refuse an existing run")
	}
	latest, err := Latest(task)
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID() != newer {
		t.Fatalf("Latest = %s, want %s", latest.ID(), newer)
	}
	if _, err := Latest(t.TempDir()); !errors.Is(err, ErrNoRuns) {
		t.Fatalf("want ErrNoRuns, got %v", err)
	}
	d, err := Open(string(latest))
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.ReadRun()
	if err != nil || r.RunID != newer {
		t.Fatalf("ReadRun = %+v, %v", r, err)
	}
	if err := d.WriteRun(r); !errors.Is(err, ErrExists) {
		t.Fatalf("second WriteRun must fail with ErrExists, got %v", err)
	}
}

func TestCandidateAndVerifyFiles(t *testing.T) {
	d, err := Create(t.TempDir(), NewRunID(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	c := &Candidate{ProposerID: "p1", Status: CandidateOK, Diff: &DiffStats{Files: []string{"a.go"}}}
	if err := d.WriteCandidate(c, []byte("raw"), "diff --git a/a.go b/a.go\n"); err != nil {
		t.Fatal(err)
	}
	got, err := d.ReadCandidate("p1")
	if err != nil || got.Status != CandidateOK {
		t.Fatalf("ReadCandidate = %+v, %v", got, err)
	}
	diff, err := d.ReadCandidateDiff("p1")
	if err != nil || diff == "" {
		t.Fatalf("ReadCandidateDiff = %q, %v", diff, err)
	}
	// no diff → no .diff file
	if err := d.WriteCandidate(&Candidate{ProposerID: "p2", Status: CandidateNoDiff}, []byte("x"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.CandidateDiff("p2")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("no diff file expected for p2")
	}
	if err := d.WriteVerify(&VerifyResult{CandidateID: "p1", Status: VerifyPass}, []byte("out"), nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(d.VerifyStdout("p1")); string(b) != "out" {
		t.Fatalf("stdout = %q", b)
	}
	if err := d.WriteSelect(&Select{SchemaVersion: SchemaVersion, RunID: d.ID(), Selection: SelectionRecord{Kind: SelectionSelected, CandidateID: "p1"}}); err != nil {
		t.Fatal(err)
	}
	s, err := d.ReadSelect()
	if err != nil || s.Selection.Kind != SelectionSelected {
		t.Fatalf("ReadSelect = %+v, %v", s, err)
	}
	// atomic write leaves no temp files behind
	entries, _ := os.ReadDir(filepath.Join(string(d), "candidates"))
	for _, e := range entries {
		if e.Name()[0] == '.' {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteVerification(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "verification")
	v := &Verification{
		SchemaVersion: SchemaVersion,
		Task:          "hello",
		Rev:           "0123456789abcdef0123456789abcdef01234567",
		DiffSHA256:    "deadbeef",
		Label:         "reference-1",
		VerifyResult:  VerifyResult{Status: VerifyPass, StartedAt: time.Now().UTC()},
		CMoAVersion:   "dev",
	}
	if err := WriteVerification(dir, v, []byte("out"), []byte("err")); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"stdout.txt": "out", "stderr.txt": "err"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != want {
			t.Errorf("%s = %q, %v", name, b, err)
		}
	}
	b, err := os.ReadFile(VerificationFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	// The verification has no candidate; the embedded field is omitted.
	if bytes.Contains(b, []byte("candidate_id")) {
		t.Errorf("result.json = %s", b)
	}
	var got Verification
	if err := json.Unmarshal(b, &got); err != nil || got.Label != "reference-1" || got.Status != VerifyPass {
		t.Fatalf("%+v: %v", got, err)
	}
	// A verification directory records one verification.
	if err := WriteVerification(dir, v, nil, nil); !errors.Is(err, ErrExists) {
		t.Fatalf("second WriteVerification = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "stdout.txt")); string(b) != "out" {
		t.Error("a refused write must not touch stdout.txt")
	}
}
