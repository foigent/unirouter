package runtimechange

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DeploymentLifecycleReceiptContractVersion identifies the durable application
// contract for canonical v2 release transitions. It is independent from the
// runtime-change candidate and assignment contract versions: changing the HTTP
// or idempotency receipt shape must not silently reinterpret a sealed release.
const DeploymentLifecycleReceiptContractVersion = "unirouter.model-deployment-lifecycle-receipt.v1"

// DeploymentLifecycleAction is deliberately stage-specific. In particular,
// removing a canary and restoring a previous stable release are different
// authority decisions even though the lower-level runtime state machine can
// implement both as a rollback transition.
type DeploymentLifecycleAction string

const (
	DeploymentLifecycleActivateInitial DeploymentLifecycleAction = "activate_initial"
	DeploymentLifecycleStartCanary     DeploymentLifecycleAction = "start_canary"
	DeploymentLifecyclePromote         DeploymentLifecycleAction = "promote"
	DeploymentLifecycleCancelCanary    DeploymentLifecycleAction = "cancel_canary"
	DeploymentLifecycleRollbackStable  DeploymentLifecycleAction = "rollback_stable"
	DeploymentLifecycleCutoverLegacy   DeploymentLifecycleAction = "cutover_legacy"
	DeploymentLifecycleRollbackLegacy  DeploymentLifecycleAction = "rollback_legacy"
)

var ErrDeploymentLifecycleIdempotencyConflict = errors.New("deployment lifecycle idempotency key conflicts with another request")

// DeploymentLifecycleMutation is the normalized body of one exact release
// transition. ReleaseRef comes from the route and is included here only after
// route/body normalization so it participates in the request fingerprint.
type DeploymentLifecycleMutation struct {
	Action                 DeploymentLifecycleAction `json:"action"`
	ReleaseRef             string                    `json:"release_ref"`
	ExpectedReleaseVersion int64                     `json:"expected_release_version"`
	ExpectedSlotVersion    int64                     `json:"expected_slot_version,omitempty"`
	CanaryBasisPoints      int                       `json:"canary_basis_points,omitempty"`
	Reason                 string                    `json:"reason,omitempty"`
}

// DeploymentLifecycleCommand adds authenticated tenant and idempotency
// identity to a normalized mutation. Only hashes reach durable storage; the
// raw Idempotency-Key never crosses this boundary.
type DeploymentLifecycleCommand struct {
	Mutation           DeploymentLifecycleMutation
	CustomerOrgID      int64
	ActorUserID        int64
	ActorOrgID         int64
	IdempotencyKeyHash string
	RequestFingerprint string
	ServerOperationID  string
	Audit              DeploymentLifecycleAudit
}

// DeploymentLifecycleAudit contains request metadata required to insert the
// audit row in the same repeatable-read transaction as the receipt and route
// assignment. It intentionally contains no authorization decision or secret.
type DeploymentLifecycleAudit struct {
	RequestID       string
	ClientRequestID string
	Source          string
	IP              string
	UserAgent       string
}

// DeploymentLifecycleSlotState is the live, tenant-scoped slot projection used
// to calculate server-owned action descriptors. StructurallyAvailable is never
// a promise that a later write will succeed: the write transaction revalidates
// release, route, credential, qualification and optimistic versions.
type DeploymentLifecycleSlotState struct {
	Exists                bool   `json:"exists"`
	PublicModelID         int64  `json:"public_model_id"`
	AssignedPublicModelID *int64 `json:"assigned_public_model_id,omitempty"`
	ModelName             string `json:"model_name"`
	StableReleaseID       *int64 `json:"stable_release_id,omitempty"`
	CanaryReleaseID       *int64 `json:"canary_release_id,omitempty"`
	CanaryBasisPoints     int    `json:"canary_basis_points"`
	Version               int64  `json:"version"`
}

