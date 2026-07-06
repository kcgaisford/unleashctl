package differ

import (
	"testing"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

func strPtrT(s string) *string { return &s }
func boolPtrT(b bool) *bool    { return &b }

func TestDiffCreate(t *testing.T) {
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: "new-checkout", Service: "payments"},
			Spec:     state.Spec{Enabled: boolPtrT(true)},
		},
	}
	remote := &gen.ExportResultSchema{Features: []gen.FeatureSchema{}, FeatureStrategies: []gen.FeatureStrategySchema{}}

	result := Diff(files, remote, "production", "prod")
	if len(result.Changes) != 1 || result.Changes[0].Action != ActionCreate {
		t.Fatalf("want one create, got %+v", result.Changes)
	}
}

func TestDiffUpdateOnEnabledMismatch(t *testing.T) {
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: "new-checkout", Service: "payments"},
			Spec:     state.Spec{Enabled: boolPtrT(true), Type: strPtrT("release")},
		},
	}
	name := "new-checkout"
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release")}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: false},
		},
		FeatureStrategies: []gen.FeatureStrategySchema{},
	}

	result := Diff(files, remote, "production", "prod")
	if len(result.Changes) != 1 || result.Changes[0].Action != ActionUpdate {
		t.Fatalf("want one update, got %+v", result.Changes)
	}
}

func TestDiffNoChangesWhenIdentical(t *testing.T) {
	name := "new-checkout"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec: state.Spec{
				Enabled: boolPtrT(true),
				Type:    strPtrT("release"),
				Strategies: &[]state.Strategy{
					{Name: "flexibleRollout", Parameters: map[string]string{"rollout": "25"}},
				},
			},
		},
	}
	params := gen.ParametersSchema{"rollout": "25"}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release")}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: true},
		},
		FeatureStrategies: []gen.FeatureStrategySchema{
			{Name: "flexibleRollout", FeatureName: &name, Parameters: &params},
		},
	}

	result := Diff(files, remote, "production", "prod")
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes, got %+v", result.Changes)
	}
}

func TestDiffInformationalForUnmatchedRemote(t *testing.T) {
	var files []*state.File
	remote := &gen.ExportResultSchema{
		Features:          []gen.FeatureSchema{{Name: "retired-flag"}},
		FeatureStrategies: []gen.FeatureStrategySchema{},
	}

	result := Diff(files, remote, "production", "prod")
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes (additive-only default), got %+v", result.Changes)
	}
	if len(result.Informational) != 1 || result.Informational[0] != "retired-flag" {
		t.Fatalf("want retired-flag reported informationally, got %v", result.Informational)
	}
}

// TestDiffNoChangesForParameterlessStrategy is a regression test: Unleash's
// API always returns "parameters": {} for a strategy with no parameters
// (e.g. "default"), while a local flags/*.yaml with no `parameters:` block
// resolves to a nil map. Without normalizing nil/empty as equal, this would
// show a spurious UPDATE on every diff, forever.
func TestDiffNoChangesForParameterlessStrategy(t *testing.T) {
	name := "kill-switch-maintenance"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec: state.Spec{
				Enabled:    boolPtrT(false),
				Type:       strPtrT("operational"),
				Strategies: &[]state.Strategy{{Name: "default"}},
			},
		},
	}
	emptyParams := gen.ParametersSchema{}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("operational")}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: false},
		},
		FeatureStrategies: []gen.FeatureStrategySchema{
			{Name: "default", FeatureName: &name, Parameters: &emptyParams},
		},
	}

	result := Diff(files, remote, "production", "prod")
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes, got %+v", result.Changes)
	}
}
