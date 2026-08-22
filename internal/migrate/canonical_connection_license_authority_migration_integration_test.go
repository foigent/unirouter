//go:build integration

package migrate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCanonicalConnectionLicenseAuthoritySplitLockOrderIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	downgradeObservationProviderCostReconciliationForM79Test(t, pool)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	downBody, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000079_canonical_connection_license_authority.down.sql",
	))
	require.NoError(t, err)
	down := string(downBody)

	t.Run("connection writer completes before migration takes resource lock", func(t *testing.T) {
		writer, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer writer.Release()
		var writerPID int
		require.NoError(t, writer.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
		_, err = writer.Exec(ctx, `BEGIN`)
		require.NoError(t, err)
		_, err = writer.Exec(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('m79-lock-writer','custom','https://m79-lock-writer.invalid',
        'openai_compatible','{}','enabled',1)`)
		require.NoError(t, err,
			"the uncommitted writer must acquire the active_channels resource lock before migration waits")

		downConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer downConn.Release()
		var downPID int
		require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
		downResult := make(chan error, 1)
		go func() {
			_, execErr := downConn.Exec(ctx, down)
			downResult <- execErr
		}()
		waitForBackendBlockedBy(t, pool, downPID, writerPID)
		_, err = writer.Exec(ctx, `ROLLBACK`)
		require.NoError(t, err)
		select {
		case downErr := <-downResult:
			require.NoError(t, downErr)
		case <-ctx.Done():
			t.Fatal("M79 down did not finish after the provider writer released its table and resource locks")
		}
	})

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	downgradeObservationProviderCostReconciliationForM79Test(t, pool)
	_, err = pool.Exec(ctx, `
INSERT INTO commercial_license_entitlements (
  singleton,license_id,deployment_id,key_id,expires_at,features,active_channels)
VALUES (TRUE,'m79-lock-license','m79-lock-deployment','m79-lock-key',
        NOW()+INTERVAL '1 day','["*"]'::jsonb,NULL)`)
	require.NoError(t, err)

	t.Run("renewal releases resource and entitlement before migration proceeds", func(t *testing.T) {
		renewal, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer renewal.Release()
		var renewalPID int
		require.NoError(t, renewal.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&renewalPID))
		_, err = renewal.Exec(ctx, `BEGIN`)
		require.NoError(t, err)
		_, err = renewal.Exec(ctx, `
SELECT pg_advisory_xact_lock(
  hashtextextended('unirouter.commercial_limit.active_channels',0)
);
UPDATE commercial_license_entitlements
   SET updated_at=NOW()
 WHERE singleton=TRUE`)
		require.NoError(t, err)

		downConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer downConn.Release()
		var downPID int
		require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
		downResult := make(chan error, 1)
		go func() {
			_, execErr := downConn.Exec(ctx, down)
			downResult <- execErr
		}()
		waitForBackendBlockedBy(t, pool, downPID, renewalPID)
		_, err = renewal.Exec(ctx, `ROLLBACK`)
		require.NoError(t, err)
		select {
		case downErr := <-downResult:
			require.NoError(t, downErr, "split lock order must not produce PostgreSQL 40P01")
		case <-ctx.Done():
			t.Fatal("M79 down did not finish after renewal released the resource and entitlement locks")
		}
	})
}

func TestCanonicalConnectionLicenseAuthorityConcurrentActivationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	downgradeObservationProviderCostReconciliationForM79Test(t, pool)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `
INSERT INTO commercial_license_entitlements (
  singleton,license_id,deployment_id,key_id,expires_at,features,active_channels)
VALUES (TRUE,'m79-concurrent-license','m79-concurrent-deployment','m79-concurrent-key',
        NOW()+INTERVAL '1 day','["*"]'::jsonb,1)`)
	require.NoError(t, err)

	const attempts = 8
	start := make(chan struct{})
	results := make(chan error, attempts)
	var wait sync.WaitGroup
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, insertErr := pool.Exec(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'custom',$2,'openai_compatible','{}','enabled',1)`,
				fmt.Sprintf("m79-concurrent-%d", index),
				fmt.Sprintf("https://m79-concurrent-%d.invalid", index),
			)
			results <- insertErr
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)
	var successes int
	for result := range results {
		if result == nil {
			successes++
			continue
		}
		require.ErrorContains(t, result, "commercial License limit exceeded for active_channels")
	}
	require.Equal(t, 1, successes,
		"the shared active_channels advisory lock must serialize concurrent canonical activations")
	var enabled int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM provider_connections WHERE admin_state='enabled'`).Scan(&enabled))
	require.EqualValues(t, 1, enabled)
}

func TestCanonicalConnectionLicenseAuthorityMigrationAndSafeRollbackIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	downgradeObservationProviderCostReconciliationForM79Test(t, pool)
	ctx := t.Context()
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx, `
INSERT INTO commercial_license_entitlements (
  singleton,license_id,deployment_id,key_id,expires_at,features,active_channels)
VALUES (TRUE,'m79-license','m79-deployment','m79-key',
        NOW()+INTERVAL '1 day','["*"]'::jsonb,1)`)
		return err
	}())

	var legacyChannelID, connectionID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
  name,provider,base_url,adapter_type,adapter_config,status,routing_epoch)
VALUES ('m79-connection','custom','https://m79.invalid',
        'openai_compatible','{}','active',1) RETURNING id`).Scan(&legacyChannelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  legacy_channel_id,name,provider,base_url,adapter_type,adapter_config,
  admin_state,config_revision)
VALUES ($1,'m79-connection','custom','https://m79.invalid',
        'openai_compatible','{}','enabled',1) RETURNING id`, legacyChannelID).Scan(&connectionID))
	require.NotZero(t, connectionID)

	var pausedConnectionID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('m79-paused-native','custom','https://m79-paused.invalid',
        'openai_compatible','{}','disabled',1) RETURNING id`).Scan(&pausedConnectionID))
	var nativeCredentialID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (
  connection_id,key_enc,status,state_revision)
