package billing

import (
	"context"
	"errors"
	"testing"
)

// --- QueryModelPrice unit tests ---

func TestQueryModelPrice_Success(t *testing.T) {
	b := newTestBilling(mockPrice(5, 15))
	input, output, err := b.QueryModelPrice(context.Background(), "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input != 5 {
		t.Errorf("expected input 5, got %d", input)
	}
	if output != 15 {
		t.Errorf("expected output 15, got %d", output)
	}
}

func TestQueryModelPrice_Error(t *testing.T) {
	b := newTestBilling(errPrice(errors.New("db timeout")))
	_, _, err := b.QueryModelPrice(context.Background(), "model")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "db timeout" {
		t.Errorf("expected 'db timeout', got %q", err.Error())
	}
}

func TestQueryModelPrice_ZeroPrices(t *testing.T) {
	b := newTestBilling(mockPrice(0, 0))
	input, output, err := b.QueryModelPrice(context.Background(), "free-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input != 0 || output != 0 {
		t.Errorf("expected 0/0, got %d/%d", input, output)
	}
}

// --- CalculateCost edge cases ---

func TestCalculateCost_SingleToken(t *testing.T) {
	b := newTestBilling(mockPrice(1000, 1000))
	cost, err := b.CalculateCost(context.Background(), "model", 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (1*1000 + 1*1000) / 1000 = 2
	if cost != 2 {
		t.Errorf("expected 2, got %d", cost)
	}
}

func TestCalculateCost_HighPriceModel(t *testing.T) {
	b := newTestBilling(mockPrice(100, 200))
	cost, err := b.CalculateCost(context.Background(), "expensive", 500, 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (500*100 + 500*200) / 1000 = 150000 / 1000 = 150
	if cost != 150 {
		t.Errorf("expected 150, got %d", cost)
	}
}

// --- EstimateFreezeAmount edge cases ---

func TestEstimateFreezeAmount_LargeSafetyFactor(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10))
	amount, err := b.EstimateFreezeAmount(context.Background(), "model", 1000, 10.0, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// math.Round(1000 * 10 / 1000 * 10.0) = math.Round(100.0) = 100
	if amount != 100 {
		t.Errorf("expected 100, got %d", amount)
	}
}

func TestEstimateFreezeAmount_ExactMinFreeze(t *testing.T) {
	b := newTestBilling(mockPrice(0, 1))
	amount, err := b.EstimateFreezeAmount(context.Background(), "model", 100, 1.0, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// math.Round(100 * 1 / 1000 * 1.0) = math.Round(0.1) = 0 → clamped to 1
	if amount != 1 {
		t.Errorf("expected 1 (min freeze), got %d", amount)
	}
}

// --- WithPriceProvider chaining ---

func TestWithPriceProvider_ReturnsSelf(t *testing.T) {
	b := &Billing{}
	result := b.WithPriceProvider(mockPrice(1, 1))
	if result != b {
		t.Error("WithPriceProvider should return same Billing instance")
	}
}

func TestWithPriceProvider_NilPriceProvider(t *testing.T) {
	b := &Billing{}
	b.WithPriceProvider(nil)
	// QueryModelPrice should panic with nil prices
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic with nil PriceProvider")
		}
	}()
	b.QueryModelPrice(context.Background(), "model")
}
