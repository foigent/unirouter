//go:build integration

package runtimechange_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDeploymentLifecycleReceiptExecutorIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	manager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()

	t.Run("exact start replay and canary contraction", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "m76-replay")
		candidateFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "m76-replay-child", "active", 100, 100,
		)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_m76_replay_child")
		command := deploymentLifecycleReceiptCommand(
			fixture, runtimechange.DeploymentLifecycleStartCanary, candidate,
			1, 750, 'a', 'b', "op_m76_start_replay",
		)

		started, err := manager.ApplyDeploymentLifecycle(ctx, command)
		require.NoError(t, err)
		require.False(t, started.IdempotencyReplay)
		require.Equal(t, runtimechange.DeploymentLifecycleReceiptContractVersion, started.ContractVersion)
		require.Equal(t, int64(1), started.Slot.BeforeVersion)
		require.Equal(t, int64(2), started.Slot.AfterVersion)
		require.Equal(t, candidate.ID, *started.Slot.AfterCanaryReleaseID)
		require.Len(t, started.Releases, 2)
		require.Len(t, started.Qualifications, 2)

		sameOperationReplay, err := manager.ApplyDeploymentLifecycle(ctx, command)
		require.NoError(t, err)
		require.False(t, sameOperationReplay.IdempotencyReplay,
			"the same server operation remains the original response")
		require.Equal(t, started.ReceiptID, sameOperationReplay.ReceiptID)

		laterReplayCommand := command
		laterReplayCommand.ServerOperationID += "_later"
		laterReplayCommand.Audit.RequestID = laterReplayCommand.ServerOperationID
		replayed, err := manager.ApplyDeploymentLifecycle(ctx, laterReplayCommand)
		require.NoError(t, err)
		require.True(t, replayed.IdempotencyReplay)
		require.Equal(t, started.ReceiptID, replayed.ReceiptID)
		require.Equal(t, started.Slot, replayed.Slot)
		require.Equal(t, started.Releases, replayed.Releases)
		require.Equal(t, started.Qualifications, replayed.Qualifications)

		conflicting := command
		conflicting.RequestFingerprint = "hmac-sha256:" + strings.Repeat("c", 64)
		_, err = manager.ApplyDeploymentLifecycle(ctx, conflicting)
		require.ErrorIs(t, err, runtimechange.ErrDeploymentLifecycleIdempotencyConflict)

		_, err = pool.Exec(ctx, `
UPDATE provider_connections
   SET admin_state='disabled'
 WHERE id=$1`, candidateFixture.ConnectionID)
		require.NoError(t, err, "cancel is a safety contraction and must survive target drift")
		cancelCommand := deploymentLifecycleReceiptCommand(
			fixture, runtimechange.DeploymentLifecycleCancelCanary,
			mustDeploymentRelease(t, manager, candidate.ReleaseRef),
			2, 0, 'd', 'e', "op_m76_cancel_canary",
		)
		cancelled, err := manager.ApplyDeploymentLifecycle(ctx, cancelCommand)
		require.NoError(t, err)
		require.Equal(t, stable.ID, *cancelled.Slot.AfterStableReleaseID)
		require.Nil(t, cancelled.Slot.AfterCanaryReleaseID)
		require.Equal(t, int64(3), cancelled.Slot.AfterVersion)

		_, err = pool.Exec(ctx, `
UPDATE model_registry SET model_name=$2 WHERE id=$1`, fixture.PublicModelID, fixture.ModelName+"-renamed")
		require.NoError(t, err, "direct SQL rename exercises frozen receipt readback")
		renameReplay, err := manager.ApplyDeploymentLifecycle(ctx, laterReplayCommand)
		require.NoError(t, err)
		require.True(t, renameReplay.IdempotencyReplay)
		require.Equal(t, started.Slot, renameReplay.Slot)
		require.Equal(t, started.Releases, renameReplay.Releases)
		require.Equal(t, started.Qualifications, renameReplay.Qualifications)
	})

	t.Run("promote and stable rollback preserve every decision receipt", func(t *testing.T) {
		fixture, stable := seedCanonicalStable(t, pool, manager, "m76-rollback")
		candidateFixture := createAdditionalDeploymentFixture(
			t, pool, fixture, "m76-rollback-child", "active", 100, 100,
		)
		replaceCanonicalCredentialWithNative(t, pool, &candidateFixture)
		candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_m76_rollback_child")

		started, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
			fixture, runtimechange.DeploymentLifecycleStartCanary, candidate,
			1, 1000, '1', '2', "op_m76_rollback_start",
		))
		require.NoError(t, err)
		promoted, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
			fixture, runtimechange.DeploymentLifecyclePromote,
			mustDeploymentRelease(t, manager, candidate.ReleaseRef),
			started.Slot.AfterVersion, 0, '3', '4', "op_m76_promote",
		))
		require.NoError(t, err)
		require.Equal(t, candidate.ID, *promoted.Slot.AfterStableReleaseID)
		require.Len(t, promoted.Releases, 2)

		_, err = pool.Exec(ctx, `
UPDATE provider_credential_bindings
   SET status='disabled',state_revision=state_revision+1,updated_at=NOW()
 WHERE id=$1`, candidateFixture.CredentialID)
		require.NoError(t, err, "rollback must not require the failing active target to remain healthy")
		active := mustDeploymentRelease(t, manager, candidate.ReleaseRef)
		rolledBack, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
			fixture, runtimechange.DeploymentLifecycleRollbackStable, active,
			promoted.Slot.AfterVersion, 0, '5', '6', "op_m76_rollback_stable",
		))
		require.NoError(t, err)
		require.Equal(t, stable.ID, *rolledBack.Slot.AfterStableReleaseID)
		require.Equal(t, int64(4), rolledBack.Slot.AfterVersion)
		require.Len(t, rolledBack.Releases, 2)

		var completedReceipts, auditRows int
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND status='completed'`, fixture.OrgID).Scan(&completedReceipts))
		require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM audit_logs
 WHERE org_id=$1 AND action LIKE 'model_deployment.lifecycle.%'`, fixture.OrgID).Scan(&auditRows))
		require.Equal(t, 4, completedReceipts,
			"initial activation, start, promote, and rollback each publish one receipt")
		require.Equal(t, completedReceipts, auditRows)
	})
}

