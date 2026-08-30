//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelDeploymentCreateIdempotencyMigrationIsScopedImmutableAndReversibleWhenEmpty(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000075_model_operation_production_qualification.down.sql")
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('create-receipt-migration','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'create-receipt-owner','fixture','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))

	var receiptID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployment_create_receipts (
  customer_org_id,actor_user_id,actor_org_id,operation_scope,
  idempotency_key_hash,request_fingerprint,server_operation_id,
  created_at,expires_at,updated_at)
VALUES ($1,$2,$1,'model_deployment.create.v1',repeat('a',64),
        'hmac-sha256:'||repeat('b',64),'op_migration_receipt',
        NOW(),NOW()+INTERVAL '24 hours',NOW()) RETURNING id`, orgID, userID).Scan(&receiptID))
	var otherOrgID, otherUserID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('create-receipt-other-tenant','active','customer') RETURNING id`).Scan(&otherOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'create-receipt-other-owner','fixture','org_admin','active','org_admin') RETURNING id`, otherOrgID).Scan(&otherUserID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_create_receipts (
  customer_org_id,actor_user_id,actor_org_id,operation_scope,
  idempotency_key_hash,request_fingerprint,server_operation_id,
  created_at,expires_at,updated_at)
VALUES ($1,$2,$1,'model_deployment.create.v1',repeat('a',64),
        'hmac-sha256:'||repeat('b',64),'op_other_tenant_receipt',
        NOW(),NOW()+INTERVAL '24 hours',NOW())`, otherOrgID, otherUserID)
	require.NoError(t, err, "the same key digest must be isolated by tenant and actor")

	_, err = pool.Exec(ctx, `
UPDATE model_deployment_create_receipts SET idempotency_key_hash=repeat('c',64)
 WHERE id=$1`, receiptID)
	require.ErrorContains(t, err, "receipt identity is immutable")
	_, err = pool.Exec(ctx, `DELETE FROM model_deployment_create_receipts WHERE id=$1`, receiptID)
	require.ErrorContains(t, err, "retained until expiry")

	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000074_model_deployment_create_idempotency.down.sql",
	))
	require.NoError(t, err)
	downWire := string(downSQL)
	advisoryOffset := strings.Index(downWire, "SELECT pg_advisory_xact_lock(86420517831702491);")
	lockOffset := strings.Index(downWire, "LOCK TABLE model_deployment_create_receipts IN ACCESS EXCLUSIVE MODE")
	guardOffset := strings.Index(downWire, "DO $$")
	require.GreaterOrEqual(t, advisoryOffset, 0)
	require.Greater(t, lockOffset, advisoryOffset)
	require.Greater(t, guardOffset, lockOffset, "down must freeze writers before inspecting later migrations or durable receipts")
	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000075_future_contract')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version='000075_future_contract'`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.ErrorContains(t, err, "durable mutation receipts exist")

	// Test-only emptying exposes the ordered down/reapply path; production
	// retention and reset use the reviewed expiry/trigger contract.
	_, err = pool.Exec(ctx, `TRUNCATE model_deployment_create_receipts`)
	require.NoError(t, err)
	execMigrationDown(t, ctx, pool, "000074_model_deployment_create_idempotency.down.sql")
	var tableExists bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT to_regclass('public.model_deployment_create_receipts') IS NOT NULL`).Scan(&tableExists))
	require.False(t, tableExists)
	require.NoError(t, migrate.RunThrough(ctx, pool, testutil.MigrationsDir(t), "000074_model_deployment_create_idempotency"))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT to_regclass('public.model_deployment_create_receipts') IS NOT NULL`).Scan(&tableExists))
	require.True(t, tableExists)
}

