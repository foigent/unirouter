package endpointprotocol

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/modeloperation"
)

type publicOpenAIResponsesProfile struct {
	base openAIResponsesProfile
}

func (profile publicOpenAIResponsesProfile) Descriptor() PublicNonStreamDescriptor {
	base := profile.base.Descriptor()
	return PublicNonStreamDescriptor{
		Identity: publicIdentity(
			modeloperation.IDResponses,
			modeloperation.PathResponses,
			base.ProfileID,
			base.ProfileVersion,
		),
		EndpointKind:       base.EndpointKind,
		DownstreamEndpoint: base.DownstreamEndpoint,
		UpstreamEndpoint:   base.UpstreamEndpoint,
		CachePolicy:        PublicCachePolicyBypass,
		Streaming:          false,
		Modality:           "text",
	}
}

type publicOpenAIRequestDocument struct {
	envelope openAIResponsesEnvelope
	paths    []string
}

func (document publicOpenAIRequestDocument) canonicalBody() ([]byte, error) {
	return json.Marshal(document.envelope)
}

func (document publicOpenAIRequestDocument) texts() []PublicText {
	result := make([]PublicText, 0, len(document.paths))
	for _, path := range document.paths {
		value, _ := decodeJSONString(document.envelope[path])
		result = append(result, PublicText{Path: path, Text: value})
	}
	return result
}

func (document publicOpenAIRequestDocument) withTexts(replacements []string) (publicRequestDocument, error) {
	if len(replacements) != len(document.paths) {
		return nil, replacementError(len(document.paths), len(replacements))
	}
	envelope := make(openAIResponsesEnvelope, len(document.envelope))
	for key, value := range document.envelope {
		envelope[key] = append(json.RawMessage(nil), value...)
	}
	for index, path := range document.paths {
		if replacements[index] == "" {
			return nil, protocolError(ErrInvalidRequest, "guardrail_text_empty")
		}
		encoded, err := json.Marshal(replacements[index])
		if err != nil {
			return nil, protocolError(ErrInvalidRequest, "guardrail_text_encoding_failed")
		}
		envelope[path] = encoded
	}
	if err := validateOpenAIResponsesRequest(envelope, mustJSONString(envelope["model"])); err != nil {
		return nil, err
	}
	return publicOpenAIRequestDocument{envelope: envelope, paths: append([]string(nil), document.paths...)}, nil
}

func mustJSONString(raw json.RawMessage) string {
	value, _ := decodeJSONString(raw)
	return value
}

func (profile publicOpenAIResponsesProfile) PrepareRequest(body []byte, requestedModel string) (*PublicPreparedRequest, error) {
	envelope, err := parseOpenAIResponsesEnvelope(body)
	if err != nil {
		return nil, err
	}
	if err := validateOpenAIResponsesRequest(envelope, requestedModel); err != nil {
		return nil, err
	}
	rawMax, exists := envelope["max_output_tokens"]
	if !exists {
		return nil, protocolError(ErrInvalidRequest, "max_output_tokens_required")
	}
	value, valid := decodeJSONNumber(rawMax)
	if !valid {
		return nil, protocolError(ErrInvalidRequest, "max_output_tokens_invalid")
	}
	maxOutput, err := parseRequestInt64(value)
	if err != nil || maxOutput < 16 {
		return nil, protocolError(ErrInvalidRequest, "max_output_tokens_invalid")
	}

	// The public contract always emits an explicit synchronous, stateless
	// provider request, including when the client omitted the equivalent false
	// flags.
	envelope["store"] = json.RawMessage("false")
	envelope["stream"] = json.RawMessage("false")
	paths := []string{"input"}
	if _, exists := envelope["instructions"]; exists {
		paths = append(paths, "instructions")
	}
	document := publicOpenAIRequestDocument{envelope: envelope, paths: paths}
	if err := validatePublicRequestDocument(document); err != nil {
		return nil, err
	}
	return &PublicPreparedRequest{
		identity:       profile.Descriptor().Identity,
		endpointKind:   EndpointResponses,
		requestedModel: requestedModel,
		maxOutput:      maxOutput,
		document:       document,
	}, nil
}

func (profile publicOpenAIResponsesProfile) BuildUpstream(
	ctx context.Context,
	prepared *PublicPreparedRequest,
	input PublicUpstreamInput,
) (*PublicUpstreamRequest, error) {
	return buildPublicUpstream(ctx, profile, profile.base, prepared, input)
}

