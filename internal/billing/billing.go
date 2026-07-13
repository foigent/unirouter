package billing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	internaldb "github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/outbox"
)

// FreezeRequest contains the parameters for a freeze operation.
type FreezeRequest struct {
	RequestID                string
	OrgID                    int64
	ProjectID                int64
	Model                    string
	EstimatedTokens          int
	EstimatedInputTokens     int
	ReservedCompletionTokens int
	SafetyFactor             float64
	MinFreezeAmount          int
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
	RequestID            string
	AttemptID            string
	OrgID                int64
	ActualCost           int
	PromptTokens         int
	UncachedPromptTokens int
	CacheReadTokens      int
	CacheCreationTokens  int
	CompletionTokens     int
	UsageSource          string
	CacheUsageSchema     string
	CacheUsageStatus     string
}

// SettleResult contains the result of a settlement.
type SettleResult struct {
	SettleID                         int64
	ActualCost                       int
	ChargedCost                      int
	UncoveredCost                    int
	TopUpAmount                      int
	Refund                           int
	NewBalance                       int
	PricingVersion                   string
	EffectiveInputPriceCents         float64
	EffectiveCacheReadPriceCents     float64
	EffectiveCacheCreationPriceCents float64
	EffectiveOutputPriceCents        float64
	CacheReadRatio                   float64
	CacheCreationRatio               float64
	UncachedInputCostCents           float64
	CacheReadCostCents               float64
	CacheCreationCostCents           float64
	OutputCostCents                  float64
	Success                          bool
	ErrorCode                        string
}

type priceQuote struct {
	InputPriceCents         float64
	CacheReadPriceCents     float64
	CacheCreationPriceCents float64
	OutputPriceCents        float64
	CacheReadRatio          float64
	CacheCreationRatio      float64
	Multiplier              float64
	Version                 string
}

type reservationRecord struct {
	Model                       string
	ProjectID                   int64
	FrozenAmount                int
	ReservedBalanceAmount       int
	ReservedGrantedAmount       int
	EffectiveInputPrice         float64
	EffectiveCacheReadPrice     float64
	EffectiveCacheCreationPrice float64
	EffectiveOutputPrice        float64
	CacheReadRatio              float64
	CacheCreationRatio          float64
	PricingVersion              string
	Status                      string
	PromptTokens                int
	UncachedPromptTokens        int
	CacheReadTokens             int
	CacheCreationTokens         int
	CompletionTokens            int
	UsageSource                 string
	CacheUsageSchema            string
	CacheUsageStatus            string
	OutputStartedAt             sql.NullTime
	SettlementPayloadAt         sql.NullTime
	SettlementAttempts          int
	CalculatedCost              int
	ChargedCost                 int
	UncoveredCost               int
	TopUpAmount                 int
	RefundAmount                int
	UncachedInputCost           float64
	CacheReadCost               float64
	CacheCreationCost           float64
	OutputCost                  float64
	TerminalTransactionID       sql.NullInt64
	BalanceAfter                sql.NullInt64
}

var ErrSettlementPending = errors.New("billing reservation has billable output pending settlement")
var ErrSettlementUsageNotReady = errors.New("billing settlement usage is not durably prepared")
var ErrSettlementUsageConflict = errors.New("billing settlement usage conflicts with the durable payload")
var ErrSettlementFundingPending = errors.New("billing settlement requires additional prepaid funds")

const (
	cacheUsageStatusNotReported = "not_reported"
	maxPersistedTokenCount      = 1<<31 - 1
)

// Billing handles the freeze/settle/cancel state machine.
type Billing struct {
	db     *pgxpool.Pool
	rdb    *redis.Client
	prices PriceProvider
	outbox *outbox.Service
}

// NewBilling creates a new Billing instance.
func NewBilling(db *pgxpool.Pool, rdb *redis.Client) *Billing {
	return &Billing{db: db, rdb: rdb, prices: NewDBPriceProvider(db)}
}

