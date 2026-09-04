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

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

func glmStreamFixtureBindings(t *testing.T) []qualification.Binding {
	t.Helper()
	bindings, err := publication.CurrentBindings()
	require.NoError(t, err)
	var result []qualification.Binding
	for _, b := range bindings {
		if b.ProbeContractID != qualification.GLMStreamFunctionToolsProbeContractID || b.CapabilityID != modelprofile.CapabilityFunctionTools {
			continue
		}
		exact, err := qualification.Resolve(b.OperationID, b.CapabilityID, b.ExecutionProfileID, b.ExecutionProfileVersion, b.ProtocolProfileID, b.ProtocolProfileVersion)
		require.NoError(t, err)
		result = append(result, exact)
	}
	require.Len(t, result, 6)
	return result
}

func TestGLMStreamProbeAndAgentRequestsReachExactProviderWire(t *testing.T) {
	for _, b := range glmStreamFixtureBindings(t) {
		for _, scenario := range []string{"probe", "first-default", "first-8192", "history-65536"} {
			t.Run(b.ExpectedUpstreamModel+"/"+scenario, func(t *testing.T) {
				body, err := qualification.BuildOpenAIChatFunctionToolsProbeRequest(b)
				require.NoError(t, err)
				var request map[string]interface{}
				require.NoError(t, json.Unmarshal(body, &request))
				if scenario != "probe" {
					// Preserve the observed client shape, with only public fixture
					// content. No private task bytes or paid requests in this test.
					delete(request, "thinking")
					if scenario == "first-default" {
						delete(request, "max_tokens")
					} else {
						request["max_tokens"] = 8192
					}
					if scenario == "history-65536" {
						request["max_tokens"] = 65536
						request["messages"] = []interface{}{
							map[string]interface{}{"role": "user", "content": "Echo the public result."},
							map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{map[string]interface{}{"id": "call_echo", "type": "function", "function": map[string]interface{}{"name": "unirouter_probe_echo", "arguments": `{"value":"UNIROUTER_TOOL_PROBE_OK"}`}}}},
							map[string]interface{}{"role": "tool", "tool_call_id": "call_echo", "content": "UNIROUTER_TOOL_PROBE_OK"},
						}
					}
				}
				body, err = json.Marshal(request)
				require.NoError(t, err)
				profilePlan := executionProfilePlan(t, b.ExecutionProfileID, b.ExecutionProfileVersion)
				evidence, err := validateChatExecutionProfileRequest(body, profilePlan)
				require.NoError(t, err)
				evidence.bindAdmissionBody(body)
				_, err = estimateChatAdmissionForValidatedPlan(body, profilePlan, evidence)
				require.NoError(t, err)
				key := []byte("0123456789abcdef0123456789abcdef")
				plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://open.bigmodel.cn/api/paas/v4", key, nil)
				setExecutionTestProfile(t, &plan, b.ExecutionProfileID, b.ExecutionProfileVersion)
				plan.Routes[0].Provider, plan.Routes[0].UpstreamModel = "glm", b.ExpectedUpstreamModel
				plan.PublicOutputLimit = 131072
				plan.Routes[0].DefaultMaxOutput = 65536
				relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
				require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
				var output *int
				if value, exists := request["max_tokens"]; exists {
					n := 1024
					if x, ok := value.(int); ok {
						n = x
					}
					output = &n
				}
				batch, err := relay.prepareModelExecutionRoutes(context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions, "public-model", "public-model", "glm-stream-wire", modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: output, EstimateVersion: chatAdmissionEstimateVersion}, body, time.Now())
				require.NoError(t, err)
				wire, err := providerRequestBody(batch.routes[0].upstreamRequest)
				require.NoError(t, err)
				var sent, input map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(wire, &sent))
				require.NoError(t, json.Unmarshal(body, &input))
				for _, name := range []string{"messages", "tools", "tool_choice", "stream", "stream_options"} {
					require.JSONEq(t, string(input[name]), string(sent[name]), name)
				}
				if output == nil {
					require.JSONEq(t, "65536", string(sent["max_tokens"]))
				} else {
					require.JSONEq(t, string(input["max_tokens"]), string(sent["max_tokens"]))
				}
				require.JSONEq(t, `"`+b.ExpectedUpstreamModel+`"`, string(sent["model"]))
				if scenario != "probe" {
					require.NotContains(t, sent, "thinking", "do not invent client controls")
				}
			})
		}
	}
}

