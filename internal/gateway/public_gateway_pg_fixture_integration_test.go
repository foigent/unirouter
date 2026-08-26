//go:build integration

package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

type publicGatewayPGSnapshotBuilder struct{}

func (publicGatewayPGSnapshotBuilder) BuildRuntimeChangeSnapshots(
	context.Context,
	string,
	[]routingpolicy.ChannelOverride,
	[]routingpolicy.ChannelOverride,
) (governancesim.RouteSnapshot, governancesim.RouteSnapshot, error) {
	return governancesim.RouteSnapshot{}, governancesim.RouteSnapshot{}, nil
}

// publicGatewayPGSuite owns the customer-scoped state shared by the focused
// public gateway integration test. The caller owns the one migrated database;
// adding routes never creates another template or silently changes customer.
type publicGatewayPGSuite struct {
	pool        *pgxpool.Pool
	manager     *runtimechange.Manager
	billing     *billing.Billing
	planner     *modelrouting.Planner
	coordinator *modelexecution.Coordinator

	orgID, userID int64
	apiKeyIDs     []int64
	encryptionKey []byte
	origin        string
	fixtureSuffix string
}

// publicGatewayPGFixture is one exact Responses or Messages route. It embeds
// the suite so the real planner, coordinator, billing ledger, wallet, and API
// key identities remain shared across independently mutable route scenarios.
type publicGatewayPGFixture struct {
	*publicGatewayPGSuite

	model, upstreamModel, providerSecret string
	provider, adapterType, origin        string
	publicModelID, connectionID          int64
	deploymentID, credentialBindingID    int64
	operationRevision, activeBindingID   int64
	operation                            modeloperation.ID
	identity                             endpointprotocol.PublicContractIdentity
	route                                runtimechange.DeploymentRoute
	releaseSuffix                        string
}

type publicGatewayPGExecutionProfile struct {
	id, version            string
	baseURL, upstreamModel string
	context, output        int
	capabilities           []string
}

