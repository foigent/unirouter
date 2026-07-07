package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestGetLogDetail_NullSafeUpstreamError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "COALESCE(upstream_error, '')") {
				t.Fatalf("expected upstream_error to be null-safe, got query: %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				now := time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC)
				*dest[0].(*int64) = 42
				*dest[1].(*string) = "batch16-request"
				*dest[2].(*string) = "batch16-smoke-gpt-4o"
				*dest[3].(*string) = "batch16-mock"
				*dest[4].(*string) = "prd2-batch16-openai-compatible-mock"
				*dest[5].(*int) = 10
				*dest[6].(*int) = 5
				*dest[7].(*int) = 15
				*dest[8].(*int) = 1
				*dest[9].(*int) = http.StatusOK
				*dest[10].(*int) = 12
				*dest[11].(*int) = 0
				*dest[12].(**int) = nil
				*dest[13].(*string) = ""
				*dest[14].(*string) = "upstream"
				*dest[15].(*bool) = false
				*dest[16].(*string) = "127.0.0.1"
				*dest[17].(*string) = "company-chat"
				*dest[18].(*time.Time) = now
				return nil
			}}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/logs/7", nil)
	req.SetPathValue("id", "7")
	rec := httptest.NewRecorder()

	h.GetLogDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"upstream_error":""`) {
		t.Fatalf("expected empty upstream_error in response, got %s", rec.Body.String())
	}
}

func TestGetRequestTrace_ProjectScopedAccess(t *testing.T) {
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "FROM project_members") {
					*dest[0].(*int) = 1
					return nil
				}
				if !strings.Contains(sql, "FROM call_logs") {
					t.Fatalf("unexpected QueryRow SQL: %s", sql)
				}
				if args[0] != int64(31) {
					t.Fatalf("expected log id arg 31, got %#v", args)
				}
				*dest[0].(*int64) = 7
				*dest[1].(*int64) = 1
				*dest[2].(*string) = "req-trace-001"
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "FROM request_trace_events") {
				t.Fatalf("expected trace event query, got %s", sql)
			}
			if !strings.Contains(sql, "COALESCE(project_id, 0) = $3") {
				t.Fatalf("expected project-scoped trace filter, got %s", sql)
			}
			if args[0] != "req-trace-001" || args[1] != int64(1) || args[2] != int64(7) {
				t.Fatalf("unexpected trace query args: %#v", args)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "gateway.request.accepted"
						*dest[2].(*string) = "gateway"
						*dest[3].(*string) = "info"
						*dest[4].(*string) = "gpt-4o"
						*dest[5].(*string) = "company-chat"
						*dest[6].(*string) = ""
						*dest[7].(*string) = ""
						*dest[8].(**int) = nil
						*dest[9].(*int) = 3
						*dest[10].(*[]byte) = []byte(`{"estimated_tokens":100}`)
						*dest[11].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/logs/31/trace", nil)
	req.SetPathValue("id", "31")
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(10))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.GetRequestTrace(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		RequestID string                   `json:"request_id"`
		Events    []map[string]interface{} `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.RequestID != "req-trace-001" || len(resp.Events) != 1 {
		t.Fatalf("unexpected trace response: %#v", resp)
	}
	if resp.Events[0]["event_type"] != "gateway.request.accepted" {
		t.Fatalf("expected event_type in response, got %#v", resp.Events[0])
	}
}
