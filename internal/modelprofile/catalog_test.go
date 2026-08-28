package modelprofile

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

func TestCurrentProfileStatusInventoryMatchesExactCatalog(t *testing.T) {
	// This is a documentation drift check, never a runtime or scope authority.
	data, err := os.ReadFile("../../docs/tech-ref/42-model-execution-profile-capability-status.md")
	require.NoError(t, err)
	_, inventory, ok := strings.Cut(string(data), "<!-- current-profile-inventory:start -->")
	require.True(t, ok)
	inventory, _, ok = strings.Cut(inventory, "<!-- current-profile-inventory:end -->")
	require.True(t, ok)
	var documented []string
	for _, line := range strings.Split(inventory, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		identity, _, found := strings.Cut(strings.TrimPrefix(line, "| `"), "` |")
		require.True(t, found, line)
		documented = append(documented, identity)
	}
	var current []string
	for _, profile := range All() {
		if profile.Current && profile.ID != UnverifiedProfileID {
			current = append(current, profile.ID+"@"+profile.Version)
		}
	}
	require.ElementsMatch(t, current, documented,
		"current-state documentation must contain every exact attachable identity once")
}

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

func TestGPT56SurfaceSpecificChatProfilesUseTheLocallyConformantCoreContract(t *testing.T) {
	models := []struct {
		id string
	}{
		{"openai.gpt-5.6-sol.chat"},
		{"openai.gpt-5.6-terra.chat"},
		{"openai.gpt-5.6-luna.chat"},
	}
	for _, model := range models {
		t.Run(model.id, func(t *testing.T) {
			current, ok := Lookup(model.id, "1.0.0")
			require.True(t, ok)
			require.True(t, current.Current)
			require.Equal(t, "protocol.openai-chat.v5", current.ProtocolProfile)
			require.Equal(t, 1_050_000, current.ProviderContextWindow)
			require.Equal(t, 272_000, current.ExecutableContextWindow)
			require.Equal(t, 272_000, current.DefaultContextWindow)
			require.Equal(t, "reasoning.openai-gpt-5.6-chat.v1", current.ReasoningProfile)
			require.Equal(t, "cache-usage.openai-gpt-5.6.v1", current.CacheUsageProfile)
			for _, capability := range []string{
				CapabilityStreaming, "reasoning", "reasoning_effort", CapabilityFunctionTools,
				CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
				CapabilityJSONObject, CapabilityStrictJSONSchema, CapabilityVision,
				CapabilityExplicitCache, CapabilityCacheReadUsage, CapabilityCacheCreateUsage,
			} {
				require.Contains(t, current.Capabilities, capability)
			}
			require.NotContains(t, current.Capabilities, CapabilityBuiltinWebSearch)
			require.NotContains(t, current.Capabilities, CapabilityJSONSchema)
			require.NotContains(t, current.Capabilities, "service_tier")

			contract, ok := ResolveChatProtocolContract(current.ProtocolProfile)
			require.True(t, ok)
			require.Equal(t, []string{"max_completion_tokens"}, contract.OutputLimitAcceptedFields)
			require.EqualValues(t, 128_000, contract.OutputLimitMax)
			require.Equal(t, []string{"implicit", "explicit"}, contract.PromptCacheModes)
			require.Equal(t, []string{"30m"}, contract.PromptCacheTTLs)
			require.Empty(t, contract.ProcessingTierPolicyID)
		})
	}

	reasoning, ok := ResolveReasoningContract("reasoning.openai-gpt-5.6-chat.v1")
	require.True(t, ok)
	require.Equal(t, "medium", reasoning.EffortControl.ProviderDefault)
	require.Equal(t, []string{"none", "low", "medium", "high", "xhigh", "max"}, reasoning.EffortControl.AllowedValues)

	cache, ok := ResolveCacheUsageContract("cache-usage.openai-gpt-5.6.v1")
	require.True(t, ok)
	require.Equal(t, CacheUsageWireOpenAIReadWriteV1, cache.WireSchema)
	require.True(t, cache.ReportsRead)
	require.True(t, cache.ReportsCreation)
}

func TestGPT56SurfaceIdentitiesRetireTheMixedIDWithoutRewritingHistory(t *testing.T) {
	models := []struct {
		name                    string
		legacyID                string
		chatID                  string
		responsesID             string
		currentResponsesVersion string
		responsesCoreConformant bool
		responsesReasoning      string
	}{
		{"sol", "openai.gpt-5.6-sol.api", "openai.gpt-5.6-sol.chat", "openai.gpt-5.6-sol.responses", "1.3.0", true, "reasoning.openai-gpt-5.6-sol-responses.v1"},
		{"terra", "openai.gpt-5.6-terra.api", "openai.gpt-5.6-terra.chat", "openai.gpt-5.6-terra.responses", "1.3.0", true, "reasoning.openai-gpt-5.6-terra-responses.v1"},
		{"luna", "openai.gpt-5.6-luna.api", "openai.gpt-5.6-luna.chat", "openai.gpt-5.6-luna.responses", "1.3.0", true, "reasoning.openai-gpt-5.6-luna-responses.v1"},
	}
	for _, model := range models {
		t.Run(model.name, func(t *testing.T) {
			assertGPT56SurfaceIdentities(
				t,
				model.legacyID,
				model.chatID,
				model.responsesID,
				"gpt-5.6-"+model.name,
				model.currentResponsesVersion,
				model.responsesCoreConformant,
				model.responsesReasoning,
			)
		})
	}
}

func TestGPT56LunaResponsesReasoningHistoricalContractRemainsExact(t *testing.T) {
	profile, ok := Lookup("openai.gpt-5.6-luna.responses", "1.2.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "unirouter.openai.responses.text.v1", profile.ProtocolProfile)
	require.Equal(t, "1.0.0", profile.ProtocolProfileVersion)
	require.Equal(t, "reasoning.openai-gpt-5.6-luna-responses.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoningEffort}, profile.Capabilities)
	require.Equal(t, []string{EndpointClassOpenAIOfficialOrigin}, profile.EndpointClasses)
	require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))
}

func TestGPT56SolResponsesStreamingProfileIsCurrentAndAttachableAfterClosure(t *testing.T) {
	profile, ok := Lookup("openai.gpt-5.6-sol.responses", "1.3.0")
	require.True(t, ok)
	require.True(t, profile.Current)
	require.Equal(t, APISurfaceOpenAIResponses, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIResponses}, profile.Operations)
	require.Equal(t, endpointprotocol.ProfileOpenAIResponsesTextV2, profile.ProtocolProfile)
	require.Equal(t, "1.0.0", profile.ProtocolProfileVersion)
	require.Equal(t, []string{CapabilityReasoningEffort, CapabilityStreaming}, profile.Capabilities)
	require.Equal(t, []string{EndpointClassOpenAIOfficialOrigin}, profile.EndpointClasses)
	require.NoError(t, ValidateExecutableComponents(profile))
	require.NoError(t, ValidateNewAttachment(profile.ID, profile.Version))

	current, ok := MatchForSurface("gpt-5.6-sol", APISurfaceOpenAIResponses)
	require.True(t, ok)
	require.Equal(t, "1.3.0", current.Version)

	historical, ok := Lookup("openai.gpt-5.6-sol.responses", "1.2.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
}

