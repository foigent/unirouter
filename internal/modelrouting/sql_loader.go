package modelrouting

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

type sqlSnapshotLoader struct{}

func (sqlSnapshotLoader) Load(ctx context.Context, tx pgx.Tx, request Request) (snapshot, error) {
	return loadSQLSnapshot(ctx, tx, request, nil)
}

// loadSQLSnapshotForRelease is used only by the startup authority audit. The
// release id must already be one of the slot's current stable/canary members;
// callers cannot use it to route customer traffic to an arbitrary release.
func loadSQLSnapshotForRelease(ctx context.Context, tx pgx.Tx, request Request, releaseID int64) (snapshot, error) {
	if releaseID <= 0 {
		return snapshot{}, fmt.Errorf("%w: current release", ErrInvalidRequest)
	}
	return loadSQLSnapshot(ctx, tx, request, &releaseID)
}

func loadSQLSnapshot(ctx context.Context, tx pgx.Tx, request Request, forcedReleaseID *int64) (snapshot, error) {
	value := snapshot{
		Mode: request.Mode, CustomerOrgID: request.CustomerOrgID,
		PublicModel: request.PublicModel, RequestedOperationID: request.ModelOperationID,
		ExecutionProfileID:      modelprofile.UnverifiedProfileID,
		ExecutionProfileVersion: modelprofile.UnverifiedProfileVersion,
	}
	if err := tx.QueryRow(ctx, `SELECT NOW()`).Scan(&value.DBNow); err != nil {
		return snapshot{}, fmt.Errorf("%w: read database clock: %v", ErrUnavailable, err)
	}
	value.DBNow = value.DBNow.UTC()
	if err := tx.QueryRow(ctx, `
SELECT id,lifecycle,context_length,output_limit,execution_profile_id,execution_profile_version
  FROM model_registry
 WHERE org_id=$1 AND model_name=$2`, request.CustomerOrgID, request.PublicModel).Scan(
		&value.PublicModelID, &value.PublicModelLifecycle,
		&value.PublicContextLimit, &value.PublicOutputLimit,
		&value.ExecutionProfileID, &value.ExecutionProfileVersion,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if request.Mode != ModeLegacy {
				return snapshot{}, ErrNoRoute
			}
		} else {
			return snapshot{}, fmt.Errorf("%w: read public model: %v", ErrUnavailable, err)
		}
	}
	if value.PublicModelLifecycle == "retired" {
		return snapshot{}, ErrNoRoute
	}

	slot, err := loadSlot(ctx, tx, value.PublicModelID, value.PublicModel, request.SelectionKey, forcedReleaseID)
	if err != nil {
		return snapshot{}, err
	}
	value.Slot = slot
	if request.Mode != ModeLegacy && slot.Selected.CandidateVersion == canonicalCandidateVersion {
		if slot.Selected.ExecutionProfileID == "" || slot.Selected.ExecutionProfileVersion == "" ||
			slot.Selected.PublicContextLimit <= 0 || slot.Selected.PublicOutputLimit <= 0 {
			return snapshot{}, fmt.Errorf("%w: sealed model execution contract", ErrManifestStale)
		}
		value.ExecutionProfileID = slot.Selected.ExecutionProfileID
		value.ExecutionProfileVersion = slot.Selected.ExecutionProfileVersion
		value.PublicContextLimit = slot.Selected.PublicContextLimit
		value.PublicOutputLimit = slot.Selected.PublicOutputLimit
	}
	if request.Mode != ModeCanonical {
		legacy, loadErr := loadLegacyCandidates(ctx, tx, value.PublicModel)
		if loadErr != nil {
			return snapshot{}, loadErr
		}
		value.Legacy = legacy
	}
	profile, ok := modelprofile.Lookup(value.ExecutionProfileID, value.ExecutionProfileVersion)
	if !ok {
		return snapshot{}, fmt.Errorf("%w: unknown model execution profile %s@%s", ErrManifestStale, value.ExecutionProfileID, value.ExecutionProfileVersion)
	}
	value.CapabilityRequirements, err = ResolveCapabilityRequirements(profile, request)
	if err != nil {
		return snapshot{}, err
	}
	value.CapabilitiesResolved = true
	// Legacy mode must remain the exact pre-cutover authority. In particular it
	// does not read normalized manifests, canonical projections, operation rows,
	// probes or capability evidence and therefore cannot be broken by an
	// unprepared projection rollout.
	if request.Mode == ModeLegacy {
		if slot.Selected.ID > 0 && slot.Selected.CandidateVersion == legacyCandidateVersion {
			value.LegacyRelease = slot.Selected
			value.LegacyOverrides, err = decodeLegacyOverrides(slot.Selected.CandidateJSON, value.PublicModel)
			if err != nil {
				return snapshot{}, err
			}
		}
		return value, nil
	}
	if slot.Selected.ID > 0 {
		value.Manifest, err = loadManifest(ctx, tx, slot.Selected.ID)
		if err != nil {
			return snapshot{}, err
		}
	}
	switch {
	case slot.Selected.ID > 0 && slot.Selected.CandidateVersion == legacyCandidateVersion:
		value.LegacyRelease = slot.Selected
		value.LegacyManifest = append([]manifestItem(nil), value.Manifest...)
	case request.Mode == ModeVerify && slot.Selected.ID > 0 && slot.Selected.CandidateVersion == canonicalCandidateVersion:
		if slot.Selected.ParentStableReleaseID <= 0 {
			return snapshot{}, fmt.Errorf("%w: canonical release has no legacy compatibility parent", ErrManifestStale)
		}
		value.LegacyRelease, err = loadRelease(ctx, tx, slot.Selected.ParentStableReleaseID)
		if err != nil {
			return snapshot{}, err
		}
		value.LegacyManifest, err = loadManifest(ctx, tx, value.LegacyRelease.ID)
		if err != nil {
			return snapshot{}, err
		}
	}
	if value.LegacyRelease.ID > 0 {
		value.LegacyOverrides, err = decodeLegacyOverrides(value.LegacyRelease.CandidateJSON, value.PublicModel)
		if err != nil {
			return snapshot{}, err
		}
	}
	operation := request.ModelOperationID
	if slot.Selected.CandidateVersion == legacyCandidateVersion {
		operation = legacyCompatibilityOp
	}
	if value.PublicModelID == 0 {
		return value, nil
	}
	canonical, err := loadCanonicalCandidates(
		ctx, tx, value.CustomerOrgID, value.PublicModelID, operation, slot.Selected.ID, value.Manifest,
	)
	if err != nil {
		return snapshot{}, err
	}
	if err := loadCapabilities(ctx, tx, value.CustomerOrgID, value.PublicModelID, operation, capabilityIDs(value.CapabilityRequirements), canonical); err != nil {
		return snapshot{}, err
	}
	value.Canonical = canonical
	return value, nil
}

