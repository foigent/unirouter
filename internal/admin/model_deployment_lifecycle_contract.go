package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

const canonicalDeploymentLifecycleMaxRequestBytes = 16 << 10

var (
	errCanonicalDeploymentLifecycleInvalid             = errors.New("canonical deployment lifecycle request is invalid")
	errCanonicalDeploymentLifecycleIdempotencyRequired = errors.New("canonical deployment lifecycle idempotency key is required")
)

type canonicalDeploymentLifecycleRequest struct {
	ExpectedReleaseVersion int64  `json:"expected_release_version"`
	ExpectedSlotVersion    int64  `json:"expected_slot_version,omitempty"`
	CanaryBasisPoints      int    `json:"canary_basis_points,omitempty"`
	Reason                 string `json:"reason,omitempty"`
}

type canonicalDeploymentLifecycleView struct {
	ContractVersion string                                         `json:"contract_version"`
	Release         runtimechange.DeploymentRelease                `json:"release"`
	Slot            runtimechange.DeploymentLifecycleSlotState     `json:"slot"`
	StableParent    *runtimechange.DeploymentReleaseSummary        `json:"stable_parent,omitempty"`
	Actions         []canonicalDeploymentLifecycleActionDescriptor `json:"actions"`
}

// canonicalDeploymentLifecycleActionDescriptor is server-owned UI truth. Its
// Available flag means the authenticated caller may request the transactional
// preflight now; every mutation still revalidates dynamic state under locks.
type canonicalDeploymentLifecycleActionDescriptor struct {
	Action                runtimechange.DeploymentLifecycleAction `json:"action"`
	Method                string                                  `json:"method"`
	Path                  string                                  `json:"path"`
	Available             bool                                    `json:"available"`
	ReasonCode            string                                  `json:"reason_code"`
	StructurallyAvailable bool                                    `json:"structurally_available"`
	Authorized            bool                                    `json:"authorized"`
	Licensed              bool                                    `json:"licensed"`
	LicenseFeature        string                                  `json:"license_feature,omitempty"`
	IdempotencyRequired   bool                                    `json:"idempotency_required"`
	RequiredFields        []string                                `json:"required_fields"`
	SafetyDirection       string                                  `json:"safety_direction"`
}

func canonicalDeploymentLifecycleMutation(
	action runtimechange.DeploymentLifecycleAction,
	releaseRef string,
	request canonicalDeploymentLifecycleRequest,
) (runtimechange.DeploymentLifecycleMutation, error) {
	mutation, err := runtimechange.NormalizeDeploymentLifecycleMutation(runtimechange.DeploymentLifecycleMutation{
		Action: action, ReleaseRef: releaseRef,
		ExpectedReleaseVersion: request.ExpectedReleaseVersion,
		ExpectedSlotVersion:    request.ExpectedSlotVersion,
		CanaryBasisPoints:      request.CanaryBasisPoints,
		Reason:                 request.Reason,
	})
	if err != nil {
		return runtimechange.DeploymentLifecycleMutation{}, fmt.Errorf("%w: %v", errCanonicalDeploymentLifecycleInvalid, err)
	}
	return mutation, nil
}

func canonicalDeploymentLifecycleFingerprint(
	mutation runtimechange.DeploymentLifecycleMutation,
	secret string,
) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("canonical deployment lifecycle fingerprint secret is unavailable")
	}
	normalized, err := runtimechange.NormalizeDeploymentLifecycleMutation(mutation)
	if err != nil {
		return "", errCanonicalDeploymentLifecycleInvalid
	}
	wire, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal canonical deployment lifecycle fingerprint: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("unirouter.model-deployment-lifecycle.request.v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(wire)
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil)), nil
}

