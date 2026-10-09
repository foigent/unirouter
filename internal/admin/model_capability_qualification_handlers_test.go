package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
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
		UpstreamModel: "deepseek-v4-flash", OperationID: modelprofile.OperationOpenAIChat,
		CapabilityID: "streaming", ExecutionProfileID: "deepseek.v4-flash.api", ExecutionProfileVersion: "1.5.0",
		ProtocolProfileID: "protocol.deepseek-openai-chat.v4", ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
		ExpectedProvider: "deepseek", ExpectedUpstreamModel: "deepseek-v4-flash", PublicPath: "/v1/chat/completions",
		EncryptedSecret: encrypted, ProbeContractID: modelcapabilityqualification.DeepSeekChatSSEProbeContractID,
		ProbeContractVersion: modelcapabilityqualification.DeepSeekChatSSEProbeContractCurrentVersion,
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

func TestRepresentativeFunctionToolsProbesUseSharedNormalizedMechanics(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	for _, test := range []struct {
		name, provider, baseURL, adapterConfig, upstreamModel string
		profileID, profileVersion, protocolID, contractID     string
		responseBody, expectedThinking, expectedToolChoice    string
	}{
		{
			name: "minimax normalized envelope", provider: "minimax", baseURL: "https://api.minimaxi.com/v1",
			adapterConfig: `{}`, upstreamModel: "MiniMax-M3",
			profileID: "minimax.m3.token-plan", profileVersion: "1.7.0",
			protocolID: "protocol.minimax-m3-openai-chat.v9", contractID: modelcapabilityqualification.MiniMaxChatFunctionToolsProbeContractID,
			responseBody: `{"id":"chatcmpl-minimax","object":"chat.completion","model":"MiniMax-M3","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","name":"MiniMax AI","audio_content":"","content":"<think>probe</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"prompt_tokens_details":{"cached_tokens":3}},"input_sensitive":false,"output_sensitive":false,"input_sensitive_type":0,"output_sensitive_type":0,"output_sensitive_int":0,"base_resp":{"status_code":0,"status_msg":"success"}}`,
		},
		{
			name: "minimax payg m2 exact binding", provider: "minimax", baseURL: "https://api.minimax.cn/v1",
			adapterConfig: `{}`, upstreamModel: "MiniMax-M2.7",
			profileID: "minimax.m2.7.payg", profileVersion: "1.1.0",
			protocolID: "protocol.minimax-m2-openai-chat.v7", contractID: modelcapabilityqualification.MiniMaxPAYGM2ChatFunctionToolsProbeContractID,
			responseBody: `{"id":"chatcmpl-minimax","object":"chat.completion","model":"MiniMax-M2.7","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","name":"MiniMax AI","audio_content":"","content":"<think>probe</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"prompt_tokens_details":{"cached_tokens":3}},"input_sensitive":false,"output_sensitive":false,"input_sensitive_type":0,"output_sensitive_type":0,"output_sensitive_int":0,"base_resp":{"status_code":0,"status_msg":"success"}}`,
		},
		{
			name: "minimax payg m3 official cn exact binding", provider: "minimax", baseURL: "https://api.minimax.cn/v1",
			adapterConfig: `{}`, upstreamModel: "MiniMax-M3",
			profileID: "minimax.m3.payg", profileVersion: "1.1.0",
			protocolID: "protocol.minimax-m3-openai-chat.v9", contractID: modelcapabilityqualification.MiniMaxPAYGM3ChatFunctionToolsProbeContractID,
			responseBody: `{"id":"chatcmpl-minimax","object":"chat.completion","model":"MiniMax-M3","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","name":"MiniMax AI","audio_content":"","content":"<think>probe</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"prompt_tokens_details":{"cached_tokens":3}},"input_sensitive":false,"output_sensitive":false,"input_sensitive_type":0,"output_sensitive_type":0,"output_sensitive_int":0,"base_resp":{"status_code":0,"status_msg":"success"}}`,
		},
		{
			name: "glm official open platform", provider: "glm", baseURL: "https://open.bigmodel.cn/api/paas/v4",
			adapterConfig: `{"endpoint_path_mode":"base_relative"}`, upstreamModel: "glm-5.3",
			profileID: "glm.5.3.api", profileVersion: "1.8.0",
			protocolID: "protocol.glm-openai-chat.v5", contractID: modelcapabilityqualification.GLMChatFunctionToolsProbeContractID,
			responseBody:     `{"id":"chatcmpl-glm","object":"chat.completion","model":"glm-5.3","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`,
			expectedThinking: "enabled", expectedToolChoice: "auto",
		},
		{
			name: "glm toggle family exact binding", provider: "glm", baseURL: "https://open.bigmodel.cn/api/paas/v4",
			adapterConfig: `{"endpoint_path_mode":"base_relative"}`, upstreamModel: "glm-5.1",
			profileID: "glm.5.1.api", profileVersion: "1.0.0",
			protocolID: "protocol.glm-openai-chat.v6", contractID: modelcapabilityqualification.GLMToggleChatFunctionToolsProbeContractID,
			responseBody:       `{"id":"chatcmpl-glm","object":"chat.completion","model":"glm-5.1","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"reasoning_content":"probe","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`,
			expectedToolChoice: "auto",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(nil, "capability-jwt", encryptionKey, nil)
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, test.baseURL+"/chat/completions", request.URL.String())
				body, readErr := io.ReadAll(request.Body)
				require.NoError(t, readErr)
				var payload map[string]interface{}
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, test.upstreamModel, payload["model"])
				require.Equal(t, false, payload["stream"])
				if test.provider == "minimax" {
					require.Equal(t, float64(256), payload["max_completion_tokens"])
					require.NotContains(t, payload, "max_tokens")
				} else {
					require.Equal(t, float64(256), payload["max_tokens"])
					require.NotContains(t, payload, "max_completion_tokens")
				}
				require.Len(t, payload["tools"], 1)
				if test.expectedThinking == "" {
					require.NotContains(t, payload, "thinking")
				} else {
					require.Equal(t, test.expectedThinking, payload["thinking"].(map[string]interface{})["type"])
				}
				if test.expectedToolChoice == "" {
					require.NotContains(t, payload, "tool_choice")
				} else {
					require.Equal(t, test.expectedToolChoice, payload["tool_choice"])
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(test.responseBody)),
				}, nil
			}))

			outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
				Provider: test.provider, BaseURL: test.baseURL,
				AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(test.adapterConfig),
				UpstreamModel: test.upstreamModel, OperationID: modelprofile.OperationOpenAIChat,
				CapabilityID:       modelprofile.CapabilityFunctionTools,
				ExecutionProfileID: test.profileID, ExecutionProfileVersion: test.profileVersion,
				ProtocolProfileID: test.protocolID, ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
				ExpectedProvider: test.provider, ExpectedUpstreamModel: test.upstreamModel, PublicPath: "/v1/chat/completions",
				EncryptedSecret: encrypted, ProbeContractID: test.contractID,
				ProbeContractVersion: modelcapabilityqualification.OpenAIChatFunctionToolsProbeContractVersion,
			}, "representative-function-tools-probe")

			require.True(t, outcome.ProviderSuccess)
			require.True(t, outcome.ContractSuccess)
			require.True(t, outcome.ToolCallObserved)
			require.Equal(t, "ok", outcome.ReasonCode)
			require.Equal(t, int64(19), outcome.TotalTokens)
		})
	}
}

