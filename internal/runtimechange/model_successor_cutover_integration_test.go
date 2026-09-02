//go:build integration

package runtimechange_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelSuccessorCutoverAtomicIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	manager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()

	predecessor := createCanonicalDeploymentFixture(t, pool, "successor-predecessor", "active", 100, 100)
	predecessor.ModelName = "glm-5.2"
	_, err = pool.Exec(ctx, `
UPDATE model_registry SET model_name=$2,display_name='GLM-5.2'
 WHERE id=$1`, predecessor.PublicModelID, predecessor.ModelName)
	require.NoError(t, err)

	successor := predecessor
	successor.ModelName = "glm-5.2-20260902"
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,
  context_length,output_limit)
VALUES ($1,$2,'GLM-5.2','custom','staging',FALSE,128000,8192)
RETURNING id`, successor.OrgID, successor.ModelName).Scan(&successor.PublicModelID))
	successor = createAdditionalDeploymentFixture(
		t, pool, successor, "successor-target", "active", 100, 100,
	)
	makeSoleInitialCustomer(t, pool, successor.OrgID)
	sealed := sealInitialDeploymentRoutes(
		t, pool, manager, successor, []runtimechange.DeploymentRoute{deploymentRoute(successor)},
		"rcr_glm52_successor",
	)

	var modelSetID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_sets (org_id,name,slug,status)
VALUES ($1,'Stable GLM','stable-glm','active') RETURNING id`, predecessor.OrgID).Scan(&modelSetID))
	_, err = pool.Exec(ctx, `
INSERT INTO model_set_items (model_set_id,item_type,item_name)
VALUES ($1,'model',$2)`, modelSetID, predecessor.ModelName)
	require.NoError(t, err)

	command := runtimechange.ModelSuccessorCutoverCommand{
		Mutation: runtimechange.ModelSuccessorCutoverMutation{
			ReleaseRef: sealed.ReleaseRef, ExpectedReleaseVersion: sealed.Version,
			StableRequestModel:           predecessor.ModelName,
			PredecessorPublicModelID:     predecessor.PublicModelID,
			PredecessorModelName:         predecessor.ModelName,
			ExpectedPredecessorLifecycle: "ga", ExpectedPredecessorApprovedCatalog: true,
			SuccessorPublicModelID:     successor.PublicModelID,
			SuccessorModelName:         successor.ModelName,
			ExpectedSuccessorLifecycle: "staging", ExpectedSuccessorApprovedCatalog: false,
			ExpectedModelSets: []runtimechange.ModelSuccessorExpectedModelSet{{
				ModelSetID: modelSetID, BeforeAliasExists: false,
			}},
			Reason: "first stable request identity cutover",
		},
		CustomerOrgID: successor.OrgID, ActorUserID: successor.UserID, ActorOrgID: successor.OrgID,
		IdempotencyKeyHash: strings.Repeat("a", 64),
		RequestFingerprint: "hmac-sha256:" + strings.Repeat("b", 64),
		ServerOperationID:  "op_glm52_successor",
		Audit: runtimechange.DeploymentLifecycleAudit{
			RequestID: "op_glm52_successor", Source: "admin_console", IP: "127.0.0.1",
		},
	}
	_, err = pool.Exec(ctx, `
INSERT INTO model_aliases (org_id,alias,target_model,display_name,status)
VALUES ($1,$2,$3,'conflict','active')`, successor.OrgID, predecessor.ModelName, successor.ModelName)
	require.NoError(t, err)
	_, err = manager.ApplyModelSuccessorCutover(ctx, command)
	require.ErrorContains(t, err, "stable request alias already exists")
	var failedReceiptCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_lifecycle_receipts
 WHERE idempotency_key_hash=$1`, command.IdempotencyKeyHash).Scan(&failedReceiptCount))
	require.Zero(t, failedReceiptCount)
	var failedPredecessorLifecycle, failedSuccessorLifecycle string
	require.NoError(t, pool.QueryRow(ctx, `SELECT lifecycle FROM model_registry WHERE id=$1`, predecessor.PublicModelID).Scan(&failedPredecessorLifecycle))
	require.NoError(t, pool.QueryRow(ctx, `SELECT lifecycle FROM model_registry WHERE id=$1`, successor.PublicModelID).Scan(&failedSuccessorLifecycle))
	require.Equal(t, "ga", failedPredecessorLifecycle)
	require.Equal(t, "staging", failedSuccessorLifecycle)
	_, err = pool.Exec(ctx, `DELETE FROM model_aliases WHERE org_id=$1 AND alias=$2`, successor.OrgID, predecessor.ModelName)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_aliases (org_id,alias,target_model,display_name,status)
VALUES ($1,'glm-secondary',$2,'secondary','active')`, successor.OrgID, successor.ModelName)
	require.NoError(t, err)
	_, err = manager.ApplyModelSuccessorCutover(ctx, command)
	require.ErrorContains(t, err, "successor already has an active alias")
	_, err = pool.Exec(ctx, `DELETE FROM model_aliases WHERE org_id=$1 AND alias='glm-secondary'`, successor.OrgID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `DELETE FROM model_set_items WHERE model_set_id=$1 AND item_type='model'`, modelSetID)
	require.NoError(t, err)
	_, err = manager.ApplyModelSuccessorCutover(ctx, command)
	require.ErrorContains(t, err, "model-set snapshot drift")
	_, err = pool.Exec(ctx, `
INSERT INTO model_set_items (model_set_id,item_type,item_name)
VALUES ($1,'model',$2),($1,'alias',$2)`, modelSetID, predecessor.ModelName)
	require.NoError(t, err)
	_, err = manager.ApplyModelSuccessorCutover(ctx, command)
	require.ErrorContains(t, err, "model-set snapshot drift")
	command.Mutation.ExpectedModelSets[0].BeforeAliasExists = true

	var addedModelSetID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_sets (org_id,name,slug,status)
