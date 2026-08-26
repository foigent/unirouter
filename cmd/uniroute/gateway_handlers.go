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
	"github.com/uniroute/unirouter/internal/gatewayerror"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/modellist"
	"github.com/uniroute/unirouter/internal/modeloperation"
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

// newGatewayPublicNonStreamHandler retains its historical constructor name;
// the exact Responses v2 gateway entry may dispatch to its separately compiled
// stream executor after authorization and retained-body transport inspection.
func newGatewayPublicNonStreamHandler(
	pool *pgxpool.Pool,
	relay *gateway.Relay,
	operation modeloperation.ID,
	outboxServices ...*outbox.Service,
) http.HandlerFunc {
	var outboxService *outbox.Service
	if len(outboxServices) > 0 {
		outboxService = outboxServices[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		governedRequest, ok := prepareGatewayPublicModelRequest(w, r, pool, relay, outboxService)
		if !ok {
			return
		}
		relay.HandlePublicNonStream(w, governedRequest, operation)
	}
}

// prepareGatewayPublicModelRequest binds authorization to the caller's public
// model while preserving the retained request bytes exactly. Alias resolution
// remains internal planning/governance evidence and is never written into the
// Responses or Messages document sent through the public protocol profile.
func prepareGatewayPublicModelRequest(
	w http.ResponseWriter,
	r *http.Request,
	pool *pgxpool.Pool,
	relay *gateway.Relay,
	outboxService *outbox.Service,
) (*http.Request, bool) {
	requestID := middleware.GetRequestIDFromContext(r.Context())
	writeError := func(status int, code, message string) {
		gatewayerror.Write(w, r.URL.Path, requestID, status, code, message)
	}
	apiKey := middleware.GetAPIKeyFromContext(r.Context())
	if apiKey == nil {
		writeError(http.StatusUnauthorized, "auth_error.invalid_api_key", "Invalid API key")
		return nil, false
	}
	body, retained := payloadpolicy.BodyFromContext(r.Context())
	if !retained {
		writeError(http.StatusServiceUnavailable, "server_error.public_execution_unavailable", "Public model execution is temporarily unavailable")
		return nil, false
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil || document == nil {
		writeError(http.StatusBadRequest, "validation_error.invalid_request", "Invalid request")
		return nil, false
	}
	var requestedModel string
	modelJSON, exists := document["model"]
	if !exists || json.Unmarshal(modelJSON, &requestedModel) != nil || requestedModel == "" ||
		requestedModel != strings.TrimSpace(requestedModel) {
		writeError(http.StatusBadRequest, "validation_error.invalid_request", "A non-empty model string is required")
		return nil, false
	}
	if !apiKeyAllowsRequestedModel(apiKey, requestedModel) {
		writeError(http.StatusForbidden, "auth_error.model_not_allowed", "The requested model is not allowed for this API key")
		return nil, false
	}
	if relay == nil {
		writeError(http.StatusServiceUnavailable, "server_error.public_execution_unavailable", "Public model execution is temporarily unavailable")
		return nil, false
	}

	projectID := int64(0)
	if apiKey.ProjectID != nil {
		projectID = *apiKey.ProjectID
	}
	access, err := modelgov.CheckAccess(r.Context(), pool, apiKey.OrgID, projectID, requestedModel)
	if errors.Is(err, modelgov.ErrModelRetired) || errors.Is(err, modelgov.ErrModelDenied) {
		writeError(http.StatusForbidden, "auth_error.model_not_allowed", "The requested model is not available for this project")
		return nil, false
	}
	if err != nil && !errors.Is(err, modelgov.ErrModelRequired) {
		writeError(http.StatusServiceUnavailable, "server_error.model_governance_unavailable", "Model governance is temporarily unavailable")
		return nil, false
	}
	if access.Lifecycle == "" {
		recordUnregisteredModelAudit(r.Context(), pool, outboxService, apiKey, requestedModel, projectID)
	}
	resolvedModel := strings.TrimSpace(access.ResolvedModel)
	if resolvedModel == "" {
		resolvedModel = requestedModel
	}
	decision, decisionErr := relay.EvaluateModelAuthorization(r.Context(), r.URL.Path, requestedModel, resolvedModel)
	if decisionErr != nil {
		writeError(http.StatusServiceUnavailable, "server_error.model_authorization_unavailable", "Model authorization is temporarily unavailable")
		return nil, false
	}
	if !decision.Allowed {
		recordModelAuthorizationDenial(r.Context(), pool, outboxService, apiKey, decision)
		writeError(http.StatusForbidden, "auth_error.model_call_not_authorized", "Model call is outside the active enterprise authorization policy")
		return nil, false
	}
	r = r.WithContext(modelauthz.WithDecision(r.Context(), decision))
	r = r.WithContext(context.WithValue(r.Context(), middleware.CtxModelAlias, requestedModel))
	return r, true
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
		requestedModel := strings.TrimSpace(req.Model)
		if !apiKeyAllowsRequestedModel(apiKey, requestedModel) {
			writeGatewayModelScopeDenied(w)
			return nil, false
		}
		var ok bool
		var resolvedModel string
		r, bodyBytes, resolvedModel, ok = applyModelGovernance(w, r, pool, outboxService, apiKey, requestedModel, bodyBytes)
		if !ok {
			return nil, false
		}
		if relay != nil {
			decision, decisionErr := relay.EvaluateModelAuthorization(r.Context(), r.URL.Path, requestedModel, resolvedModel)
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
					"model": requestedModel, "reason": decision.Reason, "policy_refs": decision.PolicyRefs,
				}})
				return nil, false
			}
			r = r.WithContext(modelauthz.WithDecision(r.Context(), decision))
		}
		// This context identity is written only after API-key scope, governance,
		// and enterprise authorization have all accepted the normalized public
		// model. Freeze compares it with the private early receipt marker.
		r = r.WithContext(context.WithValue(r.Context(), middleware.CtxModelAlias, requestedModel))
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

