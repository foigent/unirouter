//go:build integration

package runtimechange_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPublicModelExecutionAuthorityManifestBindingIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	base := createCanonicalDeploymentFixture(t, pool, "m77-public-binding", "active", 100, 100)
	makeSoleInitialCustomer(t, pool, base.OrgID)
	fixture := qualifyResponsesDeployment(t, pool, base)
	manager, err := runtimechange.NewManager(pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)

	command := deploymentReleaseAuthoringCommand(
		fixture,
		runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial},
		[]runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.DeploymentID, OperationID: fixture.OperationID,
		}},
		"8", "9", "op_m78_public_binding",
	)
	command.Mutation.Reason = "M78 exact public execution authority"
	authored, err := manager.AuthorSealedDeploymentRelease(ctx, command)
	require.NoError(t, err)
	prepared := authored.Release
	require.Len(t, prepared.Candidate.DeploymentRoutes, 1)
	route := prepared.Candidate.DeploymentRoutes[0]
	require.Positive(t, route.PublicExecutionBindingID)
	require.Equal(t, endpointprotocol.PublicDeliveryContractV1, route.DeliveryContractVersion)
	require.Equal(t, endpointprotocol.PublicCustomerChargeContractV1, route.ChargeContractVersion)
	require.Equal(t, endpointprotocol.PublicIdempotencyContractV1, route.IdempotencyContractVersion)

	_, err = pool.Exec(ctx, `
INSERT INTO runtime_route_revision_items (
    release_id,ordinal,deployment_id,operation_id,
    expected_connection_revision,expected_deployment_revision,expected_operation_revision,
    protocol_profile_id,protocol_profile_version,public_path,executor_contract_version,
    admission,source_candidate_version)
VALUES ($1,2,$2,$3,$4,$5,$6,$7,$8,$9,$10,'allow',$11)`,
		prepared.ID, route.DeploymentID, route.ModelOperationID,
		route.ExpectedConnectionRevision, route.ExpectedDeploymentRevision,
		route.ExpectedOperationRevision, route.ProtocolProfileID,
		route.ProtocolProfileVersion, route.PublicPath,
		route.ExecutorContractVersion, runtimechange.DeploymentCandidateVersion)
	require.ErrorContains(t, err, "Responses/Messages runtime item requires the exact M77 public execution binding")

	var bindingID, itemBindingID int64
	var releaseID, publicModelID, deploymentID, connectionID, approvalID int64
	var ordinal int
	var publicExecutable bool
	var state, sourceKind, sourceRef string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT binding.public_execution_binding_id,item.public_execution_binding_id,
       binding.release_id,binding.ordinal,binding.public_model_id,
       binding.deployment_id,binding.connection_id,binding.production_approval_id,
       qualification.public_executable,qualification.qualification_state,
       binding.source_kind,binding.source_ref
  FROM model_operation_public_execution_bindings binding
  JOIN runtime_route_revision_items item
    ON item.public_execution_binding_id=binding.public_execution_binding_id
   AND item.release_id=binding.release_id AND item.ordinal=binding.ordinal
  JOIN model_operation_public_execution_qualifications qualification
    ON qualification.public_execution_binding_id=binding.public_execution_binding_id
 WHERE binding.public_execution_binding_id=$1`, route.PublicExecutionBindingID).Scan(
		&bindingID, &itemBindingID, &releaseID, &ordinal, &publicModelID,
		&deploymentID, &connectionID, &approvalID, &publicExecutable, &state,
		&sourceKind, &sourceRef,
	))
	require.Equal(t, route.PublicExecutionBindingID, bindingID)
	require.Equal(t, bindingID, itemBindingID)
	require.Equal(t, prepared.ID, releaseID)
	require.Equal(t, 1, ordinal)
	require.Equal(t, fixture.PublicModelID, publicModelID)
	require.Equal(t, fixture.DeploymentID, deploymentID)
	require.Equal(t, fixture.ConnectionID, connectionID)
	require.Positive(t, approvalID)
	require.True(t, publicExecutable)
	require.Equal(t, "public_executable", state)
	require.Equal(t, "runtime.public_execution_binding.v1", sourceKind)
	require.Equal(t, "release:"+strconv.FormatInt(prepared.ID, 10)+"/item:1", sourceRef)

	require.NotNil(t, prepared.RouteItemsSealedAt)
	_, err = pool.Exec(ctx, `
