// Package state implements the flags/*.yaml desired-state model: parsing,
// and resolving a feature's per-(environment, context) configuration per
// spec §5.1 (spec -> envOverride -> contextOverride, shallow-merged,
// arrays replaced wholesale).
package state

// Strategy is one activation strategy entry under spec/envOverride/contextOverride.
type Strategy struct {
	Name       string            `yaml:"name"`
	Parameters map[string]string `yaml:"parameters,omitempty"`
	Disabled   *bool             `yaml:"disabled,omitempty"`
}

// Metadata identifies the feature and its owning service (spec §5.1, §6.4).
type Metadata struct {
	Name    string `yaml:"name"`
	Service string `yaml:"service,omitempty"`
}

// Spec is the shared, overridable feature configuration. Pointer/nil fields
// distinguish "not set in this block" from "explicitly set to zero value" so
// resolution can tell which keys an override block actually touches.
type Spec struct {
	Type           *string     `yaml:"type,omitempty"`
	Description    *string     `yaml:"description,omitempty"`
	Enabled        *bool       `yaml:"enabled,omitempty"`
	ImpressionData *bool       `yaml:"impressionData,omitempty"`
	Strategies     *[]Strategy `yaml:"strategies,omitempty"`
}

// Link is one entry under the top-level links: section (spec §5.1) — a URL
// related to the feature, with an optional title. Not part of Spec: links
// aren't environment/context-scoped in Unleash, so they're declared once per
// feature file and untouched by envOverride/contextOverride resolution.
type Link struct {
	Title *string `yaml:"title,omitempty"`
	URL   string  `yaml:"url"`
}

// Tag is one entry under the top-level tags: section (spec §5.1) — an
// additional Unleash tag beyond the automatic metadata.service tag. Not part
// of Spec, for the same reason as Link.
type Tag struct {
	Type  string `yaml:"type"`
	Value string `yaml:"value"`
}

// File is the parsed contents of one flags/*.yaml file.
type File struct {
	APIVersion      string          `yaml:"apiVersion"`
	Kind            string          `yaml:"kind"`
	Metadata        Metadata        `yaml:"metadata"`
	Spec            Spec            `yaml:"spec"`
	Links           *[]Link         `yaml:"links,omitempty"`
	Tags            *[]Tag          `yaml:"tags,omitempty"`
	EnvOverride     map[string]Spec `yaml:"envOverride,omitempty"`
	ContextOverride map[string]Spec `yaml:"contextOverride,omitempty"`

	// Path is the source file path, kept for error messages. Not part of the schema.
	Path string `yaml:"-"`
}

// Resolve computes the effective Spec for a given (environment, context) pair:
// start from spec, shallow-merge envOverride[environment], then shallow-merge
// contextOverride[context] on top. Per spec §5.1, each key found in an override
// replaces the corresponding key wholesale (strategies included) rather than
// merging element-wise.
func (f *File) Resolve(environment, context string) Spec {
	result := f.Spec
	if envSpec, ok := f.EnvOverride[environment]; ok {
		result = mergeSpec(result, envSpec)
	}
	if ctxSpec, ok := f.ContextOverride[context]; ok {
		result = mergeSpec(result, ctxSpec)
	}
	return result
}

func mergeSpec(base, override Spec) Spec {
	merged := base
	if override.Type != nil {
		merged.Type = override.Type
	}
	if override.Description != nil {
		merged.Description = override.Description
	}
	if override.Enabled != nil {
		merged.Enabled = override.Enabled
	}
	if override.ImpressionData != nil {
		merged.ImpressionData = override.ImpressionData
	}
	if override.Strategies != nil {
		merged.Strategies = override.Strategies
	}
	return merged
}
