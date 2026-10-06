package resetplan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/schemaauthority"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestBuildHasStableSemanticScopeAndVariableArtifactIdentity(t *testing.T) {
	firstSnapshot := syntheticClassifiedSnapshot()
	secondSnapshot := syntheticClassifiedSnapshot()
	secondSnapshot.Redis.Prefixes[0].KeyCount = 77
	first, err := Build(firstSnapshot, testTarget(), "ecs.first-integration", "ecs.second-integration.v2", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("Build first: %v", err)
	}
	second, err := Build(secondSnapshot, testTarget(), "ecs.first-integration", "ecs.second-integration.v2", time.Unix(100, 0))
	if err != nil {
		t.Fatalf("Build second: %v", err)
	}
	if first.ScopeSHA256 != second.ScopeSHA256 {
		t.Fatalf("generated_at or Redis observation changed semantic scope: %s != %s", first.ScopeSHA256, second.ScopeSHA256)
	}
	firstJSON, err := Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if ArtifactSHA256(firstJSON) == ArtifactSHA256(secondJSON) {
		t.Fatal("different generated artifacts unexpectedly have the same artifact digest")
	}
	if first.ActionAuthorized || !first.ApplySupported || first.MutationPerformed || !first.ScopeComplete {
		t.Fatalf("unexpected preview authority boundary: %+v", first)
	}
	if first.Summary.ReviewRequiredCount != 0 || first.Summary.ProtectedCount != len(protectedTables) {
		t.Fatalf("unexpected classification summary: %+v", first.Summary)
	}
	if first.Summary.TableCount != 140 || first.Summary.ResetCandidateCount != 112 ||
		first.Summary.ProtectedCount != 26 || first.Summary.ProtectedPartialCount != 1 ||
		first.Summary.SchemaLedgerCount != 1 {
		t.Fatalf("reset classification cardinality drifted: %+v", first.Summary)
	}
	if !strings.HasPrefix(first.ScopeSHA256, "sha256:") || len(first.DeleteOrder) == 0 {
		t.Fatalf("missing digest or deterministic delete order: %+v", first)
	}
	if !slices.Contains(first.ExecutionPreconditions, "tls_certificate_profile_aware_validity_and_renewal_gate_satisfied") ||
		slices.Contains(first.ExecutionPreconditions, "tls_validity_strictly_greater_than_seven_days") {
		t.Fatalf("reset plan does not declare the profile-aware TLS gate: %+v", first.ExecutionPreconditions)
	}
}

