package judge

import (
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// consensusGroups partitions the candidates into sets that agree, in the
// order the candidates were given. A candidate that agrees with nobody is a
// group of one, and a candidate whose answer normalises to nothing agrees
// with nobody at all — an empty answer is not the same answer as another
// empty answer.
//
// Agreement is an equivalence in practice — both tests compare a canonical
// form — so a greedy pass finds the same partition a clique search would,
// and a candidate is added to a group only when it agrees with every member
// already in it.
func consensusGroups(ids []string, tx Texts) [][]string {
	taken := map[string]bool{}
	var groups [][]string
	for _, id := range ids {
		if taken[id] {
			continue
		}
		taken[id] = true
		group := []string{id}
		for _, other := range ids {
			if taken[other] {
				continue
			}
			all := true
			for _, member := range group {
				if _, ok := agree(tx.norm(member), tx.norm(other)); !ok {
					all = false
					break
				}
			}
			if all {
				taken[other] = true
				group = append(group, other)
			}
		}
		groups = append(groups, group)
	}
	return groups
}

// groupAgreement is how a group agreed: exact when every pair of members is
// the same normalised text, numeric when at least one pair needed the
// weaker test. The weaker finding is the honest label for the group.
func groupAgreement(group []string, tx Texts) trace.ConsensusAgreement {
	for i := range group {
		for k := i + 1; k < len(group); k++ {
			if how, ok := agree(tx.norm(group[i]), tx.norm(group[k])); !ok || how != trace.AgreementExact {
				return trace.AgreementNumeric
			}
		}
	}
	return trace.AgreementExact
}

// consensus returns the stage-1 finding when more than half the candidates
// say the same thing, and nil otherwise. More than half of three is two, so
// the common case — two proposers agreeing and one dissenting — is a
// consensus, and not one judge call is spent on it. The tie-break says
// which member of the group is returned.
func consensus(ids []string, tx Texts) (*trace.Consensus, *trace.TieBreak) {
	if len(ids) < 2 {
		return nil, nil
	}
	groups := consensusGroups(ids, tx)
	var win []string
	for _, g := range groups {
		if len(g) > len(win) {
			win = g
		}
	}
	if len(win) < 2 || 2*len(win) <= len(ids) {
		return nil, nil
	}
	chosen, key := tieBreak(win, ids, tx)
	return &trace.Consensus{
		Normalisation: Normalisation,
		Groups:        groups,
		Chosen:        chosen,
		Agreement:     groupAgreement(win, tx),
	}, &trace.TieBreak{
		Among: sortedIDs(win), Key: key, Chosen: chosen,
	}
}
