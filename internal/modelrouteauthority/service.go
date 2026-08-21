// Package modelrouteauthority owns the explicit operator workflow that
// reconciles legacy projections, seals compatibility manifests and proves the
// selected startup route authority. It never changes the process authority
// mode or publishes/cuts over a runtime slot.
package modelrouteauthority

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
)

const ApplyContractVersion = "unirouter.model-route-authority-apply.v1"

const SafetyControlContractVersion = "unirouter.model-route-safety-control.v1"

var (
	ErrApplyIncomplete = errors.New("modelrouteauthority: apply incomplete")
	ErrApplyMode       = errors.New("modelrouteauthority: apply is only valid for verify preparation; use check for legacy or canonical authority")
	ErrApplyPhase      = errors.New("modelrouteauthority: apply requires current v1 authority; use read-only check after v2 assignment")
)

type Database interface {
	db.DBQuerier
	modelrouting.TxStarter
}

type Service struct {
	database Database
	policy   routingpolicy.Config
}

type ApplyRequest struct {
	Mode          modelrouting.Mode `json:"mode"`
	CustomerOrgID int64             `json:"customer_org_id"`
}

type ApplyReport struct {
	ContractVersion string                                      `json:"contract_version"`
	Mode            modelrouting.Mode                           `json:"mode"`
	CustomerOrgID   int64                                       `json:"customer_org_id"`
	GeneratedAt     time.Time                                   `json:"generated_at"`
	Ready           bool                                        `json:"ready"`
	Reconciliation  modeldeployment.Report                      `json:"reconciliation"`
	Sealed          []runtimechange.LegacyCompatibilityManifest `json:"sealed"`
	AlreadySealed   int                                         `json:"already_sealed"`
	Authority       modelrouting.AuthorityCheckReport           `json:"authority"`
}

type legacySealCandidate struct {
	ReleaseRef      string
	ReleaseVersion  int64
	PublicModelID   int64
	StoredModelID   *int64
	RouteItemsHash  *string
	RouteItemsCount *int
	SealedAt        *time.Time
}

func New(database Database, policy routingpolicy.Config) (*Service, error) {
	if database == nil {
		return nil, fmt.Errorf("modelrouteauthority: database is required")
	}
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("modelrouteauthority: routing policy: %w", err)
	}
	return &Service{database: database, policy: policy}, nil
}

func (s *Service) Check(ctx context.Context, request modelrouting.AuthorityCheckRequest) (modelrouting.AuthorityCheckReport, error) {
	if s == nil || s.database == nil {
		return modelrouting.AuthorityCheckReport{}, fmt.Errorf("modelrouteauthority: service unavailable")
	}
	report, err := modelrouting.CheckAuthority(ctx, s.database, s.policy, request)
	report.ControlPlaneContract = SafetyControlContractVersion
	report.ControlPlaneReady = true
	report.Ready = report.DatabaseReady && report.ControlPlaneReady
	return report, err
}

// Apply is intentionally explicit and idempotent. Reconciliation may commit a
// safe partial projection and return ErrIncomplete; in that case no release is
// sealed and the non-ready report remains operator-visible.
func (s *Service) Apply(ctx context.Context, request ApplyRequest) (ApplyReport, error) {
	report := ApplyReport{
		ContractVersion: ApplyContractVersion,
		Mode:            request.Mode, CustomerOrgID: request.CustomerOrgID,
		GeneratedAt: time.Now().UTC(),
		Sealed:      make([]runtimechange.LegacyCompatibilityManifest, 0),
	}
	if s == nil || s.database == nil || request.CustomerOrgID <= 0 {
		return report, fmt.Errorf("modelrouteauthority: database and positive customer org ID are required")
	}
	if _, err := modelrouting.ParseMode(string(request.Mode)); err != nil {
		return report, err
	}
	// Reconciliation projects legacy state into canonical rows and therefore
	// has exactly one valid direction: preparing verify mode. Once canonical is
	// selected, legacy rows are no longer an input authority; an apply there
	// would be a reverse writer capable of undoing canonical pause/revoke/config.
	if request.Mode != modelrouting.ModeVerify {
		return report, ErrApplyMode
	}
	reconciliation, err := modeldeployment.ReconcileLegacyProjection(ctx, s.database, request.CustomerOrgID)
	report.Reconciliation = reconciliation
	if err != nil {
		if errors.Is(err, modeldeployment.ErrCanonicalAuthority) {
			return report, fmt.Errorf("%w: %v", ErrApplyPhase, err)
		}
		if errors.Is(err, modeldeployment.ErrIncomplete) {
			return report, fmt.Errorf("%w: %v", ErrApplyIncomplete, err)
		}
		return report, err
	}

	candidates, err := s.legacySealCandidates(ctx, request.CustomerOrgID)
	if err != nil {
		return report, err
	}
	for _, candidate := range candidates {
		if legacyCandidateAlreadySealed(candidate) {
			report.AlreadySealed++
			continue
		}
		sealed, sealErr := runtimechange.PrepareLegacyCompatibilitySeal(
			ctx, s.database, candidate.ReleaseRef, candidate.ReleaseVersion,
			request.CustomerOrgID, candidate.PublicModelID,
		)
		if sealErr != nil {
			// A concurrent identical seal is an idempotent success only after an
			// exact database re-read. Every other conflict remains fail-closed.
			if errors.Is(sealErr, runtimechange.ErrConflict) {
				current, readErr := s.readLegacySealCandidate(ctx, candidate.ReleaseRef, request.CustomerOrgID)
				if readErr == nil && legacyCandidateAlreadySealed(current) {
					report.AlreadySealed++
					continue
				}
			}
			return report, fmt.Errorf("modelrouteauthority: seal %s: %w", candidate.ReleaseRef, sealErr)
		}
		report.Sealed = append(report.Sealed, sealed)
	}

	authority, err := s.Check(ctx, modelrouting.AuthorityCheckRequest{
		Mode: request.Mode, CustomerOrgID: request.CustomerOrgID,
	})
	report.Authority = authority
	report.Ready = err == nil && authority.Ready
	if err != nil {
		if errors.Is(err, modelrouting.ErrAuthorityNotReady) {
			return report, fmt.Errorf("%w: %v", ErrApplyIncomplete, err)
		}
		return report, err
	}
	return report, nil
}

