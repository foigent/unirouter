//go:build integration

package gateway

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/responsecache"
)

// VerifyContentPolicyCachePaths exercises the real gateway cache read and commit
// paths with an externally provided content authority. The external test supplies
// the actual sourcefacts service without creating a gateway/sourcefacts import cycle.
func VerifyContentPolicyCachePaths(t *testing.T, service contentcapture.Starter, orgID, projectID, keyID int64, updatePolicy func([]string), omitChatProtocol func()) {
	t.Helper()
	ctx := context.WithValue(t.Context(), middleware.CtxProjectID, projectID)
	allPoints := []string{"client_request", "provider_request", "provider_response", "client_response"}
	ring, err := responsecache.NewStaticKeyring("cache-key", map[string][]byte{"cache-key": bytes.Repeat([]byte{'k'}, 32)})
	require.NoError(t, err)
	cache, err := responsecache.NewService(responsecache.NewMemoryBackend(time.Now), bytes.Repeat([]byte{'i'}, 32), ring, responsecache.DefaultConfig())
	require.NoError(t, err)
	draft := invocationopt.DefaultDraft()
	draft.OrgID, draft.ProjectID = orgID, projectID
	draft.PolicyRef, draft.DisplayName = "cache-policy", "Cache policy"
	draft.ResponseCacheTarget, draft.ResponseCacheCanaryBPS = invocationopt.TargetCanary, 1000
	draft.ResponseCachePersistence = true
	policy := invocationopt.Policy{ID: 1, ContractVersion: invocationopt.PolicyContractVersion, Draft: draft,
		Stage: invocationopt.StageCanary, Revision: 1, CreatedBy: 1, UpdatedBy: 1}
	relay := &Relay{invocationOptimization: canaryCompressionManager(t, policy), responseCache: cache, contentCapture: service,
		contentPolicyRequiredForCache: true, runtimeEnvironment: "test", runtimeVersion: "v1", runtimeCommit: "abc123"}
	route := &RouteResult{Channel: models.Channel{ID: 19, Provider: "openai", AdapterType: "openai", BaseURL: "https://example.invalid",
		RoutingEpoch: 1, CircuitEpoch: 1}, ChannelKey: models.ChannelKey{ID: 23, ChannelID: 19, StateEpoch: 1},
		MappedModel: "gpt-4o", RoutingPolicyVersion: "route-v1", RuntimeReleaseRef: "release-v1"}
	apiKey := &models.ApiKey{ID: keyID, OrgID: orgID, ProjectID: &projectID, KeyType: "shared", AuthEpoch: 1}
	header := http.Header{}
	header.Set(responseCacheOptInHeader, "allow")
	requestID := ""
	for i := 0; i < 10000; i++ {
		candidate := fmt.Sprintf("op_%032x", i)
		if invocationopt.Assigned(policy, invocationopt.DriverResponseCache, keyID, candidate) {
			requestID = candidate
			break
		}
	}
	require.NotEmpty(t, requestID)
	evaluate := func(prompt string) cacheRouteDecision {
		t.Helper()
		body := []byte(fmt.Sprintf(`{"model":"gpt-4o","messages":[{"role":"user","content":%q}],"stream":false}`, prompt))
		upstream, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", bytes.NewReader(body))
		require.NoError(t, err)
		return relay.evaluateResponseCache(ctx, header, requestID, "gpt-4o", apiKey,
			invocationOptimizationState{policy: policy, found: true}, route, upstream, "price-v1", nil)
	}
	first := evaluate("first prompt")
	require.NotNil(t, first.miss, "both response points allow a cache reservation: %s", first.reason)
	require.NoError(t, cache.Commit(ctx, first.miss.reservation, responsecache.StoreEntry{
		SourceResponse: []byte(`{"id":"chatcmpl-cache","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"cached answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`),
		Proof: responsecache.UsageProof{SourceRequestID: "source-cache-1", PromptTokens: 4, UncachedPromptTokens: 4, CompletionTokens: 3,
			UsageSource: metering.UsageSourceUpstream, UsageAuthority: metering.UsageAuthorityProviderReported,
			UsagePurpose: metering.UsagePurposeCustomerCharge, UsageFinality: metering.UsageFinalityFinal,
			UsageContractVersion: metering.UsageContractVersion, CustomerPricing: first.identity.CustomerPricingVersion,
			DeliveryFinality: responsecache.DeliveryCompleted, SettlementFinality: responsecache.SettlementSettled,
			Provider: first.identity.Provider, MappedModel: first.identity.ResolvedModel, ProviderRequests: 1, HTTPStatus: 200},
	}))
	require.NotNil(t, evaluate("first prompt").hit, "allowed effective policy serves an existing hit")
	updatePolicy([]string{"client_request", "provider_request"})
	denied := evaluate("first prompt")
	require.Equal(t, responsecache.ReasonPersistenceDenied, denied.reason)
	require.Nil(t, denied.hit, "omitting response points must block even a preexisting hit")
	require.Nil(t, denied.miss)
	require.False(t, denied.reject, "provider execution remains available")

	updatePolicy(allPoints)
	inFlight := evaluate("second prompt")
	require.NotNil(t, inFlight.miss)
	updatePolicy([]string{"client_request", "provider_response"})
	relay.commitResponseCacheMiss(ctx, inFlight.miss, []byte(`{"choices":[{"message":{"content":"private answer"}}]}`),
		adapters.Usage{}, "source-cache-2", 1, nil)
	updatePolicy(allPoints)
	after := evaluate("second prompt")
	require.NotNil(t, after.miss, "policy change before provider return must release reservation without a write: %s", after.reason)
	require.Nil(t, after.hit)
	require.NoError(t, cache.Release(ctx, after.miss.reservation))

	omitChatProtocol()
	updatePolicy(allPoints)
	unsupported := evaluate("first prompt")
	require.Equal(t, responsecache.ReasonPersistenceDenied, unsupported.reason)
	require.Nil(t, unsupported.hit, "full content for another protocol cannot serve a Chat hit")
	require.Nil(t, unsupported.miss)
	require.False(t, unsupported.reject)
}
