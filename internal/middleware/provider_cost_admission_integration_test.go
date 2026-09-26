//go:build integration

package middleware

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/metering"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F40CAffectedKeyPaidAdmissionBlockClearsOnlyAfterResolution(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var orgID, apiKeyID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','standard') RETURNING id`,
		fmt.Sprintf("p6f40c-auth-%d", time.Now().UnixNano())).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO api_keys (org_id,name,key_prefix,key_hash,status) VALUES ($1,'blocked-key','ur-test','hash','active') RETURNING id`,
		orgID).Scan(&apiKeyID))
	_, err := pool.Exec(ctx, `INSERT INTO wallets (org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros) VALUES ($1,10000000,0,0,0)`, orgID)
	require.NoError(t, err)
	model := fmt.Sprintf("p6f40c-auth-model-%d", time.Now().UnixNano())
	_, err = pool.Exec(ctx, `INSERT INTO model_prices (model_name,provider,input_price_cents,output_price_cents) VALUES ($1,'test',2,10)`, model)
	require.NoError(t, err)
	b := billing.NewBilling(pool, nil)
	requestID := "op_p6f40c_auth"
	freeze, err := b.Freeze(ctx, billing.FreezeRequest{RequestID: requestID, OrgID: orgID, Model: model, EstimatedTokens: 1, SafetyFactor: "1", MinFreezeCents: 100})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	attempt := billing.ProviderAttemptStart{
		RequestID: requestID, AttemptID: "att_p6f40c_auth", OrgID: orgID, APIKeyID: apiKeyID,
		ChannelID: 1, ChannelKeyID: 1, RouteSequence: 1, RouteTotal: 1,
		Endpoint: "/v1/chat/completions", Provider: "test", Channel: "test",
	}
	require.NoError(t, b.BeginProviderAttempt(ctx, attempt))
	blocked, err := providerCostAdmissionBlocked(ctx, pool, orgID, apiKeyID, 0)
	require.NoError(t, err)
	require.False(t, blocked, "an ordinary in-flight dispatch must not serialize the entire API key")
	require.NoError(t, b.CompleteProviderAttempt(ctx, billing.ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: billing.ProviderAttemptOutcomeUnknown, FailureKind: "transport",
	}))
	blocked, err = providerCostAdmissionBlocked(ctx, pool, orgID, apiKeyID, 0)
	require.NoError(t, err)
	require.True(t, blocked)
	_, err = b.ReconcileProviderAttempt(ctx, billing.ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: billing.ProviderReconciliationRefund, ActorUserID: 77,
		Reason: "provider confirmed no customer delivery", EvidenceRef: "case://refund-auth",
	})
	require.NoError(t, err)
	blocked, err = providerCostAdmissionBlocked(ctx, pool, orgID, apiKeyID, 0)
	require.NoError(t, err)
	require.False(t, blocked)
}

// These fixtures use the existing Billing writers and migrated schema. Tokens
// are synthetic, local-only credentials; no provider or Redis is contacted.
type pendingAdmissionFixture struct {
	pool          *pgxpool.Pool
	billing       *billing.Billing
	org, key      int64
	model, rawKey string
}

func newPendingAdmissionFixture(t *testing.T, mode billing.BillingMode) pendingAdmissionFixture {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	f := pendingAdmissionFixture{pool: pool, billing: billing.NewBilling(pool, nil).WithMode(mode)}
	f.org = testutil.NewTestOrg(t, pool)
	f.rawKey = fmt.Sprintf("ur-%04d-00000000000000000000000000000001", f.org)
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys (org_id,name,key_prefix,key_hash,status)
	 VALUES ($1,'pending-admission','ur-synthetic',$2,'active') RETURNING id`,
		f.org, auth.HashAPIKey(f.rawKey, "test-secret")).Scan(&f.key))
	if mode == billing.BillingModePrepaidEnforced {
		_, err := pool.Exec(ctx, `INSERT INTO wallets (org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros)
		 VALUES ($1,10000000,0,0,0)`, f.org)
		require.NoError(t, err)
	}
	f.model = fmt.Sprintf("pending-admission-%d", f.org)
	_, err := pool.Exec(ctx, `INSERT INTO model_prices (model_name,provider,input_price_cents,output_price_cents)
	 VALUES ($1,'test',2,10)`, f.model)
	require.NoError(t, err)
	return f
}

