//go:build integration

package billing

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

// --- helpers ---
// （helper 逻辑直接内联在各测试中，使用 testutil.SetupTestDB/NewTestOrg/NewTestWallet/NewTestGrant/NewTestModelPrice）

// --- Freeze tests ---

func TestFreeze_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	result, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       fmt.Sprintf("freeze-ok-%d", time.Now().UnixNano()),
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 1000,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "freeze should succeed: %s - %s", result.ErrorCode, result.ErrorDetail)
	require.GreaterOrEqual(t, result.Amount, 10, "freeze amount should be >= minFreezeAmount")

	// Verify wallet state
	var balance, frozen int
	err = pool.QueryRow(context.Background(),
		`SELECT balance, frozen FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen)
	require.NoError(t, err)
	require.Equal(t, result.Amount, frozen, "frozen should match freeze amount")
	require.Equal(t, 10000-result.Amount, balance, "balance should decrease by freeze amount")
}

func TestFreeze_InsufficientBalance_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 5) // only 5 cents
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	result, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       fmt.Sprintf("freeze-insuf-%d", time.Now().UnixNano()),
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 10000,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Success, "freeze should fail with insufficient balance")
	require.Equal(t, "insufficient_balance", result.ErrorCode)
}

func TestFreeze_Idempotency_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	reqID := fmt.Sprintf("freeze-idem-%d", time.Now().UnixNano())

	// First freeze
	result1, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       reqID,
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 1000,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.True(t, result1.Success)

	// Second freeze with same request_id — should be idempotent
	result2, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       reqID,
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 1000,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.NotNil(t, result2)

	// Verify balance only deducted once
	var balance int
	err = pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 10000-result1.Amount, balance, "balance should only be deducted once")
}

func TestFreeze_ConcurrentDuplicateRequestAcrossInstances_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	stable := NewBilling(pool, nil)
	canary := NewBilling(pool, nil)
	reqID := fmt.Sprintf("ha-freeze-%d", time.Now().UnixNano())
	req := FreezeRequest{
		RequestID:       reqID,
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 1000,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	}

	start := make(chan struct{})
	results := make(chan *FreezeResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, instance := range []*Billing{stable, canary} {
		wg.Add(1)
		go func(b *Billing) {
			defer wg.Done()
			<-start
			result, err := b.Freeze(context.Background(), req)
			results <- result
			errs <- err
		}(instance)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	var freezeAmount int
	for err := range errs {
		require.NoError(t, err, "duplicate request across HA instances should not surface an idempotency race")
	}
	for result := range results {
		require.NotNil(t, result)
		require.True(t, result.Success)
		if freezeAmount == 0 {
			freezeAmount = result.Amount
		}
		require.Equal(t, freezeAmount, result.Amount)
	}

	var balance, frozen, idemRows, freezeTxRows int
	err := pool.QueryRow(context.Background(),
		`SELECT balance, frozen FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen)
	require.NoError(t, err)
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM freeze_idempotency WHERE request_id = $1`, reqID,
	).Scan(&idemRows)
	require.NoError(t, err)
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM transactions WHERE request_id = $1 AND type = 'freeze'`, reqID,
	).Scan(&freezeTxRows)
	require.NoError(t, err)

	require.Equal(t, 1, idemRows, "shared DB idempotency row must be unique")
	require.Equal(t, 1, freezeTxRows, "duplicate HA request must not write duplicate freeze transactions")
	require.Equal(t, freezeAmount, frozen, "wallet frozen amount should reflect a single freeze")
	require.Equal(t, 10000-freezeAmount, balance, "wallet balance should be deducted once")
}

