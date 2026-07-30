package responsecache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
)

var (
	fixtureRequestBody  = []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"exact private prompt"}],"stream":false,"temperature":0}`)
	fixtureResponseBody = []byte(`{"id":"chatcmpl-fixture","object":"chat.completion","model":"gpt-4o-2024-08-06","choices":[{"index":0,"message":{"role":"assistant","content":"exact private answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`)
)

type mutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mutableClock) Time() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mutableClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

func fixtureIdentity() Identity {
	return Identity{
		Scope: Scope{
			OrgID: 73, ProjectID: 19, Region: "us-east-1", Residency: "us",
		},
		APIKeyID:                  41,
		AuthorizationEpoch:        "auth-v1",
		Endpoint:                  EndpointChatCompletions,
		RequestedModel:            "gpt-4o",
		ResolvedModel:             "gpt-4o-2024-08-06",
		AdapterProfileVersion:     "openai-v1",
		RoutePolicyVersion:        "route-v7",
		GuardrailPolicyVersion:    "guardrail-v3",
		CustomerPricingVersion:    "customer-price-v5",
		ResponseCachePriceVersion: "response-cache-price-unresolved-v1",
		RequestSchemaVersion:      RequestSchemaChatV1,
		ResponseSchemaVersion:     ResponseSchemaChatV1,
	}
}

func fixtureRequest() Request {
	return Request{
		Identity: fixtureIdentity(), EffectiveBody: append([]byte(nil), fixtureRequestBody...),
		OptIn: true, PersistenceAllowed: true,
	}
}

func fixtureProof(identity Identity) UsageProof {
	return UsageProof{
		PromptTokens: 4, UncachedPromptTokens: 4, CompletionTokens: 3,
		UsageSource:          metering.UsageSourceUpstream,
		UsageAuthority:       metering.UsageAuthorityProviderReported,
		UsagePurpose:         metering.UsagePurposeCustomerCharge,
		UsageFinality:        metering.UsageFinalityFinal,
		UsageContractVersion: metering.UsageContractVersion,
		CustomerPricing:      identity.CustomerPricingVersion,
		DeliveryFinality:     DeliveryCompleted,
		SettlementFinality:   SettlementSettled,
		Provider:             "openai",
		MappedModel:          identity.ResolvedModel,
		ProviderRequests:     1,
		HTTPStatus:           200,
	}
}

func fixtureConfig() Config {
	config := DefaultConfig()
	config.TTL = 5 * time.Minute
	config.LeaseTTL = time.Second
	config.MaxWait = 80 * time.Millisecond
	config.PollInterval = 2 * time.Millisecond
	config.DependencyTimeout = 30 * time.Millisecond
	return config
}

func fixtureKeyring(t *testing.T, current string, keys map[string][]byte) *StaticKeyring {
	t.Helper()
	ring, err := NewStaticKeyring(current, keys)
	require.NoError(t, err)
	return ring
}

func newFixtureService(
	t *testing.T,
	backend Backend,
	clock func() time.Time,
	keyring EncryptionKeyring,
	config Config,
) *Service {
	t.Helper()
	service, err := NewService(
		backend,
		bytes.Repeat([]byte{0x51}, 32),
		keyring,
		config,
		WithClock(clock),
	)
	require.NoError(t, err)
	return service
}

func newMemoryFixture(t *testing.T) (*Service, *MemoryBackend, *mutableClock) {
	t.Helper()
	clock := &mutableClock{now: time.Date(2026, 7, 31, 12, 0, 0, 123456000, time.UTC)}
	backend := NewMemoryBackend(clock.Time)
	ring := fixtureKeyring(t, "k1", map[string][]byte{
		"k1": bytes.Repeat([]byte{0x31}, 32),
	})
	return newFixtureService(t, backend, clock.Time, ring, fixtureConfig()), backend, clock
}

func storeFixture(t *testing.T, service *Service, request Request) Decision {
	t.Helper()
	owner := service.Begin(context.Background(), request)
	require.Equal(t, DecisionOwner, owner.Kind)
	require.NotNil(t, owner.Reservation)
	require.NoError(t, service.Commit(context.Background(), owner.Reservation, StoreEntry{
		SourceResponse: fixtureResponseBody,
		Proof:          fixtureProof(request.Identity),
	}))
	hit := service.Begin(context.Background(), request)
	require.Equal(t, DecisionHit, hit.Kind)
	require.NotNil(t, hit.Hit)
	return hit
}

func TestExactHitIsEncryptedAndDoesNotInventBilling(t *testing.T) {
	service, backend, _ := newMemoryFixture(t)
	request := fixtureRequest()
	hit := storeFixture(t, service, request)

	require.Equal(t, fixtureResponseBody, hit.Hit.SourceResponse)
	require.EqualValues(t, 0, hit.Hit.CurrentProviderRequests)
	require.Equal(t, CurrentProviderCostZero, hit.Hit.CurrentProviderCostMicros)
	require.Equal(t, CustomerChargeUnpriced, hit.Hit.CustomerChargeState)
	require.Equal(t, fixtureProof(request.Identity), hit.Hit.SourceProof)

	backend.mu.Lock()
	defer backend.mu.Unlock()
	require.Len(t, backend.values, 1)
	for key, value := range backend.values {
		require.NotContains(t, key, "exact private")
		require.NotContains(t, key, request.Identity.RequestedModel)
		require.NotContains(t, key, fmt.Sprintf("%d", request.Identity.Scope.OrgID))
		require.False(t, bytes.Contains(value.bytes, []byte("exact private prompt")))
		require.False(t, bytes.Contains(value.bytes, []byte("exact private answer")))
		require.False(t, bytes.Contains(value.bytes, []byte(request.Identity.ResolvedModel)))
		require.False(t, bytes.Contains(value.bytes, []byte("openai")))
	}
}

func TestDeterministicOpaqueIndexVectors(t *testing.T) {
	service, _, _ := newMemoryFixture(t)
	request := fixtureRequest()
	require.Equal(
		t,
		"rc:v1:scope:f326daea3bf39bb1982c92558b8fa604555999b87b28bdd75560359185e668cc",
		service.scopeKey(request.Identity.Scope),
	)
	require.Equal(
		t,
		"rc:v1:entry:2e3c1d1bf915257844868441b8e4779e921acaedcd5f4f7446917a93df8d7ca5",
		service.entryKey(request.Identity, request.EffectiveBody, 0),
	)
}

func TestExactIdentityAndRequestBytesAreIsolationBoundary(t *testing.T) {
	service, _, _ := newMemoryFixture(t)
	base := fixtureRequest()
	storeFixture(t, service, base)

	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{"org", func(r *Request) { r.Identity.Scope.OrgID++ }},
		{"project", func(r *Request) { r.Identity.Scope.ProjectID++ }},
		{"region", func(r *Request) { r.Identity.Scope.Region = "eu-west-1" }},
		{"residency", func(r *Request) { r.Identity.Scope.Residency = "eu" }},
		{"api-key", func(r *Request) { r.Identity.APIKeyID++ }},
		{"authorization-epoch", func(r *Request) { r.Identity.AuthorizationEpoch = "auth-v2" }},
		{"requested-model", func(r *Request) { r.Identity.RequestedModel = "gpt-4o-mini" }},
		{"resolved-model", func(r *Request) { r.Identity.ResolvedModel = "gpt-4o-mini-2024-07-18" }},
		{"adapter", func(r *Request) { r.Identity.AdapterProfileVersion = "openai-v2" }},
		{"route-policy", func(r *Request) { r.Identity.RoutePolicyVersion = "route-v8" }},
		{"guardrail", func(r *Request) { r.Identity.GuardrailPolicyVersion = "guardrail-v4" }},
		{"customer-pricing", func(r *Request) { r.Identity.CustomerPricingVersion = "customer-price-v6" }},
		{"cache-pricing", func(r *Request) { r.Identity.ResponseCachePriceVersion = "response-cache-price-v2" }},
		{"request-bytes", func(r *Request) {
			r.EffectiveBody = []byte("{ \"model\":\"gpt-4o\", \"messages\":[{\"role\":\"user\",\"content\":\"exact private prompt\"}], \"stream\":false, \"temperature\":0 }")
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := fixtureRequest()
			test.mutate(&request)
			decision := service.Begin(context.Background(), request)
			require.Equal(t, DecisionOwner, decision.Kind)
			require.NoError(t, service.Release(context.Background(), decision.Reservation))
		})
	}
	require.Equal(t, DecisionHit, service.Begin(context.Background(), base).Kind)
}

func TestIneligibleRequestsBypassBeforeBackend(t *testing.T) {
	service, backend, _ := newMemoryFixture(t)
	backend.SetAvailable(false)

	tests := []struct {
		name   string
		mutate func(*Request)
		reason string
	}{
		{"not opted in", func(r *Request) { r.OptIn = false }, ReasonNotOptedIn},
		{"persistence denied", func(r *Request) { r.PersistenceAllowed = false }, ReasonPersistenceDenied},
		{"no store", func(r *Request) { r.NoStore = true }, ReasonNoStore},
		{"empty", func(r *Request) { r.EffectiveBody = nil }, ReasonIneligibleShape},
		{"stream", func(r *Request) {
			r.EffectiveBody = []byte(`{"model":"gpt-4o","stream":true,"messages":[]}`)
		}, ReasonIneligibleShape},
		{"tools", func(r *Request) {
			r.EffectiveBody = []byte(`{"model":"gpt-4o","messages":[],"tools":[]}`)
		}, ReasonIneligibleShape},
		{"nested duplicate", func(r *Request) {
			r.EffectiveBody = []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":{"text":"a","text":"b"}}]}`)
		}, ReasonIneligibleShape},
		{"tool role", func(r *Request) {
			r.EffectiveBody = []byte(`{"model":"gpt-4o","messages":[{"role":"tool","content":"result"}]}`)
		}, ReasonIneligibleShape},
		{"nested tool calls", func(r *Request) {
			r.EffectiveBody = []byte(`{"model":"gpt-4o","messages":[{"role":"assistant","content":null,"tool_calls":[]}]}`)
		}, ReasonIneligibleShape},
		{"multimodal content", func(r *Request) {
			r.EffectiveBody = []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid/x"}}]}]}`)
		}, ReasonIneligibleShape},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := fixtureRequest()
			test.mutate(&request)
			decision := service.Begin(context.Background(), request)
			require.Equal(t, DecisionBypass, decision.Kind)
			require.Equal(t, test.reason, decision.Reason)
		})
	}

	eligible := fixtureRequest()
	decision := service.Begin(context.Background(), eligible)
	require.Equal(t, DecisionBypass, decision.Kind)
	require.Equal(t, ReasonBackendUnavailable, decision.Reason)
}

