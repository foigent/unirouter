package endpointprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/uniroute/unirouter/internal/modeloperation"
)

func TestPublicAnthropicMessagesStreamAcceptsOmittedThinkingAndGuardedText(t *testing.T) {
	stream := buildPublicAnthropicMessagesStream(t)

	start := parsePublicAnthropicStreamEvent(t, stream, "message_start", `{
		"type":"message_start",
		"message":{"id":"msg_provider","type":"message","role":"assistant","model":"provider-private-claude",
		"content":[],"stop_reason":null,"stop_sequence":null,
		"usage":{"input_tokens":10,"cache_creation_input_tokens":2,"cache_read_input_tokens":3,
		"output_tokens":0,"inference_geo":"us","service_tier":"standard"}}
	}`)
	var startBody map[string]any
	if json.Unmarshal(start.Body(), &startBody) != nil {
		t.Fatalf("start body=%s", start.Body())
	}
	message := startBody["message"].(map[string]any)
	if message["model"] != "public-claude" {
		t.Fatalf("provider model leaked: %+v", message)
	}

	parsePublicAnthropicStreamEvent(t, stream, "content_block_start", `{
		"type":"content_block_start","index":0,
		"content_block":{"type":"thinking","thinking":"","signature":""}
	}`)
	signature := parsePublicAnthropicStreamEvent(t, stream, "content_block_delta", `{
		"type":"content_block_delta","index":0,
		"delta":{"type":"signature_delta","signature":"opaque-signature"}
	}`)
	if signature.Body() == nil || len(signature.Texts()) != 0 {
		t.Fatalf("signature event=%+v body=%s", signature, signature.Body())
	}
	parsePublicAnthropicStreamEvent(t, stream, "content_block_stop", `{
		"type":"content_block_stop","index":0
	}`)
	parsePublicAnthropicStreamEvent(t, stream, "ping", `{"type":"ping"}`)
	parsePublicAnthropicStreamEvent(t, stream, "content_block_start", `{
		"type":"content_block_start","index":1,
		"content_block":{"type":"text","text":""}
	}`)
	delta := parsePublicAnthropicStreamEvent(t, stream, "content_block_delta", `{
		"type":"content_block_delta","index":1,
		"delta":{"type":"text_delta","text":"provider answer"}
	}`)
	if delta.Body() != nil || len(delta.Texts()) != 1 || delta.Texts()[0].Path != "content[1].text" {
		t.Fatalf("unguarded delta became renderable: body=%s texts=%+v", delta.Body(), delta.Texts())
	}
	if err := delta.CommitTexts([]string{"guarded answer"}); err != nil {
		t.Fatal(err)
	}
	var deltaBody map[string]any
	if json.Unmarshal(delta.Body(), &deltaBody) != nil ||
		deltaBody["delta"].(map[string]any)["text"] != "guarded answer" {
		t.Fatalf("delta body=%s", delta.Body())
	}
	parsePublicAnthropicStreamEvent(t, stream, "content_block_stop", `{
		"type":"content_block_stop","index":1
	}`)
	parsePublicAnthropicStreamEvent(t, stream, "message_delta", `{
		"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},
		"usage":{"output_tokens":4,"output_tokens_details":{"thinking_tokens":1}}
	}`)
	stop := parsePublicAnthropicStreamEvent(t, stream, "message_stop", `{"type":"message_stop"}`)
	if !stop.Terminal() || !stop.Success() || stop.CustomerUsage() != (PublicCustomerUsage{
		InputTokens: 15, CacheReadTokens: 3, CacheWriteTokens: 2, OutputTokens: 4,
	}) {
		t.Fatalf("terminal=%v success=%v usage=%+v", stop.Terminal(), stop.Success(), stop.CustomerUsage())
	}
	if _, err := stream.ParseEvent("ping", []byte(`{"type":"ping"}`)); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("terminal stream accepted another event: %v", err)
	}
}

