//go:build integration

package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelauthz"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/releaseexecution"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestNativeToolsUpgradePreservesImportedCapabilityProjectionIntegration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.CustomerStandardPricingHead)
	ctx := t.Context()
	dir := testutil.MigrationsDir(t)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	authority := publication.AuthorityV2{
		Environment: "integration", ArtifactMigrationLedger: ledger[:slices.Index(ledger, schemaauthority.CustomerStandardPricingHead)+1],
		Publisher: publication.PublisherIdentityV2{Artifact: publication.ArtifactIdentity{
			Kind: "source", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), Version: "test", PublisherExecutableDigest: strings.Repeat("c", 64),
		}},
		ValidateBinding: func(b publication.DefinitionBindingV2, d modelprofile.Definition) error {
			_, err := qualification.BindDefinition(qualification.Binding{
				OperationID: b.OperationID, CapabilityID: b.CapabilityID, ExecutionProfileID: b.ExecutionProfileID, ExecutionProfileVersion: b.ExecutionProfileVersion,
				ProtocolProfileID: b.ProtocolProfileID, ProtocolProfileVersion: b.ProtocolProfileVersion, ProbeContractID: b.ProbeContractID, ProbeContractVersion: b.ProbeContractVersion,
				ExpectedProvider: b.ExpectedProvider, ExpectedUpstreamModel: b.ExpectedUpstreamModel, PublicPath: b.PublicPath,
			}, d)
			return err
		},
	}
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	wire, err := manifest.CanonicalBytes()
	require.NoError(t, err)
	digest, err := manifest.Digest()
	require.NoError(t, err)
	artifact := "sha256:" + strings.Repeat("a", 64)
	instance := "uri_" + strings.Repeat("b", 32)
	_, err = pool.Exec(ctx, "INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)", artifact, digest, wire)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
VALUES($1,$2,$3,$4,'serving','receiving',clock_timestamp()+interval '1 hour')`, instance, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.CustomerStandardPricingHead)
	require.NoError(t, err)

	plan := func(action publication.PlanRequestV2) publication.PlanV2 {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		result, err := publication.BuildPlanV2(ctx, tx, authority, action)
		require.NoError(t, err)
		return result
	}
	apply := func(action publication.PlanRequestV2) {
		result, err := publication.ApplyV2(ctx, pool, authority, action, plan(action))
		require.NoError(t, err)
		require.Equal(t, "committed", result.Status)
	}
	bundleRequest := func(path, id string) publication.PlanRequestV2 {
		bytes, err := os.ReadFile("../../capability-bundles/" + path)
		require.NoError(t, err)
		require.NoError(t, qualification.ValidateBundleSource(bytes))
		authority.ApprovedBundleDigest = fmt.Sprintf("%x", sha256.Sum256(bytes))
		authority.Publisher.Carrier = &publication.CarrierIdentityV2{Repository: "fixture/repository", Commit: strings.Repeat("f", 40), Tree: strings.Repeat("a", 40), Path: "capability-bundles/" + path, WorkflowRunID: 1, ArtifactID: 2}
		return publication.PlanRequestV2{PublicationID: id, Kind: "delta", BundleBytes: bytes}
	}
	projection := func() string {
		require.NoError(t, publication.VerifyPublicationProjectionV2(ctx, pool))
		p, err := publication.ReadStaticProjectionV2(ctx, pool)
		require.NoError(t, err)
		digest, err := p.Digest()
		require.NoError(t, err)
		return digest
	}

	apply(publication.PlanRequestV2{PublicationID: "m131-fixture-import", Kind: "legacy_import"})
	apply(bundleRequest("qwen-max-first-model.json", "m131-fixture-delta"))
	before := projection()
	require.ErrorContains(t, migrate.RunExistingThrough(ctx, pool, dir, schemaauthority.OpenAIResponsesToolsHead), "M132 requires stopped writers")
	require.Equal(t, before, projection())
	_, err = pool.Exec(ctx, "UPDATE runtime_generations SET state='stopped',ingress_state='absent',revision=revision+1 WHERE instance_id=$1", instance)
	require.NoError(t, err)
	require.ErrorContains(t, migrate.RunRuntime(ctx, pool, dir), "outside this artifact's exact supported runtime ledgers")
	require.NoError(t, migrate.RunExistingThrough(ctx, pool, dir, schemaauthority.OpenAIResponsesToolsHead))
	require.Equal(t, before, projection(), "migration must leave the V2 publication projection intact")
	require.NoError(t, migrate.RunExistingThrough(ctx, pool, dir, schemaauthority.NativeInlineJPEGHead))
	require.Equal(t, before, projection(), "native vision schema must leave the V2 publication projection intact")
	visionDown, err := os.ReadFile(dir + "/" + schemaauthority.NativeInlineJPEGHead + ".down.sql")
	require.NoError(t, err)
	checkVisionDown := func(wantError string) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		_, err = tx.Exec(ctx, strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(visionDown)), "BEGIN;"), "COMMIT;"))
		if wantError != "" {
			require.ErrorContains(t, err, wantError)
			return
		}
		require.NoError(t, err)
		var retained int
		require.NoError(t, tx.QueryRow(ctx, `
