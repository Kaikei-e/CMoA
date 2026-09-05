// Package serve is CMoA's OpenAI-compatible HTTP face: one endpoint that
// takes a chat completion request, runs the whole chat face behind it —
// every proposer, then the judge — and answers with the selected answer.
//
// It is a thin shell around the CLI's own path, not a second
// implementation. Each request writes a task directory and a full run
// trace, so an answer served over HTTP is as reconstructible as one
// produced by `cmoa propose` and `cmoa select`, and the id of the proposer
// whose answer won is in the trace and deliberately not in the response:
// a client that could see it could learn to ask for it.
//
// A selection that did not happen is an error, not a 200 with an apology.
// No candidate is 502, a judge that ran out of time is 504, and a judge
// that could not be reached is 502 — because a caller that cannot tell
// "nobody answered well" from "here is an answer" will treat the second as
// the first.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/harnessdir"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/selection"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// Options tune a server.
type Options struct {
	AsOf    string          // YYYY-MM-DD; empty means today
	Version string          // cmoa version string for run.json
	Harness *harnessdir.Dir // nil means no harness directory
	Client  *llm.Client     // nil means a default client
	Log     func(format string, args ...any)
	Now     func() time.Time
}

// Server answers /v1/models and /v1/chat/completions.
type Server struct {
	cfg  *config.Config
	opt  Options
	sem  chan struct{}
	once sync.Once
}

// New validates that cfg can serve and returns the server.
func New(cfg *config.Config, opt Options) (*Server, error) {
	if cfg.Serve == nil {
		return nil, errors.New("serve: cmoa.json declares no serve block (version 2 adds it)")
	}
	if cfg.Judge == nil {
		return nil, errors.New("serve: cmoa.json declares no judge; the chat face cannot select without one")
	}
	if opt.Log == nil {
		opt.Log = func(string, ...any) {}
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Client == nil {
		opt.Client = &llm.Client{HTTP: &http.Client{}}
	}
	return &Server{cfg: cfg, opt: opt, sem: make(chan struct{}, cfg.Serve.MaxInflight)}, nil
}

// Handler is the routing table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", s.models)
	mux.HandleFunc("POST /v1/chat/completions", s.completions)
	return mux
}

// CheckListen refuses a non-loopback address unless the caller said so on
// the command line. There is no auth and no TLS here: binding a fleet's
// front door to the network is a decision a person types, not a default.
func CheckListen(addr string, allowRemote bool) error {
	if allowRemote {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("serve: listen %q is not host:port: %w", addr, err)
	}
	if host == "" {
		return fmt.Errorf("serve: listen %q binds every interface; cmoa serve has no auth, so pass --allow-remote to mean it", addr)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if host == "localhost" {
		return nil
	}
	return fmt.Errorf("serve: listen %q is not a loopback address; cmoa serve has no auth, so pass --allow-remote to mean it", addr)
}

// ListenAndServe serves until ctx is done, then shuts down gracefully.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{Addr: s.cfg.Serve.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", s.cfg.Serve.Listen)
	if err != nil {
		return err
	}
	s.opt.Log("listening on http://%s (pool %q, %d in flight, runs under %s)",
		ln.Addr(), s.cfg.Serve.PoolName, s.cfg.Serve.MaxInflight, s.cfg.Serve.RunsDir)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.opt.Log("shutting down")
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}

func (s *Server) models(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, modelList{
		Object: "list",
		Data:   []model{{ID: s.cfg.Serve.PoolName, Object: "model", OwnedBy: "cmoa"}},
	})
}

type modelList struct {
	Object string  `json:"object"`
	Data   []model `json:"data"`
}

type model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// request is the subset of the OpenAI body CMoA acts on. Every other
// top-level field is ignored: a pool of local models cannot honour most of
// them, and refusing a request for carrying `top_p` would break clients
// that always send it.
type request struct {
	Model    string             `json:"model"`
	Messages []task.ConvMessage `json:"messages"`
	Stream   bool               `json:"stream"`
}

