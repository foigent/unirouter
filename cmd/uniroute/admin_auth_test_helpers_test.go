package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

func activeAdminToken(t testing.TB, userID, orgID int64, role, secret string) string {
	t.Helper()
	token, _, err := auth.GenerateAdminSessionJWT(userID, orgID, role, 0, secret, 1)
	if err != nil {
		t.Fatalf("generate admin session JWT: %v", err)
	}
	return token
}

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
