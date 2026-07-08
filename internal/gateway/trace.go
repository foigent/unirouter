package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/traceutil"
)

const (
	evidenceSchemaVersion      = "unirouter.evidence.v1"
	evidenceCaptureModeNative  = "native"
	evidenceCaptureGapProvider = "provider_not_returned"
	evidenceCaptureGapSource   = "not_available_from_source"
	evidencePayloadMetadata    = traceutil.PayloadCaptureModeMetadataOnly
	evidenceTraceRefSourceType = "unirouter"
)

type evidenceSpanStateKey struct{}

type evidenceSpanState struct {
	mu               sync.Mutex
	rootSpanID       string
	componentSpanIDs map[string]string
}

func contextWithEvidenceSpanState(ctx context.Context) context.Context {
	if ctx == nil {
		return ctx
	}
	if _, ok := ctx.Value(evidenceSpanStateKey{}).(*evidenceSpanState); ok {
		return ctx
	}
	return context.WithValue(ctx, evidenceSpanStateKey{}, &evidenceSpanState{})
}

func parentSpanForEvent(ctx context.Context, spanID, sourceComponent, trustedParentSpanID string) string {
	state, _ := ctx.Value(evidenceSpanStateKey{}).(*evidenceSpanState)
	if state == nil {
		return trustedParentSpanID
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	sourceComponent = strings.TrimSpace(sourceComponent)
	if sourceComponent == "" {
		sourceComponent = "unknown"
	}
	if state.componentSpanIDs == nil {
		state.componentSpanIDs = map[string]string{}
	}
	if state.rootSpanID == "" {
		state.rootSpanID = spanID
		state.componentSpanIDs[sourceComponent] = spanID
		return trustedParentSpanID
	}
	if componentSpanID := state.componentSpanIDs[sourceComponent]; componentSpanID != "" {
		return componentSpanID
	}
	state.componentSpanIDs[sourceComponent] = spanID
	return state.rootSpanID
}

func (relay *Relay) recordTraceEvent(ctx context.Context, requestID, eventType, stage, severity, model, requestedModel string, route *RouteResult, statusCode, latencyMs int, metadata map[string]interface{}, upstreamRequestIDs ...string) {
	if relay == nil || relay.db == nil || strings.TrimSpace(requestID) == "" {
		return
	}
	if !relay.shouldRecordTraceEvent(requestID) {
		return
	}
	orgID := middleware.GetOrgIDFromContext(ctx)
	if orgID <= 0 {
		return
	}
	projectID := middleware.GetProjectIDFromContext(ctx)
	var projectIDArg interface{}
	if projectID > 0 {
		projectIDArg = projectID
	}
	keyID := middleware.GetKeyIDFromContext(ctx)
	var keyIDArg interface{}
	if keyID > 0 {
		keyIDArg = keyID
	}
	var ownerUserIDArg interface{}
	keyType := "unknown"
	actorType := "unknown"
	var actorRefArg interface{}
	if apiKey := middleware.GetAPIKeyFromContext(ctx); apiKey != nil {
		if apiKey.OwnerUserID != nil && *apiKey.OwnerUserID > 0 {
			ownerUserIDArg = *apiKey.OwnerUserID
		}
		if apiKey.KeyType == "shared" || apiKey.KeyType == "personal" || apiKey.KeyType == "service" {
			keyType = apiKey.KeyType
		}
		actorType, actorRefArg = actorEvidence(keyType, keyID, apiKey.OwnerUserID)
	}
	evidenceContext := traceutil.EvidenceFromContext(ctx)
	if evidenceContext.ActorType != "" {
		actorType = evidenceContext.ActorType
		actorRefArg = nullableString(evidenceContext.ActorRef)
	}
	if severity != "warning" && severity != "error" {
		severity = "info"
	}
	if route != nil {
		if model == "" {
			model = route.MappedModel
		}
		if metadata == nil {
			metadata = map[string]interface{}{}
		}
		if route.MappedModel != "" {
			metadata["mapped_model"] = route.MappedModel
		}
	}
	provider := ""
	channel := ""
	if route != nil {
		provider = route.Channel.Provider
		channel = route.Channel.Name
	}
	var statusCodeArg interface{}
	if statusCode > 0 {
		statusCodeArg = statusCode
	}
	if latencyMs < 0 {
		latencyMs = 0
	}
	sanitizedMetadata, captureGap := traceutil.SanitizeMetadataWithGap(metadata)
	metadataJSON, err := json.Marshal(sanitizedMetadata)
	if err != nil {
		metadataJSON = []byte("{}")
		captureGap = "redacted"
	}
	traceID := requestID
	if evidenceContext.TraceID != "" {
		traceID = evidenceContext.TraceID
	}
	spanID := newEvidenceID()
	sourceComponent := sourceComponentForTraceStage(stage)
	parentSpanID := parentSpanForEvent(ctx, spanID, sourceComponent, evidenceContext.ParentSpanID)
	evidenceRef := fmt.Sprintf("%s:request_trace_events:request_id:%s:span:%s", evidenceTraceRefSourceType, requestID, spanID)
	upstreamRequestID := ""
	if len(upstreamRequestIDs) > 0 {
		upstreamRequestID = sanitizeUpstreamRequestID(upstreamRequestIDs[0])
	}
	if upstreamRequestID == "" && captureGap == "" {
		captureGap = providerMissingUpstreamCaptureGap(eventType)
	}
	payloadCaptureMode := relay.currentPayloadCaptureMode()
	sourceEventHash := hashTraceEvent(requestID, traceID, parentSpanID, eventType, stage, severity, model, requestedModel, provider, channel, statusCodeArg, latencyMs, upstreamRequestID, captureGap, actorType, stringFromNullable(actorRefArg), evidenceContext.OnBehalfOf, evidenceContext.SessionID, payloadCaptureMode, metadataJSON)
	decisionKind, decisionResult, decisionReason := decisionEvidence(eventType, sanitizedMetadata)
	if _, err := relay.db.Exec(ctx,
		`INSERT INTO request_trace_events (
			request_id, org_id, project_id, api_key_id, owner_user_id, key_type,
			event_type, stage, severity, model, requested_model, provider, channel,
			status_code, latency_ms, metadata,
			trace_id, span_id, parent_span_id, schema_version, source_component, capture_mode,
			capture_gap, evidence_ref, upstream_request_id, decision_kind, decision_result, decision_reason,
			actor_type, actor_ref, on_behalf_of, session_id, payload_capture_mode, source_event_hash
		)
		 VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16,
			$17, $18, $19, $20, $21, $22,
			$23, $24, $25, $26, $27, $28,
			$29, $30, $31, $32, $33, $34
		)`,
		requestID, orgID, projectIDArg, keyIDArg, ownerUserIDArg, keyType,
		eventType, stage, severity, model, requestedModel, provider, channel,
		statusCodeArg, latencyMs, string(metadataJSON),
		traceID, spanID, nullableString(parentSpanID), evidenceSchemaVersion, sourceComponent, evidenceCaptureModeNative,
		nullableString(captureGap), evidenceRef, nullableString(upstreamRequestID), nullableString(decisionKind), nullableString(decisionResult), nullableString(decisionReason),
		actorType, actorRefArg, nullableString(evidenceContext.OnBehalfOf), nullableString(evidenceContext.SessionID), payloadCaptureMode, sourceEventHash,
	); err != nil {
		logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "trace"}).Warn("Failed to record trace event %s: %v", eventType, err)
	}
}

