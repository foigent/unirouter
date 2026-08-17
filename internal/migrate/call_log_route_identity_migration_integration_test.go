//go:build integration

package migrate_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCallLogRouteIdentityMigrationBackfillsOnlyAuthoritativeAttempts(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	execMigrationDown(t, ctx, pool, "000067_call_log_route_identity.down.sql")

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('call-log-route-migration','active','customer') RETURNING id`,
	).Scan(&orgID))
	_, err := pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (request_id,org_id,status,pricing_version)
		VALUES ('route-migration-proven',$1,'settled','migration-test')`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO billing_provider_attempts (
		  org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
		  route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty,http_status
		) VALUES ($1,'route-migration-proven','attempt-proven',101,4242,301,1,1,
		  '/v1/chat/completions','provider-a','duplicate-name','settled','known_usage',200)`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO call_logs (request_id,org_id,model,provider,channel,attempt_id,status_code)
		VALUES
		 ('route-migration-proven',$1,'model-a','provider-a','duplicate-name','attempt-proven',200),
		 ('route-migration-unproven',$1,'model-b','provider-b','duplicate-name',NULL,200)`, orgID)
	require.NoError(t, err)

	require.NoError(t, migrate.Run(ctx, pool, testutil.MigrationsDir(t)))
	var provenRoute int64
	var unprovenRoute *int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT route_channel_id FROM call_logs
		WHERE org_id=$1 AND request_id='route-migration-proven'`, orgID).Scan(&provenRoute))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT route_channel_id FROM call_logs
		WHERE org_id=$1 AND request_id='route-migration-unproven'`, orgID).Scan(&unprovenRoute))
	require.Equal(t, int64(4242), provenRoute)
	require.Nil(t, unprovenRoute, "a duplicate display name is not route identity evidence")

	var indexExists, ledgerExists bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT to_regclass('public.idx_call_logs_org_route_channel_created') IS NOT NULL,
		       EXISTS (SELECT 1 FROM schema_migrations WHERE version='000067_call_log_route_identity')`,
	).Scan(&indexExists, &ledgerExists))
	require.True(t, indexExists)
	require.True(t, ledgerExists)
}