func TestPublicAnthropicMessagesStreamAcceptsQueuedGuardedSummarizedThinkingBeforeSignature(t *testing.T) {
	contract := haiku45ThinkingPublicMessagesContract()
	contract.ExecutionProfileVersion = "1.2.0"
	stream := buildPublicAnthropicMessagesStreamForContract(t, []byte(`{
		"model":"public-claude","max_tokens":4096,"stream":true,
		"thinking":{"type":"enabled","budget_tokens":1024},
		"messages":[{"role":"user","content":"hello"}]
	}`), contract)
	parsePublicAnthropicStreamEvent(t, stream, "message_start", `{
		"type":"message_start","message":{"id":"msg_summary","type":"message","role":"assistant",
		"model":"provider-private-claude","content":[],"stop_reason":null,"stop_sequence":null,
		"usage":{"input_tokens":5,"output_tokens":0}}
	}`)
	parsePublicAnthropicStreamEvent(t, stream, "content_block_start", `{
		"type":"content_block_start","index":0,
		"content_block":{"type":"thinking","thinking":"","signature":""}
	}`)
	first := parsePublicAnthropicStreamEvent(t, stream, "content_block_delta", `{
		"type":"content_block_delta","index":0,
		"delta":{"type":"thinking_delta","thinking":"first summary "}
	}`)
	second := parsePublicAnthropicStreamEvent(t, stream, "content_block_delta", `{
		"type":"content_block_delta","index":0,
		"delta":{"type":"thinking_delta","thinking":"second summary"}
	}`)
	if len(stream.pending) != 2 || first.Body() != nil || second.Body() != nil ||
		first.Texts()[0].Path != "content[0].thinking" || second.Texts()[0].Path != "content[0].thinking" {
		t.Fatalf("pending=%d first=%s second=%s texts=%+v/%+v",
			len(stream.pending), first.Body(), second.Body(), first.Texts(), second.Texts())
	}
	if err := first.CommitTexts([]string{"rewritten summary"}); !errors.Is(err, ErrPublicTextReplacement) ||
		len(stream.pending) != 2 {
		t.Fatalf("signed thinking rewrite err=%v pending=%d", err, len(stream.pending))
	}
	signature := parsePublicAnthropicStreamEvent(t, stream, "content_block_delta", `{
		"type":"content_block_delta","index":0,
		"delta":{"type":"signature_delta","signature":"opaque-signature"}
	}`)
	if signature.Body() != nil {
		t.Fatalf("signature rendered before guarded summaries: %s", signature.Body())
	}
	if err := first.CommitTexts([]string{"first summary "}); err != nil {
		t.Fatal(err)
	}
	if signature.Body() != nil {
		t.Fatalf("signature rendered with one summary still pending: %s", signature.Body())
	}
	if err := second.CommitTexts([]string{"second summary"}); err != nil {
		t.Fatal(err)
	}
	if signature.Body() == nil || len(stream.pending) != 0 {
		t.Fatalf("signature=%s pending=%d", signature.Body(), len(stream.pending))
	}
	parsePublicAnthropicStreamEvent(t, stream, "content_block_stop", `{
		"type":"content_block_stop","index":0
	}`)
	parsePublicAnthropicStreamEvent(t, stream, "content_block_start", `{
		"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}
	}`)
	answer := parsePublicAnthropicStreamEvent(t, stream, "content_block_delta", `{
		"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"provider answer"}
	}`)
	if err := answer.CommitTexts([]string{"guarded answer"}); err != nil {
		t.Fatal(err)
	}
	parsePublicAnthropicStreamEvent(t, stream, "content_block_stop", `{
		"type":"content_block_stop","index":1
	}`)
	parsePublicAnthropicStreamEvent(t, stream, "message_delta", `{
		"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},
		"usage":{"output_tokens":8,"output_tokens_details":{"thinking_tokens":5}}
	}`)
	stop := parsePublicAnthropicStreamEvent(t, stream, "message_stop", `{"type":"message_stop"}`)
	if !stop.Success() || stop.CustomerUsage().InputTokens != 5 || stop.CustomerUsage().OutputTokens != 8 {
		t.Fatalf("stop=%+v usage=%+v", stop, stop.CustomerUsage())
	}
}