// SetOutbox enables the repository-local P6-F20 durable evidence path. The
// financial transaction fails closed if its transactional envelope cannot be
// enqueued; callers that do not configure an outbox retain legacy test wiring.
func (b *Billing) SetOutbox(service *outbox.Service) {
	if b != nil {
		b.outbox = service
	}
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
		return newPriceQuote(model, inputCost, outputCost, 1, 1, 1, time.Time{}, time.Time{}, "injected"), nil
	}

	var baseInput, baseOutput, cacheReadRatio, cacheCreationRatio, multiplier float64
	var baseUpdatedAt, orgUpdatedAt sql.NullTime
	err := b.db.QueryRow(ctx, `
		SELECT
			COALESCE(mp.input_price_cents, 2)::float8,
			COALESCE(mp.output_price_cents, 10)::float8,
			COALESCE(mp.cache_read_ratio, 1)::float8,
			COALESCE(mp.cache_creation_ratio, 1)::float8,
			COALESCE(omp.multiplier, 1.0)::float8,
			mp.updated_at,
			omp.updated_at
		FROM (SELECT 1) AS stub
		LEFT JOIN model_prices mp ON mp.model_name = $1
		LEFT JOIN org_model_prices omp ON omp.org_id = $2 AND omp.model_name = $1
	`, model, orgID).Scan(&baseInput, &baseOutput, &cacheReadRatio, &cacheCreationRatio, &multiplier, &baseUpdatedAt, &orgUpdatedAt)
	if err != nil {
		return priceQuote{}, fmt.Errorf("query model price for org %d model %s: %w", orgID, model, err)
	}
	return newPriceQuote(model, baseInput, baseOutput, cacheReadRatio, cacheCreationRatio, multiplier, nullableTime(baseUpdatedAt), nullableTime(orgUpdatedAt), "database"), nil
}