UPDATE model_operation_public_execution_bindings
   SET charge_contract_version='other'
 WHERE public_execution_binding_id=$1`, bindingID)
	require.ErrorContains(t, err, "append-only")

	// M75 approval proves the immutable protocol/origin/revision structure.
	// Revoking its historical credential does not rewrite that structural
	// receipt; current credential eligibility is a separate Planner gate.
	_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1,updated_at=NOW()
 WHERE id=$1`, fixture.CredentialID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT public_executable,qualification_state
  FROM model_operation_public_execution_qualifications
 WHERE public_execution_binding_id=$1`, bindingID).Scan(&publicExecutable, &state))
	require.True(t, publicExecutable)
	require.Equal(t, "public_executable", state)
}

func TestPublicModelExecutionAuthorityAuthorRetryKeepsCallerSelectionImmutableIntegration(t *testing.T) {
	tests := []struct {
		name       string
		suffix     string
		installSQL string
		removeSQL  string
	}{
		{
			name:   "route insert failure",
			suffix: "insert",
			installSQL: `
CREATE FUNCTION reject_m77_route_insert_once() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'injected route insert failure';
END;
$$;
CREATE TRIGGER reject_m77_route_insert_once
BEFORE INSERT ON runtime_route_revision_items
FOR EACH ROW EXECUTE FUNCTION reject_m77_route_insert_once();`,
			removeSQL: `
DROP TRIGGER reject_m77_route_insert_once ON runtime_route_revision_items;
DROP FUNCTION reject_m77_route_insert_once();`,
		},
		{
			name:   "commit failure",
			suffix: "commit",
			installSQL: `
CREATE FUNCTION reject_m77_prepare_commit_once() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'injected prepare commit failure';
END;
$$;
CREATE CONSTRAINT TRIGGER reject_m77_prepare_commit_once
AFTER INSERT ON runtime_change_releases
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION reject_m77_prepare_commit_once();`,
			removeSQL: `
DROP TRIGGER reject_m77_prepare_commit_once ON runtime_change_releases;
DROP FUNCTION reject_m77_prepare_commit_once();`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := context.Background()
			base := createCanonicalDeploymentFixture(t, pool, "m77-retry-"+test.suffix, "active", 100, 100)
			makeSoleInitialCustomer(t, pool, base.OrgID)
			fixture := qualifyResponsesDeployment(t, pool, base)
			manager, err := runtimechange.NewManager(pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
			require.NoError(t, err)
			command := deploymentReleaseAuthoringCommand(
				fixture,
				runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial},
				[]runtimechange.DeploymentReleaseSelection{{
					DeploymentID: fixture.DeploymentID, OperationID: fixture.OperationID,
				}},
				"a", "b", "op_m78_public_binding_retry_"+test.suffix,
			)
			command.Mutation.Reason = "retry exact public execution authority"
			original := command

			_, err = pool.Exec(ctx, test.installSQL)
			require.NoError(t, err)
			_, err = manager.AuthorSealedDeploymentRelease(ctx, command)
			require.Error(t, err)
			require.Equal(t, original, command)
			_, err = pool.Exec(ctx, test.removeSQL)
			require.NoError(t, err)

			authored, err := manager.AuthorSealedDeploymentRelease(ctx, command)
			require.NoError(t, err)
			require.Positive(t, authored.Release.Candidate.DeploymentRoutes[0].PublicExecutionBindingID)
			require.Equal(t, original, command)
		})
	}
}