func TestGLMStreamActualRelaySharesProbeFactsAndKeepsUsageAuthority(t *testing.T) {
	fixture, err := os.ReadFile("../adapters/testdata/glm_stream_tools.sse")
	require.NoError(t, err)
	for _, b := range glmStreamFixtureBindings(t) {
		base := strings.ReplaceAll(string(fixture), "glm-5.3", b.ExpectedUpstreamModel)
		// A continuation may answer without another tool invocation. This is
		// valid Runtime delivery, but intentionally does not prove tools to Probe.
		var answerLines []string
		for _, line := range strings.Split(base, "\n") {
			if !strings.Contains(line, `"tool_calls":[`) {
				answerLines = append(answerLines, line)
			}
		}
		answer := strings.ReplaceAll(strings.Join(answerLines, "\n"), `"finish_reason":"tool_calls"`, `"finish_reason":"stop"`)
		answer = strings.ReplaceAll(answer, `"content":""`, `"content":"Finished."`)
		for _, tc := range []struct {
			name, wire string
			failure    bool
		}{
			{"tools", base, false},
			{"final answer", answer, false},
			{"length", strings.ReplaceAll(answer, `"finish_reason":"stop"`, `"finish_reason":"length"`), false},
			{"CR framing", strings.ReplaceAll(base, "\n", "\r"), false},
			{"trailing", base + "data: private-trailing-bytes\n\n", false},
			{"invalid sibling with usage", strings.Replace(base, `"usage":`, `"unmodeled":true,"usage":`, 1), true},
			{"missing DONE", strings.ReplaceAll(base, "data: [DONE]", ": no done"), true},
		} {
			t.Run(b.ExpectedUpstreamModel+"/"+tc.name, func(t *testing.T) {
				var settled billing.SettleRequest
				var supplier map[string]interface{}
				db := p6f11RelayDB()
				original := db.QueryRowFn
				db.QueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
					if !strings.Contains(sql, "INSERT INTO internal_outbox_events") {
						return original(ctx, sql, args...)
					}
					var payload traceOutboxPayload
					require.NoError(t, json.Unmarshal([]byte(args[5].(string)), &payload))
					if payload.EventType == "provider.usage.observed" {
						require.NoError(t, json.Unmarshal(payload.Metadata, &supplier))
					}
					return &testutil.MockRow{ScanFn: func(dest ...any) error { *dest[0].(*int64) = 1; return nil }}
				}
				relay := newTestRelay(db, &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					settled = req
					return &billing.SettleResult{SettleID: 1, Success: true}, nil
				}})
				queue, err := outbox.New(db, outbox.DefaultOptions())
				require.NoError(t, err)
				relay.SetOutbox(queue)
				relay.SetTraceEvents(true, 1)
				route := &RouteResult{MappedModel: b.ExpectedUpstreamModel, Channel: models.Channel{ID: 1, Provider: "glm"}, CacheUsageProfile: "cache-usage.glm-openai.v1", ModelProtocolProfile: b.ProtocolProfileID, PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{CapabilityID: modelprofile.CapabilityToolChoiceAuto, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}}}}}
				recorder := httptest.NewRecorder()
				relay.relayStreamRequest(p6f11StreamContext(nil, "glm-stream-fixture"), recorder, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.wire))}, route, "glm-stream-fixture", 1, 2, "public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
				result := recorder.Body.String()
				require.NotContains(t, result, "private-trailing-bytes")
				require.NotContains(t, result, "unmodeled")
				if tc.failure {
					require.Contains(t, result, "stream_error")
					require.NotContains(t, result, "data: [DONE]")
					require.Equal(t, metering.UsageSourceCounterV2, settled.UsageSource)
				} else {
					require.NotContains(t, result, "stream_error")
					require.Equal(t, 1, strings.Count(result, "data: [DONE]"))
					require.Contains(t, result, "reasoning_content")
					require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)
					require.Equal(t, 100, settled.PromptTokens)
					obs, err := qualification.ValidateOpenAIChatStreamFunctionToolsForBinding(b, "text/event-stream", strings.NewReader(tc.wire))
					if tc.name == "final answer" || tc.name == "length" {
						require.Contains(t, result, "Finished.")
						require.Error(t, err, "an answer is not a successful function-tools qualification")
					} else {
						require.Contains(t, result, "unirouter_probe_echo")
						require.NoError(t, err)
						require.True(t, obs.ToolCallObserved)
					}
				}
				require.EqualValues(t, 100, supplier["prompt_tokens"], "safe Provider usage survives failed delivery")
			})
		}
	}
}

