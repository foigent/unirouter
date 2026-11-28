package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/executionevidence"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestBoundedChatMediaRequestsUseExactProfiles(t *testing.T) {
	clip, err := os.ReadFile("../modelcapabilityqualification/testdata/inline-video-colors.mp4")
	require.NoError(t, err)
	video := base64.StdEncoding.EncodeToString(clip)
	wave, err := os.ReadFile("../endpointprotocol/testdata/deepseek-speech-synthetic.wav")
	require.NoError(t, err)
	audio := base64.StdEncoding.EncodeToString(wave)
	for _, bundleName := range []string{"glm-53-bounded-inline-file", "mimo-26-paygo-inline-media", "minimax-m31-bounded-inline-video", "qwen-beijing-omni-chat", "kimi-qwen27b-bounded-inline-video", "qwen-hybrid-bounded-inline-video", "glm-53-release-video-sample"} {
		raw, err := os.ReadFile("../../capability-bundles/" + bundleName + ".json")
		require.NoError(t, err)
		b, err := publication.DecodeBundleV2(raw)
		require.NoError(t, err)
		for _, row := range b.Definitions {
			definition, err := modelprofile.DecodeDefinition(row)
			require.NoError(t, err)
			p := definition.Profile()
			model := p.UpstreamModelIDs[0]
			t.Run(model, func(t *testing.T) {
				var origin, mode, limitField string
				var controls map[string]any
				switch p.Provider {
				case "glm":
					origin, mode, limitField = "https://open.bigmodel.cn/api/paas/v4", "base_relative", "max_tokens"
					controls = map[string]any{"thinking": map[string]string{"type": "enabled"}}
				case "mimo":
					origin, mode, limitField = "https://api.xiaomimimo.com/v1", "canonical_v1", "max_completion_tokens"
					controls = map[string]any{"thinking": map[string]string{"type": "disabled"}}
				case "minimax":
					origin, mode, limitField = "https://api.minimax.cn/v1", "canonical_v1", "max_completion_tokens"
					controls = map[string]any{"thinking": map[string]string{"type": "adaptive"}}
				case "kimi":
					origin, mode, limitField = "https://api.moonshot.cn/v1", "canonical_v1", "max_completion_tokens"
					controls = nil
				case "dashscope":
					origin, mode, limitField = "https://workspace-test.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "canonical_v1", "max_tokens"
					controls = map[string]any{"enable_thinking": false}
					if p.ProtocolProfile == modelprofile.QwenHybridInlineVideoProtocol {
						limitField = "max_completion_tokens"
					}
					if model == modelprofile.QwenOmniModel {
						controls = map[string]any{"reasoning_effort": "none", "modalities": []string{"text"}}
					}
				}
				key := []byte("0123456789abcdef0123456789abcdef")
				plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, origin, key, nil)
				plan.Definition, plan.ExecutionProfileID, plan.ExecutionProfileVersion = definition, p.ID, p.Version
				plan.PublicContextLimit, plan.PublicOutputLimit, plan.RequestDefaultOutputLimit = p.DefaultContextWindow, p.DefaultOutputLimit, 48
				plan.ModelProtocolProfile, plan.ReasoningProfile, plan.CacheUsageProfile = p.ProtocolProfile, p.ReasoningProfile, p.CacheUsageProfile
				plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = p.Provider, model
				plan.Routes[0].MaxContext, plan.Routes[0].DefaultMaxOutput = p.DefaultContextWindow, 48
				plan.Routes[0].AdapterConfig = json.RawMessage(fmt.Sprintf(`{"endpoint_path_mode":%q}`, mode))
				plan.Routes[0].ProtocolProfileID, plan.Routes[0].ProtocolProfileVersion = p.ProtocolProfile, modelprofile.ChatOperationProtocolBindingVersionV1
				relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
				coordinator := &executionCoordinatorStub{authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
					digest, err := executionevidence.Digest(route.Evidence)
					return modelexecution.Decision{Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest, ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID}}, err
				}}
				require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default()))
				for _, capability := range p.Capabilities {
					var part map[string]any
					var media string
					switch capability {
					case modelprofile.CapabilityFiles:
						media = "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("Example verification code\n"))
						part = map[string]any{"type": "file", "file": map[string]string{"file_data": media, "filename": "sample.txt"}}
					case modelprofile.CapabilityVideo:
						media = "data:video/mp4;base64," + video
						if model == modelprofile.QwenOmniModel {
							media = "data:;base64," + video
						}
						if p.ProtocolProfile == modelprofile.GLM53PublicVideoProtocol {
							media = "https://media.example.org/project/first.mp4"
						}
						part = map[string]any{"type": "video_url", "video_url": map[string]string{"url": media}}
					case modelprofile.CapabilityAudio:
						media = "data:audio/wav;base64," + audio
						audioBlock := map[string]string{"data": media}
						if model == modelprofile.QwenOmniModel {
							audioBlock["format"] = "wav"
						}
						part = map[string]any{"type": "input_audio", "input_audio": audioBlock}
					case modelprofile.CapabilityVision:
						media = testJPEGDataURL(t, 32, 32)
						part = map[string]any{"type": "image_url", "image_url": map[string]string{"url": media}}
					default:
						continue
					}
					t.Run(capability, func(t *testing.T) {
						document := map[string]any{"model": "public-model", "stream": false, limitField: 48,
							"messages": []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": "Use the attachment."}, part}}}}
						for field, value := range controls {
							document[field] = value
						}
						body, err := json.Marshal(document)
						require.NoError(t, err)
						// Pre-profile ingress must admit the exact bytes independently
						// of the later selected-profile capability check.
						_, err = estimateChatPreProfileAdmission(body)
						require.NoError(t, err)
						output := 48
						batch, err := relay.prepareModelExecutionRoutes(t.Context(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions, "public-model", "public-model", "fixture", modelExecutionRequestLimits{ExplicitOutputTokens: &output}, body, time.Now())
						require.NoError(t, err)
						sent, err := providerRequestBody(batch.routes[0].upstreamRequest)
						require.NoError(t, err)
						require.Contains(t, string(sent), media)
						require.Contains(t, string(sent), fmt.Sprintf(`"model":%q`, model))
						if capability == modelprofile.CapabilityVideo && p.ProtocolProfile != modelprofile.GLM53PublicVideoProtocol {
							require.GreaterOrEqual(t, batch.limits.EstimatedInputTokens, 24576)
						}
						if capability == modelprofile.CapabilityVideo && p.ProtocolProfile == modelprofile.GLM53PublicVideoProtocol {
							second := "https://cdn.example.com/project/second.MP4"
							var dispatched []string
							relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
								require.Equal(t, origin+"/chat/completions", r.URL.String())
								var outgoing struct {
									Messages []struct {
										Content []struct {
											Video struct{ URL string } `json:"video_url"`
										}
									}
								}
								require.NoError(t, json.NewDecoder(r.Body).Decode(&outgoing))
								dispatched = append(dispatched, outgoing.Messages[0].Content[1].Video.URL)
								response := fmt.Sprintf(`{"id":"public-video-fixture","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"Fixture response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":171,"completion_tokens":6,"total_tokens":177}}`, model)
								return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
							})}
							for index, value := range []string{media, second} {
								reqBody := strings.Replace(string(body), media, value, 1)
								rec := httptest.NewRecorder()
								relay.HandleChatCompletion(rec, executionRequest(http.MethodPost, "/v1/chat/completions", fmt.Sprintf("fixture-public-video-%d", index), reqBody))
								require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
							}
							require.Equal(t, []string{media, second}, dispatched)
							for _, badURL := range []string{"http://cdn.example.com/first.mp4", "https://name:pass@cdn.example.com/first.mp4", "https://cdn.example.com/first.mp4?secret=hidden", "https://127.0.0.1/first.mp4", "https://[::1]/first.mp4", "https://localhost/first.mp4", "https://object.internal/first.mp4", "https://100.100.100.200/first.mp4", "https://8.8.8.8/first.mp4", "https://cdn.example.com:8443/first.mp4", "https://cdn.example.com/first.mov", "https://cdn.example.com/first.mp4#secret"} {
								bad := strings.Replace(string(body), media, badURL, 1)
								rec := httptest.NewRecorder()
								relay.HandleChatCompletion(rec, executionRequest(http.MethodPost, "/v1/chat/completions", "fixture-media-invalid-public-url", bad))
								require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
							}
							require.Len(t, dispatched, 2)
						}
						if capability == modelprofile.CapabilityFiles {
							originalPart, err := json.Marshal(part)
							require.NoError(t, err)
							fileIDPart := `{"type":"file","file_id":"x"}`
							fileIDRequest := strings.Replace(string(body), string(originalPart), fileIDPart, 1)
							require.NotEqual(t, string(body), fileIDRequest)
							originalDoer := relay.providerHTTPDoer
							calls := 0
							relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
								calls++
								return nil, fmt.Errorf("unexpected provider request")
							})}
							recorded := httptest.NewRecorder()
							relay.HandleChatCompletion(recorded, executionRequest(http.MethodPost, "/v1/chat/completions", "fixture-media-file-id", fileIDRequest))
							relay.providerHTTPDoer = originalDoer
							require.Equal(t, http.StatusBadRequest, recorded.Code, recorded.Body.String())
							require.Zero(t, calls)
						}
						bad := strings.Replace(string(body), media, "https://example.invalid/not-an-inline-asset", 1)
						rec := httptest.NewRecorder()
						relay.HandleChatCompletion(rec, executionRequest(http.MethodPost, "/v1/chat/completions", "fixture-media-wrong", bad))
						require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
					})
				}
			})
		}
	}
}
