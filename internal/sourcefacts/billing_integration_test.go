//go:build integration

package sourcefacts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/executioncontext"
)

func TestFreezePersistsPricedZeroSeparatelyFromUnpricedObservation(t *testing.T) {
	pool, source, org, project := setupSourceDB(t)
	b := billing.NewBilling(pool, nil).WithMode(billing.BillingModeCostObservation)
	b.SetReservationMaterializer(source)
	_, err := pool.Exec(t.Context(), `INSERT INTO model_prices(model_name,input_price_cents,output_price_cents) VALUES('source-priced-zero',0,0)`)
	require.NoError(t, err)
	for _, tc := range []struct{ model, state string }{{"source-priced-zero", "priced"}, {"source-missing-price", "unpriced"}} {
		result, err := b.Freeze(t.Context(), billing.FreezeRequest{RequestID: tc.model, OrgID: org, ProjectID: project, Model: tc.model, EstimatedTokens: 16, SafetyFactor: "1"})
		require.NoError(t, err)
		require.True(t, result.Success)
		var raw []byte
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT source_cost_basis FROM freeze_idempotency WHERE request_id=$1`, tc.model).Scan(&raw))
		var basis billing.SourceBasis
		require.NoError(t, json.Unmarshal(raw, &basis))
		require.Equal(t, tc.state, basis.PricingState)
		require.Equal(t, "cost_observation", basis.BillingMode)
		require.Equal(t, "reservation", basis.ModeSource)
		require.Equal(t, "0", basis.InputRate)
		require.Equal(t, "0", basis.OutputRate)
		if tc.state == "unpriced" {
			require.True(t, strings.HasPrefix(basis.PricingVersion, "uo1_"))
		} else {
			require.False(t, strings.HasPrefix(basis.PricingVersion, "uo1_"))
		}
	}
}

func sourceTestTerminal(request string, org, project, amount int64, at time.Time) billing.TerminalRecord {
	return billing.TerminalRecord{EventKey: "billing:settled:" + request, RequestID: request, OrgID: org, ProjectID: project, Status: "settled", Model: "synthetic-model", PromptTokens: 10, UncachedPromptTokens: 10, CompletionTokens: 2, UsageSource: "provider", UsageAuthority: "authoritative", UsagePurpose: "billing", UsageFinality: "final", UsageContractVersion: "usage.v1", CacheUsageSchema: "cache.v1", CacheUsageStatus: "not_used", CalculatedCostMicros: amount, ChargedCostMicros: 0, PricingVersion: "test-pricing.v1", OccurredAt: at, BillingMode: "cost_observation", BillingModeSource: "journal_payload"}
}

func TestTerminalIsSelfContainedAcrossRetentionAndMatchesI01(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	ctx := context.Background()
	s.options.I01Enabled = true
	v1, e := completedfeed.NewService(pool, strings.Repeat("x", 32), s.SourceInstance(), true)
	require.NoError(t, e)
	basis := billing.SourceBasis{BillingMode: "cost_observation", ModeSource: "reservation", PricingAuthority: "rate_card", PricingVersion: "test-pricing.v1", PricingState: "priced", RateCardRef: "synthetic-card", RateCardVersion: "1", RateCardLayer: "customer", InputRate: "1000000", OutputRate: "2000000", CacheReadRate: "0", CacheCreationRate: "0", Multiplier: "1", Currency: "CNY", AmountScale: "1000000", UnitRates: []billing.SourceUnitRate{}}
	for i, amount := range []int64{30, 20, 40, 10, 5} {
		request := fmt.Sprintf("i2-r%d", i+1)
		snap := testSnapshot()
		snap.Core.RequestID = request
		snap.Core.OrgID = executioncontext.ID(org)
		snap.Core.ProjectID = executioncontext.ID(project)
		snap.Core.WorkloadApplicationID = "77"
		require.NoError(t, s.CaptureAuthenticated(ctx, snap))
		_, e = pool.Exec(ctx, `INSERT INTO freeze_idempotency(request_id,org_id,project_id,status,billing_mode,model_name,pricing_version) VALUES($1,$2,$3,'frozen','cost_observation','synthetic-model','test-pricing.v1')`, request, org, project)
		require.NoError(t, e)
		tx, e := pool.Begin(ctx)
		require.NoError(t, e)
		require.NoError(t, s.MaterializeReservationTx(executioncontext.WithSnapshot(ctx, snap), tx, billing.FreezeRequest{OrgID: org, ProjectID: project, APIKeyID: 9007199254740993, RequestID: request}, basis))
		require.NoError(t, tx.Commit(ctx))
		// Expire only the early source pages. The unresolved reservation retains
		// its contemporaneous context and cost basis for terminal publication.
		_, e = s.DeleteExpired(ctx, time.Now().Add(time.Hour))
		require.NoError(t, e)
		var seed []byte
		require.NoError(t, pool.QueryRow(ctx, `SELECT source_context FROM freeze_idempotency WHERE request_id=$1`, request).Scan(&seed))
		require.NotEmpty(t, seed)
		record := sourceTestTerminal(request, org, project, amount*1000000, time.Now().UTC().Truncate(time.Microsecond))
		tx, e = pool.Begin(ctx)
		require.NoError(t, e)
		require.NoError(t, (billing.TerminalMaterializers{v1, s}).MaterializeTerminalTx(ctx, tx, record))
		require.NoError(t, tx.Commit(ctx))
		rows, e := pool.Query(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 ORDER BY profile`, request)
		require.NoError(t, e)
		docs := map[string]Document{}
		for rows.Next() {
			var wire []byte
			require.NoError(t, rows.Scan(&wire))
			d, e := Decode(wire)
			require.NoError(t, e)
			docs[d.Profile] = d
			_, e = Event(d, wire)
			require.NoError(t, e)
		}
		require.NoError(t, rows.Err())
		rows.Close()
		require.Len(t, docs, 3)
		require.Equal(t, "77", docs[ContextProfile].Core.WorkloadApplicationID)
		require.Equal(t, snap.Core.CapturedAt, docs[ContextProfile].Core.CapturedAt)
		require.Equal(t, "9007199254740994", docs[IdentityProfile].Identity.OwnerUserID)
		require.Equal(t, "unknown", docs[IdentityProfile].Identity.ActualHuman)
		var hash string
		require.NoError(t, pool.QueryRow(ctx, `SELECT snapshot_sha256 FROM completed_execution_feed_items WHERE request_id=$1`, request).Scan(&hash))
		require.Equal(t, hash, docs[CostProfile].Cost.Reference.SnapshotSHA256)
		require.Equal(t, "published_by_i01", docs[CostProfile].Cost.Reference.PublicationStatus)
		require.Equal(t, basis, *docs[CostProfile].Cost.Basis)
		wire, e := json.Marshal(docs[CostProfile])
		require.NoError(t, e)
		var value map[string]any
		require.NoError(t, json.Unmarshal(wire, &value))
		cost := value["cost"].(map[string]any)
		require.NotContains(t, cost, "calculated_cost_micros")
		require.NotContains(t, cost, "charged_cost_micros")
		if i == 3 || i == 4 {
			var observed, charged int64
			require.NoError(t, pool.QueryRow(ctx, `SELECT SUM((convert_from(snapshot,'UTF8')::jsonb->'money'->>'calculated_cost_micros')::bigint),SUM((convert_from(snapshot,'UTF8')::jsonb->'money'->>'charged_cost_micros')::bigint) FROM completed_execution_feed_items`).Scan(&observed, &charged))
			expected := int64(100000000)
			if i == 4 {
				expected = 105000000
			}
			require.Equal(t, expected, observed)
			require.Zero(t, charged)
		}
	}
}

