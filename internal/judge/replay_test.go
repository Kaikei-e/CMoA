package judge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// snapshot is every file under dir, by path and digest. A replay reads a
// run and writes elsewhere, and "elsewhere" is a claim worth checking.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		body, err := os.ReadFile(path) //nolint:gosec // a directory the test made
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[rel] = llm.SHA256(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The replay of a run reproduces the run: the same six calls, the same
// requests and answers, the same outcome — and no server was asked
// anything, which is what makes it worth having.
func TestReplayReproducesTheRun(t *testing.T) {
	f := &fakeJudge{t: t, script: map[order]string{
		{"a", "b"}: trace.ChoiceA, {"b", "a"}: trace.ChoiceB,
		{"a", "c"}: trace.ChoiceA, {"c", "a"}: trace.ChoiceB,
		{"b", "c"}: trace.ChoiceA, {"c", "b"}: trace.ChoiceB,
	}}
	j, src := fixture(t, f)
	in := input("a", "b", "c")
	live, err := j.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 6 {
		t.Fatalf("live run made %d call(s)", f.calls.Load())
	}
	before := snapshot(t, string(src))

	replayer, err := OpenReplay(src)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := replayer.PromptVersion(), live.Judge.PromptVersion; got != want {
		t.Errorf("prompt version %s, want %s", got, want)
	}
	if got, want := replayer.Seed(), live.Presentation.Seed; got != want {
		t.Errorf("seed %d, want %d", got, want)
	}

	dst := trace.Dir(t.TempDir())
	seed := replayer.Seed()
	in.Seed = &seed
	again := &Judge{Cfg: replayer.Params(j.Cfg), Client: replayer, Dir: dst}
	got, err := again.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != 6 {
		t.Errorf("the replay asked the server %d more call(s)", f.calls.Load()-6)
	}
	if got.Outcome != live.Outcome {
		t.Errorf("outcome %+v, want %+v", got.Outcome, live.Outcome)
	}
	if got.Presentation.Nonce != live.Presentation.Nonce {
		t.Errorf("nonce %q, want %q", got.Presentation.Nonce, live.Presentation.Nonce)
	}
	if len(got.Pairs) != len(live.Pairs) {
		t.Fatalf("%d pair(s), want %d", len(got.Pairs), len(live.Pairs))
	}
	for i, pair := range got.Pairs {
		want := live.Pairs[i]
		if pair.Verdict != want.Verdict || pair.DrawReason != want.DrawReason {
			t.Errorf("pair %d: %s/%s, want %s/%s", i, pair.Verdict, pair.DrawReason, want.Verdict, want.DrawReason)
		}
		for k, order := range pair.Orders {
			w := want.Orders[k]
			if order.Status != w.Status || order.Choice != w.Choice {
				t.Errorf("pair %d order %d: %s/%s, want %s/%s", i, k, order.Status, order.Choice, w.Status, w.Choice)
			}
			// The digests are the check that the replay answered the same
			// question with the same answer, rather than merely reaching
			// the same verdict by another road.
			if order.RequestSHA256 != w.RequestSHA256 {
				t.Errorf("pair %d order %d: request %s, want %s", i, k, order.RequestSHA256, w.RequestSHA256)
			}
			if order.ResponseSHA256 != w.ResponseSHA256 {
				t.Errorf("pair %d order %d: response %s, want %s", i, k, order.ResponseSHA256, w.ResponseSHA256)
			}
		}
	}
	if diff := diffSnapshots(before, snapshot(t, string(src))); diff != "" {
		t.Errorf("the replay changed the run it read: %s", diff)
	}
	rec := replayer.Record()
	if rec.RunID != live.RunID || rec.PromptVersion != live.Judge.PromptVersion || len(rec.Calls) != 6 {
		t.Errorf("replayed_from %+v", rec)
	}
	if _, err := dst.ReadJudge(); err != nil {
		t.Fatal(err)
	}
}

// A run the candidates settled between themselves recorded no call, and a
// replay of it asks for none: the consensus stage runs again on the same
// answers and returns before the call list exists.
func TestReplayOfAConsensusRunAsksNothing(t *testing.T) {
	f := &fakeJudge{t: t, script: map[order]string{}}
	j, src := fixture(t, f)
	in := input("a", "b", "c")
	for i := range in.Candidates {
		in.Candidates[i].Answer = "The answer is 42."
	}
	live, err := j.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if live.Consensus == nil || len(live.Pairs) != 0 || f.calls.Load() != 0 {
		t.Fatalf("the source was not a consensus run: %+v, %d call(s)", live.Consensus, f.calls.Load())
	}
	replayer, err := OpenReplay(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayer.Record().Calls) != 0 {
		t.Errorf("replayed_from names %d call file(s)", len(replayer.Record().Calls))
	}
	seed := replayer.Seed()
	in.Seed = &seed
	dst := trace.Dir(t.TempDir())
	again := &Judge{Cfg: replayer.Params(j.Cfg), Client: replayer, Dir: dst}
	got, err := again.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pairs) != 0 || got.Outcome != live.Outcome {
		t.Errorf("%d pair(s), outcome %+v, want 0 and %+v", len(got.Pairs), got.Outcome, live.Outcome)
	}
	if got.Consensus == nil || got.Consensus.Chosen != live.Consensus.Chosen {
		t.Errorf("consensus %+v, want %+v", got.Consensus, live.Consensus)
	}
}

// What a replayer does with a call it has no record of. Both are refusals
// rather than silence: a run that quietly invented an answer would be
// indistinguishable from one that read a good record.
func TestReplayRefusesWhatItDoesNotHave(t *testing.T) {
	f := &fakeJudge{t: t, script: map[order]string{
		{"a", "b"}: trace.ChoiceA, {"b", "a"}: trace.ChoiceB,
	}}
	j, src := fixture(t, f)
	if _, err := j.Run(context.Background(), input("a", "b")); err != nil {
		t.Fatal(err)
	}
	replayer, err := OpenReplay(src)
	if err != nil {
		t.Fatal(err)
	}
	// A pair the source never had.
	if _, err := replayer.ChatCompletion(context.Background(), Call{Pair: 7, Order: "ab"}, llm.Request{}); err == nil {
		t.Error("a call the record does not hold was answered")
	}
	// A retry the source never needed: the first attempt parsed, so there
	// is no second one to hand back.
	_, err = replayer.ChatCompletion(context.Background(), Call{Pair: 0, Order: "ab", Attempt: 1}, llm.Request{})
	if err == nil {
		t.Error("an attempt the record does not hold was answered")
	}
}

// diffSnapshots names the first path two snapshots disagree about.
func diffSnapshots(before, after map[string]string) string {
	for path, sum := range before {
		if got, ok := after[path]; !ok {
			return path + " is gone"
		} else if got != sum {
			return path + " changed"
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			return path + " is new"
		}
	}
	return ""
}