// apiKeyAllowsRequestedModel is the first model-specific authority boundary.
// It deliberately evaluates only the caller-supplied public identity before
// any alias, catalog, project-policy, audit, or enterprise-authorization read.
// The resolved target is subsequently constrained by those server-owned
// authorities and must never be disclosed by this denial path.
func apiKeyAllowsRequestedModel(apiKey *models.ApiKey, requestedModel string) bool {
	if apiKey == nil || apiKey.ModelScopeDenied {
		return false
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return false
	}
	if strings.TrimSpace(apiKey.AllowedModels) == "" {
		return true
	}
	for _, allowed := range modellist.Parse(apiKey.AllowedModels) {
		if allowed == "*" || allowed == requestedModel {
			return true
		}
	}
	return false
}

func writeGatewayModelScopeDenied(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"type":    "permission_error",
			"code":    "auth_error.model_not_allowed",
			"message": "The requested model is not allowed for this API key",
		},
	})
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

	resolvedModel := strings.TrimSpace(access.ResolvedModel)
	if resolvedModel == "" {
		resolvedModel = requestedModel
	}

	rewritten, err := rewriteGovernedModel(bodyBytes, resolvedModel)
	if err != nil {
		http.Error(w, `{"error":{"type":"server_error","message":"Model governance rewrite failed"}}`, http.StatusInternalServerError)
		return nil, nil, "", false
	}
	return r, rewritten, resolvedModel, true
}

// rewriteGovernedModel changes only the server-governed model field. Every
// other request member stays as raw JSON so alias resolution cannot erase
// newer OpenAI fields or round provider extension numbers through float64.
func rewriteGovernedModel(body []byte, resolvedModel string) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, errors.New("gateway request must be a JSON object")
	}
	modelJSON, err := json.Marshal(resolvedModel)
	if err != nil {
		return nil, err
	}
	payload["model"] = modelJSON
	return json.Marshal(payload)
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
