package endpointprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/modeloperation"
)

func TestPublicResponsesStreamExactLifecycleReconcilesGuardedTextUsageAndModel(t *testing.T) {
	stream := buildPublicResponsesStream(t)
	events := []struct {
		name string
		data string
	}{
		{"response.created", responseStreamResponseEvent("response.created", 0, "in_progress", `[]`, `null`)},
		{"response.in_progress", responseStreamResponseEvent("response.in_progress", 1, "in_progress", `[]`, `null`)},
		{"response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","status":"in_progress","role":"assistant","content":[]},"sequence_number":2}`},
		{"response.content_part.added", `{"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]},"sequence_number":3}`},
		{"response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"Secret","sequence_number":4}`},
		{"response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":" answer","sequence_number":5}`},
		{"response.output_text.done", `{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":"Secret answer","sequence_number":6}`},
		{"response.content_part.done", `{"type":"response.content_part.done","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"Secret answer","annotations":[]},"sequence_number":7}`},
		{"response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Secret answer","annotations":[]}]},"sequence_number":8}`},
		{"response.completed", responseStreamResponseEvent("response.completed", 9, "completed", `[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Secret answer","annotations":[]}]}]`, `{"input_tokens":10,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":2},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":14}`)},
	}

	for index, input := range events {
		event, err := stream.ParseEvent(input.name, []byte(input.data))
		if err != nil {
			t.Fatalf("event %d %s: %v", index, input.name, err)
		}
		switch index {
		case 0, 1:
			if strings.Contains(string(event.Body()), "provider-private-model") || !strings.Contains(string(event.Body()), `"model":"public-text"`) {
				t.Fatalf("event %d leaked mapped model: %s", index, event.Body())
			}
		case 4:
			if event.Body() != nil || len(event.Texts()) != 1 || event.Texts()[0].Text != "Secret" {
				t.Fatalf("uncommitted delta=%s texts=%+v", event.Body(), event.Texts())
			}
			if err := event.CommitTexts([]string{"[redacted]"}); err != nil {
				t.Fatal(err)
			}
		case 5:
			if err := event.CommitTexts([]string{" answer"}); err != nil {
				t.Fatal(err)
			}
		case 6, 7, 8:
			if !strings.Contains(string(event.Body()), "[redacted] answer") || strings.Contains(string(event.Body()), "Secret answer") {
				t.Fatalf("done event %d was not reconciled: %s", index, event.Body())
			}
		case 9:
			if !event.Terminal() || !event.Success() || event.CustomerUsage() != (PublicCustomerUsage{
				InputTokens: 10, CacheReadTokens: 3, CacheWriteTokens: 2, OutputTokens: 4,
			}) {
				t.Fatalf("terminal=%v success=%v usage=%+v", event.Terminal(), event.Success(), event.CustomerUsage())
			}
			body := string(event.Body())
			if strings.Contains(body, "provider-private-model") || strings.Contains(body, "Secret answer") ||
				!strings.Contains(body, `"model":"public-text"`) || !strings.Contains(body, "[redacted] answer") {
				t.Fatalf("completed body=%s", body)
			}
		}
	}
}

