package task

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// loadDoctor fills Reference, Mutants and Doctor, reading every declared diff.
func loadDoctor(t *Task, m *manifest) error {
	t.Doctor = DoctorSpec{KillRateMin: DefaultKillRateMin, ReferenceRuns: DefaultReferenceRuns}
	if d := m.Doctor; d != nil {
		if d.KillRateMin != nil {
			if *d.KillRateMin <= 0 || *d.KillRateMin > 1 {
				return &ValidationError{"doctor.kill_rate_min", fmt.Sprintf("%v is outside (0, 1]", *d.KillRateMin)}
			}
			t.Doctor.KillRateMin = *d.KillRateMin
		}
		if d.ReferenceRuns != nil {
			if *d.ReferenceRuns < 1 {
				return &ValidationError{"doctor.reference_runs", "must be at least 1"}
			}
			t.Doctor.ReferenceRuns = *d.ReferenceRuns
		}
	}
	if r := m.Reference; r != nil {
		diff, err := t.readDiff(r.Diff, allowEmpty)
		if err != nil {
			return &ValidationError{"reference.diff", err.Error()}
		}
		t.Reference = &Reference{Path: filepath.ToSlash(filepath.Clean(r.Diff)), Diff: diff}
	}
	seen := map[string]bool{}
	for i, mm := range m.Mutants {
		at := fmt.Sprintf("mutants[%d]", i)
		diff, err := t.readDiff(mm.Diff, requireDiff)
		if err != nil {
			return &ValidationError{at + ".diff", err.Error()}
		}
		path := filepath.ToSlash(filepath.Clean(mm.Diff))
		if seen[path] {
			return &ValidationError{at + ".diff", "duplicate path " + path}
		}
		seen[path] = true
		expect := MutantExpect(mm.Expect)
		if expect == "" {
			expect = ExpectKilled
		}
		switch expect {
		case ExpectKilled, ExpectEquivalent:
		default:
			return &ValidationError{at + ".expect", fmt.Sprintf("%q is not an expectation; one of [%s %s]", mm.Expect, ExpectKilled, ExpectEquivalent)}
		}
		origin := MutantOrigin(mm.Origin)
		if origin == "" {
			origin = OriginHand
		}
		switch origin {
		case OriginHand, OriginGenerated:
		default:
			return &ValidationError{at + ".origin", fmt.Sprintf("%q is not an origin; one of [%s %s]", mm.Origin, OriginHand, OriginGenerated)}
		}
		t.Mutants = append(t.Mutants, Mutant{Path: path, Diff: diff, Expect: expect, Origin: origin, Operator: mm.Operator, Note: mm.Note})
	}
	return nil
}

type emptyDiff bool

const (
	requireDiff emptyDiff = false
	allowEmpty  emptyDiff = true
)

func (t *Task) readDiff(p string, empty emptyDiff) (string, error) {
	clean, b, err := t.readTaskPath(p)
	if err != nil {
		return "", err
	}
	if !empty && strings.TrimSpace(string(b)) == "" {
		return "", errors.New(clean + " is empty")
	}
	return string(b), nil
}
