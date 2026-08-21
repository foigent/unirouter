package endpointprotocol

import (
	"context"
	"encoding/json"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
)

type openAIResponsesProfile struct{}

// openAIResponsesEnvelope keeps every provider field as raw JSON. The profile
// validates the fields whose semantics it understands and replaces only the
// model binding without converting admitted numbers through float64. Unknown
// request semantics remain raw long enough to be rejected before dispatch.
type openAIResponsesEnvelope map[string]json.RawMessage

type openAIResponsesResponse struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	Status            string            `json:"status"`
	Model             string            `json:"model"`
	Error             json.RawMessage   `json:"error"`
	IncompleteDetails json.RawMessage   `json:"incomplete_details"`
	OutputText        json.RawMessage   `json:"output_text"`
	Output            []json.RawMessage `json:"output"`
	Usage             json.RawMessage   `json:"usage"`
}

type openAIResponsesUsage struct {
	InputTokens        json.Number                  `json:"input_tokens"`
	InputTokenDetails  *openAIResponsesInputDetail  `json:"input_tokens_details"`
	OutputTokens       json.Number                  `json:"output_tokens"`
	OutputTokenDetails *openAIResponsesOutputDetail `json:"output_tokens_details"`
	TotalTokens        json.Number                  `json:"total_tokens"`
}

type openAIResponsesInputDetail struct {
	CachedTokens     *json.Number `json:"cached_tokens"`
	CacheWriteTokens *json.Number `json:"cache_write_tokens"`
}

type openAIResponsesOutputDetail struct {
	ReasoningTokens *json.Number `json:"reasoning_tokens"`
}

func (openAIResponsesProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion:     ContractVersion,
		ProfileID:           ProfileOpenAIResponsesText,
		ProfileVersion:      "1.0.0",
		Provider:            "openai",
		EndpointKind:        EndpointResponses,
		DownstreamEndpoint:  "/v1/responses",
		UpstreamEndpoint:    "/v1/responses",
		Authentication:      "bearer",
		RuntimeState:        RuntimeContractOnly,
		ProviderFinality:    "status_completed_with_provider_usage",
		CustomerChargeState: ChargeObservedOnly,
		SupportedModalities: []string{"text"},
		ObservedUnitClasses: []UnitClass{
			UnitInputTokens,
			UnitCacheReadTokens,
			UnitCacheWriteTokens,
			UnitOutputTokens,
		},
		UnsupportedSemantics: []string{
			"background",
			"conversation_state",
			"function_or_hosted_tools",
			"include_expansions",
			"multimodal_or_structured_input",
			"reasoning_controls",
			"streaming",
			"stored_response_state",
			"structured_output",
		},
	}
}

func (profile openAIResponsesProfile) BuildRequest(
	ctx context.Context,
	input BuildInput,
) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, true)
	if err != nil {
		return nil, nil, err
	}
	envelope, err := parseOpenAIResponsesEnvelope(input.Body)
	if err != nil {
		return nil, nil, err
	}
	if err := validateOpenAIResponsesRequest(envelope, input.RequestedModel); err != nil {
		return nil, nil, err
	}

	mappedModel, _ := json.Marshal(input.MappedModel)
	envelope["model"] = mappedModel
	envelope["store"] = json.RawMessage("false")
	envelope["stream"] = json.RawMessage("false")
	wireBody, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	request, err := buildPOSTRequest(ctx, baseURL, "/v1/responses", input.APIKey, input.RequestID, wireBody)
	if err != nil {
		return nil, nil, err
	}
	return request, &Binding{
		profileID:      profile.Descriptor().ProfileID,
		profileVersion: profile.Descriptor().ProfileVersion,
		requestedModel: input.RequestedModel,
		mappedModel:    input.MappedModel,
	}, nil
}

