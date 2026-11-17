package differ

import (
	"reflect"
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

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
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

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
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

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
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

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes (additive-only default), got %+v", result.Changes)
	}
	if len(result.Informational) != 1 || result.Informational[0] != "retired-flag" {
		t.Fatalf("want retired-flag reported informationally, got %v", result.Informational)
	}
}

// TestDiffArchiveMissingMovesInformationalToArchive verifies that
// archiveMissing=true routes remote-only names into Archive instead of
// Informational, and HasChanges reflects the pending archive.
func TestDiffArchiveMissingMovesInformationalToArchive(t *testing.T) {
	var files []*state.File
	remote := &gen.ExportResultSchema{
		Features:          []gen.FeatureSchema{{Name: "retired-flag"}},
		FeatureStrategies: []gen.FeatureStrategySchema{},
	}

	result := Diff(files, remote, "production", "prod", UIManaged{}, true)
	if len(result.Informational) != 0 {
		t.Fatalf("want no informational names when archiveMissing, got %v", result.Informational)
	}
	if len(result.Archive) != 1 || result.Archive[0] != "retired-flag" {
		t.Fatalf("want retired-flag as an archive candidate, got %v", result.Archive)
	}
	if !result.HasChanges() {
		t.Fatalf("want HasChanges true for an archive-only result")
	}
}

// TestDiffArchivedRemoteFeatureProposesRevive verifies that a local file
// whose remote feature is archived (still returned by ExportByTag, per a
// live-instance check — the tag-scoped export doesn't filter archived out)
// produces a single ActionRevive change instead of a spurious update diff.
func TestDiffArchivedRemoteFeatureProposesRevive(t *testing.T) {
	name := "email-verification"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "other-repo"},
			Spec:     state.Spec{Enabled: boolPtrT(true), Type: strPtrT("release")},
		},
	}
	remote := &gen.ExportResultSchema{
		Features:          []gen.FeatureSchema{{Name: name, Type: strPtrT("release"), Archived: boolPtrT(true)}},
		FeatureStrategies: []gen.FeatureStrategySchema{},
	}

	result := Diff(files, remote, "development", "dev", UIManaged{}, false)
	if len(result.Changes) != 1 || result.Changes[0].Action != ActionRevive {
		t.Fatalf("want one revive, got %+v", result.Changes)
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

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes, got %+v", result.Changes)
	}
}

// TestDiffSkipsEnabledWhenUIManaged verifies the ui_managed_enabled behavior:
// a local/remote "enabled" mismatch produces no change when uiManagedEnabled
// is true, even though every other field matches.
func TestDiffSkipsEnabledWhenUIManaged(t *testing.T) {
	name := "new-checkout"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec:     state.Spec{Enabled: boolPtrT(true), Type: strPtrT("release")},
		},
	}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release")}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: false}, // opposite of local
		},
		FeatureStrategies: []gen.FeatureStrategySchema{},
	}

	result := Diff(files, remote, "production", "prod", UIManaged{Enabled: true}, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes with uiManagedEnabled, got %+v", result.Changes)
	}
	if result.HasChanges() {
		t.Fatalf("enabled drift must not count as a pending change")
	}
	want := []UIDrift{{Name: name, Field: "enabled", Live: "false", Local: "true"}}
	if !reflect.DeepEqual(result.UIDrift, want) {
		t.Fatalf("want UIDrift %+v, got %+v", want, result.UIDrift)
	}
}

// TestDiffNoEnabledDriftWhenLocalUnset verifies a file with no enabled: on a
// UI-managed context defers fully to the UI — no drift reported.
func TestDiffNoEnabledDriftWhenLocalUnset(t *testing.T) {
	name := "new-checkout"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec:     state.Spec{Type: strPtrT("release")},
		},
	}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release")}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: true},
		},
		FeatureStrategies: []gen.FeatureStrategySchema{},
	}

	result := Diff(files, remote, "production", "prod", UIManaged{Enabled: true}, false)
	if len(result.Changes) != 0 || len(result.UIDrift) != 0 {
		t.Fatalf("want no changes or drift, got changes=%+v drift=%+v", result.Changes, result.UIDrift)
	}
}

