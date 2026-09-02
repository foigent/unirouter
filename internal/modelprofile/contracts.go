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
	CacheUsageWireOpenAICachedTokensV1   = "openai.prompt_tokens_details.v1"
	CacheUsageWireOpenAIReadWriteV1      = "openai.prompt_tokens_details.read-write.v1"
	CacheUsageWireQwenContextCacheV1     = "qwen.prompt_tokens_details.v1"

	ReasoningControlWireString  = "string"
	ReasoningControlWireBoolean = "boolean"

	ReasoningResponseThinkingOmitted    = "omitted"
	ReasoningResponseThinkingSummarized = "summarized"
	ReasoningResponseThinkingEmbedded   = "embedded_in_content"
)

// ReasoningControlContract describes one exact request control owned by a
// reasoning profile. Toggle and effort remain separate because providers such
// as DeepSeek expose both controls while Kimi profiles expose only one.
type ReasoningControlContract struct {
	Field           string
	WireType        string
	AllowedValues   []string
	ProviderDefault string
	DisabledValues  []string
	// CompatibilityValues are provider-documented wire aliases whose effective
	// value is one of AllowedValues. They remain visible in evidence instead of
	// being silently rewritten by UniRouter.
	CompatibilityValues map[string]string
}

type ReasoningBudgetContract struct {
	Field                       string
	Minimum                     int64
	Maximum                     int64
	ProviderDefault             int64
	RequiresActiveReasoning     bool
	MutuallyExclusiveWithEffort bool
}

// ReasoningContract is the immutable request-side behavior owned by one
// reasoning profile. It deliberately describes component behavior rather than
// provider names, model aliases, or handler branches.
type ReasoningContract struct {
	ProfileID string
	Mode      string
	// RequestObjectField is set when the controls below live inside a typed
	// request object rather than at the operation root. For example, OpenAI
	// Responses carries effort as reasoning.effort while Chat Completions uses
	// reasoning_effort. An empty value preserves the historical root shape.
	RequestObjectField string
	// ToggleObjectField and EffortObjectField override RequestObjectField when
	// one provider places the two controls in different typed objects. They are
	// wire paths, not a second reasoning authority: both remain frozen by this
	// single immutable ReasoningContract.
	ToggleObjectField                  string
	EffortObjectField                  string
	ToggleControl                      ReasoningControlContract
	EffortControl                      ReasoningControlContract
	EffortValuesWhenDisabled           []string
	BudgetControl                      ReasoningBudgetContract
	ResponseThinkingTextMode           string
	DefaultAuthority                   string
	NamedToolChoiceWhileReasoning      bool
	RequiredToolChoiceWhileReasoning   bool
	PreserveAssistantReasoningForTools bool
	// PreserveAssistantReasoningForToolRequests is stricter than the historical
	// tool-call-only flag above: while reasoning is active and the current
	// invocation carries function tools, every assistant history message must
	// retain the provider reasoning field, even if that message made no call.
	PreserveAssistantReasoningForToolRequests bool
	ForbiddenRequestFieldsWhileActive         []string
	UnsupportedRequestFieldsWhileActive       []string
	ThinkingKeepValues                        []string
	ThinkingKeepProviderDefault               string
	ThinkingKeepRequiresEnabled               bool
	ThinkingClearSupported                    bool
	ThinkingClearProviderDefault              bool
	AllowedToolChoices                        []string
	FixedRequestNumbers                       map[string]string
	FixedRequestNumbersWhenDisabled           map[string]string
	FixedRequestNumbersReasoningOnly          bool
	EffortEnablesReasoning                    bool
	EffortEffectiveWithTools                  map[string]string
	ToggleAndEffortMutuallyExclusive          bool
	ActiveNonStreamTimeoutSeconds             int
	ActiveOutputFieldMaximums                 map[string]int64
	PreserveThinkingField                     string
	PreserveThinkingProviderDefault           bool
	PreserveThinkingRequiresEnabled           bool
	// SplitOutputField is an operation-root boolean that changes only the
	// provider response/replay shape. It remains part of this one reasoning
	// contract so admission, execution evidence, and historical fail-closed
	// behavior cannot drift into separate authorities.
	SplitOutputField            string
	SplitOutputProviderDefault  bool
	SplitOutputEnabledSupported bool
	ActiveStreamingUnsupported  bool
	// AssistantToolHistoryReplayField is provider state that must be replayed
	// on reasoning-active assistant tool history. It is distinct from visible
	// reasoning text and is validated by the selected immutable contract.
	AssistantToolHistoryReplayField string
}

