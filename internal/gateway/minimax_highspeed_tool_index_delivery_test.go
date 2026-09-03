package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

func highspeedToolIndexFixture(metadata string) []byte {
	return []byte(`{"id":"chatcmpl-fixture","object":"chat.completion","model":"MiniMax-M2.7-highspeed","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"<think>synthetic fixture</think>","tool_calls":[{` + metadata + `"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":600,"completion_tokens":7,"total_tokens":607,"prompt_tokens_details":{"cached_tokens":128}}}`)
}

func highspeedToolIndexRoute() *RouteResult {
	return &RouteResult{
		Channel: models.Channel{ID: 1, Provider: "minimax", AdapterType: adapters.TypeOpenAICompatible}, ChannelKey: models.ChannelKey{ID: 1},
		MappedModel: "MiniMax-M2.7-highspeed", ModelOperationID: modelprofile.OperationOpenAIChat,
		ExecutionProfileID: "minimax.m2.7-highspeed.payg", ExecutionProfileVersion: "1.2.0",
		ModelProtocolProfile: "protocol.minimax-m2-openai-chat.v7", OperationProtocolProfileID: "protocol.minimax-m2-openai-chat.v7", OperationProtocolProfileVersion: "1.0.0",
		CacheUsageProfile: "cache-usage.minimax-openai.v2",
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{CapabilityID: modelprofile.CapabilityToolChoiceAuto,
			FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}}}},
	}
}

func TestMiniMaxHighspeedToolIndexProbeAndRuntimeDelivery(t *testing.T) {
	route := highspeedToolIndexRoute()
	binding := modelcapabilityqualification.Binding{
		OperationID: route.ModelOperationID, CapabilityID: modelprofile.CapabilityFunctionTools,
		ExecutionProfileID: route.ExecutionProfileID, ExecutionProfileVersion: route.ExecutionProfileVersion,
		ProtocolProfileID: route.ModelProtocolProfile, ProtocolProfileVersion: "1.0.0",
		ProbeContractID:      modelcapabilityqualification.MiniMaxPAYGM2ChatFunctionToolsProbeContractID,
		ProbeContractVersion: modelcapabilityqualification.OpenAIChatRuntimeFunctionToolsProbeContractV2,
		ExpectedProvider:     "minimax", ExpectedUpstreamModel: route.MappedModel, PublicPath: "/v1/chat/completions",
	}
	for _, test := range []struct {
		name, metadata      string
		probeOK, deliveryOK bool
	}{
		{"missing", "", true, true},
		{"zero", `"index":0,`, true, true},
		{"large", `"index":99999999999999999999999999999,`, true, true},
		{"null", `"index":null,`, false, false},
		{"string", `"index":"0",`, false, false},
		{"negative", `"index":-1,`, false, false},
		{"fraction", `"index":0.0,`, false, false},
		{"exponent", `"index":1e0,`, false, false},
		{"unknown sibling", `"index":0,"unknown_sibling":true,`, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err := adapters.NewOpenAICompatible().ParseResponse(context.Background(), adapters.ResponseInput{
				Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: highspeedToolIndexFixture(test.metadata),
				Channel: route.Channel, UpstreamModel: route.MappedModel, ModelOperationID: route.ModelOperationID,
				ExecutionProfileID: route.ExecutionProfileID, ExecutionProfileVersion: route.ExecutionProfileVersion,
				ProtocolProfile: route.ModelProtocolProfile, OperationProtocolProfileID: route.OperationProtocolProfileID,
				OperationProtocolProfileVersion: route.OperationProtocolProfileVersion, CacheUsageProfile: route.CacheUsageProfile,
			})
			require.NoError(t, err)
			require.Equal(t, 607, out.Usage.TotalTokens)
			observation, err := modelcapabilityqualification.ValidateNormalizedOpenAIChatFunctionToolsForBinding(binding, "application/json", bytes.NewReader(out.Body))
			if test.probeOK {
				require.NoError(t, err)
				require.Equal(t, int64(128), observation.CachedTokens)
			} else {
				require.Equal(t, "delivery_shape", modelcapabilityqualification.OpenAIChatFunctionToolsErrorCode(err))
			}
			var object map[string]interface{}
			require.NoError(t, decodeJSONUseNumber(out.Body, &object))
			inspection := inspectNonStreamOutputTextsForRoute(object, route)
			require.Equal(t, test.deliveryOK, inspection.complete(), inspection.incompletePaths)
		})
	}
	// Raw indexes cannot qualify by bypassing the exact adapter preparation.
	_, err := modelcapabilityqualification.ValidateNormalizedOpenAIChatFunctionToolsForBinding(binding, "application/json", bytes.NewReader(highspeedToolIndexFixture(`"index":0,`)))
	require.Equal(t, "delivery_shape", modelcapabilityqualification.OpenAIChatFunctionToolsErrorCode(err))
	for _, version := range []string{"1.0.0", "1.1.0"} {
		historical := binding
		historical.ExecutionProfileVersion = version
		historical.ProbeContractVersion = modelcapabilityqualification.OpenAIChatFunctionToolsProbeContractVersion
		_, err := modelcapabilityqualification.ValidateNormalizedOpenAIChatFunctionToolsForBinding(historical, "application/json", bytes.NewReader(highspeedToolIndexFixture(`"index":0,`)))
		require.NoError(t, err, "historical typed Probe behavior remains unchanged")
	}
}

func TestMiniMaxHighspeedInvalidToolIndexRetainsUsageAndWithholdsDelivery(t *testing.T) {
	var held billing.SettleRequest
	var reason string
	deliveryPrepared := false
	b := &mockBilling{
		holdOutputFn: func(_ context.Context, request billing.SettleRequest, holdReason string) error {
			held, reason = request, holdReason
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			deliveryPrepared = true
			return nil, nil
		},
	}
	relay := newTestRelay(&testutil.MockDB{}, b)
	route := highspeedToolIndexRoute()
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(highspeedToolIndexFixture(`"index":null,`))))}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = contextWithRouteAttempt(ctx, 1, 1)
	ctx = contextWithAttemptID(ctx, "att-highspeed-index")
	recorder := httptest.NewRecorder()
	relay.relayNonStreamRequest(ctx, recorder, response, route, "req-highspeed-index", 1, 2, route.MappedModel, route.MappedModel,
		"127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 600, 7, "")
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.False(t, deliveryPrepared)
	require.Equal(t, billing.OutputHoldReasonProviderResponseUnusable, reason)
	require.Equal(t, 600, held.PromptTokens)
	require.Equal(t, 7, held.CompletionTokens)
	require.Equal(t, metering.UsagePurposeProviderEvidence, held.UsagePurpose)
	require.Equal(t, "att-highspeed-index", held.AttemptID)
	require.NotContains(t, recorder.Body.String(), "unirouter_probe_echo")
}
