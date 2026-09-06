// Package prompt builds the messages a proposer receives, and the two the
// judge receives.
//
// On the coding face a proposer is sent a system message stating the output
// contract (one unified diff, nothing else) and a user message holding the
// task instruction and the full text of every file the task lists. The
// choice of files is the task's, never the model's, so two runs of the same
// task send byte-identical prompts.
//
// On the chat face a proposer is sent one system message — a neutral
// assistant contract, with the harness appended to it — followed by the
// task's conversation verbatim. The judge is sent a system message stating
// its own contract and a user message holding the task, the reference
// answer and rubric if the task has them, and the two candidate answers
// inside nonced blocks. The reference and the rubric never reach a
// proposer: they would tell it the answer.
//
// A run may also carry a Harness: the rendered form of the harness edits
// that are in force. The contract comes first and verbatim — a harness
// edit appends to it, and can never delete it — while the harness's notes
// and skills reach the user message. An empty Harness renders the same two
// messages a run without one renders, byte for byte.
package prompt

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/task"
)

//go:embed system.tmpl user.tmpl chat-system.tmpl judge-system.tmpl judge-user.tmpl
var files embed.FS

// templateNames is every template the package renders, in the order
// Version digests them. Adding one changes prompt_version, which is the
// point: a run must be able to say which prompts it sent.
var templateNames = []string{"system.tmpl", "user.tmpl", "chat-system.tmpl", "judge-system.tmpl", "judge-user.tmpl"}

var (
	systemTmpl      = template.Must(template.New("system").Parse(mustRead("system.tmpl")))
	userTmpl        = template.Must(template.New("user").Parse(mustRead("user.tmpl")))
	chatSystemTmpl  = template.Must(template.New("chat-system").Parse(mustRead("chat-system.tmpl")))
	judgeSystemTmpl = template.Must(template.New("judge-system").Parse(mustRead("judge-system.tmpl")))
	judgeUserTmpl   = template.Must(template.New("judge-user").Parse(mustRead("judge-user.tmpl")))
)

// GenericRubric is what the judge is given when the task names no rubric of
// its own. It is deliberately about the answer and not about its shape: the
// three debiasing clauses live in the judge's system contract, and this
// last line repeats the one a rubric is most often written against.
const GenericRubric = `- Does the answer do what the last user message asks?
- Is it correct, and would its claims survive checking?
- Does it cover what matters and leave out what does not?
- Is it clear, and in the language the user wrote in?
- Length, formatting and tone are not quality.`

// Harness is a rendered harness directory as the prompt sees it: the text
// appended to the system contract, the notes in the order they are read,
// and the skills a proposer is told exist. A skill contributes its name and
// its description only; CMoA v0 has no step at which a body could be
// loaded, so rendering one would model a harness that does not exist.
type Harness struct {
	SystemAppendix string  // harness/system-prompt.md, trailing newlines trimmed
	Notes          []Note  // harness/memory/**/*.md, in path order
	Skills         []Skill // harness/skills/<name>/SKILL.md, in path order
}

// Note is one memory file. Path is kept for the record and for ordering;
// only Body is rendered.
type Note struct {
	Path string
	Body string
}

// Skill is one skill, named by its directory.
type Skill struct {
	Name        string
	Description string
}

// Empty reports whether the harness contributes nothing to either message.
func (h Harness) Empty() bool {
	return h.SystemAppendix == "" && len(h.Notes) == 0 && len(h.Skills) == 0
}

// HasContext reports whether the harness contributes to the user message.
func (h Harness) HasContext() bool { return len(h.Notes) > 0 || len(h.Skills) > 0 }

// Bytes is how many bytes of harness content reach the two messages: the
// appendix, every note body, and one name-plus-description per skill. It is
// what propose adds to the task's own context budget — a Notes section is
// as much of the model's context as a file is.
func (h Harness) Bytes() int {
	h = h.normalized()
	n := len(h.SystemAppendix)
	for _, note := range h.Notes {
		n += len(note.Body)
	}
	for _, s := range h.Skills {
		n += len(s.Name) + len(s.Description)
	}
	return n
}

// normalized trims the trailing newlines the templates supply themselves,
// so the rendered shape does not depend on how a caller ended a file.
func (h Harness) normalized() Harness {
	out := Harness{SystemAppendix: strings.TrimRight(h.SystemAppendix, "\n")}
	for _, n := range h.Notes {
		out.Notes = append(out.Notes, Note{Path: n.Path, Body: strings.TrimRight(n.Body, "\n")})
	}
	out.Skills = append(out.Skills, h.Skills...)
	return out
}

func mustRead(name string) string {
	b, err := files.ReadFile(name)
	if err != nil {
		panic("prompt: " + err.Error())
	}
	return string(b)
}

