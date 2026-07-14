package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

// --- Mock infrastructure for admin handler tests ---

// --- Helper to create handler with mock DB ---
func newTestHandler(db *testutil.MockDB) *Handler {
	return NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
}

func pricingActorRequest(req *http.Request, orgID int64, role string) *http.Request {
	ctx := context.WithValue(req.Context(), middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, role)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(7))
	return req.WithContext(ctx)
}

func platformPricingRequest(method, target string, body io.Reader) *http.Request {
	return pricingActorRequest(httptest.NewRequest(method, target, body), 1, auth.RolePlatformAdmin)
}

func platformGlobalControlRequest(req *http.Request) *http.Request {
	return pricingActorRequest(req, 1, auth.RolePlatformAdmin)
}

func globalControlTestDB(db *testutil.MockDB) *testutil.MockDB {
	db.BeginFn = func(ctx context.Context) (pgx.Tx, error) {
		return &testutil.MockTx{
			QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "RETURNING value::text") {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*string) = fmt.Sprint(args[1])
						return nil
					}}
				}
				if strings.Contains(sql, "RETURNING updated_at") {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*time.Time) = time.Now().UTC()
						return nil
					}}
				}
				if db.QueryRowFn != nil {
					return db.QueryRowFn(ctx, sql, args...)
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			},
			ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if db.ExecFn != nil {
					return db.ExecFn(ctx, sql, args...)
				}
				return pgconn.NewCommandTag("UPDATE 1"), nil
			},
		}, nil
	}
	return db
}

func modelPriceMockRow(id int64, modelName string, inputPrice, outputPrice float64) pgx.Row {
	return modelPriceExactMockRow(id, modelName,
		strconv.FormatFloat(inputPrice, 'f', -1, 64),
		strconv.FormatFloat(outputPrice, 'f', -1, 64), "1", "1")
}

func modelPriceExactMockRow(id int64, modelName, inputPrice, outputPrice, cacheReadRatio, cacheCreationRatio string) pgx.Row {
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		if len(dest) == 1 {
			if overflow, ok := dest[0].(*bool); ok {
				*overflow = false
				return nil
			}
		}
		*dest[0].(*int64) = id
		*dest[1].(*string) = modelName
		*dest[2].(*string) = inputPrice
		*dest[3].(*string) = outputPrice
		if len(dest) > 4 {
			*dest[4].(*string) = cacheReadRatio
			*dest[5].(*string) = cacheCreationRatio
		}
		return nil
	}}
}

func orgModelPriceMockRow(id, orgID int64, modelName string, multiplier float64) pgx.Row {
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		if len(dest) == 1 {
			if overflow, ok := dest[0].(*bool); ok {
				*overflow = false
				return nil
			}
		}
		*dest[0].(*int64) = id
		*dest[1].(*int64) = orgID
		*dest[2].(*string) = modelName
		*dest[3].(*string) = strconv.FormatFloat(multiplier, 'f', -1, 64)
		return nil
	}}
}

func effectivePriceOverflowMockRow(overflow bool) pgx.Row {
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		*dest[0].(*bool) = overflow
		return nil
	}}
}

// ===================================================================
// Login tests
// ===================================================================

func TestLogin_Success(t *testing.T) {
	// Generate a valid bcrypt hash for "password123"
	// We'll use the real bcrypt from the handler, so we need a pre-hashed password
	// bcrypt hash of "password123" with cost 10
	hash := "$2a$10$FQbk3L3IVXe.UiYojf7YVeq1nbfb4oI.igx8Jed8fxF6slCek.hLG"

	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1         // id
					*dest[1].(*int64) = 1         // org_id
					*dest[2].(*string) = hash     // password_hash
					*dest[3].(*string) = "admin"  // role
					*dest[4].(*string) = "active" // status
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"username":"admin","password":"password123"}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["token"]; !ok {
		t.Fatal("expected 'token' in response")
	}
	if resp["role"] != "admin" {
		t.Fatalf("expected role=admin, got %v", resp["role"])
	}
}

func TestLogin_WritesAuditWithActor(t *testing.T) {
	hash := "$2a$10$FQbk3L3IVXe.UiYojf7YVeq1nbfb4oI.igx8Jed8fxF6slCek.hLG"
	var auditOrgID, auditUserID int64
	var auditAction string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 7
					*dest[1].(*int64) = 3
					*dest[2].(*string) = hash
					*dest[3].(*string) = "admin"
					*dest[4].(*string) = "active"
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if !strings.Contains(sql, "INSERT INTO audit_logs") {
				return pgconn.CommandTag{}, nil
			}
			auditOrgID = args[0].(int64)
			auditUserID = args[1].(int64)
			auditAction = args[2].(string)
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"admin","password":"password123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if auditOrgID != 3 || auditUserID != 7 || auditAction != "login" {
		t.Fatalf("expected login audit org=3 user=7 action=login, got org=%d user=%d action=%s", auditOrgID, auditUserID, auditAction)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	hash := "$2a$10$FQbk3L3IVXe.UiYojf7YVeq1nbfb4oI.igx8Jed8fxF6slCek.hLG"
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*string) = hash
					*dest[3].(*string) = "admin"
					*dest[4].(*string) = "active"
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"username":"admin","password":"wrongpassword"}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestLogin_DefaultAdminPasswordBlocked(t *testing.T) {
	t.Setenv("UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN", "")
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123456"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*string) = string(hash)
					*dest[3].(*string) = "admin"
					*dest[4].(*string) = "active"
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"admin","password":"admin123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for default admin password, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLogin_DefaultAdminPasswordAllowedForExplicitDevOverride(t *testing.T) {
	t.Setenv("UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN", "true")
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123456"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*string) = string(hash)
					*dest[3].(*string) = "admin"
					*dest[4].(*string) = "active"
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"username":"admin","password":"admin123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with explicit override, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("no rows")
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"username":"nonexistent","password":"password123"}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestChangePassword_Success(t *testing.T) {
	oldHash := "$2a$10$FQbk3L3IVXe.UiYojf7YVeq1nbfb4oI.igx8Jed8fxF6slCek.hLG"
	var updatedHash string
	var updatedUserID int64
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if args[0] != int64(42) {
				t.Fatalf("expected user id 42, got %v", args[0])
			}
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*string) = oldHash
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				return pgconn.NewCommandTag("INSERT 1"), nil
			}
			updatedHash = args[0].(string)
			updatedUserID = args[1].(int64)
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)

	body := `{"old_password":"password123","new_password":"newpass123"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/password", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(42))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if updatedUserID != 42 {
		t.Fatalf("expected updated user id 42, got %d", updatedUserID)
	}
	if updatedHash == "" || updatedHash == oldHash {
		t.Fatal("expected a new password hash")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(updatedHash), []byte("newpass123")); err != nil {
		t.Fatalf("updated hash does not match new password: %v", err)
	}
}

func TestChangePassword_WrongOldPassword(t *testing.T) {
	oldHash := "$2a$10$FQbk3L3IVXe.UiYojf7YVeq1nbfb4oI.igx8Jed8fxF6slCek.hLG"
	execCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*string) = oldHash
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execCalled = true
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"old_password":"wrongpass","new_password":"newpass123"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/password", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(42))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if execCalled {
		t.Fatal("password update should not run when old password is wrong")
	}
}

func TestChangePassword_Unauthenticated(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"old_password":"password123","new_password":"newpass123"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/password", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ChangePassword(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestLogin_DisabledAccount(t *testing.T) {
	hash := "$2a$10$FQbk3L3IVXe.UiYojf7YVeq1nbfb4oI.igx8Jed8fxF6slCek.hLG"
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*string) = hash
					*dest[3].(*string) = "admin"
					*dest[4].(*string) = "disabled"
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"username":"admin","password":"password123"}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestLogin_EmptyBody(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	body := `{"username":"","password":""}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLogin_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLogin_WrongMethod(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	req := httptest.NewRequest("GET", "/auth/login", nil)
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// ===================================================================
// GetWallet tests
// ===================================================================

func TestGetWallet_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1           // org_id
						*dest[1].(*int64) = 100_000_000 // balance_micros
						*dest[2].(*int64) = 50_000_000  // granted_balance_micros
						*dest[3].(*int64) = 0           // frozen_micros
						*dest[4].(*int64) = 25_000_000  // total_spent_micros
						*dest[5].(*string) = "USD"      // currency
						*dest[6].(*bool) = true         // auto_freeze
						*dest[7].(*int64) = 1000        // low_balance_threshold_cents
						*dest[8].(*time.Time) = now     // created_at
						*dest[9].(*time.Time) = now     // updated_at
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet", nil)
	req = withTestActor(req, 1, 1, auth.RoleViewer)
	rec := httptest.NewRecorder()

	h.GetWallet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["balance_micros"] != "100000000" {
		t.Fatalf("expected balance_micros=100000000, got %v", resp["balance_micros"])
	}
	if resp["currency"] != "USD" {
		t.Fatalf("expected currency=USD, got %v", resp["currency"])
	}
	if resp["granted_balance_micros"] != "50000000" {
		t.Fatalf("expected granted_balance_micros=50000000, got %v", resp["granted_balance_micros"])
	}
	if resp["threshold_micros"] != "10000000" {
		t.Fatalf("expected threshold_micros=10000000, got %v", resp["threshold_micros"])
	}
	if resp["is_admin"].(bool) {
		t.Fatalf("expected is_admin=false without admin role context")
	}
}

func TestGetWallet_AdminRole(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*int64) = 100_000_000
						*dest[2].(*int64) = 50_000_000
						*dest[3].(*int64) = 0
						*dest[4].(*int64) = 25_000_000
						*dest[5].(*string) = "USD"
						*dest[6].(*bool) = true
						*dest[7].(*int64) = 1000
						*dest[8].(*time.Time) = now
						*dest[9].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet", nil)
	req = withTestActor(req, 1, 1, auth.RolePlatformAdmin)
	rec := httptest.NewRecorder()

	h.GetWallet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp["is_admin"].(bool) {
		t.Fatal("expected is_admin=true for admin role")
	}
}

