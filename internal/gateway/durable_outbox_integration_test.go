//go:build integration

package gateway

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/executionprojection"
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
	evidenceCtx = contextWithDurableLegacyProviderAttempt(evidenceCtx, orgID, requestID, 201)
	relay.recordTraceEvent(evidenceCtx, requestID, "gateway.request.accepted", "gateway", "info", "model-a", "model-a", nil, 0, 3, map[string]interface{}{
		"outbox_version": outbox.Version,
	})
	relay.recordRequest(
		evidenceCtx, requestID, orgID, 0, 201, "provider-a", "channel-a",
		"model-a", "model-a", "127.0.0.1", 10, 2, 3, 200, 20, 0,
		false, metering.UsageSourceUpstream, 12, "upstream-a",
	)

	var queued, logs, traces int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM internal_outbox_events`).Scan(&queued))
	require.Equal(t, 3, queued, "one call log and two trace envelopes should be durable before materialization")
	var exactProjectionPayloads int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM internal_outbox_events
 WHERE payload->>'attempt_id'=$1
   AND payload#>>'{route_projection,source}'='precontract_provider_attempt'`, attemptID).Scan(&exactProjectionPayloads))
	require.Equal(t, 3, exactProjectionPayloads,
		"every durable envelope must carry the exact attempt projection prepared by the synchronous path")
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
	var routeChannelID int64
	var routeSource string
	var routeVersion *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT upstream_status_code,route_channel_id,route_evidence_source,route_evidence_version
		   FROM call_logs WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&upstreamStatus, &routeChannelID, &routeSource, &routeVersion))
	require.Equal(t, 202, upstreamStatus, "call-log materialization must preserve the authoritative provider-attempt status")
	require.Equal(t, int64(201), routeChannelID, "call-log materialization must preserve the immutable route identity")
	require.Equal(t, "precontract_provider_attempt", routeSource)
	require.Nil(t, routeVersion)
	var matchingTraceProjections int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM request_trace_events
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
   AND route_evidence_source='precontract_provider_attempt' AND route_channel_id=201`,
		orgID, requestID, attemptID).Scan(&matchingTraceProjections))
	require.Equal(t, 2, matchingTraceProjections,
		"call-log and trace outbox consumers must materialize the same exact pre-contract attempt identity")

	var createdAt time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT created_at FROM call_logs WHERE org_id=$1 AND request_id=$2`, orgID, requestID,
	).Scan(&createdAt))
	require.WithinDuration(t, time.Now().UTC(), createdAt, 5*time.Second)

	// A duplicate producer call after the original outbox row was removed is
	// still harmless because each destination keeps its durable event key.
	relay.recordRequest(
		evidenceCtx, requestID, orgID, 0, 201, "provider-a", "channel-a",
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
	syncEvidenceCtx = contextWithDurableLegacyProviderAttempt(syncEvidenceCtx, orgID, syncRequestID, 202)
	relay.recordRequest(
		syncEvidenceCtx, syncRequestID, orgID, 0, 202, "provider-b", "channel-b",
		"model-b", "model-b", "127.0.0.1", 10, 2, 3, 200, 20, 0,
		false, metering.UsageSourceUpstream, 12, "upstream-b",
	)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT upstream_status_code,route_channel_id,route_evidence_source,route_evidence_version
		   FROM call_logs WHERE org_id=$1 AND request_id=$2`, orgID, syncRequestID,
	).Scan(&upstreamStatus, &routeChannelID, &routeSource, &routeVersion))
	require.Equal(t, 204, upstreamStatus, "synchronous call-log fallback must preserve the authoritative provider-attempt status")
	require.Equal(t, int64(202), routeChannelID, "synchronous call-log fallback must preserve the immutable route identity")
	require.Equal(t, "precontract_provider_attempt", routeSource)
	require.Nil(t, routeVersion)
}

func TestPreDispatchTraceEvidencePersistsWithoutProviderAttemptReceipt(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('pre-dispatch-trace-projection','active','customer') RETURNING id`).Scan(&orgID))

	relay := &Relay{
		db: pool, traceEventsEnabled: true, traceEventSampleRate: 1,
		payloadCaptureMode: traceutil.PayloadCaptureModeMetadataOnly,
	}
	events := []struct {
		eventType string
		stage     string
	}{
		{eventType: "provider.key_decrypt_failed", stage: "provider"},
		{eventType: "provider.adapter_unknown", stage: "provider"},
		{eventType: "provider.request_build_failed", stage: "provider"},
		{eventType: "routing.probe_claim_failed", stage: "routing"},
		{eventType: "routing.route_selected", stage: "routing"},
		{eventType: "billing.provider_attempt.begin_failed", stage: "billing"},
	}
	record := func(requestID, attemptID string) {
		evidenceCtx := contextWithPreDispatchProviderAttempt(
			context.WithValue(ctx, middleware.CtxOrgID, orgID), attemptID,
		)
		for _, event := range events {
			relay.recordTraceEvent(
				evidenceCtx, requestID, event.eventType, event.stage, "warning",
				"model-a", "model-a", nil, 0, 1, nil,
			)
		}
	}

	record("pre-dispatch-sync", "att_pre_dispatch_sync")

	options := outbox.DefaultOptions()
	options.WorkerCount = 1
	options.BatchSize = 20
	service, err := outbox.New(pool, options)
	require.NoError(t, err)
	require.NoError(t, service.RegisterHandler(outbox.EventTrace, relay.ProcessTraceOutbox))
	relay.outbox = service
	record("pre-dispatch-outbox", "att_pre_dispatch_outbox")
	processed, err := service.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, len(events), processed)

	var total, explicitNoAttempt, retainedCorrelation, compactIdentity int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*),
       COUNT(*) FILTER (WHERE route_evidence_source='no_provider_attempt'),
       COUNT(*) FILTER (WHERE attempt_id IN ('att_pre_dispatch_sync','att_pre_dispatch_outbox')),
       COUNT(*) FILTER (WHERE route_evidence_version IS NOT NULL
                         OR route_evidence_hash IS NOT NULL
                         OR route_deployment_id IS NOT NULL
                         OR route_connection_id IS NOT NULL
                         OR route_credential_binding_id IS NOT NULL)
  FROM request_trace_events
 WHERE org_id=$1 AND request_id IN ('pre-dispatch-sync','pre-dispatch-outbox')`, orgID,
	).Scan(&total, &explicitNoAttempt, &retainedCorrelation, &compactIdentity))
	require.Equal(t, len(events)*2, total)
	require.Equal(t, total, explicitNoAttempt)
	require.Equal(t, total, retainedCorrelation)
	require.Zero(t, compactIdentity, "pre-dispatch correlation must not manufacture receipt identity")

	// A durable dispatch/response contract still fails closed when the exact
	// immutable provider-attempt receipt is absent.
	for _, eventType := range []string{"provider.dispatch_started", "provider.response_accepted"} {
		_, err := relay.prepareTraceMaterialization(ctx, traceOutboxPayload{
			OrgID: orgID, RequestID: "durable-missing-receipt", AttemptID: "att_missing_receipt",
			ProviderAttemptStage: string(providerAttemptStageDurable), EventType: eventType,
			SourceEventHash: strings.Repeat("e", 64),
		})
		require.ErrorIs(t, err, executionprojection.ErrExpectedAttemptMissing)
	}
}

func TestExecutionProjectionMaterializesExplicitNoAttemptSources(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('projection-no-attempt','active','customer') RETURNING id`).Scan(&orgID))
	relay := &Relay{
		db: pool, traceEventsEnabled: true, traceEventSampleRate: 1,
		payloadCaptureMode: traceutil.PayloadCaptureModeMetadataOnly,
	}
	evidenceCtx := context.WithValue(ctx, middleware.CtxOrgID, orgID)

	relay.recordRequestWithUsage(
		evidenceCtx, "projection-cache-call", orgID, 0, 0, "", "", "model", "model", "",
		adapters.NewResponseCacheUsage(), 0, 200, 1, 0, false, 0, "", nil,
	)
	relay.recordTraceEvent(
		evidenceCtx, "projection-cache-trace", "response_cache.hit", "cache", "info",
		"model", "model", nil, 200, 1, nil,
	)
	relay.recordRequest(
		evidenceCtx, "projection-no-route-call", orgID, 0, 0, "", "", "model", "model", "",
		0, 0, 0, 503, 1, 0, false, metering.UsageSourceNoRoute, 0, "",
	)
	relay.recordTraceEvent(
		evidenceCtx, "projection-local-trace", "auth.model_not_allowed", "auth", "warning",
		"model", "model", nil, 403, 1, map[string]any{"error_code": "auth_error.model_not_allowed"},
	)

	for _, test := range []struct {
		table, requestID, source string
	}{
		{table: "call_logs", requestID: "projection-cache-call", source: "response_cache"},
		{table: "request_trace_events", requestID: "projection-cache-trace", source: "response_cache"},
		{table: "call_logs", requestID: "projection-no-route-call", source: "no_provider_attempt"},
		{table: "request_trace_events", requestID: "projection-local-trace", source: "no_provider_attempt"},
	} {
		var actualSource string
		var attemptID *string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT route_evidence_source,attempt_id FROM `+test.table+` WHERE org_id=$1 AND request_id=$2`,
			orgID, test.requestID).Scan(&actualSource, &attemptID))
		require.Equal(t, test.source, actualSource)
		require.Nil(t, attemptID, "a local terminal source must not manufacture a provider attempt identity")
	}
}
