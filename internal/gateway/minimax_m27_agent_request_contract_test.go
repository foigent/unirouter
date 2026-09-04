package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

// Retains the structural shape captured from OpenClaw on 2026-09-04. Prompt
// content and local paths are replaced with public test data; this is not a
// replay of an uncaptured Provider response.
func miniMaxM27AgentRequest(t *testing.T) map[string]interface{} {
	t.Helper()
	var request map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(`{"model":"minimax-m2.7","stream":true,"tool_choice":"auto","max_completion_tokens":2048,"messages":[{"role":"system","content":"Read the public release gate files."},{"role":"user","content":"Does an architecture documentation change require version metadata?"}],"tools":[{"type":"function","function":{"name":"read","description":"Read a public text file.","parameters":{"type":"object","required":["path"],"properties":{"path":{"type":"string"},"offset":{"type":"integer","minimum":1},"limit":{"type":"number"},"cursor":{"type":"integer","minimum":0},"optional":{"type":"boolean","const":true}}}}}]}`), &request))
	return request
}

func TestMiniMaxM27StreamClientRequestsReachExactWire(t *testing.T) {
	for _, history := range []bool{false, true} {
		for _, field := range []string{"max_tokens", "max_completion_tokens"} {
			request := miniMaxM27AgentRequest(t)
			delete(request, "max_completion_tokens")
			request[field] = 8192
			request["stream_options"] = map[string]bool{"include_usage": true}
			if history {
				request["messages"] = []interface{}{
					map[string]interface{}{"role": "user", "content": "Read VERSION."},
					map[string]interface{}{"role": "assistant", "content": nil, "tool_calls": []interface{}{map[string]interface{}{"id": "callreadversion1", "type": "function", "function": map[string]interface{}{"name": "read", "arguments": `{"path":"VERSION"}`}}}},
					map[string]interface{}{"role": "tool", "tool_call_id": "callreadversion1", "content": "1.2.48"},
				}
			}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			profilePlan := executionProfilePlan(t, "minimax.m2.7.payg", "1.3.0")
			evidence, err := validateChatExecutionProfileRequest(body, profilePlan)
			require.NoError(t, err)
			evidence.bindAdmissionBody(body)
			_, err = estimateChatAdmissionForValidatedPlan(body, profilePlan, evidence)
			require.NoError(t, err)
			key := []byte("0123456789abcdef0123456789abcdef")
			plan := executionTestPlan(t, modelrouting.ModeCanonical, modeloperation.IDChatCompletions, "https://api.minimax.cn/v1", key, nil)
			setExecutionTestProfile(t, &plan, "minimax.m2.7.payg", "1.3.0")
			plan.Routes[0].Provider = "minimax"
			plan.Routes[0].UpstreamModel = "MiniMax-M2.7"
			plan.PublicOutputLimit = 8192
			plan.Routes[0].DefaultMaxOutput = 8192
			relay := NewRelay(executionTestDB(), nil, nil, &mockBilling{}, nil, key)
			require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeCanonical, &executionPlannerStub{plan: plan}, &executionCoordinatorStub{}, routingpolicy.Default()))
			cap := 8192
			batch, err := relay.prepareModelExecutionRoutes(context.Background(), modeloperation.IDChatCompletions, adapters.EndpointChatCompletions, "public-model", "public-model", "req-m27-agent", modelExecutionRequestLimits{EstimatedInputTokens: 1, ExplicitOutputTokens: &cap, EstimateVersion: chatAdmissionEstimateVersion}, body, time.Now())
			require.NoError(t, err)
			wire, err := providerRequestBody(batch.routes[0].upstreamRequest)
			require.NoError(t, err)
			var upstream map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(wire, &upstream))
			var input map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &input))
			for _, name := range []string{"messages", "tools", "tool_choice", "stream", "stream_options", field} {
				require.JSONEq(t, string(input[name]), string(upstream[name]), name)
			}
			require.JSONEq(t, `"MiniMax-M2.7"`, string(upstream["model"]))
		}
	}
}

