package judge

import (
	"strings"
	"unicode"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

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
