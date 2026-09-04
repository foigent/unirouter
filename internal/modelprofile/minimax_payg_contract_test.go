package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxPAYGOfficialCNEndpointSuccessorsAreCurrentAndHistoricalProfilesRemainAddressable(t *testing.T) {
	tests := []struct {
		profileID, version, upstream, protocol string
		contextWindow, maxOutput               int
	}{
		{"minimax.m3.payg", "1.2.0", "MiniMax-M3", "protocol.minimax-m3-openai-chat.v9", 1_000_000, 524_288},
		{"minimax.m2.7.payg", "1.3.0", "MiniMax-M2.7", "protocol.minimax-m27-openai-chat.v1", 204_800, 204_800},
		{"minimax.m2.7-highspeed.payg", "1.2.0", "MiniMax-M2.7-highspeed", "protocol.minimax-m2-openai-chat.v7", 204_800, 204_800},
	}
	for _, test := range tests {
		profile, ok := Lookup(test.profileID, test.version)
		require.True(t, ok, test.profileID)
		require.True(t, profile.Current)
		require.Equal(t, "minimax", profile.Provider)
		require.Equal(t, []string{test.upstream}, profile.UpstreamModelIDs)
		require.Equal(t, []string{EndpointClassMiniMaxOfficialCNOpenAIV1}, profile.EndpointClasses)
		require.Equal(t, test.protocol, profile.ProtocolProfile)
		require.Equal(t, test.contextWindow, profile.ProviderContextWindow)
		require.Equal(t, test.maxOutput, profile.ProviderMaxOutput)
		require.Contains(t, profile.Capabilities, CapabilityFunctionTools)
		require.NoError(t, ValidateNewAttachment(profile.ID, profile.Version))

		identity, found, err := ResolveOperationProtocolIdentity(profile.ID, profile.Version, OperationOpenAIChat)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, test.protocol, identity.ProtocolProfileID)
		require.Equal(t, ChatOperationProtocolBindingVersionV1, identity.ProtocolProfileVersion)

		require.NoError(t, ValidateProductBinding(profile, ProductBinding{
			Provider: "minimax", BaseURL: "https://api.minimax.cn/v1",
			AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeCanonicalV1,
			OperationID: OperationOpenAIChat, ProtocolProfileID: identity.ProtocolProfileID,
			ProtocolProfileVersion: identity.ProtocolProfileVersion, UpstreamModel: test.upstream,
		}))

		historical, ok := Lookup(test.profileID, "1.0.0")
		require.True(t, ok, test.profileID)
		require.False(t, historical.Current)
		require.Equal(t, []string{EndpointClassMiniMaxCNOpenAIV1}, historical.EndpointClasses)
		require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
		require.NoError(t, ValidateProductBinding(historical, ProductBinding{
			Provider: "minimax", BaseURL: "https://api.minimaxi.com/v1",
			AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeCanonicalV1,
			OperationID: OperationOpenAIChat, ProtocolProfileID: historical.ProtocolProfile,
			ProtocolProfileVersion: identity.ProtocolProfileVersion, UpstreamModel: test.upstream,
		}))
	}

	for _, historical := range []struct{ id, version string }{
		{"minimax.m3.token-plan", "1.7.0"},
		{"minimax.m2.7.token-plan", "1.5.0"},
		{"minimax.m2.5-highspeed.token-plan", "1.5.0"},
	} {
		profile, ok := Lookup(historical.id, historical.version)
		require.True(t, ok)
		require.False(t, profile.Current)
		require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))
	}
}

func TestMiniMaxPAYGProfilesRejectCrossedEndpointIdentityAndUpstream(t *testing.T) {
	profile, ok := Lookup("minimax.m2.7-highspeed.payg", "1.2.0")
	require.True(t, ok)
	identity, found, err := ResolveOperationProtocolIdentity(profile.ID, profile.Version, OperationOpenAIChat)
	require.NoError(t, err)
	require.True(t, found)
	binding := ProductBinding{
		Provider: "minimax", BaseURL: "https://api.minimax.cn/v1",
		AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeCanonicalV1,
		OperationID: OperationOpenAIChat, ProtocolProfileID: identity.ProtocolProfileID,
		ProtocolProfileVersion: identity.ProtocolProfileVersion, UpstreamModel: "MiniMax-M2.7-highspeed",
	}

	wrongEndpoint := binding
	wrongEndpoint.BaseURL = "https://api.minimaxi.com/v1"
	require.Error(t, ValidateProductBinding(profile, wrongEndpoint))
	historical, ok := Lookup("minimax.m2.7-highspeed.payg", "1.0.0")
	require.True(t, ok)
	require.Error(t, ValidateProductBinding(historical, binding))
	wrongModel := binding
	wrongModel.UpstreamModel = "MiniMax-M2.7"
	require.Error(t, ValidateProductBinding(profile, wrongModel))
}
