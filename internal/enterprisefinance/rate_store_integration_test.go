//go:build integration

package enterprisefinance

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/testutil"
)

type ratePriceQueryCounter struct {
	db.DBQuerier
	calls int
}

func (counter *ratePriceQueryCounter) Query(ctx context.Context, statement string, args ...any) (pgx.Rows, error) {
	counter.calls++
	return counter.DBQuerier.Query(ctx, statement, args...)
}

func TestPostgresStoreRequestPriceQuoteMatchesLatestCardAuthority(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	org := insertFinanceTestOrg(t, ctx, pool, "quote-model")
	otherOrg := insertFinanceTestOrg(t, ctx, pool, "quote-other-org")
	start := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	components := []RateComponent{
		{Meter: MeterInputTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "9007199254740993", Currency: "CNY", Scale: 1_000_000}},
		{Meter: MeterOutputTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "2", Currency: "CNY", Scale: 1_000_000}},
		{Meter: MeterCacheReadTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "1", Currency: "CNY", Scale: 1_000_000}},
		{Meter: MeterCacheCreationTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "3", Currency: "CNY", Scale: 1_000_000}},
	}
	insertCard := func(tenant, version int64, ref, model, provider string, state string, rates []RateComponent) {
		t.Helper()
		var predecessor *int64
		if version > 1 {
			previousVersion := version - 1
			predecessor = &previousVersion
		}
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		_, err = insertRateCardVersion(ctx, tx, RateCard{
			RateCardRef: ref, OrgID: tenant, Version: version, ProviderRef: provider, ModelRef: model,
			Layer: RateSupplierContract, State: state, EffectiveFrom: start,
			SourceEvidenceRef: "evidence:test", CreatedBy: "user:1", ApprovedBy: "user:2", Components: rates,
		}, "user:2", predecessor, &start)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
	}
	insertCard(org, 1, "rate-target", "target", "provider-a", RateActive, components)
	_, err = pool.Exec(ctx, `UPDATE enterprise_finance_rate_card_versions
		SET model_ref=$1 WHERE org_id=$2 AND rate_card_ref='rate-target'`, "\ttarget\u00a0", org)
	require.NoError(t, err)
	insertCard(otherOrg, 1, "rate-foreign", "target", "provider-z", RateActive, components)
	insertCard(org, 1, "rate-remapped", "target", "provider-b", RateActive, components)
	insertCard(org, 2, "rate-remapped", "other", "provider-b", RateActive, components)
	insertCard(org, 1, "rate-retired", "target", "provider-c", RateActive, components)
	insertCard(org, 2, "rate-retired", "target", "provider-c", RateRetired, components)
	insertCard(org, 1, "rate-incomplete", "incomplete", "provider-c", RateActive, nil)

	records, err := store.ListLatestRateCards(ctx, org)
	require.NoError(t, err)
	want, err := ResolveRequestPriceQuote(records, org, "target", start)
	require.NoError(t, err)
	counter := &ratePriceQueryCounter{DBQuerier: pool}
	requestStore, err := NewPostgresStore(counter)
	require.NoError(t, err)
	got, err := requestStore.ResolveRequestPriceQuote(ctx, org, " target ", start)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, 1, counter.calls, "quote resolution must use one query")
	_, err = store.ResolveRequestPriceQuote(ctx, org, "missing", start)
	require.ErrorIs(t, err, ErrRequestPricingNotReady)
	_, err = store.ResolveRequestPriceQuote(ctx, org, "incomplete", start)
	require.Error(t, err)

	insertCard(org, 1, "rate-ambiguous", "target", "provider-d", RateActive, components)
	_, err = store.ResolveRequestPriceQuote(ctx, org, "target", start)
	require.ErrorIs(t, err, ErrRequestPricingAmbiguous)
}