func loadSlot(ctx context.Context, tx pgx.Tx, publicModelID int64, modelName, selectionKey string, forcedReleaseID *int64) (slotSnapshot, error) {
	const query = `
SELECT s.public_model_id,s.slot_version,s.canary_basis_points,
       COALESCE(stable.id,0),COALESCE(stable.release_ref,''),
       COALESCE(stable.candidate_version,''),COALESCE(stable.candidate_hash,''),
       COALESCE(stable.route_items_hash,''),COALESCE(stable.route_items_count,0),
       stable.route_items_sealed_at,COALESCE(stable.stage,''),COALESCE(stable.version,0),
       COALESCE(stable.public_model_id,0),
	   COALESCE(stable.execution_profile_id,''),COALESCE(stable.execution_profile_version,''),
	   COALESCE(stable.public_context_limit,0),COALESCE(stable.public_output_limit,0),
	   COALESCE(stable.parent_stable_release_id,0),
       COALESCE(stable.candidate,'{}'::jsonb),
       COALESCE(canary.id,0),COALESCE(canary.release_ref,''),
       COALESCE(canary.candidate_version,''),COALESCE(canary.candidate_hash,''),
       COALESCE(canary.route_items_hash,''),COALESCE(canary.route_items_count,0),
	   canary.route_items_sealed_at,COALESCE(canary.stage,''),COALESCE(canary.version,0),
	   COALESCE(canary.public_model_id,0),COALESCE(canary.parent_stable_release_id,0),
	   COALESCE(canary.execution_profile_id,''),COALESCE(canary.execution_profile_version,''),
	   COALESCE(canary.public_context_limit,0),COALESCE(canary.public_output_limit,0),
	   COALESCE(canary.candidate,'{}'::jsonb)
  FROM runtime_change_slots s
  LEFT JOIN runtime_change_releases stable ON stable.id=s.stable_release_id
  LEFT JOIN runtime_change_releases canary ON canary.id=s.canary_release_id
 WHERE s.model_name=$1 AND (s.public_model_id IS NULL OR s.public_model_id=$2)`
	var result slotSnapshot
	var slotPublicModelID *int64
	err := tx.QueryRow(ctx, query, modelName, publicModelID).Scan(
		&slotPublicModelID, &result.Version, &result.CanaryBasisPoints,
		&result.Stable.ID, &result.Stable.Ref, &result.Stable.CandidateVersion,
		&result.Stable.CandidateHash, &result.Stable.RouteItemsHash,
		&result.Stable.RouteItemsCount, &result.Stable.RouteItemsSealed,
		&result.Stable.Stage, &result.Stable.Revision, &result.Stable.PublicModelID,
		&result.Stable.ExecutionProfileID, &result.Stable.ExecutionProfileVersion,
		&result.Stable.PublicContextLimit, &result.Stable.PublicOutputLimit,
		&result.Stable.ParentStableReleaseID,
		&result.Stable.CandidateJSON,
		&result.Canary.ID, &result.Canary.Ref, &result.Canary.CandidateVersion,
		&result.Canary.CandidateHash, &result.Canary.RouteItemsHash,
		&result.Canary.RouteItemsCount, &result.Canary.RouteItemsSealed,
		&result.Canary.Stage, &result.Canary.Revision, &result.Canary.PublicModelID,
		&result.Canary.ParentStableReleaseID,
		&result.Canary.ExecutionProfileID, &result.Canary.ExecutionProfileVersion,
		&result.Canary.PublicContextLimit, &result.Canary.PublicOutputLimit,
		&result.Canary.CandidateJSON,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return slotSnapshot{}, fmt.Errorf("%w: read runtime slot: %v", ErrUnavailable, err)
	}
	result.Present = true
	if slotPublicModelID != nil {
		result.PublicModelID = *slotPublicModelID
	}
	if result.Stable.ID > 0 && result.Stable.Stage != "active" {
		return slotSnapshot{}, fmt.Errorf("%w: stable release stage", ErrManifestStale)
	}
	if result.Canary.ID > 0 {
		if result.Canary.Stage != "canary" || result.CanaryBasisPoints < 1 || result.CanaryBasisPoints > 5000 {
			return slotSnapshot{}, fmt.Errorf("%w: canary release stage or allocation", ErrManifestStale)
		}
		if result.Stable.ID > 0 && result.Stable.CandidateVersion != result.Canary.CandidateVersion {
			return slotSnapshot{}, fmt.Errorf("%w: mixed runtime authority versions", ErrManifestStale)
		}
	} else if result.CanaryBasisPoints != 0 {
		return slotSnapshot{}, fmt.Errorf("%w: canary allocation without release", ErrManifestStale)
	}
	if forcedReleaseID != nil {
		switch *forcedReleaseID {
		case result.Stable.ID:
			result.Selected = result.Stable
		case result.Canary.ID:
			if result.Canary.ID <= 0 {
				return slotSnapshot{}, fmt.Errorf("%w: current release", ErrManifestStale)
			}
			result.Selected = result.Canary
		default:
			return slotSnapshot{}, fmt.Errorf("%w: release is not current in slot", ErrManifestStale)
		}
	} else if result.Canary.ID > 0 && canarySelected(modelName, selectionKey, result.Canary.Ref, result.CanaryBasisPoints) {
		result.Selected = result.Canary
	} else {
		result.Selected = result.Stable
	}
	result.Selected.Selected = true
	return result, nil
}

