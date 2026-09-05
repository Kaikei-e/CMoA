// Package band reads the answer of a banded verifier. Such a verifier does
// not say pass or fail with its exit status: it measures a set of
// invariants and prints, somewhere on its stdout, a CSV block whose header
// line is exactly
//
//	invariant,value,ci_half,band_lo,band_hi,verdict
//
// followed by one row per invariant. Everything else on stdout is the
// container's own logging and is ignored, so a verifier need not keep its
// output clean. When several blocks appear the last one is read: a gate
// that reruns a measurement prints the run that counts last.
//
// The package only reads. What a block means for one verification — which
// verdict makes a `fail`, which makes a `runner_error` — is decided by
// cmd/cmoa, where the rest of the vocabulary lives.
package band

import (
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

// Columns are the gate CSV's fields, in order. Header is the line that must
// appear verbatim for a block to be recognised.
var Columns = []string{"invariant", "value", "ci_half", "band_lo", "band_hi", "verdict"}

// Header is the exact header line of a gate CSV block.
var Header = strings.Join(Columns, ",")

// ErrNoCSV means stdout held no gate CSV: no header line, or a header with
// no rows under it. Either way the verifier did not answer, which is a
// broken harness rather than a failing candidate.
var ErrNoCSV = errors.New("band verifier printed no gate CSV")

// Parse reads the last gate CSV block on stdout. It returns ErrNoCSV when
// there is none, and a descriptive error when a line inside a block is not
// a row a verdict can be read from.
func Parse(stdout []byte) (*trace.Band, error) {
	lines := strings.Split(strings.ReplaceAll(string(stdout), "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == Header {
			start = i
		}
	}
	if start < 0 {
		return nil, ErrNoCSV
	}
	b := &trace.Band{Failed: []string{}, Skipped: []string{}, Rows: []trace.BandRow{}}
	for i := start + 1; i < len(lines); i++ {
		rec, ok := record(lines[i])
		if !ok {
			// Not a six-field record: the block has ended and the rest of
			// stdout is logging again.
			break
		}
		row, err := parseRow(rec)
		if err != nil {
			return nil, fmt.Errorf("band verifier printed a malformed gate CSV: line %d: %w", i+1, err)
		}
		b.Rows = append(b.Rows, row)
		switch row.Verdict {
		case trace.BandPass:
			b.Judged++
		case trace.BandFail:
			b.Judged++
			b.Failed = append(b.Failed, row.Invariant)
		case trace.BandSkipped:
			b.Skipped = append(b.Skipped, row.Invariant)
		case trace.BandInfo:
			// Reported, never judged: an info row carries a measurement no
			// band was declared for.
		}
	}
	if len(b.Rows) == 0 {
		return nil, ErrNoCSV
	}
	return b, nil
}

// record reads one line as a CSV record of exactly len(Columns) fields.
// Anything else — a blank line, a log line, a row of the wrong width — is
// not part of the block.
func record(line string) ([]string, bool) {
	if strings.TrimSpace(line) == "" {
		return nil, false
	}
	r := csv.NewReader(strings.NewReader(line))
	r.FieldsPerRecord = len(Columns)
	rec, err := r.Read()
	if err != nil {
		return nil, false
	}
	return rec, true
}

// parseRow turns one record into a BandRow. The four numbers may be empty —
// a skipped or info row has nothing to report — but what is written must be
// a finite number, because the row is about to become JSON.
func parseRow(rec []string) (trace.BandRow, error) {
	row := trace.BandRow{Invariant: strings.TrimSpace(rec[0])}
	if row.Invariant == "" {
		return row, errors.New("the invariant name is empty")
	}
	nums := [...]**float64{&row.Value, &row.CIHalf, &row.BandLo, &row.BandHi}
	for i, field := range nums {
		n, err := parseNumber(rec[i+1])
		if err != nil {
			return row, fmt.Errorf("%s: %w", Columns[i+1], err)
		}
		*field = n
	}
	row.Verdict = trace.BandVerdict(strings.TrimSpace(rec[5]))
	switch row.Verdict {
	case trace.BandPass, trace.BandFail, trace.BandSkipped, trace.BandInfo:
	default:
		return row, fmt.Errorf("verdict: %q is not a verdict; one of [%s %s %s %s]",
			rec[5], trace.BandPass, trace.BandFail, trace.BandSkipped, trace.BandInfo)
	}
	return row, nil
}

// parseNumber reads one measurement. An empty field is a measurement that
// was not taken, which is null in the trace, not zero.
func parseNumber(s string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not a number", s)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, fmt.Errorf("%q is not a finite number", s)
	}
	return &f, nil
}