// DeploymentLifecycleReceipt is an immutable replay result. A replay returns
// these frozen transition-time facts even when the live release has advanced.
type DeploymentLifecycleReceipt struct {
	ContractVersion    string                                    `json:"contract_version"`
	ReceiptID          int64                                     `json:"receipt_id"`
	Action             DeploymentLifecycleAction                 `json:"action"`
	TargetReleaseRef   string                                    `json:"target_release_ref"`
	CustomerOrgID      int64                                     `json:"customer_org_id"`
	ActorUserID        int64                                     `json:"actor_user_id,omitempty"`
	ServerOperationID  string                                    `json:"server_operation_id"`
	OriginalHTTPStatus int                                       `json:"original_http_status"`
	IdempotencyReplay  bool                                      `json:"idempotency_replay"`
	Reason             string                                    `json:"reason,omitempty"`
	LegacyPlanHash     string                                    `json:"legacy_plan_hash,omitempty"`
	DeploymentPlanHash string                                    `json:"deployment_plan_hash,omitempty"`
	Slot               DeploymentLifecycleSlotReceipt            `json:"slot"`
	Releases           []DeploymentLifecycleReleaseReceipt       `json:"releases"`
	Qualifications     []DeploymentLifecycleQualificationReceipt `json:"qualifications"`
	CreatedAt          time.Time                                 `json:"created_at"`
	CompletedAt        time.Time                                 `json:"completed_at"`
}

// DeploymentLifecycleSlotReceipt freezes the slot CAS before and after the
// transition. Nil release IDs represent an absent assignment, including the
// initial-activation precondition.
type DeploymentLifecycleSlotReceipt struct {
	PublicModelID           int64  `json:"public_model_id"`
	ModelName               string `json:"model_name"`
	BeforePublicModelID     *int64 `json:"before_public_model_id,omitempty"`
	BeforeStableReleaseID   *int64 `json:"before_stable_release_id,omitempty"`
	BeforeCanaryReleaseID   *int64 `json:"before_canary_release_id,omitempty"`
	BeforeCanaryBasisPoints int    `json:"before_canary_basis_points"`
	BeforeVersion           int64  `json:"before_version"`
	AfterPublicModelID      *int64 `json:"after_public_model_id,omitempty"`
	AfterStableReleaseID    *int64 `json:"after_stable_release_id,omitempty"`
	AfterCanaryReleaseID    *int64 `json:"after_canary_release_id,omitempty"`
	AfterCanaryBasisPoints  int    `json:"after_canary_basis_points"`
	AfterVersion            int64  `json:"after_version"`
}

// DeploymentLifecycleReleaseReceipt records every release whose state was
// used by the decision, not just rows that were updated. This preserves the
// exact stable-parent authority for canary and rollback decisions.
type DeploymentLifecycleReleaseReceipt struct {
	Role           string `json:"role"`
	ReleaseID      int64  `json:"release_id"`
	ReleaseRef     string `json:"release_ref"`
	CandidateHash  string `json:"candidate_hash"`
	RouteItemsHash string `json:"route_items_hash"`
	BeforeStage    string `json:"before_stage"`
	BeforeVersion  int64  `json:"before_version"`
	AfterStage     string `json:"after_stage"`
	AfterVersion   int64  `json:"after_version"`
}

// DeploymentLifecycleQualificationReceipt freezes M75 qualification truth for
// each sealed route item consulted by the lifecycle decision. Operations that
// do not require M75 approval are explicit "not_required" rows rather than
// disappearing from the receipt.
type DeploymentLifecycleQualificationReceipt struct {
	ReleaseID                  int64  `json:"release_id"`
	ReleaseRole                string `json:"release_role"`
	Ordinal                    int    `json:"ordinal"`
	DeploymentID               int64  `json:"deployment_id"`
	OperationID                string `json:"operation_id"`
	QualificationRequired      bool   `json:"qualification_required"`
	Qualified                  bool   `json:"qualified"`
	QualificationState         string `json:"qualification_state"`
	ApprovalID                 *int64 `json:"approval_id,omitempty"`
	ProbeID                    *int64 `json:"probe_id,omitempty"`
	ConnectionID               int64  `json:"connection_id,omitempty"`
	HTTPSOrigin                string `json:"https_origin,omitempty"`
	PublicPath                 string `json:"public_path,omitempty"`
	ExecutorContractVersion    string `json:"executor_contract_version,omitempty"`
	ExpectedConnectionRevision int64  `json:"expected_connection_revision"`
	ExpectedDeploymentRevision int64  `json:"expected_deployment_revision"`
	ExpectedOperationRevision  int64  `json:"expected_operation_revision"`
	CurrentConnectionRevision  int64  `json:"current_connection_revision,omitempty"`
	CurrentDeploymentRevision  int64  `json:"current_deployment_revision,omitempty"`
	CurrentOperationRevision   int64  `json:"current_operation_revision,omitempty"`
	ApprovedConnectionRevision int64  `json:"approved_connection_revision,omitempty"`
	ApprovedDeploymentRevision int64  `json:"approved_deployment_revision,omitempty"`
	ApprovedOperationRevision  int64  `json:"approved_operation_revision,omitempty"`
}

