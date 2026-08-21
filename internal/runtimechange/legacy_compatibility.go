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
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

type LegacyCompatibilityManifest struct {
	ReleaseID       int64     `json:"release_id"`
	ReleaseRef      string    `json:"release_ref"`
	PublicModelID   int64     `json:"public_model_id"`
	ModelName       string    `json:"model_name"`
	RouteItemsHash  string    `json:"route_items_hash"`
	RouteItemsCount int       `json:"route_items_count"`
	SealedAt        time.Time `json:"sealed_at"`
	SlotVersion     int64     `json:"slot_version"`
}

// PrepareLegacyCompatibilitySeal is the narrow projection-maintenance entry
// point used by the explicit model-route-authority CLI. It deliberately reuses
// the Manager implementation without requiring a runtime snapshot builder,
// because compatibility sealing neither simulates nor publishes a release.
func PrepareLegacyCompatibilitySeal(
	ctx context.Context,
	database db.DBQuerier,
	ref string,
	expectedVersion, customerOrgID, publicModelID int64,
) (LegacyCompatibilityManifest, error) {
	if database == nil {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy compatibility database", ErrInvalid)
	}
	manager := &Manager{db: database, now: time.Now}
	return manager.PrepareLegacyCompatibilitySeal(ctx, ref, expectedVersion, customerOrgID, publicModelID)
}

