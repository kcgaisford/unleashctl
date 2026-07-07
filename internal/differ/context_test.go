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

	result := DiffContexts(files, nil, false)
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

	result := DiffContexts(files, remote, false)
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
				SortOrder:   intPtrT(10),
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
			SortOrder:   intPtrT(10),
			LegalValues: &[]gen.LegalValueSchema{
				{Value: "silver"},
				{Value: "gold"},
			},
		},
	}

	result := DiffContexts(files, remote, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes (legalValues order shouldn't matter), got %+v", result.Changes)
	}
}

func TestDiffContextsInformationalForUnmatchedRemote(t *testing.T) {
	var files []*state.ContextFile
	remote := []gen.ContextFieldSchema{{Name: "retired-field"}}

	result := DiffContexts(files, remote, false)
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

	result := DiffContexts(files, remote, true)
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

func intPtrT(i int) *int { return &i }
