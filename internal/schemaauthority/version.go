// Package schemaauthority owns the exact schema identity supported by the
// current binary's lifecycle gates. It deliberately contains no migration
// runner or product-domain policy.
package schemaauthority

const LegacyCapabilityHead = "000113_glm_stream_function_tools_qualification"
const CapabilityExpandHead = "000114_capability_delivery_expand"
const LatestSupportedMigration = CapabilityExpandHead
