package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
)

func TestModelCapabilityProbePreservesSafeContractFailureEvidence(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	handler := NewHandler(nil, "capability-jwt", encryptionKey, nil)
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		body := strings.Join([]string{
			`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"content":"partial"},"finish_reason":null}]}`,
			"",
			"event: outside-the-typed-deepseek-contract",
			"",
		}, "\r\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}))

	outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
		Provider: "deepseek", BaseURL: "https://api.deepseek.com",
		AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		UpstreamModel:         "deepseek-v4-flash",
		ExpectedUpstreamModel: "deepseek-v4-flash",
		EncryptedSecret:       encrypted,
		ProbeContractID:       modelcapabilityqualification.DeepSeekChatSSEProbeContractID,
		ProbeContractVersion:  modelcapabilityqualification.DeepSeekChatSSEProbeContractCurrentVersion,
	}, "capability-probe-test")

	require.True(t, outcome.ProviderSuccess)
	require.False(t, outcome.ContractSuccess)
	require.Equal(t, "provider_stream_contract_invalid", outcome.ReasonCode)
	require.Equal(t, "unsupported_field", outcome.ContractErrorCode)
	require.Equal(t, "deepseek-v4-flash", outcome.ReportedModel)
	require.Equal(t, 1, outcome.DataEventCount)
	require.Equal(t, 1, outcome.DeltaCount)
	require.Positive(t, outcome.ResponseBytes)
}

