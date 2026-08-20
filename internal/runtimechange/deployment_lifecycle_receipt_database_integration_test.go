//go:build integration

package runtimechange_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

type lifecycleReceiptSQLExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func TestModelDeploymentLifecycleReceiptDatabaseGuardsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	manager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()
	fixture, stable := seedCanonicalStable(t, pool, manager, "m76-db-guards")
	candidateFixture := createAdditionalDeploymentFixture(
		t, pool, fixture, "m76-db-guards-child", "active", 100, 100,
	)
	candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_m76_db_guards_child")
	completed, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		fixture, runtimechange.DeploymentLifecycleStartCanary, candidate,
		1, 750, 'a', 'b', "op_m76_db_guard_source",
	))
	require.NoError(t, err)

	t.Run("direct completed insert is rejected before constraints can be forged", func(t *testing.T) {
		_, insertErr := pool.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipts (
  contract_version,customer_org_id,actor_user_id,actor_org_id,
  operation_scope,action,idempotency_key_hash,request_fingerprint,
  server_operation_id,target_release_ref,expected_release_version,
  expected_slot_version,requested_canary_basis_points,reason,status)
VALUES (
  'unirouter.model-deployment-lifecycle-receipt.v1',$1,$2,$1,
  'model_deployment.lifecycle.start_canary.v1','start_canary',repeat('c',64),
  'hmac-sha256:' || repeat('d',64),'op_m76_direct_completed',$3,1,1,100,
  'direct completed insert must fail','completed')`,
			fixture.OrgID, fixture.UserID, candidate.ReleaseRef)
		require.ErrorContains(t, insertErr, "must be claimed in progress")
	})

	var sourceAuditID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT audit_log_id FROM model_deployment_lifecycle_receipts WHERE receipt_id=$1`,
		completed.ReceiptID).Scan(&sourceAuditID))

	t.Run("foreign audit cannot complete a receipt", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '1')
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, sourceAuditID)
		require.ErrorContains(t, completionErr, "requires the exact audit row")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("zero release evidence cannot complete a receipt", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '2')
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires exact release cardinality")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("missing qualification evidence cannot complete a receipt", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '3')
		copyLifecycleReceiptReleases(t, ctx, tx, cloneID, completed.ReceiptID, "")
		rebindLifecycleReceiptEvents(t, ctx, tx, cloneID, completed.ReceiptID)
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires exact qualification evidence")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("missing receipt-bound event cannot complete a receipt", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '4')
		copyLifecycleReceiptReleases(t, ctx, tx, cloneID, completed.ReceiptID, "")
		copyLifecycleReceiptQualifications(t, ctx, tx, cloneID, completed.ReceiptID)
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires exact lifecycle events")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("mismatched live slot cannot complete a receipt", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '5')
		copyLifecycleReceiptReleases(t, ctx, tx, cloneID, completed.ReceiptID, "")
		copyLifecycleReceiptQualifications(t, ctx, tx, cloneID, completed.ReceiptID)
		rebindLifecycleReceiptEvents(t, ctx, tx, cloneID, completed.ReceiptID)
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, err = tx.Exec(ctx, `
UPDATE runtime_change_slots SET slot_version=slot_version+1 WHERE public_model_id=$1`, fixture.PublicModelID)
		require.NoError(t, err)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires the exact live after slot")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("terminal receipt cannot accept later evidence", func(t *testing.T) {
		_, appendErr := pool.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipt_releases (
  receipt_id,release_role,release_id,release_ref,candidate_hash,route_items_hash,
  before_stage,before_version,after_stage,after_version)
