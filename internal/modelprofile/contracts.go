package modelprofile

import "slices"

const (
	ReasoningModeToggle   = "toggle"
	ReasoningModeRequired = "required"
	ReasoningModeAlwaysOn = "always_on"

	ReasoningDefaultProvider = "provider"

	CacheUsageModeAutomatic = "automatic"

	CacheUsageWireMoonshotCachedTokensV1 = "moonshot.cached_tokens.v1"
)

// ReasoningContract is the immutable request-side behavior owned by one
// reasoning profile. It deliberately describes component behavior rather than
// provider names, model aliases, or handler branches.
type ReasoningContract struct {
	ProfileID                          string
	Mode                               string
	RequestField                       string
	AllowedValues                      []string
	ProviderDefault                    string
	DefaultAuthority                   string
	NamedToolChoiceWhileReasoning      bool
	RequiredToolChoiceWhileReasoning   bool
	PreserveAssistantReasoningForTools bool
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
		Mode:      ReasoningModeToggle, RequestField: "thinking",
		AllowedValues:   []string{"enabled", "disabled"},
		ProviderDefault: "enabled", DefaultAuthority: ReasoningDefaultProvider,
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
		Mode:      ReasoningModeAlwaysOn, RequestField: "reasoning_effort",
		AllowedValues:   []string{"low", "high", "max"},
		ProviderDefault: "max", DefaultAuthority: ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning: false, RequiredToolChoiceWhileReasoning: true,
		PreserveAssistantReasoningForTools: true,
	},
}

var cacheUsageContracts = map[string]CacheUsageContract{
	"cache-usage.kimi-platform.v1": {
		ProfileID: "cache-usage.kimi-platform.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireMoonshotCachedTokensV1,
	},
}

func ResolveReasoningContract(profileID string) (ReasoningContract, bool) {
	contract, ok := reasoningContracts[profileID]
	if !ok {
		return ReasoningContract{}, false
	}
	contract.AllowedValues = slices.Clone(contract.AllowedValues)
	return contract, true
}

func ResolveCacheUsageContract(profileID string) (CacheUsageContract, bool) {
	contract, ok := cacheUsageContracts[profileID]
	return contract, ok
}
