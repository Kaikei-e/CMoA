// Package judge is the chat face's aggregation: a single model is asked to
// compare candidates two at a time, in both orders, and the answer is
// selected only when it wins every pair it appears in. There is no panel,
// no synthesis and no vote among proposers.
//
// The protocol is round-robin pairwise with an order swap: three candidates
// make three pairs and six calls. A pair is won only when both orders name
// the same candidate; disagreement, or a tie in either order, is a draw and
// scores nothing for either side. A unique candidate that wins every pair
// is the Condorcet winner and is selected; anything else is no candidate,
// with a sub-reason. There is no re-ask beyond one retry for malformed
// JSON, and no deterministic fallback: choosing "the first" or "the
// shorter" answer would reinstate as a rule exactly the position and length
// biases the swap is there to detect.
//
// Everything the judge saw is reconstructible from the trace: the
// permutation and the nonce, the exact request and response of every call,
// what the sanitiser rewrote, and which candidates carried injection-shaped
// text.
package judge

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/prompt"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// Candidate is one answer to compare, by the id the run knows it as.
type Candidate struct {
	ID     string
	Answer string
}

// Input is one selection: the task as the judge sees it, and the answers.
type Input struct {
	RunID        trace.RunID
	Conversation []task.ConvMessage
	Reference    string // the task's reference answer; the proposers never saw it
	Rubric       string // the task's rubric; empty means the generic one
	AllowTie     bool
	Candidates   []Candidate // in the order the caller considers canonical
	// Seed overrides the presentation permutation and the nonce, so a
	// re-run can be asked for a different presentation of the same
	// candidates. It never touches the judge's own sampling seed.
	Seed *int64
}

// Judge asks one endpoint and writes what it answered.
type Judge struct {
	Cfg    *config.Judge
	Client *llm.Client
	Dir    trace.Dir
	Now    func() time.Time
	// Log is called from the goroutine that made each call, so it must be
	// safe for concurrent use.
	Log func(format string, args ...any)
}