func (f pendingAdmissionFixture) accepted(t *testing.T, requestID string, legacy bool) billing.SettleRequest {
	t.Helper()
	key := f.key
	if legacy {
		key = 0
	}
	freezeRequest := billing.FreezeRequest{
		RequestID: requestID, OrgID: f.org, APIKeyID: key, Model: f.model,
		EstimatedTokens: 1, SafetyFactor: "1", MinFreezeCents: 10,
	}
	if !legacy {
		require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT COALESCE(project_id,0),COALESCE(access_credential_id,0),COALESCE(credential_ref,''),COALESCE(access_grant_id,0)
		 FROM api_keys WHERE id=$1`, key).Scan(&freezeRequest.ProjectID, &freezeRequest.AccessCredentialID, &freezeRequest.CredentialRef, &freezeRequest.AccessGrantID))
	}
	frozen, err := f.billing.Freeze(t.Context(), freezeRequest)
	require.NoError(t, err)
	require.True(t, frozen.Success)
	attempt := "att_" + requestID
	require.NoError(t, f.billing.BeginProviderAttempt(t.Context(), billing.ProviderAttemptStart{
		RequestID: requestID, AttemptID: attempt, OrgID: f.org, APIKeyID: f.key,
		ChannelID: 1, ChannelKeyID: 1, RouteSequence: 1, RouteTotal: 1,
		Endpoint: "/v1/chat/completions", Provider: "test", Channel: "test",
	}))
	require.NoError(t, f.billing.CompleteProviderAttempt(t.Context(), billing.ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt, OrgID: f.org,
		Outcome: billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	return billing.SettleRequest{
		RequestID: requestID, AttemptID: attempt, OrgID: f.org,
		PromptTokens: 20, UncachedPromptTokens: 20, CompletionTokens: 5,
		UsageSource: metering.UsageSourceUpstream, UsageAuthority: metering.UsageAuthorityProviderReported,
		UsagePurpose: metering.UsagePurposeCustomerCharge, UsageFinality: metering.UsageFinalityFinal,
		UsageContractVersion: metering.UsageContractVersion, CacheUsageStatus: "not_reported",
	}
}

func pendingAdmissionRequest(ctx context.Context, rawKey string) *http.Request {
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", nil)
	r.Header.Set("Authorization", "Bearer "+rawKey)
	return r
}

func TestPendingAdmissionIndependentRequestsAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   billing.BillingMode
		legacy bool
	}{
		{"prepaid", billing.BillingModePrepaidEnforced, false},
		{"prepaid_grant", billing.BillingModePrepaidEnforced, false},
		{"known_zero", billing.BillingModePrepaidEnforced, false},
		{"legacy_null_key", billing.BillingModePrepaidEnforced, true},
		{"observation_without_wallet", billing.BillingModeCostObservation, false},
		{"observation_unpriced", billing.BillingModeCostObservation, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPendingAdmissionFixture(t, tc.mode)
			if tc.name == "observation_unpriced" {
				f.model += "-unpriced"
			}
			if tc.name == "prepaid_grant" {
				testutil.NewTestGrantCents(t, f.pool, f.org, 10, nil)
				_, err := f.pool.Exec(t.Context(), `UPDATE wallets SET granted_balance_micros=100000 WHERE org_id=$1`, f.org)
				require.NoError(t, err)
			}
			a := f.accepted(t, "op_barrier_a", tc.legacy)
			if tc.name == "known_zero" {
				a.PromptTokens, a.UncachedPromptTokens, a.CompletionTokens = 0, 0, 0
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			steps := make(chan func(context.Context) error)
			results := make(chan error, 1)
			done := make(chan struct{})
			aResponse := httptest.NewRecorder()
			aHandler := GatewayAuth(f.pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for {
					select {
					case <-r.Context().Done():
						return
					case step := <-steps:
						results <- step(r.Context())
					}
				}
			}))
			go func() { defer close(done); aHandler.ServeHTTP(aResponse, pendingAdmissionRequest(ctx, f.rawKey)) }()
			t.Cleanup(func() { cancel(); <-done })
			for i, outcome := range []string{"prepared", billing.DeliveryOutcomeWriteStarted, billing.DeliveryOutcomeCompleted} {
				step := func(ctx context.Context) error {
					if outcome == "prepared" {
						p, err := f.billing.PrepareNonStreamDelivery(ctx, a)
						if err == nil && !p.Funded {
							return fmt.Errorf("A preparation not funded")
						}
						return err
					}
					var bytes int64
					var chunks int
					if outcome == billing.DeliveryOutcomeCompleted {
						bytes, chunks = 128, 1
					}
					return f.billing.RecordDelivery(ctx, billing.DeliveryEvent{
						RequestID: a.RequestID, AttemptID: a.AttemptID, OrgID: f.org,
						Mode: billing.DeliveryModeNonStream, Outcome: outcome,
						DeliveredBytes: bytes, DeliveredChunks: chunks, HTTPStatus: 200,
					})
				}
				select {
				case steps <- step:
				case <-done:
					t.Fatalf("A rejected before barrier: %s", aResponse.Body.String())
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				select {
				case err := <-results:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				require.ErrorIs(t, f.billing.Cancel(ctx, a.RequestID, f.org), billing.ErrSettlementPending)
				require.Error(t, f.billing.BeginProviderAttempt(ctx, billing.ProviderAttemptStart{
					RequestID: a.RequestID, AttemptID: "att_duplicate", OrgID: f.org, APIKeyID: f.key,
					ChannelID: 1, ChannelKeyID: 1, RouteSequence: 1, RouteTotal: 1,
					Endpoint: "/v1/chat/completions", Provider: "test", Channel: "test",
				}))
				if outcome != billing.DeliveryOutcomeCompleted {
					_, err := f.billing.Settle(ctx, a)
					require.ErrorIs(t, err, billing.ErrDeliveryNotChargeable)
				}
				bID := fmt.Sprintf("op_barrier_b_%d", i)
				var bFreeze *billing.FreezeResult
				var bErr error
				bResponse := httptest.NewRecorder()
				GatewayAuth(f.pool, "test-secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					bFreeze, bErr = f.billing.Freeze(r.Context(), billing.FreezeRequest{
						RequestID: bID, OrgID: f.org, APIKeyID: f.key, Model: f.model,
						EstimatedTokens: 1, SafetyFactor: "1", MinFreezeCents: 10,
					})
					w.WriteHeader(http.StatusOK)
				})).ServeHTTP(bResponse, pendingAdmissionRequest(ctx, f.rawKey))
				require.Equal(t, http.StatusOK, bResponse.Code, "%s: %s", outcome, bResponse.Body.String())
				require.NoError(t, bErr)
				require.NotNil(t, bFreeze)
				require.True(t, bFreeze.Success)
				require.NoError(t, f.billing.Cancel(ctx, bID, f.org))
				if tc.mode == billing.BillingModePrepaidEnforced {
					var spent, held, reserved int64
					require.NoError(t, f.pool.QueryRow(ctx, `SELECT w.total_spent_micros,w.frozen_micros,f.frozen_amount_micros
					 FROM wallets w JOIN freeze_idempotency f ON f.org_id=w.org_id WHERE f.org_id=$1 AND f.request_id=$2`, f.org, a.RequestID).Scan(&spent, &held, &reserved))
					require.Zero(t, spent)
					require.Equal(t, reserved, held, "B must not consume A's hold")
				}
			}
			cancel()
			<-done // A exits without live settlement; a fresh instance recovers durable facts.
			recovered, err := billing.NewBilling(f.pool, nil).WithMode(tc.mode).RecoverPendingSettlements(t.Context(), 10)
			require.NoError(t, err)
			require.Equal(t, 1, recovered.Settled)
			settled, err := f.billing.Settle(t.Context(), a)
			require.NoError(t, err)
			require.True(t, settled.Success)
			if tc.mode == billing.BillingModeCostObservation {
				require.Zero(t, settled.ChargedCostMicros)
			}
			blocked, err := providerCostAdmissionBlocked(t.Context(), f.pool, f.org, f.key, 0)
			require.NoError(t, err)
			require.False(t, blocked)
		})
	}
}

func TestPendingAdmissionDurableEvidenceFailsClosed(t *testing.T) {
	f := newPendingAdmissionFixture(t, billing.BillingModePrepaidEnforced)
	a := f.accepted(t, "op_evidence", false)
	prepared, err := f.billing.PrepareNonStreamDelivery(t.Context(), a)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	for _, tc := range []struct {
		name, mutation string
		blocked        bool
	}{
		{"complete_evidence", "", false},
		{"missing_payload", "settlement_payload_ready_at=NULL", true},
		{"unknown_usage", "usage_authority='none',usage_purpose='provider_evidence',usage_finality='missing',usage_estimate_version=''", true},
		{"estimated_usage", "usage_source='estimated',usage_authority='conservative_estimate',usage_finality='estimated',usage_estimate_version='unirouter.token-ceiling.v1'", false},
		{"counter_v2", "usage_source='counter_v2',usage_authority='gateway_counter',usage_finality='estimated',usage_estimate_version='unirouter.stream-counter.v2'", false},
		{"counter_v3", "usage_source='counter_v3',usage_authority='gateway_counter',usage_finality='estimated',usage_estimate_version='unirouter.stream-counter.v3'", false},
		{"explicit_marker", "provider_cost_pending_at=NOW()", true},
		{"funding_reason", "settlement_pending_reason='insufficient_topup',provider_cost_pending_at=NOW()", true},
		{"provider_reason", "settlement_pending_reason='provider_attempt_unresolved',provider_cost_pending_at=NOW()", true},
		{"uncovered_cost", "uncovered_cost_micros=1", true},
		{"insufficient_reserve", "calculated_cost_micros=100001", true},
		{"bigint_cash_boundary", "calculated_cost_micros=9223372036854775807,reserved_balance_micros=9223372036854775807,reserved_granted_micros=0,frozen_amount_micros=9223372036854775807", false},
		{"bigint_grant_boundary", "calculated_cost_micros=9223372036854775807,reserved_balance_micros=0,reserved_granted_micros=9223372036854775807,frozen_amount_micros=9223372036854775807", false},
		{"bigint_one_micro_shortfall", "calculated_cost_micros=9223372036854775807,reserved_balance_micros=9223372036854775806,reserved_granted_micros=0,frozen_amount_micros=9223372036854775806", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := f.pool.Begin(t.Context())
			require.NoError(t, err)
			defer tx.Rollback(t.Context())
			if tc.mutation != "" {
				_, err = tx.Exec(t.Context(), `UPDATE freeze_idempotency SET `+tc.mutation+` WHERE org_id=$1 AND request_id=$2`, f.org, a.RequestID)
				require.NoError(t, err)
			}
			blocked, err := providerCostAdmissionBlocked(t.Context(), tx, f.org, f.key, 0)
			require.NoError(t, err)
			require.Equal(t, tc.blocked, blocked)
		})
	}
	// A second candidate must not borrow A's exact bound attempt, even if it
	// shares its operation, key and known-usage classification.
	tx, err := f.pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	_, err = tx.Exec(t.Context(), `INSERT INTO billing_provider_attempts
	 (org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty,retry_authorized)
	 SELECT org_id,request_id,'att_other',api_key_id,channel_id,channel_key_id,route_sequence,route_total,endpoint,provider,channel_name,'delivery_pending','known_usage',FALSE
	 FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, f.org, a.RequestID, a.AttemptID)
	require.NoError(t, err)
	blocked, err := providerCostAdmissionBlocked(t.Context(), tx, f.org, f.key, 0)
	require.NoError(t, err)
	require.True(t, blocked)
}

