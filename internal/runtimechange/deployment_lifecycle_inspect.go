package runtimechange

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const DeploymentReleaseAuthoringOptionsContractVersion = "unirouter.model-deployment-release-authoring-options.v1"

// DeploymentReleaseSummary is the bounded collection/read projection. The
// immutable candidate remains available from the exact release endpoint; the
// collection carries enough identity to reopen lifecycle work after refresh.
type DeploymentReleaseSummary struct {
	ID                      int64      `json:"id"`
	ReleaseRef              string     `json:"release_ref"`
	PublicModelID           int64      `json:"public_model_id"`
	ModelName               string     `json:"model_name"`
	ExecutionProfileID      string     `json:"execution_profile_id,omitempty"`
	ExecutionProfileVersion string     `json:"execution_profile_version,omitempty"`
	PublicContextLimit      int        `json:"public_context_limit,omitempty"`
	PublicOutputLimit       int        `json:"public_output_limit,omitempty"`
	CandidateVersion        string     `json:"candidate_version"`
	CandidateHash           string     `json:"candidate_hash"`
	RouteItemsHash          string     `json:"route_items_hash"`
	RouteItemsCount         int        `json:"route_items_count"`
	RouteItemsSealedAt      *time.Time `json:"route_items_sealed_at,omitempty"`
	Stage                   string     `json:"stage"`
	Version                 int64      `json:"version"`
	ParentStableReleaseID   *int64     `json:"parent_stable_release_id,omitempty"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type DeploymentReleasePublicModelSummary struct {
	ID          int64  `json:"id"`
	ModelName   string `json:"model_name"`
	DisplayName string `json:"display_name"`
	Lifecycle   string `json:"lifecycle"`
}

type DeploymentReleaseSelectionOption struct {
	DeploymentID            int64   `json:"deployment_id"`
	OperationID             string  `json:"operation_id"`
	UpstreamModel           string  `json:"upstream_model,omitempty"`
	ConnectionName          string  `json:"connection_name,omitempty"`
	ProfileDefinitionDigest string  `json:"profile_definition_digest,omitempty"`
	MaxContext              int     `json:"max_context,omitempty"`
	DefaultMaxOutput        int     `json:"default_max_output,omitempty"`
	CapabilityApprovalIDs   []int64 `json:"capability_approval_ids,omitempty"`
}

// DeploymentReleaseAuthoringOption is server-derived optimistic truth for one
// public model. A client may echo Baseline in POST, but never constructs it or
// supplies route revisions, hashes, parent IDs or a release reference.
type DeploymentReleaseAuthoringOption struct {
	PublicModel           DeploymentReleasePublicModelSummary `json:"public_model"`
	StructurallyAvailable bool                                `json:"structurally_available"`
	ReasonCode            string                              `json:"reason_code"`
	Baseline              *DeploymentReleaseBaseline          `json:"baseline,omitempty"`
	Slot                  DeploymentLifecycleSlotState        `json:"slot"`
	StableRelease         *DeploymentReleaseSummary           `json:"stable_release,omitempty"`
	SelectionOptions      []DeploymentReleaseSelectionOption  `json:"selection_options"`
}

type DeploymentReleaseAuthoringOptions struct {
	ContractVersion string                             `json:"contract_version"`
	GeneratedAt     time.Time                          `json:"generated_at"`
	Models          []DeploymentReleaseAuthoringOption `json:"models"`
	Releases        []DeploymentReleaseSummary         `json:"releases"`
}

// DeploymentLifecycleState is a transactionally consistent administrative
// projection of one canonical v2 release and its tenant-scoped runtime slot.
// Action availability remains advisory; every write revalidates under locks.
type DeploymentLifecycleState struct {
	Release      DeploymentRelease            `json:"release"`
	Slot         DeploymentLifecycleSlotState `json:"slot"`
	StableParent *DeploymentReleaseSummary    `json:"stable_parent,omitempty"`
}

// InspectDeploymentLifecycle reads one canonical release and its live slot in
// a repeatable-read, read-only transaction. The explicit customer ownership
// check prevents a globally unique release_ref from becoming a cross-tenant
// read capability.
func (m *Manager) InspectDeploymentLifecycle(
	ctx context.Context,
	releaseRef string,
	customerOrgID int64,
) (DeploymentLifecycleState, error) {
	releaseRef = strings.TrimSpace(strings.ToLower(releaseRef))
	if m == nil || customerOrgID <= 0 || !releaseRefPattern.MatchString(releaseRef) {
		return DeploymentLifecycleState{}, fmt.Errorf("%w: deployment lifecycle inspection identity", ErrInvalid)
	}
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return DeploymentLifecycleState{}, fmt.Errorf("%w: begin deployment lifecycle inspection: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return DeploymentLifecycleState{}, fmt.Errorf("%w: configure deployment lifecycle inspection: %v", ErrUnavailable, err)
	}
	release, err := readDeploymentRelease(ctx, tx, releaseRef, false)
	if err != nil {
		return DeploymentLifecycleState{}, err
	}
	var owned bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
      FROM model_registry
     WHERE id=$1 AND org_id=$2 AND model_name=$3
)`, release.PublicModelID, customerOrgID, release.ModelName).Scan(&owned); err != nil {
		return DeploymentLifecycleState{}, fmt.Errorf("%w: read deployment lifecycle owner: %v", ErrUnavailable, err)
	}
	if !owned {
		return DeploymentLifecycleState{}, ErrNotFound
	}
	slot, err := readDeploymentLifecycleSlotState(ctx, tx, release.PublicModelID, release.ModelName)
	if err != nil {
		return DeploymentLifecycleState{}, err
	}
	var stableParent *DeploymentReleaseSummary
	if release.ParentStableReleaseID != nil {
		parent, readErr := readDeploymentReleaseSummaryByID(ctx, tx, *release.ParentStableReleaseID)
		if readErr != nil {
			return DeploymentLifecycleState{}, readErr
		}
		if parent.PublicModelID != release.PublicModelID || parent.ModelName != release.ModelName {
			return DeploymentLifecycleState{}, fmt.Errorf("%w: deployment lifecycle parent identity", ErrUnavailable)
		}
		stableParent = &parent
	}
	if slot.Exists && slot.AssignedPublicModelID == nil &&
		(stableParent == nil || stableParent.CandidateVersion != CandidateVersion ||
			slot.StableReleaseID == nil || *slot.StableReleaseID != stableParent.ID ||
			slot.CanaryReleaseID != nil || slot.CanaryBasisPoints != 0) {
		// A raw legacy slot has no tenant-bearing public_model_id. Until its
		// exact stable parent is proven to be this owned release's sealed v1
		// parent, expose only the logical collision fact and no raw IDs.
		slot = DeploymentLifecycleSlotState{
			Exists: true, PublicModelID: release.PublicModelID, ModelName: release.ModelName,
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return DeploymentLifecycleState{}, fmt.Errorf("%w: commit deployment lifecycle inspection: %v", ErrUnavailable, err)
	}
	return DeploymentLifecycleState{Release: release, Slot: slot, StableParent: stableParent}, nil
}

