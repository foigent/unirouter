package gateway

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

type modelExecutionProfileEvidence struct {
	ExecutionProfileID            string
	ExecutionProfileVersion       string
	ReasoningProfile              string
	ReasoningMode                 string
	ReasoningRequest              string
	ReasoningProviderDefault      string
	ReasoningDefaultAuthority     string
	ReasoningToggleRequest        string
	ReasoningToggleEffective      string
	ReasoningThinkingKeepRequest  string
	ReasoningThinkingKeepDefault  string
	ReasoningThinkingClearRequest string
	ReasoningThinkingClearDefault string
	ReasoningEffortRequest        string
	ReasoningEffortEffective      string
	ReasoningTimeoutSeconds       int
	ReasoningBudgetRequest        string
	ReasoningBudgetDefault        string
	ReasoningPreserveRequest      string
	ReasoningPreserveDefault      string
	ReasoningSplitRequest         string
	ReasoningSplitDefault         string
	CacheUsageProfile             string
	ModelProtocolProfile          string
	ProcessingTierPolicy          string
	ProcessingTierRequest         string
	ProcessingTierEffective       string
	ProcessingTierTimeoutSeconds  int
	ProcessingTierExplicit        bool
	ProcessingTierFieldPresent    bool
	ReservedMeterUsage            []metering.MeterQuantity
	ProviderRequestNormalizations []string
	FunctionToolInvocationModeled bool
	FunctionToolsRequested        bool
	FunctionToolChoiceRequest     string
	FunctionToolChoiceEffective   string
	FunctionToolChoiceOrigin      string
	DeclaredFunctionNames         []string
	SelectedFunctionName          string
	ParallelToolCallsRequest      string
	ParallelToolCallsEffective    bool
	ParallelToolCallsOrigin       string
	ToolStreamRequested           bool
	SearchRequested               bool
	PartialModeRequested          bool
	DynamicToolsRequested         bool
	BuiltinWebSearchRequested     bool
	ImageRequested                bool
	AudioRequested                bool
	VideoRequested                bool
	FilesRequested                bool
	ImageCount                    int
	ImageAdmissionTokenUpper      int64
	ImageAdmissionEstimateVersion string
	AdmissionShadowBody           []byte
	admissionBodyHash             [sha256.Size]byte
}

func (e *modelExecutionProfileEvidence) bindAdmissionBody(body []byte) {
	e.admissionBodyHash = sha256.Sum256(body)
}

func (e modelExecutionProfileEvidence) matchesAdmissionBody(body []byte) bool {
	return e.admissionBodyHash != [sha256.Size]byte{} && e.admissionBodyHash == sha256.Sum256(body)
}

func (e modelExecutionProfileEvidence) metadata() map[string]interface{} {
	return map[string]interface{}{
		"execution_profile_id":             e.ExecutionProfileID,
		"execution_profile_version":        e.ExecutionProfileVersion,
		"reasoning_profile":                e.ReasoningProfile,
		"reasoning_mode":                   e.ReasoningMode,
		"reasoning_request":                e.ReasoningRequest,
		"reasoning_provider_default":       e.ReasoningProviderDefault,
		"reasoning_default_authority":      e.ReasoningDefaultAuthority,
		"reasoning_toggle_request":         e.ReasoningToggleRequest,
		"reasoning_toggle_effective":       e.ReasoningToggleEffective,
		"reasoning_thinking_keep_request":  e.ReasoningThinkingKeepRequest,
		"reasoning_thinking_keep_default":  e.ReasoningThinkingKeepDefault,
		"reasoning_thinking_clear_request": e.ReasoningThinkingClearRequest,
		"reasoning_thinking_clear_default": e.ReasoningThinkingClearDefault,
		"reasoning_effort_request":         e.ReasoningEffortRequest,
		"reasoning_effort_effective":       e.ReasoningEffortEffective,
		"reasoning_timeout_seconds":        e.ReasoningTimeoutSeconds,
		"reasoning_budget_request":         e.ReasoningBudgetRequest,
		"reasoning_budget_default":         e.ReasoningBudgetDefault,
		"reasoning_preserve_request":       e.ReasoningPreserveRequest,
		"reasoning_preserve_default":       e.ReasoningPreserveDefault,
		"reasoning_split_request":          e.ReasoningSplitRequest,
		"reasoning_split_default":          e.ReasoningSplitDefault,
		"cache_usage_profile":              e.CacheUsageProfile,
		"model_protocol_profile":           e.ModelProtocolProfile,
		"processing_tier_policy":           e.ProcessingTierPolicy,
		"processing_tier_request":          e.ProcessingTierRequest,
		"processing_tier_effective":        e.ProcessingTierEffective,
		"processing_tier_timeout_seconds":  e.ProcessingTierTimeoutSeconds,
		"processing_tier_explicit":         e.ProcessingTierExplicit,
		"processing_tier_field_present":    e.ProcessingTierFieldPresent,
		"reserved_meter_usage":             e.ReservedMeterUsage,
		"provider_request_normalizations":  strings.Join(e.ProviderRequestNormalizations, ","),
		"function_tool_invocation_modeled": e.FunctionToolInvocationModeled,
		"function_tools_requested":         e.FunctionToolsRequested,
		"function_tool_choice_request":     e.FunctionToolChoiceRequest,
		"function_tool_choice_effective":   e.FunctionToolChoiceEffective,
		"function_tool_choice_origin":      e.FunctionToolChoiceOrigin,
		"parallel_tool_calls_request":      e.ParallelToolCallsRequest,
		"parallel_tool_calls_effective":    e.ParallelToolCallsEffective,
		"parallel_tool_calls_origin":       e.ParallelToolCallsOrigin,
		"tool_stream_requested":            e.ToolStreamRequested,
		"search_requested":                 e.SearchRequested,
		"partial_mode_requested":           e.PartialModeRequested,
		"dynamic_tools_requested":          e.DynamicToolsRequested,
		"builtin_web_search_requested":     e.BuiltinWebSearchRequested,
		"image_requested":                  e.ImageRequested,
		"audio_requested":                  e.AudioRequested,
		"video_requested":                  e.VideoRequested,
		"files_requested":                  e.FilesRequested,
		"image_count":                      e.ImageCount,
		"image_admission_token_upper":      e.ImageAdmissionTokenUpper,
		"image_admission_estimate_version": e.ImageAdmissionEstimateVersion,
	}
}

