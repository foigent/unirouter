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

func glmRequestIDDeliveryFixture(model, metadata string) []byte {
	return []byte(`{"id":"chatcmpl-glm",` + metadata + `"model":"` + model + `","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"reasoning_content":"fixture reasoning","tool_calls":[{"id":"call_1","type":"function","function":{"name":"unirouter_probe_echo","arguments":"{\"value\":\"UNIROUTER_TOOL_PROBE_OK\"}"}}]}}],"usage":{"prompt_tokens":37,"completion_tokens":243,"total_tokens":280}}`)
}

func TestExactGLMRequestIDProbeAndRuntimeDelivery(t *testing.T) {
	for _, tuple := range []struct{ model, profile, version, protocol string }{
		{"glm-5.3", "glm.5.3.api", "1.8.0", "protocol.glm-openai-chat.v5"},
		{"glm-5.2", "glm.5.2.api", "1.8.0", "protocol.glm-openai-chat.v6"},
		{"glm-5.1", "glm.5.1.api", "1.0.0", "protocol.glm-openai-chat.v6"},
		{"glm-5-turbo", "glm.5-turbo.api", "1.0.0", "protocol.glm-openai-chat.v6"},
		{"glm-5", "glm.5.api", "1.0.0", "protocol.glm-openai-chat.v6"},
		{"glm-5v-turbo", "glm.5v-turbo.api", "1.11.0", "protocol.glm-openai-chat.glm5v.v5"},
	} {
		t.Run(tuple.model, func(t *testing.T) {
			contract := modelcapabilityqualification.GLMToggleChatFunctionToolsProbeContractID
			if tuple.model == "glm-5.3" {
				contract = modelcapabilityqualification.GLMChatFunctionToolsProbeContractID
			}
			binding := modelcapabilityqualification.Binding{
				OperationID: modelprofile.OperationOpenAIChat, CapabilityID: modelprofile.CapabilityFunctionTools,
				ExecutionProfileID: tuple.profile, ExecutionProfileVersion: tuple.version,
				ProtocolProfileID: tuple.protocol, ProtocolProfileVersion: "1.0.0",
				ProbeContractID: contract, ProbeContractVersion: modelcapabilityqualification.OpenAIChatRuntimeFunctionToolsProbeContractV2,
				ExpectedProvider: "glm", ExpectedUpstreamModel: tuple.model, PublicPath: "/v1/chat/completions",
			}
			route := &RouteResult{
				Channel: models.Channel{Provider: "glm"}, MappedModel: tuple.model,
				ExecutionProfileID: tuple.profile, ExecutionProfileVersion: tuple.version,
				ModelProtocolProfile: tuple.protocol,
				PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
					CapabilityID:       modelprofile.CapabilityToolChoiceAuto,
					FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}},
				}},
			}
			for name, metadata := range map[string]string{
				"documented string": `"request_id":"provider-only",`,
				"empty string":      `"request_id":"",`,
				"invalid type":      `"request_id":null,`,
				"unknown sibling":   `"request_id":"provider-only","unknown_sibling":true,`,
			} {
				t.Run(name, func(t *testing.T) {
					body := glmRequestIDDeliveryFixture(tuple.model, metadata)
					var raw map[string]interface{}
					require.NoError(t, decodeJSONUseNumber(body, &raw))
					require.False(t, inspectNonStreamOutputTextsForRoute(raw, route).complete(), "original documented envelope must demonstrate the root mismatch")
					normalized, err := adapters.NewOpenAICompatible().ParseResponse(context.Background(), adapters.ResponseInput{
						Endpoint: adapters.EndpointChatCompletions, StatusCode: 200, Body: body,
						Channel: models.Channel{Provider: "glm"}, UpstreamModel: tuple.model,
						ModelOperationID:   modelprofile.OperationOpenAIChat,
						ExecutionProfileID: tuple.profile, ExecutionProfileVersion: tuple.version,
						ProtocolProfile: tuple.protocol, OperationProtocolProfileID: tuple.protocol, OperationProtocolProfileVersion: "1.0.0",
					})
					require.NoError(t, err)
					require.Equal(t, 37, normalized.Usage.PromptTokens)
					require.Equal(t, 243, normalized.Usage.CompletionTokens)
					_, probeErr := modelcapabilityqualification.ValidateNormalizedOpenAIChatFunctionToolsForBinding(binding, "application/json", bytes.NewReader(normalized.Body))
					if name == "invalid type" {
						require.Error(t, probeErr)
					} else {
						require.NoError(t, probeErr)
					}
					var object map[string]interface{}
					require.NoError(t, decodeJSONUseNumber(normalized.Body, &object))
					inspection := inspectNonStreamOutputTextsForRoute(object, route)
					require.Equal(t, name == "documented string" || name == "empty string", inspection.complete(), inspection.incompletePaths)
				})
			}
		})
	}
}

func TestExactGLMInvalidRequestIDRetainsProviderUsageAndWithholdsDelivery(t *testing.T) {
	var held billing.SettleRequest
	var holdReason string
	deliveryPrepared := false
	b := &mockBilling{
		holdOutputFn: func(_ context.Context, request billing.SettleRequest, reason string) error {
			held, holdReason = request, reason
			return nil
		},
		prepareDeliveryFn: func(context.Context, billing.SettleRequest) (*billing.PrepareDeliveryResult, error) {
			deliveryPrepared = true
			return nil, nil
		},
	}
	relay := newTestRelay(&testutil.MockDB{}, b)
	route := &RouteResult{
		Channel:    models.Channel{ID: 1, Provider: "glm", Name: "glm", AdapterType: adapters.TypeOpenAICompatible},
		ChannelKey: models.ChannelKey{ID: 1}, MappedModel: "glm-5.3", ModelOperationID: modelprofile.OperationOpenAIChat,
		ExecutionProfileID: "glm.5.3.api", ExecutionProfileVersion: "1.8.0",
		ModelProtocolProfile: "protocol.glm-openai-chat.v5", OperationProtocolProfileID: "protocol.glm-openai-chat.v5", OperationProtocolProfileVersion: "1.0.0",
		PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{
			CapabilityID:       modelprofile.CapabilityToolChoiceAuto,
			FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}},
		}},
	}
	response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(glmRequestIDDeliveryFixture("glm-5.3", `"request_id":null,`))))}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = contextWithRouteAttempt(ctx, 1, 1)
	ctx = contextWithAttemptID(ctx, "att-glm-metadata")
	recorder := httptest.NewRecorder()
	relay.relayNonStreamRequest(ctx, recorder, response, route, "req-glm-metadata", 1, 2,
		"glm-5.3", "glm-5.3", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 37, 243, "")
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.False(t, deliveryPrepared)
	require.Equal(t, billing.OutputHoldReasonProviderResponseUnusable, holdReason)
	require.Equal(t, 37, held.PromptTokens)
	require.Equal(t, 243, held.CompletionTokens)
	require.Equal(t, metering.UsagePurposeProviderEvidence, held.UsagePurpose)
	require.Equal(t, "att-glm-metadata", held.AttemptID)
	require.NotContains(t, recorder.Body.String(), "unirouter_probe_echo")
}
