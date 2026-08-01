//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseDirectoryFoundationMigrationAndRollbackGuard(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var tables, constraints, credentialColumns, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name IN (
		  'enterprise_directory_sources',
		  'enterprise_directory_snapshots',
		  'enterprise_directory_unit_snapshots'
		)`).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname IN (
		  'enterprise_directory_sources_identity_unique',
		  'enterprise_directory_sources_observation_shape_check',
		  'enterprise_directory_snapshots_revision_unique',
		  'enterprise_directory_snapshots_delta_parent_check',
		  'enterprise_directory_unit_snapshots_scope',
		  'enterprise_directory_unit_snapshots_window_check'
		)`).Scan(&constraints))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public'
		  AND table_name LIKE 'enterprise_directory_%'
		  AND column_name ~ '(credential|secret|password|token|key)'`).Scan(&credentialColumns))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000047_prd8_enterprise_directory_foundation'`).Scan(&ledger))
	require.Equal(t, 3, tables)
	require.Equal(t, 6, constraints)
	require.Zero(t, credentialColumns)
	require.Equal(t, 1, ledger)

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('enterprise-directory-migration', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))
	_, err := pool.Exec(ctx, `
		INSERT INTO enterprise_directory_sources (
		  org_id, contract_version, source_ref, authority, connection
	) VALUES ($1,'unirouter.enterprise-directory-projection.v1','hcm','hcm','unconfigured')`, orgID)
	require.NoError(t, err)
	rollbackEmptyPRD8RuntimeControlMigrations(t, pool)
	rollbackEmptyEnterpriseFinanceMigrations(t, pool)

	peopleAccountsDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000048_prd8_enterprise_directory_people_accounts.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(peopleAccountsDownSQL))
	require.NoError(t, err)

	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000047_prd8_enterprise_directory_foundation.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "directory projection data exists")

	_, err = pool.Exec(ctx, `DELETE FROM enterprise_directory_sources WHERE org_id = $1`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name LIKE 'enterprise_directory_%'`,
	).Scan(&tables))
	require.Zero(t, tables)
}
