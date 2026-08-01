package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

func newGatewayChatHandler(pool *pgxpool.Pool, relay *gateway.Relay, outboxServices ...*outbox.Service) http.HandlerFunc {
	var outboxService *outbox.Service
	if len(outboxServices) > 0 {
		outboxService = outboxServices[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		governedRequest, ok := prepareGatewayModelRequest(w, r, pool, relay, outboxService)
		if !ok {
			return
		}
		relay.HandleChatCompletion(w, governedRequest)
	}
}

func newGatewayEmbeddingsHandler(pool *pgxpool.Pool, relay *gateway.Relay, outboxServices ...*outbox.Service) http.HandlerFunc {
	var outboxService *outbox.Service
	if len(outboxServices) > 0 {
		outboxService = outboxServices[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		governedRequest, ok := prepareGatewayModelRequest(w, r, pool, relay, outboxService)
		if !ok {
			return
		}
		relay.HandleEmbeddings(w, governedRequest)
	}
}

func prepareGatewayModelRequest(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, relay *gateway.Relay, outboxService *outbox.Service) (*http.Request, bool) {
	apiKey := middleware.GetAPIKeyFromContext(r.Context())
	if apiKey == nil {
		http.Error(w, `{"error":{"type":"auth_error.invalid_api_key","message":"No API key"}}`, http.StatusUnauthorized)
		return nil, false
	}

	bodyBytes, cached := payloadpolicy.BodyFromContext(r.Context())
	if !cached {
		maxBytes := int64(8 * payloadpolicy.MiB)
		if r.URL.Path == "/v1/embeddings" {
			maxBytes = 16 * payloadpolicy.MiB
		}
		var err error
		bodyBytes, err = io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
		if err != nil {
			http.Error(w, `{"error":{"type":"server_error","message":"Failed to read request body"}}`, http.StatusBadRequest)
			return nil, false
		}
		if int64(len(bodyBytes)) > maxBytes {
			http.Error(w, `{"error":{"type":"validation_error.payload_too_large","message":"Request payload exceeds the endpoint safety limit"}}`, http.StatusRequestEntityTooLarge)
			return nil, false
		}
	}
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(bodyBytes, &req)

	if req.Model != "" {
		var ok bool
		var resolvedModel string
		r, bodyBytes, resolvedModel, ok = applyModelGovernance(w, r, pool, outboxService, apiKey, req.Model, bodyBytes)
		if !ok {
			return nil, false
		}
		if relay != nil {
			decision, decisionErr := relay.EvaluateModelAuthorization(r.Context(), r.URL.Path, req.Model, resolvedModel)
			if decisionErr != nil {
				http.Error(w, `{"error":{"type":"server_error.model_authorization_unavailable","message":"Model authorization is temporarily unavailable"}}`, http.StatusServiceUnavailable)
				return nil, false
			}
			if !decision.Allowed {
				recordModelAuthorizationDenial(r.Context(), pool, outboxService, apiKey, decision)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{
					"type": "auth_error.model_call_not_authorized", "message": "Model call is outside the active enterprise authorization policy",
					"model": req.Model, "reason": decision.Reason, "policy_refs": decision.PolicyRefs,
				}})
				return nil, false
			}
			r = r.WithContext(modelauthz.WithDecision(r.Context(), decision))
		}
	}

	// Relay owns API-key model denial so correlation and decision evidence are emitted.
	var replaceErr error
	r, replaceErr = payloadpolicy.ReplaceRequestBody(r, bodyBytes)
	if replaceErr != nil {
		http.Error(w, `{"error":{"type":"server_error.payload_capacity_exceeded","message":"Payload capacity is temporarily unavailable"}}`, http.StatusServiceUnavailable)
		return nil, false
	}
	return r, true
}

