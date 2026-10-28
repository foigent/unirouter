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
const ContentCaptureExpandHead = "000119_execution_content_capture"
const ContentReclaimExpandHead = "000120_unpublished_content_reclamation"
const PreDispatchExpandHead = "000121_gateway_pre_dispatch_terminal"
const ScopedContentExpandHead = "000122_scoped_content_policy"
const XInsightPolicyExpandHead = "000123_xinsight_policy_delegation"
const XInsightAdoptionExpandHead = "000124_xinsight_receiver_adoption"
const XInsightCredentialExpandHead = "000125_xinsight_control_credential_lifecycle"
const CommercialLicenseRetirementHead = "000126_commercial_license_retirement"
const XInsightCommunicationHead = "000127_xinsight_communication_policy"
const UpstreamReleaseObservationHead = "000128_upstream_release_observation"
const LatestSupportedMigration = UpstreamReleaseObservationHead

func SupportedSchemaHeads() []string {
	return []string{LegacyCapabilityHead, CapabilityExpandHead, StreamCounterExpandHead, SourceFactsExpandHead, ObservationExpandHead, BudgetSnapshotExpandHead, ContentCaptureExpandHead, ContentReclaimExpandHead, PreDispatchExpandHead, ScopedContentExpandHead, XInsightPolicyExpandHead, XInsightAdoptionExpandHead, XInsightCredentialExpandHead, CommercialLicenseRetirementHead, XInsightCommunicationHead, UpstreamReleaseObservationHead}
}
