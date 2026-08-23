package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionprojection"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
)

type traceOutboxPayload struct {
	RequestID            string                         `json:"request_id"`
	ClientRequestID      string                         `json:"client_request_id,omitempty"`
	IdempotencyKeyHash   string                         `json:"idempotency_key_hash,omitempty"`
	OrgID                int64                          `json:"org_id"`
	ProjectID            *int64                         `json:"project_id,omitempty"`
	APIKeyID             *int64                         `json:"api_key_id,omitempty"`
	OwnerUserID          *int64                         `json:"owner_user_id,omitempty"`
	KeyType              string                         `json:"key_type"`
	EventType            string                         `json:"event_type"`
	Stage                string                         `json:"stage"`
	Severity             string                         `json:"severity"`
	Model                string                         `json:"model"`
	RequestedModel       string                         `json:"requested_model"`
	Provider             string                         `json:"provider"`
	Channel              string                         `json:"channel"`
	ChannelID            int64                          `json:"route_channel_id,omitempty"`
	StatusCode           *int                           `json:"status_code,omitempty"`
	LatencyMS            int                            `json:"latency_ms"`
	Metadata             json.RawMessage                `json:"metadata"`
	TraceID              string                         `json:"trace_id"`
	SpanID               string                         `json:"span_id"`
	ParentSpanID         string                         `json:"parent_span_id,omitempty"`
	SchemaVersion        string                         `json:"schema_version"`
	SourceComponent      string                         `json:"source_component"`
	CaptureMode          string                         `json:"capture_mode"`
	CaptureGap           string                         `json:"capture_gap,omitempty"`
	EvidenceRef          string                         `json:"evidence_ref"`
	UpstreamRequestID    string                         `json:"upstream_request_id,omitempty"`
	DecisionKind         string                         `json:"decision_kind,omitempty"`
	DecisionResult       string                         `json:"decision_result,omitempty"`
	DecisionReason       string                         `json:"decision_reason,omitempty"`
	ActorType            string                         `json:"actor_type"`
	ActorRef             string                         `json:"actor_ref,omitempty"`
	OnBehalfOf           string                         `json:"on_behalf_of,omitempty"`
	SessionID            string                         `json:"session_id,omitempty"`
	WorkflowID           string                         `json:"workflow_id,omitempty"`
	AgentID              string                         `json:"agent_id,omitempty"`
	AttemptID            string                         `json:"attempt_id,omitempty"`
	ProviderAttemptStage string                         `json:"provider_attempt_stage,omitempty"`
	PayloadCaptureMode   string                         `json:"payload_capture_mode"`
	SourceEventHash      string                         `json:"source_event_hash"`
	RouteProjection      executionprojection.Projection `json:"route_projection,omitempty"`
	OccurredAt           time.Time                      `json:"occurred_at"`
}

