//go:build integration

package migrate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelCapabilityQualificationKimiK26FunctionToolsProbeContractV4MigrationLifecycleIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "function-tools-v4-lifecycle", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m100Dir := migrationsThrough(t, testutil.MigrationsDir(t), "000100_kimi_k26_function_tools_tool_index_normalizer.up.sql")

	require.NoError(t, migrate.Run(ctx, pool, m100Dir))
	assertKimiK26FunctionToolsProbeContractV4State(t, pool, fixture.deploymentID, "4.0.0", 4)
	execMigrationDown(t, ctx, pool, "000100_kimi_k26_function_tools_tool_index_normalizer.down.sql")
	assertKimiK26FunctionToolsProbeContractV4State(t, pool, fixture.deploymentID, "3.0.0", 3)
	require.NoError(t, migrate.Run(ctx, pool, m100Dir))
	assertKimiK26FunctionToolsProbeContractV4State(t, pool, fixture.deploymentID, "4.0.0", 4)
}

func TestModelCapabilityQualificationKimiK26FunctionToolsProbeContractV4DownAllowsHistoricalFailedV3ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "function-tools-v3-failure-before-v4", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m99Dir := migrationsThrough(t, testutil.MigrationsDir(t), "000099_kimi_k26_function_tools_probe_v3.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m99Dir))
	insertKimiK26CapabilityProbeFixture(t, pool, fixture, "tools", kimiK26FunctionToolsProbeContractID, "3.0.0", false, "m100-historical-v3-failure")

	m100Dir := migrationsThrough(t, testutil.MigrationsDir(t), "000100_kimi_k26_function_tools_tool_index_normalizer.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m100Dir))
	execMigrationDown(t, ctx, pool, "000100_kimi_k26_function_tools_tool_index_normalizer.down.sql")
	assertKimiK26FunctionToolsProbeContractV4State(t, pool, fixture.deploymentID, "3.0.0", 3)

	var retained int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_probes
 WHERE execution_profile_id='kimi.k2.6.api'
   AND capability_id='tools'
   AND probe_contract_version='3.0.0'`).Scan(&retained))
	require.Equal(t, 1, retained)
}

func TestModelCapabilityQualificationKimiK26FunctionToolsProbeContractV4DownRefusesRetainedV4ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "function-tools-v4-retained", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m100Dir := migrationsThrough(t, testutil.MigrationsDir(t), "000100_kimi_k26_function_tools_tool_index_normalizer.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m100Dir))
	insertKimiK26CapabilityProbeFixture(t, pool, fixture, "tools", kimiK26FunctionToolsProbeContractID, "4.0.0", true, "m100-retained-v4")

	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000100_kimi_k26_function_tools_tool_index_normalizer.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "retained v4 qualification evidence")
	assertKimiK26FunctionToolsProbeContractV4State(t, pool, fixture.deploymentID, "4.0.0", 4)
}

func TestModelCapabilityQualificationKimiK26FunctionToolsProbeContractV4DownRefusesProjectionIdentityDriftIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "function-tools-v4-drift", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m100Dir := migrationsThrough(t, testutil.MigrationsDir(t), "000100_kimi_k26_function_tools_tool_index_normalizer.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m100Dir))

	_, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET protocol_profile_id='unverified.chat',revision=revision+1
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.deploymentID)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000100_kimi_k26_function_tools_tool_index_normalizer.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "projection identity drift")
}

func assertKimiK26FunctionToolsProbeContractV4State(
	t *testing.T, pool *pgxpool.Pool, deploymentID int64, wantVersion string, wantRevision int64,
) {
	t.Helper()
	ctx := t.Context()
	var current int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_current_probe_contracts
 WHERE operation_id='openai.chat_completions.v1'
   AND capability_id IN ('tools','function_tools','tool_choice_auto')
   AND execution_profile_id='kimi.k2.6.api'
   AND execution_profile_version='1.7.0'
   AND protocol_profile_id='protocol.kimi-openai-chat.v5'
   AND protocol_profile_version='1.0.0'
   AND probe_contract_id='unirouter.capability-probe.kimi-chat-function-tools.v1'
   AND probe_contract_version=$1`, wantVersion).Scan(&current))
	require.Equal(t, 3, current)

	var projections int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_capabilities
 WHERE deployment_id=$1
   AND operation_id='openai.chat_completions.v1'
   AND capability_id IN ('tools','function_tools','tool_choice_auto')
   AND evidence_ref='capability_contract:unirouter.capability-probe.kimi-chat-function-tools.v1@' || $2
   AND revision=$3`, deploymentID, wantVersion, wantRevision).Scan(&projections))
	require.Equal(t, 3, projections)
}
