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
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

type canonicalOperationProbeFixture struct {
	DeploymentID int64
	ConnectionID int64
	BindingID    int64
	OtherBinding int64
	Secret       string
}

func TestCanonicalOperationProbeExactEvidenceLifecycleIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('canonical-probe-customer','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'canonical-probe-admin','fixture','org_admin','org_admin','active')
RETURNING id`, orgID).Scan(&actorID))

	encryptionKey := []byte("0123456789abcdef01234567")
	authorized := canonicalApplicationAuthorizedRequest(orgID, actorID)

	t.Run("platform admin may operate from a separate deployment identity organization", func(t *testing.T) {
		fixture := insertCanonicalOperationProbeFixture(t, pool, orgID, encryptionKey, "platform")
		var operatorOrgID, platformUserID int64
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('canonical-probe-operator','active','platform') RETURNING id`).Scan(&operatorOrgID))
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'canonical-probe-platform-admin','fixture','platform_admin','platform_admin','active')
RETURNING id`, operatorOrgID).Scan(&platformUserID))
		platform := canonicalOperationProbeAuthorizedRequest(
			operatorOrgID, orgID, platformUserID, auth.RolePlatformAdmin,
		)
		handler := NewHandler(pool, "canonical-probe-jwt", encryptionKey, nil)
		handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(_ *http.Request) (*http.Response, error) {
			return canonicalProbeHTTPResponse(http.StatusOK, `{"model":"reported-platform","choices":[{"message":{}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), nil
		}))
		result := serveCanonicalOperationProbeForTest(
			t, handler, platform, fixture.DeploymentID, string(modeloperation.IDChatCompletions),
			fixture.BindingID, "", http.StatusOK,
		)
		require.True(t, result.Success)
		require.True(t, result.Current)
	})

	t.Run("same deployment keeps chat and embeddings evidence independent", func(t *testing.T) {
		fixture := insertCanonicalOperationProbeFixture(t, pool, orgID, encryptionKey, "dual")
		calls := 0
		handler := NewHandler(pool, "canonical-probe-jwt", encryptionKey, nil)
		handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			switch request.URL.Path {
			case "/v1/chat/completions":
				return canonicalProbeHTTPResponse(http.StatusOK, `{"model":"reported-chat","choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`), nil
			case "/v1/embeddings":
				return canonicalProbeHTTPResponse(http.StatusOK, `{"model":"reported-embedding","data":[{"embedding":[0.1,0.2],"index":0}],"usage":{"prompt_tokens":2,"total_tokens":2}}`), nil
			default:
				t.Fatalf("unexpected probe path %q", request.URL.Path)
				return nil, nil
			}
		}))

		chat := serveCanonicalOperationProbeForTest(
			t, handler, authorized, fixture.DeploymentID, string(modeloperation.IDChatCompletions),
			fixture.BindingID, "probe-body-must-not-persist", http.StatusOK,
		)
		require.True(t, chat.Success)
		require.True(t, chat.ProviderSuccess)
		require.True(t, chat.Current)
		require.False(t, chat.Stale)
		require.True(t, chat.OperationAdvanced)
		require.Equal(t, "ok", chat.ReasonCode)
		require.Equal(t, "reported-chat", chat.ReportedModel)

		var chatState, embeddingState string
		var chatRevision, embeddingRevision int64
		require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_state,revision FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.DeploymentID, modeloperation.IDChatCompletions).Scan(&chatState, &chatRevision))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_state,revision FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.DeploymentID, modeloperation.IDEmbeddings).Scan(&embeddingState, &embeddingRevision))
		require.Equal(t, "live_probed", chatState)
		require.Equal(t, int64(2), chatRevision)
		require.Equal(t, "declared", embeddingState)
		require.Equal(t, int64(1), embeddingRevision)

		embedding := serveCanonicalOperationProbeForTest(
			t, handler, authorized, fixture.DeploymentID, string(modeloperation.IDEmbeddings),
			fixture.BindingID, "embedding-probe", http.StatusOK,
		)
		require.True(t, embedding.Success)
		require.True(t, embedding.OperationAdvanced)
		require.Equal(t, "reported-embedding", embedding.ReportedModel)
		require.Equal(t, 2, calls)

		var capabilityState, capabilityRef string
		var capabilityRevision int64
		require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_state,evidence_ref,revision FROM model_deployment_capabilities
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id='tools'`,
			fixture.DeploymentID, modeloperation.IDChatCompletions).Scan(&capabilityState, &capabilityRef, &capabilityRevision))
		require.Equal(t, "declared", capabilityState)
		require.Empty(t, capabilityRef)
		require.Equal(t, int64(1), capabilityRevision)

		var requestSummary, responseSummary, auditDetail string
		require.NoError(t, pool.QueryRow(ctx, `
