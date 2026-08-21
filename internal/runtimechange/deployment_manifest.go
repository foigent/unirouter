package runtimechange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

const maxDeploymentRoutes = 256

func NormalizeDeploymentDraft(request DeploymentDraftRequest) (DeploymentDraftRequest, error) {
	request = cloneDeploymentDraftRequest(request)
	request.ReleaseRef = strings.TrimSpace(strings.ToLower(request.ReleaseRef))
	request.Reason = strings.TrimSpace(request.Reason)
	if request.ReleaseRef != "" && !releaseRefPattern.MatchString(request.ReleaseRef) {
		return DeploymentDraftRequest{}, fmt.Errorf("%w: release_ref", ErrInvalid)
	}
	if request.PublicModelID <= 0 {
		return DeploymentDraftRequest{}, fmt.Errorf("%w: public_model_id", ErrInvalid)
	}
	if request.Reason == "" || len(request.Reason) > 500 {
		return DeploymentDraftRequest{}, fmt.Errorf("%w: reason", ErrInvalid)
	}
	if len(request.DeploymentRoutes) == 0 || len(request.DeploymentRoutes) > maxDeploymentRoutes {
		return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes", ErrInvalid)
	}
	seen := make(map[string]struct{}, len(request.DeploymentRoutes))
	for index := range request.DeploymentRoutes {
		route := &request.DeploymentRoutes[index]
		if route.DeploymentID <= 0 || route.ExpectedConnectionRevision <= 0 ||
			route.ExpectedDeploymentRevision <= 0 || route.ExpectedOperationRevision <= 0 {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] identity", ErrInvalid, index)
		}
		operation, err := modeldeployment.NormalizeOperationID(route.ModelOperationID)
		if err != nil {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] model_operation_id", ErrInvalid, index)
		}
		route.ModelOperationID = operation
		if _, ok := modeloperation.LookupID(modeloperation.ID(operation)); !ok {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] unknown model_operation_id", ErrInvalid, index)
		}
		profileID, err := modeldeployment.NormalizeIdentity(route.ProtocolProfileID, 100)
		if err != nil || profileID == modeldeployment.LegacyOperationID ||
			profileID == modeldeployment.LegacyProbeProfileID {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] protocol_profile_id", ErrInvalid, index)
		}
		profileVersion, err := modeldeployment.NormalizeIdentity(route.ProtocolProfileVersion, 40)
		if err != nil {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] protocol_profile_version", ErrInvalid, index)
		}
		route.ProtocolProfileID = profileID
		route.ProtocolProfileVersion = profileVersion
		if modelqualification.IsQualificationRequired(route.ModelOperationID) {
			binding, bindingErr := modelqualification.CompiledBinding(route.ModelOperationID)
			if bindingErr != nil || route.PublicPath != binding.PublicPath ||
				route.ProtocolProfileID != binding.ProtocolProfileID ||
				route.ProtocolProfileVersion != binding.ProtocolProfileVersion ||
				route.ExecutorContractVersion != binding.ExecutorContractVersion {
				return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] execution support binding", ErrInvalid, index)
			}
			if route.PublicExecutionBindingID != 0 || route.DeliveryContractVersion != "" ||
				route.ChargeContractVersion != "" || route.IdempotencyContractVersion != "" {
				return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] public execution binding is server-owned", ErrInvalid, index)
			}
			publicBinding, publicBindingErr := modelqualification.CompiledPublicBinding(route.ModelOperationID)
			if publicBindingErr != nil {
				return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] public execution support", ErrInvalid, index)
			}
			route.DeliveryContractVersion = publicBinding.DeliveryContractVersion
			route.ChargeContractVersion = publicBinding.ChargeContractVersion
			route.IdempotencyContractVersion = publicBinding.IdempotencyContractVersion
		} else if route.PublicPath != "" || route.ExecutorContractVersion != "" ||
			route.PublicExecutionBindingID != 0 || route.DeliveryContractVersion != "" ||
			route.ChargeContractVersion != "" || route.IdempotencyContractVersion != "" {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] unexpected execution support binding", ErrInvalid, index)
		}
		if route.ModelOperationID == modeldeployment.LegacyOperationID {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] legacy operation is compatibility-only", ErrInvalid, index)
		}
		route.Admission = strings.ToLower(strings.TrimSpace(route.Admission))
		if route.Admission != routingpolicy.AdmissionAllow {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] admission must be allow for a full manifest", ErrInvalid, index)
		}
		if route.Priority != nil && (*route.Priority < 1 || *route.Priority > 10000) {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] priority", ErrInvalid, index)
		}
		if route.Weight != nil && (*route.Weight < 1 || *route.Weight > 1000) {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: deployment_routes[%d] weight", ErrInvalid, index)
		}
		key := fmt.Sprintf("%d\x00%s", route.DeploymentID, route.ModelOperationID)
		if _, duplicate := seen[key]; duplicate {
			return DeploymentDraftRequest{}, fmt.Errorf("%w: duplicate deployment route", ErrInvalid)
		}
		seen[key] = struct{}{}
	}
	return request, nil
}