func TestEventService_CheckProjectBudgetPeriodBoundaries_Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	cases := []struct {
		name                string
		period              string
		now                 time.Time
		currentPeriodLogAt  time.Time
		previousPeriodLogAt time.Time
		expectedPeriodStart string
	}{
		{
			name:                "daily budget ignores previous day spend",
			period:              "daily",
			now:                 time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC),
			currentPeriodLogAt:  time.Date(2026, 7, 15, 1, 15, 0, 0, time.UTC),
			previousPeriodLogAt: time.Date(2026, 7, 14, 23, 59, 0, 0, time.UTC),
			expectedPeriodStart: "2026-07-15T00:00:00Z",
		},
		{
			name:                "monthly budget ignores previous month spend",
			period:              "monthly",
			now:                 time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC),
			currentPeriodLogAt:  time.Date(2026, 7, 2, 1, 15, 0, 0, time.UTC),
			previousPeriodLogAt: time.Date(2026, 6, 30, 23, 59, 0, 0, time.UTC),
			expectedPeriodStart: "2026-07-01T00:00:00Z",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.TruncateAll(t, pool)

			var orgID int64
			err := pool.QueryRow(ctx,
				`INSERT INTO orgs (name, status, org_type)
				 VALUES ($1, 'active', 'customer')
				 RETURNING id`,
				fmt.Sprintf("budget-boundary-%s-%d", tc.period, time.Now().UnixNano()),
			).Scan(&orgID)
			require.NoError(t, err)

			var projectID int64
			err = pool.QueryRow(ctx,
				`INSERT INTO projects (org_id, name, slug, description, status, budget_cents, budget_period, created_at, updated_at)
				 VALUES ($1, $2, $3, 'period boundary fixture', 'active', 1000, $4, NOW(), NOW())
				 RETURNING id`,
				orgID, "Budget Boundary Project", fmt.Sprintf("budget-boundary-%s", tc.period), tc.period,
			).Scan(&projectID)
			require.NoError(t, err)

			_, err = pool.Exec(ctx,
				`INSERT INTO call_logs (request_id, org_id, project_id, model, cost_cents, status_code, created_at)
				 VALUES
				   ($1, $2, $3, 'budget-boundary-model', 1100, 200, $4),
				   ($5, $2, $3, 'budget-boundary-model', 5000, 200, $6)`,
				fmt.Sprintf("budget-current-%s-%d", tc.period, time.Now().UnixNano()), orgID, projectID, tc.currentPeriodLogAt,
				fmt.Sprintf("budget-previous-%s-%d", tc.period, time.Now().UnixNano()), tc.previousPeriodLogAt,
			)
			require.NoError(t, err)

			svc := NewEventService(pool)
			svc.now = func() time.Time { return tc.now }
			result, err := svc.CheckProjectBudget(ctx, orgID, projectID)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.Created)
			require.Equal(t, DeliveryStateNotConfigured, result.DeliveryState)

			var periodStart string
			var spentCents, usagePercent int
			err = pool.QueryRow(ctx,
				`SELECT payload->>'period_start',
				        (payload->>'spent_cents')::int,
				        (payload->>'usage_percent')::int
				 FROM notification_events
				 WHERE id = $1`,
				result.EventID,
			).Scan(&periodStart, &spentCents, &usagePercent)
			require.NoError(t, err)
			require.Equal(t, tc.expectedPeriodStart, periodStart)
			require.Equal(t, 1100, spentCents, "previous-period spend must not count toward the current budget threshold")
			require.Equal(t, 110, usagePercent)
		})
	}
}

func TestFreeze_MinAmount_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	result, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       fmt.Sprintf("freeze-min-%d", time.Now().UnixNano()),
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 1, // very small
		SafetyFactor:    1.0,
		MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success)
	require.GreaterOrEqual(t, result.Amount, 10, "should respect min freeze amount")
}

// --- Settle tests ---