func TestRepresentativeFunctionToolsProbeRejectsRawDuplicatesBeforeMiniMaxEnvelopeNormalization(t *testing.T) {
	snapshot := modelCapabilitySnapshot{
		Provider: "minimax", BaseURL: "https://api.minimaxi.com/v1",
		AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		UpstreamModel: "MiniMax-M3", OperationID: modelprofile.OperationOpenAIChat,
		CapabilityID:       modelprofile.CapabilityFunctionTools,
		ExecutionProfileID: "minimax.m3.token-plan", ExecutionProfileVersion: "1.7.0",
		ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9", ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
	}
	for _, body := range []string{
		`{"choices":[],"base_resp":{"status_code":0,"status_msg":"success"},"base_resp":{"status_code":0,"status_msg":"success"}}`,
		`{"choices":[],"base_resp":{"status_code":0,"status_msg":"success"},"\u0062ase_resp":{"status_code":0,"status_msg":"success"}}`,
	} {
		response := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}
		_, err := normalizeOpenAIChatFunctionToolsProbeResponse(
			context.Background(), adapters.NewOpenAICompatible(), snapshot, response,
		)
		require.ErrorIs(t, err, strictjson.ErrDuplicateKey)
	}
}

func TestMiniMaxRuntimeCapabilityProbePreservesCachePresenceAndReasoningWithoutPrivateText(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	handler := NewHandler(nil, "capability-jwt", encryptionKey, nil)
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		require.NoError(t, readErr)
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Len(t, payload.Messages, 1)
		require.Greater(t, len(payload.Messages[0].Content), 10_000)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"model":"MiniMax-M3","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"<think>private runtime reasoning</think>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":600,"completion_tokens":7,"total_tokens":607,"prompt_tokens_details":{"cached_tokens":128}}}`,
			)),
		}, nil
	}))

	outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
		Provider: "minimax", BaseURL: "https://api.minimax.cn/v1",
		AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
		UpstreamModel: "MiniMax-M3", OperationID: modelprofile.OperationOpenAIChat,
		CapabilityID: modelprofile.CapabilityPromptCache, ExecutionProfileID: "minimax.m3.payg", ExecutionProfileVersion: "1.2.0",
		ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9", ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
		ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M3", PublicPath: "/v1/chat/completions",
		EncryptedSecret: encrypted, ProbeContractID: modelcapabilityqualification.MiniMaxPAYGM3ChatFunctionToolsProbeContractID,
		ProbeContractVersion: modelcapabilityqualification.OpenAIChatRuntimeFunctionToolsProbeContractV2,
	}, "minimax-runtime-capability-probe-test")

	require.True(t, outcome.ProviderSuccess)
	require.True(t, outcome.ContractSuccess)
	require.True(t, outcome.CacheReadCounterObserved)
	require.Equal(t, int64(128), outcome.CachedTokens)
	require.True(t, outcome.ReasoningObserved)
	require.True(t, outcome.ToolCallObserved)
	safeOutcome, err := json.Marshal(outcome)
	require.NoError(t, err)
	require.NotContains(t, string(safeOutcome), "private runtime reasoning")
}