// cloneDeploymentDraftRequest gives normalization and server-owned authority
// allocation an isolated value. A struct copy alone still aliases the caller's
// route slice and priority/weight pointers, which would make a rolled-back
// prepare poison an exact retry of the original request.
func cloneDeploymentDraftRequest(request DeploymentDraftRequest) DeploymentDraftRequest {
	request.DeploymentRoutes = append([]DeploymentRoute(nil), request.DeploymentRoutes...)
	for index := range request.DeploymentRoutes {
		route := &request.DeploymentRoutes[index]
		if route.Priority != nil {
			priority := *route.Priority
			route.Priority = &priority
		}
		if route.Weight != nil {
			weight := *route.Weight
			route.Weight = &weight
		}
	}
	return request
}

// PrepareDeploymentDraft writes the v2 JSON candidate and its normalized
// route rows in one transaction. customerOrgID is mandatory: on-premises is a
// single-customer product, but ownership is still never inferred from global
// row counts or a default organization.
func (m *Manager) PrepareDeploymentDraft(
	ctx context.Context,
	request DeploymentDraftRequest,
	actorUserID, customerOrgID int64,
) (DeploymentRelease, error) {
	return DeploymentRelease{}, fmt.Errorf(
		"%w: legacy two-transaction deployment authoring is disabled; use AuthorSealedDeploymentRelease",
		ErrConflict,
	)
}

// SealDeploymentDraft re-verifies ownership, pinned revisions, the full JSON
// candidate and every normalized item before making the manifest immutable.
func (m *Manager) SealDeploymentDraft(
	ctx context.Context,
	ref string,
	expectedVersion, actorUserID, customerOrgID int64,
) (DeploymentRelease, error) {
	return DeploymentRelease{}, fmt.Errorf(
		"%w: legacy two-transaction deployment sealing is disabled; use AuthorSealedDeploymentRelease",
		ErrConflict,
	)
}

func (m *Manager) GetDeploymentRelease(ctx context.Context, ref string) (DeploymentRelease, error) {
	return readDeploymentRelease(ctx, m.db, strings.TrimSpace(strings.ToLower(ref)), false)
}

type deploymentManifestStore interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
}

// validateDeploymentActor binds every immutable release signature and runtime
// mutation to an active user in the customer organization that owns the
// public model. A positive user ID or a foreign-key match alone is not
// sufficient evidence of tenant authority.
func validateDeploymentActor(
	ctx context.Context,
	store deploymentManifestStore,
	actorUserID, customerOrgID int64,
) error {
	var exists bool
	if err := store.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM users
     WHERE id=$1 AND org_id=$2 AND status='active'
)`, actorUserID, customerOrgID).Scan(&exists); err != nil {
		return fmt.Errorf("%w: read deployment actor: %v", ErrUnavailable, err)
	}
	if !exists {
		return fmt.Errorf("%w: actor organization or status", ErrConflict)
	}
	return nil
}

func readDeploymentRelease(ctx context.Context, store deploymentManifestStore, ref string, lock bool) (DeploymentRelease, error) {
	query := `
