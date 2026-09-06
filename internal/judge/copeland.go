package judge

import (
	"bytes"
	"crypto/sha256"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

// lengthGateRatio is how much longer the longest tied answer must be than
// the shortest before brevity is allowed to decide. It is a tunable, not a
// measured optimum: below it the difference is a matter of phrasing, and
// letting a word or two of politeness pick the answer would be a length
// bias rather than a counter-lever to one.
const lengthGateRatio = 1.5

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

// tieBreakKeys is the chain, most meaningful first. Each key narrows the
// set it is given; the first one that narrows it to a single candidate has
// decided, and the last one always does.
//
// The order is the whole design, so it is a table: swapping two rows is the
// only edit a change of mind needs.
var tieBreakKeys = []struct {
	key    trace.TieBreakKey
	narrow func(among, all []string, norm map[string]string) []string
}{
	{trace.TieBreakConsensus, narrowByCentrality},
	{trace.TieBreakLength, narrowByLength},
	{trace.TieBreakHash, narrowByHash},
}

// tieBreak picks one candidate from among, deterministically, and says
// which key parted them. all is the run's candidates in the order the
// caller gave them, which is the run's proposer order.
//
// A tie has to be broken by something, and nothing in the chain may be the
// presentation position or the proposer order: those are exactly the biases
// the order swap exists to detect, and a fleet-wide preference for whoever
// is configured first is not a judgment about this question. The keys are
// ranked by how much they are about the answers — agreement with the other
// candidates, then brevity where the difference is real, and last a hash of
// the answer's own text, which is arbitrary but is at least arbitrary about
// the answer.
func tieBreak(among, all []string, norm map[string]string) (string, trace.TieBreakKey) {
	set := inOrder(among, all)
	if len(set) == 1 {
		return set[0], "" // nothing was tied, so no key decided
	}
	for _, k := range tieBreakKeys {
		next := k.narrow(set, all, norm)
		switch {
		case len(next) == 1:
			return next[0], k.key
		case len(next) > 1:
			set = next
		}
	}
	return set[0], trace.TieBreakHash
}

// narrowByCentrality keeps the candidates whose normalised answer agrees
// with the most others in the run — the whole run, not just the tied set,
// because agreement with a candidate that lost is still agreement. It is
// the selector universal self-consistency and the similarity-vote
// ensembles use, and the one signal a pairwise judge throws away.
func narrowByCentrality(among, all []string, norm map[string]string) []string {
	best, keep := -1, []string(nil)
	for _, id := range among {
		n := 0
		for _, other := range all {
			if other == id {
				continue
			}
			if _, ok := agree(norm[id], norm[other]); ok {
				n++
			}
		}
		switch {
		case n > best:
			best, keep = n, []string{id}
		case n == best:
			keep = append(keep, id)
		}
	}
	return keep
}

// narrowByLength keeps the uniquely shortest normalised answer, but only
// when the tied answers differ in length by lengthGateRatio or more.
//
// The evidence is that LLM judges prefer long answers, so preferring the
// short one is a counter-lever rather than a new bias — but only where the
// difference is a real one. The gate is what keeps this from becoming a
// preference for terse proposers: two answers of nearly the same length say
// nothing about verbosity, and this key declines to decide them.
func narrowByLength(among, _ []string, norm map[string]string) []string {
	shortest, longest := -1, 0
	for _, id := range among {
		n := len([]rune(norm[id]))
		if shortest < 0 || n < shortest {
			shortest = n
		}
		if n > longest {
			longest = n
		}
	}
	if shortest <= 0 || float64(longest) < lengthGateRatio*float64(shortest) {
		return nil // within the gate: not this key's decision to make
	}
	var keep []string
	for _, id := range among {
		if len([]rune(norm[id])) == shortest {
			keep = append(keep, id)
		}
	}
	if len(keep) > 1 {
		return nil // no unique shortest: the key does not fire
	}
	return keep
}

// narrowByHash keeps the candidates whose normalised answer has the lowest
// SHA-256 digest.
//
// It is the last key and it is arbitrary, but it is arbitrary about the
// answer and nothing else: not the position it was shown in, not the
// proposer that wrote it, not its length. The same answers give the same
// winner in every run, on every machine, which a coin flip would not.
//
// Two candidates whose normalised text is byte-identical would have been
// grouped by the consensus stage and never reach a tie; if they do, the key
// still returns them and tieBreak takes the first.
func narrowByHash(among, _ []string, norm map[string]string) []string {
	var best []byte
	var keep []string
	for _, id := range among {
		sum := sha256.Sum256([]byte(norm[id]))
		switch cmp := bytes.Compare(sum[:], best); {
		case best == nil || cmp < 0:
			best, keep = sum[:], []string{id}
		case cmp == 0:
			keep = append(keep, id)
		}
	}
	return keep
}

// inOrder returns among sorted by its position in all. No key decides by
// that order — it exists so a set read out of a map is written down the
// same way twice, in the order a reader of run.json will recognise.
func inOrder(among, all []string) []string {
	in := map[string]bool{}
	for _, id := range among {
		in[id] = true
	}
	out := make([]string, 0, len(among))
	for _, id := range all {
		if in[id] {
			out = append(out, id)
			delete(in, id)
		}
	}
	for _, id := range among { // anything all did not name, in the given order
		if in[id] {
			out = append(out, id)
			delete(in, id)
		}
	}
	return out
}

// topScore returns the highest score in among and everyone who has it.
func topScore(among []string, sc map[string]float64) (float64, []string) {
	best, keep := 0.0, []string(nil)
	for _, id := range among {
		v := sc[id]
		switch {
		case keep == nil || v > best:
			best, keep = v, []string{id}
		case v == best:
			keep = append(keep, id)
		}
	}
	return best, keep
}

// ranked orders the candidates by Copeland score, breaking equal scores
// with the same chain the outcome uses. It is informational: only the
// outcome selects. Ordering by wins made every all-draw run rank in
// proposer order, which said nothing about the answers at all.
func ranked(ids []string, sc map[string]float64, norm map[string]string) []string {
	rest := append([]string{}, ids...)
	out := make([]string, 0, len(ids))
	for len(rest) > 0 {
		_, top := topScore(rest, sc)
		next, _ := tieBreak(top, ids, norm)
		out = append(out, next)
		for i, id := range rest {
			if id == next {
				rest = append(rest[:i], rest[i+1:]...)
				break
			}
		}
	}
	return out
}
