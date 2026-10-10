//go:build integration

package migrate_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func sourceMarkerDefinition(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var result string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT pg_get_functiondef('mark_micro_ledger_runtime_evidence()'::regprocedure)`).Scan(&result))
	return result
}

func sourceMarkerState(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var result string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT json_build_object('xmin',xmin::text,'written',micro_evidence_written,'first',first_evidence_at)::text FROM micro_ledger_runtime_state WHERE singleton`).Scan(&result))
	return result
}

func TestSourceFactsExpandMarkerExactPredecessorAndRollbackIntegration(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t) // Always a new temporary database.
	dir := testutil.PreRetirementMigrationsDir(t)
	expected := sourceMarkerDefinition(t, pool)
	require.Contains(t, expected, "NOT micro_evidence_written OR first_evidence_at IS NULL")
	require.NoError(t, testutil.RewindEmptySourceFactsForLegacyTest(t.Context(), pool, dir))
	before := sourceMarkerDefinition(t, pool)
	require.NotContains(t, before, "NOT micro_evidence_written")
	state := sourceMarkerState(t, pool)
	var triggers int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_trigger WHERE tgfoid='mark_micro_ledger_runtime_evidence()'::regprocedure AND NOT tgisinternal`).Scan(&triggers))
	require.Equal(t, 11, triggers)
	hooks := sourceExpandHooks(t)
	hooks.After = func(context.Context, pgx.Tx) error { return errors.New("marker expansion rollback") }
	require.ErrorContains(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, hooks), "marker expansion rollback")
	sourceExpandPresent(t, pool, false)
	require.Equal(t, before, sourceMarkerDefinition(t, pool))
	require.Equal(t, state, sourceMarkerState(t, pool))
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, sourceExpandHooks(t)))
	require.Equal(t, expected, sourceMarkerDefinition(t, pool))
	require.Equal(t, state, sourceMarkerState(t, pool))
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, sourceExpandHooks(t)))
	require.Equal(t, state, sourceMarkerState(t, pool))
	require.NoError(t, testutil.RewindEmptySourceFactsForLegacyTest(t.Context(), pool, dir))
	require.Equal(t, before, sourceMarkerDefinition(t, pool), "down restores the exact original function")
	require.Equal(t, state, sourceMarkerState(t, pool), "down never resets the financial marker")
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, sourceExpandHooks(t)))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM pg_trigger WHERE tgfoid='mark_micro_ledger_runtime_evidence()'::regprocedure AND NOT tgisinternal`).Scan(&triggers))
	require.Equal(t, 11, triggers)
	_, err := pool.Exec(t.Context(), `UPDATE wallets SET balance_micros=balance_micros WHERE FALSE`)
	require.NoError(t, err, "the original statement trigger still marks even a zero-row statement")
	down, err := os.ReadFile(filepath.Join(dir, "000036_v1_micro_precision_ledger.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(down))
	require.ErrorContains(t, err, "post-activation micro-ledger evidence exists")
}

func TestSourceFactsExpandMarkerColdAndWarmRepeatableReadIntegration(t *testing.T) {
	for _, firstCommits := range []bool{true, false} {
		name := "first_rolls_back"
		if firstCommits {
			name = "first_commits"
		}
		t.Run(name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			// Prepare the pre-first-evidence state only in this newly created
			// synthetic database. This is never an operational reset path.
			_, err := pool.Exec(t.Context(), `UPDATE micro_ledger_runtime_state SET micro_evidence_written=FALSE,first_evidence_at=NULL WHERE singleton`)
			require.NoError(t, err)
			cold := sourceMarkerState(t, pool)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			a, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			require.NoError(t, err)
			defer a.Rollback(context.Background())
			b, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			require.NoError(t, err)
			defer b.Rollback(context.Background())
			var marked bool
			var bPID int
			require.NoError(t, b.QueryRow(ctx, `SELECT micro_evidence_written,pg_backend_pid() FROM micro_ledger_runtime_state WHERE singleton`).Scan(&marked, &bPID))
			require.False(t, marked)
			_, err = a.Exec(ctx, `UPDATE wallets SET balance_micros=balance_micros WHERE FALSE`)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				_, err := b.Exec(ctx, `UPDATE wallets SET balance_micros=balance_micros WHERE FALSE`)
				done <- err
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := pool.QueryRow(ctx, `SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=$1`, bPID).Scan(&waiting)
				return err == nil && waiting
			}, 2*time.Second, 5*time.Millisecond, "the second old RR snapshot actually waits on the cold marker")
			if firstCommits {
				require.NoError(t, a.Commit(ctx))
				var pgErr *pgconn.PgError
				require.ErrorAs(t, <-done, &pgErr)
				require.Equal(t, "40001", pgErr.Code, "cold old snapshots retain the original serialization contract")
				require.NoError(t, b.Rollback(ctx))
			} else {
				require.NoError(t, a.Rollback(ctx))
				require.Equal(t, cold, sourceMarkerState(t, pool), "uncommitted first evidence rolls back completely")
				require.NoError(t, <-done)
				require.NoError(t, b.Commit(ctx))
			}
			state := sourceMarkerState(t, pool)
			require.NotEqual(t, cold, state)
			var warm []pgx.Tx
			for range 4 {
				tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
				require.NoError(t, err)
				defer tx.Rollback(context.Background())
				require.NoError(t, tx.QueryRow(ctx, `SELECT micro_evidence_written FROM micro_ledger_runtime_state WHERE singleton`).Scan(&marked))
				require.True(t, marked)
				warm = append(warm, tx)
			}
			for _, tx := range warm {
				_, err := tx.Exec(ctx, `UPDATE wallets SET balance_micros=balance_micros WHERE FALSE`)
				require.NoError(t, err)
			}
			for _, tx := range warm {
				require.NoError(t, tx.Commit(ctx))
			}
			require.Equal(t, state, sourceMarkerState(t, pool), "warm transactions neither change xmin nor first-evidence time")
		})
	}
}