SELECT receipt_id,release_role,release_id,release_ref,candidate_hash,route_items_hash,
       before_stage,before_version,after_stage,after_version
  FROM model_deployment_lifecycle_receipt_releases
 WHERE receipt_id=$1 AND release_role='target'`, completed.ReceiptID)
		require.ErrorContains(t, appendErr, "evidence is terminal")
	})

	t.Run("completed receipt event is durable append-only evidence", func(t *testing.T) {
		_, insertErr := pool.Exec(ctx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail)
VALUES ($1,$2,'rollback','active','rolled_back',$3,$4,$2,
        jsonb_build_object('lifecycle_receipt_id',$5::bigint))`,
			stable.ID, stable.Version+1, fixture.UserID, stable.Version, completed.ReceiptID)
		require.ErrorContains(t, insertErr, "requires an in-progress lifecycle receipt")
		_, malformedInsertErr := pool.Exec(ctx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail)
VALUES ($1,$2,'rollback','active','rolled_back',$3,$4,$2,
        '{"lifecycle_receipt_id":"not-an-integer"}'::jsonb)`,
			stable.ID, stable.Version+1, fixture.UserID, stable.Version)
		require.ErrorContains(t, malformedInsertErr, "requires a positive lifecycle receipt identity")
		_, updateErr := pool.Exec(ctx, `
UPDATE runtime_change_events
   SET detail=detail || '{"tampered":true}'::jsonb
 WHERE detail->>'lifecycle_receipt_id'=$1::bigint::text`, completed.ReceiptID)
		require.ErrorContains(t, updateErr, "runtime change events are append-only evidence")
		_, deleteErr := pool.Exec(ctx, `
