package judge

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

// ParseAnswer reads the judge's object out of a completion. The last
// balanced object wins: a model that reasons in prose before answering
// leaves earlier braces behind, and the answer is the one it finished with.
func ParseAnswer(content string, allowTie bool) (*trace.JudgeAnswer, error) {
	obj, err := LastObject(content)
	if err != nil {
		return nil, err
	}
	// Pointers, so a key that is absent is told from a key that is empty.
	// DisallowUnknownFields catches the extra key; nothing but this catches
	// the missing one, and an object with no reason is the format bypassing
	// the reasoning the schema exists to force.
	var raw struct {
		Reason *string `json:"reason"`
		Choice *string `json:"choice"`
	}
	dec := json.NewDecoder(strings.NewReader(obj))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	switch {
	case raw.Reason == nil:
		return nil, errors.New(`reason: is required`)
	case raw.Choice == nil:
		return nil, errors.New(`choice: is required`)
	}
	a := trace.JudgeAnswer{Reason: *raw.Reason, Choice: *raw.Choice}
	switch a.Choice {
	case trace.ChoiceA, trace.ChoiceB:
	case trace.ChoiceTie:
		if !allowTie {
			return nil, errors.New(`choice: "tie" is not offered by this task`)
		}
	default:
		return nil, fmt.Errorf("choice: %q is not a choice", a.Choice)
	}
	return &a, nil
}

// LastObject returns the last balanced brace-delimited span of s, ignoring
// braces inside JSON strings.
func LastObject(s string) (string, error) {
	depth, start, last := 0, -1, ""
	inString, escaped := false, false
	for i, r := range s {
		switch {
		case escaped:
			escaped = false
		case inString && r == '\\':
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
		case r == '{':
			if depth == 0 {
				start = i
			}
			depth++
		case r == '}':
			if depth > 0 {
				depth--
				if depth == 0 {
					last = s[start : i+1]
				}
			}
		}
	}
	if last == "" {
		return "", errors.New("no JSON object in the completion")
	}
	return last, nil
}
