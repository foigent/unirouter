//go:build integration

package enterprisefinance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRateCardSupersedeAtomicallyRetiresExactActiveAndPreservesOldSnapshotAuthority(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "rate-supersede")
	base := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)

	oldApproved := createApprovedRequestRate(t, ctx, store, orgID, "rate-old", "provider-a", "model-a", RateSupplierContract, base, 10)
	oldActive, err := store.ActivateRateCard(ctx, orgID, oldApproved.Card.RateCardRef, oldApproved.Card.Version, "user:902", base.Add(time.Hour))
	require.NoError(t, err)
	newApproved := createApprovedRequestRate(t, ctx, store, orgID, "rate-new", "provider-a", "model-a", RateSupplierContract, base.Add(2*time.Hour), 20)

	_, err = store.ActivateRateCard(ctx, orgID, newApproved.Card.RateCardRef, newApproved.Card.Version, "user:902", base.Add(3*time.Hour))
	require.ErrorIs(t, err, ErrRequestPricingAmbiguous, "replacement must never guess the current active card")

	newActive, err := store.ActivateRateCardWithSupersede(
		ctx, orgID, newApproved.Card.RateCardRef, newApproved.Card.Version,
		RateCardActivationOptions{
			SupersedesRateCardRef:     oldActive.Card.RateCardRef,
			ExpectedSupersededVersion: oldActive.Card.Version,
		},
		"user:902", base.Add(3*time.Hour),
	)
	require.NoError(t, err)
	require.Equal(t, RateActive, newActive.Card.State)

	latest, err := store.ListLatestRateCards(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, latest, 2)
	byRef := make(map[string]RateCardRecord, len(latest))
	for _, record := range latest {
		byRef[record.Card.RateCardRef] = record
	}
	require.Equal(t, RateRetired, byRef[oldActive.Card.RateCardRef].Card.State)
	require.Equal(t, oldActive.Card.Version+1, byRef[oldActive.Card.RateCardRef].Card.Version)
	require.Equal(t, RateActive, byRef[newActive.Card.RateCardRef].Card.State)

	resolved, err := store.ResolveRequestPriceQuote(ctx, orgID, "model-a", base.Add(4*time.Hour))
	require.NoError(t, err)
	require.Equal(t, newActive.Card.RateCardRef, resolved.RateCardRef)

	// The superseded active row remains immutable evidence. A snapshot priced
	// before replacement can still bind and validate that exact historical
	// version even though the latest version for the old ref is retired.
	oldHash, err := RateComponentIdentity(supersedeRequestComponents(10)[0])
	require.NoError(t, err)
	snapshot := PricingSnapshot{
		ContractVersion: ContractVersion, OrgID: orgID, RequestRef: "old-rate-snapshot",
		ProjectRef: "project-a", PricedAt: base.Add(90 * time.Minute),
		Bindings: []PricingBinding{{
			Meter: MeterInputTokens, Quantity: "100", RateCardRef: oldActive.Card.RateCardRef,
			RateCardVersion: oldActive.Card.Version, RateComponentHash: oldHash,
			Charge: Amount{ValueMinor: "1", Currency: "CNY", Scale: 1_000_000},
		}},
	}
	snapshot.SnapshotHash, err = PricingSnapshotIdentity(snapshot)
	require.NoError(t, err)
	_, err = store.PersistPricingSnapshot(ctx, snapshot)
	require.NoError(t, err)
}

