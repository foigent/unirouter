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
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

type deploymentReleaseAuthoringClaim struct {
	ReceiptID          int64
	RequestFingerprint string
	ServerOperationID  string
	TargetReleaseRef   string
	Status             string
	CreatedAt          time.Time
}

type deploymentReleaseAuthoringClaimRetryableError struct {
	operation string
	cause     error
}

func (e *deploymentReleaseAuthoringClaimRetryableError) Error() string {
	return e.operation + ": " + e.cause.Error()
}

func (e *deploymentReleaseAuthoringClaimRetryableError) Unwrap() error { return e.cause }

// AuthorSealedDeploymentRelease is the sole canonical v2 authoring writer. It
// publishes no slot and activates no traffic: one RR transaction produces a
// sealed validated candidate, two events, audit evidence and a terminal receipt.
func (m *Manager) AuthorSealedDeploymentRelease(
	ctx context.Context,
	command DeploymentReleaseAuthoringCommand,
) (DeploymentReleaseAuthoringReceipt, error) {
	command, err := NormalizeDeploymentReleaseAuthoringCommand(command)
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, err
	}
	targetReleaseRef, err := automaticReleaseRef("deployment", m.now())
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, fmt.Errorf("%w: generate release ref: %v", ErrUnavailable, err)
	}
	tx, err := m.beginDeploymentReleaseAuthoringTransaction(ctx)
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, err
	}
	defer tx.Rollback(ctx)

	claimed, claim, err := claimDeploymentReleaseAuthoringReceipt(ctx, tx, command, targetReleaseRef)
	if err != nil {
		if isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			_ = tx.Rollback(ctx)
			if recovered, ok, recoveryErr := m.recoverDeploymentReleaseAuthoringReceipt(ctx, command); recoveryErr != nil {
				return DeploymentReleaseAuthoringReceipt{}, recoveryErr
			} else if ok {
				m.Invalidate()
				return recovered, nil
			}
		}
		return DeploymentReleaseAuthoringReceipt{}, err
	}
	if !claimed {
		if claim.RequestFingerprint != command.RequestFingerprint {
			return DeploymentReleaseAuthoringReceipt{}, ErrDeploymentReleaseAuthoringIdempotencyConflict
		}
		if claim.Status != "completed" {
			return DeploymentReleaseAuthoringReceipt{}, fmt.Errorf("%w: authoring receipt is not terminal", ErrConflict)
		}
		receipt, readErr := readDeploymentReleaseAuthoringReceipt(ctx, tx, claim.ReceiptID)
		if readErr != nil {
			return DeploymentReleaseAuthoringReceipt{}, readErr
		}
		receipt.IdempotencyReplay = claim.ServerOperationID != command.ServerOperationID
		if commitErr := tx.Commit(ctx); commitErr != nil {
			if recovered, ok, recoveryErr := m.recoverDeploymentReleaseAuthoringReceipt(ctx, command); recoveryErr != nil {
				return DeploymentReleaseAuthoringReceipt{}, recoveryErr
			} else if ok {
				return recovered, nil
			}
			return DeploymentReleaseAuthoringReceipt{}, lifecycleWriteError("commit deployment release authoring replay", commitErr)
		}
		return receipt, nil
	}

	release, err := m.authorSealedDeploymentReleaseInTx(ctx, tx, command, claim)
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, err
	}
	auditID, err := insertDeploymentReleaseAuthoringAudit(ctx, tx, claim.ReceiptID, command, release)
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, err
	}
	completedAt, err := completeDeploymentReleaseAuthoringReceipt(ctx, tx, claim.ReceiptID, release, auditID)
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, err
	}
	receipt := deploymentReleaseAuthoringReceiptFromResult(
		claim, command, release, completedAt,
	)
	if err := tx.Commit(ctx); err != nil {
		if recovered, ok, recoveryErr := m.recoverDeploymentReleaseAuthoringReceipt(ctx, command); recoveryErr != nil {
			return DeploymentReleaseAuthoringReceipt{}, recoveryErr
		} else if ok {
			m.Invalidate()
			return recovered, nil
		}
		return DeploymentReleaseAuthoringReceipt{}, lifecycleWriteError("commit deployment release authoring", err)
	}
	m.Invalidate()
	return receipt, nil
}

