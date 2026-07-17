package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/streampolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type countingReadCloser struct {
	reader io.Reader
	closes atomic.Int32
}

func (body *countingReadCloser) Read(payload []byte) (int, error) {
	return body.reader.Read(payload)
}

func (body *countingReadCloser) Close() error {
	body.closes.Add(1)
	return nil
}

type readThenError struct {
	payload []byte
	done    bool
}

type failAfterWritesResponseWriter struct {
	header    http.Header
	body      strings.Builder
	writes    int
	failAt    int
	partialAt int
	flushes   int
}

func (writer *failAfterWritesResponseWriter) Header() http.Header {
	return writer.header
}

func (writer *failAfterWritesResponseWriter) WriteHeader(int) {}

func (writer *failAfterWritesResponseWriter) Write(payload []byte) (int, error) {
	writer.writes++
	if writer.writes == writer.partialAt {
		written := len(payload) / 2
		_, _ = writer.body.Write(payload[:written])
		return written, errors.New("partial downstream write")
	}
	if writer.writes == writer.failAt {
		return 0, errors.New("downstream write failed")
	}
	return writer.body.Write(payload)
}

func (writer *failAfterWritesResponseWriter) Flush() {
	writer.flushes++
}

func (writer *failAfterWritesResponseWriter) SetWriteDeadline(time.Time) error {
	return nil
}

func (reader *readThenError) Read(target []byte) (int, error) {
	if reader.done {
		return 0, io.ErrUnexpectedEOF
	}
	reader.done = true
	count := copy(target, reader.payload)
	return count, io.ErrUnexpectedEOF
}

func streamTransportTestDB() *testutil.MockDB {
	return &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*int) = 10000
			return nil
		}}
	}}
}

func runStreamTransportTest(t *testing.T, relay *Relay, body io.ReadCloser, requestID string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	response := &http.Response{StatusCode: http.StatusOK, Body: body}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	relay.relayStreamRequest(context.Background(), recorder, response, route, requestID, 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "hello"}}, logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 1000, nil, "")
	return recorder
}

func TestP6StreamTransportAcceptsLargeBoundedSSELines(t *testing.T) {
	for _, size := range []int{64*1024 + 1, 1024 * 1024} {
		t.Run(strings.ReplaceAll(time.Duration(size).String(), "µ", "u"), func(t *testing.T) {
			body := &countingReadCloser{reader: strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"" + strings.Repeat("x", size) + "\"}}]}\n\ndata: [DONE]\n\n")}
			relay := newTestRelay(streamTransportTestDB(), &mockBilling{})
			recorder := runStreamTransportTest(t, relay, body, "req-large-sse")
			if !strings.Contains(recorder.Body.String(), "data: [DONE]") {
				t.Fatalf("bounded line size %d did not complete", size)
			}
			if body.closes.Load() != 1 {
				t.Fatalf("body close count = %d, want 1", body.closes.Load())
			}
		})
	}
}

func TestP6StreamTransportOversizedLineCancelsWithoutSuccessTerminal(t *testing.T) {
	cancelCalls := 0
	settleCalls := 0
	body := &countingReadCloser{reader: strings.NewReader("data: " + strings.Repeat("x", 64*1024+1) + "\n\n")}
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		cancelFn: func(context.Context, string, int64) error {
			cancelCalls++
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{Success: true}, nil
		},
	})
	policy := streampolicy.Default()
	policy.MaxSSELineBytes = 64 * 1024
	if err := relay.SetStreamPolicy(policy); err != nil {
		t.Fatalf("SetStreamPolicy() error = %v", err)
	}
	recorder := runStreamTransportTest(t, relay, body, "req-oversized-sse")
	if cancelCalls != 1 || settleCalls != 0 {
		t.Fatalf("cancel=%d settle=%d, want 1/0", cancelCalls, settleCalls)
	}
	if !strings.Contains(recorder.Body.String(), "stream_error.frame_too_large") || strings.Contains(recorder.Body.String(), "data: [DONE]") {
		t.Fatalf("oversized stream advertised success: %s", recorder.Body.String())
	}
	if body.closes.Load() != 1 {
		t.Fatalf("body close count = %d, want 1", body.closes.Load())
	}
}