// TestDiffUpdateOnImpressionDataMismatch verifies impressionData is diffed
// field-by-field like type/description: a local/remote mismatch produces an
// update.
func TestDiffUpdateOnImpressionDataMismatch(t *testing.T) {
	name := "new-checkout"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec:     state.Spec{Enabled: boolPtrT(true), Type: strPtrT("release"), ImpressionData: boolPtrT(true)},
		},
	}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release"), ImpressionData: boolPtrT(false)}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: true},
		},
		FeatureStrategies: []gen.FeatureStrategySchema{},
	}

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
	if len(result.Changes) != 1 || result.Changes[0].Action != ActionUpdate {
		t.Fatalf("want one update, got %+v", result.Changes)
	}
}

// TestDiffTagsAddedProducesUpdate verifies a local tags: entry with no
// matching remote FeatureTags entry produces an update, and that the
// auto-generated `service` tag isn't double-counted as a phantom local tag.
func TestDiffTagsAddedProducesUpdate(t *testing.T) {
	name := "new-checkout"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec:     state.Spec{Enabled: boolPtrT(true), Type: strPtrT("release")},
			Tags:     &[]state.Tag{{Type: "team", Value: "checkout"}},
		},
	}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release")}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: true},
		},
		FeatureStrategies: []gen.FeatureStrategySchema{},
		FeatureTags: &[]gen.FeatureTagSchema{
			{FeatureName: name, TagType: strPtrT("service"), TagValue: "payments"},
		},
	}

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
	if len(result.Changes) != 1 || result.Changes[0].Action != ActionUpdate {
		t.Fatalf("want one update for added tag, got %+v", result.Changes)
	}
}

// TestDiffTagsNoChangeWhenReordered verifies tag order doesn't cause a
// spurious diff.
func TestDiffTagsNoChangeWhenReordered(t *testing.T) {
	name := "new-checkout"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec:     state.Spec{Enabled: boolPtrT(true), Type: strPtrT("release")},
			Tags: &[]state.Tag{
				{Type: "team", Value: "checkout"},
				{Type: "priority", Value: "high"},
			},
		},
	}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release")}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: true},
		},
		FeatureStrategies: []gen.FeatureStrategySchema{},
		FeatureTags: &[]gen.FeatureTagSchema{
			{FeatureName: name, TagType: strPtrT("service"), TagValue: "payments"},
			{FeatureName: name, TagType: strPtrT("priority"), TagValue: "high"},
			{FeatureName: name, TagType: strPtrT("team"), TagValue: "checkout"},
		},
	}

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes for reordered tags, got %+v", result.Changes)
	}
}

// TestDiffLinksAddedProducesUpdate verifies a local links: entry with no
// matching remote Links entry produces an update.
func TestDiffLinksAddedProducesUpdate(t *testing.T) {
	name := "new-checkout"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec:     state.Spec{Enabled: boolPtrT(true), Type: strPtrT("release")},
			Links:    &[]state.Link{{URL: "https://wiki.internal/new-checkout", Title: strPtrT("Design doc")}},
		},
	}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release")}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: true},
		},
		FeatureStrategies: []gen.FeatureStrategySchema{},
	}

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
	if len(result.Changes) != 1 || result.Changes[0].Action != ActionUpdate {
		t.Fatalf("want one update for added link, got %+v", result.Changes)
	}
}

// TestDiffLinksNoChangeWhenReordered verifies link order doesn't cause a
// spurious diff.
func TestDiffLinksNoChangeWhenReordered(t *testing.T) {
	name := "new-checkout"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec:     state.Spec{Enabled: boolPtrT(true), Type: strPtrT("release")},
			Links: &[]state.Link{
				{URL: "https://wiki.internal/a", Title: strPtrT("A")},
				{URL: "https://wiki.internal/b", Title: strPtrT("B")},
			},
		},
	}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release")}},
		FeatureEnvironments: &[]gen.FeatureEnvironmentSchema{
			{Name: name, FeatureName: &name, Enabled: true},
		},
		FeatureStrategies: []gen.FeatureStrategySchema{},
		Links: &[]gen.FeatureLinksSchema{
			{Feature: name, Links: []gen.FeatureLinkSchema{
				{Url: "https://wiki.internal/b", Title: strPtrT("B")},
				{Url: "https://wiki.internal/a", Title: strPtrT("A")},
			}},
		},
	}

	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes for reordered links, got %+v", result.Changes)
	}
}