// validateChatExecutionProfileRequest applies the immutable provider component
// contract selected by the planner. It is one stage of executable validation,
// not a standalone root-field allowlist: prepareModelExecutionRoutes must next
// apply the generic admission grammar before secret decryption, Freeze, attempt
// creation, coordinator authorization, or provider I/O.
func validateChatExecutionProfileRequest(body []byte, plan modelrouting.Plan) (modelExecutionProfileEvidence, error) {
	profile, err := plan.ExecutionProfile()
	if err != nil {
		return modelExecutionProfileEvidence{}, err
	}
	if plan.ReasoningProfile != profile.ReasoningProfile ||
		plan.CacheUsageProfile != profile.CacheUsageProfile ||
		plan.ModelProtocolProfile != profile.ProtocolProfile {
		return modelExecutionProfileEvidence{}, fmt.Errorf("model execution component identity drift")
	}

	evidence := modelExecutionProfileEvidence{
		ExecutionProfileID: plan.ExecutionProfileID, ExecutionProfileVersion: plan.ExecutionProfileVersion,
		ReasoningProfile: plan.ReasoningProfile, CacheUsageProfile: plan.CacheUsageProfile,
		ModelProtocolProfile: plan.ModelProtocolProfile,
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return modelExecutionProfileEvidence{}, fmt.Errorf("chat request must be an object")
	}
	_, evidence.ProcessingTierFieldPresent = envelope["service_tier"]
	if protocolContract, modeled := modelprofile.ResolveChatProtocolContract(plan.ModelProtocolProfile); modeled {
		validation, err := validateAdvancedChatProtocolRequestWithAuthority(body, protocolContract, profile.LegacyProtocolAuthority)
		if err != nil {
			return modelExecutionProfileEvidence{}, err
		}
		evidence.ReservedMeterUsage = slices.Clone(validation.ReservedMeterUsage)
		evidence.ProcessingTierPolicy = validation.ProcessingTierPolicy
		evidence.ProcessingTierRequest = validation.ProcessingTierRequest
		evidence.ProcessingTierEffective = validation.ProcessingTierEffective
		evidence.ProcessingTierTimeoutSeconds = validation.ProcessingTierTimeoutSeconds
		evidence.ProcessingTierExplicit = validation.ProcessingTierExplicit
		evidence.ProviderRequestNormalizations = append(
			evidence.ProviderRequestNormalizations,
			validation.ProviderRequestNormalizations...,
		)
		evidence.FunctionToolInvocationModeled = validation.FunctionToolInvocationModeled
		evidence.FunctionToolsRequested = validation.FunctionToolsRequested
		evidence.FunctionToolChoiceRequest = validation.FunctionToolChoiceRequest
		evidence.FunctionToolChoiceEffective = validation.FunctionToolChoiceEffective
		evidence.FunctionToolChoiceOrigin = validation.FunctionToolChoiceOrigin
		evidence.DeclaredFunctionNames = slices.Clone(validation.DeclaredFunctionNames)
		evidence.SelectedFunctionName = validation.SelectedFunctionName
		evidence.ParallelToolCallsRequest = validation.ParallelToolCallsRequest
		evidence.ParallelToolCallsEffective = validation.ParallelToolCallsEffective
		evidence.ParallelToolCallsOrigin = validation.ParallelToolCallsOrigin
		evidence.ToolStreamRequested = validation.ToolStreamRequested
		evidence.SearchRequested = validation.SearchRequested
		evidence.PartialModeRequested = validation.PartialModeRequested
		evidence.DynamicToolsRequested = validation.DynamicToolsRequested
		evidence.BuiltinWebSearchRequested = validation.BuiltinWebSearchRequested
		evidence.ImageRequested = validation.ImageRequested
		evidence.AudioRequested = validation.AudioRequested
		evidence.VideoRequested = validation.VideoRequested
		evidence.FilesRequested = validation.FilesRequested
		evidence.ImageCount = validation.ImageCount
		evidence.ImageAdmissionTokenUpper = validation.ImageAdmissionTokenUpper
		evidence.ImageAdmissionEstimateVersion = validation.ImageAdmissionEstimateVersion
		evidence.AdmissionShadowBody = slices.Clone(validation.SanitizedBody)
	}
	streamNormalization, err := validateCurrentKimiChatStreamUsageRequest(envelope, plan)
	if err != nil {
		return modelExecutionProfileEvidence{}, err
	}
	if streamNormalization != "" && !slices.Contains(evidence.ProviderRequestNormalizations, streamNormalization) {
		evidence.ProviderRequestNormalizations = append(evidence.ProviderRequestNormalizations, streamNormalization)
	}

	contract, modeled := modelprofile.ResolveReasoningContract(plan.ReasoningProfile)
	if !modeled {
		if hasAnyNonNullJSONField(envelope, "thinking", "enable_thinking", "reasoning_effort", "reasoning", "thinking_budget", "preserve_thinking", "reasoning_split") {
			return modelExecutionProfileEvidence{}, fmt.Errorf("reasoning control is not supported by profile %s", plan.ReasoningProfile)
		}
		return evidence, nil
	}
	evidence.ReasoningMode = contract.Mode
	evidence.ReasoningProviderDefault = reasoningControlSummary(
		contract.ToggleControl.ProviderDefault,
		contract.EffortControl.ProviderDefault,
	)
	evidence.ReasoningDefaultAuthority = contract.DefaultAuthority

	reasoningActive := contract.Mode == modelprofile.ReasoningModeAlwaysOn || contract.Mode == modelprofile.ReasoningModeRequired
	for _, field := range []string{"thinking", "enable_thinking", "reasoning_effort", "reasoning", "thinking_budget", "preserve_thinking", "reasoning_split"} {
		if hasNonNullJSONField(envelope, field) &&
			field != contract.ToggleControl.Field && field != contract.EffortControl.Field &&
			field != contract.BudgetControl.Field && field != contract.PreserveThinkingField &&
			field != contract.SplitOutputField {
			return modelExecutionProfileEvidence{}, fmt.Errorf("profile %s does not accept %s", plan.ReasoningProfile, field)
		}
	}
	if contract.ToggleAndEffortMutuallyExclusive &&
		hasNonNullJSONField(envelope, contract.ToggleControl.Field) &&
		hasNonNullJSONField(envelope, contract.EffortControl.Field) {
		return modelExecutionProfileEvidence{}, fmt.Errorf("%s and %s cannot be set together for profile %s", contract.ToggleControl.Field, contract.EffortControl.Field, plan.ReasoningProfile)
	}

	toggleRequest := ""
	if contract.ToggleControl.Field != "" {
		value, effective, present, err := reasoningControlValue(envelope[contract.ToggleControl.Field], contract.ToggleControl)
		if err != nil {
			return modelExecutionProfileEvidence{}, err
		}
		toggleRequest = "omitted"
		evidence.ReasoningToggleEffective = "omitted"
		reasoningActive = reasoningControlProviderDefaultActive(contract.ToggleControl)
		if present {
			toggleRequest = value
			evidence.ReasoningToggleEffective = effective
			reasoningActive = effective != "disabled"
		}
		evidence.ReasoningToggleRequest = toggleRequest
		if contract.ToggleControl.Field == "thinking" {
			keep, keepPresent, keepErr := optionalThinkingKeep(envelope[contract.ToggleControl.Field])
			if keepErr != nil {
				return modelExecutionProfileEvidence{}, keepErr
			}
			if keepPresent && len(contract.ThinkingKeepValues) == 0 {
				return modelExecutionProfileEvidence{}, fmt.Errorf("thinking.keep is not supported by profile %s", plan.ReasoningProfile)
			}
			if len(contract.ThinkingKeepValues) > 0 {
				evidence.ReasoningThinkingKeepRequest = "omitted"
				evidence.ReasoningThinkingKeepDefault = contract.ThinkingKeepProviderDefault
				if keepPresent {
					if !slices.Contains(contract.ThinkingKeepValues, keep) {
						return modelExecutionProfileEvidence{}, fmt.Errorf("thinking.keep must be one of %s", strings.Join(contract.ThinkingKeepValues, ", "))
					}
					if contract.ThinkingKeepRequiresEnabled && !reasoningActive {
						return modelExecutionProfileEvidence{}, fmt.Errorf("thinking.keep requires thinking.type=enabled for profile %s", plan.ReasoningProfile)
					}
					evidence.ReasoningThinkingKeepRequest = keep
				}
			}
			clearThinking, clearPresent, clearErr := optionalThinkingBoolean(envelope[contract.ToggleControl.Field], "clear_thinking")
			if clearErr != nil {
				return modelExecutionProfileEvidence{}, clearErr
			}
			if clearPresent && !contract.ThinkingClearSupported {
				return modelExecutionProfileEvidence{}, fmt.Errorf("thinking.clear_thinking is not supported by profile %s", plan.ReasoningProfile)
			}
			if contract.ThinkingClearSupported {
				evidence.ReasoningThinkingClearRequest = "omitted"
				evidence.ReasoningThinkingClearDefault = fmt.Sprintf("%t", contract.ThinkingClearProviderDefault)
				if clearPresent {
					evidence.ReasoningThinkingClearRequest = fmt.Sprintf("%t", clearThinking)
				}
			}
		}
	}

	effortRequest := ""
	if contract.EffortControl.Field != "" {
		if contract.ToggleControl.Field == "" && contract.EffortEnablesReasoning && contract.EffortControl.ProviderDefault != "" {
			defaultRaw, marshalErr := json.Marshal(contract.EffortControl.ProviderDefault)
			if marshalErr != nil {
				return modelExecutionProfileEvidence{}, fmt.Errorf("profile %s reasoning effort default is invalid: %w", plan.ReasoningProfile, marshalErr)
			}
			_, defaultEffective, present, defaultErr := reasoningControlValue(defaultRaw, contract.EffortControl)
			if defaultErr != nil {
				return modelExecutionProfileEvidence{}, fmt.Errorf("profile %s reasoning effort default is invalid: %w", plan.ReasoningProfile, defaultErr)
			}
			if !present {
				return modelExecutionProfileEvidence{}, fmt.Errorf("profile %s reasoning effort default is unavailable", plan.ReasoningProfile)
			}
			reasoningActive = defaultEffective != "disabled"
		}
		value, effective, present, err := reasoningControlValue(envelope[contract.EffortControl.Field], contract.EffortControl)
		if err != nil {
			return modelExecutionProfileEvidence{}, err
		}
		effortRequest = "omitted"
		evidence.ReasoningEffortEffective = "omitted"
		if present {
			if !reasoningActive && !contract.EffortEnablesReasoning {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s requires active reasoning for profile %s", contract.EffortControl.Field, plan.ReasoningProfile)
			}
			if contract.EffortEnablesReasoning {
				reasoningActive = true
			}
			effortRequest = value
			evidence.ReasoningEffortEffective = effective
			if effectiveWithTools, ok := contract.EffortEffectiveWithTools[effective]; ok && requestHasFunctionTools(envelope["tools"]) {
				evidence.ReasoningEffortEffective = effectiveWithTools
			}
			if effective == "disabled" {
				reasoningActive = false
			}
		}
		evidence.ReasoningEffortRequest = effortRequest
	}
	if contract.BudgetControl.Field != "" {
		evidence.ReasoningBudgetRequest = "omitted"
		evidence.ReasoningBudgetDefault = fmt.Sprintf("%d", contract.BudgetControl.ProviderDefault)
		budget, present, err := optionalJSONInteger(envelope[contract.BudgetControl.Field], contract.BudgetControl.Field)
		if err != nil {
			return modelExecutionProfileEvidence{}, err
		}
		if present {
			if budget < contract.BudgetControl.Minimum || budget > contract.BudgetControl.Maximum {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s must be between %d and %d", contract.BudgetControl.Field, contract.BudgetControl.Minimum, contract.BudgetControl.Maximum)
			}
			if contract.BudgetControl.RequiresActiveReasoning && !reasoningActive {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s requires active reasoning for profile %s", contract.BudgetControl.Field, plan.ReasoningProfile)
			}
			if contract.BudgetControl.MutuallyExclusiveWithEffort && effortRequest != "omitted" && effortRequest != "" {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s and %s cannot be set together for profile %s", contract.BudgetControl.Field, contract.EffortControl.Field, plan.ReasoningProfile)
			}
			evidence.ReasoningBudgetRequest = fmt.Sprintf("%d", budget)
		}
	}

	if contract.PreserveThinkingField != "" {
		evidence.ReasoningPreserveRequest = "omitted"
		evidence.ReasoningPreserveDefault = fmt.Sprintf("%t", contract.PreserveThinkingProviderDefault)
		if raw, exists := envelope[contract.PreserveThinkingField]; exists && !isJSONNull(raw) {
			var preserve bool
			if json.Unmarshal(raw, &preserve) != nil {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s must be a boolean", contract.PreserveThinkingField)
			}
			if preserve && contract.PreserveThinkingRequiresEnabled && !reasoningActive {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s requires active reasoning for profile %s", contract.PreserveThinkingField, plan.ReasoningProfile)
			}
			evidence.ReasoningPreserveRequest = fmt.Sprintf("%t", preserve)
		}
	}

	if contract.SplitOutputField != "" {
		evidence.ReasoningSplitRequest = "omitted"
		evidence.ReasoningSplitDefault = fmt.Sprintf("%t", contract.SplitOutputProviderDefault)
		if raw, exists := envelope[contract.SplitOutputField]; exists && !isJSONNull(raw) {
			var split bool
			if json.Unmarshal(raw, &split) != nil {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s must be a boolean", contract.SplitOutputField)
			}
			if split && !contract.SplitOutputEnabledSupported {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s=true is not supported by profile %s", contract.SplitOutputField, plan.ReasoningProfile)
			}
			evidence.ReasoningSplitRequest = fmt.Sprintf("%t", split)
		}
	}

	evidence.ReasoningRequest = reasoningControlSummary(toggleRequest, effortRequest)
	if evidence.ReasoningRequest == "" {
		evidence.ReasoningRequest = "required"
	}
	if reasoningActive {
		if contract.ActiveStreamingUnsupported && jsonBooleanField(envelope["stream"]) {
			return modelExecutionProfileEvidence{}, fmt.Errorf("streaming is not supported while reasoning is active for profile %s", plan.ReasoningProfile)
		}
		if !jsonBooleanField(envelope["stream"]) {
			evidence.ReasoningTimeoutSeconds = contract.ActiveNonStreamTimeoutSeconds
		}
		for field, maximum := range contract.ActiveOutputFieldMaximums {
			value, present, err := optionalJSONInteger(envelope[field], field)
			if err != nil {
				return modelExecutionProfileEvidence{}, err
			}
			if present && value > maximum {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s must be at most %d while reasoning is active for profile %s", field, maximum, plan.ReasoningProfile)
			}
		}
		for _, field := range contract.UnsupportedRequestFieldsWhileActive {
			if hasNonNullJSONField(envelope, field) {
				if field == "tool_choice" && redundantAutoToolChoice(envelope) {
					evidence.ProviderRequestNormalizations = append(evidence.ProviderRequestNormalizations, "tool_choice=auto->omitted")
					continue
				}
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s is not supported while reasoning is active for profile %s", field, plan.ReasoningProfile)
			}
		}
		for _, field := range contract.ForbiddenRequestFieldsWhileActive {
			if hasNonNullJSONField(envelope, field) {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s is ineffective while reasoning is active for profile %s", field, plan.ReasoningProfile)
			}
		}
	}

	choice, err := executionToolChoice(envelope["tool_choice"])
	if err != nil {
		return modelExecutionProfileEvidence{}, err
	}
	if choice != "" && len(contract.AllowedToolChoices) > 0 && !slices.Contains(contract.AllowedToolChoices, choice) {
		return modelExecutionProfileEvidence{}, fmt.Errorf("tool_choice=%s is not supported by profile %s", choice, plan.ReasoningProfile)
	}
	if reasoningActive && choice == "named" && !contract.NamedToolChoiceWhileReasoning {
		return modelExecutionProfileEvidence{}, fmt.Errorf("named tool_choice is incompatible with active reasoning for profile %s", plan.ReasoningProfile)
	}
	if reasoningActive && choice == "required" && !contract.RequiredToolChoiceWhileReasoning {
		return modelExecutionProfileEvidence{}, fmt.Errorf("tool_choice=required is incompatible with active reasoning for profile %s", plan.ReasoningProfile)
	}
	if reasoningActive && contract.PreserveAssistantReasoningForTools {
		if err := requireAssistantStateForToolHistory(envelope["messages"], "reasoning_content"); err != nil {
			return modelExecutionProfileEvidence{}, err
		}
	}
	if reasoningActive && contract.PreserveAssistantReasoningForToolRequests && requestHasFunctionTools(envelope["tools"]) {
		if err := requireAssistantReasoningForToolRequestHistory(envelope["messages"], "reasoning_content"); err != nil {
			return modelExecutionProfileEvidence{}, err
		}
	}
	if reasoningActive && contract.AssistantToolHistoryReplayField != "" {
		if err := requireAssistantStateForToolHistory(envelope["messages"], contract.AssistantToolHistoryReplayField); err != nil {
			return modelExecutionProfileEvidence{}, err
		}
	}
	if err := validateFixedRequestNumbers(envelope, contract, reasoningActive, plan.ReasoningProfile); err != nil {
		return modelExecutionProfileEvidence{}, err
	}
	return evidence, nil
}

