package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
)

type traceOutboxPayload struct {
	RequestID          string          `json:"request_id"`
	ClientRequestID    string          `json:"client_request_id,omitempty"`
	IdempotencyKeyHash string          `json:"idempotency_key_hash,omitempty"`
	OrgID              int64           `json:"org_id"`
	ProjectID          *int64          `json:"project_id,omitempty"`
	APIKeyID           *int64          `json:"api_key_id,omitempty"`
	OwnerUserID        *int64          `json:"owner_user_id,omitempty"`
	KeyType            string          `json:"key_type"`
	EventType          string          `json:"event_type"`
	Stage              string          `json:"stage"`
	Severity           string          `json:"severity"`
	Model              string          `json:"model"`
	RequestedModel     string          `json:"requested_model"`
	Provider           string          `json:"provider"`
	Channel            string          `json:"channel"`
	StatusCode         *int            `json:"status_code,omitempty"`
	LatencyMS          int             `json:"latency_ms"`
	Metadata           json.RawMessage `json:"metadata"`
	TraceID            string          `json:"trace_id"`
	SpanID             string          `json:"span_id"`
	ParentSpanID       string          `json:"parent_span_id,omitempty"`
	SchemaVersion      string          `json:"schema_version"`
	SourceComponent    string          `json:"source_component"`
	CaptureMode        string          `json:"capture_mode"`
	CaptureGap         string          `json:"capture_gap,omitempty"`
	EvidenceRef        string          `json:"evidence_ref"`
	UpstreamRequestID  string          `json:"upstream_request_id,omitempty"`
	DecisionKind       string          `json:"decision_kind,omitempty"`
	DecisionResult     string          `json:"decision_result,omitempty"`
	DecisionReason     string          `json:"decision_reason,omitempty"`
	ActorType          string          `json:"actor_type"`
	ActorRef           string          `json:"actor_ref,omitempty"`
	OnBehalfOf         string          `json:"on_behalf_of,omitempty"`
	SessionID          string          `json:"session_id,omitempty"`
	WorkflowID         string          `json:"workflow_id,omitempty"`
	AgentID            string          `json:"agent_id,omitempty"`
	AttemptID          string          `json:"attempt_id,omitempty"`
	PayloadCaptureMode string          `json:"payload_capture_mode"`
	SourceEventHash    string          `json:"source_event_hash"`
	OccurredAt         time.Time       `json:"occurred_at"`
}

