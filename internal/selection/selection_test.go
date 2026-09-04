package selection

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
	"github.com/Kaikei-e/CMoA/internal/verify"
)

const goodDiff = "diff --git a/add.go b/add.go\n--- a/add.go\n+++ b/add.go\n@@ -1,3 +1,3 @@\n package add\n \n-func Add(a, b int) int { return a - b }\n+func Add(a, b int) int { return a + b }\n"
const badDiff = "diff --git a/add.go b/add.go\n--- a/add.go\n+++ b/add.go\n@@ -1,3 +1,3 @@\n package add\n \n-func Add(a, b int) int { return a * b }\n+func Add(a, b int) int { return a + b }\n"

// fakeRunner inspects the worktree it is handed and passes when add.go
// contains "a + b". It records concurrency.
type fakeRunner struct {
	mu        sync.Mutex
	seen      []verify.Spec
	inflight  atomic.Int32
	maxSeen   atomic.Int32
	delay     time.Duration
	fail      error
	exitCodes map[string]int
}

func (f *fakeRunner) Run(_ context.Context, s verify.Spec) (*verify.Result, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	n := f.inflight.Add(1)
	for {
		m := f.maxSeen.Load()
		if n <= m || f.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(f.delay)
	f.inflight.Add(-1)
	f.mu.Lock()
	f.seen = append(f.seen, s)
	f.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.CandidateDir, "add.go"))
	if err != nil {
		return nil, err
	}
	code := 1
	if strings.Contains(string(b), "a + b") {
		code = 0
	}
	if f.exitCodes != nil {
		if c, ok := f.exitCodes[filepath.Base(s.CandidateDir)]; ok {
			code = c
		}
	}
	return &verify.Result{ExitCode: code, Command: []string{"fake"}, Stdout: []byte("out")}, nil
}

