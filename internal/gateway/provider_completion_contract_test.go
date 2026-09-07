package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func providerCompletionTestPlan(t *testing.T) modelrouting.Plan {
	t.Helper()
	p, ok := modelprofile.Lookup("qwen.3.7-flash.api", "1.7.0")
	require.True(t, ok)
	p.Version, p.ProtocolProfile = "99.0.0", "protocol.qwen-openai-chat.flash.v8"
	p.Capabilities = []string{modelprofile.CapabilityStreaming, modelprofile.CapabilityReasoning, modelprofile.CapabilityReasoningBudget, modelprofile.CapabilityTools, modelprofile.CapabilityFunctionTools, modelprofile.CapabilityToolChoiceAuto}
	p.EvidenceRefs = []string{"https://help.aliyun.com/zh/model-studio/qwen3-7-flash", "https://help.aliyun.com/zh/model-studio/qwen-api-via-openai-chat-completions"}
	d, err := modelprofile.NewDefinition(p, "dashscope/qwen3.7-flash", "")
	require.NoError(t, err)
	key := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://dashscope.aliyuncs.com/compatible-mode/v1", key, nil)
	setExecutionTestProfile(t, &plan, p.ID, "1.7.0")
	plan.Definition, plan.ExecutionProfileVersion, plan.ModelProtocolProfile = d, p.Version, p.ProtocolProfile
	plan.PublicOutputLimit, plan.RequestDefaultOutputLimit = p.ProviderMaxOutput, 8192
	plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = p.Provider, p.UpstreamModelIDs[0]
	plan.Routes[0].DefaultMaxOutput = 8192
	plan.Routes[0].MaxContext = p.DefaultContextWindow
	plan.PublicContextLimit = p.DefaultContextWindow
	return plan
}

func TestProviderCompletionAdmissionFreezesAcceptedAndEmittedLimits(t *testing.T) {
	plan := providerCompletionTestPlan(t)
	for _, n := range []int{0, 11, 8192, 131072} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			request := map[string]any{"model": "public-model", "messages": []any{map[string]any{"role": "user", "content": "Hello"}}, "stream": true, "enable_thinking": false}
			var explicit *int
			accepted := n
			if n == 0 {
				accepted = 8192
			} else {
				request["max_completion_tokens"] = n
				explicit = &n
			}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			r := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, []byte("0123456789abcdef0123456789abcdef"))
			require.NoError(t, r.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
			batch, err := r.prepareModelExecutionRoutes(t.Context(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions, "public-model", "public-model", "completion-contract", modelExecutionRequestLimits{ExplicitOutputTokens: explicit}, body, time.Now())
			require.NoError(t, err)
			require.Equal(t, accepted, batch.limits.EstimatedOutputTokens, "reservation and context admission retain the accepted total")
			require.Equal(t, accepted-10, batch.limits.ProviderOutputLimit)
			for range 2 {
				wire, _, err := prepareExecutionRouteBody(adapters.EndpointChatCompletions, batch, batch.plan.Routes[0])
				require.NoError(t, err)
				var sent map[string]any
				require.NoError(t, json.Unmarshal(wire, &sent))
				require.Equal(t, float64(accepted-10), sent["max_completion_tokens"], "repeated preparation never subtracts twice")
				require.NotContains(t, sent, "max_tokens")
			}
			wire, err := providerRequestBody(batch.routes[0].upstreamRequest)
			require.NoError(t, err)
			var sent map[string]any
			require.NoError(t, json.Unmarshal(wire, &sent))
			require.Equal(t, float64(accepted-10), sent["max_completion_tokens"])
		})
	}
	for _, fields := range []string{`"max_tokens":8192`, `"max_completion_tokens":10`, `"max_completion_tokens":0`, `"max_completion_tokens":8192,"max_tokens":8192`} {
		_, err := validateChatExecutionProfileRequest([]byte(`{"model":"public-model","messages":[],"stream":true,`+fields+`}`), plan)
		require.Error(t, err, fields)
	}
	tooLarge := plan.PublicOutputLimit + 1
	_, err := (modelExecutionRequestLimits{ExplicitOutputTokens: &tooLarge}).resolveOutputReservation(plan, nil)
	require.ErrorIs(t, err, errModelExecutionOutputLimitExceeded)
}

