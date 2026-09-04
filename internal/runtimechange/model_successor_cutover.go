package runtimechange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/jackc/pgx/v5"
)

const (
	modelSuccessorLifecycleOperationScope         = "model_deployment.lifecycle.activate_initial.successor.v1"
	stagingModelSuccessorLifecycleOperationScope  = "model_deployment.lifecycle.activate_initial.staging-successor.v2"
	repeatedModelSuccessorLifecycleOperationScope = "model_deployment.lifecycle.activate_initial.primary-successor.v3"
)

type modelSuccessorContractIdentity struct {
	lifecycleVersion string
	childVersion     string
	operationScope   string
}

func successorContractIdentity(mutation ModelSuccessorCutoverMutation) modelSuccessorContractIdentity {
	if mutation.PredecessorAuthorityMode == ModelSuccessorPredecessorPrimaryAlias {
		return modelSuccessorContractIdentity{
			lifecycleVersion: RepeatedModelSuccessorLifecycleReceiptContractVersion,
			childVersion:     RepeatedModelSuccessorCutoverReceiptContractVersion,
			operationScope:   repeatedModelSuccessorLifecycleOperationScope,
		}
	}
	if mutation.PredecessorAuthorityMode == ModelSuccessorPredecessorNeverActivated {
		return modelSuccessorContractIdentity{
			lifecycleVersion: StagingModelSuccessorLifecycleReceiptContractVersion,
			childVersion:     StagingModelSuccessorCutoverReceiptContractVersion,
			operationScope:   stagingModelSuccessorLifecycleOperationScope,
		}
	}
	return modelSuccessorContractIdentity{
		lifecycleVersion: ModelSuccessorLifecycleReceiptContractVersion,
		childVersion:     ModelSuccessorCutoverReceiptContractVersion,
		operationScope:   modelSuccessorLifecycleOperationScope,
	}
}

type successorCutoverRegistryState struct {
	ModelSuccessorRegistryState
	OrgID int64
}

