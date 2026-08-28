package endpointprotocol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const PublicAnthropicMessagesStreamMaxEventBytes = 16 << 20

type publicAnthropicMessagesStreamBlock struct {
	index        int64
	kind         string
	providerText strings.Builder
	customerText strings.Builder
	signature    bool
}

// PublicAnthropicMessagesStream is the single request-bound authority for the
// strict Messages text/reasoning-evidence SSE subset. It never exposes provider
// model identity or unguarded text.
type PublicAnthropicMessagesStream struct {
	profile      publicAnthropicMessagesProfile
	binding      *publicResponseBinding
	messageID    string
	stage        int
	nextBlock    int64
	active       *publicAnthropicMessagesStreamBlock
	hasText      bool
	hasReasoning bool
	textPhase    bool
	startUsage   map[string]json.RawMessage
	finalUsage   PublicCustomerUsage
	terminal     bool
	pending      []*PublicAnthropicMessagesStreamEvent
}

// PublicAnthropicMessagesStreamEvent is a sanitized customer-facing SSE event.
// Text deltas remain unrenderable until the output guard commits exactly one
// replacement.
type PublicAnthropicMessagesStreamEvent struct {
	stream    *PublicAnthropicMessagesStream
	block     *publicAnthropicMessagesStreamBlock
	event     string
	envelope  map[string]json.RawMessage
	texts     []PublicText
	usage     PublicCustomerUsage
	terminal  bool
	success   bool
	committed bool
}

func (event *PublicAnthropicMessagesStreamEvent) Event() string {
	if event == nil {
		return ""
	}
	return event.event
}

func (event *PublicAnthropicMessagesStreamEvent) Texts() []PublicText {
	if event == nil {
		return nil
	}
	return clonePublicTexts(event.texts)
}

func (event *PublicAnthropicMessagesStreamEvent) CustomerUsage() PublicCustomerUsage {
	if event == nil {
		return PublicCustomerUsage{}
	}
	return event.usage
}

func (event *PublicAnthropicMessagesStreamEvent) Terminal() bool {
	return event != nil && event.terminal
}
func (event *PublicAnthropicMessagesStreamEvent) Success() bool { return event != nil && event.success }

func (event *PublicAnthropicMessagesStreamEvent) Body() []byte {
	if event == nil || (len(event.texts) > 0 && !event.committed) ||
		(len(event.texts) == 0 && event.stream != nil && len(event.stream.pending) > 0) {
		return nil
	}
	body, err := json.Marshal(event.envelope)
	if err != nil {
		return nil
	}
	return body
}

func (event *PublicAnthropicMessagesStreamEvent) CommitTexts(replacements []string) error {
	if event == nil || event.stream == nil || event.block == nil || event.event != "content_block_delta" ||
		event.committed || len(event.stream.pending) == 0 || event.stream.pending[0] != event ||
		len(event.texts) != 1 || len(replacements) != 1 {
		return protocolError(ErrPublicTextReplacement, "stream_text_commit_invalid")
	}
	if replacements[0] == "" {
		return protocolError(ErrPublicTextReplacement, "guardrail_text_empty")
	}
	if event.block.kind == "thinking" && replacements[0] != event.texts[0].Text {
		return protocolError(ErrPublicTextReplacement, "signed_thinking_rewrite_forbidden")
	}
	var delta map[string]json.RawMessage
	if json.Unmarshal(event.envelope["delta"], &delta) != nil || delta == nil {
		return protocolError(ErrPublicTextReplacement, "stream_text_delta_invalid")
	}
	field := "text"
	if event.block.kind == "thinking" {
		field = "thinking"
	}
	delta[field] = mustRawJSON(replacements[0])
	event.envelope["delta"] = mustRawJSON(delta)
	event.block.customerText.WriteString(replacements[0])
	event.committed = true
	event.stream.pending = event.stream.pending[1:]
	return nil
}

func (upstream *PublicUpstreamRequest) NewAnthropicMessagesStream() (*PublicAnthropicMessagesStream, error) {
	if upstream == nil || upstream.binding == nil || upstream.profile == nil || upstream.binding.base == nil ||
		!upstream.binding.base.streaming || !upstream.profile.Descriptor().Streaming {
		return nil, protocolError(ErrPublicContractMismatch, "public_stream_binding_mismatch")
	}
	profile, ok := upstream.profile.(publicAnthropicMessagesProfile)
	if !ok || !slices.Contains([]string{ProfileAnthropicMessagesTextV2, ProfileAnthropicMessagesTextV3}, profile.identity.ProfileID) || !profile.streaming {
		return nil, protocolError(ErrPublicContractMismatch, "public_stream_profile_mismatch")
	}
	if err := requirePublicBinding(profile, upstream.binding); err != nil {
		return nil, err
	}
	return &PublicAnthropicMessagesStream{profile: profile, binding: upstream.binding}, nil
}

