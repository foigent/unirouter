package gateway

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/responsecache"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

type dispatchFenceCountingCacheBackend struct{ calls atomic.Int64 }

func (backend *dispatchFenceCountingCacheBackend) Generation(context.Context, string) (uint64, error) {
	backend.calls.Add(1)
	return 0, nil
}

func (backend *dispatchFenceCountingCacheBackend) Read(context.Context, string) ([]byte, error) {
	backend.calls.Add(1)
	return nil, responsecache.ErrCacheMiss
}

func (backend *dispatchFenceCountingCacheBackend) Acquire(context.Context, string, string, time.Duration) (bool, error) {
	backend.calls.Add(1)
	return true, nil
}

func (backend *dispatchFenceCountingCacheBackend) Commit(context.Context, string, string, string, []byte, time.Duration) error {
	backend.calls.Add(1)
	return nil
}

func (backend *dispatchFenceCountingCacheBackend) Release(context.Context, string, string) error {
	backend.calls.Add(1)
	return nil
}

func (backend *dispatchFenceCountingCacheBackend) Invalidate(context.Context, string) error {
	backend.calls.Add(1)
	return nil
}

func (backend *dispatchFenceCountingCacheBackend) Purge(context.Context, string) (uint64, error) {
	backend.calls.Add(1)
	return 0, nil
}

func TestResponsesAndMessagesGatewayFenceFollowsPlannerAndPrecedesProviderPreparation(t *testing.T) {
	tests := []struct {
		name           string
		operation      modeloperation.ID
		publicEvidence bool
	}{
		{name: "responses operation", operation: modeloperation.IDResponses},
		{name: "messages operation", operation: modeloperation.IDMessages},
		{name: "chat with V2 route", operation: modeloperation.IDChatCompletions, publicEvidence: true},
		{name: "embeddings with V2 route", operation: modeloperation.IDEmbeddings, publicEvidence: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := modelrouting.Route{
				// Invalid provider material is intentional: reaching decrypt,
				// adapter lookup, or request construction would change the
				// returned error and fail this boundary contract.
				EncryptedSecret: "not-ciphertext", AdapterType: "not-an-adapter",
			}
			if test.publicEvidence {
				route.PublicEvidence = &executionevidence.EvidenceV2{}
			}
			plan := modelrouting.Plan{
				AuthorityMode: modelrouting.ModeCanonical,
				PublicModel:   "public-model", ModelOperationID: string(test.operation),
				PublicContextLimit: 128000, PublicOutputLimit: 8192,
				Routes: []modelrouting.Route{route},
			}
			planner := &executionPlannerStub{plan: plan}
			coordinator := &executionCoordinatorStub{}
			var freezes, ledgerAttempts, providerCalls atomic.Int64
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					freezes.Add(1)
					return &billing.FreezeResult{Success: true}, nil
				},
				beginAttemptFn: func(context.Context, billing.ProviderAttemptStart) error {
					ledgerAttempts.Add(1)
					return nil
				},
			}, nil, []byte("0123456789abcdef0123456789abcdef"))
			if err := relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()); err != nil {
				t.Fatal(err)
			}
			cacheBackend := &dispatchFenceCountingCacheBackend{}
			keyring, err := responsecache.NewStaticKeyring("fixture-v1", map[string][]byte{
				"fixture-v1": []byte("0123456789abcdef0123456789abcdef"),
			})
			if err != nil {
				t.Fatal(err)
			}
			cache, err := responsecache.NewService(
				cacheBackend, []byte("abcdef0123456789abcdef0123456789"), keyring, responsecache.DefaultConfig(),
			)
			if err != nil {
				t.Fatal(err)
			}
			relay.SetInvocationOptimization(nil, cache, "fixture-runtime", "fixture-commit", "test")
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				providerCalls.Add(1)
				return nil, errors.New("provider must not be reached")
			})}

			batch, err := relay.prepareModelExecutionRoutes(
				context.Background(), test.operation, adapters.EndpointChatCompletions,
				"public-model", "public-model", "req-dispatch-fence", nil,
				modelExecutionRequestLimits{EstimatedInputTokens: 1, EstimateVersion: chatAdmissionEstimateVersion},
				[]byte(`{"model":"public-model"}`), time.Now(),
			)
			if batch != nil || !errors.Is(err, modelexecution.ErrDispatchNotWired) {
				t.Fatalf("batch=%+v err=%v", batch, err)
			}
			planner.mu.Lock()
			requests := append([]modelrouting.Request(nil), planner.requests...)
			planner.mu.Unlock()
			if len(requests) != 1 || requests[0].ModelOperationID != string(test.operation) {
				t.Fatalf("planner boundary mismatch: %+v", requests)
			}
			if cacheBackend.calls.Load() != 0 || freezes.Load() != 0 || ledgerAttempts.Load() != 0 ||
				coordinator.authorizeCount.Load() != 0 || providerCalls.Load() != 0 {
				t.Fatalf("dispatch fence leaked side effects: cache=%d freeze=%d ledger=%d coordinator=%d provider=%d",
					cacheBackend.calls.Load(), freezes.Load(), ledgerAttempts.Load(), coordinator.authorizeCount.Load(), providerCalls.Load())
			}
		})
	}
}
