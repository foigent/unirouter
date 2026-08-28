package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

func TestQwenProductBindingAcceptsOnlyBeijingPayGoChatEndpoints(t *testing.T) {
	profile, ok := Lookup("qwen.3.8-max.api", "1.8.0")
	require.True(t, ok)
	base := ProductBinding{
		Provider: "dashscope", AdapterType: "openai_compatible", OperationID: OperationOpenAIChat,
		EndpointPathMode: EndpointPathModeCanonicalV1, UpstreamModel: "qwen3.8-max",
	}
	for _, endpoint := range []string{
		"https://workspace-123.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		"https://dashscope.aliyuncs.com/compatible-mode/v1",
	} {
		binding := base
		binding.BaseURL = endpoint
		require.NoError(t, ValidateProductBinding(profile, binding), endpoint)
	}
	for _, endpoint := range []string{
		"https://workspace.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
		"https://trial.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		"https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		"https://nested.workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1",
		"https://workspace.cn-beijing.maas.aliyuncs.com/api/v1",
	} {
		binding := base
		binding.BaseURL = endpoint
		require.Error(t, ValidateProductBinding(profile, binding), endpoint)
	}

	wrongProvider := base
	wrongProvider.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	wrongProvider.Provider = "openrouter"
	require.Error(t, ValidateProductBinding(profile, wrongProvider))
	wrongOperation := base
	wrongOperation.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	wrongOperation.OperationID = OperationOpenAIResponses
	require.Error(t, ValidateProductBinding(profile, wrongOperation))
	wrongUpstreamModel := base
	wrongUpstreamModel.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	wrongUpstreamModel.UpstreamModel = "qwen3.7-plus"
	require.Error(t, ValidateProductBinding(profile, wrongUpstreamModel))
}

func TestStableProductIdentityCannotBeReinterpretedByCurrentManifest(t *testing.T) {
	profile, ok := Lookup("openai.gpt-5.6-sol.api", "1.0.0")
	require.True(t, ok)
	require.Equal(t, "openai", profile.Provider)
	require.Equal(t, "global", profile.Region)
	require.Equal(t, APISurfaceOpenAIResponses, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIResponses}, profile.Operations)
	require.Equal(t, []string{EndpointClassOpenAIOfficialOrigin}, profile.EndpointClasses)
	require.Equal(t, []string{"gpt-5.6-sol"}, profile.UpstreamModelIDs)

	drifted := profile
	drifted.Region = "cn-beijing"
	require.Error(t, ValidateExecutableComponents(drifted))
}

func TestOpenAIProductBindingAcceptsOnlyOfficialV1Endpoint(t *testing.T) {
	profile, ok := Lookup("openai.gpt-5.6-sol.chat", "1.0.0")
	require.True(t, ok)
	binding := ProductBinding{
		Provider: "openai", BaseURL: "https://api.openai.com/v1",
		AdapterType: "openai_compatible", OperationID: OperationOpenAIChat,
		EndpointPathMode: EndpointPathModeCanonicalV1, UpstreamModel: "gpt-5.6-sol",
	}
	require.NoError(t, ValidateProductBinding(profile, binding))
	binding.BaseURL = "https://api.openai.com/v1/"
	require.NoError(t, ValidateProductBinding(profile, binding))

	for _, endpoint := range []string{
		"https://api.openai.com",
		"https://arbitrary.example/v1",
		"https://api.openai.com/v1/chat/completions",
		"https://api.openai.com:443/v1",
		"http://api.openai.com/v1",
	} {
		candidate := binding
		candidate.BaseURL = endpoint
		require.Error(t, ValidateProductBinding(profile, candidate), endpoint)
	}
}

