package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
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
	var adapterConfig json.RawMessage
	err = h.db.QueryRow(r.Context(),
		`SELECT base_url, models, COALESCE(adapter_type, 'openai_compatible'), COALESCE(adapter_config, '{}'::jsonb)
		 FROM channels WHERE id = $1`, id,
	).Scan(&baseURL, &channelModels, &adapterType, &adapterConfig)
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
		for _, m := range strings.Split(channelModels, ",") {
			if model := strings.TrimSpace(m); model != "" {
				reqBody.Model = model
				break
			}
		}
	}
	if strings.TrimSpace(reqBody.Model) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model is required"})
		return
	}
	if strings.TrimSpace(reqBody.Prompt) == "" {
		reqBody.Prompt = "Hello, this is a test message."
	}

	// Get first active channel key for this channel
	var keyEnc string
	err = h.db.QueryRow(r.Context(),
		`SELECT key_enc FROM channel_keys WHERE channel_id = $1 AND status = 'active' ORDER BY id LIMIT 1`, id,
	).Scan(&keyEnc)
	if err != nil {
		h.recordChannelTestAudit(r, id, reqBody.Model, adapterType, map[string]interface{}{
			"success": false,
			"status":  "no_key",
			"error":   "No active key found for this channel",
		})
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":  "No active key found for this channel",
			"status": "no_key",
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
			AdapterType:   adapters.NormalizeType(adapterType),
			AdapterConfig: adapterConfig,
		},
		APIKey: apiKey,
		Model:  reqBody.Model,
		Prompt: reqBody.Prompt,
	})
	if err != nil {
		h.recordChannelTestAudit(r, id, reqBody.Model, adapterType, map[string]interface{}{
			"success": false,
			"status":  "error",
			"error":   auditErrorSummary(err.Error()),
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "error", "error": err.Error(),
		})
		return
	}

	// Build curl example
	curlExample := fmt.Sprintf("curl %s -H \"Authorization: Bearer sk-***\" -H \"Content-Type: application/json\" -d '{\"model\":\"%s\",\"messages\":[{\"role\":\"user\",\"content\":\"%s\"}]}'", result.RequestURL, reqBody.Model, reqBody.Prompt)
	if adapters.NormalizeType(adapterType) == adapters.TypeAnthropicNative {
		curlExample = fmt.Sprintf("curl %s -H \"x-api-key: sk-***\" -H \"anthropic-version: 2023-06-01\" -H \"Content-Type: application/json\" -d '{\"model\":\"%s\",\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"%s\"}]}'", result.RequestURL, reqBody.Model, reqBody.Prompt)
	} else if adapters.NormalizeType(adapterType) == adapters.TypeAzureOpenAI {
		curlExample = fmt.Sprintf("curl %s -H \"api-key: ***\" -H \"Content-Type: application/json\" -d '{\"messages\":[{\"role\":\"user\",\"content\":\"%s\"}],\"max_tokens\":16}'", result.RequestURL, reqBody.Prompt)
	}

	h.recordChannelTestAudit(r, id, reqBody.Model, adapterType, map[string]interface{}{
		"success":     result.Success,
		"status":      result.Status,
		"status_code": result.StatusCode,
		"latency_ms":  result.LatencyMs,
		"tokens_used": result.TokensUsed,
		"error":       auditErrorSummary(result.Error),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      result.Success,
		"status":       result.Status,
		"channel_id":   id,
		"adapter_type": adapters.NormalizeType(adapterType),
		"status_code":  result.StatusCode,
		"latency_ms":   result.LatencyMs,
		"model":        reqBody.Model,
		"tokens_used":  result.TokensUsed,
		"models":       channelModels,
		"error":        result.Error,
		"response":     string(result.Body),
		"curl_example": curlExample,
	})
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