func TestGPT56TerraResponsesStreamingProfileIsCurrentAndAttachableAfterClosure(t *testing.T) {
	profile, ok := Lookup("openai.gpt-5.6-terra.responses", "1.3.0")
	require.True(t, ok)
	require.True(t, profile.Current)
	require.Equal(t, APISurfaceOpenAIResponses, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIResponses}, profile.Operations)
	require.Equal(t, endpointprotocol.ProfileOpenAIResponsesTextV2, profile.ProtocolProfile)
	require.Equal(t, "1.0.0", profile.ProtocolProfileVersion)
	require.Equal(t, "reasoning.openai-gpt-5.6-terra-responses.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoningEffort, CapabilityStreaming}, profile.Capabilities)
	require.Equal(t, []string{EndpointClassOpenAIOfficialOrigin}, profile.EndpointClasses)
	require.Contains(t, profile.EvidenceRefs, "https://developers.openai.com/api/docs/models/gpt-5.6-terra")
	require.NoError(t, ValidateExecutableComponents(profile))
	require.NoError(t, ValidateNewAttachment(profile.ID, profile.Version))

	current, ok := MatchForSurface("gpt-5.6-terra", APISurfaceOpenAIResponses)
	require.True(t, ok)
	require.Equal(t, "1.3.0", current.Version)

	historical, ok := Lookup("openai.gpt-5.6-terra.responses", "1.2.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
}

func TestGPT56LunaResponsesStreamingProfileIsCurrentAndAttachableAfterClosure(t *testing.T) {
	profile, ok := Lookup("openai.gpt-5.6-luna.responses", "1.3.0")
	require.True(t, ok)
	require.True(t, profile.Current)
	require.Equal(t, APISurfaceOpenAIResponses, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIResponses}, profile.Operations)
	require.Equal(t, endpointprotocol.ProfileOpenAIResponsesTextV2, profile.ProtocolProfile)
	require.Equal(t, "1.0.0", profile.ProtocolProfileVersion)
	require.Equal(t, "reasoning.openai-gpt-5.6-luna-responses.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoningEffort, CapabilityStreaming}, profile.Capabilities)
	require.Equal(t, []string{EndpointClassOpenAIOfficialOrigin}, profile.EndpointClasses)
	require.Contains(t, profile.EvidenceRefs, "https://developers.openai.com/api/docs/models/gpt-5.6-luna")
	require.NoError(t, ValidateExecutableComponents(profile))
	require.NoError(t, ValidateNewAttachment(profile.ID, profile.Version))

	current, ok := MatchForSurface("gpt-5.6-luna", APISurfaceOpenAIResponses)
	require.True(t, ok)
	require.Equal(t, "1.3.0", current.Version)

	historical, ok := Lookup("openai.gpt-5.6-luna.responses", "1.2.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Error(t, ValidateNewAttachment(historical.ID, historical.Version))
}

func assertGPT56SurfaceIdentities(
	t *testing.T,
	legacyID,
	chatID,
	responsesID,
	modelName,
	currentResponsesVersion string,
	responsesCoreConformant bool,
	responsesReasoning string,
) {
	t.Helper()
	historicalResponses, ok := Lookup(legacyID, "1.0.0")
	require.True(t, ok)
	require.False(t, historicalResponses.Current)
	require.Equal(t, APISurfaceOpenAIResponses, historicalResponses.APISurface)
	require.Equal(t, []string{OperationOpenAIResponses}, historicalResponses.Operations)
	require.Equal(t, []string{EndpointClassOpenAIOfficialOrigin}, historicalResponses.EndpointClasses)

	historicalChat, ok := Lookup(legacyID, "1.6.0")
	require.True(t, ok)
	require.False(t, historicalChat.Current)
	require.Equal(t, APISurfaceOpenAIChat, historicalChat.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, historicalChat.Operations)
	require.Equal(t, []string{EndpointClassOpenAIOfficialV1}, historicalChat.EndpointClasses)
	require.Error(t, ValidateNewAttachment(historicalResponses.ID, historicalResponses.Version))
	require.Error(t, ValidateNewAttachment(historicalChat.ID, historicalChat.Version))

	documentedResponses, ok := Lookup(responsesID, "1.0.0")
	require.True(t, ok)
	require.Equal(t, currentResponsesVersion == "1.0.0", documentedResponses.Current)
	require.Equal(t, "protocol.openai-responses.documented.v1", documentedResponses.ProtocolProfile)
	require.Empty(t, documentedResponses.ProtocolProfileVersion)
	require.Empty(t, documentedResponses.Capabilities)
	require.Equal(t, "documented", documentedResponses.CatalogEvidenceState)

	responses, ok := Lookup(responsesID, currentResponsesVersion)
	require.True(t, ok)
	require.True(t, responses.Current)
	require.Equal(t, APISurfaceOpenAIResponses, responses.APISurface)
	require.Equal(t, []string{OperationOpenAIResponses}, responses.Operations)
	require.Equal(t, 1_050_000, responses.ProviderContextWindow)
	require.Equal(t, 272_000, responses.ExecutableContextWindow)
	require.Equal(t, 272_000, responses.DefaultContextWindow)
	if responsesReasoning != "" {
		expectedCapabilities := []string{CapabilityReasoningEffort}
		if currentResponsesVersion == "1.3.0" {
			expectedCapabilities = append(expectedCapabilities, CapabilityStreaming)
		}
		require.Equal(t, expectedCapabilities, responses.Capabilities)
		require.Equal(t, responsesReasoning, responses.ReasoningProfile)
		reasoning, modeled := ResolveReasoningContract(responses.ReasoningProfile)
		require.True(t, modeled)
		require.Equal(t, "reasoning", reasoning.RequestObjectField)
		require.Equal(t, "effort", reasoning.EffortControl.Field)
		require.Equal(t, []string{"none", "low", "medium", "high", "xhigh", "max"}, reasoning.EffortControl.AllowedValues)
		require.Equal(t, "medium", reasoning.EffortControl.ProviderDefault)
		core, exists := Lookup(responsesID, "1.1.0")
		require.True(t, exists)
		require.False(t, core.Current)
		require.Empty(t, core.Capabilities)
		require.Error(t, ValidateNewAttachment(core.ID, core.Version))
	} else {
		require.Empty(t, responses.Capabilities)
	}
	require.Equal(t, []string{EndpointClassOpenAIOfficialOrigin}, responses.EndpointClasses)
	require.NoError(t, ValidateNewAttachment(responses.ID, responses.Version))
	if responsesCoreConformant {
		require.False(t, documentedResponses.Current)
		require.Error(t, ValidateNewAttachment(documentedResponses.ID, documentedResponses.Version))
		expectedProtocol := "unirouter.openai.responses.text.v1"
		if currentResponsesVersion == "1.3.0" {
			expectedProtocol = endpointprotocol.ProfileOpenAIResponsesTextV2
		}
		require.Equal(t, expectedProtocol, responses.ProtocolProfile)
		require.Equal(t, "1.0.0", responses.ProtocolProfileVersion)
		require.Equal(t, "locally_conformant", responses.CatalogEvidenceState)
		driftedProtocol := responses
		driftedProtocol.ProtocolProfileVersion = "9.9.9"
		require.ErrorContains(t, ValidateExecutableComponents(driftedProtocol), "endpoint protocol identity is not immutable")
	} else {
		require.Equal(t, documentedResponses, responses)
	}

	chat, ok := Lookup(chatID, "1.0.0")
	require.True(t, ok)
	require.True(t, chat.Current)
	require.Equal(t, APISurfaceOpenAIChat, chat.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, chat.Operations)
	require.Equal(t, "protocol.openai-chat.v5", chat.ProtocolProfile)
	require.Equal(t, []string{EndpointClassOpenAIOfficialV1}, chat.EndpointClasses)
	require.Contains(t, chat.Capabilities, CapabilityStreaming)
	require.NoError(t, ValidateNewAttachment(chat.ID, chat.Version))
	driftedCurrent := chat
	driftedCurrent.APISurface = APISurfaceOpenAIResponses
	driftedCurrent.Operations = []string{OperationOpenAIResponses}
	require.ErrorContains(t, ValidateExecutableComponents(driftedCurrent), "execution identity is not immutable")

	_, ok = Match(modelName)
	require.False(t, ok, "surface-agnostic alias resolution must fail closed")
	resolvedChat, ok := MatchForSurface(modelName, APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, chat.ID, resolvedChat.ID)
	resolvedResponses, ok := MatchForSurface(modelName, APISurfaceOpenAIResponses)
	require.True(t, ok)
	require.Equal(t, responses.ID, resolvedResponses.ID)
}

func TestCurrentPointerCannotRewriteHistoricalExecutionIdentity(t *testing.T) {
	const profileID = "openai.gpt-5.6-terra.api"
	original, existed := currentProfileManifests[profileID]
	t.Cleanup(func() {
		if existed {
			currentProfileManifests[profileID] = original
			return
		}
		delete(currentProfileManifests, profileID)
	})

	currentProfileManifests[profileID] = currentProfile("1.0.0")
	responses, ok := Lookup(profileID, "1.0.0")
	require.True(t, ok)
	require.True(t, responses.Current)
	require.Equal(t, APISurfaceOpenAIResponses, responses.APISurface)
	require.Equal(t, []string{OperationOpenAIResponses}, responses.Operations)
	require.Equal(t, DefaultOutputPolicy, responses.DefaultOutputAuthority)

	chat, ok := Lookup(profileID, "1.6.0")
	require.True(t, ok)
	require.False(t, chat.Current)
	require.Equal(t, APISurfaceOpenAIChat, chat.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, chat.Operations)
	require.Equal(t, DefaultOutputPolicy, chat.DefaultOutputAuthority)
}

func TestGrok46PromotesOnlyBoundedCoreChatWithoutInventingAProviderOutputMaximum(t *testing.T) {
	documented, ok := Lookup("xai.grok-4.6.api", "1.0.0")
	require.True(t, ok)
	require.False(t, documented.Current)
	require.Equal(t, "protocol.xai-openai-chat.documented.v1", documented.ProtocolProfile)
	require.Zero(t, documented.ProviderMaxOutput)
	require.Zero(t, documented.ExecutableMaxOutput)
	require.Empty(t, documented.Capabilities)

	current, ok := Match("grok-4.6")
	require.True(t, ok)
	require.Equal(t, "1.1.0", current.Version)
	require.Equal(t, "Grok 4.6 OpenAI-compatible Core Chat 子集", current.Label)
	require.Equal(t, "protocol.xai-openai-chat.v1", current.ProtocolProfile)
	require.Equal(t, 500_000, current.ProviderContextWindow)
	require.Zero(t, current.ProviderMaxOutput, "xAI documents no fixed text-output limit")
	require.Equal(t, 131_072, current.ExecutableMaxOutput)
	require.Equal(t, 32_768, current.DefaultOutputLimit)
	require.Equal(t, DefaultOutputPolicy, current.DefaultOutputAuthority)
	require.Equal(t, []string{CapabilityStreaming}, current.Capabilities)
	require.Equal(t, []string{EndpointClassXAIOfficialOpenAIV1}, current.EndpointClasses)
	require.Equal(t, []string{"grok-4.6"}, current.UpstreamModelIDs)

	contract, ok := ResolveChatProtocolContract(current.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.Streaming)
	require.Equal(t, "max_tokens", contract.OutputLimitField)
	require.EqualValues(t, 131_072, contract.OutputLimitMax)
	require.Empty(t, contract.OutputLimitAcceptedFields)
	require.True(t, contract.FunctionToolsModeled)
	require.False(t, contract.FunctionTools)
	require.True(t, contract.ResponseFormatModeled)
	require.Equal(t, []string{"text"}, contract.ResponseFormatTypes)
	require.Empty(t, contract.ProcessingTierPolicyID)

	require.NoError(t, ValidateEffectiveLimits(current, 500_000, 131_072))
	require.ErrorContains(t, ValidateEffectiveLimits(current, 500_000, 131_073), "executable profile maximum 131072")
}

func TestCatalogHasExactlyOneExplicitCurrentVersionPerProfileID(t *testing.T) {
	counts := map[string]int{}
	for _, profile := range All() {
		if profile.Current {
			counts[profile.ID]++
			manifest, declared := currentProfileManifests[profile.ID]
			require.True(t, declared, profile.ID)
			require.Equal(t, manifest.Version, profile.Version, profile.ID)
		}
	}
	require.Len(t, counts, len(currentProfileManifests))
	for profileID := range currentProfileManifests {
		require.Equal(t, 1, counts[profileID], profileID)
	}
}

func TestNewAttachmentEligibilityUsesTheCurrentManifestWithoutInvalidatingHistory(t *testing.T) {
	require.NoError(t, ValidateNewAttachment("kimi.k2.6.api", "1.7.0"))
	require.NoError(t, ValidateNewAttachment("kimi.k2.7-code.api", "1.3.0"))
	require.NoError(t, ValidateNewAttachment("kimi.k3.api", "1.8.0"))
	require.NoError(t, ValidateNewAttachment(UnverifiedProfileID, UnverifiedProfileVersion))

	for _, historical := range []struct{ id, version string }{
		{id: "kimi.k2.6.api", version: "1.6.0"},
		{id: "kimi.k2.6.api", version: "1.5.0"},
		{id: "kimi.k2.7-code.api", version: "1.2.0"},
		{id: "kimi.k2.7-code.api", version: "1.1.0"},
		{id: "kimi.k3.api", version: "1.7.0"},
		{id: "kimi.k3.api", version: "1.6.0"},
		{id: "kimi.k3.api", version: "1.5.0"},
	} {
		profile, ok := Lookup(historical.id, historical.version)
		require.True(t, ok, historical)
		require.False(t, profile.Current, historical)
		require.Error(t, ValidateNewAttachment(historical.id, historical.version), historical)
	}
	require.Error(t, ValidateNewAttachment("kimi.k3.api", "9.9.9"))
}

func TestCurrentProfilesExposeTypedProductIdentityWithoutDeploymentClaims(t *testing.T) {
	for _, profile := range All() {
		if !profile.Current {
			continue
		}
		require.NotEmpty(t, profile.Provider, profile.ID)
		require.NotEmpty(t, profile.Region, profile.ID)
		require.NotEmpty(t, profile.APISurface, profile.ID)
		require.Len(t, profile.Operations, 1, profile.ID)
		require.NotEmpty(t, profile.EndpointClasses, profile.ID)
		if profile.ID != UnverifiedProfileID {
			require.NotEmpty(t, profile.UpstreamModelIDs, profile.ID)
		}
		require.Contains(t, []string{DefaultOutputProvider, DefaultOutputPolicy, DefaultOutputCustomer}, profile.DefaultOutputAuthority, profile.ID)
		require.Contains(t, []string{"unverified", "documented", "locally_conformant"}, profile.CatalogEvidenceState, profile.ID)
		require.NotContains(t, []string{"canary_live_verified", "released"}, profile.CatalogEvidenceState, profile.ID)
	}

	qwen, ok := Lookup("qwen.3.8-max.api", "1.8.0")
	require.True(t, ok)
	require.Equal(t, "dashscope", qwen.Provider)
	require.Equal(t, "cn-beijing", qwen.Region)
	require.Equal(t, APISurfaceOpenAIChat, qwen.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, qwen.Operations)
	require.Equal(t, []string{EndpointClassDashScopeBeijingWorkspacePayGo, EndpointClassDashScopeBeijingSharedPayGo}, qwen.EndpointClasses)
	require.Equal(t, []string{"qwen3.8-max"}, qwen.UpstreamModelIDs)
	require.Equal(t, DefaultOutputProvider, qwen.DefaultOutputAuthority)
	require.Equal(t, "locally_conformant", qwen.CatalogEvidenceState)

	gpt, ok := Lookup("openai.gpt-5.6-sol.chat", "1.0.0")
	require.True(t, ok)
	require.Equal(t, DefaultOutputPolicy, gpt.DefaultOutputAuthority)
	require.Equal(t, APISurfaceOpenAIChat, gpt.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, gpt.Operations)
	require.Equal(t, []string{EndpointClassOpenAIOfficialV1}, gpt.EndpointClasses)

	documentedResponses, ok := Lookup("openai.gpt-5.6-sol.responses", "1.0.0")
	require.True(t, ok)
	require.False(t, documentedResponses.Current)
	require.Equal(t, APISurfaceOpenAIResponses, documentedResponses.APISurface)
	require.Equal(t, []string{OperationOpenAIResponses}, documentedResponses.Operations)
	require.Equal(t, []string{EndpointClassOpenAIOfficialOrigin}, documentedResponses.EndpointClasses)

	conformantResponses, ok := Lookup("openai.gpt-5.6-sol.responses", "1.3.0")
	require.True(t, ok)
	require.True(t, conformantResponses.Current)
	require.Equal(t, endpointprotocol.ProfileOpenAIResponsesTextV2, conformantResponses.ProtocolProfile)
	require.Equal(t, "1.0.0", conformantResponses.ProtocolProfileVersion)
	require.Equal(t, "locally_conformant", conformantResponses.CatalogEvidenceState)
	require.Equal(t, []string{CapabilityReasoningEffort, CapabilityStreaming}, conformantResponses.Capabilities)
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

func TestReviewedPublicLimitDefaultsUseExecutableCeilingsNotRequestDefaults(t *testing.T) {
	deepSeek, ok := Lookup("deepseek.v4-pro.api", "1.2.0")
	require.True(t, ok)
	require.Equal(t, 1_000_000, DefaultPublicContextLimit(deepSeek))
	require.Equal(t, 384_000, DefaultPublicOutputLimit(deepSeek))
	require.Equal(t, 32_768, deepSeek.DefaultOutputLimit)

	grok, ok := Lookup("xai.grok-4.6.api", "1.1.0")
	require.True(t, ok)
	require.Equal(t, 500_000, DefaultPublicContextLimit(grok))
	require.Equal(t, 131_072, DefaultPublicOutputLimit(grok))
	require.Zero(t, grok.ProviderMaxOutput)
	require.Equal(t, 32_768, grok.DefaultOutputLimit)
	require.Equal(t, 32_768, DefaultRequestOutputLimit(grok, 131_072))
	require.Equal(t, 16_384, DefaultRequestOutputLimit(grok, 16_384))
}

func TestGPT56ExecutableContextStopsBeforeUnmodeledLongContextPricing(t *testing.T) {
	for _, profileID := range []string{"openai.gpt-5.6-sol.chat", "openai.gpt-5.6-terra.chat", "openai.gpt-5.6-luna.chat"} {
		t.Run(profileID, func(t *testing.T) {
			profile, ok := Lookup(profileID, "1.0.0")
			require.True(t, ok)
			require.NoError(t, ValidateEffectiveLimits(profile, 272_000, 128_000))
			require.ErrorContains(t, ValidateEffectiveLimits(profile, 272_001, 128_000), "executable profile maximum 272000")
		})
	}
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

	current, ok := Match("MiniMax-M2.7")
	require.True(t, ok)
	require.Equal(t, "1.5.0", current.Version)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v7", current.ProtocolProfile)
	require.Equal(t, "cache-usage.minimax-openai.v1", current.CacheUsageProfile)
	require.Equal(t, []string{EndpointClassMiniMaxCNOpenAIV1}, current.EndpointClasses)
	require.Equal(t, []string{"MiniMax-M2.7"}, current.UpstreamModelIDs)
	require.Equal(t, []string{"MiniMax M2.7", "MiniMax-M2.7"}, current.Aliases)
	require.Contains(t, current.Capabilities, CapabilityStreaming)
	require.Contains(t, current.Capabilities, CapabilityReasoning)
	require.Contains(t, current.Capabilities, CapabilityPromptCache)
	require.Contains(t, current.Capabilities, CapabilityCacheReadUsage)
	require.Equal(t, []string{
		CapabilityReasoning, CapabilityPromptCache, CapabilityCacheReadUsage,
	}, current.AlwaysRequiredCapabilities)
	contract, ok := ResolveChatProtocolContract(current.ProtocolProfile)
	require.True(t, ok)
	require.EqualValues(t, 204_800, contract.OutputLimitMax)

	m25, ok := Match("MiniMax-M2.5-highspeed")
	require.True(t, ok)
	require.Equal(t, "1.5.0", m25.Version)
	require.Equal(t, "protocol.minimax-m2-openai-chat.v7", m25.ProtocolProfile)
	require.Equal(t, "cache-usage.minimax-openai.v1", m25.CacheUsageProfile)
	require.Equal(t, []string{EndpointClassMiniMaxCNOpenAIV1}, m25.EndpointClasses)
	require.Contains(t, m25.Capabilities, CapabilityReasoning)
	require.Contains(t, m25.Capabilities, CapabilityPromptCache)
	require.Contains(t, m25.Capabilities, CapabilityCacheReadUsage)
	require.Equal(t, []string{
		CapabilityReasoning, CapabilityPromptCache, CapabilityCacheReadUsage,
	}, m25.AlwaysRequiredCapabilities)

	currentM3, ok := Match("MiniMax-M3")
	require.True(t, ok)
	require.Equal(t, "1.7.0", currentM3.Version)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v9", currentM3.ProtocolProfile)
	require.Equal(t, "cache-usage.minimax-openai.v1", currentM3.CacheUsageProfile)
	require.Equal(t, []string{EndpointClassMiniMaxCNOpenAIV1}, currentM3.EndpointClasses)
	require.Contains(t, currentM3.Capabilities, CapabilityReasoning)
	require.Contains(t, currentM3.Capabilities, CapabilityVision)
	require.Contains(t, currentM3.Capabilities, CapabilityServiceTier)
	require.Contains(t, currentM3.Capabilities, CapabilityPromptCache)
	require.Contains(t, currentM3.Capabilities, CapabilityCacheReadUsage)
	require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, currentM3.AlwaysRequiredCapabilities)
	require.NotContains(t, currentM3.Capabilities, CapabilityVideo)
	m3Contract, ok := ResolveChatProtocolContract(currentM3.ProtocolProfile)
	require.True(t, ok)
	require.EqualValues(t, 524_288, m3Contract.OutputLimitMax)

	historical, ok := Lookup("minimax.m2.7.token-plan", "1.1.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, []string{EndpointClassProviderHTTPS}, historical.EndpointClasses)
	require.Equal(t, []string{"MiniMax-M2.7", "MiniMax-M2.7-highspeed"}, historical.UpstreamModelIDs)
}

func TestLegacyProviderBundlesRemainImmutableWhenTypedProtocolsAdvance(t *testing.T) {
	deepSeekLegacy, ok := Lookup("deepseek.v4-pro.api", "1.1.0")
	require.True(t, ok)
	require.Equal(t, "protocol.deepseek-openai-chat.v1", deepSeekLegacy.ProtocolProfile)
	require.Equal(t, []string{EndpointClassProviderHTTPS}, deepSeekLegacy.EndpointClasses)
	require.NotContains(t, deepSeekLegacy.Capabilities, CapabilityStreaming)
	require.True(t, deepSeekLegacy.LegacyProtocolAuthority)
	_, ok = ResolveChatProtocolContract(deepSeekLegacy.ProtocolProfile)
	require.False(t, ok)

	deepSeekCurrent, ok := Match("DeepSeek V4 Pro")
	require.True(t, ok)
	require.Equal(t, "1.5.0", deepSeekCurrent.Version)
	require.Equal(t, "protocol.deepseek-openai-chat.v4", deepSeekCurrent.ProtocolProfile)
	require.Equal(t, []string{EndpointClassDeepSeekOfficialOpenAI}, deepSeekCurrent.EndpointClasses)
	require.Contains(t, deepSeekCurrent.Capabilities, CapabilityStreaming)
	require.NotContains(t, deepSeekCurrent.Capabilities, CapabilityParallelTools)
	require.False(t, deepSeekCurrent.LegacyProtocolAuthority)

	kimiLegacy, ok := Lookup("kimi.k2.6.api", "1.5.0")
	require.True(t, ok)
	require.Equal(t, "protocol.kimi-openai-chat.v3", kimiLegacy.ProtocolProfile)
	require.NotContains(t, kimiLegacy.Capabilities, CapabilityStreaming)
	require.True(t, kimiLegacy.LegacyProtocolAuthority)
	kimiLegacyContract, ok := ResolveChatProtocolContract(kimiLegacy.ProtocolProfile)
	require.True(t, ok)
	require.False(t, kimiLegacyContract.FunctionToolsModeled)
	require.True(t, kimiLegacyContract.PartialMode)
	kimiCurrent, ok := Match("Kimi K2.6")
	require.True(t, ok)
	require.Equal(t, "1.7.0", kimiCurrent.Version)
	require.Equal(t, "protocol.kimi-openai-chat.v5", kimiCurrent.ProtocolProfile)
	require.Contains(t, kimiCurrent.Capabilities, CapabilityStreaming)
	require.False(t, kimiCurrent.LegacyProtocolAuthority)

	miniMaxLegacy, ok := Lookup("minimax.m3.token-plan", "1.0.0")
	require.True(t, ok)
	require.Equal(t, "protocol.minimax-openai-chat.v2", miniMaxLegacy.ProtocolProfile)
	require.NotContains(t, miniMaxLegacy.Capabilities, CapabilityStreaming)
	require.True(t, miniMaxLegacy.LegacyProtocolAuthority)
	_, ok = ResolveChatProtocolContract(miniMaxLegacy.ProtocolProfile)
	require.False(t, ok)
	miniMaxCurrent, ok := Match("MiniMax-M3")
	require.True(t, ok)
	require.Equal(t, "1.7.0", miniMaxCurrent.Version)
	require.Equal(t, "protocol.minimax-m3-openai-chat.v9", miniMaxCurrent.ProtocolProfile)
	require.Contains(t, miniMaxCurrent.Capabilities, CapabilityStreaming)
	require.Contains(t, miniMaxCurrent.Capabilities, CapabilityReasoning)
	require.Contains(t, miniMaxCurrent.Capabilities, CapabilityServiceTier)
	require.Contains(t, miniMaxCurrent.Capabilities, CapabilityPromptCache)
	require.Contains(t, miniMaxCurrent.Capabilities, CapabilityCacheReadUsage)
	require.False(t, miniMaxCurrent.LegacyProtocolAuthority)
}

func TestLegacyProtocolAuthorityContainsOnlyExistingHistoricalAddresses(t *testing.T) {
	for _, profile := range All() {
		if profile.Current {
			require.False(t, profile.LegacyProtocolAuthority, "%s@%s", profile.ID, profile.Version)
		}
	}
	for address := range immutableLegacyProtocolAuthorities {
		parts := strings.Split(address, "@")
		require.Len(t, parts, 2, address)
		profile, ok := Lookup(parts[0], parts[1])
		require.True(t, ok, address)
		require.False(t, profile.Current, address)
		require.True(t, profile.LegacyProtocolAuthority, address)
	}
}

func TestCatalogReturnsDefensiveCopies(t *testing.T) {
	profiles := All()
	profiles[0].Aliases = append(profiles[0].Aliases, "mutation")
	profiles[len(profiles)-1].ProviderCapabilities = append(profiles[len(profiles)-1].ProviderCapabilities, "mutation")
	again := All()
	require.NotContains(t, again[0].Aliases, "mutation")
	require.NotContains(t, again[len(again)-1].ProviderCapabilities, "mutation")
}

func TestClaudeSonnet5StrictMessagesProfileIsAppendOnlyAndTyped(t *testing.T) {
	historical, ok := Lookup("anthropic.claude-sonnet-5.api", "1.0.0")
	require.True(t, ok)
	require.Empty(t, historical.ProtocolProfileVersion)
	require.Empty(t, historical.OperationPolicyProfile)
	require.Empty(t, historical.Capabilities)
	require.Equal(t, []string{EndpointClassProviderHTTPS}, historical.EndpointClasses)

	profile, ok := Lookup("anthropic.claude-sonnet-5.api", "1.1.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, endpointprotocol.ProfileAnthropicMessagesText, profile.ProtocolProfile)
	require.Equal(t, "1.0.0", profile.ProtocolProfileVersion)
	require.Equal(t, AnthropicMessagesSonnet5CorePolicyV1, profile.OperationPolicyProfile)
	require.Equal(t, "reasoning.anthropic-sonnet-5-adaptive.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoning}, profile.Capabilities)
	require.Equal(t, []string{EndpointClassAnthropicOfficialOrigin}, profile.EndpointClasses)
	require.Equal(t, "locally_conformant", profile.CatalogEvidenceState)
	require.NoError(t, ValidateExecutableComponents(profile))

	reasoning, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeToggle, reasoning.Mode)
	require.Equal(t, "thinking", reasoning.RequestObjectField)
	require.Equal(t, "type", reasoning.ToggleControl.Field)
	require.Equal(t, []string{"adaptive", "disabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, "adaptive", reasoning.ToggleControl.ProviderDefault)
	require.Equal(t, []string{"temperature", "top_p", "top_k"}, reasoning.UnsupportedRequestFieldsWhileActive)

	policy, ok := ResolveAnthropicMessagesOperationContract(profile.OperationPolicyProfile)
	require.True(t, ok)
	require.True(t, policy.RequireFinalUserMessage)
	require.True(t, policy.RefusalStopDetails)
	require.Contains(t, policy.AllowedResponseStops, "refusal")

	controls, ok := Lookup("anthropic.claude-sonnet-5.api", "1.2.0")
	require.True(t, ok)
	require.False(t, controls.Current)
	require.Equal(t, endpointprotocol.ProfileAnthropicMessagesText, controls.ProtocolProfile)
	require.Equal(t, "1.0.0", controls.ProtocolProfileVersion)
	require.Equal(t, AnthropicMessagesSonnet5EvidencePolicyV2, controls.OperationPolicyProfile)
	require.Equal(t, []string{CapabilityReasoning, CapabilityReasoningEffort}, controls.Capabilities)
	require.Equal(t, "reasoning.anthropic-sonnet-5-adaptive-effort.v1", controls.ReasoningProfile)
	require.NoError(t, ValidateExecutableComponents(controls))

	reasoning, ok = ResolveReasoningContract(controls.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, "thinking", reasoning.ToggleRequestObjectField())
	require.Equal(t, "output_config", reasoning.EffortRequestObjectField())
	require.Equal(t, []string{"adaptive", "disabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, "adaptive", reasoning.ToggleControl.ProviderDefault)
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, "high", reasoning.EffortControl.ProviderDefault)

	controlsPolicy, ok := ResolveAnthropicMessagesOperationContract(controls.OperationPolicyProfile)
	require.True(t, ok)
	require.True(t, controlsPolicy.ResponseInferenceGeo)
	require.True(t, controlsPolicy.ResponseServiceTier)

	streaming, ok := Lookup("anthropic.claude-sonnet-5.api", "1.3.0")
	require.True(t, ok)
	require.False(t, streaming.Current)
	require.Equal(t, APISurfaceAnthropicMessage, streaming.APISurface)
	require.Equal(t, []string{OperationAnthropicMessage}, streaming.Operations)
	require.Equal(t, endpointprotocol.ProfileAnthropicMessagesTextV2, streaming.ProtocolProfile)
	require.Equal(t, "1.0.0", streaming.ProtocolProfileVersion)
	require.Equal(t, AnthropicMessagesSonnet5EvidencePolicyV2, streaming.OperationPolicyProfile)
	require.Equal(t, []string{CapabilityReasoning, CapabilityReasoningEffort, CapabilityStreaming}, streaming.Capabilities)
	require.Equal(t, []string{EndpointClassAnthropicOfficialOrigin}, streaming.EndpointClasses)
	require.NoError(t, ValidateExecutableComponents(streaming))
	require.Error(t, ValidateNewAttachment(streaming.ID, streaming.Version))
	require.Error(t, ValidateNewAttachment(controls.ID, controls.Version))

	tools, ok := Lookup("anthropic.claude-sonnet-5.api", "1.4.0")
	require.True(t, ok)
	require.True(t, tools.Current)
	require.Equal(t, endpointprotocol.ProfileAnthropicMessagesTextV3, tools.ProtocolProfile)
	require.Equal(t, "1.0.0", tools.ProtocolProfileVersion)
	require.Equal(t, AnthropicMessagesSonnet5ToolsPolicyV3, tools.OperationPolicyProfile)
	require.Equal(t, []string{
		CapabilityReasoning, CapabilityReasoningEffort, CapabilityStreaming,
		CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
		CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
	}, tools.Capabilities)
	require.NoError(t, ValidateExecutableComponents(tools))
	require.NoError(t, ValidateNewAttachment(tools.ID, tools.Version))
	toolsPolicy, ok := ResolveAnthropicMessagesOperationContract(tools.OperationPolicyProfile)
	require.True(t, ok)
	require.NotNil(t, toolsPolicy.FunctionTools)
	require.True(t, toolsPolicy.FunctionTools.ExactHistory)
	require.True(t, toolsPolicy.FunctionTools.ExactResponse)
	require.True(t, toolsPolicy.FunctionTools.ToolInputIdentityGuarded)

	current, ok := MatchForSurface("claude-sonnet-5", APISurfaceAnthropicMessage)
	require.True(t, ok)
	require.Equal(t, "1.4.0", current.Version)
}

func TestClaudeOpus5MessagesProfileModelsDisabledThinkingEffortConstraint(t *testing.T) {
	historical, ok := Lookup("anthropic.claude-opus-5.api", "1.0.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Empty(t, historical.ProtocolProfileVersion)
	require.Empty(t, historical.OperationPolicyProfile)
	require.Empty(t, historical.Capabilities)

	profile, ok := Lookup("anthropic.claude-opus-5.api", "1.1.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, endpointprotocol.ProfileAnthropicMessagesText, profile.ProtocolProfile)
	require.Equal(t, "1.0.0", profile.ProtocolProfileVersion)
	require.Equal(t, AnthropicMessagesOpus5EvidencePolicyV1, profile.OperationPolicyProfile)
	require.Equal(t, "reasoning.anthropic-opus-5-adaptive-effort.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoning, CapabilityReasoningEffort}, profile.Capabilities)
	require.Equal(t, []string{EndpointClassAnthropicOfficialOrigin}, profile.EndpointClasses)
	require.Equal(t, "locally_conformant", profile.CatalogEvidenceState)
	require.NoError(t, ValidateExecutableComponents(profile))

	reasoning, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, []string{"adaptive", "disabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, []string{"disabled"}, reasoning.ToggleControl.DisabledValues)
	require.Equal(t, "adaptive", reasoning.ToggleControl.ProviderDefault)
	require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, []string{"low", "medium", "high"}, reasoning.EffortValuesWhenDisabled)
	require.Equal(t, "high", reasoning.EffortControl.ProviderDefault)

	policy, ok := ResolveAnthropicMessagesOperationContract(profile.OperationPolicyProfile)
	require.True(t, ok)
	require.True(t, policy.ResponseInferenceGeo)
	require.True(t, policy.ResponseServiceTier)

	streaming, ok := Lookup("anthropic.claude-opus-5.api", "1.2.0")
	require.True(t, ok)
	require.True(t, streaming.Current)
	require.Equal(t, endpointprotocol.ProfileAnthropicMessagesTextV2, streaming.ProtocolProfile)
	require.Equal(t, "1.0.0", streaming.ProtocolProfileVersion)
	require.Equal(t, AnthropicMessagesOpus5EvidencePolicyV1, streaming.OperationPolicyProfile)
	require.Equal(t, "reasoning.anthropic-opus-5-adaptive-effort.v1", streaming.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoning, CapabilityReasoningEffort, CapabilityStreaming}, streaming.Capabilities)
	require.Equal(t, []string{EndpointClassAnthropicOfficialOrigin}, streaming.EndpointClasses)
	require.NoError(t, ValidateExecutableComponents(streaming))
	require.NoError(t, ValidateNewAttachment(streaming.ID, streaming.Version))
	require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))

	current, ok := MatchForSurface("claude-opus-5", APISurfaceAnthropicMessage)
	require.True(t, ok)
	require.Equal(t, "1.2.0", current.Version)
}

func TestClaudeHaiku45MessagesProfileModelsManualThinkingBudgetAndStrictStreaming(t *testing.T) {
	historical, ok := Lookup("anthropic.claude-haiku-4.5.api", "1.0.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Empty(t, historical.ProtocolProfileVersion)
	require.Empty(t, historical.OperationPolicyProfile)
	require.Empty(t, historical.Capabilities)

	profile, ok := Lookup("anthropic.claude-haiku-4.5.api", "1.1.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, endpointprotocol.ProfileAnthropicMessagesText, profile.ProtocolProfile)
	require.Equal(t, "1.0.0", profile.ProtocolProfileVersion)
	require.Equal(t, AnthropicMessagesHaiku45EvidencePolicyV1, profile.OperationPolicyProfile)
	require.Equal(t, "reasoning.anthropic-haiku-4.5-extended-budget.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoning, CapabilityReasoningBudget}, profile.Capabilities)
	require.Equal(t, []string{EndpointClassAnthropicOfficialOrigin}, profile.EndpointClasses)
	require.Equal(t, "locally_conformant", profile.CatalogEvidenceState)
	require.NoError(t, ValidateExecutableComponents(profile))

	reasoning, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, []string{"enabled", "disabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, []string{"disabled"}, reasoning.ToggleControl.DisabledValues)
	require.Equal(t, "disabled", reasoning.ToggleControl.ProviderDefault)
	require.Equal(t, "budget_tokens", reasoning.BudgetControl.Field)
	require.EqualValues(t, 1_024, reasoning.BudgetControl.Minimum)
	require.EqualValues(t, 63_999, reasoning.BudgetControl.Maximum)
	require.True(t, reasoning.BudgetControl.RequiresActiveReasoning)
	require.Equal(t, ReasoningResponseThinkingSummarized, reasoning.ResponseThinkingTextMode)

	policy, ok := ResolveAnthropicMessagesOperationContract(profile.OperationPolicyProfile)
	require.True(t, ok)
	require.True(t, policy.ResponseInferenceGeo)
	require.True(t, policy.ResponseServiceTier)

	streaming, ok := Lookup("anthropic.claude-haiku-4.5.api", "1.2.0")
	require.True(t, ok)
	require.True(t, streaming.Current)
	require.Equal(t, endpointprotocol.ProfileAnthropicMessagesTextV2, streaming.ProtocolProfile)
	require.Equal(t, "1.0.0", streaming.ProtocolProfileVersion)
	require.Equal(t, AnthropicMessagesHaiku45EvidencePolicyV1, streaming.OperationPolicyProfile)
	require.Equal(t, "reasoning.anthropic-haiku-4.5-extended-budget.v1", streaming.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoning, CapabilityReasoningBudget, CapabilityStreaming}, streaming.Capabilities)
	require.Equal(t, []string{EndpointClassAnthropicOfficialOrigin}, streaming.EndpointClasses)
	require.NoError(t, ValidateExecutableComponents(streaming))
	require.NoError(t, ValidateNewAttachment(streaming.ID, streaming.Version))
	require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))

	current, ok := MatchForSurface("claude-haiku-4-5-20251001", APISurfaceAnthropicMessage)
	require.True(t, ok)
	require.Equal(t, "1.2.0", current.Version)
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
		{"glm-5.3", "glm.5.3.api", 1_000_000, 131_072, 65_536, "protocol.glm-openai-chat.v5"},
		{"glm-5.2", "glm.5.2.api", 1_000_000, 131_072, 65_536, "protocol.glm-openai-chat.v5"},
		{"glm-5v-turbo", "glm.5v-turbo.api", 200_000, 131_072, 65_536, "protocol.glm-openai-chat.glm5v.v4"},
		{"qwen3.8-max", "qwen.3.8-max.api", 1_000_000, 131_072, 131_072, "protocol.qwen-openai-chat.v6"},
		{"qwen3.7-plus", "qwen.3.7-plus.api", 1_000_000, 131_072, 131_072, "protocol.qwen-openai-chat.v6"},
		{"qwen3.7-flash", "qwen.3.7-flash.api", 1_000_000, 131_072, 131_072, "protocol.qwen-openai-chat.flash.v6"},
		{"gemini-3.7-flash", "gemini.3.7-flash.api", 1_048_576, 65_536, 32_768, "protocol.gemini-openai-chat.v6"},
		{"gemini-3.5-flash", "gemini.3.5-flash.api", 1_048_576, 65_536, 32_768, "protocol.gemini-openai-chat.v6"},
		{"gemini-3.5-flash-lite", "gemini.3.5-flash-lite.api", 1_048_576, 65_536, 32_768, "protocol.gemini-openai-chat.v6"},
		{"mimo-v2.5-pro", "mimo.v2.5-pro.api", 1_000_000, 131_072, 131_072, "protocol.mimo-openai-chat.v3"},
		{"mimo-v2.5", "mimo.v2.5.api", 1_000_000, 131_072, 32_768, "protocol.mimo-openai-chat.v4"},
		{"hy3", "hy3.api", 262_144, 131_072, 131_072, "protocol.hy3-openai-chat.v4"},
		{"gpt-5.6-sol", "openai.gpt-5.6-sol.chat", 1_050_000, 128_000, 32_768, "protocol.openai-chat.v5"},
		{"gpt-5.6-terra", "openai.gpt-5.6-terra.chat", 1_050_000, 128_000, 32_768, "protocol.openai-chat.v5"},
		{"gpt-5.6-luna", "openai.gpt-5.6-luna.chat", 1_050_000, 128_000, 32_768, "protocol.openai-chat.v5"},
		{"grok-4.6", "xai.grok-4.6.api", 500_000, 0, 32_768, "protocol.xai-openai-chat.v1"},
		{"claude-opus-5", "anthropic.claude-opus-5.api", 1_000_000, 128_000, 32_768, endpointprotocol.ProfileAnthropicMessagesTextV2},
		{"claude-sonnet-5", "anthropic.claude-sonnet-5.api", 1_000_000, 128_000, 32_768, endpointprotocol.ProfileAnthropicMessagesTextV3},
		{"claude-haiku-4-5-20251001", "anthropic.claude-haiku-4.5.api", 200_000, 64_000, 16_384, endpointprotocol.ProfileAnthropicMessagesTextV2},
		{"doubao-seed-evolving", "doubao.seed-evolving.api", 1_048_576, 262_144, 4_096, "protocol.doubao-seed-evolving-openai-chat.v6"},
		{"doubao-seed-2-1-pro-260628", "doubao.seed-2.1-pro.api", 262_144, 262_144, 4_096, "protocol.doubao-seed-2.1-pro-openai-chat.v5"},
		{"doubao-seed-2-1-turbo-260628", "doubao.seed-2.1-turbo.api", 262_144, 262_144, 4_096, "protocol.doubao-seed-2.1-turbo-openai-chat.v5"},
	}
	require.Len(t, tests, 22)
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			profile, ok := Match(test.model)
			if strings.HasPrefix(test.model, "gpt-5.6-") {
				profile, ok = MatchForSurface(test.model, APISurfaceOpenAIChat)
			}
			require.True(t, ok)
			require.Equal(t, test.profileID, profile.ID)
			require.Equal(t, test.context, profile.ProviderContextWindow)
			require.Equal(t, test.maximum, profile.ProviderMaxOutput)
			require.Equal(t, test.defaultOut, profile.DefaultOutputLimit)
			require.Equal(t, test.protocolRef, profile.ProtocolProfile)
			require.NotEmpty(t, profile.ProviderCapabilities)
			if test.model == "glm-5.3" {
				require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "json_object", "prompt_cache"}, profile.Capabilities)
			} else if test.model == "glm-5.2" {
				require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "json_object", "prompt_cache"}, profile.Capabilities)
			} else if test.model == "glm-5v-turbo" {
				require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "prompt_cache", CapabilityVision, CapabilityVideo, CapabilityFiles}, profile.Capabilities)
			} else if test.model == "qwen3.8-max" {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq,
					CapabilityParallelTools, CapabilityToolStream, CapabilityJSONObject, CapabilityStrictJSONSchema,
					CapabilityPromptCache, CapabilityExplicitCache, CapabilityCacheReadUsage, CapabilityCacheCreateUsage,
					CapabilityVision, CapabilityVideo,
					CapabilityBuiltinSearchReq, CapabilityReasoningBudget,
				}, profile.Capabilities)
				require.Equal(t, "1.8.0", profile.Version)
				require.NotContains(t, profile.Capabilities, CapabilityJSONSchema)
			} else if test.model == "qwen3.7-plus" {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq,
					CapabilityParallelTools, CapabilityToolStream, CapabilityJSONObject, CapabilityStrictJSONSchema,
					CapabilityPromptCache, CapabilityExplicitCache, CapabilityCacheReadUsage, CapabilityCacheCreateUsage,
					CapabilityVision, CapabilityVideo,
					CapabilityBuiltinSearchReq,
				}, profile.Capabilities)
				require.Equal(t, "1.7.0", profile.Version)
				require.NotContains(t, profile.Capabilities, CapabilityJSONSchema)
			} else if test.model == "qwen3.7-flash" {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq,
					CapabilityParallelTools, CapabilityToolStream, CapabilityJSONObject,
					CapabilityPromptCache, CapabilityExplicitCache, CapabilityCacheReadUsage, CapabilityCacheCreateUsage,
					CapabilityVision, CapabilityVideo,
					CapabilityBuiltinSearchReq,
				}, profile.Capabilities)
				require.NotContains(t, profile.ProviderCapabilities, CapabilityJSONSchema)
				require.Equal(t, "1.7.0", profile.Version)
			} else if test.model == "gemini-3.7-flash" {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityStrictJSONSchema, CapabilityVision, CapabilityAudio,
					CapabilityServiceTier,
				}, profile.Capabilities)
				require.Equal(t, "1.7.0", profile.Version)
				require.Equal(t, ProcessingTierPolicyGeminiOpenAICompatibilityV1, profile.ProcessingTierPolicy)
				require.NotContains(t, profile.Capabilities, CapabilityJSONSchema)
			} else if test.model == "gemini-3.5-flash" {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityStrictJSONSchema, CapabilityVision, CapabilityAudio,
					CapabilityServiceTier,
				}, profile.Capabilities)
				require.Equal(t, "1.7.0", profile.Version)
				require.Equal(t, ProcessingTierPolicyGeminiOpenAICompatibilityV1, profile.ProcessingTierPolicy)
				require.NotContains(t, profile.Capabilities, CapabilityJSONSchema)
			} else if test.model == "gemini-3.5-flash-lite" {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityStrictJSONSchema, CapabilityVision, CapabilityAudio,
					CapabilityServiceTier,
				}, profile.Capabilities)
				require.Equal(t, "1.7.0", profile.Version)
				require.Equal(t, ProcessingTierPolicyGeminiOpenAICompatibilityV1, profile.ProcessingTierPolicy)
				require.NotContains(t, profile.Capabilities, CapabilityJSONSchema)
			} else if test.model == "mimo-v2.5-pro" {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityJSONObject,
					CapabilityPromptCache, CapabilityCacheReadUsage,
				}, profile.Capabilities)
				require.NotContains(t, profile.ProviderCapabilities, CapabilityJSONSchema)
				require.Equal(t, "1.5.0", profile.Version)
			} else if test.model == "mimo-v2.5" {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityJSONObject,
					CapabilityPromptCache, CapabilityCacheReadUsage,
					CapabilityVision, CapabilityAudio, CapabilityVideo,
				}, profile.Capabilities)
				require.NotContains(t, profile.ProviderCapabilities, CapabilityJSONSchema)
				require.Equal(t, "1.6.0", profile.Version)
			} else if test.model == "hy3" {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityJSONObject, CapabilityJSONSchema,
					CapabilityPromptCache, CapabilityCacheReadUsage, CapabilityBuiltinSearchReq,
				}, profile.Capabilities)
				require.NotContains(t, profile.Capabilities, CapabilityVision)
				require.Equal(t, "1.6.0", profile.Version)
			} else if strings.HasPrefix(test.model, "gpt-5.6-") {
				require.Equal(t, []string{
					CapabilityStreaming, "reasoning", "reasoning_effort", CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
					CapabilityJSONObject, CapabilityStrictJSONSchema, CapabilityVision,
					CapabilityPromptCache, CapabilityExplicitCache, CapabilityCacheReadUsage, CapabilityCacheCreateUsage,
				}, profile.Capabilities)
				require.NotContains(t, profile.Capabilities, CapabilityBuiltinWebSearch)
				require.NotContains(t, profile.Capabilities, "service_tier")
				require.Equal(t, "1.0.0", profile.Version)
			} else if test.model == "grok-4.6" {
				require.Equal(t, []string{CapabilityStreaming}, profile.Capabilities)
				require.Equal(t, "1.1.0", profile.Version)
				require.Zero(t, profile.ProviderMaxOutput)
				require.Equal(t, 131_072, profile.ExecutableMaxOutput)
			} else if test.model == "claude-opus-5" {
				require.Equal(t, []string{CapabilityReasoning, CapabilityReasoningEffort, CapabilityStreaming}, profile.Capabilities)
				require.Equal(t, "1.2.0", profile.Version)
				require.Equal(t, AnthropicMessagesOpus5EvidencePolicyV1, profile.OperationPolicyProfile)
				require.Equal(t, "reasoning.anthropic-opus-5-adaptive-effort.v1", profile.ReasoningProfile)
			} else if test.model == "claude-sonnet-5" {
				require.Equal(t, []string{
					CapabilityReasoning, CapabilityReasoningEffort, CapabilityStreaming,
					CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
					CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
				}, profile.Capabilities)
				require.Equal(t, "1.4.0", profile.Version)
				require.Equal(t, AnthropicMessagesSonnet5ToolsPolicyV3, profile.OperationPolicyProfile)
				require.Equal(t, "reasoning.anthropic-sonnet-5-adaptive-effort.v1", profile.ReasoningProfile)
			} else if test.model == "claude-haiku-4-5-20251001" {
				require.Equal(t, []string{CapabilityReasoning, CapabilityReasoningBudget, CapabilityStreaming}, profile.Capabilities)
				require.Equal(t, "1.2.0", profile.Version)
				require.Equal(t, AnthropicMessagesHaiku45EvidencePolicyV1, profile.OperationPolicyProfile)
				require.Equal(t, "reasoning.anthropic-haiku-4.5-extended-budget.v1", profile.ReasoningProfile)
			} else if test.model == "doubao-seed-2-1-pro-260628" {
				require.Equal(t, []string{
					CapabilityReasoning, CapabilityStreaming, CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
					CapabilityStrictJSONSchema, CapabilityPromptCache, CapabilityCacheReadUsage, CapabilityVision,
				}, profile.Capabilities)
				require.Equal(t, "1.6.0", profile.Version)
				require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, profile.AlwaysRequiredCapabilities)
				require.Equal(t, "cache-usage.doubao-ark-chat.v1", profile.CacheUsageProfile)
				require.Equal(t, DefaultOutputProvider, profile.DefaultOutputAuthority)
				require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
			} else if test.model == "doubao-seed-2-1-turbo-260628" {
				require.Equal(t, []string{
					CapabilityReasoningEffort, CapabilityStreaming, CapabilityTools, CapabilityFunctionTools,
					CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
					CapabilityStrictJSONSchema, CapabilityPromptCache, CapabilityCacheReadUsage, CapabilityVision,
				}, profile.Capabilities)
				require.Equal(t, "1.6.0", profile.Version)
				require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, profile.AlwaysRequiredCapabilities)
				require.Equal(t, "cache-usage.doubao-ark-chat.v1", profile.CacheUsageProfile)
				require.Equal(t, DefaultOutputProvider, profile.DefaultOutputAuthority)
				require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
			} else if test.model == "doubao-seed-evolving" {
				require.ElementsMatch(t, []string{
					CapabilityStreaming, CapabilityReasoning, CapabilityReasoningEffort,
					CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
					CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
					CapabilityStrictJSONSchema, CapabilityVision,
					CapabilityPromptCache, CapabilityCacheReadUsage,
				}, profile.Capabilities)
				require.Equal(t, "1.8.0", profile.Version)
				require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, profile.AlwaysRequiredCapabilities)
				require.Equal(t, "cache-usage.doubao-ark-chat.v1", profile.CacheUsageProfile)
				require.Equal(t, DefaultOutputProvider, profile.DefaultOutputAuthority)
				require.Equal(t, UpstreamVersionFloating, profile.UpstreamVersionPolicy)
				require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
			} else {
				require.Empty(t, profile.Capabilities, "documented provider claims must not become executable before conformance")
			}
		})
	}
}

