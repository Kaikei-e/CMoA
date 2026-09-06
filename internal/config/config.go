// Package config reads cmoa.json, the one file that decides which proposers
// run, where the harness vault is, and how candidates are verified. Loading
// fills defaults and validates every field, so the rest of CMoA never sees
// a half-formed configuration. YAML is deliberately not accepted.
//
// cmoa.json has two versions. Version 1 is the coding face: proposers, the
// vault, the verifier. Version 2 adds the chat face's two blocks, `judge`
// and `serve`; a version 1 file means "no judge, no serve" and refuses
// either block rather than ignoring it.
package config

import "encoding/json"

// Config is the effective configuration: what the file said, plus defaults.
type Config struct {
	Version   int        `json:"version"`
	Proposers []Proposer `json:"proposers"`
	Harness   Harness    `json:"harness"`
	Verify    Verify     `json:"verify"`
	Selection Selection  `json:"selection"`
	Judge     *Judge     `json:"judge,omitempty"` // version 2; nil means the chat face is not configured
	Serve     *Serve     `json:"serve,omitempty"` // version 2; nil means cmoa serve is not configured
}

// Judge is the single model that selects on the chat face. It is
// deliberately one endpoint and not a pool: a panel of judges buys far less
// than its cost, and the one judge is measured by calibration instead.
type Judge struct {
	BaseURL        string   `json:"base_url"`
	Model          string   `json:"model"`
	Temperature    *float64 `json:"temperature"` // nil in the file means DefaultJudgeTemperature
	MaxTokens      int      `json:"max_tokens"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	// Seed nil means "derive from the run id", which is recorded either way.
	Seed     *int64 `json:"seed,omitempty"`
	Parallel int    `json:"parallel"`
	// OutputFormat is how the judge's JSON object is constrained.
	OutputFormat JudgeOutputFormat          `json:"output_format"`
	APIKeyEnv    string                     `json:"api_key_env,omitempty"`
	ExtraBody    map[string]json.RawMessage `json:"extra_body,omitempty"`
}

// JudgeOutputFormat is how the judge is made to answer in JSON. It is a
// closed enumeration; add a constant, and the exhaustive linter finds every
// switch that must learn it.
type JudgeOutputFormat string

const (
	// OutputJSONSchema sends an OpenAI `response_format` of type
	// `json_schema`, which a server composes with its own chat format. It
	// fixes the key order — the reason is written before the choice, so the
	// choice cannot be reached without passing through it — and bounds the
	// reason and the choice enum.
	OutputJSONSchema JudgeOutputFormat = "json_schema"
	// OutputNone constrains nothing and relies on the prompt alone. It is
	// what a server that does not implement response_format needs.
	OutputNone JudgeOutputFormat = "none"
)

// Serve configures the OpenAI-compatible HTTP face. It has no auth and no
// TLS: it binds loopback, and a non-loopback address needs --allow-remote
// on the command line, where a person types it.
type Serve struct {
	Listen       string `json:"listen"`
	PoolName     string `json:"pool_name"`
	RunsDir      string `json:"runs_dir"` // relative to the config file; absolute after Load
	MaxBodyBytes int64  `json:"max_body_bytes"`
	MaxInflight  int    `json:"max_inflight"`
}

// Proposer is one model endpoint the router asks for a candidate.
type Proposer struct {
	ID             ProposerID                 `json:"id"`
	BaseURL        string                     `json:"base_url"`
	Model          string                     `json:"model"`
	Temperature    *float64                   `json:"temperature"` // nil in the file means DefaultTemperature
	MaxTokens      int                        `json:"max_tokens"`
	TimeoutSeconds int                        `json:"timeout_seconds"`
	Seed           *int64                     `json:"seed,omitempty"`
	APIKeyEnv      string                     `json:"api_key_env,omitempty"`
	ExtraBody      map[string]json.RawMessage `json:"extra_body,omitempty"`
}

// Harness names the DocDag vault whose binding documents the run reads.
type Harness struct {
	Vault  string `json:"vault"`
	Docdag string `json:"docdag"`
}

// Verify bounds the per-candidate verifier containers.
type Verify struct {
	MaxParallel    int `json:"max_parallel"`
	TimeoutSeconds int `json:"timeout_seconds"`
}

// Selection names the rule that picks among passing candidates.
type Selection struct {
	Rule SelectionRule `json:"rule"`
}

// SelectionRule is a closed enumeration; add a constant, and the exhaustive
// linter finds every switch that must learn it.
type SelectionRule string

const (
	// RuleFirst selects the first passing candidate in configured order.
	RuleFirst SelectionRule = "first"
)

// Defaults.
const (
	DefaultTemperature    = 0.2
	DefaultMaxTokens      = 4096
	DefaultTimeoutSeconds = 300
	DefaultDocdag         = "docdag"
	DefaultMaxParallel    = 1
	DefaultVerifyTimeout  = 600
)

// Defaults for the chat face.
const (
	DefaultJudgeTemperature = 0.0
	DefaultJudgeMaxTokens   = 512
	DefaultJudgeTimeout     = 120
	DefaultJudgeParallel    = 1
	DefaultOutputFormat     = OutputJSONSchema
	DefaultListen           = "127.0.0.1:8095"
	DefaultPoolName         = "cmoa"
	DefaultRunsDir          = "runs"
	DefaultMaxBodyBytes     = 1 << 20
	DefaultMaxInflight      = 1
)

// MaxVersion is the newest cmoa.json this build understands.
const MaxVersion = 2

// ValidationError reports one field that failed validation.
type ValidationError struct {
	Path string // JSON path, e.g. proposers[1].base_url
	Msg  string
}

func (e *ValidationError) Error() string { return "cmoa.json: " + e.Path + ": " + e.Msg }
