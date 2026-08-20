//go:build integration

package modelexecution

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

type publicExecutionSnapshotBuilder struct{}

func (publicExecutionSnapshotBuilder) BuildRuntimeChangeSnapshots(
	context.Context,
	string,
	[]routingpolicy.ChannelOverride,
	[]routingpolicy.ChannelOverride,
) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error) {
	return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, nil
}

type publicExecutionFixture struct {
	pool    *pgxpool.Pool
	manager *runtimechange.Manager
	billing *billing.Billing

	orgID, userID, apiKeyID             int64
	publicModelID, connectionID         int64
	deploymentID, credentialBindingID   int64
	operationRevision, activeBindingID  int64
	model, upstreamModel, releaseSuffix string
	route                               runtimechange.DeploymentRoute
}

func TestPublicCoordinatorPersistsExactM77AttemptAfterPlannerExactItemJoinIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	fixture := newPublicExecutionFixture(t, pool, "exact")
	siblingBindingID := fixture.createSealedSiblingRelease(t)
	require.NotEqual(t, fixture.activeBindingID, siblingBindingID)

	plan, route := fixture.plan(t, "exact-selected-item")
	require.NotNil(t, route.PublicEvidence)
	require.Equal(t, fixture.activeBindingID, route.PublicEvidence.PublicExecutionBindingID,
		"a qualified sibling release for the same deployment must not authorize the selected item")
	require.NotEqual(t, siblingBindingID, route.PublicEvidence.PublicExecutionBindingID)
	require.Equal(t, fixture.credentialBindingID, route.PublicEvidence.CredentialBindingID)

	identity := fixture.freeze(t, "exact-selected-item")
	coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
	require.NoError(t, err)
	prepared, err := preparePublicRequest(plan, route, identity)
	require.NoError(t, err)
	prepared, err = coordinator.prepareDispatchCapabilities(prepared)
	require.NoError(t, err)
	decision, retry, err := coordinator.authorizePublicOnce(t.Context(), prepared)
	require.NoError(t, err)
	require.Equal(t, retryNone, retry)
	require.Equal(t, Applied, decision.Status)
	require.NotNil(t, decision.PublicEvidence)
	require.Equal(t, executionevidence.EvidenceV1{}, decision.Evidence)
	wantDigest, err := executionevidence.DigestV2(*decision.PublicEvidence)
	require.NoError(t, err)
	require.Equal(t, wantDigest, decision.EvidenceHash)

	var version, evidenceHash, delivery, charge, idempotency string
	var publicBindingID int64
	var channelID, channelKeyID *int64
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT route_evidence_version,route_evidence_hash,
       route_public_execution_binding_id,route_delivery_contract_version,
       route_charge_contract_version,route_idempotency_contract_version,
       channel_id,channel_key_id
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3`, fixture.orgID,
		identity.RequestID, identity.AttemptID).Scan(
		&version, &evidenceHash, &publicBindingID, &delivery, &charge, &idempotency,
		&channelID, &channelKeyID,
	))
	require.Equal(t, executionevidence.ContractVersionV2, version)
	require.Equal(t, decision.EvidenceHash, evidenceHash)
	require.Equal(t, fixture.activeBindingID, publicBindingID)
	require.Equal(t, executionevidence.PublicDeliveryContractV1, delivery)
	require.Equal(t, executionevidence.PublicChargeContractV1, charge)
	require.Equal(t, executionevidence.PublicIdempotencyContractV1, idempotency)
	require.Nil(t, channelID)
	require.Nil(t, channelKeyID)

	downgraded := decision
	downgraded.Evidence, err = decision.PublicEvidence.RouteEvidenceV1()
	require.NoError(t, err)
	downgraded.PublicEvidence = nil
	_, err = coordinator.MarkSuccess(t.Context(), downgraded)
	require.ErrorIs(t, err, ErrInvalid, "public attempt health must not accept a V1 digest downgrade")

	require.NoError(t, fixture.billing.CompleteProviderAttempt(t.Context(), billing.ProviderAttemptCompletion{
		OrgID: decision.ProviderAttemptKey.OrgID, RequestID: decision.ProviderAttemptKey.RequestID,
		AttemptID: decision.ProviderAttemptKey.AttemptID,
		Outcome:   billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
	}))
	firstHealth, err := coordinator.MarkSuccess(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, firstHealth.Status)
	var firstHealthAt time.Time
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT health_applied_at
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
   AND health_outcome='success' AND route_evidence_version=$4
   AND route_evidence_hash=$5`, fixture.orgID, identity.RequestID, identity.AttemptID,
		executionevidence.ContractVersionV2, decision.EvidenceHash).Scan(&firstHealthAt))

	replayedHealth, err := coordinator.MarkSuccess(t.Context(), decision)
	require.NoError(t, err)
	require.Equal(t, Applied, replayedHealth.Status)
	var replayedHealthAt time.Time
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT health_applied_at
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
   AND health_outcome='success' AND route_evidence_version=$4
   AND route_evidence_hash=$5`, fixture.orgID, identity.RequestID, identity.AttemptID,
		executionevidence.ContractVersionV2, decision.EvidenceHash).Scan(&replayedHealthAt))
	require.True(t, firstHealthAt.Equal(replayedHealthAt), "exact health replay must not reapply the terminal V2 fence")
}

func TestPublicCoordinatorRollsBackAttemptOnPostFreezeM77AndCredentialDriftIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	tests := []struct {
		name       string
		wantStatus Status
		mutate     func(*testing.T, *publicExecutionFixture)
	}{
		{
			name: "structural qualification revision", wantStatus: Stale,
			mutate: func(t *testing.T, fixture *publicExecutionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE model_deployment_operations
   SET revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.deploymentID, modeloperation.IDResponses)
				require.NoError(t, err)
				var executable bool
				var state string
				require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT public_executable,qualification_state
  FROM model_operation_public_execution_qualifications
 WHERE public_execution_binding_id=$1`, fixture.activeBindingID).Scan(&executable, &state))
				require.False(t, executable)
				require.Equal(t, "structural_qualification_stale", state)
			},
		},
		{
			name: "selected credential disabled", wantStatus: Disabled,
			mutate: func(t *testing.T, fixture *publicExecutionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1,updated_at=NOW()
 WHERE id=$1`, fixture.credentialBindingID)
				require.NoError(t, err)
			},
		},
		{
			name: "selected credential link epoch", wantStatus: Stale,
			mutate: func(t *testing.T, fixture *publicExecutionFixture) {
				_, err := fixture.pool.Exec(t.Context(), `
DELETE FROM credential_binding_deployments
 WHERE binding_id=$1 AND deployment_id=$2 AND connection_id=$3`,
					fixture.credentialBindingID, fixture.deploymentID, fixture.connectionID)
				require.NoError(t, err)
				_, err = fixture.pool.Exec(t.Context(), `
INSERT INTO credential_binding_deployments(binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,clock_timestamp()+INTERVAL '1 second')`,
					fixture.credentialBindingID, fixture.deploymentID, fixture.connectionID)
				require.NoError(t, err)
			},
		},
		{
			name: "selected credential newer own probe", wantStatus: Stale,
			mutate: func(t *testing.T, fixture *publicExecutionFixture) {
				fixture.insertCurrentProbe(t, "newer-own-probe", time.Now().UTC().Add(time.Second))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPublicExecutionFixture(t, pool, strings.ReplaceAll(test.name, " ", "-"))
			plan, route := fixture.plan(t, "post-freeze-"+strings.ReplaceAll(test.name, " ", "-"))
			identity := fixture.freeze(t, "post-freeze-"+strings.ReplaceAll(test.name, " ", "-"))
			coordinator, err := New(fixture.pool, fixture.billing, DefaultConfig())
			require.NoError(t, err)
			prepared, err := preparePublicRequest(plan, route, identity)
			require.NoError(t, err)
			test.mutate(t, fixture)
			prepared, err = coordinator.prepareDispatchCapabilities(prepared)
			require.NoError(t, err)
			decision, retry, err := coordinator.authorizePublicOnce(t.Context(), prepared)
			require.NoError(t, err)
			require.Equal(t, retryNone, retry)
			require.Equal(t, test.wantStatus, decision.Status)
			fixture.requireAttemptCount(t, identity.RequestID, 0)
		})
	}
}

func newPublicExecutionFixture(t *testing.T, pool *pgxpool.Pool, label string) *publicExecutionFixture {
	t.Helper()
	ctx := t.Context()
	suffix := fmt.Sprintf("%s-%d", strings.ReplaceAll(label, "_", "-"), time.Now().UnixNano())
	fixture := &publicExecutionFixture{
		pool: pool, model: "m77-exec-" + suffix, upstreamModel: "upstream-m77-" + suffix,
		releaseSuffix: strings.ReplaceAll(suffix, "-", "_"),
	}
	fixture.orgID = testutil.NewTestOrg(t, pool)
	_, err := pool.Exec(ctx, `
UPDATE orgs SET org_type='standard',updated_at=NOW()
 WHERE org_type='customer' AND id<>$1`, fixture.orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE orgs SET status='active',org_type='customer',parent_id=NULL,updated_at=NOW()
 WHERE id=$1`, fixture.orgID)
	require.NoError(t, err)
	testutil.NewTestWalletCents(t, pool, fixture.orgID, 10000)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users(org_id,username,password_hash,role,status,organization_role)
VALUES ($1,$2,'fixture','platform_admin','active','platform_admin') RETURNING id`,
		fixture.orgID, "m77-exec-owner-"+suffix).Scan(&fixture.userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO api_keys(org_id,name,key_prefix,key_hash,status)
VALUES ($1,'m77-execution-key',$2,$3,'active') RETURNING id`, fixture.orgID,
		"ur-"+hashText("prefix-" + suffix)[7:19], hashText("api-key-" + suffix)[7:]).Scan(&fixture.apiKeyID))
	_, err = pool.Exec(ctx, `
INSERT INTO model_prices(model_name,provider,input_price_cents,output_price_cents)
VALUES ($1,'openai',2,10)`, fixture.model)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry(
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
    context_length,output_limit)