func nullableTime(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func newPriceQuote(model string, baseInput, baseOutput, cacheReadRatio, cacheCreationRatio, multiplier float64, baseUpdatedAt, orgUpdatedAt time.Time, source string) priceQuote {
	canonical := fmt.Sprintf(
		"pv2|%s|%.6f|%.6f|%.6f|%.6f|%.6f|%d|%d|%s",
		model,
		baseInput,
		baseOutput,
		cacheReadRatio,
		cacheCreationRatio,
		multiplier,
		baseUpdatedAt.UnixNano(),
		orgUpdatedAt.UnixNano(),
		source,
	)
	digest := sha256.Sum256([]byte(canonical))
	return priceQuote{
		InputPriceCents:         baseInput * multiplier,
		CacheReadPriceCents:     baseInput * cacheReadRatio * multiplier,
		CacheCreationPriceCents: baseInput * cacheCreationRatio * multiplier,
		OutputPriceCents:        baseOutput * multiplier,
		CacheReadRatio:          cacheReadRatio,
		CacheCreationRatio:      cacheCreationRatio,
		Multiplier:              multiplier,
		Version:                 "pv2_" + hex.EncodeToString(digest[:]),
	}
}

func estimateFreezeAmountFromQuote(quote priceQuote, estimatedTokens int, safetyFactor float64, minFreezeAmount int) int {
	reservationPrice := math.Max(
		math.Max(quote.InputPriceCents, quote.OutputPriceCents),
		math.Max(quote.CacheReadPriceCents, quote.CacheCreationPriceCents),
	)
	estimated := int(math.Round(float64(estimatedTokens) * reservationPrice / 1000.0 * safetyFactor))
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
			project_id,
			reserved_balance_amount, reserved_granted_amount,
			model_name, estimated_tokens, safety_factor, min_freeze_amount,
			effective_input_price_cents, effective_cache_read_price_cents,
			effective_cache_creation_price_cents, effective_output_price_cents,
			cache_read_ratio, cache_creation_ratio,
			price_multiplier, pricing_version, updated_at
		 ) VALUES (
			$1, $2, $3, 'frozen',
			$4,
			$5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, NOW()
		 )`,
		req.RequestID, req.OrgID, freezeAmount,
		req.ProjectID,
		reservedBalance, reservedGranted,
		req.Model, req.EstimatedTokens, req.SafetyFactor, req.MinFreezeAmount,
		quote.InputPriceCents, quote.CacheReadPriceCents, quote.CacheCreationPriceCents,
		quote.OutputPriceCents, quote.CacheReadRatio, quote.CacheCreationRatio,
		quote.Multiplier, quote.Version,
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
		        effective_input_price_cents::float8,
		        effective_cache_read_price_cents::float8,
		        effective_cache_creation_price_cents::float8,
		        effective_output_price_cents::float8,
		        cache_read_ratio::float8, cache_creation_ratio::float8,
		        pricing_version, status,
		        prompt_tokens, uncached_prompt_tokens, cache_read_tokens, cache_creation_tokens,
		        completion_tokens, usage_source, cache_usage_schema, cache_usage_status,
		        output_started_at,
		        settlement_payload_ready_at, settlement_attempts,
		        calculated_cost, charged_cost, uncovered_cost, topup_amount,
		        refund_amount,
		        uncached_input_cost_cents::float8, cache_read_cost_cents::float8,
		        cache_creation_cost_cents::float8, output_cost_cents::float8,
		        terminal_transaction_id, balance_after
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2`,
		requestID, orgID,
	).Scan(
		&record.FrozenAmount,
		&record.ReservedBalanceAmount,
		&record.ReservedGrantedAmount,
		&record.EffectiveInputPrice,
		&record.EffectiveCacheReadPrice,
		&record.EffectiveCacheCreationPrice,
		&record.EffectiveOutputPrice,
		&record.CacheReadRatio,
		&record.CacheCreationRatio,
		&record.PricingVersion,
		&record.Status,
		&record.PromptTokens,
		&record.UncachedPromptTokens,
		&record.CacheReadTokens,
		&record.CacheCreationTokens,
		&record.CompletionTokens,
		&record.UsageSource,
		&record.CacheUsageSchema,
		&record.CacheUsageStatus,
		&record.OutputStartedAt,
		&record.SettlementPayloadAt,
		&record.SettlementAttempts,
		&record.CalculatedCost,
		&record.ChargedCost,
		&record.UncoveredCost,
		&record.TopUpAmount,
		&record.RefundAmount,
		&record.UncachedInputCost,
		&record.CacheReadCost,
		&record.CacheCreationCost,
		&record.OutputCost,
		&record.TerminalTransactionID,
		&record.BalanceAfter,
	)
	return record, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func normalizeSettlementUsage(req SettleRequest) (SettleRequest, error) {
	req.AttemptID = strings.TrimSpace(req.AttemptID)
	if len(req.AttemptID) > 64 {
		return req, fmt.Errorf("billing attempt_id exceeds 64 characters")
	}
	values := map[string]int{
		"prompt_tokens":          req.PromptTokens,
		"uncached_prompt_tokens": req.UncachedPromptTokens,
		"cache_read_tokens":      req.CacheReadTokens,
		"cache_creation_tokens":  req.CacheCreationTokens,
		"completion_tokens":      req.CompletionTokens,
		"actual_cost":            req.ActualCost,
	}
	for name, value := range values {
		if value < 0 {
			return req, fmt.Errorf("billing %s must be nonnegative", name)
		}
		if name != "actual_cost" && value > maxPersistedTokenCount {
			return req, fmt.Errorf("billing %s exceeds the persisted integer limit", name)
		}
	}
	partitionTotal := int64(req.UncachedPromptTokens) + int64(req.CacheReadTokens) + int64(req.CacheCreationTokens)
	if partitionTotal > maxPersistedTokenCount {
		return req, fmt.Errorf("billing prompt partition exceeds the persisted integer limit")
	}
	if req.PromptTokens == 0 && partitionTotal > 0 {
		req.PromptTokens = int(partitionTotal)
	}
	if partitionTotal == 0 && req.PromptTokens > 0 && strings.TrimSpace(req.CacheUsageSchema) == "" {
		req.UncachedPromptTokens = req.PromptTokens
		partitionTotal = int64(req.PromptTokens)
	}
	if partitionTotal != int64(req.PromptTokens) {
		return req, fmt.Errorf(
			"billing prompt partition mismatch: total=%d uncached=%d cache_read=%d cache_creation=%d",
			req.PromptTokens,
			req.UncachedPromptTokens,
			req.CacheReadTokens,
			req.CacheCreationTokens,
		)
	}
	if int64(req.PromptTokens)+int64(req.CompletionTokens) > maxPersistedTokenCount {
		return req, fmt.Errorf("billing prompt+completion tokens exceed the persisted integer limit")
	}
	req.CacheUsageSchema = strings.TrimSpace(req.CacheUsageSchema)
	req.CacheUsageStatus = strings.TrimSpace(req.CacheUsageStatus)
	if req.CacheUsageSchema == "" {
		if req.CacheReadTokens != 0 || req.CacheCreationTokens != 0 {
			return req, fmt.Errorf("billing cache token classes require an authoritative usage schema")
		}
		req.CacheUsageStatus = cacheUsageStatusNotReported
	} else if req.CacheUsageStatus == "" || req.CacheUsageStatus == cacheUsageStatusNotReported {
		return req, fmt.Errorf("billing cache usage schema requires a reported status")
	}
	if len(req.CacheUsageSchema) > 80 {
		return req, fmt.Errorf("billing cache usage schema exceeds 80 characters")
	}
	if len(req.CacheUsageStatus) > 40 {
		return req, fmt.Errorf("billing cache usage status exceeds 40 characters")
	}
	return req, nil
}

