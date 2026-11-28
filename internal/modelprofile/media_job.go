package modelprofile

import (
	"slices"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

const OpenAIImageFlare = endpointprotocol.OpenAIImageFlare

// MediaJobProfile admits a single media artifact under an exact typed profile. Prompt
// limits are UTF-8 bytes and never substitute for actual provider usage.
func MediaJobProfile(p Profile) bool {
	if (p.APISurface != APISurfaceOpenAIImages && p.APISurface != APISurfaceVideoJobs && p.APISurface != APISurfaceMediaTools) || !slices.Equal(p.Operations, []string{OperationMediaJobs}) ||
		len(p.UpstreamModelIDs) != 1 || !SupportsMediaJobBinding(p.ProtocolProfile, p.Provider, p.UpstreamModelIDs[0]) ||
		p.ProtocolProfileVersion != "1.0.0" || p.ProviderMaxOutput != 1 || p.DefaultOutputLimit != 1 ||
		p.DefaultOutputAuthority != DefaultOutputPolicy || p.ReasoningProfile != "" || p.CacheUsageProfile != "" ||
		p.TokenizerProfileID != "" || p.TokenizerProfileVersion != "" || p.OperationPolicyProfile != "" ||
		p.ProcessingTierPolicy != "" || p.LegacyProtocolAuthority || len(p.Capabilities) != 0 || len(p.AlwaysRequiredCapabilities) != 0 {
		return false
	}
	if (p.APISurface == APISurfaceVideoJobs) != endpointprotocol.IsVideoJobProfileID(p.ProtocolProfile) ||
		(p.APISurface == APISurfaceMediaTools) != endpointprotocol.IsMediaToolJobProfileID(p.ProtocolProfile) {
		return false
	}
	promptLimit := 32000
	endpoint := EndpointClassOpenAIOfficialOrigin
	switch p.ProtocolProfile {
	case endpointprotocol.ProfileDashScopeWanVideo:
		promptLimit = endpointprotocol.DashScopeWanMaxPromptBytes
		endpoint = EndpointClassDashScopeBeijingMediaOrigin
	case endpointprotocol.ProfileDashScopeQwenImage:
		promptLimit = endpointprotocol.DashScopeQwenMaxPromptBytes
		endpoint = EndpointClassDashScopeBeijingMediaOrigin
	case endpointprotocol.ProfileDeepSeekASR:
		promptLimit = endpointprotocol.MaxASRWAVBytes
		endpoint = EndpointClassDeepSeekOfficialOpenAI
	case endpointprotocol.ProfileDeepSeekTTS:
		promptLimit = endpointprotocol.MediaTTSMaxTextBytes
		endpoint = EndpointClassDeepSeekOfficialOpenAI
	case endpointprotocol.ProfileGLMOCR:
		promptLimit = endpointprotocol.MaxOCRPNGBytes
		endpoint = EndpointClassGLMOfficialMediaOrigin
	case endpointprotocol.ProfileDeepSeekOCR:
		promptLimit = endpointprotocol.MaxOCRPNGBytes
		endpoint = EndpointClassDeepSeekOfficialOpenAI
	case endpointprotocol.ProfileGLMImage:
		promptLimit = endpointprotocol.GLMImagePromptBytes
		endpoint = EndpointClassGLMOfficialMediaOrigin
	case endpointprotocol.ProfileGLMCogVideo:
		promptLimit = endpointprotocol.GLMCogVideoPromptBytes
		endpoint = EndpointClassGLMOfficialMediaOrigin
	case endpointprotocol.ProfileMiniMaxASR:
		promptLimit = endpointprotocol.MaxASRWAVBytes
		endpoint = EndpointClassMiniMaxOfficialCNMediaOrigin
	case endpointprotocol.ProfileMiniMaxTTS:
		promptLimit = endpointprotocol.MediaTTSMaxTextBytes
		endpoint = EndpointClassMiniMaxOfficialCNMediaOrigin
	case endpointprotocol.ProfileGLMASR:
		promptLimit = endpointprotocol.MaxASRWAVBytes
		endpoint = EndpointClassGLMOfficialMediaOrigin
	case endpointprotocol.ProfileGLMTTS:
		promptLimit = endpointprotocol.MediaTTSMaxTextBytes
		endpoint = EndpointClassGLMOfficialMediaOrigin
	case endpointprotocol.ProfileOpenAITranscribe:
		promptLimit = endpointprotocol.MaxASRWAVBytes
		endpoint = EndpointClassOpenAIOfficialOrigin
	case endpointprotocol.ProfileMiMoASR:
		promptLimit = endpointprotocol.MaxASRWAVBytes
		endpoint = EndpointClassMiMoChinaSpeechOrigin
	case endpointprotocol.ProfileMiMoTTS:
		promptLimit = endpointprotocol.MediaTTSMaxTextBytes
		endpoint = EndpointClassMiMoChinaSpeechOrigin
	case endpointprotocol.ProfileDashScopeASR:
		promptLimit = endpointprotocol.MaxASRWAVBytes
		endpoint = EndpointClassDashScopeBeijingMediaOrigin
	case endpointprotocol.ProfileDashScopeTTS:
		promptLimit = endpointprotocol.MediaTTSMaxTextBytes
		endpoint = EndpointClassDashScopeBeijingMediaOrigin
	case endpointprotocol.ProfileMiniMaxH3Video:
		promptLimit = endpointprotocol.MiniMaxH3VideoPromptBytes
		endpoint = EndpointClassMiniMaxOfficialCNMediaOrigin
	case endpointprotocol.ProfileMiniMaxH3Image:
		promptLimit = endpointprotocol.MiniMaxImagePromptBytes
		endpoint = EndpointClassMiniMaxOfficialCNMediaOrigin
	}
	return slices.Equal(p.EndpointClasses, []string{endpoint}) &&
		p.ProviderContextWindow == promptLimit && p.DefaultContextWindow == promptLimit
}

func ImageJobProfile(p Profile) bool {
	return p.APISurface == APISurfaceOpenAIImages && MediaJobProfile(p)
}

func SupportsMediaJobBinding(protocol, provider, model string) bool {
	switch protocol {
	case endpointprotocol.ProfileDashScopeWanVideo:
		return provider == "dashscope" && model == endpointprotocol.DashScopeWanVideo
	case endpointprotocol.ProfileOpenAIImageGPT:
		return provider == "openai" && model == OpenAIImageFlare
	case endpointprotocol.ProfileDashScopeQwenImage:
		return provider == "dashscope" && endpointprotocol.IsDashScopeQwenImageModel(model)
	case endpointprotocol.ProfileDeepSeekASR:
		return provider == "deepseek" && model == endpointprotocol.DeepSeekASR
	case endpointprotocol.ProfileDeepSeekTTS:
		return provider == "deepseek" && model == endpointprotocol.DeepSeekTTS
	case endpointprotocol.ProfileGLMOCR:
		return provider == "glm" && model == endpointprotocol.GLMOCR
	case endpointprotocol.ProfileDeepSeekOCR:
		return provider == "deepseek" && model == endpointprotocol.DeepSeekOCR
	case endpointprotocol.ProfileGLMImage:
		return provider == "glm" && model == endpointprotocol.GLMImage
	case endpointprotocol.ProfileGLMCogVideo:
		return provider == "glm" && model == endpointprotocol.GLMCogVideo
	case endpointprotocol.ProfileMiniMaxASR:
		return provider == "minimax" && model == endpointprotocol.MiniMaxASR
	case endpointprotocol.ProfileMiniMaxTTS:
		return provider == "minimax" && model == endpointprotocol.MiniMaxTTS
	case endpointprotocol.ProfileGLMASR:
		return provider == "glm" && model == endpointprotocol.GLMASR
	case endpointprotocol.ProfileGLMTTS:
		return provider == "glm" && model == endpointprotocol.GLMTTS
	case endpointprotocol.ProfileOpenAITranscribe:
		return provider == "openai" && model == endpointprotocol.OpenAITranscribe
	case endpointprotocol.ProfileMiMoASR:
		return provider == "mimo" && model == endpointprotocol.MiMoASR
	case endpointprotocol.ProfileMiMoTTS:
		return provider == "mimo" && model == endpointprotocol.MiMoTTS
	case endpointprotocol.ProfileDashScopeASR:
		return provider == "dashscope" && model == endpointprotocol.DashScopeASR
	case endpointprotocol.ProfileDashScopeTTS:
		return provider == "dashscope" && model == endpointprotocol.DashScopeTTS
	case endpointprotocol.ProfileMiniMaxH3Video:
		return provider == "minimax" && model == endpointprotocol.MiniMaxH3Video
	case endpointprotocol.ProfileMiniMaxH3Image:
		return provider == "minimax" && model == endpointprotocol.MiniMaxH3Image
	default:
		return false
	}
}