func TestPublicAnthropicMessagesStreamRequiresManualThinkingEvidenceBeforeText(t *testing.T) {
	contract := haiku45ThinkingPublicMessagesContract()
	contract.ExecutionProfileVersion = "1.2.0"
	stream := buildPublicAnthropicMessagesStreamForContract(t, []byte(`{
		"model":"public-claude","max_tokens":4096,"stream":true,
		"thinking":{"type":"enabled","budget_tokens":1024},
		"messages":[{"role":"user","content":"hello"}]
	}`), contract)
	parsePublicAnthropicStreamEvent(t, stream, "message_start", `{
		"type":"message_start","message":{"id":"msg_missing","type":"message","role":"assistant",
		"model":"provider-private-claude","content":[],"stop_reason":null,"stop_sequence":null,
		"usage":{"input_tokens":5,"output_tokens":0}}
	}`)
	_, err := stream.ParseEvent("content_block_start", []byte(`{
		"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}
	}`))
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("missing required manual thinking err=%v", err)
	}
}

func TestPublicAnthropicMessagesStreamRejectsEmptyGuardReplacementAtomically(t *testing.T) {
	stream := buildPublicAnthropicMessagesStream(t)
	parsePublicAnthropicStreamEvent(t, stream, "message_start", `{
		"type":"message_start","message":{"id":"msg_guard","type":"message","role":"assistant",
		"model":"provider-private-claude","content":[],"stop_reason":null,"stop_sequence":null,
		"usage":{"input_tokens":5,"output_tokens":0}}
	}`)
	parsePublicAnthropicStreamEvent(t, stream, "content_block_start", `{
		"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}
	}`)
	delta := parsePublicAnthropicStreamEvent(t, stream, "content_block_delta", `{
		"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"provider answer"}
	}`)
	if err := delta.CommitTexts([]string{""}); !errors.Is(err, ErrPublicTextReplacement) {
		t.Fatalf("empty replacement err=%v", err)
	}
	if delta.Body() != nil || len(stream.pending) != 1 || stream.pending[0] != delta ||
		delta.block.customerText.Len() != 0 {
		t.Fatalf("failed commit mutated state: body=%s pending=%v delta=%p customer=%q",
			delta.Body(), stream.pending, delta, delta.block.customerText.String())
	}
	if err := delta.CommitTexts([]string{"guarded answer"}); err != nil {
		t.Fatal(err)
	}
	if delta.Body() == nil || len(stream.pending) != 0 || delta.block.customerText.String() != "guarded answer" {
		t.Fatalf("valid retry did not commit atomically: body=%s pending=%v customer=%q",
			delta.Body(), stream.pending, delta.block.customerText.String())
	}
}

