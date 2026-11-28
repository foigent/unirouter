package gateway

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/uniroute/unirouter/internal/middleware"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func TestQwenHostedOrdinaryChatSeparateLimits(t *testing.T) {
	wire, err := os.ReadFile("../../capability-bundles/qwen-beijing-hosted-chat.json")
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	for _, encoded := range bundle.Definitions {
		d, err := modelprofile.DecodeDefinition(encoded)
		require.NoError(t, err)
		p := d.Profile()
		t.Run(p.UpstreamModelIDs[0], func(t *testing.T) {
			plan := providerCompletionTestPlan(t)
			plan.Definition = d
			plan.ExecutionProfileID, plan.ExecutionProfileVersion = p.ID, p.Version
			plan.ReasoningProfile, plan.CacheUsageProfile, plan.ModelProtocolProfile = p.ReasoningProfile, p.CacheUsageProfile, p.ProtocolProfile
			plan.PublicOutputLimit, plan.PublicContextLimit, plan.RequestDefaultOutputLimit = 8192, 32768, 512
			plan.Routes[0].UpstreamModel = p.UpstreamModelIDs[0]
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, []byte("0123456789abcdef0123456789abcdef"))
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
			is27B := p.ReasoningProfile == modelprofile.QwenHosted27BReasoning
			for _, tc := range []struct {
				name, fields string
				thinking     int
				ok           bool
			}{
				{"explicit thinking", `,"thinking_budget":64`, 64, true},
				{"explicit disable", `,"enable_thinking":false`, 0, is27B},
				{"disable with budget", `,"enable_thinking":false,"thinking_budget":64`, 0, false},
				{"missing budget", "", 0, false},
				{"wrong control", `,"thinking_budget":64,"reasoning_effort":"high"`, 0, false},
				{"above budget", `,"thinking_budget":4097`, 0, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":32` + tc.fields + `}`)
					visible := 32
					batch, err := relay.prepareModelExecutionRoutes(t.Context(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions, "public-model", "public-model", "fixture", modelExecutionRequestLimits{ExplicitOutputTokens: &visible}, body, time.Now())
					if !tc.ok {
						require.Error(t, err)
						return
					}
					require.NoError(t, err)
					require.Equal(t, visible+tc.thinking, batch.limits.EstimatedOutputTokens)
					require.Equal(t, tc.thinking, batch.limits.HostedReasoningTokenLimit)
					request := executionRequest(http.MethodPost, "/v1/chat/completions/preflight", "hosted-fixture", string(body))
					budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
					recorder := httptest.NewRecorder()
					handler := payloadpolicy.RequestMiddleware(budget, payloadpolicy.ProfileChatRequest, middleware.GetOrgIDFromContext)(
						middleware.StrictJSONIngress(http.HandlerFunc(relay.HandleChatPreflight)),
					)
					handler.ServeHTTP(recorder, request)
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					var preflight chatPreflightResult
					require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &preflight))
					require.True(t, preflight.LocallyAdmissible, recorder.Body.String())
					require.Equal(t, visible+tc.thinking, preflight.AcceptedCompletionTokens)
					require.Equal(t, 22, preflight.ProviderAnswerTokens)
					require.NotNil(t, preflight.ProviderReasoningTokens)
					require.Equal(t, tc.thinking, *preflight.ProviderReasoningTokens)
					outbound, err := providerRequestBody(batch.routes[0].upstreamRequest)
					require.NoError(t, err)
					var actual map[string]any
					require.NoError(t, json.Unmarshal(outbound, &actual))
					require.Equal(t, p.UpstreamModelIDs[0], actual["model"])
					require.EqualValues(t, 22, actual["max_tokens"])
					require.NotContains(t, actual, "max_completion_tokens")
					if tc.thinking == 0 {
						require.Equal(t, false, actual["enable_thinking"])
						require.NotContains(t, actual, "thinking_budget")
					} else {
						require.EqualValues(t, tc.thinking, actual["thinking_budget"])
					}
				})
			}
		})
	}
}

