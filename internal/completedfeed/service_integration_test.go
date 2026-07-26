//go:build integration

package completedfeed

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestMaterializationProjectFenceOrdersVisibilityByCommit(t *testing.T) {
	pool, service, orgID, projectID := setupCompletedFeedIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	first := completedFeedTerminalRecord("order-first", orgID, projectID, time.Now().UTC())
	second := completedFeedTerminalRecord("order-second", orgID, projectID, time.Now().UTC().Add(-time.Hour))

	firstTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer firstTx.Rollback(context.Background())
	require.NoError(t, service.MaterializeTerminalTx(ctx, firstTx, first))

	secondTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer secondTx.Rollback(context.Background())
	secondResult := make(chan error, 1)
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		secondResult <- service.MaterializeTerminalTx(ctx, secondTx, second)
	}()
	<-secondStarted

	select {
	case err := <-secondResult:
		require.Failf(t, "second materialization returned before first commit", "error=%v", err)
	case <-time.After(150 * time.Millisecond):
	}

	beforeCommit, err := service.reader.Read(ctx, orgID, projectID, 0, MaxPageItems+1)
	require.NoError(t, err)
	require.Empty(t, beforeCommit.Items)
	require.Zero(t, beforeCommit.HighWatermark)

	require.NoError(t, firstTx.Commit(ctx))
	select {
	case err := <-secondResult:
		require.NoError(t, err)
	case <-ctx.Done():
		require.FailNow(t, "second materialization did not acquire the project fence")
	}

	firstVisible, err := service.reader.Read(ctx, orgID, projectID, 0, MaxPageItems+1)
	require.NoError(t, err)
	require.Len(t, firstVisible.Items, 1)
	require.Equal(t, first.RequestID, firstVisible.Items[0].RequestID)
	require.Equal(t, firstVisible.Items[0].Sequence, firstVisible.HighWatermark)

	require.NoError(t, secondTx.Commit(ctx))
	bothVisible, err := service.reader.Read(ctx, orgID, projectID, 0, MaxPageItems+1)
	require.NoError(t, err)
	require.Len(t, bothVisible.Items, 2)
	require.Equal(t, first.RequestID, bothVisible.Items[0].RequestID)
	require.Equal(t, second.RequestID, bothVisible.Items[1].RequestID)
	require.Less(t, bothVisible.Items[0].Sequence, bothVisible.Items[1].Sequence)
	require.Equal(t, bothVisible.Items[1].Sequence, bothVisible.HighWatermark)
}

