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

const CandidateVersion = "unirouter.runtime-change-candidate.v1"

var (
	ErrInvalid     = errors.New("runtimechange: invalid request")
	ErrConflict    = errors.New("runtimechange: revision conflict")
	ErrNotFound    = errors.New("runtimechange: release not found")
	ErrUnavailable = errors.New("runtimechange: state unavailable")

	releaseRefPattern = regexp.MustCompile(`^rcr_[a-z0-9][a-z0-9._-]{2,63}$`)
)

type Candidate struct {
	ContractVersion string                          `json:"contract_version"`
	ModelName       string                          `json:"model_name"`
	Overrides       []routingpolicy.ChannelOverride `json:"overrides"`
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