func TestP6StreamTransportFinalWriteFailurePreservesSettlementWithoutDone(t *testing.T) {
	markCalls := 0
	persistCalls := 0
	settleCalls := 0
	body := &countingReadCloser{reader: strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"delivered\"}}]}\n\ndata: [DONE]\n\n")}
	terminationMetadata := ""
	database := streamTransportTestDB()
	database.ExecFn = func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(query, "INSERT INTO request_trace_events") && len(args) > 17 && args[8] == "stream.terminated" {
			terminationMetadata, _ = args[17].(string)
		}
		return pgconn.CommandTag{}, nil
	}
	relay := newTestRelay(database, &mockBilling{
		markOutputFn: func(context.Context, string, int64) error {
			markCalls++
			return nil
		},
		persistUsageFn: func(context.Context, billing.SettleRequest) error {
			persistCalls++
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
		},
	})
	writer := &failAfterWritesResponseWriter{header: make(http.Header), failAt: 3}
	response := &http.Response{StatusCode: http.StatusOK, Body: body}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	relay.relayStreamRequest(ctx, writer, response, route, "req-final-write-failure", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "hello"}}, logger.New(logger.Fields{RequestID: "req-final-write-failure", OrgID: 1}), time.Now(), 1000, nil, "")

	if markCalls != 1 || persistCalls != 1 || settleCalls != 1 {
		t.Fatalf("mark=%d persist=%d settle=%d, want 1/1/1", markCalls, persistCalls, settleCalls)
	}
	if !strings.Contains(writer.body.String(), "delivered") || strings.Contains(writer.body.String(), "data: [DONE]") {
		t.Fatalf("unexpected downstream body after terminal write failure: %s", writer.body.String())
	}
	if !strings.Contains(terminationMetadata, `"error_code":"downstream_stream_write_failed"`) || !strings.Contains(terminationMetadata, `"billing_status":"settled_partial_stream"`) {
		t.Fatalf("missing sanitized termination evidence: %s", terminationMetadata)
	}
	if body.closes.Load() != 1 {
		t.Fatalf("body close count = %d, want 1", body.closes.Load())
	}
}

func TestP6F41BStreamCompleteHasMonotonicDeliveryReceipt(t *testing.T) {
	var outcomes []string
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			outcomes = append(outcomes, event.Outcome)
			return nil
		},
	})
	body := &countingReadCloser{reader: strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"complete\"}}]}\n\ndata: [DONE]\n\n")}
	recorder := runStreamTransportTest(t, relay, body, "req-p6f41b-complete")
	if got, want := strings.Join(outcomes, ","), "write_started,partial,completed"; got != want {
		t.Fatalf("delivery outcomes = %s, want %s", got, want)
	}
	if !strings.Contains(recorder.Body.String(), "data: [DONE]") {
		t.Fatalf("completed stream lost terminal marker: %s", recorder.Body.String())
	}
}

func TestP6F41BInterruptedStreamSeparatesProviderUsageFromCustomerCharge(t *testing.T) {
	var outcomes []string
	var persisted, settled billing.SettleRequest
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			outcomes = append(outcomes, event.Outcome)
			return nil
		},
		persistUsageFn: func(_ context.Context, req billing.SettleRequest) error {
			persisted = req
			return nil
		},
		settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settled = req
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
		},
	})
	body := &countingReadCloser{reader: &readThenError{payload: []byte(
		"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n" +
			"data: {\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":100,\"total_tokens\":200}}\n\n",
	)}}
	recorder := runStreamTransportTest(t, relay, body, "req-p6f41b-separated")
	if got, want := strings.Join(outcomes, ","), "write_started,partial,interrupted"; got != want {
		t.Fatalf("delivery outcomes = %s, want %s", got, want)
	}
	if persisted.UsageAuthority != metering.UsageAuthorityGatewayCounter ||
		persisted.UsagePurpose != metering.UsagePurposeCustomerCharge ||
		persisted.UsageFinality != metering.UsageFinalityEstimated ||
		persisted.UsageEstimateVersion != metering.GatewayCounterEstimateVersionV2 {
		t.Fatalf("interrupted stream did not use delivered-counter authority: %+v", persisted)
	}
	if persisted.CompletionTokens <= 0 || persisted.CompletionTokens >= 100 {
		t.Fatalf("customer completion tokens = %d, want delivered subset below provider-reported 100", persisted.CompletionTokens)
	}
	if settled != persisted {
		t.Fatalf("settlement changed the durable delivered usage: persisted=%+v settled=%+v", persisted, settled)
	}
	if strings.Contains(recorder.Body.String(), "data: [DONE]") {
		t.Fatalf("interrupted stream advertised completion: %s", recorder.Body.String())
	}
}

