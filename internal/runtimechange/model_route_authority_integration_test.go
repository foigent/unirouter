//go:build integration

package runtimechange_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelrouteauthority"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCanonicalAuthorityAcceptsParentlessV2WithoutLegacyProvenance(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	manager, err := runtimechange.NewManager(pool, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	fixture := createCanonicalDeploymentFixture(t, pool, "authority-parentless", "active", 100, 100)
	makeSoleInitialCustomer(t, pool, fixture.OrgID)

	_, err = pool.Exec(ctx, `UPDATE model_deployments
       SET legacy_channel_id=NULL,legacy_public_model=NULL WHERE id=$1`, fixture.DeploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE provider_credential_bindings
       SET legacy_channel_key_id=NULL WHERE id=$1`, fixture.CredentialID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE provider_connections
       SET legacy_channel_id=NULL WHERE id=$1`, fixture.ConnectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM channels WHERE id=$1`, fixture.ChannelID)
	require.NoError(t, err)

	sealed := sealInitialDeploymentRoutes(
		t, pool, manager, fixture, []runtimechange.DeploymentRoute{deploymentRoute(fixture)},
		"rcr_authority_parentless",
	)
	activateInitialDeploymentReleaseFixture(t, manager, fixture, sealed)

	canonical, err := modelrouting.CheckAuthority(ctx, pool, routingpolicy.Default(), modelrouting.AuthorityCheckRequest{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.OrgID,
	})
	require.NoErrorf(t, err, "canonical authority report: %+v", canonical)
	require.True(t, canonical.Ready)

	verify, err := modelrouting.CheckAuthority(ctx, pool, routingpolicy.Default(), modelrouting.AuthorityCheckRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: fixture.OrgID,
	})
	require.ErrorIs(t, err, modelrouting.ErrAuthorityNotReady)
	require.False(t, verify.Ready, "verify must still require a v1 compatibility parent")

	authorityService, err := modelrouteauthority.New(pool, routingpolicy.Default())
	require.NoError(t, err)
	noopAudit := func(context.Context, pgx.Tx, modelrouteauthority.SafetyAuditRecord) error { return nil }
	paused, err := authorityService.PauseConnection(ctx, modelrouteauthority.PauseConnectionRequest{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.OrgID,
		ActorUserID:  fixture.UserID,
		ConnectionID: fixture.ConnectionID, ExpectedConfigRevision: fixture.ConnectionRevision,
	}, noopAudit)
	require.NoError(t, err)
	require.True(t, paused.Changed)
	var connectionState, deploymentState string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT connection.admin_state,deployment.admin_state
  FROM provider_connections connection
  JOIN model_deployments deployment ON deployment.connection_id=connection.id
 WHERE connection.id=$1 AND deployment.id=$2`, fixture.ConnectionID, fixture.DeploymentID).Scan(
		&connectionState, &deploymentState,
	))
	require.Equal(t, "disabled", connectionState)
	require.Equal(t, "enabled", deploymentState)

	revoked, err := authorityService.RevokeCredential(ctx, modelrouteauthority.RevokeCredentialRequest{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: fixture.OrgID,
		ActorUserID:         fixture.UserID,
		CredentialBindingID: fixture.CredentialID, ExpectedStateRevision: fixture.CredentialRevision,
	}, noopAudit)
	require.NoError(t, err)
	require.True(t, revoked.Changed)
	var credentialState, encryptedSecret string
	var retiredAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
SELECT status,key_enc,retired_at FROM provider_credential_bindings WHERE id=$1`, fixture.CredentialID).Scan(
		&credentialState, &encryptedSecret, &retiredAt,
	))
	require.Equal(t, "retired", credentialState)
	require.Empty(t, encryptedSecret)
	require.NotNil(t, retiredAt)
}
