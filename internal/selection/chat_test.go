package selection

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// chatRun writes a chat task and a chat run holding the given answers, the
// way propose would have left them. An empty answer is a candidate with
// status empty, which the judge never sees.
func chatRun(t *testing.T, answers map[string]string) (*task.Task, trace.Dir) {
	t.Helper()
	dir := t.TempDir()
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "task.json"), `{"version":3,"id":"c","face":"chat"}`)
	write(filepath.Join(dir, "conversation.json"), `[{"role":"user","content":"why?"}]`)
	tk, err := task.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := trace.Dir(filepath.Join(trace.RunsRoot(dir), "20260905T120000Z-abcdef01"))
	rec := &trace.Run{
		SchemaVersion: trace.SchemaVersion, RunID: run.ID(), Face: trace.FaceChat,
		Task: trace.TaskRef{ID: "c"}, CandidatesOrigin: trace.OriginProposers,
	}
	for _, id := range sortedKeys(answers) {
		rec.Proposers = append(rec.Proposers, trace.ProposerRef{ID: id})
	}
	if err := run.WriteRun(rec); err != nil {
		t.Fatal(err)
	}
	for id, answer := range answers {
		c := &trace.Candidate{ProposerID: id, Face: trace.FaceChat, Status: trace.CandidateOK}
		if answer == "" {
			c.Status = trace.CandidateEmpty
		}
		if err := run.WriteChatCandidate(c, []byte(answer), answer); err != nil {
			t.Fatal(err)
		}
	}
	return tk, run
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for k := i + 1; k < len(out); k++ {
			if out[k] < out[i] {
				out[i], out[k] = out[k], out[i]
			}
		}
	}
	return out
}

