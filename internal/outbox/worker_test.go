package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

// Exercise the real claim/process/runWorker control flow without a database.
// SQL eligibility itself remains covered by the separate PG selectors.
func TestOutboxWorkerSchedulingOffline(t *testing.T) {
	for _, kind := range []string{"busy", "empty", "future_only", "claim_error", "handler_error", "cancel_during_batch", "busy_lag_refresh"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				pending := 76
				var processed, queries, lagReads atomic.Int64
				if kind == "empty" || kind == "future_only" || kind == "claim_error" {
					pending = 0
				}
				if kind == "handler_error" {
					pending = 1
				}
				if kind == "cancel_during_batch" {
					pending = 25
				}
				if kind == "busy_lag_refresh" {
					pending = 76
				}
				nextID := int64(0)
				store := &testutil.MockDB{}
				store.QueryFn = func(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
					queries.Add(1)
					if kind == "claim_error" {
						return nil, errors.New("synthetic claim failure")
					}
					rows := &testutil.MockRows{}
					if args[3].(string) == "high" {
						return rows, nil
					}
					for i := 0; i < args[0].(int) && pending > 0; i++ {
						pending--
						nextID++
						id := nextID
						owner := args[1].(string)
						rows.Rows = append(rows.Rows, func(d ...any) error {
							*d[0].(*int64) = id
							*d[1].(*string) = "synthetic"
							*d[2].(*string) = EventTrace
							*d[3].(*int) = PriorityTraceInfo
							*d[4].(*int64) = 1
							*d[5].(*string) = "synthetic-op"
							*d[6].(*json.RawMessage) = json.RawMessage(`{}`)
							*d[7].(*time.Time) = time.Now()
							*d[8].(*int) = 1
							*d[9].(*string) = owner
							return nil
						})
					}
					return rows, nil
				}
				store.QueryRowFn = func(context.Context, string, ...any) pgx.Row {
					lagReads.Add(1)
					return &testutil.MockRow{ScanFn: func(d ...any) error { *d[0].(*float64) = 0; return nil }}
				}
				store.ExecFn = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "DELETE") {
						return pgconn.NewCommandTag("DELETE 1"), nil
					}
					return pgconn.NewCommandTag("UPDATE 1"), nil
				}
				service, err := New(store, DefaultOptions())
				require.NoError(t, err)
				require.NoError(t, service.RegisterHandler(EventTrace, func(context.Context, Event) error {
					processed.Add(1)
					if kind == "handler_error" {
						return errors.New("synthetic handler failure")
					}
					if kind == "busy_lag_refresh" {
						time.Sleep(110 * time.Millisecond)
					}
					if kind == "cancel_during_batch" {
						cancel()
					}
					return nil
				}))
				start := time.Now()
				done := make(chan struct{})
				go func() { defer close(done); service.runWorker(ctx, 0) }()
				if kind == "busy_lag_refresh" {
					time.Sleep(9 * time.Second)
				}
				synctest.Wait()
				if kind == "busy" {
					require.Equal(t, int64(76), processed.Load())
					require.Equal(t, time.Duration(0), time.Since(start))
					require.Equal(t, int64(1), lagReads.Load())
				}
				if kind == "busy_lag_refresh" {
					require.Equal(t, int64(76), processed.Load())
					require.GreaterOrEqual(t, lagReads.Load(), int64(2))
				}
				if kind == "cancel_during_batch" {
					require.Equal(t, int64(3), queries.Load(), "cancelled worker must not claim another batch")
				}
				if kind == "empty" || kind == "future_only" || kind == "claim_error" || kind == "handler_error" {
					before := queries.Load()
					time.Sleep(DefaultOptions().PollInterval / 2)
					synctest.Wait()
					require.Equal(t, before, queries.Load(), "idle/error must wait for original ticker")
					time.Sleep(DefaultOptions().PollInterval / 2)
					synctest.Wait()
					require.Greater(t, queries.Load(), before)
					if kind == "handler_error" {
						require.Equal(t, int64(1), processed.Load(), "failed handler is not immediately retried")
					}
				}
				cancel()
				<-done
			})
		})
	}
}
