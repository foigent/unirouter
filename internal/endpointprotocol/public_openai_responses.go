package endpointprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
)

type publicOpenAIResponsesProfile struct {
	base      Profile
	identity  PublicContractIdentity
	streaming bool
}

func (profile publicOpenAIResponsesProfile) Descriptor() PublicNonStreamDescriptor {
	base := profile.base.Descriptor()
	return PublicNonStreamDescriptor{
		Identity:           profile.identity,
		EndpointKind:       base.EndpointKind,
		DownstreamEndpoint: base.DownstreamEndpoint,
		UpstreamEndpoint:   base.UpstreamEndpoint,
		CachePolicy:        PublicCachePolicyBypass,
		Streaming:          profile.streaming,
		Modality:           "text",
	}
}

type publicOpenAIRequestDocument struct {
	envelope        openAIResponsesEnvelope
	paths           []string
	requestContract PublicRequestContract
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
	if err := validatePublicOpenAIResponsesRequestForMode(
		envelope, mustJSONString(envelope["model"]), document.requestContract,
		mustJSONBool(envelope["stream"]),
	); err != nil {
		return nil, err
	}
	return publicOpenAIRequestDocument{
		envelope: envelope, paths: append([]string(nil), document.paths...),
		requestContract: clonePublicRequestContract(document.requestContract),
	}, nil
}

func mustJSONString(raw json.RawMessage) string {
	value, _ := decodeJSONString(raw)
	return value
}

func mustJSONBool(raw json.RawMessage) bool {
	var value bool
	_ = json.Unmarshal(raw, &value)
	return value
}

