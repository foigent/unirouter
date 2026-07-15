package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/testutil"
)

const p6f16JWTSecret = "p6f16-test-jwt-secret-32-bytes!!"

type p6f16Fixture struct {
	orgA, orgB                                           int64
	orgAdminA, platformA, legacyAdminA, billingAdminA    int64
	projectAdminA, developerA, normalA, orgViewerA       int64
	unknownRoleA, orgAdminB, normalB                     int64
	projectA, projectB, keyA, keyB, modelSetA, modelSetB int64
}

func (fx p6f16Fixture) actorForRole(role string) int64 {
	switch role {
	case auth.RolePlatformAdmin:
		return fx.platformA
	case auth.RoleLegacyAdmin:
		return fx.legacyAdminA
	case auth.RoleOrgAdmin:
		return fx.orgAdminA
	case auth.RoleBillingAdmin:
		return fx.billingAdminA
	case auth.RoleProjectAdmin:
		return fx.projectAdminA
	case auth.RoleDeveloper:
		return fx.developerA
	case auth.RoleViewer:
		return fx.normalA
	case auth.RoleOrgViewer:
		return fx.orgViewerA
	default:
		return fx.unknownRoleA
	}
}

func TestP6F16TenantIsolationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)
	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	mux := p6f16AdminMux(h)

	serve := func(tb testing.TB, method, target, body string, userID, orgID int64, role string) *httptest.ResponseRecorder {
		tb.Helper()
		token, err := auth.GenerateJWT(userID, orgID, role, p6f16JWTSecret, 1)
		require.NoError(tb, err)
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	assertStatus := func(tb testing.TB, rec *httptest.ResponseRecorder, want int) {
		tb.Helper()
		if rec.Code != want {
			tb.Fatalf("expected status %d, got %d: %s", want, rec.Code, rec.Body.String())
		}
	}

	t.Run("org admin lists only its keys", func(t *testing.T) {
		rec := serve(t, http.MethodGet, "/api/v1/keys", "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusOK)
		assertResponseOrgIDs(t, rec.Body.Bytes(), fx.orgA)
	})

	t.Run("org admin key reads and mutations stay in its org", func(t *testing.T) {
		var auditBefore, eventsBefore int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditBefore))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_events WHERE org_id = $1`, fx.orgB).Scan(&eventsBefore))
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/keys/%d", fx.keyB), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)

		var beforeCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE org_id = $1`, fx.orgB).Scan(&beforeCount))
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/keys", fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"foreign-new"}`, fx.orgB, fx.projectB), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		var afterCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE org_id = $1`, fx.orgB).Scan(&afterCount))
		require.Equal(t, beforeCount, afterCount)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", fx.keyB), `{"name":"foreign-changed"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", fx.keyB), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		var name, status string
		require.NoError(t, pool.QueryRow(ctx, `SELECT name, status FROM api_keys WHERE id = $1`, fx.keyB).Scan(&name, &status))
		require.Equal(t, "key-b", name)
		require.Equal(t, "active", status)
		var auditAfter, eventsAfter int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditAfter))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_events WHERE org_id = $1`, fx.orgB).Scan(&eventsAfter))
		require.Equal(t, auditBefore, auditAfter)
		require.Equal(t, eventsBefore, eventsAfter)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", fx.keyA), `{"name":"key-a-updated"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		require.NoError(t, pool.QueryRow(ctx, `SELECT name FROM api_keys WHERE id = $1`, fx.keyA).Scan(&name))
		require.Equal(t, "key-a-updated", name)
	})

	t.Run("same-tenant key create read and disable succeeds", func(t *testing.T) {
		rec := serve(t, http.MethodPost, "/api/v1/keys", fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"same-tenant-key"}`, fx.orgA, fx.projectA), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusCreated)
		keyID := responseID(t, rec.Body.Bytes())
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/keys/%d", keyID), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", keyID), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		var orgID int64
		var status string
		require.NoError(t, pool.QueryRow(ctx, `SELECT org_id, status FROM api_keys WHERE id = $1`, keyID).Scan(&orgID, &status))
		require.Equal(t, fx.orgA, orgID)
		require.Equal(t, "disabled", status)
	})

	t.Run("org admin user operations stay in its org and cannot create platform authority", func(t *testing.T) {
		rec := serve(t, http.MethodGet, "/api/v1/users", "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusOK)
		assertResponseOrgIDs(t, rec.Body.Bytes(), fx.orgA)

		var beforeCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE org_id = $1`, fx.orgB).Scan(&beforeCount))
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/users", fmt.Sprintf(`{"org_id":%d,"username":"foreign-new","password":"secret123","role":"viewer"}`, fx.orgB), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		var afterCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE org_id = $1`, fx.orgB).Scan(&afterCount))
		require.Equal(t, beforeCount, afterCount)

		for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin} {
			target := strings.ReplaceAll(role, "_", "-")
			assertStatus(t, serve(t, http.MethodPost, "/api/v1/users", fmt.Sprintf(`{"org_id":%d,"username":"blocked-%s","password":"secret123","role":"%s"}`, fx.orgA, target, role), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
			assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", fx.normalA), fmt.Sprintf(`{"role":"%s"}`, role), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		}
		var blockedRoleCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE username IN ('blocked-platform-admin', 'blocked-admin')`).Scan(&blockedRoleCount))
		require.Zero(t, blockedRoleCount)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", fx.platformA), `{"status":"disabled"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", fx.platformA), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", fx.normalB), `{"status":"disabled"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", fx.normalB), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)

		var foreignUserCount int
		var foreignKeyStatus, foreignUserStatus, platformStatus string
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE id = $1`, fx.normalB).Scan(&foreignUserCount))
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, fx.keyB).Scan(&foreignKeyStatus))
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, fx.normalB).Scan(&foreignUserStatus))
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, fx.platformA).Scan(&platformStatus))
		require.Equal(t, 1, foreignUserCount)
		require.Equal(t, "active", foreignKeyStatus)
		require.Equal(t, "active", foreignUserStatus)
		require.Equal(t, "active", platformStatus)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", fx.normalA), `{"role":"developer"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		var role string
		require.NoError(t, pool.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, fx.normalA).Scan(&role))
		require.Equal(t, auth.RoleDeveloper, role)
	})

	t.Run("same-tenant user create update and delete succeeds", func(t *testing.T) {
		rec := serve(t, http.MethodPost, "/api/v1/users", fmt.Sprintf(`{"org_id":%d,"username":"same-tenant-user","password":"secret123","role":"viewer"}`, fx.orgA), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusCreated)
		userID := responseID(t, rec.Body.Bytes())
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", userID), `{"status":"disabled"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", userID), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE id = $1`, userID).Scan(&count))
		require.Zero(t, count)
	})

	t.Run("org admin project and model-set operations stay in its org", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects?org_id=%d", fx.orgB), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		rec := serve(t, http.MethodGet, "/api/v1/projects", "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusOK)
		assertResponseOrgIDs(t, rec.Body.Bytes(), fx.orgA)

		var beforeCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM projects WHERE org_id = $1`, fx.orgB).Scan(&beforeCount))
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/projects", fmt.Sprintf(`{"org_id":%d,"name":"Foreign New"}`, fx.orgB), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		var afterCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM projects WHERE org_id = $1`, fx.orgB).Scan(&afterCount))
		require.Equal(t, beforeCount, afterCount)

		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d", fx.projectB), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d", fx.projectB), `{"description":"foreign-changed"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d/model-sets", fx.projectB), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d/model-sets", fx.projectB), `{"model_set_ids":[]}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)

		var description string
		var assignmentCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT description FROM projects WHERE id = $1`, fx.projectB).Scan(&description))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_model_sets WHERE project_id = $1 AND model_set_id = $2`, fx.projectB, fx.modelSetB).Scan(&assignmentCount))
		require.Equal(t, "project-b", description)
		require.Equal(t, 1, assignmentCount)

		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d", fx.projectA), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d", fx.projectA), `{"description":"project-a-updated"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		require.NoError(t, pool.QueryRow(ctx, `SELECT description FROM projects WHERE id = $1`, fx.projectA).Scan(&description))
		require.Equal(t, "project-a-updated", description)
	})

	t.Run("same-tenant project create and model-set replacement succeeds", func(t *testing.T) {
		rec := serve(t, http.MethodPost, "/api/v1/projects", fmt.Sprintf(`{"org_id":%d,"name":"Same Tenant Project"}`, fx.orgA), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusCreated)
		projectID := responseID(t, rec.Body.Bytes())
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d", projectID), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d/model-sets", fx.projectA), fmt.Sprintf(`{"model_set_ids":[%d]}`, fx.modelSetA), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_model_sets WHERE project_id = $1 AND model_set_id = $2`, fx.projectA, fx.modelSetA).Scan(&count))
		require.Equal(t, 1, count)
	})

	t.Run("project model-set replacement rejects foreign sets without partial mutation", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d/model-sets", fx.projectA), fmt.Sprintf(`{"model_set_ids":[%d]}`, fx.modelSetB), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusBadRequest)

		var ownCount, foreignCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_model_sets WHERE project_id = $1 AND model_set_id = $2`, fx.projectA, fx.modelSetA).Scan(&ownCount))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_model_sets WHERE project_id = $1 AND model_set_id = $2`, fx.projectA, fx.modelSetB).Scan(&foreignCount))
		require.Equal(t, 1, ownCount)
		require.Zero(t, foreignCount)
	})

	t.Run("malformed cross-org membership cannot authorize a stale org claim", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'project_developer') ON CONFLICT DO NOTHING`,
			fx.projectB, fx.developerA,
		)
		require.NoError(t, err)

		rec := serve(t, http.MethodGet, "/api/v1/projects", "", fx.developerA, fx.orgB, auth.RoleDeveloper)
		assertStatus(t, rec, http.StatusUnauthorized)
		rec = serve(t, http.MethodGet, "/api/v1/keys", "", fx.developerA, fx.orgB, auth.RoleDeveloper)
		assertStatus(t, rec, http.StatusUnauthorized)
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d", fx.projectB), "", fx.developerA, fx.orgB, auth.RoleDeveloper), http.StatusUnauthorized)
	})

	t.Run("runtime ignores pre-existing cross-org model-set assignments", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`DELETE FROM project_model_sets WHERE project_id = $1`,
			fx.projectA,
		)
		require.NoError(t, err)
		_, err = pool.Exec(ctx,
			`INSERT INTO project_model_sets (project_id, model_set_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			fx.projectA, fx.modelSetB,
		)
		require.NoError(t, err)

		result, err := modelgov.CheckAccess(ctx, pool, fx.orgA, fx.projectA, "unconfigured-local-model")
		require.NoError(t, err)
		require.True(t, result.Allowed)
		rec := serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d/model-sets", fx.projectA), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusOK)
		assertEmptyData(t, rec.Body.Bytes())
	})

	t.Run("platform admin retains explicit cross-tenant exception", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d", fx.projectB), "", fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d", fx.projectB), `{"description":"platform-updated"}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d/model-sets", fx.projectB), "", fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d/model-sets", fx.projectB), fmt.Sprintf(`{"model_set_ids":[%d]}`, fx.modelSetB), fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/keys/%d", fx.keyB), "", fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", fx.keyB), `{"name":"platform-key-update"}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", fx.keyB), "", fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)

		rec := serve(t, http.MethodPost, "/api/v1/users", fmt.Sprintf(`{"org_id":%d,"username":"platform-foreign-user","password":"secret123","role":"viewer"}`, fx.orgB), fx.platformA, fx.orgA, auth.RolePlatformAdmin)
		assertStatus(t, rec, http.StatusCreated)
		userID := responseID(t, rec.Body.Bytes())
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", userID), `{"status":"disabled"}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", userID), "", fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)

		rec = serve(t, http.MethodPost, "/api/v1/projects", fmt.Sprintf(`{"org_id":%d,"name":"Platform Foreign Project"}`, fx.orgB), fx.platformA, fx.orgA, auth.RolePlatformAdmin)
		assertStatus(t, rec, http.StatusCreated)
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d", fx.projectB), "", fx.legacyAdminA, fx.orgA, auth.RoleLegacyAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d", fx.projectB), `{"description":"legacy-admin-updated"}`, fx.legacyAdminA, fx.orgA, auth.RoleLegacyAdmin), http.StatusOK)
		var description string
		require.NoError(t, pool.QueryRow(ctx, `SELECT description FROM projects WHERE id = $1`, fx.projectB).Scan(&description))
		require.Equal(t, "legacy-admin-updated", description)
	})

	t.Run("non-platform actor with zero organization fails closed", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodGet, "/api/v1/keys", "", fx.orgAdminA, 0, auth.RoleOrgAdmin), http.StatusUnauthorized)
		assertStatus(t, serve(t, http.MethodGet, "/api/v1/users", "", fx.orgAdminA, 0, auth.RoleOrgAdmin), http.StatusUnauthorized)
		assertStatus(t, serve(t, http.MethodGet, "/api/v1/projects", "", fx.orgAdminA, 0, auth.RoleOrgAdmin), http.StatusUnauthorized)
	})
}

