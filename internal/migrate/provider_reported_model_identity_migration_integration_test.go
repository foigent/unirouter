//go:build integration

package migrate_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestProviderReportedModelIdentityMigrationAppliesRollsBackAndReapplies(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var columnExists, ledgerExists bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (
         SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='channel_model_probes' AND column_name='reported_model'
       ),
       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000068_provider_reported_model_identity')`,
	).Scan(&columnExists, &ledgerExists))
	require.True(t, columnExists)
	require.True(t, ledgerExists)

	execMigrationDown(t, ctx, pool, "000071_model_execution_evidence.down.sql")
	execMigrationDown(t, ctx, pool, "000070_runtime_deployment_route_manifest.down.sql")
	execMigrationDown(t, ctx, pool, "000069_model_deployment_expand.down.sql")
	execMigrationDown(t, ctx, pool, "000068_provider_reported_model_identity.down.sql")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (
         SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='channel_model_probes' AND column_name='reported_model'
       ),
       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000068_provider_reported_model_identity')`,
	).Scan(&columnExists, &ledgerExists))
	require.False(t, columnExists)
	require.False(t, ledgerExists)

	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000068_provider_reported_model_identity"))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (
         SELECT 1 FROM information_schema.columns
          WHERE table_schema='public' AND table_name='channel_model_probes' AND column_name='reported_model'
       ),
       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000068_provider_reported_model_identity')`,
	).Scan(&columnExists, &ledgerExists))
	require.True(t, columnExists)
	require.True(t, ledgerExists)
}