func TestHy3ProfilesSealCurrentChatContractWithoutRewritingDocumentedHistory(t *testing.T) {
	locallyConformant := locallyConformantProviderProfiles()
	require.Condition(t, func() bool {
		return slices.ContainsFunc(locallyConformant, func(profile Profile) bool {
			return profile.ID == "hy3.api" && profile.Version == "1.6.0"
		})
	}, "Hy3 conformant chain must be generated")
	documented, ok := Lookup("hy3.api", "1.0.0")
	require.True(t, ok)
	require.Equal(t, 16_384, documented.DefaultOutputLimit)
	require.Equal(t, "protocol.hy3-openai-chat.documented.v1", documented.ProtocolProfile)

	current, ok := Match("hy3")
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
	require.Equal(t, 131_072, current.DefaultOutputLimit)
	require.Equal(t, "reasoning.hy3-chat.v1", current.ReasoningProfile)
	require.Equal(t, "cache-usage.hy3-openai.v1", current.CacheUsageProfile)
	require.NoError(t, ValidateExecutableComponents(current))

	contract, ok := ResolveChatProtocolContract(current.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"max_completion_tokens", "max_tokens"}, contract.OutputLimitAcceptedFields)
	require.EqualValues(t, 131_072, contract.OutputLimitMax)
	require.Equal(t, []string{"none", "auto"}, contract.FunctionToolChoices)
	require.True(t, contract.JSONSchemaAdditionalPropertiesFalseRequired)
	require.Equal(t, "web_search_options", contract.SearchOptionsField)
	require.Equal(t, []string{"lite", "standard"}, contract.SearchStrategies)

	reasoning, ok := ResolveReasoningContract(current.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, "disabled", reasoning.ToggleControl.ProviderDefault)
	require.Equal(t, []string{"low", "medium", "high"}, reasoning.EffortControl.AllowedValues)
	require.True(t, reasoning.EffortEnablesReasoning)
	require.Equal(t, map[string]string{"low": "high"}, reasoning.EffortEffectiveWithTools)

	cache, ok := ResolveCacheUsageContract(current.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, cache.WireSchema)
	require.True(t, cache.ReportsRead)
}