// ApplyModelSuccessorCutover atomically activates a successor's first stable
// release and creates or advances one primary stable request identity. Ordinary
// Alias CRUD remains unable to perform this coordinated lifecycle transition.
func (m *Manager) ApplyModelSuccessorCutover(
	ctx context.Context,
	command ModelSuccessorCutoverCommand,
) (ModelSuccessorCutoverReceipt, error) {
	command, err := NormalizeModelSuccessorCutoverCommand(command)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	tx, err := m.beginDeploymentLifecycleTransaction(ctx)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	defer tx.Rollback(ctx)
	contractIdentity := successorContractIdentity(command.Mutation)
	// Take governance writer locks before the receipt claim establishes the
	// repeatable-read snapshot. This closes absent-alias and model-set insert
	// races without inventing a second coordination protocol.
	if _, err := tx.Exec(ctx, `LOCK TABLE model_registry,model_aliases,model_set_items IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("lock model successor governance writers", err)
	}
	if err := lockInitialDeploymentCustomerRootSet(ctx, tx); err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}

	claimed, claim, err := claimModelSuccessorLifecycleReceipt(ctx, tx, command)
	if err != nil {
		if isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			_ = tx.Rollback(ctx)
			if recovered, ok, recoveryErr := m.recoverModelSuccessorCutoverReceipt(ctx, command); recoveryErr != nil {
				return ModelSuccessorCutoverReceipt{}, recoveryErr
			} else if ok {
				m.Invalidate()
				return recovered, nil
			}
		}
		return ModelSuccessorCutoverReceipt{}, err
	}
	if !claimed {
		if claim.RequestFingerprint != command.RequestFingerprint {
			return ModelSuccessorCutoverReceipt{}, ErrDeploymentLifecycleIdempotencyConflict
		}
		if claim.Status != "completed" {
			return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: successor cutover receipt is not terminal", ErrConflict)
		}
		receipt, err := readModelSuccessorCutoverReceipt(ctx, tx, claim.ReceiptID)
		if err != nil {
			return ModelSuccessorCutoverReceipt{}, err
		}
		receipt.Lifecycle.IdempotencyReplay = claim.ServerOperationID != command.ServerOperationID
		if err := tx.Commit(ctx); err != nil {
			if recovered, ok, recoveryErr := m.recoverModelSuccessorCutoverReceipt(ctx, command); recoveryErr != nil {
				return ModelSuccessorCutoverReceipt{}, recoveryErr
			} else if ok {
				return recovered, nil
			}
			return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("commit model successor cutover replay", err)
		}
		return receipt, nil
	}

	predecessorBefore, successorBefore, err := lockAndValidateSuccessorRegistryState(ctx, tx, command)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	if command.Mutation.PredecessorAuthorityMode != ModelSuccessorPredecessorPrimaryAlias {
		if err := requireModelSuccessorAliasAbsent(ctx, tx, command.CustomerOrgID, command.Mutation.StableRequestModel); err != nil {
			return ModelSuccessorCutoverReceipt{}, err
		}
	}
	if err := requireNoActiveSuccessorAliases(ctx, tx, command.CustomerOrgID, command.Mutation.SuccessorModelName); err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	predecessorAuthoritySnapshot := map[string]int64(nil)
	if command.Mutation.PredecessorAuthorityMode == ModelSuccessorPredecessorPrimaryAlias {
		predecessorAuthoritySnapshot, err = requirePrimaryAliasPredecessor(ctx, tx, command)
		if err != nil {
			return ModelSuccessorCutoverReceipt{}, err
		}
	}
	if command.Mutation.PredecessorAuthorityMode == ModelSuccessorPredecessorNeverActivated {
		predecessorAuthoritySnapshot, err = requireNeverActivatedSuccessorPredecessor(ctx, tx, command)
		if err != nil {
			return ModelSuccessorCutoverReceipt{}, err
		}
	}
	modelSets, err := lockModelSuccessorModelSets(ctx, tx, command)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	if err := insertModelSuccessorCutoverEvidence(
		ctx, tx, claim.ReceiptID, command, predecessorBefore, successorBefore,
		modelSets, predecessorAuthoritySnapshot,
	); err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}

	lifecycleCommand := modelSuccessorLifecycleCommand(command)
	targetBefore, err := readDeploymentRelease(ctx, tx, command.Mutation.ReleaseRef, false)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	if targetBefore.PublicModelID != command.Mutation.SuccessorPublicModelID ||
		targetBefore.ModelName != command.Mutation.SuccessorModelName || targetBefore.ParentStableReleaseID != nil {
		return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: successor release identity", ErrConflict)
	}
	targetAfter, err := m.applyDeploymentLifecycleMutationInTx(
		ctx, tx, lifecycleCommand, claim.ReceiptID, targetBefore.PublicModelID,
	)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	if err := applyModelSuccessorGovernanceState(ctx, tx, command, modelSets); err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}

	afterSlot, err := readDeploymentLifecycleSlotState(ctx, tx, targetAfter.PublicModelID, targetAfter.ModelName)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	if !afterSlot.Exists {
		return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: successor cutover produced no canonical slot", ErrUnavailable)
	}
	beforeSlot, err := deriveDeploymentLifecycleBeforeSlot(lifecycleCommand.Mutation, targetBefore, nil)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	slotReceipt := deploymentLifecycleSlotReceipt(beforeSlot, afterSlot)
	releaseReceipts := []DeploymentLifecycleReleaseReceipt{
		deploymentLifecycleReleaseReceipt("target", targetBefore, targetAfter),
	}
	qualificationReceipts, err := snapshotDeploymentLifecycleQualifications(ctx, tx, "target", targetAfter)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	if err := insertDeploymentLifecycleReleaseReceipts(ctx, tx, claim.ReceiptID, releaseReceipts); err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	if err := insertDeploymentLifecycleQualificationReceipts(ctx, tx, claim.ReceiptID, qualificationReceipts); err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	predecessorAfter := predecessorBefore.ModelSuccessorRegistryState
	predecessorAfter.Lifecycle, predecessorAfter.ApprovedCatalog = "retired", false
	successorAfter := successorBefore.ModelSuccessorRegistryState
	successorAfter.Lifecycle, successorAfter.ApprovedCatalog = "ga", true
	aliasAfter := ModelSuccessorAliasState{
		Alias: command.Mutation.StableRequestModel, TargetModel: command.Mutation.SuccessorModelName,
		IsPrimary: true, Status: "active",
	}
	successorAudit := map[string]interface{}{
		"contract_version":     contractIdentity.childVersion,
		"stable_request_model": command.Mutation.StableRequestModel,
		"predecessor_before":   predecessorBefore.ModelSuccessorRegistryState,
		"predecessor_after":    predecessorAfter,
		"successor_before":     successorBefore.ModelSuccessorRegistryState,
		"successor_after":      successorAfter,
		"alias_after":          aliasAfter,
		"model_set_count":      len(modelSets),
		"model_sets_before":    modelSuccessorModelSetsBeforeManifest(modelSets),
		"model_sets_after":     modelSuccessorModelSetsAfterManifest(modelSets),
	}
	if command.Mutation.PredecessorAuthorityMode != ModelSuccessorPredecessorPublished {
		successorAudit["predecessor_authority_mode"] = command.Mutation.PredecessorAuthorityMode
		successorAudit["predecessor_authority_snapshot"] = predecessorAuthoritySnapshot
	}
	extra := map[string]interface{}{"successor_cutover": successorAudit}
	auditID, err := insertDeploymentLifecycleAuditVersion(
		ctx, tx, claim.ReceiptID, lifecycleCommand, targetAfter, slotReceipt,
		releaseReceipts, qualificationReceipts, "", "",
		contractIdentity.lifecycleVersion, extra,
	)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	completedAt, err := completeDeploymentLifecycleReceipt(
		ctx, tx, claim.ReceiptID, targetAfter, slotReceipt, "", "", auditID,
	)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	lifecycleReceipt := DeploymentLifecycleReceipt{
		ContractVersion: contractIdentity.lifecycleVersion,
		ReceiptID:       claim.ReceiptID, Action: DeploymentLifecycleActivateInitial,
		TargetReleaseRef: targetAfter.ReleaseRef, CustomerOrgID: command.CustomerOrgID,
		ActorUserID: command.ActorUserID, ServerOperationID: command.ServerOperationID,
		OriginalHTTPStatus: 200, Reason: command.Mutation.Reason,
		Slot: slotReceipt, Releases: releaseReceipts, Qualifications: qualificationReceipts,
		CreatedAt: claim.CreatedAt.UTC(), CompletedAt: completedAt.UTC(),
	}
	receipt := ModelSuccessorCutoverReceipt{
		ContractVersion:              contractIdentity.childVersion,
		PredecessorAuthoritySnapshot: predecessorAuthoritySnapshot,
		Lifecycle:                    lifecycleReceipt, StableRequestModel: command.Mutation.StableRequestModel,
		PredecessorBefore: predecessorBefore.ModelSuccessorRegistryState,
		PredecessorAfter:  predecessorAfter, SuccessorBefore: successorBefore.ModelSuccessorRegistryState,
		SuccessorAfter: successorAfter, AliasAfter: aliasAfter, ModelSets: modelSets,
	}
	if command.Mutation.PredecessorAuthorityMode != ModelSuccessorPredecessorPublished {
		receipt.PredecessorAuthorityMode = command.Mutation.PredecessorAuthorityMode
	}
	if err := tx.Commit(ctx); err != nil {
		if recovered, ok, recoveryErr := m.recoverModelSuccessorCutoverReceipt(ctx, command); recoveryErr != nil {
			return ModelSuccessorCutoverReceipt{}, recoveryErr
		} else if ok {
			m.Invalidate()
			return recovered, nil
		}
		return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("commit model successor cutover", err)
	}
	m.Invalidate()
	return receipt, nil
}

func modelSuccessorLifecycleCommand(command ModelSuccessorCutoverCommand) DeploymentLifecycleCommand {
	return DeploymentLifecycleCommand{
		Mutation: DeploymentLifecycleMutation{
			Action: DeploymentLifecycleActivateInitial, ReleaseRef: command.Mutation.ReleaseRef,
			ExpectedReleaseVersion: command.Mutation.ExpectedReleaseVersion, Reason: command.Mutation.Reason,
		},
		CustomerOrgID: command.CustomerOrgID, ActorUserID: command.ActorUserID,
		ActorOrgID: command.ActorOrgID, IdempotencyKeyHash: command.IdempotencyKeyHash,
		RequestFingerprint: command.RequestFingerprint, ServerOperationID: command.ServerOperationID,
		Audit: command.Audit,
	}
}

func claimModelSuccessorLifecycleReceipt(
	ctx context.Context,
	tx pgx.Tx,
	command ModelSuccessorCutoverCommand,
) (bool, deploymentLifecycleReceiptClaim, error) {
	var claim deploymentLifecycleReceiptClaim
	contractIdentity := successorContractIdentity(command.Mutation)
	err := tx.QueryRow(ctx, `
INSERT INTO model_deployment_lifecycle_receipts (
  contract_version,customer_org_id,actor_user_id,actor_org_id,
  operation_scope,action,idempotency_key_hash,request_fingerprint,
  server_operation_id,target_release_ref,expected_release_version,
  expected_slot_version,requested_canary_basis_points,reason,
  successor_predecessor_public_model_id,successor_public_model_id,
  successor_stable_request_model)
VALUES ($1,$2,$3,$4,$5,'activate_initial',$6,$7,$8,$9,$10,0,0,$11,$12,$13,$14)
ON CONFLICT (customer_org_id,actor_user_id,operation_scope,idempotency_key_hash)
DO NOTHING
RETURNING receipt_id,request_fingerprint,server_operation_id,status,created_at`,
		contractIdentity.lifecycleVersion, command.CustomerOrgID,
		command.ActorUserID, command.ActorOrgID, contractIdentity.operationScope,
		command.IdempotencyKeyHash, command.RequestFingerprint, command.ServerOperationID,
		command.Mutation.ReleaseRef, command.Mutation.ExpectedReleaseVersion,
		command.Mutation.Reason, command.Mutation.PredecessorPublicModelID,
		command.Mutation.SuccessorPublicModelID, command.Mutation.StableRequestModel,
	).Scan(&claim.ReceiptID, &claim.RequestFingerprint, &claim.ServerOperationID, &claim.Status, &claim.CreatedAt)
	if err == nil {
		return true, claim, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		if isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			return false, deploymentLifecycleReceiptClaim{}, &deploymentLifecycleReceiptClaimRetryableError{
				operation: "claim model successor cutover receipt", cause: err,
			}
		}
		return false, deploymentLifecycleReceiptClaim{}, deploymentLifecycleReceiptClaimWriteError("claim model successor cutover receipt", err)
	}
	err = tx.QueryRow(ctx, `
SELECT receipt_id,request_fingerprint,server_operation_id,status,created_at
  FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND actor_user_id=$2 AND operation_scope=$3
   AND idempotency_key_hash=$4
	 FOR SHARE`, command.CustomerOrgID, command.ActorUserID, contractIdentity.operationScope,
		command.IdempotencyKeyHash,
	).Scan(&claim.ReceiptID, &claim.RequestFingerprint, &claim.ServerOperationID, &claim.Status, &claim.CreatedAt)
	if err != nil {
		if isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			return false, deploymentLifecycleReceiptClaim{}, &deploymentLifecycleReceiptClaimRetryableError{
				operation: "read model successor cutover receipt winner", cause: err,
			}
		}
		return false, deploymentLifecycleReceiptClaim{}, lifecycleWriteError("read model successor cutover receipt winner", err)
	}
	return false, claim, nil
}

func lockAndValidateSuccessorRegistryState(
	ctx context.Context,
	tx pgx.Tx,
	command ModelSuccessorCutoverCommand,
) (successorCutoverRegistryState, successorCutoverRegistryState, error) {
	ids := []int64{command.Mutation.PredecessorPublicModelID, command.Mutation.SuccessorPublicModelID}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rows, err := tx.Query(ctx, `
SELECT id,org_id,model_name,lifecycle,approved_catalog
  FROM model_registry
 WHERE org_id=$1 AND id=ANY($2::bigint[])
 ORDER BY id
 FOR UPDATE`, command.CustomerOrgID, ids)
	if err != nil {
		return successorCutoverRegistryState{}, successorCutoverRegistryState{}, lifecycleWriteError("lock successor registry state", err)
	}
	defer rows.Close()
	states := make(map[int64]successorCutoverRegistryState, 2)
	for rows.Next() {
		var state successorCutoverRegistryState
		if err := rows.Scan(&state.PublicModelID, &state.OrgID, &state.ModelName, &state.Lifecycle, &state.ApprovedCatalog); err != nil {
			return successorCutoverRegistryState{}, successorCutoverRegistryState{}, lifecycleWriteError("scan successor registry state", err)
		}
		states[state.PublicModelID] = state
	}
	if err := rows.Err(); err != nil {
		return successorCutoverRegistryState{}, successorCutoverRegistryState{}, lifecycleWriteError("iterate successor registry state", err)
	}
	predecessor, predecessorOK := states[command.Mutation.PredecessorPublicModelID]
	successor, successorOK := states[command.Mutation.SuccessorPublicModelID]
	if !predecessorOK || !successorOK || predecessor.OrgID != command.CustomerOrgID || successor.OrgID != command.CustomerOrgID ||
		predecessor.ModelName != command.Mutation.PredecessorModelName || predecessor.Lifecycle != command.Mutation.ExpectedPredecessorLifecycle ||
		predecessor.ApprovedCatalog != command.Mutation.ExpectedPredecessorApprovedCatalog ||
		successor.ModelName != command.Mutation.SuccessorModelName || successor.Lifecycle != command.Mutation.ExpectedSuccessorLifecycle ||
		successor.ApprovedCatalog != command.Mutation.ExpectedSuccessorApprovedCatalog {
		return successorCutoverRegistryState{}, successorCutoverRegistryState{}, fmt.Errorf("%w: model successor registry state drift", ErrConflict)
	}
	return predecessor, successor, nil
}

func requireModelSuccessorAliasAbsent(ctx context.Context, tx pgx.Tx, orgID int64, alias string) error {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM model_aliases WHERE org_id=$1 AND alias=$2 FOR UPDATE`, orgID, alias).Scan(&id)
	if err == nil {
		return fmt.Errorf("%w: stable request alias already exists", ErrConflict)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return lifecycleWriteError("read stable request alias", err)
	}
	return nil
}

