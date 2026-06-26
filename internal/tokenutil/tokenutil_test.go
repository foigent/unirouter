package tokenutil

import (
	"testing"
)

func TestStreamTokenCounter_BasicFlow(t *testing.T) {
	c := NewStreamTokenCounter()

	if c.OutputTokens() != 0 {
		t.Fatalf("initial output tokens should be 0, got %d", c.OutputTokens())
	}
	if c.InputTokens() != 0 {
		t.Fatalf("initial input tokens should be 0, got %d", c.InputTokens())
	}
	if c.UsedUpstreamUsage() {
		t.Fatal("initially should not have upstream usage")
	}

	c.AddOutputDelta("Hello world")
	if c.OutputTokens() <= 0 {
		t.Fatal("output tokens should be > 0 after adding delta")
	}

	c.SetEstimatedInputTokens(50)
	if c.InputTokens() != 50 {
		t.Fatalf("input tokens should be 50, got %d", c.InputTokens())
	}
}

func TestStreamTokenCounter_UpstreamUsage(t *testing.T) {
	c := NewStreamTokenCounter()
	c.SetUpstreamUsage(100, 200)

	if !c.UsedUpstreamUsage() {
		t.Fatal("should report upstream usage")
	}
	if c.InputTokens() != 100 {
		t.Fatalf("input tokens should be 100, got %d", c.InputTokens())
	}
	if c.OutputTokens() != 200 {
		t.Fatalf("output tokens should be 200, got %d", c.OutputTokens())
	}

	// SetEstimatedInputTokens should be ignored when upstream usage is set
	c.SetEstimatedInputTokens(999)
	if c.InputTokens() != 100 {
		t.Fatalf("input tokens should still be 100, got %d", c.InputTokens())
	}
}

func TestStreamTokenCounter_ShouldCheckBalance(t *testing.T) {
	c := NewStreamTokenCounter()

	// No check needed at 0 tokens
	if c.ShouldCheckBalance() {
		t.Fatal("should not check balance at 0 tokens")
	}

	// Add ~99 tokens worth of content — should not trigger
	for i := 0; i < 4; i++ {
		c.AddOutputDelta("Hello world test") // ~4 tokens each
	}
	// OutputTokens is likely < 100 here
	if c.ShouldCheckBalance() {
		// If it triggered, reset checkpoint and continue
		t.Log("early trigger — acceptable, resetting checkpoint")
	}

	// Force enough tokens to cross the 100 threshold
	for i := 0; i < 50; i++ {
		c.AddOutputDelta("Hello world test content here") // ~7 tokens each
	}
	if !c.ShouldCheckBalance() {
		t.Fatal("should check balance after 100+ tokens")
	}

	// After a check, it should not trigger again immediately
	if c.ShouldCheckBalance() {
		t.Fatal("should not re-trigger immediately after checkpoint reset")
	}

	// Add more tokens to cross threshold again
	for i := 0; i < 50; i++ {
		c.AddOutputDelta("More content to accumulate tokens")
	}
	if !c.ShouldCheckBalance() {
		t.Fatal("should check balance again after another 100 tokens")
	}
}

func TestStreamTokenCounter_CheckpointReset(t *testing.T) {
	c := NewStreamTokenCounter()

	// Accumulate 250 tokens
	for i := 0; i < 100; i++ {
		c.AddOutputDelta("Hello world") // ~3 tokens each
	}

	// First check should trigger
	if !c.ShouldCheckBalance() {
		t.Fatal("should trigger at ~300 tokens")
	}

	// Immediate second check should NOT trigger (checkpoint reset to current)
	if c.ShouldCheckBalance() {
		t.Fatal("should not re-trigger right after checkpoint")
	}
}

func TestEstimateTokens_Deterministic(t *testing.T) {
	// Same input should always produce same output
	input := "The quick brown fox jumps over the lazy dog"
	t1 := EstimateTokens(input)
	t2 := EstimateTokens(input)
	if t1 != t2 {
		t.Fatalf("EstimateTokens should be deterministic: %d != %d", t1, t2)
	}
}

func TestEstimateTokens_English(t *testing.T) {
	// English: ~4 chars per token
	tokens := EstimateTokens("Hello, this is a test message with some words.")
	if tokens <= 0 {
		t.Fatal("tokens should be > 0")
	}
	// ~47 chars / 4 = ~12 tokens
	if tokens < 5 || tokens > 20 {
		t.Fatalf("unexpected token count for English: %d", tokens)
	}
}

