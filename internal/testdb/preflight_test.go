package testdb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type versionRow struct {
	version int
	err     error
}

func (row versionRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*int) = row.version
	return nil
}

type versionProbe struct {
	queries []string
	row     versionRow
}

func (probe *versionProbe) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	probe.queries = append(probe.queries, sql)
	return probe.row
}

func TestRequiredDatabasePreflightRejectsImplicitDSN(t *testing.T) {
	t.Setenv("REQUIRE_TEST_DATABASE", "1")
	t.Setenv("TEST_DATABASE_URL", "")
	dsn, err := PreflightDSN()
	require.Empty(t, dsn)
	require.ErrorContains(t, err, "explicit TEST_DATABASE_URL")
	// SetupIsolated and SetupTestDB both call this before opening a pool.
}

func TestRequiredDatabasePreflightRejectsWrongServerBeforeWrite(t *testing.T) {
	t.Setenv("REQUIRE_TEST_DATABASE", "1")
	probe := &versionProbe{row: versionRow{version: 180003}}
	err := PreflightServer(context.Background(), probe)
	require.ErrorContains(t, err, "PostgreSQL 18")
	require.Equal(t, []string{"SELECT current_setting('server_version_num')::int"}, probe.queries)

	probe.row = versionRow{version: 160014}
	require.NoError(t, PreflightServer(context.Background(), probe))
	probe.row = versionRow{err: errors.New("read denied")}
	require.ErrorContains(t, PreflightServer(context.Background(), probe), "read denied")
}

func TestOptionalDatabasePreflightKeepsExistingFallback(t *testing.T) {
	t.Setenv("REQUIRE_TEST_DATABASE", "")
	t.Setenv("TEST_DATABASE_URL", "")
	dsn, err := PreflightDSN()
	require.NoError(t, err)
	require.Equal(t, DSN(), dsn)
	probe := &versionProbe{row: versionRow{version: 180003}}
	require.NoError(t, PreflightServer(context.Background(), probe))
	require.Empty(t, probe.queries)
}

func TestRequiredDatabasePreflightIsWiredBeforeWrites(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	read := func(path, start, end string) string {
		wire, err := os.ReadFile(filepath.Join(filepath.Dir(file), path))
		require.NoError(t, err)
		body := strings.SplitN(string(wire), start, 2)
		require.Len(t, body, 2)
		return strings.SplitN(body[1], end, 2)[0]
	}
	assertOrder := func(body string, markers ...string) {
		last := -1
		for _, marker := range markers {
			index := strings.Index(body, marker)
			require.Greater(t, index, last, marker)
			last = index
		}
	}
	isolated := read("testdb.go", "func SetupIsolated(", "// MigrationsDir")
	assertOrder(isolated, "PreflightDSN()", "pgxpool.New(ctx, adminDSN)", "PreflightServer(ctx, adminPool)", "CREATE DATABASE")
	shared := read("../testutil/testdb.go", "func SetupTestDB(", "// SetupIsolatedTestDB")
	assertOrder(shared, "PreflightDSN()", "pgxpool.New(ctx, dsn)", "PreflightServer(ctx, pool)", "TruncateAll(t, pool)")
	template := read("../testutil/testdb.go", "func setupMigratedTestDBFromTemplate(", "func ensureMigratedTestDBTemplate(")
	assertOrder(template, "PreflightDSN()", "ensureMigratedTestDBTemplate(t, baseDSN, ceiling)", "PreflightServer(ctx, adminPool)", "CREATE DATABASE")
	admin := read("../testutil/testdb.go", "func ensureMigratedTestDBTemplate(", "func testDatabaseUnavailable(")
	assertOrder(admin, "pgxpool.New(ctx, adminDSN)", "PreflightServer(ctx, adminPool)", "CREATE DATABASE")
}
