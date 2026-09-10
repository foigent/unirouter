// Package schemaauthority owns the exact schema identity supported by the
// current binary's lifecycle gates. It deliberately contains no migration
// runner or product-domain policy.
package schemaauthority

const LegacyCapabilityHead = "000113_glm_stream_function_tools_qualification"
const CapabilityExpandHead = "000114_capability_delivery_expand"
const StreamCounterExpandHead = "000115_stream_counter_version"
const SourceFactsExpandHead = "000116_execution_source_facts"
const ObservationExpandHead = "000117_execution_observations"
const BudgetSnapshotExpandHead = "000118_source_budget_snapshots"
const LatestSupportedMigration = BudgetSnapshotExpandHead

func SupportedSchemaHeads() []string {
	return []string{LegacyCapabilityHead, CapabilityExpandHead, StreamCounterExpandHead, SourceFactsExpandHead, ObservationExpandHead, BudgetSnapshotExpandHead}
}
