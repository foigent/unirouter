//go:build integration

package runtimechange_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDeploymentLifecycleReceiptSameKeyWinnerRecoveryIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	otherPool, err := pgxpool.New(ctx, pool.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(otherPool.Close)
	firstManager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	secondManager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: otherPool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	fixture, _ := seedCanonicalStable(t, pool, firstManager, "m76-same-key")
	candidateFixture := createAdditionalDeploymentFixture(
		t, pool, fixture, "m76-same-key-target", "active", 100, 100,
	)
	candidate := prepareSealedChild(t, pool, firstManager, fixture, candidateFixture, "rcr_m76_same_key_target")
	firstCommand := deploymentLifecycleReceiptCommand(
		fixture, runtimechange.DeploymentLifecycleStartCanary, candidate,
		1, 500, 'c', 'd', "op_m76_same_key_first",
	)
	secondCommand := firstCommand
	secondCommand.ServerOperationID = "op_m76_same_key_second"
	secondCommand.Audit.RequestID = secondCommand.ServerOperationID

	type applyResult struct {
		receipt runtimechange.DeploymentLifecycleReceipt
		err     error
	}
	start := make(chan struct{})
	results := make(chan applyResult, 2)
	apply := func(manager *runtimechange.Manager, command runtimechange.DeploymentLifecycleCommand) {
		<-start
		receipt, applyErr := manager.ApplyDeploymentLifecycle(ctx, command)
		results <- applyResult{receipt: receipt, err: applyErr}
	}
	go apply(firstManager, firstCommand)
	go apply(secondManager, secondCommand)
	close(start)
	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, first.receipt.ReceiptID, second.receipt.ReceiptID)
	require.Equal(t, first.receipt.Slot, second.receipt.Slot)
	require.Equal(t, first.receipt.Releases, second.receipt.Releases)
	require.NotEqual(t, first.receipt.IdempotencyReplay, second.receipt.IdempotencyReplay,
		"exactly one concurrent caller must observe the frozen winner as a replay")

	var receiptCount, auditCount, eventCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND operation_scope='model_deployment.lifecycle.start_canary.v1'
   AND idempotency_key_hash=$2`, fixture.OrgID, firstCommand.IdempotencyKeyHash).Scan(&receiptCount))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM audit_logs
 WHERE org_id=$1 AND action='model_deployment.lifecycle.start_canary'
   AND detail->>'receipt_id'=$2::bigint::text`, fixture.OrgID, first.receipt.ReceiptID).Scan(&auditCount))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM runtime_change_events
 WHERE detail->>'lifecycle_receipt_id'=$1::bigint::text`, first.receipt.ReceiptID).Scan(&eventCount))
	require.Equal(t, 1, receiptCount)
	require.Equal(t, 1, auditCount)
	require.Equal(t, 1, eventCount)
}

func TestDeploymentLifecycleReceiptGenericCommitAmbiguityRecoveryIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixtureManager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	fixture, _ := seedCanonicalStable(t, pool, fixtureManager, "m76-ambiguous-commit")
	candidateFixture := createAdditionalDeploymentFixture(
		t, pool, fixture, "m76-ambiguous-target", "active", 100, 100,
	)
	candidate := prepareSealedChild(t, pool, fixtureManager, fixture, candidateFixture, "rcr_m76_ambiguous_target")

	ambiguousPool := &commitAmbiguityPool{Pool: pool, failNextCommit: true}
	ambiguousManager, err := runtimechange.NewManager(
		ambiguousPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	command := deploymentLifecycleReceiptCommand(
		fixture, runtimechange.DeploymentLifecycleStartCanary, candidate,
		1, 500, 'e', 'f', "op_m76_ambiguous_commit",
	)
	receipt, err := ambiguousManager.ApplyDeploymentLifecycle(ctx, command)
	require.NoError(t, err, "a committed receipt must recover from a lost commit acknowledgement")
	require.False(t, receipt.IdempotencyReplay,
		"the same server operation recovering its own commit remains the original response")

	sameOperationReplay, err := fixtureManager.ApplyDeploymentLifecycle(ctx, command)
	require.NoError(t, err)
	require.False(t, sameOperationReplay.IdempotencyReplay)
	require.Equal(t, receipt.ReceiptID, sameOperationReplay.ReceiptID)

	laterReplayCommand := command
	laterReplayCommand.ServerOperationID += "_later"
	laterReplayCommand.Audit.RequestID = laterReplayCommand.ServerOperationID
	replayed, err := fixtureManager.ApplyDeploymentLifecycle(ctx, laterReplayCommand)
	require.NoError(t, err)
	require.True(t, replayed.IdempotencyReplay)
	require.Equal(t, receipt.ReceiptID, replayed.ReceiptID)
	require.Equal(t, receipt.Slot, replayed.Slot)
	require.Equal(t, receipt.Releases, replayed.Releases)
	require.Equal(t, receipt.Qualifications, replayed.Qualifications)

	var receiptCount, auditCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND operation_scope='model_deployment.lifecycle.start_canary.v1'
   AND idempotency_key_hash=$2`, fixture.OrgID, command.IdempotencyKeyHash).Scan(&receiptCount))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM audit_logs
 WHERE org_id=$1 AND action='model_deployment.lifecycle.start_canary'
   AND detail->>'receipt_id'=$2::bigint::text`, fixture.OrgID, receipt.ReceiptID).Scan(&auditCount))
	require.Equal(t, 1, receiptCount)
	require.Equal(t, 1, auditCount)
}

func TestDeploymentLifecycleReceiptLegacyCutoverSameKeyWinnerRecoveryIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	fixture := createCanonicalDeploymentFixture(t, pool, "m78-legacy-same-key", "active", 100, 100)
	basePlanner, err := modelrouting.NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	baseManager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(), basePlanner,
	)
	require.NoError(t, err)
	insertLegacyStableRelease(t, pool, fixture)
	_, err = baseManager.PrepareLegacyCompatibilitySeal(
		ctx, legacyReleaseRef(fixture), 1, fixture.OrgID, fixture.PublicModelID,
	)
	require.NoError(t, err)
	target := authorLegacyCutoverRelease(
		t, pool, baseManager, fixture, "concurrent exact-key legacy cutover target",
	)

	const claimBarrierKey int64 = 780076002
	_, err = pool.Exec(ctx, `
