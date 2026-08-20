package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestModelOperationProductionQualificationMigrationFreezesExactAuthority(t *testing.T) {
	upBytes, err := os.ReadFile("../../migrations/000075_model_operation_production_qualification.up.sql")
	if err != nil {
		t.Fatalf("read migration 75 up: %v", err)
	}
	up := string(upBytes)
	beginOffset := strings.Index(up, "BEGIN;")
	advisoryOffset := strings.Index(up, modelExecutionMigrationAdvisoryLock)
	lockOffset := strings.Index(up, "LOCK TABLE ")
	guardOffset := strings.Index(up, "pre-authority production approval exists")
	if beginOffset < 0 || advisoryOffset <= beginOffset || lockOffset <= advisoryOffset || guardOffset <= lockOffset {
		t.Fatalf("migration 75 up lock/guard order invalid: begin=%d advisory=%d lock=%d guard=%d", beginOffset, advisoryOffset, lockOffset, guardOffset)
	}
	for _, required := range []string{
		"model_operation_execution_support_bindings",
		"authority_host LIKE '%.%'",
		"parsed_ip <<= '::ffff:0:0/96'::INET",
		"'openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v1','1.0.0'",
		"'anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text.v1','1.0.0'",
		"'unirouter.endpoint-profile-nonstream-executor.v1'",
		"admin.endpointprotocol_live_probe.v1",
		"model_operation_production_approvals",
		"FOREIGN KEY (customer_org_id,deployment_id)",
		"REFERENCES model_deployments(customer_org_id,id)",
		"credential_binding_id BIGINT NOT NULL",
		"credential_state_revision BIGINT NOT NULL",
		"credential_link_created_at TIMESTAMPTZ(6) NOT NULL",
		"FOR UPDATE OF probe,operation,deployment,connection,binding,credential_link",
		"proof.credential_cooldown_until>transaction_timestamp()",
		"production approval customer root authority is stale or ambiguous",
		"approved_probe.credential_binding_id IS DISTINCT FROM approval.credential_binding_id",
		"model_operation_execution_qualifications",
		"public_path IS NOT NULL AND executor_contract_version IS NOT NULL",
		"runtime_change_releases_p3_qualification_guard",
		"Responses/Messages runtime manifest lacks current production qualification",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("migration 75 up is missing %q", required)
		}
	}
	if strings.Count(up, modelExecutionMigrationAdvisoryLock) != 1 {
		t.Fatal("migration 75 up must acquire the lifecycle advisory lock exactly once")
	}
	if strings.Count(up, "CONSTRAINT model_operation_production_approvals_deployment_fk") != 1 ||
		strings.Count(up, "CREATE FUNCTION apply_model_operation_production_approval()") != 1 {
		t.Fatal("migration 75 up contains a duplicated approval definition")
	}
	successFence := strings.Index(up, "IF NEW.success AND (")
	profileFence := strings.Index(up, "OR NEW.probe_profile_id IS DISTINCT FROM operation_row.protocol_profile_id")
	if successFence < 0 || profileFence <= successFence {
		t.Fatal("only successful Responses/Messages probes may require the still-current operation/profile revisions")
	}
}

func TestModelOperationProductionQualificationDownGuardsAllEvidenceBeforeDrop(t *testing.T) {
	downBytes, err := os.ReadFile("../../migrations/000075_model_operation_production_qualification.down.sql")
	if err != nil {
		t.Fatalf("read migration 75 down: %v", err)
	}
	down := string(downBytes)
	beginOffset := strings.Index(down, "BEGIN;")
	advisoryOffset := strings.Index(down, modelExecutionMigrationAdvisoryLock)
	lockOffset := strings.Index(down, "LOCK TABLE ")
	guardOffset := strings.Index(down, "DO $$")
	dropOffset := strings.Index(down, "DROP TABLE model_operation_production_approvals")
	if beginOffset < 0 || advisoryOffset <= beginOffset || lockOffset <= advisoryOffset || guardOffset <= lockOffset || dropOffset <= guardOffset {
		t.Fatalf("migration 75 down order invalid: begin=%d advisory=%d lock=%d guard=%d drop=%d", beginOffset, advisoryOffset, lockOffset, guardOffset, dropOffset)
	}
	for _, required := range []string{
		"version>'000075_model_operation_production_qualification'",
		"append-only approvals exist",
		"frozen network probe evidence exists",
		"sealed qualification membership exists",
		"operation_revision IS NOT NULL OR public_path IS NOT NULL",
		"executor_contract_version IS NOT NULL OR https_origin IS NOT NULL",
	} {
		if !strings.Contains(down, required) {
			t.Fatalf("migration 75 down is missing %q", required)
		}
	}
}

func TestCISelectsModelOperationProductionQualificationPostgresContracts(t *testing.T) {
	workflowBytes, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	workflow := string(workflowBytes)
	const selector = "TestModelOperation(HTTPSOriginGoSQLGoldenVectors|ProductionQualification(DatabaseGuards|DownRejects(FrozenEvidence|FutureM76)))Integration"
	if !strings.Contains(workflow, selector) {
		t.Fatalf("CI consolidated integration gate does not select M75 PostgreSQL contracts with %q", selector)
	}
	if !strings.Contains(workflow, "go test -tags=integration -race") || !strings.Contains(workflow, "./internal/migrate") {
		t.Fatal("CI M75 selector is not backed by the tagged race PostgreSQL migration package gate")
	}
}

func TestRuntimeRouteItemGuardsSerializeWithReleaseSeal(t *testing.T) {
	manifestBytes, err := os.ReadFile("../../migrations/000070_runtime_deployment_route_manifest.up.sql")
	if err != nil {
		t.Fatalf("read migration 70 up: %v", err)
	}
	manifest := string(manifestBytes)
	identityStart := strings.Index(manifest, "CREATE FUNCTION enforce_runtime_route_item_identity()")
	identityEnd := strings.Index(manifest, "CREATE TRIGGER runtime_route_revision_items_seal_guard")
	if identityStart < 0 || identityEnd <= identityStart {
		t.Fatal("migration 70 route item identity guard is missing")
	}
	identityGuard := manifest[identityStart:identityEnd]
	if strings.Contains(identityGuard, "FOR KEY SHARE") || strings.Count(identityGuard, "FOR SHARE;") != 2 {
		t.Fatal("migration 70 route item guard must hold a parent FOR SHARE lock for source and target rows")
	}

	qualificationBytes, err := os.ReadFile("../../migrations/000075_model_operation_production_qualification.up.sql")
	if err != nil {
		t.Fatalf("read migration 75 up: %v", err)
	}
	qualification := string(qualificationBytes)
	qualificationStart := strings.Index(qualification, "CREATE FUNCTION enforce_runtime_route_item_production_qualification()")
	qualificationEnd := strings.Index(qualification, "CREATE TRIGGER runtime_route_revision_items_p3_qualification_guard")
	if qualificationStart < 0 || qualificationEnd <= qualificationStart {
		t.Fatal("migration 75 route item qualification guard is missing")
	}
	qualificationGuard := qualification[qualificationStart:qualificationEnd]
	if strings.Contains(qualificationGuard, "FOR KEY SHARE") || !strings.Contains(qualificationGuard, "FOR SHARE;") {
		t.Fatal("migration 75 route item guard must share the parent seal serialization boundary")
	}
}
