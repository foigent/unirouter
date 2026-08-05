//go:build integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestResetPreviewCLIInventoriesMigratedDatabaseWithoutMutation(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("preview-integration-fixture"), bcrypt.MinCost)
	require.NoError(t, err)
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('Preview Admin Shell', 'active', 'partner') RETURNING id`).Scan(&orgID))
	_, err = pool.Exec(ctx, `
		INSERT INTO users (org_id, username, password_hash, role, status, auth_provider)
		VALUES ($1, 'Admin', $2, 'platform_admin', 'active', 'local')`, orgID, string(passwordHash))
	require.NoError(t, err)
	var channelID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (name, provider, base_url, status)
		VALUES ('reset-preview', 'fixture', 'https://example.invalid', 'disabled')
		RETURNING id`).Scan(&channelID))

	redisServer := miniredis.RunT(t)
	output := filepath.Join(t.TempDir(), "final-preview.json")
	cfg := &config.Config{
		DB_DSN: pool.Config().ConnString(), DBMaxConnections: 4, DBMaxIdleConns: 0,
		RedisAddr: redisServer.Addr(), RedisMaxConns: 4,
		RuntimeVersion: "v2.0.2", RuntimeCommit: strings.Repeat("a", 40),
		CompletedExecutionFeedSourceInstance: "source-before",
	}
	err = runResetCLI(ctx, []string{
		"preview",
		"--next-source-instance", "source-after",
		"--target-tree", strings.Repeat("b", 40),
		"--preserve-local-login", "admin",
		"--output", output,
	}, cfg)
	require.NoError(t, err)
	payload, err := os.ReadFile(output)
	require.NoError(t, err)
	plan, err := resetplan.Unmarshal(payload)
	require.NoError(t, err)
	require.True(t, plan.ScopeComplete)
	require.True(t, plan.ApplySupported)
	require.False(t, plan.ActionAuthorized)
	require.False(t, plan.MutationPerformed)
	info, err := os.Stat(output)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o077)

	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM channels WHERE id=$1`, channelID).Scan(&rows))
	require.Equal(t, 1, rows, "read-only preview changed product data")
}
