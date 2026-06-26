package billing

import (
	"context"
	"errors"
	"math"
	"testing"
)

// mockPrice returns a PriceProvider that always returns the given prices.
func mockPrice(inputCents, outputCents int) PriceProvider {
	return PriceProviderFunc(func(_ context.Context, _ string) (int, int, error) {
		return inputCents, outputCents, nil
	})
}

// errPrice returns a PriceProvider that always returns an error.
func errPrice(err error) PriceProvider {
	return PriceProviderFunc(func(_ context.Context, _ string) (int, int, error) {
		return 0, 0, err
	})
}

func newTestBilling(pp PriceProvider) *Billing {
	return &Billing{prices: pp}
}

// --- CalculateCost unit tests ---

func TestCalculateCost_Normal(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10)) // gpt-4o default
	cost, err := b.CalculateCost(context.Background(), "gpt-4o", 1000, 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (1000*2 + 500*10) / 1000 = 7000 / 1000 = 7
	if cost != 7 {
		t.Errorf("expected 7, got %d", cost)
	}
}

func TestCalculateCost_SubCent(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10))
	cost, err := b.CalculateCost(context.Background(), "gpt-4o", 1, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (1*2 + 0*10) / 1000 = 0 → clamped to 1
	if cost != 1 {
		t.Errorf("expected 1 (min clamp), got %d", cost)
	}
}

func TestCalculateCost_ZeroTokens(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10))
	cost, err := b.CalculateCost(context.Background(), "gpt-4o", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 0 / 1000 = 0 → clamped to 1
	if cost != 1 {
		t.Errorf("expected 1 (min clamp), got %d", cost)
	}
}

func TestCalculateCost_LargeTokens(t *testing.T) {
	b := newTestBilling(mockPrice(50, 100))
	cost, err := b.CalculateCost(context.Background(), "model", 1_000_000, 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (1_000_000*50 + 1_000_000*100) / 1000 = 150_000_000 / 1000 = 150000
	if cost != 150000 {
		t.Errorf("expected 150000, got %d", cost)
	}
}

func TestCalculateCost_ExactBoundary1000(t *testing.T) {
	b := newTestBilling(mockPrice(1, 0))
	cost, err := b.CalculateCost(context.Background(), "model", 1000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (1000*1 + 0) / 1000 = 1
	if cost != 1 {
		t.Errorf("expected 1, got %d", cost)
	}
}

func TestCalculateCost_BelowBoundary999(t *testing.T) {
	b := newTestBilling(mockPrice(1, 0))
	cost, err := b.CalculateCost(context.Background(), "model", 999, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (999*1 + 0) / 1000 = 0 → clamped to 1
	if cost != 1 {
		t.Errorf("expected 1 (min clamp), got %d", cost)
	}
}

func TestCalculateCost_OnlyCompletionTokens(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10))
	cost, err := b.CalculateCost(context.Background(), "model", 0, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (0*2 + 1000*10) / 1000 = 10
	if cost != 10 {
		t.Errorf("expected 10, got %d", cost)
	}
}

func TestCalculateCost_PriceProviderError(t *testing.T) {
	b := newTestBilling(errPrice(errors.New("db down")))
	_, err := b.CalculateCost(context.Background(), "model", 100, 100)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "db down" {
		t.Errorf("expected 'db down', got %q", err.Error())
	}
}

func TestCalculateCost_BothZeroPrice(t *testing.T) {
	b := newTestBilling(mockPrice(0, 0))
	cost, err := b.CalculateCost(context.Background(), "model", 10000, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (10000*0 + 10000*0) / 1000 = 0 → clamped to 1
	if cost != 1 {
		t.Errorf("expected 1 (min clamp with zero price), got %d", cost)
	}
}

// --- EstimateFreezeAmount unit tests ---

func TestEstimateFreezeAmount_Normal(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10))
	amount, err := b.EstimateFreezeAmount(context.Background(), "gpt-4o", 1000, 1.5, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// math.Round(1000 * 10 / 1000 * 1.5) = math.Round(15.0) = 15
	if amount != 15 {
		t.Errorf("expected 15, got %d", amount)
	}
}

func TestEstimateFreezeAmount_MinFreezeDominates(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10))
	amount, err := b.EstimateFreezeAmount(context.Background(), "gpt-4o", 1, 1.0, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// math.Round(1 * 10 / 1000 * 1.0) = math.Round(0.01) = 0 → clamped to 100
	if amount != 100 {
		t.Errorf("expected 100 (min freeze), got %d", amount)
	}
}

func TestEstimateFreezeAmount_SafetyFactorZero(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10))
	amount, err := b.EstimateFreezeAmount(context.Background(), "model", 1000, 0, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// math.Round(1000 * 10 / 1000 * 0) = 0 → clamped to 5
	if amount != 5 {
		t.Errorf("expected 5 (min freeze with safety=0), got %d", amount)
	}
}

func TestEstimateFreezeAmount_LargeValues(t *testing.T) {
	b := newTestBilling(mockPrice(50, 100))
	amount, err := b.EstimateFreezeAmount(context.Background(), "model", 1_000_000, 2.0, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// math.Round(1_000_000 * 100 / 1000 * 2.0) = math.Round(200000.0) = 200000
	if amount != 200000 {
		t.Errorf("expected 200000, got %d", amount)
	}
}

func TestEstimateFreezeAmount_ZeroEstimatedTokens(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10))
	amount, err := b.EstimateFreezeAmount(context.Background(), "model", 0, 1.5, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// math.Round(0 * 10 / 1000 * 1.5) = 0 → clamped to 10
	if amount != 10 {
		t.Errorf("expected 10 (min freeze with zero tokens), got %d", amount)
	}
}

func TestEstimateFreezeAmount_PriceProviderError(t *testing.T) {
	b := newTestBilling(errPrice(errors.New("connection refused")))
	_, err := b.EstimateFreezeAmount(context.Background(), "model", 100, 1.5, 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "connection refused" {
		t.Errorf("expected 'connection refused', got %q", err.Error())
	}
}

func TestEstimateFreezeAmount_Rounding(t *testing.T) {
	b := newTestBilling(mockPrice(0, 7))
	amount, err := b.EstimateFreezeAmount(context.Background(), "model", 150, 1.0, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// math.Round(150 * 7 / 1000 * 1.0) = math.Round(1.05) = 1
	expected := int(math.Round(150.0 * 7.0 / 1000.0))
	if amount != expected {
		t.Errorf("expected %d (rounding), got %d", expected, amount)
	}
}

// --- WithPriceProvider integration ---

func TestWithPriceProvider_OverridesDefault(t *testing.T) {
	// Create with nil DB (would panic if PriceProvider not overridden)
	b := &Billing{db: nil, rdb: nil}

	// Override with mock
	mock := mockPrice(99, 99)
	b.WithPriceProvider(mock)

	cost, err := b.CalculateCost(context.Background(), "any", 1000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (1000*99 + 0) / 1000 = 99
	if cost != 99 {
		t.Errorf("expected 99, got %d", cost)
	}
}
