package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const minimal = `{
  "version": 1,
  "proposers": [{"id": "a", "base_url": "http://127.0.0.1:8081/v1/", "model": "m"}],
  "harness": {"vault": "vault"}
}`

func TestParseFillsDefaults(t *testing.T) {
	cfg, err := Parse([]byte(minimal))
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Proposers[0]
	if *p.Temperature != DefaultTemperature || p.MaxTokens != DefaultMaxTokens || p.TimeoutSeconds != DefaultTimeoutSeconds {
		t.Fatalf("defaults not filled: %+v", p)
	}
	if p.BaseURL != "http://127.0.0.1:8081/v1" {
		t.Fatalf("trailing slash not trimmed: %q", p.BaseURL)
	}
	if cfg.Harness.Docdag != DefaultDocdag || cfg.Verify.MaxParallel != 1 || cfg.Verify.TimeoutSeconds != DefaultVerifyTimeout || cfg.Selection.Rule != RuleFirst {
		t.Fatalf("defaults not filled: %+v", cfg)
	}
	if n, f := cfg.ByzantineTolerance(); n != 1 || f != 0 {
		t.Fatalf("byzantine = %d,%d", n, f)
	}
}

func TestExplicitZeroTemperatureIsKept(t *testing.T) {
	cfg, err := Parse([]byte(`{"version":1,"proposers":[{"id":"a","base_url":"http://h","model":"m","temperature":0}],"harness":{"vault":"v"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if *cfg.Proposers[0].Temperature != 0 {
		t.Fatalf("temperature 0 must be honoured, got %v", *cfg.Proposers[0].Temperature)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"version":                       `{"version":2,"proposers":[{"id":"a","base_url":"http://h","model":"m"}],"harness":{"vault":"v"}}`,
		"proposers":                     `{"version":1,"proposers":[],"harness":{"vault":"v"}}`,
		"proposers[0].id":               `{"version":1,"proposers":[{"id":"Bad_ID","base_url":"http://h","model":"m"}],"harness":{"vault":"v"}}`,
		"proposers[1].id":               `{"version":1,"proposers":[{"id":"a","base_url":"http://h","model":"m"},{"id":"a","base_url":"http://h","model":"m"}],"harness":{"vault":"v"}}`,
		"proposers[0].base_url":         `{"version":1,"proposers":[{"id":"a","base_url":"ftp://h","model":"m"}],"harness":{"vault":"v"}}`,
		"proposers[0].model":            `{"version":1,"proposers":[{"id":"a","base_url":"http://h","model":" "}],"harness":{"vault":"v"}}`,
		"proposers[0].temperature":      `{"version":1,"proposers":[{"id":"a","base_url":"http://h","model":"m","temperature":3}],"harness":{"vault":"v"}}`,
		"proposers[0].extra_body.model": `{"version":1,"proposers":[{"id":"a","base_url":"http://h","model":"m","extra_body":{"model":"x"}}],"harness":{"vault":"v"}}`,
		"harness.vault":                 `{"version":1,"proposers":[{"id":"a","base_url":"http://h","model":"m"}]}`,
		"verify.max_parallel":           `{"version":1,"proposers":[{"id":"a","base_url":"http://h","model":"m"}],"harness":{"vault":"v"},"verify":{"max_parallel":-1}}`,
		"selection.rule":                `{"version":1,"proposers":[{"id":"a","base_url":"http://h","model":"m"}],"harness":{"vault":"v"},"selection":{"rule":"random"}}`,
	}
	for path, src := range cases {
		_, err := Parse([]byte(src))
		ve, ok := errors.AsType[*ValidationError](err)
		if !ok {
			t.Errorf("%s: want ValidationError, got %v", path, err)
			continue
		}
		if ve.Path != path {
			t.Errorf("want error at %s, got %s (%s)", path, ve.Path, ve.Msg)
		}
	}
}

func TestUnknownFieldIsAnError(t *testing.T) {
	if _, err := Parse([]byte(`{"version":1,"proposerz":[],"harness":{"vault":"v"}}`)); err == nil {
		t.Fatal("unknown field must be rejected")
	}
}

func TestLoadResolvesVaultRelativeToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cmoa.json")
	if err := os.WriteFile(path, []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Harness.Vault != filepath.Join(dir, "vault") {
		t.Fatalf("vault = %q", cfg.Harness.Vault)
	}
}

func TestDiscover(t *testing.T) {
	if p, _ := Discover("x.json", ""); p != "x.json" {
		t.Fatal("flag must win")
	}
	t.Setenv("CMOA_CONFIG", "env.json")
	if p, _ := Discover("", ""); p != "env.json" {
		t.Fatal("env must be second")
	}
	t.Setenv("CMOA_CONFIG", "")
	task := t.TempDir()
	os.WriteFile(filepath.Join(task, "cmoa.json"), []byte("{}"), 0o644)
	if p, _ := Discover("", task); p != filepath.Join(task, "cmoa.json") {
		t.Fatalf("task dir third, got %s", p)
	}
	if _, err := Discover("", t.TempDir()); err == nil {
		t.Fatal("expected not found")
	}
}

func TestAPIKey(t *testing.T) {
	p := Proposer{ID: "a", APIKeyEnv: "CMOA_TEST_KEY"}
	if _, err := p.APIKey(); err == nil {
		t.Fatal("unset env must error")
	}
	t.Setenv("CMOA_TEST_KEY", "s3")
	if k, err := p.APIKey(); err != nil || k != "s3" {
		t.Fatalf("APIKey = %q, %v", k, err)
	}
	if k, err := (&Proposer{}).APIKey(); err != nil || k != "" {
		t.Fatalf("no env configured: %q, %v", k, err)
	}
}
