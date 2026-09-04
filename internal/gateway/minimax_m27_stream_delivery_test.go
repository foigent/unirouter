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

func TestMiniMaxM27StreamActualRelayPreservesTerminalAndUsage(t *testing.T) {
	fixture, err := os.ReadFile("../adapters/testdata/minimax_m27_stream_tools.sse")
	require.NoError(t, err)
	for _, test := range []struct {
		name, stream string
		failure      bool
	}{
		{"clean EOF", string(fixture), false},
		{"DONE ignores trailing data", string(fixture) + "data: [DONE]\n\ndata: secret-trailing-bytes\n\n", false},
		{"CR framing", strings.ReplaceAll(string(fixture), "\n", "\r"), false},
		{"valid usage with invalid sibling", strings.Replace(string(fixture), `"usage":{"prompt_tokens"`, `"unknown_sibling":true,"usage":{"prompt_tokens"`, 1), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var settled billing.SettleRequest
			var providerEvidence map[string]interface{}
			db := p6f11RelayDB()
			queryRow := db.QueryRowFn
			db.QueryRowFn = func(ctx context.Context, sql string, args ...any) pgx.Row {
				if !strings.Contains(sql, "INSERT INTO internal_outbox_events") {
					return queryRow(ctx, sql, args...)
				}
				var payload traceOutboxPayload
				require.NoError(t, json.Unmarshal([]byte(args[5].(string)), &payload))
				if payload.EventType == "provider.usage.observed" {
					require.NoError(t, json.Unmarshal(payload.Metadata, &providerEvidence))
				}
				return &testutil.MockRow{ScanFn: func(dest ...any) error { *dest[0].(*int64) = 1; return nil }}
			}
			relay := newTestRelay(db, &mockBilling{settleFn: func(_ context.Context, request billing.SettleRequest) (*billing.SettleResult, error) {
				settled = request
				return &billing.SettleResult{SettleID: 1, Success: true}, nil
			}})
			queue, err := outbox.New(db, outbox.DefaultOptions())
			require.NoError(t, err)
			relay.SetOutbox(queue)
			relay.SetTraceEvents(true, 1)
			route := &RouteResult{
				MappedModel:                "MiniMax-M2.7",
				Channel:                    models.Channel{ID: 1, Name: "fixture", Provider: "minimax"},
				CacheUsageProfile:          "cache-usage.minimax-m27-openai.v3",
				ModelProtocolProfile:       "protocol.minimax-m27-openai-chat.v1",
				PlanCapabilityRequirements: []modelrouting.CapabilityRequirement{{CapabilityID: modelprofile.CapabilityToolChoiceAuto, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}}}},
			}
			recorder := httptest.NewRecorder()
			relay.relayStreamRequest(p6f11StreamContext(nil, "req-m27-stream"), recorder,
				&http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.stream))}, route,
				"req-m27-stream", 1, 2, "public-model", "public-model", "127.0.0.1", nil,
				logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
			body := recorder.Body.String()
			require.NotContains(t, body, "secret-trailing-bytes")
			require.NotContains(t, body, "unknown_sibling")
			if test.failure {
				require.Contains(t, body, "stream_error.invalid_frame")
				require.NotContains(t, body, "data: [DONE]")
			} else {
				require.NotContains(t, body, "stream_error")
				require.Equal(t, 1, strings.Count(body, "data: [DONE]"))
				require.Contains(t, body, "unirouter_probe_echo")
			}
			require.EqualValues(t, 5585, providerEvidence["prompt_tokens"])
			if test.failure {
				require.Equal(t, metering.UsageSourceCounterV2, settled.UsageSource, "failed delivery must not convert all supplier usage into customer debt")
			} else {
				require.Equal(t, 5585, settled.PromptTokens)
				require.Equal(t, metering.UsageSourceUpstream, settled.UsageSource)
				require.Equal(t, adapters.CacheUsageStatusNotReported, settled.CacheUsageStatus)
				require.Zero(t, settled.CacheReadTokens)
			}
		})
	}
}

func TestMiniMaxM27StreamRejectsToolsOutsideFrozenRequest(t *testing.T) {
	fixture, err := os.ReadFile("../adapters/testdata/minimax_m27_stream_tools.sse")
	require.NoError(t, err)
	auto := func(name string) modelrouting.CapabilityRequirement {
		return modelrouting.CapabilityRequirement{CapabilityID: modelprofile.CapabilityToolChoiceAuto, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{name}}}
	}
	for name, requirements := range map[string][]modelrouting.CapabilityRequirement{
		"no tools":                      nil,
		"undeclared tool":               {auto("read")},
		"conflicting constraints":       {auto("unirouter_probe_echo"), auto("read")},
		"inapplicable named constraint": {{CapabilityID: modelprofile.CapabilityToolChoiceNamed, FunctionToolChoice: &modelrouting.FunctionToolChoiceConstraint{DeclaredFunctionNames: []string{"unirouter_probe_echo"}, SelectedFunctionName: "unirouter_probe_echo"}}},
	} {
		t.Run(name, func(t *testing.T) {
			relay := newTestRelay(p6f11RelayDB(), &mockBilling{})
			route := &RouteResult{MappedModel: "MiniMax-M2.7", Channel: models.Channel{ID: 1, Provider: "minimax"}, CacheUsageProfile: "cache-usage.minimax-m27-openai.v3", ModelProtocolProfile: "protocol.minimax-m27-openai-chat.v1", PlanCapabilityRequirements: requirements}
			recorder := httptest.NewRecorder()
			relay.relayStreamRequest(p6f11StreamContext(nil, "req-m27-tools-denied"), recorder, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(fixture)))}, route, "req-m27-tools-denied", 1, 2, "public-model", "public-model", "127.0.0.1", nil, logger.New(logger.Fields{}), time.Now(), 1000, adapters.NewOpenAICompatible(), "")
			require.Contains(t, recorder.Body.String(), "stream_error.invalid_frame")
			require.NotContains(t, recorder.Body.String(), "unirouter_probe_echo")
			require.NotContains(t, recorder.Body.String(), "call_echo")
			require.NotContains(t, recorder.Body.String(), "data: [DONE]")
		})
	}
}
