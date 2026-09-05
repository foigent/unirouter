package runtimechange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

type deploymentLifecycleSlot struct {
	StableReleaseID   *int64
	CanaryReleaseID   *int64
	CanaryBasisPoints int
	Version           int64
}

// StartDeploymentCanary is the retired pre-receipt shell. Canonical lifecycle
// writes must use ApplyDeploymentLifecycle.
func (m *Manager) StartDeploymentCanary(
	ctx context.Context,
	request DeploymentLifecycleRequest,
	basisPoints int,
) (DeploymentRelease, error) {
	return DeploymentRelease{}, ErrLegacyRuntimeChangeRetired
}

func (m *Manager) startDeploymentCanaryInTx(
	ctx context.Context,
	tx pgx.Tx,
	request DeploymentLifecycleRequest,
	basisPoints int,
) (DeploymentRelease, error) {

	target, err := readDeploymentRelease(ctx, tx, request.ReleaseRef, true)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if target.PublicModelID != request.PublicModelID || target.Stage != "validated" ||
		target.Version != request.ExpectedReleaseVersion || target.ParentStableReleaseID == nil {
		return DeploymentRelease{}, ErrConflict
	}
	if err := validateDeploymentLifecycleScope(ctx, tx, request, target.ModelName); err != nil {
		return DeploymentRelease{}, err
	}
	parent, err := readDeploymentReleaseByID(ctx, tx, *target.ParentStableReleaseID, true)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateDeploymentParent(target, parent, "active"); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateSealedDeploymentRelease(ctx, tx, target, "validated", request.ExpectedSlotVersion); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateSealedDeploymentRelease(ctx, tx, parent, "active", request.ExpectedSlotVersion); err != nil {
		return DeploymentRelease{}, err
	}
	if err := lockDeploymentRouteAuthority(ctx, tx, target.Candidate.DeploymentRoutes); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateDeploymentRoutes(ctx, tx, request.CustomerOrgID, request.PublicModelID, target.Candidate.DeploymentRoutes); err != nil {
		return DeploymentRelease{}, err
	}
	if err := m.validateAnyExecutableDeploymentRoute(
		ctx, tx, request.CustomerOrgID, request.PublicModelID, target,
	); err != nil {
		return DeploymentRelease{}, err
	}
	slot, err := lockDeploymentLifecycleSlot(ctx, tx, target.ModelName, request.PublicModelID)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if slot.Version != request.ExpectedSlotVersion || slot.StableReleaseID == nil ||
		*slot.StableReleaseID != parent.ID || slot.CanaryReleaseID != nil || slot.CanaryBasisPoints != 0 {
		return DeploymentRelease{}, fmt.Errorf("%w: canonical slot changed or another canary is active", ErrConflict)
	}
	now, err := deploymentLifecycleNow(ctx, tx)
	if err != nil {
		return DeploymentRelease{}, err
	}

	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='canary',version=version+1,canary_basis_points=$3,
       activated_by=$4,activated_at=$5,updated_at=$5
 WHERE id=$1 AND stage='validated' AND version=$2
   AND candidate_version='unirouter.runtime-change-candidate.v2'
   AND public_model_id=$6 AND route_items_sealed_at IS NOT NULL`, target.ID,
		request.ExpectedReleaseVersion, basisPoints, request.ActorUserID, now, request.PublicModelID)
	if err != nil {
		return DeploymentRelease{}, lifecycleWriteError("start deployment canary", err)
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET canary_release_id=$1,canary_basis_points=$2,
       slot_version=slot_version+1,updated_at=$3
 WHERE model_name=$4 AND public_model_id=$5 AND stable_release_id=$6
   AND canary_release_id IS NULL AND canary_basis_points=0 AND slot_version=$7`,
		target.ID, basisPoints, now, target.ModelName, request.PublicModelID,
		parent.ID, request.ExpectedSlotVersion)
	if err != nil {
		return DeploymentRelease{}, lifecycleWriteError("assign deployment canary", err)
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, ErrConflict
	}
	detail := deploymentLifecycleDetail(request, target, request.ExpectedSlotVersion+1)
	detail["canary_basis_points"] = basisPoints
	detail["stable_release_ref"] = parent.ReleaseRef
	if err := appendDeploymentLifecycleEvent(
		ctx, tx, target.ID, request.ExpectedReleaseVersion+1, "advance", "validated", "canary",
		request.ActorUserID, request.ExpectedReleaseVersion, detail, now,
	); err != nil {
		return DeploymentRelease{}, err
	}
	transitioned, err := readDeploymentRelease(ctx, tx, request.ReleaseRef, false)
	if err != nil {
		return DeploymentRelease{}, err
	}
	return transitioned, nil
}