func (s *Service) legacySealCandidates(ctx context.Context, customerOrgID int64) ([]legacySealCandidate, error) {
	rows, err := s.database.Query(ctx, `
SELECT release.release_ref,release.version,registry.id,
       release.public_model_id,release.route_items_hash,
       release.route_items_count,release.route_items_sealed_at
  FROM runtime_change_slots slot
 CROSS JOIN LATERAL (
       VALUES (slot.stable_release_id,'active'::varchar,1),
              (slot.canary_release_id,'canary'::varchar,2)
 ) current_release(release_id,expected_stage,ordinal)
  JOIN runtime_change_releases release
    ON release.id=current_release.release_id
   AND release.stage=current_release.expected_stage
  JOIN model_registry registry
    ON registry.org_id=$1 AND registry.model_name=slot.model_name
 WHERE release.candidate_version='unirouter.runtime-change-candidate.v1'
 ORDER BY slot.model_name,current_release.ordinal`, customerOrgID)
	if err != nil {
		return nil, fmt.Errorf("modelrouteauthority: read legacy seal candidates: %w", err)
	}
	defer rows.Close()
	result := make([]legacySealCandidate, 0)
	for rows.Next() {
		var candidate legacySealCandidate
		if err := rows.Scan(
			&candidate.ReleaseRef, &candidate.ReleaseVersion, &candidate.PublicModelID,
			&candidate.StoredModelID, &candidate.RouteItemsHash,
			&candidate.RouteItemsCount, &candidate.SealedAt,
		); err != nil {
			return nil, fmt.Errorf("modelrouteauthority: scan legacy seal candidate: %w", err)
		}
		result = append(result, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("modelrouteauthority: iterate legacy seal candidates: %w", err)
	}
	return result, nil
}

func (s *Service) readLegacySealCandidate(ctx context.Context, releaseRef string, customerOrgID int64) (legacySealCandidate, error) {
	var candidate legacySealCandidate
	err := s.database.QueryRow(ctx, `
SELECT release.release_ref,release.version,registry.id,
       release.public_model_id,release.route_items_hash,
       release.route_items_count,release.route_items_sealed_at
  FROM runtime_change_releases release
  JOIN model_registry registry
    ON registry.org_id=$2 AND registry.model_name=release.model_name
 WHERE release.release_ref=$1
   AND release.candidate_version='unirouter.runtime-change-candidate.v1'`,
		releaseRef, customerOrgID,
	).Scan(
		&candidate.ReleaseRef, &candidate.ReleaseVersion, &candidate.PublicModelID,
		&candidate.StoredModelID, &candidate.RouteItemsHash,
		&candidate.RouteItemsCount, &candidate.SealedAt,
	)
	if err != nil {
		return legacySealCandidate{}, fmt.Errorf("modelrouteauthority: read sealed release: %w", err)
	}
	return candidate, nil
}

func legacyCandidateAlreadySealed(candidate legacySealCandidate) bool {
	return candidate.StoredModelID != nil && *candidate.StoredModelID == candidate.PublicModelID &&
		candidate.RouteItemsHash != nil && *candidate.RouteItemsHash != "" &&
		candidate.RouteItemsCount != nil && *candidate.RouteItemsCount > 0 &&
		candidate.SealedAt != nil
}
