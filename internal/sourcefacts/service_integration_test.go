//go:build integration

package sourcefacts

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestMain(m *testing.M) { testutil.RunWithMigratedTestDBTemplate(m) }

func setupSourceDB(t *testing.T) (*pgxpool.Pool, *Service, int64, int64) {
	t.Helper()
	return setupSourceDBWithPool(t, testutil.SetupMigratedTestDB(t))
}

func setupSourceDBWithPool(t *testing.T, pool *pgxpool.Pool) (*pgxpool.Pool, *Service, int64, int64) {
	t.Helper()
	ctx := context.Background()
	var org, project int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('source-test','active','standard') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Source','source','active','none') RETURNING id`, org).Scan(&project))
	s, e := New(pool, Options{Enabled: true, SourceInstance: "synthetic-ur", CursorSecret: strings.Repeat("x", 32)})
	require.NoError(t, e)
	require.NoError(t, s.CheckReady(ctx))
	return pool, s, org, project
}

func TestPublicationCommitOrderCannotSkipBlockedEarlierFacts(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	snap := testSnapshot()
	snap.Core.OrgID = executioncontext.ID(org)
	snap.Core.ProjectID = executioncontext.ID(project)
	snap.Core.RequestID = "first"
	first := s.contexts(org, project, "first", "authenticated", snap.Core.CapturedAt, snap.Core.CapturedAt, &snap)
	secondSnap := snap
	secondSnap.Core.RequestID = "second"
	second := s.contexts(org, project, "second", "authenticated", snap.Core.CapturedAt, snap.Core.CapturedAt, &secondSnap)
	slices.Reverse(second) // Callers cannot invert the global profile lock order.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	require.NoError(t, s.PublishTx(ctx, tx, first))
	pid := make(chan uint32, 1)
	done := make(chan error, 1)
	go func() {
		other, err := pool.Begin(ctx)
		if err != nil {
			done <- err
			return
		}
		defer other.Rollback(context.Background())
		pid <- other.Conn().PgConn().PID()
		if err = s.PublishTx(ctx, other, second); err == nil {
			err = other.Commit(ctx)
		}
		done <- err
	}()
	var writerPID uint32
	select {
	case writerPID = <-pid:
	case err := <-done:
		require.NoError(t, err)
		t.Fatal("writer exited before taking a source lock")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Eventually(t, func() bool {
		var waiting bool
		err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, writerPID).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, tx.Commit(ctx))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for _, profile := range []string{ContextProfile, IdentityProfile} {
		var requests []string
		var high int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT array_agg(request_id ORDER BY sequence) FROM execution_source_facts WHERE profile=$1`, profile).Scan(&requests))
		require.Equal(t, []string{"first", "second"}, requests)
		require.NoError(t, pool.QueryRow(ctx, `SELECT high_sequence FROM execution_source_streams WHERE profile=$1`, profile).Scan(&high))
		require.EqualValues(t, 2, high)
	}
}

func TestPublicationAtomicImmutableAndReplay(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	ctx := context.Background()
	snap := testSnapshot()
	snap.Core.OrgID = executioncontext.ID(org)
	snap.Core.ProjectID = executioncontext.ID(project)
	docs := s.contexts(org, project, snap.Core.RequestID, "authenticated", snap.Core.CapturedAt, snap.Core.CapturedAt, &snap)
	tx, e := pool.Begin(ctx)
	require.NoError(t, e)
	require.NoError(t, s.PublishTx(ctx, tx, docs))
	require.NoError(t, tx.Rollback(ctx))
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts`).Scan(&count))
	require.Zero(t, count)
	for range 2 {
		tx, e = pool.Begin(ctx)
		require.NoError(t, e)
		require.NoError(t, s.PublishTx(ctx, tx, docs))
		require.NoError(t, tx.Commit(ctx))
	}
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts`).Scan(&count))
	require.Equal(t, 2, count)
	_, e = pool.Exec(ctx, `UPDATE execution_source_facts SET request_id='changed'`)
	require.ErrorContains(t, e, "immutable")
	conflict := docs[0]
	copyCore := *conflict.Core
	copyCore.APIKeyID = "99"
	conflict.Core = &copyCore
	tx, e = pool.Begin(ctx)
	require.NoError(t, e)
	require.ErrorContains(t, s.PublishTx(ctx, tx, []Document{conflict}), "conflict")
	require.NoError(t, tx.Rollback(ctx))
	deleted, e := s.DeleteExpired(ctx, time.Now().Add(time.Hour))
	require.NoError(t, e)
	require.EqualValues(t, 2, deleted)
	var floor, high int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT retained_after_sequence,high_sequence FROM execution_source_streams WHERE profile=$1`, ContextProfile).Scan(&floor, &high))
	require.EqualValues(t, 1, floor)
	require.Equal(t, high, floor)
}

type sourceBatchFaultTx struct {
	pgx.Tx
	phase, calls int
	readError    bool
}

func (tx *sourceBatchFaultTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	tx.calls++
	result := tx.Tx.SendBatch(ctx, batch)
	if tx.calls == tx.phase {
		return sourceBatchFaultResults{BatchResults: result, readError: tx.readError}
	}
	return result
}

type sourceBatchFaultResults struct {
	pgx.BatchResults
	readError bool
}