// NormalizeDeploymentLifecycleMutation validates action-specific optimistic
// concurrency fields before any database or idempotency receipt access.
func NormalizeDeploymentLifecycleMutation(
	mutation DeploymentLifecycleMutation,
) (DeploymentLifecycleMutation, error) {
	mutation.ReleaseRef = strings.TrimSpace(strings.ToLower(mutation.ReleaseRef))
	mutation.Reason = strings.TrimSpace(mutation.Reason)
	if !releaseRefPattern.MatchString(mutation.ReleaseRef) || mutation.ExpectedReleaseVersion <= 0 {
		return DeploymentLifecycleMutation{}, fmt.Errorf("%w: deployment lifecycle receipt identity", ErrInvalid)
	}
	if len(mutation.Reason) > 1000 || strings.ContainsAny(mutation.Reason, "\x00\r\n") {
		return DeploymentLifecycleMutation{}, fmt.Errorf("%w: deployment lifecycle reason", ErrInvalid)
	}
	switch mutation.Action {
	case DeploymentLifecycleActivateInitial:
		if mutation.ExpectedSlotVersion != 0 || mutation.CanaryBasisPoints != 0 {
			return DeploymentLifecycleMutation{}, fmt.Errorf("%w: initial activation precondition", ErrInvalid)
		}
	case DeploymentLifecycleStartCanary:
		if mutation.ExpectedSlotVersion <= 0 || mutation.CanaryBasisPoints < 1 || mutation.CanaryBasisPoints > 5000 {
			return DeploymentLifecycleMutation{}, fmt.Errorf("%w: canary transition precondition", ErrInvalid)
		}
	case DeploymentLifecyclePromote, DeploymentLifecycleCancelCanary,
		DeploymentLifecycleRollbackStable:
		if mutation.ExpectedSlotVersion <= 0 || mutation.CanaryBasisPoints != 0 {
			return DeploymentLifecycleMutation{}, fmt.Errorf("%w: deployment lifecycle precondition", ErrInvalid)
		}
	case DeploymentLifecycleCutoverLegacy, DeploymentLifecycleRollbackLegacy:
		if mutation.ExpectedSlotVersion <= 0 || mutation.CanaryBasisPoints != 0 || mutation.Reason == "" {
			return DeploymentLifecycleMutation{}, fmt.Errorf("%w: legacy deployment lifecycle precondition", ErrInvalid)
		}
	default:
		return DeploymentLifecycleMutation{}, fmt.Errorf("%w: deployment lifecycle action", ErrInvalid)
	}
	return mutation, nil
}

// NormalizeDeploymentLifecycleCommand validates the authenticated and hashed
// application identity. The raw key and request body are intentionally absent.
func NormalizeDeploymentLifecycleCommand(
	command DeploymentLifecycleCommand,
) (DeploymentLifecycleCommand, error) {
	mutation, err := NormalizeDeploymentLifecycleMutation(command.Mutation)
	if err != nil {
		return DeploymentLifecycleCommand{}, err
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
		return DeploymentLifecycleCommand{}, fmt.Errorf("%w: deployment lifecycle command identity", ErrInvalid)
	}
	return command, nil
}

func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
