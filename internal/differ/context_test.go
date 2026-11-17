package differ

import (
	"testing"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

func TestDiffContextsCreate(t *testing.T) {
	files := []*state.ContextFile{
		{
			Metadata: state.Metadata{Name: "subscriptionTier"},
			Spec:     state.ContextSpec{Stickiness: boolPtrT(true)},
		},
	}

	result := DiffContexts(files, nil, false, false)
	if len(result.Changes) != 1 || result.Changes[0].Action != ContextActionCreate {
		t.Fatalf("want one create, got %+v", result.Changes)
	}
}

func TestDiffContextsUpdateOnDescriptionMismatch(t *testing.T) {
	files := []*state.ContextFile{
		{
			Metadata: state.Metadata{Name: "subscriptionTier"},
			Spec:     state.ContextSpec{Description: strPtrT("new description")},
		},
	}
	remote := []gen.ContextFieldSchema{
		{Name: "subscriptionTier", Description: strPtrT("old description")},
	}

	result := DiffContexts(files, remote, false, false)
	if len(result.Changes) != 1 || result.Changes[0].Action != ContextActionUpdate {
		t.Fatalf("want one update, got %+v", result.Changes)
	}
}

func TestDiffContextsNoChangesWhenIdentical(t *testing.T) {
	files := []*state.ContextFile{
		{
			Metadata: state.Metadata{Name: "subscriptionTier"},
			Spec: state.ContextSpec{
				Description: strPtrT("desc"),
				Stickiness:  boolPtrT(true),
				LegalValues: &[]state.LegalValue{
					{Value: "gold"},
					{Value: "silver"},
				},
			},
		},
	}
	remote := []gen.ContextFieldSchema{
		{
			Name:        "subscriptionTier",
			Description: strPtrT("desc"),
			Stickiness:  boolPtrT(true),
			LegalValues: &[]gen.LegalValueSchema{
				{Value: "silver"},
				{Value: "gold"},
			},
		},
	}

	result := DiffContexts(files, remote, false, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes (legalValues order shouldn't matter), got %+v", result.Changes)
	}
}

func TestDiffContextsInformationalForUnmatchedRemote(t *testing.T) {
	var files []*state.ContextFile
	remote := []gen.ContextFieldSchema{{Name: "retired-field"}}

	result := DiffContexts(files, remote, false, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes (additive-only default), got %+v", result.Changes)
	}
	if len(result.Informational) != 1 || result.Informational[0] != "retired-field" {
		t.Fatalf("want retired-field reported informationally, got %v", result.Informational)
	}
	if len(result.Delete) != 0 {
		t.Fatalf("want no delete candidates without deleteMissing, got %v", result.Delete)
	}
}

func TestDiffContextsDeleteMissingMovesInformationalToDelete(t *testing.T) {
	var files []*state.ContextFile
	remote := []gen.ContextFieldSchema{{Name: "retired-field"}}

	result := DiffContexts(files, remote, false, true)
	if len(result.Informational) != 0 {
		t.Fatalf("want no informational names when deleteMissing, got %v", result.Informational)
	}
	if len(result.Delete) != 1 || result.Delete[0] != "retired-field" {
		t.Fatalf("want retired-field as a delete candidate, got %v", result.Delete)
	}
	if !result.HasChanges() {
		t.Fatalf("want HasChanges true for a delete-only result")
	}
}

// TestDiffContextsNoChangesForNilVsEmptyLegalValues is a regression test: a
// local file with no legalValues: block resolves to a nil pointer, while
// Unleash always round-trips legalValues as [] rather than omitting it.
// Without normalizing nil/empty as equal, this showed a spurious
// "(none) -> (none)" update on every diff, forever.
func TestDiffContextsNoChangesForNilVsEmptyLegalValues(t *testing.T) {
	files := []*state.ContextFile{
		{
			Metadata: state.Metadata{Name: "noLegalValues"},
			Spec:     state.ContextSpec{Stickiness: boolPtrT(true)},
		},
	}
	empty := []gen.LegalValueSchema{}
	remote := []gen.ContextFieldSchema{
		{Name: "noLegalValues", Stickiness: boolPtrT(true), LegalValues: &empty},
	}

	result := DiffContexts(files, remote, false, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes for nil-vs-empty legalValues, got %+v", result.Changes)
	}
}

func legalValuesFixture() ([]*state.ContextFile, []gen.ContextFieldSchema) {
	files := []*state.ContextFile{{Metadata: state.Metadata{Name: "tier"}}}
	remote := []gen.ContextFieldSchema{{
		Name:        "tier",
		LegalValues: &[]gen.LegalValueSchema{{Value: "gold"}, {Value: "silver"}},
	}}
	return files, remote
}

// TestDiffContextsLegalValuesReconciledByDefault: without UI management a
// file with no legalValues: would wipe live ones, so it shows as an update.
func TestDiffContextsLegalValuesReconciledByDefault(t *testing.T) {
	files, remote := legalValuesFixture()
	result := DiffContexts(files, remote, false, false)
	if len(result.Changes) != 1 {
		t.Fatalf("want one update, got %+v", result.Changes)
	}
}

func TestDiffContextsIgnoresLegalValuesWhenUIManaged(t *testing.T) {
	files, remote := legalValuesFixture()
	result := DiffContexts(files, remote, true, false)
	if len(result.Changes) != 0 || len(result.UIDrift) != 0 {
		t.Fatalf("want no changes or drift, got changes=%+v drift=%+v", result.Changes, result.UIDrift)
	}
}

func TestDiffContextsReportsLegalValueDriftWhenUIManagedAndExplicit(t *testing.T) {
	files, remote := legalValuesFixture()
	files[0].Spec.LegalValues = &[]state.LegalValue{{Value: "gold"}}
	result := DiffContexts(files, remote, true, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no pending changes, got %+v", result.Changes)
	}
	if len(result.UIDrift) != 1 || result.UIDrift[0].Field != "legalValues" {
		t.Fatalf("want one legalValues drift, got %+v", result.UIDrift)
	}
}