// Run performs the protocol and writes judge/<pair>-<order>.json and
// judge.json. It returns the report; an error means the trace could not be
// written, never that the judge disagreed with itself.
func (j *Judge) Run(ctx context.Context, in Input) (*trace.JudgeReport, error) {
	if j.Cfg == nil {
		return nil, errors.New("judge: no judge is configured")
	}
	now := j.Now
	if now == nil {
		now = time.Now
	}
	logf := j.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	started := now()

	// Refuse before spending, not after. judge.json is write-once and is
	// written last, so a run that already has one would otherwise pay for
	// all six calls and then fail at the final write.
	if _, err := os.Stat(j.Dir.JudgeFile()); err == nil {
		return nil, fmt.Errorf("%w: %s; a run is judged once", trace.ErrExists, j.Dir.JudgeFile())
	}
	// A previous attempt that died between the calls and judge.json leaves
	// call files behind. They belong to a selection that never happened, and
	// a different seed makes different pairs, so they are cleared rather
	// than left to be read as part of this one.
	if n, err := clearCalls(j.Dir); err != nil {
		return nil, err
	} else if n > 0 {
		logf("judge: cleared %d call file(s) from an attempt that left no judge.json", n)
	}

	rep := &trace.JudgeReport{
		SchemaVersion: trace.SchemaVersion,
		RunID:         in.RunID,
		Judge: trace.JudgeParams{
			Model: j.Cfg.Model, BaseURL: j.Cfg.BaseURL, Temperature: *j.Cfg.Temperature,
			Seed: j.Cfg.Seed, MaxTokens: j.Cfg.MaxTokens, OutputFormat: string(j.Cfg.OutputFormat),
			Parallel: j.Cfg.Parallel, AllowTie: in.AllowTie, PromptVersion: prompt.Version(),
			ExtraBody: j.Cfg.ExtraBody,
		},
		Candidates:     []string{},
		Wins:           map[string]int{},
		Pairs:          []trace.JudgePair{},
		Ranked:         []string{},
		Sanitized:      []trace.Sanitized{},
		InjectionFlags: map[string][]string{},
	}

	// Sanitise and flag before anything is presented. A rewrite changes
	// what is judged, so it is recorded next to the outcome it could
	// explain; a flag is recorded and never acted on, because acting on it
	// would be a second, unmeasured judge.
	texts := make([]string, len(in.Candidates))
	for i, c := range in.Candidates {
		rep.Candidates = append(rep.Candidates, c.ID)
		rep.Wins[c.ID] = 0
		clean, rewrites := Sanitize(c.Answer)
		texts[i] = clean
		for _, r := range rewrites {
			rep.Sanitized = append(rep.Sanitized, trace.Sanitized{Candidate: c.ID, What: r.What, Count: r.Count})
		}
		rep.InjectionFlags[c.ID] = InjectionFlags(c.Answer)
	}

	seed, seedSource := PresentationSeed(in.RunID, in.Seed)
	nonce := Nonce(seed)
	rep.Presentation = trace.Presentation{Seed: seed, SeedSource: seedSource, Nonce: nonce}

	if len(in.Candidates) < 2 {
		Aggregate(rep)
		return rep, j.finish(rep, started, now)
	}

	// Round-robin in the caller's order. There is no shuffle: both orders
	// of every pair are asked, so permuting the candidates would only
	// renumber the pairs and swap which order is filed as -ab, without
	// changing one byte of any request. The seeded nonce is what a re-run
	// actually varies.
	type call struct {
		pair          int
		order         string
		first, second int // indices into in.Candidates
	}
	var calls []call
	for i := range in.Candidates {
		for k := i + 1; k < len(in.Candidates); k++ {
			p := len(rep.Pairs)
			rep.Pairs = append(rep.Pairs, trace.JudgePair{
				Pair:    []string{in.Candidates[i].ID, in.Candidates[k].ID},
				Orders:  []trace.JudgeOrder{{}, {}},
				Verdict: trace.VerdictDraw,
			})
			calls = append(calls, call{pair: p, order: "ab", first: i, second: k})
			calls = append(calls, call{pair: p, order: "ba", first: k, second: i})
		}
	}

	base := prompt.JudgeInput{
		Conversation: in.Conversation, Reference: in.Reference, Rubric: in.Rubric,
		Nonce: nonce, AllowTie: in.AllowTie,
	}
	var mu sync.Mutex
	var writeErr error
	sem := make(chan struct{}, j.Cfg.Parallel)
	var wg sync.WaitGroup
	for _, c := range calls {
		wg.Add(1)
		go func(c call) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ci := base
			ci.Candidates = []prompt.JudgeCandidate{
				{Label: trace.ChoiceA, Text: texts[c.first]},
				{Label: trace.ChoiceB, Text: texts[c.second]},
			}
			rec, order := j.ask(ctx, in, ci, c.pair, c.order, in.Candidates[c.first].ID, in.Candidates[c.second].ID, now)
			logf("judge pair %d %s: %s %s (%s)", c.pair, c.order, order.Status, order.ChoiceCandidate,
				time.Duration(order.LatencyMS)*time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			idx := 0
			if c.order == "ba" {
				idx = 1
			}
			rep.Pairs[c.pair].Orders[idx] = order
			for _, a := range rec.Attempts {
				rep.Usage.PromptTokens += a.Usage.PromptTokens
				rep.Usage.CompletionTokens += a.Usage.CompletionTokens
			}
			rep.InvalidOutputRetries += order.Retries
			if err := j.Dir.WriteJudgeCall(rec); err != nil && writeErr == nil {
				writeErr = err
			}
		}(c)
	}
	wg.Wait()
	if writeErr != nil {
		return nil, writeErr
	}

	Aggregate(rep)
	return rep, j.finish(rep, started, now)
}

func (j *Judge) finish(rep *trace.JudgeReport, started time.Time, now func() time.Time) error {
	rep.FinishedAt = now().UTC()
	rep.LatencyMS = rep.FinishedAt.Sub(started).Milliseconds()
	return j.Dir.WriteJudge(rep)
}

