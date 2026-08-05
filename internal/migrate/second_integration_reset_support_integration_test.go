//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestSecondIntegrationResetSupportMigrationUpDownAndRollForward(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000056_prd8_invocation_optimization.up.sql")
	ctx := context.Background()
	assertSecondIntegrationResetSupportContract(t, pool, false)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	assertSecondIntegrationResetSupportContract(t, pool, true)

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('Reset support cycle','active','standard') RETURNING id`).Scan(&orgID))
	const requestID = "reset-support-cycle"
	const attemptID = "reset-support-attempt"
	_, err := pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
		VALUES ($1,$2,'settled','reset-support-test')`, requestID, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO billing_provider_attempts (
			org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
			route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty
		) VALUES ($1,$2,$3,101,201,301,1,1,'/test','test-provider','test-channel','settled','known_usage')`,
		orgID, requestID, attemptID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE freeze_idempotency SET delivery_attempt_id=$1
		WHERE org_id=$2 AND request_id=$3`, attemptID, orgID, requestID)
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	deferred := false
	defer func() {
		if !deferred {
			_ = tx.Rollback(ctx)
		}
	}()
	_, err = tx.Exec(ctx, `SET CONSTRAINTS
		billing_provider_attempts_reservation_fk,
		freeze_idempotency_delivery_attempt_provider_fk DEFERRED`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`, orgID, requestID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	deferred = true

	_, err = pool.Exec(ctx, `
		INSERT INTO audit_logs (org_id,user_id,action,resource,client_request_id)
		VALUES (1,1,'bootstrap.first_customer','organization','bootstrap-migration-replay')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO audit_logs (org_id,user_id,action,resource,client_request_id)
		VALUES (1,1,'bootstrap.first_customer','organization','bootstrap-migration-replay')`)
	require.Error(t, err, "bootstrap operation identity must be globally replay-safe")

	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000057_second_integration_reset_support.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000058_future_contract')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	assertSecondIntegrationResetSupportContract(t, pool, true)

	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000058_future_contract'`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)
	assertSecondIntegrationResetSupportContract(t, pool, false)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	assertSecondIntegrationResetSupportContract(t, pool, true)
}

func assertSecondIntegrationResetSupportContract(t *testing.T, pool *pgxpool.Pool, expected bool) {
	t.Helper()
	ctx := context.Background()
	var deferrableCount, noActionCount, restrictCount, initiallyDeferredCount, indexCount, ledgerCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname IN (
			'billing_provider_attempts_reservation_fk',
			'freeze_idempotency_delivery_attempt_provider_fk'
		) AND condeferrable`).Scan(&deferrableCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname IN (
			'billing_provider_attempts_reservation_fk',
			'freeze_idempotency_delivery_attempt_provider_fk'
		) AND condeferred`).Scan(&initiallyDeferredCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname IN (
			'billing_provider_attempts_reservation_fk',
			'freeze_idempotency_delivery_attempt_provider_fk'
		) AND confdeltype='a'`).Scan(&noActionCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname IN (
			'billing_provider_attempts_reservation_fk',
			'freeze_idempotency_delivery_attempt_provider_fk'
		) AND confdeltype='r'`).Scan(&restrictCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname='public'
		  AND indexname='audit_logs_first_customer_bootstrap_idempotency'
		  AND indexdef LIKE 'CREATE UNIQUE INDEX%'
		  AND indexdef LIKE '%bootstrap.first_customer%'`).Scan(&indexCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000057_second_integration_reset_support'`).Scan(&ledgerCount))
	if expected {
		require.Equal(t, 2, deferrableCount)
		require.Equal(t, 2, noActionCount)
		require.Zero(t, restrictCount)
		require.Equal(t, 1, indexCount)
		require.Equal(t, 1, ledgerCount)
	} else {
		require.Zero(t, deferrableCount)
		require.Zero(t, noActionCount)
		require.Equal(t, 2, restrictCount)
		require.Zero(t, indexCount)
		require.Zero(t, ledgerCount)
	}
	require.Zero(t, initiallyDeferredCount, "reviewed cycle constraints must remain initially immediate")
}
