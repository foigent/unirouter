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

func TestPRD8RuntimeControlMigrationsAndRollbackGuards(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var tables, constraints, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name IN (
		  'runtime_change_releases','runtime_change_slots','runtime_change_events',
		  'model_call_authorizations')`).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pg_constraint WHERE conname IN (
		  'channels_runtime_status_check','runtime_change_releases_identity_check',
		  'runtime_change_releases_lifecycle_check','runtime_change_slots_canary_check',
		  'runtime_change_events_version_check','model_call_authorizations_identity_check',
		  'model_call_authorizations_lifecycle_check')`).Scan(&constraints))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations WHERE version IN (
		  '000052_prd8_runtime_change_control','000053_prd8_model_call_authorization')`).Scan(&ledger))
	require.Equal(t, 4, tables)
	require.Equal(t, 7, constraints)
	require.Equal(t, 2, ledger)

	// This test exercises the 000052/000053 rollback guards directly. Newer,
	// empty PRD8 migrations must be reversed first so their ordering guard does
	// not mask the evidence-specific assertions below.
	rollbackEmptyMigrations(t, pool, []string{
		"000056_prd8_invocation_optimization.down.sql",
		"000055_prd8_deployment_profiles.down.sql",
		"000054_prd8_workload_applications.down.sql",
	})

	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('prd8-runtime-migration','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status)
		VALUES ($1,'prd8-runtime-owner','test-hash','platform_admin','active') RETURNING id`, orgID).Scan(&userID))

	_, err := pool.Exec(ctx, `
		INSERT INTO model_call_authorizations (
		  org_id,authorization_ref,subject_type,subject_ref,environment,effect,
		  allowed_models,allowed_endpoints,status,reason,created_by)
		VALUES ($1,'mca_rollback_guard','agent','agent:guard','*','deny',
		  '["*"]','["/v1/chat/completions"]','draft','rollback guard',$2)`, orgID, userID)
	require.NoError(t, err)
	authorizationDown := readPRD8DownMigration(t, "000053_prd8_model_call_authorization.down.sql")
	_, err = pool.Exec(ctx, authorizationDown)
	require.ErrorContains(t, err, "authorization evidence exists")
	_, err = pool.Exec(ctx, `DELETE FROM model_call_authorizations`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, authorizationDown)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO runtime_change_releases (
		  release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
		  candidate,provider_pack_pins,simulation_report,overall_risk,stage,reason,created_by)
		VALUES ('rcr_rollback_guard','gpt-runtime','unirouter.runtime-change-candidate.v1',
		  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		  'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
		  '{"contract_version":"unirouter.runtime-change-candidate.v1","model_name":"gpt-runtime","overrides":[]}',
		  '[]','{}','unchanged','draft','rollback guard',$1)`, userID)
	require.NoError(t, err)
	runtimeDown := readPRD8DownMigration(t, "000052_prd8_runtime_change_control.down.sql")
	_, err = pool.Exec(ctx, runtimeDown)
	require.ErrorContains(t, err, "release evidence exists")
	_, err = pool.Exec(ctx, `DELETE FROM runtime_change_releases`)
	require.NoError(t, err)

	var channelID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (name,provider,base_url,models,status)
		VALUES ('staged-rollback-guard','openai','https://provider.invalid','gpt-runtime','staged')
		RETURNING id`).Scan(&channelID))
	_, err = pool.Exec(ctx, runtimeDown)
	require.ErrorContains(t, err, "staged channels exist")
	_, err = pool.Exec(ctx, `UPDATE channels SET status='disabled' WHERE id=$1`, channelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, runtimeDown)
	require.NoError(t, err)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations WHERE version IN (
		  '000052_prd8_runtime_change_control','000053_prd8_model_call_authorization')`).Scan(&ledger))
	require.Zero(t, ledger)
}

func readPRD8DownMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), name))
	require.NoError(t, err)
	return string(body)
}
