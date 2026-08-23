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
	// CompatibilityValues are provider-documented wire aliases whose effective
	// value is one of AllowedValues. They remain visible in evidence instead of
	// being silently rewritten by UniRouter.
	CompatibilityValues map[string]string
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
	ThinkingKeepValues                  []string
	ThinkingKeepProviderDefault         string
	ThinkingKeepRequiresEnabled         bool
	AllowedToolChoices                  []string
	FixedRequestNumbers                 map[string]string
	FixedRequestNumbersWhenDisabled     map[string]string
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
	"reasoning.kimi-k2.6-thinking-toggle.v2": {
		ProfileID: "reasoning.kimi-k2.6-thinking-toggle.v2",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		DefaultAuthority:              ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning: false, RequiredToolChoiceWhileReasoning: false,
		PreserveAssistantReasoningForTools: true,
		ThinkingKeepValues:                 []string{"all"},
		ThinkingKeepProviderDefault:        "null",
		ThinkingKeepRequiresEnabled:        true,
		AllowedToolChoices:                 []string{"none", "auto"},
		FixedRequestNumbers: map[string]string{
			"temperature": "1", "top_p": "0.95", "n": "1",
			"presence_penalty": "0", "frequency_penalty": "0",
		},
		FixedRequestNumbersWhenDisabled: map[string]string{"temperature": "0.6"},
	},
	"reasoning.kimi-k2.7-thinking-required.v1": {
		ProfileID: "reasoning.kimi-k2.7-thinking-required.v1",
		Mode:      ReasoningModeRequired, DefaultAuthority: ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning: false, RequiredToolChoiceWhileReasoning: false,
		PreserveAssistantReasoningForTools: true,
	},
	"reasoning.kimi-k2.7-thinking-required.v2": {
		ProfileID: "reasoning.kimi-k2.7-thinking-required.v2",
		Mode:      ReasoningModeRequired,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled"}, ProviderDefault: "enabled",
		},
		DefaultAuthority:              ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning: false, RequiredToolChoiceWhileReasoning: false,
		PreserveAssistantReasoningForTools: true,
		ThinkingKeepValues:                 []string{"all"},
		ThinkingKeepProviderDefault:        "all",
		ThinkingKeepRequiresEnabled:        true,
		AllowedToolChoices:                 []string{"none", "auto"},
		FixedRequestNumbers: map[string]string{
			"temperature": "1", "top_p": "0.95", "n": "1",
			"presence_penalty": "0", "frequency_penalty": "0",
		},
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
	"reasoning.kimi-k3-effort.v2": {
		ProfileID: "reasoning.kimi-k3-effort.v2",
		Mode:      ReasoningModeAlwaysOn,
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"low", "high", "max"}, ProviderDefault: "max",
		},
		DefaultAuthority:              ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning: false, RequiredToolChoiceWhileReasoning: true,
		PreserveAssistantReasoningForTools: true,
		AllowedToolChoices:                 []string{"none", "auto", "required"},
		FixedRequestNumbers: map[string]string{
			"temperature": "1", "top_p": "0.95", "n": "1",
			"presence_penalty": "0", "frequency_penalty": "0",
		},
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
	"reasoning.deepseek-thinking-toggle.v2": {
		ProfileID: "reasoning.deepseek-thinking-toggle.v2",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"high", "max"}, ProviderDefault: "high",
			CompatibilityValues: map[string]string{"low": "high", "medium": "high", "xhigh": "max"},
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
	contract.ToggleControl.CompatibilityValues = cloneStringMap(contract.ToggleControl.CompatibilityValues)
	contract.EffortControl.AllowedValues = slices.Clone(contract.EffortControl.AllowedValues)
	contract.EffortControl.CompatibilityValues = cloneStringMap(contract.EffortControl.CompatibilityValues)
	contract.ForbiddenRequestFieldsWhileActive = slices.Clone(contract.ForbiddenRequestFieldsWhileActive)
	contract.UnsupportedRequestFieldsWhileActive = slices.Clone(contract.UnsupportedRequestFieldsWhileActive)
	contract.ThinkingKeepValues = slices.Clone(contract.ThinkingKeepValues)
	contract.AllowedToolChoices = slices.Clone(contract.AllowedToolChoices)
	contract.FixedRequestNumbers = cloneStringMap(contract.FixedRequestNumbers)
	contract.FixedRequestNumbersWhenDisabled = cloneStringMap(contract.FixedRequestNumbersWhenDisabled)
	return contract, true
}

func ResolveCacheUsageContract(profileID string) (CacheUsageContract, bool) {
	contract, ok := cacheUsageContracts[profileID]
	return contract, ok
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
