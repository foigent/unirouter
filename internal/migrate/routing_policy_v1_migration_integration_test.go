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

func TestP6F08RoutingPolicyV1MigrationUpDownGuardAndRollForward(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()
	migrationsDir := testutil.PreRetirementMigrationsDir(t)

	assertUp := func() {
		t.Helper()
		var channelColumns, keyColumns, ledgerCount int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM information_schema.columns
 WHERE table_schema='public' AND table_name='channels'
   AND column_name IN ('routing_weight','routing_epoch','circuit_epoch','half_open_probe_until','half_open_probe_token')`).Scan(&channelColumns))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM information_schema.columns
 WHERE table_schema='public' AND table_name='channel_keys' AND column_name='state_epoch'`).Scan(&keyColumns))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000025_prd6_routing_policy_v1'`).Scan(&ledgerCount))
		require.Equal(t, 5, channelColumns)
		require.Equal(t, 1, keyColumns)
		require.Equal(t, 1, ledgerCount)
	}
	assertUp()

	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000025_prd6_routing_policy_v1.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "default weights permit controlled schema rollback")

	var remainingColumns, ledgerCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM information_schema.columns
 WHERE table_schema='public'
   AND ((table_name='channels' AND column_name='routing_weight') OR (table_name='channel_keys' AND column_name='state_epoch'))`).Scan(&remainingColumns))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000025_prd6_routing_policy_v1'`).Scan(&ledgerCount))
	require.Zero(t, remainingColumns)
	require.Zero(t, ledgerCount)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	assertUp()

	var channelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (name,provider,base_url,models,priority,routing_weight,status,circuit_state)
VALUES ('p6f08-down-guard','openai','https://provider.invalid','gpt-p6f08-down-guard',10,200,'active','healthy')
RETURNING id`).Scan(&channelID))
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F08 down refused")
	assertUp()

	var weight int
	require.NoError(t, pool.QueryRow(ctx, `SELECT routing_weight FROM channels WHERE id=$1`, channelID).Scan(&weight))
	require.Equal(t, 200, weight, "refused rollback must preserve activated routing configuration")

	_, err = pool.Exec(ctx, `UPDATE channels SET routing_weight=100 WHERE id=$1`, channelID)
	require.NoError(t, err)
	var keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,status,state_epoch)
VALUES ($1,'encrypted-migration-guard','active',2)
RETURNING id`, channelID).Scan(&keyID))
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "P6-F08 down refused")
	assertUp()
	var stateEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT state_epoch FROM channel_keys WHERE id=$1`, keyID).Scan(&stateEpoch))
	require.EqualValues(t, 2, stateEpoch, "refused rollback must preserve activated key-state epochs")
}
