//go:build integration

package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/testutil"
)

// Only terminal payload persistence fails. Stream start, checkpoints, receipts,
// lease ownership and subsequent recovery all use the real Billing and database.
type checkpointPayloadFailure struct {
	*billing.Billing
	last  billing.SettleRequest
	calls int
}

func (b *checkpointPayloadFailure) PersistSettlementUsage(_ context.Context, req billing.SettleRequest) error {
	b.last = req
	b.calls++
	return errors.New("test: terminal usage persistence unavailable")
}

func TestP6F11RelayCheckpointRecoveryUsesDeliveredCounterIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	const requestID, attemptID, model = "op_relay_checkpoint_counter", "att_relay_checkpoint_counter", "checkpoint-counter-model"
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type)
		VALUES ('checkpoint-counter','active','standard') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `INSERT INTO wallets(org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros)
		VALUES ($1,100000000,0,0,0)`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO model_prices(model_name,provider,input_price_cents,output_price_cents)
		VALUES ($1,'test',2,10)`, model)
	require.NoError(t, err)
	actualBilling := billing.NewBilling(pool, nil)
	frozen, err := actualBilling.Freeze(ctx, billing.FreezeRequest{
		RequestID: requestID, OrgID: orgID, Model: model, EstimatedTokens: 1100,
		ReservedCompletionTokens: 1000, SafetyFactor: "1", MinFreezeCents: 10,
	})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.NoError(t, actualBilling.BeginProviderAttempt(ctx, billing.ProviderAttemptStart{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID, APIKeyID: 101,
		ChannelID: 201, ChannelKeyID: 301, RouteSequence: 1, RouteTotal: 1,
		Endpoint: "/v1/chat/completions", Provider: "test-provider", Channel: "test-channel",
	}))
	require.NoError(t, actualBilling.CompleteProviderAttempt(ctx, billing.ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attemptID, OrgID: orgID,
		Outcome: billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: http.StatusOK,
	}))
	fault := &checkpointPayloadFailure{Billing: actualBilling}
	relay := NewRelay(pool, nil, nil, fault, nil, nil)
	installLegacyHandlerTestRuntime(relay)
	frames := checkpointCounterMixedFrames(t)
	body := &countingReadCloser{reader: &readThenError{payload: []byte(strings.Join(frames, ""))}}
	response := &http.Response{StatusCode: http.StatusOK, Body: body}
	route := &RouteResult{Channel: models.Channel{ID: 201, Name: "test-channel", Provider: "openai"}}
	relayCtx := contextWithDurableProviderAttempt(contextWithAttemptID(ctx, attemptID))
	relayCtx = contextWithStreamReservation(relayCtx, 1000, false)
	recorder := httptest.NewRecorder()
	relay.relayStreamRequest(relayCtx, recorder, response, route, requestID, orgID, 101, model, model, "127.0.0.1",
		[]Message{{Role: "user", Content: "hello"}}, logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID)}), time.Now(), 1000, nil, "")
	require.Positive(t, fault.calls)
	require.Contains(t, recorder.Body.String(), "settlement_pending_usage_unpersisted")
	require.NotContains(t, recorder.Body.String(), "data: [DONE]")
	require.Equal(t, 12, fault.last.CompletionTokens)

	var checkpoint billing.StreamDeliveryCheckpoint
	checkpoint.RequestID, checkpoint.OrgID = requestID, orgID
	require.NotNil(t, fault.last.StreamFence)
	checkpoint.OwnerID = fault.last.StreamFence.OwnerID
	require.NoError(t, pool.QueryRow(ctx, `SELECT attempt_id,lease_generation,delivered_bytes,delivered_chunks,
		prompt_tokens,completion_tokens,policy_set_version FROM stream_delivery_checkpoints
		WHERE org_id=$1 AND request_id=$2 ORDER BY sequence DESC LIMIT 1`, orgID, requestID).Scan(
		&checkpoint.AttemptID, &checkpoint.Generation, &checkpoint.DeliveredBytes, &checkpoint.DeliveredChunks,
		&checkpoint.PromptTokens, &checkpoint.CompletionTokens, &checkpoint.PolicySetVersion))
	require.Equal(t, attemptID, checkpoint.AttemptID)
	require.Equal(t, fault.last.StreamFence.Generation, checkpoint.Generation)
	require.Equal(t, 12, checkpoint.CompletionTokens)
	require.Equal(t, fault.last.PromptTokens, checkpoint.PromptTokens)
	require.Equal(t, 3, checkpoint.DeliveredChunks)
	require.Greater(t, checkpoint.DeliveredBytes, int64(49))
	var state string
	var payloadMissing, leaseActive bool
	var inputRate, outputRate int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,settlement_payload_ready_at IS NULL,
		effective_input_price_micros_per_million,effective_output_price_micros_per_million
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&state, &payloadMissing, &inputRate, &outputRate))
	require.Equal(t, "settlement_pending", state)
	require.True(t, payloadMissing)
	require.NoError(t, pool.QueryRow(ctx, `SELECT state='active' AND lease_expires_at>clock_timestamp()
		FROM stream_delivery_leases WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&leaseActive))
	require.True(t, leaseActive)

	// Recovery must use Freeze's prices, even if the mutable price row changes.
	_, err = pool.Exec(ctx, `UPDATE model_prices SET input_price_cents=200,output_price_cents=1000 WHERE model_name=$1`, model)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE stream_delivery_leases SET lease_expires_at=NOW()-INTERVAL '1 second'
		WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.NoError(t, err)
	recovered, err := actualBilling.RecoverExpiredStreamDeliveries(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, billing.StreamRecoveryResult{Claimed: 1, Settled: 1}, recovered)
	charge, err := money.Calculate(
		money.Component{Tokens: int64(checkpoint.PromptTokens), PriceMicrosPerMillion: inputRate},
		money.Component{Tokens: 12, PriceMicrosPerMillion: outputRate})
	require.NoError(t, err)
	var promptTokens, completionTokens int
	var charged int64
	var deliveryAttempt, estimateVersion string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,prompt_tokens,completion_tokens,charged_cost_micros,
		delivery_attempt_id,usage_estimate_version FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(
		&state, &promptTokens, &completionTokens, &charged, &deliveryAttempt, &estimateVersion))
	require.Equal(t, "settled", state)
	require.Equal(t, checkpoint.PromptTokens, promptTokens)
	require.Equal(t, 12, completionTokens)
	require.Equal(t, attemptID, deliveryAttempt)
	require.Equal(t, metering.GatewayCounterEstimateVersionV2, estimateVersion)
	require.Equal(t, charge.TotalMicros, charged)
	require.Positive(t, charged)

	assertOneSettlement := func() {
		t.Helper()
		var settles, terminals, attempts int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions
			WHERE org_id=$1 AND request_id=$2 AND type='settle'`, orgID, requestID).Scan(&settles))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_delivery_receipts
			WHERE org_id=$1 AND request_id=$2 AND outcome IN ('interrupted','completed','write_failed')`, orgID, requestID).Scan(&terminals))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM billing_provider_attempts
			WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&attempts))
		require.Equal(t, 1, settles)
		require.Equal(t, 1, terminals)
		require.Equal(t, 1, attempts)
	}
	assertOneSettlement()
	again, err := actualBilling.RecoverExpiredStreamDeliveries(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, billing.StreamRecoveryResult{}, again)
	assertOneSettlement()
	checkpoint.LeaseTTL = time.Minute
	_, err = actualBilling.CheckpointStreamDelivery(ctx, checkpoint)
	require.ErrorIs(t, err, billing.ErrStreamLeaseConflict)
	require.ErrorIs(t, actualBilling.RenewStreamDelivery(ctx, billing.StreamDeliveryStart{
		RequestID: requestID, AttemptID: attemptID, OwnerID: checkpoint.OwnerID, OrgID: orgID,
		Generation: checkpoint.Generation, LeaseTTL: time.Minute,
	}), billing.ErrStreamLeaseConflict)
}