DELETE FROM runtime_change_events
 WHERE detail->>'lifecycle_receipt_id'=$1::bigint::text`, completed.ReceiptID)
		require.ErrorContains(t, deleteErr, "runtime change events are append-only evidence")
		_, truncateErr := pool.Exec(ctx, `TRUNCATE runtime_change_events`)
		require.ErrorContains(t, truncateErr, "runtime change events are append-only evidence")
	})
}

func TestDeploymentLifecycleReceiptLegacyDatabaseGuardsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	recordedPool := &recordingBeginTxPool{Pool: pool}
	planner, err := modelrouting.NewPlanner(recordedPool, routingpolicy.Default())
	require.NoError(t, err)
	manager, err := runtimechange.NewManagerWithLegacyCutoverVerifier(
		recordedPool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(), planner,
	)
	require.NoError(t, err)
	ctx := context.Background()
	fixture := createCanonicalDeploymentFixture(t, pool, "m78-legacy-db-guards", "active", 100, 100)
	insertLegacyStableRelease(t, pool, fixture)
	_, err = manager.PrepareLegacyCompatibilitySeal(
		ctx, legacyReleaseRef(fixture), 1, fixture.OrgID, fixture.PublicModelID,
	)
	require.NoError(t, err)
	target := authorLegacyCutoverRelease(t, pool, manager, fixture, "legacy DB guard source")
	completed, err := applyLegacyLifecycle(t, pool, manager, fixture, fixture.UserID,
		runtimechange.DeploymentLifecycleMutation{
			Action:                 runtimechange.DeploymentLifecycleCutoverLegacy,
			ReleaseRef:             target.ReleaseRef,
			ExpectedReleaseVersion: target.Version,
			ExpectedSlotVersion:    1,
			Reason:                 "publish exact legacy DB guard evidence",
		},
	)
	require.NoError(t, err)
	require.Equal(t, runtimechange.DeploymentLifecycleCutoverLegacy, completed.Action)

	var sourceAuditID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT audit_log_id FROM model_deployment_lifecycle_receipts WHERE receipt_id=$1`,
		completed.ReceiptID).Scan(&sourceAuditID))

	t.Run("direct completed insert is rejected by the legacy trigger", func(t *testing.T) {
		_, insertErr := pool.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipts (
  contract_version,customer_org_id,actor_user_id,actor_org_id,
  operation_scope,action,idempotency_key_hash,request_fingerprint,
  server_operation_id,target_release_ref,expected_release_version,
  expected_slot_version,requested_canary_basis_points,reason,status)
SELECT contract_version,customer_org_id,actor_user_id,actor_org_id,
       operation_scope,action,repeat('1',64),'hmac-sha256:' || repeat('1',64),
       'op_m78_legacy_direct_completed',target_release_ref,expected_release_version,
       expected_slot_version,requested_canary_basis_points,reason,'completed'
  FROM model_deployment_lifecycle_receipts WHERE receipt_id=$1`, completed.ReceiptID)
		require.ErrorContains(t, insertErr, "must be claimed in progress")
	})

	t.Run("zero release evidence cannot complete", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '2')
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires two release images")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("wrong dual release evidence cannot complete", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '3')
		copyLifecycleReceiptReleases(t, ctx, tx, cloneID, completed.ReceiptID, "target")
		_, insertErr := tx.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipt_releases (
  receipt_id,release_role,release_id,release_ref,candidate_hash,route_items_hash,
  before_stage,before_version,after_stage,after_version)
SELECT $1,'stable_parent',release_id,release_ref,candidate_hash,route_items_hash,
       before_stage,before_version,after_stage,after_version
  FROM model_deployment_lifecycle_receipt_releases
 WHERE receipt_id=$2 AND release_role='target'`, cloneID, completed.ReceiptID)
		require.NoError(t, insertErr)
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires exact dual-release evidence")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("foreign audit cannot complete", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '4')
		_, completionErr := completeClonedLifecycleReceipt(
			ctx, tx, cloneID, completed.ReceiptID, sourceAuditID,
		)
		require.ErrorContains(t, completionErr, "requires the exact audit row")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("missing receipt events cannot complete", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '5')
		copyLifecycleReceiptReleases(t, ctx, tx, cloneID, completed.ReceiptID, "")
		copyLifecycleReceiptQualifications(t, ctx, tx, cloneID, completed.ReceiptID)
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires exactly two receipt events")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("missing qualification evidence cannot complete", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '6')
		copyLifecycleReceiptReleases(t, ctx, tx, cloneID, completed.ReceiptID, "")
		rebindLifecycleReceiptEvents(t, ctx, tx, cloneID, completed.ReceiptID)
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires exact qualification evidence")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("raw slot mismatch cannot complete", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '7')
		copyLifecycleReceiptReleases(t, ctx, tx, cloneID, completed.ReceiptID, "")
		copyLifecycleReceiptQualifications(t, ctx, tx, cloneID, completed.ReceiptID)
		rebindLifecycleReceiptEvents(t, ctx, tx, cloneID, completed.ReceiptID)
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, err = tx.Exec(ctx, `ALTER TABLE runtime_change_slots DISABLE TRIGGER USER`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
UPDATE runtime_change_slots SET public_model_id=NULL WHERE model_name=$1`, fixture.ModelName)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `ALTER TABLE runtime_change_slots ENABLE TRIGGER USER`)
		require.NoError(t, err)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires exact raw after slot")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("forged event hash cannot complete", func(t *testing.T) {
		tx := beginLifecycleGuardTx(t, ctx, pool)
		cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, '8')
		copyLifecycleReceiptReleases(t, ctx, tx, cloneID, completed.ReceiptID, "")
		copyLifecycleReceiptQualifications(t, ctx, tx, cloneID, completed.ReceiptID)
		rebindLifecycleReceiptEvents(t, ctx, tx, cloneID, completed.ReceiptID)
		auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
		_, err = tx.Exec(ctx, `
ALTER TABLE runtime_change_events DISABLE TRIGGER runtime_change_events_append_only`)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
UPDATE runtime_change_events
   SET detail=jsonb_set(detail,'{legacy_plan_hash}',to_jsonb($1::text),TRUE)
 WHERE detail->>'lifecycle_receipt_id'=$2::bigint::text`,
			"sha256:"+strings.Repeat("f", 64), cloneID)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `
ALTER TABLE runtime_change_events ENABLE TRIGGER runtime_change_events_append_only`)
		require.NoError(t, err)
		_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
		require.ErrorContains(t, completionErr, "requires exactly two receipt events")
		require.NoError(t, tx.Rollback(ctx))
	})

	t.Run("terminal evidence cannot mutate or append", func(t *testing.T) {
		_, updateErr := pool.Exec(ctx, `
UPDATE model_deployment_lifecycle_receipts SET reason=reason || ' tampered' WHERE receipt_id=$1`,
			completed.ReceiptID)
		require.ErrorContains(t, updateErr, "identity is immutable")
		_, deleteErr := pool.Exec(ctx, `
DELETE FROM model_deployment_lifecycle_receipts WHERE receipt_id=$1`, completed.ReceiptID)
		require.ErrorContains(t, deleteErr, "durable evidence")
		_, appendErr := pool.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipt_releases (
  receipt_id,release_role,release_id,release_ref,candidate_hash,route_items_hash,
  before_stage,before_version,after_stage,after_version)
SELECT receipt_id,release_role,release_id,release_ref,candidate_hash,route_items_hash,
       before_stage,before_version,after_stage,after_version
  FROM model_deployment_lifecycle_receipt_releases
 WHERE receipt_id=$1 AND release_role='target'`, completed.ReceiptID)
		require.ErrorContains(t, appendErr, "evidence is terminal")
		_, eventErr := pool.Exec(ctx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail)
SELECT release_id,after_version+100,'rollback',after_stage,'validated',$2,
       after_version,after_version+100,jsonb_build_object('lifecycle_receipt_id',$1::bigint)
  FROM model_deployment_lifecycle_receipt_releases
 WHERE receipt_id=$1 AND release_role='target'`, completed.ReceiptID, fixture.UserID)
		require.ErrorContains(t, eventErr, "requires an in-progress lifecycle receipt")
	})
}

