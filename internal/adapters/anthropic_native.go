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

	"github.com/uniroute/unirouter/internal/eventconnector"
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

	downstream, err := parseAnthropicCompatibilityRequest(in.DownstreamBody)
	if err != nil {
		return nil, err
	}
	cfg, err := parseAnthropicConfig(in.Channel.AdapterConfig)
	if err != nil {
		return nil, err
	}
	if cfg.Beta != "" {
		return nil, fmt.Errorf("anthropic_native cannot preserve anthropic_beta without a declared beta protocol profile")
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

	maxTokens := 0
	if downstream.MaxCompletionTokens != nil {
		maxTokens = *downstream.MaxCompletionTokens
	} else if downstream.MaxTokens != nil {
		maxTokens = *downstream.MaxTokens
	}
	if maxTokens == 0 {
		maxTokens = in.Channel.DefaultMaxOutput
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	messages, system, err := convertAnthropicMessages(downstream.Messages)
	if err != nil {
		return nil, err
	}
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

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", in.APIKey)
	req.Header.Set("anthropic-version", cfg.Version)
	if in.RequestID != "" {
		req.Header.Set("X-Request-ID", in.RequestID)
	}
	return req, nil
}

func (a *AnthropicNative) ParseResponse(ctx context.Context, in ResponseInput) (*NormalizedResponse, error) {
	_ = ctx
	initialUsage := NewMissingUsage()
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
	if parsed.Usage == nil {
		normalized.Usage = NewMissingUsage()
	} else if err := validateAnthropicUsageWire(*parsed.Usage); err != nil {
		return nil, fmt.Errorf("normalize anthropic usage: %w", err)
	} else if parsed.Usage.InputTokens == nil || parsed.Usage.OutputTokens == nil {
		normalized.Usage = NewMissingUsage()
	} else {
		usage, err := normalizeAnthropicUsage(*parsed.Usage, metering.UsageSourceUpstream)
		if err != nil {
			return nil, fmt.Errorf("normalize anthropic usage: %w", err)
		}
		normalized.Usage = usage
	}

	model := strings.TrimSpace(in.RequestedModel)
	if model == "" {
		model = strings.TrimSpace(in.UpstreamModel)
	}
	if model == "" {
		model = parsed.Model
	}
	content, err := joinAnthropicText(parsed.Content)
	if err != nil {
		return nil, err
	}
	switch parsed.StopReason {
	case "", "end_turn", "max_tokens", "stop_sequence":
	default:
		return nil, fmt.Errorf("anthropic_native cannot preserve response stop_reason %q", parsed.StopReason)
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
					"content": content,
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
			if event.Message.Usage != nil {
				if err := validateAnthropicUsageWire(*event.Message.Usage); err != nil {
					return nil, fmt.Errorf("normalize anthropic stream usage: %w", err)
				}
				if event.Message.Usage.InputTokens != nil {
					usage, err := normalizeAnthropicUsage(*event.Message.Usage, metering.UsageSourceUpstream)
					if err != nil {
						return nil, fmt.Errorf("normalize anthropic stream usage: %w", err)
					}
					in.State.ProviderUsageReported = true
					in.State.PromptTokens = usage.PromptTokens
					in.State.UncachedPromptTokens = usage.UncachedPromptTokens
					in.State.CacheReadTokens = usage.CacheReadTokens
					in.State.CacheCreationTokens = usage.CacheCreationTokens
					in.State.CompletionTokens = usage.CompletionTokens
					in.State.CacheUsageSchema = usage.CacheUsageSchema
					in.State.CacheUsageStatus = usage.CacheUsageStatus
				}
			}
		}
		result.Data = marshalOpenAIStreamChunk(in.State, model, map[string]interface{}{"role": "assistant"}, nil)
	case "content_block_start":
		if event.ContentBlock == nil {
			result.Skip = true
			return result, nil
		}
		if event.ContentBlock.Type != "text" {
			return nil, fmt.Errorf("anthropic_native cannot preserve streaming content block type %q", event.ContentBlock.Type)
		}
		if event.ContentBlock.Text == "" {
			result.Skip = true
			return result, nil
		}
		result.TextDelta = event.ContentBlock.Text
		result.Data = marshalOpenAIStreamChunk(in.State, model, map[string]interface{}{"content": event.ContentBlock.Text}, nil)
	case "content_block_delta":
		if event.Delta == nil {
			result.Skip = true
			return result, nil
		}
		if event.Delta.Type != "text_delta" {
			return nil, fmt.Errorf("anthropic_native cannot preserve streaming delta type %q", event.Delta.Type)
		}
		if event.Delta.Text == "" {
			result.Skip = true
			return result, nil
		}
		result.TextDelta = event.Delta.Text
		result.Data = marshalOpenAIStreamChunk(in.State, model, map[string]interface{}{"content": event.Delta.Text}, nil)
	case "message_delta":
		if event.Usage != nil {
			if err := validateAnthropicUsageWire(*event.Usage); err != nil {
				return nil, fmt.Errorf("normalize anthropic final stream usage: %w", err)
			}
			if event.Usage.OutputTokens != nil && in.State.ProviderUsageReported {
				in.State.CompletionTokens = *event.Usage.OutputTokens
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
		switch event.Delta.StopReason {
		case "end_turn", "max_tokens", "stop_sequence":
		default:
			return nil, fmt.Errorf("anthropic_native cannot preserve streaming stop_reason %q", event.Delta.StopReason)
		}
		result.Data = marshalOpenAIStreamChunk(in.State, model, map[string]interface{}{}, normalizeAnthropicStopReason(event.Delta.StopReason))
	case "message_stop":
		result.Skip = true
		result.Done = true
	case "content_block_stop", "ping":
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
		return nil, fmt.Errorf("anthropic_native cannot preserve streaming event type %q", event.Type)
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
		Success:       success,
		Status:        status,
		StatusCode:    resp.StatusCode,
		LatencyMs:     latency,
		Model:         in.Model,
		ReportedModel: responseModelIdentity(body),
		TokensUsed:    normalized.Usage.TotalTokens,
		Body:          normalized.Body,
		Error:         errorMessage,
		RequestURL:    req.URL.String(),
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

func parseAnthropicConfig(raw []byte) (anthropicConfig, error) {
	cfg := anthropicConfig{Version: defaultAnthropicVersion}
	if len(bytes.TrimSpace(raw)) > 0 {
		if strictErr := eventconnector.ValidateStrictJSON(raw, eventconnector.StrictJSONOptions{}); strictErr != nil {
			return anthropicConfig{}, fmt.Errorf("anthropic_native adapter config is invalid: %s", strictErr.Code)
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cfg); err != nil {
			return anthropicConfig{}, fmt.Errorf("anthropic_native adapter config is invalid")
		}
	}
	if strings.TrimSpace(cfg.Version) == "" {
		cfg.Version = defaultAnthropicVersion
	}
	if strings.ContainsAny(cfg.Version, "\r\n") {
		return anthropicConfig{}, fmt.Errorf("anthropic_native anthropic_version is invalid")
	}
	cfg.Beta = strings.TrimSpace(cfg.Beta)
	return cfg, nil
}

type anthropicCompatibilityRequest struct {
	Model               string            `json:"model"`
	Messages            []json.RawMessage `json:"messages"`
	MaxTokens           *int              `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int              `json:"max_completion_tokens,omitempty"`
	Temperature         *float64          `json:"temperature,omitempty"`
	TopP                *float64          `json:"top_p,omitempty"`
	Stream              bool              `json:"stream"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func parseAnthropicCompatibilityRequest(raw []byte) (anthropicCompatibilityRequest, error) {
	if strictErr := eventconnector.ValidateStrictJSON(raw, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native downstream JSON is invalid: %s", strictErr.Code)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native downstream body must be a JSON object")
	}
	allowed := map[string]struct{}{
		"model": {}, "messages": {}, "max_tokens": {}, "max_completion_tokens": {},
		"temperature": {}, "top_p": {}, "stream": {},
	}
	for field := range fields {
		if _, ok := allowed[field]; ok {
			continue
		}
		switch field {
		case "function_call", "functions", "parallel_tool_calls", "tool_choice", "tools":
			return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native cannot preserve tools or tool choice")
		case "response_format":
			return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native cannot preserve structured output")
		case "reasoning", "reasoning_effort":
			return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native cannot preserve reasoning controls")
		case "audio", "modalities":
			return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native cannot preserve non-text output")
		case "prompt_cache_key", "prompt_cache_retention":
			return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native cannot preserve cache controls")
		default:
			return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native cannot preserve downstream field %q", field)
		}
	}
	var downstream anthropicCompatibilityRequest
	if err := json.Unmarshal(raw, &downstream); err != nil {
		return anthropicCompatibilityRequest{}, fmt.Errorf("decode downstream body: %w", err)
	}
	if downstream.MaxTokens != nil && *downstream.MaxTokens <= 0 {
		return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native max_tokens must be positive")
	}
	if downstream.MaxCompletionTokens != nil && *downstream.MaxCompletionTokens <= 0 {
		return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native max_completion_tokens must be positive")
	}
	if downstream.MaxTokens != nil && downstream.MaxCompletionTokens != nil && *downstream.MaxTokens != *downstream.MaxCompletionTokens {
		return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native max_tokens and max_completion_tokens must match")
	}
	if downstream.Temperature != nil && (*downstream.Temperature < 0 || *downstream.Temperature > 1) {
		return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native temperature must be between 0 and 1")
	}
	if downstream.TopP != nil && (*downstream.TopP < 0 || *downstream.TopP > 1) {
		return anthropicCompatibilityRequest{}, fmt.Errorf("anthropic_native top_p must be between 0 and 1")
	}
	return downstream, nil
}

func convertAnthropicMessages(messages []json.RawMessage) ([]anthropicMessage, string, error) {
	result := make([]anthropicMessage, 0, len(messages))
	var systemParts []string
	conversationStarted := false
	for _, raw := range messages {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			return nil, "", fmt.Errorf("anthropic_native chat message must be an object")
		}
		for _, field := range []string{"function_call", "tool_call_id", "tool_calls"} {
			if _, exists := fields[field]; exists {
				return nil, "", fmt.Errorf("anthropic_native cannot preserve tool messages")
			}
		}
		if len(fields) != 2 {
			return nil, "", fmt.Errorf("anthropic_native cannot preserve chat message fields")
		}
		var role string
		if err := json.Unmarshal(fields["role"], &role); err != nil {
			return nil, "", fmt.Errorf("anthropic_native chat message role is invalid")
		}
		role = strings.TrimSpace(role)
		if role == "tool" || role == "function" {
			return nil, "", fmt.Errorf("anthropic_native cannot preserve tool messages")
		}
		if role != "system" && role != "assistant" && role != "user" {
			return nil, "", fmt.Errorf("anthropic_native cannot preserve chat message role %q", role)
		}
		content, err := openAIContentToText(fields["content"])
		if err != nil {
			return nil, "", err
		}
		if content == "" {
			return nil, "", fmt.Errorf("anthropic_native cannot preserve empty chat message content")
		}
		switch role {
		case "system":
			if conversationStarted {
				return nil, "", fmt.Errorf("anthropic_native cannot preserve a system message after conversation messages")
			}
			systemParts = append(systemParts, content)
		case "assistant":
			conversationStarted = true
			result = append(result, anthropicMessage{Role: "assistant", Content: content})
		case "user":
			conversationStarted = true
			result = append(result, anthropicMessage{Role: "user", Content: content})
		}
	}
	if len(result) == 0 {
		return nil, "", fmt.Errorf("anthropic_native requires at least one user or assistant message")
	}
	return result, strings.Join(systemParts, "\n\n"), nil
}

func openAIContentToText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("anthropic_native chat message content is required")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text), nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil {
		values := make([]string, 0, len(parts))
		for _, partRaw := range parts {
			var fields map[string]json.RawMessage
			if json.Unmarshal(partRaw, &fields) != nil || fields == nil {
				return "", fmt.Errorf("anthropic_native cannot preserve non-text content")
			}
			if _, exists := fields["cache_control"]; exists {
				return "", fmt.Errorf("anthropic_native cannot preserve prompt cache controls or beta semantics")
			}
			var contentType string
			if json.Unmarshal(fields["type"], &contentType) != nil || contentType != "text" || len(fields) != 2 {
				if contentType == "tool_use" || contentType == "tool_result" {
					return "", fmt.Errorf("anthropic_native cannot preserve tool content")
				}
				return "", fmt.Errorf("anthropic_native cannot preserve non-text content")
			}
			var partText string
			if json.Unmarshal(fields["text"], &partText) != nil || strings.TrimSpace(partText) == "" {
				return "", fmt.Errorf("anthropic_native cannot preserve empty or invalid text content")
			}
			values = append(values, strings.TrimSpace(partText))
		}
		return strings.Join(values, "\n"), nil
	}
	return "", fmt.Errorf("anthropic_native cannot preserve non-text content")
}

type anthropicUsageWire struct {
	InputTokens              *int `json:"input_tokens"`
	OutputTokens             *int `json:"output_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
	CacheCreation            *struct {
		Ephemeral5mInputTokens *int `json:"ephemeral_5m_input_tokens,omitempty"`
		Ephemeral1hInputTokens *int `json:"ephemeral_1h_input_tokens,omitempty"`
	} `json:"cache_creation,omitempty"`
}

func normalizeAnthropicUsage(wire anthropicUsageWire, source string) (Usage, error) {
	if err := validateAnthropicUsageWire(wire); err != nil {
		return Usage{}, err
	}
	inputTokens := valueOrZero(wire.InputTokens)
	outputTokens := valueOrZero(wire.OutputTokens)
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
		return NewUsage(inputTokens, outputTokens, inputTokens+outputTokens, source)
	}
	status := CacheUsageStatusReadWriteReported
	if readReported && !creationReported {
		status = CacheUsageStatusReadReported
	} else if creationReported && !readReported {
		status = CacheUsageStatusWriteReported
	}
	promptTotal := inputTokens + readTokens + creationTokens
	return NewCacheAwareUsage(
		promptTotal,
		inputTokens,
		readTokens,
		creationTokens,
		outputTokens,
		promptTotal+outputTokens,
		source,
		CacheUsageSchemaAnthropicV1,
		status,
	)
}

func validateAnthropicUsageWire(wire anthropicUsageWire) error {
	counters := []optionalUsageCounter{
		{name: "input_tokens", value: wire.InputTokens},
		{name: "output_tokens", value: wire.OutputTokens},
		{name: "cache_read_input_tokens", value: wire.CacheReadInputTokens},
		{name: "cache_creation_input_tokens", value: wire.CacheCreationInputTokens},
	}
	if wire.CacheCreation != nil {
		counters = append(counters,
			optionalUsageCounter{name: "cache_creation.ephemeral_5m_input_tokens", value: wire.CacheCreation.Ephemeral5mInputTokens},
			optionalUsageCounter{name: "cache_creation.ephemeral_1h_input_tokens", value: wire.CacheCreation.Ephemeral1hInputTokens},
		)
	}
	return validateOptionalUsageCounters(counters...)
}

type anthropicMessageResponse struct {
	ID         string              `json:"id"`
	Model      string              `json:"model"`
	StopReason string              `json:"stop_reason"`
	Content    []json.RawMessage   `json:"content"`
	Usage      *anthropicUsageWire `json:"usage"`
}

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Message *struct {
		ID    string              `json:"id"`
		Model string              `json:"model"`
		Usage *anthropicUsageWire `json:"usage"`
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

func joinAnthropicText(content []json.RawMessage) (string, error) {
	parts := make([]string, 0, len(content))
	for _, raw := range content {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return "", fmt.Errorf("anthropic_native response content block is invalid")
		}
		var contentType string
		if json.Unmarshal(fields["type"], &contentType) != nil || contentType != "text" || len(fields) != 2 {
			return "", fmt.Errorf("anthropic_native cannot preserve response content type %q", contentType)
		}
		var text string
		if json.Unmarshal(fields["text"], &text) != nil {
			return "", fmt.Errorf("anthropic_native response text content is invalid")
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, ""), nil
}

func normalizeAnthropicStopReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
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
