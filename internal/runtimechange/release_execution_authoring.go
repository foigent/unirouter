package runtimechange

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/releaseexecution"
)

func readReleaseExecutionMetadata(ctx context.Context, q releaseexecution.Reader, result DeploymentRelease) (DeploymentRelease, error) {
	snapshot, err := releaseexecution.ReadSnapshot(ctx, q, result.ID)
	if err != nil {
		return result, err
	}
	if snapshot != nil {
		result.ProfileDefinitionDigest = snapshot.ProfileDefinitionDigest
		result.ExecutionSnapshotDigest, err = snapshot.Digest()
	}
	return result, err
}

func resolveAuthoringDefinition(ctx context.Context, tx pgx.Tx, mutation DeploymentReleaseAuthoringMutation, legacy modelprofile.Profile) (modelprofile.Definition, error) {
	definition, err := modelprofile.ReadDefinition(ctx, tx, mutation.ProfileDefinitionDigest)
	if err != nil {
		return modelprofile.Definition{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	if !modelprofile.SameStableProductSurface(legacy, definition.Profile()) {
		return modelprofile.Definition{}, fmt.Errorf("%w: product surface change requires an explicit product decision", ErrConflict)
	}
	var lineage string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(product_lineage_id,'') FROM model_registry WHERE id=$1 FOR UPDATE`, mutation.PublicModelID).Scan(&lineage); err != nil {
		return modelprofile.Definition{}, err
	}
	if lineage != "" && lineage != definition.ProductLineageID() {
		return modelprofile.Definition{}, fmt.Errorf("%w: stable product lineage differs", ErrConflict)
	}
	if lineage == "" {
		// The exact authoring command chooses this lineage via the definition;
		// no periodic importer or name-derived background assignment exists.
		_, err = tx.Exec(ctx, `UPDATE model_registry SET product_lineage_id=$2 WHERE id=$1 AND product_lineage_id IS NULL`, mutation.PublicModelID, definition.ProductLineageID())
		if err != nil {
			return modelprofile.Definition{}, err
		}
	}
	return definition, nil
}

func buildReleaseExecutionSnapshot(ctx context.Context, tx pgx.Tx, command DeploymentReleaseAuthoringCommand, definition modelprofile.Definition, routes []DeploymentRoute, routeHash string) (releaseexecution.Snapshot, error) {
	p := definition.Profile()
	s := releaseexecution.Snapshot{SchemaVersion: releaseexecution.SnapshotSchema, PublicModelID: command.Mutation.PublicModelID,
		ProductLineageID: definition.ProductLineageID(), ProfileDefinitionDigest: definition.Digest(), ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version,
		PublicContextLimit: command.Mutation.PublicContextLimit, PublicOutputLimit: command.Mutation.PublicOutputLimit, RouteItemsHash: routeHash, CapabilityPins: []releaseexecution.CapabilityPin{}}
	targets := map[int64]releaseexecution.CapabilityTarget{}
	for _, route := range routes {
		var operationDefinition, legacyID, legacyVersion string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(o.profile_definition_digest,''),m.execution_profile_id,m.execution_profile_version
FROM model_deployment_operations o JOIN model_deployments d ON d.id=o.deployment_id JOIN model_registry m ON m.id=d.public_model_id
WHERE d.id=$1 AND o.operation_id=$2`, route.DeploymentID, route.ModelOperationID).Scan(&operationDefinition, &legacyID, &legacyVersion); err != nil {
			return s, err
		}
		if operationDefinition != definition.Digest() && !(operationDefinition == "" && legacyID == p.ID && legacyVersion == p.Version) {
			return s, fmt.Errorf("%w: candidate operation is not bound to the selected exact definition", ErrConflict)
		}
		if route.ProtocolProfileID != p.ProtocolProfile || route.ProtocolProfileVersion != modelprofile.ChatOperationProtocolBindingVersionV1 {
			return s, fmt.Errorf("%w: data Profile protocol differs from route", ErrConflict)
		}
		targets[route.DeploymentID] = releaseexecution.CapabilityTarget{CustomerOrgID: command.CustomerOrgID, PublicModelID: command.Mutation.PublicModelID, DeploymentID: route.DeploymentID,
			OperationID: route.ModelOperationID, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, ProtocolProfileID: route.ProtocolProfileID, ProtocolProfileVersion: route.ProtocolProfileVersion}
	}
	for _, approvalID := range command.Mutation.CapabilityApprovalIDs {
		var deploymentID int64
		if err := tx.QueryRow(ctx, `SELECT deployment_id FROM model_capability_routing_approvals WHERE approval_id=$1`, approvalID).Scan(&deploymentID); err != nil {
			return s, err
		}
		target, ok := targets[deploymentID]
		if !ok {
			return s, fmt.Errorf("%w: capability decision is not a selected route", ErrConflict)
		}
		decision, err := releaseexecution.ReadCapabilityDecision(ctx, tx, target, approvalID, nil, false)
		if err != nil {
			return s, fmt.Errorf("%w: %v", ErrConflict, err)
		}
		if !slices.Contains(p.Capabilities, decision.Pin.CapabilityID) {
			return s, fmt.Errorf("%w: capability decision outside Profile", ErrConflict)
		}
		s.CapabilityPins = append(s.CapabilityPins, decision.Pin)
	}
	for _, route := range routes {
		for _, required := range p.AlwaysRequiredCapabilities {
			found := false
			for _, pin := range s.CapabilityPins {
				found = found || pin.DeploymentID == route.DeploymentID && pin.CapabilityID == required
			}
			if !found {
				return s, fmt.Errorf("%w: required capability decision %s missing", ErrConflict, required)
			}
		}
	}
	s.Requirements = releaseexecution.Requirements(definition, s.CapabilityPins)
	if err := s.ValidateDefinition(definition); err != nil {
		return s, err
	}
	_, err := s.CanonicalBytes()
	return s, err
}
