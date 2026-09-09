package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
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
	_ = writeJSON(w, http.StatusOK, modelList{
		Object: "list",
		Data: []model{{
			ID:      s.cfg.Serve.PoolName,
			Object:  "model",
			OwnedBy: "cmoa",
		}},
	})
}

func (s *Server) completions(w http.ResponseWriter, r *http.Request) {
	accepted := time.Now()
	requestID := "req_" + string(trace.NewRunID(s.opt.Now()))
	w.Header().Set("X-CMoA-Request-ID", requestID)
	perf := performance{RequestID: requestID, AcceptedAt: accepted.UTC()}
	defer func() {
		perf.CompletedAt = time.Now().UTC()
		perf.TotalMS = elapsedMS(accepted)
		s.logPerformance(perf)
	}()

	parsed := time.Now()
	req, failure := s.parseCompletionRequest(r)
	perf.ParseMS = elapsedMS(parsed)
	if r.Context().Err() != nil {
		perf.Outcome = performanceExecutionCanceled
		perf.CanceledPhase = performanceParse
		return
	}
	if failure != nil {
		perf.Outcome = performanceInvalidRequest
		if err := writeError(w, failure.status, failure.apiError); err != nil {
			perf.Outcome = performanceWriteError
		}
		return
	}
	// One selection at a time by default: the proposers can be asked in
	// parallel by the fleet, but a second judge in flight halves the one
	// accelerator the first is using and makes every latency in the trace
	// a measurement of contention.
	queued := time.Now()
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-r.Context().Done():
		perf.Outcome = performanceQueueCanceled
		perf.CanceledPhase = performanceQueue
		perf.QueueMS = elapsedMS(queued)
		return
	}
	startedAt := time.Now().UTC()
	perf.StartedAt = &startedAt
	perf.QueueMS = elapsedMS(queued)

	out, err := s.answer(r.Context(), req, &perf)
	if err != nil {
		s.opt.Log("error: %v", err)
		if errors.Is(r.Context().Err(), context.Canceled) || errors.Is(r.Context().Err(), context.DeadlineExceeded) {
			perf.Outcome = performanceExecutionCanceled
			perf.CanceledPhase = perf.Phase
		} else {
			perf.Outcome = performanceRunError
		}
		perf.Phase = performanceWrite
		started := time.Now()
		if writeErr := writeRunError(w, err); writeErr != nil {
			perf.Outcome = performanceWriteError
		}
		perf.WriteMS = elapsedMS(started)
		perf.noteCancellation(r.Context())
		perf.Phase = ""
		return
	}
	if r.Context().Err() != nil {
		perf.Outcome = performanceExecutionCanceled
		if perf.CanceledPhase == "" {
			perf.CanceledPhase = perf.Phase
		}
		return
	}
	started := time.Now()
	if err := writeCompletion(w, req.Stream, out); err != nil {
		perf.Outcome = performanceWriteError
	}
	perf.WriteMS = elapsedMS(started)
	perf.Phase = performanceWrite
	perf.noteCancellation(r.Context())
	perf.Phase = ""
	if perf.Outcome == performanceWriteError {
		return
	}
	if perf.CanceledPhase != "" || r.Context().Err() != nil {
		perf.Outcome = performanceExecutionCanceled
		if perf.CanceledPhase == "" {
			perf.CanceledPhase = performanceWrite
		}
	} else if out.apiErr != nil {
		perf.Outcome = performanceSelectionError
	} else {
		perf.Outcome = performanceSuccess
	}
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

func writeRunError(w http.ResponseWriter, err error) error {
	// A conversation the task refuses is the caller's mistake: clients must
	// not retry it as a server failure.
	if _, ok := errors.AsType[*task.ValidationError](err); ok || errors.Is(err, propose.ErrContextBudget) {
		return writeError(w, http.StatusBadRequest, apiError{Message: err.Error(), Type: "invalid_request_error", Param: "messages"})
	}
	return writeError(w, http.StatusInternalServerError, apiError{Message: err.Error(), Type: "internal_error"})
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
