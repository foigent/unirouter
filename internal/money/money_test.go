package money

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestCentsMicrosBoundaries(t *testing.T) {
	for _, tc := range []struct {
		cents  int64
		micros int64
	}{{0, 0}, {1, 10_000}, {-1, -10_000}, {123, 1_230_000}} {
		got, err := CentsToMicros(tc.cents)
		if err != nil || got != tc.micros {
			t.Fatalf("CentsToMicros(%d) = %d, %v; want %d", tc.cents, got, err, tc.micros)
		}
		back, err := MicrosToCentsExact(got)
		if err != nil || back != tc.cents {
			t.Fatalf("MicrosToCentsExact(%d) = %d, %v; want %d", got, back, err, tc.cents)
		}
	}
	if _, err := CentsToMicros(math.MaxInt64); !errors.Is(err, ErrOverflow) {
		t.Fatalf("large cents error = %v, want ErrOverflow", err)
	}
	if _, err := MicrosToCentsExact(1); err == nil {
		t.Fatal("non-divisible micro amount must fail closed")
	}
}

func TestDecimalCentsToMicros(t *testing.T) {
	for input, expected := range map[string]int64{
		"0": 0, "1": 10_000, "0.0001": 1, "1.23456": 12_346,
	} {
		actual, err := DecimalCentsToMicros(input)
		if err != nil || actual != expected {
			t.Fatalf("DecimalCentsToMicros(%q) = %d, %v; want %d", input, actual, err, expected)
		}
	}
}

func TestCheckedAddAndSub(t *testing.T) {
	if got, err := Add(2, 3); err != nil || got != 5 {
		t.Fatalf("Add(2,3) = %d, %v", got, err)
	}
	if _, err := Add(math.MaxInt64, 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow Add error = %v", err)
	}
	if got, err := Sub(5, 3); err != nil || got != 2 {
		t.Fatalf("Sub(5,3) = %d, %v", got, err)
	}
	if _, err := Sub(math.MaxInt64, -1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow Sub error = %v", err)
	}
	if got, err := Sub(-1, math.MinInt64); err != nil || got != math.MaxInt64 {
		t.Fatalf("Sub(-1, MinInt64) = %d, %v; want MaxInt64", got, err)
	}
	if _, err := Sub(0, math.MinInt64); !errors.Is(err, ErrOverflow) {
		t.Fatalf("MinInt64 subtraction error = %v, want ErrOverflow", err)
	}
}

func TestParseDecimalCanonical(t *testing.T) {
	for _, value := range []string{"0", "1", "-1", "+1.25", ".5", "5."} {
		if _, err := ParseDecimal(value); err != nil {
			t.Fatalf("ParseDecimal(%q): %v", value, err)
		}
	}
	for _, value := range []string{"", " ", " 1", "1 ", ".", "1e3", "1.2.3", "--1"} {
		if _, err := ParseDecimal(value); !errors.Is(err, ErrInvalidDecimal) {
			t.Fatalf("ParseDecimal(%q) error = %v, want ErrInvalidDecimal", value, err)
		}
	}
}

func TestPriceMicrosPerMillionFromInternal(t *testing.T) {
	got, err := PriceMicrosPerMillionFromInternal("0.2", "1", "1")
	if err != nil || got != 2_000_000 {
		t.Fatalf("price = %d, %v; want 2000000", got, err)
	}
	got, err = PriceMicrosPerMillionFromInternal("0.2", "0.1", "1.5")
	if err != nil || got != 300_000 {
		t.Fatalf("cache price = %d, %v; want 300000", got, err)
	}
	got, err = PriceMicrosPerMillionFromInternal("0.00000005", "1", "1")
	if err != nil || got != 1 {
		t.Fatalf("half-up price = %d, %v; want 1", got, err)
	}
	if _, err := PriceMicrosPerMillionFromInternal("-1", "1", "1"); !errors.Is(err, ErrNegativeValue) {
		t.Fatalf("negative price error = %v, want ErrNegativeValue", err)
	}
}

func TestCalculateMicroChargeAndZero(t *testing.T) {
	calculation, err := Calculate(Component{Tokens: 1, PriceMicrosPerMillion: 2_000_000})
	if err != nil || calculation.TotalMicros != 2 {
		t.Fatalf("one-token calculation = %+v, %v; want 2 micros", calculation, err)
	}
	zero, err := Calculate(Component{Tokens: 0, PriceMicrosPerMillion: 2_000_000})
	if err != nil || zero.TotalMicros != 0 {
		t.Fatalf("zero calculation = %+v, %v; want zero", zero, err)
	}
}

func TestCalculateRoundsTotalOnceAndConservesComponents(t *testing.T) {
	calculation, err := Calculate(
		Component{Tokens: 1, PriceMicrosPerMillion: 500_000},
		Component{Tokens: 1, PriceMicrosPerMillion: 500_000},
		Component{Tokens: 1, PriceMicrosPerMillion: 500_000},
	)
	if err != nil {
		t.Fatal(err)
	}
	if calculation.TotalMicros != 2 {
		t.Fatalf("total = %d, want 2", calculation.TotalMicros)
	}
	if !reflect.DeepEqual(calculation.ComponentMicros, []int64{1, 1, 0}) {
		t.Fatalf("components = %v, want deterministic [1 1 0]", calculation.ComponentMicros)
	}
	var sum int64
	for _, value := range calculation.ComponentMicros {
		sum += value
	}
	if sum != calculation.TotalMicros {
		t.Fatalf("component sum = %d, total = %d", sum, calculation.TotalMicros)
	}
}

func TestCalculateHalfUpAndOverflow(t *testing.T) {
	half, err := Calculate(Component{Tokens: 1, PriceMicrosPerMillion: 500_000})
	if err != nil || half.TotalMicros != 1 {
		t.Fatalf("half-up = %+v, %v; want 1", half, err)
	}
	if _, err := Calculate(Component{Tokens: math.MaxInt64, PriceMicrosPerMillion: math.MaxInt64}); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow error = %v, want ErrOverflow", err)
	}
	if _, err := Calculate(Component{Tokens: -1, PriceMicrosPerMillion: 1}); !errors.Is(err, ErrNegativeValue) {
		t.Fatalf("negative error = %v, want ErrNegativeValue", err)
	}
}

func TestEstimateHoldCeilsThenAppliesMinimum(t *testing.T) {
	hold, err := EstimateHold(1, 2_000_000, "1.5", 0)
	if err != nil || hold != 3 {
		t.Fatalf("hold = %d, %v; want 3 micros", hold, err)
	}
	hold, err = EstimateHold(1, 2_000_000, "1.5", 10)
	if err != nil || hold != 100_000 {
		t.Fatalf("minimum hold = %d, %v; want 100000 micros", hold, err)
	}
	if _, err := EstimateHold(math.MaxInt64, math.MaxInt64, "2", 0); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow error = %v, want ErrOverflow", err)
	}
}
