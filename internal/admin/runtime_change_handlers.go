package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/runtimechange"
)

func (h *Handler) ListRuntimeChanges(w http.ResponseWriter, r *http.Request) {
	if h.runtimeChangeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime change control unavailable"})
		return
	}
	page, pageSize := parsePagination(r)
	items, err := h.runtimeChangeManager.ListPage(r.Context(), r.URL.Query().Get("model"), page, pageSize)
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) GetRuntimeChange(w http.ResponseWriter, r *http.Request) {
	if h.runtimeChangeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime change control unavailable"})
		return
	}
	item, err := h.runtimeChangeManager.Get(r.Context(), r.PathValue("ref"))
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) GetRuntimeChangeSlot(w http.ResponseWriter, r *http.Request) {
	if h.runtimeChangeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime change control unavailable"})
		return
	}
	item, err := h.runtimeChangeManager.Slot(r.Context(), r.URL.Query().Get("model"))
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) CreateRuntimeChange(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) ValidateRuntimeChange(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) AdvanceRuntimeChangeShadow(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) ActivateInitialRuntimeChange(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) StartRuntimeChangeCanary(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) PromoteRuntimeChange(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) RollbackRuntimeChange(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func (h *Handler) RejectRuntimeChange(w http.ResponseWriter, _ *http.Request) {
	writeLegacyModelMutationRetired(w)
}

func writeRuntimeChangeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, runtimechange.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Runtime change rejected", "message": err.Error()})
	case errors.Is(err, runtimechange.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Runtime change state conflict", "message": err.Error()})
	case errors.Is(err, runtimechange.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Runtime change not found"})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime change control unavailable"})
	}
}

func decodeStrictJSONBody(r *http.Request, target interface{}) error {
	const maxBodyBytes = 2 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxBodyBytes {
		return errors.New("JSON body too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}