func TestSettle_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	reqID := fmt.Sprintf("settle-ok-%d", time.Now().UnixNano())

	// Freeze first
	freezeResult, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       reqID,
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 1000,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.True(t, freezeResult.Success)

	// Settle with actual cost less than frozen
	settleResult, err := b.Settle(context.Background(), SettleRequest{
		RequestID:        reqID,
		OrgID:            orgID,
		ActualCost:       freezeResult.Amount - 5,
		PromptTokens:     500,
		CompletionTokens: 300,
		UsageSource:      "upstream",
	})
	require.NoError(t, err)
	require.NotNil(t, settleResult)
	require.True(t, settleResult.Success, "settle should succeed: %s", settleResult.ErrorCode)
	require.GreaterOrEqual(t, settleResult.Refund, 0, "refund should be non-negative")

	// Verify frozen is now 0
	var frozen int
	err = pool.QueryRow(context.Background(),
		`SELECT frozen FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&frozen)
	require.NoError(t, err)
	require.Equal(t, 0, frozen, "frozen should be 0 after settle")
}

func TestSettle_NotFound_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	b := NewBilling(pool, nil)
	result, err := b.Settle(context.Background(), SettleRequest{
		RequestID:  "nonexistent-request-id",
		OrgID:      orgID,
		ActualCost: 100,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Success)
	require.Equal(t, "not_found", result.ErrorCode)
}

// --- Cancel tests ---

func TestCancel_Success_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	reqID := fmt.Sprintf("cancel-ok-%d", time.Now().UnixNano())

	// Freeze
	freezeResult, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       reqID,
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 1000,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.True(t, freezeResult.Success)

	// Cancel
	err = b.Cancel(context.Background(), reqID, orgID)
	require.NoError(t, err)

	// Verify frozen is 0 and balance restored
	var balance, frozen int
	err = pool.QueryRow(context.Background(),
		`SELECT balance, frozen FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &frozen)
	require.NoError(t, err)
	require.Equal(t, 0, frozen, "frozen should be 0 after cancel")
	require.Equal(t, 10000, balance, "balance should be fully restored")
}

func TestCancel_AlreadyCancelled_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	reqID := fmt.Sprintf("cancel-twice-%d", time.Now().UnixNano())

	// Freeze
	freezeResult, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       reqID,
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 1000,
		SafetyFactor:    1.5,
		MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.True(t, freezeResult.Success)

	// Cancel first time
	err = b.Cancel(context.Background(), reqID, orgID)
	require.NoError(t, err)

	// Cancel second time — idempotent, no error expected
	err = b.Cancel(context.Background(), reqID, orgID)
	// Cancel is idempotent: canceling an already-cancelled freeze succeeds silently
	require.NoError(t, err, "cancel should be idempotent")
}

// --- EstimateFreezeAmount tests ---

func TestEstimateFreezeAmount_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)

	// Normal case
	amount, err := b.EstimateFreezeAmount(context.Background(), "gpt-4o", 1000, 1.5, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, amount, 10)

	// Small estimate should respect min
	amount, err = b.EstimateFreezeAmount(context.Background(), "gpt-4o", 1, 1.0, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, amount, 10)
}

// --- CalculateCost tests ---

func TestCalculateCost_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)

	// cost = (500*2 + 300*10) / 1000 = 4
	cost, err := b.CalculateCost(context.Background(), "gpt-4o", 500, 300)
	require.NoError(t, err)
	require.Equal(t, 4, cost)

	// Very small should be at least 1
	cost, err = b.CalculateCost(context.Background(), "gpt-4o", 1, 1)
	require.NoError(t, err)
	require.GreaterOrEqual(t, cost, 1, "minimum cost should be 1 cent")
}

// --- QueryModelPrice tests ---

func TestQueryModelPrice_Found_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 5, 15)

	b := NewBilling(pool, nil)
	inputCost, outputCost, err := b.QueryModelPrice(context.Background(), "gpt-4o")
	require.NoError(t, err)
	require.InDelta(t, 5.0, inputCost, 0.000001)
	require.InDelta(t, 15.0, outputCost, 0.000001)
}

