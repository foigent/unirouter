package endpointprotocol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const PublicResponsesStreamMaxEventBytes = 16 << 20

var publicResponsesStreamResponseEventSchema = exactJSONObjectSchema{
	"type": {}, "response": {object: publicOpenAIResponsesExactResponseSchema}, "sequence_number": {},
}

var publicResponsesStreamItemEventSchema = exactJSONObjectSchema{
	"type": {}, "output_index": {}, "sequence_number": {},
	"item": {object: exactJSONObjectSchema{
		"id": {}, "type": {}, "status": {}, "role": {}, "phase": {}, "content": {},
		"summary": {}, "encrypted_content": {},
	}},
}

var publicResponsesStreamMessageItemSchema = exactJSONObjectSchema{
	"id": {}, "type": {}, "status": {}, "role": {}, "phase": {}, "content": {},
}

var publicResponsesStreamReasoningItemSchema = exactJSONObjectSchema{
	"id": {}, "type": {}, "status": {}, "summary": {}, "content": {}, "encrypted_content": {},
}

var publicResponsesStreamPartEventSchema = exactJSONObjectSchema{
	"type": {}, "item_id": {}, "output_index": {}, "content_index": {}, "sequence_number": {},
	"part": {object: exactJSONObjectSchema{
		"type": {}, "text": {}, "annotations": {}, "logprobs": {},
	}},
}

var publicResponsesStreamDeltaEventSchema = exactJSONObjectSchema{
	"type": {}, "item_id": {}, "output_index": {}, "content_index": {},
	"delta": {}, "logprobs": {}, "sequence_number": {},
}

var publicResponsesStreamTextDoneEventSchema = exactJSONObjectSchema{
	"type": {}, "item_id": {}, "output_index": {}, "content_index": {},
	"text": {}, "logprobs": {}, "sequence_number": {},
}

type publicResponsesStreamItemState struct {
	id           string
	kind         string
	partAdded    bool
	textDone     bool
	partDone     bool
	itemDone     bool
	providerText strings.Builder
	customerText strings.Builder
}

// PublicResponsesStream is a request-bound, single-use event parser. It owns
// the only provider/customer text reconciliation state for one exact public
// Responses attempt; callers cannot construct it without the private binding
// minted by BuildUpstream.
type PublicResponsesStream struct {
	profile      publicOpenAIResponsesProfile
	binding      *publicResponseBinding
	responseID   string
	hasSequence  bool
	lastSequence int64
	stage        int
	items        map[int]*publicResponsesStreamItemState
	pending      []*PublicResponsesStreamEvent
	terminal     bool
	messageIndex int
}

// PublicResponsesStreamEvent is a sanitized customer event. A text delta is
// intentionally unrenderable until CommitTexts applies the output-guard result
// and advances the single customer-text authority in its owning stream.
type PublicResponsesStreamEvent struct {
	stream    *PublicResponsesStream
	event     string
	envelope  map[string]json.RawMessage
	texts     []PublicText
	usage     PublicCustomerUsage
	terminal  bool
	success   bool
	committed bool
}

func (event *PublicResponsesStreamEvent) Event() string {
	if event == nil {
		return ""
	}
	return event.event
}

func (event *PublicResponsesStreamEvent) Texts() []PublicText {
	if event == nil {
		return nil
	}
	return clonePublicTexts(event.texts)
}

func (event *PublicResponsesStreamEvent) CustomerUsage() PublicCustomerUsage {
	if event == nil {
		return PublicCustomerUsage{}
	}
	return event.usage
}

func (event *PublicResponsesStreamEvent) Terminal() bool { return event != nil && event.terminal }
func (event *PublicResponsesStreamEvent) Success() bool  { return event != nil && event.success }

func (event *PublicResponsesStreamEvent) Body() []byte {
	if event == nil || (len(event.texts) > 0 && !event.committed) {
		return nil
	}
	body, err := json.Marshal(event.envelope)
	if err != nil {
		return nil
	}
	return body
}