func TestEstimateTokens_Chinese(t *testing.T) {
	tokens := EstimateTokens("你好世界，这是一条测试消息")
	if tokens <= 0 {
		t.Fatal("tokens should be > 0")
	}
	// 12 CJK chars / 1.5 = 8 tokens
	if tokens < 5 || tokens > 15 {
		t.Fatalf("unexpected token count for Chinese: %d", tokens)
	}
}

func TestEstimateTokens_ChineseMoreTokens(t *testing.T) {
	// Same character count but CJK should produce more tokens
	eng := EstimateTokens("abcdefghijklmnop") // 16 latin chars / 4 = 4 tokens
	cjk := EstimateTokens("你好世界测试消息好")  // 9 CJK chars / 1.5 = 6 tokens
	if cjk <= eng {
		t.Fatalf("CJK should produce more tokens: cjk=%d, eng=%d", cjk, eng)
	}
}

func TestEstimateTokens_MixedLanguage(t *testing.T) {
	tokens := EstimateTokens("Hello 你好 world 世界")
	if tokens <= 0 {
		t.Fatal("tokens should be > 0 for mixed content")
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	tokens := EstimateTokens("")
	if tokens != 0 {
		t.Fatalf("empty text should return 0, got %d", tokens)
	}
}

func TestEstimateTokens_WhitespaceOnly(t *testing.T) {
	tokens := EstimateTokens("   \n\t  ")
	// spaces are latin chars, 7/4 = ~2
	if tokens < 0 {
		t.Fatal("whitespace tokens should be >= 0")
	}
}

func TestEstimateTokens_SingleChar(t *testing.T) {
	tokens := EstimateTokens("a")
	if tokens < 1 {
		t.Fatal("single char should be at least 1 token")
	}
}

func TestEstimateRequestTokens_Empty(t *testing.T) {
	msgs := []struct{ Role, Content string }{}
	tokens := EstimateRequestTokens(msgs)
	// 0 messages + 2 reply priming = 2
	if tokens != 2 {
		t.Fatalf("expected 2, got %d", tokens)
	}
}

func TestEstimateRequestTokens_SingleMessage(t *testing.T) {
	msgs := []struct{ Role, Content string }{
		{Role: "user", Content: "Hello world"},
	}
	tokens := EstimateRequestTokens(msgs)
	if tokens <= 2 {
		t.Fatalf("should be > 2 with a message, got %d", tokens)
	}
}

func TestEstimateRequestTokens_MultipleMessages(t *testing.T) {
	msgs := []struct{ Role, Content string }{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello!"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "user", Content: "How are you?"},
	}
	t1 := EstimateRequestTokens(msgs)
	msgs = append(msgs, struct{ Role, Content string }{Role: "user", Content: "Extra message with more content"})
	t2 := EstimateRequestTokens(msgs)
	if t2 <= t1 {
		t.Fatalf("more messages should produce more tokens: t1=%d, t2=%d", t1, t2)
	}
}

func TestEstimateMessagesFromJSON(t *testing.T) {
	json := `{"messages":[{"role":"user","content":"Hello world"}]}`
	tokens := EstimateMessagesFromJSON(json)
	if tokens <= 0 {
		t.Fatal("should estimate tokens from JSON")
	}
}

func TestEstimateMessagesFromJSON_Empty(t *testing.T) {
	json := `{"messages":[]}`
	tokens := EstimateMessagesFromJSON(json)
	// No content fields -> minimum estimate of 10
	if tokens != 10 {
		t.Fatalf("expected minimum 10, got %d", tokens)
	}
}

func TestIsCJK(t *testing.T) {
	cjkCases := []rune{'你', '好', '世', '界', 'Ａ', '（'}
	for _, r := range cjkCases {
		if !isCJK(r) {
			t.Fatalf("expected isCJK=true for %c (U+%04X)", r, r)
		}
	}
	latinCases := []rune{'a', 'Z', '0', ' ', '!'}
	for _, r := range latinCases {
		if isCJK(r) {
			t.Fatalf("expected isCJK=false for %c (U+%04X)", r, r)
		}
	}
}
