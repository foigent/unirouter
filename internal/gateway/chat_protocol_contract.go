package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

type advancedChatProtocolValidation struct {
	SanitizedBody                 []byte
	ReservedMeterUsage            []metering.MeterQuantity
	ProviderRequestNormalizations []string
	ProcessingTierPolicy          string
	ProcessingTierRequest         string
	ProcessingTierEffective       string
	ProcessingTierTimeoutSeconds  int
	ProcessingTierExplicit        bool
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
}

type modeledFunctionToolInvocation struct {
	ToolsRequested      bool
	ToolStreamRequested bool
	ChoiceRequest       string
	ChoiceEffective     string
	ChoiceOrigin        string
	DeclaredNames       []string
	SelectedName        string
	ParallelRequest     string
	ParallelEffective   bool
	ParallelOrigin      string
}

// validateAdvancedChatProtocolRequest validates only advanced shapes that the
// generic OpenAI-compatible admission contract deliberately rejects. It also
// builds a validation-only shadow body for the existing strict validator; the
// original request bytes remain the admission-counting and provider payload.
func validateAdvancedChatProtocolRequest(body []byte, contract modelprofile.ChatProtocolContract) (advancedChatProtocolValidation, error) {
	return validateAdvancedChatProtocolRequestWithAuthority(body, contract, false)
}

