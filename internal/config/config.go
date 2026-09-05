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

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	Seed      *int64                     `json:"seed,omitempty"`
	Parallel  int                        `json:"parallel"`
	Grammar   bool                       `json:"grammar"`
	APIKeyEnv string                     `json:"api_key_env,omitempty"`
	ExtraBody map[string]json.RawMessage `json:"extra_body,omitempty"`
}

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

// Defaults for the chat face.
const (
	DefaultJudgeTemperature = 0.0
	DefaultJudgeMaxTokens   = 512
	DefaultJudgeTimeout     = 120
	DefaultJudgeParallel    = 1
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
	// Relative vault and runs paths are relative to the config file.
	cfg.Harness.Vault = absoluteTo(filepath.Dir(path), cfg.Harness.Vault)
	if cfg.Serve != nil {
		cfg.Serve.RunsDir = absoluteTo(filepath.Dir(path), cfg.Serve.RunsDir)
	}
	return cfg, nil
}

func absoluteTo(base, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
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
	if c.Version < 1 || c.Version > MaxVersion {
		return &ValidationError{"version", fmt.Sprintf("must be between 1 and %d, got %d", MaxVersion, c.Version)}
	}
	if c.Version == 1 {
		switch {
		case c.Judge != nil:
			return &ValidationError{"judge", "requires version 2"}
		case c.Serve != nil:
			return &ValidationError{"serve", "requires version 2"}
		}
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
	if err := c.fillJudge(); err != nil {
		return err
	}
	return c.fillServe()
}

func (c *Config) fillJudge() error {
	j := c.Judge
	if j == nil {
		return nil
	}
	u, err := url.Parse(j.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return &ValidationError{"judge.base_url", fmt.Sprintf("%q must be an http(s) URL", j.BaseURL)}
	}
	j.BaseURL = strings.TrimRight(j.BaseURL, "/")
	if strings.TrimSpace(j.Model) == "" {
		return &ValidationError{"judge.model", "must not be empty"}
	}
	if j.Temperature == nil {
		t := DefaultJudgeTemperature
		j.Temperature = &t
	}
	if *j.Temperature < 0 || *j.Temperature > 2 {
		return &ValidationError{"judge.temperature", fmt.Sprintf("%v is outside [0, 2]", *j.Temperature)}
	}
	if j.MaxTokens == 0 {
		j.MaxTokens = DefaultJudgeMaxTokens
	}
	if j.MaxTokens < 1 {
		return &ValidationError{"judge.max_tokens", "must be positive"}
	}
	if j.TimeoutSeconds == 0 {
		j.TimeoutSeconds = DefaultJudgeTimeout
	}
	if j.TimeoutSeconds < 1 {
		return &ValidationError{"judge.timeout_seconds", "must be positive"}
	}
	if j.Parallel == 0 {
		j.Parallel = DefaultJudgeParallel
	}
	if j.Parallel < 1 {
		return &ValidationError{"judge.parallel", "must be positive"}
	}
	if j.APIKeyEnv != "" && !envNamePattern.MatchString(j.APIKeyEnv) {
		return &ValidationError{"judge.api_key_env", fmt.Sprintf("%q is not an environment variable name", j.APIKeyEnv)}
	}
	for k := range j.ExtraBody {
		switch k {
		case "model", "messages", "temperature", "max_tokens", "seed", "stream", "n", "grammar":
			return &ValidationError{"judge.extra_body." + k, "is set by CMoA and cannot be overridden"}
		}
	}
	return nil
}

func (c *Config) fillServe() error {
	s := c.Serve
	if s == nil {
		return nil
	}
	if s.Listen == "" {
		s.Listen = DefaultListen
	}
	if _, _, err := net.SplitHostPort(s.Listen); err != nil {
		return &ValidationError{"serve.listen", fmt.Sprintf("%q is not host:port: %v", s.Listen, err)}
	}
	if s.PoolName == "" {
		s.PoolName = DefaultPoolName
	}
	if !poolNamePattern.MatchString(s.PoolName) {
		return &ValidationError{"serve.pool_name", fmt.Sprintf("%q must match %s: it is the model id clients ask for", s.PoolName, poolNamePattern)}
	}
	if s.RunsDir == "" {
		s.RunsDir = DefaultRunsDir
	}
	if s.MaxBodyBytes == 0 {
		s.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if s.MaxBodyBytes < 1 {
		return &ValidationError{"serve.max_body_bytes", "must be positive"}
	}
	if s.MaxInflight == 0 {
		s.MaxInflight = DefaultMaxInflight
	}
	if s.MaxInflight < 1 {
		return &ValidationError{"serve.max_inflight", "must be positive"}
	}
	return nil
}

var (
	envNamePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	poolNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

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
	return lookupKey(p.APIKeyEnv, "proposer "+string(p.ID))
}

// APIKey resolves the judge's key from the environment, empty when none is
// configured.
func (j *Judge) APIKey() (string, error) { return lookupKey(j.APIKeyEnv, "judge") }

func lookupKey(name, who string) (string, error) {
	if name == "" {
		return "", nil
	}
	v, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("config: %s: environment variable %s is not set", who, name)
	}
	return v, nil
}