func (m *Manager) beginDeploymentReleaseAuthoringTransaction(ctx context.Context) (pgx.Tx, error) {
	database, ok := m.db.(txOptionsBeginner)
	if !ok {
		return nil, fmt.Errorf("%w: database does not support explicit transaction isolation", ErrUnavailable)
	}
	tx, err := database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadWrite})
	if err != nil {
		return nil, lifecycleWriteError("begin deployment release authoring", err)
	}
	locks := []string{
		"LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE",
		"LOCK TABLE runtime_change_slots IN SHARE MODE",
		"LOCK TABLE orgs IN SHARE MODE",
	}
	for _, statement := range locks {
		if _, err := tx.Exec(ctx, statement); err != nil {
			_ = tx.Rollback(ctx)
			return nil, lifecycleWriteError("lock deployment release authoring authority", err)
		}
	}
	return tx, nil
}

func claimDeploymentReleaseAuthoringReceipt(
	ctx context.Context,
	tx pgx.Tx,
	command DeploymentReleaseAuthoringCommand,
	targetReleaseRef string,
) (bool, deploymentReleaseAuthoringClaim, error) {
	selectionsJSON, err := json.Marshal(command.Mutation.Selections)
	if err != nil {
		return false, deploymentReleaseAuthoringClaim{}, fmt.Errorf("%w: encode authoring selections: %v", ErrInvalid, err)
	}
	var stableRef interface{}
	var stableVersion interface{}
	if command.Mutation.Baseline.Kind == DeploymentReleaseBaselineStable ||
		command.Mutation.Baseline.Kind == DeploymentReleaseBaselineLegacyCutover {
		stableRef = command.Mutation.Baseline.StableReleaseRef
		stableVersion = command.Mutation.Baseline.ExpectedStableReleaseVersion
	}
	var claim deploymentReleaseAuthoringClaim
	err = tx.QueryRow(ctx, `
INSERT INTO model_deployment_release_authoring_receipts (
  contract_version,customer_org_id,actor_user_id,actor_org_id,operation_scope,
  idempotency_key_hash,request_fingerprint,server_operation_id,public_model_id,
  baseline_kind,expected_slot_version,expected_stable_release_ref,
  expected_stable_release_version,requested_selections,reason,target_release_ref)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15,$16)
ON CONFLICT (customer_org_id,actor_user_id,operation_scope,idempotency_key_hash)
DO NOTHING
RETURNING receipt_id,request_fingerprint,server_operation_id,target_release_ref,status,created_at`,
		DeploymentReleaseAuthoringReceiptContractVersion, command.CustomerOrgID,
		command.ActorUserID, command.ActorOrgID, deploymentReleaseAuthoringOperationScope,
		command.IdempotencyKeyHash, command.RequestFingerprint, command.ServerOperationID,
		command.Mutation.PublicModelID, command.Mutation.Baseline.Kind,
		command.Mutation.Baseline.ExpectedSlotVersion, stableRef, stableVersion,
		string(selectionsJSON), command.Mutation.Reason, targetReleaseRef,
	).Scan(&claim.ReceiptID, &claim.RequestFingerprint, &claim.ServerOperationID,
		&claim.TargetReleaseRef, &claim.Status, &claim.CreatedAt)
	if err == nil {
		return true, claim, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		if isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			return false, deploymentReleaseAuthoringClaim{}, &deploymentReleaseAuthoringClaimRetryableError{
				operation: "claim deployment release authoring receipt", cause: err,
			}
		}
		return false, deploymentReleaseAuthoringClaim{}, lifecycleWriteError("claim deployment release authoring receipt", err)
	}
	err = tx.QueryRow(ctx, `
SELECT receipt_id,request_fingerprint,server_operation_id,target_release_ref,status,created_at
  FROM model_deployment_release_authoring_receipts
 WHERE customer_org_id=$1 AND actor_user_id=$2 AND operation_scope=$3
   AND idempotency_key_hash=$4
 FOR SHARE`, command.CustomerOrgID, command.ActorUserID,
		deploymentReleaseAuthoringOperationScope, command.IdempotencyKeyHash,
	).Scan(&claim.ReceiptID, &claim.RequestFingerprint, &claim.ServerOperationID,
		&claim.TargetReleaseRef, &claim.Status, &claim.CreatedAt)
	if err != nil {
		if isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			return false, deploymentReleaseAuthoringClaim{}, &deploymentReleaseAuthoringClaimRetryableError{
				operation: "read deployment release authoring winner", cause: err,
			}
		}
		return false, deploymentReleaseAuthoringClaim{}, lifecycleWriteError("read deployment release authoring winner", err)
	}
	return false, claim, nil
}