func TestCommitRequiresFinalProviderProofAndSafeResponse(t *testing.T) {
	tests := []struct {
		name          string
		mutateProof   func(*UsageProof)
		response      []byte
		expectedError error
	}{
		{"delivery not final", func(p *UsageProof) { p.DeliveryFinality = "" }, fixtureResponseBody, ErrInvalidProof},
		{"settlement not final", func(p *UsageProof) { p.SettlementFinality = "" }, fixtureResponseBody, ErrInvalidProof},
		{"estimated authority", func(p *UsageProof) {
			p.UsageAuthority = metering.UsageAuthorityConservativeEstimate
		}, fixtureResponseBody, ErrInvalidProof},
		{"pricing mismatch", func(p *UsageProof) { p.CustomerPricing = "other" }, fixtureResponseBody, ErrInvalidProof},
		{"model mismatch", func(p *UsageProof) { p.MappedModel = "other-model" }, fixtureResponseBody, ErrInvalidProof},
		{"invalid partition", func(p *UsageProof) { p.UncachedPromptTokens = 3 }, fixtureResponseBody, ErrInvalidProof},
		{"counter overflow", func(p *UsageProof) {
			p.PromptTokens = maxUsageTokens + 1
			p.UncachedPromptTokens = maxUsageTokens + 1
		}, fixtureResponseBody, ErrInvalidProof},
		{"http only", func(p *UsageProof) {
			p.DeliveryFinality = ""
			p.SettlementFinality = ""
		}, fixtureResponseBody, ErrInvalidProof},
		{"tool response", nil, []byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[]},"finish_reason":"tool_calls"}]}`), ErrInvalidResponse},
		{"request extension replay", nil, []byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"uniroute":{"request_id":"secret"}}`), ErrInvalidResponse},
		{"nested duplicate response", nil, []byte(`{"choices":[{"message":{"role":"assistant","content":"a","content":"b"},"finish_reason":"stop"}]}`), ErrInvalidResponse},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _, _ := newMemoryFixture(t)
			request := fixtureRequest()
			owner := service.Begin(context.Background(), request)
			require.Equal(t, DecisionOwner, owner.Kind)
			proof := fixtureProof(request.Identity)
			if test.mutateProof != nil {
				test.mutateProof(&proof)
			}
			err := service.Commit(context.Background(), owner.Reservation, StoreEntry{
				SourceResponse: test.response,
				Proof:          proof,
			})
			require.ErrorIs(t, err, test.expectedError)
		})
	}
}