func loadRelease(ctx context.Context, tx pgx.Tx, releaseID int64) (releaseSnapshot, error) {
	var result releaseSnapshot
	if err := tx.QueryRow(ctx, `
SELECT id,release_ref,candidate_version,candidate_hash,COALESCE(route_items_hash,''),
       COALESCE(route_items_count,0),route_items_sealed_at,stage,version,
       COALESCE(public_model_id,0),
	   COALESCE(execution_profile_id,''),COALESCE(execution_profile_version,''),
	   COALESCE(public_context_limit,0),COALESCE(public_output_limit,0),
	   COALESCE(parent_stable_release_id,0),candidate
  FROM runtime_change_releases
 WHERE id=$1`, releaseID).Scan(
		&result.ID, &result.Ref, &result.CandidateVersion, &result.CandidateHash,
		&result.RouteItemsHash, &result.RouteItemsCount, &result.RouteItemsSealed,
		&result.Stage, &result.Revision, &result.PublicModelID,
		&result.ExecutionProfileID, &result.ExecutionProfileVersion,
		&result.PublicContextLimit, &result.PublicOutputLimit,
		&result.ParentStableReleaseID, &result.CandidateJSON,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return releaseSnapshot{}, fmt.Errorf("%w: runtime release %d", ErrManifestStale, releaseID)
		}
		return releaseSnapshot{}, fmt.Errorf("%w: read runtime release: %v", ErrUnavailable, err)
	}
	return result, nil
}

