// Package patch turns a proposer's answer into something git can apply. A
// small model's diff is rarely pristine: it comes fenced, with CRLF, with
// hunk counts that do not add up, sometimes with prose around it. Extract
// finds the diff and normalises it; Apply hands it to git apply with the
// flags that forgive the usual damage; Stats summarises it for the trace.
package patch

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// ErrNoDiff means the text held nothing that looks like a unified diff.
var ErrNoDiff = errors.New("patch: no unified diff found")

var (
	fenceOpen  = regexp.MustCompile("(?m)^[ \t]*```[ \t]*(?:diff|patch)?[ \t]*$")
	fenceClose = regexp.MustCompile("(?m)^[ \t]*```[ \t]*$")
	// A diff starts at a git header, or at a ---/+++ pair (prefixes optional).
	diffStart = regexp.MustCompile(`(?m)^(diff --git |--- \S[^\n]*\n\+\+\+ |Index: )`)
)

// Extract returns the unified diff in content, normalised: LF line endings,
// fences removed, leading prose dropped, trailing newline guaranteed. When
// several fenced blocks hold diffs they are concatenated in order. It
// returns ErrNoDiff when nothing diff-shaped is present.
func Extract(content string) (string, error) {
	s := strings.ReplaceAll(content, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var blocks []string
	rest := s
	for {
		open := fenceOpen.FindStringIndex(rest)
		if open == nil {
			break
		}
		body := rest[open[1]:]
		body = strings.TrimPrefix(body, "\n")
		close := fenceClose.FindStringIndex(body)
		var block string
		if close == nil {
			block = body // unterminated fence: model hit max_tokens; keep what there is
			rest = ""
		} else {
			block = body[:close[0]]
			rest = body[close[1]:]
		}
		if diffStart.MatchString(block) {
			blocks = append(blocks, block)
		}
		if rest == "" {
			break
		}
	}
	var diff string
	switch {
	case len(blocks) > 0:
		diff = strings.Join(blocks, "\n")
	default:
		loc := diffStart.FindStringIndex(s)
		if loc == nil {
			return "", ErrNoDiff
		}
		diff = s[loc[0]:]
	}
	diff = trimToDiff(diff)
	if !diffStart.MatchString(diff) {
		return "", ErrNoDiff
	}
	diff = normalizeHeaders(diff)
	if !strings.HasSuffix(diff, "\n") {
		diff += "\n"
	}
	return diff, nil
}

// normalizeHeaders gives every file block the git shape: `---`/`+++` paths
// carry a/ and b/ prefixes and a `diff --git` line precedes them. Small
// models often omit the prefixes or the header; with `-p1` git would then
// strip the first real path component. A synthesised header makes git read
// the paths from it and ignore what follows (git-apply(1), "-p<n>").
func normalizeHeaders(diff string) string {
	lines := strings.Split(diff, "\n")
	out := make([]string, 0, len(lines)+8)
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if !strings.HasPrefix(l, "--- ") || i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
			out = append(out, l)
			continue
		}
		oldPath := headerPath(l[4:])
		newPath := headerPath(lines[i+1][4:])
		if oldPath == "" && newPath == "" {
			out = append(out, l)
			continue
		}
		name := newPath
		if name == "" {
			name = oldPath
		}
		hasGitHeader := len(out) > 0 && strings.HasPrefix(lastHeaderLine(out), "diff --git ")
		if !hasGitHeader {
			out = append(out, "diff --git a/"+name+" b/"+name)
			switch {
			case oldPath == "":
				out = append(out, "new file mode 100644")
			case newPath == "":
				out = append(out, "deleted file mode 100644")
			}
		}
		if oldPath == "" {
			out = append(out, "--- /dev/null")
		} else {
			out = append(out, "--- a/"+oldPath)
		}
		if newPath == "" {
			out = append(out, "+++ /dev/null")
		} else {
			out = append(out, "+++ b/"+newPath)
		}
		i++
	}
	return strings.Join(out, "\n")
}

// lastHeaderLine returns the nearest preceding line that is not a file
// mode/index line, so a `diff --git` followed by `new file mode` is found.
func lastHeaderLine(out []string) string {
	for j := len(out) - 1; j >= 0; j-- {
		l := out[j]
		if strings.HasPrefix(l, "new file mode") || strings.HasPrefix(l, "deleted file mode") ||
			strings.HasPrefix(l, "index ") || strings.HasPrefix(l, "old mode") || strings.HasPrefix(l, "new mode") ||
			strings.HasPrefix(l, "similarity index") || strings.HasPrefix(l, "rename ") {
			continue
		}
		return l
	}
	return ""
}

