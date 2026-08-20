package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestModelDeploymentLifecycleReceiptMigrationSealsExactCompletionEvidence(t *testing.T) {
	upBytes, err := os.ReadFile("../../migrations/000076_model_deployment_lifecycle_receipts.up.sql")
	if err != nil {
		t.Fatalf("read migration 76 up: %v", err)
	}
	up := string(upBytes)
	beginOffset := strings.Index(up, "BEGIN;")
	advisoryOffset := strings.Index(up, modelExecutionMigrationAdvisoryLock)
	lockOffset := strings.Index(up, "LOCK TABLE ")
	createOffset := strings.Index(up, "CREATE TABLE model_deployment_lifecycle_receipts")
	if beginOffset < 0 || advisoryOffset <= beginOffset || lockOffset <= advisoryOffset || createOffset <= lockOffset {
		t.Fatalf("migration 76 lock/create order invalid: begin=%d advisory=%d lock=%d create=%d",
			beginOffset, advisoryOffset, lockOffset, createOffset)
	}
	upLock := up[lockOffset:createOffset]
	upReleaseLock := strings.Index(upLock, "runtime_change_releases")
	upSlotLock := strings.Index(upLock, "runtime_change_slots")
	upEventLock := strings.Index(upLock, "runtime_change_events")
	if upReleaseLock < 0 || upSlotLock <= upReleaseLock || upEventLock <= upSlotLock {
		t.Fatalf("migration 76 up runtime lock order invalid: release=%d slot=%d event=%d",
			upReleaseLock, upSlotLock, upEventLock)
	}
	for _, required := range []string{
		"unirouter.model-deployment-lifecycle-receipt.v1",
		"UNIQUE (customer_org_id,actor_user_id,operation_scope,idempotency_key_hash)",
		"CREATE TABLE model_deployment_lifecycle_receipt_releases",
		"CREATE TABLE model_deployment_lifecycle_receipt_qualifications",
		"model deployment lifecycle receipt must be claimed in progress",
		"BEFORE INSERT OR UPDATE OR DELETE ON model_deployment_lifecycle_receipts",
		"model deployment lifecycle completion requires the exact audit row",
		"audit.detail->>'server_operation_id'=NEW.server_operation_id",
		"model deployment lifecycle completion requires exact release cardinality",
		"release.candidate_version='unirouter.runtime-change-candidate.v2'",
		"evidence.release_id=target_parent_release_id",
		"model deployment lifecycle completion requires the exact live after slot",
		"model deployment lifecycle completion requires exact lifecycle events",
		"model deployment lifecycle completion requires exact qualification evidence",
		"model deployment lifecycle authority expansion requires current production qualification",
		"CREATE TRIGGER runtime_change_events_append_only",
		"BEFORE UPDATE OR DELETE OR TRUNCATE ON runtime_change_events",
		"runtime change events are append-only evidence",
		"CREATE TRIGGER runtime_change_events_receipt_link_guard",
		"BEFORE INSERT ON runtime_change_events",
		"runtime change event requires a positive lifecycle receipt identity",
		"runtime change event requires an in-progress lifecycle receipt",
		"CREATE CONSTRAINT TRIGGER model_deployment_lifecycle_receipts_terminal",
		"DEFERRABLE INITIALLY DEFERRED",
		"FOR SHARE;",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("migration 76 up is missing %q", required)
		}
	}
	childStart := strings.Index(up, "CREATE FUNCTION preserve_model_deployment_lifecycle_receipt_child()")
	childEnd := strings.Index(up, "CREATE TRIGGER model_deployment_lifecycle_receipt_releases_append_only")
	if childStart < 0 || childEnd <= childStart {
		t.Fatal("migration 76 child serialization function is missing")
	}
	childGuard := up[childStart:childEnd]
	if strings.Contains(childGuard, "FOR KEY SHARE") || !strings.Contains(childGuard, "FOR SHARE;") {
		t.Fatal("lifecycle receipt children must conflict with parent completion through FOR SHARE")
	}
	eventStart := strings.Index(up, "CREATE FUNCTION preserve_runtime_change_event_receipt_link()")
	eventEnd := strings.Index(up, "CREATE TRIGGER runtime_change_events_receipt_link_guard")
	if eventStart < 0 || eventEnd <= eventStart ||
		!strings.Contains(up[eventStart:eventEnd], "FOR SHARE;") {
		t.Fatal("receipt-linked event inserts must serialize with parent completion through FOR SHARE")
	}
	if strings.Count(up, modelExecutionMigrationAdvisoryLock) != 1 {
		t.Fatal("migration 76 up must acquire the shared migration advisory lock exactly once")
	}
}

func TestModelDeploymentLifecycleReceiptDownRefusesFutureOrDurableEvidenceBeforeDrop(t *testing.T) {
	downBytes, err := os.ReadFile("../../migrations/000076_model_deployment_lifecycle_receipts.down.sql")
	if err != nil {
		t.Fatalf("read migration 76 down: %v", err)
	}
	down := string(downBytes)
	beginOffset := strings.Index(down, "BEGIN;")
	advisoryOffset := strings.Index(down, modelExecutionMigrationAdvisoryLock)
	lockOffset := strings.Index(down, "IN ACCESS EXCLUSIVE MODE")
	guardOffset := strings.Index(down, "DO $$")
	dropOffset := strings.Index(down, "DROP TABLE model_deployment_lifecycle_receipt_qualifications")
	if beginOffset < 0 || advisoryOffset <= beginOffset || lockOffset <= advisoryOffset ||
		guardOffset <= lockOffset || dropOffset <= guardOffset {
		t.Fatalf("migration 76 down order invalid: begin=%d advisory=%d lock=%d guard=%d drop=%d",
			beginOffset, advisoryOffset, lockOffset, guardOffset, dropOffset)
	}
	downLockStart := strings.Index(down, "LOCK TABLE ")
	if downLockStart < 0 {
		t.Fatal("migration 76 down lock statement is missing")
	}
	downLock := down[downLockStart:lockOffset]
	downReleaseLock := strings.Index(downLock, "runtime_change_releases")
	downSlotLock := strings.Index(downLock, "runtime_change_slots")
	downEventLock := strings.Index(downLock, "runtime_change_events")
	if downReleaseLock < 0 || downSlotLock <= downReleaseLock ||
		downEventLock <= downSlotLock {
		t.Fatalf("migration 76 down runtime lock order invalid: release=%d slot=%d event=%d",
			downReleaseLock, downSlotLock, downEventLock)
	}
	for _, required := range []string{
		"version>'000076_model_deployment_lifecycle_receipts'",
		"later migrations must be reversed first",
		"durable lifecycle receipts exist",
		"DROP FUNCTION IF EXISTS preserve_model_deployment_lifecycle_receipt_child()",
		"DROP FUNCTION IF EXISTS preserve_model_deployment_lifecycle_receipt()",
		"DROP TRIGGER IF EXISTS runtime_change_events_append_only ON runtime_change_events",
		"DROP FUNCTION IF EXISTS reject_runtime_change_event_mutation()",
		"DROP TRIGGER IF EXISTS runtime_change_events_receipt_link_guard ON runtime_change_events",
		"DROP FUNCTION IF EXISTS preserve_runtime_change_event_receipt_link()",
	} {
		if !strings.Contains(down, required) {
			t.Fatalf("migration 76 down is missing %q", required)
		}
	}
}
