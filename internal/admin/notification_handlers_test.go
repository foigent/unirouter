package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

func TestListNotificationEvents_BillingUsesFinancialEventAllowlist(t *testing.T) {
	assertFinancialScope := func(query string, args []any) {
		t.Helper()
		if !strings.Contains(query, "event_type = ANY(") {
			t.Fatalf("billing event query is missing the financial allowlist: %s", query)
		}
		if len(args) < 2 {
			t.Fatalf("billing event query is missing allowlist args: %#v", args)
		}
		assertFinancialNotificationTypes(t, args[1])
	}
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, query string, args ...any) pgx.Row {
			assertFinancialScope(query, args)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(_ context.Context, query string, args ...any) (pgx.Rows, error) {
			assertFinancialScope(query, args)
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/notification-events?event_type=api_key.disabled", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleBillingAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ListNotificationEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected billing-scoped empty response, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListNotificationDeliveries_ShowsFailureState(t *testing.T) {
	now := time.Now().UTC()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			if !strings.Contains(query, "COUNT(*) FROM notification_deliveries") {
				t.Fatalf("unexpected QueryRow: %s", query)
			}
			if !strings.Contains(query, "e.event_type = ANY(") {
				t.Fatalf("billing delivery query is missing the financial allowlist: %s", query)
			}
			assertFinancialNotificationTypes(t, args[1])
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(query, "JOIN notification_events") || !strings.Contains(query, "d.status =") || !strings.Contains(query, "e.event_type = ANY(") {
				t.Fatalf("expected delivery join/filter query, got: %s", query)
			}
			assertFinancialNotificationTypes(t, args[1])
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
	var inserted, audited, committed bool
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			if !strings.Contains(query, "INSERT INTO notification_webhooks") {
				t.Fatalf("unexpected QueryRow: %s", query)
			}
			if args[0].(int64) != 3 || args[2].(string) != "Ops webhook" {
				t.Fatalf("unexpected webhook insert args: %#v", args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 7
				inserted = true
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "INSERT INTO audit_logs") {
				if !inserted {
					t.Fatal("webhook audit ran before the webhook insert")
				}
				var decoded map[string]interface{}
				if err := json.Unmarshal([]byte(args[5].(string)), &decoded); err != nil {
					t.Fatalf("decode audit detail: %v", err)
				}
				auditAfter = decoded["after"].(map[string]interface{})
				audited = true
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
		CommitFn: func(context.Context) error {
			if !audited {
				t.Fatal("webhook transaction committed before its audit")
			}
			committed = true
			return nil
		},
	}
	db := &testutil.MockDB{
		BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil },
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/notification-webhooks", strings.NewReader(`{"org_id":3,"name":"Ops webhook","url":"https://ops.example.test/hook/token-123?access_key=secret","event_types":["project_budget.threshold"]}`))
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
	if !committed {
		t.Fatal("webhook and audit transaction was not committed")
	}
	if auditAfter["name"] != "Ops webhook" || auditAfter["enabled"] != true {
		t.Fatalf("expected webhook audit after-state, got %#v", auditAfter)
	}
	if auditAfter["url"] != "https://ops.example.test/..." || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("webhook audit must contain only a masked target, got %#v", auditAfter)
	}
}

func TestCreateNotificationWebhook_AuditFailureRollsBackWebhook(t *testing.T) {
	auditErr := errors.New("audit unavailable")
	var committed, rolledBack bool
	tx := &testutil.MockTx{
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 7
				return nil
			}}
		},
		ExecFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "INSERT INTO audit_logs") {
				return pgconn.CommandTag{}, auditErr
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
		CommitFn: func(context.Context) error {
			committed = true
			return nil
		},
		RollbackFn: func(context.Context) error {
			rolledBack = true
			return nil
		},
	}
	db := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/notification-webhooks", strings.NewReader(`{"org_id":3,"name":"Ops webhook","url":"https://ops.example.test/hook","event_types":["project_budget.threshold"]}`))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.CreateNotificationWebhook(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if committed || !rolledBack {
		t.Fatalf("audit failure must roll back webhook creation, committed=%v rolledBack=%v", committed, rolledBack)
	}
}