SELECT id,release_ref,public_model_id,model_name,candidate_version,candidate_hash,
       baseline_hash,route_items_hash,route_items_count,route_items_sealed_at,
       candidate,stage,version,parent_stable_release_id,canary_basis_points,
       reason,created_by,validated_by,activated_by,created_at,validated_at,
       activated_at,rolled_back_at,updated_at
  FROM runtime_change_releases
 WHERE release_ref=$1 AND candidate_version='unirouter.runtime-change-candidate.v2'`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanDeploymentRelease(store.QueryRow(ctx, query, ref))
}

func readDeploymentReleaseByID(ctx context.Context, store deploymentManifestStore, id int64, lock bool) (DeploymentRelease, error) {
	query := `
SELECT id,release_ref,public_model_id,model_name,candidate_version,candidate_hash,
       baseline_hash,route_items_hash,route_items_count,route_items_sealed_at,
       candidate,stage,version,parent_stable_release_id,canary_basis_points,
       reason,created_by,validated_by,activated_by,created_at,validated_at,
       activated_at,rolled_back_at,updated_at
  FROM runtime_change_releases
 WHERE id=$1 AND candidate_version='unirouter.runtime-change-candidate.v2'`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanDeploymentRelease(store.QueryRow(ctx, query, id))
}

func scanDeploymentRelease(row interface{ Scan(...interface{}) error }) (DeploymentRelease, error) {
	var result DeploymentRelease
	var candidateJSON []byte
	err := row.Scan(
		&result.ID, &result.ReleaseRef, &result.PublicModelID, &result.ModelName,
		&result.CandidateVersion, &result.CandidateHash, &result.BaselineHash,
		&result.RouteItemsHash, &result.RouteItemsCount, &result.RouteItemsSealedAt,
		&candidateJSON, &result.Stage, &result.Version, &result.ParentStableReleaseID,
		&result.CanaryBasisPoints, &result.Reason, &result.CreatedBy, &result.ValidatedBy,
		&result.ActivatedBy, &result.CreatedAt, &result.ValidatedAt, &result.ActivatedAt,
		&result.RolledBackAt, &result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentRelease{}, ErrNotFound
	}
	if err != nil {
		return DeploymentRelease{}, fmt.Errorf("%w: read deployment release: %v", ErrUnavailable, err)
	}
	if json.Unmarshal(candidateJSON, &result.Candidate) != nil {
		return DeploymentRelease{}, fmt.Errorf("%w: not a valid deployment release", ErrUnavailable)
	}
	return result, nil
}

func canonicalStableBaseline(ctx context.Context, store deploymentManifestStore, publicModelID int64, modelName string) (*int64, string, error) {
	rows, err := store.Query(ctx, `
