package runtimechange

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const deploymentLifecycleOperationScopePrefix = "model_deployment.lifecycle."

type deploymentLifecycleReceiptClaim struct {
	ReceiptID          int64
	RequestFingerprint string
	ServerOperationID  string
	Status             string
	CreatedAt          time.Time
}

type deploymentLifecycleReceiptClaimRetryableError struct {
	operation string
	cause     error
}

func (e *deploymentLifecycleReceiptClaimRetryableError) Error() string {
	return e.operation + ": " + e.cause.Error()
}

func (e *deploymentLifecycleReceiptClaimRetryableError) Unwrap() error { return e.cause }

// ApplyDeploymentLifecycle is the sole M76 application executor. It commits
// the idempotency receipt, release/slot/event transition, M75 qualification
// snapshot and audit row in one repeatable-read transaction.
func (m *Manager) ApplyDeploymentLifecycle(
	ctx context.Context,
	command DeploymentLifecycleCommand,
) (DeploymentLifecycleReceipt, error) {
	command, err := NormalizeDeploymentLifecycleCommand(command)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	tx, err := m.beginDeploymentLifecycleTransaction(ctx)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	defer tx.Rollback(ctx)

	// Initial activation freezes the deployment-global root-customer set before
	// the receipt claim establishes the repeatable-read snapshot. The inner
	// transition reacquires the same compatible lock defensively.
	if command.Mutation.Action == DeploymentLifecycleActivateInitial {
		if err := lockInitialDeploymentCustomerRootSet(ctx, tx); err != nil {
			return DeploymentLifecycleReceipt{}, err
		}
	} else if isLegacyDeploymentLifecycleAction(command.Mutation.Action) {
		if _, err := tx.Exec(ctx, `LOCK TABLE orgs IN SHARE MODE`); err != nil {
			return DeploymentLifecycleReceipt{}, lifecycleWriteError("lock legacy lifecycle actor authority", err)
		}
	}
	claimed, claim, err := claimDeploymentLifecycleReceipt(ctx, tx, command)
	if err != nil {
		if isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			_ = tx.Rollback(ctx)
			if recovered, ok, recoveryErr := m.recoverDeploymentLifecycleReceipt(ctx, command); recoveryErr != nil {
				return DeploymentLifecycleReceipt{}, recoveryErr
			} else if ok {
				m.Invalidate()
				return recovered, nil
			}
		}
		return DeploymentLifecycleReceipt{}, err
	}
	if !claimed {
		if claim.RequestFingerprint != command.RequestFingerprint {
			return DeploymentLifecycleReceipt{}, ErrDeploymentLifecycleIdempotencyConflict
		}
		if claim.Status != "completed" {
			return DeploymentLifecycleReceipt{}, fmt.Errorf("%w: lifecycle receipt is not terminal", ErrConflict)
		}
		receipt, err := readDeploymentLifecycleReceipt(ctx, tx, claim.ReceiptID)
		if err != nil {
			return DeploymentLifecycleReceipt{}, err
		}
		receipt.IdempotencyReplay = claim.ServerOperationID != command.ServerOperationID
		if err := tx.Commit(ctx); err != nil {
			if recovered, ok, recoveryErr := m.recoverDeploymentLifecycleReceipt(ctx, command); recoveryErr != nil {
				return DeploymentLifecycleReceipt{}, recoveryErr
			} else if ok {
				return recovered, nil
			}
			return DeploymentLifecycleReceipt{}, lifecycleWriteError("commit deployment lifecycle replay", err)
		}
		return receipt, nil
	}
	if isLegacyDeploymentLifecycleAction(command.Mutation.Action) {
		receipt, err := m.applyLegacyDeploymentLifecycleReceiptInTx(ctx, tx, command, claim)
		if err != nil {
			return DeploymentLifecycleReceipt{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			if recovered, ok, recoveryErr := m.recoverDeploymentLifecycleReceipt(ctx, command); recoveryErr != nil {
				return DeploymentLifecycleReceipt{}, recoveryErr
			} else if ok {
				m.Invalidate()
				return recovered, nil
			}
			return DeploymentLifecycleReceipt{}, lifecycleWriteError("commit legacy deployment lifecycle receipt", err)
		}
		m.Invalidate()
		return receipt, nil
	}

	targetBefore, err := readDeploymentRelease(ctx, tx, command.Mutation.ReleaseRef, false)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	var parentBefore *DeploymentRelease
	if targetBefore.ParentStableReleaseID != nil {
		parent, readErr := readDeploymentReleaseByID(ctx, tx, *targetBefore.ParentStableReleaseID, false)
		if readErr != nil {
			return DeploymentLifecycleReceipt{}, readErr
		}
		parentBefore = &parent
	}

	targetAfter, err := m.applyDeploymentLifecycleMutationInTx(
		ctx, tx, command, claim.ReceiptID, targetBefore.PublicModelID,
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	var parentAfter *DeploymentRelease
	if parentBefore != nil {
		parent, readErr := readDeploymentReleaseByID(ctx, tx, parentBefore.ID, false)
		if readErr != nil {
			return DeploymentLifecycleReceipt{}, readErr
		}
		parentAfter = &parent
	}
	afterSlot, err := readDeploymentLifecycleSlotState(
		ctx, tx, targetAfter.PublicModelID, targetAfter.ModelName,
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	if !afterSlot.Exists {
		return DeploymentLifecycleReceipt{}, fmt.Errorf("%w: lifecycle transition produced no canonical slot", ErrUnavailable)
	}
	beforeSlot, err := deriveDeploymentLifecycleBeforeSlot(
		command.Mutation, targetBefore, parentBefore,
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	slotReceipt := deploymentLifecycleSlotReceipt(beforeSlot, afterSlot)

	releaseReceipts := []DeploymentLifecycleReleaseReceipt{
		deploymentLifecycleReleaseReceipt("target", targetBefore, targetAfter),
	}
	qualificationReceipts, err := snapshotDeploymentLifecycleQualifications(
		ctx, tx, "target", targetAfter,
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	if parentBefore != nil && parentAfter != nil {
		releaseReceipts = append(releaseReceipts,
			deploymentLifecycleReleaseReceipt("stable_parent", *parentBefore, *parentAfter),
		)
		parentQualifications, snapshotErr := snapshotDeploymentLifecycleQualifications(
			ctx, tx, "stable_parent", *parentAfter,
		)
		if snapshotErr != nil {
			return DeploymentLifecycleReceipt{}, snapshotErr
		}
		qualificationReceipts = append(qualificationReceipts, parentQualifications...)
	}
	if err := insertDeploymentLifecycleReleaseReceipts(ctx, tx, claim.ReceiptID, releaseReceipts); err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	if err := insertDeploymentLifecycleQualificationReceipts(ctx, tx, claim.ReceiptID, qualificationReceipts); err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	auditID, err := insertDeploymentLifecycleAudit(
		ctx, tx, claim.ReceiptID, command, targetAfter, slotReceipt,
		releaseReceipts, qualificationReceipts, "", "",
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	completedAt, err := completeDeploymentLifecycleReceipt(
		ctx, tx, claim.ReceiptID, targetAfter, slotReceipt, "", "", auditID,
	)
	if err != nil {
		return DeploymentLifecycleReceipt{}, err
	}
	receipt := DeploymentLifecycleReceipt{
		ContractVersion: DeploymentLifecycleReceiptContractVersion,
		ReceiptID:       claim.ReceiptID, Action: command.Mutation.Action,
		TargetReleaseRef: targetAfter.ReleaseRef, CustomerOrgID: command.CustomerOrgID,
		ActorUserID: command.ActorUserID, ServerOperationID: command.ServerOperationID,
		OriginalHTTPStatus: 200, Reason: command.Mutation.Reason,
		Slot: slotReceipt, Releases: releaseReceipts, Qualifications: qualificationReceipts,
		CreatedAt: claim.CreatedAt.UTC(), CompletedAt: completedAt.UTC(),
	}
	if err := tx.Commit(ctx); err != nil {
		if recovered, ok, recoveryErr := m.recoverDeploymentLifecycleReceipt(ctx, command); recoveryErr != nil {
			return DeploymentLifecycleReceipt{}, recoveryErr
		} else if ok {
			m.Invalidate()
			return recovered, nil
		}
		return DeploymentLifecycleReceipt{}, lifecycleWriteError("commit deployment lifecycle receipt", err)
	}
	m.Invalidate()
	return receipt, nil
}

func (m *Manager) applyDeploymentLifecycleMutationInTx(
	ctx context.Context,
	tx pgx.Tx,
	command DeploymentLifecycleCommand,
	receiptID int64,
	publicModelID int64,
) (DeploymentRelease, error) {
	switch command.Mutation.Action {
	case DeploymentLifecycleActivateInitial:
		request, err := normalizeInitialDeploymentActivationRequest(InitialDeploymentActivationRequest{
			ReleaseRef: command.Mutation.ReleaseRef, PublicModelID: publicModelID,
			ExpectedReleaseVersion: command.Mutation.ExpectedReleaseVersion,
			ActorUserID:            command.ActorUserID, ActorOrgID: command.ActorOrgID,
			CustomerOrgID: command.CustomerOrgID, LifecycleReceiptID: receiptID,
		})
		if err != nil {
			return DeploymentRelease{}, err
		}
		return m.activateInitialDeploymentStableInTx(ctx, tx, request)
	case DeploymentLifecycleStartCanary, DeploymentLifecyclePromote,
		DeploymentLifecycleCancelCanary, DeploymentLifecycleRollbackStable:
		request, err := normalizeDeploymentLifecycleRequest(DeploymentLifecycleRequest{
			ReleaseRef: command.Mutation.ReleaseRef, PublicModelID: publicModelID,
			ExpectedReleaseVersion: command.Mutation.ExpectedReleaseVersion,
			ExpectedSlotVersion:    command.Mutation.ExpectedSlotVersion,
			ActorUserID:            command.ActorUserID, ActorOrgID: command.ActorOrgID,
			CustomerOrgID: command.CustomerOrgID, LifecycleReceiptID: receiptID,
		})
		if err != nil {
			return DeploymentRelease{}, err
		}
		switch command.Mutation.Action {
		case DeploymentLifecycleStartCanary:
			return m.startDeploymentCanaryInTx(ctx, tx, request, command.Mutation.CanaryBasisPoints)
		case DeploymentLifecyclePromote:
			return m.promoteDeploymentCanaryInTx(ctx, tx, request)
		case DeploymentLifecycleCancelCanary:
			return m.rollbackDeploymentReleaseInTx(ctx, tx, request, "canary")
		case DeploymentLifecycleRollbackStable:
			return m.rollbackDeploymentReleaseInTx(ctx, tx, request, "active")
		}
	}
	return DeploymentRelease{}, fmt.Errorf("%w: deployment lifecycle action", ErrInvalid)
}

func claimDeploymentLifecycleReceipt(
	ctx context.Context,
	tx pgx.Tx,
	command DeploymentLifecycleCommand,
) (bool, deploymentLifecycleReceiptClaim, error) {
	operationScope := deploymentLifecycleOperationScopePrefix + string(command.Mutation.Action) + ".v1"
	var claim deploymentLifecycleReceiptClaim
	err := tx.QueryRow(ctx, `
INSERT INTO model_deployment_lifecycle_receipts (
  contract_version,customer_org_id,actor_user_id,actor_org_id,
  operation_scope,action,idempotency_key_hash,request_fingerprint,
  server_operation_id,target_release_ref,expected_release_version,
  expected_slot_version,requested_canary_basis_points,reason)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (customer_org_id,actor_user_id,operation_scope,idempotency_key_hash)
DO NOTHING
RETURNING receipt_id,request_fingerprint,server_operation_id,status,created_at`,
		DeploymentLifecycleReceiptContractVersion, command.CustomerOrgID,
		command.ActorUserID, command.ActorOrgID, operationScope, command.Mutation.Action,
		command.IdempotencyKeyHash, command.RequestFingerprint, command.ServerOperationID,
		command.Mutation.ReleaseRef, command.Mutation.ExpectedReleaseVersion,
		command.Mutation.ExpectedSlotVersion, command.Mutation.CanaryBasisPoints,
		command.Mutation.Reason,
	).Scan(
		&claim.ReceiptID, &claim.RequestFingerprint, &claim.ServerOperationID,
		&claim.Status, &claim.CreatedAt,
	)
	if err == nil {
		return true, claim, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		if isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			return false, deploymentLifecycleReceiptClaim{}, &deploymentLifecycleReceiptClaimRetryableError{
				operation: "claim deployment lifecycle receipt", cause: err,
			}
		}
		return false, deploymentLifecycleReceiptClaim{}, deploymentLifecycleReceiptClaimWriteError(
			"claim deployment lifecycle receipt", err,
		)
	}
	err = tx.QueryRow(ctx, `
SELECT receipt_id,request_fingerprint,server_operation_id,status,created_at
  FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND actor_user_id=$2 AND operation_scope=$3
   AND idempotency_key_hash=$4
 FOR SHARE`, command.CustomerOrgID, command.ActorUserID, operationScope,
		command.IdempotencyKeyHash).Scan(
		&claim.ReceiptID, &claim.RequestFingerprint, &claim.ServerOperationID,
		&claim.Status, &claim.CreatedAt,
	)
	if err != nil {
		if isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			return false, deploymentLifecycleReceiptClaim{}, &deploymentLifecycleReceiptClaimRetryableError{
				operation: "read deployment lifecycle receipt winner", cause: err,
			}
		}
		return false, deploymentLifecycleReceiptClaim{}, lifecycleWriteError("read deployment lifecycle receipt winner", err)
	}
	return false, claim, nil
}

func deploymentLifecycleReceiptClaimWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" &&
		pgErr.ConstraintName == "model_deployment_lifecycle_receipts_actor_tenant_fk" {
		return fmt.Errorf("%w: deployment lifecycle actor authority", ErrConflict)
	}
	return lifecycleWriteError(operation, err)
}

// recoverDeploymentLifecycleReceipt performs one bounded, outside-transaction
// winner read after a serialization conflict or ambiguous commit. It never
// retries the mutation: only an already-completed, exact-fingerprint receipt
// can be returned.
func (m *Manager) recoverDeploymentLifecycleReceipt(
	ctx context.Context,
	command DeploymentLifecycleCommand,
) (DeploymentLifecycleReceipt, bool, error) {
	operationScope := deploymentLifecycleOperationScopePrefix + string(command.Mutation.Action) + ".v1"
	var claim deploymentLifecycleReceiptClaim
	err := m.db.QueryRow(ctx, `
SELECT receipt_id,request_fingerprint,server_operation_id,status,created_at
  FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND actor_user_id=$2 AND operation_scope=$3
   AND idempotency_key_hash=$4`, command.CustomerOrgID, command.ActorUserID,
		operationScope, command.IdempotencyKeyHash,
	).Scan(
		&claim.ReceiptID, &claim.RequestFingerprint, &claim.ServerOperationID,
		&claim.Status, &claim.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentLifecycleReceipt{}, false, nil
	}
	if err != nil {
		return DeploymentLifecycleReceipt{}, false, lifecycleWriteError("recover deployment lifecycle receipt winner", err)
	}
	if claim.RequestFingerprint != command.RequestFingerprint {
		return DeploymentLifecycleReceipt{}, false, ErrDeploymentLifecycleIdempotencyConflict
	}
	if claim.Status != "completed" {
		return DeploymentLifecycleReceipt{}, false, nil
	}
	receipt, err := readDeploymentLifecycleReceipt(ctx, m.db, claim.ReceiptID)
	if err != nil {
		return DeploymentLifecycleReceipt{}, false, err
	}
	receipt.IdempotencyReplay = claim.ServerOperationID != command.ServerOperationID
	return receipt, true, nil
}

func isDeploymentLifecycleReceiptRecoveryCandidate(err error) bool {
	var pgErr interface{ SQLState() string }
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.SQLState() == "40001" || pgErr.SQLState() == "40P01"
}

func readDeploymentLifecycleSlotState(
	ctx context.Context,
	store deploymentManifestStore,
	publicModelID int64,
	modelName string,
) (DeploymentLifecycleSlotState, error) {
	var result DeploymentLifecycleSlotState
	result.PublicModelID = publicModelID
	err := store.QueryRow(ctx, `
SELECT public_model_id,model_name,stable_release_id,canary_release_id,
       canary_basis_points,slot_version
  FROM runtime_change_slots
 WHERE model_name=$1`, modelName).Scan(
		&result.AssignedPublicModelID, &result.ModelName, &result.StableReleaseID,
		&result.CanaryReleaseID, &result.CanaryBasisPoints, &result.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentLifecycleSlotState{
			PublicModelID: publicModelID, ModelName: modelName,
		}, nil
	}
	if err != nil {
		return DeploymentLifecycleSlotState{}, lifecycleWriteError("read deployment lifecycle slot state", err)
	}
	if result.AssignedPublicModelID != nil && *result.AssignedPublicModelID != publicModelID {
		return DeploymentLifecycleSlotState{}, fmt.Errorf("%w: deployment lifecycle slot public model identity", ErrConflict)
	}
	result.Exists = true
	return result, nil
}

func deriveDeploymentLifecycleBeforeSlot(
	mutation DeploymentLifecycleMutation,
	target DeploymentRelease,
	parent *DeploymentRelease,
) (DeploymentLifecycleSlotState, error) {
	result := DeploymentLifecycleSlotState{
		PublicModelID: target.PublicModelID, ModelName: target.ModelName,
		Version: mutation.ExpectedSlotVersion,
	}
	switch mutation.Action {
	case DeploymentLifecycleActivateInitial:
		return result, nil
	case DeploymentLifecycleStartCanary:
		if parent == nil {
			return DeploymentLifecycleSlotState{}, fmt.Errorf("%w: canary parent receipt", ErrConflict)
		}
		result.Exists, result.AssignedPublicModelID = true, lifecycleReleaseIDPointer(target.PublicModelID)
		result.StableReleaseID = lifecycleReleaseIDPointer(parent.ID)
	case DeploymentLifecyclePromote, DeploymentLifecycleCancelCanary:
		if parent == nil {
			return DeploymentLifecycleSlotState{}, fmt.Errorf("%w: canary parent receipt", ErrConflict)
		}
		result.Exists = true
		result.AssignedPublicModelID = lifecycleReleaseIDPointer(target.PublicModelID)
		result.StableReleaseID = lifecycleReleaseIDPointer(parent.ID)
		result.CanaryReleaseID = lifecycleReleaseIDPointer(target.ID)
		result.CanaryBasisPoints = target.CanaryBasisPoints
	case DeploymentLifecycleRollbackStable:
		if parent == nil {
			return DeploymentLifecycleSlotState{}, fmt.Errorf("%w: rollback parent receipt", ErrConflict)
		}
		result.Exists, result.AssignedPublicModelID = true, lifecycleReleaseIDPointer(target.PublicModelID)
		result.StableReleaseID = lifecycleReleaseIDPointer(target.ID)
	default:
		return DeploymentLifecycleSlotState{}, fmt.Errorf("%w: deployment lifecycle action", ErrInvalid)
	}
	return result, nil
}

func deploymentLifecycleSlotReceipt(
	before DeploymentLifecycleSlotState,
	after DeploymentLifecycleSlotState,
) DeploymentLifecycleSlotReceipt {
	return DeploymentLifecycleSlotReceipt{
		PublicModelID: before.PublicModelID, ModelName: before.ModelName,
		BeforeStableReleaseID:   before.StableReleaseID,
		BeforeCanaryReleaseID:   before.CanaryReleaseID,
		BeforeCanaryBasisPoints: before.CanaryBasisPoints, BeforeVersion: before.Version,
		AfterStableReleaseID:   after.StableReleaseID,
		AfterCanaryReleaseID:   after.CanaryReleaseID,
		AfterCanaryBasisPoints: after.CanaryBasisPoints, AfterVersion: after.Version,
	}
}

func deploymentLifecycleReleaseReceipt(
	role string,
	before DeploymentRelease,
	after DeploymentRelease,
) DeploymentLifecycleReleaseReceipt {
	return DeploymentLifecycleReleaseReceipt{
		Role: role, ReleaseID: after.ID, ReleaseRef: after.ReleaseRef,
		CandidateHash: after.CandidateHash, RouteItemsHash: after.RouteItemsHash,
		BeforeStage: before.Stage, BeforeVersion: before.Version,
		AfterStage: after.Stage, AfterVersion: after.Version,
	}
}

func snapshotDeploymentLifecycleQualifications(
	ctx context.Context,
	store deploymentManifestStore,
	releaseRole string,
	release DeploymentRelease,
) ([]DeploymentLifecycleQualificationReceipt, error) {
	rows, err := store.Query(ctx, `
SELECT item.ordinal,item.deployment_id,item.operation_id,
       item.expected_connection_revision,item.expected_deployment_revision,
       item.expected_operation_revision,
       deployment.connection_id,connection.config_revision,
	       deployment.config_revision,operation.revision,
	       qualification.approval_id,qualification.probe_id,
	       qualification.https_origin,item.public_path,
	       item.executor_contract_version,
       qualification.approved_connection_revision,
       qualification.approved_deployment_revision,
       qualification.approved_operation_revision,
       qualification.qualification_state,qualification.qualified
  FROM runtime_route_revision_items item
  JOIN model_deployments deployment ON deployment.id=item.deployment_id
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=item.deployment_id
   AND operation.operation_id=item.operation_id
  LEFT JOIN model_operation_execution_qualifications qualification
    ON qualification.deployment_id=item.deployment_id
   AND qualification.operation_id=item.operation_id
 WHERE item.release_id=$1
 ORDER BY item.ordinal`, release.ID)
	if err != nil {
		return nil, lifecycleWriteError("read deployment lifecycle qualification snapshot", err)
	}
	defer rows.Close()
	result := make([]DeploymentLifecycleQualificationReceipt, 0, release.RouteItemsCount)
	for rows.Next() {
		var item DeploymentLifecycleQualificationReceipt
		var approvalID, probeID sql.NullInt64
		var httpsOrigin, publicPath, executor, state sql.NullString
		var approvedConnection, approvedDeployment, approvedOperation sql.NullInt64
		var qualified sql.NullBool
		if err := rows.Scan(
			&item.Ordinal, &item.DeploymentID, &item.OperationID,
			&item.ExpectedConnectionRevision, &item.ExpectedDeploymentRevision,
			&item.ExpectedOperationRevision, &item.ConnectionID,
			&item.CurrentConnectionRevision, &item.CurrentDeploymentRevision,
			&item.CurrentOperationRevision, &approvalID, &probeID,
			&httpsOrigin, &publicPath, &executor,
			&approvedConnection, &approvedDeployment, &approvedOperation,
			&state, &qualified,
		); err != nil {
			return nil, lifecycleWriteError("scan deployment lifecycle qualification snapshot", err)
		}
		item.ReleaseID, item.ReleaseRole = release.ID, releaseRole
		item.QualificationRequired = item.OperationID == "openai.responses.v1" || item.OperationID == "anthropic.messages.v1"
		if !item.QualificationRequired {
			item.Qualified, item.QualificationState = true, "not_required"
		} else {
			item.Qualified = qualified.Valid && qualified.Bool
			item.QualificationState = state.String
			if item.QualificationState == "" {
				item.QualificationState = "qualification_missing"
			}
			item.ApprovalID = nullableLifecycleInt64(approvalID)
			item.ProbeID = nullableLifecycleInt64(probeID)
			item.HTTPSOrigin, item.PublicPath = httpsOrigin.String, publicPath.String
			item.ExecutorContractVersion = executor.String
			item.ApprovedConnectionRevision = approvedConnection.Int64
			item.ApprovedDeploymentRevision = approvedDeployment.Int64
			item.ApprovedOperationRevision = approvedOperation.Int64
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, lifecycleWriteError("iterate deployment lifecycle qualification snapshot", err)
	}
	if len(result) != release.RouteItemsCount {
		return nil, fmt.Errorf("%w: lifecycle qualification snapshot route count", ErrConflict)
	}
	return result, nil
}

func insertDeploymentLifecycleReleaseReceipts(
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	releases []DeploymentLifecycleReleaseReceipt,
) error {
	for _, release := range releases {
		if _, err := tx.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipt_releases (
  receipt_id,release_role,release_id,release_ref,candidate_hash,route_items_hash,
  before_stage,before_version,after_stage,after_version)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, receiptID, release.Role,
			release.ReleaseID, release.ReleaseRef, release.CandidateHash, release.RouteItemsHash,
			release.BeforeStage, release.BeforeVersion, release.AfterStage, release.AfterVersion,
		); err != nil {
			return lifecycleWriteError("insert deployment lifecycle release receipt", err)
		}
	}
	return nil
}

func insertDeploymentLifecycleQualificationReceipts(
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	qualifications []DeploymentLifecycleQualificationReceipt,
) error {
	for _, item := range qualifications {
		if _, err := tx.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipt_qualifications (
  receipt_id,release_id,release_role,ordinal,deployment_id,operation_id,
  qualification_required,qualified,qualification_state,approval_id,probe_id,
  connection_id,https_origin,public_path,executor_contract_version,
  expected_connection_revision,expected_deployment_revision,expected_operation_revision,
  current_connection_revision,current_deployment_revision,current_operation_revision,
  approved_connection_revision,approved_deployment_revision,approved_operation_revision)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
        $19,$20,$21,$22,$23,$24)`, receiptID, item.ReleaseID, item.ReleaseRole,
			item.Ordinal, item.DeploymentID, item.OperationID,
			item.QualificationRequired, item.Qualified, item.QualificationState,
			item.ApprovalID, item.ProbeID, nullablePositiveInt64(item.ConnectionID),
			nullableStringValue(item.HTTPSOrigin), nullableStringValue(item.PublicPath),
			nullableStringValue(item.ExecutorContractVersion),
			item.ExpectedConnectionRevision, item.ExpectedDeploymentRevision,
			item.ExpectedOperationRevision, nullablePositiveInt64(item.CurrentConnectionRevision),
			nullablePositiveInt64(item.CurrentDeploymentRevision),
			nullablePositiveInt64(item.CurrentOperationRevision),
			nullablePositiveInt64(item.ApprovedConnectionRevision),
			nullablePositiveInt64(item.ApprovedDeploymentRevision),
			nullablePositiveInt64(item.ApprovedOperationRevision),
		); err != nil {
			return lifecycleWriteError("insert deployment lifecycle qualification receipt", err)
		}
	}
	return nil
}

func insertDeploymentLifecycleAudit(
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	command DeploymentLifecycleCommand,
	target DeploymentRelease,
	slot DeploymentLifecycleSlotReceipt,
	releases []DeploymentLifecycleReleaseReceipt,
	qualifications []DeploymentLifecycleQualificationReceipt,
	legacyPlanHash string,
	deploymentPlanHash string,
) (int64, error) {
	return insertDeploymentLifecycleAuditVersion(
		ctx, tx, receiptID, command, target, slot, releases, qualifications,
		legacyPlanHash, deploymentPlanHash, DeploymentLifecycleReceiptContractVersion, nil,
	)
}

func insertDeploymentLifecycleAuditVersion(
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	command DeploymentLifecycleCommand,
	target DeploymentRelease,
	slot DeploymentLifecycleSlotReceipt,
	releases []DeploymentLifecycleReleaseReceipt,
	qualifications []DeploymentLifecycleQualificationReceipt,
	legacyPlanHash string,
	deploymentPlanHash string,
	contractVersion string,
	extra map[string]interface{},
) (int64, error) {
	beforeSlot := map[string]interface{}{
		"stable_release_id":   slot.BeforeStableReleaseID,
		"canary_release_id":   slot.BeforeCanaryReleaseID,
		"canary_basis_points": slot.BeforeCanaryBasisPoints,
		"slot_version":        slot.BeforeVersion,
	}
	afterSlot := map[string]interface{}{
		"stable_release_id":   slot.AfterStableReleaseID,
		"canary_release_id":   slot.AfterCanaryReleaseID,
		"canary_basis_points": slot.AfterCanaryBasisPoints,
		"slot_version":        slot.AfterVersion,
	}
	if isLegacyDeploymentLifecycleAction(command.Mutation.Action) {
		beforeSlot["public_model_id"] = slot.BeforePublicModelID
		afterSlot["public_model_id"] = slot.AfterPublicModelID
	}
	payload := map[string]interface{}{
		"receipt_id":          receiptID,
		"contract_version":    contractVersion,
		"server_operation_id": command.ServerOperationID,
		"action":              command.Mutation.Action,
		"runtime_mutation":    "bounded_route_assignment",
		"before":              map[string]interface{}{"slot": beforeSlot},
		"after":               map[string]interface{}{"slot": afterSlot},
		"releases":            releases, "qualifications": qualifications,
	}
	if legacyPlanHash != "" {
		payload["legacy_plan_hash"] = legacyPlanHash
	}
	if deploymentPlanHash != "" {
		payload["deployment_plan_hash"] = deploymentPlanHash
	}
	for key, value := range extra {
		payload[key] = value
	}
	detail, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("%w: encode deployment lifecycle audit: %v", ErrUnavailable, err)
	}
	source := strings.TrimSpace(command.Audit.Source)
	if source == "" || source == "admin" {
		source = "admin_console"
	}
	action := "model_deployment.lifecycle." + string(command.Mutation.Action)
	var auditID int64
	err = tx.QueryRow(ctx, `
INSERT INTO audit_logs (
  org_id,user_id,action,resource,resource_id,detail,ip,user_agent,
  request_id,source,before_state,after_state,client_request_id,created_at)
VALUES ($1,$2,$3,'model_deployment_release',$4,$5::jsonb,$6,$7,$8,$9,
        $10::jsonb,$11::jsonb,$12,transaction_timestamp())
RETURNING id`, command.CustomerOrgID, command.ActorUserID, action,
		strconv.FormatInt(target.ID, 10), string(detail), command.Audit.IP,
		command.Audit.UserAgent, command.Audit.RequestID, source,
		string(detail), string(detail), nullableStringValue(command.Audit.ClientRequestID),
	).Scan(&auditID)
	if err != nil {
		return 0, lifecycleWriteError("insert deployment lifecycle audit", err)
	}
	return auditID, nil
}

func completeDeploymentLifecycleReceipt(
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	target DeploymentRelease,
	slot DeploymentLifecycleSlotReceipt,
	legacyPlanHash string,
	deploymentPlanHash string,
	auditID int64,
) (time.Time, error) {
	var completedAt time.Time
	err := tx.QueryRow(ctx, `
UPDATE model_deployment_lifecycle_receipts
   SET target_release_id=$2,public_model_id=$3,status='completed',
	       original_http_status=200,
	       before_slot_public_model_id=$4,before_stable_release_id=$5,
	       before_canary_release_id=$6,before_canary_basis_points=$7,
	       before_slot_version=$8,after_slot_public_model_id=$9,
	       after_stable_release_id=$10,after_canary_release_id=$11,
	       after_canary_basis_points=$12,after_slot_version=$13,
	       legacy_plan_hash=$14,deployment_plan_hash=$15,
	       audit_log_id=$16,completed_at=transaction_timestamp()
 WHERE receipt_id=$1 AND status='in_progress'
	 RETURNING completed_at`, receiptID, target.ID, target.PublicModelID,
		slot.BeforePublicModelID, slot.BeforeStableReleaseID,
		slot.BeforeCanaryReleaseID, slot.BeforeCanaryBasisPoints, slot.BeforeVersion,
		slot.AfterPublicModelID, slot.AfterStableReleaseID, slot.AfterCanaryReleaseID,
		slot.AfterCanaryBasisPoints, slot.AfterVersion,
		nullableStringValue(legacyPlanHash), nullableStringValue(deploymentPlanHash), auditID,
	).Scan(&completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("%w: lifecycle receipt claim lost", ErrConflict)
	}
	if err != nil {
		return time.Time{}, lifecycleWriteError("complete deployment lifecycle receipt", err)
	}
	return completedAt.UTC(), nil
}

func readDeploymentLifecycleReceipt(
	ctx context.Context,
	store deploymentManifestStore,
	receiptID int64,
) (DeploymentLifecycleReceipt, error) {
	var result DeploymentLifecycleReceipt
	var action string
	var legacyPlanHash, deploymentPlanHash sql.NullString
	err := store.QueryRow(ctx, `
SELECT contract_version,receipt_id,action,target_release_ref,customer_org_id,
       actor_user_id,server_operation_id,original_http_status,reason,
	       public_model_id,before_slot_public_model_id,before_stable_release_id,
	       before_canary_release_id,before_canary_basis_points,before_slot_version,
	       after_slot_public_model_id,after_stable_release_id,after_canary_release_id,
	       after_canary_basis_points,after_slot_version,
	       legacy_plan_hash,deployment_plan_hash,created_at,completed_at
  FROM model_deployment_lifecycle_receipts
 WHERE receipt_id=$1 AND status='completed'`, receiptID).Scan(
		&result.ContractVersion, &result.ReceiptID, &action, &result.TargetReleaseRef,
		&result.CustomerOrgID, &result.ActorUserID, &result.ServerOperationID,
		&result.OriginalHTTPStatus, &result.Reason, &result.Slot.PublicModelID,
		&result.Slot.BeforePublicModelID, &result.Slot.BeforeStableReleaseID,
		&result.Slot.BeforeCanaryReleaseID,
		&result.Slot.BeforeCanaryBasisPoints, &result.Slot.BeforeVersion,
		&result.Slot.AfterPublicModelID, &result.Slot.AfterStableReleaseID,
		&result.Slot.AfterCanaryReleaseID,
		&result.Slot.AfterCanaryBasisPoints, &result.Slot.AfterVersion,
		&legacyPlanHash, &deploymentPlanHash,
		&result.CreatedAt, &result.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentLifecycleReceipt{}, ErrNotFound
	}
	if err != nil {
		return DeploymentLifecycleReceipt{}, lifecycleWriteError("read deployment lifecycle receipt", err)
	}
	// PostgreSQL timestamps represent instants, but pgx may scan them using the
	// session location. Keep the durable replay body independent of that session
	// setting, matching the UTC timestamps returned by the original write path.
	result.CreatedAt = result.CreatedAt.UTC()
	result.CompletedAt = result.CompletedAt.UTC()
	result.Action = DeploymentLifecycleAction(action)
	result.LegacyPlanHash, result.DeploymentPlanHash = legacyPlanHash.String, deploymentPlanHash.String
	if err := store.QueryRow(ctx, `
SELECT release.model_name
  FROM model_deployment_lifecycle_receipt_releases evidence
  JOIN runtime_change_releases release ON release.id=evidence.release_id
 WHERE evidence.receipt_id=$1 AND evidence.release_role='target'`,
		receiptID).Scan(&result.Slot.ModelName); err != nil {
		return DeploymentLifecycleReceipt{}, lifecycleWriteError("read deployment lifecycle receipt model", err)
	}

	releaseRows, err := store.Query(ctx, `
SELECT release_role,release_id,release_ref,candidate_hash,route_items_hash,
       before_stage,before_version,after_stage,after_version
  FROM model_deployment_lifecycle_receipt_releases
 WHERE receipt_id=$1
 ORDER BY CASE release_role WHEN 'target' THEN 0 ELSE 1 END`, receiptID)
	if err != nil {
		return DeploymentLifecycleReceipt{}, lifecycleWriteError("read deployment lifecycle release receipts", err)
	}
	for releaseRows.Next() {
		var item DeploymentLifecycleReleaseReceipt
		if err := releaseRows.Scan(
			&item.Role, &item.ReleaseID, &item.ReleaseRef, &item.CandidateHash,
			&item.RouteItemsHash, &item.BeforeStage, &item.BeforeVersion,
			&item.AfterStage, &item.AfterVersion,
		); err != nil {
			releaseRows.Close()
			return DeploymentLifecycleReceipt{}, lifecycleWriteError("scan deployment lifecycle release receipts", err)
		}
		result.Releases = append(result.Releases, item)
	}
	if err := releaseRows.Err(); err != nil {
		releaseRows.Close()
		return DeploymentLifecycleReceipt{}, lifecycleWriteError("iterate deployment lifecycle release receipts", err)
	}
	releaseRows.Close()

	qualificationRows, err := store.Query(ctx, `
SELECT release_id,release_role,ordinal,deployment_id,operation_id,
       qualification_required,qualified,qualification_state,approval_id,probe_id,
       connection_id,https_origin,public_path,executor_contract_version,
       expected_connection_revision,expected_deployment_revision,expected_operation_revision,
       current_connection_revision,current_deployment_revision,current_operation_revision,
       approved_connection_revision,approved_deployment_revision,approved_operation_revision
  FROM model_deployment_lifecycle_receipt_qualifications
 WHERE receipt_id=$1
 ORDER BY CASE release_role WHEN 'target' THEN 0 ELSE 1 END,ordinal`, receiptID)
	if err != nil {
		return DeploymentLifecycleReceipt{}, lifecycleWriteError("read deployment lifecycle qualification receipts", err)
	}
	for qualificationRows.Next() {
		var item DeploymentLifecycleQualificationReceipt
		var approvalID, probeID, connectionID sql.NullInt64
		var httpsOrigin, publicPath, executor sql.NullString
		var currentConnection, currentDeployment, currentOperation sql.NullInt64
		var approvedConnection, approvedDeployment, approvedOperation sql.NullInt64
		if err := qualificationRows.Scan(
			&item.ReleaseID, &item.ReleaseRole, &item.Ordinal, &item.DeploymentID,
			&item.OperationID, &item.QualificationRequired, &item.Qualified,
			&item.QualificationState, &approvalID, &probeID, &connectionID,
			&httpsOrigin, &publicPath, &executor,
			&item.ExpectedConnectionRevision, &item.ExpectedDeploymentRevision,
			&item.ExpectedOperationRevision, &currentConnection, &currentDeployment,
			&currentOperation, &approvedConnection, &approvedDeployment, &approvedOperation,
		); err != nil {
			qualificationRows.Close()
			return DeploymentLifecycleReceipt{}, lifecycleWriteError("scan deployment lifecycle qualification receipts", err)
		}
		item.ApprovalID, item.ProbeID = nullableLifecycleInt64(approvalID), nullableLifecycleInt64(probeID)
		item.ConnectionID = connectionID.Int64
		item.HTTPSOrigin, item.PublicPath = httpsOrigin.String, publicPath.String
		item.ExecutorContractVersion = executor.String
		item.CurrentConnectionRevision, item.CurrentDeploymentRevision = currentConnection.Int64, currentDeployment.Int64
		item.CurrentOperationRevision = currentOperation.Int64
		item.ApprovedConnectionRevision, item.ApprovedDeploymentRevision = approvedConnection.Int64, approvedDeployment.Int64
		item.ApprovedOperationRevision = approvedOperation.Int64
		result.Qualifications = append(result.Qualifications, item)
	}
	if err := qualificationRows.Err(); err != nil {
		qualificationRows.Close()
		return DeploymentLifecycleReceipt{}, lifecycleWriteError("iterate deployment lifecycle qualification receipts", err)
	}
	qualificationRows.Close()
	if len(result.Releases) == 0 || len(result.Qualifications) == 0 {
		return DeploymentLifecycleReceipt{}, fmt.Errorf("%w: incomplete lifecycle receipt", ErrUnavailable)
	}
	return result, nil
}

func lifecycleReleaseIDPointer(value int64) *int64 { return &value }

func nullableLifecycleInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullablePositiveInt64(value int64) interface{} {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableStringValue(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}
