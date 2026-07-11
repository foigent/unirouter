package billing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	internaldb "github.com/uniroute/unirouter/internal/db"
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
	FreezeID       int64
	Amount         int
	NewBalance     int
	PricingVersion string
	Success        bool
	ErrorCode      string
	ErrorDetail    string
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
	SettleID       int64
	ActualCost     int
	ChargedCost    int
	UncoveredCost  int
	Refund         int
	NewBalance     int
	PricingVersion string
	Success        bool
	ErrorCode      string
}

type priceQuote struct {
	InputPriceCents  float64
	OutputPriceCents float64
	Multiplier       float64
	Version          string
}

type reservationRecord struct {
	FrozenAmount          int
	ReservedBalanceAmount int
	ReservedGrantedAmount int
	EffectiveInputPrice   float64
	EffectiveOutputPrice  float64
	PricingVersion        string
	Status                string
	PromptTokens          int
	CompletionTokens      int
	UsageSource           string
	SettlementPayloadAt   sql.NullTime
	SettlementAttempts    int
	CalculatedCost        int
	ChargedCost           int
	UncoveredCost         int
	RefundAmount          int
	TerminalTransactionID sql.NullInt64
	BalanceAfter          sql.NullInt64
}

var ErrSettlementPending = errors.New("billing reservation has billable output pending settlement")
var ErrSettlementUsageNotReady = errors.New("billing settlement usage is not durably prepared")
var ErrSettlementUsageConflict = errors.New("billing settlement usage conflicts with the durable payload")

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
	quote, err := b.queryPriceQuoteForOrg(ctx, orgID, model)
	if err != nil {
		return 0, 0, err
	}
	return quote.InputPriceCents, quote.OutputPriceCents, nil
}

// queryPriceQuoteForOrg resolves pricing before a wallet transaction begins.
// Production uses one database round trip for the base price, organization
// multiplier and their versions. Tests may inject a PriceProvider; injected
// prices are treated as already effective with multiplier 1.
func (b *Billing) queryPriceQuoteForOrg(ctx context.Context, orgID int64, model string) (priceQuote, error) {
	if _, ok := b.prices.(*DBPriceProvider); !ok {
		inputCost, outputCost, err := b.prices.QueryModelPrice(ctx, model)
		if err != nil {
			return priceQuote{}, err
		}
		return newPriceQuote(model, inputCost, outputCost, 1, time.Time{}, time.Time{}, "injected"), nil
	}

	var baseInput, baseOutput, multiplier float64
	var baseUpdatedAt, orgUpdatedAt sql.NullTime
	err := b.db.QueryRow(ctx, `
		SELECT
			COALESCE(mp.input_price_cents, 2)::float8,
			COALESCE(mp.output_price_cents, 10)::float8,
			COALESCE(omp.multiplier, 1.0)::float8,
			mp.updated_at,
			omp.updated_at
		FROM (SELECT 1) AS stub
		LEFT JOIN model_prices mp ON mp.model_name = $1
		LEFT JOIN org_model_prices omp ON omp.org_id = $2 AND omp.model_name = $1
	`, model, orgID).Scan(&baseInput, &baseOutput, &multiplier, &baseUpdatedAt, &orgUpdatedAt)
	if err != nil {
		return priceQuote{}, fmt.Errorf("query model price for org %d model %s: %w", orgID, model, err)
	}
	return newPriceQuote(model, baseInput, baseOutput, multiplier, nullableTime(baseUpdatedAt), nullableTime(orgUpdatedAt), "database"), nil
}

