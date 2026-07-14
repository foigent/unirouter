package billing

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F17FinancialOperationIdentityIsTenantScoped(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgA, orgB int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('p6f17-billing-a', 'active', 'standard') RETURNING id`,
	).Scan(&orgA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('p6f17-billing-b', 'active', 'standard') RETURNING id`,
	).Scan(&orgB))
	_, err := pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
		 VALUES ($1, 10000000, 0, 0, 0), ($2, 10000000, 0, 0, 0)`, orgA, orgB)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO model_prices (model_name, provider, input_price_cents, output_price_cents)
		 VALUES ('p6f17-model', 'test', 1, 10)`)
	require.NoError(t, err)

	b := NewBilling(pool, nil)
	operationID := "op_00000000000000000000000000000001"
	for _, orgID := range []int64{orgA, orgB} {
		result, freezeErr := b.Freeze(ctx, FreezeRequest{
			RequestID:       operationID,
			OrgID:           orgID,
			Model:           "p6f17-model",
			EstimatedTokens: 1000,
			SafetyFactor:    "1",
			MinFreezeCents:  10,
		})
		require.NoError(t, freezeErr)
		require.True(t, result.Success)
	}

	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM freeze_idempotency WHERE request_id = $1`, operationID,
	).Scan(&rows))
	require.Equal(t, 2, rows, "the same external-looking value must remain isolated by organization")

	settled, err := b.Settle(ctx, SettleRequest{RequestID: operationID, OrgID: orgA, PromptTokens: 1000, UsageSource: "upstream"})
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.NoError(t, b.Cancel(ctx, operationID, orgB))

	var stateA, stateB string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgA, operationID,
	).Scan(&stateA))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgB, operationID,
	).Scan(&stateB))
	require.Equal(t, "settled", stateA)
	require.Equal(t, "cancelled", stateB)
}

func TestP6F17HundredDescendantOperationsSettleAtMostOnceEach(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('p6f17-agent-tree', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))
	_, err := pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
		 VALUES ($1, 100000000, 0, 0, 0)`, orgID)
	require.NoError(t, err)

	b := NewBilling(pool, nil).WithPriceProvider(PriceProviderFunc(func(context.Context, string) (string, string, error) {
		return "1", "10", nil
	}))
	for child := 0; child < 100; child++ {
		operationID := fmt.Sprintf("op_%032x", child+1)
		frozen, freezeErr := b.Freeze(ctx, FreezeRequest{
			RequestID:       operationID,
			OrgID:           orgID,
			Model:           "p6f17-agent-model",
			EstimatedTokens: 1000,
			SafetyFactor:    "1",
			MinFreezeCents:  10,
		})
		require.NoError(t, freezeErr)
		require.True(t, frozen.Success)

		settled, settleErr := b.Settle(ctx, SettleRequest{RequestID: operationID, OrgID: orgID, PromptTokens: 1000, UsageSource: "upstream"})
		require.NoError(t, settleErr)
		require.True(t, settled.Success)
		duplicate, duplicateErr := b.Settle(ctx, SettleRequest{RequestID: operationID, OrgID: orgID, PromptTokens: 1000, UsageSource: "upstream"})
		require.NoError(t, duplicateErr)
		require.True(t, duplicate.Success, "child %d must receive the durable terminal receipt", child)
		require.Equal(t, settled.SettleID, duplicate.SettleID)
	}

	var settledRows, distinctOperations, settleTransactions int
	var frozenMicros, totalSpentMicros int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(DISTINCT request_id)
		 FROM freeze_idempotency WHERE org_id = $1 AND status = 'settled'`, orgID,
	).Scan(&settledRows, &distinctOperations))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions WHERE org_id = $1 AND type = 'settle'`, orgID,
	).Scan(&settleTransactions))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT frozen_micros, total_spent_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&frozenMicros, &totalSpentMicros))
	require.Equal(t, 100, settledRows)
	require.Equal(t, 100, distinctOperations)
	require.Equal(t, 100, settleTransactions)
	require.Zero(t, frozenMicros)
	require.Equal(t, testMicros(100), totalSpentMicros)
}
