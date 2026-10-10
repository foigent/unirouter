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

func TestReceiverFeedCredentialMigrationContractAndDownRefusal(t *testing.T) {
	pool := setupM46RollbackPrestate(t)
	ctx := context.Background()
	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('receiver-feed-migration', 'active', 'standard')
		 RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status, budget_period)
		 VALUES ($1, 'Receiver feed migration', 'receiver-feed-migration', 'active', 'none')
		 RETURNING id`,
		orgID,
	).Scan(&projectID))

	var legacyScheme, legacyOperation string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO api_keys (
		    org_id, name, key_hash, key_prefix, status, purpose
		 ) VALUES ($1, 'legacy default', $2, 'ur-legacy-default', 'active', '')
		 RETURNING verifier_scheme, registration_operation_id`,
		orgID,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	).Scan(&legacyScheme, &legacyOperation))
	require.Equal(t, "legacy_hmac_sha256_v1", legacyScheme)
	require.Empty(t, legacyOperation)

	var receiverID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO api_keys (
		    org_id, project_id, key_type, name, key_hash, key_prefix, status,
		    allowed_ips, allowed_models, purpose, verifier_scheme,
		    registration_operation_id
		 ) VALUES (
		    $1, $2, 'service', 'receiver staged', $3, 'ur-xf1-0123456789abc',
		    'feed_staged', '127.0.0.1/32', '', 'xinsight.feed.read',
		    'receiver_sha256_v1', '0123456789abcdef0123456789abcdef'
		 ) RETURNING id`,
		orgID,
		projectID,
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	).Scan(&receiverID))

	_, err := pool.Exec(ctx,
		`INSERT INTO api_keys (
		    org_id, name, key_hash, key_prefix, status, purpose,
		    verifier_scheme, registration_operation_id
		 ) VALUES (
		    $1, 'invalid generic receiver', $2, 'ur-xf1-fedcba9876543',
		    'active', '', 'receiver_sha256_v1',
		    'fedcba9876543210fedcba9876543210'
		 )`,
		orgID,
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	)
	require.Error(t, err)

	downSQL, err := os.ReadFile(filepath.Join(
		testutil.PreRetirementMigrationsDir(t),
		"000045_v1_receiver_feed_credential.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "later migrations must be reversed first")

	rollbackAsyncMediaImageFromM46Prestate(t, pool)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "serving or staged receiver credentials exist")

	_, err = pool.Exec(ctx, `UPDATE api_keys SET status = 'feed_revoked' WHERE id = $1`, receiverID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var columns int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*)
		 FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'api_keys'
		   AND column_name IN ('verifier_scheme', 'registration_operation_id')`,
	).Scan(&columns))
	require.Zero(t, columns)
}