func validateCurrentKimiChatStreamUsageRequest(envelope map[string]json.RawMessage, plan modelrouting.Plan) (string, error) {
	if plan.ModelOperationID != modelprofile.OperationOpenAIChat {
		return "", nil
	}
	expectedProtocol := ""
	if d := plan.Definition; d.Digest() != "" && d.Profile().Provider == "kimi" && (d.Profile().ProtocolProfile == "protocol.kimi-openai-chat.v5" || d.Profile().ProtocolProfile == "protocol.kimi-openai-chat.k3.v5") {
		expectedProtocol = d.Profile().ProtocolProfile
	} else {
		switch strings.TrimSpace(plan.ExecutionProfileID) + "\x00" + strings.TrimSpace(plan.ExecutionProfileVersion) {
		case "kimi.k2.6.api\x001.7.0", "kimi.k2.7-code.api\x001.3.0":
			expectedProtocol = "protocol.kimi-openai-chat.v5"
		case "kimi.k3.api\x001.9.0":
			expectedProtocol = "protocol.kimi-openai-chat.k3.v5"
		default:
			return "", nil
		}
	}
	if strings.TrimSpace(plan.ModelProtocolProfile) != expectedProtocol ||
		strings.TrimSpace(plan.CacheUsageProfile) != "cache-usage.kimi-platform.v1" {
		return "", fmt.Errorf("exact current Kimi Chat stream request identity mismatch")
	}
	stream := false
	if raw, exists := envelope["stream"]; exists && !isJSONNull(raw) {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return "", fmt.Errorf("stream must be a boolean")
		}
	}
	if !stream {
		return "", nil
	}
	options := make(map[string]json.RawMessage)
	if raw, exists := envelope["stream_options"]; exists && !isJSONNull(raw) {
		if err := json.Unmarshal(raw, &options); err != nil || options == nil {
			return "", fmt.Errorf("stream_options must be an object")
		}
	}
	rawIncludeUsage, present := options["include_usage"]
	if !present || isJSONNull(rawIncludeUsage) {
		return "stream_options.include_usage=omitted->true", nil
	}
	var includeUsage bool
	if err := json.Unmarshal(rawIncludeUsage, &includeUsage); err != nil {
		return "", fmt.Errorf("stream_options.include_usage must be a boolean")
	}
	if !includeUsage {
		return "", fmt.Errorf("stream_options.include_usage=false is not supported by exact current Kimi Chat streaming")
	}
	return "", nil
}

