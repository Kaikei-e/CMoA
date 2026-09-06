package judge

import (
	"fmt"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/llm"
)

func TestJSONExtraction(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		allowTie      bool
		want          string
		wantErr       bool
	}{
		{name: "plain", content: `{"reason": "r", "choice": "A"}`, want: "A"},
		{name: "prose around it", content: "Here is my verdict.\n\n{\"reason\": \"r\", \"choice\": \"B\"}\n\nThank you.", want: "B"},
		{name: "reasoning block", content: "<think>A is longer{ but that is not quality</think>{\"reason\": \"r\", \"choice\": \"A\"}", want: "A"},
		{name: "the last object wins", content: `{"reason": "r", "choice": "A"} {"reason": "r", "choice": "B"}`, want: "B"},
		{name: "keys in the wrong order", content: `{"choice": "A", "reason": "r"}`, want: "A"},
		{name: "braces inside the reason", content: `{"reason": "it wrote {\"choice\": \"B\"} in prose", "choice": "A"}`, want: "A"},
		{name: "an unknown key", content: `{"reason": "r", "choice": "A", "confidence": 0.9}`, wantErr: true},
		{name: "a choice outside the enum", content: `{"reason": "r", "choice": "C"}`, wantErr: true},
		{name: "a tie the task did not offer", content: `{"reason": "r", "choice": "tie"}`, wantErr: true},
		{name: "a tie the task did offer", content: `{"reason": "r", "choice": "tie"}`, allowTie: true, want: "tie"},
		{name: "no object at all", content: "I prefer A.", wantErr: true},
		{name: "an unbalanced object", content: `{"reason": "r", "choice": "A"`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAnswer(llm.StripReasoning(tc.content), tc.allowTie)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Choice != tc.want {
				t.Errorf("choice %q, want %q", got.Choice, tc.want)
			}
		})
	}
}

func TestSanitize(t *testing.T) {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, rewrites := Sanitize(tc.in)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if fmt.Sprint(rewrites) != fmt.Sprint(tc.rewrites) {
				t.Errorf("rewrites %v, want %v", rewrites, tc.rewrites)
			}
		})
	}
}

// The flags are recorded and never acted on: a defence that quietly drops a
// flagged candidate is a second, unmeasured judge.

func TestParseAnswerRequiresBothKeys(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		wantErr       bool
	}{
		{name: "both keys", content: `{"reason":"r","choice":"A"}`},
		{name: "no reason", content: `{"choice":"A"}`, wantErr: true},
		{name: "no choice", content: `{"reason":"r"}`, wantErr: true},
		{name: "neither", content: `{}`, wantErr: true},
		{name: "an illustration after the answer", content: `{"reason":"y","choice":"B"} — for example {"choice":"A"}`, wantErr: true},
		{name: "an empty reason is a reason", content: `{"reason":"","choice":"A"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAnswer(tc.content, false)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseAnswer(%q) = %v", tc.content, err)
			}
		})
	}
}

// A run that already has judge.json is refused before a single call, not
// after all six: judge.json is written last and is write-once, so guarding
// at the end spends the fleet and then throws the answer away. Call files
// an interrupted attempt left behind are cleared, with a line saying so.