func fixture(t *testing.T, candidates map[string]struct {
	status trace.CandidateStatus
	diff   string
}) (*config.Config, *task.Task, trace.Dir) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "add.go"), []byte("package add\n\nfunc Add(a, b int) int { return a - b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "task.json"), []byte(`{"version":1,"id":"t","repo":"repo","files":["add.go"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instruction.md"), []byte("fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {verify: {image: x}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tk, err := task.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := tk.ResolveRev(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var props []config.Proposer
	ids := []string{"p1", "p2", "p3"}
	for _, id := range ids {
		if _, ok := candidates[id]; ok {
			props = append(props, config.Proposer{ID: config.ProposerID(id), BaseURL: "http://x", Model: "m"})
		}
	}
	cfg := &config.Config{Version: 1, Proposers: props, Harness: config.Harness{Vault: "v"}, Verify: config.Verify{MaxParallel: 1, TimeoutSeconds: 10}, Selection: config.Selection{Rule: config.RuleFirst}}
	rd, err := trace.Create(dir, trace.NewRunID(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err := rd.WriteRun(&trace.Run{SchemaVersion: 1, RunID: rd.ID(), Task: trace.TaskRef{ID: "t", ResolvedRev: rev}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		c, ok := candidates[id]
		if !ok {
			continue
		}
		if err := rd.WriteCandidate(&trace.Candidate{ProposerID: id, Status: c.status}, []byte("raw"), c.diff); err != nil {
			t.Fatal(err)
		}
	}
	return cfg, tk, rd
}

type cand = struct {
	status trace.CandidateStatus
	diff   string
}

func TestFirstPassingInOrder(t *testing.T) {
	cfg, tk, rd := fixture(t, map[string]cand{
		"p1": {trace.CandidateNoDiff, ""},
		"p2": {trace.CandidateOK, badDiff},
		"p3": {trace.CandidateOK, goodDiff},
	})
	fr := &fakeRunner{}
	sel, err := Run(context.Background(), cfg, tk, rd, Options{Runner: fr})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := sel.(Selected)
	if !ok || s.CandidateID != "p3" {
		t.Fatalf("sel = %#v", sel)
	}
	rec, err := rd.ReadSelect()
	if err != nil {
		t.Fatal(err)
	}
	if rec.Selection.Kind != trace.SelectionSelected || rec.Selection.CandidateID != "p3" || len(rec.AlsoPassed) != 0 || strings.Join(rec.Order, ",") != "p1,p2,p3" {
		t.Fatalf("select.json = %+v", rec)
	}
	// p1 skipped, p2 apply_failed, p3 pass
	for id, want := range map[string]trace.VerifyStatus{"p1": trace.VerifySkipped, "p2": trace.VerifyApplyFailed, "p3": trace.VerifyPass} {
		var r trace.VerifyResult
		b, err := os.ReadFile(rd.VerifyResult(id))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !strings.Contains(string(b), `"status": "`+string(want)+`"`) {
			t.Fatalf("%s: %s", id, b)
		}
		_ = r
	}
	if len(fr.seen) != 1 {
		t.Fatalf("runner should see only p3, saw %d", len(fr.seen))
	}
	if !strings.HasPrefix(fr.seen[0].ProjectName, "cmoa-t-") || fr.seen[0].Timeout != 10*time.Second {
		t.Fatalf("spec = %+v", fr.seen[0])
	}
	// worktree gone
	if _, err := os.Stat(fr.seen[0].CandidateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worktree not removed")
	}
	// second select refused
	if _, err := Run(context.Background(), cfg, tk, rd, Options{Runner: fr}); err == nil {
		t.Fatal("second select must fail")
	}
}

func TestAlsoPassedAndOrder(t *testing.T) {
	cfg, tk, rd := fixture(t, map[string]cand{
		"p1": {trace.CandidateOK, goodDiff},
		"p2": {trace.CandidateOK, goodDiff},
	})
	sel, err := Run(context.Background(), cfg, tk, rd, Options{Runner: &fakeRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	if s := sel.(Selected); s.CandidateID != "p1" {
		t.Fatalf("want p1 first, got %s", s.CandidateID)
	}
	rec, _ := rd.ReadSelect()
	if strings.Join(rec.AlsoPassed, ",") != "p2" {
		t.Fatalf("also_passed = %v", rec.AlsoPassed)
	}
}

func TestNoCandidate(t *testing.T) {
	cfg, tk, rd := fixture(t, map[string]cand{
		"p1": {trace.CandidateHTTPError, ""},
		"p2": {trace.CandidateOK, badDiff},
	})
	sel, err := Run(context.Background(), cfg, tk, rd, Options{Runner: &fakeRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := sel.(NoCandidate); !ok || n.Tried != 2 {
		t.Fatalf("sel = %#v", sel)
	}
}

func TestVerifierFailed(t *testing.T) {
	cfg, tk, rd := fixture(t, map[string]cand{"p1": {trace.CandidateOK, goodDiff}})
	sel, err := Run(context.Background(), cfg, tk, rd, Options{Runner: &fakeRunner{fail: &verify.RunnerError{Stage: "docker binary", Err: errors.New("nope")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sel.(VerifierFailed); !ok {
		t.Fatalf("sel = %#v", sel)
	}
	rec, _ := rd.ReadSelect()
	if rec.Selection.Kind != trace.SelectionVerifierFailed || !strings.Contains(rec.Selection.Error, "nope") {
		t.Fatalf("%+v", rec.Selection)
	}
}

func TestParallelism(t *testing.T) {
	cfg, tk, rd := fixture(t, map[string]cand{
		"p1": {trace.CandidateOK, goodDiff},
		"p2": {trace.CandidateOK, goodDiff},
		"p3": {trace.CandidateOK, goodDiff},
	})
	cfg.Verify.MaxParallel = 3
	fr := &fakeRunner{delay: 150 * time.Millisecond}
	if _, err := Run(context.Background(), cfg, tk, rd, Options{Runner: fr}); err != nil {
		t.Fatal(err)
	}
	if fr.maxSeen.Load() < 2 {
		t.Fatalf("max_parallel 3 should overlap, saw %d", fr.maxSeen.Load())
	}
}

func TestRecordIsExhaustive(t *testing.T) {
	for _, s := range []Selection{Selected{CandidateID: "a"}, NoCandidate{Tried: 1}, JudgeTimeout{After: time.Second}, VerifierFailed{Err: errors.New("x")}} {
		if Record(s).Kind == "" {
			t.Fatalf("empty kind for %#v", s)
		}
	}
}

// A task that names its own verifier timeout overrides the configuration's.
func TestTaskTimeoutOverridesConfig(t *testing.T) {
	cfg, tk, rd := fixture(t, map[string]cand{"p1": {trace.CandidateOK, goodDiff}})
	tk.Verify.TimeoutSeconds = 5
	fr := &fakeRunner{}
	if _, err := Run(context.Background(), cfg, tk, rd, Options{Runner: fr}); err != nil {
		t.Fatal(err)
	}
	if len(fr.seen) != 1 || fr.seen[0].Timeout != 5*time.Second {
		t.Fatalf("spec = %+v", fr.seen)
	}
}
