package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func qwenDataOperationProbeSnapshot(t *testing.T) canonicalOperationProbeSnapshot {
	t.Helper()
	p, ok := modelprofile.Lookup("qwen.3.7-flash.api", "1.7.0")
	require.True(t, ok)
	p.Version, p.ProtocolProfile = "99.0.0", "protocol.qwen-openai-chat.flash.v7"
	p.Capabilities = []string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningBudget, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto}
	p.EvidenceRefs = []string{"https://help.aliyun.com/zh/model-studio/qwen3-7-flash", "https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions", "https://help.aliyun.com/zh/model-studio/qwen-function-calling"}
	d, err := modelprofile.NewDefinition(p, "dashscope/qwen3.7-flash", "")
	require.NoError(t, err)
	_, compiled := modelprofile.Lookup(p.ID, p.Version)
	require.False(t, compiled)
	return canonicalOperationProbeSnapshot{DeploymentID: 41, ConnectionID: 14, CredentialBindingID: 14,
		OperationID: modelprofile.OperationOpenAIChat, UpstreamModel: p.UpstreamModelIDs[0],
		ProfileID: p.ProtocolProfile, ProfileVersion: "1.0.0", ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version,
		ProfileDefinitionDigest: d.Digest(), Definition: d, EncryptedSecret: "must-not-appear"}
}

func TestDataOperationProbeUsesBoundedBooleanAndSafeSummary(t *testing.T) {
	s := qwenDataOperationProbeSnapshot(t)
	wire, err := canonicalOperationProbePayloadForSnapshot(s, "private fixture prompt")
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"qwen3.7-flash","stream":false,"max_completion_tokens":64,"enable_thinking":false,"messages":[{"role":"user","content":"private fixture prompt"}]}`, string(wire))
	summary, err := canonicalOperationProbeRequestSummary(s)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(summary, &fields))
	require.Equal(t, false, fields["enable_thinking"])
	require.Equal(t, false, fields["stream"])
	require.Equal(t, float64(64), fields["max_completion_tokens"])
	require.Equal(t, s.ProfileDefinitionDigest, fields["profile_definition_digest"])
	require.Equal(t, "99.0.0", fields["execution_profile_version"])
	for _, absent := range []string{"messages", "max_tokens", "thinking_type", "thinking_budget", "must-not-appear", "private fixture prompt", "UniRouter connectivity probe"} {
		require.NotContains(t, string(summary), absent)
	}
}

func TestDataOperationProbeRejectsIdentityDriftBeforePayloadOrSummary(t *testing.T) {
	for name, change := range map[string]func(*canonicalOperationProbeSnapshot){
		"digest":           func(s *canonicalOperationProbeSnapshot) { s.ProfileDefinitionDigest = strings.Repeat("0", 64) },
		"protocol":         func(s *canonicalOperationProbeSnapshot) { s.ProfileID = "openai.chat_completions" },
		"protocol version": func(s *canonicalOperationProbeSnapshot) { s.ProfileVersion = "2.0.0" },
		"upstream":         func(s *canonicalOperationProbeSnapshot) { s.UpstreamModel = "unrelated" },
		"operation":        func(s *canonicalOperationProbeSnapshot) { s.OperationID = "anthropic.messages.v1" },
	} {
		t.Run(name, func(t *testing.T) {
			s := qwenDataOperationProbeSnapshot(t)
			change(&s)
			body, err := canonicalOperationProbePayloadForSnapshot(s, "")
			require.ErrorIs(t, err, errCanonicalOperationProbeUnsupported)
			require.Empty(t, body)
			summary, err := canonicalOperationProbeRequestSummary(s)
			require.ErrorIs(t, err, errCanonicalOperationProbeUnsupported)
			require.Empty(t, summary)
		})
	}
}

func TestQwenHostedOperationProbeChecksSentBoundsAndResponse(t *testing.T) {
	raw, err := os.ReadFile("../../capability-bundles/qwen-beijing-hosted-chat.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(raw)
	require.NoError(t, err)
	key := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("fixture-only"), key)
	require.NoError(t, err)
	for _, rawDefinition := range bundle.Definitions {
		d, err := modelprofile.DecodeDefinition(rawDefinition)
		require.NoError(t, err)
		p := d.Profile()
		model := p.UpstreamModelIDs[0]
		t.Run(model, func(t *testing.T) {
			snapshot := canonicalOperationProbeSnapshot{
				Definition: d, ProfileDefinitionDigest: d.Digest(),
				Provider: p.Provider, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", AdapterType: adapters.TypeOpenAICompatible,
				AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"` + modelprofile.EndpointPathModeCanonicalV1 + `"}`),
				OperationID:   modelprofile.OperationOpenAIChat, UpstreamModel: model, EncryptedSecret: encrypted,
				ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version,
				ProfileID: p.ProtocolProfile, ProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
			}
			requiresThinking := p.ReasoningProfile == modelprofile.QwenHostedA95BReasoning
			thinking := 0
			if requiresThinking {
				thinking = 512
			}
			for _, tc := range []struct {
				name              string
				answer, reasoning int
				include, ok       bool
			}{
				{"bounded", 12, thinking, true, true},
				{"official text includes reasoning", 29, min(thinking, 15), true, true},
				{"answer over", 257, 0, true, false},
				{"thinking over", 1, thinking + 1, true, false},
				{"missing partition", 8, 0, false, !requiresThinking},
			} {
				t.Run(tc.name, func(t *testing.T) {
					response := fmt.Sprintf(`{"id":"fixture","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"Ready."},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":%d,"total_tokens":%d`, model, tc.answer+tc.reasoning, 12+tc.answer+tc.reasoning)
					if tc.include {
						response += fmt.Sprintf(`,"completion_tokens_details":{"reasoning_tokens":%d,"text_tokens":%d}`, tc.reasoning, tc.answer+tc.reasoning)
					}
					response += "}}"
					h := NewHandler(nil, "fixture-jwt", key, nil)
					requests := 0
					h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(req *http.Request) (*http.Response, error) {
						requests++
						require.Equal(t, http.MethodPost, req.Method)
						require.Nil(t, req.GetBody)
						require.Equal(t, snapshot.BaseURL+"/chat/completions", req.URL.String())
						var sent map[string]any
						require.NoError(t, json.NewDecoder(req.Body).Decode(&sent))
						require.Equal(t, model, sent["model"])
						require.EqualValues(t, 246, sent["max_tokens"])
						require.NotContains(t, sent, "max_completion_tokens")
						require.NotContains(t, sent, "reasoning_effort")
						if requiresThinking {
							require.EqualValues(t, thinking, sent["thinking_budget"])
							require.NotContains(t, sent, "enable_thinking")
						} else {
							require.Equal(t, false, sent["enable_thinking"])
							require.NotContains(t, sent, "thinking_budget")
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
					}))
					outcome := h.executeCanonicalOperationProbe(t.Context(), snapshot, "", "fixture")
					require.Equal(t, 1, requests, outcome.ReasonCode)
					require.Equal(t, tc.ok, outcome.ProviderSuccess, outcome.ReasonCode)
				})
			}
		})
	}
}
