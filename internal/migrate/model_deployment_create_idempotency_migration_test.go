package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestModelDeploymentCreateIdempotencyDownFreezesWritersBeforeEvidenceChecks(t *testing.T) {
	down, err := os.ReadFile("../../migrations/000074_model_deployment_create_idempotency.down.sql")
	if err != nil {
		t.Fatalf("read migration 74 down: %v", err)
	}
	wire := string(down)
	beginOffset := strings.Index(wire, "BEGIN;")
	advisoryOffset := strings.Index(wire, "SELECT pg_advisory_xact_lock(86420517831702491);")
	lockOffset := strings.Index(wire, "LOCK TABLE model_deployment_create_receipts IN ACCESS EXCLUSIVE MODE")
	guardOffset := strings.Index(wire, "DO $$")
	dropOffset := strings.Index(wire, "DROP TABLE model_deployment_create_receipts")
	if beginOffset < 0 || advisoryOffset <= beginOffset || lockOffset <= advisoryOffset || guardOffset <= lockOffset || dropOffset <= guardOffset {
		t.Fatalf("migration 74 down order invalid: begin=%d advisory=%d table-lock=%d guard=%d drop=%d",
			beginOffset, advisoryOffset, lockOffset, guardOffset, dropOffset)
	}
	if strings.Count(wire, "pg_advisory_xact_lock(86420517831702491)") != 1 {
		t.Fatal("migration 74 down must acquire the shared migration advisory lock exactly once")
	}
	if between := strings.TrimSpace(wire[beginOffset+len("BEGIN;") : advisoryOffset]); between != "" {
		t.Fatalf("shared migration advisory lock must be the first rollback statement after BEGIN: %q", between)
	}
	for _, guard := range []string{
		"version > '000074_model_deployment_create_idempotency'",
		"IF EXISTS (SELECT 1 FROM model_deployment_create_receipts)",
		"durable mutation receipts exist",
	} {
		if !strings.Contains(wire, guard) {
			t.Fatalf("migration 74 down is missing fail-closed guard %q", guard)
		}
	}
}

func TestModelDeploymentCreateIdempotencyCompletedReceiptRequiresNonNullResultIdentity(t *testing.T) {
	up, err := os.ReadFile("../../migrations/000074_model_deployment_create_idempotency.up.sql")
	if err != nil {
		t.Fatalf("read migration 74 up: %v", err)
	}
	wire := string(up)
	for _, required := range []string{
		"deployment_id IS NOT NULL",
		"model_operation_id IS NOT NULL",
		"original_http_status IS NOT NULL",
	} {
		if !strings.Contains(wire, required) {
			t.Fatalf("completed receipt state check is missing %q", required)
		}
	}
}
