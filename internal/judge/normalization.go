package judge

import (
	"strings"
	"unicode"
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
