//go:build integration

package completedfeed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPostgresAccessRecorderTracksPullAndMonotonicReceiverAcknowledgement(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, keyID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('feed-control-recorder', 'active', 'standard')
		 RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status, budget_period)
		 VALUES ($1, 'Feed control recorder', 'feed-control-recorder', 'active', 'none')
		 RETURNING id`,
		orgID,
	).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO api_keys (
		    org_id, project_id, key_type, name, key_prefix, key_hash, status,
		    allowed_ips, allowed_models, purpose
		 ) VALUES (
		    $1, $2, 'service', 'feed recorder', 'ur-feed-recorder',
		    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		    'feed_active', '127.0.0.1/32', '', 'xinsight.feed.read'
		 ) RETURNING id`,
		orgID, projectID,
	).Scan(&keyID))
	_, err := pool.Exec(ctx,
		`INSERT INTO completed_execution_feed_connections (
		    org_id, project_id, status, acknowledged_sequence
		 ) VALUES ($1, $2, 'paused', 0)`,
		orgID, projectID,
	)
	require.NoError(t, err)

	recorder := postgresAccessRecorder{db: pool}
	status, err := recorder.RecordAuthenticatedPull(ctx, orgID, projectID, keyID)
	require.NoError(t, err)
	require.Equal(t, ConnectionStatusPaused, status)

	_, err = pool.Exec(ctx,
		`UPDATE completed_execution_feed_connections
		 SET status = 'active'
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	)
	require.NoError(t, err)
	require.NoError(t, recorder.Acknowledge(ctx, orgID, projectID, keyID, 19))
	require.NoError(t, recorder.Acknowledge(ctx, orgID, projectID, keyID, 7))

	var acknowledged int64
	var pullRecorded bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT acknowledged_sequence, last_authenticated_pull_at IS NOT NULL
		 FROM completed_execution_feed_connections
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&acknowledged, &pullRecorded))
	require.EqualValues(t, 19, acknowledged)
	require.True(t, pullRecorded)

	_, err = pool.Exec(ctx, `UPDATE api_keys SET status = 'feed_staged' WHERE id = $1`, keyID)
	require.NoError(t, err)
	require.ErrorIs(t, recorder.Acknowledge(ctx, orgID, projectID, keyID, 20), ErrConnectionMissing)
	_, err = pool.Exec(ctx,
		`UPDATE completed_execution_feed_connections SET status = 'paused'
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	)
	require.NoError(t, err)
	require.NoError(t, recorder.Acknowledge(ctx, orgID, projectID, keyID, 20))
}
