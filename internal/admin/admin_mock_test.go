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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/uniroute/unirouter/internal/testutil"
)

// --- Mock infrastructure for admin handler tests ---

// --- Helper to create handler with mock DB ---
func newTestHandler(db *testutil.MockDB) *Handler {
	return NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
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
					*dest[0].(*int64) = 1    // id
					*dest[1].(*int64) = 1    // org_id
					*dest[2].(*string) = hash // password_hash
					*dest[3].(*string) = "admin" // role
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
						*dest[0].(*int64) = 1     // org_id
						*dest[1].(*int) = 10000   // balance
						*dest[2].(*int) = 5000    // granted_balance
						*dest[3].(*int) = 0       // frozen
						*dest[4].(*int) = 2500    // total_spent
						*dest[5].(*string) = "USD" // currency
						*dest[6].(*bool) = true   // auto_freeze
						*dest[7].(*int) = 1000    // low_balance_threshold_cents
						*dest[8].(*time.Time) = now // created_at
						*dest[9].(*time.Time) = now // updated_at
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet", nil)
	rec := httptest.NewRecorder()

	h.GetWallet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["balance_cents"].(float64) != 10000 {
		t.Fatalf("expected balance_cents=10000, got %v", resp["balance_cents"])
	}
	if resp["currency"] != "USD" {
		t.Fatalf("expected currency=USD, got %v", resp["currency"])
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
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1 // wallet org_id
					return nil
				},
			}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
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

	body := `{"amount_cents":5000,"description":"Test top-up"}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
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
	callCount := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					callCount++
					if callCount == 1 {
						*dest[0].(*int64) = 1 // wallet org_id
						return nil
					}
					// Second call: idempotency check finds existing transaction
					*dest[0].(*int64) = 42 // existing transaction id
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"amount_cents":5000,"client_request_id":"req-123"}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.TopUp(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["existing_id"].(float64) != 42 {
		t.Fatalf("expected existing_id=42, got %v", resp["existing_id"])
	}
}

func TestTopUp_WalletNotFound(t *testing.T) {
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

	body := `{"amount_cents":5000}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
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
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					return nil
				},
			}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return nil, fmt.Errorf("connection pool exhausted")
		},
	}
	h := newTestHandler(db)

	body := `{"amount_cents":5000}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
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
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					return nil
				},
			}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
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

	body := `{"amount_cents":5000}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
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
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					return nil
				},
			}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
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

	body := `{"amount_cents":5000}`
	req := httptest.NewRequest("POST", "/wallet/topup", strings.NewReader(body))
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
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1  // org_id
					*dest[1].(*int) = 5000 // current balance
					return nil
				},
			}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					return pgconn.CommandTag{}, nil
				},
			}, nil
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil // audit log
		},
	}
	h := newTestHandler(db)

	body := `{"target_balance":10000,"description":"Manual adjustment"}`
	req := httptest.NewRequest("POST", "/wallet/adjust", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.BalanceAdjust(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["previous"].(float64) != 5000 {
		t.Fatalf("expected previous=5000, got %v", resp["previous"])
	}
	if resp["new_balance"].(float64) != 10000 {
		t.Fatalf("expected new_balance=10000, got %v", resp["new_balance"])
	}
	if resp["adjust_amount"].(float64) != 5000 {
		t.Fatalf("expected adjust_amount=5000, got %v", resp["adjust_amount"])
	}
}

func TestBalanceAdjust_NegativeTarget(t *testing.T) {
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
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					return fmt.Errorf("no rows")
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"target_balance":10000}`
	req := httptest.NewRequest("POST", "/wallet/adjust", strings.NewReader(body))
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
						*dest[0].(*int64) = 1           // id
						*dest[1].(*string) = "openai"   // name
						*dest[2].(*string) = "openai"   // provider
						*dest[3].(*string) = "https://api.openai.com" // base_url
						*dest[4].(*string) = "gpt-4o"   // models
						*dest[5].(*int) = 100           // priority
						*dest[6].(*int) = 5             // degradation_threshold
						*dest[7].(*string) = "active"   // status
						*dest[8].(*string) = "healthy"  // circuit_state
						*dest[9].(*int) = 0             // fail_count
						*dest[10].(*time.Time) = now    // created_at
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
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execCalled = true
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	// recordAudit should not panic even on success
	h.recordAudit(context.Background(), "test.action", "resource", "1", map[string]interface{}{"key": "value"}, "127.0.0.1")

	if !execCalled {
		t.Fatal("expected exec to be called")
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
	// Should have 5 exec calls: call_logs, audit_logs, transactions, freeze_idempotency, grants expire
	// Plus 1 for deducting expired grants from wallets
	if execCount < 5 {
		t.Fatalf("expected at least 5 exec calls, got %d", execCount)
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
						*dest[0].(*int64) = 1           // id
						*dest[1].(*int64) = 1           // org_id
						*dest[2].(*string) = "test-key" // name
						*dest[3].(*string) = "sk-ab12"  // key_prefix
						*dest[4].(*string) = "active"   // status
						*dest[5].(*int) = 60            // rpm_limit
						*dest[6].(*int) = 10            // qps_limit
						*dest[7].(*int) = 0             // budget_limit
						*dest[8].(*string) = ""         // allowed_ips
						*dest[9].(*string) = ""         // allowed_models
						*dest[10].(*time.Time) = now    // created_at
						*dest[11].(*string) = "Default" // org_name
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

// ===================================================================
// DeleteUser tests
// ===================================================================

func TestDeleteUser_Success(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 1"), nil
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
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("fk violation")
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

	req := httptest.NewRequest("GET", "/settings", nil)
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

	req := httptest.NewRequest("GET", "/settings", nil)
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
	h := newTestHandler(db)

	body := `{"value":"new-value"}`
	req := httptest.NewRequest("PUT", "/settings/site_name", strings.NewReader(body))
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
	h := newTestHandler(db)

	body := `{"value":"new-value"}`
	req := httptest.NewRequest("PUT", "/settings/site_name", strings.NewReader(body))
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
						*dest[0].(*int64) = 1           // id
						*dest[1].(*int64) = 1           // org_id
						*dest[2].(**string) = strPtr("Default") // org_name
						*dest[3].(*string) = "admin"    // username
						*dest[4].(*string) = "admin"    // role
						*dest[5].(*string) = "active"   // status
						*dest[6].(*time.Time) = now     // created_at
						*dest[7].(*time.Time) = now     // updated_at
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
						*dest[2].(*int) = 5000
						*dest[3].(*string) = "Admin top-up"
						
						return nil
					},
					func(dest ...any) error {
						*dest[0].(*int64) = 2
						*dest[1].(*string) = "settle"
						*dest[2].(*int) = -100
						*dest[3].(*string) = "Settle for request"
						
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/wallet/transactions", nil)
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
					*dest[5].(*int) = 100
					*dest[6].(*int) = 5
					*dest[7].(*string) = "active"
					*dest[8].(*string) = "healthy"
					*dest[9].(*int) = 0
					*dest[10].(*time.Time) = now
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
					*dest[2].(*string) = "test-key"
					*dest[3].(*string) = "sk-ab12"
					*dest[4].(*string) = "active"
					*dest[5].(*int) = 60
					*dest[6].(*int) = 10
					*dest[7].(*int) = 0
					*dest[8].(*string) = ""
					*dest[9].(*string) = ""
					*dest[10].(*time.Time) = now
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
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "赌博"
						*dest[2].(*time.Time) = time.Now()
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/sensitive-words", nil)
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

	req := httptest.NewRequest("GET", "/sensitive-words", nil)
	rec := httptest.NewRecorder()

	h.ListSensitiveWords(rec, req)

	// ListSensitiveWords returns 200 with empty list on error
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	data := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Fatalf("expected empty data, got %d items", len(data))
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
						*dest[1].(*int64) = 0
						*dest[2].(*string) = "channel.create"
						*dest[3].(*string) = "channel"
						*dest[4].(*string) = "1"
						*dest[5].(*[]byte) = []byte(`{}`)
						*dest[6].(*time.Time) = now
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
						*dest[2].(*int) = 5
						*dest[3].(*int) = 15
						
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
						*dest[1].(*string) = "req-001"
						*dest[2].(*string) = "gpt-4o"
						*dest[3].(*string) = "openai"
						*dest[4].(*string) = "test-channel"
						*dest[5].(*int) = 100
						*dest[6].(*int) = 50
						*dest[7].(*int) = 150
						*dest[8].(*int) = 200
						*dest[9].(*int) = 200
						*dest[10].(*int) = 350
						*dest[11].(*int) = 0
						dest[12] = (*int)(nil)
						*dest[13].(*string) = ""
						*dest[14].(*string) = "backend"
						*dest[15].(*bool) = false
						*dest[16].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/logs?page=1&page_size=10", nil)
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
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "req-002"
						*dest[2].(*string) = "gpt-4o"
						*dest[3].(*string) = "openai"
						*dest[4].(*string) = "test-channel"
						*dest[5].(*int) = 200
						*dest[6].(*int) = 100
						*dest[7].(*int) = 300
						*dest[8].(*int) = 200
						*dest[9].(*int) = 200
						*dest[10].(*int) = 500
						*dest[11].(*int) = 0
						dest[12] = (*int)(nil)
						*dest[13].(*string) = ""
						*dest[14].(*string) = "backend"
						*dest[15].(*bool) = false
						*dest[16].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/logs?model=gpt-4o&provider=openai&status_code=200", nil)
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
						*dest[1].(*string) = "req-001"
						*dest[2].(*string) = "gpt-4o"
						*dest[3].(*int) = 150
						*dest[4].(*int) = 100
						*dest[5].(*int) = 50
						*dest[6].(*int) = 200
						*dest[7].(*string) = "2026-06-25T04:00:00Z"
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/reconciliation", nil)
	rec := httptest.NewRecorder()

	h.ListReconciliation(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["total"].(float64) != 1 {
		t.Fatalf("expected total=1, got %v", resp["total"])
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
	queryCount := 0
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					queryCount++
					// Different queries scan different types
					if queryCount == 3 || queryCount == 8 {
						// avgLatencyMs is float64
						*dest[0].(*float64) = 350.5
						return nil
					}
					switch len(dest) {
					case 1:
						*dest[0].(*int) = 100
					case 2:
						*dest[0].(*int) = 500
						*dest[1].(*int) = 100
					case 3:
						*dest[0].(*int) = 5000
						*dest[1].(*int) = 2000
						*dest[2].(*int) = 0
					case 4:
						*dest[0].(*int) = 5000
						*dest[1].(*int) = 2000
						*dest[2].(*int) = 0
						*dest[3].(*int) = 1000
					default:
						// avg latency
						if len(dest) == 1 {
							*dest[0].(*float64) = 350.5
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
							*dest[1].(*int) = 5
							*dest[2].(*int) = 15
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
						if strings.Contains(sql, "DATE") {
							// daily trend: day, requests, cost
							*dest[0].(*time.Time) = now
							*dest[1].(*int) = 50
							*dest[2].(*int) = 1000
						} else {
							// recent logs: request_id, model, status_code, latency_ms, cost_cents, created_at
							*dest[0].(*string) = "req-001"
							*dest[1].(*string) = "gpt-4o"
							*dest[2].(*int) = 200
							*dest[3].(*int) = 350
							*dest[4].(*int) = 5
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
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil // RowsAffected=0
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

// ===================================================================
// DeleteModelPrice tests
// ===================================================================

func TestDeleteModelPrice_Success(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/model-prices/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteModelPrice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteModelPrice_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("DELETE", "/model-prices/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.DeleteModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteModelPrice_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("fk violation")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/model-prices/1", nil)
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
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"model_name":"gpt-4o","input_price_cents":5,"output_price_cents":15}`
	req := httptest.NewRequest("POST", "/model-prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateModelPrice_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("POST", "/model-prices", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateModelPrice_DBError(t *testing.T) {
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

	body := `{"model_name":"gpt-4o","input_price_cents":5,"output_price_cents":15}`
	req := httptest.NewRequest("POST", "/model-prices", strings.NewReader(body))
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
						*dest[2].(*string) = "gpt-4o"
						*dest[3].(*float64) = 1.5
						*dest[4].(*time.Time) = now
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("GET", "/org-model-prices", nil)
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

	req := httptest.NewRequest("GET", "/org-model-prices", nil)
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
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 42
					return nil
				},
			}
		},
	}
	h := newTestHandler(db)

	body := `{"org_id":1,"model_name":"gpt-4o","multiplier":1.5}`
	req := httptest.NewRequest("POST", "/org-model-prices", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateOrgModelPrice(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrgModelPrice_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("POST", "/org-model-prices", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateOrgModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateOrgModelPrice_DBError(t *testing.T) {
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

	body := `{"org_id":1,"model_name":"gpt-4o","multiplier":1.5}`
	req := httptest.NewRequest("POST", "/org-model-prices", strings.NewReader(body))
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
	h := newTestHandler(db)

	body := `{"words":["赌博","色情","spam"]}`
	req := httptest.NewRequest("PUT", "/sensitive-words", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.UpdateSensitiveWords(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateSensitiveWords_InvalidJSON(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("PUT", "/sensitive-words", strings.NewReader("not json"))
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
	h := newTestHandler(db)

	body := `{"words":["赌博"]}`
	req := httptest.NewRequest("PUT", "/sensitive-words", strings.NewReader(body))
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
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/org-model-prices/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.DeleteOrgModelPrice(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteOrgModelPrice_InvalidID(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest("DELETE", "/org-model-prices/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()

	h.DeleteOrgModelPrice(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestDeleteOrgModelPrice_DBError(t *testing.T) {
	db := &testutil.MockDB{
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("fk violation")
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest("DELETE", "/org-model-prices/1", nil)
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
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)

	body := `{"input_price_cents":10,"output_price_cents":30}`
	req := httptest.NewRequest("PUT", "/model-prices/1", strings.NewReader(body))
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

	req := httptest.NewRequest("PUT", "/model-prices/abc", strings.NewReader(`{}`))
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

	req := httptest.NewRequest("PUT", "/model-prices/1", strings.NewReader("not json"))
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

	req := httptest.NewRequest("PUT", "/model-prices/1", strings.NewReader(`{}`))
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
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	body := `{"input_price_cents":10}`
	req := httptest.NewRequest("PUT", "/model-prices/1", strings.NewReader(body))
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
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"input_price_cents":10}`
	req := httptest.NewRequest("PUT", "/model-prices/999", strings.NewReader(body))
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
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	h := newTestHandler(db)

	body := `{"multiplier":2.0}`
	req := httptest.NewRequest("PUT", "/org-model-prices/1", strings.NewReader(body))
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

	req := httptest.NewRequest("PUT", "/org-model-prices/abc", strings.NewReader(`{}`))
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

	req := httptest.NewRequest("PUT", "/org-model-prices/1", strings.NewReader("not json"))
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

	req := httptest.NewRequest("PUT", "/org-model-prices/1", strings.NewReader(`{}`))
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
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db error")
		},
	}
	h := newTestHandler(db)

	body := `{"multiplier":2.0}`
	req := httptest.NewRequest("PUT", "/org-model-prices/1", strings.NewReader(body))
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
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
	h := newTestHandler(db)

	body := `{"multiplier":2.0}`
	req := httptest.NewRequest("PUT", "/org-model-prices/999", strings.NewReader(body))
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
	h := newTestHandler(db)

	body := `{"words":["色情","spam"]}`
	req := httptest.NewRequest("POST", "/sensitive-words/add", strings.NewReader(body))
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

	req := httptest.NewRequest("POST", "/sensitive-words/add", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.AddSensitiveWords(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAddSensitiveWords_DBQueryError(t *testing.T) {
	// When query fails, valueJSON defaults to "[]"
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
	h := newTestHandler(db)

	body := `{"words":["新词"]}`
	req := httptest.NewRequest("POST", "/sensitive-words/add", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.AddSensitiveWords(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
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
	h := newTestHandler(db)

	body := `{"words":["test"]}`
	req := httptest.NewRequest("POST", "/sensitive-words/add", strings.NewReader(body))
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
	h := newTestHandler(db)

	body := `{"words":["spam"]}`
	req := httptest.NewRequest("POST", "/sensitive-words/delete", strings.NewReader(body))
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

	req := httptest.NewRequest("POST", "/sensitive-words/delete", strings.NewReader("not json"))
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
	h := newTestHandler(db)

	body := `{"words":["test"]}`
	req := httptest.NewRequest("POST", "/sensitive-words/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.DeleteSensitiveWords(rec, req)

	// Query error returns 200 with empty list
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
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
	h := newTestHandler(db)

	body := `{"words":["赌博"]}`
	req := httptest.NewRequest("POST", "/sensitive-words/delete", strings.NewReader(body))
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
						*dest[0].(*int64) = 1
						*dest[1].(*string) = "topup"
						*dest[2].(*int) = 5000
						*dest[3].(*int) = 15000
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

// ===================================================================
// CreateGrant tests
// ===================================================================

func TestCreateGrant_Success(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			callCount := 0
			return &testutil.MockRow{
				ScanFn: func(dest ...any) error {
					callCount++
					if callCount == 1 {
						// idempotency check: no existing grant
						return fmt.Errorf("no rows")
					}
					return nil
				},
			}
		},
		BeginFn: func(ctx context.Context) (pgx.Tx, error) {
			return &testutil.MockTx{
				QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
					return &testutil.MockRow{
						ScanFn: func(dest ...any) error {
							*dest[0].(*int64) = 42 // grant id
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
					return fmt.Errorf("no rows")
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
