package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// loadChat reads the conversation and documents visible only to the judge.
func loadChat(abs string, id TaskID, m *manifest, logf func(string, ...any)) (*Task, error) {
	if err := rejectCodingFields(m); err != nil {
		return nil, err
	}
	t := &Task{ID: id, Dir: abs, Face: FaceChat, MaxContextBytes: m.MaxContextBytes, Chat: &Chat{AllowTie: true}}
	if m.Judge != nil && m.Judge.AllowTie != nil {
		t.Chat.AllowTie = *m.Judge.AllowTie
	}
	if m.Conversation == "" {
		m.Conversation = ConversationFile
	}
	conv, err := cleanTaskPath(m.Conversation)
	if err != nil {
		return nil, &ValidationError{"conversation", err.Error()}
	}
	t.Chat.ConversationPath = conv
	b, err := os.ReadFile(filepath.Join(abs, filepath.FromSlash(conv)))
	if err != nil {
		return nil, &ValidationError{"conversation", err.Error()}
	}
	if t.Chat.Conversation, err = parseConversation(b); err != nil {
		return nil, &ValidationError{"conversation", err.Error()}
	}
	if m.Reference != nil && m.Reference.Answer != "" {
		p, body, err := t.readTaskFile(m.Reference.Answer)
		if err != nil {
			return nil, &ValidationError{"reference.answer", err.Error()}
		}
		t.Chat.ReferencePath, t.Chat.ReferenceAnswer = p, body
	}
	if m.Rubric != "" {
		p, body, err := t.readTaskFile(m.Rubric)
		if err != nil {
			return nil, &ValidationError{"rubric", err.Error()}
		}
		t.Chat.RubricPath, t.Chat.Rubric = p, body
	}
	if _, err := os.Stat(filepath.Join(abs, InstructionFile)); err == nil {
		logf("task %s: %s is not used on the chat face; the conversation is the task", id, InstructionFile)
	}
	if total := t.ContextBytes(); total > t.MaxContextBytes {
		return nil, &ValidationError{"conversation", fmt.Sprintf("the conversation totals %d bytes, over max_context_bytes %d", total, t.MaxContextBytes)}
	}
	return t, nil
}

func parseConversation(b []byte) ([]ConvMessage, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("is not valid UTF-8; proposers only see text")
	}
	var msgs []ConvMessage
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&msgs); err != nil {
		return nil, err
	}
	if err := ValidateConversation(msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

// ValidateConversation checks the shape a chat task and cmoa serve both require.
func ValidateConversation(msgs []ConvMessage) error {
	if len(msgs) == 0 {
		return errors.New("at least one message is required")
	}
	for i, msg := range msgs {
		switch msg.Role {
		case RoleSystem, RoleUser, RoleAssistant:
		default:
			return fmt.Errorf("messages[%d].role: %q is not a role; one of [%s %s %s]", i, msg.Role, RoleSystem, RoleUser, RoleAssistant)
		}
		if strings.TrimSpace(msg.Content) == "" {
			return fmt.Errorf("messages[%d].content: must not be empty", i)
		}
	}
	if last := msgs[len(msgs)-1]; last.Role != RoleUser {
		return fmt.Errorf("the last message is %q; a conversation ends with a %s message, which is what the proposers answer", last.Role, RoleUser)
	}
	return nil
}

func rejectCodingFields(m *manifest) error {
	const msg = "belongs to the coding face"
	switch {
	case m.Repo != "":
		return &ValidationError{"repo", msg}
	case m.Rev != "":
		return &ValidationError{"rev", msg}
	case m.Files != nil:
		return &ValidationError{"files", msg}
	case m.Verify != (manifest{}).Verify:
		return &ValidationError{"verify", msg}
	case m.Mutants != nil:
		return &ValidationError{"mutants", msg}
	case m.Doctor != nil:
		return &ValidationError{"doctor", msg}
	case m.Reference != nil && m.Reference.Diff != "":
		return &ValidationError{"reference.diff", msg}
	}
	return nil
}
