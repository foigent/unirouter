package admin

import (
	"context"
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

func p6f16Request(method, target, body, role string, orgID, userID int64) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, role)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserID, userID)
	return req.WithContext(ctx)
}

func TestP6F16ListAPIKeysScopesOrgAdmin(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "ak.org_id = $1") {
				t.Fatalf("expected actor organization predicate in count, got %s", sql)
			}
			if len(args) != 1 || args[0] != int64(7) {
				t.Fatalf("expected actor org argument 7 in count, got %#v", args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "ak.org_id = $1") {
				t.Fatalf("expected actor organization predicate, got %s", sql)
			}
			if len(args) != 3 || args[0] != int64(7) || args[1] != 20 || args[2] != 0 {
				t.Fatalf("expected actor org and pagination arguments, got %#v", args)
			}
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodGet, "/api/v1/keys", "", auth.RoleOrgAdmin, 7, 70)
	rec := httptest.NewRecorder()

	h.ListAPIKeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestP6F16ListUsersScopesOrgAdmin(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "org_id = $1") {
				t.Fatalf("expected scoped user count, got %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "u.org_id = $1") {
				t.Fatalf("expected scoped user rows, got %s", sql)
			}
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodGet, "/api/v1/users", "", auth.RoleOrgAdmin, 7, 70)
	rec := httptest.NewRecorder()

	h.ListUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestP6F16CreateProjectRejectsForeignOrgBeforeInsert(t *testing.T) {
	queryCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			queryCalled = true
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 42
				return nil
			}}
		},
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPost, "/api/v1/projects", `{"org_id":8,"name":"Foreign"}`, auth.RoleOrgAdmin, 7, 70)
	rec := httptest.NewRecorder()

	h.CreateProject(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if queryCalled {
		t.Fatal("foreign create must be rejected before database work")
	}
}

func TestP6F16OrgAdminCannotCreatePlatformAdmin(t *testing.T) {
	queryCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			queryCalled = true
			return &testutil.MockRow{}
		},
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPost, "/api/v1/users", `{"org_id":7,"username":"elevated","password":"secret-password-123","role":"platform_admin"}`, auth.RoleOrgAdmin, 7, 70)
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if queryCalled {
		t.Fatal("role escalation must be rejected before database work")
	}
}

func TestP6F16UpdateAPIKeyMasksForeignOrgBeforeMutation(t *testing.T) {
	execCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "api_keys.org_id = $2") || len(args) < 2 || args[1] != int64(7) {
				t.Fatalf("foreign-key lookup must be actor scoped: sql=%s args=%#v", sql, args)
			}
			return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "pg_advisory_xact_lock") {
				return pgconn.NewCommandTag("SELECT 1"), nil
			}
			execCalled = true
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPut, "/api/v1/keys/9", `{"name":"changed"}`, auth.RoleOrgAdmin, 7, 70)
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected stable 404, got %d: %s", rec.Code, rec.Body.String())
	}
	if execCalled {
		t.Fatal("foreign key update must have no mutation")
	}
}

func TestP6F16DeleteUserRejectsForeignOrgBeforeMutation(t *testing.T) {
	execCalled := false
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 8
				*dest[1].(*string) = auth.RoleOrgViewer
				*dest[2].(*string) = "active"
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "pg_advisory_xact_lock") {
				return pgconn.NewCommandTag("SELECT 1"), nil
			}
			execCalled = true
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}
	db := &testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil }}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodDelete, "/api/v1/users/9", "", auth.RoleOrgAdmin, 7, 70)
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.DeleteUser(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if execCalled {
		t.Fatal("foreign user delete must have no mutation")
	}
}

