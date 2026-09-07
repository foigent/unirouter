package modelprofile

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

const (
	CapabilityStreaming        = "streaming"
	CapabilityPartialMode      = "partial_mode"
	CapabilityDynamicTools     = "dynamic_tools"
	CapabilityBuiltinWebSearch = "builtin_web_search"
	CapabilityVision           = "vision"
	CapabilityAudio            = "audio"
	CapabilityVideo            = "video"
	CapabilityFiles            = "files"
	CapabilityTools            = "tools"
	CapabilityFunctionTools    = "function_tools"
	CapabilityToolChoiceAuto   = "tool_choice_auto"
	CapabilityToolChoiceNamed  = "tool_choice_named"
	CapabilityToolChoiceReq    = "tool_choice_required"
	CapabilityParallelTools    = "parallel_tool_calls"
	CapabilityToolStream       = "tool_stream"
	CapabilityReasoning        = "reasoning"
	CapabilityReasoningBudget  = "reasoning_budget"
	CapabilityReasoningEffort  = "reasoning_effort"
	CapabilityJSONObject       = "json_object"
	CapabilityJSONSchema       = "json_schema"
	CapabilityStrictJSONSchema = "strict_json_schema"
	CapabilityJSONOutput       = "json_output"
	CapabilityPromptCache      = "prompt_cache"
	CapabilityExplicitCache    = "explicit_prompt_cache"
	CapabilityCacheReadUsage   = "cache_read_usage"
	CapabilityCacheCreateUsage = "cache_creation_usage"
	CapabilityBuiltinSearchReq = "builtin_web_search_request"
	CapabilityServiceTier      = "service_tier"
)

// ChatProtocolContract describes advanced Chat Completions shapes whose wire
// compatibility alone is insufficient. The gateway still owns admission,
// guardrails, and billing; this profile says which exact shapes may be used by
// an immutable model execution profile.
type ChatProtocolContract struct {
	ProfileID string

	StreamingModeled                   bool
	Streaming                          bool
	StreamContract                     ChatStreamContract
	OutputEnforcement                  string
	OutputLimitModeled                 bool
	OutputLimitField                   string
	OutputLimitAcceptedFields          []string
	OutputLimitFieldsMutuallyExclusive bool
	OutputLimitMin                     int64
	OutputLimitMax                     int64
	// OutputLimitFieldMaximums narrows individual accepted aliases when the
	// provider documents different wire ceilings. OutputLimitMax remains the
	// broad profile ceiling and the default for fields absent from this map.
	OutputLimitFieldMaximums                map[string]int64
	FunctionToolsModeled                    bool
	FunctionTools                           bool
	MaxFunctionTools                        int
	FunctionToolDescriptionRequired         bool
	FunctionToolParametersRequired          bool
	FunctionToolParametersObjectWhenPresent bool
	FunctionToolStrictSupported             bool
	FunctionToolNamePattern                 string
	FunctionToolChoices                     []string
	// FunctionToolProviderDefaultChoice is the normalized effective choice when
	// this invocation declares function tools but omits tool_choice. It is kept
	// separate from FunctionToolChoices because a provider can document an
	// omitted default without accepting the same value as an explicit wire field.
	FunctionToolProviderDefaultChoice string
	NamedFunctionToolChoice           bool
	ParallelFunctionToolCalls         bool
	FunctionToolChoiceRequiresTools   bool
	// FunctionToolChoicesRequiringTools narrows the historical all-or-nothing
	// FunctionToolChoiceRequiresTools switch. "named" represents the typed
	// object form. Existing immutable contracts keep their original behavior.
	FunctionToolChoicesRequiringTools         []string
	FunctionToolInvocationCapabilitiesModeled bool
	FunctionToolsRequireNonStream             bool
	ParallelFunctionToolCallsDefaultModeled   bool
	ParallelFunctionToolCallsProviderDefault  bool
	ExactFunctionToolHistoryModeled           bool
	ExactFunctionToolResponseModeled          bool
	// AssistantToolResponseReplayField is provider state that must be present
	// on each successful assistant tool-call response so the next invocation
	// can satisfy the same immutable replay contract.
	AssistantToolResponseReplayField            string
	ToolStream                                  bool
	ToolStreamField                             string
	ResponseFormatModeled                       bool
	ResponseFormatTypes                         []string
	JSONObjectPromptKeyword                     string
	JSONSchemaStrictRequired                    bool
	JSONSchemaObjectRequired                    bool
	JSONSchemaAdditionalPropertiesFalseRequired bool
	JSONSchemaRequiresNonStream                 bool
	JSONSchemaDisallowsFunctionTools            bool
	JSONSchemaDescriptionStringWhenPresent      bool
	JSONSchemaIncompatibleRequestFields         []string
	PromptCacheControlModeled                   bool
	ExplicitPromptCache                         bool
	PromptCacheBreakpointField                  string
	PromptCacheControlType                      string
	MaxPromptCacheMarkers                       int
	PromptCacheOptionsField                     string
	PromptCacheModes                            []string
	PromptCacheTTLs                             []string
	SearchRequestModeled                        bool
	BuiltinSearchRequest                        bool
	SearchEnableField                           string
	SearchOptionsField                          string
	SearchOptionEnableField                     string
	SearchStrategyField                         string
	SearchForcedField                           string
	SearchUserLocation                          bool
	SearchStrategies                            []string
	SearchDefaultStrategy                       string
	ForcedSearch                                bool
	ProcessingTierPolicyID                      string
	// AssistantReasoningHistoryModeled makes request-side replay fields an
	// exact protocol decision. When true, only the named fields may appear and
	// only on assistant messages; an empty allowlist means history replay is
	// explicitly unsupported rather than inherited from generic admission.
	AssistantReasoningHistoryModeled bool
	AssistantReasoningHistoryFields  []string
	// AssistantReasoningResponseModeled makes response-side reasoning text an
	// exact protocol decision. When true, only the named text fields may appear;
	// an empty allowlist means a plain-text-only response contract.
	AssistantReasoningResponseModeled bool
	AssistantReasoningResponseFields  []string
	// AssistantOpaqueHistoryFields are request-side provider state. They are
	// validated on the selected immutable contract, removed only from the
	// admission shadow, and preserved byte-for-byte in the provider payload.
	AssistantOpaqueHistoryModeled bool
	AssistantOpaqueHistoryFields  []string
	// OpaqueAssistantMessageFields names exact provider response members that
	// are preserved as non-text evidence. They are allowed only when the
	// selected immutable protocol profile declares them and never enter the
	// content guardrail or become a second response authority.
	OpaqueAssistantMessageFields []string

	PartialMode             bool
	PartialRejectJSONObject bool
	DynamicSystemTools      bool
	BuiltinWebSearch        bool
	BuiltinWebSearchName    string
	BuiltinSearchMeter      string

	// Builtin search v1 is intentionally bounded to one non-stream call per
	// provider response. This gives Freeze a finite liability ceiling and lets
	// the non-stream response parser produce authoritative usage evidence.
	BuiltinSearchRequiresNonStream        bool
	BuiltinSearchRequiresSerial           bool
	BuiltinSearchRequiresThinkingDisabled bool
	MaxBuiltinSearchCalls                 int64

	InlineImageDataURL   bool
	InlineAudioBase64    bool
	RemoteImageHTTPSURL  bool
	RemoteAudioHTTPSURL  bool
	InlineVideoDataURL   bool
	RemoteVideoHTTPSURL  bool
	RemoteFileHTTPSURL   bool
	ExclusiveMediaKinds  bool
	InlineImageMIMETypes []string
	ImageDetails         []string
	// ImageInputPolicy adds exact request and admission constraints for an
	// image-capable immutable protocol. The legacy booleans and MIME list above
	// remain the transport/MIME authority; a zero policy preserves every
	// historical protocol unchanged.
	ImageInputPolicy     ChatImageInputPolicy
	InlineAudioFormats   []string
	InlineVideoMIMETypes []string
	VideoFPSSupported    bool
	VideoFPSMinimum      float64
	VideoFPSMaximum      float64
	VideoResolutions     []string
	RemoteFileExtensions []string
}

// ChatImageInputPolicy is a bounded, provider/surface-specific image request
// contract. AdmissionTokenUpperPerPixel is a versioned UniRouter safety bound,
// not a provider tokenizer claim and not usage or pricing authority.
type ChatImageInputPolicy struct {
	Modeled                     bool
	DecodedFormat               string
	RequireURLObject            bool
	AllowedMessageRoles         []string
	MaximumImages               int
	MaximumDecodedBytes         int64
	MinimumWidth                int
	MinimumHeight               int
	MinimumPixels               int64
	MaximumPixels               int64
	MaximumAspectRatio          int64
	RequiresNonStream           bool
	IncompatibleRequestFields   []string
	AdmissionTokenUpperPerPixel int64
	AdmissionEstimateVersion    string
}

// ChatStreamContract is an exact opt-in for request options and response
// frames that cannot safely inherit the generic OpenAI-compatible stream
// union. A zero value preserves every historical protocol profile unchanged.
type ChatStreamContract struct {
	RequestOptionsModeled     bool
	AllowedRequestOptions     []string
	RequireIncludeUsage       bool
	ResponseModeled           bool
	RequireDone               bool
	RequireTerminalUsage      bool
	AllowTerminalUsageChoices bool
	DeltaTextFields           []string
	DeltaOpaqueFields         []string
	FinishReasons             []string
	ReasoningOpaqueAnswerFlow bool
	// Opt-in to bounded shared function-tool deltas; zero preserves old streams.
	FunctionToolDeltas bool
}

const (
	ProcessingTierStandard = "standard"
	ProcessingTierFlex     = "flex"
	ProcessingTierPriority = "priority"

	ProcessingTierPolicyGeminiOpenAICompatibilityV1 = "processing-tier.gemini-openai-compatibility.v1"
	ProcessingTierPolicyMiniMaxOpenAIChatV1         = "processing-tier.minimax-openai-chat.v1"
)

// ProcessingTierRequestValue maps one provider/surface wire value onto the
// normalized UniRouter customer tier and the provider I/O patience required by
// that exact contract. It is deliberately not a universal provider enum.
type ProcessingTierRequestValue struct {
	WireValue             string
	NormalizedTier        string
	RequestTimeoutSeconds int
}

// ProcessingTierPolicy composes request semantics, runtime policy, and
// response evidence for one provider API surface. Customer pricing remains a
// separate authority and must not be inferred from these provider wire values.
type ProcessingTierPolicy struct {
	ID                       string
	Provider                 string
	APISurface               string
	RequestField             string
	OmittedNormalizedTier    string
	OmittedTimeoutSeconds    int
	RequestValues            []ProcessingTierRequestValue
	ObservedResponseHeader   string
	ObservedNormalizedValues []string
}

