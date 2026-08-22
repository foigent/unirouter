// Package runtimechange implements UniRouter's product-local, bounded
// progressive release store. It is not a shared connector runtime and cannot
// extend routing, billing, IAM, credential or provider execution authority.
package runtimechange

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const (
	CandidateVersion            = "unirouter.runtime-change-candidate.v1"
	DeploymentCandidateVersion  = "unirouter.runtime-change-candidate.v2"
	DeploymentAssignmentVersion = "unirouter.runtime-deployment-assignment.v1"
)

var (
	ErrInvalid                    = errors.New("runtimechange: invalid request")
	ErrConflict                   = errors.New("runtimechange: revision conflict")
	ErrNotFound                   = errors.New("runtimechange: release not found")
	ErrUnavailable                = errors.New("runtimechange: state unavailable")
	ErrLegacyRuntimeChangeRetired = errors.New("runtimechange: legacy direct writer retired")

	releaseRefPattern = regexp.MustCompile(`^rcr_[a-z0-9][a-z0-9._-]{2,63}$`)
)

type Candidate struct {
	ContractVersion string                          `json:"contract_version"`
	ModelName       string                          `json:"model_name"`
	Overrides       []routingpolicy.ChannelOverride `json:"overrides"`
}

// DeploymentRoute is one ordered member of a complete v2 route manifest. A
// v2 candidate is not a patch: a deployment absent from DeploymentRoutes is
// absent from that release. The revisions pin connection/deployment identity;
// mutable emergency state remains stronger than the release.
type DeploymentRoute struct {
	DeploymentID               int64  `json:"deployment_id"`
	ModelOperationID           string `json:"model_operation_id"`
	ExpectedConnectionRevision int64  `json:"expected_connection_revision"`
	ExpectedDeploymentRevision int64  `json:"expected_deployment_revision"`
	ExpectedOperationRevision  int64  `json:"expected_operation_revision"`
	ProtocolProfileID          string `json:"protocol_profile_id"`
	ProtocolProfileVersion     string `json:"protocol_profile_version"`
	PublicPath                 string `json:"public_path,omitempty"`
	ExecutorContractVersion    string `json:"executor_contract_version,omitempty"`
	PublicExecutionBindingID   int64  `json:"public_execution_binding_id,omitempty"`
	DeliveryContractVersion    string `json:"delivery_contract_version,omitempty"`
	ChargeContractVersion      string `json:"charge_contract_version,omitempty"`
	IdempotencyContractVersion string `json:"idempotency_contract_version,omitempty"`
	Admission                  string `json:"admission"`
	Priority                   *int   `json:"priority,omitempty"`
	Weight                     *int   `json:"weight,omitempty"`
}

type DeploymentCandidate struct {
	ContractVersion  string            `json:"contract_version"`
	PublicModelID    int64             `json:"public_model_id"`
	ModelName        string            `json:"model_name"`
	DeploymentRoutes []DeploymentRoute `json:"deployment_routes"`
}

type DeploymentDraftRequest struct {
	ReleaseRef       string            `json:"release_ref"`
	PublicModelID    int64             `json:"public_model_id"`
	DeploymentRoutes []DeploymentRoute `json:"deployment_routes"`
	Reason           string            `json:"reason"`
}

type DeploymentRelease struct {
	ID                      int64               `json:"id"`
	ReleaseRef              string              `json:"release_ref"`
	PublicModelID           int64               `json:"public_model_id"`
	ModelName               string              `json:"model_name"`
	ExecutionProfileID      string              `json:"execution_profile_id"`
	ExecutionProfileVersion string              `json:"execution_profile_version"`
	PublicContextLimit      int                 `json:"public_context_limit"`
	PublicOutputLimit       int                 `json:"public_output_limit"`
	CandidateVersion        string              `json:"candidate_version"`
	CandidateHash           string              `json:"candidate_hash"`
	BaselineHash            string              `json:"baseline_hash"`
	RouteItemsHash          string              `json:"route_items_hash"`
	RouteItemsCount         int                 `json:"route_items_count"`
	RouteItemsSealedAt      *time.Time          `json:"route_items_sealed_at,omitempty"`
	Candidate               DeploymentCandidate `json:"candidate"`
	Stage                   string              `json:"stage"`
	Version                 int64               `json:"version"`
	ParentStableReleaseID   *int64              `json:"parent_stable_release_id,omitempty"`
	CanaryBasisPoints       int                 `json:"canary_basis_points"`
	Reason                  string              `json:"reason"`
	CreatedBy               int64               `json:"created_by"`
	ValidatedBy             *int64              `json:"validated_by,omitempty"`
	ActivatedBy             *int64              `json:"activated_by,omitempty"`
	CreatedAt               time.Time           `json:"created_at"`
	ValidatedAt             *time.Time          `json:"validated_at,omitempty"`
	ActivatedAt             *time.Time          `json:"activated_at,omitempty"`
	RolledBackAt            *time.Time          `json:"rolled_back_at,omitempty"`
	UpdatedAt               time.Time           `json:"updated_at"`
}