func TestPublicAnthropicMessagesStreamAcceptsOpaqueRedactedThinking(t *testing.T) {
	stream := buildPublicAnthropicMessagesStream(t)
	parsePublicAnthropicStreamEvent(t, stream, "message_start", `{
		"type":"message_start","message":{"id":"msg_redacted","type":"message","role":"assistant",
		"model":"provider-private-claude","content":[],"stop_reason":null,"stop_sequence":null,
		"usage":{"input_tokens":5,"output_tokens":0}}
	}`)
	redacted := parsePublicAnthropicStreamEvent(t, stream, "content_block_start", `{
		"type":"content_block_start","index":0,
		"content_block":{"type":"redacted_thinking","data":"opaque-redacted-data"}
	}`)
	if redacted.Body() == nil || len(redacted.Texts()) != 0 {
		t.Fatalf("redacted evidence treated as text: body=%s texts=%+v", redacted.Body(), redacted.Texts())
	}
	parsePublicAnthropicStreamEvent(t, stream, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	parsePublicAnthropicStreamEvent(t, stream, "content_block_start", `{
		"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}
	}`)
	delta := parsePublicAnthropicStreamEvent(t, stream, "content_block_delta", `{
		"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer"}
	}`)
	if err := delta.CommitTexts([]string{"answer"}); err != nil {
		t.Fatal(err)
	}
	parsePublicAnthropicStreamEvent(t, stream, "content_block_stop", `{"type":"content_block_stop","index":1}`)
	parsePublicAnthropicStreamEvent(t, stream, "message_delta", `{
		"type":"message_delta","delta":{"stop_reason":"max_tokens","stop_sequence":null},
		"usage":{"output_tokens":2,"output_tokens_details":{"thinking_tokens":1}}
	}`)
	stop := parsePublicAnthropicStreamEvent(t, stream, "message_stop", `{"type":"message_stop"}`)
	if !stop.Success() || stop.CustomerUsage().InputTokens != 5 || stop.CustomerUsage().OutputTokens != 2 {
		t.Fatalf("stop=%+v usage=%+v", stop, stop.CustomerUsage())
	}
}

func TestPublicAnthropicMessagesStreamFailsClosedOnUnsupportedOrIncompleteEvidence(t *testing.T) {
	tests := []struct {
		name   string
		events []anthropicStreamTestEvent
		want   error
	}{
		{
			name: "visible thinking delta",
			events: append(anthropicStreamStartEvents(),
				anthropicStreamTestEvent{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"secret"}}`}),
			want: ErrUnsupportedSemantics,
		},
		{
			name: "thinking block missing signature",
			events: append(anthropicStreamStartEvents(),
				anthropicStreamTestEvent{"content_block_stop", `{"type":"content_block_stop","index":0}`}),
			want: ErrInvalidResponse,
		},
		{
			name: "tool block",
			events: append(anthropicStreamMessageStartOnly(),
				anthropicStreamTestEvent{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use"}}`}),
			want: ErrUnsupportedSemantics,
		},
		{
			name:   "unknown event",
			events: []anthropicStreamTestEvent{{"message_chunk", `{"type":"message_chunk"}`}},
			want:   ErrUnsupportedSemantics,
		},
		{
			name: "message stop before delta",
			events: append(anthropicStreamMessageStartOnly(),
				anthropicStreamTestEvent{"message_stop", `{"type":"message_stop"}`}),
			want: ErrInvalidResponse,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream := buildPublicAnthropicMessagesStream(t)
			var err error
			for _, input := range test.events {
				_, err = stream.ParseEvent(input.event, []byte(input.data))
				if err != nil {
					break
				}
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("err=%v want %v", err, test.want)
			}
		})
	}
}

