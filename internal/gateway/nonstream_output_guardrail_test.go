package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/models"
)

func TestMiniMaxNormalizedResponseMeetsStrictChatDeliveryContract(t *testing.T) {
	response, err := adapters.NewOpenAICompatible().ParseResponse(context.Background(), adapters.ResponseInput{
		Endpoint:   adapters.EndpointChatCompletions,
		StatusCode: http.StatusOK,
		Channel:    models.Channel{Provider: "minimax", AdapterType: adapters.TypeOpenAICompatible},
		Body:       []byte(`{"id":"chatcmpl-minimax","object":"chat.completion","created":1780152150,"model":"MiniMax-M3","choices":[{"finish_reason":"stop","index":0,"message":{"content":"safe answer","role":"assistant","name":"MiniMax AI","audio_content":""}}],"usage":{"total_tokens":9,"total_characters":0,"prompt_tokens":5,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":0}},"input_sensitive":false,"output_sensitive":false,"input_sensitive_type":0,"output_sensitive_type":0,"output_sensitive_int":0,"base_resp":{"status_code":0,"status_msg":"success"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]interface{}
	if err := decodeJSONUseNumber(response.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	inspection := inspectNonStreamOutputTexts(envelope)
	if !inspection.complete() {
		t.Fatalf("documented safe MiniMax response did not meet strict delivery contract: %v body=%s", inspection.incompletePaths, response.Body)
	}
	if len(inspection.texts) != 1 || inspection.texts[0].Content != "safe answer" {
		t.Fatalf("MiniMax model output was not preserved exactly: %+v", inspection.texts)
	}
}

func TestDeepSeekToolResponseMeetsStrictChatDeliveryContract(t *testing.T) {
	response, err := adapters.NewOpenAICompatible().ParseResponse(context.Background(), adapters.ResponseInput{
		Endpoint:          adapters.EndpointChatCompletions,
		StatusCode:        http.StatusOK,
		Channel:           models.Channel{Provider: "deepseek", AdapterType: adapters.TypeOpenAICompatible},
		CacheUsageProfile: "cache-usage.deepseek-v4.v1",
		Body:              []byte(`{"id":"chatcmpl-deepseek","object":"chat.completion","created":1787504000,"model":"deepseek-v4-flash","system_fingerprint":"fp","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Shanghai\"}"}}]},"logprobs":null,"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":297,"completion_tokens":38,"total_tokens":335,"prompt_cache_hit_tokens":256,"prompt_cache_miss_tokens":41,"prompt_tokens_details":{"cached_tokens":256}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Usage.CacheReadTokens != 256 || response.Usage.UncachedPromptTokens != 41 {
		t.Fatalf("DeepSeek cache usage was not retained: %+v", response.Usage)
	}
	var envelope map[string]interface{}
	if err := decodeJSONUseNumber(response.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	inspection := inspectNonStreamOutputTexts(envelope)
	if !inspection.complete() {
		t.Fatalf("documented DeepSeek tool response did not meet strict delivery contract: %v body=%s", inspection.incompletePaths, response.Body)
	}
}

func TestNonStreamOutputInspectionCollectsModeledMessageTextValues(t *testing.T) {
	response := map[string]interface{}{
		"choices": []interface{}{
			map[string]interface{}{"message": map[string]interface{}{
				"content":           "content-string",
				"refusal":           "refusal-string",
				"reasoning_content": "reasoning-content-string",
				"reasoning":         "reasoning-string",
			}},
			map[string]interface{}{"message": map[string]interface{}{
				"content":           []interface{}{map[string]interface{}{"type": "text", "text": "content-part"}},
				"refusal":           []interface{}{map[string]interface{}{"type": "output_text", "text": "refusal-part"}},
				"reasoning_content": []interface{}{map[string]interface{}{"type": "text", "text": "reasoning-content-part"}},
				"reasoning":         []interface{}{map[string]interface{}{"type": "output_text", "text": "reasoning-part"}},
			}},
		},
	}

	inspection := inspectNonStreamOutputTexts(response)
	if !inspection.complete() {
		t.Fatalf("supported modeled text fields were marked incomplete: %v", inspection.incompletePaths)
	}
	wantPaths := []string{
		"choices[0].message.content",
		"choices[0].message.refusal",
		"choices[0].message.reasoning_content",
		"choices[0].message.reasoning",
		"choices[1].message.content[0].text",
		"choices[1].message.refusal[0].text",
		"choices[1].message.reasoning_content[0].text",
		"choices[1].message.reasoning[0].text",
	}
	wantContents := []string{
		"content-string",
		"refusal-string",
		"reasoning-content-string",
		"reasoning-string",
		"content-part",
		"refusal-part",
		"reasoning-content-part",
		"reasoning-part",
	}
	if len(inspection.texts) != len(wantPaths) || len(inspection.refs) != len(wantPaths) {
		t.Fatalf("modeled text collection lost values: texts=%+v refs=%+v", inspection.texts, inspection.refs)
	}
	transformed := make([]guardrail.Text, len(inspection.texts))
	for index, item := range inspection.texts {
		if item.Path != wantPaths[index] || item.Content != wantContents[index] {
			t.Fatalf("unexpected text %d: got=%+v want_path=%q want_content=%q", index, item, wantPaths[index], wantContents[index])
		}
		transformed[index] = guardrail.Text{Path: item.Path, Content: "redacted-" + item.Content}
	}
	if err := applyOutputGuardrailTransform(guardrail.Result{Transformed: true, Texts: transformed}, inspection.refs); err != nil {
		t.Fatal(err)
	}

	firstMessage := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	secondMessage := response["choices"].([]interface{})[1].(map[string]interface{})["message"].(map[string]interface{})
	for index, key := range []string{"content", "refusal", "reasoning_content", "reasoning"} {
		if got := firstMessage[key]; got != "redacted-"+wantContents[index] {
			t.Fatalf("string field %s was not transformed through its exact ref: %#v", key, got)
		}
		part := secondMessage[key].([]interface{})[0].(map[string]interface{})
		if got := part["text"]; got != "redacted-"+wantContents[index+4] {
			t.Fatalf("text part %s was not transformed through its exact ref: %#v", key, got)
		}
	}
}

func TestKimiPreservedThinkingProfileWithholdsReasoningOrToolTranscriptRewrite(t *testing.T) {
	response := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{
			"content":           "safe",
			"reasoning_content": "private reasoning",
			"tool_calls": []interface{}{map[string]interface{}{
				"id": "call_1", "type": "function",
				"function": map[string]interface{}{"name": "lookup", "arguments": `{"id":1}`},
			}},
		}}},
	}
	inspection := inspectNonStreamOutputTexts(response)
	if !inspection.complete() {
		t.Fatalf("fixture incomplete: %v", inspection.incompletePaths)
	}
	transformed := append([]guardrail.Text(nil), inspection.texts...)
	for index := range transformed {
		if strings.Contains(transformed[index].Path, ".reasoning_content") {
			transformed[index].Content = "[REDACTED]"
		}
	}
	result := guardrail.Result{Transformed: true, Texts: transformed}
	k3 := &RouteResult{ReasoningProfile: "reasoning.kimi-k3-effort.v1"}
	if err := applyProfileAwareOutputGuardrailTransform(k3, result, inspection.refs); err == nil ||
		!strings.Contains(err.Error(), "preserved reasoning") {
		t.Fatalf("K3 transcript rewrite was not withheld: %v", err)
	}
	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	if message["reasoning_content"] != "private reasoning" {
		t.Fatalf("protected transcript changed before withholding: %#v", message)
	}

	generic := &RouteResult{ReasoningProfile: "reasoning.unverified.v1"}
	if err := applyProfileAwareOutputGuardrailTransform(generic, result, inspection.refs); err != nil {
		t.Fatalf("generic redaction behavior changed: %v", err)
	}
	if message["reasoning_content"] != "[REDACTED]" {
		t.Fatalf("generic redaction was not applied: %#v", message)
	}
}

