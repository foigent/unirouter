package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrModelPriceNotConfigured is returned when a routed model has no explicit
// price authority. Enforced and strict pricing modes fail closed. The default
// local cost-observation mode may continue with an explicit immutable
// "unpriced" snapshot so missing cost metadata does not become traffic policy.
var ErrModelPriceNotConfigured = errors.New("model price is not configured")

// PriceProvider returns the legacy database source unit as exact decimal text:
// CNY cents per 1,000 tokens. Text keeps customer calculations out of binary
// floating point while allowing tests to inject deterministic prices.
type PriceProvider interface {
	QueryModelPrice(ctx context.Context, model string) (inputCentsPerThousand, outputCentsPerThousand string, err error)
}

// DBPriceProvider is the production implementation backed by model_prices table.
type DBPriceProvider struct {
	db *pgxpool.Pool
}

// NewDBPriceProvider creates a DBPriceProvider from a connection pool.
func NewDBPriceProvider(db *pgxpool.Pool) *DBPriceProvider {
	return &DBPriceProvider{db: db}
}

// QueryModelPrice returns the internal CNY-cent rate per 1,000 tokens for a
// model. Missing prices and query failures are both returned to callers.
func (p *DBPriceProvider) QueryModelPrice(ctx context.Context, model string) (inputCost, outputCost string, err error) {
	err = p.db.QueryRow(ctx,
		`SELECT input_price_cents::text, output_price_cents::text FROM model_prices WHERE model_name = $1`,
		model).Scan(&inputCost, &outputCost)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", fmt.Errorf("%w: %s", ErrModelPriceNotConfigured, model)
		}
		return "", "", fmt.Errorf("query model price for %s: %w", model, err)
	}
	return
}

// PriceProviderFunc is a function adapter for PriceProvider, convenient for tests.
type PriceProviderFunc func(ctx context.Context, model string) (inputCost, outputCost string, err error)

// QueryModelPrice implements PriceProvider.
func (f PriceProviderFunc) QueryModelPrice(ctx context.Context, model string) (string, string, error) {
	return f(ctx, model)
}
