package modelprofile

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogIdentitiesAreStableAndUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for _, profile := range All() {
		require.NotEmpty(t, profile.ID)
		require.NotEmpty(t, profile.Version)
		require.NotEmpty(t, profile.TokenizerProfileID)
		require.NotEmpty(t, profile.TokenizerProfileVersion)
		identity := profile.ID + "\x00" + profile.Version
		_, duplicate := seen[identity]
		require.False(t, duplicate, identity)
		seen[identity] = struct{}{}
	}
}

func TestMatchUsesExactNormalizedAliasNotSubstring(t *testing.T) {
	profile, ok := Match(" MiniMax_M2.7 ")
	require.True(t, ok)
	require.Equal(t, "minimax.m2.7.token-plan", profile.ID)

	profile, ok = Match("DeepSeek V4 Pro")
	require.True(t, ok)
	require.Equal(t, "deepseek.v4-pro.api", profile.ID)

	_, ok = Match("fake-MiniMax-M2.7-preview")
	require.False(t, ok)
}

func TestEffectiveLimitsMayNarrowButNeverExceedProfile(t *testing.T) {
	profile, ok := Lookup("minimax.m2.7.token-plan", "1.0.0")
	require.True(t, ok)
	require.NoError(t, ValidateEffectiveLimits(profile, 128_000, 4_096))
	require.Error(t, ValidateEffectiveLimits(profile, 204_801, 4_096))
	require.Error(t, ValidateEffectiveLimits(profile, 128_000, 128_001))
	require.Error(t, ValidateEffectiveLimits(profile, 128_000, 0))
}

func TestMiniMaxOfficialLimitsAndExecutableCapabilitiesRemainExact(t *testing.T) {
	m27, ok := Lookup("minimax.m2.7.token-plan", "1.0.0")
	require.True(t, ok)
	require.Equal(t, 204_800, m27.ProviderContextWindow)
	require.Equal(t, 204_800, m27.ProviderMaxOutput)
	require.Equal(t, 65_536, m27.DefaultOutputLimit)
	require.Equal(t, "protocol.minimax-openai-chat.v2", m27.ProtocolProfile)

	m3, ok := Lookup("minimax.m3.token-plan", "1.0.0")
	require.True(t, ok)
	require.Equal(t, 1_000_000, m3.ProviderContextWindow)
	require.Equal(t, 524_288, m3.ProviderMaxOutput)
	require.Equal(t, 131_072, m3.DefaultOutputLimit)
	require.Contains(t, m3.ProviderCapabilities, "vision")
	require.Contains(t, m3.ProviderCapabilities, "video")
	require.NotContains(t, m3.Capabilities, "vision")
	require.NotContains(t, m3.Capabilities, "video")
}

func TestCatalogReturnsDefensiveCopies(t *testing.T) {
	profiles := All()
	profiles[0].Aliases = append(profiles[0].Aliases, "mutation")
	profiles[len(profiles)-1].ProviderCapabilities = append(profiles[len(profiles)-1].ProviderCapabilities, "mutation")
	again := All()
	require.NotContains(t, again[0].Aliases, "mutation")
	require.NotContains(t, again[len(again)-1].ProviderCapabilities, "mutation")
}

