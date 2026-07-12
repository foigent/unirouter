package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/metering"
)

const defaultAnthropicVersion = "2023-06-01"

type AnthropicNative struct{}

func NewAnthropicNative() *AnthropicNative {
	return &AnthropicNative{}
}

func (a *AnthropicNative) Type() string {
	return TypeAnthropicNative
}

func (a *AnthropicNative) BuildRequest(ctx context.Context, in RequestInput) (*http.Request, error) {
	if in.Endpoint != EndpointChatCompletions {
		return nil, fmt.Errorf("anthropic_native does not support endpoint %s", in.Endpoint)
	}

	var downstream struct {
		Model       string                   `json:"model"`
		Messages    []anthropicOpenAIMessage `json:"messages"`
		MaxTokens   int                      `json:"max_tokens"`
		Temperature *float64                 `json:"temperature,omitempty"`
		TopP        *float64                 `json:"top_p,omitempty"`
		Stream      bool                     `json:"stream"`
	}
	if err := json.Unmarshal(in.DownstreamBody, &downstream); err != nil {
		return nil, fmt.Errorf("decode downstream body: %w", err)
	}

	mappedModel := strings.TrimSpace(in.MappedModel)
	if mappedModel == "" {
		mappedModel = strings.TrimSpace(in.RequestedModel)
	}
	if mappedModel == "" {
		mappedModel = strings.TrimSpace(downstream.Model)
	}
	if mappedModel == "" {
		return nil, fmt.Errorf("model is required")
	}

	maxTokens := downstream.MaxTokens
	if maxTokens <= 0 {
		maxTokens = in.Channel.DefaultMaxOutput
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	messages, system := convertAnthropicMessages(downstream.Messages)
	payload := map[string]interface{}{
		"model":      mappedModel,
		"max_tokens": maxTokens,
		"messages":   messages,
	}
	if system != "" {
		payload["system"] = system
	}
	if downstream.Temperature != nil {
		payload["temperature"] = *downstream.Temperature
	}
	if downstream.TopP != nil {
		payload["top_p"] = *downstream.TopP
	}
	if downstream.Stream {
		payload["stream"] = true
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode anthropic request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, UpstreamEndpoint(in.Channel.BaseURL, "/v1/messages"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	cfg := parseAnthropicConfig(in.Channel.AdapterConfig)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", in.APIKey)
	req.Header.Set("anthropic-version", cfg.Version)
	if cfg.Beta != "" {
		req.Header.Set("anthropic-beta", cfg.Beta)
	}
	if in.RequestID != "" {
		req.Header.Set("X-Request-ID", in.RequestID)
	}
	return req, nil
}

func (a *AnthropicNative) ParseResponse(ctx context.Context, in ResponseInput) (*NormalizedResponse, error) {
	_ = ctx
	initialUsage, err := NewUsage(0, 0, 0, metering.UsageSourceUpstream)
	if err != nil {
		return nil, err
	}
	normalized := &NormalizedResponse{
		StatusCode: in.StatusCode,
		Header:     in.Header,
		Meta: ProviderMeta{
			Provider:       in.Channel.Provider,
			ChannelID:      in.Channel.ID,
			ChannelName:    in.Channel.Name,
			AdapterType:    NormalizeType(in.Channel.AdapterType),
			RequestedModel: in.RequestedModel,
			UpstreamModel:  in.UpstreamModel,
			TTFTMillis:     in.TTFTMillis,
		},
		Usage: initialUsage,
	}

	if in.StatusCode < 200 || in.StatusCode >= 300 {
		normalized.Body = in.Body
		normalized.Error = a.ClassifyHTTPStatus(in.StatusCode, in.Body)
		return normalized, nil
	}

	var parsed anthropicMessageResponse
	if err := json.Unmarshal(in.Body, &parsed); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	normalized.Usage, err = normalizeAnthropicUsage(parsed.Usage, metering.UsageSourceUpstream)
	if err != nil {
		return nil, fmt.Errorf("normalize anthropic usage: %w", err)
	}

	model := strings.TrimSpace(in.RequestedModel)
	if model == "" {
		model = strings.TrimSpace(in.UpstreamModel)
	}
	if model == "" {
		model = parsed.Model
	}
	body, err := json.Marshal(map[string]interface{}{
		"id":      firstNonEmpty(parsed.ID, "chatcmpl-anthropic"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"finish_reason": normalizeAnthropicStopReason(parsed.StopReason),
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": joinAnthropicText(parsed.Content),
				},
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     normalized.Usage.PromptTokens,
			"completion_tokens": normalized.Usage.CompletionTokens,
			"total_tokens":      normalized.Usage.TotalTokens,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode openai-compatible response: %w", err)
	}
	normalized.Body = body
	return normalized, nil
}

func (a *AnthropicNative) NormalizeStreamChunk(ctx context.Context, in StreamChunkInput) (*NormalizedStreamChunk, error) {
	_ = ctx
	if in.State == nil {
		return nil, fmt.Errorf("stream state is required")
	}
	if in.State.Created == 0 {
		in.State.Created = time.Now().Unix()
	}

	var event anthropicStreamEvent
	if err := json.Unmarshal(in.Data, &event); err != nil {
		return nil, fmt.Errorf("decode anthropic stream chunk: %w", err)
	}

	model := firstNonEmpty(in.RequestedModel, in.UpstreamModel)
	result := &NormalizedStreamChunk{}
	switch event.Type {
	case "message_start":
		if event.Message != nil {
			in.State.ID = firstNonEmpty(event.Message.ID, in.State.ID, "chatcmpl-anthropic")
			if model == "" {
				model = event.Message.Model
			}
			usage, err := normalizeAnthropicUsage(event.Message.Usage, metering.UsageSourceUpstream)
			if err != nil {
				return nil, fmt.Errorf("normalize anthropic stream usage: %w", err)
			}
			in.State.PromptTokens = usage.PromptTokens
			in.State.UncachedPromptTokens = usage.UncachedPromptTokens
			in.State.CacheReadTokens = usage.CacheReadTokens
			in.State.CacheCreationTokens = usage.CacheCreationTokens
			in.State.CompletionTokens = usage.CompletionTokens
			in.State.CacheUsageSchema = usage.CacheUsageSchema
			in.State.CacheUsageStatus = usage.CacheUsageStatus
		}
		result.Data = marshalOpenAIStreamChunk(in.State, model, map[string]interface{}{"role": "assistant"}, nil)
	case "content_block_start":
		if event.ContentBlock == nil || event.ContentBlock.Type != "text" || event.ContentBlock.Text == "" {
			result.Skip = true
			return result, nil
		}
		result.TextDelta = event.ContentBlock.Text
		result.Data = marshalOpenAIStreamChunk(in.State, model, map[string]interface{}{"content": event.ContentBlock.Text}, nil)
	case "content_block_delta":
		if event.Delta == nil || event.Delta.Type != "text_delta" || event.Delta.Text == "" {
			result.Skip = true
			return result, nil
		}
		result.TextDelta = event.Delta.Text
		result.Data = marshalOpenAIStreamChunk(in.State, model, map[string]interface{}{"content": event.Delta.Text}, nil)
	case "message_delta":
		if event.Usage != nil {
			in.State.CompletionTokens = event.Usage.OutputTokens
			if in.State.PromptTokens > 0 {
				result.HasUsage = true
				usage, err := UsageFromStreamState(in.State, metering.UsageSourceUpstream)
				if err != nil {
					return nil, fmt.Errorf("normalize anthropic final stream usage: %w", err)
				}
				result.Usage = usage
			}
		}
		if event.Delta == nil || event.Delta.StopReason == "" {
			result.Skip = true
			return result, nil
		}
		result.Data = marshalOpenAIStreamChunk(in.State, model, map[string]interface{}{}, normalizeAnthropicStopReason(event.Delta.StopReason))
	case "message_stop":
		result.Skip = true
		result.Done = true
	case "ping":
		result.Skip = true
	case "error":
		body, err := json.Marshal(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    firstNonEmpty(event.Error.Type, "upstream_error"),
				"message": firstNonEmpty(event.Error.Message, "Anthropic stream error"),
			},
		})
		if err != nil {
			return nil, err
		}
		result.Data = body
		result.Done = true
	default:
		result.Skip = true
	}
	return result, nil
}

func (a *AnthropicNative) Test(ctx context.Context, in TestInput) (*TestResult, error) {
	client := in.Client
	if client == nil {
		client = http.DefaultClient
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		prompt = "Hello, this is a test message."
	}
	payload, err := json.Marshal(map[string]interface{}{
		"model": in.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 16,
	})
	if err != nil {
		return nil, err
	}
	req, err := a.BuildRequest(ctx, RequestInput{
		Endpoint:       EndpointChatCompletions,
		DownstreamBody: payload,
		RequestedModel: in.Model,
		MappedModel:    in.Model,
		Channel:        in.Channel,
		APIKey:         in.APIKey,
	})
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return &TestResult{Success: false, Status: "error", LatencyMs: latency, Model: in.Model, Error: err.Error(), RequestURL: req.URL.String()}, nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	normalized, err := a.ParseResponse(ctx, ResponseInput{
		StatusCode:     resp.StatusCode,
		Header:         resp.Header,
		Body:           body,
		Channel:        in.Channel,
		RequestedModel: in.Model,
		UpstreamModel:  in.Model,
	})
	if err != nil {
		return nil, err
	}
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	status := "failed"
	errorMessage := ""
	if success {
		status = "ok"
	} else if normalized.Error != nil {
		errorMessage = normalized.Error.Message
	}
	return &TestResult{
		Success:    success,
		Status:     status,
		StatusCode: resp.StatusCode,
		LatencyMs:  latency,
		Model:      in.Model,
		TokensUsed: normalized.Usage.TotalTokens,
		Body:       normalized.Body,
		Error:      errorMessage,
		RequestURL: req.URL.String(),
	}, nil
}

func (a *AnthropicNative) ClassifyHTTPStatus(statusCode int, body []byte) *NormalizedError {
	err := &NormalizedError{
		HTTPStatus: statusCode,
		Category:   "upstream_error",
		Code:       fmt.Sprintf("anthropic_http_%d", statusCode),
		Message:    fmt.Sprintf("anthropic returned HTTP %d", statusCode),
	}
	if code, msg := extractAnthropicError(body); msg != "" || code != "" {
		if code != "" {
			err.Code = code
		}
		if msg != "" {
			err.Message = msg
		}
	}
	err.FailureKind, err.Retryable = classifyHTTPFailure(statusCode)
	if statusCode == 529 {
		err.FailureKind = FailureChannel
		err.Retryable = true
	}
	return err
}

type anthropicConfig struct {
	Version string `json:"anthropic_version"`
	Beta    string `json:"anthropic_beta"`
}

func parseAnthropicConfig(raw []byte) anthropicConfig {
	cfg := anthropicConfig{Version: defaultAnthropicVersion}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	if strings.TrimSpace(cfg.Version) == "" {
		cfg.Version = defaultAnthropicVersion
	}
	cfg.Beta = strings.TrimSpace(cfg.Beta)
	return cfg
}

type anthropicOpenAIMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func convertAnthropicMessages(messages []anthropicOpenAIMessage) ([]anthropicMessage, string) {
	result := make([]anthropicMessage, 0, len(messages))
	var systemParts []string
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		content := openAIContentToText(message.Content)
		if content == "" {
			continue
		}
		switch role {
		case "system":
			systemParts = append(systemParts, content)
		case "assistant":
			result = append(result, anthropicMessage{Role: "assistant", Content: content})
		default:
			result = append(result, anthropicMessage{Role: "user", Content: content})
		}
	}
	return result, strings.Join(systemParts, "\n\n")
}

func openAIContentToText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
				values = append(values, strings.TrimSpace(part.Text))
			}
		}
		return strings.Join(values, "\n")
	}
	return ""
}

