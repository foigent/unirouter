package runtimechange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func (m *Manager) Validate(ctx context.Context, ref string, expectedVersion, actorUserID int64) (Release, error) {
	if expectedVersion <= 0 || actorUserID <= 0 {
		return Release{}, fmt.Errorf("%w: transition identity", ErrInvalid)
	}
	release, err := m.Get(ctx, ref)
	if err != nil {
		return Release{}, err
	}
	if release.Stage != "draft" || release.Version != expectedVersion {
		return Release{}, ErrConflict
	}
	baseline, parentID, err := m.stableCandidate(ctx, release.ModelName)
	if err != nil {
		return Release{}, err
	}
	if !sameOptionalID(parentID, release.ParentStableReleaseID) {
		return Release{}, fmt.Errorf("%w: stable baseline changed", ErrConflict)
	}
	if err := m.validateChannelPins(ctx, m.db, release.ModelName, release.Candidate.Overrides); err != nil {
		return Release{}, err
	}
	pins, report, baselineHash, candidateHash, err := m.evaluateCandidate(
		ctx, release.ReleaseRef, release.Reason, actorUserID, baseline, release.Candidate,
	)
	if err != nil {
		return Release{}, err
	}
	if report.OverallRisk == governancesim.RiskBreaking {
		return Release{}, fmt.Errorf("%w: governance simulation classified candidate breaking", ErrInvalid)
	}
	pinsJSON, _ := json.Marshal(pins)
	reportJSON, _ := json.Marshal(report)
	return m.runTransition(ctx, transition{
		Ref: ref, ExpectedVersion: expectedVersion, ActorUserID: actorUserID,
		From: "draft", To: "validated", Action: "validate",
		UpdateSQL: `validated_by=$4, validated_at=NOW(), candidate_hash=$5,
                    baseline_hash=$6, provider_pack_pins=$7::jsonb,
                    simulation_report=$8::jsonb, overall_risk=$9`,
		UpdateArgs: []interface{}{actorUserID, candidateHash, baselineHash, string(pinsJSON), string(reportJSON), report.OverallRisk},
	})
}

func (m *Manager) AdvanceShadow(ctx context.Context, ref string, expectedVersion, actorUserID int64) (Release, error) {
	return m.runTransition(ctx, transition{
		Ref: ref, ExpectedVersion: expectedVersion, ActorUserID: actorUserID,
		From: "validated", To: "shadow", Action: "advance",
	})
}

