package metering

import "testing"

func TestNormalizeUsageSource_LegacyAliases(t *testing.T) {
	cases := map[string]string{
		"":                UsageSourceUpstream,
		"api":             UsageSourceUpstream,
		"stream_estimate": UsageSourceCounterV2,
		"estimated":       UsageSourceEstimated,
	}
	for input, want := range cases {
		if got := NormalizeUsageSource(input); got != want {
			t.Fatalf("NormalizeUsageSource(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSourceConfidence(t *testing.T) {
	if got := SourceConfidence(UsageSourceUpstream); got != 1.0 {
		t.Fatalf("upstream confidence = %v", got)
	}
	if got := SourceConfidence(UsageSourceCounterV2); got <= 0 || got >= 1 {
		t.Fatalf("counter_v2 confidence should be between 0 and 1, got %v", got)
	}
}

func TestEstimatedTokenRatio(t *testing.T) {
	if got := EstimatedTokenRatio(25, 75); got != 0.25 {
		t.Fatalf("ratio = %v, want 0.25", got)
	}
	if got := EstimatedTokenRatio(0, 0); got != 0 {
		t.Fatalf("zero ratio = %v, want 0", got)
	}
}
