package billing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	internaldb "github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
)

// FreezeRequest contains the parameters for a freeze operation.
type FreezeRequest struct {
	RequestID                string
	OrgID                    int64
	ProjectID                int64
	APIKeyID                 int64
	AccessCredentialID       int64
	CredentialRef            string
	AccessGrantID            int64
	Model                    string
	EstimatedTokens          int
	EstimatedInputTokens     int
	ReservedCompletionTokens int
	ReservedMeterUsage       []metering.MeterQuantity
	// ExecutionSnapshotHash binds pricing snapshot and any wallet authorization
	// to the one normalized modelrouting Plan used for provider dispatch.
	ExecutionSnapshotHash string
	SafetyFactor          string
	MinFreezeCents        int64
}

// FreezeResult contains the result of a freeze operation.
type FreezeResult struct {
	FreezeID     int64
	AmountMicros int64
	// CalculatedCostMicros is the immutable conservative admission estimate.
	// AmountMicros remains the financial hold, which is zero in observation mode.
	CalculatedCostMicros int64
	NewBalanceMicros     int64
	PricingVersion       string
	BillingMode          BillingMode
	Success              bool
	ErrorCode            string
	ErrorDetail          string
}

// SettleRequest contains the parameters for a settlement.
type SettleRequest struct {
	RequestID            string
	AttemptID            string
	StreamFence          *StreamLeaseFence
	OrgID                int64
	PromptTokens         int
	UncachedPromptTokens int
	CacheReadTokens      int
	CacheCreationTokens  int
	CompletionTokens     int
	UsageSource          string
	UsageAuthority       metering.UsageAuthority
	UsagePurpose         metering.UsagePurpose
	UsageFinality        metering.UsageFinality
	UsageContractVersion string
	UsageEstimateVersion string
	CacheUsageSchema     string
	CacheUsageStatus     string
	MeterUsage           []metering.MeterQuantity

	// settlementRecovery is an internal, durable-claim-bound capability used
	// only by the generic recovery worker. External/live callers cannot mint it
	// or opt out of current stream lease validation.
	settlementRecovery *settlementRecoveryAuthority
}

type settlementRecoveryAuthority struct {
	ClaimAttempt     int
	StreamOwnerID    string
	StreamGeneration int64
}

// SettleResult contains the result of a settlement.
type SettleResult struct {
	SettleID                                    int64
	ObservationReceiptID                        int64
	ActualCostMicros                            int64
	ChargedCostMicros                           int64
	UncoveredCostMicros                         int64
	TopUpAmountMicros                           int64
	RefundMicros                                int64
	NewBalanceMicros                            int64
	PricingVersion                              string
	EffectiveInputPriceMicrosPerMillion         int64
	EffectiveCacheReadPriceMicrosPerMillion     int64
	EffectiveCacheCreationPriceMicrosPerMillion int64
	EffectiveOutputPriceMicrosPerMillion        int64
	CacheReadRatio                              float64
	CacheCreationRatio                          float64
	UncachedInputCostMicros                     int64
	CacheReadCostMicros                         int64
	CacheCreationCostMicros                     int64
	OutputCostMicros                            int64
	NonTokenCostMicros                          int64
	MeterUsage                                  []metering.MeterQuantity
	EffectiveUnitRates                          []UnitRate
	BillingMode                                 BillingMode
	Success                                     bool
	ErrorCode                                   string
}

type priceQuote struct {
	InputPriceMicrosPerMillion         int64
	CacheReadPriceMicrosPerMillion     int64
	CacheCreationPriceMicrosPerMillion int64
	OutputPriceMicrosPerMillion        int64
	CacheReadRatio                     float64
	CacheCreationRatio                 float64
	Multiplier                         string
	Version                            string
	SelectionKey                       string
	RateCardRef                        string
	RateCardVersion                    int64
	RateCardLayer                      string
	ProviderRef                        string
	UnitRates                          []UnitRate
}

type reservationRecord struct {
	BillingMode                                 BillingMode
	Model                                       string
	ProjectID                                   int64
	APIKeyID                                    sql.NullInt64
	AccessCredentialID                          sql.NullInt64
	CredentialRef                               sql.NullString
	AccessGrantID                               sql.NullInt64
	ExecutionSnapshotHash                       sql.NullString
	FrozenAmountMicros                          int64
	AdmissionCalculatedCostMicros               int64
	ReservedBalanceMicros                       int64
	ReservedGrantedMicros                       int64
	EffectiveInputPriceMicrosPerMillion         int64
	EffectiveCacheReadPriceMicrosPerMillion     int64
	EffectiveCacheCreationPriceMicrosPerMillion int64
	EffectiveOutputPriceMicrosPerMillion        int64
	CacheReadRatio                              float64
	CacheCreationRatio                          float64
	PricingVersion                              string
	Status                                      string
	SettlementPendingReason                     string
	DeliveryAttemptID                           sql.NullString
	PromptTokens                                int
	UncachedPromptTokens                        int
	CacheReadTokens                             int
	CacheCreationTokens                         int
	CompletionTokens                            int
	UsageSource                                 string
	UsageAuthority                              metering.UsageAuthority
	UsagePurpose                                metering.UsagePurpose
	UsageFinality                               metering.UsageFinality
	UsageContractVersion                        string
	UsageEstimateVersion                        string
	CacheUsageSchema                            string
	CacheUsageStatus                            string
	OutputStartedAt                             sql.NullTime
	SettlementPayloadAt                         sql.NullTime
	SettlementStreamOwnerID                     sql.NullString
	SettlementStreamGeneration                  sql.NullInt64
	SettlementAttempts                          int
	CalculatedCostMicros                        int64
	ChargedCostMicros                           int64
	UncoveredCostMicros                         int64
	TopUpAmountMicros                           int64
	RefundAmountMicros                          int64
	UncachedInputCostMicros                     int64
	CacheReadCostMicros                         int64
	CacheCreationCostMicros                     int64
	OutputCostMicros                            int64
	NonTokenCostMicros                          int64
	EffectiveUnitRates                          []UnitRate
	ReservedMeterUsage                          []metering.MeterQuantity
	MeterUsage                                  []metering.MeterQuantity
	TerminalTransactionID                       sql.NullInt64
	TerminalObservationReceiptID                sql.NullInt64
	BalanceAfter                                sql.NullInt64
}

var ErrSettlementPending = errors.New("billing reservation has billable output pending settlement")
var ErrSettlementUsageNotReady = errors.New("billing settlement usage is not durably prepared")
var ErrSettlementUsageConflict = errors.New("billing settlement usage conflicts with the durable payload")
var ErrSettlementFundingPending = errors.New("billing settlement requires additional prepaid funds")

const (
	cacheUsageStatusNotReported = "not_reported"
	maxPersistedTokenCount      = metering.MaxPersistedTokenCount
)

// Billing handles the freeze/settle/cancel state machine.
type Billing struct {
	recoveryAllowed          func() bool
	db                       *pgxpool.Pool
	rdb                      *redis.Client
	mode                     BillingMode
	requestPricingAuthority  RequestPricingAuthority
	prices                   PriceProvider
	outbox                   *outbox.Service
	terminalMaterializer     TerminalMaterializer
	reservationMaterializer  ReservationMaterializer
	committedObservationSink CommittedObservationSink
}

// NewBilling creates a new Billing instance.
func NewBilling(db *pgxpool.Pool, rdb *redis.Client) *Billing {
	return &Billing{
		db: db, rdb: rdb, mode: BillingModePrepaidEnforced,
		requestPricingAuthority: RequestPricingAuthorityLegacy,
		prices:                  NewDBPriceProvider(db),
	}
}

// WithMode configures the mode used only for new reservations. Every later
// state transition reads the immutable mode snapshot from the reservation.
// Invalid and not-yet-supported values remain installed so request admission
// fails closed instead of silently changing financial semantics.
func (b *Billing) WithMode(mode BillingMode) *Billing {
	if b != nil {
		b.mode = mode
	}
	return b
}

// FinancialJournalReady reports the original durable journal attachment, not source profile state.
func (b *Billing) FinancialJournalReady() bool { return b != nil && b.outbox != nil }

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

// QueryModelPriceMicrosForOrg returns immutable effective integer prices in
// micro-CNY per one million tokens.
func (b *Billing) QueryModelPriceMicrosForOrg(ctx context.Context, orgID int64, model string) (inputCost, outputCost int64, err error) {
	quote, err := b.queryPriceQuoteForOrg(ctx, orgID, model)
	if err != nil {
		return 0, 0, err
	}
	return quote.InputPriceMicrosPerMillion, quote.OutputPriceMicrosPerMillion, nil
}

// queryPriceQuoteForOrg resolves pricing before a wallet transaction begins.
// Production uses exactly one explicitly configured authority: legacy tables
// during the upgrade window, or active versioned Rate Cards after cutover.
// Tests may inject a non-DB PriceProvider; injected prices are treated as
// already effective with multiplier 1 and never participate in production
// wiring.
func (b *Billing) queryPriceQuoteForOrg(ctx context.Context, orgID int64, model string) (priceQuote, error) {
	if _, ok := b.prices.(*DBPriceProvider); !ok {
		inputCost, outputCost, err := b.prices.QueryModelPrice(ctx, model)
		if err != nil {
			return priceQuote{}, err
		}
		return newPriceQuote(model, inputCost, outputCost, "1", "1", "1", time.Time{}, time.Time{}, "injected")
	}
	switch b.requestPricingAuthority {
	case RequestPricingAuthorityLegacy:
		quote, err := b.queryLegacyPriceQuote(ctx, orgID, model)
		if err != nil && b.mode == BillingModeCostObservation && errors.Is(err, ErrModelPriceNotConfigured) {
			return unpricedObservationQuote(model), nil
		}
		return quote, err
	case RequestPricingAuthorityRateCard:
		return b.queryRateCardPriceQuote(ctx, orgID, model, time.Now().UTC())
	default:
		return priceQuote{}, fmt.Errorf("invalid request pricing authority %q", b.requestPricingAuthority)
	}
}

// unpricedObservationQuote keeps the default local observation mode usable
// before an administrator configures authoritative prices. Zero here means
// "cost unknown", not "the provider is free"; the distinct immutable version
// is carried through reservations, receipts, journals, and diagnostics.
func unpricedObservationQuote(model string) priceQuote {
	digest := sha256.Sum256([]byte("unpriced-observation-v1\x00" + strings.TrimSpace(model)))
	return priceQuote{
		CacheReadRatio: 1, CacheCreationRatio: 1, Multiplier: "1",
		Version: "uo1_" + hex.EncodeToString(digest[:]),
	}
}

