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

func TestModelExecutionPlanFreezeFencePopulatedPredecessorRoundTripIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000084_official_rate_source_currency_drafts.up.sql")
	ctx := t.Context()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name) VALUES ('m85-predecessor') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
VALUES ('m85-legacy',$1,'frozen','legacy-price')`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO billing_provider_attempts (
  org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
  route_sequence,route_total,endpoint,provider,channel_name,
  route_evidence_version,route_authority_mode,route_execution_contract_source,
  route_evidence_hash,route_model_operation_id,
  route_legacy_routing_revision,route_legacy_circuit_revision,
  route_legacy_credential_state_revision,route_requires_probe
) VALUES ($1,'m85-legacy','legacy-attempt',101,201,301,1,1,
  '/v1/chat/completions','provider','channel','unirouter.execution-route.v1',
  'legacy','legacy_adapter','sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  'openai.chat_completions.v1',1,1,1,FALSE)`, orgID)
	require.NoError(t, err)
	var legacyBindings string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT jsonb_agg(to_jsonb(b) ORDER BY operation_id)::text
FROM model_operation_current_public_execution_contracts b`).Scan(&legacyBindings))

	for cycle := 0; cycle < 2; cycle++ {
		require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
		var freezeHash, attemptHash *string
		require.NoError(t, pool.QueryRow(ctx, `
SELECT f.execution_snapshot_hash,a.execution_snapshot_hash
FROM freeze_idempotency f JOIN billing_provider_attempts a USING(org_id,request_id)
WHERE f.org_id=$1 AND f.request_id='m85-legacy'`, orgID).Scan(&freezeHash, &attemptHash))
		require.Nil(t, freezeHash, "migration must not invent a historical Plan identity")
		require.Nil(t, attemptHash)
		var preservedBindings string
		require.NoError(t, pool.QueryRow(ctx, `
SELECT jsonb_agg(to_jsonb(b) ORDER BY operation_id)::text
FROM model_operation_exact_public_execution_contracts b
WHERE protocol_profile_id IN ('unirouter.openai.responses.text.v1','unirouter.anthropic.messages.text.v1')`).Scan(&preservedBindings))
		require.JSONEq(t, legacyBindings, preservedBindings, "M86/M87 must preserve exact v1 tuples")
		execMigrationDown(t, ctx, pool, "000085_model_execution_plan_freeze_fence.down.sql")
		var retainedRows int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM freeze_idempotency f
JOIN billing_provider_attempts a USING(org_id,request_id)
WHERE f.org_id=$1 AND f.request_id='m85-legacy' AND f.pricing_version='legacy-price'`, orgID).Scan(&retainedRows))
		require.Equal(t, 1, retainedRows)
		require.NoError(t, pool.QueryRow(ctx, `
SELECT jsonb_agg(to_jsonb(b) ORDER BY operation_id)::text
FROM model_operation_current_public_execution_contracts b`).Scan(&preservedBindings))
		require.JSONEq(t, legacyBindings, preservedBindings)
	}
}

func TestModelExecutionPlanFreezeFenceDownWaitsForEvidenceWriterIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	execMigrationDown(t, ctx, pool, "000086_exact_public_execution_bindings.down.sql")
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name) VALUES ('m85-writer') RETURNING id`).Scan(&orgID))
	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer writer.Rollback(context.Background())
	_, err = writer.Exec(ctx, `
INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version,execution_snapshot_hash)
VALUES ('m85-writer',$1,'frozen','test','mrs1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')`, orgID)
	require.NoError(t, err)
	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var pid int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000085_model_execution_plan_freeze_fence.down.sql"))
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		_, downErr := downConn.Exec(ctx, string(down))
		result <- downErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(ctx, `SELECT COALESCE(wait_event_type='Lock',FALSE) FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, writer.Commit(ctx))
	select {
	case downErr := <-result:
		// End a refused down transaction before checking retained evidence.
		_, _ = downConn.Exec(ctx, "ROLLBACK")
		require.ErrorContains(t, downErr, "durable snapshot bindings exist")
	case <-ctx.Done():
		t.Fatal("rollback did not finish after the evidence writer committed")
	}
	var retainedHash string
	require.NoError(t, pool.QueryRow(ctx, `SELECT execution_snapshot_hash FROM freeze_idempotency WHERE org_id=$1 AND request_id='m85-writer'`, orgID).Scan(&retainedHash))
	require.Equal(t, "mrs1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", retainedHash)
}

