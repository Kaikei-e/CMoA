// Package prompt builds the two messages a proposer receives: a system
// message stating the output contract (one unified diff, nothing else) and
// a user message holding the task instruction and the full text of every
// file the task lists. The choice of files is the task's, never the
// model's, so two runs of the same task send byte-identical prompts.
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
	systemText string
	userTmpl   = template.Must(template.New("user").Parse(mustRead("user.tmpl")))
)

func init() { systemText = strings.TrimRight(mustRead("system.tmpl"), "\n") }

func mustRead(name string) string {
	b, err := files.ReadFile(name)
	if err != nil {
		panic("prompt: " + err.Error())
	}
	return string(b)
}

// Build renders the messages for t.
func Build(t *task.Task) ([]llm.Message, error) {
	type file struct{ Path, Content string }
	data := struct {
		Instruction string
		Files       []file
	}{Instruction: ensureNewline(t.Instruction)}
	for _, f := range t.Files {
		data.Files = append(data.Files, file{Path: f.Path, Content: ensureNewline(f.Content)})
	}
	var buf bytes.Buffer
	if err := userTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return []llm.Message{
		{Role: "system", Content: systemText},
		{Role: "user", Content: buf.String()},
	}, nil
}

// Version is a digest of the templates, written into traces so a later
// reader can tell which prompt a run used.
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