func TestBuildImportPayloadIncludesImpressionDataTagsAndLinks(t *testing.T) {
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: "new-checkout", Service: "payments"},
			Spec: state.Spec{
				Enabled:        boolPtrT(true),
				Type:           strPtrT("release"),
				ImpressionData: boolPtrT(true),
			},
			Tags:  &[]state.Tag{{Type: "team", Value: "checkout"}},
			Links: &[]state.Link{{URL: "https://wiki.internal/new-checkout", Title: strPtrT("Design doc")}},
		},
	}

	payload := BuildImportPayload(files, nil, "production", "prod", UIManaged{})

	if len(payload.Features) != 1 || payload.Features[0].ImpressionData == nil || !*payload.Features[0].ImpressionData {
		t.Fatalf("want ImpressionData true on feature, got %+v", payload.Features)
	}
	if payload.FeatureTags == nil || len(*payload.FeatureTags) != 2 {
		t.Fatalf("want 2 tags (service + team), got %+v", payload.FeatureTags)
	}
	if payload.Links == nil || len(*payload.Links) != 1 || (*payload.Links)[0].Feature != "new-checkout" {
		t.Fatalf("want Links populated for new-checkout, got %+v", payload.Links)
	}
	if len((*payload.Links)[0].Links) != 1 || (*payload.Links)[0].Links[0].Url != "https://wiki.internal/new-checkout" {
		t.Fatalf("want link url set, got %+v", (*payload.Links)[0].Links)
	}
	foundTeamType := false
	for _, tt := range payload.TagTypes {
		if tt.Name == "team" {
			foundTeamType = true
		}
	}
	if !foundTeamType {
		t.Fatalf("want tagTypes to include custom type 'team', got %+v", payload.TagTypes)
	}
}

func TestBuildImportPayloadOmitsFeatureEnvironmentsWhenUIManaged(t *testing.T) {
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: "new-checkout", Service: "payments"},
			Spec: state.Spec{
				Enabled:    boolPtrT(true),
				Type:       strPtrT("release"),
				Strategies: &[]state.Strategy{{Name: "flexibleRollout", Parameters: map[string]string{"rollout": "25"}}},
			},
		},
	}

	payload := BuildImportPayload(files, nil, "production", "prod", UIManaged{Enabled: true})
	if payload.FeatureEnvironments != nil {
		t.Fatalf("want FeatureEnvironments omitted when uiManagedEnabled, got %+v", *payload.FeatureEnvironments)
	}
	if len(payload.Features) != 1 || payload.Features[0].Name != "new-checkout" {
		t.Fatalf("want Features still populated, got %+v", payload.Features)
	}
	if len(payload.FeatureStrategies) != 1 || payload.FeatureStrategies[0].Name != "flexibleRollout" {
		t.Fatalf("want FeatureStrategies still populated, got %+v", payload.FeatureStrategies)
	}
	if payload.FeatureTags == nil || len(*payload.FeatureTags) != 1 {
		t.Fatalf("want FeatureTags still populated, got %+v", payload.FeatureTags)
	}
}

// constraintFixture: one feature, one local flexibleRollout with no
// constraints: key, and a live flexibleRollout that has a UI-added
// constraint.
func constraintFixture() ([]*state.File, *gen.ExportResultSchema) {
	name := "new-checkout"
	files := []*state.File{
		{
			Metadata: state.Metadata{Name: name, Service: "payments"},
			Spec: state.Spec{
				Type:       strPtrT("release"),
				Strategies: &[]state.Strategy{{Name: "flexibleRollout", Parameters: map[string]string{"rollout": "25"}}},
			},
		},
	}
	params := gen.ParametersSchema{"rollout": "25"}
	remote := &gen.ExportResultSchema{
		Features: []gen.FeatureSchema{{Name: name, Type: strPtrT("release")}},
		FeatureStrategies: []gen.FeatureStrategySchema{{
			Name:        "flexibleRollout",
			FeatureName: &name,
			Parameters:  &params,
			Constraints: &[]gen.ConstraintSchema{{ContextName: "userId", Operator: "IN", Values: &[]string{"a", "b"}}},
		}},
	}
	return files, remote
}

