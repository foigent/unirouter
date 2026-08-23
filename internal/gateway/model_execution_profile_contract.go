package gateway

import (
	"encoding/json"
	"fmt"
	"slices"
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
	ReasoningEffortRequest        string
	CacheUsageProfile             string
	ModelProtocolProfile          string
	ReservedMeterUsage            []metering.MeterQuantity
	ProviderRequestNormalizations []string
}

func (e modelExecutionProfileEvidence) metadata() map[string]interface{} {
	return map[string]interface{}{
		"execution_profile_id":            e.ExecutionProfileID,
		"execution_profile_version":       e.ExecutionProfileVersion,
		"reasoning_profile":               e.ReasoningProfile,
		"reasoning_mode":                  e.ReasoningMode,
		"reasoning_request":               e.ReasoningRequest,
		"reasoning_provider_default":      e.ReasoningProviderDefault,
		"reasoning_default_authority":     e.ReasoningDefaultAuthority,
		"reasoning_toggle_request":        e.ReasoningToggleRequest,
		"reasoning_effort_request":        e.ReasoningEffortRequest,
		"cache_usage_profile":             e.CacheUsageProfile,
		"model_protocol_profile":          e.ModelProtocolProfile,
		"reserved_meter_usage":            e.ReservedMeterUsage,
		"provider_request_normalizations": strings.Join(e.ProviderRequestNormalizations, ","),
	}
}

// validateChatExecutionProfileRequest applies the immutable component
// contract selected by the planner. It runs before secret decryption, Freeze,
// attempt creation, coordinator authorization, and provider I/O.
func validateChatExecutionProfileRequest(body []byte, plan modelrouting.Plan) (modelExecutionProfileEvidence, error) {
	profile, ok := modelprofile.Lookup(plan.ExecutionProfileID, plan.ExecutionProfileVersion)
	if !ok {
		return modelExecutionProfileEvidence{}, fmt.Errorf("unknown model execution profile %s@%s", plan.ExecutionProfileID, plan.ExecutionProfileVersion)
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
	if protocolContract, modeled := modelprofile.ResolveChatProtocolContract(plan.ModelProtocolProfile); modeled {
		validation, err := validateAdvancedChatProtocolRequest(body, protocolContract)
		if err != nil {
			return modelExecutionProfileEvidence{}, err
		}
		evidence.ReservedMeterUsage = slices.Clone(validation.ReservedMeterUsage)
		evidence.ProviderRequestNormalizations = append(
			evidence.ProviderRequestNormalizations,
			validation.ProviderRequestNormalizations...,
		)
	}

	contract, modeled := modelprofile.ResolveReasoningContract(plan.ReasoningProfile)
	if !modeled {
		if hasAnyNonNullJSONField(envelope, "thinking", "reasoning_effort", "reasoning") {
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
	for _, field := range []string{"thinking", "reasoning_effort", "reasoning"} {
		if hasNonNullJSONField(envelope, field) &&
			field != contract.ToggleControl.Field && field != contract.EffortControl.Field {
			return modelExecutionProfileEvidence{}, fmt.Errorf("profile %s does not accept %s", plan.ReasoningProfile, field)
		}
	}

	toggleRequest := ""
	if contract.ToggleControl.Field != "" {
		value, present, err := reasoningControlValue(envelope[contract.ToggleControl.Field], contract.ToggleControl)
		if err != nil {
			return modelExecutionProfileEvidence{}, err
		}
		toggleRequest = "omitted"
		reasoningActive = contract.ToggleControl.ProviderDefault == "enabled"
		if present {
			toggleRequest = value
			reasoningActive = value == "enabled"
		}
		evidence.ReasoningToggleRequest = toggleRequest
	}

	effortRequest := ""
	if contract.EffortControl.Field != "" {
		value, present, err := reasoningControlValue(envelope[contract.EffortControl.Field], contract.EffortControl)
		if err != nil {
			return modelExecutionProfileEvidence{}, err
		}
		effortRequest = "omitted"
		if present {
			if !reasoningActive {
				return modelExecutionProfileEvidence{}, fmt.Errorf("%s requires active reasoning for profile %s", contract.EffortControl.Field, plan.ReasoningProfile)
			}
			effortRequest = value
		}
		evidence.ReasoningEffortRequest = effortRequest
	}

	evidence.ReasoningRequest = reasoningControlSummary(toggleRequest, effortRequest)
	if evidence.ReasoningRequest == "" {
		evidence.ReasoningRequest = "required"
	}
	if reasoningActive {
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
	if reasoningActive && choice == "named" && !contract.NamedToolChoiceWhileReasoning {
		return modelExecutionProfileEvidence{}, fmt.Errorf("named tool_choice is incompatible with active reasoning for profile %s", plan.ReasoningProfile)
	}
	if reasoningActive && choice == "required" && !contract.RequiredToolChoiceWhileReasoning {
		return modelExecutionProfileEvidence{}, fmt.Errorf("tool_choice=required is incompatible with active reasoning for profile %s", plan.ReasoningProfile)
	}
	if reasoningActive && contract.PreserveAssistantReasoningForTools {
		if err := requireAssistantReasoningForToolHistory(envelope["messages"]); err != nil {
			return modelExecutionProfileEvidence{}, err
		}
	}
	return evidence, nil
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

func reasoningControlValue(raw json.RawMessage, contract modelprofile.ReasoningControlContract) (string, bool, error) {
	var value string
	var present bool
	var err error
	if contract.Field == "thinking" {
		value, present, err = optionalThinkingType(raw)
	} else {
		value, present, err = optionalJSONString(raw, contract.Field)
	}
	if err != nil || !present {
		return value, present, err
	}
	if !slices.Contains(contract.AllowedValues, value) {
		return "", false, fmt.Errorf("%s must be one of %s", contract.Field, strings.Join(contract.AllowedValues, ", "))
	}
	return value, true, nil
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
	if len(value) != 1 || value["type"] == nil {
		return "", false, fmt.Errorf("thinking must contain exactly type")
	}
	typeValue, present, err := optionalJSONString(value["type"], "thinking.type")
	if err != nil || !present {
		if err != nil {
			return "", false, err
		}
		return "", false, fmt.Errorf("thinking.type is required")
	}
	return typeValue, true, nil
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

func requireAssistantReasoningForToolHistory(raw json.RawMessage) error {
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
		if _, present, err := optionalJSONString(message["reasoning_content"], "reasoning_content"); err != nil || !present {
			return fmt.Errorf("assistant tool history message %d must preserve reasoning_content", index)
		}
	}
	return nil
}