VALUES ($1,$2,$2,'openai','ga',TRUE,128000,8192) RETURNING id`,
		fixture.orgID, fixture.model).Scan(&fixture.publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections(
    name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'openai','https://provider.invalid','openai_compatible','{}','enabled',1)
RETURNING id`, "m77-exec-connection-"+suffix).Scan(&fixture.connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments(
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,priority,routing_weight,max_context,default_max_output,degradation_threshold)
VALUES ($1,$2,$3,$4,'enabled',1,100,100,128000,4096,10)
RETURNING id`, fixture.orgID, fixture.publicModelID, fixture.connectionID,
		fixture.upstreamModel).Scan(&fixture.deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings(connection_id,key_enc,status,state_revision)
VALUES ($1,'cipher:m77-execution','active',1) RETURNING id`, fixture.connectionID).Scan(&fixture.credentialBindingID))
	_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments(binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, fixture.credentialBindingID, fixture.deploymentID, fixture.connectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations(
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','declared','',1,$3,'1.0.0')`, fixture.deploymentID,
		modeloperation.IDResponses, endpointprotocol.ProfileOpenAIResponsesText)
	require.NoError(t, err)
	probeID, probeTestedAt := fixture.insertCurrentProbe(t, "approval", time.Now().UTC())
	result, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='live_probed',evidence_ref='probe:' || $3::bigint::text,
       verified_at=$4,revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND revision=1`, fixture.deploymentID,
		modeloperation.IDResponses, probeID, probeTestedAt)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.RowsAffected())
	_, err = pool.Exec(ctx, `
INSERT INTO model_operation_production_approvals(
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
       operation.revision,operation.revision+1,$2,$3,'M77 coordinator integration fixture',
       'admin.model_operation_production_approval.v1','probe:' || probe.probe_id::text,NOW()
  FROM model_deployment_probes probe
  JOIN model_deployments deployment ON deployment.id=probe.deployment_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=probe.deployment_id AND operation.operation_id=probe.operation_id
  JOIN credential_binding_deployments credential_link
    ON credential_link.binding_id=probe.credential_binding_id
   AND credential_link.deployment_id=probe.deployment_id
   AND credential_link.connection_id=probe.connection_id
 WHERE probe.probe_id=$1`, probeID, fixture.userID, fixture.orgID)
	require.NoError(t, err)
	var qualified bool
	var qualificationState string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT qualified,qualification_state,current_operation_revision
  FROM model_operation_execution_qualifications
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.deploymentID, modeloperation.IDResponses).Scan(
		&qualified, &qualificationState, &fixture.operationRevision,
	))
	require.True(t, qualified)
	require.Equal(t, "production_qualified", qualificationState)
	// The approval probe proves the M75 structural transition from revision 1;
	// dispatch must independently select a current, per-credential probe of the
	// approved operation revision rather than reusing that historical receipt.
	fixture.insertCurrentProbe(t, "dispatch-current", time.Now().UTC())

	fixture.route = runtimechange.DeploymentRoute{
		DeploymentID: fixture.deploymentID, ModelOperationID: string(modeloperation.IDResponses),
		ExpectedConnectionRevision: 1, ExpectedDeploymentRevision: 1,
		ExpectedOperationRevision: fixture.operationRevision,
		ProtocolProfileID:         endpointprotocol.ProfileOpenAIResponsesText, ProtocolProfileVersion: "1.0.0",
		PublicPath: modeloperation.PathResponses, ExecutorContractVersion: modelqualification.ExecutorContractVersion,
		Admission: routingpolicy.AdmissionAllow,
	}
	fixture.manager, err = runtimechange.NewManager(pool, publicExecutionSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	sealed := integrationfixture.AuthorSealedDeploymentRelease(t, pool, fixture.manager, integrationfixture.AtomicRelease{
		PublicModelID: fixture.publicModelID,
		CustomerOrgID: fixture.orgID,
		ActorUserID:   fixture.userID,
		ActorOrgID:    fixture.orgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.deploymentID,
			OperationID:  string(modeloperation.IDResponses),
		}},
		Reason: "M77 coordinator public execution authority: " + fixture.releaseSuffix,
	})
	active := integrationfixture.ActivateInitialDeploymentRelease(t, fixture.manager, integrationfixture.AtomicRelease{
		PublicModelID: fixture.publicModelID,
		CustomerOrgID: fixture.orgID,
		ActorUserID:   fixture.userID,
		ActorOrgID:    fixture.orgID,
	}, sealed)
	fixture.activeBindingID = active.Candidate.DeploymentRoutes[0].PublicExecutionBindingID
	require.Positive(t, fixture.activeBindingID)
	fixture.billing = billing.NewBilling(pool, nil)
	return fixture
}

func (fixture *publicExecutionFixture) insertCurrentProbe(t *testing.T, source string, testedAt time.Time) (int64, time.Time) {
	t.Helper()
	var probeID int64
	var storedAt time.Time
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
INSERT INTO model_deployment_probes(
    deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
    upstream_model,reported_model,connection_revision,deployment_revision,
    operation_revision,public_path,executor_contract_version,https_origin,
    credential_binding_id,credential_state_revision,success,status_code,latency_ms,
    source_kind,source_ref,source_fingerprint,tested_by,tested_at)
VALUES ($1,$2,$3,$4,'1.0.0',$5,$5,1,1,$6,$7,$8,'https://provider.invalid',
        $9,1,TRUE,200,1,$10,$11,$12,$13,$14)
RETURNING probe_id,tested_at`, fixture.deploymentID, fixture.connectionID,
		modeloperation.IDResponses, endpointprotocol.ProfileOpenAIResponsesText,
		fixture.upstreamModel, max(fixture.operationRevision, 1), modeloperation.PathResponses,
		modelqualification.ExecutorContractVersion, fixture.credentialBindingID,
		modelqualification.LiveProbeSourceKind, "m77-coordinator:"+source+":"+fixture.releaseSuffix,
		hashText("m77-coordinator:"+source+":"+fixture.releaseSuffix), fixture.userID, testedAt,
	).Scan(&probeID, &storedAt))
	return probeID, storedAt
}

