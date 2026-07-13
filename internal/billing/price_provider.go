package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrModelPriceNotConfigured is returned when a routed model has no explicit
// price authority. Billing must fail closed instead of inventing a default
// customer price.
var ErrModelPriceNotConfigured = errors.New("model price is not configured")

// PriceProvider abstracts model price lookup so that billing calculation
// logic can be unit-tested without a live PostgreSQL connection.
type PriceProvider interface {
	QueryModelPrice(ctx context.Context, model string) (inputCost, outputCost float64, err error)
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
func (p *DBPriceProvider) QueryModelPrice(ctx context.Context, model string) (inputCost, outputCost float64, err error) {
	err = p.db.QueryRow(ctx,
		`SELECT input_price_cents::float8, output_price_cents::float8 FROM model_prices WHERE model_name = $1`,
		model).Scan(&inputCost, &outputCost)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, fmt.Errorf("%w: %s", ErrModelPriceNotConfigured, model)
		}
		return 0, 0, fmt.Errorf("query model price for %s: %w", model, err)
	}
	return
}

// PriceProviderFunc is a function adapter for PriceProvider, convenient for tests.
type PriceProviderFunc func(ctx context.Context, model string) (inputCost, outputCost float64, err error)

// QueryModelPrice implements PriceProvider.
func (f PriceProviderFunc) QueryModelPrice(ctx context.Context, model string) (float64, float64, error) {
	return f(ctx, model)
}
