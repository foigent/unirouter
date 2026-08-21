// Package resetplan defines the fail-closed, reviewable contract shared by the
// second-integration reset preview and apply paths. It contains no database or
// Redis mutation code.
package resetplan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/schemaauthority"
)

const (
	ContractVersion = "unirouter.second-integration-reset.v3"
	Profile         = "second_integration_fresh_customer"
	PreservedLogin  = "admin"
)

const (
	ScopeResetCandidate   = "reset_candidate"
	ScopeProtected        = "protected"
	ScopeProtectedPartial = "protected_partial"
	ScopeSchemaLedger     = "schema_ledger"
	ScopeReviewRequired   = "review_required"
)

const (
	ActionDeleteAll                 = "delete_all"
	ActionDeleteExceptSelectedAdmin = "delete_others_and_normalize_selected_admin"
	ActionDeleteExceptSelectedOrg   = "delete_others_and_normalize_selected_admin_org"
	ActionResetSettingsBaseline     = "delete_nonplatform_settings_and_reset_required_defaults"
	ActionPreserveAll               = "preserve_all"
)

var (
	ErrInvalid = errors.New("resetplan: invalid plan")

	sourceInstancePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	sha256Pattern         = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	gitObjectPattern      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	identifierPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

	protectedTables = map[string]string{
		"commercial_license_entitlements":                      "verified commercial entitlement is deployment-scoped operability state",
		"deployment_profiles":                                  "desired deployment topology is host-operability state",
		"gateway_endpoints":                                    "gateway and TLS references are host-operability state",
		"micro_ledger_runtime_state":                           "database-owned ledger unit-contract and downgrade-safety marker must remain intact",
		"model_operation_execution_support_bindings":           "migration-owned compiled execution support must remain exact",
		"model_operation_public_execution_contract_extensions": "migration-owned public delivery, charge and idempotency support must remain exact",
		"release_history":                                      "installed release provenance remains append-only",
		"release_state":                                        "installed release and rollback state remains intact",
	}
	resetCandidateTables = stringSet(
		"access_credentials", "access_grant_revisions", "access_grants", "access_operations", "access_requests",
		"admin_sessions", "api_keys", "async_media_charge_facts", "async_media_jobs",
		"async_media_limit_reservations",
		"audit_logs", "billing_provider_attempts", "billing_reservation_extensions",
		"billing_observation_receipts",
		"billing_reservation_grant_allocations", "call_logs", "channel_model_probes", "channel_keys", "channels",
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
		"enterprise_finance_rate_source_evidence",
		"freeze_idempotency", "grants", "guardrail_advisory_gap_batches",
		"guardrail_advisory_gap_receipts", "guardrail_policies", "integration_exchange_deliveries",
		"integration_exchange_items", "integration_exchange_members", "integration_exchange_receipts",
		"integration_exchanges", "internal_outbox_events", "invocation_optimization_policies",
		"invocation_optimization_policy_revisions", "invocation_optimization_receipts",
		"model_aliases", "model_call_authorizations",
		"model_deployment_create_receipts",
		"model_deployment_lifecycle_receipt_qualifications",
		"model_deployment_lifecycle_receipt_releases",
		"model_deployment_lifecycle_receipts",
		"model_deployment_release_authoring_receipts",
		"model_deployment_capabilities", "model_deployment_operations", "model_deployment_probes",
		"model_operation_production_approvals",
		"model_operation_public_execution_bindings",
		"model_deployments", "model_prices", "model_registry", "model_set_items", "model_sets",
		"notification_deliveries", "notification_events", "notification_webhooks",
		"org_model_prices", "orgs", "project_budget_usage", "project_members",
		"project_model_sets", "projects", "provider_cost_reconciliations",
		"request_delivery_receipt_quarantine", "request_delivery_receipts",
		"provider_connections", "provider_credential_bindings", "credential_binding_deployments",
		"request_idempotency", "request_trace_events", "response_cache_commercial_receipts",
		"response_cache_delivery_receipts", "runtime_change_events", "runtime_change_releases",
		"runtime_change_slots", "runtime_route_revision_items", "runtime_terminal_hooks", "stream_delivery_checkpoints",
		"stream_delivery_leases", "transactions", "usage_finalization_journal", "users",
		"wallets", "workload_application_client_profiles", "workload_applications",
		"xinsight_connector_control_bindings", "xinsight_connector_installations",
		"xinsight_connector_operations", "xinsight_connector_phase_operations",
	)
	protectedSettingKeys     = []string{"ledger.amount_scale", "ledger.version", "license.offline"}
	platformBaselineSettings = []string{"sensitive_words"}
	deferredCycleConstraints = []string{
		"access_requests_target_grant_scope_fk",
		"billing_observation_receipt_reservation_fk",
		"billing_provider_attempts_reservation_fk",
		"freeze_idempotency_delivery_attempt_provider_fk",
		"freeze_idempotency_observation_receipt_fk",
	}
	resetDeleteTriggers = []TriggerRef{
		{Table: "access_grant_revisions", Name: "access_grant_revisions_append_only"},
		{Table: "access_operations", Name: "access_operations_append_only"},
		{Table: "billing_provider_attempts", Name: "billing_provider_attempts_micro_ledger_evidence"},
		{Table: "billing_observation_receipts", Name: "billing_observation_receipt_append_only"},
		{Table: "billing_reservation_extensions", Name: "billing_reservation_extension_append_only"},
		{Table: "billing_reservation_extensions", Name: "billing_reservation_extensions_micro_ledger_evidence"},
		{Table: "billing_reservation_grant_allocations", Name: "billing_reservation_grants_micro_ledger_evidence"},
		{Table: "call_logs", Name: "call_logs_micro_ledger_evidence"},
		{Table: "provider_connections", Name: "provider_connections_identity_immutable"},
		{Table: "model_deployments", Name: "model_deployments_identity_immutable"},
		{Table: "model_deployment_create_receipts", Name: "model_deployment_create_receipts_immutable"},
		{Table: "model_deployment_lifecycle_receipt_qualifications", Name: "model_deployment_lifecycle_receipt_qualifications_append_only"},
		{Table: "model_deployment_lifecycle_receipt_releases", Name: "model_deployment_lifecycle_receipt_releases_append_only"},
		{Table: "model_deployment_lifecycle_receipts", Name: "model_deployment_lifecycle_receipts_immutable"},
		{Table: "model_deployment_release_authoring_receipts", Name: "model_deployment_release_authoring_receipts_immutable"},
		{Table: "runtime_change_events", Name: "runtime_change_events_append_only"},
		{Table: "model_operation_production_approvals", Name: "model_operation_production_approvals_append_only"},
		{Table: "model_operation_public_execution_bindings", Name: "model_operation_public_execution_bindings_append_only"},
		{Table: "provider_credential_bindings", Name: "provider_credential_bindings_identity_immutable"},
		{Table: "model_deployment_probes", Name: "model_deployment_probes_append_only"},
		{Table: "runtime_route_revision_items", Name: "runtime_route_revision_items_seal_guard"},
		{Table: "runtime_route_revision_items", Name: "runtime_route_revision_items_p3_qualification_guard"},
		{Table: "freeze_idempotency", Name: "freeze_idempotency_micro_ledger_evidence"},
		{Table: "grants", Name: "grants_micro_ledger_evidence"},
		{Table: "invocation_optimization_receipts", Name: "invocation_optimization_receipts_append_only"},
		{Table: "project_budget_usage", Name: "project_budget_usage_micro_ledger_evidence"},
		{Table: "request_delivery_receipts", Name: "request_delivery_receipt_append_only"},
		{Table: "response_cache_commercial_receipts", Name: "response_cache_commercial_receipts_append_only"},
		{Table: "response_cache_delivery_receipts", Name: "response_cache_delivery_receipts_append_only"},
		{Table: "runtime_terminal_hooks", Name: "runtime_terminal_hooks_micro_ledger_evidence"},
		{Table: "stream_delivery_checkpoints", Name: "stream_delivery_checkpoint_append_only"},
		{Table: "transactions", Name: "transactions_micro_ledger_evidence"},
		{Table: "usage_finalization_journal", Name: "usage_journal_micro_ledger_evidence"},
		{Table: "wallets", Name: "wallets_micro_ledger_evidence"},
	}
	legacyRoleCompatibilityTrigger = TriggerRef{
		Table: "users", Name: "users_legacy_role_organization_role_compatibility",
	}
	legacyRoleCompatibilityFunction       = "sync_users_organization_role_from_legacy_role_write"
	projectMemberRoleCompatibilityTrigger = TriggerRef{
		Table: "project_members", Name: "project_members_legacy_role_compatibility",
	}
	projectMemberRoleCompatibilityFunction = "normalize_project_members_role_from_legacy_write"
	workloadEnvironmentImmutableTrigger    = TriggerRef{
		Table: "workload_applications", Name: "workload_applications_identity_tuple_immutable",
	}
	workloadEnvironmentImmutableFunction      = "preserve_workload_identity_tuple"
	workloadClientProfileCompatibilityTrigger = TriggerRef{
		Table: "workload_applications", Name: "workload_applications_legacy_client_profile_compatibility",
	}
	workloadClientProfileCompatibilityFunction = "sync_workload_primary_client_profile_from_legacy_write"
	redisKeyPrefixes                           = []string{
		"limit:{",
		"rl:",
		"uniroute:response-cache:v2:",
		"ur:agentcap:v2:",
		"wallet:v2:",
		"workload:{admission-v1}:",
	}
)

