package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// fleet is a fake proposer pool and a fake judge behind one handler: the
// proposers answer with the text they are given, and the judge prefers
// whichever candidate block holds `wants`.
type fleet struct {
	t       *testing.T
	answer  string // what p1 says
	other   string // what p2 says; empty means the same as p1
	wants   string
	judge   func(w http.ResponseWriter) bool // nil: answer normally
	mutate  func(*config.Config)             // nil: leave the parsed config alone
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *fleet) proposer(w http.ResponseWriter, r *http.Request) {
	if f.entered != nil {
		f.once.Do(func() { close(f.entered) })
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-r.Context().Done():
			return
		}
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.t.Error(err)
	}
	content := f.answer
	if body.Model == "m2" && f.other != "" {
		content = f.other
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": content}, "finish_reason": "stop"}},
		"usage":   map[string]int{"prompt_tokens": 30, "completion_tokens": 7},
	})
}

func (f *fleet) verdict(w http.ResponseWriter, r *http.Request) {
	if f.judge != nil && f.judge(w) {
		return
	}
	var body struct {
		Messages []llm.Message `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.t.Error(err)
	}
	blocks := strings.Split(body.Messages[len(body.Messages)-1].Content, `<candidate id=`)[1:]
	choice := trace.ChoiceTie
	if len(blocks) == 2 {
		if strings.Contains(blocks[0], f.wants) {
			choice = trace.ChoiceA
		} else if strings.Contains(blocks[1], f.wants) {
			choice = trace.ChoiceB
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": fmt.Sprintf(`{"reason":"r","choice":%q}`, choice)}}},
		"usage":   map[string]int{"prompt_tokens": 200, "completion_tokens": 5},
	})
}

// server builds a fake fleet and returns the handler and the runs
// directory the served tasks are written under.
func server(t *testing.T, f *fleet) (http.Handler, string) {
	s, runs := serverInstance(t, f, Options{})
	return s.Handler(), runs
}

func serverInstance(t *testing.T, f *fleet, opt Options) (*Server, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	prop := httptest.NewServer(http.HandlerFunc(f.proposer))
	judge := httptest.NewServer(http.HandlerFunc(f.verdict))
	t.Cleanup(prop.Close)
	t.Cleanup(judge.Close)

	vault := filepath.Join(dir, "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "docdag.yaml"), []byte("preset: adr\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-q", "-m", "i", "--allow-empty"}} {
		cmd := exec.Command("git", append([]string{"-C", vault}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runs := filepath.Join(dir, "runs")
	docdag, _ := filepath.Abs("../propose/testdata/bin/docdag")
	cfg, err := config.Parse([]byte(`{"version":2,"proposers":[
	   {"id":"p1","base_url":"` + prop.URL + `/v1","model":"m1"},
	   {"id":"p2","base_url":"` + prop.URL + `/v1","model":"m2"}],
	  "harness":{"vault":"` + vault + `","docdag":"` + docdag + `"},
	  "judge":{"base_url":"` + judge.URL + `/v1","model":"j"},
	  "serve":{"runs_dir":"` + runs + `","pool_name":"cmoa"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if f.mutate != nil {
		f.mutate(cfg)
	}
	if opt.Version == "" {
		opt.Version = "test"
	}
	s, err := New(cfg, opt)
	if err != nil {
		t.Fatal(err)
	}
	return s, runs
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const ask = `{"model":"cmoa","messages":[{"role":"user","content":"why?"}],"top_p":0.9}`

func performanceLogs(t *testing.T, lines []string) []performance {
	t.Helper()
	var got []performance
	for _, line := range lines {
		if !strings.HasPrefix(line, "performance ") {
			continue
		}
		var record performance
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "performance ")), &record); err != nil {
			t.Fatal(err)
		}
		got = append(got, record)
	}
	return got
}

func loggedServer(t *testing.T, f *fleet) (*Server, func() []performance) {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	s, _ := serverInstance(t, f, Options{Log: func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}})
	return s, func() []performance {
		mu.Lock()
		defer mu.Unlock()
		return performanceLogs(t, lines)
	}
}

