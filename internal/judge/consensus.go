package judge

import (
	"strings"
	"unicode"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

// Normalisation is the version word written into judge.json's consensus
// block. It is a version, not a description: a trace says which normaliser
// produced its groups, so a later one can be told from this one instead of
// being compared with it silently.
const Normalisation = "nfkc-v1"

// maxNumericRunes is how long a normalised answer may be and still be
// compared by its last number. Beyond it an answer is prose that happens to
// hold a figure, and two essays agreeing on one number is not agreement.
const maxNumericRunes = 160

// Normalize folds one answer down to the text two candidates are compared
// on. It is deliberately conservative and deliberately cheap: it must never
// make two different answers look the same, and it reads nothing but the
// answer — not the question, not a reference.
//
// The passes, in order:
//
//	the compatibility fold below (full-width forms become ASCII)
//	lower case
//	the markdown emphasis markers * _ ` are removed
//	a list marker at the start of a line is removed
//	whitespace runs collapse to one space, and the ends are trimmed
//	trailing . 。 ! ? are trimmed
//
// The fold is hand-rolled rather than a real Unicode NFKC, because CMoA
// declares no dependencies and the standard library has no normaliser. It
// covers the forms a local model actually emits in Japanese answers — the
// full-width ASCII block and the ideographic space — and nothing else.
func Normalize(s string) string {
	s = strings.ToLower(foldCompatibility(s))
	s = strings.Map(func(r rune) rune {
		if r == '*' || r == '_' || r == '`' {
			return -1
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = stripListMarker(line)
	}
	s = strings.Join(strings.Fields(strings.Join(lines, "\n")), " ")
	return strings.TrimRight(s, ".。!?")
}

// foldCompatibility maps the full-width ASCII block onto ASCII and the
// ideographic space onto a space. １０１ and 101 are the same answer, and a
// normaliser that could not see that would refuse the agreement the whole
// stage exists to find.
func foldCompatibility(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0xff01 && r <= 0xff5e:
			return r - 0xfee0
		case r == 0x3000:
			return ' '
		}
		return r
	}, s)
}

// stripListMarker removes one leading bullet or number from a line, so an
// answer written as a list is compared with the same answer written as a
// sentence.
//
// A number is a marker only when what follows it is not another digit: a
// line that is "101." is an answer, "1. 答え" is a list item, and "1.101" is
// one decimal number. Eating the digits of a bare numeric answer would
// invent an agreement between every two answers that have no number left.
func stripListMarker(line string) string {
	rest := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(rest, "•") {
		return strings.TrimLeft(strings.TrimPrefix(rest, "•"), " \t")
	}
	i, bullet := 0, strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, "+")
	if bullet {
		i = 1
	} else {
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		if i == 0 || i >= len(rest) || (rest[i] != '.' && rest[i] != ')') {
			return rest
		}
		i++
		if i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			return rest // a decimal number, not a numbered item
		}
	}
	tail := strings.TrimLeft(rest[i:], " \t")
	if tail == "" || (bullet && len(tail) == len(rest[i:])) {
		// Nothing behind the marker, or a bullet with no space after it,
		// which is a hyphen or a plus sign inside the answer.
		return rest
	}
	return tail
}

// agree says whether two normalised answers are the same answer, and by
// which of the two tests. Both are conservative; neither looks at the
// question, so neither can be talked into an agreement by a shared
// misreading of it.
func agree(a, b string) (trace.ConsensusAgreement, bool) {
	if a != "" && a == b {
		return trace.AgreementExact, true
	}
	if len([]rune(a)) > maxNumericRunes || len([]rune(b)) > maxNumericRunes {
		return "", false
	}
	na, oka := lastNumber(a)
	nb, okb := lastNumber(b)
	if oka && okb && na == nb {
		return trace.AgreementNumeric, true
	}
	return "", false
}

// lastNumber returns the canonical form of the last number in a normalised
// answer. The last one is the answer: a worked calculation states its
// operands first and its result last.
//
// Thousands separators are dropped, a sign is kept when it is a sign and
// not a hyphen inside a word, and zeros that carry no value are dropped, so
// 276,416 and 276416 and 276416.0 are one number.
func lastNumber(s string) (string, bool) {
	r := []rune(s)
	for i := len(r) - 1; i >= 0; i-- {
		if !unicode.IsDigit(r[i]) {
			continue
		}
		end := i + 1
		start, dot := i, false
		// Walk back over the digits, the thousands separators and at most
		// one decimal point that has a digit on both sides.
	scan:
		for start >= 0 {
			switch c := r[start]; {
			case unicode.IsDigit(c) || c == ',' || c == '、':
				start--
			case c == '.' && !dot && start > 0 && unicode.IsDigit(r[start-1]):
				dot, start = true, start-1
			default:
				break scan
			}
		}
		start++
		if start > 0 && r[start-1] == '-' && (start == 1 || !isWordRune(r[start-2])) {
			start--
		}
		return canonicalNumber(string(r[start:end])), true
	}
	return "", false
}

func isWordRune(r rune) bool { return unicode.IsDigit(r) || unicode.IsLetter(r) }

// canonicalNumber strips the separators and the zeros that carry no value,
// so two spellings of one quantity compare equal as strings.
func canonicalNumber(s string) string {
	s = strings.NewReplacer(",", "", "、", "").Replace(s)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	if hasFrac {
		if frac = strings.TrimRight(frac, "0"); frac != "" {
			whole += "." + frac
		}
	}
	if whole == "0" {
		sign = ""
	}
	return sign + whole
}

// consensusGroups partitions the candidates into sets that agree, in the
// order the candidates were given. A candidate that agrees with nobody is a
// group of one.
//
// Agreement is an equivalence in practice — both tests compare a canonical
// form — so a greedy pass finds the same partition a clique search would,
// and a candidate is added to a group only when it agrees with every member
// already in it.
func consensusGroups(ids []string, norm map[string]string) [][]string {
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
				if _, ok := agree(norm[member], norm[other]); !ok {
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
func groupAgreement(group []string, norm map[string]string) trace.ConsensusAgreement {
	for i := range group {
		for k := i + 1; k < len(group); k++ {
			if how, ok := agree(norm[group[i]], norm[group[k]]); !ok || how != trace.AgreementExact {
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
func consensus(ids []string, norm map[string]string) (*trace.Consensus, *trace.TieBreak) {
	if len(ids) < 2 {
		return nil, nil
	}
	groups := consensusGroups(ids, norm)
	var win []string
	for _, g := range groups {
		if len(g) > len(win) {
			win = g
		}
	}
	if len(win) < 2 || 2*len(win) <= len(ids) {
		return nil, nil
	}
	chosen, key := tieBreak(win, ids, norm)
	return &trace.Consensus{
		Normalisation: Normalisation,
		Groups:        groups,
		Chosen:        chosen,
		Agreement:     groupAgreement(win, norm),
	}, &trace.TieBreak{
		Among: win, Key: key, Chosen: chosen,
	}
}