func TestLateCaptureUsesPersistedRatesAndExplicitI01Absence(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	ctx := context.Background()
	request := "legacy-late-terminal"
	_, e := pool.Exec(ctx, `INSERT INTO freeze_idempotency(request_id,org_id,project_id,status,billing_mode,model_name,pricing_version,effective_input_price_micros_per_million) VALUES($1,$2,$3,'frozen','cost_observation','synthetic-model','legacy-price',12345)`, request, org, project)
	require.NoError(t, e)
	record := sourceTestTerminal(request, org, project, 0, time.Now().UTC().Truncate(time.Microsecond))
	record.BillingModeSource = "legacy_interpretation.v1"
	tx, e := pool.Begin(ctx)
	require.NoError(t, e)
	require.NoError(t, s.MaterializeTerminalTx(ctx, tx, record))
	require.NoError(t, tx.Commit(ctx))
	var wire []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2`, request, CostProfile).Scan(&wire))
	d, e := Decode(wire)
	require.NoError(t, e)
	require.Equal(t, "persisted_legacy", d.Cost.BasisStatus)
	require.Equal(t, "12345", d.Cost.Basis.InputRate)
	require.Equal(t, "not_captured", d.Cost.Basis.PricingAuthority)
	require.Equal(t, "unknown", d.Cost.Basis.PricingState)
	require.Equal(t, "not_published_by_i01", d.Cost.Reference.PublicationStatus)
	require.Empty(t, d.Cost.Basis.RateCardRef)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND profile<>$2 AND convert_from(document,'UTF8')::jsonb->>'capture_status'='not_captured'`, request, CostProfile).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM completed_execution_feed_items`).Scan(&count))
	require.Zero(t, count)
}
