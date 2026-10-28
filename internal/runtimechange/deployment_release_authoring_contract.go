package runtimechange

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const (
	DeploymentReleaseAuthoringReceiptContractVersion = "unirouter.model-deployment-release-authoring-receipt.v1"
	deploymentReleaseAuthoringOperationScope         = "model_deployment.release_authoring.create.v1"
)

var (
	ErrDeploymentReleaseAuthoringIdempotencyConflict = errors.New("deployment release authoring idempotency key conflicts with another request")
	authoringKeyHashPattern                          = regexp.MustCompile(`^[a-f0-9]{64}$`)
	authoringFingerprintPattern                      = regexp.MustCompile(`^hmac-sha256:[a-f0-9]{64}$`)
)

type DeploymentReleaseBaselineKind string

const (
	DeploymentReleaseBaselineInitial       DeploymentReleaseBaselineKind = "initial"
	DeploymentReleaseBaselineStable        DeploymentReleaseBaselineKind = "stable"
	DeploymentReleaseBaselineLegacyCutover DeploymentReleaseBaselineKind = "legacy_cutover"
)

// DeploymentReleaseBaseline is the caller's exact optimistic baseline. The
// stable release ID, candidate hash and parent pointer remain server-owned.
type DeploymentReleaseBaseline struct {
	Kind                         DeploymentReleaseBaselineKind `json:"kind"`
	ExpectedSlotVersion          int64                         `json:"expected_slot_version"`
	StableReleaseRef             string                        `json:"stable_release_ref,omitempty"`
	ExpectedStableReleaseVersion int64                         `json:"expected_stable_release_version,omitempty"`
}

// DeploymentReleaseSelection is the complete caller-owned part of one route
// member. Revisions, profiles, paths, executor and public contracts are read
// from the locked database snapshot by the server.
type DeploymentReleaseSelection struct {
	ExpectedUpstreamObservationIdentity string `json:"expected_upstream_observation_identity,omitempty"`
	DeploymentID                        int64  `json:"deployment_id"`
	OperationID                         string `json:"operation_id"`
}

type DeploymentReleaseAuthoringMutation struct {
	ProfileDefinitionDigest string                       `json:"profile_definition_digest,omitempty"`
	PublicContextLimit      int                          `json:"public_context_limit,omitempty"`
	PublicOutputLimit       int                          `json:"public_output_limit,omitempty"`
	CapabilityApprovalIDs   []int64                      `json:"capability_approval_ids,omitempty"`
	PublicModelID           int64                        `json:"public_model_id"`
	Baseline                DeploymentReleaseBaseline    `json:"baseline"`
	Selections              []DeploymentReleaseSelection `json:"selections"`
	Reason                  string                       `json:"reason"`
}

type DeploymentReleaseAuthoringCommand struct {
	Mutation           DeploymentReleaseAuthoringMutation
	CustomerOrgID      int64
	ActorUserID        int64
	ActorOrgID         int64
	IdempotencyKeyHash string
	RequestFingerprint string
	ServerOperationID  string
	Audit              DeploymentReleaseAuthoringAudit
}

type DeploymentReleaseAuthoringAudit struct {
	RequestID       string
	ClientRequestID string
	Source          string
	IP              string
	UserAgent       string
}

// DeploymentReleaseAuthoringReceipt is the frozen result of the one atomic
// draft-to-validated command. Same-server-operation commit-ACK recovery keeps
// IdempotencyReplay false; only a later operation observing the winner sets it.
type DeploymentReleaseAuthoringReceipt struct {
	ContractVersion    string                       `json:"contract_version"`
	ReceiptID          int64                        `json:"receipt_id"`
	CustomerOrgID      int64                        `json:"customer_org_id"`
	ActorUserID        int64                        `json:"actor_user_id,omitempty"`
	ServerOperationID  string                       `json:"server_operation_id"`
	OriginalHTTPStatus int                          `json:"original_http_status"`
	OriginalLocation   string                       `json:"original_location"`
	IdempotencyReplay  bool                         `json:"idempotency_replay"`
	Baseline           DeploymentReleaseBaseline    `json:"baseline"`
	Selections         []DeploymentReleaseSelection `json:"selections"`
	Release            DeploymentRelease            `json:"release"`
	CreatedAt          time.Time                    `json:"created_at"`
	CompletedAt        time.Time                    `json:"completed_at"`
}

