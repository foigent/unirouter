//go:build integration

package runtimechange_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestAtomicModelReleaseAuthoringReceiptAndBaselineIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	recordedPool := &recordingBeginTxPool{Pool: pool}
	manager, err := runtimechange.NewManager(
		recordedPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()
	fixture := createCanonicalDeploymentFixture(t, pool, "m78-author", "active", 73, 29)
	makeSoleInitialCustomer(t, pool, fixture.OrgID)
	_, err = pool.Exec(ctx, `
UPDATE users SET organization_role='org_admin',role='org_admin' WHERE id=$1`, fixture.UserID)
	require.NoError(t, err)
	initialSecond := createAdditionalDeploymentFixture(
		t, pool, fixture, "m78-author-second", "active", 61, 19,
	)

	initialCommand := deploymentReleaseAuthoringCommand(
		fixture, runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial},
		[]runtimechange.DeploymentReleaseSelection{
			{DeploymentID: fixture.DeploymentID, OperationID: fixture.OperationID},
			{DeploymentID: initialSecond.DeploymentID, OperationID: initialSecond.OperationID},
		}, "a", "b", "op_m78_initial",
	)
	foreignOrgID := testutil.NewTestOrg(t, pool)
	var foreignOrgAdminID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'m78-foreign-org-admin','fixture','org_admin','org_admin','active')
