package gateway

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

const publicModelExecutionProfileInclude = "execution_profile"

type publicModelExecutionProfile struct {
	ParameterControls          *modelprofile.ParameterControls `json:"parameter_controls,omitempty"`
	OutputEnforcement          *modelprofile.OutputEnforcement `json:"output_enforcement,omitempty"`
	StreamOutputAccounting     string                          `json:"stream_output_accounting,omitempty"`
	ProfileDefinitionDigest    string                          `json:"profile_definition_digest,omitempty"`
	Status                     string                          `json:"status"`
	Source                     string                          `json:"source,omitempty"`
	DisplayName                string                          `json:"display_name"`
	ProfileID                  string                          `json:"profile_id,omitempty"`
	ProfileVersion             string                          `json:"profile_version,omitempty"`
	ProtocolProfile            string                          `json:"protocol_profile,omitempty"`
	ContextWindow              int                             `json:"context_window,omitempty"`
	MaxOutputTokens            int                             `json:"max_output_tokens,omitempty"`
	RequestDefaultOutputTokens int                             `json:"request_default_output_tokens,omitempty"`
	Capabilities               []string                        `json:"capabilities,omitempty"`
	ReasoningMode              string                          `json:"reasoning_mode,omitempty"`
	ReasoningEffortValues      []string                        `json:"reasoning_effort_values,omitempty"`
}

func requestIncludesModelExecutionProfiles(includeValues []string) bool {
	for _, value := range includeValues {
		for _, item := range strings.Split(value, ",") {
			if strings.TrimSpace(item) == publicModelExecutionProfileInclude {
				return true
			}
		}
	}
	return false
}