func TestPublicAnthropicMessagesStreamRejectsUsageConflictAndReasoningMismatch(t *testing.T) {
	for _, test := range []struct {
		name  string
		final string
	}{
		{
			name:  "input conflict",
			final: `{"input_tokens":11,"output_tokens":2,"output_tokens_details":{"thinking_tokens":1}}`,
		},
		{
			name:  "reasoning tokens without evidence",
			final: `{"output_tokens":2,"output_tokens_details":{"thinking_tokens":1}}`,
		},
		{
			name:  "missing output",
			final: `{"input_tokens":10}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := buildPublicAnthropicMessagesStream(t)
			parsePublicAnthropicStreamEvent(t, stream, "message_start", `{
				"type":"message_start","message":{"id":"msg_usage","type":"message","role":"assistant",
				"model":"provider-private-claude","content":[],"stop_reason":null,"stop_sequence":null,
				"usage":{"input_tokens":10,"output_tokens":0}}
			}`)
			parsePublicAnthropicStreamEvent(t, stream, "content_block_start", `{
				"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}
			}`)
			delta := parsePublicAnthropicStreamEvent(t, stream, "content_block_delta", `{
				"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer"}
			}`)
			if err := delta.CommitTexts([]string{"answer"}); err != nil {
				t.Fatal(err)
			}
			parsePublicAnthropicStreamEvent(t, stream, "content_block_stop", `{"type":"content_block_stop","index":0}`)
			_, err := stream.ParseEvent("message_delta", []byte(`{
				"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},
				"usage":`+test.final+`
			}`))
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestPublicAnthropicMessagesStreamSanitizesProviderErrorWithoutUsage(t *testing.T) {
	stream := buildPublicAnthropicMessagesStream(t)
	event := parsePublicAnthropicStreamEvent(t, stream, "error", `{
		"type":"error","error":{"type":"overloaded_error","message":"private provider diagnostic"}
	}`)
	if !event.Terminal() || event.Success() || event.CustomerUsage() != (PublicCustomerUsage{}) ||
		string(event.Body()) != `{"error":{"message":"upstream stream failed","type":"upstream_error"},"type":"error"}` {
		t.Fatalf("event terminal=%v success=%v usage=%+v body=%s",
			event.Terminal(), event.Success(), event.CustomerUsage(), event.Body())
	}
}

func TestPublicAnthropicMessagesStreamingRequestedIsTransportOnlyAndStrict(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want bool
		err  bool
	}{
		{name: "true", body: `{"model":"claude","stream":true}`, want: true},
		{name: "false", body: `{"model":"claude","stream":false}`},
		{name: "omitted", body: `{"model":"claude"}`},
		{name: "string", body: `{"model":"claude","stream":"true"}`, err: true},
		{name: "duplicate", body: `{"model":"claude","stream":true,"stream":true}`, err: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := PublicAnthropicMessagesStreamingRequested([]byte(test.body))
			if got != test.want || (err != nil) != test.err {
				t.Fatalf("got=%v err=%v", got, err)
			}
		})
	}
}

type anthropicStreamTestEvent struct {
	event string
	data  string
}

func buildPublicAnthropicMessagesStream(t *testing.T) *PublicAnthropicMessagesStream {
	t.Helper()
	return buildPublicAnthropicMessagesStreamForContract(t, []byte(`{
		"model":"public-claude","max_tokens":64,"stream":true,
		"messages":[{"role":"user","content":"hello"}]
	}`), sonnet5ControlsPublicMessagesContract())
}

func buildPublicAnthropicMessagesStreamForContract(
	t *testing.T,
	body []byte,
	contract PublicRequestContract,
) *PublicAnthropicMessagesStream {
	t.Helper()
	identity := PublicContractIdentity{
		ModelOperationID:           string(modeloperation.IDMessages),
		PublicPath:                 modeloperation.PathMessages,
		ExecutorContractVersion:    "unirouter.endpoint-profile-messages-dual-executor.v1",
		ProfileID:                  ProfileAnthropicMessagesTextV2,
		ProfileVersion:             "1.0.0",
		DeliveryContractVersion:    PublicAnthropicMessagesDeliveryContractV2,
		ChargeContractVersion:      PublicCustomerChargeContractV1,
		IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
	profile, err := NewPublicStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := profile.PrepareRequest(body, "public-claude", contract)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.anthropic.com", APIKey: "secret", RequestID: "req-stream",
		MappedModel: "provider-private-claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := upstream.NewAnthropicMessagesStream()
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func parsePublicAnthropicStreamEvent(
	t *testing.T,
	stream *PublicAnthropicMessagesStream,
	event string,
	data string,
) *PublicAnthropicMessagesStreamEvent {
	t.Helper()
	parsed, err := stream.ParseEvent(event, []byte(data))
	if err != nil {
		t.Fatalf("parse %s: %v", event, err)
	}
	return parsed
}

func anthropicStreamMessageStartOnly() []anthropicStreamTestEvent {
	return []anthropicStreamTestEvent{{"message_start", `{
		"type":"message_start","message":{"id":"msg_negative","type":"message","role":"assistant",
		"model":"provider-private-claude","content":[],"stop_reason":null,"stop_sequence":null,
		"usage":{"input_tokens":10,"output_tokens":0}}
	}`}}
}

func anthropicStreamStartEvents() []anthropicStreamTestEvent {
	return append(anthropicStreamMessageStartOnly(), anthropicStreamTestEvent{
		"content_block_start", `{
			"type":"content_block_start","index":0,
			"content_block":{"type":"thinking","thinking":"","signature":""}
		}`,
	})
}