func TestModelDeploymentLifecycleReceiptChildCompletionSerializationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	manager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()
	fixture, _ := seedCanonicalStable(t, pool, manager, "m76-child-lock")
	candidateFixture := createAdditionalDeploymentFixture(
		t, pool, fixture, "m76-child-lock-target", "active", 100, 100,
	)
	candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_m76_child_lock_target")
	completed, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		fixture, runtimechange.DeploymentLifecycleStartCanary, candidate,
		1, 500, '6', '7', "op_m76_child_lock_source",
	))
	require.NoError(t, err)

	var cloneID int64
	_, err = pool.Exec(ctx, `
ALTER TABLE model_deployment_lifecycle_receipts
  DISABLE TRIGGER model_deployment_lifecycle_receipts_terminal`)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, lifecycleReceiptCloneSQL,
		completed.ReceiptID, lifecycleGuardKey('8'), lifecycleGuardFingerprint('8'),
		lifecycleGuardOperation('8')).Scan(&cloneID))
	_, err = pool.Exec(ctx, `
ALTER TABLE model_deployment_lifecycle_receipts
  ENABLE TRIGGER model_deployment_lifecycle_receipts_terminal`)
	require.NoError(t, err)

	childTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	copyLifecycleReceiptReleases(t, ctx, childTx, cloneID, completed.ReceiptID, "target")

	otherPool, err := pgxpool.New(ctx, pool.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(otherPool.Close)
	var sourceAuditID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT audit_log_id FROM model_deployment_lifecycle_receipts WHERE receipt_id=$1`,
		completed.ReceiptID).Scan(&sourceAuditID))

	completionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	completionResult := make(chan error, 1)
	go func() {
		_, completionErr := completeClonedLifecycleReceipt(
			completionCtx, otherPool, cloneID, completed.ReceiptID, sourceAuditID,
		)
		completionResult <- completionErr
	}()
	select {
	case completionErr := <-completionResult:
		t.Fatalf("completion crossed an uncommitted child append: %v", completionErr)
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, childTx.Commit(ctx))
	select {
	case completionErr := <-completionResult:
		require.ErrorContains(t, completionErr, "requires the exact audit row")
	case <-time.After(5 * time.Second):
		t.Fatal("completion remained blocked after child evidence committed")
	}
	var status string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT status FROM model_deployment_lifecycle_receipts WHERE receipt_id=$1`, cloneID).Scan(&status))
	require.Equal(t, "in_progress", status)
}

