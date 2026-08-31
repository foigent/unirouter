package gateway

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
)

func TestKimiK26FixedProbeRequestsPassExactProfileValidationAndAdmission(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.6.api", "1.7.0")
	tests := []struct {
		name string
		body string
	}{
		{
			name: "streaming",
			body: `{"model":"kimi-k2.6","messages":[{"role":"user","content":"Reply with exactly: UNIROUTER_STREAM_PROBE_OK"}],"max_completion_tokens":64,"stream":true,"stream_options":{"include_usage":true},"thinking":{"type":"disabled"}}`,
		},
		{
			name: "provider default reasoning",
			body: `{"model":"kimi-k2.6","messages":[{"role":"user","content":"Reply with exactly: UNIROUTER_REASONING_PROBE_OK"}],"max_completion_tokens":512,"stream":false}`,
		},
		{
			name: "automatic function tools",
			body: `{"model":"kimi-k2.6","messages":[{"role":"user","content":"Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}],"max_completion_tokens":256,"stream":false,"thinking":{"type":"disabled"},"tools":[{"type":"function","function":{"name":"unirouter_probe_echo","description":"Return the supplied probe value through a function call.","parameters":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}}}],"tool_choice":"auto"}`,
		},
		{
			name: "json object",
			body: `{"model":"kimi-k2.6","messages":[{"role":"system","content":"Return valid JSON only."},{"role":"user","content":"Return exactly {\"status\":\"UNIROUTER_JSON_PROBE_OK\"}."}],"max_completion_tokens":64,"stream":false,"thinking":{"type":"disabled"},"response_format":{"type":"json_object"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := validateChatExecutionProfileRequest([]byte(test.body), plan)
			require.NoError(t, err)
			evidence.bindAdmissionBody([]byte(test.body))
			_, err = estimateChatAdmissionForValidatedPlan([]byte(test.body), plan, evidence)
			require.NoError(t, err)

			legacyField := strings.Replace(test.body, "max_completion_tokens", "max_tokens", 1)
			_, err = validateChatExecutionProfileRequest([]byte(legacyField), plan)
			require.Error(t, err, "the K2.6 v5 contract must not accept the legacy output field")
		})
	}
}

func TestKimiK27FixedProbeRequestsPassExactProfileValidationAndAdmission(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k2.7-code.api", "1.3.0")
	tests := []struct {
		name string
		body string
	}{
		{
			name: "streaming required thinking",
			body: `{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"Reply with exactly: UNIROUTER_STREAM_PROBE_OK"}],"max_completion_tokens":64,"stream":true,"stream_options":{"include_usage":true},"thinking":{"type":"enabled","keep":"all"}}`,
		},
		{
			name: "reasoning required thinking",
			body: `{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"Reply with exactly: UNIROUTER_REASONING_PROBE_OK"}],"max_completion_tokens":512,"stream":false,"thinking":{"type":"enabled","keep":"all"}}`,
		},
		{
			name: "automatic function tools required thinking",
			body: `{"model":"kimi-k2.7-code","messages":[{"role":"user","content":"Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}],"max_completion_tokens":256,"stream":false,"thinking":{"type":"enabled","keep":"all"},"tools":[{"type":"function","function":{"name":"unirouter_probe_echo","description":"Return the supplied probe value through a function call.","parameters":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}}}],"tool_choice":"auto"}`,
		},
		{
			name: "json object required thinking",
			body: `{"model":"kimi-k2.7-code","messages":[{"role":"system","content":"Return valid JSON only."},{"role":"user","content":"Return exactly {\"status\":\"UNIROUTER_JSON_PROBE_OK\"}."}],"max_completion_tokens":64,"stream":false,"thinking":{"type":"enabled","keep":"all"},"response_format":{"type":"json_object"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(test.body)
			evidence, err := validateChatExecutionProfileRequest(body, plan)
			require.NoError(t, err)
			evidence.bindAdmissionBody(body)
			_, err = estimateChatAdmissionForValidatedPlan(body, plan, evidence)
			require.NoError(t, err)

			disabled := strings.Replace(test.body, `"type":"enabled","keep":"all"`, `"type":"disabled"`, 1)
			_, err = validateChatExecutionProfileRequest([]byte(disabled), plan)
			require.Error(t, err, "K2.7 required-thinking Probe must not degrade to the K2.6 disabled form")

			legacyOutput := strings.Replace(test.body, "max_completion_tokens", "max_tokens", 1)
			_, err = validateChatExecutionProfileRequest([]byte(legacyOutput), plan)
			require.Error(t, err, "K2.7 v5 Probe must not use the legacy output field")
		})
	}

	k26Plan := executionProfilePlan(t, "kimi.k2.6.api", "1.7.0")
	k26Historical := []byte(`{"model":"kimi-k2.6","messages":[{"role":"user","content":"Reply with exactly: UNIROUTER_STREAM_PROBE_OK"}],"max_completion_tokens":64,"stream":true,"stream_options":{"include_usage":true},"thinking":{"type":"disabled"}}`)
	k26Evidence, err := validateChatExecutionProfileRequest(k26Historical, k26Plan)
	require.NoError(t, err, "the historical K2.6 Probe request must remain valid")
	k26Evidence.bindAdmissionBody(k26Historical)
	_, err = estimateChatAdmissionForValidatedPlan(k26Historical, k26Plan, k26Evidence)
	require.NoError(t, err)
	_, err = validateChatExecutionProfileRequest(k26Historical, plan)
	require.Error(t, err, "the K2.6 disabled-thinking Probe must not be admitted under K2.7")
}

func TestKimiK3FixedProbeRequestsPassExactProfileValidationAndAdmission(t *testing.T) {
	plan := executionProfilePlan(t, "kimi.k3.api", "1.9.0")
	tool := `{"type":"function","function":{"name":"unirouter_probe_echo","description":"Return the supplied probe value through a function call.","parameters":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}}}`
	tests := []struct {
		name string
		body string
	}{
		{
			name: "streaming always-on reasoning",
			body: `{"model":"kimi-k3","messages":[{"role":"user","content":"Reply with exactly: UNIROUTER_STREAM_PROBE_OK"}],"max_completion_tokens":512,"stream":true,"stream_options":{"include_usage":true},"reasoning_effort":"low"}`,
		},
		{
			name: "provider default reasoning",
			body: `{"model":"kimi-k3","messages":[{"role":"user","content":"Reply with exactly: UNIROUTER_REASONING_PROBE_OK"}],"max_completion_tokens":4096,"stream":false}`,
		},
		{
			name: "explicit low reasoning effort",
			body: `{"model":"kimi-k3","messages":[{"role":"user","content":"Reply with exactly: UNIROUTER_REASONING_PROBE_OK"}],"max_completion_tokens":512,"stream":false,"reasoning_effort":"low"}`,
		},
		{
			name: "automatic function tools",
			body: `{"model":"kimi-k3","messages":[{"role":"user","content":"Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}],"max_completion_tokens":512,"stream":false,"reasoning_effort":"low","tools":[` + tool + `],"tool_choice":"auto"}`,
		},
		{
			name: "required function tools",
			body: `{"model":"kimi-k3","messages":[{"role":"user","content":"Call unirouter_probe_echo with value UNIROUTER_TOOL_PROBE_OK. Do not answer directly."}],"max_completion_tokens":512,"stream":false,"reasoning_effort":"low","tools":[` + tool + `],"tool_choice":"required"}`,
		},
		{
			name: "json object",
			body: `{"model":"kimi-k3","messages":[{"role":"system","content":"Return valid JSON only."},{"role":"user","content":"Return exactly {\"status\":\"UNIROUTER_JSON_PROBE_OK\"}."}],"max_completion_tokens":512,"stream":false,"reasoning_effort":"low","response_format":{"type":"json_object"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(test.body)
			require.NotContains(t, test.body, `"thinking"`)
			evidence, err := validateChatExecutionProfileRequest(body, plan)
			require.NoError(t, err)
			evidence.bindAdmissionBody(body)
			_, err = estimateChatAdmissionForValidatedPlan(body, plan, evidence)
			require.NoError(t, err)

			withK2Thinking := strings.Replace(test.body, `"stream":false`, `"stream":false,"thinking":{"type":"enabled","keep":"all"}`, 1)
			if withK2Thinking != test.body {
				_, err = validateChatExecutionProfileRequest([]byte(withK2Thinking), plan)
				require.Error(t, err, "K3 must not inherit a K2 thinking request contract")
			}
		})
	}
}

func TestKimiK26SuccessfulNonStreamProbeFixturesAreDeliverable(t *testing.T) {
	tests := []struct {
		name, body, capability, contractID string
		validate                           func(string) error
	}{
		{
			name: "reasoning", capability: modelprofile.CapabilityReasoning,
			contractID: modelcapabilityqualification.KimiChatReasoningProbeContractID,
			body:       `{"id":"chatcmpl-kimi","object":"chat.completion","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"message":{"role":"assistant","content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private"},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"cached_tokens":0,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":4}}}`,
			validate: func(body string) error {
				_, err := modelcapabilityqualification.ValidateKimiChatReasoning(
					modelcapabilityqualification.KimiChatReasoningProbeContractID,
					modelcapabilityqualification.KimiChatCoreProbeContractVersionV2,
					"kimi-k2.6", "application/json", strings.NewReader(body),
				)
				return err
			},
		},
		{
			name: "tools", capability: modelprofile.CapabilityToolChoiceAuto,
			contractID: modelcapabilityqualification.KimiChatFunctionToolsProbeContractID,
			body:       `{"id":"chatcmpl-kimi","object":"chat.completion","created":1787932800,"model":"kimi-k2.6","service_tier":"standard","choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":"","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"finish_reason":"tool_calls","logprobs":null}],"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29,"cached_tokens":0,"completion_tokens_details":{"reasoning_tokens":0,"provider_tool_tokens":4}}}`,
			validate: func(body string) error {
				_, err := modelcapabilityqualification.ValidateKimiChatFunctionTools(
					modelcapabilityqualification.KimiChatFunctionToolsProbeContractID,
					modelcapabilityqualification.KimiChatFunctionToolsProbeContractVersionV4,
					"kimi-k2.6", "application/json", strings.NewReader(body),
				)
				return err
			},
		},
		{
			name: "json object", capability: modelprofile.CapabilityJSONObject,
			contractID: modelcapabilityqualification.KimiChatJSONObjectProbeContractID,
			body:       `{"id":"chatcmpl-kimi","object":"chat.completion","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"message":{"role":"assistant","content":"{\"status\":\"UNIROUTER_JSON_PROBE_OK\"}"},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":15,"completion_tokens":5,"total_tokens":20,"cached_tokens":0,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
			validate: func(body string) error {
				_, err := modelcapabilityqualification.ValidateKimiChatJSONObject(
					modelcapabilityqualification.KimiChatJSONObjectProbeContractID,
					modelcapabilityqualification.KimiChatCoreProbeContractVersionV2,
					"kimi-k2.6", "application/json", strings.NewReader(body),
				)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, test.validate(test.body))
			canonicalBody := []byte(test.body)
			if test.capability == modelprofile.CapabilityToolChoiceAuto {
				var changed bool
				var err error
				canonicalBody, changed, err = adapters.NormalizeOpenAICompatibleChatToolCallIndex(
					"kimi", "kimi-k2.6", "protocol.kimi-openai-chat.v5", canonicalBody,
				)
				require.NoError(t, err)
				require.True(t, changed)
				require.NotContains(t, string(canonicalBody), `"tool_calls":[{"index"`)
			}
			var response map[string]interface{}
			require.NoError(t, decodeJSONUseNumber(canonicalBody, &response))
			route := &RouteResult{
				Channel: models.Channel{Provider: "kimi"}, MappedModel: "kimi-k2.6",
				ModelProtocolProfile: "protocol.kimi-openai-chat.v5",
			}
			if test.capability == modelprofile.CapabilityToolChoiceAuto {
				route.PlanCapabilityRequirements = []modelrouting.CapabilityRequirement{{
					CapabilityID: modelprofile.CapabilityToolChoiceAuto,
					Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
					FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{
						DeclaredFunctionNames: []string{"unirouter_probe_echo"},
					},
				}}
			}
			require.True(t, inspectNonStreamOutputTextsForRoute(response, route).complete(), test.contractID)

			unknown := strings.Replace(test.body, `"model":"kimi-k2.6",`, `"model":"kimi-k2.6","unknown":"x",`, 1)
			require.Error(t, test.validate(unknown))
		})
	}
}

