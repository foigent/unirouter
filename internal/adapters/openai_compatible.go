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
	"github.com/uniroute/unirouter/internal/modelprofile"
)

type OpenAICompatible struct{}

const (
	OpenAIEndpointPathCanonicalV1  = modelprofile.EndpointPathModeCanonicalV1
	OpenAIEndpointPathBaseRelative = modelprofile.EndpointPathModeBaseRelative
)

type openAICompatibleConfig struct {
	EndpointPathMode string `json:"endpoint_path_mode"`
}

func NewOpenAICompatible() *OpenAICompatible {
	return &OpenAICompatible{}
}

func (a *OpenAICompatible) Type() string {
	return TypeOpenAICompatible
}

func (a *OpenAICompatible) ValidateConfig(raw []byte) error {
	_, err := parseOpenAICompatibleConfig(raw)
	return err
}

// EffectiveEndpointPathMode returns the normalized request-path composition
// used by an adapter configuration. Product identity validation consumes this
// value so a reviewed endpoint cannot be paired with a URL-incompatible mode.
func EffectiveEndpointPathMode(adapterType string, raw []byte) (string, error) {
	switch NormalizeType(adapterType) {
	case TypeAnthropicNative:
		if _, err := parseAnthropicConfig(raw); err != nil {
			return "", err
		}
		// anthropic_native always appends the exact /v1/messages path to a
		// canonical provider origin; this is adapter behavior, not a mutable
		// connection option.
		return modelprofile.EndpointPathModeCanonicalV1, nil
	case TypeOpenAICompatible:
		config, err := parseOpenAICompatibleConfig(raw)
		if err != nil {
			return "", err
		}
		return config.EndpointPathMode, nil
	default:
		return "", nil
	}
}

func (a *OpenAICompatible) BuildRequest(ctx context.Context, in RequestInput) (*http.Request, error) {
	config, err := parseOpenAICompatibleConfig(in.Channel.AdapterConfig)
	if err != nil {
		return nil, err
	}
	mappedModel := strings.TrimSpace(in.MappedModel)
	if mappedModel == "" {
		mappedModel = in.RequestedModel
	}
	// Keep every field other than model as raw JSON. Chat Completions clients
	// regularly send provider extensions and newer OpenAI fields that the
	// gateway must not round through float64 or a stale request struct.
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(in.DownstreamBody, &payload); err != nil {
		return nil, fmt.Errorf("decode downstream body: %w", err)
	}
	if payload == nil {
		return nil, fmt.Errorf("decode downstream body: JSON object is required")
	}
	if mappedModel != "" {
		modelJSON, err := json.Marshal(mappedModel)
		if err != nil {
			return nil, fmt.Errorf("encode upstream model: %w", err)
		}
		payload["model"] = modelJSON
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode upstream body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAICompatibleUpstreamEndpoint(in.Channel.BaseURL, in.Endpoint, config.EndpointPathMode), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+in.APIKey)
	if in.RequestID != "" {
		req.Header.Set("X-Request-ID", in.RequestID)
	}
	return req, nil
}

