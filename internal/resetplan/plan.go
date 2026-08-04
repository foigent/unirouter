// Package resetplan builds a read-only, fail-closed inventory for a future
// operator-controlled product-data reset. It deliberately contains no SQL or
// API capable of deleting data.
package resetplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

const ContractVersion = "unirouter.reset-preview.v1"

const (
	ScopeResetCandidate = "reset_candidate"
	ScopeProtected      = "protected"
	ScopeSchemaLedger   = "schema_ledger"
	ScopeReviewRequired = "review_required"
)

var (
	ErrInvalid = errors.New("resetplan: invalid preview")

	sourceInstancePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	protectedTables       = map[string]string{
		"commercial_license_entitlements": "commercial release authority is deployment-scoped and is not business test data",
		"deployment_profiles":             "deployment desired state is preserved across a product-data reset",
		"gateway_endpoints":               "gateway network and TLS references are preserved across a product-data reset",
		"release_history":                 "installed release provenance must remain append-only",
		"release_state":                   "installed release and rollback protection state must remain intact",
		"settings":                        "deployment and security settings require a separate explicit review",
	}
	resetCandidateTables = stringSet(
		"admin_sessions", "api_keys", "async_media_charge_facts", "async_media_jobs",
		"audit_logs", "billing_provider_attempts", "billing_reservation_extensions",
		"billing_reservation_grant_allocations", "call_logs", "channel_keys", "channels",
		"completed_execution_feed_connections", "completed_execution_feed_items",
		"completed_execution_feed_states", "delivery_attempt_quarantine",
		"distributed_limit_policies", "enterprise_directory_account_snapshots",
		"enterprise_directory_iam_actions", "enterprise_directory_person_snapshots",
		"enterprise_directory_snapshots", "enterprise_directory_sources",
		"enterprise_directory_unit_snapshots", "enterprise_finance_adjustment_journal_versions",
		"enterprise_finance_allocation_results", "enterprise_finance_allocation_runs",
		"enterprise_finance_budget_versions", "enterprise_finance_cost_center_versions",
		"enterprise_finance_fiscal_periods", "enterprise_finance_period_events",
		"enterprise_finance_pricing_bindings", "enterprise_finance_pricing_snapshots",
		"enterprise_finance_rate_card_versions", "enterprise_finance_rate_components",
		"freeze_idempotency", "grants", "guardrail_advisory_gap_batches",
		"guardrail_advisory_gap_receipts", "guardrail_policies", "integration_exchange_deliveries",
		"integration_exchange_items", "integration_exchange_members", "integration_exchange_receipts",
		"integration_exchanges", "internal_outbox_events", "invocation_optimization_policies",
		"invocation_optimization_policy_revisions", "invocation_optimization_receipts",
		"micro_ledger_runtime_state",
		"model_aliases", "model_call_authorizations", "model_prices", "model_registry",
		"model_set_items", "model_sets", "notification_deliveries", "notification_events",
		"notification_webhooks", "org_model_prices", "orgs", "project_budget_usage",
		"project_members", "project_model_sets", "projects", "provider_cost_reconciliations",
		"request_delivery_receipt_quarantine", "request_delivery_receipts", "request_idempotency",
		"request_trace_events", "response_cache_commercial_receipts",
		"response_cache_delivery_receipts", "runtime_change_events", "runtime_change_releases",
		"runtime_change_slots", "runtime_terminal_hooks", "stream_delivery_checkpoints",
		"stream_delivery_leases", "transactions", "usage_finalization_journal", "users",
		"wallets", "workload_applications",
	)
)

type TableInventory struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
}

type TablePlan struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
	Scope    string `json:"scope"`
	Reason   string `json:"reason"`
}

type Summary struct {
	TableCount          int   `json:"table_count"`
	RowCount            int64 `json:"row_count"`
	ResetCandidateCount int   `json:"reset_candidate_count"`
	ProtectedCount      int   `json:"protected_count"`
	SchemaLedgerCount   int   `json:"schema_ledger_count"`
	ReviewRequiredCount int   `json:"review_required_count"`
}

