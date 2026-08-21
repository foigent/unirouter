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

type OpenAICompatible struct{}

func NewOpenAICompatible() *OpenAICompatible {
	return &OpenAICompatible{}
}

func (a *OpenAICompatible) Type() string {
	return TypeOpenAICompatible
}

func (a *OpenAICompatible) BuildRequest(ctx context.Context, in RequestInput) (*http.Request, error) {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, UpstreamEndpoint(in.Channel.BaseURL, string(in.Endpoint)), bytes.NewReader(body))
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

func (a *OpenAICompatible) ParseResponse(ctx context.Context, in ResponseInput) (*NormalizedResponse, error) {
	_ = ctx
	usage := NewMissingUsage()
	responseBody := in.Body
	if in.StatusCode >= 200 && in.StatusCode < 300 {
		var err error
		usage, _, err = ParseOpenAICompatibleUsage(in.Body, in.Endpoint, metering.UsageSourceUpstream)
		if err != nil {
			return nil, err
		}
		if in.Endpoint == EndpointChatCompletions {
			responseBody, err = normalizeMiniMaxOpenAIEnvelope(in.Channel.Provider, in.Body)
			if err != nil {
				return nil, fmt.Errorf("normalize MiniMax response: %w", err)
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

// NormalizeStreamFrame strips only documented, non-model MiniMax control
// metadata. The gateway still validates every resulting OpenAI-compatible
// frame and fails closed on unknown or unsafe fields.
func (a *OpenAICompatible) NormalizeStreamFrame(ctx context.Context, in StreamChunkInput) ([]byte, error) {
	_ = ctx
	return normalizeMiniMaxOpenAIEnvelope(in.Channel.Provider, in.Data)
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
		rawMessage, exists := choice["message"]
		if !exists {
			continue
		}
		var message map[string]json.RawMessage
		if err := json.Unmarshal(rawMessage, &message); err != nil || message == nil {
			continue
		}
		messageChanged := false
		if rawName, exists := message["name"]; exists && rawJSONStringIs(rawName, "MiniMax AI") {
			delete(message, "name")
			messageChanged = true
		}
		if rawAudio, exists := message["audio_content"]; exists && (rawJSONStringIs(rawAudio, "") || string(rawAudio) == "null") {
			delete(message, "audio_content")
			messageChanged = true
		}
		if !messageChanged {
			continue
		}
		normalizedMessage, err := json.Marshal(message)
		if err != nil {
			return nil, false, err
		}
		choice["message"] = normalizedMessage
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