func (stream *PublicAnthropicMessagesStream) ParseEvent(eventName string, data []byte) (*PublicAnthropicMessagesStreamEvent, error) {
	if stream == nil || stream.binding == nil || stream.terminal ||
		(len(stream.pending) > 0 && eventName != "content_block_delta") ||
		eventName == "" || eventName != strings.TrimSpace(eventName) || len(data) == 0 ||
		len(data) > PublicAnthropicMessagesStreamMaxEventBytes {
		return nil, protocolError(ErrInvalidResponse, "stream_event_state_invalid")
	}
	schema, err := stream.eventSchema(eventName)
	if err != nil {
		return nil, err
	}
	if err := validateExactJSONObjectMembers(
		data, PublicAnthropicMessagesStreamMaxEventBytes, ErrInvalidResponse, "stream_event", schema,
	); err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil || envelope == nil {
		return nil, protocolError(ErrInvalidResponse, "stream_event_invalid")
	}
	wireType, ok := decodeRequiredString(envelope["type"])
	if !ok || wireType != eventName {
		return nil, protocolError(ErrInvalidResponse, "stream_event_type_mismatch")
	}
	event := &PublicAnthropicMessagesStreamEvent{
		stream: stream, event: eventName, envelope: envelope, committed: true,
	}
	switch eventName {
	case "ping":
		// Anthropic documents ping as transport-only and allows it at any point.
	case "message_start":
		err = stream.acceptMessageStart(event)
	case "content_block_start":
		err = stream.acceptContentBlockStart(event)
	case "content_block_delta":
		err = stream.acceptContentBlockDelta(event)
	case "content_block_stop":
		err = stream.acceptContentBlockStop(event)
	case "message_delta":
		err = stream.acceptMessageDelta(event)
	case "message_stop":
		err = stream.acceptMessageStop(event)
	case "error":
		err = stream.acceptError(event)
	}
	if err != nil {
		return nil, err
	}
	if len(event.texts) > 0 {
		event.committed = false
		stream.pending = append(stream.pending, event)
	}
	return event, nil
}

