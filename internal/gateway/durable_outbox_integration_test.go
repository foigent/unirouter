package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/traceutil"
)

func TestP6F20GatewayEvidenceIsReplayableAndIdempotent(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ($1, 'active', 'standard') RETURNING id`, "p6f20-gateway-outbox",
	).Scan(&orgID))

	options := outbox.DefaultOptions()
	options.WorkerCount = 1
	options.BatchSize = 20
	service, err := outbox.New(pool, options)
	require.NoError(t, err)
	relay := &Relay{
		db:                   pool,
		outbox:               service,
		traceEventsEnabled:   true,
		traceEventSampleRate: 1,
		payloadCaptureMode:   traceutil.PayloadCaptureModeMetadataOnly,
	}
	require.NoError(t, service.RegisterHandler(outbox.EventTrace, relay.ProcessTraceOutbox))
	require.NoError(t, service.RegisterHandler(outbox.EventCallLog, relay.ProcessCallLogOutbox))

	evidenceCtx := contextWithEvidenceSpanState(context.WithValue(ctx, middleware.CtxOrgID, orgID))
	requestID := "op_p6f20_gateway_evidence"
	relay.recordTraceEvent(evidenceCtx, requestID, "gateway.request.accepted", "gateway", "info", "model-a", "model-a", nil, 0, 3, map[string]interface{}{
		"outbox_version": outbox.Version,
	})
	relay.recordRequest(
		evidenceCtx, requestID, orgID, 0, 0, "provider-a", "channel-a",
		"model-a", "model-a", "127.0.0.1", 10, 2, 3, 200, 20, 0,
		false, metering.UsageSourceUpstream, 12, "upstream-a",
	)

	var queued, logs, traces int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM internal_outbox_events`).Scan(&queued))
	require.Equal(t, 3, queued, "one call log and two trace envelopes should be durable before materialization")
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs`).Scan(&logs))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_trace_events`).Scan(&traces))
	require.Zero(t, logs)
	require.Zero(t, traces)

	processed, err := service.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, processed)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs`).Scan(&logs))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_trace_events`).Scan(&traces))
	require.Equal(t, 1, logs)
	require.Equal(t, 2, traces)

	var createdAt time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT created_at FROM call_logs WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&createdAt))
	require.WithinDuration(t, time.Now().UTC(), createdAt, 5*time.Second)

	// A duplicate producer call after the original outbox row was removed is
	// still harmless because each destination keeps its durable event key.
	relay.recordRequest(
		evidenceCtx, requestID, orgID, 0, 0, "provider-a", "channel-a",
		"model-a", "model-a", "127.0.0.1", 10, 2, 3, 200, 20, 0,
		false, metering.UsageSourceUpstream, 12, "upstream-a",
	)
	processed, err = service.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, processed)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs`).Scan(&logs))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_trace_events`).Scan(&traces))
	require.Equal(t, 1, logs)
	require.Equal(t, 3, traces, "the duplicate producer attempt remains visible as a distinct trace while the call log stays singular")
}