// PromoteDeploymentCanary is the retired pre-receipt shell. Canonical
// lifecycle writes must use ApplyDeploymentLifecycle.
func (m *Manager) PromoteDeploymentCanary(
	ctx context.Context,
	request DeploymentLifecycleRequest,
) (DeploymentRelease, error) {
	return DeploymentRelease{}, ErrLegacyRuntimeChangeRetired
}

func (m *Manager) promoteDeploymentCanaryInTx(
	ctx context.Context,
	tx pgx.Tx,
	request DeploymentLifecycleRequest,
) (DeploymentRelease, error) {

	target, err := readDeploymentRelease(ctx, tx, request.ReleaseRef, true)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if target.PublicModelID != request.PublicModelID || target.Stage != "canary" ||
		target.Version != request.ExpectedReleaseVersion || target.ParentStableReleaseID == nil ||
		target.CanaryBasisPoints < 1 || target.CanaryBasisPoints > 5000 {
		return DeploymentRelease{}, ErrConflict
	}
	if err := validateDeploymentLifecycleScope(ctx, tx, request, target.ModelName); err != nil {
		return DeploymentRelease{}, err
	}
	parent, err := readDeploymentReleaseByID(ctx, tx, *target.ParentStableReleaseID, true)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateDeploymentParent(target, parent, "active"); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateSealedDeploymentRelease(ctx, tx, target, "canary", request.ExpectedSlotVersion); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateSealedDeploymentRelease(ctx, tx, parent, "active", request.ExpectedSlotVersion); err != nil {
		return DeploymentRelease{}, err
	}
	if err := lockDeploymentRouteAuthority(ctx, tx, target.Candidate.DeploymentRoutes); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateDeploymentRoutes(ctx, tx, request.CustomerOrgID, request.PublicModelID, target.Candidate.DeploymentRoutes); err != nil {
		return DeploymentRelease{}, err
	}
	if err := m.validateAnyExecutableDeploymentRoute(
		ctx, tx, request.CustomerOrgID, request.PublicModelID, target,
	); err != nil {
		return DeploymentRelease{}, err
	}
	slot, err := lockDeploymentLifecycleSlot(ctx, tx, target.ModelName, request.PublicModelID)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if slot.Version != request.ExpectedSlotVersion || slot.StableReleaseID == nil ||
		*slot.StableReleaseID != parent.ID || slot.CanaryReleaseID == nil ||
		*slot.CanaryReleaseID != target.ID || slot.CanaryBasisPoints != target.CanaryBasisPoints {
		return DeploymentRelease{}, fmt.Errorf("%w: canonical canary slot changed", ErrConflict)
	}
	now, err := deploymentLifecycleNow(ctx, tx)
	if err != nil {
		return DeploymentRelease{}, err
	}

	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='rolled_back',version=version+1,rolled_back_at=$3,updated_at=$3
 WHERE id=$1 AND stage='active' AND version=$2
   AND candidate_version='unirouter.runtime-change-candidate.v2'
   AND public_model_id=$4 AND route_items_sealed_at IS NOT NULL`,
		parent.ID, parent.Version, now, request.PublicModelID)
	if err != nil {
		return DeploymentRelease{}, lifecycleWriteError("retain previous deployment stable", err)
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='active',version=version+1,canary_basis_points=0,updated_at=$3
 WHERE id=$1 AND stage='canary' AND version=$2
   AND candidate_version='unirouter.runtime-change-candidate.v2'
   AND public_model_id=$4 AND route_items_sealed_at IS NOT NULL`,
		target.ID, request.ExpectedReleaseVersion, now, request.PublicModelID)
	if err != nil {
		return DeploymentRelease{}, lifecycleWriteError("promote deployment canary", err)
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, ErrConflict
	}
	tag, err = tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=$1,canary_release_id=NULL,canary_basis_points=0,
	   slot_version=slot_version+1,updated_at=$2
 WHERE model_name=$3 AND public_model_id=$4 AND stable_release_id=$5
	AND canary_release_id=$1 AND canary_basis_points=$6 AND slot_version=$7`,
		target.ID, now, target.ModelName, request.PublicModelID, parent.ID,
		target.CanaryBasisPoints, request.ExpectedSlotVersion)
	if err != nil {
		return DeploymentRelease{}, lifecycleWriteError("switch deployment stable", err)
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, ErrConflict
	}
	parentDetail := deploymentLifecycleDetail(request, parent, request.ExpectedSlotVersion+1)
	parentDetail["replaced_by_release_ref"] = target.ReleaseRef
	if err := appendDeploymentLifecycleEvent(
		ctx, tx, parent.ID, parent.Version+1, "rollback", "active", "rolled_back",
		request.ActorUserID, parent.Version, parentDetail, now,
	); err != nil {
		return DeploymentRelease{}, err
	}
	targetDetail := deploymentLifecycleDetail(request, target, request.ExpectedSlotVersion+1)
	targetDetail["replaced_release_ref"] = parent.ReleaseRef
	if err := appendDeploymentLifecycleEvent(
		ctx, tx, target.ID, request.ExpectedReleaseVersion+1, "promote", "canary", "active",
		request.ActorUserID, request.ExpectedReleaseVersion, targetDetail, now,
	); err != nil {
		return DeploymentRelease{}, err
	}
	transitioned, err := readDeploymentRelease(ctx, tx, request.ReleaseRef, false)
	if err != nil {
		return DeploymentRelease{}, err
	}
	return transitioned, nil
}

// RollbackDeploymentRelease is the retired pre-receipt shell. Canonical
// lifecycle writes must use ApplyDeploymentLifecycle.
func (m *Manager) RollbackDeploymentRelease(
	ctx context.Context,
	request DeploymentLifecycleRequest,
) (DeploymentRelease, error) {
	return DeploymentRelease{}, ErrLegacyRuntimeChangeRetired
}

// rollbackDeploymentReleaseInTx accepts an optional requiredTargetStage for
// M76's stage-specific cancel_canary and rollback_stable actions. The empty
// value preserves the historical RollbackDeploymentRelease behavior.
func (m *Manager) rollbackDeploymentReleaseInTx(
	ctx context.Context,
	tx pgx.Tx,
	request DeploymentLifecycleRequest,
	requiredTargetStage string,
) (DeploymentRelease, error) {

	target, err := readDeploymentRelease(ctx, tx, request.ReleaseRef, true)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if target.PublicModelID != request.PublicModelID || target.Version != request.ExpectedReleaseVersion ||
		(target.Stage != "canary" && target.Stage != "active") || target.ParentStableReleaseID == nil {
		return DeploymentRelease{}, ErrConflict
	}
	if requiredTargetStage != "" && target.Stage != requiredTargetStage {
		return DeploymentRelease{}, fmt.Errorf("%w: lifecycle action does not match target stage", ErrConflict)
	}
	if err := validateDeploymentLifecycleScope(ctx, tx, request, target.ModelName); err != nil {
		return DeploymentRelease{}, err
	}
	parent, err := readDeploymentReleaseByID(ctx, tx, *target.ParentStableReleaseID, true)
	if err != nil {
		return DeploymentRelease{}, err
	}
	parentStage := "active"
	if target.Stage == "active" {
		parentStage = "rolled_back"
	}
	if err := validateDeploymentParent(target, parent, parentStage); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateSealedDeploymentRelease(ctx, tx, target, target.Stage, request.ExpectedSlotVersion); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateSealedDeploymentRelease(ctx, tx, parent, parentStage, request.ExpectedSlotVersion); err != nil {
		return DeploymentRelease{}, err
	}
	if target.Stage == "active" {
		if err := lockDeploymentRouteAuthority(ctx, tx, parent.Candidate.DeploymentRoutes); err != nil {
			return DeploymentRelease{}, err
		}
		if err := m.validateAnyExecutableDeploymentRoute(
			ctx, tx, request.CustomerOrgID, request.PublicModelID, parent,
		); err != nil {
			return DeploymentRelease{}, err
		}
	}
	slot, err := lockDeploymentLifecycleSlot(ctx, tx, target.ModelName, request.PublicModelID)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if slot.Version != request.ExpectedSlotVersion || slot.StableReleaseID == nil {
		return DeploymentRelease{}, fmt.Errorf("%w: canonical rollback slot changed", ErrConflict)
	}
	if target.Stage == "canary" {
		if *slot.StableReleaseID != parent.ID || slot.CanaryReleaseID == nil ||
			*slot.CanaryReleaseID != target.ID || slot.CanaryBasisPoints != target.CanaryBasisPoints {
			return DeploymentRelease{}, fmt.Errorf("%w: canonical canary slot changed", ErrConflict)
		}
	} else if *slot.StableReleaseID != target.ID || slot.CanaryReleaseID != nil || slot.CanaryBasisPoints != 0 {
		return DeploymentRelease{}, fmt.Errorf("%w: canonical stable slot changed", ErrConflict)
	}
	now, err := deploymentLifecycleNow(ctx, tx)
	if err != nil {
		return DeploymentRelease{}, err
	}

	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='rolled_back',version=version+1,canary_basis_points=0,
	   rolled_back_at=$3,updated_at=$3
 WHERE id=$1 AND stage=$4 AND version=$2
	AND candidate_version='unirouter.runtime-change-candidate.v2'
	AND public_model_id=$5 AND route_items_sealed_at IS NOT NULL`, target.ID,
		request.ExpectedReleaseVersion, now, target.Stage, request.PublicModelID)
	if err != nil {
		return DeploymentRelease{}, lifecycleWriteError("rollback deployment release", err)
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, ErrConflict
	}
	if target.Stage == "canary" {
		tag, err = tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET canary_release_id=NULL,canary_basis_points=0,
	   slot_version=slot_version+1,updated_at=$1
 WHERE model_name=$2 AND public_model_id=$3 AND stable_release_id=$4
	AND canary_release_id=$5 AND canary_basis_points=$6 AND slot_version=$7`, now,
			target.ModelName, request.PublicModelID, parent.ID, target.ID,
			target.CanaryBasisPoints, request.ExpectedSlotVersion)
		if err != nil {
			return DeploymentRelease{}, lifecycleWriteError("remove deployment canary", err)
		}
	} else {
		tag, err = tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='active',version=version+1,rolled_back_at=NULL,updated_at=$3
 WHERE id=$1 AND stage='rolled_back' AND version=$2
	AND candidate_version='unirouter.runtime-change-candidate.v2'
	AND public_model_id=$4 AND route_items_sealed_at IS NOT NULL`, parent.ID,
			parent.Version, now, request.PublicModelID)
		if err != nil {
			return DeploymentRelease{}, lifecycleWriteError("restore deployment parent", err)
		}
		if tag.RowsAffected() != 1 {
			return DeploymentRelease{}, ErrConflict
		}
		tag, err = tx.Exec(ctx, `
UPDATE runtime_change_slots
   SET stable_release_id=$1,canary_release_id=NULL,canary_basis_points=0,
	   slot_version=slot_version+1,updated_at=$2
 WHERE model_name=$3 AND public_model_id=$4 AND stable_release_id=$5
	AND canary_release_id IS NULL AND canary_basis_points=0 AND slot_version=$6`,
			parent.ID, now, target.ModelName, request.PublicModelID, target.ID,
			request.ExpectedSlotVersion)
		if err != nil {
			return DeploymentRelease{}, lifecycleWriteError("restore deployment stable slot", err)
		}
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, ErrConflict
	}
	targetDetail := deploymentLifecycleDetail(request, target, request.ExpectedSlotVersion+1)
	targetDetail["restored_release_ref"] = parent.ReleaseRef
	if err := appendDeploymentLifecycleEvent(
		ctx, tx, target.ID, request.ExpectedReleaseVersion+1, "rollback", target.Stage, "rolled_back",
		request.ActorUserID, request.ExpectedReleaseVersion, targetDetail, now,
	); err != nil {
		return DeploymentRelease{}, err
	}
	if target.Stage == "active" {
		parentDetail := deploymentLifecycleDetail(request, parent, request.ExpectedSlotVersion+1)
		parentDetail["replaced_release_ref"] = target.ReleaseRef
		if err := appendDeploymentLifecycleEvent(
			ctx, tx, parent.ID, parent.Version+1, "promote", "rolled_back", "active",
			request.ActorUserID, parent.Version, parentDetail, now,
		); err != nil {
			return DeploymentRelease{}, err
		}
	}
	transitioned, err := readDeploymentRelease(ctx, tx, request.ReleaseRef, false)
	if err != nil {
		return DeploymentRelease{}, err
	}
	return transitioned, nil
}

