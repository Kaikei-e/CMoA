// Package config reads cmoa.json, the one file that decides which proposers
// run, where the harness vault is, and how candidates are verified. Loading
// fills defaults and validates every field, so the rest of CMoA never sees
// a half-formed configuration. YAML is deliberately not accepted.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config is the effective configuration: what the file said, plus defaults.
type Config struct {
	Version   int        `json:"version"`
	Proposers []Proposer `json:"proposers"`
	Harness   Harness    `json:"harness"`
	Verify    Verify     `json:"verify"`
	Selection Selection  `json:"selection"`
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

// ProposerID is a validated identifier; it names files under a run
// directory, so its alphabet is restricted.
type ProposerID string

var proposerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ParseProposerID validates s.
func ParseProposerID(s string) (ProposerID, error) {
	if !proposerIDPattern.MatchString(s) {
		return "", fmt.Errorf("proposer id %q must match %s", s, proposerIDPattern)
	}
	return ProposerID(s), nil
}

// Defaults.
const (
	DefaultTemperature    = 0.2
	DefaultMaxTokens      = 4096
	DefaultTimeoutSeconds = 300
	DefaultDocdag         = "docdag"
	DefaultMaxParallel    = 1
	DefaultVerifyTimeout  = 600
)

// ValidationError reports one field that failed validation.
type ValidationError struct {
	Path string // JSON path, e.g. proposers[1].base_url
	Msg  string
}

func (e *ValidationError) Error() string { return "cmoa.json: " + e.Path + ": " + e.Msg }

// Load reads and validates the file at path.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	cfg, err := Parse(b)
	if err != nil {
		return nil, err
	}
	// Relative vault paths are relative to the config file.
	if !filepath.IsAbs(cfg.Harness.Vault) {
		cfg.Harness.Vault = filepath.Join(filepath.Dir(path), cfg.Harness.Vault)
	}
	if abs, err := filepath.Abs(cfg.Harness.Vault); err == nil {
		cfg.Harness.Vault = abs
	}
	return cfg, nil
}

// Parse decodes and validates JSON bytes. Unknown fields are errors: a typo
// in a key must not silently fall back to a default.
func Parse(b []byte) (*Config, error) {
	var cfg Config
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("config: decode: %w", err)
	}
	if err := cfg.fillAndValidate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) fillAndValidate() error {
	if c.Version != 1 {
		return &ValidationError{"version", fmt.Sprintf("must be 1, got %d", c.Version)}
	}
	if len(c.Proposers) == 0 {
		return &ValidationError{"proposers", "at least one proposer is required"}
	}
	seen := map[ProposerID]bool{}
	for i := range c.Proposers {
		p := &c.Proposers[i]
		at := fmt.Sprintf("proposers[%d]", i)
		id, err := ParseProposerID(string(p.ID))
		if err != nil {
			return &ValidationError{at + ".id", err.Error()}
		}
		if seen[id] {
			return &ValidationError{at + ".id", fmt.Sprintf("duplicate id %q", id)}
		}
		seen[id] = true
		u, err := url.Parse(p.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return &ValidationError{at + ".base_url", fmt.Sprintf("%q must be an http(s) URL", p.BaseURL)}
		}
		p.BaseURL = strings.TrimRight(p.BaseURL, "/")
		if strings.TrimSpace(p.Model) == "" {
			return &ValidationError{at + ".model", "must not be empty"}
		}
		if p.Temperature == nil {
			t := DefaultTemperature
			p.Temperature = &t
		}
		if *p.Temperature < 0 || *p.Temperature > 2 {
			return &ValidationError{at + ".temperature", fmt.Sprintf("%v is outside [0, 2]", *p.Temperature)}
		}
		if p.MaxTokens == 0 {
			p.MaxTokens = DefaultMaxTokens
		}
		if p.MaxTokens < 1 {
			return &ValidationError{at + ".max_tokens", "must be positive"}
		}
		if p.TimeoutSeconds == 0 {
			p.TimeoutSeconds = DefaultTimeoutSeconds
		}
		if p.TimeoutSeconds < 1 {
			return &ValidationError{at + ".timeout_seconds", "must be positive"}
		}
		if p.APIKeyEnv != "" && !envNamePattern.MatchString(p.APIKeyEnv) {
			return &ValidationError{at + ".api_key_env", fmt.Sprintf("%q is not an environment variable name", p.APIKeyEnv)}
		}
		for k := range p.ExtraBody {
			switch k {
			case "model", "messages", "temperature", "max_tokens", "seed", "stream", "n":
				return &ValidationError{at + ".extra_body." + k, "is set by CMoA and cannot be overridden"}
			}
		}
	}
	if strings.TrimSpace(c.Harness.Vault) == "" {
		return &ValidationError{"harness.vault", "is required: CMoA records which harness a run read"}
	}
	if c.Harness.Docdag == "" {
		c.Harness.Docdag = DefaultDocdag
	}
	if c.Verify.MaxParallel == 0 {
		c.Verify.MaxParallel = DefaultMaxParallel
	}
	if c.Verify.MaxParallel < 1 {
		return &ValidationError{"verify.max_parallel", "must be positive"}
	}
	if c.Verify.TimeoutSeconds == 0 {
		c.Verify.TimeoutSeconds = DefaultVerifyTimeout
	}
	if c.Verify.TimeoutSeconds < 1 {
		return &ValidationError{"verify.timeout_seconds", "must be positive"}
	}
	if c.Selection.Rule == "" {
		c.Selection.Rule = RuleFirst
	}
	switch c.Selection.Rule {
	case RuleFirst:
	default:
		return &ValidationError{"selection.rule", fmt.Sprintf("%q is not a selection rule; one of [first]", c.Selection.Rule)}
	}
	return nil
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Discover returns the config path from, in order, the --config flag, the
// CMOA_CONFIG environment variable, <taskDir>/cmoa.json, ./cmoa.json.
func Discover(flagValue, taskDir string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if v := os.Getenv("CMOA_CONFIG"); v != "" {
		return v, nil
	}
	candidates := []string{filepath.Join(taskDir, "cmoa.json"), "cmoa.json"}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("config: no cmoa.json found (use --config, $CMOA_CONFIG, <task>/cmoa.json or ./cmoa.json)")
}

// ByzantineTolerance returns the pool size and how many deceptive proposers
// it tolerates: f = floor((n-1)/3). Three proposers tolerate none.
func (c *Config) ByzantineTolerance() (n, f int) {
	n = len(c.Proposers)
	return n, (n - 1) / 3
}

// Redacted returns the effective config as JSON with nothing secret in it.
// The API key never lives in the file (only its variable name), so this is
// a plain encoding; it exists so callers do not reach for json.Marshal and
// forget the day a secret field is added.
func (c *Config) Redacted() (json.RawMessage, error) {
	return json.Marshal(c)
}

// APIKey resolves the proposer's key from the environment, empty when none
// is configured.
func (p *Proposer) APIKey() (string, error) {
	if p.APIKeyEnv == "" {
		return "", nil
	}
	v, ok := os.LookupEnv(p.APIKeyEnv)
	if !ok {
		return "", fmt.Errorf("config: proposer %s: environment variable %s is not set", p.ID, p.APIKeyEnv)
	}
	return v, nil
}
