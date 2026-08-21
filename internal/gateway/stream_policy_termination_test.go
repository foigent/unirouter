package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/streampolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type delayedSegmentReader struct {
	segments [][]byte
	delay    time.Duration
	index    int
	offset   int
}

func (r *delayedSegmentReader) Read(p []byte) (int, error) {
	for r.index < len(r.segments) && r.offset >= len(r.segments[r.index]) {
		r.index++
		r.offset = 0
		if r.index < len(r.segments) && r.delay > 0 {
			time.Sleep(r.delay)
		}
	}
	if r.index >= len(r.segments) {
		return 0, io.EOF
	}
	n := copy(p, r.segments[r.index][r.offset:])
	r.offset += n
	return n, nil
}

func p6f11StreamGuardrail(t *testing.T, action string) *guardrail.Pinned {
	t.Helper()
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 61, Name: "p6f11 stream email", Version: 1, Stage: guardrail.StageStreamOutput,
		DetectorRef: guardrail.DetectorPII, Action: action,
		Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureFailClosed,
		Config: json.RawMessage(`{"entities":["email"]}`),
	})
	return service.Pin(guardrail.Subject{OrgID: 1, APIKeyID: 2, Model: "gpt-4o", Endpoint: "chat"})
}

func TestP6F11StreamGuardrailDetectsCrossFrameContent(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionBlock)
	profile := pinned.StreamProfile()
	if !profile.Enforced || profile.LookbehindBytes < len("alice@") {
		t.Fatalf("unexpected synchronous stream profile: %+v", profile)
	}
	history := make(map[string]string)
	first, err := inspectGuardedStreamFrame(t.Context(), pinned, profile,
		[]byte(`{"choices":[{"delta":{"content":"alice@"}}]}`), history)
	if err != nil || first.Result.Decision == guardrail.DecisionBlock {
		t.Fatalf("first partial frame should remain undecided: result=%+v err=%v", first.Result, err)
	}
	second, err := inspectGuardedStreamFrame(t.Context(), pinned, profile,
		[]byte(`{"choices":[{"delta":{"content":"example.com"}}]}`), history)
	if err != nil {
		t.Fatal(err)
	}
	if second.Result.Decision != guardrail.DecisionBlock {
		t.Fatalf("cross-frame email was not blocked: %+v", second.Result)
	}
}

func TestP6F11StreamGuardrailBlocksCrossFrameContentInsideOneMicroBuffer(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionBlock)
	profile := pinned.StreamProfile()
	batch, err := inspectGuardedStreamBatch(t.Context(), pinned, profile, [][]byte{
		[]byte(`{"choices":[{"index":0,"delta":{"content":"alice@"}}]}`),
		[]byte(`{"choices":[{"index":0,"delta":{"content":"example.com"}}]}`),
	}, make(map[string]string))
	if err != nil {
		t.Fatal(err)
	}
	expectedBytes := len(`{"choices":[{"index":0,"delta":{"content":"alice@"}}]}`) +
		len(`{"choices":[{"index":0,"delta":{"content":"example.com"}}]}`)
	if batch.Result.Decision != guardrail.DecisionBlock || batch.BufferedBytes != expectedBytes {
		t.Fatalf("micro-buffer did not withhold the combined unsafe value: %+v", batch)
	}
}

func TestP6F11StreamGuardrailUsesSemanticChoiceIndexesAcrossSparseFrames(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionBlock)
	profile := pinned.StreamProfile()
	history := make(map[string]string)
	first, err := inspectGuardedStreamFrame(t.Context(), pinned, profile,
		[]byte(`{"choices":[{"index":1,"delta":{"content":"alice@"}}]}`), history)
	if err != nil || first.Result.Decision == guardrail.DecisionBlock {
		t.Fatalf("first sparse choice frame should remain undecided: result=%+v err=%v", first.Result, err)
	}
	otherChoice, err := inspectGuardedStreamFrame(t.Context(), pinned, profile,
		[]byte(`{"choices":[{"index":0,"delta":{"content":"example.com"}}]}`), history)
	if err != nil || otherChoice.Result.Decision == guardrail.DecisionBlock {
		t.Fatalf("different semantic choices were mixed: result=%+v err=%v", otherChoice.Result, err)
	}
	sameChoice, err := inspectGuardedStreamFrame(t.Context(), pinned, profile,
		[]byte(`{"choices":[{"index":1,"delta":{"content":"example.com"}}]}`), history)
	if err != nil {
		t.Fatal(err)
	}
	if sameChoice.Result.Decision != guardrail.DecisionBlock {
		t.Fatalf("same semantic choice did not retain lookbehind: %+v", sameChoice.Result)
	}
}