func (m *Manager) authorSealedDeploymentReleaseInTx(
	ctx context.Context,
	tx pgx.Tx,
	command DeploymentReleaseAuthoringCommand,
	claim deploymentReleaseAuthoringClaim,
) (DeploymentRelease, error) {
	if err := validateDeploymentLifecycleActor(
		ctx, tx, command.ActorUserID, command.ActorOrgID, command.CustomerOrgID,
	); err != nil {
		return DeploymentRelease{}, err
	}
	var modelName, lifecycle, executionProfileID, executionProfileVersion string
	var publicContextLimit, publicOutputLimit int
	if err := tx.QueryRow(ctx, `
SELECT model_name,lifecycle,context_length,output_limit,
	   execution_profile_id,execution_profile_version
  FROM model_registry
 WHERE id=$1 AND org_id=$2
 FOR SHARE`, command.Mutation.PublicModelID, command.CustomerOrgID).Scan(
		&modelName, &lifecycle, &publicContextLimit, &publicOutputLimit,
		&executionProfileID, &executionProfileVersion,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DeploymentRelease{}, fmt.Errorf("%w: public model ownership", ErrConflict)
		}
		return DeploymentRelease{}, lifecycleWriteError("read deployment release authoring model", err)
	}
	if lifecycle == "retired" {
		return DeploymentRelease{}, fmt.Errorf("%w: public model is retired", ErrConflict)
	}
	profile, ok := modelprofile.Lookup(executionProfileID, executionProfileVersion)
	if !ok {
		return DeploymentRelease{}, fmt.Errorf("%w: unknown model execution profile", ErrConflict)
	}
	if err := modelprofile.ValidateEffectiveLimits(profile, publicContextLimit, publicOutputLimit); err != nil {
		return DeploymentRelease{}, fmt.Errorf("%w: model execution profile limits: %v", ErrConflict, err)
	}
	parentID, baselineHash, err := exactDeploymentReleaseAuthoringBaseline(
		ctx, tx, command.Mutation.PublicModelID, modelName, command.Mutation.Baseline,
	)
	if err != nil {
		return DeploymentRelease{}, err
	}
	routes, err := projectDeploymentReleaseRoutes(
		ctx, tx, command.CustomerOrgID, command.Mutation.PublicModelID,
		command.Mutation.Selections,
	)
	if err != nil {
		return DeploymentRelease{}, err
	}
	if err := validateDeploymentRouteInputs(
		ctx, tx, command.CustomerOrgID, command.Mutation.PublicModelID, routes,
	); err != nil {
		return DeploymentRelease{}, err
	}
	for index := range routes {
		if !modelqualification.IsQualificationRequired(routes[index].ModelOperationID) {
			continue
		}
		if err := tx.QueryRow(ctx, `
SELECT nextval(pg_get_serial_sequence(
    'model_operation_public_execution_bindings','public_execution_binding_id'
))`).Scan(&routes[index].PublicExecutionBindingID); err != nil {
			return DeploymentRelease{}, lifecycleWriteError("allocate public execution binding", err)
		}
	}
	candidate := DeploymentCandidate{
		ContractVersion: DeploymentCandidateVersion, PublicModelID: command.Mutation.PublicModelID,
		ModelName: modelName, DeploymentRoutes: routes,
	}
	candidateHash := hashJSON(candidate)
	routeItemsHash := hashJSON(routes)
	candidateJSON, _ := json.Marshal(candidate)
	reportJSON, _ := json.Marshal(map[string]interface{}{
		"contract_version": "unirouter.runtime-deployment-manifest-seal.v1",
		"state":            "prepared", "route_items_hash": routeItemsHash,
		"route_items_count": len(routes),
	})
	var releaseID int64
	err = tx.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
  release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
  candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
  parent_stable_release_id,canary_basis_points,reason,created_by,
  public_model_id,route_items_hash,route_items_count,authoring_receipt_id,
  execution_profile_id,execution_profile_version,public_context_limit,public_output_limit)
