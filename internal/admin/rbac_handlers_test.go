package admin

import (
	"context"
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

func TestListLogs_ProjectScopedRoleRequiresProjectID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(10))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListLogs_ProjectScopedRoleWithMembership(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "FROM project_members") {
					*dest[0].(*int) = 1
					return nil
				}
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "project_id =") {
				t.Fatalf("expected project_id filter, got %s", sql)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*int64) = 7
						*dest[2].(*string) = "req-001"
						*dest[3].(*string) = "client-001"
						*dest[4].(*string) = "workflow-001"
						*dest[5].(*string) = "agent-001"
						*dest[6].(*string) = "att_001"
						*dest[7].(*string) = "gpt-4o"
						*dest[8].(*string) = "openai"
						*dest[9].(*string) = "test-channel"
						*dest[10].(*int) = 100
						*dest[11].(*int) = 100
						*dest[12].(*int) = 0
						*dest[13].(*int) = 0
						*dest[14].(*int) = 50
						*dest[15].(*int) = 150
						*dest[16].(*int64) = 2_000_000
						*dest[17].(*int) = 200
						*dest[18].(*int) = 350
						*dest[19].(*int) = 0
						*dest[20].(**int) = nil
						*dest[21].(*string) = ""
						*dest[22].(*string) = "api"
						*dest[23].(*string) = ""
						*dest[24].(*string) = "not_reported"
						*dest[25].(*string) = ""
						*dest[26].(*float64) = 1
						*dest[27].(*float64) = 1
						for i := 28; i <= 33; i++ {
							*dest[i].(*int64) = 0
						}
						*dest[34].(*bool) = false
						*dest[35].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/logs?project_id=7", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(10))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAPIKey_DeveloperRequiresProjectID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPost, "/keys", strings.NewReader(`{"org_id":1,"name":"dev-key"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(10))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListAPIKeys_ProjectIDFilterRequiresProjectAccess(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "SELECT COUNT(*) FROM api_keys ak") {
				if len(args) != 3 || args[0] != int64(1) || args[1] != int64(7) || args[2] != int64(10) {
					t.Fatalf("unexpected key count args: %#v", args)
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 0
					return nil
				}}
			}
			if !strings.Contains(sql, "FROM project_members") {
				t.Fatalf("expected membership check, got %s", sql)
			}
			if len(args) < 3 || args[0] != int64(7) || args[1] != int64(10) || args[2] != int64(1) {
				t.Fatalf("unexpected membership args: %#v", args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "ak.org_id = $1") || !strings.Contains(sql, "ak.project_id = $2") {
				t.Fatalf("expected api key project_id filter, got %s", sql)
			}
			if !strings.Contains(sql, "pm.project_id = ak.project_id") {
				t.Fatalf("expected project-scoped membership filter, got %s", sql)
			}
			if len(args) != 5 || args[0] != int64(1) || args[1] != int64(7) || args[2] != int64(10) || args[3] != 20 || args[4] != 0 {
				t.Fatalf("unexpected query args: %#v", args)
			}
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/keys?project_id=7", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(10))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ListAPIKeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListAPIKeys_ProjectIDFilterRejectsUnauthorizedProject(t *testing.T) {
	queryCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCalled = true
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/keys?project_id=8", nil)
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(10))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ListAPIKeys(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if queryCalled {
		t.Fatal("expected key query to be skipped after forbidden project access")
	}
}

func TestCreateUser_InvalidRole(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"org_id":1,"username":"u","password":"secret-password-123","role":"owner"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateUser_InvalidRole(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPut, "/users/1", strings.NewReader(`{"role":"owner"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
