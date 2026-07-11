package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F33NotificationTenantIsolationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)

	insertUser := func(orgID int64, username, role string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO users (org_id, username, password_hash, role, status)
			 VALUES ($1, $2, 'test-hash', $3, 'active') RETURNING id`,
			orgID, username, role,
		).Scan(&id))
		return id
	}
	billingA := insertUser(fx.orgA, "billing-a", auth.RoleBillingAdmin)
	projectAdminA := insertUser(fx.orgA, "project-admin-a", auth.RoleProjectAdmin)
	var projectAOther int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status, created_by)
		 VALUES ($1, 'Project A Other', 'project-a-other', 'same-org non-member project', 'active', $2) RETURNING id`,
		fx.orgA, fx.orgAdminA,
	).Scan(&projectAOther))
	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role)
		 VALUES ($1, $2, 'project_admin')`,
		fx.projectA, projectAdminA,
	)
	require.NoError(t, err)

	var eventA, eventB int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO notification_events
		 (org_id, project_id, event_type, severity, title, message, resource, resource_id, dedupe_key)
		 VALUES ($1, $2, 'tenant.test', 'info', 'event-a', 'a', 'project', $3, 'p6f33-event-a') RETURNING id`,
		fx.orgA, fx.projectA, fmt.Sprint(fx.projectA),
	).Scan(&eventA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO notification_events
		 (org_id, project_id, event_type, severity, title, message, resource, resource_id, dedupe_key)
		 VALUES ($1, $2, 'tenant.test', 'info', 'event-b', 'b', 'project', $3, 'p6f33-event-b') RETURNING id`,
		fx.orgB, fx.projectB, fmt.Sprint(fx.projectB),
	).Scan(&eventB))

	var webhookA, webhookB int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO notification_webhooks (org_id, project_id, name, url, event_types, enabled, scope_verified, scope_write_generation)
		 VALUES ($1, $2, 'webhook-a', 'https://a.example.test/hook', '["tenant.test"]', TRUE, TRUE, 1) RETURNING id`,
		fx.orgA, fx.projectA,
	).Scan(&webhookA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO notification_webhooks (org_id, project_id, name, url, event_types, enabled, scope_verified, scope_write_generation)
		 VALUES ($1, $2, 'webhook-b', 'https://b.example.test/hook', '["tenant.test"]', FALSE, TRUE, 1) RETURNING id`,
		fx.orgB, fx.projectB,
	).Scan(&webhookB))
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_deliveries (event_id, webhook_id, status, attempts)
		 VALUES ($1, $2, 'delivered', 1), ($3, $4, 'failed', 1)`,
		eventA, webhookA, eventB, webhookB,
	)
	require.NoError(t, err)

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	mux := p6f33AdminMux(h)
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

	t.Run("tenant roles cannot select a foreign organization", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			userID int64
			role   string
		}{
			{name: "org admin", userID: fx.orgAdminA, role: auth.RoleOrgAdmin},
			{name: "billing admin", userID: billingA, role: auth.RoleBillingAdmin},
		} {
			t.Run(tc.name, func(t *testing.T) {
				for _, target := range []string{
					fmt.Sprintf("/api/v1/notification-events?org_id=%d", fx.orgB),
					fmt.Sprintf("/api/v1/notification-deliveries?org_id=%d", fx.orgB),
					fmt.Sprintf("/api/v1/notification-webhooks?org_id=%d", fx.orgB),
				} {
					assertStatus(t, serve(t, http.MethodGet, target, "", tc.userID, fx.orgA, tc.role), http.StatusForbidden)
				}
			})
		}
	})

	t.Run("same tenant reads stay scoped and platform may select a tenant", func(t *testing.T) {
		for _, target := range []string{
			"/api/v1/notification-events",
			"/api/v1/notification-webhooks",
		} {
			rec := serve(t, http.MethodGet, target, "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
			assertStatus(t, rec, http.StatusOK)
			assertResponseOrgIDs(t, rec.Body.Bytes(), fx.orgA)
		}
		deliveries := serve(t, http.MethodGet, "/api/v1/notification-deliveries", "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
		assertStatus(t, deliveries, http.StatusOK)
		var deliveryResponse struct {
			Data []struct {
				EventID int64 `json:"event_id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(deliveries.Body.Bytes(), &deliveryResponse))
		require.NotEmpty(t, deliveryResponse.Data)
		for _, delivery := range deliveryResponse.Data {
			require.NotEqual(t, eventB, delivery.EventID)
		}
		rec := serve(t, http.MethodGet, fmt.Sprintf("/api/v1/notification-events?org_id=%d", fx.orgB), "", fx.platformA, fx.orgA, auth.RolePlatformAdmin)
		assertStatus(t, rec, http.StatusOK)
		assertResponseOrgIDs(t, rec.Body.Bytes(), fx.orgB)
	})

	t.Run("foreign organization and mismatched project creates have no side effects", func(t *testing.T) {
		var webhookCount, auditCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_webhooks`).Scan(&webhookCount))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditCount))

		assertStatus(t, serve(t, http.MethodPost, "/api/v1/notification-webhooks",
			fmt.Sprintf(`{"org_id":%d,"name":"foreign","url":"https://attacker.example.test/hook","event_types":["tenant.test"]}`, fx.orgB),
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/notification-webhooks",
			fmt.Sprintf(`{"org_id":%d,"name":"billing-foreign","url":"https://attacker.example.test/hook","event_types":["tenant.test"]}`, fx.orgB),
			billingA, fx.orgA, auth.RoleBillingAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/notification-webhooks",
			fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"mismatch","url":"https://attacker.example.test/hook","event_types":["tenant.test"]}`, fx.orgA, fx.projectB),
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/notification-webhooks",
			fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"project-mismatch","url":"https://attacker.example.test/hook","event_types":["tenant.test"]}`, fx.orgB, fx.projectA),
			projectAdminA, fx.orgA, auth.RoleProjectAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/notification-webhooks",
			fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"platform-mismatch","url":"https://attacker.example.test/hook","event_types":["tenant.test"]}`, fx.orgB, fx.projectA),
			fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusForbidden)

		var webhookAfter, auditAfter int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_webhooks`).Scan(&webhookAfter))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditAfter))
		require.Equal(t, webhookCount, webhookAfter)
		require.Equal(t, auditCount, auditAfter)
	})

	t.Run("project administrator writes only its own project and new routes are verified", func(t *testing.T) {
		rec := serve(t, http.MethodPost, "/api/v1/notification-webhooks",
			fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"project-own","url":"https://project.example.test/hook","event_types":["project.own"]}`, fx.orgA, fx.projectA),
			projectAdminA, fx.orgA, auth.RoleProjectAdmin)
		assertStatus(t, rec, http.StatusCreated)
		createdID := responseID(t, rec.Body.Bytes())
		var scopeVerified bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT scope_verified FROM notification_webhooks WHERE id = $1`, createdID).Scan(&scopeVerified))
		require.True(t, scopeVerified)

		assertStatus(t, serve(t, http.MethodPost, "/api/v1/notification-webhooks",
			fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"project-other","url":"https://project.example.test/hook","event_types":["project.other"]}`, fx.orgA, projectAOther),
			projectAdminA, fx.orgA, auth.RoleProjectAdmin), http.StatusForbidden)
	})

	t.Run("foreign webhook update is denied and same tenant update succeeds", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/notification-webhooks/%d", webhookB),
			`{"url":"https://attacker.example.test/hook"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusNotFound)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/notification-webhooks/%d", webhookB),
			`{"url":"https://attacker.example.test/hook"}`, billingA, fx.orgA, auth.RoleBillingAdmin), http.StatusNotFound)
		assertStatus(t, serve(t, http.MethodPut, "/api/v1/notification-webhooks/9223372036854775807",
			`{"url":"https://attacker.example.test/hook"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusNotFound)
		var foreignURL string
		require.NoError(t, pool.QueryRow(ctx, `SELECT url FROM notification_webhooks WHERE id = $1`, webhookB).Scan(&foreignURL))
		require.Equal(t, "https://b.example.test/hook", foreignURL)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/notification-webhooks/%d", webhookA),
			`{"name":"webhook-a-updated"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusOK)
		var ownName string
		var ownVerified, ownEnabled, ownPreviousEnabled bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT name, scope_verified, enabled, COALESCE(scope_quarantine_previous_enabled, FALSE)
			 FROM notification_webhooks WHERE id = $1`, webhookA,
		).Scan(&ownName, &ownVerified, &ownEnabled, &ownPreviousEnabled))
		require.Equal(t, "webhook-a-updated", ownName)
		require.False(t, ownVerified, "every route edit requires a fresh explicit scope confirmation")
		require.False(t, ownEnabled)
		require.True(t, ownPreviousEnabled)
	})

	t.Run("historical routes are quarantined until their tenant explicitly verifies scope", func(t *testing.T) {
		var calls atomic.Int32
		receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer receiver.Close()

		var historicalID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO notification_webhooks
			 (org_id, project_id, name, url, event_types, enabled, scope_verified, scope_quarantine_previous_enabled, scope_write_generation)
			 VALUES ($1, $2, 'historical-unverified', $3, '["victim.legacy"]', FALSE, FALSE, TRUE, 1) RETURNING id`,
			fx.orgB, fx.projectB, receiver.URL,
		).Scan(&historicalID))

		result, emitErr := billing.NewEventService(pool).Emit(ctx, billing.EventInput{
			OrgID:     fx.orgB,
			ProjectID: fx.projectB,
			EventType: "victim.legacy",
			Severity:  billing.NotificationSeverityWarning,
			Title:     "historical victim event",
			DedupeKey: "p6f33-historical-before-verify",
		})
		require.NoError(t, emitErr)
		require.Equal(t, billing.DeliveryStateNotConfigured, result.DeliveryState)
		require.Zero(t, calls.Load())
		_, err := pool.Exec(ctx,
			`INSERT INTO notification_deliveries
			 (event_id, webhook_id, status, attempts, next_retry_at, retry_scope_quarantined, scope_quarantine_previous_next_retry_at)
			 VALUES ($1, $2, 'failed', 1, NULL, TRUE, NOW() - INTERVAL '1 minute')`,
			result.EventID, historicalID,
		)
		require.NoError(t, err)
		retried, retryErr := billing.NewEventService(pool).RetryDueDeliveries(ctx, 10)
		require.NoError(t, retryErr)
		require.Zero(t, retried)
		require.Zero(t, calls.Load())

		var scopeVerified, enabled, previousEnabled, previousEnabledIsNull bool
		var updatedAt time.Time
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT scope_verified, enabled, COALESCE(scope_quarantine_previous_enabled, FALSE),
			        scope_quarantine_previous_enabled IS NULL, updated_at
			 FROM notification_webhooks WHERE id = $1`, historicalID,
		).Scan(&scopeVerified, &enabled, &previousEnabled, &previousEnabledIsNull, &updatedAt))
		verifyBody := func(value time.Time) string {
			return fmt.Sprintf(`{"expected_updated_at":%q}`, value.Format(time.RFC3339Nano))
		}
		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/notification-webhooks/%d/verify-scope", historicalID),
			verifyBody(updatedAt), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusNotFound)
		require.False(t, scopeVerified)
		require.False(t, enabled)
		require.True(t, previousEnabled)
		require.False(t, previousEnabledIsNull)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/notification-webhooks/%d", historicalID),
			`{"name":"historical-reviewed","enabled":true}`, fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin), http.StatusOK)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT enabled, COALESCE(scope_quarantine_previous_enabled, FALSE), updated_at
			 FROM notification_webhooks WHERE id = $1`, historicalID,
		).Scan(&enabled, &previousEnabled, &updatedAt))
		require.False(t, enabled, "editing must not bypass quarantine for an older binary")
		require.True(t, previousEnabled)

		_, err = pool.Exec(ctx, `
			CREATE FUNCTION p6f33_reject_scope_audit() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
				IF NEW.action = 'notification_webhook.scope_verify' THEN
					RAISE EXCEPTION 'injected audit failure';
				END IF;
				RETURN NEW;
			END;
			$$;
			CREATE TRIGGER p6f33_reject_scope_audit
			BEFORE INSERT ON audit_logs
			FOR EACH ROW EXECUTE FUNCTION p6f33_reject_scope_audit();`)
		require.NoError(t, err)
		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/notification-webhooks/%d/verify-scope", historicalID),
			verifyBody(updatedAt), fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin), http.StatusInternalServerError)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT scope_verified, enabled FROM notification_webhooks WHERE id = $1`, historicalID,
		).Scan(&scopeVerified, &enabled))
		require.False(t, scopeVerified, "audit failure must roll back scope verification")
		require.False(t, enabled, "audit failure must keep the historical route disabled")
		_, err = pool.Exec(ctx, `DROP TRIGGER p6f33_reject_scope_audit ON audit_logs`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `DROP FUNCTION p6f33_reject_scope_audit()`)
		require.NoError(t, err)

		assertStatus(t, serve(t, http.MethodPost, fmt.Sprintf("/api/v1/notification-webhooks/%d/verify-scope", historicalID),
			verifyBody(updatedAt), fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin), http.StatusOK)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT scope_verified, enabled, scope_quarantine_previous_enabled IS NULL
			 FROM notification_webhooks WHERE id = $1`, historicalID,
		).Scan(&scopeVerified, &enabled, &previousEnabledIsNull))
		require.True(t, scopeVerified)
		require.True(t, enabled)
		require.True(t, previousEnabledIsNull)
		var verificationAuditCount int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs
			 WHERE org_id = $1 AND user_id = $2 AND action = 'notification_webhook.scope_verify'
			   AND resource = 'notification_webhook' AND resource_id = $3`,
			fx.orgB, fx.orgAdminB, fmt.Sprint(historicalID),
		).Scan(&verificationAuditCount))
		require.Equal(t, 1, verificationAuditCount)
		retried, retryErr = billing.NewEventService(pool).RetryDueDeliveries(ctx, 10)
		require.NoError(t, retryErr)
		require.Zero(t, retried, "scope verification must not replay a pre-P6-F33 failed delivery")
		require.Zero(t, calls.Load())

		result, emitErr = billing.NewEventService(pool).Emit(ctx, billing.EventInput{
			OrgID:     fx.orgB,
			ProjectID: fx.projectB,
			EventType: "victim.legacy",
			Severity:  billing.NotificationSeverityWarning,
			Title:     "verified victim event",
			DedupeKey: "p6f33-historical-after-verify",
		})
		require.NoError(t, emitErr)
		require.Equal(t, billing.DeliveryStateDelivered, result.DeliveryState)
		require.Equal(t, int32(1), calls.Load())
	})

	t.Run("missing tenant identity fails closed", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodGet, "/api/v1/notification-events", "", fx.orgAdminA, 0, auth.RoleOrgAdmin), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/notification-webhooks",
			fmt.Sprintf(`{"org_id":%d,"name":"zero-org","url":"https://attacker.example.test/hook","event_types":["tenant.test"]}`, fx.orgA),
			fx.orgAdminA, 0, auth.RoleOrgAdmin), http.StatusForbidden)
	})

	t.Run("denied foreign sink receives no victim event", func(t *testing.T) {
		var calls atomic.Int32
		receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer receiver.Close()

		assertStatus(t, serve(t, http.MethodPost, "/api/v1/notification-webhooks",
			fmt.Sprintf(`{"org_id":%d,"name":"victim-exfil","url":%q,"event_types":["victim.event"]}`, fx.orgB, receiver.URL),
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin), http.StatusForbidden)
		result, emitErr := billing.NewEventService(pool).Emit(ctx, billing.EventInput{
			OrgID:     fx.orgB,
			ProjectID: fx.projectB,
			EventType: "victim.event",
			Severity:  billing.NotificationSeverityWarning,
			Title:     "victim event",
			DedupeKey: "p6f33-victim-event",
		})
		require.NoError(t, emitErr)
		require.NotNil(t, result)
		require.Zero(t, calls.Load())
	})

	t.Run("platform administrator keeps explicit cross-tenant authority", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/notification-webhooks/%d", webhookB),
			`{"name":"platform-updated-b"}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin), http.StatusOK)
		var name string
		require.NoError(t, pool.QueryRow(ctx, `SELECT name FROM notification_webhooks WHERE id = $1`, webhookB).Scan(&name))
		require.Equal(t, "platform-updated-b", name)
	})
}

func p6f33AdminMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	roleRoute := func(handler http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(p6f16JWTSecret)(middleware.RequireRoles(roles...)(handler))
	}
	readRoles := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin}
	mux.Handle("GET /api/v1/notification-events", roleRoute(h.ListNotificationEvents, readRoles...))
	mux.Handle("GET /api/v1/notification-deliveries", roleRoute(h.ListNotificationDeliveries, readRoles...))
	mux.Handle("GET /api/v1/notification-webhooks", roleRoute(h.ListNotificationWebhooks, readRoles...))
	mux.Handle("POST /api/v1/notification-webhooks", roleRoute(h.CreateNotificationWebhook, readRoles...))
	mux.Handle("PUT /api/v1/notification-webhooks/{id}", roleRoute(h.UpdateNotificationWebhook, readRoles...))
	mux.Handle("POST /api/v1/notification-webhooks/{id}/verify-scope", roleRoute(h.VerifyNotificationWebhookScope, readRoles...))
	return mux
}