func TestP6F11EnforcedStreamRejectsUnknownContentShapeButAllowsUsageMetadata(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionBlock)
	profile := pinned.StreamProfile()
	_, err := inspectGuardedStreamBatch(t.Context(), pinned, profile, [][]byte{
		[]byte(`{"choices":[{"delta":{"content":{"type":"unknown"}}}]}`),
	}, make(map[string]string))
	if !errors.Is(err, errStreamGuardrailUnsupportedText) {
		t.Fatalf("unknown model content shape was not failed closed: %v", err)
	}
	_, err = inspectGuardedStreamBatch(t.Context(), pinned, profile, [][]byte{
		[]byte(`{"output_text":"alice@example.com"}`),
	}, make(map[string]string))
	if !errors.Is(err, errStreamGuardrailUnsupportedText) {
		t.Fatalf("unknown top-level output text was not failed closed: %v", err)
	}
	_, err = inspectGuardedStreamBatch(t.Context(), pinned, profile, [][]byte{
		[]byte(`{"choices":[{"delta":{"content":"safe","new_text":"alice@example.com"}}]}`),
	}, make(map[string]string))
	if !errors.Is(err, errStreamGuardrailUnsupportedText) {
		t.Fatalf("unknown delta text field was not failed closed: %v", err)
	}
	advisoryOnly, err := inspectGuardedStreamBatch(t.Context(), pinned, guardrail.StreamProfile{Advisory: true}, [][]byte{
		[]byte(`{"output_text":"compatibility passthrough"}`),
	}, make(map[string]string))
	if !errors.Is(err, errStreamGuardrailUnsupportedText) || len(advisoryOnly.Frames) != 1 {
		t.Fatalf("non-enforced unknown shape was not withheld: batch=%+v err=%v", advisoryOnly, err)
	}
	usageOnly, err := inspectGuardedStreamBatch(t.Context(), pinned, profile, [][]byte{
		[]byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`),
	}, make(map[string]string))
	if err != nil || len(usageOnly.Frames) != 1 {
		t.Fatalf("bounded usage-only metadata was incorrectly blocked: batch=%+v err=%v", usageOnly, err)
	}
	terminal, err := inspectGuardedStreamBatch(t.Context(), pinned, profile, [][]byte{
		[]byte(`{"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1,"model":"kimi-k2.6","choices":[{"index":0,"delta":{},"finish_reason":"stop","logprobs":null}],"usage":{"prompt_tokens":19,"completion_tokens":13,"total_tokens":32,"cached_tokens":12,"completion_tokens_details":{"reasoning_tokens":10}}}`),
	}, make(map[string]string))
	if err != nil || len(terminal.Frames) != 1 {
		t.Fatalf("nullable terminal logprobs metadata was incorrectly blocked: batch=%+v err=%v", terminal, err)
	}
	choiceUsageTerminal, err := inspectGuardedStreamBatch(t.Context(), pinned, profile, [][]byte{
		[]byte(`{"id":"chatcmpl-kimi","object":"chat.completion.chunk","created":1,"model":"kimi-k2.6","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":19,"completion_tokens":13,"total_tokens":32,"completion_tokens_details":{"reasoning_tokens":10}}}]}`),
	}, make(map[string]string))
	if err != nil || len(choiceUsageTerminal.Frames) != 1 {
		t.Fatalf("bounded choice-level terminal usage was incorrectly blocked: batch=%+v err=%v", choiceUsageTerminal, err)
	}
	_, err = inspectGuardedStreamBatch(t.Context(), pinned, profile, [][]byte{
		[]byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"provider_note":"hidden"}}]}`),
	}, make(map[string]string))
	if !errors.Is(err, errStreamGuardrailUnsupportedText) {
		t.Fatalf("choice-level nonnumeric metadata bypassed the stream guardrail: %v", err)
	}
	_, err = inspectGuardedStreamBatch(t.Context(), pinned, profile, [][]byte{
		[]byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop","logprobs":{"content":[{"token":"hidden"}]}}]}`),
	}, make(map[string]string))
	if !errors.Is(err, errStreamGuardrailUnsupportedText) {
		t.Fatalf("non-null logprobs output bypassed the stream guardrail: %v", err)
	}
}