func TestModelCapabilityReasoningProbeUsesTypedRequestAndDoesNotReturnPrivateReasoning(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	tests := []struct {
		name                string
		contractID          string
		expectedThinking    string
		expectedEffort      string
		forbiddenRequestKey string
	}{
		{
			name:                "explicit thinking",
			contractID:          modelcapabilityqualification.DeepSeekChatReasoningProbeContractID,
			expectedThinking:    "enabled",
			forbiddenRequestKey: "reasoning_effort",
		},
		{
			name:                "explicit reasoning effort",
			contractID:          modelcapabilityqualification.DeepSeekChatReasoningEffortProbeContractID,
			expectedEffort:      "low",
			forbiddenRequestKey: "thinking",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(nil, "capability-jwt", encryptionKey, nil)
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
				body, readErr := io.ReadAll(request.Body)
				require.NoError(t, readErr)
				var payload map[string]interface{}
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, "deepseek-v4-flash", payload["model"])
				require.Equal(t, false, payload["stream"])
				require.Equal(t, float64(512), payload["max_tokens"])
				require.NotContains(t, payload, test.forbiddenRequestKey)
				if test.expectedThinking != "" {
					require.Equal(t, test.expectedThinking, payload["thinking"].(map[string]interface{})["type"])
				}
				if test.expectedEffort != "" {
					require.Equal(t, test.expectedEffort, payload["reasoning_effort"])
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"model":"deepseek-v4-flash","choices":[{"finish_reason":"stop","message":{"content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private reasoning must not escape"}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`)),
				}, nil
			}))

			outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
				Provider: "deepseek", BaseURL: "https://api.deepseek.com",
				AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
				UpstreamModel: "deepseek-v4-flash", ExpectedUpstreamModel: "deepseek-v4-flash", EncryptedSecret: encrypted,
				ProbeContractID:      test.contractID,
				ProbeContractVersion: modelcapabilityqualification.DeepSeekChatReasoningProbeContractVersion,
			}, "capability-reasoning-probe-test")

			require.True(t, outcome.ProviderSuccess)
			require.True(t, outcome.ContractSuccess)
			require.True(t, outcome.ReasoningObserved)
			require.Equal(t, "ok", outcome.ReasonCode)
			require.Equal(t, int64(19), outcome.TotalTokens)
			safeOutcome, marshalErr := json.Marshal(outcome)
			require.NoError(t, marshalErr)
			require.NotContains(t, string(safeOutcome), "private reasoning must not escape")
		})
	}
}

func TestModelCapabilityToolsAndJSONObjectProbesUseFixedContracts(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	tests := []struct {
		name         string
		capabilityID string
		contractID   string
		responseBody string
		assertBody   func(*testing.T, map[string]interface{})
		assertResult func(*testing.T, modelCapabilityProbeOutcome)
	}{
		{
			name: "named function tool", capabilityID: "tool_choice_named",
			contractID:   modelcapabilityqualification.DeepSeekChatFunctionToolsProbeContractID,
			responseBody: `{"model":"deepseek-v4-flash","choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29}}`,
			assertBody: func(t *testing.T, payload map[string]interface{}) {
				choice := payload["tool_choice"].(map[string]interface{})
				require.Equal(t, "function", choice["type"])
				require.Equal(t, "unirouter_probe_echo", choice["function"].(map[string]interface{})["name"])
			},
			assertResult: func(t *testing.T, outcome modelCapabilityProbeOutcome) {
				require.True(t, outcome.ToolCallObserved)
			},
		},
		{
			name: "json object", capabilityID: "json_object",
			contractID:   modelcapabilityqualification.DeepSeekChatJSONObjectProbeContractID,
			responseBody: `{"model":"deepseek-v4-flash","choices":[{"finish_reason":"stop","message":{"content":"{\"status\":\"UNIROUTER_JSON_PROBE_OK\"}"}}],"usage":{"prompt_tokens":15,"completion_tokens":5,"total_tokens":20}}`,
			assertBody: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, "json_object", payload["response_format"].(map[string]interface{})["type"])
			},
			assertResult: func(t *testing.T, outcome modelCapabilityProbeOutcome) {
				require.True(t, outcome.StructuredObserved)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(nil, "capability-jwt", encryptionKey, nil)
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
				body, readErr := io.ReadAll(request.Body)
				require.NoError(t, readErr)
				var payload map[string]interface{}
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, false, payload["stream"])
				require.Equal(t, "disabled", payload["thinking"].(map[string]interface{})["type"])
				test.assertBody(t, payload)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(test.responseBody)),
				}, nil
			}))
			outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
				Provider: "deepseek", BaseURL: "https://api.deepseek.com",
				AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
				UpstreamModel: "deepseek-v4-flash", ExpectedUpstreamModel: "deepseek-v4-flash", EncryptedSecret: encrypted,
				CapabilityID: test.capabilityID, ProbeContractID: test.contractID,
				ProbeContractVersion: modelcapabilityqualification.DeepSeekChatCoreProbeContractVersion,
			}, "capability-core-probe-test")
			require.True(t, outcome.ProviderSuccess)
			require.True(t, outcome.ContractSuccess)
			require.Equal(t, "ok", outcome.ReasonCode)
			test.assertResult(t, outcome)
			safeOutcome, marshalErr := json.Marshal(outcome)
			require.NoError(t, marshalErr)
			require.NotContains(t, string(safeOutcome), "UNIROUTER_TOOL_PROBE_OK")
			require.NotContains(t, string(safeOutcome), "UNIROUTER_JSON_PROBE_OK")
		})
	}
}

