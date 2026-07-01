package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/auth"
)

func (h *Handler) TestChannel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid channel ID"})
		return
	}

	var baseURL, models string
	err = h.db.QueryRow(r.Context(),
		`SELECT base_url, models FROM channels WHERE id = $1`, id,
	).Scan(&baseURL, &models)
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
		for _, m := range strings.Split(models, ",") {
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
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":  "No active key found for this channel",
			"status": "no_key",
		})
		return
	}

	// Decrypt the key
	apiKey, err := auth.DecryptKey(keyEnc, h.encKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to decrypt key"})
		return
	}

	// Build the test URL
	testURL := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(testURL, "/v1") {
		testURL += "/chat/completions"
	} else {
		testURL += "/v1/chat/completions"
	}

	payload, err := json.Marshal(map[string]interface{}{
		"model": reqBody.Model,
		"messages": []map[string]string{
			{"role": "user", "content": reqBody.Prompt},
		},
		"max_tokens":  16,
		"temperature": 0,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to build test payload"})
		return
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(r.Context(), "POST", testURL, bytes.NewReader(payload))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "error", "error": err.Error(),
		})
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":    false,
			"status":     "error",
			"error":      err.Error(),
			"latency_ms": latency,
		})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	status := "failed"
	if success {
		status = "ok"
	}
	errorMessage := ""
	if !success {
		errorMessage = fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode)
	}

	var tokensUsed int
	var parsed struct {
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		tokensUsed = parsed.Usage.TotalTokens
	}

	// Build curl example
	curlExample := fmt.Sprintf("curl %s -H \"Authorization: Bearer sk-***\" -H \"Content-Type: application/json\" -d '{\"model\":\"%s\",\"messages\":[{\"role\":\"user\",\"content\":\"%s\"}]}'", testURL, reqBody.Model, reqBody.Prompt)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      success,
		"status":       status,
		"channel_id":   id,
		"status_code":  resp.StatusCode,
		"latency_ms":   latency,
		"model":        reqBody.Model,
		"tokens_used":  tokensUsed,
		"models":       models,
		"error":        errorMessage,
		"response":     string(body),
		"curl_example": curlExample,
	})
}
