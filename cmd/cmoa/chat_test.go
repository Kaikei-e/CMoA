package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/selection"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

func TestJudgeUsageErrors(t *testing.T) {
	var out, errb bytes.Buffer
	// No --candidate is a usage error before anything is loaded.
	if code := run([]string{"judge", "--task", t.TempDir()}, &out, &errb); code != exitUsage {
		t.Fatalf("judge with no candidate: %d", code)
	}
	if !strings.Contains(errb.String(), "--candidate") {
		t.Errorf("%s", errb.String())
	}
	// A coding task is not judged.
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	for _, p := range []struct{ path, body string }{
		{"task.json", `{"version":1,"id":"h","repo":"repo","files":["a.go"]}`},
		{"instruction.md", "do it\n"},
		{"compose.yaml", "services: {verify: {image: x}}\n"},
		{"cand.txt", "an answer\n"},
	} {
		if err := os.WriteFile(filepath.Join(dir, p.path), []byte(p.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmoa.json"), []byte(
		`{"version":2,"proposers":[{"id":"p","base_url":"http://127.0.0.1:1","model":"m"}],
		  "harness":{"vault":"v"},"judge":{"base_url":"http://127.0.0.1:1","model":"j"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	errb.Reset()
	code := run([]string{"judge", "--task", dir, "--candidate", filepath.Join(dir, "cand.txt")}, &out, &errb)
	if code != exitInvalid || !strings.Contains(errb.String(), "chat answers") {
		t.Fatalf("%d: %s", code, errb.String())
	}
}

// A candidate file that cannot be read, is not text, or holds nothing is an
// input error: nobody asked a model, so there is no candidate to record.
func TestReadCandidates(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, body []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("good.txt", []byte("an answer\n"))
	blank := write("blank.txt", []byte("  \n"))
	binary := write("bin.txt", []byte{0xff, 0xfe, 0x00})
	var errb bytes.Buffer
	if got, code := readCandidates([]string{good}, &errb); code != exitOK || len(got) != 1 || got[0].Text != "an answer\n" {
		t.Fatalf("%d %+v", code, got)
	}
	for _, f := range []string{blank, binary, filepath.Join(dir, "absent.txt")} {
		if _, code := readCandidates([]string{f}, &errb); code != exitInvalid {
			t.Errorf("%s: %d", f, code)
		}
	}
}

// The chat face prints one JSON object, so uzushio reads the sub-reason and
// the ranking rather than parsing prose.
func TestPrintOutcome(t *testing.T) {
	dir := trace.Dir(t.TempDir())
	if err := dir.WriteSelect(&trace.Select{Ranked: []string{"c2", "c1"}}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := printOutcome(&out, dir, selection.Selected{CandidateID: "c2", Reason: "condorcet winner"}); err != nil {
		t.Fatal(err)
	}
	var got outcome
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if got.Kind != "selected" || got.CandidateID != "c2" || got.Answer != dir.CandidateAnswer("c2") ||
		got.Run != string(dir) || got.Judge != dir.JudgeFile() || strings.Join(got.Ranked, ",") != "c2,c1" {
		t.Fatalf("%+v", got)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Error("the outcome must be one line")
	}
	out.Reset()
	got = outcome{}
	if err := printOutcome(&out, dir, selection.NoCandidate{Tried: 3, Reason: trace.ReasonCycle}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "no_candidate" || got.Reason != "cycle" || got.Answer != "" {
		t.Fatalf("%+v", got)
	}
}

func TestServeUsageErrors(t *testing.T) {
	var out, errb bytes.Buffer
	dir := t.TempDir()
	cfg := filepath.Join(dir, "cmoa.json")
	if err := os.WriteFile(cfg, []byte(
		`{"version":2,"proposers":[{"id":"p","base_url":"http://127.0.0.1:1","model":"m"}],
		  "harness":{"vault":"v"},"judge":{"base_url":"http://127.0.0.1:1","model":"j"},"serve":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Binding a non-loopback address needs saying so: the server has no auth.
	if code := run([]string{"serve", "--config", cfg, "--listen", "0.0.0.0:8095"}, &out, &errb); code != exitUsage {
		t.Fatalf("%d: %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "--allow-remote") {
		t.Errorf("%s", errb.String())
	}
	// A version 1 config cannot serve, and says why.
	errb.Reset()
	plain := filepath.Join(dir, "plain.json")
	if err := os.WriteFile(plain, []byte(
		`{"version":1,"proposers":[{"id":"p","base_url":"http://127.0.0.1:1","model":"m"}],"harness":{"vault":"v"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"serve", "--config", plain}, &out, &errb); code != exitInvalid {
		t.Fatalf("%d: %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "serve block") {
		t.Errorf("%s", errb.String())
	}
}

func TestUsageMentionsBothFaces(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"help"}, &out, &errb); code != exitOK {
		t.Fatal(code)
	}
	for _, want := range []string{"cmoa judge", "cmoa serve", "--candidate", "--allow-remote"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage does not mention %q:\n%s", want, out.String())
		}
	}
}

// The refusals --replay-from makes before it reads anything back. Each is a
// way of asking a different question than the one the record answers, and
// answering it with recorded bytes would produce a trace that looks like a
// measurement and is not one.
func TestJudgeReplayRefusals(t *testing.T) {
	dir := t.TempDir()
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "task.json"), `{"version":3,"id":"c","face":"chat"}`)
	write(filepath.Join(dir, "conversation.json"), `[{"role":"user","content":"why?"}]`)
	write(filepath.Join(dir, "cmoa.json"), `{"version":2,
	  "proposers":[{"id":"p","base_url":"http://127.0.0.1:1","model":"m"}],
	  "harness":{"vault":"`+dir+`"},
	  "judge":{"base_url":"http://127.0.0.1:1","model":"j"}}`)
	write(filepath.Join(dir, "cand.txt"), "an answer\n")
	src := filepath.Join(trace.RunsRoot(dir), "20260905T120000Z-abcdef01")
	write(filepath.Join(src, "run.json"),
		`{"schema_version":1,"run_id":"20260905T120000Z-abcdef01","face":"chat","task":{"id":"c"},
		  "proposers":[{"id":"c1"}]}`)
	write(filepath.Join(src, "judge.json"),
		`{"schema_version":1,"run_id":"20260905T120000Z-abcdef01",
		  "judge":{"model":"j","prompt_version":"0000000000000000"},"outcome":{"kind":"no_candidate"}}`)

	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"candidates too", []string{"--candidate", filepath.Join(dir, "cand.txt")}, exitUsage, "--candidate"},
		{"a seed too", []string{"--seed", "3"}, exitUsage, "--seed"},
		{"another prompt", nil, exitInvalid, "another question"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			args := append([]string{"judge", "--task", dir, "--replay-from", src}, tc.args...)
			if code := run(args, &out, &errb); code != tc.code {
				t.Fatalf("exit %d, want %d: %s", code, tc.code, errb.String())
			}
			if !strings.Contains(errb.String(), tc.want) {
				t.Errorf("want %q in\n%s", tc.want, errb.String())
			}
		})
	}
}