func TestP6F11StreamGuardrailInspectsReasoningContent(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionBlock)
	batch, err := inspectGuardedStreamBatch(t.Context(), pinned, pinned.StreamProfile(), [][]byte{
		[]byte(`{"choices":[{"index":0,"delta":{"reasoning_content":"alice@example.com"}}]}`),
	}, make(map[string]string))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Result.Decision != guardrail.DecisionBlock {
		t.Fatalf("reasoning content bypassed stream policy: %+v", batch.Result)
	}
}

func TestP6F11StreamAdvisoryCarriesBoundedCrossFrameContext(t *testing.T) {
	history := make(map[string]string)
	first := streamAdvisoryTextsWithHistory([]guardrail.Text{{Path: "choices[0].content", Content: "alice@"}}, history)
	second := streamAdvisoryTextsWithHistory([]guardrail.Text{{Path: "choices[0].content", Content: "example.com"}}, history)
	if len(first) != 1 || first[0].Content != "alice@" || len(second) != 1 || second[0].Content != "alice@example.com" {
		t.Fatalf("advisory context did not preserve the disclosed split value: first=%+v second=%+v", first, second)
	}
	long := strings.Repeat("x", guardrail.MaxStreamLookbehindBytes+10)
	streamAdvisoryTextsWithHistory([]guardrail.Text{{Path: "choices[0].content", Content: long}}, history)
	if len(history["choices[0].content"]) > guardrail.MaxStreamLookbehindBytes {
		t.Fatalf("advisory history exceeded its bound: %d", len(history["choices[0].content"]))
	}
}

func TestP6F11StreamRedactionNeverRewritesAlreadyDisclosedPrefix(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionRedact)
	profile := pinned.StreamProfile()
	history := make(map[string]string)
	_, err := inspectGuardedStreamFrame(t.Context(), pinned, profile,
		[]byte(`{"choices":[{"delta":{"content":"alice@"}}]}`), history)
	if err != nil {
		t.Fatal(err)
	}
	_, err = inspectGuardedStreamFrame(t.Context(), pinned, profile,
		[]byte(`{"choices":[{"delta":{"content":"example.com"}}]}`), history)
	if !errors.Is(err, errStreamGuardrailCrossBoundary) {
		t.Fatalf("cross-boundary transform = %v, want safe termination", err)
	}

	containedHistory := make(map[string]string)
	contained, err := inspectGuardedStreamFrame(t.Context(), pinned, profile,
		[]byte(`{"choices":[{"delta":{"content":"email alice@example.com"}}]}`), containedHistory)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contained.Data), "alice@example.com") || !strings.Contains(string(contained.Data), "[REDACTED]") {
		t.Fatalf("contained PII was not safely transformed: %s", contained.Data)
	}
}

func p6f11RelayDB() *testutil.MockDB {
	return &testutil.MockDB{
		QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				if len(dest) > 0 {
					if value, ok := dest[0].(*int); ok {
						*value = 10_000
					}
				}
				return nil
			}}
		},
		ExecFn: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}
}

func p6f11StreamContext(pinned *guardrail.Pinned, requestID string) context.Context {
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxKeyID, int64(2))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, requestID)
	ctx = contextWithGuardrailPin(ctx, pinned)
	ctx = contextWithRouteAttempt(ctx, 1, 1)
	ctx = contextWithAttemptID(ctx, "att_"+requestID)
	return contextWithStreamReservation(ctx, 1_000, false)
}

