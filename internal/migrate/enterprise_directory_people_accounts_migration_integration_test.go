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

func TestEnterpriseDirectoryPeopleAccountsMigrationAndRollbackGuard(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()

	var tables, constraints, credentialColumns, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name IN (
		  'enterprise_directory_person_snapshots',
		  'enterprise_directory_account_snapshots',
		  'enterprise_directory_iam_actions'
		)`).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint
		WHERE conname IN (
		  'enterprise_directory_person_snapshots_scope',
		  'enterprise_directory_person_snapshots_status_check',
		  'enterprise_directory_account_snapshots_scope',
		  'enterprise_directory_account_snapshots_binding_check',
		  'enterprise_directory_iam_actions_snapshot_scope',
		  'enterprise_directory_iam_actions_action_check',
		  'enterprise_directory_iam_actions_result_check'
		)`).Scan(&constraints))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public'
		  AND table_name IN (
		    'enterprise_directory_person_snapshots',
		    'enterprise_directory_account_snapshots',
		    'enterprise_directory_iam_actions'
		  )
		  AND column_name ~ '(credential|secret|password|token|key)'`).Scan(&credentialColumns))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000048_prd8_enterprise_directory_people_accounts'`).Scan(&ledger))
	require.Equal(t, 3, tables)
	require.Equal(t, 7, constraints)
	require.Zero(t, credentialColumns)
	require.Equal(t, 1, ledger)

	var orgID, sourceID, snapshotID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('enterprise-directory-people-migration','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO enterprise_directory_sources (
		  org_id,contract_version,source_ref,authority,connection
		) VALUES ($1,'unirouter.enterprise-directory-projection.v1','hcm','hcm','connected') RETURNING id`, orgID).Scan(&sourceID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO enterprise_directory_snapshots (
		  snapshot_ref,org_id,source_id,kind,source_revision,observed_at
		) VALUES ('sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',$1,$2,'full','rev-1',NOW()) RETURNING id`,
		orgID, sourceID).Scan(&snapshotID))
	_, err := pool.Exec(ctx, `
		INSERT INTO enterprise_directory_person_snapshots (
		  snapshot_id,org_id,source_id,person_ref,external_ref,employment_status,
		  source_revision,valid_from
		) VALUES ($1,$2,$3,'person-1','external-1','active','rev-1',NOW())`,
		snapshotID, orgID, sourceID)
	require.NoError(t, err)
	rollbackEmptyPRD8RuntimeControlMigrations(t, pool)
	rollbackEmptyEnterpriseFinanceMigrations(t, pool)

	downSQL, err := os.ReadFile(filepath.Join(
		testutil.PreRetirementMigrationsDir(t),
		"000048_prd8_enterprise_directory_people_accounts.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "projection or IAM evidence exists")

	_, err = pool.Exec(ctx, `DELETE FROM enterprise_directory_snapshots WHERE id=$1`, snapshotID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name IN (
		  'enterprise_directory_person_snapshots',
		  'enterprise_directory_account_snapshots',
		  'enterprise_directory_iam_actions'
		)`).Scan(&tables))
	require.Zero(t, tables)
}
