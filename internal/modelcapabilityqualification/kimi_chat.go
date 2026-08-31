package modelcapabilityqualification

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const (
	KimiChatMaxResponseBytes = 1 << 20
	KimiChatMaxSSELineBytes  = 256 << 10
)

var ErrInvalidKimiChatProbe = errors.New("invalid Kimi Chat capability probe response")

type kimiChatValidationError struct {
	code  string
	cause error
}

func (e *kimiChatValidationError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", ErrInvalidKimiChatProbe, e.code, e.cause)
	}
	return fmt.Sprintf("%s: %s", ErrInvalidKimiChatProbe, e.code)
}

func (e *kimiChatValidationError) Unwrap() error { return ErrInvalidKimiChatProbe }

func invalidKimiChat(code string, cause ...error) error {
	err := &kimiChatValidationError{code: code}
	if len(cause) != 0 {
		err.cause = cause[0]
	}
	return err
}

func KimiChatErrorCode(err error) string {
	var validationErr *kimiChatValidationError
	if errors.As(err, &validationErr) {
		return validationErr.code
	}
	return ""
}

// KimiChatDiagnostic projects a stable failure category and, when safe, a
// separately bounded, syntax-constrained provider-controlled member identifier.
// The member is diagnostic evidence only: it is not part of the error-code
// identity, and its value and the complete raw response body are not returned.
type KimiChatDiagnostic struct {
	Code   string
	Member string
}

func DescribeKimiChatDiagnostic(err error) KimiChatDiagnostic {
	var validationErr *kimiChatValidationError
	if !errors.As(err, &validationErr) {
		return KimiChatDiagnostic{}
	}
	if validationErr.code != "json" || validationErr.cause == nil {
		return KimiChatDiagnostic{Code: validationErr.code}
	}
	const unknownFieldPrefix = "json: unknown field \""
	message := validationErr.cause.Error()
	if strings.HasPrefix(message, unknownFieldPrefix) && strings.HasSuffix(message, "\"") {
		field := strings.TrimSuffix(strings.TrimPrefix(message, unknownFieldPrefix), "\"")
		if kimiDiagnosticIdentifier(field) {
			return KimiChatDiagnostic{Code: "json_unknown_field", Member: field}
		}
		return KimiChatDiagnostic{Code: "json_unknown_field"}
	}
	var typeError *json.UnmarshalTypeError
	if errors.As(validationErr.cause, &typeError) {
		return KimiChatDiagnostic{Code: "json_type_mismatch"}
	}
	if strings.Contains(strings.ToLower(message), "duplicate") {
		return KimiChatDiagnostic{Code: "json_duplicate_field"}
	}
	return KimiChatDiagnostic{Code: "json_decode_error"}
}

func KimiChatDiagnosticCode(err error) string {
	return DescribeKimiChatDiagnostic(err).Code
}

// CanonicalizeKimiChatDiagnostic preserves immutable 1.2.18 summaries while
// projecting their historical dynamic unknown-field code through the stable
// 1.2.19 API shape. It never mutates stored evidence.
func CanonicalizeKimiChatDiagnostic(code, member string) KimiChatDiagnostic {
	const legacyUnknownFieldPrefix = "json_unknown_field_"
	if strings.HasPrefix(code, legacyUnknownFieldPrefix) {
		legacyMember := strings.TrimPrefix(code, legacyUnknownFieldPrefix)
		if kimiDiagnosticIdentifier(legacyMember) {
			return KimiChatDiagnostic{Code: "json_unknown_field", Member: legacyMember}
		}
		return KimiChatDiagnostic{Code: "json_unknown_field"}
	}
	if code == "json_unknown_field" {
		if kimiDiagnosticIdentifier(member) {
			return KimiChatDiagnostic{Code: code, Member: member}
		}
		return KimiChatDiagnostic{Code: code}
	}
	return KimiChatDiagnostic{Code: code}
}

func kimiDiagnosticIdentifier(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return true
}

type KimiChatObservation struct {
	ReportedModel            string
	FinishReason             string
	PromptTokens             int64
	CompletionTokens         int64
	TotalTokens              int64
	CachedTokens             int64
	ReasoningObserved        bool
	ToolCallObserved         bool
	StructuredOutputObserved bool
	DataEventCount           int
	DeltaCount               int
	DoneSeen                 bool
	ResponseBytes            int
}

type kimiJSONNumber string

func (number *kimiJSONNumber) UnmarshalJSON(raw []byte) error {
	value := string(bytes.TrimSpace(raw))
	if !kimiIntegerLexeme(value) {
		return errors.New("not a JSON integer")
	}
	*number = kimiJSONNumber(value)
	return nil
}

