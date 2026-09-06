package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestExistingFamilyDataCacheProbeUsesFrozenNormalizer(t *testing.T) {
	for _, tc := range []struct{ id, version, protocol string }{
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v10"},
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v11"},
		{"minimax.m2.7-highspeed.payg", "1.2.0", "protocol.minimax-m2-openai-chat.v8"},
		{"minimax.m2.7.payg", "1.3.0", "protocol.minimax-m27-openai-chat.v1"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			p, ok := modelprofile.Lookup(tc.id, tc.version)
			require.True(t, ok)
			p.Version, p.ProtocolProfile = "99.0.0", tc.protocol
			probe := qualification.MiniMaxPAYGM2ChatFunctionToolsProbeContractID
			if tc.id == "minimax.m3.payg" {
				p.ReasoningProfile = "reasoning.minimax-m3-adaptive-embedded.v2"
				probe = qualification.MiniMaxPAYGM3ChatFunctionToolsProbeContractID
			}
			d, err := modelprofile.NewDefinition(p, "minimax/"+strings.ToLower(p.UpstreamModelIDs[0]), "")
			require.NoError(t, err)
			b, err := qualification.BindDefinition(qualification.Binding{OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityPromptCache, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, ProtocolProfileID: tc.protocol, ProtocolProfileVersion: "1.0.0", ProbeContractID: probe, ProbeContractVersion: "2.0.0", ExpectedProvider: p.Provider, ExpectedUpstreamModel: p.UpstreamModelIDs[0], PublicPath: "/v1/chat/completions"}, d)
			require.NoError(t, err)
			key := []byte("0123456789abcdef01234567")
			encrypted, err := auth.EncryptKey([]byte("fixture-only"), key)
			require.NoError(t, err)
			h := NewHandler(nil, "fixture-jwt", key, nil)
			calls := 0
			h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, false, body["stream"])
				require.Contains(t, body["messages"].([]any)[0].(map[string]any)["content"], "unirouter-cache-eligible-prefix")
				tool := map[string]any{"id": "call_echo", "type": "function", "function": map[string]any{"name": "unirouter_probe_echo", "arguments": `{"value":"UNIROUTER_TOOL_PROBE_OK"}`}}
				if tc.id == "minimax.m2.7-highspeed.payg" {
					tool["index"] = 0
				}
				response, err := json.Marshal(map[string]any{"model": p.UpstreamModelIDs[0], "choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "content": "<think>Check echo.</think>", "tool_calls": []any{tool}}}}, "usage": map[string]any{"prompt_tokens": 600, "completion_tokens": 20, "total_tokens": 620, "prompt_tokens_details": map[string]any{"cached_tokens": 128}}})
				require.NoError(t, err)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(response)))}, nil
			}))
			outcome := h.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
				Definition: d, DataBinding: b, Provider: p.Provider, BaseURL: "https://api.minimax.cn/v1", AdapterType: "openai_compatible", AdapterConfig: json.RawMessage(`{}`),
				UpstreamModel: b.ExpectedUpstreamModel, OperationID: b.OperationID, CapabilityID: b.CapabilityID,
				ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, ProtocolProfileID: b.ProtocolProfileID, ProtocolProfileVersion: b.ProtocolProfileVersion,
				ExpectedProvider: b.ExpectedProvider, ExpectedUpstreamModel: b.ExpectedUpstreamModel, PublicPath: b.PublicPath,
				EncryptedSecret: encrypted, ProbeContractID: b.ProbeContractID, ProbeContractVersion: b.ProbeContractVersion,
			}, "data-cache-fixture")
			require.True(t, outcome.ProviderSuccess, outcome.ReasonCode)
			require.True(t, outcome.ContractSuccess, outcome.ReasonCode)
			require.Equal(t, int64(128), outcome.CachedTokens)
			require.Equal(t, 1, calls)
		})
	}
}
