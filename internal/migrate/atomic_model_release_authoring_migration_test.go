package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestAtomicModelReleaseAuthoringMigrationFreezesExactReceiptAndProvenance(t *testing.T) {
	wire, err := os.ReadFile("../../migrations/000078_atomic_model_release_authoring.up.sql")
	if err != nil {
		t.Fatalf("read migration 78 up: %v", err)
	}
	up := string(wire)
	beginOffset := strings.Index(up, "BEGIN;")
	advisoryOffset := strings.Index(up, modelExecutionMigrationAdvisoryLock)
	lockOffset := strings.Index(up, "LOCK TABLE ")
	legacyDraftGuardOffset := strings.Index(up, "seal every canonical v2 draft with the M77 artifact SealDeploymentDraft")
	firstSchemaMutationOffset := strings.Index(up, "ALTER TABLE model_deployment_lifecycle_receipts")
	createOffset := strings.Index(up, "CREATE TABLE model_deployment_release_authoring_receipts")
	if beginOffset < 0 || advisoryOffset <= beginOffset || lockOffset <= advisoryOffset ||
		legacyDraftGuardOffset <= lockOffset || firstSchemaMutationOffset <= legacyDraftGuardOffset ||
		createOffset <= firstSchemaMutationOffset {
		t.Fatalf("migration 78 lock/guard/mutation/create order invalid: begin=%d advisory=%d lock=%d guard=%d mutation=%d create=%d",
			beginOffset, advisoryOffset, lockOffset, legacyDraftGuardOffset,
			firstSchemaMutationOffset, createOffset)
	}
	lock := up[lockOffset:createOffset]
	releaseOffset := strings.Index(lock, "runtime_change_releases")
	slotOffset := strings.Index(lock, "runtime_change_slots")
	orgOffset := strings.Index(lock, "orgs")
	if releaseOffset < 0 || slotOffset <= releaseOffset || orgOffset <= slotOffset {
		t.Fatalf("migration 78 release/slot/org lock order invalid: %d/%d/%d", releaseOffset, slotOffset, orgOffset)
	}
	for _, required := range []string{
		"unirouter.model-deployment-release-authoring-receipt.v1",
		"release.route_items_sealed_at IS NULL OR release.stage='draft'",
		"the M78 binary cannot recover an M77 draft",
		"model_deployment.release_authoring.create.v1",
		"baseline_kind IN ('stable','legacy_cutover')",
		"stable.candidate_version='unirouter.runtime-change-candidate.v1'",
		"'cutover_legacy','rollback_legacy'",
		"before_slot_public_model_id",
		"after_slot_public_model_id",
		"legacy_plan_hash",
		"deployment_plan_hash",
		"preserve_legacy_model_deployment_lifecycle_receipt",
		"legacy deployment lifecycle completion requires exact raw after slot",
		"legacy deployment lifecycle authority expansion requires current production qualification",
		"UNIQUE (customer_org_id,actor_user_id,operation_scope,idempotency_key_hash)",
		"ADD COLUMN authoring_receipt_id BIGINT",
		"new canonical v2 create and seal require an in-progress authoring receipt",
		"CREATE CONSTRAINT TRIGGER model_deployment_release_authoring_receipts_terminal",
		"DEFERRABLE INITIALLY DEFERRED",
		"jsonb_array_elements(release.candidate->'deployment_routes')",
		"WITH ORDINALITY",
		"SELECT * FROM runtime_route_revision_items source_item",
		"SELECT * FROM model_operation_public_execution_bindings source_binding",
		"to_jsonb(item.expected_connection_revision)",
		"bidirectional field-exact candidate items",
		"bidirectional exact and authorized public bindings",
		"authoring_receipt_id",
		"lifecycle_receipt_id",
		"canonical v2 runtime event requires exactly one receipt provenance",
		"event_count<>2",
		"created_by_org_id>0",
		"proof.actor_role<>'platform_admin'",
		"proof.actor_role<>'org_admin'",
		"completion requires an active authorized actor",
		"jsonb_object_keys(release.candidate)",
		"stable.candidate_version='unirouter.runtime-change-candidate.v2'",
		"model_operation_public_execution_qualifications public_qualification",
		"public_qualification.public_executable IS DISTINCT FROM TRUE",
		"binding.source_kind IS DISTINCT FROM 'runtime.public_execution_binding.v1'",
		"original_http_status=201",
		"original_location='/api/v1/model-deployment-releases/' || target_release_ref",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("migration 78 up is missing %q", required)
		}
	}
	if strings.Contains(up, "expires_at") {
		t.Fatal("M78 durable authoring receipt must not expire")
	}
	if strings.Contains(up, "SET before_slot_public_model_id") ||
		strings.Contains(up, "SET after_slot_public_model_id") {
		t.Fatal("M78 must not backfill legacy-only raw slot identity onto canonical M76 receipts")
	}
	if !strings.Contains(up, "action NOT IN ('cutover_legacy','rollback_legacy')\n                    AND before_slot_public_model_id IS NULL") {
		t.Fatal("M78 canonical receipts must preserve the M76 shape with legacy-only raw slot identity absent")
	}
	if strings.Count(up, modelExecutionMigrationAdvisoryLock) != 1 {
		t.Fatal("migration 78 up must acquire the shared advisory lock exactly once")
	}
}

func TestAtomicModelReleaseAuthoringDownRequiresEmptyEvidenceAndRestoresM77(t *testing.T) {
	wire, err := os.ReadFile("../../migrations/000078_atomic_model_release_authoring.down.sql")
	if err != nil {
		t.Fatalf("read migration 78 down: %v", err)
	}
	down := string(wire)
	guardOffset := strings.Index(down, "DO $$")
	dropOffset := strings.Index(down, "DROP TABLE model_deployment_release_authoring_receipts")
	restoreOffset := strings.Index(down, "created_by>0 AND created_by_org_id=customer_org_id")
	if guardOffset < 0 || dropOffset <= guardOffset || restoreOffset <= dropOffset {
		t.Fatalf("migration 78 down guard/drop/restore order invalid: %d/%d/%d", guardOffset, dropOffset, restoreOffset)
	}
	for _, required := range []string{
		"version>'000078_atomic_model_release_authoring'",
		"durable authoring evidence exists",
		"durable legacy lifecycle evidence exists",
		"DROP COLUMN before_slot_public_model_id",
		"DROP COLUMN deployment_plan_hash",
		"SELECT 1 FROM model_operation_public_execution_bindings",
		"public execution bindings must be empty before restoring M77 creator authority",
		"DROP COLUMN authoring_receipt_id",
		"CREATE OR REPLACE FUNCTION preserve_runtime_change_event_receipt_link()",
		"runtime change event requires an in-progress lifecycle receipt",
		"created_by>0 AND created_by_org_id=customer_org_id",
		"DELETE FROM schema_migrations",
	} {
		if !strings.Contains(down, required) {
			t.Fatalf("migration 78 down is missing %q", required)
		}
	}
	if strings.Count(down, modelExecutionMigrationAdvisoryLock) != 1 {
		t.Fatal("migration 78 down must acquire the shared advisory lock exactly once")
	}
}
