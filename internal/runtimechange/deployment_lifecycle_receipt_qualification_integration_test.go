//go:build integration

package runtimechange_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDeploymentLifecycleReceiptRequiredQualificationContractionIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	manager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()
	stableFixture, stable, candidateFixture, candidate := seedRequiredResponsesLifecycle(
		t, pool, manager, "m76-required-cancel",
	)

	started, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		stableFixture, runtimechange.DeploymentLifecycleStartCanary, candidate,
		1, 500, '1', '2', "op_m76_required_start",
	))
	require.NoError(t, err)
	assertRequiredQualification(t, started, "target", true, "production_qualified")
	assertRequiredQualification(t, started, "stable_parent", true, "production_qualified")

	_, err = pool.Exec(ctx, `
UPDATE provider_connections SET admin_state='disabled' WHERE id=$1`, candidateFixture.ConnectionID)
	require.NoError(t, err)
	canary := mustDeploymentRelease(t, manager, candidate.ReleaseRef)
	promoteCommand := deploymentLifecycleReceiptCommand(
		stableFixture, runtimechange.DeploymentLifecyclePromote, canary,
		started.Slot.AfterVersion, 0, '3', '4', "op_m76_required_promote_drift",
	)
	_, err = manager.ApplyDeploymentLifecycle(ctx, promoteCommand)
	require.Error(t, err, "authority expansion must fail closed after required target drift")
	var promoteReceipts int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND operation_scope='model_deployment.lifecycle.promote.v1'
   AND idempotency_key_hash=$2`, stableFixture.OrgID, promoteCommand.IdempotencyKeyHash).Scan(&promoteReceipts))
	require.Zero(t, promoteReceipts, "failed expansion must roll back its receipt claim")

	cancelled, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		stableFixture, runtimechange.DeploymentLifecycleCancelCanary, canary,
		started.Slot.AfterVersion, 0, '5', '6', "op_m76_required_cancel_drift",
	))
	require.NoError(t, err, "cancel is a safety contraction even when required target qualification drifted")
	require.Equal(t, stable.ID, *cancelled.Slot.AfterStableReleaseID)
	require.Nil(t, cancelled.Slot.AfterCanaryReleaseID)
	assertRequiredQualification(t, cancelled, "target", false, "connection_disabled")
	assertRequiredQualification(t, cancelled, "stable_parent", true, "production_qualified")
}

func TestDeploymentLifecycleReceiptRequiredQualificationRollbackIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	manager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()
	stableFixture, stable, candidateFixture, candidate := seedRequiredResponsesLifecycle(
		t, pool, manager, "m76-required-rollback",
	)
	started, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		stableFixture, runtimechange.DeploymentLifecycleStartCanary, candidate,
		1, 500, '7', '8', "op_m76_required_rollback_start",
	))
	require.NoError(t, err)
	promoted, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		stableFixture, runtimechange.DeploymentLifecyclePromote,
		mustDeploymentRelease(t, manager, candidate.ReleaseRef),
		started.Slot.AfterVersion, 0, '9', 'a', "op_m76_required_rollback_promote",
	))
	require.NoError(t, err)
	assertRequiredQualification(t, promoted, "target", true, "production_qualified")

	_, err = pool.Exec(ctx, `
UPDATE provider_connections SET admin_state='disabled' WHERE id=$1`, candidateFixture.ConnectionID)
	require.NoError(t, err)
	rolledBack, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		stableFixture, runtimechange.DeploymentLifecycleRollbackStable,
		mustDeploymentRelease(t, manager, candidate.ReleaseRef),
		promoted.Slot.AfterVersion, 0, 'b', 'c', "op_m76_required_rollback_drift",
	))
	require.NoError(t, err,
		"stable rollback authority is the qualified stable parent, not the failing active target")
	require.Equal(t, stable.ID, *rolledBack.Slot.AfterStableReleaseID)
	assertRequiredQualification(t, rolledBack, "target", false, "connection_disabled")
	assertRequiredQualification(t, rolledBack, "stable_parent", true, "production_qualified")
}

func seedRequiredResponsesLifecycle(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	suffix string,
) (
	canonicalDeploymentFixture,
	runtimechange.DeploymentRelease,
	canonicalDeploymentFixture,
	runtimechange.DeploymentRelease,
) {
	t.Helper()
	chatBase := createCanonicalDeploymentFixture(t, pool, suffix, "active", 100, 100)
	makeSoleInitialCustomer(t, pool, chatBase.OrgID)
	stableFixture := qualifyResponsesDeployment(t, pool, chatBase)
	sealedStable := sealInitialDeploymentRoutes(
		t, pool, manager, stableFixture, []runtimechange.DeploymentRoute{responsesDeploymentRoute(stableFixture)},
		"rcr_"+safeReleaseSuffix(suffix)+"_stable",
	)
	stable := activateInitialDeploymentReleaseFixture(t, manager, stableFixture, sealedStable)

	childChat := createAdditionalDeploymentFixture(
		t, pool, chatBase, suffix+"-target", "active", 100, 100,
	)
	candidateFixture := qualifyResponsesDeployment(t, pool, childChat)
	candidate := prepareSealedChildRoutes(
		t, pool, manager, stableFixture, []runtimechange.DeploymentRoute{responsesDeploymentRoute(candidateFixture)},
		"rcr_"+safeReleaseSuffix(suffix)+"_target",
	)
	return stableFixture, stable, candidateFixture, candidate
}

func responsesDeploymentRoute(fixture canonicalDeploymentFixture) runtimechange.DeploymentRoute {
	route := deploymentRoute(fixture)
	route.PublicPath = modeloperation.PathResponses
	route.ExecutorContractVersion = modelqualification.ExecutorContractVersion
	return route
}

func qualifyResponsesDeployment(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture canonicalDeploymentFixture,
) canonicalDeploymentFixture {
	t.Helper()
	ctx := context.Background()
	fixture.OperationID = string(modeloperation.IDResponses)
	fixture.OperationRevision = 1
	fixture.ProtocolProfileID = endpointprotocol.ProfileOpenAIResponsesText
	fixture.ProtocolVersion = "1.0.0"
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','declared','',1,$3,$4)`,
		fixture.DeploymentID, fixture.OperationID, fixture.ProtocolProfileID, fixture.ProtocolVersion)
	require.NoError(t, err)

	var probeID int64
	var testedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,reported_model,connection_revision,deployment_revision,
  operation_revision,public_path,executor_contract_version,https_origin,
  credential_binding_id,credential_state_revision,success,status_code,latency_ms,
  source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,$5,$6,$6,$7,$8,1,$9,$10,'https://provider.invalid',
        $11,$12,TRUE,200,1,'admin.endpointprotocol_live_probe.v1',$13,$14,$15,NOW())
