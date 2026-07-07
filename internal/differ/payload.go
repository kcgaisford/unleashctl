package differ

import (
	"sort"

	"github.com/kcgaisford/unleashctl/internal/client/gen"
	"github.com/kcgaisford/unleashctl/internal/state"
)

const serviceTagType = "service"

// BuildImportPayload builds the ExportResultSchema apply sends to the scoped
// validate/import endpoints. It always encodes the *complete* desired state
// for every file in files (not just the changed ones): the Unleash import
// path deletes and recreates strategies and tags for every feature named in
// the payload (confirmed against export-import-service.ts), so a partial
// payload would silently wipe strategies/tags it omits.
//
// uiManagedEnabled, when true, omits FeatureEnvironments entirely: Unleash's
// import applies "enabled" via (dto.data.featureEnvironments || []).map(...)
// (export-import-service.ts), a no-op over an absent list, while
// strategies/tags/type/description (driven by separate top-level lists)
// keep applying normally. A brand-new feature still lands disabled, since
// Unleash auto-provisions every environment at enabled:false on creation.
func BuildImportPayload(files []*state.File, environment, context string, uiManagedEnabled bool) gen.ExportResultSchema {
	features := make([]gen.FeatureSchema, 0, len(files))
	featureEnvs := make([]gen.FeatureEnvironmentSchema, 0, len(files))
	var strategies []gen.FeatureStrategySchema
	var tags []gen.FeatureTagSchema
	var links []gen.FeatureLinksSchema
	tagTypesSeen := map[string]bool{}

	for _, file := range files {
		name := file.Metadata.Name
		resolved := file.Resolve(environment, context)

		typ := "release"
		if resolved.Type != nil {
			typ = *resolved.Type
		}
		features = append(features, gen.FeatureSchema{
			Name:           name,
			Type:           &typ,
			Description:    resolved.Description,
			ImpressionData: resolved.ImpressionData,
		})

		if !uiManagedEnabled {
			enabled := resolved.Enabled != nil && *resolved.Enabled
			nameCopy, envCopy := name, environment
			featureEnvs = append(featureEnvs, gen.FeatureEnvironmentSchema{
				Name:        nameCopy,
				FeatureName: &nameCopy,
				Environment: &envCopy,
				Enabled:     enabled,
			})
		}

		if resolved.Strategies != nil {
			for _, s := range *resolved.Strategies {
				fn := name
				var params *gen.ParametersSchema
				if s.Parameters != nil {
					p := gen.ParametersSchema(s.Parameters)
					params = &p
				}
				strategies = append(strategies, gen.FeatureStrategySchema{
					Name:        s.Name,
					FeatureName: &fn,
					Parameters:  params,
					Disabled:    s.Disabled,
				})
			}
		}

		if file.Metadata.Service != "" {
			tagTypesSeen[serviceTagType] = true
			tags = append(tags, gen.FeatureTagSchema{
				FeatureName: name,
				TagType:     strPtr(serviceTagType),
				TagValue:    file.Metadata.Service,
			})
		}

		if file.Tags != nil {
			for _, t := range *file.Tags {
				tagTypesSeen[t.Type] = true
				tags = append(tags, gen.FeatureTagSchema{
					FeatureName: name,
					TagType:     strPtr(t.Type),
					TagValue:    t.Value,
				})
			}
		}

		if file.Links != nil && len(*file.Links) > 0 {
			featureLinks := make([]gen.FeatureLinkSchema, len(*file.Links))
			for i, l := range *file.Links {
				featureLinks[i] = gen.FeatureLinkSchema{Title: l.Title, Url: l.URL}
			}
			links = append(links, gen.FeatureLinksSchema{Feature: name, Links: featureLinks})
		}
	}

	tagTypes := make([]gen.TagTypeSchema, 0, len(tagTypesSeen))
	for name := range tagTypesSeen {
		tagTypes = append(tagTypes, gen.TagTypeSchema{Name: name})
	}
	sort.Slice(tagTypes, func(i, j int) bool { return tagTypes[i].Name < tagTypes[j].Name })

	payload := gen.ExportResultSchema{
		Features:          features,
		FeatureStrategies: strategies,
		TagTypes:          tagTypes,
	}
	if len(featureEnvs) > 0 {
		payload.FeatureEnvironments = &featureEnvs
	}
	if len(tags) > 0 {
		payload.FeatureTags = &tags
	}
	if len(links) > 0 {
		payload.Links = &links
	}
	return payload
}

func strPtr(s string) *string { return &s }
