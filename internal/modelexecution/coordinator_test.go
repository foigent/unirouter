package modelexecution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

type dispatchFenceTxStarter struct{ beginCalls int }

func (starter *dispatchFenceTxStarter) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	starter.beginCalls++
	return nil, errors.New("dispatch fence database access must not occur")
}

type dispatchFenceLedger struct{ beginCalls int }

func (ledger *dispatchFenceLedger) BeginProviderAttemptOwnedTx(context.Context, pgx.Tx, billing.ProviderAttemptStart, string) (bool, error) {
	ledger.beginCalls++
	return false, errors.New("dispatch fence ledger access must not occur")
}

func (*dispatchFenceLedger) ValidateProviderAttemptOwnerTx(context.Context, pgx.Tx, billing.ProviderAttemptOwnerRequest) error {
	return errors.New("dispatch fence owner validation must not occur")
}

func (*dispatchFenceLedger) FenceProviderAttemptHealthTx(context.Context, pgx.Tx, billing.ProviderAttemptHealthFenceRequest) (billing.ProviderAttemptHealthFenceResult, error) {
	return 0, errors.New("dispatch fence health mutation must not occur")
}

func TestResponsesAndMessagesDispatchFencePrecedesTokenDatabaseAndProviderAttempt(t *testing.T) {
	for _, operationID := range []string{"openai.responses.v1", "anthropic.messages.v1"} {
		t.Run(operationID, func(t *testing.T) {
			plan, route := unitPublicPlan(t, operationID)
			binding, err := modelqualification.CompiledBinding(operationID)
			require.NoError(t, err)
			database := &dispatchFenceTxStarter{}
			ledger := &dispatchFenceLedger{}
			tokenCalls := 0
			coordinator := &Coordinator{
				database: database, ledger: ledger, config: DefaultConfig(),
				newToken: func() (string, error) { tokenCalls++; return "must-not-be-issued", nil },
			}
			_, err = coordinator.Authorize(t.Context(), plan, route, AttemptIdentity{
				RequestID: "request-fenced", AttemptID: "attempt-fenced", OrgID: 1, APIKeyID: 2,
				RouteSequence: 1, RouteTotal: 1, Endpoint: binding.PublicPath,
			})
			require.ErrorIs(t, err, ErrDispatchNotWired)
			require.Zero(t, tokenCalls)
			require.Zero(t, database.beginCalls)
			require.Zero(t, ledger.beginCalls)
		})
	}
}

func TestAuthorizePublicRejectsNonM77BeforeTokenDatabaseAndProviderAttempt(t *testing.T) {
	starter := &dispatchFenceTxStarter{}
	ledger := &dispatchFenceLedger{}
	tokenCalls := 0
	coordinator := &Coordinator{
		database: starter,
		ledger:   ledger,
		config:   DefaultConfig(),
		newToken: func() (string, error) {
			tokenCalls++
			return "must-not-be-created", nil
		},
	}
	_, err := coordinator.AuthorizePublic(t.Context(), modelrouting.Plan{
		ModelOperationID: "openai.chat_completions.v1",
	}, modelrouting.Route{}, AttemptIdentity{})
	if !errors.Is(err, ErrInvalid) || tokenCalls != 0 || starter.beginCalls != 0 || ledger.beginCalls != 0 {
		t.Fatalf("non-M77 public authorize crossed fence: err=%v token=%d db=%d ledger=%d",
			err, tokenCalls, starter.beginCalls, ledger.beginCalls)
	}
}

