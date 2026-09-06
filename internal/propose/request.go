package propose

import (
	"context"
	"errors"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// ask sends one request and writes prompt/<id>.json and candidates/<id>.*.
// Only trace write failures are returned.
func ask(ctx context.Context, client *llm.Client, p *config.Proposer, face task.Face, messages []llm.Message, dir trace.Dir, now func() time.Time, logf func(string, ...any)) error {
	key, err := p.APIKey()
	if err != nil {
		return err
	}
	req := llm.Request{BaseURL: p.BaseURL, APIKey: key, Model: p.Model, Messages: messages, Temperature: *p.Temperature, MaxTokens: p.MaxTokens, Seed: p.Seed, ExtraBody: p.ExtraBody}
	cand := &trace.Candidate{ProposerID: string(p.ID), Model: p.Model, Face: string(face), StartedAt: now().UTC()}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSeconds)*time.Second)
	defer cancel()
	resp, callErr := client.ChatCompletion(reqCtx, req)
	cand.FinishedAt = now().UTC()
	cand.Timings.RequestMS = cand.FinishedAt.Sub(cand.StartedAt).Milliseconds()

	raw, promptRecord := requestTrace(ctx, p.ID, messages, cand, resp, callErr)
	if err := dir.WritePrompt(promptRecord); err != nil {
		return err
	}
	var body string
	if callErr == nil {
		body = classifyCompletion(face, cand, resp.Content)
	}
	logf("%s: %s (%s, %d tokens)", p.ID, cand.Status, time.Duration(cand.Timings.RequestMS)*time.Millisecond, cand.Usage.CompletionTokens)
	return writeCompletion(face, dir, cand, raw, body)
}

func requestTrace(parent context.Context, id config.ProposerID, messages []llm.Message, cand *trace.Candidate, resp *llm.Response, callErr error) ([]byte, *trace.Prompt) {
	promptRecord := &trace.Prompt{ProposerID: string(id), Messages: toTraceMessages(messages)}
	if callErr != nil {
		return requestFailure(parent, cand, callErr), promptRecord
	}
	cand.FinishReason = resp.FinishReason
	cand.Usage = trace.Usage{PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens, ReasoningTokens: resp.Usage.Details.ReasoningTokens}
	cand.ReasoningBytes = reasoningBytes(resp)
	cand.Timings.ServerPromptMS = resp.Timings.PromptMS
	cand.Timings.ServerPredictedMS = resp.Timings.PredictedMS
	cand.Timings.TokensPerSecond = resp.Timings.PredictedPerSecond
	cand.RequestSHA256 = llm.SHA256(resp.RequestBody)
	cand.ResponseSHA256 = llm.SHA256(resp.ResponseBody)
	promptRecord.Request = resp.RequestBody
	promptRecord.SHA256 = cand.RequestSHA256
	return []byte(resp.RawContent), promptRecord
}

func requestFailure(parent context.Context, cand *trace.Candidate, callErr error) []byte {
	cand.Error = callErr.Error()
	var httpErr *llm.HTTPError
	var decodeErr *llm.DecodeError
	switch {
	case errors.Is(callErr, context.DeadlineExceeded) && parent.Err() == nil:
		cand.Status = trace.CandidateTimeout
	case errors.As(callErr, &httpErr):
		cand.Status = trace.CandidateHTTPError
		return httpErr.Body
	case errors.As(callErr, &decodeErr):
		cand.Status = trace.CandidateMalformed
		return decodeErr.Body
	default:
		cand.Status = trace.CandidateHTTPError
	}
	return nil
}

func writeCompletion(face task.Face, dir trace.Dir, cand *trace.Candidate, raw []byte, body string) error {
	switch face {
	case task.FaceChat:
		return dir.WriteChatCandidate(cand, raw, body)
	case task.FaceCoding:
	}
	return dir.WriteCandidate(cand, raw, body)
}

func reasoningBytes(resp *llm.Response) int {
	if n := len(resp.Reasoning); n > 0 {
		return n
	}
	if n := len(resp.RawContent) - len(resp.Content); n > 0 {
		return n
	}
	return 0
}

func toTraceMessages(ms []llm.Message) []trace.Message {
	out := make([]trace.Message, len(ms))
	for i, m := range ms {
		out[i] = trace.Message{Role: m.Role, Content: m.Content}
	}
	return out
}