// headerPath strips a/ or b/ prefixes and a trailing tab-timestamp; it
// returns "" for /dev/null.
func headerPath(p string) string {
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSpace(p)
	if p == "/dev/null" {
		return ""
	}
	p = strings.TrimPrefix(p, "a/")
	p = strings.TrimPrefix(p, "b/")
	p = strings.TrimPrefix(p, "./")
	return p
}

// trimToDiff drops leading lines before the first diff header and trailing
// prose after the last hunk line.
func trimToDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	start := 0
	for i, l := range lines {
		if diffStart.MatchString(l) {
			start = i
			break
		}
	}
	end := len(lines)
	for end > start && !isDiffLine(lines[end-1]) {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

func isDiffLine(l string) bool {
	if l == "" {
		return false
	}
	switch l[0] {
	case '+', '-', ' ', '@', '\\':
		return true
	}
	return strings.HasPrefix(l, "diff ") || strings.HasPrefix(l, "index ") ||
		strings.HasPrefix(l, "new file mode") || strings.HasPrefix(l, "deleted file mode") ||
		strings.HasPrefix(l, "similarity index") || strings.HasPrefix(l, "rename ") ||
		strings.HasPrefix(l, "old mode") || strings.HasPrefix(l, "new mode") ||
		strings.HasPrefix(l, "Binary files")
}

// Stats summarises a diff.
type Stats struct {
	Files     []string // paths touched (b/ side, or a/ side for deletions), in order
	Additions int
	Deletions int
	SHA256    string
}

var (
	gitHeader = regexp.MustCompile(`^diff --git a/(.+?) b/(.+)$`)
	minusHdr  = regexp.MustCompile(`^--- (?:a/)?(.+?)(?:\t.*)?$`)
	plusHdr   = regexp.MustCompile(`^\+\+\+ (?:b/)?(.+?)(?:\t.*)?$`)
)

// Stats counts files and lines. It does not validate hunk counts; git apply
// --recount does.
func ComputeStats(diff string) (Stats, error) {
	var st Stats
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || p == "/dev/null" || seen[p] {
			return
		}
		seen[p] = true
		st.Files = append(st.Files, p)
	}
	sc := bufio.NewScanner(strings.NewReader(diff))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	inHunk := false
	var pendingMinus string
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "diff --git "):
			inHunk = false
			if m := gitHeader.FindStringSubmatch(l); m != nil {
				add(m[2])
			}
		case strings.HasPrefix(l, "--- "):
			inHunk = false
			if m := minusHdr.FindStringSubmatch(l); m != nil {
				pendingMinus = m[1]
			}
		case strings.HasPrefix(l, "+++ "):
			inHunk = false
			if m := plusHdr.FindStringSubmatch(l); m != nil {
				if m[1] == "/dev/null" {
					add(pendingMinus)
				} else {
					add(m[1])
				}
			}
		case strings.HasPrefix(l, "@@"):
			inHunk = true
		case inHunk && strings.HasPrefix(l, "+"):
			st.Additions++
		case inHunk && strings.HasPrefix(l, "-"):
			st.Deletions++
		}
	}
	if err := sc.Err(); err != nil {
		return st, err
	}
	if len(st.Files) == 0 {
		return st, ErrNoDiff
	}
	sum := sha256.Sum256([]byte(diff))
	st.SHA256 = hex.EncodeToString(sum[:])
	return st, nil
}

// ApplyError carries git apply's stderr.
type ApplyError struct {
	Stderr string
	Err    error
}

func (e *ApplyError) Error() string {
	return fmt.Sprintf("patch: git apply: %v: %s", e.Err, strings.TrimSpace(e.Stderr))
}

func (e *ApplyError) Unwrap() error { return e.Err }

// Apply applies diff inside dir with git apply. --recount forgives wrong
// hunk line counts, --ignore-whitespace forgives drifted context
// whitespace, --whitespace=nowarn keeps trailing-space warnings out of the
// trace. Paths in the diff are relative to dir. A diff that reaches outside
// dir (../, symlinks) is refused by git itself.
func Apply(ctx context.Context, dir, diff string) error {
	if strings.TrimSpace(diff) == "" {
		return &ApplyError{Err: ErrNoDiff}
	}
	cmd := exec.CommandContext(ctx, "git", "apply", "--recount", "--ignore-whitespace", "--whitespace=nowarn", "-")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(diff)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &ApplyError{Stderr: stderr.String(), Err: err}
	}
	return nil
}
