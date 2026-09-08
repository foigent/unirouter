package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/models"
)

func checkpointCounterFrame(t *testing.T, delta map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
	require.NoError(t, err)
	return "data: " + string(encoded) + "\n\n"
}

func checkpointCounterMixedFrames(t *testing.T) []string {
	t.Helper()
	return []string{
		checkpointCounterFrame(t, map[string]any{"content": "abcdefgh"}),
		checkpointCounterFrame(t, map[string]any{"reasoning_content": "你好世界"}),
		checkpointCounterFrame(t, map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": "call_counter", "type": "function",
			"function": map[string]any{"name": "read", "arguments": `{"ok":"🧭"}`},
		}}}),
	}
}

func TestP6F11StreamCheckpointUsesDeliveredCustomerCounter(t *testing.T) {
	var checkpoints []billing.StreamDeliveryCheckpoint
	var persisted billing.SettleRequest
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		checkpointFn: func(_ context.Context, checkpoint billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error) {
			checkpoints = append(checkpoints, checkpoint)
			return &billing.StreamCheckpointResult{Sequence: len(checkpoints)}, nil
		},
		persistUsageFn: func(_ context.Context, req billing.SettleRequest) error { persisted = req; return nil },
	})
	frames := checkpointCounterMixedFrames(t)
	body := &countingReadCloser{reader: &readThenError{payload: []byte(strings.Join(frames, ""))}}
	recorder := runStreamTransportTest(t, relay, body, "req-checkpoint-counter")
	require.NotContains(t, recorder.Body.String(), "data: [DONE]")
	require.NotEmpty(t, checkpoints)
	last := checkpoints[len(checkpoints)-1]
	// Existing per-frame counter-v2: 8 Latin characters -> 2; 4 CJK -> 3;
	// the tool frame's ID, name and JSON arguments total 26 runes -> 7.
	// Those disclosed text fields total 49 UTF-8 bytes, not 12 tokens.
	require.Equal(t, 12, last.CompletionTokens)
	require.Equal(t, 3, last.DeliveredChunks)
	require.Greater(t, last.DeliveredBytes, int64(49))
	require.Equal(t, persisted.CompletionTokens, last.CompletionTokens)
	require.Equal(t, persisted.PromptTokens, last.PromptTokens)
	require.Equal(t, metering.GatewayCounterEstimateVersionV2, persisted.UsageEstimateVersion)
}

type checkpointCounterWriter struct {
	*httptest.ResponseRecorder
	beforeWrite func(int)
	writes      int
}

func (w *checkpointCounterWriter) Write(payload []byte) (int, error) {
	w.writes++
	w.beforeWrite(w.writes)
	return w.ResponseRecorder.Write(payload)
}

func TestP6F11StreamCheckpointTokenCadenceUsesEstimate(t *testing.T) {
	var checkpoints []billing.StreamDeliveryCheckpoint
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		checkpointFn: func(_ context.Context, checkpoint billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error) {
			checkpoints = append(checkpoints, checkpoint)
			return &billing.StreamCheckpointResult{Sequence: len(checkpoints)}, nil
		},
	})
	var frames []string
	for _, size := range []int{4, 256, 256, 4} {
		frames = append(frames, checkpointCounterFrame(t, map[string]any{"content": strings.Repeat("x", size)}))
	}
	checkedBelow, checkedAt := false, false
	writer := &checkpointCounterWriter{ResponseRecorder: httptest.NewRecorder(), beforeWrite: func(write int) {
		switch write {
		case 3:
			// First frame forces a checkpoint. The next 256 text bytes are only
			// 64 estimated tokens, below the 128-token non-force threshold.
			require.Len(t, checkpoints, 1)
			require.Equal(t, 1, checkpoints[0].CompletionTokens)
			checkedBelow = true
		case 4:
			// Two subsequent 64-token frames now cross that same threshold.
			require.Len(t, checkpoints, 2)
			require.Equal(t, 129, checkpoints[1].CompletionTokens)
			require.Equal(t, 3, checkpoints[1].DeliveredChunks)
			checkedAt = true
		}
	}}
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Join(frames, "") + "data: [DONE]\n\n"))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	relay.relayStreamRequest(t.Context(), writer, response, route, "req-checkpoint-cadence", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "hello"}}, logger.New(logger.Fields{OrgID: 1}), time.Now(), 1000, nil, "")
	require.True(t, checkedBelow)
	require.True(t, checkedAt)
}

