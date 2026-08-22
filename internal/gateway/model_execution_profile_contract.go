package gateway

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

type modelExecutionProfileEvidence struct {
	ExecutionProfileID        string
	ExecutionProfileVersion   string
	ReasoningProfile          string
	ReasoningMode             string
	ReasoningRequest          string
	ReasoningProviderDefault  string
	ReasoningDefaultAuthority string
	CacheUsageProfile         string
}

func (e modelExecutionProfileEvidence) metadata() map[string]interface{} {
	return map[string]interface{}{
		"execution_profile_id":        e.ExecutionProfileID,
		"execution_profile_version":   e.ExecutionProfileVersion,
		"reasoning_profile":           e.ReasoningProfile,
		"reasoning_mode":              e.ReasoningMode,
		"reasoning_request":           e.ReasoningRequest,
		"reasoning_provider_default":  e.ReasoningProviderDefault,
		"reasoning_default_authority": e.ReasoningDefaultAuthority,
		"cache_usage_profile":         e.CacheUsageProfile,
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
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return modelExecutionProfileEvidence{}, fmt.Errorf("chat request must be an object")
	}

	contract, modeled := modelprofile.ResolveReasoningContract(plan.ReasoningProfile)
	if !modeled {
		if hasNonNullJSONField(envelope, "thinking") {
			return modelExecutionProfileEvidence{}, fmt.Errorf("reasoning control is not supported by profile %s", plan.ReasoningProfile)
		}
		return evidence, nil
	}
	evidence.ReasoningMode = contract.Mode
	evidence.ReasoningProviderDefault = contract.ProviderDefault
	evidence.ReasoningDefaultAuthority = contract.DefaultAuthority

	reasoningActive := contract.Mode == modelprofile.ReasoningModeAlwaysOn || contract.Mode == modelprofile.ReasoningModeRequired
	switch contract.RequestField {
	case "reasoning_effort":
		if hasNonNullJSONField(envelope, "thinking") || hasNonNullJSONField(envelope, "reasoning") {
			return modelExecutionProfileEvidence{}, fmt.Errorf("K3 accepts reasoning_effort, not K2.x thinking or generic reasoning")
		}
		value, present, err := optionalJSONString(envelope["reasoning_effort"], "reasoning_effort")
		if err != nil {
			return modelExecutionProfileEvidence{}, err
		}
		if present {
			if !slices.Contains(contract.AllowedValues, value) {
				return modelExecutionProfileEvidence{}, fmt.Errorf("reasoning_effort must be one of low, high, or max")
			}
			evidence.ReasoningRequest = value
		} else {
			evidence.ReasoningRequest = "omitted"
		}
	case "thinking":
		if hasNonNullJSONField(envelope, "reasoning_effort") || hasNonNullJSONField(envelope, "reasoning") {
			return modelExecutionProfileEvidence{}, fmt.Errorf("K2.6 accepts thinking, not reasoning_effort or generic reasoning")
		}
		value, present, err := optionalThinkingType(envelope["thinking"])
		if err != nil {
			return modelExecutionProfileEvidence{}, err
		}
		if present {
			if !slices.Contains(contract.AllowedValues, value) {
				return modelExecutionProfileEvidence{}, fmt.Errorf("thinking.type must be enabled or disabled")
			}
			evidence.ReasoningRequest = value
			reasoningActive = value == "enabled"
		} else {
			evidence.ReasoningRequest = "omitted"
			reasoningActive = contract.ProviderDefault == "enabled"
		}
	default:
		if hasNonNullJSONField(envelope, "thinking") || hasNonNullJSONField(envelope, "reasoning_effort") || hasNonNullJSONField(envelope, "reasoning") {
			return modelExecutionProfileEvidence{}, fmt.Errorf("profile %s does not accept a reasoning control field", plan.ReasoningProfile)
		}
		evidence.ReasoningRequest = "required"
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