func p6f16AdminMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	roleRoute := func(handler http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(h.db, p6f16JWTSecret)(middleware.RequireRoles(roles...)(handler))
	}
	projectRead := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer}
	projectWrite := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper}
	orgAdmin := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}

	mux.Handle("GET /api/v1/keys", roleRoute(h.ListAPIKeys, projectRead...))
	mux.Handle("POST /api/v1/keys", roleRoute(h.CreateAPIKey, projectWrite...))
	mux.Handle("GET /api/v1/keys/{id}", roleRoute(h.GetAPIKey, projectRead...))
	mux.Handle("PUT /api/v1/keys/{id}", roleRoute(h.UpdateAPIKey, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))
	mux.Handle("POST /api/v1/keys/{id}/disable", roleRoute(h.DisableAPIKey, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))

	mux.Handle("GET /api/v1/users", roleRoute(h.ListUsers, orgAdmin...))
	mux.Handle("POST /api/v1/users", roleRoute(h.CreateUser, orgAdmin...))
	mux.Handle("PUT /api/v1/users/{id}", roleRoute(h.UpdateUser, orgAdmin...))
	mux.Handle("DELETE /api/v1/users/{id}", roleRoute(h.DeleteUser, orgAdmin...))

	mux.Handle("GET /api/v1/projects", roleRoute(h.ListProjects, projectRead...))
	mux.Handle("POST /api/v1/projects", roleRoute(h.CreateProject, orgAdmin...))
	mux.Handle("GET /api/v1/projects/{id}", roleRoute(h.GetProject, projectRead...))
	mux.Handle("PUT /api/v1/projects/{id}", roleRoute(h.UpdateProject, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))
	mux.Handle("GET /api/v1/projects/{id}/model-sets", roleRoute(h.ListProjectModelSets, projectRead...))
	mux.Handle("PUT /api/v1/projects/{id}/model-sets", roleRoute(h.ReplaceProjectModelSets, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin))
	return mux
}

