package propose

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

const goodDiff = "diff --git a/add.go b/add.go\n--- a/add.go\n+++ b/add.go\n@@ -1,3 +1,3 @@\n package add\n \n-func Add(a, b int) int { return a - b }\n+func Add(a, b int) int { return a + b }\n"

func gitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "init", "--allow-empty")
}

func fixture(t *testing.T) (*task.Task, string) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(repo, 0o755))
	must(os.WriteFile(filepath.Join(repo, "add.go"), []byte("package add\n\nfunc Add(a, b int) int { return a - b }\n"), 0o644))
	gitRepo(t, repo)
	must(os.WriteFile(filepath.Join(dir, "task.json"), []byte(`{"version":1,"id":"t","repo":"repo","files":["add.go"]}`), 0o644))
	must(os.WriteFile(filepath.Join(dir, "instruction.md"), []byte("fix\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {verify: {image: x}}\n"), 0o644))
	tk, err := task.Load(dir)
	must(err)
	vault := filepath.Join(dir, "vault")
	must(os.MkdirAll(vault, 0o755))
	must(os.WriteFile(filepath.Join(vault, "docdag.yaml"), []byte("preset: adr\n"), 0o644))
	gitRepo(t, vault)
	return tk, vault
}

func proposer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return s.URL + "/v1"
}

func reply(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": content}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 50, "completion_tokens": 20},
			"timings": map[string]float64{"prompt_ms": 10, "predicted_ms": 100, "predicted_per_second": 200},
		})
	}
}

func TestRunWritesEverything(t *testing.T) {
	tk, vault := fixture(t)
	docdag, _ := filepath.Abs("testdata/bin/docdag")
	var inflight, maxInflight atomic.Int32
	slow := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			n := inflight.Add(1)
			for {
				m := maxInflight.Load()
				if n <= m || maxInflight.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
			inflight.Add(-1)
			next(w, r)
		}
	}
	var seenBody map[string]any
	good := proposer(t, slow(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &seenBody)
		reply("Sure:\n```diff\n"+goodDiff+"```\n")(w, r)
	}))
	prose := proposer(t, slow(reply("I would change the minus to a plus.")))
	broken := proposer(t, slow(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "{not json") }))
	down := proposer(t, slow(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "loading", http.StatusServiceUnavailable) }))
	// Never answers within the proposer's 1s timeout; bounded so that
	// httptest.Server.Close, which waits for handlers, does not hang.
	hang := proposer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})

	cfg, err := config.Parse([]byte(`{"version":1,"proposers":[
	  {"id":"good","base_url":"` + good + `","model":"m1","seed":1,"extra_body":{"chat_template_kwargs":{"enable_thinking":false}}},
	  {"id":"prose","base_url":"` + prose + `","model":"m2"},
	  {"id":"broken","base_url":"` + broken + `","model":"m3"},
	  {"id":"down","base_url":"` + down + `","model":"m4"},
	  {"id":"hang","base_url":"` + hang + `","model":"m5","timeout_seconds":1}
	],"harness":{"vault":"` + vault + `","docdag":"` + docdag + `"}}`))
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	dir, err := Run(context.Background(), cfg, tk, Options{AsOf: "2026-09-04", Version: "test", Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	if maxInflight.Load() < 2 {
		t.Fatalf("proposers must be asked concurrently, max inflight %d", maxInflight.Load())
	}
	run, err := dir.ReadRun()
	if err != nil {
		t.Fatal(err)
	}
	if run.Harness.AsOf != "2026-09-04" || len(run.Harness.At) != 40 || len(run.Harness.Binding) != 1 || run.Harness.DocdagVersion != "v0.3.0-test" {
		t.Fatalf("harness = %+v", run.Harness)
	}
	if run.Byzantine.N != 5 || run.Byzantine.F != 1 || run.Task.ID != "t" || len(run.Task.ResolvedRev) != 40 || run.PromptVersion == "" || run.CMoAVersion != "test" {
		t.Fatalf("run = %+v", run)
	}
	if !strings.Contains(string(run.Config), `"max_tokens": 4096`) && !strings.Contains(string(run.Config), `"max_tokens":4096`) {
		t.Fatalf("effective config not recorded: %s", run.Config)
	}
	want := map[string]trace.CandidateStatus{
		"good": trace.CandidateOK, "prose": trace.CandidateNoDiff, "broken": trace.CandidateMalformed,
		"down": trace.CandidateHTTPError, "hang": trace.CandidateTimeout,
	}
	for id, st := range want {
		c, err := dir.ReadCandidate(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if c.Status != st {
			t.Errorf("%s: status %s, want %s (%s)", id, c.Status, st, c.Error)
		}
		if _, err := os.Stat(dir.PromptFile(id)); err != nil {
			t.Errorf("%s: prompt file missing", id)
		}
	}
	good1, _ := dir.ReadCandidate("good")
	if good1.Diff == nil || good1.Diff.Files[0] != "add.go" || good1.Diff.Additions != 1 || good1.Usage.CompletionTokens != 20 || good1.Timings.TokensPerSecond != 200 || good1.FinishReason != "stop" {
		t.Fatalf("good = %+v diff=%+v", good1, good1.Diff)
	}
	if d, _ := dir.ReadCandidateDiff("good"); d != goodDiff {
		t.Fatalf("diff = %q", d)
	}
	if _, err := os.Stat(dir.CandidateDiff("prose")); err == nil {
		t.Fatal("no diff file expected for prose")
	}
	if seenBody["model"] != "m1" || seenBody["temperature"].(float64) != 0.2 || seenBody["seed"].(float64) != 1 {
		t.Fatalf("request body = %v", seenBody)
	}
	if _, ok := seenBody["chat_template_kwargs"]; !ok {
		t.Fatal("extra_body not forwarded")
	}
	msgs := seenBody["messages"].([]any)
	if len(msgs) != 2 || !strings.Contains(msgs[1].(map[string]any)["content"].(string), "func Add") {
		t.Fatalf("messages = %v", msgs)
	}
}

func TestRunFailsWithoutVault(t *testing.T) {
	tk, _ := fixture(t)
	cfg, _ := config.Parse([]byte(`{"version":1,"proposers":[{"id":"a","base_url":"http://127.0.0.1:1","model":"m"}],"harness":{"vault":"/nonexistent"}}`))
	if _, err := Run(context.Background(), cfg, tk, Options{}); err == nil {
		t.Fatal("missing vault must fail before anything is written")
	}
	if _, err := os.Stat(filepath.Join(tk.Dir, "runs")); err == nil {
		t.Fatal("runs/ must not be created when the harness snapshot fails")
	}
}

func TestExplicitRunID(t *testing.T) {
	tk, vault := fixture(t)
	docdag, _ := filepath.Abs("testdata/bin/docdag")
	p := proposer(t, reply("```diff\n"+goodDiff+"```"))
	cfg, _ := config.Parse([]byte(`{"version":1,"proposers":[{"id":"a","base_url":"` + p + `","model":"m"}],"harness":{"vault":"` + vault + `","docdag":"` + docdag + `"}}`))
	id := trace.RunID("20260904T120000Z-deadbeef")
	dir, err := Run(context.Background(), cfg, tk, Options{RunID: id})
	if err != nil || dir.ID() != id {
		t.Fatalf("dir=%s err=%v", dir, err)
	}
	if _, err := Run(context.Background(), cfg, tk, Options{RunID: id}); err == nil {
		t.Fatal("duplicate run id must fail")
	}
}