// ask performs one call, with the single retry a malformed answer earns.
//
// The results are named: the latency is filled in by a deferred function,
// and an unnamed result would be copied out of the function before that
// function ran, leaving every order in judge.json at zero.
func (j *Judge) ask(ctx context.Context, in Input, pi prompt.JudgeInput, pair int, order, first, second string, now func() time.Time) (rec *trace.JudgeCall, out trace.JudgeOrder) {
	rec = &trace.JudgeCall{
		SchemaVersion: trace.SchemaVersion, RunID: in.RunID, Pair: pair, Order: order,
		First: first, Second: second, Model: j.Cfg.Model, BaseURL: j.Cfg.BaseURL,
	}
	out = trace.JudgeOrder{First: first, Second: second, File: trace.JudgeCallName(pair, order)}
	started := now()
	defer func() {
		// The call's own wall clock, which covers both attempts when the
		// first did not parse. Each attempt's share is in the call file.
		rec.LatencyMS = now().Sub(started).Milliseconds()
		out.LatencyMS = rec.LatencyMS
	}()

	messages, err := prompt.BuildJudge(pi)
	if err != nil {
		rec.Status, out.Status, out.Error = trace.JudgeCallError, trace.JudgeCallError, err.Error()
		return rec, out
	}
	key, err := j.Cfg.APIKey()
	if err != nil {
		rec.Status, out.Status, out.Error = trace.JudgeCallError, trace.JudgeCallError, err.Error()
		return rec, out
	}
	body, err := j.extraBody(in.AllowTie)
	if err != nil {
		rec.Status, out.Status, out.Error = trace.JudgeCallError, trace.JudgeCallError, err.Error()
		return rec, out
	}

	// The retry appends one instruction and nothing else: a second prompt
	// that argued with the model would be a different question, and the
	// retry rate is only a usable measure while every retry is the same
	// retry.
	for attempt := 0; attempt < 2; attempt++ {
		msgs := messages
		if attempt == 1 {
			msgs = append(append([]llm.Message{}, messages...), llm.Message{Role: task.RoleUser, Content: RetryInstruction})
			out.Retries++
		}
		at, answer, status := j.one(ctx, msgs, key, body, in.AllowTie, now)
		rec.Attempts = append(rec.Attempts, at)
		out.RequestSHA256, out.ResponseSHA256 = at.RequestSHA256, at.ResponseSHA256
		if status == trace.JudgeCallOK {
			rec.Status, rec.Choice = status, answer.Choice
			out.Status, out.Choice = status, answer.Choice
			out.ChoiceCandidate = candidateOf(answer.Choice, first, second)
			return rec, out
		}
		out.Error = at.Error
		if out.Error == "" {
			out.Error = at.ParseError
		}
		if status != trace.JudgeCallInvalidOutput {
			// A transport failure or a timeout is not the model's answer
			// being wrong; asking again would measure the network.
			rec.Status, out.Status = status, status
			return rec, out
		}
	}
	rec.Status, out.Status = trace.JudgeCallInvalidOutput, trace.JudgeCallInvalidOutput
	return rec, out
}

// RetryInstruction is the one thing appended when an answer did not parse.
const RetryInstruction = "Return only the JSON object."