func TestQwenProtocolMilestonesRemainImmutable(t *testing.T) {
	base, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.v1")
	require.True(t, ok)
	require.True(t, base.Streaming)
	require.True(t, base.FunctionToolsModeled)
	require.False(t, base.FunctionTools)
	require.True(t, base.ResponseFormatModeled)
	require.Equal(t, []string{"text"}, base.ResponseFormatTypes)
	require.True(t, base.PromptCacheControlModeled)
	require.False(t, base.ExplicitPromptCache)
	require.False(t, base.RemoteImageHTTPSURL)
	require.True(t, base.SearchRequestModeled)
	require.False(t, base.BuiltinSearchRequest)
	require.True(t, base.OutputLimitFieldsMutuallyExclusive)

	tools, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.v2")
	require.True(t, ok)
	require.True(t, tools.FunctionToolsModeled)
	require.Equal(t, "tool_stream", tools.ToolStreamField)
	require.Equal(t, []string{"text"}, tools.ResponseFormatTypes)

	qwen38Reasoning, ok := ResolveReasoningContract("reasoning.qwen3.8-max-hybrid.v1")
	require.True(t, ok)
	require.Equal(t, "preserve_thinking", qwen38Reasoning.PreserveThinkingField)
	require.True(t, qwen38Reasoning.PreserveThinkingProviderDefault)
	require.Equal(t, map[string]int64{"max_tokens": 32_768}, qwen38Reasoning.ActiveOutputFieldMaximums)

	qwen38CorrectedReasoning, ok := ResolveReasoningContract("reasoning.qwen3.8-max-hybrid.v2")
	require.True(t, ok)
	require.Equal(t, "preserve_thinking", qwen38CorrectedReasoning.PreserveThinkingField)
	require.True(t, qwen38CorrectedReasoning.PreserveThinkingProviderDefault)
	require.Empty(t, qwen38CorrectedReasoning.ActiveOutputFieldMaximums)
	require.EqualValues(t, 0, qwen38CorrectedReasoning.BudgetControl.Minimum)
	require.EqualValues(t, 262_144, qwen38CorrectedReasoning.BudgetControl.Maximum)
	require.EqualValues(t, 131_072, qwen38CorrectedReasoning.BudgetControl.ProviderDefault)
	qwen38CorrectedProfile, ok := Lookup("qwen.3.8-max.api", "1.8.0")
	require.True(t, ok)
	require.Contains(t, qwen38CorrectedProfile.Capabilities, CapabilityReasoningBudget)

	structured, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.v3")
	require.True(t, ok)
	require.True(t, structured.ResponseFormatModeled)
	require.True(t, structured.PromptCacheControlModeled)
	require.False(t, structured.ExplicitPromptCache)

	cache, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.v4")
	require.True(t, ok)
	require.True(t, cache.PromptCacheControlModeled)
	require.False(t, cache.RemoteImageHTTPSURL)

	multimodal, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.v5")
	require.True(t, ok)
	require.True(t, multimodal.RemoteImageHTTPSURL)
	require.True(t, multimodal.SearchRequestModeled)
	require.False(t, multimodal.BuiltinSearchRequest)

	search, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.v6")
	require.True(t, ok)
	require.True(t, search.SearchRequestModeled)
}

func TestPreProfileAdmissionContractIsDerivedFromTypedProviderContracts(t *testing.T) {
	contract := PreProfileChatProtocolContract()
	require.Equal(t, "protocol.preprofile.chat.v2", contract.ProfileID)
	require.True(t, contract.PartialMode)
	require.True(t, contract.DynamicSystemTools)
	require.True(t, contract.BuiltinWebSearch)
	require.Equal(t, "$web_search", contract.BuiltinWebSearchName)
	require.True(t, contract.InlineImageDataURL)
	require.True(t, contract.RemoteImageHTTPSURL)
	require.True(t, contract.InlineAudioBase64)
	require.True(t, contract.RemoteAudioHTTPSURL)
	require.True(t, contract.InlineVideoDataURL)
	require.True(t, contract.RemoteVideoHTTPSURL)
	require.True(t, contract.RemoteFileHTTPSURL)
	require.False(t, contract.ExclusiveMediaKinds)
	require.Equal(t, "prompt_cache_breakpoint", contract.PromptCacheBreakpointField)

	fields := make(map[string]RequestExtensionWireType)
	for _, field := range PreProfileRequestExtensionFields() {
		fields[field.Field] = field.WireType
	}
	require.Equal(t, RequestExtensionInteger, fields["thinking_budget"])
	require.Equal(t, RequestExtensionBoolean, fields["preserve_thinking"])
	require.Equal(t, RequestExtensionBoolean, fields["tool_stream"])
	require.Equal(t, RequestExtensionBoolean, fields["enable_search"])
	require.Equal(t, RequestExtensionObject, fields["search_options"])
	require.Equal(t, RequestExtensionObject, fields["web_search_options"])
	require.Equal(t, RequestExtensionObject, fields["prompt_cache_options"])
}

func TestQwen37FlashUsesJSONObjectOnlyProtocolChain(t *testing.T) {
	versions := []struct {
		version  string
		protocol string
	}{
		{"1.1.0", "protocol.qwen-openai-chat.v1"},
		{"1.2.0", "protocol.qwen-openai-chat.v1"},
		{"1.3.0", "protocol.qwen-openai-chat.v2"},
		{"1.4.0", "protocol.qwen-openai-chat.flash.v3"},
		{"1.5.0", "protocol.qwen-openai-chat.flash.v4"},
		{"1.6.0", "protocol.qwen-openai-chat.flash.v5"},
		{"1.7.0", "protocol.qwen-openai-chat.flash.v6"},
	}
	for _, expected := range versions {
		profile, ok := Lookup("qwen.3.7-flash.api", expected.version)
		require.True(t, ok, expected.version)
		require.Equal(t, expected.protocol, profile.ProtocolProfile)
		require.NoError(t, ValidateExecutableComponents(profile))
	}

	structured, ok := ResolveChatProtocolContract("protocol.qwen-openai-chat.flash.v3")
	require.True(t, ok)
	require.Equal(t, []string{"text", "json_object"}, structured.ResponseFormatTypes)
	require.NotContains(t, structured.ResponseFormatTypes, "json_schema")

	current, ok := Match("qwen3.7-flash")
	require.True(t, ok)
	require.Equal(t, "1.7.0", current.Version)
	require.Contains(t, current.Capabilities, CapabilityJSONObject)
	require.NotContains(t, current.Capabilities, CapabilityJSONSchema)
}

