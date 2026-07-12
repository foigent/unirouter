package adapters

import (
	"encoding/json"
	"fmt"
)

const (
	CacheUsageSchemaOpenAIV1    = "openai.prompt_tokens_details.v1"
	CacheUsageSchemaOpenAIV2    = "openai.prompt_tokens_details.v2"
	CacheUsageSchemaDeepSeekV1  = "deepseek.prompt_cache.v1"
	CacheUsageSchemaAnthropicV1 = "anthropic.prompt_cache.v1"

	CacheUsageStatusNotReported       = "not_reported"
	CacheUsageStatusReadReported      = "cache_read_reported"
	CacheUsageStatusWriteReported     = "cache_write_reported"
	CacheUsageStatusHitMissReported   = "cache_hit_miss_reported"
	CacheUsageStatusReadWriteReported = "cache_read_write_reported"

	maxPersistedUsageTokens = 1<<31 - 1
)

// NewUsage constructs one normalized prompt-token partition. PromptTokens is
// always the inclusive input total; the three input classes must partition it
// exactly. A blank cache schema means the provider did not report cache usage,
// so every prompt token remains ordinary uncached input.
func NewUsage(promptTokens, completionTokens, totalTokens int, source string) (Usage, error) {
	return NewCacheAwareUsage(
		promptTokens,
		promptTokens,
		0,
		0,
		completionTokens,
		totalTokens,
		source,
		"",
		CacheUsageStatusNotReported,
	)
}

// NewCacheAwareUsage validates a provider-reported prompt-cache partition.
// Invalid or contradictory upstream counters are rejected instead of becoming
// billing truth.
func NewCacheAwareUsage(
	promptTokens, uncachedPromptTokens, cacheReadTokens, cacheCreationTokens,
	completionTokens, totalTokens int,
	source, cacheUsageSchema, cacheUsageStatus string,
) (Usage, error) {
	values := map[string]int{
		"prompt_tokens":          promptTokens,
		"uncached_prompt_tokens": uncachedPromptTokens,
		"cache_read_tokens":      cacheReadTokens,
		"cache_creation_tokens":  cacheCreationTokens,
		"completion_tokens":      completionTokens,
		"total_tokens":           totalTokens,
	}
	for name, value := range values {
		if value < 0 {
			return Usage{}, fmt.Errorf("%s must be nonnegative", name)
		}
		if value > maxPersistedUsageTokens {
			return Usage{}, fmt.Errorf("%s exceeds the persisted integer limit", name)
		}
	}
	partitionTotal := int64(uncachedPromptTokens) + int64(cacheReadTokens) + int64(cacheCreationTokens)
	if partitionTotal > maxPersistedUsageTokens {
		return Usage{}, fmt.Errorf("prompt token partition exceeds the persisted integer limit")
	}
	if promptTokens == 0 && partitionTotal > 0 {
		promptTokens = int(partitionTotal)
	}
	if partitionTotal != int64(promptTokens) {
		return Usage{}, fmt.Errorf(
			"prompt token partition mismatch: total=%d uncached=%d cache_read=%d cache_creation=%d",
			promptTokens,
			uncachedPromptTokens,
			cacheReadTokens,
			cacheCreationTokens,
		)
	}
	combinedTokens := int64(promptTokens) + int64(completionTokens)
	if combinedTokens > maxPersistedUsageTokens {
		return Usage{}, fmt.Errorf("prompt+completion tokens exceed the persisted integer limit")
	}
	if totalTokens == 0 && combinedTokens > 0 {
		totalTokens = int(combinedTokens)
	}
	if totalTokens > 0 && int64(totalTokens) < combinedTokens {
		return Usage{}, fmt.Errorf(
			"total_tokens=%d is smaller than prompt+completion=%d",
			totalTokens,
			combinedTokens,
		)
	}
	if cacheUsageSchema == "" {
		if cacheReadTokens != 0 || cacheCreationTokens != 0 || uncachedPromptTokens != promptTokens {
			return Usage{}, fmt.Errorf("cache token classes require an authoritative usage schema")
		}
		cacheUsageStatus = CacheUsageStatusNotReported
	} else if cacheUsageStatus == "" || cacheUsageStatus == CacheUsageStatusNotReported {
		return Usage{}, fmt.Errorf("cache usage schema requires a reported status")
	}

	return Usage{
		PromptTokens:         promptTokens,
		UncachedPromptTokens: uncachedPromptTokens,
		CacheReadTokens:      cacheReadTokens,
		CacheCreationTokens:  cacheCreationTokens,
		CompletionTokens:     completionTokens,
		TotalTokens:          totalTokens,
		Source:               source,
		CacheUsageSchema:     cacheUsageSchema,
		CacheUsageStatus:     cacheUsageStatus,
	}, nil
}

