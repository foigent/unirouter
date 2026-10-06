// Package testdb owns the dependency-light PostgreSQL database primitives used
// by integration tests. It deliberately does not install application schema;
// higher-level test helpers must use the same supported migration/publication
// orchestration as the runtime artifact.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func Required() bool {
	required := strings.TrimSpace(os.Getenv("REQUIRE_TEST_DATABASE"))
	return required == "1" || strings.EqualFold(required, "true")
}

// PreflightDSN refuses an implicit local database when the caller requires
// integration tests to run. Call this before opening any connection.
func PreflightDSN() (string, error) {
	if Required() && strings.TrimSpace(os.Getenv("TEST_DATABASE_URL")) == "" {
		return "", errors.New("REQUIRE_TEST_DATABASE requires explicit TEST_DATABASE_URL")
	}
	return DSN(), nil
}

type versionReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// PreflightServer runs before CREATE DATABASE or TRUNCATE in required mode.
func PreflightServer(ctx context.Context, server versionReader) error {
	if !Required() {
		return nil
	}
	var version int
	if err := server.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&version); err != nil {
		return fmt.Errorf("read required test database version: %w", err)
	}
	if version/10000 != 16 {
		return fmt.Errorf("required test database is PostgreSQL %d; expected PostgreSQL 16", version/10000)
	}
	return nil
}

// SetupIsolated creates one empty temporary PostgreSQL database.
func SetupIsolated(t *testing.T) *pgxpool.Pool {
	t.Helper()
	baseDSN, err := PreflightDSN()
	require.NoError(t, err)
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
	if err := PreflightServer(ctx, adminPool); err != nil {
		adminPool.Close()
		Unavailable(t, "%v", err)
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
	started := time.Now()
	conn, err := adminPool.Acquire(ctx)
	if err != nil {
		return newDropError("acquire", started, err)
	}
	defer conn.Release()
	started = time.Now()
	_, err = conn.Exec(ctx, `DROP DATABASE IF EXISTS `+QuoteIdent(dbName))
	if err != nil {
		return newDropError("execute", started, err)
	}
	return nil
}

// Drop diagnostics preserve the original deadline and error chain, but never
// include connection strings, server error detail or SQL parameters in logs.
type dropError struct {
	phase   string
	elapsed time.Duration
	cause   error
}

func newDropError(phase string, started time.Time, cause error) error {
	return &dropError{phase: phase, elapsed: time.Since(started), cause: cause}
}

func (e *dropError) Error() string {
	class, sqlstate := "other", "none"
	var pgerr *pgconn.PgError
	switch {
	case errors.As(e.cause, &pgerr):
		class, sqlstate = "postgres", pgerr.Code
	case errors.Is(e.cause, context.DeadlineExceeded):
		class = "deadline"
	case errors.Is(e.cause, context.Canceled):
		class = "canceled"
	}
	return fmt.Sprintf("phase=%s elapsed=%s class=%s sqlstate=%s", e.phase, e.elapsed, class, sqlstate)
}

func (e *dropError) Unwrap() error { return e.cause }

func Unavailable(t *testing.T, format string, args ...any) {
	t.Helper()
	if Required() {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}
