//go:build integration

package resetexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/resetplan"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestApplyDeterministicResetPreservesExactAdminPlatformStateAndSequences(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	adminID, adminOrgID, passwordHash := seedResetIntegrationFixture(t, pool)

	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	for _, key := range []string{"wallet:v2:fixture", "rl:fixture", "unrelated:preserve"} {
		require.NoError(t, redisClient.Set(ctx, key, "fixture", 0).Err())
	}

	target := resetplan.TargetIdentity{
		RuntimeVersion: "v2.0.2",
		RuntimeCommit:  strings.Repeat("a", 40),
		SourceTree:     strings.Repeat("b", 40),
	}
	plan, err := Preview(ctx, pool, redisClient, target, "source-old", "source-new", time.Unix(1, 0))
	require.NoError(t, err)
	require.True(t, plan.ScopeComplete)
	require.True(t, plan.ApplySupported)
	require.Equal(t, adminID, plan.SelectedAdmin.UserID)
	require.Equal(t, adminOrgID, plan.SelectedAdmin.OrganizationID)
	require.Positive(t, plan.Summary.ExpectedDeletedRows)

	result, err := Apply(ctx, pool, redisClient, plan, target, "source-new")
	require.NoError(t, err)
	require.True(t, result.EmptyBaseline)
	require.Equal(t, adminID, result.RetainedAdminID)
	require.Equal(t, adminOrgID, result.RetainedOrgID)
	require.Equal(t, int64(2), result.RedisKeysDeleted)

	var retainedPassword, retainedUsername, retainedOrganizationRole, retainedCompatibilityRole string
	var retainedAuthEpoch int64
	var retainedEmail *string
	var orgSettings string
	require.NoError(t, pool.QueryRow(ctx, `SELECT username, password_hash, organization_role, role, auth_epoch, email FROM users WHERE id=$1`, adminID).Scan(&retainedUsername, &retainedPassword, &retainedOrganizationRole, &retainedCompatibilityRole, &retainedAuthEpoch, &retainedEmail))
	require.NoError(t, pool.QueryRow(ctx, `SELECT settings::text FROM orgs WHERE id=$1`, adminOrgID).Scan(&orgSettings))
	require.Equal(t, "Admin", retainedUsername)
	require.Equal(t, passwordHash, retainedPassword)
	require.Equal(t, auth.RolePlatformAdmin, retainedOrganizationRole)
	require.Equal(t, auth.RolePlatformAdmin, retainedCompatibilityRole)
	require.EqualValues(t, 1, retainedAuthEpoch)
	require.Nil(t, retainedEmail)
	require.Equal(t, `{}`, orgSettings)
	for _, table := range plan.Tables {
		var count int64
		require.NoError(t, pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+quotedTable(table.Name)).Scan(&count))
		want := table.ExpectedRetainedRows
		if table.Action == resetplan.ActionPreserveAll {
			want = table.RowCount
		}
		require.Equalf(t, want, count, "post-reset count for %s", table.Name)
	}
	value, err := redisClient.Get(ctx, "unrelated:preserve").Result()
	require.NoError(t, err)
	require.Equal(t, "fixture", value)

	post, err := Preview(ctx, pool, redisClient, target, "source-old", "source-new", time.Unix(2, 0))
	require.NoError(t, err)
	require.Equal(t, plan.Schema, post.Schema, "schema, trigger, FK, and sequence evidence changed")
	require.Equal(t, plan.Protected, post.Protected, "protected platform state changed")
}

