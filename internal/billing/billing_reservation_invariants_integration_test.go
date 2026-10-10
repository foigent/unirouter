package billing

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/testutil"
)

func testMicros(cents int) int64 {
	return int64(cents) * money.MicrosPerCent
}

func newReservationFixture(t *testing.T, balance, granted int) (*pgxpool.Pool, int64, string) {
	t.Helper()
	return reservationFixtureWithPool(t, testutil.SetupMigratedTestDB(t), balance, granted)
}

func newPreRetirementReservationFixture(t *testing.T, balance, granted int) (*pgxpool.Pool, int64, string) {
	t.Helper()
	return reservationFixtureWithPool(t, testutil.SetupPreRetirementTestDB(t), balance, granted)
}

func reservationFixtureWithPool(t *testing.T, pool *pgxpool.Pool, balance, granted int) (*pgxpool.Pool, int64, string) {
	t.Helper()
	ctx := t.Context()

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ($1, 'active', 'standard') RETURNING id`,
		fmt.Sprintf("p6f18-%s-%d", t.Name(), time.Now().UnixNano()),
	).Scan(&orgID))
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
			 VALUES ($1, $2, $3, 0, 0)`, orgID, testMicros(balance), testMicros(granted))
		return err
	}())
	model := fmt.Sprintf("p6f18-model-%d", time.Now().UnixNano())
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`INSERT INTO model_prices (model_name, provider, input_price_cents, output_price_cents)
			 VALUES ($1, 'test', 2, 10)`, model)
		return err
	}())
	return pool, orgID, model
}

func freezeForInvariantTest(t *testing.T, b *Billing, orgID int64, model, requestID string, amount int) *FreezeResult {
	return freezeForInvariantTestWithSnapshot(t, b, orgID, model, requestID, amount, "")
}

func freezeForInvariantTestWithSnapshot(
	t *testing.T,
	b *Billing,
	orgID int64,
	model, requestID string,
	amount int,
	executionSnapshotHash string,
) *FreezeResult {
	t.Helper()
	result, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID:             requestID,
		OrgID:                 orgID,
		Model:                 model,
		EstimatedTokens:       1,
		SafetyFactor:          "1",
		MinFreezeCents:        int64(amount),
		ExecutionSnapshotHash: executionSnapshotHash,
	})
	require.NoError(t, err)
	require.True(t, result.Success, "%s: %s", result.ErrorCode, result.ErrorDetail)
	require.Equal(t, testMicros(amount), result.AmountMicros)
	return result
}

func TestP6F18FreezeDoesNotReenterSingleConnectionPool(t *testing.T) {
	base, orgID, model := newReservationFixture(t, 1000, 0)
	config, err := pgxpool.ParseConfig(base.Config().ConnString())
	require.NoError(t, err)
	config.MaxConns = 1
	capped, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(capped.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result, err := NewBilling(capped, nil).Freeze(ctx, FreezeRequest{
		RequestID:       "op_p6f18_pool_barrier",
		OrgID:           orgID,
		Model:           model,
		EstimatedTokens: 1000,
		SafetyFactor:    "1",
		MinFreezeCents:  10,
	})
	require.NoError(t, err)
	require.True(t, result.Success)
}

func TestFreezeReplayRejectsChangedExecutionSnapshot(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_execution_snapshot_replay"
	snapshotA := "mrs1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	snapshotB := "mrs1_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	first := freezeForInvariantTestWithSnapshot(t, b, orgID, model, requestID, 100, snapshotA)
	replay := freezeForInvariantTestWithSnapshot(t, b, orgID, model, requestID, 100, snapshotA)
	require.Equal(t, first.AmountMicros, replay.AmountMicros)

	conflict, err := b.Freeze(t.Context(), FreezeRequest{
		RequestID:             requestID,
		OrgID:                 orgID,
		Model:                 model,
		EstimatedTokens:       1,
		SafetyFactor:          "1",
		MinFreezeCents:        100,
		ExecutionSnapshotHash: snapshotB,
	})
	require.NoError(t, err)
	require.False(t, conflict.Success)
	require.Equal(t, "request_identity_conflict", conflict.ErrorCode)
	require.Contains(t, conflict.ErrorDetail, "Plan snapshot changed")

	var storedSnapshot string
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT execution_snapshot_hash
  FROM freeze_idempotency
 WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&storedSnapshot))
	require.Equal(t, snapshotA, storedSnapshot)
}