// TestDiffConstraintsReconciledByDefault: without ui_managed_constraints a
// live constraint missing from flags/*.yaml is a pending change (it would be
// removed by apply), not silently ignored.
func TestDiffConstraintsReconciledByDefault(t *testing.T) {
	files, remote := constraintFixture()
	result := Diff(files, remote, "production", "prod", UIManaged{}, false)
	if len(result.Changes) != 1 || result.Changes[0].Action != ActionUpdate {
		t.Fatalf("want one update for constraint removal, got %+v", result.Changes)
	}
}

func TestDiffIgnoresConstraintsWhenUIManaged(t *testing.T) {
	files, remote := constraintFixture()
	result := Diff(files, remote, "production", "prod", UIManaged{Constraints: true}, false)
	if len(result.Changes) != 0 || len(result.UIDrift) != 0 {
		t.Fatalf("want no changes or drift, got changes=%+v drift=%+v", result.Changes, result.UIDrift)
	}
}

func TestDiffReportsConstraintDriftWhenUIManagedAndExplicit(t *testing.T) {
	files, remote := constraintFixture()
	(*files[0].Spec.Strategies)[0].Constraints = &[]state.Constraint{{ContextName: "userId", Operator: "IN", Values: &[]string{"c"}}}
	result := Diff(files, remote, "production", "prod", UIManaged{Constraints: true}, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no pending changes, got %+v", result.Changes)
	}
	if len(result.UIDrift) != 1 || result.UIDrift[0].Field != "strategies[0] flexibleRollout constraints" {
		t.Fatalf("want one constraint drift, got %+v", result.UIDrift)
	}
}

// TestBuildImportPayloadKeepsLiveConstraintsWhenUIManaged is the regression
// for UI-set constraints being wiped: import recreates strategies, so the
// payload must carry the live constraints, even when the local file declares
// different ones.
func TestBuildImportPayloadKeepsLiveConstraintsWhenUIManaged(t *testing.T) {
	files, remote := constraintFixture()
	(*files[0].Spec.Strategies)[0].Constraints = &[]state.Constraint{{ContextName: "userId", Operator: "IN", Values: &[]string{"c"}}}

	payload := BuildImportPayload(files, remote, "production", "prod", UIManaged{Constraints: true})
	got := *payload.FeatureStrategies[0].Constraints
	if len(got) != 1 || (*got[0].Values)[0] != "a" {
		t.Fatalf("want live constraint userId IN a,b carried over, got %+v", got)
	}

	payload = BuildImportPayload(files, remote, "production", "prod", UIManaged{})
	got = *payload.FeatureStrategies[0].Constraints
	if len(got) != 1 || (*got[0].Values)[0] != "c" {
		t.Fatalf("want local constraint when not UI-managed, got %+v", got)
	}
}

// TestMatchRemoteStrategiesPositionalByName: the Nth local strategy of a
// name pairs with the Nth live one (by sortOrder); extras get nil.
func TestMatchRemoteStrategiesPositionalByName(t *testing.T) {
	f := func(v float32) *float32 { return &v }
	remote := []gen.FeatureStrategySchema{
		{Name: "flexibleRollout", Id: strPtrT("second"), SortOrder: f(2)},
		{Name: "default", Id: strPtrT("d"), SortOrder: f(0)},
		{Name: "flexibleRollout", Id: strPtrT("first"), SortOrder: f(1)},
	}
	local := []state.Strategy{{Name: "flexibleRollout"}, {Name: "flexibleRollout"}, {Name: "flexibleRollout"}, {Name: "default"}}
	got := matchRemoteStrategies(local, remote)
	ids := []string{}
	for _, g := range got {
		if g == nil {
			ids = append(ids, "<nil>")
		} else {
			ids = append(ids, *g.Id)
		}
	}
	want := []string{"first", "second", "<nil>", "d"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("want %v, got %v", want, ids)
	}
}