func parseOpenAIResponsesEnvelope(body []byte) (openAIResponsesEnvelope, error) {
	if err := validateStrictJSON(body, maxRequestBytes, ErrInvalidRequest); err != nil {
		return nil, err
	}
	var envelope openAIResponsesEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, protocolError(ErrInvalidRequest, "request_must_be_object")
	}
	return envelope, nil
}

func validateOpenAIResponsesRequest(envelope openAIResponsesEnvelope, requestedModel string) error {
	model, ok := decodeRequiredString(envelope["model"])
	if !ok || model != requestedModel {
		return protocolError(ErrInvalidRequest, "requested_model_mismatch")
	}
	inputText, ok := decodeRequiredString(envelope["input"])
	if !ok {
		if _, exists := envelope["input"]; exists {
			return protocolError(ErrUnsupportedSemantics, "unsupported_multimodal_or_structured_input")
		}
		return protocolError(ErrInvalidRequest, "input_text_invalid")
	}
	if len(inputText) > maxRequestBytes/2 {
		return protocolError(ErrInvalidRequest, "input_text_invalid")
	}

	if raw, exists := envelope["instructions"]; exists {
		instructions, valid := decodeJSONString(raw)
		if !valid || len(instructions) > maxRequestBytes/2 {
			return protocolError(ErrInvalidRequest, "instructions_invalid")
		}
	}
	if raw, exists := envelope["max_output_tokens"]; exists {
		value, valid := decodeJSONNumber(raw)
		if !valid {
			return protocolError(ErrInvalidRequest, "max_output_tokens_invalid")
		}
		parsed, parseErr := parseRequestInt64(value)
		if parseErr != nil || parsed < 16 {
			return protocolError(ErrInvalidRequest, "max_output_tokens_invalid")
		}
	}
	if err := validateOptionalDecimal(envelope, "temperature", big.NewRat(0, 1), big.NewRat(2, 1)); err != nil {
		return err
	}
	if err := validateOptionalDecimal(envelope, "top_p", big.NewRat(0, 1), big.NewRat(1, 1)); err != nil {
		return err
	}
	if err := validateOptionalBool(envelope, "store", false, "unsupported_stored_response_state"); err != nil {
		return err
	}
	if err := validateOptionalBool(envelope, "stream", false, "unsupported_streaming"); err != nil {
		return err
	}
	if err := validateOpenAIMetadata(envelope["metadata"]); err != nil {
		return err
	}
	if err := validateOpenAITextConfig(envelope["text"]); err != nil {
		return err
	}
	if _, exists := envelope["response_format"]; exists {
		// response_format is a Chat Completions field, not a stable Responses
		// request field. It is recognized here so a compatibility client cannot
		// silently turn an unsupported structured-output request into plain text.
		return protocolError(ErrUnsupportedSemantics, "unsupported_structured_output")
	}

	unsupported := map[string]string{
		"background":           "unsupported_background",
		"context_management":   "unsupported_conversation_state",
		"conversation":         "unsupported_conversation_state",
		"include":              "unsupported_include_expansions",
		"max_tool_calls":       "unsupported_tools",
		"moderation":           "unsupported_moderation",
		"parallel_tool_calls":  "unsupported_tools",
		"previous_response_id": "unsupported_conversation_state",
		"prompt":               "unsupported_prompt_template",
		"reasoning":            "unsupported_reasoning_controls",
		"stream_options":       "unsupported_streaming",
		"tool_choice":          "unsupported_tools",
		"tools":                "unsupported_tools",
		"truncation":           "unsupported_truncation",
	}
	for field, code := range unsupported {
		if _, exists := envelope[field]; exists {
			return protocolError(ErrUnsupportedSemantics, code)
		}
	}
	allowed := map[string]struct{}{
		"input": {}, "instructions": {}, "max_output_tokens": {}, "metadata": {},
		"model": {}, "store": {}, "stream": {}, "temperature": {}, "text": {}, "top_p": {},
	}
	for field := range envelope {
		if _, exists := allowed[field]; !exists {
			return protocolError(ErrUnsupportedSemantics, "unsupported_unknown_provider_field")
		}
	}
	return nil
}

