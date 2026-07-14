package billing

import "testing"

func TestPublicPriceUnitConversionIsExact(t *testing.T) {
	tests := []struct {
		public   string
		internal string
	}{
		{public: "0", internal: "0"},
		{public: "2", internal: "0.2"},
		{public: "0.00001", internal: "0.000001"},
		{public: "1234.56789", internal: "123.456789"},
		{public: "9223372036854.7758", internal: "922337203685.47758"},
	}
	for _, test := range tests {
		internal, ok := CNYPerMillionToInternalPriceCents(test.public)
		if !ok || internal != test.internal {
			t.Fatalf("%s CNY/M: internal=%q ok=%v, want %q", test.public, internal, ok, test.internal)
		}
		public, ok := InternalPriceCentsToCNYPerMillion(internal)
		if !ok || public != test.public {
			t.Fatalf("%s cents/K: public=%q ok=%v, want %q", internal, public, ok, test.public)
		}
	}
}

func TestNormalizeCNYPerMillionPrice(t *testing.T) {
	accepted := map[string]string{
		"0":          "0",
		"0002.50000": "2.5",
		"0.00001":    "0.00001",
		"1234.56789": "1234.56789",
	}
	for value, want := range accepted {
		if normalized, ok := NormalizeCNYPerMillionPrice(value); !ok || normalized != want {
			t.Fatalf("expected %q to normalize to %q, got %q ok=%v", value, want, normalized, ok)
		}
	}
	for _, value := range []string{"-1", "+1", "0.000001", "1e3", "NaN", "9223372036854.77581", "10000000000000", "1.", ".1", " 1"} {
		if _, ok := NormalizeCNYPerMillionPrice(value); ok {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestNormalizeExactPricingRatios(t *testing.T) {
	for input, want := range map[string]string{"0": "0", "0.100000": "0.1", "100.000000": "100"} {
		if got, ok := NormalizeCachePriceRatio(input); !ok || got != want {
			t.Fatalf("cache ratio %q: got %q ok=%v, want %q", input, got, ok, want)
		}
	}
	for _, value := range []string{"-0.1", "0.0000001", "100.000001", "1e0"} {
		if _, ok := NormalizeCachePriceRatio(value); ok {
			t.Fatalf("expected cache ratio %q to be rejected", value)
		}
	}
	for input, want := range map[string]string{"0.001": "0.001", "01.200": "1.2", "99.999": "99.999"} {
		if got, ok := NormalizeOrgModelPriceMultiplier(input); !ok || got != want {
			t.Fatalf("org multiplier %q: got %q ok=%v, want %q", input, got, ok, want)
		}
	}
	for _, value := range []string{"0", "0.0009", "100", "99.9990", "1e0"} {
		if _, ok := NormalizeOrgModelPriceMultiplier(value); ok {
			t.Fatalf("expected org multiplier %q to be rejected", value)
		}
	}
}