func TestConcurrentMissHasOneOwnerAndWaitersBecomeHits(t *testing.T) {
	service, _, _ := newMemoryFixture(t)
	request := fixtureRequest()
	const contenders = 32
	start := make(chan struct{})
	decisions := make(chan Decision, contenders)
	var group sync.WaitGroup
	for index := 0; index < contenders; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			decisions <- service.Begin(context.Background(), request)
		}()
	}
	close(start)
	group.Wait()
	close(decisions)

	var owner *Reservation
	waits := make([]*Wait, 0, contenders-1)
	for decision := range decisions {
		switch decision.Kind {
		case DecisionOwner:
			require.Nil(t, owner)
			owner = decision.Reservation
		case DecisionWait:
			waits = append(waits, decision.Wait)
		default:
			t.Fatalf("unexpected decision: %+v", decision)
		}
	}
	require.NotNil(t, owner)
	require.Len(t, waits, contenders-1)
	require.NoError(t, service.Commit(context.Background(), owner, StoreEntry{
		SourceResponse: fixtureResponseBody,
		Proof:          fixtureProof(request.Identity),
	}))

	group = sync.WaitGroup{}
	hits := make(chan Decision, len(waits))
	for _, wait := range waits {
		group.Add(1)
		go func(wait *Wait) {
			defer group.Done()
			hits <- service.Await(context.Background(), wait)
		}(wait)
	}
	group.Wait()
	close(hits)
	for decision := range hits {
		require.Equal(t, DecisionHit, decision.Kind)
		require.Equal(t, fixtureResponseBody, decision.Hit.SourceResponse)
	}
}

