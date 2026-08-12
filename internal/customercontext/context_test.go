package customercontext

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestResolveCustomerContextStates(t *testing.T) {
	tests := []struct {
		name        string
		count       int64
		orgID       int64
		orgName     string
		status      string
		want        State
		wantOrgID   int64
		wantOrgName string
	}{
		{name: "not initialized", count: 0, want: StateNotInitialized},
		{name: "ambiguous", count: 2, orgID: 9, orgName: "ignored", status: "active", want: StateAmbiguous},
		{name: "inactive", count: 1, orgID: 7, orgName: "Customer", status: "disabled", want: StateInactive, wantOrgID: 7, wantOrgName: "Customer"},
		{name: "ready", count: 1, orgID: 8, orgName: "Customer", status: "active", want: StateReady, wantOrgID: 8, wantOrgName: "Customer"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*(dest[0].(*int64)) = test.count
					*(dest[1].(*int64)) = test.orgID
					*(dest[2].(*string)) = test.orgName
					*(dest[3].(*string)) = test.status
					return nil
				}}
			}}
			snapshot, err := Resolve(context.Background(), database)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if snapshot.State != test.want || snapshot.OrganizationID != test.wantOrgID || snapshot.OrganizationName != test.wantOrgName {
				t.Fatalf("Resolve() = %+v, want state=%q org_id=%d org_name=%q", snapshot, test.want, test.wantOrgID, test.wantOrgName)
			}
		})
	}
}

func TestResolveCustomerContextUnavailable(t *testing.T) {
	snapshot, err := Resolve(context.Background(), nil)
	if err == nil || snapshot.State != StateUnavailable {
		t.Fatalf("expected unavailable state and error, got state=%q err=%v", snapshot.State, err)
	}

	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(...any) error { return errors.New("database down") }}
	}}
	snapshot, err = Resolve(context.Background(), database)
	if err == nil || snapshot.State != StateUnavailable {
		t.Fatalf("expected database error to be unavailable, got state=%q err=%v", snapshot.State, err)
	}
}