SELECT s.stable_release_id,COALESCE(stable.candidate_hash,'')
  FROM runtime_change_slots s
  LEFT JOIN runtime_change_releases stable ON stable.id=s.stable_release_id
 WHERE s.public_model_id=$1 OR (s.public_model_id IS NULL AND s.model_name=$2)
 ORDER BY (s.public_model_id IS NOT NULL) DESC,s.model_name
 FOR UPDATE OF s`, publicModelID, modelName)
	if err != nil {
		return nil, "", fmt.Errorf("%w: read stable deployment baseline: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	var parentID *int64
	var baselineHash string
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&parentID, &baselineHash); err != nil {
			return nil, "", fmt.Errorf("%w: scan stable deployment baseline: %v", ErrUnavailable, err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("%w: read stable deployment baseline: %v", ErrUnavailable, err)
	}
	if count > 1 {
		return nil, "", fmt.Errorf("%w: ambiguous public model runtime slot", ErrConflict)
	}
	if count == 0 || parentID == nil {
		return nil, hashJSON(DeploymentCandidate{
			ContractVersion:  DeploymentCandidateVersion,
			PublicModelID:    publicModelID,
			ModelName:        modelName,
			DeploymentRoutes: []DeploymentRoute{},
		}), nil
	}
	if parentID != nil && baselineHash == "" {
		return nil, "", fmt.Errorf("%w: corrupt stable baseline", ErrUnavailable)
	}
	return parentID, baselineHash, nil
}

func validateDeploymentRoutes(ctx context.Context, store deploymentManifestStore, orgID, publicModelID int64, routes []DeploymentRoute) error {
	for _, route := range routes {
		if _, err := inspectDeploymentRoute(ctx, store, orgID, publicModelID, route); err != nil {
			return err
		}
	}
	return nil
}

// validateDeploymentRouteInputs proves the current structural qualification
// before a server-owned M77 binding identity exists. Every later seal and
// lifecycle transition uses validateDeploymentRoutes and therefore requires
// the complete per-release-item binding.
func validateDeploymentRouteInputs(ctx context.Context, store deploymentManifestStore, orgID, publicModelID int64, routes []DeploymentRoute) error {
	for _, route := range routes {
		if _, err := inspectDeploymentRouteAuthority(ctx, store, orgID, publicModelID, route, false); err != nil {
			return err
		}
	}
	return nil
}

type deploymentRouteRuntime struct {
	DeploymentID     int64
	UpstreamModel    string
	Provider         string
	Priority         int
	Weight           int
	MaxContext       int
	DefaultMaxOutput int
	CircuitState     string
	HalfOpenUntil    *time.Time
	CredentialIDs    []int64
}

// inspectDeploymentRoute is the single admission proof shared by sealing and
// every later lifecycle transition. A credential is eligible only when its own
// latest probe matches the current deployment, connection, binding scope epoch
// and explicit protocol profile.
func inspectDeploymentRoute(
	ctx context.Context,
	store deploymentManifestStore,
	orgID, publicModelID int64,
	route DeploymentRoute,
) (deploymentRouteRuntime, error) {
	return inspectDeploymentRouteAuthority(ctx, store, orgID, publicModelID, route, true)
}

func inspectDeploymentRouteAuthority(
	ctx context.Context,
	store deploymentManifestStore,
	orgID, publicModelID int64,
	route DeploymentRoute,
	requirePublicExecutionBinding bool,
) (deploymentRouteRuntime, error) {
	var runtime deploymentRouteRuntime
	var storedOrgID, storedPublicModelID, connectionRevision, deploymentRevision int64
	var connectionState, deploymentState, upstreamModel, supportState, evidenceState string
	var operationRevision int64
	var operationProfileID, operationProfileVersion string
	var currentHTTPSOrigin string
	var qualificationQualified bool
	var qualificationState string
	var qualificationApprovalID int64
	var qualificationPublicPath, qualificationExecutor string
	var operationVerifiedAt *time.Time
	err := store.QueryRow(ctx, `
