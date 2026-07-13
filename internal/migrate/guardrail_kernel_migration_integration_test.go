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

func TestP6F10GuardrailKernelMigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.MigrationsDir(t)
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000033_prd6_guardrail_kernel.down.sql"))
	require.NoError(t, err)

	assertUp := func() {
		t.Helper()
		var tables, constraints, ledger int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='guardrail_policies'`).Scan(&tables))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_constraint WHERE conname IN ('guardrail_policy_mandatory_authority','guardrail_policy_config_object','guardrail_policy_sensitive_redact')`).Scan(&constraints))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000033_prd6_guardrail_kernel'`).Scan(&ledger))
		require.Equal(t, 1, tables)
		require.Equal(t, 3, constraints)
		require.Equal(t, 1, ledger)
	}

	assertUp()
	var orgA, orgB, projectB, keyB int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name) VALUES ('p6f10-scope-a') RETURNING id`).Scan(&orgA))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name) VALUES ('p6f10-scope-b') RETURNING id`).Scan(&orgB))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug) VALUES ($1,'P6F10 B','p6f10-b') RETURNING id`, orgB).Scan(&projectB))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_prefix,key_hash) VALUES ($1,$2,'P6F10 B','p6f10-scope-b','hash') RETURNING id`, orgB, projectB).Scan(&keyB))
	_, err = pool.Exec(ctx, `INSERT INTO guardrail_policies (org_id,project_id,name,stage,detector_ref,action,mode,failure_mode,config) VALUES ($1,$2,'foreign-project','input','builtin:pii_secrets','audit','shadow','degraded_allow','{}')`, orgA, projectB)
	require.Error(t, err, "database must reject a cross-organization project selector")
	_, err = pool.Exec(ctx, `INSERT INTO guardrail_policies (org_id,api_key_id,name,stage,detector_ref,action,mode,failure_mode,config) VALUES ($1,$2,'foreign-key','input','builtin:pii_secrets','audit','shadow','degraded_allow','{}')`, orgA, keyB)
	require.Error(t, err, "database must reject a cross-organization API-key selector")

	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "unused guardrail schema permits controlled rollback")
	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()

	_, err = pool.Exec(ctx, `INSERT INTO guardrail_policies (name,stage,detector_ref,action,mode,failure_mode,mandatory,config) VALUES ('mandatory','input','builtin:pii_secrets','block','shadow','fail_closed',TRUE,'{}')`)
	require.Error(t, err, "database must reject a mandatory shadow policy")
	_, err = pool.Exec(ctx, `INSERT INTO guardrail_policies (name,stage,detector_ref,action,mode,failure_mode,mandatory,config) VALUES ('configured','input','builtin:pii_secrets','audit','shadow','degraded_allow',FALSE,'{}')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F10 rollback refused")
	assertUp()
}