func TestRelayStreamRequestAcceptsKimiTerminalChoiceUsage(t *testing.T) {
	var settled billing.SettleRequest
	var terminal billing.DeliveryEvent
	var leaseState string
	var leaseReason string
	b := &mockBilling{
		settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settled = req
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
		},
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			if event.Outcome == billing.DeliveryOutcomeCompleted || event.Outcome == billing.DeliveryOutcomeInterrupted {
				terminal = event
			}
			return nil
		},
		finishStreamFn: func(_ context.Context, _ billing.StreamDeliveryStart, state, reason string) error {
			leaseState = state
			leaseReason = reason
			return nil
		},
	}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"KIMI_OK\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\",\"usage\":{\"prompt_tokens\":13,\"completion_tokens\":4,\"total_tokens\":17,\"completion_tokens_details\":{\"reasoning_tokens\":0}}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":13,\"completion_tokens\":4,\"total_tokens\":17,\"completion_tokens_details\":{\"reasoning_tokens\":0}}}\n\n" +
		"data: [DONE]\n\n"
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "Kimi K2.6", Provider: "kimi"}}
	requestID := "req-kimi-terminal-choice-usage"

	relay.relayStreamRequest(p6f11StreamContext(nil, requestID), rec, resp, route,
		requestID, 1, 2, "Kimi K2.6", "Kimi K2.6", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 1_000, nil, "")

	body := rec.Body.String()
	if !strings.Contains(body, "KIMI_OK") || !strings.Contains(body, `"finish_reason":"stop","usage":`) || strings.Count(body, "data: [DONE]") != 1 {
		t.Fatalf("Kimi stream did not reach its exact terminal marker: %s", body)
	}
	if strings.Contains(body, "stream_error.invalid_frame") {
		t.Fatalf("choice-level terminal usage was rejected as an invalid frame: %s", body)
	}
	if settled.PromptTokens != 13 || settled.CompletionTokens != 4 {
		t.Fatalf("provider usage was not settled exactly: %+v", settled)
	}
	if terminal.Outcome != billing.DeliveryOutcomeCompleted || terminal.ErrorCode != "" {
		t.Fatalf("stream delivery was not completed: %+v", terminal)
	}
	if leaseState != billing.StreamLeaseStateCompleted || leaseReason != "stream_completed" {
		t.Fatalf("stream lease terminal state = %q/%q", leaseState, leaseReason)
	}
}

func TestRelayStreamRequestNormalizesSafeMiniMaxControlMetadata(t *testing.T) {
	var settled billing.SettleRequest
	var terminal billing.DeliveryEvent
	b := &mockBilling{
		settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settled = req
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
		},
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			if event.Outcome == billing.DeliveryOutcomeCompleted || event.Outcome == billing.DeliveryOutcomeInterrupted {
				terminal = event
			}
			return nil
		},
	}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := "data: {\"id\":\"minimax-chunk\",\"object\":\"chat.completion.chunk\",\"created\":1780152150,\"model\":\"MiniMax-M2.7\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"MINIMAX_OK\",\"name\":\"MiniMax AI\",\"audio_content\":\"\"},\"finish_reason\":null}],\"input_sensitive\":false,\"output_sensitive\":false,\"input_sensitive_type\":0,\"output_sensitive_type\":0,\"output_sensitive_int\":0,\"base_resp\":{\"status_code\":0,\"status_msg\":\"\"}}\n\n" +
		"data: {\"id\":\"minimax-chunk\",\"object\":\"chat.completion.chunk\",\"created\":1780152150,\"model\":\"MiniMax-M2.7\",\"choices\":[{\"index\":0,\"delta\":{\"name\":\"MiniMax AI\",\"audio_content\":null},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":13,\"completion_tokens\":4,\"total_tokens\":17,\"total_characters\":0,\"prompt_tokens_details\":{\"cached_tokens\":0}},\"input_sensitive\":false,\"output_sensitive\":false,\"base_resp\":{\"status_code\":0,\"status_msg\":\"success\"}}\n\n" +
		"data: [DONE]\n\n"
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "MiniMax Token Plan", Provider: "minimax", AdapterType: adapters.TypeOpenAICompatible}}
	requestID := "req-minimax-safe-stream-metadata"

	relay.relayStreamRequest(p6f11StreamContext(nil, requestID), rec, resp, route,
		requestID, 1, 2, "MiniMax-M2.7", "MiniMax-M2.7", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 1_000, adapters.NewOpenAICompatible(), "")

	body := rec.Body.String()
	if !strings.Contains(body, "MINIMAX_OK") || strings.Count(body, "data: [DONE]") != 1 {
		t.Fatalf("MiniMax stream did not reach its terminal marker: %s", body)
	}
	for _, field := range []string{"input_sensitive", "output_sensitive", "base_resp", "audio_content", `\"name\"`} {
		if strings.Contains(body, field) {
			t.Fatalf("safe MiniMax stream control metadata %q escaped: %s", field, body)
		}
	}
	if strings.Contains(body, "stream_error.invalid_frame") {
		t.Fatalf("safe MiniMax stream metadata was rejected: %s", body)
	}
	if settled.PromptTokens != 13 || settled.CompletionTokens != 4 {
		t.Fatalf("provider usage was not settled exactly: %+v", settled)
	}
	if terminal.Outcome != billing.DeliveryOutcomeCompleted || terminal.ErrorCode != "" {
		t.Fatalf("stream delivery was not completed: %+v", terminal)
	}
}