func TestMiniMaxM27StreamCapabilityProbeUsesExactSharedMechanics(t *testing.T) {
	key := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("fixture-only"), key)
	require.NoError(t, err)
	fixture, err := os.ReadFile("../adapters/testdata/minimax_m27_stream_tools.sse")
	require.NoError(t, err)
	for _, invalidSibling := range []bool{false, true} {
		handler := NewHandler(nil, "fixture-jwt", key, nil)
		handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(body, &payload))
			require.Equal(t, true, payload["stream"])
			require.Equal(t, "auto", payload["tool_choice"])
			require.Equal(t, "https://api.minimax.cn/v1/chat/completions", request.URL.String())
			response := string(fixture)
			if invalidSibling {
				response = strings.Replace(response, `"usage":{"prompt_tokens"`, `"unexpected":true,"usage":{"prompt_tokens"`, 1)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
		}))
		outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
			Provider: "minimax", BaseURL: "https://api.minimax.cn/v1", AdapterType: adapters.TypeOpenAICompatible,
			AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
			UpstreamModel: "MiniMax-M2.7", OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
			ExecutionProfileID: "minimax.m2.7.payg", ExecutionProfileVersion: "1.3.0",
			ProtocolProfileID: "protocol.minimax-m27-openai-chat.v1", ProtocolProfileVersion: "1.0.0",
			ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M2.7", PublicPath: "/v1/chat/completions",
			EncryptedSecret: encrypted, ProbeContractID: modelcapabilityqualification.MiniMaxM27StreamFunctionToolsProbeContractID, ProbeContractVersion: "1.0.0",
		}, "m27-local-fixture")
		require.True(t, outcome.ProviderSuccess)
		require.Equal(t, !invalidSibling, outcome.ContractSuccess)
		require.Equal(t, int64(5585), outcome.PromptTokens)
		require.False(t, outcome.CacheReadCounterObserved)
		safe, err := json.Marshal(outcome)
		require.NoError(t, err)
		require.NotContains(t, string(safe), "<think>")
	}
}

