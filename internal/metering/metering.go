package metering

import "strings"

const (
	UsageSourceUpstream         = "upstream"
	UsageSourceEstimated        = "estimated"
	UsageSourceAdapterEstimated = "adapter_estimated"
	UsageSourceCounterV2        = "counter_v2"
	UsageSourceResponseCache    = "response_cache"
	UsageSourceMissing          = "missing"
	UsageSourceNoRoute          = "no_route"
	UsageSourceUpstreamFailed   = "upstream_failed"

	DefaultEstimatedRatioAlertThreshold = 0.20
)

type UsageAuthority string

const (
	UsageAuthorityProviderReported     UsageAuthority = "provider_reported"
	UsageAuthorityGatewayCounter       UsageAuthority = "gateway_counter"
	UsageAuthorityConservativeEstimate UsageAuthority = "conservative_estimate"
	UsageAuthorityLocalCache           UsageAuthority = "local_cache"
	UsageAuthorityNone                 UsageAuthority = "none"
)

type UsagePurpose string

const (
	UsagePurposeProviderEvidence UsagePurpose = "provider_evidence"
	UsagePurposeCustomerCharge   UsagePurpose = "customer_charge"
)

type UsageFinality string

const (
	UsageFinalityFinal     UsageFinality = "final"
	UsageFinalityEstimated UsageFinality = "estimated"
	UsageFinalityMissing   UsageFinality = "missing"
)

const (
	UsageContractVersion            = "unirouter.usage.v1"
	UsageContractVersionV2          = "unirouter.usage.v2"
	ConservativeEstimateVersionV1   = "unirouter.token-ceiling.v1"
	GatewayCounterEstimateVersionV2 = "unirouter.stream-counter.v2"

	// MaxPersistedTokenCount is the common upper bound for every final usage
	// contract that can be materialized by billing's INTEGER-backed receipts.
	// Protocol parsers must enforce it before accepting provider liability.
	MaxPersistedTokenCount = 1<<31 - 1
)

func NormalizeUsageSource(source string) string {
	switch strings.TrimSpace(source) {
	case "", "api":
		return UsageSourceUpstream
	case "stream_estimate":
		return UsageSourceCounterV2
	case UsageSourceUpstream, UsageSourceEstimated, UsageSourceAdapterEstimated, UsageSourceCounterV2, UsageSourceResponseCache, UsageSourceMissing, UsageSourceNoRoute, UsageSourceUpstreamFailed:
		return strings.TrimSpace(source)
	default:
		return strings.TrimSpace(source)
	}
}

func LegacyUsageContract(source string, customerCharge bool) (UsageAuthority, UsagePurpose, UsageFinality, string) {
	purpose := UsagePurposeProviderEvidence
	if customerCharge {
		purpose = UsagePurposeCustomerCharge
	}
	switch NormalizeUsageSource(source) {
	case UsageSourceUpstream:
		return UsageAuthorityProviderReported, purpose, UsageFinalityFinal, ""
	case UsageSourceCounterV2:
		return UsageAuthorityGatewayCounter, purpose, UsageFinalityEstimated, GatewayCounterEstimateVersionV2
	case UsageSourceEstimated, UsageSourceAdapterEstimated:
		return UsageAuthorityConservativeEstimate, purpose, UsageFinalityEstimated, ConservativeEstimateVersionV1
	default:
		return UsageAuthorityNone, UsagePurposeProviderEvidence, UsageFinalityMissing, ""
	}
}

func IsNonUpstreamUsage(source string) bool {
	switch NormalizeUsageSource(source) {
	case UsageSourceEstimated, UsageSourceAdapterEstimated, UsageSourceCounterV2, UsageSourceResponseCache:
		return true
	default:
		return false
	}
}

func SourceConfidence(source string) float64 {
	switch NormalizeUsageSource(source) {
	case UsageSourceUpstream:
		return 1.0
	case UsageSourceResponseCache:
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