SELECT deployment.customer_org_id,deployment.public_model_id,
       connection.config_revision,deployment.config_revision,
       connection.admin_state,deployment.admin_state,deployment.upstream_model,
       operation.support_state,operation.evidence_state,operation.verified_at,operation.revision,
       operation.protocol_profile_id,operation.protocol_profile_version,
       COALESCE(model_operation_https_origin(connection.base_url),''),
       COALESCE(qualification.qualified,FALSE),COALESCE(qualification.qualification_state,''),
       COALESCE(qualification.approval_id,0),COALESCE(qualification.public_path,''),
       COALESCE(qualification.executor_contract_version,''),
       connection.provider,deployment.priority,deployment.routing_weight,
       deployment.max_context,deployment.default_max_output,
       deployment.circuit_state,deployment.half_open_until
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id AND operation.operation_id=$2
  LEFT JOIN model_operation_execution_qualifications qualification
    ON qualification.deployment_id=operation.deployment_id
   AND qualification.operation_id=operation.operation_id
 WHERE deployment.id=$1`, route.DeploymentID, route.ModelOperationID).Scan(
		&storedOrgID, &storedPublicModelID, &connectionRevision, &deploymentRevision,
		&connectionState, &deploymentState, &upstreamModel,
		&supportState, &evidenceState, &operationVerifiedAt, &operationRevision,
		&operationProfileID, &operationProfileVersion, &currentHTTPSOrigin,
		&qualificationQualified, &qualificationState, &qualificationApprovalID,
		&qualificationPublicPath, &qualificationExecutor, &runtime.Provider,
		&runtime.Priority, &runtime.Weight, &runtime.MaxContext, &runtime.DefaultMaxOutput,
		&runtime.CircuitState, &runtime.HalfOpenUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment route %d/%s", ErrInvalid, route.DeploymentID, route.ModelOperationID)
	}
	if err != nil {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: read deployment route: %v", ErrUnavailable, err)
	}
	if storedOrgID != orgID || storedPublicModelID != publicModelID {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment route ownership", ErrConflict)
	}
	if connectionRevision != route.ExpectedConnectionRevision || deploymentRevision != route.ExpectedDeploymentRevision {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment route revision", ErrConflict)
	}
	if connectionState != "enabled" || deploymentState != "enabled" {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment route is disabled", ErrConflict)
	}
	if supportState != "enabled" {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment operation is unsupported", ErrConflict)
	}
	if operationRevision != route.ExpectedOperationRevision {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment operation revision", ErrConflict)
	}
	if operationProfileID != route.ProtocolProfileID || operationProfileVersion != route.ProtocolProfileVersion {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment protocol profile revision", ErrConflict)
	}
	if modelqualification.IsQualificationRequired(route.ModelOperationID) {
		binding := modelqualification.Binding{
			OperationID: route.ModelOperationID, PublicPath: route.PublicPath,
			ProtocolProfileID:       route.ProtocolProfileID,
			ProtocolProfileVersion:  route.ProtocolProfileVersion,
			ExecutorContractVersion: route.ExecutorContractVersion,
		}
		if modelqualification.ValidateCompiledBinding(binding) != nil ||
			!qualificationQualified || qualificationState != "production_qualified" ||
			qualificationApprovalID <= 0 || qualificationPublicPath != route.PublicPath ||
			qualificationExecutor != route.ExecutorContractVersion || currentHTTPSOrigin == "" {
			return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment operation lacks current production qualification", ErrConflict)
		}
		publicBinding, publicBindingErr := modelqualification.CompiledPublicBinding(route.ModelOperationID)
		if publicBindingErr != nil || route.DeliveryContractVersion != publicBinding.DeliveryContractVersion ||
			route.ChargeContractVersion != publicBinding.ChargeContractVersion ||
			route.IdempotencyContractVersion != publicBinding.IdempotencyContractVersion {
			return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment operation lacks the compiled public execution contract", ErrConflict)
		}
		if requirePublicExecutionBinding {
			var publicExecutable bool
			err := store.QueryRow(ctx, `
SELECT public_executable
  FROM model_operation_public_execution_qualifications
 WHERE public_execution_binding_id=$1
   AND customer_org_id=$2 AND public_model_id=$3
   AND deployment_id=$4 AND operation_id=$5
   AND public_path=$6 AND protocol_profile_id=$7
   AND protocol_profile_version=$8 AND executor_contract_version=$9
   AND delivery_contract_version=$10 AND charge_contract_version=$11
   AND idempotency_contract_version=$12`,
				route.PublicExecutionBindingID, orgID, publicModelID,
				route.DeploymentID, route.ModelOperationID, route.PublicPath,
				route.ProtocolProfileID, route.ProtocolProfileVersion,
				route.ExecutorContractVersion, route.DeliveryContractVersion,
				route.ChargeContractVersion, route.IdempotencyContractVersion,
			).Scan(&publicExecutable)
			if errors.Is(err, pgx.ErrNoRows) || !publicExecutable {
				return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment route lacks a current public execution binding", ErrConflict)
			}
			if err != nil {
				return deploymentRouteRuntime{}, fmt.Errorf("%w: read public execution binding: %v", ErrUnavailable, err)
			}
		} else if route.PublicExecutionBindingID != 0 {
			return deploymentRouteRuntime{}, fmt.Errorf("%w: public execution binding identity is allocated by the server", ErrInvalid)
		}
	}
	switch evidenceState {
	case string(modeldeployment.EvidenceLiveProbed),
		string(modeldeployment.EvidenceConformanceVerified),
		string(modeldeployment.EvidenceProductionApproved):
		if operationVerifiedAt == nil {
			return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment operation evidence is unverified", ErrConflict)
		}
	default:
		return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment operation evidence is insufficient", ErrConflict)
	}
	rows, err := store.Query(ctx, `
