//go:build integration

package sourcefacts

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

func TestGroupedPublicationPGAtomicReplayAndRollback(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	records := groupedPublicationFixture(t, s, org, project, "grouped-pg")
	publish := func(records []sourcePublication, commit bool) {
		tx, err := pool.Begin(t.Context())
		require.NoError(t, err)
		defer tx.Rollback(context.Background())
		require.NoError(t, s.publishRecordsTx(t.Context(), tx, records))
		if commit {
			require.NoError(t, tx.Commit(t.Context()))
		} else {
			require.NoError(t, tx.Rollback(t.Context()))
		}
	}
	publish(records, false)
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts`).Scan(&n))
	require.Zero(t, n)
	// Retain one runtime event, then append the other two in one allocation.
	seed := []sourcePublication{}
	runtimeRecords := []sourcePublication{}
	for _, record := range records {
		if record.Profile == RuntimeProfile {
			runtimeRecords = append(runtimeRecords, record)
		} else {
			seed = append(seed, record)
		}
	}
	slices.SortFunc(runtimeRecords, func(a, b sourcePublication) int { return strings.Compare(a.EventID, b.EventID) })
	seed = append(seed, runtimeRecords[0])
	publish(seed, true) // Mixed replay/new facts in the same profile, then full replay.
	input := slices.Clone(records)
	slices.Reverse(input)
	publish(input, true)
	publish(records, true)
	expected := map[string]sourcePublication{}
	for _, r := range records {
		expected[r.EventID] = r
	}
	rows, err := pool.Query(t.Context(), `SELECT profile,sequence,event_id,document,source_hash FROM execution_source_facts ORDER BY profile,sequence`)
	require.NoError(t, err)
	sequences := map[string]int64{}
	observed := map[string][]string{}
	for rows.Next() {
		var profile, event, hash string
		var seq int64
		var wire []byte
		require.NoError(t, rows.Scan(&profile, &seq, &event, &wire, &hash))
		p, ok := expected[event]
		require.True(t, ok)
		require.Equal(t, p.Wire, wire)
		require.Equal(t, eventconnector.HashSourceBytes(p.Wire), hash)
		sequences[profile]++
		require.Equal(t, sequences[profile], seq)
		observed[profile] = append(observed[profile], event)
		delete(expected, event)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Empty(t, expected)
	require.True(t, slices.IsSorted(observed[RuntimeProfile]), "shared-profile sequence retains event ordering")
	for profile, high := range sequences {
		var got int64
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT high_sequence FROM execution_source_streams WHERE profile=$1`, profile).Scan(&got))
		require.Equal(t, high, got)
	}
}

func TestGroupedPublicationPGOverflowRollsBackOtherProfiles(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	records := groupedPublicationFixture(t, s, org, project, "grouped-overflow")
	profiles := map[string]bool{}
	for _, p := range records {
		profiles[p.Profile] = true
	}
	for profile := range profiles {
		high := int64(0)
		if profile == RuntimeProfile {
			high = math.MaxInt64 - 1
		}
		_, err := pool.Exec(t.Context(), `INSERT INTO execution_source_streams(source_instance,org_id,project_id,profile,high_sequence) VALUES($1,$2,$3,$4,$5)`, s.SourceInstance(), org, project, profile, high)
		require.NoError(t, err)
	}
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	require.Error(t, s.publishRecordsTx(t.Context(), tx, records))
	require.NoError(t, tx.Rollback(t.Context()))
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts`).Scan(&count))
	require.Zero(t, count)
	for profile := range profiles {
		var high int64
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT high_sequence FROM execution_source_streams WHERE profile=$1`, profile).Scan(&high))
		if profile == RuntimeProfile {
			require.EqualValues(t, math.MaxInt64-1, high)
		} else {
			require.Zero(t, high)
		}
	}
}

