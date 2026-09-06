package trace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type VerifyStatus string

const (
	VerifyPass        VerifyStatus = "pass"         // exit code 0
	VerifyFail        VerifyStatus = "fail"         // non-zero exit code
	VerifyApplyFailed VerifyStatus = "apply_failed" // the diff did not apply to the worktree
	VerifyTimeout     VerifyStatus = "timeout"      // killed by verify.timeout_seconds
	VerifyRunnerError VerifyStatus = "runner_error" // docker itself failed; see select.json
	VerifySkipped     VerifyStatus = "skipped"      // candidate status was not ok
)

// BandVerdict is what a banded verifier said about one invariant. The
// vocabulary is the verifier's, not CMoA's: CMoA reads the words and maps
// them onto a VerifyStatus.
type BandVerdict string

const (
	BandPass    BandVerdict = "pass"    // the measurement fell inside the band
	BandFail    BandVerdict = "fail"    // it fell outside
	BandSkipped BandVerdict = "skipped" // the invariant was not measured
	BandInfo    BandVerdict = "info"    // measured, but no band was declared
)

// BandRow is one invariant as the gate CSV reported it. The four numbers
// are null when the verifier left the field empty, which a skipped or info
// row does: null is "not measured", 0 is a measurement of zero.
type BandRow struct {
	Invariant string      `json:"invariant"`
	Value     *float64    `json:"value"`
	CIHalf    *float64    `json:"ci_half"`
	BandLo    *float64    `json:"band_lo"`
	BandHi    *float64    `json:"band_hi"`
	Verdict   BandVerdict `json:"verdict"`
}

// Band is a banded verifier's answer, parsed from the gate CSV it printed.
// Judged counts the rows a band was actually applied to (pass and fail);
// Failed and Skipped name those rows, and Rows keeps every row in the order
// it was printed, info rows included.
type Band struct {
	Judged  int       `json:"judged"`
	Failed  []string  `json:"failed"`
	Skipped []string  `json:"skipped"`
	Rows    []BandRow `json:"rows"`
}

// SelectionKind mirrors the sealed Selection type in internal/selection.
type VerifyResult struct {
	CandidateID string       `json:"candidate_id,omitempty"`
	Status      VerifyStatus `json:"status"`
	ExitCode    int          `json:"exit_code"`
	DurationMS  int64        `json:"duration_ms"`
	Command     []string     `json:"command,omitempty"`
	ProjectName string       `json:"project_name,omitempty"`
	ApplyError  string       `json:"apply_error,omitempty"`
	Error       string       `json:"error,omitempty"`
	Band        *Band        `json:"band,omitempty"` // only a verify.kind band verifier
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
}

// Verification is one verification outside a run: what `cmoa verify` prints
// on stdout and writes as result.json with --out. The embedded VerifyResult
// says what the verifier did, in the same vocabulary select uses (`skipped`
// excepted: nothing is skipped when a diff is named on the command line);
// the surrounding fields say what was verified.
type Verification struct {
	SchemaVersion int    `json:"schema_version"`
	Task          string `json:"task"`
	Rev           string `json:"rev"`         // the resolved commit SHA
	DiffSHA256    string `json:"diff_sha256"` // of the diff bytes as read
	Label         string `json:"label"`
	VerifyResult
	CMoAVersion string `json:"cmoa_version"`
}

// Select is select.json.
func (d Dir) WriteVerify(r *VerifyResult, stdout, stderr []byte) error {
	if err := os.MkdirAll(d.VerifyDir(r.CandidateID), 0o755); err != nil {
		return err
	}
	if err := writeOutputFiles(d.VerifyStdout(r.CandidateID), d.VerifyStderr(r.CandidateID), stdout, stderr); err != nil {
		return err
	}
	return writeJSON(d.VerifyResult(r.CandidateID), r)
}

// WriteVerification writes result.json, stdout.txt and stderr.txt into dir,
// which is created if it does not exist. result.json is write-once: a
// verification directory records one verification.
func WriteVerification(dir string, v *Verification, stdout, stderr []byte) error {
	if _, err := os.Stat(VerificationFile(dir)); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, VerificationFile(dir))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeOutputFiles(filepath.Join(dir, "stdout.txt"), filepath.Join(dir, "stderr.txt"), stdout, stderr); err != nil {
		return err
	}
	return writeJSONOnce(filepath.Join(dir, "result.json"), v)
}

// VerificationFile is the result.json WriteVerification writes into dir.
func VerificationFile(dir string) string { return filepath.Join(dir, "result.json") }