func TestMiniMaxHighspeedCapabilityProbeUsesSharedToolIndexPreparation(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)
	for _, rawIndex := range []string{`0`, `999999999999999999999999999`, `null`, `"0"`, `-1`} {
		t.Run(rawIndex, func(t *testing.T) {
			handler := NewHandler(nil, "capability-jwt", encryptionKey, nil)
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"model":"MiniMax-M2.7-highspeed","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"<think>private fixture</think>","tool_calls":[{"index":` + rawIndex + `,"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":600,"completion_tokens":7,"total_tokens":607,"prompt_tokens_details":{"cached_tokens":128}}}`))}, nil
			}))
			outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
				Provider: "minimax", BaseURL: "https://api.minimax.cn/v1",
				AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
				UpstreamModel: "MiniMax-M2.7-highspeed", OperationID: modelprofile.OperationOpenAIChat,
				CapabilityID: modelprofile.CapabilityFunctionTools, ExecutionProfileID: "minimax.m2.7-highspeed.payg", ExecutionProfileVersion: "1.2.0",
				ProtocolProfileID: "protocol.minimax-m2-openai-chat.v7", ProtocolProfileVersion: "1.0.0",
				ExpectedProvider: "minimax", ExpectedUpstreamModel: "MiniMax-M2.7-highspeed", PublicPath: "/v1/chat/completions",
				EncryptedSecret: encrypted, ProbeContractID: modelcapabilityqualification.MiniMaxPAYGM2ChatFunctionToolsProbeContractID,
				ProbeContractVersion: modelcapabilityqualification.OpenAIChatRuntimeFunctionToolsProbeContractV2,
			}, "highspeed-tool-index-fixture")
			require.True(t, outcome.ProviderSuccess)
			require.Equal(t, rawIndex == `0` || rawIndex == `999999999999999999999999999`, outcome.ContractSuccess)
			safe, err := json.Marshal(outcome)
			require.NoError(t, err)
			require.NotContains(t, string(safe), "private fixture")
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
				`data: {"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{"content":"UNIROUTER_STREAM_PROBE_OK","reasoning_content":""},"finish_reason":null}],"usage":null}`,
				`data: {"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":0,"completion_tokens_details":{"reasoning_tokens":1}}}]}`,
				`data: {"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1787932800,"model":"kimi-k2.6","choices":[],"usage":{"prompt_tokens":16,"completion_tokens":8,"total_tokens":24,"cached_tokens":0,"completion_tokens_details":{"reasoning_tokens":1}}}`,
				`data: [DONE]`, "",
			}, "\n\n"),
			assertBody: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, true, payload["stream"])
				require.Equal(t, true, payload["stream_options"].(map[string]interface{})["include_usage"])
			},
			assertOutcome: func(t *testing.T, outcome modelCapabilityProbeOutcome) {
				require.Equal(t, 4, outcome.DataEventCount)
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
			probeContractVersion := modelcapabilityqualification.KimiChatCoreProbeContractVersionV2
			if test.capabilityID == modelprofile.CapabilityStreaming {
				probeContractVersion = modelcapabilityqualification.KimiChatSSEProbeContractVersionV3
			}
			outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
				Provider: "kimi", BaseURL: "https://api.moonshot.cn/v1",
				AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
				OperationID:        "openai.chat_completions.v1",
				ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.7.0",
				ProtocolProfileID: "protocol.kimi-openai-chat.v5", ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
				ExpectedProvider: "kimi", PublicPath: "/v1/chat/completions",
				UpstreamModel: "kimi-k2.6", ExpectedUpstreamModel: "kimi-k2.6", EncryptedSecret: encrypted,
				CapabilityID: test.capabilityID, ProbeContractID: test.contractID,
				ProbeContractVersion: probeContractVersion,
			}, "kimi-k26-capability-probe-test")
			require.True(t, outcome.ProviderSuccess)
			require.True(t, outcome.ContractSuccess)
			require.Equal(t, "ok", outcome.ReasonCode)
			test.assertOutcome(t, outcome)
		})
	}
}