func TestWaitTimeoutRejectsInsteadOfStampeding(t *testing.T) {
	service, _, _ := newMemoryFixture(t)
	request := fixtureRequest()
	owner := service.Begin(context.Background(), request)
	require.Equal(t, DecisionOwner, owner.Kind)
	waiter := service.Begin(context.Background(), request)
	require.Equal(t, DecisionWait, waiter.Kind)

	result := service.Await(context.Background(), waiter.Wait)
	require.Equal(t, DecisionReject, result.Kind)
	require.Equal(t, ReasonWaitTimeout, result.Reason)
	require.NoError(t, service.Release(context.Background(), owner.Reservation))
}

func TestWaiterCapacityIsFailClosed(t *testing.T) {
	service, _, _ := newMemoryFixture(t)
	service.config.MaxWait = 150 * time.Millisecond
	service.waiters = make(chan struct{}, 1)
	request := fixtureRequest()
	owner := service.Begin(context.Background(), request)
	waiter := service.Begin(context.Background(), request)
	require.Equal(t, DecisionWait, waiter.Kind)

	firstResult := make(chan Decision, 1)
	go func() {
		firstResult <- service.Await(context.Background(), waiter.Wait)
	}()
	require.Eventually(t, func() bool { return len(service.waiters) == 1 }, time.Second, time.Millisecond)

	second := service.Await(context.Background(), waiter.Wait)
	require.Equal(t, DecisionReject, second.Kind)
	require.Equal(t, ReasonWaiterCapacity, second.Reason)
	require.Equal(t, DecisionReject, (<-firstResult).Kind)
	require.NoError(t, service.Release(context.Background(), owner.Reservation))
}

