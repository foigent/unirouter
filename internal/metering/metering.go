package metering

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	UsageSourceUpstream         = "upstream"
	UsageSourceEstimated        = "estimated"
	UsageSourceAdapterEstimated = "adapter_estimated"
	UsageSourceCounterV2        = "counter_v2"
	UsageSourceCounterV3        = "counter_v3"
	UsageSourceResponseCache    = "response_cache"
	UsageSourceMissing          = "missing"
	UsageSourceNoRoute          = "no_route"
	UsageSourceUpstreamFailed   = "upstream_failed"

	DefaultEstimatedRatioAlertThreshold = 0.20
)

// MeterQuantity is a provider-evidenced non-token usage unit. Token classes
// remain explicit fields because their partition and estimation contracts are
// different; this envelope is for bounded meters such as builtin search calls.
type MeterQuantity struct {
	Meter    string `json:"meter"`
	Quantity int64  `json:"quantity"`
}

const MaxPersistedMeterQuantity = int64(1_000_000_000)

var meterNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,63}$`)

// NormalizeMeterQuantities provides one canonical order and duplicate/range
// contract shared by adapters, Billing, receipts, and replays.
func NormalizeMeterQuantities(values []MeterQuantity) ([]MeterQuantity, error) {
	if len(values) == 0 {
		return []MeterQuantity{}, nil
	}
	result := slices.Clone(values)
	slices.SortFunc(result, func(left, right MeterQuantity) int {
		return strings.Compare(strings.TrimSpace(left.Meter), strings.TrimSpace(right.Meter))
	})
	for index := range result {
		result[index].Meter = strings.TrimSpace(result[index].Meter)
		if !meterNamePattern.MatchString(result[index].Meter) {
			return nil, fmt.Errorf("meter %q is invalid", result[index].Meter)
		}
		if result[index].Quantity < 0 || result[index].Quantity > MaxPersistedMeterQuantity {
			return nil, fmt.Errorf("meter %s quantity is outside the persisted range", result[index].Meter)
		}
		if index > 0 && result[index-1].Meter == result[index].Meter {
			return nil, fmt.Errorf("meter %s is duplicated", result[index].Meter)
		}
	}
	return result, nil
}

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
	GatewayCounterEstimateVersionV3 = "unirouter.stream-counter.v3"

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
	case UsageSourceUpstream, UsageSourceEstimated, UsageSourceAdapterEstimated, UsageSourceCounterV2, UsageSourceCounterV3, UsageSourceResponseCache, UsageSourceMissing, UsageSourceNoRoute, UsageSourceUpstreamFailed:
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
	case UsageSourceCounterV3:
		return UsageAuthorityGatewayCounter, purpose, UsageFinalityEstimated, GatewayCounterEstimateVersionV3
	case UsageSourceEstimated, UsageSourceAdapterEstimated:
		return UsageAuthorityConservativeEstimate, purpose, UsageFinalityEstimated, ConservativeEstimateVersionV1
	default:
		return UsageAuthorityNone, UsagePurposeProviderEvidence, UsageFinalityMissing, ""
	}
}

func IsNonUpstreamUsage(source string) bool {
	switch NormalizeUsageSource(source) {
	case UsageSourceEstimated, UsageSourceAdapterEstimated, UsageSourceCounterV2, UsageSourceCounterV3, UsageSourceResponseCache:
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
	case UsageSourceCounterV2, UsageSourceCounterV3:
		return 0.65
	case UsageSourceEstimated:
		return 0.50
	default:
		return 0.0
	}
}

// CounterSource and CounterVersion preserve the exact finite estimator identity.
// They never infer a current version from an unknown or empty value.
func CounterSource(version string) (string, error) {
	switch version {
	case GatewayCounterEstimateVersionV2:
		return UsageSourceCounterV2, nil
	case GatewayCounterEstimateVersionV3:
		return UsageSourceCounterV3, nil
	default:
		return "", fmt.Errorf("unsupported stream counter version %q", version)
	}
}

func CounterVersion(source string) (string, error) {
	switch source {
	case UsageSourceCounterV2:
		return GatewayCounterEstimateVersionV2, nil
	case UsageSourceCounterV3:
		return GatewayCounterEstimateVersionV3, nil
	default:
		return "", fmt.Errorf("unsupported stream counter source %q", source)
	}
}

func ValidCounterPair(source, version string) bool {
	expected, err := CounterSource(version)
	return err == nil && source == expected
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
