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
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCreateAPIKeyPlatformRequiresExplicitOrganization(t *testing.T) {
	dbCalled := false
	db := &testutil.MockDB{
		QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			dbCalled = true
			return &testutil.MockRows{}, nil
		},
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			dbCalled = true
			return &testutil.MockRow{}
		},
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPost, "/api/v1/keys", `{"name":"missing-org"}`, auth.RolePlatformAdmin, 1, 10)
	rec := httptest.NewRecorder()

	h.CreateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if dbCalled {
		t.Fatal("missing organization must be rejected before database work")
	}
}

func TestUpdateAPIKeyRejectsUnsupportedStatusBeforeDatabaseAccess(t *testing.T) {
	dbCalled := false
	db := &testutil.MockDB{
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			dbCalled = true
			return &testutil.MockRow{}
		},
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			dbCalled = true
			return pgconn.CommandTag{}, nil
		},
		BeginFn: func(context.Context) (pgx.Tx, error) {
			dbCalled = true
			return &testutil.MockTx{}, nil
		},
	}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/keys/1", strings.NewReader(`{"status":"revoked"}`))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()

	h.UpdateAPIKey(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Invalid status") {
		t.Fatalf("expected 400 Invalid status, got %d: %s", rec.Code, rec.Body.String())
	}
	if dbCalled {
		t.Fatal("unsupported API key status must be rejected before database work")
	}
}

func TestAPIKeyMutationsRejectAStaleAuthorizedSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		run  func(*Handler, http.ResponseWriter, *http.Request)
		row  func([]any, int64)
	}{
		{
			name: "update",
			body: `{"name":"stale-update"}`,
			run:  (*Handler).UpdateAPIKey,
			row: func(dest []any, projectID int64) {
				*dest[0].(*int64) = 3
				*dest[1].(*int64) = projectID
				*dest[2].(*int64) = 42
				*dest[3].(*string) = "personal"
			},
		},
		{
			name: "disable",
			run:  (*Handler).DisableAPIKey,
			row: func(dest []any, projectID int64) {
				*dest[0].(*int64) = 3
				*dest[1].(*int64) = projectID
				*dest[2].(*int64) = 42
				*dest[3].(*string) = "stale-key"
				*dest[4].(*string) = "uk_stale"
				*dest[5].(*string) = "active"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutationCalled := false
			tx := &testutil.MockTx{
				QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
					switch {
					case strings.Contains(sql, "FROM users u"):
						return &testutil.MockRow{ScanFn: func(dest ...any) error {
							*dest[0].(*int64) = 1
							*dest[1].(*string) = auth.RolePlatformAdmin
							*dest[2].(*string) = auth.RolePlatformAdmin
							*dest[3].(*string) = "active"
							*dest[4].(*string) = "active"
							return nil
						}}
					case strings.Contains(sql, "SELECT status FROM orgs"):
						return &testutil.MockRow{ScanFn: func(dest ...any) error {
							*dest[0].(*string) = "active"
							return nil
						}}
					case strings.Contains(sql, "SELECT status FROM projects"):
						return &testutil.MockRow{ScanFn: func(dest ...any) error {
							*dest[0].(*string) = "active"
							return nil
						}}
					case strings.Contains(sql, "FROM api_keys") && strings.Contains(sql, "FOR UPDATE"):
						return &testutil.MockRow{ScanFn: func(dest ...any) error {
							tc.row(dest, 10)
							return nil
						}}
					default:
						t.Fatalf("unexpected transaction read: %s", sql)
						return &testutil.MockRow{}
					}
				},
				ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "pg_advisory_xact_lock") {
						return pgconn.NewCommandTag("SELECT 1"), nil
					}
					mutationCalled = true
					return pgconn.NewCommandTag("UPDATE 1"), nil
				},
			}
			db := &testutil.MockDB{
				QueryRowFn: func(context.Context, string, ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(dest ...any) error {
						tc.row(dest, 9)
						return nil
					}}
				},
				BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil },
			}
			h := newTestHandler(db)
			req := p6f16Request(http.MethodPut, "/api/v1/keys/1", tc.body, auth.RolePlatformAdmin, 1, 70)
			req.SetPathValue("id", "1")
			rec := httptest.NewRecorder()

			tc.run(h, rec, req)

			if rec.Code != http.StatusConflict {
				t.Fatalf("expected 409 for stale key scope, got %d: %s", rec.Code, rec.Body.String())
			}
			if mutationCalled {
				t.Fatal("stale authorization snapshot must not mutate or audit the key")
			}
		})
	}
}

