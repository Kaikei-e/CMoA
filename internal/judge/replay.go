package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// Call names one judge call inside a run: which pair, which order of it,
// and which attempt — 0, or 1 for the single retry a malformed answer
// earns.
//
// The identity travels with the request because a replayer answers from a
// record indexed by it. Matching on the request bytes instead would look
// tidier and would be wrong: the recorded calls were made under whatever
// the judge configuration said that day, and a run that has since changed
// its max_tokens would find no match for any of its six calls and fail for
// a reason that has nothing to do with what the judge said.
type Call struct {
	Pair    int
	Order   string
	Attempt int
}

// Completer performs one judge call. It is an interface with exactly one
// implementation that speaks HTTP and one that speaks to a directory: the
// aggregation above it — consensus, the Copeland score, the tie-break
// chain — is a pure function of the answers, and it is worth being able to
// run it over answers that have already been given.
type Completer interface {
	ChatCompletion(ctx context.Context, call Call, req llm.Request) (*llm.Response, error)
}

// Live is the Completer that asks the endpoint. It drops the call
// identity, which is a replayer's business and no server's.
type Live struct{ Client *llm.Client }

// ChatCompletion sends the request.
func (l Live) ChatCompletion(ctx context.Context, _ Call, req llm.Request) (*llm.Response, error) {
	return l.Client.ChatCompletion(ctx, req)
}

// ErrReplay is every refusal a replay can make.
var ErrReplay = errors.New("judge: replay")

// Replayer answers judge calls out of a run that was already made.
//
// It never opens a socket. What it hands back for a call is the attempt the
// source recorded at the same position, with the request and response bytes
// as they were, so the answer is re-parsed by the same ParseAnswer and
// re-aggregated by the same rules rather than copied from the source's
// verdict. A replay that reproduces the source's outcome has therefore
// shown the aggregation agrees with itself; a replay that does not has
// found a change in the rule, which is the entire point of running one.
type Replayer struct {
	dir    trace.Dir
	report *trace.JudgeReport
	calls  map[string]*trace.JudgeCall
	record *trace.Replayed
}

// OpenReplay reads a run's judge record and its call files.
func OpenReplay(dir trace.Dir) (*Replayer, error) {
	report, err := dir.ReadJudge()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReplay, err)
	}
	judgeSum, err := digest(dir.JudgeFile())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReplay, err)
	}
	r := &Replayer{
		dir:    dir,
		report: report,
		calls:  map[string]*trace.JudgeCall{},
		record: &trace.Replayed{
			RunID: report.RunID, Dir: filepath.ToSlash(string(dir)),
			PromptVersion: report.Judge.PromptVersion, JudgeSHA256: judgeSum,
			Calls: []trace.ReplayedCall{},
		},
	}
	names, err := filepath.Glob(filepath.Join(dir.JudgeDir(), "*.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReplay, err)
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := os.ReadFile(name) //nolint:gosec // a trace file the caller named
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrReplay, err)
		}
		var call trace.JudgeCall
		if err := json.Unmarshal(body, &call); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrReplay, name, err)
		}
		key := trace.JudgeCallName(call.Pair, call.Order)
		r.calls[key] = &call
		r.record.Calls = append(r.record.Calls, trace.ReplayedCall{File: key, SHA256: llm.SHA256(body)})
	}
	return r, nil
}

// PromptVersion is the version of the judge prompt the source was asked
// with.
func (r *Replayer) PromptVersion() string { return r.report.Judge.PromptVersion }

// Seed is the presentation seed the source used. A replay passes it back so
// the nonce inside the candidate fences is the one the recorded answers were
// given for.
func (r *Replayer) Seed() int64 { return r.report.Presentation.Seed }

// Outcome is what the source concluded, for a caller that wants to compare.
func (r *Replayer) Outcome() trace.JudgeOutcome { return r.report.Outcome }

// Record is what run.json says about the source.
func (r *Replayer) Record() *trace.Replayed { return r.record }

