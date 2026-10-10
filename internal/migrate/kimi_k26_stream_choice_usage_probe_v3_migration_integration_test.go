//go:build integration

package migrate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelCapabilityQualificationKimiK26StreamProbeContractV3MigrationLifecycleIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "stream-v3-lifecycle", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m97Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000097_kimi_k26_stream_choice_usage_probe_v3.up.sql")

	require.NoError(t, migrate.Run(ctx, pool, m97Dir))
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "3.0.0", 3)
	execMigrationDown(t, ctx, pool, "000097_kimi_k26_stream_choice_usage_probe_v3.down.sql")
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)
	require.NoError(t, migrate.Run(ctx, pool, m97Dir))
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "3.0.0", 3)
}

func TestModelCapabilityQualificationKimiK26StreamProbeContractV3DownAllowsHistoricalFailedV2ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "stream-v2-failure", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m96Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000096_kimi_k26_stream_probe_contract_v2.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m96Dir))
	insertKimiK26StreamProbeFixture(t, pool, fixture, "2.0.0", false, "m97-historical-v2-failure")

	m97Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000097_kimi_k26_stream_choice_usage_probe_v3.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m97Dir))
	execMigrationDown(t, ctx, pool, "000097_kimi_k26_stream_choice_usage_probe_v3.down.sql")
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "2.0.0", 2)
}

func TestModelCapabilityQualificationKimiK26StreamProbeContractV3DownRefusesRetainedV3ProbeIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "stream-v3-retained", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m97Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000097_kimi_k26_stream_choice_usage_probe_v3.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m97Dir))
	insertKimiK26StreamProbeFixture(t, pool, fixture, "3.0.0", true, "m97-retained-v3")

	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000097_kimi_k26_stream_choice_usage_probe_v3.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "retained v3 qualification evidence")
	assertKimiK26StreamProbeContractState(t, pool, fixture.deploymentID, "3.0.0", 3)
}

func TestModelCapabilityQualificationKimiK26StreamProbeContractV3DownRefusesProjectionIdentityDriftIntegration(t *testing.T) {
	pool, _ := setupMigratedThrough(t, "000094_deepseek_v4_pro_chat_core_qualification.up.sql")
	fixture := insertM95KimiCapabilityFixture(t, pool, "stream-v3-drift", "protocol.kimi-openai-chat.v5")
	ctx := t.Context()
	m97Dir := migrationsThrough(t, testutil.PreRetirementMigrationsDir(t), "000097_kimi_k26_stream_choice_usage_probe_v3.up.sql")
	require.NoError(t, migrate.Run(ctx, pool, m97Dir))

	_, err := pool.Exec(ctx, `
UPDATE model_deployment_operations
   SET protocol_profile_id='unverified.chat',revision=revision+1
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.deploymentID)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(testutil.PreRetirementMigrationsDir(t), "000097_kimi_k26_stream_choice_usage_probe_v3.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "projection identity drift")

	var applied bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
 WHERE version='000097_kimi_k26_stream_choice_usage_probe_v3')`).Scan(&applied))
	require.True(t, applied)
}
