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

func TestXInsightConnectorCP1MigrationUpDownAndEvidenceGuard(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var ledger, tables int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations WHERE version='000060_xinsight_connector_cp1'`).Scan(&ledger))
	require.Equal(t, 1, ledger)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='public'
  AND table_name IN ('xinsight_connector_installations','xinsight_connector_operations')`).Scan(&tables))
	require.Equal(t, 2, tables)
	rollbackEmptyMigrations(t, pool, []string{
		"000064_rate_card_request_pricing_authority.down.sql",
		"000063_onprem_billing_mode.down.sql",
	})
	redeemDown, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000062_xinsight_connector_redeem_lifecycle.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(redeemDown))
	require.NoError(t, err)
	cp2Down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000061_xinsight_connector_cp2a_phase_contract.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(cp2Down))
	require.NoError(t, err)

	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('connector-migration','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Connector migration','connector-migration','active') RETURNING id`,
		orgID,
	).Scan(&projectID))
	_, err = pool.Exec(ctx, `
INSERT INTO xinsight_connector_installations (
    installation_id,org_id,project_id,x_deployment_id,invitation_sha256,
    event_scope,source_instance,source_binding_sha256,desired_state,observed_state)
VALUES ('migration-installation',$1,$2,'migration-x',
        'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        'model.request.completed','source.migration',
        'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        'active','provisioning')`, orgID, projectID)
	require.NoError(t, err)

	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000060_xinsight_connector_cp1.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "durable installation or operation evidence exists")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='000060_xinsight_connector_cp1'`,
	).Scan(&ledger))
	require.Equal(t, 1, ledger)

	_, err = pool.Exec(ctx, `DELETE FROM xinsight_connector_installations`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema='public'
  AND table_name IN ('xinsight_connector_installations','xinsight_connector_operations')`).Scan(&tables))
	require.Zero(t, tables)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='000060_xinsight_connector_cp1'`,
	).Scan(&ledger))
	require.Zero(t, ledger)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='000060_xinsight_connector_cp1'`,
	).Scan(&ledger))
	require.Equal(t, 1, ledger)
}
