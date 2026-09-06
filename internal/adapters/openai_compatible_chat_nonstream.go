package adapters

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

var errInvalidChatNonStreamMechanics = errors.New("invalid OpenAI-compatible Chat non-stream mechanics")

// ChatNonStreamDeliveryIssue is a closed, process-local delivery outcome. It
// is not a protocol, Probe, evidence or persistence identity.
type ChatNonStreamDeliveryIssue string

const (
	ChatNonStreamDeliveryIssueNone             ChatNonStreamDeliveryIssue = ""
	ChatNonStreamToolCallNormalizationInvalid  ChatNonStreamDeliveryIssue = "tool_call_normalization_invalid"
	ChatNonStreamRequestIDNormalizationInvalid ChatNonStreamDeliveryIssue = "request_id_normalization_invalid"
)

// ChatNonStreamMechanics is selected by existing exact runtime or Probe
// authorities. It must never be accepted from a client or persisted as a new
// policy axis.
type ChatNonStreamMechanics struct {
	RejectDuplicateMembers bool
	NormalizeToolCallIndex bool
	NormalizeRequestID     bool
	// Set only by the validated immutable data Profile selector.
	dataToolCallIndex bool
}

// ChatNonStreamPrepared contains only the bytes prepared for the caller's
// existing role-specific validator. Runtime decodes its final delivery object
// after usage/cache projections; Probe keeps its immutable typed decoder.
type ChatNonStreamPrepared struct {
	Body          []byte
	Changed       bool
	DeliveryIssue ChatNonStreamDeliveryIssue
}

func validateChatNonStreamMechanics(mechanics ChatNonStreamMechanics) error {
	if mechanics.dataToolCallIndex && (!mechanics.NormalizeToolCallIndex || !mechanics.RejectDuplicateMembers) {
		return errInvalidChatNonStreamMechanics
	}
	if (mechanics.NormalizeToolCallIndex || mechanics.NormalizeRequestID) && !mechanics.RejectDuplicateMembers {
		return errInvalidChatNonStreamMechanics
	}
	return nil
}

func runtimeChatNonStreamMechanics(in ResponseInput) (ChatNonStreamMechanics, bool, error) {
	if in.ProfileDefinition.Digest() != "" {
		return dataChatNonStreamMechanics(in)
	}
	mechanics, found, err := kimiRuntimeChatNonStreamMechanics(in)
	if err != nil || found {
		return mechanics, found, err
	}
	mechanics, found, err = glmRuntimeChatNonStreamMechanics(in)
	if err != nil || found {
		return mechanics, found, err
	}
	return miniMaxHighspeedRuntimeChatNonStreamMechanics(in)
}

func miniMaxHighspeedToolCallIndexScope(provider, upstreamModel, protocolProfile string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "minimax") &&
		strings.TrimSpace(upstreamModel) == "MiniMax-M2.7-highspeed" &&
		strings.TrimSpace(protocolProfile) == "protocol.minimax-m2-openai-chat.v7"
}

func miniMaxHighspeedRuntimeChatNonStreamMechanics(in ResponseInput) (ChatNonStreamMechanics, bool, error) {
	if in.Endpoint != EndpointChatCompletions ||
		strings.TrimSpace(in.ExecutionProfileID) != "minimax.m2.7-highspeed.payg" ||
		strings.TrimSpace(in.ExecutionProfileVersion) != "1.2.0" {
		return ChatNonStreamMechanics{}, false, nil
	}
	if !miniMaxHighspeedToolCallIndexScope(in.Channel.Provider, in.UpstreamModel, in.ProtocolProfile) ||
		strings.TrimSpace(in.ModelOperationID) != modelprofile.OperationOpenAIChat ||
		strings.TrimSpace(in.OperationProtocolProfileID) != "protocol.minimax-m2-openai-chat.v7" ||
		strings.TrimSpace(in.OperationProtocolProfileVersion) != modelprofile.ChatOperationProtocolBindingVersionV1 {
		return ChatNonStreamMechanics{}, false, fmt.Errorf("exact MiniMax Highspeed Chat runtime identity mismatch")
	}
	return ChatNonStreamMechanics{RejectDuplicateMembers: true, NormalizeToolCallIndex: true}, true, nil
}

