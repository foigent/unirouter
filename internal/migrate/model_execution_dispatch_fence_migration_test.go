package migrate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelExecutionDispatchFenceDownLocksBeforeEveryRepresentabilityCheck(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(
		testutil.PreRetirementMigrationsDir(t), "000073_model_execution_dispatch_fence.down.sql",
	))
	require.NoError(t, err)
	sql := string(body)
	begin := strings.Index(sql, "BEGIN;")
	advisoryLock := "SELECT pg_advisory_xact_lock(86420517831702491);"
	advisoryLockIndex := strings.Index(sql, advisoryLock)
	lock := "LOCK TABLE billing_provider_attempts IN ACCESS EXCLUSIVE MODE;"
	lockIndex := strings.Index(sql, lock)
	checkIndex := strings.Index(sql, "DO $$")
	require.Equal(t, 1, strings.Count(sql, advisoryLock))
	require.Equal(t, 1, strings.Count(sql, lock))
	require.Greater(t, advisoryLockIndex, begin)
	require.Less(t, advisoryLockIndex, lockIndex,
		"migration lifecycle serialization must precede table stabilization")
	require.Greater(t, lockIndex, begin)
	require.Less(t, lockIndex, checkIndex)
	require.Less(t, lockIndex, strings.Index(sql, "FROM schema_migrations"))
	require.Less(t, lockIndex, strings.Index(sql, "FROM billing_provider_attempts"))
	require.Empty(t, strings.TrimSpace(sql[begin+len("BEGIN;"):advisoryLockIndex]),
		"the migration advisory lock must be the first rollback statement after BEGIN")
}

func TestModelExecutionDispatchFenceSchemaDeclaresReleasedAttemptAsTerminal(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(
		testutil.MigrationsDir(t), "000073_model_execution_dispatch_fence.up.sql",
	))
	require.NoError(t, err)
	sql := string(body)
	require.Contains(t, sql, "'released_undispatched'")
	require.Contains(t, sql, "'not_dispatched'")
	require.Contains(t, sql, "released-undispatched provider attempt is terminal")
	require.Contains(t, sql, "provider attempt health outcome is out of sequence")
}
