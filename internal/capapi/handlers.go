package capapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/errdocs"
	"github.com/uniroute/unirouter/internal/middleware"
)

const maxRequestBytes = 64 << 10

type Handler struct {
	service *agentcap.Service
}

func NewHandler(service *agentcap.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Exchange(w http.ResponseWriter, r *http.Request) {
	if middleware.GetAuthKindFromContext(r.Context()) != middleware.AuthKindAPIKey {
		writeError(w, http.StatusForbidden, "agent_capability.root_key_required", "A root API key is required to register a workflow")
		return
	}
	var request agentcap.ExchangeRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "agent_capability.invalid_request", "Invalid capability exchange request")
		return
	}
	response, err := h.service.Exchange(r.Context(), middleware.GetAPIKeyFromContext(r.Context()), request)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeTokenJSON(w, http.StatusCreated, response)
}

func (h *Handler) Delegate(w http.ResponseWriter, r *http.Request) {
	if middleware.GetAuthKindFromContext(r.Context()) != middleware.AuthKindAgentCapability {
		writeError(w, http.StatusForbidden, "agent_capability.parent_required", "An authenticated parent Agent capability is required")
		return
	}
	var request agentcap.DelegateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "agent_capability.invalid_request", "Invalid child Agent registration request")
		return
	}
	response, err := h.service.Delegate(r.Context(), middleware.GetAgentCapabilityFromContext(r.Context()), request)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeTokenJSON(w, http.StatusCreated, response)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	if middleware.GetAuthKindFromContext(r.Context()) != middleware.AuthKindAgentCapability {
		writeError(w, http.StatusForbidden, "agent_capability.parent_required", "An authenticated Agent capability is required")
		return
	}
	if err := requireEmptyBody(w, r); err != nil {
		writeError(w, http.StatusBadRequest, "agent_capability.invalid_request", "Refresh does not accept a request body")
		return
	}
	response, err := h.service.Refresh(r.Context(), middleware.GetAgentCapabilityFromContext(r.Context()))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeTokenJSON(w, http.StatusOK, response)
}

func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	if middleware.GetAuthKindFromContext(r.Context()) != middleware.AuthKindAPIKey {
		writeError(w, http.StatusForbidden, "agent_capability.root_key_required", "The workflow root API key is required to revoke a workflow")
		return
	}
	var request agentcap.RevokeRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "agent_capability.invalid_request", "Invalid capability revocation request")
		return
	}
	response, err := h.service.Revoke(r.Context(), middleware.GetAPIKeyFromContext(r.Context()), request.WorkflowID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func requireEmptyBody(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	var oneByte [1]byte
	read, err := r.Body.Read(oneByte[:])
	if read > 0 {
		return errors.New("request body must be empty")
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain exactly one JSON object")
	}
	return nil
}

func writeTokenJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, status, value)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentcap.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "agent_capability.state_unavailable", "Agent capability state is temporarily unavailable")
	case errors.Is(err, agentcap.ErrRefreshTooEarly):
		writeError(w, http.StatusConflict, "agent_capability.refresh_too_early", "Refresh after the token's advised refresh time; clients should singleflight concurrent refreshes")
	case errors.Is(err, agentcap.ErrConflict):
		writeError(w, http.StatusConflict, "agent_capability.conflict", "The workflow or child Agent is already registered")
	case errors.Is(err, agentcap.ErrLimitExceeded):
		writeError(w, http.StatusForbidden, "agent_capability.limit_exceeded", "Requested child authority exceeds a root or ancestor limit")
	case errors.Is(err, agentcap.ErrManagementForbidden):
		writeError(w, http.StatusForbidden, "agent_capability.management_forbidden", "This identity cannot manage Agent capabilities")
	case errors.Is(err, agentcap.ErrRevoked), errors.Is(err, agentcap.ErrSuperseded), errors.Is(err, agentcap.ErrAuthorityChanged), errors.Is(err, agentcap.ErrInactiveRoot), errors.Is(err, agentcap.ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, "agent_capability.revoked", "The capability is expired, superseded, revoked, or its root authority changed")
	default:
		writeError(w, http.StatusBadRequest, "agent_capability.invalid_request", "The Agent capability request is invalid")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"type":    "agent_capability_error",
			"code":    code,
			"message": message,
			"doc_url": errdocs.URLForCode(code),
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
