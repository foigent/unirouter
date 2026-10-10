package migrate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseProjectRoleSeparationDownLocksBeforeRepresentabilityChecks(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000058_enterprise_project_role_separation.down.sql"))
	require.NoError(t, err)
	sql := string(body)
	advisoryLock := "SELECT pg_advisory_xact_lock(86420517831702491);"
	lock := "LOCK TABLE users, project_members IN SHARE ROW EXCLUSIVE MODE;"
	require.Equal(t, 1, strings.Count(sql, advisoryLock), "rollback must join the reviewed migration advisory-lock protocol exactly once")
	require.Equal(t, 1, strings.Count(sql, lock), "rollback must use one reviewed stabilization lock")
	advisoryLockIndex := strings.Index(sql, advisoryLock)
	lockIndex := strings.Index(sql, lock)
	checkIndex := strings.Index(sql, "DO $$")
	require.Greater(t, advisoryLockIndex, strings.Index(sql, "BEGIN;"))
	require.Greater(t, lockIndex, advisoryLockIndex, "table stabilization must begin only after migration lifecycle serialization")
	require.Greater(t, checkIndex, lockIndex, "all representability checks must run after both tables are locked")
	require.Less(t, lockIndex, strings.Index(sql, "FROM schema_migrations"))
	require.Less(t, lockIndex, strings.Index(sql, "FROM users u"))
}