func nullableTime(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func newPriceQuote(model string, baseInput, baseOutput, multiplier float64, baseUpdatedAt, orgUpdatedAt time.Time, source string) priceQuote {
	canonical := fmt.Sprintf(
		"pv1|%s|%.6f|%.6f|%.6f|%d|%d|%s",
		model,
		baseInput,
		baseOutput,
		multiplier,
		baseUpdatedAt.UnixNano(),
		orgUpdatedAt.UnixNano(),
		source,
	)
	digest := sha256.Sum256([]byte(canonical))
	return priceQuote{
		InputPriceCents:  baseInput * multiplier,
		OutputPriceCents: baseOutput * multiplier,
		Multiplier:       multiplier,
		Version:          "pv1_" + hex.EncodeToString(digest[:]),
	}
}

func estimateFreezeAmountFromQuote(quote priceQuote, estimatedTokens int, safetyFactor float64, minFreezeAmount int) int {
	estimated := int(math.Round(float64(estimatedTokens) * quote.OutputPriceCents / 1000.0 * safetyFactor))
	if estimated < minFreezeAmount {
		estimated = minFreezeAmount
	}
	return estimated
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
	// Resolve the effective price before acquiring a transaction connection. A
	// one-connection pool must not deadlock by trying to query through the pool
	// while its only connection is already held by tx.
	quote, err := b.queryPriceQuoteForOrg(ctx, req.OrgID, req.Model)
	if err != nil {
		return nil, fmt.Errorf("quote freeze price: %w", err)
	}
	freezeAmount := estimateFreezeAmountFromQuote(quote, req.EstimatedTokens, req.SafetyFactor, req.MinFreezeAmount)

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	// Serialize only the same tenant-operation pair before reading its receipt.
	// Without this short transaction-scoped lock, a concurrent duplicate can
	// miss the uncommitted row, wait for the wallet, then incorrectly report
	// insufficient funds after the first caller used the exact available amount.
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`,
		fmt.Sprintf("%d:%s", req.OrgID, req.RequestID),
	); err != nil {
		return nil, fmt.Errorf("lock reservation identity: %w", err)
	}

	// Check the durable reservation receipt, regardless of terminal state. A
	// server operation ID cannot be silently reused after settlement/cancel.
	var existing reservationRecord
	err = tx.QueryRow(ctx,
		`SELECT frozen_amount, status, pricing_version
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2`,
		req.RequestID, req.OrgID,
	).Scan(&existing.FrozenAmount, &existing.Status, &existing.PricingVersion)
	if err == nil {
		if existing.Status == "frozen" {
			return &FreezeResult{
				Amount:         existing.FrozenAmount,
				PricingVersion: existing.PricingVersion,
				Success:        true,
			}, nil
		}
		return &FreezeResult{
			Success:     false,
			ErrorCode:   "request_terminal",
			ErrorDetail: "the operation already reached state " + existing.Status,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("check reservation: %w", err)
	}

	// Get current balance
	var balance, grantedBalance, frozen int
	err = tx.QueryRow(ctx,
		`SELECT balance, granted_balance, frozen FROM wallets WHERE org_id = $1 FOR UPDATE`,
		req.OrgID).Scan(&balance, &grantedBalance, &frozen)
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	_, expiredGrantBalance, err := expireGrantRowsInLockedWallet(ctx, tx, req.OrgID)
	if err != nil {
		return nil, err
	}
	grantedBalance -= minInt(grantedBalance, expiredGrantBalance)

	// balance and granted_balance are already debited when a reservation is
	// created. frozen is a held-funds control total, not another deduction from
	// spendable funds.
	available := balance + grantedBalance
	if available < freezeAmount {
		// Persist any lazy grant expiry performed above even though no
		// reservation is created.
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit expired grant reconciliation: %w", err)
		}
		return &FreezeResult{
			Success:     false,
			ErrorCode:   "insufficient_balance",
			ErrorDetail: fmt.Sprintf("available=%d, required=%d", available, freezeAmount),
		}, nil
	}

	// Deduct: prioritize granted_balance first, then cash. Grant-row balances
	// are reduced in earliest-expiry order so the aggregate wallet and grant
	// ledger cannot later be charged a second time by expiry cleanup.
	reservedGranted := minInt(grantedBalance, freezeAmount)
	reservedBalance := freezeAmount - reservedGranted
	newGranted := grantedBalance - reservedGranted
	newBalance := balance - reservedBalance
	grantAllocations, err := reserveGrantRows(ctx, tx, req.OrgID, reservedGranted)
	if err != nil {
		return nil, err
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

	// Insert the self-contained reservation and its immutable price snapshot.
	_, err = tx.Exec(ctx,
		`INSERT INTO freeze_idempotency (
			request_id, org_id, frozen_amount, status,
			reserved_balance_amount, reserved_granted_amount,
			model_name, estimated_tokens, safety_factor, min_freeze_amount,
			effective_input_price_cents, effective_output_price_cents,
			price_multiplier, pricing_version, updated_at
		 ) VALUES (
			$1, $2, $3, 'frozen',
			$4, $5, $6, $7, $8, $9,
			$10, $11, $12, $13, NOW()
		 )`,
		req.RequestID, req.OrgID, freezeAmount,
		reservedBalance, reservedGranted,
		req.Model, req.EstimatedTokens, req.SafetyFactor, req.MinFreezeAmount,
		quote.InputPriceCents, quote.OutputPriceCents, quote.Multiplier, quote.Version,
	)
	if err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			record, lookupErr := b.lookupReservation(ctx, req.RequestID, req.OrgID)
			if lookupErr == nil && record.Status == "frozen" {
				return &FreezeResult{
					Amount:         record.FrozenAmount,
					PricingVersion: record.PricingVersion,
					Success:        true,
				}, nil
			}
		}
		return nil, fmt.Errorf("insert idempotency: %w", err)
	}

	for _, allocation := range grantAllocations {
		_, err = tx.Exec(ctx,
			`INSERT INTO billing_reservation_grant_allocations (
				org_id, request_id, grant_id, reserved_amount
			 ) VALUES ($1, $2, $3, $4)`,
			req.OrgID, req.RequestID, allocation.GrantID, allocation.Amount,
		)
		if err != nil {
			return nil, fmt.Errorf("record grant reservation allocation: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &FreezeResult{
		FreezeID:       0,
		Amount:         freezeAmount,
		NewBalance:     newBalance,
		PricingVersion: quote.Version,
		Success:        true,
	}, nil
}

type grantReservationAllocation struct {
	GrantID int64
	Amount  int
}

func reserveGrantRows(ctx context.Context, tx pgx.Tx, orgID int64, requested int) ([]grantReservationAllocation, error) {
	if requested <= 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx,
		`SELECT id, balance
		 FROM grants
		 WHERE org_id = $1
		   AND status = 'active'
		   AND balance > 0
		   AND (expires_at IS NULL OR expires_at > NOW())
		 ORDER BY expires_at ASC NULLS LAST, created_at ASC, id ASC
		 FOR UPDATE`, orgID)
	if err != nil {
		return nil, fmt.Errorf("lock grant balances: %w", err)
	}
	defer rows.Close()

	remaining := requested
	allocations := make([]grantReservationAllocation, 0)
	for rows.Next() && remaining > 0 {
		var grantID int64
		var balance int
		if err := rows.Scan(&grantID, &balance); err != nil {
			return nil, fmt.Errorf("scan grant balance: %w", err)
		}
		amount := minInt(balance, remaining)
		allocations = append(allocations, grantReservationAllocation{GrantID: grantID, Amount: amount})
		remaining -= amount
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grant balances: %w", err)
	}
	rows.Close()
	for _, allocation := range allocations {
		if _, err := tx.Exec(ctx,
			`UPDATE grants SET balance = balance - $1, updated_at = NOW() WHERE id = $2`,
			allocation.Amount, allocation.GrantID,
		); err != nil {
			return nil, fmt.Errorf("reserve grant balance: %w", err)
		}
	}
	// Any remaining aggregate grant balance is retained as an unallocated
	// legacy pool. It is still source-labelled as grant and can never be
	// converted to cash on refund.
	return allocations, nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (b *Billing) lookupReservation(ctx context.Context, requestID string, orgID int64) (reservationRecord, error) {
	var record reservationRecord
	err := b.db.QueryRow(ctx,
		`SELECT frozen_amount, reserved_balance_amount, reserved_granted_amount,
		        effective_input_price_cents::float8, effective_output_price_cents::float8,
		        pricing_version, status, prompt_tokens, completion_tokens, usage_source,
		        settlement_payload_ready_at, settlement_attempts,
		        calculated_cost, charged_cost, uncovered_cost,
		        refund_amount, terminal_transaction_id, balance_after
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2`,
		requestID, orgID,
	).Scan(
		&record.FrozenAmount,
		&record.ReservedBalanceAmount,
		&record.ReservedGrantedAmount,
		&record.EffectiveInputPrice,
		&record.EffectiveOutputPrice,
		&record.PricingVersion,
		&record.Status,
		&record.PromptTokens,
		&record.CompletionTokens,
		&record.UsageSource,
		&record.SettlementPayloadAt,
		&record.SettlementAttempts,
		&record.CalculatedCost,
		&record.ChargedCost,
		&record.UncoveredCost,
		&record.RefundAmount,
		&record.TerminalTransactionID,
		&record.BalanceAfter,
	)
	return record, err
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

	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &SettleResult{Success: false, ErrorCode: "not_found"}, nil
		}
		return nil, fmt.Errorf("lock reservation: %w", err)
	}

	switch record.Status {
	case "settled":
		return settleResultFromRecord(record), nil
	case "cancelled":
		return &SettleResult{Success: false, ErrorCode: "already_cancelled"}, nil
	case "settlement_pending":
		if !record.SettlementPayloadAt.Valid {
			return nil, ErrSettlementUsageNotReady
		}
		if req.PromptTokens != record.PromptTokens ||
			req.CompletionTokens != record.CompletionTokens ||
			req.UsageSource != record.UsageSource {
			return nil, ErrSettlementUsageConflict
		}
		// Only the durable payload may drive recovery of billable output.
		req.PromptTokens = record.PromptTokens
		req.CompletionTokens = record.CompletionTokens
		req.UsageSource = record.UsageSource
	case "frozen":
		// Non-stream responses are withheld until this transaction succeeds, so
		// they can still settle directly from the caller-provided usage.
	default:
		return &SettleResult{Success: false, ErrorCode: "invalid_state"}, nil
	}

	calculatedCost, err := calculateReservationCost(record, req)
	if err != nil {
		return nil, err
	}
	chargedCost := minInt(calculatedCost, record.FrozenAmount)
	uncoveredCost := calculatedCost - chargedCost
	grantConsumed := minInt(chargedCost, record.ReservedGrantedAmount)
	cashConsumed := chargedCost - grantConsumed
	refundCash := record.ReservedBalanceAmount - cashConsumed
	if refundCash < 0 {
		return nil, fmt.Errorf("reservation cash source invariant violated for %s", req.RequestID)
	}
	if err := lockWalletForBilling(ctx, tx, req.OrgID); err != nil {
		return nil, err
	}

	restoredGrant, forfeitedGrant, err := finalizeGrantAllocations(
		ctx,
		tx,
		req.OrgID,
		req.RequestID,
		record.ReservedGrantedAmount,
		grantConsumed,
	)
	if err != nil {
		return nil, err
	}
	refund := refundCash + restoredGrant

	// The wallet row is updated once while the reservation row lock guarantees
	// that mixed Settle/Cancel callers cannot both produce terminal effects.
	var newBalance int
	err = tx.QueryRow(ctx,
		`UPDATE wallets
		 SET frozen = frozen - $1,
		     balance = balance + $2,
		     granted_balance = granted_balance + $3,
		     total_spent = total_spent + $4,
		     updated_at = NOW()
		 WHERE org_id = $5 AND frozen >= $1
		 RETURNING balance`,
		record.FrozenAmount, refundCash, restoredGrant, chargedCost, req.OrgID,
	).Scan(&newBalance)
	if err != nil {
		return nil, fmt.Errorf("settle balance: %w", err)
	}

	// Record settle transaction
	var settleID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount, balance_after, note)
		 VALUES ($1, $2, 'settle', 'completed', $3, $4, $5)
		 RETURNING id`,
		req.RequestID,
		req.OrgID,
		chargedCost,
		newBalance,
		fmt.Sprintf("Settle for request %s (uncovered=%d)", req.RequestID, uncoveredCost),
	).Scan(&settleID)
	if err != nil {
		return nil, fmt.Errorf("record settle: %w", err)
	}

	// Update idempotency status
	_, err = tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET status = 'settled',
		     prompt_tokens = $3,
		     completion_tokens = $4,
		     usage_source = $5,
		     calculated_cost = $6,
		     charged_cost = $7,
		     uncovered_cost = $8,
		     refund_amount = $9,
		     forfeited_grant_amount = $10,
		     terminal_transaction_id = $11,
		     balance_after = $12,
		     terminal_at = NOW(),
		     settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NULL,
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2 AND status IN ('frozen', 'settlement_pending')`,
		req.RequestID, req.OrgID,
		req.PromptTokens, req.CompletionTokens, req.UsageSource,
		calculatedCost, chargedCost, uncoveredCost, refund, forfeitedGrant,
		settleID, newBalance,
	)
	if err != nil {
		return nil, fmt.Errorf("update idempotency: %w", err)
	}

	// A recovered stream may already have a best-effort call log with zero
	// cost. Converge it to the durable settlement receipt in the same
	// transaction when that row exists. The normal request path may insert the
	// log after settlement, in which case this update intentionally affects 0.
	if record.SettlementAttempts > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE call_logs
			 SET prompt_tokens = $3,
			     completion_tokens = $4,
			     total_tokens = $3::integer + $4::integer,
			     cost_cents = $5,
			     usage_source = $6
			 WHERE request_id = $1 AND org_id = $2`,
			req.RequestID, req.OrgID, req.PromptTokens, req.CompletionTokens,
			chargedCost, req.UsageSource,
		); err != nil {
			return nil, fmt.Errorf("converge recovered call log: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	if uncoveredCost > 0 {
		log.Printf("[billing] WARNING: request %s calculated_cost=%d > frozen=%d, uncovered=%d cents",
			req.RequestID, calculatedCost, record.FrozenAmount, uncoveredCost)
	}

	return &SettleResult{
		SettleID:       settleID,
		ActualCost:     calculatedCost,
		ChargedCost:    chargedCost,
		UncoveredCost:  uncoveredCost,
		Refund:         refund,
		NewBalance:     newBalance,
		PricingVersion: record.PricingVersion,
		Success:        true,
	}, nil
}

