package main

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/testutil"
)

func activeAdminPrincipalDB() *testutil.MockDB {
	return &testutil.MockDB{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
		if !strings.Contains(sql, "SELECT EXISTS") || !strings.Contains(sql, "FROM users") {
			return &testutil.MockRow{}
		}
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*bool) = true
			return nil
		}}
	}}
}