RETURNING id`, foreignOrgID).Scan(&foreignOrgAdminID))
	foreignActor := initialCommand
	foreignActor.ActorUserID = foreignOrgAdminID
	foreignActor.ActorOrgID = foreignOrgID
	foreignActor.IdempotencyKeyHash = strings.Repeat("c", 64)
	foreignActor.RequestFingerprint = "hmac-sha256:" + strings.Repeat("d", 64)
	foreignActor.ServerOperationID = "op_m78_foreign_org_admin"
	_, err = manager.AuthorSealedDeploymentRelease(ctx, foreignActor)
	require.ErrorIs(t, err, runtimechange.ErrConflict)

	initial, err := manager.AuthorSealedDeploymentRelease(ctx, initialCommand)
	require.NoError(t, err)
	require.Equal(t, runtimechange.DeploymentReleaseAuthoringReceiptContractVersion, initial.ContractVersion)
	require.Equal(t, 201, initial.OriginalHTTPStatus)
	require.Equal(t, "validated", initial.Release.Stage)
	require.Equal(t, int64(2), initial.Release.Version)
	require.Nil(t, initial.Release.ParentStableReleaseID)
	require.Len(t, initial.Release.Candidate.DeploymentRoutes, 2)
	route := initial.Release.Candidate.DeploymentRoutes[0]
	require.Equal(t, routingpolicy.AdmissionAllow, route.Admission)
	require.Nil(t, route.Priority)
	require.Nil(t, route.Weight)
	require.Equal(t, fixture.ConnectionRevision, route.ExpectedConnectionRevision)
	require.Equal(t, fixture.DeploymentRevision, route.ExpectedDeploymentRevision)
	require.Equal(t, fixture.OperationRevision, route.ExpectedOperationRevision)
	require.Equal(t, initialSecond.DeploymentID, initial.Release.Candidate.DeploymentRoutes[1].DeploymentID)
	recordedPool.assertLatestRepeatableRead(t)

	var receiptStatus string
	var releaseReceiptID int64
	var eventCount, slotCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT receipt.status,release.authoring_receipt_id,
       (SELECT COUNT(*) FROM runtime_change_events event
         WHERE event.release_id=release.id
           AND event.detail->>'authoring_receipt_id'=receipt.receipt_id::text),
       (SELECT COUNT(*) FROM runtime_change_slots slot
         WHERE slot.model_name=release.model_name OR slot.public_model_id=release.public_model_id)
  FROM model_deployment_release_authoring_receipts receipt
  JOIN runtime_change_releases release ON release.id=receipt.release_id
 WHERE receipt.receipt_id=$1`, initial.ReceiptID).Scan(
		&receiptStatus, &releaseReceiptID, &eventCount, &slotCount,
	))
	require.Equal(t, "completed", receiptStatus)
	require.Equal(t, initial.ReceiptID, releaseReceiptID)
	require.Equal(t, 2, eventCount)
	require.Zero(t, slotCount, "authoring must not activate or start a canary")

	sameOperation, err := manager.AuthorSealedDeploymentRelease(ctx, initialCommand)
	require.NoError(t, err)
	require.Equal(t, initial.ReceiptID, sameOperation.ReceiptID)
	require.False(t, sameOperation.IdempotencyReplay, "same server operation is commit-ACK recovery")
	require.Equal(t, initial.CreatedAt, sameOperation.CreatedAt)
	require.Equal(t, initial.CompletedAt, sameOperation.CompletedAt)
	require.Equal(t, time.UTC, sameOperation.CreatedAt.Location())
	require.Equal(t, time.UTC, sameOperation.CompletedAt.Location())
	laterCommand := initialCommand
	laterCommand.ServerOperationID = "op_m78_initial_later"
	laterReplay, err := manager.AuthorSealedDeploymentRelease(ctx, laterCommand)
	require.NoError(t, err)
	require.Equal(t, initial.ReceiptID, laterReplay.ReceiptID)
	require.True(t, laterReplay.IdempotencyReplay)
	require.Equal(t, initial.CreatedAt, laterReplay.CreatedAt)
	require.Equal(t, initial.CompletedAt, laterReplay.CompletedAt)
	require.Equal(t, time.UTC, laterReplay.CreatedAt.Location())
	require.Equal(t, time.UTC, laterReplay.CompletedAt.Location())
	conflict := laterCommand
	conflict.RequestFingerprint = "hmac-sha256:" + strings.Repeat("c", 64)
	_, err = manager.AuthorSealedDeploymentRelease(ctx, conflict)
	require.ErrorIs(t, err, runtimechange.ErrDeploymentReleaseAuthoringIdempotencyConflict)

	activated, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		fixture, runtimechange.DeploymentLifecycleActivateInitial, initial.Release,
		0, 0, 'd', 'e', "op_m78_activate_initial",
	))
	require.NoError(t, err, "M76 lifecycle provenance must remain legal after M78")
	require.Equal(t, int64(1), activated.Slot.AfterVersion)
	stable := mustDeploymentRelease(t, manager, initial.Release.ReleaseRef)
	childFixture := createAdditionalDeploymentFixture(
		t, pool, fixture, "m78-child", "active", 11, 7,
	)
	childSecond := createAdditionalDeploymentFixture(
		t, pool, fixture, "m78-child-second", "active", 13, 5,
	)
	platformOrgID := testutil.NewTestOrg(t, pool)
	var platformAdminID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'m78-cross-org-platform','fixture','platform_admin','platform_admin','active')
