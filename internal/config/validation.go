package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
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
	if err := c.fillProposers(); err != nil {
		return err
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

// fillProposers validates the ordered candidate pool and supplies each
// proposer's request defaults.
func (c *Config) fillProposers() error {
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
		if err := normalizeHTTPURL(at+".base_url", &p.BaseURL); err != nil {
			return err
		}
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
	return nil
}

// normalizeHTTPURL validates an endpoint and removes its optional trailing
// slash, so proposers and the judge use the same wire endpoint form.
func normalizeHTTPURL(path string, value *string) error {
	u, err := url.Parse(*value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return &ValidationError{path, fmt.Sprintf("%q must be an http(s) URL", *value)}
	}
	*value = strings.TrimRight(*value, "/")
	return nil
}

func (c *Config) fillJudge() error {
	j := c.Judge
	if j == nil {
		return nil
	}
	if err := normalizeHTTPURL("judge.base_url", &j.BaseURL); err != nil {
		return err
	}
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
	if j.OutputFormat == "" {
		j.OutputFormat = DefaultOutputFormat
	}
	switch j.OutputFormat {
	case OutputJSONSchema, OutputNone:
	default:
		return &ValidationError{"judge.output_format", fmt.Sprintf("%q is not an output format; one of [%s %s]", j.OutputFormat, OutputJSONSchema, OutputNone)}
	}
	if j.APIKeyEnv != "" && !envNamePattern.MatchString(j.APIKeyEnv) {
		return &ValidationError{"judge.api_key_env", fmt.Sprintf("%q is not an environment variable name", j.APIKeyEnv)}
	}
	for k := range j.ExtraBody {
		switch k {
		case "model", "messages", "temperature", "max_tokens", "seed", "stream", "n", "response_format":
			return &ValidationError{"judge.extra_body." + k, "is set by CMoA and cannot be overridden"}
		case "grammar":
			// A raw GBNF grammar is parsed beside the server's own chat
			// format rather than composed with it, and a judge that speaks
			// a structured chat format answers HTTP 500 to one. The
			// supported way to constrain the answer is
			// judge.output_format.
			return &ValidationError{"judge.extra_body.grammar", "a raw grammar fights the judge's chat format; use judge.output_format instead"}
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
