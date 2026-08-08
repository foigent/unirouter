package admin

import (
	"bytes"
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

func TestAPIKeyObjectLookupMasksExistenceAndPurposeFromInaccessibleActors(t *testing.T) {
	type operation struct {
		name   string
		method string
		body   string
		invoke func(*Handler, http.ResponseWriter, *http.Request)
	}
	operations := []operation{
		{name: "get", method: http.MethodGet, invoke: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.GetAPIKey(w, r) }},
		{name: "update", method: http.MethodPut, body: `{"name":"valid-name"}`, invoke: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.UpdateAPIKey(w, r) }},
		{name: "disable", method: http.MethodPost, invoke: func(h *Handler, w http.ResponseWriter, r *http.Request) { h.DisableAPIKey(w, r) }},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			absent := serveAPIKeyObjectLookup(t, operation.method, operation.body, false, "", operation.invoke)
			inaccessible := serveAPIKeyObjectLookup(t, operation.method, operation.body, true, "", operation.invoke)
			inaccessibleFeed := serveAPIKeyObjectLookup(t, operation.method, operation.body, true, middleware.APIKeyPurposeXInsightFeedRead, operation.invoke)

			for label, response := range map[string]*httptest.ResponseRecorder{
				"absent":            absent,
				"inaccessible":      inaccessible,
				"inaccessible_feed": inaccessibleFeed,
			} {
				if response.Code != http.StatusNotFound {
					t.Fatalf("%s %s status=%d body=%s; want stable not found", operation.name, label, response.Code, response.Body.String())
				}
				var payload map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatalf("decode %s %s response: %v", operation.name, label, err)
				}
				if payload["error"] != "API key not found" {
					t.Fatalf("%s %s semantic body=%#v", operation.name, label, payload)
				}
			}
			if inaccessible.Body.String() != absent.Body.String() || inaccessibleFeed.Body.String() != absent.Body.String() {
				t.Fatalf("%s lookup responses differ: absent=%q inaccessible=%q feed=%q", operation.name, absent.Body.String(), inaccessible.Body.String(), inaccessibleFeed.Body.String())
			}
		})
	}
}

func serveAPIKeyObjectLookup(
	t *testing.T,
	method, body string,
	present bool,
	purpose string,
	invoke func(*Handler, http.ResponseWriter, *http.Request),
) *httptest.ResponseRecorder {
	t.Helper()
	db := &testutil.MockDB{QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			// The corrected handlers constrain the initial lookup in SQL. Simulate
			// PostgreSQL applying that predicate; the vulnerable global lookup has
			// no actor-org predicate and therefore still exposes the row to the
			// handler, making this regression fail before the fix.
			if !present || strings.Contains(sql, ".org_id = $2") {
				return pgx.ErrNoRows
			}
			switch len(dest) {
			case 7: // UpdateAPIKey pre-read.
				*dest[0].(*int64), *dest[1].(*int64), *dest[2].(*int64) = 2, 22, 0
				*dest[3].(*string), *dest[4].(*string) = "service", purpose
				*dest[5].(*string), *dest[6].(*string) = "", ""
			case 8: // DisableAPIKey pre-read.
				*dest[0].(*int64), *dest[1].(*int64), *dest[2].(*int64) = 2, 22, 0
				*dest[3].(*string), *dest[4].(*string), *dest[5].(*string) = "foreign", "ur-foreign", "active"
				*dest[6].(*string), *dest[7].(*string) = purpose, "service"
			case 19: // GetAPIKey full read.
				*dest[0].(*int64), *dest[1].(*int64), *dest[2].(*int64) = 7, 2, 22
				*dest[3].(*string), *dest[4].(*string), *dest[5].(*string) = "foreign", "ur-foreign", "active"
				*dest[6].(*int), *dest[7].(*int), *dest[8].(*int) = 60, 10, 0
				*dest[9].(*string), *dest[10].(*string) = "", ""
				*dest[11].(*time.Time) = time.Unix(1, 0).UTC()
				*dest[12].(*string), *dest[13].(*string) = "foreign-org", "foreign-project"
				*dest[14].(*int64) = 0
				*dest[15].(*string), *dest[16].(*string), *dest[17].(*string), *dest[18].(*string) = "service", "", "", purpose
			default:
				t.Fatalf("unexpected API-key lookup scan width %d for SQL %s", len(dest), sql)
			}
			return nil
		}}
	}}

	handler := newTestHandler(db)
	request := httptest.NewRequest(method, "/api/v1/keys/7", bytes.NewBufferString(body))
	request.SetPathValue("id", "7")
	ctx := context.WithValue(request.Context(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxUserID, int64(101))
	ctx = context.WithValue(ctx, middleware.CtxUserRole, auth.RoleOrgViewer)
	ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, auth.RoleOrgViewer)
	response := httptest.NewRecorder()
	invoke(handler, response, request.WithContext(ctx))
	return response
}