var processingTierPolicies = map[string]ProcessingTierPolicy{
	ProcessingTierPolicyGeminiOpenAICompatibilityV1: {
		ID:                     ProcessingTierPolicyGeminiOpenAICompatibilityV1,
		Provider:               "gemini",
		APISurface:             APISurfaceOpenAIChat,
		RequestField:           "service_tier",
		OmittedNormalizedTier:  ProcessingTierStandard,
		OmittedTimeoutSeconds:  300,
		ObservedResponseHeader: "x-gemini-service-tier",
		RequestValues: []ProcessingTierRequestValue{
			{WireValue: "default", NormalizedTier: ProcessingTierStandard, RequestTimeoutSeconds: 300},
			{WireValue: "flex", NormalizedTier: ProcessingTierFlex, RequestTimeoutSeconds: 900},
			{WireValue: "priority", NormalizedTier: ProcessingTierPriority, RequestTimeoutSeconds: 300},
		},
		ObservedNormalizedValues: []string{
			ProcessingTierStandard,
			ProcessingTierFlex,
			ProcessingTierPriority,
		},
	},
	ProcessingTierPolicyMiniMaxOpenAIChatV1: {
		ID:                    ProcessingTierPolicyMiniMaxOpenAIChatV1,
		Provider:              "minimax",
		APISurface:            APISurfaceOpenAIChat,
		RequestField:          "service_tier",
		OmittedNormalizedTier: ProcessingTierStandard,
		OmittedTimeoutSeconds: 300,
		RequestValues: []ProcessingTierRequestValue{
			{WireValue: "standard", NormalizedTier: ProcessingTierStandard, RequestTimeoutSeconds: 300},
			{WireValue: "priority", NormalizedTier: ProcessingTierPriority, RequestTimeoutSeconds: 300},
		},
		// The current MiniMax OpenAI-compatible response contract does not
		// report the delivered tier. Empty observation fields are an explicit
		// evidence boundary; the gateway must never infer one from the request.
	},
}

func ResolveProcessingTierPolicy(policyID string) (ProcessingTierPolicy, bool) {
	policy, ok := processingTierPolicies[strings.TrimSpace(policyID)]
	policy.RequestValues = slices.Clone(policy.RequestValues)
	policy.ObservedNormalizedValues = slices.Clone(policy.ObservedNormalizedValues)
	return policy, ok
}

func ResolveProcessingTierRequest(policy ProcessingTierPolicy, wireValue string) (ProcessingTierRequestValue, bool) {
	for _, candidate := range policy.RequestValues {
		if candidate.WireValue == wireValue {
			return candidate, true
		}
	}
	return ProcessingTierRequestValue{}, false
}

func validateProcessingTierPolicyShape(policy ProcessingTierPolicy) error {
	if strings.TrimSpace(policy.ID) == "" || policy.ID != strings.TrimSpace(policy.ID) ||
		strings.TrimSpace(policy.Provider) == "" || policy.Provider != strings.TrimSpace(policy.Provider) ||
		strings.TrimSpace(policy.APISurface) == "" || policy.APISurface != strings.TrimSpace(policy.APISurface) ||
		policy.RequestField != "service_tier" || policy.OmittedTimeoutSeconds <= 0 || len(policy.RequestValues) == 0 {
		return fmt.Errorf("processing-tier policy identity and request semantics are incomplete")
	}
	validNormalizedTier := func(value string) bool {
		return slices.Contains([]string{ProcessingTierStandard, ProcessingTierFlex, ProcessingTierPriority}, value)
	}
	if !validNormalizedTier(policy.OmittedNormalizedTier) {
		return fmt.Errorf("processing-tier policy has unsupported omitted tier %q", policy.OmittedNormalizedTier)
	}
	seenWireValues := make(map[string]struct{}, len(policy.RequestValues))
	for _, value := range policy.RequestValues {
		if strings.TrimSpace(value.WireValue) == "" || value.WireValue != strings.TrimSpace(value.WireValue) ||
			!validNormalizedTier(value.NormalizedTier) || value.RequestTimeoutSeconds <= 0 {
			return fmt.Errorf("processing-tier policy has invalid request value %q", value.WireValue)
		}
		if _, duplicate := seenWireValues[value.WireValue]; duplicate {
			return fmt.Errorf("processing-tier policy has duplicate request value %q", value.WireValue)
		}
		seenWireValues[value.WireValue] = struct{}{}
	}
	headerModeled := strings.TrimSpace(policy.ObservedResponseHeader) != ""
	valuesModeled := len(policy.ObservedNormalizedValues) != 0
	if headerModeled != valuesModeled {
		return fmt.Errorf("processing-tier observation header and normalized values must be modeled together")
	}
	if headerModeled && policy.ObservedResponseHeader != strings.TrimSpace(policy.ObservedResponseHeader) {
		return fmt.Errorf("processing-tier observation header is invalid")
	}
	seenObserved := make(map[string]struct{}, len(policy.ObservedNormalizedValues))
	for _, value := range policy.ObservedNormalizedValues {
		if !validNormalizedTier(value) {
			return fmt.Errorf("processing-tier policy has unsupported observed tier %q", value)
		}
		if _, duplicate := seenObserved[value]; duplicate {
			return fmt.Errorf("processing-tier policy has duplicate observed tier %q", value)
		}
		seenObserved[value] = struct{}{}
	}
	return nil
}

type RequestExtensionWireType string

const (
	RequestExtensionBoolean RequestExtensionWireType = "boolean"
	RequestExtensionInteger RequestExtensionWireType = "integer"
	RequestExtensionObject  RequestExtensionWireType = "object"
)

type RequestExtensionField struct {
	Field    string
	WireType RequestExtensionWireType
}

func openAIChatBaseContract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:        profileID,
		StreamingModeled: true, Streaming: true,
		OutputLimitModeled: true, OutputLimitField: "max_completion_tokens",
		OutputLimitAcceptedFields: []string{"max_completion_tokens"},
		OutputLimitMin:            1, OutputLimitMax: 128_000,
		FunctionToolsModeled:      true,
		ResponseFormatModeled:     true,
		ResponseFormatTypes:       []string{"text"},
		PromptCacheControlModeled: true,
	}
}

func openAIChatToolsContract(profileID string) ChatProtocolContract {
	contract := openAIChatBaseContract(profileID)
	contract.FunctionTools = true
	// OpenAI does not publish a GPT-5.6-specific function-count limit. This is
	// UniRouter's local safety bound, not a provider maximum.
	contract.MaxFunctionTools = 128
	// OpenAI permits parameters to be omitted for functions without arguments.
	// The typed contract separately requires an object when the field is present.
	contract.FunctionToolParametersRequired = false
	contract.FunctionToolParametersObjectWhenPresent = true
	contract.FunctionToolStrictSupported = true
	contract.FunctionToolNamePattern = `^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`
	contract.FunctionToolChoices = []string{"none", "auto", "required"}
	contract.NamedFunctionToolChoice = true
	contract.ParallelFunctionToolCalls = true
	contract.FunctionToolChoiceRequiresTools = true
	return contract
}

func openAIChatStructuredOutputContract(profileID string) ChatProtocolContract {
	contract := openAIChatToolsContract(profileID)
	contract.ResponseFormatTypes = []string{"text", "json_object", "json_schema"}
	contract.JSONObjectPromptKeyword = "json"
	contract.JSONSchemaStrictRequired = true
	contract.JSONSchemaObjectRequired = true
	return contract
}

func openAIChatVisionContract(profileID string) ChatProtocolContract {
	contract := openAIChatStructuredOutputContract(profileID)
	contract.InlineImageDataURL = true
	contract.RemoteImageHTTPSURL = true
	contract.InlineImageMIMETypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}
	contract.ImageDetails = []string{"auto", "low", "high", "original"}
	return contract
}

func openAIChatCacheContract(profileID string) ChatProtocolContract {
	contract := openAIChatVisionContract(profileID)
	contract.ExplicitPromptCache = true
	contract.PromptCacheBreakpointField = "prompt_cache_breakpoint"
	contract.MaxPromptCacheMarkers = 4
	contract.PromptCacheOptionsField = "prompt_cache_options"
	contract.PromptCacheModes = []string{"implicit", "explicit"}
	contract.PromptCacheTTLs = []string{"30m"}
	return contract
}

var kimiImageMIMETypes = []string{
	"image/png", "image/jpeg", "image/webp", "image/gif",
}

var kimiVideoMIMETypes = []string{
	"video/mp4", "video/mpeg", "video/quicktime", "video/x-msvideo",
	"video/avi", "video/x-flv", "video/webm", "video/x-ms-wmv", "video/3gpp",
}

func qwenChatBaseContract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:        profileID,
		StreamingModeled: true, Streaming: true,
		OutputLimitModeled: true, OutputLimitField: "max_completion_tokens",
		OutputLimitAcceptedFields:          []string{"max_completion_tokens", "max_tokens"},
		OutputLimitFieldsMutuallyExclusive: true,
		OutputLimitMin:                     1, OutputLimitMax: 131_072,
		FunctionToolsModeled:      true,
		ResponseFormatModeled:     true,
		ResponseFormatTypes:       []string{"text"},
		PromptCacheControlModeled: true,
		SearchRequestModeled:      true,
		SearchEnableField:         "enable_search",
		SearchOptionsField:        "search_options",
		SearchStrategyField:       "search_strategy",
		SearchForcedField:         "forced_search",
	}
}

func qwenChatToolsContract(profileID string) ChatProtocolContract {
	contract := qwenChatBaseContract(profileID)
	contract.FunctionToolsModeled = true
	contract.FunctionTools = true
	contract.MaxFunctionTools = 128
	contract.FunctionToolDescriptionRequired = true
	contract.FunctionToolNamePattern = `^[a-zA-Z0-9_-]{1,64}$`
	contract.FunctionToolChoices = []string{"none", "auto", "required"}
	contract.NamedFunctionToolChoice = true
	contract.ParallelFunctionToolCalls = true
	contract.FunctionToolChoiceRequiresTools = true
	contract.ToolStream = true
	contract.ToolStreamField = "tool_stream"
	return contract
}

func qwenChatStructuredOutputContract(profileID string, jsonSchema bool) ChatProtocolContract {
	contract := qwenChatToolsContract(profileID)
	contract.ResponseFormatModeled = true
	contract.ResponseFormatTypes = []string{"text", "json_object"}
	contract.JSONObjectPromptKeyword = "json"
	if jsonSchema {
		contract.ResponseFormatTypes = append(contract.ResponseFormatTypes, "json_schema")
		contract.JSONSchemaStrictRequired = true
		contract.JSONSchemaObjectRequired = true
	}
	return contract
}

func qwenChatCacheContract(profileID string, jsonSchema bool) ChatProtocolContract {
	contract := qwenChatStructuredOutputContract(profileID, jsonSchema)
	contract.PromptCacheControlModeled = true
	contract.ExplicitPromptCache = true
	contract.PromptCacheControlType = "ephemeral"
	contract.MaxPromptCacheMarkers = 4
	return contract
}

func qwenChatMultimodalContract(profileID string, jsonSchema bool) ChatProtocolContract {
	contract := qwenChatCacheContract(profileID, jsonSchema)
	contract.InlineImageDataURL = true
	contract.RemoteImageHTTPSURL = true
	contract.InlineVideoDataURL = true
	contract.RemoteVideoHTTPSURL = true
	contract.InlineImageMIMETypes = []string{"image/jpeg", "image/png"}
	contract.InlineVideoMIMETypes = []string{"video/mp4"}
	return contract
}

func qwenChatSearchContract(profileID string, jsonSchema bool) ChatProtocolContract {
	contract := qwenChatMultimodalContract(profileID, jsonSchema)
	contract.SearchRequestModeled = true
	contract.BuiltinSearchRequest = true
	contract.SearchStrategies = []string{"turbo", "max"}
	contract.SearchDefaultStrategy = "turbo"
	contract.ForcedSearch = true
	return contract
}

