package billing

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
)

func TestP6F20FinancialJournalIncrementalBudgetAndWebhookReplay(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	ctx := t.Context()
	var projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (
			org_id, name, slug, description, status, budget_cents, budget_period
		 ) VALUES ($1, 'P6F20', $2, 'durable outbox fixture', 'active', 1, 'daily')
		 RETURNING id`, orgID, "p6f20-durable-outbox",
	).Scan(&projectID))
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`INSERT INTO call_logs (request_id, org_id, project_id, cost_micros, created_at)
			 VALUES ('legacy-before-p6f20', $1, $2, 50000, NOW())`, orgID, projectID)
		return err
	}())
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`INSERT INTO project_budget_usage (
				org_id, project_id, budget_period, period_start, spent_micros
			 ) VALUES ($1, $2, 'daily', $3, 50000)`,
			orgID, projectID, budgetPeriodStart(time.Now().UTC(), "daily"))
		return err
	}(), "simulate the one-time migration baseline for legacy request logs")

	var webhookCalls atomic.Int64
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "unirouter-notification-delivery-1", r.Header.Get("Idempotency-Key"))
		webhookCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(webhook.Close)
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`INSERT INTO notification_webhooks (
				org_id, project_id, name, url, event_types, enabled,
				scope_verified, scope_write_generation
			 ) VALUES ($1, $2, 'p6f20', $3, '[]'::jsonb, TRUE, TRUE, 1)`,
			orgID, projectID, webhook.URL)
		return err
	}())

	options := outbox.DefaultOptions()
	options.WorkerCount = 1
	options.BatchSize = 20
	outboxService, err := outbox.New(pool, options)
	require.NoError(t, err)
	billingService := NewBilling(pool, nil)
	billingService.SetOutbox(outboxService)
	eventService := NewEventService(pool)
	eventService.SetOutbox(outboxService)
	for _, eventType := range []string{
		outbox.EventBillingUsagePrepared,
		outbox.EventBillingSettled,
		outbox.EventBillingCancelled,
	} {
		require.NoError(t, outboxService.RegisterHandler(eventType, billingService.ProcessUsageJournalEvent))
	}
	require.NoError(t, outboxService.RegisterHandler(outbox.EventBudgetEvaluate, eventService.ProcessBudgetEvaluation))
	require.NoError(t, outboxService.RegisterHandler(outbox.EventNotificationDelivery, eventService.ProcessNotificationDelivery))

	requestID := "op_p6f20_durable_settlement"
	freeze, err := billingService.Freeze(ctx, FreezeRequest{
		RequestID:       requestID,
		OrgID:           orgID,
		ProjectID:       projectID,
		Model:           model,
		EstimatedTokens: 100,
		SafetyFactor:    "1",
		MinFreezeCents:  10,
	})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	require.NoError(t, billingService.MarkBillableOutput(ctx, requestID, orgID))
	usage := SettleRequest{
		RequestID:            requestID,
		OrgID:                orgID,
		PromptTokens:         1,
		UncachedPromptTokens: 1,
		CompletionTokens:     1,
		UsageSource:          "upstream",
		CacheUsageStatus:     "not_reported",
	}
	require.NoError(t, billingService.PersistSettlementUsage(ctx, usage))
	settled, err := billingService.Settle(ctx, usage)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Equal(t, int64(120), settled.ChargedCostMicros)

	var queued, journalRows, callLogRows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM internal_outbox_events`).Scan(&queued))
	require.Equal(t, 2, queued, "usage preparation and settlement must commit their own durable envelopes")
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM usage_finalization_journal`).Scan(&journalRows))
	require.Zero(t, journalRows, "materialization must be asynchronous")
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs`).Scan(&callLogRows))
	require.Equal(t, 1, callLogRows, "the current request must not require a synchronous call-log insert")

	processed, err := outboxService.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, processed)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM usage_finalization_journal`).Scan(&journalRows))
	require.Equal(t, 2, journalRows)
	var spent int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT spent_micros FROM project_budget_usage
		 WHERE org_id = $1 AND project_id = $2`, orgID, projectID,
	).Scan(&spent))
	require.EqualValues(t, 50_120, spent, "the incremental ledger must seed existing period spend before adding the exact new settlement")

	processed, err = outboxService.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed, "settlement schedules one incremental budget evaluation")
	require.Zero(t, webhookCalls.Load(), "budget evaluation must only enqueue delivery")
	processed, err = outboxService.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.EqualValues(t, 1, webhookCalls.Load())

	var eventCount, deliveryCount int
	var deliveryState, deliveryStatus string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MAX(delivery_state), '')
		 FROM notification_events
		 WHERE org_id = $1 AND event_type = $2`, orgID, EventProjectBudgetThreshold,
	).Scan(&eventCount, &deliveryState))
	require.Equal(t, 1, eventCount)
	require.Equal(t, DeliveryStateDelivered, deliveryState)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MAX(status), '') FROM notification_deliveries`,
	).Scan(&deliveryCount, &deliveryStatus))
	require.Equal(t, 1, deliveryCount)
	require.Equal(t, "delivered", deliveryStatus)

	// Replaying the terminal API and the threshold evaluator cannot duplicate
	// spend, the journal, the event, or external delivery.
	replayed, err := billingService.Settle(ctx, usage)
	require.NoError(t, err)
	require.Equal(t, settled.SettleID, replayed.SettleID)
	processed, err = outboxService.DrainOnce(ctx)
	require.NoError(t, err)
	require.Zero(t, processed)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT spent_micros FROM project_budget_usage
		 WHERE org_id = $1 AND project_id = $2`, orgID, projectID,
	).Scan(&spent))
	require.EqualValues(t, 50_120, spent)
	require.EqualValues(t, 1, webhookCalls.Load())
}

func TestP6F20IncrementalBudgetDoesNotDoubleCountAheadOfJournalCallLogs(t *testing.T) {
	pool, orgID, _ := newReservationFixture(t, 1000, 0)
	ctx := t.Context()
	var projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (
			org_id, name, slug, description, status, budget_cents, budget_period
		 ) VALUES ($1, 'P6F20 ordering', $2, '', 'active', 100, 'daily')
		 RETURNING id`, orgID, "p6f20-budget-ordering",
	).Scan(&projectID))
	_, err := pool.Exec(ctx,
		`INSERT INTO call_logs (request_id, org_id, project_id, cost_micros, created_at)
		 VALUES
		 ('op_p6f20_order_a', $1, $2, 10000, NOW()),
		 ('op_p6f20_order_b', $1, $2, 10000, NOW())`, orgID, projectID)
	require.NoError(t, err)

	options := outbox.DefaultOptions()
	options.WorkerCount = 1
	service, err := outbox.New(pool, options)
	require.NoError(t, err)
	financial := NewBilling(pool, nil)
	financial.SetOutbox(service)
	for _, requestID := range []string{"op_p6f20_order_a", "op_p6f20_order_b"} {
		event, eventErr := outbox.NewEvent(
			outbox.EventBillingSettled,
			"billing:settled:"+requestID,
			outbox.PriorityBilling,
			orgID,
			requestID,
			usageJournalPayload{
				EventType:            "settled",
				RequestID:            requestID,
				OrgID:                orgID,
				ProjectID:            projectID,
				PromptTokens:         1,
				UncachedPromptTokens: 1,
				CompletionTokens:     1,
				CacheUsageStatus:     "not_reported",
				MoneySchema:          outbox.MoneyVersion,
				Currency:             "CNY",
				AmountScale:          money.MicrosPerCNY,
				CalculatedCostMicros: "10000",
				ChargedCostMicros:    "10000",
				OccurredAt:           time.Now().UTC(),
			},
			time.Now().UTC(),
		)
		require.NoError(t, eventErr)
		require.NoError(t, financial.ProcessUsageJournalEvent(ctx, event))
	}

	var spent int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT spent_micros FROM project_budget_usage
		 WHERE org_id = $1 AND project_id = $2`, orgID, projectID,
	).Scan(&spent))
	require.EqualValues(t, 20_000, spent, "call-log materialization order must not influence financial increments")
}

func TestP6F20CancellationProducesOneTerminalJournal(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	ctx := t.Context()
	options := outbox.DefaultOptions()
	options.WorkerCount = 1
	outboxService, err := outbox.New(pool, options)
	require.NoError(t, err)
	billingService := NewBilling(pool, nil)
	billingService.SetOutbox(outboxService)
	require.NoError(t, outboxService.RegisterHandler(outbox.EventBillingCancelled, billingService.ProcessUsageJournalEvent))

	requestID := "op_p6f20_cancelled"
	freeze, err := billingService.Freeze(ctx, FreezeRequest{
		RequestID:       requestID,
		OrgID:           orgID,
		Model:           model,
		EstimatedTokens: 1,
		SafetyFactor:    "1",
		MinFreezeCents:  10,
	})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	require.NoError(t, billingService.Cancel(ctx, requestID, orgID))
	require.NoError(t, billingService.Cancel(ctx, requestID, orgID))
	processed, err := outboxService.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM usage_finalization_journal
		 WHERE org_id = $1 AND request_id = $2 AND event_type = 'cancelled'`, orgID, requestID,
	).Scan(&rows))
	require.Equal(t, 1, rows)
}
