package differ

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

// SegmentAction is what segments diff/apply would do for a given segment.
// There's no delete Action — deletion is opt-in (--delete-missing) and
// reported via SegmentResult.Delete, mirroring ContextAction.
type SegmentAction string

const (
	SegmentActionCreate SegmentAction = "create"
	SegmentActionUpdate SegmentAction = "update"
)

// SegmentChange describes one pending create/update for a single segment.
// ID is nil for a create (the segment doesn't exist remotely yet) and set
// to the remote segment's id for an update — segments are id-keyed
// (PUT/DELETE /api/admin/segments/{id}), unlike ContextField's name-keyed
// endpoints, so apply needs the id to act on the change.
type SegmentChange struct {
	Name    string
	ID      *int
	Action  SegmentAction
	Details []string
}

// SegmentDeleteCandidate pairs a remote-only segment's name with the id
// DeleteSegment needs.
type SegmentDeleteCandidate struct {
	Name string
	ID   int
}

// SegmentResult is the outcome of comparing local segments/*.yaml files
// against the full remote segment list (segments are global — there's no
// service/environment scoping to fetch against, unlike Features).
type SegmentResult struct {
	Changes []SegmentChange
	// Informational lists remote segments with no matching local file. Set
	// only when deleteMissing is false.
	Informational []string
	// Delete lists remote segments with no matching local file, when
	// deleteMissing is true. Set instead of Informational, never both.
	Delete []SegmentDeleteCandidate
}

// HasChanges reports whether any create/update/delete is pending.
func (r SegmentResult) HasChanges() bool { return len(r.Changes) > 0 || len(r.Delete) > 0 }

type normConstraint struct {
	ContextName     string
	Operator        string
	CaseInsensitive bool
	Inverted        bool
	Value           string
	Values          string
}

// DiffSegments compares files against remote, the full list of segments
// fetched via client.ListSegments. deleteMissing selects whether
// remote-only segments are reported as Delete candidates or purely
// informationally (see SegmentResult). Matching is by name, same as
// ContextField, even though the underlying API is id-keyed.
func DiffSegments(files []*state.SegmentFile, remote []gen.AdminSegmentSchema, deleteMissing bool) SegmentResult {
	remoteSegments := make(map[string]gen.AdminSegmentSchema, len(remote))
	for _, s := range remote {
		remoteSegments[s.Name] = s
	}

	localNames := make(map[string]bool, len(files))
	var changes []SegmentChange

	for _, file := range files {
		name := file.Metadata.Name
		localNames[name] = true
		spec := file.Spec

		rs, hasSegment := remoteSegments[name]
		if !hasSegment {
			changes = append(changes, SegmentChange{
				Name:    name,
				Action:  SegmentActionCreate,
				Details: segmentSpecDetails(spec),
			})
			continue
		}

		var details []string

		if spec.Description != nil && (rs.Description == nil || *rs.Description != *spec.Description) {
			details = append(details, fmt.Sprintf("description: %q -> %q", derefStr(rs.Description), *spec.Description))
		}
		if spec.Project != nil && (rs.Project == nil || *rs.Project != *spec.Project) {
			details = append(details, fmt.Sprintf("project: %q -> %q", derefStr(rs.Project), *spec.Project))
		}

		localConstraints := normalizeLocalConstraints(spec.Constraints)
		remoteConstraints := normalizeRemoteConstraints(rs.Constraints)
		if !reflect.DeepEqual(localConstraints, remoteConstraints) {
			details = append(details, fmt.Sprintf("constraints: %s -> %s",
				formatNormConstraints(remoteConstraints), formatNormConstraints(localConstraints)))
		}

		if len(details) > 0 {
			id := rs.Id
			changes = append(changes, SegmentChange{Name: name, ID: &id, Action: SegmentActionUpdate, Details: details})
		}
	}

	var informational []string
	var deleteCandidates []SegmentDeleteCandidate
	for name, rs := range remoteSegments {
		if localNames[name] {
			continue
		}
		if deleteMissing {
			deleteCandidates = append(deleteCandidates, SegmentDeleteCandidate{Name: name, ID: rs.Id})
		} else {
			informational = append(informational, name)
		}
	}
	sort.Strings(informational)
	sort.Slice(deleteCandidates, func(i, j int) bool { return deleteCandidates[i].Name < deleteCandidates[j].Name })
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })

	return SegmentResult{Changes: changes, Informational: informational, Delete: deleteCandidates}
}

// segmentSpecDetails renders a full local spec into detail lines for the
// Terraform-style full-spec dump shown on Create, as opposed to Update's
// field-level diff lines.
func segmentSpecDetails(spec state.SegmentSpec) []string {
	var d []string
	if spec.Description != nil {
		d = append(d, fmt.Sprintf("description: %q", *spec.Description))
	}
	if spec.Project != nil {
		d = append(d, fmt.Sprintf("project: %q", *spec.Project))
	}
	if spec.Constraints != nil && len(*spec.Constraints) > 0 {
		d = append(d, fmt.Sprintf("constraints: %s", formatNormConstraints(normalizeLocalConstraints(spec.Constraints))))
	}
	return d
}

// normalizeLocalConstraints/normalizeRemoteConstraints sort by a composite
// key so constraints: order in the YAML (or the order Unleash returns them
// in) never causes a spurious diff — constraints are logically AND-ed, so
// only set membership matters. A nil list and an empty list both normalize
// to the same non-nil empty slice, same nil-vs-empty fix as
// normalizeLocalLegalValues/normalizeRemoteLegalValues in context.go.
func normalizeLocalConstraints(constraints *[]state.Constraint) []normConstraint {
	if constraints == nil || len(*constraints) == 0 {
		return []normConstraint{}
	}
	out := make([]normConstraint, len(*constraints))
	for i, c := range *constraints {
		out[i] = normConstraint{
			ContextName:     c.ContextName,
			Operator:        c.Operator,
			CaseInsensitive: derefBool(c.CaseInsensitive),
			Inverted:        derefBool(c.Inverted),
			Value:           derefStr(c.Value),
			Values:          strings.Join(derefStrSlice(c.Values), ","),
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].constraintKey() < out[j].constraintKey() })
	return out
}

func normalizeRemoteConstraints(constraints []gen.ConstraintSchema) []normConstraint {
	if len(constraints) == 0 {
		return []normConstraint{}
	}
	out := make([]normConstraint, len(constraints))
	for i, c := range constraints {
		out[i] = normConstraint{
			ContextName:     c.ContextName,
			Operator:        string(c.Operator),
			CaseInsensitive: derefBool(c.CaseInsensitive),
			Inverted:        derefBool(c.Inverted),
			Value:           derefStr(c.Value),
			Values:          strings.Join(derefStrSlice(c.Values), ","),
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].constraintKey() < out[j].constraintKey() })
	return out
}

func (n normConstraint) constraintKey() string {
	return strings.Join([]string{n.ContextName, n.Operator, n.Value, n.Values}, "\x00")
}

func formatNormConstraints(constraints []normConstraint) string {
	if len(constraints) == 0 {
		return "(none)"
	}
	parts := make([]string, len(constraints))
	for i, c := range constraints {
		v := c.Value
		if c.Values != "" {
			v = c.Values
		}
		parts[i] = fmt.Sprintf("%s %s %s", c.ContextName, c.Operator, v)
	}
	return strings.Join(parts, ", ")
}

// derefStrSlice returns the dereferenced slice, or nil for a nil pointer —
// shared by both the state.Constraint and gen.ConstraintSchema shapes,
// which both represent Values as *[]string.
func derefStrSlice(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}