func TestPendingAdmissionExplicitRisksStillBlockMixedScope(t *testing.T) {
	f := newPendingAdmissionFixture(t, billing.BillingModePrepaidEnforced)
	a := f.accepted(t, "op_safe", false)
	prepared, err := f.billing.PrepareNonStreamDelivery(t.Context(), a)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	risk := f.accepted(t, "op_risk", false)
	for _, state := range []string{"outcome_unknown", "response_unusable", "funding_pending", "delivery_failed", "output_withheld", "delivery_pending"} {
		t.Run(state, func(t *testing.T) {
			tx, err := f.pool.Begin(t.Context())
			require.NoError(t, err)
			defer tx.Rollback(t.Context())
			certainty := "known_usage"
			if state == "outcome_unknown" || state == "response_unusable" {
				certainty = "unknown"
			}
			_, err = tx.Exec(t.Context(), `UPDATE billing_provider_attempts SET state=$3,outcome_certainty=$4 WHERE org_id=$1 AND request_id=$2`, f.org, risk.RequestID, state, certainty)
			require.NoError(t, err)
			blocked, err := providerCostAdmissionBlocked(t.Context(), tx, f.org, f.key, 0)
			require.NoError(t, err)
			require.True(t, blocked, "safe A must not hide risky B")
			blocked, err = providerCostAdmissionBlocked(t.Context(), tx, f.org, f.key+1, 0)
			require.NoError(t, err)
			require.False(t, blocked)
			blocked, err = providerCostAdmissionBlocked(t.Context(), tx, f.org+1, f.key, 0)
			require.NoError(t, err)
			require.False(t, blocked)
		})
	}
}

