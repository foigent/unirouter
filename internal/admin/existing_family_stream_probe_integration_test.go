//go:build integration

package admin

import (
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
	for _, tc := range []struct{ id, version, protocol, probe, base string }{
		{"deepseek.v4-flash.api", "1.5.0", "protocol.deepseek-openai-chat.v5", qualification.DeepSeekStreamFunctionToolsProbeContractID, "https://api.deepseek.com"},
		{"kimi.k3.api", "1.9.0", "protocol.kimi-openai-chat.k3.v6", qualification.KimiStreamFunctionToolsProbeContractID, "https://api.moonshot.cn/v1"},
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v10", qualification.MiniMaxM3StreamFunctionToolsProbeContractID, "https://api.minimax.cn/v1"},
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
			legacy := createCanonicalApplicationForTest(t, h, authorized, request, http.StatusCreated)
			p.Version, p.ProtocolProfile = "99.0.0", tc.protocol
			if p.Provider == "minimax" {
				p.ReasoningProfile = "reasoning.minimax-m3-adaptive-embedded.v2"
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
VALUES($1,'function_tools',$2,$3,$4,'1.0.0',$5,'1.0.0',$6,$7,'/v1/chat/completions','family-fixture',$8)`, modelprofile.OperationOpenAIChat, p.ID, p.Version, tc.protocol, tc.probe, p.Provider, p.UpstreamModelIDs[0], d.Digest())
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO model_capability_current_probe_contracts(operation_id,capability_id,execution_profile_id,execution_profile_version,protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version)
VALUES($1,'function_tools',$2,$3,$4,'1.0.0',$5,'1.0.0')`, modelprofile.OperationOpenAIChat, p.ID, p.Version, tc.protocol, tc.probe)
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
			calls := 0
			for _, valid := range []bool{false, true} {
				h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					var body map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, p.UpstreamModelIDs[0], body["model"])
					require.Equal(t, true, body["stream"])
					require.Equal(t, map[string]any{"include_usage": true}, body["stream_options"])
					contract, _ := modelprofile.ResolveChatProtocolContract(tc.protocol)
					require.Equal(t, float64(1024), body[contract.OutputLimitField])
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
				path := fmt.Sprintf("/api/v1/model-deployments/%d/operations/%s/capabilities/function_tools/probes", candidate.Application.Deployment.ID, modelprofile.OperationOpenAIChat)
				req := authorized(http.MethodPost, path, fmt.Sprintf(`{"credential_binding_id":%d}`, request.Credential.BindingID))
				req.SetPathValue("deploymentID", strconv.FormatInt(candidate.Application.Deployment.ID, 10))
				req.SetPathValue("operationID", modelprofile.OperationOpenAIChat)
				req.SetPathValue("capabilityID", "function_tools")
				rec := httptest.NewRecorder()
				h.ProbeCanonicalModelCapability(rec, req)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var result modelCapabilityProbeResult
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				require.Equal(t, valid, result.Success, rec.Body.String())
				require.True(t, result.ProviderSuccess, rec.Body.String())
				require.Equal(t, int64(120), result.TotalTokens)
				var storedVersion, storedProtocol string
				var storedSuccess bool
				require.NoError(t, pool.QueryRow(ctx, `SELECT execution_profile_version,protocol_profile_id,success FROM model_capability_probes WHERE capability_probe_id=$1`, result.CapabilityProbeID).Scan(&storedVersion, &storedProtocol, &storedSuccess))
				require.Equal(t, "99.0.0", storedVersion)
				require.Equal(t, tc.protocol, storedProtocol)
				require.Equal(t, valid, storedSuccess)
			}
			require.Equal(t, 2, calls, "one fake Provider dispatch per explicit Probe")
		})
	}
}