func (s *Server) completions(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, s.cfg.Serve.MaxBodyBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, apiError{Message: err.Error(), Type: "invalid_request_error"})
		return
	}
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, apiError{Message: "the body is not a chat completion request: " + err.Error(), Type: "invalid_request_error"})
		return
	}
	s.once.Do(func() {
		s.opt.Log("note: top-level fields other than model, messages and stream are ignored")
	})
	if req.Model != s.cfg.Serve.PoolName {
		writeError(w, http.StatusNotFound, apiError{
			Message: fmt.Sprintf("model %q does not exist; this server serves %q", req.Model, s.cfg.Serve.PoolName),
			Type:    "invalid_request_error", Param: "model", Code: "model_not_found",
		})
		return
	}
	if err := task.ValidateConversation(req.Messages); err != nil {
		writeError(w, http.StatusBadRequest, apiError{Message: err.Error(), Type: "invalid_request_error", Param: "messages"})
		return
	}
	// Checked here rather than left to task.Load, so a conversation nobody
	// can answer does not first leave a task directory behind: every
	// rejected request would otherwise write up to max_body_bytes under
	// runs_dir.
	if n := conversationBytes(req.Messages); n > task.DefaultMaxContextBytes {
		writeError(w, http.StatusBadRequest, apiError{
			Message: fmt.Sprintf("the conversation totals %d bytes, over max_context_bytes %d", n, task.DefaultMaxContextBytes),
			Type:    "invalid_request_error", Param: "messages",
		})
		return
	}

	// One selection at a time by default: the proposers can be asked in
	// parallel by the fleet, but a second judge in flight halves the one
	// accelerator the first is using and makes every latency in the trace
	// a measurement of contention.
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-r.Context().Done():
		return
	}

	out, err := s.answer(r.Context(), req)
	if err != nil {
		s.opt.Log("error: %v", err)
		// A conversation the task refuses is the caller's mistake, not the
		// server's: it must read as 400, or a client retries a request that
		// can never succeed.
		if _, ok := errors.AsType[*task.ValidationError](err); ok || errors.Is(err, propose.ErrContextBudget) {
			writeError(w, http.StatusBadRequest, apiError{Message: err.Error(), Type: "invalid_request_error", Param: "messages"})
			return
		}
		writeError(w, http.StatusInternalServerError, apiError{Message: err.Error(), Type: "internal_error"})
		return
	}
	if out.apiErr != nil {
		writeError(w, out.status, *out.apiErr)
		return
	}
	if req.Stream {
		writeStream(w, out.completion)
		return
	}
	writeJSON(w, http.StatusOK, out.completion)
}

// conversationBytes is what the messages will cost the proposers' context,
// counted the way task.ContextBytes counts it.
func conversationBytes(msgs []task.ConvMessage) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return n
}

// answered is one finished request: either a completion or the error object
// the caller gets instead.
type answered struct {
	completion *completion
	apiErr     *apiError
	status     int
}

func (s *Server) answer(ctx context.Context, req request) (*answered, error) {
	id := trace.NewRunID(s.opt.Now())
	taskDir := filepath.Join(s.cfg.Serve.RunsDir, string(id))
	t, err := s.writeTask(taskDir, id, req.Messages)
	if err != nil {
		return nil, err
	}
	s.opt.Log("%s: %d messages", id, len(req.Messages))

	dir, err := propose.Run(ctx, s.cfg, t, propose.Options{
		AsOf: s.opt.AsOf, RunID: id, Client: s.opt.Client, Version: s.opt.Version,
		Harness: s.opt.Harness, Log: s.opt.Log, Now: s.opt.Now,
	})
	if err != nil {
		return nil, err
	}
	sel, err := selection.RunChat(ctx, s.cfg, t, dir, selection.ChatOptions{
		Client: s.opt.Client, Log: s.opt.Log, Now: s.opt.Now,
	})
	if err != nil {
		return nil, err
	}
	return s.respond(dir, id, sel)
}

// writeTask materialises the request as a task directory, so a served
// answer is a task like any other: it can be re-run, judged again, or added
// to a suite without being transcribed by hand.
func (s *Server) writeTask(dir string, id trace.RunID, msgs []task.ConvMessage) (*task.Task, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	conv, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, task.ConversationFile), append(conv, '\n'), 0o644); err != nil {
		return nil, err
	}
	// A run id is a valid task id once lower-cased, and reusing it means
	// the task directory, the run directory and the completion id all name
	// the same request.
	manifest := map[string]any{
		"version":      3,
		"id":           strings.ToLower(string(id)),
		"face":         string(task.FaceChat),
		"conversation": task.ConversationFile,
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, task.ManifestFile), append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	return task.Load(dir, task.WithLog(s.opt.Log))
}