RETURNING probe_id,tested_at`,
		fixture.DeploymentID, fixture.ConnectionID, fixture.OperationID,
		fixture.ProtocolProfileID, fixture.ProtocolVersion, fixture.UpstreamModel,
		fixture.ConnectionRevision, fixture.DeploymentRevision, modeloperation.PathResponses,
		modelqualification.ExecutorContractVersion, fixture.CredentialID, fixture.CredentialRevision,
		fmt.Sprintf("m76-required:%d", fixture.DeploymentID),
		fmt.Sprintf("sha256:%064x", fixture.DeploymentID), fixture.UserID,
	).Scan(&probeID, &testedAt))
	result, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='live_probed',evidence_ref='probe:' || $3::bigint::text,
       verified_at=$4,revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND revision=1`,
		fixture.DeploymentID, fixture.OperationID, probeID, testedAt)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.RowsAffected())

	_, err = pool.Exec(ctx, `
INSERT INTO model_operation_production_approvals (
  customer_org_id,deployment_id,connection_id,operation_id,probe_id,
  credential_binding_id,credential_state_revision,credential_link_created_at,
  public_path,protocol_profile_id,protocol_profile_version,executor_contract_version,https_origin,
  connection_revision,deployment_revision,probe_operation_revision,
  source_operation_revision,approved_operation_revision,approved_by,approved_by_org_id,
  reason,source_kind,source_ref,approved_at)
SELECT deployment.customer_org_id,probe.deployment_id,probe.connection_id,probe.operation_id,probe.probe_id,
       probe.credential_binding_id,probe.credential_state_revision,credential_link.created_at,
       probe.public_path,probe.probe_profile_id,probe.probe_profile_version,
       probe.executor_contract_version,probe.https_origin,
       probe.connection_revision,probe.deployment_revision,probe.operation_revision,
       operation.revision,operation.revision+1,$2,$3,'M76 required qualification fixture',
       'admin.model_operation_production_approval.v1','probe:' || probe.probe_id::text,NOW()
  FROM model_deployment_probes probe
  JOIN model_deployments deployment ON deployment.id=probe.deployment_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=probe.deployment_id AND operation.operation_id=probe.operation_id
  JOIN credential_binding_deployments credential_link
    ON credential_link.binding_id=probe.credential_binding_id
   AND credential_link.deployment_id=probe.deployment_id
   AND credential_link.connection_id=probe.connection_id
 WHERE probe.probe_id=$1`, probeID, fixture.UserID, fixture.OrgID)
	require.NoError(t, err)

	var qualified bool
	var state string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT qualified,qualification_state,current_operation_revision
  FROM model_operation_execution_qualifications
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.DeploymentID, fixture.OperationID).Scan(
		&qualified, &state, &fixture.OperationRevision,
	))
	require.True(t, qualified)
	require.Equal(t, "production_qualified", state)
	return fixture
}

func assertRequiredQualification(
	t *testing.T,
	receipt runtimechange.DeploymentLifecycleReceipt,
	role string,
	qualified bool,
	state string,
) {
	t.Helper()
	for _, evidence := range receipt.Qualifications {
		if evidence.ReleaseRole != role {
			continue
		}
		require.True(t, evidence.QualificationRequired)
		require.Equal(t, qualified, evidence.Qualified)
		require.Equal(t, state, evidence.QualificationState)
		require.Equal(t, modeloperation.PathResponses, evidence.PublicPath)
		require.Equal(t, modelqualification.ExecutorContractVersion, evidence.ExecutorContractVersion)
		return
	}
	t.Fatalf("missing required qualification evidence for %s", role)
}