func TestProviderCompletionStreamKeepsOldByteContractAndNewFinalUsageHold(t *testing.T) {
	for _, scenario := range []string{"old_bytes", "success", "overrun", "hold_failure", "health_failure", "interrupted"} {
		t.Run(scenario, func(t *testing.T) {
			p, _ := modelprofile.Lookup("qwen.3.7-flash.api", "1.7.0")
			p.ProtocolProfile = "protocol.qwen-openai-chat.flash.v8"
			if scenario == "old_bytes" {
				p.ProtocolProfile = "protocol.qwen-openai-chat.flash.v7"
			}
			wire := existingFamilyRelayFixture(t, p, "tools")
			if scenario == "overrun" || scenario == "hold_failure" || scenario == "health_failure" {
				wire = strings.ReplaceAll(strings.ReplaceAll(wire, `"completion_tokens":20`, `"completion_tokens":21`), `"total_tokens":120`, `"total_tokens":121`)
			}
			if scenario == "interrupted" {
				wire = wire[:strings.LastIndex(wire, "data: {")]
			}
			settles, holds, persisted := 0, 0, 0
			var settled billing.SettleRequest
			b := &mockBilling{
				settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					settles++
					settled = req
					return &billing.SettleResult{Success: true, SettleID: 1}, nil
				},
				persistUsageFn: func(context.Context, billing.SettleRequest) error { persisted++; return nil },
				holdStreamOutputFn: func(_ context.Context, req billing.SettleRequest, bytes int64, chunks int) error {
					holds++
					require.Equal(t, 21, req.CompletionTokens)
					require.Equal(t, metering.UsagePurposeProviderEvidence, req.UsagePurpose)
					require.NotNil(t, req.StreamFence)
					require.Positive(t, bytes)
					require.Positive(t, chunks)
					if scenario == "hold_failure" {
						return errors.New("storage unavailable")
					}
					return nil
				},
			}
			r := newTestRelay(p6f11RelayDB(), b)
			coordinator := &executionCoordinatorStub{}
			if scenario == "health_failure" {
				coordinator.providerFn = func(modelexecution.Decision) (modelexecution.HealthResult, error) {
					return modelexecution.HealthResult{}, errors.New("health unavailable")
				}
			}
			r.modelExecution = &modelExecutionRuntime{coordinator: coordinator}
			ctx := contextWithExecutionDecision(contextWithStreamReservation(p6f11StreamContext(nil, "completion-stream"), 20, false), modelexecution.Decision{Status: modelexecution.Applied})
			route := &RouteResult{MappedModel: p.UpstreamModelIDs[0], Channel: models.Channel{ID: 1, Provider: p.Provider}, CacheUsageProfile: p.CacheUsageProfile, ModelProtocolProfile: p.ProtocolProfile, PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{CapabilityID: modelprofile.CapabilityToolChoiceAuto, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}}}}}
			rec := httptest.NewRecorder()
			r.relayStreamRequest(ctx, rec, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(wire))}, route, "completion-stream", 1, 2, "public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
			result := rec.Body.String()
			switch scenario {
			case "success":
				require.Contains(t, result, "data: [DONE]")
				require.Equal(t, 1, settles)
				require.Equal(t, 20, settled.CompletionTokens)
				require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)
				require.Zero(t, holds)
			case "old_bytes":
				require.Contains(t, result, "stream_error.output_limit_exceeded")
				require.NotContains(t, result, "data: [DONE]")
				require.Zero(t, holds)
			case "interrupted":
				require.NotContains(t, result, "data: [DONE]")
				require.Equal(t, 1, settles)
				require.Equal(t, metering.UsageSourceCounterV2, settled.UsageSource)
				require.Zero(t, holds)
			default:
				require.Contains(t, result, "stream_error.provider_output_limit_exceeded")
				require.NotContains(t, result, "data: [DONE]")
				require.Equal(t, 1, holds)
				require.Zero(t, settles)
				require.Zero(t, persisted)
				require.EqualValues(t, 1, coordinator.providerCount.Load())
				require.Zero(t, coordinator.successCount.Load())
				if scenario == "hold_failure" {
					require.Contains(t, result, "provider_usage_hold_unpersisted")
				} else {
					require.Contains(t, result, "provider_cost_pending")
				}
			}
		})
	}
}

func TestBoundedStreamReaderCountsIgnoredFramesAndExactBoundary(t *testing.T) {
	for _, payload := range []string{"", ": comment\r\n\r\n", "data: 雪\n\n", strings.Repeat(":ignored\n", 100)} {
		for _, cap := range []int{0, len(payload), max(0, len(payload)-1), len(payload) + 1} {
			r := &boundedStreamReader{reader: strings.NewReader(payload), remaining: int64(cap)}
			_, err := io.Copy(io.Discard, r)
			if len(payload) > cap {
				require.ErrorIs(t, err, errStreamResourceLimit)
			} else {
				require.NoError(t, err)
			}
		}
	}
}