func TestP6F18AvailableBalanceDoesNotSubtractHeldFundsTwice(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	freezeForInvariantTest(t, b, orgID, model, "op_p6f18_available_1", 40)
	freezeForInvariantTest(t, b, orgID, model, "op_p6f18_available_2", 50)

	var balanceMicros, frozenMicros int64
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance_micros, frozen_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &frozenMicros))
	require.Equal(t, testMicros(10), balanceMicros)
	require.Equal(t, testMicros(90), frozenMicros)
}

func TestP6F18ConcurrentDuplicateWithExactFundsReturnsOneReceipt(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 100, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f18_exact_funds_duplicate"

	start := make(chan struct{})
	results := make(chan *FreezeResult, 20)
	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := b.Freeze(context.Background(), FreezeRequest{
				RequestID:       requestID,
				OrgID:           orgID,
				Model:           model,
				EstimatedTokens: 1,
				SafetyFactor:    "1",
				MinFreezeCents:  100,
			})
			results <- result
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for result := range results {
		require.NotNil(t, result)
		require.True(t, result.Success, "%s: %s", result.ErrorCode, result.ErrorDetail)
		require.Equal(t, testMicros(100), result.AmountMicros)
	}

	var balanceMicros, frozenMicros int64
	var freezeEvents int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance_micros, frozen_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &frozenMicros))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type = 'freeze'`, orgID, requestID,
	).Scan(&freezeEvents))
	require.Zero(t, balanceMicros)
	require.Equal(t, testMicros(100), frozenMicros)
	require.Equal(t, 1, freezeEvents)
}

func TestP6F18CancelRestoresOriginalCashAndGrantPools(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 5000, 500)
	ctx := t.Context()
	var grantID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO grants (org_id, amount_micros, balance_micros, status, reason, created_by)
		 VALUES ($1, 5000000, 5000000, 'active', 'p6f18', 0) RETURNING id`, orgID,
	).Scan(&grantID))

	b := NewBilling(pool, nil)
	requestID := "op_p6f18_source_cancel"
	freezeForInvariantTest(t, b, orgID, model, requestID, 2000)
	require.NoError(t, b.Cancel(ctx, requestID, orgID))
	require.NoError(t, b.Cancel(ctx, requestID, orgID), "duplicate cancel must be idempotent")

	var balanceMicros, grantedMicros, frozenMicros, grantBalanceMicros int64
	var terminals int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance_micros, granted_balance_micros, frozen_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &grantedMicros, &frozenMicros))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance_micros FROM grants WHERE id = $1`, grantID,
	).Scan(&grantBalanceMicros))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminals))
	require.Equal(t, testMicros(5000), balanceMicros)
	require.Equal(t, testMicros(500), grantedMicros)
	require.Zero(t, frozenMicros)
	require.Equal(t, testMicros(500), grantBalanceMicros)
	require.Equal(t, 1, terminals)
}

func TestP6F18SettleRefundsUnusedFundsToOriginalPools(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 5000, 500)
	ctx := t.Context()
	var grantID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO grants (org_id, amount_micros, balance_micros, status, reason, created_by)
		 VALUES ($1, 5000000, 5000000, 'active', 'p6f18-settle', 0) RETURNING id`, orgID,
	).Scan(&grantID))

	b := NewBilling(pool, nil)
	requestID := "op_p6f18_source_settle"
	freezeForInvariantTest(t, b, orgID, model, requestID, 2000)
	settled, err := b.Settle(ctx, SettleRequest{RequestID: requestID, OrgID: orgID, PromptTokens: 50_000, UsageSource: "upstream"})
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Equal(t, testMicros(1900), settled.RefundMicros)

	var balanceMicros, grantedMicros, frozenMicros, spentMicros, grantBalanceMicros int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance_micros, granted_balance_micros, frozen_micros, total_spent_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &grantedMicros, &frozenMicros, &spentMicros))
	require.NoError(t, pool.QueryRow(ctx, `SELECT balance_micros FROM grants WHERE id = $1`, grantID).Scan(&grantBalanceMicros))
	require.Equal(t, testMicros(5000), balanceMicros, "all reserved cash was unused and must return to cash")
	require.Equal(t, testMicros(400), grantedMicros, "100 cents consumed from the earliest grant")
	require.Equal(t, testMicros(400), grantBalanceMicros)
	require.Zero(t, frozenMicros)
	require.Equal(t, testMicros(100), spentMicros)
	require.Equal(t, testMicros(5500), balanceMicros+grantedMicros+frozenMicros+spentMicros)
}