func TestP6F11ExplicitZeroOutputReservationIsNotReplacedByDefault(t *testing.T) {
	state := streamReservationFromContext(contextWithStreamReservation(t.Context(), 0, false))
	if state.ReservedCompletionTokens != 0 || state.AllowExtension {
		t.Fatalf("explicit zero output ceiling was widened: %+v", state)
	}
}

func TestP6F11RelayBlocksUnsafeFrameWithoutRetryOrCustomerChargeForIt(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionBlock)
	var settled billing.SettleRequest
	var terminal billing.DeliveryEvent
	startedAt := time.Now()
	var deliveryBeganAt time.Time
	b := &mockBilling{
		beginStreamFn: func(context.Context, billing.StreamDeliveryStart) error {
			deliveryBeganAt = time.Now()
			return nil
		},
		settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			settled = req
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
		},
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			if event.Outcome == billing.DeliveryOutcomeInterrupted {
				terminal = event
			}
			return nil
		},
	}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := &delayedSegmentReader{delay: 300 * time.Millisecond, segments: [][]byte{
		[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"safe prefix\"}}]}\n\n"),
		[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"alice@example.com\"}}]}\n\ndata: [DONE]\n\n"),
	}}
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(stream)}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	requestID := "req-p6f11-stream-block"

	relay.relayStreamRequest(p6f11StreamContext(pinned, requestID), rec, resp, route,
		requestID, 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1",
		[]Message{{Role: "user", Content: "hello"}},
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 1_000, nil, "")

	body := rec.Body.String()
	if !strings.Contains(body, "safe prefix") || strings.Contains(body, "alice@example.com") {
		t.Fatalf("guardrail disclosure boundary failed: %s", body)
	}
	if !strings.Contains(body, "guardrail_error.stream_output_blocked") || strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream did not expose an explicit interrupted terminal: %s", body)
	}
	if settled.RequestID != requestID || settled.CompletionTokens <= 0 {
		t.Fatalf("only the already delivered prefix should settle: %+v", settled)
	}
	if terminal.ErrorCode != "guardrail_stream_blocked" || terminal.DeliveredBytes <= 0 || terminal.DeliveredChunks != 1 {
		t.Fatalf("missing interrupted delivery evidence: %+v", terminal)
	}
	bufferWait := deliveryBeganAt.Sub(startedAt)
	if deliveryBeganAt.IsZero() || bufferWait < 150*time.Millisecond || bufferWait > 750*time.Millisecond {
		t.Fatalf("200ms micro-buffer release was outside its measured bound: %s", bufferWait)
	}
}

