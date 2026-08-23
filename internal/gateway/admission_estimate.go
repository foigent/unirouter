package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/tokenprofile"
)

// These versions are evidence identities, not tokenizer names. The contract is
// deliberately provider-neutral: one UTF-8 byte is reserved as at most one
// token, plus bounded protocol framing. That is conservative for byte-backed
// tokenizers and, unlike the historical chars/4 heuristic, cannot make a large
// tool schema or provider extension free at admission.
const (
	chatAdmissionEstimateVersion        = "chat-json-byte-upper.v1"
	embeddingsAdmissionEstimateVersion  = "embeddings-input-byte-upper.v1"
	providerAdmissionEstimateVersion    = "provider-json-byte-upper.v1"
	kimiK26ChatEstimateVersion          = "chat-json-kimi-k2.6-framed-upper.v1"
	kimiK26ProviderEstimateVersion      = "provider-json-kimi-k2.6-framed-upper.v1"
	miniMaxM27ChatEstimateVersion       = "chat-json-minimax-m2.7-framed-upper.v1"
	miniMaxM27ProviderEstimateVersion   = "provider-json-minimax-m2.7-framed-upper.v1"
	chatAdmissionBaseOverhead           = int64(64)
	chatAdmissionMessageOverhead        = int64(32)
	chatAdmissionCollectionOverhead     = int64(16)
	verifiedTokenizerBaseOverhead       = int64(4_096)
	verifiedTokenizerMessageOverhead    = int64(64)
	verifiedTokenizerCollectionOverhead = int64(256)
	maxEmbeddingTokenID                 = int64(math.MaxInt32)
)

var (
	errAdmissionUnsupportedShape = errors.New("admission request shape is not safely measurable")
	errAdmissionEstimateOverflow = errors.New("admission estimate overflow")
)

type requestAdmissionEstimate struct {
	InputTokens int
	Version     string
}

func estimateChatPreProfileAdmission(body []byte) (requestAdmissionEstimate, error) {
	validation, err := validateAdvancedChatProtocolRequest(body, modelprofile.ChatProtocolContract{
		ProfileID:   "protocol.preprofile.chat.v1",
		PartialMode: true, DynamicSystemTools: true,
		BuiltinWebSearch: true, BuiltinWebSearchName: "$web_search",
		BuiltinSearchMeter:             "builtin_web_search_calls",
		BuiltinSearchRequiresNonStream: true, BuiltinSearchRequiresSerial: true,
		MaxBuiltinSearchCalls: 1,
		InlineImageDataURL:    true, InlineVideoDataURL: true,
	})
	if err != nil {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: %v", errAdmissionUnsupportedShape, err)
	}
	if _, err := estimateChatAdmission(validation.SanitizedBody); err != nil {
		return requestAdmissionEstimate{}, err
	}
	messages, collections, err := chatEnvelopeCardinality(body)
	if err != nil {
		return requestAdmissionEstimate{}, err
	}
	return estimateChatByteUpper(body, messages, collections)
}

// estimateChatAdmissionForPlan keeps syntax/shape validation provider-neutral,
// then applies the exact tokenizer identity frozen into the planner snapshot.
// The tokenizer count covers the retained JSON envelope; explicit framing
// overhead remains conservative because provider-side chat serialization is
// not observable at admission time. This is an upper bound, not a claim that
// provider-reported prompt_tokens will equal the estimate.
func estimateChatAdmissionForPlan(body []byte, plan modelrouting.Plan) (requestAdmissionEstimate, error) {
	byteUpper := requestAdmissionEstimate{}
	protocolContract, advanced := modelprofile.ResolveChatProtocolContract(plan.ModelProtocolProfile)
	if advanced {
		validation, err := validateAdvancedChatProtocolRequest(body, protocolContract)
		if err != nil {
			return requestAdmissionEstimate{}, fmt.Errorf("%w: %v", errAdmissionUnsupportedShape, err)
		}
		if _, err := estimateChatAdmission(validation.SanitizedBody); err != nil {
			return requestAdmissionEstimate{}, err
		}
		messages, collections, err := chatEnvelopeCardinality(body)
		if err != nil {
			return requestAdmissionEstimate{}, err
		}
		byteUpper, err = estimateChatByteUpper(body, messages, collections)
		if err != nil {
			return requestAdmissionEstimate{}, err
		}
	} else {
		var err error
		byteUpper, err = estimateChatAdmission(body)
		if err != nil {
			return requestAdmissionEstimate{}, err
		}
	}
	switch plan.TokenizerProfileID + "\x00" + plan.TokenizerProfileVersion {
	case modelprofile.TokenizerByteUpperID + "\x00" + modelprofile.TokenizerByteUpperVersion:
		return byteUpper, nil
	case modelprofile.TokenizerKimiK26ID + "\x00" + modelprofile.TokenizerKimiK26Version:
		messages, collections, err := chatEnvelopeCardinality(body)
		if err != nil {
			return requestAdmissionEstimate{}, err
		}
		return estimateVerifiedTokenizerJSON(
			body, plan.TokenizerProfileID, plan.TokenizerProfileVersion,
			messages, collections, kimiK26ChatEstimateVersion,
		)
	case modelprofile.TokenizerMiniMaxM27ID + "\x00" + modelprofile.TokenizerMiniMaxM27Version:
		messages, collections, err := chatEnvelopeCardinality(body)
		if err != nil {
			return requestAdmissionEstimate{}, err
		}
		return estimateVerifiedTokenizerJSON(
			body, plan.TokenizerProfileID, plan.TokenizerProfileVersion,
			messages, collections, miniMaxM27ChatEstimateVersion,
		)
	default:
		return requestAdmissionEstimate{}, fmt.Errorf("%w: unsupported tokenizer profile %s@%s", errAdmissionUnsupportedShape, plan.TokenizerProfileID, plan.TokenizerProfileVersion)
	}
}