type callLogOutboxPayload struct {
	RequestID                                 string      `json:"request_id"`
	ClientRequestID                           string      `json:"client_request_id,omitempty"`
	IdempotencyKeyHash                        string      `json:"idempotency_key_hash,omitempty"`
	OrgID                                     int64       `json:"org_id"`
	ProjectID                                 *int64      `json:"project_id,omitempty"`
	APIKeyID                                  int64       `json:"api_key_id"`
	OwnerUserID                               *int64      `json:"owner_user_id,omitempty"`
	KeyType                                   string      `json:"key_type"`
	Model                                     string      `json:"model"`
	RequestedModel                            string      `json:"requested_model"`
	Provider                                  string      `json:"provider"`
	Channel                                   string      `json:"channel"`
	PromptTokens                              int         `json:"prompt_tokens"`
	UncachedPromptTokens                      int         `json:"uncached_prompt_tokens"`
	CacheReadTokens                           int         `json:"cache_read_tokens"`
	CacheCreationTokens                       int         `json:"cache_creation_tokens"`
	CompletionTokens                          int         `json:"completion_tokens"`
	MoneySchema                               string      `json:"money_schema,omitempty"`
	Currency                                  string      `json:"currency,omitempty"`
	AmountScale                               int64       `json:"amount_scale,omitempty"`
	CostMicros                                string      `json:"cost_micros,omitempty"`
	StatusCode                                int         `json:"status_code"`
	LatencyMS                                 int         `json:"latency_ms"`
	TTFTMS                                    int         `json:"ttft_ms"`
	UsageSource                               string      `json:"usage_source"`
	UsageAuthority                            string      `json:"usage_authority"`
	UsagePurpose                              string      `json:"usage_purpose"`
	UsageFinality                             string      `json:"usage_finality"`
	UsageContractVersion                      string      `json:"usage_contract_version"`
	UsageEstimateVersion                      string      `json:"usage_estimate_version,omitempty"`
	CacheUsageSchema                          string      `json:"cache_usage_schema"`
	CacheUsageStatus                          string      `json:"cache_usage_status"`
	PricingVersion                            string      `json:"pricing_version"`
	EffectiveInputPriceMicrosPerMillion       string      `json:"effective_input_price_micros_per_million,omitempty"`
	EffectiveCacheReadPriceMicrosPerMillion   string      `json:"effective_cache_read_price_micros_per_million,omitempty"`
	EffectiveCacheCreatePriceMicrosPerMillion string      `json:"effective_cache_creation_price_micros_per_million,omitempty"`
	EffectiveOutputPriceMicrosPerMillion      string      `json:"effective_output_price_micros_per_million,omitempty"`
	CacheReadCostMicros                       string      `json:"cache_read_cost_micros,omitempty"`
	CacheCreationCostMicros                   string      `json:"cache_creation_cost_micros,omitempty"`
	LegacyCostCents                           json.Number `json:"cost_cents,omitempty"`
	LegacyEffectiveInputPrice                 json.Number `json:"effective_input_price_cents,omitempty"`
	LegacyEffectiveCacheReadPrice             json.Number `json:"effective_cache_read_price_cents,omitempty"`
	LegacyEffectiveCacheCreatePrice           json.Number `json:"effective_cache_creation_price_cents,omitempty"`
	LegacyEffectiveOutputPrice                json.Number `json:"effective_output_price_cents,omitempty"`
	LegacyCacheReadCost                       json.Number `json:"cache_read_cost_cents,omitempty"`
	LegacyCacheCreationCost                   json.Number `json:"cache_creation_cost_cents,omitempty"`
	IsStream                                  bool        `json:"is_stream"`
	IPAddress                                 string      `json:"ip_address"`
	EstimatedTokens                           int         `json:"estimated_tokens"`
	UsageConfidence                           float64     `json:"usage_confidence"`
	TraceID                                   string      `json:"trace_id"`
	SessionID                                 string      `json:"session_id,omitempty"`
	SchemaVersion                             string      `json:"schema_version"`
	CaptureMode                               string      `json:"capture_mode"`
	EvidenceRef                               string      `json:"evidence_ref"`
	UpstreamRequestID                         string      `json:"upstream_request_id,omitempty"`
	WorkflowID                                string      `json:"workflow_id,omitempty"`
	AgentID                                   string      `json:"agent_id,omitempty"`
	AttemptID                                 string      `json:"attempt_id,omitempty"`
	ActorType                                 string      `json:"actor_type"`
	ActorRef                                  string      `json:"actor_ref,omitempty"`
	OnBehalfOf                                string      `json:"on_behalf_of,omitempty"`
	PayloadCaptureMode                        string      `json:"payload_capture_mode"`
	SourceEventHash                           string      `json:"source_event_hash"`
	OccurredAt                                time.Time   `json:"occurred_at"`
}

func (relay *Relay) enqueueTraceOutbox(ctx context.Context, payload traceOutboxPayload) (bool, error) {
	priority := outbox.PriorityTraceInfo
	if payload.Severity == "warning" {
		priority = outbox.PriorityTraceWarning
	} else if payload.Severity == "error" {
		priority = outbox.PriorityCallLog
	}
	event, err := outbox.NewEvent(
		outbox.EventTrace,
		"trace:"+payload.SourceEventHash,
		priority,
		payload.OrgID,
		payload.RequestID,
		payload,
		payload.OccurredAt,
	)
	if err != nil {
		return false, err
	}
	_, created, err := relay.outbox.Enqueue(ctx, event)
	return created, err
}

