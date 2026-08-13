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

func TestCreateModelRegistry_InvalidLifecycle(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPost, "/model-registry", strings.NewReader(`{"org_id":1,"model_name":"gpt-4o","lifecycle":"live"}`))
	req.Header.Set("Content-Type", "application/json")
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.CreateModelRegistry(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestContextLengthInputJSONContract(t *testing.T) {
	valid := []struct {
		json string
		want int
	}{
		{json: `0`, want: 0},
		{json: `131072`, want: 131072},
		{json: `2147483647`, want: 2147483647},
	}
	for _, testCase := range valid {
		t.Run("accepts_"+testCase.json, func(t *testing.T) {
			var value contextLengthInput
			if err := json.Unmarshal([]byte(testCase.json), &value); err != nil {
				t.Fatalf("expected exact integer to be accepted: %v", err)
			}
			if int(value) != testCase.want {
				t.Fatalf("expected %d, got %d", testCase.want, value)
			}
		})
	}

	for _, input := range []string{`2147483648`, `1.5`, `1e3`, `-1`, `"128K"`, `"131072"`, `null`} {
		t.Run("rejects_"+input, func(t *testing.T) {
			var value contextLengthInput
			if err := json.Unmarshal([]byte(input), &value); !errors.Is(err, errInvalidContextLength) {
				t.Fatalf("expected stable context-length error for %s, got %v", input, err)
			}
		})
	}
}

func TestModelRegistryContextLengthValidationIsStableAndPreDatabase(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "create overflow", method: http.MethodPost, target: "/model-registry", body: `{"org_id":1,"model_name":"overflow","context_length":2147483648}`},
		{name: "create fractional", method: http.MethodPost, target: "/model-registry", body: `{"org_id":1,"model_name":"fraction","context_length":1.5}`},
		{name: "create string shorthand", method: http.MethodPost, target: "/model-registry", body: `{"org_id":1,"model_name":"shorthand","context_length":"128K"}`},
		{name: "update scientific notation", method: http.MethodPut, target: "/model-registry/10", body: `{"context_length":1e3}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			beginCalled := false
			db := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
				beginCalled = true
				return nil, errors.New("database must not be reached")
			}}
			h := newTestHandler(db)
			req := httptest.NewRequest(testCase.method, testCase.target, strings.NewReader(testCase.body))
			req.Header.Set("Content-Type", "application/json")
			if testCase.method == http.MethodPut {
				req.SetPathValue("id", "10")
			}
			req = platformModelGovernanceRequest(req)
			rec := httptest.NewRecorder()

			if testCase.method == http.MethodPost {
				h.CreateModelRegistry(rec, req)
			} else {
				h.UpdateModelRegistry(rec, req)
			}

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if beginCalled {
				t.Fatal("invalid context_length must be rejected before PostgreSQL")
			}
			var response map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response["code"] != contextLengthErrorCode || response["field"] != "context_length" || response["reason_code"] != contextLengthReasonCode {
				t.Fatalf("unexpected stable validation response: %#v", response)
			}
		})
	}
}

func TestCreateModelRegistryAcceptsMaximumContextLengthBeforeDatabase(t *testing.T) {
	beginCalled := false
	db := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) {
		beginCalled = true
		return nil, errors.New("deliberate database stop")
	}}
	h := newTestHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/model-registry", strings.NewReader(`{"org_id":1,"model_name":"max-context","context_length":2147483647}`))
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.CreateModelRegistry(rec, req)

	if !beginCalled {
		t.Fatal("maximum PostgreSQL INTEGER token count should reach the mutation transaction")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected deliberate database stop after validation, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateModelRegistry_LifecycleAuditBeforeAfter(t *testing.T) {
	var auditArgs []any
	lockedBeforeRead := false
	tx := &testutil.MockTx{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			lockedBefore := strings.Contains(sql, "FOR UPDATE")
			lockedBeforeRead = lockedBeforeRead || lockedBefore
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 1
				*dest[1].(*string) = "gpt-4o"
				*dest[2].(*string) = "GPT-4o"
				*dest[3].(*string) = "openai"
				*dest[4].(*string) = "ga"
				*dest[5].(*int) = 128000
				*dest[6].(*int) = 16384
				*dest[7].(*string) = "chat,vision"
				*dest[8].(*string) = "global"
				*dest[9].(*string) = "gpt-4o"
				*dest[10].(*bool) = true
				*dest[11].(*string) = ""
				if !lockedBefore {
					*dest[4].(*string) = "deprecated"
					*dest[11].(*string) = "Use company-chat-v2"
				}
				return nil
			}}
		},
		ExecFn: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "INSERT INTO audit_logs") {
				auditArgs = append([]any(nil), args...)
				return pgconn.CommandTag{}, nil
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}
	db := &testutil.MockDB{BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil }}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodPut, "/model-registry/10", strings.NewReader(`{"lifecycle":"deprecated","migration_recommendation":"Use company-chat-v2"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "audit-test")
	req.SetPathValue("id", "10")
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(7))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.UpdateModelRegistry(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(auditArgs) != 14 {
		t.Fatalf("expected 14 audit args, got %#v", auditArgs)
	}
	if !lockedBeforeRead {
		t.Fatal("model registry before state must be locked in the mutation transaction")
	}
	if auditArgs[1] != int64(7) || auditArgs[9] != "admin_console" {
		t.Fatalf("expected actor/source in audit args, got %#v", auditArgs)
	}
	if !strings.Contains(auditArgs[11].(string), `"lifecycle":"ga"`) {
		t.Fatalf("expected before lifecycle ga, got %v", auditArgs[11])
	}
	if !strings.Contains(auditArgs[12].(string), `"lifecycle":"deprecated"`) {
		t.Fatalf("expected after lifecycle deprecated, got %v", auditArgs[12])
	}
}

func TestListApprovedModelCatalog_FiltersApprovedLifecycle(t *testing.T) {
	now := time.Now()
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "approved_catalog = TRUE") || !strings.Contains(sql, "mr.lifecycle IN ('ga', 'deprecated')") {
				t.Fatalf("catalog count query missing approved lifecycle filters: %s", sql)
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "approved_catalog = TRUE") || !strings.Contains(sql, "mr.lifecycle IN ('ga', 'deprecated')") {
				t.Fatalf("catalog query missing approved lifecycle filters: %s", sql)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*int64) = 10
						*dest[1].(*int64) = 1
						*dest[2].(*string) = "gpt-4o"
						*dest[3].(*string) = "GPT-4o"
						*dest[4].(*string) = "openai"
						*dest[5].(*string) = "ga"
						*dest[6].(*int) = 128000
						*dest[7].(*int) = 4096
						*dest[8].(*string) = "chat,vision"
						*dest[9].(*string) = "global"
						*dest[10].(*string) = "gpt-4o"
						*dest[11].(*bool) = true
						*dest[12].(*string) = ""
						*dest[13].(*time.Time) = now
						*dest[14].(*time.Time) = now
						*dest[15].(*string) = "company-chat-fast"
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/model-catalog?org_id=1", nil)
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.ListApprovedModelCatalog(rec, req)

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

func TestListApprovedModelCatalog_RoutableOnlyRequiresStableActiveRelease(t *testing.T) {
	const stableActiveFilter = "JOIN runtime_change_releases rcr ON rcr.id = rcs.stable_release_id AND rcr.stage = 'active'"
	countChecked := false
	listChecked := false
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, stableActiveFilter) || !strings.Contains(sql, "rcs.model_name = mr.model_name") {
				t.Fatalf("routable catalog count query missing stable active exact-model filter: %s", sql)
			}
			countChecked = true
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int) = 0
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, stableActiveFilter) || !strings.Contains(sql, "rcs.model_name = mr.model_name") {
				t.Fatalf("routable catalog list query missing stable active exact-model filter: %s", sql)
			}
			listChecked = true
			return &testutil.MockRows{}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/model-catalog?org_id=1&routable_only=true", nil)
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.ListApprovedModelCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !countChecked || !listChecked {
		t.Fatalf("expected routable filter in both count and list queries: count=%v list=%v", countChecked, listChecked)
	}
}