func TestEnterpriseRateCardsActivateWithoutChangingExistingSettlementAndBindImmutablePricing(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "finance-rate-card")
	effectiveFrom := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	component := RateComponent{
		Meter: "input_tokens", UnitQuantity: "1000000",
		Amount: Amount{ValueMinor: "9007199254740993", Currency: "CNY", Scale: 1_000_000},
	}
	components := []RateComponent{
		component,
		{Meter: MeterOutputTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "9007199254740994", Currency: "CNY", Scale: 1_000_000}},
		{Meter: MeterCacheReadTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "900719925474099", Currency: "CNY", Scale: 1_000_000}},
		{Meter: MeterCacheCreationTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "9007199254740993", Currency: "CNY", Scale: 1_000_000}},
	}
	draft, err := store.CreateRateCardDraft(ctx, RateCard{
		RateCardRef: "RATE-OPENAI-GPT", OrgID: orgID, ProviderRef: "openai", ModelRef: "gpt-enterprise",
		Layer: RateSupplierContract, EffectiveFrom: effectiveFrom, Components: components,
	}, "user:401")
	require.NoError(t, err)
	require.Equal(t, RateDraft, draft.Card.State)
	_, err = store.ApproveRateCard(ctx, orgID, draft.Card.RateCardRef, 1, "user:401")
	require.ErrorIs(t, err, ErrMakerChecker)
	approved, err := store.ApproveRateCard(ctx, orgID, draft.Card.RateCardRef, 1, "user:402")
	require.NoError(t, err)
	require.Equal(t, int64(2), approved.Card.Version)
	active, err := store.ActivateRateCard(ctx, orgID, draft.Card.RateCardRef, 2, "user:402", effectiveFrom.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, RateActive, active.Card.State)
	require.Equal(t, int64(3), active.Card.Version)

	componentHash, err := RateComponentIdentity(component)
	require.NoError(t, err)
	snapshot := PricingSnapshot{
		ContractVersion: ContractVersion, RequestRef: "request-rate-1", OrgID: orgID,
		ProjectRef: "project-7", PricedAt: effectiveFrom.Add(2 * time.Hour), SnapshotHash: "pending",
		Bindings: []PricingBinding{{
			Meter: "input_tokens", Quantity: "9223372036854775807",
			RateCardRef: active.Card.RateCardRef, RateCardVersion: active.Card.Version,
			RateComponentHash: componentHash,
			Charge:            Amount{ValueMinor: "18446744073709551615", Currency: "CNY", Scale: 1_000_000},
		}},
	}
	snapshot.SnapshotHash, err = PricingSnapshotIdentity(snapshot)
	require.NoError(t, err)
	persisted, err := store.PersistPricingSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.Equal(t, PricingPersistApplied, persisted.Result)
	replay, err := store.PersistPricingSnapshot(ctx, snapshot)
	require.NoError(t, err)
	require.Equal(t, PricingPersistDuplicate, replay.Result)
	loaded, err := store.GetPricingSnapshot(ctx, orgID, snapshot.RequestRef)
	require.NoError(t, err)
	require.Equal(t, snapshot.SnapshotHash, loaded.Snapshot.SnapshotHash)
	require.Equal(t, "9223372036854775807", loaded.Snapshot.Bindings[0].Quantity)
	require.Equal(t, "18446744073709551615", loaded.Snapshot.Bindings[0].Charge.ValueMinor)

	changed := snapshot
	changed.Bindings = append([]PricingBinding(nil), snapshot.Bindings...)
	changed.Bindings[0].Charge.ValueMinor = "18446744073709551616"
	changed.SnapshotHash, err = PricingSnapshotIdentity(changed)
	require.NoError(t, err)
	_, err = store.PersistPricingSnapshot(ctx, changed)
	require.ErrorIs(t, err, ErrConflict)

	var callLogs, transactions int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs WHERE org_id=$1`, orgID).Scan(&callLogs))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE org_id=$1`, orgID).Scan(&transactions))
	require.Zero(t, callLogs)
	require.Zero(t, transactions)
}

func TestEnterpriseRateCardActivationRejectsExactEconomicWindowCollision(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "finance-rate-collision")
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	createActive := func(ref, meter string) RateCardRecord {
		t.Helper()
		draft, createErr := store.CreateRateCardDraft(ctx, RateCard{
			RateCardRef: ref, OrgID: orgID, ProviderRef: "provider-a", ModelRef: "model-a",
			Layer: RateInternalShowback, EffectiveFrom: start,
			Components: []RateComponent{{
				Meter: meter, UnitQuantity: "1000",
				Amount: Amount{ValueMinor: "10", Currency: "CNY", Scale: 1_000_000},
			}},
		}, "user:501")
		require.NoError(t, createErr)
		approved, approveErr := store.ApproveRateCard(ctx, orgID, ref, draft.Card.Version, "user:502")
		require.NoError(t, approveErr)
		active, activateErr := store.ActivateRateCard(ctx, orgID, ref, approved.Card.Version, "user:502", start.Add(time.Hour))
		require.NoError(t, activateErr)
		return active
	}
	createActive("RATE-A", "input_tokens")

	draft, err := store.CreateRateCardDraft(ctx, RateCard{
		RateCardRef: "RATE-B", OrgID: orgID, ProviderRef: "provider-a", ModelRef: "model-a",
		Layer: RateInternalShowback, EffectiveFrom: start,
		Components: []RateComponent{{
			Meter: "input_tokens", UnitQuantity: "1000",
			Amount: Amount{ValueMinor: "9", Currency: "CNY", Scale: 1_000_000},
		}},
	}, "user:503")
	require.NoError(t, err)
	approved, err := store.ApproveRateCard(ctx, orgID, draft.Card.RateCardRef, 1, "user:504")
	require.NoError(t, err)
	_, err = store.ActivateRateCard(ctx, orgID, draft.Card.RateCardRef, approved.Card.Version, "user:504", start.Add(2*time.Hour))
	require.ErrorIs(t, err, ErrConflict)

	// A different meter is not an economic collision and remains independently versioned.
	other := createActive("RATE-C", "output_tokens")
	require.Equal(t, RateActive, other.Card.State)
}

