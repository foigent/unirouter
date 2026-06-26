package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// --- Mock DBQuerier for middleware tests ---

type mwMockRow struct {
	scanFn func(dest ...any) error
}

func (m *mwMockRow) Scan(dest ...any) error {
	if m.scanFn != nil {
		return m.scanFn(dest...)
	}
	return fmt.Errorf("no rows")
}
func (m *mwMockRow) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mwMockRow) Values() ([]any, error)                       { return nil, nil }
func (m *mwMockRow) RawValues() [][]byte                          { return nil }
func (m *mwMockRow) Err() error                                   { return nil }
func (m *mwMockRow) Close()                                       {}
func (m *mwMockRow) Next() bool                                   { return false }

type mwMockDB struct {
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
}

func (m *mwMockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mwMockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mwMockRow{}
}
func (m *mwMockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *mwMockDB) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, fmt.Errorf("not implemented")
}

// --- resolveAPIKey tests ---

func TestResolveAPIKey_Success(t *testing.T) {
	budgetLimit := 10000
	db := &mwMockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mwMockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1          // id
					*dest[1].(*int64) = 1          // org_id
					*dest[2].(*string) = "active"  // status
					*dest[3].(*string) = ""        // allowed_ips
					*dest[4].(*string) = ""        // allowed_models
					*dest[5].(*int) = 60           // rpm_limit
					*dest[6].(*int) = 10           // qps_limit
					*dest[7].(**int) = &budgetLimit // budget_limit
					*dest[8].(*string) = "sk-ab12" // key_prefix
					*dest[9].(*string) = "test"    // name
					*dest[10].(*string) = "active" // org_status
					return nil
				},
			}
		},
	}

	apiKey, orgStatus, err := resolveAPIKey(context.Background(), db, "hash123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if apiKey.ID != 1 {
		t.Fatalf("expected ID=1, got %d", apiKey.ID)
	}
	if apiKey.OrgID != 1 {
		t.Fatalf("expected OrgID=1, got %d", apiKey.OrgID)
	}
	if orgStatus != "active" {
		t.Fatalf("expected orgStatus=active, got %s", orgStatus)
	}
	if apiKey.RPMLimit != 60 {
		t.Fatalf("expected RPMLimit=60, got %d", apiKey.RPMLimit)
	}
}

func TestResolveAPIKey_NotFound(t *testing.T) {
	db := &mwMockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mwMockRow{
				scanFn: func(dest ...any) error {
					return fmt.Errorf("no rows")
				},
			}
		},
	}

	_, _, err := resolveAPIKey(context.Background(), db, "invalid-hash")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

// --- GatewayAuth tests ---

func TestGatewayAuth_Success(t *testing.T) {
	budgetLimit := 10000
	db := &mwMockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mwMockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*string) = "active"
					*dest[3].(*string) = ""
					*dest[4].(*string) = ""
					*dest[5].(*int) = 60
					*dest[6].(*int) = 10
					*dest[7].(**int) = &budgetLimit
					*dest[8].(*string) = "sk-ab12"
					*dest[9].(*string) = "test"
					*dest[10].(*string) = "active"
					return nil
				},
			}
		},
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify context values are set
		keyID := GetKeyIDFromContext(r.Context())
		if keyID != 1 {
			t.Errorf("expected keyID=1, got %d", keyID)
		}
		orgID := GetOrgIDFromContext(r.Context())
		if orgID != 1 {
			t.Errorf("expected orgID=1, got %d", orgID)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	// Generate a valid API key
	rawKey := "sk-test-key-12345"

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayAuth_MissingHeader(t *testing.T) {
	db := &mwMockDB{}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestGatewayAuth_InvalidKey(t *testing.T) {
	db := &mwMockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mwMockRow{
				scanFn: func(dest ...any) error {
					return fmt.Errorf("no rows")
				},
			}
		},
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-invalid-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestGatewayAuth_DisabledKey(t *testing.T) {
	budgetLimit := 10000
	db := &mwMockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mwMockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*string) = "disabled" // key disabled
					*dest[3].(*string) = ""
					*dest[4].(*string) = ""
					*dest[5].(*int) = 60
					*dest[6].(*int) = 10
					*dest[7].(**int) = &budgetLimit
					*dest[8].(*string) = "sk-ab12"
					*dest[9].(*string) = "test"
					*dest[10].(*string) = "active"
					return nil
				},
			}
		},
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	rawKey := "sk-test-key-12345"
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestGatewayAuth_DisabledOrg(t *testing.T) {
	budgetLimit := 10000
	db := &mwMockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mwMockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*string) = "active"
					*dest[3].(*string) = ""
					*dest[4].(*string) = ""
					*dest[5].(*int) = 60
					*dest[6].(*int) = 10
					*dest[7].(**int) = &budgetLimit
					*dest[8].(*string) = "sk-ab12"
					*dest[9].(*string) = "test"
					*dest[10].(*string) = "disabled" // org disabled
					return nil
				},
			}
		},
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	rawKey := "sk-test-key-12345"
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestGatewayAuth_IPWhitelist(t *testing.T) {
	budgetLimit := 10000
	db := &mwMockDB{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mwMockRow{
				scanFn: func(dest ...any) error {
					*dest[0].(*int64) = 1
					*dest[1].(*int64) = 1
					*dest[2].(*string) = "active"
					*dest[3].(*string) = "10.0.0.0/8" // only allow internal IPs
					*dest[4].(*string) = ""
					*dest[5].(*int) = 60
					*dest[6].(*int) = 10
					*dest[7].(**int) = &budgetLimit
					*dest[8].(*string) = "sk-ab12"
					*dest[9].(*string) = "test"
					*dest[10].(*string) = "active"
					return nil
				},
			}
		},
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := GatewayAuth(db, "test-secret")(inner)

	rawKey := "sk-test-key-12345"
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("X-Forwarded-For", "8.8.8.8") // external IP
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (IP not allowed), got %d", rec.Code)
	}
}

// Suppress unused import
var _ = pgtype.Int4{}