func newPublicGatewayPGSuite(t *testing.T, pool *pgxpool.Pool, label string) *publicGatewayPGSuite {
	t.Helper()
	require.NotNil(t, pool)

	ctx := t.Context()
	suffix := publicGatewayPGUniqueSuffix(label)
	suite := &publicGatewayPGSuite{
		pool:          pool,
		encryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		origin:        "https://provider.example",
		fixtureSuffix: suffix,
	}
	suite.orgID = testutil.NewTestOrg(t, pool)
	_, err := pool.Exec(ctx, `
UPDATE orgs SET org_type='standard',updated_at=NOW()
 WHERE org_type='customer' AND id<>$1`, suite.orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE orgs SET status='active',org_type='customer',parent_id=NULL,updated_at=NOW()
 WHERE id=$1`, suite.orgID)
	require.NoError(t, err)
	testutil.NewTestWalletCents(t, pool, suite.orgID, 1_000_000)

	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users(org_id,username,password_hash,role,status,organization_role)
VALUES ($1,$2,'fixture','platform_admin','active','platform_admin') RETURNING id`,
		suite.orgID, "m77-gateway-owner-"+suffix).Scan(&suite.userID))

	for index := 1; index <= 2; index++ {
		digest := publicGatewayPGHash(fmt.Sprintf("api-key-%s-%d", suffix, index))
		var apiKeyID int64
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO api_keys(org_id,name,key_prefix,key_hash,status)
VALUES ($1,$2,$3,$4,'active') RETURNING id`, suite.orgID,
			fmt.Sprintf("m77-gateway-key-%d", index), "ur-"+digest[:17], digest,
		).Scan(&apiKeyID))
		suite.apiKeyIDs = append(suite.apiKeyIDs, apiKeyID)
	}

	suite.manager, err = runtimechange.NewManager(pool, publicGatewayPGSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	suite.billing = billing.NewBilling(pool, nil)
	suite.planner, err = modelrouting.NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	suite.coordinator, err = modelexecution.New(pool, suite.billing, modelexecution.DefaultConfig())
	require.NoError(t, err)
	return suite
}

// newPublicGatewayPGFixture is a convenience for a one-route scenario. Tests
// that need several routes under one customer should create one suite and call
// suite.newRoute for each unique label.
func newPublicGatewayPGFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	label string,
	operation modeloperation.ID,
) *publicGatewayPGFixture {
	t.Helper()
	suite := newPublicGatewayPGSuite(t, pool, label)
	return suite.newRoute(t, label, operation)
}

func (suite *publicGatewayPGSuite) newRoute(
	t *testing.T,
	label string,
	operation modeloperation.ID,
) *publicGatewayPGFixture {
	return suite.newRouteWithExecutionProfile(t, label, operation, publicGatewayPGExecutionProfile{})
}

func (suite *publicGatewayPGSuite) newRouteWithExecutionProfile(
	t *testing.T,
	label string,
	operation modeloperation.ID,
	executionProfile publicGatewayPGExecutionProfile,
) *publicGatewayPGFixture {
	t.Helper()
	require.NotNil(t, suite)
	require.NotNil(t, suite.pool)

	identity, err := modelqualification.CompiledPublicBinding(string(operation))
	require.NoError(t, err)
	if executionProfile.id != "" && executionProfile.id != modelprofile.UnverifiedProfileID {
		profile, ok := modelprofile.Lookup(executionProfile.id, executionProfile.version)
		require.True(t, ok, "exact execution profile must exist")
		binding, bindingErr := modelqualification.CompiledBindingFor(
			string(operation), profile.ProtocolProfile, profile.ProtocolProfileVersion,
		)
		require.NoError(t, bindingErr)
		identity, err = modelqualification.CompiledPublicBindingFor(binding)
		require.NoError(t, err)
	}
	require.Contains(t, []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages}, operation)

	suffix := publicGatewayPGUniqueSuffix(suite.fixtureSuffix + "-" + label)
	fixture := &publicGatewayPGFixture{
		publicGatewayPGSuite: suite,
		model:                "m77-gateway-" + suffix,
		upstreamModel:        "upstream-m77-" + suffix,
		providerSecret:       "provider-secret-" + suffix,
		origin:               suite.origin,
		operation:            operation,
		identity:             identity,
		releaseSuffix:        strings.ReplaceAll(suffix, "-", "_"),
	}
	switch operation {
	case modeloperation.IDResponses:
		fixture.provider = "openai"
		fixture.adapterType = adapters.TypeOpenAICompatible
	case modeloperation.IDMessages:
		fixture.provider = "anthropic"
		fixture.adapterType = adapters.TypeAnthropicNative
	default:
		t.Fatalf("unsupported public gateway operation %q", operation)
	}
	if executionProfile.baseURL != "" {
		fixture.origin = executionProfile.baseURL
	}
	if executionProfile.upstreamModel != "" {
		fixture.upstreamModel = executionProfile.upstreamModel
	}

	ctx := t.Context()
	_, err = suite.pool.Exec(ctx, `
INSERT INTO model_prices(model_name,provider,input_price_cents,output_price_cents)
VALUES ($1,$2,2,10)`, fixture.model, fixture.provider)
	require.NoError(t, err)
	if executionProfile.id == "" {
		executionProfile.id = modelprofile.UnverifiedProfileID
		executionProfile.version = modelprofile.UnverifiedProfileVersion
	}
	if executionProfile.context <= 0 {
		executionProfile.context = 128000
	}
	if executionProfile.output <= 0 {
		executionProfile.output = 8192
	}
	require.NoError(t, suite.pool.QueryRow(ctx, `
INSERT INTO model_registry(
    org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
    context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,$2,$2,$3,'ga',TRUE,$4,$5,$6,$7) RETURNING id`,
		suite.orgID, fixture.model, fixture.provider, executionProfile.context,
		executionProfile.output, executionProfile.id, executionProfile.version,
	).Scan(&fixture.publicModelID))
	require.NoError(t, suite.pool.QueryRow(ctx, `
INSERT INTO provider_connections(
    name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,$2,$3,$4,'{}','enabled',1)
RETURNING id`, "m77-gateway-connection-"+suffix, fixture.provider, fixture.origin,
		fixture.adapterType).Scan(&fixture.connectionID))
	require.NoError(t, suite.pool.QueryRow(ctx, `
INSERT INTO model_deployments(
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
    config_revision,priority,routing_weight,max_context,default_max_output,degradation_threshold)
VALUES ($1,$2,$3,$4,'enabled',1,100,100,128000,4096,10)
RETURNING id`, suite.orgID, fixture.publicModelID, fixture.connectionID,
		fixture.upstreamModel).Scan(&fixture.deploymentID))

	encryptedSecret, err := auth.EncryptKey([]byte(fixture.providerSecret), suite.encryptionKey)
	require.NoError(t, err)
	require.NoError(t, suite.pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings(connection_id,key_enc,status,state_revision)
VALUES ($1,$2,'active',1) RETURNING id`, fixture.connectionID, encryptedSecret,
	).Scan(&fixture.credentialBindingID))
	_, err = suite.pool.Exec(ctx, `
INSERT INTO credential_binding_deployments(binding_id,deployment_id,connection_id)
VALUES ($1,$2,$3)`, fixture.credentialBindingID, fixture.deploymentID, fixture.connectionID)
	require.NoError(t, err)
	_, err = suite.pool.Exec(ctx, `
INSERT INTO model_deployment_operations(
    deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
    protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','declared','',1,$3,$4)`, fixture.deploymentID,
		operation, identity.ProfileID, identity.ProfileVersion)
	require.NoError(t, err)

	probeID, probeTestedAt := fixture.insertCurrentProbe(t, "approval", time.Now().UTC())
	result, err := suite.pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='live_probed',evidence_ref='probe:' || $3::bigint::text,
       verified_at=$4,revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND revision=1`, fixture.deploymentID,
		operation, probeID, probeTestedAt)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.RowsAffected())

	_, err = suite.pool.Exec(ctx, `
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
       operation.revision,operation.revision+1,$2,$3,'M77 public gateway integration fixture',
       'admin.model_operation_production_approval.v1','probe:' || probe.probe_id::text,NOW()
  FROM model_deployment_probes probe
  JOIN model_deployments deployment ON deployment.id=probe.deployment_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=probe.deployment_id AND operation.operation_id=probe.operation_id
  JOIN credential_binding_deployments credential_link
    ON credential_link.binding_id=probe.credential_binding_id
   AND credential_link.deployment_id=probe.deployment_id
   AND credential_link.connection_id=probe.connection_id
 WHERE probe.probe_id=$1`, probeID, suite.userID, suite.orgID)
	require.NoError(t, err)

	var qualified bool
	var qualificationState string
	require.NoError(t, suite.pool.QueryRow(ctx, `
