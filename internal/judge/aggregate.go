package judge

import (
	"fmt"
	"strconv"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

// lengthGateRatio is how much longer the longest tied answer must be than
// the shortest before brevity is allowed to decide. It is a tunable, not a
// measured optimum: below it the difference is a matter of phrasing, and
// letting a word or two of politeness pick the answer would be a length
// bias rather than a counter-lever to one.
const lengthGateRatio = 1.5

// Text is one candidate's answer as the tie-break reads it: the normalised
// form every key uses, and the raw form, read only to part two candidates
// whose normalised text is the same to the byte.
type Text struct{ Norm, Raw string }

// Texts is the answers of one run, by candidate id. It may be nil: a caller
// that has only the pairs still gets a deterministic outcome, decided one
// key further down the chain.
type Texts map[string]Text

func (t Texts) norm(id string) string { return t[id].Norm }

// scores is the Copeland score of every candidate: a win is 1, and a draw
// the judge did answer is half a win to each side.
//
// A tie and a disagreement under swap both score 0.5, and that is the whole
// point: MT-Bench's protocol is to swap the order and call an inconsistent
// pair a tie, so a pair the swap did not survive is half-and-half evidence,
// not the absence of evidence. Why it drew is a different question, and
// pairs[].draw_reason is where that answer lives.
//
// A draw nobody could measure — a timeout, a transport error, an answer no
// parser could read — scores nothing for either side. A machine failure is
// not a judgment, and paying half a win for one would let an unreachable
// judge decide.
func scores(rep *trace.JudgeReport) map[string]float64 {
	out := map[string]float64{}
	for _, id := range rep.Candidates {
		out[id] = 0
	}
	for _, p := range rep.Pairs {
		if p.Verdict != trace.VerdictDraw {
			out[p.Verdict]++
			continue
		}
		switch p.DrawReason {
		case trace.DrawTie, trace.DrawDisagree:
			for _, id := range p.Pair {
				out[id] += 0.5
			}
		case trace.DrawInvalid, trace.DrawUnmeasured, "":
		}
	}
	return out
}

// Aggregate fills the pair verdicts and their draw reasons, the wins, the
// scores, the ranking and the outcome of a report whose orders have been
// answered.
//
// tx holds each candidate's answer, by id; it is what the tie-break reads.
// It may be nil — a caller that has only the pairs still gets a
// deterministic outcome, decided at the end of the chain.
func Aggregate(rep *trace.JudgeReport, tx Texts) {
	rep.DrawReasons = map[trace.DrawReason]int{}
	for i := range rep.Pairs {
		p := &rep.Pairs[i]
		// A verdict is recomputed, never inherited: aggregating a report
		// twice, or one assembled by a caller, must not leave a stale
		// winner behind for the score to spend.
		p.Verdict, p.DrawReason = trace.VerdictDraw, ""
		a, b := p.Orders[0], p.Orders[1]
		// Consistency is about the candidate, not the label: choosing A in
		// one order and B in the other is the same answer twice, and
		// choosing A in both is the position speaking.
		if a.Status == trace.JudgeCallOK && b.Status == trace.JudgeCallOK && a.ChoiceCandidate == b.ChoiceCandidate {
			rep.SwapConsistentPairs++
		}
		// The conservative rule: a win needs both orders, and any tie or
		// disagreement is a draw. A pair the swap did not survive is not
		// evidence, and treating it as one is how a coin flip becomes a
		// decision.
		if a.Status == trace.JudgeCallOK && b.Status == trace.JudgeCallOK &&
			a.ChoiceCandidate != "" && a.ChoiceCandidate == b.ChoiceCandidate {
			p.Verdict = a.ChoiceCandidate
			rep.Wins[p.Verdict]++
			continue
		}
		p.DrawReason = drawReason(a, b)
		rep.DrawReasons[p.DrawReason]++
	}
	rep.Scores = scores(rep)
	rep.Ranked = ranked(rep.Candidates, rep.Scores, tx)
	rep.Outcome = outcome(rep, tx)
}

// drawReason says why one pair produced no winner, most severe first: a
// pair nobody asked outranks a pair nobody could parse, which outranks an
// abstention, which outranks a contradiction.
func drawReason(a, b trace.JudgeOrder) trace.DrawReason {
	for _, o := range []trace.JudgeOrder{a, b} {
		switch o.Status {
		case trace.JudgeCallTimeout, trace.JudgeCallError:
			return trace.DrawUnmeasured
		case trace.JudgeCallOK, trace.JudgeCallInvalidOutput:
		}
	}
	if a.Status == trace.JudgeCallInvalidOutput || b.Status == trace.JudgeCallInvalidOutput {
		return trace.DrawInvalid
	}
	if a.Choice == trace.ChoiceTie || b.Choice == trace.ChoiceTie {
		return trace.DrawTie
	}
	return trace.DrawDisagree
}

// outcome reads the consensus, then the wins, and only then asks whether a
// failed call mattered. It records the tie-break it needed, next to the
// outcome that needed it.
//
// A pair that was never answered does not discard a winner it could not
// have unseated: if one candidate has already beaten every other, no answer
// to the pair between two losers can change that, and returning
// judge_timeout there would throw away a selection the judge did make. The
// failure is escalated only when the missing answers could still decide the
// outcome — that is, when some candidate could still reach a clean sweep if
// every unanswered pair went its way.
//
// Everything after that is the score. A machine failure still outranks it:
// an unmeasured pair that could have decided is a timeout or a transport
// error, and a pair no parser could read is invalid_output, because neither
// is a judgment the score is entitled to spend.
func outcome(rep *trace.JudgeReport, tx Texts) trace.JudgeOutcome {
	if len(rep.Candidates) < 2 {
		return trace.JudgeOutcome{Kind: trace.SelectionNoCandidate, Reason: string(trace.ReasonTooFewCandidates)}
	}
	if c := rep.Consensus; c != nil {
		agreed := 0
		for _, g := range c.Groups {
			if len(g) > agreed {
				agreed = len(g)
			}
		}
		return trace.JudgeOutcome{
			Kind:        trace.SelectionSelected,
			CandidateID: c.Chosen,
			Reason: fmt.Sprintf("consensus: %d of %d agree on the normalised answer (%s)",
				agreed, len(rep.Candidates), c.Agreement),
		}
	}
	// A Condorcet winner beats every other candidate; with n candidates
	// that is n-1 pairs, and there can be at most one.
	need := len(rep.Candidates) - 1
	var winners []string
	for _, id := range rep.Candidates {
		if rep.Wins[id] == need {
			winners = append(winners, id)
		}
	}
	if len(winners) == 1 {
		return trace.JudgeOutcome{
			Kind:        trace.SelectionSelected,
			CandidateID: winners[0],
			Reason:      fmt.Sprintf("condorcet winner, %d of %d pairs agreed under both orders", need, len(rep.Pairs)),
		}
	}
	if o := escalate(rep); o != nil {
		return *o
	}
	for _, p := range rep.Pairs {
		if p.DrawReason == trace.DrawInvalid {
			// An answer no parser could read is not a draw the score may
			// spend: the judge was asked and never said anything.
			return trace.JudgeOutcome{Kind: trace.SelectionNoCandidate, Reason: string(trace.ReasonInvalidOutput)}
		}
	}
	// The Copeland score. Nobody swept, so the highest score is selected;
	// no cycle, no all-draws and no blocked majority is a refusal any more,
	// because a draw is half a win rather than the absence of one.
	best, top := topScore(rep.Candidates, rep.Scores)
	if len(top) == 0 {
		return trace.JudgeOutcome{Kind: trace.SelectionNoCandidate, Reason: string(trace.ReasonNoMajority)}
	}
	if len(top) == 1 {
		return trace.JudgeOutcome{
			Kind: trace.SelectionSelected, CandidateID: top[0],
			Reason: fmt.Sprintf("copeland winner, score %s of %d (no condorcet winner)", number(best), need),
		}
	}
	chosen, key := tieBreak(top, rep.Candidates, tx)
	among := sortedIDs(top)
	rep.TieBreak = &trace.TieBreak{Among: among, Key: key, Chosen: chosen}
	return trace.JudgeOutcome{
		Kind: trace.SelectionSelected, CandidateID: chosen,
		Reason: fmt.Sprintf("copeland tie, score %s of %d, tie broken by %s among %v",
			number(best), need, key, among),
	}
}

// number prints a score the way a reader writes it: 2, not 2.0.
func number(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// escalate returns judge_timeout or judge_failed when the pairs that were
// never answered could still have changed who wins, and nil when they could
// not. A timeout outranks a transport error: it is the one a caller retries.
//
// The question is asked in the currency the outcome is decided in. Under
// the score a missing answer does not have to produce a clean sweep to
// matter — half a point is enough to overtake a leader or to join the tied
// set the chain then parts — so a candidate that could still reach the top
// score escalates, and only a failure that could not move the answer is
// swallowed. The sole top scorer is not counted against itself: more points
// for the leader only confirm it, and an opponent that could catch it is
// checked on its own row.
func escalate(rep *trace.JudgeReport) *trace.JudgeOutcome {
	missing := map[string]int{}
	var timedOut, failed *trace.JudgeOrder
	for i := range rep.Pairs {
		p := &rep.Pairs[i]
		if p.DrawReason != trace.DrawUnmeasured {
			continue
		}
		for _, id := range p.Pair {
			missing[id]++
		}
		for k := range p.Orders {
			switch p.Orders[k].Status {
			case trace.JudgeCallTimeout:
				if timedOut == nil {
					timedOut = &p.Orders[k]
				}
			case trace.JudgeCallError:
				if failed == nil {
					failed = &p.Orders[k]
				}
			case trace.JudgeCallOK, trace.JudgeCallInvalidOutput:
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	best, top := topScore(rep.Candidates, rep.Scores)
	couldDecide := false
	for _, id := range rep.Candidates {
		if missing[id] == 0 || (len(top) == 1 && top[0] == id) {
			continue
		}
		if rep.Scores[id]+float64(missing[id]) >= best {
			couldDecide = true
		}
	}
	if !couldDecide {
		return nil
	}
	if timedOut != nil {
		return &trace.JudgeOutcome{Kind: trace.SelectionJudgeTimeout, Reason: "the judge did not answer in time"}
	}
	return &trace.JudgeOutcome{Kind: trace.SelectionJudgeFailed, Reason: failed.Error}
}
