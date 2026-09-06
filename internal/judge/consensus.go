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
//	the markdown emphasis markers * and ` are removed
//	list markers are removed wherever they start an item in a line
//	whitespace runs collapse to one space, and the ends are trimmed
//	trailing . 。 ! ? are trimmed
//
// The underscore is left alone on purpose: it is a word character in every
// language the fleet writes about, and removing it would make user_id and
// userid the same answer.
//
// The fold is hand-rolled rather than a real Unicode NFKC, because CMoA
// declares no dependencies and the standard library has no normaliser. It
// covers the forms a local model actually emits in Japanese answers — the
// full-width ASCII block and the ideographic space — and nothing else.
func Normalize(s string) string {
	s = strings.ToLower(foldCompatibility(s))
	s = strings.Map(func(r rune) rune {
		if r == '*' || r == '`' {
			return -1
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = stripListMarkers(line)
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

// stripListMarkers removes every bullet and every numbered marker that
// starts an item, so an answer written as a list is compared with the same
// answer written as a sentence — and so the numbers of a list are never
// mistaken for the answer's number. A list written on one line has markers
// after the first, and stripping only the leading one left "1. りんご 2.
// みかん 3. ぶどう" with a last number of 3, which agreed with any short
// answer ending in three.
//
// A marker is only a marker at the start of the line or after a space, and
// only when something follows it that is not a digit: "101." is an answer,
// "1.101" is one decimal number, and "1. 答え" is a list item.
func stripListMarkers(line string) string {
	r := []rune(line)
	out := make([]rune, 0, len(r))
	boundary := true
	for i := 0; i < len(r); {
		if boundary {
			if n := markerAt(r, i); n > 0 {
				for i += n; i < len(r) && unicode.IsSpace(r[i]); i++ {
				}
				continue
			}
		}
		boundary = unicode.IsSpace(r[i])
		out = append(out, r[i])
		i++
	}
	return string(out)
}

// markerAt returns the length in runes of the list marker at i, or 0 when
// what is there is not one.
func markerAt(r []rune, i int) int {
	j := i
	switch r[j] {
	case '-', '+', '•':
		j++
	default:
		for j < len(r) && unicode.IsDigit(r[j]) {
			j++
		}
		if j == i || j >= len(r) || (r[j] != '.' && r[j] != ')') {
			return 0
		}
		j++
	}
	// A marker at the very end of the text is a full stop or a sign, and a
	// marker followed by a digit is a decimal point or a hyphen; eating
	// either would delete the answer's own number.
	if j >= len(r) || unicode.IsDigit(r[j]) {
		return 0
	}
	k := j
	for k < len(r) && unicode.IsSpace(r[k]) {
		k++
	}
	if k >= len(r) {
		return 0 // nothing behind it: an item with no text is not an item
	}
	return j - i
}

// negationsJA are matched as plain substrings: Japanese has no word
// boundaries to anchor to, and these forms do not occur inside unrelated
// words.
var negationsJA = []string{"ない", "ません", "ではない", "ではなく", "いいえ", "違い"}

// negationsEN are matched as whole words, so "no" does not fire on "know".
var negationsEN = []string{"not", "no", "never", "none", "cannot"}

// hasNegation reports whether a normalised answer denies something.
//
// It exists for the numeric test only, and it is the difference between
// "3つあります" and "3つではありません": both are short, both end in the
// same number, and without this they were the same answer at zero judge
// calls. The list is small and will miss forms; a missed negation costs an
// agreement, and a spurious one costs an agreement too, so both directions
// of error fall back on asking the judge.
func hasNegation(s string) bool {
	for _, t := range negationsJA {
		if strings.Contains(s, t) {
			return true
		}
	}
	if strings.Contains(s, "n't") {
		return true
	}
	for _, t := range negationsEN {
		if containsWord(s, t) {
			return true
		}
	}
	return false
}

// containsWord reports whether w occurs in s with a non-word rune on each
// side of it.
func containsWord(s, w string) bool {
	for at := 0; ; {
		i := strings.Index(s[at:], w)
		if i < 0 {
			return false
		}
		i += at
		before := i == 0 || !isWordRune(lastRune(s[:i]))
		after := i+len(w) >= len(s) || !isWordRune(firstRune(s[i+len(w):]))
		if before && after {
			return true
		}
		at = i + len(w)
	}
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func lastRune(s string) rune {
	var last rune
	for _, r := range s {
		last = r
	}
	return last
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
	if oka && okb && na == nb && hasNegation(a) == hasNegation(b) {
		return trace.AgreementNumeric, true
	}
	return "", false
}

// lastNumber returns the canonical form of the last number in a normalised
// answer. The last one is the answer: a worked calculation states its
// operands first and its result last.
//
// A comma is a thousands separator only in the shape a thousands separator
// has — a digit before it and exactly three digits after — because 、 is
// also the ordinary Japanese comma, and "候補は 1、2、3 です" is a list of
// three numbers rather than the number one hundred and twenty-three.
//
// An exponent is kept verbatim rather than evaluated: 1e5 is one number,
// not the number 5, and it is deliberately not equal to 100000 either. The
// miss is safe; the false agreement would not be.
func lastNumber(s string) (string, bool) {
	r := []rune(s)
	last, found := "", false
	for i := 0; i < len(r); {
		if !unicode.IsDigit(r[i]) {
			i++
			continue
		}
		start := i
		for i < len(r) {
			if unicode.IsDigit(r[i]) {
				i++
				continue
			}
			if isGroupSeparator(r, i) {
				i += 4
				continue
			}
			break
		}
		if i+1 < len(r) && r[i] == '.' && unicode.IsDigit(r[i+1]) {
			for i++; i < len(r) && unicode.IsDigit(r[i]); i++ {
			}
		}
		exponent := false
		if end := exponentEnd(r, i); end > i {
			i, exponent = end, true
		}
		// A hyphen is a sign only where a hyphen inside a word cannot be.
		if start > 0 && r[start-1] == '-' && (start == 1 || !isWordRune(r[start-2])) {
			start--
		}
		token := string(r[start:i])
		if exponent {
			last = token
		} else {
			last = canonicalNumber(token)
		}
		found = true
	}
	return last, found
}

// isGroupSeparator reports whether the comma at i groups thousands: a digit
// before it, exactly three digits after it, and no fourth.
func isGroupSeparator(r []rune, i int) bool {
	if r[i] != ',' && r[i] != '、' {
		return false
	}
	if i == 0 || !unicode.IsDigit(r[i-1]) || i+3 >= len(r) {
		return false
	}
	for k := 1; k <= 3; k++ {
		if !unicode.IsDigit(r[i+k]) {
			return false
		}
	}
	return i+4 >= len(r) || !unicode.IsDigit(r[i+4])
}

// exponentEnd returns the index after an exponent that starts at i, or i
// when there is none.
func exponentEnd(r []rune, i int) int {
	if i >= len(r) || (r[i] != 'e' && r[i] != 'E') {
		return i
	}
	j := i + 1
	if j < len(r) && (r[j] == '+' || r[j] == '-') {
		j++
	}
	if j >= len(r) || !unicode.IsDigit(r[j]) {
		return i
	}
	for j < len(r) && unicode.IsDigit(r[j]) {
		j++
	}
	return j
}

func isWordRune(r rune) bool { return unicode.IsDigit(r) || unicode.IsLetter(r) || r == '_' }

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
