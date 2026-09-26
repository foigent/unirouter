package admin

import (
	"io"
	"net/http"
)

// ContentControl is mounted only in the original platform safety or licensed
// data-exchange route group. The coordinator repeats actor authorization in tx.
func (h *Handler) ContentControl(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		safety := action == "pause" || action == "cancel" || action == "revoke"
		if !h.sourceControlReady(w, !safety) {
			return
		}
		wire, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": "Invalid content control JSON"})
			return
		}
		request, err := decodeContentControl(wire, action)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": "Invalid content control JSON"})
			return
		}
		project, err := xInsightFeedProjectID(r)
		if err != nil {
			writeXInsightFeedControlError(w, err)
			return
		}
		org, err := h.xInsightFeedExistingProjectOrg(r.Context(), project)
		if err != nil {
			writeXInsightFeedControlError(w, err)
			return
		}
		tx, err := h.db.Begin(r.Context())
		if err != nil {
			writeXInsightFeedControlError(w, err)
			return
		}
		defer tx.Rollback(r.Context())
		present, err := hasContentScopeTx(r.Context(), tx)
		if err != nil {
			writeXInsightFeedControlError(w, err)
			return
		}
		if !present {
			writeJSON(w, 404, map[string]any{"error": "Content lifecycle is not supported"})
			return
		}
		result, err := h.contentControlTx(r.Context(), tx, org, project, action, request, r)
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			writeXInsightFeedControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