func TestMiniMaxM27StreamRuntimeUsesSharedProbeFixture(t *testing.T) {
	fixture, err := os.ReadFile("../adapters/testdata/minimax_m27_stream_tools.sse")
	require.NoError(t, err)
	state := exactStreamStateForProtocol("protocol.minimax-m27-openai-chat.v1", "MiniMax-M2.7")
	assembler, _ := adapters.NewChatSSEEventAssembler(1 << 20)
	scanner := bufio.NewScanner(strings.NewReader(string(fixture)))
	scanner.Split(adapters.SplitSSELines)
	toolArgumentRefs := 0
	for scanner.Scan() {
		event, ready, err := assembler.AcceptLine(scanner.Text())
		require.NoError(t, err)
		if !ready {
			continue
		}
		body, err := adapters.NewOpenAICompatible().NormalizeStreamFrame(context.Background(), adapters.StreamChunkInput{Data: event.Data, Channel: models.Channel{Provider: "minimax"}, CacheUsageProfile: "cache-usage.minimax-m27-openai.v3"})
		require.NoError(t, err)
		usage, present, err := adapters.ParseOpenAICompatibleStreamUsageWithCacheProfile(body, adapters.EndpointChatCompletions, metering.UsageSourceUpstream, "cache-usage.minimax-m27-openai.v3")
		require.NoError(t, err)
		frame, err := parseGuardedStreamFrameForProtocol(body, "protocol.minimax-m27-openai-chat.v1")
		require.NoError(t, err)
		require.False(t, frame.unsupported)
		for _, ref := range frame.refs {
			if strings.HasSuffix(ref.path, ".arguments") {
				toolArgumentRefs++
			}
		}
		require.NoError(t, state.accept(frame, present))
		if present {
			require.Equal(t, adapters.CacheUsageStatusNotReported, usage.CacheUsageStatus)
		}
	}
	require.NoError(t, scanner.Err())
	require.NoError(t, state.validateCleanEOF(true))
	require.NoError(t, state.validateDone())
	require.Equal(t, 2, toolArgumentRefs)
}

func TestMiniMaxM27HistoricalProfileRejectsActualAgentComposition(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m2.7.payg", "1.2.0")
	for _, test := range []struct {
		name, limitField, wantError string
		stream, explicitAuto        bool
	}{
		{"captured first turn", "max_completion_tokens", "require stream=false", true, true},
		{"stream independently", "max_completion_tokens", "require stream=false", true, false},
		{"auto independently", "max_completion_tokens", "tool_choice is not supported", false, true},
		{"provider default new limit spelling", "max_completion_tokens", "", false, false},
		{"provider default legacy limit spelling", "max_tokens", "", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := miniMaxM27AgentRequest(t)
			request["stream"] = test.stream
			if !test.explicitAuto {
				delete(request, "tool_choice")
			}
			delete(request, "max_completion_tokens")
			request[test.limitField] = 2048
			body, err := json.Marshal(request)
			require.NoError(t, err)
			evidence, err := validateChatExecutionProfileRequest(body, plan)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			evidence.bindAdmissionBody(body)
			_, err = estimateChatAdmissionForValidatedPlan(body, plan, evidence)
			require.NoError(t, err)
		})
	}
}

func TestMiniMaxM27HistoricalToolContinuationRemainsExact(t *testing.T) {
	plan := executionProfilePlan(t, "minimax.m2.7.payg", "1.2.0")
	request := miniMaxM27AgentRequest(t)
	request["stream"] = false
	delete(request, "tool_choice")
	request["messages"] = []interface{}{
		map[string]interface{}{"role": "user", "content": "Read VERSION."},
		map[string]interface{}{"role": "assistant", "content": "<think>Read the version.</think>", "tool_calls": []interface{}{
			map[string]interface{}{"id": "call_read_version", "type": "function", "function": map[string]interface{}{"name": "read", "arguments": `{"path":"VERSION"}`}},
		}},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_read_version", "content": "1.2.48"},
	}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	_, err = validateChatExecutionProfileRequest(body, plan)
	require.NoError(t, err)
	_, err = estimateChatAdmissionForPlan(body, plan)
	require.NoError(t, err)
	// The captured successful direct continuation omits replayed thinking:
	// content is null, and the client consistently normalizes tool IDs in both
	// assistant history and tool results. The historical reasoning contract
	// rejects this even though the protocol-only history validator accepts it.
	request["messages"].([]interface{})[1].(map[string]interface{})["content"] = nil
	body, err = json.Marshal(request)
	require.NoError(t, err)
	_, err = validateChatExecutionProfileRequest(body, plan)
	require.ErrorContains(t, err, "must preserve content")
	request["messages"].([]interface{})[1].(map[string]interface{})["content"] = "<think>Read the version.</think>"
	request["messages"].([]interface{})[2].(map[string]interface{})["tool_call_id"] = "unknown_call"
	body, err = json.Marshal(request)
	require.NoError(t, err)
	_, err = validateChatExecutionProfileRequest(body, plan)
	require.ErrorContains(t, err, "does not reference prior assistant tool_calls")
}