func TestP6F41BCompleteStreamIncompleteUsageUsesGatewayCounter(t *testing.T) {
	var persisted, settled billing.SettleRequest
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		persistUsageFn: func(_ context.Context, req billing.SettleRequest) error {
			persisted = req
			return nil
		},
		settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settled = req
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
		},
	})
	body := &countingReadCloser{reader: strings.NewReader(
		"data: {\"choices\":[{\"delta\":{\"content\":\"delivered\"}}]}\n\n" +
			"data: {\"usage\":{\"prompt_tokens\":100}}\n\n" +
			"data: [DONE]\n\n",
	)}
	recorder := runStreamTransportTest(t, relay, body, "req-p6f41b-incomplete-usage")
	if !strings.Contains(recorder.Body.String(), "data: [DONE]") {
		t.Fatalf("completed stream lost terminal marker: %s", recorder.Body.String())
	}
	if persisted.UsageAuthority != metering.UsageAuthorityGatewayCounter ||
		persisted.UsageEstimateVersion != metering.GatewayCounterEstimateVersionV2 ||
		persisted.CompletionTokens <= 0 {
		t.Fatalf("incomplete stream usage did not use delivered-counter authority: %+v", persisted)
	}
	if settled != persisted {
		t.Fatalf("settlement changed the durable delivered usage: persisted=%+v settled=%+v", persisted, settled)
	}
}

func TestP6F41BFirstStreamWriteFailureHasNoCustomerCharge(t *testing.T) {
	var outcomes []string
	persistCalls := 0
	settleCalls := 0
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			outcomes = append(outcomes, event.Outcome)
			return nil
		},
		persistUsageFn: func(context.Context, billing.SettleRequest) error {
			persistCalls++
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{Success: true}, nil
		},
	})
	writer := &failAfterWritesResponseWriter{header: make(http.Header), failAt: 1}
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"not delivered\"}}]}\n\ndata: [DONE]\n\n"))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	relay.relayStreamRequest(context.Background(), writer, response, route, "req-p6f41b-first-write-fail", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "hello"}}, logger.New(logger.Fields{RequestID: "req-p6f41b-first-write-fail", OrgID: 1}), time.Now(), 1000, nil, "")
	if got, want := strings.Join(outcomes, ","), "write_started,write_failed"; got != want {
		t.Fatalf("delivery outcomes = %s, want %s", got, want)
	}
	if persistCalls != 0 || settleCalls != 0 {
		t.Fatalf("undelivered stream persisted=%d settled=%d, want 0/0", persistCalls, settleCalls)
	}
}

