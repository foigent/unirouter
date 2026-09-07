package admin

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

// Data candidate authoring selects an immutable definition for an existing
// stable product. It never changes that product's bootstrap contract, active
// Deployment, qualification or Slot. The normal create receipt remains owner.
func resolveCanonicalDataDefinition(ctx context.Context, tx pgx.Tx, model canonicalPublicModelIdentity, input canonicalDeploymentInput) (canonicalPublicModelIdentity, error) {
	d, err := modelprofile.ReadDefinition(ctx, tx, input.ProfileDefinitionDigest)
	if err != nil {
		return model, fmt.Errorf("read candidate definition: %w", err)
	}
	var lineage string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(product_lineage_id,'') FROM model_registry WHERE id=$1 FOR UPDATE`, model.ID).Scan(&lineage); err != nil {
		return model, err
	}
	bootstrap, err := modelprofile.ResolveAuthoringBootstrap(ctx, tx, model.ExecutionProfileID, model.ExecutionProfileVersion, lineage)
	if err != nil || !modelprofile.SameStableProductSurface(bootstrap, d.Profile()) || input.MaxContext == nil || input.DefaultMaxOutput == nil ||
		modelprofile.ValidateEffectiveLimits(d.Profile(), *input.MaxContext, *input.DefaultMaxOutput) != nil {
		return model, errCanonicalApplicationInvalid
	}
	if lineage != "" && lineage != d.ProductLineageID() {
		return model, errCanonicalApplicationConflict
	}
	if lineage == "" {
		if _, err := tx.Exec(ctx, `UPDATE model_registry SET product_lineage_id=$2 WHERE id=$1 AND product_lineage_id IS NULL`, model.ID, d.ProductLineageID()); err != nil {
			return model, err
		}
	}
	model.Definition = d
	model.ExecutionProfileID, model.ExecutionProfileVersion = d.Profile().ID, d.Profile().Version
	return model, nil
}

func ensureCanonicalDataCapabilities(ctx context.Context, tx pgx.Tx, deploymentID int64, definition modelprofile.Definition, connection canonicalConnectionIdentity, upstream string, operation canonicalOperationInput) error {
	bindings, err := modelcapabilityqualification.ResolveDefinitionBindings(ctx, tx, definition, operation.OperationID, operation.ProtocolProfileID, operation.ProtocolProfileVersion)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.ExpectedProvider != connection.Provider || binding.ExpectedUpstreamModel != upstream {
			return errCanonicalApplicationConflict
		}
		ref := "capability_contract:" + binding.ProbeContractID + "@" + binding.ProbeContractVersion
		if _, err := tx.Exec(ctx, `INSERT INTO model_deployment_capabilities(deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES($1,$2,$3,'enabled','declared',$4,1) ON CONFLICT(deployment_id,operation_id,capability_id) DO NOTHING`, deploymentID, binding.OperationID, binding.CapabilityID, ref); err != nil {
			return err
		}
	}
	return nil
}