func TestInitialDeploymentLifecycleReceiptExecutorIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	manager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	fixture, sealed := prepareInitialDeploymentRelease(t, pool, manager, "m76_receipt")

	receipt, err := manager.ApplyDeploymentLifecycle(
		context.Background(),
		deploymentLifecycleReceiptCommand(
			fixture, runtimechange.DeploymentLifecycleActivateInitial, sealed,
			0, 0, '7', '8', "op_m76_activate_initial",
		),
	)
	require.NoError(t, err)
	require.Equal(t, int64(0), receipt.Slot.BeforeVersion)
	require.Equal(t, int64(1), receipt.Slot.AfterVersion)
	require.Equal(t, sealed.ID, *receipt.Slot.AfterStableReleaseID)
	require.Len(t, receipt.Releases, 1)
	require.Len(t, receipt.Qualifications, 1)
}

func deploymentLifecycleReceiptCommand(
	fixture canonicalDeploymentFixture,
	action runtimechange.DeploymentLifecycleAction,
	release runtimechange.DeploymentRelease,
	expectedSlotVersion int64,
	canaryBasisPoints int,
	keyDigit rune,
	fingerprintDigit rune,
	operationID string,
) runtimechange.DeploymentLifecycleCommand {
	return runtimechange.DeploymentLifecycleCommand{
		Mutation: runtimechange.DeploymentLifecycleMutation{
			Action: action, ReleaseRef: release.ReleaseRef,
			ExpectedReleaseVersion: release.Version,
			ExpectedSlotVersion:    expectedSlotVersion,
			CanaryBasisPoints:      canaryBasisPoints,
			Reason:                 "M76 lifecycle receipt integration",
		},
		CustomerOrgID: fixture.OrgID, ActorUserID: fixture.UserID, ActorOrgID: fixture.OrgID,
		IdempotencyKeyHash: strings.Repeat(string(keyDigit), 64),
		RequestFingerprint: "hmac-sha256:" + strings.Repeat(string(fingerprintDigit), 64),
		ServerOperationID:  operationID,
		Audit: runtimechange.DeploymentLifecycleAudit{
			RequestID: operationID, Source: "admin_console", IP: "127.0.0.1",
			UserAgent: "m76-integration",
		},
	}
}
