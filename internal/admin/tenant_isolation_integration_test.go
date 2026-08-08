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
		token := testutil.MustAdminSessionJWT(tb, ctx, pool, userID, orgID, role, p6f16JWTSecret)
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

	t.Run("organization viewer has organization metadata read but no implicit write or credential authority", func(t *testing.T) {
		projects := serve(t, http.MethodGet, "/api/v1/projects", "", fx.orgViewerA, fx.orgA, auth.RoleOrgViewer)
		assertStatus(t, projects, http.StatusOK)
		assertResponseOrgIDs(t, projects.Body.Bytes(), fx.orgA)
		var projectList struct {
			Data []map[string]interface{} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(projects.Body.Bytes(), &projectList))
		require.NotEmpty(t, projectList.Data)
		for _, project := range projectList.Data {
			require.NotContains(t, project, "budget_cents")
			require.NotContains(t, project, "key_count")
		}

		detail := serve(t, http.MethodGet, fmt.Sprintf("/api/v1/projects/%d", fx.projectA), "", fx.orgViewerA, fx.orgA, auth.RoleOrgViewer)
		assertStatus(t, detail, http.StatusOK)
		var projectDetail map[string]interface{}
		require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &projectDetail))
		require.NotContains(t, projectDetail, "budget_cents")
		require.NotContains(t, projectDetail, "key_count")
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/projects/%d", fx.projectA), `{"name":"must-not-change"}`, fx.orgViewerA, fx.orgA, auth.RoleOrgViewer), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/keys", fmt.Sprintf(`{"project_id":%d,"name":"must-not-create"}`, fx.projectA), fx.orgViewerA, fx.orgA, auth.RoleOrgViewer), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/keys?project_id=%d", fx.projectA), "", fx.orgViewerA, fx.orgA, auth.RoleOrgViewer), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/keys/%d", fx.keyA), "", fx.orgViewerA, fx.orgA, auth.RoleOrgViewer), http.StatusNotFound)

		_, err := pool.Exec(ctx,
			`INSERT INTO project_members (project_id, user_id, role)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
			fx.projectA, fx.orgViewerA, projectMemberViewer,
		)
		require.NoError(t, err)

		for _, target := range []string{
			"/api/v1/keys",
			fmt.Sprintf("/api/v1/keys?project_id=%d", fx.projectA),
			fmt.Sprintf("/api/v1/keys/%d", fx.keyA),
			fmt.Sprintf("/api/v1/projects/%d/model-sets", fx.projectA),
		} {
			assertStatus(t, serve(t, http.MethodGet, target, "", fx.orgViewerA, fx.orgA, auth.RoleOrgViewer), http.StatusOK)
		}
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", fx.keyA), `{"name":"must-not-update"}`, fx.orgViewerA, fx.orgA, auth.RoleOrgViewer), http.StatusNotFound)
	})

	t.Run("organization choices respect platform and tenant scope", func(t *testing.T) {
		decode := func(tb testing.TB, rec *httptest.ResponseRecorder) []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} {
			tb.Helper()
			var response struct {
				Data []struct {
					ID   int64  `json:"id"`
					Name string `json:"name"`
				} `json:"data"`
			}
			require.NoError(tb, json.Unmarshal(rec.Body.Bytes(), &response))
			return response.Data
		}

		platform := serve(t, http.MethodGet, "/api/v1/organizations", "", fx.platformA, fx.orgA, auth.RolePlatformAdmin)
		assertStatus(t, platform, http.StatusOK)
		platformOrganizations := decode(t, platform)
		var sawOrgA, sawOrgB bool
		for i, organization := range platformOrganizations {
			if i > 0 {
				require.LessOrEqual(t, platformOrganizations[i-1].Name, organization.Name)
			}
			sawOrgA = sawOrgA || organization.ID == fx.orgA
			sawOrgB = sawOrgB || organization.ID == fx.orgB
		}
		require.True(t, sawOrgA && sawOrgB)

		organization := serve(t, http.MethodGet, "/api/v1/organizations", "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, organization, http.StatusOK)
		organizationChoices := decode(t, organization)
		require.Len(t, organizationChoices, 1)
		require.Equal(t, fx.orgA, organizationChoices[0].ID)

		assertStatus(t, serve(t, http.MethodGet, "/api/v1/organizations", "", fx.unknownRoleA, fx.orgA, "unknown_role"), http.StatusUnauthorized)
	})

	t.Run("tenant create omission binds resources to actor organization", func(t *testing.T) {
		key := serve(t, http.MethodPost, "/api/v1/keys", fmt.Sprintf(`{"project_id":%d,"name":"implicit-own-key"}`, fx.projectA), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, key, http.StatusCreated)
		keyID := responseID(t, key.Body.Bytes())
		var keyOrgID int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT org_id FROM api_keys WHERE id = $1`, keyID).Scan(&keyOrgID))
		require.Equal(t, fx.orgA, keyOrgID)

		user := serve(t, http.MethodPost, "/api/v1/users", `{"username":"implicit-own-user","password":"secret-password-123"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, user, http.StatusCreated)
		userID := responseID(t, user.Body.Bytes())
		var userOrgID int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT org_id FROM users WHERE id = $1`, userID).Scan(&userOrgID))
		require.Equal(t, fx.orgA, userOrgID)
	})

	t.Run("api key pagination returns database total", func(t *testing.T) {
		rec := serve(t, http.MethodGet, "/api/v1/keys?page=2&page_size=1", "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusOK)
		var response struct {
			Data  []map[string]interface{} `json:"data"`
			Total int                      `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Len(t, response.Data, 1)
		var expectedTotal int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE org_id = $1`, fx.orgA).Scan(&expectedTotal))
		require.GreaterOrEqual(t, expectedTotal, 2)
		require.Equal(t, expectedTotal, response.Total)
	})

	t.Run("platform create requires explicit target organization", func(t *testing.T) {
		var keysBefore, usersBefore, auditBefore int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys`).Scan(&keysBefore))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&usersBefore))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditBefore))

		assertStatus(t, serve(t, http.MethodPost, "/api/v1/keys", `{"name":"platform-missing-org"}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusBadRequest)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/users", `{"username":"platform-missing-org","password":"secret-password-123","role":"viewer"}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusBadRequest)

		var keysAfter, usersAfter, auditAfter int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys`).Scan(&keysAfter))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&usersAfter))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditAfter))
		require.Equal(t, keysBefore, keysAfter)
		require.Equal(t, usersBefore, usersAfter)
		require.Equal(t, auditBefore, auditAfter)
	})

	t.Run("org admin key reads and mutations stay in its org", func(t *testing.T) {
		var auditBefore, eventsBefore int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditBefore))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_events WHERE org_id = $1`, fx.orgB).Scan(&eventsBefore))
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/keys/%d", fx.keyB), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusNotFound)

		var beforeCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE org_id = $1`, fx.orgB).Scan(&beforeCount))
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/keys", fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"foreign-new"}`, fx.orgB, fx.projectB), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		var afterCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE org_id = $1`, fx.orgB).Scan(&afterCount))
		require.Equal(t, beforeCount, afterCount)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", fx.keyB), `{"name":"foreign-changed"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusNotFound)
		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", fx.keyB), "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusNotFound)
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

	t.Run("developer disables only an owned key in an assigned project", func(t *testing.T) {
		var unassignedProjectID, unassignedKeyID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO projects (org_id, name, slug, status, created_by)
			 VALUES ($1, 'Developer Unassigned', 'developer-unassigned', 'active', $2) RETURNING id`,
			fx.orgA, fx.orgAdminA,
		).Scan(&unassignedProjectID))
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO api_keys (org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix, status, rpm_limit, qps_limit)
			 VALUES ($1, $2, $3, 'personal', 'developer-unassigned-key', $4, 'p6f16-dev-unassign', 'active', 60, 10) RETURNING id`,
			fx.orgA, unassignedProjectID, fx.developerA, strings.Repeat("c", 64),
		).Scan(&unassignedKeyID))
		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", unassignedKeyID), "", fx.developerA, fx.orgA, auth.RoleDeveloper), http.StatusNotFound)

		_, err := pool.Exec(ctx,
			`INSERT INTO project_members (project_id, user_id, role)
			 VALUES ($1, $2, 'project_user'), ($1, $3, 'project_viewer')
			 ON CONFLICT DO NOTHING`,
			fx.projectA, fx.developerA, fx.normalA,
		)
		require.NoError(t, err)

		created := serve(t, http.MethodPost, "/api/v1/keys", fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"developer-owned-key"}`, fx.orgA, fx.projectA), fx.developerA, fx.orgA, auth.RoleDeveloper)
		assertStatus(t, created, http.StatusCreated)
		ownedKeyID := responseID(t, created.Body.Bytes())
		var createdResponse struct {
			OwnerUserID int64 `json:"owner_user_id"`
		}
		require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createdResponse))
		require.Equal(t, fx.developerA, createdResponse.OwnerUserID)

		var otherKeyID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO api_keys (org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix, status, rpm_limit, qps_limit)
			 VALUES ($1, $2, $3, 'personal', 'developer-other-key', $4, 'p6f16-dev-other', 'active', 60, 10) RETURNING id`,
			fx.orgA, fx.projectA, fx.normalA, strings.Repeat("d", 64),
		).Scan(&otherKeyID))

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", ownedKeyID), `{"name":"developer-owned-key-updated"}`, fx.developerA, fx.orgA, auth.RoleDeveloper), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", otherKeyID), "", fx.developerA, fx.orgA, auth.RoleDeveloper), http.StatusNotFound)
		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/disable", ownedKeyID), "", fx.developerA, fx.orgA, auth.RoleDeveloper), http.StatusOK)

		var ownedStatus, otherStatus, unassignedStatus string
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, ownedKeyID).Scan(&ownedStatus))
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, otherKeyID).Scan(&otherStatus))
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, unassignedKeyID).Scan(&unassignedStatus))
		require.Equal(t, "disabled", ownedStatus)
		require.Equal(t, "active", otherStatus)
		require.Equal(t, "active", unassignedStatus)
	})

	t.Run("org admin user operations stay in its org and cannot create platform authority", func(t *testing.T) {
		rec := serve(t, http.MethodGet, "/api/v1/users", "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusOK)
		assertResponseOrgIDs(t, rec.Body.Bytes(), fx.orgA)

		var beforeCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE org_id = $1`, fx.orgB).Scan(&beforeCount))
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/users", fmt.Sprintf(`{"org_id":%d,"username":"foreign-new","password":"secret-password-123","organization_role":"org_viewer"}`, fx.orgB), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		var afterCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE org_id = $1`, fx.orgB).Scan(&afterCount))
		require.Equal(t, beforeCount, afterCount)

		for _, role := range []string{auth.RolePlatformAdmin, auth.RoleLegacyAdmin} {
			target := strings.ReplaceAll(role, "_", "-")
			assertStatus(t, serve(t, http.MethodPost, "/api/v1/users", fmt.Sprintf(`{"org_id":%d,"username":"blocked-%s","password":"secret-password-123","role":"%s"}`, fx.orgA, target, role), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
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

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", fx.normalA), `{"organization_role":"org_viewer"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		var organizationRole, role string
		require.NoError(t, pool.QueryRow(ctx, `SELECT organization_role,role FROM users WHERE id = $1`, fx.normalA).Scan(&organizationRole, &role))
		require.Equal(t, auth.RoleOrgViewer, organizationRole)
		require.Equal(t, auth.RoleOrgViewer, role)
	})

	t.Run("same-tenant user create update and delete succeeds", func(t *testing.T) {
		rec := serve(t, http.MethodPost, "/api/v1/users", fmt.Sprintf(`{"org_id":%d,"username":"same-tenant-user","password":"secret-password-123"}`, fx.orgA), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
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
			`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'project_user') ON CONFLICT DO NOTHING`,
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

		rec := serve(t, http.MethodPost, "/api/v1/users", fmt.Sprintf(`{"org_id":%d,"username":"platform-foreign-user","password":"secret-password-123","organization_role":"org_viewer"}`, fx.orgB), fx.platformA, fx.orgA, auth.RolePlatformAdmin)
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
	adminRoute := func(handler http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(h.db, p6f16JWTSecret)(handler)
	}
	roleRoute := func(handler http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(h.db, p6f16JWTSecret)(middleware.RequireRoles(roles...)(handler))
	}
	organizationRead := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer}
	orgAdmin := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}

	mux.Handle("GET /api/v1/keys", adminRoute(h.ListAPIKeys))
	mux.Handle("POST /api/v1/keys", adminRoute(h.CreateAPIKey))
	mux.Handle("GET /api/v1/keys/{id}", adminRoute(h.GetAPIKey))
	mux.Handle("PUT /api/v1/keys/{id}", adminRoute(h.UpdateAPIKey))
	mux.Handle("POST /api/v1/keys/{id}/disable", adminRoute(h.DisableAPIKey))
	mux.Handle("GET /api/v1/organizations", roleRoute(h.ListOrganizations, organizationRead...))

	mux.Handle("GET /api/v1/users", roleRoute(h.ListUsers, orgAdmin...))
	mux.Handle("POST /api/v1/users", roleRoute(h.CreateUser, orgAdmin...))
	mux.Handle("PUT /api/v1/users/{id}", roleRoute(h.UpdateUser, orgAdmin...))
	mux.Handle("DELETE /api/v1/users/{id}", roleRoute(h.DeleteUser, orgAdmin...))

	mux.Handle("GET /api/v1/projects", adminRoute(h.ListProjects))
	mux.Handle("POST /api/v1/projects", roleRoute(h.CreateProject, orgAdmin...))
	mux.Handle("GET /api/v1/projects/{id}", adminRoute(h.GetProject))
	mux.Handle("PUT /api/v1/projects/{id}", adminRoute(h.UpdateProject))
	mux.Handle("GET /api/v1/projects/{id}/model-sets", adminRoute(h.ListProjectModelSets))
	mux.Handle("PUT /api/v1/projects/{id}/model-sets", adminRoute(h.ReplaceProjectModelSets))
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
	// Invalid-role JWT coverage needs a real auth_epoch row, not an invalid
	// database principal. Migration 000058 intentionally makes an unknown
	// users.role impossible to persist, so reuse a valid account and put the
	// malformed role only in the test token.
	fx.unknownRoleA = fx.normalA
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
