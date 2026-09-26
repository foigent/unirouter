//go:build integration

package sourcefacts

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	assets "github.com/uniroute/unirouter/contracts/source-facts/v2"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/testutil"
)

func frozenDispatchFixture(t *testing.T, s *Service, org, project int64, request string, at time.Time) []Observation {
	t.Helper()
	var vectors struct {
		Valid []struct {
			Canonical string `json:"canonical_utf8"`
		} `json:"valid"`
	}
	require.NoError(t, json.Unmarshal(assets.Vectors(), &vectors))
	documents := []Observation{}
	for _, v := range vectors.Valid {
		d, err := DecodeObservation([]byte(v.Canonical))
		require.NoError(t, err)
		if d.Kind != "attempt_registered" && d.Kind != "selection" && d.Kind != "dispatch_registered" {
			continue
		}
		d.SourceInstance = s.SourceInstance()
		d.OrgID = executioncontext.ID(org)
		d.ProjectID = executioncontext.ID(project)
		d.RequestID = request
		d.SubjectKind = "provider_attempt"
		d.SubjectID = "synthetic-attempt-" + request
		d.OccurredAt = executioncontext.Time(at)
		d.RecordedAt = d.OccurredAt
		if d.Attempt != nil {
			d.Attempt.AttemptID = d.SubjectID
		}
		d.SourceFactID = d.factID()
		d.EventID = d.eventID()
		_, err = EncodeObservation(d)
		require.NoError(t, err)
		documents = append(documents, d)
	}
	require.Len(t, documents, 3)
	return documents
}

