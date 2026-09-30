//go:build integration

package gateway

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/responsecache"
)

type mutableCacheContentPolicy struct{ allowed bool }

func (*mutableCacheContentPolicy) Begin(context.Context, contentcapture.Observation) (*contentcapture.Capture, error) {
	return nil, nil
}

func (p *mutableCacheContentPolicy) CachePersistenceAllowed(context.Context, contentcapture.CapturePrincipal, int64, int64) (bool, error) {
	return p.allowed, nil
}

func TestResponseCachePolicySwitchReleasesRealRedisReservationIntegration(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	require.NotEmpty(t, addr, "select this task's isolated Redis explicitly")
	client := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD")})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	ctx := t.Context()
	require.NoError(t, client.Ping(ctx).Err())
	backend, err := responsecache.NewRedisBackend(client, "uniroute:response-cache:p2-policy-test")
	require.NoError(t, err)
	ring, err := responsecache.NewStaticKeyring("p2-key", map[string][]byte{"p2-key": bytes.Repeat([]byte{'k'}, 32)})
	require.NoError(t, err)
	cache, err := responsecache.NewService(backend, bytes.Repeat([]byte{'i'}, 32), ring, responsecache.DefaultConfig())
	require.NoError(t, err)
	policy := &mutableCacheContentPolicy{allowed: true}
	relay := &Relay{responseCache: cache, contentCapture: policy}
	principal := contentcapture.CapturePrincipal{APIKeyID: 41, KeyType: "shared"}
	request := responsecache.Request{
		Identity: responsecache.Identity{
			Scope:    responsecache.Scope{OrgID: 73, ProjectID: 19, Region: "local", Residency: "local"},
			APIKeyID: 41, AuthorizationEpoch: "auth-v1", Endpoint: responsecache.EndpointChatCompletions,
			RequestedModel: "gpt-4o", ResolvedModel: "gpt-4o-2024-08-06", Provider: "openai",
			ChannelID: 17, ChannelRoutingEpoch: 3, ChannelCircuitEpoch: 8, CredentialID: 29, CredentialEpoch: 4,
			AdapterProfileVersion: "openai-v1", RoutePolicyVersion: "route-v7", RuntimeReleaseRef: "none",
			RuntimeStage: "stable", RuntimeAssignmentID: "none", GuardrailPolicyVersion: "guardrail-v3",
			OptimizationPolicyVersion: "opt-r4", CustomerPricingVersion: "price-v5",
			ResponseCachePriceVersion: "response-cache-v1", RequestSchemaVersion: responsecache.RequestSchemaChatV1,
			ResponseSchemaVersion: responsecache.ResponseSchemaChatV1,
		},
		EffectiveBody: []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"p2 private prompt"}]}`),
		OptIn:         true, PersistenceAllowed: true,
	}
	owner := cache.Begin(ctx, request)
	require.Equal(t, responsecache.DecisionOwner, owner.Kind)
	require.NotNil(t, owner.Reservation)
	policy.allowed = false // Effective policy changes while the provider call is in flight.
	relay.commitResponseCacheMiss(ctx, &responseCacheMissState{
		reservation: owner.Reservation, identity: request.Identity, principal: principal, orgID: 73, projectID: 19,
	}, []byte(`{"choices":[{"message":{"content":"p2 private answer"}}]}`),
		adapters.Usage{}, "p2-source-request", 1, nil)
	keys, err := client.Keys(ctx, "uniroute:response-cache:p2-policy-test:rc:v2:*").Result()
	require.NoError(t, err)
	require.Empty(t, keys, "denied commit must release the lease without storing the response")
	policy.allowed = true
	again := cache.Begin(ctx, request)
	require.Equal(t, responsecache.DecisionOwner, again.Kind, "the denied write must not create a cache hit")
	require.NoError(t, cache.Release(ctx, again.Reservation))
}
