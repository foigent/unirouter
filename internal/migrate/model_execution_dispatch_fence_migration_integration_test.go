//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelExecutionDispatchFenceHasOrderedEmptyRollbackAndReapply(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.PreRetirementMigrationsDir(t), "000073_model_execution_dispatch_fence.down.sql",
	))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000075_future_contract')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000075_future_contract'`)
	require.NoError(t, err)

	execMigrationDown(t, ctx, pool, "000073_model_execution_dispatch_fence.down.sql")
	var columns int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name='billing_provider_attempts'
   AND column_name IN ('dispatch_owner_hash','health_outcome','health_applied_at')`).Scan(&columns))
	require.Zero(t, columns)

	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.PreRetirementMigrationsDir(t), "000073_model_execution_dispatch_fence"))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema='public' AND table_name='billing_provider_attempts'
   AND column_name IN ('dispatch_owner_hash','health_outcome','health_applied_at')`).Scan(&columns))
	require.Equal(t, 3, columns)
	var applied bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM schema_migrations
     WHERE version='000073_model_execution_dispatch_fence'
)`).Scan(&applied))
	require.True(t, applied)
}

func TestModelExecutionDispatchFenceDownWaitsForConcurrentEvidenceWriterThenFailsClosed(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	execMigrationDown(t, ctx, pool, "000074_model_deployment_create_idempotency.down.sql")

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('dispatch-fence-down-race','active','customer') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ('dispatch-fence-down-race',$1,'frozen','migration-test')`, orgID)
	require.NoError(t, err)
	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	writerOpen := true
	t.Cleanup(func() {
		if writerOpen {
			_ = writer.Rollback(context.Background())
		}
	})
	_, err = writer.Exec(ctx, `
INSERT INTO billing_provider_attempts (
    org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
    route_sequence,route_total,endpoint,provider,channel_name,
    route_evidence_version,route_authority_mode,route_execution_contract_source,
    route_evidence_hash,route_model_operation_id,
    route_legacy_routing_revision,route_legacy_circuit_revision,
    route_legacy_credential_state_revision,route_requires_probe,dispatch_owner_hash)
VALUES ($1,'dispatch-fence-down-race','attempt-race',101,201,301,1,1,
        '/v1/chat/completions','provider','channel',
        'unirouter.execution-route.v1','legacy','legacy_adapter',$2,
        'openai.chat_completions.v1',1,1,1,FALSE,$3)`,
		orgID, "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64))
	require.NoError(t, err)

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.PreRetirementMigrationsDir(t), "000073_model_execution_dispatch_fence.down.sql",
	))
	require.NoError(t, err)
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(context.Background(), string(downSQL))
		downResult <- execErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(ctx, `
SELECT COALESCE(wait_event_type='Lock',FALSE)
  FROM pg_stat_activity WHERE pid=$1`, downPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 20*time.Millisecond,
		"rollback must stabilize the attempt table before inspecting durable evidence")

	require.NoError(t, writer.Commit(ctx))
	writerOpen = false
	select {
	case downErr := <-downResult:
		require.ErrorContains(t, downErr, "dispatch owner or terminal health evidence exists")
	case <-ctx.Done():
		t.Fatal("dispatch-fence rollback did not recheck evidence after the writer committed")
	}
	var ownerHash string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT dispatch_owner_hash FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id='dispatch-fence-down-race'`, orgID).Scan(&ownerHash))
	require.Equal(t, "sha256:"+strings.Repeat("b", 64), ownerHash)
}

func TestModelExecutionDispatchFenceDownSerializesWithFutureMigrationLedgerWriter(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	execMigrationDown(t, ctx, pool, "000074_model_deployment_create_idempotency.down.sql")

	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	writerOpen := true
	t.Cleanup(func() {
		if writerOpen {
			_ = writer.Rollback(context.Background())
		}
	})
	_, err = writer.Exec(ctx, `SELECT pg_advisory_xact_lock(86420517831702491)`)
	require.NoError(t, err)
	_, err = writer.Exec(ctx, `
INSERT INTO schema_migrations(version) VALUES ('000075_future_contract')`)
	require.NoError(t, err)

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.PreRetirementMigrationsDir(t), "000073_model_execution_dispatch_fence.down.sql",
	))
	require.NoError(t, err)
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(context.Background(), string(downSQL))
		downResult <- execErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(ctx, `
SELECT COALESCE(wait_event_type='Lock',FALSE)
  FROM pg_stat_activity WHERE pid=$1`, downPID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 20*time.Millisecond,
		"rollback must wait for the migration lifecycle lock even when the future migration does not touch the attempt table")

	require.NoError(t, writer.Commit(ctx))
	writerOpen = false
	select {
	case downErr := <-downResult:
		require.ErrorContains(t, downErr, "later migrations must be reversed first")
	case <-ctx.Done():
		t.Fatal("dispatch-fence rollback did not inspect the ledger after the future migration committed")
	}
	var futureApplied bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM schema_migrations WHERE version='000075_future_contract'
)`).Scan(&futureApplied))
	require.True(t, futureApplied)
}
