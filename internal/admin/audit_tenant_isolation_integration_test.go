package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F37AuditLogTenantIsolationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)

	var billingAdminA int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'p6f37-billing-a', 'test-hash', $2, 'active') RETURNING id`,
		fx.orgA, auth.RoleBillingAdmin,
	).Scan(&billingAdminA))

	_, err := pool.Exec(ctx,
		`INSERT INTO audit_logs
		 (org_id, project_id, user_id, action, resource, resource_id, detail, before_state, after_state, source, request_id, created_at)
		 VALUES
		 ($1, $2, $3, 'p6f37.audit', 'notification_webhook', 'route-a',
		  '{"tenant":"a"}'::jsonb, '{}'::jsonb, '{}'::jsonb, 'admin_console', 'p6f37-a', NOW()),
		 ($4, $5, $6, 'p6f37.audit', 'notification_webhook', 'route-b',
		  '{"tenant":"b","url":"https://tenant-b.example.test/hook?secret=victim"}'::jsonb,
		  '{}'::jsonb, '{}'::jsonb, 'admin_console', 'p6f37-b', NOW())`,
		fx.orgA, fx.projectA, fx.orgAdminA,
		fx.orgB, fx.projectB, fx.orgAdminB,
	)
	require.NoError(t, err)

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	route := middleware.AdminAuth(p6f16JWTSecret)(
		middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin)(http.HandlerFunc(h.ListAuditLogs)),
	)
	serve := func(tb testing.TB, target string, userID, orgID int64, role string) *httptest.ResponseRecorder {
		tb.Helper()
		token, tokenErr := auth.GenerateJWT(userID, orgID, role, p6f16JWTSecret, 1)
		require.NoError(tb, tokenErr)
		req := httptest.NewRequest(http.MethodGet, target, bytes.NewReader(nil))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		route.ServeHTTP(rec, req)
		return rec
	}
	assertStatus := func(tb testing.TB, rec *httptest.ResponseRecorder, want int) {
		tb.Helper()
		if rec.Code != want {
			tb.Fatalf("expected status %d, got %d: %s", want, rec.Code, rec.Body.String())
		}
	}
	type auditResponse struct {
		Data []struct {
			OrgID     int64           `json:"org_id"`
			ProjectID int64           `json:"project_id"`
			ActorID   int64           `json:"actor_id"`
			RequestID string          `json:"request_id"`
			Detail    json.RawMessage `json:"detail"`
		} `json:"data"`
		Total int `json:"total"`
	}
	decode := func(tb testing.TB, rec *httptest.ResponseRecorder) auditResponse {
		tb.Helper()
		var response auditResponse
		require.NoError(tb, json.Unmarshal(rec.Body.Bytes(), &response))
		return response
	}

	for _, tc := range []struct {
		name   string
		userID int64
		role   string
	}{
		{name: "organization administrator", userID: fx.orgAdminA, role: auth.RoleOrgAdmin},
		{name: "billing administrator", userID: billingAdminA, role: auth.RoleBillingAdmin},
	} {
		t.Run(tc.name+" is always constrained to its JWT organization", func(t *testing.T) {
			rec := serve(t, "/api/v1/audit-logs?action=p6f37.audit", tc.userID, fx.orgA, tc.role)
			assertStatus(t, rec, http.StatusOK)
			response := decode(t, rec)
			require.Equal(t, 1, response.Total)
			require.Len(t, response.Data, 1)
			require.Equal(t, fx.orgA, response.Data[0].OrgID)
			require.Equal(t, "p6f37-a", response.Data[0].RequestID)
			require.NotContains(t, string(response.Data[0].Detail), "victim")

			foreignOrg := serve(t,
				fmt.Sprintf("/api/v1/audit-logs?org_id=%d&action=p6f37.audit", fx.orgB),
				tc.userID, fx.orgA, tc.role,
			)
			assertStatus(t, foreignOrg, http.StatusForbidden)

			foreignProject := serve(t,
				fmt.Sprintf("/api/v1/audit-logs?project_id=%d&action=p6f37.audit", fx.projectB),
				tc.userID, fx.orgA, tc.role,
			)
			assertStatus(t, foreignProject, http.StatusForbidden)
		})
	}

	t.Run("same-organization filters remain available", func(t *testing.T) {
		rec := serve(t,
			fmt.Sprintf("/api/v1/audit-logs?org_id=%d&project_id=%d&actor_id=%d&action=p6f37.audit", fx.orgA, fx.projectA, fx.orgAdminA),
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin,
		)
		assertStatus(t, rec, http.StatusOK)
		response := decode(t, rec)
		require.Equal(t, 1, response.Total)
		require.Len(t, response.Data, 1)
		require.Equal(t, fx.projectA, response.Data[0].ProjectID)
		require.Equal(t, fx.orgAdminA, response.Data[0].ActorID)
	})

	t.Run("platform administrator keeps explicit cross-tenant authority", func(t *testing.T) {
		rec := serve(t,
			fmt.Sprintf("/api/v1/audit-logs?org_id=%d&action=p6f37.audit", fx.orgB),
			fx.platformA, fx.orgA, auth.RolePlatformAdmin,
		)
		assertStatus(t, rec, http.StatusOK)
		response := decode(t, rec)
		require.Equal(t, 1, response.Total)
		require.Len(t, response.Data, 1)
		require.Equal(t, fx.orgB, response.Data[0].OrgID)
		require.Contains(t, string(response.Data[0].Detail), "victim")
	})

	t.Run("missing organization identity fails closed", func(t *testing.T) {
		rec := serve(t, "/api/v1/audit-logs?action=p6f37.audit", fx.orgAdminA, 0, auth.RoleOrgAdmin)
		assertStatus(t, rec, http.StatusForbidden)
	})
}
