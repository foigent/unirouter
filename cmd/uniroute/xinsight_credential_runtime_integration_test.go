//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type runtimeCredentialSource struct{}

func (runtimeCredentialSource) Enabled() bool          { return true }
func (runtimeCredentialSource) SourceInstance() string { return "ecs.runtime-credential-test.v1" }

type runtimeCredentialCounterpart struct {
	xInsightConnectorRuntimeCounterpart
	run func(context.Context) error
}

func (c runtimeCredentialCounterpart) RunCredential(ctx context.Context) error { return c.run(ctx) }

func runtimeCredentialHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// The production runCycle uses the durable journal as its actual discovery
// source. This fixture supplies an already redeemed installation and simulates
// only the XI response loss; the pending operation and restart discovery are
// persisted by product code in PostgreSQL.
func TestXInsightCredentialRuntimePostgresIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	const installation = "xci_AAAAAAAAAAAAAAAAAAAAAA"
	const deployment = "xid_ZZZZZZZZZZZZZZZZZZZZZZ"
	const origin = "https://control.example.test"
	const invitation = "xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	var orgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type)
VALUES ('runtime-credential','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,status)
VALUES ($1,'runtime-credential-owner','hash','platform_admin','active') RETURNING id`, orgID).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,created_by)
VALUES ($1,'Runtime credential','runtime-credential','active',$2) RETURNING id`, orgID, actorID).Scan(&projectID))
	manager, err := xinsightconnector.NewManager(pool,
		xinsightconnector.CommitAuthorizerFunc(func(context.Context, pgx.Tx, int64, int64, int64, string) error { return nil }),
		runtimeCredentialSource{})
	require.NoError(t, err)
	_, _, err = manager.CreateInstallation(ctx, xinsightconnector.Consent{
		InstallationID: installation, XDeploymentID: deployment,
		InvitationSHA256: runtimeCredentialHash([]byte(invitation)),
		OrgID:            orgID, ProjectID: projectID,
		EventScope:     xinsightconnector.EventScopeModelCompleted,
		IdempotencyKey: "runtime-credential", ConsentedByUserID: actorID,
	})
	require.NoError(t, err)
	journal, err := xinsightconnector.NewPostgresPhaseJournal(pool, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	require.NoError(t, journal.StageInvitation(ctx, installation, invitation))
	prepared, err := journal.PrepareRedeem(ctx, installation)
	require.NoError(t, err)
	// The initial, already redeemed prestate is synthetic. The test exercises
	// real journal preparation and exact pending replay from this point on.
	_, err = pool.Exec(ctx, `UPDATE xinsight_connector_control_bindings
SET invitation_token_enc=NULL,redeem_result_sha256=$2,redeemed_origin=$3,
control_token_expires_at=NOW()+INTERVAL '12 hours'
WHERE installation_id=$1`, installation, runtimeCredentialHash([]byte("synthetic-redeem-result")), origin)
	require.NoError(t, err)
	var fence int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT control_fence FROM xinsight_connector_installations WHERE installation_id=$1`, installation).Scan(&fence))
	consent, err := json.Marshal(map[string]any{"control_fence": fence, "xi_consent": map[string]any{
		"xi_origin": origin, "ur_origin": "https://ur.example.test",
		"installation_id": installation, "deployment_id": deployment,
	}})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE xinsight_connector_installations SET
setup_request_id='00000000-0000-4000-8000-000000000125',setup_consent_canonical=$2,
setup_consent_sha256=$3,setup_state='ready',setup_progress='{}'::jsonb
WHERE installation_id=$1`, installation, consent, runtimeCredentialHash(consent))
	require.NoError(t, err)
	require.NotEmpty(t, prepared.ControlProjectID)

	credentialRuns, polls := 0, 0
	makeSupervisor := func(current *xinsightconnector.PostgresPhaseJournal) *xInsightConnectorRuntimeSupervisor {
		t.Helper()
		supervisor, err := newXInsightConnectorRuntimeSupervisor(true, time.Second, current,
			xInsightConnectorRuntimeNoopRedeemerFactory,
			func(_ context.Context, d xinsightconnector.RuntimeInstallationDescriptor) (xinsightconnector.ControlCounterpart, error) {
				require.Equal(t, installation, d.InstallationID)
				return &runtimeCredentialCounterpart{run: func(ctx context.Context) error {
					credentialRuns++
					_, err := current.PrepareRotate(ctx, installation)
					if err != nil {
						return err
					}
					return errors.New("synthetic XI response loss")
				}}, nil
			},
			func(_ string, _ xinsightconnector.ControlCounterpart) (xInsightConnectorRuntimeAgent, error) {
				return xInsightConnectorRuntimeAgentFunc(func(context.Context) (bool, error) {
					polls++
					return false, nil
				}), nil
			}, nil)
		require.NoError(t, err)
		return supervisor
	}
	supervisor := makeSupervisor(journal)
	require.NoError(t, supervisor.runCycle(ctx))
	require.Equal(t, 0, credentialRuns)
	require.Equal(t, 1, polls)

	_, err = pool.Exec(ctx, `UPDATE xinsight_connector_control_bindings
SET control_token_expires_at=NOW()+INTERVAL '5 hours' WHERE installation_id=$1`, installation)
	require.NoError(t, err)
	require.ErrorContains(t, supervisor.runCycle(ctx), "credential lifecycle failed")
	require.Equal(t, 1, credentialRuns)
	require.Equal(t, 1, polls, "credential due must block ordinary Poll in the same cycle")
	status, err := journal.CredentialStatus(ctx, installation)
	require.NoError(t, err)
	require.Equal(t, "rotation_pending", status.State)
	require.NotEmpty(t, status.PendingOperationID)
	firstOperation := status.PendingOperationID

	// Once the old bearer expires, durable pending recovery remains discoverable;
	// ordinary Poll still has no valid credential and cannot resume.
	_, err = pool.Exec(ctx, `UPDATE xinsight_connector_control_bindings
SET created_at=NOW()-INTERVAL '25 hours',control_token_expires_at=NOW()-INTERVAL '1 hour'
WHERE installation_id=$1`, installation)
	require.NoError(t, err)
	// Recreate both product objects over the same durable database and key.
	// This tests reconstruction after a supervisor restart, not a process restart.
	restartedJournal, err := xinsightconnector.NewPostgresPhaseJournal(pool, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	supervisor = makeSupervisor(restartedJournal)
	require.ErrorContains(t, supervisor.runCycle(ctx), "credential lifecycle failed")
	require.Equal(t, 2, credentialRuns)
	require.Equal(t, 1, polls)
	status, err = restartedJournal.CredentialStatus(ctx, installation)
	require.NoError(t, err)
	require.Equal(t, firstOperation, status.PendingOperationID, "reconstructed supervisor must replay one durable operation")

	_, err = pool.Exec(ctx, `UPDATE xinsight_connector_installations SET desired_state='disconnected',observed_state='disconnected'
WHERE installation_id=$1`, installation)
	require.NoError(t, err)
	require.NoError(t, supervisor.runCycle(ctx))
	require.Equal(t, 2, credentialRuns, "revocation must remove lifecycle authority")
	require.Equal(t, 1, polls)
}
