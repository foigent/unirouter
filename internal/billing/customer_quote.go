package billing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

const (
	customerQuoteVersionPrefix    = "ccq1_"
	customerAuthorizationExceeded = OutputHoldReasonCustomerAuthorizationExceeded
	customerQuoteContract         = "unirouter.customer-charge-quote.v1"
)

const OutputHoldReasonCustomerAuthorizationExceeded = "customer_authorization_exceeded"

// ErrCustomerAuthorizationExceeded confirms provider usage was retained for manual review.
var ErrCustomerAuthorizationExceeded = errors.New("provider usage exceeds the authorized customer quote")

// HasCustomerChargeQuote reports which financial contract owns this reservation.
func (result FreezeResult) HasCustomerChargeQuote() bool {
	return customerQuotePricing(result.PricingVersion)
}

type customerQuoteReceipt struct {
	ContractVersion       string                                   `json:"contract_version"`
	QuoteID               string                                   `json:"customer_charge_quote_id"`
	PricingVersion        string                                   `json:"pricing_version"`
	SelectorSchemaVersion string                                   `json:"selector_schema_version"`
	SelectorHash          string                                   `json:"selector_sha256"`
	PlanResolverVersion   string                                   `json:"plan_resolver_version"`
	RequestID             string                                   `json:"request_id"`
	OrgID                 int64                                    `json:"org_id"`
	ProjectID             int64                                    `json:"project_id"`
	APIKeyID              int64                                    `json:"api_key_id"`
	Selection             enterprisefinance.CustomerPriceSelection `json:"selection"`
	PricedAt              time.Time                                `json:"priced_at"`
	Currency              string                                   `json:"currency"`
	AmountScale           int64                                    `json:"amount_scale"`
	EstimatedInputTokens  int                                      `json:"estimated_input_tokens"`
	ReservedOutputTokens  int                                      `json:"reserved_output_tokens"`
	QuotedCustomerCost    string                                   `json:"quoted_customer_cost_micros"`
	MaximumAuthorizedCost string                                   `json:"maximum_authorized_customer_cost_micros"`
}

func customerQuotePricing(version string) bool {
	return strings.HasPrefix(version, customerQuoteVersionPrefix)
}

func customerRequestTier(req FreezeRequest) string {
	if req.ProcessingTier == "" {
		return enterprisefinance.CustomerPricingTierStandard
	}
	return req.ProcessingTier
}