VALUES ($1,$2,$3,$4,$5,$6::jsonb,'[]'::jsonb,$7::jsonb,'review_required',
        'draft',1,$8,0,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
RETURNING id`, claim.TargetReleaseRef, modelName, DeploymentCandidateVersion,
		candidateHash, baselineHash, string(candidateJSON), string(reportJSON), parentID,
		command.Mutation.Reason, command.ActorUserID, command.Mutation.PublicModelID,
		routeItemsHash, len(routes), claim.ReceiptID,
		executionProfileID, executionProfileVersion, publicContextLimit, publicOutputLimit,
	).Scan(&releaseID)
	if err != nil {
		return DeploymentRelease{}, classifyWriteError(err)
	}
	for index, route := range routes {
		if modelqualification.IsQualificationRequired(route.ModelOperationID) {
			if err := insertPublicExecutionBinding(
				ctx, tx, releaseID, index+1, command.CustomerOrgID,
				command.Mutation.PublicModelID, command.ActorUserID, command.ActorOrgID, route,
			); err != nil {
				return DeploymentRelease{}, err
			}
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO runtime_route_revision_items (
  release_id,ordinal,deployment_id,operation_id,
  expected_connection_revision,expected_deployment_revision,expected_operation_revision,
  protocol_profile_id,protocol_profile_version,public_path,executor_contract_version,
  public_execution_binding_id,delivery_contract_version,charge_contract_version,
  idempotency_contract_version,admission,priority,weight,source_candidate_version)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'allow',NULL,NULL,$16)`,
			releaseID, index+1, route.DeploymentID, route.ModelOperationID,
			route.ExpectedConnectionRevision, route.ExpectedDeploymentRevision,
			route.ExpectedOperationRevision, route.ProtocolProfileID,
			route.ProtocolProfileVersion, nullableDeploymentExecutionBinding(route.PublicPath),
			nullableDeploymentExecutionBinding(route.ExecutorContractVersion),
			nullableDeploymentExecutionBindingID(route.PublicExecutionBindingID),
			nullableDeploymentExecutionBinding(route.DeliveryContractVersion),
			nullableDeploymentExecutionBinding(route.ChargeContractVersion),
			nullableDeploymentExecutionBinding(route.IdempotencyContractVersion),
			DeploymentCandidateVersion,
		); err != nil {
			return DeploymentRelease{}, lifecycleWriteError("write server-owned deployment route", err)
		}
	}
	preparedDetail, _ := json.Marshal(map[string]interface{}{
		"authoring_receipt_id": claim.ReceiptID, "canonical_authoring": true,
		"runtime_mutation": "none", "manifest_state": "prepared",
		"public_model_id":  command.Mutation.PublicModelID,
		"route_items_hash": routeItemsHash, "route_items_count": len(routes),
		"execution_profile_id": executionProfileID, "execution_profile_version": executionProfileVersion,
		"public_context_limit": publicContextLimit, "public_output_limit": publicOutputLimit,
	})
	if _, err := tx.Exec(ctx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail,occurred_at)
VALUES ($1,1,'create','none','draft',$2,0,1,$3::jsonb,$4)`,
		releaseID, command.ActorUserID, string(preparedDetail),
		m.now().UTC().Truncate(time.Microsecond),
	); err != nil {
		return DeploymentRelease{}, lifecycleWriteError("append deployment authoring create event", err)
	}
	if err := validateDeploymentRoutes(
		ctx, tx, command.CustomerOrgID, command.Mutation.PublicModelID, routes,
	); err != nil {
		return DeploymentRelease{}, err
	}
	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET stage='validated',version=2,validated_by=$2,validated_at=transaction_timestamp(),
       route_items_sealed_at=transaction_timestamp(),updated_at=transaction_timestamp()
 WHERE id=$1 AND stage='draft' AND version=1 AND route_items_sealed_at IS NULL`,
		releaseID, command.ActorUserID)
	if err != nil {
		return DeploymentRelease{}, lifecycleWriteError("seal authored deployment release", err)
	}
	if tag.RowsAffected() != 1 {
		return DeploymentRelease{}, ErrConflict
	}
	sealedDetail, _ := json.Marshal(map[string]interface{}{
		"authoring_receipt_id": claim.ReceiptID, "canonical_authoring": true,
		"runtime_mutation": "none", "manifest_state": "sealed",
		"public_model_id":  command.Mutation.PublicModelID,
		"route_items_hash": routeItemsHash, "route_items_count": len(routes),
		"execution_profile_id": executionProfileID, "execution_profile_version": executionProfileVersion,
		"public_context_limit": publicContextLimit, "public_output_limit": publicOutputLimit,
	})
	if _, err := tx.Exec(ctx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail,occurred_at)