func (r sourceBatchFaultResults) Query() (pgx.Rows, error) {
	rows, err := r.BatchResults.Query()
	if err == nil && r.readError {
		return sourceBatchFaultRows{Rows: rows}, nil
	}
	return rows, err
}
func (r sourceBatchFaultResults) Close() error {
	if err := r.BatchResults.Close(); err != nil {
		return err
	}
	if !r.readError {
		return errors.New("synthetic batch close failure")
	}
	return nil
}

type sourceBatchFaultRows struct{ pgx.Rows }

func (r sourceBatchFaultRows) Err() error {
	if err := r.Rows.Err(); err != nil {
		return err
	}
	return errors.New("synthetic batch row read failure")
}

func TestPublicationBatchErrorsRequireWholeTransactionRollback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase int
		read  bool
	}{
		{"read_rows", 1, true}, {"read_close", 1, false}, {"write_close", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, s, org, project := setupSourceDB(t)
			snap := testSnapshot()
			snap.Core.OrgID = executioncontext.ID(org)
			snap.Core.ProjectID = executioncontext.ID(project)
			docs := s.contexts(org, project, snap.Core.RequestID, "authenticated", snap.Core.CapturedAt, snap.Core.CapturedAt, &snap)
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)
			err = s.PublishTx(t.Context(), &sourceBatchFaultTx{Tx: tx, phase: tc.phase, readError: tc.read}, docs)
			require.ErrorContains(t, err, "synthetic batch")
			wantStage := "batch_close"
			if tc.read {
				wantStage = "replay_predecessor_read"
			}
			require.Equal(t, wantStage, CaptureErrorStage(err))
			require.NoError(t, tx.Rollback(t.Context()))
			var facts, streams int
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_source_facts),(SELECT count(*) FROM execution_source_streams)`).Scan(&facts, &streams))
			require.Zero(t, facts)
			require.Zero(t, streams)
			require.NoError(t, s.CaptureAuthenticated(t.Context(), snap))
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts`).Scan(&facts))
			require.Equal(t, 2, facts)
		})
	}
}

func TestPublicationBatchCancellationAndRevisionPredecessor(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	snap := testSnapshot()
	snap.Core.OrgID = executioncontext.ID(org)
	snap.Core.ProjectID = executioncontext.ID(project)
	docs := s.contexts(org, project, snap.Core.RequestID, "authenticated", snap.Core.CapturedAt, snap.Core.CapturedAt, &snap)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	require.NoError(t, s.PublishTx(t.Context(), tx, docs))
	other, err := pool.Begin(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	err = s.PublishTx(ctx, other, docs)
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	_ = other.Rollback(t.Context()) // pgx may already discard the canceled connection.
	require.NoError(t, tx.Commit(t.Context()))
	var originalHash string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT source_hash FROM execution_source_facts WHERE event_id=$1`, docs[0].EventID).Scan(&originalHash))
	revision := docs[0]
	revision.Revision = "2"
	revision.PreviousRevision = "1"
	revision.PreviousHash = originalHash
	revision.CorrectionReason = "late_observation"
	revision.EventID = revision.eventID()
	for _, good := range []bool{false, true, true} {
		candidate := revision
		if !good {
			candidate.PreviousHash = "sha256:" + strings.Repeat("0", 64)
		}
		tx, err = pool.Begin(t.Context())
		require.NoError(t, err)
		err = s.PublishTx(t.Context(), tx, []Document{candidate})
		if good {
			require.NoError(t, err)
			require.NoError(t, tx.Commit(t.Context()))
		} else {
			require.ErrorContains(t, err, "predecessor differs")
			require.NoError(t, tx.Rollback(t.Context()))
		}
	}
	var facts, high int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts`).Scan(&facts))
	require.Equal(t, 3, facts)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT high_sequence FROM execution_source_streams WHERE profile=$1`, docs[0].Profile).Scan(&high))
	require.Equal(t, 2, high)
}

// Reproduce a stale REPEATABLE READ stream-row conflict without HTTP load or
// changing production isolation/retry/budgets. The diagnostic is evidence of
// the SQL return stage, not a claim about the deleted earlier performance DB.
func TestSourceCaptureStageConcurrentStreamPG(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	snapshot := testSnapshot()
	snapshot.Core.OrgID = executioncontext.ID(org)
	snapshot.Core.ProjectID = executioncontext.ID(project)
	require.NoError(t, s.CaptureAuthenticated(t.Context(), snapshot))
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(t.Context(), `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
	require.NoError(t, err)
	var sequence int64
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT high_sequence FROM execution_source_streams WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4`, s.SourceInstance(), org, project, ContextProfile).Scan(&sequence))
	snapshot.Core.RequestID = "source-stage-committed"
	require.NoError(t, s.CaptureAuthenticated(t.Context(), snapshot))
	var before int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts`).Scan(&before))
	snapshot.Core.RequestID = "source-stage-stale"
	docs := s.contexts(org, project, snapshot.Core.RequestID, "authenticated", snapshot.Core.CapturedAt, snapshot.Core.CapturedAt, &snapshot)
	err = s.PublishTx(t.Context(), tx, docs)
	var pgerr *pgconn.PgError
	require.ErrorAs(t, err, &pgerr)
	require.Equal(t, "40001", pgerr.Code)
	require.Contains(t, []string{"stream_insert", "stream_lock"}, CaptureErrorStage(err))
	t.Logf("source_capture stage=%s sqlstate=%s", CaptureErrorStage(err), pgerr.Code)
	require.NoError(t, tx.Rollback(t.Context()))
	var after int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts`).Scan(&after))
	require.Equal(t, before, after)
}