// PrepareLegacyCompatibilitySeal converts the already-reconciled normalized
// rows of a current v1 stable or canary release into immutable compatibility
// evidence.
// It never changes the v1 candidate JSON and never assigns public_model_id to
// the legacy slot. SQL migration code cannot call this path because ownership
// must be provided explicitly by the customer-aware application service.
func (m *Manager) PrepareLegacyCompatibilitySeal(
	ctx context.Context,
	ref string,
	expectedVersion, customerOrgID, publicModelID int64,
) (LegacyCompatibilityManifest, error) {
	ref = strings.TrimSpace(strings.ToLower(ref))
	if !releaseRefPattern.MatchString(ref) || expectedVersion <= 0 || customerOrgID <= 0 || publicModelID <= 0 {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy compatibility identity", ErrInvalid)
	}
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: begin legacy compatibility seal: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)

	var releaseID, version int64
	var modelName, candidateVersion, candidateHash, stage string
	var candidateJSON []byte
	var storedPublicModelID *int64
	var storedRouteHash *string
	var storedRouteCount *int
	var storedSealedAt *time.Time
	if err := tx.QueryRow(ctx, `
SELECT id,model_name,candidate_version,candidate_hash,candidate,stage,version,
       public_model_id,route_items_hash,route_items_count,route_items_sealed_at
  FROM runtime_change_releases
 WHERE release_ref=$1
 FOR UPDATE`, ref).Scan(
		&releaseID, &modelName, &candidateVersion, &candidateHash, &candidateJSON,
		&stage, &version, &storedPublicModelID, &storedRouteHash, &storedRouteCount,
		&storedSealedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LegacyCompatibilityManifest{}, ErrNotFound
		}
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: read legacy compatibility release: %v", ErrUnavailable, err)
	}
	if candidateVersion != CandidateVersion || (stage != "active" && stage != "canary") || version != expectedVersion {
		return LegacyCompatibilityManifest{}, ErrConflict
	}
	if storedPublicModelID != nil || storedRouteHash != nil || storedRouteCount != nil || storedSealedAt != nil {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy compatibility release already prepared", ErrConflict)
	}

	var ownerOrgID int64
	var publicModelName string
	if err := tx.QueryRow(ctx, `SELECT org_id,model_name FROM model_registry WHERE id=$1`, publicModelID).Scan(&ownerOrgID, &publicModelName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LegacyCompatibilityManifest{}, fmt.Errorf("%w: public model", ErrInvalid)
		}
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: read compatibility public model: %v", ErrUnavailable, err)
	}
	if ownerOrgID != customerOrgID || publicModelName != modelName {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy compatibility public model ownership", ErrConflict)
	}

	var stableReleaseID *int64
	var canaryReleaseID, slotPublicModelID *int64
	var slotVersion int64
	if err := tx.QueryRow(ctx, `
SELECT stable_release_id,canary_release_id,public_model_id,slot_version
  FROM runtime_change_slots
 WHERE model_name=$1
 FOR UPDATE`, modelName).Scan(&stableReleaseID, &canaryReleaseID, &slotPublicModelID, &slotVersion); err != nil {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: lock legacy compatibility slot: %v", ErrUnavailable, err)
	}
	isStable := stage == "active" && stableReleaseID != nil && *stableReleaseID == releaseID
	isCanary := stage == "canary" && canaryReleaseID != nil && *canaryReleaseID == releaseID
	if isStable == isCanary || slotPublicModelID != nil {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: release is not a current v1 stable or canary", ErrConflict)
	}

	var candidate Candidate
	if json.Unmarshal(candidateJSON, &candidate) != nil || candidate.ContractVersion != CandidateVersion || candidate.ModelName != modelName {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: corrupt v1 compatibility candidate", ErrUnavailable)
	}
	if hashJSON(candidate) != candidateHash {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: v1 candidate hash mismatch", ErrConflict)
	}
	normalized, err := routingpolicy.ValidateOverrides(candidate.Overrides)
	if err != nil || !reflect.DeepEqual(normalized, candidate.Overrides) {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: v1 candidate overrides are not canonical", ErrConflict)
	}

	for index, override := range normalized {
		var deploymentID, connectionRevision, deploymentRevision, operationRevision int64
		var operationProfileID, operationProfileVersion string
		if err := tx.QueryRow(ctx, `
SELECT deployment.id,connection.config_revision,deployment.config_revision,operation.revision,
       operation.protocol_profile_id,operation.protocol_profile_version
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id AND operation.operation_id=$5
 WHERE deployment.customer_org_id=$1 AND deployment.public_model_id=$2
   AND deployment.legacy_channel_id=$3 AND deployment.legacy_public_model=$4
   AND deployment.admin_state<>'retired'`,
			customerOrgID, publicModelID, override.ChannelID, modelName,
			modeldeployment.LegacyOperationID).Scan(
			&deploymentID, &connectionRevision, &deploymentRevision, &operationRevision,
			&operationProfileID, &operationProfileVersion,
		); err != nil {
			return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy route item %d has no canonical provenance: %v", ErrConflict, index+1, err)
		}
		if connectionRevision != override.ExpectedRoutingEpoch || deploymentRevision != override.ExpectedRoutingEpoch {
			return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy route item %d revision", ErrConflict, index+1)
		}

		var storedDeploymentID, storedConnectionRevision, storedDeploymentRevision int64
		var storedOperation, admission, sourceVersion string
		var storedOperationRevision *int64
		var storedProfileID, storedProfileVersion *string
		var priority, weight *int
		if err := tx.QueryRow(ctx, `
SELECT deployment_id,operation_id,expected_connection_revision,
       expected_deployment_revision,expected_operation_revision,
       protocol_profile_id,protocol_profile_version,
       admission,priority,weight,source_candidate_version
  FROM runtime_route_revision_items
 WHERE release_id=$1 AND ordinal=$2
 FOR UPDATE`, releaseID, index+1).Scan(
			&storedDeploymentID, &storedOperation, &storedConnectionRevision,
			&storedDeploymentRevision, &storedOperationRevision,
			&storedProfileID, &storedProfileVersion,
			&admission, &priority, &weight, &sourceVersion,
		); err != nil {
			return LegacyCompatibilityManifest{}, fmt.Errorf("%w: read legacy route item %d: %v", ErrUnavailable, index+1, err)
		}
		if storedDeploymentID != deploymentID || storedOperation != modeldeployment.LegacyOperationID ||
			storedConnectionRevision != override.ExpectedRoutingEpoch || storedDeploymentRevision != override.ExpectedRoutingEpoch ||
			admission != override.Admission || !sameOptionalInt(priority, override.Priority) ||
			!sameOptionalInt(weight, override.Weight) || sourceVersion != CandidateVersion {
			return LegacyCompatibilityManifest{}, fmt.Errorf("%w: normalized v1 route item %d differs from candidate provenance", ErrConflict, index+1)
		}
		if storedOperationRevision != nil && *storedOperationRevision != operationRevision {
			return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy operation revision changed", ErrConflict)
		}
		if storedProfileID != nil && *storedProfileID != operationProfileID ||
			storedProfileVersion != nil && *storedProfileVersion != operationProfileVersion {
			return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy protocol profile changed", ErrConflict)
		}
		if storedOperationRevision == nil || storedProfileID == nil || storedProfileVersion == nil {
			tag, err := tx.Exec(ctx, `
UPDATE runtime_route_revision_items
   SET expected_operation_revision=$3,protocol_profile_id=$4,protocol_profile_version=$5
 WHERE release_id=$1 AND ordinal=$2
   AND expected_operation_revision IS NULL
   AND protocol_profile_id IS NULL AND protocol_profile_version IS NULL`,
				releaseID, index+1, operationRevision, operationProfileID, operationProfileVersion)
			if err != nil {
				return LegacyCompatibilityManifest{}, fmt.Errorf("%w: pin legacy operation revision: %v", ErrUnavailable, err)
			}
			if tag.RowsAffected() != 1 {
				return LegacyCompatibilityManifest{}, ErrConflict
			}
		}
	}

	var itemCount int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_route_revision_items WHERE release_id=$1`, releaseID).Scan(&itemCount); err != nil {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: count legacy route items: %v", ErrUnavailable, err)
	}
	if itemCount != len(normalized) || itemCount == 0 {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy route item count", ErrConflict)
	}
	routes, sourceVersions, err := loadDeploymentRouteItems(ctx, tx, releaseID)
	if err != nil {
		return LegacyCompatibilityManifest{}, err
	}
	for _, sourceVersion := range sourceVersions {
		if sourceVersion != CandidateVersion {
			return LegacyCompatibilityManifest{}, fmt.Errorf("%w: legacy route source version", ErrConflict)
		}
	}
	routeHash := hashJSON(routes)
	sealedAt := m.now().UTC().Truncate(time.Microsecond)
	tag, err := tx.Exec(ctx, `