func seedP6F16Fixture(t *testing.T, pool *pgxpool.Pool) p6f16Fixture {
	t.Helper()
	ctx := context.Background()
	var fx p6f16Fixture
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('p6f16-org-a', 'active', 'standard') RETURNING id`,
	).Scan(&fx.orgA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('p6f16-org-b', 'active', 'standard') RETURNING id`,
	).Scan(&fx.orgB))

	insertUser := func(orgID int64, username, role string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO users (org_id, username, password_hash, role, status) VALUES ($1, $2, 'test-hash', $3, 'active') RETURNING id`,
			orgID, username, role,
		).Scan(&id))
		return id
	}
	fx.orgAdminA = insertUser(fx.orgA, "org-admin-a", auth.RoleOrgAdmin)
	fx.platformA = insertUser(fx.orgA, "platform-a", auth.RolePlatformAdmin)
	fx.legacyAdminA = insertUser(fx.orgA, "legacy-admin-a", auth.RoleLegacyAdmin)
	fx.billingAdminA = insertUser(fx.orgA, "billing-admin-a", auth.RoleBillingAdmin)
	fx.projectAdminA = insertUser(fx.orgA, "project-admin-a", auth.RoleProjectAdmin)
	fx.developerA = insertUser(fx.orgA, "developer-a", auth.RoleDeveloper)
	fx.normalA = insertUser(fx.orgA, "normal-a", auth.RoleViewer)
	fx.orgViewerA = insertUser(fx.orgA, "org-viewer-a", auth.RoleOrgViewer)
	fx.unknownRoleA = insertUser(fx.orgA, "unknown-role-a", "unknown_role")
	fx.orgAdminB = insertUser(fx.orgB, "org-admin-b", auth.RoleOrgAdmin)
	fx.normalB = insertUser(fx.orgB, "normal-b", auth.RoleViewer)

	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, created_by) VALUES ($1, 'Project A', 'project-a', 'project-a', 'active', $2) RETURNING id`,
		fx.orgA, fx.orgAdminA,
	).Scan(&fx.projectA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, created_by) VALUES ($1, 'Project B', 'project-b', 'project-b', 'active', $2) RETURNING id`,
		fx.orgB, fx.orgAdminB,
	).Scan(&fx.projectB))

	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO api_keys (org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix, status, rpm_limit, qps_limit)
		 VALUES ($1, $2, $3, 'personal', 'key-a', $4, 'p6f16-a', 'active', 60, 10) RETURNING id`,
		fx.orgA, fx.projectA, fx.normalA, strings.Repeat("a", 64),
	).Scan(&fx.keyA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO api_keys (org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix, status, rpm_limit, qps_limit)
		 VALUES ($1, $2, $3, 'personal', 'key-b', $4, 'p6f16-b', 'active', 60, 10) RETURNING id`,
		fx.orgB, fx.projectB, fx.normalB, strings.Repeat("b", 64),
	).Scan(&fx.keyB))

	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_sets (org_id, name, slug, description, status) VALUES ($1, 'Set A', 'set-a', 'set-a', 'active') RETURNING id`,
		fx.orgA,
	).Scan(&fx.modelSetA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_sets (org_id, name, slug, description, status) VALUES ($1, 'Set B', 'set-b', 'set-b', 'active') RETURNING id`,
		fx.orgB,
	).Scan(&fx.modelSetB))
	_, err := pool.Exec(ctx,
		`INSERT INTO model_set_items (model_set_id, item_type, item_name) VALUES ($1, 'model', 'foreign-model')`,
		fx.modelSetB,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO project_model_sets (project_id, model_set_id) VALUES ($1, $2)`,
		fx.projectA, fx.modelSetA,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO project_model_sets (project_id, model_set_id) VALUES ($1, $2)`,
		fx.projectB, fx.modelSetB,
	)
	require.NoError(t, err)
	return fx
}

func assertResponseOrgIDs(t *testing.T, body []byte, wantOrgID int64) {
	t.Helper()
	var response struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.NotEmpty(t, response.Data)
	for _, item := range response.Data {
		require.Equal(t, float64(wantOrgID), item["org_id"], "cross-tenant object leaked: %#v", item)
	}
}

func assertEmptyData(t *testing.T, body []byte) {
	t.Helper()
	var response struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.Empty(t, response.Data, "unexpected cross-tenant data: %#v", response.Data)
}

func responseID(t *testing.T, body []byte) int64 {
	t.Helper()
	var response struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.Positive(t, response.ID)
	return response.ID
}