func TestP6F16GetProjectRejectsForeignOrg(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		if strings.Contains(sql, "COUNT(*)") && strings.Contains(sql, "FROM projects") {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		}
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*int64) = 42
			*dest[1].(*int64) = 8
			*dest[2].(*string) = "Foreign"
			*dest[3].(*string) = "foreign"
			*dest[4].(*string) = ""
			*dest[5].(*string) = "active"
			*dest[6].(*int) = 0
			*dest[7].(*string) = "monthly"
			*dest[8].(*time.Time) = now
			*dest[9].(*time.Time) = now
			*dest[10].(*string) = "Org 8"
			*dest[11].(*int64) = 0
			return nil
		}}
	}}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodGet, "/api/v1/projects/42", "", auth.RoleOrgAdmin, 7, 70)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.GetProject(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestP6F16ProjectMembershipRequiresActorOrg(t *testing.T) {
	db := &testutil.MockDB{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		if !strings.Contains(sql, "JOIN projects") || !strings.Contains(sql, "JOIN users") ||
			!strings.Contains(sql, "u.org_id = p.org_id") || !strings.Contains(sql, "p.org_id") {
			t.Fatalf("expected project membership to bind actor user and project org, got %s", sql)
		}
		if len(args) != 4 || args[2] != int64(7) {
			t.Fatalf("expected actor org argument 7, got %#v", args)
		}
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*int) = 1
			return nil
		}}
	}}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodGet, "/api/v1/projects/42", "", auth.RoleDeveloper, 7, 70)

	if !h.canReadProject(req.Context(), 42) {
		t.Fatal("expected same-org membership to be readable")
	}
}

func TestP6F16UpdateUserFinalPredicateProtectsPlatformRoles(t *testing.T) {
	updateCalled := false
	sessionRevokeCalled := false
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 7
				*dest[1].(*string) = auth.RoleOrgViewer
				*dest[2].(*string) = auth.RoleOrgViewer
				*dest[3].(*string) = "active"
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "pg_advisory_xact_lock") {
				return pgconn.NewCommandTag("SELECT 1"), nil
			}
			if strings.Contains(sql, "DELETE FROM project_members") {
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				return pgconn.NewCommandTag("INSERT 0 1"), nil
			}
			if strings.Contains(sql, "UPDATE admin_sessions") {
				sessionRevokeCalled = true
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}
			if !strings.Contains(sql, "organization_role <> 'platform_admin'") {
				t.Fatalf("expected final update predicate to close platform-role race, got %s", sql)
			}
			if !strings.Contains(sql, "organization_role = $1") || !strings.Contains(sql, "role = $2") {
				t.Fatalf("current organization-role writer must update both canonical and compatibility axes, got %s", sql)
			}
			updateCalled = true
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	db := &testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil }}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPut, "/api/v1/users/9", `{"organization_role":"billing_admin"}`, auth.RoleOrgAdmin, 7, 70)
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)
	if !updateCalled {
		t.Fatal("expected guarded user update")
	}
	if !sessionRevokeCalled {
		t.Fatal("expected live admin sessions to be revoked with the principal change")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestP6F16ProjectModelSetReplaceRollsBackAfterPartialWriteError(t *testing.T) {
	rolledBack := false
	committed := false
	insertCount := 0
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "FROM projects") {
					*dest[0].(*int64) = 7
					return nil
				}
				*dest[0].(*int64) = args[0].(int64)
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO project_model_sets") {
				insertCount++
				if insertCount == 2 {
					return pgconn.CommandTag{}, errors.New("injected second insert failure")
				}
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		CommitFn: func(ctx context.Context) error {
			committed = true
			return nil
		},
		RollbackFn: func(ctx context.Context) error {
			rolledBack = true
			return nil
		},
	}
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil },
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPut, "/api/v1/projects/42/model-sets", `{"model_set_ids":[1,2]}`, auth.RoleOrgAdmin, 7, 70)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.ReplaceProjectModelSets(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !rolledBack || committed {
		t.Fatalf("expected rollback without commit, rollback=%v commit=%v", rolledBack, committed)
	}
}

func TestP6F16PlatformAdminCrossTenantException(t *testing.T) {
	inserted := false
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			inserted = true
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 42
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO audit_logs") {
				t.Fatalf("unexpected exec: %s", sql)
			}
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	db := &testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) { return tx, nil }}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPost, "/api/v1/projects", `{"org_id":8,"name":"Foreign"}`, auth.RolePlatformAdmin, 7, 70)
	rec := httptest.NewRecorder()

	h.CreateProject(rec, req)

	if rec.Code != http.StatusCreated {
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		t.Fatalf("expected 201 for platform admin, got %d: %#v", rec.Code, body)
	}
	if !inserted {
		t.Fatal("expected platform-admin cross-tenant insert")
	}
}