func validateAdvancedChatProtocolRequestWithAuthority(
	body []byte,
	contract modelprofile.ChatProtocolContract,
	legacyProtocolAuthority bool,
) (advancedChatProtocolValidation, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return advancedChatProtocolValidation{}, fmt.Errorf("chat request must be an object")
	}
	streamRequested := false
	streamNormalizations := make([]string, 0, 1)
	if contract.StreamingModeled {
		if rawStream, exists := envelope["stream"]; exists && !isJSONNull(rawStream) {
			var stream bool
			if err := json.Unmarshal(rawStream, &stream); err != nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("stream must be a boolean")
			}
			if stream && !contract.Streaming {
				return advancedChatProtocolValidation{}, fmt.Errorf("streaming is not supported by protocol profile %s", contract.ProfileID)
			}
			streamRequested = stream
		}
	}
	if streamRequested && contract.StreamContract.RequestOptionsModeled {
		rawOptions, exists := envelope["stream_options"]
		options := make(map[string]json.RawMessage)
		if exists && !isJSONNull(rawOptions) {
			if err := json.Unmarshal(rawOptions, &options); err != nil || options == nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("stream_options must be an object")
			}
			if err := rejectUnknownJSONFields(options, "stream_options", contract.StreamContract.AllowedRequestOptions...); err != nil {
				return advancedChatProtocolValidation{}, err
			}
		}
		if contract.StreamContract.RequireIncludeUsage {
			rawIncludeUsage, present := options["include_usage"]
			if !present || isJSONNull(rawIncludeUsage) {
				streamNormalizations = append(streamNormalizations, "stream_options.include_usage=omitted->true")
			} else {
				var includeUsage bool
				if err := json.Unmarshal(rawIncludeUsage, &includeUsage); err != nil {
					return advancedChatProtocolValidation{}, fmt.Errorf("stream_options.include_usage must be a boolean")
				}
				if !includeUsage {
					return advancedChatProtocolValidation{}, fmt.Errorf("stream_options.include_usage=false is not supported by protocol profile %s", contract.ProfileID)
				}
			}
		}
	}
	if contract.OutputLimitModeled {
		acceptedFields := contract.OutputLimitAcceptedFields
		if len(acceptedFields) == 0 && contract.OutputLimitField != "" {
			acceptedFields = []string{contract.OutputLimitField}
		}
		var firstLimit *int64
		providedLimitFields := 0
		for _, field := range []string{"max_tokens", "max_completion_tokens"} {
			rawLimit, exists := envelope[field]
			if !exists {
				continue
			}
			if !slices.Contains(acceptedFields, field) {
				return advancedChatProtocolValidation{}, &unsupportedOutputParameterError{parameter: field, accepted: slices.Clone(acceptedFields), profile: contract.ProfileID}
			}
			if isJSONNull(rawLimit) {
				return advancedChatProtocolValidation{}, fmt.Errorf("%s must be an integer", field)
			}
			var limit int64
			if err := json.Unmarshal(rawLimit, &limit); err != nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("%s must be an integer", field)
			}
			maximum := contract.OutputLimitMax
			if fieldMaximum, ok := contract.OutputLimitFieldMaximums[field]; ok {
				maximum = fieldMaximum
			}
			if limit < contract.OutputLimitMin || limit > maximum {
				return advancedChatProtocolValidation{}, fmt.Errorf("%s must be between %d and %d", field, contract.OutputLimitMin, maximum)
			}
			providedLimitFields++
			if contract.OutputLimitFieldsMutuallyExclusive && providedLimitFields > 1 {
				return advancedChatProtocolValidation{}, fmt.Errorf("max_tokens and max_completion_tokens cannot be set together for protocol profile %s", contract.ProfileID)
			}
			if firstLimit != nil && *firstLimit != limit {
				return advancedChatProtocolValidation{}, fmt.Errorf("max_tokens and max_completion_tokens must match when both are provided")
			}
			value := limit
			firstLimit = &value
		}
	}
	var functionToolInvocation modeledFunctionToolInvocation
	if contract.FunctionToolsModeled {
		var err error
		functionToolInvocation, err = validateModeledFunctionTools(envelope, contract)
		if err != nil {
			return advancedChatProtocolValidation{}, err
		}
		if contract.ToolStreamField != "" {
			delete(envelope, contract.ToolStreamField)
		}
	}
	if contract.ResponseFormatModeled {
		if err := validateModeledResponseFormat(envelope, contract); err != nil {
			return advancedChatProtocolValidation{}, err
		}
	}
	searchRequested := false
	if contract.SearchRequestModeled {
		var err error
		searchRequested, err = validateModeledSearchRequest(envelope, contract)
		if err != nil {
			return advancedChatProtocolValidation{}, err
		}
		if contract.SearchEnableField != "" {
			delete(envelope, contract.SearchEnableField)
		}
		if contract.SearchOptionsField != "" {
			delete(envelope, contract.SearchOptionsField)
		}
	}
	processingTierPolicy := ""
	processingTierRequest := ""
	processingTierEffective := ""
	processingTierTimeoutSeconds := 0
	processingTierExplicit := false
	if contract.ProcessingTierPolicyID != "" && !legacyProtocolAuthority {
		policy, ok := modelprofile.ResolveProcessingTierPolicy(contract.ProcessingTierPolicyID)
		if !ok {
			return advancedChatProtocolValidation{}, fmt.Errorf("processing-tier policy %s is not available", contract.ProcessingTierPolicyID)
		}
		processingTierPolicy = policy.ID
		processingTierRequest = "omitted"
		processingTierEffective = policy.OmittedNormalizedTier
		processingTierTimeoutSeconds = policy.OmittedTimeoutSeconds
	}
	if rawTier, exists := envelope["service_tier"]; exists && !legacyProtocolAuthority {
		if contract.ProcessingTierPolicyID == "" {
			return advancedChatProtocolValidation{}, fmt.Errorf("service_tier is not supported by protocol profile %s", contract.ProfileID)
		}
		var tier string
		if isJSONNull(rawTier) || json.Unmarshal(rawTier, &tier) != nil {
			return advancedChatProtocolValidation{}, fmt.Errorf("service_tier must be a string")
		}
		policy, _ := modelprofile.ResolveProcessingTierPolicy(contract.ProcessingTierPolicyID)
		resolved, ok := modelprofile.ResolveProcessingTierRequest(policy, tier)
		if !ok {
			accepted := make([]string, 0, len(policy.RequestValues))
			for _, candidate := range policy.RequestValues {
				accepted = append(accepted, candidate.WireValue)
			}
			return advancedChatProtocolValidation{}, fmt.Errorf("service_tier must be one of %s", strings.Join(accepted, ", "))
		}
		processingTierRequest = resolved.WireValue
		processingTierEffective = resolved.NormalizedTier
		processingTierTimeoutSeconds = resolved.RequestTimeoutSeconds
		processingTierExplicit = true
	}

	rawMessages, ok := envelope["messages"]
	if !ok || isJSONNull(rawMessages) {
		return advancedChatProtocolValidation{}, fmt.Errorf("messages must be an array")
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(rawMessages, &messages); err != nil {
		return advancedChatProtocolValidation{}, fmt.Errorf("messages must be an array of objects")
	}
	if contract.PromptCacheControlModeled {
		if err := validateModeledPromptCache(envelope, messages, contract); err != nil {
			return advancedChatProtocolValidation{}, err
		}
	}
	partialRequested := false
	dynamicToolsRequested := false
	exactDynamicFunctionTools := contract.DynamicSystemTools && contract.FunctionToolInvocationCapabilitiesModeled
	dynamicDeclaredFunctionNames := make([]string, 0)
	visibleFunctionNames := make(map[string]struct{}, len(functionToolInvocation.DeclaredNames))
	for _, name := range functionToolInvocation.DeclaredNames {
		visibleFunctionNames[name] = struct{}{}
	}
	requestedMediaKinds := make(map[string]struct{})
	imageRequested := false
	imageCount := 0
	var imageAdmissionTokenUpper int64
	for index := range messages {
		role, err := requiredMessageRole(messages[index])
		if err != nil {
			return advancedChatProtocolValidation{}, fmt.Errorf("message %d: %w", index, err)
		}
		if contract.AssistantReasoningHistoryModeled {
			for _, field := range []string{"reasoning_content", "reasoning"} {
				raw, present := messages[index][field]
				if !present || isJSONNull(raw) {
					continue
				}
				if role != "assistant" || !slices.Contains(contract.AssistantReasoningHistoryFields, field) {
					return advancedChatProtocolValidation{}, fmt.Errorf("message %d %s history is not supported by protocol profile %s", index, field, contract.ProfileID)
				}
			}
		}
		for _, field := range contract.OpaqueAssistantMessageFields {
			if raw, present := messages[index][field]; present && !isJSONNull(raw) {
				if contract.AssistantOpaqueHistoryModeled && slices.Contains(contract.AssistantOpaqueHistoryFields, field) {
					continue
				}
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d %s history is not supported by protocol profile %s", index, field, contract.ProfileID)
			}
		}
		for _, field := range contract.AssistantOpaqueHistoryFields {
			raw, present := messages[index][field]
			if !present || isJSONNull(raw) {
				continue
			}
			var value string
			if role != "assistant" || json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) == "" {
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d %s must be a non-empty assistant string", index, field)
			}
			delete(messages[index], field)
		}
		if rawPartial, exists := messages[index]["partial"]; exists && !isJSONNull(rawPartial) {
			if !contract.PartialMode {
				return advancedChatProtocolValidation{}, fmt.Errorf("partial mode is not supported by protocol profile %s", contract.ProfileID)
			}
			var partial bool
			if err := json.Unmarshal(rawPartial, &partial); err != nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d partial must be a boolean", index)
			}
			if partial && (role != "assistant" || index != len(messages)-1) {
				return advancedChatProtocolValidation{}, fmt.Errorf("partial=true is allowed only on the final assistant message")
			}
			partialRequested = partialRequested || partial
			delete(messages[index], "partial")
		}
		if rawTools, exists := messages[index]["tools"]; exists && !isJSONNull(rawTools) {
			if !contract.DynamicSystemTools {
				return advancedChatProtocolValidation{}, fmt.Errorf("dynamic system tools are not supported by protocol profile %s", contract.ProfileID)
			}
			if role != "system" {
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d tools are allowed only on system messages", index)
			}
			if _, carriesContent := messages[index]["content"]; carriesContent {
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d dynamic tools cannot be used with content", index)
			}
			declaredNames, err := validateDynamicSystemTools(rawTools)
			if err != nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d dynamic tools: %w", index, err)
			}
			if exactDynamicFunctionTools {
				namePattern, compileErr := regexp.Compile(contract.FunctionToolNamePattern)
				if compileErr != nil {
					return advancedChatProtocolValidation{}, fmt.Errorf("protocol profile %s has an invalid function name pattern", contract.ProfileID)
				}
				for _, name := range declaredNames {
					if !namePattern.MatchString(name) {
						return advancedChatProtocolValidation{}, fmt.Errorf("message %d dynamic tools: function name must match %s", index, namePattern.String())
					}
					if _, duplicate := visibleFunctionNames[name]; duplicate {
						return advancedChatProtocolValidation{}, fmt.Errorf("message %d dynamic tools: duplicate function name %s", index, name)
					}
					visibleFunctionNames[name] = struct{}{}
					dynamicDeclaredFunctionNames = append(dynamicDeclaredFunctionNames, name)
				}
			}
			dynamicToolsRequested = dynamicToolsRequested || len(declaredNames) > 0
			delete(messages[index], "tools")
		}
		if rawContent, exists := messages[index]["content"]; exists && !isJSONNull(rawContent) && firstJSONToken(rawContent) == '[' {
			kinds, kindsErr := mediaKindsInContentParts(rawContent)
			if kindsErr != nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d content: %w", index, kindsErr)
			}
			for kind := range kinds {
				requestedMediaKinds[kind] = struct{}{}
			}
			sanitized, imageValidation, err := sanitizeAdvancedContentParts(rawContent, role, contract)
			if err != nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d content: %w", index, err)
			}
			imageRequested = imageRequested || imageValidation.Requested
			imageCount += imageValidation.Count
			if imageValidation.AdmissionTokenUpper > 0 {
				if imageAdmissionTokenUpper > (1<<63-1)-imageValidation.AdmissionTokenUpper {
					return advancedChatProtocolValidation{}, fmt.Errorf("image admission estimate overflow")
				}
				imageAdmissionTokenUpper += imageValidation.AdmissionTokenUpper
			}
			messages[index]["content"] = sanitized
		}
	}
	if exactDynamicFunctionTools {
		var err error
		functionToolInvocation, err = finalizeDynamicFunctionToolInvocation(
			functionToolInvocation, dynamicDeclaredFunctionNames, envelope, contract,
		)
		if err != nil {
			return advancedChatProtocolValidation{}, err
		}
	}
	if imageRequested {
		if err := validateImageRequestCombination(envelope, messages, streamRequested, imageCount, contract); err != nil {
			return advancedChatProtocolValidation{}, err
		}
	}
	functionToolHistoryRequested := false
	if contract.ExactFunctionToolHistoryModeled {
		if err := validateExactFunctionToolHistory(messages, contract); err != nil {
			return advancedChatProtocolValidation{}, err
		}
		functionToolHistoryRequested = exactFunctionToolHistoryRequested(messages)
	}
	if functionToolHistoryRequested && contract.JSONSchemaDisallowsFunctionTools && responseFormatType(envelope["response_format"]) == "json_schema" {
		return advancedChatProtocolValidation{}, fmt.Errorf("response_format.type=json_schema cannot be combined with function tool history for protocol profile %s", contract.ProfileID)
	}
	if contract.ExclusiveMediaKinds && len(requestedMediaKinds) > 1 {
		return advancedChatProtocolValidation{}, fmt.Errorf("image, video, and file inputs cannot be combined by protocol profile %s", contract.ProfileID)
	}
	sanitizedMessages, err := json.Marshal(messages)
	if err != nil {
		return advancedChatProtocolValidation{}, fmt.Errorf("encode sanitized messages: %w", err)
	}
	envelope["messages"] = sanitizedMessages
	if partialRequested && contract.PartialRejectJSONObject && responseFormatType(envelope["response_format"]) == "json_object" {
		return advancedChatProtocolValidation{}, fmt.Errorf("partial mode is incompatible with response_format.type=json_object for protocol profile %s", contract.ProfileID)
	}

	builtinRequested := false
	if rawTools, exists := envelope["tools"]; exists && !isJSONNull(rawTools) {
		var tools []json.RawMessage
		if err := json.Unmarshal(rawTools, &tools); err != nil {
			return advancedChatProtocolValidation{}, fmt.Errorf("tools must be an array")
		}
		regular := make([]json.RawMessage, 0, len(tools))
		for index, rawTool := range tools {
			builtin, err := validateBuiltinWebSearchTool(rawTool, contract)
			if err != nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("tool %d: %w", index, err)
			}
			if builtin {
				if builtinRequested {
					return advancedChatProtocolValidation{}, fmt.Errorf("builtin web search tool may be declared only once")
				}
				builtinRequested = true
				continue
			}
			regular = append(regular, rawTool)
		}
		regularWire, err := json.Marshal(regular)
		if err != nil {
			return advancedChatProtocolValidation{}, fmt.Errorf("encode regular tools: %w", err)
		}
		if err := validateMeasurableTools(regularWire); err != nil {
			return advancedChatProtocolValidation{}, err
		}
		envelope["tools"] = regularWire
	}

	result := advancedChatProtocolValidation{
		ProviderRequestNormalizations: streamNormalizations,
		ProcessingTierPolicy:          processingTierPolicy,
		ProcessingTierRequest:         processingTierRequest,
		ProcessingTierEffective:       processingTierEffective,
		ProcessingTierTimeoutSeconds:  processingTierTimeoutSeconds,
		ProcessingTierExplicit:        processingTierExplicit,
		FunctionToolInvocationModeled: contract.FunctionToolInvocationCapabilitiesModeled,
		FunctionToolsRequested:        functionToolInvocation.ToolsRequested || functionToolHistoryRequested,
		FunctionToolChoiceRequest:     functionToolInvocation.ChoiceRequest,
		FunctionToolChoiceEffective:   functionToolInvocation.ChoiceEffective,
		FunctionToolChoiceOrigin:      functionToolInvocation.ChoiceOrigin,
		DeclaredFunctionNames:         slices.Clone(functionToolInvocation.DeclaredNames),
		SelectedFunctionName:          functionToolInvocation.SelectedName,
		ParallelToolCallsRequest:      functionToolInvocation.ParallelRequest,
		ParallelToolCallsEffective:    functionToolInvocation.ParallelEffective,
		ParallelToolCallsOrigin:       functionToolInvocation.ParallelOrigin,
		ToolStreamRequested:           functionToolInvocation.ToolStreamRequested,
		SearchRequested:               searchRequested,
		PartialModeRequested:          partialRequested,
		DynamicToolsRequested:         dynamicToolsRequested,
		BuiltinWebSearchRequested:     builtinRequested,
		ImageRequested:                imageRequested,
		AudioRequested:                hasMediaKind(requestedMediaKinds, "audio"),
		VideoRequested:                hasMediaKind(requestedMediaKinds, "video"),
		FilesRequested:                hasMediaKind(requestedMediaKinds, "file"),
		ImageCount:                    imageCount,
		ImageAdmissionTokenUpper:      imageAdmissionTokenUpper,
		ImageAdmissionEstimateVersion: contract.ImageInputPolicy.AdmissionEstimateVersion,
	}
	if builtinRequested {
		if contract.BuiltinSearchRequiresThinkingDisabled && !thinkingExplicitlyDisabled(envelope["thinking"]) {
			return advancedChatProtocolValidation{}, fmt.Errorf("builtin web search requires thinking.type=disabled for protocol profile %s", contract.ProfileID)
		}
		if contract.BuiltinSearchRequiresNonStream && jsonBooleanField(envelope["stream"]) {
			return advancedChatProtocolValidation{}, fmt.Errorf("builtin web search requires stream=false for metered protocol %s", contract.ProfileID)
		}
		if contract.BuiltinSearchRequiresSerial {
			if rawParallel, exists := envelope["parallel_tool_calls"]; exists && !isJSONNull(rawParallel) {
				var parallel bool
				if err := json.Unmarshal(rawParallel, &parallel); err != nil {
					return advancedChatProtocolValidation{}, fmt.Errorf("parallel_tool_calls must be a boolean")
				}
				if parallel {
					return advancedChatProtocolValidation{}, fmt.Errorf("builtin web search requires parallel_tool_calls=false")
				}
			} else {
				result.ProviderRequestNormalizations = append(result.ProviderRequestNormalizations, "parallel_tool_calls=omitted->false")
			}
		}
		result.ReservedMeterUsage = []metering.MeterQuantity{{
			Meter: contract.BuiltinSearchMeter, Quantity: contract.MaxBuiltinSearchCalls,
		}}
	}
	result.ReservedMeterUsage, err = metering.NormalizeMeterQuantities(result.ReservedMeterUsage)
	if err != nil {
		return advancedChatProtocolValidation{}, err
	}
	result.SanitizedBody, err = json.Marshal(envelope)
	if err != nil {
		return advancedChatProtocolValidation{}, fmt.Errorf("encode protocol validation body: %w", err)
	}
	return result, nil
}

var glmFunctionNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func validateModeledFunctionTools(envelope map[string]json.RawMessage, contract modelprofile.ChatProtocolContract) (modeledFunctionToolInvocation, error) {
	result := modeledFunctionToolInvocation{ChoiceRequest: "omitted", ParallelRequest: "omitted"}
	deferDynamicFinalization := contract.DynamicSystemTools && contract.FunctionToolInvocationCapabilitiesModeled
	for _, field := range []string{"functions", "function_call"} {
		if raw, exists := envelope[field]; exists && !isJSONNull(raw) {
			return result, fmt.Errorf("%s is not supported by function-tools protocol profile %s", field, contract.ProfileID)
		}
	}
	toolStreamField := contract.ToolStreamField
	if toolStreamField == "" {
		toolStreamField = "tool_stream"
	}
	if toolStreamField != "" {
		raw, exists := envelope[toolStreamField]
		if exists && !isJSONNull(raw) {
			if !contract.ToolStream || contract.ToolStreamField == "" {
				return result, fmt.Errorf("%s is not supported by function-tools protocol profile %s", toolStreamField, contract.ProfileID)
			}
			var enabled bool
			if err := json.Unmarshal(raw, &enabled); err != nil {
				return result, fmt.Errorf("%s must be a boolean", toolStreamField)
			}
			if enabled && !jsonBooleanField(envelope["stream"]) {
				return result, fmt.Errorf("%s=true requires stream=true for protocol profile %s", toolStreamField, contract.ProfileID)
			}
			result.ToolStreamRequested = enabled
		}
	}
	rawTools, toolsPresent := envelope["tools"]
	if toolsPresent && isJSONNull(rawTools) {
		return result, fmt.Errorf("tools must be an array")
	}
	var tools []map[string]json.RawMessage
	builtinFunctionDeclared := false
	functionNames := make(map[string]struct{})
	if toolsPresent {
		if !contract.FunctionTools {
			return result, fmt.Errorf("function tools are not supported by protocol profile %s", contract.ProfileID)
		}
		if err := json.Unmarshal(rawTools, &tools); err != nil {
			return result, fmt.Errorf("tools must be an array of objects")
		}
		if len(tools) > contract.MaxFunctionTools {
			return result, fmt.Errorf("tools may contain at most %d functions", contract.MaxFunctionTools)
		}
		for index, tool := range tools {
			if err := rejectUnknownJSONFields(tool, "tool", "type", "function"); err != nil {
				return result, fmt.Errorf("tool %d: %w", index, err)
			}
			var kind string
			if err := json.Unmarshal(tool["type"], &kind); err != nil {
				return result, fmt.Errorf("tool %d: type must be a string", index)
			}
			if kind == "builtin_function" && contract.BuiltinWebSearch {
				builtinFunctionDeclared = true
				continue
			}
			if kind != "function" {
				return result, fmt.Errorf("tool %d: only function tools are locally conformant", index)
			}
			var function map[string]json.RawMessage
			if err := json.Unmarshal(tool["function"], &function); err != nil || function == nil {
				return result, fmt.Errorf("tool %d: function must be an object", index)
			}
			allowedFields := []string{"name", "description", "parameters"}
			if contract.FunctionToolStrictSupported {
				allowedFields = append(allowedFields, "strict")
			}
			if err := rejectUnknownJSONFields(function, "function", allowedFields...); err != nil {
				return result, fmt.Errorf("tool %d: %w", index, err)
			}
			var name string
			namePattern := glmFunctionNamePattern
			if contract.FunctionToolNamePattern != "" {
				compiled, err := regexp.Compile(contract.FunctionToolNamePattern)
				if err != nil {
					return result, fmt.Errorf("protocol profile %s has an invalid function name pattern", contract.ProfileID)
				}
				namePattern = compiled
			}
			if err := json.Unmarshal(function["name"], &name); err != nil || !namePattern.MatchString(name) {
				return result, fmt.Errorf("tool %d: function name must match %s", index, namePattern.String())
			}
			if _, duplicate := functionNames[name]; duplicate && contract.FunctionToolInvocationCapabilitiesModeled {
				return result, fmt.Errorf("tool %d: duplicate function name %s", index, name)
			}
			functionNames[name] = struct{}{}
			if contract.FunctionToolDescriptionRequired {
				var description string
				if err := json.Unmarshal(function["description"], &description); err != nil {
					return result, fmt.Errorf("tool %d: function description must be a string", index)
				}
			}
			parameters, parametersExist := function["parameters"]
			if contract.FunctionToolParametersRequired && !parametersExist {
				return result, fmt.Errorf("tool %d: function parameters must be a JSON schema object", index)
			}
			if parametersExist && (contract.FunctionToolParametersRequired || contract.FunctionToolParametersObjectWhenPresent) &&
				(isJSONNull(parameters) || firstJSONToken(parameters) != '{') {
				return result, fmt.Errorf("tool %d: function parameters must be a JSON schema object", index)
			}
			if rawStrict, exists := function["strict"]; exists && !isJSONNull(rawStrict) {
				var strict bool
				if !contract.FunctionToolStrictSupported || json.Unmarshal(rawStrict, &strict) != nil {
					return result, fmt.Errorf("tool %d: function strict must be a boolean supported by the protocol profile", index)
				}
			}
		}
	}
	result.DeclaredNames = make([]string, 0, len(functionNames))
	for name := range functionNames {
		result.DeclaredNames = append(result.DeclaredNames, name)
	}
	slices.Sort(result.DeclaredNames)
	result.ToolsRequested = len(functionNames) > 0
	if contract.StreamContract.FunctionToolDeltas && jsonBooleanField(envelope["stream"]) {
		if raw, present := envelope["n"]; present && strings.TrimSpace(string(raw)) != "1" {
			return result, fmt.Errorf("stream function tools require n=1")
		}
	}
	if result.ToolsRequested && contract.FunctionToolsRequireNonStream && jsonBooleanField(envelope["stream"]) {
		return result, fmt.Errorf("function tools require stream=false for protocol profile %s", contract.ProfileID)
	}
	parallelPresent := false
	if _, exists := envelope["parallel_tool_calls"]; exists && contract.FunctionToolInvocationCapabilitiesModeled &&
		!contract.ParallelFunctionToolCalls && !(builtinFunctionDeclared && contract.BuiltinSearchRequiresSerial) {
		return result, fmt.Errorf("parallel_tool_calls is not supported by function-tools protocol profile %s", contract.ProfileID)
	}
	if raw, exists := envelope["parallel_tool_calls"]; exists && !isJSONNull(raw) {
		parallelPresent = true
		result.ParallelRequest = strings.TrimSpace(string(raw))
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return result, fmt.Errorf("parallel_tool_calls must be a boolean")
		}
		if !contract.ParallelFunctionToolCalls && !(builtinFunctionDeclared && contract.BuiltinSearchRequiresSerial) {
			return result, fmt.Errorf("parallel_tool_calls is not supported by function-tools protocol profile %s", contract.ProfileID)
		}
		result.ParallelEffective = enabled
		result.ParallelOrigin = modelrouting.CapabilityOriginExplicitRequest
	}
	rawChoice, choicePresent := envelope["tool_choice"]
	if !choicePresent {
		if result.ToolsRequested {
			result.ChoiceEffective = contract.FunctionToolProviderDefaultChoice
			if result.ChoiceEffective == "" {
				// Historical immutable contracts predate typed tool defaults and
				// retain their established OpenAI-compatible auto behavior.
				result.ChoiceEffective = "auto"
			}
		} else {
			result.ChoiceEffective = "none"
		}
		result.ChoiceOrigin = modelrouting.CapabilityOriginProviderDefault
	} else {
		if len(contract.FunctionToolChoices) == 0 && !contract.NamedFunctionToolChoice {
			return result, fmt.Errorf("tool_choice is not supported by protocol profile %s", contract.ProfileID)
		}
		if isJSONNull(rawChoice) {
			return result, fmt.Errorf("tool_choice must be one of %s", strings.Join(contract.FunctionToolChoices, ", "))
		}
		result.ChoiceOrigin = modelrouting.CapabilityOriginExplicitRequest
		if firstJSONToken(rawChoice) == '{' {
			if !contract.NamedFunctionToolChoice {
				return result, fmt.Errorf("named tool_choice is not supported by protocol profile %s", contract.ProfileID)
			}
			name, err := measurableToolChoiceName(rawChoice)
			if err != nil {
				return result, err
			}
			if contract.FunctionToolInvocationCapabilitiesModeled {
				if _, exists := functionNames[name]; !exists {
					return result, fmt.Errorf("named tool_choice function %s is not declared", name)
				}
			}
			result.ChoiceRequest = "named"
			result.ChoiceEffective = "named"
			result.SelectedName = name
		} else {
			var choice string
			if err := json.Unmarshal(rawChoice, &choice); err != nil || !slices.Contains(contract.FunctionToolChoices, choice) {
				return result, fmt.Errorf("tool_choice must be one of %s", strings.Join(contract.FunctionToolChoices, ", "))
			}
			result.ChoiceRequest = choice
			result.ChoiceEffective = choice
		}
	}
	requiresTools := contract.FunctionToolChoiceRequiresTools && choicePresent
	if slices.Contains(contract.FunctionToolChoicesRequiringTools, result.ChoiceEffective) {
		requiresTools = true
	}
	if requiresTools && !result.ToolsRequested && !deferDynamicFinalization {
		return result, fmt.Errorf("tool_choice=%s requires at least one function tool", result.ChoiceEffective)
	}
	toolCallsCanOccur := result.ToolsRequested && result.ChoiceEffective != "none"
	if parallelPresent && contract.FunctionToolInvocationCapabilitiesModeled && !toolCallsCanOccur && !deferDynamicFinalization {
		return result, fmt.Errorf("parallel_tool_calls requires callable function tools")
	}
	if !parallelPresent && contract.ParallelFunctionToolCallsDefaultModeled && toolCallsCanOccur {
		result.ParallelEffective = contract.ParallelFunctionToolCallsProviderDefault
		result.ParallelOrigin = modelrouting.CapabilityOriginProviderDefault
	}
	return result, nil
}