func estimateProviderJSONAdmissionForPlan(body []byte, plan modelrouting.Plan) (requestAdmissionEstimate, error) {
	switch plan.TokenizerProfileID + "\x00" + plan.TokenizerProfileVersion {
	case modelprofile.TokenizerByteUpperID + "\x00" + modelprofile.TokenizerByteUpperVersion:
		return estimateProviderJSONAdmission(body)
	case modelprofile.TokenizerKimiK26ID + "\x00" + modelprofile.TokenizerKimiK26Version:
		if !json.Valid(body) {
			return requestAdmissionEstimate{}, fmt.Errorf("%w: provider request body must be JSON", errAdmissionUnsupportedShape)
		}
		messages, collections, err := chatEnvelopeCardinality(body)
		if err != nil {
			return requestAdmissionEstimate{}, err
		}
		return estimateVerifiedTokenizerJSON(
			body, plan.TokenizerProfileID, plan.TokenizerProfileVersion,
			messages, collections, kimiK26ProviderEstimateVersion,
		)
	case modelprofile.TokenizerMiniMaxM27ID + "\x00" + modelprofile.TokenizerMiniMaxM27Version:
		if !json.Valid(body) {
			return requestAdmissionEstimate{}, fmt.Errorf("%w: provider request body must be JSON", errAdmissionUnsupportedShape)
		}
		messages, collections, err := chatEnvelopeCardinality(body)
		if err != nil {
			return requestAdmissionEstimate{}, err
		}
		return estimateVerifiedTokenizerJSON(
			body, plan.TokenizerProfileID, plan.TokenizerProfileVersion,
			messages, collections, miniMaxM27ProviderEstimateVersion,
		)
	default:
		return requestAdmissionEstimate{}, fmt.Errorf("%w: unsupported tokenizer profile %s@%s", errAdmissionUnsupportedShape, plan.TokenizerProfileID, plan.TokenizerProfileVersion)
	}
}

func estimateVerifiedTokenizerJSON(body []byte, profileID, profileVersion string, messages, collections int, version string) (requestAdmissionEstimate, error) {
	count, err := tokenprofile.Count(profileID, profileVersion, body)
	if err != nil {
		return requestAdmissionEstimate{}, fmt.Errorf("tokenizer profile %s@%s: %w", profileID, profileVersion, err)
	}
	messageOverhead, ok := checkedAdmissionMultiply(int64(messages), verifiedTokenizerMessageOverhead)
	if !ok {
		return requestAdmissionEstimate{}, errAdmissionEstimateOverflow
	}
	collectionOverhead, ok := checkedAdmissionMultiply(int64(collections), verifiedTokenizerCollectionOverhead)
	if !ok {
		return requestAdmissionEstimate{}, errAdmissionEstimateOverflow
	}
	overhead, ok := checkedAdmissionAdd(verifiedTokenizerBaseOverhead, messageOverhead)
	if !ok {
		return requestAdmissionEstimate{}, errAdmissionEstimateOverflow
	}
	overhead, ok = checkedAdmissionAdd(overhead, collectionOverhead)
	if !ok {
		return requestAdmissionEstimate{}, errAdmissionEstimateOverflow
	}
	tokens, err := checkedAdmissionInt(int64(count), overhead)
	if err != nil {
		return requestAdmissionEstimate{}, err
	}
	return requestAdmissionEstimate{InputTokens: tokens, Version: version}, nil
}

func chatEnvelopeCardinality(body []byte) (messages, collections int, err error) {
	var envelope map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(body, &envelope); unmarshalErr != nil || envelope == nil {
		return 0, 0, fmt.Errorf("%w: chat request must be an object", errAdmissionUnsupportedShape)
	}
	if raw, ok := envelope["messages"]; ok && !isJSONNull(raw) {
		var values []json.RawMessage
		if unmarshalErr := json.Unmarshal(raw, &values); unmarshalErr != nil {
			return 0, 0, fmt.Errorf("%w: messages must be an array", errAdmissionUnsupportedShape)
		}
		messages = len(values)
		for _, value := range values {
			var message map[string]json.RawMessage
			if unmarshalErr := json.Unmarshal(value, &message); unmarshalErr != nil || message == nil {
				return 0, 0, fmt.Errorf("%w: chat message must be an object", errAdmissionUnsupportedShape)
			}
			if rawTools, exists := message["tools"]; exists && !isJSONNull(rawTools) {
				var dynamicTools []json.RawMessage
				if unmarshalErr := json.Unmarshal(rawTools, &dynamicTools); unmarshalErr != nil {
					return 0, 0, fmt.Errorf("%w: message tools must be an array", errAdmissionUnsupportedShape)
				}
				if collections > math.MaxInt-len(dynamicTools) {
					return 0, 0, errAdmissionEstimateOverflow
				}
				collections += len(dynamicTools)
			}
		}
	}
	for _, field := range []string{"tools", "functions"} {
		if raw, ok := envelope[field]; ok && !isJSONNull(raw) {
			var values []json.RawMessage
			if unmarshalErr := json.Unmarshal(raw, &values); unmarshalErr != nil {
				return 0, 0, fmt.Errorf("%w: %s must be an array", errAdmissionUnsupportedShape, field)
			}
			if collections > math.MaxInt-len(values) {
				return 0, 0, errAdmissionEstimateOverflow
			}
			collections += len(values)
		}
	}
	return messages, collections, nil
}

