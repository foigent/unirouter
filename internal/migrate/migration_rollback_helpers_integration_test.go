//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

var enterpriseFinanceDownMigrations = []string{
	"000051_prd8_enterprise_finance_allocation_close.down.sql",
	"000050_prd8_enterprise_finance_rate_cards.down.sql",
	"000049_prd8_enterprise_finance_budgeting.down.sql",
}

var prd8RuntimeControlDownMigrations = []string{
	"000062_xinsight_connector_redeem_lifecycle.down.sql",
	"000061_xinsight_connector_cp2a_phase_contract.down.sql",
	"000060_xinsight_connector_cp1.down.sql",
	"000059_enterprise_access_provisioning.down.sql",
	"000058_enterprise_project_role_separation.down.sql",
	"000057_second_integration_reset_support.down.sql",
	"000056_prd8_invocation_optimization.down.sql",
	"000055_prd8_deployment_profiles.down.sql",
	"000054_prd8_workload_applications.down.sql",
	"000053_prd8_model_call_authorization.down.sql",
	"000052_prd8_runtime_change_control.down.sql",
}

var enterpriseDirectoryDownMigrations = []string{
	"000048_prd8_enterprise_directory_people_accounts.down.sql",
	"000047_prd8_enterprise_directory_foundation.down.sql",
}

func rollbackEmptyMigrations(t *testing.T, pool *pgxpool.Pool, names []string) {
	t.Helper()
	ctx := context.Background()
	for _, name := range names {
		wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(wire))
		require.NoError(t, err, name)
	}
}

func rollbackEmptyEnterpriseFinanceMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rollbackEmptyMigrations(t, pool, enterpriseFinanceDownMigrations)
}

func rollbackEmptyPRD8RuntimeControlMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rollbackEmptyMigrations(t, pool, prd8RuntimeControlDownMigrations)
}

func rollbackEmptyPRD8Migrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rollbackEmptyPRD8RuntimeControlMigrations(t, pool)
	rollbackEmptyEnterpriseFinanceMigrations(t, pool)
	rollbackEmptyMigrations(t, pool, enterpriseDirectoryDownMigrations)
}

func rollbackAsyncMediaImageMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rollbackEmptyPRD8Migrations(t, pool)
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000046_v1_async_media_image_jobs.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), string(downSQL))
	require.NoError(t, err)
}
