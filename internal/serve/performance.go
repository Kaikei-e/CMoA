package serve

import (
	"context"
	"encoding/json"
	"time"
)

const (
	performanceSuccess           = "success"
	performanceSelectionError    = "selection_error"
	performanceInvalidRequest    = "invalid_request"
	performanceQueueCanceled     = "queue_canceled"
	performanceExecutionCanceled = "execution_canceled"
	performanceRunError          = "run_error"
	performanceWriteError        = "write_error"

	performanceQueue   = "queue"
	performanceParse   = "parse"
	performanceTask    = "task"
	performancePropose = "propose"
	performanceSelect  = "select"
	performanceRespond = "respond"
	performanceWrite   = "write"
)

// performance is one terminal structured-log record for a chat request.
// It deliberately excludes request content, authorization, and request
// headers. Times measure server-side wall intervals only: write_ms ends when
// this handler returns from writing, not when a client receives the body.
type performance struct {
	RequestID     string     `json:"request_id"`
	RunID         string     `json:"run_id,omitempty"`
	Outcome       string     `json:"outcome"`
	CanceledPhase string     `json:"canceled_phase,omitempty"`
	AcceptedAt    time.Time  `json:"accepted_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   time.Time  `json:"completed_at"`
	ParseMS       int64      `json:"parse_ms"`
	QueueMS       int64      `json:"queue_ms"`
	TaskMS        int64      `json:"task_ms"`
	ProposeMS     int64      `json:"propose_ms"`
	SelectMS      int64      `json:"select_ms"`
	RespondMS     int64      `json:"respond_ms"`
	WriteMS       int64      `json:"write_ms"`
	TotalMS       int64      `json:"total_ms"`
	Phase         string     `json:"-"`
}

func elapsedMS(start time.Time) int64 { return time.Since(start).Milliseconds() }

func (p *performance) noteCancellation(ctx context.Context) {
	if ctx.Err() != nil && p.CanceledPhase == "" {
		p.CanceledPhase = p.Phase
	}
}

func (s *Server) logPerformance(p performance) {
	b, err := json.Marshal(p)
	if err != nil {
		s.opt.Log("performance log: %v", err)
		return
	}
	s.opt.Log("performance %s", b)
}
