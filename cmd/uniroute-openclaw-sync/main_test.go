package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeHTTPDoer struct {
	do func(*http.Request) (*http.Response, error)
}

func (doer fakeHTTPDoer) Do(request *http.Request) (*http.Response, error) {
	return doer.do(request)
}

type fakeOpenClawConfig struct {
	existing      []map[string]any
	validated     []map[string]any
	written       []map[string]any
	validateCalls int
	writeCalls    int
}

func (config *fakeOpenClawConfig) ReadModels(context.Context, string) ([]map[string]any, error) {
	return config.existing, nil
}

func (config *fakeOpenClawConfig) ValidateModels(_ context.Context, _ string, models []map[string]any) error {
	config.validateCalls++
	config.validated = models
	return nil
}

func (config *fakeOpenClawConfig) WriteModels(_ context.Context, _ string, models []map[string]any) error {
	config.writeCalls++
	config.written = models
	return nil
}

func TestModelCatalogURLAcceptsGatewayRootOrV1(t *testing.T) {
	root, err := modelCatalogURL("https://gateway.example.com/")
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example.com/v1/models?include=execution_profile", root)

	v1, err := modelCatalogURL("https://gateway.example.com/proxy/v1")
	require.NoError(t, err)
	require.Equal(t, "https://gateway.example.com/proxy/v1/models?include=execution_profile", v1)

	_, err = modelCatalogURL("https://key@gateway.example.com/v1")
	require.ErrorContains(t, err, "must not contain credentials")
}

func TestSynchronizeProjectsVerifiedProfilesAndPreservesUserOverrides(t *testing.T) {
	t.Setenv("TEST_UNIROUTER_API_KEY", "secret-must-not-appear")
	doer := fakeHTTPDoer{do: func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/models", request.URL.Path)
		require.Equal(t, "execution_profile", request.URL.Query().Get("include"))
		require.Equal(t, "Bearer secret-must-not-appear", request.Header.Get("Authorization"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
              "data": [
                {
                  "id": "Kimi K2.6",
                  "x_unirouter_execution": {
                    "status": "verified",
                    "display_name": "Kimi K2.6",
                    "context_window": 262144,
                    "max_output_tokens": 32768,
                    "capabilities": ["reasoning", "tools", "vision", "video"]
                  }
                },
                {
                  "id": "custom-model",
                  "x_unirouter_execution": {
                    "status": "unverified",
                    "display_name": "Custom Model"
                  }
                }
              ]
            }`)),
		}, nil
	}}
	config := &fakeOpenClawConfig{existing: []map[string]any{
		{
			"id": "Kimi K2.6", "name": "old name", "contextWindow": float64(1),
			"cost":   map[string]any{"input": float64(12)},
			"params": map[string]any{"temperature": float64(0.2)},
			"compat": map[string]any{"supportsStore": true},
		},
		{
			"id": "custom-model", "name": "old custom", "reasoning": true,
			"input": []any{"text", "image"}, "contextWindow": float64(128000), "maxTokens": float64(8192),
			"cost":   map[string]any{"input": float64(3)},
			"compat": map[string]any{"supportsTools": true, "supportsStore": true},
		},
		{"id": "no-longer-authorized", "name": "remove me"},
	}}

	report, err := synchronize(context.Background(), options{
		BaseURL: "https://gateway.example.com", APIKeyEnv: "TEST_UNIROUTER_API_KEY",
		Provider: "unirouter", Write: false,
	}, doer, config)

	require.NoError(t, err)
	require.Equal(t, syncReport{
		Provider: "unirouter", Models: 2, VerifiedModels: 1,
		UnverifiedModels: []string{"custom-model"}, RemovedModels: []string{"no-longer-authorized"}, Written: false,
	}, report)
	require.Equal(t, 1, config.validateCalls)
	require.Zero(t, config.writeCalls)
	require.Len(t, config.validated, 2)

	kimi := config.validated[0]
	require.Equal(t, "Kimi K2.6", kimi["name"])
	require.Equal(t, 262144, kimi["contextWindow"])
	require.Equal(t, 32768, kimi["maxTokens"])
	require.Equal(t, []string{"text", "image", "video"}, kimi["input"])
	require.Equal(t, map[string]any{"input": float64(12)}, kimi["cost"])
	require.Equal(t, map[string]any{"temperature": float64(0.2)}, kimi["params"])
	require.Equal(t, map[string]any{
		"supportsStore": true, "supportsTools": true, "supportsReasoningEffort": false,
	}, kimi["compat"])

	custom := config.validated[1]
	require.Equal(t, "Custom Model", custom["name"])
	require.NotContains(t, custom, "reasoning")
	require.NotContains(t, custom, "input")
	require.NotContains(t, custom, "contextWindow")
	require.NotContains(t, custom, "maxTokens")
	require.Equal(t, map[string]any{"input": float64(3)}, custom["cost"])
	require.Equal(t, map[string]any{"supportsStore": true}, custom["compat"])
}

func TestSynchronizeWriteOccursOnlyAfterValidation(t *testing.T) {
	t.Setenv("TEST_UNIROUTER_API_KEY", "key")
	doer := fakeHTTPDoer{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{
          "data":[{"id":"Kimi K3","x_unirouter_execution":{"status":"verified","display_name":"Kimi K3","context_window":262144,"max_output_tokens":32768,"capabilities":["reasoning","tools"],"reasoning_effort_values":["low","high","max"]}}]
        }`))}, nil
	}}
	config := &fakeOpenClawConfig{existing: []map[string]any{{"id": "Kimi K3", "name": "Kimi K3"}}}

	report, err := synchronize(context.Background(), options{
		BaseURL: "https://gateway.example.com/v1", APIKeyEnv: "TEST_UNIROUTER_API_KEY",
		Provider: "unirouter", Write: true,
	}, doer, config)

	require.NoError(t, err)
	require.True(t, report.Written)
	require.Equal(t, 1, config.validateCalls)
	require.Equal(t, 1, config.writeCalls)
	compat := config.written[0]["compat"].(map[string]any)
	require.Equal(t, true, compat["supportsReasoningEffort"])
	require.Equal(t, []string{"low", "high", "max"}, compat["supportedReasoningEfforts"])
}

func TestFetchCatalogDoesNotEchoUpstreamErrorBody(t *testing.T) {
	doer := fakeHTTPDoer{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader("sensitive-upstream-body")),
		}, nil
	}}

	_, err := fetchCatalog(context.Background(), doer, "https://gateway.example.com/v1/models", "key")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "sensitive-upstream-body")
	require.ErrorContains(t, err, "HTTP 401")
}
