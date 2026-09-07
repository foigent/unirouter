package modelprofile

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParameterControlsDescribeExactContractsWithoutChangingProfile(t *testing.T) {
	flash := Profile{ReasoningProfile: "reasoning.qwen3.7-hybrid.v1", ProtocolProfile: "protocol.qwen-openai-chat.v1"}
	before, err := json.Marshal(flash)
	require.NoError(t, err)
	c := DescribeParameterControls(flash)
	require.Equal(t, ParameterControlsVersion, c.Version)
	require.Equal(t, "boolean", c.Reasoning.Toggle.WireType)
	require.Equal(t, "enable_thinking", c.Reasoning.Toggle.Field)
	require.Nil(t, c.Reasoning.Effort)
	require.EqualValues(t, 262144, c.Reasoning.Budget.Maximum)
	require.EqualValues(t, 262144, c.Reasoning.Budget.ProviderDefault)
	max := DescribeParameterControls(Profile{ReasoningProfile: "reasoning.qwen3.8-max-hybrid.v2"})
	require.Equal(t, []string{"low", "medium", "xhigh"}, max.Reasoning.Effort.Values)
	require.Equal(t, "xhigh", max.Reasoning.Effort.Aliases["high"])
	require.EqualValues(t, 0, max.Reasoning.Budget.Minimum)
	require.EqualValues(t, 131072, max.Reasoning.Budget.ProviderDefault)
	require.True(t, max.Reasoning.Budget.MutuallyExclusiveWithEffort)
	max.Reasoning.Effort.Aliases["high"] = "forged"
	c.Reasoning.Toggle.Values[0] = "forged"
	require.Equal(t, "xhigh", DescribeParameterControls(Profile{ReasoningProfile: "reasoning.qwen3.8-max-hybrid.v2"}).Reasoning.Effort.Aliases["high"])
	require.Equal(t, "enabled", DescribeParameterControls(flash).Reasoning.Toggle.Values[0])
	after, err := json.Marshal(flash)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}

func TestParameterControlsPreserveLegacyPathsRequiredAndUnknown(t *testing.T) {
	ds := DescribeParameterControls(Profile{ReasoningProfile: "reasoning.deepseek-v4-pro-thinking-toggle.v5"})
	require.Equal(t, "thinking", ds.Reasoning.Toggle.Object)
	require.Equal(t, "type", ds.Reasoning.Toggle.Field)
	require.Equal(t, "string", ds.Reasoning.Toggle.WireType)
	require.Empty(t, ds.Reasoning.Effort.Object)
	require.Equal(t, "reasoning_effort", ds.Reasoning.Effort.Field)
	mini := DescribeParameterControls(Profile{ReasoningProfile: "reasoning.minimax-m27-required-embedded.v2"})
	require.Equal(t, ReasoningModeRequired, mini.Reasoning.Mode)
	require.Nil(t, mini.Reasoning.Toggle)
	require.Nil(t, mini.Reasoning.Budget)
	unknown := DescribeParameterControls(Profile{})
	require.Nil(t, unknown.Reasoning)
	require.Nil(t, unknown.Output)
	for _, p := range All() {
		d := DescribeParameterControls(p)
		if d.Reasoning == nil {
			continue
		}
		for _, c := range []*ParameterControl{d.Reasoning.Toggle, d.Reasoning.Effort} {
			if c != nil {
				require.Contains(t, []string{"string", "boolean"}, c.WireType, p.ID)
			}
		}
	}
}