func TestUpdateNotificationWebhookLocksAndAuditsInTransaction(t *testing.T) {
	var locked, updated, audited, committed bool
	now := time.Now().UTC()
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, query string, args ...any) pgx.Row {
			if !strings.Contains(query, "FROM notification_webhooks") || !strings.Contains(query, "FOR UPDATE") {
				t.Fatalf("webhook update must lock its snapshot in the transaction: %s", query)
			}
			if len(args) != 1 || args[0] != int64(7) {
				t.Fatalf("unexpected locked lookup args: %#v", args)
			}
			locked = true
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 3
				*dest[1].(*int64) = 9
				*dest[2].(*string) = "Old name"
				*dest[3].(*string) = "https://hooks.example.test/old-secret"
				*dest[4].(*json.RawMessage) = json.RawMessage(`["old.event"]`)
				*dest[5].(*bool) = true
				*dest[6].(*bool) = true
				*dest[7].(*sql.NullBool) = sql.NullBool{}
				*dest[8].(*time.Time) = now
				return nil
			}}
		},
		ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(query, "UPDATE notification_webhooks"):
				if !locked {
					t.Fatal("webhook was updated before its snapshot was locked")
				}
				updated = true
				return pgconn.NewCommandTag("UPDATE 1"), nil
			case strings.Contains(query, "INSERT INTO audit_logs"):
				if !updated {
					t.Fatal("webhook audit was written before the mutation")
				}
				if args[0] != int64(3) || args[2] != "notification_webhook.update" || args[10] != int64(9) {
					t.Fatalf("audit must use the target webhook org/project: %#v", args)
				}
				audited = true
				return pgconn.NewCommandTag("INSERT 1"), nil
			default:
				t.Fatalf("unexpected transaction exec: %s", query)
				return pgconn.CommandTag{}, nil
			}
		},
		CommitFn: func(_ context.Context) error {
			if !audited {
				t.Fatal("webhook transaction committed without its audit")
			}
			committed = true
			return nil
		},
	}
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
			if strings.Contains(query, "COUNT(*) FROM projects") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 1
					return nil
				}}
			}
			t.Fatalf("webhook snapshot escaped its transaction: %s", query)
			return &testutil.MockRow{}
		},
		ExecFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			t.Fatalf("webhook mutation or audit escaped its transaction: %s", query)
			return pgconn.CommandTag{}, nil
		},
		BeginFn: func(_ context.Context) (pgx.Tx, error) { return tx, nil },
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPut, "/notification-webhooks/7", strings.NewReader(`{"name":"Updated name"}`))
	req.SetPathValue("id", "7")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(5))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.UpdateNotificationWebhook(rec, req)

	if rec.Code != http.StatusOK || !locked || !updated || !audited || !committed {
		t.Fatalf("expected locked atomic cross-org update: code=%d body=%s locked=%v updated=%v audited=%v committed=%v", rec.Code, rec.Body.String(), locked, updated, audited, committed)
	}
}

func TestBillingAdminCannotAccessNotificationWebhookHandlers(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{
		QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
			t.Fatalf("billing webhook request reached persistence: %s", query)
			return &testutil.MockRow{}
		},
		QueryFn: func(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
			t.Fatalf("billing webhook request reached persistence: %s", query)
			return nil, nil
		},
	})
	tests := []struct {
		name   string
		method string
		target string
		body   string
		invoke func(http.ResponseWriter, *http.Request)
	}{
		{name: "list", method: http.MethodGet, target: "/notification-webhooks", invoke: h.ListNotificationWebhooks},
		{name: "create", method: http.MethodPost, target: "/notification-webhooks", body: `{"org_id":3,"name":"Finance","url":"https://finance.example.test/hook"}`, invoke: h.CreateNotificationWebhook},
		{name: "update", method: http.MethodPut, target: "/notification-webhooks/7", body: `{"name":"Finance"}`, invoke: h.UpdateNotificationWebhook},
		{name: "verify", method: http.MethodPost, target: "/notification-webhooks/7/verify-scope", body: `{"expected_updated_at":"2026-07-16T00:00:00Z"}`, invoke: h.VerifyNotificationWebhookScope},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			req.SetPathValue("id", "7")
			ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
			ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleBillingAdmin)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			tc.invoke(rec, req)

			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Forbidden") {
				t.Fatalf("expected billing webhook access to fail closed, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListNotificationWebhooksMasksStoredTarget(t *testing.T) {
	now := time.Now().UTC()
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return &testutil.MockRows{Rows: []func(dest ...any) error{func(dest ...any) error {
				*dest[0].(*int64) = 7
				*dest[1].(*int64) = 3
				*dest[2].(*int64) = 0
				*dest[3].(*string) = "Ops"
				*dest[4].(*string) = "https://hooks.example.test/customer/token-123?access_key=secret"
				*dest[5].(*json.RawMessage) = json.RawMessage(`[]`)
				*dest[6].(*bool) = true
				*dest[7].(*bool) = true
				*dest[8].(*sql.NullBool) = sql.NullBool{}
				*dest[9].(*time.Time) = now
				*dest[10].(*time.Time) = now
				return nil
			}}}, nil
		},
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/notification-webhooks", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ListNotificationWebhooks(rec, req)

	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "token-123") || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("stored webhook secret leaked in list response: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"url":"https://hooks.example.test/..."`) {
		t.Fatalf("expected masked webhook target, got %s", rec.Body.String())
	}
}

