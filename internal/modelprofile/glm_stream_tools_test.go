package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGLMStreamProfilesKeepHistoricalAuthorities(t *testing.T) {
	for _, tuple := range []struct{ id, old, next string }{
		{"glm.5.3.api", "1.8.0", "1.9.0"}, {"glm.5.2.api", "1.8.0", "1.9.0"},
		{"glm.5.1.api", "1.0.0", "1.1.0"}, {"glm.5-turbo.api", "1.0.0", "1.1.0"},
		{"glm.5.api", "1.0.0", "1.1.0"}, {"glm.5v-turbo.api", "1.11.0", "1.12.0"},
	} {
		t.Run(tuple.id, func(t *testing.T) {
			old, ok := Lookup(tuple.id, tuple.old)
			require.True(t, ok)
			next, ok := Lookup(tuple.id, tuple.next)
			require.True(t, ok)
			require.False(t, old.Current)
			require.True(t, next.Current)
			require.Error(t, ValidateNewAttachment(old.ID, old.Version))
			require.NoError(t, ValidateNewAttachment(next.ID, next.Version))
			require.Equal(t, old.EndpointClasses, next.EndpointClasses)
			require.Equal(t, []string{EndpointClassGLMOfficialOpenAIV4}, next.EndpointClasses)
			require.Equal(t, old.UpstreamModelIDs, next.UpstreamModelIDs)
			require.Equal(t, old.ReasoningProfile, next.ReasoningProfile)
			require.Equal(t, old.CacheUsageProfile, next.CacheUsageProfile)
			require.Equal(t, old.TokenizerProfileID, next.TokenizerProfileID)
			require.Equal(t, old.DefaultContextWindow, next.DefaultContextWindow)
			require.Equal(t, old.DefaultOutputLimit, next.DefaultOutputLimit)
			require.Equal(t, old.ProviderMaxOutput, next.ProviderMaxOutput)
			require.Equal(t, old.Capabilities, next.Capabilities)
			a, _ := ResolveChatProtocolContract(old.ProtocolProfile)
			b, _ := ResolveChatProtocolContract(next.ProtocolProfile)
			require.False(t, a.StreamContract.ResponseModeled)
			require.True(t, b.StreamContract.FunctionToolDeltas)
			require.True(t, b.StreamContract.RequireDone)
			require.Equal(t, a.InlineImageMIMETypes, b.InlineImageMIMETypes)
			require.Equal(t, a.RemoteFileExtensions, b.RemoteFileExtensions)
			identity, found, err := ResolveOperationProtocolIdentity(tuple.id, tuple.next, OperationOpenAIChat)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, next.ProtocolProfile, identity.ProtocolProfileID)
		})
	}
}
