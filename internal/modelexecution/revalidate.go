package modelexecution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modellist"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const (
	legacyCandidateVersion    = "unirouter.runtime-change-candidate.v1"
	canonicalCandidateVersion = "unirouter.runtime-change-candidate.v2"
)

type runtimeProof struct {
	candidateVersion string
	parentReleaseID  int64
	item             routeItemProof
	legacyOverride   *routingpolicy.ChannelOverride
}

type routeItemProof struct {
	ordinal             int
	connectionRevision  int64
	deploymentRevision  int64
	operationRevision   int64
	profileID           string
	profileVersion      string
	publicPath          string
	executorContract    string
	publicBindingID     int64
	deliveryContract    string
	chargeContract      string
	idempotencyContract string
	admission           string
	priority            *int
	weight              *int
}

type dispatchProbeClaim struct {
	canonical []breakerSnapshot
	legacy    *breakerSnapshot
}

func (c *Coordinator) revalidateAndClaim(ctx context.Context, tx pgx.Tx, request preparedRequest) (semanticResult, error) {
	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); err != nil {
		return semanticResult{}, err
	}
	result := semanticResult{status: Applied, databaseTime: dbNow.UTC()}

	if status, err := revalidatePublicModel(ctx, tx, request); err != nil || status != Applied {
		result.status = status
		return result, err
	}
	runtime, status, err := revalidateRuntime(ctx, tx, request)
	if err != nil || status != Applied {
		result.status = status
		return result, err
	}
	if request.publicEvidence != nil {
		status, err = revalidatePublicExecutionBinding(ctx, tx, request, runtime)
		if err != nil || status != Applied {
			result.status = status
			return result, err
		}
	}

	token := request.probeToken
	if request.route.RequiresProbe != (token != "") || (token != "" && !validEphemeralToken(token)) {
		return semanticResult{}, fmt.Errorf("%w: invalid prepared probe owner", ErrUnavailable)
	}

	claim := dispatchProbeClaim{}
	if request.evidence.AuthorityMode != executionevidence.ModeLegacy {
		status, err = c.revalidateCanonical(ctx, tx, request, runtime, dbNow, &claim)
		if err != nil || status != Applied {
			result.status = status
			return result, err
		}
	}
	if request.evidence.AuthorityMode != executionevidence.ModeCanonical {
		status, err = c.revalidateLegacy(ctx, tx, request, runtime, dbNow, &claim)
		if err != nil || status != Applied {
			result.status = status
			return result, err
		}
	}
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); err != nil {
		return semanticResult{}, err
	}
	dbNow = dbNow.UTC()
	result.databaseTime = dbNow
	if request.route.RequiresProbe {
		for _, current := range claim.canonical {
			if status := breakerEligibility(
				current.circuitState, current.halfOpenUntil, current.probeUntil, current.probeToken, dbNow, true,
			); status != Applied {
				result.status = status
				return result, nil
			}
		}
		if claim.legacy != nil {
			current := *claim.legacy
			if status := breakerEligibility(
				current.circuitState, current.halfOpenUntil, current.probeUntil, current.probeToken, dbNow, true,
			); status != Applied {
				result.status = status
				return result, nil
			}
		}
		target := dbNow.Add(c.config.HalfOpenLease)
		if len(claim.canonical) > 0 {
			status, err = claimCanonicalProbeGroup(ctx, tx, claim.canonical, dbNow, target, token)
			if err != nil || status != Applied {
				result.status = status
				return result, err
			}
		}
		if claim.legacy != nil {
			status, err = claimLegacyProbe(ctx, tx, request.evidence, *claim.legacy, dbNow, target, token)
			if err != nil || status != Applied {
				result.status = status
				return result, err
			}
		}
	}
	result.probeToken = token
	return result, nil
}

