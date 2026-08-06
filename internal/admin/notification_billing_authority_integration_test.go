package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestBillingNotificationAuthorityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)

	var projectAOther int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, created_by)
		 VALUES ($1, 'Billing Other Project', 'billing-other-project', 'billing authority fixture', 'active', $2)
		 RETURNING id`,
		fx.orgA, fx.orgAdminA,
	).Scan(&projectAOther))

	insertEvent := func(orgID, projectID int64, eventType, title, dedupeKey string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO notification_events
			 (org_id, project_id, event_type, severity, title, message, resource, resource_id, dedupe_key)
			 VALUES ($1, $2, $3, 'warning', $4, $5, 'project', $6, $7)
			 RETURNING id`,
			orgID, projectID, eventType, title, title, fmt.Sprint(projectID), dedupeKey,
		).Scan(&id))
		return id
	}
	budgetEventA := insertEvent(fx.orgA, fx.projectA, billing.EventProjectBudgetThreshold, "own budget", "billing-authority-own-budget")
	meteringEventA := insertEvent(fx.orgA, projectAOther, billing.EventTokenMeteringEstimatedRatioHigh, "own metering", "billing-authority-own-metering")
	operationalEventA := insertEvent(fx.orgA, fx.projectA, billing.EventAPIKeyDisabled, "own operational", "billing-authority-own-operational")
	foreignBudgetEvent := insertEvent(fx.orgB, fx.projectB, billing.EventProjectBudgetThreshold, "foreign budget", "billing-authority-foreign-budget")

	insertWebhook := func(orgID, projectID int64, name string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO notification_webhooks
			 (org_id, project_id, name, url, event_types, enabled, scope_verified, scope_write_generation)
			 VALUES ($1, $2, $3, 'https://notify.example.test/hook', '[]', TRUE, TRUE, 1)
			 RETURNING id`,
			orgID, projectID, name,
		).Scan(&id))
		return id
	}
	webhookA := insertWebhook(fx.orgA, fx.projectA, "own route")
	webhookAOther := insertWebhook(fx.orgA, projectAOther, "other project route")
	webhookB := insertWebhook(fx.orgB, fx.projectB, "foreign route")

	insertDelivery := func(eventID, webhookID int64) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO notification_deliveries (event_id, webhook_id, status, attempts, response_status)
			 VALUES ($1, $2, 'delivered', 1, 204) RETURNING id`,
			eventID, webhookID,
		).Scan(&id))
		return id
	}
	budgetDeliveryA := insertDelivery(budgetEventA, webhookA)
	meteringDeliveryA := insertDelivery(meteringEventA, webhookAOther)
	operationalDeliveryA := insertDelivery(operationalEventA, webhookA)
	foreignDelivery := insertDelivery(foreignBudgetEvent, webhookB)

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	mux := p6f33AdminMux(h)
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
	decodeEvents := func(tb testing.TB, rec *httptest.ResponseRecorder) []struct {
		ID        int64  `json:"id"`
		OrgID     int64  `json:"org_id"`
		ProjectID *int64 `json:"project_id"`
		EventType string `json:"event_type"`
	} {
		tb.Helper()
		var response struct {
			Data []struct {
				ID        int64  `json:"id"`
				OrgID     int64  `json:"org_id"`
				ProjectID *int64 `json:"project_id"`
				EventType string `json:"event_type"`
			} `json:"data"`
		}
		require.NoError(tb, json.Unmarshal(rec.Body.Bytes(), &response))
		return response.Data
	}
	decodeDeliveryIDs := func(tb testing.TB, rec *httptest.ResponseRecorder) []int64 {
		tb.Helper()
		var response struct {
			Data []struct {
				ID int64 `json:"id"`
			} `json:"data"`
		}
		require.NoError(tb, json.Unmarshal(rec.Body.Bytes(), &response))
		ids := make([]int64, 0, len(response.Data))
		for _, row := range response.Data {
			ids = append(ids, row.ID)
		}
		return ids
	}

	t.Run("billing reads only financial events and deliveries in its organization", func(t *testing.T) {
		events := serve(t, http.MethodGet, "/api/v1/notification-events", "", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
		assertStatus(t, events, http.StatusOK)
		rows := decodeEvents(t, events)
		require.Len(t, rows, 2)
		seen := map[int64]bool{}
		for _, row := range rows {
			require.Equal(t, fx.orgA, row.OrgID)
			require.True(t, billing.IsFinancialNotificationEventType(row.EventType))
			seen[row.ID] = true
		}
		require.True(t, seen[budgetEventA])
		require.True(t, seen[meteringEventA])
		require.False(t, seen[operationalEventA])
		require.False(t, seen[foreignBudgetEvent])

		deliveries := serve(t, http.MethodGet, "/api/v1/notification-deliveries", "", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
		assertStatus(t, deliveries, http.StatusOK)
		deliveryIDs := decodeDeliveryIDs(t, deliveries)
		require.ElementsMatch(t, []int64{budgetDeliveryA, meteringDeliveryA}, deliveryIDs)
		require.NotContains(t, deliveryIDs, operationalDeliveryA)
		require.NotContains(t, deliveryIDs, foreignDelivery)
	})

	t.Run("event filters cannot widen the finance allowlist or tenant boundary", func(t *testing.T) {
		nonFinancial := serve(t, http.MethodGet,
			"/api/v1/notification-events?event_type="+billing.EventAPIKeyDisabled,
			"", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
		assertStatus(t, nonFinancial, http.StatusOK)
		require.Empty(t, decodeEvents(t, nonFinancial))

		foreignOrg := serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/notification-events?org_id=%d", fx.orgB),
			"", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
		assertStatus(t, foreignOrg, http.StatusForbidden)

		ownProject := serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/notification-events?project_id=%d", projectAOther),
			"", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
		assertStatus(t, ownProject, http.StatusOK)
		ownProjectRows := decodeEvents(t, ownProject)
		require.Len(t, ownProjectRows, 1)
		require.Equal(t, meteringEventA, ownProjectRows[0].ID)

		foreignProject := serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/notification-events?project_id=%d", fx.projectB),
			"", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
		assertStatus(t, foreignProject, http.StatusForbidden)

		foreignProjectDeliveries := serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/notification-deliveries?project_id=%d", fx.projectB),
			"", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
		assertStatus(t, foreignProjectDeliveries, http.StatusForbidden)
	})

	t.Run("billing lifecycle transitions preserve finance tenant and project scope", func(t *testing.T) {
		ack := serve(t, http.MethodPost,
			fmt.Sprintf("/api/v1/notification-events/%d/acknowledge", budgetEventA),
			"", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
		assertStatus(t, ack, http.StatusOK)

		for _, tc := range []struct {
			name   string
			path   string
			event  int64
			status string
		}{
			{name: "non-financial", path: fmt.Sprintf("/api/v1/notification-events/%d/acknowledge", operationalEventA), event: operationalEventA, status: "open"},
			{name: "foreign tenant", path: fmt.Sprintf("/api/v1/notification-events/%d/acknowledge", foreignBudgetEvent), event: foreignBudgetEvent, status: "open"},
			{name: "mismatched project filter", path: fmt.Sprintf("/api/v1/notification-events/%d/resolve?project_id=%d", meteringEventA, fx.projectA), event: meteringEventA, status: "open"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rec := serve(t, http.MethodPost, tc.path, "", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
				assertStatus(t, rec, http.StatusNotFound)
				var status string
				require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM notification_events WHERE id = $1`, tc.event).Scan(&status))
				require.Equal(t, tc.status, status)
			})
		}

		resolve := serve(t, http.MethodPost,
			fmt.Sprintf("/api/v1/notification-events/%d/resolve?project_id=%d", meteringEventA, projectAOther),
			"", fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
		assertStatus(t, resolve, http.StatusOK)
		var budgetStatus, meteringStatus string
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM notification_events WHERE id = $1`, budgetEventA).Scan(&budgetStatus))
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM notification_events WHERE id = $1`, meteringEventA).Scan(&meteringStatus))
		require.Equal(t, "acknowledged", budgetStatus)
		require.Equal(t, "resolved", meteringStatus)
	})

	t.Run("all webhook operations return forbidden without persistence or audit side effects", func(t *testing.T) {
		var webhookCount, deliveryCount, auditCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_webhooks`).Scan(&webhookCount))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_deliveries`).Scan(&deliveryCount))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditCount))
		var beforeName, beforeURL string
		var beforeEnabled, beforeVerified bool
		var beforeUpdatedAt time.Time
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT name, url, enabled, scope_verified, updated_at FROM notification_webhooks WHERE id = $1`, webhookA,
		).Scan(&beforeName, &beforeURL, &beforeEnabled, &beforeVerified, &beforeUpdatedAt))

		tests := []struct {
			name   string
			method string
			path   string
			body   string
		}{
			{name: "list", method: http.MethodGet, path: "/api/v1/notification-webhooks"},
			{name: "create", method: http.MethodPost, path: "/api/v1/notification-webhooks", body: fmt.Sprintf(`{"org_id":%d,"name":"forbidden","url":"https://forbidden.example.test/hook"}`, fx.orgA)},
			{name: "update", method: http.MethodPut, path: fmt.Sprintf("/api/v1/notification-webhooks/%d", webhookA), body: `{"name":"forbidden update"}`},
			{name: "verify", method: http.MethodPost, path: fmt.Sprintf("/api/v1/notification-webhooks/%d/verify-scope", webhookA), body: fmt.Sprintf(`{"expected_updated_at":%q}`, beforeUpdatedAt.Format(time.RFC3339Nano))},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				rec := serve(t, tc.method, tc.path, tc.body, fx.billingAdminA, fx.orgA, auth.RoleBillingAdmin)
				assertStatus(t, rec, http.StatusForbidden)
			})
		}

		var webhookAfter, deliveryAfter, auditAfter int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_webhooks`).Scan(&webhookAfter))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_deliveries`).Scan(&deliveryAfter))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditAfter))
		require.Equal(t, webhookCount, webhookAfter)
		require.Equal(t, deliveryCount, deliveryAfter)
		require.Equal(t, auditCount, auditAfter)
		var afterName, afterURL string
		var afterEnabled, afterVerified bool
		var afterUpdatedAt time.Time
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT name, url, enabled, scope_verified, updated_at FROM notification_webhooks WHERE id = $1`, webhookA,
		).Scan(&afterName, &afterURL, &afterEnabled, &afterVerified, &afterUpdatedAt))
		require.Equal(t, beforeName, afterName)
		require.Equal(t, beforeURL, afterURL)
		require.Equal(t, beforeEnabled, afterEnabled)
		require.Equal(t, beforeVerified, afterVerified)
		require.True(t, beforeUpdatedAt.Equal(afterUpdatedAt))
	})
}