SELECT qualified,qualification_state,current_operation_revision
  FROM model_operation_execution_qualifications
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.deploymentID, operation).Scan(
		&qualified, &qualificationState, &fixture.operationRevision,
	))
	require.True(t, qualified)
	require.Equal(t, "production_qualified", qualificationState)

	// Approval proves the historical structural transition. Dispatch requires
	// a distinct current probe for the exact approved operation/credential epoch.
	fixture.insertCurrentProbe(t, "dispatch-current", time.Now().UTC())
	for _, capability := range executionProfile.capabilities {
		_, err = suite.pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities(
    deployment_id,operation_id,capability_id,support_state,evidence_state,
    evidence_ref,verified_at,revision)
VALUES ($1,$2,$3,'enabled','production_approved','integration:exact-profile',NOW(),1)`,
			fixture.deploymentID, operation, capability)
		require.NoError(t, err)
	}

	fixture.route = runtimechange.DeploymentRoute{
		DeploymentID: fixture.deploymentID, ModelOperationID: string(operation),
		ExpectedConnectionRevision: 1, ExpectedDeploymentRevision: 1,
		ExpectedOperationRevision: fixture.operationRevision,
		ProtocolProfileID:         identity.ProfileID, ProtocolProfileVersion: identity.ProfileVersion,
		PublicPath: identity.PublicPath, ExecutorContractVersion: identity.ExecutorContractVersion,
		Admission: routingpolicy.AdmissionAllow,
	}
	sealed := integrationfixture.AuthorSealedDeploymentRelease(t, suite.pool, suite.manager, integrationfixture.AtomicRelease{
		PublicModelID: fixture.publicModelID,
		CustomerOrgID: suite.orgID,
		ActorUserID:   suite.userID,
		ActorOrgID:    suite.orgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.deploymentID,
			OperationID:  string(operation),
		}},
		Reason: "M77 public gateway execution authority fixture",
	})
	active := integrationfixture.ActivateInitialDeploymentRelease(t, suite.manager, integrationfixture.AtomicRelease{
		PublicModelID: fixture.publicModelID,
		CustomerOrgID: suite.orgID,
		ActorUserID:   suite.userID,
		ActorOrgID:    suite.orgID,
	}, sealed)
	require.Len(t, active.Candidate.DeploymentRoutes, 1)
	fixture.activeBindingID = active.Candidate.DeploymentRoutes[0].PublicExecutionBindingID
	require.Positive(t, fixture.activeBindingID)
	fixture.route = active.Candidate.DeploymentRoutes[0]
	return fixture
}

func (fixture *publicGatewayPGFixture) insertCurrentProbe(
	t *testing.T,
	source string,
	testedAt time.Time,
) (int64, time.Time) {
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
VALUES ($1,$2,$3,$4,$5,$6,$6,1,1,$7,$8,$9,$10,
        $11,1,TRUE,200,1,$12,$13,$14,$15,$16)
RETURNING probe_id,tested_at`, fixture.deploymentID, fixture.connectionID,
		fixture.operation, fixture.identity.ProfileID, fixture.identity.ProfileVersion,
		fixture.upstreamModel, max(fixture.operationRevision, 1), fixture.identity.PublicPath,
		fixture.identity.ExecutorContractVersion, fixture.origin, fixture.credentialBindingID,
		modelqualification.LiveProbeSourceKind,
		"m77-gateway:"+source+":"+fixture.releaseSuffix,
		"sha256:"+publicGatewayPGHash("m77-gateway:"+source+":"+fixture.releaseSuffix),
		fixture.userID, testedAt,
	).Scan(&probeID, &storedAt))
	return probeID, storedAt
}

