package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestModelCapabilityExecutionQualificationMigrationSeparatesSelectionFromBoundValidation(t *testing.T) {
	upBytes, err := os.ReadFile("../../migrations/000088_model_capability_execution_qualification.up.sql")
	if err != nil {
		t.Fatalf("read migration 88 up: %v", err)
	}
	up := string(upBytes)
	for _, required := range []string{
		"model_capability_probe_contract_bindings",
		"model_capability_current_probe_contracts",
		"model_capability_probes",
		"model_capability_routing_approvals",
		"model_capability_approval_qualifications",
		"model_capability_execution_qualifications",
		"approved_at DESC,approval_id DESC",
		"Pre-dispatch validates the receipt already bound to the frozen Plan",
		"capability_approval:",
		"capability evidence projection lacks a current typed receipt",
		"current model capability probe contracts are migration-owned",
		"expected_contract_provider",
		"'protocol.deepseek-openai-chat.v4','1.0.0'",
		"'openai.chat_completions','1.0.0'",
		"BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_probe_contract_bindings",
		"BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_current_probe_contracts",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("migration 88 up is missing %q", required)
		}
	}
	preferredStart := strings.Index(up, "CREATE VIEW model_capability_execution_qualifications")
	boundStart := strings.Index(up, "CREATE VIEW model_capability_approval_qualifications")
	if boundStart < 0 || preferredStart <= boundStart {
		t.Fatalf("per-receipt qualification must exist before preferred selection: bound=%d preferred=%d", boundStart, preferredStart)
	}
	qualificationSQL := up[boundStart:preferredStart]
	if strings.Contains(qualificationSQL, "current_credential_state_revision") || strings.Contains(qualificationSQL, "current_credential_cooldown") {
		t.Fatal("current capability qualification must not bind transient credential health state")
	}
}

func TestModelCapabilityExecutionQualificationDownGuardsEvidenceBeforeDrop(t *testing.T) {
	downBytes, err := os.ReadFile("../../migrations/000088_model_capability_execution_qualification.down.sql")
	if err != nil {
		t.Fatalf("read migration 88 down: %v", err)
	}
	down := string(downBytes)
	guard := strings.Index(down, "cannot remove M88 while capability qualification evidence exists")
	drop := strings.Index(down, "DROP TABLE model_capability_routing_approvals")
	if guard < 0 || drop <= guard {
		t.Fatalf("migration 88 down must guard evidence before drop: guard=%d drop=%d", guard, drop)
	}
	for _, required := range []string{
		"version>'000088_model_capability_execution_qualification'",
		"capability_probe:%",
		"capability_approval:%",
		"route_capability_evidence::TEXT",
	} {
		if !strings.Contains(down, required) {
			t.Fatalf("migration 88 down is missing %q", required)
		}
	}
}

func TestModelCapabilityExecutionQualificationIntegrationIsSelectedByCI(t *testing.T) {
	workflow := consolidatedIntegrationSource(t)
	for _, required := range []string{
		"TestModelCapabilityQualification.*Integration",
		"TestPlannerAndCutoverSelectTheSamePreferredCapabilityApprovalIntegration",
		"TestPreDispatchValidatesBoundCapabilityApprovalInsteadOfLatestIntegration",
		"./internal/modelrouting",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("CI consolidated integration gate is missing %q", required)
		}
	}
}