func TestClaudeSonnet5ProductBindingAcceptsOnlyOfficialAnthropicOrigin(t *testing.T) {
	for _, test := range []struct {
		version, protocolProfile string
	}{
		{version: "1.1.0", protocolProfile: endpointprotocol.ProfileAnthropicMessagesText},
		{version: "1.2.0", protocolProfile: endpointprotocol.ProfileAnthropicMessagesText},
		{version: "1.3.0", protocolProfile: endpointprotocol.ProfileAnthropicMessagesTextV2},
	} {
		t.Run(test.version, func(t *testing.T) {
			profile, ok := Lookup("anthropic.claude-sonnet-5.api", test.version)
			require.True(t, ok)
			binding := ProductBinding{
				Provider: "anthropic", BaseURL: "https://api.anthropic.com",
				AdapterType: "anthropic_native", OperationID: OperationAnthropicMessage,
				EndpointPathMode:  EndpointPathModeCanonicalV1,
				ProtocolProfileID: test.protocolProfile, ProtocolProfileVersion: "1.0.0",
				UpstreamModel: "claude-sonnet-5",
			}
			require.NoError(t, ValidateProductBinding(profile, binding))
			binding.BaseURL += "/"
			require.NoError(t, ValidateProductBinding(profile, binding))
			for _, endpoint := range []string{
				"https://provider.example",
				"https://api.anthropic.com/v1",
				"https://api.anthropic.com/v1/messages",
				"https://api.anthropic.com:443",
				"http://api.anthropic.com",
			} {
				candidate := binding
				candidate.BaseURL = endpoint
				require.Error(t, ValidateProductBinding(profile, candidate), endpoint)
			}
			binding.BaseURL = "https://api.anthropic.com"
			binding.ProtocolProfileVersion = "9.9.9"
			require.Error(t, ValidateProductBinding(profile, binding))
		})
	}
}

func TestClaudeOpus5StreamingCandidateRequiresExactMessagesV2OfficialOrigin(t *testing.T) {
	profile, ok := Lookup("anthropic.claude-opus-5.api", "1.2.0")
	require.True(t, ok)
	binding := ProductBinding{
		Provider: "anthropic", BaseURL: "https://api.anthropic.com",
		AdapterType: "anthropic_native", OperationID: OperationAnthropicMessage,
		EndpointPathMode:       EndpointPathModeCanonicalV1,
		ProtocolProfileID:      endpointprotocol.ProfileAnthropicMessagesTextV2,
		ProtocolProfileVersion: "1.0.0", UpstreamModel: "claude-opus-5",
	}
	require.NoError(t, ValidateProductBinding(profile, binding))

	legacyProtocol := binding
	legacyProtocol.ProtocolProfileID = endpointprotocol.ProfileAnthropicMessagesText
	require.Error(t, ValidateProductBinding(profile, legacyProtocol))

	forgedOrigin := binding
	forgedOrigin.BaseURL = "https://provider.example"
	require.Error(t, ValidateProductBinding(profile, forgedOrigin))
}

func TestClaudeHaiku45StreamingCandidateRequiresExactMessagesV2OfficialOrigin(t *testing.T) {
	profile, ok := Lookup("anthropic.claude-haiku-4.5.api", "1.2.0")
	require.True(t, ok)
	binding := ProductBinding{
		Provider: "anthropic", BaseURL: "https://api.anthropic.com",
		AdapterType: "anthropic_native", OperationID: OperationAnthropicMessage,
		EndpointPathMode:       EndpointPathModeCanonicalV1,
		ProtocolProfileID:      endpointprotocol.ProfileAnthropicMessagesTextV2,
		ProtocolProfileVersion: "1.0.0", UpstreamModel: "claude-haiku-4-5-20251001",
	}
	require.NoError(t, ValidateProductBinding(profile, binding))

	legacyProtocol := binding
	legacyProtocol.ProtocolProfileID = endpointprotocol.ProfileAnthropicMessagesText
	require.Error(t, ValidateProductBinding(profile, legacyProtocol))

	forgedOrigin := binding
	forgedOrigin.BaseURL = "https://provider.example"
	require.Error(t, ValidateProductBinding(profile, forgedOrigin))
}

func TestOpenAIResponsesBindingsUseOnlyOfficialOriginAndExactEndpointContract(t *testing.T) {
	for _, model := range []string{"sol", "terra", "luna"} {
		t.Run(model, func(t *testing.T) {
			version := "1.1.0"
			if model == "sol" {
				version = "1.2.0"
			}
			profile, ok := Lookup("openai.gpt-5.6-"+model+".responses", version)
			require.True(t, ok)
			binding := ProductBinding{
				Provider: "openai", BaseURL: "https://api.openai.com",
				AdapterType: "openai_compatible", OperationID: OperationOpenAIResponses,
				EndpointPathMode:  EndpointPathModeCanonicalV1,
				ProtocolProfileID: "unirouter.openai.responses.text.v1", ProtocolProfileVersion: "1.0.0",
				UpstreamModel: "gpt-5.6-" + model,
			}
			require.NoError(t, ValidateProductBinding(profile, binding))
			binding.BaseURL = "https://api.openai.com/"
			require.NoError(t, ValidateProductBinding(profile, binding))
			binding.BaseURL = "https://api.openai.com/v1"
			require.Error(t, ValidateProductBinding(profile, binding))
			binding.BaseURL = "https://api.openai.com"
			binding.ProtocolProfileVersion = "9.9.9"
			require.Error(t, ValidateProductBinding(profile, binding))
		})
	}
}

