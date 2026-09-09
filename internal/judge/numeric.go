package judge

import (
	"regexp"
	"strings"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

// A numeric agreement requires the entire normalised answer to be one
// decimal literal. Units, prose, negations, expressions and lists must not
// disappear merely because their last numbers match. Japanese commas are
// excluded because they can separate list items even without whitespace.
var decimalAnswer = regexp.MustCompile(`^-?(?:[0-9]+|[0-9]{1,3}(?:,[0-9]{3})+)(?:\.[0-9]+)?$`)

func agree(a, b string) (trace.ConsensusAgreement, bool) {
	if a != "" && a == b {
		return trace.AgreementExact, true
	}
	na, oka := numericAnswer(a)
	nb, okb := numericAnswer(b)
	if oka && okb && na == nb {
		return trace.AgreementNumeric, true
	}
	return "", false
}

// numericAnswer compares decimal values using strings, avoiding rounding
// and overflow. Unsupported spellings can still agree by exact text.
func numericAnswer(s string) (string, bool) {
	if len(s) > maxNumericRunes || !decimalAnswer.MatchString(s) {
		return "", false
	}
	return canonicalNumber(s), true
}

// canonicalNumber strips the separators and the zeros that carry no value,
// so two spellings of one quantity compare equal as strings.
func canonicalNumber(s string) string {
	s = strings.ReplaceAll(s, ",", "")
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
