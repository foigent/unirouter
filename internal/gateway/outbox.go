package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
	RequestID                 string    `json:"request_id"`
	ClientRequestID           string    `json:"client_request_id,omitempty"`
	IdempotencyKeyHash        string    `json:"idempotency_key_hash,omitempty"`
	OrgID                     int64     `json:"org_id"`
	ProjectID                 *int64    `json:"project_id,omitempty"`
	APIKeyID                  int64     `json:"api_key_id"`
	OwnerUserID               *int64    `json:"owner_user_id,omitempty"`
	KeyType                   string    `json:"key_type"`
	Model                     string    `json:"model"`
	RequestedModel            string    `json:"requested_model"`
	Provider                  string    `json:"provider"`
	Channel                   string    `json:"channel"`
	PromptTokens              int       `json:"prompt_tokens"`
	UncachedPromptTokens      int       `json:"uncached_prompt_tokens"`
	CacheReadTokens           int       `json:"cache_read_tokens"`
	CacheCreationTokens       int       `json:"cache_creation_tokens"`
	CompletionTokens          int       `json:"completion_tokens"`
	CostCents                 int       `json:"cost_cents"`
	StatusCode                int       `json:"status_code"`
	LatencyMS                 int       `json:"latency_ms"`
	TTFTMS                    int       `json:"ttft_ms"`
	UsageSource               string    `json:"usage_source"`
	CacheUsageSchema          string    `json:"cache_usage_schema"`
	CacheUsageStatus          string    `json:"cache_usage_status"`
	PricingVersion            string    `json:"pricing_version"`
	EffectiveInputPrice       float64   `json:"effective_input_price_cents"`
	EffectiveCacheReadPrice   float64   `json:"effective_cache_read_price_cents"`
	EffectiveCacheCreatePrice float64   `json:"effective_cache_creation_price_cents"`
	EffectiveOutputPrice      float64   `json:"effective_output_price_cents"`
	CacheReadCost             float64   `json:"cache_read_cost_cents"`
	CacheCreationCost         float64   `json:"cache_creation_cost_cents"`
	IsStream                  bool      `json:"is_stream"`
	IPAddress                 string    `json:"ip_address"`
	EstimatedTokens           int       `json:"estimated_tokens"`
	UsageConfidence           float64   `json:"usage_confidence"`
	TraceID                   string    `json:"trace_id"`
	SessionID                 string    `json:"session_id,omitempty"`
	SchemaVersion             string    `json:"schema_version"`
	CaptureMode               string    `json:"capture_mode"`
	EvidenceRef               string    `json:"evidence_ref"`
	UpstreamRequestID         string    `json:"upstream_request_id,omitempty"`
	WorkflowID                string    `json:"workflow_id,omitempty"`
	AgentID                   string    `json:"agent_id,omitempty"`
	AttemptID                 string    `json:"attempt_id,omitempty"`
	ActorType                 string    `json:"actor_type"`
	ActorRef                  string    `json:"actor_ref,omitempty"`
	OnBehalfOf                string    `json:"on_behalf_of,omitempty"`
	PayloadCaptureMode        string    `json:"payload_capture_mode"`
	SourceEventHash           string    `json:"source_event_hash"`
	OccurredAt                time.Time `json:"occurred_at"`
}

func (relay *Relay) enqueueTraceOutbox(ctx context.Context, payload traceOutboxPayload) error {
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
		return err
	}
	_, _, err = relay.outbox.Enqueue(ctx, event)
	return err
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

func (relay *Relay) ProcessCallLogOutbox(ctx context.Context, event outbox.Event) error {
	if relay == nil || relay.db == nil {
		return fmt.Errorf("gateway evidence database is unavailable")
	}
	var payload callLogOutboxPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode call-log outbox payload: %w", err)
	}
	_, err := relay.db.Exec(ctx,
		`INSERT INTO call_logs (
			request_id, client_request_id, idempotency_key_hash,
			org_id, project_id, api_key_id, owner_user_id, key_type,
			model, requested_model, provider, channel,
			prompt_tokens, uncached_prompt_tokens, cache_read_tokens, cache_creation_tokens,
			completion_tokens, total_tokens, cost_cents, status_code, latency_ms, ttft_ms, usage_source,
			cache_usage_schema, cache_usage_status, pricing_version,
			effective_input_price_cents, effective_cache_read_price_cents,
			effective_cache_creation_price_cents, effective_output_price_cents,
			cache_read_cost_cents, cache_creation_cost_cents,
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
			$17, $18, $19, $20, $21, $22, $23,
			$24, $25, $26,
			$27, $28, $29, $30,
			$31, $32,
			$33, $34, $35, $36,
			$37, $38, $39, $40, $41, $42,
			$43, $44, $45,
			$46, $47, $48, $49, $50,
			$51, $52
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
		payload.CostCents, payload.StatusCode, payload.LatencyMS, payload.TTFTMS, payload.UsageSource,
		payload.CacheUsageSchema, payload.CacheUsageStatus, payload.PricingVersion,
		payload.EffectiveInputPrice, payload.EffectiveCacheReadPrice, payload.EffectiveCacheCreatePrice, payload.EffectiveOutputPrice,
		payload.CacheReadCost, payload.CacheCreationCost,
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
