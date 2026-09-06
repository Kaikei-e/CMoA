package judge

import (
	"encoding/json"

	"github.com/Kaikei-e/CMoA/internal/config"
	"github.com/Kaikei-e/CMoA/internal/trace"
)

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