// queryLegacyPriceQuote is an explicit upgrade-compatibility authority, never
// a fallback from strict Rate Card mode. Operators leave it only after the
// Rate Card readiness preflight succeeds.
func (b *Billing) queryLegacyPriceQuote(ctx context.Context, orgID int64, model string) (priceQuote, error) {
	var baseInput, baseOutput, cacheReadRatio, cacheCreationRatio, multiplier string
	var baseUpdatedAt, orgUpdatedAt sql.NullTime
	err := b.db.QueryRow(ctx, `
		SELECT mp.input_price_cents::text,mp.output_price_cents::text,
		       COALESCE(mp.cache_read_ratio,1)::text,COALESCE(mp.cache_creation_ratio,1)::text,
		       COALESCE(omp.multiplier,1)::text,mp.updated_at,omp.updated_at
		FROM model_prices mp
		LEFT JOIN org_model_prices omp ON omp.org_id=$2 AND omp.model_name=$1
		WHERE mp.model_name=$1`, model, orgID).Scan(
		&baseInput, &baseOutput, &cacheReadRatio, &cacheCreationRatio, &multiplier,
		&baseUpdatedAt, &orgUpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return priceQuote{}, fmt.Errorf("%w: %s", ErrModelPriceNotConfigured, model)
		}
		return priceQuote{}, fmt.Errorf("query model price from explicit legacy authority for org %d model %s: %w", orgID, model, err)
	}
	return newPriceQuote(
		model, baseInput, baseOutput, cacheReadRatio, cacheCreationRatio, multiplier,
		nullablePricingTime(baseUpdatedAt), nullablePricingTime(orgUpdatedAt), "legacy_authority",
	)
}

func nullablePricingTime(value sql.NullTime) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func newPriceQuote(model, baseInput, baseOutput, cacheReadRatio, cacheCreationRatio, multiplier string, baseUpdatedAt, orgUpdatedAt time.Time, source string) (priceQuote, error) {
	inputPrice, err := money.PriceMicrosPerMillionFromInternal(baseInput, "1", multiplier)
	if err != nil {
		return priceQuote{}, fmt.Errorf("input price for %s: %w", model, err)
	}
	cacheReadPrice, err := money.PriceMicrosPerMillionFromInternal(baseInput, cacheReadRatio, multiplier)
	if err != nil {
		return priceQuote{}, fmt.Errorf("cache-read price for %s: %w", model, err)
	}
	cacheCreationPrice, err := money.PriceMicrosPerMillionFromInternal(baseInput, cacheCreationRatio, multiplier)
	if err != nil {
		return priceQuote{}, fmt.Errorf("cache-creation price for %s: %w", model, err)
	}
	outputPrice, err := money.PriceMicrosPerMillionFromInternal(baseOutput, "1", multiplier)
	if err != nil {
		return priceQuote{}, fmt.Errorf("output price for %s: %w", model, err)
	}
	cacheReadRatioFloat, err := strconv.ParseFloat(cacheReadRatio, 64)
	if err != nil {
		return priceQuote{}, fmt.Errorf("cache-read display ratio for %s: %w", model, err)
	}
	cacheCreationRatioFloat, err := strconv.ParseFloat(cacheCreationRatio, 64)
	if err != nil {
		return priceQuote{}, fmt.Errorf("cache-creation display ratio for %s: %w", model, err)
	}
	canonical := fmt.Sprintf(
		"pv3|%s|%d|%d|%d|%d|%s|%s|%s|%d|%d|%s",
		model,
		inputPrice,
		cacheReadPrice,
		cacheCreationPrice,
		outputPrice,
		cacheReadRatio,
		cacheCreationRatio,
		multiplier,
		baseUpdatedAt.UnixNano(),
		orgUpdatedAt.UnixNano(),
		source,
	)
	digest := sha256.Sum256([]byte(canonical))
	return priceQuote{
		InputPriceMicrosPerMillion:         inputPrice,
		CacheReadPriceMicrosPerMillion:     cacheReadPrice,
		CacheCreationPriceMicrosPerMillion: cacheCreationPrice,
		OutputPriceMicrosPerMillion:        outputPrice,
		CacheReadRatio:                     cacheReadRatioFloat,
		CacheCreationRatio:                 cacheCreationRatioFloat,
		Multiplier:                         multiplier,
		Version:                            "pv3_" + hex.EncodeToString(digest[:]),
	}, nil
}

func estimateFreezeAmountFromQuote(quote priceQuote, estimatedTokens int, safetyFactor string, minFreezeCents int64) (int64, error) {
	return estimateFreezeAmountWithMeters(quote, estimatedTokens, nil, safetyFactor, minFreezeCents)
}

func estimateFreezeAmountWithMeters(quote priceQuote, estimatedTokens int, reservedMeterUsage []metering.MeterQuantity, safetyFactor string, minFreezeCents int64) (int64, error) {
	if strings.HasPrefix(quote.Version, "uo1_") {
		if len(reservedMeterUsage) > 0 {
			return 0, fmt.Errorf("non-token meters require an authoritative Rate Card")
		}
		return 0, nil
	}
	reservationPrice := quote.InputPriceMicrosPerMillion
	for _, candidate := range []int64{quote.OutputPriceMicrosPerMillion, quote.CacheReadPriceMicrosPerMillion, quote.CacheCreationPriceMicrosPerMillion} {
		if candidate > reservationPrice {
			reservationPrice = candidate
		}
	}
	tokenHold, err := money.EstimateHold(int64(estimatedTokens), reservationPrice, safetyFactor, 0)
	if err != nil {
		return 0, err
	}
	unitHold, err := estimateUnitMeterHold(quote.UnitRates, reservedMeterUsage, safetyFactor)
	if err != nil {
		return 0, err
	}
	hold, err := money.Add(tokenHold, unitHold)
	if err != nil {
		return 0, err
	}
	minimum, err := money.CentsToMicros(minFreezeCents)
	if err != nil {
		return 0, err
	}
	if hold < minimum {
		return minimum, nil
	}
	return hold, nil
}

// CalculateCostMicros calculates a base-price request charge in micro-CNY.
func (b *Billing) CalculateCostMicros(ctx context.Context, model string, promptTokens, completionTokens int) (int64, error) {
	inputSource, outputSource, err := b.prices.QueryModelPrice(ctx, model)
	if err != nil {
		return 0, err
	}
	quote, err := newPriceQuote(model, inputSource, outputSource, "1", "1", "1", time.Time{}, time.Time{}, "base")
	if err != nil {
		return 0, err
	}
	calculation, err := money.Calculate(
		money.Component{Tokens: int64(promptTokens), PriceMicrosPerMillion: quote.InputPriceMicrosPerMillion},
		money.Component{Tokens: int64(completionTokens), PriceMicrosPerMillion: quote.OutputPriceMicrosPerMillion},
	)
	return calculation.TotalMicros, err
}

// CalculateCostMicrosForOrg applies an organization's immutable multiplier.
func (b *Billing) CalculateCostMicrosForOrg(ctx context.Context, orgID int64, model string, promptTokens, completionTokens int) (int64, error) {
	quote, err := b.queryPriceQuoteForOrg(ctx, orgID, model)
	if err != nil {
		return 0, err
	}
	calculation, err := money.Calculate(
		money.Component{Tokens: int64(promptTokens), PriceMicrosPerMillion: quote.InputPriceMicrosPerMillion},
		money.Component{Tokens: int64(completionTokens), PriceMicrosPerMillion: quote.OutputPriceMicrosPerMillion},
	)
	return calculation.TotalMicros, err
}

// EstimateFreezeMicros calculates a conservative hold from an exact decimal
// safety factor and an explicitly cents-denominated policy minimum.
func (b *Billing) EstimateFreezeMicros(ctx context.Context, model string, estimatedTokens int, safetyFactor string, minFreezeCents int64) (int64, error) {
	inputSource, outputSource, err := b.prices.QueryModelPrice(ctx, model)
	if err != nil {
		return 0, err
	}
	quote, err := newPriceQuote(model, inputSource, outputSource, "1", "1", "1", time.Time{}, time.Time{}, "base")
	if err != nil {
		return 0, err
	}
	return estimateFreezeAmountFromQuote(quote, estimatedTokens, safetyFactor, minFreezeCents)
}