type TableInventory struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
}

type TablePlan struct {
	Name                 string `json:"name"`
	RowCount             int64  `json:"row_count"`
	Scope                string `json:"scope"`
	Action               string `json:"action"`
	Reason               string `json:"reason"`
	ExpectedDeletedRows  int64  `json:"expected_deleted_rows"`
	ExpectedRetainedRows int64  `json:"expected_retained_rows"`
}

type DeleteStep struct {
	Position             int    `json:"position"`
	Table                string `json:"table"`
	Action               string `json:"action"`
	ExpectedDeletedRows  int64  `json:"expected_deleted_rows"`
	ExpectedRetainedRows int64  `json:"expected_retained_rows"`
}

type TargetIdentity struct {
	RuntimeVersion string `json:"runtime_version"`
	RuntimeCommit  string `json:"runtime_commit"`
	SourceTree     string `json:"source_tree"`
}

type AdminSelection struct {
	UserID                int64  `json:"user_id"`
	OrganizationID        int64  `json:"organization_id"`
	NormalizedLogin       string `json:"normalized_login"`
	OrganizationRole      string `json:"organization_role"`
	Role                  string `json:"role"`
	Status                string `json:"status"`
	AuthProvider          string `json:"auth_provider"`
	AuthEpoch             int64  `json:"auth_epoch"`
	OrganizationStatus    string `json:"organization_status"`
	UserRowSHA256         string `json:"user_row_sha256"`
	OrganizationRowSHA256 string `json:"organization_row_sha256"`
	PasswordSHA256        string `json:"password_verifier_sha256"`
	PasswordVerifierValid bool   `json:"password_verifier_valid"`
	EmailPresent          bool   `json:"email_present"`
	OIDCIdentityPresent   bool   `json:"oidc_identity_present"`
	OrganizationType      string `json:"organization_type"`
	OrganizationEmpty     bool   `json:"organization_settings_empty"`
}

