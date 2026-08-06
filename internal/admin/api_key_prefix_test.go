package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCreateAPIKeyRetriesPrefixCollision(t *testing.T) {
	insertAttempts := 0
	insertedPrefixes := make([]string, 0, 2)
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				switch {
				case strings.Contains(sql, "FROM projects"):
					*dest[0].(*int64) = 9
					return nil
				case strings.Contains(sql, "INSERT INTO api_keys"):
					insertAttempts++
					insertedPrefixes = append(insertedPrefixes, args[6].(string))
					if insertAttempts == 1 {
						return &pgconn.PgError{Code: "23505", ConstraintName: "idx_api_keys_prefix"}
					}
					*dest[0].(*int64) = 42
					return nil
				default:
					return fmt.Errorf("unexpected query: %s", sql)
				}
			}}
		},
	}
	h := NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
	req := httptest.NewRequest(http.MethodPost, "/keys", strings.NewReader(`{"org_id":1,"project_id":9,"name":"collision-retry"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 after retry, got %d: %s", rec.Code, rec.Body.String())
	}
	if insertAttempts != 2 {
		t.Fatalf("expected 2 insert attempts, got %d", insertAttempts)
	}
	if len(insertedPrefixes) != 2 || len(insertedPrefixes[0]) != 20 || len(insertedPrefixes[1]) != 20 {
		t.Fatalf("expected two 20-character prefixes, got %#v", insertedPrefixes)
	}
	var response struct {
		Key       string `json:"key"`
		KeyPrefix string `json:"key_prefix"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.KeyPrefix != insertedPrefixes[1] || !strings.HasPrefix(response.Key, response.KeyPrefix) {
		t.Fatalf("response must expose only the successfully inserted key: %#v", response)
	}
}

func TestCreateAPIKeyPrefixCollisionRetryIsBounded(t *testing.T) {
	insertAttempts := 0
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "FROM projects") {
					*dest[0].(*int64) = 9
					return nil
				}
				if strings.Contains(sql, "INSERT INTO api_keys") {
					insertAttempts++
					return &pgconn.PgError{Code: "23505", ConstraintName: "idx_api_keys_prefix"}
				}
				return fmt.Errorf("unexpected query: %s", sql)
			}}
		},
	}
	h := NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
	req := httptest.NewRequest(http.MethodPost, "/keys", strings.NewReader(`{"org_id":1,"project_id":9,"name":"collision-exhausted"}`))
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after retry exhaustion, got %d: %s", rec.Code, rec.Body.String())
	}
	if insertAttempts != apiKeyPrefixInsertMaxAttempts {
		t.Fatalf("expected %d attempts, got %d", apiKeyPrefixInsertMaxAttempts, insertAttempts)
	}
}

func TestIsAPIKeyPrefixCollisionTargetsOnlyPrefixIndex(t *testing.T) {
	if !isAPIKeyPrefixCollision(fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "23505", ConstraintName: "idx_api_keys_prefix"})) {
		t.Fatal("expected wrapped prefix-index violation to be retryable")
	}
	for _, err := range []error{
		&pgconn.PgError{Code: "23505", ConstraintName: "api_keys_pkey"},
		&pgconn.PgError{Code: "23503", ConstraintName: "idx_api_keys_prefix"},
		fmt.Errorf("duplicate key"),
	} {
		if isAPIKeyPrefixCollision(err) {
			t.Fatalf("unexpected retryable error: %v", err)
		}
	}
}