func normalizeDeploymentLifecycleRequest(request DeploymentLifecycleRequest) (DeploymentLifecycleRequest, error) {
	request.ReleaseRef = strings.TrimSpace(strings.ToLower(request.ReleaseRef))
	if request.ActorOrgID == 0 {
		request.ActorOrgID = request.CustomerOrgID
	}
	if !releaseRefPattern.MatchString(request.ReleaseRef) || request.PublicModelID <= 0 ||
		request.ExpectedReleaseVersion <= 0 || request.ExpectedSlotVersion <= 0 ||
		request.ActorUserID <= 0 || request.ActorOrgID <= 0 || request.CustomerOrgID <= 0 ||
		request.LifecycleReceiptID < 0 {
		return DeploymentLifecycleRequest{}, fmt.Errorf("%w: deployment lifecycle identity", ErrInvalid)
	}
	return request, nil
}

func (m *Manager) beginDeploymentLifecycleTransaction(ctx context.Context) (pgx.Tx, error) {
	database, ok := m.db.(txOptionsBeginner)
	if !ok {
		return nil, fmt.Errorf("%w: database does not support explicit transaction isolation", ErrUnavailable)
	}
	tx, err := database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, lifecycleWriteError("begin deployment lifecycle", err)
	}
	if err := lockDeploymentAssignmentWriterTables(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// lockDeploymentAssignmentWriterTables establishes the global lock order for
// v2 slot/release transitions before any deployment, credential, actor or slot
// row is locked. Legacy reconciliation takes SHARE locks in this same order,
// so it can prove that no current v2 assignment exists without racing a
// cutover or deadlocking on lower-level route rows.
func lockDeploymentAssignmentWriterTables(ctx context.Context, tx pgx.Tx) error {
	if err := schemaauthority.LockExecutionCompatibility(ctx, tx); err != nil {
		return lifecycleWriteError("lock execution compatibility", err)
	}
	return nil
}

func validateDeploymentLifecycleScope(
	ctx context.Context,
	tx pgx.Tx,
	request DeploymentLifecycleRequest,
	modelName string,
) error {
	var storedModelName, lifecycle string
	if err := tx.QueryRow(ctx, `
SELECT model_name,lifecycle
  FROM model_registry
 WHERE id=$1 AND org_id=$2
 FOR SHARE`, request.PublicModelID, request.CustomerOrgID).Scan(&storedModelName, &lifecycle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: public model ownership", ErrConflict)
		}
		return fmt.Errorf("%w: read public model ownership: %v", ErrUnavailable, err)
	}
	if storedModelName != modelName || lifecycle == "retired" {
		return fmt.Errorf("%w: public model identity or lifecycle", ErrConflict)
	}
	return validateDeploymentLifecycleActor(
		ctx, tx, request.ActorUserID, request.ActorOrgID, request.CustomerOrgID,
	)
}