RETURNING id`, platformOrgID).Scan(&platformAdminID))
	stableCommand := deploymentReleaseAuthoringCommand(
		fixture, runtimechange.DeploymentReleaseBaseline{
			Kind:                         runtimechange.DeploymentReleaseBaselineStable,
			ExpectedSlotVersion:          activated.Slot.AfterVersion,
			StableReleaseRef:             stable.ReleaseRef,
			ExpectedStableReleaseVersion: stable.Version,
		},
		[]runtimechange.DeploymentReleaseSelection{
			{DeploymentID: childFixture.DeploymentID, OperationID: childFixture.OperationID},
			{DeploymentID: childSecond.DeploymentID, OperationID: childSecond.OperationID},
		}, "f", "1", "op_m78_stable_child",
	)
	stableCommand.ActorUserID = platformAdminID
	stableCommand.ActorOrgID = platformOrgID
	child, err := manager.AuthorSealedDeploymentRelease(ctx, stableCommand)
	require.NoError(t, err)
	require.NotNil(t, child.Release.ParentStableReleaseID)
	require.Equal(t, stable.ID, *child.Release.ParentStableReleaseID)
	require.Equal(t, stable.CandidateHash, child.Release.BaselineHash)
	require.Len(t, child.Release.Candidate.DeploymentRoutes, 2)
	require.Equal(t, childSecond.DeploymentID, child.Release.Candidate.DeploymentRoutes[1].DeploymentID)

	stale := stableCommand
	stale.IdempotencyKeyHash = strings.Repeat("2", 64)
	stale.RequestFingerprint = "hmac-sha256:" + strings.Repeat("3", 64)
	stale.ServerOperationID = "op_m78_stale_baseline"
	stale.Mutation.Baseline.ExpectedStableReleaseVersion++
	_, err = manager.AuthorSealedDeploymentRelease(ctx, stale)
	require.ErrorIs(t, err, runtimechange.ErrConflict)
	assertConcurrentDeploymentReleaseAuthoringReplay(t, pool)
	assertDeploymentReleaseAuthoringCommitAmbiguity(t, pool)
	assertDeploymentReleaseAuthoringDriftRollsBack(t, pool)

	_, err = pool.Exec(ctx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail)
VALUES ($1,3,'advance','validated','shadow',$2,2,3,'{}'::jsonb)`,
		child.Release.ID, fixture.UserID)
	require.ErrorContains(t, err, "requires exactly one receipt provenance")
}

func assertDeploymentReleaseAuthoringCommitAmbiguity(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	fixture := createCanonicalDeploymentFixture(t, pool, "m78-author-commit-ambiguity", "active", 41, 23)
	makeSoleInitialCustomer(t, pool, fixture.OrgID)
	ambiguousManager, err := runtimechange.NewManager(
		&commitAmbiguityPool{Pool: pool, failNextCommit: true},
		unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	command := deploymentReleaseAuthoringCommand(
		fixture, runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial},
		[]runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.DeploymentID, OperationID: fixture.OperationID,
		}}, "6", "7", "op_m78_author_commit_ambiguity",
	)
	receipt, err := ambiguousManager.AuthorSealedDeploymentRelease(t.Context(), command)
	require.NoError(t, err, "the authoring winner must recover a lost commit acknowledgement")
	require.False(t, receipt.IdempotencyReplay)

	laterManager, err := runtimechange.NewManager(
		pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	command.ServerOperationID = "op_m78_author_commit_ambiguity_later"
	later, err := laterManager.AuthorSealedDeploymentRelease(t.Context(), command)
	require.NoError(t, err)
	require.True(t, later.IdempotencyReplay)
	require.Equal(t, receipt.ReceiptID, later.ReceiptID)
}

func assertDeploymentReleaseAuthoringDriftRollsBack(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	fixture := createCanonicalDeploymentFixture(t, pool, "m78-author-drift", "active", 43, 17)
	makeSoleInitialCustomer(t, pool, fixture.OrgID)
	manager, err := runtimechange.NewManager(
		pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	command := deploymentReleaseAuthoringCommand(
		fixture, runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial},
		[]runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.DeploymentID, OperationID: fixture.OperationID,
		}}, "8", "9", "op_m78_author_route_drift",
	)
	_, err = pool.Exec(t.Context(), `
UPDATE provider_connections
   SET base_url='https://m78-route-drift.invalid',config_revision=config_revision+1
 WHERE id=$1`, fixture.ConnectionID)
	require.NoError(t, err)
	_, err = manager.AuthorSealedDeploymentRelease(t.Context(), command)
	require.ErrorIs(t, err, runtimechange.ErrConflict)

	var receiptCount, releaseCount, eventCount, auditCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT
  (SELECT COUNT(*) FROM model_deployment_release_authoring_receipts
    WHERE customer_org_id=$1 AND actor_user_id=$2 AND idempotency_key_hash=$3),
  (SELECT COUNT(*) FROM runtime_change_releases WHERE model_name=$4),
  (SELECT COUNT(*) FROM runtime_change_events event
    JOIN runtime_change_releases release ON release.id=event.release_id
   WHERE release.model_name=$4),
  (SELECT COUNT(*) FROM audit_logs
    WHERE org_id=$1 AND action='model_deployment.release_authoring.create')`,
		fixture.OrgID, fixture.UserID, command.IdempotencyKeyHash, fixture.ModelName,
	).Scan(&receiptCount, &releaseCount, &eventCount, &auditCount))
	require.Zero(t, receiptCount)
	require.Zero(t, releaseCount)
	require.Zero(t, eventCount)
	require.Zero(t, auditCount)
}

func assertConcurrentDeploymentReleaseAuthoringReplay(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	const claimBarrierKey int64 = 780076001
	fixture := createCanonicalDeploymentFixture(t, pool, "m78-concurrent-author", "active", 31, 17)
	makeSoleInitialCustomer(t, pool, fixture.OrgID)
	_, err := pool.Exec(t.Context(), `