func (fixture *publicGatewayPGFixture) plan(
	t *testing.T,
	selection string,
) (modelrouting.Plan, modelrouting.Route) {
	t.Helper()
	plan, err := fixture.planner.Plan(t.Context(), modelrouting.Request{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.orgID,
		PublicModel: fixture.model, ModelOperationID: string(fixture.operation),
		SelectionKey: selection,
	})
	require.NoError(t, err)
	require.Len(t, plan.Routes, 1)
	require.NotNil(t, plan.Routes[0].PublicEvidence)
	require.Equal(t, fixture.activeBindingID, plan.Routes[0].PublicEvidence.PublicExecutionBindingID)
	return plan, plan.Routes[0]
}

func (fixture *publicGatewayPGFixture) newOrchestrator(
	doer PublicNonStreamHTTPDoer,
	guard publicNonStreamGuard,
) *publicNonStreamOrchestrator {
	return fixture.newOrchestratorWithCoordinator(fixture.coordinator, doer, guard)
}

func (fixture *publicGatewayPGFixture) newOrchestratorWithCoordinator(
	coordinator publicModelExecutionCoordinator,
	doer PublicNonStreamHTTPDoer,
	guard publicNonStreamGuard,
) *publicNonStreamOrchestrator {
	return &publicNonStreamOrchestrator{
		mode: modelrouting.ModeCanonical, planner: fixture.planner, coordinator: coordinator,
		billing: fixture.billing, registry: endpointprotocol.NewPublicNonStreamRegistry(),
		decrypt: auth.DecryptKey, encryptionKey: fixture.encryptionKey,
		doer: doer, guard: guard, safetyFactor: "1", minFreeze: 1,
	}
}

func (fixture *publicGatewayPGFixture) mutateStructuralRevision(ctx context.Context) error {
	result, err := fixture.pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.deploymentID, fixture.operation)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("mutate structural revision: affected %d rows", result.RowsAffected())
	}
	return nil
}

func (fixture *publicGatewayPGFixture) mutateCredentialDisabled(ctx context.Context) error {
	result, err := fixture.pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1,updated_at=NOW()
 WHERE id=$1`, fixture.credentialBindingID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("mutate credential: affected %d rows", result.RowsAffected())
	}
	return nil
}

func publicGatewayPGUniqueSuffix(label string) string {
	clean := strings.ToLower(strings.TrimSpace(label))
	var builder strings.Builder
	for _, value := range clean {
		switch {
		case value >= 'a' && value <= 'z', value >= '0' && value <= '9':
			builder.WriteRune(value)
		case builder.Len() > 0 && builder.String()[builder.Len()-1] != '-':
			builder.WriteByte('-')
		}
		if builder.Len() >= 18 {
			break
		}
	}
	prefix := strings.Trim(builder.String(), "-")
	if prefix == "" {
		prefix = "route"
	}
	nonce := publicGatewayPGHash(fmt.Sprintf("%s:%d", label, time.Now().UnixNano()))[:12]
	return prefix + "-" + nonce
}

func publicGatewayPGHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