func kimiIntegerLexeme(value string) bool {
	if value == "" {
		return false
	}
	if value[0] == '-' {
		value = value[1:]
		if value == "" {
			return false
		}
	}
	if value == "0" {
		return true
	}
	if value[0] < '1' || value[0] > '9' {
		return false
	}
	for index := 1; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func (number kimiJSONNumber) jsonNumber() json.Number { return json.Number(number) }

type kimiChatUsage struct {
	PromptTokens     json.Number  `json:"prompt_tokens"`
	CompletionTokens json.Number  `json:"completion_tokens"`
	TotalTokens      json.Number  `json:"total_tokens"`
	CachedTokens     *json.Number `json:"cached_tokens,omitempty"`
}

type kimiChatStreamUsage struct {
	PromptTokens     json.Number  `json:"prompt_tokens"`
	CompletionTokens json.Number  `json:"completion_tokens"`
	TotalTokens      json.Number  `json:"total_tokens"`
	CachedTokens     *json.Number `json:"cached_tokens,omitempty"`
	PromptDetails    *struct {
		CachedTokens *json.Number `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionDetails *struct {
		ReasoningTokens *json.Number `json:"reasoning_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

type kimiChatCoreV2Usage struct {
	PromptTokens     kimiJSONNumber  `json:"prompt_tokens"`
	CompletionTokens kimiJSONNumber  `json:"completion_tokens"`
	TotalTokens      kimiJSONNumber  `json:"total_tokens"`
	CachedTokens     *kimiJSONNumber `json:"cached_tokens,omitempty"`
	PromptDetails    *struct {
		CachedTokens *kimiJSONNumber `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionDetails *struct {
		ReasoningTokens *kimiJSONNumber `json:"reasoning_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

type kimiChatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type kimiChatMessage struct {
	Role             string             `json:"role"`
	Content          json.RawMessage    `json:"content"`
	ReasoningContent json.RawMessage    `json:"reasoning_content,omitempty"`
	Reasoning        json.RawMessage    `json:"reasoning,omitempty"`
	Refusal          json.RawMessage    `json:"refusal,omitempty"`
	ToolCalls        []kimiChatToolCall `json:"tool_calls,omitempty"`
}

type kimiChatChoice struct {
	Index        int             `json:"index"`
	Message      kimiChatMessage `json:"message"`
	Text         json.RawMessage `json:"text,omitempty"`
	FinishReason string          `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs,omitempty"`
}

type kimiChatCompletion struct {
	ID                string           `json:"id"`
	Object            string           `json:"object"`
	Created           int64            `json:"created"`
	Model             string           `json:"model"`
	SystemFingerprint string           `json:"system_fingerprint,omitempty"`
	ServiceTier       json.RawMessage  `json:"service_tier,omitempty"`
	Metadata          json.RawMessage  `json:"metadata,omitempty"`
	Choices           []kimiChatChoice `json:"choices"`
	Usage             json.RawMessage  `json:"usage"`
}

func kimiChatCoreProbeContractSupported(version string) bool {
	return version == KimiChatCoreProbeContractVersion || version == KimiChatCoreProbeContractVersionV2
}

func kimiChatReasoningProbeContractSupported(contractID, version string) bool {
	switch contractID {
	case KimiChatReasoningProbeContractID:
		return kimiChatCoreProbeContractSupported(version)
	case KimiChatReasoningEffortProbeContractID:
		return version == KimiChatReasoningEffortProbeContractVersion
	default:
		return false
	}
}

func kimiChatFunctionToolsProbeContractSupported(version string) bool {
	return kimiChatCoreProbeContractSupported(version) ||
		version == KimiChatFunctionToolsProbeContractVersionV3 ||
		version == KimiChatFunctionToolsProbeContractVersionV4
}

func kimiChatFunctionToolsV3DeliveryShape(version string) bool {
	return version == KimiChatFunctionToolsProbeContractVersionV3 ||
		version == KimiChatFunctionToolsProbeContractVersionV4
}

func legacyKimiChatProbeBinding(probeContractID, probeContractVersion, expectedModel string) Binding {
	binding := Binding{
		OperationID: "openai.chat_completions.v1", ProbeContractID: probeContractID,
		ProbeContractVersion: probeContractVersion, ExpectedProvider: "kimi",
		ExpectedUpstreamModel: expectedModel, PublicPath: "/v1/chat/completions",
		ProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
	}
	switch strings.TrimSpace(expectedModel) {
	case "kimi-k2.6":
		binding.ExecutionProfileID = "kimi.k2.6.api"
		binding.ExecutionProfileVersion = "1.7.0"
		binding.ProtocolProfileID = "protocol.kimi-openai-chat.v5"
	case "kimi-k2.7-code":
		binding.ExecutionProfileID = "kimi.k2.7-code.api"
		binding.ExecutionProfileVersion = "1.3.0"
		binding.ProtocolProfileID = "protocol.kimi-openai-chat.v5"
	case "kimi-k3":
		binding.ExecutionProfileID = "kimi.k3.api"
		binding.ExecutionProfileVersion = "1.9.0"
		binding.ProtocolProfileID = "protocol.kimi-openai-chat.k3.v5"
	}
	return binding
}

func kimiChatNonStreamMechanics(binding Binding) (adapters.ChatNonStreamMechanics, error) {
	expectedProfileID, expectedProfileVersion, expectedProtocol := "", "", ""
	switch strings.TrimSpace(binding.ExpectedUpstreamModel) {
	case "kimi-k2.6":
		expectedProfileID, expectedProfileVersion, expectedProtocol = "kimi.k2.6.api", "1.7.0", "protocol.kimi-openai-chat.v5"
	case "kimi-k2.7-code":
		expectedProfileID, expectedProfileVersion, expectedProtocol = "kimi.k2.7-code.api", "1.3.0", "protocol.kimi-openai-chat.v5"
	case "kimi-k3":
		expectedProfileID, expectedProfileVersion, expectedProtocol = "kimi.k3.api", "1.9.0", "protocol.kimi-openai-chat.k3.v5"
	default:
		return adapters.ChatNonStreamMechanics{}, invalidKimiChat("expected_model")
	}
	if !strings.EqualFold(strings.TrimSpace(binding.ExpectedProvider), "kimi") ||
		strings.TrimSpace(binding.OperationID) != "openai.chat_completions.v1" ||
		strings.TrimSpace(binding.ExecutionProfileID) != expectedProfileID ||
		strings.TrimSpace(binding.ExecutionProfileVersion) != expectedProfileVersion ||
		strings.TrimSpace(binding.ProtocolProfileID) != expectedProtocol ||
		strings.TrimSpace(binding.ProtocolProfileVersion) != modelprofile.ChatOperationProtocolBindingVersionV1 ||
		strings.TrimSpace(binding.PublicPath) != "/v1/chat/completions" {
		return adapters.ChatNonStreamMechanics{}, invalidKimiChat("probe_contract")
	}
	mechanics := adapters.ChatNonStreamMechanics{}
	if binding.ProbeContractID == KimiChatReasoningEffortProbeContractID ||
		kimiChatFunctionToolsV3DeliveryShape(binding.ProbeContractVersion) {
		mechanics.RejectDuplicateMembers = true
	}
	if binding.ProbeContractID == KimiChatFunctionToolsProbeContractID &&
		binding.ProbeContractVersion == KimiChatFunctionToolsProbeContractVersionV4 {
		mechanics.NormalizeToolCallIndex = true
	}
	return mechanics, nil
}

func ValidateKimiChatReasoning(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (KimiChatObservation, error) {
	return ValidateKimiChatReasoningForBinding(
		legacyKimiChatProbeBinding(probeContractID, probeContractVersion, expectedModel),
		contentType, reader,
	)
}

func ValidateKimiChatReasoningForBinding(
	binding Binding, contentType string, reader io.Reader,
) (KimiChatObservation, error) {
	if !kimiChatReasoningProbeContractSupported(binding.ProbeContractID, binding.ProbeContractVersion) {
		return KimiChatObservation{}, invalidKimiChat("probe_contract")
	}
	payload, observation, err := readKimiChatCompletion(binding, contentType, reader)
	if err != nil {
		return observation, err
	}
	choice, err := validateKimiSingleChoice(payload, &observation, "stop")
	if err != nil {
		return observation, err
	}
	var content, reasoning string
	if json.Unmarshal(choice.Message.Content, &content) != nil || strings.TrimSpace(content) != "UNIROUTER_REASONING_PROBE_OK" {
		return observation, invalidKimiChat("content")
	}
	if json.Unmarshal(choice.Message.ReasoningContent, &reasoning) != nil || strings.TrimSpace(reasoning) == "" {
		return observation, invalidKimiChat("reasoning_content")
	}
	if len(choice.Message.ToolCalls) != 0 {
		return observation, invalidKimiChat("tool_calls")
	}
	observation.ReasoningObserved = true
	return observation, nil
}

func ValidateKimiChatFunctionTools(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (KimiChatObservation, error) {
	return ValidateKimiChatFunctionToolsForBinding(
		legacyKimiChatProbeBinding(probeContractID, probeContractVersion, expectedModel),
		contentType, reader,
	)
}

func ValidateKimiChatFunctionToolsForBinding(
	binding Binding, contentType string, reader io.Reader,
) (KimiChatObservation, error) {
	if binding.ProbeContractID != KimiChatFunctionToolsProbeContractID ||
		!kimiChatFunctionToolsProbeContractSupported(binding.ProbeContractVersion) {
		return KimiChatObservation{}, invalidKimiChat("probe_contract")
	}
	payload, observation, err := readKimiChatCompletion(binding, contentType, reader)
	if err != nil {
		return observation, err
	}
	choice, err := validateKimiSingleChoice(payload, &observation, "tool_calls")
	if err != nil {
		return observation, err
	}
	if len(choice.Message.ToolCalls) != 1 {
		return observation, invalidKimiChat("tool_calls")
	}
	if err := validateKimiK27RequiredReasoning(binding.ExpectedUpstreamModel, choice.Message.ReasoningContent); err != nil {
		return observation, err
	}
	call := choice.Message.ToolCalls[0]
	if strings.TrimSpace(call.ID) == "" || call.Type != "function" || call.Function.Name != "unirouter_probe_echo" {
		return observation, invalidKimiChat("tool_call_identity")
	}
	var arguments struct {
		Value string `json:"value"`
	}
	if kimiChatFunctionToolsV3DeliveryShape(binding.ProbeContractVersion) {
		if err := strictjson.Validate([]byte(call.Function.Arguments)); err != nil {
			return observation, invalidKimiChat("tool_arguments", err)
		}
	}
	if err := decodeKimiJSONStrict([]byte(call.Function.Arguments), &arguments); err != nil || arguments.Value != "UNIROUTER_TOOL_PROBE_OK" {
		return observation, invalidKimiChat("tool_arguments", err)
	}
	observation.ToolCallObserved = true
	return observation, nil
}

func ValidateKimiChatJSONObject(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (KimiChatObservation, error) {
	return ValidateKimiChatJSONObjectForBinding(
		legacyKimiChatProbeBinding(probeContractID, probeContractVersion, expectedModel),
		contentType, reader,
	)
}

func ValidateKimiChatJSONObjectForBinding(
	binding Binding, contentType string, reader io.Reader,
) (KimiChatObservation, error) {
	if binding.ProbeContractID != KimiChatJSONObjectProbeContractID ||
		!kimiChatCoreProbeContractSupported(binding.ProbeContractVersion) {
		return KimiChatObservation{}, invalidKimiChat("probe_contract")
	}
	payload, observation, err := readKimiChatCompletion(binding, contentType, reader)
	if err != nil {
		return observation, err
	}
	choice, err := validateKimiSingleChoice(payload, &observation, "stop")
	if err != nil {
		return observation, err
	}
	var content string
	if json.Unmarshal(choice.Message.Content, &content) != nil {
		return observation, invalidKimiChat("structured_output")
	}
	var object struct {
		Status string `json:"status"`
	}
	if err := decodeKimiJSONStrict([]byte(strings.TrimSpace(content)), &object); err != nil || object.Status != "UNIROUTER_JSON_PROBE_OK" {
		return observation, invalidKimiChat("structured_output", err)
	}
	if len(choice.Message.ToolCalls) != 0 {
		return observation, invalidKimiChat("tool_calls")
	}
	if err := validateKimiK27RequiredReasoning(binding.ExpectedUpstreamModel, choice.Message.ReasoningContent); err != nil {
		return observation, err
	}
	observation.StructuredOutputObserved = true
	return observation, nil
}

func validateKimiK27RequiredReasoning(expectedModel string, raw json.RawMessage) error {
	if strings.TrimSpace(expectedModel) != "kimi-k2.7-code" {
		return nil
	}
	var reasoning string
	if json.Unmarshal(raw, &reasoning) != nil || strings.TrimSpace(reasoning) == "" {
		return invalidKimiChat("reasoning_content")
	}
	return nil
}

func ValidateKimiChatSSE(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (KimiChatObservation, error) {
	if probeContractID != KimiChatSSEProbeContractID ||
		(probeContractVersion != KimiChatCoreProbeContractVersion &&
			probeContractVersion != KimiChatSSEProbeContractVersionV2 &&
			probeContractVersion != KimiChatSSEProbeContractVersionV3 &&
			probeContractVersion != KimiChatSSEProbeContractVersionV4) {
		return KimiChatObservation{}, invalidKimiChat("probe_contract")
	}
	if safeReportedModel(strings.TrimSpace(expectedModel)) != strings.TrimSpace(expectedModel) || strings.TrimSpace(expectedModel) == "" {
		return KimiChatObservation{}, invalidKimiChat("expected_model")
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "text/event-stream" {
		return KimiChatObservation{}, invalidKimiChat("content_type")
	}
	limited := &io.LimitedReader{R: reader, N: KimiChatMaxResponseBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, 64*1024), KimiChatMaxSSELineBytes)
	scanner.Split(splitSSELines)
	var observation KimiChatObservation
	var content strings.Builder
	var dataLines []string
	finished, usageSeen, topLevelUsageSeen, usageOnlySeen := false, false, false, false
	recordUsage := func(raw json.RawMessage) error {
		usage, err := decodeKimiStreamUsage(raw, probeContractVersion)
		if err != nil {
			return err
		}
		var candidate KimiChatObservation
		if err := applyKimiUsage(&candidate, usage); err != nil {
			return err
		}
		if usageSeen {
			if candidate.PromptTokens != observation.PromptTokens ||
				candidate.CompletionTokens != observation.CompletionTokens ||
				candidate.TotalTokens != observation.TotalTokens ||
				candidate.CachedTokens != observation.CachedTokens {
				return invalidKimiChat("usage_conflict")
			}
			return nil
		}
		observation.PromptTokens = candidate.PromptTokens
		observation.CompletionTokens = candidate.CompletionTokens
		observation.TotalTokens = candidate.TotalTokens
		observation.CachedTokens = candidate.CachedTokens
		usageSeen = true
		return nil
	}
	observed := func() KimiChatObservation {
		observation.ResponseBytes = KimiChatMaxResponseBytes + 1 - int(limited.N)
		return observation
	}
	process := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		observation.DataEventCount++
		if payload == "[DONE]" {
			if observation.DoneSeen || !finished || !usageSeen || !topLevelUsageSeen {
				return invalidKimiChat("terminal_order")
			}
			observation.DoneSeen = true
			return nil
		}
		if observation.DoneSeen || usageOnlySeen {
			return invalidKimiChat("data_after_terminal")
		}
		if probeContractVersion == KimiChatSSEProbeContractVersionV2 ||
			probeContractVersion == KimiChatSSEProbeContractVersionV3 {
			normalized, _, normalizeErr := adapters.NormalizeKimiK26ChatStreamChunk(
				"kimi", expectedModel, "protocol.kimi-openai-chat.v5", []byte(payload),
			)
			if normalizeErr != nil {
				return invalidKimiChat("delivery_shape", normalizeErr)
			}
			payload = string(normalized)
		}
		if probeContractVersion == KimiChatSSEProbeContractVersionV4 {
			if validationErr := strictjson.Validate([]byte(payload)); validationErr != nil {
				return invalidKimiChat("json", validationErr)
			}
		}
		var chunk struct {
			ID                string `json:"id"`
			Object            string `json:"object"`
			Created           int64  `json:"created"`
			Model             string `json:"model"`
			SystemFingerprint string `json:"system_fingerprint,omitempty"`
			Choices           []struct {
				Index int `json:"index"`
				Delta struct {
					Role             string          `json:"role,omitempty"`
					Content          json.RawMessage `json:"content,omitempty"`
					ReasoningContent json.RawMessage `json:"reasoning_content,omitempty"`
				} `json:"delta"`
				FinishReason json.RawMessage `json:"finish_reason"`
				Logprobs     json.RawMessage `json:"logprobs,omitempty"`
				Usage        json.RawMessage `json:"usage,omitempty"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := decodeKimiJSONStrict([]byte(payload), &chunk); err != nil {
			return invalidKimiChat("json", err)
		}
		if safeReportedModel(chunk.Model) != expectedModel {
			return invalidKimiChat("reported_model")
		}
		observation.ReportedModel = chunk.Model
		if len(chunk.Choices) == 0 {
			if !finished || !kimiJSONValuePresent(chunk.Usage) {
				return invalidKimiChat("usage_order")
			}
			if err := recordUsage(chunk.Usage); err != nil {
				return err
			}
			topLevelUsageSeen = true
			usageOnlySeen = true
			return nil
		}
		if len(chunk.Choices) != 1 || finished {
			return invalidKimiChat("choices")
		}
		choice := chunk.Choices[0]
		observation.DeltaCount++
		if len(choice.Delta.ReasoningContent) != 0 && string(choice.Delta.ReasoningContent) != "null" {
			if probeContractVersion != KimiChatSSEProbeContractVersionV4 {
				return invalidKimiChat("reasoning_content")
			}
			var reasoning string
			if json.Unmarshal(choice.Delta.ReasoningContent, &reasoning) != nil {
				return invalidKimiChat("reasoning_content")
			}
			if strings.TrimSpace(reasoning) != "" {
				observation.ReasoningObserved = true
			}
		}
		if len(choice.Delta.Content) != 0 && string(choice.Delta.Content) != "null" {
			var delta string
			if json.Unmarshal(choice.Delta.Content, &delta) != nil {
				return invalidKimiChat("content")
			}
			content.WriteString(delta)
		}
		if len(choice.FinishReason) != 0 && string(choice.FinishReason) != "null" {
			if json.Unmarshal(choice.FinishReason, &observation.FinishReason) != nil || observation.FinishReason != "stop" {
				return invalidKimiChat("finish_reason")
			}
			finished = true
		}
		if len(choice.Usage) != 0 && probeContractVersion != KimiChatSSEProbeContractVersionV3 &&
			probeContractVersion != KimiChatSSEProbeContractVersionV4 {
			return invalidKimiChat("usage_order")
		}
		if kimiJSONValuePresent(choice.Usage) {
			if !finished {
				return invalidKimiChat("usage_order")
			}
			if err := recordUsage(choice.Usage); err != nil {
				return err
			}
		}
		if kimiJSONValuePresent(chunk.Usage) {
			if !finished {
				return invalidKimiChat("usage_order")
			}
			if err := recordUsage(chunk.Usage); err != nil {
				return err
			}
			topLevelUsageSeen = true
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := process(); err != nil {
				return observed(), err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok || field != "data" {
			return observed(), invalidKimiChat("unsupported_field")
		}
		dataLines = append(dataLines, strings.TrimPrefix(value, " "))
	}
	if err := scanner.Err(); err != nil {
		return observed(), invalidKimiChat("read", err)
	}
	if len(dataLines) != 0 {
		if err := process(); err != nil {
			return observed(), err
		}
	}
	if limited.N <= 0 {
		return observed(), invalidKimiChat("response_too_large")
	}
	if !observation.DoneSeen || strings.TrimSpace(content.String()) != "UNIROUTER_STREAM_PROBE_OK" {
		return observed(), invalidKimiChat("content")
	}
	if probeContractVersion == KimiChatSSEProbeContractVersionV4 && !observation.ReasoningObserved {
		return observed(), invalidKimiChat("reasoning_content")
	}
	return observed(), nil
}

func kimiJSONValuePresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) != 0 && !bytes.Equal(trimmed, []byte("null"))
}

func kimiJSONFieldPresent(raw json.RawMessage) bool {
	return len(bytes.TrimSpace(raw)) != 0
}

func decodeKimiStreamUsage(raw json.RawMessage, probeContractVersion string) (kimiChatUsage, error) {
	if !kimiJSONValuePresent(raw) {
		return kimiChatUsage{}, invalidKimiChat("usage")
	}
	if probeContractVersion != KimiChatSSEProbeContractVersionV3 &&
		probeContractVersion != KimiChatSSEProbeContractVersionV4 {
		var usage kimiChatUsage
		if err := decodeKimiJSONStrict(raw, &usage); err != nil {
			return kimiChatUsage{}, invalidKimiChat("usage", err)
		}
		return usage, nil
	}
	return decodeKimiDetailedUsage(raw)
}

func decodeKimiCompletionUsage(raw json.RawMessage, probeContractID, probeContractVersion string) (kimiChatUsage, error) {
	if !kimiJSONValuePresent(raw) {
		return kimiChatUsage{}, invalidKimiChat("usage")
	}
	if probeContractID == KimiChatReasoningEffortProbeContractID {
		if probeContractVersion != KimiChatReasoningEffortProbeContractVersion {
			return kimiChatUsage{}, invalidKimiChat("probe_contract")
		}
		return decodeKimiCoreV2CompletionUsage(raw)
	}
	if probeContractVersion == KimiChatCoreProbeContractVersion {
		var usage kimiChatUsage
		if err := decodeKimiJSONStrict(raw, &usage); err != nil {
			return kimiChatUsage{}, invalidKimiChat("json", err)
		}
		return usage, nil
	}
	if kimiChatFunctionToolsV3DeliveryShape(probeContractVersion) {
		return decodeKimiFunctionToolsV3Usage(raw)
	}
	if probeContractVersion != KimiChatCoreProbeContractVersionV2 {
		return kimiChatUsage{}, invalidKimiChat("probe_contract")
	}
	return decodeKimiCoreV2CompletionUsage(raw)
}

func decodeKimiCoreV2CompletionUsage(raw json.RawMessage) (kimiChatUsage, error) {
	usage, err := decodeKimiCoreV2Usage(raw)
	if err != nil {
		var validationErr *kimiChatValidationError
		if errors.As(err, &validationErr) && validationErr.code == "usage" {
			return kimiChatUsage{}, invalidKimiChat("json", err)
		}
	}
	return usage, err
}

// Function Tools Probe v3 aligns only the delivery-shape boundary with the
// public Chat runtime. Provider usage extensions are accepted when they remain
// a bounded JSON tree of integer counters, but only the explicitly modeled
// Kimi core/cache/reasoning counters below contribute to qualification
// observations. Unknown counters never become usage or billing authority.
func decodeKimiFunctionToolsV3Usage(raw json.RawMessage) (kimiChatUsage, error) {
	if err := strictjson.Validate(raw); err != nil {
		return kimiChatUsage{}, invalidKimiChat("json", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return kimiChatUsage{}, invalidKimiChat("json", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return kimiChatUsage{}, invalidKimiChat("json", err)
	}
	if !kimiUsageDeliveryShapeSupported(value) {
		return kimiChatUsage{}, invalidKimiChat("json")
	}
	var wire kimiChatCoreV2Usage
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&wire); err != nil {
		return kimiChatUsage{}, invalidKimiChat("json", err)
	}
	detailed := kimiChatStreamUsage{
		PromptTokens: wire.PromptTokens.jsonNumber(), CompletionTokens: wire.CompletionTokens.jsonNumber(),
		TotalTokens: wire.TotalTokens.jsonNumber(), CachedTokens: kimiJSONNumberPointer(wire.CachedTokens),
	}
	if wire.PromptDetails != nil {
		detailed.PromptDetails = &struct {
			CachedTokens *json.Number `json:"cached_tokens,omitempty"`
		}{CachedTokens: kimiJSONNumberPointer(wire.PromptDetails.CachedTokens)}
	}
	if wire.CompletionDetails != nil {
		detailed.CompletionDetails = &struct {
			ReasoningTokens *json.Number `json:"reasoning_tokens,omitempty"`
		}{ReasoningTokens: kimiJSONNumberPointer(wire.CompletionDetails.ReasoningTokens)}
	}
	return validateKimiDetailedUsage(detailed)
}

func kimiUsageDeliveryShapeSupported(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case json.Number:
		_, err := typed.Int64()
		return err == nil
	case map[string]interface{}:
		for _, nested := range typed {
			if !kimiUsageDeliveryShapeSupported(nested) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func decodeKimiDetailedUsage(raw json.RawMessage) (kimiChatUsage, error) {
	var detailed kimiChatStreamUsage
	if err := decodeKimiJSONStrict(raw, &detailed); err != nil {
		return kimiChatUsage{}, invalidKimiChat("usage", err)
	}
	return validateKimiDetailedUsage(detailed)
}

func decodeKimiCoreV2Usage(raw json.RawMessage) (kimiChatUsage, error) {
	var wire kimiChatCoreV2Usage
	if err := decodeKimiJSONStrict(raw, &wire); err != nil {
		return kimiChatUsage{}, invalidKimiChat("usage", err)
	}
	detailed := kimiChatStreamUsage{
		PromptTokens: wire.PromptTokens.jsonNumber(), CompletionTokens: wire.CompletionTokens.jsonNumber(),
		TotalTokens: wire.TotalTokens.jsonNumber(), CachedTokens: kimiJSONNumberPointer(wire.CachedTokens),
	}
	if wire.PromptDetails != nil {
		detailed.PromptDetails = &struct {
			CachedTokens *json.Number `json:"cached_tokens,omitempty"`
		}{CachedTokens: kimiJSONNumberPointer(wire.PromptDetails.CachedTokens)}
	}
	if wire.CompletionDetails != nil {
		detailed.CompletionDetails = &struct {
			ReasoningTokens *json.Number `json:"reasoning_tokens,omitempty"`
		}{ReasoningTokens: kimiJSONNumberPointer(wire.CompletionDetails.ReasoningTokens)}
	}
	return validateKimiDetailedUsage(detailed)
}

func kimiJSONNumberPointer(number *kimiJSONNumber) *json.Number {
	if number == nil {
		return nil
	}
	value := number.jsonNumber()
	return &value
}

func validateKimiDetailedUsage(detailed kimiChatStreamUsage) (kimiChatUsage, error) {
	usage := kimiChatUsage{
		PromptTokens: detailed.PromptTokens, CompletionTokens: detailed.CompletionTokens,
		TotalTokens: detailed.TotalTokens, CachedTokens: detailed.CachedTokens,
	}
	if detailed.PromptDetails != nil && detailed.PromptDetails.CachedTokens != nil {
		detailCached, err := parseKimiNonnegativeInteger(*detailed.PromptDetails.CachedTokens)
		if err != nil {
			return kimiChatUsage{}, invalidKimiChat("cached_tokens", err)
		}
		if usage.CachedTokens == nil {
			return kimiChatUsage{}, invalidKimiChat("cached_tokens")
		}
		topCached, err := parseKimiNonnegativeInteger(*usage.CachedTokens)
		if err != nil {
			return kimiChatUsage{}, invalidKimiChat("cached_tokens", err)
		}
		if topCached != detailCached {
			return kimiChatUsage{}, invalidKimiChat("cached_tokens_conflict")
		}
	}
	if detailed.CompletionDetails != nil && detailed.CompletionDetails.ReasoningTokens != nil {
		reasoningTokens, err := parseKimiNonnegativeInteger(*detailed.CompletionDetails.ReasoningTokens)
		if err != nil {
			return kimiChatUsage{}, invalidKimiChat("reasoning_tokens", err)
		}
		completionTokens, err := parseKimiNonnegativeInteger(detailed.CompletionTokens)
		if err != nil || reasoningTokens > completionTokens {
			return kimiChatUsage{}, invalidKimiChat("reasoning_tokens", err)
		}
	}
	return usage, nil
}

func readKimiChatCompletion(binding Binding, contentType string, reader io.Reader) (kimiChatCompletion, KimiChatObservation, error) {
	expectedModel := strings.TrimSpace(binding.ExpectedUpstreamModel)
	if expectedModel == "" || safeReportedModel(expectedModel) != expectedModel {
		return kimiChatCompletion{}, KimiChatObservation{}, invalidKimiChat("expected_model")
	}
	mechanics, err := kimiChatNonStreamMechanics(binding)
	if err != nil {
		return kimiChatCompletion{}, KimiChatObservation{}, err
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return kimiChatCompletion{}, KimiChatObservation{}, invalidKimiChat("content_type")
	}
	limited := &io.LimitedReader{R: reader, N: KimiChatMaxResponseBytes + 1}
	body, err := io.ReadAll(limited)
	observation := KimiChatObservation{ResponseBytes: len(body)}
	if err != nil {
		return kimiChatCompletion{}, observation, invalidKimiChat("read", err)
	}
	if limited.N <= 0 {
		return kimiChatCompletion{}, observation, invalidKimiChat("response_too_large")
	}
	if err := adapters.ValidateOpenAICompatibleChatNonStreamDocument(body, mechanics); err != nil {
		return kimiChatCompletion{}, observation, invalidKimiChat("json", err)
	}
	prepared, prepareErr := adapters.PrepareOpenAICompatibleChatNonStreamDocument(
		binding.ExpectedProvider, expectedModel, binding.ProtocolProfileID,
		binding.ProtocolProfileVersion, body, mechanics,
	)
	if prepareErr != nil {
		return kimiChatCompletion{}, observation, invalidKimiChat("probe_contract", prepareErr)
	}
	if prepared.DeliveryIssue != adapters.ChatNonStreamDeliveryIssueNone {
		return kimiChatCompletion{}, observation, invalidKimiChat("delivery_shape")
	}
	body = prepared.Body
	var payload kimiChatCompletion
	if err := decodeKimiJSONStrict(body, &payload); err != nil {
		return kimiChatCompletion{}, observation, invalidKimiChat("json", err)
	}
	if !kimiChatFunctionToolsV3DeliveryShape(binding.ProbeContractVersion) {
		if kimiJSONFieldPresent(payload.ServiceTier) || kimiJSONFieldPresent(payload.Metadata) {
			return kimiChatCompletion{}, observation, invalidKimiChat("json")
		}
		for _, choice := range payload.Choices {
			if kimiJSONFieldPresent(choice.Text) || kimiJSONFieldPresent(choice.Message.Reasoning) || kimiJSONFieldPresent(choice.Message.Refusal) {
				return kimiChatCompletion{}, observation, invalidKimiChat("json")
			}
		}
	} else if err := validateKimiFunctionToolsV3DeliveryFields(payload); err != nil {
		return kimiChatCompletion{}, observation, err
	}
	observation.ReportedModel = safeReportedModel(payload.Model)
	if observation.ReportedModel != expectedModel {
		return payload, observation, invalidKimiChat("reported_model")
	}
	usage, err := decodeKimiCompletionUsage(payload.Usage, binding.ProbeContractID, binding.ProbeContractVersion)
	if err != nil {
		return payload, observation, err
	}
	if err := applyKimiUsage(&observation, usage); err != nil {
		return payload, observation, err
	}
	return payload, observation, nil
}

func validateKimiFunctionToolsV3DeliveryFields(payload kimiChatCompletion) error {
	if kimiJSONValuePresent(payload.ServiceTier) {
		var value string
		if json.Unmarshal(payload.ServiceTier, &value) != nil {
			return invalidKimiChat("delivery_shape")
		}
	}
	for _, choice := range payload.Choices {
		if kimiJSONValuePresent(choice.Text) {
			return invalidKimiChat("delivery_shape")
		}
		for _, field := range []json.RawMessage{
			choice.Message.Content,
			choice.Message.ReasoningContent,
			choice.Message.Reasoning,
			choice.Message.Refusal,
		} {
			if !kimiJSONValuePresent(field) {
				continue
			}
			var value string
			if json.Unmarshal(field, &value) != nil {
				return invalidKimiChat("delivery_shape")
			}
		}
	}
	return nil
}

func validateKimiSingleChoice(payload kimiChatCompletion, observation *KimiChatObservation, finishReason string) (*kimiChatChoice, error) {
	if len(payload.Choices) != 1 || payload.Choices[0].FinishReason != finishReason {
		return nil, invalidKimiChat("finish_reason")
	}
	observation.FinishReason = payload.Choices[0].FinishReason
	return &payload.Choices[0], nil
}

func applyKimiUsage(observation *KimiChatObservation, usage kimiChatUsage) error {
	values := []json.Number{usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens}
	parsed := make([]int64, len(values))
	for index, value := range values {
		integer, err := parseKimiNonnegativeInteger(value)
		if err != nil {
			return invalidKimiChat("usage", err)
		}
		parsed[index] = integer
	}
	if parsed[0] <= 0 || parsed[1] <= 0 || parsed[0]+parsed[1] != parsed[2] {
		return invalidKimiChat("usage_total")
	}
	observation.PromptTokens = parsed[0]
	observation.CompletionTokens = parsed[1]
	observation.TotalTokens = parsed[2]
	if usage.CachedTokens != nil {
		cached, err := parseKimiNonnegativeInteger(*usage.CachedTokens)
		if err != nil || cached > observation.PromptTokens {
			return invalidKimiChat("cached_tokens", err)
		}
		observation.CachedTokens = cached
	}
	return nil
}

func parseKimiNonnegativeInteger(value json.Number) (int64, error) {
	if value == "" || strings.ContainsAny(string(value), ".eE+") {
		return 0, errors.New("not an integer")
	}
	integer, err := strconv.ParseInt(string(value), 10, 64)
	if err != nil || integer < 0 {
		return 0, errors.New("not a nonnegative integer")
	}
	return integer, nil
}

func decodeKimiJSONStrict(body []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