func revalidatePublicModel(ctx context.Context, tx pgx.Tx, request preparedRequest) (Status, error) {
	if request.plan.PublicModelID == 0 {
		if request.evidence.AuthorityMode == executionevidence.ModeLegacy {
			return Applied, nil
		}
		return Stale, nil
	}
	var name, lifecycle, executionProfileID, executionProfileVersion string
	var contextLimit, outputLimit int
	err := tx.QueryRow(ctx, `
SELECT model_name,lifecycle,context_length,output_limit,execution_profile_id,execution_profile_version
  FROM model_registry
 WHERE id=$1 AND org_id=$2
 FOR SHARE`, request.plan.PublicModelID, request.identity.OrgID).Scan(
		&name, &lifecycle, &contextLimit, &outputLimit, &executionProfileID, &executionProfileVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if lifecycle == "retired" {
		return Disabled, nil
	}
	if name != request.plan.PublicModel || contextLimit != request.plan.PublicContextLimit || outputLimit != request.plan.PublicOutputLimit ||
		executionProfileID != request.plan.ExecutionProfileID || executionProfileVersion != request.plan.ExecutionProfileVersion {
		return Stale, nil
	}
	return Applied, nil
}

func revalidateRuntime(ctx context.Context, tx pgx.Tx, request preparedRequest) (runtimeProof, Status, error) {
	var stableID, canaryID, slotModelID sql.NullInt64
	var slotVersion int64
	err := tx.QueryRow(ctx, `
SELECT stable_release_id,canary_release_id,public_model_id,slot_version
  FROM runtime_change_slots
 WHERE model_name=$1
 FOR SHARE`, request.plan.PublicModel).Scan(&stableID, &canaryID, &slotModelID, &slotVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		if request.plan.RuntimeReleaseID == 0 && request.plan.SlotVersion == 0 {
			return runtimeProof{}, Applied, nil
		}
		return runtimeProof{}, Stale, nil
	}
	if err != nil {
		return runtimeProof{}, "", err
	}
	if slotVersion != request.plan.SlotVersion {
		return runtimeProof{}, Stale, nil
	}
	if request.plan.RuntimeReleaseID == 0 {
		return runtimeProof{}, Applied, nil
	}
	selectedID := stableID.Int64
	if request.plan.RuntimeStage == "canary" {
		selectedID = canaryID.Int64
	} else if request.plan.RuntimeStage != "active" {
		return runtimeProof{}, Stale, nil
	}
	if selectedID != request.plan.RuntimeReleaseID {
		return runtimeProof{}, Stale, nil
	}

	var proof runtimeProof
	var releaseRef, modelName, stage, candidateHash string
	var candidateJSON json.RawMessage
	var releasePublicModel sql.NullInt64
	var sealedAt *time.Time
	var routeItemsHash string
	var routeItemsCount int
	err = tx.QueryRow(ctx, `
SELECT release_ref,model_name,candidate_version,stage,public_model_id,candidate_hash,candidate,
       route_items_sealed_at,COALESCE(route_items_hash,''),COALESCE(route_items_count,0),
       COALESCE(parent_stable_release_id,0)
  FROM runtime_change_releases
 WHERE id=$1
 FOR SHARE`, selectedID).Scan(
		&releaseRef, &modelName, &proof.candidateVersion, &stage, &releasePublicModel, &candidateHash, &candidateJSON,
		&sealedAt, &routeItemsHash, &routeItemsCount, &proof.parentReleaseID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return runtimeProof{}, Stale, nil
	}
	if err != nil {
		return runtimeProof{}, "", err
	}
	if releaseRef != request.plan.RuntimeReleaseRef || modelName != request.plan.PublicModel || stage != request.plan.RuntimeStage {
		return runtimeProof{}, Stale, nil
	}
	if request.evidence.AuthorityMode != executionevidence.ModeLegacy {
		if releasePublicModel.Int64 != request.evidence.PublicModelID || sealedAt == nil || routeItemsHash == "" || routeItemsCount <= 0 {
			return runtimeProof{}, Stale, nil
		}
		if request.evidence.ExecutionContractSource == executionevidence.ContractSourceCanonicalProfile {
			if proof.candidateVersion != canonicalCandidateVersion || !slotModelID.Valid || slotModelID.Int64 != request.evidence.PublicModelID {
				return runtimeProof{}, Stale, nil
			}
		} else if proof.candidateVersion != legacyCandidateVersion || slotModelID.Valid {
			return runtimeProof{}, Stale, nil
		}
	}
	if request.evidence.AuthorityMode == executionevidence.ModeLegacy && proof.candidateVersion != legacyCandidateVersion {
		return runtimeProof{}, Stale, nil
	}
	if proof.candidateVersion == legacyCandidateVersion {
		override, valid := legacyCandidateOverride(candidateJSON, candidateHash, request.plan.PublicModel, request.evidence.LegacyChannelID)
		if !valid {
			return runtimeProof{}, Stale, nil
		}
		proof.legacyOverride = override
	}

	if request.evidence.AuthorityMode != executionevidence.ModeLegacy {
		item, found, itemErr := loadRouteItem(ctx, tx, selectedID, request.evidence.DeploymentID, request.underlying.id)
		if itemErr != nil {
			return runtimeProof{}, "", itemErr
		}
		if !found || item.admission == "deny" {
			return runtimeProof{}, Stale, nil
		}
		proof.item = item
	}
	if request.evidence.AuthorityMode == executionevidence.ModeVerify && proof.candidateVersion == canonicalCandidateVersion {
		status, parentErr := revalidateCompatibilityParent(ctx, tx, request, proof.parentReleaseID)
		if parentErr != nil || status != Applied {
			return runtimeProof{}, status, parentErr
		}
	}
	return proof, Applied, nil
}

func loadRouteItem(ctx context.Context, tx pgx.Tx, releaseID, deploymentID int64, operationID string) (routeItemProof, bool, error) {
	var result routeItemProof
	var priority, weight sql.NullInt32
	err := tx.QueryRow(ctx, `
SELECT expected_connection_revision,expected_deployment_revision,
       expected_operation_revision,protocol_profile_id,protocol_profile_version,
       ordinal,COALESCE(public_path,''),COALESCE(executor_contract_version,''),
       COALESCE(public_execution_binding_id,0),COALESCE(delivery_contract_version,''),
       COALESCE(charge_contract_version,''),COALESCE(idempotency_contract_version,''),
       admission,priority,weight
  FROM runtime_route_revision_items
 WHERE release_id=$1 AND deployment_id=$2 AND operation_id=$3
 FOR SHARE`, releaseID, deploymentID, operationID).Scan(
		&result.connectionRevision, &result.deploymentRevision, &result.operationRevision,
		&result.profileID, &result.profileVersion, &result.ordinal,
		&result.publicPath, &result.executorContract, &result.publicBindingID,
		&result.deliveryContract, &result.chargeContract, &result.idempotencyContract,
		&result.admission, &priority, &weight,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return routeItemProof{}, false, nil
	}
	if err != nil {
		return routeItemProof{}, false, err
	}
	if priority.Valid {
		value := int(priority.Int32)
		result.priority = &value
	}
	if weight.Valid {
		value := int(weight.Int32)
		result.weight = &value
	}
	return result, true, nil
}

func revalidatePublicExecutionBinding(
	ctx context.Context,
	tx pgx.Tx,
	request preparedRequest,
	runtime runtimeProof,
) (Status, error) {
	public := request.publicEvidence
	if public == nil || runtime.item.ordinal <= 0 ||
		runtime.item.publicBindingID != public.PublicExecutionBindingID ||
		runtime.item.publicPath != request.route.PublicPath ||
		runtime.item.executorContract != request.route.ExecutorContractVersion ||
		runtime.item.deliveryContract != public.DeliveryContractVersion ||
		runtime.item.chargeContract != public.ChargeContractVersion ||
		runtime.item.idempotencyContract != public.IdempotencyContractVersion {
		return Stale, nil
	}

	var customerOrgID, publicModelID, releaseID, deploymentID, connectionID int64
	var publicBindingID, productionApprovalID int64
	var ordinal int
	var connectionRevision, deploymentRevision, operationRevision int64
	var operationID, publicPath, profileID, profileVersion, executorContract string
	var deliveryContract, chargeContract, idempotencyContract, qualificationState string
	var publicExecutable bool
	err := tx.QueryRow(ctx, `
SELECT public_execution_binding_id,customer_org_id,public_model_id,release_id,ordinal,
       deployment_id,connection_id,operation_id,production_approval_id,
       approved_connection_revision,approved_deployment_revision,
       approved_operation_revision,public_path,protocol_profile_id,
       protocol_profile_version,executor_contract_version,
       delivery_contract_version,charge_contract_version,
       idempotency_contract_version,qualification_state,public_executable
  FROM model_operation_public_execution_qualifications
 WHERE public_execution_binding_id=$1 AND release_id=$2 AND ordinal=$3`,
		public.PublicExecutionBindingID, request.plan.RuntimeReleaseID, runtime.item.ordinal,
	).Scan(
		&publicBindingID, &customerOrgID, &publicModelID, &releaseID, &ordinal,
		&deploymentID, &connectionID, &operationID, &productionApprovalID,
		&connectionRevision, &deploymentRevision, &operationRevision,
		&publicPath, &profileID, &profileVersion, &executorContract,
		&deliveryContract, &chargeContract, &idempotencyContract,
		&qualificationState, &publicExecutable,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	evidence := request.evidence
	if !publicExecutable || qualificationState != "public_executable" ||
		publicBindingID != public.PublicExecutionBindingID ||
		customerOrgID != request.identity.OrgID || publicModelID != evidence.PublicModelID ||
		releaseID != evidence.RuntimeReleaseID || ordinal != runtime.item.ordinal ||
		deploymentID != evidence.DeploymentID || connectionID != evidence.ConnectionID ||
		operationID != evidence.ModelOperationID || productionApprovalID != request.route.ProductionApprovalID ||
		connectionRevision != evidence.ConnectionRevision || deploymentRevision != evidence.DeploymentRevision ||
		operationRevision != evidence.OperationRevision || publicPath != request.route.PublicPath ||
		profileID != evidence.ProtocolProfileID || profileVersion != evidence.ProtocolProfileVersion ||
		executorContract != request.route.ExecutorContractVersion ||
		deliveryContract != public.DeliveryContractVersion ||
		chargeContract != public.ChargeContractVersion ||
		idempotencyContract != public.IdempotencyContractVersion {
		return Stale, nil
	}
	return Applied, nil
}

func revalidateCompatibilityParent(ctx context.Context, tx pgx.Tx, request preparedRequest, parentID int64) (Status, error) {
	if parentID <= 0 {
		return Stale, nil
	}
	var candidateVersion, stage string
	var publicModelID sql.NullInt64
	var sealedAt *time.Time
	err := tx.QueryRow(ctx, `
SELECT candidate_version,stage,public_model_id,route_items_sealed_at
  FROM runtime_change_releases
 WHERE id=$1
 FOR SHARE`, parentID).Scan(&candidateVersion, &stage, &publicModelID, &sealedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if candidateVersion != legacyCandidateVersion || stage != "rolled_back" ||
		publicModelID.Int64 != request.evidence.PublicModelID || sealedAt == nil {
		return Stale, nil
	}
	item, found, err := loadRouteItem(ctx, tx, parentID, request.evidence.DeploymentID, "legacy.any.v1")
	if err != nil {
		return "", err
	}
	if !found || item.admission == "deny" || item.profileID != "legacy.channel_model_probe" || item.profileVersion != "v1" {
		return Stale, nil
	}
	return Applied, nil
}

func (c *Coordinator) revalidateCanonical(
	ctx context.Context,
	tx pgx.Tx,
	request preparedRequest,
	runtime runtimeProof,
	dbNow time.Time,
	claim *dispatchProbeClaim,
) (Status, error) {
	evidence := request.evidence
	var connectionName, provider, baseURL, adapterType, connectionState string
	var adapterConfig json.RawMessage
	var connectionRevision int64
	var legacyChannelID sql.NullInt64
	err := tx.QueryRow(ctx, `
SELECT name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision,legacy_channel_id
  FROM provider_connections
 WHERE id=$1
 FOR SHARE`, evidence.ConnectionID).Scan(
		&connectionName, &provider, &baseURL, &adapterType, &adapterConfig,
		&connectionState, &connectionRevision, &legacyChannelID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if connectionState != "enabled" {
		return Disabled, nil
	}
	if connectionRevision != evidence.ConnectionRevision || runtime.item.connectionRevision != evidence.ConnectionRevision ||
		connectionName != request.route.ConnectionName || provider != request.route.Provider || baseURL != request.route.BaseURL ||
		adapters.NormalizeType(adapterType) != request.route.AdapterType || !sameJSON(adapterConfig, request.route.AdapterConfig) {
		return Stale, nil
	}

	var verifyProbeGroup []breakerSnapshot
	if request.route.RequiresProbe && evidence.AuthorityMode == executionevidence.ModeVerify {
		var groupStatus Status
		verifyProbeGroup, groupStatus, err = lockVerifyCanonicalProbeGroup(ctx, tx, evidence, dbNow)
		if err != nil || groupStatus != Applied {
			return groupStatus, err
		}
	}

	lock := "FOR SHARE"
	if request.route.RequiresProbe {
		lock = "FOR UPDATE"
	}
	var publicModelID, connectionID, deploymentRevision, circuitRevision int64
	var upstreamModel, deploymentState, circuitState string
	var deploymentLegacyChannelID sql.NullInt64
	var deploymentLegacyPublicModel sql.NullString
	var priority, weight, maxContext, maxOutput int
	var halfOpenUntil, halfOpenProbeUntil *time.Time
	var halfOpenProbeToken string
	query := `
SELECT public_model_id,connection_id,upstream_model,admin_state,config_revision,
       priority,routing_weight,max_context,default_max_output,circuit_state,circuit_revision,
       half_open_until,half_open_probe_until,COALESCE(half_open_probe_token,''),
       legacy_channel_id,legacy_public_model
  FROM model_deployments
 WHERE id=$1 AND customer_org_id=$2 ` + lock
	err = tx.QueryRow(ctx, query, evidence.DeploymentID, request.identity.OrgID).Scan(
		&publicModelID, &connectionID, &upstreamModel, &deploymentState, &deploymentRevision,
		&priority, &weight, &maxContext, &maxOutput, &circuitState, &circuitRevision,
		&halfOpenUntil, &halfOpenProbeUntil, &halfOpenProbeToken,
		&deploymentLegacyChannelID, &deploymentLegacyPublicModel,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if deploymentState != "enabled" {
		return Disabled, nil
	}
	if publicModelID != evidence.PublicModelID || connectionID != evidence.ConnectionID ||
		deploymentRevision != evidence.DeploymentRevision || runtime.item.deploymentRevision != evidence.DeploymentRevision ||
		circuitRevision != evidence.CircuitRevision || upstreamModel != request.route.UpstreamModel ||
		maxContext != request.route.MaxContext || maxOutput != request.route.DefaultMaxOutput {
		return Stale, nil
	}
	effectivePriority, effectiveWeight := priority, weight
	if runtime.item.priority != nil {
		effectivePriority = *runtime.item.priority
	}
	if runtime.item.weight != nil {
		effectiveWeight = *runtime.item.weight
	}
	if effectivePriority != request.route.Priority || effectiveWeight != request.route.Weight {
		return Stale, nil
	}
	if evidence.AuthorityMode == executionevidence.ModeVerify &&
		(!legacyChannelID.Valid || legacyChannelID.Int64 != evidence.LegacyChannelID ||
			!deploymentLegacyChannelID.Valid || deploymentLegacyChannelID.Int64 != evidence.LegacyChannelID ||
			!deploymentLegacyPublicModel.Valid || deploymentLegacyPublicModel.String != request.plan.PublicModel) {
		return Stale, nil
	}
	if status := breakerEligibility(circuitState, halfOpenUntil, halfOpenProbeUntil, halfOpenProbeToken, dbNow, request.route.RequiresProbe); status != Applied {
		return status, nil
	}

	if status, operationErr := revalidateOperation(ctx, tx, request, runtime); operationErr != nil || status != Applied {
		return status, operationErr
	}
	if status, capabilityErr := revalidateCapabilities(ctx, tx, request); capabilityErr != nil || status != Applied {
		return status, capabilityErr
	}
	if status, bindingErr := revalidateBindingAndProbe(ctx, tx, request, dbNow); bindingErr != nil || status != Applied {
		return status, bindingErr
	}

	if request.route.RequiresProbe {
		if evidence.AuthorityMode == executionevidence.ModeVerify {
			claim.canonical = verifyProbeGroup
		} else {
			claim.canonical = []breakerSnapshot{{
				id: evidence.DeploymentID, connectionID: connectionID, publicModelID: publicModelID,
				legacyChannelID: deploymentLegacyChannelID, adminState: deploymentState,
				configRevision: deploymentRevision, circuitState: circuitState,
				circuitRevision: circuitRevision, halfOpenUntil: halfOpenUntil,
				probeUntil: halfOpenProbeUntil, probeToken: halfOpenProbeToken,
			}}
		}
	}
	return Applied, nil
}

func (c *Coordinator) revalidateLegacy(
	ctx context.Context,
	tx pgx.Tx,
	request preparedRequest,
	runtime runtimeProof,
	dbNow time.Time,
	claim *dispatchProbeClaim,
) (Status, error) {
	evidence := request.evidence
	lock := "FOR SHARE"
	if request.route.RequiresProbe {
		lock = "FOR UPDATE"
	}
	var name, provider, baseURL, models, adapterType, status, circuitState string
	var modelMapping, adapterConfig json.RawMessage
	var maxContext, maxOutput, priority, weight int
	var routingRevision, circuitRevision int64
	var halfOpenUntil, halfOpenProbeUntil *time.Time
	var halfOpenProbeToken string
	query := `
SELECT name,provider,base_url,models,model_mapping,
       COALESCE(adapter_type,'openai_compatible'),COALESCE(adapter_config,'{}'::jsonb),
       max_context,default_max_output,priority,routing_weight,status,circuit_state,
       routing_epoch,circuit_epoch,half_open_until,half_open_probe_until,
       COALESCE(half_open_probe_token,'')
  FROM channels
 WHERE id=$1 ` + lock
	err := tx.QueryRow(ctx, query, evidence.LegacyChannelID).Scan(
		&name, &provider, &baseURL, &models, &modelMapping, &adapterType, &adapterConfig,
		&maxContext, &maxOutput, &priority, &weight, &status, &circuitState,
		&routingRevision, &circuitRevision, &halfOpenUntil, &halfOpenProbeUntil, &halfOpenProbeToken,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	effectiveStatus := status
	if request.evidence.AuthorityMode == executionevidence.ModeLegacy && runtime.legacyOverride != nil {
		switch runtime.legacyOverride.Admission {
		case routingpolicy.AdmissionAllow:
			// The selected v1 release is the legacy administrative authority.
			// It may promote a staged channel, but it must never resurrect an
			// explicitly disabled channel.
			if status != "disabled" {
				effectiveStatus = "active"
			}
		case routingpolicy.AdmissionDeny:
			effectiveStatus = "disabled"
		case routingpolicy.AdmissionInherit:
		default:
			return Stale, nil
		}
	}
	if effectiveStatus != "active" {
		return Disabled, nil
	}
	if routingRevision != evidence.LegacyRoutingRevision || circuitRevision != evidence.LegacyCircuitRevision ||
		name != request.route.ConnectionName || provider != request.route.Provider || baseURL != request.route.BaseURL ||
		adapters.NormalizeType(adapterType) != request.route.AdapterType || !sameJSON(adapterConfig, request.route.AdapterConfig) ||
		maxContext != request.route.MaxContext || maxOutput != request.route.DefaultMaxOutput ||
		!modellist.Contains(models, request.plan.PublicModel) || mappedLegacyModel(request.plan.PublicModel, modelMapping) != request.route.UpstreamModel {
		return Stale, nil
	}
	effectivePriority, effectiveWeight := priority, weight
	if request.evidence.AuthorityMode == executionevidence.ModeLegacy && runtime.legacyOverride != nil {
		if runtime.legacyOverride.Priority != nil {
			effectivePriority = *runtime.legacyOverride.Priority
		}
		if runtime.legacyOverride.Weight != nil {
			effectiveWeight = *runtime.legacyOverride.Weight
		}
	} else {
		if runtime.item.priority != nil {
			effectivePriority = *runtime.item.priority
		}
		if runtime.item.weight != nil {
			effectiveWeight = *runtime.item.weight
		}
	}
	if effectivePriority != request.route.Priority || effectiveWeight != request.route.Weight {
		return Stale, nil
	}
	if status := breakerEligibility(circuitState, halfOpenUntil, halfOpenProbeUntil, halfOpenProbeToken, dbNow, request.route.RequiresProbe); status != Applied {
		return status, nil
	}

	var channelID, stateRevision int64
	var encrypted, keyStatus, allowedModels string
	var cooldown *time.Time
	err = tx.QueryRow(ctx, `
SELECT channel_id,key_enc,status,state_epoch,cooldown_until,COALESCE(allowed_models,'')
  FROM channel_keys
 WHERE id=$1
 FOR SHARE`, evidence.LegacyChannelKeyID).Scan(
		&channelID, &encrypted, &keyStatus, &stateRevision, &cooldown, &allowedModels,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if keyStatus != "active" || (cooldown != nil && cooldown.After(dbNow)) {
		return Disabled, nil
	}
	if channelID != evidence.LegacyChannelID || stateRevision != evidence.LegacyCredentialStateRevision ||
		(allowedModels != "" && !modellist.Contains(allowedModels, request.plan.PublicModel)) ||
		subtle.ConstantTimeCompare([]byte(encrypted), []byte(request.route.EncryptedSecret)) != 1 {
		return Stale, nil
	}

	if request.route.RequiresProbe {
		claim.legacy = &breakerSnapshot{
			id: evidence.LegacyChannelID, adminState: status, configRevision: routingRevision,
			circuitState: circuitState, circuitRevision: circuitRevision,
			halfOpenUntil: halfOpenUntil, probeUntil: halfOpenProbeUntil, probeToken: halfOpenProbeToken,
		}
	}
	return Applied, nil
}

func breakerEligibility(state string, halfOpenUntil, probeUntil *time.Time, probeToken string, now time.Time, requiresProbe bool) Status {
	if !requiresProbe {
		if state == "healthy" {
			return Applied
		}
		return Stale
	}
	if state != "half_open" || halfOpenUntil == nil || halfOpenUntil.After(now) {
		return Stale
	}
	if probeToken != "" && probeUntil != nil && probeUntil.After(now) {
		return LeaseLost
	}
	return Applied
}

func revalidateOperation(ctx context.Context, tx pgx.Tx, request preparedRequest, runtime runtimeProof) (Status, error) {
	var support, evidenceState, evidenceRef, profileID, profileVersion string
	var verifiedAt *time.Time
	var revision int64
	err := tx.QueryRow(ctx, `
SELECT support_state,evidence_state,evidence_ref,verified_at,revision,
       protocol_profile_id,protocol_profile_version
  FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2
 FOR SHARE`, request.evidence.DeploymentID, request.underlying.id).Scan(
		&support, &evidenceState, &evidenceRef, &verifiedAt, &revision, &profileID, &profileVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if support != "enabled" {
		return Disabled, nil
	}
	if revision != runtime.item.operationRevision || profileID != runtime.item.profileID || profileVersion != runtime.item.profileVersion ||
		profileID != request.underlying.profileID || profileVersion != request.underlying.profileVersion {
		return Stale, nil
	}
	if request.evidence.ExecutionContractSource == executionevidence.ContractSourceCanonicalProfile {
		if revision != request.evidence.OperationRevision || evidenceState != request.evidence.OperationEvidenceState ||
			evidenceRef != request.evidence.OperationEvidenceRef || !sameTimePointer(verifiedAt, request.evidence.OperationVerifiedAt) {
			return Stale, nil
		}
	}
	return Applied, nil
}

func revalidateCapabilities(ctx context.Context, tx pgx.Tx, request preparedRequest) (Status, error) {
	if request.evidence.ExecutionContractSource != executionevidence.ContractSourceCanonicalProfile {
		return Applied, nil
	}
	wanted := request.evidence.CapabilityEvidence
	ids := make([]string, 0, len(wanted))
	for _, item := range wanted {
		ids = append(ids, item.CapabilityID)
	}
	if !slices.Equal(ids, request.plan.RequiredCapabilities) {
		return Stale, nil
	}
	if len(ids) == 0 {
		return Applied, nil
	}
	rows, err := tx.Query(ctx, `
SELECT capability_id,support_state,evidence_state,evidence_ref,verified_at,revision
  FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id=ANY($3::varchar[])
 ORDER BY capability_id
 FOR SHARE`, request.evidence.DeploymentID, request.underlying.id, ids)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	actual := make([]executionevidence.CapabilityEntry, 0, len(ids))
	for rows.Next() {
		var item executionevidence.CapabilityEntry
		if err := rows.Scan(&item.CapabilityID, &item.SupportState, &item.EvidenceState, &item.EvidenceRef, &item.VerifiedAt, &item.Revision); err != nil {
			return "", err
		}
		actual = append(actual, item)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(actual) != len(wanted) {
		return Stale, nil
	}
	for index := range actual {
		if actual[index].CapabilityID != wanted[index].CapabilityID || actual[index].SupportState != wanted[index].SupportState ||
			actual[index].EvidenceState != wanted[index].EvidenceState || actual[index].EvidenceRef != wanted[index].EvidenceRef ||
			actual[index].Revision != wanted[index].Revision || !sameTimePointer(actual[index].VerifiedAt, wanted[index].VerifiedAt) {
			return Stale, nil
		}
	}
	return Applied, nil
}

func revalidateBindingAndProbe(ctx context.Context, tx pgx.Tx, request preparedRequest, dbNow time.Time) (Status, error) {
	evidence := request.evidence
	var connectionID, stateRevision int64
	var encrypted, status string
	var cooldown *time.Time
	var legacyKeyID sql.NullInt64
	err := tx.QueryRow(ctx, `
SELECT connection_id,key_enc,status,state_revision,cooldown_until,legacy_channel_key_id
  FROM provider_credential_bindings
 WHERE id=$1
 FOR SHARE`, evidence.CredentialBindingID).Scan(
		&connectionID, &encrypted, &status, &stateRevision, &cooldown, &legacyKeyID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if status != "active" || (cooldown != nil && cooldown.After(dbNow)) {
		return Disabled, nil
	}
	if connectionID != evidence.ConnectionID || stateRevision != evidence.CredentialStateRevision ||
		subtle.ConstantTimeCompare([]byte(encrypted), []byte(request.route.EncryptedSecret)) != 1 {
		return Stale, nil
	}
	if evidence.AuthorityMode == executionevidence.ModeVerify &&
		(!legacyKeyID.Valid || legacyKeyID.Int64 != evidence.LegacyChannelKeyID) {
		return Stale, nil
	}

	var linkedAt time.Time
	err = tx.QueryRow(ctx, `
SELECT created_at
  FROM credential_binding_deployments
 WHERE binding_id=$1 AND deployment_id=$2 AND connection_id=$3
 FOR SHARE`, evidence.CredentialBindingID, evidence.DeploymentID, evidence.ConnectionID).Scan(&linkedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if !sameTime(linkedAt, evidence.CredentialLinkedAt) {
		return Stale, nil
	}

	var probeID, connectionRevision, deploymentRevision, bindingID, credentialRevision int64
	var operationRevision int64
	var success bool
	var operationID, profileID, profileVersion, upstream string
	var publicPath, executorContract, httpsOrigin, sourceKind string
	var testedAt time.Time
	err = tx.QueryRow(ctx, `
SELECT probe_id,success,operation_id,probe_profile_id,probe_profile_version,
       upstream_model,connection_revision,deployment_revision,credential_binding_id,
       credential_state_revision,tested_at,COALESCE(operation_revision,0),
       COALESCE(public_path,''),COALESCE(executor_contract_version,''),
       COALESCE(https_origin,''),COALESCE(source_kind,'')
  FROM model_deployment_probes
 WHERE deployment_id=$1 AND operation_id=$2 AND credential_binding_id=$3
 ORDER BY tested_at DESC,probe_id DESC
 LIMIT 1
 FOR SHARE`, evidence.DeploymentID, request.underlying.id, evidence.CredentialBindingID).Scan(
		&probeID, &success, &operationID, &profileID, &profileVersion, &upstream,
		&connectionRevision, &deploymentRevision, &bindingID, &credentialRevision, &testedAt,
		&operationRevision, &publicPath, &executorContract, &httpsOrigin, &sourceKind,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stale, nil
	}
	if err != nil {
		return "", err
	}
	if !success || probeID != evidence.CredentialProbeID || operationID != request.underlying.id ||
		profileID != request.underlying.profileID || profileVersion != request.underlying.profileVersion ||
		upstream != request.route.UpstreamModel || connectionRevision != evidence.ConnectionRevision ||
		deploymentRevision != evidence.DeploymentRevision || bindingID != evidence.CredentialBindingID ||
		credentialRevision != evidence.CredentialStateRevision || !sameTime(testedAt, evidence.CredentialProbeTestedAt) || testedAt.Before(linkedAt) {
		return Stale, nil
	}
	if request.publicEvidence != nil {
		origin, originErr := modelqualification.NormalizeHTTPSOrigin(request.route.BaseURL)
		if originErr != nil || operationRevision != evidence.OperationRevision ||
			publicPath != request.route.PublicPath || executorContract != request.route.ExecutorContractVersion ||
			httpsOrigin != origin || sourceKind != modelqualification.LiveProbeSourceKind {
			return Stale, nil
		}
	}
	return Applied, nil
}

func sameJSON(left, right json.RawMessage) bool {
	decode := func(raw json.RawMessage) (any, bool) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if len(raw) == 0 || decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			return nil, false
		}
		return value, true
	}
	l, ok := decode(left)
	if !ok {
		return bytes.Equal(left, right)
	}
	r, ok := decode(right)
	if !ok {
		return false
	}
	lb, _ := json.Marshal(l)
	rb, _ := json.Marshal(r)
	return bytes.Equal(lb, rb)
}

func sameTime(left, right time.Time) bool {
	return left.UTC().Truncate(time.Microsecond).Equal(right.UTC().Truncate(time.Microsecond))
}

func sameTimePointer(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return sameTime(*left, *right)
}

func mappedLegacyModel(publicModel string, raw json.RawMessage) string {
	var mapping map[string]string
	if len(raw) != 0 && json.Unmarshal(raw, &mapping) == nil {
		if upstream := strings.TrimSpace(mapping[publicModel]); upstream != "" {
			return upstream
		}
	}
	return publicModel
}

func legacyCandidateOverride(raw json.RawMessage, expectedHash, model string, channelID int64) (*routingpolicy.ChannelOverride, bool) {
	var candidate struct {
		ContractVersion string                          `json:"contract_version"`
		ModelName       string                          `json:"model_name"`
		Overrides       []routingpolicy.ChannelOverride `json:"overrides"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &candidate) != nil ||
		candidate.ContractVersion != legacyCandidateVersion || candidate.ModelName != model {
		return nil, false
	}
	normalized, err := routingpolicy.ValidateOverrides(candidate.Overrides)
	if err != nil {
		return nil, false
	}
	payload := struct {
		ContractVersion string                          `json:"contract_version"`
		ModelName       string                          `json:"model_name"`
		Overrides       []routingpolicy.ChannelOverride `json:"overrides"`
	}{candidate.ContractVersion, candidate.ModelName, normalized}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	sum := sha256.Sum256(body)
	if expectedHash != "sha256:"+hex.EncodeToString(sum[:]) {
		return nil, false
	}
	for index := range normalized {
		if normalized[index].ChannelID == channelID {
			copy := normalized[index]
			return &copy, copy.ExpectedRoutingEpoch > 0
		}
	}
	return nil, true
}
