package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
