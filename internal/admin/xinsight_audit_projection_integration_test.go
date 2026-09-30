//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestXInsightAuditHumanSystemServiceProjectionIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)
	_, err := pool.Exec(ctx, `INSERT INTO audit_logs(org_id,user_id,source,action,resource) VALUES
 ($1,$2,'system','old.human.system.source','policy'),
 ($1,0,'system','old.system','policy')`, fx.orgA, fx.orgAdminA)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO audit_logs(org_id,user_id,source,actor_kind,actor_ref,action,resource)
 VALUES($1,NULL,'xinsight_integration','service','api_key:123','policy.machine','policy')`, fx.orgA)
	require.NoError(t, err)
	for _, bad := range []struct {
		name, sql string
		args      []any
	}{
		{"human null", `INSERT INTO audit_logs(org_id,user_id,actor_kind,actor_ref,action,resource) VALUES($1,NULL,'human','user:1','invalid','policy')`, []any{fx.orgA}},
		{"human empty ref", `INSERT INTO audit_logs(org_id,user_id,actor_kind,actor_ref,action,resource) VALUES($1,$2,'human','','invalid','policy')`, []any{fx.orgA, fx.orgAdminA}},
		{"human wrong ref", `INSERT INTO audit_logs(org_id,user_id,actor_kind,actor_ref,action,resource) VALUES($1,$2,'human','user:999','invalid','policy')`, []any{fx.orgA, fx.orgAdminA}},
	} {
		t.Run(bad.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, bad.sql, bad.args...)
			require.Error(t, err)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23514", pgErr.Code)
			require.Equal(t, "audit_actor_identity", pgErr.ConstraintName)
		})
	}
	var humanKind, humanRef string
	require.NoError(t, pool.QueryRow(ctx, `SELECT actor_kind,actor_ref FROM audit_logs WHERE action='old.human.system.source'`).Scan(&humanKind, &humanRef))
	require.Equal(t, "human", humanKind)
	require.Equal(t, "user:"+strconv.FormatInt(fx.orgAdminA, 10), humanRef)

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	serve := func(path string, handler http.HandlerFunc) map[string]any {
		t.Helper()
		jwt := testutil.MustAdminSessionJWT(t, ctx, pool, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, p6f16JWTSecret)
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+jwt)
		response := httptest.NewRecorder()
		middleware.AdminAuth(pool, p6f16JWTSecret)(middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin)(handler)).ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		return body
	}
	v1 := serve("/api/v1/audit-logs?action=policy.machine", h.ListAuditLogs)
	require.Equal(t, float64(1), v1["total"])
	require.Equal(t, "all_records_legacy_actor_projection", v1["coverage"])
	v1Row := v1["data"].([]any)[0].(map[string]any)
	require.Equal(t, float64(0), v1Row["actor_id"])
	require.Equal(t, float64(0), v1Row["user_id"])
	require.Equal(t, "service", v1Row["actor_kind"])
	v2 := serve("/api/v2/audit-logs?actor_kind=service", h.ListAuditLogsV2)
	require.Equal(t, float64(1), v2["total"])
	v2Row := v2["data"].([]any)[0].(map[string]any)
	require.Nil(t, v2Row["user_id"])
	require.Equal(t, "api_key:123", v2Row["actor"].(map[string]any)["ref"])
	require.Equal(t, float64(0), serve("/api/v2/audit-logs?actor_ref=api_key:999", h.ListAuditLogsV2)["total"])
}

func TestXInsightAuditM123HistoricalActorBackfillIntegration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.ScopedContentExpandHead)
	ctx := context.Background()
	folder := testutil.MigrationsDir(t)
	var head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.ScopedContentExpandHead, head)
	fx := seedP6F16Fixture(t, pool)
	var err error
	_, err = pool.Exec(ctx, `INSERT INTO audit_logs(org_id,user_id,source,action,resource) VALUES
 ($1,$2,'system','before.m123.human','policy'),($1,0,'system','before.m123.system','policy')`, fx.orgA, fx.orgAdminA)
	require.NoError(t, err)
	up, err := os.ReadFile(filepath.Join(folder, "000123_xinsight_policy_delegation.up.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(up))
	require.NoError(t, err)
	for _, tc := range []struct{ action, kind, ref string }{
		{"before.m123.human", "human", "user:" + strconv.FormatInt(fx.orgAdminA, 10)},
		{"before.m123.system", "system", "system"},
	} {
		var kind, ref string
		require.NoError(t, pool.QueryRow(ctx, `SELECT actor_kind,actor_ref FROM audit_logs WHERE action=$1`, tc.action).Scan(&kind, &ref))
		require.Equal(t, tc.kind, kind)
		require.Equal(t, tc.ref, ref)
	}
}
