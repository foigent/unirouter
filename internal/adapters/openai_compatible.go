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
	var payload map[string]interface{}
	if err := json.Unmarshal(in.DownstreamBody, &payload); err != nil {
		return nil, fmt.Errorf("decode downstream body: %w", err)
	}
	if mappedModel != "" {
		payload["model"] = mappedModel
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
	usage := Usage{Source: metering.UsageSourceUpstream}
	if in.StatusCode >= 200 && in.StatusCode < 300 {
		var parsed struct {
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(in.Body, &parsed)
		usage.PromptTokens = parsed.Usage.PromptTokens
		usage.CompletionTokens = parsed.Usage.CompletionTokens
		usage.TotalTokens = parsed.Usage.TotalTokens
		if usage.TotalTokens == 0 && usage.PromptTokens+usage.CompletionTokens > 0 {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
	}
	normalized := &NormalizedResponse{
		StatusCode: in.StatusCode,
		Header:     in.Header,
		Body:       in.Body,
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
		"max_tokens":  16,
		"temperature": 0,
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
		Body:       body,
		Error:      errorMessage,
		RequestURL: req.URL.String(),
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