VALUES ($1,'Late GLM','late-glm','active') RETURNING id`, predecessor.OrgID).Scan(&addedModelSetID))
	_, err = pool.Exec(ctx, `
INSERT INTO model_set_items (model_set_id,item_type,item_name)
VALUES ($1,'model',$2)`, addedModelSetID, predecessor.ModelName)
	require.NoError(t, err)
	_, err = manager.ApplyModelSuccessorCutover(ctx, command)
	require.ErrorContains(t, err, "model-set snapshot drift")
	_, err = pool.Exec(ctx, `DELETE FROM model_set_items WHERE model_set_id=$1`, addedModelSetID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM model_sets WHERE id=$1`, addedModelSetID)
	require.NoError(t, err)

	receipt, err := manager.ApplyModelSuccessorCutover(ctx, command)
	require.NoError(t, err)
	require.Equal(t, runtimechange.ModelSuccessorCutoverReceiptContractVersion, receipt.ContractVersion)
	require.Equal(t, runtimechange.ModelSuccessorLifecycleReceiptContractVersion, receipt.Lifecycle.ContractVersion)
	require.Equal(t, predecessor.ModelName, receipt.StableRequestModel)
	require.Len(t, receipt.ModelSets, 1)
	require.False(t, receipt.Lifecycle.IdempotencyReplay)

	var predecessorLifecycle, successorLifecycle, aliasTarget, itemType, itemName string
	var predecessorCatalog, successorCatalog, primary bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT lifecycle,approved_catalog FROM model_registry WHERE id=$1`, predecessor.PublicModelID).Scan(&predecessorLifecycle, &predecessorCatalog))
	require.NoError(t, pool.QueryRow(ctx, `SELECT lifecycle,approved_catalog FROM model_registry WHERE id=$1`, successor.PublicModelID).Scan(&successorLifecycle, &successorCatalog))
	require.NoError(t, pool.QueryRow(ctx, `SELECT target_model,is_primary FROM model_aliases WHERE org_id=$1 AND alias=$2`, successor.OrgID, predecessor.ModelName).Scan(&aliasTarget, &primary))
	require.NoError(t, pool.QueryRow(ctx, `SELECT item_type,item_name FROM model_set_items WHERE model_set_id=$1`, modelSetID).Scan(&itemType, &itemName))
	require.Equal(t, "retired", predecessorLifecycle)
	require.False(t, predecessorCatalog)
	require.Equal(t, "ga", successorLifecycle)
	require.True(t, successorCatalog)
	require.Equal(t, successor.ModelName, aliasTarget)
	require.True(t, primary)
	require.Equal(t, "alias", itemType)
	require.Equal(t, predecessor.ModelName, itemName)

	_, err = pool.Exec(ctx, `UPDATE model_aliases SET status='disabled' WHERE org_id=$1 AND alias=$2`, successor.OrgID, predecessor.ModelName)
	require.ErrorContains(t, err, "primary model alias is immutable")
	_, err = pool.Exec(ctx, `UPDATE model_registry SET lifecycle='ga',approved_catalog=TRUE WHERE id=$1`, predecessor.PublicModelID)
	require.ErrorContains(t, err, "cannot be revived")
	_, err = pool.Exec(ctx, `UPDATE model_registry SET lifecycle='staging',approved_catalog=FALSE WHERE id=$1`, successor.PublicModelID)
	require.ErrorContains(t, err, "cannot be staged")
	_, err = pool.Exec(ctx, `UPDATE model_successor_cutover_receipts SET stable_request_model='other' WHERE lifecycle_receipt_id=$1`, receipt.Lifecycle.ReceiptID)
	require.ErrorContains(t, err, "evidence is immutable")
	_, err = pool.Exec(ctx, `
