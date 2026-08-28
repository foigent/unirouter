package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxM3InlineJPEGCandidateIsAppendOnly(t *testing.T) {
	historical, ok := Lookup("minimax.m3.token-plan", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v5", historical.ProtocolProfile)
	require.NotContains(t, historical.Capabilities, CapabilityVision)

	candidate, ok := Lookup("minimax.m3.token-plan", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v6", candidate.ProtocolProfile)
	require.Contains(t, candidate.Capabilities, CapabilityVision)
	require.NotContains(t, candidate.Capabilities, CapabilityVideo)
	require.Empty(t, candidate.CacheUsageProfile)

	protocol, ok := ResolveChatProtocolContract(candidate.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.InlineImageDataURL)
	require.False(t, protocol.RemoteImageHTTPSURL)
	require.Equal(t, []string{"image/jpeg"}, protocol.InlineImageMIMETypes)
	require.True(t, protocol.ImageInputPolicy.Modeled)
	require.Equal(t, "jpeg", protocol.ImageInputPolicy.DecodedFormat)
	require.Equal(t, []string{"user"}, protocol.ImageInputPolicy.AllowedMessageRoles)
	require.Equal(t, 8, protocol.ImageInputPolicy.MaximumImages)
	require.EqualValues(t, 256*1024, protocol.ImageInputPolicy.MaximumDecodedBytes)
	require.True(t, protocol.ImageInputPolicy.RequiresNonStream)
	require.EqualValues(t, 4, protocol.ImageInputPolicy.AdmissionTokenUpperPerPixel)

	require.False(t, candidate.Current)
	require.Error(t, ValidateNewAttachment(candidate.ID, candidate.Version))
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
}
