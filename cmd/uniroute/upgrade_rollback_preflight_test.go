package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestLegacyRollbackPreflightRequiresPurposeCredentialDisablement(t *testing.T) {
	for _, test := range []struct {
		name    string
		count   int
		wantErr string
	}{
		{name: "no active purpose credential", count: 0},
		{
			name:    "active feed credential",
			count:   1,
			wantErr: "stop feed consumption and disable the exact feed credential",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := &testutil.MockDB{QueryRowFn: func(_ context.Context, sql string, _ ...any) pgx.Row {
				if !strings.Contains(sql, "purpose <> ''") ||
					!strings.Contains(sql, "'feed_active'") ||
					!strings.Contains(sql, "'feed_staged'") {
					t.Fatalf("rollback preflight did not query the purpose-active compatibility boundary: %s", sql)
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int) = test.count
					return nil
				}}
			}}
			err := preflightLegacyPurposeRollback(context.Background(), database)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected rollback preflight error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("rollback preflight error=%v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestLegacyRollbackPreflightFailsClosedWhenStateCannotBeRead(t *testing.T) {
	database := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(...any) error {
			return errors.New("database unavailable")
		}}
	}}
	err := preflightLegacyPurposeRollback(context.Background(), database)
	if err == nil || !strings.Contains(err.Error(), "read active purpose-scoped credential count") {
		t.Fatalf("unexpected rollback preflight error: %v", err)
	}
}
