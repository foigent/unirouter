//go:build integration

package migrate_test

import (
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/releaseexecution"
	"github.com/uniroute/unirouter/internal/testutil"
	"strings"
	"testing"
)

func TestSealedCapabilityDecisionSurvivesSelectorButHonorsDisable(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	f := insertM88CapabilityFixture(t, pool, "pinned-decision")
	probe := insertM88CapabilityProbe(t, pool, f, 1, "pin")
	approval := insertM88CapabilityApproval(t, pool, f, probe, "pin")
	target := releaseexecution.CapabilityTarget{CustomerOrgID: f.orgID, PublicModelID: f.modelID, DeploymentID: f.deploymentID, OperationID: "openai.chat_completions.v1", ExecutionProfileID: "deepseek.v4-flash.api", ExecutionProfileVersion: "1.4.0", ProtocolProfileID: "protocol.deepseek-openai-chat.v4", ProtocolProfileVersion: "1.0.0"}
	decision, err := releaseexecution.ReadCapabilityDecision(ctx, pool, target, approval, nil, false)
	require.NoError(t, err)
	connection, err := pool.Acquire(ctx)
	require.NoError(t, err)
	_, err = connection.Exec(ctx, `SET TIME ZONE 'Asia/Shanghai'`)
	require.NoError(t, err)
	read, err := releaseexecution.ReadCapabilityDecision(ctx, connection, target, approval, &decision.Pin, false)
	require.NoError(t, err)
	require.Equal(t, decision.Pin, read.Pin)
	_, err = connection.Exec(ctx, `SET TIME ZONE 'UTC'`)
	require.NoError(t, err)
	connection.Release()
	// Schema expansion must not reinterpret a retained facts digest.
	_, err = pool.Exec(ctx, `ALTER TABLE model_capability_probes ADD COLUMN fixture_future_note TEXT DEFAULT 'irrelevant'`)
	require.NoError(t, err)
	_, err = releaseexecution.ReadCapabilityDecision(ctx, pool, target, approval, &decision.Pin, false)
	require.NoError(t, err)
	// Explicit isolated corruption injection proves the reader boundary; this
	// is not the publication writer and is rolled back with the test database.
	_, err = pool.Exec(ctx, `ALTER TABLE model_capability_current_probe_contracts DISABLE TRIGGER ALL;
DELETE FROM model_capability_current_probe_contracts WHERE execution_profile_id='deepseek.v4-flash.api' AND execution_profile_version='1.4.0';
ALTER TABLE model_capability_current_probe_contracts ENABLE TRIGGER ALL`)
	require.NoError(t, err)
	_, err = releaseexecution.ReadCapabilityDecision(ctx, pool, target, approval, nil, false)
	require.ErrorContains(t, err, "authoring selector differs")
	_, err = releaseexecution.ReadCapabilityDecision(ctx, pool, target, approval, &decision.Pin, false)
	require.NoError(t, err, "sealed interpretation never reselects a current contract")
	bad := decision.Pin
	bad.EvidenceDigest = strings.Repeat("a", 64)
	_, err = releaseexecution.ReadCapabilityDecision(ctx, pool, target, approval, &bad, false)
	require.ErrorContains(t, err, "retained capability facts differ")
	_, err = pool.Exec(ctx, `UPDATE model_deployment_capabilities SET support_state='unsupported',revision=revision+1 WHERE deployment_id=$1 AND operation_id=$2 AND capability_id='streaming'`, f.deploymentID, target.OperationID)
	require.NoError(t, err)
	_, err = releaseexecution.ReadCapabilityDecision(ctx, pool, target, approval, &decision.Pin, false)
	require.ErrorContains(t, err, "current safety state denies")
}