func decodeRequiredString(raw json.RawMessage) (string, bool) {
	value, ok := decodeJSONString(raw)
	return value, ok && value != ""
}

func decodeJSONString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || isExplicitJSONNull(raw) {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func decodeJSONNumber(raw json.RawMessage) (json.Number, bool) {
	if len(raw) == 0 || isExplicitJSONNull(raw) {
		return "", false
	}
	var value json.Number
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", false
	}
	return value, true
}

func parseRequestInt64(value json.Number) (int64, error) {
	if !canonicalUintPattern.MatchString(value.String()) {
		return 0, protocolError(ErrInvalidRequest, "integer_not_canonical_uint")
	}
	parsed, err := strconv.ParseInt(value.String(), 10, 64)
	if err != nil {
		return 0, protocolError(ErrInvalidRequest, "integer_out_of_range")
	}
	return parsed, nil
}

func validateOptionalDecimal(envelope openAIResponsesEnvelope, field string, minimum, maximum *big.Rat) error {
	raw, exists := envelope[field]
	if !exists {
		return nil
	}
	value, valid := decodeJSONNumber(raw)
	if !valid {
		return protocolError(ErrInvalidRequest, field+"_invalid")
	}
	if _, err := parseBoundedDecimal(value, field, minimum, maximum); err != nil {
		return protocolError(ErrInvalidRequest, field+"_invalid")
	}
	return nil
}

func validateOptionalBool(envelope openAIResponsesEnvelope, field string, admitted bool, unsupportedCode string) error {
	raw, exists := envelope[field]
	if !exists {
		return nil
	}
	var value bool
	if isExplicitJSONNull(raw) || json.Unmarshal(raw, &value) != nil {
		return protocolError(ErrInvalidRequest, field+"_invalid")
	}
	if value != admitted {
		return protocolError(ErrUnsupportedSemantics, unsupportedCode)
	}
	return nil
}

func validateOpenAIMetadata(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if isExplicitJSONNull(raw) {
		return protocolError(ErrInvalidRequest, "metadata_invalid")
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil || len(metadata) > 16 {
		return protocolError(ErrInvalidRequest, "metadata_invalid")
	}
	for key, value := range metadata {
		if key == "" || len(key) > 64 || len(value) > 512 {
			return protocolError(ErrInvalidRequest, "metadata_invalid")
		}
	}
	return nil
}

func validateOpenAITextConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if isExplicitJSONNull(raw) {
		return protocolError(ErrInvalidRequest, "text_config_invalid")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil || config == nil {
		return protocolError(ErrInvalidRequest, "text_config_invalid")
	}
	if format, exists := config["format"]; exists {
		var formatObject map[string]json.RawMessage
		if isExplicitJSONNull(format) || json.Unmarshal(format, &formatObject) != nil || formatObject == nil {
			return protocolError(ErrInvalidRequest, "text_format_invalid")
		}
		formatType, ok := decodeRequiredString(formatObject["type"])
		if !ok {
			return protocolError(ErrInvalidRequest, "text_format_invalid")
		}
		if formatType != "text" {
			return protocolError(ErrUnsupportedSemantics, "unsupported_structured_output")
		}
		if len(formatObject) != 1 {
			return protocolError(ErrUnsupportedSemantics, "unsupported_unknown_text_format_field")
		}
	}
	if verbosity, exists := config["verbosity"]; exists {
		value, ok := decodeRequiredString(verbosity)
		if !ok || (value != "low" && value != "medium" && value != "high") {
			return protocolError(ErrInvalidRequest, "text_verbosity_invalid")
		}
	}
	for field := range config {
		if field != "format" && field != "verbosity" {
			return protocolError(ErrUnsupportedSemantics, "unsupported_unknown_text_field")
		}
	}
	return nil
}

func (profile openAIResponsesProfile) ParseResponse(
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
	var response openAIResponsesResponse
	if err := decodeResponse(body, &response); err != nil {
		return Outcome{}, err
	}
	if response.ID == "" || response.Object != "response" || response.Model != binding.mappedModel {
		return Outcome{}, protocolError(ErrInvalidResponse, "response_identity_invalid")
	}
	switch response.Status {
	case "completed":
		if !isExplicitJSONNull(response.Error) || !isExplicitJSONNull(response.IncompleteDetails) {
			return Outcome{}, protocolError(ErrInvalidResponse, "completed_response_has_failure_details")
		}
	case "queued", "in_progress", "incomplete":
		return Outcome{}, protocolError(ErrNonFinalResponse, "response_not_completed")
	case "failed", "cancelled":
		return Outcome{}, protocolError(ErrUpstreamResponse, "response_"+response.Status)
	default:
		return Outcome{}, protocolError(ErrInvalidResponse, "response_status_invalid")
	}

	text, outputCount, err := validateOpenAIResponsesOutput(response.Output)
	if err != nil {
		return Outcome{}, err
	}
	if len(response.OutputText) > 0 && !isExplicitJSONNull(response.OutputText) {
		outputText, valid := decodeJSONString(response.OutputText)
		if !valid || outputText != text {
			return Outcome{}, protocolError(ErrInvalidResponse, "output_text_mismatch")
		}
	}
	units, err := parseOpenAIResponsesUsage(response.Usage)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		ContractVersion: ContractVersion,
		ProfileID:       profile.Descriptor().ProfileID,
		ProviderState:   "completed",
		ProviderObject:  response.ID,
		MappedModel:     binding.mappedModel,
		OutputCount:     outputCount,
		Units:           units,
	}, nil
}