func requireNoActiveSuccessorAliases(ctx context.Context, tx pgx.Tx, orgID int64, successorModel string) error {
	var alias string
	err := tx.QueryRow(ctx, `
SELECT alias FROM model_aliases
 WHERE org_id=$1 AND target_model=$2 AND status='active'
 ORDER BY alias LIMIT 1 FOR UPDATE`, orgID, successorModel).Scan(&alias)
	if err == nil {
		return fmt.Errorf("%w: successor already has an active alias", ErrConflict)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return lifecycleWriteError("read active successor aliases", err)
	}
	return nil
}

func requireNeverActivatedSuccessorPredecessor(
	ctx context.Context,
	tx pgx.Tx,
	command ModelSuccessorCutoverCommand,
) (map[string]int64, error) {
	var slotCount, authoritativeReleaseCount, activeAliasCount, completedSuccessorReceiptCount int64
	err := tx.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM runtime_change_slots slot
    WHERE slot.public_model_id=$1 OR slot.model_name=$2),
  (SELECT COUNT(*) FROM runtime_change_releases release
    WHERE release.public_model_id=$1
      AND (release.route_items_sealed_at IS NOT NULL
        OR release.stage IN ('validated','shadow','canary','active','rolled_back'))),
  (SELECT COUNT(*) FROM model_aliases alias
    WHERE alias.org_id=$3 AND alias.status='active'
      AND (alias.alias=$2 OR alias.target_model=$2)),
  (SELECT COUNT(*)
     FROM model_successor_cutover_receipts child
     JOIN model_deployment_lifecycle_receipts parent
       ON parent.receipt_id=child.lifecycle_receipt_id AND parent.status='completed'
    WHERE child.customer_org_id=$3
      AND (child.predecessor_public_model_id=$1 OR child.successor_public_model_id=$1))`,
		command.Mutation.PredecessorPublicModelID,
		command.Mutation.PredecessorModelName,
		command.CustomerOrgID,
	).Scan(&slotCount, &authoritativeReleaseCount, &activeAliasCount, &completedSuccessorReceiptCount)
	if err != nil {
		return nil, lifecycleWriteError("read never-activated successor predecessor authority", err)
	}
	snapshot := map[string]int64{
		"runtime_slot_count":                 slotCount,
		"sealed_or_executable_release_count": authoritativeReleaseCount,
		"active_alias_count":                 activeAliasCount,
		"completed_successor_receipt_count":  completedSuccessorReceiptCount,
	}
	if slotCount != 0 || authoritativeReleaseCount != 0 || activeAliasCount != 0 || completedSuccessorReceiptCount != 0 {
		return nil, fmt.Errorf("%w: staging predecessor has prior execution or successor authority", ErrConflict)
	}
	return snapshot, nil
}

func validNeverActivatedPredecessorAuthoritySnapshot(snapshot map[string]int64) bool {
	if len(snapshot) != 4 {
		return false
	}
	for _, key := range []string{
		"runtime_slot_count",
		"sealed_or_executable_release_count",
		"active_alias_count",
		"completed_successor_receipt_count",
	} {
		value, ok := snapshot[key]
		if !ok || value != 0 {
			return false
		}
	}
	return true
}

func lockModelSuccessorModelSets(
	ctx context.Context,
	tx pgx.Tx,
	command ModelSuccessorCutoverCommand,
) ([]ModelSuccessorModelSetReceipt, error) {
	rows, err := tx.Query(ctx, `
