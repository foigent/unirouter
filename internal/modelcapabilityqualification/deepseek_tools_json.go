package modelcapabilityqualification

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/strictjson"
)

const DeepSeekCoreMaxResponseBytes = 1 << 20

var ErrInvalidDeepSeekCoreCapability = errors.New("invalid DeepSeek Chat core capability probe response")

type deepSeekCoreValidationError struct {
	code  string
	cause error
}

func (e *deepSeekCoreValidationError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", ErrInvalidDeepSeekCoreCapability, e.code, e.cause)
	}
	return fmt.Sprintf("%s: %s", ErrInvalidDeepSeekCoreCapability, e.code)
}

func (e *deepSeekCoreValidationError) Unwrap() error { return ErrInvalidDeepSeekCoreCapability }

func invalidDeepSeekCore(code string, cause ...error) error {
	err := &deepSeekCoreValidationError{code: code}
	if len(cause) > 0 {
		err.cause = cause[0]
	}
	return err
}

func DeepSeekCoreErrorCode(err error) string {
	var validationErr *deepSeekCoreValidationError
	if errors.As(err, &validationErr) {
		return validationErr.code
	}
	return ""
}

type DeepSeekCoreObservation struct {
	ReportedModel            string
	FinishReason             string
	PromptTokens             int64
	CompletionTokens         int64
	TotalTokens              int64
	ToolCallObserved         bool
	StructuredOutputObserved bool
	ResponseBytes            int
}

func ValidateDeepSeekChatFunctionTools(
	probeContractID, probeContractVersion, contentType string,
	reader io.Reader,
) (DeepSeekCoreObservation, error) {
	return ValidateDeepSeekChatFunctionToolsForModel(
		probeContractID, probeContractVersion, "deepseek-v4-flash", contentType, reader,
	)
}

// ValidateDeepSeekChatFunctionToolsForModel applies the shared DeepSeek Chat
// function-tool grammar to one exact binding-owned upstream model.
func ValidateDeepSeekChatFunctionToolsForModel(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (DeepSeekCoreObservation, error) {
	expectedModel = strings.TrimSpace(expectedModel)
	if expectedModel == "" || safeReportedModel(expectedModel) != expectedModel {
		return DeepSeekCoreObservation{}, invalidDeepSeekCore("expected_model")
	}
	if probeContractID != DeepSeekChatFunctionToolsProbeContractID ||
		(probeContractVersion != DeepSeekChatCoreProbeContractVersion &&
			probeContractVersion != DeepSeekChatFunctionToolsProbeContractV2) {
		return DeepSeekCoreObservation{}, invalidDeepSeekCore("probe_contract")
	}
	body, observation, err := readDeepSeekCoreResponse(contentType, reader)
	if err != nil {
		return observation, err
	}
	if probeContractVersion == DeepSeekChatFunctionToolsProbeContractV2 {
		normalized, _, err := adapters.NormalizeDeepSeekV4ChatToolResponse(
			"deepseek", expectedModel, "protocol.deepseek-openai-chat.v4", body,
		)
		if err != nil {
			return observation, invalidDeepSeekCore("delivery_shape", err)
		}
		body = normalized
	}
	var payload struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := decodeOneJSON(body, &payload); err != nil {
		return observation, invalidDeepSeekCore("json", err)
	}
	observation.ReportedModel = safeReportedModel(payload.Model)
	if observation.ReportedModel != expectedModel {
		return observation, invalidDeepSeekCore("reported_model")
	}
	if len(payload.Choices) != 1 {
		return observation, invalidDeepSeekCore("choices")
	}
	choice := payload.Choices[0]
	observation.FinishReason = choice.FinishReason
	if observation.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) != 1 {
		return observation, invalidDeepSeekCore("tool_calls")
	}
	call := choice.Message.ToolCalls[0]
	if strings.TrimSpace(call.ID) == "" || call.Type != "function" || call.Function.Name != "unirouter_probe_echo" {
		return observation, invalidDeepSeekCore("tool_call_identity")
	}
	arguments := []byte(call.Function.Arguments)
	if err := strictjson.Validate(arguments); err != nil {
		return observation, invalidDeepSeekCore("tool_arguments", err)
	}
	var decodedArguments map[string]interface{}
	if err := decodeOneJSON(arguments, &decodedArguments); err != nil || len(decodedArguments) != 1 || decodedArguments["value"] != "UNIROUTER_TOOL_PROBE_OK" {
		return observation, invalidDeepSeekCore("tool_arguments")
	}
	observation.ToolCallObserved = true
	if err := applyDeepSeekCoreUsage(&observation, payload.Usage); err != nil {
		return observation, err
	}
	return observation, nil
}