func parseOpenAICompatibleConfig(raw []byte) (openAICompatibleConfig, error) {
	config := openAICompatibleConfig{EndpointPathMode: OpenAIEndpointPathCanonicalV1}
	if len(bytes.TrimSpace(raw)) == 0 {
		return config, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decoded openAICompatibleConfig
	if err := decoder.Decode(&decoded); err != nil {
		return openAICompatibleConfig{}, fmt.Errorf("decode openai_compatible adapter_config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return openAICompatibleConfig{}, fmt.Errorf("decode openai_compatible adapter_config: trailing JSON value")
		}
		return openAICompatibleConfig{}, fmt.Errorf("decode openai_compatible adapter_config trailing content: %w", err)
	}
	if strings.TrimSpace(decoded.EndpointPathMode) != "" {
		config.EndpointPathMode = strings.TrimSpace(decoded.EndpointPathMode)
	}
	switch config.EndpointPathMode {
	case OpenAIEndpointPathCanonicalV1, OpenAIEndpointPathBaseRelative:
		return config, nil
	default:
		return openAICompatibleConfig{}, fmt.Errorf("openai_compatible endpoint_path_mode must be %q or %q", OpenAIEndpointPathCanonicalV1, OpenAIEndpointPathBaseRelative)
	}
}

func (a *OpenAICompatible) ParseResponse(ctx context.Context, in ResponseInput) (*NormalizedResponse, error) {
	_ = ctx
	usage := NewMissingUsage()
	responseBody := in.Body
	if in.StatusCode >= 200 && in.StatusCode < 300 {
		var err error
		var usagePresent bool
		usage, usagePresent, err = ParseOpenAICompatibleUsageWithCacheProfile(
			in.Body, in.Endpoint, metering.UsageSourceUpstream, in.CacheUsageProfile,
		)
		if err != nil {
			return nil, err
		}
		if cacheContract, ok := modelprofile.ResolveCacheUsageContract(strings.TrimSpace(in.CacheUsageProfile)); ok && cacheContract.RequireReadCounter && !usagePresent {
			return nil, fmt.Errorf("cache profile %s requires final response usage with prompt_tokens_details.cached_tokens", cacheContract.ProfileID)
		}
		if in.Endpoint == EndpointChatCompletions {
			meterUsage, meterErr := parseBuiltinToolMeterUsage(in.Body, in.ProtocolProfile)
			if meterErr != nil {
				return nil, meterErr
			}
			usage.MeterUsage = meterUsage
			responseBody, err = normalizeMiniMaxOpenAIEnvelope(in.Channel.Provider, in.Body)
			if err != nil {
				return nil, fmt.Errorf("normalize MiniMax response: %w", err)
			}
			responseBody, err = normalizeOpenAIClientCacheUsage(responseBody, usage)
			if err != nil {
				return nil, fmt.Errorf("normalize provider cache usage: %w", err)
			}
		}
	}
	normalized := &NormalizedResponse{
		StatusCode: in.StatusCode,
		Header:     in.Header,
		Body:       responseBody,
		Usage:      usage,
		Meta: ProviderMeta{
			Provider:       in.Channel.Provider,
			ChannelID:      in.Channel.ID,
			ChannelName:    in.Channel.Name,
			AdapterType:    NormalizeType(in.Channel.AdapterType),
			RequestedModel: in.RequestedModel,
			UpstreamModel:  in.UpstreamModel,
			TTFTMillis:     in.TTFTMillis,
		},
	}
	if in.StatusCode < 200 || in.StatusCode >= 300 {
		normalized.Error = a.ClassifyHTTPStatus(in.StatusCode, in.Body)
	}
	return normalized, nil
}

func parseBuiltinToolMeterUsage(body []byte, protocolProfile string) ([]metering.MeterQuantity, error) {
	contract, ok := modelprofile.ResolveChatProtocolContract(protocolProfile)
	if !ok || !contract.BuiltinWebSearch {
		return []metering.MeterQuantity{}, nil
	}
	var envelope struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				ToolCalls []struct {
					Type     string `json:"type"`
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode builtin tool usage: %w", err)
	}
	var count int64
	for _, choice := range envelope.Choices {
		if choice.FinishReason != "tool_calls" {
			continue
		}
		for _, call := range choice.Message.ToolCalls {
			if call.Type == "function" && call.Function.Name == contract.BuiltinWebSearchName {
				count++
			}
		}
	}
	if count > contract.MaxBuiltinSearchCalls {
		return nil, fmt.Errorf("builtin web search usage %d exceeds profile maximum %d", count, contract.MaxBuiltinSearchCalls)
	}
	if count == 0 {
		return []metering.MeterQuantity{}, nil
	}
	return metering.NormalizeMeterQuantities([]metering.MeterQuantity{{
		Meter: contract.BuiltinSearchMeter, Quantity: count,
	}})
}

// NormalizeStreamFrame strips only documented, non-model MiniMax control
// metadata. The gateway still validates every resulting OpenAI-compatible
// frame and fails closed on unknown or unsafe fields.
func (a *OpenAICompatible) NormalizeStreamFrame(ctx context.Context, in StreamChunkInput) ([]byte, error) {
	_ = ctx
	body, err := normalizeMiniMaxOpenAIEnvelope(in.Channel.Provider, in.Data)
	if err != nil {
		return nil, err
	}
	usage, present, err := ParseOpenAICompatibleUsageWithCacheProfile(
		body, EndpointChatCompletions, metering.UsageSourceUpstream, in.CacheUsageProfile,
	)
	if err != nil || !present {
		return body, err
	}
	return normalizeOpenAIClientCacheUsage(body, usage)
}

// normalizeOpenAIClientCacheUsage mirrors profile-authoritative provider cache
// counters into the standard OpenAI prompt_tokens_details shape used by generic
// SDKs. Provider-owned fields remain intact for native clients and evidence;
// ParseOpenAICompatibleUsageWithCacheProfile validates contradictions before
// this projection is reached.
func normalizeOpenAIClientCacheUsage(body []byte, usage Usage) ([]byte, error) {
	if usage.CacheUsageSchema != CacheUsageSchemaKimiV1 && usage.CacheUsageSchema != CacheUsageSchemaDeepSeekV1 {
		return body, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode response envelope: %w", err)
	}
	if envelope == nil {
		return nil, fmt.Errorf("decode response envelope: JSON object is required")
	}
	rawUsage, exists := envelope["usage"]
	if !exists || isJSONNullUsage(rawUsage) {
		return nil, fmt.Errorf("authoritative cache usage has no response usage object")
	}
	var usageObject map[string]json.RawMessage
	if err := json.Unmarshal(rawUsage, &usageObject); err != nil {
		return nil, fmt.Errorf("decode response usage object: %w", err)
	}
	if usageObject == nil {
		return nil, fmt.Errorf("decode response usage object: JSON object is required")
	}

	promptDetails := make(map[string]json.RawMessage)
	if rawDetails, ok := usageObject["prompt_tokens_details"]; ok && !isJSONNullUsage(rawDetails) {
		if err := json.Unmarshal(rawDetails, &promptDetails); err != nil || promptDetails == nil {
			return nil, fmt.Errorf("decode prompt_tokens_details object")
		}
		if rawCached, mirrored := promptDetails["cached_tokens"]; mirrored && !isJSONNullUsage(rawCached) {
			return body, nil
		}
	}
	cachedJSON, err := json.Marshal(usage.CacheReadTokens)
	if err != nil {
		return nil, fmt.Errorf("encode cached token projection: %w", err)
	}
	promptDetails["cached_tokens"] = cachedJSON
	detailsJSON, err := json.Marshal(promptDetails)
	if err != nil {
		return nil, fmt.Errorf("encode prompt_tokens_details projection: %w", err)
	}
	usageObject["prompt_tokens_details"] = detailsJSON
	usageJSON, err := json.Marshal(usageObject)
	if err != nil {
		return nil, fmt.Errorf("encode response usage projection: %w", err)
	}
	envelope["usage"] = usageJSON
	projected, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode response envelope projection: %w", err)
	}
	return projected, nil
}

