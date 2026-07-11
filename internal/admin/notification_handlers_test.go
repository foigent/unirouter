package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestListNotificationEvents_Success(t *testing.T) {
	now := time.Now().UTC()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			if !strings.Contains(query, "COUNT(*) FROM notification_events") {
				t.Fatalf("unexpected QueryRow: %s", query)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(query, "FROM notification_events") || !strings.Contains(query, "event_type =") || !strings.Contains(query, "delivery_state =") {
				t.Fatalf("expected event filters in query, got: %s", query)
			}
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error {
					*dest[0].(*int64) = 10
					*dest[1].(*int64) = 3
					*dest[2].(*int64) = 9
					*dest[3].(*string) = "project_budget.threshold"
					*dest[4].(*string) = "critical"
					*dest[5].(*string) = "Budget reached"
					*dest[6].(*string) = "Project Core reached 100%"
					*dest[7].(*string) = "project"
					*dest[8].(*string) = "9"
					*dest[9].(*json.RawMessage) = json.RawMessage(`{"spent_cents":1200}`)
					*dest[10].(*string) = "open"
					*dest[11].(*string) = "project_budget.threshold:9:20260701T000000Z:100"
					*dest[12].(*string) = "failed"
					*dest[13].(*sql.NullTime) = sql.NullTime{Time: now, Valid: true}
					*dest[14].(*time.Time) = now
					*dest[15].(*time.Time) = now
					return nil
				},
			}}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/notification-events?event_type=project_budget.threshold&delivery_state=failed", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ListNotificationEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	data := resp["data"].([]interface{})
	row := data[0].(map[string]interface{})
	if row["delivery_state"] != "failed" || row["project_id"].(float64) != 9 {
		t.Fatalf("unexpected event row: %#v", row)
	}
}

func TestListNotificationDeliveries_ShowsFailureState(t *testing.T) {
	now := time.Now().UTC()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			if !strings.Contains(query, "COUNT(*) FROM notification_deliveries") {
				t.Fatalf("unexpected QueryRow: %s", query)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(query, "JOIN notification_events") || !strings.Contains(query, "d.status =") {
				t.Fatalf("expected delivery join/filter query, got: %s", query)
			}
			return &testutil.MockRows{Rows: []func(dest ...any) error{
				func(dest ...any) error {
					*dest[0].(*int64) = 20
					*dest[1].(*int64) = 10
					*dest[2].(*int64) = 7
					*dest[3].(*string) = "failed"
					*dest[4].(*int) = 1
					*dest[5].(*sql.NullInt64) = sql.NullInt64{Int64: 500, Valid: true}
					*dest[6].(*string) = "webhook returned status 500"
					*dest[7].(*sql.NullTime) = sql.NullTime{Time: now.Add(5 * time.Minute), Valid: true}
					*dest[8].(*bool) = false
					*dest[9].(*sql.NullTime) = sql.NullTime{}
					*dest[10].(*sql.NullTime) = sql.NullTime{}
					*dest[11].(*time.Time) = now
					*dest[12].(*time.Time) = now
					*dest[13].(*string) = "project_budget.threshold"
					*dest[14].(*string) = "Budget reached"
					*dest[15].(*string) = "Ops webhook"
					return nil
				},
			}}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/notification-deliveries?status=failed", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleBillingAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ListNotificationDeliveries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	row := resp["data"].([]interface{})[0].(map[string]interface{})
	if row["status"] != "failed" || row["response_status"].(float64) != 500 || row["next_retry_at"] == nil {
		t.Fatalf("expected visible failed delivery state, got %#v", row)
	}
}

func TestCreateNotificationWebhook_WritesAudit(t *testing.T) {
	var auditAfter map[string]interface{}
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			if !strings.Contains(query, "INSERT INTO notification_webhooks") {
				t.Fatalf("unexpected QueryRow: %s", query)
			}
			if args[0].(int64) != 3 || args[2].(string) != "Ops webhook" {
				t.Fatalf("unexpected webhook insert args: %#v", args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 7
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "INSERT INTO audit_logs") {
				var decoded map[string]interface{}
				if err := json.Unmarshal([]byte(args[5].(string)), &decoded); err != nil {
					t.Fatalf("decode audit detail: %v", err)
				}
				auditAfter = decoded["after"].(map[string]interface{})
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/notification-webhooks", strings.NewReader(`{"org_id":3,"name":"Ops webhook","url":"https://ops.example.test/hook","event_types":["project_budget.threshold"]}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.CreateNotificationWebhook(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if auditAfter["name"] != "Ops webhook" || auditAfter["enabled"] != true {
		t.Fatalf("expected webhook audit after-state, got %#v", auditAfter)
	}
}
