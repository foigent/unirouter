//go:build integration

package migrate_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestAdminSessionRevocationMigrationConstraintsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	var userID int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'session-migration-user', 'unused', 'org_admin', 'active')
		 RETURNING id`, orgID).Scan(&userID))

	var epoch int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT auth_epoch FROM users WHERE id = $1`, userID).Scan(&epoch))
	require.Zero(t, epoch)
	_, err := pool.Exec(t.Context(), `UPDATE users SET auth_epoch = 2 WHERE id = $1`, userID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE users SET auth_epoch = 1 WHERE id = $1`, userID)
	require.Error(t, err, "database must reject an auth epoch rollback")
	_, err = pool.Exec(t.Context(), `UPDATE users SET password_hash = 'rotated-outside-app' WHERE id = $1`, userID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT auth_epoch FROM users WHERE id = $1`, userID).Scan(&epoch))
	require.Equal(t, int64(3), epoch, "every direct password hash rotation must revoke old sessions")

	now := time.Now().UTC()
	_, err = pool.Exec(t.Context(),
		`INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, bytes.Repeat([]byte{1}, 31), now, now.Add(time.Hour))
	require.Error(t, err, "sid lookup key must be exactly one SHA-256 digest")
	_, err = pool.Exec(t.Context(),
		`INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at) VALUES ($1, $2, $3, $3)`,
		userID, bytes.Repeat([]byte{2}, 32), now)
	require.Error(t, err, "session expiry must be after issuance")

	validHash := bytes.Repeat([]byte{3}, 32)
	_, err = pool.Exec(t.Context(),
		`INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, validHash, now, now.Add(time.Hour))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, userID)
	require.NoError(t, err)
	var sessions int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM admin_sessions WHERE user_id = $1`, userID).Scan(&sessions))
	require.Zero(t, sessions, "deleted users must not leave reusable session rows")
}

func TestAdminSessionRevocationMigrationUnusedDownAndEvidenceGuardsIntegration(t *testing.T) {
	pool, migrationsDir := setupMigratedThrough(t, "000038_v1_admin_session_revocation.up.sql")
	ctx := context.Background()
	downSQL, err := os.ReadFile(filepath.Join(migrationsDir, "000038_v1_admin_session_revocation.down.sql"))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "an unused schema permits a strict pre-activation rollback")
	var tableName string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(to_regclass('admin_sessions')::text, '')`).Scan(&tableName))
	require.Empty(t, tableName)
	var authEpochColumns, ledger int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_schema='public' AND table_name='users' AND column_name='auth_epoch'`).Scan(&authEpochColumns))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='000038_v1_admin_session_revocation'`).Scan(&ledger))
	require.Zero(t, authEpochColumns)
	require.Zero(t, ledger)

	require.NoError(t, migrate.Run(ctx, pool, migrationsDir))
	orgID := testutil.NewTestOrg(t, pool)
	var userID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'session-rollback-guard', 'unused', 'org_admin', 'active')
		 RETURNING id`, orgID).Scan(&userID))
	now := time.Now().UTC()
	require.NoError(t, func() error {
		_, execErr := pool.Exec(ctx,
			`INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at)
			 VALUES ($1, $2, $3, $4)`,
			userID, bytes.Repeat([]byte{4}, 32), now, now.Add(time.Hour))
		return execErr
	}())

	connection, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer connection.Release()
	_, err = connection.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "server-side session evidence exists")
	_, rollbackErr := connection.Exec(ctx, "ROLLBACK")
	require.NoError(t, rollbackErr)
	require.NoError(t, connection.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_sessions WHERE user_id=$1`, userID).Scan(&ledger))
	require.Equal(t, 1, ledger, "refused rollback must preserve session revocation evidence")

	_, err = connection.Exec(ctx, `DELETE FROM admin_sessions WHERE user_id=$1`, userID)
	require.NoError(t, err)
	_, err = connection.Exec(ctx, `UPDATE users SET auth_epoch=1 WHERE id=$1`, userID)
	require.NoError(t, err)
	_, err = connection.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "authentication revocation epoch evidence exists")
	_, rollbackErr = connection.Exec(ctx, "ROLLBACK")
	require.NoError(t, rollbackErr)
	var epoch int64
	require.NoError(t, connection.QueryRow(ctx,
		`SELECT auth_epoch FROM users WHERE id=$1`, userID).Scan(&epoch))
	require.EqualValues(t, 1, epoch, "refused rollback must preserve password-revocation evidence")
}
