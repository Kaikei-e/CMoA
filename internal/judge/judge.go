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
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
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
	Log    func(format string, args ...any)
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

	rep := &trace.JudgeReport{
		SchemaVersion: trace.SchemaVersion,
		RunID:         in.RunID,
		Judge: trace.JudgeParams{
			Model: j.Cfg.Model, BaseURL: j.Cfg.BaseURL, Temperature: *j.Cfg.Temperature,
			Seed: j.Cfg.Seed, MaxTokens: j.Cfg.MaxTokens, OutputFormat: string(j.Cfg.OutputFormat),
			Parallel: j.Cfg.Parallel, AllowTie: in.AllowTie, PromptVersion: prompt.Version(),
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

	perm, seedSource := Permutation(in.RunID, in.Seed, len(in.Candidates))
	nonce, err := Nonce()
	if err != nil {
		return nil, err
	}
	rep.Presentation = trace.Presentation{Permutation: perm, Nonce: nonce, SeedSource: seedSource}

	if len(in.Candidates) < 2 {
		rep.Outcome = trace.JudgeOutcome{
			Kind:   trace.SelectionNoCandidate,
			Reason: string(trace.ReasonTooFewCandidates),
		}
		rep.Ranked = ranked(rep.Candidates, rep.Wins)
		return rep, j.finish(rep, started, now)
	}

	// Round-robin over the presented order: the pairs, and the order inside
	// each pair, are what the permutation decides, so the same run id asks
	// the same six questions.
	type call struct {
		pair          int
		order         string
		first, second int // indices into in.Candidates
	}
	var calls []call
	for i := 0; i < len(perm); i++ {
		for k := i + 1; k < len(perm); k++ {
			p := len(rep.Pairs)
			a, b := perm[i], perm[k]
			rep.Pairs = append(rep.Pairs, trace.JudgePair{
				Pair:    []string{in.Candidates[a].ID, in.Candidates[b].ID},
				Orders:  []trace.JudgeOrder{{}, {}},
				Verdict: trace.VerdictDraw,
			})
			calls = append(calls, call{pair: p, order: "ab", first: a, second: b})
			calls = append(calls, call{pair: p, order: "ba", first: b, second: a})
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
func (j *Judge) ask(ctx context.Context, in Input, pi prompt.JudgeInput, pair int, order, first, second string, now func() time.Time) (*trace.JudgeCall, trace.JudgeOrder) {
	rec := &trace.JudgeCall{
		SchemaVersion: trace.SchemaVersion, RunID: in.RunID, Pair: pair, Order: order,
		First: first, Second: second, Model: j.Cfg.Model, BaseURL: j.Cfg.BaseURL,
	}
	out := trace.JudgeOrder{First: first, Second: second, File: trace.JudgeCallName(pair, order)}
	started := now()
	defer func() {
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
		at, answer, status := j.one(ctx, msgs, key, body, now)
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

func (j *Judge) one(ctx context.Context, msgs []llm.Message, key string, body map[string]json.RawMessage, now func() time.Time) (trace.JudgeAttempt, *trace.JudgeAnswer, trace.JudgeCallStatus) {
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
	answer, perr := ParseAnswer(resp.Content, j.allowTie(msgs))
	if perr != nil {
		at.ParseError = perr.Error()
		return at, nil, trace.JudgeCallInvalidOutput
	}
	at.Parsed = answer
	return at, answer, trace.JudgeCallOK
}

// allowTie reads the contract back off the rendered prompt rather than
// carrying it twice: whatever the user message offered is what the parser
// accepts.
func (j *Judge) allowTie(msgs []llm.Message) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, `"choice": "A" | "B" | "tie"`) {
			return true
		}
	}
	return false
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
	format := map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "verdict",
			"strict": true,
			"schema": map[string]any{
				"type": "object",
				// The key order is the schema's order: the reason is
				// written before the choice, so a model cannot reach the
				// choice without passing through its own reasons for it.
				"properties": map[string]any{
					"reason": map[string]any{"type": "string", "maxLength": MaxReasonChars},
					"choice": map[string]any{"type": "string", "enum": choices},
				},
				"required":             []string{"reason", "choice"},
				"additionalProperties": false,
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

// Permutation returns the order the candidates are presented in and where
// its seed came from. Without the permutation in the trace, a re-run cannot
// be compared with the run it repeats.
func Permutation(id trace.RunID, seed *int64, n int) ([]int, string) {
	source := "run_id"
	sum := sha256.Sum256([]byte(id))
	hi, lo := binary.BigEndian.Uint64(sum[0:8]), binary.BigEndian.Uint64(sum[8:16])
	if seed != nil {
		source = "flag"
		hi, lo = uint64(*seed), ^uint64(*seed)
	}
	return mathrand.New(mathrand.NewPCG(hi, lo)).Perm(n), source
}

// Nonce is the per-selection fence label: 8 hex digits from crypto/rand, so
// a candidate cannot guess the closing sequence and write one of its own.
func Nonce() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("judge: crypto/rand: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Rewrite is one change the sanitiser made.
type Rewrite struct {
	What  string
	Count int
}

// The two rewrites the sanitiser makes, named the way the trace names them.
const (
	RewriteClosingTag = "closing-tag-like sequence escaped"
	RewriteControl    = "control characters dropped"
)

var closingTag = regexp.MustCompile(`(?i)</candidate`)

// Sanitize prepares one answer for a candidate block. It escapes anything
// that looks like the block's own closing tag and drops the C0 control
// characters other than tab and newline, and it reports what it did:
// rewriting an answer changes what is judged, so a silent rewrite would
// make an outcome unexplainable.
func Sanitize(s string) (string, []Rewrite) {
	var out []Rewrite
	if n := len(closingTag.FindAllString(s, -1)); n > 0 {
		s = closingTag.ReplaceAllStringFunc(s, func(m string) string { return `<\/` + m[2:] })
		out = append(out, Rewrite{What: RewriteClosingTag, Count: n})
	}
	dropped := 0
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			dropped++
			return -1
		}
		return r
	}, s)
	if dropped > 0 {
		out = append(out, Rewrite{What: RewriteControl, Count: dropped})
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
	regexp.MustCompile(`(?i)choose\s+(?:A|B)\b`),
}

// InjectionFlags lists, in lower case and without repeats, the
// injection-shaped phrases the answer holds.
func InjectionFlags(s string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, re := range injectionPatterns {
		for _, m := range re.FindAllString(s, -1) {
			m = strings.ToLower(strings.Join(strings.Fields(m), " "))
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
	var a trace.JudgeAnswer
	dec := json.NewDecoder(strings.NewReader(obj))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
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

// Aggregate fills the pair verdicts, the wins, the ranking and the outcome
// of a report whose orders have been answered.
func Aggregate(rep *trace.JudgeReport) {
	decided, invalid := 0, false
	for i := range rep.Pairs {
		p := &rep.Pairs[i]
		a, b := p.Orders[0], p.Orders[1]
		if a.Status == trace.JudgeCallInvalidOutput || b.Status == trace.JudgeCallInvalidOutput {
			invalid = true
		}
		// MT-Bench's conservative rule: a win needs both orders, and any
		// tie or disagreement is a draw. A pair the swap did not survive is
		// not evidence, and treating it as one is how a coin flip becomes a
		// decision.
		if a.Status == trace.JudgeCallOK && b.Status == trace.JudgeCallOK && a.Choice == b.Choice {
			rep.SwapConsistentPairs++
		}
		if a.Status != trace.JudgeCallOK || b.Status != trace.JudgeCallOK {
			continue
		}
		if a.ChoiceCandidate != "" && a.ChoiceCandidate == b.ChoiceCandidate {
			p.Verdict = a.ChoiceCandidate
			rep.Wins[p.Verdict]++
			decided++
		}
	}
	rep.Ranked = ranked(rep.Candidates, rep.Wins)
	rep.Outcome = outcome(rep, decided, invalid)
}

func outcome(rep *trace.JudgeReport, decided int, invalid bool) trace.JudgeOutcome {
	for _, p := range rep.Pairs {
		for _, o := range p.Orders {
			switch o.Status {
			case trace.JudgeCallTimeout:
				return trace.JudgeOutcome{Kind: trace.SelectionJudgeTimeout, Reason: "the judge did not answer in time"}
			case trace.JudgeCallError, trace.JudgeCallOK, trace.JudgeCallInvalidOutput:
			}
		}
	}
	for _, p := range rep.Pairs {
		for _, o := range p.Orders {
			if o.Status == trace.JudgeCallError {
				return trace.JudgeOutcome{Kind: trace.SelectionJudgeFailed, Reason: o.Error}
			}
		}
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
	reason := trace.ReasonNoMajority
	switch {
	case invalid:
		reason = trace.ReasonInvalidOutput
	case decided == 0 && len(rep.Pairs) > 1:
		reason = trace.ReasonAllDraws
	case decided == len(rep.Pairs):
		// Every pair was decided and still nobody beat everybody: the wins
		// run in a circle, which is a property of the judge, not of a
		// missing answer.
		reason = trace.ReasonCycle
	}
	return trace.JudgeOutcome{Kind: trace.SelectionNoCandidate, Reason: string(reason)}
}

// ranked orders the candidates by wins, breaking ties by the caller's
// order. It is informational: only the outcome selects.
func ranked(ids []string, wins map[string]int) []string {
	out := append([]string{}, ids...)
	sort.SliceStable(out, func(i, k int) bool { return wins[out[i]] > wins[out[k]] })
	return out
}