type publicOpenAIResponsesProviderResponse struct {
	ID                string                             `json:"id"`
	Object            string                             `json:"object"`
	Status            string                             `json:"status"`
	Model             string                             `json:"model"`
	Error             json.RawMessage                    `json:"error"`
	IncompleteDetails json.RawMessage                    `json:"incomplete_details"`
	OutputText        *string                            `json:"output_text,omitempty"`
	Output            []publicOpenAIResponsesOutput      `json:"output"`
	Usage             publicOpenAIResponsesProviderUsage `json:"usage"`
}

type publicOpenAIResponsesOutput struct {
	ID      string                         `json:"id"`
	Type    string                         `json:"type"`
	Status  string                         `json:"status"`
	Role    string                         `json:"role"`
	Content []publicOpenAIResponsesContent `json:"content"`
}

type publicOpenAIResponsesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type publicOpenAIResponsesProviderUsage struct {
	InputTokens        json.Number                  `json:"input_tokens"`
	InputTokenDetails  *openAIResponsesInputDetail  `json:"input_tokens_details,omitempty"`
	OutputTokens       json.Number                  `json:"output_tokens"`
	OutputTokenDetails *openAIResponsesOutputDetail `json:"output_tokens_details,omitempty"`
	TotalTokens        json.Number                  `json:"total_tokens"`
}

var publicOpenAIResponsesExactResponseSchema = exactJSONObjectSchema{
	"id":                 {},
	"object":             {},
	"status":             {},
	"model":              {},
	"error":              {},
	"incomplete_details": {},
	"output_text":        {},
	"output": {arrayItem: exactJSONObjectSchema{
		"id":     {},
		"type":   {},
		"status": {},
		"role":   {},
		"content": {arrayItem: exactJSONObjectSchema{
			"type": {},
			"text": {},
		}},
	}},
	"usage": {object: exactJSONObjectSchema{
		"input_tokens":  {},
		"output_tokens": {},
		"total_tokens":  {},
		"input_tokens_details": {object: exactJSONObjectSchema{
			"cached_tokens":      {},
			"cache_write_tokens": {},
		}},
		"output_tokens_details": {object: exactJSONObjectSchema{
			"reasoning_tokens": {},
		}},
	}},
}

type publicOpenAIResponsesDeliveryDocument struct {
	response publicOpenAIResponsesProviderResponse
}

func (document publicOpenAIResponsesDeliveryDocument) canonicalBody() ([]byte, error) {
	return json.Marshal(document.response)
}

func (document publicOpenAIResponsesDeliveryDocument) texts() []PublicText {
	var result []PublicText
	for outputIndex, output := range document.response.Output {
		for contentIndex, content := range output.Content {
			result = append(result, PublicText{
				Path: "output[" + strconv.Itoa(outputIndex) + "].content[" + strconv.Itoa(contentIndex) + "].text",
				Text: content.Text,
			})
		}
	}
	return result
}

func (document publicOpenAIResponsesDeliveryDocument) withTexts(replacements []string) (publicDeliveryDocument, error) {
	texts := document.texts()
	if len(replacements) != len(texts) {
		return nil, replacementError(len(texts), len(replacements))
	}
	response := document.response
	response.Output = append([]publicOpenAIResponsesOutput(nil), document.response.Output...)
	var combined strings.Builder
	textIndex := 0
	for outputIndex := range response.Output {
		response.Output[outputIndex].Content = append([]publicOpenAIResponsesContent(nil), document.response.Output[outputIndex].Content...)
		for contentIndex := range response.Output[outputIndex].Content {
			response.Output[outputIndex].Content[contentIndex].Text = replacements[textIndex]
			combined.WriteString(replacements[textIndex])
			textIndex++
		}
	}
	outputText := combined.String()
	response.OutputText = &outputText
	return publicOpenAIResponsesDeliveryDocument{response: response}, nil
}