func TestQueryModelPrice_NotFound_Fallback_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	b := NewBilling(pool, nil)
	inputCost, outputCost, err := b.QueryModelPrice(context.Background(), "nonexistent-model")
	require.NoError(t, err)
	require.InDelta(t, 2.0, inputCost, 0.000001, "should fallback to default input cost")
	require.InDelta(t, 10.0, outputCost, 0.000001, "should fallback to default output cost")
}

// --- QueryModelPriceForOrg tests ---

func TestQueryModelPriceForOrg_NoMultiplier_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "claude-opus", 15, 75)

	b := NewBilling(pool, nil)
	inputCost, outputCost, err := b.QueryModelPriceForOrg(context.Background(), 99999, "claude-opus")
	require.NoError(t, err)
	require.InDelta(t, 15.0, inputCost, 0.000001, "should return base price when no org multiplier")
	require.InDelta(t, 75.0, outputCost, 0.000001)
}

func TestQueryModelPriceForOrg_WithMultiplier_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestModelPrice(t, pool, "org-model-test", 10, 50)

	// Set org-level multiplier
	_, err := pool.Exec(context.Background(),
		`INSERT INTO org_model_prices (org_id, model_name, multiplier) VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, model_name) DO UPDATE SET multiplier = $3`,
		orgID, "org-model-test", 2.0)
	require.NoError(t, err)

	b := NewBilling(pool, nil)
	inputCost, outputCost, err := b.QueryModelPriceForOrg(context.Background(), orgID, "org-model-test")
	require.NoError(t, err)
	require.InDelta(t, 20.0, inputCost, 0.000001, "10 * 2.0 = 20")
	require.InDelta(t, 100.0, outputCost, 0.000001, "50 * 2.0 = 100")
}

func TestQueryModelPriceForOrg_MultiplierOne_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestModelPrice(t, pool, "mult1-model", 10, 50)

	_, err := pool.Exec(context.Background(),
		`INSERT INTO org_model_prices (org_id, model_name, multiplier) VALUES ($1, $2, $3)`,
		orgID, "mult1-model", 1.0)
	require.NoError(t, err)

	b := NewBilling(pool, nil)
	inputCost, outputCost, err := b.QueryModelPriceForOrg(context.Background(), orgID, "mult1-model")
	require.NoError(t, err)
	require.InDelta(t, 10.0, inputCost, 0.000001, "10 * 1.0 = 10")
	require.InDelta(t, 50.0, outputCost, 0.000001, "50 * 1.0 = 50")
}

func TestQueryModelPriceForOrg_FallbackThenMultiplier_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	// No model_prices entry → will fallback to default (2, 10)

	_, err := pool.Exec(context.Background(),
		`INSERT INTO org_model_prices (org_id, model_name, multiplier) VALUES ($1, $2, $3)`,
		orgID, "fallback-model", 3.0)
	require.NoError(t, err)

	b := NewBilling(pool, nil)
	inputCost, outputCost, err := b.QueryModelPriceForOrg(context.Background(), orgID, "fallback-model")
	require.NoError(t, err)
	require.InDelta(t, 6.0, inputCost, 0.000001, "2 * 3.0 = 6")
	require.InDelta(t, 30.0, outputCost, 0.000001, "10 * 3.0 = 30")
}

func TestQueryModelPriceForOrg_QueryErrorDoesNotFallback_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	b := NewBilling(pool, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inputCost, outputCost, err := b.QueryModelPriceForOrg(ctx, 99999, "gpt-4o")
	require.Error(t, err)
	require.Zero(t, inputCost)
	require.Zero(t, outputCost)
	require.Contains(t, err.Error(), "query model price")
}

// --- CalculateCost: boundary tests ---

func TestCalculateCost_ExactlyOneCent_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	// price: input=2, output=10 cents per 1K
	// cost = (prompt*2 + completion*10) / 1000
	// For cost=1: need promptTokens*2 + completionTokens*10 >= 1000
	// e.g. 500 prompt (=1000 input units) → cost=1
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	// 500 prompt tokens * 2 / 1000 = 1.0 → ceil? No, it's int division: 1000/1000 = 1
	cost, err := b.CalculateCost(context.Background(), "gpt-4o", 500, 0)
	require.NoError(t, err)
	require.Equal(t, 1, cost, "500 * 2 / 1000 = 1")
}

