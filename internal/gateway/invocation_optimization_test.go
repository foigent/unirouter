package gateway

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/responsecache"
	"github.com/uniroute/unirouter/internal/testutil"
)

func canaryCompressionManager(t *testing.T, policy invocationopt.Policy) *invocationopt.Manager {
	t.Helper()
	database := &testutil.MockDB{QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*int64) = policy.ID
			*dest[1].(*string) = policy.ContractVersion
			*dest[2].(*int64) = policy.OrgID
			*dest[3].(*int64) = policy.ProjectID
			*dest[4].(*string) = policy.PolicyRef
			*dest[5].(*string) = policy.DisplayName
			*dest[6].(*string) = policy.ModelSelector
			*dest[7].(*string) = policy.Region
			*dest[8].(*string) = policy.Residency
			*dest[9].(*string) = policy.ResponseCacheTarget
			*dest[10].(*int) = policy.ResponseCacheCanaryBPS
			*dest[11].(*int) = policy.ResponseCacheTTLSeconds
			*dest[12].(*bool) = policy.ResponseCachePersistence
			*dest[13].(*bool) = policy.ResponseCacheRequireOptIn
			*dest[14].(*string) = policy.CompressionTarget
			*dest[15].(*int) = policy.CompressionCanaryBPS
			*dest[16].(*bool) = policy.CompressionRequireOptIn
			*dest[17].(*int) = policy.CompressionMinimumInputTokens
			*dest[18].(*int) = policy.CompressionMinimumSavedTokens
			*dest[19].(*int) = policy.CompressionMaximumReductionBPS
			*dest[20].(*int) = policy.MinimumShadowSamples
			*dest[21].(*string) = policy.Status
			*dest[22].(*string) = policy.Stage
			*dest[23].(*int64) = policy.Revision
			*dest[24].(*int64) = policy.CreatedBy
			*dest[25].(*int64) = policy.UpdatedBy
			*dest[26].(**time.Time) = policy.ValidatedAt
			*dest[27].(*time.Time) = policy.CreatedAt
			*dest[28].(*time.Time) = policy.UpdatedAt
			return nil
		}}
	}}
	manager, err := invocationopt.NewManager(database)
	require.NoError(t, err)
	return manager
}