func onePerformance(t *testing.T, records []performance) performance {
	t.Helper()
	if len(records) != 1 {
		t.Fatalf("got %d performance records: %+v", len(records), records)
	}
	return records[0]
}

func TestPerformanceSuccessLinksRequestAndRun(t *testing.T) {
	s, records := loggedServer(t, &fleet{t: t, answer: "a", other: "b", wants: "a"})
	w := post(t, s.Handler(), ask)
	if w.Code != http.StatusOK {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	p := onePerformance(t, records())
	if p.Outcome != performanceSuccess || p.RequestID == "" || p.RunID == "" || p.RunID == p.RequestID ||
		w.Header().Get("X-CMoA-Request-ID") != p.RequestID || p.StartedAt == nil ||
		p.AcceptedAt.IsZero() || p.CompletedAt.IsZero() || p.AcceptedAt.After(*p.StartedAt) || p.StartedAt.After(p.CompletedAt) ||
		p.TotalMS < p.ParseMS+p.QueueMS {
		t.Fatalf("performance %+v, header %q", p, w.Header().Get("X-CMoA-Request-ID"))
	}
}

func TestPerformanceQueueCancellationHasNoRun(t *testing.T) {
	f := &fleet{t: t, answer: "a", other: "b", wants: "a", entered: make(chan struct{}), release: make(chan struct{})}
	s, records := loggedServer(t, f)
	first := make(chan struct{})
	go func() {
		defer close(first)
		_ = post(t, s.Handler(), ask)
	}()
	<-f.entered
	ctx, cancel := context.WithCancel(context.Background())
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Handler().ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", strings.NewReader(ask)))
	}()
	// The first request owns the only semaphore slot, while parsing this
	// small valid request completes well within this interval.
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done
	close(f.release)
	<-first
	var canceled *performance
	for _, p := range records() {
		if p.Outcome == performanceQueueCanceled {
			p := p
			canceled = &p
		}
	}
	if canceled == nil || canceled.RunID != "" || canceled.StartedAt != nil || canceled.CanceledPhase != performanceQueue ||
		w.Header().Get("X-CMoA-Request-ID") != canceled.RequestID {
		t.Fatalf("performance %+v header %q", canceled, w.Header().Get("X-CMoA-Request-ID"))
	}
}

func TestPerformancePreParseCancellationHasNoStartOrRun(t *testing.T) {
	s, records := loggedServer(t, &fleet{t: t, answer: "a", other: "b", wants: "a"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", strings.NewReader(ask)))
	p := onePerformance(t, records())
	if p.Outcome != performanceExecutionCanceled || p.CanceledPhase != performanceParse || p.RunID != "" || p.StartedAt != nil ||
		w.Header().Get("X-CMoA-Request-ID") != p.RequestID {
		t.Fatalf("performance %+v", p)
	}
}

func TestPerformanceInvalidRequestHasNoStartOrRun(t *testing.T) {
	s, records := loggedServer(t, &fleet{t: t, answer: "a", other: "b", wants: "a"})
	w := post(t, s.Handler(), `{"model":"other","messages":[{"role":"user","content":"why?"}]}`)
	p := onePerformance(t, records())
	if w.Code != http.StatusNotFound || p.Outcome != performanceInvalidRequest || p.RunID != "" || p.StartedAt != nil ||
		p.CanceledPhase != "" || w.Header().Get("X-CMoA-Request-ID") != p.RequestID {
		t.Fatalf("status %d performance %+v", w.Code, p)
	}
}

func TestPerformanceExecutionCancellationKeepsTraceLink(t *testing.T) {
	f := &fleet{t: t, answer: "a", other: "b", wants: "a", entered: make(chan struct{}), release: make(chan struct{})}
	s, records := loggedServer(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", strings.NewReader(ask)))
	}()
	<-f.entered
	cancel()
	<-done
	var canceled *performance
	for _, p := range records() {
		if p.CanceledPhase != "" {
			p := p
			canceled = &p
		}
	}
	if canceled == nil || canceled.RunID == "" || canceled.CanceledPhase != performancePropose ||
		canceled.Outcome != performanceExecutionCanceled {
		t.Fatalf("performance %+v", canceled)
	}
	close(f.release)
	if w := post(t, s.Handler(), ask); w.Code != http.StatusOK {
		t.Fatalf("slot was not released: %d: %s", w.Code, w.Body)
	}
}

func TestPerformanceRecordsConcurrentRequestsSeparately(t *testing.T) {
	f := &fleet{t: t, answer: "a", other: "b", wants: "a"}
	f.mutate = func(c *config.Config) { c.Serve.MaxInflight = 2 }
	s, records := loggedServer(t, f)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = post(t, s.Handler(), ask)
		}()
	}
	wg.Wait()
	got := records()
	if len(got) != 2 || got[0].RequestID == got[1].RequestID || got[0].RunID == got[1].RunID ||
		got[0].RunID == "" || got[1].RunID == "" {
		t.Fatalf("performance %+v", got)
	}
}