func (relay *Relay) recordPayloadPolicyDecision(ctx context.Context, requestID, model, requestedModel string, latencyMs int) {
	mode := relay.currentPayloadCaptureMode()
	reason := "payload.metadata_only"
	if mode == traceutil.PayloadCaptureModeDisabled {
		reason = "payload.disabled"
	}
	relay.recordTraceEvent(ctx, requestID, "payload.policy.applied", "payload_policy", "info", model, requestedModel, nil, 0, latencyMs, map[string]interface{}{
		"payload_capture_mode":  mode,
		"payload_policy_reason": reason,
		"payload_ref_present":   false,
		"raw_payload_stored":    false,
	})
}

func (relay *Relay) shouldRecordTraceEvent(requestID string) bool {
	if relay == nil || !relay.traceEventsEnabled {
		return false
	}
	if relay.traceEventSampleRate >= 1 {
		return true
	}
	if relay.traceEventSampleRate <= 0 {
		return false
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(requestID))
	bucket := float64(hash.Sum32()%1000000) / 1000000
	return bucket < relay.traceEventSampleRate
}

func actorEvidence(keyType string, keyID int64, ownerUserID *int64) (string, interface{}) {
	switch keyType {
	case "personal":
		if ownerUserID != nil && *ownerUserID > 0 {
			return "human", fmt.Sprintf("user:%d", *ownerUserID)
		}
		if keyID > 0 {
			return "api_key", fmt.Sprintf("api_key:%d", keyID)
		}
	case "service":
		if keyID > 0 {
			return "service", fmt.Sprintf("api_key:%d", keyID)
		}
		return "service", nil
	case "shared":
		if keyID > 0 {
			return "api_key", fmt.Sprintf("api_key:%d", keyID)
		}
		return "api_key", nil
	}
	return "unknown", nil
}

