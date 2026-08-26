package endpointprotocol

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"

	"github.com/uniroute/unirouter/internal/modeloperation"
)

type publicAnthropicMessagesProfile struct {
	base      anthropicMessagesProfile
	identity  PublicContractIdentity
	streaming bool
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

func publicAnthropicRequestSchema(contract PublicRequestContract) exactJSONObjectSchema {
	schema := make(exactJSONObjectSchema, len(publicAnthropicExactRequestSchema)+4)
	for field, member := range publicAnthropicExactRequestSchema {
		schema[field] = member
	}
	if contract.AnthropicMessages == nil {
		return schema
	}
	if tools := contract.AnthropicMessages.FunctionTools; tools != nil {
		schema["tools"] = exactJSONMemberSchema{arrayItem: exactJSONObjectSchema{
			"name": {}, "description": {}, "input_schema": {},
		}}
		schema["tool_choice"] = exactJSONMemberSchema{object: exactJSONObjectSchema{
			"type": {}, "name": {}, "disable_parallel_tool_use": {},
		}}
	}
	if reasoning := contract.AnthropicMessages.Reasoning; reasoning != nil {
		thinking := exactJSONObjectSchema{reasoning.ThinkingTypeField: {}}
		if reasoning.BudgetField != "" {
			thinking[reasoning.BudgetField] = exactJSONMemberSchema{}
		}
		schema[reasoning.ThinkingObjectField] = exactJSONMemberSchema{object: thinking}
		if reasoning.OutputConfigObjectField != "" {
			schema[reasoning.OutputConfigObjectField] = exactJSONMemberSchema{object: exactJSONObjectSchema{
				reasoning.EffortField: {},
			}}
		}
	}
	return schema
}

func (profile publicAnthropicMessagesProfile) Descriptor() PublicNonStreamDescriptor {
	base := profile.base.Descriptor()
	identity := profile.identity
	if !identity.valid() {
		identity = publicIdentity(
			modeloperation.IDMessages,
			modeloperation.PathMessages,
			base.ProfileID,
			base.ProfileVersion,
		)
	}
	return PublicNonStreamDescriptor{
		Identity:           identity,
		EndpointKind:       base.EndpointKind,
		DownstreamEndpoint: base.DownstreamEndpoint,
		UpstreamEndpoint:   base.UpstreamEndpoint,
		CachePolicy:        PublicCachePolicyBypass,
		Streaming:          profile.streaming,
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

func (profile publicAnthropicMessagesProfile) PrepareRequest(
	body []byte,
	requestedModel string,
	contracts ...PublicRequestContract,
) (*PublicPreparedRequest, error) {
	contract, err := resolvePublicRequestContract(contracts)
	if err != nil {
		return nil, err
	}
	if contract.Reasoning != nil {
		return nil, protocolError(ErrUnsupportedSemantics, "unsupported_reasoning_controls")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, protocolError(ErrInvalidRequest, "public_request_shape_invalid")
	}
	if messages := contract.AnthropicMessages; messages != nil {
		for _, field := range messages.ForbiddenRootFields {
			if _, present := envelope[field]; present {
				return nil, protocolError(ErrUnsupportedSemantics, "unsupported_"+field)
			}
		}
	}
	if err := validateExactJSONObjectMembers(
		body,
		maxRequestBytes,
		ErrInvalidRequest,
		"public_request",
		publicAnthropicRequestSchema(contract),
	); err != nil {
		return nil, err
	}
	streaming, err := exactAnthropicMessagesStreamValue(envelope)
	if err != nil || streaming != profile.streaming {
		return nil, protocolError(ErrInvalidRequest, "stream_contract_mismatch")
	}
	if contract.AnthropicMessages != nil && contract.AnthropicMessages.FunctionTools != nil &&
		publicAnthropicToolsShapeRequested(envelope) {
		return profile.prepareToolsRequest(body, requestedModel, contract, envelope, streaming)
	}
	// Reuse the historical profile's strict request semantics without selecting
	// a provider credential. A fixed synthetic HTTPS origin/key and the public
	// model as both sides of the binding make this a validation-only build.
	core := make(map[string]json.RawMessage, len(envelope))
	for field, raw := range envelope {
		core[field] = raw
	}
	core["stream"] = json.RawMessage("false")
	coreBody, err := json.Marshal(core)
	if err != nil {
		return nil, protocolError(ErrInvalidRequest, "public_request_normalization_failed")
	}
	if messages := contract.AnthropicMessages; messages != nil && messages.Reasoning != nil {
		core = make(map[string]json.RawMessage, len(envelope))
		for field, raw := range envelope {
			if field != messages.Reasoning.ThinkingObjectField &&
				(messages.Reasoning.OutputConfigObjectField == "" || field != messages.Reasoning.OutputConfigObjectField) {
				core[field] = raw
			}
		}
		core["stream"] = json.RawMessage("false")
		coreBody, err = json.Marshal(core)
		if err != nil {
			return nil, protocolError(ErrInvalidRequest, "public_request_normalization_failed")
		}
	}
	request, _, err := profile.base.BuildRequest(context.Background(), BuildInput{
		BaseURL:        "https://validation.invalid",
		APIKey:         "validation-only",
		RequestedModel: requestedModel,
		MappedModel:    requestedModel,
		Body:           coreBody,
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
	normalized.Stream = profile.streaming
	maxOutput, err := parseRequestInt64(normalized.MaxTokens)
	if err != nil || maxOutput <= 0 {
		return nil, protocolError(ErrInvalidRequest, "max_tokens_invalid")
	}
	thinking, outputConfig, semantics, err := normalizePublicAnthropicReasoning(
		envelope, contract.AnthropicMessages, maxOutput, profile.streaming,
	)
	if err != nil {
		return nil, err
	}
	normalized.Thinking = thinking
	normalized.OutputConfig = outputConfig
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
	if contract.AnthropicMessages != nil && contract.AnthropicMessages.RequireFinalUserMessage &&
		normalized.Messages[len(normalized.Messages)-1].Role != "user" {
		return nil, protocolError(ErrUnsupportedSemantics, "unsupported_assistant_prefill")
	}
	return &PublicPreparedRequest{
		identity:        profile.Descriptor().Identity,
		endpointKind:    EndpointMessages,
		requestedModel:  requestedModel,
		maxOutput:       maxOutput,
		requestContract: contract,
		document:        document,
		semantics:       semantics,
	}, nil
}

func exactAnthropicMessagesStreamValue(envelope map[string]json.RawMessage) (bool, error) {
	raw, present := envelope["stream"]
	if !present {
		return false, nil
	}
	var streaming bool
	if json.Unmarshal(raw, &streaming) != nil || string(raw) != strconv.FormatBool(streaming) {
		return false, protocolError(ErrInvalidRequest, "stream_invalid")
	}
	return streaming, nil
}

func decodePublicAnthropicControl(
	raw json.RawMessage,
	field string,
	allowed []string,
	code string,
) (string, error) {
	if isExplicitJSONNull(raw) {
		return "", protocolError(ErrInvalidRequest, code+"_invalid")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil || len(object) != 1 {
		return "", protocolError(ErrInvalidRequest, code+"_invalid")
	}
	value, ok := decodeRequiredString(object[field])
	if !ok || !slices.Contains(allowed, value) {
		return "", protocolError(ErrInvalidRequest, code+"_invalid")
	}
	return value, nil
}

func decodePublicAnthropicThinkingControl(
	raw json.RawMessage,
	contract *PublicAnthropicReasoningRequestContract,
	maxOutput int64,
) (string, *json.Number, error) {
	if contract == nil || isExplicitJSONNull(raw) {
		return "", nil, protocolError(ErrInvalidRequest, "reasoning_control_invalid")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return "", nil, protocolError(ErrInvalidRequest, "reasoning_control_invalid")
	}
	value, ok := decodeRequiredString(object[contract.ThinkingTypeField])
	if !ok || !slices.Contains(contract.AllowedThinkingTypes, value) {
		return "", nil, protocolError(ErrInvalidRequest, "reasoning_control_invalid")
	}
	budgetRequired := slices.Contains(contract.BudgetRequiredTypes, value)
	rawBudget, budgetPresent := object[contract.BudgetField]
	wantMembers := 1
	if budgetRequired {
		wantMembers++
	}
	if len(object) != wantMembers || budgetPresent != budgetRequired {
		return "", nil, protocolError(ErrInvalidRequest, "reasoning_budget_invalid")
	}
	if !budgetRequired {
		return value, nil, nil
	}
	budgetNumber := json.Number(string(rawBudget))
	budget, err := parseRequestInt64(budgetNumber)
	if err != nil || budget < contract.BudgetMinimum || budget > contract.BudgetMaximum || budget >= maxOutput {
		return "", nil, protocolError(ErrInvalidRequest, "reasoning_budget_invalid")
	}
	return value, &budgetNumber, nil
}

func normalizePublicAnthropicReasoning(
	envelope map[string]json.RawMessage,
	messages *PublicAnthropicMessagesContract,
	maxOutput int64,
	streaming bool,
) (*anthropicThinkingRequest, *anthropicOutputConfigRequest, PublicInvocationSemantics, error) {
	semantics := PublicInvocationSemantics{Streaming: streaming}
	if messages == nil || messages.Reasoning == nil {
		return nil, nil, semantics, nil
	}
	reasoning := messages.Reasoning
	semantics.ReasoningControlEffective = reasoning.ThinkingProviderDefault
	semantics.ReasoningEffortEffective = reasoning.EffortProviderDefault
	var thinking *anthropicThinkingRequest
	if raw, present := envelope[reasoning.ThinkingObjectField]; present {
		value, budget, err := decodePublicAnthropicThinkingControl(raw, reasoning, maxOutput)
		if err != nil {
			return nil, nil, PublicInvocationSemantics{}, err
		}
		thinking = &anthropicThinkingRequest{Type: value, BudgetTokens: budget}
		semantics.ReasoningControlExplicit = true
		semantics.ReasoningControlEffective = value
		if budget != nil {
			semantics.ReasoningBudgetExplicit = true
			semantics.ReasoningBudgetEffective, _ = strconv.ParseInt(budget.String(), 10, 64)
		}
	}
	var outputConfig *anthropicOutputConfigRequest
	if reasoning.OutputConfigObjectField != "" {
		if raw, present := envelope[reasoning.OutputConfigObjectField]; present {
			value, err := decodePublicAnthropicControl(
				raw, reasoning.EffortField, reasoning.AllowedEfforts, "reasoning_effort",
			)
			if err != nil {
				return nil, nil, PublicInvocationSemantics{}, err
			}
			outputConfig = &anthropicOutputConfigRequest{Effort: value}
			semantics.ReasoningEffortExplicit = true
			semantics.ReasoningEffortEffective = value
		}
	}
	if slices.Contains(reasoning.DisabledThinkingTypes, semantics.ReasoningControlEffective) &&
		len(reasoning.EffortsAllowedDisabled) > 0 &&
		!slices.Contains(reasoning.EffortsAllowedDisabled, semantics.ReasoningEffortEffective) {
		return nil, nil, PublicInvocationSemantics{}, protocolError(ErrInvalidRequest, "thinking_effort_incompatible")
	}
	return thinking, outputConfig, semantics, nil
}

func (profile publicAnthropicMessagesProfile) BuildUpstream(
	ctx context.Context,
	prepared *PublicPreparedRequest,
	input PublicUpstreamInput,
) (*PublicUpstreamRequest, error) {
	if err := requirePublicPrepared(profile, prepared); err != nil {
		return nil, err
	}
	var body []byte
	var err error
	switch document := prepared.document.(type) {
	case publicAnthropicRequestDocument:
		wire := document.request
		wire.Model = input.MappedModel
		body, err = json.Marshal(wire)
	case publicAnthropicToolsRequestDocument:
		wire := document.request
		wire.Model = input.MappedModel
		body, err = json.Marshal(wire)
	default:
		return nil, protocolError(ErrPublicContractMismatch, "public_prepared_request_mismatch")
	}
	if err != nil {
		return nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	buildInput := BuildInput{
		BaseURL: input.BaseURL, APIKey: input.APIKey, RequestID: input.RequestID,
		RequestedModel: prepared.requestedModel, MappedModel: input.MappedModel, Body: body,
	}
	baseURL, err := validateBuildInput(buildInput, false)
	if err != nil {
		return nil, err
	}
	descriptor := profile.base.Descriptor()
	descriptor.ProfileID = prepared.identity.ProfileID
	descriptor.ProfileVersion = prepared.identity.ProfileVersion
	httpRequest, binding, err := buildAnthropicMessagesHTTPRequest(ctx, *baseURL, buildInput, body, descriptor)
	if err != nil {
		return nil, err
	}
	binding.streaming = profile.streaming
	return &PublicUpstreamRequest{
		profile: profile, request: httpRequest,
		binding: &publicResponseBinding{
			identity: prepared.identity, base: binding,
			requestedModel: prepared.requestedModel, mappedModel: input.MappedModel,
			requestContract:     clonePublicRequestContract(prepared.requestContract),
			invocationSemantics: prepared.semantics,
		},
	}, nil
}

type publicAnthropicProviderResponse struct {
	ID           string                        `json:"id"`
	Type         string                        `json:"type"`
	Role         string                        `json:"role"`
	Model        string                        `json:"model"`
	Content      []publicAnthropicContent      `json:"content"`
	StopReason   string                        `json:"stop_reason"`
	StopSequence json.RawMessage               `json:"stop_sequence"`
	StopDetails  *publicAnthropicStopDetails   `json:"stop_details,omitempty"`
	Usage        *publicAnthropicProviderUsage `json:"usage"`
}

type publicAnthropicStopDetails struct {
	Type        string  `json:"type"`
	Category    *string `json:"category"`
	Explanation *string `json:"explanation"`
}

type publicAnthropicContent struct {
	Type      string          `json:"type"`
	Text      *string         `json:"text,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature *string         `json:"signature,omitempty"`
	Data      *string         `json:"data,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

type publicAnthropicProviderUsage struct {
	InputTokens              json.Number                        `json:"input_tokens"`
	CacheCreationInputTokens *json.Number                       `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *json.Number                       `json:"cache_read_input_tokens,omitempty"`
	OutputTokens             json.Number                        `json:"output_tokens"`
	CacheCreation            *anthropicCacheCreationDetails     `json:"cache_creation,omitempty"`
	OutputTokensDetails      *publicAnthropicOutputTokenDetails `json:"output_tokens_details,omitempty"`
	InferenceGeo             json.RawMessage                    `json:"inference_geo,omitempty"`
	ServiceTier              json.RawMessage                    `json:"service_tier,omitempty"`
}

type publicAnthropicOutputTokenDetails struct {
	ThinkingTokens json.Number `json:"thinking_tokens"`
}

var publicAnthropicExactResponseSchema = exactJSONObjectSchema{
	"id":    {},
	"type":  {},
	"role":  {},
	"model": {},
	"content": {arrayItem: exactJSONObjectSchema{
		"type": {}, "text": {}, "thinking": {}, "signature": {}, "data": {},
		"id": {}, "name": {}, "input": {},
	}},
	"stop_reason":   {},
	"stop_sequence": {},
	"stop_details": {object: exactJSONObjectSchema{
		"type": {}, "category": {}, "explanation": {},
	}},
	"usage": {object: exactJSONObjectSchema{
		"input_tokens":                {},
		"cache_creation_input_tokens": {},
		"cache_read_input_tokens":     {},
		"output_tokens":               {},
		"cache_creation": {object: exactJSONObjectSchema{
			"ephemeral_5m_input_tokens": {},
			"ephemeral_1h_input_tokens": {},
		}},
		"output_tokens_details": {object: exactJSONObjectSchema{
			"thinking_tokens": {},
		}},
	}},
}

func publicAnthropicResponseSchema(contract *PublicAnthropicMessagesContract) exactJSONObjectSchema {
	schema := make(exactJSONObjectSchema, len(publicAnthropicExactResponseSchema))
	for name, member := range publicAnthropicExactResponseSchema {
		schema[name] = member
	}
	usage := make(exactJSONObjectSchema, len(schema["usage"].object)+2)
	for name, member := range schema["usage"].object {
		usage[name] = member
	}
	if contract != nil && contract.ResponseInferenceGeo {
		usage["inference_geo"] = exactJSONMemberSchema{}
	}
	if contract != nil && contract.ResponseServiceTier {
		usage["service_tier"] = exactJSONMemberSchema{}
	}
	schema["usage"] = exactJSONMemberSchema{object: usage}
	return schema
}

type publicAnthropicDeliveryDocument struct {
	response              publicAnthropicProviderResponse
	thinkingTextIsVisible bool
	toolResponse          bool
}

func (document publicAnthropicDeliveryDocument) canonicalBody() ([]byte, error) {
	return json.Marshal(document.response)
}

func (document publicAnthropicDeliveryDocument) texts() []PublicText {
	capacity := 0
	for _, content := range document.response.Content {
		if content.Type == "text" || (content.Type == "thinking" && document.thinkingTextIsVisible) {
			capacity++
		} else if content.Type == "tool_use" {
			capacity += len(publicJSONStringLeaves(content.Input, ""))
		}
	}
	if document.response.StopDetails != nil && document.response.StopDetails.Explanation != nil {
		capacity++
	}
	result := make([]PublicText, 0, capacity)
	for index, content := range document.response.Content {
		switch {
		case content.Type == "text":
			result = append(result, PublicText{
				Path: "content[" + strconv.Itoa(index) + "].text",
				Text: *content.Text,
			})
		case content.Type == "thinking" && document.thinkingTextIsVisible:
			result = append(result, PublicText{
				Path: "content[" + strconv.Itoa(index) + "].thinking",
				Text: *content.Thinking,
			})
		case content.Type == "tool_use":
			result = append(result, publicJSONStringLeaves(
				content.Input, "content["+strconv.Itoa(index)+"].input",
			)...)
		}
	}
	if document.response.StopDetails != nil && document.response.StopDetails.Explanation != nil {
		result = append(result, PublicText{
			Path: "stop_details.explanation", Text: *document.response.StopDetails.Explanation,
		})
	}
	return result
}

func (document publicAnthropicDeliveryDocument) withTexts(replacements []string) (publicDeliveryDocument, error) {
	expected := 0
	for _, content := range document.response.Content {
		if content.Type == "text" || (content.Type == "thinking" && document.thinkingTextIsVisible) {
			expected++
		} else if content.Type == "tool_use" {
			expected += len(publicJSONStringLeaves(content.Input, ""))
		}
	}
	if document.response.StopDetails != nil && document.response.StopDetails.Explanation != nil {
		expected++
	}
	if len(replacements) != expected {
		return nil, replacementError(expected, len(replacements))
	}
	response := document.response
	response.Content = make([]publicAnthropicContent, len(document.response.Content))
	for index, content := range document.response.Content {
		response.Content[index] = content
		response.Content[index].Text = cloneStringPointer(content.Text)
		response.Content[index].Thinking = cloneStringPointer(content.Thinking)
		response.Content[index].Signature = cloneStringPointer(content.Signature)
		response.Content[index].Data = cloneStringPointer(content.Data)
		response.Content[index].Input = slices.Clone(content.Input)
	}
	replacementIndex := 0
	for index := range response.Content {
		isText := response.Content[index].Type == "text"
		isVisibleThinking := response.Content[index].Type == "thinking" && document.thinkingTextIsVisible
		if response.Content[index].Type == "tool_use" {
			for _, leaf := range publicJSONStringLeaves(response.Content[index].Input, "") {
				if replacements[replacementIndex] != leaf.Text {
					return nil, protocolError(ErrPublicTextReplacement, "structured_tool_rewrite_forbidden")
				}
				replacementIndex++
			}
			continue
		}
		if !isText && !isVisibleThinking {
			continue
		}
		if replacements[replacementIndex] == "" {
			return nil, protocolError(ErrInvalidResponse, "guardrail_text_empty")
		}
		replacement := replacements[replacementIndex]
		if isText {
			response.Content[index].Text = &replacement
		} else {
			if document.toolResponse && replacement != *response.Content[index].Thinking {
				return nil, protocolError(ErrPublicTextReplacement, "tool_thinking_rewrite_forbidden")
			}
			response.Content[index].Thinking = &replacement
		}
		replacementIndex++
	}
	if document.response.StopDetails != nil {
		details := *document.response.StopDetails
		details.Category = cloneStringPointer(document.response.StopDetails.Category)
		details.Explanation = cloneStringPointer(document.response.StopDetails.Explanation)
		if details.Explanation != nil {
			if replacements[replacementIndex] == "" {
				return nil, protocolError(ErrInvalidResponse, "refusal_explanation_empty")
			}
			replacement := replacements[replacementIndex]
			details.Explanation = &replacement
		}
		response.StopDetails = &details
	}
	return publicAnthropicDeliveryDocument{
		response: response, thinkingTextIsVisible: document.thinkingTextIsVisible,
		toolResponse: document.toolResponse,
	}, nil
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
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
	messagesContract := binding.requestContract.AnthropicMessages
	var response publicAnthropicProviderResponse
	if err := decodeStrictPublicResponse(body, &response, publicAnthropicResponseSchema(messagesContract)); err != nil {
		return PublicDelivery{}, err
	}
	var responseFields map[string]json.RawMessage
	if err := json.Unmarshal(body, &responseFields); err != nil || responseFields == nil {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "public_response_shape_invalid")
	}
	if response.ID == "" || response.Type != "message" || response.Role != "assistant" ||
		response.Model != binding.mappedModel || response.Usage == nil {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "public_response_identity_invalid")
	}
	if response.Usage.OutputTokensDetails != nil &&
		(messagesContract == nil || messagesContract.Reasoning == nil) {
		return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_output_tokens_details")
	}
	if err := validatePublicAnthropicUsageMetadata(*response.Usage, messagesContract); err != nil {
		return PublicDelivery{}, err
	}
	_, stopDetailsPresent := responseFields["stop_details"]
	if stopDetailsPresent && messagesContract == nil {
		return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_response_stop_details")
	}
	if messagesContract != nil && !slices.Contains(messagesContract.AllowedResponseStops, response.StopReason) {
		return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_response_stop_reason")
	}
	if err := validatePublicAnthropicStop(response, messagesContract); err != nil {
		return PublicDelivery{}, err
	}
	if response.StopReason != "refusal" && len(response.Content) == 0 {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "response_content_missing")
	}
	hasText := false
	hasReasoningEvidence := false
	contentPhase := "reasoning"
	toolUseCount := 0
	toolUseIDs := map[string]struct{}{}
	thinkingTextMode := ""
	if messagesContract != nil && messagesContract.Reasoning != nil {
		thinkingTextMode = messagesContract.Reasoning.ResponseThinkingTextMode
	}
	thinkingDisabled := messagesContract != nil && messagesContract.Reasoning != nil && slices.Contains(
		messagesContract.Reasoning.DisabledThinkingTypes,
		binding.invocationSemantics.ReasoningControlEffective,
	)
	for _, content := range response.Content {
		switch content.Type {
		case "text":
			if contentPhase == "tools" || content.Text == nil || *content.Text == "" ||
				content.Thinking != nil || content.Signature != nil || content.Data != nil ||
				content.ID != "" || content.Name != "" || len(content.Input) != 0 {
				return PublicDelivery{}, protocolError(ErrInvalidResponse, "public_response_content_invalid")
			}
			hasText = true
			contentPhase = "text"
		case "thinking":
			if messagesContract == nil || messagesContract.Reasoning == nil || thinkingDisabled || contentPhase != "reasoning" ||
				content.Text != nil || content.Thinking == nil || content.Signature == nil || *content.Signature == "" ||
				content.Data != nil || content.ID != "" || content.Name != "" || len(content.Input) != 0 {
				return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_public_response_content")
			}
			hasReasoningEvidence = true
			switch thinkingTextMode {
			case "omitted":
				if *content.Thinking != "" {
					return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_public_response_content")
				}
			case "summarized":
				if *content.Thinking == "" {
					return PublicDelivery{}, protocolError(ErrInvalidResponse, "response_thinking_missing")
				}
			default:
				return PublicDelivery{}, protocolError(ErrInvalidConfiguration, "public_anthropic_reasoning_contract_invalid")
			}
		case "redacted_thinking":
			if messagesContract == nil || messagesContract.Reasoning == nil || thinkingDisabled || contentPhase != "reasoning" ||
				content.Text != nil || content.Thinking != nil || content.Signature != nil ||
				content.Data == nil || *content.Data == "" || content.ID != "" || content.Name != "" || len(content.Input) != 0 {
				return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_public_response_content")
			}
			hasReasoningEvidence = true
		case "tool_use":
			if messagesContract == nil || messagesContract.FunctionTools == nil ||
				content.Text != nil || content.Thinking != nil || content.Signature != nil || content.Data != nil ||
				content.ID == "" || len(content.ID) > 256 || content.Name == "" {
				return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_public_response_content")
			}
			if _, duplicate := toolUseIDs[content.ID]; duplicate {
				return PublicDelivery{}, protocolError(ErrInvalidResponse, "tool_use_id_duplicate")
			}
			if !slices.Contains(binding.invocationSemantics.DeclaredToolNames, content.Name) ||
				binding.invocationSemantics.ToolChoiceEffective == "none" ||
				(binding.invocationSemantics.ToolChoiceEffective == "named" && content.Name != binding.invocationSemantics.NamedToolChoice) {
				return PublicDelivery{}, protocolError(ErrInvalidResponse, "tool_use_choice_mismatch")
			}
			if _, inputErr := canonicalPublicJSONObject(content.Input, "tool_use_input_invalid"); inputErr != nil {
				return PublicDelivery{}, inputErr
			}
			toolUseIDs[content.ID] = struct{}{}
			toolUseCount++
			contentPhase = "tools"
		default:
			return PublicDelivery{}, protocolError(ErrUnsupportedSemantics, "unsupported_public_response_content")
		}
	}
	if response.StopReason != "refusal" && toolUseCount == 0 &&
		(binding.invocationSemantics.ToolChoiceEffective == "required" ||
			binding.invocationSemantics.ToolChoiceEffective == "named") {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "tool_use_choice_mismatch")
	}
	if (response.StopReason == "tool_use") != (toolUseCount > 0) {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "tool_use_stop_mismatch")
	}
	if toolUseCount > 1 && !binding.invocationSemantics.ParallelToolUseEffective {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "parallel_tool_use_not_allowed")
	}
	if response.StopReason != "refusal" && response.StopReason != "tool_use" && !hasText {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "response_text_missing")
	}
	thinkingEnabled := messagesContract != nil && messagesContract.Reasoning != nil && !thinkingDisabled
	if response.StopReason != "refusal" && thinkingEnabled && thinkingTextMode == "summarized" &&
		!hasReasoningEvidence {
		return PublicDelivery{}, protocolError(ErrInvalidResponse, "response_thinking_order_invalid")
	}
	if response.Usage.OutputTokensDetails != nil {
		thinking, parseErr := parseNonnegativeInt64(
			response.Usage.OutputTokensDetails.ThinkingTokens, "thinking_tokens",
		)
		if parseErr != nil || (thinkingDisabled && thinking != 0) ||
			(response.StopReason != "refusal" && thinkingEnabled && thinkingTextMode == "summarized" && thinking == 0) {
			return PublicDelivery{}, protocolError(ErrInvalidResponse, "response_thinking_usage_invalid")
		}
	}
	usage, err := publicAnthropicUsage(*response.Usage)
	if err != nil {
		return PublicDelivery{}, err
	}
	if err := usage.validate(); err != nil {
		return PublicDelivery{}, err
	}
	providerObject := response.ID
	if response.StopReason != "refusal" && (messagesContract == nil || messagesContract.Reasoning == nil) &&
		(messagesContract == nil || messagesContract.FunctionTools == nil) {
		outcome, err := profile.base.ParseResponse(ctx, binding.base, statusCode, body)
		if err != nil {
			return PublicDelivery{}, err
		}
		providerObject = outcome.ProviderObject
	}
	response.Model = binding.requestedModel
	document := publicAnthropicDeliveryDocument{
		response: response, thinkingTextIsVisible: thinkingEnabled && thinkingTextMode == "summarized",
		toolResponse: toolUseCount > 0,
	}
	if err := validatePublicDeliveryDocument(document); err != nil {
		return PublicDelivery{}, err
	}
	return PublicDelivery{
		endpointKind:   EndpointMessages,
		providerObject: providerObject,
		usage:          usage,
		document:       document,
	}, nil
}

func validatePublicAnthropicUsageMetadata(
	usage publicAnthropicProviderUsage,
	contract *PublicAnthropicMessagesContract,
) error {
	if len(usage.InferenceGeo) > 0 {
		if contract == nil || !contract.ResponseInferenceGeo {
			return protocolError(ErrUnsupportedSemantics, "unsupported_response_inference_geo")
		}
		if !isExplicitJSONNull(usage.InferenceGeo) {
			if _, ok := decodeRequiredString(usage.InferenceGeo); !ok {
				return protocolError(ErrInvalidResponse, "response_inference_geo_invalid")
			}
		}
	}
	if len(usage.ServiceTier) > 0 {
		if contract == nil || !contract.ResponseServiceTier {
			return protocolError(ErrUnsupportedSemantics, "unsupported_response_service_tier")
		}
		if !isExplicitJSONNull(usage.ServiceTier) {
			value, ok := decodeRequiredString(usage.ServiceTier)
			if !ok || !slices.Contains([]string{"standard", "priority", "batch"}, value) {
				return protocolError(ErrInvalidResponse, "response_service_tier_invalid")
			}
		}
	}
	return nil
}

func validatePublicAnthropicStop(
	response publicAnthropicProviderResponse,
	contract *PublicAnthropicMessagesContract,
) error {
	switch response.StopReason {
	case "end_turn", "max_tokens":
		if !isExplicitJSONNull(response.StopSequence) || response.StopDetails != nil {
			return protocolError(ErrInvalidResponse, "stop_evidence_invalid")
		}
	case "stop_sequence":
		var stopSequence string
		if json.Unmarshal(response.StopSequence, &stopSequence) != nil || stopSequence == "" || response.StopDetails != nil {
			return protocolError(ErrInvalidResponse, "stop_evidence_invalid")
		}
	case "refusal":
		if contract == nil || !contract.RefusalStopDetails || !isExplicitJSONNull(response.StopSequence) ||
			response.StopDetails == nil || response.StopDetails.Type != "refusal" {
			return protocolError(ErrInvalidResponse, "refusal_stop_details_invalid")
		}
		for _, value := range []*string{response.StopDetails.Category, response.StopDetails.Explanation} {
			if value != nil && *value == "" {
				return protocolError(ErrInvalidResponse, "refusal_stop_details_invalid")
			}
		}
	case "tool_use":
		if contract == nil || contract.FunctionTools == nil || !isExplicitJSONNull(response.StopSequence) || response.StopDetails != nil {
			return protocolError(ErrInvalidResponse, "stop_evidence_invalid")
		}
	default:
		return protocolError(ErrUnsupportedSemantics, "unsupported_response_stop_reason")
	}
	return nil
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
	if raw.OutputTokensDetails != nil {
		thinking, parseErr := parseNonnegativeInt64(
			raw.OutputTokensDetails.ThinkingTokens, "thinking_tokens",
		)
		if parseErr != nil || thinking > output {
			return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "thinking_tokens_invalid")
		}
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
