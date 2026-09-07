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
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/testutil"
)

// These are constructed wire boundary fixtures, not paid Provider evidence.
func existingFamilyRelayFixture(t *testing.T, p modelprofile.Profile, scenario string) string {
	t.Helper()
	if scenario == "captured M3 tier" {
		wire, err := os.ReadFile("../adapters/testdata/minimax_m3_stream_tier_20260906.sse")
		require.NoError(t, err)
		return string(wire)
	}
	usage := map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}
	switch p.Provider {
	case "deepseek":
		usage["prompt_cache_hit_tokens"], usage["prompt_cache_miss_tokens"] = 10, 90
	case "kimi":
		usage["cached_tokens"] = 10
	default:
		usage["prompt_tokens_details"] = map[string]any{"cached_tokens": 10}
	}
	frame := func(choices []any, counters map[string]any, invalid bool) string {
		envelope := map[string]any{"id": "fixture", "object": "chat.completion.chunk", "created": 1, "model": p.UpstreamModelIDs[0], "choices": choices}
		if counters != nil {
			envelope["usage"] = counters
		}
		if invalid {
			envelope["unmodeled"] = true
		}
		if p.ProtocolProfile == "protocol.minimax-m3-openai-chat.v11" {
			envelope["service_tier"] = "standard"
			if scenario == "invalid tier with usage" && counters != nil {
				envelope["service_tier"] = 1
			}
		}
		if p.Provider == "dashscope" {
			envelope["system_fingerprint"], envelope["service_tier"] = nil, nil
			if scenario == "invalid tier with usage" && counters != nil {
				envelope["service_tier"] = "unknown"
			}
			for _, raw := range choices {
				choice := raw.(map[string]any)
				choice["logprobs"] = nil
				delta := choice["delta"].(map[string]any)
				delta["function_call"], delta["refusal"] = nil, nil
				if _, present := delta["role"]; !present {
					delta["role"] = nil
				}
			}
		} else if p.Provider != "minimax" {
			envelope["system_fingerprint"] = "fixture-fingerprint"
		}
		wire, err := json.Marshal(envelope)
		require.NoError(t, err)
		return "data: " + string(wire) + "\n\n"
	}
	delta := map[string]any{"role": "assistant"}
	if p.Provider == "minimax" {
		delta["content"] = "<think>Check the public echo.</think>"
	} else {
		delta["reasoning_content"] = "Check the public echo."
	}
	answer := scenario == "answer"
	if answer {
		delta["content"] = "Finished."
	} else {
		delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "call_echo", "type": "function", "function": map[string]any{"name": "unirouter_probe_echo", "arguments": `{"value":`}}}
	}
	wire := frame([]any{map[string]any{"index": 0, "delta": delta}}, nil, false)
	terminalDelta := map[string]any{"content": ""}
	finish := "stop"
	if !answer {
		finish = "tool_calls"
		terminalDelta["tool_calls"] = []any{map[string]any{"index": 0, "function": map[string]any{"arguments": `"UNIROUTER_TOOL_PROBE_OK"}`}}}
		if p.Provider == "dashscope" {
			terminalDelta["tool_calls"].([]any)[0].(map[string]any)["id"] = ""
		}
	}
	terminal := map[string]any{"index": 0, "delta": terminalDelta, "finish_reason": finish}
	terminalUsage := usage
	if p.Provider == "kimi" && scenario != "invalid sibling with usage" {
		terminal["usage"], terminalUsage = usage, nil
	}
	wire += frame([]any{terminal}, terminalUsage, scenario == "invalid sibling with usage")
	if p.Provider == "kimi" && (scenario == "tools" || scenario == "conflicting repeat") {
		if scenario == "conflicting repeat" {
			usage["cached_tokens"] = 9
		}
		wire += frame([]any{}, usage, false)
	}
	if scenario != "EOF" {
		wire += "data: [DONE]\n\n"
	}
	return wire
}