func TestGLMStreamRejectsUnfrozenModelAndToolIdentities(t *testing.T) {
	fixture, err := os.ReadFile("../adapters/testdata/glm_stream_tools.sse")
	require.NoError(t, err)
	for _, name := range []string{"missing upstream", "wrong upstream", "undeclared tool"} {
		t.Run(name, func(t *testing.T) {
			route := &RouteResult{MappedModel: "glm-5.3", Channel: models.Channel{ID: 1, Provider: "glm"}, CacheUsageProfile: "cache-usage.glm-openai.v1", ModelProtocolProfile: "protocol.glm-openai-chat.v7"}
			if name == "missing upstream" {
				route.MappedModel = ""
			}
			if name == "wrong upstream" {
				route.MappedModel = "glm-5.2"
			}
			if name != "undeclared tool" {
				route.PlanCapabilityRequirements = []modelrouting.CapabilityRequirement{{CapabilityID: modelprofile.CapabilityToolChoiceAuto, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}}}}
			}
			recorder := httptest.NewRecorder()
			relay := newTestRelay(p6f11RelayDB(), &mockBilling{})
			relay.relayStreamRequest(p6f11StreamContext(nil, "glm-reject"), recorder, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(fixture)))}, route, "glm-reject", 1, 2, "public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
			require.Contains(t, recorder.Body.String(), "stream_error")
			require.NotContains(t, recorder.Body.String(), "call_echo")
			require.NotContains(t, recorder.Body.String(), "data: [DONE]")
		})
	}
}

func TestGLMStreamPreflightConnectsProfileDefaultMaximumAndPolicy(t *testing.T) {
	for _, b := range glmStreamFixtureBindings(t) {
		for _, tc := range []struct {
			name, output                string
			policyOutput, policyContext int
			pass                        bool
		}{
			{"default", "", 0, 0, true},
			{"explicit default", "65536", 0, 0, true},
			{"parameter maximum", "131072", 0, 0, true},
			{"above maximum", "131073", 0, 0, false},
			{"narrowed default", "", 4096, 0, true},
			{"narrowed explicit", "8192", 4096, 0, false},
			{"context includes nonzero input", "131072", 0, 131072, false},
		} {
			t.Run(b.ExpectedUpstreamModel+"/"+tc.name, func(t *testing.T) {
				r, planner, coordinator := newPreflightFixture(t)
				profile, ok := modelprofile.Lookup(b.ExecutionProfileID, b.ExecutionProfileVersion)
				require.True(t, ok)
				setExecutionTestProfile(t, &planner.plan, profile.ID, profile.Version)
				planner.plan.PublicContextLimit = profile.DefaultContextWindow
				planner.plan.PublicOutputLimit = profile.ProviderMaxOutput
				planner.plan.Routes[0].MaxContext = profile.DefaultContextWindow
				planner.plan.Routes[0].UpstreamModel = b.ExpectedUpstreamModel
				if tc.policyOutput != 0 {
					planner.plan.PublicOutputLimit = tc.policyOutput
					planner.plan.RequestDefaultOutputLimit = modelprofile.DefaultRequestOutputLimit(profile, tc.policyOutput)
				}
				if tc.policyContext != 0 {
					planner.plan.PublicContextLimit = tc.policyContext
				}
				body := strings.Replace(preflightToolRequest, `"stream":true`, `"stream":true,"stream_options":{"include_usage":true}`, 1)
				if tc.output == "" {
					body = strings.Replace(body, `"max_tokens":8192,`, "", 1)
				} else {
					body = strings.Replace(body, `"max_tokens":8192`, `"max_tokens":`+tc.output, 1)
				}
				w, result := runChatPreflight(t, r, body)
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Equal(t, tc.pass, result.LocallyAdmissible, w.Body.String())
				require.Zero(t, coordinator.authorizeCount.Load())
			})
		}
	}
}