func (j *Judge) one(ctx context.Context, msgs []llm.Message, key string, body map[string]json.RawMessage, allowTie bool, now func() time.Time) (trace.JudgeAttempt, *trace.JudgeAnswer, trace.JudgeCallStatus) {
	at := trace.JudgeAttempt{Messages: toTraceMessages(msgs)}
	req := llm.Request{
		BaseURL: j.Cfg.BaseURL, APIKey: key, Model: j.Cfg.Model, Messages: msgs,
		Temperature: *j.Cfg.Temperature, MaxTokens: j.Cfg.MaxTokens, Seed: j.Cfg.Seed, ExtraBody: body,
	}
	started := now()
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(j.Cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	resp, err := j.Client.ChatCompletion(callCtx, req)
	at.LatencyMS = now().Sub(started).Milliseconds()
	if err != nil {
		at.Error = err.Error()
		var he *llm.HTTPError
		var de *llm.DecodeError
		switch {
		case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
			return at, nil, trace.JudgeCallTimeout
		case errors.As(err, &he):
			at.Response = rawJSON(he.Body)
		case errors.As(err, &de):
			at.Response = rawJSON(de.Body)
		}
		return at, nil, trace.JudgeCallError
	}
	at.Request = rawJSON(resp.RequestBody)
	at.Response = rawJSON(resp.ResponseBody)
	at.RequestSHA256 = llm.SHA256(resp.RequestBody)
	at.ResponseSHA256 = llm.SHA256(resp.ResponseBody)
	at.Content = resp.Content
	at.Usage = trace.Usage{PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens}
	answer, perr := ParseAnswer(resp.Content, allowTie)
	if perr != nil {
		at.ParseError = perr.Error()
		return at, nil, trace.JudgeCallInvalidOutput
	}
	at.Parsed = answer
	return at, answer, trace.JudgeCallOK
}

// extraBody merges the configured extra_body with the response format CMoA
// owns. A raw GBNF grammar is deliberately never sent: a server with its
// own structured chat format parses one beside that format rather than
// composing the two, and answers an error.
func (j *Judge) extraBody(allowTie bool) (map[string]json.RawMessage, error) {
	body := map[string]json.RawMessage{}
	for k, v := range j.Cfg.ExtraBody {
		body[k] = v
	}
	switch j.Cfg.OutputFormat {
	case config.OutputNone:
		return body, nil
	case config.OutputJSONSchema:
	}
	choices := []string{trace.ChoiceA, trace.ChoiceB}
	if allowTie {
		choices = append(choices, trace.ChoiceTie)
	}
	// Structs, not maps: encoding/json writes map keys in sorted order,
	// which would put "choice" before "reason" in the schema and so in the
	// answer. The reason must be written first, or the choice is reached
	// without passing through it.
	format := responseFormat{
		Type: "json_schema",
		JSONSchema: jsonSchema{
			Name:   "verdict",
			Strict: true,
			Schema: verdictSchema{
				Type: "object",
				Properties: verdictProperties{
					Reason: schemaField{Type: "string", MaxLength: MaxReasonChars},
					Choice: schemaField{Type: "string", Enum: choices},
				},
				Required:             []string{"reason", "choice"},
				AdditionalProperties: false,
			},
		},
	}
	b, err := json.Marshal(format)
	if err != nil {
		return nil, err
	}
	body["response_format"] = b
	return body, nil
}

// The judge's answer schema, as structs so the field order is the wire
// order. Nothing here is read back: it is written into the request and
// recorded in the call file.
type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema jsonSchema `json:"json_schema"`
}

type jsonSchema struct {
	Name   string        `json:"name"`
	Strict bool          `json:"strict"`
	Schema verdictSchema `json:"schema"`
}

type verdictSchema struct {
	Type                 string            `json:"type"`
	Properties           verdictProperties `json:"properties"`
	Required             []string          `json:"required"`
	AdditionalProperties bool              `json:"additionalProperties"`
}

// verdictProperties fixes the order: reason, then choice.
type verdictProperties struct {
	Reason schemaField `json:"reason"`
	Choice schemaField `json:"choice"`
}

type schemaField struct {
	Type      string   `json:"type"`
	MaxLength int      `json:"maxLength,omitempty"`
	Enum      []string `json:"enum,omitempty"`
}

// MaxReasonChars bounds the rationale. A short one is deliberate: a long
// free chain of thought before a preference reads as a post-hoc
// justification of a label the model had already anchored on.
const MaxReasonChars = 400

func candidateOf(choice, first, second string) string {
	switch choice {
	case trace.ChoiceA:
		return first
	case trace.ChoiceB:
		return second
	}
	return ""
}

func rawJSON(b []byte) json.RawMessage {
	if !json.Valid(b) {
		q, err := json.Marshal(string(b))
		if err != nil {
			return nil
		}
		return q
	}
	return json.RawMessage(b)
}

