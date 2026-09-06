package propose

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// chatFixture writes a chat task and a vault, and returns both.
func chatFixture(t *testing.T) (*task.Task, string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("task.json", `{"version":3,"id":"c","face":"chat","reference":{"answer":"reference.md"},"rubric":"rubric.md"}`)
	write("conversation.json", `[{"role":"user","content":"why is the sky blue?"}]`)
	write("reference.md", "Rayleigh scattering.\n")
	write("rubric.md", "- names Rayleigh scattering\n")
	tk, err := task.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(dir, "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join("vault", "docdag.yaml"), "preset: adr\n")
	gitRepo(t, vault)
	return tk, vault
}

func chatConfig(t *testing.T, vault string, proposers string) *config.Config {
	t.Helper()
	docdag, _ := filepath.Abs("testdata/bin/docdag")
	cfg, err := config.Parse([]byte(`{"version":2,"proposers":[` + proposers + `],
	  "harness":{"vault":"` + vault + `","docdag":"` + docdag + `"},
	  "judge":{"base_url":"http://127.0.0.1:1/v1","model":"j"}}`))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The chat face sends the conversation, keeps the answer, and records the
// style metadata at the time the answer is written.
func TestRunChatFace(t *testing.T) {
	tk, vault := chatFixture(t)
	var seen struct {
		Messages []llm.Message `json:"messages"`
	}
	answer := "# Why\n\n- Short wavelengths scatter more.\n- **Rayleigh** scattering.\n\n```go\nfmt.Println(1)\n```\n"
	good := proposer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seen)
		reply(answer)(w, r)
	})
	blank := proposer(t, reply("   \n"))
	cfg := chatConfig(t, vault, `{"id":"good","base_url":"`+good+`","model":"m1"},{"id":"blank","base_url":"`+blank+`","model":"m2"}`)

	dir, err := Run(context.Background(), cfg, tk, Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := dir.ReadRun()
	if err != nil {
		t.Fatal(err)
	}
	if run.Face != trace.FaceChat || run.ConversationSHA256 == "" || run.CandidatesOrigin != trace.OriginProposers {
		t.Fatalf("run %+v", run)
	}
	if run.Task.ResolvedRev != "" || run.Task.Repo != "" {
		t.Errorf("a chat run resolves no revision: %+v", run.Task)
	}
	// One system message, then the conversation. The judge's documents
	// never reach a proposer.
	if len(seen.Messages) != 2 || seen.Messages[0].Role != "system" || seen.Messages[1].Content != "why is the sky blue?" {
		t.Fatalf("messages %+v", seen.Messages)
	}
	for _, m := range seen.Messages {
		if strings.Contains(m.Content, "Rayleigh") {
			t.Fatal("the reference answer and the rubric are the judge's, not the proposers'")
		}
	}
	c, err := dir.ReadCandidate("good")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != trace.CandidateOK || c.Face != trace.FaceChat || c.AnswerSHA256 == "" || c.Diff != nil {
		t.Fatalf("candidate %+v", c)
	}
	if c.AnswerBytes != len(strings.TrimSpace(answer)) {
		t.Errorf("answer_bytes %d", c.AnswerBytes)
	}
	m := c.Metadata
	if m == nil || m.TokenLen != 20 || m.HeaderCount != 1 || m.ListCount != 2 || m.BoldCount != 1 || m.CodeFenceCount != 2 {
		t.Errorf("metadata %+v", m)
	}
	got, err := dir.ReadCandidateAnswer("good")
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.TrimSpace(answer) {
		t.Errorf("answer %q", got)
	}
	if _, err := os.Stat(dir.CandidateDiff("good")); err == nil {
		t.Error("the chat face writes no .diff")
	}
	// A proposer that answered nothing is `empty`, which is a different
	// failure from not answering at all.
	if b, err := dir.ReadCandidate("blank"); err != nil || b.Status != trace.CandidateEmpty {
		t.Fatalf("blank = %+v %v", b, err)
	}
	if _, err := os.Stat(dir.CandidateAnswer("blank")); err == nil {
		t.Error("an empty answer writes no .txt")
	}
}

