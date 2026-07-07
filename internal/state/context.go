package state

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LegalValue is one entry under spec/legalValues — a single allowed value
// for a context field, with an optional human-readable description.
type LegalValue struct {
	Value       string  `yaml:"value"`
	Description *string `yaml:"description,omitempty"`
}

// ContextSpec is the desired configuration of a custom context field.
// Unlike Feature's Spec, there's no environment/context dimension here —
// context fields are global, per-instance settings — so no envOverride or
// contextOverride exists for this kind.
type ContextSpec struct {
	Description *string       `yaml:"description,omitempty"`
	Stickiness  *bool         `yaml:"stickiness,omitempty"`
	SortOrder   *int          `yaml:"sortOrder,omitempty"`
	LegalValues *[]LegalValue `yaml:"legalValues,omitempty"`
}

// ContextFile is the parsed contents of one contexts/*.yaml file.
type ContextFile struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Metadata   Metadata    `yaml:"metadata"`
	Spec       ContextSpec `yaml:"spec"`

	// Path is the source file path, kept for error messages. Not part of the schema.
	Path string `yaml:"-"`
}

// LoadContexts reads every *.yaml/*.yml file directly under dir and parses
// it as a state.ContextFile. It does not recurse into subdirectories.
func LoadContexts(dir string) ([]*ContextFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading contexts dir %s: %w", dir, err)
	}

	var files []*ContextFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		f, err := loadContextFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func loadContextFile(path string) (*ContextFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var f ContextFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	f.Path = path
	if f.Metadata.Name == "" {
		return nil, fmt.Errorf("%s: metadata.name is required", path)
	}
	return &f, nil
}
