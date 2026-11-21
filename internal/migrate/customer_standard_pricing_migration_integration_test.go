//go:build integration

package migrate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestV1PricingAuthorityCustomerStandardMigrationIntegration(t *testing.T) {
	const predecessor = schemaauthority.UpgradeAdmissionRecoveryHead
	const version = schemaauthority.CustomerStandardPricingHead
	pool := testutil.SetupMigratedThroughTestDB(t, predecessor)
	ctx := t.Context()
	dir := testutil.MigrationsDir(t)
	require.Error(t, migrate.RunRuntime(ctx, pool, dir))
	var head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, predecessor, head)
	require.NoError(t, migrate.RunExisting(ctx, pool, dir))
	require.NoError(t, migrate.RunExisting(ctx, pool, dir))
	require.NoError(t, pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, version, head)
	_, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(dir, version+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, predecessor, head)

	require.NoError(t, migrate.RunExisting(ctx, pool, dir))
	require.NoError(t, pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, version, head)
	org := testutil.NewTestOrg(t, pool)
	_, err = pool.Exec(ctx, `
		INSERT INTO customer_price_table_versions(org_id,table_ref,version,effective_from,entries,created_by)
		VALUES ($1,'retained',1,NOW(),'[{}]'::jsonb,'user:1')`, org)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "retained customer pricing or quote evidence")
	var retained int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM customer_price_table_versions WHERE org_id=$1`, org).Scan(&retained))
	require.Equal(t, 1, retained)
	require.NoError(t, pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, version, head)
}