func TestModelDeploymentCreateCompletedReceiptRejectsNullResultIdentity(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID, publicModelID, connectionID, deploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('create-receipt-null-state','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'create-receipt-null-owner','fixture','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (
  org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,'receipt-null-model','Receipt Null Model','openai','ga',TRUE) RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config)
VALUES ('receipt-null-connection','openai','https://receipt-null.invalid/v1','openai_compatible','{}')
RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model)
VALUES ($1,$2,$3,'receipt-null-upstream') RETURNING id`, orgID, publicModelID, connectionID).Scan(&deploymentID))
	const operationID = "openai.chat_completions.v1"
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,$2,'enabled','declared','',1,'openai.chat_completions','1.0.0')`, deploymentID, operationID)
	require.NoError(t, err)

	tests := []struct {
		name               string
		deploymentID       any
		modelOperationID   any
		originalHTTPStatus any
	}{
		{name: "deployment id", deploymentID: nil, modelOperationID: operationID, originalHTTPStatus: 201},
		{name: "model operation id", deploymentID: deploymentID, modelOperationID: nil, originalHTTPStatus: 201},
		{name: "original HTTP status", deploymentID: deploymentID, modelOperationID: operationID, originalHTTPStatus: nil},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			keyHash := strings.Repeat(string(rune('1'+index)), 64)
			_, insertErr := pool.Exec(ctx, `
INSERT INTO model_deployment_create_receipts (
  customer_org_id,actor_user_id,actor_org_id,operation_scope,
  idempotency_key_hash,request_fingerprint,server_operation_id,
  deployment_id,model_operation_id,status,original_http_status,
  reused_connection,reused_public_model,reused_deployment,reused_credential,reused_operation,
  created_at,expires_at,completed_at,updated_at)
VALUES ($1,$2,$1,'model_deployment.create.v1',$3,
        'hmac-sha256:'||repeat('c',64),$4,
        $5,$6,'completed',$7,FALSE,FALSE,FALSE,FALSE,FALSE,
        NOW()-INTERVAL '1 minute',NOW()+INTERVAL '24 hours',NOW(),NOW())`,
				orgID, userID, keyHash, "op_null_completed_"+string(rune('1'+index)),
				test.deploymentID, test.modelOperationID, test.originalHTTPStatus,
			)
			require.ErrorContains(t, insertErr, "model_deployment_create_receipts_state_check")
		})
	}
	var receiptCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployment_create_receipts`).Scan(&receiptCount))
	require.Zero(t, receiptCount)
}

func TestModelDeploymentCreateReceiptDownSerializesWithConcurrentWriter(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	execMigrationDown(t, ctx, pool, "000075_model_operation_production_qualification.down.sql")
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('create-receipt-down-race','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'create-receipt-down-writer','fixture','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))

	writer, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer writer.Rollback(ctx)
	_, err = writer.Exec(ctx, `LOCK TABLE model_deployment_create_receipts IN ROW EXCLUSIVE MODE`)
	require.NoError(t, err)

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000074_model_deployment_create_idempotency.down.sql",
	))
	require.NoError(t, err)
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(ctx, string(downSQL))
		downResult <- execErr
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var waiting bool
		require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM pg_locks
   WHERE pid=$1 AND relation='model_deployment_create_receipts'::regclass
     AND mode='AccessExclusiveLock' AND NOT granted
)`, downPID).Scan(&waiting))
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("down migration did not wait for its up-front ACCESS EXCLUSIVE lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	_, err = writer.Exec(ctx, `
INSERT INTO model_deployment_create_receipts (
  customer_org_id,actor_user_id,actor_org_id,operation_scope,
  idempotency_key_hash,request_fingerprint,server_operation_id,
  created_at,expires_at,updated_at)
VALUES ($1,$2,$1,'model_deployment.create.v1',repeat('f',64),
        'hmac-sha256:'||repeat('0',64),'op_concurrent_down_receipt',
        NOW(),NOW()+INTERVAL '24 hours',NOW())`, orgID, userID)
	require.NoError(t, err)
	require.NoError(t, writer.Commit(ctx))

	select {
	case downErr := <-downResult:
		require.ErrorContains(t, downErr, "durable mutation receipts exist")
	case <-time.After(2 * time.Second):
		t.Fatal("down migration did not resume after the concurrent writer committed")
	}
	_, _ = downConn.Exec(ctx, `ROLLBACK`)

	var tableExists bool
	var receiptCount, ledgerCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('public.model_deployment_create_receipts') IS NOT NULL`).Scan(&tableExists))
	require.True(t, tableExists)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployment_create_receipts`).Scan(&receiptCount))
	require.Equal(t, 1, receiptCount)
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM schema_migrations
 WHERE version='000074_model_deployment_create_idempotency'`).Scan(&ledgerCount))
	require.Equal(t, 1, ledgerCount)
}

func TestModelDeploymentCreateReceiptDownSharesMigrationRunnerAdvisoryLock(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	execMigrationDown(t, ctx, pool, "000075_model_operation_production_qualification.down.sql")

	runner, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer runner.Rollback(ctx)
	_, err = runner.Exec(ctx, `SELECT pg_advisory_xact_lock(86420517831702491)`)
	require.NoError(t, err)
	_, err = runner.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000075_future_contract')`)
	require.NoError(t, err)

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer downConn.Release()
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000074_model_deployment_create_idempotency.down.sql",
	))
	require.NoError(t, err)
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(ctx, string(downSQL))
		downResult <- execErr
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var waiting bool
		require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM pg_locks
   WHERE pid=$1 AND locktype='advisory' AND NOT granted
)`, downPID).Scan(&waiting))
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("down migration did not wait for the shared migration-runner advisory lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	require.NoError(t, runner.Commit(ctx))

	select {
	case downErr := <-downResult:
		require.ErrorContains(t, downErr, "later migrations must be reversed first")
	case <-time.After(2 * time.Second):
		t.Fatal("down migration did not resume after the migration runner released its advisory lock")
	}
	_, _ = downConn.Exec(ctx, `ROLLBACK`)

	var futureApplied, tableExists bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version='000075_future_contract'),
       to_regclass('public.model_deployment_create_receipts') IS NOT NULL`).Scan(&futureApplied, &tableExists))
	require.True(t, futureApplied)
	require.True(t, tableExists)
}

func TestModelDeploymentCreateReceiptAllowsOnlyExpiredRetentionDelete(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('expired-create-receipt','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,'expired-create-owner','fixture','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_create_receipts (
  customer_org_id,actor_user_id,actor_org_id,operation_scope,
  idempotency_key_hash,request_fingerprint,server_operation_id,
  created_at,expires_at,updated_at)
VALUES ($1,$2,$1,'model_deployment.create.v1',repeat('d',64),
        'hmac-sha256:'||repeat('e',64),'op_expired_receipt',
        NOW()-INTERVAL '25 hours',NOW()-INTERVAL '1 hour',NOW()-INTERVAL '25 hours')`, orgID, userID)
	require.NoError(t, err)
	tag, err := pool.Exec(ctx, `DELETE FROM model_deployment_create_receipts WHERE expires_at < NOW()`)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}