// Settle settles a frozen request with the actual cost.
// Wrapped in a single transaction for atomicity.
func (b *Billing) Settle(ctx context.Context, req SettleRequest) (*SettleResult, error) {
	var err error
	req, err = normalizeSettlementUsage(req)
	if err != nil {
		return nil, err
	}
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
	record = canonicalizeLegacyReservationUsage(record)

	switch record.Status {
	case "settled":
		// A cache-aware terminal receipt is immutable evidence, not merely an
		// idempotency shortcut. Preserve legacy cost-only replay behavior, but
		// never hide a conflicting authoritative provider partition after the
		// financial terminal state already exists.
		if (record.CacheUsageSchema != "" || req.CacheUsageSchema != "") && !settlementUsageMatches(record, req) {
			return nil, ErrSettlementUsageConflict
		}
		return settleResultFromRecord(record), nil
	case "cancelled":
		return &SettleResult{Success: false, ErrorCode: "already_cancelled"}, nil
	case "settlement_pending":
		if !record.SettlementPayloadAt.Valid {
			return nil, ErrSettlementUsageNotReady
		}
		if !settlementUsageMatches(record, req) {
			return nil, ErrSettlementUsageConflict
		}
		// Only the durable payload may drive recovery of billable output.
		req.PromptTokens = record.PromptTokens
		req.UncachedPromptTokens = record.UncachedPromptTokens
		req.CacheReadTokens = record.CacheReadTokens
		req.CacheCreationTokens = record.CacheCreationTokens
		req.CompletionTokens = record.CompletionTokens
		req.UsageSource = record.UsageSource
		req.CacheUsageSchema = record.CacheUsageSchema
		req.CacheUsageStatus = record.CacheUsageStatus
	case "frozen":
		// Non-stream responses are withheld until this transaction succeeds, so
		// they can still settle directly from the caller-provided usage.
	default:
		return &SettleResult{Success: false, ErrorCode: "invalid_state"}, nil
	}

	calculatedCost, costBreakdown, err := calculateReservationCost(record, req)
	if err != nil {
		return nil, err
	}
	// A reservation is an admission hold, not a terminal charge ceiling. Use
	// available prepaid cash for an atomic top-up; never close a known positive
	// shortfall as an implicit platform subsidy.
	chargedCost := calculatedCost
	uncoveredCost := 0
	grantConsumed := minInt(chargedCost, record.ReservedGrantedAmount)
	totalCashConsumed := chargedCost - grantConsumed
	topUpAmount := totalCashConsumed - record.ReservedBalanceAmount
	if topUpAmount < 0 {
		topUpAmount = 0
	}
	reservedCashConsumed := totalCashConsumed - topUpAmount
	refundCash := record.ReservedBalanceAmount - reservedCashConsumed
	if refundCash < 0 {
		return nil, fmt.Errorf("reservation cash source invariant violated for %s", req.RequestID)
	}
	availableCash, err := lockWalletCashForBilling(ctx, tx, req.OrgID)
	if err != nil {
		return nil, err
	}
	if availableCash < topUpAmount {
		if err := persistFundingPendingTx(ctx, tx, b.outbox, record, req, calculatedCost, topUpAmount, costBreakdown); err != nil {
			return nil, err
		}
		if err := markProviderAttemptFundingPendingTx(ctx, tx, req, calculatedCost); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit funding-pending settlement: %w", err)
		}
		return &SettleResult{
			ActualCost:     calculatedCost,
			ChargedCost:    0,
			UncoveredCost:  topUpAmount,
			TopUpAmount:    0,
			PricingVersion: record.PricingVersion,
			Success:        false,
			ErrorCode:      "insufficient_topup",
		}, nil
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
		     balance = balance + $2 - $3,
		     granted_balance = granted_balance + $4,
		     total_spent = total_spent + $5,
		     updated_at = NOW()
		 WHERE org_id = $6 AND frozen >= $1 AND balance >= $3
		 RETURNING balance`,
		record.FrozenAmount, refundCash, topUpAmount, restoredGrant, chargedCost, req.OrgID,
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
		fmt.Sprintf("Settle for request %s (prepaid_topup=%d)", req.RequestID, topUpAmount),
	).Scan(&settleID)
	if err != nil {
		return nil, fmt.Errorf("record settle: %w", err)
	}

	// Update idempotency status
	_, err = tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET status = 'settled',
		     prompt_tokens = $3,
		     uncached_prompt_tokens = $4,
		     cache_read_tokens = $5,
		     cache_creation_tokens = $6,
		     completion_tokens = $7,
		     usage_source = $8,
		     cache_usage_schema = $9,
		     cache_usage_status = $10,
		     calculated_cost = $11,
		     charged_cost = $12,
		     uncovered_cost = $13,
		     topup_amount = $14,
		     refund_amount = $15,
		     forfeited_grant_amount = $16,
		     uncached_input_cost_cents = $17,
		     cache_read_cost_cents = $18,
		     cache_creation_cost_cents = $19,
		     output_cost_cents = $20,
		     terminal_transaction_id = $21,
		     balance_after = $22,
		     terminal_at = NOW(),
		     provider_cost_pending_at = NULL,
		     settlement_pending_reason = '',
		     settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NULL,
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2 AND status IN ('frozen', 'settlement_pending')`,
		req.RequestID, req.OrgID,
		req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
		req.CompletionTokens, req.UsageSource, req.CacheUsageSchema, req.CacheUsageStatus,
		calculatedCost, chargedCost, uncoveredCost, topUpAmount, refund, forfeitedGrant,
		costBreakdown.UncachedInputCostCents, costBreakdown.CacheReadCostCents,
		costBreakdown.CacheCreationCostCents, costBreakdown.OutputCostCents,
		settleID, newBalance,
	)
	if err != nil {
		return nil, fmt.Errorf("update idempotency: %w", err)
	}
	if err := markProviderAttemptSettledTx(ctx, tx, req, calculatedCost, chargedCost, settleID); err != nil {
		return nil, err
	}

	// A recovered stream may already have a best-effort call log with zero
	// cost. Converge it to the durable settlement receipt in the same
	// transaction when that row exists. The normal request path may insert the
	// log after settlement, in which case this update intentionally affects 0.
	if record.SettlementAttempts > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE call_logs
			 SET prompt_tokens = $3,
			     uncached_prompt_tokens = $4,
			     cache_read_tokens = $5,
			     cache_creation_tokens = $6,
			     completion_tokens = $7,
			     total_tokens = $3::integer + $7::integer,
			     cost_cents = $8,
			     usage_source = $9,
			     cache_usage_schema = $10,
			     cache_usage_status = $11,
			     pricing_version = $12,
			     effective_input_price_cents = $13,
			     effective_cache_read_price_cents = $14,
			     effective_cache_creation_price_cents = $15,
			     effective_output_price_cents = $16,
			     cache_read_cost_cents = $17,
			     cache_creation_cost_cents = $18
			 WHERE request_id = $1 AND org_id = $2`,
			req.RequestID, req.OrgID,
			req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
			req.CompletionTokens, chargedCost, req.UsageSource,
			req.CacheUsageSchema, req.CacheUsageStatus, record.PricingVersion,
			record.EffectiveInputPrice, record.EffectiveCacheReadPrice,
			record.EffectiveCacheCreationPrice, record.EffectiveOutputPrice,
			costBreakdown.CacheReadCostCents, costBreakdown.CacheCreationCostCents,
		); err != nil {
			return nil, fmt.Errorf("converge recovered call log: %w", err)
		}
	}

	if err := enqueueUsageJournalTx(
		ctx, tx, b.outbox, outbox.EventBillingSettled, "settled",
		record, req, calculatedCost, chargedCost, settleID,
	); err != nil {
		return nil, fmt.Errorf("enqueue settled usage journal: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &SettleResult{
		SettleID:                         settleID,
		ActualCost:                       calculatedCost,
		ChargedCost:                      chargedCost,
		UncoveredCost:                    uncoveredCost,
		TopUpAmount:                      topUpAmount,
		Refund:                           refund,
		NewBalance:                       newBalance,
		PricingVersion:                   record.PricingVersion,
		EffectiveInputPriceCents:         record.EffectiveInputPrice,
		EffectiveCacheReadPriceCents:     record.EffectiveCacheReadPrice,
		EffectiveCacheCreationPriceCents: record.EffectiveCacheCreationPrice,
		EffectiveOutputPriceCents:        record.EffectiveOutputPrice,
		CacheReadRatio:                   record.CacheReadRatio,
		CacheCreationRatio:               record.CacheCreationRatio,
		UncachedInputCostCents:           costBreakdown.UncachedInputCostCents,
		CacheReadCostCents:               costBreakdown.CacheReadCostCents,
		CacheCreationCostCents:           costBreakdown.CacheCreationCostCents,
		OutputCostCents:                  costBreakdown.OutputCostCents,
		Success:                          true,
	}, nil
}

func persistFundingPendingTx(
	ctx context.Context,
	tx pgx.Tx,
	service *outbox.Service,
	record reservationRecord,
	req SettleRequest,
	calculatedCost int,
	shortfall int,
	costBreakdown reservationCostBreakdown,
) error {
	result, err := tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET status = 'settlement_pending',
		     provider_cost_pending_at = COALESCE(provider_cost_pending_at, NOW()),
		     settlement_pending_reason = 'insufficient_topup',
		     prompt_tokens = $3,
		     uncached_prompt_tokens = $4,
		     cache_read_tokens = $5,
		     cache_creation_tokens = $6,
		     completion_tokens = $7,
		     usage_source = $8,
		     cache_usage_schema = $9,
		     cache_usage_status = $10,
		     calculated_cost = $11,
		     charged_cost = 0,
		     uncovered_cost = $12,
		     topup_amount = 0,
		     uncached_input_cost_cents = $13,
		     cache_read_cost_cents = $14,
		     cache_creation_cost_cents = $15,
		     output_cost_cents = $16,
		     settlement_payload_ready_at = COALESCE(settlement_payload_ready_at, NOW()),
		     settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NULL,
		     settlement_last_error = 'insufficient prepaid balance for automatic top-up',
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2
		   AND status IN ('frozen', 'settlement_pending')`,
		req.RequestID, req.OrgID,
		req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
		req.CompletionTokens, req.UsageSource, req.CacheUsageSchema, req.CacheUsageStatus,
		calculatedCost, shortfall,
		costBreakdown.UncachedInputCostCents, costBreakdown.CacheReadCostCents,
		costBreakdown.CacheCreationCostCents, costBreakdown.OutputCostCents,
	)
	if err != nil {
		return fmt.Errorf("persist funding-pending settlement: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("persist funding-pending settlement: reservation state changed")
	}
	if err := enqueueUsageJournalTx(
		ctx, tx, service, outbox.EventBillingUsagePrepared, "funding_pending",
		record, req, calculatedCost, 0, 0,
	); err != nil {
		return fmt.Errorf("enqueue funding-pending usage journal: %w", err)
	}
	return nil
}

