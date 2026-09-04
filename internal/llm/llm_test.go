package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func server(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return s
}

func TestChatCompletion(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	s := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"<think>hmm</think>\n\ndiff --git a/x b/x\n"},"finish_reason":"stop"}],
		  "usage":{"prompt_tokens":10,"completion_tokens":5},"timings":{"prompt_ms":1.5,"predicted_ms":20,"predicted_per_second":250}}`)
	})
	seed := int64(7)
	resp, err := (&Client{}).ChatCompletion(context.Background(), Request{
		BaseURL: s.URL + "/v1", APIKey: "k", Model: "m",
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Temperature: 0.2, MaxTokens: 100, Seed: &seed,
		ExtraBody: map[string]json.RawMessage{"chat_template_kwargs": json.RawMessage(`{"enable_thinking":false}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "diff --git a/x b/x\n" || resp.RawContent[:7] != "<think>" {
		t.Fatalf("content = %q raw = %q", resp.Content, resp.RawContent)
	}
	if resp.FinishReason != "stop" || resp.Usage.PromptTokens != 10 || resp.Timings.PredictedPerSecond != 250 {
		t.Fatalf("%+v", resp)
	}
	if gotAuth != "Bearer k" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotBody["model"] != "m" || gotBody["stream"] != false || gotBody["seed"].(float64) != 7 || gotBody["max_tokens"].(float64) != 100 {
		t.Fatalf("body = %v", gotBody)
	}
	if _, ok := gotBody["chat_template_kwargs"]; !ok {
		t.Fatal("extra_body not merged")
	}
	if _, ok := gotBody["n"]; ok {
		t.Fatal("n must not be sent")
	}
	if len(resp.RequestBody) == 0 || len(resp.ResponseBody) == 0 || resp.Elapsed <= 0 {
		t.Fatal("bodies/elapsed not captured")
	}
}

func TestHTTPError(t *testing.T) {
	s := server(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not loaded", http.StatusServiceUnavailable)
	})
	_, err := (&Client{}).ChatCompletion(context.Background(), Request{BaseURL: s.URL + "/v1", Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	he, ok := errors.AsType[*HTTPError](err)
	if !ok || he.Status != 503 {
		t.Fatalf("want HTTPError 503, got %v", err)
	}
}

func TestDecodeError(t *testing.T) {
	for _, body := range []string{"not json", `{"choices":[]}`} {
		s := server(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		_, err := (&Client{}).ChatCompletion(context.Background(), Request{BaseURL: s.URL + "/v1", Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
		if _, ok := errors.AsType[*DecodeError](err); !ok {
			t.Fatalf("%q: want DecodeError, got %v", body, err)
		}
	}
}

func TestTimeout(t *testing.T) {
	s := server(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := (&Client{}).ChatCompletion(ctx, Request{BaseURL: s.URL + "/v1", Model: "m", Messages: []Message{{Role: "user", Content: "x"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}

func TestNoAuthHeaderWithoutKey(t *testing.T) {
	s := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("unexpected Authorization header")
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	})
	if _, err := (&Client{}).ChatCompletion(context.Background(), Request{BaseURL: s.URL + "/v1", Model: "m", Messages: []Message{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestStripReasoning(t *testing.T) {
	cases := map[string]string{
		"<think>a\nb</think>\n\nout":         "out",
		"out":                                "out",
		"<think>unterminated":                "<think>unterminated",
		"<think>a</think>x<think>b</think>y": "xy",
	}
	for in, want := range cases {
		if got := StripReasoning(in); got != want {
			t.Errorf("StripReasoning(%q) = %q, want %q", in, got, want)
		}
	}
}