func TestOfficialRateDraftImportIsAtomicEvidenceBackedAndNeverActive(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "official-rate-import")
	fetchedAt := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	components := []RateComponent{
		{Meter: MeterInputTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "10", Currency: "CNY", Scale: 1_000_000}},
		{Meter: MeterOutputTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "20", Currency: "CNY", Scale: 1_000_000}},
		{Meter: MeterCacheReadTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "2", Currency: "CNY", Scale: 1_000_000}},
		{Meter: MeterCacheCreationTokens, UnitQuantity: "1000000", Amount: Amount{ValueMinor: "12", Currency: "CNY", Scale: 1_000_000}},
		{Meter: MeterBuiltinWebSearchCalls, UnitQuantity: "1", Amount: Amount{ValueMinor: "30000", Currency: "CNY", Scale: 1_000_000}},
	}
	record, err := store.ImportOfficialRateCardDraft(ctx, OfficialRateDraftImport{
		Card: RateCard{
			RateCardRef: "official-rate", OrgID: orgID, ProviderRef: "provider-a", ModelRef: "model-a",
			EffectiveFrom: fetchedAt, Components: components,
		},
		Evidence: RateSourceEvidence{
			EvidenceRef: "official-source", SourceURL: "https://example.invalid/pricing",
			SourceRevision: "revision-7", FetchedAt: fetchedAt,
			PayloadSHA256:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			TransformVersion: "official-transform.v1", LastSuccessAt: fetchedAt,
			StaleAfter: fetchedAt.Add(24 * time.Hour),
		},
	}, "user:701")
	require.NoError(t, err)
	require.Equal(t, RateDraft, record.Card.State)
	require.Equal(t, RateProviderList, record.Card.Layer)
	require.Empty(t, record.Card.ApprovedBy)
	require.Nil(t, record.ActivatedAt)
	require.NotNil(t, record.SourceEvidence)
	require.Equal(t, RateSourceUnitComponents, record.SourceEvidence.UnitContract)

	listed, err := store.ListLatestRateCards(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.NotNil(t, listed[0].SourceEvidence)
	require.Equal(t, "revision-7", listed[0].SourceEvidence.SourceRevision)
	require.Equal(t, RateSourceUnitComponents, listed[0].SourceEvidence.UnitContract)

	usdComponents := make([]RateComponent, len(components)-1)
	for index, component := range components[:len(components)-1] {
		usdComponents[index] = component
		usdComponents[index].Amount.Currency = "USD"
	}
	usdRecord, err := store.ImportOfficialRateCardDraft(ctx, OfficialRateDraftImport{
		Card: RateCard{
			RateCardRef: "official-usd-rate", OrgID: orgID, ProviderRef: "openai", ModelRef: "gpt-5.6-sol",
			EffectiveFrom: fetchedAt, Components: usdComponents,
		},
		Evidence: RateSourceEvidence{
			EvidenceRef: "official-usd-source", SourceURL: "https://example.invalid/usd-pricing",
			SourceRevision: "revision-usd-1", FetchedAt: fetchedAt,
			PayloadSHA256:    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			TransformVersion: "official-transform.v1", LastSuccessAt: fetchedAt,
			StaleAfter: fetchedAt.Add(24 * time.Hour),
		},
	}, "user:703")
	require.NoError(t, err)
	require.Equal(t, "USD", usdRecord.SourceEvidence.Currency)
	require.Equal(t, RateDraft, usdRecord.Card.State)
	_, err = store.ApproveRateCard(ctx, orgID, usdRecord.Card.RateCardRef, usdRecord.Card.Version, "user:704")
	require.ErrorIs(t, err, ErrRequestPricingNotReady)

	// A broken card causes the entire import to roll back, including evidence.
	broken := OfficialRateDraftImport{
		Card: RateCard{
			RateCardRef: "broken-rate", OrgID: orgID, ProviderRef: "provider-a", ModelRef: "model-b",
			EffectiveFrom: fetchedAt, Components: components[:2],
		},
		Evidence: RateSourceEvidence{
			EvidenceRef: "broken-source", SourceURL: "https://example.invalid/pricing",
			SourceRevision: "revision-8", FetchedAt: fetchedAt,
			PayloadSHA256:    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			TransformVersion: "official-transform.v1", LastSuccessAt: fetchedAt,
			StaleAfter: fetchedAt.Add(24 * time.Hour),
		},
	}
	_, err = store.ImportOfficialRateCardDraft(ctx, broken, "user:702")
	require.ErrorIs(t, err, ErrRequestPricingNotReady)
	var brokenEvidence int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM enterprise_finance_rate_source_evidence
		WHERE org_id=$1 AND evidence_ref='broken-source'`, orgID).Scan(&brokenEvidence))
	require.Zero(t, brokenEvidence)
}
