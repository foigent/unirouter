//go:build integration

package enterprisefinance

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDailyPriceBandsRoundTripSelectAndBindImmutableSnapshot(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "daily-price-bands")
	day := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	base := supersedeRequestComponents(50)
	bands := []DailyPriceBand{
		{BandRef: "peak", StartMinuteUTC: 12 * 60, EndMinuteUTC: 24 * 60, Components: supersedeRequestComponents(30)},
		{BandRef: "base", StartMinuteUTC: 0, EndMinuteUTC: 6 * 60, Components: supersedeRequestComponents(20)},
		{BandRef: "valley", StartMinuteUTC: 6 * 60, EndMinuteUTC: 12 * 60, Components: supersedeRequestComponents(5)},
	}
	draft, err := store.CreateRateCardDraft(ctx, RateCard{
		RateCardRef: "scheduled-rate", OrgID: orgID, ProviderRef: "provider-a", ModelRef: "scheduled-model",
		Layer: RateSupplierContract, EffectiveFrom: day, Components: base, DailyPriceBands: bands,
	}, "user:1101")
	require.NoError(t, err)
	require.Equal(t, []string{"base", "valley", "peak"}, []string{
		draft.Card.DailyPriceBands[0].BandRef, draft.Card.DailyPriceBands[1].BandRef, draft.Card.DailyPriceBands[2].BandRef,
	})
	approved, err := store.ApproveRateCard(ctx, orgID, draft.Card.RateCardRef, draft.Card.Version, "user:1102")
	require.NoError(t, err)
	active, err := store.ActivateRateCard(ctx, orgID, approved.Card.RateCardRef, approved.Card.Version, "user:1102", day.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, active.Card.DailyPriceBands, 3)

	listed, err := store.ListLatestRateCards(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, bands[1].StartMinuteUTC, listed[0].Card.DailyPriceBands[0].StartMinuteUTC)

	valley, err := store.ResolveRequestPriceQuote(ctx, orgID, "scheduled-model", day.Add(7*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "valley", valley.SelectedBand.BandRef)
	input, err := RequestPriceMicros(valley, MeterInputTokens)
	require.NoError(t, err)
	require.Equal(t, int64(5), input)

	inputHash := ""
	for _, component := range valley.Components {
		if component.Meter == MeterInputTokens {
			inputHash = component.ComponentHash
		}
	}
	require.NotEmpty(t, inputHash)
	snapshot := PricingSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, RequestRef: "scheduled-request",
		ProjectRef: "project-a", PricedAt: day.Add(7 * time.Hour),
		Bindings: []PricingBinding{{
			Meter: MeterInputTokens, Quantity: "100", RateCardRef: active.Card.RateCardRef,
			RateCardVersion: active.Card.Version, RateComponentHash: inputHash,
			Charge: Amount{ValueMinor: "1", Currency: "CNY", Scale: 1_000_000},
		}},
	}
	snapshot.SnapshotHash, err = PricingSnapshotIdentity(snapshot)
	require.NoError(t, err)
	_, err = store.PersistPricingSnapshot(ctx, snapshot)
	require.NoError(t, err)

	wrongBand := snapshot
	wrongBand.RequestRef = "scheduled-request-wrong-band"
	wrongBand.PricedAt = day.Add(13 * time.Hour)
	wrongBand.SnapshotHash, err = PricingSnapshotIdentity(wrongBand)
	require.NoError(t, err)
	_, err = store.PersistPricingSnapshot(ctx, wrongBand)
	require.ErrorIs(t, err, ErrConflict)
}

func TestDailyPriceBandTamperFailsActivationClosed(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "daily-price-band-tamper")
	day := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	components := supersedeRequestComponents(10)
	draft, err := store.CreateRateCardDraft(ctx, RateCard{
		RateCardRef: "tampered-schedule", OrgID: orgID, ProviderRef: "provider-a", ModelRef: "tampered-model",
		Layer: RateSupplierContract, EffectiveFrom: day, Components: components,
		DailyPriceBands: []DailyPriceBand{{
			BandRef: "whole-day", StartMinuteUTC: 0, EndMinuteUTC: 1440, Components: components,
		}},
	}, "user:1201")
	require.NoError(t, err)
	approved, err := store.ApproveRateCard(ctx, orgID, draft.Card.RateCardRef, draft.Card.Version, "user:1202")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE enterprise_finance_rate_card_versions
		SET daily_price_bands = jsonb_set(daily_price_bands, '{0,end_minute_utc}', '1439'::jsonb)
		WHERE org_id=$1 AND rate_card_ref=$2 AND version=$3`, orgID, approved.Card.RateCardRef, approved.Card.Version)
	require.NoError(t, err)

	_, err = store.ActivateRateCard(ctx, orgID, approved.Card.RateCardRef, approved.Card.Version, "user:1202", day.Add(time.Hour))
	require.ErrorIs(t, err, ErrInvalid)
	assertLatestRateState(t, ctx, pool, orgID, approved.Card.RateCardRef, approved.Card.Version, RateApproved)
}

func TestWeekdayPriceBandsRoundTripAndSelectWeekendWithoutPeak(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "weekday-price-bands")
	monday := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	base := supersedeRequestComponents(5)
	peak := supersedeRequestComponents(10)
	weekdays := []int{1, 2, 3, 4, 5}
	bands := []DailyPriceBand{
		{BandRef: "weekend", WeekdaysUTC: []int{0, 6}, StartMinuteUTC: 0, EndMinuteUTC: 1440, Components: base},
		{BandRef: "weekday-base-before", WeekdaysUTC: weekdays, StartMinuteUTC: 0, EndMinuteUTC: 60, Components: base},
		{BandRef: "weekday-peak", WeekdaysUTC: weekdays, StartMinuteUTC: 60, EndMinuteUTC: 240, Components: peak},
		{BandRef: "weekday-base-after", WeekdaysUTC: weekdays, StartMinuteUTC: 240, EndMinuteUTC: 1440, Components: base},
	}
	draft, err := store.CreateRateCardDraft(ctx, RateCard{
		RateCardRef: "weekday-rate", OrgID: orgID, ProviderRef: "deepseek", ModelRef: "DeepSeek V4 Flash",
		Layer: RateSupplierContract, EffectiveFrom: monday.Add(-7 * 24 * time.Hour), Components: base, DailyPriceBands: bands,
	}, "user:1301")
	require.NoError(t, err)
	require.Equal(t, []int{0, 6}, draft.Card.DailyPriceBands[0].WeekdaysUTC)
	approved, err := store.ApproveRateCard(ctx, orgID, draft.Card.RateCardRef, draft.Card.Version, "user:1302")
	require.NoError(t, err)
	_, err = store.ActivateRateCard(ctx, orgID, approved.Card.RateCardRef, approved.Card.Version, "user:1302", monday)
	require.NoError(t, err)

	weekday, err := store.ResolveRequestPriceQuote(ctx, orgID, "DeepSeek V4 Flash", monday.Add(2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "weekday-peak", weekday.SelectedBand.BandRef)
	weekend, err := store.ResolveRequestPriceQuote(ctx, orgID, "DeepSeek V4 Flash", monday.Add(-24*time.Hour+2*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "weekend", weekend.SelectedBand.BandRef)
}