func TestModelExecutionPlanFreezeFenceSharesMigrationLifecycleLockIntegration(t *testing.T) {
	for _, direction := range []string{"up", "down"} {
		t.Run(direction, func(t *testing.T) {
			predecessor := "000084_official_rate_source_currency_drafts.up.sql"
			if direction == "down" {
				predecessor = "000085_model_execution_plan_freeze_fence.up.sql"
			}
			pool, _ := setupMigratedThrough(t, predecessor)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			runner, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer runner.Rollback(context.Background())
			// Hold only the runner lifecycle lock: ledger/table writes would
			// mask a wrong advisory key by blocking rollback for another reason.
			_, err = runner.Exec(ctx, `SELECT pg_advisory_xact_lock(86420517831702491)`)
			require.NoError(t, err)
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()
			var pid int
			require.NoError(t, conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
			body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000085_model_execution_plan_freeze_fence."+direction+".sql"))
			require.NoError(t, err)
			result := make(chan error, 1)
			go func() {
				_, execErr := conn.Exec(ctx, string(body))
				result <- execErr
			}()
			finished := false
			var earlyErr error
			require.Eventually(t, func() bool {
				select {
				case earlyErr = <-result:
					finished = true
					return true
				default:
				}
				var waiting bool
				err := pool.QueryRow(ctx, `SELECT COALESCE(wait_event='advisory',FALSE) FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 20*time.Millisecond)
			require.False(t, finished, "M85 %s bypassed the migration lifecycle lock: %v", direction, earlyErr)
			require.NoError(t, runner.Commit(ctx))
			select {
			case err := <-result:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("M85 did not finish after releasing the migration lifecycle lock")
			}
		})
	}
}

func TestModelExecutionPlanFreezeFenceMigrationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000086_exact_public_execution_bindings.down.sql")

	var columnCount, constraintCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM information_schema.columns
 WHERE table_schema='public'
   AND table_name IN ('freeze_idempotency','billing_provider_attempts')
   AND column_name='execution_snapshot_hash'`).Scan(&columnCount))
	require.Equal(t, 2, columnCount)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM pg_constraint
 WHERE conname IN (
   'freeze_idempotency_execution_snapshot_hash_check',
   'billing_provider_attempts_execution_snapshot_hash_check'
 )`).Scan(&constraintCount))
	require.Equal(t, 2, constraintCount)

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name) VALUES ('m85-plan-freeze-fence') RETURNING id`,
	).Scan(&orgID))
	_, err := pool.Exec(ctx, `
INSERT INTO freeze_idempotency (
  request_id,org_id,status,pricing_version,execution_snapshot_hash
) VALUES (
  'm85-invalid-snapshot',$1,'frozen','m85','sha256:invalid'
)`, orgID)
	require.ErrorContains(t, err, "freeze_idempotency_execution_snapshot_hash_check")

	validSnapshot := "mrs1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, err = pool.Exec(ctx, `
INSERT INTO freeze_idempotency (
  request_id,org_id,status,pricing_version,execution_snapshot_hash
) VALUES (
  'm85-valid-snapshot',$1,'frozen','m85',$2
)`, orgID, validSnapshot)
	require.NoError(t, err)

	var stored string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT execution_snapshot_hash
  FROM freeze_idempotency
 WHERE org_id=$1 AND request_id='m85-valid-snapshot'`, orgID).Scan(&stored))
	require.Equal(t, validSnapshot, stored)
	_, err = pool.Exec(ctx, `
UPDATE freeze_idempotency
   SET execution_snapshot_hash='mrs1_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
 WHERE org_id=$1 AND request_id='m85-valid-snapshot'`, orgID)
	require.ErrorContains(t, err, "model execution Plan snapshot is immutable")

	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000085_model_execution_plan_freeze_fence.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "durable snapshot bindings exist")
}