type anthropicUsageWire struct {
	InputTokens              int  `json:"input_tokens"`
	OutputTokens             int  `json:"output_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
	CacheCreation            *struct {
		Ephemeral5mInputTokens *int `json:"ephemeral_5m_input_tokens,omitempty"`
		Ephemeral1hInputTokens *int `json:"ephemeral_1h_input_tokens,omitempty"`
	} `json:"cache_creation,omitempty"`
}

func normalizeAnthropicUsage(wire anthropicUsageWire, source string) (Usage, error) {
	readTokens := valueOrZero(wire.CacheReadInputTokens)
	creationTokens := valueOrZero(wire.CacheCreationInputTokens)
	readReported := wire.CacheReadInputTokens != nil
	creationReported := wire.CacheCreationInputTokens != nil || wire.CacheCreation != nil
	cacheReported := readReported || creationReported
	if wire.CacheCreation != nil {
		detailTotal := valueOrZero(wire.CacheCreation.Ephemeral5mInputTokens) + valueOrZero(wire.CacheCreation.Ephemeral1hInputTokens)
		if wire.CacheCreationInputTokens == nil {
			creationTokens = detailTotal
		} else if detailTotal != creationTokens {
			return Usage{}, fmt.Errorf(
				"cache_creation_input_tokens=%d does not match 5m+1h detail=%d",
				creationTokens,
				detailTotal,
			)
		}
	}
	if !cacheReported {
		return NewUsage(wire.InputTokens, wire.OutputTokens, wire.InputTokens+wire.OutputTokens, source)
	}
	status := CacheUsageStatusReadWriteReported
	if readReported && !creationReported {
		status = CacheUsageStatusReadReported
	} else if creationReported && !readReported {
		status = CacheUsageStatusWriteReported
	}
	promptTotal := wire.InputTokens + readTokens + creationTokens
	return NewCacheAwareUsage(
		promptTotal,
		wire.InputTokens,
		readTokens,
		creationTokens,
		wire.OutputTokens,
		promptTotal+wire.OutputTokens,
		source,
		CacheUsageSchemaAnthropicV1,
		status,
	)
}

type anthropicMessageResponse struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage anthropicUsageWire `json:"usage"`
}

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Message *struct {
		ID    string             `json:"id"`
		Model string             `json:"model"`
		Usage anthropicUsageWire `json:"usage"`
	} `json:"message,omitempty"`
	ContentBlock *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content_block,omitempty"`
	Delta *struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta,omitempty"`
	Usage *anthropicUsageWire `json:"usage,omitempty"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func marshalOpenAIStreamChunk(state *StreamState, model string, delta map[string]interface{}, finishReason interface{}) []byte {
	id := "chatcmpl-anthropic"
	created := time.Now().Unix()
	if state != nil {
		id = firstNonEmpty(state.ID, id)
		if state.Created > 0 {
			created = state.Created
		}
	}
	body, _ := json.Marshal(map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         delta,
				"finish_reason": finishReason,
			},
		},
	})
	return body
}

func joinAnthropicText(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	parts := make([]string, 0, len(content))
	for _, item := range content {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "")
}

func normalizeAnthropicStopReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

func extractAnthropicError(body []byte) (string, string) {
	var parsed struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return "", ""
	}
	return parsed.Error.Type, parsed.Error.Message
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