SELECT binding.id
      FROM credential_binding_deployments link
      JOIN provider_credential_bindings binding
        ON binding.id=link.binding_id AND binding.connection_id=link.connection_id
      JOIN LATERAL (
          SELECT probe.*
            FROM model_deployment_probes probe
           WHERE probe.deployment_id=$1
             AND probe.operation_id=$2
             AND probe.credential_binding_id=binding.id
           ORDER BY probe.tested_at DESC,probe.probe_id DESC
           LIMIT 1
      ) probe ON TRUE
     WHERE link.deployment_id=$1
       AND link.connection_id=binding.connection_id
       AND binding.status='active'
       AND binding.key_enc<>''
       AND (binding.cooldown_until IS NULL OR binding.cooldown_until<=NOW())
       AND probe.success
       AND probe.upstream_model=$3
       AND probe.connection_revision=$4
       AND probe.deployment_revision=$5
       AND probe.credential_state_revision=binding.state_revision
       AND probe.probe_profile_id=$6
       AND probe.probe_profile_version=$7
       AND ($8='' OR (
           probe.operation_revision IS NOT NULL
           AND probe.public_path=$8
           AND probe.executor_contract_version=$9
           AND probe.https_origin=$10
           AND probe.source_kind='admin.endpointprotocol_live_probe.v1'
       ))
       AND probe.tested_at>=link.created_at
	 ORDER BY binding.id`, route.DeploymentID, route.ModelOperationID, upstreamModel, connectionRevision,
		deploymentRevision, operationProfileID, operationProfileVersion,
		route.PublicPath, route.ExecutorContractVersion, currentHTTPSOrigin)
	if err != nil {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: read deployment credential probe evidence: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	for rows.Next() {
		var bindingID int64
		if err := rows.Scan(&bindingID); err != nil {
			return deploymentRouteRuntime{}, fmt.Errorf("%w: scan deployment credential probe evidence: %v", ErrUnavailable, err)
		}
		runtime.CredentialIDs = append(runtime.CredentialIDs, bindingID)
	}
	if err := rows.Err(); err != nil {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: iterate deployment credential probe evidence: %v", ErrUnavailable, err)
	}
	if len(runtime.CredentialIDs) == 0 {
		return deploymentRouteRuntime{}, fmt.Errorf("%w: deployment route has no active credential with its own current successful probe", ErrConflict)
	}
	runtime.DeploymentID = route.DeploymentID
	runtime.UpstreamModel = upstreamModel
	return runtime, nil
}

func loadDeploymentRouteItems(ctx context.Context, store deploymentManifestStore, releaseID int64) ([]DeploymentRoute, []string, error) {
	rows, err := store.Query(ctx, `
SELECT deployment_id,operation_id,expected_connection_revision,
       expected_deployment_revision,expected_operation_revision,
       protocol_profile_id,protocol_profile_version,
       COALESCE(public_path,''),COALESCE(executor_contract_version,''),
       COALESCE(public_execution_binding_id,0),COALESCE(delivery_contract_version,''),
       COALESCE(charge_contract_version,''),COALESCE(idempotency_contract_version,''),
       admission,priority,weight,source_candidate_version
  FROM runtime_route_revision_items
 WHERE release_id=$1
 ORDER BY ordinal`, releaseID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: read deployment route items: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	routes := make([]DeploymentRoute, 0)
	versions := make([]string, 0)
	for rows.Next() {
		var route DeploymentRoute
		var version string
		if err := rows.Scan(&route.DeploymentID, &route.ModelOperationID,
			&route.ExpectedConnectionRevision, &route.ExpectedDeploymentRevision,
			&route.ExpectedOperationRevision,
			&route.ProtocolProfileID, &route.ProtocolProfileVersion,
			&route.PublicPath, &route.ExecutorContractVersion,
			&route.PublicExecutionBindingID, &route.DeliveryContractVersion,
			&route.ChargeContractVersion, &route.IdempotencyContractVersion,
			&route.Admission, &route.Priority, &route.Weight, &version); err != nil {
			return nil, nil, fmt.Errorf("%w: scan deployment route item: %v", ErrUnavailable, err)
		}
		routes = append(routes, route)
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("%w: read deployment route items: %v", ErrUnavailable, err)
	}
	return routes, versions, nil
}

func nullableDeploymentExecutionBinding(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func nullableDeploymentExecutionBindingID(value int64) interface{} {
	if value == 0 {
		return nil
	}
	return value
}

func insertPublicExecutionBinding(
	ctx context.Context,
	tx pgx.Tx,
	releaseID int64,
	ordinal int,
	customerOrgID, publicModelID, actorUserID, actorOrgID int64,
	route DeploymentRoute,
) error {
	tag, err := tx.Exec(ctx, `
