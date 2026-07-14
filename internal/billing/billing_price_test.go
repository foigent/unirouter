package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func newCanceledQueryPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://invalid:invalid@127.0.0.1:1/invalid?sslmode=disable")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// --- QueryModelPriceMicrosForOrg unit tests ---

func TestQueryModelPrice_Success(t *testing.T) {
	b := newTestBilling(mockPrice(5, 15))
	input, output, err := b.QueryModelPriceMicrosForOrg(context.Background(), 1, "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input != 50_000_000 {
		t.Errorf("expected input 50000000 micro-CNY/million, got %v", input)
	}
	if output != 150_000_000 {
		t.Errorf("expected output 150000000 micro-CNY/million, got %v", output)
	}
}

func TestQueryModelPrice_Error(t *testing.T) {
	b := newTestBilling(errPrice(errors.New("db timeout")))
	_, _, err := b.QueryModelPriceMicrosForOrg(context.Background(), 1, "model")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "db timeout" {
		t.Errorf("expected 'db timeout', got %q", err.Error())
	}
}

func TestDBPriceProvider_QueryErrorDoesNotFallback(t *testing.T) {
	provider := NewDBPriceProvider(newCanceledQueryPool(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input, output, err := provider.QueryModelPrice(ctx, "gpt-4o")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "expected wrapped context cancellation, got %v", err)
	require.Zero(t, input)
	require.Zero(t, output)
}

func TestQueryModelPriceForOrg_QueryErrorDoesNotFallback(t *testing.T) {
	b := NewBilling(newCanceledQueryPool(t), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input, output, err := b.QueryModelPriceMicrosForOrg(ctx, 1, "gpt-4o")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "expected wrapped context cancellation, got %v", err)
	require.Zero(t, input)
	require.Zero(t, output)
}

func TestQueryModelPrice_ZeroPrices(t *testing.T) {
	b := newTestBilling(mockPrice(0, 0))
	input, output, err := b.QueryModelPriceMicrosForOrg(context.Background(), 1, "free-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input != 0 || output != 0 {
		t.Errorf("expected 0/0, got %v/%v", input, output)
	}
}

// --- CalculateCost edge cases ---

func TestCalculateCost_SingleToken(t *testing.T) {
	b := newTestBilling(mockPrice(1000, 1000))
	cost, err := b.CalculateCostMicros(context.Background(), "model", 1, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != 20_000 {
		t.Errorf("expected 20000 micros, got %d", cost)
	}
}

func TestCalculateCost_HighPriceModel(t *testing.T) {
	b := newTestBilling(mockPrice(100, 200))
	cost, err := b.CalculateCostMicros(context.Background(), "expensive", 500, 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cost != 1_500_000 {
		t.Errorf("expected 1500000 micros, got %d", cost)
	}
}

// --- EstimateFreezeAmount edge cases ---

func TestEstimateFreezeAmount_LargeSafetyFactor(t *testing.T) {
	b := newTestBilling(mockPrice(2, 10))
	amount, err := b.EstimateFreezeMicros(context.Background(), "model", 1000, "10", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amount != 1_000_000 {
		t.Errorf("expected 1000000 micros, got %d", amount)
	}
}

func TestEstimateFreezeAmount_ExactMinFreeze(t *testing.T) {
	b := newTestBilling(mockPrice(0, 1))
	amount, err := b.EstimateFreezeMicros(context.Background(), "model", 100, "1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amount != 10_000 {
		t.Errorf("expected 10000 micros (min freeze), got %d", amount)
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
	// QueryModelPriceMicrosForOrg should panic with nil prices
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic with nil PriceProvider")
		}
	}()
	b.QueryModelPriceMicrosForOrg(context.Background(), 1, "model")
}