type ContentDigest struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
	SHA256   string `json:"sha256"`
}

type ProtectedState struct {
	Tables      []ContentDigest `json:"tables"`
	SettingKeys []string        `json:"setting_keys"`
	Settings    ContentDigest   `json:"settings"`
}

type SettingReset struct {
	Key               string `json:"key"`
	CurrentRowSHA256  string `json:"current_row_sha256"`
	TargetValueSHA256 string `json:"target_value_sha256"`
	AlreadyAtTarget   bool   `json:"already_at_target"`
}

type SequenceState struct {
	Name             string `json:"name"`
	OwnedTable       string `json:"owned_table"`
	OwnedColumn      string `json:"owned_column"`
	IncrementBy      int64  `json:"increment_by"`
	Cycle            bool   `json:"cycle"`
	LastValue        int64  `json:"last_value"`
	IsCalled         bool   `json:"is_called"`
	NextValue        int64  `json:"next_value"`
	MaxObservedValue int64  `json:"max_observed_value"`
}

type ForeignKey struct {
	Name              string `json:"name"`
	ChildTable        string `json:"child_table"`
	ParentTable       string `json:"parent_table"`
	Definition        string `json:"definition"`
	DeleteAction      string `json:"delete_action"`
	Deferrable        bool   `json:"deferrable"`
	InitiallyDeferred bool   `json:"initially_deferred"`
}