func calculateReservationCost(record reservationRecord, req SettleRequest) (int, error) {
	if req.PromptTokens < 0 || req.CompletionTokens < 0 || req.ActualCost < 0 {
		return 0, fmt.Errorf("billing usage values must be nonnegative")
	}
	if req.PromptTokens > 0 || req.CompletionTokens > 0 {
		if record.EffectiveInputPrice > 0 || record.EffectiveOutputPrice > 0 {
			cost := int((float64(req.PromptTokens)*record.EffectiveInputPrice +
				float64(req.CompletionTokens)*record.EffectiveOutputPrice) / 1000)
			if cost < 1 {
				cost = 1
			}
			return cost, nil
		}
	}
	return req.ActualCost, nil
}

func settleResultFromRecord(record reservationRecord) *SettleResult {
	result := &SettleResult{
		ActualCost:     record.CalculatedCost,
		ChargedCost:    record.ChargedCost,
		UncoveredCost:  record.UncoveredCost,
		Refund:         record.RefundAmount,
		PricingVersion: record.PricingVersion,
		Success:        true,
	}
	if record.TerminalTransactionID.Valid {
		result.SettleID = record.TerminalTransactionID.Int64
	}
	if record.BalanceAfter.Valid {
		result.NewBalance = int(record.BalanceAfter.Int64)
	}
	return result
}