// MarshalJSON preserves the exact v1 authoring-receipt wire contract. M82 adds
// release-owned execution pins, but those facts are exposed by the exact
// release and lifecycle inspection APIs rather than retroactively changing an
// already versioned idempotency receipt body.
func (receipt DeploymentReleaseAuthoringReceipt) MarshalJSON() ([]byte, error) {
	type releaseV1 struct {
		ID                    int64               `json:"id"`
		ReleaseRef            string              `json:"release_ref"`
		PublicModelID         int64               `json:"public_model_id"`
		ModelName             string              `json:"model_name"`
		CandidateVersion      string              `json:"candidate_version"`
		CandidateHash         string              `json:"candidate_hash"`
		BaselineHash          string              `json:"baseline_hash"`
		RouteItemsHash        string              `json:"route_items_hash"`
		RouteItemsCount       int                 `json:"route_items_count"`
		RouteItemsSealedAt    *time.Time          `json:"route_items_sealed_at,omitempty"`
		Candidate             DeploymentCandidate `json:"candidate"`
		Stage                 string              `json:"stage"`
		Version               int64               `json:"version"`
		ParentStableReleaseID *int64              `json:"parent_stable_release_id,omitempty"`
		CanaryBasisPoints     int                 `json:"canary_basis_points"`
		Reason                string              `json:"reason"`
		CreatedBy             int64               `json:"created_by"`
		ValidatedBy           *int64              `json:"validated_by,omitempty"`
		ActivatedBy           *int64              `json:"activated_by,omitempty"`
		CreatedAt             time.Time           `json:"created_at"`
		ValidatedAt           *time.Time          `json:"validated_at,omitempty"`
		ActivatedAt           *time.Time          `json:"activated_at,omitempty"`
		RolledBackAt          *time.Time          `json:"rolled_back_at,omitempty"`
		UpdatedAt             time.Time           `json:"updated_at"`
	}
	type receiptV1 struct {
		ContractVersion    string                       `json:"contract_version"`
		ReceiptID          int64                        `json:"receipt_id"`
		CustomerOrgID      int64                        `json:"customer_org_id"`
		ActorUserID        int64                        `json:"actor_user_id,omitempty"`
		ServerOperationID  string                       `json:"server_operation_id"`
		OriginalHTTPStatus int                          `json:"original_http_status"`
		OriginalLocation   string                       `json:"original_location"`
		IdempotencyReplay  bool                         `json:"idempotency_replay"`
		Baseline           DeploymentReleaseBaseline    `json:"baseline"`
		Selections         []DeploymentReleaseSelection `json:"selections"`
		Release            releaseV1                    `json:"release"`
		CreatedAt          time.Time                    `json:"created_at"`
		CompletedAt        time.Time                    `json:"completed_at"`
	}
	release := receipt.Release
	return json.Marshal(receiptV1{
		ContractVersion: receipt.ContractVersion, ReceiptID: receipt.ReceiptID,
		CustomerOrgID: receipt.CustomerOrgID, ActorUserID: receipt.ActorUserID,
		ServerOperationID: receipt.ServerOperationID, OriginalHTTPStatus: receipt.OriginalHTTPStatus,
		OriginalLocation: receipt.OriginalLocation, IdempotencyReplay: receipt.IdempotencyReplay,
		Baseline: receipt.Baseline, Selections: receipt.Selections,
		Release: releaseV1{
			ID: release.ID, ReleaseRef: release.ReleaseRef, PublicModelID: release.PublicModelID,
			ModelName: release.ModelName, CandidateVersion: release.CandidateVersion,
			CandidateHash: release.CandidateHash, BaselineHash: release.BaselineHash,
			RouteItemsHash: release.RouteItemsHash, RouteItemsCount: release.RouteItemsCount,
			RouteItemsSealedAt: release.RouteItemsSealedAt, Candidate: release.Candidate,
			Stage: release.Stage, Version: release.Version,
			ParentStableReleaseID: release.ParentStableReleaseID,
			CanaryBasisPoints:     release.CanaryBasisPoints, Reason: release.Reason,
			CreatedBy: release.CreatedBy, ValidatedBy: release.ValidatedBy,
			ActivatedBy: release.ActivatedBy, CreatedAt: release.CreatedAt,
			ValidatedAt: release.ValidatedAt, ActivatedAt: release.ActivatedAt,
			RolledBackAt: release.RolledBackAt, UpdatedAt: release.UpdatedAt,
		},
		CreatedAt: receipt.CreatedAt, CompletedAt: receipt.CompletedAt,
	})
}

func NormalizeDeploymentReleaseAuthoringCommand(
	command DeploymentReleaseAuthoringCommand,
) (DeploymentReleaseAuthoringCommand, error) {
	mutation, err := NormalizeDeploymentReleaseAuthoringMutation(command.Mutation)
	if err != nil {
		return DeploymentReleaseAuthoringCommand{}, err
	}
	command.Mutation = mutation
	command.ServerOperationID = strings.TrimSpace(command.ServerOperationID)
	if command.CustomerOrgID <= 0 || command.ActorUserID <= 0 || command.ActorOrgID <= 0 ||
		command.Mutation.PublicModelID <= 0 ||
		!authoringKeyHashPattern.MatchString(command.IdempotencyKeyHash) ||
		!authoringFingerprintPattern.MatchString(command.RequestFingerprint) ||
		command.ServerOperationID == "" || len(command.ServerOperationID) > 80 ||
		strings.ContainsAny(command.ServerOperationID, "\x00\r\n") {
		return DeploymentReleaseAuthoringCommand{}, fmt.Errorf("%w: deployment release authoring identity", ErrInvalid)
	}
	return command, nil
}