func toTraceMessages(ms []llm.Message) []trace.Message {
	out := make([]trace.Message, len(ms))
	for i, m := range ms {
		out[i] = trace.Message{Role: m.Role, Content: m.Content}
	}
	return out
}

// clearCalls removes the judge/ call files of an attempt that left no
// judge.json, and returns how many there were.
func clearCalls(dir trace.Dir) (int, error) {
	entries, err := os.ReadDir(dir.JudgeDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(dir.JudgeDir(), e.Name())); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// PresentationSeed returns the seed the nonce is derived from and where it
// came from: the caller's --seed, or the run id. Either way it is recorded,
// so a selection can be reproduced byte for byte from its trace.
func PresentationSeed(id trace.RunID, seed *int64) (int64, string) {
	if seed != nil {
		return *seed, "flag"
	}
	sum := sha256.Sum256([]byte(id))
	return int64(binary.BigEndian.Uint64(sum[0:8])), "run_id"
}

// Nonce is the per-selection fence label: 8 hex digits derived from the
// presentation seed.
//
// It is deliberately not from crypto/rand. The nonce has two jobs, and only
// one of them wants unpredictability. It fences the candidate blocks, which
// a candidate cannot defeat by guessing as long as the value is fresh per
// selection; and it is the one token a re-run can vary, which makes
// `--seed` a metamorphic perturbation — the same question in different
// irrelevant bytes, whose answer ought not to change. A crypto/rand nonce
// would make that perturbation unrepeatable, and a selection would not be
// reproducible from its own trace. The seed itself comes from the run id
// when the caller names none, and a run id is 8 hex from crypto/rand.
func Nonce(seed int64) string {
	//nolint:gosec // not a secret: a fence label, recorded in the trace.
	r := mathrand.New(mathrand.NewPCG(uint64(seed), ^uint64(seed)))
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(r.Uint64()>>32))
	return hex.EncodeToString(b[:])
}

// Rewrite is one change the sanitiser made.
type Rewrite struct {
	What  string
	Count int
}

// The three rewrites the sanitiser makes, named the way the trace names
// them, in the order they are applied.
const (
	RewriteControl    = "control characters dropped"
	RewriteZeroWidth  = "zero-width characters dropped"
	RewriteClosingTag = "closing-tag-like sequence escaped"
)

// closingTag is deliberately tolerant. A candidate that wants to end its
// own block early will not write the sequence the way the template does,
// and a literal match would let `< /candidate` and `</ candidate` through.
var closingTag = regexp.MustCompile(`(?i)<\s*/\s*candidate`)

// zeroWidth are the invisible runes a candidate can hide inside a closing
// tag: they survive a literal comparison, render as nothing, and — before
// the order of these two passes was fixed — were dropped *after* the escape
// had failed to match, reconstituting the tag the escape was there to
// break.
func zeroWidth(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
		return true
	}
	return false
}

