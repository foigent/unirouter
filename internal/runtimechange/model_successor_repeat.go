package runtimechange

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

// requirePrimaryAliasPredecessor observes existing authority under the same
// transaction as the cutover. Only immutable identity and execution pins are
// required of the predecessor: an unhealthy old credential must not prevent a
// qualified replacement. The new target still undergoes normal activation.
func requirePrimaryAliasPredecessor(ctx context.Context, tx pgx.Tx, command ModelSuccessorCutoverCommand) (map[string]int64, error) {
	m := command.Mutation
	var aliasID int64
	var target, status string
	var primary bool
	err := tx.QueryRow(ctx, `SELECT id,target_model,status,is_primary FROM model_aliases
 WHERE org_id=$1 AND alias=$2 FOR UPDATE`, command.CustomerOrgID, m.StableRequestModel).Scan(&aliasID, &target, &status, &primary)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: current primary alias is absent", ErrConflict)
	}
	if err != nil {
		return nil, lifecycleWriteError("lock current primary alias", err)
	}
	if aliasID != m.ExpectedPrimaryAliasID || target != m.PredecessorModelName || status != "active" || !primary {
		return nil, fmt.Errorf("%w: primary alias before-state drift", ErrConflict)
	}
	old, err := readDeploymentRelease(ctx, tx, m.ExpectedPredecessorReleaseRef, true)
	if err != nil {
		return nil, err
	}
	if old.PublicModelID != m.PredecessorPublicModelID || old.ModelName != m.PredecessorModelName {
		return nil, fmt.Errorf("%w: predecessor release identity", ErrConflict)
	}
	slot, err := lockDeploymentLifecycleSlot(ctx, tx, m.PredecessorModelName, m.PredecessorPublicModelID)
	if err != nil {
		return nil, err
	}
	if slot.Version != m.ExpectedPredecessorSlotVersion || slot.StableReleaseID == nil ||
		*slot.StableReleaseID != old.ID || slot.CanaryReleaseID != nil || slot.CanaryBasisPoints != 0 {
		return nil, fmt.Errorf("%w: predecessor slot before-state drift", ErrConflict)
	}
	if err := validateSealedDeploymentRelease(ctx, tx, old, "active", slot.Version); err != nil {
		return nil, err
	}
	next, err := readDeploymentRelease(ctx, tx, m.ReleaseRef, true)
	if err != nil {
		return nil, err
	}
	if next.PublicModelID != m.SuccessorPublicModelID || next.ModelName != m.SuccessorModelName ||
		next.Version != m.ExpectedReleaseVersion || next.Stage != "validated" || next.ParentStableReleaseID != nil {
		return nil, fmt.Errorf("%w: repeated successor target release", ErrConflict)
	}
	if err := validateSealedDeploymentRelease(ctx, tx, next, "validated", 1); err != nil {
		return nil, err
	}
	if old.ExecutionProfileID == "" || old.ExecutionProfileID != next.ExecutionProfileID || old.ExecutionProfileID == modelprofile.UnverifiedProfileID {
		return nil, fmt.Errorf("%w: repeated successor requires the same verified model product", ErrConflict)
	}
	for _, release := range []DeploymentRelease{old, next} {
		profile, ok := modelprofile.Lookup(release.ExecutionProfileID, release.ExecutionProfileVersion)
		if !ok || profile.ID == modelprofile.UnverifiedProfileID {
			return nil, fmt.Errorf("%w: repeated successor exact profile is unavailable", ErrConflict)
		}
		if err := validateSuccessorReleaseProducts(ctx, tx, command.CustomerOrgID, release, profile); err != nil {
			return nil, err
		}
	}
	return map[string]int64{
		"primary_alias_id": aliasID, "slot_version": slot.Version,
		"stable_release_id": old.ID, "release_version": old.Version,
	}, nil
}

func validateSuccessorReleaseProducts(ctx context.Context, tx pgx.Tx, orgID int64, release DeploymentRelease, profile modelprofile.Profile) error {
	for _, route := range release.Candidate.DeploymentRoutes {
		var binding modelprofile.ProductBinding
		var config []byte
		var connectionRevision, deploymentRevision int64
		err := tx.QueryRow(ctx, `
SELECT connection.provider,connection.base_url,connection.adapter_type,connection.adapter_config,
       deployment.upstream_model,connection.config_revision,deployment.config_revision
 FROM model_deployments deployment JOIN provider_connections connection ON connection.id=deployment.connection_id
 WHERE deployment.id=$1 AND deployment.customer_org_id=$2 AND deployment.public_model_id=$3
 FOR SHARE OF deployment,connection`, route.DeploymentID, orgID, release.PublicModelID).Scan(
			&binding.Provider, &binding.BaseURL, &binding.AdapterType, &config, &binding.UpstreamModel, &connectionRevision, &deploymentRevision)
		if err != nil {
			return lifecycleWriteError("read successor product binding", err)
		}
		if connectionRevision != route.ExpectedConnectionRevision || deploymentRevision != route.ExpectedDeploymentRevision {
			return fmt.Errorf("%w: successor product binding revision", ErrConflict)
		}
		binding.OperationID, binding.ProtocolProfileID, binding.ProtocolProfileVersion = route.ModelOperationID, route.ProtocolProfileID, route.ProtocolProfileVersion
		binding.EndpointPathMode, err = adapters.EffectiveEndpointPathMode(binding.AdapterType, config)
		if err != nil {
			return fmt.Errorf("%w: successor endpoint configuration", ErrConflict)
		}
		if err := modelprofile.ValidateProductBinding(profile, binding); err != nil {
			return fmt.Errorf("%w: successor product identity: %v", ErrConflict, err)
		}
	}
	return nil
}

func validPrimaryAliasAuthoritySnapshot(snapshot map[string]int64) bool {
	if len(snapshot) != 4 {
		return false
	}
	for _, key := range []string{"primary_alias_id", "slot_version", "stable_release_id", "release_version"} {
		if snapshot[key] <= 0 {
			return false
		}
	}
	return true
}