func TestKimiK26FailedProbeReturnsOnlyBoundedSchemaDiagnostic(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)
	handler := NewHandler(nil, "capability-jwt", encryptionKey, nil)
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(*http.Request) (*http.Response, error) {
		body := `{"id":"chatcmpl-kimi","object":"chat.completion","created":1787932800,"model":"kimi-k2.6","private_payload":"must-not-leak","choices":[],"usage":{}}`
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
		Provider: "kimi", BaseURL: "https://api.moonshot.cn/v1",
		AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		OperationID:        "openai.chat_completions.v1",
		ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.7.0",
		ProtocolProfileID: "protocol.kimi-openai-chat.v5", ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
		ExpectedProvider: "kimi", PublicPath: "/v1/chat/completions",
		UpstreamModel: "kimi-k2.6", ExpectedUpstreamModel: "kimi-k2.6", EncryptedSecret: encrypted,
		CapabilityID:         modelprofile.CapabilityTools,
		ProbeContractID:      modelcapabilityqualification.KimiChatFunctionToolsProbeContractID,
		ProbeContractVersion: modelcapabilityqualification.KimiChatFunctionToolsProbeContractVersionV3,
	}, "kimi-k26-safe-diagnostic-test")
	require.True(t, outcome.ProviderSuccess)
	require.False(t, outcome.ContractSuccess)
	require.Equal(t, "json_unknown_field", outcome.ContractErrorCode)
	require.Equal(t, "private_payload", outcome.DiagnosticMember)
	require.NotContains(t, outcome.DiagnosticMember, "must-not-leak")
}

func TestKimiK27ProbeRequestsUseExactRequiredThinkingContract(t *testing.T) {
	for _, contractID := range []string{
		modelcapabilityqualification.KimiChatSSEProbeContractID,
		modelcapabilityqualification.KimiChatReasoningProbeContractID,
		modelcapabilityqualification.KimiChatFunctionToolsProbeContractID,
		modelcapabilityqualification.KimiChatJSONObjectProbeContractID,
	} {
		t.Run(contractID, func(t *testing.T) {
			snapshot := modelCapabilitySnapshot{
				ExecutionProfileID: "kimi.k2.7-code.api", ExecutionProfileVersion: "1.3.0",
				UpstreamModel: "kimi-k2.7-code", ProbeContractID: contractID,
				CapabilityID: modelprofile.CapabilityTools,
			}
			body, err := buildModelCapabilityProbeRequestBody(snapshot)
			require.NoError(t, err)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(body, &payload))
			require.Equal(t, map[string]interface{}{"type": "enabled", "keep": "all"}, payload["thinking"])
			summary, err := summarizeModelCapabilityProbeRequest(snapshot, body)
			require.NoError(t, err)
			require.Equal(t, "enabled", summary["thinking_type"])
			require.Equal(t, "all", summary["thinking_keep"])
			if contractID == modelcapabilityqualification.KimiChatSSEProbeContractID {
				require.Equal(t, true, summary["stream"])
				require.Equal(t, true, summary["stream_options_include_usage"])
			} else {
				require.Equal(t, false, summary["stream"], "only the SSE contract may stream")
			}
		})
	}

	k26Body, err := buildModelCapabilityProbeRequestBody(modelCapabilitySnapshot{
		ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.7.0",
		UpstreamModel: "kimi-k2.6", ProbeContractID: modelcapabilityqualification.KimiChatSSEProbeContractID,
	})
	require.NoError(t, err)
	var k26Payload map[string]interface{}
	require.NoError(t, json.Unmarshal(k26Body, &k26Payload))
	require.Equal(t, map[string]interface{}{"type": "disabled"}, k26Payload["thinking"])
	k26Summary, err := summarizeModelCapabilityProbeRequest(modelCapabilitySnapshot{
		ExecutionProfileID: "kimi.k2.6.api", ExecutionProfileVersion: "1.7.0",
		UpstreamModel: "kimi-k2.6", ProbeContractID: modelcapabilityqualification.KimiChatSSEProbeContractID,
	}, k26Body)
	require.NoError(t, err)
	require.Equal(t, "disabled", k26Summary["thinking_type"])
	require.NotContains(t, k26Summary, "thinking_keep")

	_, err = buildModelCapabilityProbeRequestBody(modelCapabilitySnapshot{
		ExecutionProfileID: "kimi.k2.7-code.api", ExecutionProfileVersion: "1.2.0",
		UpstreamModel: "kimi-k2.7-code", ProbeContractID: modelcapabilityqualification.KimiChatSSEProbeContractID,
	})
	require.ErrorIs(t, err, errModelCapabilityUnsupported)
}

