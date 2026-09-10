//go:build integration

package sourcefacts

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/outbox"
)

func TestSourceBudgetOriginalFinancialQueueGapsAndMaterialization(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	ctx := t.Context()
	events, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	financial := billing.NewBilling(pool, nil)
	financial.SetOutbox(events)
	s.options.BudgetSnapshotsEnabled = true
	s.options.FinancialJournalReady = financial.FinancialJournalReady
	_, err = pool.Exec(ctx, `INSERT INTO wallets(org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros) VALUES($1,2000000000,0,0,0)`, org)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO model_prices(model_name,provider,input_price_cents,output_price_cents) VALUES('synthetic-D-charge-600','test',60000,0)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE projects SET budget_cents=100000,budget_period='monthly' WHERE id=$1`, project)
	require.NoError(t, err)
	frozen, err := financial.Freeze(ctx, billing.FreezeRequest{RequestID: "D-original-600", OrgID: org, ProjectID: project, Model: "synthetic-D-charge-600", EstimatedTokens: 1000, SafetyFactor: "1"})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.NoError(t, financial.MarkBillableOutput(ctx, "D-original-600", org))
	usage := billing.SettleRequest{RequestID: "D-original-600", OrgID: org, PromptTokens: 1000, UncachedPromptTokens: 1000, UsageSource: "upstream", CacheUsageStatus: "not_reported"}
	require.NoError(t, financial.PersistSettlementUsage(ctx, usage))
	settled, err := financial.Settle(ctx, usage)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.EqualValues(t, 600000000, settled.ChargedCostMicros)
	a := budgetAccess{access: access{org: org, project: project, instance: s.SourceInstance()}}
	summaryTx := func(tx pgx.Tx, cutoff time.Time) BudgetPeriodSummary {
		record, e := readBudgetRecordTx(ctx, tx, a, gatewayBudgetPrefix+strconv.FormatInt(project, 10))
		require.NoError(t, e)
		result, e := s.gatewayBudgetSummaryTx(ctx, tx, a, record.definition, cutoff)
		require.NoError(t, e)
		return result
	}
	read := func() BudgetPeriodSummary {
		tx, e := pool.Begin(ctx)
		require.NoError(t, e)
		defer tx.Rollback(context.Background())
		_, e = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
		require.NoError(t, e)
		var cutoff time.Time
		require.NoError(t, tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&cutoff))
		return summaryTx(tx, cutoff)
	}
	// Delayed, claimed, retried and dead-lettered financial work remains a gap.
	_, err = pool.Exec(ctx, `UPDATE internal_outbox_events SET available_at=clock_timestamp()+interval '1 day',claimed_by='synthetic-worker',claimed_until=clock_timestamp()+interval '1 hour',attempts=7,dead_lettered_at=clock_timestamp() WHERE org_id=$1 AND event_type=ANY($2)`, org, budgetFinancialEventTypes)
	require.NoError(t, err)
	before := read()
	require.Equal(t, "0", before.KnownSubtotal.ValueMinor)
	require.Equal(t, "2", before.Coverage.MaterializationPending)
	require.Contains(t, before.Coverage.Reasons, "financial_materialization_gap")
	require.Nil(t, before.Comparison.Remaining)
	rows, err := pool.Query(ctx, `SELECT id,event_key,event_type,org_id,request_id,payload,occurred_at FROM internal_outbox_events WHERE org_id=$1 AND event_type=ANY($2) ORDER BY id`, org, budgetFinancialEventTypes)
	require.NoError(t, err)
	queued := []outbox.Event{}
	for rows.Next() {
		var e outbox.Event
		require.NoError(t, rows.Scan(&e.ID, &e.Key, &e.Type, &e.OrgID, &e.RequestID, &e.Payload, &e.OccurredAt))
		queued = append(queued, e)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Len(t, queued, 2)
	require.NoError(t, financial.ProcessUsageJournalEvent(ctx, queued[0]))
	partial := read()
	require.Equal(t, "1", partial.Coverage.MaterializationPending)
	require.Equal(t, "1", partial.Coverage.MaterializedAwaitingAck)
	held, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer held.Rollback(context.Background())
	_, err = held.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
	require.NoError(t, err)
	var heldCutoff time.Time
	require.NoError(t, held.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&heldCutoff))
	require.Equal(t, "0", summaryTx(held, heldCutoff).KnownSubtotal.ValueMinor)
	require.NoError(t, financial.ProcessUsageJournalEvent(ctx, queued[1]))
	require.NoError(t, financial.ProcessUsageJournalEvent(ctx, queued[1]))
	require.Equal(t, "0", summaryTx(held, heldCutoff).KnownSubtotal.ValueMinor, "later materialization is not visible in an earlier RR reading")
	require.NoError(t, held.Rollback(ctx))
	materialized := read()
	require.Equal(t, "600000000", materialized.KnownSubtotal.ValueMinor)
	require.Equal(t, "0", materialized.Coverage.MaterializationPending)
	require.Equal(t, "2", materialized.Coverage.MaterializedAwaitingAck)
	require.NotContains(t, materialized.Coverage.Reasons, "financial_materialization_gap")
	require.Contains(t, materialized.Coverage.Reasons, "period_before_certification")
	require.Nil(t, materialized.Comparison.Remaining)
	_, err = pool.Exec(ctx, `INSERT INTO call_logs(request_id,org_id,project_id,cost_micros) VALUES('D-detail-only-200',$1,$2,200000000)`, org, project)
	require.NoError(t, err)
	require.Equal(t, "600000000", read().KnownSubtotal.ValueMinor)
	// Synthetic future event times test half-open UTC windows and a complete
	// post-certification period without rewriting the production certificate.
	// This is a controlled timestamp fixture, not a claim of future live usage.
	now := time.Now().UTC()
	futureStart := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	futureCutoff := futureStart.Add(time.Hour)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	for _, event := range queued {
		var payload map[string]any
		require.NoError(t, json.Unmarshal(event.Payload, &payload))
		payload["occurred_at"] = futureStart.Format(time.RFC3339Nano)
		wire, err := json.Marshal(payload)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `UPDATE internal_outbox_events SET occurred_at=$2,payload=$3 WHERE id=$1`, event.ID, futureStart, wire)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `UPDATE usage_finalization_journal SET occurred_at=$2 WHERE event_key=$1`, event.Key, futureStart)
		require.NoError(t, err)
	}
	complete := summaryTx(tx, futureCutoff)
	require.Equal(t, "complete_settled_as_of", complete.Coverage.State)
	require.Equal(t, "600000000", complete.KnownSubtotal.ValueMinor)
	require.Equal(t, "400000000", complete.Comparison.Remaining.ValueMinor)
	boundary := summaryTx(tx, futureStart)
	require.Equal(t, "0", boundary.KnownSubtotal.ValueMinor, "equal-to-cutoff is excluded")
	_, err = tx.Exec(ctx, `UPDATE internal_outbox_events SET payload=jsonb_set(payload,'{project_id}','999999999'::jsonb) WHERE id=$1`, queued[1].ID)
	require.NoError(t, err)
	unknown := summaryTx(tx, futureCutoff)
	require.Equal(t, "unknown", unknown.Coverage.State)
	require.Contains(t, unknown.Coverage.Reasons, "unattributable_financial_outbox")
	require.Nil(t, unknown.Comparison.Remaining)
	require.Equal(t, "600000000", complete.KnownSubtotal.ValueMinor)
	require.Equal(t, "400000000", complete.Comparison.Remaining.ValueMinor, "old readings are retained values")
	require.NoError(t, tx.Rollback(ctx))
	require.Equal(t, "600000000", read().KnownSubtotal.ValueMinor)
	// The original observation-only billing mode can calculate 400 CNY while
	// charging zero. Only its original charged terminal enters D's subtotal.
	financial.WithMode(billing.BillingModeCostObservation)
	_, err = pool.Exec(ctx, `INSERT INTO model_prices(model_name,provider,input_price_cents,output_price_cents) VALUES('synthetic-D-calculated-400','test',40000,0)`)
	require.NoError(t, err)
	zeroFrozen, err := financial.Freeze(ctx, billing.FreezeRequest{RequestID: "D-zero-charged-400-calculated", OrgID: org, ProjectID: project, Model: "synthetic-D-calculated-400", EstimatedTokens: 1000, SafetyFactor: "1"})
	require.NoError(t, err)
	require.True(t, zeroFrozen.Success)
	require.NoError(t, financial.MarkBillableOutput(ctx, "D-zero-charged-400-calculated", org))
	zeroUsage := billing.SettleRequest{RequestID: "D-zero-charged-400-calculated", OrgID: org, PromptTokens: 1000, UncachedPromptTokens: 1000, UsageSource: "upstream", CacheUsageStatus: "not_reported"}
	require.NoError(t, financial.PersistSettlementUsage(ctx, zeroUsage))
	zeroSettled, err := financial.Settle(ctx, zeroUsage)
	require.NoError(t, err)
	require.True(t, zeroSettled.Success)
	require.Zero(t, zeroSettled.ChargedCostMicros)
	require.EqualValues(t, 400000000, zeroSettled.ActualCostMicros)
	zeroRows, err := pool.Query(ctx, `SELECT event_key,event_type,org_id,request_id,payload,occurred_at FROM internal_outbox_events WHERE org_id=$1 AND request_id=$2 AND event_type=ANY($3) ORDER BY id`, org, zeroUsage.RequestID, budgetFinancialEventTypes)
	require.NoError(t, err)
	zeroEvents := []outbox.Event{}
	for zeroRows.Next() {
		var event outbox.Event
		require.NoError(t, zeroRows.Scan(&event.Key, &event.Type, &event.OrgID, &event.RequestID, &event.Payload, &event.OccurredAt))
		zeroEvents = append(zeroEvents, event)
	}
	require.NoError(t, zeroRows.Err())
	zeroRows.Close()
	require.Len(t, zeroEvents, 2)
	for _, event := range zeroEvents {
		require.NoError(t, financial.ProcessUsageJournalEvent(ctx, event))
	}
	require.Equal(t, "600000000", read().KnownSubtotal.ValueMinor, "calculated 400 must not become charged spend")

}