func TestP6F18ExpiredGrantCannotBeSpentBeforeScheduledCleanup(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 0, 100)
	ctx := t.Context()
	var grantID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO grants (org_id, amount_micros, balance_micros, status, expires_at, reason, created_by)
		 VALUES ($1, 1000000, 1000000, 'active', NOW() - INTERVAL '1 second', 'expired', 0)
		 RETURNING id`, orgID,
	).Scan(&grantID))

	result, err := NewBilling(pool, nil).Freeze(ctx, FreezeRequest{
		RequestID:       "op_p6f18_expired_grant",
		OrgID:           orgID,
		Model:           model,
		EstimatedTokens: 1,
		SafetyFactor:    "1",
		MinFreezeCents:  100,
	})
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "insufficient_balance", result.ErrorCode)

	var grantedMicros, grantBalanceMicros int64
	var status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT granted_balance_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&grantedMicros))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance_micros, status FROM grants WHERE id = $1`, grantID,
	).Scan(&grantBalanceMicros, &status))
	require.Zero(t, grantedMicros)
	require.Zero(t, grantBalanceMicros)
	require.Equal(t, "expired", status)
}

func TestP6F18ExpiredHeldGrantIsNotConvertedToCashOnCancel(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 0, 100)
	ctx := t.Context()
	var grantID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO grants (org_id, amount_micros, balance_micros, status, expires_at, reason, created_by)
		 VALUES ($1, 1000000, 1000000, 'active', NOW() + INTERVAL '1 day', 'held-expiry', 0)
		 RETURNING id`, orgID,
	).Scan(&grantID))

	b := NewBilling(pool, nil)
	requestID := "op_p6f18_held_grant_expiry"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	_, err := pool.Exec(ctx, `UPDATE grants SET expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, grantID)
	require.NoError(t, err)
	require.NoError(t, b.Cancel(ctx, requestID, orgID))

	var balanceMicros, grantedMicros, frozenMicros, forfeitedMicros int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT balance_micros, granted_balance_micros, frozen_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &grantedMicros, &frozenMicros))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT forfeited_grant_amount_micros FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&forfeitedMicros))
	require.Zero(t, balanceMicros, "expired grant credit must not become cash")
	require.Zero(t, grantedMicros)
	require.Zero(t, frozenMicros)
	require.Equal(t, testMicros(100), forfeitedMicros)
}

