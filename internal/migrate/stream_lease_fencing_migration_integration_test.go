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

func TestV1StreamLeaseFencingMigrationUnusedDownAndRollForward(t *testing.T) {
	pool, migrationsDir := setupMigratedThrough(t, "000041_v1_stream_lease_fencing.up.sql")
	ctx := context.Background()
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000041_v1_stream_lease_fencing.down.sql"))
	require.NoError(t, err)

	assertUp := func() {
		t.Helper()
		var ledger, leaseGeneration, checkpointGeneration, receiptFence, settlementFence, constraints int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations
			WHERE version='000041_v1_stream_lease_fencing'`).Scan(&ledger))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema='public' AND table_name='stream_delivery_leases' AND column_name='generation'`).Scan(&leaseGeneration))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema='public' AND table_name='stream_delivery_checkpoints' AND column_name='lease_generation'`).Scan(&checkpointGeneration))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema='public' AND table_name='request_delivery_receipts'
			  AND column_name IN ('stream_owner_id','stream_lease_generation')`).Scan(&receiptFence))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema='public' AND table_name='freeze_idempotency'
			  AND column_name IN ('settlement_stream_owner_id','settlement_stream_generation')`).Scan(&settlementFence))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_constraint WHERE conname IN (
			'stream_delivery_leases_generation_positive',
			'stream_delivery_checkpoints_generation_positive',
			'request_delivery_receipts_stream_fence_pair',
			'freeze_idempotency_stream_fence_pair'
		)`).Scan(&constraints))
		require.Equal(t, 1, ledger)
		require.Equal(t, 1, leaseGeneration)
		require.Equal(t, 1, checkpointGeneration)
		require.Equal(t, 2, receiptFence)
		require.Equal(t, 2, settlementFence)
		require.Equal(t, 4, constraints)
	}

	assertUp()
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)
	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()
}