SELECT count(*) FROM model_operation_public_execution_contract_extensions
WHERE introduced_migration=$1`, schemaauthority.NativeInlineJPEGHead).Scan(&retained))
		require.Zero(t, retained)
	}
	checkVisionDown("")
	authority.ArtifactMigrationLedger = ledger[:slices.Index(ledger, schemaauthority.NativeInlineJPEGHead)+1]
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
VALUES($1,$2,$3,$4,'serving','receiving',clock_timestamp()+interval '1 hour')`, "uri_"+strings.Repeat("c", 32), artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.NativeInlineJPEGHead)
	require.NoError(t, err)
	request := bundleRequest("native-responses-messages-client-tools.json", "m132-fixture-native-tools")
	bundle, err := publication.DecodeBundleV2(request.BundleBytes)
	require.NoError(t, err)
	bad := bundle
	bad.BindingDelta = append([]publication.DefinitionBindingV2(nil), bundle.BindingDelta...)
	bad.BindingDelta[0].ExpectedUpstreamModel = "another-model"
	badWire, err := bad.CanonicalBytes()
	require.NoError(t, err)
	require.ErrorContains(t, qualification.ValidateBundleSource(badWire), "exact data Profile")
	candidate := plan(request)
	require.Len(t, candidate.DefinitionDigests, 5)
	require.Len(t, candidate.BindingDelta, 20)
	require.Len(t, candidate.SelectorDelta, 20)
	require.Equal(t, "m131-fixture-delta", candidate.ExpectedPredecessor)
	apply(request)
	down, err := os.ReadFile(dir + "/" + schemaauthority.OpenAIResponsesToolsHead + ".down.sql")
	require.NoError(t, err)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, downErr := tx.Exec(ctx, strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;"))
	require.NoError(t, tx.Rollback(ctx))
	require.ErrorContains(t, downErr, "immutable evidence refers to it")
	for _, table := range []string{"model_deployments", "model_capability_probes", "model_capability_routing_approvals", "model_operation_production_approvals"} {
		var count int
		require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count))
		require.Zero(t, count, table)
	}
	require.NotEqual(t, before, projection())
	visionRequest := bundleRequest("native-responses-messages-inline-jpeg.json", "m132-fixture-native-inline-jpeg")
	vision, err := publication.DecodeBundleV2(visionRequest.BundleBytes)
	require.NoError(t, err)
	visionPlan := plan(visionRequest)
	require.Len(t, visionPlan.DefinitionDigests, 5)
	require.Len(t, visionPlan.BindingDelta, 25)
	apply(visionRequest)
	var org, actor, operator, approver int64
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO orgs(name,status,org_type) VALUES($1,'active','customer') RETURNING id", "native-tools-customer").Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,$2,'fixture','org_admin','org_admin','active') RETURNING id", org, "native-tools-customer").Scan(&actor))
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO orgs(name,status,org_type) VALUES($1,'active','platform') RETURNING id", "native-tools-operator").Scan(&operator))
	require.NoError(t, pool.QueryRow(ctx, "INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,$2,'fixture','platform_admin','platform_admin','active') RETURNING id", operator, "native-tools-operator").Scan(&approver))
	customer := canonicalApplicationAuthorizedRequest(org, actor)
	platform := canonicalOperationProbeAuthorizedRequest(operator, org, approver, auth.RolePlatformAdmin)
	for _, raw := range append(bundle.Definitions, vision.Definitions...) {
		definition, err := modelprofile.DecodeDefinition(raw)
		require.NoError(t, err)
		t.Run(definition.Profile().ID+"@"+definition.Profile().Version, func(t *testing.T) {
			testNativeRegisteredOperationAndRelease(t, pool, definition, runtimegeneration.Identity{
				Instance: "uri_" + strings.Repeat("c", 32), Artifact: artifact,
			}, org, customer, platform)
		})
	}
	checkVisionDown("M134 requires stopped writers")
	_, err = pool.Exec(ctx, "UPDATE runtime_generations SET state='stopped',ingress_state='absent',revision=revision+1 WHERE instance_id=$1", "uri_"+strings.Repeat("c", 32))
	require.NoError(t, err)
	checkVisionDown("M134 rollback would remove retained native vision execution evidence")
}

