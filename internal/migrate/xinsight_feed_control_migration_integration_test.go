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

func TestXInsightFeedControlMigrationEnforcesStagedAndRollbackLifecycle(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('feed-control-migration', 'active', 'standard')
		 RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status, budget_period)
		 VALUES ($1, 'Feed control migration', 'feed-control-migration', 'active', 'none')
		 RETURNING id`,
		orgID,
	).Scan(&projectID))

	insertKey := func(prefix, hash string) error {
		_, err := pool.Exec(ctx,
			`INSERT INTO api_keys (
			    org_id, project_id, key_type, name, key_prefix, key_hash, status,
			    allowed_ips, allowed_models, purpose
			 ) VALUES ($1, $2, 'service', $3, $3, $4, 'feed_staged',
			           '127.0.0.1/32', '', 'xinsight.feed.read')`,
			orgID, projectID, prefix, hash,
		)
		return err
	}
	require.NoError(t, insertKey(
		"ur-feed-staged-one",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	))
	require.Error(t, insertKey(
		"ur-feed-staged-two",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	), "one project cannot have two staged replacements")

	receiverDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000045_v1_receiver_feed_credential.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(receiverDownSQL))
	require.NoError(t, err)

	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000044_v1_xinsight_feed_control.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "serving or staged feed credentials exist")

	_, err = pool.Exec(ctx,
		`UPDATE api_keys SET status = 'feed_revoked'
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM api_keys WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&status))
	require.Equal(t, "disabled", status)
	var connectionTable int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = 'public'
		   AND table_name = 'completed_execution_feed_connections'`,
	).Scan(&connectionTable))
	require.Zero(t, connectionTable)
}
