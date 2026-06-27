package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// --- resolveModelMapping tests ---

func TestResolveModelMapping_EmptyMapping(t *testing.T) {
	result := resolveModelMapping("gpt-4o", nil)
	if result != "gpt-4o" {
		t.Fatalf("expected gpt-4o, got %s", result)
	}
}

func TestResolveModelMapping_EmptyBytes(t *testing.T) {
	result := resolveModelMapping("gpt-4o", json.RawMessage{})
	if result != "gpt-4o" {
		t.Fatalf("expected gpt-4o, got %s", result)
	}
}

func TestResolveModelMapping_EmptyObject(t *testing.T) {
	result := resolveModelMapping("gpt-4o", json.RawMessage(`{}`))
	if result != "gpt-4o" {
		t.Fatalf("expected gpt-4o, got %s", result)
	}
}

func TestResolveModelMapping_NullJSON(t *testing.T) {
	result := resolveModelMapping("gpt-4o", json.RawMessage(`null`))
	if result != "gpt-4o" {
		t.Fatalf("expected gpt-4o, got %s", result)
	}
}

func TestResolveModelMapping_Found(t *testing.T) {
	mapping := json.RawMessage(`{"gpt-4": "gpt-4-0613", "gpt-3.5": "gpt-3.5-turbo"}`)
	result := resolveModelMapping("gpt-4", mapping)
	if result != "gpt-4-0613" {
		t.Fatalf("expected gpt-4-0613, got %s", result)
	}
}

func TestResolveModelMapping_NotFound(t *testing.T) {
	mapping := json.RawMessage(`{"gpt-4": "gpt-4-0613"}`)
	result := resolveModelMapping("claude-3", mapping)
	if result != "claude-3" {
		t.Fatalf("expected claude-3 (passthrough), got %s", result)
	}
}

func TestResolveModelMapping_InvalidJSON(t *testing.T) {
	result := resolveModelMapping("gpt-4o", json.RawMessage(`not json`))
	if result != "gpt-4o" {
		t.Fatalf("invalid JSON should passthrough, got %s", result)
	}
}

func TestResolveModelMapping_EmptyModelName(t *testing.T) {
	mapping := json.RawMessage(`{"gpt-4": "gpt-4-0613"}`)
	result := resolveModelMapping("", mapping)
	if result != "" {
		t.Fatalf("empty model should passthrough, got %s", result)
	}
}

// --- estimateTokens tests ---

func TestEstimateTokens_EmptyMessages(t *testing.T) {
	result := estimateTokens(nil, nil)
	// 0 input + 2 reply priming + 1000 default output = 1002
	if result != 1002 {
		t.Fatalf("expected 1002, got %d", result)
	}
}

func TestEstimateTokens_WithMaxTokens(t *testing.T) {
	maxTokens := 500
	msgs := []Message{{Role: "user", Content: "Hello"}}
	result := estimateTokens(msgs, &maxTokens)
	// "Hello" ≈ 2 tokens + 4 overhead + 2 reply priming = 8 input + 500 output = 508
	if result != 508 {
		t.Fatalf("expected 508, got %d", result)
	}
}

func TestEstimateTokens_CJKContent(t *testing.T) {
	maxTokens := 100
	msgs := []Message{{Role: "user", Content: "你好世界"}} // 4 CJK chars
	result := estimateTokens(msgs, &maxTokens)
	// CJK: 4/1.5 ≈ 2.67 → ceil = 3, + 4 overhead + 2 priming = 9 input + 100 output = 109
	if result <= 100 {
		t.Fatalf("CJK should produce more than 100 tokens, got %d", result)
	}
}

func TestEstimateTokens_LatinContent(t *testing.T) {
	maxTokens := 100
	msgs := []Message{{Role: "user", Content: "Hello world"}} // 11 chars (incl space)
	result := estimateTokens(msgs, &maxTokens)
	// Latin: 11/4 ≈ 2.75 → ceil = 3, + 4 overhead + 2 priming = 9 input + 100 output = 109
	if result <= 100 {
		t.Fatalf("should be > 100, got %d", result)
	}
}

func TestEstimateTokens_MultipleMessages(t *testing.T) {
	maxTokens := 100
	msgs := []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
	}
	result := estimateTokens(msgs, &maxTokens)
	// Should be higher than single message
	if result <= 100 {
		t.Fatalf("multiple messages should produce more tokens, got %d", result)
	}
}

