//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestV1DeliveryAttemptBindingMigrationBackfillsOnlyUniqueEvidence(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000039_v1_guardrail_tenant_isolation.up.sql")
	ctx := context.Background()
	allMigrations := testutil.MigrationsDir(t)
	throughBinding := migrationsThrough(t, allMigrations, "000040_v1_delivery_attempt_binding.up.sql")

	insertReservation := func(requestID string) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO freeze_idempotency (
				request_id,org_id,frozen_amount_micros,reserved_balance_micros,
				status,settlement_pending_reason,output_started_at
			) VALUES ($1,7101,100,100,'settlement_pending','billable_output',NOW())`, requestID)
		require.NoError(t, err)
	}
	insertAttempt := func(requestID, attemptID string) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO billing_provider_attempts (
				org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
				route_sequence,route_total,endpoint,provider,channel_name,
				state,outcome_certainty
			) VALUES (7101,$1,$2,1,2,3,1,1,'/v1/chat/completions','test','test-channel',
				'response_accepted','pending')`, requestID, attemptID)
		require.NoError(t, err)
	}
	insertDeliveredReceipt := func(requestID string) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO request_delivery_receipts (
				org_id,request_id,sequence,mode,outcome,delivered_bytes,delivered_chunks,http_status
			) VALUES
				(7101,$1,1,'nonstream','write_started',0,0,200),
				(7101,$1,2,'nonstream','completed',64,1,200)`, requestID)
		require.NoError(t, err)
	}

	insertReservation("delivery-binding-unique")
	insertAttempt("delivery-binding-unique", "attempt-unique")
	insertDeliveredReceipt("delivery-binding-unique")

	insertReservation("delivery-binding-ambiguous")
	insertAttempt("delivery-binding-ambiguous", "attempt-a")
	insertAttempt("delivery-binding-ambiguous", "attempt-b")
	insertDeliveredReceipt("delivery-binding-ambiguous")

	require.NoError(t, migrate.Run(ctx, pool, throughBinding))

	var binding string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT delivery_attempt_id FROM freeze_idempotency
		WHERE org_id=7101 AND request_id='delivery-binding-unique'`).Scan(&binding))
	require.Equal(t, "attempt-unique", binding)
	var uniqueReceipts int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM request_delivery_receipts
		WHERE org_id=7101 AND request_id='delivery-binding-unique' AND attempt_id='attempt-unique'`).Scan(&uniqueReceipts))
	require.Equal(t, 2, uniqueReceipts)

	var reason string
	var quarantinedReceipts, liveAmbiguousReceipts, quarantinedReservations int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT settlement_pending_reason FROM freeze_idempotency
		WHERE org_id=7101 AND request_id='delivery-binding-ambiguous'`).Scan(&reason))
	require.Equal(t, "ambiguous_attempt", reason)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM request_delivery_receipt_quarantine
		WHERE org_id=7101 AND request_id='delivery-binding-ambiguous'`).Scan(&quarantinedReceipts))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM request_delivery_receipts
		WHERE org_id=7101 AND request_id='delivery-binding-ambiguous'`).Scan(&liveAmbiguousReceipts))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM delivery_attempt_quarantine
		WHERE org_id=7101 AND request_id='delivery-binding-ambiguous'
		  AND candidate_attempt_ids=ARRAY['attempt-a','attempt-b']::TEXT[]`).Scan(&quarantinedReservations))
	require.Equal(t, 2, quarantinedReceipts)
	require.Zero(t, liveAmbiguousReceipts, "ambiguous historical evidence must never remain charge-authorizing")
	require.Equal(t, 1, quarantinedReservations)

	_, err := pool.Exec(ctx, `UPDATE freeze_idempotency SET delivery_attempt_id='attempt-a'
		WHERE org_id=7101 AND request_id='delivery-binding-unique'`)
	require.ErrorContains(t, err, "immutable")
	insertAttempt("delivery-binding-unique", "attempt-other")
	_, err = pool.Exec(ctx, `
		INSERT INTO request_delivery_receipts (
			org_id,request_id,attempt_id,sequence,mode,outcome,delivered_bytes,delivered_chunks,http_status
		) VALUES (7101,'delivery-binding-unique','attempt-other',3,'nonstream','completed',64,1,200)`)
	require.Error(t, err, "cross-attempt delivery proof must be rejected by the database")
	_, err = pool.Exec(ctx, `
		INSERT INTO request_delivery_receipts (
			org_id,request_id,attempt_id,sequence,mode,outcome,delivered_bytes,delivered_chunks,http_status
		) VALUES (7101,'delivery-binding-unique','',3,'nonstream','completed',64,1,200)`)
	require.Error(t, err, "empty attempt identity must be rejected")

	downSQL, err := os.ReadFile(filepath.Join(allMigrations, "000040_v1_delivery_attempt_binding.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "C1.2 rollback refused")
}

func TestV1DeliveryAttemptBindingMigrationUnusedDownAndRollForward(t *testing.T) {
	pool, migrationsDir := setupMigratedThrough(t, "000040_v1_delivery_attempt_binding.up.sql")
	ctx := context.Background()
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000040_v1_delivery_attempt_binding.down.sql"))
	require.NoError(t, err)
	require.NoError(t, func() error {
		_, execErr := pool.Exec(ctx, string(downSQL))
		return execErr
	}())
	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	var ledger, bindingColumn, receiptColumn int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations
		WHERE version='000040_v1_delivery_attempt_binding'`).Scan(&ledger))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='freeze_idempotency' AND column_name='delivery_attempt_id'`).Scan(&bindingColumn))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='request_delivery_receipts' AND column_name='attempt_id'`).Scan(&receiptColumn))
	require.Equal(t, 1, ledger)
	require.Equal(t, 1, bindingColumn)
	require.Equal(t, 1, receiptColumn)
}