VALUES ($1,2,'validate','draft','validated',$2,1,2,$3::jsonb,$4)`,
		releaseID, command.ActorUserID, string(sealedDetail),
		m.now().UTC().Truncate(time.Microsecond),
	); err != nil {
		return DeploymentRelease{}, lifecycleWriteError("append deployment authoring validate event", err)
	}
	return readDeploymentReleaseByID(ctx, tx, releaseID, false)
}

func exactDeploymentReleaseAuthoringBaseline(
	ctx context.Context,
	tx pgx.Tx,
	publicModelID int64,
	modelName string,
	baseline DeploymentReleaseBaseline,
) (*int64, string, error) {
	rows, err := tx.Query(ctx, `
SELECT slot.model_name,slot.public_model_id,slot.stable_release_id,
	       slot.canary_release_id,slot.canary_basis_points,slot.slot_version,
	       stable.release_ref,stable.version,stable.stage,stable.candidate_hash,
	       stable.candidate_version,stable.public_model_id,stable.model_name,
	       stable.route_items_sealed_at,stable.route_items_count,stable.route_items_hash
  FROM runtime_change_slots slot
  LEFT JOIN runtime_change_releases stable ON stable.id=slot.stable_release_id
 WHERE slot.model_name=$1 OR slot.public_model_id=$2
 ORDER BY slot.model_name`, modelName, publicModelID)
	if err != nil {
		return nil, "", lifecycleWriteError("read exact deployment authoring baseline", err)
	}
	defer rows.Close()
	type row struct {
		modelName                          string
		publicModelID, stableID, canaryID  sql.NullInt64
		basisPoints                        int
		slotVersion                        int64
		stableRef, stableStage, stableHash sql.NullString
		stableVersion, stablePublicModelID sql.NullInt64
		stableCandidateVersion             sql.NullString
		stableModelName                    sql.NullString
		stableSealedAt                     sql.NullTime
		stableRouteItemsCount              sql.NullInt64
		stableRouteItemsHash               sql.NullString
	}
	values := make([]row, 0, 2)
	for rows.Next() {
		var value row
		if err := rows.Scan(
			&value.modelName, &value.publicModelID, &value.stableID, &value.canaryID,
			&value.basisPoints, &value.slotVersion, &value.stableRef,
			&value.stableVersion, &value.stableStage, &value.stableHash,
			&value.stableCandidateVersion, &value.stablePublicModelID,
			&value.stableModelName, &value.stableSealedAt,
			&value.stableRouteItemsCount, &value.stableRouteItemsHash,
		); err != nil {
			return nil, "", lifecycleWriteError("scan exact deployment authoring baseline", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, "", lifecycleWriteError("read exact deployment authoring baseline", err)
	}
	if baseline.Kind == DeploymentReleaseBaselineInitial {
		if len(values) != 0 {
			return nil, "", fmt.Errorf("%w: initial public model already has a runtime slot", ErrConflict)
		}
		return nil, hashJSON(DeploymentCandidate{
			ContractVersion: DeploymentCandidateVersion, PublicModelID: publicModelID,
			ModelName: modelName, DeploymentRoutes: []DeploymentRoute{},
		}), nil
	}
	if len(values) != 1 {
		return nil, "", fmt.Errorf("%w: stable public model slot identity is ambiguous", ErrConflict)
	}
	value := values[0]
	if value.modelName != modelName || !value.stableID.Valid || value.canaryID.Valid || value.basisPoints != 0 ||
		value.slotVersion != baseline.ExpectedSlotVersion || !value.stableRef.Valid ||
		value.stableRef.String != baseline.StableReleaseRef || !value.stableVersion.Valid ||
		value.stableVersion.Int64 != baseline.ExpectedStableReleaseVersion ||
		!value.stableStage.Valid || value.stableStage.String != "active" ||
		!value.stableHash.Valid || value.stableHash.String == "" ||
		!value.stablePublicModelID.Valid || value.stablePublicModelID.Int64 != publicModelID ||
		!value.stableModelName.Valid || value.stableModelName.String != modelName ||
		!value.stableSealedAt.Valid || !value.stableRouteItemsCount.Valid ||
		value.stableRouteItemsCount.Int64 <= 0 {
		return nil, "", fmt.Errorf("%w: exact stable release baseline changed", ErrConflict)
	}
	switch baseline.Kind {
	case DeploymentReleaseBaselineStable:
		if !value.publicModelID.Valid || value.publicModelID.Int64 != publicModelID ||
			!value.stableCandidateVersion.Valid ||
			value.stableCandidateVersion.String != DeploymentCandidateVersion {
			return nil, "", fmt.Errorf("%w: exact stable release baseline changed", ErrConflict)
		}
	case DeploymentReleaseBaselineLegacyCutover:
		if value.publicModelID.Valid || !value.stableCandidateVersion.Valid ||
			value.stableCandidateVersion.String != CandidateVersion ||
			!value.stableRouteItemsHash.Valid || value.stableRouteItemsHash.String == "" {
			return nil, "", fmt.Errorf("%w: exact legacy cutover baseline changed", ErrConflict)
		}
		if err := verifyLegacyCompatibilityManifest(
			ctx, tx, value.stableID.Int64, value.stableRouteItemsHash.String,
			int(value.stableRouteItemsCount.Int64),
		); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", fmt.Errorf("%w: release baseline kind", ErrInvalid)
	}
	parentID := value.stableID.Int64
	return &parentID, value.stableHash.String, nil
}

func projectDeploymentReleaseRoutes(
	ctx context.Context,
	tx pgx.Tx,
	customerOrgID, publicModelID int64,
	selections []DeploymentReleaseSelection,
) ([]DeploymentRoute, error) {
	routes := make([]DeploymentRoute, 0, len(selections))
	for _, selection := range selections {
		var route DeploymentRoute
		var connectionState, deploymentState, supportState string
		var qualified bool
		var publicPath, executor, delivery, charge, idempotency sql.NullString
		err := tx.QueryRow(ctx, `
