package admin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F35GlobalControlAuthorityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	verifier := licensing.NewVerifier(publicKey, 30).WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	})
	oldLicense := p6f35SignedLicense(t, privateKey, "p6f35-old-deployment", "2026-12-01T00:00:00Z")
	newLicense := p6f35SignedLicense(t, privateKey, "p6f35-new-deployment", "2026-12-31T00:00:00Z")
	_, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	invalidLicense := p6f35SignedLicense(t, otherPrivateKey, "p6f35-invalid-deployment", "2026-12-31T00:00:00Z")

	_, err = pool.Exec(ctx,
		`INSERT INTO settings (key, value, description, updated_at) VALUES
		 ($1, $2::jsonb, 'Offline UniRouter license document', NOW()),
		 ($3, $4::jsonb, 'Sensitive words', NOW()),
		 ('budget_alert_webhook_url', '"https://p6f35-webhook-secret.example/token"'::jsonb, 'Webhook URL', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, description = EXCLUDED.description, updated_at = NOW()`,
		licenseSettingKey, string(oldLicense), sensitiveWordsSettingKey, `["p6f35-old-word"]`)
	require.NoError(t, err)

	sensitiveFilter := filter.NewDFAFilter([]string{"p6f35-old-word"})
	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), sensitiveFilter)
	h.SetLicenseVerifier(verifier)
	mux := p6f35GlobalControlMux(h)
	serveOn := func(tb testing.TB, targetMux *http.ServeMux, method, target, body string, userID, orgID int64, role, requestID string) *httptest.ResponseRecorder {
		tb.Helper()
		token := testutil.MustAdminSessionJWT(tb, ctx, pool, userID, orgID, role, p6f16JWTSecret)
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if requestID != "" {
			req = req.WithContext(context.WithValue(req.Context(), middleware.CtxRequestID, requestID))
		}
		rec := httptest.NewRecorder()
		targetMux.ServeHTTP(rec, req)
		return rec
	}
	serve := func(tb testing.TB, method, target, body string, userID, orgID int64, role, requestID string) *httptest.ResponseRecorder {
		tb.Helper()
		return serveOn(tb, mux, method, target, body, userID, orgID, role, requestID)
	}
	assertStatus := func(tb testing.TB, rec *httptest.ResponseRecorder, want int) {
		tb.Helper()
		if rec.Code != want {
			tb.Fatalf("expected status %d, got %d: %s", want, rec.Code, rec.Body.String())
		}
	}
	tenantRoles := []string{
		auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin,
		auth.RoleDeveloper, auth.RoleViewer, auth.RoleOrgViewer,
	}

	t.Run("reads are platform only and redact dedicated or secret settings", func(t *testing.T) {
		for _, role := range tenantRoles {
			for _, target := range []string{"/api/v1/settings", "/api/v1/license", "/api/v1/sensitive-words"} {
				assertStatus(t, serve(t, http.MethodGet, target, "", fx.actorForRole(role), fx.orgA, role, ""), http.StatusForbidden)
			}
		}
		assertStatus(t, serve(t, http.MethodGet, "/api/v1/settings", "", fx.unknownRoleA, fx.orgA, "unknown_role", ""), http.StatusUnauthorized)

		settings := serve(t, http.MethodGet, "/api/v1/settings", "", fx.platformA, fx.orgA, auth.RolePlatformAdmin, "")
		assertStatus(t, settings, http.StatusOK)
		require.NotContains(t, settings.Body.String(), "p6f35-webhook-secret")
		require.NotContains(t, settings.Body.String(), "p6f35-old-word")
		require.NotContains(t, settings.Body.String(), `"signature"`)
		require.Contains(t, settings.Body.String(), `"redacted":true`)
		require.Contains(t, settings.Body.String(), `"dedicated_endpoint":"/api/v1/license"`)
		require.Contains(t, settings.Body.String(), `"retired":true`)
		require.Contains(t, settings.Body.String(), `"effective":false`)
		require.Contains(t, settings.Body.String(), `"replacement":"/api/v1/notification-webhooks"`)

		license := serve(t, http.MethodGet, "/api/v1/license", "", fx.platformA, fx.orgA, auth.RolePlatformAdmin, "")
		assertStatus(t, license, http.StatusOK)
		require.Contains(t, license.Body.String(), "p6f35-old-deployment")
		require.NotContains(t, license.Body.String(), `"signature"`)
		words := serve(t, http.MethodGet, "/api/v1/sensitive-words", "", fx.platformA, fx.orgA, auth.RolePlatformAdmin, "")
		assertStatus(t, words, http.StatusOK)
		require.Contains(t, words.Body.String(), "p6f35-old-word")
	})

	t.Run("tenant mutations are denied before parsing with no side effects", func(t *testing.T) {
		before := readP6F35Snapshot(t, pool, sensitiveFilter)
		routes := []struct {
			method string
			target string
		}{
			{http.MethodPut, "/api/v1/settings/maintenance_mode"},
			{http.MethodPut, "/api/v1/license"},
			{http.MethodPost, "/api/v1/sensitive-words"},
			{http.MethodPut, "/api/v1/sensitive-words"},
			{http.MethodDelete, "/api/v1/sensitive-words"},
		}
		for _, role := range tenantRoles {
			for _, route := range routes {
				assertStatus(t, serve(t, route.method, route.target, "not-json",
					fx.actorForRole(role), fx.orgA, role, "p6f35-denied"), http.StatusForbidden)
			}
		}
		for _, route := range routes {
			assertStatus(t, serve(t, route.method, route.target, "not-json",
				fx.unknownRoleA, fx.orgA, "unknown_role", "p6f35-invalid-role"), http.StatusUnauthorized)
		}
		require.Equal(t, before, readP6F35Snapshot(t, pool, sensitiveFilter))
	})

	t.Run("generic settings cannot bypass dedicated controls", func(t *testing.T) {
		before := readP6F35Snapshot(t, pool, sensitiveFilter)
		for _, target := range []string{
			"/api/v1/settings/license.offline",
			"/api/v1/settings/license.future",
			"/api/v1/settings/sensitive_words",
			"/api/v1/settings/budget_alert_webhook_url",
			"/api/v1/settings/budget_alert_threshold_cents",
			"/api/v1/settings/budget_cents",
		} {
			assertStatus(t, serve(t, http.MethodPut, target, `{"value":{"bypass":true}}`,
				fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f35-reserved"), http.StatusConflict)
		}
		require.Equal(t, before, readP6F35Snapshot(t, pool, sensitiveFilter))
	})

	t.Run("platform writes audit atomically without raw control material", func(t *testing.T) {
		requestIDs := []string{
			"p6f35-setting", "p6f35-webhook", "p6f35-license",
			"p6f35-words-add", "p6f35-words-replace", "p6f35-words-delete",
		}
		assertStatus(t, serve(t, http.MethodPut, "/api/v1/settings/maintenance_mode", `{"value":true}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[0]), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPut, "/api/v1/settings/support_contact",
			`{"value":"support@example.test"}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[1]), http.StatusOK)
		assertStatus(t, serve(t, http.MethodPut, "/api/v1/license", fmt.Sprintf(`{"license":%s}`, newLicense),
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[2]), http.StatusOK)

		assertStatus(t, serve(t, http.MethodPost, "/api/v1/sensitive-words", `{"words":["p6f35-added-word"]}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[3]), http.StatusOK)
		blocked, _ := sensitiveFilter.IsSensitive("contains p6f35-added-word")
		require.True(t, blocked)
		assertStatus(t, serve(t, http.MethodPut, "/api/v1/sensitive-words", `{"words":["p6f35-replace-word","p6f35-remove-word"]}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[4]), http.StatusOK)
		assertStatus(t, serve(t, http.MethodDelete, "/api/v1/sensitive-words", `{"words":["p6f35-remove-word"]}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[5]), http.StatusOK)
		blocked, _ = sensitiveFilter.IsSensitive("contains p6f35-replace-word")
		require.True(t, blocked)
		blocked, _ = sensitiveFilter.IsSensitive("contains p6f35-remove-word")
		require.False(t, blocked)

		var auditCount int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs WHERE request_id = ANY($1::text[])`, requestIDs,
		).Scan(&auditCount))
		require.Equal(t, len(requestIDs), auditCount)
		var actorOrgID, actorUserID int64
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT org_id, user_id FROM audit_logs WHERE request_id = 'p6f35-setting'`,
		).Scan(&actorOrgID, &actorUserID))
		require.Equal(t, fx.orgA, actorOrgID)
		require.Equal(t, fx.platformA, actorUserID)

		var auditJSON string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COALESCE(string_agg(detail::text || before_state::text || after_state::text, ''), '')
			 FROM audit_logs WHERE request_id = ANY($1::text[])`, requestIDs,
		).Scan(&auditJSON))
		for _, secret := range []string{
			"p6f35-new-webhook-secret", "p6f35-added-word", "p6f35-replace-word", "p6f35-remove-word",
			`"signature"`,
		} {
			require.NotContains(t, auditJSON, secret)
		}
		require.Contains(t, auditJSON, "platform_global")
	})

	t.Run("long valid control identifiers remain auditable", func(t *testing.T) {
		longKey := strings.Repeat("k", 100)
		assertStatus(t, serve(t, http.MethodPut, "/api/v1/settings/"+longKey, `{"value":true}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f35-long-setting-key"), http.StatusOK)
		var settingResourceID, settingDetail string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT resource_id, detail::text FROM audit_logs WHERE request_id = 'p6f35-long-setting-key'`,
		).Scan(&settingResourceID, &settingDetail))
		require.Len(t, settingResourceID, 64)
		require.Contains(t, settingDetail, longKey)
		require.Contains(t, settingDetail, `"resource_id_hashed": true`)

		longDeploymentID := strings.Repeat("deployment-", 8)
		longLicense := p6f35SignedLicense(t, privateKey, longDeploymentID, "2026-12-31T00:00:00Z")
		assertStatus(t, serve(t, http.MethodPut, "/api/v1/license", fmt.Sprintf(`{"license":%s}`, longLicense),
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f35-long-license-id"), http.StatusOK)
		var licenseResourceID, licenseDetail string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT resource_id, detail::text FROM audit_logs WHERE request_id = 'p6f35-long-license-id'`,
		).Scan(&licenseResourceID, &licenseDetail))
		require.Len(t, licenseResourceID, 64)
		require.Contains(t, licenseDetail, longDeploymentID)
		require.Contains(t, licenseDetail, `"resource_id_hashed": true`)
	})

	t.Run("database audit failure rolls back all three control mutations", func(t *testing.T) {
		_, err := pool.Exec(ctx, `CREATE OR REPLACE FUNCTION p6f35_fail_selected_audit() RETURNS trigger AS $$
		BEGIN
		  IF COALESCE(NEW.request_id, '') LIKE 'p6f35-forced-audit-%' THEN
		    RAISE EXCEPTION 'forced p6f35 audit failure';
		  END IF;
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `CREATE TRIGGER p6f35_fail_selected_audit
			BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION p6f35_fail_selected_audit()`)
		require.NoError(t, err)
		defer func() {
			_, _ = pool.Exec(ctx, `DROP TRIGGER IF EXISTS p6f35_fail_selected_audit ON audit_logs`)
			_, _ = pool.Exec(ctx, `DROP FUNCTION IF EXISTS p6f35_fail_selected_audit()`)
		}()

		cases := []struct {
			method, target, body, requestID string
		}{
			{http.MethodPut, "/api/v1/settings/maintenance_mode", `{"value":false}`, "p6f35-forced-audit-setting"},
			{http.MethodPut, "/api/v1/license", fmt.Sprintf(`{"license":%s}`, oldLicense), "p6f35-forced-audit-license"},
			{http.MethodPost, "/api/v1/sensitive-words", `{"words":["p6f35-forced-audit-word"]}`, "p6f35-forced-audit-words"},
		}
		for _, tc := range cases {
			before := readP6F35Snapshot(t, pool, sensitiveFilter)
			rec := serve(t, tc.method, tc.target, tc.body, fx.platformA, fx.orgA, auth.RolePlatformAdmin, tc.requestID)
			assertStatus(t, rec, http.StatusInternalServerError)
			require.Contains(t, rec.Body.String(), "Audit failed")
			require.Equal(t, before, readP6F35Snapshot(t, pool, sensitiveFilter))
		}
	})

	t.Run("invalid license has no write audit or event side effect", func(t *testing.T) {
		before := readP6F35Snapshot(t, pool, sensitiveFilter)
		rec := serve(t, http.MethodPut, "/api/v1/license", fmt.Sprintf(`{"license":%s}`, invalidLicense),
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f35-invalid-license")
		assertStatus(t, rec, http.StatusBadRequest)
		require.Equal(t, before, readP6F35Snapshot(t, pool, sensitiveFilter))
	})

	t.Run("concurrent sensitive-word additions preserve the union", func(t *testing.T) {
		var startingRaw string
		var startingVersion time.Time
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT value::text, updated_at FROM settings WHERE key = $1`, sensitiveWordsSettingKey,
		).Scan(&startingRaw, &startingVersion))
		startingWords, err := decodeSensitiveWords([]byte(startingRaw), true)
		require.NoError(t, err)
		secondFilter := filter.NewDFAFilter(nil)
		require.True(t, secondFilter.UpdateWordsVersioned(startingWords, startingVersion))
		secondHandler := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), secondFilter)
		secondMux := p6f35GlobalControlMux(secondHandler)

		type addition struct {
			requestID string
			word      string
			targetMux *http.ServeMux
		}
		additions := []addition{
			{requestID: "p6f35-concurrent-a", word: "p6f35-concurrent-word-a", targetMux: mux},
			{requestID: "p6f35-concurrent-b", word: "p6f35-concurrent-word-b", targetMux: secondMux},
		}
		start := make(chan struct{})
		responses := make(chan *httptest.ResponseRecorder, len(additions))
		for _, addition := range additions {
			addition := addition
			go func() {
				<-start
				responses <- serveOn(t, addition.targetMux, http.MethodPost, "/api/v1/sensitive-words",
					fmt.Sprintf(`{"words":[%q]}`, addition.word),
					fx.platformA, fx.orgA, auth.RolePlatformAdmin, addition.requestID)
			}()
		}
		close(start)
		for range additions {
			assertStatus(t, <-responses, http.StatusOK)
		}
		var raw string
		var finalVersion time.Time
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT value::text, updated_at FROM settings WHERE key = $1`, sensitiveWordsSettingKey,
		).Scan(&raw, &finalVersion))
		require.True(t, finalVersion.After(startingVersion))
		finalWords, err := decodeSensitiveWords([]byte(raw), true)
		require.NoError(t, err)
		sensitiveFilter.UpdateWordsVersioned(finalWords, finalVersion)
		secondFilter.UpdateWordsVersioned(finalWords, finalVersion)
		for _, addition := range additions {
			require.Contains(t, raw, addition.word)
			for _, instanceFilter := range []*filter.DFAFilter{sensitiveFilter, secondFilter} {
				blocked, _ := instanceFilter.IsSensitive("contains " + addition.word)
				require.True(t, blocked)
			}
		}
		var auditCount int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs WHERE request_id = ANY($1::text[])`,
			[]string{additions[0].requestID, additions[1].requestID},
		).Scan(&auditCount))
		require.Equal(t, 2, auditCount)
		rows, err := pool.Query(ctx,
			`SELECT before_state->>'value_sha256', after_state->>'value_sha256', detail->>'policy_version'
			 FROM audit_logs WHERE request_id = ANY($1::text[]) ORDER BY id`,
			[]string{additions[0].requestID, additions[1].requestID})
		require.NoError(t, err)
		defer rows.Close()
		type auditHashPair struct{ before, after, version string }
		var chain []auditHashPair
		for rows.Next() {
			var pair auditHashPair
			require.NoError(t, rows.Scan(&pair.before, &pair.after, &pair.version))
			chain = append(chain, pair)
		}
		require.NoError(t, rows.Err())
		require.Len(t, chain, 2)
		require.Equal(t, chain[0].after, chain[1].before)
		firstVersion, err := time.Parse(time.RFC3339Nano, chain[0].version)
		require.NoError(t, err)
		secondVersion, err := time.Parse(time.RFC3339Nano, chain[1].version)
		require.NoError(t, err)
		require.True(t, secondVersion.After(firstVersion))
	})

	t.Run("legacy admin compatibility and invalid authentication", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodGet, "/api/v1/settings", "", fx.legacyAdminA, fx.orgA, auth.RoleLegacyAdmin, ""), http.StatusOK)
		for _, authorization := range []string{"", "Bearer invalid"} {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/maintenance_mode", bytes.NewBufferString("not-json"))
			if authorization != "" {
				req.Header.Set("Authorization", authorization)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
		}
	})
}

func p6f35GlobalControlMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	platformRoute := func(handler http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(h.db, p6f16JWTSecret)(middleware.RequireRoles(auth.RolePlatformAdmin)(handler))
	}
	mux.Handle("GET /api/v1/settings", platformRoute(h.ListSettings))
	mux.Handle("PUT /api/v1/settings/{key}", platformRoute(h.UpdateSetting))
	mux.Handle("GET /api/v1/license", platformRoute(h.GetLicense))
	mux.Handle("PUT /api/v1/license", platformRoute(h.UpdateLicense))
	mux.Handle("GET /api/v1/sensitive-words", platformRoute(h.ListSensitiveWords))
	mux.Handle("POST /api/v1/sensitive-words", platformRoute(h.AddSensitiveWords))
	mux.Handle("PUT /api/v1/sensitive-words", platformRoute(h.UpdateSensitiveWords))
	mux.Handle("DELETE /api/v1/sensitive-words", platformRoute(h.DeleteSensitiveWords))
	return mux
}

func p6f35SignedLicense(t testing.TB, privateKey ed25519.PrivateKey, deploymentID, expiresAt string) []byte {
	t.Helper()
	doc, err := licensing.SignPayload(licensing.Payload{
		Customer: "P6F35 Customer", DeploymentID: deploymentID, Tier: "enterprise",
		IssuedAt: "2026-07-01T00:00:00Z", ExpiresAt: expiresAt,
		Features: []string{"projects"}, Limits: map[string]int64{"projects": 20},
	}, privateKey)
	require.NoError(t, err)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	return raw
}

type p6f35Snapshot struct {
	SettingsCount, AuditCount, EventCount int
	License, SensitiveWords               string
	MaintenanceMode, WebhookURL           string
	FilterWordCount                       int
	OldWordBlocked, DeniedWordBlocked     bool
}

func readP6F35Snapshot(t testing.TB, pool *pgxpool.Pool, sensitiveFilter *filter.DFAFilter) p6f35Snapshot {
	t.Helper()
	ctx := context.Background()
	var got p6f35Snapshot
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM settings`).Scan(&got.SettingsCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&got.AuditCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_events`).Scan(&got.EventCount))
	for key, dest := range map[string]*string{
		licenseSettingKey: &got.License, sensitiveWordsSettingKey: &got.SensitiveWords,
		"maintenance_mode": &got.MaintenanceMode, "budget_alert_webhook_url": &got.WebhookURL,
	} {
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COALESCE((SELECT value::text FROM settings WHERE key = $1), '<missing>')`, key,
		).Scan(dest))
	}
	got.FilterWordCount = sensitiveFilter.WordCount()
	got.OldWordBlocked, _ = sensitiveFilter.IsSensitive("contains p6f35-old-word")
	got.DeniedWordBlocked, _ = sensitiveFilter.IsSensitive("contains p6f35-denied-word")
	return got
}