SELECT request_summary::text,response_summary::text
  FROM model_deployment_probes WHERE probe_id=$1`, chat.ProbeID).Scan(&requestSummary, &responseSummary))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT detail::text FROM audit_logs
 WHERE action='model_deployment.operation_probe' AND resource_id=$1
   AND (detail->'after'->>'probe_id')::bigint=$2
	 ORDER BY id DESC LIMIT 1`, strconv.FormatInt(fixture.DeploymentID, 10), chat.ProbeID).Scan(&auditDetail))
		for _, forbidden := range []string{
			"probe-body-must-not-persist", fixture.Secret, "provider.example", "choices", "embedding",
		} {
			require.NotContains(t, requestSummary, forbidden)
			require.NotContains(t, responseSummary, forbidden)
			require.NotContains(t, auditDetail, forbidden)
		}
	})

	t.Run("reviewed DeepSeek exact Chat probe unlocks release without rewriting capability evidence", func(t *testing.T) {
		policy := routingpolicy.Default()
		router, err := gateway.NewRouterWithPolicy(pool, policy)
		require.NoError(t, err)
		manager, err := runtimechange.NewManager(pool, router, policy)
		require.NoError(t, err)
		router.SetRuntimeResolver(manager)
		handler := NewHandlerWithAPIKeyHashSecret(
			pool, "canonical-probe-jwt", "canonical-probe-hash", encryptionKey, nil,
		)
		handler.SetRuntimeChangeManager(manager)

		created := createCanonicalApplicationForTest(t, handler, authorized, canonicalModelApplicationCreateRequest{
			Connection: canonicalConnectionInput{
				Name: "DeepSeek Pro exact operation probe", Provider: "deepseek",
				BaseURL: "https://api.deepseek.com", AdapterType: "openai_compatible",
				AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
			},
			PublicModel: canonicalPublicModelInput{
				ModelName: "deepseek-pro-exact-probe", ExecutionProfileID: "deepseek.v4-pro.api",
				ExecutionProfileVersion: "1.5.0",
			},
			Deployment: canonicalDeploymentInput{UpstreamModel: "deepseek-v4-pro"},
			Credential: canonicalCredentialInput{APIKey: "deepseek-pro-probe-secret"},
			Operation:  canonicalOperationInput{OperationID: string(modeloperation.IDChatCompletions)},
		}, http.StatusCreated)
		deploymentID := created.Application.Deployment.ID
		bindingID := created.Application.Credential.Bindings[0].ID
		publicModelID := created.Application.PublicModel.ID
		var operatorOrgID, platformUserID int64
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('deepseek-pro-probe-operator','active','platform') RETURNING id`).Scan(&operatorOrgID))
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'deepseek-pro-probe-platform-admin','fixture','platform_admin','platform_admin','active')
RETURNING id`, operatorOrgID).Scan(&platformUserID))
		platformAuthorized := canonicalOperationProbeAuthorizedRequest(
			operatorOrgID, orgID, platformUserID, auth.RolePlatformAdmin,
		)

		handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
			require.Equal(t, "/chat/completions", request.URL.Path)
			body, readErr := io.ReadAll(request.Body)
			require.NoError(t, readErr)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(body, &payload))
			require.Equal(t, "deepseek-v4-pro", payload["model"])
			if payload["stream"] == true {
				stream := strings.Join([]string{
					`data: {"model":"deepseek-v4-pro","choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
					"",
					`data: {"model":"deepseek-v4-pro","choices":[{"delta":{"content":"UNIROUTER_STREAM_PROBE_OK"},"finish_reason":null}],"usage":null}`,
					"",
					`data: {"model":"deepseek-v4-pro","choices":[{"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24}}`,
					"", "data: [DONE]", "",
				}, "\r\n")
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
					Body:       io.NopCloser(strings.NewReader(stream)),
				}, nil
			}
			return canonicalProbeHTTPResponse(http.StatusOK, `{"model":"deepseek-v4-pro","choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`), nil
		}))

		capabilityTarget := fmt.Sprintf(
			"/api/v1/model-deployments/%d/operations/openai.chat_completions.v1/capabilities/streaming",
			deploymentID,
		)
		capabilityProbeRequest := platformAuthorized(http.MethodPost, capabilityTarget+"/probes",
			fmt.Sprintf(`{"credential_binding_id":%d}`, bindingID))
		capabilityProbeRequest.SetPathValue("deploymentID", strconv.FormatInt(deploymentID, 10))
		capabilityProbeRequest.SetPathValue("operationID", string(modeloperation.IDChatCompletions))
		capabilityProbeRequest.SetPathValue("capabilityID", "streaming")
		capabilityProbeResponse := httptest.NewRecorder()
		handler.ProbeCanonicalModelCapability(capabilityProbeResponse, capabilityProbeRequest)
		require.Equal(t, http.StatusOK, capabilityProbeResponse.Code, capabilityProbeResponse.Body.String())
		var capabilityProbe modelCapabilityProbeResult
		require.NoError(t, json.Unmarshal(capabilityProbeResponse.Body.Bytes(), &capabilityProbe))

		approvalRequest := platformAuthorized(http.MethodPost, capabilityTarget+"/routing-approvals",
			fmt.Sprintf(`{"capability_probe_id":%d,"reason":"exact Pro separation regression"}`, capabilityProbe.CapabilityProbeID))
		approvalRequest.SetPathValue("deploymentID", strconv.FormatInt(deploymentID, 10))
		approvalRequest.SetPathValue("operationID", string(modeloperation.IDChatCompletions))
		approvalRequest.SetPathValue("capabilityID", "streaming")
		approvalResponse := httptest.NewRecorder()
		handler.ApproveCanonicalModelCapabilityRouting(approvalResponse, approvalRequest)
		require.Equal(t, http.StatusCreated, approvalResponse.Code, approvalResponse.Body.String())
		var approval modelCapabilityApprovalResult
		require.NoError(t, json.Unmarshal(approvalResponse.Body.Bytes(), &approval))
		require.True(t, approval.Qualified)

		before := serveCanonicalDeploymentReleaseCollection(t, handler, authorized, http.StatusOK)
		beforeModel := canonicalDeploymentReleaseCollectionModel(t, before, publicModelID)
		require.NotNil(t, beforeModel.Authoring.Baseline)
		_, _ = serveCanonicalDeploymentReleaseAuthoring(t, handler, authorized,
			canonicalDeploymentReleaseAuthoringRequest{
				PublicModelID: publicModelID, Baseline: *beforeModel.Authoring.Baseline,
				Selections: []runtimechange.DeploymentReleaseSelection{{
					DeploymentID: deploymentID, OperationID: string(modeloperation.IDChatCompletions),
				}},
				Reason: "operation evidence must remain independent",
			},
			"deepseek-pro-before-operation-probe", http.StatusConflict,
		)

		result := serveCanonicalOperationProbeForTest(
			t, handler, authorized, deploymentID, string(modeloperation.IDChatCompletions),
			bindingID, "exact DeepSeek probe", http.StatusOK,
		)
		require.True(t, result.Success)
		require.True(t, result.OperationAdvanced)

		after := serveCanonicalDeploymentReleaseCollection(t, handler, authorized, http.StatusOK)
		afterModel := canonicalDeploymentReleaseCollectionModel(t, after, publicModelID)
		require.NotNil(t, afterModel.Authoring.Baseline)
		receipt, _ := serveCanonicalDeploymentReleaseAuthoring(t, handler, authorized,
			canonicalDeploymentReleaseAuthoringRequest{
				PublicModelID: publicModelID, Baseline: *afterModel.Authoring.Baseline,
				Selections: []runtimechange.DeploymentReleaseSelection{{
					DeploymentID: deploymentID, OperationID: string(modeloperation.IDChatCompletions),
				}},
				Reason: "exact Pro operation evidence unlocks authoring",
			},
			"deepseek-pro-after-operation-probe", http.StatusCreated,
		)
		var releaseProfileID, releaseProfileVersion string
		require.NoError(t, pool.QueryRow(ctx, `
SELECT execution_profile_id,execution_profile_version
  FROM runtime_change_releases WHERE release_ref=$1`, receipt.Release.ReleaseRef,
		).Scan(&releaseProfileID, &releaseProfileVersion))
		require.Equal(t, "deepseek.v4-pro.api", releaseProfileID)
		require.Equal(t, "1.5.0", releaseProfileVersion)

		var operationEvidence, probeProfileID, probeProfileVersion, capabilityEvidence, capabilityRef string
		require.NoError(t, pool.QueryRow(ctx, `
SELECT operation.evidence_state,probe.probe_profile_id,probe.probe_profile_version,
       capability.evidence_state,capability.evidence_ref
  FROM model_deployment_operations operation
  JOIN model_deployment_probes probe ON probe.probe_id=$2
  JOIN model_deployment_capabilities capability
    ON capability.deployment_id=operation.deployment_id
   AND capability.operation_id=operation.operation_id
   AND capability.capability_id='streaming'
 WHERE operation.deployment_id=$1 AND operation.operation_id=$3`,
			deploymentID, result.ProbeID, modeloperation.IDChatCompletions,
		).Scan(&operationEvidence, &probeProfileID, &probeProfileVersion,
			&capabilityEvidence, &capabilityRef))
		require.Equal(t, "live_probed", operationEvidence)
		require.Equal(t, "protocol.deepseek-openai-chat.v4", probeProfileID)
		require.Equal(t, "1.0.0", probeProfileVersion)
		require.Equal(t, "production_approved", capabilityEvidence)
		require.Equal(t, "capability_approval:"+strconv.FormatInt(approval.ApprovalID, 10), capabilityRef)
		var currentCapabilityQualifications int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_execution_qualifications
 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id='streaming'`,
			deploymentID, modeloperation.IDChatCompletions,
		).Scan(&currentCapabilityQualifications))
		require.Zero(t, currentCapabilityQualifications,
			"operation evidence revision must not rewrite capability evidence; it makes the prior exact qualification stale")
	})

	t.Run("unlinked exact binding and foreign organization never dispatch", func(t *testing.T) {
		fixture := insertCanonicalOperationProbeFixture(t, pool, orgID, encryptionKey, "identity")
		calls := 0
		handler := NewHandler(pool, "canonical-probe-jwt", encryptionKey, nil)
		handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			return canonicalProbeHTTPResponse(http.StatusOK, `{"choices":[{"message":{}}]}`), nil
		}))
		serveCanonicalOperationProbeForTest(
			t, handler, authorized, fixture.DeploymentID, string(modeloperation.IDChatCompletions),
			fixture.OtherBinding, "", http.StatusNotFound,
		)

		var foreignOrgID, foreignUserID int64
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type,parent_id)
VALUES ('canonical-probe-foreign','active','standard',$1) RETURNING id`, orgID).Scan(&foreignOrgID))
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'canonical-probe-foreign-admin','fixture','org_admin','org_admin','active')
RETURNING id`, foreignOrgID).Scan(&foreignUserID))
		foreign := canonicalApplicationAuthorizedRequest(foreignOrgID, foreignUserID)
		serveCanonicalOperationProbeForTest(
			t, handler, foreign, fixture.DeploymentID, string(modeloperation.IDChatCompletions),
			fixture.BindingID, "", http.StatusNotFound,
		)
		require.Zero(t, calls)
	})

	t.Run("provider failure is evidence but never advances operation", func(t *testing.T) {
		fixture := insertCanonicalOperationProbeFixture(t, pool, orgID, encryptionKey, "failure")
		handler := NewHandler(pool, "canonical-probe-jwt", encryptionKey, nil)
		handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(_ *http.Request) (*http.Response, error) {
			return canonicalProbeHTTPResponse(http.StatusUnauthorized, `{"error":{"message":"provider rejected secret"}}`), nil
		}))
		result := serveCanonicalOperationProbeForTest(
			t, handler, authorized, fixture.DeploymentID, string(modeloperation.IDChatCompletions),
			fixture.BindingID, "", http.StatusOK,
		)
		require.False(t, result.Success)
		require.False(t, result.ProviderSuccess)
		require.True(t, result.Current)
		require.False(t, result.Stale)
		require.Equal(t, "provider_http_error", result.ReasonCode)
		require.Equal(t, "provider_http_error", result.ProviderReasonCode)
		require.Empty(t, result.EvidenceRef)
		var evidenceState string
		var persistedSuccess bool
		var errorSummary string
		require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_state FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.DeploymentID, modeloperation.IDChatCompletions).Scan(&evidenceState))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT success,error_summary FROM model_deployment_probes WHERE probe_id=$1`, result.ProbeID).Scan(&persistedSuccess, &errorSummary))
		require.Equal(t, "declared", evidenceState)
		require.False(t, persistedSuccess)
		require.Equal(t, "provider_http_error", errorSummary)
		require.NotContains(t, errorSummary, "secret")
	})

	t.Run("concurrent operation evidence advancement does not poison successful binding evidence", func(t *testing.T) {
		fixture := insertCanonicalOperationProbeFixture(t, pool, orgID, encryptionKey, "operation-evidence-race")
		handler := NewHandler(pool, "canonical-probe-jwt", encryptionKey, nil)
		handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(_ *http.Request) (*http.Response, error) {
			tag, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='live_probed',evidence_ref='concurrent:evidence',verified_at=NOW(),
       revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2 AND revision=1`,
				fixture.DeploymentID, modeloperation.IDChatCompletions)
			require.NoError(t, err)
			require.EqualValues(t, 1, tag.RowsAffected())
			return canonicalProbeHTTPResponse(http.StatusOK, `{"model":"reported-concurrent","choices":[{"message":{}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), nil
		}))
		result := serveCanonicalOperationProbeForTest(
			t, handler, authorized, fixture.DeploymentID, string(modeloperation.IDChatCompletions),
			fixture.BindingID, "", http.StatusOK,
		)
		require.True(t, result.ProviderSuccess)
		require.True(t, result.Success, "the exact binding probe remains usable evidence: %+v", result)
		require.False(t, result.Current)
		require.True(t, result.Stale)
		require.False(t, result.OperationAdvanced)
		require.Equal(t, "operation_revision_changed_during_probe", result.ReasonCode)
		require.Equal(t, "ok", result.ProviderReasonCode)
		require.NotEmpty(t, result.EvidenceRef)

		var persistedSuccess bool
		var errorSummary, evidenceState, evidenceRef string
		var operationRevision int64
		require.NoError(t, pool.QueryRow(ctx, `
SELECT success,error_summary FROM model_deployment_probes WHERE probe_id=$1`, result.ProbeID).Scan(&persistedSuccess, &errorSummary))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_state,evidence_ref,revision FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.DeploymentID, modeloperation.IDChatCompletions).Scan(
			&evidenceState, &evidenceRef, &operationRevision,
		))
		require.True(t, persistedSuccess)
		require.Empty(t, errorSummary)
		require.Equal(t, "live_probed", evidenceState)
		require.Equal(t, "concurrent:evidence", evidenceRef)
		require.Equal(t, int64(2), operationRevision)
	})

	t.Run("link epoch drift preserves stale evidence and cannot become route evidence", func(t *testing.T) {
		fixture := insertCanonicalOperationProbeFixture(t, pool, orgID, encryptionKey, "link-drift")
		handler := NewHandler(pool, "canonical-probe-jwt", encryptionKey, nil)
		handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(_ *http.Request) (*http.Response, error) {
			_, err := pool.Exec(ctx, `
DELETE FROM credential_binding_deployments WHERE binding_id=$1 AND deployment_id=$2`, fixture.BindingID, fixture.DeploymentID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()+INTERVAL '1 second')`, fixture.BindingID, fixture.DeploymentID, fixture.ConnectionID)
			require.NoError(t, err)
			return canonicalProbeHTTPResponse(http.StatusOK, `{"model":"reported-stale","choices":[{"message":{}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), nil
		}))
		result := serveCanonicalOperationProbeForTest(
			t, handler, authorized, fixture.DeploymentID, string(modeloperation.IDChatCompletions),
			fixture.BindingID, "", http.StatusOK,
		)
		require.False(t, result.Success)
		require.True(t, result.ProviderSuccess)
		require.False(t, result.Current)
		require.True(t, result.Stale)
		require.Equal(t, "configuration_changed_during_probe", result.ReasonCode)
		require.Equal(t, "ok", result.ProviderReasonCode)
		var persistedSuccess bool
		var evidenceState string
		require.NoError(t, pool.QueryRow(ctx, `SELECT success FROM model_deployment_probes WHERE probe_id=$1`, result.ProbeID).Scan(&persistedSuccess))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT evidence_state FROM model_deployment_operations
 WHERE deployment_id=$1 AND operation_id=$2`, fixture.DeploymentID, modeloperation.IDChatCompletions).Scan(&evidenceState))
		require.False(t, persistedSuccess)
		require.Equal(t, "declared", evidenceState)
	})

	t.Run("connection revision drift preserves the tested revision without promotion", func(t *testing.T) {
		fixture := insertCanonicalOperationProbeFixture(t, pool, orgID, encryptionKey, "revision-drift")
		handler := NewHandler(pool, "canonical-probe-jwt", encryptionKey, nil)
		handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(_ *http.Request) (*http.Response, error) {
			_, err := pool.Exec(ctx, `
UPDATE provider_connections
   SET base_url='https://changed-provider.example/v1',config_revision=config_revision+1
 WHERE id=$1`, fixture.ConnectionID)
			require.NoError(t, err)
			return canonicalProbeHTTPResponse(http.StatusOK, `{"model":"reported-old-revision","choices":[{"message":{}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), nil
		}))
		result := serveCanonicalOperationProbeForTest(
			t, handler, authorized, fixture.DeploymentID, string(modeloperation.IDChatCompletions),
			fixture.BindingID, "", http.StatusOK,
		)
		require.True(t, result.ProviderSuccess)
		require.False(t, result.Success)
		require.True(t, result.Stale)
		require.Equal(t, "configuration_changed_during_probe", result.ReasonCode)
		var testedRevision, currentRevision int64
		var persistedSuccess bool
		require.NoError(t, pool.QueryRow(ctx, `
SELECT connection_revision,success FROM model_deployment_probes WHERE probe_id=$1`, result.ProbeID).Scan(&testedRevision, &persistedSuccess))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT config_revision FROM provider_connections WHERE id=$1`, fixture.ConnectionID).Scan(&currentRevision))
		require.Equal(t, int64(1), testedRevision)
		require.Equal(t, int64(2), currentRevision)
		require.False(t, persistedSuccess)
	})
}

func insertCanonicalOperationProbeFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	orgID int64,
	encryptionKey []byte,
	suffix string,
) canonicalOperationProbeFixture {
	t.Helper()
	ctx := context.Background()
	fixture := canonicalOperationProbeFixture{Secret: "provider-secret-" + suffix}
	encrypted, err := auth.EncryptKey([]byte(fixture.Secret), encryptionKey)
	require.NoError(t, err)
	var publicModelID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit)
VALUES ($1,$2,$2,'openai','ga',TRUE,8192,128) RETURNING id`,
		orgID, "canonical-probe-"+suffix).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (
  name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ($1,'openai','https://provider.example','openai_compatible','{}','enabled',1)
RETURNING id`, "canonical-probe-"+suffix).Scan(&fixture.ConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state,
  config_revision,priority,routing_weight,max_context,default_max_output,degradation_threshold)
VALUES ($1,$2,$3,$4,'enabled',1,100,100,8192,128,10) RETURNING id`,
		orgID, publicModelID, fixture.ConnectionID, "upstream-"+suffix).Scan(&fixture.DeploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,$2,'active',1) RETURNING id`, fixture.ConnectionID, encrypted).Scan(&fixture.BindingID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,$2,'active',1) RETURNING id`, fixture.ConnectionID, encrypted).Scan(&fixture.OtherBinding))
	_, err = pool.Exec(ctx, `
INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id,created_at)
VALUES ($1,$2,$3,NOW()-INTERVAL '1 minute')`, fixture.BindingID, fixture.DeploymentID, fixture.ConnectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES
  ($1,$2,'enabled','declared','',1,'openai.chat_completions','1.0.0'),
  ($1,$3,'enabled','declared','',1,'openai.embeddings','1.0.0')`,
		fixture.DeploymentID, modeloperation.IDChatCompletions, modeloperation.IDEmbeddings)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_capabilities (
  deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision)
VALUES ($1,$2,'tools','enabled','declared','',1)`, fixture.DeploymentID, modeloperation.IDChatCompletions)
	require.NoError(t, err)
	return fixture
}

