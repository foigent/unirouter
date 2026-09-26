package sourcefacts

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

var priceMultiplierPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func sourceText(value string, limit int, required bool) bool {
	minimum := 0
	if required {
		minimum = 1
	}
	_, err := eventconnector.NewOpaqueString("source field", value, minimum, limit, true)
	return err == nil && strings.TrimSpace(value) == value
}

func nonnegativeDecimal(value string) bool {
	n, e := strconv.ParseInt(value, 10, 64)
	return e == nil && n >= 0 && strconv.FormatInt(n, 10) == value
}

func (c CostDocument) Validate() error {
	if (c.JournalBillingMode == "") != (c.JournalModeSource == "") ||
		c.JournalBillingMode != "" && (!slices.Contains([]string{string(billing.BillingModePrepaidEnforced), string(billing.BillingModeCostObservation)}, c.JournalBillingMode) || !slices.Contains([]string{"journal_payload", "legacy_interpretation.v1"}, c.JournalModeSource)) {
		return errors.New("invalid journal mode provenance")
	}
	if c.Reference.Source == "completed_execution" && (c.MediaAmountState != "" || c.Reference.ChargeFactRef != "") {
		return errors.New("text cost has media fields")
	}
	if c.Reference.Source == "async_media_charge_fact" && (!slices.Contains([]string{"not_eligible", "pricing_pending_media_rate_card"}, c.MediaAmountState) || (c.Reference.ChargeFactRef == "") != (c.MediaAmountState == "not_eligible") || len(c.Reference.ChargeFactRef) > 96 || c.Basis != nil) {
		return errors.New("invalid media amount evidence")
	}
	if !sourceText(c.Model, 256, true) || !sourceText(c.TerminalStatus, 64, true) ||
		!slices.Contains([]string{"available", "persisted_legacy", "not_captured", "not_applicable"}, c.BasisStatus) ||
		(c.Basis == nil) != (c.BasisStatus == "not_captured" || c.BasisStatus == "not_applicable") {
		return errors.New("invalid cost basis status")
	}
	if c.Basis == nil {
		return nil
	}
	b := c.Basis
	if c.BasisStatus == "available" && (b.ModeSource != "reservation" || b.PricingState == "unknown") ||
		c.BasisStatus == "persisted_legacy" && (b.ModeSource != "persisted_reservation" || b.PricingAuthority != "not_captured" || b.PricingState != "unknown") {
		return errors.New("cost basis provenance mismatch")
	}
	if b.PricingState == "unpriced" && (b.BillingMode != string(billing.BillingModeCostObservation) || b.PricingAuthority != string(billing.RequestPricingAuthorityLegacy) || !strings.HasPrefix(b.PricingVersion, "uo1_") || len(b.PricingVersion) <= 4) {
		return errors.New("unpriced interpretation is outside the legacy observation writer")
	}
	if !slices.Contains([]string{string(billing.BillingModePrepaidEnforced), string(billing.BillingModeCostObservation)}, b.BillingMode) ||
		!slices.Contains([]string{"reservation", "persisted_reservation"}, b.ModeSource) ||
		!slices.Contains([]string{"priced", "unpriced", "unknown"}, b.PricingState) || b.PricingAuthority == "" || len(b.PricingAuthority) > 64 || len(b.PricingVersion) > 256 ||
		!sourceText(b.PricingAuthority, 64, true) || !sourceText(b.PricingVersion, 256, false) ||
		!sourceText(b.RateCardRef, 128, false) || !sourceText(b.RateCardLayer, 64, false) || !sourceText(b.ExecutionSnapshotHash, 128, false) ||
		b.RateCardVersion != "" && !positiveID(b.RateCardVersion) || b.Currency != "CNY" || b.AmountScale != "1000000" || b.UnitRates == nil || len(b.UnitRates) > 64 {
		return errors.New("invalid cost basis contract")
	}
	for _, value := range []string{b.InputRate, b.CacheReadRate, b.CacheCreationRate, b.OutputRate} {
		if !nonnegativeDecimal(value) {
			return errors.New("invalid cost rate")
		}
	}
	if len(b.Multiplier) > 64 || !priceMultiplierPattern.MatchString(b.Multiplier) {
		return errors.New("invalid price multiplier")
	}
	rates := make([]billing.UnitRate, 0, len(b.UnitRates))
	for index, r := range b.UnitRates {
		if !sourceText(r.Meter, 64, true) || !positiveID(r.UnitQuantity) || !nonnegativeDecimal(r.AmountMinor) || r.Currency != "CNY" || r.AmountScale != "1000000" || !sourceText(r.ComponentHash, 128, true) || index > 0 && b.UnitRates[index-1].Meter >= r.Meter {
			return errors.New("invalid unit rate")
		}
		unit, _ := strconv.ParseInt(r.UnitQuantity, 10, 64)
		amount, _ := strconv.ParseInt(r.AmountMinor, 10, 64)
		rates = append(rates, billing.UnitRate{Meter: r.Meter, UnitQuantity: unit, AmountMinor: amount, Currency: r.Currency, AmountScale: 1000000, ComponentHash: r.ComponentHash})
	}
	_, err := billing.CanonicalUnitRates(rates)
	return err
}
