package endpointprotocol

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"

	"github.com/uniroute/unirouter/internal/modeloperation"
)

type publicAnthropicMessagesProfile struct {
	base anthropicMessagesProfile
}

var publicAnthropicExactRequestSchema = exactJSONObjectSchema{
	"model":      {},
	"max_tokens": {},
	"messages": {arrayItem: exactJSONObjectSchema{
		"role":    {},
		"content": {},
	}},
	"system":      {},
	"temperature": {},
	"top_p":       {},
	"stream":      {},
}

func (profile publicAnthropicMessagesProfile) Descriptor() PublicNonStreamDescriptor {
	base := profile.base.Descriptor()
	return PublicNonStreamDescriptor{
		Identity: publicIdentity(
			modeloperation.IDMessages,
			modeloperation.PathMessages,
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

type publicAnthropicRequestDocument struct {
	request anthropicMessagesWireRequest
	paths   []string
}

func (document publicAnthropicRequestDocument) canonicalBody() ([]byte, error) {
	return json.Marshal(document.request)
}

func (document publicAnthropicRequestDocument) texts() []PublicText {
	result := make([]PublicText, 0, len(document.paths))
	pathIndex := 0
	if document.request.System != "" {
		result = append(result, PublicText{Path: document.paths[pathIndex], Text: document.request.System})
		pathIndex++
	}
	for _, message := range document.request.Messages {
		result = append(result, PublicText{Path: document.paths[pathIndex], Text: message.Content})
		pathIndex++
	}
	return result
}

func (document publicAnthropicRequestDocument) withTexts(replacements []string) (publicRequestDocument, error) {
	if len(replacements) != len(document.paths) {
		return nil, replacementError(len(document.paths), len(replacements))
	}
	request := document.request
	request.Messages = append([]anthropicMessageRequest(nil), document.request.Messages...)
	textIndex := 0
	if request.System != "" {
		if replacements[textIndex] == "" {
			return nil, protocolError(ErrInvalidRequest, "guardrail_text_empty")
		}
		request.System = replacements[textIndex]
		textIndex++
	}
	for messageIndex := range request.Messages {
		if replacements[textIndex] == "" {
			return nil, protocolError(ErrInvalidRequest, "guardrail_text_empty")
		}
		request.Messages[messageIndex].Content = replacements[textIndex]
		textIndex++
	}
	return publicAnthropicRequestDocument{request: request, paths: append([]string(nil), document.paths...)}, nil
}

func (profile publicAnthropicMessagesProfile) PrepareRequest(body []byte, requestedModel string) (*PublicPreparedRequest, error) {
	if err := validateExactJSONObjectMembers(
		body,
		maxRequestBytes,
		ErrInvalidRequest,
		"public_request",
		publicAnthropicExactRequestSchema,
	); err != nil {
		return nil, err
	}
	// Reuse the historical profile's strict request semantics without selecting
	// a provider credential. A fixed synthetic HTTPS origin/key and the public
	// model as both sides of the binding make this a validation-only build.
	request, _, err := profile.base.BuildRequest(context.Background(), BuildInput{
		BaseURL:        "https://validation.invalid",
		APIKey:         "validation-only",
		RequestedModel: requestedModel,
		MappedModel:    requestedModel,
		Body:           body,
	})
	if err != nil {
		return nil, err
	}
	defer request.Body.Close()
	var normalized anthropicMessagesWireRequest
	decoder := json.NewDecoder(request.Body)
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&normalized); err != nil || requireDecoderEOF(decoder) != nil {
		return nil, protocolError(ErrInvalidRequest, "public_request_normalization_failed")
	}
	maxOutput, err := parseRequestInt64(normalized.MaxTokens)
	if err != nil || maxOutput <= 0 {
		return nil, protocolError(ErrInvalidRequest, "max_tokens_invalid")
	}
	paths := make([]string, 0, len(normalized.Messages)+1)
	if normalized.System != "" {
		paths = append(paths, "system")
	}
	for index := range normalized.Messages {
		paths = append(paths, "messages["+strconv.Itoa(index)+"].content")
	}
	document := publicAnthropicRequestDocument{request: normalized, paths: paths}
	if err := validatePublicRequestDocument(document); err != nil {
		return nil, err
	}
	return &PublicPreparedRequest{
		identity:       profile.Descriptor().Identity,
		endpointKind:   EndpointMessages,
		requestedModel: requestedModel,
		maxOutput:      maxOutput,
		document:       document,
	}, nil
}

func (profile publicAnthropicMessagesProfile) BuildUpstream(
	ctx context.Context,
	prepared *PublicPreparedRequest,
	input PublicUpstreamInput,
) (*PublicUpstreamRequest, error) {
	return buildPublicUpstream(ctx, profile, profile.base, prepared, input)
}

type publicAnthropicProviderResponse struct {
	ID           string                        `json:"id"`
	Type         string                        `json:"type"`
	Role         string                        `json:"role"`
	Model        string                        `json:"model"`
	Content      []publicAnthropicContent      `json:"content"`
	StopReason   string                        `json:"stop_reason"`
	StopSequence json.RawMessage               `json:"stop_sequence"`
	Usage        *publicAnthropicProviderUsage `json:"usage"`
}

type publicAnthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type publicAnthropicProviderUsage struct {
	InputTokens              json.Number                    `json:"input_tokens"`
	CacheCreationInputTokens *json.Number                   `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *json.Number                   `json:"cache_read_input_tokens,omitempty"`
	OutputTokens             json.Number                    `json:"output_tokens"`
	CacheCreation            *anthropicCacheCreationDetails `json:"cache_creation,omitempty"`
}

var publicAnthropicExactResponseSchema = exactJSONObjectSchema{
	"id":    {},
	"type":  {},
	"role":  {},
	"model": {},
	"content": {arrayItem: exactJSONObjectSchema{
		"type": {},
		"text": {},
	}},
	"stop_reason":   {},
	"stop_sequence": {},
	"usage": {object: exactJSONObjectSchema{
		"input_tokens":                {},
		"cache_creation_input_tokens": {},
		"cache_read_input_tokens":     {},
		"output_tokens":               {},
		"cache_creation": {object: exactJSONObjectSchema{
			"ephemeral_5m_input_tokens": {},
			"ephemeral_1h_input_tokens": {},
		}},
	}},
}

type publicAnthropicDeliveryDocument struct {
	response publicAnthropicProviderResponse
}

func (document publicAnthropicDeliveryDocument) canonicalBody() ([]byte, error) {
	return json.Marshal(document.response)
}

func (document publicAnthropicDeliveryDocument) texts() []PublicText {
	result := make([]PublicText, 0, len(document.response.Content))
	for index, content := range document.response.Content {
		result = append(result, PublicText{
			Path: "content[" + strconv.Itoa(index) + "].text",
			Text: content.Text,
		})
	}
	return result
}

func (document publicAnthropicDeliveryDocument) withTexts(replacements []string) (publicDeliveryDocument, error) {
	if len(replacements) != len(document.response.Content) {
		return nil, replacementError(len(document.response.Content), len(replacements))
	}
	response := document.response
	response.Content = append([]publicAnthropicContent(nil), document.response.Content...)
	for index := range response.Content {
		response.Content[index].Text = replacements[index]
	}
	return publicAnthropicDeliveryDocument{response: response}, nil
}

func (profile publicAnthropicMessagesProfile) parseDelivery(
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
	var response publicAnthropicProviderResponse
	if err := decodeStrictPublicResponse(body, &response, publicAnthropicExactResponseSchema); err != nil {
		return PublicDelivery{}, err
	}
	outcome, err := profile.base.ParseResponse(ctx, binding.base, statusCode, body)
	if err != nil {
		return PublicDelivery{}, err
	}
	if response.ID == "" || response.Type != "message" || response.Role != "assistant" ||
		response.Model != binding.mappedModel || len(response.Content) == 0 || response.Usage == nil {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "public_response_identity_invalid")
	}
	for _, content := range response.Content {
		if content.Type != "text" {
			return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_public_response_content")
		}
	}
	usage, err := publicAnthropicUsage(*response.Usage)
	if err != nil {
		return PublicDelivery{}, err
	}
	if err := usage.validate(); err != nil {
		return PublicDelivery{}, err
	}
	response.Model = binding.requestedModel
	document := publicAnthropicDeliveryDocument{response: response}
	if err := validatePublicDeliveryDocument(document); err != nil {
		return PublicDelivery{}, err
	}
	return PublicDelivery{
		endpointKind:   EndpointMessages,
		providerObject: outcome.ProviderObject,
		usage:          usage,
		document:       document,
	}, nil
}

// Anthropic reports uncached input separately from cache read/write input.
// The canonical charge contract exposes total input with cache counters as
// subsets, preserving the invariant cache_read + cache_write <= input.
func publicAnthropicUsage(raw publicAnthropicProviderUsage) (PublicCustomerUsage, error) {
	uncached, err := parseNonnegativeInt64(raw.InputTokens, string(UnitInputTokens))
	if err != nil {
		return PublicCustomerUsage{}, err
	}
	var cacheWrite, cacheRead int64
	if raw.CacheCreationInputTokens != nil {
		cacheWrite, err = parseNonnegativeInt64(*raw.CacheCreationInputTokens, string(UnitCacheWriteTokens))
		if err != nil {
			return PublicCustomerUsage{}, err
		}
	}
	if raw.CacheReadInputTokens != nil {
		cacheRead, err = parseNonnegativeInt64(*raw.CacheReadInputTokens, string(UnitCacheReadTokens))
		if err != nil {
			return PublicCustomerUsage{}, err
		}
	}
	output, err := parseNonnegativeInt64(raw.OutputTokens, string(UnitOutputTokens))
	if err != nil {
		return PublicCustomerUsage{}, err
	}
	if uncached > math.MaxInt64-cacheRead || uncached+cacheRead > math.MaxInt64-cacheWrite {
		return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "customer_usage_input_overflow")
	}
	if raw.CacheCreation != nil {
		short, parseErr := parseNonnegativeInt64(raw.CacheCreation.Ephemeral5mInputTokens, "cache_creation_ephemeral_5m_input_tokens")
		if parseErr != nil {
			return PublicCustomerUsage{}, parseErr
		}
		long, parseErr := parseNonnegativeInt64(raw.CacheCreation.Ephemeral1hInputTokens, "cache_creation_ephemeral_1h_input_tokens")
		if parseErr != nil || short > cacheWrite || long != cacheWrite-short {
			return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "cache_creation_partition_mismatch")
		}
	}
	return PublicCustomerUsage{
		InputTokens:      uncached + cacheRead + cacheWrite,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		OutputTokens:     output,
	}, nil
}

func (profile publicAnthropicMessagesProfile) ClassifyHTTPStatus(statusCode int) (string, bool) {
	return profile.base.ClassifyHTTPStatus(statusCode)
}
