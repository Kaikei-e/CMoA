package serve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kaikei-e/CMoA/internal/judge"
	"github.com/Kaikei-e/CMoA/internal/propose"
	"github.com/Kaikei-e/CMoA/internal/selection"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// answer materialises a task, runs its proposers, and selects an answer.
func (s *Server) answer(ctx context.Context, req request) (*answered, error) {
	id := trace.NewRunID(s.opt.Now())
	taskDir := filepath.Join(s.cfg.Serve.RunsDir, string(id))
	t, err := s.writeTask(taskDir, id, req.Messages)
	if err != nil {
		return nil, err
	}
	s.opt.Log("%s: %d messages", id, len(req.Messages))

	dir, err := propose.Run(ctx, s.cfg, t, propose.Options{
		AsOf: s.opt.AsOf, RunID: id, Client: s.opt.Client, Version: s.opt.Version,
		Harness: s.opt.Harness, Log: s.opt.Log, Now: s.opt.Now,
	})
	if err != nil {
		return nil, err
	}
	sel, err := selection.RunChat(ctx, s.cfg, t, dir, selection.ChatOptions{
		Client: judge.Live{Client: s.opt.Client}, Log: s.opt.Log, Now: s.opt.Now,
	})
	if err != nil {
		return nil, err
	}
	return s.respond(dir, id, sel)
}

// writeTask materialises the request as a regular chat task so that a served
// answer can be reproduced, judged again, or added to a suite.
func (s *Server) writeTask(dir string, id trace.RunID, msgs []task.ConvMessage) (*task.Task, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	conv, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, task.ConversationFile), append(conv, '\n'), 0o644); err != nil {
		return nil, err
	}
	// A run id is a valid task id once lower-cased, and reusing it means
	// the task directory, the run directory and the completion id all name
	// the same request.
	manifest := map[string]any{
		"version":      3,
		"id":           strings.ToLower(string(id)),
		"face":         string(task.FaceChat),
		"conversation": task.ConversationFile,
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, task.ManifestFile), append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	return task.Load(dir, task.WithLog(s.opt.Log))
}
