package metering

import "testing"

func TestCounterVersionPairsRemainExact(t *testing.T) {
	for _, pair := range [][2]string{{UsageSourceCounterV2, GatewayCounterEstimateVersionV2}, {UsageSourceCounterV3, GatewayCounterEstimateVersionV3}} {
		source, err := CounterSource(pair[1])
		if err != nil || source != pair[0] {
			t.Fatal(source, err)
		}
		version, err := CounterVersion(pair[0])
		if err != nil || version != pair[1] {
			t.Fatal(version, err)
		}
		if !ValidCounterPair(source, version) || !IsNonUpstreamUsage(source) || SourceConfidence(source) != 0.65 {
			t.Fatal("counter classification")
		}
	}
	for _, pair := range [][2]string{{UsageSourceCounterV2, GatewayCounterEstimateVersionV3}, {UsageSourceCounterV3, GatewayCounterEstimateVersionV2}, {"counter_v4", GatewayCounterEstimateVersionV3}, {UsageSourceCounterV3, ""}} {
		if ValidCounterPair(pair[0], pair[1]) {
			t.Fatal("mismatched pair accepted", pair)
		}
	}
	if NormalizeUsageSource("stream_estimate") != UsageSourceCounterV2 {
		t.Fatal("historical alias changed")
	}
}
