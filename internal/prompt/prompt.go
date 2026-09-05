// Package prompt builds the two messages a proposer receives: a system
// message stating the output contract (one unified diff, nothing else) and
// a user message holding the task instruction and the full text of every
// file the task lists. The choice of files is the task's, never the
// model's, so two runs of the same task send byte-identical prompts.
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
	"strings"
	"text/template"

	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/task"
)

//go:embed system.tmpl user.tmpl
var files embed.FS

var (
	systemTmpl = template.Must(template.New("system").Parse(mustRead("system.tmpl")))
	userTmpl   = template.Must(template.New("user").Parse(mustRead("user.tmpl")))
)

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
// reader can tell which prompt a run used. It says nothing about the
// harness a run rendered; harness.render in run.json does that.
func Version() string {
	sum := sha256.Sum256([]byte(mustRead("system.tmpl") + "\x00" + mustRead("user.tmpl")))
	return hex.EncodeToString(sum[:8])
}

func ensureNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
