//go:build integration

package billing

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestV1PricingAuthorityCustomerStandardPreservesFrozenQuoteAndHoldsOverrun(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	org := testutil.NewTestOrg(t, pool)
	otherOrg := testutil.NewTestOrg(t, pool)
	testutil.NewTestWalletCents(t, pool, org, 100)
	var project int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug) VALUES ($1,'Quote Project','quote-project') RETURNING id`, org).Scan(&project))
	var key int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_prefix,key_hash,status)
		VALUES ($1,$2,'Quote Key','ur-local-quote','fixture-customer-quote','active') RETURNING id`, org, project).Scan(&key))
	store, err := enterprisefinance.NewPostgresStore(pool)
	require.NoError(t, err)
	billingService := NewBilling(pool, nil).WithRequestPricingAuthority(RequestPricingAuthorityCustomerStandard)
	request := FreezeRequest{
		OrgID: org, ProjectID: project, APIKeyID: key, Model: "public-model", ModelOperation: modeloperation.IDChatCompletions,
		RequestID: "customer-first", EstimatedTokens: 10, EstimatedInputTokens: 7, ReservedCompletionTokens: 3, SafetyFactor: "1",
	}
	missing, err := billingService.Freeze(ctx, request)
	require.NoError(t, err)
	require.False(t, missing.Success)
	require.Equal(t, "billing_contract_unavailable", missing.ErrorCode)

	writePrice := func(ref, model, rate string) {
		t.Helper()
		var components []enterprisefinance.RateComponent
		for _, meter := range []string{enterprisefinance.MeterInputTokens, enterprisefinance.MeterOutputTokens, enterprisefinance.MeterCacheReadTokens, enterprisefinance.MeterCacheCreationTokens} {
			amount := rate
			if ref == "zero-input" && meter == enterprisefinance.MeterInputTokens {
				amount = "0"
			}
			components = append(components, enterprisefinance.RateComponent{Meter: meter, UnitQuantity: "1000000",
				Amount: enterprisefinance.Amount{ValueMinor: amount, Currency: "CNY", Scale: 1_000_000}})
		}
		_, err := store.CreateCustomerPriceTable(ctx, enterprisefinance.CustomerPriceTable{
			OrgID: org, Ref: ref, Version: 1, EffectiveFrom: time.Now().UTC().Add(-time.Hour),
			Entries: []enterprisefinance.CustomerPriceEntry{{PublicModel: model, Operation: request.ModelOperation, Components: components}},
		}, "user:1")
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE customer_price_table_versions SET approved_at=NOW() WHERE org_id=$1 AND table_ref=$2`, org, ref)
		require.Error(t, err, "an approval timestamp alone must fail at the database boundary")
		require.ErrorIs(t, store.ApproveCustomerPriceTable(ctx, org, ref, 1, "user:1"), enterprisefinance.ErrConflict)
		require.NoError(t, store.ApproveCustomerPriceTable(ctx, org, ref, 1, "user:2"))
	}
	bind := func(projectID, revision int64, ref string) {
		t.Helper()
		binding, err := store.BindCustomerPriceTable(ctx, enterprisefinance.CustomerPriceBinding{
			OrgID: org, ProjectID: projectID, TableRef: ref, TableVersion: 1,
		}, revision, "user:2")
		require.NoError(t, err)
		require.Equal(t, revision+1, binding.Revision)
	}
	writePrice("org-standard", request.Model, "1000000")
	bind(0, 0, "org-standard")
	_, err = store.BindCustomerPriceTable(ctx, enterprisefinance.CustomerPriceBinding{
		OrgID: otherOrg, TableRef: "org-standard", TableVersion: 1,
	}, 0, "user:2")
	require.ErrorIs(t, err, enterprisefinance.ErrConflict)

	original, err := billingService.Freeze(ctx, request)
	require.NoError(t, err)
	require.True(t, original.Success)
	require.EqualValues(t, 10, original.AmountMicros)
	require.Regexp(t, "^ccq1_[a-f0-9]{64}$", original.PricingVersion)

	writePrice("project-standard", request.Model, "2000000")
	bind(project, 0, "project-standard")
	replay, err := NewBilling(pool, nil).Freeze(ctx, request)
	require.NoError(t, err)
	require.Equal(t, original.PricingVersion, replay.PricingVersion)
	require.Equal(t, original.AmountMicros, replay.AmountMicros)
	changed := request
	changed.ModelOperation = modeloperation.IDEmbeddings
	conflict, err := billingService.Freeze(ctx, changed)
	require.NoError(t, err)
	require.Equal(t, "request_identity_conflict", conflict.ErrorCode)

	second := request
	second.RequestID = "customer-project"
	frozen, err := billingService.Freeze(ctx, second)
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.EqualValues(t, 20, frozen.AmountMicros)
	writePrice("project-incomplete", "different-model", "4000000")
	bind(project, 1, "project-incomplete")
	third := request
	third.RequestID = "customer-missing-model"
	notQuoted, err := billingService.Freeze(ctx, third)
	require.NoError(t, err)
	require.Equal(t, "billing_contract_unavailable", notQuoted.ErrorCode)

	extension, err := billingService.ExtendReservation(ctx, ExtendReservationRequest{
		OrgID: org, RequestID: request.RequestID, Sequence: 1, AdditionalCompletionTokens: 1, TargetCompletionTokens: 4,
	})
	require.NoError(t, err)
	require.False(t, extension.Success)

	settlement := SettleRequest{
		OrgID: org, RequestID: request.RequestID, PromptTokens: 100, UncachedPromptTokens: 100,
		UsageSource: metering.UsageSourceUpstream, UsageAuthority: metering.UsageAuthorityProviderReported,
		UsagePurpose: metering.UsagePurposeCustomerCharge, UsageFinality: metering.UsageFinalityFinal,
		UsageContractVersion: metering.UsageContractVersion,
	}
	result, err := NewBilling(pool, nil).Settle(ctx, settlement)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, customerAuthorizationExceeded, result.ErrorCode)
	require.Zero(t, result.ChargedCostMicros)
	require.Zero(t, result.TopUpAmountMicros)
	require.EqualValues(t, 90, result.UncoveredCostMicros)

	var walletHold, totalSpent, debits int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT frozen_micros,total_spent_micros FROM wallets WHERE org_id=$1`, org).Scan(&walletHold, &totalSpent))
	require.EqualValues(t, 30, walletHold)
	require.Zero(t, totalSpent)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE org_id=$1 AND type='settle'`, org).Scan(&debits))
	require.Zero(t, debits)
	wire, err := store.GetCustomerChargeQuote(ctx, org, request.RequestID)
	require.NoError(t, err)
	var receipt struct {
		Quote      customerQuoteReceipt `json:"quote"`
		Authorized string               `json:"wallet_authorized_cost_micros"`
		Charged    string               `json:"charged_customer_cost_micros"`
		Reason     string               `json:"settlement_pending_reason"`
	}
	require.NoError(t, json.Unmarshal(wire, &receipt))
	require.Equal(t, "org-standard", receipt.Quote.Selection.Binding.TableRef)
	require.Equal(t, "10", receipt.Authorized)
	require.Equal(t, "0", receipt.Charged)
	require.Equal(t, customerAuthorizationExceeded, receipt.Reason)
	candidates, err := billingService.claimPendingSettlements(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, candidates)

	writePrice("zero-input", request.Model, "2000000")
	bind(project, 2, "zero-input")
	request.RequestID = "customer-zero-input"
	resultingQuote, err := billingService.Freeze(ctx, request)
	require.NoError(t, err)
	require.True(t, resultingQuote.Success)
	require.EqualValues(t, 20, resultingQuote.AmountMicros)
	var inputPrice, cachePrice int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT effective_input_price_micros_per_million,effective_cache_read_price_micros_per_million
		FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, org, request.RequestID).Scan(&inputPrice, &cachePrice))
	require.Zero(t, inputPrice)
	require.EqualValues(t, 2_000_000, cachePrice)
	cacheUsage := settlement
	cacheUsage.RequestID = request.RequestID
	cacheUsage.PromptTokens, cacheUsage.UncachedPromptTokens, cacheUsage.CacheReadTokens = 3, 0, 3
	cacheUsage.CacheUsageSchema, cacheUsage.CacheUsageStatus = "openai.prompt_tokens_details.v2", "cache_read_reported"
	settled, err := billingService.Settle(ctx, cacheUsage)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.EqualValues(t, 6, settled.ChargedCostMicros)

	secondUsage := settlement
	secondUsage.RequestID = second.RequestID
	secondOverrun, err := billingService.Settle(ctx, secondUsage)
	require.NoError(t, err)
	require.Equal(t, customerAuthorizationExceeded, secondOverrun.ErrorCode)
	// Force a tied time to exercise the Request ID tiebreaker across pages.
	_, err = pool.Exec(ctx, `UPDATE freeze_idempotency SET provider_cost_pending_at=$3
		WHERE org_id=$1 AND request_id IN ($2, $4)`, org, settlement.RequestID, time.Now().UTC(), second.RequestID)
	require.NoError(t, err)
	firstPage, err := store.ListCustomerChargeQuoteOverruns(ctx, org, enterprisefinance.CustomerChargeQuoteOverrunCursor{}, 1)
	require.NoError(t, err)
	require.Len(t, firstPage.Data, 1)
	require.NotNil(t, firstPage.Next)
	require.Equal(t, settlement.RequestID, firstPage.Data[0].RequestID)
	require.Equal(t, project, firstPage.Data[0].ProjectID, "use the request project for an organization-scoped price")
	require.Equal(t, "10", firstPage.Data[0].AuthorizedMicros)
	require.Equal(t, "100", firstPage.Data[0].CalculatedMicros)
	require.Equal(t, "0", firstPage.Data[0].ChargedMicros)
	require.Equal(t, "90", firstPage.Data[0].UncoveredMicros)
	require.EqualValues(t, 100, firstPage.Data[0].UncachedInput)
	secondPage, err := store.ListCustomerChargeQuoteOverruns(ctx, org, *firstPage.Next, 1)
	require.NoError(t, err)
	require.Len(t, secondPage.Data, 1)
	require.Equal(t, second.RequestID, secondPage.Data[0].RequestID)
	require.Nil(t, secondPage.Next, "settled quotes are excluded")
	otherPage, err := store.ListCustomerChargeQuoteOverruns(ctx, otherOrg, enterprisefinance.CustomerChargeQuoteOverrunCursor{}, 1)
	require.NoError(t, err)
	require.Empty(t, otherPage.Data)

	for _, stream := range []bool{false, true} {
		hold := request
		hold.RequestID = "op_standard_nonstream_output_hold"
		if stream {
			hold.RequestID = "op_standard_stream_output_hold"
		}
		quote, err := billingService.Freeze(ctx, hold)
		require.NoError(t, err)
		require.True(t, quote.Success)
		attempt := "att_" + hold.RequestID
		require.NoError(t, billingService.BeginProviderAttempt(ctx, ProviderAttemptStart{
			RequestID: hold.RequestID, OrgID: org, AttemptID: attempt,
			APIKeyID: key, ChannelID: 88, ChannelKeyID: 99, RouteSequence: 1, RouteTotal: 1,
			Endpoint: "/v1/chat/completions", Provider: "fixture", Channel: "fixture",
		}))
		require.NoError(t, billingService.CompleteProviderAttempt(ctx, ProviderAttemptCompletion{
			RequestID: hold.RequestID, OrgID: org, AttemptID: attempt,
			Outcome: ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200,
		}))
		confirmed := p6f41ProviderUsage(hold.RequestID, attempt, org)
		confirmed.CompletionTokens = 50
		confirmed.UsagePurpose = metering.UsagePurposeProviderEvidence
		if stream {
			lease := StreamDeliveryStart{OrgID: org, RequestID: hold.RequestID, AttemptID: attempt,
				OwnerID: "standard-hold-fixture", Generation: 1, LeaseTTL: time.Minute}
			require.NoError(t, billingService.BeginStreamDelivery(ctx, lease))
			confirmed.StreamFence = testStreamFence(lease)
			require.NoError(t, billingService.HoldStreamOutput(ctx, confirmed, 24, 0))
			require.NoError(t, billingService.HoldStreamOutput(ctx, confirmed, 24, 0))
			require.NoError(t, billingService.readStreamOutputHold(ctx, confirmed, 24, 0))
		} else {
			require.NoError(t, billingService.HoldNonStreamOutput(ctx, confirmed, OutputHoldReasonGuardrailWithheld))
			require.NoError(t, billingService.HoldNonStreamOutput(ctx, confirmed, OutputHoldReasonGuardrailWithheld))
		}
		var pendingReason string
		var overdraft, charged int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT settlement_pending_reason,uncovered_cost_micros,charged_cost_micros
			FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, org, hold.RequestID).Scan(&pendingReason, &overdraft, &charged))
		require.Equal(t, customerAuthorizationExceeded, pendingReason)
		require.Positive(t, overdraft)
		require.Zero(t, charged)
		for _, action := range []string{ProviderReconciliationCollect, ProviderReconciliationRefund, ProviderReconciliationSubsidy} {
			_, err := billingService.ReconcileProviderAttempt(ctx, ProviderReconciliationRequest{
				OrgID: org, RequestID: hold.RequestID, AttemptID: attempt, Action: action,
				ActorUserID: 77, Reason: "retain customer authorization overrun", EvidenceRef: "test://standard-overrun",
			})
			require.ErrorIs(t, err, ErrProviderReconciliationUnsafe)
		}
	}
}
