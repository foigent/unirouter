package runtimechange

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type legacyLifecycleRelease struct {
	ID              int64
	ReleaseRef      string
	PublicModelID   int64
	ModelName       string
	CandidateHash   string
	RouteItemsHash  string
	RouteItemsCount int
	SealedAt        time.Time
	Candidate       Candidate
	Stage           string
	Version         int64
}

type rawLegacyLifecycleSlot struct {
	StableReleaseID   *int64
	CanaryReleaseID   *int64
	PublicModelID     *int64
	CanaryBasisPoints int
	Version           int64
}

type legacyLifecycleTransition struct {
	TargetBefore       DeploymentRelease
	TargetAfter        DeploymentRelease
	ParentBefore       legacyLifecycleRelease
	ParentAfterStage   string
	ParentAfterVersion int64
	Slot               DeploymentLifecycleSlotReceipt
	LegacyPlanHash     string
	DeploymentPlanHash string
}

func isLegacyDeploymentLifecycleAction(action DeploymentLifecycleAction) bool {
	return action == DeploymentLifecycleCutoverLegacy || action == DeploymentLifecycleRollbackLegacy
}

func (m *Manager) applyLegacyDeploymentLifecycleReceiptInTx(
	ctx context.Context,
	tx pgx.Tx,
	command DeploymentLifecycleCommand,
	claim deploymentLifecycleReceiptClaim,
) (DeploymentLifecycleReceipt, error) {
	transition, err := m.applyLegacyDeploymentLifecycleMutationInTx(ctx, tx, command, claim.ReceiptID)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	releases := []DeploymentLifecycleReleaseReceipt{
		deploymentLifecycleReleaseReceipt("target", transition.TargetBefore, transition.TargetAfter),
		{
			Role: "stable_parent", ReleaseID: transition.ParentBefore.ID,
			ReleaseRef:     transition.ParentBefore.ReleaseRef,
			CandidateHash:  transition.ParentBefore.CandidateHash,
			RouteItemsHash: transition.ParentBefore.RouteItemsHash,
			BeforeStage:    transition.ParentBefore.Stage, BeforeVersion: transition.ParentBefore.Version,
			AfterStage: transition.ParentAfterStage, AfterVersion: transition.ParentAfterVersion,
		},
	}
	qualifications, err := snapshotDeploymentLifecycleQualifications(
		ctx, tx, "target", transition.TargetAfter,
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	parentQualifications, err := snapshotDeploymentLifecycleQualifications(
		ctx, tx, "stable_parent", DeploymentRelease{
			ID: transition.ParentBefore.ID, RouteItemsCount: transition.ParentBefore.RouteItemsCount,
		},
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	qualifications = append(qualifications, parentQualifications...)
	if err := insertDeploymentLifecycleReleaseReceipts(ctx, tx, claim.ReceiptID, releases); err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	if err := insertDeploymentLifecycleQualificationReceipts(ctx, tx, claim.ReceiptID, qualifications); err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	auditID, err := insertDeploymentLifecycleAudit(
		ctx, tx, claim.ReceiptID, command, transition.TargetAfter, transition.Slot,
		releases, qualifications, transition.LegacyPlanHash, transition.DeploymentPlanHash,
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	completedAt, err := completeDeploymentLifecycleReceipt(
		ctx, tx, claim.ReceiptID, transition.TargetAfter, transition.Slot,
		transition.LegacyPlanHash, transition.DeploymentPlanHash, auditID,
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	return DeploymentLifecycleReceipt{
		ContractVersion: DeploymentLifecycleReceiptContractVersion,
		ReceiptID:       claim.ReceiptID, Action: command.Mutation.Action,
		TargetReleaseRef: transition.TargetAfter.ReleaseRef,
		CustomerOrgID:    command.CustomerOrgID, ActorUserID: command.ActorUserID,
		ServerOperationID: command.ServerOperationID, OriginalHTTPStatus: 200,
		Reason: command.Mutation.Reason, LegacyPlanHash: transition.LegacyPlanHash,
		DeploymentPlanHash: transition.DeploymentPlanHash, Slot: transition.Slot,
		Releases: releases, Qualifications: qualifications,
		CreatedAt: claim.CreatedAt.UTC(), CompletedAt: completedAt.UTC(),
	}, nil
}

func (m *Manager) applyLegacyDeploymentLifecycleMutationInTx(
	ctx context.Context,
	tx pgx.Tx,
	command DeploymentLifecycleCommand,
	receiptID int64,
) (legacyLifecycleTransition, error) {
	targetID, parentID, err := lockLegacyLifecycleReleasePair(
		ctx, tx, command.Mutation.ReleaseRef,
	)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	target, err := readDeploymentReleaseByID(ctx, tx, targetID, false)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	parent, err := readLegacyLifecycleRelease(ctx, tx, parentID)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	request := DeploymentLifecycleRequest{
		ReleaseRef: target.ReleaseRef, PublicModelID: target.PublicModelID,
		ExpectedReleaseVersion: command.Mutation.ExpectedReleaseVersion,
		ExpectedSlotVersion:    command.Mutation.ExpectedSlotVersion,
		ActorUserID:            command.ActorUserID, ActorOrgID: command.ActorOrgID,
		CustomerOrgID: command.CustomerOrgID, LifecycleReceiptID: receiptID,
	}
	if err := validateDeploymentLifecycleScope(ctx, tx, request, target.ModelName); err != nil {
		return legacyLifecycleTransition{}, err
	}
	if target.ParentStableReleaseID == nil || *target.ParentStableReleaseID != parent.ID ||
		target.PublicModelID != parent.PublicModelID || target.ModelName != parent.ModelName ||
		target.BaselineHash != parent.CandidateHash || target.CanaryBasisPoints != 0 {
		return legacyLifecycleTransition{}, fmt.Errorf("%w: legacy lifecycle release lineage", ErrConflict)
	}

	switch command.Mutation.Action {
	case DeploymentLifecycleCutoverLegacy:
		return m.cutoverLegacyDeploymentInTx(ctx, tx, request, target, parent)
	case DeploymentLifecycleRollbackLegacy:
		return m.rollbackLegacyDeploymentInTx(ctx, tx, request, target, parent)
	default:
		return legacyLifecycleTransition{}, fmt.Errorf("%w: legacy lifecycle action", ErrInvalid)
	}
}

func lockLegacyLifecycleReleasePair(
	ctx context.Context,
	tx pgx.Tx,
	targetRef string,
) (int64, int64, error) {
	var targetID int64
	var parentID sql.NullInt64
	if err := tx.QueryRow(ctx, `
SELECT id,parent_stable_release_id
  FROM runtime_change_releases
 WHERE release_ref=$1 AND candidate_version=$2`, targetRef, DeploymentCandidateVersion).Scan(
		&targetID, &parentID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, ErrNotFound
		}
		return 0, 0, lifecycleWriteError("read legacy lifecycle release pair", err)
	}
	if !parentID.Valid || parentID.Int64 <= 0 || parentID.Int64 == targetID {
		return 0, 0, fmt.Errorf("%w: legacy lifecycle parent", ErrConflict)
	}
	rows, err := tx.Query(ctx, `
SELECT id
  FROM runtime_change_releases
 WHERE id=ANY($1::bigint[])
 ORDER BY id
 FOR UPDATE`, []int64{targetID, parentID.Int64})
	if err != nil {
		return 0, 0, lifecycleWriteError("lock legacy lifecycle release pair", err)
	}
	defer rows.Close()
	locked := 0
	for rows.Next() {
		var ignored int64
		if err := rows.Scan(&ignored); err != nil {
			return 0, 0, lifecycleWriteError("scan legacy lifecycle release lock", err)
		}
		locked++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, lifecycleWriteError("iterate legacy lifecycle release locks", err)
	}
	if locked != 2 {
		return 0, 0, fmt.Errorf("%w: legacy lifecycle release pair changed", ErrConflict)
	}
	return targetID, parentID.Int64, nil
}

func readLegacyLifecycleRelease(
	ctx context.Context,
	tx pgx.Tx,
	releaseID int64,
) (legacyLifecycleRelease, error) {
	var result legacyLifecycleRelease
	var candidateVersion string
	var publicModelID sql.NullInt64
	var routeItemsHash sql.NullString
	var routeItemsCount sql.NullInt64
	var sealedAt sql.NullTime
	var candidateJSON []byte
	err := tx.QueryRow(ctx, `
SELECT id,release_ref,public_model_id,model_name,candidate_version,candidate_hash,
       route_items_hash,route_items_count,route_items_sealed_at,candidate,stage,version
  FROM runtime_change_releases
 WHERE id=$1`, releaseID).Scan(
		&result.ID, &result.ReleaseRef, &publicModelID, &result.ModelName,
		&candidateVersion, &result.CandidateHash, &routeItemsHash, &routeItemsCount,
		&sealedAt, &candidateJSON, &result.Stage, &result.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return legacyLifecycleRelease{}, ErrNotFound
	}
	if err != nil {
		return legacyLifecycleRelease{}, lifecycleWriteError("read legacy lifecycle parent", err)
	}
	if candidateVersion != CandidateVersion || !publicModelID.Valid || !routeItemsHash.Valid ||
		!routeItemsCount.Valid || routeItemsCount.Int64 <= 0 || !sealedAt.Valid ||
		!cutoverPlanHashPattern.MatchString(routeItemsHash.String) ||
		json.Unmarshal(candidateJSON, &result.Candidate) != nil ||
		result.Candidate.ContractVersion != CandidateVersion ||
		result.Candidate.ModelName != result.ModelName || hashJSON(result.Candidate) != result.CandidateHash {
		return legacyLifecycleRelease{}, fmt.Errorf("%w: sealed v1 lifecycle parent", ErrConflict)
	}
	result.PublicModelID = publicModelID.Int64
	result.RouteItemsHash = routeItemsHash.String
	result.RouteItemsCount = int(routeItemsCount.Int64)
	result.SealedAt = sealedAt.Time.UTC()
	return result, nil
}

func (m *Manager) cutoverLegacyDeploymentInTx(
	ctx context.Context,
	tx pgx.Tx,
	request DeploymentLifecycleRequest,
	target DeploymentRelease,
	parent legacyLifecycleRelease,
) (legacyLifecycleTransition, error) {
	if m.legacyCutoverVerifier == nil {
		return legacyLifecycleTransition{}, fmt.Errorf("%w: legacy cutover verifier", ErrUnavailable)
	}
	if target.Stage != "validated" || target.Version != request.ExpectedReleaseVersion ||
		target.RouteItemsSealedAt == nil || parent.Stage != "active" {
		return legacyLifecycleTransition{}, fmt.Errorf("%w: legacy cutover release state", ErrConflict)
	}
	if err := validateSealedDeploymentRelease(
		ctx, tx, target, "validated", request.ExpectedSlotVersion,
	); err != nil {
		return legacyLifecycleTransition{}, err
	}
	if err := lockDeploymentRouteAuthority(ctx, tx, target.Candidate.DeploymentRoutes); err != nil {
		return legacyLifecycleTransition{}, err
	}
	if err := validateDeploymentRoutes(
		ctx, tx, request.CustomerOrgID, request.PublicModelID, target.Candidate.DeploymentRoutes,
	); err != nil {
		return legacyLifecycleTransition{}, err
	}
	if err := m.validateAnyExecutableDeploymentRoute(
		ctx, tx, request.CustomerOrgID, request.PublicModelID, target,
	); err != nil {
		return legacyLifecycleTransition{}, err
	}
	beforeSlot, err := lockRawLegacyLifecycleSlot(ctx, tx, target.ModelName)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	if beforeSlot.Version != request.ExpectedSlotVersion || beforeSlot.PublicModelID != nil ||
		beforeSlot.StableReleaseID == nil || *beforeSlot.StableReleaseID != parent.ID ||
		beforeSlot.CanaryReleaseID != nil || beforeSlot.CanaryBasisPoints != 0 {
		return legacyLifecycleTransition{}, fmt.Errorf("%w: exact legacy stable slot changed", ErrConflict)
	}
	if err := verifyLegacyCompatibilityManifest(
		ctx, tx, parent.ID, parent.RouteItemsHash, parent.RouteItemsCount,
	); err != nil {
		return legacyLifecycleTransition{}, err
	}
	if err := lockLegacyRollbackAuthority(ctx, tx); err != nil {
		return legacyLifecycleTransition{}, err
	}
	if err := verifyLegacyDeploymentStructure(
		ctx, tx, request.CustomerOrgID, request.PublicModelID, target.ModelName,
		parent.Candidate.Overrides, target.Candidate.DeploymentRoutes,
	); err != nil {
		return legacyLifecycleTransition{}, err
	}
	legacyPlanHash, deploymentPlanHash, err := m.legacyCutoverVerifier.Verify(
		ctx, tx, request.CustomerOrgID, request.PublicModelID, parent.ID, target.ID,
	)
	if err != nil {
		return legacyLifecycleTransition{}, fmt.Errorf("%w: deployment cutover parity: %v", ErrConflict, err)
	}
	if !cutoverPlanHashPattern.MatchString(legacyPlanHash) ||
		legacyPlanHash != deploymentPlanHash {
		return legacyLifecycleTransition{}, fmt.Errorf("%w: deployment cutover plan hash mismatch", ErrConflict)
	}
	now, err := deploymentLifecycleNow(ctx, tx)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	if err := applyLegacyCutoverState(
		ctx, tx, request, target, parent, legacyPlanHash, deploymentPlanHash, now,
	); err != nil {
		return legacyLifecycleTransition{}, err
	}
	targetAfter, err := readDeploymentReleaseByID(ctx, tx, target.ID, false)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	afterSlot, err := readRawLegacyLifecycleSlot(ctx, tx, target.ModelName)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	return legacyLifecycleTransition{
		TargetBefore: target, TargetAfter: targetAfter, ParentBefore: parent,
		ParentAfterStage: "rolled_back", ParentAfterVersion: parent.Version + 1,
		Slot:           legacyLifecycleSlotReceipt(target.PublicModelID, target.ModelName, beforeSlot, afterSlot),
		LegacyPlanHash: legacyPlanHash, DeploymentPlanHash: deploymentPlanHash,
	}, nil
}

func (m *Manager) rollbackLegacyDeploymentInTx(
	ctx context.Context,
	tx pgx.Tx,
	request DeploymentLifecycleRequest,
	target DeploymentRelease,
	parent legacyLifecycleRelease,
) (legacyLifecycleTransition, error) {
	if target.Stage != "active" || target.Version != request.ExpectedReleaseVersion ||
		target.RouteItemsSealedAt == nil || parent.Stage != "rolled_back" {
		return legacyLifecycleTransition{}, fmt.Errorf("%w: legacy rollback release state", ErrConflict)
	}
	// Emergency contraction deliberately does not inspect target route health,
	// qualification or parity. Only target/slot identity and the sealed v1
	// parent's current executable authority can block restoring legacy traffic.
	beforeSlot, err := lockRawLegacyLifecycleSlot(ctx, tx, target.ModelName)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	if beforeSlot.Version != request.ExpectedSlotVersion || beforeSlot.PublicModelID == nil ||
		*beforeSlot.PublicModelID != target.PublicModelID || beforeSlot.StableReleaseID == nil ||
		*beforeSlot.StableReleaseID != target.ID || beforeSlot.CanaryReleaseID != nil ||
		beforeSlot.CanaryBasisPoints != 0 {
		return legacyLifecycleTransition{}, fmt.Errorf("%w: exact canonical stable slot changed", ErrConflict)
	}
	if err := verifyLegacyCompatibilityManifest(
		ctx, tx, parent.ID, parent.RouteItemsHash, parent.RouteItemsCount,
	); err != nil {
		return legacyLifecycleTransition{}, err
	}
	if err := lockLegacyRollbackAuthority(ctx, tx); err != nil {
		return legacyLifecycleTransition{}, err
	}
	legacyPlanHash, err := m.validateLegacyRollbackPlanWithHash(ctx, tx, parent.Candidate)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	if !cutoverPlanHashPattern.MatchString(legacyPlanHash) {
		return legacyLifecycleTransition{}, fmt.Errorf("%w: legacy rollback plan hash", ErrUnavailable)
	}
	now, err := deploymentLifecycleNow(ctx, tx)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	if err := applyLegacyRollbackState(ctx, tx, request, target, parent, legacyPlanHash, now); err != nil {
		return legacyLifecycleTransition{}, err
	}
	targetAfter, err := readDeploymentReleaseByID(ctx, tx, target.ID, false)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	afterSlot, err := readRawLegacyLifecycleSlot(ctx, tx, target.ModelName)
	if err != nil {
		return legacyLifecycleTransition{}, err
	}
	return legacyLifecycleTransition{
		TargetBefore: target, TargetAfter: targetAfter, ParentBefore: parent,
		ParentAfterStage: "active", ParentAfterVersion: parent.Version + 1,
		Slot:           legacyLifecycleSlotReceipt(target.PublicModelID, target.ModelName, beforeSlot, afterSlot),
		LegacyPlanHash: legacyPlanHash,
	}, nil
}

func applyLegacyCutoverState(
	ctx context.Context,
	tx pgx.Tx,
	request DeploymentLifecycleRequest,
	target DeploymentRelease,
	parent legacyLifecycleRelease,
	legacyPlanHash string,
	deploymentPlanHash string,
	now time.Time,
) error {
	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='rolled_back',version=version+1,canary_basis_points=0,
       rolled_back_at=$3,updated_at=$3
 WHERE id=$1 AND candidate_version=$4 AND stage='active' AND version=$2`,
		parent.ID, parent.Version, now, CandidateVersion)
	if err != nil {
		return lifecycleWriteError("close v1 cutover parent", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='active',version=version+1,activated_by=$3,activated_at=$4,
       canary_basis_points=0,updated_at=$4
 WHERE id=$1 AND candidate_version=$5 AND public_model_id=$6
   AND parent_stable_release_id=$7 AND stage='validated' AND version=$2`,
		target.ID, request.ExpectedReleaseVersion, request.ActorUserID, now,
		DeploymentCandidateVersion, target.PublicModelID, parent.ID)
	if err != nil {
		return lifecycleWriteError("activate v2 cutover release", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=$1,canary_release_id=NULL,canary_basis_points=0,
       public_model_id=$2,slot_version=slot_version+1,updated_at=$3
 WHERE model_name=$4 AND stable_release_id=$5 AND canary_release_id IS NULL
   AND canary_basis_points=0 AND public_model_id IS NULL AND slot_version=$6`,
		target.ID, target.PublicModelID, now, target.ModelName, parent.ID,
		request.ExpectedSlotVersion)
	if err != nil {
		return lifecycleWriteError("switch v2 stable slot", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	parentDetail := legacyLifecycleEventDetail(request, parent.RouteItemsHash, legacyPlanHash, deploymentPlanHash)
	parentDetail["canonical_cutover"] = true
	parentDetail["replaced_by_release_ref"] = target.ReleaseRef
	if err := appendDeploymentLifecycleEvent(
		ctx, tx, parent.ID, parent.Version+1, "rollback", "active", "rolled_back",
		request.ActorUserID, parent.Version, parentDetail, now,
	); err != nil {
		return err
	}
	targetDetail := legacyLifecycleEventDetail(request, target.RouteItemsHash, legacyPlanHash, deploymentPlanHash)
	targetDetail["canonical_cutover"] = true
	targetDetail["replaced_release_ref"] = parent.ReleaseRef
	return appendDeploymentLifecycleEvent(
		ctx, tx, target.ID, request.ExpectedReleaseVersion+1, "promote", "validated", "active",
		request.ActorUserID, request.ExpectedReleaseVersion, targetDetail, now,
	)
}

func applyLegacyRollbackState(
	ctx context.Context,
	tx pgx.Tx,
	request DeploymentLifecycleRequest,
	target DeploymentRelease,
	parent legacyLifecycleRelease,
	legacyPlanHash string,
	now time.Time,
) error {
	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='validated',version=version+1,activated_by=NULL,activated_at=NULL,
       canary_basis_points=0,updated_at=$3
 WHERE id=$1 AND candidate_version=$4 AND public_model_id=$5
   AND parent_stable_release_id=$6 AND stage='active' AND version=$2`,
		target.ID, request.ExpectedReleaseVersion, now, DeploymentCandidateVersion,
		target.PublicModelID, parent.ID)
	if err != nil {
		return lifecycleWriteError("close canonical stable release", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='active',version=version+1,rolled_back_at=NULL,updated_at=$3
 WHERE id=$1 AND candidate_version=$4 AND stage='rolled_back' AND version=$2`,
		parent.ID, parent.Version, now, CandidateVersion)
	if err != nil {
		return lifecycleWriteError("reactivate legacy stable release", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=$1,canary_release_id=NULL,canary_basis_points=0,
       public_model_id=NULL,slot_version=slot_version+1,updated_at=$2
 WHERE model_name=$3 AND stable_release_id=$4 AND canary_release_id IS NULL
   AND canary_basis_points=0 AND public_model_id=$5 AND slot_version=$6`,
		parent.ID, now, target.ModelName, target.ID, target.PublicModelID,
		request.ExpectedSlotVersion)
	if err != nil {
		return lifecycleWriteError("restore legacy stable slot", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	targetDetail := legacyLifecycleEventDetail(request, target.RouteItemsHash, legacyPlanHash, "")
	targetDetail["canonical_rollback"] = true
	targetDetail["restored_release_ref"] = parent.ReleaseRef
	if err := appendDeploymentLifecycleEvent(
		ctx, tx, target.ID, request.ExpectedReleaseVersion+1, "rollback", "active", "validated",
		request.ActorUserID, request.ExpectedReleaseVersion, targetDetail, now,
	); err != nil {
		return err
	}
	parentDetail := legacyLifecycleEventDetail(request, parent.RouteItemsHash, legacyPlanHash, "")
	parentDetail["canonical_rollback"] = true
	parentDetail["replaced_release_ref"] = target.ReleaseRef
	return appendDeploymentLifecycleEvent(
		ctx, tx, parent.ID, parent.Version+1, "promote", "rolled_back", "active",
		request.ActorUserID, parent.Version, parentDetail, now,
	)
}

func legacyLifecycleEventDetail(
	request DeploymentLifecycleRequest,
	routeItemsHash string,
	legacyPlanHash string,
	deploymentPlanHash string,
) map[string]interface{} {
	detail := map[string]interface{}{
		"runtime_mutation": "bounded_route_assignment", "canonical_lifecycle": true,
		"legacy_bridge": true, "lifecycle_receipt_id": request.LifecycleReceiptID,
		"public_model_id": request.PublicModelID, "route_items_hash": routeItemsHash,
		"expected_slot_version":  request.ExpectedSlotVersion,
		"resulting_slot_version": request.ExpectedSlotVersion + 1,
		"legacy_plan_hash":       legacyPlanHash,
	}
	if deploymentPlanHash != "" {
		detail["deployment_plan_hash"] = deploymentPlanHash
	}
	return detail
}

func lockRawLegacyLifecycleSlot(
	ctx context.Context,
	tx pgx.Tx,
	modelName string,
) (rawLegacyLifecycleSlot, error) {
	return queryRawLegacyLifecycleSlot(ctx, tx, modelName, true)
}

func readRawLegacyLifecycleSlot(
	ctx context.Context,
	tx pgx.Tx,
	modelName string,
) (rawLegacyLifecycleSlot, error) {
	return queryRawLegacyLifecycleSlot(ctx, tx, modelName, false)
}

func queryRawLegacyLifecycleSlot(
	ctx context.Context,
	tx pgx.Tx,
	modelName string,
	lock bool,
) (rawLegacyLifecycleSlot, error) {
	query := `
SELECT stable_release_id,canary_release_id,public_model_id,
       canary_basis_points,slot_version
  FROM runtime_change_slots
 WHERE model_name=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	var result rawLegacyLifecycleSlot
	if err := tx.QueryRow(ctx, query, modelName).Scan(
		&result.StableReleaseID, &result.CanaryReleaseID, &result.PublicModelID,
		&result.CanaryBasisPoints, &result.Version,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return rawLegacyLifecycleSlot{}, fmt.Errorf("%w: legacy lifecycle runtime slot", ErrConflict)
		}
		return rawLegacyLifecycleSlot{}, lifecycleWriteError("read legacy lifecycle runtime slot", err)
	}
	return result, nil
}

func legacyLifecycleSlotReceipt(
	targetPublicModelID int64,
	modelName string,
	before rawLegacyLifecycleSlot,
	after rawLegacyLifecycleSlot,
) DeploymentLifecycleSlotReceipt {
	return DeploymentLifecycleSlotReceipt{
		PublicModelID: targetPublicModelID, ModelName: modelName,
		BeforePublicModelID:     cloneLifecycleID(before.PublicModelID),
		BeforeStableReleaseID:   cloneLifecycleID(before.StableReleaseID),
		BeforeCanaryReleaseID:   cloneLifecycleID(before.CanaryReleaseID),
		BeforeCanaryBasisPoints: before.CanaryBasisPoints, BeforeVersion: before.Version,
		AfterPublicModelID:     cloneLifecycleID(after.PublicModelID),
		AfterStableReleaseID:   cloneLifecycleID(after.StableReleaseID),
		AfterCanaryReleaseID:   cloneLifecycleID(after.CanaryReleaseID),
		AfterCanaryBasisPoints: after.CanaryBasisPoints, AfterVersion: after.Version,
	}
}

func cloneLifecycleID(value *int64) *int64 {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
