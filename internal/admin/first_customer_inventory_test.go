package admin

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/resetplan"
)

func TestFirstCustomerInventoryKeepsContentSeparateFromReset(t *testing.T) {
	content := []string{"execution_content_enterprise_policies", "execution_content_health", "execution_content_manifests", "execution_content_objects", "execution_content_policies", "execution_content_streams", "execution_content_user_policies", "execution_source_content_bindings", "execution_source_content_operations", "execution_source_content_scopes", "xinsight_connector_credential_operations", "xinsight_communication_policies", "xinsight_communication_runs", "xinsight_receiver_adoptions", "xinsight_machine_credentials", "xinsight_policy_delegations", "xinsight_policy_operations", "xinsight_source_enrollment_bindings", "xinsight_source_enrollment_exclusions", "xinsight_source_enrollments"}
	require.Equal(t, content, firstCustomerContentTables())
	reset := resetplan.ExpectedPublicTables()
	require.Len(t, reset, 140)
	expected := append(slices.Clone(reset), content...)
	slices.Sort(expected)
	actual := firstCustomerExpectedPublicTables()
	require.Equal(t, expected, actual)
	require.Len(t, slices.Compact(slices.Clone(actual)), len(reset)+len(content))
	zero := firstCustomerZeroTables()
	require.Equal(t, zero, slices.Compact(slices.Clone(zero)))
	for _, name := range content {
		require.Contains(t, zero, name)
		require.NotContains(t, reset, name)
		require.NotContains(t, resetplan.BootstrapZeroTables(), name)
	}
}

func TestFirstCustomerInventoryCoversCurrentMigrationTables(t *testing.T) {
	matches := regexp.MustCompile(`(?im)CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+([a-z][a-z0-9_]*)`)
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	tables := map[string]bool{"schema_migrations": true}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, match := range matches.FindAllStringSubmatch(string(contents), -1) {
			tables[match[1]] = true
		}
	}
	actual := make([]string, 0, len(tables))
	for name := range tables {
		actual = append(actual, name)
	}
	slices.Sort(actual)
	require.Equal(t, actual, firstCustomerExpectedPublicTables())
}