func TestProviderCompletionPreflightReportsAcceptedAndProviderLimits(t *testing.T) {
	r, planner, coordinator := newPreflightFixture(t)
	planner.plan = providerCompletionTestPlan(t)
	w, result := runChatPreflight(t, r, `{"model":"public-model","messages":[{"role":"user","content":"Hello"}],"stream":true,"enable_thinking":false,"max_completion_tokens":8192}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.True(t, result.LocallyAdmissible, w.Body.String())
	require.NotNil(t, result.OutputEnforcement)
	require.Equal(t, modelprofile.OutputEnforcementProviderCompletionV1, result.OutputEnforcement.Accounting)
	require.Equal(t, "reasoning_and_answer", result.OutputEnforcement.Scope)
	require.Equal(t, 8192, result.AcceptedCompletionTokens)
	require.Equal(t, 8182, result.ProviderCompletionTokens)
	require.Zero(t, coordinator.authorizeCount.Load())
}

func TestProviderCompletionNonStreamViolationIsHeldBeforeDisclosure(t *testing.T) {
	for _, protocol := range []string{"protocol.qwen-openai-chat.flash.v7", "protocol.qwen-openai-chat.flash.v8"} {
		t.Run(protocol, func(t *testing.T) {
			holds, settles := 0, 0
			b := &mockBilling{settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
				settles++
				return &billing.SettleResult{Success: true}, nil
			}, holdOutputFn: func(_ context.Context, req billing.SettleRequest, reason string) error {
				holds++
				require.Equal(t, 21, req.CompletionTokens)
				require.Equal(t, metering.UsagePurposeProviderEvidence, req.UsagePurpose)
				require.Equal(t, billing.OutputHoldReasonProviderResponseUnusable, reason)
				return nil
			}}
			r := newTestRelay(p6f11RelayDB(), b)
			rec := httptest.NewRecorder()
			wire := `{"id":"fixture","object":"chat.completion","created":1,"model":"qwen3.7-flash","choices":[{"index":0,"message":{"role":"assistant","content":"private-output-marker"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":21,"total_tokens":31}}`
			r.relayNonStreamRequest(contextWithAttemptID(t.Context(), "att_nonstream_contract"), rec, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(wire))}, &RouteResult{MappedModel: "qwen3.7-flash", Channel: testOpenAIChannel(), ModelProtocolProfile: protocol}, "op_nonstream_contract", 1, 2, "qwen3.7-flash", "qwen3.7-flash", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 10, 20, "")
			if protocol == "protocol.qwen-openai-chat.flash.v8" {
				require.Equal(t, 502, rec.Code, rec.Body.String())
				require.Equal(t, 1, holds)
				require.Zero(t, settles)
				require.NotContains(t, rec.Body.String(), "private-output-marker")
			} else {
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Equal(t, 1, settles)
				require.Zero(t, holds)
			}
		})
	}
}

func TestProviderCompletionDecodedResourceBoundStopsBeforeDisclosure(t *testing.T) {
	p, _ := modelprofile.Lookup("qwen.3.7-flash.api", "1.7.0")
	p.ProtocolProfile = "protocol.qwen-openai-chat.flash.v8"
	const chunkSize = 16 << 10
	content := strings.Repeat("x", chunkSize)
	frame := `data: {"id":"fixture","object":"chat.completion.chunk","created":1,"model":"qwen3.7-flash","choices":[{"index":0,"delta":{"content":"` + content + `"},"finish_reason":null}]}` + "\n\n"
	// One more frame than the decoded cap; every line remains within the
	// separately enforced parser line limit. No final provider usage exists.
	wire := strings.Repeat(frame, 1025)
	settles, holds := 0, 0
	b := &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
		settles++
		require.Equal(t, metering.UsageSourceCounterV2, req.UsageSource)
		return &billing.SettleResult{Success: true}, nil
	}, holdStreamOutputFn: func(context.Context, billing.SettleRequest, int64, int) error { holds++; return nil }}
	r := newTestRelay(p6f11RelayDB(), b)
	route := &RouteResult{MappedModel: p.UpstreamModelIDs[0], Channel: models.Channel{ID: 1, Provider: p.Provider}, CacheUsageProfile: p.CacheUsageProfile, ModelProtocolProfile: p.ProtocolProfile}
	rec := httptest.NewRecorder()
	ctx := contextWithStreamReservation(p6f11StreamContext(nil, "resource-limit"), 131072, false)
	r.relayStreamRequest(ctx, rec, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(wire))}, route, "resource-limit", 1, 2, "public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
	require.Contains(t, rec.Body.String(), "stream_error.resource_limit_exceeded")
	require.NotContains(t, rec.Body.String(), "data: [DONE]")
	require.Equal(t, 1024, strings.Count(rec.Body.String(), content), "the frame crossing the decoded cap is withheld")
	require.Equal(t, 1, settles)
	require.Zero(t, holds)
}
