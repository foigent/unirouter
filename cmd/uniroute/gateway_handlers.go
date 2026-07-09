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
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/models"
)

func newGatewayChatHandler(pool *pgxpool.Pool, relay *gateway.Relay) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		governedRequest, ok := prepareGatewayModelRequest(w, r, pool)
		if !ok {
			return
		}
		relay.HandleChatCompletion(w, governedRequest)
	}
}

func newGatewayEmbeddingsHandler(pool *pgxpool.Pool, relay *gateway.Relay) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		governedRequest, ok := prepareGatewayModelRequest(w, r, pool)
		if !ok {
			return
		}
		relay.HandleEmbeddings(w, governedRequest)
	}
}

func prepareGatewayModelRequest(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool) (*http.Request, bool) {
	apiKey := middleware.GetAPIKeyFromContext(r.Context())
	if apiKey == nil {
		http.Error(w, `{"error":{"type":"auth_error.invalid_api_key","message":"No API key"}}`, http.StatusUnauthorized)
		return nil, false
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":{"type":"server_error","message":"Failed to read request body"}}`, http.StatusBadRequest)
		return nil, false
	}
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(bodyBytes, &req)

	if req.Model != "" {
		var ok bool
		r, bodyBytes, _, ok = applyModelGovernance(w, r, pool, apiKey, req.Model, bodyBytes)
		if !ok {
			return nil, false
		}
	}

	// Relay owns API-key model denial so correlation and decision evidence are emitted.
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	return r, true
}

func applyModelGovernance(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, apiKey *models.ApiKey, requestedModel string, bodyBytes []byte) (*http.Request, []byte, string, bool) {
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
		recordUnregisteredModelAudit(r.Context(), pool, apiKey, requestedModel, projectID)
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

func recordUnregisteredModelAudit(ctx context.Context, pool *pgxpool.Pool, apiKey *models.ApiKey, requestedModel string, projectID int64) {
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
	_, _ = pool.Exec(ctx,
		`INSERT INTO audit_logs (org_id, user_id, action, resource, resource_id, detail, ip, user_agent, request_id, source, project_id, before_state, after_state, created_at)
		 VALUES ($1, 0, 'model.unregistered.call', 'model', $2, $3::jsonb, '', '', $4, 'gateway_api', $5, '{}'::jsonb, $3::jsonb, NOW())`,
		apiKey.OrgID, resourceID, string(detailJSON), middleware.GetRequestIDFromContext(ctx), projectIDValue,
	)
}