// A chat task against a configuration with no judge is refused before
// anything is spent: answers with nowhere to be selected are answers wasted.
func TestChatNeedsAJudge(t *testing.T) {
	tk, vault := chatFixture(t)
	docdag, _ := filepath.Abs("testdata/bin/docdag")
	cfg, err := config.Parse([]byte(`{"version":1,"proposers":[{"id":"a","base_url":"http://127.0.0.1:1","model":"m"}],
	  "harness":{"vault":"` + vault + `","docdag":"` + docdag + `"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), cfg, tk, Options{}); err == nil || !strings.Contains(err.Error(), "needs a judge") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(trace.RunsRoot(tk.Dir)); err == nil {
		t.Error("nothing must be written")
	}
}

// External candidates make a run with no proposer call in it, and record
// where each answer came from.
func TestExternal(t *testing.T) {
	tk, vault := chatFixture(t)
	cfg := chatConfig(t, vault, `{"id":"unused","base_url":"http://127.0.0.1:1/v1","model":"m"}`)
	files := []ExternalAnswer{
		{File: "a.txt", Text: "first answer\n"},
		{File: "b.txt", Text: "second answer\n"},
		{File: "c.txt", Text: "  \n"},
	}
	dir, err := External(context.Background(), cfg, tk, files, Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := dir.ReadRun()
	if err != nil {
		t.Fatal(err)
	}
	if run.CandidatesOrigin != trace.OriginExternal || len(run.ExternalCandidates) != 3 {
		t.Fatalf("run %+v", run)
	}
	if run.ExternalCandidates[0].ID != "c1" || run.ExternalCandidates[0].File != "a.txt" ||
		run.ExternalCandidates[0].SHA256 != llm.SHA256([]byte("first answer\n")) {
		t.Errorf("external %+v", run.ExternalCandidates[0])
	}
	if len(run.Proposers) != 3 || run.Proposers[2].ID != "c3" {
		t.Errorf("proposers %+v", run.Proposers)
	}
	for _, tc := range []struct {
		id     string
		status trace.CandidateStatus
	}{{"c1", trace.CandidateOK}, {"c2", trace.CandidateOK}, {"c3", trace.CandidateEmpty}} {
		c, err := dir.ReadCandidate(tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if c.Status != tc.status || c.Origin != trace.OriginExternal || c.Face != trace.FaceChat {
			t.Errorf("%s: %+v", tc.id, c)
		}
	}
	if a, _ := dir.ReadCandidateAnswer("c1"); a != "first answer" {
		t.Errorf("answer %q", a)
	}
	// A coding task has no external candidates.
	coding, _ := fixture(t)
	if _, err := External(context.Background(), cfg, coding, files, Options{}); err == nil {
		t.Error("External must refuse the coding face")
	}
}

// A proposer that spends its whole budget on reasoning answers `empty`,
// and the trace must say so: the finish reason, the reasoning tokens and
// the reasoning bytes together tell "thought the budget away" from "said
// nothing", which completion_tokens alone cannot.
func TestReasoningIsAccountedFor(t *testing.T) {
	tk, vault := chatFixture(t)
	thoughtful := proposer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{
				"content": "", "reasoning_content": strings.Repeat("thinking. ", 40),
			}, "finish_reason": "length"}},
			"usage": map[string]any{
				"prompt_tokens": 50, "completion_tokens": 4096,
				"completion_tokens_details": map[string]int{"reasoning_tokens": 4096},
			},
		})
	})
	// A server that does not separate reasoning leaves a think block in the
	// content instead; the same quantity is the difference between the raw
	// and the stripped text.
	inline := proposer(t, reply("<think>weighing it up</think>the answer"))
	cfg := chatConfig(t, vault, `{"id":"thoughtful","base_url":"`+thoughtful+`","model":"m1"},{"id":"inline","base_url":"`+inline+`","model":"m2"}`)
	dir, err := Run(context.Background(), cfg, tk, Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := dir.ReadCandidate("thoughtful")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != trace.CandidateEmpty || c.FinishReason != "length" {
		t.Fatalf("%+v", c)
	}
	if c.Usage.ReasoningTokens != 4096 || c.ReasoningBytes != 400 {
		t.Errorf("reasoning tokens %d, bytes %d", c.Usage.ReasoningTokens, c.ReasoningBytes)
	}
	i, err := dir.ReadCandidate("inline")
	if err != nil {
		t.Fatal(err)
	}
	if i.Status != trace.CandidateOK || i.ReasoningBytes != len("<think>weighing it up</think>") {
		t.Errorf("%+v (reasoning %d)", i, i.ReasoningBytes)
	}
}