func (relay *Relay) enqueueCallLogOutbox(ctx context.Context, payload callLogOutboxPayload) error {
	event, err := outbox.NewEvent(
		outbox.EventCallLog,
		fmt.Sprintf("call-log:%d:%s", payload.OrgID, payload.RequestID),
		outbox.PriorityCallLog,
		payload.OrgID,
		payload.RequestID,
		payload,
		payload.OccurredAt,
	)
	if err != nil {
		return err
	}
	_, _, err = relay.outbox.Enqueue(ctx, event)
	return err
}

func (relay *Relay) ProcessTraceOutbox(ctx context.Context, event outbox.Event) error {
	if relay == nil || relay.db == nil {
		return fmt.Errorf("gateway evidence database is unavailable")
	}
	var payload traceOutboxPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode trace outbox payload: %w", err)
	}
	_, err := relay.db.Exec(ctx,
		`INSERT INTO request_trace_events (
			request_id, client_request_id, idempotency_key_hash,
			org_id, project_id, api_key_id, owner_user_id, key_type,
			event_type, stage, severity, model, requested_model, provider, channel,
			status_code, latency_ms, metadata,
			trace_id, span_id, parent_span_id, schema_version, source_component, capture_mode,
			capture_gap, evidence_ref, upstream_request_id, decision_kind, decision_result, decision_reason,
			actor_type, actor_ref, on_behalf_of, session_id, workflow_id, agent_id, attempt_id,
			payload_capture_mode, source_event_hash, outbox_event_key, created_at
		 ) VALUES (
			$1, $2, $3,
			$4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18,
			$19, $20, $21, $22, $23, $24,
			$25, $26, $27, $28, $29, $30,
			$31, $32, $33, $34, $35, $36, $37,
			$38, $39, $40, $41
		 ) ON CONFLICT (outbox_event_key) DO NOTHING`,
		payload.RequestID, nullableString(payload.ClientRequestID), nullableString(payload.IdempotencyKeyHash),
		payload.OrgID, payload.ProjectID, payload.APIKeyID, payload.OwnerUserID, payload.KeyType,
		payload.EventType, payload.Stage, payload.Severity, payload.Model, payload.RequestedModel, payload.Provider, payload.Channel,
		payload.StatusCode, payload.LatencyMS, string(payload.Metadata),
		payload.TraceID, payload.SpanID, nullableString(payload.ParentSpanID), payload.SchemaVersion, payload.SourceComponent, payload.CaptureMode,
		nullableString(payload.CaptureGap), payload.EvidenceRef, nullableString(payload.UpstreamRequestID), nullableString(payload.DecisionKind), nullableString(payload.DecisionResult), nullableString(payload.DecisionReason),
		payload.ActorType, nullableString(payload.ActorRef), nullableString(payload.OnBehalfOf), nullableString(payload.SessionID), nullableString(payload.WorkflowID), nullableString(payload.AgentID), nullableString(payload.AttemptID),
		payload.PayloadCaptureMode, payload.SourceEventHash, event.Key, payload.OccurredAt,
	)
	return err
}

type callLogMoney struct {
	CostMicros                                int64
	EffectiveInputPriceMicrosPerMillion       int64
	EffectiveCacheReadPriceMicrosPerMillion   int64
	EffectiveCacheCreatePriceMicrosPerMillion int64
	EffectiveOutputPriceMicrosPerMillion      int64
	CacheReadCostMicros                       int64
	CacheCreationCostMicros                   int64
}

