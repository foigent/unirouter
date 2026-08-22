package modelprofile

import "slices"

const (
	ReasoningModeToggle   = "toggle"
	ReasoningModeRequired = "required"
	ReasoningModeAlwaysOn = "always_on"

	ReasoningDefaultProvider = "provider"

	CacheUsageModeAutomatic = "automatic"

	CacheUsageWireMoonshotCachedTokensV1 = "moonshot.cached_tokens.v1"
	CacheUsageWireDeepSeekHitMissV1      = "deepseek.prompt-cache-hit-miss.v1"
)

// ReasoningControlContract describes one exact request control owned by a
// reasoning profile. Toggle and effort remain separate because providers such
// as DeepSeek expose both controls while Kimi profiles expose only one.
type ReasoningControlContract struct {
	Field           string
	AllowedValues   []string
	ProviderDefault string
}

// ReasoningContract is the immutable request-side behavior owned by one
// reasoning profile. It deliberately describes component behavior rather than
// provider names, model aliases, or handler branches.
type ReasoningContract struct {
	ProfileID                           string
	Mode                                string
	ToggleControl                       ReasoningControlContract
	EffortControl                       ReasoningControlContract
	DefaultAuthority                    string
	NamedToolChoiceWhileReasoning       bool
	RequiredToolChoiceWhileReasoning    bool
	PreserveAssistantReasoningForTools  bool
	ForbiddenRequestFieldsWhileActive   []string
	UnsupportedRequestFieldsWhileActive []string
}

// CacheUsageContract binds one cache-usage profile to the exact provider wire
// evidence that may become billing truth. Unknown cache-like fields remain
// untrusted and receive no discount.
type CacheUsageContract struct {
	ProfileID  string
	Mode       string
	WireSchema string
}

var reasoningContracts = map[string]ReasoningContract{
	"reasoning.kimi-k2.6-thinking-toggle.v1": {
		ProfileID: "reasoning.kimi-k2.6-thinking-toggle.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		DefaultAuthority:              ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning: false, RequiredToolChoiceWhileReasoning: false,
		PreserveAssistantReasoningForTools: true,
	},
	"reasoning.kimi-k2.7-thinking-required.v1": {
		ProfileID: "reasoning.kimi-k2.7-thinking-required.v1",
		Mode:      ReasoningModeRequired, DefaultAuthority: ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning: false, RequiredToolChoiceWhileReasoning: false,
		PreserveAssistantReasoningForTools: true,
	},
	"reasoning.kimi-k3-effort.v1": {
		ProfileID: "reasoning.kimi-k3-effort.v1",
		Mode:      ReasoningModeAlwaysOn,
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"low", "high", "max"}, ProviderDefault: "max",
		},
		DefaultAuthority:              ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning: false, RequiredToolChoiceWhileReasoning: true,
		PreserveAssistantReasoningForTools: true,
	},
	"reasoning.deepseek-thinking-toggle.v1": {
		ProfileID: "reasoning.deepseek-thinking-toggle.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"high", "max"}, ProviderDefault: "high",
		},
		DefaultAuthority:                   ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:      true,
		RequiredToolChoiceWhileReasoning:   true,
		PreserveAssistantReasoningForTools: true,
		ForbiddenRequestFieldsWhileActive: []string{
			"temperature", "top_p", "presence_penalty", "frequency_penalty",
		},
		UnsupportedRequestFieldsWhileActive: []string{"tool_choice"},
	},
}

var cacheUsageContracts = map[string]CacheUsageContract{
	"cache-usage.kimi-platform.v1": {
		ProfileID: "cache-usage.kimi-platform.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireMoonshotCachedTokensV1,
	},
	"cache-usage.deepseek-v4.v1": {
		ProfileID: "cache-usage.deepseek-v4.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireDeepSeekHitMissV1,
	},
}

func ResolveReasoningContract(profileID string) (ReasoningContract, bool) {
	contract, ok := reasoningContracts[profileID]
	if !ok {
		return ReasoningContract{}, false
	}
	contract.ToggleControl.AllowedValues = slices.Clone(contract.ToggleControl.AllowedValues)
	contract.EffortControl.AllowedValues = slices.Clone(contract.EffortControl.AllowedValues)
	contract.ForbiddenRequestFieldsWhileActive = slices.Clone(contract.ForbiddenRequestFieldsWhileActive)
	contract.UnsupportedRequestFieldsWhileActive = slices.Clone(contract.UnsupportedRequestFieldsWhileActive)
	return contract, true
}

func ResolveCacheUsageContract(profileID string) (CacheUsageContract, bool) {
	contract, ok := cacheUsageContracts[profileID]
	return contract, ok
}