func recordModelAuthorizationDenial(ctx context.Context, pool *pgxpool.Pool, outboxService *outbox.Service, apiKey *models.ApiKey, decision modelauthz.Decision) {
	if pool == nil || apiKey == nil {
		return
	}
	projectID := int64(0)
	if apiKey.ProjectID != nil {
		projectID = *apiKey.ProjectID
	}
	detail := map[string]interface{}{
		"api_key_id": apiKey.ID, "model": decision.RequestedModel, "resolved_model": decision.ResolvedModel,
		"endpoint": decision.Endpoint, "reason": decision.Reason, "policy_refs": decision.PolicyRefs,
		"runtime_mutation": "none",
	}
	if outboxService != nil {
		if err := admin.EnqueueGatewayAudit(ctx, outboxService, apiKey.OrgID, projectID,
			"model_authorization.deny", "model_authorization", decision.RequestedModel, detail); err == nil {
			return
		}
	}
	body, _ := json.Marshal(detail)
	var project interface{}
	if projectID > 0 {
		project = projectID
	}
	_, _ = pool.Exec(ctx, `INSERT INTO audit_logs
        (org_id,user_id,action,resource,resource_id,detail,ip,user_agent,request_id,source,project_id,before_state,after_state,created_at)
        VALUES ($1,0,'model_authorization.deny','model_authorization',$2,$3::jsonb,'','',$4,'gateway_api',$5,'{}'::jsonb,'{}'::jsonb,NOW())`,
		apiKey.OrgID, truncateAuditResource(decision.RequestedModel), string(body), middleware.GetRequestIDFromContext(ctx), project)
}

func truncateAuditResource(value string) string {
	if len(value) > 64 {
		return value[:64]
	}
	return value
}

func applyModelGovernance(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, outboxService *outbox.Service, apiKey *models.ApiKey, requestedModel string, bodyBytes []byte) (*http.Request, []byte, string, bool) {
	projectID := int64(0)
	if apiKey.ProjectID != nil {
		projectID = *apiKey.ProjectID
	}
	access, err := modelgov.CheckAccess(r.Context(), pool, apiKey.OrgID, projectID, requestedModel)
	if errors.Is(err, modelgov.ErrModelRetired) || errors.Is(err, modelgov.ErrModelDenied) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    "auth_error.model_not_allowed",
				"message": "Model is not available for this project",
				"model":   requestedModel,
				"reason":  access.Reason,
			},
		})
		return nil, nil, "", false
	}
	if err != nil && !errors.Is(err, modelgov.ErrModelRequired) {
		http.Error(w, `{"error":{"type":"server_error","message":"Model governance check failed"}}`, http.StatusInternalServerError)
		return nil, nil, "", false
	}
	if access.Lifecycle == "" {
		recordUnregisteredModelAudit(r.Context(), pool, outboxService, apiKey, requestedModel, projectID)
	}

	resolvedModel := access.ResolvedModel
	if resolvedModel == "" || resolvedModel == requestedModel {
		return r, bodyBytes, resolvedModel, true
	}

	r = r.WithContext(context.WithValue(r.Context(), middleware.CtxModelAlias, requestedModel))
	var payload map[string]interface{}
	if json.Unmarshal(bodyBytes, &payload) != nil {
		return r, bodyBytes, resolvedModel, true
	}
	payload["model"] = resolvedModel
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return r, bodyBytes, resolvedModel, true
	}
	return r, rewritten, resolvedModel, true
}

func recordUnregisteredModelAudit(ctx context.Context, pool *pgxpool.Pool, outboxService *outbox.Service, apiKey *models.ApiKey, requestedModel string, projectID int64) {
	if pool == nil || apiKey == nil || apiKey.OrgID <= 0 || strings.TrimSpace(requestedModel) == "" {
		return
	}
	projectIDValue := interface{}(nil)
	if projectID > 0 {
		projectIDValue = projectID
	}
	detail := map[string]interface{}{
		"model":      requestedModel,
		"api_key_id": apiKey.ID,
		"key_prefix": apiKey.KeyPrefix,
		"project_id": projectIDValue,
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return
	}
	resourceID := requestedModel
	if len(resourceID) > 64 {
		resourceID = resourceID[:64]
	}
	if outboxService != nil {
		if err := admin.EnqueueGatewayAudit(
			ctx, outboxService, apiKey.OrgID, projectID,
			"model.unregistered.call", "model", resourceID, detail,
		); err == nil {
			return
		}
	}
	_, _ = pool.Exec(ctx,
		`INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail, ip, user_agent, request_id, source, project_id, before_state, after_state, created_at)
		 VALUES ($1, 0, 'model.unregistered.call', 'model', $2, $3::jsonb, '', '', $4, 'gateway_api', $5, '{}'::jsonb, $3::jsonb, NOW())`,
		apiKey.OrgID, resourceID, string(detailJSON), middleware.GetRequestIDFromContext(ctx), projectIDValue,
	)
}
