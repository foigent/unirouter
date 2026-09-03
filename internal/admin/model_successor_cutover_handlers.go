package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/strictjson"
)

// ActivateModelSuccessor performs the only supported first-cutover command:
// validated successor release activation and stable request identity switch in
// one database transaction.
func (h *Handler) ActivateModelSuccessor(w http.ResponseWriter, r *http.Request) {
	h.activateModelSuccessor(w, r, modelSuccessorCutoverMutation)
}

// ActivateStagingModelSuccessor atomically replaces a never-activated staging
// canonical candidate with its first immutable successor. It uses a distinct
// receipt and idempotency namespace from the published-predecessor command.
func (h *Handler) ActivateStagingModelSuccessor(w http.ResponseWriter, r *http.Request) {
	h.activateModelSuccessor(w, r, stagingModelSuccessorCutoverMutation)
}

func (h *Handler) activateModelSuccessor(
	w http.ResponseWriter,
	r *http.Request,
	buildMutation func(string, modelSuccessorCutoverRequest) (runtimechange.ModelSuccessorCutoverMutation, error),
) {
	identity := middleware.GetRequestIdentityFromContext(r.Context())
	if !identity.IdempotencyKeyPresent || identity.IdempotencyKeyHash == "" {
		writeCanonicalDeploymentLifecycleError(w, errCanonicalDeploymentLifecycleIdempotencyRequired)
		return
	}
	if h == nil || h.deploymentLifecycleManager == nil {
		writeCanonicalDeploymentLifecycleError(w, runtimechange.ErrUnavailable)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	request, err := decodeModelSuccessorCutoverRequest(w, r)
	if err != nil {
		writeModelSuccessorCutoverError(w, err)
		return
	}
	mutation, err := buildMutation(r.PathValue("releaseRef"), request)
	if err != nil {
		writeModelSuccessorCutoverError(w, err)
		return
	}
	fingerprint, err := modelSuccessorCutoverFingerprint(mutation, h.apiKeyHashSecret)
	if err != nil {
		writeModelSuccessorCutoverError(w, runtimechange.ErrUnavailable)
		return
	}
	meta := requestAuditMeta(r, orgID, 0)
	receipt, err := h.deploymentLifecycleManager.ApplyModelSuccessorCutover(r.Context(), runtimechange.ModelSuccessorCutoverCommand{
		Mutation: mutation, CustomerOrgID: orgID,
		ActorUserID:        middleware.GetUserIDFromContext(r.Context()),
		ActorOrgID:         middleware.GetOrgIDFromContext(r.Context()),
		IdempotencyKeyHash: identity.IdempotencyKeyHash,
		RequestFingerprint: fingerprint, ServerOperationID: identity.OperationID,
		Audit: runtimechange.DeploymentLifecycleAudit{
			RequestID: meta.RequestID, ClientRequestID: meta.ClientRequestID,
			Source: meta.Source, IP: meta.IP, UserAgent: meta.UserAgent,
		},
	})
	if err != nil {
		writeModelSuccessorCutoverError(w, err)
		return
	}
	if receipt.Lifecycle.OriginalHTTPStatus != http.StatusOK {
		writeModelSuccessorCutoverError(w, runtimechange.ErrUnavailable)
		return
	}
	if receipt.Lifecycle.IdempotencyReplay {
		w.Header().Set(middleware.IdempotencyReplayHeader, "true")
		if original := strings.TrimSpace(receipt.Lifecycle.ServerOperationID); original != "" {
			w.Header().Set(middleware.OriginalOperationHeader, original)
		}
	} else {
		h.invalidateRoutingSnapshots()
	}
	writeJSON(w, http.StatusOK, receipt)
}

func decodeModelSuccessorCutoverRequest(w http.ResponseWriter, r *http.Request) (modelSuccessorCutoverRequest, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, modelSuccessorCutoverMaxRequestBytes))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return modelSuccessorCutoverRequest{}, maxBytesError
		}
		return modelSuccessorCutoverRequest{}, errModelSuccessorCutoverInvalid
	}
	if strictjson.Validate(body) != nil {
		return modelSuccessorCutoverRequest{}, errModelSuccessorCutoverInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request modelSuccessorCutoverRequest
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
		return modelSuccessorCutoverRequest{}, errModelSuccessorCutoverInvalid
	}
	return request, nil
}

func writeModelSuccessorCutoverError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesError):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error": "Model successor cutover request is too large", "code": "model_successor_cutover_request_too_large",
		})
	case errors.Is(err, errModelSuccessorCutoverInvalid), errors.Is(err, runtimechange.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid model successor cutover request", "code": "model_successor_cutover_invalid",
		})
	case errors.Is(err, runtimechange.ErrDeploymentLifecycleIdempotencyConflict), errors.Is(err, runtimechange.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Model successor cutover conflicts with current state", "code": "model_successor_cutover_conflict",
		})
	case errors.Is(err, runtimechange.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": "Model successor cutover resource not found", "code": "model_successor_cutover_not_found",
		})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "Model successor cutover is unavailable", "code": "model_successor_cutover_unavailable",
		})
	}
}
