//go:build integration

package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/capbilling"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/policybilling"
	"github.com/uniroute/unirouter/internal/testutil"
)

// The terminal path uses the real policy/Agent-cap wrappers and PostgreSQL,
// including stream checkpoints, final evidence, recovery and reconciliation.
// Supplier frames are fixtures: this test makes no Provider request.
func TestProviderCompletionRelayHoldSurvivesRecoveryIntegration(t *testing.T) {
	cases := []struct {
		family string
		mode   billing.BillingMode
	}{
		{"qwen", billing.BillingModePrepaidEnforced}, {"qwen", billing.BillingModeCostObservation},
		{"deepseek", billing.BillingModePrepaidEnforced}, {"deepseek", billing.BillingModeCostObservation},
	}
	for _, tc := range cases {
		family, mode := tc.family, tc.mode
		t.Run(family+"/"+string(mode), func(t *testing.T) {
			reserved, reported := 20, 21
			if family == "deepseek" {
				reserved, reported = 2048, 2049
			}
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			const requestID, attemptID, model = "op_completion_hold", "att_completion_hold", "completion-hold-model"
			var orgID int64
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES ('completion-hold','active','standard') RETURNING id`).Scan(&orgID))
			_, err := pool.Exec(ctx, `INSERT INTO wallets(org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros) VALUES ($1,100000000,0,0,0)`, orgID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO model_prices(model_name,provider,input_price_cents,output_price_cents) VALUES ($1,'test',2,10)`, model)
			require.NoError(t, err)
			actual := billing.NewBilling(pool, nil).WithMode(mode)
			frozen, err := actual.Freeze(ctx, billing.FreezeRequest{RequestID: requestID, OrgID: orgID, Model: model, EstimatedTokens: 100 + reserved, ReservedCompletionTokens: reserved, SafetyFactor: "1", MinFreezeCents: 10})
			require.NoError(t, err)
			require.True(t, frozen.Success)
			require.Equal(t, mode, frozen.BillingMode)
			require.NoError(t, actual.BeginProviderAttempt(ctx, billing.ProviderAttemptStart{RequestID: requestID, AttemptID: attemptID, OrgID: orgID, APIKeyID: 101, ChannelID: 201, ChannelKeyID: 301, RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions", Provider: map[string]string{"qwen": "dashscope", "deepseek": "deepseek"}[family], Channel: "fixture"}))
			require.NoError(t, actual.CompleteProviderAttempt(ctx, billing.ProviderAttemptCompletion{RequestID: requestID, AttemptID: attemptID, OrgID: orgID, Outcome: billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200}))
			terminalChain := policybilling.New(capbilling.New(actual, nil), nil)
			relay := NewRelay(pool, nil, nil, terminalChain, nil, nil)
			installLegacyHandlerTestRuntime(relay)
			p, ok := modelprofile.Lookup("qwen.3.7-flash.api", "1.7.0")
			require.True(t, ok)
			p.ProtocolProfile = "protocol.qwen-openai-chat.flash.v8"
			if family == "deepseek" {
				p = deepSeekCompletionDefinition(t).Profile()
			}
			wire := existingFamilyRelayFixture(t, p, "tools")
			if family == "deepseek" {
				wire = deepSeekCompletionFixture(t)
			}
			wire = strings.ReplaceAll(strings.ReplaceAll(wire, `"completion_tokens":20`, `"completion_tokens":`+strconv.Itoa(reported)), `"total_tokens":120`, `"total_tokens":`+strconv.Itoa(100+reported))
			route := &RouteResult{MappedModel: p.UpstreamModelIDs[0], Channel: models.Channel{ID: 201, Provider: p.Provider}, CacheUsageProfile: p.CacheUsageProfile, ModelProtocolProfile: p.ProtocolProfile, PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{CapabilityID: modelprofile.CapabilityToolChoiceAuto, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}}}}}
			if family == "deepseek" {
				route.ProfileDefinition = deepSeekCompletionDefinition(t)
				route.ExecutionProfileID, route.ExecutionProfileVersion = p.ID, p.Version
				route.ModelOperationID = modelprofile.OperationOpenAIChat
				route.OperationProtocolProfileID, route.OperationProtocolProfileVersion = p.ProtocolProfile, modelprofile.ChatOperationProtocolBindingVersionV1
				route.ReasoningProfile = p.ReasoningProfile
			}
			relayCtx := contextWithStreamReservation(contextWithDurableProviderAttempt(contextWithAttemptID(ctx, attemptID)), reserved, false)
			rec := httptest.NewRecorder()
			relay.relayStreamRequest(relayCtx, rec, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(wire))}, route, requestID, orgID, 101, model, model, "127.0.0.1", nil, logger.New(logger.Fields{RequestID: requestID}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
			require.Contains(t, rec.Body.String(), "stream_error.provider_output_limit_exceeded")
			require.Contains(t, rec.Body.String(), "provider_cost_pending")
			require.NotContains(t, rec.Body.String(), "provider_usage_hold_unpersisted")
			require.NotContains(t, rec.Body.String(), "data: [DONE]")
			var status, reason, purpose, authority, finality, leaseState string
			var completion, receipts, attempts, terminals int
			var spent int64
			require.NoError(t, pool.QueryRow(ctx, `SELECT status,settlement_pending_reason,completion_tokens,usage_purpose,usage_authority,usage_finality FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&status, &reason, &completion, &purpose, &authority, &finality))
			require.Equal(t, "settlement_pending", status)
			require.Equal(t, billing.OutputHoldReasonProviderResponseUnusable, reason)
			require.Equal(t, reported, completion)
			require.Equal(t, "provider_evidence", purpose)
			require.Equal(t, "provider_reported", authority)
			require.Equal(t, "final", finality)
			require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM stream_delivery_leases WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&leaseState))
			require.Equal(t, billing.StreamLeaseStateInterrupted, leaseState)
			_, err = pool.Exec(ctx, `UPDATE stream_delivery_leases SET lease_expires_at=NOW()-INTERVAL '1 hour' WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
			require.NoError(t, err)
			streams, err := actual.RecoverExpiredStreamDeliveries(ctx, 10)
			require.NoError(t, err)
			require.Zero(t, streams.Claimed)
			settlements, err := actual.RecoverPendingSettlements(ctx, 10)
			require.NoError(t, err)
			require.Zero(t, settlements.Claimed)
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_delivery_receipts WHERE org_id=$1 AND request_id=$2 AND error_code='provider_output_limit_exceeded' AND delivered_bytes>0 AND delivered_chunks>0`, orgID, requestID).Scan(&receipts))
			require.Equal(t, 1, receipts)
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND state='delivery_failed' AND outcome_certainty='known_usage' AND NOT retry_authorized`, orgID, requestID).Scan(&attempts))
			require.Equal(t, 1, attempts)
			require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND request_id=$2 AND type='settle')+(SELECT COUNT(*) FROM billing_observation_receipts WHERE org_id=$1 AND request_id=$2)`, orgID, requestID).Scan(&terminals))
			require.Zero(t, terminals)
			require.NoError(t, pool.QueryRow(ctx, `SELECT total_spent_micros FROM wallets WHERE org_id=$1`, orgID).Scan(&spent))
			require.Zero(t, spent)
			reconciliation := billing.ProviderReconciliationRequest{OrgID: orgID, RequestID: requestID, AttemptID: attemptID, ActorUserID: 77, Reason: "reviewed supplier output overrun", EvidenceRef: "fixture://known-overrun", Action: billing.ProviderReconciliationCollect}
			_, err = actual.ReconcileProviderAttempt(ctx, reconciliation)
			require.ErrorIs(t, err, billing.ErrProviderReconciliationUnsafe)
			reconciliation.Action = billing.ProviderReconciliationRefund
			result, err := actual.ReconcileProviderAttempt(ctx, reconciliation)
			require.NoError(t, err)
			require.Equal(t, "0", result.CustomerChargedMicros)
			replay, err := actual.ReconcileProviderAttempt(ctx, reconciliation)
			require.NoError(t, err)
			require.Equal(t, result, replay)
			if mode == billing.BillingModeCostObservation {
				require.Positive(t, result.TerminalObservationReceiptID)
				require.Zero(t, result.TerminalTransactionID)
			}
		})
	}
}
