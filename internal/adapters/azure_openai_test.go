package adapters

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/models"
)

func TestAzureOpenAIBuildRequestUsesDeploymentScopedGAEndpoint(t *testing.T) {
	adapter := NewAzureOpenAI()
	req, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint: EndpointChatCompletions,
		DownstreamBody: []byte(`{
			"model":"company-chat",
			"messages":[{"role":"user","content":"hello"}],
			"max_tokens":16
		}`),
		RequestedModel: "company-chat",
		MappedModel:    "gpt-4o-prod",
		Channel: models.Channel{
			BaseURL:       "https://contoso.openai.azure.com",
			Provider:      "azure",
			AdapterType:   TypeAzureOpenAI,
			AdapterConfig: json.RawMessage(`{"api_version":"2024-10-21","deployment":"gpt-4o-prod"}`),
		},
		APIKey:    "fixture-key",
		RequestID: "req-azure-1",
	})
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	if got, want := req.URL.String(), "https://contoso.openai.azure.com/openai/deployments/gpt-4o-prod/chat/completions?api-version=2024-10-21"; got != want {
		t.Fatalf("url=%q want %q", got, want)
	}
	if got := req.Header.Get("api-key"); got != "fixture-key" {
		t.Fatalf("api-key=%q", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("unexpected bearer header %q", got)
	}
	if got := req.Header.Get("X-Request-ID"); got != "req-azure-1" {
		t.Fatalf("request id=%q", got)
	}
	body, _ := io.ReadAll(req.Body)
	if strings.Contains(string(body), `"model"`) {
		t.Fatalf("deployment-scoped request retained conflicting model: %s", body)
	}
	if !strings.Contains(string(body), `"max_tokens":16`) {
		t.Fatalf("request lost exact integer field: %s", body)
	}
}

func TestAzureOpenAIBuildRequestSupportsEmbeddingsAndEntraBearer(t *testing.T) {
	adapter := NewAzureOpenAI()
	req, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointEmbeddings,
		DownstreamBody: []byte(`{"model":"company-embed","input":"hello"}`),
		MappedModel:    "embed-prod",
		Channel: models.Channel{
			BaseURL:       "https://contoso.services.ai.azure.com",
			AdapterType:   TypeAzureOpenAI,
			AdapterConfig: json.RawMessage(`{"api_version":"2024-10-21","auth_mode":"entra_bearer"}`),
		},
		APIKey: "fixture-entra-token",
	})
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	if got, want := req.URL.String(), "https://contoso.services.ai.azure.com/openai/deployments/embed-prod/embeddings?api-version=2024-10-21"; got != want {
		t.Fatalf("url=%q want %q", got, want)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer fixture-entra-token" {
		t.Fatalf("authorization=%q", got)
	}
	if got := req.Header.Get("api-key"); got != "" {
		t.Fatalf("unexpected api-key header %q", got)
	}
}

func TestAzureOpenAIBuildRequestEscapesDeploymentExactlyOnce(t *testing.T) {
	adapter := NewAzureOpenAI()
	req, err := adapter.BuildRequest(context.Background(), RequestInput{
		Endpoint:       EndpointEmbeddings,
		DownstreamBody: []byte(`{"model":"company-embed","input":"hello"}`),
		MappedModel:    "embed blue",
		Channel: models.Channel{
			BaseURL:     "https://contoso.openai.azure.com",
			AdapterType: TypeAzureOpenAI,
		},
		APIKey: "fixture-key",
	})
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}
	if got, want := req.URL.String(), "https://contoso.openai.azure.com/openai/deployments/embed%20blue/embeddings?api-version=2024-10-21"; got != want {
		t.Fatalf("url=%q want %q", got, want)
	}
}

func TestAzureOpenAIConfigRejectsAmbiguousOrUnsafeInputs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "duplicate", raw: `{"api_version":"2024-10-21","api_version":"2025-01-01"}`, want: "duplicate_json_key"},
		{name: "preview", raw: `{"api_version":"2025-04-01-preview"}`, want: "must be 2024-10-21"},
		{name: "unfrozen GA date", raw: `{"api_version":"2025-01-01"}`, want: "must be 2024-10-21"},
		{name: "unknown", raw: `{"api_version":"2024-10-21","tenant":"x"}`, want: "unknown field"},
		{name: "bad auth", raw: `{"auth_mode":"basic"}`, want: "auth_mode"},
		{name: "path deployment", raw: `{"deployment":"one/two"}`, want: "reserved URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAzureOpenAIConfig([]byte(tt.raw))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v want containing %q", err, tt.want)
			}
		})
	}
}

