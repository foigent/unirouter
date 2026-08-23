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
	Status                string   `json:"status"`
	DisplayName           string   `json:"display_name"`
	ProfileID             string   `json:"profile_id,omitempty"`
	ProfileVersion        string   `json:"profile_version,omitempty"`
	ProtocolProfile       string   `json:"protocol_profile,omitempty"`
	ContextWindow         int      `json:"context_window,omitempty"`
	MaxOutputTokens       int      `json:"max_output_tokens,omitempty"`
	Capabilities          []string `json:"capabilities,omitempty"`
	ReasoningMode         string   `json:"reasoning_mode,omitempty"`
	ReasoningEffortValues []string `json:"reasoning_effort_values,omitempty"`
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
       COALESCE(registry.context_length, 0),
       COALESCE(registry.output_limit, 0),
       COALESCE(registry.execution_profile_id, ''),
       COALESCE(registry.execution_profile_version, '')
FROM resolved
LEFT JOIN model_registry registry
  ON registry.org_id = $1::bigint
 AND registry.model_name = resolved.resolved_model
ORDER BY resolved.ordinality`, orgID, models)
	if err != nil {
		return nil, fmt.Errorf("query public model execution profiles: %w", err)
	}
	defer rows.Close()

	result := make(map[string]publicModelExecutionProfile, len(models))
	for rows.Next() {
		var (
			modelID, displayName, profileID, profileVersion string
			registered                                      bool
			contextWindow, outputLimit                      int
		)
		if err := rows.Scan(&modelID, &displayName, &registered, &contextWindow, &outputLimit, &profileID, &profileVersion); err != nil {
			return nil, fmt.Errorf("scan public model execution profile: %w", err)
		}

		metadata := publicModelExecutionProfile{
			Status:         "unregistered",
			DisplayName:    displayName,
			ProfileID:      strings.TrimSpace(profileID),
			ProfileVersion: strings.TrimSpace(profileVersion),
		}
		if !registered {
			result[modelID] = metadata
			continue
		}

		profile, ok := modelprofile.Lookup(metadata.ProfileID, metadata.ProfileVersion)
		if !ok || modelprofile.ValidateEffectiveLimits(profile, contextWindow, outputLimit) != nil {
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
		metadata.Capabilities = slices.Clone(profile.Capabilities)
		if reasoning, ok := modelprofile.ResolveReasoningContract(profile.ReasoningProfile); ok {
			metadata.ReasoningMode = reasoning.Mode
			metadata.ReasoningEffortValues = slices.Clone(reasoning.EffortControl.AllowedValues)
		}
		result[modelID] = metadata
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public model execution profiles: %w", err)
	}
	return result, nil
}