func hy3ChatBaseContract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:          profileID,
		StreamingModeled:   true,
		Streaming:          true,
		OutputLimitModeled: true,
		OutputLimitField:   "max_completion_tokens",
		OutputLimitAcceptedFields: []string{
			"max_completion_tokens", "max_tokens",
		},
		OutputLimitMin:        1,
		OutputLimitMax:        131_072,
		FunctionToolsModeled:  true,
		ResponseFormatModeled: true,
		ResponseFormatTypes:   []string{"text"},
	}
}

func hy3ChatToolsContract(profileID string) ChatProtocolContract {
	contract := hy3ChatBaseContract(profileID)
	contract.FunctionTools = true
	// TokenHub does not publish a model-specific tool-count maximum. Keep the
	// executable surface bounded by UniRouter instead of presenting an inferred
	// provider limit as documentation.
	contract.MaxFunctionTools = 64
	contract.FunctionToolNamePattern = `^[a-zA-Z0-9_-]+$`
	contract.FunctionToolChoices = []string{"none", "auto"}
	contract.FunctionToolChoiceRequiresTools = true
	return contract
}

func hy3ChatStructuredOutputContract(profileID string) ChatProtocolContract {
	contract := hy3ChatToolsContract(profileID)
	contract.ResponseFormatTypes = []string{"text", "json_object", "json_schema"}
	contract.JSONObjectPromptKeyword = "json"
	contract.JSONSchemaObjectRequired = true
	contract.JSONSchemaAdditionalPropertiesFalseRequired = true
	return contract
}

func hy3ChatSearchContract(profileID string) ChatProtocolContract {
	contract := hy3ChatStructuredOutputContract(profileID)
	contract.SearchRequestModeled = true
	contract.BuiltinSearchRequest = true
	contract.SearchOptionsField = "web_search_options"
	contract.SearchOptionEnableField = "enable"
	contract.SearchStrategyField = "search_source"
	contract.SearchUserLocation = true
	contract.SearchStrategies = []string{"lite", "standard"}
	contract.SearchDefaultStrategy = "standard"
	return contract
}

func geminiChatBaseContract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:          profileID,
		StreamingModeled:   true,
		Streaming:          true,
		OutputLimitModeled: true,
		OutputLimitField:   "max_completion_tokens",
		OutputLimitAcceptedFields: []string{
			"max_completion_tokens", "max_tokens",
		},
		OutputLimitMin:            1,
		OutputLimitMax:            65_536,
		FunctionToolsModeled:      true,
		ResponseFormatModeled:     true,
		ResponseFormatTypes:       []string{"text"},
		PromptCacheControlModeled: true,
	}
}

func geminiChatToolsContract(profileID string) ChatProtocolContract {
	contract := geminiChatBaseContract(profileID)
	contract.FunctionTools = true
	contract.MaxFunctionTools = 128
	contract.FunctionToolNamePattern = `^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`
	contract.FunctionToolChoices = []string{"auto"}
	contract.FunctionToolChoiceRequiresTools = true
	return contract
}

func geminiChatStructuredOutputContract(profileID string) ChatProtocolContract {
	contract := geminiChatToolsContract(profileID)
	contract.ResponseFormatTypes = []string{"text", "json_schema"}
	contract.JSONSchemaStrictRequired = true
	contract.JSONSchemaObjectRequired = true
	return contract
}

func geminiChatImageContract(profileID string) ChatProtocolContract {
	contract := geminiChatStructuredOutputContract(profileID)
	contract.InlineImageDataURL = true
	contract.InlineImageMIMETypes = []string{"image/jpeg"}
	return contract
}

func geminiChatAudioContract(profileID string) ChatProtocolContract {
	contract := geminiChatImageContract(profileID)
	contract.InlineAudioBase64 = true
	contract.InlineAudioFormats = []string{"wav"}
	return contract
}

func geminiChatProcessingTierContract(profileID string) ChatProtocolContract {
	contract := geminiChatAudioContract(profileID)
	contract.ProcessingTierPolicyID = ProcessingTierPolicyGeminiOpenAICompatibilityV1
	return contract
}

func mimoChatBaseContract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:          profileID,
		StreamingModeled:   true,
		Streaming:          true,
		OutputLimitModeled: true,
		OutputLimitField:   "max_completion_tokens",
		OutputLimitAcceptedFields: []string{
			"max_completion_tokens",
		},
		OutputLimitMin:        1,
		OutputLimitMax:        131_072,
		FunctionToolsModeled:  true,
		ResponseFormatModeled: true,
		ResponseFormatTypes:   []string{"text"},
	}
}

func mimoChatToolsContract(profileID string) ChatProtocolContract {
	contract := mimoChatBaseContract(profileID)
	contract.FunctionTools = true
	contract.MaxFunctionTools = 128
	contract.FunctionToolStrictSupported = true
	contract.FunctionToolNamePattern = `^[a-zA-Z0-9_-]{1,64}$`
	contract.FunctionToolChoices = []string{"auto"}
	contract.FunctionToolChoiceRequiresTools = true
	return contract
}

func mimoChatStructuredOutputContract(profileID string) ChatProtocolContract {
	contract := mimoChatToolsContract(profileID)
	contract.ResponseFormatTypes = []string{"text", "json_object"}
	contract.JSONObjectPromptKeyword = "json"
	return contract
}

func mimoChatMultimodalContract(profileID string) ChatProtocolContract {
	contract := mimoChatStructuredOutputContract(profileID)
	contract.InlineImageDataURL = true
	contract.RemoteImageHTTPSURL = true
	contract.InlineAudioBase64 = true
	contract.RemoteAudioHTTPSURL = true
	contract.InlineVideoDataURL = true
	contract.RemoteVideoHTTPSURL = true
	contract.VideoFPSSupported = true
	contract.VideoFPSMinimum = 0.1
	contract.VideoFPSMaximum = 10.0
	contract.VideoResolutions = []string{"default", "max"}
	return contract
}

func deepSeekChatContract(profileID string, streaming bool) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:                   profileID,
		StreamingModeled:            true,
		Streaming:                   streaming,
		OutputLimitModeled:          true,
		OutputLimitField:            "max_tokens",
		OutputLimitMin:              1,
		OutputLimitMax:              384_000,
		FunctionToolsModeled:        true,
		FunctionTools:               true,
		MaxFunctionTools:            128,
		FunctionToolStrictSupported: true,
		FunctionToolNamePattern:     `^[a-zA-Z0-9_-]{1,64}$`,
		FunctionToolChoices:         []string{"none", "auto", "required"},
		NamedFunctionToolChoice:     true,
		ParallelFunctionToolCalls:   true,
		ResponseFormatModeled:       true,
		ResponseFormatTypes:         []string{"text", "json_object"},
	}
}

func deepSeekV4FlashExactFunctionToolsContract(profileID string) ChatProtocolContract {
	contract := deepSeekChatContract(profileID, true)
	// DeepSeek strict function tools are currently documented on the separate
	// beta origin. This immutable contract is for the standard API origin only.
	contract.FunctionToolStrictSupported = false
	contract.FunctionToolParametersObjectWhenPresent = true
	contract.FunctionToolProviderDefaultChoice = "auto"
	contract.FunctionToolChoicesRequiringTools = []string{"auto", "required", "named"}
	contract.FunctionToolInvocationCapabilitiesModeled = true
	contract.FunctionToolsRequireNonStream = true
	contract.ParallelFunctionToolCalls = false
	contract.ExactFunctionToolHistoryModeled = false
	contract.ExactFunctionToolResponseModeled = true
	return contract
}

func deepSeekV4ExactFunctionToolHistoryContract(profileID string) ChatProtocolContract {
	contract := deepSeekV4FlashExactFunctionToolsContract(profileID)
	contract.ExactFunctionToolHistoryModeled = true
	return contract
}

func kimiK2xChatContract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:        profileID,
		StreamingModeled: true, Streaming: true,
		OutputLimitModeled: true, OutputLimitField: "max_completion_tokens", OutputLimitMin: 1, OutputLimitMax: 262_144,
		FunctionToolsModeled: true, FunctionTools: true, MaxFunctionTools: 128,
		FunctionToolDescriptionRequired: true, FunctionToolParametersRequired: true,
		FunctionToolStrictSupported: true, FunctionToolNamePattern: `^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`,
		FunctionToolChoices: []string{"none", "auto", "required"}, NamedFunctionToolChoice: true,
		ResponseFormatModeled: true, ResponseFormatTypes: []string{"text", "json_object", "json_schema"},
		PartialMode: true, PartialRejectJSONObject: true,
		BuiltinWebSearch: true, BuiltinWebSearchName: "$web_search", BuiltinSearchMeter: "builtin_web_search_calls",
		BuiltinSearchRequiresNonStream: true, BuiltinSearchRequiresSerial: true,
		BuiltinSearchRequiresThinkingDisabled: true, MaxBuiltinSearchCalls: 1,
		InlineImageDataURL: true, InlineVideoDataURL: true,
		InlineImageMIMETypes: kimiImageMIMETypes, InlineVideoMIMETypes: kimiVideoMIMETypes,
	}
}

func kimiK2xExactFunctionToolsContract(profileID string) ChatProtocolContract {
	contract := kimiK2xChatContract(profileID)
	// K2.6 and K2.7 Code accept only none/auto. The generic Kimi Chat schema
	// is broader, but required/named choices are not part of these surfaces.
	contract.FunctionToolChoices = []string{"none", "auto"}
	contract.NamedFunctionToolChoice = false
	contract.FunctionToolProviderDefaultChoice = "auto"
	contract.FunctionToolChoicesRequiringTools = []string{"auto"}
	contract.FunctionToolInvocationCapabilitiesModeled = true
	contract.FunctionToolsRequireNonStream = true
	contract.ExactFunctionToolHistoryModeled = true
	contract.ExactFunctionToolResponseModeled = true
	return contract
}

func kimiK3ChatContract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:        profileID,
		StreamingModeled: true, Streaming: true,
		OutputLimitModeled: true, OutputLimitField: "max_completion_tokens", OutputLimitMin: 1, OutputLimitMax: 1_048_576,
		FunctionToolsModeled: true, FunctionTools: true, MaxFunctionTools: 128,
		FunctionToolDescriptionRequired: true, FunctionToolParametersRequired: true,
		FunctionToolStrictSupported: true, FunctionToolNamePattern: `^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`,
		FunctionToolChoices: []string{"none", "auto", "required"}, NamedFunctionToolChoice: true,
		ResponseFormatModeled: true, ResponseFormatTypes: []string{"text", "json_object", "json_schema"},
		PartialMode: true, PartialRejectJSONObject: true, DynamicSystemTools: true,
		BuiltinWebSearch: true, BuiltinWebSearchName: "$web_search", BuiltinSearchMeter: "builtin_web_search_calls",
		BuiltinSearchRequiresNonStream: true, BuiltinSearchRequiresSerial: true,
		MaxBuiltinSearchCalls: 1,
		InlineImageDataURL:    true, InlineVideoDataURL: true,
		InlineImageMIMETypes: kimiImageMIMETypes, InlineVideoMIMETypes: kimiVideoMIMETypes,
	}
}