func TestBuildRequiresNewSourceExactAdminAndCompleteClassification(t *testing.T) {
	snapshot := syntheticClassifiedSnapshot()
	if _, err := Build(snapshot, testTarget(), "source-a", "source-a", time.Unix(0, 0)); err == nil {
		t.Fatal("reusing source_instance must fail")
	}

	badAdmin := syntheticClassifiedSnapshot()
	badAdmin.Admin.NormalizedLogin = "other"
	if _, err := Build(badAdmin, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil {
		t.Fatal("non-admin selected identity must fail")
	}

	unknown := syntheticClassifiedSnapshot()
	unknown.Tables = append(unknown.Tables, TableInventory{Name: "future_unknown"})
	plan, err := Build(unknown, testTarget(), "source-a", "source-b", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("Build unknown inventory: %v", err)
	}
	if plan.ScopeComplete || plan.Summary.ReviewRequiredCount != 1 {
		t.Fatalf("unknown table did not fail closed: %+v", plan.Summary)
	}
}

func TestBuildChecksSyntheticArtifactLedgerAndRejectsPreviousOrFutureMax(t *testing.T) {
	current := syntheticClassifiedSnapshot()
	plan, err := Build(current, testTarget(), "source-a", "source-b", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("Build current schema: %v", err)
	}
	if got := plan.Schema.MigrationVersions[len(plan.Schema.MigrationVersions)-1]; got != schemaauthority.LatestSupportedMigration {
		t.Fatalf("fixture migration = %q, want %q", got, schemaauthority.LatestSupportedMigration)
	}
	if plan.Summary.TableCount != 140 || plan.Summary.ResetCandidateCount != 112 || plan.Summary.ReviewRequiredCount != 0 {
		t.Fatalf("current schema changed reviewed table classification: %+v", plan.Summary)
	}
	if plan.Schema.TriggerCount != len(resetDeleteTriggers)+6 {
		t.Fatalf("current schema compatibility trigger missing from reset contract: %+v", plan.Schema)
	}
	if plan.SelectedAdmin.OrganizationRole != "platform_admin" || plan.SelectedAdmin.Role != "platform_admin" {
		t.Fatalf("selected admin identity axes drifted: %+v", plan.SelectedAdmin)
	}

	previous := syntheticClassifiedSnapshot()
	previous.MigrationVersions = previous.MigrationVersions[:len(previous.MigrationVersions)-1]
	if _, err := Build(previous, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "reset_support_migration") {
		t.Fatalf("previous schema as max error = %v", err)
	}

	future := syntheticClassifiedSnapshot()
	future.MigrationVersions = append(future.MigrationVersions, "999999_future_contract")
	if _, err := Build(future, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "reset_support_migration") {
		t.Fatalf("future migration error = %v", err)
	}

	missingMiddle := syntheticClassifiedSnapshot()
	missingMiddle.MigrationVersions = []string{
		missingMiddle.MigrationVersions[0],
		missingMiddle.MigrationVersions[len(missingMiddle.MigrationVersions)-1],
	}
	if _, err := Build(missingMiddle, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "missing=") {
		t.Fatalf("missing middle migration error = %v", err)
	}

	extraBeforeCeiling := syntheticClassifiedSnapshot()
	extraBeforeCeiling.MigrationVersions = []string{
		extraBeforeCeiling.MigrationVersions[0],
		extraBeforeCeiling.MigrationVersions[1],
		"000078_unexpected_reset_contract",
		extraBeforeCeiling.MigrationVersions[2],
	}
	if _, err := Build(extraBeforeCeiling, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "extra=") {
		t.Fatalf("extra pre-ceiling migration error = %v", err)
	}

	duplicate := syntheticClassifiedSnapshot()
	duplicate.MigrationVersions = append(duplicate.MigrationVersions[:2], duplicate.MigrationVersions[1:]...)
	if _, err := Build(duplicate, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "duplicate migration") {
		t.Fatalf("duplicate migration error = %v", err)
	}

	reordered := syntheticClassifiedSnapshot()
	reordered.MigrationVersions[0], reordered.MigrationVersions[1] = reordered.MigrationVersions[1], reordered.MigrationVersions[0]
	if _, err := Build(reordered, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "not strictly ordered") {
		t.Fatalf("reordered migration error = %v", err)
	}

	wrongAdminAxis := syntheticClassifiedSnapshot()
	wrongAdminAxis.Admin.OrganizationRole = "member"
	if _, err := Build(wrongAdminAxis, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "selected_admin") {
		t.Fatalf("wrong selected admin organization role error = %v", err)
	}
}

func TestBuildOrdersCanonicalEvidenceBeforeReleaseAndDeploymentParents(t *testing.T) {
	snapshot := syntheticClassifiedSnapshot()
	snapshot.ForeignKeys = append(snapshot.ForeignKeys,
		ForeignKey{Name: "release_authoring_receipt", ChildTable: "runtime_change_releases", ParentTable: "model_deployment_release_authoring_receipts", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "lifecycle_qualification_release", ChildTable: "model_deployment_lifecycle_receipt_qualifications", ParentTable: "model_deployment_lifecycle_receipt_releases", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "lifecycle_release_receipt", ChildTable: "model_deployment_lifecycle_receipt_releases", ParentTable: "model_deployment_lifecycle_receipts", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "lifecycle_release_runtime", ChildTable: "model_deployment_lifecycle_receipt_releases", ParentTable: "runtime_change_releases", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "lifecycle_receipt_runtime", ChildTable: "model_deployment_lifecycle_receipts", ParentTable: "runtime_change_releases", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "create_receipt_deployment", ChildTable: "model_deployment_create_receipts", ParentTable: "model_deployments", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "create_receipt_operation", ChildTable: "model_deployment_create_receipts", ParentTable: "model_deployment_operations", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "route_release", ChildTable: "runtime_route_revision_items", ParentTable: "runtime_change_releases", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "route_deployment", ChildTable: "runtime_route_revision_items", ParentTable: "model_deployments", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "route_operation", ChildTable: "runtime_route_revision_items", ParentTable: "model_deployment_operations", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "probe_deployment", ChildTable: "model_deployment_probes", ParentTable: "model_deployments", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "probe_operation", ChildTable: "model_deployment_probes", ParentTable: "model_deployment_operations", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "probe_credential", ChildTable: "model_deployment_probes", ParentTable: "provider_credential_bindings", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "capability_operation", ChildTable: "model_deployment_capabilities", ParentTable: "model_deployment_operations", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "operation_deployment", ChildTable: "model_deployment_operations", ParentTable: "model_deployments", DeleteAction: "CASCADE"},
		ForeignKey{Name: "binding_deployment_binding", ChildTable: "credential_binding_deployments", ParentTable: "provider_credential_bindings", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "binding_deployment_deployment", ChildTable: "credential_binding_deployments", ParentTable: "model_deployments", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "call_log_deployment", ChildTable: "call_logs", ParentTable: "model_deployments", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "provider_attempt_deployment", ChildTable: "billing_provider_attempts", ParentTable: "model_deployments", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "binding_connection", ChildTable: "provider_credential_bindings", ParentTable: "provider_connections", DeleteAction: "RESTRICT"},
		ForeignKey{Name: "deployment_connection", ChildTable: "model_deployments", ParentTable: "provider_connections", DeleteAction: "RESTRICT"},
	)
	plan, err := Build(snapshot, testTarget(), "source-a", "source-b", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("Build canonical delete order: %v", err)
	}
	positions := make(map[string]int, len(plan.DeleteOrder))
	for _, step := range plan.DeleteOrder {
		positions[step.Table] = step.Position
	}
	for _, relation := range [][2]string{
		{"runtime_change_releases", "model_deployment_release_authoring_receipts"},
		{"model_deployment_lifecycle_receipt_qualifications", "model_deployment_lifecycle_receipt_releases"},
		{"model_deployment_lifecycle_receipt_releases", "model_deployment_lifecycle_receipts"},
		{"model_deployment_lifecycle_receipt_releases", "runtime_change_releases"},
		{"model_deployment_lifecycle_receipts", "runtime_change_releases"},
		{"model_deployment_create_receipts", "model_deployment_operations"},
		{"model_deployment_create_receipts", "model_deployments"},
		{"runtime_route_revision_items", "runtime_change_releases"},
		{"runtime_route_revision_items", "model_deployment_operations"},
		{"runtime_route_revision_items", "model_deployments"},
		{"model_deployment_probes", "model_deployments"},
		{"model_deployment_probes", "model_deployment_operations"},
		{"model_deployment_probes", "provider_credential_bindings"},
		{"model_deployment_capabilities", "model_deployment_operations"},
		{"model_deployment_operations", "model_deployments"},
		{"credential_binding_deployments", "provider_credential_bindings"},
		{"credential_binding_deployments", "model_deployments"},
		{"call_logs", "model_deployments"},
		{"billing_provider_attempts", "model_deployments"},
		{"provider_credential_bindings", "provider_connections"},
		{"model_deployments", "provider_connections"},
	} {
		if positions[relation[0]] >= positions[relation[1]] {
			t.Fatalf("delete order must place child %s before parent %s: %+v", relation[0], relation[1], plan.DeleteOrder)
		}
	}
}

func TestBuildRejectsUnreviewedFKCycleAndMissingGuardTrigger(t *testing.T) {
	cycle := syntheticClassifiedSnapshot()
	cycle.ForeignKeys = append(cycle.ForeignKeys,
		ForeignKey{Name: "cycle_a", ChildTable: "channels", ParentTable: "api_keys"},
		ForeignKey{Name: "cycle_b", ChildTable: "api_keys", ParentTable: "channels"},
	)
	if _, err := Build(cycle, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "unreviewed_fk_cycle") {
		t.Fatalf("unreviewed FK cycle error = %v", err)
	}

	missingTrigger := syntheticClassifiedSnapshot()
	missingTrigger.Triggers = missingTrigger.Triggers[1:]
	if _, err := Build(missingTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "guarded_delete_trigger") {
		t.Fatalf("missing guarded trigger error = %v", err)
	}

	missingCanonicalLifecycleTrigger := syntheticClassifiedSnapshot()
	missingCanonicalLifecycleTrigger.Triggers = slices.DeleteFunc(missingCanonicalLifecycleTrigger.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == "model_deployments_identity_immutable"
	})
	if _, err := Build(missingCanonicalLifecycleTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "guarded_delete_trigger_model_deployments_model_deployments_identity_immutable") {
		t.Fatalf("missing canonical lifecycle trigger error = %v", err)
	}

	missingP3QualificationTrigger := syntheticClassifiedSnapshot()
	missingP3QualificationTrigger.Triggers = slices.DeleteFunc(missingP3QualificationTrigger.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == "runtime_route_revision_items_p3_qualification_guard"
	})
	if _, err := Build(missingP3QualificationTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil ||
		!strings.Contains(err.Error(), "guarded_delete_trigger_runtime_route_revision_items_runtime_route_revision_items_p3_qualification_guard") {
		t.Fatalf("missing P3 qualification reset trigger error = %v", err)
	}

	missingLifecycleReceiptTrigger := syntheticClassifiedSnapshot()
	missingLifecycleReceiptTrigger.Triggers = slices.DeleteFunc(missingLifecycleReceiptTrigger.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == "model_deployment_lifecycle_receipts_immutable"
	})
	if _, err := Build(missingLifecycleReceiptTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil ||
		!strings.Contains(err.Error(), "guarded_delete_trigger_model_deployment_lifecycle_receipts_model_deployment_lifecycle_receipts_immutable") {
		t.Fatalf("missing lifecycle receipt reset trigger error = %v", err)
	}

	missingSuccessorCompletionGuard := syntheticClassifiedSnapshot()
	missingSuccessorCompletionGuard.Triggers = slices.DeleteFunc(missingSuccessorCompletionGuard.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == successorCompletionGuardTrigger.Name
	})
	if _, err := Build(missingSuccessorCompletionGuard, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil ||
		!strings.Contains(err.Error(), "successor_completion_guard") {
		t.Fatalf("missing successor completion guard error = %v", err)
	}

	missingAuthoringReceiptTrigger := syntheticClassifiedSnapshot()
	missingAuthoringReceiptTrigger.Triggers = slices.DeleteFunc(missingAuthoringReceiptTrigger.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == "model_deployment_release_authoring_receipts_immutable"
	})
	if _, err := Build(missingAuthoringReceiptTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil ||
		!strings.Contains(err.Error(), "guarded_delete_trigger_model_deployment_release_authoring_receipts_model_deployment_release_authoring_receipts_immutable") {
		t.Fatalf("missing authoring receipt reset trigger error = %v", err)
	}

	missingRuntimeEventTrigger := syntheticClassifiedSnapshot()
	missingRuntimeEventTrigger.Triggers = slices.DeleteFunc(missingRuntimeEventTrigger.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == "runtime_change_events_append_only"
	})
	if _, err := Build(missingRuntimeEventTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil ||
		!strings.Contains(err.Error(), "guarded_delete_trigger_runtime_change_events_runtime_change_events_append_only") {
		t.Fatalf("missing runtime event reset trigger error = %v", err)
	}

	missingCompatibilityTrigger := syntheticClassifiedSnapshot()
	missingCompatibilityTrigger.Triggers = slices.DeleteFunc(missingCompatibilityTrigger.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == legacyRoleCompatibilityTrigger.Name
	})
	if _, err := Build(missingCompatibilityTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "legacy_role_compatibility_trigger") {
		t.Fatalf("missing legacy-role compatibility trigger error = %v", err)
	}

	missingProjectMemberCompatibilityTrigger := syntheticClassifiedSnapshot()
	missingProjectMemberCompatibilityTrigger.Triggers = slices.DeleteFunc(missingProjectMemberCompatibilityTrigger.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == projectMemberRoleCompatibilityTrigger.Name
	})
	if _, err := Build(missingProjectMemberCompatibilityTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "project_member_role_compatibility_trigger") {
		t.Fatalf("missing project-member compatibility trigger error = %v", err)
	}

	restrictCycle := syntheticClassifiedSnapshot()
	restrictCycle.ForeignKeys[0].DeleteAction = "RESTRICT"
	if _, err := Build(restrictCycle, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "deferred_cycle_constraint") {
		t.Fatalf("non-deferable delete action error = %v", err)
	}

	unsafeSequence := syntheticClassifiedSnapshot()
	unsafeSequence.Sequences[0].NextValue = unsafeSequence.Sequences[0].MaxObservedValue
	if _, err := Build(unsafeSequence, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "sequence_state") {
		t.Fatalf("sequence reuse risk error = %v", err)
	}
}

