package judge

import (
	"bytes"
	"crypto/sha256"
	"sort"

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

// tieBreakKeys is the chain, most meaningful first. Each key narrows the
// set it is given; the first one that narrows it to a single candidate has
// decided.
//
// The order is the whole design, so it is a table: swapping two rows is the
// only edit a change of mind needs.
var tieBreakKeys = []struct {
	key    trace.TieBreakKey
	narrow func(among, all []string, tx Texts) []string
}{
	{trace.TieBreakConsensus, narrowByCentrality},
	{trace.TieBreakLength, narrowByLength},
	{trace.TieBreakHash, narrowByHash},
}

// tieBreak picks one candidate from among, deterministically, and says
// which key parted them. all is the run's candidates in the order the
// caller gave them; it is read for the centrality count and never for the
// order itself.
//
// A tie has to be broken by something, and nothing in the chain may be the
// presentation position or the proposer order: those are exactly the biases
// the order swap exists to detect, and a fleet-wide preference for whoever
// is configured first is not a judgment about this question. The keys are
// ranked by how much they are about the answers — agreement with the other
// candidates, then brevity where the difference is real, and last a hash of
// the answer's own text, which is arbitrary but is at least arbitrary about
// the answer.
//
// An answer that normalises to nothing — "***", "。。。" — is dropped before
// any key runs, unless that would leave nothing: a blank answer must not
// win a tie, and it must not switch off the key that would have punished
// it.
func tieBreak(among, all []string, tx Texts) (string, trace.TieBreakKey) {
	set := inOrder(among, all)
	if len(set) == 1 {
		return set[0], "" // nothing was tied, so no key decided
	}
	if kept := withoutBlanks(set, tx); len(kept) < len(set) {
		set = kept
		if len(set) == 1 {
			// An answer that normalises to nothing is the shortest answer
			// there is, and length is the only sense in which it can be
			// compared with one that says something.
			return set[0], trace.TieBreakLength
		}
	}
	for _, k := range tieBreakKeys {
		next := k.narrow(set, all, tx)
		switch {
		case len(next) == 1:
			return next[0], k.key
		case len(next) > 1:
			set = next
		}
	}
	// Every key read the same answer twice: these candidates wrote the same
	// thing to the byte, and nothing about the answers can part them. The
	// id is a property of the configuration rather than of a position, and
	// the recorded key says plainly that no key decided.
	return sortedIDs(set)[0], trace.TieBreakIdentical
}

// withoutBlanks drops the candidates whose normalised answer is empty,
// keeping the set as it was if they all are.
func withoutBlanks(among []string, tx Texts) []string {
	var keep []string
	for _, id := range among {
		if tx.norm(id) != "" {
			keep = append(keep, id)
		}
	}
	if len(keep) == 0 {
		return among
	}
	return keep
}

// narrowByCentrality keeps the candidates whose normalised answer agrees
// with the most others in the run — the whole run, not just the tied set,
// because agreement with a candidate that lost is still agreement. It is
// the selector universal self-consistency and the similarity-vote
// ensembles use, and the one signal a pairwise judge throws away.
func narrowByCentrality(among, all []string, tx Texts) []string {
	best, keep := -1, []string(nil)
	for _, id := range among {
		n := 0
		for _, other := range all {
			if other == id {
				continue
			}
			if _, ok := agree(tx.norm(id), tx.norm(other)); ok {
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
func narrowByLength(among, _ []string, tx Texts) []string {
	shortest, longest := -1, 0
	for _, id := range among {
		n := len([]rune(tx.norm(id)))
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
		if len([]rune(tx.norm(id))) == shortest {
			keep = append(keep, id)
		}
	}
	if len(keep) > 1 {
		return nil // no unique shortest: the key does not fire
	}
	return keep
}

// narrowByHash keeps the candidates whose normalised answer has the lowest
// SHA-256 digest, and falls to the digest of the raw answer when the
// normalised texts are the same — which two candidates can be without
// having been grouped by the consensus stage, because grouping needs a
// strict majority and two of four is not one.
//
// It is the last key and it is arbitrary, but it is arbitrary about the
// answer and nothing else: not the position it was shown in, not the
// proposer that wrote it, not its length. The same answers give the same
// winner in every run, on every machine, which a coin flip would not.
func narrowByHash(among, _ []string, tx Texts) []string {
	keep := lowestDigest(among, func(id string) string { return tx.norm(id) })
	if len(keep) > 1 {
		keep = lowestDigest(keep, func(id string) string { return tx[id].Raw })
	}
	return keep
}

func lowestDigest(among []string, text func(string) string) []string {
	var best []byte
	var keep []string
	for _, id := range among {
		sum := sha256.Sum256([]byte(text(id)))
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
// that order — it exists so a set read out of a map is walked the same way
// twice.
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

// sortedIDs is the order a tied set is written down in and, at the very end
// of the chain, chosen in. It is the same list whatever order the run
// happened to present the candidates in, which proposer order is not.
func sortedIDs(ids []string) []string {
	out := append([]string{}, ids...)
	sort.Strings(out)
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
func ranked(ids []string, sc map[string]float64, tx Texts) []string {
	rest := append([]string{}, ids...)
	out := make([]string, 0, len(ids))
	for len(rest) > 0 {
		_, top := topScore(rest, sc)
		next, _ := tieBreak(top, ids, tx)
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