func canonicalizeLegacyReservationUsage(record reservationRecord) reservationRecord {
	if record.PromptTokens > 0 &&
		record.UncachedPromptTokens == 0 &&
		record.CacheReadTokens == 0 &&
		record.CacheCreationTokens == 0 &&
		strings.TrimSpace(record.CacheUsageSchema) == "" {
		record.UncachedPromptTokens = record.PromptTokens
	}
	if strings.TrimSpace(record.CacheUsageStatus) == "" {
		record.CacheUsageStatus = cacheUsageStatusNotReported
	}
	return record
}

type reservationCostBreakdown struct {
	UncachedInputCostCents float64
	CacheReadCostCents     float64
	CacheCreationCostCents float64
	OutputCostCents        float64
}

func calculateReservationCost(record reservationRecord, req SettleRequest) (int, reservationCostBreakdown, error) {
	if req.PromptTokens < 0 || req.CompletionTokens < 0 || req.ActualCost < 0 {
		return 0, reservationCostBreakdown{}, fmt.Errorf("billing usage values must be nonnegative")
	}
	if req.PromptTokens > 0 || req.CompletionTokens > 0 {
		if record.EffectiveInputPrice > 0 || record.EffectiveCacheReadPrice > 0 || record.EffectiveCacheCreationPrice > 0 || record.EffectiveOutputPrice > 0 {
			cacheReadPrice := record.EffectiveCacheReadPrice
			cacheCreationPrice := record.EffectiveCacheCreationPrice
			if !strings.HasPrefix(record.PricingVersion, "pv2_") {
				cacheReadPrice = record.EffectiveInputPrice
				cacheCreationPrice = record.EffectiveInputPrice
			}
			breakdown := reservationCostBreakdown{
				UncachedInputCostCents: float64(req.UncachedPromptTokens) * record.EffectiveInputPrice / 1000,
				CacheReadCostCents:     float64(req.CacheReadTokens) * cacheReadPrice / 1000,
				CacheCreationCostCents: float64(req.CacheCreationTokens) * cacheCreationPrice / 1000,
				OutputCostCents:        float64(req.CompletionTokens) * record.EffectiveOutputPrice / 1000,
			}
			cost := int(breakdown.UncachedInputCostCents + breakdown.CacheReadCostCents + breakdown.CacheCreationCostCents + breakdown.OutputCostCents)
			if cost < 1 {
				cost = 1
			}
			return cost, breakdown, nil
		}
	}
	return req.ActualCost, reservationCostBreakdown{}, nil
}