// Params are the judge parameters the recorded calls were made under,
// merged onto base.
//
// A replay takes them from the record rather than from today's cmoa.json,
// so the trace it writes describes the calls it actually replayed. The
// alternative — writing the current configuration over answers produced by
// another one — makes a trace that contradicts its own call files, and the
// contradiction is silent.
func (r *Replayer) Params(base *config.Judge) *config.Judge {
	out := *base
	p := r.report.Judge
	temperature := p.Temperature
	out.BaseURL, out.Model = p.BaseURL, p.Model
	out.Temperature, out.MaxTokens, out.Seed = &temperature, p.MaxTokens, p.Seed
	out.OutputFormat = config.JudgeOutputFormat(p.OutputFormat)
	out.ExtraBody = p.ExtraBody
	if p.Parallel > 0 {
		out.Parallel = p.Parallel
	}
	return &out
}

// ChatCompletion answers one call from the record.
//
// A recorded transport failure comes back as a failure and a recorded
// timeout as a timeout, because a replay that quietly turned either into an
// answer would report a cleaner run than the one it read.
func (r *Replayer) ChatCompletion(_ context.Context, call Call, _ llm.Request) (*llm.Response, error) {
	name := trace.JudgeCallName(call.Pair, call.Order)
	rec, ok := r.calls[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s recorded no %s", ErrReplay, r.dir, name)
	}
	if call.Attempt >= len(rec.Attempts) {
		return nil, fmt.Errorf("%w: %s recorded %d attempt(s) of %s, and this is attempt %d",
			ErrReplay, r.dir, len(rec.Attempts), name, call.Attempt+1)
	}
	at := rec.Attempts[call.Attempt]
	if at.Error != "" {
		last := call.Attempt == len(rec.Attempts)-1
		if last && rec.Status == trace.JudgeCallTimeout {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("%w: %s recorded a failure: %s", ErrReplay, name, at.Error)
	}
	return &llm.Response{
		Content:      at.Content,
		RawContent:   at.Content,
		RequestBody:  restore(at.Request, at.RequestSHA256),
		ResponseBody: restore(at.Response, at.ResponseSHA256),
		Usage: llm.Usage{
			PromptTokens:     at.Usage.PromptTokens,
			CompletionTokens: at.Usage.CompletionTokens,
		},
		Elapsed: time.Duration(at.LatencyMS) * time.Millisecond,
	}, nil
}

// restore recovers the bytes a call actually sent or received from the copy
// the trace kept of them, and the record's own digest says when it has.
//
// A trace stores each blob decoded, so three habits of the writer stand
// between what is on disk and what went over the wire: the indentation, the
// `<`, `>` and `&` Go's encoder escapes inside any blob it re-encodes, and
// a trailing newline a server sent that JSON does not carry. Each is undone
// or not, and the digest beside the blob says which combination was the
// original — a search with an answer key rather than a guess. That is what
// makes a replayed call's request_sha256 and response_sha256 equal the ones
// the source recorded, which is the cheapest check anybody can run that a
// replay answered the question it claims to have answered. A blob no
// combination reproduces is handed back compacted, with a digest that will
// not match and will therefore be noticed.
func restore(raw json.RawMessage, want string) []byte {
	small := compact(raw)
	if want == "" {
		return small
	}
	for _, form := range [][]byte{small, unescapeHTML(small)} {
		for _, candidate := range [][]byte{form, append(append([]byte{}, form...), '\n')} {
			if llm.SHA256(candidate) == want {
				return candidate
			}
		}
	}
	return small
}

// compact removes the indentation a trace file gains when it is written.
func compact(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}

// unescapeHTML undoes the three escapes Go's JSON encoder adds and nothing
// else. It is the inverse of one encoder's one habit, not a JSON unescaper.
func unescapeHTML(b []byte) []byte {
	out := b
	for _, e := range []struct{ from, to string }{
		{`\u003c`, "<"}, {`\u003e`, ">"}, {`\u0026`, "&"},
	} {
		out = bytes.ReplaceAll(out, []byte(e.from), []byte(e.to))
	}
	return out
}

// digest is the SHA-256 of a file, as the record spells it.
func digest(name string) (string, error) {
	body, err := os.ReadFile(name) //nolint:gosec // a trace file the caller named
	if err != nil {
		return "", err
	}
	return llm.SHA256(body), nil
}
