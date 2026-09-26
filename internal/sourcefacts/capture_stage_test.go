package sourcefacts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/executionmeasurement"
	"github.com/uniroute/unirouter/internal/testutil"
)

type captureStageTestTx struct {
	*testutil.MockTx
	fault   string
	cause   error
	batches int
}

func (tx *captureStageTestTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	tx.batches++
	return &captureStageTestBatch{write: tx.batches == 2, fault: tx.fault, cause: tx.cause}
}

type captureStageTestBatch struct {
	write bool
	fault string
	cause error
}

func (b *captureStageTestBatch) Exec() (pgconn.CommandTag, error) {
	if b.write && b.fault == "sequence_and_fact_write" || !b.write && b.fault == "stream_insert" {
		return pgconn.CommandTag{}, b.cause
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (b *captureStageTestBatch) QueryRow() pgx.Row {
	return &testutil.MockRow{ScanFn: func(d ...any) error {
		switch dst := d[0].(type) {
		case *int64:
			if strings.HasPrefix(b.fault, "stream_lock") {
				return b.cause
			}
			*dst = 0
		case *time.Time:
			if b.fault == "publication_clock" {
				return b.cause
			}
			*dst = time.Now()
		}
		return nil
	}}
}
func (b *captureStageTestBatch) Query() (pgx.Rows, error) {
	if b.fault == "replay_predecessor_read" {
		return nil, b.cause
	}
	return &testutil.MockRows{}, nil
}
func (b *captureStageTestBatch) Close() error {
	if !b.write && b.fault == "read_close" || b.write && b.fault == "write_close" || b.fault == "stream_lock+close" {
		return b.cause
	}
	return nil
}

func TestSourceCapturePublicationStagesOffline(t *testing.T) {
	for _, fault := range []string{"stream_insert", "stream_lock", "stream_lock+close", "publication_clock", "replay_predecessor_read", "read_close", "sequence_and_fact_write", "write_close"} {
		t.Run(fault, func(t *testing.T) {
			cause := &pgconn.PgError{Code: "40001", Message: "synthetic conflict"}
			tx := &captureStageTestTx{MockTx: &testutil.MockTx{}, fault: fault, cause: cause}
			service := &Service{options: Options{SourceInstance: "synthetic-ur"}}
			snapshot := testSnapshot()
			doc := service.contexts(1, 2, snapshot.Core.RequestID, "authenticated", snapshot.Core.CapturedAt, snapshot.Core.CapturedAt, &snapshot)[0]
			record, err := publicationA(doc)
			require.NoError(t, err)
			err = service.publishRecordsTx(t.Context(), tx, []sourcePublication{record})
			require.ErrorIs(t, err, cause)
			var pgerr *pgconn.PgError
			require.ErrorAs(t, fmt.Errorf("outer: %w", err), &pgerr)
			require.Same(t, cause, pgerr)
			want := fault
			if strings.HasSuffix(fault, "_close") {
				want = "batch_close"
			}
			if fault == "stream_lock+close" {
				want = "stream_lock"
			}
			require.Equal(t, want, CaptureErrorStage(err))
		})
	}
	require.Equal(t, "unknown", CaptureErrorStage(errors.New("stream_lock: not a typed stage")))
	require.Equal(t, "unknown", CaptureErrorStage(captureError("untrusted-secret", context.Canceled)))
	require.Nil(t, captureError("commit", nil))
}

func TestSourceCaptureTransactionStagesOffline(t *testing.T) {
	for _, phase := range []string{"authenticated", "admission_rejection", "measurements"} {
		stages := []string{"begin", "commit"}
		if phase != "authenticated" {
			stages = append(stages, "transaction_setup")
		}
		if phase == "admission_rejection" {
			stages = append(stages, "admission_lookup")
		}
		for _, stage := range stages {
			t.Run(phase+"/"+stage, func(t *testing.T) {
				cause := context.DeadlineExceeded
				begins := 0
				setups := 0
				rollbacks := 0
				tx := &captureStageTestTx{MockTx: &testutil.MockTx{}}
				tx.ExecFn = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
					setups++
					require.Equal(t, `SET TRANSACTION ISOLATION LEVEL READ COMMITTED; SET LOCAL lock_timeout='100ms'; SET LOCAL statement_timeout='200ms'`, sql)
					if stage == "transaction_setup" {
						return pgconn.CommandTag{}, cause
					}
					return pgconn.CommandTag{}, nil
				}
				tx.QueryRowFn = func(context.Context, string, ...any) pgx.Row {
					return &testutil.MockRow{ScanFn: func(d ...any) error {
						if stage == "admission_lookup" {
							return cause
						}
						*d[0].(*bool) = false
						return nil
					}}
				}
				tx.CommitFn = func(context.Context) error {
					if stage == "commit" {
						return cause
					}
					return nil
				}
				tx.RollbackFn = func(context.Context) error { rollbacks++; return nil }
				store := &testutil.MockDB{BeginFn: func(ctx context.Context) (pgx.Tx, error) {
					begins++
					if phase != "authenticated" {
						deadline, ok := ctx.Deadline()
						require.True(t, ok)
						require.LessOrEqual(t, time.Until(deadline), 250*time.Millisecond)
					}
					if stage == "begin" {
						return nil, cause
					}
					return tx, nil
				}}
				service, err := New(store, Options{Enabled: true, ObservationsEnabled: true, SourceInstance: "synthetic-ur", CursorSecret: strings.Repeat("s", 32)})
				require.NoError(t, err)
				snapshot := testSnapshot()
				switch phase {
				case "authenticated":
					err = service.CaptureAuthenticated(t.Context(), snapshot)
				case "admission_rejection":
					err = service.CaptureAdmissionRejection(t.Context(), snapshot, 429)
				case "measurements":
					duration := int64(1)
					err = service.CaptureMeasurements(t.Context(), snapshot, []executionmeasurement.Sample{{Kind: executionmeasurement.ResponseComplete, State: "observed", Nanoseconds: &duration}})
				}
				require.ErrorIs(t, err, cause)
				require.Equal(t, stage, CaptureErrorStage(err))
				require.Equal(t, 1, begins, "diagnostics must not add retries")
				if stage != "begin" {
					require.Equal(t, 1, rollbacks)
				}
				if phase != "authenticated" && stage != "begin" {
					require.Equal(t, 1, setups)
				}
			})
		}
	}
}