func finalizeDynamicFunctionToolInvocation(
	result modeledFunctionToolInvocation,
	dynamicDeclaredNames []string,
	envelope map[string]json.RawMessage,
	contract modelprofile.ChatProtocolContract,
) (modeledFunctionToolInvocation, error) {
	declaredNames := append(slices.Clone(result.DeclaredNames), dynamicDeclaredNames...)
	if len(declaredNames) > contract.MaxFunctionTools {
		return result, fmt.Errorf("top-level and dynamic tools may declare at most %d functions", contract.MaxFunctionTools)
	}
	slices.Sort(declaredNames)
	result.DeclaredNames = declaredNames
	result.ToolsRequested = len(declaredNames) > 0

	if result.ChoiceRequest == "omitted" {
		if result.ToolsRequested {
			result.ChoiceEffective = contract.FunctionToolProviderDefaultChoice
			if result.ChoiceEffective == "" {
				result.ChoiceEffective = "auto"
			}
		} else {
			result.ChoiceEffective = "none"
		}
		result.ChoiceOrigin = modelrouting.CapabilityOriginProviderDefault
	}

	requiresTools := contract.FunctionToolChoiceRequiresTools && result.ChoiceRequest != "omitted"
	if slices.Contains(contract.FunctionToolChoicesRequiringTools, result.ChoiceEffective) {
		requiresTools = true
	}
	if requiresTools && !result.ToolsRequested {
		return result, fmt.Errorf("tool_choice=%s requires at least one function tool", result.ChoiceEffective)
	}
	if result.ToolsRequested && contract.FunctionToolsRequireNonStream && jsonBooleanField(envelope["stream"]) {
		return result, fmt.Errorf("function tools require stream=false for protocol profile %s", contract.ProfileID)
	}
	toolCallsCanOccur := result.ToolsRequested && result.ChoiceEffective != "none"
	if result.ParallelRequest != "omitted" && contract.FunctionToolInvocationCapabilitiesModeled && !toolCallsCanOccur {
		return result, fmt.Errorf("parallel_tool_calls requires callable function tools")
	}
	if result.ParallelRequest == "omitted" && contract.ParallelFunctionToolCallsDefaultModeled && toolCallsCanOccur {
		result.ParallelEffective = contract.ParallelFunctionToolCallsProviderDefault
		result.ParallelOrigin = modelrouting.CapabilityOriginProviderDefault
	}
	return result, nil
}

func measurableToolChoiceName(raw json.RawMessage) (string, error) {
	if err := validateMeasurableToolChoice(raw); err != nil {
		return "", err
	}
	var choice struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &choice); err != nil || strings.TrimSpace(choice.Function.Name) == "" {
		return "", fmt.Errorf("named tool_choice function name is required")
	}
	return choice.Function.Name, nil
}

func validateExactFunctionToolHistory(messages []map[string]json.RawMessage, contract modelprofile.ChatProtocolContract) error {
	declaredCalls := make(map[string]struct{})
	pendingCalls := make(map[string]struct{})
	for index, message := range messages {
		role, err := requiredMessageRole(message)
		if err != nil {
			return fmt.Errorf("message %d: %w", index, err)
		}
		if raw, present := message["function_call"]; present && !isJSONNull(raw) {
			return fmt.Errorf("message %d legacy function_call history is not supported", index)
		}
		if role != "tool" && len(pendingCalls) > 0 {
			return fmt.Errorf("message %d appears before prior assistant tool_calls are resolved", index)
		}
		switch role {
		case "system", "user":
			if err := rejectUnknownJSONFields(message, role+" message", "role", "content", "name"); err != nil {
				return fmt.Errorf("message %d: %w", index, err)
			}
		case "assistant":
			if err := rejectUnknownJSONFields(message, "assistant message", "role", "content", "reasoning_content", "tool_calls"); err != nil {
				return fmt.Errorf("message %d: %w", index, err)
			}
			if rawReasoning, present := message["reasoning_content"]; present && !isJSONNull(rawReasoning) {
				var reasoning string
				if json.Unmarshal(rawReasoning, &reasoning) != nil {
					return fmt.Errorf("message %d assistant reasoning_content must be a string or null", index)
				}
			}
			rawCalls, hasCalls := message["tool_calls"]
			hasCalls = hasCalls && !isJSONNull(rawCalls)
			hasContent := false
			if rawContent, present := message["content"]; present && !isJSONNull(rawContent) {
				var content string
				if json.Unmarshal(rawContent, &content) != nil {
					return fmt.Errorf("message %d assistant content must be a string or null", index)
				}
				hasContent = true
			}
			if !hasContent && !hasCalls {
				return fmt.Errorf("message %d assistant requires content or tool_calls", index)
			}
			if !hasCalls {
				continue
			}
			var calls []map[string]json.RawMessage
			if json.Unmarshal(rawCalls, &calls) != nil || len(calls) == 0 {
				return fmt.Errorf("message %d assistant tool_calls must be a non-empty array", index)
			}
			for callIndex, call := range calls {
				if err := rejectUnknownJSONFields(call, "assistant tool_call", "id", "type", "function"); err != nil {
					return fmt.Errorf("message %d tool_call %d: %w", index, callIndex, err)
				}
				var id, kind string
				if json.Unmarshal(call["id"], &id) != nil || strings.TrimSpace(id) == "" {
					return fmt.Errorf("message %d tool_call %d id must be a non-empty string", index, callIndex)
				}
				if _, duplicate := declaredCalls[id]; duplicate {
					return fmt.Errorf("message %d tool_call %d id is duplicated", index, callIndex)
				}
				if json.Unmarshal(call["type"], &kind) != nil || kind != "function" {
					return fmt.Errorf("message %d tool_call %d type must be function", index, callIndex)
				}
				if err := validateMeasurableFunctionCall(call["function"]); err != nil {
					return fmt.Errorf("message %d tool_call %d: %w", index, callIndex, err)
				}
				var function struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(call["function"], &function) != nil || strings.TrimSpace(function.Name) == "" {
					return fmt.Errorf("message %d tool_call %d function name must be non-empty", index, callIndex)
				}
				if contract.FunctionToolNamePattern != "" {
					pattern, compileErr := regexp.Compile(contract.FunctionToolNamePattern)
					if compileErr != nil || !pattern.MatchString(function.Name) {
						return fmt.Errorf("message %d tool_call %d function name must match %s", index, callIndex, contract.FunctionToolNamePattern)
					}
				}
				declaredCalls[id] = struct{}{}
				pendingCalls[id] = struct{}{}
			}
		case "tool":
			if err := rejectUnknownJSONFields(message, "tool message", "role", "content", "tool_call_id", "name"); err != nil {
				return fmt.Errorf("message %d: %w", index, err)
			}
			var callID string
			if json.Unmarshal(message["tool_call_id"], &callID) != nil || strings.TrimSpace(callID) == "" {
				return fmt.Errorf("message %d tool_call_id must be a non-empty string", index)
			}
			if _, exists := pendingCalls[callID]; !exists {
				return fmt.Errorf("message %d tool_call_id does not reference prior assistant tool_calls", index)
			}
			var content string
			if json.Unmarshal(message["content"], &content) != nil {
				return fmt.Errorf("message %d tool content must be a string", index)
			}
			if rawName, present := message["name"]; present && !isJSONNull(rawName) {
				var name string
				if json.Unmarshal(rawName, &name) != nil || strings.TrimSpace(name) == "" {
					return fmt.Errorf("message %d tool name must be a non-empty string", index)
				}
			}
			delete(pendingCalls, callID)
		default:
			return fmt.Errorf("message %d role %q is not supported by exact function-tool history", index, role)
		}
	}
	if len(pendingCalls) > 0 {
		return fmt.Errorf("assistant tool_calls must be followed by matching tool messages")
	}
	return nil
}

