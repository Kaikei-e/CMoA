package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/judge"
	"github.com/Kaikei-e/CMoA/internal/prompt"
	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// openReplay reads the run a replay is to be made from and refuses every
// way it could be the wrong one.
//
// The prompt version is the refusal that matters. A recorded answer is an
// answer to the question the source asked, and re-aggregating it under a
// prompt that has since changed would produce a report about a judge nobody
// ran. The task is checked for the same reason at a coarser grain.
func openReplay(src string, t *task.Task, cfg *config.Config, stderr io.Writer) (*judge.Replayer, int) {
	dir := trace.Dir(src)
	run, err := dir.ReadRun()
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return nil, exitInvalid
	}
	if run.Task.ID != string(t.ID) {
		fmt.Fprintf(stderr, "cmoa: run %s judged task %q, not %q\n", dir.ID(), run.Task.ID, t.ID)
		return nil, exitInvalid
	}
	replay, err := judge.OpenReplay(dir)
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return nil, exitInvalid
	}
	if got, want := replay.PromptVersion(), prompt.Version(); got != want {
		fmt.Fprintf(stderr, "cmoa: run %s was judged at prompt version %s and this binary is %s; "+
			"the recorded answers are answers to another question\n", dir.ID(), got, want)
		return nil, exitInvalid
	}
	cfg.Judge = replay.Params(cfg.Judge)
	return replay, exitOK
}

// replayCandidates reads the answers the source run judged, out of the run
// itself rather than out of wherever they were read from that day. The files
// the source names may have moved; what it judged has not.
func replayCandidates(src string, stderr io.Writer) ([]propose.ExternalAnswer, int) {
	dir := trace.Dir(src)
	run, err := dir.ReadRun()
	if err != nil {
		fmt.Fprintln(stderr, "cmoa:", err)
		return nil, exitInvalid
	}
	var out []propose.ExternalAnswer
	for _, p := range run.Proposers {
		text, err := dir.ReadCandidateAnswer(p.ID)
		if err != nil {
			fmt.Fprintln(stderr, "cmoa:", err)
			return nil, exitInvalid
		}
		out = append(out, propose.ExternalAnswer{File: filepath.ToSlash(dir.CandidateAnswer(p.ID)), Text: text})
	}
	return out, exitOK
}