func normalizeCallLogMoney(eventType string, payload callLogOutboxPayload) (callLogMoney, error) {
	if eventType == outbox.EventCallLogV1 {
		return legacyCallLogMoney(payload)
	}
	if eventType != outbox.EventCallLog || payload.MoneySchema != outbox.MoneyVersion ||
		payload.Currency != "CNY" || payload.AmountScale != money.MicrosPerCNY {
		return callLogMoney{}, fmt.Errorf("unsupported call-log money contract")
	}
	values := []*string{
		&payload.CostMicros,
		&payload.EffectiveInputPriceMicrosPerMillion,
		&payload.EffectiveCacheReadPriceMicrosPerMillion,
		&payload.EffectiveCacheCreatePriceMicrosPerMillion,
		&payload.EffectiveOutputPriceMicrosPerMillion,
		&payload.CacheReadCostMicros,
		&payload.CacheCreationCostMicros,
	}
	parsed := make([]int64, len(values))
	for i, value := range values {
		amount, err := parseCanonicalNonnegativeInt64(*value)
		if err != nil {
			return callLogMoney{}, fmt.Errorf("invalid call-log micro amount: %w", err)
		}
		parsed[i] = amount
	}
	return callLogMoney{
		CostMicros: parsed[0], EffectiveInputPriceMicrosPerMillion: parsed[1],
		EffectiveCacheReadPriceMicrosPerMillion: parsed[2], EffectiveCacheCreatePriceMicrosPerMillion: parsed[3],
		EffectiveOutputPriceMicrosPerMillion: parsed[4], CacheReadCostMicros: parsed[5], CacheCreationCostMicros: parsed[6],
	}, nil
}

func parseCanonicalNonnegativeInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, fmt.Errorf("%q is not a canonical nonnegative int64", value)
	}
	return parsed, nil
}

func legacyCallLogMoney(payload callLogOutboxPayload) (callLogMoney, error) {
	cost, err := legacyDecimalCents(payload.LegacyCostCents)
	if err != nil {
		return callLogMoney{}, err
	}
	readCost, err := legacyDecimalCents(payload.LegacyCacheReadCost)
	if err != nil {
		return callLogMoney{}, err
	}
	createCost, err := legacyDecimalCents(payload.LegacyCacheCreationCost)
	if err != nil {
		return callLogMoney{}, err
	}
	prices := []json.Number{
		payload.LegacyEffectiveInputPrice, payload.LegacyEffectiveCacheReadPrice,
		payload.LegacyEffectiveCacheCreatePrice, payload.LegacyEffectiveOutputPrice,
	}
	parsedPrices := make([]int64, len(prices))
	for i, price := range prices {
		value := price.String()
		if value == "" {
			value = "0"
		}
		parsedPrices[i], err = money.PriceMicrosPerMillionFromInternal(value, "1", "1")
		if err != nil {
			return callLogMoney{}, fmt.Errorf("convert legacy call-log price: %w", err)
		}
	}
	return callLogMoney{
		CostMicros: cost, EffectiveInputPriceMicrosPerMillion: parsedPrices[0],
		EffectiveCacheReadPriceMicrosPerMillion: parsedPrices[1], EffectiveCacheCreatePriceMicrosPerMillion: parsedPrices[2],
		EffectiveOutputPriceMicrosPerMillion: parsedPrices[3], CacheReadCostMicros: readCost, CacheCreationCostMicros: createCost,
	}, nil
}

func legacyDecimalCents(value json.Number) (int64, error) {
	text := value.String()
	if text == "" {
		text = "0"
	}
	converted, err := money.DecimalCentsToMicros(text)
	if err != nil {
		return 0, fmt.Errorf("convert legacy call-log amount: %w", err)
	}
	return converted, nil
}