func (b *Billing) queryCustomerChargeQuote(ctx context.Context, req FreezeRequest) (priceQuote, error) {
	if len(req.ReservedMeterUsage) != 0 {
		return priceQuote{}, ErrModelPriceNotConfigured
	}
	store, err := enterprisefinance.NewPostgresStore(b.db)
	if err != nil {
		return priceQuote{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	selection, err := store.ResolveCustomerPrice(ctx, req.OrgID, req.ProjectID, req.Model, req.ModelOperation, customerRequestTier(req), now)
	if errors.Is(err, enterprisefinance.ErrRequestPricingNotReady) {
		return priceQuote{}, ErrModelPriceNotConfigured
	}
	if err != nil {
		return priceQuote{}, err
	}
	stable, err := json.Marshal(selection)
	if err != nil {
		return priceQuote{}, err
	}
	digest := sha256.Sum256(stable)
	version := fmt.Sprintf("%s%x", customerQuoteVersionPrefix, digest)
	rates, err := enterprisefinance.CustomerPriceTokenRates(selection)
	if err != nil {
		return priceQuote{}, err
	}
	input, read, creation := rates[enterprisefinance.MeterInputTokens], rates[enterprisefinance.MeterCacheReadTokens], rates[enterprisefinance.MeterCacheCreationTokens]
	var readRatio, createRatio float64
	if input > 0 {
		readRatio = float64(read) / float64(input)
		createRatio = float64(creation) / float64(input)
	}

	return priceQuote{
		InputPriceMicrosPerMillion: input, CacheReadPriceMicrosPerMillion: read, CacheCreationPriceMicrosPerMillion: creation,
		OutputPriceMicrosPerMillion: rates[enterprisefinance.MeterOutputTokens], CacheReadRatio: readRatio, CacheCreationRatio: createRatio,
		Multiplier: "1", Version: version,
		customerQuote: &customerQuoteReceipt{
			ContractVersion: customerQuoteContract, PricingVersion: version,
			SelectorSchemaVersion: "unirouter.customer-price-selector.v1", SelectorHash: fmt.Sprintf("sha256:%x", digest),
			PlanResolverVersion: "unirouter.project-then-org-price-binding.v1",
			RequestID:           req.RequestID, OrgID: req.OrgID, ProjectID: req.ProjectID, APIKeyID: req.APIKeyID,
			Selection: selection, PricedAt: now, Currency: "CNY", AmountScale: 1_000_000,
			EstimatedInputTokens: req.EstimatedInputTokens, ReservedOutputTokens: req.ReservedCompletionTokens,
		},
	}, nil
}

func insertCustomerQuoteReceipt(ctx context.Context, tx pgx.Tx, quote customerQuoteReceipt, calculated, maximum int64) error {
	quote.QuotedCustomerCost = strconv.FormatInt(calculated, 10)
	quote.MaximumAuthorizedCost = strconv.FormatInt(maximum, 10)
	identity, err := json.Marshal(quote)
	if err != nil {
		return err
	}
	quote.QuoteID = fmt.Sprintf("sha256:%x", sha256.Sum256(identity))
	wire, err := json.Marshal(quote)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO customer_charge_quote_receipts
		(org_id,request_id,pricing_version,maximum_authorized_micros,receipt) VALUES ($1,$2,$3,$4,$5)`,
		quote.OrgID, quote.RequestID, quote.PricingVersion, maximum, wire)
	return err
}

type customerQuoteReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func customerQuoteMatchesRequest(ctx context.Context, db customerQuoteReader, req FreezeRequest, version string) (bool, error) {
	if !customerQuotePricing(version) {
		return true, nil
	}
	var projectID int64
	var model, tier string
	var operation modeloperation.ID
	err := db.QueryRow(ctx, `SELECT (receipt->>'project_id')::bigint, receipt#>>'{selection,rate,public_model}',
		receipt#>>'{selection,rate,operation}', receipt#>>'{selection,tier}'
		FROM customer_charge_quote_receipts WHERE org_id=$1 AND request_id=$2 AND pricing_version=$3`,
		req.OrgID, req.RequestID, version).Scan(&projectID, &model, &operation, &tier)
	if err != nil {
		return false, fmt.Errorf("resolve frozen customer quote: %w", err)
	}
	return projectID == req.ProjectID && model == strings.TrimSpace(req.Model) &&
		operation == req.ModelOperation && tier == customerRequestTier(req), nil
}

func customerQuoteOverrun(ctx context.Context, tx pgx.Tx, record reservationRecord, req SettleRequest, cost int64) (int64, error) {
	if !customerQuotePricing(record.PricingVersion) {
		return 0, nil
	}
	if req.UsageAuthority != metering.UsageAuthorityProviderReported || req.UsageFinality != metering.UsageFinalityFinal {
		return 0, ErrProviderFinalUsageRequired
	}
	var maximum int64
	if err := tx.QueryRow(ctx, `SELECT maximum_authorized_micros FROM customer_charge_quote_receipts
		WHERE org_id=$1 AND request_id=$2 AND pricing_version=$3`, req.OrgID, req.RequestID, record.PricingVersion).Scan(&maximum); err != nil {
		return 0, fmt.Errorf("read authorized customer maximum: %w", err)
	}
	if maximum != record.FrozenAmountMicros {
		return 0, errors.New("customer authorization and reservation differ")
	}
	if record.BillingMode == BillingModePrepaidEnforced && cost > maximum {
		return cost - maximum, nil
	}
	return 0, nil
}

func (b *Billing) holdCustomerAuthorizationOverrun(ctx context.Context, tx pgx.Tx, record reservationRecord, req SettleRequest, cost, overrun int64, breakdown reservationCostBreakdown) error {
	if err := persistFundingPendingTx(ctx, tx, b.outbox, record, req, cost, overrun, breakdown, customerAuthorizationExceeded); err != nil {
		return err
	}
	if err := markProviderAttemptFundingPendingTx(ctx, tx, req, cost); err != nil {
		return err
	}
	if err := updateRequestIdempotencyUsageTx(ctx, tx, req); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
