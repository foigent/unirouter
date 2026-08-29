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

type kimiChatUsage struct {
	PromptTokens     json.Number  `json:"prompt_tokens"`
	CompletionTokens json.Number  `json:"completion_tokens"`
	TotalTokens      json.Number  `json:"total_tokens"`
	CachedTokens     *json.Number `json:"cached_tokens,omitempty"`
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
	ToolCalls        []kimiChatToolCall `json:"tool_calls,omitempty"`
}

type kimiChatCompletion struct {
	ID                string `json:"id"`
	Object            string `json:"object"`
	Created           int64  `json:"created"`
	Model             string `json:"model"`
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
	Choices           []struct {
		Index        int             `json:"index"`
		Message      kimiChatMessage `json:"message"`
		FinishReason string          `json:"finish_reason"`
		Logprobs     json.RawMessage `json:"logprobs,omitempty"`
	} `json:"choices"`
	Usage kimiChatUsage `json:"usage"`
}

func ValidateKimiChatReasoning(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (KimiChatObservation, error) {
	if probeContractID != KimiChatReasoningProbeContractID || probeContractVersion != KimiChatCoreProbeContractVersion {
		return KimiChatObservation{}, invalidKimiChat("probe_contract")
	}
	payload, observation, err := readKimiChatCompletion(expectedModel, contentType, reader)
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
	if probeContractID != KimiChatFunctionToolsProbeContractID || probeContractVersion != KimiChatCoreProbeContractVersion {
		return KimiChatObservation{}, invalidKimiChat("probe_contract")
	}
	payload, observation, err := readKimiChatCompletion(expectedModel, contentType, reader)
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
	call := choice.Message.ToolCalls[0]
	if strings.TrimSpace(call.ID) == "" || call.Type != "function" || call.Function.Name != "unirouter_probe_echo" {
		return observation, invalidKimiChat("tool_call_identity")
	}
	var arguments struct {
		Value string `json:"value"`
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
	if probeContractID != KimiChatJSONObjectProbeContractID || probeContractVersion != KimiChatCoreProbeContractVersion {
		return KimiChatObservation{}, invalidKimiChat("probe_contract")
	}
	payload, observation, err := readKimiChatCompletion(expectedModel, contentType, reader)
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
	observation.StructuredOutputObserved = true
	return observation, nil
}

func ValidateKimiChatSSE(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (KimiChatObservation, error) {
	if probeContractID != KimiChatSSEProbeContractID ||
		(probeContractVersion != KimiChatCoreProbeContractVersion &&
			probeContractVersion != KimiChatSSEProbeContractVersionV2) {
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
	finished, usageSeen := false, false
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
			if observation.DoneSeen || !finished || !usageSeen {
				return invalidKimiChat("terminal_order")
			}
			observation.DoneSeen = true
			return nil
		}
		if observation.DoneSeen || usageSeen {
			return invalidKimiChat("data_after_terminal")
		}
		if probeContractVersion == KimiChatSSEProbeContractVersionV2 {
			normalized, _, normalizeErr := adapters.NormalizeKimiK26ChatStreamChunk(
				"kimi", expectedModel, "protocol.kimi-openai-chat.v5", []byte(payload),
			)
			if normalizeErr != nil {
				return invalidKimiChat("delivery_shape", normalizeErr)
			}
			payload = string(normalized)
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
			} `json:"choices"`
			Usage *kimiChatUsage `json:"usage"`
		}
		if err := decodeKimiJSONStrict([]byte(payload), &chunk); err != nil {
			return invalidKimiChat("json", err)
		}
		if safeReportedModel(chunk.Model) != expectedModel {
			return invalidKimiChat("reported_model")
		}
		observation.ReportedModel = chunk.Model
		if len(chunk.Choices) == 0 {
			if !finished || chunk.Usage == nil {
				return invalidKimiChat("usage_order")
			}
			if err := applyKimiUsage(&observation, *chunk.Usage); err != nil {
				return err
			}
			usageSeen = true
			return nil
		}
		if len(chunk.Choices) != 1 || finished {
			return invalidKimiChat("choices")
		}
		choice := chunk.Choices[0]
		observation.DeltaCount++
		if len(choice.Delta.ReasoningContent) != 0 && string(choice.Delta.ReasoningContent) != "null" {
			return invalidKimiChat("reasoning_content")
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
		if chunk.Usage != nil {
			if !finished {
				return invalidKimiChat("usage_order")
			}
			if err := applyKimiUsage(&observation, *chunk.Usage); err != nil {
				return err
			}
			usageSeen = true
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
	return observed(), nil
}

func readKimiChatCompletion(expectedModel, contentType string, reader io.Reader) (kimiChatCompletion, KimiChatObservation, error) {
	expectedModel = strings.TrimSpace(expectedModel)
	if expectedModel == "" || safeReportedModel(expectedModel) != expectedModel {
		return kimiChatCompletion{}, KimiChatObservation{}, invalidKimiChat("expected_model")
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
	var payload kimiChatCompletion
	if err := decodeKimiJSONStrict(body, &payload); err != nil {
		return kimiChatCompletion{}, observation, invalidKimiChat("json", err)
	}
	observation.ReportedModel = safeReportedModel(payload.Model)
	if observation.ReportedModel != expectedModel {
		return payload, observation, invalidKimiChat("reported_model")
	}
	if err := applyKimiUsage(&observation, payload.Usage); err != nil {
		return payload, observation, err
	}
	return payload, observation, nil
}

func validateKimiSingleChoice(payload kimiChatCompletion, observation *KimiChatObservation, finishReason string) (*struct {
	Index        int             `json:"index"`
	Message      kimiChatMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs,omitempty"`
}, error) {
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