func (relay *Relay) ProcessCallLogOutbox(ctx context.Context, event outbox.Event) error {
	if relay == nil || relay.db == nil {
		return fmt.Errorf("gateway evidence database is unavailable")
	}
	var payload callLogOutboxPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode call-log outbox payload: %w", err)
	}
	moneyValues, err := normalizeCallLogMoney(event.Type, payload)
	if err != nil {
		return err
	}
	if payload.UsageAuthority == "" {
		authority, purpose, finality, estimateVersion := metering.LegacyUsageContract(payload.UsageSource, true)
		payload.UsageAuthority = string(authority)
		payload.UsagePurpose = string(purpose)
		payload.UsageFinality = string(finality)
		payload.UsageContractVersion = metering.UsageContractVersion
		payload.UsageEstimateVersion = estimateVersion
	}
	_, err = relay.db.Exec(ctx,
		`INSERT INTO call_logs (
			request_id, client_request_id, idempotency_key_hash,
			org_id, project_id, api_key_id, owner_user_id, key_type,
			model, requested_model, provider, channel,
			prompt_tokens, uncached_prompt_tokens, cache_read_tokens, cache_creation_tokens,
			completion_tokens, total_tokens, cost_micros, status_code, upstream_status_code, latency_ms, ttft_ms, usage_source,
			usage_authority, usage_purpose, usage_finality, usage_contract_version, usage_estimate_version,
			cache_usage_schema, cache_usage_status, pricing_version,
			effective_input_price_micros_per_million, effective_cache_read_price_micros_per_million,
			effective_cache_creation_price_micros_per_million, effective_output_price_micros_per_million,
			cache_read_cost_micros, cache_creation_cost_micros,
			is_stream, ip_address, estimated_tokens, usage_confidence,
			trace_id, session_id, schema_version, capture_mode, evidence_ref, upstream_request_id,
			workflow_id, agent_id, attempt_id,
			actor_type, actor_ref, on_behalf_of, payload_capture_mode, source_event_hash,
			outbox_event_key, created_at
		 )
		 SELECT
			$1, $2, $3,
			$4, $5, $6, $7, $8,
			$9, $10, $11, $12,
			$13, $14, $15, $16,
			$17, $18, $19, $20,
			(SELECT provider_attempt.http_status
			   FROM billing_provider_attempts provider_attempt
			  WHERE provider_attempt.org_id = $4::bigint
			    AND provider_attempt.request_id = $1::varchar(64)
			    AND provider_attempt.attempt_id = $50::varchar(64)
			  LIMIT 1),
			$21, $22, $23,
			$24, $25, $26, $27, $28,
			$29, $30, $31,
			$32, $33, $34, $35,
			$36, $37,
			$38, $39, $40, $41,
			$42, $43, $44, $45, $46, $47,
			$48, $49, $50,
			$51, $52, $53, $54, $55,
			$56, $57
		 WHERE NOT EXISTS (
			SELECT 1 FROM call_logs
			 WHERE org_id = $4::bigint AND request_id = $1::varchar(64)
		 )
		 ON CONFLICT (outbox_event_key) DO NOTHING`,
		payload.RequestID, nullableString(payload.ClientRequestID), nullableString(payload.IdempotencyKeyHash),
		payload.OrgID, payload.ProjectID, payload.APIKeyID, payload.OwnerUserID, payload.KeyType,
		payload.Model, payload.RequestedModel, payload.Provider, payload.Channel,
		payload.PromptTokens, payload.UncachedPromptTokens, payload.CacheReadTokens, payload.CacheCreationTokens,
		payload.CompletionTokens, payload.PromptTokens+payload.CompletionTokens,
		moneyValues.CostMicros, payload.StatusCode, payload.LatencyMS, payload.TTFTMS, payload.UsageSource,
		payload.UsageAuthority, payload.UsagePurpose, payload.UsageFinality,
		payload.UsageContractVersion, payload.UsageEstimateVersion,
		payload.CacheUsageSchema, payload.CacheUsageStatus, payload.PricingVersion,
		moneyValues.EffectiveInputPriceMicrosPerMillion, moneyValues.EffectiveCacheReadPriceMicrosPerMillion,
		moneyValues.EffectiveCacheCreatePriceMicrosPerMillion, moneyValues.EffectiveOutputPriceMicrosPerMillion,
		moneyValues.CacheReadCostMicros, moneyValues.CacheCreationCostMicros,
		payload.IsStream, payload.IPAddress, payload.EstimatedTokens, payload.UsageConfidence,
		payload.TraceID, nullableString(payload.SessionID), payload.SchemaVersion, payload.CaptureMode, payload.EvidenceRef,
		nullableString(payload.UpstreamRequestID), nullableString(payload.WorkflowID), nullableString(payload.AgentID), nullableString(payload.AttemptID),
		payload.ActorType, nullableString(payload.ActorRef), nullableString(payload.OnBehalfOf), payload.PayloadCaptureMode, payload.SourceEventHash,
		event.Key, payload.OccurredAt,
	)
	return err
}

func int64Pointer(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	copy := value
	return &copy
}

func intPointer(value int) *int {
	if value <= 0 {
		return nil
	}
	copy := value
	return &copy
}