// NormalizeDeploymentReleaseAuthoringMutation validates the complete
// caller-owned HTTP payload independently from authenticated command identity.
// Revisions, route contracts, hashes, parent IDs and the release reference are
// deliberately absent: the atomic writer projects those server-owned fields.
func NormalizeDeploymentReleaseAuthoringMutation(
	mutation DeploymentReleaseAuthoringMutation,
) (DeploymentReleaseAuthoringMutation, error) {
	if mutation.ProfileDefinitionDigest == "" {
		if mutation.PublicContextLimit != 0 || mutation.PublicOutputLimit != 0 || len(mutation.CapabilityApprovalIDs) > 0 {
			return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: execution snapshot requires exact Profile definition", ErrInvalid)
		}
	} else {
		if !modelprofile.IsDefinitionDigest(mutation.ProfileDefinitionDigest) || mutation.PublicContextLimit <= 0 || mutation.PublicOutputLimit <= 0 || len(mutation.CapabilityApprovalIDs) > 1024 {
			return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: data Release execution pins", ErrInvalid)
		}
		mutation.CapabilityApprovalIDs = slices.Clone(mutation.CapabilityApprovalIDs)
		slices.Sort(mutation.CapabilityApprovalIDs)
		for i, id := range mutation.CapabilityApprovalIDs {
			if id <= 0 || (i > 0 && id == mutation.CapabilityApprovalIDs[i-1]) {
				return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: duplicate or invalid capability approval", ErrInvalid)
			}
		}
	}
	mutation.Selections = append([]DeploymentReleaseSelection(nil), mutation.Selections...)
	mutation.Reason = strings.TrimSpace(mutation.Reason)
	mutation.Baseline.StableReleaseRef = strings.ToLower(strings.TrimSpace(
		mutation.Baseline.StableReleaseRef,
	))
	if mutation.PublicModelID <= 0 {
		return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: deployment release authoring public model", ErrInvalid)
	}
	if mutation.Reason == "" || len(mutation.Reason) > 500 ||
		strings.ContainsAny(mutation.Reason, "\x00\r\n") {
		return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: deployment release authoring reason", ErrInvalid)
	}
	switch mutation.Baseline.Kind {
	case DeploymentReleaseBaselineInitial:
		if mutation.Baseline.ExpectedSlotVersion != 0 ||
			mutation.Baseline.StableReleaseRef != "" ||
			mutation.Baseline.ExpectedStableReleaseVersion != 0 {
			return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: initial release baseline", ErrInvalid)
		}
	case DeploymentReleaseBaselineStable, DeploymentReleaseBaselineLegacyCutover:
		if mutation.Baseline.ExpectedSlotVersion <= 0 ||
			!releaseRefPattern.MatchString(mutation.Baseline.StableReleaseRef) ||
			mutation.Baseline.ExpectedStableReleaseVersion <= 0 {
			return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: stable release baseline", ErrInvalid)
		}
	default:
		return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: release baseline kind", ErrInvalid)
	}
	if len(mutation.Selections) == 0 || len(mutation.Selections) > maxDeploymentRoutes {
		return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: deployment release selections", ErrInvalid)
	}
	allowed := map[string]struct{}{
		string(modeloperation.IDChatCompletions): {},
		string(modeloperation.IDEmbeddings):      {},
		string(modeloperation.IDResponses):       {},
		string(modeloperation.IDMessages):        {},
	}
	seen := make(map[string]struct{}, len(mutation.Selections))
	for index := range mutation.Selections {
		selection := &mutation.Selections[index]
		selection.OperationID = strings.TrimSpace(selection.OperationID)
		if !upstreamrelease.ValidIdentity(selection.ExpectedUpstreamObservationIdentity) {
			return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: upstream observation identity", ErrInvalid)
		}
		if mutation.ProfileDefinitionDigest != "" && selection.OperationID != modelprofile.OperationOpenAIChat {
			return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: data Release v1 supports typed Chat only", ErrInvalid)
		}
		if selection.DeploymentID <= 0 {
			return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: selections[%d].deployment_id", ErrInvalid, index)
		}
		if _, ok := allowed[selection.OperationID]; !ok {
			return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: selections[%d].operation_id", ErrInvalid, index)
		}
		key := fmt.Sprintf("%d\x00%s", selection.DeploymentID, selection.OperationID)
		if _, duplicate := seen[key]; duplicate {
			return DeploymentReleaseAuthoringMutation{}, fmt.Errorf("%w: duplicate deployment release selection", ErrInvalid)
		}
		seen[key] = struct{}{}
	}
	return mutation, nil
}