func sourceComponentForTraceStage(stage string) string {
	switch strings.TrimSpace(strings.ToLower(stage)) {
	case "gateway":
		return "gateway"
	case "auth":
		return "auth"
	case "quota":
		return "quota"
	case "budget":
		return "budget"
	case "routing", "route":
		return "routing"
	case "provider", "upstream":
		return "provider"
	case "billing":
		return "billing"
	case "logging", "log":
		return "logging"
	case "trace":
		return "trace"
	case "payload_policy":
		return "payload_policy"
	case "admin":
		return "admin"
	default:
		return "unknown"
	}
}

func decisionEvidence(eventType string, metadata map[string]interface{}) (string, string, string) {
	reason := eventType
	if value, ok := metadata["error_code"].(string); ok && strings.TrimSpace(value) != "" {
		reason = value
	} else if value, ok := metadata["failure_kind"].(string); ok && strings.TrimSpace(value) != "" {
		reason = value
	} else if value, ok := metadata["payload_policy_reason"].(string); ok && strings.TrimSpace(value) != "" {
		reason = value
	}

	switch {
	case eventType == "gateway.request.accepted":
		return "admission", "allowed", reason
	case eventType == "payload.policy.applied":
		if mode, ok := metadata["payload_capture_mode"].(string); ok && mode == traceutil.PayloadCaptureModeDisabled {
			return "payload_capture", "skipped", reason
		}
		return "payload_capture", "allowed", reason
	case strings.HasPrefix(eventType, "billing.freeze."):
		switch eventType {
		case "billing.freeze.succeeded":
			return "budget", "allowed", reason
		case "billing.freeze.rejected":
			return "budget", "rejected", reason
		default:
			return "budget", "failed", reason
		}
	case strings.HasPrefix(eventType, "routing."):
		switch eventType {
		case "routing.route_selected":
			return "routing", "selected", reason
		case "routing.no_route":
			return "routing", "failed", reason
		default:
			return "routing", "unknown", reason
		}
	case strings.HasPrefix(eventType, "provider."):
		switch eventType {
		case "provider.response_accepted":
			return "provider", "selected", reason
		default:
			return "provider", "failed", reason
		}
	case strings.HasPrefix(eventType, "billing.settle."):
		if eventType == "billing.settle.succeeded" {
			return "billing", "allowed", reason
		}
		return "billing", "failed", reason
	default:
		return "", "", ""
	}
}

func providerMissingUpstreamCaptureGap(eventType string) string {
	switch eventType {
	case "provider.response_accepted", "provider.response_rejected":
		return evidenceCaptureGapProvider
	case "provider.key_decrypt_failed", "provider.adapter_unknown", "provider.request_build_failed", "provider.request_failed", "provider.response_parse_failed":
		return evidenceCaptureGapSource
	default:
		return ""
	}
}

func newEvidenceID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(sum[:8])
}