SELECT deployment.id,operation.operation_id,connection.config_revision,
       deployment.config_revision,operation.revision,
       operation.protocol_profile_id,operation.protocol_profile_version,
       connection.admin_state,deployment.admin_state,operation.support_state,
       COALESCE(qualification.qualified,FALSE),qualification.public_path,
       qualification.executor_contract_version,contract.delivery_contract_version,
       contract.charge_contract_version,contract.idempotency_contract_version
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id AND operation.operation_id=$2
  LEFT JOIN model_operation_execution_qualifications qualification
    ON qualification.deployment_id=operation.deployment_id
   AND qualification.operation_id=operation.operation_id
  LEFT JOIN model_operation_current_public_execution_contracts contract
    ON contract.operation_id=qualification.operation_id
   AND contract.public_path=qualification.public_path
   AND contract.protocol_profile_id=qualification.protocol_profile_id
   AND contract.protocol_profile_version=qualification.protocol_profile_version
   AND contract.executor_contract_version=qualification.executor_contract_version
 WHERE deployment.id=$1 AND deployment.customer_org_id=$3
   AND deployment.public_model_id=$4
 FOR SHARE OF deployment,connection,operation`, selection.DeploymentID,
			selection.OperationID, customerOrgID, publicModelID,
		).Scan(
			&route.DeploymentID, &route.ModelOperationID,
			&route.ExpectedConnectionRevision, &route.ExpectedDeploymentRevision,
			&route.ExpectedOperationRevision, &route.ProtocolProfileID,
			&route.ProtocolProfileVersion, &connectionState, &deploymentState,
			&supportState, &qualified, &publicPath, &executor, &delivery, &charge,
			&idempotency,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: deployment route selection %d/%s", ErrInvalid, selection.DeploymentID, selection.OperationID)
		}
		if err != nil {
			return nil, lifecycleWriteError("project server-owned deployment route", err)
		}
		if connectionState != "enabled" || deploymentState != "enabled" || supportState != "enabled" {
			return nil, fmt.Errorf("%w: selected deployment route is disabled", ErrConflict)
		}
		requiresQualification := modelqualification.IsQualificationRequired(route.ModelOperationID)
		if requiresQualification {
			if !qualified || !publicPath.Valid || !executor.Valid || !delivery.Valid ||
				!charge.Valid || !idempotency.Valid {
				return nil, fmt.Errorf("%w: selected operation lacks current public execution qualification", ErrConflict)
			}
			route.PublicPath, route.ExecutorContractVersion = publicPath.String, executor.String
			route.DeliveryContractVersion = delivery.String
			route.ChargeContractVersion = charge.String
			route.IdempotencyContractVersion = idempotency.String
		} else if route.ModelOperationID != string(modeloperation.IDChatCompletions) &&
			route.ModelOperationID != string(modeloperation.IDEmbeddings) {
			return nil, fmt.Errorf("%w: unsupported deployment operation", ErrInvalid)
		}
		route.Admission = routingpolicy.AdmissionAllow
		route.Priority = nil
		route.Weight = nil
		routes = append(routes, route)
	}
	return routes, nil
}

func insertDeploymentReleaseAuthoringAudit(
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	command DeploymentReleaseAuthoringCommand,
	release DeploymentRelease,
) (int64, error) {
	detail, err := json.Marshal(map[string]interface{}{
		"receipt_id": receiptID, "contract_version": DeploymentReleaseAuthoringReceiptContractVersion,
		"server_operation_id": command.ServerOperationID, "runtime_mutation": "none",
		"public_model_id": command.Mutation.PublicModelID,
		"release_ref":     release.ReleaseRef, "candidate_hash": release.CandidateHash,
		"route_items_hash": release.RouteItemsHash, "route_items_count": release.RouteItemsCount,
		"baseline": command.Mutation.Baseline, "selections": command.Mutation.Selections,
	})
	if err != nil {
		return 0, fmt.Errorf("%w: encode deployment release authoring audit: %v", ErrUnavailable, err)
	}
	source := strings.TrimSpace(command.Audit.Source)
	if source == "" || source == "admin" {
		source = "admin_console"
	}
	var auditID int64
	err = tx.QueryRow(ctx, `
