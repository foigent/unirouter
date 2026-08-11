//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F07DistributedLimitPolicyMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)

	assertUp := func() {
		t.Helper()
		var tableCount, columnCount, ledgerCount int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema='public' AND table_name='distributed_limit_policies'`).Scan(&tableCount))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema='public' AND table_name='api_keys' AND column_name='policy_labels'`).Scan(&columnCount))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM schema_migrations
			WHERE version='000024_prd6_distributed_limit_policy'`).Scan(&ledgerCount))
		require.Equal(t, 1, tableCount)
		require.Equal(t, 1, columnCount)
		require.Equal(t, 1, ledgerCount)
	}
	assertUp()

	// Schema 59 owns family-stable policy selectors that depend on the legacy
	// policy_labels column. Exercise the migration in valid reverse order.
	rollbackEmptyMigrations(t, pool, []string{
		"000061_xinsight_connector_cp2a_phase_contract.down.sql",
		"000060_xinsight_connector_cp1.down.sql",
		"000059_enterprise_access_provisioning.down.sql",
	})
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000024_prd6_distributed_limit_policy.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var tableCount, columnCount, ledgerCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name='distributed_limit_policies'`).Scan(&tableCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='api_keys' AND column_name='policy_labels'`).Scan(&columnCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000024_prd6_distributed_limit_policy'`).Scan(&ledgerCount))
	require.Zero(t, tableCount)
	require.Zero(t, columnCount)
	require.Zero(t, ledgerCount)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()

	orgID := testutil.NewTestOrg(t, pool)
	var keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (org_id,name,key_hash,key_prefix,status,rpm_limit,qps_limit,policy_labels)
		VALUES ($1,'migration-guard-key','p6f07-migration-guard-hash','ur-p6f07-guard','active',60,10,'production')
		RETURNING id`, orgID).Scan(&keyID))
	_, err = pool.Exec(ctx, `
		INSERT INTO distributed_limit_policies (org_id,api_key_id,name,mode,failure_mode,rpm_limit)
		VALUES ($1,$2,'migration-guard-policy','enforce','fail_closed',60)`, orgID, keyID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F07 down refused")
	assertUp()

	var policyCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM distributed_limit_policies WHERE name='migration-guard-policy'`).Scan(&policyCount))
	require.Equal(t, 1, policyCount, "refused rollback must preserve configured policies")
}
