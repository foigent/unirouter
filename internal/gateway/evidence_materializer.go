package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionprojection"
	"github.com/uniroute/unirouter/internal/metering"
)

type routeProjectionValues struct {
	Source                     string
	Version                    any
	AuthorityMode              any
	EvidenceHash               any
	PublicModelID              any
	DeploymentID               any
	ConnectionID               any
	CredentialBindingID        any
	ModelOperationID           any
	RuntimeReleaseID           any
	SnapshotHash               any
	LegacyChannelID            int64
	PublicExecutionBindingID   any
	DeliveryContractVersion    any
	ChargeContractVersion      any
	IdempotencyContractVersion any
}

type traceProjectionMode uint8

const (
	traceProjectionNoProviderAttempt traceProjectionMode = iota
	traceProjectionResponseCache
	traceProjectionExactProviderAttempt
)

// These bounded contracts are only a compatibility bridge for trace envelopes
// written before provider_attempt_stage was added. New producers carry the
// explicit pre_dispatch/durable stage and never infer receipt existence from an
// attempt ID or a display name.
var preDispatchTraceEvents = map[string]struct{}{
	"provider.key_decrypt_failed":           {},
	"provider.adapter_unknown":              {},
	"provider.request_build_failed":         {},
	"routing.probe_claim_failed":            {},
	"routing.probe_lease_unavailable":       {},
	"billing.provider_attempt.begin_failed": {},
}

var responseCacheTraceEvents = map[string]struct{}{
	"response_cache.hit":           {}, // legacy test/fixture name
	"response_cache.hit.delivered": {},
}

var durableProviderTraceEvents = map[string]struct{}{
	"provider.dispatch_started":                      {},
	"provider.request_failed":                        {},
	"provider.response_accepted":                     {},
	"provider.response_parse_failed":                 {},
	"provider.response_rejected":                     {},
	"provider.response_unusable":                     {},
	"provider.usage.observed":                        {},
	"billing.provider_attempt.complete_failed":       {},
	"billing.guardrail_hold.failed":                  {},
	"billing.delivery_prepare.failed":                {},
	"billing.settle.pending_recovery":                {},
	"billing.settle.succeeded":                       {},
	"billing.stream_reservation_extension.denied":    {},
	"billing.stream_reservation_extension.succeeded": {},
	"billing.cancelled.no_billable_output":           {},
	"billing.settlement_usage.failed":                {},
	"delivery.receipt.failed":                        {},
	"guardrail.transform.failed":                     {},
	"guardrail.stream_advisory.completed":            {},
	"guardrail.stream_advisory.gap":                  {},
	"logging.call_log.queued":                        {},
	"logging.call_log.recorded":                      {},
	"stream.terminated":                              {},
	"usage.provider_missing":                         {},
}

func projectionValues(value executionprojection.Projection, fallbackChannelID int64) (routeProjectionValues, error) {
	normalized, err := executionprojection.Normalize(value)
	if err != nil {
		return routeProjectionValues{}, err
	}
	result := routeProjectionValues{Source: string(normalized.Source), LegacyChannelID: fallbackChannelID}
	switch normalized.Source {
	case executionprojection.SourceProviderAttempt:
		compact := normalized.Compact
		result.Version = compact.ContractVersion
		result.AuthorityMode = string(compact.AuthorityMode)
		result.EvidenceHash = compact.EvidenceHash
		result.PublicModelID = nullablePositiveInt64(compact.PublicModelID)
		result.DeploymentID = nullablePositiveInt64(compact.DeploymentID)
		result.ConnectionID = nullablePositiveInt64(compact.ConnectionID)
		result.CredentialBindingID = nullablePositiveInt64(compact.CredentialBindingID)
		result.ModelOperationID = compact.ModelOperationID
		result.RuntimeReleaseID = nullablePositiveInt64(compact.RuntimeReleaseID)
		result.SnapshotHash = nullableString(compact.SnapshotHash)
		result.LegacyChannelID = compact.LegacyChannelID
		result.PublicExecutionBindingID = nullablePositiveInt64(compact.PublicExecutionBindingID)
		result.DeliveryContractVersion = nullableString(compact.DeliveryContractVersion)
		result.ChargeContractVersion = nullableString(compact.ChargeContractVersion)
		result.IdempotencyContractVersion = nullableString(compact.IdempotencyContractVersion)
	case executionprojection.SourcePrecontractProviderAttempt:
		result.LegacyChannelID = normalized.LegacyChannelID
	case executionprojection.SourceResponseCache, executionprojection.SourceNoProviderAttempt:
		// A selected route may still be useful routing context, but it is never
		// promoted into an execution receipt when no upstream attempt occurred.
	default:
		return routeProjectionValues{}, fmt.Errorf("unsupported execution projection source %q", normalized.Source)
	}
	return result, nil
}