func kimiK3ExactTopLevelFunctionToolsContract(profileID string) ChatProtocolContract {
	contract := kimiK3ChatContract(profileID)
	contract.NamedFunctionToolChoice = false
	contract.FunctionToolProviderDefaultChoice = "auto"
	contract.FunctionToolChoicesRequiringTools = []string{"auto", "required"}
	contract.FunctionToolInvocationCapabilitiesModeled = true
	contract.FunctionToolsRequireNonStream = true
	contract.ExactFunctionToolHistoryModeled = true
	contract.ExactFunctionToolResponseModeled = true
	// Dynamic Tools have position-dependent declarations and require their own
	// normalized snapshot contract before they can share exact response proof.
	contract.DynamicSystemTools = false
	return contract
}

func kimiK3ExactDynamicFunctionToolsContract(profileID string) ChatProtocolContract {
	contract := kimiK3ExactTopLevelFunctionToolsContract(profileID)
	// K3 Dynamic Tools use the same exact function definition and request-level
	// tool_choice contract as top-level tools. The gateway derives one final
	// visible-name union after all messages have been validated; message
	// positions remain request semantics and are not copied into route evidence.
	contract.DynamicSystemTools = true
	return contract
}

func miniMaxChatContract(profileID string, streaming bool, outputLimitMax int64) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:                 profileID,
		StreamingModeled:          true,
		Streaming:                 streaming,
		OutputLimitModeled:        true,
		OutputLimitField:          "max_completion_tokens",
		OutputLimitAcceptedFields: []string{"max_completion_tokens", "max_tokens"},
		OutputLimitMin:            1,
		OutputLimitMax:            outputLimitMax,
		FunctionToolsModeled:      true,
		FunctionTools:             true,
		MaxFunctionTools:          128,
		FunctionToolNamePattern:   `^[a-zA-Z0-9_-]{1,64}$`,
		FunctionToolChoices:       []string{"none", "auto"},
		ResponseFormatModeled:     true,
	}
}

func miniMaxM3EmbeddedReasoningChatContract(profileID string) ChatProtocolContract {
	contract := miniMaxChatContract(profileID, true, 524_288)
	contract.FunctionToolsRequireNonStream = true
	contract.AssistantReasoningHistoryModeled = true
	contract.AssistantReasoningHistoryFields = []string{}
	contract.AssistantReasoningResponseModeled = true
	contract.AssistantReasoningResponseFields = []string{}
	contract.AssistantToolResponseReplayField = "content"
	return contract
}

func miniMaxM2EmbeddedReasoningChatContract(profileID string) ChatProtocolContract {
	contract := miniMaxChatContract(profileID, true, 204_800)
	contract.FunctionToolsRequireNonStream = true
	contract.AssistantReasoningHistoryModeled = true
	contract.AssistantReasoningHistoryFields = []string{}
	contract.AssistantReasoningResponseModeled = true
	contract.AssistantReasoningResponseFields = []string{}
	contract.AssistantToolResponseReplayField = "content"
	return contract
}

func miniMaxM2CacheReadChatContract(profileID string) ChatProtocolContract {
	contract := miniMaxM2EmbeddedReasoningChatContract(profileID)
	contract.StreamContract = miniMaxOpenAIUsageStreamContract()
	return contract
}

func miniMaxOpenAIUsageStreamContract() ChatStreamContract {
	return ChatStreamContract{
		RequestOptionsModeled:     true,
		AllowedRequestOptions:     []string{"include_usage"},
		RequireIncludeUsage:       true,
		ResponseModeled:           true,
		RequireTerminalUsage:      true,
		AllowTerminalUsageChoices: true,
		DeltaTextFields:           []string{"content"},
		FinishReasons:             []string{"stop", "length"},
	}
}

func miniMaxM3InlineJPEGContract(profileID string) ChatProtocolContract {
	contract := miniMaxM3EmbeddedReasoningChatContract(profileID)
	contract.InlineImageDataURL = true
	contract.InlineImageMIMETypes = []string{"image/jpeg"}
	contract.ImageInputPolicy = ChatImageInputPolicy{
		Modeled:                     true,
		DecodedFormat:               "jpeg",
		RequireURLObject:            true,
		AllowedMessageRoles:         []string{"user"},
		MaximumImages:               8,
		MaximumDecodedBytes:         256 * 1024,
		MinimumWidth:                15,
		MinimumHeight:               15,
		MinimumPixels:               196,
		MaximumPixels:               131_072,
		MaximumAspectRatio:          150,
		RequiresNonStream:           true,
		IncompatibleRequestFields:   []string{"tools", "tool_choice", "parallel_tool_calls", "functions", "function_call", "tool_stream", "frequency_penalty", "presence_penalty", "n"},
		AdmissionTokenUpperPerPixel: 4,
		AdmissionEstimateVersion:    "chat-inline-jpeg-rgba-pixel-upper.v1",
	}
	return contract
}

func miniMaxM3ProcessingTierContract(profileID string) ChatProtocolContract {
	contract := miniMaxM3InlineJPEGContract(profileID)
	contract.ProcessingTierPolicyID = ProcessingTierPolicyMiniMaxOpenAIChatV1
	return contract
}

func miniMaxM3CacheReadContract(profileID string) ChatProtocolContract {
	contract := miniMaxM3ProcessingTierContract(profileID)
	contract.StreamContract = miniMaxOpenAIUsageStreamContract()
	return contract
}

func miniMaxExactFunctionToolsContract(contract ChatProtocolContract) ChatProtocolContract {
	contract.FunctionToolChoices = nil
	contract.FunctionToolProviderDefaultChoice = "auto"
	contract.FunctionToolParametersRequired = true
	contract.FunctionToolParametersObjectWhenPresent = true
	contract.FunctionToolInvocationCapabilitiesModeled = true
	contract.ExactFunctionToolHistoryModeled = true
	contract.ExactFunctionToolResponseModeled = true
	return contract
}

func miniMaxM3FunctionToolsContract(profileID string) ChatProtocolContract {
	return miniMaxExactFunctionToolsContract(miniMaxM3CacheReadContract(profileID))
}

func miniMaxM2FunctionToolsContract(profileID string) ChatProtocolContract {
	return miniMaxExactFunctionToolsContract(miniMaxM2CacheReadChatContract(profileID))
}

func miniMaxM27StreamFunctionToolsContract(profileID string) ChatProtocolContract {
	contract := miniMaxM2FunctionToolsContract(profileID)
	contract.FunctionToolsRequireNonStream = false
	contract.FunctionToolChoices = []string{"auto"}
	contract.FunctionToolChoicesRequiringTools = []string{"auto"}
	contract.StreamContract.FunctionToolDeltas = true
	contract.StreamContract.FinishReasons = []string{"stop", "length", "tool_calls"}
	return contract
}

func glmOpenAIChatV5Contract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:        profileID,
		StreamingModeled: true, Streaming: true,
		OutputLimitModeled: true, OutputLimitField: "max_tokens", OutputLimitMin: 1, OutputLimitMax: 131_072,
		FunctionToolsModeled: true, FunctionTools: true, MaxFunctionTools: 128,
		FunctionToolDescriptionRequired: true, FunctionToolParametersRequired: true,
		FunctionToolChoices: []string{"auto"}, FunctionToolChoiceRequiresTools: true,
		ResponseFormatModeled: true, ResponseFormatTypes: []string{"text", "json_object"},
	}
}

func glm5VChatV4Contract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:        profileID,
		StreamingModeled: true, Streaming: true,
		OutputLimitModeled: true, OutputLimitField: "max_tokens", OutputLimitMin: 1, OutputLimitMax: 131_072,
		FunctionToolsModeled: true, FunctionTools: true, MaxFunctionTools: 128,
		FunctionToolDescriptionRequired: true, FunctionToolParametersRequired: true,
		FunctionToolChoices: []string{"auto"}, FunctionToolChoiceRequiresTools: true,
		ResponseFormatModeled: true,
		InlineImageDataURL:    true, RemoteImageHTTPSURL: true,
		InlineImageMIMETypes: []string{"image/png", "image/jpeg"},
		RemoteVideoHTTPSURL:  true, RemoteFileHTTPSURL: true,
		RemoteFileExtensions: []string{".txt", ".pdf"}, ExclusiveMediaKinds: true,
	}
}

func glmExactFunctionToolsContract(contract ChatProtocolContract) ChatProtocolContract {
	contract.FunctionToolInvocationCapabilitiesModeled = true
	return contract
}

func doubaoSeed21ChatCoreContract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:                          profileID,
		StreamingModeled:                   true,
		Streaming:                          false,
		OutputLimitModeled:                 true,
		OutputLimitField:                   "max_tokens",
		OutputLimitAcceptedFields:          []string{"max_tokens", "max_completion_tokens"},
		OutputLimitFieldsMutuallyExclusive: true,
		OutputLimitMin:                     1,
		OutputLimitMax:                     262_144,
		OutputLimitFieldMaximums: map[string]int64{
			"max_tokens":            262_144,
			"max_completion_tokens": 65_536,
		},
		FunctionToolsModeled:              true,
		FunctionTools:                     false,
		ResponseFormatModeled:             true,
		ResponseFormatTypes:               []string{"text"},
		AssistantReasoningHistoryModeled:  true,
		AssistantReasoningResponseModeled: true,
		AssistantReasoningResponseFields:  []string{"reasoning_content"},
		OpaqueAssistantMessageFields:      []string{"encrypted_content"},
	}
}

func doubaoSeedEvolvingChatCoreContract(profileID string) ChatProtocolContract {
	return ChatProtocolContract{
		ProfileID:                          profileID,
		StreamingModeled:                   true,
		Streaming:                          false,
		OutputLimitModeled:                 true,
		OutputLimitField:                   "max_tokens",
		OutputLimitAcceptedFields:          []string{"max_tokens", "max_completion_tokens"},
		OutputLimitFieldsMutuallyExclusive: true,
		OutputLimitMin:                     1,
		OutputLimitMax:                     262_144,
		OutputLimitFieldMaximums: map[string]int64{
			"max_tokens":            262_144,
			"max_completion_tokens": 65_536,
		},
		FunctionToolsModeled:              true,
		FunctionTools:                     false,
		ResponseFormatModeled:             true,
		ResponseFormatTypes:               []string{"text"},
		AssistantReasoningHistoryModeled:  true,
		AssistantReasoningResponseModeled: true,
	}
}

func doubaoSeedEvolvingChatReasoningContract(profileID string) ChatProtocolContract {
	contract := doubaoSeedEvolvingChatCoreContract(profileID)
	contract.AssistantReasoningHistoryFields = []string{"reasoning_content"}
	contract.AssistantReasoningResponseFields = []string{"reasoning_content"}
	contract.OpaqueAssistantMessageFields = []string{"encrypted_content"}
	return contract
}

func doubaoSeedEvolvingChatStreamingContract(profileID string) ChatProtocolContract {
	contract := doubaoSeedEvolvingChatReasoningContract(profileID)
	contract.Streaming = true
	contract.StreamContract = doubaoArkReasoningStreamContract()
	return contract
}

func doubaoSeed21ProChatStreamingContract(profileID string) ChatProtocolContract {
	contract := doubaoSeed21ChatCoreContract(profileID)
	contract.Streaming = true
	contract.StreamContract = doubaoArkReasoningStreamContract()
	return contract
}

