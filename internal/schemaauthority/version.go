// Package schemaauthority owns the exact schema identity supported by the
// current binary's lifecycle gates. It deliberately contains no migration
// runner or product-domain policy.
package schemaauthority

const LatestSupportedMigration = "000103_minimax_m3_glm53_function_tools_qualification"
