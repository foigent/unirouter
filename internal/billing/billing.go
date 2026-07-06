package billing

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// FreezeRequest contains the parameters for a freeze operation.
type FreezeRequest struct {
	RequestID       string
	OrgID           int64
	Model           string
	EstimatedTokens int
	SafetyFactor    float64
	MinFreezeAmount int
}

// FreezeResult contains the result of a freeze operation.
type FreezeResult struct {
	FreezeID    int64
	Amount      int
	NewBalance  int
	Success     bool
	ErrorCode   string
	ErrorDetail string
}

// SettleRequest contains the parameters for a settlement.
type SettleRequest struct {
	RequestID        string
	OrgID            int64
	ActualCost       int
	PromptTokens     int
	CompletionTokens int
	UsageSource      string
}

// SettleResult contains the result of a settlement.
type SettleResult struct {
	SettleID   int64
	Refund     int
	NewBalance int
	Success    bool
	ErrorCode  string
}

// Billing handles the freeze/settle/cancel state machine.
type Billing struct {
	db     *pgxpool.Pool
	rdb    *redis.Client
	prices PriceProvider
}

// NewBilling creates a new Billing instance.
func NewBilling(db *pgxpool.Pool, rdb *redis.Client) *Billing {
	return &Billing{db: db, rdb: rdb, prices: NewDBPriceProvider(db)}
}

// WithPriceProvider overrides the default DB-backed price provider (for testing).
func (b *Billing) WithPriceProvider(p PriceProvider) *Billing {
	b.prices = p
	return b
}

// QueryModelPrice returns the per-token cost in cents for a model.
// Falls back to a default rate if the model is not in model_prices.
func (b *Billing) QueryModelPrice(ctx context.Context, model string) (inputCost, outputCost float64, err error) {
	return b.prices.QueryModelPrice(ctx, model)
}

// QueryModelPriceForOrg returns per-token costs with org-level multiplier applied.
// Merges base price lookup and org multiplier into a single SQL query to reduce
// round-trips from 3 (PriceProvider + fallback + multiplier) to 1.
func (b *Billing) QueryModelPriceForOrg(ctx context.Context, orgID int64, model string) (inputCost, outputCost float64, err error) {
	var multiplier float64
	err = b.db.QueryRow(ctx, `
		SELECT
			COALESCE(mp.input_price_cents, 2)::float8,
			COALESCE(mp.output_price_cents, 10)::float8,
			COALESCE(omp.multiplier, 1.0)
		FROM (SELECT 1) AS stub
		LEFT JOIN model_prices mp ON mp.model_name = $1
		LEFT JOIN org_model_prices omp ON omp.org_id = $2 AND omp.model_name = $1
	`, model, orgID).Scan(&inputCost, &outputCost, &multiplier)
	if err != nil {
		return 0, 0, fmt.Errorf("query model price for org %d model %s: %w", orgID, model, err)
	}
	inputCost = inputCost * multiplier
	outputCost = outputCost * multiplier
	return
}

// CalculateCost calculates the cost of a request in cents.
func (b *Billing) CalculateCost(ctx context.Context, model string, promptTokens, completionTokens int) (int, error) {
	inputCost, outputCost, err := b.prices.QueryModelPrice(ctx, model)
	if err != nil {
		return 0, err
	}
	total := int((float64(promptTokens)*inputCost + float64(completionTokens)*outputCost) / 1000)
	if total < 1 {
		total = 1
	}
	return total, nil
}

// EstimateFreezeAmount calculates the freeze amount based on estimated tokens.
func (b *Billing) EstimateFreezeAmount(ctx context.Context, model string, estimatedTokens int, safetyFactor float64, minFreezeAmount int) (int, error) {
	_, outputCost, err := b.prices.QueryModelPrice(ctx, model)
	if err != nil {
		return 0, err
	}
	estimated := int(math.Round(float64(estimatedTokens) * outputCost / 1000.0 * safetyFactor))
	if estimated < minFreezeAmount {
		estimated = minFreezeAmount
	}
	return estimated, nil
}

