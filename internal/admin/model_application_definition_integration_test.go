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

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDataCandidateUsesSameStableProductAndConnectionWithoutMutatingLegacyIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var org, actor int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('data-candidate','active','customer') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'data-candidate','fixture','org_admin','org_admin','active') RETURNING id`, org).Scan(&actor))
	h := NewHandler(pool, "data-fixture", []byte("0123456789abcdef01234567"), nil)
	actorRequest := canonicalApplicationAuthorizedRequest(org, actor)
	request := canonicalModelApplicationCreateRequest{
		Connection:  canonicalConnectionInput{Name: "Data candidate GLM", Provider: "glm", BaseURL: "https://open.bigmodel.cn/api/paas/v4", AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)},
		PublicModel: canonicalPublicModelInput{ModelName: "data-glm-stable", ExecutionProfileID: "glm.5.3.api", ExecutionProfileVersion: "1.9.0"},
		Deployment:  canonicalDeploymentInput{UpstreamModel: "glm-5.3"}, Credential: canonicalCredentialInput{APIKey: "isolated-test-secret"},
		Operation: canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
	}
	legacy := createCanonicalApplicationForTest(t, h, actorRequest, request, http.StatusCreated)
	p, ok := modelprofile.Lookup("glm.5.3.api", "1.9.0")
	require.True(t, ok)
	p.Version = "99.0.0"
	d, err := modelprofile.NewDefinition(p, "glm/glm-5.3", "")
	require.NoError(t, err)
	// Isolated immutable storage fixture, never a Target publication receipt.
	// The materializer's real import/apply/ambiguity path has separate DB tests.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	wire := []byte(`{"fixture":"candidate creation"}`)
	_, err = tx.Exec(ctx, `INSERT INTO capability_publications(publication_id,kind,legacy_through,legacy_projection_digest,expected_projection_digest,result_projection_digest,plan_digest,plan_bytes,materializer_contract,target_identity,publisher_identity)
