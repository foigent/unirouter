package endpointprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
)

const anthropicMessagesAPIVersion = "2023-06-01"

type anthropicMessagesProfile struct{}

type anthropicMessagesRequest struct {
	Model       string            `json:"model"`
	MaxTokens   json.Number       `json:"max_tokens"`
	Messages    []json.RawMessage `json:"messages"`
	System      *string           `json:"system,omitempty"`
	Temperature *json.Number      `json:"temperature,omitempty"`
	TopP        *json.Number      `json:"top_p,omitempty"`
	Stream      *bool             `json:"stream,omitempty"`
}

type anthropicMessagesWireRequest struct {
	Model       string                    `json:"model"`
	MaxTokens   json.Number               `json:"max_tokens"`
	Messages    []anthropicMessageRequest `json:"messages"`
	System      string                    `json:"system,omitempty"`
	Temperature *json.Number              `json:"temperature,omitempty"`
	TopP        *json.Number              `json:"top_p,omitempty"`
	Stream      bool                      `json:"stream"`
}

type anthropicMessageRequest struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicMessagesResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Model        string                  `json:"model"`
	Content      []json.RawMessage       `json:"content"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence json.RawMessage         `json:"stop_sequence"`
	Usage        *anthropicMessagesUsage `json:"usage"`
}

type anthropicMessagesUsage struct {
	InputTokens              json.Number                    `json:"input_tokens"`
	CacheCreationInputTokens *json.Number                   `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *json.Number                   `json:"cache_read_input_tokens"`
	OutputTokens             json.Number                    `json:"output_tokens"`
	CacheCreation            *anthropicCacheCreationDetails `json:"cache_creation"`
	ServerToolUse            json.RawMessage                `json:"server_tool_use"`
	OutputTokensDetails      json.RawMessage                `json:"output_tokens_details"`
}