func TestMaterializationRollbackAndReplayRemainSingleFinalItem(t *testing.T) {
	pool, service, orgID, projectID := setupCompletedFeedIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	record := completedFeedTerminalRecord("rollback-replay", orgID, projectID, time.Now().UTC())

	rolledBack, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, service.MaterializeTerminalTx(ctx, rolledBack, record))
	require.NoError(t, rolledBack.Rollback(ctx))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM completed_execution_feed_items WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&count))
	require.Zero(t, count)

	committed, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, service.MaterializeTerminalTx(ctx, committed, record))
	require.NoError(t, committed.Commit(ctx))

	var retainedID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM completed_execution_feed_items WHERE event_key = $1`,
		record.EventKey,
	).Scan(&retainedID))

	replayed, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, service.MaterializeTerminalTx(ctx, replayed, record))
	require.NoError(t, replayed.Commit(ctx))

	var replayedID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM completed_execution_feed_items WHERE event_key = $1`,
		record.EventKey,
	).Scan(&replayedID))
	require.Equal(t, retainedID, replayedID)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM completed_execution_feed_items WHERE org_id = $1 AND request_id = $2`,
		orgID, record.RequestID,
	).Scan(&count))
	require.Equal(t, 1, count)
}

func TestMaterializationCanonicalizesTerminalTimestampToPostgresPrecision(t *testing.T) {
	pool, service, orgID, projectID := setupCompletedFeedIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	terminalAt := time.Date(2026, 7, 26, 9, 52, 52, 123456789, time.UTC)
	record := completedFeedTerminalRecord(
		"postgres-time-precision",
		orgID,
		projectID,
		terminalAt,
	)

	materializeCompletedFeedRecord(t, ctx, pool, service, record)

	window, err := service.reader.Read(ctx, orgID, projectID, 0, MaxPageItems+1)
	require.NoError(t, err)
	require.Len(t, window.Items, 1)
	snapshot, err := DecodeSnapshot(window.Items[0].Snapshot)
	require.NoError(t, err)
	expected := terminalAt.Truncate(time.Microsecond)
	require.True(t, expected.Equal(window.Items[0].TerminalAvailableAt))
	require.True(t, expected.Equal(snapshot.Terminal.AvailableAt))
	require.Zero(t, window.Items[0].TerminalAvailableAt.Nanosecond()%int(time.Microsecond))
	require.Zero(t, snapshot.Terminal.AvailableAt.Nanosecond()%int(time.Microsecond))
}

func TestRetentionUsesPublicationOrderNotTerminalTime(t *testing.T) {
	pool, service, orgID, projectID := setupCompletedFeedIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()

	olderPublication := completedFeedTerminalRecord("retention-old-publication", orgID, projectID, now)
	newerPublication := completedFeedTerminalRecord("retention-new-publication", orgID, projectID, now.Add(-30*24*time.Hour))
	materializeCompletedFeedRecord(t, ctx, pool, service, olderPublication)
	materializeCompletedFeedRecord(t, ctx, pool, service, newerPublication)

	_, err := pool.Exec(ctx,
		`UPDATE completed_execution_feed_items
		 SET created_at = CASE request_id
		     WHEN $1 THEN $3::timestamptz
		     WHEN $2 THEN $4::timestamptz
		 END
		 WHERE org_id = $5 AND project_id = $6 AND request_id IN ($1, $2)`,
		olderPublication.RequestID,
		newerPublication.RequestID,
		now.Add(-8*24*time.Hour),
		now.Add(-24*time.Hour),
		orgID,
		projectID,
	)
	require.NoError(t, err)

	var oldID, retainedID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM completed_execution_feed_items WHERE request_id = $1`,
		olderPublication.RequestID,
	).Scan(&oldID))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM completed_execution_feed_items WHERE request_id = $1`,
		newerPublication.RequestID,
	).Scan(&retainedID))

	deleted, err := deleteExpiredBatch(ctx, pool, now.Add(-RetentionWindow), retentionBatchSize)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var floor int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT retained_after_id
		 FROM completed_execution_feed_states
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&floor))
	require.Equal(t, oldID, floor)
	require.Greater(t, retainedID, floor)

	var remainingRequest string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT request_id
		 FROM completed_execution_feed_items
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&remainingRequest))
	require.Equal(t, newerPublication.RequestID, remainingRequest)
}

func TestRetentionProjectFencePreventsFloorJumpOnConcurrentRollback(t *testing.T) {
	pool, service, orgID, projectID := setupCompletedFeedIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()

	for index := 0; index < 3; index++ {
		record := completedFeedTerminalRecord(
			fmt.Sprintf("retention-concurrent-%d", index),
			orgID,
			projectID,
			now.Add(time.Duration(index)*time.Minute),
		)
		materializeCompletedFeedRecord(t, ctx, pool, service, record)
	}
	_, err := pool.Exec(ctx,
		`UPDATE completed_execution_feed_items
		 SET created_at = $1
		 WHERE org_id = $2 AND project_id = $3`,
		now.Add(-8*24*time.Hour), orgID, projectID,
	)
	require.NoError(t, err)

	var firstID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT MIN(id)
		 FROM completed_execution_feed_items
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&firstID))

	heldRetention, err := pool.Begin(ctx)
	require.NoError(t, err)
	var initialFloor int64
	require.NoError(t, heldRetention.QueryRow(ctx,
		`SELECT retained_after_id
		 FROM completed_execution_feed_states
		 WHERE org_id = $1 AND project_id = $2
		 FOR UPDATE`,
		orgID, projectID,
	).Scan(&initialFloor))
	require.Zero(t, initialFloor)
	_, err = heldRetention.Exec(ctx,
		`DELETE FROM completed_execution_feed_items WHERE id = $1`,
		firstID,
	)
	require.NoError(t, err)

	deletedWhileLocked, err := deleteExpiredBatch(
		ctx,
		pool,
		now.Add(-RetentionWindow),
		retentionBatchSize,
	)
	require.NoError(t, err)
	require.Zero(t, deletedWhileLocked)
	require.NoError(t, heldRetention.Rollback(ctx))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM completed_execution_feed_items
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&count))
	require.Equal(t, 3, count)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT retained_after_id
		 FROM completed_execution_feed_states
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&initialFloor))
	require.Zero(t, initialFloor)

	firstBatch, err := deleteExpiredBatch(ctx, pool, now.Add(-RetentionWindow), 2)
	require.NoError(t, err)
	require.EqualValues(t, 2, firstBatch)

	var floor, minimumRetained int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT retained_after_id
		 FROM completed_execution_feed_states
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&floor))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT MIN(id)
		 FROM completed_execution_feed_items
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&minimumRetained))
	require.Greater(t, minimumRetained, floor)

	finalBatch, err := deleteExpiredBatch(ctx, pool, now.Add(-RetentionWindow), 2)
	require.NoError(t, err)
	require.EqualValues(t, 1, finalBatch)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT retained_after_id
		 FROM completed_execution_feed_states
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&floor))
	require.Equal(t, minimumRetained, floor)
}

func TestBillingTerminalMaterializationFailureIsRetriedWithoutPartialFeedState(t *testing.T) {
	pool, service, orgID, projectID := setupCompletedFeedIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	materializer := &failOnceTerminalMaterializer{delegate: service}
	billingService := billing.NewBilling(pool, nil)
	billingService.SetTerminalMaterializer(materializer)
	options := outbox.DefaultOptions()
	options.WorkerCount = 1
	options.BatchSize = 2
	outboxService, err := outbox.New(pool, options)
	require.NoError(t, err)
	billingService.SetOutbox(outboxService)
	require.NoError(t, outboxService.RegisterHandler(
		outbox.EventBillingSettled,
		billingService.ProcessUsageJournalEvent,
	))

	occurredAt := time.Now().UTC()
	event, err := outbox.NewEvent(
		outbox.EventBillingSettled,
		"billing:settled:completed-feed-retry",
		outbox.PriorityBilling,
		orgID,
		"completed-feed-retry",
		map[string]any{
			"event_type":              TerminalSettled,
			"request_id":              "completed-feed-retry",
			"org_id":                  orgID,
			"project_id":              projectID,
			"model":                   "integration-model",
			"prompt_tokens":           10,
			"uncached_prompt_tokens":  10,
			"cache_read_tokens":       0,
			"cache_creation_tokens":   0,
			"completion_tokens":       2,
			"usage_source":            metering.UsageSourceUpstream,
			"usage_authority":         metering.UsageAuthorityProviderReported,
			"usage_purpose":           metering.UsagePurposeCustomerCharge,
			"usage_finality":          metering.UsageFinalityFinal,
			"usage_contract_version":  metering.UsageContractVersion,
			"usage_estimate_version":  "",
			"cache_usage_schema":      "cache.v1",
			"cache_usage_status":      "not_used",
			"money_schema":            outbox.MoneyVersion,
			"currency":                "CNY",
			"amount_scale":            money.MicrosPerCNY,
			"calculated_cost_micros":  "25",
			"charged_cost_micros":     "25",
			"pricing_version":         "pricing.v1",
			"terminal_transaction_id": 0,
			"occurred_at":             occurredAt,
		},
		occurredAt,
	)
	require.NoError(t, err)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, created, err := outbox.EnqueueTx(ctx, tx, event)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, tx.Commit(ctx))

	processed, err := outboxService.DrainOnce(ctx)
	require.Equal(t, 1, processed)
	require.ErrorContains(t, err, "synthetic feed materialization failure")
	assertCompletedFeedRetryCounts(t, ctx, pool, 1, 0, 0)

	_, err = pool.Exec(ctx,
		`UPDATE internal_outbox_events
		 SET available_at = NOW()
		 WHERE event_key = $1`,
		event.Key,
	)
	require.NoError(t, err)
	processed, err = outboxService.DrainOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	assertCompletedFeedRetryCounts(t, ctx, pool, 0, 1, 1)
	require.Equal(t, 2, materializer.calls)
}

type failOnceTerminalMaterializer struct {
	delegate *Service
	calls    int
}

func (materializer *failOnceTerminalMaterializer) MaterializeTerminalTx(
	ctx context.Context,
	tx pgx.Tx,
	record billing.TerminalRecord,
) error {
	materializer.calls++
	if materializer.calls == 1 {
		return errors.New("synthetic feed materialization failure")
	}
	return materializer.delegate.MaterializeTerminalTx(ctx, tx, record)
}

func assertCompletedFeedRetryCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	outboxCount, journalCount, feedCount int,
) {
	t.Helper()
	var actualOutbox, actualJournal, actualFeed int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM internal_outbox_events`,
	).Scan(&actualOutbox))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM usage_finalization_journal`,
	).Scan(&actualJournal))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM completed_execution_feed_items`,
	).Scan(&actualFeed))
	require.Equal(t, outboxCount, actualOutbox)
	require.Equal(t, journalCount, actualJournal)
	require.Equal(t, feedCount, actualFeed)
}