func TestApplyStopsBeforeRedisMutationWhenSemanticScopeDrifts(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	seedResetIntegrationFixture(t, pool)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	require.NoError(t, redisClient.Set(ctx, "wallet:v2:must-remain", "fixture", 0).Err())
	target := resetplan.TargetIdentity{
		RuntimeVersion: "v2.0.2",
		RuntimeCommit:  strings.Repeat("c", 40),
		SourceTree:     strings.Repeat("d", 40),
	}
	plan, err := Preview(ctx, pool, redisClient, target, "source-old", "source-new", time.Unix(1, 0))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO channels (name, provider, base_url, status) VALUES ('drift', 'fixture', 'https://example.invalid', 'disabled')`)
	require.NoError(t, err)

	_, err = Apply(ctx, pool, redisClient, plan, target, "source-new")
	require.ErrorIs(t, err, ErrPlanDrift)
	require.Equal(t, "fixture", redisClient.Get(ctx, "wallet:v2:must-remain").Val())
}

func seedResetIntegrationFixture(t *testing.T, pool *pgxpool.Pool) (int64, int64, string) {
	t.Helper()
	ctx := context.Background()
	passwordHashBytes, err := bcrypt.GenerateFromPassword([]byte("local-integration-fixture-password"), bcrypt.MinCost)
	require.NoError(t, err)
	passwordHash := string(passwordHashBytes)
	var adminOrgID, adminID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type, settings)
		VALUES ('Reset Admin Shell', 'active', 'partner', '{"campaign":"old"}'::jsonb) RETURNING id`).Scan(&adminOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id, username, password_hash, organization_role, role, status, auth_provider, email)
		VALUES ($1, 'Admin', $2, 'platform_admin', 'platform_admin', 'active', 'local', 'old-admin@example.invalid') RETURNING id`,
		adminOrgID, passwordHash).Scan(&adminID))
	_, err = pool.Exec(ctx, `
		INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at)
		VALUES ($1, decode(repeat('ab', 32), 'hex'), NOW(), NOW() + INTERVAL '1 hour')`, adminID)
	require.NoError(t, err)

	var oldOrgID, oldUserID, projectID, apiKeyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('Old Campaign Customer', 'active', 'customer') RETURNING id`).Scan(&oldOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id, username, password_hash, organization_role, role, status, auth_provider)
		VALUES ($1, 'old-user', $2, 'org_admin', 'org_admin', 'active', 'local') RETURNING id`,
		oldOrgID, passwordHash).Scan(&oldUserID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id, name, slug, status, created_by)
		VALUES ($1, 'Old Project', 'old-project', 'active', $2) RETURNING id`,
		oldOrgID, oldUserID).Scan(&projectID))
	_, err = pool.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'project_admin')`, projectID, oldUserID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO wallets (org_id, balance_micros) VALUES ($1, 1000000)`, oldOrgID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
			org_id, project_id, owner_user_id, name, key_prefix, key_hash,
			allowed_ips, allowed_models, status, key_type, purpose
		) VALUES (
			$1, $2, $3, 'old-feed', 'ur-fixture-old', $4,
			'192.0.2.0/24', '', 'disabled', 'service', 'xinsight.feed.read'
		) RETURNING id`, oldOrgID, projectID, oldUserID, strings.Repeat("e", 64)).Scan(&apiKeyID))
	_, err = pool.Exec(ctx, `
		INSERT INTO completed_execution_feed_connections (
			org_id, project_id, status, acknowledged_sequence, last_authenticated_key_id
		) VALUES ($1, $2, 'paused', 0, $3)`, oldOrgID, projectID, apiKeyID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO completed_execution_feed_states (org_id, project_id, retained_after_id)
		VALUES ($1, $2, 0)`, oldOrgID, projectID)
	require.NoError(t, err)
	fixtureSnapshot := []byte(`{}`)
	_, err = pool.Exec(ctx, `
		INSERT INTO completed_execution_feed_items (
			event_key, org_id, project_id, request_id, terminal_status,
			terminal_available_at, snapshot, snapshot_sha256
		) VALUES ('fixture-feed-event', $1, $2, 'fixture-request', 'settled', NOW(), $3, $4)`,
		oldOrgID, projectID, fixtureSnapshot, digestFixture(fixtureSnapshot))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail)
		VALUES ($1, $2, 'old.campaign', 'fixture', 'old', '{}'::jsonb)`, oldOrgID, oldUserID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO internal_outbox_events (
			event_key, event_type, priority, org_id, payload
		) VALUES ('fixture-outbox', 'fixture.event', 100, $1, '{}'::jsonb)`, oldOrgID)
	require.NoError(t, err)
	const cycleRequestID = "reset-cycle-request"
	const cycleAttemptID = "reset-cycle-attempt"
	_, err = pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (request_id, org_id, status, pricing_version)
		VALUES ($1, $2, 'settled', 'reset-cycle-test')`, cycleRequestID, oldOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO billing_provider_attempts (
			org_id, request_id, attempt_id, api_key_id, channel_id, channel_key_id,
			route_sequence, route_total, endpoint, provider, channel_name,
			state, outcome_certainty
		) VALUES ($1,$2,$3,$4,201,301,1,1,'/test','fixture','fixture-channel','settled','known_usage')`,
		oldOrgID, cycleRequestID, cycleAttemptID, apiKeyID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE freeze_idempotency SET delivery_attempt_id=$1
		WHERE org_id=$2 AND request_id=$3`, cycleAttemptID, oldOrgID, cycleRequestID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO channels (name, provider, base_url, status)
		VALUES ('Old Provider', 'fixture', 'https://example.invalid', 'disabled')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO settings (key, value, description)
		VALUES ('campaign.fixture', 'true'::jsonb, 'reset integration fixture')
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`)
	require.NoError(t, err)

	return adminID, adminOrgID, passwordHash
}

func quotedTable(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func digestFixture(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}