func TestV1DeliveryAttemptBindingCutoverSerializesOldWriter(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000039_v1_guardrail_tenant_isolation.up.sql")
	ctx := context.Background()
	const requestID = "delivery-attempt-cutover-race"
	const attemptID = "attempt-cutover-race"
	_, err := pool.Exec(ctx, `INSERT INTO freeze_idempotency (
		request_id,org_id,frozen_amount_micros,reserved_balance_micros,status
	) VALUES ($1,7501,100,100,'frozen')`, requestID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts (
		org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
		route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty
	) VALUES (7501,$1,$2,1,2,3,1,1,'/v1/chat/completions','test','test-channel',
		'response_accepted','pending')`, requestID, attemptID)
	require.NoError(t, err)

	// Hold the second table altered by 000040. The migration must retain its
	// leading freeze AccessExclusive lock while it waits, closing the mixed-
	// schema writer window until the receipt attempt constraint is active.
	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer blocker.Rollback(ctx)
	_, err = blocker.Exec(ctx, `LOCK TABLE request_delivery_receipts IN ACCESS SHARE MODE`)
	require.NoError(t, err)
	throughBinding := migrationsThrough(t, testutil.MigrationsDir(t), "000040_v1_delivery_attempt_binding.up.sql")
	migrationErr := make(chan error, 1)
	go func() {
		migrationErr <- migrate.Run(context.Background(), pool, throughBinding)
	}()

	waitForLock := func(pid *int32, mode string, granted bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			var present bool
			query := `SELECT EXISTS (
				SELECT 1 FROM pg_locks l JOIN pg_class c ON c.oid=l.relation
				WHERE c.relname='freeze_idempotency' AND l.mode=$1 AND l.granted=$2`
			args := []any{mode, granted}
			if pid != nil {
				query += ` AND l.pid=$3`
				args = append(args, *pid)
			}
			query += `)`
			require.NoError(t, pool.QueryRow(ctx, query, args...).Scan(&present))
			if present {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for freeze lock mode=%s granted=%t pid=%v", mode, granted, pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitForLock(nil, "AccessExclusiveLock", true)

	writerPID := make(chan int32, 1)
	writerErr := make(chan error, 1)
	go func() {
		writerCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tx, beginErr := pool.Begin(writerCtx)
		if beginErr != nil {
			writerErr <- beginErr
			return
		}
		defer tx.Rollback(writerCtx)
		var pid int32
		if queryErr := tx.QueryRow(writerCtx, `SELECT pg_backend_pid()`).Scan(&pid); queryErr != nil {
			writerErr <- queryErr
			return
		}
		writerPID <- pid
		var status string
		if queryErr := tx.QueryRow(writerCtx, `SELECT status FROM freeze_idempotency
			WHERE org_id=7501 AND request_id=$1 FOR UPDATE`, requestID).Scan(&status); queryErr != nil {
			writerErr <- queryErr
			return
		}
		if _, execErr := tx.Exec(writerCtx, `UPDATE freeze_idempotency
			SET status='settlement_pending',settlement_pending_reason='billable_output',output_started_at=NOW()
			WHERE org_id=7501 AND request_id=$1 AND status='frozen'`, requestID); execErr != nil {
			writerErr <- execErr
			return
		}
		// This is the pre-C1.2 writer shape: no receipt attempt identity.
		if _, execErr := tx.Exec(writerCtx, `INSERT INTO request_delivery_receipts (
			org_id,request_id,sequence,mode,outcome,delivered_bytes,delivered_chunks,http_status
		) VALUES (7501,$1,1,'stream','write_started',0,0,200)`, requestID); execErr != nil {
			writerErr <- execErr
			return
		}
		writerErr <- tx.Commit(writerCtx)
	}()
	pid := <-writerPID
	waitForLock(&pid, "RowShareLock", false)

	require.NoError(t, blocker.Commit(ctx))
	require.NoError(t, <-migrationErr)
	require.ErrorContains(t, <-writerErr, "attempt_id",
		"a queued pre-C1.2 writer must fail after the attempt constraint becomes active")

	var receipts, ledger int
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_delivery_receipts
		WHERE org_id=7501 AND request_id=$1`, requestID).Scan(&receipts))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM freeze_idempotency
		WHERE org_id=7501 AND request_id=$1`, requestID).Scan(&status))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations
		WHERE version='000040_v1_delivery_attempt_binding'`).Scan(&ledger))
	require.Zero(t, receipts)
	require.Equal(t, "frozen", status)
	require.Equal(t, 1, ledger)
}
