package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/strictjson"
)

type deploymentLifecycleApplication interface {
	InspectDeploymentLifecycle(context.Context, string, int64) (runtimechange.DeploymentLifecycleState, error)
	ListDeploymentReleaseAuthoringOptions(context.Context, int64) (runtimechange.DeploymentReleaseAuthoringOptions, error)
	AuthorSealedDeploymentRelease(context.Context, runtimechange.DeploymentReleaseAuthoringCommand) (runtimechange.DeploymentReleaseAuthoringReceipt, error)
	ApplyDeploymentLifecycle(context.Context, runtimechange.DeploymentLifecycleCommand) (runtimechange.DeploymentLifecycleReceipt, error)
	ApplyModelSuccessorCutover(context.Context, runtimechange.ModelSuccessorCutoverCommand) (runtimechange.ModelSuccessorCutoverReceipt, error)
}

func (h *Handler) GetCanonicalDeploymentLifecycle(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.deploymentLifecycleManager == nil {
		writeCanonicalDeploymentLifecycleError(w, runtimechange.ErrUnavailable)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	state, err := h.deploymentLifecycleManager.InspectDeploymentLifecycle(
		r.Context(), r.PathValue("releaseRef"), orgID,
	)
	if err != nil {
		writeCanonicalDeploymentLifecycleError(w, err)
		return
	}
	canWrite := auth.RoleIn(actorOrganizationRole(r.Context()), auth.RolePlatformAdmin, auth.RoleOrgAdmin)
	gatewayLicensed := h.licenseRuntime != nil && h.licenseRuntime.Authorize(licensing.FeatureGateway).Allowed
	writeJSON(w, http.StatusOK, canonicalDeploymentLifecycleView{
		ContractVersion: runtimechange.DeploymentLifecycleReceiptContractVersion,
		Release:         state.Release,
		Slot:            state.Slot,
		StableParent:    state.StableParent,
		Actions: canonicalDeploymentLifecycleActions(
			state.Release, state.Slot, state.StableParent, canWrite, gatewayLicensed,
		),
	})
}

func (h *Handler) ActivateInitialCanonicalDeploymentLifecycle(w http.ResponseWriter, r *http.Request) {
	h.applyCanonicalDeploymentLifecycle(w, r, runtimechange.DeploymentLifecycleActivateInitial)
}

func (h *Handler) StartCanonicalDeploymentCanary(w http.ResponseWriter, r *http.Request) {
	h.applyCanonicalDeploymentLifecycle(w, r, runtimechange.DeploymentLifecycleStartCanary)
}

func (h *Handler) PromoteCanonicalDeploymentCanary(w http.ResponseWriter, r *http.Request) {
	h.applyCanonicalDeploymentLifecycle(w, r, runtimechange.DeploymentLifecyclePromote)
}

func (h *Handler) CancelCanonicalDeploymentCanary(w http.ResponseWriter, r *http.Request) {
	h.applyCanonicalDeploymentLifecycle(w, r, runtimechange.DeploymentLifecycleCancelCanary)
}

func (h *Handler) RollbackCanonicalDeploymentStable(w http.ResponseWriter, r *http.Request) {
	h.applyCanonicalDeploymentLifecycle(w, r, runtimechange.DeploymentLifecycleRollbackStable)
}

func (h *Handler) CutoverLegacyCanonicalDeploymentLifecycle(w http.ResponseWriter, r *http.Request) {
	h.applyCanonicalDeploymentLifecycle(w, r, runtimechange.DeploymentLifecycleCutoverLegacy)
}

func (h *Handler) RollbackLegacyCanonicalDeploymentLifecycle(w http.ResponseWriter, r *http.Request) {
	h.applyCanonicalDeploymentLifecycle(w, r, runtimechange.DeploymentLifecycleRollbackLegacy)
}

func (h *Handler) applyCanonicalDeploymentLifecycle(
	w http.ResponseWriter,
	r *http.Request,
	action runtimechange.DeploymentLifecycleAction,
) {
	if h == nil {
		writeCanonicalDeploymentLifecycleError(w, runtimechange.ErrUnavailable)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	identity := middleware.GetRequestIdentityFromContext(r.Context())
	if !identity.IdempotencyKeyPresent || identity.IdempotencyKeyHash == "" {
		writeCanonicalDeploymentLifecycleError(w, errCanonicalDeploymentLifecycleIdempotencyRequired)
		return
	}
	request, err := decodeCanonicalDeploymentLifecycleRequest(w, r, action)
	if err != nil {
		writeCanonicalDeploymentLifecycleError(w, err)
		return
	}
	mutation, err := canonicalDeploymentLifecycleMutation(action, r.PathValue("releaseRef"), request)
	if err != nil {
		writeCanonicalDeploymentLifecycleError(w, err)
		return
	}
	fingerprint, err := canonicalDeploymentLifecycleFingerprint(mutation, h.apiKeyHashSecret)
	if err != nil {
		writeCanonicalDeploymentLifecycleError(w, runtimechange.ErrUnavailable)
		return
	}
	if h.deploymentLifecycleManager == nil {
		writeCanonicalDeploymentLifecycleError(w, runtimechange.ErrUnavailable)
		return
	}
	meta := requestAuditMeta(r, orgID, 0)
	receipt, err := h.deploymentLifecycleManager.ApplyDeploymentLifecycle(r.Context(), runtimechange.DeploymentLifecycleCommand{
		Mutation:           mutation,
		CustomerOrgID:      orgID,
		ActorUserID:        middleware.GetUserIDFromContext(r.Context()),
		ActorOrgID:         middleware.GetOrgIDFromContext(r.Context()),
		IdempotencyKeyHash: identity.IdempotencyKeyHash,
		RequestFingerprint: fingerprint,
		ServerOperationID:  identity.OperationID,
		Audit: runtimechange.DeploymentLifecycleAudit{
			RequestID: meta.RequestID, ClientRequestID: meta.ClientRequestID,
			Source: meta.Source, IP: meta.IP, UserAgent: meta.UserAgent,
		},
	})
	if err != nil {
		writeCanonicalDeploymentLifecycleError(w, err)
		return
	}
	if receipt.OriginalHTTPStatus != http.StatusOK {
		writeCanonicalDeploymentLifecycleError(w, runtimechange.ErrUnavailable)
		return
	}
	if receipt.IdempotencyReplay {
		w.Header().Set(middleware.IdempotencyReplayHeader, "true")
		if original := strings.TrimSpace(receipt.ServerOperationID); original != "" {
			w.Header().Set(middleware.OriginalOperationHeader, original)
		}
	} else {
		h.invalidateRoutingSnapshots()
	}
	writeJSON(w, receipt.OriginalHTTPStatus, receipt)
}

func decodeCanonicalDeploymentLifecycleRequest(
	w http.ResponseWriter,
	r *http.Request,
	action runtimechange.DeploymentLifecycleAction,
) (canonicalDeploymentLifecycleRequest, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, canonicalDeploymentLifecycleMaxRequestBytes))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return canonicalDeploymentLifecycleRequest{}, maxBytesError
		}
		return canonicalDeploymentLifecycleRequest{}, errCanonicalDeploymentLifecycleInvalid
	}
	if err := strictjson.Validate(body); err != nil {
		return canonicalDeploymentLifecycleRequest{}, errCanonicalDeploymentLifecycleInvalid
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil || members == nil {
		return canonicalDeploymentLifecycleRequest{}, errCanonicalDeploymentLifecycleInvalid
	}
	for member := range members {
		if !canonicalDeploymentLifecycleRequestFieldAllowed(action, member) {
			return canonicalDeploymentLifecycleRequest{}, errCanonicalDeploymentLifecycleInvalid
		}
	}
	var request canonicalDeploymentLifecycleRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return canonicalDeploymentLifecycleRequest{}, errCanonicalDeploymentLifecycleInvalid
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return canonicalDeploymentLifecycleRequest{}, errCanonicalDeploymentLifecycleInvalid
	}
	return request, nil
}

