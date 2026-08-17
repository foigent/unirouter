//go:build integration

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

	requestID := "op_p6f20_gateway_evidence"
	attemptID := "att_p6f20_gateway_evidence"
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO freeze_idempotency (request_id, org_id, status, pricing_version)
		 VALUES ($1, $2, 'settled', 'p6f20-test')
		 RETURNING request_id`, requestID, orgID,
	).Scan(&requestID))
	_, err = pool.Exec(ctx,
		`INSERT INTO billing_provider_attempts (
			org_id, request_id, attempt_id, api_key_id, channel_id, channel_key_id,
			route_sequence, route_total, endpoint, provider, channel_name,
			state, outcome_certainty, http_status
		 ) VALUES ($1, $2, $3, 101, 201, 301, 1, 1,
			'/v1/chat/completions', 'provider-a', 'channel-a', 'settled', 'known_usage', 202)`,
		orgID, requestID, attemptID,
	)
	require.NoError(t, err)
	evidenceCtx := contextWithEvidenceSpanState(context.WithValue(ctx, middleware.CtxOrgID, orgID))
	evidenceCtx = contextWithAttemptID(evidenceCtx, attemptID)
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
	var upstreamStatus int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT upstream_status_code FROM call_logs WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&upstreamStatus))
	require.Equal(t, 202, upstreamStatus, "call-log materialization must preserve the authoritative provider-attempt status")

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

	// The synchronous fallback uses the same authoritative attempt lookup when
	// the durable outbox is unavailable.
	syncRequestID := "op_p6f20_gateway_evidence_sync"
	syncAttemptID := "att_p6f20_gateway_evidence_sync"
	_, err = pool.Exec(ctx,
		`INSERT INTO freeze_idempotency (request_id, org_id, status, pricing_version)
		 VALUES ($1, $2, 'settled', 'p6f20-test')`,
		syncRequestID, orgID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO billing_provider_attempts (
			org_id, request_id, attempt_id, api_key_id, channel_id, channel_key_id,
			route_sequence, route_total, endpoint, provider, channel_name,
			state, outcome_certainty, http_status
		 ) VALUES ($1, $2, $3, 102, 202, 302, 1, 1,
			'/v1/chat/completions', 'provider-b', 'channel-b', 'settled', 'known_usage', 204)`,
		orgID, syncRequestID, syncAttemptID,
	)
	require.NoError(t, err)
	relay.outbox = nil
	syncEvidenceCtx := contextWithAttemptID(context.WithValue(ctx, middleware.CtxOrgID, orgID), syncAttemptID)
	relay.recordRequest(
		syncEvidenceCtx, syncRequestID, orgID, 0, 0, "provider-b", "channel-b",
		"model-b", "model-b", "127.0.0.1", 10, 2, 3, 200, 20, 0,
		false, metering.UsageSourceUpstream, 12, "upstream-b",
	)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT upstream_status_code FROM call_logs WHERE org_id=$1 AND request_id=$2`, orgID, syncRequestID,
	).Scan(&upstreamStatus))
	require.Equal(t, 204, upstreamStatus, "synchronous call-log fallback must preserve the authoritative provider-attempt status")
}