func TestUserMutationLookupDatabaseErrorsReturnInternalServerError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		body   string
		run    func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{name: "update", method: http.MethodPut, body: `{"status":"disabled"}`, run: (*Handler).UpdateUser},
		{name: "delete", method: http.MethodDelete, run: (*Handler).DeleteUser},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &testutil.MockTx{
				QueryRowFn: func(context.Context, string, ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(...any) error { return fmt.Errorf("database unavailable") }}
				},
				ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					return pgconn.NewCommandTag("SELECT 1"), nil
				},
			}
			h := newTestHandler(&testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }})
			req := p6f16Request(tc.method, "/api/v1/users/42", tc.body, auth.RolePlatformAdmin, 1, 99)
			req.SetPathValue("id", "42")
			rec := httptest.NewRecorder()

			tc.run(h, rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("expected 500 for database lookup error, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestReplaceProjectModelSetsLocksActiveSetsInStableOrder(t *testing.T) {
	var lockedIDs []int64
	tx := &testutil.MockTx{
		QueryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if strings.Contains(sql, "FROM projects") {
					*dest[0].(*int64) = 7
					return nil
				}
				if !strings.Contains(sql, "FROM model_sets") || !strings.Contains(sql, "FOR SHARE") {
					t.Fatalf("active model-set validation must hold a shared row lock, got %s", sql)
				}
				modelSetID := args[0].(int64)
				lockedIDs = append(lockedIDs, modelSetID)
				*dest[0].(*int64) = modelSetID
				return nil
			}}
		},
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	db := &testutil.MockDB{
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil },
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPut, "/api/v1/projects/42/model-sets", `{"model_set_ids":[2,1,2]}`, auth.RoleOrgAdmin, 7, 70)
	req.SetPathValue("id", "42")
	rec := httptest.NewRecorder()

	h.ReplaceProjectModelSets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fmt.Sprint(lockedIDs) != "[1 2]" {
		t.Fatalf("expected deterministic ascending lock order [1 2], got %v", lockedIDs)
	}
}

func TestCreateUserPlatformRequiresExplicitOrganization(t *testing.T) {
	dbCalled := false
	db := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		dbCalled = true
		return &testutil.MockRow{}
	}}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPost, "/api/v1/users", `{"username":"missing-org","password":"secret-password-123"}`, auth.RolePlatformAdmin, 1, 10)
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if dbCalled {
		t.Fatal("missing organization must be rejected before database work")
	}
}

func TestCreateHandlersRejectNegativeOrganization(t *testing.T) {
	tests := []struct {
		name string
		body string
		run  func(*Handler, http.ResponseWriter, *http.Request)
	}{
		{
			name: "api key",
			body: `{"org_id":-1,"name":"negative-org"}`,
			run:  (*Handler).CreateAPIKey,
		},
		{
			name: "user",
			body: `{"org_id":-1,"username":"negative-org","password":"secret-password-123"}`,
			run:  (*Handler).CreateUser,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dbCalled := false
			db := &testutil.MockDB{
				QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
					dbCalled = true
					return &testutil.MockRows{}, nil
				},
				QueryRowFn: func(context.Context, string, ...any) pgx.Row {
					dbCalled = true
					return &testutil.MockRow{}
				},
			}
			h := newTestHandler(db)
			req := p6f16Request(http.MethodPost, "/", tc.body, auth.RolePlatformAdmin, 1, 10)
			rec := httptest.NewRecorder()

			tc.run(h, rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if dbCalled {
				t.Fatal("negative organization must be rejected before database work")
			}
		})
	}
}