func canonicalDeploymentLifecycleRequestFieldAllowed(
	action runtimechange.DeploymentLifecycleAction,
	field string,
) bool {
	switch field {
	case "expected_release_version", "reason":
		return action == runtimechange.DeploymentLifecycleActivateInitial ||
			action == runtimechange.DeploymentLifecycleStartCanary ||
			action == runtimechange.DeploymentLifecyclePromote ||
			action == runtimechange.DeploymentLifecycleCancelCanary ||
			action == runtimechange.DeploymentLifecycleRollbackStable ||
			action == runtimechange.DeploymentLifecycleCutoverLegacy ||
			action == runtimechange.DeploymentLifecycleRollbackLegacy
	case "expected_slot_version":
		return action == runtimechange.DeploymentLifecycleStartCanary ||
			action == runtimechange.DeploymentLifecyclePromote ||
			action == runtimechange.DeploymentLifecycleCancelCanary ||
			action == runtimechange.DeploymentLifecycleRollbackStable ||
			action == runtimechange.DeploymentLifecycleCutoverLegacy ||
			action == runtimechange.DeploymentLifecycleRollbackLegacy
	case "canary_basis_points":
		return action == runtimechange.DeploymentLifecycleStartCanary
	default:
		return false
	}
}

func writeCanonicalDeploymentLifecycleError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesError):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error": "Canonical deployment lifecycle request is too large",
			"code":  "canonical_deployment_lifecycle_request_too_large",
		})
	case errors.Is(err, errCanonicalDeploymentLifecycleIdempotencyRequired):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Idempotency-Key is required for canonical deployment lifecycle mutations",
			"code":  "canonical_deployment_lifecycle_idempotency_required",
		})
	case errors.Is(err, runtimechange.ErrDeploymentLifecycleIdempotencyConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "The Idempotency-Key was already used with a different canonical deployment lifecycle request",
			"code":  "canonical_deployment_lifecycle_idempotency_conflict",
		})
	case errors.Is(err, errCanonicalDeploymentLifecycleInvalid), errors.Is(err, runtimechange.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid canonical deployment lifecycle request",
			"code":  "canonical_deployment_lifecycle_invalid",
		})
	case errors.Is(err, runtimechange.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": "Canonical deployment release not found",
			"code":  "canonical_deployment_lifecycle_not_found",
		})
	case errors.Is(err, runtimechange.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Canonical deployment lifecycle conflicts with current state",
			"code":  "canonical_deployment_lifecycle_conflict",
		})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "Canonical deployment lifecycle is unavailable",
			"code":  "canonical_deployment_lifecycle_unavailable",
		})
	}
}