SELECT ms.id,EXISTS (
  SELECT 1 FROM model_set_items alias_item
   WHERE alias_item.model_set_id=ms.id AND alias_item.item_type='alias' AND alias_item.item_name=$3)
  FROM model_sets ms
  JOIN model_set_items model_item ON model_item.model_set_id=ms.id
 WHERE ms.org_id=$1 AND model_item.item_type='model' AND model_item.item_name=$2
 ORDER BY ms.id
 FOR UPDATE OF ms,model_item`, command.CustomerOrgID, command.Mutation.PredecessorModelName, command.Mutation.StableRequestModel)
	if err != nil {
		return nil, lifecycleWriteError("lock successor model sets", err)
	}
	defer rows.Close()
	result := make([]ModelSuccessorModelSetReceipt, 0)
	for rows.Next() {
		var item ModelSuccessorModelSetReceipt
		if err := rows.Scan(&item.ModelSetID, &item.BeforeAliasExists); err != nil {
			return nil, lifecycleWriteError("scan successor model set", err)
		}
		item.BeforeModelName = command.Mutation.PredecessorModelName
		item.AfterAliasName = command.Mutation.StableRequestModel
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, lifecycleWriteError("iterate successor model sets", err)
	}
	if len(result) != len(command.Mutation.ExpectedModelSets) {
		return nil, fmt.Errorf("%w: model-set snapshot drift", ErrConflict)
	}
	for index, item := range result {
		expected := command.Mutation.ExpectedModelSets[index]
		if item.ModelSetID != expected.ModelSetID || item.BeforeAliasExists != expected.BeforeAliasExists {
			return nil, fmt.Errorf("%w: model-set snapshot drift", ErrConflict)
		}
	}
	return result, nil
}

func modelSuccessorModelSetsBeforeManifest(items []ModelSuccessorModelSetReceipt) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]interface{}{
			"model_set_id": item.ModelSetID, "model": item.BeforeModelName, "alias_exists": item.BeforeAliasExists,
		})
	}
	return result
}

func modelSuccessorModelSetsAfterManifest(items []ModelSuccessorModelSetReceipt) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]interface{}{
			"model_set_id": item.ModelSetID, "alias": item.AfterAliasName,
		})
	}
	return result
}

func insertModelSuccessorCutoverEvidence(
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	command ModelSuccessorCutoverCommand,
	predecessor successorCutoverRegistryState,
	successor successorCutoverRegistryState,
	modelSets []ModelSuccessorModelSetReceipt,
	predecessorAuthoritySnapshot map[string]int64,
) error {
	contractIdentity := successorContractIdentity(command.Mutation)
	beforeManifest, err := json.Marshal(modelSuccessorModelSetsBeforeManifest(modelSets))
	if err != nil {
		return lifecycleWriteError("marshal model successor before manifest", err)
	}
	afterManifest, err := json.Marshal(modelSuccessorModelSetsAfterManifest(modelSets))
	if err != nil {
		return lifecycleWriteError("marshal model successor after manifest", err)
	}
	var authorityMode interface{}
	var authoritySnapshot interface{}
	if command.Mutation.PredecessorAuthorityMode != ModelSuccessorPredecessorPublished {
		snapshot, marshalErr := json.Marshal(predecessorAuthoritySnapshot)
		if marshalErr != nil {
			return lifecycleWriteError("marshal model successor predecessor authority", marshalErr)
		}
		authorityMode = command.Mutation.PredecessorAuthorityMode
		authoritySnapshot = string(snapshot)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO model_successor_cutover_receipts (
  lifecycle_receipt_id,contract_version,customer_org_id,stable_request_model,
  predecessor_public_model_id,predecessor_model_name,
  predecessor_before_lifecycle,predecessor_before_approved_catalog,
  predecessor_after_lifecycle,predecessor_after_approved_catalog,
  successor_public_model_id,successor_model_name,
  successor_before_lifecycle,successor_before_approved_catalog,
  successor_after_lifecycle,successor_after_approved_catalog,
  alias_before_exists,alias_after_target_model,alias_after_status,alias_after_is_primary,
  model_set_count,model_sets_before,model_sets_after,
  predecessor_authority_mode,predecessor_authority_snapshot)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'retired',FALSE,$9,$10,$11,$12,'ga',TRUE,
        $18,$10,'active',TRUE,$13,$14::jsonb,$15::jsonb,$16,$17::jsonb)`,
		receiptID, contractIdentity.childVersion, command.CustomerOrgID,
		command.Mutation.StableRequestModel, predecessor.PublicModelID, predecessor.ModelName,
		predecessor.Lifecycle, predecessor.ApprovedCatalog, successor.PublicModelID,
		successor.ModelName, successor.Lifecycle, successor.ApprovedCatalog,
		len(modelSets), string(beforeManifest), string(afterManifest), authorityMode, authoritySnapshot,
		command.Mutation.PredecessorAuthorityMode == ModelSuccessorPredecessorPrimaryAlias,
	); err != nil {
		return lifecycleWriteError("insert model successor cutover evidence", err)
	}
	for _, item := range modelSets {
		if _, err := tx.Exec(ctx, `
INSERT INTO model_successor_cutover_receipt_model_sets (
  lifecycle_receipt_id,model_set_id,before_model_name,before_alias_exists,after_alias_name)
VALUES ($1,$2,$3,$4,$5)`, receiptID, item.ModelSetID, item.BeforeModelName,
			item.BeforeAliasExists, item.AfterAliasName); err != nil {
			return lifecycleWriteError("insert model successor model-set evidence", err)
		}
	}
	return nil
}