func TestGetWallet_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil // empty rows
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.GetWallet(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetWallet_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.GetWallet(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// TopUp tests
// ===================================================================

func TestTopUp_Success(t *testing.T) {
	txCommitted := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*int64) = 150_000_000
						return nil
					}}
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, nil
				},
				CommitFn: func(ctx context.Context) error {
					txCommitted = true
					return nil
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil // audit log
		},
	}
	h := newTestHandler(db)

	body := `{"amount_cents":5000,"description":"Test top-up","client_request_id":"topup-success"}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !txCommitted {
		t.Fatal("expected transaction to be committed")
	}
}

func TestTopUp_NegativeAmount(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"amount_cents":-100}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTopUp_ZeroAmount(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"amount_cents":0}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTopUp_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTopUp_Idempotency(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					*dest[1].(*int64) = 50_000_000
					*dest[2].(*int64) = 150_000_000
					*dest[3].(*string) = "Replay top-up"
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"amount_cents":5000,"description":"Replay top-up","client_request_id":"req-123"}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["idempotent_replay"] != true || resp["balance_micros"] != "150000000" {
		t.Fatalf("expected replayed balance, got %v", resp)
	}
}

func TestTopUp_IdempotencyReadErrorDoesNotBeginFinancialWrite(t *testing.T) {
	beginCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				return fmt.Errorf("database unavailable")
			}}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			beginCalled = true
			return nil, fmt.Errorf("unexpected transaction")
		},
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/wallet/topup", strings.NewReader(`{"amount_cents":5000,"description":"DB error","client_request_id":"req-db-error"}`))
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if beginCalled {
		t.Fatal("idempotency read failure must not begin a financial transaction")
	}
}

func TestTopUp_WalletNotFound(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"amount_cents":5000,"description":"Missing wallet","client_request_id":"missing-wallet"}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestTopUp_TxBeginFail(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return nil, fmt.Errorf("connection pool exhausted")
		},
	}
	h := newTestHandler(db)

	body := `{"amount_cents":5000,"description":"Begin failure","client_request_id":"begin-failure"}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestTopUp_TxExecFail_WalletUpdate(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error { return fmt.Errorf("deadlock") }}
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "UPDATE wallets") {
						return pgconn.CommandTag{}, fmt.Errorf("deadlock")
					}
					return pgconn.CommandTag{}, nil
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"amount_cents":5000,"description":"Update failure","client_request_id":"update-failure"}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestTopUp_TxCommitFail(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*int64) = 50_000_000
						return nil
					}}
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, nil
				},
				CommitFn: func(ctx context.Context) error {
					return fmt.Errorf("commit failed")
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"amount_cents":5000,"description":"Commit failure","client_request_id":"commit-failure"}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// BalanceAdjust tests
// ===================================================================

func TestBalanceAdjust_Success(t *testing.T) {
	var auditArgs []any
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*int64) = 50_000_000
						return nil
					}}
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, nil
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditArgs = append([]any(nil), args...)
			}
			return pgconn.CommandTag{}, nil // audit log
		},
	}
	h := newTestHandler(db)

	body := `{"delta_cents":5000,"description":"Manual adjustment","client_request_id":"adjust-success"}`
	req := httptest.NewRequest("POST", "/wallet/adjust", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["previous_balance_micros"] != "50000000" {
		t.Fatalf("expected previous_balance_micros=50000000, got %v", resp["previous_balance_micros"])
	}
	if resp["balance_micros"] != "100000000" {
		t.Fatalf("expected balance_micros=100000000, got %v", resp["balance_micros"])
	}
	if resp["delta_micros"] != "50000000" {
		t.Fatalf("expected delta_micros=50000000, got %v", resp["delta_micros"])
	}
	if len(auditArgs) != 14 {
		t.Fatalf("expected 14 audit args, got %#v", auditArgs)
	}
	if !strings.Contains(auditArgs[11].(string), `"balance_micros":"50000000"`) || !strings.Contains(auditArgs[12].(string), `"balance_micros":"100000000"`) {
		t.Fatalf("expected wallet before/after balance audit, got before=%v after=%v", auditArgs[11], auditArgs[12])
	}
}

func TestBalanceAdjust_RejectsLegacyTargetBalance(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"target_balance":-100}`
	req := httptest.NewRequest("POST", "/wallet/adjust", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestBalanceAdjust_WrongMethod(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("GET", "/wallet/adjust", nil)
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestBalanceAdjust_WalletNotFound(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"delta_cents":10000,"description":"Missing wallet","client_request_id":"adjust-missing-wallet"}`
	req := httptest.NewRequest("POST", "/wallet/adjust", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestBalanceAdjust_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("POST", "/wallet/adjust", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// ===================================================================
// ListChannels tests
// ===================================================================

func TestListChannels_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1                         // id
						*dest[1].(*string) = "openai"                 // name
						*dest[2].(*string) = "openai"                 // provider
						*dest[3].(*string) = "https://api.openai.com" // base_url
						*dest[4].(*string) = "gpt-4o"                 // models
						*dest[5].(*string) = "openai_compatible"      // adapter_type
						*dest[6].(*json.RawMessage) = json.RawMessage(`{}`)
						*dest[7].(*int) = 100           // priority
						*dest[8].(*int) = 5             // degradation_threshold
						*dest[9].(*string) = "active"   // status
						*dest[10].(*string) = "healthy" // circuit_state
						*dest[11].(*int) = 0            // fail_count
						*dest[12].(*time.Time) = now    // created_at
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/channels", nil)
	rec := httptest.NewRecorder()

	h.ListChannels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	total := resp["total"].(float64)
	if total != 1 {
		t.Fatalf("expected total=1, got %v", total)
	}
	data := resp["data"].([]interface{})
	ch := data[0].(map[string]interface{})
	if ch["name"] != "openai" {
		t.Fatalf("expected name=openai, got %v", ch["name"])
	}
}

func TestListChannels_Empty(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/channels", nil)
	rec := httptest.NewRecorder()

	h.ListChannels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	total := resp["total"].(float64)
	if total != 0 {
		t.Fatalf("expected total=0, got %v", total)
	}
}

func TestListChannels_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/channels", nil)
	rec := httptest.NewRecorder()

	h.ListChannels(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// CreateUser tests
// ===================================================================

func TestCreateUser_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42 // new user id
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"username":"newuser","password":"secret123","role":"org_viewer","org_id":1}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["id"].(float64) != 42 {
		t.Fatalf("expected id=42, got %v", resp["id"])
	}
}

func TestCreateUser_EmptyUsername(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"username":"","password":"secret123"}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateUser_EmptyPassword(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"username":"testuser","password":""}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("duplicate key value violates unique constraint")
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"username":"existinguser","password":"secret123","org_id":1}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}