func glmRuntimeChatNonStreamMechanics(in ResponseInput) (ChatNonStreamMechanics, bool, error) {
	if in.Endpoint != EndpointChatCompletions {
		return ChatNonStreamMechanics{}, false, nil
	}
	expectedModel, expectedProtocol := "", ""
	switch strings.TrimSpace(in.ExecutionProfileID) + "\x00" + strings.TrimSpace(in.ExecutionProfileVersion) {
	case "glm.5.3.api\x001.8.0":
		expectedModel, expectedProtocol = "glm-5.3", "protocol.glm-openai-chat.v5"
	case "glm.5.2.api\x001.8.0":
		expectedModel, expectedProtocol = "glm-5.2", "protocol.glm-openai-chat.v6"
	case "glm.5.1.api\x001.0.0":
		expectedModel, expectedProtocol = "glm-5.1", "protocol.glm-openai-chat.v6"
	case "glm.5-turbo.api\x001.0.0":
		expectedModel, expectedProtocol = "glm-5-turbo", "protocol.glm-openai-chat.v6"
	case "glm.5.api\x001.0.0":
		expectedModel, expectedProtocol = "glm-5", "protocol.glm-openai-chat.v6"
	case "glm.5v-turbo.api\x001.11.0":
		expectedModel, expectedProtocol = "glm-5v-turbo", "protocol.glm-openai-chat.glm5v.v5"
	case "glm.5.3.api\x001.9.0":
		expectedModel, expectedProtocol = "glm-5.3", "protocol.glm-openai-chat.v7"
	case "glm.5.2.api\x001.9.0":
		expectedModel, expectedProtocol = "glm-5.2", "protocol.glm-openai-chat.v7"
	case "glm.5.1.api\x001.1.0":
		expectedModel, expectedProtocol = "glm-5.1", "protocol.glm-openai-chat.v7"
	case "glm.5-turbo.api\x001.1.0":
		expectedModel, expectedProtocol = "glm-5-turbo", "protocol.glm-openai-chat.v7"
	case "glm.5.api\x001.1.0":
		expectedModel, expectedProtocol = "glm-5", "protocol.glm-openai-chat.v7"
	case "glm.5v-turbo.api\x001.12.0":
		expectedModel, expectedProtocol = "glm-5v-turbo", "protocol.glm-openai-chat.glm5v.v6"
	default:
		return ChatNonStreamMechanics{}, false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(in.Channel.Provider), "glm") ||
		strings.TrimSpace(in.UpstreamModel) != expectedModel ||
		strings.TrimSpace(in.ModelOperationID) != modelprofile.OperationOpenAIChat ||
		strings.TrimSpace(in.ProtocolProfile) != expectedProtocol ||
		strings.TrimSpace(in.OperationProtocolProfileID) != expectedProtocol ||
		strings.TrimSpace(in.OperationProtocolProfileVersion) != modelprofile.ChatOperationProtocolBindingVersionV1 {
		return ChatNonStreamMechanics{}, false, fmt.Errorf("exact GLM Chat runtime identity mismatch")
	}
	return ChatNonStreamMechanics{RejectDuplicateMembers: true, NormalizeRequestID: true}, true, nil
}

func kimiRuntimeChatNonStreamMechanics(in ResponseInput) (ChatNonStreamMechanics, bool, error) {
	if in.Endpoint != EndpointChatCompletions {
		return ChatNonStreamMechanics{}, false, nil
	}
	profileID := strings.TrimSpace(in.ExecutionProfileID)
	profileVersion := strings.TrimSpace(in.ExecutionProfileVersion)
	expectedModel, expectedProtocol := "", ""
	switch profileID + "\x00" + profileVersion {
	case "kimi.k2.6.api\x001.7.0":
		expectedModel, expectedProtocol = "kimi-k2.6", "protocol.kimi-openai-chat.v5"
	case "kimi.k2.7-code.api\x001.3.0":
		expectedModel, expectedProtocol = "kimi-k2.7-code", "protocol.kimi-openai-chat.v5"
	case "kimi.k3.api\x001.9.0":
		expectedModel, expectedProtocol = "kimi-k3", "protocol.kimi-openai-chat.k3.v5"
	default:
		return ChatNonStreamMechanics{}, false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(in.Channel.Provider), "kimi") ||
		strings.TrimSpace(in.UpstreamModel) != expectedModel ||
		strings.TrimSpace(in.ModelOperationID) != "openai.chat_completions.v1" ||
		strings.TrimSpace(in.ProtocolProfile) != expectedProtocol ||
		strings.TrimSpace(in.OperationProtocolProfileID) != expectedProtocol ||
		strings.TrimSpace(in.OperationProtocolProfileVersion) != modelprofile.ChatOperationProtocolBindingVersionV1 {
		return ChatNonStreamMechanics{}, false, fmt.Errorf("exact Kimi Chat runtime identity mismatch")
	}
	return ChatNonStreamMechanics{RejectDuplicateMembers: true, NormalizeToolCallIndex: true}, true, nil
}