func TestGroupedPublicationPGCommitOrder(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	started := time.Now()
	first := groupedPublicationFixture(t, s, org, project, "grouped-first")
	second := groupedPublicationFixture(t, s, org, project, "grouped-second")
	slices.Reverse(second)
	beginAt := time.Now()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	ownerPID := tx.Conn().PgConn().PID()
	t.Logf("publication_stage kind=ab role=owner pid=%d stage=begin duration_ns=%d", ownerPID, time.Since(beginAt).Nanoseconds())
	publicationAt := time.Now()
	require.NoError(t, s.publishRecordsTx(ctx, tx, first))
	t.Logf("publication_stage kind=ab role=owner pid=%d stage=publish duration_ns=%d", ownerPID, time.Since(publicationAt).Nanoseconds())
	// Pin one SELECT-only observer; owner + writer + observer use at most three
	// fixture connections. The existing pool configuration remains unchanged.
	observer, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer observer.Release()
	pid := make(chan uint32, 1)
	done := make(chan error, 1)
	writerExited := make(chan struct{})
	go func() {
		defer close(writerExited)
		beginAt := time.Now()
		other, e := pool.Begin(ctx)
		if e != nil {
			done <- e
			return
		}
		defer other.Rollback(context.Background())
		writerPID := other.Conn().PgConn().PID()
		t.Logf("publication_stage kind=ab role=writer pid=%d stage=begin duration_ns=%d", writerPID, time.Since(beginAt).Nanoseconds())
		pid <- writerPID
		publicationAt := time.Now()
		e = s.publishRecordsTx(ctx, other, second)
		t.Logf("publication_stage kind=ab role=writer pid=%d stage=publish duration_ns=%d ok=%t", writerPID, time.Since(publicationAt).Nanoseconds(), e == nil)
		if e == nil {
			commitAt := time.Now()
			e = other.Commit(ctx)
			t.Logf("publication_stage kind=ab role=writer pid=%d stage=commit duration_ns=%d ok=%t", writerPID, time.Since(commitAt).Nanoseconds(), e == nil)
		}
		t.Logf("publication_stage kind=ab role=writer pid=%d stage=finished elapsed_ns=%d ok=%t", writerPID, time.Since(started).Nanoseconds(), e == nil)
		done <- e
	}()
	defer func() {
		cancel()
		_ = tx.Rollback(context.Background())
		<-writerExited // Join before the fixture pool/clone can be cleaned up.
	}()
	var writer uint32
	select {
	case writer = <-pid:
	case e := <-done:
		require.NoError(t, e)
		t.Fatal("writer ended before locking")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Eventually(t, func() bool {
		var waiting bool
		e := observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted) AND $2::int=ANY(pg_blocking_pids($1))`, writer, ownerPID).Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	snapshot, err := publicationCommitLockSnapshot(ctx, observer, ownerPID, writer)
	require.NoError(t, err)
	t.Logf("publication_lock kind=ab sample=%s", snapshot)
	commitAt := time.Now()
	require.NoError(t, tx.Commit(ctx))
	t.Logf("publication_stage kind=ab role=owner pid=%d stage=commit duration_ns=%d", ownerPID, time.Since(commitAt).Nanoseconds())
	select {
	case e := <-done:
		require.NoError(t, e)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var requests []string
	require.NoError(t, observer.QueryRow(ctx, `SELECT array_agg(request_id ORDER BY sequence) FROM execution_source_facts WHERE profile=$1`, RuntimeProfile).Scan(&requests))
	require.Equal(t, []string{"grouped-first", "grouped-first", "grouped-first", "grouped-second", "grouped-second", "grouped-second"}, requests)
}

func TestInitialGapPGFrozenEpochAndLostCommit(t *testing.T) {
	for _, mode := range []string{"existing", "first_use", "epoch_change", "lost_commit", "first_use_lost_commit", "first_use_append_failure"} {
		t.Run(mode, func(t *testing.T) {
			p := setupCaptureServicePG(t, contentcapture.ModeDisabled, 128)
			if strings.HasPrefix(mode, "first_use") {
				oldProject := p.identity.ProjectID
				require.NoError(t, p.pool.QueryRow(t.Context(), `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Gap first use','gap-first-use','active','none') RETURNING id`, p.identity.OrgID).Scan(&p.identity.ProjectID))
				_, err := p.pool.Exec(t.Context(), `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason) SELECT org_id,$3,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, oldProject, p.identity.ProjectID)
				require.NoError(t, err)
			}
			appendOriginal := p.service.appendManifest
			calls := 0
			lost := errors.New("synthetic reply lost after committed initial gap")
			p.service.appendManifest = func(ctx context.Context, m ContentManifest, epoch int64) (ContentAppendResult, error) {
				calls++
				require.EqualValues(t, 1, epoch)
				if mode == "first_use_append_failure" {
					return ContentAppendResult{}, lost
				}
				if mode == "epoch_change" {
					_, err := p.pool.Exec(ctx, `UPDATE execution_content_streams SET recovery_epoch=recovery_epoch+1 WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID)
					require.NoError(t, err)
				}
				result, err := appendOriginal(ctx, m, epoch)
				if strings.HasSuffix(mode, "lost_commit") && err == nil {
					return ContentAppendResult{}, lost
				}
				return result, err
			}
			capture, err := p.service.Begin(t.Context(), p.identity)
			require.Nil(t, capture)
			require.Equal(t, 1, calls)
			if mode == "epoch_change" {
				require.ErrorIs(t, err, ErrContentEpochChanged)
				p.counts(t, 0, 0, 0, 0, 0)
				require.Zero(t, contentJournalHigh(t, p.pool, p.identity.OrgID, p.identity.ProjectID))
				return
			}
			if mode == "first_use_append_failure" {
				require.ErrorIs(t, err, lost)
				p.counts(t, 0, 0, 0, 0, 1)
				require.Zero(t, contentJournalHigh(t, p.pool, p.identity.OrgID, p.identity.ProjectID))
				return
			}
			if strings.HasSuffix(mode, "lost_commit") {
				require.ErrorIs(t, err, lost)
				p.counts(t, 0, 0, 0, 1, 1)
			} else {
				require.NoError(t, err)
				p.counts(t, 0, 0, 0, 1, 0)
			}
			values := p.manifests(t)
			require.Len(t, values, 1)
			require.Equal(t, "policy_disabled", values[0].Capture.Reason)
			require.Equal(t, "1", values[0].Capture.PolicyVersion)
			require.Empty(t, values[0].ContentRef)
			replay, e := appendOriginal(t.Context(), values[0], 1)
			require.NoError(t, e)
			require.True(t, replay.Replayed)
			require.EqualValues(t, 1, replay.Sequence)
		})
	}
}