func TestEstimateTokens_DefaultMaxTokens(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "Hi"}}
	result := estimateTokens(msgs, nil)
	// Default output = 1000
	if result < 1000 {
		t.Fatalf("default max tokens should be 1000, got %d", result)
	}
}

// --- resolveModelMapping additional edge cases ---

func TestResolveModelMapping_SpecialCharsInKey(t *testing.T) {
	mapping := json.RawMessage(`{"gpt-4\"special\"": "gpt-4-0613"}`)
	result := resolveModelMapping(`gpt-4"special"`, mapping)
	if result != "gpt-4-0613" {
		t.Fatalf("special chars in key should work, got %s", result)
	}
}

func TestResolveModelMapping_VeryLongModelName(t *testing.T) {
	longName := ""
	for i := 0; i < 500; i++ {
		longName += "a"
	}
	mapping := json.RawMessage(`{"` + longName + `": "gpt-4"}`)
	result := resolveModelMapping(longName, mapping)
	if result != "gpt-4" {
		t.Fatalf("long model name mapping should work, got %s", result)
	}
}

func TestResolveModelMapping_MappedToEmpty(t *testing.T) {
	mapping := json.RawMessage(`{"gpt-4": ""}`)
	result := resolveModelMapping("gpt-4", mapping)
	if result != "" {
		t.Fatalf("mapped to empty should return empty, got %s", result)
	}
}

func TestResolveModelMapping_SingleKeyNoMatch(t *testing.T) {
	mapping := json.RawMessage(`{"gpt-4": "gpt-4-0613"}`)
	result := resolveModelMapping("gpt-4o", mapping)
	if result != "gpt-4o" {
		t.Fatalf("no match should passthrough, got %s", result)
	}
}

// --- estimateTokens additional boundary tests ---

func TestEstimateTokens_ZeroMaxTokens(t *testing.T) {
	maxTokens := 0
	msgs := []Message{{Role: "user", Content: "Hi"}}
	result := estimateTokens(msgs, &maxTokens)
	// input: ~2 + 4 overhead + 2 priming = 8 tokens. Output: 0.
	if result < 0 || result > 20 {
		t.Fatalf("zero maxTokens should result in low estimate, got %d", result)
	}
}

func TestEstimateTokens_VeryLargeMaxTokens(t *testing.T) {
	largeMax := 1_000_000
	msgs := []Message{{Role: "user", Content: "Hi"}}
	result := estimateTokens(msgs, &largeMax)
	if result < 1_000_000 {
		t.Fatalf("large maxTokens should produce large estimate, got %d", result)
	}
}

func TestEstimateTokens_EmptyContent(t *testing.T) {
	maxTokens := 100
	msgs := []Message{{Role: "user", Content: ""}}
	result := estimateTokens(msgs, &maxTokens)
	// 0 chars + 4 overhead + 2 priming = 6 input + 100 output = 106
	if result != 106 {
		t.Fatalf("expected 106, got %d", result)
	}
}

func TestEstimateTokens_EmptyMessagesSlice(t *testing.T) {
	result := estimateTokens([]Message{}, nil)
	// 0 input + 2 priming + 1000 default output = 1002
	if result != 1002 {
		t.Fatalf("empty messages should be 1002, got %d", result)
	}
}

func TestEstimateTokens_SystemAndAssistantRoles(t *testing.T) {
	maxTokens := 50
	msgs := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	result := estimateTokens(msgs, &maxTokens)
	// Three messages: each with overhead + content. Should be > 50.
	if result <= 50 {
		t.Fatalf("three messages should be > 50, got %d", result)
	}
}

// --- writeError tests (already in gateway_test.go, adding edge cases) ---

func TestWriteError_EmptyFields(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 400, "", "", "")
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	errObj := resp["error"].(map[string]interface{})
	if errObj["type"] != "" {
		t.Fatalf("expected empty type, got %v", errObj["type"])
	}
}

func TestWriteError_SpecialCharsInMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, 400, "test", "test.escapes", "message with \"quotes\" and \n newlines")
	var resp map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	errObj := resp["error"].(map[string]interface{})
	if errObj["message"] != "message with \"quotes\" and \n newlines" {
		t.Fatal("special chars should be preserved")
	}
}


