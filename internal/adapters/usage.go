package adapters

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

const (
	CacheUsageSchemaOpenAIV1    = "openai.prompt_tokens_details.v1"
	CacheUsageSchemaOpenAIV2    = "openai.prompt_tokens_details.v2"
	CacheUsageSchemaDeepSeekV1  = "deepseek.prompt_cache.v1"
	CacheUsageSchemaAnthropicV1 = "anthropic.prompt_cache.v1"
	CacheUsageSchemaKimiV1      = modelprofile.CacheUsageWireMoonshotCachedTokensV1
	CacheUsageSchemaQwenV1      = modelprofile.CacheUsageWireQwenContextCacheV1

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

// NewResponseCacheUsage is the current execution's zero-token, zero-charge
// authority. Historical provider usage remains only in the encrypted cache
// proof and is never copied into the current billing counters.
func NewResponseCacheUsage() Usage {
	return Usage{
		Source:           metering.UsageSourceResponseCache,
		Authority:        metering.UsageAuthorityLocalCache,
		Purpose:          metering.UsagePurposeCustomerCharge,
		Finality:         metering.UsageFinalityFinal,
		ContractVersion:  metering.UsageContractVersionV2,
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

func (u Usage) WithMeterUsage(values []metering.MeterQuantity) (Usage, error) {
	normalized, err := metering.NormalizeMeterQuantities(values)
	if err != nil {
		return Usage{}, err
	}
	u.MeterUsage = normalized
	return u, u.ValidateContract()
}

func (u Usage) ValidateContract() error {
	normalizedMeters, err := metering.NormalizeMeterQuantities(u.MeterUsage)
	if err != nil {
		return err
	}
	if !slices.Equal(normalizedMeters, u.MeterUsage) {
		return fmt.Errorf("meter usage is not canonical")
	}
	if u.Authority == metering.UsageAuthorityLocalCache {
		if u.ContractVersion != metering.UsageContractVersionV2 ||
			u.Source != metering.UsageSourceResponseCache ||
			u.Purpose != metering.UsagePurposeCustomerCharge ||
			u.Finality != metering.UsageFinalityFinal || u.EstimateVersion != "" ||
			u.PromptTokens != 0 || u.UncachedPromptTokens != 0 ||
			u.CacheReadTokens != 0 || u.CacheCreationTokens != 0 ||
			u.CompletionTokens != 0 || u.TotalTokens != 0 {
			return fmt.Errorf("local response-cache usage contract is inconsistent")
		}
		return nil
	}
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
	PromptTokens     *int            `json:"prompt_tokens"`
	CompletionTokens *int            `json:"completion_tokens"`
	TotalTokens      *int            `json:"total_tokens"`
	CachedTokens     json.RawMessage `json:"cached_tokens,omitempty"`
	PromptDetails    *struct {
		CachedTokens             json.RawMessage `json:"cached_tokens"`
		CacheWriteTokens         *int            `json:"cache_write_tokens"`
		CacheCreationInputTokens *int            `json:"cache_creation_input_tokens"`
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
	return ParseOpenAICompatibleUsageWithCacheProfile(data, endpoint, source, "")
}

// ParseOpenAICompatibleUsageWithCacheProfile extends the generic parser only
// when an immutable execution profile names the exact provider cache wire.
// A top-level cached_tokens field is therefore billing authority for Kimi, but
// remains an ignored unknown extension for generic OpenAI-compatible routes.
func ParseOpenAICompatibleUsageWithCacheProfile(data []byte, endpoint Endpoint, source, cacheUsageProfile string) (Usage, bool, error) {
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
	if err := validateOpenAIUsageCoreWire(wire); err != nil {
		return Usage{}, false, err
	}
	if wire.PromptTokens == nil || (endpoint == EndpointChatCompletions && wire.CompletionTokens == nil) {
		return NewMissingUsage(), false, nil
	}
	promptTokens := *wire.PromptTokens
	completionTokens := valueOrZero(wire.CompletionTokens)
	totalTokens := valueOrZero(wire.TotalTokens)
	cacheContract, cacheModeled := modelprofile.ResolveCacheUsageContract(strings.TrimSpace(cacheUsageProfile))
	var promptCachedTokens int
	var promptCachedReported, promptCachedNull bool
	if cacheModeled && wire.PromptDetails != nil {
		var promptCachedErr error
		promptCachedTokens, promptCachedReported, promptCachedNull, promptCachedErr = parseOptionalUsageCounterPresence(
			wire.PromptDetails.CachedTokens, "prompt_tokens_details.cached_tokens",
		)
		if promptCachedErr != nil {
			return Usage{}, cacheModeled, promptCachedErr
		}
	}
	if cacheModeled && cacheContract.WireSchema == modelprofile.CacheUsageWireMoonshotCachedTokensV1 {
		cachedTokens, cacheReported, err := parseOptionalUsageCounter(wire.CachedTokens, "cached_tokens")
		if err != nil {
			return Usage{}, false, err
		}
		if wire.PromptCacheHitTokens != nil || wire.PromptCacheMissTokens != nil ||
			(wire.PromptDetails != nil && (wire.PromptDetails.CacheWriteTokens != nil || wire.PromptDetails.CacheCreationInputTokens != nil)) {
			return Usage{}, true, fmt.Errorf("Kimi cache profile requires top-level cached_tokens instead of another cache schema")
		}
		if promptCachedReported {
			if !cacheReported {
				return Usage{}, true, fmt.Errorf("Kimi cache profile requires top-level cached_tokens instead of another cache schema")
			}
			if promptCachedTokens != cachedTokens {
				return Usage{}, true, fmt.Errorf("Kimi cached_tokens mirror contradicts the provider-owned top-level counter")
			}
		}
		if cacheReported {
			usage, usageErr := NewCacheAwareUsage(
				promptTokens,
				promptTokens-cachedTokens,
				cachedTokens,
				0,
				completionTokens,
				totalTokens,
				source,
				CacheUsageSchemaKimiV1,
				CacheUsageStatusReadReported,
			)
			return usage, true, usageErr
		}
	}

	// DeepSeek's official usage object reports cache hit and miss as a complete
	// prompt partition. These counters become billing truth only when the sealed
	// execution profile names the exact DeepSeek wire schema.
	if cacheModeled && cacheContract.WireSchema == modelprofile.CacheUsageWireDeepSeekHitMissV1 {
		if err := validateOptionalUsageCounters(
			optionalUsageCounter{name: "prompt_cache_hit_tokens", value: wire.PromptCacheHitTokens},
			optionalUsageCounter{name: "prompt_cache_miss_tokens", value: wire.PromptCacheMissTokens},
		); err != nil {
			return Usage{}, true, err
		}
		if (len(wire.CachedTokens) > 0 && !isJSONNullUsage(wire.CachedTokens)) ||
			(wire.PromptDetails != nil && (wire.PromptDetails.CacheWriteTokens != nil || wire.PromptDetails.CacheCreationInputTokens != nil)) {
			return Usage{}, true, fmt.Errorf("DeepSeek cache profile requires prompt_cache_hit_tokens and prompt_cache_miss_tokens instead of another cache schema")
		}
		cacheReported := wire.PromptCacheHitTokens != nil || wire.PromptCacheMissTokens != nil
		if cacheReported && (wire.PromptCacheHitTokens == nil || wire.PromptCacheMissTokens == nil) {
			return Usage{}, true, fmt.Errorf("DeepSeek prompt-cache usage requires both hit and miss counters")
		}
		if promptCachedReported {
			if !cacheReported {
				return Usage{}, true, fmt.Errorf("DeepSeek cache profile requires prompt_cache_hit_tokens and prompt_cache_miss_tokens instead of another cache schema")
			}
			if promptCachedTokens != valueOrZero(wire.PromptCacheHitTokens) {
				return Usage{}, true, fmt.Errorf("DeepSeek cached_tokens mirror contradicts the provider-owned hit counter")
			}
		}
		if cacheReported {
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
	}

	// Qwen's OpenAI-compatible context-cache response partitions explicit
	// cache creation and cache reads inside prompt_tokens_details. The same
	// cached_tokens field also carries implicit-cache hits, so the request-side
	// cache mode must remain separate pricing evidence.
	if cacheModeled && cacheContract.WireSchema == modelprofile.CacheUsageWireQwenContextCacheV1 {
		if (len(wire.CachedTokens) > 0 && !isJSONNullUsage(wire.CachedTokens)) ||
			wire.PromptCacheHitTokens != nil || wire.PromptCacheMissTokens != nil ||
			(wire.PromptDetails != nil && wire.PromptDetails.CacheWriteTokens != nil) {
			return Usage{}, true, fmt.Errorf("Qwen cache profile requires prompt_tokens_details cache counters without another cache schema")
		}
		var cachedTokens, creationTokens int
		var cachedReported, creationReported bool
		if wire.PromptDetails != nil {
			if err := validateOptionalUsageCounters(
				optionalUsageCounter{name: "prompt_tokens_details.cache_creation_input_tokens", value: wire.PromptDetails.CacheCreationInputTokens},
			); err != nil {
				return Usage{}, true, err
			}
			if promptCachedReported {
				cachedTokens, cachedReported = promptCachedTokens, true
			}
			if wire.PromptDetails.CacheCreationInputTokens != nil {
				creationTokens, creationReported = *wire.PromptDetails.CacheCreationInputTokens, true
			}
		}
		if cachedReported || creationReported {
			status := CacheUsageStatusReadWriteReported
			if !creationReported {
				status = CacheUsageStatusReadReported
			} else if !cachedReported {
				status = CacheUsageStatusWriteReported
			}
			usage, err := NewCacheAwareUsage(
				promptTokens, promptTokens-cachedTokens-creationTokens,
				cachedTokens, creationTokens, completionTokens, totalTokens,
				source, CacheUsageSchemaQwenV1, status,
			)
			return usage, true, err
		}
	}

	// GPT-5.6 reports cache reads and writes as separate prompt token classes.
	// Both fields are required to remain inside prompt_tokens_details so a
	// similarly named provider extension cannot silently become billing truth.
	if cacheModeled && cacheContract.WireSchema == modelprofile.CacheUsageWireOpenAIReadWriteV1 {
		if (len(wire.CachedTokens) > 0 && !isJSONNullUsage(wire.CachedTokens)) ||
			wire.PromptCacheHitTokens != nil || wire.PromptCacheMissTokens != nil ||
			(wire.PromptDetails != nil && wire.PromptDetails.CacheCreationInputTokens != nil) {
			return Usage{}, true, fmt.Errorf("cache profile %s requires prompt_tokens_details cached_tokens/cache_write_tokens without another cache schema", cacheContract.ProfileID)
		}
		if wire.PromptDetails != nil && (promptCachedReported || wire.PromptDetails.CacheWriteTokens != nil) {
			if err := validateOptionalUsageCounters(
				optionalUsageCounter{name: "prompt_tokens_details.cache_write_tokens", value: wire.PromptDetails.CacheWriteTokens},
			); err != nil {
				return Usage{}, true, err
			}
			cached := promptCachedTokens
			written := valueOrZero(wire.PromptDetails.CacheWriteTokens)
			status := CacheUsageStatusReadWriteReported
			if !promptCachedReported {
				status = CacheUsageStatusWriteReported
			} else if wire.PromptDetails.CacheWriteTokens == nil {
				status = CacheUsageStatusReadReported
			}
			usage, err := NewCacheAwareUsage(
				promptTokens, promptTokens-cached-written, cached, written,
				completionTokens, totalTokens, source, CacheUsageSchemaOpenAIV2, status,
			)
			return usage, true, err
		}
	}

	// OpenAI-compatible profiles such as GLM and MiMo report only cache reads in
	// prompt_tokens_details.cached_tokens. The profile rejects top-level,
	// hit/miss, and cache-write counters rather than treating a different
	// provider schema as equivalent billing evidence.
	if cacheModeled && cacheContract.WireSchema == modelprofile.CacheUsageWireOpenAICachedTokensV1 {
		if (len(wire.CachedTokens) > 0 && !isJSONNullUsage(wire.CachedTokens)) ||
			wire.PromptCacheHitTokens != nil || wire.PromptCacheMissTokens != nil ||
			(wire.PromptDetails != nil && (wire.PromptDetails.CacheWriteTokens != nil || wire.PromptDetails.CacheCreationInputTokens != nil)) {
			return Usage{}, true, fmt.Errorf("cache profile %s requires prompt_tokens_details.cached_tokens without another cache schema", cacheContract.ProfileID)
		}
		if promptCachedNull && cacheContract.ReadCounterRequiredAtOrAbovePromptTokens > 0 {
			return Usage{}, true, fmt.Errorf("cache profile %s rejects null prompt_tokens_details.cached_tokens", cacheContract.ProfileID)
		}
		if promptCachedReported {
			cached := promptCachedTokens
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
		if cacheContract.RequireReadCounter && (cacheContract.ReadCounterRequiredAtOrAbovePromptTokens == 0 ||
			promptTokens >= cacheContract.ReadCounterRequiredAtOrAbovePromptTokens) {
			return Usage{}, true, fmt.Errorf("cache profile %s requires prompt_tokens_details.cached_tokens, including an authoritative zero", cacheContract.ProfileID)
		}
	}

	// Cache-looking fields from an unmodeled or unknown profile remain in the
	// preserved provider response but cannot partition usage or affect billing.
	// OpenAI itself receives an explicit cache contract when its model profiles
	// are adapted; wire-shape similarity is not authority.
	usage, err := NewUsage(
		promptTokens,
		completionTokens,
		totalTokens,
		source,
	)
	return usage, true, err
}

func validateOpenAIUsageCoreWire(wire *openAIUsageWire) error {
	if wire == nil {
		return nil
	}
	counters := []optionalUsageCounter{
		{name: "prompt_tokens", value: wire.PromptTokens},
		{name: "completion_tokens", value: wire.CompletionTokens},
		{name: "total_tokens", value: wire.TotalTokens},
	}
	return validateOptionalUsageCounters(counters...)
}

func parseOptionalUsageCounter(raw json.RawMessage, name string) (int, bool, error) {
	value, reported, _, err := parseOptionalUsageCounterPresence(raw, name)
	return value, reported, err
}

func parseOptionalUsageCounterPresence(raw json.RawMessage, name string) (int, bool, bool, error) {
	if len(raw) == 0 {
		return 0, false, false, nil
	}
	if isJSONNullUsage(raw) {
		return 0, false, true, nil
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false, false, fmt.Errorf("%s must be an integer", name)
	}
	if err := validateOptionalUsageCounters(optionalUsageCounter{name: name, value: &value}); err != nil {
		return 0, false, false, err
	}
	return value, true, false, nil
}

func isJSONNullUsage(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
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
