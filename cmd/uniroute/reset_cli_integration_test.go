//go:build integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/resetexec"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestResetPreviewCLIReportsM118IncompleteScopeWithoutMutation(t *testing.T) {
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
	require.NoError(t, redisServer.Set("wallet:v2:must-remain", "synthetic"))
	before, err := resetexec.Inspect(ctx, pool, resetplan.RedisObservation{})
	require.NoError(t, err)
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
	require.NoError(t, err, "read-only diagnostic preview remains available")
	payload, err := os.ReadFile(output)
	require.NoError(t, err)
	var plan resetplan.Plan
	require.NoError(t, json.Unmarshal(payload, &plan))
	require.False(t, plan.ScopeComplete)
	require.False(t, plan.ActionAuthorized)
	require.False(t, plan.MutationPerformed)
	_, err = resetplan.Unmarshal(payload)
	require.ErrorIs(t, err, resetplan.ErrInvalid, "loading this preview as an executable plan is refused")
	require.ErrorContains(t, err, "contract_or_scope")
	info, err := os.Stat(output)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o077)
	after, err := resetexec.Inspect(ctx, pool, resetplan.RedisObservation{})
	require.NoError(t, err)
	require.Equal(t, before, after)
	value, err := redisServer.Get("wallet:v2:must-remain")
	require.NoError(t, err)
	require.Equal(t, "synthetic", value)

	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM channels WHERE id=$1`, channelID).Scan(&rows))
	require.Equal(t, 1, rows, "read-only preview changed product data")
}
