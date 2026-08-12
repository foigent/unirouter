package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/modellist"
	"github.com/uniroute/unirouter/internal/models"
)

func (h *Handler) TestChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}

	var baseURL, channelModels, adapterType string
	var adapterConfig, modelMapping json.RawMessage
	err = h.db.QueryRow(r.Context(),
		`SELECT base_url, models, COALESCE(adapter_type, 'openai_compatible'), COALESCE(adapter_config, '{}'::jsonb),
		        COALESCE(model_mapping, '{}'::jsonb)
		 FROM channels WHERE id = $1`, id,
	).Scan(&baseURL, &channelModels, &adapterType, &adapterConfig, &modelMapping)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Channel not found"})
		return
	}

	var reqBody struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)
	if strings.TrimSpace(reqBody.Model) == "" {
		configuredModels := modellist.Parse(channelModels)
		if len(configuredModels) > 0 {
			reqBody.Model = configuredModels[0]
		}
	}
	if strings.TrimSpace(reqBody.Model) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model is required"})
		return
	}
	reqBody.Model = strings.TrimSpace(reqBody.Model)
	upstreamModel := resolveChannelTestModel(reqBody.Model, modelMapping)
	if !channelTestModelConfigured(channelModels, reqBody.Model) {
		h.recordChannelTestAudit(r, id, reqBody.Model, adapterType, map[string]interface{}{
			"success": false,
			"status":  "model_not_configured",
			"error":   "Selected model is not configured on this channel",
		})
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":  "Selected model is not configured on this channel",
			"status": "model_not_configured",
		})
		return
	}
	if strings.TrimSpace(reqBody.Prompt) == "" {
		reqBody.Prompt = "Hello, this is a test message."
	}

	// Connectivity checks may validate staged or disabled channels before a
	// release, but they must still use the same credential model scope and
	// cooldown eligibility as runtime routing. Never fall back to an arbitrary
	// active credential for a model it does not permit.
	var keyEnc string
	err = h.db.QueryRow(r.Context(),
		`SELECT ck.key_enc
		   FROM channel_keys ck
		  WHERE ck.channel_id = $1
		    AND ck.status = 'active'
		    AND (ck.cooldown_until IS NULL OR ck.cooldown_until <= NOW())
		    AND (
		        BTRIM(COALESCE(ck.allowed_models, '')) = ''
		        OR EXISTS (
		            SELECT 1
		              FROM regexp_split_to_table(COALESCE(ck.allowed_models, ''), '[,，;；\r\n]+') AS allowed(model)
		             WHERE BTRIM(allowed.model) = $2
		        )
		    )
		  ORDER BY ck.id
		  LIMIT 1`, id, reqBody.Model,
	).Scan(&keyEnc)
	if err != nil {
		h.recordChannelTestAudit(r, id, reqBody.Model, adapterType, map[string]interface{}{
			"success": false,
			"status":  "no_eligible_key",
			"error":   "No active, non-cooling credential allows the selected model",
		})
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":  "No active, non-cooling credential allows the selected model",
			"status": "no_eligible_key",
		})
		return
	}

	// Decrypt the key
	apiKey, err := auth.DecryptKey(keyEnc, h.encKey)
	if err != nil {
		h.recordChannelTestAudit(r, id, reqBody.Model, adapterType, map[string]interface{}{
			"success": false,
			"status":  "decrypt_error",
			"error":   "Failed to decrypt key",
		})
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to decrypt key"})
		return
	}

	adapter, err := adapters.DefaultRegistry().Get(adapterType)
	if err != nil {
		h.recordChannelTestAudit(r, id, reqBody.Model, adapterType, map[string]interface{}{
			"success": false,
			"status":  "adapter_error",
			"error":   "Unknown adapter_type",
		})
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":        "Unknown adapter_type",
			"adapter_type": adapterType,
			"status":       "adapter_error",
		})
		return
	}

	result, err := adapter.Test(r.Context(), adapters.TestInput{
		Channel: models.Channel{
			ID:            id,
			BaseURL:       baseURL,
			Models:        channelModels,
			ModelMapping:  modelMapping,
			AdapterType:   adapters.NormalizeType(adapterType),
			AdapterConfig: adapterConfig,
		},
		APIKey: apiKey,
		Model:  upstreamModel,
		Prompt: reqBody.Prompt,
	})
	if err != nil {
		h.recordChannelTestAudit(r, id, reqBody.Model, adapterType, map[string]interface{}{
			"success":        false,
			"status":         "error",
			"upstream_model": upstreamModel,
			"error":          auditErrorSummary(err.Error()),
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "error", "error": err.Error(), "model": reqBody.Model, "upstream_model": upstreamModel,
		})
		return
	}

	// Build curl example
	curlExample := fmt.Sprintf("curl %s -H \"Authorization: Bearer sk-***\" -H \"Content-Type: application/json\" -d '{\"model\":\"%s\",\"messages\":[{\"role\":\"user\",\"content\":\"%s\"}]}'", result.RequestURL, upstreamModel, reqBody.Prompt)
	if adapters.NormalizeType(adapterType) == adapters.TypeAnthropicNative {
		curlExample = fmt.Sprintf("curl %s -H \"x-api-key: sk-***\" -H \"anthropic-version: 2023-06-01\" -H \"Content-Type: application/json\" -d '{\"model\":\"%s\",\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"%s\"}]}'", result.RequestURL, upstreamModel, reqBody.Prompt)
	} else if adapters.NormalizeType(adapterType) == adapters.TypeAzureOpenAI {
		curlExample = fmt.Sprintf("curl %s -H \"api-key: ***\" -H \"Content-Type: application/json\" -d '{\"messages\":[{\"role\":\"user\",\"content\":\"%s\"}],\"max_tokens\":16}'", result.RequestURL, reqBody.Prompt)
	}

	h.recordChannelTestAudit(r, id, reqBody.Model, adapterType, map[string]interface{}{
		"success":        result.Success,
		"status":         result.Status,
		"status_code":    result.StatusCode,
		"latency_ms":     result.LatencyMs,
		"tokens_used":    result.TokensUsed,
		"upstream_model": upstreamModel,
		"error":          auditErrorSummary(result.Error),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        result.Success,
		"status":         result.Status,
		"channel_id":     id,
		"adapter_type":   adapters.NormalizeType(adapterType),
		"status_code":    result.StatusCode,
		"latency_ms":     result.LatencyMs,
		"model":          reqBody.Model,
		"upstream_model": upstreamModel,
		"tokens_used":    result.TokensUsed,
		"models":         channelModels,
		"error":          result.Error,
		"response":       string(result.Body),
		"curl_example":   curlExample,
	})
}