VALUES('candidate-fixture','legacy_import','000113_glm_stream_function_tools_qualification',$1,$1,$1,$2,$3,'fixture','{}','{}')`, strings.Repeat("0", 64), fmt.Sprintf("%x", sha256.Sum256(wire)), wire)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SELECT set_config('unirouter.capability_publication_id','candidate-fixture',true)`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO model_profile_definitions(definition_digest,profile_id,profile_version,product_lineage_id,schema_version,canonical_bytes,publication_id) VALUES($1,$2,$3,$4,$5,$6,'candidate-fixture')`, d.Digest(), p.ID, p.Version, d.ProductLineageID(), modelprofile.DefinitionSchemaV1, d.CanonicalBytes())
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO model_capability_probe_contract_bindings(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version,expected_provider,expected_upstream_model,public_path,introduced_publication_id,profile_definition_digest)
SELECT operation_id,capability_id,execution_profile_id,$1,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version,expected_provider,expected_upstream_model,public_path,'candidate-fixture',$2
FROM model_capability_probe_contract_bindings WHERE execution_profile_id=$3 AND execution_profile_version='1.9.0'`, p.Version, d.Digest(), p.ID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO model_capability_current_probe_contracts(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version)
SELECT operation_id,capability_id,execution_profile_id,$1,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version FROM model_capability_current_probe_contracts WHERE execution_profile_id=$2 AND execution_profile_version='1.9.0'`, p.Version, p.ID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	request.Connection = canonicalConnectionInput{ID: legacy.Application.Connection.ID}
	request.PublicModel = canonicalPublicModelInput{ID: legacy.Application.PublicModel.ID}
	request.Credential = canonicalCredentialInput{BindingID: legacy.Application.Credential.Bindings[0].ID}
	request.Deployment.ProfileDefinitionDigest = d.Digest()
	request.Deployment.MaxContext = canonicalTestPtr(128000)
	request.Deployment.DefaultMaxOutput = canonicalTestPtr(4096)
	candidate := createCanonicalApplicationForTest(t, h, actorRequest, request, http.StatusCreated)
	require.NotEqual(t, legacy.Application.Deployment.ID, candidate.Application.Deployment.ID)
	require.Equal(t, legacy.Application.Connection.ID, candidate.Application.Connection.ID)
	require.Equal(t, legacy.Application.PublicModel.ID, candidate.Application.PublicModel.ID)
	require.Equal(t, "1.9.0", candidate.Application.PublicModel.ExecutionProfileVersion)
	require.Equal(t, d.Digest(), candidate.Application.Operations[0].ProfileDefinitionDigest)
	require.Equal(t, "99.0.0", candidate.Application.Operations[0].ExecutionProfileVersion)
	require.Len(t, candidate.Application.Operations[0].CapabilityProbeContracts, 5)
	require.True(t, candidate.Application.Operations[0].ProbeAvailable)
	replayed := createCanonicalApplicationForTest(t, h, actorRequest, request, http.StatusCreated)
	require.Equal(t, candidate.Application.Deployment.ID, replayed.Application.Deployment.ID)
	require.True(t, replayed.Reused.Deployment)
	catalogRequest := actorRequest(http.MethodGet, fmt.Sprintf("/api/v1/model-execution-profiles?public_model_id=%d", legacy.Application.PublicModel.ID), "")
	catalog := httptest.NewRecorder()
	h.ListModelExecutionProfiles(catalog, catalogRequest)
	require.Equal(t, http.StatusOK, catalog.Code, catalog.Body.String())
	require.Contains(t, catalog.Body.String(), d.Digest())
	require.Contains(t, catalog.Body.String(), "99.0.0")
	// A revision for a different stable product cannot create a candidate or
	// assign lineage as a side effect of a rejected application.
	otherRequest := request
	otherRequest.PublicModel = canonicalPublicModelInput{ModelName: "other-data-glm", ExecutionProfileID: "glm.5.2.api", ExecutionProfileVersion: "1.9.0"}
	otherRequest.Deployment = canonicalDeploymentInput{UpstreamModel: "glm-5.2"}
	other := createCanonicalApplicationForTest(t, h, actorRequest, otherRequest, http.StatusCreated)
	var beforeCount, afterCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM model_deployments WHERE customer_org_id=$1`, org).Scan(&beforeCount))
	bad := request
	bad.PublicModel = canonicalPublicModelInput{ID: other.Application.PublicModel.ID}
	createCanonicalApplicationForTest(t, h, actorRequest, bad, http.StatusBadRequest)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM model_deployments WHERE customer_org_id=$1`, org).Scan(&afterCount))
	require.Equal(t, beforeCount, afterCount)
	var otherLineage string
	require.NoError(t, pool.QueryRow(ctx, `SELECT COALESCE(product_lineage_id,'') FROM model_registry WHERE id=$1`, other.Application.PublicModel.ID).Scan(&otherLineage))
	require.Empty(t, otherLineage)
	// The real administrative Probe owner uses the stored definition. The
	// injected transport proves request and evidence, with no Provider network.
	calls := 0
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "/api/paas/v4/chat/completions", r.URL.Path)
		require.Equal(t, "glm-5.3", body["model"])
		require.Equal(t, float64(64), body["max_tokens"])
		require.Equal(t, false, body["stream"])
		return canonicalProbeHTTPResponse(200, `{"model":"glm-5.3","choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`), nil
	}))
	probe := serveCanonicalOperationProbeForTest(t, h, actorRequest, candidate.Application.Deployment.ID, string(modeloperation.IDChatCompletions), legacy.Application.Credential.Bindings[0].ID, "", http.StatusOK)
	require.True(t, probe.Success)
	require.Equal(t, 1, calls)
	var summaryDigest, oldDigest, oldState string
	var oldRevision int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT request_summary->>'profile_definition_digest' FROM model_deployment_probes WHERE probe_id=$1`, probe.ProbeID).Scan(&summaryDigest))
	require.Equal(t, d.Digest(), summaryDigest)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COALESCE(profile_definition_digest,''),evidence_state,revision FROM model_deployment_operations WHERE deployment_id=$1 AND operation_id=$2`, legacy.Application.Deployment.ID, modeloperation.IDChatCompletions).Scan(&oldDigest, &oldState, &oldRevision))
	require.Empty(t, oldDigest)
	require.Equal(t, legacy.Application.Operations[0].EvidenceState, oldState)
	require.Equal(t, legacy.Application.Operations[0].Revision, oldRevision)

	stream, err := os.ReadFile("../adapters/testdata/glm_stream_tools.sse")
	require.NoError(t, err)
	validResponse := false
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, true, body["stream"])
		require.Equal(t, "auto", body["tool_choice"])
		require.Equal(t, float64(1024), body["max_tokens"])
		require.Equal(t, "glm-5.3", body["model"])
		response := string(stream)
		if !validResponse {
			response = strings.Replace(response, `"usage":`, `"unexpected":true,"usage":`, 1)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
	}))
	capabilityPath := fmt.Sprintf("/api/v1/model-deployments/%d/operations/openai.chat_completions.v1/capabilities/function_tools", candidate.Application.Deployment.ID)
	setPath := func(r *http.Request) {
		r.SetPathValue("deploymentID", strconv.FormatInt(candidate.Application.Deployment.ID, 10))
		r.SetPathValue("operationID", string(modeloperation.IDChatCompletions))
		r.SetPathValue("capabilityID", "function_tools")
	}
	read := func() modelCapabilityQualificationView {
		r := actorRequest(http.MethodGet, capabilityPath, "")
		setPath(r)
		w := httptest.NewRecorder()
		h.GetCanonicalModelCapabilityQualification(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var view modelCapabilityQualificationView
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view))
		require.Equal(t, d.Digest(), view.ProfileDefinitionDigest)
		require.Equal(t, "99.0.0", view.ExecutionProfileVersion)
		return view
	}
	var successfulProbe modelCapabilityProbeResult
	for _, valid := range []bool{false, true} {
		validResponse = valid
		r := actorRequest(http.MethodPost, capabilityPath+"/probes", fmt.Sprintf(`{"credential_binding_id":%d}`, legacy.Application.Credential.Bindings[0].ID))
		setPath(r)
		w := httptest.NewRecorder()
		h.ProbeCanonicalModelCapability(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result modelCapabilityProbeResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.True(t, result.ProviderSuccess, "%+v", result)
		require.True(t, result.Current, "%+v", result)
		require.Equal(t, valid, result.Success)
		view := read()
		require.NotNil(t, view.LatestProbe, "stored data revision evidence must be visible without changing the stable model Profile")
		require.Equal(t, result.CapabilityProbeID, view.LatestProbe.CapabilityProbeID)
		require.Equal(t, valid, view.LatestProbe.Success)
		require.True(t, view.LatestProbe.Current)
		if valid {
			require.Equal(t, "probed_awaiting_approval", view.State)
			successfulProbe = result
		} else {
			require.Equal(t, "probe_failed", view.State)
		}
	}
	var operatorOrg, approver int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('data-approval','active','platform') RETURNING id`).Scan(&operatorOrg))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'data-approver','fixture','platform_admin','platform_admin','active') RETURNING id`, operatorOrg).Scan(&approver))
	approvalRequest := canonicalOperationProbeAuthorizedRequest(operatorOrg, org, approver, auth.RolePlatformAdmin)(http.MethodPost, capabilityPath+"/routing-approvals", fmt.Sprintf(`{"capability_probe_id":%d,"reason":"reviewed exact data revision evidence"}`, successfulProbe.CapabilityProbeID))
	setPath(approvalRequest)
	approvalResponse := httptest.NewRecorder()
	h.ApproveCanonicalModelCapabilityRouting(approvalResponse, approvalRequest)
	require.Equal(t, http.StatusCreated, approvalResponse.Code, approvalResponse.Body.String())
	require.True(t, read().Qualified)
	require.Equal(t, 3, calls, "GET and approval must not repeat the operation or capability Provider call")
	_, err = pool.Exec(ctx, `UPDATE provider_credential_bindings SET state_revision=state_revision+1 WHERE id=$1`, legacy.Application.Credential.Bindings[0].ID)
	require.NoError(t, err)
	stale := read()
	require.NotNil(t, stale.LatestProbe)
	require.False(t, stale.LatestProbe.Current)
	require.True(t, stale.Qualified, "credential eligibility remains separate from capability approval")
	_, err = pool.Exec(ctx, `UPDATE provider_connections SET config_revision=config_revision+1 WHERE id=$1`, legacy.Application.Connection.ID)
	require.NoError(t, err)
	stale = read()
	require.False(t, stale.Qualified)
	require.Equal(t, 3, calls)
	_, err = pool.Exec(ctx, `UPDATE model_deployments SET profile_definition_digest=$2 WHERE id=$1`, legacy.Application.Deployment.ID, d.Digest())
	require.Error(t, err, "cannot retrofit an existing Deployment identity")
}