func setupCompletedFeedIntegration(t *testing.T) (*pgxpool.Pool, *Service, int64, int64) {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var orgID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ($1, 'active', 'standard')
		 RETURNING id`,
		fmt.Sprintf("completed-feed-%d", time.Now().UnixNano()),
	).Scan(&orgID))
	var projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (
		    org_id, name, slug, description, status, budget_period
		 ) VALUES ($1, 'Completed Feed', $2, '', 'active', 'none')
		 RETURNING id`,
		orgID, fmt.Sprintf("completed-feed-%d", time.Now().UnixNano()),
	).Scan(&projectID))

	service, err := NewService(pool, "completed-feed-integration-secret", "integration-unirouter", true)
	require.NoError(t, err)
	return pool, service, orgID, projectID
}

func completedFeedTerminalRecord(name string, orgID, projectID int64, terminalAt time.Time) billing.TerminalRecord {
	return billing.TerminalRecord{
		EventKey:             "billing:settled:" + name,
		RequestID:            name,
		OrgID:                orgID,
		ProjectID:            projectID,
		Status:               TerminalSettled,
		Model:                "integration-model",
		PromptTokens:         10,
		UncachedPromptTokens: 10,
		CompletionTokens:     2,
		UsageSource:          "provider",
		UsageAuthority:       "authoritative",
		UsagePurpose:         "billing",
		UsageFinality:        "final",
		UsageContractVersion: "usage.v1",
		CacheUsageSchema:     "cache.v1",
		CacheUsageStatus:     "not_used",
		CalculatedCostMicros: 25,
		ChargedCostMicros:    25,
		PricingVersion:       "pricing.v1",
		OccurredAt:           terminalAt.UTC(),
	}
}

func materializeCompletedFeedRecord(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *Service,
	record billing.TerminalRecord,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, service.MaterializeTerminalTx(ctx, tx, record))
	require.NoError(t, tx.Commit(ctx))
}