func requestHasFunctionTools(raw json.RawMessage) bool {
	if len(raw) == 0 || isJSONNull(raw) {
		return false
	}
	var tools []json.RawMessage
	return json.Unmarshal(raw, &tools) == nil && len(tools) > 0
}

func redundantAutoToolChoice(envelope map[string]json.RawMessage) bool {
	choice, err := executionToolChoice(envelope["tool_choice"])
	if err != nil || choice != "auto" {
		return false
	}
	var tools []json.RawMessage
	return json.Unmarshal(envelope["tools"], &tools) == nil && len(tools) > 0
}

// normalizeProviderRequest applies only normalizations already authorized by
// validation evidence. The selected Model Profile still owns which wire fields
// are unsupported; the Operation Contract owns the equivalence between an
// explicit auto tool choice and the provider's default when tools are present.
func (e modelExecutionProfileEvidence) normalizeProviderRequest(body []byte) ([]byte, error) {
	if len(e.ProviderRequestNormalizations) == 0 {
		return body, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, fmt.Errorf("normalized chat request must be an object")
	}
	if slices.Contains(e.ProviderRequestNormalizations, "tool_choice=auto->omitted") {
		if !redundantAutoToolChoice(envelope) {
			return nil, fmt.Errorf("tool_choice normalization evidence drift")
		}
		delete(envelope, "tool_choice")
	}
	if slices.Contains(e.ProviderRequestNormalizations, "parallel_tool_calls=omitted->false") {
		if raw, exists := envelope["parallel_tool_calls"]; exists && !isJSONNull(raw) {
			return nil, fmt.Errorf("parallel_tool_calls normalization evidence drift")
		}
		envelope["parallel_tool_calls"] = json.RawMessage("false")
	}
	if slices.Contains(e.ProviderRequestNormalizations, "stream_options.include_usage=omitted->true") {
		options := make(map[string]json.RawMessage)
		if raw, exists := envelope["stream_options"]; exists && !isJSONNull(raw) {
			if err := json.Unmarshal(raw, &options); err != nil || options == nil {
				return nil, fmt.Errorf("stream_options normalization evidence drift")
			}
		}
		if raw, exists := options["include_usage"]; exists && !isJSONNull(raw) {
			return nil, fmt.Errorf("stream_options.include_usage normalization evidence drift")
		}
		options["include_usage"] = json.RawMessage("true")
		raw, err := json.Marshal(options)
		if err != nil {
			return nil, fmt.Errorf("encode normalized stream_options: %w", err)
		}
		envelope["stream_options"] = raw
	}
	normalized, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode normalized chat request: %w", err)
	}
	return normalized, nil
}