func doubaoSeed21ProChatToolsContract(profileID string) ChatProtocolContract {
	contract := doubaoSeed21ProChatStreamingContract(profileID)
	contract.FunctionTools = true
	contract.MaxFunctionTools = 128
	contract.FunctionToolParametersObjectWhenPresent = true
	contract.FunctionToolNamePattern = `^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`
	contract.FunctionToolChoices = []string{"none", "auto", "required"}
	contract.NamedFunctionToolChoice = true
	contract.FunctionToolChoicesRequiringTools = []string{"auto", "required", "named"}
	contract.FunctionToolInvocationCapabilitiesModeled = true
	contract.FunctionToolsRequireNonStream = true
	contract.ParallelFunctionToolCalls = true
	contract.ParallelFunctionToolCallsDefaultModeled = true
	contract.ParallelFunctionToolCallsProviderDefault = true
	contract.ExactFunctionToolHistoryModeled = true
	contract.ExactFunctionToolResponseModeled = true
	contract.AssistantOpaqueHistoryModeled = true
	contract.AssistantOpaqueHistoryFields = []string{"encrypted_content"}
	return contract
}

func doubaoSeed21ProChatStrictJSONSchemaContract(profileID string) ChatProtocolContract {
	contract := doubaoSeed21ProChatToolsContract(profileID)
	contract.ResponseFormatModeled = true
	contract.ResponseFormatTypes = []string{"text", "json_schema"}
	contract.JSONSchemaStrictRequired = true
	contract.JSONSchemaObjectRequired = true
	contract.JSONSchemaRequiresNonStream = true
	contract.JSONSchemaDisallowsFunctionTools = true
	contract.JSONSchemaDescriptionStringWhenPresent = true
	contract.JSONSchemaIncompatibleRequestFields = []string{"frequency_penalty", "presence_penalty"}
	return contract
}

func doubaoSeed21ProChatInlineJPEGContract(profileID string) ChatProtocolContract {
	return doubaoArkInlineJPEGContract(doubaoSeed21ProChatStrictJSONSchemaContract(profileID))
}

func doubaoSeed21TurboChatStreamingContract(profileID string) ChatProtocolContract {
	contract := doubaoSeed21ChatCoreContract(profileID)
	contract.Streaming = true
	contract.StreamContract = doubaoArkReasoningStreamContract()
	return contract
}

func doubaoSeed21TurboChatToolsContract(profileID string) ChatProtocolContract {
	contract := doubaoSeed21TurboChatStreamingContract(profileID)
	contract.FunctionTools = true
	contract.MaxFunctionTools = 128
	contract.FunctionToolParametersObjectWhenPresent = true
	contract.FunctionToolNamePattern = `^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`
	contract.FunctionToolChoices = []string{"none", "auto", "required"}
	contract.NamedFunctionToolChoice = true
	contract.FunctionToolChoicesRequiringTools = []string{"auto", "required", "named"}
	contract.FunctionToolInvocationCapabilitiesModeled = true
	contract.FunctionToolsRequireNonStream = true
	contract.ParallelFunctionToolCalls = true
	contract.ParallelFunctionToolCallsDefaultModeled = true
	contract.ParallelFunctionToolCallsProviderDefault = true
	contract.ExactFunctionToolHistoryModeled = true
	contract.ExactFunctionToolResponseModeled = true
	contract.AssistantOpaqueHistoryModeled = true
	contract.AssistantOpaqueHistoryFields = []string{"encrypted_content"}
	return contract
}

func doubaoSeed21TurboChatStrictJSONSchemaContract(profileID string) ChatProtocolContract {
	contract := doubaoSeed21TurboChatToolsContract(profileID)
	contract.ResponseFormatModeled = true
	contract.ResponseFormatTypes = []string{"text", "json_schema"}
	contract.JSONSchemaStrictRequired = true
	contract.JSONSchemaObjectRequired = true
	contract.JSONSchemaRequiresNonStream = true
	contract.JSONSchemaDisallowsFunctionTools = true
	contract.JSONSchemaDescriptionStringWhenPresent = true
	contract.JSONSchemaIncompatibleRequestFields = []string{"frequency_penalty", "presence_penalty"}
	return contract
}

func doubaoSeed21TurboChatInlineJPEGContract(profileID string) ChatProtocolContract {
	return doubaoArkInlineJPEGContract(doubaoSeed21TurboChatStrictJSONSchemaContract(profileID))
}

func doubaoArkReasoningStreamContract() ChatStreamContract {
	return ChatStreamContract{
		RequestOptionsModeled:     true,
		AllowedRequestOptions:     []string{"include_usage"},
		RequireIncludeUsage:       true,
		ResponseModeled:           true,
		RequireDone:               true,
		RequireTerminalUsage:      true,
		DeltaTextFields:           []string{"content", "reasoning_content"},
		DeltaOpaqueFields:         []string{"encrypted_content"},
		FinishReasons:             []string{"stop", "length", "content_filter"},
		ReasoningOpaqueAnswerFlow: true,
	}
}

func doubaoSeedEvolvingChatToolsContract(profileID string) ChatProtocolContract {
	contract := doubaoSeedEvolvingChatStreamingContract(profileID)
	contract.FunctionTools = true
	contract.MaxFunctionTools = 128
	contract.FunctionToolParametersObjectWhenPresent = true
	contract.FunctionToolStrictSupported = true
	contract.FunctionToolNamePattern = `^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`
	contract.FunctionToolChoices = []string{"none", "auto", "required"}
	contract.NamedFunctionToolChoice = true
	contract.FunctionToolChoicesRequiringTools = []string{"auto", "required", "named"}
	contract.FunctionToolInvocationCapabilitiesModeled = true
	contract.FunctionToolsRequireNonStream = true
	contract.ParallelFunctionToolCalls = true
	contract.ParallelFunctionToolCallsDefaultModeled = true
	contract.ParallelFunctionToolCallsProviderDefault = true
	contract.ExactFunctionToolHistoryModeled = true
	contract.ExactFunctionToolResponseModeled = true
	contract.AssistantOpaqueHistoryModeled = true
	contract.AssistantOpaqueHistoryFields = []string{"encrypted_content"}
	return contract
}

func doubaoSeedEvolvingChatStrictJSONSchemaContract(profileID string) ChatProtocolContract {
	contract := doubaoSeedEvolvingChatToolsContract(profileID)
	contract.ResponseFormatModeled = true
	contract.ResponseFormatTypes = []string{"text", "json_schema"}
	contract.JSONSchemaStrictRequired = true
	contract.JSONSchemaObjectRequired = true
	contract.JSONSchemaRequiresNonStream = true
	contract.JSONSchemaDisallowsFunctionTools = true
	contract.JSONSchemaDescriptionStringWhenPresent = true
	contract.JSONSchemaIncompatibleRequestFields = []string{"frequency_penalty", "presence_penalty"}
	return contract
}

func doubaoSeedEvolvingChatInlineJPEGContract(profileID string) ChatProtocolContract {
	return doubaoArkInlineJPEGContract(doubaoSeedEvolvingChatStrictJSONSchemaContract(profileID))
}

func doubaoArkInlineJPEGContract(contract ChatProtocolContract) ChatProtocolContract {
	contract.InlineImageDataURL = true
	contract.InlineImageMIMETypes = []string{"image/jpeg"}
	contract.ImageInputPolicy = ChatImageInputPolicy{
		Modeled:                     true,
		DecodedFormat:               "jpeg",
		RequireURLObject:            true,
		AllowedMessageRoles:         []string{"user"},
		MaximumImages:               8,
		MaximumDecodedBytes:         256 * 1024,
		MinimumWidth:                15,
		MinimumHeight:               15,
		MinimumPixels:               196,
		MaximumPixels:               131_072,
		MaximumAspectRatio:          150,
		RequiresNonStream:           true,
		IncompatibleRequestFields:   []string{"tools", "tool_choice", "parallel_tool_calls", "functions", "function_call", "tool_stream", "frequency_penalty", "presence_penalty", "n"},
		AdmissionTokenUpperPerPixel: 4,
		AdmissionEstimateVersion:    "chat-inline-jpeg-rgba-pixel-upper.v1",
	}
	return contract
}