func TestQwenHostedFinalUsageChecksEachLimit(t *testing.T) {
	state := streamReservationState{ReservedCompletionTokens: 96, HostedReasoningTokenLimit: 64, HostedOutputBudgetKnown: true}
	for _, tc := range []struct {
		name, partition string
		split           bool
	}{
		{"within", `"completion_tokens":96,"total_tokens":106,"completion_tokens_details":{"reasoning_tokens":64,"text_tokens":96}`, true},
		{"inclusive official text", `"completion_tokens":44,"total_tokens":54,"completion_tokens_details":{"reasoning_tokens":15,"text_tokens":44}`, true},
		{"reasoning exceeds despite small total", `"completion_tokens":66,"total_tokens":76,"completion_tokens_details":{"reasoning_tokens":65}`, false},
		{"answer exceeds despite small total", `"completion_tokens":40,"total_tokens":50,"completion_tokens_details":{"reasoning_tokens":7}`, false},
		{"cannot infer partition", `"completion_tokens":40,"total_tokens":50`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := []byte(`{"usage":{"prompt_tokens":10,` + tc.partition + `}}`)
			usage, present, err := adapters.ParseOpenAICompatibleStreamUsageWithCacheProfile(response, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, modelprofile.QwenHostedSplitUsageProfile)
			require.NoError(t, err)
			require.True(t, present)
			require.Equal(t, tc.split, hostedCompletionFits(state, usage))
		})
	}
}

func TestQwenHostedRelayReleasesOnlyAfterFinalPartition(t *testing.T) {
	p := modelprofile.Profile{
		Provider: "dashscope", UpstreamModelIDs: []string{"qwen3.8-27b"},
		ProtocolProfile: modelprofile.QwenHostedSplitProtocol, CacheUsageProfile: modelprofile.QwenHostedSplitUsageProfile,
	}
	wire := existingFamilyRelayFixture(t, p, "answer")
	for _, tc := range []struct {
		name   string
		wire   string
		result string
	}{
		{"exact", strings.Replace(wire, `"completion_tokens":20`, `"completion_tokens":20,"completion_tokens_details":{"reasoning_tokens":12,"text_tokens":20}`, 1), "data: [DONE]"},
		{"partition absent", wire, "stream_error.invalid_frame"},
		{"reasoning over while sum fits", strings.Replace(wire, `"completion_tokens":20`, `"completion_tokens":20,"completion_tokens_details":{"reasoning_tokens":19}`, 1), "stream_error.provider_output_limit_exceeded"},
		{"unclosed", strings.TrimSuffix(wire, "data: [DONE]\n\n"), "stream_error.invalid_frame"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var settled, held int
			b := &mockBilling{
				settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					settled++
					require.Equal(t, 20, req.CompletionTokens)
					return &billing.SettleResult{Success: true, SettleID: 1}, nil
				},
				holdOutputFn: func(_ context.Context, req billing.SettleRequest, reason string) error {
					held++
					require.Equal(t, 20, req.CompletionTokens)
					require.Equal(t, billing.OutputHoldReasonProviderResponseUnusable, reason)
					return nil
				},
			}
			relay := newTestRelay(p6f11RelayDB(), b)
			relay.modelExecution = &modelExecutionRuntime{coordinator: &executionCoordinatorStub{}}
			enforcement, ok := modelprofile.ResolveOutputEnforcement(p.ProtocolProfile)
			require.True(t, ok)
			ctx := contextWithExecutionOutputBudget(p6f11StreamContext(nil, "hosted-completion"), modelExecutionRequestLimits{
				EstimatedOutputTokens: 96, HostedReasoningTokenLimit: 16, OutputEnforcement: enforcement,
			})
			ctx = contextWithExecutionDecision(ctx, modelexecution.Decision{Status: modelexecution.Applied})
			route := &RouteResult{MappedModel: p.UpstreamModelIDs[0], Channel: models.Channel{ID: 1, Provider: p.Provider},
				CacheUsageProfile: p.CacheUsageProfile, ModelProtocolProfile: p.ProtocolProfile}
			rec := httptest.NewRecorder()
			providerResponse := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.wire))}
			relay.relayStreamRequest(ctx, rec, providerResponse, route, "hosted-completion", 1, 2,
				"public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
			body := rec.Body.String()
			require.Contains(t, body, tc.result)
			if tc.name == "exact" {
				require.Contains(t, body, "Finished.")
				require.Equal(t, 1, settled)
				require.Zero(t, held)
			} else {
				require.NotContains(t, body, "Finished.")
				require.NotContains(t, body, "data: [DONE]")
				require.Zero(t, settled)
				if tc.name != "unclosed" {
					require.Equal(t, 1, held)
				}
			}
		})
	}
}

