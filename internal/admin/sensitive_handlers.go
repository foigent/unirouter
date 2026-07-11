package admin

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (h *Handler) ListSensitiveWords(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformGlobalControl(w, r) {
		return
	}
	raw, found, err := readGlobalSetting(r.Context(), h.db, sensitiveWordsSettingKey, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read sensitive words"})
		return
	}
	words, err := decodeSensitiveWords(raw, found)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Stored sensitive words are invalid"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": words})
}

// UpdateSensitiveWords replaces the entire sensitive words list.
func (h *Handler) UpdateSensitiveWords(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformGlobalControl(w, r) {
		return
	}
	words, ok := decodeSensitiveWordsRequest(w, r)
	if !ok {
		return
	}
	h.mutateSensitiveWords(w, r, "sensitive_words.replace", func(_ []string) []string { return words })
}

// AddSensitiveWords adds words to the existing list.
func (h *Handler) AddSensitiveWords(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformGlobalControl(w, r) {
		return
	}
	words, ok := decodeSensitiveWordsRequest(w, r)
	if !ok {
		return
	}
	h.mutateSensitiveWords(w, r, "sensitive_words.add", func(existing []string) []string {
		return append(existing, words...)
	})
}

// DeleteSensitiveWords removes words from the list.
func (h *Handler) DeleteSensitiveWords(w http.ResponseWriter, r *http.Request) {
	if !requirePlatformGlobalControl(w, r) {
		return
	}
	words, ok := decodeSensitiveWordsRequest(w, r)
	if !ok {
		return
	}
	remove := make(map[string]struct{}, len(words))
	for _, word := range words {
		remove[word] = struct{}{}
	}
	h.mutateSensitiveWords(w, r, "sensitive_words.delete", func(existing []string) []string {
		remaining := make([]string, 0, len(existing))
		for _, word := range existing {
			if _, removed := remove[word]; !removed {
				remaining = append(remaining, word)
			}
		}
		return remaining
	})
}

func decodeSensitiveWordsRequest(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	var req struct {
		Words *[]string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body"})
		return nil, false
	}
	if req.Words == nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "words must be an array"})
		return nil, false
	}
	return normalizeSensitiveWords(*req.Words), true
}

func decodeSensitiveWords(raw []byte, found bool) ([]string, error) {
	if !found {
		return []string{}, nil
	}
	var words []string
	if err := json.Unmarshal(raw, &words); err != nil {
		return nil, err
	}
	return normalizeSensitiveWords(words), nil
}

func normalizeSensitiveWords(words []string) []string {
	normalized := make([]string, 0, len(words))
	seen := make(map[string]struct{}, len(words))
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		if _, ok := seen[word]; ok {
			continue
		}
		seen[word] = struct{}{}
		normalized = append(normalized, word)
	}
	sort.Strings(normalized)
	return normalized
}

func (h *Handler) mutateSensitiveWords(w http.ResponseWriter, r *http.Request, action string, mutate func([]string) []string) {
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Transaction failed"})
		return
	}
	defer tx.Rollback(r.Context())
	if err := lockGlobalControlKey(r.Context(), tx, sensitiveWordsSettingKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Lock failed"})
		return
	}
	beforeRaw, beforeFound, err := readGlobalSetting(r.Context(), tx, sensitiveWordsSettingKey, true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Failed to read sensitive words"})
		return
	}
	existing, err := decodeSensitiveWords(beforeRaw, beforeFound)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Stored sensitive words are invalid"})
		return
	}
	next := normalizeSensitiveWords(mutate(append([]string(nil), existing...)))
	nextRaw, err := json.Marshal(next)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Marshal failed"})
		return
	}
	var appliedVersion time.Time
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO settings (key, value, description, updated_at)
		 VALUES ($1, $2::jsonb, '敏感词库', clock_timestamp())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, description = EXCLUDED.description,
		  updated_at = GREATEST(clock_timestamp(), settings.updated_at + interval '1 microsecond')
		 RETURNING updated_at`,
		sensitiveWordsSettingKey, string(nextRaw),
	).Scan(&appliedVersion); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Update failed"})
		return
	}
	beforeCanonical, _ := json.Marshal(existing)
	if err := insertAuditEntry(r.Context(), tx, action, "sensitive_word_policy", "global", map[string]interface{}{
		"scope":          "platform_global",
		"policy_version": appliedVersion.UTC().Format(time.RFC3339Nano),
		"before":         sensitiveWordsAuditSnapshot(beforeCanonical, beforeFound, len(existing)),
		"after":          sensitiveWordsAuditSnapshot(nextRaw, true, len(next)),
	}, globalControlAuditMeta(r)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Audit failed"})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Commit failed"})
		return
	}
	if h.sensitiveFilter != nil {
		h.sensitiveFilter.UpdateWordsVersioned(append([]string(nil), next...), appliedVersion)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": next})
}

func sensitiveWordsAuditSnapshot(raw []byte, present bool, count int) map[string]interface{} {
	snapshot := globalSettingSnapshot(raw, present)
	return map[string]interface{}{
		"present": snapshot.Present, "word_count": count,
		"value_sha256": snapshot.ValueSHA256, "value_bytes": snapshot.ValueBytes,
	}
}
