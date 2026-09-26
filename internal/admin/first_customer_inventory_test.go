package admin

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/resetplan"
)

func TestFirstCustomerInventoryKeepsContentSeparateFromReset(t *testing.T) {
	content := []string{"execution_content_health", "execution_content_manifests", "execution_content_objects", "execution_content_policies", "execution_content_streams", "execution_source_content_bindings", "execution_source_content_operations", "execution_source_content_scopes"}
	require.Equal(t, content, firstCustomerContentTables())
	reset := resetplan.ExpectedPublicTables()
	require.Len(t, reset, 140)
	expected := append(slices.Clone(reset), content...)
	slices.Sort(expected)
	actual := firstCustomerExpectedPublicTables()
	require.Equal(t, expected, actual)
	require.Len(t, slices.Compact(slices.Clone(actual)), 148)
	zero := firstCustomerZeroTables()
	require.Equal(t, zero, slices.Compact(slices.Clone(zero)))
	for _, name := range content {
		require.Contains(t, zero, name)
		require.NotContains(t, reset, name)
		require.NotContains(t, resetplan.BootstrapZeroTables(), name)
	}
}
