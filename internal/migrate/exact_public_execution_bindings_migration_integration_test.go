//go:build integration

package migrate_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestExactPublicExecutionBindingsConstraintRoundTripIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000085_model_execution_plan_freeze_fence.up.sql")
	names := []string{"billing_provider_attempts_execution_evidence_check", "call_logs_execution_projection_check", "request_trace_events_execution_projection_check"}
	definitions := func() []string {
		var result []string
		for _, name := range names {
			var definition string
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname=$1`, name).Scan(&definition))
			result = append(result, definition)
		}
		return result
	}
	before := definitions()
	for cycle := 0; cycle < 2; cycle++ {
		require.NoError(t, migrate.RunThrough(t.Context(), pool, testutil.MigrationsDir(t), "000086_exact_public_execution_bindings"))
		for index, definition := range definitions() {
			require.NotEqual(t, before[index], definition, names[index])
			require.Contains(t, definition, "COALESCE(btrim((route_delivery_contract_version)::text)", names[index])
			require.Contains(t, definition, "COALESCE(route_charge_contract_version", names[index])
			require.Contains(t, definition, "COALESCE(route_idempotency_contract_version", names[index])
		}
		execMigrationDown(t, t.Context(), pool, "000086_exact_public_execution_bindings.down.sql")
		require.Equal(t, before, definitions(), "down must restore all M77 predicates without changing historical authority")
	}
}

func TestExactPublicExecutionBindingsMigrationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	type tuple struct {
		operation string
		profile   string
		version   string
		executor  string
		delivery  string
	}
	rows, err := pool.Query(ctx, `
SELECT operation_id,protocol_profile_id,protocol_profile_version,
       executor_contract_version,delivery_contract_version
  FROM model_operation_exact_public_execution_contracts
 ORDER BY operation_id,protocol_profile_id`)
	require.NoError(t, err)
	defer rows.Close()
	var got []tuple
	for rows.Next() {
		var value tuple
		require.NoError(t, rows.Scan(
			&value.operation, &value.profile, &value.version,
			&value.executor, &value.delivery,
		))
		got = append(got, value)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []tuple{
		{
			operation: "anthropic.messages.v1",
			profile:   "unirouter.anthropic.messages.text-tools.v3",
			version:   "1.0.0",
			executor:  "unirouter.endpoint-profile-messages-tools-executor.v1",
			delivery:  "unirouter.public-anthropic-messages-tools-delivery.v1",
		},
		{
			operation: "anthropic.messages.v1",
			profile:   "unirouter.anthropic.messages.text.v1",
			version:   "1.0.0",
			executor:  "unirouter.endpoint-profile-nonstream-executor.v1",
			delivery:  "unirouter.public-nonstream-delivery.v1",
		},
		{
			operation: "anthropic.messages.v1",
			profile:   "unirouter.anthropic.messages.text.v2",
			version:   "1.0.0",
			executor:  "unirouter.endpoint-profile-messages-dual-executor.v1",
			delivery:  "unirouter.public-anthropic-messages-delivery.v2",
		},
		{
			operation: "openai.responses.v1",
			profile:   "unirouter.openai.responses.text.v1",
			version:   "1.0.0",
			executor:  "unirouter.endpoint-profile-nonstream-executor.v1",
			delivery:  "unirouter.public-nonstream-delivery.v1",
		},
		{
			operation: "openai.responses.v1",
			profile:   "unirouter.openai.responses.text.v2",
			version:   "1.0.0",
			executor:  "unirouter.endpoint-profile-responses-dual-executor.v1",
			delivery:  "unirouter.public-responses-delivery.v2",
		},
	}, got)

	var exactSupport, compatibilitySupport, exactPublic, compatibilityPublic int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM model_operation_exact_execution_support_bindings),
  (SELECT COUNT(*) FROM model_operation_current_execution_support_bindings),
  (SELECT COUNT(*) FROM model_operation_exact_public_execution_contracts),
	  (SELECT COUNT(*) FROM model_operation_current_public_execution_contracts)`,
	).Scan(&exactSupport, &compatibilitySupport, &exactPublic, &compatibilityPublic))
	require.Equal(t, 5, exactSupport)
	require.Equal(t, exactSupport, compatibilitySupport)
	require.Equal(t, 5, exactPublic)
	require.Equal(t, exactPublic, compatibilityPublic)

	_, err = pool.Exec(ctx, `
INSERT INTO model_operation_execution_support_bindings (
  operation_id,public_path,protocol_profile_id,protocol_profile_version,
  executor_contract_version,introduced_migration
) VALUES (
  'openai.responses.v1','/v1/responses','unirouter.openai.responses.text.v3','1.0.0',
  'unirouter.endpoint-profile-responses-dual-executor.v1','test'
)`)
	require.ErrorContains(t, err, "migration-owned and immutable")
}