type callLogOutboxPayload struct {
	RequestID                                 string                         `json:"request_id"`
	ClientRequestID                           string                         `json:"client_request_id,omitempty"`
	IdempotencyKeyHash                        string                         `json:"idempotency_key_hash,omitempty"`
	OrgID                                     int64                          `json:"org_id"`
	ProjectID                                 *int64                         `json:"project_id,omitempty"`
	APIKeyID                                  int64                          `json:"api_key_id"`
	OwnerUserID                               *int64                         `json:"owner_user_id,omitempty"`
	KeyType                                   string                         `json:"key_type"`
	Model                                     string                         `json:"model"`
	RequestedModel                            string                         `json:"requested_model"`
	Provider                                  string                         `json:"provider"`
	Channel                                   string                         `json:"channel"`
	ChannelID                                 int64                          `json:"route_channel_id,omitempty"`
	PromptTokens                              int                            `json:"prompt_tokens"`
	UncachedPromptTokens                      int                            `json:"uncached_prompt_tokens"`
	CacheReadTokens                           int                            `json:"cache_read_tokens"`
	CacheCreationTokens                       int                            `json:"cache_creation_tokens"`
	CompletionTokens                          int                            `json:"completion_tokens"`
	MoneySchema                               string                         `json:"money_schema,omitempty"`
	Currency                                  string                         `json:"currency,omitempty"`
	AmountScale                               int64                          `json:"amount_scale,omitempty"`
	CostMicros                                string                         `json:"cost_micros,omitempty"`
	StatusCode                                int                            `json:"status_code"`
	LatencyMS                                 int                            `json:"latency_ms"`
	TTFTMS                                    int                            `json:"ttft_ms"`
	UsageSource                               string                         `json:"usage_source"`
	UsageAuthority                            string                         `json:"usage_authority"`
	UsagePurpose                              string                         `json:"usage_purpose"`
	UsageFinality                             string                         `json:"usage_finality"`
	UsageContractVersion                      string                         `json:"usage_contract_version"`
	UsageEstimateVersion                      string                         `json:"usage_estimate_version,omitempty"`
	CacheUsageSchema                          string                         `json:"cache_usage_schema"`
	CacheUsageStatus                          string                         `json:"cache_usage_status"`
	PricingVersion                            string                         `json:"pricing_version"`
	EffectiveInputPriceMicrosPerMillion       string                         `json:"effective_input_price_micros_per_million,omitempty"`
	EffectiveCacheReadPriceMicrosPerMillion   string                         `json:"effective_cache_read_price_micros_per_million,omitempty"`
	EffectiveCacheCreatePriceMicrosPerMillion string                         `json:"effective_cache_creation_price_micros_per_million,omitempty"`
	EffectiveOutputPriceMicrosPerMillion      string                         `json:"effective_output_price_micros_per_million,omitempty"`
	CacheReadCostMicros                       string                         `json:"cache_read_cost_micros,omitempty"`
	CacheCreationCostMicros                   string                         `json:"cache_creation_cost_micros,omitempty"`
	EffectiveUnitRates                        []billing.UnitRate             `json:"effective_unit_rates,omitempty"`
	MeterUsage                                []metering.MeterQuantity       `json:"meter_usage,omitempty"`
	NonTokenCostMicros                        string                         `json:"non_token_cost_micros,omitempty"`
	LegacyCostCents                           json.Number                    `json:"cost_cents,omitempty"`
	LegacyEffectiveInputPrice                 json.Number                    `json:"effective_input_price_cents,omitempty"`
	LegacyEffectiveCacheReadPrice             json.Number                    `json:"effective_cache_read_price_cents,omitempty"`
	LegacyEffectiveCacheCreatePrice           json.Number                    `json:"effective_cache_creation_price_cents,omitempty"`
	LegacyEffectiveOutputPrice                json.Number                    `json:"effective_output_price_cents,omitempty"`
	LegacyCacheReadCost                       json.Number                    `json:"cache_read_cost_cents,omitempty"`
	LegacyCacheCreationCost                   json.Number                    `json:"cache_creation_cost_cents,omitempty"`
	IsStream                                  bool                           `json:"is_stream"`
	IPAddress                                 string                         `json:"ip_address"`
	EstimatedTokens                           int                            `json:"estimated_tokens"`
	UsageConfidence                           float64                        `json:"usage_confidence"`
	TraceID                                   string                         `json:"trace_id"`
	SessionID                                 string                         `json:"session_id,omitempty"`
	SchemaVersion                             string                         `json:"schema_version"`
	CaptureMode                               string                         `json:"capture_mode"`
	EvidenceRef                               string                         `json:"evidence_ref"`
	UpstreamRequestID                         string                         `json:"upstream_request_id,omitempty"`
	WorkflowID                                string                         `json:"workflow_id,omitempty"`
	AgentID                                   string                         `json:"agent_id,omitempty"`
	AttemptID                                 string                         `json:"attempt_id,omitempty"`
	ActorType                                 string                         `json:"actor_type"`
	ActorRef                                  string                         `json:"actor_ref,omitempty"`
	OnBehalfOf                                string                         `json:"on_behalf_of,omitempty"`
	PayloadCaptureMode                        string                         `json:"payload_capture_mode"`
	SourceEventHash                           string                         `json:"source_event_hash"`
	RouteProjection                           executionprojection.Projection `json:"route_projection,omitempty"`
	OccurredAt                                time.Time                      `json:"occurred_at"`
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
	prepared, err := relay.prepareTraceMaterialization(ctx, payload)
	if err != nil {
		return err
	}
	return relay.materializeTrace(ctx, prepared, event.Key)
}

type callLogMoney struct {
	CostMicros                                int64
	EffectiveInputPriceMicrosPerMillion       int64
	EffectiveCacheReadPriceMicrosPerMillion   int64
	EffectiveCacheCreatePriceMicrosPerMillion int64
	EffectiveOutputPriceMicrosPerMillion      int64
	CacheReadCostMicros                       int64
	CacheCreationCostMicros                   int64
	NonTokenCostMicros                        int64
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
		&payload.NonTokenCostMicros,
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
		NonTokenCostMicros: parsed[7],
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
	if payload.UsageAuthority == "" {
		authority, purpose, finality, estimateVersion := metering.LegacyUsageContract(payload.UsageSource, true)
		payload.UsageAuthority = string(authority)
		payload.UsagePurpose = string(purpose)
		payload.UsageFinality = string(finality)
		payload.UsageContractVersion = metering.UsageContractVersion
		payload.UsageEstimateVersion = estimateVersion
	}
	prepared, err := relay.prepareCallLogMaterialization(ctx, payload)
	if err != nil {
		return err
	}
	return relay.materializeCallLog(ctx, event.Type, prepared, event.Key)
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
