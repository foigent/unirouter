package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/runtimechange"
)

var (
	errCanonicalDeploymentReleaseAuthoringInvalid             = errors.New("canonical deployment release authoring request is invalid")
	errCanonicalDeploymentReleaseAuthoringIdempotencyRequired = errors.New("canonical deployment release authoring idempotency key is required")
)

type canonicalDeploymentReleaseAuthoringRequest struct {
	ProfileDefinitionDigest string                                     `json:"profile_definition_digest,omitempty"`
	PublicContextLimit      int                                        `json:"public_context_limit,omitempty"`
	PublicOutputLimit       int                                        `json:"public_output_limit,omitempty"`
	CapabilityApprovalIDs   []int64                                    `json:"capability_approval_ids,omitempty"`
	PublicModelID           int64                                      `json:"public_model_id"`
	Baseline                runtimechange.DeploymentReleaseBaseline    `json:"baseline"`
	Selections              []runtimechange.DeploymentReleaseSelection `json:"selections"`
	Reason                  string                                     `json:"reason"`
}

type canonicalDeploymentReleaseAuthoringDescriptor struct {
	Available             bool   `json:"available"`
	ReasonCode            string `json:"reason_code"`
	StructurallyAvailable bool   `json:"structurally_available"`
	Authorized            bool   `json:"authorized"`
	// Deprecated: always true means no commercial restriction, not authorization.
	Licensed            bool                                       `json:"licensed"`
	LicenseFeature      string                                     `json:"license_feature,omitempty"`
	Method              string                                     `json:"method"`
	Path                string                                     `json:"path"`
	IdempotencyRequired bool                                       `json:"idempotency_required"`
	RequiredFields      []string                                   `json:"required_fields"`
	Baseline            *runtimechange.DeploymentReleaseBaseline   `json:"baseline,omitempty"`
	Slot                runtimechange.DeploymentLifecycleSlotState `json:"slot"`
	StableRelease       *runtimechange.DeploymentReleaseSummary    `json:"stable_release,omitempty"`
}

type canonicalDeploymentReleaseAuthoringModelView struct {
	PublicModel      runtimechange.DeploymentReleasePublicModelSummary `json:"public_model"`
	Authoring        canonicalDeploymentReleaseAuthoringDescriptor     `json:"authoring"`
	SelectionOptions []runtimechange.DeploymentReleaseSelectionOption  `json:"selection_options"`
}

type canonicalDeploymentReleaseCollectionView struct {
	ContractVersion string                                         `json:"contract_version"`
	GeneratedAt     time.Time                                      `json:"generated_at"`
	Models          []canonicalDeploymentReleaseAuthoringModelView `json:"models"`
	Releases        []runtimechange.DeploymentReleaseSummary       `json:"releases"`
}

func canonicalDeploymentReleaseAuthoringMutation(
	request canonicalDeploymentReleaseAuthoringRequest,
) (runtimechange.DeploymentReleaseAuthoringMutation, error) {
	mutation, err := runtimechange.NormalizeDeploymentReleaseAuthoringMutation(
		runtimechange.DeploymentReleaseAuthoringMutation{
			ProfileDefinitionDigest: request.ProfileDefinitionDigest, PublicContextLimit: request.PublicContextLimit, PublicOutputLimit: request.PublicOutputLimit, CapabilityApprovalIDs: request.CapabilityApprovalIDs,
			PublicModelID: request.PublicModelID,
			Baseline:      request.Baseline,
			Selections:    request.Selections,
			Reason:        request.Reason,
		},
	)
	if err != nil {
		return runtimechange.DeploymentReleaseAuthoringMutation{}, fmt.Errorf(
			"%w: %v", errCanonicalDeploymentReleaseAuthoringInvalid, err,
		)
	}
	return mutation, nil
}

func canonicalDeploymentReleaseAuthoringFingerprint(
	mutation runtimechange.DeploymentReleaseAuthoringMutation,
	secret string,
) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("canonical deployment release authoring fingerprint secret is unavailable")
	}
	normalized, err := runtimechange.NormalizeDeploymentReleaseAuthoringMutation(mutation)
	if err != nil {
		return "", errCanonicalDeploymentReleaseAuthoringInvalid
	}
	wire, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal canonical deployment release authoring fingerprint: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("unirouter.model-deployment-release-authoring.request.v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(wire)
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil)), nil
}

func canonicalDeploymentReleaseAuthoringModels(
	options runtimechange.DeploymentReleaseAuthoringOptions,
	canWrite bool,
) []canonicalDeploymentReleaseAuthoringModelView {
	models := make([]canonicalDeploymentReleaseAuthoringModelView, 0, len(options.Models))
	for _, option := range options.Models {
		available := option.StructurallyAvailable && canWrite
		reasonCode := "ready_for_authoring"
		switch {
		case !option.StructurallyAvailable:
			reasonCode = option.ReasonCode
		case !canWrite:
			reasonCode = "organization_write_role_required"
		}
		models = append(models, canonicalDeploymentReleaseAuthoringModelView{
			PublicModel: option.PublicModel,
			Authoring: canonicalDeploymentReleaseAuthoringDescriptor{
				Available: available, ReasonCode: reasonCode,
				StructurallyAvailable: option.StructurallyAvailable,
				Authorized:            canWrite, Licensed: true,
				Method: "POST", Path: "/api/v1/model-deployment-releases",
				IdempotencyRequired: true,
				RequiredFields:      []string{"public_model_id", "baseline", "selections", "reason"},
				Baseline:            option.Baseline, Slot: option.Slot, StableRelease: option.StableRelease,
			},
			SelectionOptions: option.SelectionOptions,
		})
	}
	return models
}