func testNativeRegisteredOperationAndRelease(t *testing.T, pool *pgxpool.Pool, d modelprofile.Definition, identity runtimegeneration.Identity, org int64, customer, platform func(string, string, string) *http.Request) {
	t.Helper()
	p := d.Profile()
	vision := modelprofile.NativeInlineJPEGProfile(p)
	ctx := t.Context()
	upstream, operation := p.UpstreamModelIDs[0], p.Operations[0]
	h := NewHandlerWithAPIKeyHashSecret(pool, "native-tools-fixture", "native-fixture-hash", []byte("0123456789abcdef01234567"), nil)
	base, path, adapter := "https://api.openai.com", "/v1/responses", adapters.TypeOpenAICompatible
	config := json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`)
	if p.Provider == "anthropic" {
		base, path, adapter, config = "https://api.anthropic.com", "/v1/messages", adapters.TypeAnthropicNative, json.RawMessage(`{}`)
	}
	app := createCanonicalApplicationForTest(t, h, customer, canonicalModelApplicationCreateRequest{
		Connection:  canonicalConnectionInput{Name: p.ID + "-" + p.Version, Provider: p.Provider, BaseURL: base, AdapterType: adapter, AdapterConfig: config},
		PublicModel: canonicalPublicModelInput{ModelName: "native-local-" + upstream + "-" + p.Version},
		Deployment:  canonicalDeploymentInput{UpstreamModel: upstream, ProfileDefinitionDigest: d.Digest()},
		Credential:  canonicalCredentialInput{APIKey: "isolated-provider-fixture"},
		Operation:   canonicalOperationInput{OperationID: operation},
	}, http.StatusCreated).Application
	bindingID := app.Credential.Bindings[0].ID
	calls := 0
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, base+path, r.URL.String())
		var body map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.JSONEq(t, fmt.Sprintf("%q", upstream), string(body["model"]))
		if calls <= 2 {
			require.NotContains(t, body, "tools")
			return nativeClientToolHTTPResponse(nativeClientToolFixture(upstream, p.Provider, false, calls, "Ready.")), nil
		}
		if calls == 3 {
			require.Contains(t, body, "tools")
			return nativeClientToolHTTPResponse(nativeClientToolFixture(upstream, p.Provider, true, calls, "")), nil
		}
		if calls == 5 && vision {
			require.Nil(t, r.GetBody)
			return nativeClientToolHTTPResponse(nativeClientToolFixture(upstream, p.Provider, false, calls, nativeJPEGFixtureAnswer(t, p.Provider, body))), nil
		}
		wire, err := json.Marshal(body)
		require.NoError(t, err)
		value := regexp.MustCompile(`UR_[a-f0-9]{16}`).FindString(string(wire))
		require.NotEmpty(t, value)
		return nativeClientToolHTTPResponse(nativeClientToolFixture(upstream, p.Provider, false, calls, value)), nil
	}))
	checkOperationProbeAvailability(t, h, customer, app.Deployment.ID, operation, true, false)
	probe := serveCanonicalOperationProbeForTest(t, h, customer, app.Deployment.ID, operation, bindingID, "", http.StatusOK)
	require.True(t, probe.Success, "%+v", probe)
	require.Equal(t, 1, calls)

	basePath := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s", app.Deployment.ID, operation)
	call := func(requester func(string, string, string) *http.Request, method, suffix, body, capability string, handler http.HandlerFunc) *httptest.ResponseRecorder {
		r := requester(method, basePath+suffix, body)
		r.SetPathValue("deploymentID", strconv.FormatInt(app.Deployment.ID, 10))
		r.SetPathValue("operationID", operation)
		r.SetPathValue("capabilityID", capability)
		response := httptest.NewRecorder()
		handler(response, r)
		return response
	}
	var connectionRevision, deploymentRevision, operationRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT connection.config_revision,deployment.config_revision,operation.revision
  FROM model_deployments deployment
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation ON operation.deployment_id=deployment.id
 WHERE deployment.id=$1 AND operation.operation_id=$2`, app.Deployment.ID, operation).
		Scan(&connectionRevision, &deploymentRevision, &operationRevision))
	exact, err := modelqualification.CompiledBindingFor(operation, p.ProtocolProfile, p.ProtocolProfileVersion)
	require.NoError(t, err)
	opWire, err := json.Marshal(modelOperationProductionApprovalRequest{
		ProbeID: probe.ProbeID, ExpectedConnectionRevision: connectionRevision, ExpectedDeploymentRevision: deploymentRevision,
		ExpectedOperationRevision: operationRevision, ExpectedExecutorContractVersion: exact.ExecutorContractVersion, Reason: "exact native operation integration",
	})
	require.NoError(t, err)
	approved := call(platform, http.MethodPost, "/production-approvals", string(opWire), "", h.ApproveCanonicalModelOperationProduction)
	require.Equal(t, http.StatusCreated, approved.Code, approved.Body.String())
	// Structural approval increments the operation revision. A fresh probe
	// under the approved revision supplies its own dispatch eligibility.
	current := serveCanonicalOperationProbeForTest(t, h, customer, app.Deployment.ID, operation, bindingID, "", http.StatusOK)
	require.True(t, current.Success && current.Current && !current.OperationAdvanced, "%+v", current)
	require.Equal(t, 2, calls)

	planResponse := call(customer, http.MethodGet, "/capability-probe-plan?credential_binding_id="+strconv.FormatInt(bindingID, 10), "", "", h.GetCanonicalCapabilityProbePlan)
	require.Equal(t, http.StatusOK, planResponse.Code, planResponse.Body.String())
	var plan capabilityProbePlan
	require.NoError(t, json.Unmarshal(planResponse.Body.Bytes(), &plan))
	require.Len(t, plan.Groups, 1)
	group := plan.Groups[0]
	require.Equal(t, 2, group.ProviderRequests)
	require.Len(t, group.CapabilityIDs, 4)
	assertPlannedCount := func(capability string, requests int) {
		t.Helper()
		current := call(customer, http.MethodGet, "/capabilities/"+capability+"/qualification", "", capability, h.GetCanonicalModelCapabilityQualification)
		require.Equal(t, http.StatusOK, current.Code, current.Body.String())
		var got modelCapabilityQualificationView
		require.NoError(t, json.Unmarshal(current.Body.Bytes(), &got))
		require.True(t, got.ProbeAvailable, "%+v", got)
		require.Equal(t, requests, got.PlannedProviderRequests)
	}
	assertPlannedCount(group.CapabilityIDs[0], 2)
	groupWire, err := json.Marshal(capabilityProbeGroupRequest{CredentialBindingID: bindingID, PlanDigest: group.PlanDigest, CapabilityIDs: group.CapabilityIDs})
	require.NoError(t, err)
	groupResponse := call(customer, http.MethodPost, "/capability-probe-groups", string(groupWire), "", h.ProbeCanonicalCapabilityGroup)
	require.Equal(t, http.StatusOK, groupResponse.Code, groupResponse.Body.String())
	var batch capabilityProbeGroupResult
	require.NoError(t, json.Unmarshal(groupResponse.Body.Bytes(), &batch))
	require.Len(t, batch.Results, 4)
	require.Equal(t, 4, calls)
	var approvals []int64
	for _, fact := range batch.Results {
		require.True(t, fact.Success, "%+v", fact)
		wire, err := json.Marshal(modelCapabilityApprovalRequest{CapabilityProbeID: fact.CapabilityProbeID, Reason: "exact two-stage tool fixture"})
		require.NoError(t, err)
		result := call(platform, http.MethodPost, "/capabilities/"+fact.CapabilityID+"/routing-approvals", string(wire), fact.CapabilityID, h.ApproveCanonicalModelCapabilityRouting)
		require.Equal(t, http.StatusCreated, result.Code, result.Body.String())
		var approval modelCapabilityApprovalResult
		require.NoError(t, json.Unmarshal(result.Body.Bytes(), &approval))
		approvals = append(approvals, approval.ApprovalID)
	}
	if vision {
		require.Contains(t, plan.SeparateCapabilities, "vision")
		assertPlannedCount("vision", 1)
		visionProbe := call(customer, http.MethodPost, "/capabilities/vision/probes", fmt.Sprintf(`{"credential_binding_id":%d}`, bindingID), "vision", h.ProbeCanonicalModelCapability)
		require.Equal(t, http.StatusOK, visionProbe.Code, visionProbe.Body.String())
		var fact modelCapabilityProbeResult
		require.NoError(t, json.Unmarshal(visionProbe.Body.Bytes(), &fact))
		require.True(t, fact.Success && fact.ReportedModel == upstream, "%+v", fact)
		wire, err := json.Marshal(modelCapabilityApprovalRequest{CapabilityProbeID: fact.CapabilityProbeID, Reason: "exact inline JPEG fixture"})
		require.NoError(t, err)
		result := call(platform, http.MethodPost, "/capabilities/vision/routing-approvals", string(wire), "vision", h.ApproveCanonicalModelCapabilityRouting)
		require.Equal(t, http.StatusCreated, result.Code, result.Body.String())
		var approval modelCapabilityApprovalResult
		require.NoError(t, json.Unmarshal(result.Body.Bytes(), &approval))
		approvals = append(approvals, approval.ApprovalID)
	}
	router, err := gateway.NewRouterWithPolicy(pool, routingpolicy.Default())
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, routingpolicy.Default())
	require.NoError(t, err)
	h.SetRuntimeChangeManager(manager)
	frozen, _ := serveCanonicalDeploymentReleaseAuthoring(t, h, customer, canonicalDeploymentReleaseAuthoringRequest{
		PublicModelID: app.PublicModel.ID, Baseline: runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial},
		Selections:              []runtimechange.DeploymentReleaseSelection{{DeploymentID: app.Deployment.ID, OperationID: operation}},
		ProfileDefinitionDigest: d.Digest(), PublicContextLimit: 64000, PublicOutputLimit: 4096,
		CapabilityApprovalIDs: approvals, Reason: "seal exact native tool fixtures",
	}, "native-freeze-"+p.ID+"-"+p.Version, http.StatusCreated)
	snapshot, err := releaseexecution.ReadSnapshot(ctx, pool, frozen.Release.ID)
	require.NoError(t, err)
	require.NoError(t, snapshot.ValidateDefinition(d))
	require.Len(t, snapshot.CapabilityPins, len(approvals))
	require.NoError(t, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()).Require(schemaauthority.LatestSupportedMigration, snapshot.Requirements))
	_, _ = serveCanonicalLifecycleMutation(t, h, customer, frozen.Release.ReleaseRef, "activate-initial",
		fmt.Sprintf(`{"expected_release_version":%d,"reason":"native tool fixture"}`, frozen.Release.Version), "native-activate-"+p.ID+"-"+p.Version, h.ActivateInitialCanonicalDeploymentLifecycle, http.StatusOK)
	if vision {
		require.Equal(t, 5, calls, "qualification reads and release writes never dispatch")
	} else {
		require.Equal(t, 4, calls, "qualification reads and release writes never dispatch")
	}
	testNativeToolGatewayRoundTrip(t, h, pool, org, app.PublicModel.ModelName, p, path, base, frozen.Release.ID, identity)
}

