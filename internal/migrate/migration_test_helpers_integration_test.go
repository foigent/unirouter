//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

// setupMigratedThrough gives a historical migration test the schema that
// existed at its asserted boundary. Later migrations may rename the columns
// under test and must not silently weaken the older up/down contract.
func setupMigratedThrough(t *testing.T, maxUpFile string) (*pgxpool.Pool, string) {
	t.Helper()
	pool := testutil.SetupIsolatedTestDB(t)
	migrationsDir := migrationsThrough(t, testutil.MigrationsDir(t), maxUpFile)
	require.NoError(t, migrate.Run(context.Background(), pool, migrationsDir))
	testutil.TruncateAll(t, pool)
	return pool, migrationsDir
}

func migrationsThrough(t *testing.T, sourceDir, maxUpFile string) string {
	t.Helper()
	targetDir := t.TempDir()
	entries, err := os.ReadDir(sourceDir)
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") || name > maxUpFile {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(sourceDir, name))
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, name), body, 0o600))
	}
	return targetDir
}
