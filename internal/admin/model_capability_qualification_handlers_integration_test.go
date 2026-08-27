//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDeepSeekStreamingCapabilityProbeAndApprovalFlowIntegration(t *testing.T) {
	for _, testCase := range []struct {
		name                       string
		protocolProfileID          string
		verifyCanonicalMaterialize bool
	}{
		{name: "current-v4", protocolProfileID: "protocol.deepseek-openai-chat.v4", verifyCanonicalMaterialize: true},
		{name: "sealed-legacy", protocolProfileID: "openai.chat_completions"},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			testDeepSeekStreamingCapabilityProbeAndApprovalFlow(
				t, testCase.name, testCase.protocolProfileID, testCase.verifyCanonicalMaterialize,
			)
		})
	}
}

func testDeepSeekStreamingCapabilityProbeAndApprovalFlow(
	t *testing.T,
	suffix, protocolProfileID string,
	verifyCanonicalMaterialize bool,
) {
	pool := testutil.SetupMigratedTestDB(t)
	fixture := integrationfixture.InsertModelCapabilityQualificationWithProtocol(
		t, pool, "admin-flow-"+suffix, protocolProfileID, "1.0.0",
	)
	var operatorOrgID, platformUserID int64
	require.NoError(t, pool.QueryRow(t.Context(), `
INSERT INTO orgs (name,status,org_type)
VALUES ('capability-operator-admin-flow','active','platform') RETURNING id`).Scan(&operatorOrgID))
	require.NoError(t, pool.QueryRow(t.Context(), `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'capability-platform-admin-flow','fixture','platform_admin','platform_admin','active')
RETURNING id`, operatorOrgID).Scan(&platformUserID))
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("deepseek-provider-secret"), encryptionKey)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
UPDATE provider_credential_bindings
   SET key_enc=$2,state_revision=state_revision+1 WHERE id=$1`, fixture.BindingID, encrypted)
	require.NoError(t, err)

	handler := NewHandler(pool, "capability-jwt", encryptionKey, nil)
	var providerCalls atomic.Int64
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "api.deepseek.com", request.URL.Host)
		body, readErr := io.ReadAll(request.Body)
		require.NoError(t, readErr)
		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, "deepseek-v4-flash", payload["model"])
		require.Equal(t, true, payload["stream"])
		require.EqualValues(t, 64, payload["max_tokens"])
		require.Equal(t, map[string]interface{}{"include_usage": true}, payload["stream_options"])
		require.Equal(t, map[string]interface{}{"type": "disabled"}, payload["thinking"])
		stream := strings.Join([]string{
			`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
			"",
			`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"content":"UNIROUTER_STREAM_PROBE_OK"},"finish_reason":"stop"}]}`,
			"",
			`data: {"model":"deepseek-v4-flash","choices":[],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24}}`,
			"",
			`data: [DONE]`,
			"",
		}, "\r\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
		}, nil
	}))
	authorized := canonicalOperationProbeAuthorizedRequest(
		operatorOrgID, fixture.OrgID, platformUserID, auth.RolePlatformAdmin,
	)
	if verifyCanonicalMaterialize {
		_, err = pool.Exec(t.Context(), `DELETE FROM model_deployment_capabilities
			WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
			  AND capability_id='streaming'`, fixture.DeploymentID)
		require.NoError(t, err)
		created := createCanonicalApplicationForTest(t, handler, authorized, canonicalModelApplicationCreateRequest{
			Connection:  canonicalConnectionInput{ID: fixture.ConnectionID},
			PublicModel: canonicalPublicModelInput{ID: fixture.ModelID},
			Deployment: canonicalDeploymentInput{
				UpstreamModel:    "deepseek-v4-flash",
				MaxContext:       canonicalTestPtr(393216),
				DefaultMaxOutput: canonicalTestPtr(32768),
			},
			Credential: canonicalCredentialInput{BindingID: fixture.BindingID},
			Operation:  canonicalOperationInput{OperationID: "openai.chat_completions.v1"},
		}, http.StatusCreated)
		require.Equal(t, fixture.DeploymentID, created.Application.Deployment.ID)
	}
	require.Equal(t, int64(0), providerCalls.Load(), "supported capability materialization must not call the provider")
	var declaredCapabilityCount int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COUNT(*)
		FROM model_deployment_capabilities
		WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'
		  AND capability_id='streaming' AND support_state='enabled'
		  AND evidence_state='declared'
		  AND evidence_ref='capability_contract:unirouter.capability-probe.deepseek-chat-sse.v1@1.0.0'`,
		fixture.DeploymentID).Scan(&declaredCapabilityCount))
	require.Equal(t, 1, declaredCapabilityCount)

	probeTarget := fmt.Sprintf(
		"/api/v1/model-deployments/%d/operations/openai.chat_completions.v1/capabilities/streaming/probes",
		fixture.DeploymentID,
	)
	probeRequest := authorized(http.MethodPost, probeTarget,
		fmt.Sprintf(`{"credential_binding_id":%d}`, fixture.BindingID))
	probeRequest.SetPathValue("deploymentID", strconv.FormatInt(fixture.DeploymentID, 10))
	probeRequest.SetPathValue("operationID", "openai.chat_completions.v1")
	probeRequest.SetPathValue("capabilityID", "streaming")
	probeResponse := httptest.NewRecorder()
	handler.ProbeCanonicalModelCapability(probeResponse, probeRequest)
	require.Equal(t, http.StatusOK, probeResponse.Code, probeResponse.Body.String())
	var probe modelCapabilityProbeResult
	require.NoError(t, json.Unmarshal(probeResponse.Body.Bytes(), &probe))
	require.True(t, probe.Success)
	require.True(t, probe.Current)
	require.Equal(t, "stop", probe.FinishReason)
	require.Equal(t, int64(24), probe.TotalTokens)
	require.Equal(t, int64(1), providerCalls.Load())

	approvalTarget := strings.TrimSuffix(probeTarget, "/probes") + "/routing-approvals"
	approvalBody := fmt.Sprintf(`{"capability_probe_id":%d,"reason":"reviewed exact streaming evidence"}`, probe.CapabilityProbeID)
	approvalRequest := authorized(http.MethodPost, approvalTarget, approvalBody)
	approvalRequest.SetPathValue("deploymentID", strconv.FormatInt(fixture.DeploymentID, 10))
	approvalRequest.SetPathValue("operationID", "openai.chat_completions.v1")
	approvalRequest.SetPathValue("capabilityID", "streaming")
	approvalResponse := httptest.NewRecorder()
	handler.ApproveCanonicalModelCapabilityRouting(approvalResponse, approvalRequest)
	require.Equal(t, http.StatusCreated, approvalResponse.Code, approvalResponse.Body.String())
	var approval modelCapabilityApprovalResult
	require.NoError(t, json.Unmarshal(approvalResponse.Body.Bytes(), &approval))
	require.True(t, approval.Qualified)
	require.Equal(t, "qualified", approval.QualificationState)
	require.Equal(t, int64(1), providerCalls.Load(), "approval must not make another provider call")
	var storedCustomerOrgID, storedActorOrgID int64
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT customer_org_id,approved_by_org_id
  FROM model_capability_routing_approvals WHERE approval_id=$1`, approval.ApprovalID).Scan(
		&storedCustomerOrgID, &storedActorOrgID,
	))
	require.Equal(t, fixture.OrgID, storedCustomerOrgID)
	require.Equal(t, operatorOrgID, storedActorOrgID)
	replayRequest := authorized(http.MethodPost, approvalTarget, approvalBody)
	replayRequest.SetPathValue("deploymentID", strconv.FormatInt(fixture.DeploymentID, 10))
	replayRequest.SetPathValue("operationID", "openai.chat_completions.v1")
	replayRequest.SetPathValue("capabilityID", "streaming")
	replayResponse := httptest.NewRecorder()
	handler.ApproveCanonicalModelCapabilityRouting(replayResponse, replayRequest)
	require.Equal(t, http.StatusOK, replayResponse.Code, replayResponse.Body.String())
	var replay modelCapabilityApprovalResult
	require.NoError(t, json.Unmarshal(replayResponse.Body.Bytes(), &replay))
	require.True(t, replay.IdempotencyReplay)
	require.Equal(t, approval.ApprovalID, replay.ApprovalID)

	forgedActorOrg := canonicalOperationProbeAuthorizedRequest(
		fixture.OrgID, fixture.OrgID, platformUserID, auth.RolePlatformAdmin,
	)
	forgedRequest := forgedActorOrg(http.MethodPost, approvalTarget, approvalBody)
	forgedRequest.SetPathValue("deploymentID", strconv.FormatInt(fixture.DeploymentID, 10))
	forgedRequest.SetPathValue("operationID", "openai.chat_completions.v1")
	forgedRequest.SetPathValue("capabilityID", "streaming")
	forgedResponse := httptest.NewRecorder()
	handler.ApproveCanonicalModelCapabilityRouting(forgedResponse, forgedRequest)
	require.Equal(t, http.StatusConflict, forgedResponse.Code, forgedResponse.Body.String())

	getTarget := strings.TrimSuffix(probeTarget, "/probes")
	getRequest := authorized(http.MethodGet, getTarget, "")
	getRequest.SetPathValue("deploymentID", strconv.FormatInt(fixture.DeploymentID, 10))
	getRequest.SetPathValue("operationID", "openai.chat_completions.v1")
	getRequest.SetPathValue("capabilityID", "streaming")
	getResponse := httptest.NewRecorder()
	handler.GetCanonicalModelCapabilityQualification(getResponse, getRequest)
	require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
	var view modelCapabilityQualificationView
	require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &view))
	require.Equal(t, "approved_current", view.State)
	require.Equal(t, approval.ApprovalID, view.PreferredApprovalID)

	const rawProviderMarker = "PRIVATE_PROVIDER_STREAM_MARKER_MUST_NOT_PERSIST"
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		providerCalls.Add(1)
		stream := strings.Join([]string{
			`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"content":"partial"},"finish_reason":null}]}`,
			"",
			"event: " + rawProviderMarker,
			"",
		}, "\r\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
		}, nil
	}))
	failedRequest := authorized(http.MethodPost, probeTarget,
		fmt.Sprintf(`{"credential_binding_id":%d}`, fixture.BindingID))
	failedRequest.SetPathValue("deploymentID", strconv.FormatInt(fixture.DeploymentID, 10))
	failedRequest.SetPathValue("operationID", "openai.chat_completions.v1")
	failedRequest.SetPathValue("capabilityID", "streaming")
	failedResponse := httptest.NewRecorder()
	handler.ProbeCanonicalModelCapability(failedResponse, failedRequest)
	require.Equal(t, http.StatusOK, failedResponse.Code, failedResponse.Body.String())
	var failed modelCapabilityProbeResult
	require.NoError(t, json.Unmarshal(failedResponse.Body.Bytes(), &failed))
	require.False(t, failed.Success)
	require.True(t, failed.ProviderSuccess)
	require.Equal(t, "provider_stream_contract_invalid", failed.ProviderReasonCode)
	require.Equal(t, "unsupported_field", failed.ContractErrorCode)
	require.Equal(t, int64(2), providerCalls.Load())

	var storedSummary, storedError string
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT response_summary::text,error_summary
  FROM model_capability_probes WHERE capability_probe_id=$1`, failed.CapabilityProbeID).Scan(
		&storedSummary, &storedError,
	))
	require.Contains(t, storedSummary, `"contract_error_code": "unsupported_field"`)
	require.Contains(t, storedSummary, `"data_event_count": 1`)
	require.Equal(t, "provider_stream_contract_invalid", storedError)
	require.NotContains(t, storedSummary, rawProviderMarker)
	require.NotContains(t, storedError, rawProviderMarker)

	failedGetRequest := authorized(http.MethodGet, getTarget, "")
	failedGetRequest.SetPathValue("deploymentID", strconv.FormatInt(fixture.DeploymentID, 10))
	failedGetRequest.SetPathValue("operationID", "openai.chat_completions.v1")
	failedGetRequest.SetPathValue("capabilityID", "streaming")
	failedGetResponse := httptest.NewRecorder()
	handler.GetCanonicalModelCapabilityQualification(failedGetResponse, failedGetRequest)
	require.Equal(t, http.StatusOK, failedGetResponse.Code, failedGetResponse.Body.String())
	var failedView modelCapabilityQualificationView
	require.NoError(t, json.Unmarshal(failedGetResponse.Body.Bytes(), &failedView))
	require.NotNil(t, failedView.LatestProbe)
	require.Equal(t, failed.CapabilityProbeID, failedView.LatestProbe.CapabilityProbeID)
	require.Equal(t, "unsupported_field", failedView.LatestProbe.ContractErrorCode)
	require.NotContains(t, failedGetResponse.Body.String(), rawProviderMarker)
}
