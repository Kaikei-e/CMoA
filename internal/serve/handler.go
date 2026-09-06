package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/task"
)

// request is the subset of the OpenAI body CMoA acts on. Every other
// top-level field is ignored: a pool of local models cannot honour most of
// them, and refusing a request for carrying `top_p` would break clients
// that always send it.
type request struct {
	Model    string             `json:"model"`
	Messages []task.ConvMessage `json:"messages"`
	Stream   bool               `json:"stream"`
}

func (s *Server) models(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, modelList{
		Object: "list",
		Data: []model{{
			ID:      s.cfg.Serve.PoolName,
			Object:  "model",
			OwnedBy: "cmoa",
		}},
	})
}

func (s *Server) completions(w http.ResponseWriter, r *http.Request) {
	req, failure := s.parseCompletionRequest(r)
	if failure != nil {
		writeError(w, failure.status, failure.apiError)
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
		writeRunError(w, err)
		return
	}
	writeCompletion(w, req.Stream, out)
}

type requestFailure struct {
	status   int
	apiError apiError
}

// parseCompletionRequest owns all checks that must happen before a task
// directory is created.
func (s *Server) parseCompletionRequest(r *http.Request) (request, *requestFailure) {
	body, err := readBody(r, s.cfg.Serve.MaxBodyBytes)
	if err != nil {
		return request{}, badRequest(err.Error(), "")
	}
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		return request{}, badRequest("the body is not a chat completion request: "+err.Error(), "")
	}
	s.once.Do(func() {
		s.opt.Log("note: top-level fields other than model, messages and stream are ignored")
	})
	if req.Model != s.cfg.Serve.PoolName {
		return request{}, &requestFailure{
			status: http.StatusNotFound,
			apiError: apiError{
				Message: fmt.Sprintf("model %q does not exist; this server serves %q", req.Model, s.cfg.Serve.PoolName),
				Type:    "invalid_request_error",
				Param:   "model",
				Code:    "model_not_found",
			},
		}
	}
	if err := task.ValidateConversation(req.Messages); err != nil {
		return request{}, badRequest(err.Error(), "messages")
	}
	// Checked here rather than left to task.Load, so rejected requests never
	// leave a task directory behind.
	if n := conversationBytes(req.Messages); n > task.DefaultMaxContextBytes {
		return request{}, badRequest(fmt.Sprintf("the conversation totals %d bytes, over max_context_bytes %d", n, task.DefaultMaxContextBytes), "messages")
	}
	return req, nil
}

func badRequest(message, param string) *requestFailure {
	return &requestFailure{
		status: http.StatusBadRequest,
		apiError: apiError{
			Message: message,
			Type:    "invalid_request_error",
			Param:   param,
		},
	}
}

func writeRunError(w http.ResponseWriter, err error) {
	// A conversation the task refuses is the caller's mistake: clients must
	// not retry it as a server failure.
	if _, ok := errors.AsType[*task.ValidationError](err); ok || errors.Is(err, propose.ErrContextBudget) {
		writeError(w, http.StatusBadRequest, apiError{Message: err.Error(), Type: "invalid_request_error", Param: "messages"})
		return
	}
	writeError(w, http.StatusInternalServerError, apiError{Message: err.Error(), Type: "internal_error"})
}

func conversationBytes(msgs []task.ConvMessage) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content)
	}
	return n
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