func TestGemini37FlashOpenAIChatMilestonesRemainImmutable(t *testing.T) {
	versions := []struct {
		version  string
		protocol string
	}{
		{"1.1.0", "protocol.gemini-openai-chat.v1"},
		{"1.2.0", "protocol.gemini-openai-chat.v1"},
		{"1.3.0", "protocol.gemini-openai-chat.v2"},
		{"1.4.0", "protocol.gemini-openai-chat.v3"},
		{"1.5.0", "protocol.gemini-openai-chat.v4"},
		{"1.6.0", "protocol.gemini-openai-chat.v5"},
		{"1.7.0", "protocol.gemini-openai-chat.v6"},
		{"1.7.0", "protocol.gemini-openai-chat.v6"},
	}
	for _, expected := range versions {
		profile, ok := Lookup("gemini.3.7-flash.api", expected.version)
		require.True(t, ok, expected.version)
		require.Equal(t, expected.protocol, profile.ProtocolProfile)
		if expected.version != "1.6.0" && expected.version != "1.7.0" {
			require.Equal(t, []string{EndpointClassProviderHTTPS}, profile.EndpointClasses)
		}
		require.NoError(t, ValidateExecutableComponents(profile))
	}
	current, ok := Lookup("gemini.3.7-flash.api", "1.7.0")
	require.True(t, ok)
	require.Contains(t, current.Label, "OpenAI-compatible Core Chat 子集")
	require.Equal(t, []string{EndpointClassGeminiOpenAICompatibility}, current.EndpointClasses)
	for _, unsupported := range []string{CapabilityPromptCache, CapabilityBuiltinWebSearch, CapabilityVideo, "files"} {
		require.Contains(t, current.ProviderCapabilities, unsupported)
		require.NotContains(t, current.Capabilities, unsupported)
	}
	require.Contains(t, current.ProviderCapabilities, CapabilityServiceTier)
	require.Contains(t, current.Capabilities, CapabilityServiceTier)
	require.Equal(t, ProcessingTierPolicyGeminiOpenAICompatibilityV1, current.ProcessingTierPolicy)

	base, ok := ResolveChatProtocolContract("protocol.gemini-openai-chat.v1")
	require.True(t, ok)
	require.True(t, base.Streaming)
	require.Equal(t, "max_completion_tokens", base.OutputLimitField)
	require.Equal(t, []string{"max_completion_tokens", "max_tokens"}, base.OutputLimitAcceptedFields)
	require.EqualValues(t, 65_536, base.OutputLimitMax)
	require.False(t, base.FunctionTools)
	require.Equal(t, []string{"text"}, base.ResponseFormatTypes)
	require.False(t, base.InlineImageDataURL)
	require.False(t, base.InlineAudioBase64)

	tools, ok := ResolveChatProtocolContract("protocol.gemini-openai-chat.v2")
	require.True(t, ok)
	require.True(t, tools.FunctionTools)
	require.Equal(t, []string{"auto"}, tools.FunctionToolChoices)
	require.False(t, tools.NamedFunctionToolChoice)

	structured, ok := ResolveChatProtocolContract("protocol.gemini-openai-chat.v3")
	require.True(t, ok)
	require.Equal(t, []string{"text", "json_schema"}, structured.ResponseFormatTypes)
	require.True(t, structured.JSONSchemaStrictRequired)
	require.True(t, structured.JSONSchemaObjectRequired)

	image, ok := ResolveChatProtocolContract("protocol.gemini-openai-chat.v4")
	require.True(t, ok)
	require.True(t, image.InlineImageDataURL)
	require.Equal(t, []string{"image/jpeg"}, image.InlineImageMIMETypes)
	require.False(t, image.InlineAudioBase64)

	audio, ok := ResolveChatProtocolContract("protocol.gemini-openai-chat.v5")
	require.True(t, ok)
	require.True(t, audio.InlineAudioBase64)
	require.Equal(t, []string{"wav"}, audio.InlineAudioFormats)
	audio.InlineAudioFormats[0] = "mutation"
	again, ok := ResolveChatProtocolContract("protocol.gemini-openai-chat.v5")
	require.True(t, ok)
	require.Equal(t, []string{"wav"}, again.InlineAudioFormats)

	tier, ok := ResolveChatProtocolContract("protocol.gemini-openai-chat.v6")
	require.True(t, ok)
	require.Equal(t, ProcessingTierPolicyGeminiOpenAICompatibilityV1, tier.ProcessingTierPolicyID)
	policy, ok := ResolveProcessingTierPolicy(tier.ProcessingTierPolicyID)
	require.True(t, ok)
	require.Equal(t, ProcessingTierStandard, policy.OmittedNormalizedTier)
	require.Equal(t, "x-gemini-service-tier", policy.ObservedResponseHeader)
	require.Equal(t, []ProcessingTierRequestValue{
		{WireValue: "default", NormalizedTier: ProcessingTierStandard, RequestTimeoutSeconds: 300},
		{WireValue: "flex", NormalizedTier: ProcessingTierFlex, RequestTimeoutSeconds: 900},
		{WireValue: "priority", NormalizedTier: ProcessingTierPriority, RequestTimeoutSeconds: 300},
	}, policy.RequestValues)
	policy.RequestValues[0].WireValue = "mutation"
	againPolicy, ok := ResolveProcessingTierPolicy(tier.ProcessingTierPolicyID)
	require.True(t, ok)
	require.Equal(t, "default", againPolicy.RequestValues[0].WireValue)

	reasoning, ok := ResolveReasoningContract("reasoning.gemini-3.7-flash.v1")
	require.True(t, ok)
	require.Equal(t, ReasoningModeAlwaysOn, reasoning.Mode)
	require.Equal(t, "reasoning_effort", reasoning.EffortControl.Field)
	require.Equal(t, []string{"low", "medium", "high"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, "medium", reasoning.EffortControl.ProviderDefault)
}

func TestGemini35FlashUsesReviewedReasoningDefaultsAndSharedChatContracts(t *testing.T) {
	for _, expected := range []struct {
		version  string
		protocol string
	}{
		{"1.1.0", "protocol.gemini-openai-chat.v1"},
		{"1.2.0", "protocol.gemini-openai-chat.v1"},
		{"1.3.0", "protocol.gemini-openai-chat.v2"},
		{"1.4.0", "protocol.gemini-openai-chat.v3"},
		{"1.5.0", "protocol.gemini-openai-chat.v4"},
		{"1.6.0", "protocol.gemini-openai-chat.v5"},
	} {
		profile, ok := Lookup("gemini.3.5-flash.api", expected.version)
		require.True(t, ok, expected.version)
		require.Equal(t, expected.protocol, profile.ProtocolProfile)
		require.NoError(t, ValidateExecutableComponents(profile))
	}

	reasoning, ok := ResolveReasoningContract("reasoning.gemini-3.5-flash.v1")
	require.True(t, ok)
	require.Equal(t, ReasoningModeAlwaysOn, reasoning.Mode)
	require.Equal(t, []string{"minimal", "low", "medium", "high"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, "medium", reasoning.EffortControl.ProviderDefault)

	current, ok := Match("gemini-3.5-flash")
	require.True(t, ok)
	require.Equal(t, "1.7.0", current.Version)
	require.Equal(t, "reasoning.gemini-3.5-flash.v1", current.ReasoningProfile)
	require.Contains(t, current.ProviderCapabilities, "reasoning_effort")
	require.Contains(t, current.Capabilities, CapabilityAudio)
	require.Contains(t, current.Capabilities, CapabilityServiceTier)
	require.Equal(t, ProcessingTierPolicyGeminiOpenAICompatibilityV1, current.ProcessingTierPolicy)
}

func TestGemini35FlashLiteUsesMinimalReasoningDefaultAndSharedChatContracts(t *testing.T) {
	for _, version := range []string{"1.1.0", "1.2.0", "1.3.0", "1.4.0", "1.5.0", "1.6.0", "1.7.0"} {
		profile, ok := Lookup("gemini.3.5-flash-lite.api", version)
		require.True(t, ok, version)
		require.NoError(t, ValidateExecutableComponents(profile))
	}

	reasoning, ok := ResolveReasoningContract("reasoning.gemini-3.5-flash-lite.v1")
	require.True(t, ok)
	require.Equal(t, ReasoningModeAlwaysOn, reasoning.Mode)
	require.Equal(t, []string{"minimal", "low", "medium", "high"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, "minimal", reasoning.EffortControl.ProviderDefault)

	current, ok := Match("gemini-3.5-flash-lite")
	require.True(t, ok)
	require.Equal(t, "1.7.0", current.Version)
	require.Equal(t, "protocol.gemini-openai-chat.v6", current.ProtocolProfile)
	require.Equal(t, "reasoning.gemini-3.5-flash-lite.v1", current.ReasoningProfile)
	require.Contains(t, current.ProviderCapabilities, "reasoning_effort")
	require.Contains(t, current.Capabilities, CapabilityAudio)
	require.Contains(t, current.Capabilities, CapabilityServiceTier)
	require.Equal(t, ProcessingTierPolicyGeminiOpenAICompatibilityV1, current.ProcessingTierPolicy)
}

func TestMiMoV25ProfilesSeparateTextProFromOmniModalModel(t *testing.T) {
	for _, test := range []struct {
		profileID      string
		currentVersion string
		protocol       string
		multimodal     bool
	}{
		{"mimo.v2.5-pro.api", "1.5.0", "protocol.mimo-openai-chat.v3", false},
		{"mimo.v2.5.api", "1.6.0", "protocol.mimo-openai-chat.v4", true},
	} {
		current, ok := Lookup(test.profileID, test.currentVersion)
		require.True(t, ok, test.profileID)
		require.Equal(t, test.protocol, current.ProtocolProfile)
		require.Equal(t, "reasoning.mimo-v2.5-thinking-toggle.v1", current.ReasoningProfile)
		require.Equal(t, "cache-usage.mimo-openai.v1", current.CacheUsageProfile)
		require.NoError(t, ValidateExecutableComponents(current))
		for _, capability := range []string{CapabilityVision, CapabilityAudio, CapabilityVideo} {
			if test.multimodal {
				require.Contains(t, current.Capabilities, capability)
			} else {
				require.NotContains(t, current.Capabilities, capability)
			}
		}
	}

	base, ok := ResolveChatProtocolContract("protocol.mimo-openai-chat.v1")
	require.True(t, ok)
	require.True(t, base.Streaming)
	require.Equal(t, "max_completion_tokens", base.OutputLimitField)
	require.Equal(t, []string{"max_completion_tokens"}, base.OutputLimitAcceptedFields)
	require.EqualValues(t, 131_072, base.OutputLimitMax)

	tools, ok := ResolveChatProtocolContract("protocol.mimo-openai-chat.v2")
	require.True(t, ok)
	require.True(t, tools.FunctionToolStrictSupported)
	require.Equal(t, []string{"auto"}, tools.FunctionToolChoices)

	structured, ok := ResolveChatProtocolContract("protocol.mimo-openai-chat.v3")
	require.True(t, ok)
	require.Equal(t, []string{"text", "json_object"}, structured.ResponseFormatTypes)
	require.Equal(t, "json", structured.JSONObjectPromptKeyword)

	media, ok := ResolveChatProtocolContract("protocol.mimo-openai-chat.v4")
	require.True(t, ok)
	require.True(t, media.RemoteImageHTTPSURL)
	require.True(t, media.RemoteAudioHTTPSURL)
	require.True(t, media.RemoteVideoHTTPSURL)
	require.True(t, media.VideoFPSSupported)
	require.Equal(t, []string{"default", "max"}, media.VideoResolutions)

	reasoning, ok := ResolveReasoningContract("reasoning.mimo-v2.5-thinking-toggle.v1")
	require.True(t, ok)
	require.Equal(t, ReasoningModeToggle, reasoning.Mode)
	require.Equal(t, "enabled", reasoning.ToggleControl.ProviderDefault)
	require.Equal(t, map[string]string{"temperature": "1", "top_p": "0.95"}, reasoning.FixedRequestNumbers)
	require.True(t, reasoning.FixedRequestNumbersReasoningOnly)

	cache, ok := ResolveCacheUsageContract("cache-usage.mimo-openai.v1")
	require.True(t, ok)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, cache.WireSchema)
	require.True(t, cache.ReportsRead)
	require.False(t, cache.ReportsCreation)
}

func TestGLM53BaseChatProtocolIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.3.api", "1.1.0")
	require.True(t, ok)
	require.Equal(t, "glm.5.3.api", profile.ID)
	require.Equal(t, "1.1.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v1", profile.ProtocolProfile)
	require.Empty(t, profile.Capabilities, "base chat conformance must not promote advanced capabilities")

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, profile.ProtocolProfile, contract.ProfileID)
	require.False(t, contract.PartialMode)
	require.False(t, contract.DynamicSystemTools)
	require.False(t, contract.BuiltinWebSearch)
	require.False(t, contract.InlineImageDataURL)
	require.False(t, contract.InlineVideoDataURL)
	require.True(t, contract.StreamingModeled)
	require.False(t, contract.Streaming)

	documented, ok := Lookup("glm.5.3.api", "1.0.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.documented.v1", documented.ProtocolProfile)
}

func TestGLM52BaseChatProtocolIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.2.api", "1.1.0")
	require.True(t, ok)
	require.Equal(t, "glm.5.2.api", profile.ID)
	require.Equal(t, "1.1.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v1", profile.ProtocolProfile)
	require.Empty(t, profile.Capabilities)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.StreamingModeled)
	require.False(t, contract.Streaming)
	require.True(t, contract.OutputLimitModeled)
	require.Empty(t, contract.OutputLimitField)

	documented, ok := Lookup("glm.5.2.api", "1.0.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.documented.v1", documented.ProtocolProfile)
}

func TestGLM5VTurboBaseTextChatProtocolIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5v-turbo.api", "1.1.0")
	require.True(t, ok)
	require.Equal(t, "glm.5v-turbo.api", profile.ID)
	require.Equal(t, "1.1.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v1", profile.ProtocolProfile)
	require.Empty(t, profile.Capabilities, "base text chat must not promote documented multimodal or advanced capabilities")

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.StreamingModeled)
	require.False(t, contract.Streaming)
	require.True(t, contract.OutputLimitModeled)
	require.Empty(t, contract.OutputLimitField)
	require.False(t, contract.InlineImageDataURL)
	require.False(t, contract.InlineVideoDataURL)

	documented, ok := Lookup("glm.5v-turbo.api", "1.0.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.documented.v1", documented.ProtocolProfile)
	require.NoError(t, ValidateExecutableComponents(profile))
}

func TestGLM5VTurboStreamingIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5v-turbo.api", "1.2.0")
	require.True(t, ok)
	require.Equal(t, "1.2.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v2", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming}, profile.Capabilities)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.StreamingModeled)
	require.True(t, contract.Streaming)
	require.NoError(t, ValidateExecutableComponents(profile))

	base, ok := Lookup("glm.5v-turbo.api", "1.1.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.v1", base.ProtocolProfile)
}

func TestGLM5VTurboExplicitOutputLimitIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5v-turbo.api", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "1.3.0", profile.Version)
	require.Equal(t, 65_536, profile.DefaultOutputLimit)
	require.Equal(t, 131_072, profile.ProviderMaxOutput)
	require.Equal(t, "protocol.glm-openai-chat.v3", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming}, profile.Capabilities)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, "max_tokens", contract.OutputLimitField)
	require.Equal(t, int64(1), contract.OutputLimitMin)
	require.Equal(t, int64(131_072), contract.OutputLimitMax)
	require.NoError(t, ValidateExecutableComponents(profile))

	streaming, ok := Lookup("glm.5v-turbo.api", "1.2.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.v2", streaming.ProtocolProfile)
}

func TestGLM5VTurboReasoningIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5v-turbo.api", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "1.4.0", profile.Version)
	require.Equal(t, "reasoning.glm-5v-turbo-thinking-toggle.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning"}, profile.Capabilities)

	contract, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeToggle, contract.Mode)
	require.Equal(t, []string{"enabled", "disabled"}, contract.ToggleControl.AllowedValues)
	require.Equal(t, "enabled", contract.ToggleControl.ProviderDefault)
	require.Equal(t, []string{"high", "max"}, contract.EffortControl.AllowedValues)
	require.Equal(t, map[string]string{"low": "high", "medium": "high", "xhigh": "max"}, contract.EffortControl.CompatibilityValues)
	require.Equal(t, []string{"none", "minimal"}, contract.EffortControl.DisabledValues)
	require.Equal(t, "max", contract.EffortControl.ProviderDefault)
	require.True(t, contract.ThinkingClearSupported)
	require.True(t, contract.ThinkingClearProviderDefault)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract.EffortControl.CompatibilityValues["low"] = "mutation"
	again, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, "high", again.EffortControl.CompatibilityValues["low"])
}

func TestGLM5VTurboFunctionToolsAreVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5v-turbo.api", "1.5.0")
	require.True(t, ok)
	require.Equal(t, "1.5.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v4", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools"}, profile.Capabilities)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.FunctionToolsModeled)
	require.True(t, contract.FunctionTools)
	require.Equal(t, 128, contract.MaxFunctionTools)
	require.True(t, contract.FunctionToolDescriptionRequired)
	require.True(t, contract.FunctionToolParametersRequired)
	require.Equal(t, []string{"auto"}, contract.FunctionToolChoices)
	require.NoError(t, ValidateExecutableComponents(profile))

	reasoning, ok := Lookup("glm.5v-turbo.api", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.v3", reasoning.ProtocolProfile)
}

func TestGLM5VTurboStructuredOutputIsExplicitlyUnsupported(t *testing.T) {
	profile, ok := Lookup("glm.5v-turbo.api", "1.6.0")
	require.True(t, ok)
	require.Equal(t, "1.6.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.glm5v.v1", profile.ProtocolProfile)
	require.NotContains(t, profile.ProviderCapabilities, "json_object")
	require.NotContains(t, profile.ProviderCapabilities, "json_schema")
	require.NotContains(t, profile.Capabilities, "json_object")
	require.NotContains(t, profile.Capabilities, "json_schema")

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.ResponseFormatModeled)
	require.Empty(t, contract.ResponseFormatTypes)
	require.NoError(t, ValidateExecutableComponents(profile))

	tools, ok := Lookup("glm.5v-turbo.api", "1.5.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.v4", tools.ProtocolProfile)
}

func TestGLM5VTurboCacheReadUsageIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5v-turbo.api", "1.7.0")
	require.True(t, ok)
	require.Equal(t, "1.7.0", profile.Version)
	require.Equal(t, "cache-usage.glm-openai.v1", profile.CacheUsageProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "prompt_cache"}, profile.Capabilities)

	contract, ok := ResolveCacheUsageContract(profile.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageModeAutomatic, contract.Mode)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, contract.WireSchema)
	require.NoError(t, ValidateExecutableComponents(profile))

	structured, ok := Lookup("glm.5v-turbo.api", "1.6.0")
	require.True(t, ok)
	require.Equal(t, "cache-usage.glm-documented.v1", structured.CacheUsageProfile)
}

func TestGLM5VTurboImageInputIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5v-turbo.api", "1.8.0")
	require.True(t, ok)
	require.Equal(t, "1.8.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.glm5v.v2", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "prompt_cache", CapabilityVision}, profile.Capabilities)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.InlineImageDataURL)
	require.True(t, contract.RemoteImageHTTPSURL)
	require.Equal(t, []string{"image/png", "image/jpeg"}, contract.InlineImageMIMETypes)
	require.False(t, contract.InlineVideoDataURL)
	require.False(t, contract.RemoteVideoHTTPSURL)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract.InlineImageMIMETypes[0] = "mutation"
	again, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, "image/png", again.InlineImageMIMETypes[0])
}

func TestGLM5VTurboVideoInputIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5v-turbo.api", "1.9.0")
	require.True(t, ok)
	require.Equal(t, "1.9.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.glm5v.v3", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "prompt_cache", CapabilityVision, CapabilityVideo}, profile.Capabilities)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.RemoteVideoHTTPSURL)
	require.False(t, contract.InlineVideoDataURL)
	require.True(t, contract.ExclusiveMediaKinds)
	require.False(t, contract.RemoteFileHTTPSURL)
	require.NoError(t, ValidateExecutableComponents(profile))
}

func TestGLM5VTurboFileInputIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Match("glm-5v-turbo")
	require.True(t, ok)
	require.Equal(t, "1.10.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.glm5v.v4", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "prompt_cache", CapabilityVision, CapabilityVideo, CapabilityFiles}, profile.Capabilities)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.RemoteFileHTTPSURL)
	require.Equal(t, []string{".txt", ".pdf"}, contract.RemoteFileExtensions)
	require.True(t, contract.ExclusiveMediaKinds)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract.RemoteFileExtensions[0] = ".mutation"
	again, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, ".txt", again.RemoteFileExtensions[0])
}

func TestGLM52StreamingIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.2.api", "1.2.0")
	require.True(t, ok)
	require.Equal(t, "1.2.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v2", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming}, profile.Capabilities)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.StreamingModeled)
	require.True(t, contract.Streaming)

	base, ok := Lookup("glm.5.2.api", "1.1.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.v1", base.ProtocolProfile)
}