func TestKimiK3ProbeRequestsUseAlwaysOnReasoningWithoutThinkingToggle(t *testing.T) {
	tests := []struct {
		name, contractID, capabilityID string
		expectedMaxCompletionTokens    float64
		assertPayload                  func(*testing.T, map[string]interface{})
	}{
		{
			name: "streaming", contractID: modelcapabilityqualification.KimiChatSSEProbeContractID,
			capabilityID: modelprofile.CapabilityStreaming, expectedMaxCompletionTokens: 512,
			assertPayload: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, true, payload["stream"])
				require.Equal(t, true, payload["stream_options"].(map[string]interface{})["include_usage"])
				require.Equal(t, "low", payload["reasoning_effort"])
			},
		},
		{
			name: "provider default reasoning", contractID: modelcapabilityqualification.KimiChatReasoningProbeContractID,
			capabilityID: modelprofile.CapabilityReasoning, expectedMaxCompletionTokens: 4096,
		},
		{
			name: "low reasoning effort", contractID: modelcapabilityqualification.KimiChatReasoningEffortProbeContractID,
			capabilityID: modelprofile.CapabilityReasoningEffort, expectedMaxCompletionTokens: 512,
			assertPayload: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, "low", payload["reasoning_effort"])
			},
		},
		{
			name: "automatic tool choice", contractID: modelcapabilityqualification.KimiChatFunctionToolsProbeContractID,
			capabilityID: modelprofile.CapabilityToolChoiceAuto, expectedMaxCompletionTokens: 512,
			assertPayload: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, "auto", payload["tool_choice"])
				require.Equal(t, "low", payload["reasoning_effort"])
			},
		},
		{
			name: "required tool choice", contractID: modelcapabilityqualification.KimiChatFunctionToolsProbeContractID,
			capabilityID: modelprofile.CapabilityToolChoiceReq, expectedMaxCompletionTokens: 512,
			assertPayload: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, "required", payload["tool_choice"])
				require.Equal(t, "low", payload["reasoning_effort"])
			},
		},
		{
			name: "json object", contractID: modelcapabilityqualification.KimiChatJSONObjectProbeContractID,
			capabilityID: modelprofile.CapabilityJSONObject, expectedMaxCompletionTokens: 512,
			assertPayload: func(t *testing.T, payload map[string]interface{}) {
				require.Equal(t, "json_object", payload["response_format"].(map[string]interface{})["type"])
				require.Equal(t, "low", payload["reasoning_effort"])
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := modelCapabilitySnapshot{
				ExecutionProfileID: "kimi.k3.api", ExecutionProfileVersion: "1.9.0",
				UpstreamModel: "kimi-k3", ProbeContractID: test.contractID,
				CapabilityID: test.capabilityID,
			}
			body, err := buildModelCapabilityProbeRequestBody(snapshot)
			require.NoError(t, err)
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(body, &payload))
			require.NotContains(t, payload, "thinking")
			require.Equal(t, test.expectedMaxCompletionTokens, payload["max_completion_tokens"])
			if test.assertPayload != nil {
				test.assertPayload(t, payload)
			}
			summary, err := summarizeModelCapabilityProbeRequest(snapshot, body)
			require.NoError(t, err)
			require.Equal(t, "provider_default_enabled", summary["thinking_type"])
			require.NotContains(t, summary, "thinking_keep")
		})
	}

	_, err := buildModelCapabilityProbeRequestBody(modelCapabilitySnapshot{
		ExecutionProfileID: "kimi.k3.api", ExecutionProfileVersion: "1.8.0",
		UpstreamModel: "kimi-k3", ProbeContractID: modelcapabilityqualification.KimiChatSSEProbeContractID,
		CapabilityID: modelprofile.CapabilityStreaming,
	})
	require.ErrorIs(t, err, errModelCapabilityUnsupported)
}

