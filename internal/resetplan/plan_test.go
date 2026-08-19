package resetplan

import (
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
	firstSnapshot := completeSnapshot()
	secondSnapshot := completeSnapshot()
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
	if first.Summary.TableCount != 102 || first.Summary.ResetCandidateCount != 94 ||
		first.Summary.ProtectedCount != 6 || first.Summary.ProtectedPartialCount != 1 ||
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
	snapshot := completeSnapshot()
	if _, err := Build(snapshot, testTarget(), "source-a", "source-a", time.Unix(0, 0)); err == nil {
		t.Fatal("reusing source_instance must fail")
	}

	badAdmin := completeSnapshot()
	badAdmin.Admin.NormalizedLogin = "other"
	if _, err := Build(badAdmin, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil {
		t.Fatal("non-admin selected identity must fail")
	}

	unknown := completeSnapshot()
	unknown.Tables = append(unknown.Tables, TableInventory{Name: "future_unknown"})
	plan, err := Build(unknown, testTarget(), "source-a", "source-b", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("Build unknown inventory: %v", err)
	}
	if plan.ScopeComplete || plan.Summary.ReviewRequiredCount != 1 {
		t.Fatalf("unknown table did not fail closed: %+v", plan.Summary)
	}
}

func TestBuildSupportsExactSchema68AndRejectsPreviousOrFutureMax(t *testing.T) {
	current := completeSnapshot()
	plan, err := Build(current, testTarget(), "source-a", "source-b", time.Unix(0, 0))
	if err != nil {
		t.Fatalf("Build schema 68: %v", err)
	}
	if got := plan.Schema.MigrationVersions[len(plan.Schema.MigrationVersions)-1]; got != schemaauthority.LatestSupportedMigration {
		t.Fatalf("latest migration = %q, want %q", got, schemaauthority.LatestSupportedMigration)
	}
	if plan.Summary.TableCount != 102 || plan.Summary.ResetCandidateCount != 94 || plan.Summary.ReviewRequiredCount != 0 {
		t.Fatalf("schema 68 changed reviewed table classification: %+v", plan.Summary)
	}
	if plan.Schema.TriggerCount != len(resetDeleteTriggers)+4 {
		t.Fatalf("schema 68 compatibility trigger missing from reset contract: %+v", plan.Schema)
	}
	if plan.SelectedAdmin.OrganizationRole != "platform_admin" || plan.SelectedAdmin.Role != "platform_admin" {
		t.Fatalf("selected admin identity axes drifted: %+v", plan.SelectedAdmin)
	}

	previous := completeSnapshot()
	previous.MigrationVersions = previous.MigrationVersions[:len(previous.MigrationVersions)-1]
	if _, err := Build(previous, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "reset_support_migration") {
		t.Fatalf("previous schema as max error = %v", err)
	}

	future := completeSnapshot()
	future.MigrationVersions = append(future.MigrationVersions, "000069_future_contract")
	if _, err := Build(future, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "reset_support_migration") {
		t.Fatalf("future migration error = %v", err)
	}

	wrongAdminAxis := completeSnapshot()
	wrongAdminAxis.Admin.OrganizationRole = "member"
	if _, err := Build(wrongAdminAxis, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "selected_admin") {
		t.Fatalf("wrong selected admin organization role error = %v", err)
	}
}

func TestBuildRejectsUnreviewedFKCycleAndMissingGuardTrigger(t *testing.T) {
	cycle := completeSnapshot()
	cycle.ForeignKeys = append(cycle.ForeignKeys,
		ForeignKey{Name: "cycle_a", ChildTable: "channels", ParentTable: "api_keys"},
		ForeignKey{Name: "cycle_b", ChildTable: "api_keys", ParentTable: "channels"},
	)
	if _, err := Build(cycle, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "unreviewed_fk_cycle") {
		t.Fatalf("unreviewed FK cycle error = %v", err)
	}

	missingTrigger := completeSnapshot()
	missingTrigger.Triggers = missingTrigger.Triggers[1:]
	if _, err := Build(missingTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "guarded_delete_trigger") {
		t.Fatalf("missing guarded trigger error = %v", err)
	}

	missingCompatibilityTrigger := completeSnapshot()
	missingCompatibilityTrigger.Triggers = slices.DeleteFunc(missingCompatibilityTrigger.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == legacyRoleCompatibilityTrigger.Name
	})
	if _, err := Build(missingCompatibilityTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "legacy_role_compatibility_trigger") {
		t.Fatalf("missing legacy-role compatibility trigger error = %v", err)
	}

	missingProjectMemberCompatibilityTrigger := completeSnapshot()
	missingProjectMemberCompatibilityTrigger.Triggers = slices.DeleteFunc(missingProjectMemberCompatibilityTrigger.Triggers, func(trigger TriggerState) bool {
		return trigger.Name == projectMemberRoleCompatibilityTrigger.Name
	})
	if _, err := Build(missingProjectMemberCompatibilityTrigger, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "project_member_role_compatibility_trigger") {
		t.Fatalf("missing project-member compatibility trigger error = %v", err)
	}

	restrictCycle := completeSnapshot()
	restrictCycle.ForeignKeys[0].DeleteAction = "RESTRICT"
	if _, err := Build(restrictCycle, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "deferred_cycle_constraint") {
		t.Fatalf("non-deferable delete action error = %v", err)
	}

	unsafeSequence := completeSnapshot()
	unsafeSequence.Sequences[0].NextValue = unsafeSequence.Sequences[0].MaxObservedValue
	if _, err := Build(unsafeSequence, testTarget(), "source-a", "source-b", time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "sequence_state") {
		t.Fatalf("sequence reuse risk error = %v", err)
	}
}

func TestPlanUnmarshalRejectsUnknownAndTrailingJSON(t *testing.T) {
	plan, err := Build(completeSnapshot(), testTarget(), "source-a", "source-b", time.Unix(0, 0))
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
		if scope, _, _ := ClassifyTable(name); scope == ScopeReviewRequired {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	if len(unknown) != 0 {
		t.Fatalf("migration tables missing explicit reset classification: %v", unknown)
	}
}

func completeSnapshot() Snapshot {
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
		MigrationVersions: []string{
			"000056_prd8_invocation_optimization",
			"000057_second_integration_reset_support",
			schemaauthority.LatestSupportedMigration,
		},
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
