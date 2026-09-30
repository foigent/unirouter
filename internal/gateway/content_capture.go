package gateway

import (
	"context"
	"net/http"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

// Startup-only dependency. The capture service owns its own short transactions;
// dispatch identity and financial authority remain in the original coordinator.
func (relay *Relay) SetContentCapture(starter contentcapture.Starter) { relay.contentCapture = starter }

// RequireContentPolicyForCache is a startup-only fail-closed gate. When source
// content is not configured, no effective policy can authorize a new exact
// response cache hit or write.
func (relay *Relay) RequireContentPolicyForCache() { relay.contentPolicyRequiredForCache = true }

func (relay *Relay) observeProvider(ctx context.Context, request *http.Request, adapter adapters.Adapter, endpoint adapters.Endpoint) (*http.Request, func(*http.Response, error)) {
	protocol := ""
	if adapter != nil {
		switch adapter.Type() {
		case adapters.TypeAnthropicNative:
			if endpoint == adapters.EndpointChatCompletions {
				protocol = string(modeloperation.IDMessages)
			}
		case adapters.TypeOpenAICompatible, adapters.TypeAzureOpenAI:
			switch endpoint {
			case adapters.EndpointChatCompletions:
				protocol = string(modeloperation.IDChatCompletions)
			case adapters.EndpointEmbeddings:
				protocol = string(modeloperation.IDEmbeddings)
			}
		}
	}
	key := middleware.GetAPIKeyFromContext(ctx)
	if relay.contentCapture == nil || protocol == "" || key == nil || key.ProjectID == nil || *key.ProjectID <= 0 || attemptIDFromContext(ctx) == "" {
		return request, func(*http.Response, error) {}
	}
	principal := contentcapture.CapturePrincipal{APIKeyID: key.ID, KeyType: key.KeyType}
	if key.OwnerUserID != nil {
		principal.OwnerUserID = *key.OwnerUserID
	}
	id := contentcapture.Observation{OrgID: key.OrgID, ProjectID: *key.ProjectID, RequestID: middleware.GetRequestIDFromContext(ctx), Protocol: protocol, AttemptID: attemptIDFromContext(ctx), Principal: principal}
	return contentcapture.ObserveProvider(relay.contentCapture, id, request)
}

type publicContentObservationKey struct{}
type publicContentObservation struct {
	starter  contentcapture.Starter
	identity contentcapture.Observation
}

func withPublicContentObservation(ctx context.Context, starter contentcapture.Starter, request publicNonStreamRequest, attempt string) context.Context {
	if starter == nil || request.ProjectID <= 0 || attempt == "" {
		return ctx
	}
	key := middleware.GetAPIKeyFromContext(ctx)
	if key == nil || key.ID <= 0 {
		return ctx
	}
	principal := contentcapture.CapturePrincipal{APIKeyID: key.ID, KeyType: key.KeyType}
	if key.OwnerUserID != nil {
		principal.OwnerUserID = *key.OwnerUserID
	}
	return context.WithValue(ctx, publicContentObservationKey{}, publicContentObservation{starter, contentcapture.Observation{OrgID: request.OrgID, ProjectID: request.ProjectID, RequestID: request.RequestID, Protocol: string(request.Operation), AttemptID: attempt, Principal: principal}})
}

func observePublicProvider(ctx context.Context, request *http.Request) (*http.Request, func(*http.Response, error)) {
	observation, _ := ctx.Value(publicContentObservationKey{}).(publicContentObservation)
	return contentcapture.ObserveProvider(observation.starter, observation.identity, request)
}