// CommitTexts applies exactly one positional output-guard decision. It is the
// only mutation that advances customer-visible accumulated text.
func (event *PublicResponsesStreamEvent) CommitTexts(replacements []string) error {
	if event == nil || event.stream == nil || event.event != "response.output_text.delta" ||
		event.committed || len(event.stream.pending) == 0 || event.stream.pending[0] != event ||
		len(event.texts) != 1 || len(replacements) != 1 {
		return protocolError(ErrPublicTextReplacement, "stream_text_commit_invalid")
	}
	raw, err := json.Marshal(replacements[0])
	if err != nil {
		return protocolError(ErrPublicTextReplacement, "stream_text_encoding_failed")
	}
	var outputIndex int64
	if err := decodeEventInt(event.envelope["output_index"], &outputIndex); err != nil {
		return err
	}
	item := event.stream.items[int(outputIndex)]
	if item == nil || item.kind != "message" {
		return protocolError(ErrPublicTextReplacement, "stream_text_item_missing")
	}
	event.envelope["delta"] = raw
	item.customerText.WriteString(replacements[0])
	event.committed = true
	event.stream.pending = event.stream.pending[1:]
	return nil
}

func (upstream *PublicUpstreamRequest) NewResponsesStream() (*PublicResponsesStream, error) {
	if upstream == nil || upstream.binding == nil || upstream.profile == nil || upstream.binding.base == nil ||
		!upstream.binding.base.streaming || !upstream.profile.Descriptor().Streaming {
		return nil, protocolError(ErrPublicContractMismatch, "public_stream_binding_mismatch")
	}
	profile, ok := upstream.profile.(publicOpenAIResponsesProfile)
	if !ok || profile.identity.ProfileID != ProfileOpenAIResponsesTextV2 || !profile.streaming {
		return nil, protocolError(ErrPublicContractMismatch, "public_stream_profile_mismatch")
	}
	if err := requirePublicBinding(profile, upstream.binding); err != nil {
		return nil, err
	}
	return &PublicResponsesStream{
		profile: profile, binding: upstream.binding, items: make(map[int]*publicResponsesStreamItemState),
		messageIndex: -1,
	}, nil
}

