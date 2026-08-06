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

func TestLoginUsesExactlyOneNormalizedLocalCandidateAndStoredIdentity(t *testing.T) {
	const hash = "$2a$10$FQbk3L3IVXe.UiYojf7YVeq1nbfb4oI.igx8Jed8fxF6slCek.hLG"
	var auditDetail string
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "WITH local_candidates AS MATERIALIZED") ||
				!strings.Contains(sql, "COALESCE(NULLIF(u.auth_provider, ''), 'local') = 'local'") ||
				!strings.Contains(sql, "public.normalize_local_login_id(u.username)") ||
				!strings.Contains(sql, "COUNT(*) FROM local_candidates") ||
				strings.Contains(sql, "LIMIT 1") {
				t.Fatalf("login query does not enforce exactly-one normalized local identity: %s", sql)
			}
			if len(args) != 1 || args[0] != "CASEINPUT" {
				t.Fatalf("expected trimmed display input, got %#v", args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 41
				*dest[1].(*int64) = 7
				*dest[2].(*string) = hash
				*dest[3].(*string) = "org_admin"
				*dest[4].(*string) = "org_admin"
				*dest[5].(*string) = "active"
				*dest[6].(*string) = "Example Org"
				*dest[7].(*string) = "stored@example.test"
				*dest[8].(*string) = "local"
				*dest[9].(*int64) = 3
				*dest[10].(*string) = "CaseStored"
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditDetail = args[5].(string)
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/auth/login",
		strings.NewReader(`{"username":"\t\u00a0CASEINPUT\u3000","password":"password123"}`))
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["username"] != "CaseStored" || body["org_id"] != float64(7) {
		t.Fatalf("response must use stored identity, got %#v", body)
	}
	if !strings.Contains(auditDetail, `"username":"CaseStored"`) || strings.Contains(auditDetail, "CASEINPUT") {
		t.Fatalf("audit must use stored identity, got %q", auditDetail)
	}
}

func TestCreateUserDoesNotMislabelUnrelatedDatabaseFailureAsIdentityConflict(t *testing.T) {
	db := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(...any) error {
			return fmt.Errorf("database unavailable")
		}}
	}}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(
		`{"org_id":1,"username":"new-user","password":"valid-password-123"}`,
	))
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for unrelated database error, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "unavailable") || strings.Contains(rec.Body.String(), "database") {
		t.Fatalf("response must not mislabel or disclose raw database error: %s", rec.Body.String())
	}
}