func reservationForUpdate(ctx context.Context, tx pgx.Tx, requestID string, orgID int64) (reservationRecord, error) {
	var record reservationRecord
	err := tx.QueryRow(ctx,
		`SELECT frozen_amount, reserved_balance_amount, reserved_granted_amount,
		        effective_input_price_cents::float8, effective_output_price_cents::float8,
		        pricing_version, status, prompt_tokens, completion_tokens, usage_source,
		        settlement_payload_ready_at, settlement_attempts,
		        calculated_cost, charged_cost, uncovered_cost,
		        refund_amount, terminal_transaction_id, balance_after
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2
		 FOR UPDATE`, requestID, orgID,
	).Scan(
		&record.FrozenAmount,
		&record.ReservedBalanceAmount,
		&record.ReservedGrantedAmount,
		&record.EffectiveInputPrice,
		&record.EffectiveOutputPrice,
		&record.PricingVersion,
		&record.Status,
		&record.PromptTokens,
		&record.CompletionTokens,
		&record.UsageSource,
		&record.SettlementPayloadAt,
		&record.SettlementAttempts,
		&record.CalculatedCost,
		&record.ChargedCost,
		&record.UncoveredCost,
		&record.RefundAmount,
		&record.TerminalTransactionID,
		&record.BalanceAfter,
	)
	return record, err
}

