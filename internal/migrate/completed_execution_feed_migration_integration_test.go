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

func TestCompletedExecutionFeedPurposeCredentialIsLegacyBinaryFailClosed(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('feed-rollback-guard', 'active', 'standard')
		 RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status, budget_period)
		 VALUES ($1, 'Feed rollback guard', 'feed-rollback-guard', 'active', 'none')
		 RETURNING id`,
		orgID,
	).Scan(&projectID))

	var keyID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO api_keys (
		    org_id, project_id, key_type, name, key_prefix, key_hash,
		    status, allowed_ips, allowed_models, purpose
		 ) VALUES (
		    $1, $2, 'service', 'X-Insight feed', 'ur-feed-rollback',
		    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		    'feed_active', '127.0.0.1/32', '', 'xinsight.feed.read'
		 ) RETURNING id`,
		orgID, projectID,
	).Scan(&keyID))

	// This is the exact status shape understood by binaries predating purpose:
	// only status='active' is accepted. The reserved feed-active state is
	// therefore denied even when the old SELECT does not project purpose.
	var legacyStatus, legacyAllowedModels string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, allowed_models FROM api_keys WHERE id = $1`,
		keyID,
	).Scan(&legacyStatus, &legacyAllowedModels))
	require.Equal(t, "feed_active", legacyStatus)
	require.NotEqual(t, "active", legacyStatus)
	require.Empty(t, legacyAllowedModels)

	_, err := pool.Exec(ctx,
		`UPDATE api_keys SET status = 'active' WHERE id = $1`,
		keyID,
	)
	require.ErrorContains(t, err, "api_keys_completed_feed_status_check")

	// Coordinated binary rollback is operationally supported only after the
	// exact feed credential is disabled. Older binaries continue to deny it.
	_, err = pool.Exec(ctx,
		`UPDATE api_keys SET status = 'disabled' WHERE id = $1`,
		keyID,
	)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM api_keys WHERE id = $1`,
		keyID,
	).Scan(&legacyStatus))
	require.Equal(t, "disabled", legacyStatus)

	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000043_v1_completed_execution_feed.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "scoped credentials exist")

	// Schema rollback remains intentionally stricter than binary rollback: the
	// retired exact credential must be removed before dropping its semantics.
	_, err = pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, keyID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var purposeColumn int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'api_keys'
		   AND column_name = 'purpose'`,
	).Scan(&purposeColumn))
	require.Zero(t, purposeColumn)
}

func TestCompletedExecutionFeedMigrationDownRemainsAvailableWithoutFeedState(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000043_v1_completed_execution_feed.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), string(downSQL))
	require.NoError(t, err)
}