func (profile publicOpenAIResponsesProfile) PrepareRequest(
	body []byte,
	requestedModel string,
	contracts ...PublicRequestContract,
) (*PublicPreparedRequest, error) {
	contract, err := resolvePublicRequestContract(contracts)
	if err != nil {
		return nil, err
	}
	envelope, err := parseOpenAIResponsesEnvelope(body)
	if err != nil {
		return nil, err
	}
	if err := validatePublicOpenAIResponsesRequestForMode(envelope, requestedModel, contract, profile.streaming); err != nil {
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

	// The exact public registry fixes transport mode. Raw request data may only
	// agree with that identity; it never selects another executor after Plan.
	envelope["store"] = json.RawMessage("false")
	if profile.streaming {
		envelope["stream"] = json.RawMessage("true")
	} else {
		envelope["stream"] = json.RawMessage("false")
	}
	paths := []string{"input"}
	if _, exists := envelope["instructions"]; exists {
		paths = append(paths, "instructions")
	}
	document := publicOpenAIRequestDocument{
		envelope: envelope, paths: paths,
		requestContract: clonePublicRequestContract(contract),
	}
	if err := validatePublicRequestDocument(document); err != nil {
		return nil, err
	}
	return &PublicPreparedRequest{
		identity:        profile.Descriptor().Identity,
		endpointKind:    EndpointResponses,
		requestedModel:  requestedModel,
		maxOutput:       maxOutput,
		requestContract: contract,
		document:        document,
		semantics: PublicInvocationSemantics{
			ReasoningEffortExplicit: envelope["reasoning"] != nil,
			Streaming:               profile.streaming,
		},
	}, nil
}

func validatePublicOpenAIResponsesRequest(
	envelope openAIResponsesEnvelope,
	requestedModel string,
	contract PublicRequestContract,
) error {
	if contract.AnthropicMessages != nil {
		return protocolError(ErrInvalidConfiguration, "public_request_contract_surface_mismatch")
	}
	rawReasoning, hasReasoning := envelope["reasoning"]
	if !hasReasoning || contract.Reasoning == nil {
		return validateOpenAIResponsesRequest(envelope, requestedModel)
	}
	if contract.Reasoning.RequestObjectField != "reasoning" || contract.Reasoning.EffortField != "effort" {
		return protocolError(ErrInvalidConfiguration, "public_reasoning_contract_shape_invalid")
	}
	core := make(openAIResponsesEnvelope, len(envelope)-1)
	for field, raw := range envelope {
		if field != "reasoning" {
			core[field] = raw
		}
	}
	if err := validateOpenAIResponsesRequest(core, requestedModel); err != nil {
		return err
	}
	if isExplicitJSONNull(rawReasoning) {
		return protocolError(ErrInvalidRequest, "reasoning_invalid")
	}
	var reasoning map[string]json.RawMessage
	if json.Unmarshal(rawReasoning, &reasoning) != nil || reasoning == nil {
		return protocolError(ErrInvalidRequest, "reasoning_invalid")
	}
	for field := range reasoning {
		switch field {
		case contract.Reasoning.EffortField:
		case "summary":
			return protocolError(ErrUnsupportedSemantics, "unsupported_reasoning_summary")
		default:
			return protocolError(ErrUnsupportedSemantics, "unsupported_unknown_reasoning_field")
		}
	}
	effort, ok := decodeRequiredString(reasoning[contract.Reasoning.EffortField])
	if !ok {
		return protocolError(ErrInvalidRequest, "reasoning_effort_invalid")
	}
	for _, allowed := range contract.Reasoning.AllowedEfforts {
		if effort == allowed {
			return nil
		}
	}
	return protocolError(ErrInvalidRequest, "reasoning_effort_invalid")
}

func validatePublicOpenAIResponsesRequestForMode(
	envelope openAIResponsesEnvelope,
	requestedModel string,
	contract PublicRequestContract,
	streaming bool,
) error {
	wireMode, err := exactResponsesStreamValue(envelope)
	if err != nil || wireMode != streaming {
		return protocolError(ErrInvalidRequest, "stream_contract_mismatch")
	}
	validation := cloneOpenAIResponsesEnvelope(envelope)
	validation["stream"] = json.RawMessage("false")
	return validatePublicOpenAIResponsesRequest(validation, requestedModel, contract)
}

func (profile publicOpenAIResponsesProfile) BuildUpstream(
	ctx context.Context,
	prepared *PublicPreparedRequest,
	input PublicUpstreamInput,
) (*PublicUpstreamRequest, error) {
	if err := requirePublicPrepared(profile, prepared); err != nil {
		return nil, err
	}
	envelope, err := parseOpenAIResponsesEnvelope(prepared.CanonicalBody())
	if err != nil {
		return nil, err
	}
	if err := validatePublicOpenAIResponsesRequestForMode(
		envelope, prepared.requestedModel, prepared.requestContract, profile.streaming,
	); err != nil {
		return nil, err
	}
	mappedModel, err := json.Marshal(input.MappedModel)
	if err != nil {
		return nil, protocolError(ErrInvalidRequest, "mapped_model_encoding_failed")
	}
	envelope["model"] = mappedModel
	envelope["store"] = json.RawMessage("false")
	if profile.streaming {
		envelope["stream"] = json.RawMessage("true")
	} else {
		envelope["stream"] = json.RawMessage("false")
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	baseURL, err := validateBuildInput(BuildInput{
		BaseURL: input.BaseURL, APIKey: input.APIKey, RequestID: input.RequestID,
		RequestedModel: prepared.requestedModel, MappedModel: input.MappedModel, Body: body,
	}, true)
	if err != nil {
		return nil, err
	}
	httpRequest, err := buildPOSTRequest(ctx, baseURL, "/v1/responses", input.APIKey, input.RequestID, body)
	if err != nil {
		return nil, err
	}
	baseDescriptor := profile.base.Descriptor()
	baseBinding := &Binding{
		profileID: baseDescriptor.ProfileID, profileVersion: baseDescriptor.ProfileVersion,
		requestedModel: prepared.requestedModel, mappedModel: input.MappedModel,
		streaming: profile.streaming,
	}
	return &PublicUpstreamRequest{
		profile: profile, request: httpRequest,
		binding: &publicResponseBinding{
			identity: prepared.identity, base: baseBinding,
			requestedModel: prepared.requestedModel, mappedModel: input.MappedModel,
			requestContract: clonePublicRequestContract(prepared.requestContract),
		},
	}, nil
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

// publicOpenAIResponsesWireResponse admits the documented top-level Response
// envelope while keeping customer delivery on the narrower text projection
// above. Fields not used for delivery remain raw: accepting a provider-owned
// response attribute must not silently turn it into capability, tier, or
// billing evidence.
type publicOpenAIResponsesWireResponse struct {
	ID                   string                             `json:"id"`
	Object               string                             `json:"object"`
	Status               string                             `json:"status"`
	Model                string                             `json:"model"`
	Error                json.RawMessage                    `json:"error"`
	IncompleteDetails    json.RawMessage                    `json:"incomplete_details"`
	OutputText           *string                            `json:"output_text,omitempty"`
	Output               []json.RawMessage                  `json:"output"`
	Usage                publicOpenAIResponsesProviderUsage `json:"usage"`
	Background           json.RawMessage                    `json:"background,omitempty"`
	Billing              json.RawMessage                    `json:"billing,omitempty"`
	CompletedAt          json.RawMessage                    `json:"completed_at,omitempty"`
	ContextManagement    json.RawMessage                    `json:"context_management,omitempty"`
	Conversation         json.RawMessage                    `json:"conversation,omitempty"`
	CreatedAt            json.RawMessage                    `json:"created_at,omitempty"`
	Instructions         json.RawMessage                    `json:"instructions,omitempty"`
	MaxOutputTokens      json.RawMessage                    `json:"max_output_tokens,omitempty"`
	MaxToolCalls         json.RawMessage                    `json:"max_tool_calls,omitempty"`
	Metadata             json.RawMessage                    `json:"metadata,omitempty"`
	ParallelToolCalls    json.RawMessage                    `json:"parallel_tool_calls,omitempty"`
	PreviousResponseID   json.RawMessage                    `json:"previous_response_id,omitempty"`
	Prompt               json.RawMessage                    `json:"prompt,omitempty"`
	PromptCacheKey       json.RawMessage                    `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention json.RawMessage                    `json:"prompt_cache_retention,omitempty"`
	Reasoning            json.RawMessage                    `json:"reasoning,omitempty"`
	SafetyIdentifier     json.RawMessage                    `json:"safety_identifier,omitempty"`
	ServiceTier          json.RawMessage                    `json:"service_tier,omitempty"`
	Store                json.RawMessage                    `json:"store,omitempty"`
	Temperature          json.RawMessage                    `json:"temperature,omitempty"`
	Text                 json.RawMessage                    `json:"text,omitempty"`
	ToolChoice           json.RawMessage                    `json:"tool_choice,omitempty"`
	Tools                json.RawMessage                    `json:"tools,omitempty"`
	TopLogprobs          json.RawMessage                    `json:"top_logprobs,omitempty"`
	TopP                 json.RawMessage                    `json:"top_p,omitempty"`
	Truncation           json.RawMessage                    `json:"truncation,omitempty"`
	User                 json.RawMessage                    `json:"user,omitempty"`
}

type publicOpenAIResponsesOutput struct {
	ID      string                         `json:"id"`
	Type    string                         `json:"type"`
	Status  string                         `json:"status"`
	Role    string                         `json:"role"`
	Phase   string                         `json:"phase,omitempty"`
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
	"id": {}, "object": {}, "status": {}, "model": {}, "error": {},
	"incomplete_details": {}, "output_text": {}, "output": {},
	"background": {}, "billing": {}, "completed_at": {},
	"context_management": {}, "conversation": {}, "created_at": {},
	"instructions": {}, "max_output_tokens": {}, "max_tool_calls": {},
	"metadata": {}, "parallel_tool_calls": {}, "previous_response_id": {},
	"prompt": {}, "prompt_cache_key": {}, "prompt_cache_retention": {},
	"reasoning": {}, "safety_identifier": {}, "service_tier": {},
	"store": {}, "temperature": {}, "text": {}, "tool_choice": {},
	"tools": {}, "top_logprobs": {}, "top_p": {}, "truncation": {},
	"user": {},
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

var publicOpenAIResponsesMessageSchema = exactJSONObjectSchema{
	"id": {}, "type": {}, "status": {}, "role": {}, "phase": {},
	"content": {arrayItem: exactJSONObjectSchema{
		"type": {}, "text": {}, "annotations": {}, "logprobs": {},
	}},
}

var publicOpenAIResponsesReasoningSchema = exactJSONObjectSchema{
	"id": {}, "type": {}, "status": {}, "summary": {}, "content": {},
	"encrypted_content": {}, "phase": {},
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
	var wire publicOpenAIResponsesWireResponse
	if err := decodeStrictPublicResponse(body, &wire, publicOpenAIResponsesExactResponseSchema); err != nil {
		return PublicDelivery{}, err
	}
	if wire.ID == "" || wire.Object != "response" || wire.Status != "completed" ||
		wire.Model != binding.mappedModel || !isExplicitJSONNull(wire.Error) ||
		!isExplicitJSONNull(wire.IncompleteDetails) || len(wire.Output) == 0 {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "public_response_identity_invalid")
	}
	if err := validatePublicOpenAIResponsesCoreMetadata(wire); err != nil {
		return PublicDelivery{}, err
	}
	reasoningEnabled := binding.requestContract.Reasoning != nil
	outputs, outputText, err := projectPublicOpenAIResponsesMessages(wire.Output, reasoningEnabled)
	if err != nil {
		return PublicDelivery{}, err
	}
	if wire.OutputText != nil && *wire.OutputText != outputText {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "output_text_mismatch")
	}
	// Retain the proven provider semantic checks after the public exact-member
	// boundary has rejected aliases at every output layer. The historical parser
	// remains unchanged and may continue to tolerate future provider evidence.
	providerObject := wire.ID
	if !reasoningEnabled {
		outcome, err := profile.base.ParseResponse(ctx, binding.base, statusCode, body)
		if err != nil {
			return PublicDelivery{}, err
		}
		providerObject = outcome.ProviderObject
	}
	usage, err := publicOpenAIUsage(wire.Usage)
	if err != nil {
		return PublicDelivery{}, err
	}
	if err := usage.validate(); err != nil {
		return PublicDelivery{}, err
	}
	response := publicOpenAIResponsesProviderResponse{
		ID: wire.ID, Object: wire.Object, Status: wire.Status, Model: binding.requestedModel,
		Error: wire.Error, IncompleteDetails: wire.IncompleteDetails,
		OutputText: &outputText, Output: outputs, Usage: wire.Usage,
	}
	document := publicOpenAIResponsesDeliveryDocument{response: response}
	if err := validatePublicDeliveryDocument(document); err != nil {
		return PublicDelivery{}, err
	}
	return PublicDelivery{
		endpointKind:   EndpointResponses,
		providerObject: providerObject,
		usage:          usage,
		document:       document,
	}, nil
}

func validatePublicOpenAIResponsesCoreMetadata(response publicOpenAIResponsesWireResponse) error {
	for _, field := range []struct {
		name     string
		raw      json.RawMessage
		nullable bool
		shapes   []publicOpenAIResponseJSONShape
	}{
		{name: "background", raw: response.Background, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONBool}},
		{name: "billing", raw: response.Billing, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONObject}},
		{name: "completed_at", raw: response.CompletedAt, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONNumber}},
		{name: "context_management", raw: response.ContextManagement, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONArray}},
		{name: "conversation", raw: response.Conversation, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString, publicResponseJSONObject}},
		{name: "created_at", raw: response.CreatedAt, nullable: false, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONNumber}},
		{name: "instructions", raw: response.Instructions, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "max_output_tokens", raw: response.MaxOutputTokens, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONNumber}},
		{name: "max_tool_calls", raw: response.MaxToolCalls, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONNumber}},
		{name: "metadata", raw: response.Metadata, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONObject}},
		{name: "parallel_tool_calls", raw: response.ParallelToolCalls, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONBool}},
		{name: "previous_response_id", raw: response.PreviousResponseID, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "prompt", raw: response.Prompt, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONObject}},
		{name: "prompt_cache_key", raw: response.PromptCacheKey, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "prompt_cache_retention", raw: response.PromptCacheRetention, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "reasoning", raw: response.Reasoning, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONObject}},
		{name: "safety_identifier", raw: response.SafetyIdentifier, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "service_tier", raw: response.ServiceTier, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "store", raw: response.Store, nullable: false, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONBool}},
		{name: "temperature", raw: response.Temperature, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONNumber}},
		{name: "text", raw: response.Text, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONObject}},
		{name: "tool_choice", raw: response.ToolChoice, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString, publicResponseJSONObject}},
		{name: "tools", raw: response.Tools, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONArray}},
		{name: "top_logprobs", raw: response.TopLogprobs, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONNumber}},
		{name: "top_p", raw: response.TopP, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONNumber}},
		{name: "truncation", raw: response.Truncation, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "user", raw: response.User, nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
	} {
		if err := validatePublicOpenAIResponseRawShape(field.raw, field.name, field.nullable, field.shapes...); err != nil {
			return err
		}
	}
	if len(response.Background) > 0 && !isExplicitJSONNull(response.Background) {
		var background bool
		if json.Unmarshal(response.Background, &background) != nil {
			return protocolError(ErrInvalidResponse, "public_response_background_invalid")
		}
		if background {
			return protocolError(ErrUnsupportedSemantics, "unsupported_public_background_response")
		}
	}
	if len(response.Store) > 0 {
		var store bool
		if isExplicitJSONNull(response.Store) || json.Unmarshal(response.Store, &store) != nil {
			return protocolError(ErrInvalidResponse, "public_response_store_mismatch")
		}
		if store {
			return protocolError(ErrUnsupportedSemantics, "unsupported_public_stored_response")
		}
	}
	for _, state := range []json.RawMessage{
		response.ContextManagement,
		response.Conversation,
		response.PreviousResponseID,
		response.Prompt,
		response.PromptCacheKey,
		response.PromptCacheRetention,
		response.SafetyIdentifier,
		response.User,
	} {
		if len(state) > 0 && !isExplicitJSONNull(state) {
			return protocolError(ErrUnsupportedSemantics, "unsupported_public_conversation_response")
		}
	}
	if len(response.Tools) > 0 && !isExplicitJSONNull(response.Tools) {
		var tools []json.RawMessage
		if json.Unmarshal(response.Tools, &tools) != nil {
			return protocolError(ErrInvalidResponse, "public_response_tools_invalid")
		}
		if len(tools) != 0 {
			return protocolError(ErrUnsupportedSemantics, "unsupported_public_tools_response")
		}
	}
	if len(response.Text) > 0 && !isExplicitJSONNull(response.Text) {
		if err := validateOpenAITextConfig(response.Text); err != nil {
			if errors.Is(err, ErrUnsupportedSemantics) {
				return protocolError(ErrUnsupportedSemantics, "unsupported_public_text_config")
			}
			return protocolError(ErrInvalidResponse, "public_response_text_config_invalid")
		}
	}
	return nil
}

type publicOpenAIResponseJSONShape uint8

const (
	publicResponseJSONBool publicOpenAIResponseJSONShape = iota
	publicResponseJSONNumber
	publicResponseJSONString
	publicResponseJSONObject
	publicResponseJSONArray
)

// validatePublicOpenAIResponseRawShape is a quarantine boundary for response
// attributes that the strict public text projection deliberately drops. It
// validates only JSON shape and nullability; it does not promote those fields
// to executable capability, tier evidence, or billing authority.
func validatePublicOpenAIResponseRawShape(
	raw json.RawMessage,
	field string,
	nullable bool,
	shapes ...publicOpenAIResponseJSONShape,
) error {
	if len(raw) == 0 {
		return nil
	}
	if isExplicitJSONNull(raw) {
		if nullable {
			return nil
		}
		return protocolError(ErrInvalidResponse, "public_response_"+field+"_invalid")
	}
	for _, shape := range shapes {
		switch shape {
		case publicResponseJSONBool:
			var value bool
			if json.Unmarshal(raw, &value) == nil {
				return nil
			}
		case publicResponseJSONNumber:
			if _, ok := decodeJSONNumber(raw); ok {
				return nil
			}
		case publicResponseJSONString:
			if _, ok := decodeJSONString(raw); ok {
				return nil
			}
		case publicResponseJSONObject:
			var value map[string]json.RawMessage
			if json.Unmarshal(raw, &value) == nil && value != nil {
				return nil
			}
		case publicResponseJSONArray:
			var value []json.RawMessage
			if json.Unmarshal(raw, &value) == nil && value != nil {
				return nil
			}
		}
	}
	return protocolError(ErrInvalidResponse, "public_response_"+field+"_invalid")
}

func projectPublicOpenAIResponsesMessages(
	outputs []json.RawMessage,
	allowEncryptedReasoning bool,
) ([]publicOpenAIResponsesOutput, string, error) {
	messages := make([]publicOpenAIResponsesOutput, 0, len(outputs))
	var combined strings.Builder
	for _, raw := range outputs {
		var identity map[string]json.RawMessage
		if json.Unmarshal(raw, &identity) != nil || identity == nil {
			return nil, "", protocolError(ErrInvalidResponse, "public_response_output_invalid")
		}
		outputType, ok := decodeRequiredString(identity["type"])
		if !ok {
			return nil, "", protocolError(ErrInvalidResponse, "public_response_output_invalid")
		}
		switch outputType {
		case "reasoning":
			if err := validateExactJSONObjectMembers(raw, maxResponseBytes, ErrInvalidResponse, "public_response_reasoning", publicOpenAIResponsesReasoningSchema); err != nil {
				return nil, "", err
			}
			if err := validatePublicOpenAIResponsesReasoningShape(identity); err != nil {
				return nil, "", err
			}
			if err := validatePublicOpenAIReasoningOutput(identity, allowEncryptedReasoning); err != nil {
				return nil, "", err
			}
			continue
		case "message":
			if err := validateExactJSONObjectMembers(raw, maxResponseBytes, ErrInvalidResponse, "public_response_message", publicOpenAIResponsesMessageSchema); err != nil {
				return nil, "", err
			}
		default:
			return nil, "", protocolError(ErrUnsupportedSemantics, "unsupported_public_response_output")
		}

		var message struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Status  string `json:"status"`
			Role    string `json:"role"`
			Phase   string `json:"phase"`
			Content []struct {
				Type        string            `json:"type"`
				Text        string            `json:"text"`
				Annotations []json.RawMessage `json:"annotations"`
				Logprobs    []json.RawMessage `json:"logprobs"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &message) != nil || message.ID == "" || message.Status != "completed" ||
			message.Role != "assistant" || len(message.Content) == 0 ||
			(message.Phase != "" && message.Phase != "commentary" && message.Phase != "final_answer") {
			return nil, "", protocolError(ErrInvalidResponse, "public_response_message_invalid")
		}
		projected := publicOpenAIResponsesOutput{
			ID: message.ID, Type: message.Type, Status: message.Status, Role: message.Role, Phase: message.Phase,
			Content: make([]publicOpenAIResponsesContent, 0, len(message.Content)),
		}
		for _, content := range message.Content {
			if content.Type != "output_text" || len(content.Annotations) != 0 || len(content.Logprobs) != 0 {
				return nil, "", protocolError(ErrUnsupportedSemantics, "unsupported_public_response_content")
			}
			projected.Content = append(projected.Content, publicOpenAIResponsesContent{Type: content.Type, Text: content.Text})
			combined.WriteString(content.Text)
		}
		messages = append(messages, projected)
	}
	if len(messages) == 0 {
		return nil, "", protocolError(ErrInvalidResponse, "public_response_text_output_missing")
	}
	return messages, combined.String(), nil
}

func validatePublicOpenAIReasoningOutput(
	output map[string]json.RawMessage,
	allowEncrypted bool,
) error {
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
		if _, ok := decodeJSONString(raw); !ok {
			return protocolError(ErrInvalidResponse, "response_reasoning_item_invalid")
		}
		if !allowEncrypted {
			return protocolError(ErrUnsupportedSemantics, "unsupported_reasoning_output")
		}
	}
	return nil
}

func validatePublicOpenAIResponsesReasoningShape(output map[string]json.RawMessage) error {
	for _, field := range []struct {
		name     string
		nullable bool
		shapes   []publicOpenAIResponseJSONShape
	}{
		{name: "id", shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "type", shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "status", nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "summary", nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONArray}},
		{name: "content", nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONArray}},
		{name: "encrypted_content", nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
		{name: "phase", nullable: true, shapes: []publicOpenAIResponseJSONShape{publicResponseJSONString}},
	} {
		raw, exists := output[field.name]
		if !exists {
			continue
		}
		if err := validatePublicOpenAIResponseRawShape(raw, "reasoning_"+field.name, field.nullable, field.shapes...); err != nil {
			return err
		}
	}
	return nil
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