func TestPendingAdmissionStreamRequiresDurableFundedUsage(t *testing.T) {
	for _, mode := range []billing.BillingMode{billing.BillingModePrepaidEnforced, billing.BillingModeCostObservation} {
		t.Run(string(mode), func(t *testing.T) {
			f := newPendingAdmissionFixture(t, mode)
			a := f.accepted(t, "op_stream", false)
			start := billing.StreamDeliveryStart{RequestID: a.RequestID, AttemptID: a.AttemptID, OrgID: f.org, OwnerID: "synthetic-owner", Generation: 1, LeaseTTL: time.Minute}
			fence := start.Fence()
			a.StreamFence = &fence
			require.NoError(t, f.billing.BeginStreamDelivery(t.Context(), start))
			require.NoError(t, f.billing.RecordDelivery(t.Context(), billing.DeliveryEvent{
				RequestID: a.RequestID, AttemptID: a.AttemptID, OrgID: f.org,
				Mode: billing.DeliveryModeStream, Outcome: billing.DeliveryOutcomeCompleted,
				DeliveredBytes: 128, DeliveredChunks: 1, HTTPStatus: 200, StreamFence: &fence,
			}))
			blocked, err := providerCostAdmissionBlocked(t.Context(), f.pool, f.org, f.key, 0)
			require.NoError(t, err)
			require.True(t, blocked, "terminal receipt alone does not prove known funded usage")
			a.PromptTokens, a.UncachedPromptTokens, a.CompletionTokens = 0, 0, 2000
			require.NoError(t, f.billing.PersistSettlementUsage(t.Context(), a))
			blocked, err = providerCostAdmissionBlocked(t.Context(), f.pool, f.org, f.key, 0)
			require.NoError(t, err)
			require.Equal(t, mode == billing.BillingModePrepaidEnforced, blocked, "20 cents exceeds the 10-cent prepaid hold; observation needs no wallet")
			_, err = f.pool.Exec(t.Context(), `UPDATE freeze_idempotency SET provider_cost_pending_at=NOW() WHERE org_id=$1 AND request_id=$2`, f.org, a.RequestID)
			require.NoError(t, err)
			blocked, err = providerCostAdmissionBlocked(t.Context(), f.pool, f.org, f.key, 0)
			require.NoError(t, err)
			require.True(t, blocked)
		})
	}
}

