package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/uniroute/unirouter/internal/runtimechange"
)

const modelSuccessorCutoverMaxRequestBytes = 16 << 10

var errModelSuccessorCutoverInvalid = errors.New("model successor cutover request is invalid")

type modelSuccessorCutoverRequest struct {
	PredecessorAuthorityMode           string                                         `json:"predecessor_authority_mode,omitempty"`
	ExpectedPrimaryAliasID             int64                                          `json:"expected_primary_alias_id,omitempty"`
	ExpectedPredecessorSlotVersion     int64                                          `json:"expected_predecessor_slot_version,omitempty"`
	ExpectedPredecessorReleaseRef      string                                         `json:"expected_predecessor_release_ref,omitempty"`
	ExpectedReleaseVersion             int64                                          `json:"expected_release_version"`
	StableRequestModel                 string                                         `json:"stable_request_model"`
	PredecessorPublicModelID           int64                                          `json:"predecessor_public_model_id"`
	PredecessorModelName               string                                         `json:"predecessor_model_name"`
	ExpectedPredecessorLifecycle       string                                         `json:"expected_predecessor_lifecycle"`
	ExpectedPredecessorApprovedCatalog bool                                           `json:"expected_predecessor_approved_catalog"`
	SuccessorPublicModelID             int64                                          `json:"successor_public_model_id"`
	SuccessorModelName                 string                                         `json:"successor_model_name"`
	ExpectedSuccessorLifecycle         string                                         `json:"expected_successor_lifecycle"`
	ExpectedSuccessorApprovedCatalog   bool                                           `json:"expected_successor_approved_catalog"`
	ExpectedModelSets                  []runtimechange.ModelSuccessorExpectedModelSet `json:"expected_model_sets"`
	Reason                             string                                         `json:"reason,omitempty"`
}

func modelSuccessorCutoverMutation(releaseRef string, request modelSuccessorCutoverRequest) (runtimechange.ModelSuccessorCutoverMutation, error) {
	mode := request.PredecessorAuthorityMode
	if mode == "" {
		mode = runtimechange.ModelSuccessorPredecessorPublished
	}
	if mode != runtimechange.ModelSuccessorPredecessorPublished && mode != runtimechange.ModelSuccessorPredecessorPrimaryAlias {
		return runtimechange.ModelSuccessorCutoverMutation{}, errModelSuccessorCutoverInvalid
	}
	return modelSuccessorCutoverMutationForMode(releaseRef, request, mode)
}

func stagingModelSuccessorCutoverMutation(releaseRef string, request modelSuccessorCutoverRequest) (runtimechange.ModelSuccessorCutoverMutation, error) {
	if request.PredecessorAuthorityMode != "" && request.PredecessorAuthorityMode != runtimechange.ModelSuccessorPredecessorNeverActivated {
		return runtimechange.ModelSuccessorCutoverMutation{}, errModelSuccessorCutoverInvalid
	}
	return modelSuccessorCutoverMutationForMode(releaseRef, request, runtimechange.ModelSuccessorPredecessorNeverActivated)
}

func modelSuccessorCutoverMutationForMode(
	releaseRef string,
	request modelSuccessorCutoverRequest,
	predecessorAuthorityMode string,
) (runtimechange.ModelSuccessorCutoverMutation, error) {
	mutation, err := runtimechange.NormalizeModelSuccessorCutoverMutation(runtimechange.ModelSuccessorCutoverMutation{
		PredecessorAuthorityMode:       predecessorAuthorityMode,
		ExpectedPrimaryAliasID:         request.ExpectedPrimaryAliasID,
		ExpectedPredecessorSlotVersion: request.ExpectedPredecessorSlotVersion,
		ExpectedPredecessorReleaseRef:  request.ExpectedPredecessorReleaseRef,
		ReleaseRef:                     releaseRef, ExpectedReleaseVersion: request.ExpectedReleaseVersion,
		StableRequestModel:                 request.StableRequestModel,
		PredecessorPublicModelID:           request.PredecessorPublicModelID,
		PredecessorModelName:               request.PredecessorModelName,
		ExpectedPredecessorLifecycle:       request.ExpectedPredecessorLifecycle,
		ExpectedPredecessorApprovedCatalog: request.ExpectedPredecessorApprovedCatalog,
		SuccessorPublicModelID:             request.SuccessorPublicModelID,
		SuccessorModelName:                 request.SuccessorModelName,
		ExpectedSuccessorLifecycle:         request.ExpectedSuccessorLifecycle,
		ExpectedSuccessorApprovedCatalog:   request.ExpectedSuccessorApprovedCatalog,
		ExpectedModelSets:                  request.ExpectedModelSets,
		Reason:                             request.Reason,
	})
	if err != nil {
		return runtimechange.ModelSuccessorCutoverMutation{}, errModelSuccessorCutoverInvalid
	}
	return mutation, nil
}

func modelSuccessorCutoverFingerprint(mutation runtimechange.ModelSuccessorCutoverMutation, secret string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", errors.New("model successor cutover fingerprint secret is unavailable")
	}
	normalized, err := runtimechange.NormalizeModelSuccessorCutoverMutation(mutation)
	if err != nil {
		return "", errModelSuccessorCutoverInvalid
	}
	wire, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fingerprintDomain := "unirouter.model-successor-cutover.request.v1"
	if normalized.PredecessorAuthorityMode == runtimechange.ModelSuccessorPredecessorNeverActivated {
		fingerprintDomain = "unirouter.model-successor-cutover.request.v2"
	}
	if normalized.PredecessorAuthorityMode == runtimechange.ModelSuccessorPredecessorPrimaryAlias {
		fingerprintDomain = "unirouter.model-successor-cutover.request.v3"
	}
	_, _ = mac.Write([]byte(fingerprintDomain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(wire)
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil)), nil
}