func TestKimiK26SuccessfulStreamProbeFramesAreDeliverable(t *testing.T) {
	frames := []string{
		`{"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{"content":"UNIROUTER_STREAM_PROBE_OK","reasoning_content":""},"finish_reason":null}],"usage":null}`,
		`{"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":0,"completion_tokens_details":{"reasoning_tokens":1}}}]}`,
		`{"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":0,"completion_tokens_details":{"reasoning_tokens":1}}}`,
	}
	bodyParts := make([]string, 0, len(frames)+2)
	for _, frame := range frames {
		bodyParts = append(bodyParts, "data: "+frame)
	}
	bodyParts = append(bodyParts, "data: [DONE]", "")
	body := strings.Join(bodyParts, "\n\n")
	_, err := modelcapabilityqualification.ValidateKimiChatSSE(
		modelcapabilityqualification.KimiChatSSEProbeContractID,
		modelcapabilityqualification.KimiChatSSEProbeContractVersionV3,
		"kimi-k2.6", "text/event-stream", strings.NewReader(body),
	)
	require.NoError(t, err)
	usage, present, err := adapters.ParseOpenAICompatibleUsageWithCacheProfile(
		[]byte(frames[2]), adapters.EndpointChatCompletions,
		metering.UsageSourceUpstream, "cache-usage.kimi-platform.v1",
	)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 0, usage.CacheReadTokens)

	for _, raw := range frames {
		normalized, err := normalizeProviderStreamFrameForRoute(&RouteResult{
			Channel: models.Channel{Provider: "kimi"}, MappedModel: "kimi-k2.6",
			ModelProtocolProfile: "protocol.kimi-openai-chat.v5",
		}, []byte(raw))
		require.NoError(t, err)
		frame, err := parseGuardedStreamFrameForProtocol(normalized, "protocol.kimi-openai-chat.v5")
		require.NoError(t, err)
		require.False(t, frame.unsupported)
	}
}

