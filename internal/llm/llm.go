// Package llm is a minimal client for the OpenAI-compatible chat completion
// endpoint every proposer backend speaks (llama-server, llama-swap,
// Lemonade, Ollama's /v1). It sends one non-streaming request and returns
// the first choice. It knows nothing about tool calling on purpose: the
// diff comes back as plain text and is parsed by internal/patch.
package llm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is one chat completion call.
type Request struct {
	BaseURL     string // e.g. http://127.0.0.1:8081/v1
	APIKey      string // empty: no Authorization header
	Model       string
	Messages    []Message
	Temperature float64
	MaxTokens   int
	Seed        *int64
	// ExtraBody is merged into the JSON body after the fields above, for
	// server-specific keys such as llama-server's chat_template_kwargs.
	// Keys CMoA sets are rejected by config validation, not here.
	ExtraBody map[string]json.RawMessage
}

// Response is the first choice of a completion plus the accounting the
// server reported.
type Response struct {
	Content      string // reasoning blocks stripped; see StripReasoning
	RawContent   string // as returned
	Reasoning    string // reasoning_content, when the server separates it
	FinishReason string
	Usage        Usage
	Timings      Timings
	RequestBody  []byte // exactly what was sent (no Authorization header)
	ResponseBody []byte // exactly what came back
	Elapsed      time.Duration
}

// Usage is the OpenAI usage object. ReasoningTokens comes from
// completion_tokens_details and is zero on servers that do not report it;
// it is part of CompletionTokens, not additional to it, which is how a
// completion of 4096 tokens and no answer is told from an empty one.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	Details          struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// Timings is llama-server's per-request timings object. Servers that do not
// send it leave it zero.
type Timings struct {
	PromptMS           float64 `json:"prompt_ms"`
	PredictedMS        float64 `json:"predicted_ms"`
	PredictedPerSecond float64 `json:"predicted_per_second"`
}

// HTTPError is a non-2xx answer.
type HTTPError struct {
	Status int
	Body   []byte
}

func (e *HTTPError) Error() string {
	b := e.Body
	if len(b) > 512 {
		b = b[:512]
	}
	return fmt.Sprintf("llm: HTTP %d: %s", e.Status, strings.TrimSpace(string(b)))
}

// DecodeError is a 2xx answer that is not a chat completion.
type DecodeError struct {
	Body []byte
	Err  error
}

func (e *DecodeError) Error() string { return "llm: decode response: " + e.Err.Error() }
func (e *DecodeError) Unwrap() error { return e.Err }

// Client sends requests. The zero value uses http.DefaultClient; set HTTP to
// bound transport-level timeouts. Per-request deadlines come from ctx.
type Client struct {
	HTTP *http.Client
}

// ChatCompletion performs the call. Errors: *HTTPError, *DecodeError, or a
// transport/context error (context.DeadlineExceeded on timeout).
func (c *Client) ChatCompletion(ctx context.Context, req Request) (*Response, error) {
	body, err := encodeBody(req)
	if err != nil {
		return nil, err
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if req.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}
	start := time.Now()
	resp, err := hc.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	elapsed := time.Since(start)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &HTTPError{Status: resp.StatusCode, Body: raw}
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage   Usage   `json:"usage"`
		Timings Timings `json:"timings"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &DecodeError{Body: raw, Err: err}
	}
	if len(parsed.Choices) == 0 {
		return nil, &DecodeError{Body: raw, Err: errors.New("no choices")}
	}
	ch := parsed.Choices[0]
	return &Response{
		Content:      StripReasoning(ch.Message.Content),
		RawContent:   ch.Message.Content,
		Reasoning:    ch.Message.ReasoningContent,
		FinishReason: ch.FinishReason,
		Usage:        parsed.Usage,
		Timings:      parsed.Timings,
		RequestBody:  body,
		ResponseBody: raw,
		Elapsed:      elapsed,
	}, nil
}

func encodeBody(req Request) ([]byte, error) {
	if req.BaseURL == "" || req.Model == "" || len(req.Messages) == 0 {
		return nil, errors.New("llm: base URL, model and messages are required")
	}
	m := map[string]json.RawMessage{}
	put := func(k string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		m[k] = b
		return nil
	}
	for k, v := range req.ExtraBody {
		m[k] = v
	}
	if err := errors.Join(
		put("model", req.Model),
		put("messages", req.Messages),
		put("temperature", req.Temperature),
		put("max_tokens", req.MaxTokens),
		put("stream", false),
	); err != nil {
		return nil, err
	}
	if req.Seed != nil {
		if err := put("seed", *req.Seed); err != nil {
			return nil, err
		}
	}
	return json.Marshal(m)
}

var thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

// StripReasoning removes <think>…</think> blocks some servers leave in the
// content when reasoning is not separated into reasoning_content. An
// unterminated <think> (the model ran out of tokens while thinking) leaves
// the text as is; the caller will find no diff in it and record no_diff.
func StripReasoning(s string) string {
	return strings.TrimLeft(thinkBlock.ReplaceAllString(s, ""), "\n")
}

// SHA256 is the hex digest used for request/response hashes in traces.
func SHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
