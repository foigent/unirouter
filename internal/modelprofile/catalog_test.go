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

func TestCurrentProviderProfilesPreserveDocumentedMaximumAndRequestDefault(t *testing.T) {
	tests := []struct {
		model       string
		profileID   string
		context     int
		maximum     int
		defaultOut  int
		protocolRef string
	}{
		{"glm-5.3", "glm.5.3.api", 1_000_000, 131_072, 65_536, "protocol.glm-openai-chat.documented.v1"},
		{"glm-5.2", "glm.5.2.api", 1_000_000, 131_072, 65_536, "protocol.glm-openai-chat.documented.v1"},
		{"glm-5v-turbo", "glm.5v-turbo.api", 200_000, 131_072, 65_536, "protocol.glm-openai-chat.documented.v1"},
		{"qwen3.8-max", "qwen.3.8-max.api", 1_000_000, 131_072, 131_072, "protocol.qwen-openai-chat.documented.v1"},
		{"qwen3.7-plus", "qwen.3.7-plus.api", 1_000_000, 131_072, 131_072, "protocol.qwen-openai-chat.documented.v1"},
		{"qwen3.7-flash", "qwen.3.7-flash.api", 1_000_000, 131_072, 131_072, "protocol.qwen-openai-chat.documented.v1"},
		{"gemini-3.7-flash", "gemini.3.7-flash.api", 1_048_576, 65_536, 32_768, "protocol.gemini-openai-chat.documented.v1"},
		{"gemini-3.5-flash", "gemini.3.5-flash.api", 1_048_576, 65_536, 32_768, "protocol.gemini-openai-chat.documented.v1"},
		{"gemini-3.5-flash-lite", "gemini.3.5-flash-lite.api", 1_048_576, 65_536, 32_768, "protocol.gemini-openai-chat.documented.v1"},
		{"mimo-v2.5-pro", "mimo.v2.5-pro.api", 1_000_000, 131_072, 131_072, "protocol.mimo-openai-chat.documented.v1"},
		{"mimo-v2.5", "mimo.v2.5.api", 1_000_000, 131_072, 32_768, "protocol.mimo-openai-chat.documented.v1"},
		{"hy3", "hy3.api", 262_144, 131_072, 16_384, "protocol.hy3-openai-chat.documented.v1"},
		{"gpt-5.6-sol", "openai.gpt-5.6-sol.api", 1_050_000, 128_000, 32_768, "protocol.openai-responses.documented.v1"},
		{"gpt-5.6-terra", "openai.gpt-5.6-terra.api", 1_050_000, 128_000, 32_768, "protocol.openai-responses.documented.v1"},
		{"gpt-5.6-luna", "openai.gpt-5.6-luna.api", 1_050_000, 128_000, 32_768, "protocol.openai-responses.documented.v1"},
		{"grok-4.6", "xai.grok-4.6.api", 500_000, 0, 32_768, "protocol.xai-openai-chat.documented.v1"},
		{"claude-opus-5", "anthropic.claude-opus-5.api", 1_000_000, 128_000, 32_768, "protocol.anthropic-messages.documented.v1"},
		{"claude-sonnet-5", "anthropic.claude-sonnet-5.api", 1_000_000, 128_000, 32_768, "protocol.anthropic-messages.documented.v1"},
		{"claude-haiku-4-5-20251001", "anthropic.claude-haiku-4.5.api", 200_000, 64_000, 16_384, "protocol.anthropic-messages.documented.v1"},
		{"doubao-seed-evolving", "doubao.seed-evolving.api", 1_048_576, 262_144, 32_768, "protocol.doubao-openai-chat.documented.v1"},
		{"doubao-seed-2-1-pro-260628", "doubao.seed-2.1-pro.api", 262_144, 262_144, 32_768, "protocol.doubao-openai-chat.documented.v1"},
		{"doubao-seed-2-1-turbo-260628", "doubao.seed-2.1-turbo.api", 262_144, 262_144, 32_768, "protocol.doubao-openai-chat.documented.v1"},
	}
	require.Len(t, tests, 22)
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			profile, ok := Match(test.model)
			require.True(t, ok)
			require.Equal(t, test.profileID, profile.ID)
			require.Equal(t, test.context, profile.ProviderContextWindow)
			require.Equal(t, test.maximum, profile.ProviderMaxOutput)
			require.Equal(t, test.defaultOut, profile.DefaultOutputLimit)
			require.Equal(t, test.protocolRef, profile.ProtocolProfile)
			require.NotEmpty(t, profile.ProviderCapabilities)
			require.Empty(t, profile.Capabilities, "documented provider claims must not become executable before conformance")
		})
	}
}

