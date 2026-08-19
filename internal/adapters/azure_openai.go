package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/metering"
)

const defaultAzureOpenAIAPIVersion = "2024-10-21"

type AzureOpenAI struct{}

type AzureOpenAIConfig struct {
	APIVersion string `json:"api_version"`
	Deployment string `json:"deployment"`
	AuthMode   string `json:"auth_mode,omitempty"`
}

func NewAzureOpenAI() *AzureOpenAI {
	return &AzureOpenAI{}
}

func (a *AzureOpenAI) Type() string {
	return TypeAzureOpenAI
}

func (a *AzureOpenAI) ValidateConfig(raw []byte) error {
	_, err := ParseAzureOpenAIConfig(raw)
	return err
}

func (a *AzureOpenAI) BuildRequest(ctx context.Context, in RequestInput) (*http.Request, error) {
	if in.Endpoint != EndpointChatCompletions && in.Endpoint != EndpointEmbeddings {
		return nil, fmt.Errorf("azure_openai does not support endpoint %s", in.Endpoint)
	}
	cfg, err := ParseAzureOpenAIConfig(in.Channel.AdapterConfig)
	if err != nil {
		return nil, err
	}
	deployment := strings.TrimSpace(cfg.Deployment)
	if deployment == "" {
		deployment = strings.TrimSpace(in.MappedModel)
	}
	if deployment == "" {
		deployment = strings.TrimSpace(in.RequestedModel)
	}
	if deployment == "" {
		return nil, errors.New("azure_openai deployment is required")
	}
	if err := validateAzureOpenAIDeployment(deployment); err != nil {
		return nil, err
	}

	payload, err := decodeAzureRequestObject(in.DownstreamBody)
	if err != nil {
		return nil, err
	}
	// Deployment-scoped Azure OpenAI endpoints select the deployment in the
	// path. Omitting model prevents a downstream alias from becoming a second,
	// conflicting routing authority.
	delete(payload, "model")
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode azure_openai upstream body: %w", err)
	}

	requestURL, err := azureOpenAIEndpoint(in.Channel.BaseURL, deployment, in.Endpoint, cfg.APIVersion)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	switch cfg.AuthMode {
	case "entra_bearer":
		req.Header.Set("Authorization", "Bearer "+in.APIKey)
	default:
		req.Header.Set("api-key", in.APIKey)
	}
	if in.RequestID != "" {
		req.Header.Set("X-Request-ID", in.RequestID)
	}
	return req, nil
}

func (a *AzureOpenAI) ParseResponse(ctx context.Context, in ResponseInput) (*NormalizedResponse, error) {
	_ = ctx
	usage := NewMissingUsage()
	if in.StatusCode >= 200 && in.StatusCode < 300 {
		var err error
		usage, _, err = ParseOpenAICompatibleUsage(in.Body, in.Endpoint, metering.UsageSourceUpstream)
		if err != nil {
			return nil, err
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
			AdapterType:    TypeAzureOpenAI,
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

func (a *AzureOpenAI) Test(ctx context.Context, in TestInput) (*TestResult, error) {
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
		return &TestResult{
			Success: false, Status: "error", LatencyMs: latency, Model: in.Model,
			Error: err.Error(), RequestURL: req.URL.String(),
		}, nil
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

func (a *AzureOpenAI) ClassifyHTTPStatus(statusCode int, body []byte) *NormalizedError {
	err := &NormalizedError{
		HTTPStatus: statusCode,
		Category:   "upstream_error",
		Code:       fmt.Sprintf("azure_openai_http_%d", statusCode),
		Message:    fmt.Sprintf("azure_openai returned HTTP %d", statusCode),
	}
	if code, message := extractAzureOpenAIError(body); code != "" || message != "" {
		if code != "" {
			err.Code = code
		}
		if message != "" {
			err.Message = message
		}
	}
	err.FailureKind, err.Retryable = classifyHTTPFailure(statusCode)
	return err
}

func ParseAzureOpenAIConfig(raw []byte) (AzureOpenAIConfig, error) {
	cfg := AzureOpenAIConfig{
		APIVersion: defaultAzureOpenAIAPIVersion,
		AuthMode:   "api_key",
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return cfg, nil
	}
	if strictErr := eventconnector.ValidateStrictJSON(raw, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return AzureOpenAIConfig{}, fmt.Errorf("invalid azure_openai adapter_config: %s", strictErr.Code)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return AzureOpenAIConfig{}, fmt.Errorf("decode azure_openai adapter_config: %w", err)
	}
	if err := ensureAzureConfigEOF(decoder); err != nil {
		return AzureOpenAIConfig{}, err
	}
	cfg.APIVersion = strings.TrimSpace(cfg.APIVersion)
	cfg.Deployment = strings.TrimSpace(cfg.Deployment)
	cfg.AuthMode = strings.TrimSpace(cfg.AuthMode)
	if cfg.APIVersion == "" {
		cfg.APIVersion = defaultAzureOpenAIAPIVersion
	}
	if cfg.AuthMode == "" {
		cfg.AuthMode = "api_key"
	}
	if !validAzureAPIVersion(cfg.APIVersion) {
		return AzureOpenAIConfig{}, fmt.Errorf("azure_openai api_version must be %s in V1", defaultAzureOpenAIAPIVersion)
	}
	switch cfg.AuthMode {
	case "api_key", "entra_bearer":
	default:
		return AzureOpenAIConfig{}, errors.New("azure_openai auth_mode must be api_key or entra_bearer")
	}
	if cfg.Deployment != "" {
		if err := validateAzureOpenAIDeployment(cfg.Deployment); err != nil {
			return AzureOpenAIConfig{}, err
		}
	}
	return cfg, nil
}

func ensureAzureConfigEOF(decoder *json.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("azure_openai adapter_config contains trailing JSON")
		}
		return fmt.Errorf("decode azure_openai adapter_config trailing content: %w", err)
	}
	return nil
}

func validAzureAPIVersion(value string) bool {
	return value == defaultAzureOpenAIAPIVersion
}

func validateAzureOpenAIDeployment(deployment string) error {
	if strings.ContainsAny(deployment, "/?#") {
		return errors.New("azure_openai deployment contains reserved URL characters")
	}
	return nil
}

func decodeAzureRequestObject(body []byte) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]interface{}
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode downstream body: %w", err)
	}
	if payload == nil {
		return nil, errors.New("decode downstream body: JSON object is required")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode downstream body: trailing JSON")
		}
		return nil, fmt.Errorf("decode downstream body trailing content: %w", err)
	}
	return payload, nil
}

func azureOpenAIEndpoint(baseURL, deployment string, endpoint Endpoint, apiVersion string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse azure_openai base_url: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("azure_openai base_url must be an HTTPS origin without credentials, query or fragment")
	}
	basePath := strings.TrimRight(parsed.EscapedPath(), "/")
	if basePath != "" && basePath != "/" {
		return "", errors.New("azure_openai base_url must not contain a path")
	}
	endpointPath := strings.TrimPrefix(string(endpoint), "/v1/")
	// URL.String escapes Path exactly once. Assigning PathEscape output to Path
	// would escape '%' again for valid names containing spaces or UTF-8.
	parsed.Path = "/openai/deployments/" + deployment + "/" + endpointPath
	parsed.RawPath = ""
	query := parsed.Query()
	query.Set("api-version", apiVersion)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func extractAzureOpenAIError(body []byte) (string, string) {
	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return "", ""
	}
	return strings.TrimSpace(parsed.Error.Code), strings.TrimSpace(parsed.Error.Message)
}