func (value routeProjectionValues) compactArgs() []any {
	return []any{
		value.Source, value.Version, value.AuthorityMode, value.EvidenceHash,
		value.PublicModelID, value.DeploymentID, value.ConnectionID,
		value.CredentialBindingID, value.ModelOperationID, value.RuntimeReleaseID,
		value.SnapshotHash, value.PublicExecutionBindingID,
		value.DeliveryContractVersion, value.ChargeContractVersion,
		value.IdempotencyContractVersion,
	}
}

func nullablePositiveInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableInt64Pointer(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableIntPointer(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func (relay *Relay) prepareTraceMaterialization(ctx context.Context, payload traceOutboxPayload) (traceOutboxPayload, error) {
	mode, err := traceProjectionContract(payload)
	if err != nil {
		return payload, err
	}
	source := executionprojection.SourceNoProviderAttempt
	lookupAttempt := false
	required := false
	switch mode {
	case traceProjectionResponseCache:
		source = executionprojection.SourceResponseCache
	case traceProjectionExactProviderAttempt:
		lookupAttempt = true
		required = true
	case traceProjectionNoProviderAttempt:
	default:
		return payload, fmt.Errorf("unsupported trace projection mode %d", mode)
	}
	prepared, hash, err := relay.prepareExecutionProjection(
		ctx, payload.OrgID, payload.RequestID, payload.AttemptID, source, lookupAttempt, required,
		payload.ChannelID, payload.RouteProjection, payload.SourceEventHash,
	)
	if err != nil {
		return payload, fmt.Errorf("prepare trace execution projection: %w", err)
	}
	payload.RouteProjection = prepared
	payload.SourceEventHash = hash
	return payload, nil
}

func traceProjectionContract(payload traceOutboxPayload) (traceProjectionMode, error) {
	// A prepared envelope already contains the immutable classification and its
	// source hash is bound to that projection. This also preserves replay of
	// envelopes created before provider_attempt_stage existed.
	if payload.RouteProjection.Source != "" {
		normalized, err := executionprojection.Normalize(payload.RouteProjection)
		if err != nil {
			return 0, err
		}
		switch normalized.Source {
		case executionprojection.SourceProviderAttempt, executionprojection.SourcePrecontractProviderAttempt:
			return traceProjectionExactProviderAttempt, nil
		case executionprojection.SourceResponseCache:
			return traceProjectionResponseCache, nil
		case executionprojection.SourceNoProviderAttempt:
			return traceProjectionNoProviderAttempt, nil
		default:
			return 0, fmt.Errorf("unsupported prepared trace projection source %q", normalized.Source)
		}
	}

	eventType := strings.TrimSpace(payload.EventType)
	attemptID := strings.TrimSpace(payload.AttemptID)
	stage := providerAttemptStage(strings.TrimSpace(payload.ProviderAttemptStage))
	_, isPreDispatchEvent := preDispatchTraceEvents[eventType]
	_, isResponseCacheEvent := responseCacheTraceEvents[eventType]
	_, requiresDurableAttempt := durableProviderTraceEvents[eventType]

	if isResponseCacheEvent {
		if stage == providerAttemptStageDurable {
			return 0, fmt.Errorf("trace event %q cannot use a durable provider attempt", eventType)
		}
		return traceProjectionResponseCache, nil
	}

	switch stage {
	case providerAttemptStagePreDispatch:
		if attemptID == "" {
			return 0, fmt.Errorf("trace pre-dispatch stage requires an attempt correlation ID")
		}
		if requiresDurableAttempt {
			return 0, fmt.Errorf("trace event %q requires a durable provider attempt: %w", eventType, executionprojection.ErrExpectedAttemptMissing)
		}
		return traceProjectionNoProviderAttempt, nil
	case providerAttemptStageDurable:
		if attemptID == "" {
			return 0, executionprojection.ErrExpectedAttemptMissing
		}
		if isPreDispatchEvent {
			return 0, fmt.Errorf("pre-dispatch trace event %q cannot claim a durable provider attempt", eventType)
		}
		return traceProjectionExactProviderAttempt, nil
	case "":
		// Compatibility for unprepared legacy envelopes is deliberately bounded.
		// Ambiguous attempt-bearing events fail closed rather than guessing.
		if isPreDispatchEvent {
			return traceProjectionNoProviderAttempt, nil
		}
		if requiresDurableAttempt {
			if attemptID == "" {
				return 0, executionprojection.ErrExpectedAttemptMissing
			}
			return traceProjectionExactProviderAttempt, nil
		}
		if attemptID != "" {
			return 0, fmt.Errorf("trace event %q has an attempt ID but no provider-attempt stage contract", eventType)
		}
		return traceProjectionNoProviderAttempt, nil
	default:
		return 0, fmt.Errorf("unsupported provider-attempt stage %q", payload.ProviderAttemptStage)
	}
}

func (relay *Relay) prepareCallLogMaterialization(ctx context.Context, payload callLogOutboxPayload) (callLogOutboxPayload, error) {
	source := executionprojection.SourceNoProviderAttempt
	required := true
	switch payload.UsageSource {
	case metering.UsageSourceResponseCache:
		source = executionprojection.SourceResponseCache
		required = false
	case metering.UsageSourceNoRoute:
		required = false
	}
	prepared, hash, err := relay.prepareExecutionProjection(
		ctx, payload.OrgID, payload.RequestID, payload.AttemptID, source, required, required,
		payload.ChannelID, payload.RouteProjection, payload.SourceEventHash,
	)
	if err != nil {
		return payload, fmt.Errorf("prepare call-log execution projection: %w", err)
	}
	payload.RouteProjection = prepared
	payload.SourceEventHash = hash
	return payload, nil
}

func (relay *Relay) prepareExecutionProjection(
	ctx context.Context,
	orgID int64,
	requestID, attemptID string,
	noAttemptSource executionprojection.Source,
	lookupAttempt bool,
	attemptRequired bool,
	expectedLegacyChannelID int64,
	existing executionprojection.Projection,
	baseHash string,
) (executionprojection.Projection, string, error) {
	if existing.Source != "" {
		normalized, err := executionprojection.Normalize(existing)
		return normalized, baseHash, err
	}
	if relay == nil || relay.db == nil {
		return executionprojection.Projection{}, "", fmt.Errorf("gateway evidence database is unavailable")
	}
	if !lookupAttempt {
		if attemptRequired {
			return executionprojection.Projection{}, "", fmt.Errorf("attempt lookup cannot be disabled for required evidence")
		}
		resolved, err := executionprojection.Normalize(executionprojection.Projection{Source: noAttemptSource})
		if err != nil {
			return executionprojection.Projection{}, "", err
		}
		boundHash, err := executionprojection.BindSourceEventHash(baseHash, resolved)
		if err != nil {
			return executionprojection.Projection{}, "", err
		}
		return resolved, boundHash, nil
	}
	if resolved, ok := executionProjectionFromContext(ctx, orgID, requestID, attemptID); ok {
		actualLegacyChannelID := resolved.LegacyChannelID
		if resolved.Compact != nil {
			actualLegacyChannelID = resolved.Compact.LegacyChannelID
		}
		if expectedLegacyChannelID > 0 && actualLegacyChannelID != expectedLegacyChannelID {
			return executionprojection.Projection{}, "", fmt.Errorf("context provider attempt route mismatch")
		}
		boundHash, err := executionprojection.BindSourceEventHash(baseHash, resolved)
		if err != nil {
			return executionprojection.Projection{}, "", err
		}
		return resolved, boundHash, nil
	}
	input := executionprojection.ResolveInput{
		OrgID: orgID, RequestID: requestID, AttemptID: attemptID,
		NoAttemptSource: noAttemptSource, AttemptRequired: attemptRequired,
		ExpectedLegacyChannelID: expectedLegacyChannelID,
	}
	var resolved executionprojection.Projection
	var err error
	if resolver, ok := relay.db.(executionprojection.Resolver); ok {
		resolved, err = resolver.ResolveExecutionProjection(ctx, input)
	} else {
		resolved, err = executionprojection.Resolve(ctx, relay.db, input)
	}
	if err != nil {
		return executionprojection.Projection{}, "", err
	}
	boundHash, err := executionprojection.BindSourceEventHash(baseHash, resolved)
	if err != nil {
		return executionprojection.Projection{}, "", err
	}
	return resolved, boundHash, nil
}

func (relay *Relay) materializeTrace(ctx context.Context, payload traceOutboxPayload, outboxEventKey string) error {
	values, err := projectionValues(payload.RouteProjection, payload.ChannelID)
	if err != nil {
		return err
	}
	args := []any{
		payload.RequestID, nullableString(payload.ClientRequestID), nullableString(payload.IdempotencyKeyHash),
		payload.OrgID, nullableInt64Pointer(payload.ProjectID), nullableInt64Pointer(payload.APIKeyID), nullableInt64Pointer(payload.OwnerUserID), payload.KeyType,
		payload.EventType, payload.Stage, payload.Severity, payload.Model, payload.RequestedModel, payload.Provider, payload.Channel,
		nullableIntPointer(payload.StatusCode), payload.LatencyMS, string(payload.Metadata),
		payload.TraceID, payload.SpanID, nullableString(payload.ParentSpanID), payload.SchemaVersion, payload.SourceComponent, payload.CaptureMode,
		nullableString(payload.CaptureGap), payload.EvidenceRef, nullableString(payload.UpstreamRequestID), nullableString(payload.DecisionKind), nullableString(payload.DecisionResult), nullableString(payload.DecisionReason),
		payload.ActorType, nullableString(payload.ActorRef), nullableString(payload.OnBehalfOf), nullableString(payload.SessionID), nullableString(payload.WorkflowID), nullableString(payload.AgentID), nullableString(payload.AttemptID),
		payload.PayloadCaptureMode, payload.SourceEventHash, nullableString(outboxEventKey), payload.OccurredAt,
	}
	args = append(args, values.compactArgs()...)
	args = append(args, nullablePositiveInt64(values.LegacyChannelID))
	_, err = relay.db.Exec(ctx, `INSERT INTO request_trace_events (
		request_id,client_request_id,idempotency_key_hash,
		org_id,project_id,api_key_id,owner_user_id,key_type,
		event_type,stage,severity,model,requested_model,provider,channel,
		status_code,latency_ms,metadata,
		trace_id,span_id,parent_span_id,schema_version,source_component,capture_mode,
		capture_gap,evidence_ref,upstream_request_id,decision_kind,decision_result,decision_reason,
		actor_type,actor_ref,on_behalf_of,session_id,workflow_id,agent_id,attempt_id,
		payload_capture_mode,source_event_hash,outbox_event_key,created_at,
		route_evidence_source,route_evidence_version,route_authority_mode,route_evidence_hash,
		route_public_model_id,route_deployment_id,route_connection_id,route_credential_binding_id,
		route_model_operation_id,route_runtime_release_id,route_snapshot_hash,
		route_public_execution_binding_id,route_delivery_contract_version,
		route_charge_contract_version,route_idempotency_contract_version,route_channel_id
	) VALUES (
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
		$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,
		$38,$39,$40,$41,$42,$43,$44,$45,$46,$47,$48,$49,$50,$51,$52,$53,$54,$55,$56,$57
	) ON CONFLICT (outbox_event_key) DO NOTHING`, args...)
	return err
}

func (relay *Relay) materializeCallLog(ctx context.Context, eventType string, payload callLogOutboxPayload, outboxEventKey string) error {
	moneyValues, err := normalizeCallLogMoney(eventType, payload)
	if err != nil {
		return err
	}
	unitRates, err := billing.CanonicalUnitRates(payload.EffectiveUnitRates)
	if err != nil {
		return fmt.Errorf("canonical call-log unit rates: %w", err)
	}
	meterUsage, err := metering.NormalizeMeterQuantities(payload.MeterUsage)
	if err != nil {
		return fmt.Errorf("canonical call-log meter usage: %w", err)
	}
	unitRatesJSON, err := json.Marshal(unitRates)
	if err != nil {
		return fmt.Errorf("encode call-log unit rates: %w", err)
	}
	meterUsageJSON, err := json.Marshal(meterUsage)
	if err != nil {
		return fmt.Errorf("encode call-log meter usage: %w", err)
	}
	values, err := projectionValues(payload.RouteProjection, payload.ChannelID)
	if err != nil {
		return err
	}
	args := []any{
		payload.RequestID, nullableString(payload.ClientRequestID), nullableString(payload.IdempotencyKeyHash),
		payload.OrgID, nullableInt64Pointer(payload.ProjectID), payload.APIKeyID, nullableInt64Pointer(payload.OwnerUserID), payload.KeyType,
		payload.Model, payload.RequestedModel, payload.Provider, payload.Channel, values.LegacyChannelID,
		payload.PromptTokens, payload.UncachedPromptTokens, payload.CacheReadTokens, payload.CacheCreationTokens,
		payload.CompletionTokens, payload.PromptTokens + payload.CompletionTokens,
		moneyValues.CostMicros, payload.StatusCode, payload.LatencyMS, payload.TTFTMS, payload.UsageSource,
		payload.UsageAuthority, payload.UsagePurpose, payload.UsageFinality, payload.UsageContractVersion, payload.UsageEstimateVersion,
		payload.CacheUsageSchema, payload.CacheUsageStatus, payload.PricingVersion,
		moneyValues.EffectiveInputPriceMicrosPerMillion, moneyValues.EffectiveCacheReadPriceMicrosPerMillion,
		moneyValues.EffectiveCacheCreatePriceMicrosPerMillion, moneyValues.EffectiveOutputPriceMicrosPerMillion,
		moneyValues.CacheReadCostMicros, moneyValues.CacheCreationCostMicros,
		payload.IsStream, payload.IPAddress, payload.EstimatedTokens, payload.UsageConfidence,
		payload.TraceID, nullableString(payload.SessionID), payload.SchemaVersion, payload.CaptureMode, payload.EvidenceRef,
		nullableString(payload.UpstreamRequestID), nullableString(payload.WorkflowID), nullableString(payload.AgentID), nullableString(payload.AttemptID),
		payload.ActorType, nullableString(payload.ActorRef), nullableString(payload.OnBehalfOf), payload.PayloadCaptureMode, payload.SourceEventHash,
		nullableString(outboxEventKey), payload.OccurredAt,
	}
	args = append(args, string(unitRatesJSON), string(meterUsageJSON), moneyValues.NonTokenCostMicros)
	args = append(args, values.compactArgs()...)
	_, err = relay.db.Exec(ctx, `INSERT INTO call_logs (
		request_id,client_request_id,idempotency_key_hash,
		org_id,project_id,api_key_id,owner_user_id,key_type,
		model,requested_model,provider,channel,route_channel_id,
		prompt_tokens,uncached_prompt_tokens,cache_read_tokens,cache_creation_tokens,
		completion_tokens,total_tokens,cost_micros,status_code,upstream_status_code,latency_ms,ttft_ms,usage_source,
		usage_authority,usage_purpose,usage_finality,usage_contract_version,usage_estimate_version,
		cache_usage_schema,cache_usage_status,pricing_version,
		effective_input_price_micros_per_million,effective_cache_read_price_micros_per_million,
		effective_cache_creation_price_micros_per_million,effective_output_price_micros_per_million,
		cache_read_cost_micros,cache_creation_cost_micros,
		effective_unit_rates,meter_usage,non_token_cost_micros,
		is_stream,ip_address,estimated_tokens,usage_confidence,
		trace_id,session_id,schema_version,capture_mode,evidence_ref,upstream_request_id,
		workflow_id,agent_id,attempt_id,actor_type,actor_ref,on_behalf_of,payload_capture_mode,source_event_hash,
		outbox_event_key,created_at,
		route_evidence_source,route_evidence_version,route_authority_mode,route_evidence_hash,
		route_public_model_id,route_deployment_id,route_connection_id,route_credential_binding_id,
		route_model_operation_id,route_runtime_release_id,route_snapshot_hash,
		route_public_execution_binding_id,route_delivery_contract_version,
		route_charge_contract_version,route_idempotency_contract_version
	) SELECT
		$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,0),
		$14,$15,$16,$17,$18,$19,$20,$21,
		(SELECT attempt.http_status FROM billing_provider_attempts attempt
		  WHERE attempt.org_id=$4::bigint AND attempt.request_id=$1::varchar(64)
		    AND attempt.attempt_id=$51::varchar(64) LIMIT 1),
		$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,
		$59::jsonb,$60::jsonb,$61,
		$39,$40,$41,$42,$43,$44,$45,$46,$47,$48,$49,$50,$51,$52,$53,$54,$55,$56,$57,$58,
		$62,$63,$64,$65,$66,$67,$68,$69,$70,$71,$72,$73,$74,$75,$76
	WHERE NOT EXISTS (
		SELECT 1 FROM call_logs WHERE org_id=$4::bigint AND request_id=$1::varchar(64)
	)
	ON CONFLICT (outbox_event_key) DO NOTHING`, args...)
	return err
}
