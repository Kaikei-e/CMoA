package band

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/Kaikei-e/CMoA/internal/trace"
)

// f is a *float64 literal: the trace tells "not measured" (null) from a
// measurement of zero, so the tests have to as well.
func f(v float64) *float64 { return &v }

func TestHeader(t *testing.T) {
	// The contract is the literal line, not whatever Columns happens to say.
	if Header != "invariant,value,ci_half,band_lo,band_hi,verdict" {
		t.Fatalf("header = %q", Header)
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		stdout  string
		judged  int
		failed  []string
		skipped []string
		rows    []trace.BandRow
	}{
		{
			name: "one block among logs",
			stdout: "building...\n" +
				"warning: something\n" +
				Header + "\n" +
				"p99_latency_ms,12.5,0.4,0,15,pass\n" +
				"done in 3s\n",
			judged:  1,
			failed:  []string{},
			skipped: []string{},
			rows: []trace.BandRow{
				{Invariant: "p99_latency_ms", Value: f(12.5), CIHalf: f(0.4), BandLo: f(0), BandHi: f(15), Verdict: trace.BandPass},
			},
		},
		{
			name: "the last block wins",
			stdout: Header + "\nthroughput_rps,900,10,1000,2000,fail\n" +
				"retrying the measurement\n" +
				Header + "\nthroughput_rps,1400,12,1000,2000,pass\n",
			judged:  1,
			failed:  []string{},
			skipped: []string{},
			rows: []trace.BandRow{
				{Invariant: "throughput_rps", Value: f(1400), CIHalf: f(12), BandLo: f(1000), BandHi: f(2000), Verdict: trace.BandPass},
			},
		},
		{
			name: "skipped and info rows carry no numbers",
			stdout: Header + "\n" +
				"p99_latency_ms,12.5,0.4,0,15,pass\n" +
				"tail_alloc_bytes,,,,,skipped\n" +
				"cpu_seconds,4.25,,,,info\n" +
				"error_rate,0.03,0.001,0,0.01,fail\n",
			judged:  2,
			failed:  []string{"error_rate"},
			skipped: []string{"tail_alloc_bytes"},
			rows: []trace.BandRow{
				{Invariant: "p99_latency_ms", Value: f(12.5), CIHalf: f(0.4), BandLo: f(0), BandHi: f(15), Verdict: trace.BandPass},
				{Invariant: "tail_alloc_bytes", Verdict: trace.BandSkipped},
				{Invariant: "cpu_seconds", Value: f(4.25), Verdict: trace.BandInfo},
				{Invariant: "error_rate", Value: f(0.03), CIHalf: f(0.001), BandLo: f(0), BandHi: f(0.01), Verdict: trace.BandFail},
			},
		},
		{
			name:    "surrounding space and exponents",
			stdout:  Header + "\n alloc_bytes , 1.5e6 , 2e3 , 0 , 2e6 , pass \n",
			judged:  1,
			failed:  []string{},
			skipped: []string{},
			rows: []trace.BandRow{
				{Invariant: "alloc_bytes", Value: f(1.5e6), CIHalf: f(2e3), BandLo: f(0), BandHi: f(2e6), Verdict: trace.BandPass},
			},
		},
		{
			name:    "a quoted field holding a comma",
			stdout:  Header + "\n\"p99, cold\",12.5,0.4,0,15,pass\n",
			judged:  1,
			failed:  []string{},
			skipped: []string{},
			rows: []trace.BandRow{
				{Invariant: "p99, cold", Value: f(12.5), CIHalf: f(0.4), BandLo: f(0), BandHi: f(15), Verdict: trace.BandPass},
			},
		},
		{
			name:    "a log line of the wrong width ends the block",
			stdout:  Header + "\nrps,900,10,800,1000,pass\nteardown\nrps,0,0,800,1000,fail\n",
			judged:  1,
			failed:  []string{},
			skipped: []string{},
			rows: []trace.BandRow{
				{Invariant: "rps", Value: f(900), CIHalf: f(10), BandLo: f(800), BandHi: f(1000), Verdict: trace.BandPass},
			},
		},
		{
			name:    "CRLF and no trailing newline",
			stdout:  "log\r\n" + Header + "\r\nrps,900,10,800,1000,pass",
			judged:  1,
			failed:  []string{},
			skipped: []string{},
			rows: []trace.BandRow{
				{Invariant: "rps", Value: f(900), CIHalf: f(10), BandLo: f(800), BandHi: f(1000), Verdict: trace.BandPass},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := Parse([]byte(c.stdout))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if b.Judged != c.judged {
				t.Errorf("judged = %d, want %d", b.Judged, c.judged)
			}
			if !equal(b.Failed, c.failed) {
				t.Errorf("failed = %v, want %v", b.Failed, c.failed)
			}
			if !equal(b.Skipped, c.skipped) {
				t.Errorf("skipped = %v, want %v", b.Skipped, c.skipped)
			}
			if len(b.Rows) != len(c.rows) {
				t.Fatalf("rows = %+v, want %+v", b.Rows, c.rows)
			}
			for i, want := range c.rows {
				if !sameRow(b.Rows[i], want) {
					t.Errorf("rows[%d] = %s, want %s", i, show(b.Rows[i]), show(want))
				}
			}
		})
	}
}