func (contract ReasoningContract) ToggleRequestObjectField() string {
	if contract.ToggleObjectField != "" {
		return contract.ToggleObjectField
	}
	return contract.RequestObjectField
}

func (contract ReasoningContract) EffortRequestObjectField() string {
	if contract.EffortObjectField != "" {
		return contract.EffortObjectField
	}
	return contract.RequestObjectField
}

// CacheUsageContract binds one cache-usage profile to the exact provider wire
// evidence that may become billing truth. Unknown cache-like fields remain
// untrusted and receive no discount.
type CacheUsageContract struct {
	ProfileID          string
	Mode               string
	WireSchema         string
	ReportsRead        bool
	ReportsCreation    bool
	RequireReadCounter bool
}

var reasoningContracts = map[string]ReasoningContract{
	"reasoning.openai-gpt-5.6-sol-responses.v1": {
		ProfileID:          "reasoning.openai-gpt-5.6-sol-responses.v1",
		Mode:               ReasoningModeAlwaysOn,
		RequestObjectField: "reasoning",
		EffortControl: ReasoningControlContract{
			Field: "effort", WireType: ReasoningControlWireString,
			AllowedValues:   []string{"none", "low", "medium", "high", "xhigh", "max"},
			ProviderDefault: "medium", DisabledValues: []string{"none"},
		},
		DefaultAuthority: ReasoningDefaultProvider,
	},
	"reasoning.openai-gpt-5.6-terra-responses.v1": {
		ProfileID:          "reasoning.openai-gpt-5.6-terra-responses.v1",
		Mode:               ReasoningModeAlwaysOn,
		RequestObjectField: "reasoning",
		EffortControl: ReasoningControlContract{
			Field: "effort", WireType: ReasoningControlWireString,
			AllowedValues:   []string{"none", "low", "medium", "high", "xhigh", "max"},
			ProviderDefault: "medium", DisabledValues: []string{"none"},
		},
		DefaultAuthority: ReasoningDefaultProvider,
	},
	"reasoning.openai-gpt-5.6-luna-responses.v1": {
		ProfileID:          "reasoning.openai-gpt-5.6-luna-responses.v1",
		Mode:               ReasoningModeAlwaysOn,
		RequestObjectField: "reasoning",
		EffortControl: ReasoningControlContract{
			Field: "effort", WireType: ReasoningControlWireString,
			AllowedValues:   []string{"none", "low", "medium", "high", "xhigh", "max"},
			ProviderDefault: "medium", DisabledValues: []string{"none"},
		},
		DefaultAuthority: ReasoningDefaultProvider,
	},
	"reasoning.anthropic-sonnet-5-adaptive.v1": {
		ProfileID:          "reasoning.anthropic-sonnet-5-adaptive.v1",
		Mode:               ReasoningModeToggle,
		RequestObjectField: "thinking",
		ToggleControl: ReasoningControlContract{
			Field: "type", WireType: ReasoningControlWireString,
			AllowedValues: []string{"adaptive", "disabled"}, ProviderDefault: "adaptive",
			DisabledValues: []string{"disabled"},
		},
		DefaultAuthority: ReasoningDefaultProvider,
		UnsupportedRequestFieldsWhileActive: []string{
			"temperature", "top_p", "top_k",
		},
	},
	"reasoning.anthropic-sonnet-5-adaptive-effort.v1": {
		ProfileID:         "reasoning.anthropic-sonnet-5-adaptive-effort.v1",
		Mode:              ReasoningModeToggle,
		ToggleObjectField: "thinking",
		EffortObjectField: "output_config",
		ToggleControl: ReasoningControlContract{
			Field: "type", WireType: ReasoningControlWireString,
			AllowedValues: []string{"adaptive", "disabled"}, ProviderDefault: "adaptive",
			DisabledValues: []string{"disabled"},
		},
		EffortControl: ReasoningControlContract{
			Field: "effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"low", "medium", "high", "xhigh", "max"}, ProviderDefault: "high",
		},
		ResponseThinkingTextMode: ReasoningResponseThinkingOmitted,
		DefaultAuthority:         ReasoningDefaultProvider,
		UnsupportedRequestFieldsWhileActive: []string{
			"temperature", "top_p", "top_k",
		},
	},
	"reasoning.anthropic-opus-5-adaptive-effort.v1": {
		ProfileID:         "reasoning.anthropic-opus-5-adaptive-effort.v1",
		Mode:              ReasoningModeToggle,
		ToggleObjectField: "thinking",
		EffortObjectField: "output_config",
		ToggleControl: ReasoningControlContract{
			Field: "type", WireType: ReasoningControlWireString,
			AllowedValues: []string{"adaptive", "disabled"}, ProviderDefault: "adaptive",
			DisabledValues: []string{"disabled"},
		},
		EffortControl: ReasoningControlContract{
			Field: "effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"low", "medium", "high", "xhigh", "max"}, ProviderDefault: "high",
		},
		EffortValuesWhenDisabled: []string{"low", "medium", "high"},
		ResponseThinkingTextMode: ReasoningResponseThinkingOmitted,
		DefaultAuthority:         ReasoningDefaultProvider,
		UnsupportedRequestFieldsWhileActive: []string{
			"temperature", "top_p", "top_k",
		},
	},
	"reasoning.anthropic-haiku-4.5-extended-budget.v1": {
		ProfileID:          "reasoning.anthropic-haiku-4.5-extended-budget.v1",
		Mode:               ReasoningModeToggle,
		RequestObjectField: "thinking",
		ToggleControl: ReasoningControlContract{
			Field: "type", WireType: ReasoningControlWireString,
			AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "disabled",
			DisabledValues: []string{"disabled"},
		},
		BudgetControl: ReasoningBudgetContract{
			Field: "budget_tokens", Minimum: 1_024, Maximum: 63_999,
			RequiresActiveReasoning: true,
		},
		ResponseThinkingTextMode: ReasoningResponseThinkingSummarized,
		DefaultAuthority:         ReasoningDefaultProvider,
		UnsupportedRequestFieldsWhileActive: []string{
			"temperature", "top_p", "top_k",
		},
	},
	"reasoning.doubao-seed-2.1-pro-toggle.v1": {
		ProfileID: "reasoning.doubao-seed-2.1-pro-toggle.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		ResponseThinkingTextMode: ReasoningResponseThinkingSummarized,
		DefaultAuthority:         ReasoningDefaultProvider,
	},
	"reasoning.doubao-seed-2.1-pro-toggle-tools.v2": {
		ProfileID: "reasoning.doubao-seed-2.1-pro-toggle-tools.v2",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		ResponseThinkingTextMode:         ReasoningResponseThinkingSummarized,
		DefaultAuthority:                 ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:    true,
		RequiredToolChoiceWhileReasoning: true,
		AssistantToolHistoryReplayField:  "encrypted_content",
	},
	"reasoning.doubao-seed-2.1-turbo-effort.v1": {
		ProfileID: "reasoning.doubao-seed-2.1-turbo-effort.v1",
		Mode:      ReasoningModeToggle,
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"minimal", "low", "medium", "high"}, ProviderDefault: "high",
			DisabledValues: []string{"minimal"},
		},
		EffortEnablesReasoning:   true,
		ResponseThinkingTextMode: ReasoningResponseThinkingSummarized,
		DefaultAuthority:         ReasoningDefaultProvider,
	},
	"reasoning.doubao-seed-2.1-turbo-effort-tools.v2": {
		ProfileID: "reasoning.doubao-seed-2.1-turbo-effort-tools.v2",
		Mode:      ReasoningModeToggle,
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"minimal", "low", "medium", "high"}, ProviderDefault: "high",
			DisabledValues: []string{"minimal"},
		},
		EffortEnablesReasoning:           true,
		ResponseThinkingTextMode:         ReasoningResponseThinkingSummarized,
		DefaultAuthority:                 ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:    true,
		RequiredToolChoiceWhileReasoning: true,
		AssistantToolHistoryReplayField:  "encrypted_content",
	},
	"reasoning.doubao-seed-evolving-toggle-effort.v1": {
		ProfileID: "reasoning.doubao-seed-evolving-toggle-effort.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues:       []string{"low", "medium", "high"},
			ProviderDefault:     "high",
			DisabledValues:      []string{"none", "minimal"},
			CompatibilityValues: map[string]string{"xhigh": "high", "max": "high"},
		},
		ToggleAndEffortMutuallyExclusive: true,
		ActiveNonStreamTimeoutSeconds:    1_800,
		ResponseThinkingTextMode:         ReasoningResponseThinkingSummarized,
		DefaultAuthority:                 ReasoningDefaultProvider,
	},
	"reasoning.doubao-seed-evolving-toggle-effort.v2": {
		ProfileID: "reasoning.doubao-seed-evolving-toggle-effort.v2",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues:       []string{"low", "medium", "high"},
			ProviderDefault:     "high",
			DisabledValues:      []string{"none", "minimal"},
			CompatibilityValues: map[string]string{"xhigh": "high", "max": "high"},
		},
		ToggleAndEffortMutuallyExclusive: true,
		ActiveNonStreamTimeoutSeconds:    1_800,
		ResponseThinkingTextMode:         ReasoningResponseThinkingSummarized,
		DefaultAuthority:                 ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:    true,
		RequiredToolChoiceWhileReasoning: true,
		AssistantToolHistoryReplayField:  "encrypted_content",
	},
	"reasoning.openai-gpt-5.6-chat.v1": {
		ProfileID: "reasoning.openai-gpt-5.6-chat.v1",
		Mode:      ReasoningModeAlwaysOn,
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues:   []string{"none", "low", "medium", "high", "xhigh", "max"},
			ProviderDefault: "medium", DisabledValues: []string{"none"},
		},
		DefaultAuthority:                 ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:    true,
		RequiredToolChoiceWhileReasoning: true,
	},
	"reasoning.hy3-chat.v1": {
		ProfileID: "reasoning.hy3-chat.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled", "adaptive"}, ProviderDefault: "disabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"low", "medium", "high"}, ProviderDefault: "low",
		},
		DefaultAuthority:                   ReasoningDefaultProvider,
		EffortEnablesReasoning:             true,
		EffortEffectiveWithTools:           map[string]string{"low": "high"},
		PreserveAssistantReasoningForTools: true,
		AllowedToolChoices:                 []string{"none", "auto"},
	},
	"reasoning.mimo-v2.5-thinking-toggle.v1": {
		ProfileID: "reasoning.mimo-v2.5-thinking-toggle.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		DefaultAuthority:                   ReasoningDefaultProvider,
		PreserveAssistantReasoningForTools: true,
		AllowedToolChoices:                 []string{"auto"},
		FixedRequestNumbers: map[string]string{
			"temperature": "1", "top_p": "0.95",
		},
		FixedRequestNumbersReasoningOnly: true,
	},
	"reasoning.gemini-3.5-flash-lite.v1": {
		ProfileID: "reasoning.gemini-3.5-flash-lite.v1",
		Mode:      ReasoningModeAlwaysOn,
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"minimal", "low", "medium", "high"}, ProviderDefault: "minimal",
		},
		DefaultAuthority: ReasoningDefaultProvider,
	},
	"reasoning.gemini-3.5-flash.v1": {
		ProfileID: "reasoning.gemini-3.5-flash.v1",
		Mode:      ReasoningModeAlwaysOn,
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"minimal", "low", "medium", "high"}, ProviderDefault: "medium",
		},
		DefaultAuthority: ReasoningDefaultProvider,
	},
	"reasoning.gemini-3.7-flash.v1": {
		ProfileID: "reasoning.gemini-3.7-flash.v1",
		Mode:      ReasoningModeAlwaysOn,
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"low", "medium", "high"}, ProviderDefault: "medium",
		},
		DefaultAuthority: ReasoningDefaultProvider,
	},
	"reasoning.qwen3.7-hybrid.v1": {
		ProfileID: "reasoning.qwen3.7-hybrid.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "enable_thinking", WireType: ReasoningControlWireBoolean,
			AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		BudgetControl: ReasoningBudgetContract{
			Field: "thinking_budget", Minimum: 1, Maximum: 262_144, ProviderDefault: 262_144,
			RequiresActiveReasoning: true,
		},
		DefaultAuthority:                ReasoningDefaultProvider,
		PreserveThinkingField:           "preserve_thinking",
		PreserveThinkingProviderDefault: false,
	},
	"reasoning.qwen3.8-max-hybrid.v1": {
		ProfileID: "reasoning.qwen3.8-max-hybrid.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "enable_thinking", WireType: ReasoningControlWireBoolean,
			AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"low", "medium", "xhigh"}, ProviderDefault: "xhigh",
			CompatibilityValues: map[string]string{"minimal": "low", "high": "xhigh", "max": "xhigh"},
			DisabledValues:      []string{"none"},
		},
		BudgetControl: ReasoningBudgetContract{
			Field: "thinking_budget", Minimum: 0, Maximum: 262_144, ProviderDefault: 131_072,
			RequiresActiveReasoning: true, MutuallyExclusiveWithEffort: true,
		},
		DefaultAuthority:                 ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:    false,
		RequiredToolChoiceWhileReasoning: false,
		ActiveOutputFieldMaximums:        map[string]int64{"max_tokens": 32_768},
		PreserveThinkingField:            "preserve_thinking",
		PreserveThinkingProviderDefault:  true,
	},
	"reasoning.qwen3.8-max-hybrid.v2": {
		ProfileID: "reasoning.qwen3.8-max-hybrid.v2",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "enable_thinking", WireType: ReasoningControlWireBoolean,
			AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", WireType: ReasoningControlWireString,
			AllowedValues: []string{"low", "medium", "xhigh"}, ProviderDefault: "xhigh",
			CompatibilityValues: map[string]string{"minimal": "low", "high": "xhigh", "max": "xhigh"},
			DisabledValues:      []string{"none"},
		},
		BudgetControl: ReasoningBudgetContract{
			Field: "thinking_budget", Minimum: 0, Maximum: 262_144, ProviderDefault: 131_072,
			RequiresActiveReasoning: true, MutuallyExclusiveWithEffort: true,
		},
		DefaultAuthority:                 ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:    false,
		RequiredToolChoiceWhileReasoning: false,
		PreserveThinkingField:            "preserve_thinking",
		PreserveThinkingProviderDefault:  true,
	},
	"reasoning.glm-5v-turbo-thinking-toggle.v1": {
		ProfileID: "reasoning.glm-5v-turbo-thinking-toggle.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"high", "max"}, ProviderDefault: "max",
			CompatibilityValues: map[string]string{"low": "high", "medium": "high", "xhigh": "max"},
			DisabledValues:      []string{"none", "minimal"},
		},
		DefaultAuthority:             ReasoningDefaultProvider,
		ThinkingClearSupported:       true,
		ThinkingClearProviderDefault: true,
	},
	"reasoning.glm-5.2-thinking-toggle.v1": {
		ProfileID: "reasoning.glm-5.2-thinking-toggle.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"high", "max"}, ProviderDefault: "max",
			CompatibilityValues: map[string]string{"low": "high", "medium": "high", "xhigh": "max"},
			DisabledValues:      []string{"none", "minimal"},
		},
		DefaultAuthority:             ReasoningDefaultProvider,
		ThinkingClearSupported:       true,
		ThinkingClearProviderDefault: true,
	},
	"reasoning.glm-5-family-thinking-toggle.v1": {
		ProfileID: "reasoning.glm-5-family-thinking-toggle.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		DefaultAuthority: ReasoningDefaultProvider,
	},
	"reasoning.glm-5.3-required.v1": {
		ProfileID: "reasoning.glm-5.3-required.v1",
		Mode:      ReasoningModeRequired,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"low", "high", "max"}, ProviderDefault: "max",
		},
		DefaultAuthority:             ReasoningDefaultProvider,
		ThinkingClearSupported:       true,
		ThinkingClearProviderDefault: true,
	},
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
	"reasoning.deepseek-v4-flash-thinking-toggle.v3": {
		ProfileID: "reasoning.deepseek-v4-flash-thinking-toggle.v3",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"low", "high", "max"}, ProviderDefault: "high",
			CompatibilityValues: map[string]string{"medium": "high", "xhigh": "high"},
		},
		DefaultAuthority:                   ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:      true,
		RequiredToolChoiceWhileReasoning:   true,
		PreserveAssistantReasoningForTools: true,
		ForbiddenRequestFieldsWhileActive: []string{
			"temperature", "top_p", "presence_penalty", "frequency_penalty",
		},
		AllowedToolChoices: []string{"none", "auto", "required", "named"},
	},
	"reasoning.deepseek-v4-pro-thinking-toggle.v3": {
		ProfileID: "reasoning.deepseek-v4-pro-thinking-toggle.v3",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"low", "high", "max"}, ProviderDefault: "high",
			CompatibilityValues: map[string]string{"medium": "high", "xhigh": "high"},
		},
		DefaultAuthority:                   ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:      true,
		RequiredToolChoiceWhileReasoning:   true,
		PreserveAssistantReasoningForTools: true,
		ForbiddenRequestFieldsWhileActive: []string{
			"temperature", "top_p", "presence_penalty", "frequency_penalty",
		},
		AllowedToolChoices: []string{"none", "auto", "required", "named"},
	},
	"reasoning.deepseek-v4-flash-thinking-toggle.v4": {
		ProfileID: "reasoning.deepseek-v4-flash-thinking-toggle.v4",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"low", "high", "max"}, ProviderDefault: "high",
			CompatibilityValues: map[string]string{"medium": "high", "xhigh": "high"},
		},
		DefaultAuthority:                          ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:             true,
		RequiredToolChoiceWhileReasoning:          true,
		PreserveAssistantReasoningForTools:        true,
		PreserveAssistantReasoningForToolRequests: true,
		ForbiddenRequestFieldsWhileActive: []string{
			"temperature", "top_p", "presence_penalty", "frequency_penalty",
		},
		AllowedToolChoices: []string{"none", "auto", "required", "named"},
	},
	"reasoning.deepseek-v4-pro-thinking-toggle.v4": {
		ProfileID: "reasoning.deepseek-v4-pro-thinking-toggle.v4",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"low", "high", "max"}, ProviderDefault: "high",
			CompatibilityValues: map[string]string{"medium": "high", "xhigh": "high"},
		},
		DefaultAuthority:                          ReasoningDefaultProvider,
		NamedToolChoiceWhileReasoning:             true,
		RequiredToolChoiceWhileReasoning:          true,
		PreserveAssistantReasoningForTools:        true,
		PreserveAssistantReasoningForToolRequests: true,
		ForbiddenRequestFieldsWhileActive: []string{
			"temperature", "top_p", "presence_penalty", "frequency_penalty",
		},
		AllowedToolChoices: []string{"none", "auto", "required", "named"},
	},
	"reasoning.deepseek-v4-flash-thinking-toggle.v5": {
		ProfileID: "reasoning.deepseek-v4-flash-thinking-toggle.v5",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"low", "high", "max"}, ProviderDefault: "high",
			CompatibilityValues: map[string]string{"medium": "high", "xhigh": "high"},
		},
		DefaultAuthority:                          ReasoningDefaultProvider,
		PreserveAssistantReasoningForTools:        true,
		PreserveAssistantReasoningForToolRequests: true,
		ForbiddenRequestFieldsWhileActive: []string{
			"temperature", "top_p", "presence_penalty", "frequency_penalty",
		},
		UnsupportedRequestFieldsWhileActive: []string{"tool_choice"},
		AllowedToolChoices:                  []string{"none", "auto", "required", "named"},
	},
	"reasoning.deepseek-v4-pro-thinking-toggle.v5": {
		ProfileID: "reasoning.deepseek-v4-pro-thinking-toggle.v5",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", AllowedValues: []string{"enabled", "disabled"}, ProviderDefault: "enabled",
		},
		EffortControl: ReasoningControlContract{
			Field: "reasoning_effort", AllowedValues: []string{"low", "high", "max"}, ProviderDefault: "high",
			CompatibilityValues: map[string]string{"medium": "high", "xhigh": "high"},
		},
		DefaultAuthority:                          ReasoningDefaultProvider,
		PreserveAssistantReasoningForTools:        true,
		PreserveAssistantReasoningForToolRequests: true,
		ForbiddenRequestFieldsWhileActive: []string{
			"temperature", "top_p", "presence_penalty", "frequency_penalty",
		},
		UnsupportedRequestFieldsWhileActive: []string{"tool_choice"},
		AllowedToolChoices:                  []string{"none", "auto", "required", "named"},
	},
	"reasoning.minimax-m3-adaptive-embedded.v1": {
		ProfileID: "reasoning.minimax-m3-adaptive-embedded.v1",
		Mode:      ReasoningModeToggle,
		ToggleControl: ReasoningControlContract{
			Field: "thinking", WireType: ReasoningControlWireString,
			AllowedValues: []string{"adaptive", "disabled"}, ProviderDefault: "adaptive",
			DisabledValues: []string{"disabled"},
		},
		ResponseThinkingTextMode:        ReasoningResponseThinkingEmbedded,
		DefaultAuthority:                ReasoningDefaultProvider,
		SplitOutputField:                "reasoning_split",
		SplitOutputProviderDefault:      false,
		SplitOutputEnabledSupported:     false,
		ActiveStreamingUnsupported:      true,
		AssistantToolHistoryReplayField: "content",
	},
	"reasoning.minimax-m2-required-embedded.v1": {
		ProfileID:                       "reasoning.minimax-m2-required-embedded.v1",
		Mode:                            ReasoningModeRequired,
		ResponseThinkingTextMode:        ReasoningResponseThinkingEmbedded,
		DefaultAuthority:                ReasoningDefaultProvider,
		SplitOutputField:                "reasoning_split",
		SplitOutputProviderDefault:      false,
		SplitOutputEnabledSupported:     false,
		AssistantToolHistoryReplayField: "content",
	},
}