func applyModelSuccessorGovernanceState(
	ctx context.Context,
	tx pgx.Tx,
	command ModelSuccessorCutoverCommand,
	modelSets []ModelSuccessorModelSetReceipt,
) error {
	tag, err := tx.Exec(ctx, `
UPDATE model_registry
   SET lifecycle='retired',approved_catalog=FALSE,updated_at=transaction_timestamp()
 WHERE id=$1 AND org_id=$2 AND model_name=$3 AND lifecycle=$4 AND approved_catalog=$5`,
		command.Mutation.PredecessorPublicModelID, command.CustomerOrgID,
		command.Mutation.PredecessorModelName, command.Mutation.ExpectedPredecessorLifecycle,
		command.Mutation.ExpectedPredecessorApprovedCatalog)
	if err != nil {
		return lifecycleWriteError("retire successor predecessor", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: predecessor registry state changed", ErrConflict)
	}
	tag, err = tx.Exec(ctx, `
UPDATE model_registry
   SET lifecycle='ga',approved_catalog=TRUE,updated_at=transaction_timestamp()
 WHERE id=$1 AND org_id=$2 AND model_name=$3 AND lifecycle=$4 AND approved_catalog=$5`,
		command.Mutation.SuccessorPublicModelID, command.CustomerOrgID,
		command.Mutation.SuccessorModelName, command.Mutation.ExpectedSuccessorLifecycle,
		command.Mutation.ExpectedSuccessorApprovedCatalog)
	if err != nil {
		return lifecycleWriteError("activate successor registry", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: successor registry state changed", ErrConflict)
	}
	if command.Mutation.PredecessorAuthorityMode == ModelSuccessorPredecessorPrimaryAlias {
		tag, err = tx.Exec(ctx, `UPDATE model_aliases SET target_model=$4,updated_at=transaction_timestamp()
 WHERE id=$1 AND org_id=$2 AND alias=$3 AND target_model=$5 AND status='active' AND is_primary`,
			command.Mutation.ExpectedPrimaryAliasID, command.CustomerOrgID, command.Mutation.StableRequestModel,
			command.Mutation.SuccessorModelName, command.Mutation.PredecessorModelName)
		if err != nil {
			return lifecycleWriteError("advance primary successor alias", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: primary alias before-state drift", ErrConflict)
		}
	} else if _, err := tx.Exec(ctx, `
INSERT INTO model_aliases (
  org_id,alias,target_model,display_name,description,status,is_primary,created_at,updated_at)
VALUES ($1,$2,$3,$4,'Immutable successor stable request identity','active',TRUE,
        transaction_timestamp(),transaction_timestamp())`, command.CustomerOrgID,
		command.Mutation.StableRequestModel, command.Mutation.SuccessorModelName,
		command.Mutation.StableRequestModel); err != nil {
		return lifecycleWriteError("create primary successor alias", err)
	}
	for _, item := range modelSets {
		if _, err := tx.Exec(ctx, `
INSERT INTO model_set_items (model_set_id,item_type,item_name,created_at)
VALUES ($1,'alias',$2,transaction_timestamp())
ON CONFLICT DO NOTHING`, item.ModelSetID, command.Mutation.StableRequestModel); err != nil {
			return lifecycleWriteError("insert successor model-set alias", err)
		}
		tag, err := tx.Exec(ctx, `
DELETE FROM model_set_items
 WHERE model_set_id=$1 AND item_type='model' AND item_name=$2`,
			item.ModelSetID, command.Mutation.PredecessorModelName)
		if err != nil {
			return lifecycleWriteError("remove predecessor model-set item", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: predecessor model-set state changed", ErrConflict)
		}
	}
	return nil
}

func readModelSuccessorCutoverReceipt(
	ctx context.Context,
	store deploymentManifestStore,
	receiptID int64,
) (ModelSuccessorCutoverReceipt, error) {
	lifecycle, err := readDeploymentLifecycleReceipt(ctx, store, receiptID)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, err
	}
	var operationScope string
	if err := store.QueryRow(ctx, `SELECT operation_scope FROM model_deployment_lifecycle_receipts WHERE receipt_id=$1`, receiptID).Scan(&operationScope); err != nil {
		return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("read model successor lifecycle scope", err)
	}
	var expectedChildVersion, expectedAuthorityMode string
	switch {
	case lifecycle.ContractVersion == ModelSuccessorLifecycleReceiptContractVersion && operationScope == modelSuccessorLifecycleOperationScope:
		expectedChildVersion = ModelSuccessorCutoverReceiptContractVersion
		expectedAuthorityMode = ModelSuccessorPredecessorPublished
	case lifecycle.ContractVersion == StagingModelSuccessorLifecycleReceiptContractVersion && operationScope == stagingModelSuccessorLifecycleOperationScope:
		expectedChildVersion = StagingModelSuccessorCutoverReceiptContractVersion
		expectedAuthorityMode = ModelSuccessorPredecessorNeverActivated
	case lifecycle.ContractVersion == RepeatedModelSuccessorLifecycleReceiptContractVersion && operationScope == repeatedModelSuccessorLifecycleOperationScope:
		expectedChildVersion = RepeatedModelSuccessorCutoverReceiptContractVersion
		expectedAuthorityMode = ModelSuccessorPredecessorPrimaryAlias
	default:
		return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: model successor lifecycle receipt identity", ErrUnavailable)
	}
	if lifecycle.Action != DeploymentLifecycleActivateInitial {
		return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: model successor lifecycle receipt identity", ErrUnavailable)
	}
	result := ModelSuccessorCutoverReceipt{Lifecycle: lifecycle}
	var modelSetCount int
	var aliasBeforeExists bool
	var storedAuthorityMode *string
	var storedAuthoritySnapshot []byte
	err = store.QueryRow(ctx, `
SELECT contract_version,stable_request_model,
       predecessor_public_model_id,predecessor_model_name,
       predecessor_before_lifecycle,predecessor_before_approved_catalog,
       predecessor_after_lifecycle,predecessor_after_approved_catalog,
       successor_public_model_id,successor_model_name,
       successor_before_lifecycle,successor_before_approved_catalog,
       successor_after_lifecycle,successor_after_approved_catalog,
       alias_before_exists,alias_after_target_model,alias_after_status,alias_after_is_primary,
       model_set_count,predecessor_authority_mode,predecessor_authority_snapshot
  FROM model_successor_cutover_receipts
 WHERE lifecycle_receipt_id=$1`, receiptID).Scan(
		&result.ContractVersion, &result.StableRequestModel,
		&result.PredecessorBefore.PublicModelID, &result.PredecessorBefore.ModelName,
		&result.PredecessorBefore.Lifecycle, &result.PredecessorBefore.ApprovedCatalog,
		&result.PredecessorAfter.Lifecycle, &result.PredecessorAfter.ApprovedCatalog,
		&result.SuccessorBefore.PublicModelID, &result.SuccessorBefore.ModelName,
		&result.SuccessorBefore.Lifecycle, &result.SuccessorBefore.ApprovedCatalog,
		&result.SuccessorAfter.Lifecycle, &result.SuccessorAfter.ApprovedCatalog,
		&aliasBeforeExists, &result.AliasAfter.TargetModel, &result.AliasAfter.Status,
		&result.AliasAfter.IsPrimary, &modelSetCount, &storedAuthorityMode, &storedAuthoritySnapshot,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: missing model successor child receipt", ErrUnavailable)
	}
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("read model successor child receipt", err)
	}
	result.PredecessorAfter.PublicModelID, result.PredecessorAfter.ModelName = result.PredecessorBefore.PublicModelID, result.PredecessorBefore.ModelName
	result.SuccessorAfter.PublicModelID, result.SuccessorAfter.ModelName = result.SuccessorBefore.PublicModelID, result.SuccessorBefore.ModelName
	result.AliasAfter.Alias = result.StableRequestModel
	if result.ContractVersion != expectedChildVersion || aliasBeforeExists != (expectedAuthorityMode == ModelSuccessorPredecessorPrimaryAlias) {
		return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: invalid model successor child receipt", ErrUnavailable)
	}
	if expectedAuthorityMode == ModelSuccessorPredecessorPublished {
		if storedAuthorityMode != nil || len(storedAuthoritySnapshot) != 0 {
			return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: published successor receipt has staging authority evidence", ErrUnavailable)
		}
	} else if expectedAuthorityMode == ModelSuccessorPredecessorNeverActivated {
		if storedAuthorityMode == nil || *storedAuthorityMode != expectedAuthorityMode ||
			json.Unmarshal(storedAuthoritySnapshot, &result.PredecessorAuthoritySnapshot) != nil ||
			!validNeverActivatedPredecessorAuthoritySnapshot(result.PredecessorAuthoritySnapshot) {
			return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: invalid staging predecessor authority evidence", ErrUnavailable)
		}
		result.PredecessorAuthorityMode = expectedAuthorityMode
	} else {
		if storedAuthorityMode == nil || *storedAuthorityMode != expectedAuthorityMode ||
			json.Unmarshal(storedAuthoritySnapshot, &result.PredecessorAuthoritySnapshot) != nil ||
			!validPrimaryAliasAuthoritySnapshot(result.PredecessorAuthoritySnapshot) {
			return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: invalid primary alias authority evidence", ErrUnavailable)
		}
		result.PredecessorAuthorityMode = expectedAuthorityMode
	}
	rows, err := store.Query(ctx, `
SELECT model_set_id,before_model_name,before_alias_exists,after_alias_name
  FROM model_successor_cutover_receipt_model_sets
 WHERE lifecycle_receipt_id=$1 ORDER BY model_set_id`, receiptID)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("read model successor model-set receipts", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item ModelSuccessorModelSetReceipt
		if err := rows.Scan(&item.ModelSetID, &item.BeforeModelName, &item.BeforeAliasExists, &item.AfterAliasName); err != nil {
			return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("scan model successor model-set receipt", err)
		}
		result.ModelSets = append(result.ModelSets, item)
	}
	if err := rows.Err(); err != nil {
		return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("iterate model successor model-set receipts", err)
	}
	if len(result.ModelSets) != modelSetCount {
		return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: incomplete model successor model-set receipts", ErrUnavailable)
	}
	beforeManifest, err := json.Marshal(modelSuccessorModelSetsBeforeManifest(result.ModelSets))
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("marshal recovered model successor before manifest", err)
	}
	afterManifest, err := json.Marshal(modelSuccessorModelSetsAfterManifest(result.ModelSets))
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("marshal recovered model successor after manifest", err)
	}
	var storedBefore, storedAfter []byte
	err = store.QueryRow(ctx, `
SELECT model_sets_before,model_sets_after
  FROM model_successor_cutover_receipts
 WHERE lifecycle_receipt_id=$1`, receiptID).Scan(&storedBefore, &storedAfter)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, lifecycleWriteError("read model successor manifests", err)
	}
	var expectedBefore, expectedAfter, actualBefore, actualAfter interface{}
	if json.Unmarshal(beforeManifest, &expectedBefore) != nil || json.Unmarshal(afterManifest, &expectedAfter) != nil ||
		json.Unmarshal(storedBefore, &actualBefore) != nil || json.Unmarshal(storedAfter, &actualAfter) != nil ||
		!reflect.DeepEqual(expectedBefore, actualBefore) || !reflect.DeepEqual(expectedAfter, actualAfter) {
		return ModelSuccessorCutoverReceipt{}, fmt.Errorf("%w: inconsistent model successor manifests", ErrUnavailable)
	}
	return result, nil
}