func TestRateCardSupersedeRejectsWrongTargetVersionFutureCandidateAndUnnecessaryDeclaration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "rate-supersede-negative")
	base := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)

	oldApproved := createApprovedRequestRate(t, ctx, store, orgID, "negative-old", "provider-a", "model-negative", RateSupplierContract, base, 10)
	oldActive, err := store.ActivateRateCard(ctx, orgID, oldApproved.Card.RateCardRef, oldApproved.Card.Version, "user:902", base.Add(time.Hour))
	require.NoError(t, err)
	candidate := createApprovedRequestRate(t, ctx, store, orgID, "negative-new", "provider-b", "model-negative", RateSupplierContract, base, 20)

	for name, options := range map[string]RateCardActivationOptions{
		"wrong target":  {SupersedesRateCardRef: "another-rate", ExpectedSupersededVersion: oldActive.Card.Version},
		"wrong version": {SupersedesRateCardRef: oldActive.Card.RateCardRef, ExpectedSupersededVersion: oldActive.Card.Version + 1},
	} {
		t.Run(name, func(t *testing.T) {
			_, activateErr := store.ActivateRateCardWithSupersede(
				ctx, orgID, candidate.Card.RateCardRef, candidate.Card.Version, options, "user:902", base.Add(2*time.Hour),
			)
			require.ErrorIs(t, activateErr, ErrConflict)
		})
	}

	future := createApprovedRequestRate(t, ctx, store, orgID, "future-rate", "provider-a", "future-model", RateSupplierContract, base.Add(24*time.Hour), 30)
	_, err = store.ActivateRateCard(ctx, orgID, future.Card.RateCardRef, future.Card.Version, "user:902", base.Add(3*time.Hour))
	require.ErrorIs(t, err, ErrRequestPricingNotReady)

	standalone := createApprovedRequestRate(t, ctx, store, orgID, "standalone-rate", "provider-a", "standalone-model", RateSupplierContract, base, 40)
	_, err = store.ActivateRateCardWithSupersede(
		ctx, orgID, standalone.Card.RateCardRef, standalone.Card.Version,
		RateCardActivationOptions{SupersedesRateCardRef: oldActive.Card.RateCardRef, ExpectedSupersededVersion: oldActive.Card.Version},
		"user:902", base.Add(2*time.Hour),
	)
	require.ErrorIs(t, err, ErrConflict)

	for _, ref := range []string{"ambiguous-old-a", "ambiguous-old-b"} {
		var cardID int64
		err = pool.QueryRow(ctx, `
			INSERT INTO enterprise_finance_rate_card_versions (
			  org_id,rate_card_ref,version,provider_ref,model_ref,layer,state,effective_from,
			  created_by,approved_by,activated_by,activated_at
			) VALUES ($1,$2,1,$3,'ambiguous-model','supplier_contracted','active',$4,
			  'user:901','user:902','user:902',$4) RETURNING id`, orgID, ref, ref+"-provider", base).Scan(&cardID)
		require.NoError(t, err)
		for _, component := range supersedeRequestComponents(10) {
			hash, hashErr := RateComponentIdentity(component)
			require.NoError(t, hashErr)
			_, err = pool.Exec(ctx, `
				INSERT INTO enterprise_finance_rate_components (
				  rate_card_version_id,org_id,meter,unit_quantity,amount_minor,currency,amount_scale,component_hash
				) VALUES ($1,$2,$3,$4::numeric,$5::numeric,$6,$7,$8)`,
				cardID, orgID, component.Meter, component.UnitQuantity, component.Amount.ValueMinor,
				component.Amount.Currency, component.Amount.Scale, hash,
			)
			require.NoError(t, err)
		}
	}
	ambiguousCandidate := createApprovedRequestRate(
		t, ctx, store, orgID, "ambiguous-new", "provider-c", "ambiguous-model", RateSupplierContract, base, 50,
	)
	_, err = store.ActivateRateCardWithSupersede(
		ctx, orgID, ambiguousCandidate.Card.RateCardRef, ambiguousCandidate.Card.Version,
		RateCardActivationOptions{SupersedesRateCardRef: "ambiguous-old-a", ExpectedSupersededVersion: 1},
		"user:902", base.Add(2*time.Hour),
	)
	require.ErrorIs(t, err, ErrRequestPricingAmbiguous)

	assertLatestRateState(t, ctx, pool, orgID, oldActive.Card.RateCardRef, oldActive.Card.Version, RateActive)
	assertLatestRateState(t, ctx, pool, orgID, candidate.Card.RateCardRef, candidate.Card.Version, RateApproved)
}

func TestProviderListActivationRejectsEvidenceStaleAtActivation(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "rate-stale-source")
	activation := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	fetchedAt := activation.Add(-2 * time.Hour)
	draft, err := store.ImportOfficialRateCardDraft(ctx, OfficialRateDraftImport{
		Card: RateCard{
			RateCardRef: "stale-provider-rate", OrgID: orgID, ProviderRef: "provider-a", ModelRef: "stale-model",
			EffectiveFrom: fetchedAt, Components: supersedeRequestComponents(10),
		},
		Evidence: RateSourceEvidence{
			EvidenceRef: "stale-provider-source", SourceURL: "https://example.invalid/pricing",
			SourceRevision: "revision-stale", FetchedAt: fetchedAt,
			PayloadSHA256:    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			TransformVersion: "official-transform.v1", LastSuccessAt: fetchedAt,
			StaleAfter: activation,
		},
	}, "user:901")
	require.NoError(t, err)
	approved, err := store.ApproveRateCard(ctx, orgID, draft.Card.RateCardRef, draft.Card.Version, "user:902")
	require.NoError(t, err)
	_, err = store.ActivateRateCard(ctx, orgID, approved.Card.RateCardRef, approved.Card.Version, "user:902", activation)
	require.ErrorIs(t, err, ErrRequestPricingNotReady)
	require.ErrorContains(t, err, "stale")
}

