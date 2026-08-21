package runtimechange

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ResolveDeployment is the canonical resolver. It reads only a sealed v2
// normalized manifest. In particular, it never falls back to interpreting a
// v1 candidate JSON document as deployment authority.
func (m *Manager) ResolveDeployment(ctx context.Context, customerOrgID int64, modelName, selectionKey string) (DeploymentAssignment, error) {
	state, basisPoints, err := m.resolveDeploymentSlot(ctx, customerOrgID, strings.TrimSpace(modelName))
	if err != nil {
		return DeploymentAssignment{}, err
	}
	if state.canary.ReleaseRef != "" && canarySelected(modelName, selectionKey, state.canary.ReleaseRef, basisPoints) {
		return state.canary, nil
	}
	return state.stable, nil
}

func (m *Manager) StableDeployment(ctx context.Context, customerOrgID int64, modelName string) (DeploymentAssignment, error) {
	state, _, err := m.resolveDeploymentSlot(ctx, customerOrgID, strings.TrimSpace(modelName))
	if err != nil {
		return DeploymentAssignment{}, err
	}
	return state.stable, nil
}

type resolvedDeploymentSlot struct {
	stable DeploymentAssignment
	canary DeploymentAssignment
}

func (m *Manager) resolveDeploymentSlot(ctx context.Context, customerOrgID int64, modelName string) (resolvedDeploymentSlot, int, error) {
	if customerOrgID <= 0 || modelName == "" {
		return resolvedDeploymentSlot{}, 0, fmt.Errorf("%w: customer organization or model", ErrInvalid)
	}
	var publicModelID int64
	if err := m.db.QueryRow(ctx, `
SELECT id FROM model_registry
 WHERE org_id=$1 AND model_name=$2 AND lifecycle<>'retired'`, customerOrgID, modelName).Scan(&publicModelID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return resolvedDeploymentSlot{}, 0, nil
		}
		return resolvedDeploymentSlot{}, 0, fmt.Errorf("%w: resolve public model: %v", ErrUnavailable, err)
	}
	var stableID, canaryID *int64
	var slotVersion int64
	var basisPoints int
	err := m.db.QueryRow(ctx, `
SELECT stable_release_id,canary_release_id,canary_basis_points,slot_version
  FROM runtime_change_slots
 WHERE public_model_id=$1`, publicModelID).Scan(&stableID, &canaryID, &basisPoints, &slotVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		var legacyExists bool
		if lookupErr := m.db.QueryRow(ctx, `SELECT EXISTS(
            SELECT 1 FROM runtime_change_slots WHERE model_name=$1
        )`, modelName).Scan(&legacyExists); lookupErr != nil {
			return resolvedDeploymentSlot{}, 0, fmt.Errorf("%w: inspect legacy runtime slot: %v", ErrUnavailable, lookupErr)
		}
		if legacyExists {
			return resolvedDeploymentSlot{}, 0, fmt.Errorf("%w: public model still uses a v1 channel slot", ErrUnavailable)
		}
		return resolvedDeploymentSlot{}, 0, nil
	}
	if err != nil {
		return resolvedDeploymentSlot{}, 0, fmt.Errorf("%w: resolve deployment slot: %v", ErrUnavailable, err)
	}
	if canaryID == nil && basisPoints != 0 {
		return resolvedDeploymentSlot{}, 0, fmt.Errorf("%w: canonical canary slot", ErrUnavailable)
	}
	if canaryID != nil && (basisPoints < 1 || basisPoints > 5000) {
		return resolvedDeploymentSlot{}, 0, fmt.Errorf("%w: canonical canary basis points", ErrUnavailable)
	}
	state := resolvedDeploymentSlot{}
	if stableID != nil {
		assignment, loadErr := m.loadSealedDeploymentAssignment(ctx, *stableID, publicModelID, modelName, "active", slotVersion)
		if loadErr != nil {
			return resolvedDeploymentSlot{}, 0, loadErr
		}
		state.stable = assignment
	}
	if canaryID != nil {
		assignment, loadErr := m.loadSealedDeploymentAssignment(ctx, *canaryID, publicModelID, modelName, "canary", slotVersion)
		if loadErr != nil {
			return resolvedDeploymentSlot{}, 0, loadErr
		}
		state.canary = assignment
	}
	return state, basisPoints, nil
}

func (m *Manager) loadSealedDeploymentAssignment(
	ctx context.Context,
	releaseID, publicModelID int64,
	modelName, expectedStage string,
	slotVersion int64,
) (DeploymentAssignment, error) {
	return loadSealedDeploymentAssignmentFrom(ctx, m.db, releaseID, publicModelID, modelName, expectedStage, slotVersion)
}

func loadSealedDeploymentAssignmentFrom(
	ctx context.Context,
	store deploymentManifestStore,
	releaseID, publicModelID int64,
	modelName, expectedStage string,
	slotVersion int64,
) (DeploymentAssignment, error) {
	var ref, version, routeItemsHash, stage, storedModelName string
	var storedPublicModelID int64
	var routeItemsCount int
	var sealed bool
	if err := store.QueryRow(ctx, `
SELECT release_ref,candidate_version,public_model_id,model_name,route_items_hash,
       route_items_count,route_items_sealed_at IS NOT NULL,stage
  FROM runtime_change_releases WHERE id=$1`, releaseID).Scan(
		&ref, &version, &storedPublicModelID, &storedModelName, &routeItemsHash,
		&routeItemsCount, &sealed, &stage,
	); err != nil {
		return DeploymentAssignment{}, fmt.Errorf("%w: read canonical runtime release: %v", ErrUnavailable, err)
	}
	if version != DeploymentCandidateVersion || !sealed || storedPublicModelID != publicModelID ||
		storedModelName != modelName || stage != expectedStage || routeItemsCount <= 0 {
		return DeploymentAssignment{}, fmt.Errorf("%w: canonical runtime release identity", ErrUnavailable)
	}
	routes, versions, err := loadDeploymentRouteItems(ctx, store, releaseID)
	if err != nil {
		return DeploymentAssignment{}, err
	}
	if len(routes) != routeItemsCount || hashJSON(routes) != routeItemsHash {
		return DeploymentAssignment{}, fmt.Errorf("%w: canonical route manifest hash", ErrUnavailable)
	}
	for _, sourceVersion := range versions {
		if sourceVersion != DeploymentCandidateVersion {
			return DeploymentAssignment{}, fmt.Errorf("%w: canonical route item version", ErrUnavailable)
		}
	}
	for _, route := range routes {
		if route.ModelOperationID == "legacy.any.v1" || route.ProtocolProfileID == "legacy.any.v1" ||
			route.ProtocolProfileID == "" || route.ProtocolProfileVersion == "" {
			return DeploymentAssignment{}, fmt.Errorf("%w: canonical assignment contains a legacy protocol profile", ErrUnavailable)
		}
	}
	return DeploymentAssignment{
		ContractVersion: DeploymentAssignmentVersion,
		Identity:        fmt.Sprintf("%s:%d:%s:%s", DeploymentAssignmentVersion, slotVersion, ref, routeItemsHash),
		ReleaseRef:      ref,
		Stage:           stage,
		PublicModelID:   publicModelID,
		ModelName:       modelName,
		RouteItemsHash:  routeItemsHash,
		Routes:          routes,
	}, nil
}
