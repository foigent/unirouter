//go:build integration

package migrate_test

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

const migrationAdvisoryLockIDForTest int64 = 86420517831702491

func TestV1CommercialReleaseControlMigrationAndConcurrentResourceLimit_LedgerFailureRollsBackDDL(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := migrate.Run(ctx, pool, migrationFixtureDir(t, "ledger_failure"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "record")

	var relation string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(to_regclass('migration_ledger_atomicity_probe')::text, '')`,
	).Scan(&relation))
	require.Empty(t, relation, "migration DDL must roll back when the ledger row cannot be written")

	var ledgerRows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&ledgerRows))
	require.Zero(t, ledgerRows)
}

func TestV1CommercialReleaseControlMigrationAndConcurrentResourceLimit_ConcurrentRunAppliesOnceAndReleasesLock(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir := migrationFixtureDir(t, "concurrent")

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errs <- migrate.Run(ctx, pool, dir)
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var probeRows, ledgerRows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM migration_concurrency_probe`).Scan(&probeRows))
	require.Equal(t, 1, probeRows, "the migration body must execute exactly once")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = '000001_explicit_transaction_fixture'`,
	).Scan(&ledgerRows))
	require.Equal(t, 1, ledgerRows)

	// Use a separate pool/session so a leaked session-scoped advisory lock cannot
	// be mistaken for PostgreSQL's same-session re-entrant lock behavior.
	observerPool, err := pgxpool.New(ctx, pool.Config().ConnString())
	require.NoError(t, err)
	defer observerPool.Close()
	observer, err := observerPool.Acquire(ctx)
	require.NoError(t, err)
	defer observer.Release()
	var acquired bool
	require.NoError(t, observer.QueryRow(ctx,
		`SELECT pg_try_advisory_lock($1)`, migrationAdvisoryLockIDForTest,
	).Scan(&acquired))
	require.True(t, acquired, "Run must release its session-scoped migration lock")
	var released bool
	require.NoError(t, observer.QueryRow(ctx,
		`SELECT pg_advisory_unlock($1)`, migrationAdvisoryLockIDForTest,
	).Scan(&released))
	require.True(t, released)
}

func TestV1CommercialReleaseControlMigrationAndConcurrentResourceLimit_CanceledLockWaitDoesNotPoisonPool(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	holder, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer holder.Release()
	_, err = holder.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLockIDForTest)
	require.NoError(t, err)

	runnerConfig := pool.Config().Copy()
	runnerConfig.MaxConns = 1
	runnerPool, err := pgxpool.NewWithConfig(ctx, runnerConfig)
	require.NoError(t, err)
	defer runnerPool.Close()

	waitCtx, cancelWait := context.WithTimeout(ctx, 100*time.Millisecond)
	err = migrate.Run(waitCtx, runnerPool, migrationFixtureDir(t, "concurrent"))
	cancelWait()
	require.Error(t, err)
	require.Contains(t, err.Error(), "acquire migration advisory lock")

	var released bool
	require.NoError(t, holder.QueryRow(ctx,
		`SELECT pg_advisory_unlock($1)`, migrationAdvisoryLockIDForTest,
	).Scan(&released))
	require.True(t, released)

	// The failed acquisition session is hijacked and closed, so a one-connection
	// pool must be able to establish a clean replacement and migrate normally.
	require.NoError(t, migrate.Run(ctx, runnerPool, migrationFixtureDir(t, "concurrent")))
}

func migrationFixtureDir(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}
