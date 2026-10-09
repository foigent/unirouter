//go:build integration

package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestExistingFamilyDataProbeDispatchAndPersistenceIntegration(t *testing.T) {
	t.Run("published_deepseek_1.1.0", func(t *testing.T) {
		testPublishedDeepSeekFlashHTTPFlow(t, "deepseek-v41-flash-provider-completion.json")
	})
	for _, tc := range []struct{ id, version, protocol, probe, base string }{
		{"deepseek.v4-flash.api", "1.5.0", "protocol.deepseek-openai-chat.v5", qualification.DeepSeekStreamFunctionToolsProbeContractID, "https://api.deepseek.com"},
		{"kimi.k3.api", "1.9.0", "protocol.kimi-openai-chat.k3.v6", qualification.KimiStreamFunctionToolsProbeContractID, "https://api.moonshot.cn/v1"},
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v10", qualification.MiniMaxM3StreamFunctionToolsProbeContractID, "https://api.minimax.cn/v1"},
		{"qwen.3.7-flash.api", "1.7.0", "protocol.qwen-openai-chat.flash.v7", qualification.QwenStreamFunctionToolsProbeContractID, "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		{"qwen.3.7-flash.api", "1.7.0", "protocol.qwen-openai-chat.flash.v8", qualification.QwenProviderCompletionProbeContractID, "https://dashscope.aliyuncs.com/compatible-mode/v1"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			var org, actor int64
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('family-data','active','customer') RETURNING id`).Scan(&org))
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'family-data','fixture','org_admin','org_admin','active') RETURNING id`, org).Scan(&actor))
			h := NewHandler(pool, "fixture-jwt", []byte("0123456789abcdef01234567"), nil)
			authorized := canonicalApplicationAuthorizedRequest(org, actor)
			p, ok := modelprofile.Lookup(tc.id, tc.version)
			require.True(t, ok)
			request := canonicalModelApplicationCreateRequest{
				Connection:  canonicalConnectionInput{Name: "family fixture", Provider: p.Provider, BaseURL: tc.base, AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`)},
				PublicModel: canonicalPublicModelInput{ModelName: "family-stable", ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version},
				Deployment:  canonicalDeploymentInput{UpstreamModel: p.UpstreamModelIDs[0]}, Credential: canonicalCredentialInput{APIKey: "fixture-only-secret"},
				Operation: canonicalOperationInput{OperationID: modelprofile.OperationOpenAIChat},
			}
			if p.Provider == "deepseek" {
				request.Connection.AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"base_relative"}`)
			}
			if p.Provider == "dashscope" {
				request.Connection.AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`)
			}
			legacy := createCanonicalApplicationForTest(t, h, authorized, request, http.StatusCreated)
			p.Version, p.ProtocolProfile = "99.0.0", tc.protocol
			if p.Provider == "minimax" {
				p.ReasoningProfile = "reasoning.minimax-m3-adaptive-embedded.v2"
			}
			capability := "function_tools"
			if p.Provider == "dashscope" {
				capability = "reasoning_budget"
				p.Capabilities = []string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningBudget, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto}
				p.EvidenceRefs = []string{"https://help.aliyun.com/zh/model-studio/qwen3-7-flash", "https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions", "https://help.aliyun.com/zh/model-studio/qwen-function-calling"}
			}
			d, err := modelprofile.NewDefinition(p, p.Provider+"/"+strings.ToLower(p.UpstreamModelIDs[0]), "")
			require.NoError(t, err)
			// Isolated immutable fixture storage. Actual Publication authorization,
			// materialization and recovery are tested by the formal publisher suite.
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer tx.Rollback(ctx)
			wire := []byte(`{"fixture":"new stream implementation"}`)
			_, err = tx.Exec(ctx, `INSERT INTO capability_publications(publication_id,kind,legacy_through,legacy_projection_digest,expected_projection_digest,result_projection_digest,plan_digest,plan_bytes,materializer_contract,target_identity,publisher_identity)
VALUES('family-fixture','legacy_import','000113_glm_stream_function_tools_qualification',$1,$1,$1,$2,$3,'fixture','{}','{}')`, strings.Repeat("0", 64), fmt.Sprintf("%x", sha256.Sum256(wire)), wire)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `SELECT set_config('unirouter.capability_publication_id','family-fixture',true)`)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO model_profile_definitions(definition_digest,profile_id,profile_version,product_lineage_id,schema_version,canonical_bytes,publication_id) VALUES($1,$2,$3,$4,$5,$6,'family-fixture')`, d.Digest(), p.ID, p.Version, d.ProductLineageID(), modelprofile.DefinitionSchemaV1, d.CanonicalBytes())
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO model_capability_probe_contract_bindings(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version,expected_provider,expected_upstream_model,public_path,introduced_publication_id,profile_definition_digest)
VALUES($1,$9,$2,$3,$4,'1.0.0',$5,'1.0.0',$6,$7,'/v1/chat/completions','family-fixture',$8)`, modelprofile.OperationOpenAIChat, p.ID, p.Version, tc.protocol, tc.probe, p.Provider, p.UpstreamModelIDs[0], d.Digest(), capability)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO model_capability_current_probe_contracts(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version)
VALUES($1,$6,$2,$3,$4,'1.0.0',$5,'1.0.0')`, modelprofile.OperationOpenAIChat, p.ID, p.Version, tc.protocol, tc.probe, capability)
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			request.Connection = canonicalConnectionInput{ID: legacy.Application.Connection.ID}
			request.PublicModel = canonicalPublicModelInput{ID: legacy.Application.PublicModel.ID}
			request.Credential = canonicalCredentialInput{BindingID: legacy.Application.Credential.Bindings[0].ID}
			request.Deployment.ProfileDefinitionDigest = d.Digest()
			request.Deployment.MaxContext = canonicalTestPtr(modelprofile.DefaultPublicContextLimit(p))
			request.Deployment.DefaultMaxOutput = canonicalTestPtr(modelprofile.DefaultRequestOutputLimit(p, modelprofile.DefaultPublicOutputLimit(p)))
			candidate := createCanonicalApplicationForTest(t, h, authorized, request, http.StatusCreated)
			require.Equal(t, d.Digest(), candidate.Application.Operations[0].ProfileDefinitionDigest)
			qualificationPath := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/capabilities/%s", candidate.Application.Deployment.ID, modelprofile.OperationOpenAIChat, capability)
			qualificationRequest := authorized(http.MethodGet, qualificationPath, "")
			qualificationRequest.SetPathValue("deploymentID", strconv.FormatInt(candidate.Application.Deployment.ID, 10))
			qualificationRequest.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
			qualificationRequest.SetPathValue("capabilityID", capability)
			qualificationResponse := httptest.NewRecorder()
			h.GetCanonicalModelCapabilityQualification(qualificationResponse, qualificationRequest)
			require.Equal(t, http.StatusOK, qualificationResponse.Code, qualificationResponse.Body.String())
			var beforeProbe modelCapabilityQualificationView
			require.NoError(t, json.Unmarshal(qualificationResponse.Body.Bytes(), &beforeProbe))
			require.True(t, beforeProbe.ProbeAvailable)
			require.Equal(t, tc.probe, beforeProbe.ProbeContractID)
			require.Nil(t, beforeProbe.LatestProbe)
			calls := 0
			if p.Provider == "dashscope" {
				_, compiled := modelprofile.Lookup(p.ID, p.Version)
				require.False(t, compiled)
				require.True(t, candidate.Application.Operations[0].ProbeAvailable)
				h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.Equal(t, "/compatible-mode/v1/chat/completions", r.URL.Path)
					require.JSONEq(t, `{"model":"qwen3.7-flash","stream":false,"max_completion_tokens":64,"enable_thinking":false,"messages":[{"role":"user","content":"UniRouter connectivity probe"}]}`, string(body))
					return canonicalProbeHTTPResponse(200, `{"model":"qwen3.7-flash","choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`), nil
				}))
				probe := serveCanonicalOperationProbeForTest(t, h, authorized, candidate.Application.Deployment.ID, modelprofile.OperationOpenAIChat, request.Credential.BindingID, "", http.StatusOK)
				require.True(t, probe.Success)
				require.Equal(t, 1, calls)
				var summary map[string]any
				require.NoError(t, pool.QueryRow(ctx, `SELECT request_summary FROM model_deployment_probes WHERE probe_id=$1`, probe.ProbeID).Scan(&summary))
				require.Equal(t, d.Digest(), summary["profile_definition_digest"])
				require.Equal(t, p.ID, summary["execution_profile_id"])
				require.Equal(t, p.Version, summary["execution_profile_version"])
				require.Equal(t, tc.protocol, summary["protocol_profile_id"])
				require.Equal(t, false, summary["enable_thinking"])
				require.Equal(t, float64(64), summary["max_completion_tokens"])
				require.NotContains(t, summary, "messages")
				var oldDigest, oldState string
				var oldRevision int64
				require.NoError(t, pool.QueryRow(ctx, `SELECT COALESCE(profile_definition_digest,''),evidence_state,revision FROM model_deployment_operations WHERE deployment_id=$1 AND operation_id=$2`, legacy.Application.Deployment.ID, modelprofile.OperationOpenAIChat).Scan(&oldDigest, &oldState, &oldRevision))
				require.Empty(t, oldDigest)
				require.Equal(t, legacy.Application.Operations[0].EvidenceState, oldState)
				require.Equal(t, legacy.Application.Operations[0].Revision, oldRevision)
			}
			operationCalls := calls
			for _, valid := range []bool{false, true} {
				h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					var body map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, p.UpstreamModelIDs[0], body["model"])
					require.Equal(t, true, body["stream"])
					require.Equal(t, map[string]any{"include_usage": true}, body["stream_options"])
					contract, _ := modelprofile.ResolveChatProtocolContract(tc.protocol)
					if p.Provider == "dashscope" {
						expected := 2048
						if tc.protocol == "protocol.qwen-openai-chat.flash.v8" {
							expected -= 10
						}
						require.Equal(t, float64(expected), body[contract.OutputLimitField])
						require.Equal(t, true, body["enable_thinking"])
						require.Equal(t, float64(1024), body["thinking_budget"])
					} else {
						require.Equal(t, float64(1024), body[contract.OutputLimitField])
					}
					if p.Provider == "deepseek" {
						require.NotContains(t, body, "tool_choice")
					} else {
						require.Equal(t, "auto", body["tool_choice"])
					}
					usage := map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}
					delta := map[string]any{"role": "assistant", "reasoning_content": "Check echo.", "tool_calls": []any{map[string]any{"index": 0, "id": "call_echo", "type": "function", "function": map[string]any{"name": "unirouter_probe_echo", "arguments": `{"value":"UNIROUTER_TOOL_PROBE_OK"}`}}}}
					switch p.Provider {
					case "deepseek":
						usage["prompt_cache_hit_tokens"], usage["prompt_cache_miss_tokens"] = 10, 90
					case "kimi":
						usage["cached_tokens"] = 10
					case "dashscope":
						usage["prompt_tokens_details"] = map[string]any{"cached_tokens": 10}
					default:
						usage["prompt_tokens_details"] = map[string]any{"cached_tokens": 10}
						delete(delta, "reasoning_content")
						delta["content"] = "<think>Check echo.</think>"
						require.Equal(t, map[string]any{"type": "adaptive"}, body["thinking"])
					}
					envelope := map[string]any{"id": "fixture", "object": "chat.completion.chunk", "created": 1, "model": p.UpstreamModelIDs[0], "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": "tool_calls"}}, "usage": usage}
					if !valid {
						envelope["unmodeled"] = true
					}
					response, err := json.Marshal(envelope)
					require.NoError(t, err)
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + string(response) + "\n\ndata: [DONE]\n\n"))}, nil
				}))
				path := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/capabilities/%s/probes", candidate.Application.Deployment.ID, modelprofile.OperationOpenAIChat, capability)
				req := authorized(http.MethodPost, path, fmt.Sprintf(`{"credential_binding_id":%d}`, request.Credential.BindingID))
				req.SetPathValue("deploymentID", strconv.FormatInt(candidate.Application.Deployment.ID, 10))
				req.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
				req.SetPathValue("capabilityID", capability)
				rec := httptest.NewRecorder()
				h.ProbeCanonicalModelCapability(rec, req)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var result modelCapabilityProbeResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				require.Equal(t, valid, result.Success, rec.Body.String())
				require.True(t, result.ProviderSuccess, rec.Body.String())
				require.Equal(t, int64(120), result.TotalTokens)
				if valid {
					require.Empty(t, result.DiagnosticStage)
					require.Empty(t, result.DiagnosticMember)
				} else {
					require.Equal(t, "stream", result.ContractErrorCode)
					wantStage := "frame_shape"
					if p.Provider == "deepseek" || p.Provider == "kimi" {
						wantStage = "envelope"
					}
					require.Equal(t, wantStage, result.DiagnosticStage)
					require.Equal(t, "invalid_shape", result.DiagnosticMember)
				}
				var storedVersion, storedProtocol string
				var storedSuccess bool
				require.NoError(t, pool.QueryRow(ctx, `SELECT execution_profile_version,protocol_profile_id,success FROM model_capability_probes WHERE capability_probe_id=$1`, result.CapabilityProbeID).Scan(&storedVersion, &storedProtocol, &storedSuccess))
				require.Equal(t, "99.0.0", storedVersion)
				require.Equal(t, tc.protocol, storedProtocol)
				require.Equal(t, valid, storedSuccess)
				var responseSummary map[string]any
				require.NoError(t, pool.QueryRow(ctx, `SELECT response_summary FROM model_capability_probes WHERE capability_probe_id=$1`, result.CapabilityProbeID).Scan(&responseSummary))
				if valid {
					require.Equal(t, float64(2), responseSummary["data_event_count"])
					require.Equal(t, float64(1), responseSummary["delta_count"])
				} else {
					require.Equal(t, float64(1), responseSummary["data_event_count"])
					require.Equal(t, float64(0), responseSummary["delta_count"])
				}
				if valid {
					require.NotContains(t, responseSummary, "diagnostic_stage")
				} else {
					require.Equal(t, result.DiagnosticStage, responseSummary["diagnostic_stage"])
					require.Equal(t, result.DiagnosticMember, responseSummary["diagnostic_member"])
				}
				getPath := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/capabilities/%s", candidate.Application.Deployment.ID, modelprofile.OperationOpenAIChat, capability)
				getRequest := authorized(http.MethodGet, getPath, "")
				getRequest.SetPathValue("deploymentID", strconv.FormatInt(candidate.Application.Deployment.ID, 10))
				getRequest.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
				getRequest.SetPathValue("capabilityID", capability)
				getResponse := httptest.NewRecorder()
				h.GetCanonicalModelCapabilityQualification(getResponse, getRequest)
				require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
				var view modelCapabilityQualificationView
				require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &view))
				require.NotNil(t, view.LatestProbe)
				require.Equal(t, result.CapabilityProbeID, view.LatestProbe.CapabilityProbeID)
				require.Equal(t, result.DiagnosticStage, view.LatestProbe.DiagnosticStage)
				require.Equal(t, result.DiagnosticMember, view.LatestProbe.DiagnosticMember)
				if p.Provider == "dashscope" {
					var summary map[string]any
					require.NoError(t, pool.QueryRow(ctx, `SELECT request_summary FROM model_capability_probes WHERE capability_probe_id=$1`, result.CapabilityProbeID).Scan(&summary))
					require.Equal(t, true, summary["enable_thinking"])
					require.Equal(t, float64(1024), summary["thinking_budget"])
					require.NotContains(t, summary, "messages")
				}

			}
			if p.Provider == "deepseek" {
				var lastFailureProbeID int64
				for _, failure := range []struct {
					name, wire, stage, member string
					events                    int
				}{
					{"model alias", `data: {"id":"fixture","object":"chat.completion.chunk","created":1,"model":"deepseek-flash","choices":[{"index":0,"delta":{"role":"assistant"}}]}` + "\n\n", "model_identity", "mismatch", 1},
					{"SSE field", "event: private_probe_marker\n\n", "sse", "unsupported_field", 0},
				} {
					t.Run(failure.name, func(t *testing.T) {
						h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(*http.Request) (*http.Response, error) {
							calls++
							return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(failure.wire))}, nil
						}))
						path := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/capabilities/%s/probes", candidate.Application.Deployment.ID, modelprofile.OperationOpenAIChat, capability)
						req := authorized(http.MethodPost, path, fmt.Sprintf(`{"credential_binding_id":%d}`, request.Credential.BindingID))
						req.SetPathValue("deploymentID", strconv.FormatInt(candidate.Application.Deployment.ID, 10))
						req.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
						req.SetPathValue("capabilityID", capability)
						rec := httptest.NewRecorder()
						h.ProbeCanonicalModelCapability(rec, req)
						require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
						var result modelCapabilityProbeResult
						require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
						lastFailureProbeID = result.CapabilityProbeID
						require.False(t, result.Success)
						require.Equal(t, "stream", result.ContractErrorCode)
						require.Equal(t, failure.stage, result.DiagnosticStage)
						require.Equal(t, failure.member, result.DiagnosticMember)
						var summary map[string]any
						require.NoError(t, pool.QueryRow(ctx, `SELECT response_summary FROM model_capability_probes WHERE capability_probe_id=$1`, result.CapabilityProbeID).Scan(&summary))
						require.Equal(t, failure.stage, summary["diagnostic_stage"])
						require.Equal(t, failure.member, summary["diagnostic_member"])
						require.Equal(t, float64(failure.events), summary["data_event_count"])
						getPath := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/capabilities/%s", candidate.Application.Deployment.ID, modelprofile.OperationOpenAIChat, capability)
						getRequest := authorized(http.MethodGet, getPath, "")
						getRequest.SetPathValue("deploymentID", strconv.FormatInt(candidate.Application.Deployment.ID, 10))
						getRequest.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
						getRequest.SetPathValue("capabilityID", capability)
						getResponse := httptest.NewRecorder()
						h.GetCanonicalModelCapabilityQualification(getResponse, getRequest)
						require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
						var view modelCapabilityQualificationView
						require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &view))
						require.NotNil(t, view.LatestProbe)
						require.Equal(t, failure.stage, view.LatestProbe.DiagnosticStage)
						require.Equal(t, failure.member, view.LatestProbe.DiagnosticMember)
						for _, output := range []string{rec.Body.String(), getResponse.Body.String(), fmt.Sprint(summary)} {
							require.NotContains(t, output, "private_probe_marker")
						}
					})
				}
				// A corrupt historical summary is never projected as raw diagnostic
				// text, even if it bypassed the fixed writer classification.
				_, err = pool.Exec(ctx, `ALTER TABLE model_capability_probes DISABLE TRIGGER model_capability_probes_append_only`)
				require.NoError(t, err)
				t.Cleanup(func() {
					_, _ = pool.Exec(context.Background(), `ALTER TABLE model_capability_probes ENABLE TRIGGER model_capability_probes_append_only`)
				})
				_, err = pool.Exec(ctx, `UPDATE model_capability_probes SET response_summary=jsonb_set(jsonb_set(response_summary,'{diagnostic_stage}','"private_probe_marker"'::jsonb),'{diagnostic_member}','"private_probe_marker"'::jsonb) WHERE capability_probe_id=$1`, lastFailureProbeID)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `ALTER TABLE model_capability_probes ENABLE TRIGGER model_capability_probes_append_only`)
				require.NoError(t, err)
				getPath := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/capabilities/%s", candidate.Application.Deployment.ID, modelprofile.OperationOpenAIChat, capability)
				getRequest := authorized(http.MethodGet, getPath, "")
				getRequest.SetPathValue("deploymentID", strconv.FormatInt(candidate.Application.Deployment.ID, 10))
				getRequest.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
				getRequest.SetPathValue("capabilityID", capability)
				getResponse := httptest.NewRecorder()
				h.GetCanonicalModelCapabilityQualification(getResponse, getRequest)
				require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
				require.NotContains(t, getResponse.Body.String(), "private_probe_marker")
				var view modelCapabilityQualificationView
				require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &view))
				require.NotNil(t, view.LatestProbe)
				require.Empty(t, view.LatestProbe.DiagnosticStage)
				require.Empty(t, view.LatestProbe.DiagnosticMember)
			}
			wantCalls := operationCalls + 2
			if p.Provider == "deepseek" {
				wantCalls += 2
			}
			require.Equal(t, wantCalls, calls, "one fake Provider dispatch per explicit Probe")
		})
	}
}
