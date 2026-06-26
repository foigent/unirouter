package billing

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PriceProvider abstracts model price lookup so that billing calculation
// logic can be unit-tested without a live PostgreSQL connection.
type PriceProvider interface {
	QueryModelPrice(ctx context.Context, model string) (inputCost, outputCost int, err error)
}

// DBPriceProvider is the production implementation backed by model_prices table.
type DBPriceProvider struct {
	db *pgxpool.Pool
}

// NewDBPriceProvider creates a DBPriceProvider from a connection pool.
func NewDBPriceProvider(db *pgxpool.Pool) *DBPriceProvider {
	return &DBPriceProvider{db: db}
}

// QueryModelPrice returns per-token cost in cents for a model.
// Falls back to a default rate ($0.002/1K input, $0.01/1K output) on error.
func (p *DBPriceProvider) QueryModelPrice(ctx context.Context, model string) (inputCost, outputCost int, err error) {
	err = p.db.QueryRow(ctx,
		`SELECT input_price_cents, output_price_cents FROM model_prices WHERE model_name = $1`,
		model).Scan(&inputCost, &outputCost)
	if err != nil {
		log.Printf("QueryModelPrice: fallback to default for model %s: %v", model, err)
		inputCost = 2
		outputCost = 10
		err = nil
	}
	return
}

// PriceProviderFunc is a function adapter for PriceProvider, convenient for tests.
type PriceProviderFunc func(ctx context.Context, model string) (inputCost, outputCost int, err error)

// QueryModelPrice implements PriceProvider.
func (f PriceProviderFunc) QueryModelPrice(ctx context.Context, model string) (int, int, error) {
	return f(ctx, model)
}
