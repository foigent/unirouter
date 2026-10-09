package billing

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestStreamCounterVersionPinRecoveryAndReplay(t *testing.T) {
	cases := []struct {
		version              string
		mode                 BillingMode
		reserved, completion int
	}{
		{metering.GatewayCounterEstimateVersionV2, BillingModePrepaidEnforced, 100, 2},
		{metering.GatewayCounterEstimateVersionV3, BillingModePrepaidEnforced, 100, 2},
		{metering.GatewayCounterEstimateVersionV3, BillingModePrepaidEnforced, 2048, 3090},
		{metering.GatewayCounterEstimateVersionV3, BillingModeCostObservation, 2048, 3090},
	}
	for _, tc := range cases {
		version := tc.version
		t.Run(fmt.Sprintf("%s/%s/reserved%d", version, tc.mode, tc.reserved), func(t *testing.T) {
			pool, org, model := newReservationFixture(t, 10_000, 0)
			b := NewBilling(pool, nil).WithMode(tc.mode)
			service, err := outbox.New(pool, outbox.DefaultOptions())
			require.NoError(t, err)
			require.NoError(t, service.RegisterHandler(outbox.EventBillingUsagePrepared, b.ProcessUsageJournalEvent))
			require.NoError(t, service.RegisterHandler(outbox.EventBillingSettled, b.ProcessUsageJournalEvent))
			b.SetOutbox(service)
			start := StreamDeliveryStart{RequestID: "op_counter_pin", AttemptID: "att_counter_pin", OwnerID: "counter-owner", OrgID: org, Generation: 1, LeaseTTL: time.Minute, CounterVersion: version}
			freezeStreamReservation(t, b, org, model, start.RequestID, tc.reserved)
			beginAcceptedStreamAttempt(t, b, org, start.RequestID, start.AttemptID)
			require.NoError(t, b.BeginStreamDelivery(t.Context(), start))
			require.NoError(t, b.BeginStreamDelivery(t.Context(), start))
			other := start
			other.CounterVersion = metering.GatewayCounterEstimateVersionV2
			if version == other.CounterVersion {
				other.CounterVersion = metering.GatewayCounterEstimateVersionV3
			}
			require.ErrorIs(t, b.BeginStreamDelivery(t.Context(), other), ErrStreamLeaseConflict)
			require.ErrorIs(t, b.RenewStreamDelivery(t.Context(), other), ErrStreamLeaseConflict)
			cp := StreamDeliveryCheckpoint{RequestID: start.RequestID, AttemptID: start.AttemptID, OwnerID: start.OwnerID, OrgID: org, Generation: 1, DeliveredBytes: 512, DeliveredChunks: 3, PromptTokens: 20, CompletionTokens: tc.completion, PolicySetVersion: "counter-test", LeaseTTL: time.Minute, CounterVersion: version}
			_, err = b.CheckpointStreamDelivery(t.Context(), cp)
			require.NoError(t, err)
			replay, err := b.CheckpointStreamDelivery(t.Context(), cp)
			require.NoError(t, err)
			require.True(t, replay.Idempotent)
			wrong := cp
			wrong.CounterVersion = other.CounterVersion
			_, err = b.CheckpointStreamDelivery(t.Context(), wrong)
			require.ErrorIs(t, err, ErrStreamLeaseConflict)
			_, err = pool.Exec(t.Context(), `UPDATE stream_delivery_leases SET counter_version=$1 WHERE org_id=$2 AND request_id=$3`, other.CounterVersion, org, start.RequestID)
			require.ErrorContains(t, err, "immutable")
			source, err := metering.CounterSource(other.CounterVersion)
			require.NoError(t, err)
			conflict := SettleRequest{RequestID: start.RequestID, AttemptID: start.AttemptID, OrgID: org, StreamFence: testStreamFence(start), PromptTokens: 20, UncachedPromptTokens: 20, CompletionTokens: tc.completion, UsageSource: source}
			require.ErrorIs(t, b.PersistSettlementUsage(t.Context(), conflict), ErrSettlementUsageConflict)
			_, err = pool.Exec(t.Context(), `UPDATE stream_delivery_leases SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE org_id=$1 AND request_id=$2`, org, start.RequestID)
			require.NoError(t, err)
			b.SetRecoveryAdmission(func() bool { return false })
			result, err := b.RecoverExpiredStreamDeliveries(t.Context(), 1)
			require.NoError(t, err)
			require.Zero(t, result.Claimed)
			b.SetRecoveryAdmission(func() bool { return true })
			result, err = b.RecoverExpiredStreamDeliveries(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, result.Settled)
			result, err = b.RecoverExpiredStreamDeliveries(t.Context(), 1)
			require.NoError(t, err)
			require.Zero(t, result.Claimed)
			var storedSource, storedVersion, status string
			var completion, journals int
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT usage_source,usage_estimate_version,status,completion_tokens FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, org, start.RequestID).Scan(&storedSource, &storedVersion, &status, &completion))
			source, _ = metering.CounterSource(version)
			require.Equal(t, source, storedSource)
			require.Equal(t, version, storedVersion)
			require.Equal(t, "settled", status)
			require.Equal(t, tc.completion, completion)
			_, err = service.DrainOnce(t.Context())
			require.NoError(t, err)
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM usage_finalization_journal WHERE org_id=$1 AND request_id=$2 AND event_type='settled'`, org, start.RequestID).Scan(&journals))
			require.Equal(t, 1, journals)
			// Version agreement is checked even after the immutable terminal state exists.
			conflict.StreamFence = nil
			_, err = b.Settle(t.Context(), conflict)
			require.ErrorIs(t, err, ErrSettlementUsageConflict)
			conflict.UsageSource = source
			settled, err := b.Settle(t.Context(), conflict)
			require.NoError(t, err)
			require.True(t, settled.Success)
			require.Equal(t, tc.mode, settled.BillingMode)
			if tc.mode == BillingModeCostObservation {
				require.Zero(t, settled.ChargedCostMicros)
				require.Positive(t, settled.ObservationReceiptID)
				require.Zero(t, settled.SettleID)
			}
			var blocked int
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2 AND state IN ('outcome_unknown','response_unusable','funding_pending','delivery_failed','output_withheld','delivery_pending')`, org, start.RequestID).Scan(&blocked))
			require.Zero(t, blocked, "ordinary interrupted estimates add no provider-pending admission block")
			require.ErrorIs(t, b.RenewStreamDelivery(t.Context(), start), ErrStreamLeaseConflict)
		})
	}
}

