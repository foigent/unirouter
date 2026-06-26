package tokenutil

import (
	"math"
	"strings"
)

// StreamTokenCounter tracks token counts during SSE streaming.
// It separates upstream-provided usage from delta-based estimation,
// and provides a checkpoint-based balance check trigger.
type StreamTokenCounter struct {
	totalOutputTokens  int
	totalInputTokens   int
	estimatedInput     int
	checkpointTokens   int
	upstreamUsageSet   bool
}

// NewStreamTokenCounter creates a new counter with default settings.
func NewStreamTokenCounter() *StreamTokenCounter {
	return &StreamTokenCounter{}
}

// SetUpstreamUsage records the token counts provided by the upstream API.
func (c *StreamTokenCounter) SetUpstreamUsage(promptTokens, completionTokens int) {
	c.totalInputTokens = promptTokens
	c.totalOutputTokens = completionTokens
	c.upstreamUsageSet = true
}

// SetEstimatedInputTokens sets the fallback input token estimate (from request messages).
func (c *StreamTokenCounter) SetEstimatedInputTokens(tokens int) {
	c.estimatedInput = tokens
}

// AddOutputDelta estimates tokens from a content delta and accumulates them.
func (c *StreamTokenCounter) AddOutputDelta(content string) {
	c.totalOutputTokens += EstimateTokens(content)
}

// ShouldCheckBalance returns true if 100+ new output tokens have accumulated
// since the last checkpoint, and resets the checkpoint.
func (c *StreamTokenCounter) ShouldCheckBalance() bool {
	if c.totalOutputTokens-c.checkpointTokens >= 100 {
		c.checkpointTokens = c.totalOutputTokens
		return true
	}
	return false
}

// OutputTokens returns the total output tokens (upstream or estimated).
func (c *StreamTokenCounter) OutputTokens() int {
	return c.totalOutputTokens
}

// InputTokens returns the input token count: upstream usage if available,
// otherwise the estimated count from request messages.
func (c *StreamTokenCounter) InputTokens() int {
	if c.upstreamUsageSet {
		return c.totalInputTokens
	}
	return c.estimatedInput
}

// UsedUpstreamUsage reports whether upstream provided actual token counts.
func (c *StreamTokenCounter) UsedUpstreamUsage() bool {
	return c.upstreamUsageSet
}

// EstimateTokens provides a rough token count estimate for a text string.
// Uses character-based estimation with different ratios for CJK vs Latin text.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	cjkCount := 0
	latinCount := 0

	for _, r := range text {
		if isCJK(r) {
			cjkCount++
		} else {
			latinCount++
		}
	}

	// CJK: ~1.5 chars per token
	// Latin: ~4 chars per token
	cjkTokens := float64(cjkCount) / 1.5
	latinTokens := float64(latinCount) / 4.0

	return int(math.Ceil(cjkTokens + latinTokens))
}

// EstimateRequestTokens estimates tokens for a chat completion request.
func EstimateRequestTokens(messages []struct{ Role, Content string }) int {
	total := 0
	for _, m := range messages {
		total += EstimateTokens(m.Content) + 4 // overhead per message
	}
	total += 2 // reply priming
	return total
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x3000 && r <= 0x303F) || // CJK Symbols
		(r >= 0xFF00 && r <= 0xFFEF) // Fullwidth Forms
}

// EstimateMessagesFromJSON estimates tokens from a JSON string of messages.
// This is a simplified version for when you don't have the parsed struct.
func EstimateMessagesFromJSON(jsonStr string) int {
	// Rough estimate: count "content" values and estimate their tokens
	total := 0
	segments := strings.Split(jsonStr, `"content"`)
	for _, seg := range segments[1:] {
		// Find the string value after "content": "
		idx := strings.Index(seg, `"`)
		if idx < 0 {
			continue
		}
		end := strings.Index(seg[idx+1:], `"`)
		if end < 0 {
			continue
		}
		content := seg[idx+1 : idx+1+end]
		total += EstimateTokens(content) + 4
	}
	if total == 0 {
		total = 10 // minimum estimate
	}
	return total
}