func TestAzureOpenAIRejectsUnsafeBaseURLAndMissingDeployment(t *testing.T) {
	adapter := NewAzureOpenAI()
	base := RequestInput{
		Endpoint:       EndpointChatCompletions,
		DownstreamBody: []byte(`{"messages":[]}`),
		Channel:        models.Channel{BaseURL: "https://contoso.openai.azure.com/path"},
	}
	if _, err := adapter.BuildRequest(context.Background(), base); err == nil || !strings.Contains(err.Error(), "deployment is required") {
		t.Fatalf("expected missing deployment, got %v", err)
	}
	base.MappedModel = "deployment"
	if _, err := adapter.BuildRequest(context.Background(), base); err == nil || !strings.Contains(err.Error(), "must not contain a path") {
		t.Fatalf("expected unsafe base_url rejection, got %v", err)
	}
	base.Channel.BaseURL = "http://contoso.openai.azure.com"
	if _, err := adapter.BuildRequest(context.Background(), base); err == nil || !strings.Contains(err.Error(), "HTTPS origin") {
		t.Fatalf("expected HTTP rejection, got %v", err)
	}
	base.Channel.BaseURL = "https://contoso.openai.azure.com"
	base.MappedModel = "unsafe/deployment"
	if _, err := adapter.BuildRequest(context.Background(), base); err == nil || !strings.Contains(err.Error(), "reserved URL") {
		t.Fatalf("expected mapped deployment rejection, got %v", err)
	}
}

func TestAzureOpenAIParseResponsePreservesUsageAuthorityAndErrorClass(t *testing.T) {
	adapter := NewAzureOpenAI()
	success, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint:   EndpointChatCompletions,
		StatusCode: http.StatusOK,
		Body:       []byte(`{"usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12}}`),
		Channel: models.Channel{
			ID: 7, Name: "azure-prod", Provider: "azure", AdapterType: TypeAzureOpenAI,
		},
	})
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if success.Usage.TotalTokens != 12 ||
		success.Usage.Authority != metering.UsageAuthorityProviderReported ||
		success.Usage.Finality != metering.UsageFinalityFinal {
		t.Fatalf("unexpected usage: %+v", success.Usage)
	}
	if success.Meta.AdapterType != TypeAzureOpenAI {
		t.Fatalf("adapter type=%q", success.Meta.AdapterType)
	}

	failure, err := adapter.ParseResponse(context.Background(), ResponseInput{
		Endpoint:   EndpointChatCompletions,
		StatusCode: http.StatusTooManyRequests,
		Body:       []byte(`{"error":{"code":"RateLimitReached","message":"slow down"}}`),
	})
	if err != nil {
		t.Fatalf("ParseResponse failure path failed: %v", err)
	}
	if failure.Error == nil || failure.Error.Code != "RateLimitReached" ||
		failure.Error.Message != "slow down" || failure.Error.FailureKind != FailureRateLimit ||
		!failure.Error.Retryable {
		t.Fatalf("unexpected normalized error: %+v", failure.Error)
	}
}

func TestAzureOpenAIConnectivityTestOmitsOptionalTemperature(t *testing.T) {
	adapter := NewAzureOpenAI()
	result, err := adapter.Test(context.Background(), TestInput{
		Channel: models.Channel{
			BaseURL:       "https://contoso.openai.azure.com",
			Provider:      "azure",
			AdapterType:   TypeAzureOpenAI,
			AdapterConfig: json.RawMessage(`{"api_version":"2024-10-21","deployment":"reasoning-prod"}`),
		},
		APIKey: "fixture-key",
		Model:  "reasoning-model",
		Client: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				t.Fatalf("read test request: %v", readErr)
			}
			var payload map[string]interface{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode test request: %v", err)
			}
			if _, exists := payload["temperature"]; exists {
				t.Fatalf("connectivity test must not force an optional temperature: %s", body)
			}
			if _, exists := payload["model"]; exists {
				t.Fatalf("deployment-scoped connectivity test must not send model: %s", body)
			}
			if _, exists := payload["max_tokens"]; exists {
				t.Fatalf("connectivity test must not force an optional token limit: %s", body)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"model":"gpt-4o-2026-08-06","usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("Test failed: %v", err)
	}
	if !result.Success || result.TokensUsed != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.ReportedModel != "gpt-4o-2026-08-06" {
		t.Fatalf("reported model = %q", result.ReportedModel)
	}
}

func TestDefaultRegistryIncludesAzureOpenAI(t *testing.T) {
	if !DefaultRegistry().Has(TypeAzureOpenAI) {
		t.Fatal("default registry does not include azure_openai")
	}
}
