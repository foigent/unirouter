// Package schemaauthority owns the exact schema identity supported by the
// current binary's lifecycle gates. It deliberately contains no migration
// runner or product-domain policy.
package schemaauthority

const LatestSupportedMigration = "000084_official_rate_source_currency_drafts"