func TestPlanUnmarshalRejectsUnknownAndTrailingJSON(t *testing.T) {
	plan, err := Build(syntheticClassifiedSnapshot(), testTarget(), "source-a", "source-b", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Unmarshal(append(payload, []byte("\n{}")...)); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
	payload = []byte(strings.Replace(string(payload), `"mode":`, `"unknown_field":true,"mode":`, 1))
	if _, err := Unmarshal(payload); err == nil {
		t.Fatal("unknown plan field was accepted")
	}
}

func TestEveryCurrentMigrationTableHasAnExplicitResetClassification(t *testing.T) {
	matches := regexp.MustCompile(`(?im)CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+([a-z][a-z0-9_]*)`)
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	seen := map[string]bool{"schema_migrations": true}
	for _, path := range paths {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		for _, match := range matches.FindAllStringSubmatch(string(contents), -1) {
			seen[match[1]] = true
		}
	}
	var unknown []string
	for name := range seen {
		if scope, action, reason := ClassifyTable(name); action == "" || reason == "" || (scope == ScopeReviewRequired && action != ActionPreserveAll) {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	if len(unknown) != 0 {
		t.Fatalf("migration tables missing explicit reset classification: %v", unknown)
	}
}

func TestContentTablesPreserveAllAndRefuseResetEvenWhenEmpty(t *testing.T) {
	names := []string{"execution_content_streams", "execution_source_content_scopes", "execution_source_content_bindings", "execution_source_content_operations", "execution_content_policies", "execution_content_manifests", "execution_content_objects", "execution_content_health"}
	for _, rows := range []int64{0, 1} {
		for _, name := range names {
			scope, action, reason := ClassifyTable(name)
			if scope != ScopeReviewRequired || action != ActionPreserveAll || reason == "" {
				t.Fatalf("content classification missing for %s: %s %s %s", name, scope, action, reason)
			}
			snapshot := syntheticClassifiedSnapshot()
			snapshot.ExpectedMigrationVersions = slices.Clone(snapshot.MigrationVersions)
			for _, content := range names {
				count := int64(0)
				if content == name {
					count = rows
				}
				snapshot.Tables = append(snapshot.Tables, TableInventory{Name: content, RowCount: count})
			}
			plan, err := Build(snapshot, testTarget(), "source-a", "source-b", time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			if plan.Summary.TableCount != 148 || plan.ScopeComplete || plan.Summary.ReviewRequiredCount != len(names) || Validate(plan) == nil {
				t.Fatalf("M119 content scope must refuse apply, including empty tables: %+v", plan.Summary)
			}
			for _, entry := range plan.Tables {
				if slices.Contains(names, entry.Name) && (entry.Action != ActionPreserveAll || entry.ExpectedDeletedRows != 0 || entry.ExpectedRetainedRows != entry.RowCount) {
					t.Fatalf("content rows not fully retained: %+v", entry)
				}
			}
			for _, step := range plan.DeleteOrder {
				if slices.Contains(names, step.Table) {
					t.Fatalf("content table entered deletion order: %s", step.Table)
				}
			}
		}
	}
	if scope, action, _ := ClassifyTable("future_unknown"); scope != ScopeReviewRequired || action != "" {
		t.Fatal("unknown tables must remain unclassified")
	}
}

func TestXInsightCredentialJournalPreservesHistoryAndRefusesReset(t *testing.T) {
	const name = "xinsight_connector_credential_operations"
	for _, rows := range []int64{0, 1} {
		t.Run(fmt.Sprintf("rows_%d", rows), func(t *testing.T) {
			if scope, action, reason := ClassifyTable(name); scope != ScopeReviewRequired || action != ActionPreserveAll || reason == "" {
				t.Fatalf("credential journal classification: %s %s %s", scope, action, reason)
			}
			snapshot := syntheticClassifiedSnapshot()
			snapshot.Tables = append(snapshot.Tables, TableInventory{Name: name, RowCount: rows})
			plan, err := Build(snapshot, testTarget(), "source-a", "source-b", time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			if plan.ScopeComplete || plan.Summary.ReviewRequiredCount != 1 || plan.ActionAuthorized || plan.MutationPerformed || Validate(plan) == nil {
				t.Fatalf("credential journal entered reset authority: %+v", plan.Summary)
			}
			found := false
			for _, table := range plan.Tables {
				if table.Name == name {
					found = true
					if table.Action != ActionPreserveAll || table.ExpectedDeletedRows != 0 || table.ExpectedRetainedRows != rows {
						t.Fatalf("credential journal rows not retained: %+v", table)
					}
				}
			}
			if !found {
				t.Fatal("credential journal missing from reset preview")
			}
			for _, step := range plan.DeleteOrder {
				if step.Table == name {
					t.Fatalf("credential journal entered deletion order: %s", step.Table)
				}
			}
		})
	}
}

func TestSourceFactsDataCannotEnterTheExistingResetErasureScope(t *testing.T) {
	for _, name := range []string{"execution_source_acknowledgements", "execution_source_connections", "execution_source_credentials", "execution_source_facts", "execution_source_streams", "execution_source_budget_scopes", "execution_source_budget_definitions", "execution_source_budget_bindings", "execution_source_budget_binding_history", "execution_source_budget_coverage", "usage_finalization_journal"} {
		t.Run(name, func(t *testing.T) {
			snapshot := syntheticClassifiedSnapshot()
			for index := range snapshot.Tables {
				if snapshot.Tables[index].Name == name {
					snapshot.Tables[index].RowCount = 1
				}
			}
			for index := range snapshot.Protected.Tables {
				if snapshot.Protected.Tables[index].Name == name {
					snapshot.Protected.Tables[index].RowCount = 1
				}
			}
			plan, err := Build(snapshot, testTarget(), "source-a", "source-b", time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			if plan.ScopeComplete || plan.Summary.ReviewRequiredCount != 1 || plan.ActionAuthorized || plan.MutationPerformed {
				t.Fatalf("source data entered reset authority: %+v", plan.Summary)
			}
			for _, table := range plan.Tables {
				if table.Name == name && (table.Action != ActionPreserveAll || table.ExpectedDeletedRows != 0 || table.ExpectedRetainedRows != 1) {
					t.Fatalf("source evidence not retained: %+v", table)
				}
			}
			if err := Validate(plan); err == nil {
				t.Fatal("incomplete source reset scope passed apply validation")
			}
		})
	}
}

// This classified subset tests pure plan mechanics. It is not a complete
// current-schema inventory or evidence that a real M119 reset is supported.
// The content tests add all eight M119 tables and prove unconditional refusal.
func syntheticClassifiedSnapshot() Snapshot {
	allTables := []string{"schema_migrations", "settings"}
	for name := range protectedTables {
		allTables = append(allTables, name)
	}
	for name := range resetCandidateTables {
		allTables = append(allTables, name)
	}
	slices.Sort(allTables)
	tables := make([]TableInventory, 0, len(allTables))
	for _, name := range allTables {
		count := int64(0)
		switch name {
		case "schema_migrations":
			count = 63
		case "settings":
			count = 3
		case "users", "orgs", "micro_ledger_runtime_state", "release_state":
			count = 1
		}
		tables = append(tables, TableInventory{Name: name, RowCount: count})
	}
	protected := ProtectedState{
		SettingKeys: ProtectedSettingKeys(),
		Settings:    ContentDigest{Name: "settings:protected_keys", RowCount: 2, SHA256: testDigest},
	}
	for name := range protectedTables {
		count := int64(0)
		if name == "micro_ledger_runtime_state" || name == "release_state" {
			count = 1
		}
		protected.Tables = append(protected.Tables, ContentDigest{Name: name, RowCount: count, SHA256: testDigest})
	}
	triggers := make([]TriggerState, 0, len(resetDeleteTriggers))
	for _, ref := range resetDeleteTriggers {
		triggers = append(triggers, TriggerState{
			Table: ref.Table, Name: ref.Name,
			Definition: "CREATE TRIGGER " + ref.Name + " BEFORE DELETE OR UPDATE ON public." + ref.Table,
			Enabled:    "O",
		})
	}
	triggers = append(triggers, TriggerState{
		Table: successorCompletionGuardTrigger.Table,
		Name:  successorCompletionGuardTrigger.Name,
		Definition: "CREATE TRIGGER " + successorCompletionGuardTrigger.Name +
			" BEFORE UPDATE ON public.model_deployment_lifecycle_receipts EXECUTE FUNCTION " + successorCompletionGuardFunction + "()",
		Enabled: "O",
	})
	triggers = append(triggers, TriggerState{
		Table: legacyRoleCompatibilityTrigger.Table,
		Name:  legacyRoleCompatibilityTrigger.Name,
		Definition: "CREATE TRIGGER " + legacyRoleCompatibilityTrigger.Name +
			" BEFORE INSERT OR UPDATE ON public.users EXECUTE FUNCTION " + legacyRoleCompatibilityFunction + "()",
		Enabled: "O",
	})
	triggers = append(triggers, TriggerState{
		Table: workloadEnvironmentImmutableTrigger.Table,
		Name:  workloadEnvironmentImmutableTrigger.Name,
		Definition: "CREATE TRIGGER " + workloadEnvironmentImmutableTrigger.Name +
			" BEFORE UPDATE ON public.workload_applications EXECUTE FUNCTION " + workloadEnvironmentImmutableFunction + "()",
		Enabled: "O",
	})
	triggers = append(triggers, TriggerState{
		Table: workloadClientProfileCompatibilityTrigger.Table,
		Name:  workloadClientProfileCompatibilityTrigger.Name,
		Definition: "CREATE TRIGGER " + workloadClientProfileCompatibilityTrigger.Name +
			" AFTER INSERT OR UPDATE ON public.workload_applications EXECUTE FUNCTION " + workloadClientProfileCompatibilityFunction + "()",
		Enabled: "O",
	})
	triggers = append(triggers, TriggerState{
		Table: projectMemberRoleCompatibilityTrigger.Table,
		Name:  projectMemberRoleCompatibilityTrigger.Name,
		Definition: "CREATE TRIGGER " + projectMemberRoleCompatibilityTrigger.Name +
			" BEFORE INSERT OR UPDATE ON public.project_members EXECUTE FUNCTION " + projectMemberRoleCompatibilityFunction + "()",
		Enabled: "O",
	})
	triggers = append(triggers, TriggerState{
		Table: "billing_provider_attempts",
		Name:  "billing_provider_attempts_dispatch_fence_immutable",
		Definition: "CREATE TRIGGER billing_provider_attempts_dispatch_fence_immutable " +
			"BEFORE UPDATE ON public.billing_provider_attempts EXECUTE FUNCTION preserve_provider_attempt_dispatch_fence()",
		Enabled: "O",
	})
	migrationVersions := []string{
		"000056_prd8_invocation_optimization",
		"000057_second_integration_reset_support",
		schemaauthority.LatestSupportedMigration,
	}
	return Snapshot{
		Tables: tables,
		Admin: AdminSelection{
			UserID: 1, OrganizationID: 1, NormalizedLogin: PreservedLogin,
			OrganizationRole: "platform_admin", Role: "platform_admin", Status: "active", AuthProvider: "local",
			OrganizationStatus: "active", PasswordVerifierValid: true,
			OrganizationType: "partner", PasswordSHA256: testDigest,
			UserRowSHA256: testDigest, OrganizationRowSHA256: testDigest,
		},
		Protected: protected,
		SettingResets: []SettingReset{{
			Key: "sensitive_words", CurrentRowSHA256: testDigest,
			TargetValueSHA256: "sha256:4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945",
			AlreadyAtTarget:   true,
		}},
		MigrationVersions:         slices.Clone(migrationVersions),
		ExpectedMigrationVersions: slices.Clone(migrationVersions),
		Sequences: []SequenceState{{
			Name: "users_id_seq", OwnedTable: "users", OwnedColumn: "id",
			IncrementBy: 1, LastValue: 10, IsCalled: true, NextValue: 11, MaxObservedValue: 1,
		}},
		ForeignKeys: []ForeignKey{
			{Name: "access_requests_target_grant_scope_fk", ChildTable: "access_requests", ParentTable: "access_grants", DeleteAction: "NO ACTION", Deferrable: true},
			{Name: "billing_observation_receipt_reservation_fk", ChildTable: "billing_observation_receipts", ParentTable: "freeze_idempotency", DeleteAction: "NO ACTION", Deferrable: true},
			{Name: "billing_provider_attempts_reservation_fk", ChildTable: "billing_provider_attempts", ParentTable: "freeze_idempotency", DeleteAction: "NO ACTION", Deferrable: true},
			{Name: "freeze_idempotency_delivery_attempt_provider_fk", ChildTable: "freeze_idempotency", ParentTable: "billing_provider_attempts", DeleteAction: "NO ACTION", Deferrable: true},
			{Name: "freeze_idempotency_observation_receipt_fk", ChildTable: "freeze_idempotency", ParentTable: "billing_observation_receipts", DeleteAction: "NO ACTION", Deferrable: true},
			{Name: "provider_cost_reconciliations_observation_receipt_fk", ChildTable: "provider_cost_reconciliations", ParentTable: "billing_observation_receipts", DeleteAction: "NO ACTION", Deferrable: true},
		},
		Triggers: triggers,
		Redis: RedisObservation{Prefixes: []RedisPrefixObservation{
			{Prefix: "limit:{"},
		}},
	}
}

func testTarget() TargetIdentity {
	return TargetIdentity{
		RuntimeVersion: "v2.0.2",
		RuntimeCommit:  strings.Repeat("a", 40),
		SourceTree:     strings.Repeat("b", 40),
	}
}