type Plan struct {
	ContractVersion        string      `json:"contract_version"`
	GeneratedAt            string      `json:"generated_at"`
	Mode                   string      `json:"mode"`
	Profile                string      `json:"profile"`
	CurrentSourceInstance  string      `json:"current_source_instance,omitempty"`
	NextSourceInstance     string      `json:"next_source_instance"`
	SourceIdentityAction   string      `json:"source_identity_action"`
	ActionAuthorized       bool        `json:"action_authorized"`
	ApplySupported         bool        `json:"apply_supported"`
	MutationPerformed      bool        `json:"mutation_performed"`
	ScopeComplete          bool        `json:"scope_complete"`
	Summary                Summary     `json:"summary"`
	Tables                 []TablePlan `json:"tables"`
	ExecutionPreconditions []string    `json:"execution_preconditions"`
	PlanSHA256             string      `json:"plan_sha256"`
}

func Build(inventory []TableInventory, currentSourceInstance, nextSourceInstance string, now time.Time) (Plan, error) {
	currentSourceInstance = strings.TrimSpace(currentSourceInstance)
	nextSourceInstance = strings.TrimSpace(nextSourceInstance)
	if !sourceInstancePattern.MatchString(nextSourceInstance) {
		return Plan{}, fmt.Errorf("%w: next_source_instance", ErrInvalid)
	}
	if currentSourceInstance != "" {
		if !sourceInstancePattern.MatchString(currentSourceInstance) {
			return Plan{}, fmt.Errorf("%w: current_source_instance", ErrInvalid)
		}
		if currentSourceInstance == nextSourceInstance {
			return Plan{}, fmt.Errorf("%w: next_source_instance_must_change", ErrInvalid)
		}
	}

	ordered := slices.Clone(inventory)
	slices.SortFunc(ordered, func(left, right TableInventory) int { return strings.Compare(left.Name, right.Name) })
	plan := Plan{
		ContractVersion:       ContractVersion,
		GeneratedAt:           now.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"),
		Mode:                  "dry_run_read_only",
		Profile:               "second_integration_product_data",
		CurrentSourceInstance: currentSourceInstance,
		NextSourceInstance:    nextSourceInstance,
		SourceIdentityAction:  "mint_and_configure_before_any_reset",
		ActionAuthorized:      false,
		ApplySupported:        false,
		MutationPerformed:     false,
		ExecutionPreconditions: []string{
			"explicit_user_reset_authorization",
			"quiesced_model_and_connector_traffic",
			"verified_database_backup",
			"backup_sha256_recorded",
			"exact_delete_order_and_bootstrap_plan_reviewed",
			"new_source_instance_configured",
			"response_cache_scope_purged_or_index_key_rotated",
			"cursor_and_checkpoint_epoch_rebound_without_reuse",
			"post_reset_migration_bootstrap_and_readback",
		},
	}
	seen := make(map[string]struct{}, len(ordered))
	for _, table := range ordered {
		name := strings.TrimSpace(table.Name)
		if name == "" || table.RowCount < 0 {
			return Plan{}, fmt.Errorf("%w: table_inventory", ErrInvalid)
		}
		if _, exists := seen[name]; exists {
			return Plan{}, fmt.Errorf("%w: duplicate_table_%s", ErrInvalid, name)
		}
		seen[name] = struct{}{}
		scope, reason := ClassifyTable(name)
		entry := TablePlan{Name: name, RowCount: table.RowCount, Scope: scope, Reason: reason}
		plan.Tables = append(plan.Tables, entry)
		plan.Summary.TableCount++
		plan.Summary.RowCount += table.RowCount
		switch scope {
		case ScopeResetCandidate:
			plan.Summary.ResetCandidateCount++
		case ScopeProtected:
			plan.Summary.ProtectedCount++
		case ScopeSchemaLedger:
			plan.Summary.SchemaLedgerCount++
		default:
			plan.Summary.ReviewRequiredCount++
		}
	}
	plan.ScopeComplete = plan.Summary.ReviewRequiredCount == 0
	digestInput := plan
	digestInput.PlanSHA256 = ""
	encoded, err := json.Marshal(digestInput)
	if err != nil {
		return Plan{}, fmt.Errorf("encode digest input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	plan.PlanSHA256 = "sha256:" + hex.EncodeToString(digest[:])
	return plan, nil
}

func ClassifyTable(name string) (string, string) {
	if name == "schema_migrations" {
		return ScopeSchemaLedger, "migration history is immutable reset infrastructure"
	}
	if reason, ok := protectedTables[name]; ok {
		return ScopeProtected, reason
	}
	if _, ok := resetCandidateTables[name]; ok {
		return ScopeResetCandidate, "product or control-plane data included in the future reset review"
	}
	return ScopeReviewRequired, "unclassified table blocks any future reset until the scope contract is updated"
}

func Marshal(plan Plan) ([]byte, error) {
	return json.MarshalIndent(plan, "", "  ")
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
