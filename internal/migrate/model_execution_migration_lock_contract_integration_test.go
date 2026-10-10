//go:build integration

package migrate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelExecutionGuardedDownsSerializeWithFutureMigrationRunner(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	ctx := context.Background()

	// Put 72 at the rollback frontier. The child rollback helper also removes
	// the empty 74 receipt contract before 73.
	execMigrationDown(t, ctx, pool, "000073_model_execution_dispatch_fence.down.sql")

	tests := []struct {
		version      string
		downFile     string
		statePresent string
	}{
		{
			version:      "000072_execution_evidence_projection",
			downFile:     "000072_execution_evidence_projection.down.sql",
			statePresent: `EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='call_logs' AND column_name='route_evidence_source')`,
		},
		{
			version:      "000071_model_execution_evidence",
			downFile:     "000071_model_execution_evidence.down.sql",
			statePresent: `EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='billing_provider_attempts' AND column_name='route_evidence_version')`,
		},
		{
			version:      "000070_runtime_deployment_route_manifest",
			downFile:     "000070_runtime_deployment_route_manifest.down.sql",
			statePresent: `EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='runtime_change_releases' AND column_name='public_model_id')`,
		},
		{
			version:      "000069_model_deployment_expand",
			downFile:     "000069_model_deployment_expand.down.sql",
			statePresent: `to_regclass('public.provider_connections') IS NOT NULL`,
		},
	}

	for _, test := range tests {
		if !t.Run(test.version, func(t *testing.T) {
			assertFutureMigrationRunnerWinsBeforeGuardedDown(t, pool, test.downFile, test.version, test.statePresent)
		}) {
			return
		}
		execMigrationDown(t, ctx, pool, test.downFile)
	}
}

func assertFutureMigrationRunnerWinsBeforeGuardedDown(
	t *testing.T,
	pool *pgxpool.Pool,
	downFile string,
	version string,
	statePresentSQL string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	blocker, err := pool.Acquire(ctx)
	require.NoError(t, err)
	blockerOpen := true
	t.Cleanup(func() {
		if blockerOpen {
			_, _ = blocker.Exec(context.Background(), `ROLLBACK`)
		}
		blocker.Release()
	})
	var blockerPID int
	require.NoError(t, blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
	_, err = blocker.Exec(ctx, `BEGIN; LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)

	applicationName := "model-execution-lock-" + version[:6]
	runnerPool := modelExecutionMigrationRunnerPool(t, ctx, pool, applicationName)
	futureDir := t.TempDir()
	const futureVersion = "000075_model_execution_lock_contract"
	require.NoError(t, os.WriteFile(
		filepath.Join(futureDir, futureVersion+".up.sql"),
		[]byte("BEGIN;\nSELECT 1;\nCOMMIT;\n"),
		0o600,
	))
	runnerResult := make(chan error, 1)
	go func() {
		runnerResult <- migrate.Run(ctx, runnerPool, futureDir)
	}()
	runnerPID := waitForApplicationBlockedBy(t, pool, applicationName, blockerPID)
	require.True(t, backendHoldsMigrationAdvisoryLock(t, pool, runnerPID),
		"migrate.Run must hold the shared session lock while schema discovery is blocked")

	downConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(downConn.Release)
	var downPID int
	require.NoError(t, downConn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&downPID))
	downWire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), downFile))
	require.NoError(t, err)
	downResult := make(chan error, 1)
	go func() {
		_, execErr := downConn.Exec(ctx, string(downWire))
		downResult <- execErr
	}()
	waitForBackendBlockedBy(t, pool, downPID, runnerPID)

	_, err = blocker.Exec(ctx, `ROLLBACK`)
	require.NoError(t, err)
	blockerOpen = false

	select {
	case runnerErr := <-runnerResult:
		require.NoError(t, runnerErr)
	case <-ctx.Done():
		t.Fatal("future migration runner did not finish after its schema-ledger blocker was released")
	}
	select {
	case downErr := <-downResult:
		require.ErrorContains(t, downErr, "later migrations must be reversed first")
	case <-ctx.Done():
		t.Fatal("guarded down did not resume after migrate.Run released the shared lifecycle lock")
	}
	_, _ = downConn.Exec(context.Background(), `ROLLBACK`)

	var futureApplied, targetApplied, statePresent bool
	require.NoError(t, pool.QueryRow(ctx, fmt.Sprintf(`
SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1),
       EXISTS(SELECT 1 FROM schema_migrations WHERE version=$2),
       %s`, statePresentSQL), futureVersion, version).Scan(
		&futureApplied, &targetApplied, &statePresent,
	))
	require.True(t, futureApplied, "winning future migration ledger must be preserved")
	require.True(t, targetApplied, "refused down must preserve its own ledger")
	require.True(t, statePresent, "refused down must preserve its schema state")
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version=$1`, futureVersion)
	require.NoError(t, err)
}

func modelExecutionMigrationRunnerPool(
	t *testing.T,
	ctx context.Context,
	source *pgxpool.Pool,
	applicationName string,
) *pgxpool.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	config.MaxConns = 1
	runnerPool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(runnerPool.Close)
	return runnerPool
}
