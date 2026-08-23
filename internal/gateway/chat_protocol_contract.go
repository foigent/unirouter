package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

type advancedChatProtocolValidation struct {
	SanitizedBody                 []byte
	ReservedMeterUsage            []metering.MeterQuantity
	ProviderRequestNormalizations []string
}

// validateAdvancedChatProtocolRequest validates only advanced shapes that the
// generic OpenAI-compatible admission contract deliberately rejects. It also
// builds a validation-only shadow body for the existing strict validator; the
// original request bytes remain the admission-counting and provider payload.
func validateAdvancedChatProtocolRequest(body []byte, contract modelprofile.ChatProtocolContract) (advancedChatProtocolValidation, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return advancedChatProtocolValidation{}, fmt.Errorf("chat request must be an object")
	}

	rawMessages, ok := envelope["messages"]
	if !ok || isJSONNull(rawMessages) {
		return advancedChatProtocolValidation{}, fmt.Errorf("messages must be an array")
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(rawMessages, &messages); err != nil {
		return advancedChatProtocolValidation{}, fmt.Errorf("messages must be an array of objects")
	}
	for index := range messages {
		role, err := requiredMessageRole(messages[index])
		if err != nil {
			return advancedChatProtocolValidation{}, fmt.Errorf("message %d: %w", index, err)
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
			if err := validateDynamicSystemTools(rawTools); err != nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d dynamic tools: %w", index, err)
			}
			delete(messages[index], "tools")
		}
		if rawContent, exists := messages[index]["content"]; exists && !isJSONNull(rawContent) && firstJSONToken(rawContent) == '[' {
			sanitized, err := sanitizeAdvancedContentParts(rawContent, contract)
			if err != nil {
				return advancedChatProtocolValidation{}, fmt.Errorf("message %d content: %w", index, err)
			}
			messages[index]["content"] = sanitized
		}
	}
	sanitizedMessages, err := json.Marshal(messages)
	if err != nil {
		return advancedChatProtocolValidation{}, fmt.Errorf("encode sanitized messages: %w", err)
	}
	envelope["messages"] = sanitizedMessages

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

	result := advancedChatProtocolValidation{}
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

func validateDynamicSystemTools(raw json.RawMessage) error {
	if err := validateMeasurableTools(raw); err != nil {
		return err
	}
	var tools []struct {
		Function map[string]json.RawMessage `json:"function"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		return fmt.Errorf("tools must be an array")
	}
	for index, tool := range tools {
		description, hasDescription := tool.Function["description"]
		if !hasDescription || isJSONNull(description) || !rawJSONString(description) {
			return fmt.Errorf("tool %d function description must be a string", index)
		}
		parameters, hasParameters := tool.Function["parameters"]
		if !hasParameters || isJSONNull(parameters) || firstJSONToken(parameters) != '{' {
			return fmt.Errorf("tool %d function parameters must be a JSON schema object", index)
		}
	}
	return nil
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

func sanitizeAdvancedContentParts(raw json.RawMessage, contract modelprofile.ChatProtocolContract) (json.RawMessage, error) {
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("content parts must be objects")
	}
	for index := range parts {
		var kind string
		if rawType, exists := parts[index]["type"]; !exists || json.Unmarshal(rawType, &kind) != nil {
			continue
		}
		switch kind {
		case "image_url":
			if !contract.InlineImageDataURL {
				return nil, fmt.Errorf("image input is not supported by protocol profile %s", contract.ProfileID)
			}
			if err := validateInlineMediaPart(parts[index], "image_url", contract.InlineImageMIMETypes); err != nil {
				return nil, fmt.Errorf("part %d: %w", index, err)
			}
			parts[index] = map[string]json.RawMessage{
				"type": json.RawMessage(`"text"`), "text": json.RawMessage(`""`),
			}
		case "video_url":
			if !contract.InlineVideoDataURL {
				return nil, fmt.Errorf("video input is not supported by protocol profile %s", contract.ProfileID)
			}
			if err := validateInlineMediaPart(parts[index], "video_url", contract.InlineVideoMIMETypes); err != nil {
				return nil, fmt.Errorf("part %d: %w", index, err)
			}
			parts[index] = map[string]json.RawMessage{
				"type": json.RawMessage(`"text"`), "text": json.RawMessage(`""`),
			}
		}
	}
	return json.Marshal(parts)
}

func validateInlineMediaPart(part map[string]json.RawMessage, field string, allowedMIMETypes []string) error {
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
		return fmt.Errorf("%s currently supports only inline data URLs", field)
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