// Freeze deducts balance (prioritizing granted_balance) and creates a freeze record.
// Wrapped in a single transaction for atomicity.
func (b *Billing) Freeze(ctx context.Context, req FreezeRequest) (*FreezeResult, error) {
	if b == nil || b.db == nil {
		return nil, fmt.Errorf("billing database is unavailable")
	}
	if err := ValidateBillingMode(b.mode); err != nil {
		return nil, err
	}
	req.ExecutionSnapshotHash = strings.TrimSpace(req.ExecutionSnapshotHash)
	if req.ExecutionSnapshotHash != "" && !validModelExecutionSnapshotHash(req.ExecutionSnapshotHash) {
		return nil, fmt.Errorf("freeze execution snapshot hash is invalid")
	}
	// An admitted operation is governed by its persisted mode and price
	// snapshot. Resolve an existing receipt before consulting the current
	// pricing authority so a restart or authority switch cannot break an
	// otherwise idempotent retry. The transactional check below remains the
	// concurrency authority when no committed receipt exists yet.
	if replay, found, err := b.replayFreeze(ctx, req); err != nil {
		return nil, err
	} else if found {
		return replay, nil
	}
	mode := b.mode
	// Resolve the effective price before acquiring a transaction connection. A
	// one-connection pool must not deadlock by trying to query through the pool
	// while its only connection is already held by tx.
	quote, err := b.queryPriceQuoteForOrg(ctx, req.OrgID, req.Model)
	if err != nil {
		return nil, fmt.Errorf("quote freeze price: %w", err)
	}
	reservedMeterUsage, err := normalizeMeterUsage(req.ReservedMeterUsage)
	if err != nil {
		return nil, fmt.Errorf("normalize reserved meter usage: %w", err)
	}
	req.ReservedMeterUsage = reservedMeterUsage
	freezeAmount, err := estimateFreezeAmountWithMeters(quote, req.EstimatedTokens, req.ReservedMeterUsage, req.SafetyFactor, req.MinFreezeCents)
	if err != nil {
		return nil, fmt.Errorf("calculate freeze amount: %w", err)
	}
	minFreezeMicros, err := money.CentsToMicros(req.MinFreezeCents)
	if err != nil {
		return nil, fmt.Errorf("convert minimum freeze: %w", err)
	}

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
	var existingAPIKeyID, existingAccessCredentialID, existingAccessGrantID sql.NullInt64
	var existingCredentialRef, existingExecutionSnapshotHash sql.NullString
	err = tx.QueryRow(ctx,
		`SELECT frozen_amount_micros, admission_calculated_cost_micros,
		        status, pricing_version, billing_mode, api_key_id,
		        access_credential_id,credential_ref,access_grant_id,execution_snapshot_hash
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2`,
		req.RequestID, req.OrgID,
	).Scan(&existing.FrozenAmountMicros, &existing.AdmissionCalculatedCostMicros,
		&existing.Status, &existing.PricingVersion, &existing.BillingMode, &existingAPIKeyID,
		&existingAccessCredentialID, &existingCredentialRef, &existingAccessGrantID, &existingExecutionSnapshotHash)
	if err == nil {
		if nullableID(existingAPIKeyID) != req.APIKeyID || nullableID(existingAccessCredentialID) != req.AccessCredentialID ||
			nullableString(existingCredentialRef) != req.CredentialRef || nullableID(existingAccessGrantID) != req.AccessGrantID {
			return &FreezeResult{Success: false, ErrorCode: "request_identity_conflict", ErrorDetail: "billing access identity changed"}, nil
		}
		if nullableString(existingExecutionSnapshotHash) != req.ExecutionSnapshotHash {
			return &FreezeResult{Success: false, ErrorCode: "request_identity_conflict", ErrorDetail: "model execution Plan snapshot changed"}, nil
		}
		if existing.Status == "frozen" {
			return &FreezeResult{
				AmountMicros:         existing.FrozenAmountMicros,
				CalculatedCostMicros: existing.AdmissionCalculatedCostMicros,
				PricingVersion:       existing.PricingVersion,
				BillingMode:          existing.BillingMode,
				Success:              true,
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
	if mode == BillingModeCostObservation {
		if err := b.insertFreezeReservationTx(
			ctx, tx, req, quote, mode, minFreezeMicros, freezeAmount, 0, 0, 0,
		); err != nil {
			if isUniqueViolation(err) {
				_ = tx.Rollback(ctx)
				record, lookupErr := b.lookupReservation(ctx, req.RequestID, req.OrgID)
				if lookupErr == nil && record.Status == "frozen" && reservationExecutionSnapshotMatches(record, req.ExecutionSnapshotHash) {
					return &FreezeResult{
						AmountMicros: 0, CalculatedCostMicros: record.AdmissionCalculatedCostMicros,
						PricingVersion: record.PricingVersion,
						BillingMode:    record.BillingMode, Success: true,
					}, nil
				}
			}
			return nil, fmt.Errorf("insert observation idempotency: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit observation reservation: %w", err)
		}
		return &FreezeResult{
			AmountMicros: 0, CalculatedCostMicros: freezeAmount, NewBalanceMicros: 0,
			PricingVersion: quote.Version, BillingMode: mode, Success: true,
		}, nil
	}
	// Get current balance
	var balance, grantedBalance, frozen int64
	err = tx.QueryRow(ctx,
		`SELECT balance_micros, granted_balance_micros, frozen_micros FROM wallets WHERE org_id = $1 FOR UPDATE`,
		req.OrgID).Scan(&balance, &grantedBalance, &frozen)
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	_, expiredGrantBalance, err := expireGrantRowsInLockedWallet(ctx, tx, req.OrgID)
	if err != nil {
		return nil, err
	}
	grantedBalance -= minInt64(grantedBalance, expiredGrantBalance)

	// balance and granted_balance are already debited when a reservation is
	// created. frozen is a held-funds control total, not another deduction from
	// spendable funds.
	available, err := money.Add(balance, grantedBalance)
	if err != nil {
		return nil, fmt.Errorf("calculate available balance: %w", err)
	}
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
	reservedGranted := minInt64(grantedBalance, freezeAmount)
	reservedBalance := freezeAmount - reservedGranted
	newGranted := grantedBalance - reservedGranted
	newBalance := balance - reservedBalance
	grantAllocations, err := reserveGrantRows(ctx, tx, req.OrgID, reservedGranted)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`UPDATE wallets SET balance_micros = $1, granted_balance_micros = $2, frozen_micros = frozen_micros + $3, updated_at = NOW()
		 WHERE org_id = $4`,
		newBalance, newGranted, freezeAmount, req.OrgID)
	if err != nil {
		return nil, fmt.Errorf("update wallet: %w", err)
	}

	// Record transaction
	_, err = tx.Exec(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount_micros, note, created_at)
		 VALUES ($1, $2, 'freeze', 'completed', $3, $4, NOW())`,
		req.RequestID, req.OrgID, freezeAmount, "Freeze for request "+req.RequestID)
	if err != nil {
		return nil, fmt.Errorf("record transaction: %w", err)
	}

	// Insert the self-contained reservation and its immutable price/mode snapshot.
	err = b.insertFreezeReservationTx(
		ctx, tx, req, quote, mode, minFreezeMicros, freezeAmount,
		freezeAmount, reservedBalance, reservedGranted,
	)
	if err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			record, lookupErr := b.lookupReservation(ctx, req.RequestID, req.OrgID)
			if lookupErr == nil && record.Status == "frozen" && reservationExecutionSnapshotMatches(record, req.ExecutionSnapshotHash) {
				return &FreezeResult{
					AmountMicros:         record.FrozenAmountMicros,
					CalculatedCostMicros: record.AdmissionCalculatedCostMicros,
					PricingVersion:       record.PricingVersion,
					BillingMode:          record.BillingMode,
					Success:              true,
				}, nil
			}
		}
		return nil, fmt.Errorf("insert idempotency: %w", err)
	}

	for _, allocation := range grantAllocations {
		_, err = tx.Exec(ctx,
			`INSERT INTO billing_reservation_grant_allocations (
				org_id, request_id, grant_id, reserved_micros
			 ) VALUES ($1, $2, $3, $4)`,
			req.OrgID, req.RequestID, allocation.GrantID, allocation.Micros,
		)
		if err != nil {
			return nil, fmt.Errorf("record grant reservation allocation: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &FreezeResult{
		FreezeID:             0,
		AmountMicros:         freezeAmount,
		CalculatedCostMicros: freezeAmount,
		NewBalanceMicros:     newBalance,
		PricingVersion:       quote.Version,
		BillingMode:          mode,
		Success:              true,
	}, nil
}

func (b *Billing) replayFreeze(ctx context.Context, req FreezeRequest) (*FreezeResult, bool, error) {
	var record reservationRecord
	var apiKeyID, accessCredentialID, accessGrantID sql.NullInt64
	var credentialRef, executionSnapshotHash sql.NullString
	err := b.db.QueryRow(ctx,
		`SELECT frozen_amount_micros, admission_calculated_cost_micros,
		        status, pricing_version, billing_mode, api_key_id,
		        access_credential_id,credential_ref,access_grant_id,execution_snapshot_hash
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2`,
		req.RequestID, req.OrgID,
	).Scan(&record.FrozenAmountMicros, &record.AdmissionCalculatedCostMicros,
		&record.Status, &record.PricingVersion, &record.BillingMode, &apiKeyID,
		&accessCredentialID, &credentialRef, &accessGrantID, &executionSnapshotHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("check committed reservation: %w", err)
	}
	if err := validateReservationBillingMode(record.BillingMode); err != nil {
		return nil, true, err
	}
	if nullableID(apiKeyID) != req.APIKeyID || nullableID(accessCredentialID) != req.AccessCredentialID ||
		nullableString(credentialRef) != req.CredentialRef || nullableID(accessGrantID) != req.AccessGrantID {
		return &FreezeResult{Success: false, ErrorCode: "request_identity_conflict", ErrorDetail: "billing access identity changed"}, true, nil
	}
	if nullableString(executionSnapshotHash) != req.ExecutionSnapshotHash {
		return &FreezeResult{Success: false, ErrorCode: "request_identity_conflict", ErrorDetail: "model execution Plan snapshot changed"}, true, nil
	}
	if record.Status == "frozen" {
		return &FreezeResult{
			AmountMicros: record.FrozenAmountMicros, CalculatedCostMicros: record.AdmissionCalculatedCostMicros,
			PricingVersion: record.PricingVersion,
			BillingMode:    record.BillingMode, Success: true,
		}, true, nil
	}
	return &FreezeResult{
		Success: false, ErrorCode: "request_terminal",
		ErrorDetail: "the operation already reached state " + record.Status,
	}, true, nil
}

func (b *Billing) insertFreezeReservationTx(
	ctx context.Context,
	tx pgx.Tx,
	req FreezeRequest,
	quote priceQuote,
	mode BillingMode,
	minFreezeMicros, admissionCalculatedCost, frozenAmount, reservedBalance, reservedGranted int64,
) error {
	unitRatesJSON, err := encodeUnitRates(quote.UnitRates)
	if err != nil {
		return err
	}
	reservedMeterUsageJSON, err := encodeMeterUsage(req.ReservedMeterUsage)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO freeze_idempotency (
			request_id, org_id, frozen_amount_micros, status, billing_mode,
			admission_calculated_cost_micros,
			project_id, api_key_id, access_credential_id, credential_ref, access_grant_id,
			reserved_balance_micros, reserved_granted_micros,
			model_name, estimated_tokens, reserved_completion_tokens,
			safety_factor, min_freeze_micros,
			effective_input_price_micros_per_million, effective_cache_read_price_micros_per_million,
			effective_cache_creation_price_micros_per_million, effective_output_price_micros_per_million,
			cache_read_ratio, cache_creation_ratio,
			price_multiplier, pricing_version, effective_unit_rates,
			reserved_meter_usage, execution_snapshot_hash, updated_at
		 ) VALUES (
			$1, $2, $3, 'frozen', $4, $5,
			$6, NULLIF($7,0), NULLIF($8,0), NULLIF($9,''), NULLIF($10,0),
			$11, $12, $13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22, $23, $24, $25, $26::jsonb, $27::jsonb, NULLIF($28,''), NOW()
		 )`,
		req.RequestID, req.OrgID, frozenAmount, mode, admissionCalculatedCost,
		req.ProjectID, req.APIKeyID, req.AccessCredentialID, req.CredentialRef, req.AccessGrantID,
		reservedBalance, reservedGranted,
		req.Model, req.EstimatedTokens, req.ReservedCompletionTokens,
		req.SafetyFactor, minFreezeMicros,
		quote.InputPriceMicrosPerMillion, quote.CacheReadPriceMicrosPerMillion, quote.CacheCreationPriceMicrosPerMillion,
		quote.OutputPriceMicrosPerMillion, quote.CacheReadRatio, quote.CacheCreationRatio,
		quote.Multiplier, quote.Version, string(unitRatesJSON), string(reservedMeterUsageJSON), req.ExecutionSnapshotHash,
	)
	if err != nil {
		return err
	}
	return b.materializeSourceReservation(ctx, tx, req, quote, mode)
}

func nullableID(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func nullableString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func validModelExecutionSnapshotHash(value string) bool {
	if len(value) != 69 || !strings.HasPrefix(value, "mrs1_") || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value[len("mrs1_"):])
	return err == nil && len(decoded) == 32
}

func reservationExecutionSnapshotMatches(record reservationRecord, expected string) bool {
	return nullableString(record.ExecutionSnapshotHash) == expected
}

type grantReservationAllocation struct {
	GrantID int64
	Micros  int64
}

func reserveGrantRows(ctx context.Context, tx pgx.Tx, orgID int64, requested int64) ([]grantReservationAllocation, error) {
	if requested <= 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx,
		`SELECT id, balance_micros
		 FROM grants
		 WHERE org_id = $1
		   AND status = 'active'
		   AND balance_micros > 0
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
		var balance int64
		if err := rows.Scan(&grantID, &balance); err != nil {
			return nil, fmt.Errorf("scan grant balance: %w", err)
		}
		amount := minInt64(balance, remaining)
		allocations = append(allocations, grantReservationAllocation{GrantID: grantID, Micros: amount})
		remaining -= amount
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grant balances: %w", err)
	}
	rows.Close()
	for _, allocation := range allocations {
		if _, err := tx.Exec(ctx,
			`UPDATE grants SET balance_micros = balance_micros - $1, updated_at = NOW() WHERE id = $2`,
			allocation.Micros, allocation.GrantID,
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

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func (b *Billing) lookupReservation(ctx context.Context, requestID string, orgID int64) (reservationRecord, error) {
	var record reservationRecord
	var unitRatesJSON, reservedMeterUsageJSON, meterUsageJSON []byte
	err := b.db.QueryRow(ctx,
		`SELECT billing_mode, frozen_amount_micros, admission_calculated_cost_micros,
		        reserved_balance_micros, reserved_granted_micros,
		        effective_input_price_micros_per_million,
		        effective_cache_read_price_micros_per_million,
		        effective_cache_creation_price_micros_per_million,
		        effective_output_price_micros_per_million,
		        cache_read_ratio::float8, cache_creation_ratio::float8,
		        pricing_version, effective_unit_rates, reserved_meter_usage, meter_usage,
		        status, settlement_pending_reason, delivery_attempt_id,
		        prompt_tokens, uncached_prompt_tokens, cache_read_tokens, cache_creation_tokens,
		        completion_tokens, usage_source,
		        usage_authority, usage_purpose, usage_finality,
		        usage_contract_version, usage_estimate_version,
		        cache_usage_schema, cache_usage_status,
		        output_started_at,
		        settlement_payload_ready_at, settlement_stream_owner_id,
		        settlement_stream_generation, settlement_attempts,
		        calculated_cost_micros, charged_cost_micros, uncovered_cost_micros, topup_amount_micros,
		        refund_amount_micros,
		        uncached_input_cost_micros, cache_read_cost_micros,
		        cache_creation_cost_micros, output_cost_micros, non_token_cost_micros,
		        terminal_transaction_id, terminal_observation_receipt_id, balance_after_micros,
		        execution_snapshot_hash
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2`,
		requestID, orgID,
	).Scan(
		&record.BillingMode,
		&record.FrozenAmountMicros,
		&record.AdmissionCalculatedCostMicros,
		&record.ReservedBalanceMicros,
		&record.ReservedGrantedMicros,
		&record.EffectiveInputPriceMicrosPerMillion,
		&record.EffectiveCacheReadPriceMicrosPerMillion,
		&record.EffectiveCacheCreationPriceMicrosPerMillion,
		&record.EffectiveOutputPriceMicrosPerMillion,
		&record.CacheReadRatio,
		&record.CacheCreationRatio,
		&record.PricingVersion,
		&unitRatesJSON,
		&reservedMeterUsageJSON,
		&meterUsageJSON,
		&record.Status,
		&record.SettlementPendingReason,
		&record.DeliveryAttemptID,
		&record.PromptTokens,
		&record.UncachedPromptTokens,
		&record.CacheReadTokens,
		&record.CacheCreationTokens,
		&record.CompletionTokens,
		&record.UsageSource,
		&record.UsageAuthority,
		&record.UsagePurpose,
		&record.UsageFinality,
		&record.UsageContractVersion,
		&record.UsageEstimateVersion,
		&record.CacheUsageSchema,
		&record.CacheUsageStatus,
		&record.OutputStartedAt,
		&record.SettlementPayloadAt,
		&record.SettlementStreamOwnerID,
		&record.SettlementStreamGeneration,
		&record.SettlementAttempts,
		&record.CalculatedCostMicros,
		&record.ChargedCostMicros,
		&record.UncoveredCostMicros,
		&record.TopUpAmountMicros,
		&record.RefundAmountMicros,
		&record.UncachedInputCostMicros,
		&record.CacheReadCostMicros,
		&record.CacheCreationCostMicros,
		&record.OutputCostMicros,
		&record.NonTokenCostMicros,
		&record.TerminalTransactionID,
		&record.TerminalObservationReceiptID,
		&record.BalanceAfter,
		&record.ExecutionSnapshotHash,
	)
	if err == nil {
		err = validateReservationBillingMode(record.BillingMode)
	}
	if err == nil {
		record.EffectiveUnitRates, err = decodeUnitRates(unitRatesJSON)
	}
	if err == nil {
		record.ReservedMeterUsage, err = decodeMeterUsage(reservedMeterUsageJSON)
	}
	if err == nil {
		record.MeterUsage, err = decodeMeterUsage(meterUsageJSON)
	}
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
	if req.StreamFence != nil {
		fence, err := normalizeStreamLeaseFence(*req.StreamFence)
		if err != nil {
			return req, err
		}
		req.StreamFence = &fence
	}
	meterUsage, err := normalizeMeterUsage(req.MeterUsage)
	if err != nil {
		return req, err
	}
	req.MeterUsage = meterUsage
	values := map[string]int{
		"prompt_tokens":          req.PromptTokens,
		"uncached_prompt_tokens": req.UncachedPromptTokens,
		"cache_read_tokens":      req.CacheReadTokens,
		"cache_creation_tokens":  req.CacheCreationTokens,
		"completion_tokens":      req.CompletionTokens,
	}
	for name, value := range values {
		if value < 0 {
			return req, fmt.Errorf("billing %s must be nonnegative", name)
		}
		if value > maxPersistedTokenCount {
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
	req, err = normalizeSettlementUsageContract(req)
	if err != nil {
		return req, err
	}
	return req, nil
}

func normalizeSettlementUsageContract(req SettleRequest) (SettleRequest, error) {
	req.UsageSource = metering.NormalizeUsageSource(req.UsageSource)
	if req.UsageAuthority == "" && req.UsagePurpose == "" && req.UsageFinality == "" &&
		strings.TrimSpace(req.UsageContractVersion) == "" && strings.TrimSpace(req.UsageEstimateVersion) == "" {
		// Compatibility for repository-internal callers that predate P6-F41.
		// The adapter/gateway path always supplies the explicit contract.
		req.UsageContractVersion = metering.UsageContractVersion
		req.UsagePurpose = metering.UsagePurposeCustomerCharge
		switch req.UsageSource {
		case metering.UsageSourceUpstream:
			req.UsageAuthority = metering.UsageAuthorityProviderReported
			req.UsageFinality = metering.UsageFinalityFinal
		case metering.UsageSourceCounterV2, metering.UsageSourceCounterV3:
			req.UsageAuthority = metering.UsageAuthorityGatewayCounter
			req.UsageFinality = metering.UsageFinalityEstimated
			req.UsageEstimateVersion, _ = metering.CounterVersion(req.UsageSource)
		case metering.UsageSourceEstimated, metering.UsageSourceAdapterEstimated:
			req.UsageAuthority = metering.UsageAuthorityConservativeEstimate
			req.UsageFinality = metering.UsageFinalityEstimated
			req.UsageEstimateVersion = metering.ConservativeEstimateVersionV1
		case metering.UsageSourceResponseCache:
			req.UsageContractVersion = metering.UsageContractVersionV2
			req.UsageAuthority = metering.UsageAuthorityLocalCache
			req.UsageFinality = metering.UsageFinalityFinal
		}
	}
	req.UsageContractVersion = strings.TrimSpace(req.UsageContractVersion)
	req.UsageEstimateVersion = strings.TrimSpace(req.UsageEstimateVersion)
	if req.UsageContractVersion != metering.UsageContractVersion && req.UsageContractVersion != metering.UsageContractVersionV2 {
		return req, fmt.Errorf("billing usage contract version %q is unsupported", req.UsageContractVersion)
	}
	if req.UsagePurpose != metering.UsagePurposeCustomerCharge {
		return req, fmt.Errorf("billing settlement requires customer-charge usage purpose")
	}
	switch req.UsageAuthority {
	case metering.UsageAuthorityProviderReported:
		if req.UsageSource != metering.UsageSourceUpstream || req.UsageFinality != metering.UsageFinalityFinal || req.UsageEstimateVersion != "" {
			return req, fmt.Errorf("billing provider-reported usage contract is inconsistent")
		}
	case metering.UsageAuthorityGatewayCounter:
		if !metering.ValidCounterPair(req.UsageSource, req.UsageEstimateVersion) || req.UsageFinality != metering.UsageFinalityEstimated {
			return req, fmt.Errorf("billing gateway-counter usage contract is inconsistent")
		}
	case metering.UsageAuthorityConservativeEstimate:
		if (req.UsageSource != metering.UsageSourceEstimated && req.UsageSource != metering.UsageSourceAdapterEstimated) || req.UsageFinality != metering.UsageFinalityEstimated || req.UsageEstimateVersion == "" {
			return req, fmt.Errorf("billing conservative-estimate usage contract is inconsistent")
		}
	case metering.UsageAuthorityLocalCache:
		if req.UsageContractVersion != metering.UsageContractVersionV2 ||
			req.UsageSource != metering.UsageSourceResponseCache ||
			req.UsageFinality != metering.UsageFinalityFinal || req.UsageEstimateVersion != "" ||
			req.PromptTokens != 0 || req.UncachedPromptTokens != 0 || req.CacheReadTokens != 0 ||
			req.CacheCreationTokens != 0 || req.CompletionTokens != 0 {
			return req, fmt.Errorf("billing local-cache usage contract is inconsistent")
		}
	default:
		return req, fmt.Errorf("billing settlement rejects usage authority %q", req.UsageAuthority)
	}
	if len(req.UsageContractVersion) > 80 || len(req.UsageEstimateVersion) > 80 {
		return req, fmt.Errorf("billing usage contract metadata exceeds 80 characters")
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
	if err := requireSettlementCounterVersionTx(ctx, tx, req); err != nil {
		return nil, err
	}

	switch record.Status {
	case "settled":
		if (record.UsageAuthority == metering.UsageAuthorityGatewayCounter || req.UsageAuthority == metering.UsageAuthorityGatewayCounter) &&
			(record.UsageSource != req.UsageSource || record.UsageEstimateVersion != req.UsageEstimateVersion ||
				(req.UsageSource == metering.UsageSourceCounterV3 && !settlementUsageMatches(record, req))) {
			return nil, ErrSettlementUsageConflict
		}
		if err := requireSettlementAttempt(record, req); err != nil {
			return nil, err
		}
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
		if err := requireBoundOrInternalSettlementTx(ctx, tx, record, req); err != nil {
			return nil, err
		}
		if err := requireSettlementStreamAuthorityTx(ctx, tx, record, req); err != nil {
			return nil, err
		}
		if !record.SettlementPayloadAt.Valid {
			return nil, ErrSettlementUsageNotReady
		}
		if !settlementUsageMatches(record, req) {
			return nil, ErrSettlementUsageConflict
		}
		if err := requireChargeableDeliveryTx(ctx, tx, record, req); err != nil {
			return nil, err
		}
		// Only the durable payload may drive recovery of billable output.
		req.PromptTokens = record.PromptTokens
		req.UncachedPromptTokens = record.UncachedPromptTokens
		req.CacheReadTokens = record.CacheReadTokens
		req.CacheCreationTokens = record.CacheCreationTokens
		req.CompletionTokens = record.CompletionTokens
		req.UsageSource = record.UsageSource
		req.UsageAuthority = record.UsageAuthority
		req.UsagePurpose = record.UsagePurpose
		req.UsageFinality = record.UsageFinality
		req.UsageContractVersion = record.UsageContractVersion
		req.UsageEstimateVersion = record.UsageEstimateVersion
		req.CacheUsageSchema = record.CacheUsageSchema
		req.CacheUsageStatus = record.CacheUsageStatus
		req.MeterUsage = slices.Clone(record.MeterUsage)
	case "frozen":
		// Legacy/internal reservations without a provider attempt may still settle
		// directly. Once an upstream attempt exists, P6-F41 requires the caller to
		// prepare delivery and persist a chargeable local write receipt first.
		if err := requireBoundOrInternalSettlementTx(ctx, tx, record, req); err != nil {
			return nil, err
		}
	default:
		return &SettleResult{Success: false, ErrorCode: "invalid_state"}, nil
	}

	calculatedCost, costBreakdown, err := calculateReservationCost(record, req)
	if err != nil {
		return nil, err
	}
	if err := persistReservationMeterEvidenceTx(ctx, tx, req.RequestID, req.OrgID, req.MeterUsage, costBreakdown.NonTokenCostMicros); err != nil {
		return nil, err
	}
	if record.BillingMode == BillingModeCostObservation {
		return b.settleObservationTx(ctx, tx, record, req, calculatedCost, costBreakdown)
	}
	// A reservation is an admission hold, not a terminal charge ceiling. Use
	// available prepaid cash for an atomic top-up; never close a known positive
	// shortfall as an implicit platform subsidy.
	chargedCost := calculatedCost
	uncoveredCost := int64(0)
	grantConsumed := minInt64(chargedCost, record.ReservedGrantedMicros)
	totalCashConsumed := chargedCost - grantConsumed
	additionalTopUpAmount := totalCashConsumed - record.ReservedBalanceMicros
	if additionalTopUpAmount < 0 {
		additionalTopUpAmount = 0
	}
	totalTopUpAmount, err := money.Add(record.TopUpAmountMicros, additionalTopUpAmount)
	if err != nil {
		return nil, fmt.Errorf("calculate prepaid top-up: %w", err)
	}
	reservedCashConsumed := totalCashConsumed - additionalTopUpAmount
	refundCash := record.ReservedBalanceMicros - reservedCashConsumed
	if refundCash < 0 {
		return nil, fmt.Errorf("reservation cash source invariant violated for %s", req.RequestID)
	}
	availableCash, err := lockWalletCashForBilling(ctx, tx, req.OrgID)
	if err != nil {
		return nil, err
	}
	if availableCash < additionalTopUpAmount {
		if err := persistFundingPendingTx(ctx, tx, b.outbox, record, req, calculatedCost, additionalTopUpAmount, costBreakdown); err != nil {
			return nil, err
		}
		if err := markProviderAttemptFundingPendingTx(ctx, tx, req, calculatedCost); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit funding-pending settlement: %w", err)
		}
		return &SettleResult{
			ActualCostMicros:    calculatedCost,
			ChargedCostMicros:   0,
			UncoveredCostMicros: additionalTopUpAmount,
			TopUpAmountMicros:   0,
			PricingVersion:      record.PricingVersion,
			BillingMode:         record.BillingMode,
			Success:             false,
			ErrorCode:           "insufficient_topup",
		}, nil
	}

	restoredGrant, forfeitedGrant, err := finalizeGrantAllocations(
		ctx,
		tx,
		req.OrgID,
		req.RequestID,
		record.ReservedGrantedMicros,
		grantConsumed,
	)
	if err != nil {
		return nil, err
	}
	refund, err := money.Add(refundCash, restoredGrant)
	if err != nil {
		return nil, fmt.Errorf("calculate settlement refund: %w", err)
	}

	// The wallet row is updated once while the reservation row lock guarantees
	// that mixed Settle/Cancel callers cannot both produce terminal effects.
	var newBalance int64
	err = tx.QueryRow(ctx,
		`UPDATE wallets
		 SET frozen_micros = frozen_micros - $1,
		     balance_micros = balance_micros + $2 - $3,
		     granted_balance_micros = granted_balance_micros + $4,
		     total_spent_micros = total_spent_micros + $5,
		     updated_at = NOW()
		 WHERE org_id = $6 AND frozen_micros >= $1 AND balance_micros >= $3
		 RETURNING balance_micros`,
		record.FrozenAmountMicros, refundCash, additionalTopUpAmount, restoredGrant, chargedCost, req.OrgID,
	).Scan(&newBalance)
	if err != nil {
		return nil, fmt.Errorf("settle balance: %w", err)
	}

	// Record settle transaction
	var settleID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount_micros, balance_after_micros, note)
		 VALUES ($1, $2, 'settle', 'completed', $3, $4, $5)
		 RETURNING id`,
		req.RequestID,
		req.OrgID,
		chargedCost,
		newBalance,
		fmt.Sprintf("Settle for request %s (prepaid_topup=%d)", req.RequestID, totalTopUpAmount),
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
		     usage_authority = $9,
		     usage_purpose = $10,
		     usage_finality = $11,
		     usage_contract_version = $12,
		     usage_estimate_version = $13,
		     cache_usage_schema = $14,
		     cache_usage_status = $15,
		     calculated_cost_micros = $16,
		     charged_cost_micros = $17,
		     uncovered_cost_micros = $18,
		     topup_amount_micros = $19,
		     refund_amount_micros = $20,
		     forfeited_grant_amount_micros = $21,
		     uncached_input_cost_micros = $22,
		     cache_read_cost_micros = $23,
		     cache_creation_cost_micros = $24,
		     output_cost_micros = $25,
		     terminal_transaction_id = $26,
		     balance_after_micros = $27,
		     terminal_at = NOW(),
		     provider_cost_pending_at = NULL,
		     settlement_pending_reason = '',
		     settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NULL,
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2 AND status IN ('frozen', 'settlement_pending')`,
		req.RequestID, req.OrgID,
		req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
		req.CompletionTokens, req.UsageSource,
		req.UsageAuthority, req.UsagePurpose, req.UsageFinality,
		req.UsageContractVersion, req.UsageEstimateVersion,
		req.CacheUsageSchema, req.CacheUsageStatus,
		calculatedCost, chargedCost, uncoveredCost, totalTopUpAmount, refund, forfeitedGrant,
		costBreakdown.UncachedInputCostMicros, costBreakdown.CacheReadCostMicros,
		costBreakdown.CacheCreationCostMicros, costBreakdown.OutputCostMicros,
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
		unitRatesJSON, encodeErr := encodeUnitRates(record.EffectiveUnitRates)
		if encodeErr != nil {
			return nil, encodeErr
		}
		meterUsageJSON, encodeErr := encodeMeterUsage(req.MeterUsage)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if _, err := tx.Exec(ctx,
			`UPDATE call_logs
			 SET prompt_tokens = $3,
			     uncached_prompt_tokens = $4,
			     cache_read_tokens = $5,
			     cache_creation_tokens = $6,
			     completion_tokens = $7,
			     total_tokens = $3::integer + $7::integer,
			     cost_micros = $8,
			     usage_source = $9,
			     usage_authority = $10,
			     usage_purpose = $11,
			     usage_finality = $12,
			     usage_contract_version = $13,
			     usage_estimate_version = $14,
			     cache_usage_schema = $15,
			     cache_usage_status = $16,
			     pricing_version = $17,
			     effective_input_price_micros_per_million = $18,
			     effective_cache_read_price_micros_per_million = $19,
			     effective_cache_creation_price_micros_per_million = $20,
			     effective_output_price_micros_per_million = $21,
			     cache_read_cost_micros = $22,
			     cache_creation_cost_micros = $23,
			     effective_unit_rates = $24::jsonb,
			     meter_usage = $25::jsonb,
			     non_token_cost_micros = $26
			 WHERE request_id = $1 AND org_id = $2`,
			req.RequestID, req.OrgID,
			req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
			req.CompletionTokens, chargedCost, req.UsageSource,
			req.UsageAuthority, req.UsagePurpose, req.UsageFinality,
			req.UsageContractVersion, req.UsageEstimateVersion,
			req.CacheUsageSchema, req.CacheUsageStatus, record.PricingVersion,
			record.EffectiveInputPriceMicrosPerMillion, record.EffectiveCacheReadPriceMicrosPerMillion,
			record.EffectiveCacheCreationPriceMicrosPerMillion, record.EffectiveOutputPriceMicrosPerMillion,
			costBreakdown.CacheReadCostMicros, costBreakdown.CacheCreationCostMicros,
			string(unitRatesJSON), string(meterUsageJSON), costBreakdown.NonTokenCostMicros,
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
		SettleID:                                    settleID,
		ActualCostMicros:                            calculatedCost,
		ChargedCostMicros:                           chargedCost,
		UncoveredCostMicros:                         uncoveredCost,
		TopUpAmountMicros:                           totalTopUpAmount,
		RefundMicros:                                refund,
		NewBalanceMicros:                            newBalance,
		PricingVersion:                              record.PricingVersion,
		EffectiveInputPriceMicrosPerMillion:         record.EffectiveInputPriceMicrosPerMillion,
		EffectiveCacheReadPriceMicrosPerMillion:     record.EffectiveCacheReadPriceMicrosPerMillion,
		EffectiveCacheCreationPriceMicrosPerMillion: record.EffectiveCacheCreationPriceMicrosPerMillion,
		EffectiveOutputPriceMicrosPerMillion:        record.EffectiveOutputPriceMicrosPerMillion,
		CacheReadRatio:                              record.CacheReadRatio,
		CacheCreationRatio:                          record.CacheCreationRatio,
		UncachedInputCostMicros:                     costBreakdown.UncachedInputCostMicros,
		CacheReadCostMicros:                         costBreakdown.CacheReadCostMicros,
		CacheCreationCostMicros:                     costBreakdown.CacheCreationCostMicros,
		OutputCostMicros:                            costBreakdown.OutputCostMicros,
		NonTokenCostMicros:                          costBreakdown.NonTokenCostMicros,
		MeterUsage:                                  slices.Clone(req.MeterUsage),
		EffectiveUnitRates:                          slices.Clone(record.EffectiveUnitRates),
		BillingMode:                                 record.BillingMode,
		Success:                                     true,
	}, nil
}

func (b *Billing) settleObservationTx(
	ctx context.Context,
	tx pgx.Tx,
	record reservationRecord,
	req SettleRequest,
	calculatedCost int64,
	costBreakdown reservationCostBreakdown,
) (*SettleResult, error) {
	receiptID, err := insertObservationReceiptTx(ctx, tx, record, req, "settled", calculatedCost)
	if err != nil {
		return nil, err
	}
	result, err := tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET status = 'settled',
		     prompt_tokens = $3,
		     uncached_prompt_tokens = $4,
		     cache_read_tokens = $5,
		     cache_creation_tokens = $6,
		     completion_tokens = $7,
		     usage_source = $8,
		     usage_authority = $9,
		     usage_purpose = $10,
		     usage_finality = $11,
		     usage_contract_version = $12,
		     usage_estimate_version = $13,
		     cache_usage_schema = $14,
		     cache_usage_status = $15,
		     calculated_cost_micros = $16,
		     charged_cost_micros = 0,
		     uncovered_cost_micros = 0,
		     topup_amount_micros = 0,
		     refund_amount_micros = 0,
		     forfeited_grant_amount_micros = 0,
		     uncached_input_cost_micros = $17,
		     cache_read_cost_micros = $18,
		     cache_creation_cost_micros = $19,
		     output_cost_micros = $20,
		     terminal_transaction_id = NULL,
		     terminal_observation_receipt_id = $21,
		     balance_after_micros = NULL,
		     terminal_at = NOW(),
		     provider_cost_pending_at = NULL,
		     settlement_pending_reason = '',
		     settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NULL,
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2
		   AND billing_mode = 'cost_observation'
		   AND status IN ('frozen', 'settlement_pending')`,
		req.RequestID, req.OrgID,
		req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
		req.CompletionTokens, req.UsageSource,
		req.UsageAuthority, req.UsagePurpose, req.UsageFinality,
		req.UsageContractVersion, req.UsageEstimateVersion,
		req.CacheUsageSchema, req.CacheUsageStatus,
		calculatedCost,
		costBreakdown.UncachedInputCostMicros, costBreakdown.CacheReadCostMicros,
		costBreakdown.CacheCreationCostMicros, costBreakdown.OutputCostMicros,
		receiptID,
	)
	if err != nil {
		return nil, fmt.Errorf("terminalize observation settlement: %w", err)
	}
	if result.RowsAffected() != 1 {
		return nil, fmt.Errorf("terminalize observation settlement: reservation state changed")
	}
	if err := markProviderAttemptSettledTx(ctx, tx, req, calculatedCost, 0, 0); err != nil {
		return nil, err
	}
	if record.SettlementAttempts > 0 {
		unitRatesJSON, encodeErr := encodeUnitRates(record.EffectiveUnitRates)
		if encodeErr != nil {
			return nil, encodeErr
		}
		meterUsageJSON, encodeErr := encodeMeterUsage(req.MeterUsage)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if _, err := tx.Exec(ctx,
			`UPDATE call_logs
			 SET prompt_tokens = $3,
			     uncached_prompt_tokens = $4,
			     cache_read_tokens = $5,
			     cache_creation_tokens = $6,
			     completion_tokens = $7,
			     total_tokens = $3::integer + $7::integer,
			     cost_micros = 0,
			     usage_source = $8,
			     usage_authority = $9,
			     usage_purpose = $10,
			     usage_finality = $11,
			     usage_contract_version = $12,
			     usage_estimate_version = $13,
			     cache_usage_schema = $14,
			     cache_usage_status = $15,
			     pricing_version = $16,
			     effective_input_price_micros_per_million = $17,
			     effective_cache_read_price_micros_per_million = $18,
			     effective_cache_creation_price_micros_per_million = $19,
			     effective_output_price_micros_per_million = $20,
			     cache_read_cost_micros = $21,
			     cache_creation_cost_micros = $22,
			     effective_unit_rates = $23::jsonb,
			     meter_usage = $24::jsonb,
			     non_token_cost_micros = $25
			 WHERE request_id = $1 AND org_id = $2`,
			req.RequestID, req.OrgID,
			req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
			req.CompletionTokens, req.UsageSource,
			req.UsageAuthority, req.UsagePurpose, req.UsageFinality,
			req.UsageContractVersion, req.UsageEstimateVersion,
			req.CacheUsageSchema, req.CacheUsageStatus, record.PricingVersion,
			record.EffectiveInputPriceMicrosPerMillion, record.EffectiveCacheReadPriceMicrosPerMillion,
			record.EffectiveCacheCreationPriceMicrosPerMillion, record.EffectiveOutputPriceMicrosPerMillion,
			costBreakdown.CacheReadCostMicros, costBreakdown.CacheCreationCostMicros,
			string(unitRatesJSON), string(meterUsageJSON), costBreakdown.NonTokenCostMicros,
		); err != nil {
			return nil, fmt.Errorf("converge recovered observation call log: %w", err)
		}
	}
	if err := enqueueUsageJournalTx(
		ctx, tx, b.outbox, outbox.EventBillingSettled, "settled",
		record, req, calculatedCost, 0, 0,
	); err != nil {
		return nil, fmt.Errorf("enqueue observation usage journal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit observation settlement: %w", err)
	}
	return &SettleResult{
		ObservationReceiptID:                        receiptID,
		ActualCostMicros:                            calculatedCost,
		ChargedCostMicros:                           0,
		PricingVersion:                              record.PricingVersion,
		EffectiveInputPriceMicrosPerMillion:         record.EffectiveInputPriceMicrosPerMillion,
		EffectiveCacheReadPriceMicrosPerMillion:     record.EffectiveCacheReadPriceMicrosPerMillion,
		EffectiveCacheCreationPriceMicrosPerMillion: record.EffectiveCacheCreationPriceMicrosPerMillion,
		EffectiveOutputPriceMicrosPerMillion:        record.EffectiveOutputPriceMicrosPerMillion,
		CacheReadRatio:                              record.CacheReadRatio,
		CacheCreationRatio:                          record.CacheCreationRatio,
		UncachedInputCostMicros:                     costBreakdown.UncachedInputCostMicros,
		CacheReadCostMicros:                         costBreakdown.CacheReadCostMicros,
		CacheCreationCostMicros:                     costBreakdown.CacheCreationCostMicros,
		OutputCostMicros:                            costBreakdown.OutputCostMicros,
		NonTokenCostMicros:                          costBreakdown.NonTokenCostMicros,
		MeterUsage:                                  slices.Clone(req.MeterUsage),
		EffectiveUnitRates:                          slices.Clone(record.EffectiveUnitRates),
		BillingMode:                                 record.BillingMode,
		Success:                                     true,
	}, nil
}