func settleResultFromRecord(record reservationRecord) *SettleResult {
	result := &SettleResult{
		ActualCost:                       record.CalculatedCost,
		ChargedCost:                      record.ChargedCost,
		UncoveredCost:                    record.UncoveredCost,
		TopUpAmount:                      record.TopUpAmount,
		Refund:                           record.RefundAmount,
		PricingVersion:                   record.PricingVersion,
		EffectiveInputPriceCents:         record.EffectiveInputPrice,
		EffectiveCacheReadPriceCents:     record.EffectiveCacheReadPrice,
		EffectiveCacheCreationPriceCents: record.EffectiveCacheCreationPrice,
		EffectiveOutputPriceCents:        record.EffectiveOutputPrice,
		CacheReadRatio:                   record.CacheReadRatio,
		CacheCreationRatio:               record.CacheCreationRatio,
		UncachedInputCostCents:           record.UncachedInputCost,
		CacheReadCostCents:               record.CacheReadCost,
		CacheCreationCostCents:           record.CacheCreationCost,
		OutputCostCents:                  record.OutputCost,
		Success:                          true,
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
		`SELECT model_name, project_id,
		        frozen_amount, reserved_balance_amount, reserved_granted_amount,
		        effective_input_price_cents::float8,
		        effective_cache_read_price_cents::float8,
		        effective_cache_creation_price_cents::float8,
		        effective_output_price_cents::float8,
		        cache_read_ratio::float8, cache_creation_ratio::float8,
		        pricing_version, status,
		        prompt_tokens, uncached_prompt_tokens, cache_read_tokens, cache_creation_tokens,
		        completion_tokens, usage_source, cache_usage_schema, cache_usage_status,
		        output_started_at,
		        settlement_payload_ready_at, settlement_attempts,
		        calculated_cost, charged_cost, uncovered_cost, topup_amount,
		        refund_amount,
		        uncached_input_cost_cents::float8, cache_read_cost_cents::float8,
		        cache_creation_cost_cents::float8, output_cost_cents::float8,
		        terminal_transaction_id, balance_after
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2
		 FOR UPDATE`, requestID, orgID,
	).Scan(
		&record.Model,
		&record.ProjectID,
		&record.FrozenAmount,
		&record.ReservedBalanceAmount,
		&record.ReservedGrantedAmount,
		&record.EffectiveInputPrice,
		&record.EffectiveCacheReadPrice,
		&record.EffectiveCacheCreationPrice,
		&record.EffectiveOutputPrice,
		&record.CacheReadRatio,
		&record.CacheCreationRatio,
		&record.PricingVersion,
		&record.Status,
		&record.PromptTokens,
		&record.UncachedPromptTokens,
		&record.CacheReadTokens,
		&record.CacheCreationTokens,
		&record.CompletionTokens,
		&record.UsageSource,
		&record.CacheUsageSchema,
		&record.CacheUsageStatus,
		&record.OutputStartedAt,
		&record.SettlementPayloadAt,
		&record.SettlementAttempts,
		&record.CalculatedCost,
		&record.ChargedCost,
		&record.UncoveredCost,
		&record.TopUpAmount,
		&record.RefundAmount,
		&record.UncachedInputCost,
		&record.CacheReadCost,
		&record.CacheCreationCost,
		&record.OutputCost,
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

func lockWalletCashForBilling(ctx context.Context, tx pgx.Tx, orgID int64) (int, error) {
	var balance int
	if err := tx.QueryRow(ctx,
		`SELECT balance FROM wallets WHERE org_id = $1 FOR UPDATE`, orgID,
	).Scan(&balance); err != nil {
		return 0, fmt.Errorf("lock wallet cash: %w", err)
	}
	return balance, nil
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
	return cancelReservation(ctx, b.db, b.outbox, requestID, orgID)
}

// CancelReservation exposes the source-preserving terminal transition to
// bounded cleanup jobs that depend on the DBQuerier interface.
func CancelReservation(ctx context.Context, database internaldb.DBQuerier, requestID string, orgID int64) error {
	return cancelReservation(ctx, database, nil, requestID, orgID)
}

// CancelReservationWithOutbox preserves the same terminal semantics while
// adding the P6-F20 transactional journal envelope.
func CancelReservationWithOutbox(ctx context.Context, database internaldb.DBQuerier, service *outbox.Service, requestID string, orgID int64) error {
	return cancelReservation(ctx, database, service, requestID, orgID)
}

func cancelReservation(ctx context.Context, database internaldb.DBQuerier, service *outbox.Service, requestID string, orgID int64) error {
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
	providerAttemptHeld, err := holdUnresolvedProviderAttemptTx(ctx, tx, requestID, orgID)
	if err != nil {
		return err
	}
	if providerAttemptHeld {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit provider-attempt hold: %w", err)
		}
		return ErrProviderAttemptUnresolved
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
	cancelReq := SettleRequest{
		RequestID:        requestID,
		OrgID:            orgID,
		UsageSource:      "",
		CacheUsageStatus: cacheUsageStatusNotReported,
		CacheUsageSchema: "",
	}
	if err := enqueueUsageJournalTx(
		ctx, tx, service, outbox.EventBillingCancelled, "cancelled",
		record, cancelReq, 0, 0, cancelID,
	); err != nil {
		return fmt.Errorf("enqueue cancelled usage journal: %w", err)
	}

	return tx.Commit(ctx)
}
