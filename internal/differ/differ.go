// Package differ compares locally resolved flags/*.yaml state against live
// Unleash state and builds the outgoing import payload for apply. Shared by
// diff and apply (spec §6.3) so both commands see identical comparisons.
package differ

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

// Action is what diff/apply would do for a given feature. Archiving is
// handled separately via Result.Archive, not as an Action here, since it's
// opt-in (--archive-missing, spec §6.1) and goes through a different Admin
// API call (batch archive, not import) than create/update.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	// ActionRevive marks a local file whose feature exists remotely but is
	// archived — cmd/diff.go detects this (Diff itself has no live client to
	// check archived state, since ExportByTag never returns archived
	// features at all) and rewrites the Change in place from ActionCreate.
	ActionRevive Action = "revive"
)

// Change describes one pending create/update for a single feature.
type Change struct {
	FeatureName string
	Action      Action
	Details     []string
}

// Result is the outcome of comparing local flags/*.yaml files against a
// service-scoped remote fetch for one (environment, context).
type Result struct {
	Changes []Change
	// Informational lists remote feature names tagged with this service that
	// have no matching local file. Set only when archiveMissing is false —
	// never treated as delete candidates in that case (spec §6.1).
	Informational []string
	// Archive lists remote feature names tagged with this service that have
	// no matching local file, when archiveMissing is true (spec §6.1). Set
	// instead of Informational, never both.
	Archive []string
}

// HasChanges reports whether any create/update/archive is pending.
func (r Result) HasChanges() bool { return len(r.Changes) > 0 || len(r.Archive) > 0 }

type normStrategy struct {
	Name       string
	Parameters map[string]string
	Disabled   bool
}

// Diff compares files (already filtered to one service) against remote, a
// service-scoped export fetched via client.ExportByTag for the same
// environment. context selects which contextOverride block applies.
// uiManagedEnabled, when true, skips comparing "enabled" entirely — the
// live value is authoritative and never reported as pending (see
// Context.UIManagedEnabled).
func Diff(files []*state.File, remote *gen.ExportResultSchema, environment, context string, uiManagedEnabled, archiveMissing bool) Result {
	remoteFeatures := make(map[string]gen.FeatureSchema, len(remote.Features))
	for _, f := range remote.Features {
		remoteFeatures[f.Name] = f
	}

	remoteEnvs := make(map[string]gen.FeatureEnvironmentSchema)
	if remote.FeatureEnvironments != nil {
		for _, e := range *remote.FeatureEnvironments {
			// FeatureEnvironmentSchema.Name is the *feature* name on the wire,
			// despite its OpenAPI description saying "environment" — confirmed
			// against the Unleash server source (export-import-service.ts).
			remoteEnvs[e.Name] = e
		}
	}

	remoteStrategiesByFeature := make(map[string][]gen.FeatureStrategySchema)
	for _, s := range remote.FeatureStrategies {
		if s.FeatureName == nil {
			continue
		}
		remoteStrategiesByFeature[*s.FeatureName] = append(remoteStrategiesByFeature[*s.FeatureName], s)
	}

	localNames := make(map[string]bool, len(files))
	var changes []Change

	for _, file := range files {
		name := file.Metadata.Name
		localNames[name] = true
		resolved := file.Resolve(environment, context)

		rf, hasFeature := remoteFeatures[name]
		if !hasFeature {
			changes = append(changes, Change{
				FeatureName: name,
				Action:      ActionCreate,
				Details:     specDetails(resolved),
			})
			continue
		}

		// Archived features still come back from ExportByTag (confirmed
		// against a live instance — the tag-scoped export path doesn't
		// filter archived out, unlike the full-state export), so this is
		// detectable straight off the fetched data with no extra API call.
		// Skip the normal field-by-field diff entirely while archived: an
		// archived feature's type/strategies/etc. reflect whatever they were
		// at archive time, not a real drift to report.
		if rf.Archived != nil && *rf.Archived {
			changes = append(changes, Change{
				FeatureName: name,
				Action:      ActionRevive,
				Details:     specDetails(resolved),
			})
			continue
		}

		var details []string

		if resolved.Type != nil && (rf.Type == nil || *rf.Type != *resolved.Type) {
			details = append(details, fmt.Sprintf("type: %s -> %s", derefStr(rf.Type), *resolved.Type))
		}
		if resolved.Description != nil && (rf.Description == nil || *rf.Description != *resolved.Description) {
			details = append(details, fmt.Sprintf("description: %q -> %q", derefStr(rf.Description), *resolved.Description))
		}

		if !uiManagedEnabled {
			re, hasEnv := remoteEnvs[name]
			localEnabled := resolved.Enabled != nil && *resolved.Enabled
			remoteEnabled := hasEnv && re.Enabled
			if localEnabled != remoteEnabled {
				details = append(details, fmt.Sprintf("enabled: %t -> %t", remoteEnabled, localEnabled))
			}
		}

		localStrategies := normalizeLocal(resolved.Strategies)
		remoteStrategies := normalizeRemote(remoteStrategiesByFeature[name])
		if !reflect.DeepEqual(localStrategies, remoteStrategies) {
			details = append(details, fmt.Sprintf("strategies: %s -> %s",
				formatNormStrategies(remoteStrategies), formatNormStrategies(localStrategies)))
		}

		if len(details) > 0 {
			changes = append(changes, Change{FeatureName: name, Action: ActionUpdate, Details: details})
		}
	}

	var informational []string
	var archive []string
	for name, rf := range remoteFeatures {
		if localNames[name] {
			continue
		}
		if rf.Archived != nil && *rf.Archived {
			continue // already archived - nothing to do
		}
		if archiveMissing {
			archive = append(archive, name)
		} else {
			informational = append(informational, name)
		}
	}
	sort.Strings(informational)
	sort.Strings(archive)
	sort.Slice(changes, func(i, j int) bool { return changes[i].FeatureName < changes[j].FeatureName })

	return Result{Changes: changes, Informational: informational, Archive: archive}
}

