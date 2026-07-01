package metering

import "strings"

const (
	UsageSourceUpstream         = "upstream"
	UsageSourceEstimated        = "estimated"
	UsageSourceAdapterEstimated = "adapter_estimated"
	UsageSourceCounterV2        = "counter_v2"
	UsageSourceNoRoute          = "no_route"
	UsageSourceUpstreamFailed   = "upstream_failed"

	DefaultEstimatedRatioAlertThreshold = 0.20
)

func NormalizeUsageSource(source string) string {
	switch strings.TrimSpace(source) {
	case "", "api":
		return UsageSourceUpstream
	case "stream_estimate":
		return UsageSourceCounterV2
	case UsageSourceUpstream, UsageSourceEstimated, UsageSourceAdapterEstimated, UsageSourceCounterV2, UsageSourceNoRoute, UsageSourceUpstreamFailed:
		return strings.TrimSpace(source)
	default:
		return strings.TrimSpace(source)
	}
}

func IsNonUpstreamUsage(source string) bool {
	switch NormalizeUsageSource(source) {
	case UsageSourceEstimated, UsageSourceAdapterEstimated, UsageSourceCounterV2:
		return true
	default:
		return false
	}
}

func SourceConfidence(source string) float64 {
	switch NormalizeUsageSource(source) {
	case UsageSourceUpstream:
		return 1.0
	case UsageSourceAdapterEstimated:
		return 0.75
	case UsageSourceCounterV2:
		return 0.65
	case UsageSourceEstimated:
		return 0.50
	default:
		return 0.0
	}
}

func VarianceTokens(estimatedTokens, actualTokens int) int {
	return actualTokens - estimatedTokens
}

func VarianceRatio(estimatedTokens, actualTokens int) float64 {
	if estimatedTokens <= 0 {
		return 0
	}
	return float64(actualTokens-estimatedTokens) / float64(estimatedTokens)
}

func EstimatedTokenRatio(estimatedTokens, upstreamTokens int) float64 {
	total := estimatedTokens + upstreamTokens
	if total <= 0 {
		return 0
	}
	return float64(estimatedTokens) / float64(total)
}