func testNativeToolGatewayRoundTrip(t *testing.T, h *Handler, pool *pgxpool.Pool, org int64, model string, p modelprofile.Profile, path, origin string, releaseID int64, identity runtimegeneration.Identity) {
	t.Helper()
	ctx := t.Context()
	var keyID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,name,key_prefix,key_hash,status)
VALUES($1,'native-fixture',substr(md5($2),1,12),md5($2)||md5($2),'active') RETURNING id`, org, model).Scan(&keyID))
	bill := billing.NewBilling(pool, nil).WithMode(billing.BillingModeCostObservation)
	journal, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	bill.SetOutbox(journal)
	for _, event := range []string{outbox.EventBillingUsagePrepared, outbox.EventBillingSettled, outbox.EventBillingUsagePreparedV1, outbox.EventBillingSettledV1} {
		require.NoError(t, journal.RegisterHandler(event, bill.ProcessUsageJournalEvent))
	}
	planner, err := modelrouting.NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	coordinator, err := modelexecution.New(pool, bill, modelexecution.DefaultConfig())
	require.NoError(t, err)
	relay := gateway.NewRelay(pool, nil, nil, bill, nil, h.encKey)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()))
	guard, err := guardrail.NewService(pool, filter.NewDFAFilter(nil), []byte("native-tool-guardrail-fixture"), guardrail.DefaultOptions())
	require.NoError(t, err)
	relay.SetGuardrail(guard)
	require.NoError(t, relay.SetBillingFreezePolicy("1", 1))
	calls := 0
	const echo = "UR_0123456789abcdef"
	relay.SetProviderHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, origin+path, r.URL.String())
		var body map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.JSONEq(t, fmt.Sprintf("%q", p.UpstreamModelIDs[0]), string(body["model"]))
		if calls == 2 {
			wire, err := json.Marshal(body)
			require.NoError(t, err)
			require.Contains(t, string(wire), echo)
			require.Contains(t, string(wire), "opaque-"+p.Provider)
		}
		answer := echo
		if calls == 3 {
			answer = "Ready."
		}
		if calls == 4 {
			require.Nil(t, r.GetBody, "the single-attempt provider POST must not be replayable")
			answer = nativeJPEGFixtureAnswer(t, p.Provider, body)
		}
		return nativeClientToolHTTPResponse(nativeClientToolFixture(p.UpstreamModelIDs[0], p.Provider, calls == 1, calls, answer)), nil
	}))
	invoke := func(body []byte, id string, single bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
		requestCtx := runtimegeneration.WithIdentity(r.Context(), identity)
		requestCtx = context.WithValue(requestCtx, middleware.CtxOrgID, org)
		requestCtx = context.WithValue(requestCtx, middleware.CtxKeyID, keyID)
		requestCtx = context.WithValue(requestCtx, middleware.CtxAPIKey, &models.ApiKey{ID: keyID, OrgID: org, Status: "active"})
		requestCtx = context.WithValue(requestCtx, middleware.CtxModelAlias, model)
		r = r.WithContext(modelauthz.WithDecision(requestCtx, modelauthz.Decision{
			ContractVersion: modelauthz.ContractVersion, Allowed: true, Endpoint: path, RequestedModel: model, ResolvedModel: model, RuntimeMutation: "none",
		}))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(middleware.IdempotencyKeyHeader, model+"-"+id)
		if single {
			r.Header.Set(middleware.ChatUpstreamAttemptsHeader, "1")
		}
		operation := modeloperation.IDResponses
		if p.Provider == "anthropic" {
			operation = modeloperation.IDMessages
		}
		w := httptest.NewRecorder()
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { relay.HandlePublicNonStream(w, r, operation) })
		guardedReplay := middleware.IdempotencyGuard(middleware.NewPostgresIdempotencyStore(pool), "native-idempotency-fingerprint-fixture", handler)
		retained := middleware.StrictJSONIngress(middleware.UpstreamAttemptPolicy(guardedReplay))
		guarded := payloadpolicy.RequestMiddlewareWithMax(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), 8*payloadpolicy.MiB, middleware.GetOrgIDFromContext)(retained)
		middleware.OperationIdentityWithSecret("native-operations-fixture")(guarded).ServeHTTP(w, r)
		return w
	}
	firstBody, err := nativeToolProbeRequest(p, model)
	require.NoError(t, err)
	first := invoke(firstBody, "native-gateway-first", false)
	require.Equal(t, http.StatusOK, first.Code, "provider fixture saw %d requests: %s", calls, first.Body.String())
	next, err := nativeToolProbeContinuation(p, firstBody, first.Body.Bytes(), echo)
	require.NoError(t, err)
	second := invoke(next, "native-gateway-result", false)
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.True(t, nativeToolProbeAcknowledged(p, second.Body.Bytes(), echo))
	require.Equal(t, 2, calls)
	responses := []*httptest.ResponseRecorder{first, second}
	image, answer, err := qualification.RandomInlineJPEGChallenge()
	require.NoError(t, err)
	field, maxField := "messages", "max_tokens"
	parts := []map[string]any{
		{"type": "text", "text": qualification.InlineJPEGChallengeQuestion},
		{"type": "image", "source": map[string]string{"type": "base64", "media_type": "image/jpeg", "data": image}},
	}
	var text any = []map[string]string{{"role": "user", "content": "Say Ready."}}
	if p.Provider == "openai" {
		field, maxField = "input", "max_output_tokens"
		text = "Say Ready."
		parts[0]["type"] = "input_text"
		parts[1] = map[string]any{"type": "input_image", "image_url": "data:image/jpeg;base64," + image}
	}
	imageEnvelope := map[string]any{"model": model, maxField: 512, field: []map[string]any{{"role": "user", "content": parts}}}
	imageBody, err := json.Marshal(imageEnvelope)
	require.NoError(t, err)
	if modelprofile.NativeInlineJPEGProfile(p) {
		textBody, err := json.Marshal(map[string]any{"model": model, maxField: 512, field: text})
		require.NoError(t, err)
		plain := invoke(textBody, "native-gateway-plain", false)
		require.Equal(t, http.StatusOK, plain.Code, plain.Body.String())
		require.True(t, nativeToolProbeAcknowledged(p, plain.Body.Bytes(), "Ready."))
		visual := invoke(imageBody, "native-gateway-visual", true)
		require.Equal(t, http.StatusOK, visual.Code, visual.Body.String())
		require.Equal(t, "1", visual.Header().Get(middleware.ChatUpstreamAttemptsHeader))
		require.True(t, nativeToolProbeAcknowledged(p, visual.Body.Bytes(), answer))
		conflict := invoke(imageBody, "native-gateway-visual", false)
		require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
		imageEnvelope["stream"] = true
		streamBody, err := json.Marshal(imageEnvelope)
		require.NoError(t, err)
		stream := invoke(streamBody, "native-gateway-stream-single", true)
		require.Equal(t, http.StatusBadRequest, stream.Code, stream.Body.String())
		require.Empty(t, stream.Header().Get(middleware.ChatUpstreamAttemptsHeader))
		delete(imageEnvelope, "stream")
		responses = append(responses, plain, visual)
		imageEnvelope["tools"] = []any{}
		mixed, err := json.Marshal(imageEnvelope)
		require.NoError(t, err)
		bad := invoke(mixed, "native-gateway-mixed", false)
		require.Equal(t, http.StatusBadRequest, bad.Code, bad.Body.String())
		require.Equal(t, 4, calls)
	} else {
		legacy := invoke(imageBody, "old-native-gateway-image", false)
		require.Equal(t, http.StatusBadRequest, legacy.Code, legacy.Body.String())
		require.Equal(t, 2, calls)
	}
	_, err = journal.DrainOnce(ctx)
	require.NoError(t, err)
	for _, response := range responses {
		requestID := response.Header().Get("X-Request-ID")
		require.NotEmpty(t, requestID)
		var settled int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT count(*) FROM billing_provider_attempts
WHERE request_id=$1 AND state='settled' AND route_runtime_release_id=$2 AND execution_snapshot_hash<>''`, requestID, releaseID).Scan(&settled))
		require.Equal(t, 1, settled)
	}
}

func nativeClientToolHTTPResponse(body string) *http.Response {
	response := canonicalProbeHTTPResponse(http.StatusOK, body)
	response.Header.Set("Content-Type", "application/json")
	return response
}
