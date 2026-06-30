package admin

import (
	"encoding/json"
	"net/http"
)

func (h *Handler) ListSensitiveWords(w http.ResponseWriter, r *http.Request) {
	var valueJSON string
	err := h.db.QueryRow(r.Context(),
		`SELECT value FROM settings WHERE key = 'sensitive_words'`,
	).Scan(&valueJSON)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []string{}})
		return
	}

	var words []string
	if err := json.Unmarshal([]byte(valueJSON), &words); err != nil {
		words = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": words})
}

// UpdateSensitiveWords replaces the entire sensitive words list.
func (h *Handler) UpdateSensitiveWords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Words []string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}

	valueJSON, err := json.Marshal(req.Words)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Marshal failed"})
		return
	}

	_, err = h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ('sensitive_words', $1, '敏感词库', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = NOW()`,
		string(valueJSON),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}

	// Hot-reload the DFA filter
	if h.sensitiveFilter != nil {
		h.sensitiveFilter.UpdateWords(req.Words)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": req.Words})
}

// AddSensitiveWords adds words to the existing list.
func (h *Handler) AddSensitiveWords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Words []string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}

	// Get current words
	var valueJSON string
	err := h.db.QueryRow(r.Context(),
		`SELECT value FROM settings WHERE key = 'sensitive_words'`,
	).Scan(&valueJSON)
	if err != nil {
		valueJSON = "[]"
	}

	var existing []string
	json.Unmarshal([]byte(valueJSON), &existing)
	if existing == nil {
		existing = []string{}
	}

	// Merge (deduplicate)
	wordSet := make(map[string]bool)
	for _, w := range existing {
		wordSet[w] = true
	}
	for _, w := range req.Words {
		wordSet[w] = true
	}
	merged := make([]string, 0, len(wordSet))
	for w := range wordSet {
		merged = append(merged, w)
	}

	newJSON, _ := json.Marshal(merged)
	_, err = h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ('sensitive_words', $1, '敏感词库', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = NOW()`,
		string(newJSON),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}

	// Hot-reload
	if h.sensitiveFilter != nil {
		h.sensitiveFilter.UpdateWords(merged)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": merged})
}

// DeleteSensitiveWords removes words from the list.
func (h *Handler) DeleteSensitiveWords(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Words []string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return
	}

	var valueJSON string
	err := h.db.QueryRow(r.Context(),
		`SELECT value FROM settings WHERE key = 'sensitive_words'`,
	).Scan(&valueJSON)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []string{}})
		return
	}

	var existing []string
	json.Unmarshal([]byte(valueJSON), &existing)

	// Build removal set
	remove := make(map[string]bool)
	for _, w := range req.Words {
		remove[w] = true
	}
	var remaining []string
	for _, w := range existing {
		if !remove[w] {
			remaining = append(remaining, w)
		}
	}
	if remaining == nil {
		remaining = []string{}
	}

	newJSON, _ := json.Marshal(remaining)
	_, err = h.db.Exec(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ('sensitive_words', $1, '敏感词库', NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = NOW()`,
		string(newJSON),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}

	// Hot-reload
	if h.sensitiveFilter != nil {
		h.sensitiveFilter.UpdateWords(remaining)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": remaining})
}