func decodeOpenAICompatibleChatNonStreamObject(body []byte) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var object map[string]interface{}
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	if object == nil {
		return nil, errors.New("JSON object is required")
	}
	return object, nil
}

// ValidateOpenAICompatibleChatNonStreamDocument runs only the lexical
// ambiguity check selected by an existing authority. Callers that establish
// provider usage must invoke it before any map decoding or usage extraction.
func ValidateOpenAICompatibleChatNonStreamDocument(body []byte, mechanics ChatNonStreamMechanics) error {
	if err := validateChatNonStreamMechanics(mechanics); err != nil {
		return err
	}
	if !mechanics.RejectDuplicateMembers {
		return nil
	}
	return strictjson.Validate(body)
}

// PrepareOpenAICompatibleChatNonStreamDocument applies only the existing exact
// non-stream child tool-call-index or non-authoritative request-id normalization.
// It assumes the caller has already run the selected ambiguity check. Any normalizer failure over the
// same syntactically valid bytes is deliberately returned as a closed delivery
// issue so runtime can retain provider usage before its canonical guardrail
// rejects the original body. Probe v4 maps the same issue to delivery_shape.
func PrepareOpenAICompatibleChatNonStreamDocument(
	provider, upstreamModel, protocolProfile, protocolProfileVersion string,
	body []byte,
	mechanics ChatNonStreamMechanics,
) (ChatNonStreamPrepared, error) {
	prepared := ChatNonStreamPrepared{Body: body}
	if err := validateChatNonStreamMechanics(mechanics); err != nil {
		return prepared, err
	}
	if !mechanics.NormalizeToolCallIndex && !mechanics.NormalizeRequestID {
		return prepared, nil
	}
	if protocolProfileVersion != modelprofile.ChatOperationProtocolBindingVersionV1 {
		return prepared, errInvalidChatNonStreamMechanics
	}
	if mechanics.NormalizeToolCallIndex {
		var normalized []byte
		var changed bool
		var err error
		if mechanics.dataToolCallIndex || miniMaxHighspeedToolCallIndexScope(provider, upstreamModel, protocolProfile) {
			// An exact legacy or immutable data Profile selector enables these mechanics.
			// Keep the exported triple-only normalizer's historical scope intact.
			normalized, changed, err = normalizeOpenAICompatibleChatToolCallIndexDocument(body)
		} else {
			normalized, changed, err = NormalizeOpenAICompatibleChatToolCallIndex(provider, upstreamModel, protocolProfile, body)
		}
		if err != nil {
			prepared.DeliveryIssue = ChatNonStreamToolCallNormalizationInvalid
			return prepared, nil
		}
		prepared.Body, prepared.Changed = normalized, changed
	}
	if mechanics.NormalizeRequestID {
		object, err := decodeOpenAICompatibleChatNonStreamObject(prepared.Body)
		if err != nil {
			prepared.DeliveryIssue = ChatNonStreamRequestIDNormalizationInvalid
			return prepared, nil
		}
		if value, present := object["request_id"]; present {
			// GLM documents this non-canonical metadata as a string. It is
			// discarded, never adopted as request/trace authority. The existing
			// response-body limit bounds it; input/header ID limits do not apply.
			if _, ok := value.(string); !ok {
				prepared.DeliveryIssue = ChatNonStreamRequestIDNormalizationInvalid
				return prepared, nil
			}
			delete(object, "request_id")
			normalized, err := json.Marshal(object)
			if err != nil {
				return prepared, err
			}
			prepared.Body, prepared.Changed = normalized, true
		}
	}
	return prepared, nil
}