func TestNonStreamOutputInspectionAcceptsBoundedToolCallIndex(t *testing.T) {
	response := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{
			"content": nil,
			"role":    "assistant",
			"tool_calls": []interface{}{map[string]interface{}{
				"index": json.Number("0"), "id": "call_1", "type": "function",
				"function": map[string]interface{}{"name": "lookup", "arguments": `{"id":1}`},
			}},
		}}},
	}

	inspection := inspectNonStreamOutputTexts(response)
	if !inspection.complete() {
		t.Fatalf("bounded non-stream tool index was rejected: %v", inspection.incompletePaths)
	}
	if len(inspection.texts) != 4 {
		t.Fatalf("tool call text fields were not inspected: %+v", inspection.texts)
	}
}

func TestNonStreamOutputInspectionRejectsInvalidToolCallIndex(t *testing.T) {
	tests := []struct {
		name  string
		index interface{}
	}{
		{name: "string", index: "0"},
		{name: "fraction", index: json.Number("0.5")},
		{name: "negative", index: json.Number("-1")},
		{name: "too large", index: json.Number("1000001")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := map[string]interface{}{
				"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{
					"tool_calls": []interface{}{map[string]interface{}{
						"index": test.index, "id": "call_1", "type": "function",
						"function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
					}},
				}}},
			}

			inspection := inspectNonStreamOutputTexts(response)
			if inspection.complete() || !reflect.DeepEqual(inspection.incompletePaths, []string{"choices[0].message.tool_calls[0].index"}) {
				t.Fatalf("invalid tool index was accepted: %+v", inspection)
			}
		})
	}
}

