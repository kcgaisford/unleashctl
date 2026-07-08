package state

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Constraint is one entry under spec/constraints — a single condition a
// segment's constraints are evaluated against, mirroring Unleash's
// constraintSchema (internal/client/gen: ConstraintSchema).
type Constraint struct {
	ContextName     string    `yaml:"contextName"`
	Operator        string    `yaml:"operator"`
	CaseInsensitive *bool     `yaml:"caseInsensitive,omitempty"`
	Inverted        *bool     `yaml:"inverted,omitempty"`
	Value           *string   `yaml:"value,omitempty"`
	Values          *[]string `yaml:"values,omitempty"`
}

// SegmentSpec is the desired configuration of a segment. Like ContextSpec,
// there's no environment/context dimension — segments are global (or
// project-scoped via Project), not per-(environment, context) like
// Feature's Spec.
type SegmentSpec struct {
	Description *string       `yaml:"description,omitempty"`
	Project     *string       `yaml:"project,omitempty"`
	Constraints *[]Constraint `yaml:"constraints,omitempty"`
}

// SegmentFile is the parsed contents of one segments/*.yaml file.
type SegmentFile struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Metadata   Metadata    `yaml:"metadata"`
	Spec       SegmentSpec `yaml:"spec"`

	// Path is the source file path, kept for error messages. Not part of the schema.
	Path string `yaml:"-"`
}

// LoadSegments reads every *.yaml/*.yml file directly under dir and parses
// it as a state.SegmentFile. It does not recurse into subdirectories.
func LoadSegments(dir string) ([]*SegmentFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading segments dir %s: %w", dir, err)
	}

	var files []*SegmentFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		f, err := loadSegmentFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func loadSegmentFile(path string) (*SegmentFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var f SegmentFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	f.Path = path
	if f.Metadata.Name == "" {
		return nil, fmt.Errorf("%s: metadata.name is required", path)
	}
	return &f, nil
}
