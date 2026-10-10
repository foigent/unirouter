package migrate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseAccessProvisioningDownSerializesBeforeRepresentabilityChecks(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000059_enterprise_access_provisioning.down.sql"))
	require.NoError(t, err)
	sql := string(body)
	advisoryLock := "SELECT pg_advisory_xact_lock(86420517831702491);"
	tableLock := "LOCK TABLE users, projects, project_members, workload_applications,"
	check := "DO $$"
	require.Equal(t, 1, strings.Count(sql, advisoryLock))
	require.Equal(t, 1, strings.Count(sql, tableLock))
	require.Less(t, strings.Index(sql, advisoryLock), strings.Index(sql, tableLock))
	require.Less(t, strings.Index(sql, tableLock), strings.Index(sql, check))
	require.Less(t, strings.Index(sql, tableLock), strings.Index(sql, "FROM schema_migrations"))
}

func TestEnterpriseAccessProvisioningUpSerializesBeforeTableLocks(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000059_enterprise_access_provisioning.up.sql"))
	require.NoError(t, err)
	sql := string(body)
	begin := "BEGIN;"
	advisoryLock := "SELECT pg_advisory_xact_lock(86420517831702491);"
	tableLock := "LOCK TABLE users, projects, project_members, workload_applications, api_keys"
	require.Equal(t, 1, strings.Count(sql, advisoryLock))
	require.Equal(t, 1, strings.Count(sql, tableLock))
	require.Less(t, strings.Index(sql, begin), strings.Index(sql, advisoryLock))
	require.Less(t, strings.Index(sql, advisoryLock), strings.Index(sql, tableLock))
}

func TestEnterpriseAccessProvisioningSchemaContainsFrozenIdentityBoundaries(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000059_enterprise_access_provisioning.up.sql"))
	require.NoError(t, err)
	sql := string(body)
	require.Contains(t, sql, "UNIQUE (org_id, submitted_by, client_request_id)")
	require.Contains(t, sql, "FOREIGN KEY (subject_workload_application_id, org_id, project_id)")
	require.Contains(t, sql, "FOREIGN KEY (submitted_by) REFERENCES users(id)")
	require.NotContains(t, sql, "FOREIGN KEY (submitted_by, org_id)")
	require.Contains(t, sql, "workload_applications_identity_tuple_immutable")
	require.Contains(t, sql, "UPDATE OF identity_contract_version, contract_version, org_id, project_id, logical_ref, environment, production_use, kind, identity_mode")
	require.Contains(t, sql, "AND logical_ref IS NULL")
	require.Contains(t, sql, "AND environment_locked_at IS NULL")
}
