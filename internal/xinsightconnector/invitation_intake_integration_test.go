//go:build integration

package xinsightconnector

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestInvitationIntakeIdempotentlyPersistsInstallationAndEncryptedInvitationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('connector-intake','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,role,status) VALUES ($1,'connector-intake-owner','hash','platform_admin','active') RETURNING id`,
		orgID,
	).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status,created_by) VALUES ($1,'Connector Intake','connector-intake','active',$2) RETURNING id`,
		orgID, actorID,
	).Scan(&projectID))

	manager, err := NewManager(
		pool,
		&recordingAuthorizer{},
		sourceAuthorityFixture{enabled: true, instance: "source.connector.intake"},
	)
	require.NoError(t, err)
	journal, err := NewPostgresPhaseJournal(pool, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	intake, err := NewInvitationIntake(
		manager, journal, "https://xinsight.example", "stable",
	)
	require.NoError(t, err)
	clock := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return clock }
	journal.now = func() time.Time { return clock }
	intake.now = func() time.Time { return clock }

	var invitation FrozenInvitationV1
	decodeFrozenInvitationFixture(t,
		filepath.Join("testdata", "invitations", "valid", "01-frozen-invitation.json"),
		&invitation,
	)
	input := InvitationIntakeInput{
		Invitation: invitation, OrgID: orgID, ProjectID: projectID, ConsentedByUserID: actorID,
	}
	created, err := intake.Accept(ctx, input)
	require.NoError(t, err)
	replayed, err := intake.Accept(ctx, input)
	require.NoError(t, err)
	require.Equal(t, created.Installation.InstallationID, replayed.Installation.InstallationID)
	require.Equal(t, created.Operation.OperationRef, replayed.Operation.OperationRef)
	require.Equal(t, "source.connector.intake", created.Installation.SourceInstance)
	require.Equal(t, ObservedProvisioning, created.Installation.ObservedState)
	require.Equal(t, OperationQueued, created.Operation.State)

	var installations, operations, bindings int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM xinsight_connector_installations`).Scan(&installations))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM xinsight_connector_operations`).Scan(&operations))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM xinsight_connector_control_bindings`).Scan(&bindings))
	require.Equal(t, 1, installations)
	require.Equal(t, 1, operations)
	require.Equal(t, 1, bindings)

	var invitationCiphertext, connectionStatus, sourceInstance, invitationSHA, idempotencyKey string
	var consentedBy int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT binding.invitation_token_enc, connection.status, installation.source_instance,
       installation.invitation_sha256, installation.consented_by, operation.idempotency_key
FROM xinsight_connector_installations AS installation
JOIN xinsight_connector_control_bindings AS binding
  ON binding.installation_id=installation.installation_id
JOIN completed_execution_feed_connections AS connection
  ON connection.org_id=installation.org_id AND connection.project_id=installation.project_id
JOIN xinsight_connector_operations AS operation
  ON operation.installation_id=installation.installation_id AND operation.action='connect'
WHERE installation.installation_id=$1`, invitation.InstallationID).Scan(
		&invitationCiphertext, &connectionStatus, &sourceInstance,
		&invitationSHA, &consentedBy, &idempotencyKey,
	))
	const invitationToken = "xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	require.NotEmpty(t, invitationCiphertext)
	require.NotContains(t, invitationCiphertext, invitationToken)
	require.Equal(t, xInsightFeedPausedState, connectionStatus)
	require.Equal(t, "source.connector.intake", sourceInstance)
	require.Equal(t, hashBytes([]byte(invitationToken)), invitationSHA)
	require.Equal(t, actorID, consentedBy)
	require.Equal(t, "xinsight-invitation-"+strings.TrimPrefix(invitationSHA, "sha256:"), idempotencyKey)
	plaintext, err := journal.cipher.open(
		connectorInvitationSecret, invitation.InstallationID, invitationCiphertext,
	)
	require.NoError(t, err)
	require.Equal(t, invitationToken, plaintext)
}

const xInsightFeedPausedState = "paused"
