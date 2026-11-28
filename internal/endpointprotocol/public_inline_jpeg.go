package endpointprotocol

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const (
	ProfileOpenAIResponsesInlineJPEG    = "unirouter.openai.responses.inline-jpeg.v1"
	ProfileAnthropicMessagesInlineJPEG  = "unirouter.anthropic.messages.inline-jpeg.v1"
	PublicResponsesInlineJPEGExecutorV1 = "unirouter.endpoint-profile-responses-inline-jpeg-executor.v1"
	PublicMessagesInlineJPEGExecutorV1  = "unirouter.endpoint-profile-messages-inline-jpeg-executor.v1"
)

const inlineJPEGDataURLPrefix = "data:image/jpeg;base64,"

func publicResponsesInlineJPEGProfile() publicOpenAIResponsesProfile {
	identity := responsesToolsIdentity()
	identity.ProfileID = ProfileOpenAIResponsesInlineJPEG
	identity.ExecutorContractVersion = PublicResponsesInlineJPEGExecutorV1
	return publicOpenAIResponsesProfile{base: openAIResponsesToolsProfileV3{}, identity: identity}
}

func publicMessagesInlineJPEGProfile() publicAnthropicMessagesProfile {
	identity := anthropicMessagesV3PublicIdentity()
	identity.ProfileID = ProfileAnthropicMessagesInlineJPEG
	identity.ExecutorContractVersion = PublicMessagesInlineJPEGExecutorV1
	return publicAnthropicMessagesProfile{base: anthropicMessagesProfile{}, identity: identity}
}

// Repository conformance runs the same native request and response codecs as
// the public executor, while keeping qualification and deployment separate.
type nativeInlineJPEGRepositoryProfile struct {
	codec PublicNonStreamProfile
}

func (p nativeInlineJPEGRepositoryProfile) Descriptor() Descriptor {
	public := p.codec.Descriptor()
	d := (openAIResponsesToolsProfileV3{}).Descriptor()
	if public.EndpointKind == EndpointMessages {
		d = (anthropicMessagesProfileV3{}).Descriptor()
	}
	d.ProfileID = public.Identity.ProfileID
	d.SupportedModalities = []string{"text", "image/jpeg"}
	d.UnsupportedSemantics = []string{"files", "inline_jpeg_and_tools", "multiple_images", "remote_images", "streaming"}
	return d
}

func (p nativeInlineJPEGRepositoryProfile) BuildRequest(ctx context.Context, input BuildInput) (*http.Request, *Binding, error) {
	contract := PublicRequestContract{
		ExecutionProfileID: "endpointprotocol.native-inline-jpeg-fixture", ExecutionProfileVersion: "1.0.0",
	}
	if p.Descriptor().EndpointKind == EndpointResponses {
		contract.Reasoning = &PublicReasoningRequestContract{RequestObjectField: "reasoning", EffortField: "effort", AllowedEfforts: []string{"low", "medium"}}
	} else {
		contract = anthropicMessagesV3FixtureContract()
		contract.AnthropicMessages.Reasoning = &PublicAnthropicReasoningRequestContract{
			ThinkingObjectField: "thinking", ThinkingTypeField: "type", AllowedThinkingTypes: []string{"adaptive"},
			ThinkingProviderDefault: "adaptive", ResponseThinkingTextMode: "omitted",
		}
	}
	prepared, err := p.codec.PrepareRequest(input.Body, input.RequestedModel, contract)
	if err != nil {
		return nil, nil, err
	}
	upstream, err := p.codec.BuildUpstream(ctx, prepared, PublicUpstreamInput{
		BaseURL: input.BaseURL, APIKey: input.APIKey, RequestID: input.RequestID, MappedModel: input.MappedModel,
	})
	if err != nil {
		return nil, nil, err
	}
	binding := upstream.binding.base
	binding.profileID = p.Descriptor().ProfileID
	binding.publicContract = &upstream.binding.requestContract
	binding.publicSemantics = upstream.binding.invocationSemantics
	return upstream.request, binding, nil
}