CREATE FUNCTION m78_authoring_claim_barrier()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(780076001);
    RETURN NEW;
END;
$$;
CREATE TRIGGER m78_authoring_claim_barrier
BEFORE INSERT ON model_deployment_release_authoring_receipts
FOR EACH ROW EXECUTE FUNCTION m78_authoring_claim_barrier()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
DROP TRIGGER IF EXISTS m78_authoring_claim_barrier
    ON model_deployment_release_authoring_receipts;
DROP FUNCTION IF EXISTS m78_authoring_claim_barrier()`)
	})
	barrierOwner, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(barrierOwner.Release)
	_, err = barrierOwner.Exec(t.Context(), `SELECT pg_advisory_lock($1)`, claimBarrierKey)
	require.NoError(t, err)
	barrierHeld := true
	t.Cleanup(func() {
		if barrierHeld {
			_, _ = barrierOwner.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, claimBarrierKey)
		}
	})
	secondPool, err := pgxpool.New(t.Context(), pool.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(secondPool.Close)
	firstManager, err := runtimechange.NewManager(
		pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	secondManager, err := runtimechange.NewManager(
		secondPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	firstCommand := deploymentReleaseAuthoringCommand(
		fixture, runtimechange.DeploymentReleaseBaseline{Kind: runtimechange.DeploymentReleaseBaselineInitial},
		[]runtimechange.DeploymentReleaseSelection{{
			DeploymentID: fixture.DeploymentID, OperationID: fixture.OperationID,
		}}, "4", "5", "op_m78_concurrent_first",
	)
	secondCommand := firstCommand
	secondCommand.ServerOperationID = "op_m78_concurrent_second"
	secondCommand.Audit.RequestID = secondCommand.ServerOperationID

	type result struct {
		receipt runtimechange.DeploymentReleaseAuthoringReceipt
		err     error
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	results := make(chan result, 2)
	for _, invocation := range []struct {
		manager *runtimechange.Manager
		command runtimechange.DeploymentReleaseAuthoringCommand
	}{
		{manager: firstManager, command: firstCommand},
		{manager: secondManager, command: secondCommand},
	} {
		invocation := invocation
		go func() {
			receipt, authorErr := invocation.manager.AuthorSealedDeploymentRelease(ctx, invocation.command)
			results <- result{receipt: receipt, err: authorErr}
		}()
	}
	require.Eventually(t, func() bool {
		var waiters int
		err := pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM pg_stat_activity
 WHERE datname=current_database()
   AND wait_event_type='Lock' AND wait_event='advisory'
   AND query ILIKE '%INSERT INTO model_deployment_release_authoring_receipts%'`).Scan(&waiters)
		return err == nil && waiters == 2
	}, 5*time.Second, 10*time.Millisecond,
		"both repeatable-read commands must reach the receipt claim trigger before either winner commits")
	_, err = barrierOwner.Exec(ctx, `SELECT pg_advisory_unlock($1)`, claimBarrierKey)
	require.NoError(t, err)
	barrierHeld = false
	var firstResult, secondResult result
	select {
	case firstResult = <-results:
	case <-ctx.Done():
		t.Fatal("first authoring result did not finish after releasing the claim barrier")
	}
	select {
	case secondResult = <-results:
	case <-ctx.Done():
		t.Fatal("second authoring result did not finish after releasing the claim barrier")
	}
	require.NoError(t, firstResult.err)
	require.NoError(t, secondResult.err)
	require.Equal(t, firstResult.receipt.ReceiptID, secondResult.receipt.ReceiptID)
	require.Equal(t, firstResult.receipt.Release.ID, secondResult.receipt.Release.ID)
	require.Equal(t, firstResult.receipt.ServerOperationID, secondResult.receipt.ServerOperationID)
	require.Equal(t, 1, boolCount(firstResult.receipt.IdempotencyReplay, secondResult.receipt.IdempotencyReplay),
		"one concurrent operation owns the commit and the other is an exact later-operation replay")

	var receiptCount, releaseCount, eventCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
