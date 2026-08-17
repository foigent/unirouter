package admin

import (
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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/testutil"
)

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

func TestGlobalControlHandlersFailClosedBeforeDatabaseAccess(t *testing.T) {
	queryCalls, beginCalls := 0, 0
	db := &testutil.MockDB{
		QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			queryCalls++
			return &testutil.MockRows{}, nil
		},
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			queryCalls++
			return &testutil.MockRow{}
		},
		BeginFn: func(context.Context) (pgx.Tx, error) {
			beginCalls++
			return &testutil.MockTx{}, nil
		},
	}
	h := newTestHandler(db)
	tests := []struct {
		method, target, body string
		handler              http.HandlerFunc
	}{
		{http.MethodGet, "/settings", "", h.ListSettings},
		{http.MethodPut, "/settings/site_name", "not-json", h.UpdateSetting},
		{http.MethodGet, "/license", "", h.GetLicense},
		{http.MethodPut, "/license", "not-json", h.UpdateLicense},
		{http.MethodGet, "/sensitive-words", "", h.ListSensitiveWords},
		{http.MethodPost, "/sensitive-words", "not-json", h.AddSensitiveWords},
		{http.MethodPut, "/sensitive-words", "not-json", h.UpdateSensitiveWords},
		{http.MethodDelete, "/sensitive-words", "not-json", h.DeleteSensitiveWords},
	}
	for _, tc := range tests {
		req := pricingActorRequest(httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body)), 1, auth.RoleOrgAdmin)
		if tc.target == "/settings/site_name" {
			req.SetPathValue("key", "site_name")
		}
		rec := httptest.NewRecorder()
		tc.handler(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code, "%s %s", tc.method, tc.target)
	}
	require.Zero(t, queryCalls)
	require.Zero(t, beginCalls)
}

func TestUpdateSettingRollsBackWhenAuditInsertFails(t *testing.T) {
	committed, rolledBack := false, false
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "RETURNING value::text") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "true"
					return nil
				}}
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				return pgconn.CommandTag{}, fmt.Errorf("audit unavailable")
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		CommitFn:   func(context.Context) error { committed = true; return nil },
		RollbackFn: func(context.Context) error { rolledBack = true; return nil },
	}
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	req := platformGlobalControlRequest(httptest.NewRequest(http.MethodPut, "/settings/maintenance_mode", strings.NewReader(`{"value":true}`)))
	req.SetPathValue("key", "maintenance_mode")
	rec := httptest.NewRecorder()

	h.UpdateSetting(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Audit failed")
	require.False(t, committed)
	require.True(t, rolledBack)
}

func TestUpdateLicenseRollsBackWhenAuditInsertFails(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	licenseJSON := p6f35SignedLicense(t, privateKey, "p6f35-audit-failure", "2026-12-31T00:00:00Z")
	committed, rolledBack := false, false
	tx := &testutil.MockTx{
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				return pgconn.CommandTag{}, fmt.Errorf("audit unavailable")
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		CommitFn:   func(context.Context) error { committed = true; return nil },
		RollbackFn: func(context.Context) error { rolledBack = true; return nil },
	}
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	h.SetLicenseVerifier(licensing.NewVerifier(publicKey, 30).WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	}))
	req := platformGlobalControlRequest(httptest.NewRequest(http.MethodPut, "/license",
		strings.NewReader(fmt.Sprintf(`{"license":%s}`, licenseJSON))))
	rec := httptest.NewRecorder()

	h.UpdateLicense(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Audit failed")
	require.False(t, committed)
	require.True(t, rolledBack)
}

func TestSensitiveWordsAuditFailureDoesNotUpdateInMemoryFilter(t *testing.T) {
	committed, rolledBack := false, false
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "RETURNING updated_at") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*time.Time) = time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
					return nil
				}}
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = `["old-word"]`
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				return pgconn.CommandTag{}, fmt.Errorf("audit unavailable")
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		CommitFn:   func(context.Context) error { committed = true; return nil },
		RollbackFn: func(context.Context) error { rolledBack = true; return nil },
	}
	sensitiveFilter := filter.NewDFAFilter([]string{"old-word"})
	h := NewHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }},
		"test-secret", []byte("0123456789abcdef01234567"), sensitiveFilter)
	req := platformGlobalControlRequest(httptest.NewRequest(http.MethodPut, "/sensitive-words",
		strings.NewReader(`{"words":["new-word"]}`)))
	rec := httptest.NewRecorder()

	h.UpdateSensitiveWords(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "Audit failed")
	require.False(t, committed)
	require.True(t, rolledBack)
	oldBlocked, _ := sensitiveFilter.IsSensitive("contains old-word")
	newBlocked, _ := sensitiveFilter.IsSensitive("contains new-word")
	require.True(t, oldBlocked)
	require.False(t, newBlocked)
}

func TestDedicatedAndRetiredGlobalSettingsAreRejectedBeforeTransaction(t *testing.T) {
	for _, key := range []string{
		licenseSettingKey, "license.future", sensitiveWordsSettingKey,
		"budget_alert_webhook_url", "budget_alert_threshold_cents", "budget_cents",
	} {
		beginCalls := 0
		h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
			beginCalls++
			return &testutil.MockTx{}, nil
		}})
		req := platformGlobalControlRequest(httptest.NewRequest(http.MethodPut, "/settings/"+key,
			strings.NewReader(`{"value":true}`)))
		req.SetPathValue("key", key)
		rec := httptest.NewRecorder()

		h.UpdateSetting(rec, req)

		require.Equal(t, http.StatusConflict, rec.Code)
		require.Zero(t, beginCalls)
		if retired, ok := retiredGlobalSettingContract(key); ok {
			require.Contains(t, rec.Body.String(), retired.Replacement)
			require.Contains(t, rec.Body.String(), retired.Reason)
		}
	}
}

