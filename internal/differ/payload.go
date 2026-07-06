package differ

import (
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
	sawService := false

	for _, file := range files {
		name := file.Metadata.Name
		resolved := file.Resolve(environment, context)

		typ := "release"
		if resolved.Type != nil {
			typ = *resolved.Type
		}
		features = append(features, gen.FeatureSchema{
			Name:        name,
			Type:        &typ,
			Description: resolved.Description,
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
			sawService = true
			tags = append(tags, gen.FeatureTagSchema{
				FeatureName: name,
				TagType:     strPtr(serviceTagType),
				TagValue:    file.Metadata.Service,
			})
		}
	}

	tagTypes := []gen.TagTypeSchema{}
	if sawService {
		tagTypes = append(tagTypes, gen.TagTypeSchema{Name: serviceTagType})
	}

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
	return payload
}

func strPtr(s string) *string { return &s }
