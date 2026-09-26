//go:build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestContentPerformancePGOffline(t *testing.T) {
	t.Run("finite_classes_and_no_sensitive_output", func(t *testing.T) {
		for _, tc := range []struct{ sql, want string }{
			{"BEGIN ISOLATION LEVEL REPEATABLE READ", "begin"}, {"commit", "commit"}, {"ROLLBACK", "rollback"},
			{"rollback to savepoint sp_1", "savepoint"},
			{"SELECT high_sequence FROM execution_source_streams WHERE org_id=$1 FOR UPDATE", "ab_stream_lock"},
			{"SELECT high_sequence FROM execution_content_streams WHERE org_id=$1 FOR UPDATE", "c_stream_lock"},
			{"UPDATE freeze_idempotency SET state=$1", "finance_freeze_write_or_cte"},
			{"SELECT * FROM private_unknown WHERE password='secret-sql'", "other"},
		} {
			require.Equal(t, tc.want, contentPerformanceSQLClass(tc.sql))
		}
		d := newContentPerformancePG()
		ctx := context.WithValue(t.Context(), contentPerformancePhaseKey{}, &contentPerformancePhase{marker: "r0-plain-7"})
		call := d.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "select 'secret-sql'", Args: []any{"secret-argument"}})
		require.Equal(t, "r0-plain-7", contentPerformancePhaseFrom(call).marker)
		d.TraceQueryEnd(call, nil, pgx.TraceQueryEndData{Err: errors.New("secret-error")})
		wire, err := json.Marshal(d.snapshot())
		require.NoError(t, err)
		require.NotContains(t, string(wire), "secret")
		require.Equal(t, "r0-plain-7", d.events[0].Marker)
		require.Equal(t, "other_redacted", d.events[0].Error)
	})
	t.Run("sqlstate_and_startup_binding_fail_closed", func(t *testing.T) {
		require.Equal(t, "sqlstate_40001", contentPerformancePGError(&pgconn.PgError{Code: "40001", Message: "secret-error"}))
		require.Equal(t, "other_redacted", contentPerformancePGError(&pgconn.PgError{Code: "secret-code"}))
		for _, valid := range []bool{true, false} {
			calls := 0
			pool := &testutil.MockDB{QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
				require.Same(t, t.Context(), ctx)
				require.Empty(t, args)
				require.Contains(t, query, "LIMIT 4096")
				calls++
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*string) = "clone"
					*dest[1].(*int64) = 123
					if valid {
						*dest[2].(*map[string]string) = map[string]string{"456": "public.execution_source_streams"}
					}
					*dest[3].(*bool) = false
					return nil
				}}
			}}
			got, err := contentPerformanceReadPGDatabase(t.Context(), pool)
			if valid {
				require.NoError(t, err)
				require.EqualValues(t, 123, got.OID)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, 1, calls)
		}
	})

	t.Run("whole_transaction_and_observed_lock_suffix", func(t *testing.T) {
		d := newContentPerformancePG()
		c := &contentPerformancePGConn{id: 1}
		finish := func(class string, start, end int64, status string, err error) {
			d.complete(contentPerformancePGEvent{Kind: "query", Class: class, StartNS: start, EndNS: end, TxStatus: status, Error: contentPerformanceErrorClass(err)}, c, err)
		}
		finish("begin", 10, 20, "T", nil)
		first := c.tx
		finish("ab_stream_lock", 30, 70, "T", nil)
		finish("savepoint", 80, 90, "T", nil)
		require.Equal(t, first, c.tx)
		finish("ab_facts_write_or_cte", 100, 130, "T", nil)
		finish("commit", 150, 160, "I", nil)
		tx := d.events[len(d.events)-1]
		require.Equal(t, "transaction", tx.Kind)
		require.EqualValues(t, 150, tx.EndNS-tx.StartNS)
		require.EqualValues(t, 70, tx.FirstLockObservedNS)
		require.Equal(t, "ab_stream_lock", tx.FirstLockClass)
		require.EqualValues(t, 0, c.tx)
		require.False(t, tx.Censored)
		finish("begin", 170, 180, "T", nil)
		require.NotEqual(t, first, c.tx)
		finish("commit", 190, 200, "E", errors.New("unknown commit outcome"))
		require.True(t, d.events[len(d.events)-1].Censored)
		require.NotZero(t, c.tx)
		finish("rollback", 210, 220, "I", nil)
		require.Zero(t, c.tx)
	})
	t.Run("cancelled_acquire_inflight_and_batch_boundaries", func(t *testing.T) {
		d := newContentPerformancePG()
		ctx, cancel := context.WithCancel(t.Context())
		acquire := d.TraceAcquireStart(ctx, nil, pgxpool.TraceAcquireStartData{})
		cancel()
		require.ErrorIs(t, acquire.Err(), context.Canceled)
		require.Len(t, d.snapshot()["pending_at_snapshot"], 1)
		d.TraceAcquireEnd(acquire, nil, pgxpool.TraceAcquireEndData{Err: context.Canceled})
		require.Equal(t, "context_cancelled", d.events[0].Error)
		require.Zero(t, d.events[0].PID)
		b := &pgx.Batch{}
		b.Queue("SELECT * FROM execution_source_streams FOR UPDATE")
		batch := d.TraceBatchStart(t.Context(), nil, pgx.TraceBatchStartData{Batch: b})
		start := batch.Value(contentPerformancePGKey{}).(*contentPerformancePGCall).row.StartNS
		d.TraceBatchQuery(batch, nil, pgx.TraceBatchQueryData{SQL: b.QueuedQueries[0].SQL})
		d.TraceBatchEnd(batch, nil, pgx.TraceBatchEndData{})
		require.Equal(t, "batch_item_consumed", d.events[1].Kind)
		require.Equal(t, start, d.events[1].StartNS)
		require.Equal(t, 1, d.events[1].BatchIndex)
		require.Empty(t, d.snapshot()["pending_at_snapshot"])
	})
	t.Run("concurrent_callbacks_and_explicit_overflow", func(t *testing.T) {
		d := newContentPerformancePG()
		var wg sync.WaitGroup
		for range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 8 {
					ctx := d.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "select 'secret'"})
					d.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
					_ = d.snapshot()
				}
			}()
		}
		wg.Wait()
		require.Len(t, d.events, 256)
		require.Empty(t, d.active)
		d.events = make([]contentPerformancePGEvent, contentPerformancePGLimit)
		ctx := d.TraceAcquireStart(t.Context(), nil, pgxpool.TraceAcquireStartData{})
		d.TraceAcquireEnd(ctx, nil, pgxpool.TraceAcquireEndData{Err: context.DeadlineExceeded})
		require.Len(t, d.events, contentPerformancePGLimit)
		require.EqualValues(t, 1, d.dropped)
		require.True(t, d.snapshot()["truncated"].(bool))
		require.Empty(t, d.active)
		require.LessOrEqual(t, d.start, time.Now())
	})
}