// UsageFromStreamState returns the durable normalized usage partition carried
// across provider stream events.
func UsageFromStreamState(state *StreamState, source string) (Usage, error) {
	if state == nil {
		return Usage{}, fmt.Errorf("stream state is required")
	}
	return NewCacheAwareUsage(
		state.PromptTokens,
		state.UncachedPromptTokens,
		state.CacheReadTokens,
		state.CacheCreationTokens,
		state.CompletionTokens,
		state.PromptTokens+state.CompletionTokens,
		source,
		state.CacheUsageSchema,
		state.CacheUsageStatus,
	)
}

func (u Usage) HasAuthoritativeCacheUsage() bool {
	return u.CacheUsageSchema != ""
}

func (u Usage) HasAuthoritativeCacheHit() bool {
	return u.HasAuthoritativeCacheUsage() && u.CacheReadTokens > 0
}

type openAIUsageWire struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	PromptDetails    *struct {
		CachedTokens     *int `json:"cached_tokens"`
		CacheWriteTokens *int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	PromptCacheHitTokens  *int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens *int `json:"prompt_cache_miss_tokens,omitempty"`
}

// ParseOpenAICompatibleUsage recognizes only version-fixed, provider-owned
// fields. Generic unknown fields are deliberately ignored and never become a
// cache discount. It is shared by non-stream and final streaming usage paths.
func ParseOpenAICompatibleUsage(data []byte, source string) (Usage, bool, error) {
	var envelope struct {
		Usage *openAIUsageWire `json:"usage"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Usage{}, false, fmt.Errorf("decode OpenAI-compatible usage: %w", err)
	}
	if envelope.Usage == nil {
		usage, err := NewUsage(0, 0, 0, source)
		return usage, false, err
	}
	wire := envelope.Usage

	// DeepSeek's official usage object reports cache hit and miss as a complete
	// prompt partition. Presence is pointer-based so an authoritative zero-hit
	// request remains distinguishable from an unreported cache contract.
	if wire.PromptCacheHitTokens != nil || wire.PromptCacheMissTokens != nil {
		if wire.PromptCacheHitTokens == nil || wire.PromptCacheMissTokens == nil {
			return Usage{}, true, fmt.Errorf("DeepSeek prompt-cache usage requires both hit and miss counters")
		}
		hit := valueOrZero(wire.PromptCacheHitTokens)
		miss := valueOrZero(wire.PromptCacheMissTokens)
		promptTotal := wire.PromptTokens
		if promptTotal == 0 {
			promptTotal = hit + miss
		}
		usage, err := NewCacheAwareUsage(
			promptTotal,
			miss,
			hit,
			0,
			wire.CompletionTokens,
			wire.TotalTokens,
			source,
			CacheUsageSchemaDeepSeekV1,
			CacheUsageStatusHitMissReported,
		)
		return usage, true, err
	}

	// OpenAI reports cache reads and, on the current Chat Completions contract,
	// cache writes as disjoint subsets of prompt_tokens. The v1 schema remains
	// for cached_tokens-only responses; v2 is explicit when cache_write_tokens
	// is present. No token class is inferred from a pricing table.
	if wire.PromptDetails != nil && wire.PromptDetails.CacheWriteTokens != nil {
		cached := valueOrZero(wire.PromptDetails.CachedTokens)
		created := *wire.PromptDetails.CacheWriteTokens
		status := CacheUsageStatusWriteReported
		if wire.PromptDetails.CachedTokens != nil {
			status = CacheUsageStatusReadWriteReported
		}
		usage, err := NewCacheAwareUsage(
			wire.PromptTokens,
			wire.PromptTokens-cached-created,
			cached,
			created,
			wire.CompletionTokens,
			wire.TotalTokens,
			source,
			CacheUsageSchemaOpenAIV2,
			status,
		)
		return usage, true, err
	}

	// Older OpenAI-compatible responses expose only the cached read subset.
	if wire.PromptDetails != nil && wire.PromptDetails.CachedTokens != nil {
		cached := *wire.PromptDetails.CachedTokens
		usage, err := NewCacheAwareUsage(
			wire.PromptTokens,
			wire.PromptTokens-cached,
			cached,
			0,
			wire.CompletionTokens,
			wire.TotalTokens,
			source,
			CacheUsageSchemaOpenAIV1,
			CacheUsageStatusReadReported,
		)
		return usage, true, err
	}

	usage, err := NewUsage(
		wire.PromptTokens,
		wire.CompletionTokens,
		wire.TotalTokens,
		source,
	)
	return usage, true, err
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
