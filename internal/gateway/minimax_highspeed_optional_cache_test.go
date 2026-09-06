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
	pub "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
)

func TestHighspeedDataProfileOptionalCacheRelaySettlesOnce(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/minimax-highspeed-stream-cache.json")
	require.NoError(t, err)
	bundle, err := pub.DecodeBundleV2(wire)
	require.NoError(t, err)
	definition, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	p := definition.Profile()
	// Constructed wire boundary; no private text or claim of another paid call.
	stream := existingFamilyRelayFixture(t, p, "tools")
	stream = strings.ReplaceAll(stream, `"prompt_tokens":100`, `"prompt_tokens":5554`)
	stream = strings.ReplaceAll(stream, `"total_tokens":120`, `"total_tokens":5574`)
	for _, cache := range []string{p.CacheUsageProfile, "cache-usage.minimax-openai.v2"} {
		for _, details := range []string{"", `"prompt_tokens_details":{"cached_tokens":0},`, `"prompt_tokens_details":{"cached_tokens":null},`, `"prompt_tokens_details":{"cached_tokens":-1},`, `"prompt_tokens_details":{"cached_tokens":6000},`} {
			t.Run(cache+"/"+details, func(t *testing.T) {
				input := strings.ReplaceAll(stream, `"prompt_tokens_details":{"cached_tokens":10},`, details)
				require.NotEqual(t, stream, input)
				count := 0
				var settled billing.SettleRequest
				relay := newTestRelay(p6f11RelayDB(), &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					count++
					settled = req
					return &billing.SettleResult{SettleID: 1, Success: true}, nil
				}})
				route := &RouteResult{MappedModel: p.UpstreamModelIDs[0], Channel: models.Channel{ID: 1, Provider: p.Provider}, CacheUsageProfile: cache, ModelProtocolProfile: p.ProtocolProfile, PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{CapabilityID: modelprofile.CapabilityToolChoiceAuto, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}}}}}
				recorder := httptest.NewRecorder()
				relay.relayStreamRequest(p6f11StreamContext(nil, "highspeed-cache"), recorder, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(input))}, route, "highspeed-cache", 1, 2, "public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
				require.Equal(t, 1, count)
				result := recorder.Body.String()
				pass := details == `"prompt_tokens_details":{"cached_tokens":0},` || (details == "" && cache == p.CacheUsageProfile)
				if !pass {
					require.Contains(t, result, "stream_error")
					require.NotContains(t, result, "data: [DONE]")
					require.Equal(t, metering.UsageSourceCounterV2, settled.UsageSource)
					return
				}
				require.NotContains(t, result, "stream_error")
				require.Equal(t, 1, strings.Count(result, "data: [DONE]"))
				require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)
				require.Equal(t, 5554, settled.PromptTokens)
				frames := 0
				for _, line := range strings.Split(result, "\n") {
					var frame map[string]any
					if strings.HasPrefix(line, "data: ") && json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame) == nil && frame["usage"] != nil && frame["id"] == "fixture" {
						frames++
						if details == "" {
							require.NotContains(t, frame["usage"], "prompt_tokens_details")
						}
					}
				}
				require.Equal(t, 1, frames)
			})
		}
	}
}