func normalizeMiniMaxOpenAIEnvelope(provider string, body []byte) ([]byte, error) {
	if !strings.EqualFold(strings.TrimSpace(provider), "minimax") {
		return body, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return body, nil
	}

	changed := false
	for _, field := range []string{"input_sensitive", "output_sensitive"} {
		if raw, exists := envelope[field]; exists && rawJSONBoolIs(raw, false) {
			delete(envelope, field)
			changed = true
		}
	}
	for _, field := range []string{"input_sensitive_type", "output_sensitive_type", "output_sensitive_int"} {
		if raw, exists := envelope[field]; exists && rawJSONInt64Is(raw, 0) {
			delete(envelope, field)
			changed = true
		}
	}
	if raw, exists := envelope["base_resp"]; exists && miniMaxBaseResponseIsSuccess(raw) {
		delete(envelope, "base_resp")
		changed = true
	}

	if rawChoices, exists := envelope["choices"]; exists {
		choices, choicesChanged, err := normalizeMiniMaxChoices(rawChoices)
		if err != nil {
			return nil, err
		}
		if choicesChanged {
			envelope["choices"] = choices
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	normalized, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func normalizeMiniMaxChoices(raw json.RawMessage) (json.RawMessage, bool, error) {
	var choices []json.RawMessage
	if err := json.Unmarshal(raw, &choices); err != nil {
		return raw, false, nil
	}
	changed := false
	for index, rawChoice := range choices {
		var choice map[string]json.RawMessage
		if err := json.Unmarshal(rawChoice, &choice); err != nil || choice == nil {
			continue
		}
		choiceChanged := false
		for _, payloadField := range []string{"message", "delta"} {
			rawPayload, exists := choice[payloadField]
			if !exists {
				continue
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(rawPayload, &payload); err != nil || payload == nil {
				continue
			}
			payloadChanged := false
			if rawName, exists := payload["name"]; exists && rawJSONStringIs(rawName, "MiniMax AI") {
				delete(payload, "name")
				payloadChanged = true
			}
			if rawAudio, exists := payload["audio_content"]; exists && (rawJSONStringIs(rawAudio, "") || string(rawAudio) == "null") {
				delete(payload, "audio_content")
				payloadChanged = true
			}
			if !payloadChanged {
				continue
			}
			normalizedPayload, err := json.Marshal(payload)
			if err != nil {
				return nil, false, err
			}
			choice[payloadField] = normalizedPayload
			choiceChanged = true
		}
		if !choiceChanged {
			continue
		}
		normalizedChoice, err := json.Marshal(choice)
		if err != nil {
			return nil, false, err
		}
		choices[index] = normalizedChoice
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	normalized, err := json.Marshal(choices)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

func miniMaxBaseResponseIsSuccess(raw json.RawMessage) bool {
	var base map[string]json.RawMessage
	if err := json.Unmarshal(raw, &base); err != nil || base == nil || len(base) == 0 || len(base) > 2 {
		return false
	}
	statusCode, exists := base["status_code"]
	if !exists || !rawJSONInt64Is(statusCode, 0) {
		return false
	}
	if statusMessage, exists := base["status_msg"]; exists {
		var message string
		if err := json.Unmarshal(statusMessage, &message); err != nil {
			return false
		}
	}
	for field := range base {
		if field != "status_code" && field != "status_msg" {
			return false
		}
	}
	return true
}

func rawJSONBoolIs(raw json.RawMessage, want bool) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value == want
}

func rawJSONInt64Is(raw json.RawMessage, want int64) bool {
	var value int64
	return json.Unmarshal(raw, &value) == nil && value == want
}

func rawJSONStringIs(raw json.RawMessage, want string) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && value == want
}

func (a *OpenAICompatible) Test(ctx context.Context, in TestInput) (*TestResult, error) {
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
		Endpoint:       EndpointChatCompletions,
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
		Body:          body,
		Error:         errorMessage,
		RequestURL:    req.URL.String(),
	}, nil
}

func (a *OpenAICompatible) ClassifyHTTPStatus(statusCode int, body []byte) *NormalizedError {
	err := &NormalizedError{
		HTTPStatus: statusCode,
		Category:   "upstream_error",
		Code:       fmt.Sprintf("upstream_http_%d", statusCode),
		Message:    fmt.Sprintf("upstream returned HTTP %d", statusCode),
	}
	if msg := extractOpenAIErrorMessage(body); msg != "" {
		err.Message = msg
	}
	err.FailureKind, err.Retryable = classifyHTTPFailure(statusCode)
	return err
}

func UpstreamEndpoint(baseURL, endpoint string) string {
	base := strings.TrimRight(baseURL, "/")
	path := strings.TrimLeft(endpoint, "/")
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "v1/") {
		path = strings.TrimPrefix(path, "v1/")
	}
	return base + "/" + path
}

func openAICompatibleUpstreamEndpoint(baseURL string, endpoint Endpoint, pathMode string) string {
	base := strings.TrimRight(baseURL, "/")
	path := strings.TrimLeft(string(endpoint), "/")
	if pathMode == OpenAIEndpointPathBaseRelative {
		path = strings.TrimPrefix(path, "v1/")
	}
	return UpstreamEndpoint(base, path)
}

func extractOpenAIErrorMessage(body []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return ""
	}
	if parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return parsed.Error.Code
}
