package propose

import (
	"time"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/harness"
	"github.com/Kaikei-e/CMoA/internal/harnessdir"
	"github.com/Kaikei-e/CMoA/internal/prompt"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

func createRun(cfg *config.Config, t *task.Task, opt Options, rev string, snap *harness.Snapshot, now func() time.Time) (trace.Dir, *trace.Run, error) {
	id := opt.RunID
	if id == "" {
		id = trace.NewRunID(now())
	}
	dir, err := trace.Create(t.Dir, id)
	if err != nil {
		return "", nil, err
	}
	run, err := newRun(cfg, t, id, rev, snap, opt, now)
	if err != nil {
		return "", nil, err
	}
	if err := dir.WriteRun(run); err != nil {
		return "", nil, err
	}
	return dir, run, nil
}

// newRun builds run.json for either face. rev is empty on the chat face.
func newRun(cfg *config.Config, t *task.Task, id trace.RunID, rev string, snap *harness.Snapshot, opt Options, now func() time.Time) (*trace.Run, error) {
	effective, err := cfg.Redacted()
	if err != nil {
		return nil, err
	}
	n, f := cfg.ByzantineTolerance()
	run := &trace.Run{
		SchemaVersion: trace.SchemaVersion,
		RunID:         id,
		CreatedAt:     now().UTC(),
		CMoAVersion:   opt.Version,
		PromptVersion: prompt.Version(),
		Face:          string(t.Face),
		Task: trace.TaskRef{
			ID:                string(t.ID),
			Dir:               t.Dir,
			Repo:              t.Repo,
			Rev:               t.Rev,
			ResolvedRev:       rev,
			Files:             t.FilePaths(),
			InstructionSHA256: t.InstructionSHA256(),
		},
		Config:    effective,
		Harness:   harnessRecord(snap, opt.Harness),
		Byzantine: trace.Byzantine{N: n, F: f},
	}
	if t.Face == task.FaceChat {
		run.ConversationSHA256 = t.ConversationSHA256()
		run.CandidatesOrigin = trace.OriginProposers
	}
	for _, p := range cfg.Proposers {
		run.Proposers = append(run.Proposers, trace.ProposerRef{
			ID: string(p.ID), Model: p.Model, BaseURL: p.BaseURL,
		})
	}
	return run, nil
}

// harnessRecord converts the snapshot and optional rendered directory into
// the self-contained harness record persisted in run.json.
func harnessRecord(s *harness.Snapshot, dir *harnessdir.Dir) trace.Harness {
	h := trace.Harness{
		Vault:         s.Vault,
		AsOf:          s.AsOf,
		At:            s.At,
		DocdagVersion: s.DocdagVersion,
		Binding:       []trace.HarnessDoc{},
	}
	for _, d := range s.Binding {
		h.Binding = append(h.Binding, trace.HarnessDoc{
			ID:     d.ID,
			Title:  d.Title,
			Status: d.Status,
			Path:   d.Path,
		})
	}
	if dir != nil {
		r := &trace.HarnessRender{
			Dir:           dir.Path,
			TreeSHA256:    dir.TreeSHA256,
			RenderedBytes: dir.Harness.Bytes(),
			Files:         []trace.HarnessFile{},
		}
		for _, f := range dir.Files {
			r.Files = append(r.Files, trace.HarnessFile{Path: f.Path, SHA256: f.SHA256})
		}
		h.Render = r
	}
	return h
}
