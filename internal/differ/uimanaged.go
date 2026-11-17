package differ

import (
	"sort"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

// UIManaged selects which fields on a context are owned by the Unleash UI
// rather than flags/*.yaml (see cmd.Context). A UI-managed field is never
// reported as a pending change and never pushed by apply; a mismatch against
// an explicit local value is reported in Result.UIDrift instead.
type UIManaged struct {
	// Enabled: the per-environment on/off switch.
	Enabled bool
	// Constraints: strategy constraints. The import path deletes and
	// recreates every strategy it is given, so "not pushing" constraints
	// means carrying the live ones over onto the matching local strategy
	// (see matchRemoteStrategies), not just omitting them.
	Constraints bool
	// LegalValues: a context field's legal values. An update request
	// replaces the whole list, so "not pushing" means re-sending the live
	// list rather than the local one.
	LegalValues bool
}

// UIDrift is one live-vs-local mismatch on a UI-managed field. Report-only.
// Name is the feature or context field it belongs to.
type UIDrift struct {
	Name  string
	Field string
	Live  string
	Local string
}

// matchRemoteStrategies pairs each local strategy with the live strategy it
// most likely corresponds to, so UI-managed constraints follow it through a
// delete-and-recreate import. Strategies carry no stable id in flags/*.yaml,
// so matching is positional per strategy name: the Nth local
// flexibleRollout pairs with the Nth live flexibleRollout (live order by
// sortOrder). A local strategy with no counterpart gets nil.
func matchRemoteStrategies(local []state.Strategy, remote []gen.FeatureStrategySchema) []*gen.FeatureStrategySchema {
	byName := make(map[string][]gen.FeatureStrategySchema)
	ordered := append([]gen.FeatureStrategySchema(nil), remote...)
	sort.SliceStable(ordered, func(i, j int) bool { return sortOrder(ordered[i]) < sortOrder(ordered[j]) })
	for _, r := range ordered {
		byName[r.Name] = append(byName[r.Name], r)
	}

	seen := make(map[string]int)
	out := make([]*gen.FeatureStrategySchema, len(local))
	for i, l := range local {
		n := seen[l.Name]
		seen[l.Name] = n + 1
		if candidates := byName[l.Name]; n < len(candidates) {
			r := candidates[n]
			out[i] = &r
		}
	}
	return out
}

func sortOrder(s gen.FeatureStrategySchema) float32 {
	if s.SortOrder == nil {
		return 0
	}
	return *s.SortOrder
}

func remoteConstraints(r *gen.FeatureStrategySchema) []gen.ConstraintSchema {
	if r == nil || r.Constraints == nil {
		return nil
	}
	return *r.Constraints
}