func hashTraceEvent(requestID, traceID, parentSpanID, eventType, stage, severity, model, requestedModel, provider, channel string, statusCode interface{}, latencyMs int, upstreamRequestID, captureGap, actorType, actorRef, onBehalfOf, sessionID, payloadCaptureMode string, metadataJSON []byte) string {
	payload := struct {
		RequestID         string      `json:"request_id"`
		TraceID           string      `json:"trace_id"`
		ParentSpanID      string      `json:"parent_span_id"`
		EventType         string      `json:"event_type"`
		Stage             string      `json:"stage"`
		Severity          string      `json:"severity"`
		Model             string      `json:"model"`
		RequestedModel    string      `json:"requested_model"`
		Provider          string      `json:"provider"`
		Channel           string      `json:"channel"`
		StatusCode        interface{} `json:"status_code"`
		LatencyMS         int         `json:"latency_ms"`
		UpstreamRequestID string      `json:"upstream_request_id"`
		CaptureGap        string      `json:"capture_gap"`
		ActorType         string      `json:"actor_type"`
		ActorRef          string      `json:"actor_ref"`
		OnBehalfOf        string      `json:"on_behalf_of"`
		SessionID         string      `json:"session_id"`
		PayloadMode       string      `json:"payload_capture_mode"`
		Metadata          string      `json:"metadata"`
	}{
		RequestID: requestID, TraceID: traceID, ParentSpanID: parentSpanID, EventType: eventType, Stage: stage, Severity: severity,
		Model: model, RequestedModel: requestedModel, Provider: provider, Channel: channel,
		StatusCode: statusCode, LatencyMS: latencyMs, UpstreamRequestID: upstreamRequestID, CaptureGap: captureGap,
		ActorType: actorType, ActorRef: actorRef, OnBehalfOf: onBehalfOf, SessionID: sessionID, PayloadMode: payloadCaptureMode,
		Metadata: string(metadataJSON),
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func hashCallLogEvidence(requestID string, orgID int64, projectID interface{}, keyID int64, ownerUserID interface{}, keyType, model, requestedModel, provider, channel string, promptTokens, completionTokens, costCents, statusCode, latencyMs, ttftMs int, usageSource string, isStream bool, estimatedTokens int, usageConfidence float64, upstreamRequestID, traceID, sessionID, actorType, actorRef, onBehalfOf, payloadCaptureMode string) string {
	payload := struct {
		RequestID         string      `json:"request_id"`
		TraceID           string      `json:"trace_id"`
		SessionID         string      `json:"session_id"`
		OrgID             int64       `json:"org_id"`
		ProjectID         interface{} `json:"project_id"`
		KeyID             int64       `json:"api_key_id"`
		OwnerUserID       interface{} `json:"owner_user_id"`
		KeyType           string      `json:"key_type"`
		Model             string      `json:"model"`
		RequestedModel    string      `json:"requested_model"`
		Provider          string      `json:"provider"`
		Channel           string      `json:"channel"`
		PromptTokens      int         `json:"prompt_tokens"`
		CompletionTokens  int         `json:"completion_tokens"`
		CostCents         int         `json:"cost_cents"`
		StatusCode        int         `json:"status_code"`
		LatencyMS         int         `json:"latency_ms"`
		TTFTMS            int         `json:"ttft_ms"`
		UsageSource       string      `json:"usage_source"`
		IsStream          bool        `json:"is_stream"`
		EstimatedTokens   int         `json:"estimated_tokens"`
		UsageConfidence   float64     `json:"usage_confidence"`
		UpstreamRequestID string      `json:"upstream_request_id"`
		ActorType         string      `json:"actor_type"`
		ActorRef          string      `json:"actor_ref"`
		OnBehalfOf        string      `json:"on_behalf_of"`
		PayloadMode       string      `json:"payload_capture_mode"`
	}{
		RequestID: requestID, TraceID: traceID, SessionID: sessionID, OrgID: orgID, ProjectID: projectID, KeyID: keyID,
		OwnerUserID: ownerUserID, KeyType: keyType, Model: model, RequestedModel: requestedModel,
		Provider: provider, Channel: channel, PromptTokens: promptTokens, CompletionTokens: completionTokens,
		CostCents: costCents, StatusCode: statusCode, LatencyMS: latencyMs, TTFTMS: ttftMs,
		UsageSource: usageSource, IsStream: isStream, EstimatedTokens: estimatedTokens, UsageConfidence: usageConfidence,
		UpstreamRequestID: upstreamRequestID, ActorType: actorType, ActorRef: actorRef, OnBehalfOf: onBehalfOf,
		PayloadMode: payloadCaptureMode,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func sanitizeUpstreamRequestID(value string) string {
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if value == "" {
		return ""
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 33 || value[i] > 126 {
			return ""
		}
	}
	if len(value) > 128 {
		value = value[:128]
	}
	return value
}

func nullableString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func stringFromNullable(value interface{}) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}
