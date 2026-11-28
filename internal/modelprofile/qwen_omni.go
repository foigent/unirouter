package modelprofile

import "slices"

const (
	QwenOmniProtocol         = "protocol.qwen-openai-chat.omni-text.v1"
	QwenOmniReasoningProfile = "reasoning.qwen-omni-flash.v1"
	QwenOmniUsageProfile     = "cache-usage.qwen-omni-flash.v1"
	QwenOmniModel            = "qwen3.8-omni-flash"
)

func qwenOmniChatContract() ChatProtocolContract {
	c := boundedChatInlineJPEG(plainTextChatContract(QwenOmniProtocol, "max_tokens"))
	c.OutputLimitMax = 512
	c.BoundedAudioWAV = true
	c.InlineAudioBase64 = true
	c.InlineAudioOmitMIME = true
	c = boundedInlineMP4Contract(c)
	c.InlineVideoOmitMIME = true
	c.InlineAudioFormats = []string{"wav"}
	c.RequiredDisabledReasoningEffort = "none"
	c.AssistantReasoningResponseModeled = true
	return c
}

func QwenOmniChatProfile(p Profile) bool {
	return p.ProtocolProfile == QwenOmniProtocol && p.Provider == "dashscope" && p.Region == "cn-beijing" &&
		p.APISurface == APISurfaceOpenAIChat && p.ReasoningProfile == QwenOmniReasoningProfile &&
		p.CacheUsageProfile == QwenOmniUsageProfile && !p.LegacyProtocolAuthority &&
		slices.Equal(p.Operations, []string{OperationOpenAIChat}) &&
		slices.Equal(p.EndpointClasses, []string{EndpointClassDashScopeBeijingWorkspacePayGo}) &&
		slices.Equal(p.UpstreamModelIDs, []string{QwenOmniModel}) &&
		slices.Equal(p.Capabilities, []string{CapabilityAudio, CapabilityVideo, CapabilityVision}) &&
		p.TokenizerProfileID == TokenizerByteUpperID && p.TokenizerProfileVersion == TokenizerByteUpperVersion &&
		p.ExecutableMaxOutput > 0 && p.ExecutableMaxOutput <= 512
}
