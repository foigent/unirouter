//go:build integration

package gateway_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

// Separate concurrent journal publication from the known original same-route
// health-transaction conflict. Ingress is sequential; the four original outbox
// consumers compete on the same project and publish all three terminal profiles.
// The final authenticated pull also checks delivery and records its duration;
// ingress remains sequential, so this does not establish concurrent HTTP capacity.
func TestSourceFactsOriginalOutboxConcurrentPublicationIntegration(t *testing.T) {
	f := gateway.NewSourceFactsGatewayFixture(t, modeloperation.IDResponses)
	source, _, raw := sourceGatewayAttachSource(t, f)
	defer sourceGatewayObserveReceiver(t, f, source)()
	handler := sourceGatewayHandler(t, f, f.Relay, source)
	const requests = 32
	for range requests {
		r := httptest.NewRequest(http.MethodPost, f.Path, strings.NewReader(string(f.Body)))
		r.Header.Set("Authorization", "Bearer "+raw)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NotEmpty(t, w.Header().Get(middleware.OperationIDHeader))
	}
	var early, terminals int
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_source_facts),(SELECT count(*) FROM completed_execution_feed_items)`).Scan(&early, &terminals))
	require.Equal(t, requests*4, early)
	require.Zero(t, terminals)
	var replay outbox.Event
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT id,event_key,event_type,priority,org_id,request_id,payload,occurred_at FROM internal_outbox_events WHERE event_type=$1 ORDER BY id LIMIT 1`, outbox.EventBillingSettled).Scan(&replay.ID, &replay.Key, &replay.Type, &replay.Priority, &replay.OrgID, &replay.RequestID, &replay.Payload, &replay.OccurredAt))

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	start := make(chan struct{})
	done := make(chan error, 4)
	for range 4 {
		options := outbox.DefaultOptions()
		options.BatchSize = 4 // Both existing priority lanes must receive a slot.
		worker, err := outbox.New(f.Pool, options)
		require.NoError(t, err)
		for _, kind := range []string{outbox.EventBillingUsagePrepared, outbox.EventBillingSettled, outbox.EventBillingCancelled} {
			require.NoError(t, worker.RegisterHandler(kind, f.Billing.ProcessUsageJournalEvent))
		}
		go func() {
			<-start
			for {
				n, err := worker.DrainOnce(ctx)
				if err != nil || n == 0 {
					done <- err
					return
				}
			}
		}()
	}
	close(start)
	for range 4 {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	var remaining, unsettled, badGroups int
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM internal_outbox_events),(SELECT count(*) FROM freeze_idempotency WHERE status<>'settled'),(SELECT count(*) FROM completed_execution_feed_items)`).Scan(&remaining, &unsettled, &terminals))
	require.Zero(t, remaining)
	require.Zero(t, unsettled)
	require.Equal(t, requests, terminals)
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT request_id FROM execution_source_facts GROUP BY request_id HAVING count(*)<>7 OR count(*) FILTER(WHERE stage='terminal')<>3 OR count(DISTINCT published_at) FILTER(WHERE stage='terminal')<>1) t`).Scan(&badGroups))
	require.Zero(t, badGroups)
	var i01Order []string
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT array_agg(request_id ORDER BY id) FROM completed_execution_feed_items`).Scan(&i01Order))
	for _, profile := range sourcefacts.Profiles() {
		var order []string
		var high, count, distinct int64
		require.NoError(t, f.Pool.QueryRow(ctx, `SELECT array_agg(request_id ORDER BY sequence) FROM execution_source_facts WHERE profile=$1 AND stage='terminal'`, profile).Scan(&order))
		require.Equal(t, i01Order, order, "the original V1 order must agree with every terminal profile")
		require.NoError(t, f.Pool.QueryRow(ctx, `SELECT s.high_sequence,count(f.sequence),count(DISTINCT f.sequence) FROM execution_source_streams s JOIN execution_source_facts f USING(source_instance,org_id,project_id,profile) WHERE s.profile=$1 GROUP BY s.high_sequence`, profile).Scan(&high, &count, &distinct))
		require.Equal(t, high, count)
		require.Equal(t, count, distinct)
	}
	var mismatched int
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts s JOIN completed_execution_feed_items f USING(org_id,project_id,request_id) WHERE s.profile=$1 AND convert_from(s.document,'UTF8')::jsonb->'cost'->'economic_reference'->>'snapshot_sha256' IS DISTINCT FROM f.snapshot_sha256`, sourcefacts.CostProfile).Scan(&mismatched))
	require.Zero(t, mismatched)
	before := sourceRecoveryTableDigests(t, f.Pool)
	require.NoError(t, f.Billing.ProcessUsageJournalEvent(ctx, replay))
	require.Equal(t, before, sourceRecoveryTableDigests(t, f.Pool))
	require.EqualValues(t, requests, f.ProviderCalls())
}
