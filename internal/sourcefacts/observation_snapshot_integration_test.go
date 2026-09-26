//go:build integration

package sourcefacts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/executionmeasurement"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/outbox"
)

// A second real transaction commits after the first statement's rows have been
// read, while the original capture transaction is still open. No sleeps, retry,
// production hook or capture-budget override is needed to force this schedule.
type observationSnapshotDB struct {
	db.DBQuerier
	needle string
	after  func(context.Context)
}

func (d observationSnapshotDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := d.DBQuerier.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &observationSnapshotTx{Tx: tx, needle: d.needle, after: d.after}, nil
}

type observationSnapshotTx struct {
	pgx.Tx
	needle string
	after  func(context.Context)
}

func (tx *observationSnapshotTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := tx.Tx.QueryRow(ctx, sql, args...)
	if tx.after != nil && strings.Contains(sql, tx.needle) {
		f := tx.after
		tx.after = nil
		return observationSnapshotRow{row, func() { f(ctx) }}
	}
	return row
}

type observationSnapshotRow struct {
	pgx.Row
	after func()
}

func (r observationSnapshotRow) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	r.after()
	return nil
}

func observationSnapshotAttempt(t *testing.T, pool *pgxpool.Pool, s *Service, org, project int64, request string) (executioncontext.Snapshot, *billing.Billing, string) {
	t.Helper()
	ctx := t.Context()
	var key int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status,allowed_models) VALUES($1,$2,$3,repeat('d',64),'ur-snapshot','active','synthetic-unpriced') RETURNING id`, org, project, request).Scan(&key))
	snap := testSnapshot()
	snap.Core.OrgID = executioncontext.ID(org)
	snap.Core.ProjectID = executioncontext.ID(project)
	snap.Core.APIKeyID = executioncontext.ID(key)
	snap.Core.RequestID = request
	b := billing.NewBilling(pool, nil).WithMode(billing.BillingModeCostObservation)
	b.SetReservationMaterializer(s)
	freeze, err := b.Freeze(executioncontext.WithSnapshot(ctx, snap), billing.FreezeRequest{RequestID: request, OrgID: org, ProjectID: project, APIKeyID: key, Model: "synthetic-unpriced", EstimatedTokens: 30, ReservedCompletionTokens: 10, SafetyFactor: "1"})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	attempt := "synthetic-attempt-" + request
	require.NoError(t, b.BeginProviderAttempt(ctx, billing.ProviderAttemptStart{RequestID: request, AttemptID: attempt, OrgID: org, APIKeyID: key, ChannelID: 201, ChannelKeyID: 301, RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions", Provider: "synthetic", Channel: "synthetic"}))
	return snap, b, attempt
}

func TestIndependentObservationWritersConcurrentPublicationPG(t *testing.T) {
	for _, phase := range []string{"admission", "measurement", "committed"} {
		t.Run(phase, func(t *testing.T) {
			pool, s, org, project := setupSourceDB(t)
			s.options.ObservationsEnabled = true
			snap, b, attempt := observationSnapshotAttempt(t, pool, s, org, project, "rc-target")
			require.NoError(t, b.CompleteProviderAttempt(t.Context(), billing.ProviderAttemptCompletion{OrgID: org, RequestID: snap.Core.RequestID, AttemptID: attempt, Outcome: billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200}))
			docs := frozenDispatchFixture(t, s, org, project, snap.Core.RequestID, time.Now().UTC().Truncate(time.Microsecond))
			insertFrozenDispatch(t, pool, s, docs)
			competitor := func(ctx context.Context, request string) {
				x := snap
				x.Core.RequestID = request
				require.NoError(t, s.CaptureAuthenticated(ctx, x))
				other, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer other.Rollback(context.Background())
				require.NoError(t, s.publishTogetherTx(ctx, other, nil, frozenDispatchFixture(t, s, org, project, request, time.Now().UTC().Truncate(time.Microsecond))))
				require.NoError(t, other.Commit(ctx))
			}
			competitor(t.Context(), "rc-stream-seed")
			needle := map[string]string{"admission": "SELECT EXISTS(SELECT 1 FROM freeze_idempotency", "measurement": "jsonb_agg", "committed": "LEFT JOIN LATERAL"}[phase]
			interleavings := 0
			writer := *s
			writer.db = observationSnapshotDB{DBQuerier: pool, needle: needle, after: func(ctx context.Context) { interleavings++; competitor(ctx, "rc-competing-commit") }}
			var err error
			switch phase {
			case "admission":
				snap.Core.RequestID = "rc-rejected"
				err = writer.CaptureAdmissionRejection(t.Context(), snap, 429)
			case "measurement":
				n := int64(12)
				err = writer.CaptureMeasurements(t.Context(), snap, []executionmeasurement.Sample{{Kind: executionmeasurement.HTTPRoundTrip, AttemptID: attempt, State: "observed", Nanoseconds: &n, HTTPStatus: 200}})
			case "committed":
				err = writer.CaptureCommitted(t.Context(), billing.CommittedObservation{Kind: "provider_result", OrgID: org, RequestID: snap.Core.RequestID, AttemptID: attempt, Outcome: billing.ProviderAttemptOutcomeResponseAccepted})
			}
			require.NoError(t, err)
			require.Equal(t, 1, interleavings)
			stage := map[string]string{"admission": "admission_rejected", "measurement": "round_trip_measurement", "committed": "provider_response_received"}[phase]
			var count int
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage=$2`, snap.Core.RequestID, stage).Scan(&count))
			require.Equal(t, 1, count)
			var bad int
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_streams s WHERE s.high_sequence<>(SELECT count(*) FROM execution_source_facts f WHERE f.source_instance=s.source_instance AND f.org_id=s.org_id AND f.project_id=s.project_id AND f.profile=s.profile) OR s.high_sequence<>COALESCE((SELECT max(sequence) FROM execution_source_facts f WHERE f.source_instance=s.source_instance AND f.org_id=s.org_id AND f.project_id=s.project_id AND f.profile=s.profile),0)`).Scan(&bad))
			require.Zero(t, bad)
			var status string
			var charged int64
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT status,charged_cost_micros FROM freeze_idempotency WHERE request_id='rc-target'`).Scan(&status, &charged))
			require.Equal(t, "frozen", status)
			require.Zero(t, charged)
		})
	}
}