// Exercise the real admin execution boundary, not just the lower-level parser.
// The exact published Bundle supplies every binding; the HTTP doer is synthetic.
func TestPublishedDeepSeekCompletionProbeReachesResponseValidator(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/deepseek-v41-flash-provider-completion.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	require.Len(t, bundle.Definitions, 1)
	definition, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	fixture, err := os.ReadFile("../adapters/testdata/deepseek_provider_completion_v6.sse")
	require.NoError(t, err)
	key := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("fixture-only"), key)
	require.NoError(t, err)
	for _, row := range bundle.BindingDelta {
		t.Run(row.CapabilityID, func(t *testing.T) {
			binding, err := modelcapabilityqualification.BindDefinition(modelcapabilityqualification.Binding{
				OperationID: row.OperationID, CapabilityID: row.CapabilityID,
				ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion,
				ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion,
				ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion,
				ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath,
			}, definition)
			require.NoError(t, err)
			snapshot := modelCapabilitySnapshot{
				Definition: definition, DataBinding: binding, Provider: row.ExpectedProvider,
				BaseURL: "https://api.deepseek.com", AdapterType: adapters.TypeOpenAICompatible,
				AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
				UpstreamModel: row.ExpectedUpstreamModel, OperationID: row.OperationID, CapabilityID: row.CapabilityID,
				ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion,
				ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion,
				ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath,
				ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion, EncryptedSecret: encrypted,
			}
			for _, tc := range []struct {
				name, stream   string
				valid          bool
				status         int
				transportError bool
			}{
				{"complete", string(fixture), true, 200, false},
				{"missing done", strings.ReplaceAll(string(fixture), "data: [DONE]\n\n", ""), false, 200, false},
				{"missing usage", strings.Replace(string(fixture), `,"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":10,"prompt_cache_miss_tokens":90}`, "", 1), false, 200, false},
				{"wrong model", strings.ReplaceAll(string(fixture), `"model":"deepseek-flash"`, `"model":"deepseek-v4-flash"`), false, 200, false},
				{"completion over limit", strings.ReplaceAll(strings.ReplaceAll(string(fixture), `"completion_tokens":20`, `"completion_tokens":2049`), `"total_tokens":120`, `"total_tokens":2149`), false, 200, false},
				{"http error", string(fixture), false, 401, false},
				{"transport error", "", false, 0, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					h := NewHandler(nil, "fixture-jwt", key, nil)
					calls := 0
					h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(req *http.Request) (*http.Response, error) {
						calls++
						var body map[string]any
						require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
						require.Equal(t, "https://api.deepseek.com/chat/completions", req.URL.String())
						require.Equal(t, "deepseek-flash", body["model"])
						require.EqualValues(t, 2048, body["max_tokens"])
						require.Equal(t, true, body["stream"])
						require.Equal(t, "high", body["reasoning_effort"])
						if tc.transportError {
							return nil, fmt.Errorf("fixture transport failure")
						}
						return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.stream))}, nil
					}))
					outcome := h.executeModelCapabilityProbe(t.Context(), snapshot, "fixture-dispatch")
					require.Equal(t, 1, calls)
					require.Equal(t, tc.status == 200, outcome.ProviderSuccess)
					if tc.transportError {
						require.Equal(t, "provider_request_failed", outcome.ReasonCode)
						require.True(t, outcome.OutcomeAmbiguous)
						return
					}
					if tc.status != 200 {
						require.Equal(t, "provider_http_error", outcome.ReasonCode)
						require.Equal(t, tc.status, outcome.StatusCode)
						return
					}
					require.NotEqual(t, "probe_contract_unsupported", outcome.ReasonCode)
					require.Equal(t, tc.valid, outcome.ContractSuccess, outcome.ReasonCode)
					require.Positive(t, outcome.ResponseBytes)
					if tc.valid {
						require.Equal(t, "tool_calls", outcome.FinishReason)
						require.EqualValues(t, 20, outcome.CompletionTokens)
						require.True(t, outcome.ToolCallObserved)
						require.True(t, outcome.ReasoningObserved)
					}
				})
			}
		})
	}
}

