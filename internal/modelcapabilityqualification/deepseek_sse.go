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
	"unicode/utf8"
)

const (
	DeepSeekSSEMaxResponseBytes = 1 << 20
	DeepSeekSSEMaxLineBytes     = 256 << 10
)

var ErrInvalidDeepSeekSSE = errors.New("invalid DeepSeek Chat SSE probe response")

type DeepSeekSSEObservation struct {
	ReportedModel     string
	DataEventCount    int
	DeltaCount        int
	ContentDeltaCount int
	FinishReason      string
	PromptTokens      int64
	CompletionTokens  int64
	TotalTokens       int64
	UsageSeen         bool
	DoneSeen          bool
	ResponseBytes     int
}

func ValidateDeepSeekChatSSE(contentType string, reader io.Reader) (DeepSeekSSEObservation, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "text/event-stream" {
		return DeepSeekSSEObservation{}, fmt.Errorf("%w: content_type", ErrInvalidDeepSeekSSE)
	}
	limited := &io.LimitedReader{R: reader, N: DeepSeekSSEMaxResponseBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, 64*1024), DeepSeekSSEMaxLineBytes)
	var observation DeepSeekSSEObservation
	var dataLines []string
	process := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		observation.DataEventCount++
		if payload == "[DONE]" {
			if observation.DoneSeen || !observation.UsageSeen || observation.FinishReason == "" {
				return fmt.Errorf("%w: terminal_order", ErrInvalidDeepSeekSSE)
			}
			observation.DoneSeen = true
			return nil
		}
		if observation.DoneSeen {
			return fmt.Errorf("%w: data_after_done", ErrInvalidDeepSeekSSE)
		}
		return validateDeepSeekChunk([]byte(payload), &observation)
	}
	for scanner.Scan() {
		line := scanner.Text()
		observation.ResponseBytes += len(scanner.Bytes()) + 1
		if line == "" {
			if err := process(); err != nil {
				return DeepSeekSSEObservation{}, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok || field != "data" {
			return DeepSeekSSEObservation{}, fmt.Errorf("%w: unsupported_field", ErrInvalidDeepSeekSSE)
		}
		if strings.HasPrefix(value, " ") {
			value = value[1:]
		}
		dataLines = append(dataLines, value)
	}
	if err := scanner.Err(); err != nil {
		return DeepSeekSSEObservation{}, fmt.Errorf("%w: read: %v", ErrInvalidDeepSeekSSE, err)
	}
	if limited.N <= 0 {
		return DeepSeekSSEObservation{}, fmt.Errorf("%w: response_too_large", ErrInvalidDeepSeekSSE)
	}
	if err := process(); err != nil {
		return DeepSeekSSEObservation{}, err
	}
	if !observation.DoneSeen || !observation.UsageSeen || observation.FinishReason == "" || observation.ContentDeltaCount == 0 {
		return DeepSeekSSEObservation{}, fmt.Errorf("%w: incomplete", ErrInvalidDeepSeekSSE)
	}
	return observation, nil
}

func validateDeepSeekChunk(payload []byte, observation *DeepSeekSSEObservation) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var chunk struct {
		Model   string          `json:"model"`
		Choices json.RawMessage `json:"choices"`
		Usage   json.RawMessage `json:"usage"`
	}
	if err := decoder.Decode(&chunk); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("%w: json", ErrInvalidDeepSeekSSE)
	}
	if model := safeReportedModel(chunk.Model); model != "" {
		if observation.ReportedModel != "" && observation.ReportedModel != model {
			return fmt.Errorf("%w: model_changed", ErrInvalidDeepSeekSSE)
		}
		observation.ReportedModel = model
	}
	var choices []struct {
		Delta        json.RawMessage `json:"delta"`
		FinishReason json.RawMessage `json:"finish_reason"`
	}
	if len(chunk.Choices) == 0 || json.Unmarshal(chunk.Choices, &choices) != nil {
		return fmt.Errorf("%w: choices", ErrInvalidDeepSeekSSE)
	}
	if len(choices) == 0 {
		if observation.UsageSeen || len(chunk.Usage) == 0 || bytes.Equal(bytes.TrimSpace(chunk.Usage), []byte("null")) {
			return fmt.Errorf("%w: final_usage", ErrInvalidDeepSeekSSE)
		}
		prompt, completion, total, err := parseDeepSeekUsage(chunk.Usage)
		if err != nil {
			return err
		}
		observation.PromptTokens = prompt
		observation.CompletionTokens = completion
		observation.TotalTokens = total
		observation.UsageSeen = true
		return nil
	}
	if len(chunk.Usage) > 0 && !bytes.Equal(bytes.TrimSpace(chunk.Usage), []byte("null")) {
		return fmt.Errorf("%w: usage_mixed_with_choices", ErrInvalidDeepSeekSSE)
	}
	for _, choice := range choices {
		var delta map[string]json.RawMessage
		if len(choice.Delta) == 0 || json.Unmarshal(choice.Delta, &delta) != nil || delta == nil {
			return fmt.Errorf("%w: delta", ErrInvalidDeepSeekSSE)
		}
		observation.DeltaCount++
		if raw, exists := delta["content"]; exists && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			var content string
			if json.Unmarshal(raw, &content) != nil {
				return fmt.Errorf("%w: content", ErrInvalidDeepSeekSSE)
			}
			if content != "" {
				observation.ContentDeltaCount++
			}
		}
		if len(choice.FinishReason) > 0 && !bytes.Equal(bytes.TrimSpace(choice.FinishReason), []byte("null")) {
			var finish string
			if json.Unmarshal(choice.FinishReason, &finish) != nil || strings.TrimSpace(finish) == "" || strings.ContainsAny(finish, "\x00\r\n") {
				return fmt.Errorf("%w: finish_reason", ErrInvalidDeepSeekSSE)
			}
			if observation.FinishReason != "" && observation.FinishReason != finish {
				return fmt.Errorf("%w: finish_reason_changed", ErrInvalidDeepSeekSSE)
			}
			observation.FinishReason = finish
		}
	}
	return nil
}

func parseDeepSeekUsage(raw json.RawMessage) (int64, int64, int64, error) {
	var usage struct {
		PromptTokens     json.Number `json:"prompt_tokens"`
		CompletionTokens json.Number `json:"completion_tokens"`
		TotalTokens      json.Number `json:"total_tokens"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&usage) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return 0, 0, 0, fmt.Errorf("%w: usage", ErrInvalidDeepSeekSSE)
	}
	values := []json.Number{usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens}
	parsed := make([]int64, len(values))
	for index, value := range values {
		if value == "" || strings.ContainsAny(string(value), ".eE+") {
			return 0, 0, 0, fmt.Errorf("%w: usage", ErrInvalidDeepSeekSSE)
		}
		integer, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil || integer < 0 {
			return 0, 0, 0, fmt.Errorf("%w: usage", ErrInvalidDeepSeekSSE)
		}
		parsed[index] = integer
	}
	if parsed[0]+parsed[1] != parsed[2] {
		return 0, 0, 0, fmt.Errorf("%w: usage_total", ErrInvalidDeepSeekSSE)
	}
	return parsed[0], parsed[1], parsed[2], nil
}

func safeReportedModel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}
