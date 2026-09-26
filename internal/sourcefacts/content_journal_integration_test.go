//go:build integration

package sourcefacts

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/executioncontext"
)

func setupContentJournal(t *testing.T) (*pgxpool.Pool, *Service, int64, int64) {
	t.Helper()
	pool, s, org, project := setupSourceDB(t)
	s.options.ContentEnabled = true
	require.NoError(t, s.CheckReady(t.Context()))
	// Explicit synthetic capture-owner setup. The appender itself must never
	// initialize the stream, capture policy, or content read authorization.
	_, err := pool.Exec(t.Context(), `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,$3)`, org, project, s.SourceInstance())
	require.NoError(t, err)
	return pool, s, org, project
}
func contentJournalFixture(org, project int64, request string) ContentManifest {
	m := syntheticContentManifest(nil)
	m.OrgID = executioncontext.ID(org)
	m.ProjectID = executioncontext.ID(project)
	m.RequestID = request
	m.ContentRef = ""
	m.Representation = nil
	m.CapturedAt = ""
	m.ExpiresAt = ""
	m.Capture.Completeness = "pending"
	m.Capture.Reason = "capture_in_progress"
	m.SourceFactID = m.FactID()
	m.EventID = m.EventIDForRevision()
	return m
}
func contentJournalHigh(t *testing.T, pool *pgxpool.Pool, org, project int64) int64 {
	t.Helper()
	var high int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT high_sequence FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&high))
	return high
}
func TestContentJournalCommitReplayConsecutiveAndLateRevision(t *testing.T) {
	pool, s, org, project := setupContentJournal(t)
	first := contentJournalFixture(org, project, "synthetic-first")
	result, err := s.AppendContentManifest(t.Context(), first, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Sequence)
	require.False(t, result.Replayed)
	wire, err := EncodeContentManifest(first)
	require.NoError(t, err)
	require.Equal(t, eventconnector.HashSourceBytes(wire), result.SourceSHA256)
	replay, err := s.AppendContentManifest(t.Context(), first, 1)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, result.Sequence, replay.Sequence)
	conflicting := first
	conflicting.RecordedAt = "2026-09-15T00:00:00.000001Z"
	_, err = s.AppendContentManifest(t.Context(), conflicting, 1)
	require.ErrorIs(t, err, ErrContentJournalRevision)
	other := contentJournalFixture(org, project, "synthetic-other")
	second, err := s.AppendContentManifest(t.Context(), other, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, second.Sequence)
	late := first
	late.Revision = "2"
	late.PreviousRevision = "1"
	late.PreviousHash = result.SourceSHA256
	late.CorrectionReason = "late_capture_failure"
	late.Capture.Completeness = "not_captured"
	late.Capture.Reason = "storage_write_failed"
	late.EventID = late.EventIDForRevision()
	lateResult, err := s.AppendContentManifest(t.Context(), late, 1)
	require.NoError(t, err)
	require.EqualValues(t, 3, lateResult.Sequence)
	var ids []string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(request_id||':'||revision::text ORDER BY sequence) FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&ids))
	require.Equal(t, []string{"synthetic-first:1", "synthetic-other:1", "synthetic-first:2"}, ids, "late revisions remain discoverable after unrelated commits")
	skipped := late
	skipped.Revision = "4"
	skipped.PreviousRevision = "3"
	skipped.EventID = skipped.EventIDForRevision()
	_, err = s.AppendContentManifest(t.Context(), skipped, 1)
	require.ErrorIs(t, err, ErrContentJournalRevision)
	wrongHash := late
	wrongHash.Revision = "3"
	wrongHash.PreviousRevision = "2"
	wrongHash.PreviousHash = "sha256:" + strings.Repeat("0", 64)
	wrongHash.EventID = wrongHash.EventIDForRevision()
	_, err = s.AppendContentManifest(t.Context(), wrongHash, 1)
	require.ErrorIs(t, err, ErrContentJournalRevision)
	replay, err = s.AppendContentManifest(t.Context(), first, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, replay.Sequence)
	require.True(t, replay.Replayed)
	require.EqualValues(t, 3, contentJournalHigh(t, pool, org, project))
	_, err = pool.Exec(t.Context(), `UPDATE execution_content_manifests SET document=document WHERE org_id=$1 AND project_id=$2`, org, project)
	require.Error(t, err, "published history remains immutable")
}
func TestContentJournalRequiresExactReadyObject(t *testing.T) {
	pool, s, org, project := setupContentJournal(t)
	m := syntheticContentManifest(nil)
	m.OrgID = executioncontext.ID(org)
	m.ProjectID = executioncontext.ID(project)
	m.SourceFactID = m.FactID()
	m.EventID = m.EventIDForRevision()
	_, err := s.AppendContentManifest(t.Context(), m, 1)
	require.ErrorIs(t, err, ErrContentObjectNotReady)
	// This synthetic catalog row tests DB admission only. It does not certify
	// filesystem durability, shared storage identity, or real body availability.
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_objects(content_ref,org_id,project_id,source_fact_id,revision,storage_identity,storage_binding_token,policy_version,recovery_epoch,relative_path,state,reserved_bytes,saved_bytes,digest,expires_at,lease_owner,lease_expires_at,policy_config,policy_effective_at)
VALUES($1,$2,$3,$4,1,'synthetic-store',repeat('1',64),$10,1,$5,'reserved',0,0,$6,$7,'synthetic-worker',clock_timestamp()+interval '1 hour',$8,$9)`, m.ContentRef, org, project, m.SourceFactID, strings.Repeat("a", 64), m.Representation.Digest, m.ExpiresAt, contentFixturePolicy(t, m), m.Capture.PolicyEffectiveAt, m.Capture.PolicyVersion)
	require.NoError(t, err)
	_, err = s.AppendContentManifest(t.Context(), m, 1)
	require.ErrorIs(t, err, ErrContentObjectNotReady)
	_, err = pool.Exec(t.Context(), `UPDATE execution_content_objects SET state='ready' WHERE content_ref=$1`, m.ContentRef)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*ContentManifest){
		"digest": func(m *ContentManifest) {
			r := *m.Representation
			r.Digest = "sha256:" + strings.Repeat("b", 64)
			m.Representation = &r
		},
		"length":    func(m *ContentManifest) { r := *m.Representation; r.SavedBytes = 1; m.Representation = &r },
		"expiry":    func(m *ContentManifest) { m.ExpiresAt = "2026-09-17T00:00:00.000000Z" },
		"reference": func(m *ContentManifest) { m.ContentRef = "uc1_" + strings.Repeat("b", 64) },
		"fact": func(m *ContentManifest) {
			m.RequestID = "another-request"
			m.SourceFactID = m.FactID()
			m.EventID = m.EventIDForRevision()
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := m
			mutate(&bad)
			_, err := s.AppendContentManifest(t.Context(), bad, 1)
			require.ErrorIs(t, err, ErrContentObjectNotReady)
			require.Zero(t, contentJournalHigh(t, pool, org, project))
		})
	}
	result, err := s.AppendContentManifest(t.Context(), m, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Sequence, "zero bytes is still a real complete representation")
	partial := m
	partial.RequestID = "synthetic-partial"
	partial.ContentRef = "uc1_" + strings.Repeat("c", 64)
	partial.SourceFactID = partial.FactID()
	partial.EventID = partial.EventIDForRevision()
	partial.Capture.Completeness = "partial"
	partial.Capture.Reason = "stream_interrupted"
	representation := *m.Representation
	representation.SavedBytes = 3
	representation.Digest = eventconnector.HashSourceBytes([]byte{0, 255, 0})
	partial.Representation = &representation
	_, err = s.AppendContentManifest(t.Context(), partial, 1)
	require.ErrorIs(t, err, ErrContentObjectNotReady, "partial capture also requires its own ready mapping")
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_objects(content_ref,org_id,project_id,source_fact_id,revision,storage_identity,storage_binding_token,policy_version,recovery_epoch,relative_path,state,reserved_bytes,saved_bytes,digest,expires_at,lease_owner,lease_expires_at,policy_config,policy_effective_at)
VALUES($1,$2,$3,$4,1,'synthetic-store',repeat('1',64),$10,1,$5,'ready',3,3,$6,$7,'synthetic-worker',clock_timestamp()+interval '1 hour',$8,$9)`, partial.ContentRef, org, project, partial.SourceFactID, strings.Repeat("c", 64), representation.Digest, partial.ExpiresAt, contentFixturePolicy(t, partial), partial.Capture.PolicyEffectiveAt, partial.Capture.PolicyVersion)
	require.NoError(t, err)
	partialResult, err := s.AppendContentManifest(t.Context(), partial, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, partialResult.Sequence)
}
func TestContentJournalScopeEpochAndNoImplicitConfiguration(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	s.options.ContentEnabled = true
	m := contentJournalFixture(org, project, "synthetic-unconfigured")
	_, err := s.AppendContentManifest(t.Context(), m, 1)
	require.ErrorIs(t, err, ErrContentJournalUnconfigured)
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&count))
	require.Zero(t, count)
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_streams(org_id,project_id,source_instance,recovery_epoch) VALUES($1,$2,'other-source',2)`, org, project)
	require.NoError(t, err)
	_, err = s.AppendContentManifest(t.Context(), m, 2)
	require.ErrorIs(t, err, ErrContentJournalScope)
	_, err = pool.Exec(t.Context(), `UPDATE execution_content_streams SET source_instance=$3 WHERE org_id=$1 AND project_id=$2`, org, project, s.SourceInstance())
	require.NoError(t, err)
	_, err = s.AppendContentManifest(t.Context(), m, 1)
	require.ErrorIs(t, err, ErrContentEpochChanged)
	require.Zero(t, contentJournalHigh(t, pool, org, project))
	_, err = s.AppendContentManifest(t.Context(), m, 2)
	require.NoError(t, err)
}
func TestContentJournalRollbackDoesNotConsumeSequence(t *testing.T) {
	pool, s, org, project := setupContentJournal(t)
	// Test-owned failure trigger aborts after the stream counter was updated.
	_, err := pool.Exec(t.Context(), `CREATE FUNCTION synthetic_content_append_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic append failure'; END $$;