func TestModelCapabilityProbeRejectsUnsupportedResponseBeforeDispatch(t *testing.T) {
	h := NewHandler(nil, "fixture", []byte("0123456789abcdef01234567"), nil)
	calls := 0
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}))
	for _, contract := range []string{"", "unirouter.capability-probe.unknown.v1"} {
		outcome := h.executeModelCapabilityProbe(t.Context(), modelCapabilitySnapshot{ProbeContractID: contract}, "fixture-unsupported")
		require.Equal(t, "probe_contract_unsupported", outcome.ReasonCode)
		require.Equal(t, "not_started", outcome.DispatchState)
		require.False(t, outcome.ProviderSuccess)
		require.False(t, outcome.ContractSuccess)
		require.False(t, outcome.OutcomeAmbiguous)
		require.Zero(t, calls)
	}
}

func TestPublishedCapabilityBindingsHaveActualResponseValidator(t *testing.T) {
	paths, err := filepath.Glob("../../capability-bundles/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	// Selector-only Bundles intentionally refer to a Definition published by
	// a predecessor Bundle. Resolve the exact digest across actual sources.
	definitions := map[string]modelprofile.Definition{}
	for _, path := range paths {
		wire, err := os.ReadFile(path)
		require.NoError(t, err)
		bundle, err := publication.DecodeBundleV2(wire)
		require.NoError(t, err)
		for _, raw := range bundle.Definitions {
			d, err := modelprofile.DecodeDefinition(raw)
			require.NoError(t, err)
			definitions[d.Digest()] = d
		}
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			wire, err := os.ReadFile(path)
			require.NoError(t, err)
			bundle, err := publication.DecodeBundleV2(wire)
			require.NoError(t, err)
			require.NotEmpty(t, bundle.BindingDelta)
			for _, row := range bundle.BindingDelta {
				d, ok := definitions[row.ProfileDefinitionDigest]
				require.True(t, ok, "binding must use its published exact Definition")
				binding, err := modelcapabilityqualification.BindDefinition(modelcapabilityqualification.Binding{
					OperationID: row.OperationID, CapabilityID: row.CapabilityID,
					ExecutionProfileID: row.ExecutionProfileID, ExecutionProfileVersion: row.ExecutionProfileVersion,
					ProtocolProfileID: row.ProtocolProfileID, ProtocolProfileVersion: row.ProtocolProfileVersion,
					ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion,
					ExpectedProvider: row.ExpectedProvider, ExpectedUpstreamModel: row.ExpectedUpstreamModel, PublicPath: row.PublicPath,
				}, d)
				require.NoError(t, err)
				require.NotNil(t, resolveModelCapabilityProbeResponseValidator(modelCapabilitySnapshot{
					Definition: d, DataBinding: binding, ProbeContractID: row.ProbeContractID, ProbeContractVersion: row.ProbeContractVersion,
				}), "%s/%s must reach the actual admin response handler", row.ExecutionProfileID, row.CapabilityID)
			}
		})
	}
}