func TestP6F11EnforcedStreamClosesMicroBufferAtFourKiB(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionBlock)
	prefix := `{"choices":[{"index":0,"delta":{"content":"`
	suffix := `"}}]}`
	fillerLength := 4*1024 - len(prefix) - len(suffix)
	if fillerLength <= 0 {
		t.Fatal("invalid frame fixture overhead")
	}
	firstFrame := prefix + strings.Repeat("x", fillerLength) + suffix
	if len(firstFrame) != 4*1024 {
		t.Fatalf("fixture is %d bytes, want 4096", len(firstFrame))
	}
	var terminal billing.DeliveryEvent
	b := &mockBilling{
		settleFn: func(_ context.Context, req billing.SettleRequest) (*billing.SettleResult, error) {
			return &billing.SettleResult{SettleID: 1, ActualCostMicros: 1, ChargedCostMicros: 1, Success: true}, nil
		},
		recordDeliveryFn: func(_ context.Context, event billing.DeliveryEvent) error {
			if event.Outcome == billing.DeliveryOutcomeInterrupted {
				terminal = event
			}
			return nil
		},
	}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := "data: " + firstFrame + "\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"alice@example.com\"}}]}\n\n" +
		"data: [DONE]\n\n"
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	requestID := "req-p6f11-buffer-four-kib"
	ctx := contextWithStreamReservation(p6f11StreamContext(pinned, requestID), 10_000, false)

	relay.relayStreamRequest(ctx, rec, resp, route, requestID, 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 10_000, nil, "")

	body := rec.Body.String()
	if !strings.Contains(body, strings.Repeat("x", 64)) || strings.Contains(body, "alice@example.com") {
		t.Fatalf("4 KiB close boundary did not isolate the next unsafe frame: %s", body)
	}
	if terminal.ErrorCode != "guardrail_stream_blocked" || terminal.DeliveredChunks != 1 {
		t.Fatalf("4 KiB boundary did not retain one safe delivered checkpoint: %+v", terminal)
	}
}

func TestP6F11BudgetExtensionDenialStopsBeforeNextModelFlush(t *testing.T) {
	extensionCalls := 0
	beginCalls := 0
	cancelCalls := 0
	b := &mockBilling{
		extendFn: func(_ context.Context, req billing.ExtendReservationRequest) (*billing.ExtendReservationResult, error) {
			extensionCalls++
			return &billing.ExtendReservationResult{Success: false, Sequence: req.Sequence, ErrorCode: "tpm_exceeded"}, nil
		},
		beginStreamFn: func(context.Context, billing.StreamDeliveryStart) error {
			beginCalls++
			return nil
		},
		cancelFn: func(context.Context, string, int64) error {
			cancelCalls++
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			t.Fatal("undelivered output must not settle")
			return nil, nil
		},
	}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"must-not-leak\"}}]}\n\ndata: [DONE]\n\n"
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	requestID := "req-p6f11-budget-denied"
	ctx := p6f11StreamContext(nil, requestID)
	ctx = contextWithStreamReservation(ctx, 129, true)

	relay.relayStreamRequest(ctx, rec, resp, route, requestID, 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 129, nil, "")

	body := rec.Body.String()
	if strings.Contains(body, "must-not-leak") || !strings.Contains(body, "billing_error.stream_budget_exhausted") {
		t.Fatalf("budget denial crossed the disclosure barrier: %s", body)
	}
	if extensionCalls != 1 || beginCalls != 0 || cancelCalls != 1 {
		t.Fatalf("unexpected budget terminal calls: extend=%d begin=%d cancel=%d", extensionCalls, beginCalls, cancelCalls)
	}
}

func TestP6F11EnforcedStreamRejectsSingleFrameBeyondFourKiBBuffer(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionBlock)
	beginCalls := 0
	cancelCalls := 0
	b := &mockBilling{
		beginStreamFn: func(context.Context, billing.StreamDeliveryStart) error {
			beginCalls++
			return nil
		},
		cancelFn: func(context.Context, string, int64) error {
			cancelCalls++
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			t.Fatal("oversized withheld frame must not settle")
			return nil, nil
		},
	}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"" +
		strings.Repeat("x", 4*1024+1) + "\"}}]}\n\ndata: [DONE]\n\n"
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	requestID := "req-p6f11-buffer-oversized"

	relay.relayStreamRequest(p6f11StreamContext(pinned, requestID), rec, resp, route,
		requestID, 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 1_000, nil, "")

	body := rec.Body.String()
	if strings.Contains(body, strings.Repeat("x", 64)) || !strings.Contains(body, "guardrail_error.stream_inspection_unavailable") {
		t.Fatalf("oversized enforced frame crossed the bounded buffer: %s", body)
	}
	if beginCalls != 0 || cancelCalls != 1 {
		t.Fatalf("oversized withheld frame reached billing output boundary: begin=%d cancel=%d", beginCalls, cancelCalls)
	}
}

