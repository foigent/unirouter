package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCustomerContextInjectsReadySnapshot(t *testing.T) {
	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*(dest[0].(*int64)) = 1
			*(dest[1].(*int64)) = 42
			*(dest[2].(*string)) = "Acme"
			*(dest[3].(*string)) = "active"
			return nil
		}}
	}}

	var got customercontext.Snapshot
	var gotOrgID int64
	var gotState customercontext.State
	handler := CustomerContext(database)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = GetCustomerContextFromContext(r.Context())
		gotOrgID = GetCustomerOrgIDFromContext(r.Context())
		gotState = GetCustomerContextStateFromContext(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got.State != customercontext.StateReady || got.OrganizationID != 42 || got.OrganizationName != "Acme" || gotOrgID != 42 || gotState != customercontext.StateReady {
		t.Fatalf("unexpected context: snapshot=%+v org_id=%d state=%q", got, gotOrgID, gotState)
	}
}

func TestCustomerContextInjectsUnavailableWithoutRejecting(t *testing.T) {
	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(...any) error { return errors.New("database down") }}
	}}

	called := false
	handler := CustomerContext(database)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		snapshot, ok := GetCustomerContextFromContext(r.Context())
		if !ok || snapshot.State != customercontext.StateUnavailable {
			t.Fatalf("unexpected snapshot: %+v ok=%v", snapshot, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called || recorder.Code != http.StatusNoContent {
		t.Fatalf("deployment route was rejected: called=%v status=%d", called, recorder.Code)
	}
}
