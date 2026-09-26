//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

// Pool identity/configuration/lifetime only: no HTTP, Redis or load generator.
// Execute against an authorized isolated PG environment, never a live database.
func TestContentPerformanceRuntimePoolPG(t *testing.T) {
	observer := testutil.SetupMigratedTestDB(t)
	for _, tc := range []struct {
		name, maxEnv, minEnv string
		max, min             int32
	}{
		{"runtime_defaults", "", "", 25, 5},
		{"explicit_small_pool", "4", "0", 4, 0},
		{"runtime_defaults_diagnostics", "", "", 25, 5},
	} {
		var clone string
		var business *pgxpool.Pool
		ownerCleanupRan := false
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DB_MAX_CONNECTIONS", tc.maxEnv)
			t.Setenv("DB_MAX_IDLE_CONNECTIONS", tc.minEnv)
			preparation := testutil.SetupMigratedTestDB(t)
			var beforeOID, afterOID uint32
			require.NoError(t, preparation.QueryRow(t.Context(), `SELECT current_database(),oid FROM pg_database WHERE datname=current_database()`).Scan(&clone, &beforeOID))
			var diagnostic *contentPerformancePG
			if tc.name == "runtime_defaults_diagnostics" {
				diagnostic = newContentPerformancePG()
			}
			business = contentPerformanceRuntimePool(t, preparation, diagnostic)
			unexpected, err := preparation.Acquire(t.Context())
			if unexpected != nil {
				unexpected.Release()
			}
			require.Error(t, err, "preparation pool is closed before business traffic")
			var actualClone string
			require.NoError(t, business.QueryRow(t.Context(), `SELECT current_database(),oid FROM pg_database WHERE datname=current_database()`).Scan(&actualClone, &afterOID))
			require.Equal(t, clone, actualClone)
			require.Equal(t, beforeOID, afterOID, "the migrated database is reused, not recreated")
			var migrationCount int
			require.NoError(t, business.QueryRow(t.Context(), `SELECT count(*) FROM schema_migrations`).Scan(&migrationCount))
			require.Positive(t, migrationCount)
			cfg := business.Config()
			require.Equal(t, tc.max, cfg.MaxConns)
			require.Equal(t, tc.min, cfg.MinConns)
			require.Equal(t, time.Hour, cfg.MaxConnLifetime)
			require.Equal(t, 30*time.Minute, cfg.MaxConnIdleTime)
			require.Equal(t, time.Minute, cfg.HealthCheckPeriod)
			stats := contentPerformancePoolStats(business)
			constructor := "db.NewPostgres"
			if diagnostic != nil {
				constructor = "db.NewPostgres config + test-only pgx tracer"
			}
			require.Equal(t, constructor, stats["constructor"])
			if diagnostic != nil {
				binding, err := contentPerformanceReadPGDatabase(t.Context(), business)
				require.NoError(t, err)
				require.EqualValues(t, beforeOID, binding.OID)
				require.False(t, binding.RelationsTruncated)
				var streamOID string
				require.NoError(t, business.QueryRow(t.Context(), `SELECT 'execution_source_streams'::regclass::oid::text`).Scan(&streamOID))
				require.Equal(t, "public.execution_source_streams", binding.Relations[streamOID])
				tx, err := business.Begin(t.Context())
				require.NoError(t, err)
				batch := &pgx.Batch{}
				batch.Queue("SELECT 1")
				results := tx.SendBatch(t.Context(), batch)
				var value int
				require.NoError(t, results.QueryRow().Scan(&value))
				require.Equal(t, 1, value)
				require.NoError(t, results.Close())
				require.NoError(t, tx.Commit(t.Context()))
				snap := diagnostic.snapshot()
				require.Empty(t, snap["pending_at_snapshot"])
				require.Empty(t, snap["open_transactions_at_snapshot"])
				var acquired, batched, committed bool
				for _, event := range snap["events"].([]contentPerformancePGEvent) {
					if event.Kind == "acquire" && event.PID > 0 && event.Error == "none" {
						acquired = true
					}
					if event.Kind == "batch_item_consumed" && event.BatchIndex == 1 {
						batched = true
					}
					if event.Kind == "transaction" && event.Class == "commit" && event.Tx > 0 && !event.Censored {
						committed = true
					}
				}
				require.True(t, acquired)
				require.True(t, batched)
				require.True(t, committed)
			}
			require.Equal(t, "config.LoadDBPoolLimits", stats["limits_source"])
			require.Equal(t, tc.max, stats["max_conns"])
			require.Equal(t, tc.min, stats["min_conns"])
			// A later owner cleanup still has its live pool; the helper closes it
			// afterwards and the original testutil cleanup then drops the clone.
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, business.Ping(ctx))
				ownerCleanupRan = true
			})
			if tc.name == "explicit_small_pool" {
				var held []*pgxpool.Conn
				defer func() {
					for _, conn := range held {
						conn.Release()
					}
				}()
				for range tc.max {
					conn, err := business.Acquire(t.Context())
					require.NoError(t, err)
					held = append(held, conn)
				}
				cancelledBefore := business.Stat().CanceledAcquireCount()
				ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
				defer cancel()
				conn, err := business.Acquire(ctx)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Nil(t, conn)
				require.Equal(t, cancelledBefore+1, business.Stat().CanceledAcquireCount())
				held[0].Release()
				held = held[1:]
				recovered, err := business.Acquire(t.Context())
				require.NoError(t, err, "cancelled waiter does not consume a released slot")
				recovered.Release()
			}
		})
		if business == nil {
			continue
		}
		require.True(t, ownerCleanupRan)
		unexpected, err := business.Acquire(t.Context())
		if unexpected != nil {
			unexpected.Release()
		}
		require.Error(t, err, "business pool closed by child cleanup")
		var remaining int
		require.NoError(t, observer.QueryRow(t.Context(), `SELECT count(*) FROM pg_database WHERE datname=$1`, clone).Scan(&remaining))
		require.Zero(t, remaining, "original isolated database cleanup completed")
	}
}