func TestGeminiProductBindingAcceptsOnlyOfficialOpenAICompatibilityEndpoint(t *testing.T) {
	profile, ok := Lookup("gemini.3.7-flash.api", "1.6.0")
	require.True(t, ok)
	binding := ProductBinding{
		Provider: "gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		AdapterType: "openai_compatible", OperationID: OperationOpenAIChat,
		EndpointPathMode: EndpointPathModeBaseRelative, UpstreamModel: "gemini-3.7-flash",
	}
	require.NoError(t, ValidateProductBinding(profile, binding))
	binding.BaseURL += "/"
	require.NoError(t, ValidateProductBinding(profile, binding))
	for _, endpoint := range []string{
		"https://arbitrary.example/v1beta/openai",
		"https://generativelanguage.googleapis.com/v1beta",
		"https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
		"https://generativelanguage.googleapis.com:443/v1beta/openai",
	} {
		candidate := binding
		candidate.BaseURL = endpoint
		require.Error(t, ValidateProductBinding(profile, candidate), endpoint)
	}
	wrongPathMode := binding
	wrongPathMode.EndpointPathMode = EndpointPathModeCanonicalV1
	require.Error(t, ValidateProductBinding(profile, wrongPathMode))
}

func TestDeepSeekProductBindingAcceptsOnlyOfficialOpenAIEndpoint(t *testing.T) {
	for _, test := range []struct {
		profileID, upstreamModel string
	}{
		{profileID: "deepseek.v4-flash.api", upstreamModel: "deepseek-v4-flash"},
		{profileID: "deepseek.v4-pro.api", upstreamModel: "deepseek-v4-pro"},
	} {
		t.Run(test.profileID, func(t *testing.T) {
			profile, ok := Lookup(test.profileID, "1.2.0")
			require.True(t, ok)
			binding := ProductBinding{
				Provider: "deepseek", BaseURL: "https://api.deepseek.com",
				AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeBaseRelative,
				OperationID: OperationOpenAIChat, UpstreamModel: test.upstreamModel,
			}
			require.NoError(t, ValidateProductBinding(profile, binding))
			binding.BaseURL += "/"
			require.NoError(t, ValidateProductBinding(profile, binding))
			for _, endpoint := range []string{
				"https://arbitrary.example",
				"https://api.deepseek.com/v1",
				"https://api.deepseek.com/beta",
				"https://api.deepseek.com/chat/completions",
				"https://api.deepseek.com:443",
				"https://api.deepseek.com?",
				"https://api.deepseek.com#",
			} {
				candidate := binding
				candidate.BaseURL = endpoint
				require.Error(t, ValidateProductBinding(profile, candidate), endpoint)
			}
			wrongPathMode := binding
			wrongPathMode.EndpointPathMode = EndpointPathModeCanonicalV1
			require.Error(t, ValidateProductBinding(profile, wrongPathMode))
			wrongUpstream := binding
			if test.upstreamModel == "deepseek-v4-flash" {
				wrongUpstream.UpstreamModel = "deepseek-v4-pro"
			} else {
				wrongUpstream.UpstreamModel = "deepseek-v4-flash"
			}
			require.Error(t, ValidateProductBinding(profile, wrongUpstream))
		})
	}
}

func TestDoubaoSeedEvolvingFloatingAliasBindsRequestedAliasWithoutClaimingObservedVersion(t *testing.T) {
	profile, ok := Lookup("doubao.seed-evolving.api", "1.2.0")
	require.True(t, ok)
	binding := ProductBinding{
		Provider: "doubao", BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeBaseRelative,
		OperationID: OperationOpenAIChat, UpstreamModel: "doubao-seed-evolving",
	}
	require.NoError(t, ValidateProductBinding(profile, binding))

	binding.UpstreamModel = "doubao-seed-evolving-unknown-version"
	require.Error(t, ValidateProductBinding(profile, binding))
	binding.UpstreamModel = "doubao-seed-evolving"
	binding.BaseURL = "https://ark.cn-beijing.volces.com/api/v3/chat/completions"
	require.Error(t, ValidateProductBinding(profile, binding))
}

