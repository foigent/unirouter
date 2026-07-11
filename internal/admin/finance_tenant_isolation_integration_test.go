package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F32FinanceAndUsageTenantIsolationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)

	insertUser := func(orgID int64, username, role string) int64 {
		t.Helper()
		var userID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO users (org_id, username, password_hash, role, status)
			 VALUES ($1, $2, 'test-hash', $3, 'active') RETURNING id`,
			orgID, username, role,
		).Scan(&userID))
		return userID
	}
	billingA := insertUser(fx.orgA, "p6f32-billing-a", auth.RoleBillingAdmin)
	billingB := insertUser(fx.orgB, "p6f32-billing-b", auth.RoleBillingAdmin)
	developerB := insertUser(fx.orgB, "p6f32-developer-b", auth.RoleDeveloper)
	var projectB2 int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, created_by)
		 VALUES ($1, 'P6F32 Project B2', 'p6f32-project-b2', 'same-org non-member project', 'active', $2) RETURNING id`,
		fx.orgB, fx.orgAdminB,
	).Scan(&projectB2))
	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role)
		 VALUES ($1, $2, 'project_developer')`, fx.projectB, developerB)
	require.NoError(t, err)

	_, err = pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance, granted_balance, frozen, total_spent)
		 VALUES ($1, 1100, 100, 0, 11), ($2, 2200, 200, 0, 22)`,
		fx.orgA, fx.orgB,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_events
		 (org_id, project_id, event_type, severity, title, message, resource, resource_id)
		 VALUES
		 ($1, $2, 'p6f32.victim', 'critical', 'victim-a-private', 'victim-a-private', 'request', 'p6f32-victim-request-a'),
		 ($3, $4, 'p6f32.own', 'warning', 'own-b-incident', 'own-b-incident', 'request', 'p6f32-own-request-b')`,
		fx.orgA, fx.projectA, fx.orgB, fx.projectB,
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx,
		`INSERT INTO transactions (request_id, org_id, type, amount, balance_after, note)
		 VALUES
		 ('p6f32-tx-victim-a', $1, 'topup', 111, 1100, 'victim-a-private'),
		 ('p6f32-tx-own-b', $2, 'topup', 222, 2200, '=own-b,comma')`,
		fx.orgA, fx.orgB,
	)
	require.NoError(t, err)

	var victimGrantID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO grants (org_id, amount, balance, reason, created_by, client_request_id)
		 VALUES ($1, 111, 111, 'victim-a-private', $2, 'p6f32-grant-collision') RETURNING id`,
		fx.orgA, billingA,
	).Scan(&victimGrantID))
	_, err = pool.Exec(ctx,
		`INSERT INTO grants (org_id, amount, balance, reason, created_by, client_request_id)
		 VALUES ($1, 222, 222, 'own-b-grant', $2, 'p6f32-grant-own-b')`,
		fx.orgB, billingB,
	)
	require.NoError(t, err)

	var logA, logB, logB2 int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO call_logs
		 (request_id, org_id, project_id, model, provider, channel, prompt_tokens, completion_tokens,
		  total_tokens, estimated_tokens, cost_cents, status_code, usage_source, usage_confidence,
		  trace_id, upstream_request_id, requested_model, ip_address)
		 VALUES
		 ('p6f32-victim-request-a', $1, $2, 'victim-model-a', 'victim-provider-a', 'victim-channel-a',
		  900, 100, 1000, 100, 999, 200, 'estimated', 0.20,
		  'p6f32-victim-trace-a', 'p6f32-victim-upstream-a', 'victim-alias-a', '10.0.0.1') RETURNING id`,
		fx.orgA, fx.projectA,
	).Scan(&logA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO call_logs
		 (request_id, org_id, project_id, model, provider, channel, prompt_tokens, completion_tokens,
		  total_tokens, estimated_tokens, cost_cents, status_code, usage_source, usage_confidence,
		  trace_id, upstream_request_id, requested_model, ip_address)
		 VALUES
		 ('p6f32-own-request-b', $1, $2, 'own-model-b', 'own-provider-b', 'own-channel-b',
		  12, 8, 20, 20, 22, 200, 'upstream', 1.0,
		  'p6f32-own-trace-b', 'p6f32-own-upstream-b', 'own-alias-b', '10.0.0.2') RETURNING id`,
		fx.orgB, fx.projectB,
	).Scan(&logB))
	_, err = pool.Exec(ctx,
		`INSERT INTO request_trace_events
		 (request_id, org_id, project_id, event_type, stage, model, provider, channel, metadata)
		 VALUES
		 ('p6f32-victim-request-a', $1, $2, 'provider.response', 'provider', 'victim-model-a', 'victim-provider-a', 'victim-channel-a', '{"marker":"victim-a-private"}'),
		 ('p6f32-own-request-b', $3, $4, 'provider.response', 'provider', 'own-model-b', 'own-provider-b', 'own-channel-b', '{"marker":"own-b"}')`,
		fx.orgA, fx.projectA, fx.orgB, fx.projectB,
	)
	require.NoError(t, err)

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	h.SetEventService(billing.NewEventService(pool))
	mux := p6f32AdminMux(h)
	serve := func(tb testing.TB, method, target, body string, userID, orgID int64, role string) *httptest.ResponseRecorder {
		tb.Helper()
		token, tokenErr := auth.GenerateJWT(userID, orgID, role, p6f16JWTSecret, 1)
		require.NoError(tb, tokenErr)
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
	assertNoVictim := func(tb testing.TB, rec *httptest.ResponseRecorder) {
		tb.Helper()
		require.NotContains(tb, rec.Body.String(), "victim-a-private")
		require.NotContains(tb, rec.Body.String(), "p6f32-victim")
		require.NotContains(tb, rec.Body.String(), "victim-model-a")
	}

	t.Run("billing administrator defaults every read to its JWT organization", func(t *testing.T) {
		var databaseNow time.Time
		require.NoError(t, pool.QueryRow(ctx, `SELECT NOW()`).Scan(&databaseNow))
		dashboardTarget := fmt.Sprintf(
			"/api/v1/dashboard/stats?start_time=%s&end_time=%s",
			databaseNow.Add(-time.Hour).UTC().Format(time.RFC3339),
			databaseNow.Add(time.Hour).UTC().Format(time.RFC3339),
		)
		wallet := serve(t, http.MethodGet, "/api/v1/wallet", "", billingB, fx.orgB, auth.RoleBillingAdmin)
		assertStatus(t, wallet, http.StatusOK)
		var walletBody struct {
			OrgID        int64 `json:"org_id"`
			BalanceCents int   `json:"balance_cents"`
			IsAdmin      bool  `json:"is_admin"`
		}
		require.NoError(t, json.Unmarshal(wallet.Body.Bytes(), &walletBody))
		require.Equal(t, fx.orgB, walletBody.OrgID)
		require.Equal(t, 2200, walletBody.BalanceCents)
		require.True(t, walletBody.IsAdmin)
		for _, target := range []string{
			"/api/v1/wallet/transactions",
			"/api/v1/grants",
			"/api/v1/reconciliation",
			"/api/v1/logs",
		} {
			rec := serve(t, http.MethodGet, target, "", billingB, fx.orgB, auth.RoleBillingAdmin)
			assertStatus(t, rec, http.StatusOK)
			require.Equal(t, 1, parseP6F32ResponseTotal(t, rec), target)
			assertNoVictim(t, rec)
		}

		for _, target := range []string{
			"/api/v1/wallet/transactions/export",
			"/api/v1/usage/analytics?group_by=overall",
			"/api/v1/internal-status",
			dashboardTarget,
			"/api/v1/runtime/status",
		} {
			rec := serve(t, http.MethodGet, target, "", billingB, fx.orgB, auth.RoleBillingAdmin)
			assertStatus(t, rec, http.StatusOK)
			assertNoVictim(t, rec)
		}

		usage := serve(t, http.MethodGet, "/api/v1/usage/analytics?group_by=overall", "", billingB, fx.orgB, auth.RoleBillingAdmin)
		assertStatus(t, usage, http.StatusOK)
		var usageBody struct {
			Summary struct {
				TotalRequests  int64   `json:"total_requests"`
				TotalTokens    int64   `json:"total_tokens"`
				CostCents      int64   `json:"cost_cents"`
				EstimatedRatio float64 `json:"estimated_ratio"`
			} `json:"summary"`
		}
		require.NoError(t, json.Unmarshal(usage.Body.Bytes(), &usageBody))
		require.EqualValues(t, 1, usageBody.Summary.TotalRequests)
		require.EqualValues(t, 20, usageBody.Summary.TotalTokens)
		require.EqualValues(t, 22, usageBody.Summary.CostCents)
		require.Zero(t, usageBody.Summary.EstimatedRatio)

		internalStatus := serve(t, http.MethodGet, "/api/v1/internal-status", "", billingB, fx.orgB, auth.RoleBillingAdmin)
		assertStatus(t, internalStatus, http.StatusOK)
		require.Contains(t, internalStatus.Body.String(), "own-b-incident")
		assertNoVictim(t, internalStatus)

		dashboard := serve(t, http.MethodGet, dashboardTarget, "", billingB, fx.orgB, auth.RoleBillingAdmin)
		assertStatus(t, dashboard, http.StatusOK)
		require.Equal(t, "1", extractP6F32JSONNumber(t, dashboard, "total_requests"))
		require.Equal(t, "22", extractP6F32JSONNumber(t, dashboard, "today_cost_cents"))

		runtime := serve(t, http.MethodGet, "/api/v1/runtime/status", "", billingB, fx.orgB, auth.RoleBillingAdmin)
		assertStatus(t, runtime, http.StatusOK)
		var runtimeBody struct {
			CapacityHealth struct {
				Traffic struct {
					Requests    int64 `json:"requests"`
					TotalTokens int64 `json:"total_tokens"`
					CostCents   int64 `json:"cost_cents"`
				} `json:"traffic"`
			} `json:"capacity_health"`
		}
		require.NoError(t, json.Unmarshal(runtime.Body.Bytes(), &runtimeBody))
		require.EqualValues(t, 1, runtimeBody.CapacityHealth.Traffic.Requests)
		require.EqualValues(t, 20, runtimeBody.CapacityHealth.Traffic.TotalTokens)
		require.EqualValues(t, 22, runtimeBody.CapacityHealth.Traffic.CostCents)

		var notificationCount int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM notification_events WHERE org_id = $1 AND event_type = $2`,
			fx.orgB, billing.EventTokenMeteringEstimatedRatioHigh,
		).Scan(&notificationCount))
		require.Zero(t, notificationCount, "foreign estimated usage must not derive a tenant-B alert")
	})

	t.Run("wallet CSV is tenant scoped, parseable and formula neutralized", func(t *testing.T) {
		rec := serve(t, http.MethodGet, "/api/v1/wallet/transactions/export", "", billingB, fx.orgB, auth.RoleBillingAdmin)
		assertStatus(t, rec, http.StatusOK)
		body := strings.TrimPrefix(rec.Body.String(), "\ufeff")
		records, csvErr := csv.NewReader(strings.NewReader(body)).ReadAll()
		require.NoError(t, csvErr)
		require.Len(t, records, 2)
		require.Equal(t, "'=own-b,comma", records[1][4])
		assertNoVictim(t, rec)
	})

	t.Run("grant list accepts historical null idempotency keys", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`INSERT INTO grants (org_id, amount, balance, reason, created_by, client_request_id)
			 VALUES ($1, 1, 1, 'legacy-null-client-request', $2, NULL)`, fx.orgB, billingB)
		require.NoError(t, err)
		rec := serve(t, http.MethodGet, "/api/v1/grants", "", billingB, fx.orgB, auth.RoleBillingAdmin)
		assertStatus(t, rec, http.StatusOK)
		require.Contains(t, rec.Body.String(), "legacy-null-client-request")
	})

	t.Run("foreign filters and writes fail before any financial or derived side effect", func(t *testing.T) {
		type snapshot struct {
			balanceA, balanceB           int
			transactions, grants, audits int
			notificationEvents           int
		}
		readSnapshot := func(tb testing.TB) snapshot {
			tb.Helper()
			var got snapshot
			require.NoError(tb, pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE org_id = $1`, fx.orgA).Scan(&got.balanceA))
			require.NoError(tb, pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE org_id = $1`, fx.orgB).Scan(&got.balanceB))
			require.NoError(tb, pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions`).Scan(&got.transactions))
			require.NoError(tb, pool.QueryRow(ctx, `SELECT COUNT(*) FROM grants`).Scan(&got.grants))
			require.NoError(tb, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&got.audits))
			require.NoError(tb, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_events`).Scan(&got.notificationEvents))
			return got
		}
		before := readSnapshot(t)

		for _, target := range []string{
			fmt.Sprintf("/api/v1/wallet?org_id=%d", fx.orgA),
			fmt.Sprintf("/api/v1/wallet/transactions?org_id=%d", fx.orgA),
			fmt.Sprintf("/api/v1/wallet/transactions/export?org_id=%d", fx.orgA),
			fmt.Sprintf("/api/v1/grants?org_id=%d", fx.orgA),
			fmt.Sprintf("/api/v1/reconciliation?org_id=%d", fx.orgA),
			fmt.Sprintf("/api/v1/logs?org_id=%d", fx.orgA),
			fmt.Sprintf("/api/v1/usage/analytics?org_id=%d", fx.orgA),
			fmt.Sprintf("/api/v1/internal-status?org_id=%d", fx.orgA),
			fmt.Sprintf("/api/v1/dashboard/stats?org_id=%d", fx.orgA),
			fmt.Sprintf("/api/v1/runtime/status?org_id=%d", fx.orgA),
		} {
			assertStatus(t, serve(t, http.MethodGet, target, "", billingB, fx.orgB, auth.RoleBillingAdmin), http.StatusForbidden)
		}
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/wallet/topup",
			fmt.Sprintf(`{"org_id":%d,"amount_cents":10}`, fx.orgA), billingB, fx.orgB, auth.RoleBillingAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/wallet/adjust",
			fmt.Sprintf(`{"org_id":%d,"target_balance":10}`, fx.orgA), billingB, fx.orgB, auth.RoleBillingAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/grants",
			fmt.Sprintf(`{"org_id":%d,"amount_cents":10,"reason":"foreign"}`, fx.orgA), billingB, fx.orgB, auth.RoleBillingAdmin), http.StatusForbidden)

		require.Equal(t, before, readSnapshot(t))
	})

	t.Run("foreign and missing log identifiers are non-enumerating", func(t *testing.T) {
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO call_logs
			 (request_id, org_id, project_id, model, provider, channel, total_tokens, cost_cents, status_code, usage_source)
			 VALUES ('p6f32-same-org-other-project-b', $1, $2, 'other-project-model-b', 'own-provider-b', 'own-channel-b', 3, 4, 200, 'upstream')
			 RETURNING id`, fx.orgB, projectB2,
		).Scan(&logB2))
		for _, target := range []string{
			fmt.Sprintf("/api/v1/logs/%d", logA),
			fmt.Sprintf("/api/v1/logs/%d/trace", logA),
			"/api/v1/logs/9223372036854775807",
			"/api/v1/logs/9223372036854775807/trace",
		} {
			rec := serve(t, http.MethodGet, target, "", billingB, fx.orgB, auth.RoleBillingAdmin)
			assertStatus(t, rec, http.StatusNotFound)
			assertNoVictim(t, rec)
		}
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/logs/%d", logB), "", billingB, fx.orgB, auth.RoleBillingAdmin), http.StatusOK)
		trace := serve(t, http.MethodGet, fmt.Sprintf("/api/v1/logs/%d/trace", logB), "", billingB, fx.orgB, auth.RoleBillingAdmin)
		assertStatus(t, trace, http.StatusOK)
		require.Contains(t, trace.Body.String(), "own-channel-b")

		for _, target := range []string{
			fmt.Sprintf("/api/v1/logs/%d", logB2),
			fmt.Sprintf("/api/v1/logs/%d/trace", logB2),
			"/api/v1/logs/9223372036854775807",
			"/api/v1/logs/9223372036854775807/trace",
		} {
			assertStatus(t, serve(t, http.MethodGet, target, "", developerB, fx.orgB, auth.RoleDeveloper), http.StatusNotFound)
		}
		assertStatus(t, serve(t, http.MethodGet, fmt.Sprintf("/api/v1/logs/%d", logB), "", developerB, fx.orgB, auth.RoleDeveloper), http.StatusOK)
	})

	t.Run("same-tenant mutations touch only the actor organization", func(t *testing.T) {
		var originalA int
		require.NoError(t, pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE org_id = $1`, fx.orgA).Scan(&originalA))

		assertStatus(t, serve(t, http.MethodPost, "/api/v1/wallet/topup",
			`{"amount_cents":100,"description":"own-b-topup","client_request_id":"p6f32-topup-b"}`,
			billingB, fx.orgB, auth.RoleBillingAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/wallet/adjust",
			`{"target_balance":2400,"description":"own-b-adjust"}`,
			billingB, fx.orgB, auth.RoleBillingAdmin), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/grants",
			`{"amount_cents":50,"reason":"own-b-grant-new","client_request_id":"p6f32-grant-new-b"}`,
			billingB, fx.orgB, auth.RoleBillingAdmin), http.StatusCreated)

		var currentA int
		require.NoError(t, pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE org_id = $1`, fx.orgA).Scan(&currentA))
		require.Equal(t, originalA, currentA)
		var bAuditCount int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND user_id = $2 AND action IN ('wallet.topup', 'wallet.adjust', 'wallet.grant')`,
			fx.orgB, billingB,
		).Scan(&bAuditCount))
		require.Equal(t, 3, bAuditCount)
		var grantAuditRequestID string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT request_id FROM audit_logs
			 WHERE org_id = $1 AND user_id = $2 AND action = 'wallet.grant'
			 ORDER BY id DESC LIMIT 1`, fx.orgB, billingB,
		).Scan(&grantAuditRequestID))
		require.True(t, strings.HasPrefix(grantAuditRequestID, "op_"), grantAuditRequestID)
	})

	t.Run("tenant-scoped grant idempotency permits the same client value without disclosing a foreign grant", func(t *testing.T) {
		before := fmt.Sprint(victimGrantID)
		rec := serve(t, http.MethodPost, "/api/v1/grants",
			`{"amount_cents":10,"reason":"collision","client_request_id":"p6f32-grant-collision"}`,
			billingB, fx.orgB, auth.RoleBillingAdmin)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), before)
		var count int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM grants WHERE client_request_id = 'p6f32-grant-collision'`,
		).Scan(&count))
		require.Equal(t, 2, count)
	})

	t.Run("platform administrator can explicitly select a target organization", func(t *testing.T) {
		defaultWallet := serve(t, http.MethodGet, "/api/v1/wallet", "", fx.platformA, fx.orgA, auth.RolePlatformAdmin)
		assertStatus(t, defaultWallet, http.StatusOK)
		require.Contains(t, defaultWallet.Body.String(), fmt.Sprintf(`"org_id":%d`, fx.orgA))

		foreignWallet := serve(t, http.MethodGet, fmt.Sprintf("/api/v1/wallet?org_id=%d", fx.orgB), "", fx.platformA, fx.orgA, auth.RolePlatformAdmin)
		assertStatus(t, foreignWallet, http.StatusOK)
		require.Contains(t, foreignWallet.Body.String(), fmt.Sprintf(`"org_id":%d`, fx.orgB))
	})

	t.Run("missing tenant identity fails closed", func(t *testing.T) {
		for _, target := range []string{
			"/api/v1/wallet",
			"/api/v1/grants",
			"/api/v1/reconciliation",
			"/api/v1/logs",
			"/api/v1/usage/analytics",
			"/api/v1/internal-status",
			"/api/v1/dashboard/stats",
			"/api/v1/runtime/status",
		} {
			assertStatus(t, serve(t, http.MethodGet, target, "", billingB, 0, auth.RoleBillingAdmin), http.StatusForbidden)
		}
	})
}

func p6f32AdminMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	roleRoute := func(handler http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(p6f16JWTSecret)(middleware.RequireRoles(roles...)(handler))
	}
	billingRoles := []string{auth.RolePlatformAdmin, auth.RoleBillingAdmin}
	usageRoles := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer}

	mux.Handle("GET /api/v1/wallet", roleRoute(h.GetWallet, billingRoles...))
	mux.Handle("POST /api/v1/wallet/topup", roleRoute(h.TopUp, billingRoles...))
	mux.Handle("POST /api/v1/wallet/adjust", roleRoute(h.BalanceAdjust, billingRoles...))
	mux.Handle("GET /api/v1/wallet/transactions", roleRoute(h.ListWalletTransactions, billingRoles...))
	mux.Handle("GET /api/v1/wallet/transactions/export", roleRoute(h.ExportWalletTransactions, billingRoles...))
	mux.Handle("GET /api/v1/grants", roleRoute(h.ListGrants, billingRoles...))
	mux.Handle("POST /api/v1/grants", roleRoute(h.CreateGrant, billingRoles...))
	mux.Handle("GET /api/v1/reconciliation", roleRoute(h.ListReconciliation, billingRoles...))
	mux.Handle("GET /api/v1/logs", roleRoute(h.ListLogs, usageRoles...))
	mux.Handle("GET /api/v1/logs/{id}", roleRoute(h.GetLogDetail, usageRoles...))
	mux.Handle("GET /api/v1/logs/{id}/trace", roleRoute(h.GetRequestTrace, usageRoles...))
	mux.Handle("GET /api/v1/usage/analytics", roleRoute(h.UsageAnalytics, usageRoles...))
	mux.Handle("GET /api/v1/internal-status", roleRoute(h.InternalStatus, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin))
	mux.Handle("GET /api/v1/dashboard/stats", roleRoute(h.DashboardStats, usageRoles...))
	mux.Handle("GET /api/v1/runtime/status", roleRoute(h.RuntimeStatus, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin))
	return mux
}

func parseP6F32ResponseTotal(t testing.TB, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var response struct {
		Total json.Number `json:"total"`
	}
	decoder := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&response))
	total, err := strconv.Atoi(response.Total.String())
	require.NoError(t, err)
	return total
}

func extractP6F32JSONNumber(t testing.TB, rec *httptest.ResponseRecorder, field string) string {
	t.Helper()
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	value, ok := body[field]
	require.True(t, ok, "missing JSON field %q", field)
	var number json.Number
	require.NoError(t, json.Unmarshal(value, &number))
	return number.String()
}
