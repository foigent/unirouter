package migrate

import (
	"os"
	"strings"
	"testing"
)

func TestObservationProviderCostReconciliationMigrationFreezesExactTerminalAuthority(t *testing.T) {
	body, err := os.ReadFile("../../migrations/000080_observation_provider_cost_reconciliation.up.sql")
	if err != nil {
		t.Fatalf("read migration 80 up: %v", err)
	}
	wire := string(body)
	lockOffset := strings.Index(wire, "LOCK TABLE freeze_idempotency")
	alterOffset := strings.Index(wire, "ALTER TABLE billing_observation_receipts")
	if lockOffset < 0 || alterOffset <= lockOffset {
		t.Fatalf("migration 80 must stabilize billing writers before schema mutation: lock=%d alter=%d", lockOffset, alterOffset)
	}
	for _, required := range []string{
		"terminal_observation_receipt_id BIGINT",
		"terminal_transaction_id IS NOT NULL AND terminal_observation_receipt_id IS NULL",
		"terminal_transaction_id IS NULL AND terminal_observation_receipt_id IS NOT NULL",
		"provider_cost_reconciliations_observation_receipt_fk",
		"REFERENCES billing_observation_receipts(org_id, request_id, id)",
		"idx_provider_cost_reconciliations_one_completed",
		"completed provider reconciliation lacks exact requested authority",
		"observation reconciliation does not match its terminal reservation and attempt",
		"observation_calculated IS DISTINCT FROM reservation_calculated",
		"observation_pricing_version IS DISTINCT FROM reservation_pricing_version",
		"observation_usage_authority IS DISTINCT FROM reservation_usage_authority",
		"provider_cost_reconciliation_append_only",
	} {
		if !strings.Contains(wire, required) {
			t.Fatalf("migration 80 up is missing %q", required)
		}
	}
	for _, forbidden := range []string{"DISABLE TRIGGER", "UPDATE provider_cost_reconciliations SET"} {
		if strings.Contains(wire, forbidden) {
			t.Fatalf("migration 80 must not backfill evidence or install a trigger bypass: %q", forbidden)
		}
	}
}

func TestObservationProviderCostReconciliationDownRefusesDurableEvidenceAndRestoresM79(t *testing.T) {
	body, err := os.ReadFile("../../migrations/000080_observation_provider_cost_reconciliation.down.sql")
	if err != nil {
		t.Fatalf("read migration 80 down: %v", err)
	}
	wire := string(body)
	guardOffset := strings.Index(wire, "DO $$")
	dropOffset := strings.Index(wire, "DROP TRIGGER provider_cost_reconciliation_append_only")
	ledgerOffset := strings.Index(wire, "WHERE version='000080_observation_provider_cost_reconciliation'")
	if guardOffset < 0 || dropOffset <= guardOffset || ledgerOffset <= dropOffset {
		t.Fatalf("migration 80 down guard/drop/ledger order invalid: %d/%d/%d", guardOffset, dropOffset, ledgerOffset)
	}
	for _, required := range []string{
		"version>'000080_observation_provider_cost_reconciliation'",
		"terminal_observation_receipt_id IS NOT NULL",
		"durable observation reconciliation evidence exists",
		"DROP CONSTRAINT provider_cost_reconciliations_observation_receipt_fk",
		"DROP COLUMN terminal_observation_receipt_id",
		"phase = 'completed' AND terminal_transaction_id IS NOT NULL",
	} {
		if !strings.Contains(wire, required) {
			t.Fatalf("migration 80 down is missing %q", required)
		}
	}
}