func (p nativeInlineJPEGRepositoryProfile) ParseResponse(ctx context.Context, b *Binding, status int, body []byte) (Outcome, error) {
	if err := requireBinding(b, p.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if b.publicContract == nil {
		return Outcome{}, protocolError(ErrPublicContractMismatch, "public_inline_jpeg_response_binding_invalid")
	}
	delivery, err := p.codec.parseDelivery(ctx, &publicResponseBinding{
		identity: p.codec.Descriptor().Identity, base: b, requestedModel: b.requestedModel, mappedModel: b.mappedModel,
		requestContract: *b.publicContract, invocationSemantics: b.publicSemantics,
	}, status, body)
	if err != nil {
		return Outcome{}, err
	}
	u := delivery.CustomerUsage()
	input := u.InputTokens
	if p.Descriptor().EndpointKind == EndpointResponses {
		input -= u.CacheReadTokens + u.CacheWriteTokens
	}
	return Outcome{ContractVersion: ContractVersion, ProfileID: p.Descriptor().ProfileID, ProviderState: "completed",
		ProviderObject: delivery.ProviderObject(), MappedModel: b.mappedModel, OutputCount: 1,
		Units: []UnitObservation{
			observedUnit(UnitInputTokens, strconv.FormatInt(input, 10)),
			observedUnit(UnitCacheReadTokens, strconv.FormatInt(u.CacheReadTokens, 10)),
			observedUnit(UnitCacheWriteTokens, strconv.FormatInt(u.CacheWriteTokens, 10)),
			observedUnit(UnitOutputTokens, strconv.FormatInt(u.OutputTokens, 10)),
		},
	}, nil
}

func (p nativeInlineJPEGRepositoryProfile) ClassifyHTTPStatus(status int) (string, bool) {
	return p.codec.ClassifyHTTPStatus(status)
}

func prepareNativeRequestWithOptionalJPEG(
	core PublicNonStreamProfile, identity PublicContractIdentity, endpoint EndpointKind,
	envelope map[string]json.RawMessage, model string, contract PublicRequestContract,
) (*PublicPreparedRequest, error) {
	if endpoint == EndpointResponses && contract.Reasoning == nil ||
		endpoint == EndpointMessages && (contract.AnthropicMessages == nil || contract.AnthropicMessages.Reasoning == nil) {
		return nil, protocolError(ErrInvalidConfiguration, "native_inline_jpeg_reasoning_contract_missing")
	}
	key, imagePart := "messages", "image"
	if endpoint == EndpointResponses {
		key, imagePart = "input", "input_image"
	}
	if !nativeRequestContainsImage(envelope[key], imagePart) {
		body, err := json.Marshal(envelope)
		if err != nil {
			return nil, err
		}
		prepared, err := core.PrepareRequest(body, model, contract)
		if err != nil {
			return nil, err
		}
		prepared.identity = identity
		return prepared, nil
	}
	for _, key := range []string{"tools", "tool_choice", "parallel_tool_calls", "include"} {
		if _, exists := envelope[key]; exists {
			return nil, protocolError(ErrUnsupportedSemantics, "tools_with_inline_jpeg_unsupported")
		}
	}
	image, err := parsePublicInlineJPEGInput(envelope[key], endpoint)
	if err != nil {
		return nil, err
	}
	if endpoint == EndpointResponses {
		envelope[key], err = json.Marshal(image.text)
	} else {
		envelope[key], err = json.Marshal([]anthropicMessageRequest{{Role: "user", Content: image.text}})
	}
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	prepared, err := core.PrepareRequest(body, model, contract)
	if err != nil {
		return nil, err
	}
	doc := publicInlineJPEGDocument{core: prepared.document, input: image}
	if err := validatePublicRequestDocument(doc); err != nil {
		return nil, err
	}
	prepared.identity = identity
	prepared.document = doc
	prepared.semantics.InlineImagePixels = image.pixels
	return prepared, nil
}

func nativeRequestContainsImage(raw json.RawMessage, imagePart string) bool {
	var messages []struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &messages) != nil {
		return false
	}
	for _, message := range messages {
		var parts []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(message.Content, &parts) != nil {
			continue
		}
		for _, part := range parts {
			if part.Type == imagePart {
				return true
			}
		}
	}
	return false
}

// A native vision invocation contains exactly one user message, one image,
// and one text part. Preserve their order. Only the text is available to the
// guardrail; it cannot replace, reencode, or echo the image.
type publicInlineJPEGInput struct {
	endpoint  EndpointKind
	text      string
	image     json.RawMessage
	textIndex int
	pixels    int64
}