func TestStreamCounterBridgeRefreshesM114WithoutRestart(t *testing.T) {
	pool, org, model := newReservationFixture(t, 10_000, 0)
	dir := testutil.MigrationsDir(t)
	require.NoError(t, testutil.RewindEmptySourceFactsForLegacyTest(t.Context(), pool, dir))
	down, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".down.sql"))
	require.NoError(t, err)
	// Isolated predecessor fixture: no v3 rows, restore the actual M114 schema.
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	body := strings.ReplaceAll(strings.ReplaceAll(string(down), "BEGIN;", ""), "COMMIT;", "")
	_, err = tx.Exec(t.Context(), body)
	require.NoError(t, err)
	_, err = tx.Exec(t.Context(), `DELETE FROM schema_migrations WHERE version=$1`, schemaauthority.StreamCounterExpandHead)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	b := NewBilling(pool, nil) // The same instance is retained across the expansion.
	old := StreamDeliveryStart{RequestID: "op_pre_expand", AttemptID: "att_pre_expand", OwnerID: "old-owner", OrgID: org, Generation: 1, LeaseTTL: time.Minute}
	freezeStreamReservation(t, b, org, model, old.RequestID, 100)
	beginAcceptedStreamAttempt(t, b, org, old.RequestID, old.AttemptID)
	bad := old
	bad.CounterVersion = metering.GatewayCounterEstimateVersionV3
	require.ErrorIs(t, b.BeginStreamDelivery(t.Context(), bad), ErrStreamLeaseConflict)
	require.NoError(t, b.BeginStreamDelivery(t.Context(), old))
	cp := StreamDeliveryCheckpoint{RequestID: old.RequestID, AttemptID: old.AttemptID, OwnerID: old.OwnerID, OrgID: org, Generation: 1, DeliveredBytes: 100, DeliveredChunks: 5, PromptTokens: 20, CompletionTokens: 5, PolicySetVersion: "old", LeaseTTL: time.Minute}
	_, err = b.CheckpointStreamDelivery(t.Context(), cp)
	require.NoError(t, err)
	var before string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT row_to_json(c)::text FROM stream_delivery_checkpoints c WHERE org_id=$1 AND request_id=$2`, org, old.RequestID).Scan(&before))
	up, err := os.ReadFile(filepath.Join(dir, schemaauthority.StreamCounterExpandHead+".up.sql"))
	require.NoError(t, err)
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, migrate.CompatibleExpandHooks{Version: schemaauthority.StreamCounterExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(up)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return nil }}))
	var after, version string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT row_to_json(c)::text FROM stream_delivery_checkpoints c WHERE org_id=$1 AND request_id=$2`, org, old.RequestID).Scan(&after))
	require.Equal(t, before, after)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT counter_version FROM stream_delivery_leases WHERE org_id=$1 AND request_id=$2`, org, old.RequestID).Scan(&version))
	require.Equal(t, metering.GatewayCounterEstimateVersionV2, version)
	newer := old
	newer.RequestID = "op_post_expand"
	newer.AttemptID = "att_post_expand"
	newer.CounterVersion = metering.GatewayCounterEstimateVersionV3
	freezeStreamReservation(t, b, org, model, newer.RequestID, 100)
	beginAcceptedStreamAttempt(t, b, org, newer.RequestID, newer.AttemptID)
	require.NoError(t, b.BeginStreamDelivery(t.Context(), newer))
	cp.RequestID = newer.RequestID
	cp.AttemptID = newer.AttemptID
	cp.CounterVersion = newer.CounterVersion
	cp.CompletionTokens = 2
	_, err = b.CheckpointStreamDelivery(t.Context(), cp)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE stream_delivery_leases SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE org_id=$1`, org)
	require.NoError(t, err)
	result, err := b.RecoverExpiredStreamDeliveries(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, result.Settled)
	var sum int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT sum(completion_tokens) FROM freeze_idempotency WHERE org_id=$1`, org).Scan(&sum))
	require.Equal(t, 7, sum)
	tx, err = pool.Begin(t.Context())
	require.NoError(t, err)
	_, err = tx.Exec(t.Context(), body)
	require.ErrorContains(t, err, "v3")
	require.NoError(t, tx.Rollback(t.Context()))
}
