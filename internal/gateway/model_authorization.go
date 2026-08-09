package gateway

import (
	"context"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
)

// EvaluateModelAuthorization applies the optional enterprise PEP after model
// governance resolution. An absent evaluator preserves legacy behavior; an
// unavailable configured store fails closed in the caller.
func (relay *Relay) EvaluateModelAuthorization(ctx context.Context, endpoint, requestedModel, resolvedModel string) (modelauthz.Decision, error) {
	apiKey := middleware.GetAPIKeyFromContext(ctx)
	input := modelauthz.Input{
		Endpoint: endpoint, RequestedModel: requestedModel, ResolvedModel: resolvedModel,
	}
	if apiKey != nil {
		input.OrgID = apiKey.OrgID
		input.APIKeyID = apiKey.ID
		if apiKey.AccessCredentialID != nil {
			input.AccessCredentialID = *apiKey.AccessCredentialID
		}
		if apiKey.ProjectID != nil {
			input.ProjectID = *apiKey.ProjectID
		}
		if apiKey.OwnerUserID != nil {
			input.OwnerUserID = *apiKey.OwnerUserID
		}
	}
	if claims := middleware.GetAgentCapabilityFromContext(ctx); claims != nil {
		input.AgentID = claims.AgentID
	}
	if relay == nil || relay.modelAuthorization == nil {
		return modelauthz.EvaluatePolicies(input, nil), nil
	}
	return relay.modelAuthorization.Evaluate(ctx, input)
}