func TestGLM52ExplicitOutputLimitIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.2.api", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "1.3.0", profile.Version)
	require.Equal(t, 65_536, profile.DefaultOutputLimit)
	require.Equal(t, 131_072, profile.ProviderMaxOutput)
	require.Equal(t, "protocol.glm-openai-chat.v3", profile.ProtocolProfile)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, "max_tokens", contract.OutputLimitField)
	require.Equal(t, int64(1), contract.OutputLimitMin)
	require.Equal(t, int64(131_072), contract.OutputLimitMax)
}

func TestGLM52ReasoningIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.2.api", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "1.4.0", profile.Version)
	require.Equal(t, "reasoning.glm-5.2-thinking-toggle.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning"}, profile.Capabilities)

	contract, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeToggle, contract.Mode)
	require.Equal(t, []string{"enabled", "disabled"}, contract.ToggleControl.AllowedValues)
	require.Equal(t, []string{"high", "max"}, contract.EffortControl.AllowedValues)
	require.Equal(t, map[string]string{"low": "high", "medium": "high", "xhigh": "max"}, contract.EffortControl.CompatibilityValues)
	require.Equal(t, []string{"none", "minimal"}, contract.EffortControl.DisabledValues)
	require.Equal(t, "max", contract.EffortControl.ProviderDefault)
	require.True(t, contract.ThinkingClearSupported)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract.EffortControl.DisabledValues[0] = "mutation"
	again, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, []string{"none", "minimal"}, again.EffortControl.DisabledValues)
}

func TestGLM52FunctionToolsAreVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.2.api", "1.5.0")
	require.True(t, ok)
	require.Equal(t, "1.5.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v4", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools"}, profile.Capabilities)
	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.FunctionToolsModeled)
	require.Equal(t, 128, contract.MaxFunctionTools)
	require.Equal(t, []string{"auto"}, contract.FunctionToolChoices)
}

func TestGLM52JSONObjectIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.2.api", "1.6.0")
	require.True(t, ok)
	require.Equal(t, "1.6.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v5", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "json_object"}, profile.Capabilities)
	require.NotContains(t, profile.Capabilities, "json_schema")
	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"text", "json_object"}, contract.ResponseFormatTypes)
}

func TestGLM52CacheReadUsageIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Match("glm-5.2")
	require.True(t, ok)
	require.Equal(t, "1.7.0", profile.Version)
	require.Equal(t, "cache-usage.glm-openai.v1", profile.CacheUsageProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "json_object", "prompt_cache"}, profile.Capabilities)
	contract, ok := ResolveCacheUsageContract(profile.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, contract.WireSchema)
	require.NoError(t, ValidateExecutableComponents(profile))
}

func TestGLM53StreamingProtocolIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.3.api", "1.2.0")
	require.True(t, ok)
	require.Equal(t, "glm.5.3.api", profile.ID)
	require.Equal(t, "1.2.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v2", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming}, profile.Capabilities)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.StreamingModeled)
	require.True(t, contract.Streaming)
	require.NoError(t, ValidateExecutableComponents(profile))

	base, ok := Lookup("glm.5.3.api", "1.1.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.v1", base.ProtocolProfile)
	require.Empty(t, base.Capabilities)
}

func TestGLM53ExplicitOutputLimitIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.3.api", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "1.3.0", profile.Version)
	require.Equal(t, 65_536, profile.DefaultOutputLimit)
	require.Equal(t, 131_072, profile.ProviderMaxOutput)
	require.Equal(t, "protocol.glm-openai-chat.v3", profile.ProtocolProfile)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.OutputLimitModeled)
	require.Equal(t, "max_tokens", contract.OutputLimitField)
	require.Equal(t, int64(1), contract.OutputLimitMin)
	require.Equal(t, int64(131_072), contract.OutputLimitMax)
	require.NoError(t, ValidateExecutableComponents(profile))

	streaming, ok := Lookup("glm.5.3.api", "1.2.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.v2", streaming.ProtocolProfile)
}

func TestGLM53ReasoningIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.3.api", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "1.4.0", profile.Version)
	require.Equal(t, "reasoning.glm-5.3-required.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning"}, profile.Capabilities)

	contract, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeRequired, contract.Mode)
	require.Equal(t, "thinking", contract.ToggleControl.Field)
	require.Equal(t, []string{"enabled"}, contract.ToggleControl.AllowedValues)
	require.Equal(t, "enabled", contract.ToggleControl.ProviderDefault)
	require.Equal(t, "reasoning_effort", contract.EffortControl.Field)
	require.Equal(t, []string{"low", "high", "max"}, contract.EffortControl.AllowedValues)
	require.Equal(t, "max", contract.EffortControl.ProviderDefault)
	require.True(t, contract.ThinkingClearSupported)
	require.True(t, contract.ThinkingClearProviderDefault)
	require.NoError(t, ValidateExecutableComponents(profile))

	outputLimit, ok := Lookup("glm.5.3.api", "1.3.0")
	require.True(t, ok)
	require.Equal(t, "reasoning.glm-5.3-documented.v1", outputLimit.ReasoningProfile)
}

func TestGLM53FunctionToolsAreVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.3.api", "1.5.0")
	require.True(t, ok)
	require.Equal(t, "1.5.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v4", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools"}, profile.Capabilities)

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.FunctionToolsModeled)
	require.True(t, contract.FunctionTools)
	require.Equal(t, 128, contract.MaxFunctionTools)
	require.True(t, contract.FunctionToolDescriptionRequired)
	require.True(t, contract.FunctionToolParametersRequired)
	require.Equal(t, []string{"auto"}, contract.FunctionToolChoices)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract.FunctionToolChoices[0] = "mutation"
	again, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"auto"}, again.FunctionToolChoices)

	reasoning, ok := Lookup("glm.5.3.api", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.v3", reasoning.ProtocolProfile)
}

func TestGLM53JSONObjectIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Lookup("glm.5.3.api", "1.6.0")
	require.True(t, ok)
	require.Equal(t, "1.6.0", profile.Version)
	require.Equal(t, "protocol.glm-openai-chat.v5", profile.ProtocolProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "json_object"}, profile.Capabilities)
	require.NotContains(t, profile.ProviderCapabilities, "json_schema")
	require.NotContains(t, profile.Capabilities, "json_schema")

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.ResponseFormatModeled)
	require.Equal(t, []string{"text", "json_object"}, contract.ResponseFormatTypes)

	contract.ResponseFormatTypes[0] = "mutation"
	again, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"text", "json_object"}, again.ResponseFormatTypes)

	tools, ok := Lookup("glm.5.3.api", "1.5.0")
	require.True(t, ok)
	require.Equal(t, "protocol.glm-openai-chat.v4", tools.ProtocolProfile)
}

func TestGLM53CacheReadUsageIsVersionedAndLocallyModeled(t *testing.T) {
	profile, ok := Match("glm-5.3")
	require.True(t, ok)
	require.Equal(t, "1.7.0", profile.Version)
	require.Equal(t, "cache-usage.glm-openai.v1", profile.CacheUsageProfile)
	require.Equal(t, []string{CapabilityStreaming, "reasoning", "tools", "json_object", "prompt_cache"}, profile.Capabilities)

	contract, ok := ResolveCacheUsageContract(profile.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageModeAutomatic, contract.Mode)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, contract.WireSchema)
	require.NoError(t, ValidateExecutableComponents(profile))

	jsonObject, ok := Lookup("glm.5.3.api", "1.6.0")
	require.True(t, ok)
	require.Equal(t, "cache-usage.glm-documented.v1", jsonObject.CacheUsageProfile)
}

