package config

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"zip-forger/internal/filter"
)

const FileName = ".zip-forger.yaml"

type RepoConfig struct {
	Version int      `yaml:"version" json:"version"`
	Options Options  `yaml:"options" json:"options"`
	Presets []Preset `yaml:"presets" json:"presets"`
}

type Options struct {
	AllowAdhocFilters   *bool `yaml:"allowAdhocFilters" json:"allowAdhocFilters"`
	MaxFilesPerDownload int   `yaml:"maxFilesPerDownload" json:"maxFilesPerDownload"`
	MaxBytesPerDownload int64 `yaml:"maxBytesPerDownload" json:"maxBytesPerDownload"`
}

type Preset struct {
	ID           string   `yaml:"id" json:"id"`
	Description  string   `yaml:"description" json:"description"`
	IncludeGlobs []string `yaml:"includeGlobs" json:"includeGlobs"`
	ExcludeGlobs []string `yaml:"excludeGlobs" json:"excludeGlobs"`
	Extensions   []string `yaml:"extensions" json:"extensions"`
	PathPrefixes []string `yaml:"pathPrefixes" json:"pathPrefixes"`
}

func Default() RepoConfig {
	return RepoConfig{
		Version: 1,
		Options: Options{
			AllowAdhocFilters: boolPtr(true),
		},
		Presets: nil,
	}
}

func Parse(data []byte) (RepoConfig, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		cfg := Default()
		return cfg, nil
	}

	cfg := Default()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return RepoConfig{}, fmt.Errorf("config parse error: %w", err)
	}
	if err := validate(cfg); err != nil {
		return RepoConfig{}, err
	}
	applyDefaults(&cfg)
	return cfg, nil
}

func NormalizeAndValidate(cfg *RepoConfig) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	applyDefaults(cfg)
	return validate(*cfg)
}

func (c RepoConfig) AllowAdhocFilters() bool {
	if c.Options.AllowAdhocFilters == nil {
		return true
	}
	return *c.Options.AllowAdhocFilters
}

func (c RepoConfig) PresetByID(id string) (Preset, bool) {
	for _, preset := range c.Presets {
		if preset.ID == id {
			return preset, true
		}
	}
	return Preset{}, false
}

func (p Preset) Criteria() filter.Criteria {
	return filter.Criteria{
		IncludeGlobs: p.IncludeGlobs,
		ExcludeGlobs: p.ExcludeGlobs,
		Extensions:   p.Extensions,
		PathPrefixes: p.PathPrefixes,
	}
}

func applyDefaults(cfg *RepoConfig) {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Options.AllowAdhocFilters == nil {
		cfg.Options.AllowAdhocFilters = boolPtr(true)
	}
}

func validate(cfg RepoConfig) error {
	if cfg.Version != 1 && cfg.Version != 0 {
		return fmt.Errorf("unsupported config version: %d", cfg.Version)
	}
	if cfg.Options.MaxFilesPerDownload < 0 {
		return errors.New("options.maxFilesPerDownload must be >= 0")
	}
	if cfg.Options.MaxBytesPerDownload < 0 {
		return errors.New("options.maxBytesPerDownload must be >= 0")
	}
	seen := make(map[string]struct{}, len(cfg.Presets))
	for idx, preset := range cfg.Presets {
		presetID := strings.TrimSpace(preset.ID)
		if presetID == "" {
			return fmt.Errorf("presets[%d].id is required", idx)
		}
		if _, ok := seen[presetID]; ok {
			return fmt.Errorf("duplicate preset id: %s", presetID)
		}
		seen[presetID] = struct{}{}
	}
	return nil
}

func boolPtr(v bool) *bool {
	return &v
}
