package admin

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestListProjects_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 10
						*dest[1].(*int64) = 1
						*dest[2].(*string) = "Core"
						*dest[3].(*string) = "core"
						*dest[4].(*string) = "Core project"
						*dest[5].(*string) = "active"
						*dest[6].(*int) = 5000
						*dest[7].(*string) = "monthly"
						*dest[8].(*time.Time) = now
						*dest[9].(*time.Time) = now
						*dest[10].(*string) = "Default Org"
						*dest[11].(*int64) = 2
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/projects?org_id=1", nil)
	rec := httptest.NewRecorder()

	h.ListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["total"].(float64) != 1 {
		t.Fatalf("expected total=1, got %v", resp["total"])
	}
}

func TestListProjects_BillingResponseContainsOnlyBudgetAdministrationFields(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{Rows: []func(dest ...any) error{func(dest ...any) error {
				*dest[0].(*int64) = 10
				*dest[1].(*int64) = 1
				*dest[2].(*string) = "Core"
				*dest[3].(*string) = "core"
				*dest[4].(*string) = "internal description"
				*dest[5].(*string) = "active"
				*dest[6].(*int) = 5000
				*dest[7].(*string) = "monthly"
				*dest[8].(*time.Time) = now
				*dest[9].(*time.Time) = now
				*dest[10].(*string) = "Default Org"
				*dest[11].(*int64) = 2
				return nil
			}}}, nil
		},
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleBillingAdmin)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected one project, got %d", len(resp.Data))
	}
	for _, field := range []string{"id", "org_id", "name", "slug", "status", "budget_cents", "budget_period"} {
		if _, ok := resp.Data[0][field]; !ok {
			t.Fatalf("billing response missing %s", field)
		}
	}
	for _, field := range []string{"description", "org_name", "key_count", "created_at", "updated_at"} {
		if _, ok := resp.Data[0][field]; ok {
			t.Fatalf("billing response must not expose %s", field)
		}
	}
}

func TestCreateProject_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "INSERT INTO projects") {
				t.Fatalf("expected project insert, got %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 42
				return nil
			}}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(`{"org_id":1,"name":"Core Services"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateProject(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["slug"] != "core-services" {
		t.Fatalf("expected generated slug core-services, got %v", resp["slug"])
	}
}

func TestCreateProject_DuplicateSlug(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				return fmt.Errorf("duplicate key value violates unique constraint")
			}}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPost, "/projects", strings.NewReader(`{"org_id":1,"name":"Core"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateProject(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetProject_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 42
				*dest[1].(*int64) = 1
				*dest[2].(*string) = "Core"
				*dest[3].(*string) = "core"
				*dest[4].(*string) = ""
				*dest[5].(*string) = "active"
				*dest[6].(*int) = 0
				*dest[7].(*string) = "monthly"
				*dest[8].(*time.Time) = now
				*dest[9].(*time.Time) = now
				*dest[10].(*string) = "Default Org"
				*dest[11].(*int64) = 1
				return nil
			}}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/projects/42", nil)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.GetProject(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateProject_Success(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPut, "/projects/42", strings.NewReader(`{"status":"disabled","budget_period":"daily"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.UpdateProject(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateProject_NoFields(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPut, "/projects/42", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.UpdateProject(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateProject_ProjectAdminRevokedAfterPreflightCannotCommit(t *testing.T) {
	updateExecuted := false
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM projects") && strings.Contains(sql, "FOR UPDATE"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*string) = "Core"
					*dest[2].(*string) = "core"
					*dest[3].(*string) = ""
					*dest[4].(*string) = "active"
					// budget_cents is sql.NullInt64 and may stay at its zero value.
					*dest[6].(*string) = "monthly"
					return nil
				}}
			case strings.Contains(sql, "FROM project_members"):
				// The membership existed during the preflight but its revoke has
				// committed before the transaction-level authorization read.
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			default:
				t.Fatalf("unexpected transaction query: %s", sql)
				return &testutil.MockRow{}
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			updateExecuted = true
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "FROM project_members") {
				t.Fatalf("unexpected preflight query: %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil },
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPut, "/projects/42", strings.NewReader(`{"name":"Renamed"}`))
	req.SetPathValue("id", "42")
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleProjectAdmin)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(9))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.UpdateProject(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 after membership revoke, got %d: %s", rec.Code, rec.Body.String())
	}
	if updateExecuted {
		t.Fatal("project update executed after transaction-level authorization failed")
	}
}