func (stream *PublicResponsesStream) ParseEvent(eventName string, data []byte) (*PublicResponsesStreamEvent, error) {
	if stream == nil || stream.binding == nil || stream.terminal ||
		(len(stream.pending) > 0 && eventName != "response.output_text.delta") ||
		eventName == "" || eventName != strings.TrimSpace(eventName) || len(data) == 0 ||
		len(data) > PublicResponsesStreamMaxEventBytes {
		return nil, protocolError(ErrInvalidResponse, "stream_event_state_invalid")
	}
	var schema exactJSONObjectSchema
	switch eventName {
	case "response.created", "response.in_progress", "response.completed", "response.failed", "response.incomplete":
		schema = publicResponsesStreamResponseEventSchema
	case "response.output_item.added", "response.output_item.done":
		schema = publicResponsesStreamItemEventSchema
	case "response.content_part.added", "response.content_part.done":
		schema = publicResponsesStreamPartEventSchema
	case "response.output_text.delta":
		schema = publicResponsesStreamDeltaEventSchema
	case "response.output_text.done":
		schema = publicResponsesStreamTextDoneEventSchema
	default:
		return nil, protocolError(ErrUnsupportedSemantics, "unsupported_stream_event")
	}
	if err := validateExactJSONObjectMembers(data, PublicResponsesStreamMaxEventBytes, ErrInvalidResponse, "stream_event", schema); err != nil {
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
	sequence, err := requiredEventInt(envelope["sequence_number"])
	if err != nil || (!stream.hasSequence && (eventName != "response.created" || sequence != 0)) ||
		(stream.hasSequence && sequence != stream.lastSequence+1) {
		return nil, protocolError(ErrInvalidResponse, "stream_sequence_invalid")
	}
	stream.hasSequence = true
	stream.lastSequence = sequence

	event := &PublicResponsesStreamEvent{stream: stream, event: eventName, envelope: envelope, committed: true}
	switch eventName {
	case "response.created":
		err = stream.acceptProgressResponse(event, "in_progress", 0)
		if err == nil {
			stream.stage = 1
		}
	case "response.in_progress":
		if stream.stage != 1 {
			err = protocolError(ErrInvalidResponse, "stream_event_order_invalid")
		} else {
			err = stream.acceptProgressResponse(event, "in_progress", 1)
			stream.stage = 2
		}
	case "response.output_item.added":
		err = stream.acceptItem(event, false)
	case "response.content_part.added":
		err = stream.acceptPart(event, false)
	case "response.output_text.delta":
		err = stream.acceptDelta(event)
	case "response.output_text.done":
		err = stream.acceptTextDone(event)
	case "response.content_part.done":
		err = stream.acceptPart(event, true)
	case "response.output_item.done":
		err = stream.acceptItem(event, true)
	case "response.completed":
		err = stream.acceptCompleted(event)
	case "response.failed", "response.incomplete":
		err = stream.acceptFailure(event)
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

// PublicResponsesStreamingRequested is a transport-only discriminator used at
// the HTTP entry before the exact profile is selected. It does not validate or
// normalize capabilities; the selected public profile must parse the complete
// body again inside the Planner snapshot.
func PublicResponsesStreamingRequested(body []byte) (bool, error) {
	envelope, err := parseOpenAIResponsesEnvelope(body)
	if err != nil {
		return false, err
	}
	return exactResponsesStreamValue(envelope)
}

func (stream *PublicResponsesStream) acceptProgressResponse(event *PublicResponsesStreamEvent, status string, requiredStage int) error {
	if stream.stage != requiredStage {
		return protocolError(ErrInvalidResponse, "stream_event_order_invalid")
	}
	raw := event.envelope["response"]
	var responseMembers map[string]json.RawMessage
	var wire publicOpenAIResponsesWireResponse
	if json.Unmarshal(raw, &responseMembers) != nil || responseMembers == nil || json.Unmarshal(raw, &wire) != nil ||
		wire.ID == "" || wire.Object != "response" || wire.Status != status ||
		wire.Model != stream.binding.mappedModel || !isExplicitJSONNull(wire.Error) ||
		!isExplicitJSONNull(wire.IncompleteDetails) || len(wire.Output) != 0 ||
		!isExplicitJSONNull(responseMembers["usage"]) {
		return protocolError(ErrInvalidResponse, "stream_response_identity_invalid")
	}
	if stream.responseID == "" {
		stream.responseID = wire.ID
	} else if stream.responseID != wire.ID {
		return protocolError(ErrInvalidResponse, "stream_response_identity_invalid")
	}
	if err := validatePublicOpenAIResponsesCoreMetadata(wire); err != nil {
		return err
	}
	return rewriteResponseModel(event.envelope, stream.binding.requestedModel)
}

func (stream *PublicResponsesStream) acceptItem(event *PublicResponsesStreamEvent, done bool) error {
	if stream.stage != 2 {
		return protocolError(ErrInvalidResponse, "stream_event_order_invalid")
	}
	index64, err := requiredEventInt(event.envelope["output_index"])
	if err != nil || index64 < 0 || index64 > 1024 {
		return protocolError(ErrInvalidResponse, "stream_output_index_invalid")
	}
	index := int(index64)
	var item map[string]json.RawMessage
	if json.Unmarshal(event.envelope["item"], &item) != nil || item == nil {
		return protocolError(ErrInvalidResponse, "stream_item_invalid")
	}
	id, idOK := decodeRequiredString(item["id"])
	kind, kindOK := decodeRequiredString(item["type"])
	status, statusOK := decodeRequiredString(item["status"])
	if !idOK || !kindOK || !statusOK {
		return protocolError(ErrInvalidResponse, "stream_item_invalid")
	}
	var itemSchema exactJSONObjectSchema
	switch kind {
	case "message":
		itemSchema = publicResponsesStreamMessageItemSchema
	case "reasoning":
		itemSchema = publicResponsesStreamReasoningItemSchema
	default:
		return protocolError(ErrUnsupportedSemantics, "unsupported_stream_output_item")
	}
	if err := validateExactJSONObjectMembers(
		event.envelope["item"], PublicResponsesStreamMaxEventBytes,
		ErrInvalidResponse, "stream_"+kind+"_item", itemSchema,
	); err != nil {
		return err
	}
	if kind == "message" {
		if err := validateStreamMessagePhase(item["phase"]); err != nil {
			return err
		}
	}
	if !done {
		if index != len(stream.items) || stream.items[index] != nil || status != "in_progress" {
			return protocolError(ErrInvalidResponse, "stream_item_order_invalid")
		}
		state := &publicResponsesStreamItemState{id: id, kind: kind}
		switch kind {
		case "message":
			role, ok := decodeRequiredString(item["role"])
			if !ok || role != "assistant" || !isEmptyJSONArray(item["content"]) || stream.messageIndex >= 0 {
				return protocolError(ErrUnsupportedSemantics, "unsupported_stream_message_item")
			}
			stream.messageIndex = index
		case "reasoning":
			if stream.binding.requestContract.Reasoning == nil || !emptyOrNullJSONArray(item["summary"]) ||
				!emptyOrNullJSONArray(item["content"]) {
				return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_item")
			}
			if raw := item["encrypted_content"]; len(raw) > 0 && !isExplicitJSONNull(raw) {
				return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_item")
			}
		}
		stream.items[index] = state
		return nil
	}
	state := stream.items[index]
	if state == nil || state.itemDone || state.id != id || state.kind != kind || status != "completed" {
		return protocolError(ErrInvalidResponse, "stream_item_done_invalid")
	}
	if kind == "reasoning" {
		if !emptyOrNullJSONArray(item["summary"]) || !emptyOrNullJSONArray(item["content"]) ||
			(len(item["encrypted_content"]) > 0 && !isExplicitJSONNull(item["encrypted_content"])) {
			return protocolError(ErrUnsupportedSemantics, "unsupported_stream_reasoning_item")
		}
		state.itemDone = true
		return nil
	}
	if !state.partDone {
		return protocolError(ErrInvalidResponse, "stream_item_done_invalid")
	}
	providerText, err := exactMessageText(item)
	if err != nil || providerText != state.providerText.String() {
		return protocolError(ErrInvalidResponse, "stream_item_text_mismatch")
	}
	item["content"] = mustRawJSON([]map[string]any{{
		"type": "output_text", "text": state.customerText.String(), "annotations": []any{},
	}})
	event.envelope["item"] = mustRawJSON(item)
	state.itemDone = true
	return nil
}

func validateStreamMessagePhase(raw json.RawMessage) error {
	if len(raw) == 0 || isExplicitJSONNull(raw) {
		return nil
	}
	phase, ok := decodeRequiredString(raw)
	if !ok || (phase != "commentary" && phase != "final_answer") {
		return protocolError(ErrUnsupportedSemantics, "unsupported_stream_message_phase")
	}
	return nil
}

func (stream *PublicResponsesStream) acceptPart(event *PublicResponsesStreamEvent, done bool) error {
	state, err := stream.eventMessageState(event.envelope)
	if err != nil {
		return err
	}
	var part map[string]json.RawMessage
	if json.Unmarshal(event.envelope["part"], &part) != nil || part == nil {
		return protocolError(ErrInvalidResponse, "stream_part_invalid")
	}
	typeName, typeOK := decodeRequiredString(part["type"])
	text, textOK := decodeJSONString(part["text"])
	if !typeOK || typeName != "output_text" || !textOK || !isEmptyJSONArray(part["annotations"]) ||
		!emptyOrMissingJSONArray(part["logprobs"]) {
		return protocolError(ErrUnsupportedSemantics, "unsupported_stream_content_part")
	}
	if !done {
		if state.partAdded || text != "" {
			return protocolError(ErrInvalidResponse, "stream_part_order_invalid")
		}
		state.partAdded = true
		return nil
	}
	if !state.textDone || state.partDone || text != state.providerText.String() {
		return protocolError(ErrInvalidResponse, "stream_part_text_mismatch")
	}
	part["text"] = mustRawJSON(state.customerText.String())
	event.envelope["part"] = mustRawJSON(part)
	state.partDone = true
	return nil
}

func (stream *PublicResponsesStream) acceptDelta(event *PublicResponsesStreamEvent) error {
	state, err := stream.eventMessageState(event.envelope)
	if err != nil {
		return err
	}
	delta, ok := decodeJSONString(event.envelope["delta"])
	if !ok || delta == "" || !state.partAdded || state.textDone || !emptyOrMissingJSONArray(event.envelope["logprobs"]) {
		return protocolError(ErrInvalidResponse, "stream_text_delta_invalid")
	}
	state.providerText.WriteString(delta)
	path := "output[" + strconv.Itoa(stream.messageIndex) + "].content[0].text"
	event.texts = []PublicText{{Path: path, Text: delta}}
	return nil
}

func (stream *PublicResponsesStream) acceptTextDone(event *PublicResponsesStreamEvent) error {
	state, err := stream.eventMessageState(event.envelope)
	if err != nil {
		return err
	}
	text, ok := decodeJSONString(event.envelope["text"])
	if !ok || !state.partAdded || state.textDone || text != state.providerText.String() ||
		!emptyOrMissingJSONArray(event.envelope["logprobs"]) {
		return protocolError(ErrInvalidResponse, "stream_text_done_invalid")
	}
	event.envelope["text"] = mustRawJSON(state.customerText.String())
	state.textDone = true
	return nil
}

func (stream *PublicResponsesStream) acceptCompleted(event *PublicResponsesStreamEvent) error {
	if stream.stage != 2 || stream.messageIndex < 0 || stream.items[stream.messageIndex] == nil ||
		!stream.items[stream.messageIndex].itemDone {
		return protocolError(ErrInvalidResponse, "stream_completed_order_invalid")
	}
	for _, item := range stream.items {
		if item == nil || !item.itemDone {
			return protocolError(ErrInvalidResponse, "stream_completed_order_invalid")
		}
	}
	raw := event.envelope["response"]
	finalProfile := stream.profile
	finalProfile.streaming = false
	finalBinding := *stream.binding
	finalBase := *stream.binding.base
	finalBase.streaming = false
	finalBinding.base = &finalBase
	delivery, err := finalProfile.parseDelivery(context.Background(), &finalBinding, http.StatusOK, raw)
	if err != nil {
		return err
	}
	texts := delivery.Texts()
	message := stream.items[stream.messageIndex]
	if len(texts) != 1 || texts[0].Text != message.providerText.String() {
		return protocolError(ErrInvalidResponse, "stream_completed_text_mismatch")
	}
	delivery, err = delivery.WithTexts([]string{message.customerText.String()})
	if err != nil {
		return err
	}
	event.envelope["response"] = append(json.RawMessage(nil), delivery.Body()...)
	event.usage = delivery.CustomerUsage()
	event.terminal = true
	event.success = true
	stream.terminal = true
	stream.stage = 3
	return nil
}

func (stream *PublicResponsesStream) acceptFailure(event *PublicResponsesStreamEvent) error {
	if stream.stage < 1 {
		return protocolError(ErrInvalidResponse, "stream_failure_order_invalid")
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(event.envelope["response"], &response) != nil || response == nil {
		return protocolError(ErrInvalidResponse, "stream_failure_invalid")
	}
	id, idOK := decodeRequiredString(response["id"])
	model, modelOK := decodeRequiredString(response["model"])
	status, statusOK := decodeRequiredString(response["status"])
	if !idOK || !modelOK || !statusOK || id != stream.responseID || model != stream.binding.mappedModel ||
		((event.event == "response.failed" && status != "failed") ||
			(event.event == "response.incomplete" && status != "incomplete")) {
		return protocolError(ErrInvalidResponse, "stream_failure_invalid")
	}
	// Failure details and partial output are provider diagnostics, not a public
	// text delivery contract. Emit only a sanitized terminal identity marker.
	event.envelope["response"] = mustRawJSON(map[string]any{
		"id": id, "object": "response", "status": status, "model": stream.binding.requestedModel,
		"error": nil, "incomplete_details": nil, "output": []any{}, "usage": nil,
	})
	event.terminal = true
	stream.terminal = true
	stream.stage = 3
	return nil
}

func (stream *PublicResponsesStream) eventMessageState(envelope map[string]json.RawMessage) (*publicResponsesStreamItemState, error) {
	outputIndex, err := requiredEventInt(envelope["output_index"])
	if err != nil || int(outputIndex) != stream.messageIndex {
		return nil, protocolError(ErrInvalidResponse, "stream_message_index_invalid")
	}
	contentIndex, err := requiredEventInt(envelope["content_index"])
	if err != nil || contentIndex != 0 {
		return nil, protocolError(ErrUnsupportedSemantics, "unsupported_stream_content_index")
	}
	itemID, ok := decodeRequiredString(envelope["item_id"])
	state := stream.items[int(outputIndex)]
	if !ok || state == nil || state.kind != "message" || state.id != itemID || state.itemDone {
		return nil, protocolError(ErrInvalidResponse, "stream_message_identity_invalid")
	}
	return state, nil
}

func rewriteResponseModel(envelope map[string]json.RawMessage, requestedModel string) error {
	var response map[string]json.RawMessage
	if json.Unmarshal(envelope["response"], &response) != nil || response == nil {
		return protocolError(ErrInvalidResponse, "stream_response_invalid")
	}
	response["model"] = mustRawJSON(requestedModel)
	envelope["response"] = mustRawJSON(response)
	return nil
}

func exactMessageText(item map[string]json.RawMessage) (string, error) {
	role, roleOK := decodeRequiredString(item["role"])
	if !roleOK || role != "assistant" {
		return "", protocolError(ErrInvalidResponse, "stream_message_invalid")
	}
	var content []map[string]json.RawMessage
	if json.Unmarshal(item["content"], &content) != nil || len(content) != 1 {
		return "", protocolError(ErrUnsupportedSemantics, "unsupported_stream_message_content")
	}
	typeName, typeOK := decodeRequiredString(content[0]["type"])
	text, textOK := decodeJSONString(content[0]["text"])
	if !typeOK || typeName != "output_text" || !textOK || !isEmptyJSONArray(content[0]["annotations"]) ||
		!emptyOrMissingJSONArray(content[0]["logprobs"]) {
		return "", protocolError(ErrUnsupportedSemantics, "unsupported_stream_message_content")
	}
	return text, nil
}

func requiredEventInt(raw json.RawMessage) (int64, error) {
	var value int64
	if err := decodeEventInt(raw, &value); err != nil || value < 0 {
		return 0, protocolError(ErrInvalidResponse, "stream_event_integer_invalid")
	}
	return value, nil
}

func decodeEventInt(raw json.RawMessage, value *int64) error {
	number, ok := decodeJSONNumber(raw)
	if !ok {
		return fmt.Errorf("invalid integer")
	}
	parsed, err := parseRequestInt64(number)
	if err != nil {
		return err
	}
	*value = parsed
	return nil
}

func emptyOrMissingJSONArray(raw json.RawMessage) bool {
	return len(raw) == 0 || isEmptyJSONArray(raw)
}

func emptyOrNullJSONArray(raw json.RawMessage) bool {
	return len(raw) == 0 || isExplicitJSONNull(raw) || isEmptyJSONArray(raw)
}

func mustRawJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
