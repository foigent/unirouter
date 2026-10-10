//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

// These M44-M46 rollback cases must create their business rows in the schema
// they actually test. A current-head project would already have M118's
// irreversible budget-definition history before the rollback begins.
func setupM46RollbackPrestate(t *testing.T) *pgxpool.Pool {
	t.Helper()
	const head = "000046_v1_async_media_image_jobs"
	pool, _ := setupMigratedThrough(t, head+".up.sql")
	want, err := testutil.PreRetirementLedger(t)
	require.NoError(t, err)
	index := slices.Index(want, head)
	require.GreaterOrEqual(t, index, 0)
	var got []string
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT array_agg(version::text ORDER BY version) FROM schema_migrations`,
	).Scan(&got))
	require.Equal(t, want[:index+1], got, "the fixture must have exactly the historical M46 prefix")
	return pool
}

var enterpriseFinanceDownMigrations = []string{
	"000051_prd8_enterprise_finance_allocation_close.down.sql",
	"000050_prd8_enterprise_finance_rate_cards.down.sql",
	"000049_prd8_enterprise_finance_budgeting.down.sql",
}

var prd8RuntimeControlDownMigrations = []string{
	"000100_kimi_k26_function_tools_tool_index_normalizer.down.sql",
	"000099_kimi_k26_function_tools_probe_v3.down.sql",
	"000098_kimi_k26_nonstream_usage_probe_v2.down.sql",
	"000097_kimi_k26_stream_choice_usage_probe_v3.down.sql",
	"000096_kimi_k26_stream_probe_contract_v2.down.sql",
	"000095_kimi_k26_chat_core_qualification.down.sql",
	"000094_deepseek_v4_pro_chat_core_qualification.down.sql",
	"000093_deepseek_function_tool_probe_contract_v2.down.sql",
	"000092_deepseek_reasoning_tool_choice_contract.down.sql",
	"000091_deepseek_chat_tools_json_capabilities.down.sql",
	"000090_deepseek_chat_reasoning_capabilities.down.sql",
	"000089_deepseek_chat_sse_usage_contract_v2.down.sql",
	"000088_model_capability_execution_qualification.down.sql",
	"000087_anthropic_messages_tools_public_binding.down.sql",
	"000086_exact_public_execution_bindings.down.sql",
	"000085_model_execution_plan_freeze_fence.down.sql",
	"000084_official_rate_source_currency_drafts.down.sql",
	"000083_versioned_unit_meter_billing.down.sql",
	"000082_sealed_model_execution_contracts.down.sql",
	"000081_versioned_model_execution_profiles.down.sql",
	"000080_observation_provider_cost_reconciliation.down.sql",
	"000079_canonical_connection_license_authority.down.sql",
	"000078_atomic_model_release_authoring.down.sql",
	"000077_public_model_execution_authority.down.sql",
	"000076_model_deployment_lifecycle_receipts.down.sql",
	"000075_model_operation_production_qualification.down.sql",
	"000074_model_deployment_create_idempotency.down.sql",
	"000073_model_execution_dispatch_fence.down.sql",
	"000072_execution_evidence_projection.down.sql",
	"000071_model_execution_evidence.down.sql",
	"000070_runtime_deployment_route_manifest.down.sql",
	"000069_model_deployment_expand.down.sql",
	"000068_provider_reported_model_identity.down.sql",
	"000067_call_log_route_identity.down.sql",
	"000066_runtime_assignment_projection.down.sql",
	"000065_model_endpoint_projection.down.sql",
	"000064_rate_card_request_pricing_authority.down.sql",
	"000063_onprem_billing_mode.down.sql",
	"000062_xinsight_connector_redeem_lifecycle.down.sql",
	"000061_xinsight_connector_cp2a_phase_contract.down.sql",
	"000060_xinsight_connector_cp1.down.sql",
	"000059_enterprise_access_provisioning.down.sql",
	"000058_enterprise_project_role_separation.down.sql",
	"000057_second_integration_reset_support.down.sql",
	"000056_prd8_invocation_optimization.down.sql",
	"000055_prd8_deployment_profiles.down.sql",
	"000054_prd8_workload_applications.down.sql",
	"000053_prd8_model_call_authorization.down.sql",
	"000052_prd8_runtime_change_control.down.sql",
}

var enterpriseDirectoryDownMigrations = []string{
	"000048_prd8_enterprise_directory_people_accounts.down.sql",
	"000047_prd8_enterprise_directory_foundation.down.sql",
}

func rollbackEmptyMigrations(t *testing.T, pool *pgxpool.Pool, names []string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, testutil.RewindCapabilityPublicationsForLegacyMigrationTest(
		ctx, pool, testutil.PreRetirementMigrationsDir(t),
	))
	orderedNames := make([]string, 0, len(names)+4)
	seen := make(map[string]struct{}, len(names)+4)
	for _, name := range []string{
		"000100_kimi_k26_function_tools_tool_index_normalizer.down.sql",
		"000099_kimi_k26_function_tools_probe_v3.down.sql",
		"000098_kimi_k26_nonstream_usage_probe_v2.down.sql",
		"000097_kimi_k26_stream_choice_usage_probe_v3.down.sql",
		"000096_kimi_k26_stream_probe_contract_v2.down.sql",
		"000095_kimi_k26_chat_core_qualification.down.sql",
		"000094_deepseek_v4_pro_chat_core_qualification.down.sql",
		"000093_deepseek_function_tool_probe_contract_v2.down.sql",
		"000092_deepseek_reasoning_tool_choice_contract.down.sql",
		"000091_deepseek_chat_tools_json_capabilities.down.sql",
		"000090_deepseek_chat_reasoning_capabilities.down.sql",
		"000089_deepseek_chat_sse_usage_contract_v2.down.sql",
		"000088_model_capability_execution_qualification.down.sql",
		"000087_anthropic_messages_tools_public_binding.down.sql",
		"000086_exact_public_execution_bindings.down.sql",
		"000085_model_execution_plan_freeze_fence.down.sql",
		"000084_official_rate_source_currency_drafts.down.sql",
		"000083_versioned_unit_meter_billing.down.sql",
		"000082_sealed_model_execution_contracts.down.sql",
		"000081_versioned_model_execution_profiles.down.sql",
		"000080_observation_provider_cost_reconciliation.down.sql",
		"000079_canonical_connection_license_authority.down.sql",
		"000078_atomic_model_release_authoring.down.sql",
		"000077_public_model_execution_authority.down.sql",
		"000076_model_deployment_lifecycle_receipts.down.sql",
		"000075_model_operation_production_qualification.down.sql",
		"000074_model_deployment_create_idempotency.down.sql",
		"000073_model_execution_dispatch_fence.down.sql",
		"000072_execution_evidence_projection.down.sql",
		"000071_model_execution_evidence.down.sql",
		"000070_runtime_deployment_route_manifest.down.sql",
		"000069_model_deployment_expand.down.sql",
		"000068_provider_reported_model_identity.down.sql",
		"000067_call_log_route_identity.down.sql",
		"000066_runtime_assignment_projection.down.sql",
		"000065_model_endpoint_projection.down.sql",
		"000064_rate_card_request_pricing_authority.down.sql",
		"000063_onprem_billing_mode.down.sql",
	} {
		version := strings.TrimSuffix(name, ".down.sql")
		var applied bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version,
		).Scan(&applied))
		if applied {
			orderedNames = append(orderedNames, name)
			seen[name] = struct{}{}
		}
	}
	for _, name := range names {
		if _, duplicate := seen[name]; !duplicate {
			orderedNames = append(orderedNames, name)
			seen[name] = struct{}{}
		}
	}
	for _, name := range orderedNames {
		wire, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), name))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(wire))
		if err != nil {
			rows, queryErr := pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
			require.NoError(t, queryErr)
			versions := make([]string, 0)
			for rows.Next() {
				var version string
				require.NoError(t, rows.Scan(&version))
				versions = append(versions, version)
			}
			rows.Close()
			require.NoError(t, rows.Err())
			t.Fatalf("%s: %v; applied migrations after failed rollback: %v", name, err, versions)
		}
	}
}

func rollbackEmptyEnterpriseFinanceMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rollbackEmptyMigrations(t, pool, enterpriseFinanceDownMigrations)
}

func rollbackEmptyPRD8RuntimeControlMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rollbackEmptyMigrations(t, pool, prd8RuntimeControlDownMigrations)
}

func rollbackEmptyPRD8Migrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rollbackEmptyPRD8RuntimeControlMigrations(t, pool)
	rollbackEmptyEnterpriseFinanceMigrations(t, pool)
	rollbackEmptyMigrations(t, pool, enterpriseDirectoryDownMigrations)
}

func rollbackAsyncMediaImageMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rollbackEmptyPRD8Migrations(t, pool)
	rollbackAsyncMediaImageFromM46Prestate(t, pool)
}

func rollbackAsyncMediaImageFromM46Prestate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var head string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT max(version)::text FROM schema_migrations`).Scan(&head))
	require.Equal(t, "000046_v1_async_media_image_jobs", head)
	downSQL, err := os.ReadFile(filepath.Join(
		testutil.PreRetirementMigrationsDir(t),
		"000046_v1_async_media_image_jobs.down.sql",
	))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), string(downSQL))
	require.NoError(t, err)
}