func TestKimiComponentContractsAreVersionedAndDefensive(t *testing.T) {
	k3, ok := Match("kimi-k3")
	require.True(t, ok)
	require.Equal(t, "kimi.k3.api", k3.ID)
	require.Equal(t, "1.8.0", k3.Version)
	require.Equal(t, "protocol.kimi-openai-chat.k3.v5", k3.ProtocolProfile)
	require.Equal(t, "reasoning.kimi-k3-effort.v2", k3.ReasoningProfile)
	require.Equal(t, "cache-usage.kimi-platform.v1", k3.CacheUsageProfile)
	require.Equal(t, []string{EndpointClassKimiPlatformOpenAIV1}, k3.EndpointClasses)
	require.Equal(t, []string{"kimi-k3"}, k3.UpstreamModelIDs)
	require.Equal(t, []string{"Kimi K3", "kimi-k3"}, k3.Aliases)
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
	require.Equal(t, "1.7.0", k26.Version)
	require.Equal(t, TokenizerKimiK26ID, k26.TokenizerProfileID)
	require.Equal(t, TokenizerKimiK26Version, k26.TokenizerProfileVersion)
	require.Equal(t, "reasoning.kimi-k2.6-thinking-toggle.v2", k26.ReasoningProfile)
	require.Equal(t, "protocol.kimi-openai-chat.v5", k26.ProtocolProfile)
	require.Equal(t, []string{EndpointClassKimiPlatformOpenAIV1}, k26.EndpointClasses)
	require.Equal(t, []string{"kimi-k2.6"}, k26.UpstreamModelIDs)
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
	require.True(t, contract.BuiltinSearchRequiresThinkingDisabled)
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
	require.Equal(t, "1.3.0", k27.Version)
	require.Equal(t, "reasoning.kimi-k2.7-thinking-required.v2", k27.ReasoningProfile)
	require.Equal(t, "cache-usage.kimi-platform.v1", k27.CacheUsageProfile)
	require.Equal(t, "protocol.kimi-openai-chat.v5", k27.ProtocolProfile)
	require.Equal(t, []string{EndpointClassKimiPlatformOpenAIV1}, k27.EndpointClasses)
	require.Equal(t, []string{"kimi-k2.7-code"}, k27.UpstreamModelIDs)
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

func TestKimiHistoricalCodingAndPlatformIdentitiesRemainVersioned(t *testing.T) {
	k27Coding, ok := Lookup("kimi.k2.7-code.api", "1.0.0")
	require.True(t, ok)
	require.Equal(t, []string{EndpointClassProviderHTTPS}, k27Coding.EndpointClasses)
	require.Equal(t, []string{"kimi-for-coding", "kimi-for-coding-highspeed"}, k27Coding.UpstreamModelIDs)

	k3Coding, ok := Lookup("kimi.k3.api", "1.0.0")
	require.True(t, ok)
	require.Equal(t, []string{EndpointClassProviderHTTPS}, k3Coding.EndpointClasses)
	require.Equal(t, []string{"k3", "k3-256k"}, k3Coding.UpstreamModelIDs)

	k3HistoricalPlatform, ok := Lookup("kimi.k3.api", "1.5.0")
	require.True(t, ok)
	require.Equal(t, []string{EndpointClassProviderHTTPS}, k3HistoricalPlatform.EndpointClasses)
	require.Equal(t, []string{"kimi-k3", "k3", "k3-256k"}, k3HistoricalPlatform.UpstreamModelIDs)
}

func TestLatestDeepSeekProfilesAcceptOnlyDocumentedCompatibilityEffortAliases(t *testing.T) {
	tests := []struct {
		model         string
		version       string
		reasoningID   string
		allowed       []string
		compatibility map[string]string
	}{
		{
			model: "DeepSeek V4 Flash", version: "1.5.0",
			reasoningID:   "reasoning.deepseek-v4-flash-thinking-toggle.v5",
			allowed:       []string{"low", "high", "max"},
			compatibility: map[string]string{"medium": "high", "xhigh": "high"},
		},
		{
			model: "DeepSeek V4 Pro", version: "1.5.0",
			reasoningID:   "reasoning.deepseek-v4-pro-thinking-toggle.v5",
			allowed:       []string{"low", "high", "max"},
			compatibility: map[string]string{"medium": "high", "xhigh": "high"},
		},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			profile, ok := Match(test.model)
			require.True(t, ok)
			require.Equal(t, test.version, profile.Version)
			require.Equal(t, test.reasoningID, profile.ReasoningProfile)
			reasoning, found := ResolveReasoningContract(profile.ReasoningProfile)
			require.True(t, found)
			require.Equal(t, test.allowed, reasoning.EffortControl.AllowedValues)
			require.Equal(t, test.compatibility, reasoning.EffortControl.CompatibilityValues)
			reasoning.EffortControl.CompatibilityValues["medium"] = "mutation"
			again, resolved := ResolveReasoningContract(profile.ReasoningProfile)
			require.True(t, resolved)
			require.Equal(t, "high", again.EffortControl.CompatibilityValues["medium"])
		})
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

func TestDoubaoSeed21ProCandidateFreezesExactArkChatCore(t *testing.T) {
	historical, ok := Lookup("doubao.seed-2.1-pro.api", "1.0.0")
	require.True(t, ok)
	require.Equal(t, []string{EndpointClassProviderHTTPS}, historical.EndpointClasses)
	require.Equal(t, 32_768, historical.DefaultOutputLimit)
	require.Equal(t, DefaultOutputPolicy, historical.DefaultOutputAuthority)
	require.Empty(t, historical.Capabilities)

	profile, ok := Lookup("doubao.seed-2.1-pro.api", "1.1.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao", profile.Provider)
	require.Equal(t, "cn-beijing", profile.Region)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, []string{"doubao-seed-2-1-pro-260628"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, 262_144, profile.ProviderContextWindow)
	require.Equal(t, 262_144, profile.ProviderMaxOutput)
	require.Equal(t, 4_096, profile.DefaultOutputLimit)
	require.Equal(t, DefaultOutputProvider, profile.DefaultOutputAuthority)
	require.Equal(t, []string{CapabilityReasoning}, profile.Capabilities)
	require.Empty(t, profile.AlwaysRequiredCapabilities)
	require.Equal(t, "reasoning.doubao-seed-2.1-pro-toggle.v1", profile.ReasoningProfile)
	require.Empty(t, profile.CacheUsageProfile)
	require.Equal(t, "protocol.doubao-seed-2.1-pro-openai-chat.v1", profile.ProtocolProfile)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.StreamingModeled)
	require.False(t, contract.Streaming)
	require.Equal(t, "max_tokens", contract.OutputLimitField)
	require.Equal(t, []string{"max_tokens", "max_completion_tokens"}, contract.OutputLimitAcceptedFields)
	require.Equal(t, map[string]int64{"max_tokens": 262_144, "max_completion_tokens": 65_536}, contract.OutputLimitFieldMaximums)
	require.Equal(t, []string{"text"}, contract.ResponseFormatTypes)
	require.True(t, contract.AssistantReasoningHistoryModeled)
	require.Empty(t, contract.AssistantReasoningHistoryFields)
	require.Equal(t, []string{"encrypted_content"}, contract.OpaqueAssistantMessageFields)
	contract.OutputLimitFieldMaximums["max_completion_tokens"] = 1
	contract.OpaqueAssistantMessageFields[0] = "content"
	again, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.EqualValues(t, 65_536, again.OutputLimitFieldMaximums["max_completion_tokens"])
	require.Equal(t, []string{"encrypted_content"}, again.OpaqueAssistantMessageFields)

	reasoning, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeToggle, reasoning.Mode)
	require.Equal(t, "thinking", reasoning.ToggleControl.Field)
	require.Equal(t, []string{"enabled", "disabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, "enabled", reasoning.ToggleControl.ProviderDefault)
	require.Equal(t, ReasoningResponseThinkingSummarized, reasoning.ResponseThinkingTextMode)

	current, ok := MatchForSurface("doubao-seed-2-1-pro-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21ProStreamingProfileRemainsAppendOnlyAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-pro.api", "1.2.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao", profile.Provider)
	require.Equal(t, "cn-beijing", profile.Region)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, []string{"doubao-seed-2-1-pro-260628"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, "protocol.doubao-seed-2.1-pro-openai-chat.v2", profile.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-2.1-pro-toggle.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoning, CapabilityStreaming}, profile.Capabilities)
	require.Empty(t, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.Streaming)
	require.True(t, contract.StreamContract.RequestOptionsModeled)
	require.Equal(t, []string{"include_usage"}, contract.StreamContract.AllowedRequestOptions)
	require.True(t, contract.StreamContract.RequireIncludeUsage)
	require.True(t, contract.StreamContract.ResponseModeled)
	require.True(t, contract.StreamContract.RequireDone)
	require.True(t, contract.StreamContract.RequireTerminalUsage)
	require.Equal(t, []string{"content", "reasoning_content"}, contract.StreamContract.DeltaTextFields)
	require.Equal(t, []string{"encrypted_content"}, contract.StreamContract.DeltaOpaqueFields)
	require.Equal(t, []string{"stop", "length", "content_filter"}, contract.StreamContract.FinishReasons)
	require.True(t, contract.StreamContract.ReasoningOpaqueAnswerFlow)

	historical, ok := Lookup("doubao.seed-2.1-pro.api", "1.1.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.False(t, mustChatProtocolContract(t, historical.ProtocolProfile).Streaming)
	current, ok := MatchForSurface("doubao-seed-2-1-pro-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21ProToolsProfileRemainsAppendOnlyAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-pro.api", "1.3.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao", profile.Provider)
	require.Equal(t, "cn-beijing", profile.Region)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, []string{"doubao-seed-2-1-pro-260628"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, "protocol.doubao-seed-2.1-pro-openai-chat.v3", profile.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-2.1-pro-toggle-tools.v2", profile.ReasoningProfile)
	require.Equal(t, []string{
		CapabilityReasoning, CapabilityStreaming, CapabilityTools, CapabilityFunctionTools,
		CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
	}, profile.Capabilities)
	require.Empty(t, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.Streaming)
	require.True(t, protocol.FunctionTools)
	require.Equal(t, 128, protocol.MaxFunctionTools)
	require.False(t, protocol.FunctionToolStrictSupported)
	require.Equal(t, []string{"none", "auto", "required"}, protocol.FunctionToolChoices)
	require.True(t, protocol.NamedFunctionToolChoice)
	require.True(t, protocol.ParallelFunctionToolCalls)
	require.True(t, protocol.ParallelFunctionToolCallsDefaultModeled)
	require.True(t, protocol.ParallelFunctionToolCallsProviderDefault)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.True(t, protocol.ExactFunctionToolHistoryModeled)
	require.True(t, protocol.ExactFunctionToolResponseModeled)
	require.True(t, protocol.AssistantOpaqueHistoryModeled)
	require.Equal(t, []string{"encrypted_content"}, protocol.AssistantOpaqueHistoryFields)

	reasoning, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.True(t, reasoning.NamedToolChoiceWhileReasoning)
	require.True(t, reasoning.RequiredToolChoiceWhileReasoning)
	require.Equal(t, "encrypted_content", reasoning.AssistantToolHistoryReplayField)

	historical, ok := Lookup("doubao.seed-2.1-pro.api", "1.2.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.False(t, mustChatProtocolContract(t, historical.ProtocolProfile).FunctionTools)
	current, ok := MatchForSurface("doubao-seed-2-1-pro-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21ProStrictJSONSchemaProfileRemainsAppendOnlyAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-pro.api", "1.4.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao", profile.Provider)
	require.Equal(t, "cn-beijing", profile.Region)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, []string{"doubao-seed-2-1-pro-260628"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, "protocol.doubao-seed-2.1-pro-openai-chat.v4", profile.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-2.1-pro-toggle-tools.v2", profile.ReasoningProfile)
	require.Equal(t, []string{
		CapabilityReasoning, CapabilityStreaming, CapabilityTools, CapabilityFunctionTools,
		CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
		CapabilityStrictJSONSchema,
	}, profile.Capabilities)
	require.Empty(t, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))
	require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.Streaming)
	require.True(t, protocol.FunctionTools)
	require.Equal(t, []string{"text", "json_schema"}, protocol.ResponseFormatTypes)
	require.True(t, protocol.JSONSchemaStrictRequired)
	require.True(t, protocol.JSONSchemaObjectRequired)
	require.True(t, protocol.JSONSchemaRequiresNonStream)
	require.True(t, protocol.JSONSchemaDisallowsFunctionTools)
	require.True(t, protocol.JSONSchemaDescriptionStringWhenPresent)
	require.Equal(t, []string{"frequency_penalty", "presence_penalty"}, protocol.JSONSchemaIncompatibleRequestFields)

	historical, ok := Lookup("doubao.seed-2.1-pro.api", "1.3.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, []string{"text"}, mustChatProtocolContract(t, historical.ProtocolProfile).ResponseFormatTypes)
	current, ok := MatchForSurface("doubao-seed-2-1-pro-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21ProImplicitCacheReadProfileRemainsAppendOnlyAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-pro.api", "1.5.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "protocol.doubao-seed-2.1-pro-openai-chat.v4", profile.ProtocolProfile)
	require.Equal(t, "cache-usage.doubao-ark-chat.v1", profile.CacheUsageProfile)
	require.Contains(t, profile.Capabilities, CapabilityPromptCache)
	require.Contains(t, profile.Capabilities, CapabilityCacheReadUsage)
	require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))
	require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))

	cache, ok := ResolveCacheUsageContract(profile.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageModeAutomatic, cache.Mode)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, cache.WireSchema)
	require.True(t, cache.ReportsRead)
	require.True(t, cache.RequireReadCounter)
	require.NoError(t, validateCacheUsageContractShape(cache))

	historical, ok := Lookup("doubao.seed-2.1-pro.api", "1.4.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Empty(t, historical.CacheUsageProfile)
	require.NotContains(t, historical.Capabilities, CapabilityPromptCache)
	require.Empty(t, historical.AlwaysRequiredCapabilities)
	current, ok := MatchForSurface("doubao-seed-2-1-pro-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21ProInlineJPEGProfileIsAppendOnlyAndCurrent(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-pro.api", "1.6.0")
	require.True(t, ok)
	require.True(t, profile.Current)
	require.Equal(t, "protocol.doubao-seed-2.1-pro-openai-chat.v5", profile.ProtocolProfile)
	require.Equal(t, "cache-usage.doubao-ark-chat.v1", profile.CacheUsageProfile)
	require.Contains(t, profile.Capabilities, CapabilityVision)
	require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))
	require.NoError(t, ValidateNewAttachment(profile.ID, profile.Version))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.InlineImageDataURL)
	require.Equal(t, []string{"image/jpeg"}, protocol.InlineImageMIMETypes)
	require.True(t, protocol.ImageInputPolicy.Modeled)
	require.Equal(t, "jpeg", protocol.ImageInputPolicy.DecodedFormat)
	require.Equal(t, []string{"user"}, protocol.ImageInputPolicy.AllowedMessageRoles)
	require.Equal(t, 8, protocol.ImageInputPolicy.MaximumImages)
	require.Equal(t, int64(256*1024), protocol.ImageInputPolicy.MaximumDecodedBytes)
	require.Equal(t, int64(131_072), protocol.ImageInputPolicy.MaximumPixels)
	require.Equal(t, int64(4), protocol.ImageInputPolicy.AdmissionTokenUpperPerPixel)

	historical, ok := Lookup("doubao.seed-2.1-pro.api", "1.5.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.NotContains(t, historical.Capabilities, CapabilityVision)
	require.False(t, mustChatProtocolContract(t, historical.ProtocolProfile).ImageInputPolicy.Modeled)
	current, ok := MatchForSurface("doubao-seed-2-1-pro-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21TurboCandidateFreezesExactArkChatReasoningEffortCore(t *testing.T) {
	historical, ok := Lookup("doubao.seed-2.1-turbo.api", "1.0.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, []string{EndpointClassProviderHTTPS}, historical.EndpointClasses)
	require.Equal(t, 32_768, historical.DefaultOutputLimit)
	require.Equal(t, DefaultOutputPolicy, historical.DefaultOutputAuthority)
	require.Empty(t, historical.Capabilities)

	profile, ok := Lookup("doubao.seed-2.1-turbo.api", "1.1.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao", profile.Provider)
	require.Equal(t, "cn-beijing", profile.Region)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, []string{"doubao-seed-2-1-turbo-260628"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, 262_144, profile.ProviderContextWindow)
	require.Equal(t, 262_144, profile.ProviderMaxOutput)
	require.Equal(t, 4_096, profile.DefaultOutputLimit)
	require.Equal(t, DefaultOutputProvider, profile.DefaultOutputAuthority)
	require.Equal(t, []string{CapabilityReasoningEffort}, profile.Capabilities)
	require.Empty(t, profile.AlwaysRequiredCapabilities)
	require.Equal(t, "reasoning.doubao-seed-2.1-turbo-effort.v1", profile.ReasoningProfile)
	require.Empty(t, profile.CacheUsageProfile)
	require.Equal(t, "protocol.doubao-seed-2.1-turbo-openai-chat.v1", profile.ProtocolProfile)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, contract.StreamingModeled)
	require.False(t, contract.Streaming)
	require.Equal(t, "max_tokens", contract.OutputLimitField)
	require.Equal(t, []string{"max_tokens", "max_completion_tokens"}, contract.OutputLimitAcceptedFields)
	require.Equal(t, map[string]int64{"max_tokens": 262_144, "max_completion_tokens": 65_536}, contract.OutputLimitFieldMaximums)
	require.Equal(t, []string{"text"}, contract.ResponseFormatTypes)
	require.True(t, contract.AssistantReasoningHistoryModeled)
	require.Empty(t, contract.AssistantReasoningHistoryFields)
	require.Equal(t, []string{"encrypted_content"}, contract.OpaqueAssistantMessageFields)

	reasoning, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeToggle, reasoning.Mode)
	require.Empty(t, reasoning.ToggleControl.Field)
	require.Equal(t, "reasoning_effort", reasoning.EffortControl.Field)
	require.Equal(t, []string{"minimal", "low", "medium", "high"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, []string{"minimal"}, reasoning.EffortControl.DisabledValues)
	require.Equal(t, "high", reasoning.EffortControl.ProviderDefault)
	require.True(t, reasoning.EffortEnablesReasoning)
	require.Equal(t, ReasoningResponseThinkingSummarized, reasoning.ResponseThinkingTextMode)

	current, ok := MatchForSurface("doubao-seed-2-1-turbo-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "doubao.seed-2.1-turbo.api", current.ID)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21TurboStreamingProfileRemainsAppendOnlyAfterToolsPromotion(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-turbo.api", "1.2.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao", profile.Provider)
	require.Equal(t, "cn-beijing", profile.Region)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, []string{"doubao-seed-2-1-turbo-260628"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, "protocol.doubao-seed-2.1-turbo-openai-chat.v2", profile.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-2.1-turbo-effort.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoningEffort, CapabilityStreaming}, profile.Capabilities)
	require.Empty(t, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract := mustChatProtocolContract(t, profile.ProtocolProfile)
	require.True(t, contract.Streaming)
	require.True(t, contract.StreamContract.RequireIncludeUsage)
	require.True(t, contract.StreamContract.RequireDone)
	require.True(t, contract.StreamContract.RequireTerminalUsage)
	require.Equal(t, []string{"content", "reasoning_content"}, contract.StreamContract.DeltaTextFields)
	require.Equal(t, []string{"encrypted_content"}, contract.StreamContract.DeltaOpaqueFields)
	require.True(t, contract.StreamContract.ReasoningOpaqueAnswerFlow)

	historical, ok := Lookup("doubao.seed-2.1-turbo.api", "1.1.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.False(t, mustChatProtocolContract(t, historical.ProtocolProfile).Streaming)
	current, ok := MatchForSurface("doubao-seed-2-1-turbo-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21TurboToolsProfileRemainsAppendOnlyAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-turbo.api", "1.3.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao", profile.Provider)
	require.Equal(t, "cn-beijing", profile.Region)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, []string{"doubao-seed-2-1-turbo-260628"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, "protocol.doubao-seed-2.1-turbo-openai-chat.v3", profile.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-2.1-turbo-effort-tools.v2", profile.ReasoningProfile)
	require.Equal(t, []string{
		CapabilityReasoningEffort, CapabilityStreaming, CapabilityTools, CapabilityFunctionTools,
		CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
	}, profile.Capabilities)
	require.Empty(t, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))
	require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.Streaming)
	require.True(t, protocol.FunctionTools)
	require.Equal(t, 128, protocol.MaxFunctionTools)
	require.False(t, protocol.FunctionToolStrictSupported)
	require.Equal(t, []string{"none", "auto", "required"}, protocol.FunctionToolChoices)
	require.True(t, protocol.NamedFunctionToolChoice)
	require.True(t, protocol.ParallelFunctionToolCalls)
	require.True(t, protocol.ParallelFunctionToolCallsDefaultModeled)
	require.True(t, protocol.ParallelFunctionToolCallsProviderDefault)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.True(t, protocol.ExactFunctionToolHistoryModeled)
	require.True(t, protocol.ExactFunctionToolResponseModeled)
	require.True(t, protocol.AssistantOpaqueHistoryModeled)
	require.Equal(t, []string{"encrypted_content"}, protocol.AssistantOpaqueHistoryFields)

	reasoning, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, "reasoning_effort", reasoning.EffortControl.Field)
	require.Equal(t, []string{"minimal", "low", "medium", "high"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, "high", reasoning.EffortControl.ProviderDefault)
	require.Equal(t, []string{"minimal"}, reasoning.EffortControl.DisabledValues)
	require.True(t, reasoning.NamedToolChoiceWhileReasoning)
	require.True(t, reasoning.RequiredToolChoiceWhileReasoning)
	require.Equal(t, "encrypted_content", reasoning.AssistantToolHistoryReplayField)

	historical, ok := Lookup("doubao.seed-2.1-turbo.api", "1.2.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.False(t, mustChatProtocolContract(t, historical.ProtocolProfile).FunctionTools)
	current, ok := MatchForSurface("doubao-seed-2-1-turbo-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21TurboStrictJSONSchemaProfileRemainsAppendOnlyAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-turbo.api", "1.4.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao", profile.Provider)
	require.Equal(t, "cn-beijing", profile.Region)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, []string{"doubao-seed-2-1-turbo-260628"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, "protocol.doubao-seed-2.1-turbo-openai-chat.v4", profile.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-2.1-turbo-effort-tools.v2", profile.ReasoningProfile)
	require.Equal(t, []string{
		CapabilityReasoningEffort, CapabilityStreaming, CapabilityTools, CapabilityFunctionTools,
		CapabilityToolChoiceAuto, CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
		CapabilityStrictJSONSchema,
	}, profile.Capabilities)
	require.Empty(t, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))
	require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.Streaming)
	require.True(t, protocol.FunctionTools)
	require.Equal(t, []string{"text", "json_schema"}, protocol.ResponseFormatTypes)
	require.True(t, protocol.JSONSchemaStrictRequired)
	require.True(t, protocol.JSONSchemaObjectRequired)
	require.True(t, protocol.JSONSchemaRequiresNonStream)
	require.True(t, protocol.JSONSchemaDisallowsFunctionTools)
	require.True(t, protocol.JSONSchemaDescriptionStringWhenPresent)
	require.Equal(t, []string{"frequency_penalty", "presence_penalty"}, protocol.JSONSchemaIncompatibleRequestFields)

	historical, ok := Lookup("doubao.seed-2.1-turbo.api", "1.3.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, []string{"text"}, mustChatProtocolContract(t, historical.ProtocolProfile).ResponseFormatTypes)
	current, ok := MatchForSurface("doubao-seed-2-1-turbo-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21TurboImplicitCacheReadProfileRemainsAppendOnlyAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-turbo.api", "1.5.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "protocol.doubao-seed-2.1-turbo-openai-chat.v4", profile.ProtocolProfile)
	require.Equal(t, "cache-usage.doubao-ark-chat.v1", profile.CacheUsageProfile)
	require.Contains(t, profile.Capabilities, CapabilityPromptCache)
	require.Contains(t, profile.Capabilities, CapabilityCacheReadUsage)
	require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))
	require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))

	cache, ok := ResolveCacheUsageContract(profile.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageModeAutomatic, cache.Mode)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, cache.WireSchema)
	require.True(t, cache.ReportsRead)
	require.True(t, cache.RequireReadCounter)
	require.NoError(t, validateCacheUsageContractShape(cache))

	historical, ok := Lookup("doubao.seed-2.1-turbo.api", "1.4.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Empty(t, historical.CacheUsageProfile)
	require.NotContains(t, historical.Capabilities, CapabilityPromptCache)
	require.Empty(t, historical.AlwaysRequiredCapabilities)
	current, ok := MatchForSurface("doubao-seed-2-1-turbo-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeed21TurboInlineJPEGProfileIsAppendOnlyAndCurrent(t *testing.T) {
	profile, ok := Lookup("doubao.seed-2.1-turbo.api", "1.6.0")
	require.True(t, ok)
	require.True(t, profile.Current)
	require.Equal(t, "protocol.doubao-seed-2.1-turbo-openai-chat.v5", profile.ProtocolProfile)
	require.Equal(t, "cache-usage.doubao-ark-chat.v1", profile.CacheUsageProfile)
	require.Contains(t, profile.Capabilities, CapabilityVision)
	require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))
	require.NoError(t, ValidateNewAttachment(profile.ID, profile.Version))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.InlineImageDataURL)
	require.Equal(t, []string{"image/jpeg"}, protocol.InlineImageMIMETypes)
	require.True(t, protocol.ImageInputPolicy.Modeled)
	require.Equal(t, "jpeg", protocol.ImageInputPolicy.DecodedFormat)
	require.True(t, protocol.ImageInputPolicy.RequireURLObject)
	require.Equal(t, []string{"user"}, protocol.ImageInputPolicy.AllowedMessageRoles)
	require.Equal(t, 8, protocol.ImageInputPolicy.MaximumImages)
	require.Equal(t, int64(256*1024), protocol.ImageInputPolicy.MaximumDecodedBytes)
	require.Equal(t, int64(131_072), protocol.ImageInputPolicy.MaximumPixels)
	require.Equal(t, int64(4), protocol.ImageInputPolicy.AdmissionTokenUpperPerPixel)
	require.Equal(t, "chat-inline-jpeg-rgba-pixel-upper.v1", protocol.ImageInputPolicy.AdmissionEstimateVersion)

	historical, ok := Lookup("doubao.seed-2.1-turbo.api", "1.5.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.NotContains(t, historical.Capabilities, CapabilityVision)
	require.False(t, mustChatProtocolContract(t, historical.ProtocolProfile).ImageInputPolicy.Modeled)
	current, ok := MatchForSurface("doubao-seed-2-1-turbo-260628", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.6.0", current.Version)
}

func TestDoubaoSeedEvolvingIdentityCandidateKeepsFloatingAliasOutOfHistoricalProfile(t *testing.T) {
	historical, ok := Lookup("doubao.seed-evolving.api", "1.0.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Empty(t, historical.UpstreamModelFamily)
	require.Empty(t, historical.UpstreamVersionPolicy)
	require.Equal(t, []string{EndpointClassProviderHTTPS}, historical.EndpointClasses)
	require.Equal(t, 32_768, historical.DefaultOutputLimit)
	require.Equal(t, DefaultOutputPolicy, historical.DefaultOutputAuthority)

	profile, ok := Lookup("doubao.seed-evolving.api", "1.1.0")
	require.True(t, ok)
	require.False(t, profile.Current, "identity-only candidate must not become attachable")
	require.Equal(t, "doubao", profile.Provider)
	require.Equal(t, "cn-beijing", profile.Region)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, []string{"doubao-seed-evolving"}, profile.UpstreamModelIDs)
	require.Equal(t, "doubao-seed-evolving", profile.UpstreamModelFamily)
	require.Equal(t, UpstreamVersionFloating, profile.UpstreamVersionPolicy)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, 1_048_576, profile.ProviderContextWindow)
	require.Equal(t, 262_144, profile.ProviderMaxOutput)
	require.Equal(t, 4_096, profile.DefaultOutputLimit)
	require.Equal(t, DefaultOutputProvider, profile.DefaultOutputAuthority)
	require.Empty(t, profile.Capabilities)
	require.Equal(t, "documented", profile.CatalogEvidenceState)
	require.NoError(t, ValidateExecutableComponents(profile))

	drifted := profile
	drifted.UpstreamModelFamily = "doubao-seed-2.1-pro"
	require.Error(t, ValidateExecutableComponents(drifted))
	drifted = profile
	drifted.UpstreamVersionPolicy = UpstreamVersionPinned
	require.Error(t, ValidateExecutableComponents(drifted))

	current, ok := MatchForSurface("doubao-seed-evolving", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.8.0", current.Version)
}

func TestDoubaoSeedEvolvingChatCoreIsExactAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-evolving.api", "1.2.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao-seed-evolving", profile.UpstreamModelFamily)
	require.Equal(t, UpstreamVersionFloating, profile.UpstreamVersionPolicy)
	require.Equal(t, []string{"doubao-seed-evolving"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, DefaultOutputProvider, profile.DefaultOutputAuthority)
	require.Equal(t, 4_096, profile.DefaultOutputLimit)
	require.Empty(t, profile.ReasoningProfile)
	require.Empty(t, profile.CacheUsageProfile)
	require.Empty(t, profile.Capabilities)
	require.Equal(t, "locally_conformant", profile.CatalogEvidenceState)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v1", profile.ProtocolProfile)
	require.NoError(t, ValidateExecutableComponents(profile))

	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, profile.ProtocolProfile, contract.ProfileID)
	require.True(t, contract.StreamingModeled)
	require.False(t, contract.Streaming)
	require.Equal(t, []string{"max_tokens", "max_completion_tokens"}, contract.OutputLimitAcceptedFields)
	require.Equal(t, map[string]int64{"max_tokens": 262_144, "max_completion_tokens": 65_536}, contract.OutputLimitFieldMaximums)
	require.False(t, contract.FunctionTools)
	require.Equal(t, []string{"text"}, contract.ResponseFormatTypes)
	require.True(t, contract.AssistantReasoningHistoryModeled)
	require.Empty(t, contract.AssistantReasoningHistoryFields)
	require.True(t, contract.AssistantReasoningResponseModeled)
	require.Empty(t, contract.AssistantReasoningResponseFields)

	current, ok := MatchForSurface("doubao-seed-evolving", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, profile.ID, current.ID)
	require.Equal(t, "1.8.0", current.Version)
}