func TestPurgeGenerationTombstoneAndTTL(t *testing.T) {
	service, backend, clock := newMemoryFixture(t)
	request := fixtureRequest()
	storeFixture(t, service, request)

	backend.mu.Lock()
	require.Len(t, backend.values, 1)
	var oldKey string
	for key := range backend.values {
		oldKey = key
	}
	backend.mu.Unlock()

	generation, err := service.PurgeScope(context.Background(), request.Identity.Scope)
	require.NoError(t, err)
	require.EqualValues(t, 1, generation)
	backend.mu.Lock()
	_, oldCiphertextStillAgesOut := backend.values[oldKey]
	backend.mu.Unlock()
	require.True(t, oldCiphertextStillAgesOut)

	next := service.Begin(context.Background(), request)
	require.Equal(t, DecisionOwner, next.Kind)
	require.NoError(t, service.Commit(context.Background(), next.Reservation, StoreEntry{
		SourceResponse: fixtureResponseBody, Proof: fixtureProof(request.Identity),
	}))
	require.Equal(t, DecisionHit, service.Begin(context.Background(), request).Kind)

	clock.Advance(service.config.TTL + time.Second)
	expired := service.Begin(context.Background(), request)
	require.Equal(t, DecisionOwner, expired.Kind)
	require.NoError(t, service.Release(context.Background(), expired.Reservation))
}