func TestKimiComponentContractsAreVersionedAndDefensive(t *testing.T) {
	k3, ok := Match("kimi-k3")
	require.True(t, ok)
	require.Equal(t, "kimi.k3.api", k3.ID)
	require.Equal(t, "1.5.0", k3.Version)
	require.Equal(t, "reasoning.kimi-k3-effort.v2", k3.ReasoningProfile)
	require.Equal(t, "cache-usage.kimi-platform.v1", k3.CacheUsageProfile)
	require.Contains(t, k3.ProviderCapabilities, "dynamic_tools")
	require.Contains(t, k3.Capabilities, "dynamic_tools")
	require.Contains(t, k3.Capabilities, "partial_mode")
	require.Contains(t, k3.ProviderCapabilities, "builtin_web_search")
	require.Contains(t, k3.Capabilities, "builtin_web_search")
	require.Contains(t, k3.Capabilities, "vision")
	require.Contains(t, k3.Capabilities, "video")
	require.Contains(t, k3.Capabilities, "json_object")
	require.Contains(t, k3.Capabilities, "json_schema")
	require.Equal(t, 1_048_576, k3.ProviderContextWindow)
	require.Equal(t, 1_048_576, k3.ProviderMaxOutput)
	require.Equal(t, 131_072, k3.DefaultOutputLimit)

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
	require.Equal(t, []string{"none", "auto", "required"}, reasoning.AllowedToolChoices)
	require.Equal(t, "1", reasoning.FixedRequestNumbers["temperature"])
	require.Equal(t, "0.95", reasoning.FixedRequestNumbers["top_p"])

	reasoning.EffortControl.AllowedValues[0] = "mutation"
	reasoning.AllowedToolChoices[0] = "mutation"
	reasoning.FixedRequestNumbers["temperature"] = "999"
	again, ok := ResolveReasoningContract(k3.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, "low", again.EffortControl.AllowedValues[0])
	require.Equal(t, "none", again.AllowedToolChoices[0])
	require.Equal(t, "1", again.FixedRequestNumbers["temperature"])

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
	require.Equal(t, "1.5.0", k26.Version)
	require.Equal(t, TokenizerKimiK26ID, k26.TokenizerProfileID)
	require.Equal(t, TokenizerKimiK26Version, k26.TokenizerProfileVersion)
	require.Equal(t, "reasoning.kimi-k2.6-thinking-toggle.v2", k26.ReasoningProfile)
	require.Equal(t, "protocol.kimi-openai-chat.v3", k26.ProtocolProfile)
	require.Equal(t, 262_144, k26.ProviderContextWindow)
	for _, capability := range []string{
		"json_object", CapabilityPartialMode, CapabilityVision, CapabilityVideo, "prompt_cache",
	} {
		require.Contains(t, k26.Capabilities, capability)
	}
	require.Contains(t, k26.ProviderCapabilities, CapabilityBuiltinWebSearch)
	require.Contains(t, k26.Capabilities, CapabilityBuiltinWebSearch)
	require.NotContains(t, k26.Capabilities, CapabilityDynamicTools)
	contract, ok := ResolveChatProtocolContract(k26.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.PartialMode)
	require.True(t, contract.BuiltinWebSearch)
	require.False(t, contract.BuiltinSearchRequiresThinkingDisabled)
	require.True(t, contract.BuiltinSearchRequiresNonStream)
	require.True(t, contract.BuiltinSearchRequiresSerial)
	require.True(t, contract.InlineImageDataURL)
	require.True(t, contract.InlineVideoDataURL)
	require.Contains(t, contract.InlineImageMIMETypes, "image/png")
	require.Contains(t, contract.InlineVideoMIMETypes, "video/mp4")
	require.False(t, contract.DynamicSystemTools)
	reasoning, ok := ResolveReasoningContract(k26.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, []string{"all"}, reasoning.ThinkingKeepValues)
	require.Equal(t, "null", reasoning.ThinkingKeepProviderDefault)
	require.Equal(t, []string{"none", "auto"}, reasoning.AllowedToolChoices)
	require.Equal(t, "0.6", reasoning.FixedRequestNumbersWhenDisabled["temperature"])
}

func TestKimiK27PlatformProfileDoesNotReuseCodingSurface(t *testing.T) {
	k27, ok := Match("kimi-k2.7-code")
	require.True(t, ok)
	require.Equal(t, "kimi.k2.7-code.api", k27.ID)
	require.Equal(t, "1.1.0", k27.Version)
	require.Equal(t, "reasoning.kimi-k2.7-thinking-required.v2", k27.ReasoningProfile)
	require.Equal(t, "cache-usage.kimi-platform.v1", k27.CacheUsageProfile)
	require.Equal(t, "protocol.kimi-openai-chat.v2", k27.ProtocolProfile)
	require.Equal(t, 262_144, k27.ProviderContextWindow)
	require.Equal(t, 32_768, k27.DefaultOutputLimit)
	for _, capability := range []string{
		"reasoning", "tools", "json_object", CapabilityPartialMode,
		CapabilityVision, CapabilityVideo, "prompt_cache",
	} {
		require.Contains(t, k27.Capabilities, capability)
	}
	require.NotContains(t, k27.ProviderCapabilities, CapabilityBuiltinWebSearch)
	require.NotContains(t, k27.Capabilities, CapabilityDynamicTools)
	require.NotContains(t, k27.Capabilities, "json_schema")

	reasoning, ok := ResolveReasoningContract(k27.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeRequired, reasoning.Mode)
	require.Equal(t, []string{"enabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, []string{"all"}, reasoning.ThinkingKeepValues)
	require.Equal(t, "all", reasoning.ThinkingKeepProviderDefault)
	require.Equal(t, []string{"none", "auto"}, reasoning.AllowedToolChoices)
}

func TestLatestDeepSeekProfilesAcceptOnlyDocumentedCompatibilityEffortAliases(t *testing.T) {
	for _, model := range []string{"DeepSeek V4 Flash", "DeepSeek V4 Pro"} {
		profile, ok := Match(model)
		require.True(t, ok)
		require.Equal(t, "1.1.0", profile.Version)
		require.Equal(t, "reasoning.deepseek-thinking-toggle.v2", profile.ReasoningProfile)
		reasoning, found := ResolveReasoningContract(profile.ReasoningProfile)
		require.True(t, found)
		require.Equal(t, map[string]string{
			"low": "high", "medium": "high", "xhigh": "max",
		}, reasoning.EffortControl.CompatibilityValues)
		reasoning.EffortControl.CompatibilityValues["medium"] = "mutation"
		again, resolved := ResolveReasoningContract(profile.ReasoningProfile)
		require.True(t, resolved)
		require.Equal(t, "high", again.EffortControl.CompatibilityValues["medium"])
	}
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
