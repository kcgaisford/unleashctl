// Package ownership implements the service-tag scoping and conflict
// detection from spec §6.4, for repos sharing a single OSS instance.
package ownership

import (
	"context"
	"errors"
	"fmt"

	"github.com/kcgaisford/unleashctl/internal/client"
	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

// Conflict is a local file whose feature name collides with a remote feature
// that's tagged with a different service, or not tagged at all.
type Conflict struct {
	FeatureName   string
	Untagged      bool
	RemoteService string // set only when Untagged is false
}

func (c Conflict) String() string {
	if c.Untagged {
		return fmt.Sprintf("%s: exists remotely but is untagged (not yet CLI-managed)", c.FeatureName)
	}
	return fmt.Sprintf("%s: exists remotely tagged service:%s, not this service", c.FeatureName, c.RemoteService)
}

// Check verifies that no local file's metadata.name collides with a remote
// feature owned by a different (or no) service. Only local files not already
// present in scopedRemote need a lookup — scopedRemote came from
// client.ExportByTag("service:<service>"), so anything in it is already
// confirmed correctly tagged. Everything else might be a legitimate create,
// or might be an existing feature owned elsewhere; that's the case this
// checks for. Callers should refuse to apply while conflicts is non-empty —
// spec §6.4 requires this to be a hard refusal, not a silent overwrite.
func Check(ctx context.Context, c *client.Client, service string, files []*state.File, scopedRemote *gen.ExportResultSchema) ([]Conflict, error) {
	inScope := make(map[string]bool, len(scopedRemote.Features))
	for _, f := range scopedRemote.Features {
		inScope[f.Name] = true
	}

	var conflicts []Conflict
	for _, file := range files {
		name := file.Metadata.Name
		if inScope[name] {
			continue
		}

		tags, err := c.GetFeatureTags(ctx, name)
		if err != nil {
			var apiErr *client.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
				continue // doesn't exist remotely yet: legitimate create, no conflict
			}
			return nil, fmt.Errorf("checking ownership for %s: %w", name, err)
		}

		svc, tagged := serviceTag(tags)
		switch {
		case !tagged:
			conflicts = append(conflicts, Conflict{FeatureName: name, Untagged: true})
		case svc != service:
			conflicts = append(conflicts, Conflict{FeatureName: name, RemoteService: svc})
		}
	}
	return conflicts, nil
}

func serviceTag(tags []gen.TagSchema) (string, bool) {
	for _, t := range tags {
		if t.Type == "service" {
			return t.Value, true
		}
	}
	return "", false
}