func TestCreateUser_DefaultRole(t *testing.T) {
	var insertedRole string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "INSERT") {
				// Capture the role argument (4th arg)
				if len(args) >= 4 {
					if r, ok := args[3].(string); ok {
						insertedRole = r
					}
				}
			}
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"username":"testuser","password":"secret123","org_id":1}`
	req := httptest.NewRequest("POST", "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	if insertedRole != "org_viewer" {
		t.Fatalf("expected default role org_viewer, got %s", insertedRole)
	}
}

// ===================================================================
// DeleteChannel tests
// ===================================================================

func TestDeleteChannel_Success(t *testing.T) {
	execCalled := false
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execCalled = true
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/channels/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !execCalled {
		t.Fatal("expected exec to be called")
	}
}

func TestDeleteChannel_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("DELETE", "/channels/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.DeleteChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteChannel_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("foreign key violation")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/channels/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteChannel(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// recordAudit tests
// ===================================================================

func TestRecordAudit_Success(t *testing.T) {
	execCalled := false
	var capturedSQL string
	var capturedArgs []any
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execCalled = true
			capturedSQL = sql
			capturedArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	// recordAudit should not panic even on success
	ctx := context.WithValue(context.Background(), middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleDeveloper)
	h.recordAudit(ctx, "test.action", "resource", "1", map[string]interface{}{
		"key":    "value",
		"before": map[string]interface{}{"status": "old"},
		"after":  map[string]interface{}{"status": "new"},
	}, "127.0.0.1")

	if !execCalled {
		t.Fatal("expected exec to be called")
	}
	if !strings.Contains(capturedSQL, "before_state") || !strings.Contains(capturedSQL, "after_state") {
		t.Fatalf("expected audit insert to include before_state/after_state, got %s", capturedSQL)
	}
	if capturedArgs[1] != int64(7) {
		t.Fatalf("expected actor user id 7, got %v", capturedArgs[1])
	}
	if capturedArgs[9] != "developer_console" {
		t.Fatalf("expected developer_console source, got %v", capturedArgs[9])
	}
	if !strings.Contains(capturedArgs[11].(string), "old") || !strings.Contains(capturedArgs[12].(string), "new") {
		t.Fatalf("expected before/after JSON, got before=%v after=%v", capturedArgs[11], capturedArgs[12])
	}
}

func TestRecordAudit_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	// recordAudit should not panic on DB error (it just logs)
	h.recordAudit(context.Background(), "test.action", "resource", "1", nil, "127.0.0.1")
}

// ===================================================================
// CleanupOldData tests
// ===================================================================

func TestCleanupOldData_Success(t *testing.T) {
	execCount := 0
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execCount++
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	err := h.CleanupOldData(context.Background(), 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Includes call/audit/transaction/trace retention, freeze-idempotency
	// retention and request-idempotency receipt expiry. Grant expiry now uses a
	// bounded transaction through Begin rather than independent Exec calls.
	if execCount < 6 {
		t.Fatalf("expected at least 6 exec calls, got %d", execCount)
	}
}

func TestCleanupOldData_SelectsStaleFrozenBeforeDeletingIdempotency(t *testing.T) {
	sawStaleSelection := false
	sawSafeDelete := false
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "FROM freeze_idempotency") && strings.Contains(sql, "status = 'frozen'") && strings.Contains(sql, "INTERVAL '24 hours'") {
				sawStaleSelection = true
			}
			return &testutil.MockRows{}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "DELETE FROM freeze_idempotency") {
				if !strings.Contains(sql, "status NOT IN ('frozen', 'settlement_pending')") {
					t.Fatalf("freeze_idempotency cleanup must retain active and billable pending rows: %s", sql)
				}
				sawSafeDelete = true
			}
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	if err := h.CleanupOldData(context.Background(), 30); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawStaleSelection {
		t.Fatal("expected cleanup to select stale frozen requests for state-machine cancellation")
	}
	if !sawSafeDelete {
		t.Fatal("expected cleanup to delete only non-frozen idempotency rows")
	}
}

func TestCleanupOldData_DefaultRetention(t *testing.T) {
	var retentionArg int
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if len(args) > 0 {
				if v, ok := args[0].(int); ok && retentionArg == 0 {
					retentionArg = v
				}
			}
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	err := h.CleanupOldData(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default retention is 30 days
	if retentionArg != 30 {
		t.Fatalf("expected default retention 30, got %d", retentionArg)
	}
}

func TestCleanupOldDataWithTraceRetention(t *testing.T) {
	var traceRetentionArg int
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "DELETE FROM request_trace_events") {
				if len(args) != 1 {
					t.Fatalf("expected trace retention arg, got %#v", args)
				}
				traceRetentionArg = args[0].(int)
			}
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	err := h.CleanupOldDataWithTraceRetention(context.Background(), 30, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if traceRetentionArg != 7 {
		t.Fatalf("expected trace retention 7, got %d", traceRetentionArg)
	}
}

func TestCleanupOldDataWithTraceRetention_SkipsTraceCleanupWhenDisabled(t *testing.T) {
	sawTraceDelete := false
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "DELETE FROM request_trace_events") {
				sawTraceDelete = true
			}
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	err := h.CleanupOldDataWithTraceRetention(context.Background(), 30, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sawTraceDelete {
		t.Fatal("expected trace cleanup to be skipped when retention is 0")
	}
}

func TestCleanupOldData_ExecError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("disk full")
		},
	}
	h := newTestHandler(db)

	err := h.CleanupOldData(context.Background(), 30)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ===================================================================
// ListAPIKeys tests
// ===================================================================

func TestListAPIKeys_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1            // id
						*dest[1].(*int64) = 1            // org_id
						*dest[2].(*int64) = 7            // project_id
						*dest[3].(*string) = "test-key"  // name
						*dest[4].(*string) = "sk-ab12"   // key_prefix
						*dest[5].(*string) = "active"    // status
						*dest[6].(*int) = 60             // rpm_limit
						*dest[7].(*int) = 10             // qps_limit
						*dest[8].(*int) = 0              // budget_limit
						*dest[9].(*string) = ""          // allowed_ips
						*dest[10].(*string) = ""         // allowed_models
						*dest[11].(*time.Time) = now     // created_at
						*dest[12].(*string) = "Default"  // org_name
						*dest[13].(*string) = "Core"     // project_name
						*dest[14].(*int64) = 42          // owner_user_id
						*dest[15].(*string) = "personal" // key_type
						*dest[16].(*string) = "alice"    // owner_username
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/keys", nil)
	rec := httptest.NewRecorder()

	h.ListAPIKeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	total := resp["total"].(float64)
	if total != 1 {
		t.Fatalf("expected total=1, got %v", total)
	}
	data := resp["data"].([]interface{})
	first := data[0].(map[string]interface{})
	if first["key_type"] != "personal" || first["owner_username"] != "alice" {
		t.Fatalf("expected owner fields in list response, got %#v", first)
	}
}

func TestListAPIKeys_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/keys", nil)
	rec := httptest.NewRecorder()

	h.ListAPIKeys(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestListAPIKeys_ScanError(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "COALESCE(ak.budget_limit, 0)") {
				t.Fatalf("expected budget_limit to be null-safe, got query: %s", sql)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						return fmt.Errorf("scan error")
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/keys", nil)
	rec := httptest.NewRecorder()

	h.ListAPIKeys(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// CreateAPIKey tests
// ===================================================================

func TestCreateAPIKey_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42 // new key id
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"name":"test-key","rpm_limit":120,"qps_limit":20}`
	req := httptest.NewRequest("POST", "/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAPIKey_AuditIncludesProjectAndAfterState(t *testing.T) {
	var auditArgs []any
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					if strings.Contains(sql, "FROM projects") || strings.Contains(sql, "INSERT INTO projects") {
						*dest[0].(*int64) = 9
						return nil
					}
					*dest[0].(*int64) = 42
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditArgs = append([]any(nil), args...)
			}
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"project_id":9,"name":"project-key","rpm_limit":120,"qps_limit":20,"allowed_models":"company-chat"}`
	req := httptest.NewRequest("POST", "/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "audit-test")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(auditArgs) != 14 {
		t.Fatalf("expected 14 audit args, got %#v", auditArgs)
	}
	if auditArgs[10] != int64(9) {
		t.Fatalf("expected project_id audit arg 9, got %v", auditArgs[10])
	}
	if !strings.Contains(auditArgs[12].(string), "project-key") || !strings.Contains(auditArgs[12].(string), "company-chat") {
		t.Fatalf("expected after state to include key fields, got %v", auditArgs[12])
	}
}

func TestCreateAPIKey_PersonalOwnerFields(t *testing.T) {
	var insertArgs []any
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					switch {
					case strings.Contains(sql, "SELECT id FROM projects"):
						*dest[0].(*int64) = 9
					case strings.Contains(sql, "FROM users"):
						*dest[0].(*int) = 1
					case strings.Contains(sql, "FROM project_members"):
						*dest[0].(*int) = 1
					case strings.Contains(sql, "INSERT INTO api_keys"):
						insertArgs = append([]any(nil), args...)
						*dest[0].(*int64) = 42
					default:
						*dest[0].(*int64) = 42
					}
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"project_id":9,"owner_user_id":42,"key_type":"personal","name":"alice-key"}`
	req := httptest.NewRequest("POST", "/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(insertArgs) < 4 || insertArgs[2] != int64(42) || insertArgs[3] != "personal" {
		t.Fatalf("expected owner/key_type insert args, got %#v", insertArgs)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["owner_user_id"] != float64(42) || resp["key_type"] != "personal" {
		t.Fatalf("expected owner fields in create response, got %#v", resp)
	}
}