func validateOpenAIResponsesOutput(outputs []json.RawMessage) (string, int, error) {
	if len(outputs) == 0 {
		return "", 0, protocolError(ErrInvalidResponse, "response_output_missing")
	}
	var text strings.Builder
	outputCount := 0
	for _, rawOutput := range outputs {
		var output map[string]json.RawMessage
		if json.Unmarshal(rawOutput, &output) != nil || output == nil {
			return "", 0, protocolError(ErrInvalidResponse, "response_output_invalid")
		}
		outputType, ok := decodeRequiredString(output["type"])
		if !ok {
			return "", 0, protocolError(ErrUnsupportedSemantics, "unsupported_response_output")
		}
		if outputType == "reasoning" {
			if err := validateOpenAIReasoningOutput(output); err != nil {
				return "", 0, err
			}
			continue
		}
		if outputType != "message" {
			return "", 0, protocolError(ErrUnsupportedSemantics, "unsupported_response_output")
		}
		_, idOK := decodeRequiredString(output["id"])
		status, statusOK := decodeRequiredString(output["status"])
		role, roleOK := decodeRequiredString(output["role"])
		if !idOK || !statusOK || status != "completed" || !roleOK || role != "assistant" {
			return "", 0, protocolError(ErrInvalidResponse, "response_message_invalid")
		}
		var contents []json.RawMessage
		if json.Unmarshal(output["content"], &contents) != nil || len(contents) == 0 {
			return "", 0, protocolError(ErrInvalidResponse, "response_content_missing")
		}
		for _, rawContent := range contents {
			var content map[string]json.RawMessage
			if json.Unmarshal(rawContent, &content) != nil || content == nil {
				return "", 0, protocolError(ErrInvalidResponse, "response_content_invalid")
			}
			contentType, typeOK := decodeRequiredString(content["type"])
			if !typeOK || contentType != "output_text" {
				return "", 0, protocolError(ErrUnsupportedSemantics, "unsupported_response_content")
			}
			contentText, textOK := decodeJSONString(content["text"])
			if !textOK {
				return "", 0, protocolError(ErrInvalidResponse, "response_text_invalid")
			}
			text.WriteString(contentText)
			outputCount++
		}
	}
	if outputCount == 0 {
		return "", 0, protocolError(ErrInvalidResponse, "response_text_output_missing")
	}
	return text.String(), outputCount, nil
}

