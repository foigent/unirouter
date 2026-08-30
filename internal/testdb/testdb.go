// Package testdb owns the dependency-light PostgreSQL database primitives used
// by integration tests. It deliberately does not install application schema;
// higher-level test helpers must use the same supported migration/publication
// orchestration as the runtime artifact.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// DSN returns the integration-test database DSN.
func DSN() string {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://uniroute:uniroute@localhost:5432/uniroute_test?sslmode=disable"
	}
	return dsn
}

// SetupIsolated creates one empty temporary PostgreSQL database.
func SetupIsolated(t *testing.T) *pgxpool.Pool {
	t.Helper()
	baseDSN := DSN()
	adminDSN, tempDSN, dbName, err := TempDatabaseDSNs(baseDSN)
	require.NoError(t, err, "build temporary database DSN")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		Unavailable(t, "cannot connect to PostgreSQL admin database: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		Unavailable(t, "cannot ping PostgreSQL admin database: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+QuoteIdent(dbName)); err != nil {
		adminPool.Close()
		Unavailable(t, "cannot create temporary test database %s: %v", dbName, err)
	}

	pool, err := pgxpool.New(ctx, tempDSN)
	if err != nil {
		_ = Drop(ctx, adminPool, dbName)
		adminPool.Close()
		Unavailable(t, "cannot connect to temporary test database %s: %v", dbName, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		_ = Drop(ctx, adminPool, dbName)
		adminPool.Close()
		Unavailable(t, "cannot ping temporary test database %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		pool.Close()
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if err := Drop(dropCtx, adminPool, dbName); err != nil {
			t.Errorf("drop temporary test database %s: %v", dbName, err)
		}
		adminPool.Close()
	})
	return pool
}

// MigrationsDir returns the repository migration directory.
func MigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "resolve testdb path")
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
}

func TempDatabaseDSNs(baseDSN string) (adminDSN, tempDSN, dbName string, err error) {
	parsed, err := url.Parse(baseDSN)
	if err != nil {
		return "", "", "", err
	}
	dbName = fmt.Sprintf("uniroute_it_%d_%d", os.Getpid(), time.Now().UnixNano())
	admin := *parsed
	admin.Path = "/postgres"
	temp := *parsed
	temp.Path = "/" + dbName
	return admin.String(), temp.String(), dbName, nil
}

func QuoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func Drop(ctx context.Context, adminPool *pgxpool.Pool, dbName string) error {
	_, err := adminPool.Exec(ctx, `DROP DATABASE IF EXISTS `+QuoteIdent(dbName))
	return err
}

func Unavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	required := strings.TrimSpace(os.Getenv("REQUIRE_TEST_DATABASE"))
	if required == "1" || strings.EqualFold(required, "true") {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}
