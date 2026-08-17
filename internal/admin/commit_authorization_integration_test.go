//go:build integration

package admin

import (
	"context"
	"crypto/rand"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
)

// withIntegrationCommittingActor persists a real, live admin session and
// attaches the exact dual-axis principal snapshot that production AdminAuth
// would admit. Mutation integration tests use it instead of direct-handler
// contexts so commit-time actor/session checks exercise the production path.
func withIntegrationCommittingActor(t *testing.T, pool *pgxpool.Pool, request *http.Request, userID int64) *http.Request {
	t.Helper()
	ctx := context.Background()
	var orgID, authEpoch int64
	var organizationRole, compatibilityRole string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id, organization_role, role, auth_epoch
		  FROM users
		 WHERE id=$1`, userID,
	).Scan(&orgID, &organizationRole, &compatibilityRole, &authEpoch))

	sessionHash := make([]byte, 32)
	_, err := rand.Read(sessionHash)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at)
		VALUES ($1, $2, $3, $4)`,
		userID, sessionHash, time.Now().UTC().Add(-time.Minute), time.Now().UTC().Add(time.Hour),
	)
	require.NoError(t, err)

	requestCtx := context.WithValue(request.Context(), middleware.CtxUserID, userID)
	requestCtx = context.WithValue(requestCtx, middleware.CtxOrgID, orgID)
	requestCtx = context.WithValue(requestCtx, middleware.CtxUserRole, compatibilityRole)
	requestCtx = context.WithValue(requestCtx, middleware.CtxOrganizationRole, organizationRole)
	requestCtx = context.WithValue(requestCtx, middleware.CtxAuthEpoch, authEpoch)
	requestCtx = context.WithValue(requestCtx, middleware.CtxAdminSessionHash, append([]byte(nil), sessionHash...))
	return request.WithContext(requestCtx)
}
