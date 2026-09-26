package admin

import (
	"slices"

	"github.com/uniroute/unirouter/internal/resetplan"
)

// First-customer bootstrap may accept a current M119 database only while all
// content state is empty. This inventory does not authorize resetting M119;
// reset retains its separately reviewed table inventory and refusal rules.
func firstCustomerContentTables() []string {
	return []string{
		"execution_content_health",
		"execution_content_manifests",
		"execution_content_objects",
		"execution_content_policies",
		"execution_content_streams",
		"execution_source_content_bindings",
		"execution_source_content_operations",
		"execution_source_content_scopes",
	}
}

func firstCustomerExpectedPublicTables() []string {
	tables := append(resetplan.ExpectedPublicTables(), firstCustomerContentTables()...)
	slices.Sort(tables)
	return tables
}

func firstCustomerZeroTables() []string {
	tables := append(resetplan.BootstrapZeroTables(), firstCustomerContentTables()...)
	slices.Sort(tables)
	return tables
}