func TestKimiPlatformProductBindingRejectsCodingSubscriptionIdentity(t *testing.T) {
	for _, test := range []struct {
		profileID, version, upstreamModel string
	}{
		{profileID: "kimi.k2.6.api", version: "1.6.0", upstreamModel: "kimi-k2.6"},
		{profileID: "kimi.k2.7-code.api", version: "1.2.0", upstreamModel: "kimi-k2.7-code"},
		{profileID: "kimi.k3.api", version: "1.6.0", upstreamModel: "kimi-k3"},
	} {
		t.Run(test.profileID, func(t *testing.T) {
			profile, ok := Lookup(test.profileID, test.version)
			require.True(t, ok)
			binding := ProductBinding{
				Provider: "kimi", BaseURL: "https://api.moonshot.cn/v1",
				AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeCanonicalV1,
				OperationID: OperationOpenAIChat, UpstreamModel: test.upstreamModel,
			}
			require.NoError(t, ValidateProductBinding(profile, binding))
			binding.BaseURL += "/"
			require.NoError(t, ValidateProductBinding(profile, binding))
			for _, endpoint := range []string{
				"https://arbitrary.example/v1",
				"https://api.moonshot.cn",
				"https://api.moonshot.cn/v1/chat/completions",
				"https://api.moonshot.cn:443/v1",
				"https://api.kimi.com/coding/v1",
				"https://api.moonshot.cn/v1?",
			} {
				candidate := binding
				candidate.BaseURL = endpoint
				require.Error(t, ValidateProductBinding(profile, candidate), endpoint)
			}
			wrongPathMode := binding
			wrongPathMode.EndpointPathMode = EndpointPathModeBaseRelative
			require.Error(t, ValidateProductBinding(profile, wrongPathMode))
			for _, codingModelID := range []string{"k3", "k3-256k", "kimi-for-coding", "kimi-for-coding-highspeed"} {
				candidate := binding
				candidate.UpstreamModel = codingModelID
				require.Error(t, ValidateProductBinding(profile, candidate), codingModelID)
			}
		})
	}
}

func TestMiniMaxProductBindingAcceptsOnlyOfficialChinaOpenAIEndpoint(t *testing.T) {
	for _, test := range []struct {
		profileID, upstreamModel string
	}{
		{profileID: "minimax.m2.7.token-plan", upstreamModel: "MiniMax-M2.7"},
		{profileID: "minimax.m2.5-highspeed.token-plan", upstreamModel: "MiniMax-M2.5-highspeed"},
		{profileID: "minimax.m3.token-plan", upstreamModel: "MiniMax-M3"},
	} {
		t.Run(test.profileID, func(t *testing.T) {
			profile, ok := Lookup(test.profileID, "1.2.0")
			require.True(t, ok)
			binding := ProductBinding{
				Provider: "minimax", BaseURL: "https://api.minimaxi.com/v1",
				AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeCanonicalV1,
				OperationID: OperationOpenAIChat, UpstreamModel: test.upstreamModel,
			}
			require.NoError(t, ValidateProductBinding(profile, binding))
			binding.BaseURL += "/"
			require.NoError(t, ValidateProductBinding(profile, binding))
			for _, endpoint := range []string{
				"https://arbitrary.example/v1",
				"https://api.minimaxi.com",
				"https://api.minimaxi.com/v1/chat/completions",
				"https://api.minimaxi.com:443/v1",
				"https://api.minimaxi.com/v1?",
			} {
				candidate := binding
				candidate.BaseURL = endpoint
				require.Error(t, ValidateProductBinding(profile, candidate), endpoint)
			}
			wrongPathMode := binding
			wrongPathMode.EndpointPathMode = EndpointPathModeBaseRelative
			require.Error(t, ValidateProductBinding(profile, wrongPathMode))
		})
	}

	m27, ok := Lookup("minimax.m2.7.token-plan", "1.2.0")
	require.True(t, ok)
	require.Equal(t, []string{"MiniMax-M2.7"}, m27.UpstreamModelIDs)
	wrongVariant := ProductBinding{
		Provider: "minimax", BaseURL: "https://api.minimaxi.com/v1",
		AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeCanonicalV1,
		OperationID: OperationOpenAIChat, UpstreamModel: "MiniMax-M2.7-highspeed",
	}
	require.Error(t, ValidateProductBinding(m27, wrongVariant))
}