func TestCreateAPIKey_EmptyName(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"org_id":1,"name":""}`
	req := httptest.NewRequest("POST", "/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateAPIKey_RejectsUnsafeNameAndOversizedName(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})
	cases := []string{
		`{"org_id":1,"name":"<script>alert(1)</script>"}`,
		`{"org_id":1,"name":"prod'; DROP TABLE api_keys;--"}`,
		`{"org_id":1,"name":"` + strings.Repeat("a", 256) + `"}`,
	}

	for _, body := range cases {
		req := httptest.NewRequest("POST", "/keys", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.CreateAPIKey(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for body %s, got %d: %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateAPIKey_RejectsNegativeProjectID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"org_id":1,"project_id":-1,"name":"negative-project"}`
	req := httptest.NewRequest("POST", "/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAPIKey_DuplicateName(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "FROM api_keys") && strings.Contains(sql, "name") {
				return &testutil.MockRows{
					Rows: []func(dest ...any) error{
						func(dest ...any) error { return nil },
					},
				}, nil
			}
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"name":"test-key"}`
	req := httptest.NewRequest("POST", "/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateAPIKey_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("POST", "/keys", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateAPIKey_DBError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("duplicate key")
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"name":"test-key"}`
	req := httptest.NewRequest("POST", "/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestTestChannel_Upstream401IsFailureAndUsesSelectedModel(t *testing.T) {
	var gotPath string
	var gotMethod string
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer upstream.Close()

	keyEnc, err := auth.EncryptKey([]byte("bad-upstream-key"), []byte("0123456789abcdef01234567"))
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}

	queryCount := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
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
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"model":"mimo-v2.5","prompt":"ping"}`
	req := httptest.NewRequest("POST", "/channels/1/test", strings.NewReader(body))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.TestChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected handler 200 wrapper, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != false {
		t.Fatalf("expected success=false for upstream 401, got %v: %s", resp["success"], rec.Body.String())
	}
	if int(resp["status_code"].(float64)) != http.StatusUnauthorized {
		t.Fatalf("expected status_code=401, got %v", resp["status_code"])
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/chat/completions" {
		t.Fatalf("expected POST /v1/chat/completions, got %s %s", gotMethod, gotPath)
	}
	if gotModel != "mimo-v2.5" {
		t.Fatalf("expected selected model mimo-v2.5, got %q", gotModel)
	}
}

func TestTestChannel_AnthropicNativeUsesMessagesEndpoint(t *testing.T) {
	var gotPath string
	var gotAPIKey string
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id":"msg_test",
			"model":"claude-3-5-sonnet-20241022",
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":4,"output_tokens":3}
		}`))
	}))
	defer upstream.Close()

	keyEnc, err := auth.EncryptKey([]byte("sk-ant-test"), []byte("0123456789abcdef01234567"))
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}

	queryCount := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					switch queryCount {
					case 1:
						*dest[0].(*string) = upstream.URL
						*dest[1].(*string) = "claude-3-5-sonnet-20241022"
						*dest[2].(*string) = "anthropic_native"
						*dest[3].(*json.RawMessage) = json.RawMessage(`{"anthropic_version":"2023-06-01"}`)
					case 2:
						*dest[0].(*string) = keyEnc
					default:
						t.Fatalf("unexpected query count %d", queryCount)
					}
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"model":"claude-3-5-sonnet-20241022","prompt":"ping"}`
	req := httptest.NewRequest("POST", "/channels/1/test", strings.NewReader(body))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.TestChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected handler 200 wrapper, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["success"] != true {
		t.Fatalf("expected success=true, got %v: %s", resp["success"], rec.Body.String())
	}
	if resp["adapter_type"] != "anthropic_native" {
		t.Fatalf("expected adapter_type anthropic_native, got %v", resp["adapter_type"])
	}
	if int(resp["tokens_used"].(float64)) != 7 {
		t.Fatalf("expected 7 tokens, got %v", resp["tokens_used"])
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("expected POST /v1/messages, got %s", gotPath)
	}
	if gotAPIKey != "sk-ant-test" {
		t.Fatalf("expected x-api-key header, got %q", gotAPIKey)
	}
	if gotModel != "claude-3-5-sonnet-20241022" {
		t.Fatalf("expected model claude-3-5-sonnet-20241022, got %q", gotModel)
	}
	if !strings.Contains(resp["response"].(string), "chat.completion") {
		t.Fatalf("expected normalized OpenAI-compatible response, got %v", resp["response"])
	}
	if !strings.Contains(resp["curl_example"].(string), "x-api-key") || strings.Contains(resp["curl_example"].(string), "Authorization: Bearer") {
		t.Fatalf("expected anthropic-native curl example, got %v", resp["curl_example"])
	}
}

// ===================================================================
// DeleteUser tests
// ===================================================================

func TestDeleteUser_Success(t *testing.T) {
	var updatePersonalKeys bool
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = auth.RoleOrgViewer
						return nil
					}}
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "UPDATE api_keys") {
						updatePersonalKeys = true
						return pgconn.NewCommandTag("UPDATE 1"), nil
					}
					return pgconn.NewCommandTag("DELETE 1"), nil
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/users/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !updatePersonalKeys {
		t.Fatal("expected personal API keys to be disabled before deleting user")
	}
}

func TestDeleteUser_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("DELETE", "/users/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.DeleteUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteUser_DBError(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = auth.RoleOrgViewer
						return nil
					}}
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, fmt.Errorf("fk violation")
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/users/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteUser(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// ListSettings tests
// ===================================================================

func TestListSettings_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "site_name"
						*dest[1].(*[]byte) = []byte(`"UniRouter"`)
						return nil
					},
					func(dest ...any) error {
						*dest[0].(*string) = "default_model"
						*dest[1].(*[]byte) = []byte(`"gpt-4o"`)
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := platformGlobalControlRequest(httptest.NewRequest("GET", "/settings", nil))
	rec := httptest.NewRecorder()

	h.ListSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListSettings_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := platformGlobalControlRequest(httptest.NewRequest("GET", "/settings", nil))
	rec := httptest.NewRecorder()

	h.ListSettings(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// UpdateSetting tests
// ===================================================================

func TestUpdateSetting_Success(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"value":"new-value"}`
	req := platformGlobalControlRequest(httptest.NewRequest("PUT", "/settings/site_name", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("key", "site_name")
	rec := httptest.NewRecorder()

	h.UpdateSetting(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSetting_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"value":"new-value"}`
	req := platformGlobalControlRequest(httptest.NewRequest("PUT", "/settings/site_name", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("key", "site_name")
	rec := httptest.NewRecorder()

	h.UpdateSetting(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// ListUsers tests
// ===================================================================

func TestListUsers_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 1 // total
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1                   // id
						*dest[1].(*int64) = 1                   // org_id
						*dest[2].(**string) = strPtr("Default") // org_name
						*dest[3].(*string) = "admin"            // username
						*dest[4].(*string) = "admin"            // role
						*dest[5].(*string) = "active"           // status
						*dest[6].(*time.Time) = now             // created_at
						*dest[7].(*time.Time) = now             // updated_at
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/users", nil)
	rec := httptest.NewRecorder()

	h.ListUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["total"].(float64) != 1 {
		t.Fatalf("expected total=1, got %v", resp["total"])
	}
}

func TestListUsers_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return nil }}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/users", nil)
	rec := httptest.NewRecorder()

	h.ListUsers(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestListUsers_Empty(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 0
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/users", nil)
	rec := httptest.NewRecorder()

	h.ListUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// Helper to create *string
func strPtr(s string) *string { return &s }

// ===================================================================
// ListWalletTransactions tests
// ===================================================================

func TestListWalletTransactions_Success(t *testing.T) {
	_ = time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 2 // total
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "topup"
						*dest[2].(*int64) = 50_000_000
						*dest[3].(*string) = "Admin top-up"
						*dest[4].(*time.Time) = time.Now()
						return nil
					},
					func(dest ...any) error {
						*dest[0].(*int64) = 2
						*dest[1].(*string) = "settle"
						*dest[2].(*int64) = -1_000_000
						*dest[3].(*string) = "Settle for request"
						*dest[4].(*time.Time) = time.Now()
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet/transactions", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListWalletTransactions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(data))
	}
}

func TestListWalletTransactions_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return nil }}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet/transactions", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListWalletTransactions(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// GetChannel tests
// ===================================================================

func TestGetChannel_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*string) = "openai"
					*dest[2].(*string) = "openai"
					*dest[3].(*string) = "https://api.openai.com"
					*dest[4].(*string) = "gpt-4o"
					*dest[5].(*string) = "openai_compatible"
					*dest[6].(*json.RawMessage) = json.RawMessage(`{}`)
					*dest[7].(*int) = 100
					*dest[8].(*int) = 5
					*dest[9].(*string) = "active"
					*dest[10].(*string) = "healthy"
					*dest[11].(*int) = 0
					*dest[12].(*time.Time) = now
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/channels/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.GetChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["name"] != "openai" {
		t.Fatalf("expected name=openai, got %v", resp["name"])
	}
}

func TestGetChannel_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("GET", "/channels/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.GetChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetChannel_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("no rows")
				},
			}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/channels/999", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.GetChannel(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ===================================================================
// GetAPIKey tests
// ===================================================================

func TestGetAPIKey_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*int64) = 7
					*dest[3].(*string) = "test-key"
					*dest[4].(*string) = "sk-ab12"
					*dest[5].(*string) = "active"
					*dest[6].(*int) = 60
					*dest[7].(*int) = 10
					*dest[8].(*int) = 0
					*dest[9].(*string) = ""
					*dest[10].(*string) = ""
					*dest[11].(*time.Time) = now
					*dest[12].(*string) = "Default"
					*dest[13].(*string) = "Core"
					*dest[14].(*int64) = 42
					*dest[15].(*string) = "personal"
					*dest[16].(*string) = "alice"
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/keys/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.GetAPIKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["key_type"] != "personal" || resp["owner_username"] != "alice" {
		t.Fatalf("expected owner fields in get response, got %#v", resp)
	}
}

func TestGetAPIKey_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("GET", "/keys/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.GetAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetAPIKey_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("no rows")
				},
			}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/keys/999", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.GetAPIKey(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ===================================================================
// ListGrants tests
// ===================================================================

func TestListGrants_Success(t *testing.T) {
	now := time.Now()
	orgName := "Default"
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 1
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*int64) = 1
						*dest[2].(**string) = &orgName
						*dest[3].(*int64) = 10000
						*dest[4].(*string) = "welcome bonus"
						*dest[5].(*string) = "req-001"
						*dest[6].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/grants", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListGrants(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListGrants_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return nil }}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/grants", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListGrants(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// ListSensitiveWords tests
// ===================================================================

func TestListSensitiveWords_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = `["赌博"]`
				return nil
			}}
		},
	}
	h := newTestHandler(db)

	req := platformGlobalControlRequest(httptest.NewRequest("GET", "/sensitive-words", nil))
	rec := httptest.NewRecorder()

	h.ListSensitiveWords(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListSensitiveWords_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("db error")
				},
			}
		},
	}
	h := newTestHandler(db)

	req := platformGlobalControlRequest(httptest.NewRequest("GET", "/sensitive-words", nil))
	rec := httptest.NewRecorder()

	h.ListSensitiveWords(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// ListAuditLogs tests
// ===================================================================

func TestListAuditLogs_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 1
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*int64) = 3
						*dest[2].(*int64) = 9
						*dest[3].(*int64) = 7
						*dest[4].(*string) = "channel.create"
						*dest[5].(*string) = "channel"
						*dest[6].(*string) = "1"
						*dest[7].(*[]byte) = []byte(`{"key":"value"}`)
						*dest[8].(*[]byte) = []byte(`{"status":"old"}`)
						*dest[9].(*[]byte) = []byte(`{"status":"new"}`)
						*dest[10].(*string) = "127.0.0.1"
						*dest[11].(*string) = "agent"
						*dest[12].(*string) = "req-1"
						*dest[13].(*string) = "admin_console"
						*dest[14].(*string) = "client-1"
						*dest[15].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/audit-logs", nil)
	rec := httptest.NewRecorder()

	h.ListAuditLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	row := resp["data"].([]interface{})[0].(map[string]interface{})
	if row["actor_id"].(float64) != 7 || row["project_id"].(float64) != 9 {
		t.Fatalf("expected actor/project fields, got %#v", row)
	}
	if row["source"] != "admin_console" || row["request_id"] != "req-1" || row["client_request_id"] != "client-1" || row["user_agent"] != "agent" {
		t.Fatalf("expected source/request/user_agent fields, got %#v", row)
	}
}

func TestListAuditLogs_Filters(t *testing.T) {
	var capturedArgs []any
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "org_id = $1") || !strings.Contains(sql, "project_id = $2") || !strings.Contains(sql, "user_id = $3") ||
				!strings.Contains(sql, "request_id = $6") || !strings.Contains(sql, "client_request_id = $7") || !strings.Contains(sql, "source = $8") {
				t.Fatalf("expected filtered count query, got %s", sql)
			}
			capturedArgs = append([]any(nil), args...)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/audit-logs?org_id=3&project_id=9&actor_id=7&action=model_registry.update&resource=model_registry&request_id=op-1&client_request_id=client-1&source=admin", nil)
	rec := httptest.NewRecorder()

	h.ListAuditLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(capturedArgs) != 8 || capturedArgs[5] != "op-1" || capturedArgs[6] != "client-1" || capturedArgs[7] != "admin_console" {
		t.Fatalf("expected normalized admin_console source filter, got %#v", capturedArgs)
	}
}

func TestListAuditLogs_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return nil }}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/audit-logs", nil)
	rec := httptest.NewRecorder()

	h.ListAuditLogs(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// ListModelPrices tests
// ===================================================================

func TestListModelPrices_Success(t *testing.T) {
	_ = time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 1
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "gpt-4o"
						*dest[2].(*string) = "5"
						*dest[3].(*string) = "15"
						*dest[4].(*string) = "1"
						*dest[5].(*string) = "1"
						*dest[6].(*time.Time) = time.Now()
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/model-prices", nil)
	rec := httptest.NewRecorder()

	h.ListModelPrices(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListModelPrices_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/model-prices", nil)
	rec := httptest.NewRecorder()

	h.ListModelPrices(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// ListChannelKeys tests
// ===================================================================

func TestListChannelKeys_Success(t *testing.T) {
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
						*dest[0].(*int64) = 1
						*dest[1].(*int64) = 1
						*dest[2].(*string) = "openai"
						*dest[3].(*string) = "sk-...abc"
						*dest[4].(*string) = ""
						*dest[5].(*string) = "active"
						*dest[6].(*int) = 0
						*dest[7].(**time.Time) = nil
						*dest[8].(**time.Time) = nil
						*dest[9].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/channel-keys", nil)
	rec := httptest.NewRecorder()

	h.ListChannelKeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListChannelKeys_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/channel-keys", nil)
	rec := httptest.NewRecorder()

	h.ListChannelKeys(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// Suppress unused import warnings
var _ = time.Now()

// ===================================================================
// ListLogs tests
// ===================================================================

func TestListLogs_Success(t *testing.T) {
	now := time.Now()
	callCount := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 1 // total
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			callCount++
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
						*dest[11].(*int) = 70
						*dest[12].(*int) = 20
						*dest[13].(*int) = 10
						*dest[14].(*int) = 50
						*dest[15].(*int) = 150
						*dest[16].(*int64) = 2_000_000
						*dest[17].(*int) = 200
						*dest[18].(*int) = 350
						*dest[19].(*int) = 0
						*dest[20].(**int) = nil
						*dest[21].(*string) = ""
						*dest[22].(*string) = "backend"
						*dest[23].(*string) = "anthropic.prompt_cache.v1"
						*dest[24].(*string) = "cache_read_write_reported"
						*dest[25].(*string) = "pv2_test"
						*dest[26].(*float64) = 0.1
						*dest[27].(*float64) = 1.25
						*dest[28].(*int64) = 20_000_000
						*dest[29].(*int64) = 2_000_000
						*dest[30].(*int64) = 25_000_000
						*dest[31].(*int64) = 80_000_000
						*dest[32].(*int64) = 40
						*dest[33].(*int64) = 250
						*dest[34].(*bool) = false
						*dest[35].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/logs?page=1&page_size=10", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListLogs_WithFilters(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 0
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "usage_source = $5") {
				t.Fatalf("expected usage_source filter in query, got %s", sql)
			}
			if len(args) < 5 || args[4] != "counter_v2" {
				t.Fatalf("expected normalized usage_source arg counter_v2, got %#v", args)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*int64) = 7
						*dest[2].(*string) = "req-002"
						*dest[3].(*string) = "client-002"
						*dest[4].(*string) = "workflow-002"
						*dest[5].(*string) = "agent-002"
						*dest[6].(*string) = "att_002"
						*dest[7].(*string) = "gpt-4o"
						*dest[8].(*string) = "openai"
						*dest[9].(*string) = "test-channel"
						*dest[10].(*int) = 200
						*dest[11].(*int) = 200
						*dest[12].(*int) = 0
						*dest[13].(*int) = 0
						*dest[14].(*int) = 100
						*dest[15].(*int) = 300
						*dest[16].(*int64) = 2_000_000
						*dest[17].(*int) = 200
						*dest[18].(*int) = 500
						*dest[19].(*int) = 0
						*dest[20].(**int) = nil
						*dest[21].(*string) = ""
						*dest[22].(*string) = "backend"
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

	req := httptest.NewRequest("GET", "/logs?model=gpt-4o&provider=openai&status_code=200&usage_source=stream_estimate", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListLogs_StatusClassFilter(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "status_code >= $2 AND status_code < $3") {
				t.Fatalf("expected status class filter in count query, got %s", sql)
			}
			if len(args) != 3 || args[0] != int64(1) || args[1] != 400 || args[2] != 500 {
				t.Fatalf("expected 4xx count args, got %#v", args)
			}
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 0
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "status_code >= $2 AND status_code < $3") {
				t.Fatalf("expected status class filter in list query, got %s", sql)
			}
			if len(args) < 3 || args[0] != int64(1) || args[1] != 400 || args[2] != 500 {
				t.Fatalf("expected 4xx query args, got %#v", args)
			}
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/logs?status_code=4xx", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListLogs_StructuralEvidenceFilters(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			for _, condition := range []string{
				"trace_id = $2",
				"client_request_id = $3",
				"workflow_id = $4",
				"agent_id = $5",
				"upstream_request_id = $6",
				"evidence_ref = $7",
				"source_event_hash = $8",
				"identity_event.attempt_id = $9",
			} {
				if !strings.Contains(sql, condition) {
					t.Fatalf("expected %s in count query, got %s", condition, sql)
				}
			}
			expected := []any{int64(1), "trace-001", "client-001", "workflow-001", "agent-001", "upstream-001", "evidence-001", "hash-001", "att-001"}
			if !reflect.DeepEqual(args, expected) {
				t.Fatalf("expected structural filter args %#v, got %#v", expected, args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "source_event_hash = $8") || !strings.Contains(sql, "identity_event.attempt_id = $9") {
				t.Fatalf("expected structural filters in list query, got %s", sql)
			}
			if !strings.Contains(sql, "identity_event.org_id = call_logs.org_id") || !strings.Contains(sql, "identity_event.request_id = call_logs.request_id") {
				t.Fatalf("attempt lookup must remain tenant and operation scoped, got %s", sql)
			}
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/logs?trace_id=trace-001&client_request_id=client-001&workflow_id=workflow-001&agent_id=agent-001&attempt_id=att-001&upstream_request_id=upstream-001&evidence_ref=evidence-001&source_event_hash=hash-001", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListLogs_SearchesStructuralEvidenceFields(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			for _, column := range []string{
				"trace_id ILIKE $2", "client_request_id ILIKE $2", "workflow_id ILIKE $2", "agent_id ILIKE $2", "attempt_id ILIKE $2",
				"upstream_request_id ILIKE $2", "evidence_ref ILIKE $2", "source_event_hash ILIKE $2",
			} {
				if !strings.Contains(sql, column) {
					t.Fatalf("expected %s in search query, got %s", column, sql)
				}
			}
			if len(args) != 2 || args[0] != int64(1) || args[1] != "%evidence-001%" {
				t.Fatalf("expected structural search arg, got %#v", args)
			}
			if !strings.Contains(sql, "search_event.org_id = call_logs.org_id") || !strings.Contains(sql, "search_event.attempt_id ILIKE $2") {
				t.Fatalf("attempt search must include tenant-scoped trace events, got %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/logs?search=evidence-001", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListLogs_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return nil }}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/logs", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestListLogs_ScanError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 1
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "COALESCE(upstream_error, '')") {
				t.Fatalf("expected upstream_error to be null-safe, got query: %s", sql)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						return fmt.Errorf("scan error")
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/logs?page=1&page_size=10", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListLogs(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// ListReconciliation tests
// ===================================================================

func TestListReconciliation_Success(t *testing.T) {
	now := time.Date(2026, 6, 25, 4, 0, 0, 0, time.UTC)
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "FROM settings") {
				return &testutil.MockRow{
					ScanFn: func(dest ...any) error {
						*dest[0].(*string) = "0.25"
						return nil
					},
				}
			}
			if strings.Contains(sql, "AVG(CASE WHEN status_code < 400 THEN usage_confidence END)") {
				return &testutil.MockRow{
					ScanFn: func(dest ...any) error {
						*dest[0].(*int64) = 90
						*dest[1].(*int64) = 210
						*dest[2].(*int64) = 340
						*dest[3].(*int64) = 300
						*dest[4].(*float64) = 0.82
						*dest[5].(*int64) = 40
						*dest[6].(*int64) = 10
						*dest[7].(*int64) = 80
						*dest[8].(*int64) = 250
						*dest[9].(*int64) = 1
						*dest[10].(*int64) = 1
						return nil
					},
				}
			}
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 2
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "req-001"
						*dest[2].(*string) = "gpt-4o"
						*dest[3].(*int) = 150
						*dest[4].(*int) = 100
						*dest[5].(*int) = 50
						*dest[6].(*int) = 180
						*dest[7].(*int) = 200
						*dest[8].(*string) = "upstream"
						*dest[9].(*float64) = 1.0
						*dest[10].(*int) = 90
						*dest[11].(*int) = 40
						*dest[12].(*int) = 20
						*dest[13].(*string) = "anthropic.prompt_cache.v1"
						*dest[14].(*string) = "cache_read_write_reported"
						*dest[15].(*string) = "pv2_test"
						*dest[16].(*int64) = 80
						*dest[17].(*int64) = 500
						*dest[18].(*time.Time) = now
						return nil
					},
					func(dest ...any) error {
						*dest[0].(*int64) = 2
						*dest[1].(*string) = "req-002"
						*dest[2].(*string) = "moonshot-v1-8k"
						*dest[3].(*int) = 14
						*dest[4].(*int) = 12
						*dest[5].(*int) = 2
						*dest[6].(*int) = 30
						*dest[7].(*int) = 200
						*dest[8].(*string) = "counter_v2"
						*dest[9].(*float64) = 0.65
						*dest[10].(*int) = 12
						*dest[11].(*int) = 0
						*dest[12].(*int) = 0
						*dest[13].(*string) = ""
						*dest[14].(*string) = "not_reported"
						*dest[15].(*string) = "pv2_test"
						*dest[16].(*int64) = 0
						*dest[17].(*int64) = 0
						*dest[18].(*time.Time) = now.Add(time.Minute)
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/reconciliation", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListReconciliation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["total"].(float64) != 2 {
		t.Fatalf("expected total=2, got %v", resp["total"])
	}
	if got := len(resp["data"].([]interface{})); got != 2 {
		t.Fatalf("expected 2 rows, got %d", got)
	}
	first := resp["data"].([]interface{})[0].(map[string]interface{})
	if first["usage_source"] != "upstream" {
		t.Fatalf("expected usage_source upstream, got %v", first["usage_source"])
	}
	summary := resp["summary"].(map[string]interface{})
	if summary["estimated_ratio_alert_threshold"].(float64) != 0.25 {
		t.Fatalf("expected threshold 0.25, got %v", summary["estimated_ratio_alert_threshold"])
	}
	if summary["estimated_tokens"].(float64) != 90 {
		t.Fatalf("expected full-query estimated_tokens=90, got %v", summary["estimated_tokens"])
	}
	if summary["estimated_ratio_scope"] != "filtered_query" {
		t.Fatalf("expected filtered_query scope, got %v", summary["estimated_ratio_scope"])
	}
	if summary["cache_read_tokens"].(float64) != 40 || summary["cache_creation_tokens"].(float64) != 10 {
		t.Fatalf("expected prompt-cache summary, got %#v", summary)
	}
}

func TestListReconciliation_UsageSourceFilterNormalizesLegacyAlias(t *testing.T) {
	var capturedCountArgs []any
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "FROM settings") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "0.2"
					return nil
				}}
			}
			if strings.Contains(sql, "AVG(CASE WHEN status_code < 400 THEN usage_confidence END)") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 0
					*dest[1].(*int64) = 0
					*dest[2].(*int64) = 0
					*dest[3].(*int64) = 0
					*dest[4].(*float64) = 0
					*dest[5].(*int64) = 0
					*dest[6].(*int64) = 0
					*dest[7].(*int64) = 0
					*dest[8].(*int64) = 0
					*dest[9].(*int64) = 0
					*dest[10].(*int64) = 0
					return nil
				}}
			}
			capturedCountArgs = append([]any(nil), args...)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "usage_source = $2") {
				t.Fatalf("expected usage_source filter in query, got %s", sql)
			}
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/reconciliation?usage_source=stream_estimate", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListReconciliation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(capturedCountArgs) != 2 || capturedCountArgs[0] != int64(1) || capturedCountArgs[1] != "counter_v2" {
		t.Fatalf("expected normalized counter_v2 filter, got %#v", capturedCountArgs)
	}
}

func TestListReconciliation_EmitsHighEstimatedRatioEvent(t *testing.T) {
	var capturedEventArgs []any
	var deliveryStateUpdated bool
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "FROM settings"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "0.20"
					return nil
				}}
			case strings.Contains(sql, "AVG(CASE WHEN status_code < 400 THEN usage_confidence END)"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 80
					*dest[1].(*int64) = 20
					*dest[2].(*int64) = 95
					*dest[3].(*int64) = 100
					*dest[4].(*float64) = 0.62
					*dest[5].(*int64) = 0
					*dest[6].(*int64) = 0
					*dest[7].(*int64) = 0
					*dest[8].(*int64) = 0
					*dest[9].(*int64) = 0
					*dest[10].(*int64) = 1
					return nil
				}}
			case strings.Contains(sql, "INSERT INTO notification_events"):
				capturedEventArgs = append([]any(nil), args...)
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 77
					return nil
				}}
			default:
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 0
					return nil
				}}
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "UPDATE notification_events") {
				deliveryStateUpdated = true
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)
	h.SetEventService(billing.NewEventService(db))

	req := httptest.NewRequest("GET", "/reconciliation?usage_source=counter_v2", nil)
	req = withTestActor(req, 7, 3, auth.RoleBillingAdmin)
	rec := httptest.NewRecorder()

	h.ListReconciliation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(capturedEventArgs) == 0 {
		t.Fatal("expected notification event insert")
	}
	if capturedEventArgs[0] != int64(3) {
		t.Fatalf("expected org_id 3, got %#v", capturedEventArgs[0])
	}
	if capturedEventArgs[2] != billing.EventTokenMeteringEstimatedRatioHigh {
		t.Fatalf("unexpected event type: %#v", capturedEventArgs[2])
	}
	if capturedEventArgs[3] != billing.NotificationSeverityWarning {
		t.Fatalf("unexpected severity: %#v", capturedEventArgs[3])
	}
	payload, ok := capturedEventArgs[8].([]byte)
	if !ok {
		t.Fatalf("expected payload JSON bytes, got %T", capturedEventArgs[8])
	}
	var payloadData map[string]interface{}
	if err := json.Unmarshal(payload, &payloadData); err != nil {
		t.Fatalf("payload should be JSON: %v", err)
	}
	if payloadData["estimated_ratio"].(float64) != 0.8 {
		t.Fatalf("expected estimated_ratio 0.8, got %v", payloadData["estimated_ratio"])
	}
	if !deliveryStateUpdated {
		t.Fatal("expected notification event delivery state update")
	}
}

func TestListReconciliation_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return nil }}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/reconciliation", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ListReconciliation(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// DashboardStats tests
// ===================================================================

func TestDashboardStats_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					if strings.Contains(sql, "percentile_cont(0.95)") {
						switch len(dest) {
						case 1:
							*dest[0].(*float64) = 420.5
						case 2:
							*dest[0].(*float64) = 420.5
							*dest[1].(*float64) = 950.25
						}
						return nil
					}
					if strings.Contains(sql, "AVG(latency_ms)") {
						*dest[0].(*float64) = 350.5
						return nil
					}
					for _, target := range dest {
						switch value := target.(type) {
						case *int:
							*value = 100
						case *int64:
							*value = 1_000_000
						case *float64:
							*value = 350.5
						}
					}
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if strings.Contains(sql, "model_prices") {
				return &testutil.MockRows{
					Rows: []func(dest ...any) error{
						func(dest ...any) error {
							*dest[0].(*string) = "gpt-4o"
							*dest[1].(*string) = "50"
							*dest[2].(*string) = "150"
							return nil
						},
					},
				}, nil
			}
			if strings.Contains(sql, "channels") {
				// channel health: id, name, provider, status, success_count, fail_count
				return &testutil.MockRows{
					Rows: []func(dest ...any) error{
						func(dest ...any) error {
							*dest[0].(*int) = 1
							*dest[1].(*string) = "channel-1"
							*dest[2].(*string) = "openai"
							*dest[3].(*string) = "active"
							*dest[4].(*int) = 80
							*dest[5].(*int) = 5
							return nil
						},
					},
				}, nil
			}
			if strings.Contains(sql, "GROUP BY model") {
				// model distribution: model, cnt
				return &testutil.MockRows{
					Rows: []func(dest ...any) error{
						func(dest ...any) error {
							*dest[0].(*string) = "gpt-4o"
							*dest[1].(*int) = 50
							return nil
						},
					},
				}, nil
			}
			// recent logs or daily trend
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						if strings.Contains(sql, "date_trunc") {
							// trend: bucket, requests, cost
							*dest[0].(*time.Time) = now
							*dest[1].(*int) = 50
							*dest[2].(*int64) = 10_000_000
						} else {
							// recent logs: request_id, model, status_code, latency_ms, cost_micros, created_at
							*dest[0].(*string) = "req-001"
							*dest[1].(*string) = "gpt-4o"
							*dest[2].(*int) = 200
							*dest[3].(*int) = 350
							*dest[4].(*int64) = 50_000
							*dest[5].(*time.Time) = now
						}
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/dashboard/stats", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.DashboardStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["total_requests"]; !ok {
		t.Fatal("expected total_requests in response")
	}
	if _, ok := resp["recent_logs"]; !ok {
		t.Fatal("expected recent_logs in response")
	}
	if _, ok := resp["model_prices"]; !ok {
		t.Fatal("expected model_prices in response")
	}
	if _, ok := resp["daily_trend"]; !ok {
		t.Fatal("expected daily_trend in response")
	}
	if resp["p95_latency_ms"] != 420.5 {
		t.Fatalf("expected p95_latency_ms=420.5, got %#v", resp["p95_latency_ms"])
	}
	if resp["p99_latency_ms"] != 950.25 {
		t.Fatalf("expected p99_latency_ms=950.25, got %#v", resp["p99_latency_ms"])
	}
}

func TestDashboardOverview_TotalUsesTimeRange(t *testing.T) {
	var firstSQL string
	var firstArgs []any
	queryCount := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			if queryCount == 1 {
				firstSQL = sql
				firstArgs = append([]any(nil), args...)
			}
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					switch d := dest[0].(type) {
					case *int:
						*d = 7
					case *float64:
						*d = 123
					}
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	total, _, _, _ := h.dashboardOverview(context.Background(), 1, "2026-07-01T00:00:00Z", "2026-07-01T01:00:00Z")

	if total != 7 {
		t.Fatalf("expected total=7, got %d", total)
	}
	if !strings.Contains(firstSQL, "created_at >= $2") || !strings.Contains(firstSQL, "created_at <= $3") {
		t.Fatalf("expected total query to use time range, got %s", firstSQL)
	}
	if len(firstArgs) != 3 {
		t.Fatalf("expected org/start/end args, got %d", len(firstArgs))
	}
}

func TestDashboardLatencyPercentiles_UsesOrgTimeRangeAndPositiveLatency(t *testing.T) {
	var sqlSeen string
	var argsSeen []any
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			sqlSeen = sql
			argsSeen = append([]any(nil), args...)
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*float64) = 820
					*dest[1].(*float64) = 1240
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	p95, p99 := h.dashboardLatencyPercentiles(context.Background(), 42, "2026-07-01T00:00:00Z", "2026-07-01T01:00:00Z")

	if p95 != 820 || p99 != 1240 {
		t.Fatalf("expected p95=820 and p99=1240, got p95=%v p99=%v", p95, p99)
	}
	for _, expected := range []string{
		"percentile_cont(0.95)",
		"percentile_cont(0.99)",
		"org_id = $1",
		"created_at >= $2",
		"created_at <= $3",
		"latency_ms > 0",
	} {
		if !strings.Contains(sqlSeen, expected) {
			t.Fatalf("expected percentile query to contain %q, got %s", expected, sqlSeen)
		}
	}
	if len(argsSeen) != 3 || argsSeen[0] != int64(42) {
		t.Fatalf("expected org/start/end args, got %#v", argsSeen)
	}
}

// ===================================================================
// CreateChannel tests
// ===================================================================

func TestCreateChannel_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"name":"test-channel","provider":"openai","base_url":"https://api.openai.com","models":"gpt-4o"}`
	req := httptest.NewRequest("POST", "/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateChannel_MissingFields(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"name":"test-channel"}`
	req := httptest.NewRequest("POST", "/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateChannel_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("POST", "/channels", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateChannel_DBError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("duplicate key")
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"name":"test","provider":"openai","base_url":"https://api.openai.com","models":"gpt-4o"}`
	req := httptest.NewRequest("POST", "/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// DisableAPIKey tests
// ===================================================================

func TestDisableAPIKey_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 1
				*dest[1].(*int64) = 2
				*dest[2].(*string) = "Project key"
				*dest[3].(*string) = "uk_test"
				*dest[4].(*string) = "active"
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("POST", "/keys/1/disable", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DisableAPIKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDisableAPIKey_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("POST", "/keys/abc/disable", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.DisableAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDisableAPIKey_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				return pgx.ErrNoRows
			}}
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("POST", "/keys/999/disable", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.DisableAPIKey(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDisableAPIKey_EmitsNotificationEvent(t *testing.T) {
	var insertedPayload []byte
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "SELECT org_id, COALESCE(project_id, 0), name, key_prefix, status FROM api_keys"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 3
					*dest[1].(*int64) = 9
					*dest[2].(*string) = "Batch19 project key"
					*dest[3].(*string) = "uk_batch19"
					*dest[4].(*string) = "active"
					return nil
				}}
			case strings.Contains(sql, "INSERT INTO notification_events"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					if args[2] != billing.EventAPIKeyDisabled {
						t.Fatalf("expected api key disabled event, got %v", args[2])
					}
					insertedPayload = args[8].([]byte)
					*dest[0].(*int64) = 72
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow: %s", sql)
				return &testutil.MockRow{}
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "FROM notification_webhooks") {
				t.Fatalf("unexpected Query: %s", sql)
			}
			return &testutil.MockRows{}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)
	h.SetEventService(billing.NewEventService(db))

	req := httptest.NewRequest("POST", "/keys/1/disable", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DisableAPIKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(insertedPayload) == 0 || !strings.Contains(string(insertedPayload), "uk_batch19") {
		t.Fatalf("expected safe key prefix in notification payload, got %s", string(insertedPayload))
	}
}

// ===================================================================
// DeleteModelPrice tests
// ===================================================================

func TestDeleteModelPrice_Success(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return modelPriceMockRow(1, "gpt-4o", 5, 15)
			}}, nil
		},
	}
	h := newTestHandler(db)

	req := platformPricingRequest("DELETE", "/model-prices/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteModelPrice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteModelPrice_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("DELETE", "/model-prices/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.DeleteModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteModelPrice_DBError(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return fmt.Errorf("fk violation") }}
			}}, nil
		},
	}
	h := newTestHandler(db)

	req := platformPricingRequest("DELETE", "/model-prices/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteModelPrice(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// DeleteChannelKey tests
// ===================================================================

func TestDeleteChannelKey_Success(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/channel-keys/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteChannelKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteChannelKey_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("DELETE", "/channel-keys/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.DeleteChannelKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteChannelKey_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("fk violation")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/channel-keys/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteChannelKey(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// CreateModelPrice tests
// ===================================================================

func TestCreateModelPrice_Success(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "SELECT id") {
					return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
				}
				return modelPriceMockRow(42, "gpt-4o", 5, 15)
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"model_name":"gpt-4o","input_price_cny_per_million_tokens":50,"output_price_cny_per_million_tokens":150}`
	req := platformPricingRequest("POST", "/model-prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateModelPrice_DecimalCents(t *testing.T) {
	var capturedArgs []any
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "SELECT id") {
					return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
				}
				if strings.Contains(sql, "SELECT EXISTS") {
					return effectivePriceOverflowMockRow(false)
				}
				capturedArgs = append([]any(nil), args...)
				return modelPriceMockRow(42, "mimo-v2.5", 0.2, 0.1)
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"model_name":"mimo-v2.5","input_price_cny_per_million_tokens":2,"output_price_cny_per_million_tokens":1}`
	req := platformPricingRequest("POST", "/model-prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(capturedArgs) != 5 {
		t.Fatalf("expected 5 insert args, got %d", len(capturedArgs))
	}
	if capturedArgs[1] != "0.2" {
		t.Fatalf("expected input_price_cents=0.2, got %v", capturedArgs[1])
	}
	if capturedArgs[2] != "0.1" {
		t.Fatalf("expected output_price_cents=0.1, got %v", capturedArgs[2])
	}
	if capturedArgs[3] != nil || capturedArgs[4] != nil {
		t.Fatalf("expected omitted cache ratios to use SQL defaults/preserve existing values, got %v/%v", capturedArgs[3], capturedArgs[4])
	}
}

func TestCreateModelPrice_CacheRatios(t *testing.T) {
	var capturedArgs []any
	db := &testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) {
		return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "SELECT id") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
			if strings.Contains(sql, "SELECT EXISTS") {
				return effectivePriceOverflowMockRow(false)
			}
			capturedArgs = append([]any(nil), args...)
			return modelPriceMockRow(42, "claude", 4, 20)
		}}, nil
	}}
	h := newTestHandler(db)
	body := `{"model_name":"claude","input_price_cny_per_million_tokens":40,"output_price_cny_per_million_tokens":200,"cache_read_ratio":0.1,"cache_creation_ratio":1.25}`
	req := platformPricingRequest("POST", "/model-prices", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateModelPrice(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(capturedArgs) != 5 || capturedArgs[3] != "0.1" || capturedArgs[4] != "1.25" {
		t.Fatalf("unexpected cache ratio args: %#v", capturedArgs)
	}
}

func TestCreateModelPrice_PreservesExactStringDecimals(t *testing.T) {
	var capturedArgs []any
	db := &testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) {
		return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "SELECT id") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}
			if strings.Contains(sql, "SELECT EXISTS") {
				return effectivePriceOverflowMockRow(false)
			}
			capturedArgs = append([]any(nil), args...)
			return modelPriceExactMockRow(42, "exact-price", "0.000001", "123.456789", "0.123456", "1.234567")
		}}, nil
	}}
	h := newTestHandler(db)
	body := `{"model_name":"exact-price","input_price_cny_per_million_tokens":"0.00001","output_price_cny_per_million_tokens":"1234.56789","cache_read_ratio":"0.123456","cache_creation_ratio":"1.234567"}`
	req := platformPricingRequest("POST", "/model-prices", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	wantArgs := []any{"exact-price", "0.000001", "123.456789", "0.123456", "1.234567"}
	if !reflect.DeepEqual(capturedArgs, wantArgs) {
		t.Fatalf("exact DB args=%#v, want %#v", capturedArgs, wantArgs)
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["input_price_cny_per_million_tokens"] != "0.00001" ||
		response["output_price_cny_per_million_tokens"] != "1234.56789" ||
		response["cache_read_ratio"] != "0.123456" || response["cache_creation_ratio"] != "1.234567" {
		t.Fatalf("exact response contract lost precision: %s", rec.Body.String())
	}
}

func TestCreateModelPrice_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("POST", "/model-prices", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateModelPrice_DBError(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "SELECT id") {
					return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return fmt.Errorf("duplicate key") }}
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"model_name":"gpt-4o","input_price_cny_per_million_tokens":50,"output_price_cny_per_million_tokens":150}`
	req := platformPricingRequest("POST", "/model-prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// ListOrgModelPrices tests
// ===================================================================

func TestListOrgModelPrices_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int) = 1
					return nil
				},
			}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*int64) = 1
						*dest[2].(*string) = "Default"
						*dest[3].(*string) = "gpt-4o"
						*dest[4].(*string) = "1.5"
						*dest[5].(*time.Time) = now
						*dest[6].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := pricingActorRequest(httptest.NewRequest("GET", "/org-model-prices", nil), 1, auth.RoleOrgAdmin)
	rec := httptest.NewRecorder()

	h.ListOrgModelPrices(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListOrgModelPrices_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error { return nil }}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := pricingActorRequest(httptest.NewRequest("GET", "/org-model-prices", nil), 1, auth.RoleOrgAdmin)
	rec := httptest.NewRecorder()

	h.ListOrgModelPrices(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// CreateOrgModelPrice tests
// ===================================================================

func TestCreateOrgModelPrice_Success(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "SELECT id") {
					return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
				}
				return orgModelPriceMockRow(42, 1, "gpt-4o", 1.5)
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"model_name":"gpt-4o","multiplier":1.5}`
	req := platformPricingRequest("POST", "/org-model-prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateOrgModelPrice(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrgModelPrice_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("POST", "/org-model-prices", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateOrgModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateOrgModelPrice_DBError(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "SELECT id") {
					return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return fmt.Errorf("duplicate key") }}
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"model_name":"gpt-4o","multiplier":1.5}`
	req := platformPricingRequest("POST", "/org-model-prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateOrgModelPrice(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// UpdateSensitiveWords tests
// ===================================================================

func TestUpdateSensitiveWords_Success(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"words":["赌博","色情","spam"]}`
	req := platformGlobalControlRequest(httptest.NewRequest("PUT", "/sensitive-words", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateSensitiveWords(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSensitiveWords_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformGlobalControlRequest(httptest.NewRequest("PUT", "/sensitive-words", strings.NewReader("not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateSensitiveWords(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateSensitiveWords_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"words":["赌博"]}`
	req := platformGlobalControlRequest(httptest.NewRequest("PUT", "/sensitive-words", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateSensitiveWords(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// UpdateChannel tests
// ===================================================================

func TestUpdateChannel_Success(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)

	body := `{"name":"updated-name","priority":200}`
	req := httptest.NewRequest("PUT", "/channels/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateChannel_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"name":"test"}`
	req := httptest.NewRequest("PUT", "/channels/abc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateChannel_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("PUT", "/channels/1", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateChannel_NoFields(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{}`
	req := httptest.NewRequest("PUT", "/channels/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateChannel_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	body := `{"name":"test"}`
	req := httptest.NewRequest("PUT", "/channels/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestUpdateChannel_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil // RowsAffected=0
		},
	}
	h := newTestHandler(db)

	body := `{"name":"test"}`
	req := httptest.NewRequest("PUT", "/channels/999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.UpdateChannel(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ===================================================================
// UpdateAPIKey tests
// ===================================================================

func TestUpdateAPIKey_Success(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)

	body := `{"name":"updated-key","rpm_limit":120}`
	req := httptest.NewRequest("PUT", "/keys/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAPIKey_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"name":"test"}`
	req := httptest.NewRequest("PUT", "/keys/abc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateAPIKey_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("PUT", "/keys/1", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateAPIKey_RejectsUnsafeName(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("PUT", "/keys/1", strings.NewReader(`{"name":"<img src=x onerror=alert(1)>"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAPIKey_RejectsNegativeProjectID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("PUT", "/keys/1", strings.NewReader(`{"project_id":-1}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateAPIKey_NoFields(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{}`
	req := httptest.NewRequest("PUT", "/keys/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateAPIKey_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	body := `{"name":"test"}`
	req := httptest.NewRequest("PUT", "/keys/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestUpdateAPIKey_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"name":"test"}`
	req := httptest.NewRequest("PUT", "/keys/999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ===================================================================
// UpdateUser tests
// ===================================================================

func TestUpdateUser_Success(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)

	role := "admin"
	body := fmt.Sprintf(`{"role":"%s"}`, role)
	req := httptest.NewRequest("PUT", "/users/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateUser_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("PUT", "/users/abc", strings.NewReader(`{"role":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateUser_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("PUT", "/users/1", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateUser_NoFields(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("PUT", "/users/1", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateUser_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("PUT", "/users/1", strings.NewReader(`{"role":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestUpdateUser_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("PUT", "/users/999", strings.NewReader(`{"role":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.UpdateUser(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ===================================================================
// DeleteOrgModelPrice tests
// ===================================================================

func TestDeleteOrgModelPrice_Success(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return orgModelPriceMockRow(1, 1, "gpt-4o", 1.5)
			}}, nil
		},
	}
	h := newTestHandler(db)

	req := platformPricingRequest("DELETE", "/org-model-prices/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteOrgModelPrice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteOrgModelPrice_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("DELETE", "/org-model-prices/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.DeleteOrgModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteOrgModelPrice_DBError(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return fmt.Errorf("fk violation") }}
			}}, nil
		},
	}
	h := newTestHandler(db)

	req := platformPricingRequest("DELETE", "/org-model-prices/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteOrgModelPrice(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// UpdateModelPrice tests
// ===================================================================

func TestUpdateModelPrice_Success(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					if strings.Contains(sql, "FOR UPDATE") {
						return modelPriceMockRow(1, "gpt-4o", 5, 15)
					}
					return modelPriceMockRow(1, "gpt-4o", 10, 30)
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"input_price_cny_per_million_tokens":100,"output_price_cny_per_million_tokens":300}`
	req := platformPricingRequest("PUT", "/model-prices/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateModelPrice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateModelPrice_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("PUT", "/model-prices/abc", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.UpdateModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateModelPrice_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("PUT", "/model-prices/1", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateModelPrice_NoFields(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("PUT", "/model-prices/1", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateModelPrice_DBError(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return modelPriceMockRow(1, "gpt-4o", 5, 15)
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, fmt.Errorf("db error")
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"input_price_cny_per_million_tokens":100}`
	req := platformPricingRequest("PUT", "/model-prices/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateModelPrice(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestUpdateModelPrice_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"input_price_cny_per_million_tokens":100}`
	req := platformPricingRequest("PUT", "/model-prices/999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.UpdateModelPrice(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ===================================================================
// UpdateOrgModelPrice tests
// ===================================================================

func TestUpdateOrgModelPrice_Success(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "SELECT id") {
					return orgModelPriceMockRow(1, 1, "gpt-4o", 1.5)
				}
				return orgModelPriceMockRow(1, 1, "gpt-4o", 2)
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"multiplier":2.0}`
	req := platformPricingRequest("PUT", "/org-model-prices/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateOrgModelPrice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateOrgModelPrice_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("PUT", "/org-model-prices/abc", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.UpdateOrgModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateOrgModelPrice_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("PUT", "/org-model-prices/1", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateOrgModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateOrgModelPrice_NoFields(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformPricingRequest("PUT", "/org-model-prices/1", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateOrgModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateOrgModelPrice_DBError(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				if strings.Contains(sql, "SELECT id") {
					return orgModelPriceMockRow(1, 1, "gpt-4o", 1.5)
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return fmt.Errorf("db error") }}
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"multiplier":2.0}`
	req := platformPricingRequest("PUT", "/org-model-prices/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateOrgModelPrice(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestUpdateOrgModelPrice_NotFound(t *testing.T) {
	db := &testutil.MockDB{
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: func(dest ...any) error { return pgx.ErrNoRows }}
			}}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"multiplier":2.0}`
	req := platformPricingRequest("PUT", "/org-model-prices/999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()

	h.UpdateOrgModelPrice(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// ===================================================================
// AddSensitiveWords tests
// ===================================================================

func TestAddSensitiveWords_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*string) = `["赌博"]`
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"words":["色情","spam"]}`
	req := platformGlobalControlRequest(httptest.NewRequest("POST", "/sensitive-words/add", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.AddSensitiveWords(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	data := resp["data"].([]interface{})
	if len(data) != 3 {
		t.Fatalf("expected 3 words (merged), got %d", len(data))
	}
}

func TestAddSensitiveWords_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformGlobalControlRequest(httptest.NewRequest("POST", "/sensitive-words/add", strings.NewReader("not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.AddSensitiveWords(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAddSensitiveWords_DBQueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("db error")
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"words":["新词"]}`
	req := platformGlobalControlRequest(httptest.NewRequest("POST", "/sensitive-words/add", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.AddSensitiveWords(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddSensitiveWords_ExecError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*string) = `[]`
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"words":["test"]}`
	req := platformGlobalControlRequest(httptest.NewRequest("POST", "/sensitive-words/add", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.AddSensitiveWords(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// DeleteSensitiveWords tests
// ===================================================================

func TestDeleteSensitiveWords_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*string) = `["赌博","色情","spam"]`
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"words":["spam"]}`
	req := platformGlobalControlRequest(httptest.NewRequest("POST", "/sensitive-words/delete", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.DeleteSensitiveWords(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 remaining words, got %d", len(data))
	}
}

func TestDeleteSensitiveWords_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := platformGlobalControlRequest(httptest.NewRequest("POST", "/sensitive-words/delete", strings.NewReader("not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.DeleteSensitiveWords(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteSensitiveWords_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("db error")
				},
			}
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"words":["test"]}`
	req := platformGlobalControlRequest(httptest.NewRequest("POST", "/sensitive-words/delete", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.DeleteSensitiveWords(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestDeleteSensitiveWords_ExecError(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*string) = `["赌博","色情"]`
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(globalControlTestDB(db))

	body := `{"words":["赌博"]}`
	req := platformGlobalControlRequest(httptest.NewRequest("POST", "/sensitive-words/delete", strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.DeleteSensitiveWords(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// ExportWalletTransactions tests
// ===================================================================

func TestExportWalletTransactions_Success(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						balanceAfter := int64(150_000_000)
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "topup"
						*dest[2].(*int64) = 50_000_000
						*dest[3].(**int64) = &balanceAfter
						*dest[4].(*string) = "Admin top-up"
						*dest[5].(*string) = "req-001"
						*dest[6].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet/export", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ExportWalletTransactions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "text/csv; charset=utf-8" {
		t.Fatalf("expected text/csv, got %s", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "ID,类型") {
		t.Fatal("expected CSV header")
	}
	if !strings.Contains(body, "topup") {
		t.Fatal("expected topup in CSV")
	}
}

func TestExportWalletTransactions_QueryError(t *testing.T) {
	db := &testutil.MockDB{
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet/export", nil)
	req = withTestBillingActor(req)
	rec := httptest.NewRecorder()

	h.ExportWalletTransactions(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

// ===================================================================
// CreateChannelKey tests
// ===================================================================

func TestCreateChannelKey_Success(t *testing.T) {
	// encKey must be 16, 24, or 32 bytes for AES
	encKey := []byte("0123456789abcdef01234567") // 24 bytes
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := &Handler{db: db, jwtSecret: "test", encKey: encKey}

	body := `{"channel_id":1,"key":"sk-test-key-12345","allowed_models":"gpt-4o"}`
	req := httptest.NewRequest("POST", "/channel-keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannelKey(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateChannelKey_MissingFields(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"channel_id":1}`
	req := httptest.NewRequest("POST", "/channel-keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannelKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateChannelKey_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("POST", "/channel-keys", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannelKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateChannelKey_DBError(t *testing.T) {
	encKey := []byte("0123456789abcdef01234567")
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("duplicate key")
				},
			}
		},
	}
	h := &Handler{db: db, jwtSecret: "test", encKey: encKey}

	body := `{"channel_id":1,"key":"sk-test"}`
	req := httptest.NewRequest("POST", "/channel-keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannelKey(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestUpdateChannelKey_KeyResetsCooldown(t *testing.T) {
	encKey := []byte("0123456789abcdef01234567")
	var updateSQL string
	var updateArgs []any
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.HasPrefix(sql, "UPDATE channel_keys") {
				updateSQL = sql
				updateArgs = append([]any(nil), args...)
				return pgconn.NewCommandTag("UPDATE 1"), nil
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	h := &Handler{db: db, jwtSecret: "test", encKey: encKey}

	body := `{"channel_id":8,"key":"sk-new-mimo","status":"active","allowed_models":"mimo-v2.5"}`
	req := httptest.NewRequest("PUT", "/channel-keys/9", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "9")
	rec := httptest.NewRecorder()

	h.UpdateChannelKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"channel_id =", "key_enc =", "fail_count = 0", "cooldown_until = NULL", "last_used_at = NULL", "status =", "allowed_models ="} {
		if !strings.Contains(updateSQL, want) {
			t.Fatalf("expected update SQL to contain %q, got %s", want, updateSQL)
		}
	}
	if len(updateArgs) < 5 {
		t.Fatalf("expected update args, got %v", updateArgs)
	}
	encryptedKey, ok := updateArgs[1].(string)
	if !ok || encryptedKey == "" {
		t.Fatalf("expected encrypted key arg, got %#v", updateArgs[1])
	}
	decrypted, err := auth.DecryptKey(encryptedKey, encKey)
	if err != nil {
		t.Fatalf("decrypt updated key: %v", err)
	}
	if decrypted != "sk-new-mimo" {
		t.Fatalf("expected decrypted key sk-new-mimo, got %s", decrypted)
	}
}

// ===================================================================
// CreateGrant tests
// ===================================================================

func TestCreateGrant_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return pgx.ErrNoRows
				},
			}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{
						ScanFn: func(dest ...any) error {
							if strings.Contains(sql, "UPDATE wallets") {
								*dest[0].(*int64) = 110_000_000
							} else {
								*dest[0].(*int64) = 42 // grant id
							}
							return nil
						},
					}
				},
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, nil
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"amount_cents":10000,"reason":"welcome bonus"}`
	req := httptest.NewRequest("POST", "/grants", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateGrant(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateGrant_ZeroAmount(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	body := `{"org_id":1,"amount_cents":0}`
	req := httptest.NewRequest("POST", "/grants", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateGrant(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateGrant_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("POST", "/grants", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateGrant(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateGrant_Idempotency(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 99 // existing grant id
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"amount_cents":10000,"client_request_id":"req-123"}`
	req := httptest.NewRequest("POST", "/grants", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateGrant(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["duplicate"] != true {
		t.Fatal("expected duplicate=true")
	}
}

func TestCreateGrant_TxBeginFail(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return pgx.ErrNoRows
				},
			}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return nil, fmt.Errorf("connection pool exhausted")
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"amount_cents":10000,"reason":"test"}`
	req := httptest.NewRequest("POST", "/grants", strings.NewReader(body))
	req = withTestBillingActor(req)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateGrant(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestNormalizeModels(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expect string
	}{
		{"normal", "gpt-4o,gpt-4o-mini", "gpt-4o,gpt-4o-mini"},
		{"full-width comma", "gpt-4o，gpt-4o-mini", "gpt-4o,gpt-4o-mini"},
		{"mixed comma", "gpt-4o，gpt-4o-mini,claude-3", "gpt-4o,gpt-4o-mini,claude-3"},
		{"spaces around", " gpt-4o , gpt-4o-mini ", "gpt-4o,gpt-4o-mini"},
		{"duplicate removal", "gpt-4o,gpt-4o,gpt-4o-mini", "gpt-4o,gpt-4o-mini"},
		{"empty parts", "gpt-4o,,gpt-4o-mini", "gpt-4o,gpt-4o-mini"},
		{"full-width + spaces + dupes", " gpt-4o ， gpt-4o ，gpt-4o-mini", "gpt-4o,gpt-4o-mini"},
		{"single model", "gpt-4o", "gpt-4o"},
		{"all empty after trim", ", ,", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizeModels(c.input)
			if got != c.expect {
				t.Errorf("normalizeModels(%q) = %q, want %q", c.input, got, c.expect)
			}
		})
	}
}

func TestCreateChannel_NormalizesModels(t *testing.T) {
	var capturedModels string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			// args: name(0), provider(1), base_url(2), models(3), priority(4), degradation_threshold(5)
			if len(args) > 3 {
				if m, ok := args[3].(string); ok {
					capturedModels = m
				}
			}
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					return nil
				},
			}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	// Full-width comma, spaces, duplicates
	body := `{"name":"test","provider":"openai","base_url":"https://api.openai.com","models":" gpt-4o ， gpt-4o ，gpt-4o-mini "}`
	req := httptest.NewRequest("POST", "/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedModels != "gpt-4o,gpt-4o-mini" {
		t.Errorf("expected normalized models %q, got %q", "gpt-4o,gpt-4o-mini", capturedModels)
	}
}

func TestCreateChannel_NormalizesAllEmpty_Rejected(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	// All full-width commas + whitespace should normalize to empty => rejected
	body := `{"name":"test","provider":"openai","base_url":"https://api.openai.com","models":" ， ， "}`
	req := httptest.NewRequest("POST", "/channels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateChannel(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty-after-normalize models, got %d", rec.Code)
	}
}

// Suppress unused import warnings
var _ = pgtype.Int4{}