func TestP6F11StreamCheckpointExcludesPartialSecondFrameTokens(t *testing.T) {
	for _, version := range []string{metering.GatewayCounterEstimateVersionV2, metering.GatewayCounterEstimateVersionV3} {
		t.Run(version, func(t *testing.T) {
			var checkpoints []billing.StreamDeliveryCheckpoint
			var persisted billing.SettleRequest
			relay := newTestRelay(streamTransportTestDB(), &mockBilling{
				checkpointFn: func(_ context.Context, checkpoint billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error) {
					checkpoints = append(checkpoints, checkpoint)
					return &billing.StreamCheckpointResult{Sequence: len(checkpoints)}, nil
				},
				persistUsageFn: func(_ context.Context, req billing.SettleRequest) error { persisted = req; return nil },
			})
			require.NoError(t, relay.SetStreamCounterVersion(version))
			frames := checkpointCounterMixedFrames(t)
			writer := &failAfterWritesResponseWriter{header: make(http.Header), partialAt: 2}
			response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Join(frames, "")))}
			route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
			relay.relayStreamRequest(t.Context(), writer, response, route, "req-checkpoint-partial", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "hello"}}, logger.New(logger.Fields{OrgID: 1}), time.Now(), 1000, nil, "")
			require.GreaterOrEqual(t, len(checkpoints), 2)
			first, last := checkpoints[0], checkpoints[len(checkpoints)-1]
			require.Equal(t, 2, last.CompletionTokens)
			require.Equal(t, 1, last.DeliveredChunks)
			require.Equal(t, first.CompletionTokens, last.CompletionTokens)
			require.Greater(t, last.DeliveredBytes, first.DeliveredBytes)
			require.Equal(t, int64(writer.body.Len()), last.DeliveredBytes)
			require.Equal(t, 2, persisted.CompletionTokens)
			require.NotContains(t, writer.body.String(), "data: [DONE]")

			require.Equal(t, version, persisted.UsageEstimateVersion)
		})
	}
}

func TestStreamCounterV3RelayPartitionInvariant(t *testing.T) {
	for _, fragments := range [][]string{{"hello你好🧭"}, {"h", "e", "l", "l", "o", "你", "好", "🧭"}, {"hello", "你好", "🧭"}} {
		t.Run(strings.Join(fragments, "/"), func(t *testing.T) {
			var checkpoints []billing.StreamDeliveryCheckpoint
			var persisted billing.SettleRequest
			var begin billing.StreamDeliveryStart
			relay := newTestRelay(streamTransportTestDB(), &mockBilling{
				beginStreamFn: func(_ context.Context, value billing.StreamDeliveryStart) error { begin = value; return nil },
				checkpointFn: func(_ context.Context, cp billing.StreamDeliveryCheckpoint) (*billing.StreamCheckpointResult, error) {
					checkpoints = append(checkpoints, cp)
					return &billing.StreamCheckpointResult{Sequence: len(checkpoints)}, nil
				},
				persistUsageFn: func(_ context.Context, req billing.SettleRequest) error { persisted = req; return nil },
			})
			require.NoError(t, relay.SetStreamCounterVersion(metering.GatewayCounterEstimateVersionV3))
			var frames []string
			for _, text := range fragments {
				frames = append(frames, checkpointCounterFrame(t, map[string]any{"content": text}))
			}
			body := &countingReadCloser{reader: &readThenError{payload: []byte(strings.Join(frames, ""))}}
			recorder := runStreamTransportTest(t, relay, body, "req-counter-v3-partitions")
			require.NotContains(t, recorder.Body.String(), "data: [DONE]")
			require.NotEmpty(t, checkpoints)
			last := checkpoints[len(checkpoints)-1]
			// 6 non-CJK runes*3 + 2 CJK runes*8 = 34 units -> 3 tokens.
			require.Equal(t, 3, last.CompletionTokens)
			require.Equal(t, len(fragments), last.DeliveredChunks)
			require.Equal(t, metering.GatewayCounterEstimateVersionV3, begin.CounterVersion)
			require.Equal(t, begin.CounterVersion, last.CounterVersion)
			require.Equal(t, last.CompletionTokens, persisted.CompletionTokens)
			require.Equal(t, metering.UsageSourceCounterV3, persisted.UsageSource)
			require.Equal(t, begin.CounterVersion, persisted.UsageEstimateVersion)
			require.Equal(t, begin.CounterVersion, persisted.StreamFence.CounterVersion)
		})
	}
}

func TestStreamCounterV3CompletedProviderUsageStaysAuthoritative(t *testing.T) {
	var usage billing.SettleRequest
	var start billing.StreamDeliveryStart
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		beginStreamFn:  func(_ context.Context, value billing.StreamDeliveryStart) error { start = value; return nil },
		persistUsageFn: func(_ context.Context, value billing.SettleRequest) error { usage = value; return nil },
	})
	require.NoError(t, relay.SetStreamCounterVersion(metering.GatewayCounterEstimateVersionV3))
	wire := checkpointCounterFrame(t, map[string]any{"content": "hello"}) + "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" + "data: {\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":11,\"total_tokens\":111}}\n\ndata: [DONE]\n\n"
	body := &countingReadCloser{reader: strings.NewReader(wire)}
	result := runStreamTransportTest(t, relay, body, "req-counter-final-v3")
	require.Contains(t, result.Body.String(), "data: [DONE]")
	require.Equal(t, metering.GatewayCounterEstimateVersionV3, start.CounterVersion)
	require.Equal(t, metering.UsageAuthorityProviderReported, usage.UsageAuthority)
	require.Equal(t, metering.UsageFinalityFinal, usage.UsageFinality)
	require.Empty(t, usage.UsageEstimateVersion)
	require.Equal(t, 11, usage.CompletionTokens)
}
