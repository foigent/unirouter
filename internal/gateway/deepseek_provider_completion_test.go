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
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

func deepSeekCompletionDefinition(t *testing.T) modelprofile.Definition {
	t.Helper()
	return deepSeekBundleDefinition(t, "deepseek-v41-flash-provider-completion.json")
}

func deepSeekBundleDefinition(t *testing.T, name string) modelprofile.Definition {
	t.Helper()
	wire, err := os.ReadFile("../../capability-bundles/" + name)
	require.NoError(t, err)
	bundle, err := publication.DecodeBundleV2(wire)
	require.NoError(t, err)
	d, err := modelprofile.DecodeDefinition(bundle.Definitions[0])
	require.NoError(t, err)
	return d
}

func deepSeekCompletionFixture(t *testing.T) string {
	t.Helper()
	wire, err := os.ReadFile("../adapters/testdata/deepseek_provider_completion_v6.sse")
	require.NoError(t, err)
	first := strings.Index(string(wire), "\n\n") + 2
	large := "data: " + `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"deepseek-flash","choices":[{"index":0,"delta":{"reasoning_content":"` + strings.Repeat("A", 12288) + `"},"finish_reason":null}]}` + "\n\n"
	return string(wire[:first]) + large + string(wire[first:])
}

func TestDeepSeekProviderCompletionPreflightFreezeAndWireKeep2048(t *testing.T) {
	d := deepSeekCompletionDefinition(t)
	p := d.Profile()
	key := []byte("0123456789abcdef0123456789abcdef")
	plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.deepseek.com", key, nil)
	setExecutionTestProfile(t, &plan, "deepseek.v4-flash.api", "1.5.0")
	plan.Definition, plan.ExecutionProfileID, plan.ExecutionProfileVersion = d, p.ID, p.Version
	plan.ModelProtocolProfile, plan.ReasoningProfile, plan.CacheUsageProfile = p.ProtocolProfile, p.ReasoningProfile, p.CacheUsageProfile
	plan.PublicOutputLimit, plan.RequestDefaultOutputLimit = 2048, 2048
	plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = p.Provider, p.UpstreamModelIDs[0]
	plan.Routes[0].DefaultMaxOutput = 2048
	r := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
	coordinator := &executionCoordinatorStub{}
	require.NoError(t, r.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, coordinator, routingpolicy.Default()))
	for _, n := range []int{0, 1, 2048} {
		body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"Hello"}],"stream":true,"stream_options":{"include_usage":true}}`)
		var explicit *int
		if n > 0 {
			explicit = &n
			var obj map[string]any
			require.NoError(t, json.Unmarshal(body, &obj))
			obj["max_tokens"] = n
			body, _ = json.Marshal(obj)
		}
		accepted := n
		if n == 0 {
			accepted = 2048
		}
		w, result := runChatPreflight(t, r, string(body))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.True(t, result.LocallyAdmissible, w.Body.String())
		require.Equal(t, accepted, result.AcceptedCompletionTokens)
		require.Equal(t, accepted, result.ProviderCompletionTokens)
		require.Zero(t, result.OutputEnforcement.ProviderDeviationTokens)
		batch, err := r.prepareModelExecutionRoutes(t.Context(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions, "public-model", "public-model", "deepseek-completion", modelExecutionRequestLimits{ExplicitOutputTokens: explicit}, body, time.Now())
		require.NoError(t, err)
		require.Equal(t, accepted, batch.limits.EstimatedOutputTokens)
		for range 2 {
			wire, _, e := prepareExecutionRouteBody(adapters.EndpointChatCompletions, batch, batch.plan.Routes[0])
			require.NoError(t, e)
			var obj map[string]any
			require.NoError(t, json.Unmarshal(wire, &obj))
			require.EqualValues(t, accepted, obj["max_tokens"])
			require.NotContains(t, obj, "max_completion_tokens")
			require.NotEqual(t, map[string]any{"type": "disabled"}, obj["thinking"])
		}
		require.Contains(t, batch.profileEvidence.ProviderRequestNormalizations, fmt.Sprintf("max_tokens=%d->%d:provider_deviation_0", accepted, accepted))
	}
	for _, fields := range []string{`"max_tokens":0`, `"max_completion_tokens":2048`, `"max_tokens":2048,"max_completion_tokens":2048`} {
		_, err := validateChatExecutionProfileRequest([]byte(`{"messages":[],"stream":true,`+fields+`}`), plan)
		require.Error(t, err)
	}
	tooLarge := 2049
	_, err := (modelExecutionRequestLimits{ExplicitOutputTokens: &tooLarge}).resolveOutputReservation(plan, nil)
	require.ErrorIs(t, err, errModelExecutionOutputLimitExceeded)
	require.Zero(t, coordinator.authorizeCount.Load())
}

// The identical synthetic decoded output reproduces v5's byte cut and v6's
// completion. It does not claim measured DeepSeek tokenization or paid usage.
func TestDeepSeekProviderCompletionRelayCounterexampleAndAbnormalPaths(t *testing.T) {
	for _, scenario := range []string{"old_bytes", "success", "overrun", "missing_usage", "missing_done", "invalid_finish", "invalid_arguments", "fresh_terminal", "usage_only", "invalid_usage", "duplicate_usage"} {
		t.Run(scenario, func(t *testing.T) {
			d := deepSeekCompletionDefinition(t)
			wire := deepSeekCompletionFixture(t)
			if scenario == "old_bytes" {
				d = deepSeekBundleDefinition(t, "deepseek-v41-flash.json")
			}
			p := d.Profile()
			switch scenario {
			case "overrun":
				wire = strings.ReplaceAll(strings.ReplaceAll(wire, `"completion_tokens":20`, `"completion_tokens":2049`), `"total_tokens":120`, `"total_tokens":2149`)
			case "missing_usage":
				wire = wire[:strings.LastIndex(wire, "data: {")]
			case "missing_done":
				wire = strings.ReplaceAll(wire, "data: [DONE]\n\n", "")
			case "invalid_finish":
				wire = strings.Replace(wire, `"finish_reason":"tool_calls"`, `"finish_reason":"invalid"`, 1)
			case "invalid_arguments":
				wire = strings.Replace(wire, `OK\"}`, `OK\"`, 1)
			case "fresh_terminal":
				wire = strings.Replace(wire, `"delta":{},"finish_reason":"tool_calls"`, `"delta":{"reasoning_content":"late"},"finish_reason":"tool_calls"`, 1)
			case "usage_only":
				wire = strings.Replace(wire, `"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]`, `"choices":[]`, 1)
			case "invalid_usage":
				wire = strings.Replace(wire, `"total_tokens":120`, `"total_tokens":119`, 1)
			case "duplicate_usage":
				wire = strings.Replace(wire, `"completion_tokens":20`, `"completion_tokens":20,"completion_tokens":20`, 1)
			}
			settles, holds := 0, 0
			var settled billing.SettleRequest
			b := &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
				settles++
				settled = req
				return &billing.SettleResult{Success: true}, nil
			}, holdStreamOutputFn: func(_ context.Context, req billing.SettleRequest, _ int64, _ int) error {
				holds++
				require.Equal(t, 2049, req.CompletionTokens)
				require.Equal(t, metering.UsagePurposeProviderEvidence, req.UsagePurpose)
				return nil
			}}
			r := newTestRelay(p6f11RelayDB(), b)
			require.NoError(t, r.SetStreamCounterVersion(metering.GatewayCounterEstimateVersionV3))
			ctx := contextWithStreamReservation(p6f11StreamContext(nil, "deepseek-final"), 2048, false)
			route := &RouteResult{ProfileDefinition: d, ModelOperationID: modelprofile.OperationOpenAIChat, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, OperationProtocolProfileID: p.ProtocolProfile, OperationProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1, ReasoningProfile: p.ReasoningProfile, MappedModel: p.UpstreamModelIDs[0], Channel: models.Channel{ID: 1, Provider: p.Provider}, CacheUsageProfile: p.CacheUsageProfile, ModelProtocolProfile: p.ProtocolProfile, PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{CapabilityID: modelprofile.CapabilityToolChoiceAuto, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}}}}}
			rec := httptest.NewRecorder()
			r.relayStreamRequest(ctx, rec, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(wire))}, route, "deepseek-final", 1, 2, "public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
			result := rec.Body.String()
			if scenario == "success" {
				require.Contains(t, result, "data: [DONE]")
				require.NotContains(t, result, "stream_error.")
				require.Equal(t, 1, settles)
				require.Equal(t, 20, settled.CompletionTokens)
				require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)
				require.Zero(t, holds)
			} else {
				require.NotContains(t, result, "data: [DONE]")
				if scenario == "overrun" {
					require.Contains(t, result, "provider_output_limit_exceeded")
					require.Equal(t, 1, holds)
					require.Zero(t, settles)
				} else {
					require.Equal(t, 1, settles)
					require.Zero(t, holds)
					if scenario == "old_bytes" {
						require.Contains(t, result, "stream_error.output_limit_exceeded")
					}
					if scenario == "missing_usage" {
						require.Equal(t, metering.UsageSourceCounterV3, settled.UsageSource)
						require.Greater(t, settled.CompletionTokens, 2048, "existing interrupted estimate is not capped or replaced by a new hold")
						t.Logf("frozen=2048 interrupted_estimate=%d", settled.CompletionTokens)
					}
				}
			}
		})
	}
}