func hasAnyNonNullJSONField(envelope map[string]json.RawMessage, fields ...string) bool {
	for _, field := range fields {
		if hasNonNullJSONField(envelope, field) {
			return true
		}
	}
	return false
}

func reasoningControlValue(raw json.RawMessage, contract modelprofile.ReasoningControlContract) (string, string, bool, error) {
	var value string
	var present bool
	var err error
	if contract.WireType == modelprofile.ReasoningControlWireBoolean {
		var boolean bool
		if len(raw) == 0 || isJSONNull(raw) {
			return "", "", false, nil
		}
		if err := json.Unmarshal(raw, &boolean); err != nil {
			return "", "", false, fmt.Errorf("%s must be a boolean", contract.Field)
		}
		if boolean {
			return "true", "enabled", true, nil
		}
		return "false", "disabled", true, nil
	} else if contract.Field == "thinking" {
		value, present, err = optionalThinkingType(raw)
	} else {
		value, present, err = optionalJSONString(raw, contract.Field)
	}
	if err != nil || !present {
		return value, value, present, err
	}
	if slices.Contains(contract.DisabledValues, value) {
		return value, "disabled", true, nil
	}
	if slices.Contains(contract.AllowedValues, value) {
		return value, value, true, nil
	}
	if effective, ok := contract.CompatibilityValues[value]; ok && slices.Contains(contract.AllowedValues, effective) {
		return value, effective, true, nil
	}
	return "", "", false, fmt.Errorf("%s must be one of %s", contract.Field, strings.Join(reasoningControlAcceptedValues(contract), ", "))
}

