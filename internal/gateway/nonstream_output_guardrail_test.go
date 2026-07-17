package gateway

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/uniroute/unirouter/internal/guardrail"
)

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