// ListDeploymentReleaseAuthoringOptions returns tenant-scoped server-owned
// baselines and the actionable canonical v2 release index in one repeatable-
// read snapshot. Ambiguous or transitional slots remain visible but cannot be
// authored against.
func (m *Manager) ListDeploymentReleaseAuthoringOptions(
	ctx context.Context,
	customerOrgID int64,
) (DeploymentReleaseAuthoringOptions, error) {
	if m == nil || customerOrgID <= 0 {
		return DeploymentReleaseAuthoringOptions{}, fmt.Errorf("%w: deployment release collection identity", ErrInvalid)
	}
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return DeploymentReleaseAuthoringOptions{}, fmt.Errorf("%w: begin deployment release collection: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return DeploymentReleaseAuthoringOptions{}, fmt.Errorf("%w: configure deployment release collection: %v", ErrUnavailable, err)
	}

	var generatedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&generatedAt); err != nil {
		return DeploymentReleaseAuthoringOptions{}, fmt.Errorf("%w: read deployment release collection clock: %v", ErrUnavailable, err)
	}
	models, err := readDeploymentReleasePublicModels(ctx, tx, customerOrgID)
	if err != nil {
		return DeploymentReleaseAuthoringOptions{}, err
	}
	selectionOptions, err := readDeploymentReleaseSelectionOptions(ctx, tx, customerOrgID)
	if err != nil {
		return DeploymentReleaseAuthoringOptions{}, err
	}
	options := make([]DeploymentReleaseAuthoringOption, 0, len(models))
	for _, model := range models {
		option, optionErr := readDeploymentReleaseAuthoringOption(
			ctx, tx, model, selectionOptions[model.ID],
		)
		if optionErr != nil {
			return DeploymentReleaseAuthoringOptions{}, optionErr
		}
		options = append(options, option)
	}
	releases, err := readActionableDeploymentReleaseSummaries(ctx, tx, customerOrgID)
	if err != nil {
		return DeploymentReleaseAuthoringOptions{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeploymentReleaseAuthoringOptions{}, fmt.Errorf("%w: commit deployment release collection: %v", ErrUnavailable, err)
	}
	return DeploymentReleaseAuthoringOptions{
		ContractVersion: DeploymentReleaseAuthoringOptionsContractVersion,
		GeneratedAt:     generatedAt.UTC(), Models: options, Releases: releases,
	}, nil
}

