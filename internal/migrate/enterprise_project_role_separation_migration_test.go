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
	body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000058_enterprise_project_role_separation.down.sql"))
	require.NoError(t, err)
	sql := string(body)
	lock := "LOCK TABLE users, project_members IN SHARE ROW EXCLUSIVE MODE;"
	require.Equal(t, 1, strings.Count(sql, lock), "rollback must use one reviewed stabilization lock")
	lockIndex := strings.Index(sql, lock)
	checkIndex := strings.Index(sql, "DO $$")
	require.Greater(t, lockIndex, strings.Index(sql, "BEGIN;"))
	require.Greater(t, checkIndex, lockIndex, "all representability checks must run after both tables are locked")
	require.Less(t, lockIndex, strings.Index(sql, "FROM schema_migrations"))
	require.Less(t, lockIndex, strings.Index(sql, "FROM users u"))
}