// Freeze deducts balance (prioritizing granted_balance) and creates a freeze record.
// Wrapped in a single transaction for atomicity.
func (b *Billing) Freeze(ctx context.Context, req FreezeRequest) (*FreezeResult, error) {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Calculate freeze amount
	freezeAmount, err := b.EstimateFreezeAmount(ctx, req.Model, req.EstimatedTokens, req.SafetyFactor, req.MinFreezeAmount)
	if err != nil {
		return nil, fmt.Errorf("estimate freeze: %w", err)
	}

	// Check idempotency
	var existingAmount int
	err = tx.QueryRow(ctx,
		`SELECT frozen_amount FROM freeze_idempotency WHERE request_id = $1 AND status = 'frozen'`,
		req.RequestID).Scan(&existingAmount)
	if err == nil {
		return &FreezeResult{Amount: existingAmount, Success: true}, nil
	}

	// Get current balance
	var balance, grantedBalance, frozen int
	err = tx.QueryRow(ctx,
		`SELECT balance, granted_balance, frozen FROM wallets WHERE org_id = $1 FOR UPDATE`,
		req.OrgID).Scan(&balance, &grantedBalance, &frozen)
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}

	available := balance + grantedBalance - frozen
	if available < freezeAmount {
		return &FreezeResult{
			Success:     false,
			ErrorCode:   "insufficient_balance",
			ErrorDetail: fmt.Sprintf("available=%d, required=%d", available, freezeAmount),
		}, nil
	}

	// Deduct: prioritize granted_balance first
	newBalance := balance
	newGranted := grantedBalance
	remaining := freezeAmount

	if newGranted >= remaining {
		newGranted -= remaining
		remaining = 0
	} else {
		remaining -= newGranted
		newGranted = 0
		newBalance -= remaining
	}

	_, err = tx.Exec(ctx,
		`UPDATE wallets SET balance = $1, granted_balance = $2, frozen = frozen + $3, updated_at = NOW()
		 WHERE org_id = $4`,
		newBalance, newGranted, freezeAmount, req.OrgID)
	if err != nil {
		return nil, fmt.Errorf("update wallet: %w", err)
	}

	// Record transaction
	_, err = tx.Exec(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount, note, created_at)
		 VALUES ($1, $2, 'freeze', 'completed', $3, $4, NOW())`,
		req.RequestID, req.OrgID, freezeAmount, "Freeze for request "+req.RequestID)
	if err != nil {
		return nil, fmt.Errorf("record transaction: %w", err)
	}

	// Insert idempotency key
	_, err = tx.Exec(ctx,
		`INSERT INTO freeze_idempotency (request_id, org_id, frozen_amount, status)
		 VALUES ($1, $2, $3, 'frozen')`, req.RequestID, req.OrgID, freezeAmount)
	if err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			existing, lookupErr := b.lookupFrozenAmount(ctx, req.RequestID)
			if lookupErr == nil {
				return &FreezeResult{Amount: existing, Success: true}, nil
			}
		}
		return nil, fmt.Errorf("insert idempotency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &FreezeResult{
		FreezeID:   0,
		Amount:     freezeAmount,
		NewBalance: newBalance,
		Success:    true,
	}, nil
}

func (b *Billing) lookupFrozenAmount(ctx context.Context, requestID string) (int, error) {
	var existingAmount int
	err := b.db.QueryRow(ctx,
		`SELECT frozen_amount FROM freeze_idempotency WHERE request_id = $1 AND status = 'frozen'`,
		requestID).Scan(&existingAmount)
	return existingAmount, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Settle settles a frozen request with the actual cost.
// Wrapped in a single transaction for atomicity.
func (b *Billing) Settle(ctx context.Context, req SettleRequest) (*SettleResult, error) {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get the frozen amount
	var frozenAmount int
	err = tx.QueryRow(ctx,
		`SELECT frozen_amount FROM freeze_idempotency WHERE request_id = $1 AND status = 'frozen'`,
		req.RequestID).Scan(&frozenAmount)
	if err != nil {
		if err == pgx.ErrNoRows {
			return &SettleResult{Success: false, ErrorCode: "not_found"}, nil
		}
		return nil, fmt.Errorf("get frozen: %w", err)
	}

	// Calculate refund
	refund := frozenAmount - req.ActualCost
	if refund < 0 {
		refund = 0
	}

	// Settle: reduce frozen, refund excess to balance
	var newBalance int
	err = tx.QueryRow(ctx,
		`UPDATE wallets SET frozen = frozen - $1, balance = balance + $2, total_spent = total_spent + $4, updated_at = NOW()
		 WHERE org_id = $3
		 RETURNING balance`, frozenAmount, refund, req.OrgID, req.ActualCost).Scan(&newBalance)
	if err != nil {
		return nil, fmt.Errorf("settle balance: %w", err)
	}

	// Record settle transaction
	var settleID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount, note)
		 VALUES ($3, $1, 'settle', 'completed', $2, 'Settle for request ' || $4::text)
		 RETURNING id`,
		req.OrgID, req.ActualCost, req.RequestID, req.RequestID).Scan(&settleID)
	if err != nil {
		return nil, fmt.Errorf("record settle: %w", err)
	}

	// Update idempotency status
	_, err = tx.Exec(ctx,
		`UPDATE freeze_idempotency SET status = 'settled' WHERE request_id = $1`,
		req.RequestID)
	if err != nil {
		return nil, fmt.Errorf("update idempotency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// Log billing mismatch if actual cost exceeded frozen amount
	if req.ActualCost > frozenAmount {
		log.Printf("[billing] WARNING: request %s actual_cost=%d > frozen=%d, loss=%d cents",
			req.RequestID, req.ActualCost, frozenAmount, req.ActualCost-frozenAmount)
	}

	return &SettleResult{
		SettleID:   settleID,
		Refund:     refund,
		NewBalance: newBalance,
		Success:    true,
	}, nil
}

// Cancel cancels a frozen request and returns the frozen amount to balance.
// Wrapped in a single transaction for atomicity.
func (b *Billing) Cancel(ctx context.Context, requestID string, orgID int64) error {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var frozenAmount int
	err = tx.QueryRow(ctx,
		`SELECT frozen_amount FROM freeze_idempotency WHERE request_id = $1 AND status = 'frozen'`,
		requestID).Scan(&frozenAmount)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return fmt.Errorf("get frozen: %w", err)
	}

	// Unfreeze: restore to balance (conservative approach)
	_, err = tx.Exec(ctx,
		`UPDATE wallets SET frozen = frozen - $1, balance = balance + $1, updated_at = NOW()
		 WHERE org_id = $2`, frozenAmount, orgID)
	if err != nil {
		return fmt.Errorf("unfreeze balance: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE freeze_idempotency SET status = 'cancelled' WHERE request_id = $1`,
		requestID)
	if err != nil {
		return fmt.Errorf("update idempotency: %w", err)
	}

	return tx.Commit(ctx)
}