func reasoningControlProviderDefaultActive(contract modelprofile.ReasoningControlContract) bool {
	providerDefault := strings.TrimSpace(contract.ProviderDefault)
	if providerDefault == "" || providerDefault == "disabled" || slices.Contains(contract.DisabledValues, providerDefault) {
		return false
	}
	if effective, ok := contract.CompatibilityValues[providerDefault]; ok {
		return effective != "disabled" && !slices.Contains(contract.DisabledValues, effective)
	}
	return true
}

func optionalJSONInteger(raw json.RawMessage, label string) (int64, bool, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return 0, false, nil
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false, fmt.Errorf("%s must be an integer", label)
	}
	return value, true, nil
}

func reasoningControlAcceptedValues(contract modelprofile.ReasoningControlContract) []string {
	values := slices.Clone(contract.AllowedValues)
	compatibility := make([]string, 0, len(contract.CompatibilityValues))
	for value := range contract.CompatibilityValues {
		compatibility = append(compatibility, value)
	}
	sort.Strings(compatibility)
	values = append(values, compatibility...)
	return append(values, contract.DisabledValues...)
}

func reasoningControlSummary(toggle, effort string) string {
	switch {
	case toggle != "" && effort != "":
		return "thinking=" + toggle + ";reasoning_effort=" + effort
	case toggle != "":
		return toggle
	default:
		return effort
	}
}