func TestTamperAndCiphertextRelocationNeverServe(t *testing.T) {
	t.Run("tamper", func(t *testing.T) {
		service, backend, _ := newMemoryFixture(t)
		request := fixtureRequest()
		storeFixture(t, service, request)
		entryKey := service.entryKey(request.Identity, request.EffectiveBody, 0)

		backend.mu.Lock()
		value := backend.values[entryKey]
		value.bytes[len(value.bytes)-1] ^= 0xff
		backend.values[entryKey] = value
		backend.mu.Unlock()

		decision := service.Begin(context.Background(), request)
		require.Equal(t, DecisionOwner, decision.Kind)
		require.Equal(t, ReasonIntegrityRecovery, decision.Reason)
		require.NoError(t, service.Release(context.Background(), decision.Reservation))
	})

	t.Run("relocation", func(t *testing.T) {
		service, backend, _ := newMemoryFixture(t)
		source := fixtureRequest()
		storeFixture(t, service, source)
		sourceKey := service.entryKey(source.Identity, source.EffectiveBody, 0)
		target := fixtureRequest()
		target.EffectiveBody = []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"other private prompt"}],"stream":false}`)
		targetKey := service.entryKey(target.Identity, target.EffectiveBody, 0)

		backend.mu.Lock()
		backend.values[targetKey] = backend.values[sourceKey]
		backend.mu.Unlock()

		decision := service.Begin(context.Background(), target)
		require.Equal(t, DecisionOwner, decision.Kind)
		require.Equal(t, ReasonIntegrityRecovery, decision.Reason)
		require.NoError(t, service.Release(context.Background(), decision.Reservation))
	})
}

func TestVersionedEncryptionKeyRotation(t *testing.T) {
	clock := &mutableClock{now: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)}
	backend := NewMemoryBackend(clock.Time)
	keyV1 := bytes.Repeat([]byte{0x31}, 32)
	keyV2 := bytes.Repeat([]byte{0x32}, 32)
	config := fixtureConfig()
	request := fixtureRequest()

	writer := newFixtureService(t, backend, clock.Time, fixtureKeyring(t, "k1", map[string][]byte{"k1": keyV1}), config)
	storeFixture(t, writer, request)

	rotated := newFixtureService(t, backend, clock.Time, fixtureKeyring(t, "k2", map[string][]byte{
		"k1": keyV1, "k2": keyV2,
	}), config)
	require.Equal(t, DecisionHit, rotated.Begin(context.Background(), request).Kind)

	missingOldKey := newFixtureService(t, backend, clock.Time, fixtureKeyring(t, "k2", map[string][]byte{
		"k2": keyV2,
	}), config)
	recovery := missingOldKey.Begin(context.Background(), request)
	require.Equal(t, DecisionOwner, recovery.Kind)
	require.Equal(t, ReasonIntegrityRecovery, recovery.Reason)
	require.NoError(t, missingOldKey.Release(context.Background(), recovery.Reservation))
}

func TestBackendOutageFailsSafe(t *testing.T) {
	service, backend, _ := newMemoryFixture(t)
	request := fixtureRequest()

	backend.SetAvailable(false)
	begin := service.Begin(context.Background(), request)
	require.Equal(t, DecisionBypass, begin.Kind)
	require.Equal(t, ReasonBackendUnavailable, begin.Reason)
	_, err := service.PurgeScope(context.Background(), request.Identity.Scope)
	require.ErrorIs(t, err, ErrBackendUnavailable)

	backend.SetAvailable(true)
	owner := service.Begin(context.Background(), request)
	waiter := service.Begin(context.Background(), request)
	backend.SetAvailable(false)
	require.ErrorIs(t, service.Commit(context.Background(), owner.Reservation, StoreEntry{
		SourceResponse: fixtureResponseBody, Proof: fixtureProof(request.Identity),
	}), ErrBackendUnavailable)
	await := service.Await(context.Background(), waiter.Wait)
	require.Equal(t, DecisionBypass, await.Kind)
	require.Equal(t, ReasonBackendUnavailable, await.Reason)
}

func TestReservationIsServiceLocalCapability(t *testing.T) {
	service, backend, clock := newMemoryFixture(t)
	request := fixtureRequest()
	owner := service.Begin(context.Background(), request)
	other := newFixtureService(
		t,
		backend,
		clock.Time,
		fixtureKeyring(t, "k1", map[string][]byte{"k1": bytes.Repeat([]byte{0x31}, 32)}),
		fixtureConfig(),
	)
	require.ErrorIs(t, other.Commit(context.Background(), owner.Reservation, StoreEntry{
		SourceResponse: fixtureResponseBody, Proof: fixtureProof(request.Identity),
	}), ErrReservationMismatch)
	require.ErrorIs(t, other.Release(context.Background(), owner.Reservation), ErrReservationMismatch)
	require.NoError(t, service.Release(context.Background(), owner.Reservation))
}

type hangingBackend struct{}

func (hangingBackend) wait(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (b hangingBackend) Generation(ctx context.Context, _ string) (uint64, error) {
	return 0, b.wait(ctx)
}
func (b hangingBackend) Read(ctx context.Context, _ string) ([]byte, error) {
	return nil, b.wait(ctx)
}
func (b hangingBackend) Acquire(ctx context.Context, _, _ string, _ time.Duration) (bool, error) {
	return false, b.wait(ctx)
}
func (b hangingBackend) Commit(context.Context, string, string, string, []byte, time.Duration) error {
	return errors.New("not reached")
}
func (b hangingBackend) Release(context.Context, string, string) error {
	return errors.New("not reached")
}
func (b hangingBackend) Invalidate(context.Context, string) error {
	return errors.New("not reached")
}
func (b hangingBackend) Purge(ctx context.Context, _ string) (uint64, error) {
	return 0, b.wait(ctx)
}

func TestBackendOperationsHaveHardTimeout(t *testing.T) {
	config := fixtureConfig()
	config.DependencyTimeout = 10 * time.Millisecond
	service := newFixtureService(
		t,
		hangingBackend{},
		time.Now,
		fixtureKeyring(t, "k1", map[string][]byte{"k1": bytes.Repeat([]byte{0x31}, 32)}),
		config,
	)
	start := time.Now()
	decision := service.Begin(context.Background(), fixtureRequest())
	require.Equal(t, DecisionBypass, decision.Kind)
	require.Equal(t, ReasonBackendUnavailable, decision.Reason)
	require.Less(t, time.Since(start), 250*time.Millisecond)

	start = time.Now()
	_, err := service.PurgeScope(context.Background(), fixtureIdentity().Scope)
	require.Error(t, err)
	require.Less(t, time.Since(start), 250*time.Millisecond)
}

type hangingKeyring struct{}

func (hangingKeyring) Current(ctx context.Context) (string, []byte, error) {
	<-ctx.Done()
	return "", nil, ctx.Err()
}

func (hangingKeyring) ByVersion(ctx context.Context, _ string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestEncryptionKeyringHasHardTimeout(t *testing.T) {
	service, backend, clock := newMemoryFixture(t)
	service = newFixtureService(t, backend, clock.Time, hangingKeyring{}, fixtureConfig())
	request := fixtureRequest()
	owner := service.Begin(context.Background(), request)
	require.Equal(t, DecisionOwner, owner.Kind)

	start := time.Now()
	err := service.Commit(context.Background(), owner.Reservation, StoreEntry{
		SourceResponse: fixtureResponseBody, Proof: fixtureProof(request.Identity),
	})
	require.ErrorIs(t, err, ErrEncryptionKey)
	require.Less(t, time.Since(start), 250*time.Millisecond)
}
