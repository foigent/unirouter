package admin

import (
	"slices"

	"github.com/uniroute/unirouter/internal/resetplan"
)

// First-customer bootstrap may accept the current schema only while all
// content and X-Insight machine authority state is empty. This inventory does
// not authorize resetting those tables; reset retains its separately reviewed
// table inventory and refusal rules.
func firstCustomerContentTables() []string {
	return []string{
		"execution_content_enterprise_policies",
		"execution_content_health",
		"execution_content_manifests",
		"execution_content_objects",
		"execution_content_policies",
		"execution_content_streams",
		"execution_content_user_policies",
		"execution_source_content_bindings",
		"execution_source_content_operations",
		"execution_source_content_scopes",
		"xinsight_machine_credentials",
		"xinsight_policy_delegations",
		"xinsight_policy_operations",
		"xinsight_source_enrollment_bindings",
		"xinsight_source_enrollment_exclusions",
		"xinsight_source_enrollments",
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