CREATE FUNCTION m78_legacy_lifecycle_claim_barrier()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(780076002);
    RETURN NEW;
END;
$$;
CREATE TRIGGER m78_legacy_lifecycle_claim_barrier
BEFORE INSERT ON model_deployment_lifecycle_receipts
FOR EACH ROW EXECUTE FUNCTION m78_legacy_lifecycle_claim_barrier()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
DROP TRIGGER IF EXISTS m78_legacy_lifecycle_claim_barrier
    ON model_deployment_lifecycle_receipts;
DROP FUNCTION IF EXISTS m78_legacy_lifecycle_claim_barrier()`)
	})
	barrierOwner, err := pool.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(barrierOwner.Release)
	_, err = barrierOwner.Exec(ctx, `SELECT pg_advisory_lock($1)`, claimBarrierKey)
	require.NoError(t, err)
	barrierHeld := true
	t.Cleanup(func() {
		if barrierHeld {
			_, _ = barrierOwner.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, claimBarrierKey)
		}
	})
	secondPool, err := pgxpool.New(ctx, pool.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(secondPool.Close)
	firstPlanner, err := modelrouting.NewPlanner(pool, routingpolicy.Default())
	require.NoError(t, err)
	firstManager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(), firstPlanner,
	)
	require.NoError(t, err)
	secondPlanner, err := modelrouting.NewPlanner(secondPool, routingpolicy.Default())
	require.NoError(t, err)
	secondManager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		secondPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(), secondPlanner,
	)
	require.NoError(t, err)
	firstCommand := newLegacyLifecycleCommand(t, pool, fixture, fixture.UserID,
		runtimechange.DeploymentLifecycleMutation{
			Action:                 runtimechange.DeploymentLifecycleCutoverLegacy,
			ReleaseRef:             target.ReleaseRef,
			ExpectedReleaseVersion: target.Version,
			ExpectedSlotVersion:    1,
			Reason:                 "same key cutover claims one durable winner",
		},
	)
	secondCommand := firstCommand
	secondCommand.ServerOperationID = firstCommand.ServerOperationID + "-second"
	secondCommand.Audit.RequestID = secondCommand.ServerOperationID

	type result struct {
		receipt runtimechange.DeploymentLifecycleReceipt
		err     error
	}
	results := make(chan result, 2)
	for _, invocation := range []struct {
		manager *runtimechange.Manager
		command runtimechange.DeploymentLifecycleCommand
	}{
		{firstManager, firstCommand},
		{secondManager, secondCommand},
	} {
		invocation := invocation
		go func() {
			receipt, applyErr := invocation.manager.ApplyDeploymentLifecycle(ctx, invocation.command)
			results <- result{receipt: receipt, err: applyErr}
		}()
	}
	require.Eventually(t, func() bool {
		var waiters int
		err := pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM pg_stat_activity
 WHERE datname=current_database()
   AND wait_event_type='Lock' AND wait_event='advisory'
   AND query ILIKE '%INSERT INTO model_deployment_lifecycle_receipts%'`).Scan(&waiters)
		return err == nil && waiters == 2
	}, 5*time.Second, 10*time.Millisecond)
	_, err = barrierOwner.Exec(ctx, `SELECT pg_advisory_unlock($1)`, claimBarrierKey)
	require.NoError(t, err)
	barrierHeld = false
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, first.receipt.ReceiptID, second.receipt.ReceiptID)
	require.Equal(t, first.receipt.Slot, second.receipt.Slot)
	require.Equal(t, first.receipt.Releases, second.receipt.Releases)
	require.NotEqual(t, first.receipt.IdempotencyReplay, second.receipt.IdempotencyReplay)

	var receiptCount, releaseEvidenceCount, eventCount, auditCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM model_deployment_lifecycle_receipts
    WHERE customer_org_id=$1
      AND operation_scope='model_deployment.lifecycle.cutover_legacy.v1'
      AND idempotency_key_hash=$2),
  (SELECT COUNT(*) FROM model_deployment_lifecycle_receipt_releases
    WHERE receipt_id=$3),
  (SELECT COUNT(*) FROM runtime_change_events
    WHERE detail->>'lifecycle_receipt_id'=$3::bigint::text),
  (SELECT COUNT(*) FROM audit_logs
    WHERE org_id=$1 AND action='model_deployment.lifecycle.cutover_legacy'
      AND detail->>'receipt_id'=$3::bigint::text)`,
		fixture.OrgID, firstCommand.IdempotencyKeyHash, first.receipt.ReceiptID,
	).Scan(&receiptCount, &releaseEvidenceCount, &eventCount, &auditCount))
	require.Equal(t, 1, receiptCount)
	require.Equal(t, 2, releaseEvidenceCount)
	require.Equal(t, 2, eventCount)
	require.Equal(t, 1, auditCount)
}

type commitAmbiguityPool struct {
	*pgxpool.Pool
	mu             sync.Mutex
	failNextCommit bool
}

func (p *commitAmbiguityPool) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := p.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &commitAmbiguityTx{Tx: tx, pool: p}, nil
}

func (p *commitAmbiguityPool) consumeCommitAmbiguity() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.failNextCommit {
		return false
	}
	p.failNextCommit = false
	return true
}

type commitAmbiguityTx struct {
	pgx.Tx
	pool *commitAmbiguityPool
}

func (tx *commitAmbiguityTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	if tx.pool.consumeCommitAmbiguity() {
		return errors.New("injected lost commit acknowledgement")
	}
	return nil
}