func TestPendingAdmissionPreparationFundingFailureStillBlocks(t *testing.T) {
	f := newPendingAdmissionFixture(t, billing.BillingModePrepaidEnforced)
	a := f.accepted(t, "op_unfunded", false)
	a.PromptTokens, a.UncachedPromptTokens, a.CompletionTokens = 0, 0, 1000000000
	prepared, err := f.billing.PrepareNonStreamDelivery(t.Context(), a)
	require.NoError(t, err)
	require.False(t, prepared.Funded)
	blocked, err := providerCostAdmissionBlocked(t.Context(), f.pool, f.org, f.key, 0)
	require.NoError(t, err)
	require.True(t, blocked)
}

func TestPendingAdmissionLogicalCredentialRotationKeepsRiskScope(t *testing.T) {
	f := newPendingAdmissionFixture(t, billing.BillingModePrepaidEnforced)
	ctx := t.Context()
	var user, project, request, grant, credential, rotatedKey int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
	 VALUES ($1,'pending-owner','unused','org_admin','org_admin','active') RETURNING id`, f.org).Scan(&user))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status)
	 VALUES ($1,'Pending project','pending-project','active') RETURNING id`, f.org).Scan(&project))
	_, err := f.pool.Exec(ctx, `INSERT INTO project_members(project_id,user_id,role) VALUES($1,$2,'project_user')`, project, user)
	require.NoError(t, err)
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO access_requests(
	 request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,subject_user_id,
	 request_kind,requested_project_role,requested_scope_mode,requested_models,requested_endpoints,
	 environment,submission_mode,status,decision_mode,policy_reason_codes,decision_reason_codes,
	 policy_version,submitted_by,decided_by,decided_at)
	 VALUES('req_pending_synthetic','pending-synthetic',repeat('a',64),$1,$2,'user',$3,
	 'human_personal','project_user','explicit',jsonb_build_array($4::text),'["/v1/chat/completions"]',
	 'personal','direct_grant','approved','direct',ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
	 'unirouter.enterprise-access-policy.v1',$3,$3,NOW()) RETURNING id`, f.org, project, user, f.model).Scan(&request))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO access_grants(
	 grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,project_role_snapshot,
	 scope_mode,allowed_models,allowed_endpoints,environment,status,valid_from,decision_mode,
	 policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,created_by,updated_by)
	 VALUES('grant_pending_synthetic',$1,$2,$3,'user',$4,'project_user','explicit',jsonb_build_array($5::text),'["/v1/chat/completions"]',
	 'personal','active',NOW(),'direct',ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
	 'unirouter.enterprise-access-policy.v1',repeat('a',64),$4,$4) RETURNING id`, request, f.org, project, user, f.model).Scan(&grant))
	_, err = f.pool.Exec(ctx, `UPDATE api_keys SET allowed_models=$2,allowed_endpoints='/v1/chat/completions' WHERE id=$1`, f.key, f.model)
	require.NoError(t, err)
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO access_credentials(
	 credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,allowed_models,allowed_endpoints,
	 policy_labels,rpm_limit,rps_limit,concurrency_limit,legacy_budget_limit_cents,created_by)
	 SELECT 'cred_pending_synthetic',$1,org_id,$2,name,COALESCE(allowed_ips,''),COALESCE(allowed_models,''),COALESCE(allowed_endpoints,''),
	 COALESCE(policy_labels,''),rpm_limit,qps_limit,concurrency_limit,budget_limit,$3 FROM api_keys WHERE id=$4 RETURNING id`, grant, project, user, f.key).Scan(&credential))
	_, err = f.pool.Exec(ctx, `UPDATE api_keys SET project_id=$1,owner_user_id=$2,key_type='personal',access_grant_id=$3,
	 access_credential_id=$4,credential_ref='cred_pending_synthetic' WHERE id=$5`, project, user, grant, credential, f.key)
	require.NoError(t, err)
	a := f.accepted(t, "op_rotated", false)
	prepared, err := f.billing.PrepareNonStreamDelivery(ctx, a)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	// Existing M59 rotation contract: disable predecessor, then issue the same
	// logical scope. The pending attempt remains attributed to the old key.
	tx, err := f.pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE api_keys SET status='disabled' WHERE id=$1`, f.key)
	require.NoError(t, err)
	require.NoError(t, tx.QueryRow(ctx, `INSERT INTO api_keys(
	 org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
	 allowed_ips,allowed_models,allowed_endpoints,policy_labels,rpm_limit,qps_limit,concurrency_limit,budget_limit,purpose,
	 access_grant_id,access_credential_id,credential_ref,rotated_from_key_id)
	 SELECT org_id,project_id,owner_user_id,key_type,name,repeat('b',64),'ur-rotated-synthetic','active',
	 allowed_ips,allowed_models,allowed_endpoints,policy_labels,rpm_limit,qps_limit,concurrency_limit,budget_limit,purpose,
	 access_grant_id,access_credential_id,credential_ref,id FROM api_keys WHERE id=$1 RETURNING id`, f.key).Scan(&rotatedKey))
	require.NoError(t, tx.Commit(ctx))
	blocked, err := providerCostAdmissionBlocked(ctx, f.pool, f.org, rotatedKey, credential)
	require.NoError(t, err)
	require.False(t, blocked)
	// A valid successor key is still the wrong physical attribution for A.
	tx, err = f.pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE freeze_idempotency SET api_key_id=$3 WHERE org_id=$1 AND request_id=$2`, f.org, a.RequestID, rotatedKey)
	require.NoError(t, err)
	blocked, err = providerCostAdmissionBlocked(ctx, tx, f.org, rotatedKey, credential)
	require.NoError(t, err)
	require.True(t, blocked)
	require.NoError(t, tx.Rollback(ctx))
	_, err = f.pool.Exec(ctx, `UPDATE freeze_idempotency SET provider_cost_pending_at=NOW() WHERE org_id=$1 AND request_id=$2`, f.org, a.RequestID)
	require.NoError(t, err)
	blocked, err = providerCostAdmissionBlocked(ctx, f.pool, f.org, rotatedKey, credential)
	require.NoError(t, err)
	require.True(t, blocked, "rotation cannot escape the original credential's risk")
	blocked, err = providerCostAdmissionBlocked(ctx, f.pool, f.org, rotatedKey, credential+1)
	require.NoError(t, err)
	require.False(t, blocked)
}