func TestKimiK26CapabilityProbesUseExactProviderOwnedContracts(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	tests := []struct {
		name, capabilityID, contractID, contentType, responseBody string
		expectedMaxCompletionTokens                               float64
		assertBody                                                func(*testing.T, map[string]interface{})
		assertOutcome                                             func(*testing.T, modelCapabilityProbeOutcome)
	}{
		{
			name: "provider default reasoning", capabilityID: "reasoning",
			expectedMaxCompletionTokens: 512,
			contractID:                  modelcapabilityqualification.KimiChatReasoningProbeContractID,
			contentType:                 "application/json",
			responseBody:                `{"id":"chatcmpl-kimi","object":"chat.completion","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"message":{"role":"assistant","content":"UNIROUTER_REASONING_PROBE_OK","reasoning_content":"private"},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"cached_tokens":0}}`,
			assertBody: func(t *testing.T, payload map[string]interface{}) {
				require.NotContains(t, payload, "thinking")
				require.Equal(t, false, payload["stream"])
			},
			assertOutcome: func(t *testing.T, outcome modelCapabilityProbeOutcome) { require.True(t, outcome.ReasoningObserved) },
		},
		{
			name: "auto function tools", capabilityID: "tool_choice_auto",
			expectedMaxCompletionTokens: 256,
			contractID:                  modelcapabilityqualification.KimiChatFunctionToolsProbeContractID,
			contentType:                 "application/json",
			responseBody:                `{"id":"chatcmpl-kimi","object":"chat.completion","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]},"finish_reason":"tool_calls","logprobs":null}],"usage":{"prompt_tokens":20,"completion_tokens":9,"total_tokens":29}}`,
			assertBody: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, "auto", payload["tool_choice"])
				require.Equal(t, "disabled", payload["thinking"].(map[string]interface{})["type"])
			},
			assertOutcome: func(t *testing.T, outcome modelCapabilityProbeOutcome) { require.True(t, outcome.ToolCallObserved) },
		},
		{
			name: "json object", capabilityID: "json_object",
			expectedMaxCompletionTokens: 64,
			contractID:                  modelcapabilityqualification.KimiChatJSONObjectProbeContractID,
			contentType:                 "application/json",
			responseBody:                `{"id":"chatcmpl-kimi","object":"chat.completion","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"message":{"role":"assistant","content":"{\"status\":\"UNIROUTER_JSON_PROBE_OK\"}"},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":15,"completion_tokens":5,"total_tokens":20}}`,
			assertBody: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, "json_object", payload["response_format"].(map[string]interface{})["type"])
			},
			assertOutcome: func(t *testing.T, outcome modelCapabilityProbeOutcome) { require.True(t, outcome.StructuredObserved) },
		},
		{
			name: "streaming", capabilityID: "streaming",
			expectedMaxCompletionTokens: 64,
			contractID:                  modelcapabilityqualification.KimiChatSSEProbeContractID,
			contentType:                 "text/event-stream",
			responseBody: strings.Join([]string{
				`data: {"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{"content":"UNIROUTER_STREAM_PROBE_OK"},"finish_reason":null}],"usage":null}`,
				`data: {"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":0}}`,
				`data: [DONE]`, "",
			}, "\n\n"),
			assertBody: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, true, payload["stream"])
				require.Equal(t, true, payload["stream_options"].(map[string]interface{})["include_usage"])
			},
			assertOutcome: func(t *testing.T, outcome modelCapabilityProbeOutcome) {
				require.Equal(t, 3, outcome.DataEventCount)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(nil, "capability-jwt", encryptionKey, nil)
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
				body, readErr := io.ReadAll(request.Body)
				require.NoError(t, readErr)
				var payload map[string]interface{}
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, "kimi-k2.6", payload["model"])
				require.NotContains(t, payload, "max_tokens")
				require.Equal(t, test.expectedMaxCompletionTokens, payload["max_completion_tokens"])
				test.assertBody(t, payload)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{test.contentType}},
					Body:       io.NopCloser(strings.NewReader(test.responseBody)),
				}, nil
			}))
			outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
				Provider: "kimi", BaseURL: "https://api.moonshot.cn/v1",
				AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
				UpstreamModel: "kimi-k2.6", ExpectedUpstreamModel: "kimi-k2.6", EncryptedSecret: encrypted,
				CapabilityID: test.capabilityID, ProbeContractID: test.contractID,
				ProbeContractVersion: modelcapabilityqualification.KimiChatCoreProbeContractVersion,
			}, "kimi-k26-capability-probe-test")
			require.True(t, outcome.ProviderSuccess)
			require.True(t, outcome.ContractSuccess)
			require.Equal(t, "ok", outcome.ReasonCode)
			test.assertOutcome(t, outcome)
		})
	}
}