func parsePublicInlineJPEGInput(raw json.RawMessage, endpoint EndpointKind) (publicInlineJPEGInput, error) {
	invalid := protocolError(ErrInvalidRequest, "invalid_inline_jpeg_message")
	var messages []map[string]json.RawMessage
	if json.Unmarshal(raw, &messages) != nil || len(messages) != 1 {
		return publicInlineJPEGInput{}, invalid
	}
	message := messages[0]
	if len(message) != 2 || mustJSONString(message["role"]) != "user" {
		return publicInlineJPEGInput{}, invalid
	}
	var parts []json.RawMessage
	if json.Unmarshal(message["content"], &parts) != nil || len(parts) != 2 {
		return publicInlineJPEGInput{}, invalid
	}
	imageType, textType := "image", "text"
	if endpoint == EndpointResponses {
		imageType, textType = "input_image", "input_text"
	}
	input := publicInlineJPEGInput{endpoint: endpoint}
	var payload string
	for index, raw := range parts {
		var part map[string]json.RawMessage
		if json.Unmarshal(raw, &part) != nil || len(part) != 2 {
			return publicInlineJPEGInput{}, invalid
		}
		switch mustJSONString(part["type"]) {
		case textType:
			var ok bool
			if input.text != "" {
				return publicInlineJPEGInput{}, invalid
			}
			input.text, ok = decodeRequiredString(part["text"])
			if !ok {
				return publicInlineJPEGInput{}, invalid
			}
			input.textIndex = index
		case imageType:
			if input.image != nil {
				return publicInlineJPEGInput{}, invalid
			}
			if endpoint == EndpointResponses {
				url, ok := decodeJSONString(part["image_url"])
				if !ok || !strings.HasPrefix(url, inlineJPEGDataURLPrefix) {
					return publicInlineJPEGInput{}, invalid
				}
				payload = strings.TrimPrefix(url, inlineJPEGDataURLPrefix)
			} else {
				var source map[string]json.RawMessage
				if json.Unmarshal(part["source"], &source) != nil || len(source) != 3 ||
					mustJSONString(source["type"]) != "base64" || mustJSONString(source["media_type"]) != "image/jpeg" {
					return publicInlineJPEGInput{}, invalid
				}
				payload = mustJSONString(source["data"])
			}
			input.image = append(json.RawMessage(nil), raw...)
		default:
			return publicInlineJPEGInput{}, invalid
		}
	}
	if input.text == "" || input.image == nil || payload == "" || strings.ContainsAny(payload, "\r\n") {
		return publicInlineJPEGInput{}, invalid
	}
	pixels, err := CheckBoundedInlineJPEG(payload, InlineJPEGBounds{
		MaximumBytes: 256 * 1024, MinimumWidth: 32, MinimumHeight: 32,
		MinimumPixels: 1024, MaximumPixels: 131_072, MaximumAspectRatio: 8,
	})
	if err != nil {
		return publicInlineJPEGInput{}, protocolError(ErrInvalidRequest, "invalid_inline_jpeg_data")
	}
	input.pixels = pixels
	return input, nil
}

func (input publicInlineJPEGInput) inputTextPath() string {
	prefix := "messages"
	if input.endpoint == EndpointResponses {
		prefix = "input"
	}
	if input.textIndex == 0 {
		return prefix + "[0].content[0].text"
	}
	return prefix + "[0].content[1].text"
}

func (input publicInlineJPEGInput) withImage(body []byte, mappedModel string) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	key, textType := "messages", "text"
	if input.endpoint == EndpointResponses {
		key, textType = "input", "input_text"
		input.text = mustJSONString(envelope["input"])
	} else {
		var messages []anthropicMessageRequest
		if err := json.Unmarshal(envelope["messages"], &messages); err != nil || len(messages) != 1 {
			return nil, protocolError(ErrPublicContractMismatch, "public_inline_jpeg_text_missing")
		}
		input.text = messages[0].Content
	}
	if input.text == "" {
		return nil, protocolError(ErrPublicContractMismatch, "public_inline_jpeg_text_missing")
	}
	textPart, err := json.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{textType, input.text})
	if err != nil {
		return nil, err
	}
	parts := [2]json.RawMessage{}
	parts[input.textIndex], parts[1-input.textIndex] = textPart, input.image
	envelope[key], err = json.Marshal([]struct {
		Role    string             `json:"role"`
		Content [2]json.RawMessage `json:"content"`
	}{{Role: "user", Content: parts}})
	if err != nil {
		return nil, err
	}
	if mappedModel != "" {
		envelope["model"], err = json.Marshal(mappedModel)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(envelope)
}

type publicInlineJPEGDocument struct {
	core  publicRequestDocument
	input publicInlineJPEGInput
}

func (d publicInlineJPEGDocument) canonicalBody() ([]byte, error) {
	body, err := d.core.canonicalBody()
	if err != nil {
		return nil, err
	}
	return d.input.withImage(body, "")
}

func (d publicInlineJPEGDocument) texts() []PublicText {
	texts := d.core.texts()
	for index := range texts {
		if texts[index].Path == "input" || texts[index].Path == "messages[0].content" {
			texts[index].Path = d.input.inputTextPath()
		}
	}
	return texts
}

func (d publicInlineJPEGDocument) withTexts(replacements []string) (publicRequestDocument, error) {
	core, err := d.core.withTexts(replacements)
	if err != nil {
		return nil, err
	}
	d.core = core
	return d, nil
}