func TestGrok46ProductBindingAcceptsOnlyExactOfficialOpenAIV1Endpoint(t *testing.T) {
	profile, ok := Lookup("xai.grok-4.6.api", "1.1.0")
	require.True(t, ok)
	binding := ProductBinding{
		Provider: "xai", BaseURL: "https://api.x.ai/v1",
		AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeCanonicalV1,
		OperationID: OperationOpenAIChat, UpstreamModel: "grok-4.6",
	}
	require.NoError(t, ValidateProductBinding(profile, binding))
	binding.BaseURL += "/"
	require.NoError(t, ValidateProductBinding(profile, binding))
	for _, endpoint := range []string{
		"https://api.x.ai",
		"https://api.x.ai/v1/chat/completions",
		"https://api.x.ai:443/v1",
		"https://arbitrary.example/v1",
		"http://api.x.ai/v1",
		"https://api.x.ai/v1?",
	} {
		candidate := binding
		candidate.BaseURL = endpoint
		require.Error(t, ValidateProductBinding(profile, candidate), endpoint)
	}
	wrongMode := binding
	wrongMode.BaseURL = "https://api.x.ai/v1"
	wrongMode.EndpointPathMode = EndpointPathModeBaseRelative
	require.Error(t, ValidateProductBinding(profile, wrongMode))
}

func TestReviewedEndpointClassifierRejectsEmptyQueryAndFragmentDelimiters(t *testing.T) {
	for _, test := range []struct {
		provider, baseURL string
	}{
		{provider: "openai", baseURL: "https://api.openai.com/v1?"},
		{provider: "gemini", baseURL: "https://generativelanguage.googleapis.com/v1beta/openai#"},
		{provider: "deepseek", baseURL: "https://api.deepseek.com?"},
		{provider: "xai", baseURL: "https://api.x.ai/v1#"},
	} {
		_, err := ClassifyEndpoint(test.provider, test.baseURL)
		require.Error(t, err, test.baseURL)
	}
}

func TestDoubaoSeed21ProProductBindingAcceptsOnlyExactArkBeijingV3Root(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-pro.api", "1.1.0")
	require.True(t, ok)
	binding := ProductBinding{
		Provider: "doubao", BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeBaseRelative,
		OperationID: OperationOpenAIChat, UpstreamModel: "doubao-seed-2-1-pro-260628",
	}
	require.NoError(t, ValidateProductBinding(profile, binding))
	binding.BaseURL += "/"
	require.NoError(t, ValidateProductBinding(profile, binding))

	for _, endpoint := range []string{
		"https://ark.cn-beijing.volces.com",
		"https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		"https://ark.cn-beijing.volces.com:443/api/v3",
		"https://ark.ap-southeast-1.volces.com/api/v3",
		"https://example.test/api/v3",
		"http://ark.cn-beijing.volces.com/api/v3",
		"https://ark.cn-beijing.volces.com/api/v3?",
	} {
		candidate := binding
		candidate.BaseURL = endpoint
		require.Error(t, ValidateProductBinding(profile, candidate), endpoint)
	}

	wrongMode := binding
	wrongMode.BaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	wrongMode.EndpointPathMode = EndpointPathModeCanonicalV1
	require.Error(t, ValidateProductBinding(profile, wrongMode))
}

func TestDoubaoSeed21TurboProductBindingAcceptsExactArkBeijingV3Root(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-turbo.api", "1.1.0")
	require.True(t, ok)
	binding := ProductBinding{
		Provider: "doubao", BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		AdapterType: "openai_compatible", EndpointPathMode: EndpointPathModeBaseRelative,
		OperationID: OperationOpenAIChat, UpstreamModel: "doubao-seed-2-1-turbo-260628",
	}
	require.NoError(t, ValidateProductBinding(profile, binding))

	wrongModel := binding
	wrongModel.UpstreamModel = "doubao-seed-2-1-pro-260628"
	require.Error(t, ValidateProductBinding(profile, wrongModel))
	wrongRegion := binding
	wrongRegion.BaseURL = "https://ark.ap-southeast-1.volces.com/api/v3"
	require.Error(t, ValidateProductBinding(profile, wrongRegion))
}
