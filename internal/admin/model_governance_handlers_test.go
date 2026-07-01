package admin

import (
	"context"
	"encoding/json"
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

func TestCreateModelRegistry_InvalidLifecycle(t *testing.T) {
	h := newTestHandler(&testutil.MockDB{})

	req := httptest.NewRequest(http.MethodPost, "/model-registry", strings.NewReader(`{"org_id":1,"model_name":"gpt-4o","lifecycle":"live"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateModelRegistry(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
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
	rec := httptest.NewRecorder()

	h.CreateModelSet(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