func insertObservationReceiptTx(
	ctx context.Context,
	tx pgx.Tx,
	record reservationRecord,
	req SettleRequest,
	terminalStatus string,
	calculatedCost int64,
) (int64, error) {
	unitRatesJSON, err := encodeUnitRates(record.EffectiveUnitRates)
	if err != nil {
		return 0, err
	}
	meterUsageJSON, err := encodeMeterUsage(req.MeterUsage)
	if err != nil {
		return 0, err
	}
	nonTokenCost, err := calculateUnitMeterCost(record.EffectiveUnitRates, req.MeterUsage)
	if err != nil {
		return 0, err
	}
	var receiptID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO billing_observation_receipts (
			org_id, request_id, terminal_status, calculated_cost_micros,
			charged_cost_micros, pricing_version, usage_authority,
			usage_purpose, usage_finality, usage_contract_version,
			usage_estimate_version, effective_unit_rates, meter_usage,
			non_token_cost_micros, occurred_at
		 ) VALUES ($1,$2,$3,$4,0,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::jsonb,$13,NOW())
		 RETURNING id`,
		req.OrgID, req.RequestID, terminalStatus, calculatedCost,
		record.PricingVersion, req.UsageAuthority, req.UsagePurpose,
		req.UsageFinality, req.UsageContractVersion, req.UsageEstimateVersion,
		string(unitRatesJSON), string(meterUsageJSON), nonTokenCost,
	).Scan(&receiptID)
	if err != nil {
		return 0, fmt.Errorf("record observation terminal receipt: %w", err)
	}
	return receiptID, nil
}

func persistFundingPendingTx(
	ctx context.Context,
	tx pgx.Tx,
	service *outbox.Service,
	record reservationRecord,
	req SettleRequest,
	calculatedCost int64,
	shortfall int64,
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
		     usage_authority = $9,
		     usage_purpose = $10,
		     usage_finality = $11,
		     usage_contract_version = $12,
		     usage_estimate_version = $13,
		     cache_usage_schema = $14,
		     cache_usage_status = $15,
		     calculated_cost_micros = $16,
		     charged_cost_micros = 0,
		     uncovered_cost_micros = $17,
		     topup_amount_micros = 0,
		     uncached_input_cost_micros = $18,
		     cache_read_cost_micros = $19,
		     cache_creation_cost_micros = $20,
		     output_cost_micros = $21,
		     settlement_payload_ready_at = COALESCE(settlement_payload_ready_at, NOW()),
		     settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NULL,
		     settlement_last_error = 'insufficient prepaid balance for automatic top-up',
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2
		   AND status IN ('frozen', 'settlement_pending')`,
		req.RequestID, req.OrgID,
		req.PromptTokens, req.UncachedPromptTokens, req.CacheReadTokens, req.CacheCreationTokens,
		req.CompletionTokens, req.UsageSource,
		req.UsageAuthority, req.UsagePurpose, req.UsageFinality,
		req.UsageContractVersion, req.UsageEstimateVersion,
		req.CacheUsageSchema, req.CacheUsageStatus,
		calculatedCost, shortfall,
		costBreakdown.UncachedInputCostMicros, costBreakdown.CacheReadCostMicros,
		costBreakdown.CacheCreationCostMicros, costBreakdown.OutputCostMicros,
	)
	if err != nil {
		return fmt.Errorf("persist funding-pending settlement: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("persist funding-pending settlement: reservation state changed")
	}
	if err := enqueueUsageJournalTx(
		ctx, tx, service, outbox.EventBillingUsagePrepared, "usage_prepared",
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
	if record.UsageAuthority == "" && strings.TrimSpace(record.UsageContractVersion) == "" && strings.TrimSpace(record.UsageSource) != "" {
		contract, err := normalizeSettlementUsageContract(SettleRequest{UsageSource: record.UsageSource})
		if err == nil {
			record.UsageAuthority = contract.UsageAuthority
			record.UsagePurpose = contract.UsagePurpose
			record.UsageFinality = contract.UsageFinality
			record.UsageContractVersion = contract.UsageContractVersion
			record.UsageEstimateVersion = contract.UsageEstimateVersion
		}
	}
	return record
}

type reservationCostBreakdown struct {
	UncachedInputCostMicros int64
	CacheReadCostMicros     int64
	CacheCreationCostMicros int64
	OutputCostMicros        int64
	NonTokenCostMicros      int64
}

func calculateReservationCost(record reservationRecord, req SettleRequest) (int64, reservationCostBreakdown, error) {
	if req.PromptTokens < 0 || req.CompletionTokens < 0 {
		return 0, reservationCostBreakdown{}, fmt.Errorf("billing usage values must be nonnegative")
	}
	cacheReadPrice := record.EffectiveCacheReadPriceMicrosPerMillion
	cacheCreationPrice := record.EffectiveCacheCreationPriceMicrosPerMillion
	if !pricingVersionHasExplicitCacheRates(record.PricingVersion) {
		cacheReadPrice = record.EffectiveInputPriceMicrosPerMillion
		cacheCreationPrice = record.EffectiveInputPriceMicrosPerMillion
	}
	calculation, err := money.Calculate(
		money.Component{Tokens: int64(req.UncachedPromptTokens), PriceMicrosPerMillion: record.EffectiveInputPriceMicrosPerMillion},
		money.Component{Tokens: int64(req.CacheReadTokens), PriceMicrosPerMillion: cacheReadPrice},
		money.Component{Tokens: int64(req.CacheCreationTokens), PriceMicrosPerMillion: cacheCreationPrice},
		money.Component{Tokens: int64(req.CompletionTokens), PriceMicrosPerMillion: record.EffectiveOutputPriceMicrosPerMillion},
	)
	if err != nil {
		return 0, reservationCostBreakdown{}, fmt.Errorf("calculate reservation cost: %w", err)
	}
	reservedByMeter := make(map[string]int64, len(record.ReservedMeterUsage))
	for _, item := range record.ReservedMeterUsage {
		reservedByMeter[item.Meter] = item.Quantity
	}
	for _, item := range req.MeterUsage {
		reserved, ok := reservedByMeter[item.Meter]
		if !ok || item.Quantity > reserved {
			return 0, reservationCostBreakdown{}, fmt.Errorf("billing meter %s usage exceeds its frozen reservation", item.Meter)
		}
	}
	nonTokenCost, err := calculateUnitMeterCost(record.EffectiveUnitRates, req.MeterUsage)
	if err != nil {
		return 0, reservationCostBreakdown{}, err
	}
	totalCost, err := money.Add(calculation.TotalMicros, nonTokenCost)
	if err != nil {
		return 0, reservationCostBreakdown{}, fmt.Errorf("add non-token meter cost: %w", err)
	}
	return totalCost, reservationCostBreakdown{
		UncachedInputCostMicros: calculation.ComponentMicros[0],
		CacheReadCostMicros:     calculation.ComponentMicros[1],
		CacheCreationCostMicros: calculation.ComponentMicros[2],
		OutputCostMicros:        calculation.ComponentMicros[3],
		NonTokenCostMicros:      nonTokenCost,
	}, nil
}

func pricingVersionHasExplicitCacheRates(version string) bool {
	return strings.HasPrefix(version, "pv2_") || strings.HasPrefix(version, "pv3_") ||
		strings.HasPrefix(version, "rcq2_") || strings.HasPrefix(version, "rcq3_")
}

func settleResultFromRecord(record reservationRecord) *SettleResult {
	result := &SettleResult{
		ActualCostMicros:                            record.CalculatedCostMicros,
		ChargedCostMicros:                           record.ChargedCostMicros,
		UncoveredCostMicros:                         record.UncoveredCostMicros,
		TopUpAmountMicros:                           record.TopUpAmountMicros,
		RefundMicros:                                record.RefundAmountMicros,
		PricingVersion:                              record.PricingVersion,
		EffectiveInputPriceMicrosPerMillion:         record.EffectiveInputPriceMicrosPerMillion,
		EffectiveCacheReadPriceMicrosPerMillion:     record.EffectiveCacheReadPriceMicrosPerMillion,
		EffectiveCacheCreationPriceMicrosPerMillion: record.EffectiveCacheCreationPriceMicrosPerMillion,
		EffectiveOutputPriceMicrosPerMillion:        record.EffectiveOutputPriceMicrosPerMillion,
		CacheReadRatio:                              record.CacheReadRatio,
		CacheCreationRatio:                          record.CacheCreationRatio,
		UncachedInputCostMicros:                     record.UncachedInputCostMicros,
		CacheReadCostMicros:                         record.CacheReadCostMicros,
		CacheCreationCostMicros:                     record.CacheCreationCostMicros,
		OutputCostMicros:                            record.OutputCostMicros,
		NonTokenCostMicros:                          record.NonTokenCostMicros,
		MeterUsage:                                  slices.Clone(record.MeterUsage),
		EffectiveUnitRates:                          slices.Clone(record.EffectiveUnitRates),
		BillingMode:                                 record.BillingMode,
		Success:                                     true,
	}
	if record.TerminalTransactionID.Valid {
		result.SettleID = record.TerminalTransactionID.Int64
	}
	if record.BalanceAfter.Valid {
		result.NewBalanceMicros = record.BalanceAfter.Int64
	}
	if record.TerminalObservationReceiptID.Valid {
		result.ObservationReceiptID = record.TerminalObservationReceiptID.Int64
	}
	return result
}

func reservationForUpdate(ctx context.Context, tx pgx.Tx, requestID string, orgID int64) (reservationRecord, error) {
	var record reservationRecord
	var unitRatesJSON, reservedMeterUsageJSON, meterUsageJSON []byte
	err := tx.QueryRow(ctx,
		`SELECT billing_mode, model_name, project_id, api_key_id,access_credential_id,credential_ref,access_grant_id,
		        frozen_amount_micros, admission_calculated_cost_micros,
		        reserved_balance_micros, reserved_granted_micros,
		        effective_input_price_micros_per_million,
		        effective_cache_read_price_micros_per_million,
		        effective_cache_creation_price_micros_per_million,
		        effective_output_price_micros_per_million,
		        cache_read_ratio::float8, cache_creation_ratio::float8,
		        pricing_version, effective_unit_rates, reserved_meter_usage, meter_usage,
		        status, settlement_pending_reason, delivery_attempt_id,
		        prompt_tokens, uncached_prompt_tokens, cache_read_tokens, cache_creation_tokens,
		        completion_tokens, usage_source,
		        usage_authority, usage_purpose, usage_finality,
		        usage_contract_version, usage_estimate_version,
		        cache_usage_schema, cache_usage_status,
		        output_started_at,
		        settlement_payload_ready_at, settlement_stream_owner_id,
		        settlement_stream_generation, settlement_attempts,
		        calculated_cost_micros, charged_cost_micros, uncovered_cost_micros, topup_amount_micros,
		        refund_amount_micros,
		        uncached_input_cost_micros, cache_read_cost_micros,
		        cache_creation_cost_micros, output_cost_micros, non_token_cost_micros,
		        terminal_transaction_id, terminal_observation_receipt_id, balance_after_micros,
		        execution_snapshot_hash
		 FROM freeze_idempotency
		 WHERE request_id = $1 AND org_id = $2
		 FOR UPDATE`, requestID, orgID,
	).Scan(
		&record.BillingMode,
		&record.Model,
		&record.ProjectID,
		&record.APIKeyID,
		&record.AccessCredentialID,
		&record.CredentialRef,
		&record.AccessGrantID,
		&record.FrozenAmountMicros,
		&record.AdmissionCalculatedCostMicros,
		&record.ReservedBalanceMicros,
		&record.ReservedGrantedMicros,
		&record.EffectiveInputPriceMicrosPerMillion,
		&record.EffectiveCacheReadPriceMicrosPerMillion,
		&record.EffectiveCacheCreationPriceMicrosPerMillion,
		&record.EffectiveOutputPriceMicrosPerMillion,
		&record.CacheReadRatio,
		&record.CacheCreationRatio,
		&record.PricingVersion,
		&unitRatesJSON,
		&reservedMeterUsageJSON,
		&meterUsageJSON,
		&record.Status,
		&record.SettlementPendingReason,
		&record.DeliveryAttemptID,
		&record.PromptTokens,
		&record.UncachedPromptTokens,
		&record.CacheReadTokens,
		&record.CacheCreationTokens,
		&record.CompletionTokens,
		&record.UsageSource,
		&record.UsageAuthority,
		&record.UsagePurpose,
		&record.UsageFinality,
		&record.UsageContractVersion,
		&record.UsageEstimateVersion,
		&record.CacheUsageSchema,
		&record.CacheUsageStatus,
		&record.OutputStartedAt,
		&record.SettlementPayloadAt,
		&record.SettlementStreamOwnerID,
		&record.SettlementStreamGeneration,
		&record.SettlementAttempts,
		&record.CalculatedCostMicros,
		&record.ChargedCostMicros,
		&record.UncoveredCostMicros,
		&record.TopUpAmountMicros,
		&record.RefundAmountMicros,
		&record.UncachedInputCostMicros,
		&record.CacheReadCostMicros,
		&record.CacheCreationCostMicros,
		&record.OutputCostMicros,
		&record.NonTokenCostMicros,
		&record.TerminalTransactionID,
		&record.TerminalObservationReceiptID,
		&record.BalanceAfter,
		&record.ExecutionSnapshotHash,
	)
	if err == nil {
		err = validateReservationBillingMode(record.BillingMode)
	}
	if err == nil {
		record.EffectiveUnitRates, err = decodeUnitRates(unitRatesJSON)
	}
	if err == nil {
		record.ReservedMeterUsage, err = decodeMeterUsage(reservedMeterUsageJSON)
	}
	if err == nil {
		record.MeterUsage, err = decodeMeterUsage(meterUsageJSON)
	}
	return record, err
}

func validateReservationBillingMode(mode BillingMode) error {
	if err := ValidateBillingMode(mode); err != nil {
		return fmt.Errorf("reservation billing mode: %w", err)
	}
	return nil
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

func lockWalletCashForBilling(ctx context.Context, tx pgx.Tx, orgID int64) (int64, error) {
	var balance int64
	if err := tx.QueryRow(ctx,
		`SELECT balance_micros FROM wallets WHERE org_id = $1 FOR UPDATE`, orgID,
	).Scan(&balance); err != nil {
		return 0, fmt.Errorf("lock wallet cash: %w", err)
	}
	return balance, nil
}

func finalizeGrantAllocations(ctx context.Context, tx pgx.Tx, orgID int64, requestID string, reservedGrant, grantConsumed int64) (restored, forfeited int64, err error) {
	rows, err := tx.Query(ctx,
		`SELECT a.grant_id, a.reserved_micros,
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
		reserved   int64
		refundable bool
	}
	allocations := make([]allocation, 0)
	var totalAllocated int64
	for rows.Next() {
		var item allocation
		if err := rows.Scan(&item.grantID, &item.reserved, &item.refundable); err != nil {
			return 0, 0, fmt.Errorf("scan reservation grant allocation: %w", err)
		}
		allocations = append(allocations, item)
		nextTotal, addErr := money.Add(totalAllocated, item.reserved)
		if addErr != nil {
			return 0, 0, fmt.Errorf("sum reservation grant allocations: %w", addErr)
		}
		totalAllocated = nextTotal
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
		consumed := minInt64(item.reserved, remainingConsumed)
		remainingConsumed -= consumed
		refundCandidate := item.reserved - consumed
		refunded := int64(0)
		if refundCandidate > 0 {
			if item.refundable {
				if _, err := tx.Exec(ctx,
					`UPDATE grants
					 SET balance_micros = balance_micros + $1, updated_at = NOW()
					 WHERE id = $2
					   AND status = 'active'
					   AND (expires_at IS NULL OR expires_at > NOW())`,
					refundCandidate, item.grantID,
				); err != nil {
					return 0, 0, fmt.Errorf("refund grant allocation: %w", err)
				}
				refunded = refundCandidate
				restored, err = money.Add(restored, refunded)
				if err != nil {
					return 0, 0, fmt.Errorf("sum restored grant allocations: %w", err)
				}
			} else {
				forfeited, err = money.Add(forfeited, refundCandidate)
				if err != nil {
					return 0, 0, fmt.Errorf("sum forfeited grant allocations: %w", err)
				}
			}
		}
		if _, err := tx.Exec(ctx,
			`UPDATE billing_reservation_grant_allocations
			 SET consumed_micros = $1, refunded_micros = $2, updated_at = NOW()
			 WHERE org_id = $3 AND request_id = $4 AND grant_id = $5`,
			consumed, refunded, orgID, requestID, item.grantID,
		); err != nil {
			return 0, 0, fmt.Errorf("finalize grant allocation: %w", err)
		}
	}

	unallocated := reservedGrant - totalAllocated
	unallocatedConsumed := minInt64(unallocated, remainingConsumed)
	remainingConsumed -= unallocatedConsumed
	if remainingConsumed != 0 {
		return 0, 0, fmt.Errorf("grant consumption invariant violated for %s", requestID)
	}
	restored, err = money.Add(restored, unallocated-unallocatedConsumed)
	if err != nil {
		return 0, 0, fmt.Errorf("sum unallocated restored grant balance: %w", err)
	}
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
	if record.BillingMode == BillingModeCostObservation {
		return cancelObservationReservationTx(ctx, tx, service, record, requestID, orgID)
	}
	if err := lockWalletForBilling(ctx, tx, orgID); err != nil {
		return err
	}

	restoredGrant, forfeitedGrant, err := finalizeGrantAllocations(
		ctx, tx, orgID, requestID, record.ReservedGrantedMicros, 0,
	)
	if err != nil {
		return err
	}
	refund, err := money.Add(record.ReservedBalanceMicros, restoredGrant)
	if err != nil {
		return fmt.Errorf("calculate cancellation refund: %w", err)
	}

	var newBalance int64
	err = tx.QueryRow(ctx,
		`UPDATE wallets
		 SET frozen_micros = frozen_micros - $1,
		     balance_micros = balance_micros + $2,
		     granted_balance_micros = granted_balance_micros + $3,
		     updated_at = NOW()
		 WHERE org_id = $4 AND frozen_micros >= $1
		 RETURNING balance_micros`,
		record.FrozenAmountMicros, record.ReservedBalanceMicros, restoredGrant, orgID,
	).Scan(&newBalance)
	if err != nil {
		return fmt.Errorf("unfreeze balance: %w", err)
	}

	var cancelID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (request_id, org_id, type, status, amount_micros, balance_after_micros, note)
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
		     refund_amount_micros = $3,
		     forfeited_grant_amount_micros = $4,
		     terminal_transaction_id = $5,
		     balance_after_micros = $6,
		     terminal_at = NOW(),
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2 AND status = 'frozen'`,
		requestID, orgID, refund, forfeitedGrant, cancelID, newBalance,
	)
	if err != nil {
		return fmt.Errorf("update idempotency: %w", err)
	}
	cancelReq := SettleRequest{
		RequestID:            requestID,
		OrgID:                orgID,
		UsageSource:          metering.UsageSourceMissing,
		UsageAuthority:       metering.UsageAuthorityNone,
		UsagePurpose:         metering.UsagePurposeProviderEvidence,
		UsageFinality:        metering.UsageFinalityMissing,
		UsageContractVersion: metering.UsageContractVersion,
		CacheUsageStatus:     cacheUsageStatusNotReported,
		CacheUsageSchema:     "",
	}
	if err := enqueueUsageJournalTx(
		ctx, tx, service, outbox.EventBillingCancelled, "cancelled",
		record, cancelReq, 0, 0, cancelID,
	); err != nil {
		return fmt.Errorf("enqueue cancelled usage journal: %w", err)
	}

	return tx.Commit(ctx)
}

func cancelObservationReservationTx(
	ctx context.Context,
	tx pgx.Tx,
	service *outbox.Service,
	record reservationRecord,
	requestID string,
	orgID int64,
) error {
	cancelReq := SettleRequest{
		RequestID:            requestID,
		OrgID:                orgID,
		UsageSource:          metering.UsageSourceMissing,
		UsageAuthority:       metering.UsageAuthorityNone,
		UsagePurpose:         metering.UsagePurposeProviderEvidence,
		UsageFinality:        metering.UsageFinalityMissing,
		UsageContractVersion: metering.UsageContractVersion,
		CacheUsageStatus:     cacheUsageStatusNotReported,
	}
	receiptID, err := insertObservationReceiptTx(ctx, tx, record, cancelReq, "cancelled", 0)
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET status = 'cancelled',
		     refund_amount_micros = 0,
		     forfeited_grant_amount_micros = 0,
		     terminal_transaction_id = NULL,
		     terminal_observation_receipt_id = $3,
		     balance_after_micros = NULL,
		     terminal_at = NOW(),
		     updated_at = NOW()
		 WHERE request_id = $1 AND org_id = $2
		   AND billing_mode = 'cost_observation' AND status = 'frozen'`,
		requestID, orgID, receiptID,
	)
	if err != nil {
		return fmt.Errorf("terminalize observation cancellation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("terminalize observation cancellation: reservation state changed")
	}
	if err := enqueueUsageJournalTx(
		ctx, tx, service, outbox.EventBillingCancelled, "cancelled",
		record, cancelReq, 0, 0, 0,
	); err != nil {
		return fmt.Errorf("enqueue observation cancellation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit observation cancellation: %w", err)
	}
	return nil
}

// SetRecoveryAdmission is configured before workers start. The callback must
// verify the registered runtime lease; nil retains isolated/internal usage.
func (b *Billing) SetRecoveryAdmission(allowed func() bool) { b.recoveryAllowed = allowed }
func (b *Billing) canClaimRecovery() bool                   { return b.recoveryAllowed == nil || b.recoveryAllowed() }
