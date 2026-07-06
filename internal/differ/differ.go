// Package differ compares locally resolved flags/*.yaml state against live
// Unleash state and builds the outgoing import payload for apply. Shared by
// diff and apply (spec §6.3) so both commands see identical comparisons.
package differ

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

// Action is what diff/apply would do for a given feature. Phase 1 is
// additive-only (spec §6.1) — there is no ActionArchive here.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
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
	// have no matching local file. Never treated as delete candidates unless
	// --archive-missing is passed (spec §6.1) — not implemented in Phase 1.
	Informational []string
}

// HasChanges reports whether any create/update is pending.
func (r Result) HasChanges() bool { return len(r.Changes) > 0 }

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
func Diff(files []*state.File, remote *gen.ExportResultSchema, environment, context string, uiManagedEnabled bool) Result {
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
				Details:     []string{"feature does not exist remotely"},
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
			details = append(details, fmt.Sprintf("strategies: %d remote -> %d local", len(remoteStrategies), len(localStrategies)))
		}

		if len(details) > 0 {
			changes = append(changes, Change{FeatureName: name, Action: ActionUpdate, Details: details})
		}
	}

	var informational []string
	for name := range remoteFeatures {
		if !localNames[name] {
			informational = append(informational, name)
		}
	}
	sort.Strings(informational)
	sort.Slice(changes, func(i, j int) bool { return changes[i].FeatureName < changes[j].FeatureName })

	return Result{Changes: changes, Informational: informational}
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