func estimateProviderJSONAdmission(body []byte) (requestAdmissionEstimate, error) {
	if !json.Valid(body) {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: provider request body must be JSON", errAdmissionUnsupportedShape)
	}
	tokens, err := checkedAdmissionInt(int64(len(body)), chatAdmissionBaseOverhead)
	if err != nil {
		return requestAdmissionEstimate{}, err
	}
	return requestAdmissionEstimate{InputTokens: tokens, Version: providerAdmissionEstimateVersion}, nil
}

func estimateProviderEmbeddingsAdmission(body []byte) (requestAdmissionEstimate, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: provider embeddings request must be an object", errAdmissionUnsupportedShape)
	}
	if err := validateMeasurableEmbeddingsRoot(envelope, false); err != nil {
		return requestAdmissionEstimate{}, err
	}
	raw, exists := envelope["input"]
	if !exists || isJSONNull(raw) {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: provider embeddings input is required", errAdmissionUnsupportedShape)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: provider embeddings input is invalid", errAdmissionUnsupportedShape)
	}
	tokens, err := measurableEmbeddingInput(value)
	if err != nil {
		return requestAdmissionEstimate{}, err
	}
	return requestAdmissionEstimate{InputTokens: tokens, Version: embeddingsAdmissionEstimateVersion}, nil
}

// estimateChatAdmission operates on the final downstream envelope after
// synchronous guardrail transformation and prompt compression. Every retained JSON byte is
// covered, including tool schemas, tool arguments, response formats,
// predictions, reasoning configuration and explicitly modeled scalar/schema
// extensions. Standard non-text inputs and unknown fields are rejected because
// their provider-side expansion cannot be bounded from the JSON reference
// alone.
func estimateChatAdmission(body []byte) (requestAdmissionEstimate, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: chat request must be an object", errAdmissionUnsupportedShape)
	}
	if err := validateMeasurableChatRoot(envelope); err != nil {
		return requestAdmissionEstimate{}, err
	}

	rawMessages, exists := envelope["messages"]
	if !exists || isJSONNull(rawMessages) {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: messages must be an array", errAdmissionUnsupportedShape)
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(rawMessages, &messages); err != nil {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: messages must be an array", errAdmissionUnsupportedShape)
	}
	for index, raw := range messages {
		if err := validateMeasurableChatMessage(raw); err != nil {
			return requestAdmissionEstimate{}, fmt.Errorf("message %d: %w", index, err)
		}
	}

	if raw, ok := envelope["modalities"]; ok && !isJSONNull(raw) {
		var modalities []string
		if err := json.Unmarshal(raw, &modalities); err != nil {
			return requestAdmissionEstimate{}, fmt.Errorf("%w: modalities must be an array of strings", errAdmissionUnsupportedShape)
		}
		for _, modality := range modalities {
			if modality != "text" {
				return requestAdmissionEstimate{}, fmt.Errorf("%w: non-text output modality is not admission-bounded", errAdmissionUnsupportedShape)
			}
		}
	}
	if raw, ok := envelope["audio"]; ok && !isJSONNull(raw) {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: audio output is not admission-bounded", errAdmissionUnsupportedShape)
	}
	if raw, ok := envelope["prediction"]; ok && !isJSONNull(raw) {
		if err := validateMeasurablePrediction(raw); err != nil {
			return requestAdmissionEstimate{}, err
		}
	}

	collections := 0
	for _, field := range []string{"tools", "functions"} {
		if raw, ok := envelope[field]; ok && !isJSONNull(raw) {
			var values []json.RawMessage
			if err := json.Unmarshal(raw, &values); err != nil {
				return requestAdmissionEstimate{}, fmt.Errorf("%w: %s must be an array", errAdmissionUnsupportedShape, field)
			}
			if collections > math.MaxInt-len(values) {
				return requestAdmissionEstimate{}, errAdmissionEstimateOverflow
			}
			collections += len(values)
		}
	}

	return estimateChatByteUpper(body, len(messages), collections)
}

func estimateChatByteUpper(body []byte, messageCount, collections int) (requestAdmissionEstimate, error) {
	messageOverhead, ok := checkedAdmissionMultiply(int64(messageCount), chatAdmissionMessageOverhead)
	if !ok {
		return requestAdmissionEstimate{}, errAdmissionEstimateOverflow
	}
	overhead, ok := checkedAdmissionAdd(chatAdmissionBaseOverhead, messageOverhead)
	if !ok {
		return requestAdmissionEstimate{}, errAdmissionEstimateOverflow
	}
	collectionOverhead, ok := checkedAdmissionMultiply(int64(collections), chatAdmissionCollectionOverhead)
	if !ok {
		return requestAdmissionEstimate{}, errAdmissionEstimateOverflow
	}
	overhead, ok = checkedAdmissionAdd(overhead, collectionOverhead)
	if !ok {
		return requestAdmissionEstimate{}, errAdmissionEstimateOverflow
	}
	tokens, err := checkedAdmissionInt(int64(len(body)), overhead)
	if err != nil {
		return requestAdmissionEstimate{}, err
	}
	return requestAdmissionEstimate{InputTokens: tokens, Version: chatAdmissionEstimateVersion}, nil
}