func serveCanonicalOperationProbeForTest(
	t *testing.T,
	handler *Handler,
	authorized func(string, string, string) *http.Request,
	deploymentID int64,
	operationID string,
	bindingID int64,
	probeText string,
	expectedStatus int,
) canonicalOperationProbeResponse {
	t.Helper()
	body, err := json.Marshal(canonicalOperationProbeRequest{
		CredentialBindingID: bindingID, ProbeText: probeText,
	})
	require.NoError(t, err)
	target := fmt.Sprintf(
		"/api/v1/model-deployments/%d/operations/%s/probes", deploymentID, operationID,
	)
	request := authorized(http.MethodPost, target, string(body))
	request.SetPathValue("deploymentID", strconv.FormatInt(deploymentID, 10))
	request.SetPathValue("operationID", operationID)
	response := httptest.NewRecorder()
	handler.ProbeCanonicalModelOperation(response, request)
	require.Equal(t, expectedStatus, response.Code, response.Body.String())
	var result canonicalOperationProbeResponse
	if expectedStatus == http.StatusOK {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	}
	return result
}

func canonicalProbeHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func canonicalOperationProbeAuthorizedRequest(
	identityOrgID, customerOrgID, userID int64,
	organizationRole string,
) func(string, string, string) *http.Request {
	return func(method, target, body string) *http.Request {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		ctx := context.WithValue(request.Context(), middleware.CtxUserID, userID)
		ctx = context.WithValue(ctx, middleware.CtxOrgID, identityOrgID)
		ctx = context.WithValue(ctx, middleware.CtxUserRole, organizationRole)
		ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, organizationRole)
		ctx = context.WithValue(ctx, middleware.CtxCustomerContext, customercontext.Snapshot{
			State: customercontext.StateReady, OrganizationID: customerOrgID, OrganizationName: "Canonical customer",
		})
		ctx = context.WithValue(ctx, middleware.CtxCustomerOrgID, customerOrgID)
		return request.WithContext(ctx)
	}
}