func TestAuthorizePublicRejectsEmbeddedProfileDriftBeforeTokenDatabaseAndProviderAttempt(t *testing.T) {
	plan, route := unitPublicPlan(t, "openai.responses.v1")
	public := *route.PublicEvidence
	public.ProtocolProfileID = "wrong.public.profile"
	public.ProtocolProfileVersion = "v99"
	route.PublicEvidence = &public
	plan.Routes = []modelrouting.Route{route}

	starter := &dispatchFenceTxStarter{}
	ledger := &dispatchFenceLedger{}
	tokenCalls := 0
	coordinator := &Coordinator{
		database: starter,
		ledger:   ledger,
		config:   DefaultConfig(),
		newToken: func() (string, error) {
			tokenCalls++
			return "must-not-be-created", nil
		},
	}
	_, err := coordinator.AuthorizePublic(t.Context(), plan, route, AttemptIdentity{
		RequestID: "request-profile-drift", AttemptID: "attempt-profile-drift", OrgID: 1, APIKeyID: 2,
		RouteSequence: 1, RouteTotal: 1, Endpoint: route.PublicPath,
	})
	if !errors.Is(err, ErrInvalid) || tokenCalls != 0 || starter.beginCalls != 0 || ledger.beginCalls != 0 {
		t.Fatalf("embedded profile drift crossed fence: err=%v token=%d db=%d ledger=%d",
			err, tokenCalls, starter.beginCalls, ledger.beginCalls)
	}
}

func TestPreparePublicRequestKeepsV2ThroughAttemptAndDecision(t *testing.T) {
	plan, route := unitPublicPlan(t, "openai.responses.v1")
	identity := AttemptIdentity{
		RequestID: "request-public", AttemptID: "attempt-public", OrgID: 1, APIKeyID: 2,
		RouteSequence: 1, RouteTotal: 1, Endpoint: route.PublicPath,
	}
	prepared, err := preparePublicRequest(plan, route, identity)
	require.NoError(t, err)
	coordinator := &Coordinator{newToken: func() (string, error) { return "unit-public-dispatch-owner", nil }}
	prepared, err = coordinator.prepareDispatchCapabilities(prepared)
	require.NoError(t, err)
	require.NotNil(t, prepared.dispatchPublicEvidence)
	wantDigest, err := executionevidence.DigestV2(*prepared.dispatchPublicEvidence)
	require.NoError(t, err)
	require.Equal(t, wantDigest, prepared.dispatchEvidenceHash)

	start := providerAttemptStart(prepared)
	require.NotNil(t, start.PublicRouteEvidence)
	require.Nil(t, start.RouteEvidence)
	require.Zero(t, start.ChannelID)
	require.Zero(t, start.ChannelKeyID)
	compact, err := executionevidence.CompactV2(*start.PublicRouteEvidence)
	require.NoError(t, err)
	require.Equal(t, wantDigest, compact.EvidenceHash)
	require.Equal(t, route.PublicEvidence.PublicExecutionBindingID, compact.PublicExecutionBindingID)

	decision := prepared.appliedDecision(time.Now().UTC())
	require.NotNil(t, decision.PublicEvidence)
	require.Equal(t, executionevidence.EvidenceV1{}, decision.Evidence)
	require.Equal(t, wantDigest, decision.EvidenceHash)

	mixed := route
	mixed.Evidence = prepared.evidence
	plan.Routes = []modelrouting.Route{mixed}
	_, err = preparePublicRequest(plan, mixed, identity)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestPrepareHealthRequestUsesPublicEvidenceV2Digest(t *testing.T) {
	plan, route := unitPublicPlan(t, "anthropic.messages.v1")
	identity := AttemptIdentity{
		RequestID: "request-public-health", AttemptID: "attempt-public-health", OrgID: 1, APIKeyID: 2,
		RouteSequence: 1, RouteTotal: 1, Endpoint: route.PublicPath,
	}
	prepared, err := preparePublicRequest(plan, route, identity)
	require.NoError(t, err)
	coordinator := &Coordinator{newToken: func() (string, error) { return "unit-public-health-owner", nil }}
	prepared, err = coordinator.prepareDispatchCapabilities(prepared)
	require.NoError(t, err)
	decision := prepared.appliedDecision(time.Now().UTC())

	request, err := prepareHealthRequest(decision, actionSuccess)
	require.NoError(t, err)
	require.Equal(t, decision.EvidenceHash, request.evidenceHash)
	require.Equal(t, prepared.dispatchEvidence, request.evidence)

	mutated := decision
	public := *mutated.PublicEvidence
	public.PublicExecutionBindingID++
	mutated.PublicEvidence = &public
	_, err = prepareHealthRequest(mutated, actionSuccess)
	require.ErrorIs(t, err, ErrInvalid)

	mixed := decision
	mixed.Evidence = prepared.dispatchEvidence
	_, err = prepareHealthRequest(mixed, actionSuccess)
	require.ErrorIs(t, err, ErrInvalid)

	_, err = executionevidence.Digest(prepared.dispatchEvidence)
	require.Error(t, err, "public operation must not have a V1 downgrade digest")
	downgraded := decision
	downgraded.EvidenceHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	_, err = prepareHealthRequest(downgraded, actionSuccess)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestBoundedTransactionKeepsCommitRecoverySeparateFromSerializationRetry(t *testing.T) {
	tests := []struct {
		name       string
		directives []transactionRetry
		wantCalls  int
		wantErr    bool
	}{
		{name: "commit ambiguity recovers once", directives: []transactionRetry{retryCommitAmbiguity, retryNone}, wantCalls: 2},
		{name: "second commit ambiguity is returned", directives: []transactionRetry{retryCommitAmbiguity, retryCommitAmbiguity}, wantCalls: 2, wantErr: true},
		{name: "serialization retries once", directives: []transactionRetry{retrySerialization, retryNone}, wantCalls: 2},
		{name: "second serialization is returned", directives: []transactionRetry{retrySerialization, retrySerialization}, wantCalls: 2, wantErr: true},
		{name: "serialization then ambiguous commit uses both budgets", directives: []transactionRetry{retrySerialization, retryCommitAmbiguity, retryNone}, wantCalls: 3},
		{name: "ambiguous commit recovery may use remaining serialization retry", directives: []transactionRetry{retryCommitAmbiguity, retrySerialization, retryNone}, wantCalls: 3},
		{name: "ordinary failure never retries", directives: []transactionRetry{retryNone, retryNone}, wantCalls: 1, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			result, err := runBoundedTransaction(t.Context(), func(context.Context) (int, transactionRetry, error) {
				directive := test.directives[calls]
				calls++
				if directive == retryNone && calls == len(test.directives) && !test.wantErr {
					return calls, retryNone, nil
				}
				return calls, directive, errors.New("transaction failed")
			})
			require.Equal(t, test.wantCalls, calls)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.wantCalls, result)
			}
		})
	}
}

func TestCommitRecoveryUsesAFreshBoundedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	result, err := runBoundedTransaction(ctx, func(operationCtx context.Context) (int, transactionRetry, error) {
		calls++
		if calls == 1 {
			cancel()
			return 0, retryCommitAmbiguity, errors.New("ambiguous commit")
		}
		require.NoError(t, operationCtx.Err(), "commit recovery must survive caller cancellation")
		deadline, ok := operationCtx.Deadline()
		require.True(t, ok, "commit recovery must remain time bounded")
		require.WithinDuration(t, time.Now().Add(commitRecoveryTimeout), deadline, time.Second)
		return 2, retryNone, nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, result)
	require.Equal(t, 2, calls)
}

func TestExecutionDurationsAreDerivedFromTheRoutingPolicy(t *testing.T) {
	policy := routingpolicy.Default()
	config, err := ConfigFromRoutingPolicy(policy)
	require.NoError(t, err)
	require.Equal(t, time.Duration(policy.HalfOpenProbeLeaseSeconds)*time.Second, config.HalfOpenLease)
	require.Equal(t, time.Duration(policy.CircuitBreakerRecoverSeconds)*time.Second, config.BreakerRecovery)
	require.Equal(t, time.Duration(policy.AuthFailureCooldownSeconds)*time.Second, config.AuthFailureCooldown)
	require.Equal(t, time.Duration(policy.RateLimitCooldownSeconds)*time.Second, config.RateLimitCooldown)
}