func TestP6F41BPartialSSEFrameWriteHasNoCustomerCharge(t *testing.T) {
	var events []billing.DeliveryEvent
	persistCalls := 0
	settleCalls := 0
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			events = append(events, event)
			return nil
		},
		persistUsageFn: func(context.Context, billing.SettleRequest) error {
			persistCalls++
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{Success: true}, nil
		},
	})
	writer := &failAfterWritesResponseWriter{header: make(http.Header), partialAt: 1}
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"truncated\"}}]}\n\ndata: [DONE]\n\n"))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	relay.relayStreamRequest(context.Background(), writer, response, route, "req-p6f41b-partial-frame", 1, 1, "gpt-4o", "gpt-4o", "127.0.0.1", []Message{{Role: "user", Content: "hello"}}, logger.New(logger.Fields{RequestID: "req-p6f41b-partial-frame", OrgID: 1}), time.Now(), 1000, nil, "")
	if len(events) != 2 || events[0].Outcome != billing.DeliveryOutcomeWriteStarted || events[1].Outcome != billing.DeliveryOutcomeWriteFailed {
		t.Fatalf("partial frame outcomes = %+v, want write_started/write_failed", events)
	}
	if events[1].DeliveredBytes <= 0 || events[1].DeliveredChunks != 0 {
		t.Fatalf("partial frame receipt lost bounded non-chargeable bytes: %+v", events[1])
	}
	if persistCalls != 0 || settleCalls != 0 {
		t.Fatalf("partial SSE frame persisted=%d settled=%d, want 0/0", persistCalls, settleCalls)
	}
}

func TestP6StreamTransportDoesNotBillFailedNormalizedDelta(t *testing.T) {
	settledCompletionTokens := -1
	body := &countingReadCloser{reader: strings.NewReader(
		"event: message_start\n" +
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"not delivered\"}}\n\n",
	)}
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		settleFn: func(_ context.Context, request billing.SettleRequest) (*billing.SettleResult, error) {
			settledCompletionTokens = request.CompletionTokens
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
		},
	})
	// The normalized assistant-role frame succeeds; the following text delta
	// fails before it can be flushed to the caller.
	writer := &failAfterWritesResponseWriter{header: make(http.Header), failAt: 2}
	response := &http.Response{StatusCode: http.StatusOK, Body: body}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "anthropic", Provider: "anthropic", AdapterType: adapters.TypeAnthropicNative}}
	relay.relayStreamRequest(context.Background(), writer, response, route, "req-normalized-write-failure", 1, 1, "company-chat", "company-chat", "127.0.0.1", []Message{{Role: "user", Content: "hello"}}, logger.New(logger.Fields{RequestID: "req-normalized-write-failure", OrgID: 1}), time.Now(), 1000, adapters.NewAnthropicNative(), "")

	if settledCompletionTokens != 0 {
		t.Fatalf("completion tokens = %d, want 0 for a normalized delta that was not delivered", settledCompletionTokens)
	}
	if strings.Contains(writer.body.String(), "not delivered") || strings.Contains(writer.body.String(), "data: [DONE]") {
		t.Fatalf("failed normalized delta leaked or advertised success: %s", writer.body.String())
	}
	if body.closes.Load() != 1 {
		t.Fatalf("body close count = %d, want 1", body.closes.Load())
	}
}

func TestP6StreamTransportReadFailureSettlesDeliveredOutputWithoutDone(t *testing.T) {
	markCalls := 0
	persistCalls := 0
	settleCalls := 0
	cancelCalls := 0
	body := &countingReadCloser{reader: &readThenError{payload: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")}}
	relay := newTestRelay(streamTransportTestDB(), &mockBilling{
		markOutputFn: func(context.Context, string, int64) error {
			markCalls++
			return nil
		},
		persistUsageFn: func(context.Context, billing.SettleRequest) error {
			persistCalls++
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
		},
		cancelFn: func(context.Context, string, int64) error {
			cancelCalls++
			return nil
		},
	})
	recorder := runStreamTransportTest(t, relay, body, "req-partial-sse")
	if markCalls != 1 || persistCalls != 1 || settleCalls != 1 || cancelCalls != 0 {
		t.Fatalf("mark=%d persist=%d settle=%d cancel=%d", markCalls, persistCalls, settleCalls, cancelCalls)
	}
	output := recorder.Body.String()
	if !strings.Contains(output, "partial") || !strings.Contains(output, "stream_error.upstream_read_failed") || strings.Contains(output, "data: [DONE]") {
		t.Fatalf("unexpected partial-stream terminal: %s", output)
	}
	if strings.Contains(output, "\"_uniroute\"") {
		t.Fatalf("read failure must not emit a success extension: %s", output)
	}
	if body.closes.Load() != 1 {
		t.Fatalf("body close count = %d, want 1", body.closes.Load())
	}
}