func (relay *Relay) loadPublicModelExecutionProfiles(ctx context.Context, orgID int64, models []string) (map[string]publicModelExecutionProfile, error) {
	if len(models) == 0 {
		return map[string]publicModelExecutionProfile{}, nil
	}

	rows, err := relay.db.Query(ctx, `
WITH candidates AS (
  SELECT requested_model, ordinality
  FROM unnest($2::text[]) WITH ORDINALITY AS candidate(requested_model, ordinality)
), resolved AS (
  SELECT candidate.requested_model,
         candidate.ordinality,
         COALESCE(alias.target_model, candidate.requested_model) AS resolved_model
  FROM candidates candidate
  LEFT JOIN model_aliases alias
    ON alias.org_id = $1::bigint
   AND alias.alias = candidate.requested_model
   AND alias.status = 'active'
)
SELECT resolved.requested_model,
       COALESCE(NULLIF(BTRIM(registry.display_name), ''), resolved.requested_model),
       registry.id IS NOT NULL,
       COALESCE(CASE WHEN slot.stable_release_id IS NOT NULL THEN stable.public_context_limit ELSE registry.context_length END, 0),
       COALESCE(CASE WHEN slot.stable_release_id IS NOT NULL THEN stable.public_output_limit ELSE registry.output_limit END, 0),
       COALESCE(CASE WHEN slot.stable_release_id IS NOT NULL THEN stable.execution_profile_id ELSE registry.execution_profile_id END, ''),
       COALESCE(CASE WHEN slot.stable_release_id IS NOT NULL THEN stable.execution_profile_version ELSE registry.execution_profile_version END, ''),
       CASE WHEN slot.stable_release_id IS NOT NULL THEN 'stable_release' ELSE 'registry' END,
       COALESCE(slot.stable_release_id IS NULL OR (
         slot.public_model_id = registry.id AND stable.public_model_id = registry.id
         AND stable.model_name = registry.model_name
         AND stable.candidate_version = 'unirouter.runtime-change-candidate.v2'
         AND stable.stage = 'active' AND stable.route_items_sealed_at IS NOT NULL
         AND stable.route_items_hash <> '' AND stable.route_items_count > 0
       ), false),
       COALESCE(to_jsonb(stable)->>'profile_definition_digest','')
FROM resolved
LEFT JOIN model_registry registry
  ON registry.org_id = $1::bigint
 AND registry.model_name = resolved.resolved_model
LEFT JOIN runtime_change_slots slot ON slot.model_name = registry.model_name
LEFT JOIN runtime_change_releases stable ON stable.id = slot.stable_release_id
ORDER BY resolved.ordinality`, orgID, models)
	if err != nil {
		return nil, fmt.Errorf("query public model execution profiles: %w", err)
	}
	defer rows.Close()

	type catalogRow struct {
		modelID, displayName, profileID, profileVersion string
		registered                                      bool
		contextWindow, outputLimit                      int
		source                                          string
		sourceValid                                     bool
		definitionDigest                                string
	}
	var candidates []catalogRow
	for rows.Next() {
		var row catalogRow
		if err := rows.Scan(&row.modelID, &row.displayName, &row.registered, &row.contextWindow, &row.outputLimit, &row.profileID, &row.profileVersion, &row.source, &row.sourceValid, &row.definitionDigest); err != nil {
			return nil, fmt.Errorf("scan public model execution profile: %w", err)
		}
		candidates = append(candidates, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public model execution profiles: %w", err)
	}
	// Return the connection before resolving immutable definitions. Even a
	// one-connection pool must be able to serve catalog discovery.
	rows.Close()
	result := make(map[string]publicModelExecutionProfile, len(models))
	for _, row := range candidates {
		modelID, displayName, profileID, profileVersion := row.modelID, row.displayName, row.profileID, row.profileVersion
		registered, contextWindow, outputLimit := row.registered, row.contextWindow, row.outputLimit
		source, sourceValid, definitionDigest := row.source, row.sourceValid, row.definitionDigest

		metadata := publicModelExecutionProfile{
			ProfileDefinitionDigest: definitionDigest,
			Status:                  "unregistered",
			Source:                  source,
			DisplayName:             displayName,
			ProfileID:               strings.TrimSpace(profileID),
			ProfileVersion:          strings.TrimSpace(profileVersion),
		}
		if !registered {
			result[modelID] = metadata
			continue
		}
		if !sourceValid {
			metadata.Status = "invalid"
			result[modelID] = metadata
			continue
		}

		profile, err := modelprofile.ResolvePinned(ctx, relay.db, metadata.ProfileID, metadata.ProfileVersion, definitionDigest)
		if err != nil || modelprofile.ValidateEffectiveLimits(profile, contextWindow, outputLimit) != nil {
			metadata.Status = "invalid"
			result[modelID] = metadata
			continue
		}

		metadata.ProtocolProfile = profile.ProtocolProfile
		if profile.ID == modelprofile.UnverifiedProfileID {
			metadata.Status = "unverified"
			result[modelID] = metadata
			continue
		}

		metadata.Status = "verified"
		metadata.ContextWindow = contextWindow
		metadata.MaxOutputTokens = outputLimit
		if source == "stable_release" {
			metadata.RequestDefaultOutputTokens = modelprofile.DefaultRequestOutputLimit(profile, outputLimit)
		}
		metadata.Capabilities = slices.Clone(profile.Capabilities)
		controls := modelprofile.DescribeParameterControls(profile)
		metadata.ParameterControls = &controls
		if profile.APISurface == "openai_chat_completions" {
			metadata.StreamOutputAccounting = modelprofile.OutputEnforcementByteUpperBound
			if enforcement, ok := modelprofile.ResolveOutputEnforcement(profile.ProtocolProfile); ok && enforcement.Accounting != modelprofile.OutputEnforcementByteUpperBound {
				metadata.OutputEnforcement = &enforcement
				metadata.StreamOutputAccounting = enforcement.Accounting
			}
		}
		if reasoning, ok := modelprofile.ResolveReasoningContract(profile.ReasoningProfile); ok {
			metadata.ReasoningMode = reasoning.Mode
			metadata.ReasoningEffortValues = slices.Clone(reasoning.EffortControl.AllowedValues)
		}
		result[modelID] = metadata
	}
	return result, nil
}
