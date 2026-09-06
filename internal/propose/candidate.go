package propose

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Kaikei-e/CMoA/internal/llm"
	"github.com/Kaikei-e/CMoA/internal/patch"
	"github.com/Kaikei-e/CMoA/internal/task"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

func classifyCompletion(face task.Face, cand *trace.Candidate, content string) string {
	switch face {
	case task.FaceChat:
		return classifyChat(cand, content)
	case task.FaceCoding:
		return classifyDiff(cand, content)
	default:
		return ""
	}
}

// classifyChat sets the candidate's status from the completion text and
// returns the answer. An answer that is only whitespace is `empty`: the
// proposer answered, and answered nothing, which is a different failure
// from not answering at all.
func classifyChat(cand *trace.Candidate, content string) string {
	answer := strings.TrimSpace(content)
	if answer == "" {
		cand.Status = trace.CandidateEmpty
		cand.Error = "the completion held no text"
		return ""
	}
	cand.Status = trace.CandidateOK
	cand.AnswerSHA256 = llm.SHA256([]byte(answer))
	cand.AnswerBytes = len(answer)
	cand.Metadata = AnswerMetadata(answer, cand.Usage.CompletionTokens)
	return answer
}

var (
	headerLine    = regexp.MustCompile(`(?m)^#{1,6} +\S`)
	listLine      = regexp.MustCompile(`(?m)^[ \t]*(?:[-*+]|[0-9]+[.)]) +\S`)
	boldSpan      = regexp.MustCompile(`\*\*[^*\n]+\*\*`)
	codeFenceLine = regexp.MustCompile("(?m)^[ \t]*(?:```|~~~)")
)

// AnswerMetadata is the style accounting a preference harness records for
// every answer: how long it is and how decorated. None of it reaches the judge —
// it exists so a later analysis can ask whether the judge was buying length
// and formatting, and that question cannot be answered by numbers nobody
// wrote down at the time. tokens is the server's completion_tokens; -1 is
// recorded when it reported none.
func AnswerMetadata(answer string, tokens int) *trace.CandidateMetadata {
	if tokens <= 0 {
		tokens = -1
	}
	return &trace.CandidateMetadata{
		TokenLen:       tokens,
		Chars:          utf8.RuneCountInString(answer),
		HeaderCount:    len(headerLine.FindAllString(answer, -1)),
		ListCount:      len(listLine.FindAllString(answer, -1)),
		BoldCount:      len(boldSpan.FindAllString(answer, -1)),
		CodeFenceCount: len(codeFenceLine.FindAllString(answer, -1)),
	}
}

// classifyDiff sets the candidate's status from the completion text and
// returns the extracted diff (empty when there is none).
func classifyDiff(cand *trace.Candidate, content string) string {
	d, err := patch.Extract(content)
	if err != nil {
		cand.Status = trace.CandidateNoDiff
		cand.Error = err.Error()
		return ""
	}
	st, err := patch.ComputeStats(d)
	if err != nil {
		cand.Status = trace.CandidateNoDiff
		cand.Error = err.Error()
		return ""
	}
	cand.Status = trace.CandidateOK
	cand.Diff = &trace.DiffStats{
		Files:     st.Files,
		Additions: st.Additions,
		Deletions: st.Deletions,
		SHA256:    st.SHA256,
	}
	return d
}
