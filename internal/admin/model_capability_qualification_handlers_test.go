package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
)

func TestModelCapabilityProbePreservesSafeContractFailureEvidence(t *testing.T) {
	encryptionKey := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("provider-secret"), encryptionKey)
	require.NoError(t, err)

	handler := NewHandler(nil, "capability-jwt", encryptionKey, nil)
	handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
		body := strings.Join([]string{
			`data: {"model":"deepseek-v4-flash","choices":[{"delta":{"content":"partial"},"finish_reason":null}]}`,
			"",
			"event: outside-the-typed-deepseek-contract",
			"",
		}, "\r\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}))

	outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
		Provider: "deepseek", BaseURL: "https://api.deepseek.com",
		AdapterType: adapters.TypeOpenAICompatible, AdapterConfig: json.RawMessage(`{}`),
		UpstreamModel: "deepseek-v4-flash", EncryptedSecret: encrypted,
	}, "capability-probe-test")

	require.True(t, outcome.ProviderSuccess)
	require.False(t, outcome.ContractSuccess)
	require.Equal(t, "provider_stream_contract_invalid", outcome.ReasonCode)
	require.Equal(t, "unsupported_field", outcome.ContractErrorCode)
	require.Equal(t, "deepseek-v4-flash", outcome.ReportedModel)
	require.Equal(t, 1, outcome.DataEventCount)
	require.Equal(t, 1, outcome.DeltaCount)
	require.Positive(t, outcome.ResponseBytes)
}