INSERT INTO audit_logs (
  org_id,user_id,action,resource,resource_id,detail,ip,user_agent,
  request_id,source,before_state,after_state,client_request_id,created_at)
VALUES ($1,$2,'model_deployment.release_authoring.create','model_deployment_release',
        $3,$4::jsonb,$5,$6,$7,$8,'{}'::jsonb,$4::jsonb,$9,transaction_timestamp())
RETURNING id`, command.CustomerOrgID, command.ActorUserID,
		strconv.FormatInt(release.ID, 10), string(detail), command.Audit.IP,
		command.Audit.UserAgent, command.Audit.RequestID, source,
		nullableStringValue(command.Audit.ClientRequestID),
	).Scan(&auditID)
	if err != nil {
		return 0, lifecycleWriteError("insert deployment release authoring audit", err)
	}
	return auditID, nil
}

func completeDeploymentReleaseAuthoringReceipt(
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	release DeploymentRelease,
	auditID int64,
) (time.Time, error) {
	var completedAt time.Time
	err := tx.QueryRow(ctx, `
UPDATE model_deployment_release_authoring_receipts
   SET status='completed',release_id=$2,model_name=$3,candidate_hash=$4,
       route_items_hash=$5,route_items_count=$6,original_http_status=201,
       original_location='/api/v1/model-deployment-releases/' || target_release_ref,
       audit_log_id=$7,completed_at=transaction_timestamp()
 WHERE receipt_id=$1 AND status='in_progress'
 RETURNING completed_at`, receiptID, release.ID, release.ModelName,
		release.CandidateHash, release.RouteItemsHash, release.RouteItemsCount, auditID,
	).Scan(&completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("%w: authoring receipt claim lost", ErrConflict)
	}
	if err != nil {
		return time.Time{}, lifecycleWriteError("complete deployment release authoring receipt", err)
	}
	return completedAt.UTC(), nil
}

func deploymentReleaseAuthoringReceiptFromResult(
	claim deploymentReleaseAuthoringClaim,
	command DeploymentReleaseAuthoringCommand,
	release DeploymentRelease,
	completedAt time.Time,
) DeploymentReleaseAuthoringReceipt {
	return DeploymentReleaseAuthoringReceipt{
		ContractVersion: DeploymentReleaseAuthoringReceiptContractVersion,
		ReceiptID:       claim.ReceiptID, CustomerOrgID: command.CustomerOrgID,
		ActorUserID: command.ActorUserID, ServerOperationID: claim.ServerOperationID,
		OriginalHTTPStatus: 201,
		OriginalLocation:   "/api/v1/model-deployment-releases/" + claim.TargetReleaseRef,
		Baseline:           command.Mutation.Baseline,
		Selections:         append([]DeploymentReleaseSelection(nil), command.Mutation.Selections...),
		Release:            release, CreatedAt: claim.CreatedAt.UTC(), CompletedAt: completedAt,
	}
}

func readDeploymentReleaseAuthoringReceipt(
	ctx context.Context,
	store deploymentManifestStore,
	receiptID int64,
) (DeploymentReleaseAuthoringReceipt, error) {
	var result DeploymentReleaseAuthoringReceipt
	var baselineKind string
	var stableRef sql.NullString
	var stableVersion sql.NullInt64
	var selectionsJSON []byte
	var releaseID int64
	err := store.QueryRow(ctx, `