func TestCreateNotificationWebhookRejectsNegativeProjectScope(t *testing.T) {
	db := &testutil.MockDB{QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
		t.Fatal("invalid project scope must be rejected before persistence")
		return &testutil.MockRow{}
	}}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/notification-webhooks", strings.NewReader(`{"org_id":3,"project_id":-1,"name":"Ops","url":"https://ops.example.test/hook"}`))
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.CreateNotificationWebhook(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Invalid project_id") {
		t.Fatalf("expected invalid project scope rejection, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTransitionNotificationEventAcknowledgesAndAudits(t *testing.T) {
	var updateArgs []any
	var auditAction string
	tx := &testutil.MockTx{ExecFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(query, "UPDATE notification_events") {
			updateArgs = append([]any(nil), args...)
			return pgconn.NewCommandTag("UPDATE 1"), nil
		}
		if strings.Contains(query, "INSERT INTO audit_logs") {
			auditAction = args[2].(string)
		}
		return pgconn.NewCommandTag("INSERT 1"), nil
	}}
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, query string, args ...any) pgx.Row {
			if !strings.Contains(query, "FROM notification_events") || args[len(args)-1] != int64(12) {
				t.Fatalf("unexpected event lookup: %s %#v", query, args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 3
				*dest[1].(*int64) = 0
				*dest[2].(*string) = "open"
				return nil
			}}
		},
		BeginFn: func(_ context.Context) (pgx.Tx, error) { return tx, nil },
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/notification-events/12/acknowledge", nil)
	req.SetPathValue("id", "12")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.AcknowledgeNotificationEvent(rec, req)

	if rec.Code != http.StatusOK || len(updateArgs) != 5 || updateArgs[0] != "acknowledged" || updateArgs[3] != "open" || updateArgs[4] != int64(0) {
		t.Fatalf("unexpected lifecycle update: code=%d body=%s args=%#v", rec.Code, rec.Body.String(), updateArgs)
	}
	if auditAction != "notification_event.acknowledged" {
		t.Fatalf("expected lifecycle audit, got %q", auditAction)
	}
}

func TestTransitionNotificationEventRejectsReopeningResolvedEvent(t *testing.T) {
	db := &testutil.MockDB{QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*int64) = 3
			*dest[1].(*int64) = 0
			*dest[2].(*string) = "resolved"
			return nil
		}}
	}}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/notification-events/12/acknowledge", nil)
	req.SetPathValue("id", "12")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.AcknowledgeNotificationEvent(rec, req)

	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Illegal notification event status transition") {
		t.Fatalf("expected illegal transition conflict, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTransitionNotificationEventAuditFailureDoesNotCommit(t *testing.T) {
	committed := false
	tx := &testutil.MockTx{
		ExecFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			if strings.Contains(query, "INSERT INTO audit_logs") {
				return pgconn.CommandTag{}, errors.New("injected audit failure")
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		CommitFn: func(_ context.Context) error {
			committed = true
			return nil
		},
	}
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 3
				*dest[1].(*int64) = 0
				*dest[2].(*string) = "open"
				return nil
			}}
		},
		BeginFn: func(_ context.Context) (pgx.Tx, error) { return tx, nil },
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/notification-events/12/resolve", nil)
	req.SetPathValue("id", "12")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ResolveNotificationEvent(rec, req)

	if rec.Code != http.StatusInternalServerError || committed {
		t.Fatalf("event update must not commit without its audit: code=%d committed=%v body=%s", rec.Code, committed, rec.Body.String())
	}
}

func TestTransitionNotificationEventProjectRoleRequiresExplicitProject(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	req := httptest.NewRequest(http.MethodPost, "/notification-events/12/resolve", nil)
	req.SetPathValue("id", "12")
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, int64(3))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleProjectAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ResolveNotificationEvent(rec, req)

	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "project_id is required") {
		t.Fatalf("expected fail-closed project scope, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookURLPolicyAndMasking(t *testing.T) {
	for _, raw := range []string{
		"http://1.1.1.1/hook",
		"http://127.0.0.1/hook",
		"http://[::1]/hook",
		"http://10.20.30.40/hooks/notify",
		"http://192.168.1.20/hooks/notify",
		"http://100.64.0.1/hooks/notify",
		"http://198.18.0.1/hooks/notify",
		"http://169.254.169.254/latest/meta-data",
		"http://100.100.100.200/latest/meta-data",
		"http://metadata.google.internal/computeMetadata/v1",
		"https://user:password@example.test/hook",
	} {
		if validWebhookURL(raw) {
			t.Fatalf("expected unsafe webhook URL to be rejected: %s", raw)
		}
	}
	if !validWebhookURL("https://1.1.1.1/hooks/notify") {
		t.Fatal("public webhook IPs must remain supported")
	}
	masked := maskWebhookURL("https://hooks.example.test/customer/token-123?access_key=secret")
	if masked != "https://hooks.example.test/..." || strings.Contains(masked, "token-123") || strings.Contains(masked, "secret") {
		t.Fatalf("unexpected masked webhook URL: %q", masked)
	}
	legacyError := sanitizeWebhookDeliveryError(`Post "https://hooks.example.test/token-123?access_key=secret": timeout`)
	if legacyError != "webhook delivery failed" {
		t.Fatalf("legacy delivery errors must not disclose stored target URLs: %q", legacyError)
	}
}

func assertFinancialNotificationTypes(t *testing.T, value any) {
	t.Helper()
	got, ok := value.([]string)
	if !ok {
		t.Fatalf("financial notification scope arg = %T, want []string", value)
	}
	want := []string{"project_budget.threshold", "token_metering.estimated_ratio.high"}
	if len(got) != len(want) {
		t.Fatalf("financial notification scope = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("financial notification scope = %#v, want %#v", got, want)
		}
	}
}
