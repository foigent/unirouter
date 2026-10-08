//go:build integration

package admin

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/gateway"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/releaseexecution"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCurrentDeepSeekFlashDataFirstHTTPPublicationQualificationActivationIntegration(t *testing.T) {
	// A dedicated fixture DSN keeps the runtime role distinct from CI's
	// administrator. Install through the exact current head in its own database
	// rather than changing the package-wide migrated template's DSN.
	if dsn := os.Getenv("CURRENT_FLASH_TEST_DATABASE_URL"); dsn != "" {
		t.Setenv("TEST_DATABASE_URL", dsn)
	}
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.LatestSupportedMigration)
	ctx := t.Context()
	var super bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname=current_user`).Scan(&super))
	require.False(t, super, "this acceptance test requires an isolated non-superuser runtime role")
	wire, err := os.ReadFile("../../capability-bundles/deepseek-v41-flash.json")
	require.NoError(t, err)
	require.NoError(t, qualification.ValidateBundleSource(wire))
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	ledger, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	authority := publication.AuthorityV2{Environment: "integration", ArtifactMigrationLedger: ledger,
		Publisher: publication.PublisherIdentityV2{Artifact: publication.ArtifactIdentity{Kind: "source", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), Version: "1.9.6-local-fixture", PublisherExecutableDigest: strings.Repeat("c", 64)}},
		ValidateBinding: func(row publication.DefinitionBindingV2, d modelprofile.Definition) error {
			_, e := qualification.BindDefinition(qualification.Binding{OperationID: row.OperationID, CapabilityID: row.CapabilityID, ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion, ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion, ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath}, d)
			return e
		}}
	// The materializer requires a proved local receiving generation even for a
	// test publication. All identities below are explicitly synthetic fixtures.
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	mw, err := manifest.CanonicalBytes()
	require.NoError(t, err)
	md, err := manifest.Digest()
	require.NoError(t, err)
	artifact := "sha256:" + strings.Repeat("e", 64)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, artifact, md, mw)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at) VALUES($1,$2,$3,$4,'serving','receiving',clock_timestamp()+interval '1 hour')`, "uri_"+strings.Repeat("e", 32), artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.LatestSupportedMigration)
	require.NoError(t, err)
	apply := func(request publication.PlanRequestV2) {
		tx, e := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		require.NoError(t, e)
		plan, e := publication.BuildPlanV2(ctx, tx, authority, request)
		require.NoError(t, e)
		require.NoError(t, tx.Rollback(ctx))
		result, e := publication.ApplyV2(ctx, pool, authority, request, plan)
		require.NoError(t, e)
		require.Equal(t, "committed", result.Status)
	}
	apply(publication.PlanRequestV2{PublicationID: "flash-import-fixture", Kind: "legacy_import"})
	var oldBindings int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM model_capability_probe_contract_bindings WHERE execution_profile_id='deepseek.v4-flash.api'`).Scan(&oldBindings))
	authority.ApprovedBundleDigest = fmt.Sprintf("%x", sha256.Sum256(wire))
	authority.Publisher.Carrier = &publication.CarrierIdentityV2{Repository: "fixture/repository", Commit: strings.Repeat("f", 40), Tree: strings.Repeat("a", 40), Path: "capability-bundles/deepseek-v41-flash.json", WorkflowRunID: 1, ArtifactID: 2}
	apply(publication.PlanRequestV2{PublicationID: "flash-v41-fixture", Kind: "delta", BundleBytes: wire})
	require.NoError(t, publication.VerifyPublicationProjectionV2(ctx, pool))
	var retainedBindings int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM model_capability_probe_contract_bindings WHERE execution_profile_id='deepseek.v4-flash.api'`).Scan(&retainedBindings))
	require.Equal(t, oldBindings, retainedBindings)
	var org, actor, operatorOrg, approver int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('flash-local','active','customer') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'flash-local','fixture','org_admin','org_admin','active') RETURNING id`, org).Scan(&actor))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('flash-review-local','active','platform') RETURNING id`).Scan(&operatorOrg))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'flash-review-local','fixture','platform_admin','platform_admin','active') RETURNING id`, operatorOrg).Scan(&approver))
	h := NewHandlerWithAPIKeyHashSecret(pool, "flash-fixture", "flash-http-fixture-hash", []byte("0123456789abcdef01234567"), nil)
	authorized := canonicalApplicationAuthorizedRequest(org, actor)
	// A fresh public model binds the independent data Definition. No old name,
	// Profile address, candidate successor or historical Release is rewritten.
	legacy := createCanonicalApplicationForTest(t, h, authorized, canonicalModelApplicationCreateRequest{
		Connection:  canonicalConnectionInput{Name: "Existing Flash connection fixture", Provider: "deepseek", BaseURL: "https://api.deepseek.com", AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)},
		PublicModel: canonicalPublicModelInput{ModelName: "historic-flash-local", ExecutionProfileID: "deepseek.v4-flash.api", ExecutionProfileVersion: "1.5.0"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "deepseek-v4-flash"}, Credential: canonicalCredentialInput{APIKey: "local-fixture-not-provider-credential"}, Operation: canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat},
	}, http.StatusCreated).Application
	created := createCanonicalApplicationForTest(t, h, authorized, canonicalModelApplicationCreateRequest{
		Connection:  canonicalConnectionInput{ID: legacy.Connection.ID},
		PublicModel: canonicalPublicModelInput{ModelName: "deepseek-v41-flash-local"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "deepseek-flash", ProfileDefinitionDigest: d.Digest()},
		Credential:  canonicalCredentialInput{BindingID: legacy.Credential.Bindings[0].ID},
		Operation:   canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat},
	}, http.StatusCreated)
	app := created.Application
	require.NotEqual(t, legacy.PublicModel.ID, app.PublicModel.ID)
	require.Equal(t, legacy.Connection.ID, app.Connection.ID)
	require.Equal(t, legacy.Credential.Bindings[0].ID, app.Credential.Bindings[0].ID)
	require.Equal(t, "deepseek.v4-flash.api", legacy.PublicModel.ExecutionProfileID)
	require.Equal(t, d.Profile().ID, app.PublicModel.ExecutionProfileID)
	require.Equal(t, d.Profile().Version, app.PublicModel.ExecutionProfileVersion)
	require.Len(t, app.Operations[0].CapabilityProbeContracts, 6)
	require.Equal(t, d.Digest(), app.Operations[0].ProfileDefinitionDigest)
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "https://api.deepseek.com/chat/completions", r.URL.String())
		require.Equal(t, "deepseek-flash", body["model"])
		return canonicalProbeHTTPResponse(200, `{"model":"deepseek-flash","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":2}}`), nil
	}))
	connectivity := serveCanonicalOperationProbeForTest(t, h, authorized, app.Deployment.ID, modelprofile.OperationOpenAIChat, app.Credential.Bindings[0].ID, "", http.StatusOK)
	require.True(t, connectivity.Success)
	stream := `data: {"id":"fixture","object":"chat.completion.chunk","created":1,"model":"deepseek-flash","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"brief","tool_calls":[{"index":0,"id":"call_echo","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":10,"prompt_cache_miss_tokens":90}}` + "\n\ndata: [DONE]\n\n"
	badResponse := false
	currentCapability := ""
	calls := 0
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "deepseek-flash", body["model"])
		require.Equal(t, true, body["stream"])
		require.Equal(t, map[string]any{"type": "enabled"}, body["thinking"])
		require.NotContains(t, body, "tool_choice")
		if currentCapability == "reasoning_effort" {
			require.Equal(t, "high", body["reasoning_effort"])
		} else {
			require.NotContains(t, body, "reasoning_effort")
		}
		response := stream
		if badResponse {
			response = strings.Replace(response, `"model":"deepseek-flash"`, `"model":"deepseek-v4-flash"`, 1)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
	}))
	approvalIDs := []int64{}
	for _, row := range bundle.BindingDelta {
		currentCapability = row.CapabilityID
		path := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/capabilities/%s", app.Deployment.ID, row.OperationID, row.CapabilityID)
		setPath := func(r *http.Request) {
			r.SetPathValue("deploymentID", strconv.FormatInt(app.Deployment.ID, 10))
			r.SetPathValue("operationID", row.OperationID)
			r.SetPathValue("capabilityID", row.CapabilityID)
		}
		read := func() modelCapabilityQualificationView {
			r := authorized(http.MethodGet, path, "")
			setPath(r)
			w := httptest.NewRecorder()
			h.GetCanonicalModelCapabilityQualification(w, r)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var view modelCapabilityQualificationView
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view))
			require.Equal(t, d.Digest(), view.ProfileDefinitionDigest)
			require.Equal(t, row.ProbeContractVersion, view.ProbeContractVersion)
			return view
		}
		require.True(t, read().ProbeAvailable)
		attempts := []bool{false}
		if row.CapabilityID == "reasoning_effort" {
			attempts = []bool{true, false}
		}
		for _, bad := range attempts {
			badResponse = bad
			r := authorized(http.MethodPost, path+"/probes", fmt.Sprintf(`{"credential_binding_id":%d}`, app.Credential.Bindings[0].ID))
			setPath(r)
			w := httptest.NewRecorder()
			h.ProbeCanonicalModelCapability(w, r)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var result modelCapabilityProbeResult
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.Equal(t, !bad, result.Success, w.Body.String())
			var storedSuccess bool
			var storedVersion string
			var requestSummary, responseSummary map[string]any
			require.NoError(t, pool.QueryRow(ctx, `SELECT success,probe_contract_version,request_summary,response_summary FROM model_capability_probes WHERE capability_probe_id=$1`, result.CapabilityProbeID).Scan(&storedSuccess, &storedVersion, &requestSummary, &responseSummary))
			require.Equal(t, result.Success, storedSuccess)
			require.Equal(t, row.ProbeContractVersion, storedVersion)
			require.Equal(t, d.Profile().ID, requestSummary["execution_profile_id"])
			require.Equal(t, d.Profile().Version, requestSummary["execution_profile_version"])
			// Persisted tuple -> immutable binding -> unique Definition is the
			// formal identity contract; request_summary need not duplicate it.
			var bindingDigest, upstream, fingerprint string
			require.NoError(t, pool.QueryRow(ctx, `SELECT binding.profile_definition_digest,probe.upstream_model,probe.source_fingerprint FROM model_capability_probes probe JOIN model_capability_probe_contract_bindings binding USING(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version) WHERE probe.capability_probe_id=$1`, result.CapabilityProbeID).Scan(&bindingDigest, &upstream, &fingerprint))
			require.Equal(t, d.Digest(), bindingDigest)
			require.Equal(t, "deepseek-flash", upstream)
			require.True(t, strings.HasPrefix(fingerprint, "sha256:"))
			require.Equal(t, "enabled", requestSummary["thinking_type"])
			require.NotContains(t, requestSummary, "messages")
			require.NotContains(t, requestSummary, "authorization")
			if row.CapabilityID == "reasoning_effort" {
				require.Equal(t, "high", requestSummary["reasoning_effort"])
			}
			view := read()
			require.NotNil(t, view.LatestProbe)
			require.Equal(t, result.CapabilityProbeID, view.LatestProbe.CapabilityProbeID)
			require.Equal(t, result.Success, view.LatestProbe.Success)
			require.Equal(t, result.DiagnosticStage, view.LatestProbe.DiagnosticStage)
			require.Equal(t, result.DiagnosticMember, view.LatestProbe.DiagnosticMember)
			if bad {
				require.Equal(t, "model_identity", result.DiagnosticStage)
				require.Equal(t, "mismatch", responseSummary["diagnostic_member"])
			}
		}
	}
	require.Equal(t, 7, calls, "GET, approval and freeze must not repeat a Provider call")
	// Formal HTTP plan and execution, rather than inferring batch usability
	// from a shared-contract unit test. Plans and readbacks cannot dispatch.
	batchBase := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s", app.Deployment.ID, modelprofile.OperationOpenAIChat)
	setBatchPath := func(r *http.Request) {
		r.SetPathValue("deploymentID", strconv.FormatInt(app.Deployment.ID, 10))
		r.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
	}
	planRequest := authorized(http.MethodGet, batchBase+"/capability-probe-plan?credential_binding_id="+strconv.FormatInt(app.Credential.Bindings[0].ID, 10), "")
	setBatchPath(planRequest)
	planResponse := httptest.NewRecorder()
	h.GetCanonicalCapabilityProbePlan(planResponse, planRequest)
	require.Equal(t, http.StatusOK, planResponse.Code, planResponse.Body.String())
	var plan capabilityProbePlan
	require.NoError(t, json.Unmarshal(planResponse.Body.Bytes(), &plan))
	require.Len(t, plan.Groups, 2)
	require.Empty(t, plan.SeparateCapabilities)
	require.Equal(t, 7, calls)
	beforeBatchCalls := calls
	batchResults := []modelCapabilityProbeResult{}
	sourceRefs := []string{}
	for _, group := range plan.Groups {
		require.Equal(t, 1, group.ProviderRequests)
		currentCapability = "function_tools"
		if strings.HasSuffix(group.ProbeContract, "@3.0.0") {
			require.Equal(t, []string{"reasoning_effort"}, group.CapabilityIDs)
			currentCapability = "reasoning_effort"
		} else {
			require.Equal(t, qualification.DeepSeekStreamFunctionToolsProbeContractID+"@1.0.0", group.ProbeContract)
			require.Len(t, group.CapabilityIDs, 5)
		}
		input, e := json.Marshal(capabilityProbeGroupRequest{CredentialBindingID: app.Credential.Bindings[0].ID, PlanDigest: group.PlanDigest, CapabilityIDs: group.CapabilityIDs})
		require.NoError(t, e)
		r := authorized(http.MethodPost, batchBase+"/capability-probe-groups", string(input))
		setBatchPath(r)
		w := httptest.NewRecorder()
		h.ProbeCanonicalCapabilityGroup(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result capabilityProbeGroupResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, group.CapabilityIDs, result.Observation.CapabilityIDs)
		require.Len(t, result.Results, len(group.CapabilityIDs))
		sourceRefs = append(sourceRefs, result.Observation.SourceRef)
		for _, fact := range result.Results {
			require.True(t, fact.Success)
			require.True(t, fact.Current)
			batchResults = append(batchResults, fact)
		}
		r = authorized(http.MethodGet, batchBase+"/capability-probe-groups/"+result.Observation.SourceRef, "")
		setBatchPath(r)
		r.SetPathValue("sourceRef", result.Observation.SourceRef)
		w = httptest.NewRecorder()
		h.GetCanonicalCapabilityProbeGroup(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var readback capabilityProbeGroupReadback
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &readback))
		require.Equal(t, result.Observation, readback.Observation)
		require.Len(t, readback.Facts, len(group.CapabilityIDs))
	}
	require.Equal(t, 2, calls-beforeBatchCalls)
	require.Len(t, batchResults, 6)
	var facts, physical int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*),count(DISTINCT source_ref) FROM model_capability_probes WHERE deployment_id=$1 AND source_ref=ANY($2)`, app.Deployment.ID, sourceRefs).Scan(&facts, &physical))
	require.Equal(t, 6, facts)
	require.Equal(t, 2, physical)
	batchProbeIDs := map[int64]bool{}
	for _, fact := range batchResults {
		batchProbeIDs[fact.CapabilityProbeID] = true
		path := fmt.Sprintf("%s/capabilities/%s/routing-approvals", batchBase, fact.CapabilityID)
		r := canonicalOperationProbeAuthorizedRequest(operatorOrg, org, approver, auth.RolePlatformAdmin)(http.MethodPost, path, fmt.Sprintf(`{"capability_probe_id":%d,"reason":"exact shared Flash fixture"}`, fact.CapabilityProbeID))
		setBatchPath(r)
		r.SetPathValue("capabilityID", fact.CapabilityID)
		w := httptest.NewRecorder()
		h.ApproveCanonicalModelCapabilityRouting(w, r)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var approval modelCapabilityApprovalResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &approval))
		approvalIDs = append(approvalIDs, approval.ApprovalID)
	}
	require.Equal(t, 9, calls)

	router, err := gateway.NewRouterWithPolicy(pool, routingpolicy.Default())
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, routingpolicy.Default())
	require.NoError(t, err)
	h.SetRuntimeChangeManager(manager)
	receipt, _ := serveCanonicalDeploymentReleaseAuthoring(t, h, authorized, canonicalDeploymentReleaseAuthoringRequest{PublicModelID: app.PublicModel.ID, Baseline: runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial}, Selections: []runtimechange.DeploymentReleaseSelection{{DeploymentID: app.Deployment.ID, OperationID: modelprofile.OperationOpenAIChat}}, ProfileDefinitionDigest: d.Digest(), PublicContextLimit: 64000, PublicOutputLimit: 4096, CapabilityApprovalIDs: approvalIDs, Reason: "independent current Flash text fixture"}, "flash-v41-freeze", http.StatusCreated)
	sealed, err := releaseexecution.ReadSnapshot(ctx, pool, receipt.Release.ID)
	require.NoError(t, err)
	require.NoError(t, sealed.ValidateDefinition(d))
	require.Len(t, sealed.CapabilityPins, 6)
	effortPin := false
	for _, pin := range sealed.CapabilityPins {
		require.True(t, batchProbeIDs[pin.ProbeID], "freeze must pin the exact batch fact")
		if pin.CapabilityID == "reasoning_effort" {
			effortPin = true
			require.Equal(t, "3.0.0", pin.ProbeContractVersion)
		}
	}
	require.True(t, effortPin)
	verifyFirstDataGateway(t, h, authorized, receipt.Release, app, d, []byte(stream))
	retained, err := releaseexecution.ReadSnapshot(ctx, pool, receipt.Release.ID)
	require.NoError(t, err)
	require.Equal(t, sealed, retained)
	require.Equal(t, 9, calls)
}