SELECT contract_version,receipt_id,customer_org_id,actor_user_id,
       server_operation_id,original_http_status,original_location,
       baseline_kind,expected_slot_version,expected_stable_release_ref,
       expected_stable_release_version,requested_selections,release_id,
       created_at,completed_at
  FROM model_deployment_release_authoring_receipts
 WHERE receipt_id=$1 AND status='completed'`, receiptID).Scan(
		&result.ContractVersion, &result.ReceiptID, &result.CustomerOrgID,
		&result.ActorUserID, &result.ServerOperationID, &result.OriginalHTTPStatus,
		&result.OriginalLocation, &baselineKind, &result.Baseline.ExpectedSlotVersion,
		&stableRef, &stableVersion, &selectionsJSON, &releaseID,
		&result.CreatedAt, &result.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentReleaseAuthoringReceipt{}, ErrNotFound
	}
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, lifecycleWriteError("read deployment release authoring receipt", err)
	}
	// Keep exact replay independent of the PostgreSQL session location, matching
	// the UTC timestamps returned by the original authoring transaction.
	result.CreatedAt = result.CreatedAt.UTC()
	result.CompletedAt = result.CompletedAt.UTC()
	result.Baseline.Kind = DeploymentReleaseBaselineKind(baselineKind)
	if stableRef.Valid {
		result.Baseline.StableReleaseRef = stableRef.String
	}
	if stableVersion.Valid {
		result.Baseline.ExpectedStableReleaseVersion = stableVersion.Int64
	}
	if err := json.Unmarshal(selectionsJSON, &result.Selections); err != nil {
		return DeploymentReleaseAuthoringReceipt{}, fmt.Errorf("%w: decode authoring receipt selections", ErrUnavailable)
	}
	release, err := readDeploymentReleaseByID(ctx, store, releaseID, false)
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, err
	}
	// Candidate and seal facts are immutable; stage/version may have advanced by
	// M76 after authoring, so exact replay returns their frozen completion image.
	release.Stage = "validated"
	release.Version = 2
	release.CanaryBasisPoints = 0
	release.ActivatedBy = nil
	release.ActivatedAt = nil
	release.RolledBackAt = nil
	if release.ValidatedAt != nil {
		release.UpdatedAt = *release.ValidatedAt
	}
	result.Release = release
	return result, nil
}

func (m *Manager) recoverDeploymentReleaseAuthoringReceipt(
	ctx context.Context,
	command DeploymentReleaseAuthoringCommand,
) (DeploymentReleaseAuthoringReceipt, bool, error) {
	tx, err := m.beginDeploymentReleaseAuthoringTransaction(ctx)
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, false, err
	}
	defer tx.Rollback(ctx)
	var claim deploymentReleaseAuthoringClaim
	err = tx.QueryRow(ctx, `
SELECT receipt_id,request_fingerprint,server_operation_id,target_release_ref,status,created_at
  FROM model_deployment_release_authoring_receipts
 WHERE customer_org_id=$1 AND actor_user_id=$2 AND operation_scope=$3
   AND idempotency_key_hash=$4
 FOR SHARE`, command.CustomerOrgID, command.ActorUserID,
		deploymentReleaseAuthoringOperationScope, command.IdempotencyKeyHash,
	).Scan(&claim.ReceiptID, &claim.RequestFingerprint, &claim.ServerOperationID,
		&claim.TargetReleaseRef, &claim.Status, &claim.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentReleaseAuthoringReceipt{}, false, nil
	}
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, false, lifecycleWriteError("recover deployment release authoring winner", err)
	}
	if claim.RequestFingerprint != command.RequestFingerprint {
		return DeploymentReleaseAuthoringReceipt{}, false, ErrDeploymentReleaseAuthoringIdempotencyConflict
	}
	if claim.Status != "completed" {
		return DeploymentReleaseAuthoringReceipt{}, false, nil
	}
	receipt, err := readDeploymentReleaseAuthoringReceipt(ctx, tx, claim.ReceiptID)
	if err != nil {
		return DeploymentReleaseAuthoringReceipt{}, false, err
	}
	receipt.IdempotencyReplay = claim.ServerOperationID != command.ServerOperationID
	if err := tx.Commit(ctx); err != nil {
		return DeploymentReleaseAuthoringReceipt{}, false, lifecycleWriteError("commit deployment release authoring recovery", err)
	}
	return receipt, true, nil
}
