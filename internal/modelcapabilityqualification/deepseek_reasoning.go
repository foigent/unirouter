package modelcapabilityqualification

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/strictjson"
)

const DeepSeekReasoningMaxResponseBytes = 1 << 20

var ErrInvalidDeepSeekReasoning = errors.New("invalid DeepSeek Chat reasoning probe response")

type deepSeekReasoningValidationError struct {
	code  string
	cause error
}

func (e *deepSeekReasoningValidationError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", ErrInvalidDeepSeekReasoning, e.code, e.cause)
	}
	return fmt.Sprintf("%s: %s", ErrInvalidDeepSeekReasoning, e.code)
}

func (e *deepSeekReasoningValidationError) Unwrap() error { return ErrInvalidDeepSeekReasoning }

func invalidDeepSeekReasoning(code string, cause ...error) error {
	err := &deepSeekReasoningValidationError{code: code}
	if len(cause) > 0 {
		err.cause = cause[0]
	}
	return err
}

func DeepSeekReasoningErrorCode(err error) string {
	var validationErr *deepSeekReasoningValidationError
	if errors.As(err, &validationErr) {
		return validationErr.code
	}
	return ""
}

type DeepSeekReasoningObservation struct {
	ReportedModel    string
	FinishReason     string
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	ReasoningSeen    bool
	ResponseBytes    int
}

// ValidateDeepSeekChatReasoning proves the bounded non-stream response shape
// used by both the thinking toggle and reasoning-effort probes. It records only
// the presence of reasoning_content; private reasoning text is never returned
// to the caller or persisted in a probe receipt.
func ValidateDeepSeekChatReasoning(
	probeContractID, probeContractVersion, contentType string,
	reader io.Reader,
) (DeepSeekReasoningObservation, error) {
	return ValidateDeepSeekChatReasoningForModel(
		probeContractID, probeContractVersion, "deepseek-v4-flash", contentType, reader,
	)
}

// ValidateDeepSeekChatReasoningForModel validates one exact binding-owned
// DeepSeek model without changing the shared reasoning probe grammar.
func ValidateDeepSeekChatReasoningForModel(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (DeepSeekReasoningObservation, error) {
	expectedModel = strings.TrimSpace(expectedModel)
	if expectedModel == "" || safeReportedModel(expectedModel) != expectedModel {
		return DeepSeekReasoningObservation{}, invalidDeepSeekReasoning("expected_model")
	}
	if (probeContractID != DeepSeekChatReasoningProbeContractID &&
		probeContractID != DeepSeekChatReasoningEffortProbeContractID) ||
		probeContractVersion != DeepSeekChatReasoningProbeContractVersion {
		return DeepSeekReasoningObservation{}, invalidDeepSeekReasoning("probe_contract")
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return DeepSeekReasoningObservation{}, invalidDeepSeekReasoning("content_type")
	}
	limited := &io.LimitedReader{R: reader, N: DeepSeekReasoningMaxResponseBytes + 1}
	body, err := io.ReadAll(limited)
	observation := DeepSeekReasoningObservation{ResponseBytes: len(body)}
	if err != nil {
		return observation, invalidDeepSeekReasoning("read", err)
	}
	if limited.N <= 0 {
		return observation, invalidDeepSeekReasoning("response_too_large")
	}
	if err := strictjson.Validate(body); err != nil {
		return observation, invalidDeepSeekReasoning("json", err)
	}
	var payload struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason json.RawMessage `json:"finish_reason"`
			Message      struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return observation, invalidDeepSeekReasoning("json")
	}
	observation.ReportedModel = safeReportedModel(payload.Model)
	if observation.ReportedModel != expectedModel {
		return observation, invalidDeepSeekReasoning("reported_model")
	}
	if len(payload.Choices) != 1 {
		return observation, invalidDeepSeekReasoning("choices")
	}
	choice := payload.Choices[0]
	if json.Unmarshal(choice.FinishReason, &observation.FinishReason) != nil || observation.FinishReason != "stop" {
		return observation, invalidDeepSeekReasoning("finish_reason")
	}
	var content string
	if json.Unmarshal(choice.Message.Content, &content) != nil || strings.TrimSpace(content) != "UNIROUTER_REASONING_PROBE_OK" {
		return observation, invalidDeepSeekReasoning("content")
	}
	var reasoning string
	if json.Unmarshal(choice.Message.ReasoningContent, &reasoning) != nil || strings.TrimSpace(reasoning) == "" {
		return observation, invalidDeepSeekReasoning("reasoning_content")
	}
	observation.ReasoningSeen = true
	prompt, completion, total, usageErr := parseDeepSeekReasoningUsage(payload.Usage)
	if usageErr != nil {
		return observation, usageErr
	}
	observation.PromptTokens = prompt
	observation.CompletionTokens = completion
	observation.TotalTokens = total
	return observation, nil
}

func parseDeepSeekReasoningUsage(raw json.RawMessage) (int64, int64, int64, error) {
	var usage struct {
		PromptTokens     json.Number `json:"prompt_tokens"`
		CompletionTokens json.Number `json:"completion_tokens"`
		TotalTokens      json.Number `json:"total_tokens"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&usage) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return 0, 0, 0, invalidDeepSeekReasoning("usage")
	}
	values := []json.Number{usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens}
	parsed := make([]int64, len(values))
	for index, value := range values {
		if value == "" || strings.ContainsAny(string(value), ".eE+") {
			return 0, 0, 0, invalidDeepSeekReasoning("usage")
		}
		integer, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil || integer < 0 {
			return 0, 0, 0, invalidDeepSeekReasoning("usage")
		}
		parsed[index] = integer
	}
	if parsed[0] <= 0 || parsed[1] <= 0 || parsed[0]+parsed[1] != parsed[2] {
		return 0, 0, 0, invalidDeepSeekReasoning("usage_total")
	}
	return parsed[0], parsed[1], parsed[2], nil
}