func validateDeploymentLifecycleActor(
	ctx context.Context,
	tx pgx.Tx,
	actorUserID, actorOrgID, customerOrgID int64,
) error {
	var userID int64
	var organizationRole, userStatus, orgStatus string
	err := tx.QueryRow(ctx, `
SELECT users.id,COALESCE(NULLIF(users.organization_role,''),users.role),
       users.status,orgs.status
  FROM users
  JOIN orgs ON orgs.id=users.org_id
 WHERE users.id=$1 AND users.org_id=$2
 FOR SHARE OF users,orgs`, actorUserID, actorOrgID).Scan(
		&userID, &organizationRole, &userStatus, &orgStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: lifecycle actor organization", ErrConflict)
	}
	if err != nil {
		return lifecycleWriteError("read deployment lifecycle actor", err)
	}
	if userStatus != "active" || orgStatus != "active" ||
		(organizationRole != "platform_admin" &&
			(organizationRole != "org_admin" || actorOrgID != customerOrgID)) {
		return fmt.Errorf("%w: lifecycle actor authority", ErrConflict)
	}
	return nil
}

func validateDeploymentParent(target, parent DeploymentRelease, expectedStage string) error {
	if target.ParentStableReleaseID == nil || parent.ID != *target.ParentStableReleaseID ||
		parent.PublicModelID != target.PublicModelID || parent.ModelName != target.ModelName ||
		parent.Stage != expectedStage || target.BaselineHash != parent.CandidateHash {
		return fmt.Errorf("%w: sealed deployment parent identity", ErrConflict)
	}
	return nil
}