var chatProtocolContracts = map[string]ChatProtocolContract{
	"protocol.deepseek-openai-chat.v2":              deepSeekChatContract("protocol.deepseek-openai-chat.v2", true),
	"protocol.deepseek-openai-chat.v3":              deepSeekV4FlashExactFunctionToolsContract("protocol.deepseek-openai-chat.v3"),
	"protocol.deepseek-openai-chat.v4":              deepSeekV4ExactFunctionToolHistoryContract("protocol.deepseek-openai-chat.v4"),
	"protocol.deepseek-openai-chat.v5":              deepSeekStreamFunctionToolsContract("protocol.deepseek-openai-chat.v5"),
	"protocol.minimax-openai-chat.v3":               miniMaxChatContract("protocol.minimax-openai-chat.v3", true, 524_288),
	"protocol.minimax-m2-openai-chat.v4":            miniMaxChatContract("protocol.minimax-m2-openai-chat.v4", true, 204_800),
	"protocol.minimax-m2-openai-chat.v5":            miniMaxM2EmbeddedReasoningChatContract("protocol.minimax-m2-openai-chat.v5"),
	"protocol.minimax-m2-openai-chat.v6":            miniMaxM2CacheReadChatContract("protocol.minimax-m2-openai-chat.v6"),
	"protocol.minimax-m2-openai-chat.v7":            miniMaxM2FunctionToolsContract("protocol.minimax-m2-openai-chat.v7"),
	"protocol.minimax-m2-openai-chat.v8":            miniMaxM27StreamFunctionToolsContract("protocol.minimax-m2-openai-chat.v8"),
	"protocol.minimax-m27-openai-chat.v1":           miniMaxM27StreamFunctionToolsContract("protocol.minimax-m27-openai-chat.v1"),
	"protocol.minimax-m3-openai-chat.v4":            miniMaxChatContract("protocol.minimax-m3-openai-chat.v4", true, 524_288),
	"protocol.minimax-m3-openai-chat.v5":            miniMaxM3EmbeddedReasoningChatContract("protocol.minimax-m3-openai-chat.v5"),
	"protocol.minimax-m3-openai-chat.v6":            miniMaxM3InlineJPEGContract("protocol.minimax-m3-openai-chat.v6"),
	"protocol.minimax-m3-openai-chat.v7":            miniMaxM3ProcessingTierContract("protocol.minimax-m3-openai-chat.v7"),
	"protocol.minimax-m3-openai-chat.v8":            miniMaxM3CacheReadContract("protocol.minimax-m3-openai-chat.v8"),
	"protocol.minimax-m3-openai-chat.v9":            miniMaxM3FunctionToolsContract("protocol.minimax-m3-openai-chat.v9"),
	"protocol.minimax-m3-openai-chat.v10":           miniMaxM3StreamFunctionToolsContract("protocol.minimax-m3-openai-chat.v10"),
	"protocol.minimax-m3-openai-chat.v11":           miniMaxM3StreamFunctionToolsContract("protocol.minimax-m3-openai-chat.v11"),
	"protocol.qwen-openai-chat.v1":                  qwenChatBaseContract("protocol.qwen-openai-chat.v1"),
	"protocol.qwen-openai-chat.v2":                  qwenChatToolsContract("protocol.qwen-openai-chat.v2"),
	"protocol.qwen-openai-chat.v3":                  qwenChatStructuredOutputContract("protocol.qwen-openai-chat.v3", true),
	"protocol.qwen-openai-chat.v4":                  qwenChatCacheContract("protocol.qwen-openai-chat.v4", true),
	"protocol.qwen-openai-chat.v5":                  qwenChatMultimodalContract("protocol.qwen-openai-chat.v5", true),
	"protocol.qwen-openai-chat.v6":                  qwenChatSearchContract("protocol.qwen-openai-chat.v6", true),
	"protocol.qwen-openai-chat.flash.v3":            qwenChatStructuredOutputContract("protocol.qwen-openai-chat.flash.v3", false),
	"protocol.qwen-openai-chat.flash.v4":            qwenChatCacheContract("protocol.qwen-openai-chat.flash.v4", false),
	"protocol.qwen-openai-chat.flash.v5":            qwenChatMultimodalContract("protocol.qwen-openai-chat.flash.v5", false),
	"protocol.qwen-openai-chat.flash.v6":            qwenChatSearchContract("protocol.qwen-openai-chat.flash.v6", false),
	"protocol.qwen-openai-chat.flash.v7":            qwenFlashStreamFunctionToolsContract("protocol.qwen-openai-chat.flash.v7"),
	"protocol.qwen-openai-chat.flash.v8":            qwenProviderCompletionContract("protocol.qwen-openai-chat.flash.v8"),
	"protocol.gemini-openai-chat.v1":                geminiChatBaseContract("protocol.gemini-openai-chat.v1"),
	"protocol.gemini-openai-chat.v2":                geminiChatToolsContract("protocol.gemini-openai-chat.v2"),
	"protocol.gemini-openai-chat.v3":                geminiChatStructuredOutputContract("protocol.gemini-openai-chat.v3"),
	"protocol.gemini-openai-chat.v4":                geminiChatImageContract("protocol.gemini-openai-chat.v4"),
	"protocol.gemini-openai-chat.v5":                geminiChatAudioContract("protocol.gemini-openai-chat.v5"),
	"protocol.gemini-openai-chat.v6":                geminiChatProcessingTierContract("protocol.gemini-openai-chat.v6"),
	"protocol.mimo-openai-chat.v1":                  mimoChatBaseContract("protocol.mimo-openai-chat.v1"),
	"protocol.mimo-openai-chat.v2":                  mimoChatToolsContract("protocol.mimo-openai-chat.v2"),
	"protocol.mimo-openai-chat.v3":                  mimoChatStructuredOutputContract("protocol.mimo-openai-chat.v3"),
	"protocol.mimo-openai-chat.v4":                  mimoChatMultimodalContract("protocol.mimo-openai-chat.v4"),
	"protocol.doubao-seed-2.1-pro-openai-chat.v1":   doubaoSeed21ChatCoreContract("protocol.doubao-seed-2.1-pro-openai-chat.v1"),
	"protocol.doubao-seed-2.1-pro-openai-chat.v2":   doubaoSeed21ProChatStreamingContract("protocol.doubao-seed-2.1-pro-openai-chat.v2"),
	"protocol.doubao-seed-2.1-pro-openai-chat.v3":   doubaoSeed21ProChatToolsContract("protocol.doubao-seed-2.1-pro-openai-chat.v3"),
	"protocol.doubao-seed-2.1-pro-openai-chat.v4":   doubaoSeed21ProChatStrictJSONSchemaContract("protocol.doubao-seed-2.1-pro-openai-chat.v4"),
	"protocol.doubao-seed-2.1-pro-openai-chat.v5":   doubaoSeed21ProChatInlineJPEGContract("protocol.doubao-seed-2.1-pro-openai-chat.v5"),
	"protocol.doubao-seed-2.1-turbo-openai-chat.v1": doubaoSeed21ChatCoreContract("protocol.doubao-seed-2.1-turbo-openai-chat.v1"),
	"protocol.doubao-seed-2.1-turbo-openai-chat.v2": doubaoSeed21TurboChatStreamingContract("protocol.doubao-seed-2.1-turbo-openai-chat.v2"),
	"protocol.doubao-seed-2.1-turbo-openai-chat.v3": doubaoSeed21TurboChatToolsContract("protocol.doubao-seed-2.1-turbo-openai-chat.v3"),
	"protocol.doubao-seed-2.1-turbo-openai-chat.v4": doubaoSeed21TurboChatStrictJSONSchemaContract("protocol.doubao-seed-2.1-turbo-openai-chat.v4"),
	"protocol.doubao-seed-2.1-turbo-openai-chat.v5": doubaoSeed21TurboChatInlineJPEGContract("protocol.doubao-seed-2.1-turbo-openai-chat.v5"),
	"protocol.doubao-seed-evolving-openai-chat.v1":  doubaoSeedEvolvingChatCoreContract("protocol.doubao-seed-evolving-openai-chat.v1"),
	"protocol.doubao-seed-evolving-openai-chat.v2":  doubaoSeedEvolvingChatReasoningContract("protocol.doubao-seed-evolving-openai-chat.v2"),
	"protocol.doubao-seed-evolving-openai-chat.v3":  doubaoSeedEvolvingChatStreamingContract("protocol.doubao-seed-evolving-openai-chat.v3"),
	"protocol.doubao-seed-evolving-openai-chat.v4":  doubaoSeedEvolvingChatToolsContract("protocol.doubao-seed-evolving-openai-chat.v4"),
	"protocol.doubao-seed-evolving-openai-chat.v5":  doubaoSeedEvolvingChatStrictJSONSchemaContract("protocol.doubao-seed-evolving-openai-chat.v5"),
	"protocol.doubao-seed-evolving-openai-chat.v6":  doubaoSeedEvolvingChatInlineJPEGContract("protocol.doubao-seed-evolving-openai-chat.v6"),
	"protocol.hy3-openai-chat.v1":                   hy3ChatBaseContract("protocol.hy3-openai-chat.v1"),
	"protocol.hy3-openai-chat.v2":                   hy3ChatToolsContract("protocol.hy3-openai-chat.v2"),
	"protocol.hy3-openai-chat.v3":                   hy3ChatStructuredOutputContract("protocol.hy3-openai-chat.v3"),
	"protocol.hy3-openai-chat.v4":                   hy3ChatSearchContract("protocol.hy3-openai-chat.v4"),
	"protocol.openai-chat.v1":                       openAIChatBaseContract("protocol.openai-chat.v1"),
	"protocol.openai-chat.v2":                       openAIChatToolsContract("protocol.openai-chat.v2"),
	"protocol.openai-chat.v3":                       openAIChatStructuredOutputContract("protocol.openai-chat.v3"),
	"protocol.openai-chat.v4":                       openAIChatVisionContract("protocol.openai-chat.v4"),
	"protocol.openai-chat.v5":                       openAIChatCacheContract("protocol.openai-chat.v5"),
	"protocol.xai-openai-chat.v1": {
		ProfileID:          "protocol.xai-openai-chat.v1",
		StreamingModeled:   true,
		Streaming:          true,
		OutputLimitModeled: true,
		OutputLimitField:   "max_tokens",
		OutputLimitMin:     1,
		// xAI documents no provider text-output limit for grok-4.6. This
		// finite ceiling is UniRouter policy, mirrored by ExecutableMaxOutput,
		// and is intentionally not recorded as ProviderMaxOutput.
		OutputLimitMax:        131_072,
		FunctionToolsModeled:  true,
		FunctionTools:         false,
		ResponseFormatModeled: true,
		ResponseFormatTypes:   []string{"text"},
	},
	"protocol.glm-openai-chat.glm5v.v1": {
		ProfileID:                       "protocol.glm-openai-chat.glm5v.v1",
		StreamingModeled:                true,
		Streaming:                       true,
		OutputLimitModeled:              true,
		OutputLimitField:                "max_tokens",
		OutputLimitMin:                  1,
		OutputLimitMax:                  131_072,
		FunctionToolsModeled:            true,
		FunctionTools:                   true,
		MaxFunctionTools:                128,
		FunctionToolDescriptionRequired: true,
		FunctionToolParametersRequired:  true,
		FunctionToolChoices:             []string{"auto"},
		FunctionToolChoiceRequiresTools: true,
		ResponseFormatModeled:           true,
	},
	"protocol.glm-openai-chat.glm5v.v2": {
		ProfileID:                       "protocol.glm-openai-chat.glm5v.v2",
		StreamingModeled:                true,
		Streaming:                       true,
		OutputLimitModeled:              true,
		OutputLimitField:                "max_tokens",
		OutputLimitMin:                  1,
		OutputLimitMax:                  131_072,
		FunctionToolsModeled:            true,
		FunctionTools:                   true,
		MaxFunctionTools:                128,
		FunctionToolDescriptionRequired: true,
		FunctionToolParametersRequired:  true,
		FunctionToolChoices:             []string{"auto"},
		FunctionToolChoiceRequiresTools: true,
		ResponseFormatModeled:           true,
		InlineImageDataURL:              true,
		RemoteImageHTTPSURL:             true,
		InlineImageMIMETypes:            []string{"image/png", "image/jpeg"},
	},
	"protocol.glm-openai-chat.glm5v.v3": {
		ProfileID:                       "protocol.glm-openai-chat.glm5v.v3",
		StreamingModeled:                true,
		Streaming:                       true,
		OutputLimitModeled:              true,
		OutputLimitField:                "max_tokens",
		OutputLimitMin:                  1,
		OutputLimitMax:                  131_072,
		FunctionToolsModeled:            true,
		FunctionTools:                   true,
		MaxFunctionTools:                128,
		FunctionToolDescriptionRequired: true,
		FunctionToolParametersRequired:  true,
		FunctionToolChoices:             []string{"auto"},
		FunctionToolChoiceRequiresTools: true,
		ResponseFormatModeled:           true,
		InlineImageDataURL:              true,
		RemoteImageHTTPSURL:             true,
		InlineImageMIMETypes:            []string{"image/png", "image/jpeg"},
		RemoteVideoHTTPSURL:             true,
		ExclusiveMediaKinds:             true,
	},
	"protocol.glm-openai-chat.glm5v.v4": glm5VChatV4Contract("protocol.glm-openai-chat.glm5v.v4"),
	"protocol.glm-openai-chat.glm5v.v5": glmExactFunctionToolsContract(glm5VChatV4Contract("protocol.glm-openai-chat.glm5v.v5")),
	"protocol.glm-openai-chat.v5":       glmOpenAIChatV5Contract("protocol.glm-openai-chat.v5"),
	"protocol.glm-openai-chat.v6":       glmExactFunctionToolsContract(glmOpenAIChatV5Contract("protocol.glm-openai-chat.v6")),
	"protocol.glm-openai-chat.v7":       glmStreamFunctionToolsContract(glmOpenAIChatV5Contract("protocol.glm-openai-chat.v7")),
	"protocol.glm-openai-chat.glm5v.v6": glmStreamFunctionToolsContract(glm5VChatV4Contract("protocol.glm-openai-chat.glm5v.v6")),
	"protocol.glm-openai-chat.v4": {
		ProfileID:                       "protocol.glm-openai-chat.v4",
		StreamingModeled:                true,
		Streaming:                       true,
		OutputLimitModeled:              true,
		OutputLimitField:                "max_tokens",
		OutputLimitMin:                  1,
		OutputLimitMax:                  131_072,
		FunctionToolsModeled:            true,
		FunctionTools:                   true,
		MaxFunctionTools:                128,
		FunctionToolDescriptionRequired: true,
		FunctionToolParametersRequired:  true,
		FunctionToolChoices:             []string{"auto"},
		FunctionToolChoiceRequiresTools: true,
	},
	"protocol.glm-openai-chat.v3": {
		ProfileID:          "protocol.glm-openai-chat.v3",
		StreamingModeled:   true,
		Streaming:          true,
		OutputLimitModeled: true,
		OutputLimitField:   "max_tokens",
		OutputLimitMin:     1,
		OutputLimitMax:     131_072,
	},
	"protocol.glm-openai-chat.v2": {
		ProfileID:          "protocol.glm-openai-chat.v2",
		StreamingModeled:   true,
		Streaming:          true,
		OutputLimitModeled: true,
	},
	"protocol.glm-openai-chat.v1": {
		ProfileID:          "protocol.glm-openai-chat.v1",
		StreamingModeled:   true,
		OutputLimitModeled: true,
	},
	"protocol.kimi-openai-chat.v3": {
		ProfileID:               "protocol.kimi-openai-chat.v3",
		PartialMode:             true,
		PartialRejectJSONObject: true,
		BuiltinWebSearch:        true, BuiltinWebSearchName: "$web_search",
		BuiltinSearchMeter:             "builtin_web_search_calls",
		BuiltinSearchRequiresNonStream: true, BuiltinSearchRequiresSerial: true,
		MaxBuiltinSearchCalls: 1,
		InlineImageDataURL:    true, InlineVideoDataURL: true,
		InlineImageMIMETypes: kimiImageMIMETypes, InlineVideoMIMETypes: kimiVideoMIMETypes,
	},
	"protocol.kimi-openai-chat.v2": {
		ProfileID:               "protocol.kimi-openai-chat.v2",
		PartialMode:             true,
		PartialRejectJSONObject: true,
		BuiltinWebSearch:        true, BuiltinWebSearchName: "$web_search",
		BuiltinSearchMeter:             "builtin_web_search_calls",
		BuiltinSearchRequiresNonStream: true, BuiltinSearchRequiresSerial: true,
		BuiltinSearchRequiresThinkingDisabled: true,
		MaxBuiltinSearchCalls:                 1,
		InlineImageDataURL:                    true, InlineVideoDataURL: true,
		InlineImageMIMETypes: kimiImageMIMETypes, InlineVideoMIMETypes: kimiVideoMIMETypes,
	},
	"protocol.kimi-openai-chat.k3.v2": {
		ProfileID:               "protocol.kimi-openai-chat.k3.v2",
		PartialMode:             true,
		PartialRejectJSONObject: true,
		DynamicSystemTools:      true,
		BuiltinWebSearch:        true, BuiltinWebSearchName: "$web_search",
		BuiltinSearchMeter:             "builtin_web_search_calls",
		BuiltinSearchRequiresNonStream: true, BuiltinSearchRequiresSerial: true,
		MaxBuiltinSearchCalls: 1,
		InlineImageDataURL:    true, InlineVideoDataURL: true,
		InlineImageMIMETypes: kimiImageMIMETypes, InlineVideoMIMETypes: kimiVideoMIMETypes,
	},
	"protocol.kimi-openai-chat.v4":    kimiK2xChatContract("protocol.kimi-openai-chat.v4"),
	"protocol.kimi-openai-chat.v5":    kimiK2xExactFunctionToolsContract("protocol.kimi-openai-chat.v5"),
	"protocol.kimi-openai-chat.k3.v3": kimiK3ChatContract("protocol.kimi-openai-chat.k3.v3"),
	"protocol.kimi-openai-chat.k3.v4": kimiK3ExactTopLevelFunctionToolsContract("protocol.kimi-openai-chat.k3.v4"),
	"protocol.kimi-openai-chat.k3.v5": kimiK3ExactDynamicFunctionToolsContract("protocol.kimi-openai-chat.k3.v5"),
	"protocol.kimi-openai-chat.v6":    kimiStreamFunctionToolsContract("protocol.kimi-openai-chat.v6", false),
	"protocol.kimi-openai-chat.k3.v6": kimiStreamFunctionToolsContract("protocol.kimi-openai-chat.k3.v6", true),
}