func TestPublicResponsesStreamFailsClosedOnUnknownTypeSequenceAndUncommittedDelta(t *testing.T) {
	t.Run("unknown event", func(t *testing.T) {
		stream := buildPublicResponsesStream(t)
		_, err := stream.ParseEvent("response.web_search_call.in_progress", []byte(`{"type":"response.web_search_call.in_progress","sequence_number":0}`))
		if !errors.Is(err, ErrUnsupportedSemantics) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("event field mismatch", func(t *testing.T) {
		stream := buildPublicResponsesStream(t)
		_, err := stream.ParseEvent("response.created", []byte(responseStreamResponseEvent("response.in_progress", 0, "in_progress", `[]`, `null`)))
		if !errors.Is(err, ErrInvalidResponse) && !errors.Is(err, ErrInvalidJSON) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("sequence gap", func(t *testing.T) {
		stream := buildPublicResponsesStream(t)
		if _, err := stream.ParseEvent("response.created", []byte(responseStreamResponseEvent("response.created", 0, "in_progress", `[]`, `null`))); err != nil {
			t.Fatal(err)
		}
		_, err := stream.ParseEvent("response.in_progress", []byte(responseStreamResponseEvent("response.in_progress", 2, "in_progress", `[]`, `null`)))
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("sequence must start at zero", func(t *testing.T) {
		stream := buildPublicResponsesStream(t)
		_, err := stream.ParseEvent("response.created", []byte(responseStreamResponseEvent("response.created", 7, "in_progress", `[]`, `null`)))
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("duplicate member", func(t *testing.T) {
		stream := buildPublicResponsesStream(t)
		body := strings.Replace(responseStreamResponseEvent("response.created", 0, "in_progress", `[]`, `null`), `"type":"response.created"`, `"type":"response.created","type":"response.created"`, 1)
		_, err := stream.ParseEvent("response.created", []byte(body))
		if !errors.Is(err, ErrInvalidResponse) && !errors.Is(err, ErrInvalidJSON) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("uncommitted delta", func(t *testing.T) {
		stream := buildPublicResponsesStreamAtDelta(t)
		event, err := stream.ParseEvent("response.output_text.delta", []byte(`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"one","sequence_number":4}`))
		if err != nil {
			t.Fatal(err)
		}
		if event.Body() != nil {
			t.Fatalf("uncommitted body=%s", event.Body())
		}
		_, err = stream.ParseEvent("response.output_text.done", []byte(`{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":"one","sequence_number":5}`))
		if !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestPublicResponsesStreamRejectsProviderFinalTextOrUsageMismatch(t *testing.T) {
	stream := buildPublicResponsesStreamAtDelta(t)
	delta, err := stream.ParseEvent("response.output_text.delta", []byte(`{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"answer","sequence_number":4}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := delta.CommitTexts([]string{"answer"}); err != nil {
		t.Fatal(err)
	}
	_, err = stream.ParseEvent("response.output_text.done", []byte(`{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":"different","sequence_number":5}`))
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("text mismatch error=%v", err)
	}
}

func TestPublicResponsesStreamTerminalFailureCarriesNoProviderDiagnosticsOrUsage(t *testing.T) {
	stream := buildPublicResponsesStream(t)
	if _, err := stream.ParseEvent("response.created", []byte(responseStreamResponseEvent("response.created", 0, "in_progress", `[]`, `null`))); err != nil {
		t.Fatal(err)
	}
	event, err := stream.ParseEvent("response.failed", []byte(`{
		"type":"response.failed","sequence_number":1,
		"response":{"id":"resp_1","object":"response","status":"failed","model":"provider-private-model",
		"error":{"code":"private","message":"provider diagnostic"},"incomplete_details":null,
		"output":[],"usage":{"input_tokens":99,"output_tokens":99,"total_tokens":198}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !event.Terminal() || event.Success() || event.CustomerUsage() != (PublicCustomerUsage{}) {
		t.Fatalf("event=%+v usage=%+v", event, event.CustomerUsage())
	}
	body := string(event.Body())
	if strings.Contains(body, "private") || strings.Contains(body, "provider diagnostic") || strings.Contains(body, "99") ||
		!strings.Contains(body, `"model":"public-text"`) {
		t.Fatalf("failure body=%s", body)
	}
}

func TestPublicResponsesStreamReasoningItemRequiresComposedReasoningContract(t *testing.T) {
	withReasoning := buildPublicResponsesStreamWithContract(t, PublicRequestContract{
		ExecutionProfileID: "openai.gpt-5.6-sol.responses", ExecutionProfileVersion: "1.3.0",
		Reasoning: &PublicReasoningRequestContract{
			RequestObjectField: "reasoning", EffortField: "effort",
			AllowedEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"},
		},
	})
	for _, input := range []struct{ name, data string }{
		{"response.created", responseStreamResponseEvent("response.created", 0, "in_progress", `[]`, `null`)},
		{"response.in_progress", responseStreamResponseEvent("response.in_progress", 1, "in_progress", `[]`, `null`)},
		{"response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_1","type":"reasoning","status":"in_progress","summary":[],"content":[],"encrypted_content":null},"sequence_number":2}`},
		{"response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"rs_1","type":"reasoning","status":"completed","summary":[],"content":[],"encrypted_content":null},"sequence_number":3}`},
	} {
		if _, err := withReasoning.ParseEvent(input.name, []byte(input.data)); err != nil {
			t.Fatalf("%s: %v", input.name, err)
		}
	}

	withoutReasoning := buildPublicResponsesStream(t)
	if _, err := withoutReasoning.ParseEvent("response.created", []byte(responseStreamResponseEvent("response.created", 0, "in_progress", `[]`, `null`))); err != nil {
		t.Fatal(err)
	}
	if _, err := withoutReasoning.ParseEvent("response.in_progress", []byte(responseStreamResponseEvent("response.in_progress", 1, "in_progress", `[]`, `null`))); err != nil {
		t.Fatal(err)
	}
	_, err := withoutReasoning.ParseEvent("response.output_item.added", []byte(`{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_1","type":"reasoning","status":"in_progress","summary":[],"content":[]},"sequence_number":2}`))
	if !errors.Is(err, ErrUnsupportedSemantics) {
		t.Fatalf("reasoning item without contract error=%v", err)
	}
}

func TestPublicResponsesStreamItemTypeFieldsAreExact(t *testing.T) {
	for _, test := range []struct {
		name     string
		contract PublicRequestContract
		item     string
	}{
		{
			name: "message rejects reasoning-only encrypted content",
			item: `{"id":"msg_1","type":"message","status":"in_progress","role":"assistant","content":[],"encrypted_content":"opaque"}`,
		},
		{
			name: "reasoning rejects message-only role",
			contract: PublicRequestContract{
				ExecutionProfileID: "openai.gpt-5.6-sol.responses", ExecutionProfileVersion: "1.3.0",
				Reasoning: &PublicReasoningRequestContract{
					RequestObjectField: "reasoning", EffortField: "effort", AllowedEfforts: []string{"medium"},
				},
			},
			item: `{"id":"rs_1","type":"reasoning","status":"in_progress","role":"assistant","summary":[],"content":[]}`,
		},
		{
			name: "reasoning rejects opaque encrypted output",
			contract: PublicRequestContract{
				ExecutionProfileID: "openai.gpt-5.6-sol.responses", ExecutionProfileVersion: "1.3.0",
				Reasoning: &PublicReasoningRequestContract{
					RequestObjectField: "reasoning", EffortField: "effort", AllowedEfforts: []string{"medium"},
				},
			},
			item: `{"id":"rs_1","type":"reasoning","status":"in_progress","summary":[],"content":[],"encrypted_content":"opaque"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			contract := test.contract
			if contract.ExecutionProfileID == "" {
				contract = PublicRequestContract{
					ExecutionProfileID: "openai.gpt-5.6-sol.responses", ExecutionProfileVersion: "1.3.0",
				}
			}
			stream := buildPublicResponsesStreamWithContract(t, contract)
			if _, err := stream.ParseEvent("response.created", []byte(responseStreamResponseEvent("response.created", 0, "in_progress", `[]`, `null`))); err != nil {
				t.Fatal(err)
			}
			if _, err := stream.ParseEvent("response.in_progress", []byte(responseStreamResponseEvent("response.in_progress", 1, "in_progress", `[]`, `null`))); err != nil {
				t.Fatal(err)
			}
			_, err := stream.ParseEvent("response.output_item.added", []byte(`{"type":"response.output_item.added","output_index":0,"item":`+test.item+`,"sequence_number":2}`))
			if !errors.Is(err, ErrInvalidResponse) && !errors.Is(err, ErrUnsupportedSemantics) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestPublicResponsesStreamingRequestedIsTransportOnlyAndStrictlyTyped(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want bool
		err  bool
	}{
		{name: "true", body: `{"model":"public-text","input":"hello","stream":true}`, want: true},
		{name: "false", body: `{"model":"public-text","input":"hello","stream":false}`},
		{name: "omitted", body: `{"model":"public-text","input":"hello"}`},
		{name: "string rejected", body: `{"model":"public-text","input":"hello","stream":"true"}`, err: true},
		{name: "duplicate rejected", body: `{"model":"public-text","input":"hello","stream":true,"stream":true}`, err: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := PublicResponsesStreamingRequested([]byte(test.body))
			if (err != nil) != test.err || got != test.want {
				t.Fatalf("got=%v err=%v", got, err)
			}
		})
	}
}

func buildPublicResponsesStream(t *testing.T) *PublicResponsesStream {
	return buildPublicResponsesStreamWithContract(t, PublicRequestContract{
		ExecutionProfileID: "openai.gpt-5.6-sol.responses", ExecutionProfileVersion: "1.3.0",
	})
}

func buildPublicResponsesStreamWithContract(t *testing.T, contract PublicRequestContract) *PublicResponsesStream {
	t.Helper()
	identity := PublicContractIdentity{
		ModelOperationID: string(modeloperation.IDResponses), PublicPath: modeloperation.PathResponses,
		ExecutorContractVersion: "unirouter.endpoint-profile-responses-dual-executor.v1",
		ProfileID:               ProfileOpenAIResponsesTextV2, ProfileVersion: "1.0.0",
		DeliveryContractVersion:    PublicResponsesDeliveryContractV2,
		ChargeContractVersion:      PublicCustomerChargeContractV1,
		IdempotencyContractVersion: PublicIdempotencyContractV1,
	}
	profile, err := NewPublicStreamRegistry().Resolve(identity)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := profile.PrepareRequest([]byte(`{
		"model":"public-text","input":"hello","max_output_tokens":64,"stream":true
	}`), "public-text", contract)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := profile.BuildUpstream(context.Background(), prepared, PublicUpstreamInput{
		BaseURL: "https://api.openai.com", APIKey: "secret", RequestID: "req-stream",
		MappedModel: "provider-private-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := upstream.NewResponsesStream()
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func buildPublicResponsesStreamAtDelta(t *testing.T) *PublicResponsesStream {
	t.Helper()
	stream := buildPublicResponsesStream(t)
	for _, input := range []struct{ event, data string }{
		{"response.created", responseStreamResponseEvent("response.created", 0, "in_progress", `[]`, `null`)},
		{"response.in_progress", responseStreamResponseEvent("response.in_progress", 1, "in_progress", `[]`, `null`)},
		{"response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","status":"in_progress","role":"assistant","content":[]},"sequence_number":2}`},
		{"response.content_part.added", `{"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]},"sequence_number":3}`},
	} {
		if _, err := stream.ParseEvent(input.event, []byte(input.data)); err != nil {
			t.Fatal(err)
		}
	}
	return stream
}

func responseStreamResponseEvent(event string, sequence int, status, output, usage string) string {
	response := map[string]any{
		"id": "resp_1", "object": "response", "status": status, "model": "provider-private-model",
		"error": nil, "incomplete_details": nil,
	}
	response["output"] = json.RawMessage(output)
	response["usage"] = json.RawMessage(usage)
	envelope := map[string]any{"type": event, "sequence_number": sequence, "response": response}
	body, err := json.Marshal(envelope)
	if err != nil {
		panic(err)
	}
	return string(body)
}