VALUES ($1,'m79-native-fixture','disabled',1) RETURNING id`,
		pausedConnectionID).Scan(&nativeCredentialID))
	var retiredConnectionID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,retired_at,config_revision)
VALUES ('m79-retired-native','custom','https://m79-retired.invalid',
	    'openai_compatible','{}','retired',NOW(),1) RETURNING id`).Scan(&retiredConnectionID))
	_, err := pool.Exec(ctx, `
UPDATE provider_connections SET admin_state='enabled' WHERE id=$1`, pausedConnectionID)
	require.ErrorContains(t, err, "commercial License limit exceeded for active_channels")

	downBody, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000079_canonical_connection_license_authority.down.sql",
	))
	require.NoError(t, err)
	down := string(downBody)

	_, err = pool.Exec(ctx, `UPDATE channels SET name='m79-drift' WHERE id=$1`, legacyChannelID)
	require.NoError(t, err)
	connection, err := pool.Acquire(ctx)
	require.NoError(t, err)
	_, err = connection.Exec(ctx, down)
	require.ErrorContains(t, err, "lacks an exact active legacy projection")
	_, rollbackErr := connection.Exec(ctx, "ROLLBACK")
	require.NoError(t, rollbackErr)
	connection.Release()
	_, err = pool.Exec(ctx, `UPDATE channels SET name='m79-connection' WHERE id=$1`, legacyChannelID)
	require.NoError(t, err)

	var extraChannelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,status)
VALUES ('m79-extra-legacy','custom','https://m79-extra.invalid','active') RETURNING id`).Scan(&extraChannelID))
	connection, err = pool.Acquire(ctx)
	require.NoError(t, err)
	_, err = connection.Exec(ctx, down)
	require.ErrorContains(t, err, "active legacy channels exceed signed active_channels limit")
	_, rollbackErr = connection.Exec(ctx, "ROLLBACK")
	require.NoError(t, rollbackErr)
	connection.Release()
	_, err = pool.Exec(ctx, `UPDATE channels SET status='disabled' WHERE id=$1`, extraChannelID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, down)
	require.NoError(t, err)
	assertCommercialLimitTrigger(t, pool, "channels", "channels_commercial_limit", true)
	assertCommercialLimitTrigger(t, pool, "provider_connections", "provider_connections_commercial_limit", false)
	var nativeConnections, nativeCredentials int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM provider_connections WHERE id IN ($1,$2)),
  (SELECT COUNT(*) FROM provider_credential_bindings WHERE id=$3)`,
		pausedConnectionID, retiredConnectionID, nativeCredentialID,
	).Scan(&nativeConnections, &nativeCredentials))
	require.EqualValues(t, 2, nativeConnections)
	require.EqualValues(t, 1, nativeCredentials,
		"M79 down must preserve native M78 connection and credential data")
	_, err = pool.Exec(ctx, `UPDATE channels SET status='active' WHERE id=$1`, extraChannelID)
	require.ErrorContains(t, err, "commercial License limit exceeded for active_channels",
		"M79 down must restore the M78 legacy-channel counter before removing its ledger")

	var unsafeNativeID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('m79-unsafe-native','custom','https://m79-unsafe.invalid',
        'openai_compatible','{}','enabled',1) RETURNING id`).Scan(&unsafeNativeID))
	err = migrate.Run(ctx, pool, testutil.MigrationsDir(t))
	require.ErrorContains(t, err, "enabled provider connections exceed signed active_channels limit")
	_, err = pool.Exec(ctx, `
UPDATE provider_connections
   SET admin_state='retired',retired_at=NOW(),config_revision=config_revision+1
 WHERE id=$1`, unsafeNativeID)
	require.NoError(t, err)
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	assertCommercialLimitTrigger(t, pool, "channels", "channels_commercial_limit", false)
	assertCommercialLimitTrigger(t, pool, "provider_connections", "provider_connections_commercial_limit", true)
}

func downgradeObservationProviderCostReconciliationForM79Test(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	execMigrationDown(t, t.Context(), pool, "000081_versioned_model_execution_profiles.down.sql")
	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000080_observation_provider_cost_reconciliation.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(down))
	require.NoError(t, err)
}

func assertCommercialLimitTrigger(t *testing.T, pool *pgxpool.Pool, table, trigger string, want bool) {
	t.Helper()
	var present bool
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT EXISTS (
  SELECT 1
    FROM pg_trigger catalog_trigger
    JOIN pg_class relation ON relation.oid=catalog_trigger.tgrelid
   WHERE relation.relname=$1 AND catalog_trigger.tgname=$2
     AND NOT catalog_trigger.tgisinternal
)`, table, trigger).Scan(&present))
	require.Equal(t, want, present, "%s.%s", table, trigger)
}