INSERT INTO model_aliases (org_id,alias,target_model,display_name,status)
VALUES ($1,'glm-secondary',$2,'secondary','active')`, successor.OrgID, successor.ModelName)
	require.ErrorContains(t, err, "only its primary stable alias")

	replayCommand := command
	replayCommand.ServerOperationID = "op_glm52_successor_replay"
	replayCommand.Audit.RequestID = replayCommand.ServerOperationID
	replayed, err := manager.ApplyModelSuccessorCutover(ctx, replayCommand)
	require.NoError(t, err)
	require.True(t, replayed.Lifecycle.IdempotencyReplay)
	require.Equal(t, receipt.Lifecycle.ReceiptID, replayed.Lifecycle.ReceiptID)
	require.Equal(t, receipt.ModelSets, replayed.ModelSets)

	completedRecoveryManager, err := runtimechange.NewManager(
		&successorClaimErrorPool{Pool: pool, code: "40001"}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	recovered, err := completedRecoveryManager.ApplyModelSuccessorCutover(ctx, replayCommand)
	require.NoError(t, err)
	require.Equal(t, receipt.Lifecycle.ReceiptID, recovered.Lifecycle.ReceiptID)

	assertRecoverySQLState := func(t *testing.T, state, keyChar, fingerprintChar, recoveryStatus string) {
		t.Helper()
		candidate := command
		candidate.IdempotencyKeyHash = strings.Repeat(keyChar, 64)
		candidate.RequestFingerprint = "hmac-sha256:" + strings.Repeat(fingerprintChar, 64)
		candidate.ServerOperationID = "op_" + state
		claimErrorPool := &successorClaimErrorPool{Pool: pool, code: "40P01"}
		if recoveryStatus != "" {
			claimErrorPool.recoveryRow = &successorRecoveryRow{
				receiptID: 901, requestFingerprint: candidate.RequestFingerprint,
				serverOperationID: "winner_" + state, status: recoveryStatus,
			}
		}
		managerWithClaimError, managerErr := runtimechange.NewManager(
			claimErrorPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
		)
		require.NoError(t, managerErr)
		_, applyErr := managerWithClaimError.ApplyModelSuccessorCutover(ctx, candidate)
		var sqlState interface{ SQLState() string }
		require.Error(t, applyErr)
		require.True(t, errors.As(applyErr, &sqlState), "state=%s err=%v", state, applyErr)
		require.Equal(t, "40P01", sqlState.SQLState())
	}
	assertRecoverySQLState(t, "absent", "c", "d", "")
	assertRecoverySQLState(t, "nonterminal", "d", "e", "in_progress")

	conflicting := command
	conflicting.IdempotencyKeyHash = strings.Repeat("e", 64)
	conflicting.RequestFingerprint = "hmac-sha256:" + strings.Repeat("f", 64)
	conflicting.ServerOperationID = "op_conflicting"
	conflictingRecoveryPool := &successorClaimErrorPool{
		Pool: pool, code: "40001",
		recoveryRow: &successorRecoveryRow{
			receiptID: 902, requestFingerprint: "hmac-sha256:" + strings.Repeat("0", 64),
			serverOperationID: "winner_conflicting", status: "completed",
		},
	}
	conflictRecoveryManager, err := runtimechange.NewManager(
		conflictingRecoveryPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	_, err = conflictRecoveryManager.ApplyModelSuccessorCutover(ctx, conflicting)
	require.ErrorIs(t, err, runtimechange.ErrDeploymentLifecycleIdempotencyConflict)

	downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000107_stable_model_successor_cutover.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "rollback refused")
}

type successorClaimErrorPool struct {
	*pgxpool.Pool
	code        string
	recoveryRow *successorRecoveryRow
}

func (pool *successorClaimErrorPool) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := pool.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &successorClaimErrorTx{Tx: tx, code: pool.code}, nil
}

func (pool *successorClaimErrorPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if pool.recoveryRow != nil &&
		strings.Contains(sql, "SELECT receipt_id,request_fingerprint,server_operation_id,status,created_at") {
		return *pool.recoveryRow
	}
	return pool.Pool.QueryRow(ctx, sql, args...)
}

type successorClaimErrorTx struct {
	pgx.Tx
	code string
}

func (tx *successorClaimErrorTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "INSERT INTO model_deployment_lifecycle_receipts") &&
		strings.Contains(sql, "successor_predecessor_public_model_id") {
		return successorClaimErrorRow{err: &pgconn.PgError{Code: tx.code}}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

type successorClaimErrorRow struct{ err error }

func (row successorClaimErrorRow) Scan(...any) error { return row.err }

type successorRecoveryRow struct {
	receiptID          int64
	requestFingerprint string
	serverOperationID  string
	status             string
}

func (row successorRecoveryRow) Scan(dest ...any) error {
	if len(dest) != 5 {
		return errors.New("unexpected successor recovery scan shape")
	}
	*dest[0].(*int64) = row.receiptID
	*dest[1].(*string) = row.requestFingerprint
	*dest[2].(*string) = row.serverOperationID
	*dest[3].(*string) = row.status
	*dest[4].(*time.Time) = time.Unix(1, 0).UTC()
	return nil
}

func TestStableModelSuccessorCutoverMigrationGuardedRoundTripWithoutEvidence(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	downSQL, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000107_stable_model_successor_cutover.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err)

	var m106Count, m107Count int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FILTER (WHERE version='000106_minimax_official_cn_payg_endpoint_qualification'),
       COUNT(*) FILTER (WHERE version='000107_stable_model_successor_cutover')
  FROM schema_migrations`).Scan(&m106Count, &m107Count))
	require.Equal(t, 1, m106Count)
	require.Zero(t, m107Count)
	var primaryColumnCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
 WHERE table_schema=current_schema() AND table_name='model_aliases' AND column_name='is_primary'`).Scan(&primaryColumnCount))
	require.Zero(t, primaryColumnCount)
}