// Sanitize prepares one answer for a candidate block, in the order that
// makes the passes composable: the invisible characters go first, so the
// tag escape sees the text a reader would see, and only then is anything
// that still looks like a closing tag broken.
//
// It reports what it did. Rewriting an answer changes what is judged, so a
// silent rewrite would make an outcome unexplainable — and a trace that
// claims an escape it never applied is worse than one that claims nothing.
func Sanitize(s string) (string, []Rewrite) {
	var out []Rewrite
	controls, invisible := 0, 0
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f:
			controls++
			return -1
		case zeroWidth(r):
			invisible++
			return -1
		}
		return r
	}, s)
	if controls > 0 {
		out = append(out, Rewrite{What: RewriteControl, Count: controls})
	}
	if invisible > 0 {
		out = append(out, Rewrite{What: RewriteZeroWidth, Count: invisible})
	}
	if n := len(closingTag.FindAllString(s, -1)); n > 0 {
		// Escape the opening angle bracket and leave the rest as written:
		// the block stops looking like a closing tag without the record
		// losing what the candidate actually said.
		s = closingTag.ReplaceAllStringFunc(s, func(m string) string { return `<\` + m[1:] })
		out = append(out, Rewrite{What: RewriteClosingTag, Count: n})
	}
	return s, out
}

// injectionPatterns are the phrases a candidate uses when it is addressing
// the judge rather than the task. They are recorded and never acted on: a
// defence that silently discards a flagged candidate is an unmeasured
// second judge, and the flag's value is that a calibration can ask whether
// flagged candidates win more often than they should.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(?:all\s+|any\s+|the\s+)?(?:previous|prior|above)\s+instructions`),
	regexp.MustCompile(`(?i)you\s+are\s+now`),
	regexp.MustCompile(`(?i)system\s+prompt`),
	regexp.MustCompile(`(?i)as\s+the\s+judge`),
	// The labels are matched case-sensitively: read case-insensitively this
	// flags "choose a library", and a flag that fires on ordinary English
	// destroys the only question it exists to answer.
	regexp.MustCompile(`(?i:choose\s+)(?:candidate\s+)?[AB]\b`),
}

// InjectionFlags lists, without repeats, the injection-shaped phrases the
// answer holds. The text is recorded as it was written, whitespace folded:
// the labels are case-sensitive, so lower-casing the match would print
// "choose a" for a phrase that only fires on "choose A".
func InjectionFlags(s string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, re := range injectionPatterns {
		for _, m := range re.FindAllString(s, -1) {
			m = strings.Join(strings.Fields(m), " ")
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

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

// Aggregate fills the pair verdicts and their draw reasons, the wins, the
// ranking and the outcome of a report whose orders have been answered.
func Aggregate(rep *trace.JudgeReport) {
	rep.DrawReasons = map[trace.DrawReason]int{}
	for i := range rep.Pairs {
		p := &rep.Pairs[i]
		a, b := p.Orders[0], p.Orders[1]
		// Consistency is about the candidate, not the label: choosing A in
		// one order and B in the other is the same answer twice, and
		// choosing A in both is the position speaking.
		if a.Status == trace.JudgeCallOK && b.Status == trace.JudgeCallOK && a.ChoiceCandidate == b.ChoiceCandidate {
			rep.SwapConsistentPairs++
		}
		// The conservative rule: a win needs both orders, and any tie or
		// disagreement is a draw. A pair the swap did not survive is not
		// evidence, and treating it as one is how a coin flip becomes a
		// decision.
		if a.Status == trace.JudgeCallOK && b.Status == trace.JudgeCallOK &&
			a.ChoiceCandidate != "" && a.ChoiceCandidate == b.ChoiceCandidate {
			p.Verdict = a.ChoiceCandidate
			rep.Wins[p.Verdict]++
			continue
		}
		p.DrawReason = drawReason(a, b)
		rep.DrawReasons[p.DrawReason]++
	}
	rep.Ranked = ranked(rep.Candidates, rep.Wins)
	rep.Outcome = outcome(rep)
}

// drawReason says why one pair produced no winner, most severe first: a
// pair nobody asked outranks a pair nobody could parse, which outranks an
// abstention, which outranks a contradiction.
func drawReason(a, b trace.JudgeOrder) trace.DrawReason {
	for _, o := range []trace.JudgeOrder{a, b} {
		switch o.Status {
		case trace.JudgeCallTimeout, trace.JudgeCallError:
			return trace.DrawUnmeasured
		case trace.JudgeCallOK, trace.JudgeCallInvalidOutput:
		}
	}
	if a.Status == trace.JudgeCallInvalidOutput || b.Status == trace.JudgeCallInvalidOutput {
		return trace.DrawInvalid
	}
	if a.Choice == trace.ChoiceTie || b.Choice == trace.ChoiceTie {
		return trace.DrawTie
	}
	return trace.DrawDisagree
}

// outcome reads the wins first and only then asks whether a failed call
// mattered.
//
// A pair that was never answered does not discard a winner it could not
// have unseated: if one candidate has already beaten every other, no answer
// to the pair between two losers can change that, and returning
// judge_timeout there would throw away a selection the judge did make. The
// failure is escalated only when the missing answers could still decide the
// outcome — that is, when some candidate could still reach a clean sweep if
// every unanswered pair went its way.
func outcome(rep *trace.JudgeReport) trace.JudgeOutcome {
	if len(rep.Candidates) < 2 {
		return trace.JudgeOutcome{Kind: trace.SelectionNoCandidate, Reason: string(trace.ReasonTooFewCandidates)}
	}
	// A Condorcet winner beats every other candidate; with n candidates
	// that is n-1 pairs, and there can be at most one.
	need := len(rep.Candidates) - 1
	var winners []string
	for _, id := range rep.Candidates {
		if rep.Wins[id] == need {
			winners = append(winners, id)
		}
	}
	if len(winners) == 1 {
		return trace.JudgeOutcome{
			Kind:        trace.SelectionSelected,
			CandidateID: winners[0],
			Reason:      fmt.Sprintf("condorcet winner, %d of %d pairs agreed under both orders", need, len(rep.Pairs)),
		}
	}
	if o := escalate(rep, need); o != nil {
		return *o
	}
	decided, unmeasured, invalid := 0, 0, 0
	for _, p := range rep.Pairs {
		switch p.DrawReason {
		case "":
			decided++
		case trace.DrawUnmeasured:
			unmeasured++
		case trace.DrawInvalid:
			invalid++
		case trace.DrawTie, trace.DrawDisagree:
		}
	}
	reason := trace.ReasonNoMajority
	switch {
	case invalid > 0:
		reason = trace.ReasonInvalidOutput
	case decided == 0 && len(rep.Pairs) > 1:
		// A union: every pair failed to decide, whether by tie, by
		// contradiction under swap, or because it was never measured.
		// pairs[].draw_reason is where the split lives.
		reason = trace.ReasonAllDraws
	case decided+unmeasured == len(rep.Pairs) && unmeasured == 0:
		// Every pair was decided and still nobody beat everybody: the wins
		// run in a circle, which is a property of the judge, not of a
		// missing answer.
		reason = trace.ReasonCycle
	}
	return trace.JudgeOutcome{Kind: trace.SelectionNoCandidate, Reason: string(reason)}
}

// escalate returns judge_timeout or judge_failed when the pairs that were
// never answered could still have produced a winner, and nil when they
// could not. A timeout outranks a transport error: it is the one a caller
// retries.
func escalate(rep *trace.JudgeReport, need int) *trace.JudgeOutcome {
	missing := map[string]int{}
	var timedOut, failed *trace.JudgeOrder
	for i := range rep.Pairs {
		p := &rep.Pairs[i]
		if p.DrawReason != trace.DrawUnmeasured {
			continue
		}
		for _, id := range p.Pair {
			missing[id]++
		}
		for k := range p.Orders {
			switch p.Orders[k].Status {
			case trace.JudgeCallTimeout:
				if timedOut == nil {
					timedOut = &p.Orders[k]
				}
			case trace.JudgeCallError:
				if failed == nil {
					failed = &p.Orders[k]
				}
			case trace.JudgeCallOK, trace.JudgeCallInvalidOutput:
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	couldDecide := false
	for _, id := range rep.Candidates {
		if rep.Wins[id]+missing[id] >= need {
			couldDecide = true
		}
	}
	if !couldDecide {
		return nil
	}
	if timedOut != nil {
		return &trace.JudgeOutcome{Kind: trace.SelectionJudgeTimeout, Reason: "the judge did not answer in time"}
	}
	return &trace.JudgeOutcome{Kind: trace.SelectionJudgeFailed, Reason: failed.Error}
}

// ranked orders the candidates by wins, breaking ties by the caller's
// order. It is informational: only the outcome selects.
func ranked(ids []string, wins map[string]int) []string {
	out := append([]string{}, ids...)
	sort.SliceStable(out, func(i, k int) bool { return wins[out[i]] > wins[out[k]] })
	return out
}