func TestCreateUserOrgAdminDefaultsToActorOrganization(t *testing.T) {
	var insertedOrgID int64
	tx := &testutil.MockTx{QueryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
		if !strings.Contains(sql, "INSERT INTO users") {
			t.Fatalf("unexpected query: %s", sql)
		}
		insertedOrgID = args[0].(int64)
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*int64) = 42
			return nil
		}}
	}}
	db := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodPost, "/api/v1/users", `{"username":"own-org","password":"secret-password-123"}`, auth.RoleOrgAdmin, 7, 10)
	rec := httptest.NewRecorder()

	h.CreateUser(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if insertedOrgID != 7 {
		t.Fatalf("expected actor organization 7, got %d", insertedOrgID)
	}
}

func TestListAPIKeysUsesDatabaseTotalAndPagination(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "SELECT COUNT(*) FROM api_keys ak") {
				t.Fatalf("expected key count query, got %s", sql)
			}
			if len(args) != 0 {
				t.Fatalf("unexpected count args: %#v", args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 37
				return nil
			}}
		},
		QueryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "LIMIT $1 OFFSET $2") {
				t.Fatalf("expected bounded key query, got %s", sql)
			}
			if len(args) != 2 || args[0] != 10 || args[1] != 20 {
				t.Fatalf("unexpected pagination args: %#v", args)
			}
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodGet, "/api/v1/keys?page=3&page_size=10", "", auth.RolePlatformAdmin, 1, 10)
	rec := httptest.NewRecorder()

	h.ListAPIKeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 37 {
		t.Fatalf("expected database total 37, got %d", response.Total)
	}
}

func TestListOrganizationsPlatformReadsAllInStableOrder(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			if strings.Contains(sql, "WHERE") {
				t.Fatalf("platform count must not be tenant constrained: %s", sql)
			}
			if len(args) != 0 {
				t.Fatalf("unexpected count args: %#v", args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 2
				return nil
			}}
		},
		QueryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "ORDER BY name ASC, id ASC LIMIT $1 OFFSET $2") {
				t.Fatalf("expected stable paginated organization query, got %s", sql)
			}
			if len(args) != 2 || args[0] != 20 || args[1] != 0 {
				t.Fatalf("unexpected organization pagination args: %#v", args)
			}
			return &testutil.MockRows{Rows: []func(...any) error{
				organizationMockRow(2, "Alpha", "active"),
				organizationMockRow(1, "Beta", "disabled"),
			}}, nil
		},
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodGet, "/api/v1/organizations", "", auth.RolePlatformAdmin, 1, 10)
	rec := httptest.NewRecorder()

	h.ListOrganizations(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"data"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 2 || len(response.Data) != 2 {
		t.Fatalf("unexpected organization response: %#v", response)
	}
	if response.Data[0].Name != "Alpha" || response.Data[1].Name != "Beta" {
		t.Fatalf("expected stable name order, got %#v", response.Data)
	}
}

func TestListOrganizationsOrgRoleReadsOnlyActorOrganization(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "WHERE id = $1") {
				t.Fatalf("expected tenant-constrained organization count, got %s", sql)
			}
			if len(args) != 1 || args[0] != int64(7) {
				t.Fatalf("unexpected count args: %#v", args)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "WHERE id = $1") || !strings.Contains(sql, "LIMIT $2 OFFSET $3") {
				t.Fatalf("expected tenant-constrained organization rows, got %s", sql)
			}
			if len(args) != 3 || args[0] != int64(7) || args[1] != 20 || args[2] != 0 {
				t.Fatalf("unexpected organization args: %#v", args)
			}
			return &testutil.MockRows{Rows: []func(...any) error{
				organizationMockRow(7, "Own Org", "active"),
			}}, nil
		},
	}
	h := newTestHandler(db)
	req := p6f16Request(http.MethodGet, "/api/v1/organizations", "", auth.RoleOrgAdmin, 7, 10)
	rec := httptest.NewRecorder()

	h.ListOrganizations(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 1 || len(response.Data) != 1 || response.Data[0].ID != 7 {
		t.Fatalf("unexpected scoped organization response: %#v", response)
	}
}

func organizationMockRow(id int64, name, status string) func(...any) error {
	return func(dest ...any) error {
		*dest[0].(*int64) = id
		*dest[1].(*string) = name
		*dest[2].(*string) = status
		return nil
	}
}
