package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
)

func TestKimiChatDeliveryKeepsCacheReadWritePartition(t *testing.T) {
	var sources []json.RawMessage
	for _, path := range []string{"kimi-k31-pro-chat-core", "kimi-k3-cache-read-write"} {
		raw, err := os.ReadFile("../../capability-bundles/" + path + ".json")
		require.NoError(t, err)
		bundle, err := publication.DecodeBundleV2(raw)
		require.NoError(t, err)
		sources = append(sources, bundle.Definitions...)
	}
	require.Len(t, sources, 3)
	for _, source := range sources {
		definition, err := modelprofile.DecodeDefinition(source)
		require.NoError(t, err)
		profile := definition.Profile()
		model := profile.UpstreamModelIDs[0]
		for _, tc := range []struct {
			name, cache string
			read        int
			fails       bool
		}{
			{"write only", `"prompt_tokens_details":{"cache_write_tokens":256}`, 0, false},
			{"read and write", `"cached_tokens":8,"prompt_tokens_details":{"cached_tokens":8,"cache_write_tokens":256}`, 8, false},
			{"conflicting mirror", `"cached_tokens":7,"prompt_tokens_details":{"cached_tokens":8,"cache_write_tokens":256}`, 0, true},
		} {
			t.Run(model+"/"+tc.name, func(t *testing.T) {
				// Use the documented K3.1 usage-only terminal chunk after the
				// tool finish frame. The tool and all IDs are synthetic.
				body := strings.ReplaceAll(`data: {"id":"fixture","object":"chat.completion.chunk","created":1,"model":"[MODEL]","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Check the echo.","tool_calls":[{"index":0,"id":"call_echo","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}]}

data: {"id":"fixture","object":"chat.completion.chunk","created":1,"model":"[MODEL]","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: {"id":"fixture","object":"chat.completion.chunk","created":1,"model":"[MODEL]","choices":[],"usage":{"prompt_tokens":309,"completion_tokens":101,"total_tokens":410,[CACHE]}}

data: [DONE]

`, "[MODEL]", model)
				body = strings.Replace(body, "[CACHE]", tc.cache, 1)
				var settled billing.SettleRequest
				settlementCalls := 0
				relay := newTestRelay(p6f11RelayDB(), &mockBilling{settleFn: func(_ context.Context, request billing.SettleRequest) (*billing.SettleResult, error) {
					settled = request
					settlementCalls++
					return &billing.SettleResult{SettleID: 1, Success: true}, nil
				}})
				route := &RouteResult{
					ProfileDefinition: definition, MappedModel: model, Channel: models.Channel{ID: 1, Provider: "kimi"},
					ModelOperationID: modelprofile.OperationOpenAIChat, ExecutionProfileID: profile.ID, ExecutionProfileVersion: profile.Version,
					ModelProtocolProfile: profile.ProtocolProfile, OperationProtocolProfileID: profile.ProtocolProfile,
					OperationProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1,
					CacheUsageProfile:               profile.CacheUsageProfile, ReasoningProfile: profile.ReasoningProfile,
					PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
						CapabilityID:       modelprofile.CapabilityToolChoiceAuto,
						FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}},
					}},
				}
				recorder := httptest.NewRecorder()
				relay.relayStreamRequest(p6f11StreamContext(nil, "kimi-k31-fixture"), recorder,
					&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))},
					route, "kimi-k31-fixture", 1, 2, "public-model", "public-model", "127.0.0.1", nil,
					logger.New(logger.Fields{}), time.Now(), 1024, adapters.NewOpenAICompatible(), "")
				require.Equal(t, 1, settlementCalls)
				if tc.fails {
					require.Contains(t, recorder.Body.String(), "stream_error")
					require.NotContains(t, recorder.Body.String(), "data: [DONE]")
					require.Equal(t, metering.UsageSourceCounterV2, settled.UsageSource)
					require.Zero(t, settled.CacheCreationTokens)
					return
				}
				require.NotContains(t, recorder.Body.String(), "stream_error")
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
				require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)
				require.Equal(t, 309, settled.PromptTokens)
				require.Equal(t, 53-tc.read, settled.UncachedPromptTokens)
				require.Equal(t, tc.read, settled.CacheReadTokens)
				require.Equal(t, 256, settled.CacheCreationTokens)
				require.Equal(t, 101, settled.CompletionTokens)
			})
		}
	}
}