var cacheUsageContracts = map[string]CacheUsageContract{
	"cache-usage.openai-gpt-5.6.v1": {
		ProfileID: "cache-usage.openai-gpt-5.6.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireOpenAIReadWriteV1,
		ReportsRead: true, ReportsCreation: true,
	},
	"cache-usage.hy3-openai.v1": {
		ProfileID: "cache-usage.hy3-openai.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireOpenAICachedTokensV1, ReportsRead: true,
	},
	"cache-usage.mimo-openai.v1": {
		ProfileID: "cache-usage.mimo-openai.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireOpenAICachedTokensV1, ReportsRead: true,
	},
	"cache-usage.glm-openai.v1": {
		ProfileID: "cache-usage.glm-openai.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireOpenAICachedTokensV1, ReportsRead: true,
	},
	"cache-usage.doubao-ark-chat.v1": {
		ProfileID: "cache-usage.doubao-ark-chat.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireOpenAICachedTokensV1,
		ReportsRead: true, RequireReadCounter: true,
	},
	"cache-usage.minimax-openai.v1": {
		ProfileID: "cache-usage.minimax-openai.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireOpenAICachedTokensV1,
		ReportsRead: true, RequireReadCounter: true,
	},
	"cache-usage.kimi-platform.v1": {
		ProfileID: "cache-usage.kimi-platform.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireMoonshotCachedTokensV1, ReportsRead: true,
	},
	"cache-usage.deepseek-v4.v1": {
		ProfileID: "cache-usage.deepseek-v4.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireDeepSeekHitMissV1, ReportsRead: true,
	},
	"cache-usage.qwen-context-cache.v1": {
		ProfileID: "cache-usage.qwen-context-cache.v1",
		Mode:      CacheUsageModeAutomatic, WireSchema: CacheUsageWireQwenContextCacheV1,
		ReportsRead: true, ReportsCreation: true,
	},
}

