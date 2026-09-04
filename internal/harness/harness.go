// Package harness records which harness a run read: the DocDag vault, the
// day the question was asked for (as_of, valid time) and the revision the
// vault was at (at, transaction time), together with the binding documents
// `docdag query --binding` listed. With as_of and at, the same view is
// reconstructed later by `docdag --as-of <as_of> --at <at> query --binding`.
//
// CMoA does not import DocDag; it runs the binary. This is the one of the
// four DocDag entry points v0 uses.
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Snapshot is what a run read.
type Snapshot struct {
	Vault         string
	AsOf          string // YYYY-MM-DD
	At            string // git commit SHA, suffixed "-dirty" when the tree had changes
	DocdagVersion string
	Binding       []Doc
}

// Doc is one binding document.
type Doc struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Path   string `json:"path"`
}

var dayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ErrNoVault is wrapped when the vault directory or its docdag.yaml is absent.
var ErrNoVault = errors.New("harness: vault not found")

// Take runs docdag against vault. asOf empty means today (UTC). docdagBin is
// resolved through PATH when it has no separator.
func Take(ctx context.Context, vault, docdagBin, asOf string) (*Snapshot, error) {
	if asOf == "" {
		asOf = time.Now().UTC().Format("2006-01-02")
	}
	if !dayPattern.MatchString(asOf) {
		return nil, fmt.Errorf("harness: as_of %q is not YYYY-MM-DD", asOf)
	}
	if st, err := os.Stat(vault); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNoVault, vault)
	}
	if _, err := os.Stat(filepath.Join(vault, "docdag.yaml")); err != nil {
		return nil, fmt.Errorf("%w: %s has no docdag.yaml", ErrNoVault, vault)
	}
	bin, err := exec.LookPath(docdagBin)
	if err != nil {
		return nil, fmt.Errorf("harness: docdag binary %q: %w", docdagBin, err)
	}

	at, err := revision(ctx, vault)
	if err != nil {
		return nil, err
	}
	version, err := run(ctx, vault, bin, "--version")
	if err != nil {
		return nil, err
	}
	out, err := run(ctx, vault, bin, "query", "--binding", "--fields", "id,title,status,path", "--format", "json", "--as-of", asOf)
	if err != nil {
		return nil, err
	}
	var docs []Doc
	if err := json.Unmarshal(out, &docs); err != nil {
		return nil, fmt.Errorf("harness: decode docdag query output: %w", err)
	}
	if docs == nil {
		docs = []Doc{}
	}
	return &Snapshot{
		Vault:         vault,
		AsOf:          asOf,
		At:            at,
		DocdagVersion: strings.TrimSpace(strings.TrimPrefix(string(version), "docdag version ")),
		Binding:       docs,
	}, nil
}

// Error carries the failing command and its stderr.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	return fmt.Sprintf("harness: %s: %v: %s", strings.Join(e.Args, " "), e.Err, strings.TrimSpace(e.Stderr))
}

func (e *Error) Unwrap() error { return e.Err }

func run(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &Error{Args: append([]string{filepath.Base(bin)}, args...), Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

func revision(ctx context.Context, vault string) (string, error) {
	sha, err := run(ctx, vault, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	status, err := run(ctx, vault, "git", "status", "--porcelain")
	if err != nil {
		return "", err
	}
	at := strings.TrimSpace(string(sha))
	if len(bytes.TrimSpace(status)) > 0 {
		at += "-dirty"
	}
	return at, nil
}
