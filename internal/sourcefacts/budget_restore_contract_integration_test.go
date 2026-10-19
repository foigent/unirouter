//go:build integration

package sourcefacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func budgetCoverageRows(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var rows string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(c) ORDER BY coverage_epoch)::text FROM execution_source_budget_coverage c`).Scan(&rows))
	return rows
}

func TestSourceBudgetRestoreRejectsUnprovedLedger(t *testing.T) {
	helper, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
	require.NoError(t, err)
	for _, fault := range []struct{ name, sql, commitError string }{
		{"future", `INSERT INTO schema_migrations(version) VALUES('000122_unknown_future')`, "requires exact M123 predecessor"},
		{"missing_budget", `DELETE FROM schema_migrations WHERE version='000118_source_budget_snapshots'`, "requires verified support proof"},
		{"missing_prefix", `DELETE FROM schema_migrations WHERE version=(SELECT min(version) FROM schema_migrations)`, "requires verified support proof"},
		{"replaced_prefix", `UPDATE schema_migrations SET version='000001_replaced' WHERE version=(SELECT min(version) FROM schema_migrations)`, "requires verified support proof"},
		{"extra_prefix", `INSERT INTO schema_migrations(version) VALUES('000118_spurious')`, "requires verified support proof"},
		{"empty", `DELETE FROM schema_migrations`, "requires verified support proof"},
	} {
		t.Run(fault.name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			before := budgetCoverageRows(t, pool)
			// Exercise the deferred commit check independently of the restore helper.
			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)
			defer tx.Rollback(context.Background())
			_, err = tx.Exec(t.Context(), fault.sql)
			require.NoError(t, err)
			_, err = tx.Exec(t.Context(), `UPDATE execution_source_budget_coverage SET ended_at=clock_timestamp() WHERE ended_at IS NULL`)
			require.NoError(t, err)
			require.ErrorContains(t, tx.Commit(t.Context()), fault.commitError)
			require.Equal(t, before, budgetCoverageRows(t, pool))
			assertSourceFixtureLedger(t, pool, schemaauthority.LatestSupportedMigration)

			_, err = pool.Exec(t.Context(), fault.sql)
			require.NoError(t, err)
			conn, err := pool.Acquire(t.Context())
			require.NoError(t, err)
			defer conn.Release()
			_, err = conn.Exec(t.Context(), string(helper))
			require.ErrorContains(t, err, "restored budget coverage proof is invalid")
			_, err = conn.Exec(t.Context(), "ROLLBACK")
			require.NoError(t, err)
			require.Equal(t, before, budgetCoverageRows(t, pool))
		})
	}
}

func TestSourceBudgetRestoreRejectsBrokenM121SuffixWithoutWrites(t *testing.T) {
	helper, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
	require.NoError(t, err)
	for _, fault := range []struct{ name, sql string }{
		{"missing_M120", `DELETE FROM schema_migrations WHERE version='000120_unpublished_content_reclamation'`},
		{"missing_M119", `DELETE FROM schema_migrations WHERE version='000119_execution_content_capture'`},
		{"unknown_M122", `INSERT INTO schema_migrations(version) VALUES('000122_unknown_future')`},
	} {
		t.Run(fault.name, func(t *testing.T) {
			pool, _, org, project := setupContentJournal(t)
			proof := budgetCoverageRows(t, pool)
			var epoch int64
			readEpoch := func() int64 {
				var value int64
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&value))
				return value
			}
			epoch = readEpoch()
			_, err := pool.Exec(t.Context(), fault.sql)
			require.NoError(t, err)
			conn, err := pool.Acquire(t.Context())
			require.NoError(t, err)
			defer conn.Release()
			_, err = conn.Exec(t.Context(), string(helper))
			require.ErrorContains(t, err, "restored budget coverage proof is invalid")
			_, err = conn.Exec(t.Context(), "ROLLBACK")
			require.NoError(t, err)
			require.Equal(t, proof, budgetCoverageRows(t, pool))
			require.Equal(t, epoch, readEpoch())
		})
	}
}

func TestSourceBudgetCoverageCommitRejectsWrongProofHash(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	before := budgetCoverageRows(t, pool)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(t.Context(), `UPDATE execution_source_budget_coverage SET ended_at=clock_timestamp() WHERE ended_at IS NULL;
INSERT INTO execution_source_budget_coverage(certified_from,state,reason,writer_contract,schema_head,
 migration_sha256,schema_ledger_sha256,support_manifest_sha256,support_manifest_bytes,predecessor_epoch,restore_boundary)
SELECT ended_at,'certified','restore_rebase',writer_contract,schema_head,migration_sha256,
 repeat('0',64),support_manifest_sha256,support_manifest_bytes,coverage_epoch,ended_at
FROM execution_source_budget_coverage ORDER BY coverage_epoch DESC LIMIT 1`)
	require.NoError(t, err)
	require.ErrorContains(t, tx.Commit(t.Context()), "requires verified support proof")
	require.Equal(t, before, budgetCoverageRows(t, pool))
}

func TestSourceBudgetRestoreRejectsMissingCoverageWithContentLedger(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	require.NoError(t, budgetDown(t, pool))
	_, err := pool.Exec(t.Context(), `INSERT INTO schema_migrations(version) VALUES('000119_execution_content_capture')`)
	require.NoError(t, err)
	helper, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
	require.NoError(t, err)
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(t.Context(), string(helper))
	require.ErrorContains(t, err, "restored D schema is incomplete")
	_, err = conn.Exec(t.Context(), "ROLLBACK")
	require.NoError(t, err)
}