type TriggerState struct {
	Table      string `json:"table"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
	Enabled    string `json:"enabled"`
}

type TriggerRef struct {
	Table string `json:"table"`
	Name  string `json:"name"`
}

type SchemaEvidence struct {
	MigrationVersions []string `json:"migration_versions"`
	MigrationSHA256   string   `json:"migration_sha256"`
	ForeignKeyCount   int      `json:"foreign_key_count"`
	ForeignKeySHA256  string   `json:"foreign_key_sha256"`
	TriggerCount      int      `json:"trigger_count"`
	TriggerSHA256     string   `json:"trigger_sha256"`
	SequenceCount     int      `json:"sequence_count"`
	SequenceSHA256    string   `json:"sequence_sha256"`
}

type RedisPrefixObservation struct {
	Prefix   string `json:"prefix"`
	KeyCount int64  `json:"key_count"`
}

type RedisObservation struct {
	BoundToScope bool                     `json:"bound_to_scope"`
	Prefixes     []RedisPrefixObservation `json:"prefixes"`
}

type Snapshot struct {
	Tables                    []TableInventory
	Admin                     AdminSelection
	Protected                 ProtectedState
	SettingResets             []SettingReset
	MigrationVersions         []string
	ExpectedMigrationVersions []string
	Sequences                 []SequenceState
	ForeignKeys               []ForeignKey
	Triggers                  []TriggerState
	Redis                     RedisObservation
}

type Summary struct {
	TableCount            int   `json:"table_count"`
	RowCount              int64 `json:"row_count"`
	ResetCandidateCount   int   `json:"reset_candidate_count"`
	ProtectedCount        int   `json:"protected_count"`
	ProtectedPartialCount int   `json:"protected_partial_count"`
	SchemaLedgerCount     int   `json:"schema_ledger_count"`
	ReviewRequiredCount   int   `json:"review_required_count"`
	ExpectedDeletedRows   int64 `json:"expected_deleted_rows"`
	ExpectedRetainedRows  int64 `json:"expected_retained_rows"`
}

type Plan struct {
	ContractVersion        string           `json:"contract_version"`
	GeneratedAt            string           `json:"generated_at"`
	Mode                   string           `json:"mode"`
	Profile                string           `json:"profile"`
	Target                 TargetIdentity   `json:"target"`
	CurrentSourceInstance  string           `json:"current_source_instance"`
	NextSourceInstance     string           `json:"next_source_instance"`
	SourceIdentityAction   string           `json:"source_identity_action"`
	SelectedAdmin          AdminSelection   `json:"selected_admin"`
	Protected              ProtectedState   `json:"protected"`
	SettingResets          []SettingReset   `json:"required_setting_resets"`
	Schema                 SchemaEvidence   `json:"schema"`
	Sequences              []SequenceState  `json:"sequence_invariants"`
	Summary                Summary          `json:"summary"`
	Tables                 []TablePlan      `json:"tables"`
	DeleteOrder            []DeleteStep     `json:"delete_order"`
	GuardedDeleteTriggers  []TriggerRef     `json:"guarded_delete_triggers"`
	DeferredFKConstraints  []string         `json:"deferred_fk_constraints"`
	ExecutionPreconditions []string         `json:"execution_preconditions"`
	RedisObservation       RedisObservation `json:"redis_observation"`
	ActionAuthorized       bool             `json:"action_authorized"`
	ApplySupported         bool             `json:"apply_supported"`
	MutationPerformed      bool             `json:"mutation_performed"`
	ScopeComplete          bool             `json:"scope_complete"`
	ScopeSHA256            string           `json:"scope_sha256"`
}

func Build(snapshot Snapshot, target TargetIdentity, currentSourceInstance, nextSourceInstance string, now time.Time) (Plan, error) {
	if err := validateTarget(target); err != nil {
		return Plan{}, err
	}
	currentSourceInstance = strings.TrimSpace(currentSourceInstance)
	nextSourceInstance = strings.TrimSpace(nextSourceInstance)
	if !sourceInstancePattern.MatchString(currentSourceInstance) {
		return Plan{}, fmt.Errorf("%w: current_source_instance", ErrInvalid)
	}
	if !sourceInstancePattern.MatchString(nextSourceInstance) {
		return Plan{}, fmt.Errorf("%w: next_source_instance", ErrInvalid)
	}
	if currentSourceInstance == nextSourceInstance {
		return Plan{}, fmt.Errorf("%w: next_source_instance_must_change", ErrInvalid)
	}
	if err := validateAdmin(snapshot.Admin); err != nil {
		return Plan{}, err
	}
	if err := validateProtected(snapshot.Protected); err != nil {
		return Plan{}, err
	}
	settingResets, err := validateAndCanonicalizeSettingResets(snapshot.SettingResets)
	if err != nil {
		return Plan{}, err
	}

	orderedInventory := slices.Clone(snapshot.Tables)
	slices.SortFunc(orderedInventory, func(left, right TableInventory) int { return strings.Compare(left.Name, right.Name) })
	plan := Plan{
		ContractVersion:       ContractVersion,
		GeneratedAt:           now.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"),
		Mode:                  "preview_read_only",
		Profile:               Profile,
		Target:                target,
		CurrentSourceInstance: currentSourceInstance,
		NextSourceInstance:    nextSourceInstance,
		SourceIdentityAction:  "old_epoch_deleted_while_quiesced_then_new_explicit_source_enabled_without_reuse",
		SelectedAdmin:         snapshot.Admin,
		Protected:             canonicalProtected(snapshot.Protected),
		SettingResets:         settingResets,
		Sequences:             canonicalSequences(snapshot.Sequences),
		RedisObservation:      canonicalRedisObservation(snapshot.Redis),
		ActionAuthorized:      false,
		ApplySupported:        true,
		MutationPerformed:     false,
		GuardedDeleteTriggers: RequiredResetDeleteTriggers(),
		DeferredFKConstraints: DeferredCycleConstraints(),
		ExecutionPreconditions: []string{
			"explicit_data_reset_delete_baton_bound_to_final_artifact_sha256_and_scope_sha256",
			"target_runtime_exact_commit_tree_and_terminal_ci_green",
			"tls_certificate_profile_aware_validity_and_renewal_gate_satisfied",
			"model_workers_outbox_feed_and_external_consumers_quiesced",
			"fresh_database_backup_sha256_recorded_and_isolated_restore_verified",
			"final_preview_repeated_after_quiesce_and_backup_without_semantic_drift",
			"independent_xinsight_old_cursor_checkpoint_recorded_without_unirouter_mutation",
			"independent_xinsight_next_source_nonuse_and_epoch_binding_evidence_recorded",
			"apply_runtime_reads_next_source_instance_before_database_delete",
			"redis_product_prefixes_purged_and_verified_before_postgresql_transaction",
			"no_receiver_credential_tenant_wallet_admin_provider_or_application_bootstrap",
			"no_provider_feed_or_test_traffic",
		},
	}

	seen := make(map[string]struct{}, len(orderedInventory))
	tableByName := make(map[string]TablePlan, len(orderedInventory))
	for _, table := range orderedInventory {
		name := strings.TrimSpace(table.Name)
		if !identifierPattern.MatchString(name) || table.RowCount < 0 {
			return Plan{}, fmt.Errorf("%w: table_inventory", ErrInvalid)
		}
		if _, exists := seen[name]; exists {
			return Plan{}, fmt.Errorf("%w: duplicate_table_%s", ErrInvalid, name)
		}
		seen[name] = struct{}{}
		scope, action, reason := ClassifyTable(name)
		entry := TablePlan{Name: name, RowCount: table.RowCount, Scope: scope, Action: action, Reason: reason}
		switch action {
		case ActionDeleteAll:
			entry.ExpectedDeletedRows = table.RowCount
		case ActionDeleteExceptSelectedAdmin, ActionDeleteExceptSelectedOrg:
			if table.RowCount < 1 {
				return Plan{}, fmt.Errorf("%w: %s_missing_preserved_row", ErrInvalid, name)
			}
			entry.ExpectedDeletedRows = table.RowCount - 1
			entry.ExpectedRetainedRows = 1
		case ActionResetSettingsBaseline:
			retained := plan.Protected.Settings.RowCount + int64(len(plan.SettingResets))
			if table.RowCount < retained {
				return Plan{}, fmt.Errorf("%w: settings_protected_count", ErrInvalid)
			}
			entry.ExpectedDeletedRows = table.RowCount - retained
			entry.ExpectedRetainedRows = retained
		case ActionPreserveAll:
			entry.ExpectedRetainedRows = table.RowCount
		}
		plan.Tables = append(plan.Tables, entry)
		tableByName[name] = entry
		plan.Summary.TableCount++
		plan.Summary.RowCount += table.RowCount
		plan.Summary.ExpectedDeletedRows += entry.ExpectedDeletedRows
		plan.Summary.ExpectedRetainedRows += entry.ExpectedRetainedRows
		switch scope {
		case ScopeResetCandidate:
			plan.Summary.ResetCandidateCount++
		case ScopeProtected:
			plan.Summary.ProtectedCount++
		case ScopeProtectedPartial:
			plan.Summary.ProtectedPartialCount++
		case ScopeSchemaLedger:
			plan.Summary.SchemaLedgerCount++
		default:
			plan.Summary.ReviewRequiredCount++
		}
	}

	if err := validateRequiredTables(seen); err != nil {
		return Plan{}, err
	}
	if err := validateResetDeleteTriggers(snapshot.Triggers); err != nil {
		return Plan{}, err
	}
	if err := validateLegacyRoleCompatibilityTrigger(snapshot.Triggers); err != nil {
		return Plan{}, err
	}
	if err := validateProjectMemberRoleCompatibilityTrigger(snapshot.Triggers); err != nil {
		return Plan{}, err
	}
	if err := validateWorkloadIdentityTriggers(snapshot.Triggers); err != nil {
		return Plan{}, err
	}
	order, err := buildDeleteOrder(tableByName, snapshot.ForeignKeys)
	if err != nil {
		return Plan{}, err
	}
	for index, name := range order {
		table := tableByName[name]
		plan.DeleteOrder = append(plan.DeleteOrder, DeleteStep{
			Position: index + 1, Table: name, Action: table.Action,
			ExpectedDeletedRows:  table.ExpectedDeletedRows,
			ExpectedRetainedRows: table.ExpectedRetainedRows,
		})
	}

	plan.Schema, err = buildSchemaEvidence(snapshot)
	if err != nil {
		return Plan{}, err
	}
	plan.ScopeComplete = plan.Summary.ReviewRequiredCount == 0
	if !plan.ScopeComplete {
		return withDigest(plan)
	}
	return withDigest(plan)
}

func Validate(plan Plan) error {
	if plan.ContractVersion != ContractVersion || plan.Profile != Profile || plan.Mode != "preview_read_only" ||
		!plan.ApplySupported || !plan.ScopeComplete {
		return fmt.Errorf("%w: contract_or_scope", ErrInvalid)
	}
	if plan.ActionAuthorized || plan.MutationPerformed {
		return fmt.Errorf("%w: preview_claims_mutation_or_authority", ErrInvalid)
	}
	if err := validateTarget(plan.Target); err != nil {
		return err
	}
	if err := validateAdmin(plan.SelectedAdmin); err != nil {
		return err
	}
	if err := validateProtected(plan.Protected); err != nil {
		return err
	}
	if _, err := validateAndCanonicalizeSettingResets(plan.SettingResets); err != nil {
		return err
	}
	if err := validateSequenceStates(plan.Sequences); err != nil {
		return err
	}
	expected, err := scopeDigest(plan)
	if err != nil {
		return err
	}
	if !sha256Pattern.MatchString(plan.ScopeSHA256) || plan.ScopeSHA256 != expected {
		return fmt.Errorf("%w: scope_sha256", ErrInvalid)
	}
	return nil
}

func ClassifyTable(name string) (scope, action, reason string) {
	switch name {
	case "schema_migrations":
		return ScopeSchemaLedger, ActionPreserveAll, "migration ledger is immutable reset infrastructure"
	case "settings":
		return ScopeProtectedPartial, ActionResetSettingsBaseline, "preserve license and ledger unit-contract keys, reset runtime-required sensitive_words to an empty baseline, and delete other configured product settings"
	case "users":
		return ScopeResetCandidate, ActionDeleteExceptSelectedAdmin, "preserve exactly the unique active local platform administrator and password verifier, clear optional external identity fields, and advance its auth epoch"
	case "orgs":
		return ScopeResetCandidate, ActionDeleteExceptSelectedOrg, "preserve only the selected administrator's active root organization identity and clear its settings to the minimum shell"
	}
	if reason, ok := protectedTables[name]; ok {
		return ScopeProtected, ActionPreserveAll, reason
	}
	if _, ok := resetCandidateTables[name]; ok {
		return ScopeResetCandidate, ActionDeleteAll, "pre-reset product, control-plane, evidence, Feed, Outbox, checkpoint, credential, or business data"
	}
	return ScopeReviewRequired, "", "unclassified table blocks reset until the product contract is updated"
}

func ProtectedSettingKeys() []string {
	return slices.Clone(protectedSettingKeys)
}

func PlatformBaselineSettingKeys() []string {
	return slices.Clone(platformBaselineSettings)
}

func DeferredCycleConstraints() []string {
	return slices.Clone(deferredCycleConstraints)
}

func RequiredResetDeleteTriggers() []TriggerRef {
	return slices.Clone(resetDeleteTriggers)
}

func RedisKeyPrefixes() []string {
	return slices.Clone(redisKeyPrefixes)
}

// ExpectedPublicTables returns the exact public-schema inventory supported by
// this reset/bootstrap contract. A new table must be classified in a later
// contract before either operation can proceed.
func ExpectedPublicTables() []string {
	tables := make([]string, 0, len(resetCandidateTables)+len(protectedTables)+2)
	tables = append(tables, "schema_migrations", "settings")
	for name := range protectedTables {
		tables = append(tables, name)
	}
	for name := range resetCandidateTables {
		tables = append(tables, name)
	}
	slices.Sort(tables)
	return tables
}

// BootstrapZeroTables returns the reset-owned tables that must still be empty
// immediately before first-customer bootstrap. Login-created sessions and
// audit/outbox rows are verified separately; wallets are created atomically by
// bootstrap itself.
func BootstrapZeroTables() []string {
	excluded := stringSet("admin_sessions", "audit_logs", "internal_outbox_events", "orgs", "users", "wallets")
	result := make([]string, 0, len(resetCandidateTables))
	for name := range resetCandidateTables {
		if _, skip := excluded[name]; !skip {
			result = append(result, name)
		}
	}
	slices.Sort(result)
	return result
}

func Marshal(plan Plan) ([]byte, error) {
	return json.MarshalIndent(plan, "", "  ")
}

func Unmarshal(payload []byte) (Plan, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var plan Plan
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("decode reset plan: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Plan{}, fmt.Errorf("%w: trailing_json", ErrInvalid)
		}
		return Plan{}, fmt.Errorf("decode reset plan trailing content: %w", err)
	}
	if err := Validate(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func ArtifactSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func withDigest(plan Plan) (Plan, error) {
	digest, err := scopeDigest(plan)
	if err != nil {
		return Plan{}, err
	}
	plan.ScopeSHA256 = digest
	return plan, nil
}

type semanticScope struct {
	ContractVersion        string          `json:"contract_version"`
	Profile                string          `json:"profile"`
	Target                 TargetIdentity  `json:"target"`
	CurrentSourceInstance  string          `json:"current_source_instance"`
	NextSourceInstance     string          `json:"next_source_instance"`
	SourceIdentityAction   string          `json:"source_identity_action"`
	SelectedAdmin          AdminSelection  `json:"selected_admin"`
	Protected              ProtectedState  `json:"protected"`
	SettingResets          []SettingReset  `json:"required_setting_resets"`
	Schema                 SchemaEvidence  `json:"schema"`
	Sequences              []SequenceState `json:"sequence_invariants"`
	Summary                Summary         `json:"summary"`
	Tables                 []TablePlan     `json:"tables"`
	DeleteOrder            []DeleteStep    `json:"delete_order"`
	GuardedDeleteTriggers  []TriggerRef    `json:"guarded_delete_triggers"`
	DeferredFKConstraints  []string        `json:"deferred_fk_constraints"`
	ExecutionPreconditions []string        `json:"execution_preconditions"`
}

func scopeDigest(plan Plan) (string, error) {
	semantic := semanticScope{
		ContractVersion: plan.ContractVersion, Profile: plan.Profile, Target: plan.Target,
		CurrentSourceInstance: plan.CurrentSourceInstance, NextSourceInstance: plan.NextSourceInstance,
		SourceIdentityAction: plan.SourceIdentityAction, SelectedAdmin: plan.SelectedAdmin,
		Protected: plan.Protected, SettingResets: plan.SettingResets,
		Schema: plan.Schema, Sequences: plan.Sequences, Summary: plan.Summary,
		Tables: plan.Tables, DeleteOrder: plan.DeleteOrder,
		GuardedDeleteTriggers:  plan.GuardedDeleteTriggers,
		DeferredFKConstraints:  plan.DeferredFKConstraints,
		ExecutionPreconditions: plan.ExecutionPreconditions,
	}
	return canonicalSHA256(semantic)
}

func buildSchemaEvidence(snapshot Snapshot) (SchemaEvidence, error) {
	migrations := slices.Clone(snapshot.MigrationVersions)
	if err := schemaauthority.VerifyLedger(snapshot.ExpectedMigrationVersions, migrations); err != nil {
		return SchemaEvidence{}, fmt.Errorf("%w: reset_support_migration: %v", ErrInvalid, err)
	}
	foreignKeys := slices.Clone(snapshot.ForeignKeys)
	slices.SortFunc(foreignKeys, compareForeignKey)
	triggers := slices.Clone(snapshot.Triggers)
	slices.SortFunc(triggers, compareTrigger)
	sequences := canonicalSequences(snapshot.Sequences)
	if err := validateSequenceStates(sequences); err != nil {
		return SchemaEvidence{}, err
	}
	migrationDigest, err := canonicalSHA256(migrations)
	if err != nil {
		return SchemaEvidence{}, err
	}
	foreignKeyDigest, err := canonicalSHA256(foreignKeys)
	if err != nil {
		return SchemaEvidence{}, err
	}
	triggerDigest, err := canonicalSHA256(triggers)
	if err != nil {
		return SchemaEvidence{}, err
	}
	sequenceDigest, err := canonicalSHA256(sequenceRuntimeInvariants(sequences))
	if err != nil {
		return SchemaEvidence{}, err
	}
	return SchemaEvidence{
		MigrationVersions: migrations, MigrationSHA256: migrationDigest,
		ForeignKeyCount: len(foreignKeys), ForeignKeySHA256: foreignKeyDigest,
		TriggerCount: len(triggers), TriggerSHA256: triggerDigest,
		SequenceCount: len(sequences), SequenceSHA256: sequenceDigest,
	}, nil
}

type sequenceRuntimeInvariant struct {
	Name        string `json:"name"`
	OwnedTable  string `json:"owned_table"`
	OwnedColumn string `json:"owned_column"`
	IncrementBy int64  `json:"increment_by"`
	Cycle       bool   `json:"cycle"`
	LastValue   int64  `json:"last_value"`
	IsCalled    bool   `json:"is_called"`
	NextValue   int64  `json:"next_value"`
}

func canonicalSequences(sequences []SequenceState) []SequenceState {
	canonical := slices.Clone(sequences)
	slices.SortFunc(canonical, func(left, right SequenceState) int { return strings.Compare(left.Name, right.Name) })
	return canonical
}

func validateSequenceStates(sequences []SequenceState) error {
	for _, sequence := range sequences {
		if !identifierPattern.MatchString(sequence.Name) ||
			!identifierPattern.MatchString(sequence.OwnedTable) ||
			!identifierPattern.MatchString(sequence.OwnedColumn) ||
			sequence.IncrementBy <= 0 || sequence.Cycle ||
			sequence.NextValue <= sequence.MaxObservedValue {
			return fmt.Errorf("%w: sequence_state_%s", ErrInvalid, sequence.Name)
		}
		expectedNext := sequence.LastValue
		if sequence.IsCalled {
			if sequence.LastValue > int64(^uint64(0)>>1)-sequence.IncrementBy {
				return fmt.Errorf("%w: sequence_exhausted_%s", ErrInvalid, sequence.Name)
			}
			expectedNext += sequence.IncrementBy
		}
		if sequence.NextValue != expectedNext {
			return fmt.Errorf("%w: sequence_next_value_%s", ErrInvalid, sequence.Name)
		}
	}
	return nil
}

func sequenceRuntimeInvariants(sequences []SequenceState) []sequenceRuntimeInvariant {
	result := make([]sequenceRuntimeInvariant, 0, len(sequences))
	for _, sequence := range sequences {
		result = append(result, sequenceRuntimeInvariant{
			Name: sequence.Name, OwnedTable: sequence.OwnedTable, OwnedColumn: sequence.OwnedColumn,
			IncrementBy: sequence.IncrementBy, Cycle: sequence.Cycle,
			LastValue: sequence.LastValue, IsCalled: sequence.IsCalled, NextValue: sequence.NextValue,
		})
	}
	return result
}

func buildDeleteOrder(tables map[string]TablePlan, foreignKeys []ForeignKey) ([]string, error) {
	nodes := make(map[string]struct{})
	for name, table := range tables {
		if table.Action != ActionPreserveAll && table.Scope != ScopeReviewRequired {
			nodes[name] = struct{}{}
		}
	}
	deferred := stringSet(deferredCycleConstraints...)
	seenDeferred := make(map[string]bool, len(deferred))
	adjacency := make(map[string]map[string]struct{}, len(nodes))
	indegree := make(map[string]int, len(nodes))
	for node := range nodes {
		adjacency[node] = map[string]struct{}{}
		indegree[node] = 0
	}
	for _, fk := range foreignKeys {
		if _, ok := deferred[fk.Name]; ok {
			if !fk.Deferrable || fk.InitiallyDeferred || fk.DeleteAction != "NO ACTION" {
				return nil, fmt.Errorf("%w: deferred_cycle_constraint_%s", ErrInvalid, fk.Name)
			}
			seenDeferred[fk.Name] = true
			continue
		}
		if fk.ChildTable == fk.ParentTable {
			continue
		}
		if _, child := nodes[fk.ChildTable]; !child {
			continue
		}
		if _, parent := nodes[fk.ParentTable]; !parent {
			continue
		}
		if _, exists := adjacency[fk.ChildTable][fk.ParentTable]; exists {
			continue
		}
		adjacency[fk.ChildTable][fk.ParentTable] = struct{}{}
		indegree[fk.ParentTable]++
	}
	for name := range deferred {
		if !seenDeferred[name] {
			return nil, fmt.Errorf("%w: missing_deferred_cycle_constraint_%s", ErrInvalid, name)
		}
	}

	available := make([]string, 0)
	for node, count := range indegree {
		if count == 0 {
			available = append(available, node)
		}
	}
	slices.Sort(available)
	order := make([]string, 0, len(nodes))
	for len(available) > 0 {
		node := available[0]
		available = available[1:]
		order = append(order, node)
		parents := make([]string, 0, len(adjacency[node]))
		for parent := range adjacency[node] {
			parents = append(parents, parent)
		}
		slices.Sort(parents)
		for _, parent := range parents {
			indegree[parent]--
			if indegree[parent] == 0 {
				available = append(available, parent)
				slices.Sort(available)
			}
		}
	}
	if len(order) != len(nodes) {
		var blocked []string
		for node, count := range indegree {
			if count > 0 {
				blocked = append(blocked, node)
			}
		}
		slices.Sort(blocked)
		return nil, fmt.Errorf("%w: unreviewed_fk_cycle_%s", ErrInvalid, strings.Join(blocked, ","))
	}
	return order, nil
}

func validateTarget(target TargetIdentity) error {
	if strings.TrimSpace(target.RuntimeVersion) == "" || !gitObjectPattern.MatchString(target.RuntimeCommit) || !gitObjectPattern.MatchString(target.SourceTree) {
		return fmt.Errorf("%w: target_identity", ErrInvalid)
	}
	return nil
}

func validateAdmin(admin AdminSelection) error {
	if admin.UserID <= 0 || admin.OrganizationID <= 0 || admin.NormalizedLogin != PreservedLogin ||
		admin.OrganizationRole != "platform_admin" ||
		(admin.Role != "admin" && admin.Role != "platform_admin") || admin.Status != "active" ||
		admin.AuthProvider != "local" || admin.AuthEpoch < 0 || admin.AuthEpoch == int64(^uint64(0)>>1) ||
		admin.OrganizationStatus != "active" || strings.TrimSpace(admin.OrganizationType) == "" ||
		!admin.PasswordVerifierValid || !sha256Pattern.MatchString(admin.UserRowSHA256) ||
		!sha256Pattern.MatchString(admin.OrganizationRowSHA256) || !sha256Pattern.MatchString(admin.PasswordSHA256) {
		return fmt.Errorf("%w: selected_admin", ErrInvalid)
	}
	return nil
}

func validateProtected(protected ProtectedState) error {
	canonical := canonicalProtected(protected)
	if !slices.Equal(canonical.SettingKeys, protectedSettingKeys) || canonical.Settings.Name != "settings:protected_keys" ||
		canonical.Settings.RowCount < 2 || canonical.Settings.RowCount > int64(len(protectedSettingKeys)) ||
		!sha256Pattern.MatchString(canonical.Settings.SHA256) {
		return fmt.Errorf("%w: protected_settings", ErrInvalid)
	}
	if len(canonical.Tables) != len(protectedTables) {
		return fmt.Errorf("%w: protected_tables", ErrInvalid)
	}
	for _, table := range canonical.Tables {
		if _, ok := protectedTables[table.Name]; !ok || table.RowCount < 0 || !sha256Pattern.MatchString(table.SHA256) {
			return fmt.Errorf("%w: protected_table_%s", ErrInvalid, table.Name)
		}
	}
	return nil
}

func validateAndCanonicalizeSettingResets(resets []SettingReset) ([]SettingReset, error) {
	canonical := slices.Clone(resets)
	slices.SortFunc(canonical, func(left, right SettingReset) int { return strings.Compare(left.Key, right.Key) })
	if len(canonical) != len(platformBaselineSettings) {
		return nil, fmt.Errorf("%w: required_setting_resets", ErrInvalid)
	}
	expectedTarget, err := canonicalSHA256(json.RawMessage(`[]`))
	if err != nil {
		return nil, err
	}
	for index, key := range platformBaselineSettings {
		reset := canonical[index]
		if reset.Key != key || !sha256Pattern.MatchString(reset.CurrentRowSHA256) || reset.TargetValueSHA256 != expectedTarget {
			return nil, fmt.Errorf("%w: required_setting_reset_%s", ErrInvalid, key)
		}
	}
	return canonical, nil
}

func validateRequiredTables(seen map[string]struct{}) error {
	for _, name := range ExpectedPublicTables() {
		if _, ok := seen[name]; !ok {
			return fmt.Errorf("%w: missing_table_%s", ErrInvalid, name)
		}
	}
	return nil
}

func validateResetDeleteTriggers(triggers []TriggerState) error {
	byIdentity := make(map[string]TriggerState, len(triggers))
	for _, trigger := range triggers {
		byIdentity[trigger.Table+"."+trigger.Name] = trigger
	}
	for _, required := range resetDeleteTriggers {
		trigger, ok := byIdentity[required.Table+"."+required.Name]
		if !ok || trigger.Enabled != "O" || !strings.Contains(trigger.Definition, "DELETE") {
			return fmt.Errorf("%w: guarded_delete_trigger_%s_%s", ErrInvalid, required.Table, required.Name)
		}
	}
	return nil
}

func validateLegacyRoleCompatibilityTrigger(triggers []TriggerState) error {
	identity := legacyRoleCompatibilityTrigger.Table + "." + legacyRoleCompatibilityTrigger.Name
	for _, trigger := range triggers {
		if trigger.Table+"."+trigger.Name != identity {
			continue
		}
		definition := strings.ToLower(trigger.Definition)
		if trigger.Enabled == "O" && strings.Contains(definition, strings.ToLower(legacyRoleCompatibilityFunction)) {
			return nil
		}
		break
	}
	return fmt.Errorf("%w: legacy_role_compatibility_trigger", ErrInvalid)
}

func validateProjectMemberRoleCompatibilityTrigger(triggers []TriggerState) error {
	identity := projectMemberRoleCompatibilityTrigger.Table + "." + projectMemberRoleCompatibilityTrigger.Name
	for _, trigger := range triggers {
		if trigger.Table+"."+trigger.Name != identity {
			continue
		}
		definition := strings.ToLower(trigger.Definition)
		if trigger.Enabled == "O" && strings.Contains(definition, strings.ToLower(projectMemberRoleCompatibilityFunction)) {
			return nil
		}
		break
	}
	return fmt.Errorf("%w: project_member_role_compatibility_trigger", ErrInvalid)
}

func validateWorkloadIdentityTriggers(triggers []TriggerState) error {
	required := []struct {
		ref      TriggerRef
		function string
	}{
		{workloadEnvironmentImmutableTrigger, workloadEnvironmentImmutableFunction},
		{workloadClientProfileCompatibilityTrigger, workloadClientProfileCompatibilityFunction},
	}
	byIdentity := make(map[string]TriggerState, len(triggers))
	for _, trigger := range triggers {
		byIdentity[trigger.Table+"."+trigger.Name] = trigger
	}
	for _, item := range required {
		trigger, ok := byIdentity[item.ref.Table+"."+item.ref.Name]
		if !ok || trigger.Enabled != "O" ||
			!strings.Contains(strings.ToLower(trigger.Definition), strings.ToLower(item.function)) {
			return fmt.Errorf("%w: workload_identity_trigger_%s", ErrInvalid, item.ref.Name)
		}
	}
	return nil
}

func canonicalProtected(protected ProtectedState) ProtectedState {
	protected.Tables = slices.Clone(protected.Tables)
	slices.SortFunc(protected.Tables, func(left, right ContentDigest) int { return strings.Compare(left.Name, right.Name) })
	protected.SettingKeys = slices.Clone(protected.SettingKeys)
	slices.Sort(protected.SettingKeys)
	return protected
}

func canonicalRedisObservation(observation RedisObservation) RedisObservation {
	observation.BoundToScope = false
	observation.Prefixes = slices.Clone(observation.Prefixes)
	slices.SortFunc(observation.Prefixes, func(left, right RedisPrefixObservation) int { return strings.Compare(left.Prefix, right.Prefix) })
	return observation
}

func canonicalSHA256(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode digest input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func compareForeignKey(left, right ForeignKey) int {
	for _, comparison := range []int{
		strings.Compare(left.ChildTable, right.ChildTable),
		strings.Compare(left.ParentTable, right.ParentTable),
		strings.Compare(left.Name, right.Name),
	} {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

func compareTrigger(left, right TriggerState) int {
	if comparison := strings.Compare(left.Table, right.Table); comparison != 0 {
		return comparison
	}
	return strings.Compare(left.Name, right.Name)
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