type anthropicCacheCreationDetails struct {
	Ephemeral5mInputTokens json.Number `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens json.Number `json:"ephemeral_1h_input_tokens"`
}

func (anthropicMessagesProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion:     ContractVersion,
		ProfileID:           ProfileAnthropicMessagesText,
		ProfileVersion:      "1.0.0",
		Provider:            "anthropic",
		EndpointKind:        EndpointMessages,
		DownstreamEndpoint:  "/v1/messages",
		UpstreamEndpoint:    "/v1/messages",
		Authentication:      "x-api-key+anthropic-version",
		RuntimeState:        RuntimeContractOnly,
		ProviderFinality:    "synchronous_terminal_message_with_provider_usage",
		CustomerChargeState: ChargeObservedOnly,
		SupportedModalities: []string{"text"},
		ObservedUnitClasses: []UnitClass{
			UnitInputTokens,
			UnitCacheWriteTokens,
			UnitCacheReadTokens,
			UnitOutputTokens,
		},
		UnsupportedSemantics: []string{
			"anthropic_beta_features",
			"cache_control",
			"citations_or_documents",
			"extended_or_adaptive_thinking",
			"multimodal_content",
			"server_or_client_tools",
			"streaming",
			"structured_output",
		},
	}
}

func (profile anthropicMessagesProfile) BuildRequest(
	ctx context.Context,
	input BuildInput,
) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, false)
	if err != nil {
		return nil, nil, err
	}
	object, err := objectKeys(input.Body)
	if err != nil {
		return nil, nil, err
	}
	if _, exists := object["anthropic-beta"]; exists {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_anthropic_beta")
	}
	if err := rejectKnownUnsupported(
		input.Body,
		"anthropic_beta",
		"beta",
		"betas",
		"container",
		"context_management",
		"mcp_servers",
		"output_config",
		"thinking",
		"tool_choice",
		"tools",
	); err != nil {
		return nil, nil, err
	}
	if hasJSONField(input.Body, "cache_control") {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_cache_control")
	}

	var downstream anthropicMessagesRequest
	if err := decodeStrictRequest(input.Body, &downstream); err != nil {
		return nil, nil, err
	}
	if downstream.Model == "" || downstream.Model != input.RequestedModel {
		return nil, nil, protocolError(ErrInvalidRequest, "requested_model_mismatch")
	}
	if !canonicalUintPattern.MatchString(downstream.MaxTokens.String()) {
		return nil, nil, protocolError(ErrInvalidRequest, "max_tokens_invalid")
	}
	maxTokens, parseErr := strconv.ParseInt(downstream.MaxTokens.String(), 10, 64)
	if parseErr != nil || maxTokens <= 0 {
		return nil, nil, protocolError(ErrInvalidRequest, "max_tokens_invalid")
	}
	if len(downstream.Messages) == 0 {
		return nil, nil, protocolError(ErrInvalidRequest, "messages_missing")
	}
	messages := make([]anthropicMessageRequest, 0, len(downstream.Messages))
	for _, raw := range downstream.Messages {
		message, messageErr := parseAnthropicTextMessage(raw)
		if messageErr != nil {
			return nil, nil, messageErr
		}
		messages = append(messages, message)
	}
	if downstream.System != nil && (*downstream.System == "" || len(*downstream.System) > maxRequestBytes/2) {
		return nil, nil, protocolError(ErrInvalidRequest, "system_text_invalid")
	}
	if downstream.Stream != nil && *downstream.Stream {
		return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_streaming")
	}
	if downstream.Temperature != nil {
		if _, parseErr := parseBoundedDecimal(*downstream.Temperature, "temperature", big.NewRat(0, 1), big.NewRat(1, 1)); parseErr != nil {
			return nil, nil, protocolError(ErrInvalidRequest, "temperature_invalid")
		}
	}
	if downstream.TopP != nil {
		if _, parseErr := parseBoundedDecimal(*downstream.TopP, "top_p", big.NewRat(0, 1), big.NewRat(1, 1)); parseErr != nil {
			return nil, nil, protocolError(ErrInvalidRequest, "top_p_invalid")
		}
	}

	var system string
	if downstream.System != nil {
		system = *downstream.System
	}
	wireBody, marshalErr := json.Marshal(anthropicMessagesWireRequest{
		Model:       input.MappedModel,
		MaxTokens:   downstream.MaxTokens,
		Messages:    messages,
		System:      system,
		Temperature: downstream.Temperature,
		TopP:        downstream.TopP,
		Stream:      false,
	})
	if marshalErr != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	target := *baseURL
	target.Path = "/v1/messages"
	target.RawPath = ""
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(wireBody))
	if requestErr != nil {
		return nil, nil, protocolError(ErrInvalidConfiguration, "request_construction_failed")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-api-key", input.APIKey)
	request.Header.Set("anthropic-version", anthropicMessagesAPIVersion)
	return request, &Binding{
		profileID:      profile.Descriptor().ProfileID,
		profileVersion: profile.Descriptor().ProfileVersion,
		requestedModel: input.RequestedModel,
		mappedModel:    input.MappedModel,
	}, nil
}

func parseAnthropicTextMessage(raw json.RawMessage) (anthropicMessageRequest, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return anthropicMessageRequest{}, protocolError(ErrInvalidRequest, "message_shape_invalid")
	}
	if _, exists := fields["cache_control"]; exists {
		return anthropicMessageRequest{}, protocolError(ErrUnsupportedSemantics, "unsupported_cache_control")
	}
	for _, field := range []string{"tool_call_id", "tool_calls", "tool_result", "tool_use"} {
		if _, exists := fields[field]; exists {
			return anthropicMessageRequest{}, protocolError(ErrUnsupportedSemantics, "unsupported_tools")
		}
	}
	if len(fields) != 2 {
		return anthropicMessageRequest{}, protocolError(ErrInvalidRequest, "message_shape_invalid")
	}
	var message anthropicMessageRequest
	if err := json.Unmarshal(fields["role"], &message.Role); err != nil {
		return anthropicMessageRequest{}, protocolError(ErrInvalidRequest, "message_role_invalid")
	}
	if message.Role != "user" && message.Role != "assistant" {
		return anthropicMessageRequest{}, protocolError(ErrUnsupportedSemantics, "unsupported_message_role")
	}
	if err := json.Unmarshal(fields["content"], &message.Content); err != nil {
		return anthropicMessageRequest{}, protocolError(ErrUnsupportedSemantics, "unsupported_non_text_content")
	}
	if message.Content == "" || len(message.Content) > maxRequestBytes/2 {
		return anthropicMessageRequest{}, protocolError(ErrInvalidRequest, "message_text_invalid")
	}
	return message, nil
}

func (profile anthropicMessagesProfile) ParseResponse(
	_ context.Context,
	binding *Binding,
	statusCode int,
	body []byte,
) (Outcome, error) {
	if err := requireBinding(binding, profile.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if statusCode < 200 || statusCode >= 300 {
		failureKind, _ := profile.ClassifyHTTPStatus(statusCode)
		return Outcome{}, protocolError(ErrUpstreamResponse, "http_"+strconv.Itoa(statusCode)+"_"+failureKind)
	}
	var response anthropicMessagesResponse
	if err := decodeResponse(body, &response); err != nil {
		return Outcome{}, err
	}
	if response.ID == "" || response.Type != "message" || response.Role != "assistant" || response.Model != binding.mappedModel {
		return Outcome{}, protocolError(ErrInvalidResponse, "response_identity_invalid")
	}
	switch response.StopReason {
	case "end_turn", "max_tokens":
		if !isExplicitJSONNull(response.StopSequence) {
			return Outcome{}, protocolError(ErrInvalidResponse, "stop_sequence_invalid")
		}
	case "stop_sequence":
		var stopSequence string
		if json.Unmarshal(response.StopSequence, &stopSequence) != nil || stopSequence == "" {
			return Outcome{}, protocolError(ErrInvalidResponse, "stop_sequence_invalid")
		}
	default:
		return Outcome{}, protocolError(ErrUnsupportedSemantics, "unsupported_response_stop_reason")
	}
	if len(response.Content) == 0 {
		return Outcome{}, protocolError(ErrInvalidResponse, "response_content_missing")
	}
	for _, raw := range response.Content {
		if err := validateAnthropicTextBlock(raw); err != nil {
			return Outcome{}, err
		}
	}
	if response.Usage == nil {
		return Outcome{}, protocolError(ErrMissingUsage, "provider_usage_missing")
	}
	if hasNonNullJSON(response.Usage.ServerToolUse) {
		return Outcome{}, protocolError(ErrUnsupportedSemantics, "unsupported_server_tool_usage")
	}
	if hasNonNullJSON(response.Usage.OutputTokensDetails) {
		return Outcome{}, protocolError(ErrUnsupportedSemantics, "unsupported_thinking_usage")
	}
	inputTokens, err := parseNonnegativeInt64(response.Usage.InputTokens, string(UnitInputTokens))
	if err != nil {
		return Outcome{}, err
	}
	var cacheWriteTokens int64
	if response.Usage.CacheCreationInputTokens != nil {
		cacheWriteTokens, err = parseNonnegativeInt64(*response.Usage.CacheCreationInputTokens, string(UnitCacheWriteTokens))
		if err != nil {
			return Outcome{}, err
		}
	}
	var cacheReadTokens int64
	if response.Usage.CacheReadInputTokens != nil {
		cacheReadTokens, err = parseNonnegativeInt64(*response.Usage.CacheReadInputTokens, string(UnitCacheReadTokens))
		if err != nil {
			return Outcome{}, err
		}
	}
	outputTokens, err := parseNonnegativeInt64(response.Usage.OutputTokens, string(UnitOutputTokens))
	if err != nil {
		return Outcome{}, err
	}
	if details := response.Usage.CacheCreation; details != nil {
		ephemeral5m, detailErr := parseNonnegativeInt64(details.Ephemeral5mInputTokens, "cache_creation_ephemeral_5m_input_tokens")
		if detailErr != nil {
			return Outcome{}, detailErr
		}
		ephemeral1h, detailErr := parseNonnegativeInt64(details.Ephemeral1hInputTokens, "cache_creation_ephemeral_1h_input_tokens")
		if detailErr != nil || ephemeral5m > cacheWriteTokens || ephemeral1h != cacheWriteTokens-ephemeral5m {
			return Outcome{}, protocolError(ErrInvalidResponse, "cache_creation_partition_mismatch")
		}
	}
	return Outcome{
		ContractVersion: ContractVersion,
		ProfileID:       profile.Descriptor().ProfileID,
		ProviderState:   "completed",
		ProviderObject:  response.ID,
		MappedModel:     binding.mappedModel,
		OutputCount:     len(response.Content),
		Units: []UnitObservation{
			observedUnit(UnitInputTokens, strconv.FormatInt(inputTokens, 10)),
			observedUnit(UnitCacheWriteTokens, strconv.FormatInt(cacheWriteTokens, 10)),
			observedUnit(UnitCacheReadTokens, strconv.FormatInt(cacheReadTokens, 10)),
			observedUnit(UnitOutputTokens, strconv.FormatInt(outputTokens, 10)),
		},
	}, nil
}

func validateAnthropicTextBlock(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return protocolError(ErrInvalidResponse, "response_content_shape_invalid")
	}
	var contentType string
	if json.Unmarshal(fields["type"], &contentType) != nil || contentType != "text" {
		return protocolError(ErrUnsupportedSemantics, "unsupported_response_content")
	}
	var text string
	if json.Unmarshal(fields["text"], &text) != nil || len(fields) != 2 {
		return protocolError(ErrUnsupportedSemantics, "unsupported_response_content")
	}
	return nil
}

func hasNonNullJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func hasJSONField(raw []byte, name string) bool {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	return jsonValueHasField(value, name)
}

func jsonValueHasField(value any, name string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == name || jsonValueHasField(child, name) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonValueHasField(child, name) {
				return true
			}
		}
	}
	return false
}

func (anthropicMessagesProfile) ClassifyHTTPStatus(statusCode int) (string, bool) {
	switch statusCode {
	case http.StatusPaymentRequired:
		return "billing", false
	case http.StatusConflict:
		return "conflict", false
	case http.StatusRequestEntityTooLarge:
		return "client_request", false
	case 529:
		return "channel_failure", true
	default:
		return classifyHTTPStatus(statusCode)
	}
}