func TestDeepSeekProviderCompletionNonStreamReusesKnownUsageHold(t *testing.T) {
	for _, scenario := range []string{"success", "overrun"} {
		t.Run(scenario, func(t *testing.T) {
			d := deepSeekCompletionDefinition(t)
			p := d.Profile()
			holds, settles := 0, 0
			b := &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
				settles++
				require.Equal(t, 20, req.CompletionTokens)
				return &billing.SettleResult{Success: true}, nil
			}, holdOutputFn: func(_ context.Context, req billing.SettleRequest, reason string) error {
				holds++
				require.Equal(t, 2049, req.CompletionTokens)
				require.Equal(t, metering.UsagePurposeProviderEvidence, req.UsagePurpose)
				require.Equal(t, billing.OutputHoldReasonProviderResponseUnusable, reason)
				return nil
			}}
			r := newTestRelay(p6f11RelayDB(), b)
			wire := `{"id":"fixture","object":"chat.completion","created":1,"model":"deepseek-flash","choices":[{"index":0,"message":{"role":"assistant","content":"private-output-marker","reasoning_content":"brief"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":10,"prompt_cache_miss_tokens":90}}`
			if scenario == "overrun" {
				wire = strings.ReplaceAll(strings.ReplaceAll(wire, `"completion_tokens":20`, `"completion_tokens":2049`), `"total_tokens":120`, `"total_tokens":2149`)
			}
			route := &RouteResult{ProfileDefinition: d, ModelOperationID: modelprofile.OperationOpenAIChat, ExecutionProfileID: p.ID, ExecutionProfileVersion: p.Version, OperationProtocolProfileID: p.ProtocolProfile, OperationProtocolProfileVersion: modelprofile.ChatOperationProtocolBindingVersionV1, ReasoningProfile: p.ReasoningProfile, MappedModel: p.UpstreamModelIDs[0], Channel: models.Channel{ID: 1, Provider: p.Provider}, CacheUsageProfile: p.CacheUsageProfile, ModelProtocolProfile: p.ProtocolProfile}
			rec := httptest.NewRecorder()
			r.relayNonStreamRequest(contextWithAttemptID(t.Context(), "att_deepseek_nonstream"), rec, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(wire))}, route, "op_deepseek_nonstream", 1, 2, "public-model", "public-model", "127.0.0.1", logger.New(logger.Fields{}), time.Now(), adapters.NewOpenAICompatible(), 100, 2048, "")
			if scenario == "overrun" {
				require.Equal(t, 502, rec.Code, rec.Body.String())
				require.NotContains(t, rec.Body.String(), "private-output-marker")
				require.Equal(t, 1, holds)
				require.Zero(t, settles)
			} else {
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), "private-output-marker")
				require.Equal(t, 1, settles)
				require.Zero(t, holds)
			}
		})
	}
}
