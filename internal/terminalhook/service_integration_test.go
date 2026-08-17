//go:build integration

package terminalhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/terminalhook"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F20FinancialTerminalHookSurvivesWorkerFailureAndProcessReplacement(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ($1, 'active', 'standard') RETURNING id`,
		fmt.Sprintf("p6f20-terminal-%d", time.Now().UnixNano()),
	).Scan(&orgID))
	_, err := pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
		 VALUES ($1, 10000000, 0, 0, 0)`, orgID)
	require.NoError(t, err)
	model := fmt.Sprintf("p6f20-terminal-model-%d", time.Now().UnixNano())
	_, err = pool.Exec(ctx,
		`INSERT INTO model_prices (model_name, provider, input_price_cents, output_price_cents)
		 VALUES ($1, 'test', 2, 10)`, model)
	require.NoError(t, err)

	options := outbox.DefaultOptions()
	options.WorkerCount = 1
	options.BatchSize = 10
	options.MaxAttempts = 1
	firstOutbox, err := outbox.New(pool, options)
	require.NoError(t, err)
	financial := billing.NewBilling(pool, nil)
	financial.SetOutbox(firstOutbox)
	for _, eventType := range []string{outbox.EventBillingUsagePrepared, outbox.EventBillingSettled} {
		require.NoError(t, firstOutbox.RegisterHandler(eventType, financial.ProcessUsageJournalEvent))
	}

	firstHooks, err := terminalhook.New(pool, firstOutbox)
	require.NoError(t, err)
	var attempts atomic.Int64
	require.NoError(t, firstHooks.RegisterExecutor(terminalhook.HookAgentAncestor, func(
		_ context.Context,
		payload json.RawMessage,
		terminal string,
		actualTokens int64,
		actualCostMicros int64,
	) error {
		attempts.Add(1)
		require.JSONEq(t, `{"reservation_id":"server-owned"}`, string(payload))
		require.Equal(t, terminalhook.TerminalSettled, terminal)
		require.EqualValues(t, 2, actualTokens)
		require.EqualValues(t, 120, actualCostMicros)
		return errors.New("simulated Redis outage")
	}))
	require.NoError(t, firstOutbox.RegisterHandler(outbox.EventRuntimeTerminal, firstHooks.ProcessEvent))

	requestID := "op_p6f20_terminal_restart"
	frozen, err := financial.Freeze(ctx, billing.FreezeRequest{
		RequestID:       requestID,
		OrgID:           orgID,
		Model:           model,
		EstimatedTokens: 100,
		SafetyFactor:    "1",
		MinFreezeCents:  10,
	})
	require.NoError(t, err)
	require.True(t, frozen.Success)
	require.NoError(t, firstHooks.Register(
		ctx,
		terminalhook.HookAgentAncestor,
		requestID,
		orgID,
		map[string]string{"reservation_id": "server-owned"},
		time.Now().UTC().Add(time.Hour),
	))
	require.NoError(t, financial.MarkBillableOutput(ctx, requestID, orgID))
	usage := billing.SettleRequest{
		RequestID:            requestID,
		OrgID:                orgID,
		PromptTokens:         1,
		UncachedPromptTokens: 1,
		CompletionTokens:     1,
		UsageSource:          "upstream",
		CacheUsageStatus:     "not_reported",
	}
	require.NoError(t, financial.PersistSettlementUsage(ctx, usage))
	settled, err := financial.Settle(ctx, usage)
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.EqualValues(t, 120, settled.ChargedCostMicros)

	processed, err := firstOutbox.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, processed, "financial envelopes materialize before their newly queued runtime hook")
	processed, err = firstOutbox.DrainOnce(ctx)
	require.ErrorContains(t, err, "simulated Redis outage")
	require.Equal(t, 1, processed)
	require.EqualValues(t, 1, attempts.Load())

	var terminalKind, outcome, lastError string
	var completed bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT terminal_kind, terminal_outcome, last_error, completed_at IS NOT NULL
		 FROM runtime_terminal_hooks
		 WHERE operation_id = $1 AND hook_type = $2`, requestID, terminalhook.HookAgentAncestor,
	).Scan(&terminalKind, &outcome, &lastError, &completed))
	require.Equal(t, terminalhook.TerminalSettled, terminalKind)
	require.Empty(t, outcome)
	require.Contains(t, lastError, "simulated Redis outage")
	require.False(t, completed)

	// A fresh service instance represents a replacement process. Financial and
	// runtime terminal events never depend on the old process's memory.
	secondOutbox, err := outbox.New(pool, options)
	require.NoError(t, err)
	secondHooks, err := terminalhook.New(pool, secondOutbox)
	require.NoError(t, err)
	var successes atomic.Int64
	require.NoError(t, secondHooks.RegisterExecutor(terminalhook.HookAgentAncestor, func(
		_ context.Context,
		_ json.RawMessage,
		_ string,
		_ int64,
		_ int64,
	) error {
		attempts.Add(1)
		successes.Add(1)
		return nil
	}))
	require.NoError(t, secondOutbox.RegisterHandler(outbox.EventRuntimeTerminal, secondHooks.ProcessEvent))
	_, err = pool.Exec(ctx,
		`UPDATE internal_outbox_events SET available_at = NOW()
		 WHERE event_type = $1`, outbox.EventRuntimeTerminal)
	require.NoError(t, err)
	processed, err = secondOutbox.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.EqualValues(t, 2, attempts.Load())
	require.EqualValues(t, 1, successes.Load())

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT terminal_outcome, completed_at IS NOT NULL
		 FROM runtime_terminal_hooks
		 WHERE operation_id = $1 AND hook_type = $2`, requestID, terminalhook.HookAgentAncestor,
	).Scan(&outcome, &completed))
	require.Equal(t, "finalized", outcome)
	require.True(t, completed)
	var remaining int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM internal_outbox_events WHERE event_type = $1`, outbox.EventRuntimeTerminal,
	).Scan(&remaining))
	require.Zero(t, remaining)

	// Reconciliation after completion is idempotent and cannot recreate work.
	require.NoError(t, secondHooks.Reconcile(ctx, requestID, orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM internal_outbox_events WHERE event_type = $1`, outbox.EventRuntimeTerminal,
	).Scan(&remaining))
	require.Zero(t, remaining)
}
