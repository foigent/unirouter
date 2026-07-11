//go:build integration

package agentcap

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestAgentCapabilityPostgresAuthorityAndRevocationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)

	var projectID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id, name, slug, status)
		VALUES ($1, 'Capability Project', 'capability-project', 'active')
		RETURNING id`, orgID).Scan(&projectID))
	var userID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id, username, password_hash, role, status)
		VALUES ($1, 'capability-owner', 'not-used', 'developer', 'active')
		RETURNING id`, orgID).Scan(&userID))
	_, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		VALUES ($1, $2, 'project_developer')`, projectID, userID)
	require.NoError(t, err)
	var keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
			org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix,
			status, rpm_limit, qps_limit, budget_limit, allowed_ips, allowed_models
		) VALUES ($1, $2, $3, 'personal', 'capability-root', 'unused-hash', 'ur-it-root',
		          'active', 60, 10, 1000, '', 'gpt-4o')
		RETURNING id`, orgID, projectID, userID).Scan(&keyID))

	redisAddress := os.Getenv("REDIS_ADDR")
	if redisAddress == "" {
		redisAddress = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddress, Password: os.Getenv("REDIS_PASSWORD")})
	t.Cleanup(func() { _ = rdb.Close() })
	require.NoError(t, rdb.Ping(ctx).Err())
	service, err := NewServiceWithOptions(pool, rdb, "agent-capability-integration-secret", ServiceOptions{
		AuthorityCacheTTL: 50 * time.Millisecond,
	})
	require.NoError(t, err)
	rootKey := &models.ApiKey{ID: keyID, OrgID: orgID, ProjectID: &projectID, OwnerUserID: &userID, Status: "active"}
	issued, err := service.Exchange(ctx, rootKey, ExchangeRequest{
		RootAgentID:        "agent:integration-root",
		WorkflowTTLSeconds: 3600,
		Limits: Limits{
			MaxDepth: 1, MaxChildren: 2, MaxDescendants: 2,
			CostBudgetCents: 500, TokenBudget: 10_000,
			RPMLimit: 60, RPSLimit: 10, ConcurrencyLimit: 2,
			AllowedModels: []string{"gpt-4o"}, AllowedEndpoints: []string{"/v1/chat/completions"},
		},
	})
	require.NoError(t, err)
	authenticated, err := service.Authenticate(ctx, issued.CapabilityToken)
	require.NoError(t, err)
	firstReservation, err := service.ReserveInvocation(ctx, authenticated.Claims, "op_integration_first", 100, 100, time.Minute)
	require.NoError(t, err)
	secondReservation, err := service.ReserveInvocation(ctx, authenticated.Claims, "op_integration_second", 100, 100, time.Minute)
	require.NoError(t, err)
	_, err = service.ReserveInvocation(ctx, authenticated.Claims, "op_integration_third", 100, 100, time.Minute)
	require.ErrorIs(t, err, ErrConcurrencyExceeded)
	require.NoError(t, service.FinalizeInvocation(ctx, firstReservation, 80, 75))
	require.NoError(t, service.CancelInvocation(ctx, secondReservation))
	runtimeState, err := rdb.HMGet(ctx, agentKey(issued.WorkflowID, authenticated.Claims.AgentID),
		"cost_reserved_cents", "cost_used_cents", "token_reserved", "token_used").Result()
	require.NoError(t, err)
	require.Equal(t, []interface{}{"0", "75", "0", "80"}, runtimeState)

	_, err = pool.Exec(ctx, `UPDATE users SET status = 'disabled', updated_at = NOW() WHERE id = $1`, userID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, authErr := service.Authenticate(ctx, issued.CapabilityToken)
		return errors.Is(authErr, ErrInactiveRoot)
	}, time.Second, 25*time.Millisecond, "owner status change must invalidate capability within the bounded authority cache SLO")
}