func TestModelDeploymentLifecycleReceiptRejectsExtraInitialEvidenceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	manager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()
	fixture, sealed := prepareInitialDeploymentRelease(t, pool, manager, "m76_extra_evidence")
	completed, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		fixture, runtimechange.DeploymentLifecycleActivateInitial, sealed,
		0, 0, '9', 'a', "op_m76_extra_source",
	))
	require.NoError(t, err)

	tx := beginLifecycleGuardTx(t, ctx, pool)
	cloneID := cloneLifecycleReceiptInProgress(t, ctx, tx, completed.ReceiptID, 'b')
	copyLifecycleReceiptReleases(t, ctx, tx, cloneID, completed.ReceiptID, "target")
	_, err = tx.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipt_releases (
  receipt_id,release_role,release_id,release_ref,candidate_hash,route_items_hash,
  before_stage,before_version,after_stage,after_version)
SELECT $1,'stable_parent',release_id,release_ref,candidate_hash,route_items_hash,
       before_stage,before_version,after_stage,after_version
  FROM model_deployment_lifecycle_receipt_releases
 WHERE receipt_id=$2 AND release_role='target'`, cloneID, completed.ReceiptID)
	require.NoError(t, err)
	auditID := cloneLifecycleReceiptAudit(t, ctx, tx, cloneID, completed.ReceiptID)
	_, completionErr := completeClonedLifecycleReceipt(ctx, tx, cloneID, completed.ReceiptID, auditID)
	require.ErrorContains(t, completionErr, "requires exact release cardinality")
	require.NoError(t, tx.Rollback(ctx))
}

func TestModelDeploymentLifecycleReceiptDownLockOrderIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	authoringDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000078_atomic_model_release_authoring.down.sql",
	))
	require.NoError(t, err)
	downgradeCanonicalConnectionLicenseAuthorityForTest(t, pool)
	_, err = pool.Exec(t.Context(), string(authoringDownSQL))
	require.NoError(t, err)
	ctx := context.Background()
	fixture := createCanonicalDeploymentFixture(t, pool, "m76-down-lock-order", "active", 100, 100)
	stableID := insertLegacyStableRelease(t, pool, fixture)
	stableRef := legacyReleaseRef(fixture)
	const stableVersion int64 = 1
	publicAuthorityDownSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000077_public_model_execution_authority.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(publicAuthorityDownSQL))
	require.NoError(t, err)
	otherPool, err := pgxpool.New(ctx, pool.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(otherPool.Close)
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000076_model_deployment_lifecycle_receipts.down.sql",
	))
	require.NoError(t, err)

	writerTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = writerTx.Exec(ctx, `LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE`)
	require.NoError(t, err)
	_, err = writerTx.Exec(ctx, `LOCK TABLE runtime_change_slots IN ROW EXCLUSIVE MODE`)
	require.NoError(t, err)
	_, err = writerTx.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipts (
  contract_version,customer_org_id,actor_user_id,actor_org_id,
  operation_scope,action,idempotency_key_hash,request_fingerprint,
  server_operation_id,target_release_ref,expected_release_version,
  expected_slot_version,requested_canary_basis_points,reason)
VALUES (
  'unirouter.model-deployment-lifecycle-receipt.v1',$1,$2,$1,
  'model_deployment.lifecycle.rollback_stable.v1','rollback_stable',repeat('d',64),
  'hmac-sha256:' || repeat('e',64),'op_m76_down_lock_writer',$3,$4,1,0,
  'hold the application writer lock order')`,
		fixture.OrgID, fixture.UserID, stableRef, stableVersion)
	require.NoError(t, err)

	downResult := make(chan error, 1)
	go func() {
		_, downErr := otherPool.Exec(ctx, string(downSQL))
		downResult <- downErr
	}()
	select {
	case downErr := <-downResult:
		t.Fatalf("down migration crossed active release/slot writer locks: %v", downErr)
	case <-time.After(150 * time.Millisecond):
	}

	eventCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = writerTx.Exec(eventCtx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail)