func TestParseNoCSV(t *testing.T) {
	cases := map[string]string{
		"empty output":       "",
		"logs only":          "building...\ndone\n",
		"header misspelled":  "invariant,value,ci_half,band_lo,band_high,verdict\nrps,1,0,0,2,pass\n",
		"header with extras": "gate: " + Header + "\nrps,1,0,0,2,pass\n",
		"header, no rows":    "log\n" + Header + "\n",
		"header, then a log": Header + "\nall invariants held\n",
		"header last":        "rps,1,0,0,2,pass\n" + Header + "\n",
	}
	for name, stdout := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := Parse([]byte(stdout))
			if !errors.Is(err, ErrNoCSV) || b != nil {
				t.Fatalf("Parse = %+v, %v", b, err)
			}
			if err.Error() != "band verifier printed no gate CSV" {
				t.Fatalf("error text = %q", err)
			}
		})
	}
}

// A line inside a block that is six fields wide but is not a row is a
// broken harness, not a failing candidate: the caller must not read it as a
// verdict it does not carry.
func TestParseMalformedRow(t *testing.T) {
	cases := map[string]string{
		"unknown verdict":      "rps,900,10,800,1000,ok\n",
		"empty verdict":        "rps,900,10,800,1000,\n",
		"unnamed row":          " ,900,10,800,1000,pass\n",
		"value not a number":   "rps,fast,10,800,1000,pass\n",
		"band_hi not a number": "rps,900,10,800,many,pass\n",
		"not a finite value":   "rps,NaN,10,800,1000,pass\n",
		"infinite bound":       "rps,900,10,800,+Inf,pass\n",
	}
	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := Parse([]byte(Header + "\n" + row))
			if err == nil || b != nil {
				t.Fatalf("Parse = %+v, %v", b, err)
			}
			if errors.Is(err, ErrNoCSV) {
				t.Fatalf("a malformed row is not a missing CSV: %v", err)
			}
			if !strings.Contains(err.Error(), "malformed gate CSV") || !strings.Contains(err.Error(), "line 2") {
				t.Fatalf("error text = %q", err)
			}
		})
	}
	// A good row before the bad one does not rescue the block.
	if _, err := Parse([]byte(Header + "\nrps,900,10,800,1000,pass\nerrs,1,0,0,0,maybe\n")); err == nil {
		t.Fatal("a malformed second row must fail the block")
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sameRow(a, b trace.BandRow) bool {
	if a.Invariant != b.Invariant || a.Verdict != b.Verdict {
		return false
	}
	for _, pair := range [][2]*float64{{a.Value, b.Value}, {a.CIHalf, b.CIHalf}, {a.BandLo, b.BandLo}, {a.BandHi, b.BandHi}} {
		switch {
		case pair[0] == nil && pair[1] == nil:
		case pair[0] == nil || pair[1] == nil:
			return false
		case *pair[0] != *pair[1]:
			return false
		}
	}
	return true
}

func show(r trace.BandRow) string {
	var b strings.Builder
	b.WriteString(r.Invariant)
	for _, n := range []*float64{r.Value, r.CIHalf, r.BandLo, r.BandHi} {
		b.WriteByte(',')
		if n == nil {
			b.WriteString("null")
			continue
		}
		b.WriteString(strconv.FormatFloat(*n, 'g', -1, 64))
	}
	b.WriteByte(',')
	b.WriteString(string(r.Verdict))
	return b.String()
}