func validateSealedDeploymentRelease(
	ctx context.Context,
	store deploymentManifestStore,
	release DeploymentRelease,
	expectedStage string,
	slotVersion int64,
) error {
	if release.CandidateVersion != DeploymentCandidateVersion || release.RouteItemsSealedAt == nil ||
		release.Stage != expectedStage || release.RouteItemsCount <= 0 ||
		release.Candidate.ContractVersion != DeploymentCandidateVersion ||
		release.Candidate.PublicModelID != release.PublicModelID ||
		release.Candidate.ModelName != release.ModelName ||
		hashJSON(release.Candidate) != release.CandidateHash {
		return fmt.Errorf("%w: sealed deployment release identity", ErrConflict)
	}
	assignment, err := loadSealedDeploymentAssignmentFrom(
		ctx, store, release.ID, release.PublicModelID, release.ModelName, expectedStage, slotVersion,
	)
	if err != nil {
		return err
	}
	if assignment.RouteItemsHash != release.RouteItemsHash ||
		!reflect.DeepEqual(assignment.Routes, release.Candidate.DeploymentRoutes) {
		return fmt.Errorf("%w: sealed deployment candidate parity", ErrConflict)
	}
	return nil
}

func lockDeploymentLifecycleSlot(
	ctx context.Context,
	tx pgx.Tx,
	modelName string,
	publicModelID int64,
) (deploymentLifecycleSlot, error) {
	var slot deploymentLifecycleSlot
	err := tx.QueryRow(ctx, `
SELECT stable_release_id,canary_release_id,canary_basis_points,slot_version
  FROM runtime_change_slots
 WHERE model_name=$1 AND public_model_id=$2
 FOR UPDATE`, modelName, publicModelID).Scan(
		&slot.StableReleaseID, &slot.CanaryReleaseID, &slot.CanaryBasisPoints, &slot.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return deploymentLifecycleSlot{}, fmt.Errorf("%w: canonical runtime slot", ErrConflict)
	}
	if err != nil {
		return deploymentLifecycleSlot{}, lifecycleWriteError("lock canonical runtime slot", err)
	}
	return slot, nil
}