func loadLegacyCandidates(ctx context.Context, tx pgx.Tx, modelName string) ([]legacyCandidate, error) {
	const query = `
SELECT c.id,c.name,c.provider,c.base_url,c.model_mapping,
       COALESCE(c.adapter_type,'openai_compatible'),COALESCE(c.adapter_config,'{}'::jsonb),
       c.priority,c.routing_weight,c.max_context,c.default_max_output,
       c.status,c.circuit_state,c.half_open_until,
       c.half_open_probe_until,COALESCE(c.half_open_probe_token,''),c.fail_count,
       c.degradation_threshold,c.routing_epoch,c.circuit_epoch,
       COALESCE(ck.id,0),COALESCE(ck.key_enc,''),COALESCE(ck.status,''),
       COALESCE(ck.state_epoch,0),ck.cooldown_until,COALESCE(ck.allowed_models,'')
  FROM channels c
  LEFT JOIN channel_keys ck ON ck.channel_id=c.id
 WHERE EXISTS (
       SELECT 1
         FROM regexp_split_to_table(COALESCE(c.models,''),'[,，;；\r\n]+') configured(model)
        WHERE BTRIM(configured.model)=$1
 )
 ORDER BY c.id,ck.id`
	rows, err := tx.Query(ctx, query, modelName)
	if err != nil {
		return nil, fmt.Errorf("%w: read legacy candidates: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	result := make([]legacyCandidate, 0)
	positions := make(map[int64]int)
	for rows.Next() {
		var candidate legacyCandidate
		var mapping json.RawMessage
		var binding legacyBinding
		if err := rows.Scan(
			&candidate.ID, &candidate.Name, &candidate.Provider, &candidate.BaseURL, &mapping,
			&candidate.AdapterType, &candidate.AdapterConfig, &candidate.Priority, &candidate.Weight,
			&candidate.MaxContext, &candidate.DefaultMaxOutput,
			&candidate.Status, &candidate.CircuitState, &candidate.HalfOpenUntil,
			&candidate.HalfOpenProbeUntil, &candidate.HalfOpenProbeToken, &candidate.FailCount,
			&candidate.DegradationThreshold, &candidate.RoutingRevision, &candidate.CircuitRevision,
			&binding.ID, &binding.Encrypted, &binding.Status, &binding.StateRevision,
			&binding.CooldownUntil, &binding.AllowedModels,
		); err != nil {
			return nil, fmt.Errorf("%w: scan legacy candidate: %v", ErrUnavailable, err)
		}
		position, exists := positions[candidate.ID]
		if !exists {
			candidate.AdapterType = adapters.NormalizeType(candidate.AdapterType)
			candidate.AdapterConfigDigest = adapterDigest(candidate.AdapterConfig)
			candidate.UpstreamModel = mappedModel(modelName, mapping)
			position = len(result)
			positions[candidate.ID] = position
			result = append(result, candidate)
		}
		if binding.ID > 0 {
			result[position].Bindings = append(result[position].Bindings, binding)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate legacy candidates: %v", ErrUnavailable, err)
	}
	return result, nil
}

func loadManifest(ctx context.Context, tx pgx.Tx, releaseID int64) ([]manifestItem, error) {
	rows, err := tx.Query(ctx, `
SELECT ordinal,deployment_id,operation_id,expected_connection_revision,
       expected_deployment_revision,expected_operation_revision,
       COALESCE(protocol_profile_id,''),COALESCE(protocol_profile_version,''),
       COALESCE(public_path,''),COALESCE(executor_contract_version,''),
       COALESCE(public_execution_binding_id,0),COALESCE(delivery_contract_version,''),
       COALESCE(charge_contract_version,''),COALESCE(idempotency_contract_version,''),
       admission,priority,weight,source_candidate_version
  FROM runtime_route_revision_items
 WHERE release_id=$1
 ORDER BY ordinal`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("%w: read normalized route manifest: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	result := make([]manifestItem, 0)
	for rows.Next() {
		var item manifestItem
		var operationRevision *int64
		if err := rows.Scan(
			&item.Ordinal, &item.DeploymentID, &item.ModelOperationID,
			&item.ExpectedConnectionRevision, &item.ExpectedDeploymentRevision,
			&operationRevision, &item.ProtocolProfileID, &item.ProtocolProfileVersion,
			&item.PublicPath, &item.ExecutorContractVersion,
			&item.PublicExecutionBindingID, &item.DeliveryContractVersion,
			&item.ChargeContractVersion, &item.IdempotencyContractVersion,
			&item.Admission, &item.Priority, &item.Weight,
			&item.SourceCandidateVersion,
		); err != nil {
			return nil, fmt.Errorf("%w: scan normalized route manifest: %v", ErrUnavailable, err)
		}
		if operationRevision != nil {
			item.ExpectedOperationRevision = *operationRevision
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate normalized route manifest: %v", ErrUnavailable, err)
	}
	return result, nil
}

func loadCanonicalCandidates(
	ctx context.Context,
	tx pgx.Tx,
	orgID, publicModelID int64,
	operationID string,
	releaseID int64,
	manifest []manifestItem,
) ([]canonicalCandidate, error) {
	const query = `
SELECT deployment.id,deployment.public_model_id,deployment.connection_id,
       COALESCE(deployment.legacy_channel_id,0),COALESCE(deployment.legacy_public_model,''),
       connection.name,connection.provider,connection.base_url,connection.adapter_type,
       connection.adapter_config,connection.admin_state,connection.config_revision,
       deployment.upstream_model,deployment.admin_state,deployment.config_revision,
       deployment.priority,deployment.routing_weight,deployment.max_context,
       deployment.default_max_output,deployment.circuit_state,
       deployment.circuit_revision,deployment.half_open_until,
       deployment.half_open_probe_until,COALESCE(deployment.half_open_probe_token,''),
       deployment.fail_count,deployment.degradation_threshold,
       COALESCE(operation.operation_id,''),COALESCE(operation.protocol_profile_id,''),
       COALESCE(operation.protocol_profile_version,''),COALESCE(operation.support_state,''),
       COALESCE(operation.evidence_state,''),COALESCE(operation.evidence_ref,''),
       operation.verified_at,COALESCE(operation.revision,0),
       COALESCE(qualification.public_path,''),COALESCE(qualification.executor_contract_version,''),
       COALESCE(qualification.qualified,FALSE),COALESCE(qualification.qualification_state,''),
       COALESCE(qualification.approval_id,0),
       COALESCE(public_qualification.public_execution_binding_id,0),
       COALESCE(public_qualification.delivery_contract_version,''),
       COALESCE(public_qualification.charge_contract_version,''),
       COALESCE(public_qualification.idempotency_contract_version,''),
       COALESCE(public_qualification.public_executable,FALSE),
       COALESCE(public_qualification.qualification_state,''),
       COALESCE(binding.id,0),COALESCE(binding.key_enc,''),COALESCE(binding.status,''),
       COALESCE(binding.state_revision,0),binding.cooldown_until,
       COALESCE(binding.legacy_channel_key_id,0),link.created_at,
       COALESCE(binding_probe.probe_id,0),COALESCE(binding_probe.success,FALSE),
       COALESCE(binding_probe.probe_profile_id,''),COALESCE(binding_probe.probe_profile_version,''),
       COALESCE(binding_probe.upstream_model,''),COALESCE(binding_probe.connection_revision,0),
       COALESCE(binding_probe.deployment_revision,0),COALESCE(binding_probe.operation_revision,0),
       COALESCE(binding_probe.public_path,''),COALESCE(binding_probe.executor_contract_version,''),
       COALESCE(binding_probe.https_origin,''),COALESCE(binding_probe.source_kind,''),
       COALESCE(binding_probe.credential_binding_id,0),
       COALESCE(binding_probe.credential_state_revision,0),binding_probe.tested_at
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  LEFT JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id AND operation.operation_id=$3
  LEFT JOIN model_operation_execution_qualifications qualification
    ON qualification.deployment_id=operation.deployment_id
   AND qualification.operation_id=operation.operation_id
  LEFT JOIN runtime_route_revision_items selected_item
    ON selected_item.release_id=$4
   AND selected_item.deployment_id=operation.deployment_id
   AND selected_item.operation_id=operation.operation_id
  LEFT JOIN model_operation_public_execution_qualifications public_qualification
    ON public_qualification.public_execution_binding_id=selected_item.public_execution_binding_id
   AND public_qualification.release_id=selected_item.release_id
   AND public_qualification.ordinal=selected_item.ordinal
  LEFT JOIN credential_binding_deployments link ON link.deployment_id=deployment.id
  LEFT JOIN provider_credential_bindings binding
    ON binding.id=link.binding_id AND binding.connection_id=deployment.connection_id
  LEFT JOIN LATERAL (
       SELECT probe.*
         FROM model_deployment_probes probe
        WHERE probe.deployment_id=deployment.id
          AND probe.operation_id=operation.operation_id
          AND probe.credential_binding_id=binding.id
        ORDER BY probe.tested_at DESC,probe.probe_id DESC
        LIMIT 1
  ) binding_probe ON TRUE
 WHERE deployment.customer_org_id=$1 AND deployment.public_model_id=$2
   AND deployment.admin_state<>'retired'
 ORDER BY deployment.id,binding.id`
	rows, err := tx.Query(ctx, query, orgID, publicModelID, operationID, releaseID)
	if err != nil {
		return nil, fmt.Errorf("%w: read canonical candidates: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	manifestByDeployment := make(map[int64]manifestItem)
	for _, item := range manifest {
		if item.ModelOperationID == operationID {
			manifestByDeployment[item.DeploymentID] = item
		}
	}
	positions := make(map[int64]int)
	result := make([]canonicalCandidate, 0)
	for rows.Next() {
		var candidate canonicalCandidate
		var operationVerifiedAt, linkedAt, probeTestedAt *time.Time
		var binding canonicalBinding
		if err := rows.Scan(
			&candidate.DeploymentID, &candidate.PublicModelID, &candidate.ConnectionID,
			&candidate.LegacyChannelID, &candidate.LegacyPublicModel,
			&candidate.Name, &candidate.Provider, &candidate.BaseURL, &candidate.AdapterType,
			&candidate.AdapterConfig, &candidate.ConnectionState, &candidate.ConnectionRevision,
			&candidate.UpstreamModel, &candidate.DeploymentState, &candidate.DeploymentRevision,
			&candidate.Priority, &candidate.Weight, &candidate.MaxContext,
			&candidate.DefaultMaxOutput, &candidate.CircuitState,
			&candidate.CircuitRevision, &candidate.HalfOpenUntil, &candidate.HalfOpenProbeUntil,
			&candidate.HalfOpenProbeToken, &candidate.FailCount, &candidate.DegradationThreshold,
			&candidate.OperationID, &candidate.ProtocolProfileID, &candidate.ProtocolProfileVersion,
			&candidate.OperationSupportState, &candidate.OperationEvidenceState,
			&candidate.OperationEvidenceRef, &operationVerifiedAt, &candidate.OperationRevision,
			&candidate.PublicPath, &candidate.ExecutorContractVersion,
			&candidate.ProductionQualified, &candidate.QualificationState,
			&candidate.ProductionApprovalID,
			&candidate.PublicExecutionBindingID, &candidate.DeliveryContractVersion,
			&candidate.ChargeContractVersion, &candidate.IdempotencyContractVersion,
			&candidate.PublicExecutionQualified, &candidate.PublicQualificationState,
			&binding.ID, &binding.Encrypted, &binding.Status,
			&binding.StateRevision, &binding.CooldownUntil, &binding.LegacyKeyID, &linkedAt,
			&binding.Probe.ID, &binding.Probe.Success, &binding.Probe.ProfileID,
			&binding.Probe.ProfileVersion, &binding.Probe.UpstreamModel,
			&binding.Probe.ConnectionRevision, &binding.Probe.DeploymentRevision,
			&binding.Probe.OperationRevision, &binding.Probe.PublicPath,
			&binding.Probe.ExecutorContractVersion, &binding.Probe.HTTPSOrigin,
			&binding.Probe.SourceKind,
			&binding.Probe.CredentialBindingID, &binding.Probe.CredentialStateRevision,
			&probeTestedAt,
		); err != nil {
			return nil, fmt.Errorf("%w: scan canonical candidate: %v", ErrUnavailable, err)
		}
		position, exists := positions[candidate.DeploymentID]
		if !exists {
			candidate.AdapterType = adapters.NormalizeType(candidate.AdapterType)
			candidate.AdapterConfigDigest = adapterDigest(candidate.AdapterConfig)
			candidate.OperationPresent = candidate.OperationID != ""
			candidate.OperationVerifiedAt = operationVerifiedAt
			candidate.Capabilities = make(map[string]capabilitySnapshot)
			if item, found := manifestByDeployment[candidate.DeploymentID]; found {
				copy := item
				candidate.Manifest = &copy
			}
			position = len(result)
			positions[candidate.DeploymentID] = position
			result = append(result, candidate)
		}
		if binding.ID > 0 {
			if linkedAt != nil {
				binding.LinkedAt = linkedAt.UTC()
			}
			binding.Probe.Present = binding.Probe.ID > 0
			if probeTestedAt != nil {
				binding.Probe.TestedAt = probeTestedAt.UTC()
			}
			result[position].Bindings = append(result[position].Bindings, binding)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate canonical candidates: %v", ErrUnavailable, err)
	}
	return result, nil
}

func loadCapabilities(ctx context.Context, tx pgx.Tx, orgID, publicModelID int64, operationID string, required []string, candidates []canonicalCandidate) error {
	if len(required) == 0 || len(candidates) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
SELECT capability.deployment_id,capability.capability_id,capability.support_state,
       capability.evidence_state,capability.evidence_ref,
       capability.verified_at,capability.revision
  FROM model_deployment_capabilities capability
  JOIN model_deployments deployment ON deployment.id=capability.deployment_id
 WHERE deployment.customer_org_id=$1 AND deployment.public_model_id=$2
   AND capability.operation_id=$3 AND capability.capability_id=ANY($4::varchar[])
 ORDER BY capability.deployment_id,capability.capability_id`, orgID, publicModelID, operationID, required)
	if err != nil {
		return fmt.Errorf("%w: read capability evidence: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	positions := make(map[int64]int, len(candidates))
	for index := range candidates {
		positions[candidates[index].DeploymentID] = index
	}
	for rows.Next() {
		var deploymentID int64
		var item capabilitySnapshot
		if err := rows.Scan(&deploymentID, &item.ID, &item.SupportState, &item.EvidenceState,
			&item.EvidenceRef, &item.VerifiedAt, &item.Revision); err != nil {
			return fmt.Errorf("%w: scan capability evidence: %v", ErrUnavailable, err)
		}
		if position, exists := positions[deploymentID]; exists {
			candidates[position].Capabilities[item.ID] = item
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: iterate capability evidence: %v", ErrUnavailable, err)
	}
	return nil
}

func mappedModel(publicModel string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return publicModel
	}
	var mapping map[string]string
	if json.Unmarshal(raw, &mapping) != nil {
		return publicModel
	}
	if upstream := strings.TrimSpace(mapping[publicModel]); upstream != "" {
		return upstream
	}
	return publicModel
}

func decodeLegacyOverrides(raw json.RawMessage, modelName string) ([]legacyOverride, error) {
	var candidate struct {
		ContractVersion string           `json:"contract_version"`
		ModelName       string           `json:"model_name"`
		Overrides       []legacyOverride `json:"overrides"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &candidate) != nil ||
		candidate.ContractVersion != legacyCandidateVersion || candidate.ModelName != modelName {
		return nil, fmt.Errorf("%w: corrupt legacy candidate", ErrManifestStale)
	}
	normalized, err := normalizeLegacyOverrides(candidate.Overrides)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid legacy overrides", ErrManifestStale)
	}
	return normalized, nil
}

func canarySelected(modelName, selectionKey, releaseRef string, basisPoints int) bool {
	if basisPoints <= 0 || basisPoints > 5000 || selectionKey == "" || releaseRef == "" {
		return false
	}
	sum := sha256.Sum256([]byte(modelName + "|" + selectionKey + "|" + releaseRef))
	return int(binary.BigEndian.Uint64(sum[:8])%10000) < basisPoints
}