VALUES ($1,$2,'rollback','active','rolled_back',$3,$4,$2,
        '{"m76_down_lock_order":true}'::jsonb)`,
		stableID, stableVersion+1, fixture.UserID, stableVersion)
	require.NoError(t, err,
		"down must wait on release/slot before it can lock events needed by the active writer")
	require.NoError(t, writerTx.Rollback(ctx))
	select {
	case downErr := <-downResult:
		require.NoError(t, downErr)
	case <-time.After(5 * time.Second):
		t.Fatal("down migration remained blocked after the application writer rolled back")
	}
}

func TestModelDeploymentLifecycleReceiptEventCompletionSerializationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	manager, err := runtimechange.NewManager(
		&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default(),
	)
	require.NoError(t, err)
	ctx := context.Background()
	fixture, stable := seedCanonicalStable(t, pool, manager, "m76-event-lock")
	candidateFixture := createAdditionalDeploymentFixture(
		t, pool, fixture, "m76-event-lock-target", "active", 100, 100,
	)
	candidate := prepareSealedChild(t, pool, manager, fixture, candidateFixture, "rcr_m76_event_lock_target")
	completed, err := manager.ApplyDeploymentLifecycle(ctx, deploymentLifecycleReceiptCommand(
		fixture, runtimechange.DeploymentLifecycleStartCanary, candidate,
		1, 500, 'a', 'b', "op_m76_event_lock_source",
	))
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
ALTER TABLE model_deployment_lifecycle_receipts
  DISABLE TRIGGER model_deployment_lifecycle_receipts_terminal`)
	require.NoError(t, err)
	seedTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	cloneID := cloneLifecycleReceiptInProgress(t, ctx, seedTx, completed.ReceiptID, 'f')
	copyLifecycleReceiptReleases(t, ctx, seedTx, cloneID, completed.ReceiptID, "")
	copyLifecycleReceiptQualifications(t, ctx, seedTx, cloneID, completed.ReceiptID)
	auditID := cloneLifecycleReceiptAudit(t, ctx, seedTx, cloneID, completed.ReceiptID)
	require.NoError(t, seedTx.Commit(ctx))
	_, err = pool.Exec(ctx, `
ALTER TABLE model_deployment_lifecycle_receipts
  ENABLE TRIGGER model_deployment_lifecycle_receipts_terminal`)
	require.NoError(t, err)

	eventTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = eventTx.Exec(ctx, `
INSERT INTO runtime_change_events (
  release_id,sequence,action,from_stage,to_stage,actor_user_id,
  expected_version,resulting_version,detail)
VALUES ($1,$2,'rollback','active','rolled_back',$3,$4,$2,
        jsonb_build_object('lifecycle_receipt_id',$5::bigint))`,
		stable.ID, stable.Version+1, fixture.UserID, stable.Version, cloneID)
	require.NoError(t, err)

	otherPool, err := pgxpool.New(ctx, pool.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(otherPool.Close)
	completionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	completionResult := make(chan error, 1)
	go func() {
		_, completionErr := completeClonedLifecycleReceipt(
			completionCtx, otherPool, cloneID, completed.ReceiptID, auditID,
		)
		completionResult <- completionErr
	}()
	select {
	case completionErr := <-completionResult:
		t.Fatalf("completion crossed an uncommitted receipt-linked event: %v", completionErr)
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, eventTx.Commit(ctx))
	select {
	case completionErr := <-completionResult:
		require.ErrorContains(t, completionErr, "requires exact lifecycle events")
	case <-time.After(5 * time.Second):
		t.Fatal("completion remained blocked after receipt-linked event committed")
	}
	var status string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT status FROM model_deployment_lifecycle_receipts WHERE receipt_id=$1`, cloneID).Scan(&status))
	require.Equal(t, "in_progress", status)
}

func beginLifecycleGuardTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func lifecycleGuardKey(digit rune) string {
	return strings.Repeat(string(digit), 64)
}

func lifecycleGuardFingerprint(digit rune) string {
	return "hmac-sha256:" + lifecycleGuardKey(digit)
}

func lifecycleGuardOperation(digit rune) string {
	return "op_m76_guard_clone_" + string(digit)
}

const lifecycleReceiptCloneSQL = `
INSERT INTO model_deployment_lifecycle_receipts (
  contract_version,customer_org_id,actor_user_id,actor_org_id,
  operation_scope,action,idempotency_key_hash,request_fingerprint,
  server_operation_id,target_release_ref,expected_release_version,
  expected_slot_version,requested_canary_basis_points,reason)
SELECT contract_version,customer_org_id,actor_user_id,actor_org_id,
       operation_scope,action,$2,$3,$4,target_release_ref,
       expected_release_version,expected_slot_version,
       requested_canary_basis_points,reason
  FROM model_deployment_lifecycle_receipts
 WHERE receipt_id=$1
RETURNING receipt_id`

func cloneLifecycleReceiptInProgress(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	sourceReceiptID int64,
	digit rune,
) int64 {
	t.Helper()
	var receiptID int64
	require.NoError(t, tx.QueryRow(ctx, lifecycleReceiptCloneSQL,
		sourceReceiptID, lifecycleGuardKey(digit), lifecycleGuardFingerprint(digit),
		lifecycleGuardOperation(digit)).Scan(&receiptID))
	return receiptID
}

func copyLifecycleReceiptReleases(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	sourceReceiptID int64,
	role string,
) {
	t.Helper()
	_, err := tx.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipt_releases (
  receipt_id,release_role,release_id,release_ref,candidate_hash,route_items_hash,
  before_stage,before_version,after_stage,after_version)
SELECT $1,release_role,release_id,release_ref,candidate_hash,route_items_hash,
       before_stage,before_version,after_stage,after_version
  FROM model_deployment_lifecycle_receipt_releases
 WHERE receipt_id=$2 AND ($3='' OR release_role=$3)
 ORDER BY release_role`, receiptID, sourceReceiptID, role)
	require.NoError(t, err)
}