func readDeploymentReleasePublicModels(
	ctx context.Context,
	tx pgx.Tx,
	customerOrgID int64,
) ([]DeploymentReleasePublicModelSummary, error) {
	rows, err := tx.Query(ctx, `
SELECT id,model_name,display_name,lifecycle
  FROM model_registry
 WHERE org_id=$1
 ORDER BY model_name,id`, customerOrgID)
	if err != nil {
		return nil, fmt.Errorf("%w: list deployment release public models: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	models := make([]DeploymentReleasePublicModelSummary, 0)
	for rows.Next() {
		var model DeploymentReleasePublicModelSummary
		if err := rows.Scan(&model.ID, &model.ModelName, &model.DisplayName, &model.Lifecycle); err != nil {
			return nil, fmt.Errorf("%w: scan deployment release public model: %v", ErrUnavailable, err)
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate deployment release public models: %v", ErrUnavailable, err)
	}
	return models, nil
}

func readDeploymentReleaseSelectionOptions(
	ctx context.Context,
	tx pgx.Tx,
	customerOrgID int64,
) (map[int64][]DeploymentReleaseSelectionOption, error) {
	rows, err := tx.Query(ctx, `
SELECT deployment.public_model_id,deployment.id,operation.operation_id,
       deployment.upstream_model,connection.name,
       COALESCE(to_jsonb(operation)->>'profile_definition_digest',''),
       COALESCE(deployment.max_context,0),COALESCE(deployment.default_max_output,0),
       CASE WHEN COALESCE(to_jsonb(operation)->>'profile_definition_digest','')='' THEN '[]' ELSE
       COALESCE((SELECT jsonb_agg(q.approval_id ORDER BY q.approval_id)::text FROM model_capability_approval_qualifications q WHERE q.deployment_id=deployment.id AND q.operation_id=operation.operation_id AND q.qualified),'[]') END
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation ON operation.deployment_id=deployment.id
  LEFT JOIN model_operation_execution_qualifications qualification
    ON qualification.deployment_id=operation.deployment_id
   AND qualification.operation_id=operation.operation_id
 WHERE deployment.customer_org_id=$1
   AND connection.admin_state='enabled'
   AND deployment.admin_state='enabled'
   AND operation.support_state='enabled'
   AND operation.operation_id IN (
       'openai.chat_completions.v1','openai.embeddings.v1',
       'openai.responses.v1','anthropic.messages.v1')
   AND (operation.operation_id IN ('openai.chat_completions.v1','openai.embeddings.v1')
        OR COALESCE(qualification.qualified,FALSE))
 ORDER BY deployment.public_model_id,deployment.id,operation.operation_id`, customerOrgID)
	if err != nil {
		return nil, fmt.Errorf("%w: list deployment release selection options: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	result := make(map[int64][]DeploymentReleaseSelectionOption)
	for rows.Next() {
		var publicModelID int64
		var option DeploymentReleaseSelectionOption
		var approvals string
		if err := rows.Scan(
			&publicModelID, &option.DeploymentID, &option.OperationID,
			&option.UpstreamModel, &option.ConnectionName, &option.ProfileDefinitionDigest, &option.MaxContext, &option.DefaultMaxOutput, &approvals,
		); err != nil {
			return nil, fmt.Errorf("%w: scan deployment release selection option: %v", ErrUnavailable, err)
		}
		if err := json.Unmarshal([]byte(approvals), &option.CapabilityApprovalIDs); err != nil {
			return nil, err
		}
		result[publicModelID] = append(result[publicModelID], option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate deployment release selection options: %v", ErrUnavailable, err)
	}
	return result, nil
}

func readDeploymentReleaseAuthoringOption(
	ctx context.Context,
	tx pgx.Tx,
	model DeploymentReleasePublicModelSummary,
	selectionOptions []DeploymentReleaseSelectionOption,
) (DeploymentReleaseAuthoringOption, error) {
	option := DeploymentReleaseAuthoringOption{
		PublicModel: model, ReasonCode: "ready_for_authoring",
		Slot:             DeploymentLifecycleSlotState{PublicModelID: model.ID, ModelName: model.ModelName},
		SelectionOptions: append([]DeploymentReleaseSelectionOption(nil), selectionOptions...),
	}
	rows, err := tx.Query(ctx, `
SELECT slot.public_model_id,slot.model_name,slot.stable_release_id,
       slot.canary_release_id,slot.canary_basis_points,slot.slot_version
  FROM runtime_change_slots slot
 WHERE slot.model_name=$1 OR slot.public_model_id=$2
 ORDER BY slot.model_name`, model.ModelName, model.ID)
	if err != nil {
		return DeploymentReleaseAuthoringOption{}, fmt.Errorf("%w: read deployment release authoring slot: %v", ErrUnavailable, err)
	}
	type rawSlot struct {
		publicModelID, stableReleaseID, canaryReleaseID sql.NullInt64
		modelName                                       string
		canaryBasisPoints                               int
		version                                         int64
	}
	values := make([]rawSlot, 0, 2)
	for rows.Next() {
		var value rawSlot
		if err := rows.Scan(
			&value.publicModelID, &value.modelName, &value.stableReleaseID,
			&value.canaryReleaseID, &value.canaryBasisPoints, &value.version,
		); err != nil {
			rows.Close()
			return DeploymentReleaseAuthoringOption{}, fmt.Errorf("%w: scan deployment release authoring slot: %v", ErrUnavailable, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return DeploymentReleaseAuthoringOption{}, fmt.Errorf("%w: iterate deployment release authoring slot: %v", ErrUnavailable, err)
	}
	rows.Close()

	if len(values) == 0 {
		option.Baseline = &DeploymentReleaseBaseline{Kind: DeploymentReleaseBaselineInitial}
		option.StructurallyAvailable = true
	} else if len(values) != 1 {
		option.ReasonCode = "runtime_slot_identity_ambiguous"
	} else {
		value := values[0]
		option.Slot.Exists = true
		switch {
		case value.modelName != model.ModelName:
			option.ReasonCode = "runtime_slot_identity_mismatch"
		case value.publicModelID.Valid && value.publicModelID.Int64 != model.ID:
			option.ReasonCode = "runtime_slot_identity_mismatch"
		case value.canaryReleaseID.Valid || value.canaryBasisPoints != 0:
			// A transitional slot has no authoring baseline. Do not expose raw
			// IDs before both stable and canary release ownership are proven.
			if value.publicModelID.Valid {
				option.ReasonCode = "runtime_canary_transition_in_progress"
				break
			}
			if !value.stableReleaseID.Valid {
				option.ReasonCode = "runtime_stable_release_missing"
				break
			}
			stablePublicModelID, stableModelName, exists, ownerErr := readDeploymentReleaseOwnerIdentity(
				ctx, tx, value.stableReleaseID.Int64,
			)
			if ownerErr != nil {
				return DeploymentReleaseAuthoringOption{}, ownerErr
			}
			switch {
			case !exists:
				option.ReasonCode = "runtime_stable_release_missing"
			case stablePublicModelID == nil:
				option.ReasonCode = "runtime_stable_release_not_authorable"
			case *stablePublicModelID != model.ID || stableModelName != model.ModelName:
				option.ReasonCode = "runtime_slot_identity_mismatch"
			default:
				option.ReasonCode = "runtime_canary_transition_in_progress"
			}
		case !value.stableReleaseID.Valid || value.version <= 0:
			option.ReasonCode = "runtime_stable_release_missing"
		default:
			stablePublicModelID, stableModelName, exists, ownerErr := readDeploymentReleaseOwnerIdentity(
				ctx, tx, value.stableReleaseID.Int64,
			)
			if ownerErr != nil {
				return DeploymentReleaseAuthoringOption{}, ownerErr
			}
			if !exists {
				option.ReasonCode = "runtime_stable_release_missing"
				break
			}
			if stablePublicModelID == nil {
				// A legal pre-seal v1 release has no tenant-bearing public model
				// identity. Keep this model unavailable without failing unrelated
				// models in the same collection or reflecting raw release facts.
				option.ReasonCode = "runtime_stable_release_not_authorable"
				break
			}
			if *stablePublicModelID != model.ID || stableModelName != model.ModelName {
				option.ReasonCode = "runtime_slot_identity_mismatch"
				break
			}
			stable, readErr := readDeploymentReleaseSummaryByID(ctx, tx, value.stableReleaseID.Int64)
			if readErr != nil {
				if errors.Is(readErr, ErrNotFound) {
					option.ReasonCode = "runtime_stable_release_missing"
					break
				}
				return DeploymentReleaseAuthoringOption{}, readErr
			}
			if stable.PublicModelID != model.ID || stable.ModelName != model.ModelName {
				// model_name is global slot identity but tenant model identity is
				// not global. Never reflect a foreign stable release or raw slot.
				option.ReasonCode = "runtime_slot_identity_mismatch"
				break
			}
			option.StableRelease = &stable
			option.Slot.Version = value.version
			option.Slot.CanaryBasisPoints = value.canaryBasisPoints
			stableID := value.stableReleaseID.Int64
			option.Slot.StableReleaseID = &stableID
			if value.publicModelID.Valid {
				assigned := value.publicModelID.Int64
				option.Slot.AssignedPublicModelID = &assigned
			}
			if stable.Stage != "active" || stable.RouteItemsSealedAt == nil ||
				stable.RouteItemsCount <= 0 || stable.CandidateHash == "" || stable.RouteItemsHash == "" {
				option.ReasonCode = "runtime_stable_release_not_authorable"
				break
			}
			baseline := DeploymentReleaseBaseline{
				ExpectedSlotVersion:          value.version,
				StableReleaseRef:             stable.ReleaseRef,
				ExpectedStableReleaseVersion: stable.Version,
			}
			switch {
			case value.publicModelID.Valid && value.publicModelID.Int64 == model.ID &&
				stable.CandidateVersion == DeploymentCandidateVersion:
				baseline.Kind = DeploymentReleaseBaselineStable
				option.Baseline, option.StructurallyAvailable = &baseline, true
			case !value.publicModelID.Valid && stable.CandidateVersion == CandidateVersion:
				baseline.Kind = DeploymentReleaseBaselineLegacyCutover
				option.Baseline, option.StructurallyAvailable = &baseline, true
			default:
				option.ReasonCode = "runtime_slot_identity_mismatch"
			}
		}
	}
	if model.Lifecycle == "retired" {
		option.StructurallyAvailable = false
		option.ReasonCode = "public_model_retired"
	}
	if option.StructurallyAvailable && len(option.SelectionOptions) == 0 {
		option.StructurallyAvailable = false
		option.ReasonCode = "no_eligible_deployment_operations"
	}
	if option.SelectionOptions == nil {
		option.SelectionOptions = []DeploymentReleaseSelectionOption{}
	}
	return option, nil
}

func readActionableDeploymentReleaseSummaries(
	ctx context.Context,
	tx pgx.Tx,
	customerOrgID int64,
) ([]DeploymentReleaseSummary, error) {
	rows, err := tx.Query(ctx, `
SELECT release.id,release.release_ref,release.public_model_id,release.model_name,
	   release.execution_profile_id,release.execution_profile_version,
	   release.public_context_limit,release.public_output_limit,
       release.candidate_version,release.candidate_hash,release.route_items_hash,
       release.route_items_count,release.route_items_sealed_at,release.stage,
       release.version,release.parent_stable_release_id,release.updated_at
  FROM runtime_change_releases release
  JOIN model_registry model ON model.id=release.public_model_id
 WHERE model.org_id=$1
   AND release.candidate_version=$2
   AND release.stage IN ('validated','canary','active')
 ORDER BY release.updated_at DESC,release.id DESC`, customerOrgID, DeploymentCandidateVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: list canonical deployment releases: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	releases := make([]DeploymentReleaseSummary, 0)
	for rows.Next() {
		release, scanErr := scanDeploymentReleaseSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		releases = append(releases, release)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate canonical deployment releases: %v", ErrUnavailable, err)
	}
	return releases, nil
}

func readDeploymentReleaseSummaryByID(
	ctx context.Context,
	store deploymentManifestStore,
	releaseID int64,
) (DeploymentReleaseSummary, error) {
	return scanDeploymentReleaseSummary(store.QueryRow(ctx, `
SELECT id,release_ref,public_model_id,model_name,
	   execution_profile_id,execution_profile_version,public_context_limit,public_output_limit,
	   candidate_version,candidate_hash,
       route_items_hash,route_items_count,route_items_sealed_at,stage,version,
       parent_stable_release_id,updated_at
  FROM runtime_change_releases
 WHERE id=$1`, releaseID))
}

func readDeploymentReleaseOwnerIdentity(
	ctx context.Context,
	store deploymentManifestStore,
	releaseID int64,
) (*int64, string, bool, error) {
	var publicModelID sql.NullInt64
	var modelName string
	err := store.QueryRow(ctx, `
SELECT public_model_id,model_name
  FROM runtime_change_releases
 WHERE id=$1`, releaseID).Scan(&publicModelID, &modelName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", false, nil
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("%w: read deployment release owner identity: %v", ErrUnavailable, err)
	}
	if !publicModelID.Valid {
		return nil, modelName, true, nil
	}
	value := publicModelID.Int64
	return &value, modelName, true, nil
}

func scanDeploymentReleaseSummary(row interface{ Scan(...interface{}) error }) (DeploymentReleaseSummary, error) {
	var result DeploymentReleaseSummary
	var publicModelID, publicContextLimit, publicOutputLimit, routeItemsCount sql.NullInt64
	var executionProfileID, executionProfileVersion, routeItemsHash sql.NullString
	err := row.Scan(
		&result.ID, &result.ReleaseRef, &publicModelID, &result.ModelName,
		&executionProfileID, &executionProfileVersion,
		&publicContextLimit, &publicOutputLimit,
		&result.CandidateVersion, &result.CandidateHash, &routeItemsHash,
		&routeItemsCount, &result.RouteItemsSealedAt, &result.Stage, &result.Version,
		&result.ParentStableReleaseID, &result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeploymentReleaseSummary{}, ErrNotFound
	}
	if err != nil {
		return DeploymentReleaseSummary{}, fmt.Errorf("%w: read deployment release summary: %v", ErrUnavailable, err)
	}
	if !publicModelID.Valid {
		return DeploymentReleaseSummary{}, fmt.Errorf("%w: deployment release summary public model identity", ErrUnavailable)
	}
	result.PublicModelID = publicModelID.Int64
	if executionProfileID.Valid {
		result.ExecutionProfileID = executionProfileID.String
	}
	if executionProfileVersion.Valid {
		result.ExecutionProfileVersion = executionProfileVersion.String
	}
	if publicContextLimit.Valid {
		result.PublicContextLimit = int(publicContextLimit.Int64)
	}
	if publicOutputLimit.Valid {
		result.PublicOutputLimit = int(publicOutputLimit.Int64)
	}
	if routeItemsHash.Valid {
		result.RouteItemsHash = routeItemsHash.String
	}
	if routeItemsCount.Valid {
		result.RouteItemsCount = int(routeItemsCount.Int64)
	}
	if result.RouteItemsSealedAt != nil {
		value := result.RouteItemsSealedAt.UTC()
		result.RouteItemsSealedAt = &value
	}
	result.UpdatedAt = result.UpdatedAt.UTC()
	return result, nil
}
