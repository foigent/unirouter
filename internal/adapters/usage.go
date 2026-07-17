package adapters

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uniroute/unirouter/internal/metering"
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

func NewMissingUsage() Usage {
	return Usage{
		Source:           metering.UsageSourceMissing,
		Authority:        metering.UsageAuthorityNone,
		Purpose:          metering.UsagePurposeProviderEvidence,
		Finality:         metering.UsageFinalityMissing,
		ContractVersion:  metering.UsageContractVersion,
		CacheUsageStatus: CacheUsageStatusNotReported,
	}
}

func NewConservativeEstimate(promptTokens, completionTokens int) (Usage, error) {
	usage, err := NewUsage(
		promptTokens,
		completionTokens,
		promptTokens+completionTokens,
		metering.UsageSourceEstimated,
	)
	if err != nil {
		return Usage{}, err
	}
	usage.Authority = metering.UsageAuthorityConservativeEstimate
	usage.Purpose = metering.UsagePurposeCustomerCharge
	usage.Finality = metering.UsageFinalityEstimated
	usage.EstimateVersion = metering.ConservativeEstimateVersionV1
	return usage, usage.ValidateContract()
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

	usage := Usage{
		PromptTokens:         promptTokens,
		UncachedPromptTokens: uncachedPromptTokens,
		CacheReadTokens:      cacheReadTokens,
		CacheCreationTokens:  cacheCreationTokens,
		CompletionTokens:     completionTokens,
		TotalTokens:          totalTokens,
		Source:               source,
		CacheUsageSchema:     cacheUsageSchema,
		CacheUsageStatus:     cacheUsageStatus,
	}
	switch metering.NormalizeUsageSource(source) {
	case metering.UsageSourceUpstream:
		usage.Authority = metering.UsageAuthorityProviderReported
		usage.Purpose = metering.UsagePurposeProviderEvidence
		usage.Finality = metering.UsageFinalityFinal
	case metering.UsageSourceCounterV2:
		usage.Authority = metering.UsageAuthorityGatewayCounter
		usage.Purpose = metering.UsagePurposeCustomerCharge
		usage.Finality = metering.UsageFinalityEstimated
		usage.EstimateVersion = metering.GatewayCounterEstimateVersionV2
	case metering.UsageSourceEstimated, metering.UsageSourceAdapterEstimated:
		usage.Authority = metering.UsageAuthorityConservativeEstimate
		usage.Purpose = metering.UsagePurposeCustomerCharge
		usage.Finality = metering.UsageFinalityEstimated
	default:
		usage.Authority = metering.UsageAuthorityNone
		usage.Purpose = metering.UsagePurposeProviderEvidence
		usage.Finality = metering.UsageFinalityMissing
	}
	usage.ContractVersion = metering.UsageContractVersion
	return usage, nil
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

func (u Usage) IsMissing() bool {
	return u.Authority == metering.UsageAuthorityNone || u.Finality == metering.UsageFinalityMissing
}

func (u Usage) ForCustomerCharge() (Usage, error) {
	if u.IsMissing() {
		return Usage{}, fmt.Errorf("missing provider usage cannot become customer-charge authority")
	}
	u.Purpose = metering.UsagePurposeCustomerCharge
	return u, u.ValidateContract()
}

func (u Usage) ValidateContract() error {
	if strings.TrimSpace(u.ContractVersion) != metering.UsageContractVersion {
		return fmt.Errorf("unsupported usage contract version %q", u.ContractVersion)
	}
	switch u.Authority {
	case metering.UsageAuthorityProviderReported:
		if u.Source != metering.UsageSourceUpstream || u.Finality != metering.UsageFinalityFinal || u.EstimateVersion != "" {
			return fmt.Errorf("provider-reported usage contract is inconsistent")
		}
	case metering.UsageAuthorityGatewayCounter:
		if u.Source != metering.UsageSourceCounterV2 || u.Finality != metering.UsageFinalityEstimated || u.EstimateVersion != metering.GatewayCounterEstimateVersionV2 {
			return fmt.Errorf("gateway-counter usage contract is inconsistent")
		}
	case metering.UsageAuthorityConservativeEstimate:
		if u.Source != metering.UsageSourceEstimated || u.Finality != metering.UsageFinalityEstimated || strings.TrimSpace(u.EstimateVersion) == "" {
			return fmt.Errorf("conservative-estimate usage contract is inconsistent")
		}
	case metering.UsageAuthorityNone:
		if u.Source != metering.UsageSourceMissing || u.Finality != metering.UsageFinalityMissing || u.Purpose != metering.UsagePurposeProviderEvidence || u.EstimateVersion != "" {
			return fmt.Errorf("missing usage contract is inconsistent")
		}
	default:
		return fmt.Errorf("unsupported usage authority %q", u.Authority)
	}
	if u.Purpose != metering.UsagePurposeProviderEvidence && u.Purpose != metering.UsagePurposeCustomerCharge {
		return fmt.Errorf("unsupported usage purpose %q", u.Purpose)
	}
	return nil
}

type openAIUsageWire struct {
	PromptTokens     *int `json:"prompt_tokens"`
	CompletionTokens *int `json:"completion_tokens"`
	TotalTokens      *int `json:"total_tokens"`
	PromptDetails    *struct {
		CachedTokens     *int `json:"cached_tokens"`
		CacheWriteTokens *int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	PromptCacheHitTokens  *int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens *int `json:"prompt_cache_miss_tokens,omitempty"`
}

type optionalUsageCounter struct {
	name  string
	value *int
}

// ParseOpenAICompatibleUsage recognizes only version-fixed, provider-owned
// fields. Generic unknown fields are deliberately ignored and never become a
// cache discount. It is shared by non-stream and final streaming usage paths.
// The boolean is true only when the endpoint's required core counters are
// explicitly present; missing and null counters remain non-authoritative.
func ParseOpenAICompatibleUsage(data []byte, endpoint Endpoint, source string) (Usage, bool, error) {
	switch endpoint {
	case EndpointChatCompletions, EndpointEmbeddings:
	default:
		return Usage{}, false, fmt.Errorf("unsupported OpenAI-compatible usage endpoint %q", endpoint)
	}

	var envelope struct {
		Usage *openAIUsageWire `json:"usage"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Usage{}, false, fmt.Errorf("decode OpenAI-compatible usage: %w", err)
	}
	if envelope.Usage == nil {
		return NewMissingUsage(), false, nil
	}
	wire := envelope.Usage
	if err := validateOpenAIUsageWire(wire); err != nil {
		return Usage{}, false, err
	}
	if wire.PromptTokens == nil || (endpoint == EndpointChatCompletions && wire.CompletionTokens == nil) {
		return NewMissingUsage(), false, nil
	}
	promptTokens := *wire.PromptTokens
	completionTokens := valueOrZero(wire.CompletionTokens)
	totalTokens := valueOrZero(wire.TotalTokens)

	// DeepSeek's official usage object reports cache hit and miss as a complete
	// prompt partition. Presence is pointer-based so an authoritative zero-hit
	// request remains distinguishable from an unreported cache contract.
	if wire.PromptCacheHitTokens != nil || wire.PromptCacheMissTokens != nil {
		if wire.PromptCacheHitTokens == nil || wire.PromptCacheMissTokens == nil {
			return Usage{}, true, fmt.Errorf("DeepSeek prompt-cache usage requires both hit and miss counters")
		}
		hit := valueOrZero(wire.PromptCacheHitTokens)
		miss := valueOrZero(wire.PromptCacheMissTokens)
		promptTotal := promptTokens
		if promptTotal == 0 {
			promptTotal = hit + miss
		}
		usage, err := NewCacheAwareUsage(
			promptTotal,
			miss,
			hit,
			0,
			completionTokens,
			totalTokens,
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
			promptTokens,
			promptTokens-cached-created,
			cached,
			created,
			completionTokens,
			totalTokens,
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
			promptTokens,
			promptTokens-cached,
			cached,
			0,
			completionTokens,
			totalTokens,
			source,
			CacheUsageSchemaOpenAIV1,
			CacheUsageStatusReadReported,
		)
		return usage, true, err
	}

	usage, err := NewUsage(
		promptTokens,
		completionTokens,
		totalTokens,
		source,
	)
	return usage, true, err
}

func validateOpenAIUsageWire(wire *openAIUsageWire) error {
	if wire == nil {
		return nil
	}
	counters := []optionalUsageCounter{
		{name: "prompt_tokens", value: wire.PromptTokens},
		{name: "completion_tokens", value: wire.CompletionTokens},
		{name: "total_tokens", value: wire.TotalTokens},
		{name: "prompt_cache_hit_tokens", value: wire.PromptCacheHitTokens},
		{name: "prompt_cache_miss_tokens", value: wire.PromptCacheMissTokens},
	}
	if wire.PromptDetails != nil {
		counters = append(counters,
			optionalUsageCounter{name: "prompt_tokens_details.cached_tokens", value: wire.PromptDetails.CachedTokens},
			optionalUsageCounter{name: "prompt_tokens_details.cache_write_tokens", value: wire.PromptDetails.CacheWriteTokens},
		)
	}
	return validateOptionalUsageCounters(counters...)
}

func validateOptionalUsageCounters(counters ...optionalUsageCounter) error {
	for _, counter := range counters {
		if counter.value == nil {
			continue
		}
		if *counter.value < 0 {
			return fmt.Errorf("%s must be nonnegative", counter.name)
		}
		if *counter.value > maxPersistedUsageTokens {
			return fmt.Errorf("%s exceeds the persisted integer limit", counter.name)
		}
	}
	return nil
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