func (s *Server) respond(dir trace.Dir, id trace.RunID, sel selection.Selection) (*answered, error) {
	run, err := dir.ReadRun()
	if err != nil {
		return nil, err
	}
	rep, err := dir.ReadJudge()
	if err != nil {
		return nil, err
	}
	ext := extension{
		RunID:     string(id),
		Selection: selectionInfo{Kind: string(selection.Record(sel).Kind), Reason: selection.Record(sel).Reason},
		Judge: judgeInfo{
			Calls: 2 * len(rep.Pairs), SwapConsistentPairs: rep.SwapConsistentPairs,
			InvalidOutputRetries: rep.InvalidOutputRetries, LatencyMS: rep.LatencyMS,
		},
	}
	if run.Harness.Render != nil {
		ext.Harness = &harnessInfo{TreeSHA256: run.Harness.Render.TreeSHA256}
	}
	usage := usageBlock{PromptTokens: rep.Usage.PromptTokens, CompletionTokens: rep.Usage.CompletionTokens}
	for _, p := range run.Proposers {
		c, err := dir.ReadCandidate(p.ID)
		if err != nil {
			continue
		}
		ext.Candidates.Asked++
		if c.Status == trace.CandidateOK {
			ext.Candidates.OK++
		}
		usage.PromptTokens += c.Usage.PromptTokens
		usage.CompletionTokens += c.Usage.CompletionTokens
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	switch v := sel.(type) {
	case selection.Selected:
		answer, err := dir.ReadCandidateAnswer(string(v.CandidateID))
		if err != nil {
			return nil, err
		}
		return &answered{status: http.StatusOK, completion: &completion{
			ID: "chatcmpl-" + string(id), Object: "chat.completion",
			Created: s.opt.Now().UTC().Unix(), Model: s.cfg.Serve.PoolName,
			Choices: []choice{{Index: 0, Message: &message{Role: task.RoleAssistant, Content: answer}, FinishReason: "stop"}},
			Usage:   usage, CMoA: ext,
		}}, nil
	case selection.NoCandidate:
		return &answered{status: http.StatusBadGateway, apiErr: &apiError{
			Message: "no candidate was selected: " + string(v.Reason),
			Type:    "no_candidate", Code: string(v.Reason), Param: string(id),
		}}, nil
	case selection.JudgeTimeout:
		return &answered{status: http.StatusGatewayTimeout, apiErr: &apiError{
			Message: "the judge did not answer in time, after " + v.After.String(),
			Type:    "judge_timeout", Code: "judge_timeout", Param: string(id),
		}}, nil
	case selection.JudgeFailed:
		return &answered{status: http.StatusBadGateway, apiErr: &apiError{
			Message: "the judge could not be asked: " + v.Err.Error(),
			Type:    "judge_failed", Code: "judge_failed", Param: string(id),
		}}, nil
	case selection.VerifierFailed:
	}
	return nil, fmt.Errorf("serve: run %s produced a coding-face selection", id)
}

// completion is the OpenAI chat completion object, plus the cmoa extension.
type completion struct {
	ID      string     `json:"id"`
	Object  string     `json:"object"`
	Created int64      `json:"created"`
	Model   string     `json:"model"`
	Choices []choice   `json:"choices"`
	Usage   usageBlock `json:"usage"`
	CMoA    extension  `json:"cmoa"`
}

type choice struct {
	Index        int      `json:"index"`
	Message      *message `json:"message,omitempty"`
	Delta        *message `json:"delta,omitempty"` // a chunk carries a delta instead
	FinishReason string   `json:"finish_reason"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type usageBlock struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// extension is the `cmoa` field: what a client needs to know about how the
// answer was chosen, without learning which proposer wrote it.
type extension struct {
	RunID      string         `json:"run_id"`
	Selection  selectionInfo  `json:"selection"`
	Judge      judgeInfo      `json:"judge"`
	Candidates candidateCount `json:"candidates"`
	Harness    *harnessInfo   `json:"harness,omitempty"`
}

type selectionInfo struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type judgeInfo struct {
	Calls                int   `json:"calls"`
	SwapConsistentPairs  int   `json:"swap_consistent_pairs"`
	InvalidOutputRetries int   `json:"invalid_output_retries"`
	LatencyMS            int64 `json:"latency_ms"`
}

type candidateCount struct {
	Asked int `json:"asked"`
	OK    int `json:"ok"`
}

type harnessInfo struct {
	TreeSHA256 string `json:"tree_sha256"`
}

// apiError is the OpenAI error object.
type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

func readBody(r *http.Request, max int64) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, max))
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return nil, fmt.Errorf("the body is larger than serve.max_body_bytes (%d)", max)
	}
	if err != nil {
		return nil, fmt.Errorf("the body could not be read: %w", err)
	}
	return b, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, e apiError) {
	writeJSON(w, status, struct {
		Error apiError `json:"error"`
	}{e})
}

// writeStream answers a `stream: true` request. The answer is chosen before
// a single token can be sent — the judge cannot compare answers that do not
// exist yet — so the stream is one chunk and a terminator. It exists
// because clients demand the wire format, not because CMoA can stream.
func writeStream(w http.ResponseWriter, c *completion) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	chunk := *c
	chunk.Object = "chat.completion.chunk"
	chunk.Choices = []choice{{
		Index:        0,
		Delta:        &message{Role: task.RoleAssistant, Content: c.Choices[0].Message.Content},
		FinishReason: "stop",
	}}
	b, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
