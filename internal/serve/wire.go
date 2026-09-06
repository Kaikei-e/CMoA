package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kaikei-e/CMoA/internal/task"
)

type modelList struct {
	Object string  `json:"object"`
	Data   []model `json:"data"`
}

type model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// completion is the OpenAI chat completion object plus the cmoa extension.
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
	Delta        *message `json:"delta,omitempty"`
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

// extension contains public selection accounting without proposer ids.
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
	// Score is the winner's Copeland score, absent when nothing was
	// selected. A consensus costs no judge call, so its score is 0.
	Score     *float64       `json:"score,omitempty"`
	Consensus *consensusInfo `json:"consensus,omitempty"`
	TieBreak  *tieBreakInfo  `json:"tie_break,omitempty"`
}

// consensusInfo is present when the candidates agreed among themselves and
// the judge was never asked.
type consensusInfo struct {
	Normalisation string `json:"normalisation"`
	Agreement     string `json:"agreement"` // exact, or numeric
	Agreed        int    `json:"agreed"`    // how many candidates said the same thing
	Of            int    `json:"of"`        // how many were compared
}

// tieBreakInfo is present when more than one candidate was still in
// contention and a deterministic key parted them.
type tieBreakInfo struct {
	Key   string `json:"key"`   // consensus, length, hash, or identical
	Among int    `json:"among"` // how many were tied
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

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

func writeCompletion(w http.ResponseWriter, stream bool, out *answered) {
	if out.apiErr != nil {
		writeError(w, out.status, *out.apiErr)
		return
	}
	if stream {
		writeStream(w, out.completion)
		return
	}
	writeJSON(w, http.StatusOK, out.completion)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, apiErr apiError) {
	writeJSON(w, status, struct {
		Error apiError `json:"error"`
	}{apiErr})
}

// writeStream sends the selected response as one SSE chunk, followed by its
// terminator. Selection completes before any answer content is available.
func writeStream(w http.ResponseWriter, completion *completion) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	chunk := *completion
	chunk.Object = "chat.completion.chunk"
	chunk.Choices = []choice{{
		Index: 0,
		Delta: &message{
			Role:    task.RoleAssistant,
			Content: completion.Choices[0].Message.Content,
		},
		FinishReason: "stop",
	}}
	bytes, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", bytes)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// publicReason removes tie-break candidate ids from the client-visible text.
func publicReason(reason string) string {
	if i := strings.Index(reason, " among ["); i >= 0 {
		return reason[:i]
	}
	return reason
}
