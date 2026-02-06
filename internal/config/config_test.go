package config

import "testing"

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil) returned error: %v", err)
	}

	if cfg.Version != 1 {
		t.Fatalf("expected version=1, got %d", cfg.Version)
	}
	if !cfg.AllowAdhocFilters() {
		t.Fatalf("expected allowAdhocFilters=true by default")
	}
}

func TestParseDuplicatePreset(t *testing.T) {
	input := []byte(`
version: 1
presets:
  - id: dup
  - id: dup
`)

	_, err := Parse(input)
	if err == nil {
		t.Fatalf("expected duplicate preset error")
	}
}

func TestParseValidPreset(t *testing.T) {
	input := []byte(`
version: 1
options:
  allowAdhocFilters: false
presets:
  - id: pdfs
    includeGlobs: ["rules/**/*.pdf"]
`)

	cfg, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if cfg.AllowAdhocFilters() {
		t.Fatalf("expected allowAdhocFilters=false")
	}
	if _, ok := cfg.PresetByID("pdfs"); !ok {
		t.Fatalf("expected preset to be loaded")
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	cfg := RepoConfig{
		Options: Options{},
		Presets: []Preset{
			{ID: "one"},
		},
	}
	if err := NormalizeAndValidate(&cfg); err != nil {
		t.Fatalf("NormalizeAndValidate failed: %v", err)
	}
	if cfg.Version != 1 {
		t.Fatalf("expected default version=1, got %d", cfg.Version)
	}
	if !cfg.AllowAdhocFilters() {
		t.Fatalf("expected allowAdhocFilters default true")
	}
}