func TestEvaluateResponseCacheRejectsMissingAuthorizationEpoch(t *testing.T) {
	var receiptArgs []any
	database := &testutil.MockDB{ExecFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
		receiptArgs = append([]any(nil), args...)
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}}
	manager, err := invocationopt.NewManager(database)
	require.NoError(t, err)
	relay := &Relay{invocationOptimization: manager, runtimeEnvironment: "test", runtimeVersion: "v1", runtimeCommit: "abc123"}

	draft := invocationopt.DefaultDraft()
	draft.OrgID = 7
	draft.ProjectID = 11
	draft.PolicyRef = "opt_cache_epoch"
	draft.DisplayName = "Cache epoch"
	draft.ResponseCacheTarget = invocationopt.TargetCanary
	draft.ResponseCacheCanaryBPS = 1000
	draft.ResponseCachePersistence = true
	policy := invocationopt.Policy{
		ID: 1, ContractVersion: invocationopt.PolicyContractVersion, Draft: draft,
		Stage: invocationopt.StageCanary, Revision: 4, CreatedBy: 3, UpdatedBy: 3,
	}
	route := &RouteResult{
		Channel: models.Channel{
			ID: 19, Name: "openai-primary", Provider: "openai", BaseURL: "https://example.invalid",
			AdapterType: "openai", RoutingEpoch: 2, CircuitEpoch: 3,
		},
		ChannelKey:  models.ChannelKey{ID: 23, ChannelID: 19, StateEpoch: 5},
		MappedModel: "gpt-4o-2024-08-06", RoutingPolicyVersion: "route-v1",
		RuntimeReleaseRef: "release-v1", RuntimeStage: "stable", RuntimeAssignmentID: "assignment-v1",
	}
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"exact prompt"}],"stream":false}`)
	upstream, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)

	header := http.Header{}
	header.Set(responseCacheOptInHeader, "allow")
	decision := relay.evaluateResponseCache(
		context.Background(), header,
		"req-cache-epoch", "gpt-4o", &models.ApiKey{ID: 31, OrgID: 7, AuthEpoch: 0},
		invocationOptimizationState{policy: policy, found: true}, route, upstream, "price-v1",
		logger.New(logger.Fields{Module: "test"}),
	)
	relay.recordResponseCacheDecision(context.Background(), decision, logger.New(logger.Fields{Module: "test"}))

	require.Nil(t, decision.hit)
	require.Nil(t, decision.miss)
	require.False(t, decision.reject)
	require.Equal(t, responsecache.ReasonInvalidConfiguration, decision.reason)
	require.Len(t, receiptArgs, 31)
	require.Equal(t, "invalid_authorization_epoch", receiptArgs[12])
	require.Equal(t, responsecache.ReasonInvalidConfiguration, receiptArgs[13])
	require.Equal(t, false, receiptArgs[15])
}

func TestResponseCacheShadowNeverMarksCanaryAssignment(t *testing.T) {
	var receiptArgs []any
	database := &testutil.MockDB{ExecFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
		receiptArgs = append([]any(nil), args...)
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}}
	manager, err := invocationopt.NewManager(database)
	require.NoError(t, err)
	relay := &Relay{invocationOptimization: manager, runtimeEnvironment: "test", runtimeVersion: "v1", runtimeCommit: "abc123"}

	draft := invocationopt.DefaultDraft()
	draft.OrgID, draft.ProjectID = 7, 11
	draft.PolicyRef, draft.DisplayName = "opt_cache_shadow", "Cache shadow"
	draft.ResponseCacheTarget, draft.ResponseCacheCanaryBPS = invocationopt.TargetCanary, 1000
	draft.ResponseCachePersistence = true
	policy := invocationopt.Policy{
		ID: 1, ContractVersion: invocationopt.PolicyContractVersion, Draft: draft,
		Stage: invocationopt.StageShadow, Revision: 3, CreatedBy: 3, UpdatedBy: 3,
	}
	route := &RouteResult{
		Channel: models.Channel{
			ID: 19, Name: "openai-primary", Provider: "openai", BaseURL: "https://example.invalid",
			AdapterType: "openai", RoutingEpoch: 2, CircuitEpoch: 3,
		},
		ChannelKey:  models.ChannelKey{ID: 23, ChannelID: 19, StateEpoch: 5},
		MappedModel: "gpt-4o-2024-08-06", RoutingPolicyVersion: "route-v1",
		RuntimeReleaseRef: "release-v1", RuntimeStage: "stable", RuntimeAssignmentID: "assignment-v1",
	}
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"exact prompt"}],"stream":false}`)
	upstream, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	header := http.Header{}
	header.Set(responseCacheOptInHeader, "allow")

	decision := relay.evaluateResponseCache(
		context.Background(), header, "op_0123456789abcdef0123456789abcdef", "gpt-4o",
		&models.ApiKey{ID: 31, OrgID: 7, AuthEpoch: 1},
		invocationOptimizationState{policy: policy, found: true}, route, upstream, "price-v1",
		logger.New(logger.Fields{Module: "test"}),
	)
	relay.recordResponseCacheDecision(context.Background(), decision, logger.New(logger.Fields{Module: "test"}))

	require.Nil(t, decision.hit)
	require.Nil(t, decision.miss)
	require.False(t, decision.reject)
	require.Len(t, receiptArgs, 31)
	require.Equal(t, invocationopt.EffectiveShadow, receiptArgs[11])
	require.Equal(t, false, receiptArgs[14])
	require.Equal(t, false, receiptArgs[16])
}