func TestDoubaoSeedEvolvingReasoningProfileIsExactAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-evolving.api", "1.3.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao-seed-evolving", profile.UpstreamModelFamily)
	require.Equal(t, UpstreamVersionFloating, profile.UpstreamVersionPolicy)
	require.Equal(t, []string{"doubao-seed-evolving"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, DefaultOutputProvider, profile.DefaultOutputAuthority)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v2", profile.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-evolving-toggle-effort.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityReasoning, CapabilityReasoningEffort}, profile.Capabilities)
	require.Equal(t, "locally_conformant", profile.CatalogEvidenceState)
	require.NoError(t, ValidateExecutableComponents(profile))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.AssistantReasoningHistoryModeled)
	require.Equal(t, []string{"reasoning_content"}, protocol.AssistantReasoningHistoryFields)
	require.True(t, protocol.AssistantReasoningResponseModeled)
	require.Equal(t, []string{"reasoning_content"}, protocol.AssistantReasoningResponseFields)
	require.Equal(t, []string{"encrypted_content"}, protocol.OpaqueAssistantMessageFields)
	require.False(t, protocol.Streaming)
	require.False(t, protocol.FunctionTools)
	require.Equal(t, []string{"text"}, protocol.ResponseFormatTypes)

	reasoning, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.Equal(t, ReasoningModeToggle, reasoning.Mode)
	require.Equal(t, "thinking", reasoning.ToggleControl.Field)
	require.Equal(t, []string{"enabled", "disabled"}, reasoning.ToggleControl.AllowedValues)
	require.Equal(t, "enabled", reasoning.ToggleControl.ProviderDefault)
	require.Equal(t, "reasoning_effort", reasoning.EffortControl.Field)
	require.Equal(t, []string{"low", "medium", "high"}, reasoning.EffortControl.AllowedValues)
	require.Equal(t, []string{"none", "minimal"}, reasoning.EffortControl.DisabledValues)
	require.Equal(t, map[string]string{"xhigh": "high", "max": "high"}, reasoning.EffortControl.CompatibilityValues)
	require.Equal(t, "high", reasoning.EffortControl.ProviderDefault)
	require.True(t, reasoning.ToggleAndEffortMutuallyExclusive)
	require.Equal(t, 1_800, reasoning.ActiveNonStreamTimeoutSeconds)
	require.Equal(t, ReasoningResponseThinkingSummarized, reasoning.ResponseThinkingTextMode)
	require.Equal(t, ReasoningDefaultProvider, reasoning.DefaultAuthority)

	current, ok := MatchForSurface("doubao-seed-evolving", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "doubao.seed-evolving.api", current.ID)
	require.Equal(t, "1.8.0", current.Version)
}

func TestDoubaoSeedEvolvingStreamingProfileIsExactAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-evolving.api", "1.4.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao-seed-evolving", profile.UpstreamModelFamily)
	require.Equal(t, UpstreamVersionFloating, profile.UpstreamVersionPolicy)
	require.Equal(t, []string{"doubao-seed-evolving"}, profile.UpstreamModelIDs)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, DefaultOutputProvider, profile.DefaultOutputAuthority)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v3", profile.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-evolving-toggle-effort.v1", profile.ReasoningProfile)
	require.Equal(t, []string{CapabilityStreaming, CapabilityReasoning, CapabilityReasoningEffort}, profile.Capabilities)
	require.Equal(t, "locally_conformant", profile.CatalogEvidenceState)
	require.NoError(t, ValidateExecutableComponents(profile))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.Streaming)
	require.True(t, protocol.StreamContract.RequestOptionsModeled)
	require.Equal(t, []string{"include_usage"}, protocol.StreamContract.AllowedRequestOptions)
	require.True(t, protocol.StreamContract.RequireIncludeUsage)
	require.True(t, protocol.StreamContract.ResponseModeled)
	require.True(t, protocol.StreamContract.RequireDone)
	require.True(t, protocol.StreamContract.RequireTerminalUsage)
	require.Equal(t, []string{"content", "reasoning_content"}, protocol.StreamContract.DeltaTextFields)
	require.Equal(t, []string{"encrypted_content"}, protocol.StreamContract.DeltaOpaqueFields)
	require.Equal(t, []string{"stop", "length", "content_filter"}, protocol.StreamContract.FinishReasons)
	require.True(t, protocol.StreamContract.ReasoningOpaqueAnswerFlow)

	current, ok := MatchForSurface("doubao-seed-evolving", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.8.0", current.Version)
}

func TestDoubaoSeedEvolvingToolsProfileIsAppendOnlyAndHistorical(t *testing.T) {
	profile, ok := Lookup("doubao.seed-evolving.api", "1.5.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao-seed-evolving", profile.UpstreamModelFamily)
	require.Equal(t, UpstreamVersionFloating, profile.UpstreamVersionPolicy)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v4", profile.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-evolving-toggle-effort.v2", profile.ReasoningProfile)
	require.ElementsMatch(t, []string{
		CapabilityStreaming, CapabilityReasoning, CapabilityReasoningEffort,
		CapabilityTools, CapabilityFunctionTools, CapabilityToolChoiceAuto,
		CapabilityToolChoiceNamed, CapabilityToolChoiceReq, CapabilityParallelTools,
	}, profile.Capabilities)
	require.Empty(t, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.FunctionToolInvocationCapabilitiesModeled)
	require.True(t, protocol.FunctionToolsRequireNonStream)
	require.True(t, protocol.ExactFunctionToolHistoryModeled)
	require.True(t, protocol.ExactFunctionToolResponseModeled)
	require.Equal(t, 128, protocol.MaxFunctionTools)
	require.False(t, protocol.FunctionToolDescriptionRequired)
	require.False(t, protocol.FunctionToolParametersRequired)
	require.True(t, protocol.FunctionToolParametersObjectWhenPresent)
	require.True(t, protocol.FunctionToolStrictSupported)
	require.Equal(t, `^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`, protocol.FunctionToolNamePattern)
	require.Equal(t, []string{"none", "auto", "required"}, protocol.FunctionToolChoices)
	require.Equal(t, []string{"auto", "required", "named"}, protocol.FunctionToolChoicesRequiringTools)
	require.True(t, protocol.NamedFunctionToolChoice)
	require.True(t, protocol.ParallelFunctionToolCalls)
	require.True(t, protocol.ParallelFunctionToolCallsDefaultModeled)
	require.True(t, protocol.ParallelFunctionToolCallsProviderDefault)
	require.True(t, protocol.AssistantOpaqueHistoryModeled)
	require.Equal(t, []string{"encrypted_content"}, protocol.AssistantOpaqueHistoryFields)

	reasoning, ok := ResolveReasoningContract(profile.ReasoningProfile)
	require.True(t, ok)
	require.True(t, reasoning.NamedToolChoiceWhileReasoning)
	require.True(t, reasoning.RequiredToolChoiceWhileReasoning)
	require.Equal(t, "encrypted_content", reasoning.AssistantToolHistoryReplayField)

	current, ok := MatchForSurface("doubao-seed-evolving", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.8.0", current.Version)

	historical, ok := Lookup("doubao.seed-evolving.api", "1.4.0")
	require.True(t, ok)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v3", historical.ProtocolProfile)
	require.Equal(t, "reasoning.doubao-seed-evolving-toggle-effort.v1", historical.ReasoningProfile)
	require.NotContains(t, historical.Capabilities, CapabilityTools)
}

func TestDoubaoSeedEvolvingStrictJSONSchemaProfileRemainsAppendOnly(t *testing.T) {
	profile, ok := Lookup("doubao.seed-evolving.api", "1.6.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao-seed-evolving", profile.UpstreamModelFamily)
	require.Equal(t, UpstreamVersionFloating, profile.UpstreamVersionPolicy)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v5", profile.ProtocolProfile)
	require.Contains(t, profile.Capabilities, CapabilityStrictJSONSchema)
	require.NoError(t, ValidateExecutableComponents(profile))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"text", "json_schema"}, protocol.ResponseFormatTypes)
	require.True(t, protocol.JSONSchemaStrictRequired)
	require.True(t, protocol.JSONSchemaObjectRequired)
	require.True(t, protocol.JSONSchemaRequiresNonStream)
	require.True(t, protocol.JSONSchemaDisallowsFunctionTools)
	require.True(t, protocol.JSONSchemaDescriptionStringWhenPresent)
	require.Equal(t, []string{"frequency_penalty", "presence_penalty"}, protocol.JSONSchemaIncompatibleRequestFields)

	historical, ok := Lookup("doubao.seed-evolving.api", "1.5.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Equal(t, []string{"text"}, mustChatProtocolContract(t, historical.ProtocolProfile).ResponseFormatTypes)
	current, ok := MatchForSurface("doubao-seed-evolving", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.8.0", current.Version)
}

func TestDoubaoSeedEvolvingInlineJPEGProfileRemainsAppendOnly(t *testing.T) {
	profile, ok := Lookup("doubao.seed-evolving.api", "1.7.0")
	require.True(t, ok)
	require.False(t, profile.Current)
	require.Equal(t, "doubao-seed-evolving", profile.UpstreamModelFamily)
	require.Equal(t, UpstreamVersionFloating, profile.UpstreamVersionPolicy)
	require.Equal(t, []string{EndpointClassDoubaoArkBeijingOpenAIV3}, profile.EndpointClasses)
	require.Equal(t, APISurfaceOpenAIChat, profile.APISurface)
	require.Equal(t, []string{OperationOpenAIChat}, profile.Operations)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v6", profile.ProtocolProfile)
	require.Contains(t, profile.Capabilities, CapabilityVision)
	require.NoError(t, ValidateExecutableComponents(profile))

	protocol, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.True(t, protocol.InlineImageDataURL)
	require.False(t, protocol.RemoteImageHTTPSURL)
	require.Equal(t, []string{"image/jpeg"}, protocol.InlineImageMIMETypes)
	require.True(t, protocol.ImageInputPolicy.Modeled)
	require.Equal(t, "jpeg", protocol.ImageInputPolicy.DecodedFormat)
	require.True(t, protocol.ImageInputPolicy.RequireURLObject)
	require.Equal(t, []string{"user"}, protocol.ImageInputPolicy.AllowedMessageRoles)
	require.Equal(t, 8, protocol.ImageInputPolicy.MaximumImages)
	require.Equal(t, int64(256*1024), protocol.ImageInputPolicy.MaximumDecodedBytes)
	require.Equal(t, int64(131_072), protocol.ImageInputPolicy.MaximumPixels)
	require.Equal(t, int64(4), protocol.ImageInputPolicy.AdmissionTokenUpperPerPixel)
	require.Equal(t, "chat-inline-jpeg-rgba-pixel-upper.v1", protocol.ImageInputPolicy.AdmissionEstimateVersion)
	protocol.ImageInputPolicy.AllowedMessageRoles[0] = "system"
	protocol.ImageInputPolicy.IncompatibleRequestFields[0] = "mutated"
	resolvedAgain, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, ok)
	require.Equal(t, []string{"user"}, resolvedAgain.ImageInputPolicy.AllowedMessageRoles)
	require.NotContains(t, resolvedAgain.ImageInputPolicy.IncompatibleRequestFields, "mutated")

	historical, ok := Lookup("doubao.seed-evolving.api", "1.6.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.NotContains(t, historical.Capabilities, CapabilityVision)
	require.False(t, mustChatProtocolContract(t, historical.ProtocolProfile).ImageInputPolicy.Modeled)
	current, ok := MatchForSurface("doubao-seed-evolving", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, "1.8.0", current.Version)
}

func TestDoubaoSeedEvolvingImplicitCacheReadUsageIsCurrentAndMandatory(t *testing.T) {
	profile, ok := Lookup("doubao.seed-evolving.api", "1.8.0")
	require.True(t, ok)
	require.True(t, profile.Current)
	require.Equal(t, "protocol.doubao-seed-evolving-openai-chat.v6", profile.ProtocolProfile)
	require.Equal(t, "cache-usage.doubao-ark-chat.v1", profile.CacheUsageProfile)
	require.Contains(t, profile.Capabilities, CapabilityPromptCache)
	require.Contains(t, profile.Capabilities, CapabilityCacheReadUsage)
	require.Equal(t, []string{CapabilityPromptCache, CapabilityCacheReadUsage}, profile.AlwaysRequiredCapabilities)
	require.NoError(t, ValidateExecutableComponents(profile))

	cache, ok := ResolveCacheUsageContract(profile.CacheUsageProfile)
	require.True(t, ok)
	require.Equal(t, CacheUsageModeAutomatic, cache.Mode)
	require.Equal(t, CacheUsageWireOpenAICachedTokensV1, cache.WireSchema)
	require.True(t, cache.ReportsRead)
	require.True(t, cache.RequireReadCounter)
	require.NoError(t, validateCacheUsageContractShape(cache))

	historical, ok := Lookup("doubao.seed-evolving.api", "1.7.0")
	require.True(t, ok)
	require.False(t, historical.Current)
	require.Empty(t, historical.CacheUsageProfile)
	require.NotContains(t, historical.Capabilities, CapabilityPromptCache)
	require.Empty(t, historical.AlwaysRequiredCapabilities)

	current, ok := MatchForSurface("doubao-seed-evolving", APISurfaceOpenAIChat)
	require.True(t, ok)
	require.Equal(t, profile.ID, current.ID)
	require.Equal(t, profile.Version, current.Version)
}

func TestMandatoryCacheReadCounterRequiresReadReportingAndKnownWire(t *testing.T) {
	tests := []CacheUsageContract{
		{RequireReadCounter: true, WireSchema: CacheUsageWireOpenAICachedTokensV1},
		{RequireReadCounter: true, ReportsRead: true, WireSchema: CacheUsageWireDeepSeekHitMissV1},
		{RequireReadCounter: true, ReportsRead: true, WireSchema: "unknown.cache.wire.v1"},
	}
	for _, contract := range tests {
		require.Error(t, validateCacheUsageContractShape(contract))
	}
	require.NoError(t, validateCacheUsageContractShape(CacheUsageContract{}), "zero-value historical behavior must remain unchanged")
}

func mustChatProtocolContract(t *testing.T, profileID string) ChatProtocolContract {
	t.Helper()
	contract, ok := ResolveChatProtocolContract(profileID)
	require.True(t, ok)
	return contract
}

func TestTypedChatEvidenceDerivationDoesNotReclassifyExistingCatalogAddresses(t *testing.T) {
	for _, profile := range All() {
		address := profile.ID + "@" + profile.Version
		if address == "doubao.seed-evolving.api@1.2.0" {
			require.Equal(t, "locally_conformant", profile.CatalogEvidenceState)
			continue
		}
		expected := "documented"
		if profile.ID == UnverifiedProfileID {
			expected = "unverified"
		} else if len(profile.Capabilities) > 0 || profile.ProtocolProfileVersion != "" {
			expected = "locally_conformant"
		}
		require.Equal(t, expected, profile.CatalogEvidenceState, address)
	}

	historical, ok := Lookup("doubao.seed-evolving.api", "1.0.0")
	require.True(t, ok)
	require.Equal(t, "documented", historical.CatalogEvidenceState)
	identity, ok := Lookup("doubao.seed-evolving.api", "1.1.0")
	require.True(t, ok)
	require.Equal(t, "documented", identity.CatalogEvidenceState)
}
