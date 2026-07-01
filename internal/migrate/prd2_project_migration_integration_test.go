//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := context.Background()

	applyMigrationFiles(t, pool,
		"000001_init_schema.up.sql",
		"000003_add_wallet_features.up.sql",
		"000005_add_org_pricing_and_alert_fields.up.sql",
		"000006_fix_schema_gaps.up.sql",
		"000007_prd_v15_alignment.up.sql",
		"000008_model_price_decimal_cents.up.sql",
	)
	_, err := pool.Exec(ctx, `SELECT setval('orgs_id_seq', (SELECT MAX(id) FROM orgs))`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `SELECT setval('users_id_seq', (SELECT MAX(id) FROM users))`)
	require.NoError(t, err)

	var orgID, userID, apiKeyID int64
	err = pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('prd1-existing-org', 'active', 'standard')
		 RETURNING id`,
	).Scan(&orgID)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'prd1-admin', 'hash', 'org_admin', 'active')
		 RETURNING id`,
		orgID,
	).Scan(&userID)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`INSERT INTO api_keys (org_id, name, key_prefix, key_hash, allowed_models, status)
		 VALUES ($1, 'prd1-key', 'ur-prd1-key', 'hash-prd1-key', 'gpt-4o', 'active')
		 RETURNING id`,
		orgID,
	).Scan(&apiKeyID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO call_logs (request_id, org_id, api_key_id, model, provider, channel, status_code)
		 VALUES ('prd1-log-req', $1, $2, 'gpt-4o', 'mock', 'mock-channel', 200)`,
		orgID, apiKeyID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount)
		 VALUES ('prd1-tx-req', $1, 'settle', 'completed', 1)`,
		orgID,
	)
	require.NoError(t, err)

	applyMigrationFiles(t, pool, "000009_prd2_project_foundation.up.sql")

	var projectID, keyProjectID, logProjectID, txProjectID int64
	err = pool.QueryRow(ctx,
		`SELECT id FROM projects WHERE org_id = $1 AND slug = 'default'`,
		orgID,
	).Scan(&projectID)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`SELECT project_id FROM api_keys WHERE id = $1`,
		apiKeyID,
	).Scan(&keyProjectID)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`SELECT project_id FROM call_logs WHERE request_id = 'prd1-log-req'`,
	).Scan(&logProjectID)
	require.NoError(t, err)
	err = pool.QueryRow(ctx,
		`SELECT project_id FROM transactions WHERE request_id = 'prd1-tx-req'`,
	).Scan(&txProjectID)
	require.NoError(t, err)

	var membershipCount int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM project_members WHERE project_id = $1 AND user_id = $2 AND role = 'project_admin'`,
		projectID, userID,
	).Scan(&membershipCount)
	require.NoError(t, err)

	require.Equal(t, projectID, keyProjectID)
	require.Equal(t, projectID, logProjectID)
	require.Equal(t, projectID, txProjectID)
	require.Equal(t, 1, membershipCount)
}

func applyMigrationFiles(t *testing.T, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, files ...string) {
	t.Helper()
	for _, file := range files {
		sqlBytes, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), file))
		require.NoError(t, err)
		_, err = pool.Exec(context.Background(), string(sqlBytes))
		require.NoError(t, err, "apply migration %s", file)
	}
}
