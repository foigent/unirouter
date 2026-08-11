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

func TestWorkloadApplicationMigrationScopeAndRollbackGuard(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	rollbackEmptyMigrations(t, pool, []string{
		"000060_xinsight_connector_cp1.down.sql",
		"000059_enterprise_access_provisioning.down.sql",
		"000058_enterprise_project_role_separation.down.sql",
		"000057_second_integration_reset_support.down.sql",
		"000056_prd8_invocation_optimization.down.sql",
		"000055_prd8_deployment_profiles.down.sql",
	})

	var tables, columns, constraints, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name='workload_applications'`).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='api_keys'
		  AND column_name='workload_application_id'`).Scan(&columns))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint WHERE conname IN (
		  'workload_applications_identity_unique',
		  'workload_applications_project_scope_fk',
		  'workload_applications_lifecycle_check',
		  'api_keys_workload_application_scope_fk')`).Scan(&constraints))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000054_prd8_workload_applications'`).Scan(&ledger))
	require.Equal(t, 1, tables)
	require.Equal(t, 1, columns)
	require.Equal(t, 4, constraints)
	require.Equal(t, 1, ledger)

	type tenant struct{ orgID, userID, projectID int64 }
	seed := func(suffix string) tenant {
		t.Helper()
		var value tenant
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','standard') RETURNING id`, "workload-migration-"+suffix).Scan(&value.orgID))
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users (org_id,username,password_hash,role,status) VALUES ($1,$2,'hash','org_admin','active') RETURNING id`, value.orgID, "workload-owner-"+suffix).Scan(&value.userID))
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,$2,$3,'active') RETURNING id`, value.orgID, "Workload "+suffix, "workload-"+suffix).Scan(&value.projectID))
		return value
	}
	a := seed("a")
	b := seed("b")

	_, err := pool.Exec(ctx, `
		INSERT INTO workload_applications (
		  org_id,project_id,application_ref,display_name,kind,environment,
		  identity_mode,client_profile,status,created_by,updated_by)
		VALUES ($1,$2,'app_cross_scope','Cross Scope','agent','poc',
		  'service_credential','client.openai-sdk.chat.v1','active',$3,$3)`, a.orgID, b.projectID, a.userID)
	require.Error(t, err)

	var applicationID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO workload_applications (
		  org_id,project_id,application_ref,display_name,kind,environment,
		  identity_mode,client_profile,status,created_by,updated_by)
		VALUES ($1,$2,'app_migration_guard','Migration Guard','agent','poc',
		  'service_credential','client.openai-sdk.chat.v1','active',$3,$3)
		RETURNING id`, a.orgID, a.projectID, a.userID).Scan(&applicationID))

	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000054_prd8_workload_applications.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "application or credential evidence exists")
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM workload_applications WHERE id=$1`, applicationID).Scan(&tables))
	require.Equal(t, 1, tables)

	require.NoError(t, pool.QueryRow(ctx, `DELETE FROM workload_applications WHERE id=$1 RETURNING 1`, applicationID).Scan(&tables))
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000054_prd8_workload_applications'`).Scan(&ledger))
	require.Zero(t, ledger)
}
