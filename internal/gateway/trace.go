package gateway

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"strings"

	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/traceutil"
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
	if apiKey := middleware.GetAPIKeyFromContext(ctx); apiKey != nil {
		if apiKey.OwnerUserID != nil && *apiKey.OwnerUserID > 0 {
			ownerUserIDArg = *apiKey.OwnerUserID
		}
		if apiKey.KeyType == "shared" || apiKey.KeyType == "personal" || apiKey.KeyType == "service" {
			keyType = apiKey.KeyType
		}
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
	metadataJSON, err := json.Marshal(traceutil.SanitizeMetadata(metadata))
	if err != nil {
		metadataJSON = []byte("{}")
	}
	if _, err := relay.db.Exec(ctx,
		`INSERT INTO request_trace_events (request_id, org_id, project_id, api_key_id, owner_user_id, key_type, event_type, stage, severity, model, requested_model, provider, channel, status_code, latency_ms, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		requestID, orgID, projectIDArg, keyIDArg, ownerUserIDArg, keyType, eventType, stage, severity, model, requestedModel, provider, channel, statusCodeArg, latencyMs, string(metadataJSON),
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