func copyLifecycleReceiptQualifications(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	sourceReceiptID int64,
) {
	t.Helper()
	_, err := tx.Exec(ctx, `
INSERT INTO model_deployment_lifecycle_receipt_qualifications (
  receipt_id,release_id,release_role,ordinal,deployment_id,operation_id,
  qualification_required,qualified,qualification_state,approval_id,probe_id,
  connection_id,https_origin,public_path,executor_contract_version,
  expected_connection_revision,expected_deployment_revision,expected_operation_revision,
  current_connection_revision,current_deployment_revision,current_operation_revision,
  approved_connection_revision,approved_deployment_revision,approved_operation_revision)
SELECT $1,release_id,release_role,ordinal,deployment_id,operation_id,
       qualification_required,qualified,qualification_state,approval_id,probe_id,
       connection_id,https_origin,public_path,executor_contract_version,
       expected_connection_revision,expected_deployment_revision,expected_operation_revision,
       current_connection_revision,current_deployment_revision,current_operation_revision,
       approved_connection_revision,approved_deployment_revision,approved_operation_revision
  FROM model_deployment_lifecycle_receipt_qualifications
 WHERE receipt_id=$2
 ORDER BY release_id,ordinal`, receiptID, sourceReceiptID)
	require.NoError(t, err)
}