func TestPrepareRequestRequiresCompleteAccessCredentialIdentity(t *testing.T) {
	plan, route := unitCanonicalPlan(t)
	identity := AttemptIdentity{
		RequestID: "request-1", AttemptID: "attempt-1", OrgID: 1, APIKeyID: 2,
		AccessCredentialID: 3, RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions",
	}
	_, err := prepareRequest(plan, route, identity)
	require.ErrorIs(t, err, ErrInvalid)
	identity.CredentialRef = "cred_abcdefgh"
	identity.AccessGrantID = 4
	prepared, err := prepareRequest(plan, route, identity)
	require.NoError(t, err)
	prepared.dispatchEvidence = prepared.evidence
	start := providerAttemptStart(prepared)
	require.EqualValues(t, 3, start.AccessCredentialID)
	require.Equal(t, "cred_abcdefgh", start.CredentialRef)
	require.EqualValues(t, 4, start.AccessGrantID)
	require.Zero(t, start.ChannelID, "canonical attempts must not invent legacy identities")
}

func TestPrepareRequestRequiresRevisionEquivalentVerifyEvidence(t *testing.T) {
	plan, route := unitCanonicalPlan(t)
	route.Evidence.AuthorityMode = executionevidence.ModeVerify
	route.Evidence.LegacyChannelID = 9
	route.Evidence.LegacyChannelKeyID = 10
	route.Evidence.LegacyRoutingRevision = route.Evidence.DeploymentRevision
	route.Evidence.LegacyCircuitRevision = route.Evidence.CircuitRevision
	route.Evidence.LegacyCredentialStateRevision = route.Evidence.CredentialStateRevision
	plan.AuthorityMode = executionevidence.ModeVerify
	plan.Routes = []modelrouting.Route{route}
	identity := AttemptIdentity{
		RequestID: "request-verify", AttemptID: "attempt-verify", OrgID: 1, APIKeyID: 2,
		RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions",
	}

	_, err := prepareRequest(plan, route, identity)
	require.NoError(t, err)

	route.Evidence.ConnectionRevision++
	plan.Routes = []modelrouting.Route{route}
	_, err = prepareRequest(plan, route, identity)
	require.ErrorIs(t, err, ErrInvalid)
	require.ErrorContains(t, err, "revision-equivalent")
}

func TestBreakerEligibilityClassifiesStaleAndLeaseContention(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Second)
	future := now.Add(time.Second)
	require.Equal(t, Applied, breakerEligibility("healthy", nil, nil, "", now, false))
	require.Equal(t, Stale, breakerEligibility("half_open", &past, nil, "", now, false))
	require.Equal(t, Applied, breakerEligibility("half_open", &past, nil, "", now, true))
	require.Equal(t, Stale, breakerEligibility("half_open", &future, nil, "", now, true))
	require.Equal(t, LeaseLost, breakerEligibility("half_open", &past, &future, "owner", now, true))
}

func TestLegacyCandidateOverrideRequiresItsExactNormalizedHash(t *testing.T) {
	raw := []byte(`{"contract_version":"unirouter.runtime-change-candidate.v1","model_name":"public-model","overrides":[{"channel_id":7,"expected_routing_epoch":3,"admission":"allow"}]}`)
	// The normalized representation is already canonical in this fixture.
	hash := "sha256:" + "a"
	_, valid := legacyCandidateOverride(raw, hash, "public-model", 7)
	require.False(t, valid)
	// Use the package helper to avoid asserting a second hashing algorithm here.
	correct := legacyCandidateHashForTest(t, raw)
	override, valid := legacyCandidateOverride(raw, correct, "public-model", 7)
	require.True(t, valid)
	require.EqualValues(t, 3, override.ExpectedRoutingEpoch)
}

