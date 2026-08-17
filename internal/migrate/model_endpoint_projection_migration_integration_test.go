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

func TestModelEndpointProjectionMigrationAppliesRollsBackAndReapplies(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var tableExists, ledgerExists bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT to_regclass('public.channel_model_probes') IS NOT NULL,
       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000065_model_endpoint_projection')`).Scan(&tableExists, &ledgerExists))
	require.True(t, tableExists)
	require.True(t, ledgerExists)

	var primaryKeyColumns int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM information_schema.key_column_usage usage
  JOIN information_schema.table_constraints constraint_row
    ON constraint_row.constraint_schema=usage.constraint_schema
   AND constraint_row.constraint_name=usage.constraint_name
 WHERE usage.table_schema='public' AND usage.table_name='channel_model_probes'
   AND constraint_row.constraint_type='PRIMARY KEY'
   AND usage.column_name IN ('channel_id','public_model')`).Scan(&primaryKeyColumns))
	require.Equal(t, 2, primaryKeyColumns)

	execMigrationDown(t, ctx, pool, "000066_runtime_assignment_projection.down.sql")
	execMigrationDown(t, ctx, pool, "000065_model_endpoint_projection.down.sql")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT to_regclass('public.channel_model_probes') IS NOT NULL,
       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000065_model_endpoint_projection')`).Scan(&tableExists, &ledgerExists))
	require.False(t, tableExists)
	require.False(t, ledgerExists)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT to_regclass('public.channel_model_probes') IS NOT NULL,
       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000065_model_endpoint_projection')`).Scan(&tableExists, &ledgerExists))
	require.True(t, tableExists)
	require.True(t, ledgerExists)
}

func TestModelEndpointProjectionMigrationRejectsOutOfOrderRollback(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000067_future_contract')`)
	require.NoError(t, err)

	downBytes, readErr := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000065_model_endpoint_projection.down.sql"))
	require.NoError(t, readErr)
	down := string(downBytes)
	_, err = pool.Exec(ctx, down)
	require.ErrorContains(t, err, "later migrations must be reversed first")

	var tableExists, ledgerExists bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT to_regclass('public.channel_model_probes') IS NOT NULL,
       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000065_model_endpoint_projection')`).Scan(&tableExists, &ledgerExists))
	require.True(t, tableExists)
	require.True(t, ledgerExists)
}
