package migrate_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionedUnitMeterBillingMigrationContracts(t *testing.T) {
	upBody, err := os.ReadFile("../../migrations/000083_versioned_unit_meter_billing.up.sql")
	require.NoError(t, err)
	up := string(upBody)
	require.Contains(t, up, "effective_unit_rates JSONB NOT NULL DEFAULT '[]'::jsonb")
	require.Contains(t, up, "reserved_meter_usage JSONB NOT NULL DEFAULT '[]'::jsonb")
	require.Contains(t, up, "non_token_cost_micros BIGINT NOT NULL DEFAULT 0")
	require.Contains(t, up, "unit_contract IN ('tokens', 'versioned_rate_card_components.v1')")
	require.NotContains(t, strings.ToLower(up), "float")
	require.NotContains(t, strings.ToLower(up), "cache_read_ratio")
	require.NotContains(t, strings.ToLower(up), "numeric(10,6)")

	beginOffset := strings.Index(up, "BEGIN;")
	advisoryOffset := strings.Index(up, "SELECT pg_advisory_xact_lock(86420517831702491);")
	lockOffset := strings.Index(up, "LOCK TABLE ")
	alterOffset := strings.Index(up, "ALTER TABLE freeze_idempotency")
	require.Greater(t, advisoryOffset, beginOffset)
	require.Greater(t, lockOffset, advisoryOffset)
	require.Greater(t, alterOffset, lockOffset)

	downBody, err := os.ReadFile("../../migrations/000083_versioned_unit_meter_billing.down.sql")
	require.NoError(t, err)
	down := string(downBody)
	require.Contains(t, down, "version > '000083_versioned_unit_meter_billing'")
	require.Contains(t, down, "versioned component source evidence exists")
	require.Contains(t, down, "CHECK (unit_contract = 'tokens')")
	require.Contains(t, down, "durable meter evidence exists")
	require.Less(t, strings.Index(down, "durable meter evidence exists"), strings.Index(down, "ALTER TABLE call_logs"))
	require.Less(t, strings.Index(down, "ALTER TABLE freeze_idempotency"), strings.Index(down, "DELETE FROM schema_migrations"))
}