func TestMeasurementSingleSnapshotPublishAckHandoffPG(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	s.options.ObservationsEnabled = true
	snap, _, attempt := observationSnapshotAttempt(t, pool, s, org, project, "snapshot-handoff")
	docs := frozenDispatchFixture(t, s, org, project, snap.Core.RequestID, time.Now().UTC().Truncate(time.Microsecond))
	event := ownFrozenDispatch(t, pool, insertFrozenDispatch(t, pool, s, docs))
	var registered Observation
	for _, d := range docs {
		if d.Profile == DestinationProfile {
			registered = d
		}
	}
	worker, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, worker.RegisterHandler(outbox.EventSourceDispatch, s.ProcessDispatchEvent))
	poolTx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	require.NoError(t, err)
	defer poolTx.Rollback(t.Context())
	tx := &observationSnapshotTx{Tx: poolTx, needle: "jsonb_agg", after: func(ctx context.Context) {
		require.NoError(t, s.ProcessDispatchEvent(ctx, event))
		_, err := pool.Exec(ctx, `UPDATE internal_outbox_events SET claimed_until=clock_timestamp()-interval '1 second' WHERE id=$1`, event.ID)
		require.NoError(t, err)
		_, err = worker.DrainOnce(ctx)
		require.NoError(t, err)
	}}
	_, before, found, err := s.measurementAttemptSnapshot(t.Context(), tx, org, project, snap.Core.RequestID, attempt, registered, true)
	require.NoError(t, err)
	require.True(t, found)
	_, after, found, err := s.measurementAttemptSnapshot(t.Context(), tx, org, project, snap.Core.RequestID, attempt, registered, true)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, registered, before)
	require.Equal(t, before, after)
	var pending int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM internal_outbox_events WHERE id=$1`, event.ID).Scan(&pending))
	require.Zero(t, pending)
	require.NoError(t, poolTx.Commit(t.Context()))
}

func TestCommittedObservationAdvancedStateRejectedPG(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	s.options.ObservationsEnabled = true
	snap, b, attempt := observationSnapshotAttempt(t, pool, s, org, project, "advanced-state")
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), billing.ProviderAttemptCompletion{OrgID: org, RequestID: snap.Core.RequestID, AttemptID: attempt, Outcome: billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200}))
	require.NoError(t, b.CompleteProviderAttempt(t.Context(), billing.ProviderAttemptCompletion{OrgID: org, RequestID: snap.Core.RequestID, AttemptID: attempt, Outcome: billing.ProviderAttemptOutcomeResponseUnusable, HTTPStatus: 200, FailureKind: "response_invalid"}))
	err := s.CaptureCommitted(t.Context(), billing.CommittedObservation{Kind: "provider_result", OrgID: org, RequestID: snap.Core.RequestID, AttemptID: attempt, Outcome: billing.ProviderAttemptOutcomeResponseAccepted})
	require.ErrorContains(t, err, "advanced")
	var status, state string
	var charged int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT r.status,r.charged_cost_micros,p.state FROM freeze_idempotency r JOIN billing_provider_attempts p USING(org_id,request_id) WHERE r.request_id=$1`, snap.Core.RequestID).Scan(&status, &charged, &state))
	require.Equal(t, "settlement_pending", status)
	require.Equal(t, billing.ProviderAttemptOutcomeResponseUnusable, state)
	require.Zero(t, charged)
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage='provider_response_received'`, snap.Core.RequestID).Scan(&n))
	require.Zero(t, n)
}

func TestCommittedObservationReceiptSnapshotAndAdvancePG(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	s.options.ObservationsEnabled = true
	snap, b, attempt := observationSnapshotAttempt(t, pool, s, org, project, "receipt-advance")
	ctx := t.Context()
	require.NoError(t, b.CompleteProviderAttempt(ctx, billing.ProviderAttemptCompletion{OrgID: org, RequestID: snap.Core.RequestID, AttemptID: attempt, Outcome: billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200}))
	usage := billing.SettleRequest{RequestID: snap.Core.RequestID, AttemptID: attempt, OrgID: org, PromptTokens: 20, UncachedPromptTokens: 20, CompletionTokens: 5, UsageSource: metering.UsageSourceUpstream, UsageAuthority: metering.UsageAuthorityProviderReported, UsagePurpose: metering.UsagePurposeCustomerCharge, UsageFinality: metering.UsageFinalityFinal, UsageContractVersion: metering.UsageContractVersion, CacheUsageStatus: "not_reported"}
	prepared, err := b.PrepareNonStreamDelivery(ctx, usage)
	require.NoError(t, err)
	require.True(t, prepared.Funded)
	delivery := billing.DeliveryEvent{OrgID: org, RequestID: snap.Core.RequestID, AttemptID: attempt, Mode: billing.DeliveryModeNonStream, Outcome: billing.DeliveryOutcomeWriteStarted, HTTPStatus: 200}
	require.NoError(t, b.RecordDelivery(ctx, delivery))
	old := billing.CommittedObservation{Kind: "local_delivery", OrgID: org, RequestID: snap.Core.RequestID, AttemptID: attempt, Outcome: billing.DeliveryOutcomeWriteStarted}
	// Advance the durable original writer after the capture statement's snapshot.
	// The retained event must still use the old receipt, not a second current read.
	writer := *s
	writer.db = observationSnapshotDB{DBQuerier: pool, needle: "LEFT JOIN LATERAL", after: func(ctx context.Context) {
		delivery.Outcome = billing.DeliveryOutcomeCompleted
		delivery.DeliveredBytes = 12
		delivery.DeliveredChunks = 1
		require.NoError(t, b.RecordDelivery(ctx, delivery))
	}}
	require.NoError(t, writer.CaptureCommitted(ctx, old))
	var docWire []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='delivery_started'`, snap.Core.RequestID, RuntimeProfile).Scan(&docWire))
	doc, err := DecodeObservation(docWire)
	require.NoError(t, err)
	require.Equal(t, billing.DeliveryOutcomeWriteStarted, doc.Runtime.LocalDelivery)
	require.ErrorContains(t, s.CaptureCommitted(ctx, old), "advanced", "a new snapshot must not reconstruct the previous event from its old receipt")
	current := old
	current.Outcome = billing.DeliveryOutcomeCompleted
	require.NoError(t, s.CaptureCommitted(ctx, current))
	var status string
	var charged int64
	var receipts int
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,charged_cost_micros,(SELECT count(*) FROM request_delivery_receipts WHERE request_id=$1) FROM freeze_idempotency WHERE request_id=$1`, snap.Core.RequestID).Scan(&status, &charged, &receipts))
	require.Equal(t, "settlement_pending", status)
	require.Zero(t, charged)
	require.Equal(t, 2, receipts)
}