func deploymentLifecycleNow(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT NOW()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("%w: read deployment lifecycle clock: %v", ErrUnavailable, err)
	}
	return now.UTC().Truncate(time.Microsecond), nil
}

func (m *Manager) validateAnyExecutableDeploymentRoute(
	ctx context.Context,
	store deploymentManifestStore,
	orgID, publicModelID int64,
	release DeploymentRelease,
) error {
	var publicContextLimit, publicOutputLimit int
	if err := store.QueryRow(ctx, `
SELECT context_length,output_limit
  FROM model_registry
 WHERE id=$1 AND org_id=$2
 FOR SHARE`, publicModelID, orgID).Scan(&publicContextLimit, &publicOutputLimit); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: public model execution limits", ErrConflict)
		}
		return fmt.Errorf("%w: read public model execution limits: %v", ErrUnavailable, err)
	}
	if publicContextLimit <= 0 || publicOutputLimit <= 0 {
		return fmt.Errorf("%w: public model execution limits are missing", ErrConflict)
	}
	var dbNow time.Time
	if err := store.QueryRow(ctx, `SELECT NOW()`).Scan(&dbNow); err != nil {
		return fmt.Errorf("%w: read deployment lifecycle route clock: %v", ErrUnavailable, err)
	}
	channelsByOperation := make(map[string][]routingpolicy.SelectionChannel)
	operations := make([]string, 0)
	for _, route := range release.Candidate.DeploymentRoutes {
		if _, exists := channelsByOperation[route.ModelOperationID]; !exists {
			operations = append(operations, route.ModelOperationID)
			channelsByOperation[route.ModelOperationID] = nil
		}
		runtime, err := inspectDeploymentRoute(ctx, store, orgID, publicModelID, route)
		if err != nil {
			if errors.Is(err, ErrUnavailable) {
				return err
			}
			continue
		}
		if runtime.MaxContext <= 0 || runtime.DefaultMaxOutput <= 0 {
			continue
		}
		priority, weight := runtime.Priority, runtime.Weight
		if route.Priority != nil {
			priority = *route.Priority
		}
		if route.Weight != nil {
			weight = *route.Weight
		}
		channel := routingpolicy.SelectionChannel{
			ID: runtime.DeploymentID, Name: runtime.UpstreamModel, Provider: runtime.Provider,
			Priority: priority, Weight: weight, Status: "active",
			CircuitState: runtime.CircuitState, HalfOpenUntil: runtime.HalfOpenUntil,
		}
		for _, bindingID := range runtime.CredentialIDs {
			channel.Keys = append(channel.Keys, routingpolicy.SelectionKey{ID: bindingID, Status: "active"})
		}
		channelsByOperation[route.ModelOperationID] = append(channelsByOperation[route.ModelOperationID], channel)
	}
	for _, operationID := range operations {
		evaluation := routingpolicy.EvaluateSelection(routingpolicy.SelectionSnapshot{
			Model: release.ModelName, DBNow: dbNow.UTC(), Version: release.RouteItemsHash,
			Source: "sealed-v2-lifecycle", Channels: channelsByOperation[operationID],
		}, m.policy, fmt.Sprintf("deployment-lifecycle:%d:%d:%s", publicModelID, release.ID, operationID))
		if len(evaluation.Selected) == 0 {
			return fmt.Errorf(
				"%w: sealed v2 release operation %s has no executable route (%s)",
				ErrConflict, operationID, evaluation.ExclusionReasons,
			)
		}
	}
	return nil
}