func TestPrepareHealthRequestAcceptsOnlyTheImmutableAppliedDecision(t *testing.T) {
	_, route := unitCanonicalPlan(t)
	evidence, err := executionevidence.NormalizeForDispatch(route.Evidence)
	require.NoError(t, err)
	digest, err := executionevidence.Digest(evidence)
	require.NoError(t, err)
	decision := Decision{
		Status: Applied, Evidence: evidence, EvidenceHash: digest,
		DispatchOwnerToken: "unit-dispatch-owner",
		ProviderAttemptKey: ProviderAttemptKey{OrgID: 1, RequestID: "unit-request", AttemptID: "unit-attempt"},
	}

	request, err := prepareHealthRequest(decision, actionSuccess)
	require.NoError(t, err)
	require.Equal(t, evidence, request.evidence)
	_, err = prepareHealthRequest(decision, actionReleaseUndispatched)
	require.NoError(t, err, "every owned undispatched receipt must be terminalizable")
	_, err = prepareHealthRequest(decision, actionRenewLease)
	require.ErrorIs(t, err, ErrInvalid, "only a half-open route has a renewable lease")

	mutated := decision
	mutated.Evidence.DeploymentRevision++
	_, err = prepareHealthRequest(mutated, actionSuccess)
	require.ErrorIs(t, err, ErrInvalid)

	notApplied := decision
	notApplied.Status = Stale
	_, err = prepareHealthRequest(notApplied, actionSuccess)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestPrepareHealthRequestRequiresTheExactRawProbeCapability(t *testing.T) {
	_, route := unitCanonicalPlan(t)
	token := "a-valid-ephemeral-probe-token"
	evidence := route.Evidence
	evidence.RequiresProbe = true
	evidence.ProbeLeaseTokenHash = probeTokenHash(token)
	evidence, err := executionevidence.NormalizeForDispatch(evidence)
	require.NoError(t, err)
	digest, err := executionevidence.Digest(evidence)
	require.NoError(t, err)
	decision := Decision{
		Status: Applied, Evidence: evidence, EvidenceHash: digest,
		ProbeLeaseToken: token, DispatchOwnerToken: "unit-dispatch-owner",
		ProviderAttemptKey: ProviderAttemptKey{OrgID: 1, RequestID: "unit-request", AttemptID: "unit-attempt"},
	}

	_, err = prepareHealthRequest(decision, actionRenewLease)
	require.NoError(t, err)
	decision.ProbeLeaseToken = "another-token"
	_, err = prepareHealthRequest(decision, actionRenewLease)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestDecisionJSONNeverExposesRawDispatchCapabilities(t *testing.T) {
	payload, err := json.Marshal(Decision{
		Status:             Applied,
		ProbeLeaseToken:    "raw-probe-capability",
		DispatchOwnerToken: "raw-dispatch-owner-capability",
	})
	require.NoError(t, err)
	require.NotContains(t, string(payload), "raw-probe-capability")
	require.NotContains(t, string(payload), "raw-dispatch-owner-capability")
}

func legacyCandidateHashForTest(t *testing.T, raw []byte) string {
	t.Helper()
	var payload struct {
		ContractVersion string `json:"contract_version"`
		ModelName       string `json:"model_name"`
		Overrides       []struct {
			ChannelID            int64  `json:"channel_id"`
			ExpectedRoutingEpoch int64  `json:"expected_routing_epoch"`
			Admission            string `json:"admission"`
			Priority             *int   `json:"priority,omitempty"`
			Weight               *int   `json:"weight,omitempty"`
		} `json:"overrides"`
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func unitCanonicalPlan(t *testing.T) (modelrouting.Plan, modelrouting.Route) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	evidence := executionevidence.EvidenceV1{
		ContractVersion: executionevidence.ContractVersion, AuthorityMode: executionevidence.ModeCanonical,
		ExecutionContractSource: executionevidence.ContractSourceCanonicalProfile,
		PublicModelID:           1, DeploymentID: 2, ConnectionID: 3, CredentialBindingID: 4,
		ConnectionRevision: 1, DeploymentRevision: 1, CircuitRevision: 1,
		CredentialStateRevision: 1, CredentialLinkedAt: now,
		ModelOperationID: "openai.chat_completions.v1", OperationRevision: 1,
		ProtocolProfileID: "openai.chat_completions", ProtocolProfileVersion: "1.0.0",
		OperationEvidenceState: "production_approved", OperationEvidenceRef: "test:operation", OperationVerifiedAt: &now,
		CapabilityEvidence: []executionevidence.CapabilityEntry{}, CapabilityEvidenceHash: mustCapabilityHash(t, nil),
		CredentialProbeID: 5, CredentialProbeTestedAt: now,
		RuntimeReleaseID: 6, RuntimeReleaseRef: "rcr_unit_canonical", RuntimeStage: "active",
		SlotVersion: 1, SnapshotHash: "mrs1_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	route := modelrouting.Route{
		ConnectionName: "connection", Provider: "custom", BaseURL: "https://provider.invalid",
		AdapterType: "openai_compatible", AdapterConfig: []byte(`{}`),
		ProtocolProfileID: evidence.ProtocolProfileID, ProtocolProfileVersion: evidence.ProtocolProfileVersion,
		UpstreamModel: "upstream", MaxContext: 128000, DefaultMaxOutput: 8192,
		EncryptedSecret: "ciphertext", Priority: 10, Weight: 100, Evidence: evidence,
	}
	plan := modelrouting.Plan{
		AuthorityMode: executionevidence.ModeCanonical, PublicModelID: 1, PublicModel: "public-model",
		ModelOperationID: evidence.ModelOperationID, PublicContextLimit: 128000, PublicOutputLimit: 8192,
		SnapshotHash: evidence.SnapshotHash, RuntimeReleaseID: evidence.RuntimeReleaseID,
		RuntimeReleaseRef: evidence.RuntimeReleaseRef, RuntimeStage: evidence.RuntimeStage,
		SlotVersion: evidence.SlotVersion, Routes: []modelrouting.Route{route},
	}
	return plan, route
}

func unitPublicPlan(t *testing.T, operationID string) (modelrouting.Plan, modelrouting.Route) {
	t.Helper()
	plan, route := unitCanonicalPlan(t)
	binding, err := modelqualification.CompiledPublicBinding(operationID)
	require.NoError(t, err)
	base := route.Evidence
	base.ContractVersion = executionevidence.ContractVersionV2
	base.ModelOperationID = operationID
	base.ProtocolProfileID = binding.ProfileID
	base.ProtocolProfileVersion = binding.ProfileVersion
	public := executionevidence.EvidenceV2{
		EvidenceV1:                 base,
		PublicExecutionBindingID:   77,
		DeliveryContractVersion:    binding.DeliveryContractVersion,
		ChargeContractVersion:      binding.ChargeContractVersion,
		IdempotencyContractVersion: binding.IdempotencyContractVersion,
	}
	public, err = executionevidence.NormalizeV2(public)
	require.NoError(t, err)
	route.Evidence = executionevidence.EvidenceV1{}
	route.PublicEvidence = &public
	route.ProtocolProfileID = binding.ProfileID
	route.ProtocolProfileVersion = binding.ProfileVersion
	route.PublicPath = binding.PublicPath
	route.ExecutorContractVersion = binding.ExecutorContractVersion
	route.ProductionApprovalID = 91
	plan.ModelOperationID = operationID
	plan.Routes = []modelrouting.Route{route}
	return plan, route
}

func mustCapabilityHash(t *testing.T, input []executionevidence.CapabilityEntry) string {
	t.Helper()
	value, err := executionevidence.CapabilityDigest(input)
	require.NoError(t, err)
	return value
}
