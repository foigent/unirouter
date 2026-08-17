package customercontext

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestResolveCustomerContextStates(t *testing.T) {
	tests := []struct {
		name          string
		count         int64
		selectedID    int64
		selectedName  string
		selectedState string
		want          State
		wantOrgID     int64
		wantOrgName   string
	}{
		{name: "not initialized", want: StateNotInitialized},
		{name: "ambiguous explicit customers", count: 2, selectedID: 9, selectedName: "ignored", selectedState: "active", want: StateAmbiguous},
		{name: "ambiguous legacy roots", count: 2, selectedID: 9, selectedName: "ignored", selectedState: "active", want: StateAmbiguous},
		{name: "inactive explicit customer", count: 1, selectedID: 7, selectedName: "Customer", selectedState: "disabled", want: StateInactive, wantOrgID: 7, wantOrgName: "Customer"},
		{name: "ready explicit customer", count: 1, selectedID: 8, selectedName: "Customer", selectedState: "active", want: StateReady, wantOrgID: 8, wantOrgName: "Customer"},
		{name: "ready sole legacy root", count: 1, selectedID: 1, selectedName: "Platform Admin", selectedState: "active", want: StateReady, wantOrgID: 1, wantOrgName: "Platform Admin"},
		{name: "inactive sole legacy root", count: 1, selectedID: 1, selectedName: "Platform Admin", selectedState: "disabled", want: StateInactive, wantOrgID: 1, wantOrgName: "Platform Admin"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*(dest[0].(*int64)) = test.count
					*(dest[1].(*int64)) = test.selectedID
					*(dest[2].(*string)) = test.selectedName
					*(dest[3].(*string)) = test.selectedState
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

func TestEnsureBaselineKeepsDeploymentOperatorsOutOfProjectMembership(t *testing.T) {
	var executed []string
	tx := &testutil.MockTx{
		ExecFn: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			executed = append(executed, sql)
			return pgconn.CommandTag{}, nil
		},
		QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*(dest[0].(*int64)) = 37
				return nil
			}}
		},
	}
	database := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*(dest[0].(*int64)) = 1
				*(dest[1].(*int64)) = 1
				*(dest[2].(*string)) = "Platform Admin"
				*(dest[3].(*string)) = "active"
				return nil
			}}
		},
		BeginFn: func(context.Context) (pgx.Tx, error) { return tx, nil },
	}

	baseline, err := EnsureBaseline(context.Background(), database)
	if err != nil {
		t.Fatalf("EnsureBaseline() error = %v", err)
	}
	if baseline.ProjectID != 37 {
		t.Fatalf("ProjectID = %d, want 37", baseline.ProjectID)
	}
	joined := strings.Join(executed, "\n")
	if !strings.Contains(joined, "DELETE FROM project_members") || !strings.Contains(joined, "u.organization_role='platform_admin'") {
		t.Fatalf("baseline did not remove deployment operator membership: %s", joined)
	}
	if !strings.Contains(joined, "u.organization_role='org_admin'") || strings.Contains(joined, "IN ('platform_admin','org_admin')") {
		t.Fatalf("baseline still assigns deployment operators to the default project: %s", joined)
	}
}
