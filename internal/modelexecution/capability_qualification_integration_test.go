//go:build integration

package modelexecution

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPreDispatchValidatesBoundCapabilityApprovalInsteadOfLatestIntegration(t *testing.T) {
	for _, protocolProfileID := range []string{
		"protocol.deepseek-openai-chat.v4",
		"openai.chat_completions",
	} {
		protocolProfileID := protocolProfileID
		t.Run(protocolProfileID, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			fixture := integrationfixture.InsertModelCapabilityQualificationWithProtocol(
				t, pool, "dispatch-bound-"+protocolProfileID, protocolProfileID, "1.0.0",
			)
			_, approvalA, approvedAtA := integrationfixture.AppendModelCapabilityProbeApproval(t, pool, fixture, 1, "dispatch-a")
			_, approvalB, _ := integrationfixture.AppendModelCapabilityProbeApproval(t, pool, fixture, 1, "dispatch-b")
			require.Greater(t, approvalB, approvalA)

			request := boundM88CapabilityRequest(fixture.DeploymentID, approvalA, approvedAtA, protocolProfileID)
			require.Equal(t, Applied, revalidateBoundM88Capability(t, pool, request),
				"an equivalent newer receipt must not stale a frozen Plan bound to A")

			_, err := pool.Exec(context.Background(), `
UPDATE provider_credential_bindings
   SET state_revision=state_revision+1,cooldown_until=NOW()+INTERVAL '2 minutes'
 WHERE id=$1`, fixture.BindingID)
			require.NoError(t, err)
			require.Equal(t, Applied, revalidateBoundM88Capability(t, pool, request),
				"transient credential health must not revoke capability approval")

			_, err = pool.Exec(context.Background(), `
UPDATE model_deployment_operations SET revision=revision+1
 WHERE deployment_id=$1 AND operation_id='openai.chat_completions.v1'`, fixture.DeploymentID)
			require.NoError(t, err)
			require.Equal(t, Stale, revalidateBoundM88Capability(t, pool, request),
				"stable operation identity drift must reject before provider dispatch")
		})
	}
}

func boundM88CapabilityRequest(deploymentID, approvalID int64, approvedAt time.Time, protocolProfileID string) preparedRequest {
	verifiedAt := approvedAt.UTC()
	return preparedRequest{
		plan: modelrouting.Plan{
			RequiredCapabilities:    []string{"streaming"},
			ExecutionProfileID:      "deepseek.v4-flash.api",
			ExecutionProfileVersion: "1.4.0",
		},
		evidence: executionevidence.EvidenceV1{
			ExecutionContractSource: executionevidence.ContractSourceCanonicalProfile,
			DeploymentID:            deploymentID,
			CapabilityEvidence: []executionevidence.CapabilityEntry{{
				CapabilityID: "streaming", SupportState: "enabled",
				EvidenceState: "production_approved",
				EvidenceRef:   "capability_approval:" + strconv.FormatInt(approvalID, 10),
				VerifiedAt:    &verifiedAt, Revision: approvalID,
			}},
		},
		underlying: operationIdentity{
			id: "openai.chat_completions.v1", profileID: protocolProfileID, profileVersion: "1.0.0",
		},
	}
}

func revalidateBoundM88Capability(t *testing.T, pool *pgxpool.Pool, request preparedRequest) Status {
	t.Helper()
	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	status, err := revalidateCapabilities(context.Background(), tx, request)
	require.NoError(t, err)
	return status
}
