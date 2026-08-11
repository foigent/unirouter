//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestInvocationOptimizationMigrationSafetyAndRollbackGuards(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	rollbackEmptyMigrations(t, pool, []string{
		"000062_xinsight_connector_redeem_lifecycle.down.sql",
		"000061_xinsight_connector_cp2a_phase_contract.down.sql",
		"000060_xinsight_connector_cp1.down.sql",
		"000059_enterprise_access_provisioning.down.sql",
		"000058_enterprise_project_role_separation.down.sql",
		"000057_second_integration_reset_support.down.sql",
	})

	var tables, columns, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name IN (
		  'invocation_optimization_policies',
		  'invocation_optimization_policy_revisions',
		  'invocation_optimization_receipts',
		  'response_cache_delivery_receipts',
		  'response_cache_commercial_receipts')`).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='api_keys' AND column_name='auth_epoch'`).Scan(&columns))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000056_prd8_invocation_optimization'`).Scan(&ledger))
	require.Equal(t, 5, tables)
	require.Equal(t, 1, columns)
	require.Equal(t, 1, ledger)

	var orgID, userID, projectID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('Invocation optimization migration','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status)
		VALUES ($1,'invocation-opt-owner','hash','org_admin','active') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status,created_by)
		VALUES ($1,'Invocation optimization','invocation-optimization','active',$2) RETURNING id`, orgID, userID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (org_id,project_id,name,key_prefix,key_hash,status)
		VALUES ($1,$2,'migration-key','ur-mig-opt-000001',$3,'active') RETURNING id`,
		orgID, projectID, strings.Repeat("a", 64)).Scan(&keyID))

	var epoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT auth_epoch FROM api_keys WHERE id=$1`, keyID).Scan(&epoch))
	require.EqualValues(t, 1, epoch)
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx, `UPDATE api_keys SET name='migration-key-updated' WHERE id=$1`, keyID)
		return err
	}())
	require.NoError(t, pool.QueryRow(ctx, `SELECT auth_epoch FROM api_keys WHERE id=$1`, keyID).Scan(&epoch))
	require.EqualValues(t, 2, epoch)

	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000056_prd8_invocation_optimization.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "policy or runtime evidence exists")
	require.NoError(t, func() error {
		_, deleteErr := pool.Exec(ctx, `DELETE FROM api_keys WHERE id=$1`, keyID)
		return deleteErr
	}())

	_, err = pool.Exec(ctx, `
		INSERT INTO invocation_optimization_policies (
		  org_id,project_id,policy_ref,display_name,model_selector,
		  created_by,updated_by)
		VALUES ($1,$2,'opt_migration_guard','Migration guard','*',$3,$3)`, orgID, projectID, userID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "policy or runtime evidence exists")
	require.NoError(t, func() error {
		_, deleteErr := pool.Exec(ctx, `DELETE FROM invocation_optimization_policies WHERE org_id=$1`, orgID)
		return deleteErr
	}())

	_, err = pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (
		  request_id,org_id,frozen_amount_micros,reserved_balance_micros,status,
		  prompt_tokens,uncached_prompt_tokens,completion_tokens,
		  usage_source,usage_authority,usage_purpose,usage_finality,
		  usage_contract_version,usage_estimate_version)
		VALUES ('cache-v2-invalid',$1,1,1,'frozen',1,1,0,
		  'response_cache','local_cache','customer_charge','final',
		  'unirouter.usage.v2','')`, orgID)
	require.Error(t, err, "local-cache v2 usage with non-zero tokens must fail closed")

	_, err = pool.Exec(ctx, `
		INSERT INTO request_idempotency (
		  org_id,api_key_id,key_hash,request_fingerprint,operation_id,state,expires_at,
		  usage_authority,usage_purpose,usage_finality,
		  usage_contract_version,usage_estimate_version)
		VALUES ($1,1,repeat('a',64),repeat('b',64),'cache-v2-invalid-authority',
		  'in_progress',NOW()+INTERVAL '1 hour','provider_reported','customer_charge',
		  'final','unirouter.usage.v2','')`, orgID)
	require.Error(t, err, "request idempotency v2 is reserved for local-cache authority")

	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE version='000056_prd8_invocation_optimization'`).Scan(&ledger))
	require.Zero(t, ledger)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name='api_keys' AND column_name='auth_epoch'`).Scan(&columns))
	require.Zero(t, columns)
}