// Reject closes a release candidate that never received traffic. Rejected
// records remain immutable evidence but are not counted as work that can still
// be promoted. Canary and active releases must use Rollback instead.
func (m *Manager) Reject(ctx context.Context, ref string, expectedVersion, actorUserID int64, reason string) (Release, error) {
	if expectedVersion <= 0 || actorUserID <= 0 {
		return Release{}, fmt.Errorf("%w: transition identity", ErrInvalid)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 500 {
		return Release{}, fmt.Errorf("%w: rejection reason", ErrInvalid)
	}
	release, err := m.Get(ctx, ref)
	if err != nil {
		return Release{}, err
	}
	if release.Version != expectedVersion || (release.Stage != "draft" && release.Stage != "validated" && release.Stage != "shadow") {
		return Release{}, ErrConflict
	}
	return m.runTransition(ctx, transition{
		Ref: ref, ExpectedVersion: expectedVersion, ActorUserID: actorUserID,
		From: release.Stage, To: "rejected", Action: "reject", SkipPinValidation: true,
		Detail: map[string]interface{}{"reason": reason},
	})
}

// ActivateInitial promotes a validated candidate directly to the first stable
// assignment for a model. It is deliberately unavailable once any stable or
// canary assignment exists; replacements continue to use shadow and canary.
func (m *Manager) ActivateInitial(ctx context.Context, ref string, expectedVersion, actorUserID, customerOrgID int64) (Release, error) {
	if customerOrgID <= 0 {
		return Release{}, fmt.Errorf("%w: customer organization", ErrInvalid)
	}
	return m.runTransition(ctx, transition{
		Ref: ref, ExpectedVersion: expectedVersion, ActorUserID: actorUserID,
		From: "validated", To: "active", Action: "promote", InitialActivation: true,
		UpdateSQL:  `activated_by=$4, activated_at=NOW(), canary_basis_points=0`,
		UpdateArgs: []interface{}{actorUserID},
		SlotAction: func(ctx context.Context, tx pgx.Tx, release Release) error {
			if release.ParentStableReleaseID != nil {
				return fmt.Errorf("%w: initial activation requires no stable baseline", ErrConflict)
			}
			var stableID, canaryID *int64
			err := tx.QueryRow(ctx, `
SELECT stable_release_id, canary_release_id
  FROM runtime_change_slots
 WHERE model_name=$1
 FOR UPDATE`, release.ModelName).Scan(&stableID, &canaryID)
			switch {
			case err == pgx.ErrNoRows:
				tag, insertErr := tx.Exec(ctx, `
INSERT INTO runtime_change_slots (
    model_name, stable_release_id, canary_release_id, canary_basis_points, slot_version)
VALUES ($1,$2,NULL,0,1)
ON CONFLICT (model_name) DO NOTHING`, release.ModelName, release.ID)
				if insertErr != nil {
					return insertErr
				}
				if tag.RowsAffected() != 1 {
					return fmt.Errorf("%w: model assignment appeared concurrently", ErrConflict)
				}
			case err != nil:
				return fmt.Errorf("%w: lock runtime slot: %v", ErrUnavailable, err)
			case stableID != nil || canaryID != nil:
				return fmt.Errorf("%w: model already has a runtime assignment", ErrConflict)
			default:
				tag, updateErr := tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=$1, canary_release_id=NULL, canary_basis_points=0,
       slot_version=slot_version+1, updated_at=NOW()
 WHERE model_name=$2 AND stable_release_id IS NULL AND canary_release_id IS NULL`, release.ID, release.ModelName)
				if updateErr != nil {
					return updateErr
				}
				if tag.RowsAffected() != 1 {
					return ErrConflict
				}
			}

			providerFamily, providerErr := activatedProviderFamily(ctx, tx, release)
			if providerErr != nil {
				return providerErr
			}
			if catalogErr := modelgov.EnsureActivatedCatalogModel(
				ctx, tx, customerOrgID, release.ModelName, providerFamily,
			); catalogErr != nil {
				if errors.Is(catalogErr, modelgov.ErrModelRetired) {
					return fmt.Errorf("%w: activated model is retired", ErrConflict)
				}
				if errors.Is(catalogErr, modelgov.ErrModelIdentityConflict) {
					return fmt.Errorf("%w: activated model identity conflicts with an existing catalog record: %w", ErrConflict, catalogErr)
				}
				return fmt.Errorf("%w: activated model catalog: %v", ErrUnavailable, catalogErr)
			}
			return nil
		},
	})
}

// ReplaceUnavailableStable atomically rolls back a retained stable release and
// promotes its already-validated child. The caller must independently prove
// that the old assignment has no gateway candidates; this method guarantees
// that a failure never leaves the slot half-replaced.
func (m *Manager) ReplaceUnavailableStable(
	ctx context.Context,
	candidateRef string,
	candidateExpectedVersion int64,
	retainedRef string,
	retainedExpectedVersion int64,
	actorUserID int64,
	customerOrgID int64,
) (Release, error) {
	if candidateExpectedVersion <= 0 || retainedExpectedVersion <= 0 || actorUserID <= 0 || customerOrgID <= 0 {
		return Release{}, fmt.Errorf("%w: unavailable stable replacement identity", ErrInvalid)
	}
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return Release{}, fmt.Errorf("%w: begin unavailable stable replacement: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)

	candidate, err := scanRelease(tx.QueryRow(ctx, `
SELECT id, release_ref, model_name, candidate_version, candidate_hash, baseline_hash,
       candidate, provider_pack_pins, simulation_report, overall_risk, stage, version,
       parent_stable_release_id, canary_basis_points, reason, created_by, validated_by,
       activated_by, created_at, validated_at, activated_at, rolled_back_at, updated_at
  FROM runtime_change_releases WHERE release_ref=$1 FOR UPDATE`, candidateRef))
	if err != nil {
		return Release{}, err
	}
	if candidate.Stage != "validated" || candidate.Version != candidateExpectedVersion || candidate.ParentStableReleaseID == nil {
		return Release{}, ErrConflict
	}

	retained, err := scanRelease(tx.QueryRow(ctx, `
SELECT id, release_ref, model_name, candidate_version, candidate_hash, baseline_hash,
       candidate, provider_pack_pins, simulation_report, overall_risk, stage, version,
       parent_stable_release_id, canary_basis_points, reason, created_by, validated_by,
       activated_by, created_at, validated_at, activated_at, rolled_back_at, updated_at
  FROM runtime_change_releases WHERE release_ref=$1 FOR UPDATE`, retainedRef))
	if err != nil {
		return Release{}, err
	}
	if retained.Stage != "active" || retained.Version != retainedExpectedVersion ||
		retained.ID != *candidate.ParentStableReleaseID || retained.ModelName != candidate.ModelName {
		return Release{}, ErrConflict
	}
	if err := m.validateChannelPins(ctx, tx, candidate.ModelName, candidate.Candidate.Overrides); err != nil {
		return Release{}, err
	}

	var stableID, canaryID *int64
	if err := tx.QueryRow(ctx, `
SELECT stable_release_id,canary_release_id
  FROM runtime_change_slots
 WHERE model_name=$1
 FOR UPDATE`, candidate.ModelName).Scan(&stableID, &canaryID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Release{}, ErrConflict
		}
		return Release{}, fmt.Errorf("%w: lock unavailable stable slot: %v", ErrUnavailable, err)
	}
	if stableID == nil || *stableID != retained.ID || canaryID != nil {
		return Release{}, ErrConflict
	}

	providerFamily, err := activatedProviderFamily(ctx, tx, candidate)
	if err != nil {
		return Release{}, err
	}
	if catalogErr := modelgov.EnsureActivatedCatalogModel(ctx, tx, customerOrgID, candidate.ModelName, providerFamily); catalogErr != nil {
		if errors.Is(catalogErr, modelgov.ErrModelRetired) {
			return Release{}, fmt.Errorf("%w: activated model is retired", ErrConflict)
		}
		if errors.Is(catalogErr, modelgov.ErrModelIdentityConflict) {
			return Release{}, fmt.Errorf("%w: activated model identity conflicts with an existing catalog record: %w", ErrConflict, catalogErr)
		}
		return Release{}, fmt.Errorf("%w: activated model catalog: %v", ErrUnavailable, catalogErr)
	}

	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=$1,canary_release_id=NULL,canary_basis_points=0,
       slot_version=slot_version+1,updated_at=NOW()
 WHERE model_name=$2 AND stable_release_id=$3 AND canary_release_id IS NULL`,
		candidate.ID, candidate.ModelName, retained.ID)
	if err != nil {
		return Release{}, fmt.Errorf("%w: replace unavailable stable slot: %v", ErrUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Release{}, ErrConflict
	}

	tag, err = tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='rolled_back',version=version+1,canary_basis_points=0,
       rolled_back_at=NOW(),updated_at=NOW()
 WHERE id=$1 AND stage='active' AND version=$2`, retained.ID, retainedExpectedVersion)
	if err != nil {
		return Release{}, fmt.Errorf("%w: close unavailable stable release: %v", ErrUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Release{}, ErrConflict
	}

	tag, err = tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='active',version=version+1,activated_by=$3,activated_at=NOW(),
       canary_basis_points=0,updated_at=NOW()
 WHERE id=$1 AND stage='validated' AND version=$2`, candidate.ID, candidateExpectedVersion, actorUserID)
	if err != nil {
		return Release{}, fmt.Errorf("%w: activate unavailable stable replacement: %v", ErrUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Release{}, ErrConflict
	}

	eventTime := m.now().UTC().Truncate(time.Microsecond)
	rollbackDetail, _ := json.Marshal(map[string]interface{}{
		"runtime_mutation": "bounded_route_assignment", "unavailable_route_recovery": true,
		"replaced_by_release_ref": candidate.ReleaseRef,
	})
	if _, err := tx.Exec(ctx, `
INSERT INTO runtime_change_events (
    release_id,sequence,action,from_stage,to_stage,actor_user_id,
    expected_version,resulting_version,detail,occurred_at)
VALUES ($1,$2,'rollback','active','rolled_back',$3,$4,$5,$6::jsonb,$7)`,
		retained.ID, retainedExpectedVersion+1, actorUserID, retainedExpectedVersion,
		retainedExpectedVersion+1, string(rollbackDetail), eventTime); err != nil {
		return Release{}, fmt.Errorf("%w: append unavailable stable rollback event: %v", ErrUnavailable, err)
	}
	promoteDetail, _ := json.Marshal(map[string]interface{}{
		"runtime_mutation": "bounded_route_assignment", "unavailable_route_recovery": true,
		"replaced_release_ref": retained.ReleaseRef,
	})
	if _, err := tx.Exec(ctx, `
INSERT INTO runtime_change_events (
    release_id,sequence,action,from_stage,to_stage,actor_user_id,
    expected_version,resulting_version,detail,occurred_at)
VALUES ($1,$2,'promote','validated','active',$3,$4,$5,$6::jsonb,$7)`,
		candidate.ID, candidateExpectedVersion+1, actorUserID, candidateExpectedVersion,
		candidateExpectedVersion+1, string(promoteDetail), eventTime); err != nil {
		return Release{}, fmt.Errorf("%w: append unavailable stable promotion event: %v", ErrUnavailable, err)
	}
	activated, err := scanRelease(tx.QueryRow(ctx, `
SELECT id, release_ref, model_name, candidate_version, candidate_hash, baseline_hash,
       candidate, provider_pack_pins, simulation_report, overall_risk, stage, version,
       parent_stable_release_id, canary_basis_points, reason, created_by, validated_by,
       activated_by, created_at, validated_at, activated_at, rolled_back_at, updated_at
  FROM runtime_change_releases WHERE id=$1`, candidate.ID))
	if err != nil {
		return Release{}, fmt.Errorf("%w: read unavailable stable replacement before commit: %v", ErrUnavailable, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Release{}, fmt.Errorf("%w: commit unavailable stable replacement: %v", ErrUnavailable, err)
	}
	m.Invalidate()
	return activated, nil
}

func activatedProviderFamily(ctx context.Context, tx pgx.Tx, release Release) (string, error) {
	channelIDs := make([]int64, 0, len(release.Candidate.Overrides))
	for _, override := range release.Candidate.Overrides {
		if override.Admission == routingpolicy.AdmissionAllow {
			channelIDs = append(channelIDs, override.ChannelID)
		}
	}
	if len(channelIDs) == 0 {
		return "", fmt.Errorf("%w: initial activation has no admitted channel", ErrInvalid)
	}

	var providerFamily string
	if err := tx.QueryRow(ctx, `
		SELECT CASE
		         WHEN COUNT(DISTINCT provider) = 1 THEN MIN(provider)
		         ELSE 'mixed'
		       END
		  FROM channels
		 WHERE id = ANY($1::bigint[])`, channelIDs).Scan(&providerFamily); err != nil {
		return "", fmt.Errorf("%w: read activated provider family: %v", ErrUnavailable, err)
	}
	if providerFamily == "" {
		return "", fmt.Errorf("%w: activated provider family is unavailable", ErrUnavailable)
	}
	return providerFamily, nil
}

func (m *Manager) StartCanary(ctx context.Context, ref string, expectedVersion int64, basisPoints int, actorUserID int64) (Release, error) {
	if basisPoints < 1 || basisPoints > 5000 {
		return Release{}, fmt.Errorf("%w: canary_basis_points", ErrInvalid)
	}
	return m.runTransition(ctx, transition{
		Ref: ref, ExpectedVersion: expectedVersion, ActorUserID: actorUserID,
		From: "shadow", To: "canary", Action: "advance", CanaryBasisPoints: basisPoints,
		UpdateSQL:  `activated_by=$4, activated_at=NOW(), canary_basis_points=$5`,
		UpdateArgs: []interface{}{actorUserID, basisPoints},
		SlotAction: func(ctx context.Context, tx pgx.Tx, release Release) error {
			var stableID *int64
			lookupErr := tx.QueryRow(ctx, `SELECT stable_release_id FROM runtime_change_slots WHERE model_name=$1 FOR UPDATE`, release.ModelName).Scan(&stableID)
			if lookupErr != nil && lookupErr != pgx.ErrNoRows {
				return fmt.Errorf("%w: lock runtime slot: %v", ErrUnavailable, lookupErr)
			}
			if !sameOptionalID(stableID, release.ParentStableReleaseID) {
				return fmt.Errorf("%w: stable baseline changed", ErrConflict)
			}
			tag, err := tx.Exec(ctx, `
INSERT INTO runtime_change_slots (model_name, stable_release_id, canary_release_id, canary_basis_points, slot_version)
VALUES ($1,$2,$3,$4,1)
ON CONFLICT (model_name) DO UPDATE SET
    canary_release_id=EXCLUDED.canary_release_id,
    canary_basis_points=EXCLUDED.canary_basis_points,
    slot_version=runtime_change_slots.slot_version+1,
    updated_at=NOW()
WHERE runtime_change_slots.canary_release_id IS NULL`, release.ModelName, release.ParentStableReleaseID, release.ID, basisPoints)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("%w: another canary is active", ErrConflict)
			}
			return nil
		},
	})
}

func (m *Manager) Promote(ctx context.Context, ref string, expectedVersion, actorUserID int64) (Release, error) {
	return m.runTransition(ctx, transition{
		Ref: ref, ExpectedVersion: expectedVersion, ActorUserID: actorUserID,
		From: "canary", To: "active", Action: "promote",
		UpdateSQL: `canary_basis_points=0`,
		SlotAction: func(ctx context.Context, tx pgx.Tx, release Release) error {
			tag, err := tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=$1, canary_release_id=NULL, canary_basis_points=0,
       slot_version=slot_version+1, updated_at=NOW()
 WHERE model_name=$2 AND canary_release_id=$1`, release.ID, release.ModelName)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrConflict
			}
			return nil
		},
	})
}

func (m *Manager) Rollback(ctx context.Context, ref string, expectedVersion, actorUserID int64) (Release, error) {
	if expectedVersion <= 0 || actorUserID <= 0 {
		return Release{}, fmt.Errorf("%w: transition identity", ErrInvalid)
	}
	release, err := m.Get(ctx, ref)
	if err != nil {
		return Release{}, err
	}
	if release.Version != expectedVersion || (release.Stage != "canary" && release.Stage != "active") {
		return Release{}, ErrConflict
	}
	return m.runTransition(ctx, transition{
		Ref: ref, ExpectedVersion: expectedVersion, ActorUserID: actorUserID,
		From: release.Stage, To: "rolled_back", Action: "rollback",
		UpdateSQL: `canary_basis_points=0, rolled_back_at=NOW()`, SkipPinValidation: true,
		SlotAction: func(ctx context.Context, tx pgx.Tx, locked Release) error {
			if locked.Stage == "canary" {
				tag, execErr := tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET canary_release_id=NULL, canary_basis_points=0,
       slot_version=slot_version+1, updated_at=NOW()
 WHERE model_name=$1 AND canary_release_id=$2`, locked.ModelName, locked.ID)
				if execErr != nil || tag.RowsAffected() != 1 {
					if execErr != nil {
						return execErr
					}
					return ErrConflict
				}
				return nil
			}
			tag, execErr := tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=$1, canary_release_id=NULL, canary_basis_points=0,
       slot_version=slot_version+1, updated_at=NOW()
 WHERE model_name=$2 AND stable_release_id=$3 AND canary_release_id IS NULL`, locked.ParentStableReleaseID, locked.ModelName, locked.ID)
			if execErr != nil || tag.RowsAffected() != 1 {
				if execErr != nil {
					return execErr
				}
				return ErrConflict
			}
			return nil
		},
	})
}

type transition struct {
	Ref               string
	ExpectedVersion   int64
	ActorUserID       int64
	From              string
	To                string
	Action            string
	CanaryBasisPoints int
	UpdateSQL         string
	UpdateArgs        []interface{}
	SlotAction        func(context.Context, pgx.Tx, Release) error
	SkipPinValidation bool
	InitialActivation bool
	Detail            map[string]interface{}
}

func (m *Manager) runTransition(ctx context.Context, change transition) (Release, error) {
	if change.ExpectedVersion <= 0 || change.ActorUserID <= 0 {
		return Release{}, fmt.Errorf("%w: transition identity", ErrInvalid)
	}
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return Release{}, fmt.Errorf("%w: begin transition: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	locked, err := scanRelease(tx.QueryRow(ctx, `
SELECT id, release_ref, model_name, candidate_version, candidate_hash, baseline_hash,
       candidate, provider_pack_pins, simulation_report, overall_risk, stage, version,
       parent_stable_release_id, canary_basis_points, reason, created_by, validated_by,
       activated_by, created_at, validated_at, activated_at, rolled_back_at, updated_at
  FROM runtime_change_releases WHERE release_ref=$1 FOR UPDATE`, change.Ref))
	if err != nil {
		return Release{}, err
	}
	if locked.Stage != change.From || locked.Version != change.ExpectedVersion {
		return Release{}, ErrConflict
	}
	if !change.SkipPinValidation {
		if err := m.validateChannelPins(ctx, tx, locked.ModelName, locked.Candidate.Overrides); err != nil {
			return Release{}, err
		}
	}
	if change.SlotAction != nil {
		if err := change.SlotAction(ctx, tx, locked); err != nil {
			return Release{}, err
		}
	}
	query := `UPDATE runtime_change_releases SET stage=$1, version=version+1, updated_at=NOW()`
	args := []interface{}{change.To, locked.ID, change.ExpectedVersion}
	if change.UpdateSQL != "" {
		query += ", " + change.UpdateSQL
		args = append(args, change.UpdateArgs...)
	}
	query += ` WHERE id=$2 AND version=$3`
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return Release{}, fmt.Errorf("%w: update transition: %v", ErrUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return Release{}, ErrConflict
	}
	detailValue := map[string]interface{}{
		"canary_basis_points": change.CanaryBasisPoints,
		"runtime_mutation":    runtimeMutation(change.To),
	}
	for key, value := range change.Detail {
		detailValue[key] = value
	}
	if change.InitialActivation {
		detailValue["initial_activation"] = true
	}
	detail, _ := json.Marshal(detailValue)
	_, err = tx.Exec(ctx, `
INSERT INTO runtime_change_events (
    release_id, sequence, action, from_stage, to_stage, actor_user_id,
    expected_version, resulting_version, detail, occurred_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)`,
		locked.ID, change.ExpectedVersion+1, change.Action, change.From, change.To,
		change.ActorUserID, change.ExpectedVersion, change.ExpectedVersion+1,
		string(detail), m.now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		return Release{}, fmt.Errorf("%w: append transition event: %v", ErrUnavailable, err)
	}
	transitioned, err := scanRelease(tx.QueryRow(ctx, `
SELECT id, release_ref, model_name, candidate_version, candidate_hash, baseline_hash,
       candidate, provider_pack_pins, simulation_report, overall_risk, stage, version,
       parent_stable_release_id, canary_basis_points, reason, created_by, validated_by,
       activated_by, created_at, validated_at, activated_at, rolled_back_at, updated_at
  FROM runtime_change_releases WHERE id=$1`, locked.ID))
	if err != nil {
		return Release{}, fmt.Errorf("%w: read transition before commit: %v", ErrUnavailable, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Release{}, fmt.Errorf("%w: commit transition: %v", ErrUnavailable, err)
	}
	m.Invalidate()
	return transitioned, nil
}

func sameOptionalID(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func runtimeMutation(stage string) string {
	switch stage {
	case "canary", "active", "rolled_back":
		return "bounded_route_assignment"
	default:
		return "none"
	}
}
