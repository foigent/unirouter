package modelprofile

import (
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
