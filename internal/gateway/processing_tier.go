package gateway

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

type processingTierEvidenceContextKey struct{}

type processingTierExecutionEvidence struct {
	Policy    string
	Requested string
	Effective string
	Explicit  bool
}

func configureProviderWriteDeadline(
	w http.ResponseWriter,
	evidence modelExecutionProfileEvidence,
	providerTimeout time.Duration,
) error {
	if strings.TrimSpace(evidence.ProcessingTierPolicy) == "" && evidence.ReasoningTimeoutSeconds <= 0 {
		return nil
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(providerTimeout + time.Minute)); err != nil {
		return fmt.Errorf("extend typed provider response write deadline: %w", err)
	}
	return nil
}

func contextWithProcessingTierEvidence(ctx context.Context, evidence modelExecutionProfileEvidence) context.Context {
	if strings.TrimSpace(evidence.ProcessingTierPolicy) == "" {
		return ctx
	}
	return context.WithValue(ctx, processingTierEvidenceContextKey{}, processingTierExecutionEvidence{
		Policy: evidence.ProcessingTierPolicy, Requested: evidence.ProcessingTierRequest,
		Effective: evidence.ProcessingTierEffective, Explicit: evidence.ProcessingTierExplicit,
	})
}

// processingTierResponseMetadata records only typed provider evidence. Missing
// or unknown headers are explicit evidence gaps; UniRouter never infers a
// delivered tier from the request or HTTP status.
func processingTierResponseMetadata(ctx context.Context, header http.Header) map[string]interface{} {
	return processingTierResponseMetadataForOutcome(ctx, header, true)
}

func processingTierResponseMetadataForOutcome(ctx context.Context, header http.Header, accepted bool) map[string]interface{} {
	evidence, ok := ctx.Value(processingTierEvidenceContextKey{}).(processingTierExecutionEvidence)
	if !ok {
		return map[string]interface{}{}
	}
	metadata := map[string]interface{}{
		"processing_tier_policy":    evidence.Policy,
		"processing_tier_request":   evidence.Requested,
		"processing_tier_effective": evidence.Effective,
		"processing_tier_explicit":  evidence.Explicit,
	}
	policy, modeled := modelprofile.ResolveProcessingTierPolicy(evidence.Policy)
	if !modeled {
		metadata["processing_tier_observation"] = "policy_unavailable"
		return metadata
	}
	if strings.TrimSpace(policy.ObservedResponseHeader) == "" {
		metadata["processing_tier_observation"] = "unavailable_by_surface"
		return metadata
	}
	metadata["processing_tier_observation_source"] = "response_header:" + strings.ToLower(policy.ObservedResponseHeader)
	rawValues := header.Values(policy.ObservedResponseHeader)
	observedValues := make(map[string]struct{})
	for _, rawValue := range rawValues {
		for _, candidate := range strings.Split(rawValue, ",") {
			candidate = strings.ToLower(strings.TrimSpace(candidate))
			if candidate != "" {
				observedValues[candidate] = struct{}{}
			}
		}
	}
	if len(observedValues) == 0 {
		metadata["processing_tier_observation"] = "missing"
		return metadata
	}
	if len(observedValues) > 1 {
		metadata["processing_tier_observation"] = "conflicting"
		return metadata
	}
	observed := ""
	for candidate := range observedValues {
		observed = candidate
	}
	if !slices.Contains(policy.ObservedNormalizedValues, observed) {
		metadata["processing_tier_observation"] = "unknown"
		return metadata
	}
	metadata["processing_tier_observed"] = observed
	if !accepted {
		metadata["processing_tier_observation"] = "observed_on_rejected_response"
		return metadata
	}
	metadata["processing_tier_observation"] = "observed"
	switch {
	case observed == evidence.Effective:
		metadata["processing_tier_disposition"] = "confirmed"
	case evidence.Effective == modelprofile.ProcessingTierPriority && observed == modelprofile.ProcessingTierStandard:
		metadata["processing_tier_disposition"] = "provider_downgraded"
	default:
		metadata["processing_tier_disposition"] = "provider_reported_mismatch"
	}
	return metadata
}

func withProcessingTierResponseMetadata(
	ctx context.Context,
	header http.Header,
	metadata map[string]interface{},
) map[string]interface{} {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	for key, value := range processingTierResponseMetadataForOutcome(ctx, header, false) {
		metadata[key] = value
	}
	return metadata
}
