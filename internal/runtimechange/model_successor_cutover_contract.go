package runtimechange

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// ModelSuccessorLifecycleReceiptContractVersion distinguishes the larger
	// atomic successor command from an ordinary initial deployment activation.
	// The embedded runtime transition remains activate_initial.
	ModelSuccessorLifecycleReceiptContractVersion        = "unirouter.model-deployment-lifecycle-receipt.v2"
	ModelSuccessorCutoverReceiptContractVersion          = "unirouter.model-successor-cutover-receipt.v1"
	StagingModelSuccessorLifecycleReceiptContractVersion = "unirouter.model-deployment-lifecycle-receipt.v3"
	StagingModelSuccessorCutoverReceiptContractVersion   = "unirouter.model-successor-cutover-receipt.v2"

	ModelSuccessorPredecessorPublished      = "published"
	ModelSuccessorPredecessorNeverActivated = "never_activated_staging"
)

// ModelSuccessorCutoverMutation is intentionally limited to the first
// successor of a stable canonical request identity. A later primary-alias
// retarget needs an explicit authorization-carrier migration contract.
type ModelSuccessorCutoverMutation struct {
	PredecessorAuthorityMode           string                           `json:"-"`
	ReleaseRef                         string                           `json:"release_ref"`
	ExpectedReleaseVersion             int64                            `json:"expected_release_version"`
	StableRequestModel                 string                           `json:"stable_request_model"`
	PredecessorPublicModelID           int64                            `json:"predecessor_public_model_id"`
	PredecessorModelName               string                           `json:"predecessor_model_name"`
	ExpectedPredecessorLifecycle       string                           `json:"expected_predecessor_lifecycle"`
	ExpectedPredecessorApprovedCatalog bool                             `json:"expected_predecessor_approved_catalog"`
	SuccessorPublicModelID             int64                            `json:"successor_public_model_id"`
	SuccessorModelName                 string                           `json:"successor_model_name"`
	ExpectedSuccessorLifecycle         string                           `json:"expected_successor_lifecycle"`
	ExpectedSuccessorApprovedCatalog   bool                             `json:"expected_successor_approved_catalog"`
	ExpectedModelSets                  []ModelSuccessorExpectedModelSet `json:"expected_model_sets"`
	Reason                             string                           `json:"reason,omitempty"`
}

type ModelSuccessorExpectedModelSet struct {
	ModelSetID        int64 `json:"model_set_id"`
	BeforeAliasExists bool  `json:"before_alias_exists"`
}

type ModelSuccessorCutoverCommand struct {
	Mutation           ModelSuccessorCutoverMutation
	CustomerOrgID      int64
	ActorUserID        int64
	ActorOrgID         int64
	IdempotencyKeyHash string
	RequestFingerprint string
	ServerOperationID  string
	Audit              DeploymentLifecycleAudit
}

type ModelSuccessorRegistryState struct {
	PublicModelID   int64  `json:"public_model_id"`
	ModelName       string `json:"model_name"`
	Lifecycle       string `json:"lifecycle"`
	ApprovedCatalog bool   `json:"approved_catalog"`
}

type ModelSuccessorAliasState struct {
	Alias       string `json:"alias"`
	TargetModel string `json:"target_model"`
	IsPrimary   bool   `json:"is_primary"`
	Status      string `json:"status"`
}

type ModelSuccessorModelSetReceipt struct {
	ModelSetID        int64  `json:"model_set_id"`
	BeforeModelName   string `json:"before_model_name"`
	BeforeAliasExists bool   `json:"before_alias_exists"`
	AfterAliasName    string `json:"after_alias_name"`
}

type ModelSuccessorCutoverReceipt struct {
	ContractVersion              string                          `json:"contract_version"`
	PredecessorAuthorityMode     string                          `json:"predecessor_authority_mode,omitempty"`
	PredecessorAuthoritySnapshot map[string]int64                `json:"predecessor_authority_snapshot,omitempty"`
	Lifecycle                    DeploymentLifecycleReceipt      `json:"lifecycle"`
	StableRequestModel           string                          `json:"stable_request_model"`
	PredecessorBefore            ModelSuccessorRegistryState     `json:"predecessor_before"`
	PredecessorAfter             ModelSuccessorRegistryState     `json:"predecessor_after"`
	SuccessorBefore              ModelSuccessorRegistryState     `json:"successor_before"`
	SuccessorAfter               ModelSuccessorRegistryState     `json:"successor_after"`
	AliasAfter                   ModelSuccessorAliasState        `json:"alias_after"`
	ModelSets                    []ModelSuccessorModelSetReceipt `json:"model_sets"`
}

