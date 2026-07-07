package differ

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

// ContextAction is what context-fields diff/apply would do for a given
// context field. There's no "revive" action here (context fields don't
// archive) and no separate delete Action — deletion is opt-in
// (--delete-missing) and reported via ContextResult.Delete, mirroring how
// Result.Archive works for Features.
type ContextAction string

const (
	ContextActionCreate ContextAction = "create"
	ContextActionUpdate ContextAction = "update"
)

// ContextChange describes one pending create/update for a single context field.
type ContextChange struct {
	Name    string
	Action  ContextAction
	Details []string
}

// ContextResult is the outcome of comparing local contexts/*.yaml files
// against the full remote context-field list (context fields are global —
// there's no service/environment scoping to fetch against, unlike Features).
type ContextResult struct {
	Changes []ContextChange
	// Informational lists remote context fields with no matching local file.
	// Set only when deleteMissing is false.
	Informational []string
	// Delete lists remote context fields with no matching local file, when
	// deleteMissing is true. Set instead of Informational, never both.
	Delete []string
}

// HasChanges reports whether any create/update/delete is pending.
func (r ContextResult) HasChanges() bool { return len(r.Changes) > 0 || len(r.Delete) > 0 }

type normLegalValue struct {
	Value       string
	Description string
}

// DiffContexts compares files against remote, the full list of context
// fields fetched via client.ListContextFields. deleteMissing selects
// whether remote-only fields are reported as Delete candidates or purely
// informationally (see ContextResult).
func DiffContexts(files []*state.ContextFile, remote []gen.ContextFieldSchema, deleteMissing bool) ContextResult {
	remoteFields := make(map[string]gen.ContextFieldSchema, len(remote))
	for _, f := range remote {
		remoteFields[f.Name] = f
	}

	localNames := make(map[string]bool, len(files))
	var changes []ContextChange

	for _, file := range files {
		name := file.Metadata.Name
		localNames[name] = true
		spec := file.Spec

		rf, hasField := remoteFields[name]
		if !hasField {
			changes = append(changes, ContextChange{
				Name:    name,
				Action:  ContextActionCreate,
				Details: contextSpecDetails(spec),
			})
			continue
		}

		var details []string

		if spec.Description != nil && (rf.Description == nil || *rf.Description != *spec.Description) {
			details = append(details, fmt.Sprintf("description: %q -> %q", derefStr(rf.Description), *spec.Description))
		}
		if spec.Stickiness != nil && (rf.Stickiness == nil || *rf.Stickiness != *spec.Stickiness) {
			details = append(details, fmt.Sprintf("stickiness: %t -> %t", derefBool(rf.Stickiness), *spec.Stickiness))
		}

		localValues := normalizeLocalLegalValues(spec.LegalValues)
		remoteValues := normalizeRemoteLegalValues(rf.LegalValues)
		if !reflect.DeepEqual(localValues, remoteValues) {
			details = append(details, fmt.Sprintf("legalValues: %s -> %s",
				formatNormLegalValues(remoteValues), formatNormLegalValues(localValues)))
		}

		if len(details) > 0 {
			changes = append(changes, ContextChange{Name: name, Action: ContextActionUpdate, Details: details})
		}
	}

	var informational []string
	var deleteCandidates []string
	for name := range remoteFields {
		if localNames[name] {
			continue
		}
		if deleteMissing {
			deleteCandidates = append(deleteCandidates, name)
		} else {
			informational = append(informational, name)
		}
	}
	sort.Strings(informational)
	sort.Strings(deleteCandidates)
	sort.Slice(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })

	return ContextResult{Changes: changes, Informational: informational, Delete: deleteCandidates}
}

// contextSpecDetails renders a full local spec into detail lines for the
// Terraform-style full-spec dump shown on Create, as opposed to Update's
// field-level diff lines.
func contextSpecDetails(spec state.ContextSpec) []string {
	var d []string
	if spec.Description != nil {
		d = append(d, fmt.Sprintf("description: %q", *spec.Description))
	}
	if spec.Stickiness != nil {
		d = append(d, fmt.Sprintf("stickiness: %t", *spec.Stickiness))
	}
	if spec.LegalValues != nil && len(*spec.LegalValues) > 0 {
		d = append(d, fmt.Sprintf("legalValues: %s", formatNormLegalValues(normalizeLocalLegalValues(spec.LegalValues))))
	}
	return d
}

// normalizeLocalLegalValues/normalizeRemoteLegalValues sort by value so
// legalValues: order in the YAML (or the order Unleash returns them in)
// never causes a spurious diff — only set membership matters. A nil list and
// an empty list both normalize to the same non-nil empty slice: Unleash
// always round-trips legalValues as [] rather than omitting it, while a
// local file with no legalValues: block resolves to a nil pointer. Without
// this, reflect.DeepEqual would treat nil and []normLegalValue{} as
// different forever, showing a spurious "(none) -> (none)" update on every
// diff even when both sides are genuinely empty (same class of bug
// normalizeParams in differ.go fixes for strategy parameters).
func normalizeLocalLegalValues(values *[]state.LegalValue) []normLegalValue {
	if values == nil || len(*values) == 0 {
		return []normLegalValue{}
	}
	out := make([]normLegalValue, len(*values))
	for i, v := range *values {
		out[i] = normLegalValue{Value: v.Value, Description: derefStr(v.Description)}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out
}

func normalizeRemoteLegalValues(values *[]gen.LegalValueSchema) []normLegalValue {
	if values == nil || len(*values) == 0 {
		return []normLegalValue{}
	}
	out := make([]normLegalValue, len(*values))
	for i, v := range *values {
		out[i] = normLegalValue{Value: v.Value, Description: derefStr(v.Description)}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out
}

func formatNormLegalValues(values []normLegalValue) string {
	if len(values) == 0 {
		return "(none)"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		if v.Description != "" {
			parts[i] = fmt.Sprintf("%s (%s)", v.Value, v.Description)
		} else {
			parts[i] = v.Value
		}
	}
	return strings.Join(parts, ", ")
}