func TestP6F18SettlementUsesFrozenPriceAndMultiplierVersion(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 5000, 0)
	ctx := t.Context()
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`INSERT INTO org_model_prices (org_id, model_name, multiplier)
			 VALUES ($1, $2, 2)`, orgID, model)
		return err
	}())

	b := NewBilling(pool, nil)
	requestID := "op_p6f18_price_snapshot"
	freeze := freezeForInvariantTest(t, b, orgID, model, requestID, 100)
	require.Equal(t, testMicros(100), freeze.AmountMicros)

	var inputPriceMicros, outputPriceMicros int64
	var multiplier float64
	var pricingVersion string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT effective_input_price_micros_per_million,
		        effective_output_price_micros_per_million,
		        price_multiplier::float8,
		        pricing_version
		 FROM freeze_idempotency
		 WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&inputPriceMicros, &outputPriceMicros, &multiplier, &pricingVersion))
	require.Equal(t, int64(40_000_000), inputPriceMicros)
	require.Equal(t, int64(200_000_000), outputPriceMicros)
	require.Equal(t, 2.0, multiplier)
	require.NotEqual(t, "legacy", pricingVersion)

	// Mutating the live catalog after reservation must not rewrite the price of
	// the already accepted operation.
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`UPDATE model_prices SET input_price_cents = 100, output_price_cents = 200, updated_at = NOW()
			 WHERE model_name = $1`, model)
		return err
	}())
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx,
			`UPDATE org_model_prices SET multiplier = 3, updated_at = NOW()
			 WHERE org_id = $1 AND model_name = $2`, orgID, model)
		return err
	}())

	settled, err := b.Settle(ctx, SettleRequest{
		RequestID:    requestID,
		OrgID:        orgID,
		PromptTokens: 1000,
		UsageSource:  "upstream",
	})
	require.NoError(t, err)
	require.True(t, settled.Success)
	require.Equal(t, int64(40_000), settled.ActualCostMicros, "snapshot price must win over the live catalog")
	require.Equal(t, int64(960_000), settled.RefundMicros)

	duplicate, err := b.Settle(ctx, SettleRequest{
		RequestID:    requestID,
		OrgID:        orgID,
		PromptTokens: 1000,
		UsageSource:  "upstream",
	})
	require.NoError(t, err)
	require.True(t, duplicate.Success, "same terminal operation must return its durable receipt")
	require.Equal(t, settled.SettleID, duplicate.SettleID)
	require.Equal(t, settled.ActualCostMicros, duplicate.ActualCostMicros)
}

func TestP6F18HundredWaySettleCancelRaceHasOneTerminalEvent(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 1000, 0)
	b := NewBilling(pool, nil)
	requestID := "op_p6f18_terminal_race"
	freezeForInvariantTest(t, b, orgID, model, requestID, 100)

	start := make(chan struct{})
	errs := make(chan error, 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			if index%2 == 0 {
				_, err := b.Settle(context.Background(), SettleRequest{
					RequestID:    requestID,
					OrgID:        orgID,
					PromptTokens: 12_500,
					UsageSource:  "upstream",
				})
				errs <- err
				return
			}
			errs <- b.Cancel(context.Background(), requestID, orgID)
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var state string
	var balanceMicros, grantedMicros, frozenMicros, spentMicros int64
	var terminalEvents int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT status FROM freeze_idempotency WHERE org_id = $1 AND request_id = $2`, orgID, requestID,
	).Scan(&state))
	require.Contains(t, []string{"settled", "cancelled"}, state)
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT balance_micros, granted_balance_micros, frozen_micros, total_spent_micros FROM wallets WHERE org_id = $1`, orgID,
	).Scan(&balanceMicros, &grantedMicros, &frozenMicros, &spentMicros))
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT COUNT(*) FROM transactions
		 WHERE org_id = $1 AND request_id = $2 AND type IN ('settle', 'cancel')`, orgID, requestID,
	).Scan(&terminalEvents))
	require.Zero(t, frozenMicros)
	require.Equal(t, testMicros(1000), balanceMicros+grantedMicros+frozenMicros+spentMicros, "wallet plus durable spend must conserve value")
	require.Equal(t, 1, terminalEvents)
}