func deploymentLifecycleDetail(
	request DeploymentLifecycleRequest,
	release DeploymentRelease,
	resultingSlotVersion int64,
) map[string]interface{} {
	detail := map[string]interface{}{
		"runtime_mutation":       "bounded_route_assignment",
		"canonical_lifecycle":    true,
		"public_model_id":        request.PublicModelID,
		"route_items_hash":       release.RouteItemsHash,
		"expected_slot_version":  request.ExpectedSlotVersion,
		"resulting_slot_version": resultingSlotVersion,
	}
	if request.LifecycleReceiptID > 0 {
		detail["lifecycle_receipt_id"] = request.LifecycleReceiptID
	}
	return detail
}

func appendDeploymentLifecycleEvent(
	ctx context.Context,
	tx pgx.Tx,
	releaseID, sequence int64,
	action, fromStage, toStage string,
	actorUserID, expectedVersion int64,
	detail map[string]interface{},
	occurredAt time.Time,
) error {
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("%w: encode deployment lifecycle event: %v", ErrUnavailable, err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO runtime_change_events (
    release_id,sequence,action,from_stage,to_stage,actor_user_id,
    expected_version,resulting_version,detail,occurred_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)`, releaseID, sequence,
		action, fromStage, toStage, actorUserID, expectedVersion, expectedVersion+1,
		string(detailJSON), occurredAt); err != nil {
		return lifecycleWriteError("append deployment lifecycle event", err)
	}
	return nil
}

func lifecycleWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01") {
		return fmt.Errorf("%w: %s", ErrConflict, operation)
	}
	return fmt.Errorf("%w: %s: %v", ErrUnavailable, operation, err)
}