func NormalizeModelSuccessorCutoverMutation(mutation ModelSuccessorCutoverMutation) (ModelSuccessorCutoverMutation, error) {
	mutation.PredecessorAuthorityMode = strings.TrimSpace(mutation.PredecessorAuthorityMode)
	if mutation.PredecessorAuthorityMode == "" {
		mutation.PredecessorAuthorityMode = ModelSuccessorPredecessorPublished
	}
	mutation.ReleaseRef = strings.TrimSpace(strings.ToLower(mutation.ReleaseRef))
	mutation.StableRequestModel = strings.TrimSpace(mutation.StableRequestModel)
	mutation.PredecessorModelName = strings.TrimSpace(mutation.PredecessorModelName)
	mutation.SuccessorModelName = strings.TrimSpace(mutation.SuccessorModelName)
	mutation.ExpectedPredecessorLifecycle = strings.TrimSpace(mutation.ExpectedPredecessorLifecycle)
	mutation.ExpectedSuccessorLifecycle = strings.TrimSpace(mutation.ExpectedSuccessorLifecycle)
	mutation.Reason = strings.TrimSpace(mutation.Reason)
	if mutation.ExpectedModelSets == nil {
		return ModelSuccessorCutoverMutation{}, fmt.Errorf("%w: expected model-set snapshot is required", ErrInvalid)
	}
	expectedModelSets := make([]ModelSuccessorExpectedModelSet, len(mutation.ExpectedModelSets))
	copy(expectedModelSets, mutation.ExpectedModelSets)
	mutation.ExpectedModelSets = expectedModelSets
	sort.Slice(mutation.ExpectedModelSets, func(i, j int) bool {
		return mutation.ExpectedModelSets[i].ModelSetID < mutation.ExpectedModelSets[j].ModelSetID
	})
	for index, item := range mutation.ExpectedModelSets {
		if item.ModelSetID <= 0 || (index > 0 && mutation.ExpectedModelSets[index-1].ModelSetID == item.ModelSetID) {
			return ModelSuccessorCutoverMutation{}, fmt.Errorf("%w: expected model-set snapshot", ErrInvalid)
		}
	}
	publishedPredecessor := mutation.PredecessorAuthorityMode == ModelSuccessorPredecessorPublished &&
		(mutation.ExpectedPredecessorLifecycle == "ga" || mutation.ExpectedPredecessorLifecycle == "deprecated") &&
		mutation.ExpectedPredecessorApprovedCatalog
	neverActivatedPredecessor := mutation.PredecessorAuthorityMode == ModelSuccessorPredecessorNeverActivated &&
		mutation.ExpectedPredecessorLifecycle == "staging" && !mutation.ExpectedPredecessorApprovedCatalog
	if !releaseRefPattern.MatchString(mutation.ReleaseRef) || mutation.ExpectedReleaseVersion <= 0 ||
		mutation.PredecessorPublicModelID <= 0 || mutation.SuccessorPublicModelID <= 0 ||
		mutation.PredecessorPublicModelID == mutation.SuccessorPublicModelID ||
		!validSuccessorModelIdentity(mutation.StableRequestModel) ||
		!validSuccessorModelIdentity(mutation.PredecessorModelName) ||
		!validSuccessorModelIdentity(mutation.SuccessorModelName) ||
		mutation.StableRequestModel != mutation.PredecessorModelName ||
		mutation.SuccessorModelName == mutation.StableRequestModel ||
		(!publishedPredecessor && !neverActivatedPredecessor) ||
		mutation.ExpectedSuccessorLifecycle != "staging" || mutation.ExpectedSuccessorApprovedCatalog ||
		len(mutation.Reason) > 1000 || strings.ContainsAny(mutation.Reason, "\x00\r\n") {
		return ModelSuccessorCutoverMutation{}, fmt.Errorf("%w: model successor cutover mutation", ErrInvalid)
	}
	return mutation, nil
}

func NormalizeModelSuccessorCutoverCommand(command ModelSuccessorCutoverCommand) (ModelSuccessorCutoverCommand, error) {
	mutation, err := NormalizeModelSuccessorCutoverMutation(command.Mutation)
	if err != nil {
		return ModelSuccessorCutoverCommand{}, err
	}
	command.Mutation = mutation
	command.ServerOperationID = strings.TrimSpace(command.ServerOperationID)
	command.Audit.RequestID = strings.TrimSpace(command.Audit.RequestID)
	command.Audit.ClientRequestID = strings.TrimSpace(command.Audit.ClientRequestID)
	command.Audit.Source = strings.TrimSpace(command.Audit.Source)
	if command.CustomerOrgID <= 0 || command.ActorUserID <= 0 || command.ActorOrgID <= 0 ||
		len(command.IdempotencyKeyHash) != 64 || !isLowerHex(command.IdempotencyKeyHash) ||
		len(command.RequestFingerprint) != 76 || !strings.HasPrefix(command.RequestFingerprint, "hmac-sha256:") ||
		!isLowerHex(strings.TrimPrefix(command.RequestFingerprint, "hmac-sha256:")) ||
		command.ServerOperationID == "" || len(command.ServerOperationID) > 80 ||
		strings.ContainsAny(command.ServerOperationID, "\x00\r\n") {
		return ModelSuccessorCutoverCommand{}, fmt.Errorf("%w: model successor cutover command identity", ErrInvalid)
	}
	return command, nil
}

func validSuccessorModelIdentity(value string) bool {
	return value != "" && len(value) <= 100 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n")
}