func cloneLifecycleReceiptAudit(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	sourceReceiptID int64,
) int64 {
	t.Helper()
	operationID := lifecycleGuardOperation(rune('0' + receiptID%10))
	var auditID int64
	require.NoError(t, tx.QueryRow(ctx, `
INSERT INTO audit_logs (
  org_id,user_id,action,resource,resource_id,detail,ip,user_agent,
  request_id,source,before_state,after_state,client_request_id,created_at)
SELECT audit.org_id,audit.user_id,audit.action,audit.resource,audit.resource_id,
       audit.detail || jsonb_build_object(
         'receipt_id',$2::bigint,
         'server_operation_id',receipt.server_operation_id),
       audit.ip,audit.user_agent,$3,audit.source,audit.before_state,
       audit.after_state,audit.client_request_id,transaction_timestamp()
  FROM model_deployment_lifecycle_receipts receipt
  JOIN model_deployment_lifecycle_receipts source ON source.receipt_id=$1
  JOIN audit_logs audit ON audit.id=source.audit_log_id
 WHERE receipt.receipt_id=$2
RETURNING id`, sourceReceiptID, receiptID, operationID).Scan(&auditID))
	return auditID
}

func rebindLifecycleReceiptEvents(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	receiptID int64,
	sourceReceiptID int64,
) {
	t.Helper()
	_, err := tx.Exec(ctx, `
ALTER TABLE runtime_change_events DISABLE TRIGGER runtime_change_events_append_only`)
	require.NoError(t, err)
	result, err := tx.Exec(ctx, `
UPDATE runtime_change_events event
   SET detail=jsonb_set(event.detail,'{lifecycle_receipt_id}',to_jsonb($1::bigint),TRUE)
  FROM model_deployment_lifecycle_receipt_releases evidence
 WHERE evidence.receipt_id=$2
   AND evidence.after_version=evidence.before_version+1
   AND event.release_id=evidence.release_id
   AND event.sequence=evidence.after_version
   AND event.detail->>'lifecycle_receipt_id'=$2::text`, receiptID, sourceReceiptID)
	require.NoError(t, err)
	require.Positive(t, result.RowsAffected())
	_, err = tx.Exec(ctx, `
ALTER TABLE runtime_change_events ENABLE TRIGGER runtime_change_events_append_only`)
	require.NoError(t, err)
}

func completeClonedLifecycleReceipt(
	ctx context.Context,
	executor lifecycleReceiptSQLExecutor,
	receiptID int64,
	sourceReceiptID int64,
	auditID int64,
) (pgconn.CommandTag, error) {
	return executor.Exec(ctx, `
UPDATE model_deployment_lifecycle_receipts receipt
   SET target_release_id=source.target_release_id,
       public_model_id=source.public_model_id,
       status='completed',original_http_status=source.original_http_status,
       before_stable_release_id=source.before_stable_release_id,
       before_canary_release_id=source.before_canary_release_id,
       before_canary_basis_points=source.before_canary_basis_points,
       before_slot_version=source.before_slot_version,
       before_slot_public_model_id=source.before_slot_public_model_id,
       after_stable_release_id=source.after_stable_release_id,
       after_canary_release_id=source.after_canary_release_id,
       after_canary_basis_points=source.after_canary_basis_points,
       after_slot_version=source.after_slot_version,
       after_slot_public_model_id=source.after_slot_public_model_id,
       legacy_plan_hash=source.legacy_plan_hash,
       deployment_plan_hash=source.deployment_plan_hash,
       audit_log_id=$3,completed_at=transaction_timestamp()
  FROM model_deployment_lifecycle_receipts source
 WHERE receipt.receipt_id=$1 AND source.receipt_id=$2`, receiptID, sourceReceiptID, auditID)
}