SELECT
  (SELECT COUNT(*) FROM model_deployment_release_authoring_receipts receipt
    WHERE receipt.customer_org_id=$1 AND receipt.actor_user_id=$2
      AND receipt.operation_scope='model_deployment.release_authoring.create.v1'
      AND receipt.idempotency_key_hash=$3),
  (SELECT COUNT(*) FROM runtime_change_releases release
    JOIN model_deployment_release_authoring_receipts receipt
      ON receipt.receipt_id=release.authoring_receipt_id
   WHERE receipt.customer_org_id=$1 AND receipt.actor_user_id=$2
     AND receipt.operation_scope='model_deployment.release_authoring.create.v1'
     AND receipt.idempotency_key_hash=$3),
  (SELECT COUNT(*) FROM runtime_change_events event
    JOIN runtime_change_releases release ON release.id=event.release_id
    JOIN model_deployment_release_authoring_receipts receipt
      ON receipt.receipt_id=release.authoring_receipt_id
   WHERE receipt.customer_org_id=$1 AND receipt.actor_user_id=$2
     AND receipt.operation_scope='model_deployment.release_authoring.create.v1'
     AND receipt.idempotency_key_hash=$3)`, fixture.OrgID, fixture.UserID,
		firstCommand.IdempotencyKeyHash).Scan(&receiptCount, &releaseCount, &eventCount))
	require.Equal(t, 1, receiptCount)
	require.Equal(t, 1, releaseCount)
	require.Equal(t, 2, eventCount)
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func deploymentReleaseAuthoringCommand(
	fixture canonicalDeploymentFixture,
	baseline runtimechange.DeploymentReleaseBaseline,
	selections []runtimechange.DeploymentReleaseSelection,
	keyDigit, fingerprintDigit, operationID string,
) runtimechange.DeploymentReleaseAuthoringCommand {
	return runtimechange.DeploymentReleaseAuthoringCommand{
		Mutation: runtimechange.DeploymentReleaseAuthoringMutation{
			PublicModelID: fixture.PublicModelID, Baseline: baseline,
			Selections: selections, Reason: "M78 atomic sealed candidate integration",
		},
		CustomerOrgID: fixture.OrgID, ActorUserID: fixture.UserID, ActorOrgID: fixture.OrgID,
		IdempotencyKeyHash: strings.Repeat(keyDigit, 64),
		RequestFingerprint: "hmac-sha256:" + strings.Repeat(fingerprintDigit, 64),
		ServerOperationID:  operationID,
		Audit: runtimechange.DeploymentReleaseAuthoringAudit{
			RequestID: operationID, Source: "admin_console", IP: "127.0.0.1",
			UserAgent: "m78-integration",
		},
	}
}
