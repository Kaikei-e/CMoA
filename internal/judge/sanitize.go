package judge

import (
	"regexp"
	"strings"
)

// Rewrite is one change the sanitiser made.
type Rewrite struct {
	What  string
	Count int
}

// The three rewrites the sanitiser makes, named the way the trace names
// them, in the order they are applied.
const (
	RewriteControl    = "control characters dropped"
	RewriteZeroWidth  = "zero-width characters dropped"
	RewriteClosingTag = "closing-tag-like sequence escaped"
)

// closingTag is deliberately tolerant. A candidate that wants to end its
// own block early will not write the sequence the way the template does,
// and a literal match would let `< /candidate` and `</ candidate` through.
var closingTag = regexp.MustCompile(`(?i)<\s*/\s*candidate`)

// zeroWidth are the invisible runes a candidate can hide inside a closing
// tag: they survive a literal comparison, render as nothing, and — before
// the order of these two passes was fixed — were dropped *after* the escape
// had failed to match, reconstituting the tag the escape was there to
// break.
func zeroWidth(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
		return true
	}
	return false
}

// Sanitize prepares one answer for a candidate block, in the order that
// makes the passes composable: the invisible characters go first, so the
// tag escape sees the text a reader would see, and only then is anything
// that still looks like a closing tag broken.
//
// It reports what it did. Rewriting an answer changes what is judged, so a
// silent rewrite would make an outcome unexplainable — and a trace that
// claims an escape it never applied is worse than one that claims nothing.
func Sanitize(s string) (string, []Rewrite) {
	var out []Rewrite
	controls, invisible := 0, 0
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f:
			controls++
			return -1
		case zeroWidth(r):
			invisible++
			return -1
		}
		return r
	}, s)
	if controls > 0 {
		out = append(out, Rewrite{What: RewriteControl, Count: controls})
	}
	if invisible > 0 {
		out = append(out, Rewrite{What: RewriteZeroWidth, Count: invisible})
	}
	if n := len(closingTag.FindAllString(s, -1)); n > 0 {
		// Escape the opening angle bracket and leave the rest as written:
		// the block stops looking like a closing tag without the record
		// losing what the candidate actually said.
		s = closingTag.ReplaceAllStringFunc(s, func(m string) string { return `<\` + m[1:] })
		out = append(out, Rewrite{What: RewriteClosingTag, Count: n})
	}
	return s, out
}

// injectionPatterns are the phrases a candidate uses when it is addressing
// the judge rather than the task. They are recorded and never acted on: a
// defence that silently discards a flagged candidate is an unmeasured
// second judge, and the flag's value is that a calibration can ask whether
// flagged candidates win more often than they should.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(?:all\s+|any\s+|the\s+)?(?:previous|prior|above)\s+instructions`),
	regexp.MustCompile(`(?i)you\s+are\s+now`),
	regexp.MustCompile(`(?i)system\s+prompt`),
	regexp.MustCompile(`(?i)as\s+the\s+judge`),
	// The labels are matched case-sensitively: read case-insensitively this
	// flags "choose a library", and a flag that fires on ordinary English
	// destroys the only question it exists to answer.
	regexp.MustCompile(`(?i:choose\s+)(?:candidate\s+)?[AB]\b`),
}

// InjectionFlags lists, without repeats, the injection-shaped phrases the
// answer holds. The text is recorded as it was written, whitespace folded:
// the labels are case-sensitive, so lower-casing the match would print
// "choose a" for a phrase that only fires on "choose A".
func InjectionFlags(s string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, re := range injectionPatterns {
		for _, m := range re.FindAllString(s, -1) {
			m = strings.Join(strings.Fields(m), " ")
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}
