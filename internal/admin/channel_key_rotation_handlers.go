package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modellist"
)

type rotateChannelKeyRequest struct {
	Key           string  `json:"key"`
	AllowedModels *string `json:"allowed_models,omitempty"`
	Model         string  `json:"model"`
	Prompt        string  `json:"prompt,omitempty"`
}

// RotateChannelKey probes a replacement secret before it can become routing
// authority, then swaps credentials and probe evidence in one transaction.
// The current route remains usable throughout the network probe.
func (h *Handler) RotateChannelKey(w http.ResponseWriter, r *http.Request) {
	credentialID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || credentialID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid credential ID"})
		return
	}
	var request rotateChannelKeyRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid credential rotation request"})
		return
	}
	request.Key = strings.TrimSpace(request.Key)
	request.Model = strings.TrimSpace(request.Model)
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Key == "" || request.Model == "" || len(request.Model) > 160 || strings.ContainsAny(request.Model, "\x00\r\n") {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "key and model are required"})
		return
	}
	if request.Prompt == "" {
		request.Prompt = "Reply with exactly: OK"
	}

	var (
		channelID, oldStateEpoch, routingEpoch              int64
		oldAllowedModels, oldStatus, baseURL, channelModels string
		adapterType                                         string
		adapterConfig, modelMapping                         json.RawMessage
	)
	err = h.db.QueryRow(r.Context(), `
SELECT ck.channel_id,ck.allowed_models,ck.status,ck.state_epoch,
       c.base_url,c.models,COALESCE(c.adapter_type,'openai_compatible'),
       COALESCE(c.adapter_config,'{}'::jsonb),COALESCE(c.model_mapping,'{}'::jsonb),c.routing_epoch
  FROM channel_keys ck JOIN channels c ON c.id=ck.channel_id
 WHERE ck.id=$1`, credentialID).Scan(
		&channelID, &oldAllowedModels, &oldStatus, &oldStateEpoch,
		&baseURL, &channelModels, &adapterType, &adapterConfig, &modelMapping, &routingEpoch,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel credential not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read channel credential"})
		return
	}
	if oldStatus != "active" {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Only an active credential can be rotated"})
		return
	}
	if !channelTestModelConfigured(channelModels, request.Model) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Selected model is not configured on this channel"})
		return
	}
	allowedModels := oldAllowedModels
	if request.AllowedModels != nil {
		allowedModels = normalizeChannelKeyAllowedModels(*request.AllowedModels)
	}
	if strings.TrimSpace(allowedModels) != "" && !modellist.Contains(allowedModels, request.Model) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Replacement credential does not allow the selected model"})
		return
	}

	upstreamModel := resolveChannelTestModel(request.Model, modelMapping)
	result, probeErr := runProviderCredentialProbe(r.Context(), channelID, baseURL, channelModels, adapterType,
		adapterConfig, modelMapping, request.Key, upstreamModel, request.Prompt)
	if probeErr != nil {
		h.recordAuditFromRequest(r, "channelkey.rotate_probe_failed", "channel_key", strconv.FormatInt(credentialID, 10), map[string]interface{}{
			"after": map[string]interface{}{"channel_id": channelID, "model": request.Model, "upstream_model": upstreamModel, "error": auditErrorSummary(probeErr.Error())},
		})
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"error": "Replacement credential probe failed", "detail": auditErrorSummary(probeErr.Error())})
		return
	}
	if result == nil || !result.Success {
		errorSummary := "Provider rejected the replacement credential"
		statusCode := 0
		latencyMS := int64(0)
		if result != nil {
			statusCode = result.StatusCode
			latencyMS = result.LatencyMs
			if strings.TrimSpace(result.Error) != "" {
				errorSummary = auditErrorSummary(result.Error)
			}
		}
		h.recordAuditFromRequest(r, "channelkey.rotate_probe_failed", "channel_key", strconv.FormatInt(credentialID, 10), map[string]interface{}{
			"after": map[string]interface{}{"channel_id": channelID, "model": request.Model, "upstream_model": upstreamModel, "status_code": statusCode, "latency_ms": latencyMS, "error": errorSummary},
		})
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"error": errorSummary, "status": "probe_failed", "status_code": statusCode,
			"model": request.Model, "upstream_model": upstreamModel, "latency_ms": latencyMS,
		})
		return
	}

	encrypted, err := auth.EncryptKey([]byte(request.Key), h.encKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to encrypt replacement credential"})
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to begin credential rotation"})
		return
	}
	defer tx.Rollback(r.Context())

	var currentStatus string
	var currentStateEpoch, currentRoutingEpoch int64
	err = tx.QueryRow(r.Context(), `
SELECT ck.status,ck.state_epoch,c.routing_epoch
  FROM channel_keys ck JOIN channels c ON c.id=ck.channel_id
 WHERE ck.id=$1 AND ck.channel_id=$2
 FOR UPDATE OF ck,c`, credentialID, channelID).Scan(&currentStatus, &currentStateEpoch, &currentRoutingEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Credential changed while its replacement was being tested"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to lock credential rotation"})
		return
	}
	if currentStatus != "active" || currentStateEpoch != oldStateEpoch || currentRoutingEpoch != routingEpoch {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Credential or endpoint changed while its replacement was being tested; no change was applied"})
		return
	}

	var replacementID, replacementStateEpoch int64
	err = tx.QueryRow(r.Context(), `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status,fail_count,state_epoch,created_at,updated_at)
VALUES ($1,$2,$3,'active',0,1,NOW(),NOW())
RETURNING id,state_epoch`, channelID, encrypted, allowedModels).Scan(&replacementID, &replacementStateEpoch)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to create replacement credential"})
		return
	}
	retireTag, err := tx.Exec(r.Context(), `
UPDATE channel_keys
   SET status='disabled',state_epoch=state_epoch+1,updated_at=NOW()
	 WHERE id=$1 AND channel_id=$2 AND status='active' AND state_epoch=$3`, credentialID, channelID, oldStateEpoch)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to retire previous credential"})
		return
	}
	if retireTag.RowsAffected() != 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Credential changed while its replacement was being committed; no change was applied"})
		return
	}
	if err = upsertChannelModelProbe(r.Context(), tx, channelModelProbeRecord{
		ChannelID: channelID, PublicModel: request.Model, UpstreamModel: upstreamModel,
		RoutingEpoch: routingEpoch, CredentialID: replacementID, CredentialStateEpoch: replacementStateEpoch,
		Success: true, StatusCode: result.StatusCode, LatencyMS: result.LatencyMs,
	}, middleware.GetUserIDFromContext(r.Context())); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to bind replacement probe evidence"})
		return
	}
	if !isUnauthenticatedUnitContext(r.Context()) {
		err = insertAuditEntry(r.Context(), tx, "channelkey.rotate", "channel_key", strconv.FormatInt(replacementID, 10), map[string]interface{}{
			"before": map[string]interface{}{"credential_id": credentialID, "channel_id": channelID, "state_epoch": oldStateEpoch},
			"after": map[string]interface{}{
				"credential_id": replacementID, "channel_id": channelID, "state_epoch": replacementStateEpoch,
				"rotated_from_credential_id": credentialID, "model": request.Model, "upstream_model": upstreamModel,
				"probe_status_code": result.StatusCode, "probe_latency_ms": result.LatencyMs,
			},
		}, requestAuditMeta(r, middleware.GetOrgIDFromContext(r.Context()), middleware.GetProjectIDFromContext(r.Context())))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to audit credential rotation"})
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to commit credential rotation"})
		return
	}

	h.invalidateRoutingSnapshots()
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": replacementID, "key_prefix": maskKey(encrypted), "rotated_from_id": credentialID,
		"channel_id": channelID, "model": request.Model, "upstream_model": upstreamModel,
		"probe": map[string]interface{}{"success": true, "status_code": result.StatusCode, "latency_ms": result.LatencyMs},
	})
}