func channelTestModelConfigured(channelModels, selectedModel string) bool {
	selectedModel = strings.TrimSpace(selectedModel)
	if selectedModel == "" {
		return false
	}
	return modellist.Contains(channelModels, selectedModel)
}

// resolveChannelTestModel deliberately mirrors gateway routing semantics: an
// invalid or absent mapping falls back to the requested model, while an exact
// key match is sent upstream.
func resolveChannelTestModel(requestedModel string, modelMapping json.RawMessage) string {
	if len(modelMapping) == 0 || string(modelMapping) == "{}" || string(modelMapping) == "null" {
		return requestedModel
	}
	var mapping map[string]string
	if err := json.Unmarshal(modelMapping, &mapping); err != nil {
		return requestedModel
	}
	if mapped, ok := mapping[requestedModel]; ok {
		return mapped
	}
	return requestedModel
}

func (h *Handler) recordChannelTestAudit(r *http.Request, channelID int64, model, adapterType string, result map[string]interface{}) {
	after := map[string]interface{}{
		"channel_id":   channelID,
		"model":        model,
		"adapter_type": adapters.NormalizeType(adapterType),
	}
	for key, value := range result {
		if value != nil && value != "" {
			after[key] = value
		}
	}
	h.recordAuditFromRequest(r, "channel.test", "channel", strconv.FormatInt(channelID, 10), map[string]interface{}{"after": after})
}

func auditErrorSummary(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 512 {
		value = value[:512]
	}
	return value
}