func TestKimiK3SuccessfulProbeFixturesAreDeliverable(t *testing.T) {
	exactProfile, ok := modelprofile.Lookup("kimi.k3.api", "1.9.0")
	require.True(t, ok)
	require.Equal(t, "reasoning.kimi-k3-effort.v2", exactProfile.ReasoningProfile)
	require.Equal(t, "protocol.kimi-openai-chat.k3.v5", exactProfile.ProtocolProfile)

	usageV2 := `{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29,"cached_tokens":2,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":4}}`
	tests := []struct {
		name, body, capability string
		validate               func(string) error
	}{
		{
			name: "reasoning", capability: modelprofile.CapabilityReasoning,
			body: `{"id":"chatcmpl-k3","object":"chat.completion","created":1787932800,"model":"kimi-k3","choices":[{"index":0,"message":{"role":"assistant","content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private reasoning"},"finish_reason":"stop","logprobs":null}],"usage":` + usageV2 + `}`,
			validate: func(body string) error {
				_, err := modelcapabilityqualification.ValidateKimiChatReasoning(
					modelcapabilityqualification.KimiChatReasoningProbeContractID,
					modelcapabilityqualification.KimiChatCoreProbeContractVersionV2,
					"kimi-k3", "application/json", strings.NewReader(body),
				)
				return err
			},
		},
		{
			name: "reasoning effort", capability: modelprofile.CapabilityReasoningEffort,
			body: `{"id":"chatcmpl-k3","object":"chat.completion","created":1787932800,"model":"kimi-k3","choices":[{"index":0,"message":{"role":"assistant","content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private reasoning"},"finish_reason":"stop","logprobs":null}],"usage":` + usageV2 + `}`,
			validate: func(body string) error {
				_, err := modelcapabilityqualification.ValidateKimiChatReasoning(
					modelcapabilityqualification.KimiChatReasoningEffortProbeContractID,
					modelcapabilityqualification.KimiChatReasoningEffortProbeContractVersion,
					"kimi-k3", "application/json", strings.NewReader(body),
				)
				return err
			},
		},
		{
			name: "required tools", capability: modelprofile.CapabilityToolChoiceReq,
			body: `{"id":"chatcmpl-k3","object":"chat.completion","created":1787932800,"model":"kimi-k3","choices":[{"index":0,"message":{"role":"assistant","content":null,"reasoning_content":"select tool","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"finish_reason":"tool_calls","logprobs":null}],"usage":` + usageV2 + `}`,
			validate: func(body string) error {
				_, err := modelcapabilityqualification.ValidateKimiChatFunctionTools(
					modelcapabilityqualification.KimiChatFunctionToolsProbeContractID,
					modelcapabilityqualification.KimiChatFunctionToolsProbeContractVersionV4,
					"kimi-k3", "application/json", strings.NewReader(body),
				)
				return err
			},
		},
		{
			name: "json object", capability: modelprofile.CapabilityJSONObject,
			body: `{"id":"chatcmpl-k3","object":"chat.completion","created":1787932800,"model":"kimi-k3","choices":[{"index":0,"message":{"role":"assistant","content":"{\"status\":\"UNIROUTER_JSON_PROBE_OK\"}","reasoning_content":"private reasoning"},"finish_reason":"stop","logprobs":null}],"usage":` + usageV2 + `}`,
			validate: func(body string) error {
				_, err := modelcapabilityqualification.ValidateKimiChatJSONObject(
					modelcapabilityqualification.KimiChatJSONObjectProbeContractID,
					modelcapabilityqualification.KimiChatCoreProbeContractVersionV2,
					"kimi-k3", "application/json", strings.NewReader(body),
				)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, test.validate(test.body))
			canonicalBody := []byte(test.body)
			if test.capability == modelprofile.CapabilityToolChoiceReq {
				var changed bool
				var err error
				canonicalBody, changed, err = adapters.NormalizeOpenAICompatibleChatToolCallIndex(
					"kimi", "kimi-k3", exactProfile.ProtocolProfile, canonicalBody,
				)
				require.NoError(t, err)
				require.True(t, changed)
				require.NotContains(t, string(canonicalBody), `"tool_calls":[{"index"`)
			}
			var response map[string]interface{}
			require.NoError(t, decodeJSONUseNumber(canonicalBody, &response))
			route := &RouteResult{
				Channel:                 models.Channel{Provider: "kimi"},
				MappedModel:             "kimi-k3",
				ExecutionProfileID:      exactProfile.ID,
				ExecutionProfileVersion: exactProfile.Version,
				ModelProtocolProfile:    exactProfile.ProtocolProfile,
				ReasoningProfile:        exactProfile.ReasoningProfile,
				CacheUsageProfile:       exactProfile.CacheUsageProfile,
			}
			if test.capability == modelprofile.CapabilityToolChoiceReq {
				route.PlanCapabilityRequirements = []modelrouting.CapabilityRequirement{{
					CapabilityID: modelprofile.CapabilityToolChoiceReq,
					Origins:      []string{modelrouting.CapabilityOriginExplicitRequest},
					FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{
						DeclaredFunctionNames: []string{"unirouter_probe_echo"},
					},
				}}
			}
			require.True(t, inspectNonStreamOutputTextsForRoute(response, route).complete())
		})
	}

	frames := []string{
		`{"id":"chatcmpl-k3","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k3","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"private reasoning"},"finish_reason":null}],"usage":null}`,
		`{"id":"chatcmpl-k3","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k3","choices":[{"index":0,"delta":{"content":"UNIROUTER_STREAM_PROBE_OK","reasoning_content":""},"finish_reason":null}],"usage":null}`,
		`{"id":"chatcmpl-k3","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k3","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":` + usageV2 + `}]}`,
		`{"id":"chatcmpl-k3","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k3","choices":[],"usage":` + usageV2 + `}`,
	}
	parts := make([]string, 0, len(frames)+2)
	for _, frame := range frames {
		parts = append(parts, "data: "+frame)
	}
	parts = append(parts, "data: [DONE]", "")
	observation, err := modelcapabilityqualification.ValidateKimiChatSSE(
		modelcapabilityqualification.KimiChatSSEProbeContractID,
		modelcapabilityqualification.KimiChatSSEProbeContractVersionV4,
		"kimi-k3", "text/event-stream", strings.NewReader(strings.Join(parts, "\n\n")),
	)
	require.NoError(t, err)
	require.True(t, observation.DoneSeen)
	require.True(t, observation.ReasoningObserved)

	route := &RouteResult{
		Channel:                 models.Channel{Provider: "kimi"},
		MappedModel:             "kimi-k3",
		ExecutionProfileID:      exactProfile.ID,
		ExecutionProfileVersion: exactProfile.Version,
		ModelProtocolProfile:    exactProfile.ProtocolProfile,
		ReasoningProfile:        exactProfile.ReasoningProfile,
		CacheUsageProfile:       exactProfile.CacheUsageProfile,
	}
	for _, raw := range frames {
		normalized, err := normalizeProviderStreamFrameForRoute(route, []byte(raw))
		require.NoError(t, err)
		frame, err := parseGuardedStreamFrameForProtocol(normalized, route.ModelProtocolProfile)
		require.NoError(t, err)
		require.False(t, frame.unsupported)
	}
}

func TestKimiK26StreamNormalizationRejectsDuplicateMembersBeforeDelivery(t *testing.T) {
	route := &RouteResult{
		Channel: models.Channel{Provider: "kimi"}, MappedModel: "kimi-k2.6",
		ModelProtocolProfile: "protocol.kimi-openai-chat.v5",
	}
	_, err := normalizeProviderStreamFrameForRoute(route, []byte(
		`{"choices":[{"index":0,"delta":{"content":"sensitive","\u0063ontent":"safe","reasoning_content":""},"finish_reason":null}]}`,
	))
	require.Error(t, err)
}