// DeploymentLifecycleRequest is the optimistic-concurrency boundary for a
// sealed v2 release transition. Both the immutable public-model identity and
// the mutable release/slot versions are supplied by the caller; a stale admin
// page can therefore never replace a newer runtime assignment silently.
type DeploymentLifecycleRequest struct {
	ReleaseRef             string `json:"release_ref"`
	PublicModelID          int64  `json:"public_model_id"`
	ExpectedReleaseVersion int64  `json:"expected_release_version"`
	ExpectedSlotVersion    int64  `json:"expected_slot_version"`
	ActorUserID            int64  `json:"actor_user_id"`
	ActorOrgID             int64  `json:"actor_org_id,omitempty"`
	CustomerOrgID          int64  `json:"customer_org_id"`
	LifecycleReceiptID     int64  `json:"-"`
}

type DeploymentAssignment struct {
	ContractVersion string            `json:"contract_version"`
	Identity        string            `json:"identity"`
	ReleaseRef      string            `json:"release_ref"`
	Stage           string            `json:"stage"`
	PublicModelID   int64             `json:"public_model_id"`
	ModelName       string            `json:"model_name"`
	RouteItemsHash  string            `json:"route_items_hash"`
	Routes          []DeploymentRoute `json:"routes"`
}

type DraftRequest struct {
	ReleaseRef string                          `json:"release_ref"`
	ModelName  string                          `json:"model_name"`
	Overrides  []routingpolicy.ChannelOverride `json:"overrides"`
	Reason     string                          `json:"reason"`
}

type TransitionRequest struct {
	ExpectedVersion   int64  `json:"expected_version"`
	CanaryBasisPoints int    `json:"canary_basis_points,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

type Release struct {
	ID                    int64                                `json:"id"`
	ReleaseRef            string                               `json:"release_ref"`
	ModelName             string                               `json:"model_name"`
	CandidateVersion      string                               `json:"candidate_version"`
	CandidateHash         string                               `json:"candidate_hash"`
	BaselineHash          string                               `json:"baseline_hash"`
	Candidate             Candidate                            `json:"candidate"`
	ProviderPackPins      []governancesim.ProviderPackSnapshot `json:"provider_pack_pins"`
	SimulationReport      governancesim.Report                 `json:"simulation_report"`
	OverallRisk           string                               `json:"overall_risk"`
	Stage                 string                               `json:"stage"`
	Version               int64                                `json:"version"`
	ParentStableReleaseID *int64                               `json:"parent_stable_release_id,omitempty"`
	CanaryBasisPoints     int                                  `json:"canary_basis_points"`
	Reason                string                               `json:"reason"`
	CreatedBy             int64                                `json:"created_by"`
	ValidatedBy           *int64                               `json:"validated_by,omitempty"`
	ActivatedBy           *int64                               `json:"activated_by,omitempty"`
	CreatedAt             time.Time                            `json:"created_at"`
	ValidatedAt           *time.Time                           `json:"validated_at,omitempty"`
	ActivatedAt           *time.Time                           `json:"activated_at,omitempty"`
	RolledBackAt          *time.Time                           `json:"rolled_back_at,omitempty"`
	UpdatedAt             time.Time                            `json:"updated_at"`
}

type ReleaseListSummary struct {
	ActiveRecords  int `json:"active_records"`
	CurrentStable  int `json:"current_stable"`
	RetainedActive int `json:"retained_active"`
	Canary         int `json:"canary"`
	Pending        int `json:"pending"`
	History        int `json:"history"`
}

type ReleasePage struct {
	Data     []Release          `json:"data"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Summary  ReleaseListSummary `json:"summary"`
}

type Slot struct {
	ModelName         string `json:"model_name"`
	StableReleaseRef  string `json:"stable_release_ref,omitempty"`
	CanaryReleaseRef  string `json:"canary_release_ref,omitempty"`
	CanaryBasisPoints int    `json:"canary_basis_points"`
	SlotVersion       int64  `json:"slot_version"`
	RuntimeMutation   string `json:"runtime_mutation"`
}

type SnapshotBuilder interface {
	BuildRuntimeChangeSnapshots(context.Context, string, []routingpolicy.ChannelOverride, []routingpolicy.ChannelOverride) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error)
}

func NormalizeDraft(request DraftRequest) (DraftRequest, error) {
	request.ReleaseRef = strings.TrimSpace(strings.ToLower(request.ReleaseRef))
	request.ModelName = strings.TrimSpace(request.ModelName)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.ReleaseRef != "" && !releaseRefPattern.MatchString(request.ReleaseRef) {
		return DraftRequest{}, fmt.Errorf("%w: release_ref", ErrInvalid)
	}
	if request.ModelName == "" || len(request.ModelName) > 160 || strings.ContainsAny(request.ModelName, "\x00\r\n") {
		return DraftRequest{}, fmt.Errorf("%w: model_name", ErrInvalid)
	}
	if request.Reason == "" || len(request.Reason) > 500 {
		return DraftRequest{}, fmt.Errorf("%w: reason", ErrInvalid)
	}
	overrides, err := routingpolicy.ValidateOverrides(request.Overrides)
	if err != nil {
		return DraftRequest{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	request.Overrides = overrides
	return request, nil
}

func IsUnavailable(err error) bool { return errors.Is(err, ErrUnavailable) }