func hasNonNullJSONField(envelope map[string]json.RawMessage, field string) bool {
	raw, ok := envelope[field]
	return ok && !isJSONNull(raw)
}

func optionalJSONString(raw json.RawMessage, label string) (string, bool, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false, fmt.Errorf("%s must be a string", label)
	}
	return value, true, nil
}

func optionalThinkingType(raw json.RawMessage) (string, bool, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", false, nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return "", false, fmt.Errorf("thinking must be an object")
	}
	if err := rejectUnknownJSONFields(value, "thinking", "type", "keep", "clear_thinking"); err != nil {
		return "", false, err
	}
	if value["type"] == nil {
		return "", false, fmt.Errorf("thinking.type is required")
	}
	typeValue, present, err := optionalJSONString(value["type"], "thinking.type")
	if err != nil || !present {
		if err != nil {
			return "", false, err
		}
		return "", false, fmt.Errorf("thinking.type is required")
	}
	if rawKeep, exists := value["keep"]; exists && !isJSONNull(rawKeep) {
		if _, present, keepErr := optionalJSONString(rawKeep, "thinking.keep"); keepErr != nil || !present {
			if keepErr != nil {
				return "", false, keepErr
			}
			return "", false, fmt.Errorf("thinking.keep must be a string")
		}
	}
	if _, _, err := optionalThinkingBoolean(raw, "clear_thinking"); err != nil {
		return "", false, err
	}
	return typeValue, true, nil
}

