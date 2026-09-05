package admin

import (
	"bytes"
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

func (h *Handler) ListCanonicalDeploymentReleases(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.deploymentLifecycleManager == nil {
		writeCanonicalDeploymentReleaseAuthoringError(w, runtimechange.ErrUnavailable)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	options, err := h.deploymentLifecycleManager.ListDeploymentReleaseAuthoringOptions(r.Context(), orgID)
	if err != nil {
		writeCanonicalDeploymentReleaseAuthoringError(w, err)
		return
	}
	canWrite := auth.RoleIn(actorOrganizationRole(r.Context()), auth.RolePlatformAdmin, auth.RoleOrgAdmin)
	gatewayLicensed := h.licenseRuntime != nil && h.licenseRuntime.Authorize(licensing.FeatureGateway).Allowed
	writeJSON(w, http.StatusOK, canonicalDeploymentReleaseCollectionView{
		ContractVersion: options.ContractVersion,
		GeneratedAt:     options.GeneratedAt,
		Models:          canonicalDeploymentReleaseAuthoringModels(options, canWrite, gatewayLicensed),
		Releases:        options.Releases,
	})
}

func (h *Handler) AuthorCanonicalDeploymentRelease(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		writeCanonicalDeploymentReleaseAuthoringError(w, runtimechange.ErrUnavailable)
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), 0)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	identity := middleware.GetRequestIdentityFromContext(r.Context())
	if !identity.IdempotencyKeyPresent || identity.IdempotencyKeyHash == "" {
		writeCanonicalDeploymentReleaseAuthoringError(w, errCanonicalDeploymentReleaseAuthoringIdempotencyRequired)
		return
	}
	request, err := decodeCanonicalDeploymentReleaseAuthoringRequest(w, r)
	if err != nil {
		writeCanonicalDeploymentReleaseAuthoringError(w, err)
		return
	}
	mutation, err := canonicalDeploymentReleaseAuthoringMutation(request)
	if err != nil {
		writeCanonicalDeploymentReleaseAuthoringError(w, err)
		return
	}
	fingerprint, err := canonicalDeploymentReleaseAuthoringFingerprint(mutation, h.apiKeyHashSecret)
	if err != nil {
		writeCanonicalDeploymentReleaseAuthoringError(w, runtimechange.ErrUnavailable)
		return
	}
	if h.deploymentLifecycleManager == nil {
		writeCanonicalDeploymentReleaseAuthoringError(w, runtimechange.ErrUnavailable)
		return
	}
	meta := requestAuditMeta(r, orgID, 0)
	receipt, err := h.deploymentLifecycleManager.AuthorSealedDeploymentRelease(
		r.Context(),
		runtimechange.DeploymentReleaseAuthoringCommand{
			Mutation: mutation, CustomerOrgID: orgID,
			ActorUserID:        middleware.GetUserIDFromContext(r.Context()),
			ActorOrgID:         middleware.GetOrgIDFromContext(r.Context()),
			IdempotencyKeyHash: identity.IdempotencyKeyHash,
			RequestFingerprint: fingerprint,
			ServerOperationID:  identity.OperationID,
			Audit: runtimechange.DeploymentReleaseAuthoringAudit{
				RequestID: meta.RequestID, ClientRequestID: meta.ClientRequestID,
				Source: meta.Source, IP: meta.IP, UserAgent: meta.UserAgent,
			},
		},
	)
	if err != nil {
		writeCanonicalDeploymentReleaseAuthoringError(w, err)
		return
	}
	expectedLocation := "/api/v1/model-deployment-releases/" + receipt.Release.ReleaseRef
	if receipt.OriginalHTTPStatus != http.StatusCreated || receipt.OriginalLocation != expectedLocation {
		writeCanonicalDeploymentReleaseAuthoringError(w, runtimechange.ErrUnavailable)
		return
	}
	w.Header().Set("Location", receipt.OriginalLocation)
	status := receipt.OriginalHTTPStatus
	if receipt.IdempotencyReplay {
		status = http.StatusOK
		w.Header().Set(middleware.IdempotencyReplayHeader, "true")
		if original := strings.TrimSpace(receipt.ServerOperationID); original != "" {
			w.Header().Set(middleware.OriginalOperationHeader, original)
		}
	}
	writeJSON(w, status, receipt)
}

func decodeCanonicalDeploymentReleaseAuthoringRequest(
	w http.ResponseWriter,
	r *http.Request,
) (canonicalDeploymentReleaseAuthoringRequest, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, canonicalDeploymentLifecycleMaxRequestBytes))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return canonicalDeploymentReleaseAuthoringRequest{}, maxBytesError
		}
		return canonicalDeploymentReleaseAuthoringRequest{}, errCanonicalDeploymentReleaseAuthoringInvalid
	}
	if err := strictjson.Validate(body); err != nil {
		return canonicalDeploymentReleaseAuthoringRequest{}, errCanonicalDeploymentReleaseAuthoringInvalid
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil || members == nil {
		return canonicalDeploymentReleaseAuthoringRequest{}, errCanonicalDeploymentReleaseAuthoringInvalid
	}
	for member := range members {
		switch member {
		case "public_model_id", "baseline", "selections", "reason",
			"profile_definition_digest", "public_context_limit", "public_output_limit", "capability_approval_ids":
		default:
			return canonicalDeploymentReleaseAuthoringRequest{}, errCanonicalDeploymentReleaseAuthoringInvalid
		}
	}
	var request canonicalDeploymentReleaseAuthoringRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return canonicalDeploymentReleaseAuthoringRequest{}, errCanonicalDeploymentReleaseAuthoringInvalid
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return canonicalDeploymentReleaseAuthoringRequest{}, errCanonicalDeploymentReleaseAuthoringInvalid
	}
	return request, nil
}

func writeCanonicalDeploymentReleaseAuthoringError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesError):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error": "Canonical deployment release authoring request is too large",
			"code":  "canonical_deployment_release_authoring_request_too_large",
		})
	case errors.Is(err, errCanonicalDeploymentReleaseAuthoringIdempotencyRequired):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Idempotency-Key is required for canonical deployment release authoring",
			"code":  "canonical_deployment_release_authoring_idempotency_required",
		})
	case errors.Is(err, runtimechange.ErrDeploymentReleaseAuthoringIdempotencyConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "The Idempotency-Key was already used with a different canonical deployment release authoring request",
			"code":  "canonical_deployment_release_authoring_idempotency_conflict",
		})
	case errors.Is(err, errCanonicalDeploymentReleaseAuthoringInvalid), errors.Is(err, runtimechange.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid canonical deployment release authoring request",
			"code":  "canonical_deployment_release_authoring_invalid",
		})
	case errors.Is(err, runtimechange.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": "Canonical deployment release authoring target not found",
			"code":  "canonical_deployment_release_authoring_not_found",
		})
	case errors.Is(err, runtimechange.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Canonical deployment release authoring conflicts with current state",
			"code":  "canonical_deployment_release_authoring_conflict",
		})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": "Canonical deployment release authoring is unavailable",
			"code":  "canonical_deployment_release_authoring_unavailable",
		})
	}
}
