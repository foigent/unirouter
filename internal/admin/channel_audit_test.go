package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCreateChannel_AuditIncludesAfterState(t *testing.T) {
	var auditAfter string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 42
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditAfter = args[12].(string)
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/channels", strings.NewReader(`{"name":"primary","provider":"openai","base_url":"https://api.openai.com","models":"gpt-4o","priority":3}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(auditAfter, `"name":"primary"`) || !strings.Contains(auditAfter, `"adapter_type":"openai_compatible"`) {
		t.Fatalf("expected channel create audit after-state, got %s", auditAfter)
	}
}

func TestUpdateChannel_AuditIncludesBeforeAfterState(t *testing.T) {
	var auditBefore, auditAfter string
	snapshotCalls := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "FROM channels WHERE id = $1") {
				return &testutil.MockRow{}
			}
			snapshotCalls++
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = map[bool]string{true: "before", false: "after"}[snapshotCalls == 1]
				*dest[1].(*string) = "openai"
				*dest[2].(*string) = "https://api.openai.com"
				*dest[3].(*string) = "gpt-4o"
				*dest[4].(*string) = "openai_compatible"
				*dest[5].(*int) = map[bool]int{true: 1, false: 2}[snapshotCalls == 1]
				*dest[6].(*int) = 3
				*dest[7].(*string) = "staged"
				*dest[8].(*string) = "healthy"
				*dest[9].(*int) = 0
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditBefore = args[11].(string)
				auditAfter = args[12].(string)
				return pgconn.NewCommandTag("INSERT 1"), nil
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPut, "/channels/9", strings.NewReader(`{"name":"after","priority":2}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(auditBefore, `"name":"before"`) || !strings.Contains(auditAfter, `"name":"after"`) {
		t.Fatalf("expected channel update before/after audit, before=%s after=%s", auditBefore, auditAfter)
	}
}

func TestDeleteChannel_AuditIncludesBeforeState(t *testing.T) {
	var auditBefore string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = "deleted"
				*dest[1].(*string) = "openai"
				*dest[2].(*string) = "https://api.openai.com"
				*dest[3].(*string) = "gpt-4o"
				*dest[4].(*string) = "openai_compatible"
				*dest[5].(*int) = 1
				*dest[6].(*int) = 3
				*dest[7].(*string) = "active"
				*dest[8].(*string) = "healthy"
				*dest[9].(*int) = 0
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditBefore = args[11].(string)
				return pgconn.NewCommandTag("INSERT 1"), nil
			}
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodDelete, "/channels/9", nil)
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.DeleteChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(auditBefore, `"name":"deleted"`) || strings.Contains(auditBefore, "sk-") {
		t.Fatalf("expected sanitized channel delete before-state, got %s", auditBefore)
	}
}

func TestTestChannel_WritesAuditWithoutSecret(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer upstream.Close()

	keyEnc, err := auth.EncryptKey([]byte("bad-upstream-key"), []byte("0123456789abcdef01234567"))
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}

	queryCount := 0
	var auditAfter string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				switch queryCount {
				case 1:
					*dest[0].(*string) = upstream.URL
					*dest[1].(*string) = "mimo-v2.5"
					*dest[2].(*string) = "openai_compatible"
					*dest[3].(*json.RawMessage) = json.RawMessage(`{}`)
				case 2:
					*dest[0].(*string) = keyEnc
				default:
					t.Fatalf("unexpected query count %d", queryCount)
				}
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditAfter = args[12].(string)
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	h := newTestHandler(db)

	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(2))
	req := httptest.NewRequest(http.MethodPost, "/channels/1/test", strings.NewReader(`{"model":"mimo-v2.5","prompt":"ping"}`)).WithContext(ctx)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.TestChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(auditAfter, `"success":false`) || !strings.Contains(auditAfter, `"status_code":401`) {
		t.Fatalf("expected failed test-channel audit, got %s", auditAfter)
	}
	if strings.Contains(auditAfter, "bad-upstream-key") || strings.Contains(auditAfter, "sk-") || strings.Contains(auditAfter, "curl") {
		t.Fatalf("channel.test audit leaked secret/debug material: %s", auditAfter)
	}
}