type errorResponseWriter struct{ header http.Header }

func (w *errorResponseWriter) Header() http.Header       { return w.header }
func (w *errorResponseWriter) WriteHeader(int)           {}
func (w *errorResponseWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type cancelingErrorWriter struct {
	header http.Header
	cancel context.CancelFunc
}

func (w *cancelingErrorWriter) Header() http.Header { return w.header }
func (w *cancelingErrorWriter) WriteHeader(int)     {}
func (w *cancelingErrorWriter) Write([]byte) (int, error) {
	w.cancel()
	return 0, errors.New("write failed after cancellation")
}

func TestPerformanceRecordsResponseWriteError(t *testing.T) {
	s, records := loggedServer(t, &fleet{t: t, answer: "a", other: "b", wants: "a"})
	for _, body := range []string{ask, `{"model":"cmoa","stream":true,"messages":[{"role":"user","content":"why?"}]}`} {
		w := &errorResponseWriter{header: make(http.Header)}
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	}
	got := records()
	if len(got) != 2 {
		t.Fatalf("got %d performance records: %+v", len(got), got)
	}
	for _, p := range got {
		if p.Outcome != performanceWriteError || p.RunID == "" || p.WriteMS < 0 || p.RequestID == "" {
			t.Fatalf("performance %+v", p)
		}
	}
}

func TestPerformanceWriteErrorPreservesWriteCancellation(t *testing.T) {
	s, records := loggedServer(t, &fleet{t: t, answer: "a", other: "b", wants: "a"})
	ctx, cancel := context.WithCancel(context.Background())
	w := &cancelingErrorWriter{header: make(http.Header), cancel: cancel}
	s.Handler().ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", strings.NewReader(ask)))
	p := onePerformance(t, records())
	if p.Outcome != performanceWriteError || p.CanceledPhase != performanceWrite || p.RunID == "" {
		t.Fatalf("performance %+v", p)
	}
}

func TestPerformanceSelectionErrorLinksRun(t *testing.T) {
	f := &fleet{t: t, answer: "a", other: "b", wants: "a"}
	f.judge = func(w http.ResponseWriter) bool {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": "not json"}}}})
		return true
	}
	s, records := loggedServer(t, f)
	w := post(t, s.Handler(), ask)
	var response struct {
		Error struct{ Param string } `json:"error"`
	}
	if w.Code != http.StatusBadGateway || json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	p := onePerformance(t, records())
	if p.Outcome != performanceSelectionError || p.RunID == "" || p.RunID != response.Error.Param {
		t.Fatalf("performance %+v, error %+v", p, response.Error)
	}
}

func TestCompletion(t *testing.T) {
	h, _ := server(t, &fleet{t: t, answer: "because of scattering", other: "no idea", wants: "scattering"})
	w := post(t, h, ask)
	if w.Code != http.StatusOK {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	var got struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role, Content string
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		CMoA struct {
			RunID     string `json:"run_id"`
			Selection struct{ Kind, Reason string }
			Judge     struct {
				Calls                int `json:"calls"`
				SwapConsistentPairs  int `json:"swap_consistent_pairs"`
				InvalidOutputRetries int `json:"invalid_output_retries"`
			}
			Candidates struct{ Asked, OK int }
		} `json:"cmoa"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Object != "chat.completion" || got.Model != "cmoa" || got.Choices[0].Message.Content != "because of scattering" {
		t.Fatalf("%+v", got)
	}
	if !strings.HasPrefix(got.ID, "chatcmpl-") || got.CMoA.RunID == "" {
		t.Errorf("id %q run %q", got.ID, got.CMoA.RunID)
	}
	if got.CMoA.Selection.Kind != "selected" || got.CMoA.Judge.Calls != 2 || got.CMoA.Candidates.Asked != 2 || got.CMoA.Candidates.OK != 2 {
		t.Errorf("cmoa %+v", got.CMoA)
	}
	// Usage sums the proposers and the judge.
	if got.Usage.PromptTokens != 2*30+2*200 || got.Usage.CompletionTokens != 2*7+2*5 ||
		got.Usage.TotalTokens != got.Usage.PromptTokens+got.Usage.CompletionTokens {
		t.Errorf("usage %+v", got.Usage)
	}
	// Which proposer won is in the trace and deliberately not in the body.
	if strings.Contains(w.Body.String(), `"p1"`) || strings.Contains(w.Body.String(), `"p2"`) {
		t.Error("the response must not name the proposer whose answer won")
	}
	if !strings.Contains(w.Body.String(), got.CMoA.RunID) {
		t.Error("the run id must be in the response so the trace can be found")
	}
}

// cmoaBlock is the `cmoa` extension as a client reads it.
type cmoaBlock struct {
	RunID     string `json:"run_id"`
	Selection struct {
		Kind      string   `json:"kind"`
		Reason    string   `json:"reason"`
		Score     *float64 `json:"score"`
		Consensus *struct {
			Normalisation, Agreement string
			Agreed, Of               int
		} `json:"consensus"`
		TieBreak *struct {
			Key   string `json:"key"`
			Among int    `json:"among"`
		} `json:"tie_break"`
	} `json:"selection"`
	Judge struct {
		Calls int `json:"calls"`
	} `json:"judge"`
}

func cmoaOf(t *testing.T, w *httptest.ResponseRecorder) cmoaBlock {
	t.Helper()
	var got struct {
		CMoA cmoaBlock `json:"cmoa"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.CMoA
}

// No candidate is the residual case now: a judge whose answers no parser
// could read is still 502 with the sub-reason, not a 200.
func TestNoCandidateIs502(t *testing.T) {
	unreadable := &fleet{t: t, answer: "an answer", other: "a different answer", wants: "an answer"}
	unreadable.judge = func(w http.ResponseWriter) bool {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "not json, and not json after the retry either"}}},
		})
		return true
	}
	h, _ := server(t, unreadable)
	w := post(t, h, ask)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	var got struct {
		Error struct{ Message, Type, Param, Code string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Type != "no_candidate" || got.Error.Code != string(trace.ReasonInvalidOutput) || got.Error.Param == "" {
		t.Errorf("%+v", got.Error)
	}
	if !strings.Contains(got.Error.Message, "invalid_output") {
		t.Errorf("message %q", got.Error.Message)
	}
}

// Two proposers that say the same thing are answered with 200, no judge
// call, and a consensus block that says how they agreed — in counts, not in
// proposer ids.
func TestConsensusIs200(t *testing.T) {
	h, _ := server(t, &fleet{t: t, answer: "**00101.00**", other: "１０１。", wants: "nothing"})
	w := post(t, h, ask)
	if w.Code != http.StatusOK {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	got := cmoaOf(t, w)
	if got.Selection.Kind != "selected" || got.Judge.Calls != 0 {
		t.Fatalf("cmoa %+v", got.Selection)
	}
	c := got.Selection.Consensus
	if c == nil || c.Agreement != "numeric" || c.Agreed != 2 || c.Of != 2 || c.Normalisation == "" {
		t.Fatalf("consensus %+v", c)
	}
	if !strings.Contains(got.Selection.Reason, "consensus: 2 of 2") {
		t.Errorf("reason %q", got.Selection.Reason)
	}
	if strings.Contains(w.Body.String(), "p1") || strings.Contains(w.Body.String(), "p2") {
		t.Error("the response must not name the proposers")
	}
}

// A judge that calls the pair a tie is answered with 200 too, and the
// tie-break block says which key parted them and how many were tied — never
// which candidates they were.
func TestTieBreakIs200(t *testing.T) {
	h, _ := server(t, &fleet{t: t, answer: "alpha", other: "beta", wants: "nothing"})
	w := post(t, h, ask)
	if w.Code != http.StatusOK {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	got := cmoaOf(t, w)
	tb := got.Selection.TieBreak
	if tb == nil || tb.Key != "hash" || tb.Among != 2 {
		t.Fatalf("tie break %+v", tb)
	}
	if got.Selection.Score == nil || *got.Selection.Score != 0.5 || got.Judge.Calls != 2 {
		t.Errorf("score %v calls %d", got.Selection.Score, got.Judge.Calls)
	}
	// The reason explains the key without naming the candidates it parted.
	if !strings.Contains(got.Selection.Reason, "tie broken by hash") || strings.Contains(got.Selection.Reason, "[") {
		t.Errorf("reason %q", got.Selection.Reason)
	}
}

func TestJudgeFailedIs502AndTimeoutIs504(t *testing.T) {
	broken := &fleet{t: t, answer: "an answer", other: "another", wants: "an answer"}
	broken.judge = func(w http.ResponseWriter) bool {
		http.Error(w, "the judge is loading", http.StatusServiceUnavailable)
		return true
	}
	h, _ := server(t, broken)
	w := post(t, h, ask)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "judge_failed") {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	// A judge that never answers is a 504, and the difference matters: a
	// caller retries a timeout and does not retry a refusal.
	slow := &fleet{t: t, answer: "an answer", other: "another", wants: "an answer"}
	slow.judge = func(http.ResponseWriter) bool {
		time.Sleep(50 * time.Millisecond)
		return true
	}
	slow.mutate = func(c *config.Config) { c.Judge.TimeoutSeconds = 0 }
	h, _ = server(t, slow)
	w = post(t, h, ask)
	if w.Code != http.StatusGatewayTimeout || !strings.Contains(w.Body.String(), "judge_timeout") {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
}

func TestStream(t *testing.T) {
	h, _ := server(t, &fleet{t: t, answer: "streamed answer", other: "not this one", wants: "streamed"})
	w := post(t, h, `{"model":"cmoa","stream":true,"messages":[{"role":"user","content":"why?"}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content type %q", ct)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "data: {") || !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("body %q", body)
	}
	var chunk struct {
		Object  string `json:"object"`
		Choices []struct {
			Delta   *struct{ Role, Content string } `json:"delta"`
			Message *struct{ Content string }       `json:"message"`
		} `json:"choices"`
	}
	line := strings.TrimPrefix(strings.SplitN(body, "\n", 2)[0], "data: ")
	if err := json.Unmarshal([]byte(line), &chunk); err != nil {
		t.Fatal(err)
	}
	if chunk.Object != "chat.completion.chunk" || chunk.Choices[0].Delta.Content != "streamed answer" || chunk.Choices[0].Message != nil {
		t.Fatalf("chunk %+v", chunk)
	}
}

func TestRequestValidation(t *testing.T) {
	h, _ := server(t, &fleet{t: t, answer: "a", wants: "a"})
	for _, tc := range []struct {
		name, body string
		status     int
		errType    string
	}{
		{"an unknown model", `{"model":"gpt-9","messages":[{"role":"user","content":"hi"}]}`, http.StatusNotFound, "invalid_request_error"},
		{"no messages", `{"model":"cmoa","messages":[]}`, http.StatusBadRequest, "invalid_request_error"},
		{"the assistant speaks last", `{"model":"cmoa","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"ho"}]}`, http.StatusBadRequest, "invalid_request_error"},
		{"an unknown role", `{"model":"cmoa","messages":[{"role":"tool","content":"hi"}]}`, http.StatusBadRequest, "invalid_request_error"},
		{"not JSON at all", `not json`, http.StatusBadRequest, "invalid_request_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := post(t, h, tc.body)
			if w.Code != tc.status {
				t.Fatalf("%d: %s", w.Code, w.Body)
			}
			var got struct{ Error struct{ Type string } }
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Error.Type != tc.errType {
				t.Errorf("type %q", got.Error.Type)
			}
		})
	}
}

func TestModels(t *testing.T) {
	h, _ := server(t, &fleet{t: t, answer: "a", wants: "a"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
	var got modelList
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Object != "list" || len(got.Data) != 1 || got.Data[0].ID != "cmoa" || got.Data[0].OwnedBy != "cmoa" {
		t.Fatalf("%+v", got)
	}
}

// Every request leaves a task directory and a full run trace: an answer
// served over HTTP is as reconstructible as one produced by the CLI.
func TestEveryRequestLeavesATrace(t *testing.T) {
	h, runs := server(t, &fleet{t: t, answer: "traceable", other: "a different reply", wants: "traceable"})
	w := post(t, h, ask)
	var got struct {
		CMoA struct {
			RunID string `json:"run_id"`
		} `json:"cmoa"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// The task id is the run id lower-cased, so the task directory, the run
	// directory and the completion id all name the same request.
	base := filepath.Join(runs, got.CMoA.RunID)
	b, err := os.ReadFile(filepath.Join(base, "task.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version int    `json:"version"`
		ID      string `json:"id"`
		Face    string `json:"face"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 3 || manifest.Face != "chat" || manifest.ID != strings.ToLower(got.CMoA.RunID) {
		t.Fatalf("%+v", manifest)
	}
	if _, err := os.Stat(filepath.Join(base, "conversation.json")); err != nil {
		t.Error(err)
	}
	run := trace.Dir(filepath.Join(base, "runs", got.CMoA.RunID))
	if _, err := run.ReadJudge(); err != nil {
		t.Error(err)
	}
	rec, err := run.ReadSelect()
	if err != nil {
		t.Fatal(err)
	}
	if rec.Selection.Kind != trace.SelectionSelected {
		t.Errorf("%+v", rec.Selection)
	}
}

// cmoa serve has no auth: binding anything but loopback is a decision a
// person types, not a default.
func TestCheckListen(t *testing.T) {
	for _, tc := range []struct {
		addr    string
		remote  bool
		wantErr bool
	}{
		{addr: "127.0.0.1:8095"},
		{addr: "[::1]:8095"},
		{addr: "localhost:8095"},
		{addr: "0.0.0.0:8095", wantErr: true},
		{addr: ":8095", wantErr: true},
		{addr: "192.168.1.10:8095", wantErr: true},
		{addr: "0.0.0.0:8095", remote: true},
		{addr: "nonsense", wantErr: true},
	} {
		err := CheckListen(tc.addr, tc.remote)
		if (err != nil) != tc.wantErr {
			t.Errorf("CheckListen(%q, %v) = %v", tc.addr, tc.remote, err)
		}
	}
}

func TestNewRefusesAnUnservableConfig(t *testing.T) {
	cfg, err := config.Parse([]byte(`{"version":2,"proposers":[{"id":"a","base_url":"http://h","model":"m"}],"harness":{"vault":"v"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg, Options{}); err == nil {
		t.Error("a config with no serve block cannot serve")
	}
	cfg.Serve = &config.Serve{}
	if _, err := New(cfg, Options{}); err == nil {
		t.Error("a config with no judge cannot serve the chat face")
	}
}

// A conversation the task would refuse is the caller's mistake: 400, not
// 500. A 500 tells a client to retry a request that can never succeed, and
// the rejected request must not leave a task directory behind either.
func TestOversizedConversationIs400(t *testing.T) {
	h, runs := server(t, &fleet{t: t, answer: "a", other: "b", wants: "a"})
	body, err := json.Marshal(map[string]any{
		"model":    "cmoa",
		"messages": []map[string]string{{"role": "user", "content": strings.Repeat("x", 70000)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := post(t, h, string(body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	var got struct {
		Error struct{ Message, Type, Param string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Type != "invalid_request_error" || got.Error.Param != "messages" {
		t.Errorf("%+v", got.Error)
	}
	if !strings.Contains(got.Error.Message, "max_context_bytes") {
		t.Errorf("the message must name the budget: %q", got.Error.Message)
	}
	entries, err := os.ReadDir(runs)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a rejected request left %d task directories behind", len(entries))
	}
}