func validateOpenAIReasoningOutput(output map[string]json.RawMessage) error {
	if _, ok := decodeRequiredString(output["id"]); !ok {
		return protocolError(ErrInvalidResponse, "response_reasoning_item_invalid")
	}
	if raw, exists := output["status"]; exists {
		status, ok := decodeRequiredString(raw)
		if !ok || status != "completed" {
			return protocolError(ErrInvalidResponse, "response_reasoning_item_invalid")
		}
	}
	for _, field := range []string{"summary", "content"} {
		raw, exists := output[field]
		if !exists || isExplicitJSONNull(raw) || isEmptyJSONArray(raw) {
			continue
		}
		return protocolError(ErrUnsupportedSemantics, "unsupported_reasoning_output")
	}
	if raw, exists := output["encrypted_content"]; exists && !isExplicitJSONNull(raw) {
		return protocolError(ErrUnsupportedSemantics, "unsupported_reasoning_output")
	}
	return nil
}

func isEmptyJSONArray(raw json.RawMessage) bool {
	var values []json.RawMessage
	return json.Unmarshal(raw, &values) == nil && len(values) == 0
}

func parseOpenAIResponsesUsage(raw json.RawMessage) ([]UnitObservation, error) {
	if len(raw) == 0 || isExplicitJSONNull(raw) {
		return nil, protocolError(ErrMissingUsage, "provider_usage_missing")
	}
	var usage openAIResponsesUsage
	if err := decodeResponse(raw, &usage); err != nil {
		return nil, protocolError(ErrInvalidResponse, "provider_usage_invalid")
	}
	inputTokens, err := parseNonnegativeInt64(usage.InputTokens, string(UnitInputTokens))
	if err != nil {
		return nil, err
	}
	outputTokens, err := parseNonnegativeInt64(usage.OutputTokens, string(UnitOutputTokens))
	if err != nil {
		return nil, err
	}
	totalTokens, err := parseNonnegativeInt64(usage.TotalTokens, "total_tokens")
	if err != nil || inputTokens > math.MaxInt64-outputTokens || inputTokens+outputTokens != totalTokens {
		return nil, protocolError(ErrInvalidResponse, "usage_total_mismatch")
	}
	units := []UnitObservation{
		observedUnit(UnitInputTokens, usage.InputTokens.String()),
		observedUnit(UnitOutputTokens, usage.OutputTokens.String()),
	}
	if details := usage.InputTokenDetails; details != nil {
		if details.CachedTokens != nil {
			cached, parseErr := parseNonnegativeInt64(*details.CachedTokens, string(UnitCacheReadTokens))
			if parseErr != nil || cached > inputTokens {
				return nil, protocolError(ErrInvalidResponse, "cache_read_tokens_invalid")
			}
			units = append(units, observedUnit(UnitCacheReadTokens, details.CachedTokens.String()))
		}
		if details.CacheWriteTokens != nil {
			written, parseErr := parseNonnegativeInt64(*details.CacheWriteTokens, string(UnitCacheWriteTokens))
			if parseErr != nil || written > inputTokens {
				return nil, protocolError(ErrInvalidResponse, "cache_write_tokens_invalid")
			}
			units = append(units, observedUnit(UnitCacheWriteTokens, details.CacheWriteTokens.String()))
		}
	}
	if details := usage.OutputTokenDetails; details != nil && details.ReasoningTokens != nil {
		reasoning, parseErr := parseNonnegativeInt64(*details.ReasoningTokens, "reasoning_tokens")
		if parseErr != nil || reasoning > outputTokens {
			return nil, protocolError(ErrInvalidResponse, "reasoning_tokens_invalid")
		}
	}
	return units, nil
}

func (openAIResponsesProfile) ClassifyHTTPStatus(statusCode int) (string, bool) {
	return classifyHTTPStatus(statusCode)
}

func isExplicitJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}