UPDATE runtime_change_releases
   SET public_model_id=$2,route_items_hash=$3,route_items_count=$4,
       route_items_sealed_at=$5,updated_at=$5
 WHERE id=$1 AND candidate_version=$6 AND version=$7
   AND public_model_id IS NULL AND route_items_sealed_at IS NULL`,
		releaseID, publicModelID, routeHash, itemCount, sealedAt, CandidateVersion, expectedVersion)
	if err != nil {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: seal legacy compatibility manifest: %v", ErrUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return LegacyCompatibilityManifest{}, ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return LegacyCompatibilityManifest{}, fmt.Errorf("%w: commit legacy compatibility seal: %v", ErrUnavailable, err)
	}
	return LegacyCompatibilityManifest{
		ReleaseID: releaseID, ReleaseRef: ref, PublicModelID: publicModelID,
		ModelName: modelName, RouteItemsHash: routeHash, RouteItemsCount: itemCount,
		SealedAt: sealedAt, SlotVersion: slotVersion,
	}, nil
}

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func verifyLegacyCompatibilityManifest(
	ctx context.Context,
	tx pgx.Tx,
	releaseID int64,
	expectedHash string,
	expectedCount int,
) error {
	routes, sourceVersions, err := loadDeploymentRouteItems(ctx, tx, releaseID)
	if err != nil {
		return err
	}
	if len(routes) != expectedCount || hashJSON(routes) != expectedHash {
		return fmt.Errorf("%w: sealed v1 compatibility route hash", ErrConflict)
	}
	for index, sourceVersion := range sourceVersions {
		if sourceVersion != CandidateVersion || routes[index].ModelOperationID != modeldeployment.LegacyOperationID {
			return fmt.Errorf("%w: sealed v1 compatibility route identity", ErrConflict)
		}
	}
	return nil
}