func TestResponseCacheShadowAndUnassignedCanaryInvalidIdentityRemainEvidenceOnly(t *testing.T) {
	var receiptArgs []any
	database := &testutil.MockDB{ExecFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
		receiptArgs = append([]any(nil), args...)
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}}
	manager, err := invocationopt.NewManager(database)
	require.NoError(t, err)
	relay := &Relay{invocationOptimization: manager, runtimeEnvironment: "test", runtimeVersion: "v1", runtimeCommit: "abc123"}

	draft := invocationopt.DefaultDraft()
	draft.OrgID, draft.ProjectID = 7, 11
	draft.PolicyRef, draft.DisplayName = "opt_cache_shadow_invalid", "Cache shadow invalid identity"
	draft.ResponseCacheTarget, draft.ResponseCacheCanaryBPS = invocationopt.TargetCanary, 1000
	draft.ResponseCachePersistence = true
	policy := invocationopt.Policy{
		ID: 1, ContractVersion: invocationopt.PolicyContractVersion, Draft: draft,
		Stage: invocationopt.StageShadow, Revision: 3, CreatedBy: 3, UpdatedBy: 3,
	}
	route := &RouteResult{
		Channel: models.Channel{
			ID: 19, Name: "openai-primary", Provider: "openai", BaseURL: "https://example.invalid",
			AdapterType: "openai", RoutingEpoch: 0, CircuitEpoch: 3,
		},
		ChannelKey:  models.ChannelKey{ID: 23, ChannelID: 19, StateEpoch: 5},
		MappedModel: "gpt-4o-2024-08-06", RoutingPolicyVersion: "route-v1",
		RuntimeReleaseRef: "release-v1", RuntimeStage: "stable", RuntimeAssignmentID: "assignment-v1",
	}
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"exact prompt"}],"stream":false}`)
	upstream, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	header := http.Header{}
	header.Set(responseCacheOptInHeader, "allow")

	decision := relay.evaluateResponseCache(
		context.Background(), header, "op_0123456789abcdef0123456789abcdef", "gpt-4o",
		&models.ApiKey{ID: 31, OrgID: 7, AuthEpoch: 1},
		invocationOptimizationState{policy: policy, found: true}, route, upstream, "price-v1",
		logger.New(logger.Fields{Module: "test"}),
	)
	relay.recordResponseCacheDecision(context.Background(), decision, logger.New(logger.Fields{Module: "test"}))

	require.Nil(t, decision.hit)
	require.Nil(t, decision.miss)
	require.False(t, decision.reject)
	require.Equal(t, responsecache.ReasonInvalidConfiguration, decision.reason)
	require.Len(t, receiptArgs, 31)
	require.Equal(t, invocationopt.EffectiveShadow, receiptArgs[11])
	require.Equal(t, false, receiptArgs[14])
	require.Equal(t, false, receiptArgs[16])

	policy.Stage = invocationopt.StageCanary
	unassignedRequestID := ""
	for index := 0; index < 10_000; index++ {
		candidate := fmt.Sprintf("op_%032x", index)
		if !invocationopt.Assigned(policy, invocationopt.DriverResponseCache, 31, candidate) {
			unassignedRequestID = candidate
			break
		}
	}
	require.NotEmpty(t, unassignedRequestID)
	decision = relay.evaluateResponseCache(
		context.Background(), header, unassignedRequestID, "gpt-4o",
		&models.ApiKey{ID: 31, OrgID: 7, AuthEpoch: 1},
		invocationOptimizationState{policy: policy, found: true}, route, upstream, "price-v1",
		logger.New(logger.Fields{Module: "test"}),
	)
	relay.recordResponseCacheDecision(context.Background(), decision, logger.New(logger.Fields{Module: "test"}))
	require.Nil(t, decision.hit)
	require.Nil(t, decision.miss)
	require.False(t, decision.reject)
	require.Equal(t, responsecache.ReasonInvalidConfiguration, decision.reason)
	require.Equal(t, invocationopt.EffectiveCanary, receiptArgs[11])
	require.Equal(t, "canary_unassigned", receiptArgs[12])
	require.Equal(t, false, receiptArgs[14])
	require.Equal(t, false, receiptArgs[16])

	keyring, err := responsecache.NewStaticKeyring("cache-key-v1", map[string][]byte{
		"cache-key-v1": bytes.Repeat([]byte{'e'}, 32),
	})
	require.NoError(t, err)
	cacheService, err := responsecache.NewService(
		responsecache.NewMemoryBackend(time.Now), bytes.Repeat([]byte{'i'}, 32), keyring,
		responsecache.DefaultConfig(),
	)
	require.NoError(t, err)
	relay.responseCache = cacheService
	assignedRequestID := ""
	for index := 0; index < 10_000; index++ {
		candidate := fmt.Sprintf("op_%032x", index)
		if invocationopt.Assigned(policy, invocationopt.DriverResponseCache, 31, candidate) {
			assignedRequestID = candidate
			break
		}
	}
	require.NotEmpty(t, assignedRequestID)
	decision = relay.evaluateResponseCache(
		context.Background(), header, assignedRequestID, "gpt-4o",
		&models.ApiKey{ID: 31, OrgID: 7, AuthEpoch: 1},
		invocationOptimizationState{policy: policy, found: true}, route, upstream, "price-v1",
		logger.New(logger.Fields{Module: "test"}),
	)
	relay.recordResponseCacheDecision(context.Background(), decision, logger.New(logger.Fields{Module: "test"}))
	require.Nil(t, decision.hit)
	require.Nil(t, decision.miss)
	require.False(t, decision.reject)
	require.Equal(t, responsecache.ReasonInvalidConfiguration, decision.reason)
	require.Equal(t, "bypass", receiptArgs[12])
	require.Equal(t, true, receiptArgs[14])
	require.Equal(t, false, receiptArgs[16])
}

func TestApplyResponseCacheDeliveryUsageDoesNotExposeSourceCounters(t *testing.T) {
	response := map[string]any{
		"id": "chatcmpl-source",
		"usage": map[string]any{
			"prompt_tokens":           150,
			"completion_tokens":       25,
			"total_tokens":            175,
			"prompt_cache_hit_tokens": 120,
		},
	}

	applyResponseCacheDeliveryUsage(response)

	usage, ok := response["usage"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 0, usage["prompt_tokens"])
	require.Equal(t, 0, usage["completion_tokens"])
	require.Equal(t, 0, usage["total_tokens"])
	require.NotContains(t, usage, "prompt_cache_hit_tokens")
	details, ok := usage["prompt_tokens_details"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 0, details["cached_tokens"])
}

func TestApplyResponseCacheDeliveryIdentityDoesNotExposeSourceExecutionIdentity(t *testing.T) {
	response := map[string]any{
		"id":      "chatcmpl-source-provider-execution",
		"created": float64(1_700_000_000),
	}
	deliveredAt := time.Date(2026, time.August, 4, 12, 34, 56, 0, time.UTC)

	applyResponseCacheDeliveryIdentity(response, "op_0123456789abcdef0123456789abcdef", deliveredAt)

	require.Equal(t, "chatcmpl-cache-op_0123456789abcdef0123456789abcdef", response["id"])
	require.Equal(t, deliveredAt.Unix(), response["created"])
}

func TestRequestOptimizationHeadersAreExplicit(t *testing.T) {
	header := http.Header{}
	require.False(t, requestHeaderAllows(header, responseCacheOptInHeader))
	header.Set(responseCacheOptInHeader, "ALLOW")
	require.True(t, requestHeaderAllows(header, responseCacheOptInHeader))
	header.Set("Cache-Control", "private, no-store, max-age=0")
	require.True(t, requestCacheNoStore(header))
	header.Set("Cache-Control", "no-cache")
	require.False(t, requestCacheNoStore(header))
}

func TestProviderRequestCountContextUsesActualDispatchCount(t *testing.T) {
	ctx := contextWithProviderRequestCount(context.Background(), 2)
	require.EqualValues(t, 2, providerRequestCountFromContext(ctx))
	require.Zero(t, providerRequestCountFromContext(context.Background()))
	require.Zero(t, providerRequestCountFromContext(contextWithProviderRequestCount(context.Background(), 0)))
}

func TestCompressionCanaryRequiresDeterministicAssignmentAndExplicitOptIn(t *testing.T) {
	draft := invocationopt.DefaultDraft()
	draft.OrgID = 7
	draft.ProjectID = 11
	draft.PolicyRef = "opt_compression_canary"
	draft.DisplayName = "Compression canary"
	draft.CompressionTarget = invocationopt.TargetCanary
	draft.CompressionCanaryBPS = 1000
	draft.CompressionMinimumInputTokens = 1
	draft.CompressionMinimumSavedTokens = 1
	draft.CompressionMaximumReductionBPS = 5000
	policy := invocationopt.Policy{
		ID: 4, ContractVersion: invocationopt.PolicyContractVersion, Draft: draft,
		Stage: invocationopt.StageCanary, Revision: 3, CreatedBy: 2, UpdatedBy: 2,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	manager := canaryCompressionManager(t, policy)
	relay := &Relay{invocationOptimization: manager}
	apiKey := &models.ApiKey{ID: 31, OrgID: 7, AuthEpoch: 1}

	body := []byte(`{"model":"company-chat","messages":[{"role":"user","content":"Alpha  beta  gamma  delta  epsilon  zeta  eta  theta  iota  kappa  lambda  mu  nu  xi  omicron"}],"stream":false}`)
	requestID := ""
	for index := 0; index < 10_000; index++ {
		requestID = fmt.Sprintf("op_%032x", index)
		if invocationopt.Assigned(policy, invocationopt.DriverCompression, apiKey.ID, requestID) {
			break
		}
	}
	require.True(t, invocationopt.Assigned(policy, invocationopt.DriverCompression, apiKey.ID, requestID), "test vector must enter the fixed 10%% canary bucket")

	withoutOptIn := relay.evaluateCompression(context.Background(), http.Header{}, requestID, "company-chat", apiKey, "company-chat", body)
	require.Equal(t, body, withoutOptIn.effectiveBody)
	require.NotNil(t, withoutOptIn.compression)
	require.Equal(t, "request_opt_in_required", withoutOptIn.compression.Decision)
	require.False(t, withoutOptIn.compression.MutationApplied)

	header := http.Header{}
	header.Set(compressionOptInHeader, "allow")
	withOptIn := relay.evaluateCompression(context.Background(), header, requestID, "company-chat", apiKey, "company-chat", body)
	require.NotNil(t, withOptIn.compression)
	require.Equal(t, "compressed", withOptIn.compression.Decision)
	require.True(t, withOptIn.compression.MutationApplied)
	require.NotEqual(t, body, withOptIn.effectiveBody)
	require.NotContains(t, string(withOptIn.effectiveBody), "  ")
}

func TestCompressionShadowNeverMarksCanaryAssignmentOrMutates(t *testing.T) {
	draft := invocationopt.DefaultDraft()
	draft.OrgID, draft.ProjectID = 7, 11
	draft.PolicyRef, draft.DisplayName = "opt_compression_shadow", "Compression shadow"
	draft.CompressionTarget, draft.CompressionCanaryBPS = invocationopt.TargetCanary, 1000
	draft.CompressionMinimumInputTokens = 1
	draft.CompressionMinimumSavedTokens = 1
	draft.CompressionMaximumReductionBPS = 5000
	policy := invocationopt.Policy{
		ID: 4, ContractVersion: invocationopt.PolicyContractVersion, Draft: draft,
		Stage: invocationopt.StageShadow, Revision: 3, CreatedBy: 2, UpdatedBy: 2,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	relay := &Relay{invocationOptimization: canaryCompressionManager(t, policy)}
	body := []byte(`{"model":"company-chat","messages":[{"role":"user","content":"Alpha  beta  gamma  delta  epsilon  zeta  eta  theta  iota  kappa  lambda  mu  nu  xi  omicron"}],"stream":false}`)

	state := relay.evaluateCompression(
		context.Background(), http.Header{}, "op_0123456789abcdef0123456789abcdef",
		"company-chat", &models.ApiKey{ID: 31, OrgID: 7, AuthEpoch: 1}, "company-chat", body,
	)

	require.NotNil(t, state.compression)
	require.Equal(t, invocationopt.EffectiveShadow, state.compression.EffectiveMode)
	require.False(t, state.compression.Assigned)
	require.False(t, state.compression.MutationApplied)
	require.Equal(t, body, state.effectiveBody)
}