func (m *Manager) recoverModelSuccessorCutoverReceipt(
	ctx context.Context,
	command ModelSuccessorCutoverCommand,
) (ModelSuccessorCutoverReceipt, bool, error) {
	var claim deploymentLifecycleReceiptClaim
	contractIdentity := successorContractIdentity(command.Mutation)
	err := m.db.QueryRow(ctx, `
SELECT receipt_id,request_fingerprint,server_operation_id,status,created_at
  FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND actor_user_id=$2 AND operation_scope=$3
   AND idempotency_key_hash=$4`, command.CustomerOrgID, command.ActorUserID,
		contractIdentity.operationScope, command.IdempotencyKeyHash,
	).Scan(&claim.ReceiptID, &claim.RequestFingerprint, &claim.ServerOperationID, &claim.Status, &claim.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModelSuccessorCutoverReceipt{}, false, nil
	}
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, false, lifecycleWriteError("recover model successor cutover receipt", err)
	}
	if claim.RequestFingerprint != command.RequestFingerprint {
		return ModelSuccessorCutoverReceipt{}, false, ErrDeploymentLifecycleIdempotencyConflict
	}
	if claim.Status != "completed" {
		return ModelSuccessorCutoverReceipt{}, false, nil
	}
	receipt, err := readModelSuccessorCutoverReceipt(ctx, m.db, claim.ReceiptID)
	if err != nil {
		return ModelSuccessorCutoverReceipt{}, false, err
	}
	receipt.Lifecycle.IdempotencyReplay = claim.ServerOperationID != command.ServerOperationID
	return receipt, true, nil
}
