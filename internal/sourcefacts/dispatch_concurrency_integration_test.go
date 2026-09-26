//go:build integration

package sourcefacts

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func dispatchExpandHooks(t *testing.T) migrate.CompatibleExpandHooks {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.ObservationExpandHead+".up.sql"))
	require.NoError(t, err)
	return migrate.CompatibleExpandHooks{Version: schemaauthority.ObservationExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return nil }}
}

func TestSourceDispatchExpansionCoversExpiredRecoveryAndConcurrentRenew(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	require.NoError(t, budgetDown(t, pool))
	require.NoError(t, observationDown(t, pool))
	ctx := t.Context()
	dir := testutil.MigrationsDir(t)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	full := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	old := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	old.Implementations = slices.DeleteFunc(old.Implementations, func(ref modelprofile.ImplementationReference) bool { return ref.Kind == "source_dispatch" })
	oldIdentity := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}
	_, _, err = runtimegeneration.Renew(ctx, pool, oldIdentity, old, ledger, true)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 hour' WHERE instance_id=$1`, oldIdentity.Instance)
	require.NoError(t, err)
	hooks := dispatchExpandHooks(t)
	require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, dir, hooks), "all serving and recovery generations")
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped' WHERE instance_id=$1`, oldIdentity.Instance)
	require.NoError(t, err)
	current := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("c", 32), Artifact: "sha256:" + strings.Repeat("d", 64)}
	_, _, err = runtimegeneration.Renew(ctx, pool, current, full, ledger, false)
	require.NoError(t, err)
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	hooks.After = func(ctx context.Context, tx pgx.Tx) error {
		close(ready)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	migrated := make(chan error, 1)
	go func() { migrated <- migrate.RunCompatibleExpand(ctx, pool, dir, hooks) }()
	select {
	case <-ready:
	case e := <-migrated:
		t.Fatalf("expand stopped before held commit: %v", e)
	case <-time.After(5 * time.Second):
		t.Fatal("expand did not reach commit barrier")
	}
	renewed, bootstrap := make(chan error, 1), make(chan error, 1)
	go func() { _, _, e := runtimegeneration.Renew(ctx, pool, current, full, ledger, false); renewed <- e }()
	go func() {
		identity := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("e", 32), Artifact: oldIdentity.Artifact}
		_, _, e := runtimegeneration.Renew(ctx, pool, identity, old, ledger, false)
		bootstrap <- e
	}()
	require.Eventually(t, func() bool {
		var waiting int
		e := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE relation='runtime_generations'::regclass AND NOT granted`).Scan(&waiting)
		return e == nil && waiting >= 2
	}, 3*time.Second, 10*time.Millisecond, "both actual Renew paths must wait while the fleet guard is being installed")
	unblock()
	require.NoError(t, <-migrated)
	require.NoError(t, <-renewed)
	require.ErrorContains(t, <-bootstrap, "require the source dispatch handler")
	_, _, err = runtimegeneration.Renew(ctx, pool, oldIdentity, old, ledger, true)
	require.ErrorContains(t, err, "require the source dispatch handler", "expired recovery cannot bypass the installed guard")
}

func TestSourceDispatchDownCannotRaceUncommittedEnqueue(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	require.NoError(t, budgetDown(t, pool))
	pool, s, org, project := setupSourceDBWithPool(t, pool)
	docs := frozenDispatchFixture(t, s, org, project, "down-race", time.Now().UTC().Truncate(time.Microsecond))
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	require.NoError(t, s.enqueueDispatchTx(t.Context(), tx, docs[0].SubjectID, docs))
	require.ErrorContains(t, observationDown(t, pool), "could not obtain lock", "ledger lock closes the queue-check/insert race")
	require.NoError(t, tx.Commit(t.Context()))
	require.ErrorContains(t, observationDown(t, pool), "source dispatch events remain")
}

func TestSourceDispatchRCConcurrentPublishAndRRSnapshotVisibility(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	ctx := t.Context()
	events := make([]outbox.Event, 4)
	var registered Observation
	for i := range events {
		docs := frozenDispatchFixture(t, s, org, project, fmt.Sprintf("publish-concurrent-%d", i), time.Now().UTC().Truncate(time.Microsecond))
		if i == 0 {
			for _, d := range docs {
				if d.Profile == DestinationProfile {
					registered = d
				}
			}
		}
		events[i] = ownFrozenDispatch(t, pool, insertFrozenDispatch(t, pool, s, docs))
	}
	observationSnapshotAttempt(t, pool, s, org, project, events[0].RequestID)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	request := events[0].RequestID
	attempt := "synthetic-attempt-" + request
	_, before, found, err := s.measurementAttemptSnapshot(ctx, tx, org, project, request, attempt, registered, true)
	require.NoError(t, err)
	require.True(t, found)
	results := make(chan error, len(events))
	for _, e := range events {
		go func(e outbox.Event) { results <- s.ProcessDispatchEvent(ctx, e) }(e)
	}
	for range events {
		require.NoError(t, <-results, "same-project publication uses RC; no RR serialization retry is needed")
	}
	// Use the original worker to acknowledge the marked event, not a direct delete.
	_, err = pool.Exec(ctx, `UPDATE internal_outbox_events SET claimed_until=clock_timestamp()-interval '1 second' WHERE event_type=$1`, outbox.EventSourceDispatch)
	require.NoError(t, err)
	worker, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, worker.RegisterHandler(outbox.EventSourceDispatch, s.ProcessDispatchEvent))
	_, err = worker.DrainOnce(ctx)
	require.NoError(t, err)
	_, after, found, err := s.measurementAttemptSnapshot(ctx, tx, org, project, request, attempt, registered, true)
	require.NoError(t, err)
	require.True(t, found, "an old RR snapshot still sees frozen input after publication and ack")
	require.Equal(t, before, after)
	require.NoError(t, tx.Commit(ctx))
	var wire []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE event_id=$1`, before.EventID).Scan(&wire))
	decoded, err := DecodeObservation(wire)
	require.NoError(t, err)
	require.Equal(t, before, decoded, "a new snapshot sees the exact published origin")
	expectedWire, err := EncodeObservation(before)
	require.NoError(t, err)
	require.Equal(t, expectedWire, wire, "published canonical bytes match the frozen snapshot")
	var pending, bad int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM internal_outbox_events WHERE event_type=$1`, outbox.EventSourceDispatch).Scan(&pending))
	require.Zero(t, pending, "original worker acknowledged all four events")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_streams s WHERE s.high_sequence<>(SELECT count(*) FROM execution_source_facts f WHERE f.source_instance=s.source_instance AND f.org_id=s.org_id AND f.project_id=s.project_id AND f.profile=s.profile) OR s.high_sequence<>COALESCE((SELECT max(sequence) FROM execution_source_facts f WHERE f.source_instance=s.source_instance AND f.org_id=s.org_id AND f.project_id=s.project_id AND f.profile=s.profile),0)`).Scan(&bad))
	require.Zero(t, bad, "all stream sequences commit atomically without gaps")
}
