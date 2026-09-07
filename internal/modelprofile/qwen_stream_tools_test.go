package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQwenStreamContractIsExplicitAndLeavesV6Unchanged(t *testing.T) {
	old, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.flash.v6")
	require.True(t, ok)
	next, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.flash.v7")
	require.True(t, ok)
	require.False(t, old.StreamContract.FunctionToolDeltas)
	require.False(t, old.ExactFunctionToolHistoryModeled)
	require.True(t, next.StreamContract.FunctionToolDeltas)
	require.True(t, next.ExactFunctionToolHistoryModeled)
	require.True(t, next.ExactFunctionToolResponseModeled)
	require.True(t, next.StreamContract.RequireDone)
	require.True(t, next.StreamContract.RequireTerminalUsage)
	require.Equal(t, old.OutputLimitMax, next.OutputLimitMax)
	require.Equal(t, old.OutputLimitAcceptedFields, next.OutputLimitAcceptedFields)
	require.Equal(t, []string{"auto"}, next.FunctionToolChoices)
	require.False(t, next.NamedFunctionToolChoice)
	require.False(t, next.ParallelFunctionToolCalls)
	require.False(t, next.BuiltinSearchRequest)
	require.False(t, next.InlineImageDataURL)
	p, ok := Lookup("qwen.3.7-flash.api", "1.7.0")
	require.True(t, ok)
	p.Version = "1.8.0"
	p.ProtocolProfile = next.ProfileID
	p.Capabilities = []string{CapabilityStreaming, CapabilityReasoning, CapabilityReasoningBudget, CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto}
	p.EvidenceRefs = []string{"https://help.aliyun.com/zh/model-studio/qwen3-7-flash", "https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions", "https://help.aliyun.com/zh/model-studio/qwen-function-calling"}
	d, err := NewDefinition(p, "dashscope/qwen3.7-flash", "")
	require.NoError(t, err)
	identity, err := d.OperationProtocolIdentity(OperationOpenAIChat)
	require.NoError(t, err)
	require.Equal(t, next.ProfileID, identity.ProtocolProfileID)
	_, compiled := Lookup(p.ID, p.Version)
	require.False(t, compiled)
}
