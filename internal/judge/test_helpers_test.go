package judge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// order names one call the way the fake judge is scripted: which candidate
// was shown as A and which as B.
type order struct{ first, second string }

// fakeJudge answers each ordered pair with a scripted choice. It reads the
// nonced blocks back out of the prompt, so the test scripts answers in
// terms of candidate ids and never has to know the permutation.
type fakeJudge struct {
	t        *testing.T
	script   map[order]string // "A", "B", "tie", or a raw body to return verbatim
	calls    atomic.Int32
	inFlight atomic.Int32
	maxSeen  atomic.Int32
	mu       sync.Mutex
	seen     []order
	delay    time.Duration
	status   int
}

func (f *fakeJudge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		m := f.maxSeen.Load()
		if n <= m || f.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	f.calls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(`{"error":"no"}`))
		return
	}
	var body struct {
		Messages []llm.Message `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.t.Error(err)
	}
	user := body.Messages[len(body.Messages)-1].Content
	if strings.HasSuffix(user, RetryInstruction) {
		user = body.Messages[len(body.Messages)-2].Content
	}
	o := readOrder(f.t, user)
	f.mu.Lock()
	f.seen = append(f.seen, o)
	f.mu.Unlock()
	answer, ok := f.script[o]
	if !ok {
		f.t.Errorf("unscripted order %+v", o)
		answer = trace.ChoiceTie
	}
	content := answer
	if answer == trace.ChoiceA || answer == trace.ChoiceB || answer == trace.ChoiceTie {
		content = fmt.Sprintf(`{"reason": "because", "choice": %q}`, answer)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": content}, "finish_reason": "stop"}},
		"usage":   map[string]int{"prompt_tokens": 100, "completion_tokens": 10},
	})
}

// readOrder recovers which answer was shown as A and which as B by reading
// the candidate blocks; the fixtures write the candidate id as the answer.
func readOrder(t *testing.T, user string) order {
	t.Helper()
	var ids []string
	for _, part := range strings.Split(user, `<candidate id=`)[1:] {
		body := part[strings.Index(part, ">")+2:]
		// The fixtures write the candidate id on the first line of the
		// answer, so the fake can script per ordered pair without knowing
		// the permutation.
		ids = append(ids, strings.TrimSpace(strings.SplitN(body, "\n", 2)[0]))
	}
	if len(ids) != 2 {
		t.Fatalf("want two candidate blocks, got %d in\n%s", len(ids), user)
	}
	return order{ids[0], ids[1]}
}

func fixture(t *testing.T, h http.Handler, mut ...func(*config.Judge)) (*Judge, trace.Dir) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	temp := 0.0
	cfg := &config.Judge{
		BaseURL: s.URL + "/v1", Model: "j", Temperature: &temp, MaxTokens: 512,
		TimeoutSeconds: 30, Parallel: 3, OutputFormat: config.OutputJSONSchema,
	}
	for _, m := range mut {
		m(cfg)
	}
	dir := trace.Dir(t.TempDir())
	return &Judge{Cfg: cfg, Client: Live{Client: &llm.Client{HTTP: s.Client()}}, Dir: dir}, dir
}

func input(ids ...string) Input {
	in := Input{
		RunID:        "20260905T120000Z-abcdef01",
		Conversation: []task.ConvMessage{{Role: task.RoleUser, Content: "Why?"}},
		AllowTie:     true,
	}
	for _, id := range ids {
		in.Candidates = append(in.Candidates, Candidate{ID: id, Answer: id})
	}
	return in
}
