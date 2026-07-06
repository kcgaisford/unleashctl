package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Context is one entry in ~/.unleashctl/config.yaml or ./.unleashctl.yaml,
// mirroring a kubectl context (spec §2): it pairs an instance URL with the
// one environment that instance has enabled.
type Context struct {
	Name        string `yaml:"name"`
	URL         string `yaml:"url"`
	Environment string `yaml:"environment,omitempty"`
	TokenEnv    string `yaml:"token-env,omitempty"`
	AllowSync   *bool  `yaml:"allow_sync,omitempty"`

	// UIManagedEnabled means enabled/disabled state for this context is
	// authoritative in the Unleash UI, not in flags/*.yaml: diff/apply never
	// compare or push it, only strategies/type/description/tags reconcile
	// normally. Typical use: prod flags an engineer turns on by hand rather
	// than via a manifest change.
	UIManagedEnabled bool `yaml:"ui_managed_enabled,omitempty"`
}

// AllowSyncResolved implements the name-based default from spec §2: false
// for any context named "prod"/"production" (case-insensitive) unless
// explicitly overridden via allow_sync in config; true otherwise. Phase 1
// doesn't build `sync` yet, but the config schema carries this from the
// start so it's already correct when sync is added.
func (c Context) AllowSyncResolved() bool {
	if c.AllowSync != nil {
		return *c.AllowSync
	}
	lower := strings.ToLower(c.Name)
	return lower != "prod" && lower != "production"
}

// Config is the parsed contents of a contexts file.
type Config struct {
	CurrentContext string    `yaml:"current-context"`
	Contexts       []Context `yaml:"contexts"`
}

// FindContext looks up a context by name.
func (cfg *Config) FindContext(name string) (*Context, bool) {
	for i := range cfg.Contexts {
		if cfg.Contexts[i].Name == name {
			return &cfg.Contexts[i], true
		}
	}
	return nil, false
}

func configPaths() (global, projectLocal string) {
	home, _ := os.UserHomeDir()
	global = filepath.Join(home, ".unleashctl", "config.yaml")
	projectLocal = ".unleashctl.yaml"
	return
}

// LoadConfig reads project-local config (./.unleashctl.yaml) if present,
// otherwise falls back to the global config (~/.unleashctl/config.yaml), per
// spec §2's precedence (project-local overrides global). An empty Config is
// returned (not an error) if neither file exists — --url/--token/--environment
// flags can fully substitute for a config file, per spec §2's CI use case.
func LoadConfig() (*Config, error) {
	global, projectLocal := configPaths()

	if data, err := os.ReadFile(projectLocal); err == nil {
		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", projectLocal, err)
		}
		return &cfg, nil
	}

	if data, err := os.ReadFile(global); err == nil {
		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", global, err)
		}
		return &cfg, nil
	}

	return &Config{}, nil
}