func TestV1StreamLeaseFencingMigrationRefusesActiveLeaseCutover(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000040_v1_delivery_attempt_binding.up.sql")
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (
			request_id,org_id,frozen_amount_micros,reserved_balance_micros,status
		) VALUES ('stream-fence-active-cutover',7201,100,100,'frozen')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO stream_delivery_leases (
			org_id,request_id,attempt_id,owner_id,state,lease_expires_at
		) VALUES (7201,'stream-fence-active-cutover','attempt-active','old-owner','active',NOW()+INTERVAL '1 minute')`)
	require.NoError(t, err)

	throughFencing := migrationsThrough(t, testutil.MigrationsDir(t), "000041_v1_stream_lease_fencing.up.sql")
	err = migrate.Run(ctx, pool, throughFencing)
	require.ErrorContains(t, err, "drain active stream leases")
	var ledger, generationColumn int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations
		WHERE version='000041_v1_stream_lease_fencing'`).Scan(&ledger))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='stream_delivery_leases' AND column_name='generation'`).Scan(&generationColumn))
	require.Zero(t, ledger)
	require.Zero(t, generationColumn, "failed cutover must be atomic")
}

func TestV1StreamLeaseFencingMigrationRejectsUnfencedNewStreamEvidence(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (
			request_id,org_id,frozen_amount_micros,reserved_balance_micros,status
		) VALUES ('stream-fence-schema',7301,100,100,'frozen')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO billing_provider_attempts (
			org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
			route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty
		) VALUES (7301,'stream-fence-schema','attempt-schema',1,2,3,1,1,
			'/v1/chat/completions','test','test-channel','response_accepted','pending')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE freeze_idempotency SET delivery_attempt_id='attempt-schema'
		WHERE org_id=7301 AND request_id='stream-fence-schema'`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO request_delivery_receipts (
			org_id,request_id,attempt_id,sequence,mode,outcome,delivered_bytes,delivered_chunks,http_status
		) VALUES (7301,'stream-fence-schema','attempt-schema',1,'stream','write_started',0,0,200)`)
	require.ErrorContains(t, err, "ownership generation")

	_, err = pool.Exec(ctx, `
		INSERT INTO request_delivery_receipts (
			org_id,request_id,attempt_id,sequence,mode,outcome,delivered_bytes,delivered_chunks,http_status,
			stream_owner_id,stream_lease_generation
		) VALUES (7301,'stream-fence-schema','attempt-schema',1,'stream','write_started',0,0,200,'owner-one',1)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO request_delivery_receipts (
			org_id,request_id,attempt_id,sequence,mode,outcome,delivered_bytes,delivered_chunks,http_status,
			stream_owner_id,stream_lease_generation
		) VALUES (7301,'stream-fence-schema','attempt-schema',2,'stream','partial',1,1,200,'owner-stale',1)`)
	require.ErrorContains(t, err, "fence cannot regress")

	downSQL, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000041_v1_stream_lease_fencing.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "C1.3 rollback refused")
}

func TestV1StreamLeaseFencingCutoverSerializesOldWriter(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000040_v1_delivery_attempt_binding.up.sql")
	ctx := context.Background()
	const requestID = "stream-fence-cutover-race"
	const attemptID = "attempt-cutover-race"
	_, err := pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (
			request_id,org_id,frozen_amount_micros,reserved_balance_micros,status
		) VALUES ($1,7401,100,100,'frozen')`, requestID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO billing_provider_attempts (
			org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
			route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty
		) VALUES (7401,$1,$2,1,2,3,1,1,'/v1/chat/completions','test','test-channel',
			'response_accepted','pending')`, requestID, attemptID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE freeze_idempotency SET delivery_attempt_id=$3
		WHERE org_id=$1 AND request_id=$2`, 7401, requestID, attemptID)
	require.NoError(t, err)

	// Pause the migration after it takes the leading freeze authority lock but
	// before it can lock the lease table. This exposes the former guard/ALTER
	// window without modifying the production migration.
	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer blocker.Rollback(ctx)
	_, err = blocker.Exec(ctx, `LOCK TABLE stream_delivery_leases IN ACCESS SHARE MODE`)
	require.NoError(t, err)

	throughFencing := migrationsThrough(t, testutil.MigrationsDir(t), "000041_v1_stream_lease_fencing.up.sql")
	migrationErr := make(chan error, 1)
	go func() {
		migrationErr <- migrate.Run(context.Background(), pool, throughFencing)
	}()

	waitForRelationLock := func(pid *int32, relation, mode string, granted bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			var present bool
			query := `SELECT EXISTS (
				SELECT 1 FROM pg_locks l JOIN pg_class c ON c.oid=l.relation
				WHERE c.relname=$1 AND l.mode=$2 AND l.granted=$3`
			args := []any{relation, mode, granted}
			if pid != nil {
				query += ` AND l.pid=$4`
				args = append(args, *pid)
			}
			query += `)`
			require.NoError(t, pool.QueryRow(ctx, query, args...).Scan(&present))
			if present {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s %s granted=%t pid=%v", relation, mode, granted, pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitForRelationLock(nil, "freeze_idempotency", "AccessExclusiveLock", true)

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
			WHERE org_id=7401 AND request_id=$1 FOR UPDATE`, requestID).Scan(&status); queryErr != nil {
			writerErr <- queryErr
			return
		}
		if _, execErr := tx.Exec(writerCtx, `UPDATE freeze_idempotency
			SET status='settlement_pending',settlement_pending_reason='billable_output',output_started_at=NOW()
			WHERE org_id=7401 AND request_id=$1 AND status='frozen'`, requestID); execErr != nil {
			writerErr <- execErr
			return
		}
		// This is the pre-C1.3 writer shape: it has no owner-generation columns.
		if _, execErr := tx.Exec(writerCtx, `INSERT INTO request_delivery_receipts (
			org_id,request_id,attempt_id,sequence,mode,outcome,delivered_bytes,delivered_chunks,http_status
		) VALUES (7401,$1,$2,1,'stream','write_started',0,0,200)`, requestID, attemptID); execErr != nil {
			writerErr <- execErr
			return
		}
		if _, execErr := tx.Exec(writerCtx, `INSERT INTO stream_delivery_leases (
			org_id,request_id,attempt_id,owner_id,state,lease_expires_at
		) VALUES (7401,$1,$2,'old-writer','active',NOW()+INTERVAL '1 minute')`, requestID, attemptID); execErr != nil {
			writerErr <- execErr
			return
		}
		writerErr <- tx.Commit(writerCtx)
	}()
	pid := <-writerPID
	waitForRelationLock(&pid, "freeze_idempotency", "RowShareLock", false)

	require.NoError(t, blocker.Commit(ctx))
	require.NoError(t, <-migrationErr)
	require.ErrorContains(t, <-writerErr, "ownership generation",
		"a writer queued before cutover must resume only after the fenced trigger is active")

	var leases, receipts, ledger int
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM stream_delivery_leases
		WHERE org_id=7401 AND request_id=$1`, requestID).Scan(&leases))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_delivery_receipts
		WHERE org_id=7401 AND request_id=$1`, requestID).Scan(&receipts))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM freeze_idempotency
		WHERE org_id=7401 AND request_id=$1`, requestID).Scan(&status))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations
		WHERE version='000041_v1_stream_lease_fencing'`).Scan(&ledger))
	require.Zero(t, leases)
	require.Zero(t, receipts)
	require.Equal(t, "frozen", status, "the old writer transaction must roll back atomically")
	require.Equal(t, 1, ledger)
}