func (fixture *publicExecutionFixture) createSealedSiblingRelease(t *testing.T) int64 {
	t.Helper()
	sealed := integrationfixture.AuthorSealedDeploymentRelease(t, fixture.pool, fixture.manager, integrationfixture.AtomicRelease{
		PublicModelID: fixture.publicModelID,
		CustomerOrgID: fixture.orgID,
		ActorUserID:   fixture.userID,
		ActorOrgID:    fixture.orgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.deploymentID,
			OperationID:  string(modeloperation.IDResponses),
		}},
		Reason: "same deployment sibling must not authorize selected item: " + fixture.releaseSuffix,
	})
	require.Len(t, sealed.Candidate.DeploymentRoutes, 1)
	return sealed.Candidate.DeploymentRoutes[0].PublicExecutionBindingID
}

func (fixture *publicExecutionFixture) plan(t *testing.T, selection string) (modelrouting.Plan, modelrouting.Route) {
	t.Helper()
	planner, err := modelrouting.NewPlanner(fixture.pool, routingpolicy.Default())
	require.NoError(t, err)
	plan, err := planner.Plan(t.Context(), modelrouting.Request{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.orgID,
		PublicModel: fixture.model, ModelOperationID: string(modeloperation.IDResponses),
		SelectionKey: selection,
	})
	require.NoError(t, err)
	require.Len(t, plan.Routes, 1)
	return plan, plan.Routes[0]
}

func (fixture *publicExecutionFixture) freeze(t *testing.T, suffix string) AttemptIdentity {
	t.Helper()
	requestID := fmt.Sprintf("m77-exec-%d-%s", fixture.deploymentID, suffix)
	result, err := fixture.billing.Freeze(t.Context(), billing.FreezeRequest{
		RequestID: requestID, OrgID: fixture.orgID, APIKeyID: fixture.apiKeyID,
		Model: fixture.model, EstimatedTokens: 1, SafetyFactor: "1", MinFreezeCents: 1,
	})
	require.NoError(t, err)
	require.True(t, result.Success, result.ErrorDetail)
	return AttemptIdentity{
		RequestID: requestID, AttemptID: "attempt-" + suffix,
		OrgID: fixture.orgID, APIKeyID: fixture.apiKeyID,
		RouteSequence: 1, RouteTotal: 1, Endpoint: modeloperation.PathResponses,
	}
}

func (fixture *publicExecutionFixture) requireAttemptCount(t *testing.T, requestID string, want int) {
	t.Helper()
	var count int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `
SELECT COUNT(*) FROM billing_provider_attempts WHERE org_id=$1 AND request_id=$2`,
		fixture.orgID, requestID).Scan(&count))
	require.Equal(t, want, count)
}