func ResolveChatProtocolContract(profileID string) (ChatProtocolContract, bool) {
	contract, ok := chatProtocolContracts[strings.TrimSpace(profileID)]
	contract.FunctionToolChoices = slices.Clone(contract.FunctionToolChoices)
	contract.FunctionToolChoicesRequiringTools = slices.Clone(contract.FunctionToolChoicesRequiringTools)
	contract.OutputLimitAcceptedFields = slices.Clone(contract.OutputLimitAcceptedFields)
	contract.OutputLimitFieldMaximums = cloneInt64Map(contract.OutputLimitFieldMaximums)
	contract.ResponseFormatTypes = slices.Clone(contract.ResponseFormatTypes)
	contract.JSONSchemaIncompatibleRequestFields = slices.Clone(contract.JSONSchemaIncompatibleRequestFields)
	contract.AssistantReasoningHistoryFields = slices.Clone(contract.AssistantReasoningHistoryFields)
	contract.AssistantReasoningResponseFields = slices.Clone(contract.AssistantReasoningResponseFields)
	contract.AssistantOpaqueHistoryFields = slices.Clone(contract.AssistantOpaqueHistoryFields)
	contract.OpaqueAssistantMessageFields = slices.Clone(contract.OpaqueAssistantMessageFields)
	contract.StreamContract.AllowedRequestOptions = slices.Clone(contract.StreamContract.AllowedRequestOptions)
	contract.StreamContract.DeltaTextFields = slices.Clone(contract.StreamContract.DeltaTextFields)
	contract.StreamContract.DeltaOpaqueFields = slices.Clone(contract.StreamContract.DeltaOpaqueFields)
	contract.StreamContract.FinishReasons = slices.Clone(contract.StreamContract.FinishReasons)
	contract.SearchStrategies = slices.Clone(contract.SearchStrategies)
	contract.PromptCacheModes = slices.Clone(contract.PromptCacheModes)
	contract.PromptCacheTTLs = slices.Clone(contract.PromptCacheTTLs)
	contract.InlineImageMIMETypes = slices.Clone(contract.InlineImageMIMETypes)
	contract.ImageDetails = slices.Clone(contract.ImageDetails)
	contract.ImageInputPolicy.AllowedMessageRoles = slices.Clone(contract.ImageInputPolicy.AllowedMessageRoles)
	contract.ImageInputPolicy.IncompatibleRequestFields = slices.Clone(contract.ImageInputPolicy.IncompatibleRequestFields)
	contract.InlineAudioFormats = slices.Clone(contract.InlineAudioFormats)
	contract.InlineVideoMIMETypes = slices.Clone(contract.InlineVideoMIMETypes)
	contract.VideoResolutions = slices.Clone(contract.VideoResolutions)
	contract.RemoteFileExtensions = slices.Clone(contract.RemoteFileExtensions)
	return contract, ok
}