CREATE TRIGGER synthetic_content_append_failure BEFORE INSERT ON execution_content_manifests FOR EACH ROW EXECUTE FUNCTION synthetic_content_append_failure()`)
	require.NoError(t, err)
	m := contentJournalFixture(org, project, "synthetic-rollback")
	_, err = s.AppendContentManifest(t.Context(), m, 1)
	require.ErrorContains(t, err, "synthetic append failure")
	require.Zero(t, contentJournalHigh(t, pool, org, project))
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&count))
	require.Zero(t, count)
	_, err = pool.Exec(t.Context(), `DROP TRIGGER synthetic_content_append_failure ON execution_content_manifests; DROP FUNCTION synthetic_content_append_failure()`)
	require.NoError(t, err)
	result, err := s.AppendContentManifest(t.Context(), m, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Sequence)
}
func TestContentJournalCommitLockPreventsEarlyVisibilityAndLostWriter(t *testing.T) {
	pool, s, org, project := setupContentJournal(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	started := time.Now()
	beginAt := time.Now()
	owner, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	require.NoError(t, err)
	defer owner.Rollback(context.Background())
	ownerPID := owner.Conn().PgConn().PID()
	t.Logf("publication_stage kind=c role=owner pid=%d stage=begin duration_ns=%d", ownerPID, time.Since(beginAt).Nanoseconds())
	publicationAt := time.Now()
	var high int64
	require.NoError(t, owner.QueryRow(ctx, `SELECT high_sequence FROM execution_content_streams WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, org, project).Scan(&high))
	first := contentJournalFixture(org, project, "synthetic-held-first")
	wire, err := EncodeContentManifest(first)
	require.NoError(t, err)
	_, err = owner.Exec(ctx, `UPDATE execution_content_streams SET high_sequence=1 WHERE org_id=$1 AND project_id=$2`, org, project)
	require.NoError(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution_content_manifests(org_id,project_id,sequence,source_fact_id,event_id,revision,request_id,document,source_hash) VALUES($1,$2,1,$3,$4,1,$5,$6,$7)`, org, project, first.SourceFactID, first.EventID, first.RequestID, wire, eventconnector.HashSourceBytes(wire))
	require.NoError(t, err)
	t.Logf("publication_stage kind=c role=owner pid=%d stage=publish duration_ns=%d", ownerPID, time.Since(publicationAt).Nanoseconds())
	observer, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer observer.Release()
	type outcome struct {
		result ContentAppendResult
		err    error
	}
	done := make(chan outcome, 1)
	writerExited := make(chan struct{})
	go func() {
		defer close(writerExited)
		appendAt := time.Now()
		result, err := s.AppendContentManifest(ctx, contentJournalFixture(org, project, "synthetic-waiting-second"), 1)
		// Keep the public appender's own Begin/Commit; do not substitute a Tx API.
		t.Logf("publication_stage kind=c role=writer stage=append_including_begin_commit duration_ns=%d elapsed_ns=%d ok=%t", time.Since(appendAt).Nanoseconds(), time.Since(started).Nanoseconds(), err == nil)
		done <- outcome{result, err}
	}()
	defer func() {
		cancel()
		_ = owner.Rollback(context.Background())
		<-writerExited
	}()
	var writerPID uint32
	require.Eventually(t, func() bool {
		// Only one other writer exists in this clone; bind it by this owner's
		// actual blocking edge, without inspecting or exporting query text.
		err := observer.QueryRow(ctx, `SELECT COALESCE(min(pid),0) FROM pg_stat_activity
 WHERE datid=(SELECT oid FROM pg_database WHERE datname=current_database())
 AND $1::int=ANY(pg_blocking_pids(pid))`, ownerPID).Scan(&writerPID)
		return err == nil && writerPID != 0
	}, 3*time.Second, 5*time.Millisecond)
	snapshot, err := publicationCommitLockSnapshot(ctx, observer, ownerPID, writerPID)
	require.NoError(t, err)
	t.Logf("publication_lock kind=c sample=%s", snapshot)
	require.NoError(t, observer.QueryRow(ctx, `SELECT high_sequence FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&high))
	require.Zero(t, high, "reader sees no uncommitted high-water mark")
	var count int
	require.NoError(t, observer.QueryRow(ctx, `SELECT count(*) FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&count))
	require.Zero(t, count)
	commitAt := time.Now()
	require.NoError(t, owner.Commit(ctx))
	t.Logf("publication_stage kind=c role=owner pid=%d stage=commit duration_ns=%d", ownerPID, time.Since(commitAt).Nanoseconds())
	select {
	case outcome := <-done:
		require.NoError(t, outcome.err)
		require.EqualValues(t, 2, outcome.result.Sequence)
	case <-ctx.Done():
		t.Fatal(fmt.Errorf("waiting appender did not complete: %w", ctx.Err()))
	}
	require.NoError(t, observer.QueryRow(ctx, `SELECT high_sequence FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&high))
	require.EqualValues(t, 2, high)
}