func lockWalletForBilling(ctx context.Context, tx pgx.Tx, orgID int64) error {
	var lockedOrgID int64
	if err := tx.QueryRow(ctx,
		`SELECT org_id FROM wallets WHERE org_id = $1 FOR UPDATE`, orgID,
	).Scan(&lockedOrgID); err != nil {
		return fmt.Errorf("lock wallet: %w", err)
	}
	return nil
}

func finalizeGrantAllocations(ctx context.Context, tx pgx.Tx, orgID int64, requestID string, reservedGrant, grantConsumed int) (restored, forfeited int, err error) {
	rows, err := tx.Query(ctx,
		`SELECT a.grant_id, a.reserved_amount,
		        (g.status = 'active' AND (g.expires_at IS NULL OR g.expires_at > NOW())) AS refundable
		 FROM billing_reservation_grant_allocations a
		 JOIN grants g ON g.id = a.grant_id
		 WHERE a.org_id = $1 AND a.request_id = $2
		 ORDER BY g.expires_at ASC NULLS LAST, g.created_at ASC, g.id ASC
		 FOR UPDATE OF a, g`, orgID, requestID)
	if err != nil {
		return 0, 0, fmt.Errorf("lock reservation grant allocations: %w", err)
	}
	defer rows.Close()

	type allocation struct {
		grantID    int64
		reserved   int
		refundable bool
	}
	allocations := make([]allocation, 0)
	totalAllocated := 0
	for rows.Next() {
		var item allocation
		if err := rows.Scan(&item.grantID, &item.reserved, &item.refundable); err != nil {
			return 0, 0, fmt.Errorf("scan reservation grant allocation: %w", err)
		}
		allocations = append(allocations, item)
		totalAllocated += item.reserved
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("iterate reservation grant allocations: %w", err)
	}
	rows.Close()
	if totalAllocated > reservedGrant {
		return 0, 0, fmt.Errorf("grant allocation invariant violated for %s", requestID)
	}

	remainingConsumed := grantConsumed
	for _, item := range allocations {
		consumed := minInt(item.reserved, remainingConsumed)
		remainingConsumed -= consumed
		refundCandidate := item.reserved - consumed
		refunded := 0
		if refundCandidate > 0 {
			if item.refundable {
				if _, err := tx.Exec(ctx,
					`UPDATE grants
					 SET balance = balance + $1, updated_at = NOW()
					 WHERE id = $2
					   AND status = 'active'
					   AND (expires_at IS NULL OR expires_at > NOW())`,
					refundCandidate, item.grantID,
				); err != nil {
					return 0, 0, fmt.Errorf("refund grant allocation: %w", err)
				}
				refunded = refundCandidate
				restored += refunded
			} else {
				forfeited += refundCandidate
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE billing_reservation_grant_allocations
			 SET consumed_amount = $1, refunded_amount = $2, updated_at = NOW()
			 WHERE org_id = $3 AND request_id = $4 AND grant_id = $5`,
			consumed, refunded, orgID, requestID, item.grantID,
		); err != nil {
			return 0, 0, fmt.Errorf("finalize grant allocation: %w", err)
		}
	}

	unallocated := reservedGrant - totalAllocated
	unallocatedConsumed := minInt(unallocated, remainingConsumed)
	remainingConsumed -= unallocatedConsumed
	if remainingConsumed != 0 {
		return 0, 0, fmt.Errorf("grant consumption invariant violated for %s", requestID)
	}
	restored += unallocated - unallocatedConsumed
	return restored, forfeited, nil
}

// Cancel cancels a pre-output frozen request and returns funds to the same
// source buckets. settlement_pending is deliberately non-cancellable because
// it represents billable output and must be recovered by settlement.
func (b *Billing) Cancel(ctx context.Context, requestID string, orgID int64) error {
	return CancelReservation(ctx, b.db, requestID, orgID)
}

// CancelReservation exposes the source-preserving terminal transition to
// bounded cleanup jobs that depend on the DBQuerier interface.
func CancelReservation(ctx context.Context, database internaldb.DBQuerier, requestID string, orgID int64) error {
	tx, err := database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	record, err := reservationForUpdate(ctx, tx, requestID, orgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lock reservation: %w", err)
	}
	switch record.Status {
	case "cancelled", "settled":
		return nil
	case "settlement_pending":
		return ErrSettlementPending
	case "frozen":
		// Continue below.
	default:
		return fmt.Errorf("cannot cancel billing reservation in state %s", record.Status)
	}
	if err := lockWalletForBilling(ctx, tx, orgID); err != nil {
		return err
	}

	restoredGrant, forfeitedGrant, err := finalizeGrantAllocations(
		ctx, tx, orgID, requestID, record.ReservedGrantedAmount, 0,
	)
	if err != nil {
		return err
	}
	refund := record.ReservedBalanceAmount + restoredGrant

	var newBalance int
	err = tx.QueryRow(ctx,
		`UPDATE wallets
		 SET frozen = frozen - $1,
		     balance = balance + $2,
		     granted_balance = granted_balance + $3,
		     updated_at = NOW()
		 WHERE org_id = $4 AND frozen >= $1
		 RETURNING balance`,
		record.FrozenAmount, record.ReservedBalanceAmount, restoredGrant, orgID,
	).Scan(&newBalance)
	if err != nil {
		return fmt.Errorf("unfreeze balance: %w", err)
	}

	var cancelID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount, balance_after, note)
		 VALUES ($1, $2, 'cancel', 'completed', $3, $4, $5)
		 RETURNING id`, requestID, orgID, refund, newBalance,
		"Cancel reservation for request "+requestID,
	).Scan(&cancelID)
	if err != nil {
		return fmt.Errorf("record cancel: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET status = 'cancelled',
		     refund_amount = $3,
		     forfeited_grant_amount = $4,
		     terminal_transaction_id = $5,
		     balance_after = $6,
		     terminal_at = NOW(),
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2 AND status = 'frozen'`,
		requestID, orgID, refund, forfeitedGrant, cancelID, newBalance,
	)
	if err != nil {
		return fmt.Errorf("update idempotency: %w", err)
	}

	return tx.Commit(ctx)
}
