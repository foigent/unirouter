//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

const (
	redeemLifecycleInstallationID = "xci_AbCdEfGhIjKlMnOpQrStUv"
	redeemLifecycleTokenSHA       = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	redeemLifecycleRequestSHA     = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	redeemLifecycleResultSHA      = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

func TestXInsightConnectorRedeemLifecycleMigrationRefusesLegacyActiveAuthorityAtomically(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000061_xinsight_connector_cp2a_phase_contract.up.sql")
	ctx := context.Background()
	orgID, projectID := insertRedeemLifecycleInstallation(t, pool)
	_, err := pool.Exec(ctx, `
INSERT INTO xinsight_connector_control_bindings (
    installation_id,org_id,project_id,control_project_id,
    control_token_enc,control_token_sha256)
VALUES ($1,$2,$3,'project.second-integration',
        'opaque-encrypted-control',$4)`,
		redeemLifecycleInstallationID, orgID, projectID, redeemLifecycleTokenSHA,
	)
	require.NoError(t, err)

	err = migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000062_xinsight_connector_redeem_lifecycle")
	require.ErrorContains(t, err, "existing control bearer lacks expiry and redeem identities")

	var columns, ledger, bindings int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
WHERE table_schema='public'
  AND table_name='xinsight_connector_control_bindings'
  AND column_name IN ('control_token_expires_at','redeem_request_sha256','redeem_result_sha256')`).Scan(&columns))
	require.Zero(t, columns, "a refused migration must roll back every added column")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000062_xinsight_connector_redeem_lifecycle'`).Scan(&ledger))
	require.Zero(t, ledger, "a refused migration must not advance the schema ledger")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM xinsight_connector_control_bindings
WHERE installation_id=$1 AND control_token_enc='opaque-encrypted-control'`,
		redeemLifecycleInstallationID,
	).Scan(&bindings))
	require.Equal(t, 1, bindings, "legacy authority must remain untouched on refusal")
}

func TestXInsightConnectorRedeemLifecycleMigrationEnforcesThreeStateMatrix(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000061_xinsight_connector_cp2a_phase_contract.up.sql")
	ctx := context.Background()
	orgID, projectID := insertRedeemLifecycleInstallation(t, pool)
	_, err := pool.Exec(ctx, `
INSERT INTO xinsight_connector_control_bindings (
    installation_id,org_id,project_id,invitation_token_enc)
VALUES ($1,$2,$3,'opaque-encrypted-invitation')`,
		redeemLifecycleInstallationID, orgID, projectID,
	)
	require.NoError(t, err)
	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000062_xinsight_connector_redeem_lifecycle"))

	var columns, ledger int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
WHERE table_schema='public'
  AND table_name='xinsight_connector_control_bindings'
  AND column_name IN ('control_token_expires_at','redeem_request_sha256','redeem_result_sha256')`).Scan(&columns))
	require.Equal(t, 3, columns)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000062_xinsight_connector_redeem_lifecycle'`).Scan(&ledger))
	require.Equal(t, 1, ledger)

	_, err = pool.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET control_project_id='project.second-integration',
    control_token_enc='opaque-encrypted-control',
    control_token_sha256=$2,
    control_token_expires_at=created_at+INTERVAL '1 hour'
WHERE installation_id=$1`, redeemLifecycleInstallationID, redeemLifecycleTokenSHA)
	require.Error(t, err, "redeem_pending must include the immutable request identity")

	_, err = pool.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET control_project_id='project.second-integration',
    control_token_enc='opaque-encrypted-control',
    control_token_sha256=$2,
    control_token_expires_at=created_at+INTERVAL '1 hour',
    redeem_request_sha256=$3
WHERE installation_id=$1`,
		redeemLifecycleInstallationID, redeemLifecycleTokenSHA, redeemLifecycleRequestSHA,
	)
	require.NoError(t, err, "invitation_staged must transition atomically to redeem_pending")

	_, err = pool.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET invitation_token_enc=NULL
WHERE installation_id=$1`, redeemLifecycleInstallationID)
	require.Error(t, err, "control_active must not retire the invitation before a validated result")

	_, err = pool.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET invitation_token_enc=NULL,redeem_result_sha256=$2
WHERE installation_id=$1`, redeemLifecycleInstallationID, redeemLifecycleResultSHA)
	require.NoError(t, err, "redeem_pending must transition atomically to control_active")

	_, err = pool.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET invitation_token_enc='opaque-revived-invitation'
WHERE installation_id=$1`, redeemLifecycleInstallationID)
	require.Error(t, err, "control_active must not regain invitation authority")
	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000062_xinsight_connector_redeem_lifecycle.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "pending or active control authority exists")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000062_xinsight_connector_redeem_lifecycle'`).Scan(&ledger))
	require.Equal(t, 1, ledger, "a refused rollback must preserve the schema ledger")
}

func TestXInsightConnectorRedeemLifecycleMigrationAllowsRepresentableStagedRollback(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000061_xinsight_connector_cp2a_phase_contract.up.sql")
	ctx := context.Background()
	orgID, projectID := insertRedeemLifecycleInstallation(t, pool)
	_, err := pool.Exec(ctx, `
INSERT INTO xinsight_connector_control_bindings (
    installation_id,org_id,project_id,invitation_token_enc)
VALUES ($1,$2,$3,'opaque-encrypted-invitation')`,
		redeemLifecycleInstallationID, orgID, projectID,
	)
	require.NoError(t, err)
	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000062_xinsight_connector_redeem_lifecycle"))
	down, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000062_xinsight_connector_redeem_lifecycle.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000063_future_contract')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000063_future_contract'`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	var columns, ledger, staged int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
WHERE table_schema='public'
  AND table_name='xinsight_connector_control_bindings'
  AND column_name IN ('control_token_expires_at','redeem_request_sha256','redeem_result_sha256')`).Scan(&columns))
	require.Zero(t, columns)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000062_xinsight_connector_redeem_lifecycle'`).Scan(&ledger))
	require.Zero(t, ledger)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM xinsight_connector_control_bindings
WHERE installation_id=$1
  AND invitation_token_enc='opaque-encrypted-invitation'
  AND control_token_enc IS NULL`, redeemLifecycleInstallationID).Scan(&staged))
	require.Equal(t, 1, staged, "schema-61-representable invitation authority must survive rollback")

	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000062_xinsight_connector_redeem_lifecycle"))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000062_xinsight_connector_redeem_lifecycle'`).Scan(&ledger))
	require.Equal(t, 1, ledger, "a staged binding must support a clean migration-62 reapply")
}

func insertRedeemLifecycleInstallation(t *testing.T, pool *pgxpool.Pool) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('connector-redeem-lifecycle','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO projects (org_id,name,slug,status)
VALUES ($1,'Connector redeem lifecycle','connector-redeem-lifecycle','active') RETURNING id`, orgID).Scan(&projectID))
	_, err := pool.Exec(ctx, `
INSERT INTO xinsight_connector_installations (
    installation_id,org_id,project_id,x_deployment_id,invitation_sha256,
    event_scope,source_instance,source_binding_sha256,desired_state,observed_state)
VALUES ($1,$2,$3,'xid_ZyXwVuTsRqPoNmLkJiHgFe',
        'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        'model.request.completed','source.redeem-lifecycle',
        'sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',
        'active','provisioning')`, redeemLifecycleInstallationID, orgID, projectID)
	require.NoError(t, err)
	return orgID, projectID
}