// PublicAnthropicMessagesStreamingRequested is only a strict transport
// discriminator. The selected exact profile reparses the complete body inside
// the immutable Plan snapshot.
func PublicAnthropicMessagesStreamingRequested(body []byte) (bool, error) {
	if err := validateStrictJSON(body, maxRequestBytes, ErrInvalidRequest); err != nil {
		return false, err
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil || envelope == nil {
		return false, protocolError(ErrInvalidRequest, "request_must_be_object")
	}
	return exactAnthropicMessagesStreamValue(envelope)
}

func (stream *PublicAnthropicMessagesStream) eventSchema(eventName string) (exactJSONObjectSchema, error) {
	usage := publicAnthropicStreamUsageSchema(stream.binding.requestContract.AnthropicMessages)
	switch eventName {
	case "message_start":
		return exactJSONObjectSchema{
			"type": {},
			"message": {object: exactJSONObjectSchema{
				"id": {}, "type": {}, "role": {}, "model": {}, "content": {},
				"stop_reason": {}, "stop_sequence": {}, "usage": {object: usage},
			}},
		}, nil
	case "content_block_start":
		return exactJSONObjectSchema{
			"type": {}, "index": {},
			"content_block": {object: exactJSONObjectSchema{
				"type": {}, "text": {}, "thinking": {}, "signature": {}, "data": {},
			}},
		}, nil
	case "content_block_delta":
		return exactJSONObjectSchema{
			"type": {}, "index": {},
			"delta": {object: exactJSONObjectSchema{
				"type": {}, "text": {}, "thinking": {}, "signature": {},
			}},
		}, nil
	case "content_block_stop":
		return exactJSONObjectSchema{"type": {}, "index": {}}, nil
	case "message_delta":
		return exactJSONObjectSchema{
			"type":  {},
			"delta": {object: exactJSONObjectSchema{"stop_reason": {}, "stop_sequence": {}}},
			"usage": {object: usage},
		}, nil
	case "message_stop", "ping":
		return exactJSONObjectSchema{"type": {}}, nil
	case "error":
		return exactJSONObjectSchema{
			"type": {}, "error": {object: exactJSONObjectSchema{"type": {}, "message": {}}},
		}, nil
	default:
		return nil, protocolError(ErrUnsupportedSemantics, "unsupported_stream_event")
	}
}

func publicAnthropicStreamUsageSchema(contract *PublicAnthropicMessagesContract) exactJSONObjectSchema {
	return publicAnthropicResponseSchema(contract)["usage"].object
}

func (stream *PublicAnthropicMessagesStream) acceptMessageStart(event *PublicAnthropicMessagesStreamEvent) error {
	if stream.stage != 0 {
		return protocolError(ErrInvalidResponse, "stream_event_order_invalid")
	}
	var message map[string]json.RawMessage
	if json.Unmarshal(event.envelope["message"], &message) != nil || message == nil {
		return protocolError(ErrInvalidResponse, "stream_message_start_invalid")
	}
	id, idOK := decodeRequiredString(message["id"])
	typeName, typeOK := decodeRequiredString(message["type"])
	role, roleOK := decodeRequiredString(message["role"])
	model, modelOK := decodeRequiredString(message["model"])
	if !idOK || !typeOK || typeName != "message" || !roleOK || role != "assistant" ||
		!modelOK || model != stream.binding.mappedModel || !isEmptyJSONArray(message["content"]) ||
		!isExplicitJSONNull(message["stop_reason"]) || !isExplicitJSONNull(message["stop_sequence"]) {
		return protocolError(ErrInvalidResponse, "stream_message_start_invalid")
	}
	if rawUsage, present := message["usage"]; present && !isExplicitJSONNull(rawUsage) {
		members, err := parsePublicAnthropicStreamUsageMembers(rawUsage, stream.binding.requestContract.AnthropicMessages)
		if err != nil {
			return err
		}
		stream.startUsage = members
	}
	message["model"] = mustRawJSON(stream.binding.requestedModel)
	event.envelope["message"] = mustRawJSON(message)
	stream.messageID = id
	stream.stage = 1
	return nil
}

func (stream *PublicAnthropicMessagesStream) acceptContentBlockStart(event *PublicAnthropicMessagesStreamEvent) error {
	if stream.stage != 1 || stream.active != nil {
		return protocolError(ErrInvalidResponse, "stream_content_order_invalid")
	}
	index, err := requiredEventInt(event.envelope["index"])
	if err != nil || index != stream.nextBlock {
		return protocolError(ErrInvalidResponse, "stream_content_index_invalid")
	}
	var block map[string]json.RawMessage
	if json.Unmarshal(event.envelope["content_block"], &block) != nil || block == nil {
		return protocolError(ErrInvalidResponse, "stream_content_block_invalid")
	}
	kind, ok := decodeRequiredString(block["type"])
	if !ok {
		return protocolError(ErrInvalidResponse, "stream_content_block_invalid")
	}
	state := &publicAnthropicMessagesStreamBlock{index: index, kind: kind}
	switch kind {
	case "text":
		text, textOK := decodeJSONString(block["text"])
		if !textOK || text != "" || hasAnyMember(block, "thinking", "signature", "data") {
			return protocolError(ErrUnsupportedSemantics, "unsupported_stream_content_block")
		}
		if stream.reasoningBlockRequired() && !stream.hasReasoning {
			return protocolError(ErrInvalidResponse, "stream_required_reasoning_missing")
		}
		stream.textPhase = true
	case "thinking":
		if !stream.reasoningEnabled() || stream.textPhase {
			return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_block")
		}
		mode := stream.binding.requestContract.AnthropicMessages.Reasoning.ResponseThinkingTextMode
		if mode != "omitted" && mode != "summarized" {
			return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_block")
		}
		thinking, thinkingOK := decodeJSONString(block["thinking"])
		signature, signatureOK := decodeJSONString(block["signature"])
		if !thinkingOK || thinking != "" || !signatureOK || signature != "" || hasAnyMember(block, "text", "data") {
			return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_block")
		}
		stream.hasReasoning = true
	case "redacted_thinking":
		if !stream.reasoningEnabled() || stream.textPhase {
			return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_block")
		}
		data, dataOK := decodeJSONString(block["data"])
		if !dataOK || data == "" || hasAnyMember(block, "text", "thinking", "signature") {
			return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_block")
		}
		stream.hasReasoning = true
	default:
		return protocolError(ErrUnsupportedSemantics, "unsupported_stream_content_block")
	}
	stream.active = state
	return nil
}

func (stream *PublicAnthropicMessagesStream) acceptContentBlockDelta(event *PublicAnthropicMessagesStreamEvent) error {
	state, err := stream.activeBlock(event.envelope)
	if err != nil {
		return err
	}
	var delta map[string]json.RawMessage
	if json.Unmarshal(event.envelope["delta"], &delta) != nil || delta == nil {
		return protocolError(ErrInvalidResponse, "stream_content_delta_invalid")
	}
	kind, ok := decodeRequiredString(delta["type"])
	if !ok {
		return protocolError(ErrInvalidResponse, "stream_content_delta_invalid")
	}
	switch state.kind {
	case "text":
		text, textOK := decodeJSONString(delta["text"])
		if kind != "text_delta" || !textOK || text == "" || hasAnyMember(delta, "thinking", "signature") {
			return protocolError(ErrUnsupportedSemantics, "unsupported_stream_content_delta")
		}
		state.providerText.WriteString(text)
		event.block = state
		event.texts = []PublicText{{
			Path: "content[" + strconv.FormatInt(state.index, 10) + "].text", Text: text,
		}}
	case "thinking":
		mode := stream.binding.requestContract.AnthropicMessages.Reasoning.ResponseThinkingTextMode
		switch kind {
		case "thinking_delta":
			thinking, thinkingOK := decodeJSONString(delta["thinking"])
			if mode != "summarized" || !thinkingOK || thinking == "" || state.signature ||
				hasAnyMember(delta, "text", "signature") {
				return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_delta")
			}
			state.providerText.WriteString(thinking)
			event.block = state
			event.texts = []PublicText{{
				Path: "content[" + strconv.FormatInt(state.index, 10) + "].thinking", Text: thinking,
			}}
		case "signature_delta":
			_, signatureOK := decodeRequiredString(delta["signature"])
			if !signatureOK || state.signature || hasAnyMember(delta, "text", "thinking") ||
				(mode == "summarized" && state.providerText.Len() == 0) {
				return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_delta")
			}
			state.signature = true
		default:
			return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_delta")
		}
	case "redacted_thinking":
		return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_delta")
	default:
		return protocolError(ErrInvalidResponse, "stream_content_block_invalid")
	}
	return nil
}

func (stream *PublicAnthropicMessagesStream) acceptContentBlockStop(event *PublicAnthropicMessagesStreamEvent) error {
	state, err := stream.activeBlock(event.envelope)
	if err != nil {
		return err
	}
	mode := ""
	if state.kind == "thinking" {
		mode = stream.binding.requestContract.AnthropicMessages.Reasoning.ResponseThinkingTextMode
	}
	if (state.kind == "text" && state.providerText.Len() == 0) ||
		(state.kind == "thinking" && (!state.signature || (mode == "summarized" && state.providerText.Len() == 0))) {
		return protocolError(ErrInvalidResponse, "stream_content_block_incomplete")
	}
	if state.kind == "text" {
		stream.hasText = true
	}
	stream.active = nil
	stream.nextBlock++
	return nil
}

func (stream *PublicAnthropicMessagesStream) acceptMessageDelta(event *PublicAnthropicMessagesStreamEvent) error {
	if stream.stage != 1 || stream.active != nil || !stream.hasText {
		return protocolError(ErrInvalidResponse, "stream_message_delta_order_invalid")
	}
	var delta map[string]json.RawMessage
	if json.Unmarshal(event.envelope["delta"], &delta) != nil || delta == nil {
		return protocolError(ErrInvalidResponse, "stream_message_delta_invalid")
	}
	stopReason, ok := decodeRequiredString(delta["stop_reason"])
	contract := stream.binding.requestContract.AnthropicMessages
	if !ok || stopReason == "refusal" || contract == nil || !slices.Contains(contract.AllowedResponseStops, stopReason) {
		return protocolError(ErrUnsupportedSemantics, "unsupported_response_stop_reason")
	}
	switch stopReason {
	case "end_turn", "max_tokens":
		if !isExplicitJSONNull(delta["stop_sequence"]) {
			return protocolError(ErrInvalidResponse, "stop_evidence_invalid")
		}
	case "stop_sequence":
		if value, ok := decodeRequiredString(delta["stop_sequence"]); !ok || value == "" {
			return protocolError(ErrInvalidResponse, "stop_evidence_invalid")
		}
	default:
		return protocolError(ErrUnsupportedSemantics, "unsupported_response_stop_reason")
	}
	finalMembers, err := parsePublicAnthropicStreamUsageMembers(
		event.envelope["usage"], stream.binding.requestContract.AnthropicMessages,
	)
	if err != nil {
		return err
	}
	usage, err := mergePublicAnthropicStreamUsage(
		stream.startUsage, finalMembers, stream.binding.requestContract.AnthropicMessages,
		stream.reasoningDisabled(), stream.hasReasoning,
	)
	if err != nil {
		return err
	}
	stream.finalUsage = usage
	stream.stage = 2
	return nil
}

func (stream *PublicAnthropicMessagesStream) acceptMessageStop(event *PublicAnthropicMessagesStreamEvent) error {
	if stream.stage != 2 || stream.active != nil || stream.messageID == "" || !stream.hasText {
		return protocolError(ErrInvalidResponse, "stream_message_stop_order_invalid")
	}
	event.usage = stream.finalUsage
	event.terminal = true
	event.success = true
	stream.terminal = true
	stream.stage = 3
	return nil
}

func (stream *PublicAnthropicMessagesStream) acceptError(event *PublicAnthropicMessagesStreamEvent) error {
	var providerError map[string]json.RawMessage
	if json.Unmarshal(event.envelope["error"], &providerError) != nil || providerError == nil {
		return protocolError(ErrInvalidResponse, "stream_error_invalid")
	}
	if _, ok := decodeRequiredString(providerError["type"]); !ok {
		return protocolError(ErrInvalidResponse, "stream_error_invalid")
	}
	if _, ok := decodeRequiredString(providerError["message"]); !ok {
		return protocolError(ErrInvalidResponse, "stream_error_invalid")
	}
	event.envelope = map[string]json.RawMessage{
		"type":  mustRawJSON("error"),
		"error": mustRawJSON(map[string]string{"type": "upstream_error", "message": "upstream stream failed"}),
	}
	event.terminal = true
	stream.terminal = true
	stream.stage = 3
	return nil
}

func (stream *PublicAnthropicMessagesStream) activeBlock(envelope map[string]json.RawMessage) (*publicAnthropicMessagesStreamBlock, error) {
	if stream.stage != 1 || stream.active == nil {
		return nil, protocolError(ErrInvalidResponse, "stream_content_order_invalid")
	}
	index, err := requiredEventInt(envelope["index"])
	if err != nil || index != stream.active.index {
		return nil, protocolError(ErrInvalidResponse, "stream_content_index_invalid")
	}
	return stream.active, nil
}

func (stream *PublicAnthropicMessagesStream) reasoningDisabled() bool {
	contract := stream.binding.requestContract.AnthropicMessages
	return contract != nil && contract.Reasoning != nil && slices.Contains(
		contract.Reasoning.DisabledThinkingTypes,
		stream.binding.invocationSemantics.ReasoningControlEffective,
	)
}

func (stream *PublicAnthropicMessagesStream) reasoningEnabled() bool {
	contract := stream.binding.requestContract.AnthropicMessages
	return contract != nil && contract.Reasoning != nil && !stream.reasoningDisabled()
}

func (stream *PublicAnthropicMessagesStream) reasoningBlockRequired() bool {
	contract := stream.binding.requestContract.AnthropicMessages
	return contract != nil && contract.Reasoning != nil && slices.Contains(
		contract.Reasoning.BudgetRequiredTypes,
		stream.binding.invocationSemantics.ReasoningControlEffective,
	)
}

func hasAnyMember(object map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		if _, present := object[name]; present {
			return true
		}
	}
	return false
}

