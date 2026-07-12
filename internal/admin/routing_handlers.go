package admin

import (
	"net/http"
	"strings"
)

// PreviewRouting exposes the credential-free routing-policy.v1 evaluation to
// platform operators. The preview never claims a half-open probe lease and
// never returns provider credentials, request payloads, or raw responses.
func (h *Handler) PreviewRouting(w http.ResponseWriter, r *http.Request) {
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model is required"})
		return
	}
	if len(model) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "model is too long"})
		return
	}
	if h.routingController == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Routing preview unavailable"})
		return
	}
	preview, err := h.routingController.Preview(r.Context(), model)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Routing preview unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, preview)
}
