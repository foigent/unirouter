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

func TestXInsightConnectorCP2APhaseContractMigrationUpDownAndEvidenceGuard(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	redeemDown, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000062_xinsight_connector_redeem_lifecycle.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(redeemDown))
	require.NoError(t, err)

	var ledger, tables int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000061_xinsight_connector_cp2a_phase_contract'`).Scan(&ledger))
	require.Equal(t, 1, ledger)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='public'
  AND table_name IN ('xinsight_connector_control_bindings','xinsight_connector_phase_operations')`).Scan(&tables))
	require.Equal(t, 2, tables)

	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000061_xinsight_connector_cp2a_phase_contract.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ('000063_cp2a_future_fixture')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='public'
  AND table_name IN ('xinsight_connector_control_bindings','xinsight_connector_phase_operations')`).Scan(&tables))
	require.Equal(t, 2, tables, "a refused rollback must preserve both CP2A tables")
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000063_cp2a_future_fixture'`)
	require.NoError(t, err)

	var forbiddenSecretColumns int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
WHERE table_schema='public'
  AND table_name IN ('xinsight_connector_control_bindings','xinsight_connector_phase_operations')
  AND column_name ~ '(receiver|bearer|raw_token)'`).Scan(&forbiddenSecretColumns))
	require.Zero(t, forbiddenSecretColumns)

	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('connector-cp2a-migration','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Connector CP2A migration','connector-cp2a-migration','active') RETURNING id`,
		orgID,
	).Scan(&projectID))
	const installationID = "xci_AbCdEfGhIjKlMnOpQrStUv"
	_, err = pool.Exec(ctx, `
INSERT INTO xinsight_connector_installations (
    installation_id,org_id,project_id,x_deployment_id,invitation_sha256,
    event_scope,source_instance,source_binding_sha256,desired_state,observed_state)
VALUES ($1,$2,$3,'xid_ZyXwVuTsRqPoNmLkJiHgFe',
        'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        'model.request.completed','source.cp2a',
        'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        'active','provisioning')`, installationID, orgID, projectID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
INSERT INTO xinsight_connector_control_bindings (
    installation_id,org_id,project_id,invitation_token_enc)
VALUES ($1,$2,$3,'opaque-encrypted-invitation')`, installationID, orgID, projectID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET invitation_token_enc=NULL,
    control_token_enc='opaque-encrypted-control',
    control_project_id='project.second-integration',
    control_token_sha256=NULL
WHERE installation_id=$1`, installationID)
	require.Error(t, err, "a nullable digest must not bypass the control-token lifecycle CHECK")

	_, err = pool.Exec(ctx, `
INSERT INTO xinsight_connector_phase_operations (
    operation_id,installation_id,org_id,project_id,
    request_sha256,request_canonical,control_fence,claim_fence,
    claim_token,claim_expires_at,result_sha256,result_canonical)
VALUES ('xco_NNNNNNNNNNNNNNNNNNNNNN',$1,$2,$3,
        'sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
        $4::bytea,1,1,NULL,NULL,NULL,$5::bytea)`,
		installationID, orgID, projectID, []byte(`{"fixture":"request"}`), []byte(`{"fixture":"result"}`))
	require.Error(t, err, "a nullable digest must not bypass the terminal-result CHECK")

	_, err = pool.Exec(ctx, `
INSERT INTO xinsight_connector_phase_operations (
    operation_id,installation_id,org_id,project_id,
    request_sha256,request_canonical,control_fence,claim_fence,
    claim_token,claim_expires_at)
VALUES ('xco_BBBBBBBBBBBBBBBBBBBBBB',$1,$2,$3,
        'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
		$4::bytea,1,1,'11111111111111111111111111111111',NOW()+INTERVAL '1 minute')`,
		installationID, orgID, projectID, []byte(`{"fixture":true}`))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "durable binding or phase evidence exists")

	_, err = pool.Exec(ctx, `DELETE FROM xinsight_connector_phase_operations WHERE installation_id=$1`, installationID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM xinsight_connector_control_bindings WHERE installation_id=$1`, installationID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "exact-case identity cannot be represented by migration 60")

	_, err = pool.Exec(ctx, `DELETE FROM xinsight_connector_installations WHERE installation_id=$1`, installationID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
WHERE version='000061_xinsight_connector_cp2a_phase_contract'`).Scan(&ledger))
	require.Equal(t, 1, ledger)
}