func TestP6F11FinalizationLeaseLossHandsOffWithoutDualSettlement(t *testing.T) {
	renewCalls := 0
	settleCalls := 0
	finishCalls := 0
	b := &mockBilling{
		renewStreamFn: func(_ context.Context, start billing.StreamDeliveryStart) error {
			renewCalls++
			if start.LeaseTTL != streampolicy.FinalizationLeaseDuration {
				t.Errorf("terminal lease TTL = %s, want %s", start.LeaseTTL, streampolicy.FinalizationLeaseDuration)
			}
			return billing.ErrStreamLeaseConflict
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return nil, errors.New("must be handed to checkpoint recovery")
		},
		finishStreamFn: func(context.Context, billing.StreamDeliveryStart, string, string) error {
			finishCalls++
			return nil
		},
	}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"safe output\"}}]}\n\ndata: [DONE]\n\n"
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{Channel: models.Channel{ID: 1, Name: "openai", Provider: "openai"}}
	requestID := "req-p6f11-finalization-lease-loss"

	relay.relayStreamRequest(p6f11StreamContext(nil, requestID), rec, resp, route,
		requestID, 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 1_000, nil, "")

	body := rec.Body.String()
	if !strings.Contains(body, "safe output") || !strings.Contains(body, "stream_error.delivery_state_unavailable") ||
		strings.Contains(body, "data: [DONE]") {
		t.Fatalf("lease-loss handoff was not explicit: %s", body)
	}
	if renewCalls != 1 || settleCalls != 0 || finishCalls != 0 {
		t.Fatalf("lost lease retained terminal authority: renew=%d settle=%d finish=%d", renewCalls, settleCalls, finishCalls)
	}
}

func TestP6F11AnthropicNativeUnsafeOutputTerminatesBeforeNormalizedDisclosure(t *testing.T) {
	pinned := p6f11StreamGuardrail(t, guardrail.ActionBlock)
	beginCalls := 0
	cancelCalls := 0
	b := &mockBilling{
		beginStreamFn: func(context.Context, billing.StreamDeliveryStart) error {
			beginCalls++
			return nil
		},
		cancelFn: func(context.Context, string, int64) error {
			cancelCalls++
			return nil
		},
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			t.Fatal("withheld Anthropic output must not settle")
			return nil, nil
		},
	}
	relay := newTestRelay(p6f11RelayDB(), b)
	stream := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"alice@example.com\"}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(stream))}
	route := &RouteResult{Channel: models.Channel{
		ID: 1, Name: "anthropic", Provider: "anthropic", AdapterType: adapters.TypeAnthropicNative,
	}}
	requestID := "req-p6f11-anthropic-block"

	relay.relayStreamRequest(p6f11StreamContext(pinned, requestID), rec, resp, route,
		requestID, 1, 2, "gpt-4o", "gpt-4o", "127.0.0.1", nil,
		logger.New(logger.Fields{RequestID: requestID, OrgID: 1}), time.Now(), 1_000, adapters.NewAnthropicNative(), "")

	body := rec.Body.String()
	if strings.Contains(body, "alice@example.com") || !strings.Contains(body, "guardrail_error.stream_output_blocked") ||
		strings.Contains(body, "data: [DONE]") {
		t.Fatalf("Anthropic normalized output crossed the enforcement boundary: %s", body)
	}
	if beginCalls != 0 || cancelCalls != 1 {
		t.Fatalf("withheld Anthropic output reached billable delivery: begin=%d cancel=%d", beginCalls, cancelCalls)
	}
}