func TestKimiPreservedThinkingProfileRejectsStreamTranscriptTransformation(t *testing.T) {
	k3 := &RouteResult{ReasoningProfile: "reasoning.kimi-k3-effort.v1"}
	if !profileStreamGuardrailTransformUnsafe(k3, guardrail.Result{
		Transformed: true,
		Texts:       []guardrail.Text{{Path: "choices[0].reasoning_content", Content: "[REDACTED]"}},
	}) {
		t.Fatal("K3 stream reasoning rewrite was not marked unsafe")
	}
	if profileStreamGuardrailTransformUnsafe(k3, guardrail.Result{
		Transformed: true,
		Texts:       []guardrail.Text{{Path: "choices[0].content", Content: "[REDACTED]"}},
	}) {
		t.Fatal("ordinary content redaction was incorrectly treated as transcript rewrite")
	}
}

func TestNonStreamOutputInspectionMarksUnsupportedModeledTextValuesIncomplete(t *testing.T) {
	for _, key := range []string{"content", "refusal", "reasoning_content", "reasoning"} {
		t.Run(key+" scalar", func(t *testing.T) {
			inspection := inspectNonStreamOutputTexts(map[string]interface{}{
				"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{key: float64(7)}}},
			})
			want := "choices[0].message." + key
			if inspection.complete() || !reflect.DeepEqual(inspection.incompletePaths, []string{want}) {
				t.Fatalf("unsupported %s shape was treated as complete: %v", key, inspection.incompletePaths)
			}
		})
	}

	tests := []struct {
		name      string
		value     interface{}
		wantPaths []string
		wantTexts int
	}{
		{name: "non object part", value: []interface{}{"hidden"}, wantPaths: []string{"choices[0].message.reasoning[0]"}},
		{name: "missing text", value: []interface{}{map[string]interface{}{"type": "text"}}, wantPaths: []string{"choices[0].message.reasoning[0].text"}},
		{name: "non string text", value: []interface{}{map[string]interface{}{"type": "text", "text": float64(7)}}, wantPaths: []string{"choices[0].message.reasoning[0].text"}},
		{name: "unsupported part type", value: []interface{}{map[string]interface{}{"type": "image", "text": "hidden"}}, wantPaths: []string{"choices[0].message.reasoning[0].type"}, wantTexts: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inspection := inspectNonStreamOutputTexts(map[string]interface{}{
				"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"reasoning": test.value}}},
			})
			if inspection.complete() || !reflect.DeepEqual(inspection.incompletePaths, test.wantPaths) {
				t.Fatalf("unsupported text parts were treated as complete: got=%v want=%v", inspection.incompletePaths, test.wantPaths)
			}
			if len(inspection.texts) != test.wantTexts || len(inspection.refs) != test.wantTexts {
				t.Fatalf("unexpected collected subset: texts=%+v refs=%+v", inspection.texts, inspection.refs)
			}
		})
	}
}

func TestNonStreamOutputInspectionAllowsAbsentAndNullModeledTextValues(t *testing.T) {
	inspection := inspectNonStreamOutputTexts(map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{
			"content": nil, "refusal": nil, "reasoning_content": nil, "reasoning": nil,
		}}},
	})
	if !inspection.complete() || len(inspection.texts) != 0 || len(inspection.refs) != 0 {
		t.Fatalf("absent text values should remain compatible: %+v", inspection)
	}
}

