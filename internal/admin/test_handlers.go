package admin

import (
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
		testURL += "/models"
	} else {
		testURL += "/v1/models"
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(r.Context(), "GET", testURL, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "error", "error": err.Error(),
		})
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

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

	// Build curl example
	curlExample := fmt.Sprintf("curl %s -H \"Authorization: Bearer sk-***\"", testURL)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"status":       "ok",
		"status_code":  resp.StatusCode,
		"latency_ms":   latency,
		"models":       models,
		"response":     string(body),
		"curl_example": curlExample,
	})
}