// Only the two commit-order fixtures use this one-shot, SELECT-only snapshot.
// Exact clone/PIDs bound its scope. No query text, SQL arguments or credentials
// are selected. Transaction-ID locks need not name a relation; retain nulls.
func publicationCommitLockSnapshot(ctx context.Context, observer *pgxpool.Conn, owner, writer uint32) ([]byte, error) {
	var snapshot []byte
	err := observer.QueryRow(ctx, `WITH actors AS (
 SELECT pid,backend_start,xact_start,query_start,state,wait_event_type,wait_event,
        pg_blocking_pids(pid) AS blocking_pids
 FROM pg_stat_activity
 WHERE datid=(SELECT oid FROM pg_database WHERE datname=current_database())
   AND pid IN ($1,$2)
), locks AS (
 SELECT l.pid,l.locktype,l.database,l.relation AS relation_oid,
        n.nspname AS schema_name,c.relname AS relation_name,l.page,l.tuple,
        l.transactionid::text AS transactionid,l.virtualxid,l.mode,l.granted
 FROM pg_locks l LEFT JOIN pg_class c ON c.oid=l.relation
 LEFT JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE l.pid IN (SELECT pid FROM actors)
   AND (l.database IS NULL OR l.database=0 OR
        l.database=(SELECT oid FROM pg_database WHERE datname=current_database()))
)
SELECT jsonb_build_object('observed_at',clock_timestamp(),
 'database_oid',(SELECT oid FROM pg_database WHERE datname=current_database()),
 'owner_pid',$1::int,'writer_pid',$2::int,'observer_pid',pg_backend_pid(),
 'activity',COALESCE((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.pid) FROM actors a),'[]'::jsonb),
 'locks',COALESCE((SELECT jsonb_agg(to_jsonb(l) ORDER BY l.pid,l.locktype,l.relation_oid,l.page,l.tuple,l.mode) FROM locks l),'[]'::jsonb))`, owner, writer).Scan(&snapshot)
	return snapshot, err
}
