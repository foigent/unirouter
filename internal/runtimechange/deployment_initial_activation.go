package runtimechange

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// InitialDeploymentActivationRequest identifies one sealed v2 release and the
// customer authority that may make it the first stable assignment. There is no
// caller-supplied slot version: the domain precondition is that no slot row of
// any kind exists for the public model or its stable model name.
type InitialDeploymentActivationRequest struct {
	ReleaseRef             string `json:"release_ref"`
	PublicModelID          int64  `json:"public_model_id"`
	ExpectedReleaseVersion int64  `json:"expected_release_version"`
	ActorUserID            int64  `json:"actor_user_id"`
	ActorOrgID             int64  `json:"actor_org_id,omitempty"`
	CustomerOrgID          int64  `json:"customer_org_id"`
	LifecycleReceiptID     int64  `json:"-"`
}

// ActivateInitialDeploymentStable is the retired pre-receipt shell. Canonical
// lifecycle writes must use ApplyDeploymentLifecycle so their receipt and
// state mutation commit atomically.
func (m *Manager) ActivateInitialDeploymentStable(
	ctx context.Context,
	request InitialDeploymentActivationRequest,
) (DeploymentRelease, error) {
	return DeploymentRelease{}, ErrLegacyRuntimeChangeRetired
}

// activateInitialDeploymentStableInTx owns the exact state transition but not
// its transaction boundary. The receipt executor uses this helper so the
// immutable receipt, qualification snapshot and audit row commit atomically
// with the same release/slot/event writes as the historical public method.
func (m *Manager) activateInitialDeploymentStableInTx(
	ctx context.Context,
	tx pgx.Tx,
	request InitialDeploymentActivationRequest,
) (DeploymentRelease, error) {

	// Freeze the deployment-global root-customer set before the first
	// repeatable-read snapshot is established. A writer that committed before
	// this lock was granted is then visible; later writers wait until commit.
	if err := lockInitialDeploymentCustomerRootSet(ctx, tx); err != nil {
		return DeploymentRelease{}, err
	}
	// For resources shared with canonical canary and cutover transitions, keep
	// their common order: target release, model/user authority, deployment route
	// authority, then runtime slot. The slot insert happens only after the
	// release CAS because the database slot guard requires an active release.
	target, err := lockInitialDeploymentRelease(ctx, tx, request.ReleaseRef)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if target.PublicModelID != request.PublicModelID || target.Stage != "validated" ||
		target.Version != request.ExpectedReleaseVersion || target.ParentStableReleaseID != nil ||
		target.CanaryBasisPoints != 0 {
		return DeploymentRelease{}, fmt.Errorf("%w: initial deployment release identity", ErrConflict)
	}
	emptyBaseline := hashJSON(DeploymentCandidate{
		ContractVersion:  DeploymentCandidateVersion,
		PublicModelID:    request.PublicModelID,
		ModelName:        target.ModelName,
		DeploymentRoutes: []DeploymentRoute{},
	})
	if target.BaselineHash != emptyBaseline {
		return DeploymentRelease{}, fmt.Errorf("%w: initial deployment baseline is not empty", ErrConflict)
	}
	if err := validateInitialDeploymentActivationAuthority(ctx, tx, request, target.ModelName); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateSealedDeploymentRelease(ctx, tx, target, "validated", 1); err != nil {
		return DeploymentRelease{}, err
	}
	if err := rejectExistingInitialDeploymentAuthority(ctx, tx, target); err != nil {
		return DeploymentRelease{}, err
	}
	if err := lockDeploymentRouteAuthority(ctx, tx, target.Candidate.DeploymentRoutes); err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateDeploymentRoutes(
		ctx, tx, request.CustomerOrgID, request.PublicModelID, target.Candidate.DeploymentRoutes,
	); err != nil {
		return DeploymentRelease{}, err
	}
	if err := requireExpansionUpstreamScope(ctx, tx, request.CustomerOrgID, target); err != nil {
		return DeploymentRelease{}, err
	}
	if err := m.validateAnyExecutableDeploymentRoute(
		ctx, tx, request.CustomerOrgID, request.PublicModelID, target,
	); err != nil {
		return DeploymentRelease{}, err
	}
	if err := requireInitialDeploymentSlotAbsent(
		ctx, tx, target.ModelName, request.PublicModelID,
	); err != nil {
		return DeploymentRelease{}, err
	}
	now, err := deploymentLifecycleNow(ctx, tx)
	if err != nil {
		return DeploymentRelease{}, err
	}

	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='active',version=version+1,activated_by=$3,activated_at=$4,
       canary_basis_points=0,updated_at=$4
 WHERE id=$1 AND stage='validated' AND version=$2
   AND candidate_version='unirouter.runtime-change-candidate.v2'
   AND public_model_id=$5 AND parent_stable_release_id IS NULL
   AND route_items_sealed_at IS NOT NULL`, target.ID, request.ExpectedReleaseVersion,
		request.ActorUserID, now, request.PublicModelID)
	if err != nil {
		return DeploymentRelease{}, initialDeploymentActivationWriteError("activate initial deployment release", err)
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, fmt.Errorf("%w: initial deployment release changed", ErrConflict)
	}
	tag, err = tx.Exec(ctx, `
INSERT INTO runtime_change_slots (
    model_name,stable_release_id,canary_release_id,canary_basis_points,
    slot_version,public_model_id,updated_at)
VALUES ($1,$2,NULL,0,1,$3,$4)
ON CONFLICT DO NOTHING`, target.ModelName, target.ID, request.PublicModelID, now)
	if err != nil {
		return DeploymentRelease{}, initialDeploymentActivationWriteError("create initial deployment slot", err)
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, fmt.Errorf("%w: runtime slot appeared concurrently", ErrConflict)
	}
	detail := map[string]interface{}{
		"runtime_mutation":       "bounded_route_assignment",
		"canonical_lifecycle":    true,
		"initial_activation":     true,
		"slot_precondition":      "absent",
		"public_model_id":        request.PublicModelID,
		"route_items_hash":       target.RouteItemsHash,
		"resulting_slot_version": int64(1),
	}
	if request.LifecycleReceiptID > 0 {
		detail["lifecycle_receipt_id"] = request.LifecycleReceiptID
	}
	if err := appendDeploymentLifecycleEvent(
		ctx, tx, target.ID, request.ExpectedReleaseVersion+1,
		"promote", "validated", "active", request.ActorUserID,
		request.ExpectedReleaseVersion, detail, now,
	); err != nil {
		return DeploymentRelease{}, err
	}
	activated, err := readDeploymentRelease(ctx, tx, request.ReleaseRef, false)
	if err != nil {
		return DeploymentRelease{}, err
	}
	return activated, nil
}

func normalizeInitialDeploymentActivationRequest(
	request InitialDeploymentActivationRequest,
) (InitialDeploymentActivationRequest, error) {
	request.ReleaseRef = strings.TrimSpace(strings.ToLower(request.ReleaseRef))
	if request.ActorOrgID == 0 {
		request.ActorOrgID = request.CustomerOrgID
	}
	if !releaseRefPattern.MatchString(request.ReleaseRef) || request.PublicModelID <= 0 ||
		request.ExpectedReleaseVersion <= 0 || request.ActorUserID <= 0 || request.ActorOrgID <= 0 ||
		request.CustomerOrgID <= 0 || request.LifecycleReceiptID < 0 {
		return InitialDeploymentActivationRequest{}, fmt.Errorf("%w: initial deployment activation identity", ErrInvalid)
	}
	return request, nil
}

func lockInitialDeploymentRelease(
	ctx context.Context,
	tx pgx.Tx,
	ref string,
) (DeploymentRelease, error) {
	var releaseID int64
	err := tx.QueryRow(ctx, `
SELECT id
  FROM runtime_change_releases
 WHERE release_ref=$1
   AND candidate_version='unirouter.runtime-change-candidate.v2'
 FOR UPDATE`, ref).Scan(&releaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentRelease{}, ErrNotFound
	}
	if err != nil {
		return DeploymentRelease{}, initialDeploymentActivationWriteError("lock initial deployment release", err)
	}
	return readDeploymentReleaseByID(ctx, tx, releaseID, false)
}

func validateInitialDeploymentActivationAuthority(
	ctx context.Context,
	tx pgx.Tx,
	request InitialDeploymentActivationRequest,
	modelName string,
) error {
	rows, err := tx.Query(ctx, `
SELECT id
  FROM orgs
 WHERE status='active' AND org_type='customer' AND parent_id IS NULL
 ORDER BY id`)
	if err != nil {
		return initialDeploymentActivationWriteError("read initial customer authority", err)
	}
	rootIDs := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return initialDeploymentActivationWriteError("scan initial customer authority", err)
		}
		rootIDs = append(rootIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return initialDeploymentActivationWriteError("iterate initial customer authority", err)
	}
	rows.Close()
	if len(rootIDs) != 1 || rootIDs[0] != request.CustomerOrgID {
		return fmt.Errorf("%w: customer organization is not the sole active root customer", ErrConflict)
	}

	var storedModelName, lifecycle string
	if err := tx.QueryRow(ctx, `
SELECT model_name,lifecycle
  FROM model_registry
 WHERE id=$1 AND org_id=$2
 FOR SHARE`, request.PublicModelID, request.CustomerOrgID).Scan(&storedModelName, &lifecycle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: public model ownership", ErrConflict)
		}
		return initialDeploymentActivationWriteError("read initial public model authority", err)
	}
	if storedModelName != modelName || lifecycle == "retired" {
		return fmt.Errorf("%w: public model identity or lifecycle", ErrConflict)
	}
	var actorID int64
	var actorRole, actorStatus, actorOrgStatus string
	if err := tx.QueryRow(ctx, `
SELECT users.id,COALESCE(NULLIF(users.organization_role,''),users.role),
       users.status,orgs.status
  FROM users
  JOIN orgs ON orgs.id=users.org_id
 WHERE users.id=$1 AND users.org_id=$2
 FOR SHARE OF users,orgs`, request.ActorUserID, request.ActorOrgID).Scan(
		&actorID, &actorRole, &actorStatus, &actorOrgStatus,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: actor organization or status", ErrConflict)
		}
		return initialDeploymentActivationWriteError("read initial deployment actor", err)
	}
	if actorStatus != "active" || actorOrgStatus != "active" ||
		(actorRole != "platform_admin" && (actorRole != "org_admin" || request.ActorOrgID != request.CustomerOrgID)) {
		return fmt.Errorf("%w: lifecycle actor authority", ErrConflict)
	}
	return nil
}

func lockInitialDeploymentCustomerRootSet(ctx context.Context, tx pgx.Tx) error {
	// Initial activation is deployment-global in the accepted on-prem topology.
	// SHARE blocks organization INSERT/UPDATE/DELETE while allowing ordinary
	// readers and is acquired before this transaction takes its RR snapshot.
	if _, err := tx.Exec(ctx, `LOCK TABLE orgs IN SHARE MODE`); err != nil {
		return initialDeploymentActivationWriteError("lock initial customer authority", err)
	}
	return nil
}

func rejectExistingInitialDeploymentAuthority(
	ctx context.Context,
	tx pgx.Tx,
	target DeploymentRelease,
) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
      FROM runtime_change_releases
     WHERE id<>$1 AND stage IN ('active','canary')
       AND (model_name=$2 OR public_model_id=$3)
)`, target.ID, target.ModelName, target.PublicModelID).Scan(&exists); err != nil {
		return initialDeploymentActivationWriteError("inspect existing runtime releases", err)
	}
	if exists {
		return fmt.Errorf("%w: active or canary runtime release already exists", ErrConflict)
	}
	return nil
}

func requireInitialDeploymentSlotAbsent(
	ctx context.Context,
	tx pgx.Tx,
	modelName string,
	publicModelID int64,
) error {
	rows, err := tx.Query(ctx, `
SELECT model_name
  FROM runtime_change_slots
 WHERE model_name=$1 OR public_model_id=$2
 ORDER BY model_name
 FOR UPDATE`, modelName, publicModelID)
	if err != nil {
		return initialDeploymentActivationWriteError("lock initial deployment slot boundary", err)
	}
	exists := rows.Next()
	if err := rows.Err(); err != nil {
		rows.Close()
		return initialDeploymentActivationWriteError("inspect initial deployment slot boundary", err)
	}
	rows.Close()
	if exists {
		return fmt.Errorf("%w: runtime slot already exists", ErrConflict)
	}
	return nil
}

func initialDeploymentActivationWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrConflict, operation)
	}
	return lifecycleWriteError(operation, err)
}
