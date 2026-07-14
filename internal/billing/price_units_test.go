package billing

import (
	"math"
	"testing"
)

func TestPublicPriceUnitConversion(t *testing.T) {
	internal := CNYPerMillionToInternalPriceCents(2)
	if math.Abs(internal-0.2) > 1e-9 {
		t.Fatalf("expected 2 CNY/M to equal 0.2 internal cents/K, got %v", internal)
	}
	if public := InternalPriceCentsToCNYPerMillion(internal); math.Abs(public-2) > 1e-9 {
		t.Fatalf("expected round trip to preserve 2 CNY/M, got %v", public)
	}
}

func TestNormalizeCNYPerMillionPrice(t *testing.T) {
	for _, value := range []float64{0, 0.00001, 2, 1234.56789} {
		if normalized, ok := NormalizeCNYPerMillionPrice(value); !ok || normalized != value {
			t.Fatalf("expected %v to be accepted unchanged, got %v ok=%v", value, normalized, ok)
		}
	}
	for _, value := range []float64{-1, 0.000001, math.NaN(), math.Inf(1), 1e13} {
		if _, ok := NormalizeCNYPerMillionPrice(value); ok {
			t.Fatalf("expected %v to be rejected", value)
		}
	}
}