func insertFrozenDispatch(t *testing.T, pool *pgxpool.Pool, s *Service, docs []Observation) outbox.Event {
	t.Helper()
	tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	require.NoError(t, s.enqueueDispatchTx(t.Context(), tx, docs[0].SubjectID, docs))
	require.NoError(t, tx.Commit(t.Context()))
	var event outbox.Event
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT id,event_key,event_type,priority,org_id,request_id,payload,occurred_at FROM internal_outbox_events WHERE event_type=$1 AND request_id=$2`, outbox.EventSourceDispatch, docs[0].RequestID).Scan(&event.ID, &event.Key, &event.Type, &event.Priority, &event.OrgID, &event.RequestID, &event.Payload, &event.OccurredAt))
	return event
}

func ownFrozenDispatch(t *testing.T, pool *pgxpool.Pool, event outbox.Event) outbox.Event {
	t.Helper()
	event.ClaimedBy = "synthetic-dispatch-worker"
	_, err := pool.Exec(t.Context(), `UPDATE internal_outbox_events SET claimed_by=$2,claimed_until=clock_timestamp()+interval '1 minute' WHERE id=$1`, event.ID, event.ClaimedBy)
	require.NoError(t, err)
	return event
}

func TestSourceDispatchOutboxAtomicRecoveryAndRetention(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	docs := frozenDispatchFixture(t, s, org, project, "frozen-recovery", time.Now().UTC().Truncate(time.Microsecond).Add(-10*24*time.Hour))
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	require.NoError(t, s.enqueueDispatchTx(t.Context(), tx, docs[0].SubjectID, docs))
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM internal_outbox_events WHERE event_type=$1`, outbox.EventSourceDispatch).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, tx.Rollback(t.Context()))
	event := insertFrozenDispatch(t, pool, s, docs)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts`).Scan(&count))
	require.Zero(t, count, "enqueue does not allocate source sequence")
	_, _, err = s.decodeDispatch(event)
	require.NoError(t, err, "JSONB round trip preserves base64-frozen exact bytes")
	_, err = pool.Exec(t.Context(), `UPDATE internal_outbox_events SET payload=jsonb_set(payload,'{materialized}','true') WHERE id=$1`, event.ID)
	require.Error(t, err)
	event = ownFrozenDispatch(t, pool, event)
	s.options.Enabled = false
	s.options.ObservationsEnabled = false
	require.NoError(t, s.CheckDispatchRecoveryReady(t.Context()))
	require.NoError(t, s.ProcessDispatchEvent(t.Context(), event), "already committed evidence recovers with both new capture flags disabled")
	for _, doc := range docs {
		var wire []byte
		var published time.Time
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT document,published_at FROM execution_source_facts WHERE event_id=$1`, doc.EventID).Scan(&wire, &published))
		expected, e := EncodeObservation(doc)
		require.NoError(t, e)
		require.Equal(t, expected, wire)
		require.WithinDuration(t, time.Now(), published, time.Minute, "late first publication keeps actual new retention time")
	}
	_, err = pool.Exec(t.Context(), `UPDATE internal_outbox_events SET payload=jsonb_set(payload,'{materialized}','false') WHERE id=$1`, event.ID)
	require.Error(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE internal_outbox_events SET payload=jsonb_set(payload,'{request_id}','"changed"') WHERE id=$1`, event.ID)
	require.Error(t, err)
	s.options.Enabled = true
	deleted, err := s.DeleteExpired(t.Context(), time.Now().Add(8*24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 3, deleted, "synthetic future cutoff exercises the real retention writer")
	s.options.Enabled = false
	_, err = pool.Exec(t.Context(), `UPDATE internal_outbox_events SET claimed_until=clock_timestamp()+interval '1 minute' WHERE id=$1`, event.ID)
	require.NoError(t, err, "a marked event lease no longer requires retained source rows")
	require.NoError(t, s.ProcessDispatchEvent(t.Context(), event))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts`).Scan(&count))
	require.Zero(t, count, "marked replay never resurrects expired source")
	_, err = pool.Exec(t.Context(), `UPDATE internal_outbox_events SET claimed_until=clock_timestamp()-interval '1 second' WHERE id=$1`, event.ID)
	require.NoError(t, err)
	worker, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, worker.RegisterHandler(outbox.EventSourceDispatch, s.ProcessDispatchEvent))
	n, err := worker.DrainOnce(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM internal_outbox_events WHERE id=$1`, event.ID).Scan(&count))
	require.Zero(t, count)
	require.ErrorIs(t, s.ProcessDispatchEvent(t.Context(), event), errDispatchLeaseLost, "stale deleted event cannot reconstruct source")
}

func TestSourceDispatchLeaseIdentityAndDownGuards(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	require.NoError(t, budgetDown(t, pool))
	pool, s, org, project := setupSourceDBWithPool(t, pool)
	event := insertFrozenDispatch(t, pool, s, frozenDispatchFixture(t, s, org, project, "frozen-lease", time.Now().UTC().Truncate(time.Microsecond)))
	require.ErrorContains(t, observationDown(t, pool), "source dispatch events remain")
	event = ownFrozenDispatch(t, pool, event)
	require.ErrorContains(t, observationDown(t, pool), "source dispatch events remain")
	lost := event
	lost.ClaimedBy = "different-worker"
	require.ErrorIs(t, s.ProcessDispatchEvent(t.Context(), lost), errDispatchLeaseLost)
	_, err := pool.Exec(t.Context(), `UPDATE internal_outbox_events SET claimed_until=clock_timestamp()-interval '1 second',dead_lettered_at=clock_timestamp() WHERE id=$1`, event.ID)
	require.NoError(t, err)
	require.ErrorContains(t, observationDown(t, pool), "source dispatch events remain")
	require.ErrorIs(t, s.ProcessDispatchEvent(t.Context(), event), errDispatchLeaseLost)
	_, err = pool.Exec(t.Context(), `UPDATE internal_outbox_events SET dead_lettered_at=NULL WHERE id=$1`, event.ID)
	require.NoError(t, err)
	event = ownFrozenDispatch(t, pool, event)
	s.options.SourceInstance = "wrong-source"
	require.Error(t, s.CheckDispatchRecoveryReady(t.Context()))
	require.Error(t, s.ProcessDispatchEvent(t.Context(), event))
	s.options.SourceInstance = "synthetic-ur"
	require.NoError(t, s.ProcessDispatchEvent(t.Context(), event))
	require.ErrorContains(t, observationDown(t, pool), "source dispatch events remain", "materialized-awaiting-ack is still a guarded event")
}

func TestSourceDispatchFrozenRecordValidation(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	event := insertFrozenDispatch(t, pool, s, frozenDispatchFixture(t, s, org, project, "frozen-validation", time.Now().UTC().Truncate(time.Microsecond)))
	var p dispatchPayload
	require.NoError(t, json.Unmarshal(event.Payload, &p))
	for _, mutate := range []func(*dispatchPayload){
		func(p *dispatchPayload) { p.Records[0].BytesBase64 = "broken" },
		func(p *dispatchPayload) { p.ProjectID = "999" },
		func(p *dispatchPayload) { p.Records[0].SourceHash = "sha256:" + strings.Repeat("0", 64) },
		func(p *dispatchPayload) { p.Materialized = true },
	} {
		var altered dispatchPayload
		require.NoError(t, json.Unmarshal(event.Payload, &altered))
		mutate(&altered)
		bad := event
		var err error
		bad.Payload, err = json.Marshal(altered)
		require.NoError(t, err)
		_, _, err = s.decodeDispatch(bad)
		require.Error(t, err)
	}
}

func TestSourceDispatchMaximumRecordAndAggregateBounds(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	s.options.SourceInstance = strings.Repeat("s", 128)
	docs := frozenDispatchFixture(t, s, org, project, strings.Repeat("r", 64), time.Now().UTC().Truncate(time.Microsecond))
	var destination int
	for i := range docs {
		d := &docs[i]
		d.SubjectID = strings.Repeat("a", 128)
		d.SourceFactID, d.EventID = d.factID(), d.eventID()
		if d.Attempt != nil {
			d.Attempt.AttemptID = d.SubjectID
			d.Attempt.Operation = strings.Repeat("\\", 128)
			d.Attempt.EvidenceRef = strings.Repeat("\\", 256)
		}
		if c := d.Capability; c != nil {
			c.ModelRef, c.Operation, c.Surface = strings.Repeat("\\", 128), strings.Repeat("\\", 128), strings.Repeat("\\", 128)
			c.Provider = strings.Repeat("\\", 64)
			c.Protocol = ProfileReference{ID: strings.Repeat("\\", 128), Version: strings.Repeat("\\", 64), SHA256: "sha256:" + strings.Repeat("f", 64)}
			c.ExecutionProfile = c.Protocol
			c.ReleaseRef, c.BindingRef, c.ProbeRef, c.ApprovalRef = strings.Repeat("\\", 256), strings.Repeat("\\", 256), strings.Repeat("\\", 256), strings.Repeat("\\", 256)
			c.RequiredCapabilities, c.Capabilities = []string{}, []CapabilityReference{}
			for n := 0; n < 128; n++ {
				c.RequiredCapabilities = append(c.RequiredCapabilities, fmt.Sprintf("%03d", n)+strings.Repeat("\\", 77))
			}
			for n := 0; n < 64; n++ {
				c.Capabilities = append(c.Capabilities, CapabilityReference{CapabilityID: fmt.Sprintf("%03d", n) + strings.Repeat("\\", 77), SupportState: "enabled", EvidenceState: strings.Repeat("\\", 32), EvidenceRefSHA256: "sha256:" + strings.Repeat("f", 64), VerifiedAt: d.OccurredAt, Revision: "9223372036854775807"})
			}
		}
		if d.Destination != nil {
			destination = i
		}
	}
	// The largest admitted destination document is a deliberately conservative
	// contract boundary, not a claim that a real provider accepts this hostname.
	docs[destination].Destination.Origin = "https://x"
	wire, err := EncodeObservation(docs[destination])
	require.NoError(t, err)
	docs[destination].Destination.Origin += strings.Repeat("x", MaxDocumentBytes-len(wire))
	wire, err = EncodeObservation(docs[destination])
	require.NoError(t, err)
	require.Len(t, wire, MaxDocumentBytes)
	for _, d := range docs {
		wire, err := EncodeObservation(d)
		require.NoError(t, err)
		t.Logf("max boundary %s: %d bytes", d.Profile, len(wire))
	}
	event := insertFrozenDispatch(t, pool, s, docs)
	var before, after int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT octet_length(payload::text) FROM internal_outbox_events WHERE id=$1`, event.ID).Scan(&before))
	require.LessOrEqual(t, before, dispatchPayloadLimit-dispatchMarkerReserve)
	require.NoError(t, s.ProcessDispatchEvent(t.Context(), ownFrozenDispatch(t, pool, event)))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT octet_length(payload::text) FROM internal_outbox_events WHERE id=$1`, event.ID).Scan(&after))
	require.Less(t, after, dispatchPayloadLimit)
	t.Logf("real JSONB bytes: pending=%d marked=%d limit=%d", before, after, dispatchPayloadLimit)
	// Oversized source input fails before another event can commit.
	docs[destination].Destination.Origin += "x"
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	require.ErrorContains(t, s.enqueueDispatchTx(t.Context(), tx, docs[0].SubjectID, docs), "exceeds byte limit")
	require.NoError(t, tx.Rollback(t.Context()))
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM internal_outbox_events WHERE event_type=$1`, outbox.EventSourceDispatch).Scan(&count))
	require.Equal(t, 1, count)
}

func TestSourceDispatchLeaseExpiresBeforePublicationCommit(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	event := ownFrozenDispatch(t, pool, insertFrozenDispatch(t, pool, s, frozenDispatchFixture(t, s, org, project, "lease-at-commit", time.Now().UTC().Truncate(time.Microsecond))))
	_, err := pool.Exec(t.Context(), `CREATE FUNCTION delay_dispatch_publication() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.stage='attempt_registered' THEN PERFORM pg_sleep(0.12); END IF; RETURN NEW; END$$; CREATE TRIGGER delay_dispatch_publication BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION delay_dispatch_publication()`)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE internal_outbox_events SET claimed_until=clock_timestamp()+interval '90 milliseconds' WHERE id=$1`, event.ID)
	require.NoError(t, err)
	require.ErrorIs(t, s.ProcessDispatchEvent(t.Context(), event), errDispatchLeaseLost)
	var count int
	var marked bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts`).Scan(&count))
	require.Zero(t, count, "all publications roll back when the lease expires during work")
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT (payload->>'materialized')::boolean FROM internal_outbox_events WHERE id=$1`, event.ID).Scan(&marked))
	require.False(t, marked)
}