func validateMeasurableChatMessage(raw json.RawMessage) error {
	var message map[string]json.RawMessage
	if err := json.Unmarshal(raw, &message); err != nil || message == nil {
		return fmt.Errorf("%w: chat message must be an object", errAdmissionUnsupportedShape)
	}
	if err := rejectUnknownJSONFields(message, "message", "role", "content", "name", "tool_call_id", "function_call", "tool_calls", "refusal", "reasoning", "reasoning_content"); err != nil {
		return err
	}
	if role, exists := message["role"]; !exists || !rawJSONString(role) {
		return fmt.Errorf("%w: message role must be a string", errAdmissionUnsupportedShape)
	}
	if rawContent, exists := message["content"]; exists && !isJSONNull(rawContent) {
		if err := validateMeasurableTextValue(rawContent); err != nil {
			return err
		}
	}
	for _, field := range []string{"name", "tool_call_id", "refusal", "reasoning", "reasoning_content"} {
		if value, ok := message[field]; ok && !isJSONNull(value) && firstJSONToken(value) != '"' {
			return fmt.Errorf("%w: message %s must be a string or null", errAdmissionUnsupportedShape, field)
		}
	}
	if rawCall, ok := message["function_call"]; ok && !isJSONNull(rawCall) {
		if err := validateMeasurableFunctionCall(rawCall); err != nil {
			return err
		}
	}
	if rawCalls, ok := message["tool_calls"]; ok && !isJSONNull(rawCalls) {
		var calls []json.RawMessage
		if err := json.Unmarshal(rawCalls, &calls); err != nil {
			return fmt.Errorf("%w: message tool_calls must be an array", errAdmissionUnsupportedShape)
		}
		for _, rawCall := range calls {
			var call map[string]json.RawMessage
			if err := json.Unmarshal(rawCall, &call); err != nil || call == nil {
				return fmt.Errorf("%w: message tool_call must be an object", errAdmissionUnsupportedShape)
			}
			if err := rejectUnknownJSONFields(call, "message tool_call", "id", "type", "function"); err != nil {
				return err
			}
			for _, field := range []string{"id", "type"} {
				value, exists := call[field]
				if !exists || isJSONNull(value) || !rawJSONString(value) {
					return fmt.Errorf("%w: message tool_call %s must be a string", errAdmissionUnsupportedShape, field)
				}
			}
			var kind string
			if err := json.Unmarshal(call["type"], &kind); err != nil || kind != "function" {
				return fmt.Errorf("%w: only function message tool_calls are supported", errAdmissionUnsupportedShape)
			}
			rawFunction, exists := call["function"]
			if !exists || isJSONNull(rawFunction) {
				return fmt.Errorf("%w: message tool_call function is required", errAdmissionUnsupportedShape)
			}
			if err := validateMeasurableFunctionCall(rawFunction); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateMeasurableChatRoot(envelope map[string]json.RawMessage) error {
	if err := rejectUnknownJSONFields(envelope, "chat request",
		"model", "messages", "stream", "stream_options", "max_tokens", "max_completion_tokens",
		"temperature", "top_p", "n", "stop", "presence_penalty", "frequency_penalty",
		"logit_bias", "logprobs", "top_logprobs", "seed", "response_format", "tools",
		"tool_choice", "parallel_tool_calls", "functions", "function_call", "user", "service_tier",
		"store", "metadata", "modalities", "audio", "prediction", "reasoning_effort", "reasoning",
		"thinking",
		"verbosity", "safety_identifier", "prompt_cache_key", "prompt_cache_retention",
		"repetition_penalty", "min_p", "top_k", "enable_thinking", "reasoning_format",
		"include_reasoning", "guided_json", "guided_regex", "guided_choice", "guided_grammar",
		"guided_decoding_backend"); err != nil {
		return err
	}
	if model, exists := envelope["model"]; !exists || !rawJSONString(model) {
		return fmt.Errorf("%w: model must be a string", errAdmissionUnsupportedShape)
	}
	if messages, exists := envelope["messages"]; !exists || firstJSONToken(messages) != '[' {
		return fmt.Errorf("%w: messages must be an array", errAdmissionUnsupportedShape)
	}
	for _, field := range []string{"stream", "parallel_tool_calls", "logprobs", "store", "enable_thinking", "include_reasoning"} {
		if raw, exists := envelope[field]; exists && !isJSONNull(raw) && !rawJSONBool(raw) {
			return fmt.Errorf("%w: %s must be a boolean or null", errAdmissionUnsupportedShape, field)
		}
	}
	for _, field := range []string{
		"max_tokens", "max_completion_tokens", "temperature", "top_p", "n", "presence_penalty",
		"frequency_penalty", "top_logprobs", "seed", "repetition_penalty", "min_p", "top_k",
	} {
		if raw, exists := envelope[field]; exists && !isJSONNull(raw) && !rawJSONNumber(raw) {
			return fmt.Errorf("%w: %s must be a number or null", errAdmissionUnsupportedShape, field)
		}
	}
	for _, field := range []string{
		"user", "service_tier", "reasoning_effort", "verbosity", "safety_identifier", "prompt_cache_key",
		"prompt_cache_retention", "reasoning_format", "guided_regex", "guided_grammar", "guided_decoding_backend",
	} {
		if raw, exists := envelope[field]; exists && !isJSONNull(raw) && !rawJSONString(raw) {
			return fmt.Errorf("%w: %s must be a string or null", errAdmissionUnsupportedShape, field)
		}
	}
	if raw, exists := envelope["stop"]; exists && !isJSONNull(raw) {
		if err := validateStringOrStringArray(raw, "stop"); err != nil {
			return err
		}
	}
	if raw, exists := envelope["stream_options"]; exists && !isJSONNull(raw) {
		var options map[string]json.RawMessage
		if err := json.Unmarshal(raw, &options); err != nil || options == nil {
			return fmt.Errorf("%w: stream_options must be an object", errAdmissionUnsupportedShape)
		}
		if err := rejectUnknownJSONFields(options, "stream_options", "include_usage", "include_obfuscation"); err != nil {
			return err
		}
		for key, value := range options {
			if !isJSONNull(value) && !rawJSONBool(value) {
				return fmt.Errorf("%w: stream_options %s must be a boolean or null", errAdmissionUnsupportedShape, key)
			}
		}
	}
	if raw, exists := envelope["metadata"]; exists && !isJSONNull(raw) {
		if err := validateOpenScalarJSONObject(raw, "metadata"); err != nil {
			return err
		}
	}
	if raw, exists := envelope["logit_bias"]; exists && !isJSONNull(raw) {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil || values == nil {
			return fmt.Errorf("%w: logit_bias must be an object", errAdmissionUnsupportedShape)
		}
		for key, value := range values {
			if !rawJSONNumber(value) {
				return fmt.Errorf("%w: logit_bias %s must be a number", errAdmissionUnsupportedShape, key)
			}
		}
	}
	if raw, exists := envelope["reasoning"]; exists && !isJSONNull(raw) {
		if err := validateScalarJSONObject(raw, "reasoning", "effort", "summary", "enabled", "exclude", "max_tokens", "budget_tokens"); err != nil {
			return err
		}
	}
	if raw, exists := envelope["thinking"]; exists && !isJSONNull(raw) {
		if _, _, err := optionalThinkingType(raw); err != nil {
			return fmt.Errorf("%w: %v", errAdmissionUnsupportedShape, err)
		}
	}
	if raw, exists := envelope["guided_choice"]; exists && !isJSONNull(raw) {
		if err := validateStringArray(raw, "guided_choice"); err != nil {
			return err
		}
	}
	if raw, exists := envelope["guided_json"]; exists && !isJSONNull(raw) && !rawJSONString(raw) && firstJSONToken(raw) != '{' {
		return fmt.Errorf("%w: guided_json must be a string or schema object", errAdmissionUnsupportedShape)
	}
	if raw, exists := envelope["tools"]; exists && !isJSONNull(raw) {
		if err := validateMeasurableTools(raw); err != nil {
			return err
		}
	}
	if raw, exists := envelope["functions"]; exists && !isJSONNull(raw) {
		if err := validateMeasurableFunctions(raw); err != nil {
			return err
		}
	}
	if raw, exists := envelope["tool_choice"]; exists && !isJSONNull(raw) {
		if err := validateMeasurableToolChoice(raw); err != nil {
			return err
		}
	}
	if raw, exists := envelope["function_call"]; exists && !isJSONNull(raw) {
		if err := validateMeasurableFunctionChoice(raw); err != nil {
			return err
		}
	}
	if raw, exists := envelope["response_format"]; exists && !isJSONNull(raw) {
		if err := validateMeasurableResponseFormat(raw); err != nil {
			return err
		}
	}
	return nil
}

func validateMeasurableTools(raw json.RawMessage) error {
	var tools []json.RawMessage
	if err := json.Unmarshal(raw, &tools); err != nil {
		return fmt.Errorf("%w: tools must be an array", errAdmissionUnsupportedShape)
	}
	for index, item := range tools {
		var tool map[string]json.RawMessage
		if err := json.Unmarshal(item, &tool); err != nil || tool == nil {
			return fmt.Errorf("%w: tool %d must be an object", errAdmissionUnsupportedShape, index)
		}
		if err := rejectUnknownJSONFields(tool, "tool", "type", "function"); err != nil {
			return err
		}
		var kind string
		if err := json.Unmarshal(tool["type"], &kind); err != nil || kind != "function" {
			return fmt.Errorf("%w: only function tools have a bounded request contract", errAdmissionUnsupportedShape)
		}
		function, exists := tool["function"]
		if !exists || isJSONNull(function) {
			return fmt.Errorf("%w: function tool definition is required", errAdmissionUnsupportedShape)
		}
		if err := validateMeasurableFunctionDefinition(function); err != nil {
			return fmt.Errorf("tool %d: %w", index, err)
		}
	}
	return nil
}

func validateMeasurableFunctions(raw json.RawMessage) error {
	var functions []json.RawMessage
	if err := json.Unmarshal(raw, &functions); err != nil {
		return fmt.Errorf("%w: functions must be an array", errAdmissionUnsupportedShape)
	}
	for index, function := range functions {
		if err := validateMeasurableFunctionDefinition(function); err != nil {
			return fmt.Errorf("function %d: %w", index, err)
		}
	}
	return nil
}

func validateMeasurableFunctionDefinition(raw json.RawMessage) error {
	var function map[string]json.RawMessage
	if err := json.Unmarshal(raw, &function); err != nil || function == nil {
		return fmt.Errorf("%w: function definition must be an object", errAdmissionUnsupportedShape)
	}
	if err := rejectUnknownJSONFields(function, "function definition", "name", "description", "parameters", "strict"); err != nil {
		return err
	}
	if name, exists := function["name"]; !exists || !rawJSONString(name) {
		return fmt.Errorf("%w: function name must be a string", errAdmissionUnsupportedShape)
	}
	if description, exists := function["description"]; exists && !isJSONNull(description) && !rawJSONString(description) {
		return fmt.Errorf("%w: function description must be a string or null", errAdmissionUnsupportedShape)
	}
	if parameters, exists := function["parameters"]; exists && !isJSONNull(parameters) && firstJSONToken(parameters) != '{' && !rawJSONBool(parameters) {
		return fmt.Errorf("%w: function parameters must be a JSON schema object or boolean", errAdmissionUnsupportedShape)
	}
	if strict, exists := function["strict"]; exists && !isJSONNull(strict) && !rawJSONBool(strict) {
		return fmt.Errorf("%w: function strict must be a boolean or null", errAdmissionUnsupportedShape)
	}
	return nil
}

func validateMeasurableToolChoice(raw json.RawMessage) error {
	if rawJSONString(raw) {
		var choice string
		_ = json.Unmarshal(raw, &choice)
		if choice == "none" || choice == "auto" || choice == "required" {
			return nil
		}
		return fmt.Errorf("%w: unsupported tool_choice", errAdmissionUnsupportedShape)
	}
	var choice map[string]json.RawMessage
	if err := json.Unmarshal(raw, &choice); err != nil || choice == nil {
		return fmt.Errorf("%w: tool_choice must be a string or object", errAdmissionUnsupportedShape)
	}
	if err := rejectUnknownJSONFields(choice, "tool_choice", "type", "function"); err != nil {
		return err
	}
	var kind string
	if err := json.Unmarshal(choice["type"], &kind); err != nil || kind != "function" {
		return fmt.Errorf("%w: only function tool_choice is supported", errAdmissionUnsupportedShape)
	}
	return validateNamedFunctionObject(choice["function"], "tool_choice function")
}

func validateMeasurableFunctionChoice(raw json.RawMessage) error {
	if rawJSONString(raw) {
		var choice string
		_ = json.Unmarshal(raw, &choice)
		if choice == "none" || choice == "auto" {
			return nil
		}
		return fmt.Errorf("%w: unsupported function_call", errAdmissionUnsupportedShape)
	}
	return validateNamedFunctionObject(raw, "function_call")
}

func validateNamedFunctionObject(raw json.RawMessage, label string) error {
	var function map[string]json.RawMessage
	if err := json.Unmarshal(raw, &function); err != nil || function == nil {
		return fmt.Errorf("%w: %s must be an object", errAdmissionUnsupportedShape, label)
	}
	if err := rejectUnknownJSONFields(function, label, "name"); err != nil {
		return err
	}
	if name, exists := function["name"]; !exists || !rawJSONString(name) {
		return fmt.Errorf("%w: %s name must be a string", errAdmissionUnsupportedShape, label)
	}
	return nil
}

func validateMeasurableResponseFormat(raw json.RawMessage) error {
	var format map[string]json.RawMessage
	if err := json.Unmarshal(raw, &format); err != nil || format == nil {
		return fmt.Errorf("%w: response_format must be an object", errAdmissionUnsupportedShape)
	}
	if err := rejectUnknownJSONFields(format, "response_format", "type", "json_schema"); err != nil {
		return err
	}
	var kind string
	if err := json.Unmarshal(format["type"], &kind); err != nil {
		return fmt.Errorf("%w: response_format type must be a string", errAdmissionUnsupportedShape)
	}
	schema, hasSchema := format["json_schema"]
	switch kind {
	case "text", "json_object":
		if hasSchema && !isJSONNull(schema) {
			return fmt.Errorf("%w: response_format %s cannot carry json_schema", errAdmissionUnsupportedShape, kind)
		}
		return nil
	case "json_schema":
		if !hasSchema || isJSONNull(schema) {
			return fmt.Errorf("%w: response_format json_schema is required", errAdmissionUnsupportedShape)
		}
	default:
		return fmt.Errorf("%w: unsupported response_format type", errAdmissionUnsupportedShape)
	}
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(schema, &definition); err != nil || definition == nil {
		return fmt.Errorf("%w: response_format json_schema must be an object", errAdmissionUnsupportedShape)
	}
	if err := rejectUnknownJSONFields(definition, "response_format json_schema", "name", "description", "schema", "strict"); err != nil {
		return err
	}
	if name, exists := definition["name"]; !exists || !rawJSONString(name) {
		return fmt.Errorf("%w: response_format json_schema name must be a string", errAdmissionUnsupportedShape)
	}
	if description, exists := definition["description"]; exists && !isJSONNull(description) && !rawJSONString(description) {
		return fmt.Errorf("%w: response_format json_schema description must be a string or null", errAdmissionUnsupportedShape)
	}
	if value, exists := definition["schema"]; !exists || (firstJSONToken(value) != '{' && !rawJSONBool(value)) {
		return fmt.Errorf("%w: response_format schema must be a JSON schema object or boolean", errAdmissionUnsupportedShape)
	}
	if strict, exists := definition["strict"]; exists && !isJSONNull(strict) && !rawJSONBool(strict) {
		return fmt.Errorf("%w: response_format json_schema strict must be a boolean or null", errAdmissionUnsupportedShape)
	}
	return nil
}

func validateMeasurableFunctionCall(raw json.RawMessage) error {
	var call map[string]json.RawMessage
	if err := json.Unmarshal(raw, &call); err != nil || call == nil {
		return fmt.Errorf("%w: function call must be an object", errAdmissionUnsupportedShape)
	}
	if err := rejectUnknownJSONFields(call, "function call", "name", "arguments"); err != nil {
		return err
	}
	for _, field := range []string{"name", "arguments"} {
		value, ok := call[field]
		if !ok || isJSONNull(value) || firstJSONToken(value) != '"' {
			return fmt.Errorf("%w: function call %s must be a string", errAdmissionUnsupportedShape, field)
		}
	}
	return nil
}

func validateMeasurablePrediction(raw json.RawMessage) error {
	var prediction map[string]json.RawMessage
	if err := json.Unmarshal(raw, &prediction); err != nil || prediction == nil {
		return fmt.Errorf("%w: prediction must be an object", errAdmissionUnsupportedShape)
	}
	if err := rejectUnknownJSONFields(prediction, "prediction", "type", "content"); err != nil {
		return err
	}
	if kind, exists := prediction["type"]; exists {
		var value string
		if err := json.Unmarshal(kind, &value); err != nil || value != "content" {
			return fmt.Errorf("%w: unsupported prediction type", errAdmissionUnsupportedShape)
		}
	}
	content, ok := prediction["content"]
	if !ok || isJSONNull(content) {
		return nil
	}
	if err := validateMeasurableTextValue(content); err != nil {
		return fmt.Errorf("prediction: %w", err)
	}
	return nil
}

func validateMeasurableTextValue(raw json.RawMessage) error {
	switch firstJSONToken(raw) {
	case '"':
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return fmt.Errorf("%w: text value is invalid", errAdmissionUnsupportedShape)
		}
		return nil
	case '[':
		var parts []json.RawMessage
		if err := json.Unmarshal(raw, &parts); err != nil {
			return fmt.Errorf("%w: text parts must be an array", errAdmissionUnsupportedShape)
		}
		for _, rawPart := range parts {
			var part map[string]json.RawMessage
			if err := json.Unmarshal(rawPart, &part); err != nil || part == nil {
				return fmt.Errorf("%w: text part must be an object", errAdmissionUnsupportedShape)
			}
			if err := rejectUnknownJSONFields(part, "text part", "type", "text", "refusal", "cache_control"); err != nil {
				return err
			}
			kind := "text"
			if rawType, exists := part["type"]; exists {
				if err := json.Unmarshal(rawType, &kind); err != nil {
					return fmt.Errorf("%w: text part type must be a string", errAdmissionUnsupportedShape)
				}
			}
			textField := "text"
			switch kind {
			case "text", "input_text", "output_text":
			case "refusal":
				textField = "refusal"
			default:
				return fmt.Errorf("%w: non-text content part is not admission-bounded", errAdmissionUnsupportedShape)
			}
			text, exists := part[textField]
			if !exists || firstJSONToken(text) != '"' {
				return fmt.Errorf("%w: text part %s must be a string", errAdmissionUnsupportedShape, textField)
			}
			otherField := "refusal"
			if textField == "refusal" {
				otherField = "text"
			}
			if value, exists := part[otherField]; exists && !isJSONNull(value) {
				return fmt.Errorf("%w: text part contains fields for a different type", errAdmissionUnsupportedShape)
			}
			if cacheControl, exists := part["cache_control"]; exists && !isJSONNull(cacheControl) {
				if err := validateScalarJSONObject(cacheControl, "cache_control", "type", "ttl"); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: content must be text, text parts, or null", errAdmissionUnsupportedShape)
	}
}

// estimateEmbeddingsAdmission accepts exactly the OpenAI-compatible text and
// pre-tokenized shapes. Token IDs are counted exactly rather than charged a
// heuristic per array item. Strings use the same UTF-8 byte upper bound.
func estimateEmbeddingsAdmission(body []byte) (requestAdmissionEstimate, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: embeddings request must be an object", errAdmissionUnsupportedShape)
	}
	if err := validateMeasurableEmbeddingsRoot(envelope, true); err != nil {
		return requestAdmissionEstimate{}, err
	}
	raw, exists := envelope["input"]
	if !exists || isJSONNull(raw) {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: input is required", errAdmissionUnsupportedShape)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return requestAdmissionEstimate{}, fmt.Errorf("%w: embeddings input is invalid", errAdmissionUnsupportedShape)
	}
	tokens, err := measurableEmbeddingInput(value)
	if err != nil {
		return requestAdmissionEstimate{}, err
	}
	return requestAdmissionEstimate{InputTokens: tokens, Version: embeddingsAdmissionEstimateVersion}, nil
}

func measurableEmbeddingInput(value any) (int, error) {
	switch input := value.(type) {
	case string:
		return checkedAdmissionInt(int64(len(input)), 0)
	case []any:
		if len(input) == 0 {
			return 0, nil
		}
		switch input[0].(type) {
		case string:
			var total int64
			for _, item := range input {
				text, ok := item.(string)
				if !ok {
					return 0, fmt.Errorf("%w: embeddings input arrays cannot mix shapes", errAdmissionUnsupportedShape)
				}
				var safe bool
				total, safe = checkedAdmissionAdd(total, int64(len(text)))
				if !safe {
					return 0, errAdmissionEstimateOverflow
				}
			}
			return checkedAdmissionInt(total, 0)
		case json.Number:
			for _, item := range input {
				if err := validateEmbeddingTokenID(item); err != nil {
					return 0, err
				}
			}
			return checkedAdmissionInt(int64(len(input)), 0)
		case []any:
			var total int64
			for _, item := range input {
				row, ok := item.([]any)
				if !ok {
					return 0, fmt.Errorf("%w: embeddings token arrays cannot mix shapes", errAdmissionUnsupportedShape)
				}
				for _, token := range row {
					if err := validateEmbeddingTokenID(token); err != nil {
						return 0, err
					}
				}
				var safe bool
				total, safe = checkedAdmissionAdd(total, int64(len(row)))
				if !safe {
					return 0, errAdmissionEstimateOverflow
				}
			}
			return checkedAdmissionInt(total, 0)
		default:
			return 0, fmt.Errorf("%w: unsupported embeddings input array", errAdmissionUnsupportedShape)
		}
	default:
		return 0, fmt.Errorf("%w: unsupported embeddings input", errAdmissionUnsupportedShape)
	}
}

func validateEmbeddingTokenID(value any) error {
	number, ok := value.(json.Number)
	if !ok {
		return fmt.Errorf("%w: embedding token IDs must be integers", errAdmissionUnsupportedShape)
	}
	lexeme := number.String()
	if lexeme == "" || strings.HasPrefix(lexeme, "+") || strings.HasPrefix(lexeme, "-") || strings.ContainsAny(lexeme, ".eE") {
		return fmt.Errorf("%w: embedding token IDs must be canonical integers", errAdmissionUnsupportedShape)
	}
	id, err := strconv.ParseInt(lexeme, 10, 64)
	if err != nil || id < 0 || id > maxEmbeddingTokenID {
		return fmt.Errorf("%w: embedding token ID is outside the supported range", errAdmissionUnsupportedShape)
	}
	return nil
}

func validateMeasurableEmbeddingsRoot(envelope map[string]json.RawMessage, requireModel bool) error {
	if err := rejectUnknownJSONFields(envelope, "embeddings request", "model", "input", "encoding_format", "dimensions", "user", "input_type", "truncate"); err != nil {
		return err
	}
	if model, exists := envelope["model"]; requireModel && (!exists || !rawJSONString(model)) {
		return fmt.Errorf("%w: model must be a string", errAdmissionUnsupportedShape)
	} else if exists && !rawJSONString(model) {
		return fmt.Errorf("%w: model must be a string", errAdmissionUnsupportedShape)
	}
	for _, field := range []string{"encoding_format", "user", "input_type"} {
		if raw, exists := envelope[field]; exists && !isJSONNull(raw) && !rawJSONString(raw) {
			return fmt.Errorf("%w: embeddings %s must be a string or null", errAdmissionUnsupportedShape, field)
		}
	}
	if raw, exists := envelope["dimensions"]; exists && !isJSONNull(raw) && !rawJSONNumber(raw) {
		return fmt.Errorf("%w: embeddings dimensions must be a number or null", errAdmissionUnsupportedShape)
	}
	if raw, exists := envelope["truncate"]; exists && !isJSONNull(raw) && !rawJSONString(raw) && !rawJSONBool(raw) {
		return fmt.Errorf("%w: embeddings truncate must be a string, boolean, or null", errAdmissionUnsupportedShape)
	}
	return nil
}

func rejectUnknownJSONFields(fields map[string]json.RawMessage, label string, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		allowedSet[field] = struct{}{}
	}
	unknown := make([]string, 0)
	for field := range fields {
		if _, exists := allowedSet[field]; !exists {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%w: %s field %q has no bounded forwarding contract", errAdmissionUnsupportedShape, label, unknown[0])
}

func rawJSONString(raw json.RawMessage) bool {
	if firstJSONToken(raw) != '"' {
		return false
	}
	var value string
	return json.Unmarshal(raw, &value) == nil
}

func rawJSONBool(raw json.RawMessage) bool {
	if token := firstJSONToken(raw); token != 't' && token != 'f' {
		return false
	}
	var value bool
	return json.Unmarshal(raw, &value) == nil
}

func rawJSONNumber(raw json.RawMessage) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	_, ok := value.(json.Number)
	return ok
}

func rawJSONScalar(raw json.RawMessage) bool {
	return isJSONNull(raw) || rawJSONString(raw) || rawJSONBool(raw) || rawJSONNumber(raw)
}

func validateScalarJSONObject(raw json.RawMessage, label string, allowed ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("%w: %s must be an object", errAdmissionUnsupportedShape, label)
	}
	if err := rejectUnknownJSONFields(fields, label, allowed...); err != nil {
		return err
	}
	for field, value := range fields {
		if !rawJSONScalar(value) {
			return fmt.Errorf("%w: %s %s must be a scalar", errAdmissionUnsupportedShape, label, field)
		}
	}
	return nil
}

func validateOpenScalarJSONObject(raw json.RawMessage, label string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("%w: %s must be an object", errAdmissionUnsupportedShape, label)
	}
	for field, value := range fields {
		if !rawJSONScalar(value) {
			return fmt.Errorf("%w: %s %s must be a scalar", errAdmissionUnsupportedShape, label, field)
		}
	}
	return nil
}

func validateStringOrStringArray(raw json.RawMessage, label string) error {
	if rawJSONString(raw) {
		return nil
	}
	return validateStringArray(raw, label)
}

func validateStringArray(raw json.RawMessage, label string) error {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("%w: %s must be an array of strings", errAdmissionUnsupportedShape, label)
	}
	for _, value := range values {
		if !rawJSONString(value) {
			return fmt.Errorf("%w: %s must be an array of strings", errAdmissionUnsupportedShape, label)
		}
	}
	return nil
}

func checkedAdmissionAdd(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, false
	}
	return left + right, true
}

func checkedAdmissionMultiply(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || (left != 0 && right > math.MaxInt64/left) {
		return 0, false
	}
	return left * right, true
}

func checkedAdmissionInt(base, overhead int64) (int, error) {
	total, ok := checkedAdmissionAdd(base, overhead)
	if !ok || total > int64(math.MaxInt) {
		return 0, errAdmissionEstimateOverflow
	}
	return int(total), nil
}
