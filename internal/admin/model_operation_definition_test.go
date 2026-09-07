package admin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