func TestEvaluateNonStreamOutputGuardrailPropagatesCollectorCompleteness(t *testing.T) {
	service := newP6F10GuardrailService(t, guardrail.Policy{
		ID: 41, Name: "redact modeled output", Version: 1, Stage: guardrail.StageNonStreamOutput,
		DetectorRef: guardrail.DetectorPII, Action: guardrail.ActionRedact,
		Mode: guardrail.ModeEnforce, FailureMode: guardrail.FailureDegradedAllow,
		Config: json.RawMessage(`{"entities":["email"]}`),
	})
	pinned := service.Pin(guardrail.Subject{OrgID: 1, APIKeyID: 2, Model: "gpt-4o", Endpoint: "chat"})

	supported := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{
			"content": "safe", "reasoning_content": "also safe",
		}}},
	}
	result, refs := evaluateNonStreamOutputGuardrail(t.Context(), pinned, supported)
	if result.Decision != guardrail.DecisionAllow || result.Degraded || len(refs) != 2 {
		t.Fatalf("supported output changed policy behavior: result=%+v refs=%+v", result, refs)
	}

	unsupported := map[string]interface{}{
		"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{
			"content": "safe", "reasoning_content": map[string]interface{}{"text": "alice@example.com"},
		}}},
	}
	result, refs = evaluateNonStreamOutputGuardrail(t.Context(), pinned, unsupported)
	if result.Decision != guardrail.DecisionBlock || !result.Degraded || result.Transformed || len(refs) != 1 ||
		len(result.Receipts) != 1 || result.Receipts[0].Outcome != guardrail.OutcomeIndeterminate {
		t.Fatalf("collector completeness did not reach policy evaluation: result=%+v refs=%+v", result, refs)
	}
}

func TestDoubaoEncryptedReasoningEvidenceIsExactProfileBoundAndOpaque(t *testing.T) {
	response := map[string]interface{}{
		"id":    "chatcmpl-doubao",
		"model": "doubao-seed-2-1-pro-260628",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "stop",
			"message": map[string]interface{}{
				"role": "assistant", "content": "safe answer",
				"reasoning_content": "reasoning summary",
				"encrypted_content": "opaque-ciphertext",
			},
		}},
	}

	generic := inspectNonStreamOutputTexts(response)
	require.False(t, generic.complete(), "generic Chat must not inherit Doubao response extensions")

	inspection := inspectNonStreamOutputTextsForProtocol(response, "protocol.doubao-seed-2.1-pro-openai-chat.v1")
	require.True(t, inspection.complete(), inspection.incompletePaths)
	require.Len(t, inspection.texts, 2)
	require.Len(t, inspection.refs, 2)
	for _, item := range inspection.texts {
		require.NotContains(t, item.Path, "encrypted_content")
	}
	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	require.Equal(t, "opaque-ciphertext", message["encrypted_content"])

	message["encrypted_content"] = ""
	inspection = inspectNonStreamOutputTextsForProtocol(response, "protocol.doubao-seed-2.1-pro-openai-chat.v1")
	require.False(t, inspection.complete())
	message["encrypted_content"] = map[string]interface{}{"ciphertext": "hidden"}
	inspection = inspectNonStreamOutputTextsForProtocol(response, "protocol.doubao-seed-2.1-pro-openai-chat.v1")
	require.False(t, inspection.complete())

	message["encrypted_content"] = "opaque-ciphertext"
	message["tool_calls"] = []interface{}{map[string]interface{}{
		"index": json.Number("0"), "id": "call_1", "type": "function",
		"function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
	}}
	inspection = inspectNonStreamOutputTextsForProtocol(response, "protocol.doubao-seed-2.1-pro-openai-chat.v1")
	require.False(t, inspection.complete(), "tool response must not inherit generic parser support")
	delete(message, "tool_calls")
	message["function_call"] = map[string]interface{}{"name": "lookup", "arguments": `{}`}
	inspection = inspectNonStreamOutputTextsForProtocol(response, "protocol.doubao-seed-2.1-pro-openai-chat.v1")
	require.False(t, inspection.complete(), "legacy function response must remain deferred")
}

