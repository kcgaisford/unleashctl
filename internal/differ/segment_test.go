package differ

import (
	"testing"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

func TestDiffSegmentsCreate(t *testing.T) {
	files := []*state.SegmentFile{
		{
			Metadata: state.Metadata{Name: "betaUsers"},
			Spec:     state.SegmentSpec{Description: strPtrT("beta users")},
		},
	}

	result := DiffSegments(files, nil, false)
	if len(result.Changes) != 1 || result.Changes[0].Action != SegmentActionCreate {
		t.Fatalf("want one create, got %+v", result.Changes)
	}
	if result.Changes[0].ID != nil {
		t.Fatalf("want nil ID on create, got %v", result.Changes[0].ID)
	}
}

func TestDiffSegmentsUpdateOnDescriptionMismatch(t *testing.T) {
	files := []*state.SegmentFile{
		{
			Metadata: state.Metadata{Name: "betaUsers"},
			Spec:     state.SegmentSpec{Description: strPtrT("new description")},
		},
	}
	remote := []gen.AdminSegmentSchema{
		{Id: 42, Name: "betaUsers", Description: strPtrT("old description")},
	}

	result := DiffSegments(files, remote, false)
	if len(result.Changes) != 1 || result.Changes[0].Action != SegmentActionUpdate {
		t.Fatalf("want one update, got %+v", result.Changes)
	}
	if result.Changes[0].ID == nil || *result.Changes[0].ID != 42 {
		t.Fatalf("want ID=42 on update, got %v", result.Changes[0].ID)
	}
}

func TestDiffSegmentsNoChangesWhenIdentical(t *testing.T) {
	files := []*state.SegmentFile{
		{
			Metadata: state.Metadata{Name: "betaUsers"},
			Spec: state.SegmentSpec{
				Description: strPtrT("desc"),
				Constraints: &[]state.Constraint{
					{ContextName: "userId", Operator: "IN", Values: &[]string{"user-1"}},
					{ContextName: "appName", Operator: "IN", Values: &[]string{"web"}},
				},
			},
		},
	}
	remote := []gen.AdminSegmentSchema{
		{
			Id:          42,
			Name:        "betaUsers",
			Description: strPtrT("desc"),
			Constraints: []gen.ConstraintSchema{
				{ContextName: "appName", Operator: "IN", Values: &[]string{"web"}},
				{ContextName: "userId", Operator: "IN", Values: &[]string{"user-1"}},
			},
		},
	}

	result := DiffSegments(files, remote, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes (constraint order shouldn't matter), got %+v", result.Changes)
	}
}

func TestDiffSegmentsInformationalForUnmatchedRemote(t *testing.T) {
	var files []*state.SegmentFile
	remote := []gen.AdminSegmentSchema{{Id: 7, Name: "retired-segment"}}

	result := DiffSegments(files, remote, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes (additive-only default), got %+v", result.Changes)
	}
	if len(result.Informational) != 1 || result.Informational[0] != "retired-segment" {
		t.Fatalf("want retired-segment reported informationally, got %v", result.Informational)
	}
	if len(result.Delete) != 0 {
		t.Fatalf("want no delete candidates without deleteMissing, got %v", result.Delete)
	}
}

func TestDiffSegmentsDeleteMissingMovesInformationalToDelete(t *testing.T) {
	var files []*state.SegmentFile
	remote := []gen.AdminSegmentSchema{{Id: 7, Name: "retired-segment"}}

	result := DiffSegments(files, remote, true)
	if len(result.Informational) != 0 {
		t.Fatalf("want no informational names when deleteMissing, got %v", result.Informational)
	}
	if len(result.Delete) != 1 || result.Delete[0].Name != "retired-segment" || result.Delete[0].ID != 7 {
		t.Fatalf("want retired-segment (id 7) as a delete candidate, got %v", result.Delete)
	}
	if !result.HasChanges() {
		t.Fatalf("want HasChanges true for a delete-only result")
	}
}

// TestDiffSegmentsNoChangesForNilVsEmptyConstraints is a regression test: a
// local file with no constraints: block resolves to a nil pointer, while
// Unleash always round-trips constraints as [] rather than omitting it.
// Without normalizing nil/empty as equal, this would show a spurious
// "(none) -> (none)" update on every diff even when both sides are
// genuinely empty (same class of bug the legalValues normalization in
// context.go fixes).
func TestDiffSegmentsNoChangesForNilVsEmptyConstraints(t *testing.T) {
	files := []*state.SegmentFile{
		{
			Metadata: state.Metadata{Name: "noConstraints"},
			Spec:     state.SegmentSpec{Description: strPtrT("matches everyone")},
		},
	}
	remote := []gen.AdminSegmentSchema{
		{Id: 1, Name: "noConstraints", Description: strPtrT("matches everyone"), Constraints: []gen.ConstraintSchema{}},
	}

	result := DiffSegments(files, remote, false)
	if len(result.Changes) != 0 {
		t.Fatalf("want no changes for nil-vs-empty constraints, got %+v", result.Changes)
	}
}
