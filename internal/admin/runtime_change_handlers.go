package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

func (h *Handler) ListRuntimeChanges(w http.ResponseWriter, r *http.Request) {
	if h.runtimeChangeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime change control unavailable"})
		return
	}
	items, err := h.runtimeChangeManager.List(r.Context(), r.URL.Query().Get("model"), 100)
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": len(items)})
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

func (h *Handler) CreateRuntimeChange(w http.ResponseWriter, r *http.Request) {
	if h.runtimeChangeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime change control unavailable"})
		return
	}
	var request runtimechange.DraftRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid runtime change request"})
		return
	}
	item, err := h.runtimeChangeManager.CreateDraft(r.Context(), request, middleware.GetUserIDFromContext(r.Context()))
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	h.recordAuditEntry(r.Context(), "runtime_change.create", "runtime_change", item.ReleaseRef,
		map[string]interface{}{"stage": item.Stage, "candidate_hash": item.CandidateHash, "runtime_mutation": "none"},
		requestAuditMeta(r, 0, 0))
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) ValidateRuntimeChange(w http.ResponseWriter, r *http.Request) {
	h.transitionRuntimeChange(w, r, "validate")
}

func (h *Handler) AdvanceRuntimeChangeShadow(w http.ResponseWriter, r *http.Request) {
	h.transitionRuntimeChange(w, r, "shadow")
}

func (h *Handler) ActivateInitialRuntimeChange(w http.ResponseWriter, r *http.Request) {
	h.transitionRuntimeChange(w, r, "activate_initial")
}

func (h *Handler) StartRuntimeChangeCanary(w http.ResponseWriter, r *http.Request) {
	h.transitionRuntimeChange(w, r, "canary")
}

func (h *Handler) PromoteRuntimeChange(w http.ResponseWriter, r *http.Request) {
	h.transitionRuntimeChange(w, r, "promote")
}

func (h *Handler) RollbackRuntimeChange(w http.ResponseWriter, r *http.Request) {
	h.transitionRuntimeChange(w, r, "rollback")
}

func (h *Handler) transitionRuntimeChange(w http.ResponseWriter, r *http.Request, action string) {
	if h.runtimeChangeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Runtime change control unavailable"})
		return
	}
	var request runtimechange.TransitionRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid transition request"})
		return
	}
	actor := middleware.GetUserIDFromContext(r.Context())
	ref := r.PathValue("ref")
	var (
		item runtimechange.Release
		err  error
	)
	switch action {
	case "validate":
		item, err = h.runtimeChangeManager.Validate(r.Context(), ref, request.ExpectedVersion, actor)
	case "shadow":
		item, err = h.runtimeChangeManager.AdvanceShadow(r.Context(), ref, request.ExpectedVersion, actor)
	case "activate_initial":
		var customerOrgID int64
		customerOrgID, err = resolveActorOrganization(r.Context(), 0)
		if err != nil {
			writeOrganizationScopeError(w, err)
			return
		}
		item, err = h.runtimeChangeManager.ActivateInitial(r.Context(), ref, request.ExpectedVersion, actor, customerOrgID)
	case "canary":
		item, err = h.runtimeChangeManager.StartCanary(r.Context(), ref, request.ExpectedVersion, request.CanaryBasisPoints, actor)
	case "promote":
		item, err = h.runtimeChangeManager.Promote(r.Context(), ref, request.ExpectedVersion, actor)
	case "rollback":
		item, err = h.runtimeChangeManager.Rollback(r.Context(), ref, request.ExpectedVersion, actor)
	default:
		err = runtimechange.ErrInvalid
	}
	if err != nil {
		writeRuntimeChangeError(w, err)
		return
	}
	h.invalidateRoutingSnapshots()
	h.recordAuditEntry(r.Context(), "runtime_change."+action, "runtime_change", item.ReleaseRef,
		map[string]interface{}{"stage": item.Stage, "version": item.Version, "runtime_mutation": runtimeChangeMutation(item.Stage)},
		requestAuditMeta(r, 0, 0))
	writeJSON(w, http.StatusOK, item)
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

func runtimeChangeMutation(stage string) string {
	if stage == "canary" || stage == "active" || stage == "rolled_back" {
		return "bounded_route_assignment"
	}
	return "none"
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