func canonicalDeploymentLifecycleActions(
	release runtimechange.DeploymentRelease,
	slot runtimechange.DeploymentLifecycleSlotState,
	stableParent *runtimechange.DeploymentReleaseSummary,
	canWrite bool,
	gatewayLicensed bool,
) []canonicalDeploymentLifecycleActionDescriptor {
	actions := []runtimechange.DeploymentLifecycleAction{
		runtimechange.DeploymentLifecycleActivateInitial, runtimechange.DeploymentLifecycleStartCanary,
		runtimechange.DeploymentLifecyclePromote, runtimechange.DeploymentLifecycleCancelCanary,
		runtimechange.DeploymentLifecycleRollbackStable,
	}
	if stableParent != nil && stableParent.CandidateVersion == runtimechange.CandidateVersion {
		actions = []runtimechange.DeploymentLifecycleAction{
			runtimechange.DeploymentLifecycleCutoverLegacy,
			runtimechange.DeploymentLifecycleRollbackLegacy,
		}
	}
	descriptors := make([]canonicalDeploymentLifecycleActionDescriptor, 0, len(actions))
	for _, action := range actions {
		structurallyAvailable, structuralReason := canonicalDeploymentLifecycleStructuralAvailability(
			action, release, slot, stableParent,
		)
		requiresLicense := action != runtimechange.DeploymentLifecycleCancelCanary &&
			action != runtimechange.DeploymentLifecycleRollbackLegacy
		licensed := !requiresLicense || gatewayLicensed
		available := structurallyAvailable && canWrite && licensed
		reasonCode := "ready_for_transactional_preflight"
		switch {
		case !structurallyAvailable:
			reasonCode = structuralReason
		case !canWrite:
			reasonCode = "organization_write_role_required"
		case !licensed:
			reasonCode = "gateway_license_required"
		}
		requiredFields := []string{"expected_release_version", "expected_slot_version"}
		if action == runtimechange.DeploymentLifecycleActivateInitial {
			requiredFields = []string{"expected_release_version"}
		}
		if action == runtimechange.DeploymentLifecycleStartCanary {
			requiredFields = append(requiredFields, "canary_basis_points")
		}
		if action == runtimechange.DeploymentLifecycleCutoverLegacy ||
			action == runtimechange.DeploymentLifecycleRollbackLegacy {
			requiredFields = append(requiredFields, "reason")
		}
		descriptor := canonicalDeploymentLifecycleActionDescriptor{
			Action: action, Method: "POST",
			Path:      "/api/v1/model-deployment-releases/{releaseRef}/" + canonicalDeploymentLifecycleActionPath(action),
			Available: available, ReasonCode: reasonCode,
			StructurallyAvailable: structurallyAvailable, Authorized: canWrite, Licensed: licensed,
			IdempotencyRequired: true, RequiredFields: requiredFields, SafetyDirection: "expansion",
		}
		if requiresLicense {
			descriptor.LicenseFeature = licensing.FeatureGateway
		}
		if action == runtimechange.DeploymentLifecycleCancelCanary ||
			action == runtimechange.DeploymentLifecycleRollbackLegacy {
			descriptor.SafetyDirection = "contraction"
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}

func canonicalDeploymentLifecycleActionPath(action runtimechange.DeploymentLifecycleAction) string {
	switch action {
	case runtimechange.DeploymentLifecycleActivateInitial:
		return "activate-initial"
	case runtimechange.DeploymentLifecycleStartCanary:
		return "start-canary"
	case runtimechange.DeploymentLifecyclePromote:
		return "promote"
	case runtimechange.DeploymentLifecycleCancelCanary:
		return "cancel-canary"
	case runtimechange.DeploymentLifecycleRollbackStable:
		return "rollback-stable"
	case runtimechange.DeploymentLifecycleCutoverLegacy:
		return "cutover-legacy"
	case runtimechange.DeploymentLifecycleRollbackLegacy:
		return "rollback-legacy"
	default:
		return "unsupported"
	}
}

func canonicalDeploymentLifecycleStructuralAvailability(
	action runtimechange.DeploymentLifecycleAction,
	release runtimechange.DeploymentRelease,
	slot runtimechange.DeploymentLifecycleSlotState,
	stableParent *runtimechange.DeploymentReleaseSummary,
) (bool, string) {
	if release.ID <= 0 || release.PublicModelID <= 0 || release.ModelName == "" ||
		release.CandidateVersion != runtimechange.DeploymentCandidateVersion || release.RouteItemsSealedAt == nil {
		return false, "release_not_sealed_canonical_v2"
	}
	if slot.Exists && (slot.PublicModelID != release.PublicModelID || slot.ModelName != release.ModelName) {
		return false, "runtime_slot_identity_mismatch"
	}
	switch action {
	case runtimechange.DeploymentLifecycleActivateInitial:
		if release.Stage == "validated" && release.ParentStableReleaseID == nil && !slot.Exists {
			return true, ""
		}
		return false, "initial_activation_requires_parentless_validated_release_and_absent_slot"
	case runtimechange.DeploymentLifecycleStartCanary:
		if release.Stage == "validated" && release.ParentStableReleaseID != nil && slot.Exists &&
			sameOptionalID(slot.StableReleaseID, release.ParentStableReleaseID) && slot.CanaryReleaseID == nil &&
			slot.CanaryBasisPoints == 0 {
			return true, ""
		}
		return false, "start_canary_requires_validated_child_and_stable_parent_slot"
	case runtimechange.DeploymentLifecyclePromote, runtimechange.DeploymentLifecycleCancelCanary:
		if release.Stage == "canary" && release.ParentStableReleaseID != nil && slot.Exists &&
			sameOptionalID(slot.StableReleaseID, release.ParentStableReleaseID) &&
			optionalIDEquals(slot.CanaryReleaseID, release.ID) &&
			slot.CanaryBasisPoints == release.CanaryBasisPoints && release.CanaryBasisPoints > 0 {
			return true, ""
		}
		return false, "action_requires_exact_active_canary_slot"
	case runtimechange.DeploymentLifecycleRollbackStable:
		if release.Stage == "active" && release.ParentStableReleaseID != nil && slot.Exists &&
			optionalIDEquals(slot.StableReleaseID, release.ID) && slot.CanaryReleaseID == nil &&
			slot.CanaryBasisPoints == 0 {
			return true, ""
		}
		return false, "rollback_requires_exact_stable_slot_and_retained_parent"
	case runtimechange.DeploymentLifecycleCutoverLegacy:
		if stableParent != nil && stableParent.CandidateVersion == runtimechange.CandidateVersion &&
			release.ParentStableReleaseID != nil && *release.ParentStableReleaseID == stableParent.ID &&
			stableParent.PublicModelID == release.PublicModelID && stableParent.ModelName == release.ModelName &&
			stableParent.Stage == "active" && stableParent.RouteItemsSealedAt != nil &&
			release.Stage == "validated" && slot.Exists && slot.AssignedPublicModelID == nil &&
			optionalIDEquals(slot.StableReleaseID, stableParent.ID) && slot.CanaryReleaseID == nil &&
			slot.CanaryBasisPoints == 0 {
			return true, ""
		}
		return false, "legacy_cutover_requires_exact_sealed_v1_stable_slot"
	case runtimechange.DeploymentLifecycleRollbackLegacy:
		if stableParent != nil && stableParent.CandidateVersion == runtimechange.CandidateVersion &&
			release.ParentStableReleaseID != nil && *release.ParentStableReleaseID == stableParent.ID &&
			stableParent.PublicModelID == release.PublicModelID && stableParent.ModelName == release.ModelName &&
			stableParent.Stage == "rolled_back" && stableParent.RouteItemsSealedAt != nil &&
			release.Stage == "active" && slot.Exists &&
			optionalIDEquals(slot.AssignedPublicModelID, release.PublicModelID) &&
			optionalIDEquals(slot.StableReleaseID, release.ID) && slot.CanaryReleaseID == nil &&
			slot.CanaryBasisPoints == 0 {
			return true, ""
		}
		return false, "legacy_rollback_requires_exact_canonical_stable_slot"
	default:
		return false, "unsupported_lifecycle_action"
	}
}

func optionalIDEquals(value *int64, expected int64) bool {
	return value != nil && *value == expected
}

func sameOptionalID(left, right *int64) bool {
	return left != nil && right != nil && *left == *right
}
