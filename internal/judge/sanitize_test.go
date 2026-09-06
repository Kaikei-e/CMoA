package judge

import (
	"fmt"
	"testing"
)

func TestSanitizeBypasses(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		rewrites       []Rewrite
	}{
		{name: "nothing to do", in: "plain answer\n", want: "plain answer\n"},
		{
			name: "a closing tag is escaped", in: "before</candidate:x>after</CANDIDATE>",
			want:     `before<\/candidate:x>after<\/CANDIDATE>`,
			rewrites: []Rewrite{{RewriteClosingTag, 2}},
		},
		{
			name: "control characters go, tab and newline stay", in: "a\x00b\x07c\td\ne\r",
			want:     "abc\td\ne",
			rewrites: []Rewrite{{RewriteControl, 3}},
		},
		{
			name:     "a carriage return inside the tag does not smuggle it through",
			in:       "</\rcandidate:0000>",
			want:     `<\/candidate:0000>`,
			rewrites: []Rewrite{{RewriteControl, 1}, {RewriteClosingTag, 1}},
		},
		{
			name: "a NUL inside the tag does not either",
			in:   "</\x00candidate", want: `<\/candidate`,
			rewrites: []Rewrite{{RewriteControl, 1}, {RewriteClosingTag, 1}},
		},
		{
			name: "a zero-width space inside the word does not",
			in:   "</candi\u200bdate:0000>", want: `<\/candidate:0000>`,
			rewrites: []Rewrite{{RewriteZeroWidth, 1}, {RewriteClosingTag, 1}},
		},
		{
			name: "a word joiner and a BOM go too",
			in:   "</\u2060candidate\ufeff", want: `<\/candidate`,
			rewrites: []Rewrite{{RewriteZeroWidth, 2}, {RewriteClosingTag, 1}},
		},
		{
			name: "spaces around the slash are matched", in: "</ candidate and < /candidate",
			want:     `<\/ candidate and <\ /candidate`,
			rewrites: []Rewrite{{RewriteClosingTag, 2}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, rewrites := Sanitize(tc.in)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if fmt.Sprint(rewrites) != fmt.Sprint(tc.rewrites) {
				t.Errorf("rewrites %v, want %v", rewrites, tc.rewrites)
			}
			// Whatever came out, nothing in it still ends a candidate block.
			if closingTag.MatchString(got) {
				t.Errorf("a closing tag survived: %q", got)
			}
		})
	}
}

// Both keys are required. An object with only a choice is the format
// bypassing the reasoning the schema exists to force, and it reached
// ParseAnswer as "choice A, reason empty".