func TestSensitiveWordsRejectMissingOrNullWordsButAllowsExplicitEmptyArray(t *testing.T) {
	tests := []struct {
		method  string
		handler func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{http.MethodPost, (*Handler).AddSensitiveWords},
		{http.MethodPut, (*Handler).UpdateSensitiveWords},
		{http.MethodDelete, (*Handler).DeleteSensitiveWords},
	}
	for _, tc := range tests {
		for _, body := range []string{`{}`, `{"words":null}`} {
			h := newTestHandler(&testutil.MockDB{})
			req := platformGlobalControlRequest(httptest.NewRequest(tc.method, "/sensitive-words", strings.NewReader(body)))
			rec := httptest.NewRecorder()

			tc.handler(h, rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
		}
	}

	h := newTestHandler(globalControlTestDB(&testutil.MockDB{}))
	req := platformGlobalControlRequest(httptest.NewRequest(http.MethodPut, "/sensitive-words", strings.NewReader(`{"words":[]}`)))
	rec := httptest.NewRecorder()
	h.UpdateSensitiveWords(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestNormalizeGlobalControlAuditIPFitsAuditSchema(t *testing.T) {
	require.Equal(t, "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
		normalizeGlobalControlAuditIP("[ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff]:65535"))
	require.Equal(t, "fe80::1", normalizeGlobalControlAuditIP("[fe80::1%lo0]:443"))
	require.Equal(t, "192.0.2.1", normalizeGlobalControlAuditIP("192.0.2.1:1234"))
	require.Empty(t, normalizeGlobalControlAuditIP(strings.Repeat("invalid-host", 8)))
}

func TestGlobalControlAuditResourceIDPreservesShortAndHashesLongValues(t *testing.T) {
	shortID, hashed := globalControlAuditResourceID("short-id")
	require.Equal(t, "short-id", shortID)
	require.False(t, hashed)
	longID, hashed := globalControlAuditResourceID(strings.Repeat("x", 100))
	require.Len(t, longID, 64)
	require.True(t, hashed)
}

func TestGlobalControlCommitFailureDoesNotApplyPostCommitEffects(t *testing.T) {
	t.Run("setting", func(t *testing.T) {
		tx := &testutil.MockTx{
			QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "RETURNING value::text") {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*string) = "true"
						return nil
					}}
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			},
			ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
			CommitFn: func(context.Context) error { return fmt.Errorf("commit outcome unknown") },
		}
		h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
		req := platformGlobalControlRequest(httptest.NewRequest(http.MethodPut, "/settings/maintenance_mode",
			strings.NewReader(`{"value":true}`)))
		req.SetPathValue("key", "maintenance_mode")
		rec := httptest.NewRecorder()

		h.UpdateSetting(rec, req)

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Contains(t, rec.Body.String(), "Commit failed")
	})

	t.Run("license", func(t *testing.T) {
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		licenseJSON := p6f35SignedLicense(t, privateKey, "p6f35-commit-failure", "2026-07-20T00:00:00Z")
		tx := &testutil.MockTx{
			QueryRowFn: func(context.Context, string, ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			},
			ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
			CommitFn: func(context.Context) error { return fmt.Errorf("commit outcome unknown") },
		}
		h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
		h.SetLicenseVerifier(licensing.NewVerifier(publicKey, 30).WithNow(func() time.Time {
			return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
		}))
		eventQueries := 0
		eventDB := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			eventQueries++
			return &testutil.MockRow{}
		}}
		h.SetEventService(billing.NewEventService(eventDB, ""))
		req := platformGlobalControlRequest(httptest.NewRequest(http.MethodPut, "/license",
			strings.NewReader(fmt.Sprintf(`{"license":%s}`, licenseJSON))))
		rec := httptest.NewRecorder()

		h.UpdateLicense(rec, req)

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Contains(t, rec.Body.String(), "Commit failed")
		require.Zero(t, eventQueries)
	})

	t.Run("sensitive words", func(t *testing.T) {
		version := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
		tx := &testutil.MockTx{
			QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if strings.Contains(sql, "RETURNING updated_at") {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*time.Time) = version
						return nil
					}}
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*string) = `["old-word"]`
					return nil
				}}
			},
			ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("INSERT 1"), nil
			},
			CommitFn: func(context.Context) error { return fmt.Errorf("commit outcome unknown") },
		}
		sensitiveFilter := filter.NewDFAFilter([]string{"old-word"})
		h := NewHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }},
			"test-secret", []byte("0123456789abcdef01234567"), sensitiveFilter)
		req := platformGlobalControlRequest(httptest.NewRequest(http.MethodPut, "/sensitive-words",
			strings.NewReader(`{"words":["new-word"]}`)))
		rec := httptest.NewRecorder()

		h.UpdateSensitiveWords(rec, req)

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Contains(t, rec.Body.String(), "Commit failed")
		oldBlocked, _ := sensitiveFilter.IsSensitive("contains old-word")
		newBlocked, _ := sensitiveFilter.IsSensitive("contains new-word")
		require.True(t, oldBlocked)
		require.False(t, newBlocked)
	})
}