func TestReplaceProjectModelSets_DeveloperForbidden(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPut, "/projects/42/model-sets", strings.NewReader(`{"model_set_ids":[1]}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "42")
	ctx := context.WithValue(req.Context(), middleware.CtxUserRole, auth.RoleDeveloper)
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(7))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ReplaceProjectModelSets(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateModelSet_InvalidItem(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPost, "/model-sets", strings.NewReader(`{"org_id":1,"name":"Chat","items":[{"item_type":"provider","item_name":"gpt-4o"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.CreateModelSet(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListModelSetItems_ReturnsOrgScopedItems(t *testing.T) {
	db := &testutil.MockDB{
		QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*int64) = 1
				return nil
			}}
		},
		QueryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "JOIN model_sets ms") || !strings.Contains(sql, "ms.org_id = $2") {
				t.Fatalf("expected org-scoped model set item query, got %s", sql)
			}
			if args[0] != int64(10) || args[1] != int64(1) {
				t.Fatalf("unexpected args: %#v", args)
			}
			return &testutil.MockRows{
				Rows: []func(dest ...any) error{
					func(dest ...any) error {
						*dest[0].(*string) = "alias"
						*dest[1].(*string) = "company-chat"
						return nil
					},
				},
			}, nil
		},
	}
	h := newTestHandler(db)

	req := httptest.NewRequest(http.MethodGet, "/model-sets/10/items?org_id=1", nil)
	req.SetPathValue("id", "10")
	req = platformModelGovernanceRequest(req)
	rec := httptest.NewRecorder()

	h.ListModelSetItems(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	items := resp["data"].([]interface{})
	if len(items) != 1 || items[0].(map[string]interface{})["item_name"] != "company-chat" {
		t.Fatalf("unexpected items: %#v", resp["data"])
	}
}

func platformModelGovernanceRequest(req *http.Request) *http.Request {
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RolePlatformAdmin)
	return req.WithContext(ctx)
}
