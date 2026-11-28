package gateway

import (
	"context"
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
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/metering"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestQwenOmniAudioUsesOrdinaryChatRoute(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/qwen-beijing-omni-chat.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	p := d.Profile()
	wave, err := os.ReadFile("../endpointprotocol/testdata/deepseek-speech-synthetic.wav")
	require.NoError(t, err)
	data := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(wave)
	part := `{"type":"input_audio","input_audio":{"format":"wav","data":"` + data + `"}}`
	key := []byte("0123456789abcdef0123456789abcdef")
	const origin = "https://workspace-test.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, origin, key, nil)
	plan.Definition = d
	plan.ExecutionProfileID, plan.ExecutionProfileVersion = p.ID, p.Version
	plan.ReasoningProfile, plan.CacheUsageProfile, plan.ModelProtocolProfile = p.ReasoningProfile, p.CacheUsageProfile, p.ProtocolProfile
	plan.PublicOutputLimit, plan.PublicContextLimit, plan.RequestDefaultOutputLimit = 512, 98_304, 128
	plan.Routes[0].UpstreamModel = modelprofile.QwenOmniModel
	plan.Routes[0].DefaultMaxOutput = 128
	plan.Routes[0].MaxContext = 98_304
	plan.Routes[0].Provider = p.Provider
	plan.Routes[0].ProtocolProfileID, plan.Routes[0].ProtocolProfileVersion = p.ProtocolProfile, modelprofile.ChatOperationProtocolBindingVersionV1
	plan.Routes[0].AdapterConfig = json.RawMessage(`{"endpoint_path_mode":"canonical_v1"}`)
	coordinator := &executionCoordinatorStub{authorizeFn: func(_ modelrouting.Plan, route modelrouting.Route, identity modelexecution.AttemptIdentity) (modelexecution.Decision, error) {
		digest, err := executionevidence.Digest(route.Evidence)
		return modelexecution.Decision{Status: modelexecution.Applied, Evidence: route.Evidence, EvidenceHash: digest,
			ProviderAttemptKey: modelexecution.ProviderAttemptKey{OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID}}, err
	}}
	relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default()))
	body := `{"model":"public-model","messages":[{"role":"user","content":[{"type":"text","text":"Read the digits."},` + part + `]}],"max_tokens":48,"modalities":["text"],"reasoning_effort":"none","stream":false}`
	output := 48
	batch, err := relay.prepareModelExecutionRoutes(t.Context(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
		"public-model", "public-model", "fixture", modelExecutionRequestLimits{ExplicitOutputTokens: &output}, []byte(body), time.Now())
	require.NoError(t, err)
	require.True(t, batch.profileEvidence.AudioRequested)
	require.Equal(t, 48, batch.limits.EstimatedOutputTokens)
	upstream, err := providerRequestBody(batch.routes[0].upstreamRequest)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(upstream, &got))
	require.Equal(t, modelprofile.QwenOmniModel, got["model"])
	require.Equal(t, "none", got["reasoning_effort"])
	require.EqualValues(t, 48, got["max_tokens"])
	require.Contains(t, string(upstream), data)
	for _, negative := range []string{
		strings.Replace(body, `,"reasoning_effort":"none"`, "", 1),
		strings.Replace(body, `"none"`, `"low"`, 1),
		strings.Replace(body, `"stream":false`, `"stream":true`, 1),
		strings.Replace(body, data, "https://example.invalid/speech.wav", 1),
		strings.Replace(body, data, "data:audio/wav;base64,AAAA", 1),
		strings.Replace(body, part, part+","+part, 1),
		strings.Replace(body, `"role":"user"`, `"role":"assistant"`, 1),
		strings.Replace(body, part, `{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AAAA"}}`, 1),
	} {
		_, err := relay.prepareModelExecutionRoutes(t.Context(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions,
			"public-model", "public-model", "fixture", modelExecutionRequestLimits{ExplicitOutputTokens: &output}, []byte(negative), time.Now())
		require.Error(t, err)
	}
	for _, tc := range []struct {
		name                          string
		completion, reasoning, status int
	}{
		{"valid audio", 5, 0, http.StatusOK},
		{"unexpected reasoning", 5, 1, http.StatusBadGateway},
		{"over limit", 49, 0, http.StatusBadGateway},
		{"unknown usage", 0, 0, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, settled, held := 0, 0, 0
			account := &mockBilling{
				freezeFn: func(context.Context, billing.FreezeRequest) (*billing.FreezeResult, error) {
					return &billing.FreezeResult{Success: true, AmountMicros: 1, PricingVersion: "price-v1"}, nil
				},
				settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					settled++
					require.Equal(t, metering.UsageSourceUpstream, req.UsageSource)
					require.Equal(t, 171, req.PromptTokens)
					require.Equal(t, 5, req.CompletionTokens)
					return &billing.SettleResult{Success: true}, nil
				},
				holdOutputFn: func(_ context.Context, req billing.SettleRequest, reason string) error {
					held++
					require.Equal(t, tc.completion, req.CompletionTokens)
					require.Equal(t, billing.OutputHoldReasonProviderResponseUnusable, reason)
					return nil
				},
			}
			relay := NewRelay(executionTestDB(), nil, nil, account, nil, key)
			planner := &executionPlannerStub{plan: plan}
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, planner, coordinator, routingpolicy.Default()))
			relay.providerHTTPDoer = &http.Client{Transport: executionRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, origin+"/chat/completions", req.URL.String())
				sent, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Contains(t, string(sent), data)
				usage := ""
				if tc.completion > 0 {
					usage = fmt.Sprintf(`,"usage":{"prompt_tokens":171,"completion_tokens":%d,"total_tokens":%d,"prompt_tokens_details":{"audio_tokens":114,"text_tokens":57,"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":%d,"text_tokens":%d}}`, tc.completion, 171+tc.completion, tc.reasoning, tc.completion-tc.reasoning)
				}
				response := `{"id":"fixture","object":"chat.completion","created":1,"model":"qwen3.8-omni-flash","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"27415"}}]` + usage + `}`
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			rec := httptest.NewRecorder()
			relay.HandleChatCompletion(rec, executionRequest(http.MethodPost, "/v1/chat/completions", "fixture-omni-audio", body))
			require.Equal(t, 1, calls, rec.Body.String())
			require.Contains(t, planner.requests[0].InvocationCapabilities, modelrouting.CapabilityRequirement{CapabilityID: modelprofile.CapabilityAudio, Origins: []string{modelrouting.CapabilityOriginExplicitRequest}})
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.status == http.StatusOK {
				require.Contains(t, rec.Body.String(), "27415")
				require.Equal(t, 1, settled)
				require.Zero(t, held)
			} else {
				require.NotContains(t, rec.Body.String(), "27415")
				require.Zero(t, settled)
				if tc.completion > 0 {
					require.Equal(t, 1, held)
				} else {
					require.Zero(t, held)
				}
			}
		})
	}
}