func validateChatProtocolContractShape(contract ChatProtocolContract) error {
	if contract.OutputEnforcement != "" {
		if contract.OutputEnforcement != OutputEnforcementProviderCompletionV1 ||
			!contract.OutputLimitModeled || contract.OutputLimitField != "max_completion_tokens" ||
			len(contract.OutputLimitAcceptedFields) != 1 || contract.OutputLimitAcceptedFields[0] != "max_completion_tokens" ||
			contract.OutputLimitMin != 11 {
			return fmt.Errorf("unsupported output enforcement contract")
		}
	}
	if contract.FunctionToolProviderDefaultChoice != "" {
		if !contract.FunctionToolsModeled || !contract.FunctionTools {
			return fmt.Errorf("function-tool provider default requires modeled function tools")
		}
		if !slices.Contains([]string{"none", "auto", "required"}, contract.FunctionToolProviderDefaultChoice) {
			return fmt.Errorf("invalid function-tool provider default %q", contract.FunctionToolProviderDefaultChoice)
		}
	}
	accepted := slices.Clone(contract.OutputLimitAcceptedFields)
	if len(accepted) == 0 && contract.OutputLimitField != "" {
		accepted = []string{contract.OutputLimitField}
	}
	for field, maximum := range contract.OutputLimitFieldMaximums {
		if !slices.Contains(accepted, field) {
			return fmt.Errorf("output field maximum %q is not an accepted output field", field)
		}
		if maximum < contract.OutputLimitMin || maximum > contract.OutputLimitMax {
			return fmt.Errorf("output field maximum %q=%d is outside the protocol range", field, maximum)
		}
	}
	if !contract.AssistantReasoningResponseModeled && len(contract.AssistantReasoningResponseFields) > 0 {
		return fmt.Errorf("assistant reasoning response fields require an exact modeled response contract")
	}
	seenReasoningResponse := make(map[string]struct{}, len(contract.AssistantReasoningResponseFields))
	for _, rawField := range contract.AssistantReasoningResponseFields {
		field := strings.TrimSpace(rawField)
		if field != rawField || !slices.Contains([]string{"reasoning_content", "reasoning"}, field) {
			return fmt.Errorf("invalid assistant reasoning response field %q", rawField)
		}
		if _, duplicate := seenReasoningResponse[field]; duplicate {
			return fmt.Errorf("duplicate assistant reasoning response field %q", field)
		}
		seenReasoningResponse[field] = struct{}{}
	}
	seenOpaque := make(map[string]struct{}, len(contract.OpaqueAssistantMessageFields))
	for _, rawField := range contract.OpaqueAssistantMessageFields {
		field := strings.TrimSpace(rawField)
		if field == "" || field != rawField || strings.ContainsAny(field, "\x00\r\n") {
			return fmt.Errorf("invalid opaque assistant response field %q", rawField)
		}
		if slices.Contains([]string{"role", "content", "refusal", "reasoning_content", "reasoning", "function_call", "tool_calls"}, field) {
			return fmt.Errorf("text-bearing assistant response field %q cannot be opaque", field)
		}
		if _, duplicate := seenOpaque[field]; duplicate {
			return fmt.Errorf("duplicate opaque assistant response field %q", field)
		}
		seenOpaque[field] = struct{}{}
	}
	if !contract.AssistantOpaqueHistoryModeled && len(contract.AssistantOpaqueHistoryFields) > 0 {
		return fmt.Errorf("assistant opaque history fields require an exact modeled request contract")
	}
	seenOpaqueHistory := make(map[string]struct{}, len(contract.AssistantOpaqueHistoryFields))
	for _, rawField := range contract.AssistantOpaqueHistoryFields {
		field := strings.TrimSpace(rawField)
		if field == "" || field != rawField || strings.ContainsAny(field, "\x00\r\n") {
			return fmt.Errorf("invalid opaque assistant history field %q", rawField)
		}
		if slices.Contains([]string{"role", "content", "refusal", "reasoning_content", "reasoning", "function_call", "tool_calls"}, field) {
			return fmt.Errorf("structured assistant history field %q cannot be opaque", field)
		}
		if _, duplicate := seenOpaqueHistory[field]; duplicate {
			return fmt.Errorf("duplicate opaque assistant history field %q", field)
		}
		seenOpaqueHistory[field] = struct{}{}
	}
	for _, choice := range contract.FunctionToolChoicesRequiringTools {
		if choice != "named" && !slices.Contains(contract.FunctionToolChoices, choice) {
			return fmt.Errorf("tool choice %q requiring tools is not accepted", choice)
		}
		if choice == "named" && !contract.NamedFunctionToolChoice {
			return fmt.Errorf("named tool choice cannot require tools when named choice is unsupported")
		}
	}
	if contract.ParallelFunctionToolCallsDefaultModeled && !contract.ParallelFunctionToolCalls {
		return fmt.Errorf("parallel tool-call default requires modeled parallel function tools")
	}
	if contract.ExactFunctionToolResponseModeled && !contract.FunctionTools {
		return fmt.Errorf("exact function-tool response requires function tools")
	}
	if contract.ExactFunctionToolHistoryModeled && !contract.FunctionTools {
		return fmt.Errorf("exact function-tool history requires function tools")
	}
	if contract.FunctionToolInvocationCapabilitiesModeled && !contract.FunctionTools {
		return fmt.Errorf("typed function-tool invocation capabilities require function tools")
	}
	jsonSchemaModeled := contract.ResponseFormatModeled && slices.Contains(contract.ResponseFormatTypes, "json_schema")
	if (contract.JSONSchemaStrictRequired || contract.JSONSchemaObjectRequired || contract.JSONSchemaRequiresNonStream ||
		contract.JSONSchemaDisallowsFunctionTools || contract.JSONSchemaDescriptionStringWhenPresent ||
		len(contract.JSONSchemaIncompatibleRequestFields) > 0) && !jsonSchemaModeled {
		return fmt.Errorf("JSON Schema constraints require a modeled json_schema response format")
	}
	seenJSONSchemaIncompatibleFields := make(map[string]struct{}, len(contract.JSONSchemaIncompatibleRequestFields))
	for _, rawField := range contract.JSONSchemaIncompatibleRequestFields {
		field := strings.TrimSpace(rawField)
		if field == "" || field != rawField || strings.ContainsAny(field, "\x00\r\n") {
			return fmt.Errorf("invalid JSON Schema incompatible request field %q", rawField)
		}
		if _, duplicate := seenJSONSchemaIncompatibleFields[field]; duplicate {
			return fmt.Errorf("duplicate JSON Schema incompatible request field %q", field)
		}
		seenJSONSchemaIncompatibleFields[field] = struct{}{}
	}
	stream := contract.StreamContract
	if (stream.RequestOptionsModeled || stream.ResponseModeled) && (!contract.StreamingModeled || !contract.Streaming) {
		return fmt.Errorf("exact stream contract requires modeled streaming support")
	}
	if stream.RequireIncludeUsage && (!stream.RequestOptionsModeled || !slices.Contains(stream.AllowedRequestOptions, "include_usage")) {
		return fmt.Errorf("required stream usage must be an allowed modeled request option")
	}
	if (stream.RequireDone || stream.RequireTerminalUsage || stream.ReasoningOpaqueAnswerFlow) && !stream.ResponseModeled {
		return fmt.Errorf("stream response invariants require an exact modeled response contract")
	}
	if stream.RequireTerminalUsage && !stream.RequireIncludeUsage {
		return fmt.Errorf("required terminal stream usage must be requested from the provider")
	}
	if stream.AllowTerminalUsageChoices && (!stream.ResponseModeled || !stream.RequireTerminalUsage) {
		return fmt.Errorf("terminal usage with choices requires an exact terminal-usage contract")
	}
	for label, values := range map[string][]string{
		"stream request option":     stream.AllowedRequestOptions,
		"stream delta text field":   stream.DeltaTextFields,
		"stream delta opaque field": stream.DeltaOpaqueFields,
		"stream finish reason":      stream.FinishReasons,
	} {
		seen := make(map[string]struct{}, len(values))
		for _, raw := range values {
			value := strings.TrimSpace(raw)
			if value == "" || value != raw || strings.ContainsAny(value, "\x00\r\n") {
				return fmt.Errorf("invalid %s %q", label, raw)
			}
			if _, duplicate := seen[value]; duplicate {
				return fmt.Errorf("duplicate %s %q", label, value)
			}
			seen[value] = struct{}{}
		}
	}
	if stream.ReasoningOpaqueAnswerFlow &&
		(!slices.Contains(stream.DeltaTextFields, "content") ||
			!slices.Contains(stream.DeltaTextFields, "reasoning_content") ||
			!slices.Contains(stream.DeltaOpaqueFields, "encrypted_content")) {
		return fmt.Errorf("reasoning/opaque/answer stream flow requires content, reasoning_content and encrypted_content")
	}
	image := contract.ImageInputPolicy
	imageConfigured := image.DecodedFormat != "" || image.RequireURLObject || len(image.AllowedMessageRoles) > 0 || image.MaximumImages != 0 || image.MaximumDecodedBytes != 0 ||
		image.MinimumWidth != 0 || image.MinimumHeight != 0 || image.MinimumPixels != 0 || image.MaximumPixels != 0 ||
		image.MaximumAspectRatio != 0 || image.RequiresNonStream || len(image.IncompatibleRequestFields) > 0 ||
		image.AdmissionTokenUpperPerPixel != 0 || image.AdmissionEstimateVersion != ""
	if !image.Modeled && imageConfigured {
		return fmt.Errorf("image input constraints require an exact modeled image policy")
	}
	if image.Modeled {
		if !contract.InlineImageDataURL || len(contract.InlineImageMIMETypes) == 0 {
			return fmt.Errorf("modeled image policy requires inline image transport and exact MIME types")
		}
		if contract.RemoteImageHTTPSURL {
			return fmt.Errorf("modeled inline image policy cannot advertise remote image URLs")
		}
		if image.DecodedFormat != "jpeg" {
			return fmt.Errorf("modeled image policy requires the supported decoded format jpeg")
		}
		if len(image.AllowedMessageRoles) == 0 || image.MaximumImages <= 0 || image.MaximumDecodedBytes <= 0 || image.MinimumWidth <= 0 ||
			image.MinimumHeight <= 0 || image.MinimumPixels <= 0 || image.MaximumPixels < image.MinimumPixels ||
			image.MaximumAspectRatio <= 0 || image.AdmissionTokenUpperPerPixel <= 0 ||
			strings.TrimSpace(image.AdmissionEstimateVersion) == "" {
			return fmt.Errorf("modeled image policy requires bounded dimensions, bytes, pixels, aspect ratio, and admission authority")
		}
		for label, values := range map[string][]string{
			"image message role":               image.AllowedMessageRoles,
			"image incompatible request field": image.IncompatibleRequestFields,
		} {
			seen := make(map[string]struct{}, len(values))
			for _, raw := range values {
				value := strings.TrimSpace(raw)
				if value == "" || value != raw || strings.ContainsAny(value, "\x00\r\n") {
					return fmt.Errorf("invalid %s %q", label, raw)
				}
				if _, duplicate := seen[value]; duplicate {
					return fmt.Errorf("duplicate %s %q", label, value)
				}
				seen[value] = struct{}{}
			}
		}
	}
	return nil
}

// PreProfileChatProtocolContract is the least restrictive measurable union of
// immutable chat contracts. It is used only before routing, where the gateway
// must bound the raw request without yet knowing the selected profile. The
// selected profile remains the semantic authority and validates the original
// body again before Freeze and provider dispatch.
func PreProfileChatProtocolContract() ChatProtocolContract {
	union := ChatProtocolContract{ProfileID: "protocol.preprofile.chat.v2"}
	for _, contract := range chatProtocolContracts {
		union.PartialMode = union.PartialMode || contract.PartialMode
		union.DynamicSystemTools = union.DynamicSystemTools || contract.DynamicSystemTools
		if contract.AssistantOpaqueHistoryModeled {
			union.AssistantOpaqueHistoryModeled = true
			union.AssistantOpaqueHistoryFields = appendUniqueStrings(
				union.AssistantOpaqueHistoryFields, contract.AssistantOpaqueHistoryFields...,
			)
		}
		if contract.BuiltinWebSearch {
			union.BuiltinWebSearch = true
			if union.BuiltinWebSearchName == "" {
				union.BuiltinWebSearchName = contract.BuiltinWebSearchName
			}
			if union.BuiltinSearchMeter == "" {
				union.BuiltinSearchMeter = contract.BuiltinSearchMeter
			}
			if contract.MaxBuiltinSearchCalls > union.MaxBuiltinSearchCalls {
				union.MaxBuiltinSearchCalls = contract.MaxBuiltinSearchCalls
			}
		}
		union.InlineImageDataURL = union.InlineImageDataURL || contract.InlineImageDataURL
		union.RemoteImageHTTPSURL = union.RemoteImageHTTPSURL || contract.RemoteImageHTTPSURL
		union.InlineAudioBase64 = union.InlineAudioBase64 || contract.InlineAudioBase64
		union.RemoteAudioHTTPSURL = union.RemoteAudioHTTPSURL || contract.RemoteAudioHTTPSURL
		union.InlineVideoDataURL = union.InlineVideoDataURL || contract.InlineVideoDataURL
		union.RemoteVideoHTTPSURL = union.RemoteVideoHTTPSURL || contract.RemoteVideoHTTPSURL
		union.RemoteFileHTTPSURL = union.RemoteFileHTTPSURL || contract.RemoteFileHTTPSURL
		union.ImageDetails = appendUniqueStrings(union.ImageDetails, contract.ImageDetails...)
		union.VideoResolutions = appendUniqueStrings(union.VideoResolutions, contract.VideoResolutions...)
		if contract.VideoFPSSupported {
			union.VideoFPSSupported = true
			if union.VideoFPSMinimum == 0 || contract.VideoFPSMinimum < union.VideoFPSMinimum {
				union.VideoFPSMinimum = contract.VideoFPSMinimum
			}
			if contract.VideoFPSMaximum > union.VideoFPSMaximum {
				union.VideoFPSMaximum = contract.VideoFPSMaximum
			}
		}
		if contract.PromptCacheBreakpointField != "" {
			union.PromptCacheControlModeled = true
			union.ExplicitPromptCache = true
			union.PromptCacheBreakpointField = contract.PromptCacheBreakpointField
			if contract.MaxPromptCacheMarkers > union.MaxPromptCacheMarkers {
				union.MaxPromptCacheMarkers = contract.MaxPromptCacheMarkers
			}
		}
	}
	sort.Strings(union.ImageDetails)
	sort.Strings(union.VideoResolutions)
	sort.Strings(union.AssistantOpaqueHistoryFields)
	return union
}

// PreProfileRequestExtensionFields derives the provider root extensions that
// the generic admission grammar does not own. Only their measurable JSON wire
// type is checked before routing; the selected immutable profile later applies
// exact ranges, combinations, and provider semantics to the original body.
func PreProfileRequestExtensionFields() []RequestExtensionField {
	fields := make(map[string]RequestExtensionWireType)
	add := func(field string, wireType RequestExtensionWireType) {
		field = strings.TrimSpace(field)
		if field != "" {
			fields[field] = wireType
		}
	}
	for _, contract := range chatProtocolContracts {
		add(contract.ToolStreamField, RequestExtensionBoolean)
		add(contract.SearchEnableField, RequestExtensionBoolean)
		add(contract.SearchOptionsField, RequestExtensionObject)
		add(contract.PromptCacheOptionsField, RequestExtensionObject)
	}
	for _, contract := range reasoningContracts {
		add(contract.BudgetControl.Field, RequestExtensionInteger)
		add(contract.PreserveThinkingField, RequestExtensionBoolean)
		add(contract.SplitOutputField, RequestExtensionBoolean)
	}
	result := make([]RequestExtensionField, 0, len(fields))
	for field, wireType := range fields {
		result = append(result, RequestExtensionField{Field: field, WireType: wireType})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Field < result[right].Field })
	return result
}

func appendUniqueStrings(destination []string, values ...string) []string {
	for _, value := range values {
		if value != "" && !slices.Contains(destination, value) {
			destination = append(destination, value)
		}
	}
	return destination
}