func parsePublicAnthropicStreamUsageMembers(
	raw json.RawMessage,
	contract *PublicAnthropicMessagesContract,
) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || isExplicitJSONNull(raw) {
		return nil, protocolError(ErrInvalidResponse, "stream_usage_missing")
	}
	if err := validateExactJSONObjectMembers(
		raw, PublicAnthropicMessagesStreamMaxEventBytes, ErrInvalidResponse,
		"stream_usage", publicAnthropicStreamUsageSchema(contract),
	); err != nil {
		return nil, err
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil || members == nil {
		return nil, protocolError(ErrInvalidResponse, "stream_usage_invalid")
	}
	return members, nil
}

func mergePublicAnthropicStreamUsage(
	start, final map[string]json.RawMessage,
	contract *PublicAnthropicMessagesContract,
	thinkingDisabled bool,
	hasReasoning bool,
) (PublicCustomerUsage, error) {
	if final == nil {
		return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "stream_usage_missing")
	}
	merged := make(map[string]json.RawMessage, len(start)+len(final))
	for name, raw := range final {
		merged[name] = append(json.RawMessage(nil), raw...)
	}
	for _, field := range []string{
		"input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens",
		"cache_creation", "inference_geo", "service_tier",
	} {
		startRaw, startPresent := start[field]
		finalRaw, finalPresent := final[field]
		switch {
		case startPresent && finalPresent && !publicJSONValuesEqual(startRaw, finalRaw):
			return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "stream_usage_conflict")
		case startPresent && !finalPresent:
			merged[field] = append(json.RawMessage(nil), startRaw...)
		}
	}
	finalOutput, finalOutputPresent := final["output_tokens"]
	if !finalOutputPresent {
		return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "stream_output_usage_missing")
	}
	finalOutputCount, err := parsePublicUsageInteger(finalOutput, "output_tokens")
	if err != nil {
		return PublicCustomerUsage{}, err
	}
	if startOutput, present := start["output_tokens"]; present {
		startOutputCount, err := parsePublicUsageInteger(startOutput, "output_tokens")
		if err != nil || finalOutputCount < startOutputCount {
			return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "stream_output_usage_conflict")
		}
	}
	if startDetails, present := start["output_tokens_details"]; present {
		if finalDetails, finalPresent := final["output_tokens_details"]; finalPresent {
			startThinking, err := parsePublicThinkingTokens(startDetails)
			if err != nil {
				return PublicCustomerUsage{}, err
			}
			finalThinking, err := parsePublicThinkingTokens(finalDetails)
			if err != nil || finalThinking < startThinking {
				return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "stream_thinking_usage_conflict")
			}
		} else {
			merged["output_tokens_details"] = append(json.RawMessage(nil), startDetails...)
		}
	}
	if _, present := merged["input_tokens"]; !present {
		return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "stream_input_usage_missing")
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "stream_usage_invalid")
	}
	var provider publicAnthropicProviderUsage
	if json.Unmarshal(raw, &provider) != nil {
		return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "stream_usage_invalid")
	}
	if err := validatePublicAnthropicUsageMetadata(provider, contract); err != nil {
		return PublicCustomerUsage{}, err
	}
	if provider.OutputTokensDetails != nil {
		if contract == nil || contract.Reasoning == nil {
			return PublicCustomerUsage{}, protocolError(ErrUnsupportedSemantics, "unsupported_output_tokens_details")
		}
		thinking, err := parseNonnegativeInt64(provider.OutputTokensDetails.ThinkingTokens, "thinking_tokens")
		if err != nil || (thinkingDisabled && thinking != 0) || (!thinkingDisabled && thinking > 0 && !hasReasoning) ||
			(!thinkingDisabled && hasReasoning && thinking == 0) {
			return PublicCustomerUsage{}, protocolError(ErrInvalidResponse, "response_thinking_usage_invalid")
		}
	}
	usage, err := publicAnthropicUsage(provider)
	if err != nil {
		return PublicCustomerUsage{}, err
	}
	if err := usage.validate(); err != nil {
		return PublicCustomerUsage{}, err
	}
	return usage, nil
}

func parsePublicUsageInteger(raw json.RawMessage, field string) (int64, error) {
	number, ok := decodeJSONNumber(raw)
	if !ok {
		return 0, protocolError(ErrInvalidResponse, "stream_"+field+"_invalid")
	}
	return parseNonnegativeInt64(number, field)
}

func parsePublicThinkingTokens(raw json.RawMessage) (int64, error) {
	var details publicAnthropicOutputTokenDetails
	if json.Unmarshal(raw, &details) != nil {
		return 0, protocolError(ErrInvalidResponse, "stream_thinking_usage_invalid")
	}
	return parseNonnegativeInt64(details.ThinkingTokens, "thinking_tokens")
}

func publicJSONValuesEqual(left, right json.RawMessage) bool {
	decode := func(raw json.RawMessage) (any, bool) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil || requireDecoderEOF(decoder) != nil {
			return nil, false
		}
		return value, true
	}
	leftValue, leftOK := decode(left)
	rightValue, rightOK := decode(right)
	return leftOK && rightOK && reflect.DeepEqual(leftValue, rightValue)
}
