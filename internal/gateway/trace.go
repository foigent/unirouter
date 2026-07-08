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
	"time"

	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/traceutil"
)

const (
	evidenceSchemaVersion      = "unirouter.evidence.v1"
	evidenceCaptureModeNative  = "native"
	evidencePayloadMetadata    = "metadata_only"
	evidenceTraceRefSourceType = "unirouter"
)

func (relay *Relay) recordTraceEvent(ctx context.Context, requestID, eventType, stage, severity, model, requestedModel string, route *RouteResult, statusCode, latencyMs int, metadata map[string]interface{}) {
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
	sanitizedMetadata := traceutil.SanitizeMetadata(metadata)
	metadataJSON, err := json.Marshal(sanitizedMetadata)
	if err != nil {
		metadataJSON = []byte("{}")
	}
	traceID := requestID
	spanID := newEvidenceID()
	sourceComponent := sourceComponentForTraceStage(stage)
	evidenceRef := fmt.Sprintf("%s:request_trace_events:request_id:%s:span:%s", evidenceTraceRefSourceType, requestID, spanID)
	sourceEventHash := hashTraceEvent(requestID, eventType, stage, severity, model, requestedModel, provider, channel, statusCodeArg, latencyMs, metadataJSON)
	decisionKind, decisionResult, decisionReason := decisionEvidence(eventType, sanitizedMetadata)
	if _, err := relay.db.Exec(ctx,
		`INSERT INTO request_trace_events (
			request_id, org_id, project_id, api_key_id, owner_user_id, key_type,
			event_type, stage, severity, model, requested_model, provider, channel,
			status_code, latency_ms, metadata,
			trace_id, span_id, schema_version, source_component, capture_mode,
			evidence_ref, decision_kind, decision_result, decision_reason,
			actor_type, actor_ref, payload_capture_mode, source_event_hash
		)
		 VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16,
			$17, $18, $19, $20, $21,
			$22, $23, $24, $25,
			$26, $27, $28, $29
		)`,
		requestID, orgID, projectIDArg, keyIDArg, ownerUserIDArg, keyType,
		eventType, stage, severity, model, requestedModel, provider, channel,
		statusCodeArg, latencyMs, string(metadataJSON),
		traceID, spanID, evidenceSchemaVersion, sourceComponent, evidenceCaptureModeNative,
		evidenceRef, nullableString(decisionKind), nullableString(decisionResult), nullableString(decisionReason),
		actorType, actorRefArg, evidencePayloadMetadata, sourceEventHash,
	); err != nil {
		logger.New(logger.Fields{RequestID: requestID, OrgID: uint(orgID), Module: "trace"}).Warn("Failed to record trace event %s: %v", eventType, err)
	}
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
	}

	switch {
	case eventType == "gateway.request.accepted":
		return "admission", "allowed", reason
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

func newEvidenceID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(sum[:8])
}

func hashTraceEvent(requestID, eventType, stage, severity, model, requestedModel, provider, channel string, statusCode interface{}, latencyMs int, metadataJSON []byte) string {
	payload := struct {
		RequestID      string      `json:"request_id"`
		EventType      string      `json:"event_type"`
		Stage          string      `json:"stage"`
		Severity       string      `json:"severity"`
		Model          string      `json:"model"`
		RequestedModel string      `json:"requested_model"`
		Provider       string      `json:"provider"`
		Channel        string      `json:"channel"`
		StatusCode     interface{} `json:"status_code"`
		LatencyMS      int         `json:"latency_ms"`
		Metadata       string      `json:"metadata"`
	}{
		RequestID: requestID, EventType: eventType, Stage: stage, Severity: severity,
		Model: model, RequestedModel: requestedModel, Provider: provider, Channel: channel,
		StatusCode: statusCode, LatencyMS: latencyMs, Metadata: string(metadataJSON),
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func hashCallLogEvidence(requestID string, orgID int64, projectID interface{}, keyID int64, ownerUserID interface{}, keyType, model, requestedModel, provider, channel string, promptTokens, completionTokens, costCents, statusCode, latencyMs, ttftMs int, usageSource string, isStream bool, estimatedTokens int, usageConfidence float64) string {
	payload := struct {
		RequestID        string      `json:"request_id"`
		OrgID            int64       `json:"org_id"`
		ProjectID        interface{} `json:"project_id"`
		KeyID            int64       `json:"api_key_id"`
		OwnerUserID      interface{} `json:"owner_user_id"`
		KeyType          string      `json:"key_type"`
		Model            string      `json:"model"`
		RequestedModel   string      `json:"requested_model"`
		Provider         string      `json:"provider"`
		Channel          string      `json:"channel"`
		PromptTokens     int         `json:"prompt_tokens"`
		CompletionTokens int         `json:"completion_tokens"`
		CostCents        int         `json:"cost_cents"`
		StatusCode       int         `json:"status_code"`
		LatencyMS        int         `json:"latency_ms"`
		TTFTMS           int         `json:"ttft_ms"`
		UsageSource      string      `json:"usage_source"`
		IsStream         bool        `json:"is_stream"`
		EstimatedTokens  int         `json:"estimated_tokens"`
		UsageConfidence  float64     `json:"usage_confidence"`
	}{
		RequestID: requestID, OrgID: orgID, ProjectID: projectID, KeyID: keyID,
		OwnerUserID: ownerUserID, KeyType: keyType, Model: model, RequestedModel: requestedModel,
		Provider: provider, Channel: channel, PromptTokens: promptTokens, CompletionTokens: completionTokens,
		CostCents: costCents, StatusCode: statusCode, LatencyMS: latencyMs, TTFTMS: ttftMs,
		UsageSource: usageSource, IsStream: isStream, EstimatedTokens: estimatedTokens, UsageConfidence: usageConfidence,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func nullableString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
