package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelrouteauthority"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const modelRouteSafetyMaxRequestBytes = 2 << 10

type pauseProviderConnectionRequest struct {
	ExpectedConfigRevision int64 `json:"expected_config_revision"`
}

type revokeProviderCredentialRequest struct {
	ExpectedStateRevision int64 `json:"expected_state_revision"`
}

func (h *Handler) PauseProviderConnection(w http.ResponseWriter, r *http.Request) {
	connectionID, err := positivePathID(r.PathValue("connectionID"))
	if err != nil {
		writeModelRouteSafetyError(w, modelrouteauthority.ErrSafetyInvalid)
		return
	}
	var request pauseProviderConnectionRequest
	if err := decodeModelRouteSafetyRequest(w, r, &request); err != nil || request.ExpectedConfigRevision <= 0 {
		writeModelRouteSafetyError(w, modelrouteauthority.ErrSafetyInvalid)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorUserID := middleware.GetUserIDFromContext(r.Context())
	if actorUserID <= 0 || h.modelRouteSafetyController == nil {
		writeModelRouteSafetyError(w, modelrouteauthority.ErrSafetyUnavailable)
		return
	}
	meta := requestAuditMeta(r, orgID, 0)
	result, err := h.modelRouteSafetyController.PauseConnection(
		r.Context(), modelrouteauthority.PauseConnectionRequest{
			Mode: h.modelRouteAuthorityMode, CustomerOrgID: orgID,
			ActorUserID:  actorUserID,
			ConnectionID: connectionID, ExpectedConfigRevision: request.ExpectedConfigRevision,
		}, modelRouteSafetyAuditAppender(meta),
	)
	if err != nil {
		writeModelRouteSafetyError(w, err)
		return
	}
	if h.routingController != nil {
		h.routingController.Invalidate()
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) RevokeProviderCredentialBinding(w http.ResponseWriter, r *http.Request) {
	bindingID, err := positivePathID(r.PathValue("bindingID"))
	if err != nil {
		writeModelRouteSafetyError(w, modelrouteauthority.ErrSafetyInvalid)
		return
	}
	var request revokeProviderCredentialRequest
	if err := decodeModelRouteSafetyRequest(w, r, &request); err != nil || request.ExpectedStateRevision <= 0 {
		writeModelRouteSafetyError(w, modelrouteauthority.ErrSafetyInvalid)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorUserID := middleware.GetUserIDFromContext(r.Context())
	if actorUserID <= 0 || h.modelRouteSafetyController == nil {
		writeModelRouteSafetyError(w, modelrouteauthority.ErrSafetyUnavailable)
		return
	}
	meta := requestAuditMeta(r, orgID, 0)
	result, err := h.modelRouteSafetyController.RevokeCredential(
		r.Context(), modelrouteauthority.RevokeCredentialRequest{
			Mode: h.modelRouteAuthorityMode, CustomerOrgID: orgID,
			ActorUserID:         actorUserID,
			CredentialBindingID: bindingID, ExpectedStateRevision: request.ExpectedStateRevision,
		}, modelRouteSafetyAuditAppender(meta),
	)
	if err != nil {
		writeModelRouteSafetyError(w, err)
		return
	}
	if h.routingController != nil {
		h.routingController.Invalidate()
	}
	writeJSON(w, http.StatusOK, result)
}

func positivePathID(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, modelrouteauthority.ErrSafetyInvalid
	}
	return value, nil
}

func decodeModelRouteSafetyRequest(w http.ResponseWriter, r *http.Request, target any) error {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, modelRouteSafetyMaxRequestBytes))
	if err != nil || strictjson.Validate(body) != nil {
		return modelrouteauthority.ErrSafetyInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return modelrouteauthority.ErrSafetyInvalid
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return modelrouteauthority.ErrSafetyInvalid
	}
	return nil
}

func modelRouteSafetyAuditAppender(meta auditMeta) modelrouteauthority.SafetyAuditAppender {
	return func(ctx context.Context, tx pgx.Tx, record modelrouteauthority.SafetyAuditRecord) error {
		return insertAuditEntry(ctx, tx, record.Action, record.ResourceType, record.ResourceID, record.Detail, meta)
	}
}

func writeModelRouteSafetyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, modelrouteauthority.ErrSafetyInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "Invalid model route safety request", "code": "model_route_safety_invalid",
		})
	case errors.Is(err, modelrouteauthority.ErrSafetyNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "Model route safety target not found", "code": "model_route_safety_not_found",
		})
	case errors.Is(err, modelrouteauthority.ErrSafetyConflict):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "Model route safety authority changed; refresh exact revisions", "code": "model_route_safety_conflict",
		})
	case errors.Is(err, modelrouteauthority.ErrSafetyUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Model route safety action unavailable", "code": "model_route_safety_unavailable",
		})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "Model route safety action failed", "code": "model_route_safety_internal_error",
		})
	}
}
