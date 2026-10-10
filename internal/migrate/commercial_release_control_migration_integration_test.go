//go:build integration

package migrate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestV1CommercialReleaseControlMigrationAndConcurrentResourceLimit(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var releaseSequence int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT release_sequence FROM release_state WHERE singleton = TRUE`,
	).Scan(&releaseSequence))
	require.Zero(t, releaseSequence)

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('commercial-limit-test', 'active', 'customer') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commercial_license_entitlements (
			singleton, license_id, deployment_id, key_id, expires_at, features, active_users
		) VALUES (TRUE, 'license-test', 'deployment-test', 'license-key-test', NOW() + INTERVAL '1 day', '["*"]'::jsonb, 1)
		RETURNING active_users
	`).Scan(new(int64)))

	const attempts = 16
	var successes atomic.Int64
	winner := make(chan int64, attempts)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			var userID int64
			err := pool.QueryRow(ctx,
				`INSERT INTO users (org_id, username, password_hash, role, status)
				 VALUES ($1, $2, 'hash', 'viewer', 'active') RETURNING id`,
				orgID, fmt.Sprintf("concurrent-user-%d", index),
			).Scan(&userID)
			if err == nil {
				successes.Add(1)
				winner <- userID
			}
		}(i)
	}
	close(start)
	wait.Wait()
	close(winner)
	require.EqualValues(t, 1, successes.Load(), "concurrent replicas must not exceed the signed active_users limit")
	winnerID := <-winner

	_, err := pool.Exec(ctx, `UPDATE users SET status = 'disabled' WHERE id = $1`, winnerID)
	require.NoError(t, err)
	var replacementID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'replacement-user', 'hash', 'viewer', 'active') RETURNING id`, orgID,
	).Scan(&replacementID))
	require.NotZero(t, replacementID)

	_, err = pool.Exec(ctx, `UPDATE users SET status = 'active' WHERE id = $1`, winnerID)
	require.Error(t, err, "inactive-to-active transition must consume signed capacity")

	var activeUsers int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE status = 'active'`).Scan(&activeUsers))
	require.EqualValues(t, 1, activeUsers)

	_, err = pool.Exec(ctx, `UPDATE commercial_license_entitlements SET expires_at = NOW() - INTERVAL '1 minute' WHERE singleton = TRUE`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE users SET status = 'disabled' WHERE id = $1`, replacementID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id, username, password_hash, role, status)
		VALUES ($1, 'expired-license-user', 'hash', 'viewer', 'active')`, orgID)
	require.Error(t, err, "expired entitlement snapshot must block new active resources")
	require.Contains(t, err.Error(), "commercial License is expired")

	downSQL, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000037_v1_commercial_release_control.down.sql"))
	require.NoError(t, err)
	connection, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer connection.Release()
	_, downErr := connection.Exec(ctx, string(downSQL))
	require.Error(t, downErr, "downgrade must refuse to erase an active entitlement snapshot")
	require.True(t, strings.Contains(downErr.Error(), "downgrade refused"), "unexpected downgrade error: %v", downErr)
	_, rollbackErr := connection.Exec(ctx, "ROLLBACK")
	require.NoError(t, rollbackErr)
	var entitlementTable string
	require.NoError(t, pool.QueryRow(ctx, `SELECT COALESCE(to_regclass('commercial_license_entitlements')::text, '')`).Scan(&entitlementTable))
	require.Equal(t, "commercial_license_entitlements", entitlementTable)
}