func exactFunctionToolHistoryRequested(messages []map[string]json.RawMessage) bool {
	for _, message := range messages {
		var role string
		if json.Unmarshal(message["role"], &role) != nil || role != "assistant" {
			continue
		}
		if rawCalls, present := message["tool_calls"]; present && !isJSONNull(rawCalls) {
			var calls []json.RawMessage
			if json.Unmarshal(rawCalls, &calls) == nil && len(calls) > 0 {
				return true
			}
		}
	}
	return false
}

func validateModeledResponseFormat(envelope map[string]json.RawMessage, contract modelprofile.ChatProtocolContract) error {
	for _, field := range []string{
		"guided_json", "guided_regex", "guided_choice", "guided_grammar", "guided_decoding_backend",
	} {
		if raw, exists := envelope[field]; exists && !isJSONNull(raw) {
			return fmt.Errorf("%s is not supported by response-format protocol profile %s", field, contract.ProfileID)
		}
	}
	rawFormat, exists := envelope["response_format"]
	if !exists {
		return nil
	}
	if isJSONNull(rawFormat) {
		return fmt.Errorf("response_format must be an object")
	}
	var format map[string]json.RawMessage
	if err := json.Unmarshal(rawFormat, &format); err != nil || format == nil {
		return fmt.Errorf("response_format must be an object")
	}
	if err := rejectUnknownJSONFields(format, "response_format", "type", "json_schema"); err != nil {
		return err
	}
	var kind string
	if err := json.Unmarshal(format["type"], &kind); err != nil || !slices.Contains(contract.ResponseFormatTypes, kind) {
		return fmt.Errorf("response_format.type must be one of %s", strings.Join(contract.ResponseFormatTypes, ", "))
	}
	if err := validateMeasurableResponseFormat(rawFormat); err != nil {
		return err
	}
	if kind == "json_object" && contract.JSONObjectPromptKeyword != "" && !messagesContainKeyword(envelope["messages"], contract.JSONObjectPromptKeyword) {
		return fmt.Errorf("messages must contain %q for response_format.type=json_object", contract.JSONObjectPromptKeyword)
	}
	if kind != "json_schema" {
		return nil
	}
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(format["json_schema"], &definition); err != nil || definition == nil {
		return fmt.Errorf("response_format json_schema must be an object")
	}
	if contract.JSONSchemaRequiresNonStream && jsonBooleanField(envelope["stream"]) {
		return fmt.Errorf("response_format.type=json_schema requires stream=false for protocol profile %s", contract.ProfileID)
	}
	if contract.JSONSchemaDisallowsFunctionTools {
		if rawTools, exists := envelope["tools"]; exists && !isJSONNull(rawTools) {
			var tools []json.RawMessage
			if json.Unmarshal(rawTools, &tools) != nil || len(tools) > 0 {
				return fmt.Errorf("response_format.type=json_schema cannot be combined with function tools for protocol profile %s", contract.ProfileID)
			}
		}
		for _, field := range []string{"tool_choice", "parallel_tool_calls"} {
			if raw, exists := envelope[field]; exists && !isJSONNull(raw) {
				return fmt.Errorf("response_format.type=json_schema cannot be combined with %s for protocol profile %s", field, contract.ProfileID)
			}
		}
	}
	for _, field := range contract.JSONSchemaIncompatibleRequestFields {
		if raw, exists := envelope[field]; exists && !isJSONNull(raw) {
			return fmt.Errorf("response_format.type=json_schema cannot be combined with %s for protocol profile %s", field, contract.ProfileID)
		}
	}
	if contract.JSONSchemaDescriptionStringWhenPresent {
		if description, exists := definition["description"]; exists && (isJSONNull(description) || !rawJSONString(description)) {
			return fmt.Errorf("response_format json_schema description must be a string when present")
		}
	}
	if contract.JSONSchemaStrictRequired {
		var strict bool
		if rawStrict, exists := definition["strict"]; !exists || json.Unmarshal(rawStrict, &strict) != nil || !strict {
			return fmt.Errorf("response_format json_schema strict must be true")
		}
	}
	if contract.JSONSchemaObjectRequired {
		if rawSchema, exists := definition["schema"]; !exists || isJSONNull(rawSchema) || firstJSONToken(rawSchema) != '{' {
			return fmt.Errorf("response_format schema must be a JSON schema object")
		}
	}
	if contract.JSONSchemaAdditionalPropertiesFalseRequired {
		var schema map[string]json.RawMessage
		if rawSchema, exists := definition["schema"]; !exists || json.Unmarshal(rawSchema, &schema) != nil || schema == nil {
			return fmt.Errorf("response_format schema must be a JSON schema object")
		}
		var additionalProperties bool
		if raw, exists := schema["additionalProperties"]; !exists || json.Unmarshal(raw, &additionalProperties) != nil || additionalProperties {
			return fmt.Errorf("response_format schema additionalProperties must be false")
		}
	}
	return nil
}

