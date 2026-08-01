//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func rollbackAsyncMediaImageMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t),
		"000046_v1_async_media_image_jobs.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), string(downSQL))
	require.NoError(t, err)
}
