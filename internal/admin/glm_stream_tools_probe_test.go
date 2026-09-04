package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/auth"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

func TestGLMStreamCapabilityProbeUsesExactSharedMechanics(t *testing.T) {
	key := []byte("0123456789abcdef01234567")
	encrypted, err := auth.EncryptKey([]byte("fixture-only"), key)
	require.NoError(t, err)
	fixture, err := os.ReadFile("../adapters/testdata/glm_stream_tools.sse")
	require.NoError(t, err)
	bindings, err := publication.CurrentBindings()
	require.NoError(t, err)
	count := 0
	for _, b := range bindings {
		if b.ProbeContractID != qualification.GLMStreamFunctionToolsProbeContractID || b.CapabilityID != modelprofile.CapabilityFunctionTools {
			continue
		}
		count++
		for _, invalidSibling := range []bool{false, true} {
			handler := NewHandler(nil, "fixture-jwt", key, nil)
			handler.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				var payload map[string]interface{}
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, true, payload["stream"])
				require.Equal(t, "auto", payload["tool_choice"])
				require.Equal(t, float64(1024), payload["max_tokens"])
				require.Equal(t, b.ExpectedUpstreamModel, payload["model"])
				require.Equal(t, "https://open.bigmodel.cn/api/paas/v4/chat/completions", request.URL.String())
				response := strings.ReplaceAll(string(fixture), "glm-5.3", b.ExpectedUpstreamModel)
				if invalidSibling {
					response = strings.Replace(response, `"usage":`, `"unexpected":true,"usage":`, 1)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
			}))
			outcome := handler.executeModelCapabilityProbe(context.Background(), modelCapabilitySnapshot{
				Provider: "glm", BaseURL: "https://open.bigmodel.cn/api/paas/v4", AdapterType: adapters.TypeOpenAICompatible,
				AdapterConfig: json.RawMessage(`{"endpoint_path_mode":"base_relative"}`),
				UpstreamModel: b.ExpectedUpstreamModel, OperationID: b.OperationID, CapabilityID: b.CapabilityID,
				ExecutionProfileID: b.ExecutionProfileID, ExecutionProfileVersion: b.ExecutionProfileVersion,
				ProtocolProfileID: b.ProtocolProfileID, ProtocolProfileVersion: b.ProtocolProfileVersion,
				ExpectedProvider: "glm", ExpectedUpstreamModel: b.ExpectedUpstreamModel, PublicPath: b.PublicPath,
				EncryptedSecret: encrypted, ProbeContractID: b.ProbeContractID, ProbeContractVersion: b.ProbeContractVersion,
			}, "glm-local-fixture")
			require.True(t, outcome.ProviderSuccess)
			require.Equal(t, !invalidSibling, outcome.ContractSuccess)
			require.Equal(t, int64(100), outcome.PromptTokens)
			require.True(t, outcome.CacheReadCounterObserved)
			require.Equal(t, int64(10), outcome.CachedTokens)
		}
	}
	require.Equal(t, 6, count)
}