// specDetails renders a fully-resolved Spec into detail lines for the
// Terraform-style full-spec dump shown on Create/Revive, as opposed to
// Update's field-level diff lines.
func specDetails(spec state.Spec) []string {
	var d []string
	if spec.Type != nil {
		d = append(d, fmt.Sprintf("type: %s", *spec.Type))
	}
	if spec.Description != nil {
		d = append(d, fmt.Sprintf("description: %q", *spec.Description))
	}
	if spec.Enabled != nil {
		d = append(d, fmt.Sprintf("enabled: %t", *spec.Enabled))
	}
	if spec.Strategies != nil && len(*spec.Strategies) > 0 {
		d = append(d, fmt.Sprintf("strategies: %s", formatStrategies(*spec.Strategies)))
	}
	return d
}

// formatStrategy renders one strategy's name, sorted parameters, and disabled
// flag as a single compact fragment, shared by both the full-spec dump
// (Create/Revive, via formatStrategies) and the before/after Update line
// (via formatNormStrategies).
func formatStrategy(name string, params map[string]string, disabled bool) string {
	part := name
	if len(params) > 0 {
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, len(keys))
		for j, k := range keys {
			pairs[j] = fmt.Sprintf("%s=%s", k, params[k])
		}
		part += "(" + strings.Join(pairs, ", ") + ")"
	}
	if disabled {
		part += " [disabled]"
	}
	return part
}

func formatStrategies(strategies []state.Strategy) string {
	if len(strategies) == 0 {
		return "(none)"
	}
	parts := make([]string, len(strategies))
	for i, s := range strategies {
		parts[i] = formatStrategy(s.Name, s.Parameters, s.Disabled != nil && *s.Disabled)
	}
	return strings.Join(parts, ", ")
}

// formatNormStrategies mirrors formatStrategies but for the already-normalized
// comparison type (normStrategy), used for Update's before/after line.
func formatNormStrategies(strategies []normStrategy) string {
	if len(strategies) == 0 {
		return "(none)"
	}
	parts := make([]string, len(strategies))
	for i, s := range strategies {
		parts[i] = formatStrategy(s.Name, s.Parameters, s.Disabled)
	}
	return strings.Join(parts, ", ")
}

func normalizeLocal(strategies *[]state.Strategy) []normStrategy {
	if strategies == nil {
		return nil
	}
	out := make([]normStrategy, len(*strategies))
	for i, s := range *strategies {
		disabled := false
		if s.Disabled != nil {
			disabled = *s.Disabled
		}
		out[i] = normStrategy{Name: s.Name, Parameters: normalizeParams(s.Parameters), Disabled: disabled}
	}
	return out
}

func normalizeRemote(strategies []gen.FeatureStrategySchema) []normStrategy {
	if strategies == nil {
		return nil
	}
	out := make([]normStrategy, len(strategies))
	for i, s := range strategies {
		var params map[string]string
		if s.Parameters != nil {
			params = map[string]string(*s.Parameters)
		}
		disabled := false
		if s.Disabled != nil {
			disabled = *s.Disabled
		}
		out[i] = normStrategy{Name: s.Name, Parameters: normalizeParams(params), Disabled: disabled}
	}
	return out
}

// normalizeParams treats a nil map and an empty map as equivalent — Unleash's
// API always returns "parameters": {} rather than omitting it, but a local
// strategy with no `parameters:` block resolves to a nil map. Without this,
// reflect.DeepEqual would treat them as different forever, making a
// no-parameter strategy (e.g. "default") never converge (spurious UPDATE on
// every diff even right after apply).
func normalizeParams(m map[string]string) map[string]string {
	if len(m) == 0 {
		return map[string]string{}
	}
	return m
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
