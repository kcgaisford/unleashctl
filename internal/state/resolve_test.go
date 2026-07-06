package state

import "testing"

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func TestResolve(t *testing.T) {
	f := &File{
		Spec: Spec{
			Type:        strPtr("release"),
			Description: strPtr("New checkout flow"),
			Enabled:     boolPtr(true),
			Strategies: &[]Strategy{
				{Name: "flexibleRollout", Parameters: map[string]string{"rollout": "25"}},
			},
		},
		EnvOverride: map[string]Spec{
			"development": {
				Strategies: &[]Strategy{{Name: "default"}},
			},
		},
		ContextOverride: map[string]Spec{
			"prod": {
				Enabled: boolPtr(false),
			},
		},
	}

	t.Run("no override for this env/context falls back to spec", func(t *testing.T) {
		got := f.Resolve("production", "stage-prod-clone")
		if got.Type == nil || *got.Type != "release" {
			t.Fatalf("Type = %v, want release", got.Type)
		}
		if got.Enabled == nil || *got.Enabled != true {
			t.Fatalf("Enabled = %v, want true", got.Enabled)
		}
		if len(*got.Strategies) != 1 || (*got.Strategies)[0].Name != "flexibleRollout" {
			t.Fatalf("Strategies = %v, want [flexibleRollout]", got.Strategies)
		}
	})

	t.Run("envOverride replaces strategies wholesale, other fields untouched", func(t *testing.T) {
		got := f.Resolve("development", "dev")
		if len(*got.Strategies) != 1 || (*got.Strategies)[0].Name != "default" {
			t.Fatalf("Strategies = %v, want [default]", got.Strategies)
		}
		if got.Enabled == nil || *got.Enabled != true {
			t.Fatalf("Enabled = %v, want true (unaffected by envOverride)", got.Enabled)
		}
	})

	t.Run("contextOverride wins over envOverride on overlapping field", func(t *testing.T) {
		got := f.Resolve("production", "prod")
		if got.Enabled == nil || *got.Enabled != false {
			t.Fatalf("Enabled = %v, want false (contextOverride)", got.Enabled)
		}
		// strategies untouched by contextOverride in this fixture, spec's value should remain
		if len(*got.Strategies) != 1 || (*got.Strategies)[0].Name != "flexibleRollout" {
			t.Fatalf("Strategies = %v, want [flexibleRollout] (from spec, no override)", got.Strategies)
		}
	})
}
