package admin

import (
	"context"
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

func userMutationRequest(method, target, body string, actorUserID int64) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, actorUserID)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	return req.WithContext(ctx)
}

func guardedUserMutationTx(targetRole, targetStatus string, activePlatformAdmins int, mutationCalled, advisoryCalled *bool) *testutil.MockTx {
	return &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "COUNT(*)") {
					*dest[0].(*int) = activePlatformAdmins
					return nil
				}
				*dest[0].(*int64) = 1
				*dest[1].(*string) = targetRole
				*dest[2].(*string) = targetStatus
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "pg_advisory_xact_lock") {
				*advisoryCalled = true
				return pgconn.NewCommandTag("SELECT 1"), nil
			}
			*mutationCalled = true
			if strings.Contains(sql, "DELETE FROM users") {
				return pgconn.NewCommandTag("DELETE 1"), nil
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
}

func TestUpdateUserRejectsSelfDisable(t *testing.T) {
	mutationCalled, advisoryCalled := false, false
	tx := guardedUserMutationTx(auth.RolePlatformAdmin, "active", 1, &mutationCalled, &advisoryCalled)
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	req := userMutationRequest(http.MethodPut, "/api/v1/users/42", `{"status":"disabled"}`, 42)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if mutationCalled || !advisoryCalled {
		t.Fatal("self-disable must take the user invariant lock without mutating state")
	}
}

func TestUpdateUserRejectsSelfDemotion(t *testing.T) {
	mutationCalled, advisoryCalled := false, false
	tx := guardedUserMutationTx(auth.RolePlatformAdmin, "active", 2, &mutationCalled, &advisoryCalled)
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	req := userMutationRequest(http.MethodPut, "/api/v1/users/42", `{"role":"org_admin"}`, 42)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if mutationCalled || !advisoryCalled {
		t.Fatal("self-demotion must take the user invariant lock without mutating state")
	}
}

func TestDeleteUserRejectsSelfDelete(t *testing.T) {
	mutationCalled, advisoryCalled := false, false
	tx := guardedUserMutationTx(auth.RolePlatformAdmin, "active", 1, &mutationCalled, &advisoryCalled)
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	req := userMutationRequest(http.MethodDelete, "/api/v1/users/42", "", 42)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.DeleteUser(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if mutationCalled || !advisoryCalled {
		t.Fatal("self-delete must take the user invariant lock without mutating state")
	}
}

func TestUpdateUserPreservesLastActivePlatformAdmin(t *testing.T) {
	mutationCalled, advisoryCalled := false, false
	tx := guardedUserMutationTx(auth.RolePlatformAdmin, "active", 1, &mutationCalled, &advisoryCalled)
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	req := userMutationRequest(http.MethodPut, "/api/v1/users/42", `{"role":"org_admin"}`, 99)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !advisoryCalled {
		t.Fatal("expected active-platform-admin changes to acquire the transaction lock")
	}
	if mutationCalled {
		t.Fatal("last active platform admin must not be demoted")
	}
}

func TestDeleteUserPreservesLastActivePlatformAdmin(t *testing.T) {
	mutationCalled, advisoryCalled := false, false
	tx := guardedUserMutationTx(auth.RolePlatformAdmin, "active", 1, &mutationCalled, &advisoryCalled)
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	req := userMutationRequest(http.MethodDelete, "/api/v1/users/42", "", 99)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.DeleteUser(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !advisoryCalled {
		t.Fatal("expected active-platform-admin deletion to acquire the transaction lock")
	}
	if mutationCalled {
		t.Fatal("last active platform admin must not be deleted")
	}
}

func TestUpdateUserAllowsPlatformAdminDemotionWhenAnotherIsActive(t *testing.T) {
	mutationCalled, advisoryCalled := false, false
	committed := false
	tx := guardedUserMutationTx(auth.RolePlatformAdmin, "active", 2, &mutationCalled, &advisoryCalled)
	tx.CommitFn = func(context.Context) error {
		committed = true
		return nil
	}
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	req := userMutationRequest(http.MethodPut, "/api/v1/users/42", `{"role":"org_admin"}`, 99)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !advisoryCalled || !mutationCalled || !committed {
		t.Fatalf("expected locked committed mutation, lock=%v mutation=%v commit=%v", advisoryCalled, mutationCalled, committed)
	}
}

func TestUpdateUserVersionsPrincipalAndRevokesLiveSessions(t *testing.T) {
	var userVersioned, sessionsRevoked, auditWritten, committed bool
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if !strings.Contains(sql, "FROM users") {
					return pgx.ErrNoRows
				}
				*dest[0].(*int64) = 1
				*dest[1].(*string) = auth.RoleOrgViewer
				*dest[2].(*string) = "active"
				return nil
			}}
		},
		QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
		ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			switch {
			case strings.Contains(sql, "pg_advisory_xact_lock"):
				return pgconn.NewCommandTag("SELECT 1"), nil
			case strings.Contains(sql, "UPDATE users SET"):
				userVersioned = strings.Contains(sql, "auth_epoch = auth_epoch + 1")
				return pgconn.NewCommandTag("UPDATE 1"), nil
			case strings.Contains(sql, "UPDATE admin_sessions"):
				sessionsRevoked = strings.Contains(sql, "revoked_at = COALESCE")
				return pgconn.NewCommandTag("UPDATE 2"), nil
			case strings.Contains(sql, "INSERT INTO audit_logs"):
				auditWritten = true
				return pgconn.NewCommandTag("INSERT 1"), nil
			default:
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
		},
		CommitFn: func(context.Context) error {
			committed = true
			return nil
		},
	}
	h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
	req := userMutationRequest(http.MethodPut, "/api/v1/users/42", `{"status":"disabled"}`, 99)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !userVersioned || !sessionsRevoked || !auditWritten || !committed {
		t.Fatalf("versioned=%v sessions=%v audit=%v committed=%v",
			userVersioned, sessionsRevoked, auditWritten, committed)
	}
}