func TestExistingFamilyActualRelayKeepsToolUsageAndSettlementBoundaries(t *testing.T) {
	for _, tc := range []struct{ id, version, protocol string }{
		{"deepseek.v4-flash.api", "1.5.0", "protocol.deepseek-openai-chat.v5"},
		{"deepseek.v4-pro.api", "1.5.0", "protocol.deepseek-openai-chat.v5"},
		{"kimi.k2.6.api", "1.7.0", "protocol.kimi-openai-chat.v6"},
		{"kimi.k2.7-code.api", "1.3.0", "protocol.kimi-openai-chat.v6"},
		{"kimi.k3.api", "1.9.0", "protocol.kimi-openai-chat.k3.v6"},
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v10"},
		{"minimax.m3.payg", "1.2.0", "protocol.minimax-m3-openai-chat.v11"},
		{"minimax.m2.7-highspeed.payg", "1.2.0", "protocol.minimax-m2-openai-chat.v8"},
		{"qwen.3.7-flash.api", "1.7.0", "protocol.qwen-openai-chat.flash.v7"},
	} {
		p, ok := modelprofile.Lookup(tc.id, tc.version)
		require.True(t, ok)
		p.ProtocolProfile = tc.protocol
		scenarios := []string{"tools", "answer", "EOF", "invalid sibling with usage", "conflicting repeat"}
		if tc.protocol == "protocol.minimax-m3-openai-chat.v11" {
			scenarios = append(scenarios, "captured M3 tier", "invalid tier with usage")
		}
		if p.Provider == "dashscope" {
			scenarios = append(scenarios, "invalid tier with usage")
		}
		for _, scenario := range scenarios {
			if scenario == "conflicting repeat" && p.Provider != "kimi" {
				continue
			}
			t.Run(tc.id+"/"+tc.protocol+"/"+scenario, func(t *testing.T) {
				expectedPrompt, expectedCached, frameID := 100, 10, "fixture"
				if scenario == "captured M3 tier" {
					expectedPrompt, expectedCached, frameID = 441, 128, "06ec4dd7a937876decdcc6825c58ded2"
				}
				var settled billing.SettleRequest
				var supplier map[string]any
				settlements, observations := 0, 0
				db := p6f11RelayDB()
				original := db.QueryRowFn
				db.QueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
					if !strings.Contains(sql, "INSERT INTO internal_outbox_events") {
						return original(ctx, sql, args...)
					}
					var payload traceOutboxPayload
					require.NoError(t, json.Unmarshal([]byte(args[5].(string)), &payload))
					if payload.EventType == "provider.usage.observed" {
						observations++
						require.NoError(t, json.Unmarshal(payload.Metadata, &supplier))
					}
					return &testutil.MockRow{ScanFn: func(dest ...any) error { *dest[0].(*int64) = 1; return nil }}
				}
				relay := newTestRelay(db, &mockBilling{settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
					settlements++
					settled = req
					return &billing.SettleResult{SettleID: 1, Success: true}, nil
				}})
				queue, err := outbox.New(db, outbox.DefaultOptions())
				require.NoError(t, err)
				relay.SetOutbox(queue)
				relay.SetTraceEvents(true, 1)
				route := &RouteResult{MappedModel: p.UpstreamModelIDs[0], Channel: models.Channel{ID: 1, Provider: p.Provider}, CacheUsageProfile: p.CacheUsageProfile, ModelProtocolProfile: tc.protocol, PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{CapabilityID: modelprofile.CapabilityToolChoiceAuto, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}}}}}
				recorder := httptest.NewRecorder()
				relay.relayStreamRequest(p6f11StreamContext(nil, "family-stream"), recorder, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(existingFamilyRelayFixture(t, p, scenario)))}, route, "family-stream", 1, 2, "public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
				result := recorder.Body.String()
				failure := scenario == "invalid tier with usage" || scenario == "invalid sibling with usage" || scenario == "conflicting repeat" || (scenario == "EOF" && p.Provider != "minimax")
				if failure {
					require.Contains(t, result, "stream_error")
					require.NotContains(t, result, "data: [DONE]")
					require.Equal(t, metering.UsageSourceCounterV2, settled.UsageSource)
				} else {
					require.NotContains(t, result, "stream_error")
					require.Equal(t, 1, strings.Count(result, "data: [DONE]"))
					require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)
					require.Equal(t, expectedPrompt, settled.PromptTokens)
					usageFrames := 0
					for _, line := range strings.Split(result, "\n") {
						var frame map[string]any
						if strings.HasPrefix(line, "data: ") && json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame) == nil && frame["usage"] != nil && frame["id"] == frameID {
							usageFrames++
						}
					}
					require.Equal(t, 1, usageFrames, "Kimi's consistent repeated usage is not delivered twice")
				}
				require.NotContains(t, result, "unmodeled")
				if tc.protocol == "protocol.minimax-m3-openai-chat.v11" {
					require.NotContains(t, result, "service_tier")
				}
				require.Equal(t, 1, settlements)
				require.Equal(t, 1, observations)
				require.EqualValues(t, expectedPrompt, supplier["prompt_tokens"], "safe usage remains evidence even when delivery fails")
				require.EqualValues(t, expectedCached, supplier["cache_read_tokens"], "a conflicting repeat cannot replace the first complete counter vector")
			})
		}
	}
}