// judgeServer answers every call by preferring whichever block holds want.
func judgeServer(t *testing.T, want string) *config.Config {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		user := body.Messages[len(body.Messages)-1].Content
		blocks := strings.Split(user, `<candidate id=`)[1:]
		choice := trace.ChoiceTie
		if len(blocks) == 2 {
			if strings.Contains(blocks[0], want) {
				choice = trace.ChoiceA
			} else if strings.Contains(blocks[1], want) {
				choice = trace.ChoiceB
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": fmt.Sprintf(`{"reason":"r","choice":%q}`, choice)}}},
		})
	}))
	t.Cleanup(s.Close)
	cfg, err := config.Parse([]byte(`{"version":2,"proposers":[{"id":"x","base_url":"http://127.0.0.1:1","model":"m"}],
	  "harness":{"vault":"v"},"judge":{"base_url":"` + s.URL + `/v1","model":"j","parallel":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRunChat(t *testing.T) {
	tk, dir := chatRun(t, map[string]string{"a": "alpha answer", "b": "beta answer", "c": ""})
	cfg := judgeServer(t, "alpha")
	sel, err := RunChat(t.Context(), cfg, tk, dir, ChatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s, ok := sel.(Selected)
	if !ok || s.CandidateID != "a" {
		t.Fatalf("%#v", sel)
	}
	rec, err := dir.ReadSelect()
	if err != nil {
		t.Fatal(err)
	}
	if rec.Rule != RuleJudgePairwise || len(rec.AlsoPassed) != 0 {
		t.Errorf("select %+v", rec)
	}
	// Order is every candidate the run asked for, including the one that
	// answered nothing; ranked is only those the judge compared.
	if strings.Join(rec.Order, ",") != "a,b,c" || strings.Join(rec.Ranked, ",") != "a,b" {
		t.Errorf("order %v ranked %v", rec.Order, rec.Ranked)
	}
	rep, err := dir.ReadJudge()
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Pairs) != 1 || rep.Outcome.CandidateID != "a" {
		t.Errorf("report %+v", rep.Outcome)
	}
	// A run is selected once.
	if _, err := RunChat(t.Context(), cfg, tk, dir, ChatOptions{}); err == nil {
		t.Error("select.json must be write-once")
	}
}

// A no_candidate carries its sub-reason all the way into select.json: the
// distribution of those words is how a judge is measured.
func TestRunChatNoCandidate(t *testing.T) {
	tk, dir := chatRun(t, map[string]string{"a": "alpha", "b": "beta"})
	sel, err := RunChat(t.Context(), judgeServer(t, "nothing matches"), tk, dir, ChatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n, ok := sel.(NoCandidate)
	if !ok || n.Reason != trace.ReasonNoMajority || n.Tried != 2 {
		t.Fatalf("%#v", sel)
	}
	rec, err := dir.ReadSelect()
	if err != nil {
		t.Fatal(err)
	}
	if rec.Selection.Kind != trace.SelectionNoCandidate || rec.Selection.Reason != string(trace.ReasonNoMajority) {
		t.Errorf("%+v", rec.Selection)
	}
}

// A single answer is not a selection, and no judge call is spent finding
// that out.
func TestRunChatTooFewCandidates(t *testing.T) {
	tk, dir := chatRun(t, map[string]string{"a": "alpha", "b": ""})
	sel, err := RunChat(t.Context(), judgeServer(t, "alpha"), tk, dir, ChatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n, ok := sel.(NoCandidate)
	if !ok || n.Reason != trace.ReasonTooFewCandidates {
		t.Fatalf("%#v", sel)
	}
}

func TestRunChatRefusals(t *testing.T) {
	tk, dir := chatRun(t, map[string]string{"a": "alpha", "b": "beta"})
	cfg := judgeServer(t, "alpha")
	noJudge := *cfg
	noJudge.Judge = nil
	if _, err := RunChat(t.Context(), &noJudge, tk, dir, ChatOptions{}); err == nil {
		t.Error("a chat run needs a judge")
	}
	// A coding run is not selected by the judge, and a coding task is not
	// selected by RunChat.
	coding := &task.Task{ID: "c", Face: task.FaceCoding}
	if _, err := RunChat(t.Context(), cfg, coding, dir, ChatOptions{}); err == nil {
		t.Error("RunChat must refuse a coding task")
	}
}

func TestRecordAndFromJudge(t *testing.T) {
	for _, tc := range []struct {
		sel  Selection
		kind trace.SelectionKind
	}{
		{Selected{CandidateID: "a", Reason: "r"}, trace.SelectionSelected},
		{NoCandidate{Tried: 3, Reason: trace.ReasonCycle}, trace.SelectionNoCandidate},
		{JudgeTimeout{After: time.Second}, trace.SelectionJudgeTimeout},
		{JudgeFailed{Err: fmt.Errorf("boom")}, trace.SelectionJudgeFailed},
		{VerifierFailed{Err: fmt.Errorf("boom")}, trace.SelectionVerifierFailed},
	} {
		if got := Record(tc.sel); got.Kind != tc.kind {
			t.Errorf("%#v: kind %q, want %q", tc.sel, got.Kind, tc.kind)
		}
	}
	if got := Record(NoCandidate{Reason: trace.ReasonCycle}).Reason; got != "cycle" {
		t.Errorf("reason %q", got)
	}
	for _, tc := range []struct {
		out  trace.JudgeOutcome
		want Selection
	}{
		{trace.JudgeOutcome{Kind: trace.SelectionSelected, CandidateID: "a", Reason: "r"}, Selected{CandidateID: "a", Reason: "r"}},
		{trace.JudgeOutcome{Kind: trace.SelectionNoCandidate, Reason: "cycle"}, NoCandidate{Tried: 3, Reason: trace.ReasonCycle}},
		{trace.JudgeOutcome{Kind: trace.SelectionJudgeTimeout}, JudgeTimeout{}},
	} {
		if got := FromJudge(&trace.JudgeReport{Outcome: tc.out}, 3); fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", tc.want) {
			t.Errorf("%+v: got %#v, want %#v", tc.out, got, tc.want)
		}
	}
}

// A run whose judge.json is already there is refused before a single judge
// call. judge.json is written before select.json, so guarding only on
// select.json let an interrupted run pay for the whole protocol again and
// then die at the write, leaving it unselectable and the spend wasted.
func TestRunChatRefusesAnAlreadyJudgedRun(t *testing.T) {
	tk, dir := chatRun(t, map[string]string{"a": "alpha answer", "b": "beta answer"})
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": `{"reason":"r","choice":"A"}`}}},
		})
	}))
	t.Cleanup(s.Close)
	cfg, err := config.Parse([]byte(`{"version":2,"proposers":[{"id":"x","base_url":"http://127.0.0.1:1","model":"m"}],
	  "harness":{"vault":"v"},"judge":{"base_url":"` + s.URL + `/v1","model":"j"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := dir.WriteJudge(&trace.JudgeReport{SchemaVersion: trace.SchemaVersion, RunID: dir.ID()}); err != nil {
		t.Fatal(err)
	}
	if _, err := RunChat(t.Context(), cfg, tk, dir, ChatOptions{}); err == nil {
		t.Fatal("a run with judge.json must be refused")
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("the refused run spent %d judge calls", n)
	}
}
