package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/money"
)

// AdmissionReadiness is a side-effect-free view of the exact pricing and
// wallet authorities used by Freeze. It never reserves funds, expires grants,
// writes receipts, or evaluates a request-sized balance requirement.
type AdmissionReadiness struct {
	BillingMode             BillingMode
	PricingAuthority        RequestPricingAuthority
	PriceConfigured         bool
	PricingVersion          string
	SelectionKey            string
	RateCardRef             string
	RateCardVersion         int64
	RateCardLayer           string
	ProviderRef             string
	WalletAdmissionRequired bool
	WalletExists            bool
	SpendableBalanceMicros  int64
}

// InspectAdmissionReadiness resolves the same current price quote selected by
// Freeze and computes the balance that Freeze would observe after its lazy
// expired-grant reconciliation. It intentionally does not claim that a future
// request of unknown size will fit the balance.
func (b *Billing) InspectAdmissionReadiness(ctx context.Context, orgID int64, model string) (AdmissionReadiness, error) {
	if b == nil {
		return AdmissionReadiness{}, fmt.Errorf("billing readiness database is unavailable")
	}
	result := AdmissionReadiness{
		BillingMode: b.mode, PricingAuthority: b.requestPricingAuthority,
		WalletAdmissionRequired: b.mode == BillingModePrepaidEnforced,
	}
	if b.requestPricingAuthority == RequestPricingAuthorityRateCard {
		result.SelectionKey = enterprisefinance.RequestPricingKey
	}
	if b.db == nil {
		return result, fmt.Errorf("billing readiness database is unavailable")
	}
	if orgID <= 0 || strings.TrimSpace(model) == "" {
		return result, fmt.Errorf("billing readiness input is invalid")
	}
	if err := ValidateBillingMode(b.mode); err != nil {
		return result, err
	}
	if err := ValidateRequestPricingAuthority(b.requestPricingAuthority); err != nil {
		return result, err
	}

	quote, err := b.queryPriceQuoteForOrg(ctx, orgID, model)
	if err != nil {
		if !errors.Is(err, ErrModelPriceNotConfigured) {
			return result, err
		}
	} else {
		result.PriceConfigured = !strings.HasPrefix(quote.Version, "uo1_")
		result.PricingVersion = quote.Version
		result.SelectionKey = quote.SelectionKey
		result.RateCardRef = quote.RateCardRef
		result.RateCardVersion = quote.RateCardVersion
		result.RateCardLayer = quote.RateCardLayer
		result.ProviderRef = quote.ProviderRef
	}

	if !result.WalletAdmissionRequired {
		return result, nil
	}

	var cash, granted, expired int64
	err = b.db.QueryRow(ctx, `
		SELECT w.balance_micros,w.granted_balance_micros,
		       COALESCE((
		         SELECT SUM(g.balance_micros)
		           FROM grants g
		          WHERE g.org_id=w.org_id AND g.status='active'
		            AND g.expires_at IS NOT NULL AND g.expires_at<NOW()
		       ),0)
		  FROM wallets w
		 WHERE w.org_id=$1`, orgID).Scan(&cash, &granted, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read billing admission balance: %w", err)
	}
	result.WalletExists = true
	granted -= minInt64(granted, expired)
	result.SpendableBalanceMicros, err = money.Add(cash, granted)
	if err != nil {
		return AdmissionReadiness{}, fmt.Errorf("calculate billing admission balance: %w", err)
	}
	return result, nil
}
