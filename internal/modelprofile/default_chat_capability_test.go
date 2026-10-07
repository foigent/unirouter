package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultChatReasoningCapabilityPreservesAuthorityAndDefaults(t *testing.T) {
	flash, ok := Lookup("deepseek.v4-flash.api", "1.5.0")
	require.True(t, ok)
	require.Equal(t, CapabilityReasoningEffort, DefaultChatReasoningCapability(flash))
	flash.LegacyProtocolAuthority = true
	require.Empty(t, DefaultChatReasoningCapability(flash))

	pro, ok := Lookup("deepseek.v4-pro.api", "1.5.0")
	require.True(t, ok)
	pro.Capabilities = []string{CapabilityReasoning}
	require.Equal(t, CapabilityReasoning, DefaultChatReasoningCapability(pro))

	pro.ReasoningProfile = "reasoning.unknown"
	require.Empty(t, DefaultChatReasoningCapability(pro))
	hy3, ok := Lookup("hy3.api", "1.2.0")
	require.True(t, ok)
	require.Empty(t, DefaultChatReasoningCapability(hy3), "a real disabled provider default requires no reasoning pin")
	require.False(t, ReasoningControlProviderDefaultActive(ReasoningControlContract{ProviderDefault: "disabled"}))
}