func TestRateCardSupersedeRollsBackRetirementWhenCandidateActivationInsertFails(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID := insertFinanceTestOrg(t, ctx, pool, "rate-supersede-rollback")
	base := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	oldApproved := createApprovedRequestRate(t, ctx, store, orgID, "rollback-old", "provider-a", "rollback-model", RateSupplierContract, base, 10)
	oldActive, err := store.ActivateRateCard(ctx, orgID, oldApproved.Card.RateCardRef, oldApproved.Card.Version, "user:902", base.Add(time.Hour))
	require.NoError(t, err)
	candidate := createApprovedRequestRate(t, ctx, store, orgID, "rollback-new", "provider-a", "rollback-model", RateSupplierContract, base, 20)

	_, err = pool.Exec(ctx, `
		CREATE FUNCTION reject_rate_supersede_candidate() RETURNS trigger AS $$
		BEGIN
		  IF NEW.rate_card_ref='rollback-new' AND NEW.state='active' THEN
		    RAISE EXCEPTION 'injected candidate activation failure';
		  END IF;
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_rate_supersede_candidate_trigger
		BEFORE INSERT ON enterprise_finance_rate_card_versions
		FOR EACH ROW EXECUTE FUNCTION reject_rate_supersede_candidate()`)
	require.NoError(t, err)

	_, err = store.ActivateRateCardWithSupersede(
		ctx, orgID, candidate.Card.RateCardRef, candidate.Card.Version,
		RateCardActivationOptions{SupersedesRateCardRef: oldActive.Card.RateCardRef, ExpectedSupersededVersion: oldActive.Card.Version},
		"user:902", base.Add(2*time.Hour),
	)
	require.ErrorContains(t, err, "injected candidate activation failure")
	assertLatestRateState(t, ctx, pool, orgID, oldActive.Card.RateCardRef, oldActive.Card.Version, RateActive)
	assertLatestRateState(t, ctx, pool, orgID, candidate.Card.RateCardRef, candidate.Card.Version, RateApproved)
}

func createApprovedRequestRate(
	t *testing.T,
	ctx context.Context,
	store *PostgresStore,
	orgID int64,
	ref, provider, model, layer string,
	effectiveFrom time.Time,
	input int64,
) RateCardRecord {
	t.Helper()
	draft, err := store.CreateRateCardDraft(ctx, RateCard{
		RateCardRef: ref, OrgID: orgID, ProviderRef: provider, ModelRef: model,
		Layer: layer, EffectiveFrom: effectiveFrom, Components: supersedeRequestComponents(input),
	}, "user:901")
	require.NoError(t, err)
	approved, err := store.ApproveRateCard(ctx, orgID, ref, draft.Card.Version, "user:902")
	require.NoError(t, err)
	return approved
}

func supersedeRequestComponents(input int64) []RateComponent {
	values := []struct {
		meter string
		value int64
	}{
		{MeterInputTokens, input},
		{MeterOutputTokens, input * 2},
		{MeterCacheReadTokens, input / 2},
		{MeterCacheCreationTokens, input},
	}
	components := make([]RateComponent, 0, len(values))
	for _, value := range values {
		components = append(components, RateComponent{
			Meter: value.meter, UnitQuantity: "1000000",
			Amount: Amount{ValueMinor: fmt.Sprintf("%d", value.value), Currency: "CNY", Scale: 1_000_000},
		})
	}
	return components
}

func assertLatestRateState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID int64, ref string, version int64, state string) {
	t.Helper()
	var gotVersion int64
	var gotState string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT version,state FROM enterprise_finance_rate_card_versions
		WHERE org_id=$1 AND rate_card_ref=$2 ORDER BY version DESC LIMIT 1`, orgID, ref,
	).Scan(&gotVersion, &gotState))
	require.Equal(t, version, gotVersion)
	require.Equal(t, state, gotState)
}