func TestQwenHostedNonStreamUsesInclusiveTextAndChecksReasoning(t *testing.T) {
	enforcement, ok := modelprofile.ResolveOutputEnforcement(modelprofile.QwenHostedSplitProtocol)
	require.True(t, ok)
	route := &RouteResult{MappedModel: "qwen3.8-27b", Channel: models.Channel{ID: 1, Provider: "dashscope"},
		CacheUsageProfile: modelprofile.QwenHostedSplitUsageProfile, ModelProtocolProfile: modelprofile.QwenHostedSplitProtocol}
	for _, tc := range []struct {
		name, details                      string
		completion, reasoningLimit, status int
	}{
		{"inclusive", `"text_tokens":44,"reasoning_tokens":15`, 44, 16, http.StatusOK},
		{"disabled with only text count", `"text_tokens":29`, 29, 0, http.StatusOK},
		{"active without reasoning count", `"text_tokens":44`, 44, 16, http.StatusBadGateway},
		{"missing upstream usage", "", 0, 16, http.StatusBadGateway},
		{"reasoning too high while sum fits", `"text_tokens":44,"reasoning_tokens":17`, 44, 16, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settled, held := 0, 0
			b := &mockBilling{
				settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					settled++
					require.Equal(t, tc.completion, req.CompletionTokens)
					return &billing.SettleResult{Success: true, SettleID: 1}, nil
				},
				holdOutputFn: func(_ context.Context, req billing.SettleRequest, reason string) error {
					held++
					require.Equal(t, tc.completion, req.CompletionTokens)
					require.Equal(t, billing.OutputHoldReasonProviderResponseUnusable, reason)
					return nil
				},
			}
			relay := newTestRelay(p6f11RelayDB(), b)
			relay.modelExecution = &modelExecutionRuntime{coordinator: &executionCoordinatorStub{}}
			limits := modelExecutionRequestLimits{
				EstimatedOutputTokens:     32 + tc.reasoningLimit,
				HostedReasoningTokenLimit: tc.reasoningLimit,
				OutputEnforcement:         enforcement,
			}
			ctx := contextWithExecutionOutputBudget(p6f11StreamContext(nil, "hosted-completion"), limits)
			ctx = contextWithExecutionDecision(ctx, modelexecution.Decision{Status: modelexecution.Applied})
			response := fmt.Sprintf(`{"id":"fixture","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"Finished."},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":%d,"total_tokens":%d,"completion_tokens_details":{%s}}}`, route.MappedModel, tc.completion, 10+tc.completion, tc.details)
			if tc.details == "" {
				response = response[:strings.Index(response, `,"usage":`)] + "}"
			}
			rec := httptest.NewRecorder()
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(response))}
			relay.relayNonStreamRequest(ctx, rec, resp, route, "hosted-completion", 1, 2, "public-model", "public-model",
				"127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 10, limits.EstimatedOutputTokens, "")
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.status == http.StatusOK {
				require.Contains(t, rec.Body.String(), "Finished.")
				require.Equal(t, 1, settled)
				require.Zero(t, held)
			} else {
				require.NotContains(t, rec.Body.String(), "Finished.")
				require.Zero(t, settled)
				if tc.details == "" {
					require.Zero(t, held, "do not put an estimate in the provider-usage hold")
				} else {
					require.Equal(t, 1, held)
				}
			}
		})
	}
}