func TestKimiComponentContractsAreVersionedAndDefensive(t *testing.T) {
	k3, ok := Match("kimi-k3")
	require.True(t, ok)
	require.Equal(t, "kimi.k3.api", k3.ID)
	require.Equal(t, "1.3.0", k3.Version)
	require.Equal(t, "reasoning.kimi-k3-effort.v1", k3.ReasoningProfile)
	require.Equal(t, "cache-usage.kimi-platform.v1", k3.CacheUsageProfile)
	require.Contains(t, k3.ProviderCapabilities, "dynamic_tools")
	require.Contains(t, k3.Capabilities, "dynamic_tools")
	require.Contains(t, k3.Capabilities, "partial_mode")
	require.Contains(t, k3.ProviderCapabilities, "builtin_web_search")
	require.NotContains(t, k3.Capabilities, "builtin_web_search")
	require.Contains(t, k3.Capabilities, "vision")
	require.Contains(t, k3.Capabilities, "video")
	require.Contains(t, k3.Capabilities, "json_object")
	require.Contains(t, k3.Capabilities, "json_schema")
	require.Equal(t, 1_048_576, k3.ProviderContextWindow)
	require.Equal(t, 1_048_576, k3.ProviderMaxOutput)

	reasoning, ok := ResolveReasoningContract(k3.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeAlwaysOn, reasoning.Mode)
	require.Equal(t, "reasoning_effort", reasoning.EffortControl.Field)
	require.Equal(t, []string{"low", "high", "max"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, "max", reasoning.EffortControl.ProviderDefault)
	require.Equal(t, ReasoningDefaultProvider, reasoning.DefaultAuthority)
	require.False(t, reasoning.NamedToolChoiceWhileReasoning)
	require.True(t, reasoning.RequiredToolChoiceWhileReasoning)
	require.True(t, reasoning.PreserveAssistantReasoningForTools)

	reasoning.EffortControl.AllowedValues[0] = "mutation"
	again, ok := ResolveReasoningContract(k3.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, "low", again.EffortControl.AllowedValues[0])

	cache, ok := ResolveCacheUsageContract(k3.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageModeAutomatic, cache.Mode)
	require.Equal(t, CacheUsageWireMoonshotCachedTokensV1, cache.WireSchema)
}

func TestKimiK26ThinkingContractRemainsDistinctFromK3(t *testing.T) {
	k26, ok := Lookup("kimi.k2.6.api", "1.0.0")
	require.True(t, ok)
	contract, ok := ResolveReasoningContract(k26.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeToggle, contract.Mode)
	require.Equal(t, "thinking", contract.ToggleControl.Field)
	require.Equal(t, []string{"enabled", "disabled"}, contract.ToggleControl.AllowedValues)
	require.Equal(t, "enabled", contract.ToggleControl.ProviderDefault)
	require.False(t, contract.RequiredToolChoiceWhileReasoning)
}

func TestKimiK26AdvancedChatProfileIsVersionedAndExecutable(t *testing.T) {
	k26, ok := Match("Kimi K2.6")
	require.True(t, ok)
	require.Equal(t, "kimi.k2.6.api", k26.ID)
	require.Equal(t, "1.3.0", k26.Version)
	require.Equal(t, TokenizerKimiK26ID, k26.TokenizerProfileID)
	require.Equal(t, TokenizerKimiK26Version, k26.TokenizerProfileVersion)
	require.Equal(t, "protocol.kimi-openai-chat.v2", k26.ProtocolProfile)
	require.Equal(t, 262_144, k26.ProviderContextWindow)
	for _, capability := range []string{
		CapabilityPartialMode, CapabilityVision, CapabilityVideo,
	} {
		require.Contains(t, k26.Capabilities, capability)
	}
	require.Contains(t, k26.ProviderCapabilities, CapabilityBuiltinWebSearch)
	require.NotContains(t, k26.Capabilities, CapabilityBuiltinWebSearch)
	require.NotContains(t, k26.Capabilities, CapabilityDynamicTools)
	contract, ok := ResolveChatProtocolContract(k26.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.PartialMode)
	require.True(t, contract.BuiltinWebSearch)
	require.True(t, contract.BuiltinSearchRequiresThinkingDisabled)
	require.True(t, contract.InlineImageDataURL)
	require.True(t, contract.InlineVideoDataURL)
	require.Contains(t, contract.InlineImageMIMETypes, "image/png")
	require.Contains(t, contract.InlineVideoMIMETypes, "video/mp4")
	require.False(t, contract.DynamicSystemTools)
}

func TestExecutableCapabilitiesAlwaysHaveTypedComponentContracts(t *testing.T) {
	for _, profile := range All() {
		require.NoError(t, ValidateExecutableComponents(profile), profile.ID+"@"+profile.Version)
	}

	deepSeek, ok := Lookup("deepseek.v4-pro.api", "1.0.0")
	require.True(t, ok)
	reasoning, ok := ResolveReasoningContract(deepSeek.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, "thinking", reasoning.ToggleControl.Field)
	require.Equal(t, []string{"enabled", "disabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, "reasoning_effort", reasoning.EffortControl.Field)
	require.Equal(t, []string{"high", "max"}, reasoning.EffortControl.AllowedValues)
	require.ElementsMatch(t,
		[]string{"temperature", "top_p", "presence_penalty", "frequency_penalty"},
		reasoning.ForbiddenRequestFieldsWhileActive,
	)
	cache, ok := ResolveCacheUsageContract(deepSeek.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageWireDeepSeekHitMissV1, cache.WireSchema)

	for _, identity := range [][2]string{
		{"minimax.m2.7.token-plan", "1.0.0"},
		{"minimax.m2.5-highspeed.token-plan", "1.0.0"},
		{"minimax.m3.token-plan", "1.0.0"},
	} {
		profile, found := Lookup(identity[0], identity[1])
		require.True(t, found)
		require.Contains(t, profile.ProviderCapabilities, "reasoning")
		require.NotContains(t, profile.Capabilities, "reasoning")
	}
}

func TestCatalogEmptyCollectionsRemainJSONArrays(t *testing.T) {
	unverified, ok := Lookup(UnverifiedProfileID, UnverifiedProfileVersion)
	require.True(t, ok)
	require.NotNil(t, unverified.ProviderCapabilities)
	require.NotNil(t, unverified.Capabilities)
	require.NotNil(t, unverified.Aliases)
	require.NotNil(t, unverified.EvidenceRefs)

	payload, err := json.Marshal(unverified)
	require.NoError(t, err)
	require.NotContains(t, string(payload), ":null")
}