// Build renders the messages for t under h. The zero Harness is a run with
// no harness directory.
func Build(t *task.Task, h Harness) ([]llm.Message, error) {
	type file struct{ Path, Content string }
	data := struct {
		Instruction string
		Files       []file
		Harness     Harness
	}{Instruction: ensureNewline(t.Instruction), Harness: h.normalized()}
	for _, f := range t.Files {
		data.Files = append(data.Files, file{Path: f.Path, Content: ensureNewline(f.Content)})
	}
	var sys, user bytes.Buffer
	if err := systemTmpl.Execute(&sys, data); err != nil {
		return nil, err
	}
	if err := userTmpl.Execute(&user, data); err != nil {
		return nil, err
	}
	return []llm.Message{
		{Role: "system", Content: strings.TrimRight(sys.String(), "\n")},
		{Role: "user", Content: user.String()},
	}, nil
}

// Version is a digest of the templates, written into traces so a later
// reader can tell which prompt a run used. It covers every template the
// package holds, so editing any one of them changes the version a run
// records; it says nothing about the harness a run rendered, which
// harness.render in run.json does.
func Version() string {
	h := sha256.New()
	for i, name := range templateNames {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(mustRead(name)))
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// BuildChat renders the messages a chat-face proposer receives: one system
// message carrying the assistant contract and the harness, then the task's
// conversation, verbatim and in order. The conversation is the task's, so
// two runs of the same task send byte-identical prompts.
func BuildChat(t *task.Task, h Harness) ([]llm.Message, error) {
	if t.Chat == nil {
		return nil, errors.New("prompt: BuildChat needs a chat task")
	}
	var sys bytes.Buffer
	if err := chatSystemTmpl.Execute(&sys, struct{ Harness Harness }{h.normalized()}); err != nil {
		return nil, err
	}
	msgs := []llm.Message{{Role: "system", Content: strings.TrimRight(sys.String(), "\n")}}
	for _, m := range t.Chat.Conversation {
		msgs = append(msgs, llm.Message{Role: m.Role, Content: m.Content})
	}
	return msgs, nil
}

// JudgeCandidate is one answer as the judge sees it: a positional label and
// the text, already sanitised by the caller. Which candidate carries which
// label is not in the prompt — only in the trace.
type JudgeCandidate struct {
	Label string
	Text  string
}

// JudgeInput is everything the judge's user message is built from.
type JudgeInput struct {
	Conversation []task.ConvMessage
	Reference    string // the task's reference answer, empty when it has none
	Rubric       string // the task's rubric, empty for GenericRubric
	Candidates   []JudgeCandidate
	Nonce        string // 8 hex, per selection
	AllowTie     bool
}

// BuildJudge renders the two messages of one judge call. The nonce fences
// the candidate blocks; escaping any closing sequence inside the text is
// the caller's job, because a rewrite changes what is judged and has to be
// recorded where the outcome is.
func BuildJudge(in JudgeInput) ([]llm.Message, error) {
	if in.Nonce == "" {
		return nil, errors.New("prompt: BuildJudge needs a nonce")
	}
	if len(in.Candidates) != 2 {
		return nil, fmt.Errorf("prompt: BuildJudge compares two candidates, got %d", len(in.Candidates))
	}
	type conv struct{ Role, Content string }
	data := struct {
		Conversation []conv
		Reference    string
		Rubric       string
		Candidates   []JudgeCandidate
		Nonce        string
		ChoiceEnum   string
	}{Reference: trimBlank(in.Reference), Rubric: trimBlank(in.Rubric), Nonce: in.Nonce, ChoiceEnum: `"A" | "B"`}
	if data.Rubric == "" {
		data.Rubric = GenericRubric
	}
	if in.AllowTie {
		data.ChoiceEnum = `"A" | "B" | "tie"`
	}
	for _, m := range in.Conversation {
		data.Conversation = append(data.Conversation, conv{Role: m.Role, Content: trimBlank(m.Content)})
	}
	for _, c := range in.Candidates {
		data.Candidates = append(data.Candidates, JudgeCandidate{Label: c.Label, Text: trimBlank(c.Text)})
	}
	var sys, user bytes.Buffer
	if err := judgeSystemTmpl.Execute(&sys, data); err != nil {
		return nil, err
	}
	if err := judgeUserTmpl.Execute(&user, data); err != nil {
		return nil, err
	}
	return []llm.Message{
		{Role: "system", Content: strings.TrimRight(sys.String(), "\n")},
		{Role: "user", Content: user.String()},
	}, nil
}

func trimBlank(s string) string { return strings.Trim(s, "\n") }

func ensureNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