INSERT INTO model_operation_public_execution_bindings (
    public_execution_binding_id,customer_org_id,public_model_id,
    release_id,ordinal,deployment_id,connection_id,operation_id,
    production_approval_id,https_origin,approved_connection_revision,
    approved_deployment_revision,approved_operation_revision,
    public_path,protocol_profile_id,protocol_profile_version,
    executor_contract_version,delivery_contract_version,
    charge_contract_version,idempotency_contract_version,
    created_by,created_by_org_id,source_kind,source_ref)
SELECT $1::BIGINT,$2::BIGINT,$3::BIGINT,$4::BIGINT,$5::INTEGER,
       qualification.deployment_id,qualification.connection_id,
       qualification.operation_id,qualification.approval_id,qualification.https_origin,
       qualification.approved_connection_revision,qualification.approved_deployment_revision,
       qualification.approved_operation_revision,qualification.public_path,
       qualification.protocol_profile_id,qualification.protocol_profile_version,
       qualification.executor_contract_version,contract.delivery_contract_version,
       contract.charge_contract_version,contract.idempotency_contract_version,
       actor.id,actor.org_id,'runtime.public_execution_binding.v1',
       'release:' || $4::BIGINT::TEXT || '/item:' || $5::INTEGER::TEXT
  FROM model_operation_execution_qualifications qualification
  JOIN model_deployments deployment ON deployment.id=qualification.deployment_id
  JOIN users actor ON actor.id=$6 AND actor.org_id=$7 AND actor.status='active'
  JOIN model_operation_current_public_execution_contracts contract
    ON contract.operation_id=qualification.operation_id
   AND contract.public_path=qualification.public_path
   AND contract.protocol_profile_id=qualification.protocol_profile_id
   AND contract.protocol_profile_version=qualification.protocol_profile_version
   AND contract.executor_contract_version=qualification.executor_contract_version
	JOIN orgs actor_org ON actor_org.id=actor.org_id AND actor_org.status='active'
 WHERE qualification.deployment_id=$8 AND qualification.operation_id=$9
   AND deployment.customer_org_id=$2 AND deployment.public_model_id=$3
   AND qualification.qualified
   AND qualification.current_connection_revision=$10
   AND qualification.current_deployment_revision=$11
   AND qualification.current_operation_revision=$12
   AND qualification.public_path=$13
   AND qualification.protocol_profile_id=$14
   AND qualification.protocol_profile_version=$15
   AND qualification.executor_contract_version=$16
   AND contract.delivery_contract_version=$17
   AND contract.charge_contract_version=$18
   AND contract.idempotency_contract_version=$19`,
		route.PublicExecutionBindingID, customerOrgID, publicModelID, releaseID, ordinal,
		actorUserID, actorOrgID, route.DeploymentID, route.ModelOperationID,
		route.ExpectedConnectionRevision, route.ExpectedDeploymentRevision,
		route.ExpectedOperationRevision, route.PublicPath, route.ProtocolProfileID,
		route.ProtocolProfileVersion, route.ExecutorContractVersion,
		route.DeliveryContractVersion, route.ChargeContractVersion,
		route.IdempotencyContractVersion,
	)
	if err != nil {
		return fmt.Errorf("%w: write public execution binding: %v", ErrUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: public execution qualification changed", ErrConflict)
	}
	return nil
}
