package judge

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/prompt"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// ask performs one call, with the single retry a malformed answer earns.
//
// The results are named: the latency is filled in by a deferred function,
// and an unnamed result would be copied out of the function before that
// function ran, leaving every order in judge.json at zero.
func (j *Judge) ask(ctx context.Context, in Input, pi prompt.JudgeInput, pair int, order, first, second string, now func() time.Time) (rec *trace.JudgeCall, out trace.JudgeOrder) {
	rec = &trace.JudgeCall{
		SchemaVersion: trace.SchemaVersion, RunID: in.RunID, Pair: pair, Order: order,
		First: first, Second: second, Model: j.Cfg.Model, BaseURL: j.Cfg.BaseURL,
	}
	out = trace.JudgeOrder{First: first, Second: second, File: trace.JudgeCallName(pair, order)}
	started := now()
	defer func() {
		// The call's own wall clock, which covers both attempts when the
		// first did not parse. Each attempt's share is in the call file.
		rec.LatencyMS = now().Sub(started).Milliseconds()
		out.LatencyMS = rec.LatencyMS
	}()

	messages, err := prompt.BuildJudge(pi)
	if err != nil {
		rec.Status, out.Status, out.Error = trace.JudgeCallError, trace.JudgeCallError, err.Error()
		return rec, out
	}
	key, err := j.Cfg.APIKey()
	if err != nil {
		rec.Status, out.Status, out.Error = trace.JudgeCallError, trace.JudgeCallError, err.Error()
		return rec, out
	}
	body, err := j.extraBody(in.AllowTie)
	if err != nil {
		rec.Status, out.Status, out.Error = trace.JudgeCallError, trace.JudgeCallError, err.Error()
		return rec, out
	}

	// The retry appends one instruction and nothing else: a second prompt
	// that argued with the model would be a different question, and the
	// retry rate is only a usable measure while every retry is the same
	// retry.
	for attempt := 0; attempt < 2; attempt++ {
		msgs := messages
		if attempt == 1 {
			msgs = append(append([]llm.Message{}, messages...), llm.Message{Role: task.RoleUser, Content: RetryInstruction})
			out.Retries++
		}
		at, answer, status := j.one(ctx, msgs, key, body, in.AllowTie, now,
			Call{Pair: pair, Order: order, Attempt: attempt})
		rec.Attempts = append(rec.Attempts, at)
		out.RequestSHA256, out.ResponseSHA256 = at.RequestSHA256, at.ResponseSHA256
		if status == trace.JudgeCallOK {
			rec.Status, rec.Choice = status, answer.Choice
			out.Status, out.Choice = status, answer.Choice
			out.ChoiceCandidate = candidateOf(answer.Choice, first, second)
			return rec, out
		}
		out.Error = at.Error
		if out.Error == "" {
			out.Error = at.ParseError
		}
		if status != trace.JudgeCallInvalidOutput {
			// A transport failure or a timeout is not the model's answer
			// being wrong; asking again would measure the network.
			rec.Status, out.Status = status, status
			return rec, out
		}
	}
	rec.Status, out.Status = trace.JudgeCallInvalidOutput, trace.JudgeCallInvalidOutput
	return rec, out
}

// RetryInstruction is the one thing appended when an answer did not parse.
const RetryInstruction = "Return only the JSON object."

func (j *Judge) one(ctx context.Context, msgs []llm.Message, key string, body map[string]json.RawMessage, allowTie bool, now func() time.Time, call Call) (trace.JudgeAttempt, *trace.JudgeAnswer, trace.JudgeCallStatus) {
	at := trace.JudgeAttempt{Messages: toTraceMessages(msgs)}
	req := llm.Request{
		BaseURL: j.Cfg.BaseURL, APIKey: key, Model: j.Cfg.Model, Messages: msgs,
		Temperature: *j.Cfg.Temperature, MaxTokens: j.Cfg.MaxTokens, Seed: j.Cfg.Seed, ExtraBody: body,
	}
	started := now()
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(j.Cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	resp, err := j.Client.ChatCompletion(callCtx, call, req)
	at.LatencyMS = now().Sub(started).Milliseconds()
	if err != nil {
		at.Error = err.Error()
		var he *llm.HTTPError
		var de *llm.DecodeError
		switch {
		case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
			return at, nil, trace.JudgeCallTimeout
		case errors.As(err, &he):
			at.Response = rawJSON(he.Body)
		case errors.As(err, &de):
			at.Response = rawJSON(de.Body)
		}
		return at, nil, trace.JudgeCallError
	}
	at.Request = rawJSON(resp.RequestBody)
	at.Response = rawJSON(resp.ResponseBody)
	at.RequestSHA256 = llm.SHA256(resp.RequestBody)
	at.ResponseSHA256 = llm.SHA256(resp.ResponseBody)
	at.Content = resp.Content
	at.Usage = trace.Usage{PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens}
	answer, perr := ParseAnswer(resp.Content, allowTie)
	if perr != nil {
		at.ParseError = perr.Error()
		return at, nil, trace.JudgeCallInvalidOutput
	}
	at.Parsed = answer
	return at, answer, trace.JudgeCallOK
}
func candidateOf(choice, first, second string) string {
	switch choice {
	case trace.ChoiceA:
		return first
	case trace.ChoiceB:
		return second
	}
	return ""
}

func rawJSON(b []byte) json.RawMessage {
	if !json.Valid(b) {
		q, err := json.Marshal(string(b))
		if err != nil {
			return nil
		}
		return q
	}
	return json.RawMessage(b)
}

func toTraceMessages(ms []llm.Message) []trace.Message {
	out := make([]trace.Message, len(ms))
	for i, m := range ms {
		out[i] = trace.Message{Role: m.Role, Content: m.Content}
	}
	return out
}
