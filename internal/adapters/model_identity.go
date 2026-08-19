package adapters

import (
	"encoding/json"
	"strings"
	"unicode"
)

// responseModelIdentity returns only an explicit top-level provider model
// field. It never falls back to the requested model, because callers use this
// value as live identity evidence rather than request bookkeeping.
func responseModelIdentity(body []byte) string {
	var envelope struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	model := strings.TrimSpace(envelope.Model)
	if model == "" || len(model) > 200 || strings.IndexFunc(model, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return ""
	}
	return model
}
