package serve

import (
	"fmt"
	"net/http"

	"github.com/Kaikei-e/CMoA/internal/selection"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

// answered is one finished request: either a completion or an API error.
type answered struct {
	completion *completion
	apiErr     *apiError
	status     int
}

func (s *Server) respond(dir trace.Dir, id trace.RunID, sel selection.Selection) (*answered, error) {
	run, err := dir.ReadRun()
	if err != nil {
		return nil, err
	}
	report, err := dir.ReadJudge()
	if err != nil {
		return nil, err
	}
	ext, usage := responseMetadata(dir, id, *run, *report, sel)

	switch v := sel.(type) {
	case selection.Selected:
		answer, err := dir.ReadCandidateAnswer(string(v.CandidateID))
		if err != nil {
			return nil, err
		}
		return &answered{
			status: http.StatusOK,
			completion: &completion{
				ID:      "chatcmpl-" + string(id),
				Object:  "chat.completion",
				Created: s.opt.Now().UTC().Unix(),
				Model:   s.cfg.Serve.PoolName,
				Choices: []choice{{
					Index: 0,
					Message: &message{
						Role:    task.RoleAssistant,
						Content: answer,
					},
					FinishReason: "stop",
				}},
				Usage: usage,
				CMoA:  ext,
			},
		}, nil
	case selection.NoCandidate:
		return selectionError(http.StatusBadGateway, "no candidate was selected: "+string(v.Reason), "no_candidate", string(v.Reason), id), nil
	case selection.JudgeTimeout:
		return selectionError(http.StatusGatewayTimeout, "the judge did not answer in time, after "+v.After.String(), "judge_timeout", "judge_timeout", id), nil
	case selection.JudgeFailed:
		return selectionError(http.StatusBadGateway, "the judge could not be asked: "+v.Err.Error(), "judge_failed", "judge_failed", id), nil
	case selection.VerifierFailed:
	}
	return nil, fmt.Errorf("serve: run %s produced a coding-face selection", id)
}

func selectionError(status int, message, kind, code string, id trace.RunID) *answered {
	return &answered{
		status: status,
		apiErr: &apiError{
			Message: message,
			Type:    kind,
			Code:    code,
			Param:   string(id),
		},
	}
}

func responseMetadata(dir trace.Dir, id trace.RunID, run trace.Run, report trace.JudgeReport, sel selection.Selection) (extension, usageBlock) {
	record := selection.Record(sel)
	ext := extension{
		RunID: string(id),
		Selection: selectionInfo{
			Kind:   string(record.Kind),
			Reason: publicReason(record.Reason),
		},
		Judge: judgeInfo{
			Calls:                2 * len(report.Pairs),
			SwapConsistentPairs:  report.SwapConsistentPairs,
			InvalidOutputRetries: report.InvalidOutputRetries,
			LatencyMS:            report.LatencyMS,
		},
	}
	if score, ok := report.Scores[report.Outcome.CandidateID]; ok && record.Kind == trace.SelectionSelected {
		ext.Selection.Score = &score
	}
	if consensus := report.Consensus; consensus != nil {
		largest := 0
		for _, group := range consensus.Groups {
			if len(group) > largest {
				largest = len(group)
			}
		}
		ext.Selection.Consensus = &consensusInfo{
			Normalisation: consensus.Normalisation,
			Agreement:     string(consensus.Agreement),
			Agreed:        largest,
			Of:            len(report.Candidates),
		}
	}
	if tieBreak := report.TieBreak; tieBreak != nil {
		ext.Selection.TieBreak = &tieBreakInfo{
			Key:   string(tieBreak.Key),
			Among: len(tieBreak.Among),
		}
	}
	if run.Harness.Render != nil {
		ext.Harness = &harnessInfo{TreeSHA256: run.Harness.Render.TreeSHA256}
	}
	usage := usageBlock{
		PromptTokens:     report.Usage.PromptTokens,
		CompletionTokens: report.Usage.CompletionTokens,
	}
	for _, proposer := range run.Proposers {
		candidate, err := dir.ReadCandidate(proposer.ID)
		if err != nil {
			continue
		}
		ext.Candidates.Asked++
		if candidate.Status == trace.CandidateOK {
			ext.Candidates.OK++
		}
		usage.PromptTokens += candidate.Usage.PromptTokens
		usage.CompletionTokens += candidate.Usage.CompletionTokens
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return ext, usage
}