func messagesContainKeyword(rawMessages json.RawMessage, keyword string) bool {
	if len(rawMessages) == 0 || isJSONNull(rawMessages) || keyword == "" {
		return false
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(rawMessages, &messages) != nil {
		return false
	}
	needle := strings.ToLower(keyword)
	for _, message := range messages {
		rawContent, exists := message["content"]
		if !exists || isJSONNull(rawContent) {
			continue
		}
		var content string
		if json.Unmarshal(rawContent, &content) == nil && strings.Contains(strings.ToLower(content), needle) {
			return true
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(rawContent, &parts) != nil {
			continue
		}
		for _, part := range parts {
			var text string
			if json.Unmarshal(part["text"], &text) == nil && strings.Contains(strings.ToLower(text), needle) {
				return true
			}
		}
	}
	return false
}

func validateModeledPromptCache(envelope map[string]json.RawMessage, messages []map[string]json.RawMessage, contract modelprofile.ChatProtocolContract) error {
	if field := contract.PromptCacheOptionsField; field != "" {
		if rawOptions, exists := envelope[field]; exists && !isJSONNull(rawOptions) {
			var options map[string]json.RawMessage
			if json.Unmarshal(rawOptions, &options) != nil || options == nil {
				return fmt.Errorf("%s must be an object", field)
			}
			if err := rejectUnknownJSONFields(options, field, "mode", "ttl"); err != nil {
				return err
			}
			if rawMode, exists := options["mode"]; exists {
				var mode string
				if isJSONNull(rawMode) || json.Unmarshal(rawMode, &mode) != nil || !slices.Contains(contract.PromptCacheModes, mode) {
					return fmt.Errorf("%s.mode must be one of %s", field, strings.Join(contract.PromptCacheModes, ", "))
				}
			}
			if rawTTL, exists := options["ttl"]; exists {
				var ttl string
				if isJSONNull(rawTTL) || json.Unmarshal(rawTTL, &ttl) != nil || !slices.Contains(contract.PromptCacheTTLs, ttl) {
					return fmt.Errorf("%s.ttl must be one of %s", field, strings.Join(contract.PromptCacheTTLs, ", "))
				}
			}
		}
		// The original body still goes to the provider. Remove provider-only
		// controls only from the strict admission shadow body.
		delete(envelope, field)
	}

	controlField := "cache_control"
	if contract.PromptCacheBreakpointField != "" {
		controlField = contract.PromptCacheBreakpointField
	}
	markers := 0
	for messageIndex, message := range messages {
		rawContent, exists := message["content"]
		if !exists || isJSONNull(rawContent) || firstJSONToken(rawContent) != '[' {
			continue
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(rawContent, &parts) != nil {
			continue
		}
		changed := false
		for partIndex, part := range parts {
			rawControl, exists := part[controlField]
			if !exists {
				continue
			}
			if isJSONNull(rawControl) {
				if contract.PromptCacheBreakpointField != "" {
					delete(part, controlField)
					changed = true
				}
				continue
			}
			if !contract.ExplicitPromptCache {
				return fmt.Errorf("explicit prompt cache is not supported by protocol profile %s", contract.ProfileID)
			}
			var control map[string]json.RawMessage
			if json.Unmarshal(rawControl, &control) != nil || control == nil {
				return fmt.Errorf("message %d content part %d %s must be an object", messageIndex, partIndex, controlField)
			}
			if contract.PromptCacheBreakpointField != "" {
				if err := rejectUnknownJSONFields(control, controlField, "mode"); err != nil {
					return fmt.Errorf("message %d content part %d: %w", messageIndex, partIndex, err)
				}
				var mode string
				if rawMode, exists := control["mode"]; !exists || json.Unmarshal(rawMode, &mode) != nil || mode != "explicit" {
					return fmt.Errorf("message %d content part %d %s.mode must be %q", messageIndex, partIndex, controlField, "explicit")
				}
				delete(part, controlField)
				changed = true
			} else {
				if err := rejectUnknownJSONFields(control, "cache_control", "type"); err != nil {
					return fmt.Errorf("message %d content part %d: %w", messageIndex, partIndex, err)
				}
				var kind string
				if json.Unmarshal(control["type"], &kind) != nil || kind != contract.PromptCacheControlType {
					return fmt.Errorf("message %d content part %d cache_control.type must be %q", messageIndex, partIndex, contract.PromptCacheControlType)
				}
			}
			markers++
		}
		if changed {
			wire, err := json.Marshal(parts)
			if err != nil {
				return fmt.Errorf("message %d content: encode cache validation shadow: %w", messageIndex, err)
			}
			message["content"] = wire
		}
	}
	if markers > contract.MaxPromptCacheMarkers {
		return fmt.Errorf("messages may contain at most %d %s markers", contract.MaxPromptCacheMarkers, controlField)
	}
	return nil
}

func validateModeledSearchRequest(envelope map[string]json.RawMessage, contract modelprofile.ChatProtocolContract) (bool, error) {
	enableField := contract.SearchEnableField
	optionsField := contract.SearchOptionsField
	if enableField == "" {
		enableField = "enable_search"
	}
	if optionsField == "" {
		optionsField = "search_options"
	}
	rawEnabled, enabledPresent := envelope[enableField]
	enabled := false
	if enabledPresent && !isJSONNull(rawEnabled) {
		if json.Unmarshal(rawEnabled, &enabled) != nil {
			return false, fmt.Errorf("%s must be a boolean", enableField)
		}
		if enabled && !contract.BuiltinSearchRequest {
			return false, fmt.Errorf("builtin search is not supported by protocol profile %s", contract.ProfileID)
		}
	}
	rawOptions, optionsPresent := envelope[optionsField]
	if !optionsPresent || isJSONNull(rawOptions) {
		return enabled, nil
	}
	var options map[string]json.RawMessage
	if json.Unmarshal(rawOptions, &options) != nil || options == nil {
		return false, fmt.Errorf("%s must be an object", optionsField)
	}
	allowedFields := make([]string, 0, 4)
	for _, field := range []string{contract.SearchOptionEnableField, contract.SearchForcedField, contract.SearchStrategyField} {
		if field != "" {
			allowedFields = append(allowedFields, field)
		}
	}
	if contract.SearchUserLocation {
		allowedFields = append(allowedFields, "user_location")
	}
	if err := rejectUnknownJSONFields(options, optionsField, allowedFields...); err != nil {
		return false, err
	}
	if contract.SearchOptionEnableField != "" {
		rawOptionEnabled, exists := options[contract.SearchOptionEnableField]
		if !exists || isJSONNull(rawOptionEnabled) || json.Unmarshal(rawOptionEnabled, &enabled) != nil || !enabled {
			return false, fmt.Errorf("%s.%s must be true", optionsField, contract.SearchOptionEnableField)
		}
	}
	if !enabled {
		return false, fmt.Errorf("%s requires %s=true", optionsField, enableField)
	}
	if contract.SearchForcedField != "" {
		if rawForced, exists := options[contract.SearchForcedField]; exists && !isJSONNull(rawForced) {
			var forced bool
			if json.Unmarshal(rawForced, &forced) != nil || (!contract.ForcedSearch && forced) {
				return false, fmt.Errorf("%s.%s must be a supported boolean", optionsField, contract.SearchForcedField)
			}
		}
	}
	if contract.SearchStrategyField != "" {
		if rawStrategy, exists := options[contract.SearchStrategyField]; exists && !isJSONNull(rawStrategy) {
			var strategy string
			if json.Unmarshal(rawStrategy, &strategy) != nil || !slices.Contains(contract.SearchStrategies, strategy) {
				return false, fmt.Errorf("%s.%s must be one of %s", optionsField, contract.SearchStrategyField, strings.Join(contract.SearchStrategies, ", "))
			}
		}
	}
	if rawLocation, exists := options["user_location"]; exists && !isJSONNull(rawLocation) {
		if !contract.SearchUserLocation {
			return false, fmt.Errorf("%s.user_location is not supported", optionsField)
		}
		var location map[string]json.RawMessage
		if json.Unmarshal(rawLocation, &location) != nil || location == nil {
			return false, fmt.Errorf("%s.user_location must be an object", optionsField)
		}
		if err := rejectUnknownJSONFields(location, optionsField+".user_location", "type", "country", "region", "city", "timezone"); err != nil {
			return false, err
		}
		var kind string
		if rawType, exists := location["type"]; !exists || json.Unmarshal(rawType, &kind) != nil || kind != "approximate" {
			return false, fmt.Errorf("%s.user_location.type must be approximate", optionsField)
		}
	}
	return true, nil
}

func responseFormatType(raw json.RawMessage) string {
	var value struct {
		Type string `json:"type"`
	}
	if len(raw) == 0 || isJSONNull(raw) || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value.Type
}

func validateDynamicSystemTools(raw json.RawMessage) ([]string, error) {
	if err := validateMeasurableTools(raw); err != nil {
		return nil, err
	}
	var tools []struct {
		Function map[string]json.RawMessage `json:"function"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("tools must be an array")
	}
	declaredNames := make([]string, 0, len(tools))
	for index, tool := range tools {
		var name string
		if json.Unmarshal(tool.Function["name"], &name) != nil {
			return nil, fmt.Errorf("tool %d function name must be a string", index)
		}
		declaredNames = append(declaredNames, name)
		description, hasDescription := tool.Function["description"]
		if !hasDescription || isJSONNull(description) || !rawJSONString(description) {
			return nil, fmt.Errorf("tool %d function description must be a string", index)
		}
		parameters, hasParameters := tool.Function["parameters"]
		if !hasParameters || isJSONNull(parameters) || firstJSONToken(parameters) != '{' {
			return nil, fmt.Errorf("tool %d function parameters must be a JSON schema object", index)
		}
	}
	return declaredNames, nil
}

func thinkingExplicitlyDisabled(raw json.RawMessage) bool {
	var thinking struct {
		Type string `json:"type"`
	}
	return len(raw) > 0 && !isJSONNull(raw) && json.Unmarshal(raw, &thinking) == nil && thinking.Type == "disabled"
}

func requiredMessageRole(message map[string]json.RawMessage) (string, error) {
	var role string
	if raw, exists := message["role"]; !exists || json.Unmarshal(raw, &role) != nil || strings.TrimSpace(role) == "" {
		return "", fmt.Errorf("message role must be a string")
	}
	return role, nil
}

type imageContentValidation struct {
	Requested           bool
	Count               int
	AdmissionTokenUpper int64
}

func sanitizeAdvancedContentParts(
	raw json.RawMessage,
	messageRole string,
	contract modelprofile.ChatProtocolContract,
) (json.RawMessage, imageContentValidation, error) {
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, imageContentValidation{}, fmt.Errorf("content parts must be objects")
	}
	var imageValidation imageContentValidation
	for index := range parts {
		var kind string
		if rawType, exists := parts[index]["type"]; !exists || json.Unmarshal(rawType, &kind) != nil {
			continue
		}
		switch kind {
		case "image_url":
			if !contract.InlineImageDataURL && !contract.RemoteImageHTTPSURL {
				return nil, imageContentValidation{}, fmt.Errorf("image input is not supported by protocol profile %s", contract.ProfileID)
			}
			tokenUpper, err := validateImagePart(parts[index], messageRole, contract)
			if err != nil {
				return nil, imageContentValidation{}, fmt.Errorf("part %d: %w", index, err)
			}
			imageValidation.Requested = true
			imageValidation.Count++
			if tokenUpper > 0 {
				if imageValidation.AdmissionTokenUpper > (1<<63-1)-tokenUpper {
					return nil, imageContentValidation{}, fmt.Errorf("part %d: image admission estimate overflow", index)
				}
				imageValidation.AdmissionTokenUpper += tokenUpper
			}
			parts[index] = map[string]json.RawMessage{
				"type": json.RawMessage(`"text"`), "text": json.RawMessage(`""`),
			}
		case "input_audio":
			if !contract.InlineAudioBase64 && !contract.RemoteAudioHTTPSURL {
				return nil, imageContentValidation{}, fmt.Errorf("audio input is not supported by protocol profile %s", contract.ProfileID)
			}
			if err := validateAudioPart(
				parts[index], contract.InlineAudioBase64, contract.RemoteAudioHTTPSURL, contract.InlineAudioFormats,
			); err != nil {
				return nil, imageContentValidation{}, fmt.Errorf("part %d: %w", index, err)
			}
			parts[index] = map[string]json.RawMessage{
				"type": json.RawMessage(`"text"`), "text": json.RawMessage(`""`),
			}
		case "video_url":
			if !contract.InlineVideoDataURL && !contract.RemoteVideoHTTPSURL {
				return nil, imageContentValidation{}, fmt.Errorf("video input is not supported by protocol profile %s", contract.ProfileID)
			}
			if err := validateVideoPart(parts[index], contract); err != nil {
				return nil, imageContentValidation{}, fmt.Errorf("part %d: %w", index, err)
			}
			parts[index] = map[string]json.RawMessage{
				"type": json.RawMessage(`"text"`), "text": json.RawMessage(`""`),
			}
		case "file_url":
			if !contract.RemoteFileHTTPSURL {
				return nil, imageContentValidation{}, fmt.Errorf("file input is not supported by protocol profile %s", contract.ProfileID)
			}
			if err := validateMediaPart(parts[index], "file_url", false, true, nil, contract.RemoteFileExtensions); err != nil {
				return nil, imageContentValidation{}, fmt.Errorf("part %d: %w", index, err)
			}
			parts[index] = map[string]json.RawMessage{
				"type": json.RawMessage(`"text"`), "text": json.RawMessage(`""`),
			}
		}
	}
	sanitized, err := json.Marshal(parts)
	if err != nil {
		return nil, imageContentValidation{}, err
	}
	return sanitized, imageValidation, nil
}

func validateImagePart(part map[string]json.RawMessage, messageRole string, contract modelprofile.ChatProtocolContract) (int64, error) {
	policy := contract.ImageInputPolicy
	if policy.Modeled {
		if !slices.Contains(policy.AllowedMessageRoles, messageRole) {
			return 0, fmt.Errorf("image input is not supported on %s messages by protocol profile %s", messageRole, contract.ProfileID)
		}
		return validateExactInlineJPEG(part, contract)
	}
	if len(contract.ImageDetails) == 0 {
		if err := validateMediaPart(part, "image_url", contract.InlineImageDataURL, contract.RemoteImageHTTPSURL, contract.InlineImageMIMETypes, nil); err != nil {
			return 0, err
		}
		return 0, nil
	}
	if err := rejectUnknownJSONFields(part, "image content part", "type", "image_url"); err != nil {
		return 0, err
	}
	rawImage, exists := part["image_url"]
	if !exists || isJSONNull(rawImage) {
		return 0, fmt.Errorf("image_url is required")
	}
	if rawJSONString(rawImage) {
		if err := validateMediaPart(part, "image_url", contract.InlineImageDataURL, contract.RemoteImageHTTPSURL, contract.InlineImageMIMETypes, nil); err != nil {
			return 0, err
		}
		return 0, nil
	}
	var image map[string]json.RawMessage
	if json.Unmarshal(rawImage, &image) != nil || image == nil {
		return 0, fmt.Errorf("image_url must be a string or url object")
	}
	if err := rejectUnknownJSONFields(image, "image_url", "url", "detail"); err != nil {
		return 0, err
	}
	if rawDetail, exists := image["detail"]; exists {
		var detail string
		if isJSONNull(rawDetail) || json.Unmarshal(rawDetail, &detail) != nil || !slices.Contains(contract.ImageDetails, detail) {
			return 0, fmt.Errorf("image_url.detail must be one of %s", strings.Join(contract.ImageDetails, ", "))
		}
	}
	imageURLOnly, err := json.Marshal(map[string]json.RawMessage{"url": image["url"]})
	if err != nil {
		return 0, fmt.Errorf("encode image_url validation shadow: %w", err)
	}
	mediaOnly := map[string]json.RawMessage{"type": part["type"], "image_url": imageURLOnly}
	if err := validateMediaPart(mediaOnly, "image_url", contract.InlineImageDataURL, contract.RemoteImageHTTPSURL, contract.InlineImageMIMETypes, nil); err != nil {
		return 0, err
	}
	return 0, nil
}

func validateExactInlineJPEG(part map[string]json.RawMessage, contract modelprofile.ChatProtocolContract) (int64, error) {
	policy := contract.ImageInputPolicy
	if policy.DecodedFormat != "jpeg" {
		return 0, fmt.Errorf("image decoded format %q is not supported", policy.DecodedFormat)
	}
	if err := rejectUnknownJSONFields(part, "image content part", "type", "image_url"); err != nil {
		return 0, err
	}
	rawImage, exists := part["image_url"]
	if !exists || isJSONNull(rawImage) {
		return 0, fmt.Errorf("image_url is required")
	}
	if policy.RequireURLObject && rawJSONString(rawImage) {
		return 0, fmt.Errorf("image_url must use an object with a url field")
	}
	var image map[string]json.RawMessage
	if err := json.Unmarshal(rawImage, &image); err != nil || image == nil {
		return 0, fmt.Errorf("image_url must be an object with a url field")
	}
	if err := rejectUnknownJSONFields(image, "image_url", "url"); err != nil {
		return 0, err
	}
	var value string
	if rawURL, ok := image["url"]; !ok || json.Unmarshal(rawURL, &value) != nil || value == "" {
		return 0, fmt.Errorf("image_url.url must be a non-empty string")
	}
	if !strings.HasPrefix(value, "data:") {
		if !contract.RemoteImageHTTPSURL {
			return 0, fmt.Errorf("image_url currently supports only inline data URLs")
		}
		return 0, fmt.Errorf("exact inline JPEG policy does not accept remote image URLs")
	}
	if !contract.InlineImageDataURL {
		return 0, fmt.Errorf("image_url inline data URLs are not supported by the protocol profile")
	}
	separator := strings.Index(value, ";base64,")
	if separator <= len("data:") || separator+8 >= len(value) {
		return 0, fmt.Errorf("image_url must contain a non-empty base64 payload")
	}
	mimeType := strings.ToLower(strings.TrimSpace(value[len("data:"):separator]))
	if !slices.Contains(contract.InlineImageMIMETypes, mimeType) {
		return 0, fmt.Errorf("image_url MIME type %s is not supported by the protocol profile", mimeType)
	}
	payload := value[separator+8:]
	if int64(base64.StdEncoding.DecodedLen(len(payload))) > policy.MaximumDecodedBytes+2 {
		return 0, fmt.Errorf("image decoded bytes must not exceed %d", policy.MaximumDecodedBytes)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil {
		return 0, fmt.Errorf("image_url contains invalid base64 data")
	}
	if int64(len(decoded)) > policy.MaximumDecodedBytes {
		return 0, fmt.Errorf("image decoded bytes must not exceed %d", policy.MaximumDecodedBytes)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(decoded))
	if err != nil {
		return 0, fmt.Errorf("image_url payload must be a valid JPEG image")
	}
	if config.Width < policy.MinimumWidth || config.Height < policy.MinimumHeight {
		return 0, fmt.Errorf("image dimensions must be at least %dx%d", policy.MinimumWidth, policy.MinimumHeight)
	}
	pixels := int64(config.Width) * int64(config.Height)
	if pixels < policy.MinimumPixels || pixels > policy.MaximumPixels {
		return 0, fmt.Errorf("image pixels must be between %d and %d", policy.MinimumPixels, policy.MaximumPixels)
	}
	if int64(config.Width) > int64(config.Height)*policy.MaximumAspectRatio ||
		int64(config.Height) > int64(config.Width)*policy.MaximumAspectRatio {
		return 0, fmt.Errorf("image aspect ratio must not exceed %d:1", policy.MaximumAspectRatio)
	}
	decodedImage, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		return 0, fmt.Errorf("image_url payload must be a complete JPEG image")
	}
	if bounds := decodedImage.Bounds(); bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		return 0, fmt.Errorf("image JPEG dimensions are inconsistent")
	}
	if pixels > (1<<63-1)/policy.AdmissionTokenUpperPerPixel {
		return 0, fmt.Errorf("image admission estimate overflow")
	}
	return pixels * policy.AdmissionTokenUpperPerPixel, nil
}

func validateImageRequestCombination(
	envelope map[string]json.RawMessage,
	messages []map[string]json.RawMessage,
	streamRequested bool,
	imageCount int,
	contract modelprofile.ChatProtocolContract,
) error {
	policy := contract.ImageInputPolicy
	if !policy.Modeled {
		return nil
	}
	if policy.RequiresNonStream && streamRequested {
		return fmt.Errorf("image input requires stream=false for protocol profile %s", contract.ProfileID)
	}
	if imageCount <= 0 || imageCount > policy.MaximumImages {
		return fmt.Errorf("image input count must be between 1 and %d for protocol profile %s", policy.MaximumImages, contract.ProfileID)
	}
	for _, field := range policy.IncompatibleRequestFields {
		if raw, exists := envelope[field]; exists && !isJSONNull(raw) {
			return fmt.Errorf("image input is incompatible with %s for protocol profile %s", field, contract.ProfileID)
		}
	}
	if raw, exists := envelope["response_format"]; exists && !isJSONNull(raw) && responseFormatType(raw) != "text" {
		return fmt.Errorf("image input supports only text response format for protocol profile %s", contract.ProfileID)
	}
	for index, message := range messages {
		role, err := requiredMessageRole(message)
		if err != nil {
			return fmt.Errorf("message %d: %w", index, err)
		}
		if role == "tool" || role == "function" || hasAnyNonNullJSONField(message, "tools", "tool_calls", "function_call") {
			return fmt.Errorf("image input is incompatible with function-tool history for protocol profile %s", contract.ProfileID)
		}
	}
	return nil
}

func validateAudioPart(part map[string]json.RawMessage, allowInlineBase64, allowRemoteHTTPS bool, allowedFormats []string) error {
	if err := rejectUnknownJSONFields(part, "audio content part", "type", "input_audio"); err != nil {
		return err
	}
	rawAudio, exists := part["input_audio"]
	if !exists || isJSONNull(rawAudio) {
		return fmt.Errorf("input_audio is required")
	}
	var audio map[string]json.RawMessage
	if err := json.Unmarshal(rawAudio, &audio); err != nil || audio == nil {
		return fmt.Errorf("input_audio must be an object")
	}
	allowedFields := []string{"data"}
	if len(allowedFormats) > 0 {
		allowedFields = append(allowedFields, "format")
	}
	if err := rejectUnknownJSONFields(audio, "input_audio", allowedFields...); err != nil {
		return err
	}
	var data string
	if rawData, ok := audio["data"]; !ok || json.Unmarshal(rawData, &data) != nil || data == "" {
		return fmt.Errorf("input_audio.data must be a non-empty base64 string")
	}
	if strings.HasPrefix(data, "data:") {
		if !allowInlineBase64 {
			return fmt.Errorf("input_audio.data inline base64 is not supported by the protocol profile")
		}
		separator := strings.Index(data, ";base64,")
		if separator <= len("data:") || separator+8 >= len(data) {
			return fmt.Errorf("input_audio.data must contain a non-empty base64 payload")
		}
		if !strings.HasPrefix(strings.ToLower(data[len("data:"):separator]), "audio/") {
			return fmt.Errorf("input_audio.data must use an audio MIME type")
		}
		data = data[separator+8:]
	} else if remote, err := url.Parse(data); err == nil && remote.Scheme != "" {
		if !allowRemoteHTTPS || remote.Scheme != "https" || remote.Host == "" || remote.User != nil {
			return fmt.Errorf("input_audio.data remote URL must be an absolute HTTPS URL without user information")
		}
		if len(allowedFormats) > 0 {
			return fmt.Errorf("input_audio.data remote URLs cannot be combined with format")
		}
		return nil
	}
	if !allowInlineBase64 {
		return fmt.Errorf("input_audio.data inline base64 is not supported by the protocol profile")
	}
	decoder := base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(data))
	if _, err := io.Copy(io.Discard, decoder); err != nil {
		return fmt.Errorf("input_audio.data contains invalid base64 data")
	}
	if len(allowedFormats) > 0 {
		var format string
		if rawFormat, ok := audio["format"]; !ok || json.Unmarshal(rawFormat, &format) != nil || format == "" {
			return fmt.Errorf("input_audio.format must be a string")
		}
		format = strings.ToLower(strings.TrimSpace(format))
		if !slices.Contains(allowedFormats, format) {
			return fmt.Errorf("input_audio.format must be one of %s", strings.Join(allowedFormats, ", "))
		}
	}
	return nil
}

func validateVideoPart(part map[string]json.RawMessage, contract modelprofile.ChatProtocolContract) error {
	allowedFields := []string{"type", "video_url"}
	if contract.VideoFPSSupported {
		allowedFields = append(allowedFields, "fps", "media_resolution")
	}
	if err := rejectUnknownJSONFields(part, "video content part", allowedFields...); err != nil {
		return err
	}
	mediaOnly := map[string]json.RawMessage{"type": part["type"], "video_url": part["video_url"]}
	if err := validateMediaPart(
		mediaOnly, "video_url", contract.InlineVideoDataURL, contract.RemoteVideoHTTPSURL, contract.InlineVideoMIMETypes, nil,
	); err != nil {
		return err
	}
	if rawFPS, exists := part["fps"]; exists {
		var fps float64
		if isJSONNull(rawFPS) || json.Unmarshal(rawFPS, &fps) != nil || fps < contract.VideoFPSMinimum || fps > contract.VideoFPSMaximum {
			return fmt.Errorf("fps must be between %g and %g", contract.VideoFPSMinimum, contract.VideoFPSMaximum)
		}
	}
	if rawResolution, exists := part["media_resolution"]; exists {
		var resolution string
		if isJSONNull(rawResolution) || json.Unmarshal(rawResolution, &resolution) != nil || !slices.Contains(contract.VideoResolutions, resolution) {
			return fmt.Errorf("media_resolution must be one of %s", strings.Join(contract.VideoResolutions, ", "))
		}
	}
	return nil
}

func mediaKindsInContentParts(raw json.RawMessage) (map[string]struct{}, error) {
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("content parts must be objects")
	}
	kinds := make(map[string]struct{})
	for _, part := range parts {
		var kind string
		if rawType, exists := part["type"]; !exists || json.Unmarshal(rawType, &kind) != nil {
			continue
		}
		switch kind {
		case "image_url":
			kinds["image"] = struct{}{}
		case "input_audio":
			kinds["audio"] = struct{}{}
		case "video_url":
			kinds["video"] = struct{}{}
		case "file_url":
			kinds["file"] = struct{}{}
		}
	}
	return kinds, nil
}

func hasMediaKind(kinds map[string]struct{}, kind string) bool {
	_, ok := kinds[kind]
	return ok
}

func validateMediaPart(part map[string]json.RawMessage, field string, allowInlineDataURL, allowRemoteHTTPSURL bool, allowedMIMETypes, allowedRemoteExtensions []string) error {
	if err := rejectUnknownJSONFields(part, "media content part", "type", field); err != nil {
		return err
	}
	raw, exists := part[field]
	if !exists || isJSONNull(raw) {
		return fmt.Errorf("%s is required", field)
	}
	var value string
	if rawJSONString(raw) {
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("%s must be a string or url object", field)
		}
	} else {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			return fmt.Errorf("%s must be a string or url object", field)
		}
		if err := rejectUnknownJSONFields(object, field, "url"); err != nil {
			return err
		}
		if rawURL, ok := object["url"]; !ok || json.Unmarshal(rawURL, &value) != nil {
			return fmt.Errorf("%s.url must be a string", field)
		}
	}
	if !strings.HasPrefix(value, "data:") {
		if !allowRemoteHTTPSURL {
			return fmt.Errorf("%s currently supports only inline data URLs", field)
		}
		remote, err := url.Parse(value)
		if err != nil || remote.Scheme != "https" || remote.Host == "" || remote.User != nil {
			return fmt.Errorf("%s remote URL must be an absolute HTTPS URL without user information", field)
		}
		if len(allowedRemoteExtensions) > 0 {
			extension := strings.ToLower(path.Ext(remote.Path))
			if !slices.Contains(allowedRemoteExtensions, extension) {
				return fmt.Errorf("%s remote URL path must end with one of %s", field, strings.Join(allowedRemoteExtensions, ", "))
			}
		}
		return nil
	}
	if !allowInlineDataURL {
		return fmt.Errorf("%s inline data URLs are not supported by the protocol profile", field)
	}
	separator := strings.Index(value, ";base64,")
	if separator <= len("data:") || separator+8 >= len(value) {
		return fmt.Errorf("%s must contain a non-empty base64 payload", field)
	}
	mimeType := strings.ToLower(strings.TrimSpace(value[len("data:"):separator]))
	mediaKind := strings.TrimSuffix(field, "_url")
	if (len(allowedMIMETypes) == 0 && !strings.HasPrefix(mimeType, mediaKind+"/")) ||
		(len(allowedMIMETypes) > 0 && !slices.Contains(allowedMIMETypes, mimeType)) {
		return fmt.Errorf("%s MIME type %s is not supported by the protocol profile", field, mimeType)
	}
	decoder := base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(value[separator+8:]))
	if _, err := io.Copy(io.Discard, decoder); err != nil {
		return fmt.Errorf("%s contains invalid base64 data", field)
	}
	return nil
}

func validateBuiltinWebSearchTool(raw json.RawMessage, contract modelprofile.ChatProtocolContract) (bool, error) {
	var tool map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tool); err != nil || tool == nil {
		return false, fmt.Errorf("tool must be an object")
	}
	var kind string
	if rawType, exists := tool["type"]; !exists || json.Unmarshal(rawType, &kind) != nil {
		return false, nil
	}
	if kind != "builtin_function" {
		return false, nil
	}
	if !contract.BuiltinWebSearch {
		return false, fmt.Errorf("builtin functions are not supported by protocol profile %s", contract.ProfileID)
	}
	if err := rejectUnknownJSONFields(tool, "builtin tool", "type", "function"); err != nil {
		return false, err
	}
	var function map[string]json.RawMessage
	if err := json.Unmarshal(tool["function"], &function); err != nil || function == nil {
		return false, fmt.Errorf("builtin tool function is required")
	}
	if err := rejectUnknownJSONFields(function, "builtin tool function", "name"); err != nil {
		return false, err
	}
	var name string
	if err := json.Unmarshal(function["name"], &name); err != nil || name != contract.BuiltinWebSearchName {
		return false, fmt.Errorf("unsupported builtin function")
	}
	return true, nil
}

func jsonBooleanField(raw json.RawMessage) bool {
	var value bool
	return len(raw) > 0 && !isJSONNull(raw) && json.Unmarshal(raw, &value) == nil && value
}