func TestCalculateCost_SubCentRoundsUp_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	// 1 prompt token * 2 / 1000 = 0.002 → int(0) → min 1
	cost, err := b.CalculateCost(context.Background(), "gpt-4o", 1, 0)
	require.NoError(t, err)
	require.Equal(t, 1, cost, "sub-cent cost should be rounded up to 1")
}

func TestCalculateCost_LargeTokens_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	// 100000 prompt + 50000 completion = (200000 + 500000) / 1000 = 700
	cost, err := b.CalculateCost(context.Background(), "gpt-4o", 100000, 50000)
	require.NoError(t, err)
	require.Equal(t, 700, cost)
}

func TestCalculateCost_OnlyCompletionTokens_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	// 0 prompt + 100 completion * 10 / 1000 = 1
	cost, err := b.CalculateCost(context.Background(), "gpt-4o", 0, 100)
	require.NoError(t, err)
	require.Equal(t, 1, cost)
}

// --- EstimateFreezeAmount: boundary tests ---

func TestEstimateFreezeAmount_SafetyFactorBoundaries_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)

	// safetyFactor 0.5: 1000 * 10 / 1000 * 0.5 = 5 → minFreezeAmount=1 → 5
	amount, err := b.EstimateFreezeAmount(context.Background(), "gpt-4o", 1000, 0.5, 1)
	require.NoError(t, err)
	require.Equal(t, 5, amount)

	// safetyFactor 2.0: 1000 * 10 / 1000 * 2.0 = 20
	amount, err = b.EstimateFreezeAmount(context.Background(), "gpt-4o", 1000, 2.0, 1)
	require.NoError(t, err)
	require.Equal(t, 20, amount)
}

func TestEstimateFreezeAmount_MinFreezeDominates_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)

	// 1 token * 10 / 1000 * 1.0 = 0 (int rounds down) → minFreeze=100 → 100
	amount, err := b.EstimateFreezeAmount(context.Background(), "gpt-4o", 1, 1.0, 100)
	require.NoError(t, err)
	require.GreaterOrEqual(t, amount, 100, "min freeze amount should dominate")
}

func TestEstimateFreezeAmount_ZeroSafetyFactor_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)

	// safetyFactor 0.0: 1000 * 10 / 1000 * 0.0 = 0 → minFreezeAmount=5 → 5
	amount, err := b.EstimateFreezeAmount(context.Background(), "gpt-4o", 1000, 0.0, 5)
	require.NoError(t, err)
	require.Equal(t, 5, amount, "zero safety factor should fall back to min")
}

// --- Freeze: granted_balance priority tests ---

