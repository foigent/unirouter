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

const kimiK26FunctionToolsProbeContractID = "unirouter.capability-probe.kimi-chat-function-tools.v1"

func TestModelCapabilityQualificationKimiK26FunctionToolsProbeContractV3MigrationLifecycleIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "function-tools-v3-lifecycle", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m99Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000099_kimi_k26_function_tools_probe_v3.up.sql")

	require.NoError(t, migrate.Run(ctx, pool, m99Dir))
	assertKimiK26FunctionToolsProbeContractState(t, pool, fixture.deploymentID, "3.0.0", 3)
	execMigrationDown(t, ctx, pool, "000099_kimi_k26_function_tools_probe_v3.down.sql")
	assertKimiK26FunctionToolsProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)
	require.NoError(t, migrate.Run(ctx, pool, m99Dir))
	assertKimiK26FunctionToolsProbeContractState(t, pool, fixture.deploymentID, "3.0.0", 3)
}

func TestModelCapabilityQualificationKimiK26FunctionToolsProbeContractV3DownAllowsHistoricalFailedV2ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "function-tools-v2-failure", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m98Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000098_kimi_k26_nonstream_usage_probe_v2.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m98Dir))
	insertKimiK26CapabilityProbeFixture(t, pool, fixture, "tools", kimiK26FunctionToolsProbeContractID, "2.0.0", false, "m99-historical-v2-failure")

	m99Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000099_kimi_k26_function_tools_probe_v3.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m99Dir))
	execMigrationDown(t, ctx, pool, "000099_kimi_k26_function_tools_probe_v3.down.sql")
	assertKimiK26FunctionToolsProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)

	var retained int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_capability_probes
 WHERE execution_profile_id='kimi.k2.6.api'
   AND capability_id='tools'
   AND probe_contract_version='2.0.0'`).Scan(&retained))
	require.Equal(t, 1, retained)
}

func TestModelCapabilityQualificationKimiK26FunctionToolsProbeContractV3DownRefusesRetainedV3ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "function-tools-v3-retained", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m99Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000099_kimi_k26_function_tools_probe_v3.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m99Dir))
	insertKimiK26CapabilityProbeFixture(t, pool, fixture, "tools", kimiK26FunctionToolsProbeContractID, "3.0.0", true, "m99-retained-v3")

	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000099_kimi_k26_function_tools_probe_v3.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "retained v3 qualification evidence")
	assertKimiK26FunctionToolsProbeContractState(t, pool, fixture.deploymentID, "3.0.0", 3)
}

func TestModelCapabilityQualificationKimiK26FunctionToolsProbeContractV3DownRefusesProjectionIdentityDriftIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "function-tools-v3-drift", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m99Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000099_kimi_k26_function_tools_probe_v3.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m99Dir))

	_, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET protocol_profile_id='unverified.chat',revision=revision+1
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.deploymentID)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000099_kimi_k26_function_tools_probe_v3.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "projection identity drift")
}

func assertKimiK26FunctionToolsProbeContractState(
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
