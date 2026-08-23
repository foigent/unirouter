package enterprisefinance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOfficialRateSourceRequiresVerifiableBoundedFreshnessEvidence(t *testing.T) {
	fetchedAt := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	evidence := RateSourceEvidence{
		OrgID: 7, EvidenceRef: "official-openai-2026-08-13", SourceKind: RateSourceOfficialImport,
		SourceURL: "https://example.invalid/pricing", SourceRevision: "revision-7",
		FetchedAt: fetchedAt, PayloadSHA256: "sha256:" + stringRepeat("a", 64),
		TransformVersion: "openai-pricing.v1", Currency: "CNY", UnitContract: RateSourceUnitTokens,
		LastSuccessAt: fetchedAt, StaleAfter: fetchedAt.Add(24 * time.Hour), ImportedBy: "user:2",
	}
	require.NoError(t, ValidateRateSourceEvidence(evidence))
	evidence.UnitContract = RateSourceUnitComponents
	require.NoError(t, ValidateRateSourceEvidence(evidence))
	evidence.UnitContract = "calls"
	require.ErrorContains(t, ValidateRateSourceEvidence(evidence), "rate_source_currency_unit")
	evidence.UnitContract = RateSourceUnitTokens

	evidence.SourceURL = "unirouter://model_prices/model-a"
	require.ErrorContains(t, ValidateRateSourceEvidence(evidence), "official_rate_source_url")
	evidence.SourceURL = "http://official.example/pricing"
	require.ErrorContains(t, ValidateRateSourceEvidence(evidence), "official_rate_source_url")
	evidence.SourceURL = "https://example.invalid/pricing"
	evidence.PayloadSHA256 = "sha256:not-a-hash"
	require.ErrorContains(t, ValidateRateSourceEvidence(evidence), "rate_source_payload_sha256")
	evidence.PayloadSHA256 = "sha256:" + stringRepeat("a", 64)
	evidence.StaleAfter = evidence.LastSuccessAt
	require.ErrorContains(t, ValidateRateSourceEvidence(evidence), "rate_source_freshness")
}

func TestRateSourceUnitContractDistinguishesMixedMeters(t *testing.T) {
	require.Equal(t, RateSourceUnitTokens, rateSourceUnitContract([]RateComponent{{Meter: MeterInputTokens}}))
	require.Equal(t, RateSourceUnitComponents, rateSourceUnitContract([]RateComponent{
		{Meter: MeterInputTokens}, {Meter: MeterBuiltinWebSearchCalls},
	}))
}

func stringRepeat(value string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += value
	}
	return result
}