func ValidateDeepSeekChatJSONObject(
	probeContractID, probeContractVersion, contentType string,
	reader io.Reader,
) (DeepSeekCoreObservation, error) {
	return ValidateDeepSeekChatJSONObjectForModel(
		probeContractID, probeContractVersion, "deepseek-v4-flash", contentType, reader,
	)
}

// ValidateDeepSeekChatJSONObjectForModel validates one exact binding-owned
// DeepSeek model without creating a model-specific JSON probe contract.
func ValidateDeepSeekChatJSONObjectForModel(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (DeepSeekCoreObservation, error) {
	expectedModel = strings.TrimSpace(expectedModel)
	if expectedModel == "" || safeReportedModel(expectedModel) != expectedModel {
		return DeepSeekCoreObservation{}, invalidDeepSeekCore("expected_model")
	}
	if probeContractID != DeepSeekChatJSONObjectProbeContractID ||
		probeContractVersion != DeepSeekChatCoreProbeContractVersion {
		return DeepSeekCoreObservation{}, invalidDeepSeekCore("probe_contract")
	}
	body, observation, err := readDeepSeekCoreResponse(contentType, reader)
	if err != nil {
		return observation, err
	}
	var payload struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := decodeOneJSON(body, &payload); err != nil {
		return observation, invalidDeepSeekCore("json", err)
	}
	observation.ReportedModel = safeReportedModel(payload.Model)
	if observation.ReportedModel != expectedModel {
		return observation, invalidDeepSeekCore("reported_model")
	}
	if len(payload.Choices) != 1 || payload.Choices[0].FinishReason != "stop" {
		return observation, invalidDeepSeekCore("finish_reason")
	}
	observation.FinishReason = payload.Choices[0].FinishReason
	content := []byte(strings.TrimSpace(payload.Choices[0].Message.Content))
	if err := strictjson.Validate(content); err != nil {
		return observation, invalidDeepSeekCore("structured_output", err)
	}
	var object map[string]interface{}
	if err := decodeOneJSON(content, &object); err != nil || len(object) != 1 || object["status"] != "UNIROUTER_JSON_PROBE_OK" {
		return observation, invalidDeepSeekCore("structured_output")
	}
	observation.StructuredOutputObserved = true
	if err := applyDeepSeekCoreUsage(&observation, payload.Usage); err != nil {
		return observation, err
	}
	return observation, nil
}

func readDeepSeekCoreResponse(contentType string, reader io.Reader) ([]byte, DeepSeekCoreObservation, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return nil, DeepSeekCoreObservation{}, invalidDeepSeekCore("content_type")
	}
	limited := &io.LimitedReader{R: reader, N: DeepSeekCoreMaxResponseBytes + 1}
	body, err := io.ReadAll(limited)
	observation := DeepSeekCoreObservation{ResponseBytes: len(body)}
	if err != nil {
		return nil, observation, invalidDeepSeekCore("read", err)
	}
	if limited.N <= 0 {
		return nil, observation, invalidDeepSeekCore("response_too_large")
	}
	if err := strictjson.Validate(body); err != nil {
		return nil, observation, invalidDeepSeekCore("json", err)
	}
	return body, observation, nil
}

func decodeOneJSON(body []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
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

func applyDeepSeekCoreUsage(observation *DeepSeekCoreObservation, raw json.RawMessage) error {
	prompt, completion, total, err := parseDeepSeekReasoningUsage(raw)
	if err != nil {
		return invalidDeepSeekCore("usage", err)
	}
	observation.PromptTokens = prompt
	observation.CompletionTokens = completion
	observation.TotalTokens = total
	return nil
}
