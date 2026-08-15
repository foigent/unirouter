package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/uniroute/unirouter/internal/gateway"
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
		if errors.Is(err, gateway.ErrRuntimeAssignmentStale) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":     "当前发布记录与连接版本不一致，不能作为可用路由",
				"code":      "runtime_assignment_stale",
				"next_step": "在模型连接中处理该连接保留的旧发布记录后重新检测并启用",
			})
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Routing preview unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, preview)
}