func TestFreeze_ConsumesGrantedFirst_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	// Wallet: balance=5000, granted_balance=3000
	testutil.NewTestWallet(t, pool, orgID, 5000)
	testutil.NewTestGrant(t, pool, orgID, 3000, nil)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	// Also set wallet's granted_balance to match the grant
	_, err := pool.Exec(context.Background(),
		`UPDATE wallets SET granted_balance = 3000 WHERE org_id = $1`, orgID)
	require.NoError(t, err)

	b := NewBilling(pool, nil)
	// Freeze 2000: should take 2000 from granted_balance, balance stays 5000
	result, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       fmt.Sprintf("freeze-grant-%d", time.Now().UnixNano()),
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 10000,
		SafetyFactor:    1.0,
		MinFreezeAmount: 2000,
	})
	require.NoError(t, err)
	require.True(t, result.Success, "freeze should succeed: %s - %s", result.ErrorCode, result.ErrorDetail)

	var balance, grantedBalance, frozen int
	err = pool.QueryRow(context.Background(),
		`SELECT balance, granted_balance, frozen FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &grantedBalance, &frozen)
	require.NoError(t, err)
	require.Equal(t, 2000, frozen, "2000 should be frozen")
	require.Equal(t, 5000, balance, "balance should be untouched (granted consumed first)")
	require.Equal(t, 1000, grantedBalance, "3000 - 2000 = 1000 remaining granted")
}

func TestFreeze_PartialGrantPartialBalance_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	// Wallet: balance=5000, granted_balance=500
	testutil.NewTestWallet(t, pool, orgID, 5000)
	testutil.NewTestGrant(t, pool, orgID, 500, nil)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	_, err := pool.Exec(context.Background(),
		`UPDATE wallets SET granted_balance = 500 WHERE org_id = $1`, orgID)
	require.NoError(t, err)

	b := NewBilling(pool, nil)
	// Freeze 2000: should take 500 from granted, 1500 from balance
	result, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID:       fmt.Sprintf("freeze-partial-%d", time.Now().UnixNano()),
		OrgID:           orgID,
		Model:           "gpt-4o",
		EstimatedTokens: 10000,
		SafetyFactor:    1.0,
		MinFreezeAmount: 2000,
	})
	require.NoError(t, err)
	require.True(t, result.Success)

	var balance, grantedBalance, frozen int
	err = pool.QueryRow(context.Background(),
		`SELECT balance, granted_balance, frozen FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balance, &grantedBalance, &frozen)
	require.NoError(t, err)
	require.Equal(t, 2000, frozen)
	require.Equal(t, 3500, balance, "5000 - 1500 = 3500")
	require.Equal(t, 0, grantedBalance, "all granted consumed")
}

// --- Settle: boundary tests ---

func TestSettle_ExactCost_NoRefund_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	reqID := fmt.Sprintf("settle-exact-%d", time.Now().UnixNano())

	// Freeze
	fr, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID: reqID, OrgID: orgID, Model: "gpt-4o",
		EstimatedTokens: 1000, SafetyFactor: 1.0, MinFreezeAmount: 10,
	})
	require.NoError(t, err)
	require.True(t, fr.Success)

	// Settle with exact cost
	sr, err := b.Settle(context.Background(), SettleRequest{
		RequestID:  reqID,
		OrgID:      orgID,
		ActualCost: fr.Amount,
	})
	require.NoError(t, err)
	require.True(t, sr.Success)
	require.Equal(t, 0, sr.Refund, "exact cost should have 0 refund")
}

func TestSettle_CostExceedsFrozen_LossLogged_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)
	testutil.NewTestWallet(t, pool, orgID, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)

	b := NewBilling(pool, nil)
	reqID := fmt.Sprintf("settle-loss-%d", time.Now().UnixNano())

	// Freeze small
	fr, err := b.Freeze(context.Background(), FreezeRequest{
		RequestID: reqID, OrgID: orgID, Model: "gpt-4o",
		EstimatedTokens: 10, SafetyFactor: 1.0, MinFreezeAmount: 5,
	})
	require.NoError(t, err)
	require.True(t, fr.Success)

	// Settle with cost > frozen — should still succeed but log warning
	sr, err := b.Settle(context.Background(), SettleRequest{
		RequestID:        reqID,
		OrgID:            orgID,
		ActualCost:       fr.Amount + 100,
		PromptTokens:     500,
		CompletionTokens: 300,
		UsageSource:      "upstream",
	})
	require.NoError(t, err)
	require.True(t, sr.Success, "settle should succeed even when cost > frozen (refund caps at 0)")
	require.Equal(t, 0, sr.Refund, "refund should be 0 when cost exceeds frozen")
}

// --- Cancel: idempotency edge case ---

func TestCancel_NonExistent_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	orgID := testutil.NewTestOrg(t, pool)

	b := NewBilling(pool, nil)
	err := b.Cancel(context.Background(), "never-existed-request", orgID)
	require.NoError(t, err, "cancelling non-existent should be idempotent (no error)")
}