func TestDoubaoTurboEncryptedReasoningEvidenceIsExactProfileBoundAndOpaque(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-doubao-turbo", "model": "doubao-seed-2-1-turbo-260628",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "stop",
			"message": map[string]interface{}{
				"role": "assistant", "content": "safe answer",
				"reasoning_content": "reasoning summary", "encrypted_content": "opaque-ciphertext",
			},
		}},
	}
	require.False(t, inspectNonStreamOutputTexts(response).complete(), "generic Chat must not inherit Doubao response extensions")
	inspection := inspectNonStreamOutputTextsForProtocol(response, "protocol.doubao-seed-2.1-turbo-openai-chat.v1")
	require.True(t, inspection.complete(), inspection.incompletePaths)
	require.Len(t, inspection.texts, 2)
	for _, item := range inspection.texts {
		require.NotContains(t, item.Path, "encrypted_content")
	}
	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["tool_calls"] = []interface{}{map[string]interface{}{
		"index": json.Number("0"), "id": "call_1", "type": "function",
		"function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
	}}
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, "protocol.doubao-seed-2.1-turbo-openai-chat.v1").complete())
}

func TestDoubaoSeedEvolvingCoreAcceptsOnlyModeledPlainTextResponse(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-doubao-evolving", "model": "doubao-seed-evolving",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "stop",
			"message": map[string]interface{}{"role": "assistant", "content": "safe answer"},
		}},
	}
	protocol := "protocol.doubao-seed-evolving-openai-chat.v1"
	inspection := inspectNonStreamOutputTextsForProtocol(response, protocol)
	require.True(t, inspection.complete(), inspection.incompletePaths)
	require.Equal(t, []guardrail.Text{{Path: "choices[0].message.content", Content: "safe answer"}}, inspection.texts)

	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["tool_calls"] = []interface{}{map[string]interface{}{
		"index": json.Number("0"), "id": "call_1", "type": "function",
		"function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
	}}
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, protocol).complete(), "tool responses remain deferred")
	delete(message, "tool_calls")
	message["encrypted_content"] = "opaque-ciphertext"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, protocol).complete(), "opaque reasoning evidence remains deferred")
	delete(message, "encrypted_content")
	message["reasoning_content"] = "private reasoning"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, protocol).complete(), "reasoning text remains deferred")
	delete(message, "reasoning_content")
	response["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] = "tool_calls"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, protocol).complete(), "tool finish reason remains deferred")
}

func TestDoubaoSeedEvolvingReasoningResponseEvidenceIsExactAndOpaque(t *testing.T) {
	response := map[string]interface{}{
		"id": "chatcmpl-doubao-evolving-reasoning", "model": "doubao-seed-evolving",
		"choices": []interface{}{map[string]interface{}{
			"index": json.Number("0"), "finish_reason": "stop",
			"message": map[string]interface{}{
				"role": "assistant", "content": "safe answer",
				"reasoning_content": "reasoning summary", "encrypted_content": "opaque-ciphertext",
			},
		}},
	}
	protocol := "protocol.doubao-seed-evolving-openai-chat.v2"
	require.False(t, inspectNonStreamOutputTexts(response).complete(), "generic Chat must not inherit Evolving response extensions")
	inspection := inspectNonStreamOutputTextsForProtocol(response, protocol)
	require.True(t, inspection.complete(), inspection.incompletePaths)
	require.Equal(t, []guardrail.Text{
		{Path: "choices[0].message.content", Content: "safe answer"},
		{Path: "choices[0].message.reasoning_content", Content: "reasoning summary"},
	}, inspection.texts)
	for _, item := range inspection.texts {
		require.NotContains(t, item.Path, "encrypted_content")
	}

	message := response["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	message["encrypted_content"] = ""
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, protocol).complete(), "empty opaque evidence is invalid")
	message["encrypted_content"] = map[string]interface{}{"ciphertext": "opaque"}
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, protocol).complete(), "opaque evidence must retain the documented string wire type")
	message["encrypted_content"] = "opaque-ciphertext"
	message["reasoning"] = "unmodeled reasoning alias"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, protocol).complete(), "unmodeled reasoning alias remains rejected")
	delete(message, "reasoning")
	message["tool_calls"] = []interface{}{map[string]interface{}{
		"index": json.Number("0"), "id": "call_1", "type": "function",
		"function": map[string]interface{}{"name": "lookup", "arguments": `{}`},
	}}
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, protocol).complete(), "tool response remains deferred")
	delete(message, "tool_calls")
	response["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] = "tool_calls"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, protocol).complete(), "tool finish reason remains deferred")

	coreProtocol := "protocol.doubao-seed-evolving-openai-chat.v1"
	response["choices"].([]interface{})[0].(map[string]interface{})["finish_reason"] = "stop"
	require.False(t, inspectNonStreamOutputTextsForProtocol(response, coreProtocol).complete(), "historical core must not inherit reasoning evidence")
}