func ResolveReasoningContract(profileID string) (ReasoningContract, bool) {
	contract, ok := reasoningContracts[profileID]
	if !ok {
		return ReasoningContract{}, false
	}
	contract.ToggleControl.AllowedValues = slices.Clone(contract.ToggleControl.AllowedValues)
	contract.ToggleControl.DisabledValues = slices.Clone(contract.ToggleControl.DisabledValues)
	contract.ToggleControl.CompatibilityValues = cloneStringMap(contract.ToggleControl.CompatibilityValues)
	contract.EffortControl.AllowedValues = slices.Clone(contract.EffortControl.AllowedValues)
	contract.EffortControl.DisabledValues = slices.Clone(contract.EffortControl.DisabledValues)
	contract.EffortControl.CompatibilityValues = cloneStringMap(contract.EffortControl.CompatibilityValues)
	contract.EffortValuesWhenDisabled = slices.Clone(contract.EffortValuesWhenDisabled)
	contract.ForbiddenRequestFieldsWhileActive = slices.Clone(contract.ForbiddenRequestFieldsWhileActive)
	contract.UnsupportedRequestFieldsWhileActive = slices.Clone(contract.UnsupportedRequestFieldsWhileActive)
	contract.ThinkingKeepValues = slices.Clone(contract.ThinkingKeepValues)
	contract.AllowedToolChoices = slices.Clone(contract.AllowedToolChoices)
	contract.FixedRequestNumbers = cloneStringMap(contract.FixedRequestNumbers)
	contract.FixedRequestNumbersWhenDisabled = cloneStringMap(contract.FixedRequestNumbersWhenDisabled)
	contract.EffortEffectiveWithTools = cloneStringMap(contract.EffortEffectiveWithTools)
	contract.ActiveOutputFieldMaximums = cloneInt64Map(contract.ActiveOutputFieldMaximums)
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

func cloneInt64Map(input map[string]int64) map[string]int64 {
	if input == nil {
		return nil
	}
	result := make(map[string]int64, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