func optionalThinkingKeep(raw json.RawMessage) (string, bool, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", false, nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return "", false, fmt.Errorf("thinking must be an object")
	}
	return optionalJSONString(value["keep"], "thinking.keep")
}

func optionalThinkingBoolean(raw json.RawMessage, field string) (bool, bool, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return false, false, nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return false, false, fmt.Errorf("thinking must be an object")
	}
	rawValue, exists := value[field]
	if !exists || isJSONNull(rawValue) {
		return false, false, nil
	}
	var result bool
	if err := json.Unmarshal(rawValue, &result); err != nil {
		return false, false, fmt.Errorf("thinking.%s must be a boolean", field)
	}
	return result, true, nil
}

func validateFixedRequestNumbers(
	envelope map[string]json.RawMessage,
	contract modelprofile.ReasoningContract,
	reasoningActive bool,
	profileID string,
) error {
	expected := make(map[string]string, len(contract.FixedRequestNumbers)+len(contract.FixedRequestNumbersWhenDisabled))
	if reasoningActive || !contract.FixedRequestNumbersReasoningOnly {
		for field, value := range contract.FixedRequestNumbers {
			expected[field] = value
		}
	}
	if !reasoningActive {
		for field, value := range contract.FixedRequestNumbersWhenDisabled {
			expected[field] = value
		}
	}
	for field, want := range expected {
		raw, present := envelope[field]
		if !present || isJSONNull(raw) {
			continue
		}
		actualNumber, ok := new(big.Rat).SetString(strings.TrimSpace(string(raw)))
		if !ok {
			return fmt.Errorf("%s must be a JSON number", field)
		}
		expectedNumber, ok := new(big.Rat).SetString(want)
		if !ok || actualNumber.Cmp(expectedNumber) != 0 {
			return fmt.Errorf("%s must be %s for profile %s", field, want, profileID)
		}
	}
	return nil
}

func executionToolChoice(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", nil
	}
	if rawJSONString(raw) {
		var value string
		_ = json.Unmarshal(raw, &value)
		switch value {
		case "none", "auto", "required":
			return value, nil
		default:
			return "", fmt.Errorf("unsupported tool_choice")
		}
	}
	if firstJSONToken(raw) == '{' {
		return "named", nil
	}
	return "", fmt.Errorf("tool_choice must be a string or object")
}

func requireAssistantStateForToolHistory(raw json.RawMessage, field string) error {
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return fmt.Errorf("messages must be an array")
	}
	for index, message := range messages {
		role, present, err := optionalJSONString(message["role"], "message role")
		if err != nil || !present || role != "assistant" {
			continue
		}
		toolCalls, hasTools := message["tool_calls"]
		if !hasTools || isJSONNull(toolCalls) {
			continue
		}
		var calls []json.RawMessage
		if err := json.Unmarshal(toolCalls, &calls); err != nil || len(calls) == 0 {
			continue
		}
		value, present, err := optionalJSONString(message[field], field)
		if err != nil || !present || strings.TrimSpace(value) == "" {
			return fmt.Errorf("assistant tool history message %d must preserve %s", index, field)
		}
	}
	return nil
}

func requireAssistantReasoningForToolRequestHistory(raw json.RawMessage, field string) error {
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &messages); err != nil {
		return fmt.Errorf("messages must be an array")
	}
	for index, message := range messages {
		role, present, err := optionalJSONString(message["role"], "message role")
		if err != nil || !present || role != "assistant" {
			continue
		}
		value, present, err := optionalJSONString(message[field], field)
		if err != nil || !present || strings.TrimSpace(value) == "" {
			return fmt.Errorf("assistant history message %d must preserve %s when function tools are requested", index, field)
		}
	}
	return nil
}