func (profile publicOpenAIResponsesProfile) parseDelivery(
	ctx context.Context,
	binding *publicResponseBinding,
	statusCode int,
	body []byte,
) (PublicDelivery, error) {
	if err := requirePublicBinding(profile, binding); err != nil {
		return PublicDelivery{}, err
	}
	if statusCode != http.StatusOK {
		failureKind, _ := profile.ClassifyHTTPStatus(statusCode)
		return PublicDelivery{}, protocolError(ErrUpstreamResponse, "http_"+strconv.Itoa(statusCode)+"_"+failureKind)
	}
	var response publicOpenAIResponsesProviderResponse
	if err := decodeStrictPublicResponse(body, &response, publicOpenAIResponsesExactResponseSchema); err != nil {
		return PublicDelivery{}, err
	}
	// Retain the proven provider semantic checks while applying a stricter,
	// public-only response schema below. The historical parser remains unchanged.
	outcome, err := profile.base.ParseResponse(ctx, binding.base, statusCode, body)
	if err != nil {
		return PublicDelivery{}, err
	}
	if response.ID == "" || response.Object != "response" || response.Status != "completed" ||
		response.Model != binding.mappedModel || !isExplicitJSONNull(response.Error) ||
		!isExplicitJSONNull(response.IncompleteDetails) || len(response.Output) == 0 {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "public_response_identity_invalid")
	}
	var combined strings.Builder
	for _, output := range response.Output {
		if output.ID == "" || output.Type != "message" || output.Status != "completed" ||
			output.Role != "assistant" || len(output.Content) == 0 {
			return PublicDelivery{}, protocolError(ErrInvalidResponse, "public_response_message_invalid")
		}
		for _, content := range output.Content {
			if content.Type != "output_text" {
				return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_public_response_content")
			}
			combined.WriteString(content.Text)
		}
	}
	outputText := combined.String()
	if response.OutputText != nil && *response.OutputText != outputText {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "output_text_mismatch")
	}
	usage, err := publicOpenAIUsage(response.Usage)
	if err != nil {
		return PublicDelivery{}, err
	}
	if err := usage.validate(); err != nil {
		return PublicDelivery{}, err
	}
	response.Model = binding.requestedModel
	response.OutputText = &outputText
	document := publicOpenAIResponsesDeliveryDocument{response: response}
	if err := validatePublicDeliveryDocument(document); err != nil {
		return PublicDelivery{}, err
	}
	return PublicDelivery{
		endpointKind:   EndpointResponses,
		providerObject: outcome.ProviderObject,
		usage:          usage,
		document:       document,
	}, nil
}

func publicOpenAIUsage(raw publicOpenAIResponsesProviderUsage) (PublicCustomerUsage, error) {
	inputTokens, err := parseNonnegativeInt64(raw.InputTokens, string(UnitInputTokens))
	if err != nil {
		return PublicCustomerUsage{}, err
	}
	outputTokens, err := parseNonnegativeInt64(raw.OutputTokens, string(UnitOutputTokens))
	if err != nil {
		return PublicCustomerUsage{}, err
	}
	totalTokens, err := parseNonnegativeInt64(raw.TotalTokens, "total_tokens")
	if err != nil || inputTokens > math.MaxInt64-outputTokens || inputTokens+outputTokens != totalTokens {
		return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "usage_total_mismatch")
	}
	usage := PublicCustomerUsage{InputTokens: inputTokens, OutputTokens: outputTokens}
	if raw.InputTokenDetails != nil {
		if raw.InputTokenDetails.CachedTokens != nil {
			usage.CacheReadTokens, err = parseNonnegativeInt64(*raw.InputTokenDetails.CachedTokens, string(UnitCacheReadTokens))
			if err != nil {
				return PublicCustomerUsage{}, err
			}
		}
		if raw.InputTokenDetails.CacheWriteTokens != nil {
			usage.CacheWriteTokens, err = parseNonnegativeInt64(*raw.InputTokenDetails.CacheWriteTokens, string(UnitCacheWriteTokens))
			if err != nil {
				return PublicCustomerUsage{}, err
			}
		}
	}
	if raw.OutputTokenDetails != nil && raw.OutputTokenDetails.ReasoningTokens != nil {
		reasoning, parseErr := parseNonnegativeInt64(*raw.OutputTokenDetails.ReasoningTokens, "reasoning_tokens")
		if parseErr != nil || reasoning > outputTokens {
			return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "reasoning_tokens_invalid")
		}
	}
	return usage, nil
}

func (profile publicOpenAIResponsesProfile) ClassifyHTTPStatus(statusCode int) (string, bool) {
	return profile.base.ClassifyHTTPStatus(statusCode)
}
