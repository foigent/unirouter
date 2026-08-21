//go:build integration

package modelrouteauthority

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modeldeployment"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

type legacySafetyFixture struct {
	orgID              int64
	userID             int64
	channelID          int64
	credentialID       int64
	connectionID       int64
	connectionRevision int64
	bindingID          int64
	bindingRevision    int64
}

func TestApplyAndCheckLegacyCompatibilityAuthorityAreIdempotentAndReadOnly(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-ready")
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)

	first, err := service.Apply(ctx, ApplyRequest{Mode: modelrouting.ModeVerify, CustomerOrgID: orgID})
	require.NoErrorf(t, err, "first apply report: %+v", first)
	require.True(t, first.Ready)
	require.Len(t, first.Sealed, 1)
	require.Zero(t, first.AlreadySealed)
	require.True(t, first.Authority.Ready)

	second, err := service.Apply(ctx, ApplyRequest{Mode: modelrouting.ModeVerify, CustomerOrgID: orgID})
	require.NoErrorf(t, err, "second apply report: %+v", second)
	require.True(t, second.Ready)
	require.Empty(t, second.Sealed)
	require.Equal(t, 1, second.AlreadySealed)

	beforeRejectedApplySequences := authoritySequenceSnapshot(t, pool)
	beforeRejectedApplyFacts := authorityFactSnapshot(t, pool)
	rejected, err := service.Apply(ctx, ApplyRequest{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: orgID,
	})
	require.ErrorIs(t, err, ErrApplyMode)
	require.False(t, rejected.Ready)
	require.Equal(t, beforeRejectedApplySequences, authoritySequenceSnapshot(t, pool),
		"canonical apply must not advance a sequence")
	require.Equal(t, beforeRejectedApplyFacts, authorityFactSnapshot(t, pool),
		"canonical apply must not write any route authority fact")

	beforeSequences := authoritySequenceSnapshot(t, pool)
	beforeFacts := authorityFactSnapshot(t, pool)
	checked, err := service.Check(ctx, modelrouting.AuthorityCheckRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
	})
	require.NoErrorf(t, err, "check report: %+v", checked)
	require.True(t, checked.Ready)
	require.Equal(t, beforeSequences, authoritySequenceSnapshot(t, pool), "read-only check advanced a sequence")
	require.Equal(t, beforeFacts, authorityFactSnapshot(t, pool), "read-only check changed route authority facts")

	_, err = pool.Exec(ctx, `
UPDATE model_deployments
   SET config_revision=config_revision+1,updated_at=NOW()
 WHERE customer_org_id=$1`, orgID)
	require.NoError(t, err)
	drifted, err := service.Check(ctx, modelrouting.AuthorityCheckRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
	})
	require.ErrorIs(t, err, modelrouting.ErrAuthorityNotReady)
	require.False(t, drifted.Ready)
	require.NotEmpty(t, drifted.BlockingIssues)
}

func TestApplyAtExactCanonicalConnectionLimitRemainsIdempotentIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-exact-connection-limit")
	_, err := pool.Exec(ctx, `
INSERT INTO commercial_license_entitlements (
    singleton,license_id,deployment_id,key_id,expires_at,features,active_channels)
VALUES (TRUE,'authority-exact-limit','authority-exact-limit-deployment',
        'authority-exact-limit-key',NOW()+INTERVAL '1 day','["*"]'::jsonb,1)`)
	require.NoError(t, err)
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)

	first, err := service.Apply(ctx, ApplyRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
	})
	require.NoErrorf(t, err, "first exact-capacity apply report: %+v", first)
	require.True(t, first.Ready)
	var firstConnectionID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT id FROM provider_connections WHERE admin_state='enabled'`).Scan(&firstConnectionID))

	second, err := service.Apply(ctx, ApplyRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
	})
	require.NoErrorf(t, err, "second exact-capacity apply report: %+v", second)
	require.True(t, second.Ready)
	require.Equal(t, 1, second.AlreadySealed)
	var secondConnectionID, enabledConnections int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT MIN(id),COUNT(*)
  FROM provider_connections
 WHERE admin_state='enabled'`).Scan(&secondConnectionID, &enabledConnections))
	require.Equal(t, firstConnectionID, secondConnectionID)
	require.EqualValues(t, 1, enabledConnections,
		"idempotent verify apply must not consume another canonical connection seat")
}

func TestVerifyAuthorityBlocksChatOnlyAdapterFromLegacyEmbeddingsRoute(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-embeddings-adapter")
	_, err := pool.Exec(ctx, `
UPDATE channels
   SET adapter_type='anthropic_native',adapter_config='{}'::jsonb
 WHERE models='authority-model'`)
	require.NoError(t, err)
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)

	report, err := service.Apply(ctx, ApplyRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
	})
	require.ErrorIs(t, err, ErrApplyIncomplete)
	require.False(t, report.Ready)
	require.True(t, hasAuthorityIssue(report.Authority.BlockingIssues,
		"route_evidence_incomplete", string(modeloperation.IDEmbeddings), "operation_adapter_unsupported"),
		"authority report: %+v", report.Authority)
	require.False(t, hasAuthorityIssue(report.Authority.BlockingIssues,
		"route_evidence_incomplete", string(modeloperation.IDChatCompletions), "operation_adapter_unsupported"),
		"chat remains supported by anthropic_native: %+v", report.Authority)
}

func TestApplySealsCurrentLegacyStableAndCanaryBeforeVerifyCheck(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-stable-canary")
	stableID, canaryID := addLegacyCanaryFixture(t, pool, "authority-model", "rcr_authority_stable_canary")
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)

	report, err := service.Apply(ctx, ApplyRequest{Mode: modelrouting.ModeVerify, CustomerOrgID: orgID})
	require.NoErrorf(t, err, "stable+canary apply report: %+v", report)
	require.True(t, report.Ready)
	require.Len(t, report.Sealed, 2)
	require.Equal(t, 2, report.Authority.ReleasesChecked)
	var sealed int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM runtime_change_releases
 WHERE id=ANY($1::bigint[]) AND public_model_id IS NOT NULL
   AND route_items_sealed_at IS NOT NULL AND route_items_count>0`, []int64{stableID, canaryID}).Scan(&sealed))
	require.Equal(t, 2, sealed)

	checked, err := service.Check(ctx, modelrouting.AuthorityCheckRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
	})
	require.NoErrorf(t, err, "stable+canary check report: %+v", checked)
	require.True(t, checked.Ready)
	require.Equal(t, 2, checked.ReleasesChecked)
}

func TestApplyFailsClosedWhenCurrentLegacyCanaryProjectionDrifts(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-canary-drift")
	_, canaryID := addLegacyCanaryFixture(t, pool, "authority-model", "rcr_authority_canary_drift")
	projection, err := modeldeployment.ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoErrorf(t, err, "initial canary projection: %+v", projection)
	_, err = pool.Exec(ctx, `
UPDATE runtime_route_revision_items
   SET expected_deployment_revision=expected_deployment_revision+1
 WHERE release_id=$1`, canaryID)
	require.NoError(t, err)
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)

	report, err := service.Apply(ctx, ApplyRequest{Mode: modelrouting.ModeVerify, CustomerOrgID: orgID})
	require.ErrorIs(t, err, ErrApplyIncomplete)
	require.False(t, report.Ready)
	require.True(t, hasModelDeploymentIssue(report.Reconciliation.BlockingIssues, "route_revision_item_conflict"), "%+v", report.Reconciliation.BlockingIssues)
	var stableSealed, canarySealed bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM runtime_change_releases release
               JOIN runtime_change_slots slot ON slot.stable_release_id=release.id
              WHERE release.route_items_sealed_at IS NOT NULL),
       EXISTS(SELECT 1 FROM runtime_change_releases WHERE id=$1 AND route_items_sealed_at IS NOT NULL)`, canaryID).Scan(
		&stableSealed, &canarySealed,
	))
	require.False(t, stableSealed, "apply must not seal any current release after reconciliation reports drift")
	require.False(t, canarySealed)
}

func TestCheckIncludesConfiguredLegacyModelsWithoutSlots(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('authority-inventory','active','customer') RETURNING id`).Scan(&orgID))
	_, err := pool.Exec(ctx, `
INSERT INTO channels (name,provider,base_url,models,status)
VALUES ('unreconciled','custom','https://provider.invalid','unreconciled-model','staged')`)
	require.NoError(t, err)
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)
	report, err := service.Check(ctx, modelrouting.AuthorityCheckRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
	})
	require.ErrorIs(t, err, modelrouting.ErrAuthorityNotReady)
	require.Equal(t, 1, report.ModelsChecked)
	require.Contains(t, authorityIssueCodes(report), "model_registry_missing")
}

func TestVerifySafetyControlsPreserveDeploymentAxisAndAtomicallyScrubCredentials(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-safety")
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)
	_, err = service.Apply(ctx, ApplyRequest{Mode: modelrouting.ModeVerify, CustomerOrgID: orgID})
	require.NoError(t, err)
	fixture := loadLegacySafetyFixture(t, pool, orgID)
	addSharedSiblingProjection(t, pool, fixture)
	projection, err := modeldeployment.ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoErrorf(t, err, "shared connection projection: %+v", projection)
	var siblingCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployments
 WHERE connection_id=$1 AND customer_org_id=$2 AND admin_state<>'retired'`, fixture.connectionID, fixture.orgID).Scan(&siblingCount))
	require.Equal(t, 2, siblingCount)

	paused, err := service.PauseConnection(ctx, PauseConnectionRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
		ActorUserID:  fixture.userID,
		ConnectionID: fixture.connectionID, ExpectedConfigRevision: fixture.connectionRevision,
	}, appendFixtureSafetyAudit(fixture.orgID, fixture.userID))
	require.NoError(t, err)
	require.True(t, paused.Changed)
	require.Equal(t, "disabled", paused.State)
	require.Equal(t, fixture.connectionRevision, paused.Revision)
	afterPauseProjection, err := modeldeployment.ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoErrorf(t, err, "reconcile after connection pause: %+v", afterPauseProjection)

	var channelState, connectionState string
	var channelRevision, connectionRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT channel.status,channel.routing_epoch,connection.admin_state,connection.config_revision
  FROM channels channel
  JOIN provider_connections connection ON connection.legacy_channel_id=channel.id
	 WHERE channel.id=$1`, fixture.channelID).Scan(
		&channelState, &channelRevision, &connectionState, &connectionRevision,
	))
	require.Equal(t, "disabled", channelState)
	require.Equal(t, "disabled", connectionState)
	require.Equal(t, fixture.connectionRevision, channelRevision)
	require.Equal(t, fixture.connectionRevision, connectionRevision)
	pausedAuthority, err := service.Check(ctx, modelrouting.AuthorityCheckRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
	})
	require.NoErrorf(t, err, "an emergency pause must remain a structurally valid verify authority: %+v", pausedAuthority)
	require.True(t, pausedAuthority.Ready)
	var changedDeploymentAxes int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployments
 WHERE connection_id=$1 AND customer_org_id=$2
   AND (admin_state<>'enabled' OR config_revision<>$3)`, fixture.connectionID, fixture.orgID, fixture.connectionRevision).Scan(&changedDeploymentAxes))
	require.Zero(t, changedDeploymentAxes, "connection pause must not overwrite any sibling deployment axis")

	replayedPause, err := service.PauseConnection(ctx, PauseConnectionRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
		ActorUserID:  fixture.userID,
		ConnectionID: fixture.connectionID, ExpectedConfigRevision: fixture.connectionRevision,
	}, appendFixtureSafetyAudit(fixture.orgID, fixture.userID))
	require.NoError(t, err)
	require.False(t, replayedPause.Changed)
	_, err = pool.Exec(ctx, `UPDATE provider_connections SET admin_state='enabled' WHERE id=$1`, fixture.connectionID)
	require.NoError(t, err, "the compatibility projection permits a same-epoch mirror write")
	driftedAuthority, err := service.Check(ctx, modelrouting.AuthorityCheckRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
	})
	require.ErrorIs(t, err, modelrouting.ErrAuthorityNotReady)
	require.False(t, driftedAuthority.Ready, "a unilateral canonical resume must remain unusable in verify mode")
	_, err = pool.Exec(ctx, `UPDATE provider_connections SET admin_state='disabled' WHERE id=$1`, fixture.connectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE channels SET status='staged' WHERE id=$1`, fixture.channelID)
	require.NoError(t, err)
	resumedProjection, err := modeldeployment.ReconcileLegacyProjection(ctx, pool, orgID)
	require.NoErrorf(t, err, "verify projection must preserve legacy status-only recovery: %+v", resumedProjection)
	var resumedChannelState, resumedConnectionState string
	var resumedChannelRevision, resumedConnectionRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT channel.status,channel.routing_epoch,connection.admin_state,connection.config_revision
  FROM channels channel
  JOIN provider_connections connection ON connection.legacy_channel_id=channel.id
 WHERE channel.id=$1`, fixture.channelID).Scan(
		&resumedChannelState, &resumedChannelRevision, &resumedConnectionState, &resumedConnectionRevision,
	))
	require.Equal(t, "staged", resumedChannelState)
	require.Equal(t, "enabled", resumedConnectionState)
	require.Equal(t, fixture.connectionRevision, resumedChannelRevision)
	require.Equal(t, fixture.connectionRevision, resumedConnectionRevision)

	revoked, err := service.RevokeCredential(ctx, RevokeCredentialRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
		ActorUserID:         fixture.userID,
		CredentialBindingID: fixture.bindingID, ExpectedStateRevision: fixture.bindingRevision,
	}, appendFixtureSafetyAudit(fixture.orgID, fixture.userID))
	require.NoError(t, err)
	require.True(t, revoked.Changed)
	require.Equal(t, "retired", revoked.State)
	require.Equal(t, fixture.bindingRevision+1, revoked.Revision)

	var legacyStatus, legacySecret, bindingStatus, bindingSecret string
	var legacyRevision, bindingRevision int64
	var retiredAt *time.Time
	var links int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT legacy.status,legacy.key_enc,legacy.state_epoch,
       binding.status,binding.key_enc,binding.state_revision,binding.retired_at,
       (SELECT COUNT(*) FROM credential_binding_deployments WHERE binding_id=binding.id)
  FROM channel_keys legacy
  JOIN provider_credential_bindings binding ON binding.legacy_channel_key_id=legacy.id
 WHERE legacy.id=$1 AND binding.id=$2`, fixture.credentialID, fixture.bindingID).Scan(
		&legacyStatus, &legacySecret, &legacyRevision,
		&bindingStatus, &bindingSecret, &bindingRevision, &retiredAt, &links,
	))
	require.Equal(t, "retired", legacyStatus)
	require.Empty(t, legacySecret)
	require.Equal(t, fixture.bindingRevision+1, legacyRevision)
	require.Equal(t, "retired", bindingStatus)
	require.Empty(t, bindingSecret)
	require.Equal(t, fixture.bindingRevision+1, bindingRevision)
	require.NotNil(t, retiredAt)
	require.Positive(t, links, "credential evidence links must be retained")

	replayedRevoke, err := service.RevokeCredential(ctx, RevokeCredentialRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
		ActorUserID:         fixture.userID,
		CredentialBindingID: fixture.bindingID, ExpectedStateRevision: fixture.bindingRevision,
	}, appendFixtureSafetyAudit(fixture.orgID, fixture.userID))
	require.NoError(t, err)
	require.False(t, replayedRevoke.Changed)
	require.Equal(t, fixture.bindingRevision+1, replayedRevoke.Revision)

	var auditCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM audit_logs
 WHERE org_id=$1 AND resource IN ('provider_connection','provider_credential_binding')`, orgID).Scan(&auditCount))
	require.Equal(t, 4, auditCount, "each accepted operator action, including replay, remains auditable")
}

func TestVerifySafetyControlsRollbackBothAuthoritiesWhenAuditFails(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-safety-rollback")
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)
	_, err = service.Apply(ctx, ApplyRequest{Mode: modelrouting.ModeVerify, CustomerOrgID: orgID})
	require.NoError(t, err)
	fixture := loadLegacySafetyFixture(t, pool, orgID)
	rejectAudit := func(context.Context, pgx.Tx, SafetyAuditRecord) error { return errors.New("audit unavailable") }

	_, err = service.PauseConnection(ctx, PauseConnectionRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
		ActorUserID:  fixture.userID,
		ConnectionID: fixture.connectionID, ExpectedConfigRevision: fixture.connectionRevision,
	}, rejectAudit)
	require.ErrorIs(t, err, ErrSafetyUnavailable)
	var channelState, connectionState string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT channel.status,connection.admin_state
  FROM channels channel JOIN provider_connections connection ON connection.legacy_channel_id=channel.id
 WHERE channel.id=$1`, fixture.channelID).Scan(&channelState, &connectionState))
	require.Equal(t, "active", channelState)
	require.Equal(t, "enabled", connectionState)

	_, err = service.RevokeCredential(ctx, RevokeCredentialRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID,
		ActorUserID:         fixture.userID,
		CredentialBindingID: fixture.bindingID, ExpectedStateRevision: fixture.bindingRevision,
	}, rejectAudit)
	require.ErrorIs(t, err, ErrSafetyUnavailable)
	var legacyStatus, legacySecret, bindingStatus, bindingSecret string
	var legacyRevision, bindingRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT legacy.status,legacy.key_enc,legacy.state_epoch,
       binding.status,binding.key_enc,binding.state_revision
  FROM channel_keys legacy JOIN provider_credential_bindings binding ON binding.legacy_channel_key_id=legacy.id
 WHERE legacy.id=$1`, fixture.credentialID).Scan(
		&legacyStatus, &legacySecret, &legacyRevision, &bindingStatus, &bindingSecret, &bindingRevision,
	))
	require.Equal(t, "active", legacyStatus)
	require.NotEmpty(t, legacySecret)
	require.Equal(t, fixture.bindingRevision, legacyRevision)
	require.Equal(t, "active", bindingStatus)
	require.NotEmpty(t, bindingSecret)
	require.Equal(t, fixture.bindingRevision, bindingRevision)
}

func TestVerifyCredentialRevokeScrubsAnUnlinkedBinding(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-safety-unlinked")
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)
	_, err = service.Apply(ctx, ApplyRequest{Mode: modelrouting.ModeVerify, CustomerOrgID: orgID})
	require.NoError(t, err)
	fixture := loadLegacySafetyFixture(t, pool, orgID)
	_, err = pool.Exec(ctx, `DELETE FROM credential_binding_deployments WHERE binding_id=$1`, fixture.bindingID)
	require.NoError(t, err)

	result, err := service.RevokeCredential(ctx, RevokeCredentialRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID, ActorUserID: fixture.userID,
		CredentialBindingID: fixture.bindingID, ExpectedStateRevision: fixture.bindingRevision,
	}, appendFixtureSafetyAudit(fixture.orgID, fixture.userID))
	require.NoError(t, err)
	require.True(t, result.Changed)
	var legacyStatus, legacySecret, bindingStatus, bindingSecret string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT legacy.status,legacy.key_enc,binding.status,binding.key_enc
  FROM channel_keys legacy JOIN provider_credential_bindings binding ON binding.legacy_channel_key_id=legacy.id
 WHERE binding.id=$1`, fixture.bindingID).Scan(&legacyStatus, &legacySecret, &bindingStatus, &bindingSecret))
	require.Equal(t, "retired", legacyStatus)
	require.Empty(t, legacySecret)
	require.Equal(t, "retired", bindingStatus)
	require.Empty(t, bindingSecret)
}

func TestSafetyControlsRejectInactiveActorAndAmbiguousRootBeforeWriting(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-safety-actor")
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)
	_, err = service.Apply(ctx, ApplyRequest{Mode: modelrouting.ModeVerify, CustomerOrgID: orgID})
	require.NoError(t, err)
	fixture := loadLegacySafetyFixture(t, pool, orgID)

	_, err = pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, fixture.userID)
	require.NoError(t, err)
	_, err = service.PauseConnection(ctx, PauseConnectionRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID, ActorUserID: fixture.userID,
		ConnectionID: fixture.connectionID, ExpectedConfigRevision: fixture.connectionRevision,
	}, appendFixtureSafetyAudit(fixture.orgID, fixture.userID))
	require.ErrorIs(t, err, ErrSafetyConflict)
	assertLegacyConnectionStates(t, pool, fixture.channelID, "active", "enabled")

	_, err = pool.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, fixture.userID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE users SET role='org_viewer',organization_role='org_viewer' WHERE id=$1`, fixture.userID)
	require.NoError(t, err)
	_, err = service.PauseConnection(ctx, PauseConnectionRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID, ActorUserID: fixture.userID,
		ConnectionID: fixture.connectionID, ExpectedConfigRevision: fixture.connectionRevision,
	}, appendFixtureSafetyAudit(fixture.orgID, fixture.userID))
	require.ErrorIs(t, err, ErrSafetyConflict)
	assertLegacyConnectionStates(t, pool, fixture.channelID, "active", "enabled")
	_, err = pool.Exec(ctx, `
UPDATE users SET role='org_admin',organization_role='org_admin' WHERE id=$1`, fixture.userID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('ambiguous-safety-root','active','customer')`)
	require.NoError(t, err)
	_, err = service.RevokeCredential(ctx, RevokeCredentialRequest{
		Mode: modelrouting.ModeVerify, CustomerOrgID: orgID, ActorUserID: fixture.userID,
		CredentialBindingID: fixture.bindingID, ExpectedStateRevision: fixture.bindingRevision,
	}, appendFixtureSafetyAudit(fixture.orgID, fixture.userID))
	require.ErrorIs(t, err, ErrSafetyConflict)
	var legacyStatus, bindingStatus string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT legacy.status,binding.status
  FROM channel_keys legacy JOIN provider_credential_bindings binding ON binding.legacy_channel_key_id=legacy.id
 WHERE binding.id=$1`, fixture.bindingID).Scan(&legacyStatus, &bindingStatus))
	require.Equal(t, "active", legacyStatus)
	require.Equal(t, "active", bindingStatus)
	var audits int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE org_id=$1`, orgID).Scan(&audits))
	require.Zero(t, audits)
}

func TestCanonicalCredentialRevokeAllowsZeroDeploymentsAndRejectsForeignOwnership(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := seedLegacyAuthorityFixture(t, pool, "authority-safety-zero-deployment")
	service, err := New(pool, routingpolicy.Default())
	require.NoError(t, err)
	_, err = service.Apply(ctx, ApplyRequest{Mode: modelrouting.ModeVerify, CustomerOrgID: orgID})
	require.NoError(t, err)
	fixture := loadLegacySafetyFixture(t, pool, orgID)

	var emptyConnectionID, emptyBindingID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('empty-owner','custom','https://empty.invalid','openai_compatible','{}','enabled',1)
RETURNING id`).Scan(&emptyConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'zero-deployment-secret','active',1) RETURNING id`, emptyConnectionID).Scan(&emptyBindingID))
	result, err := service.RevokeCredential(ctx, RevokeCredentialRequest{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: orgID, ActorUserID: fixture.userID,
		CredentialBindingID: emptyBindingID, ExpectedStateRevision: 1,
	}, appendFixtureSafetyAudit(orgID, fixture.userID))
	require.NoError(t, err)
	require.True(t, result.Changed)
	var emptyStatus, emptySecret string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT status,key_enc FROM provider_credential_bindings WHERE id=$1`, emptyBindingID).Scan(&emptyStatus, &emptySecret))
	require.Equal(t, "retired", emptyStatus)
	require.Empty(t, emptySecret)

	var foreignOrgID, foreignModelID, foreignConnectionID, foreignBindingID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type,parent_id)
VALUES ('foreign-safety-child','active','customer',$1) RETURNING id`, orgID).Scan(&foreignOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family)
VALUES ($1,'foreign-safety-model','Foreign Safety Model','custom') RETURNING id`, foreignOrgID).Scan(&foreignModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('foreign-owner','custom','https://foreign.invalid','openai_compatible','{}','enabled',1)
RETURNING id`).Scan(&foreignConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status,state_revision)
VALUES ($1,'foreign-secret','active',1) RETURNING id`, foreignConnectionID).Scan(&foreignBindingID))
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployments (
    customer_org_id,public_model_id,connection_id,upstream_model,admin_state,config_revision)
VALUES ($1,$2,$3,'foreign-upstream','enabled',1)`, foreignOrgID, foreignModelID, foreignConnectionID)
	require.NoError(t, err)
	_, err = service.RevokeCredential(ctx, RevokeCredentialRequest{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: orgID, ActorUserID: fixture.userID,
		CredentialBindingID: foreignBindingID, ExpectedStateRevision: 1,
	}, appendFixtureSafetyAudit(orgID, fixture.userID))
	require.ErrorIs(t, err, ErrSafetyNotFound)
	var foreignStatus, foreignSecret string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT status,key_enc FROM provider_credential_bindings WHERE id=$1`, foreignBindingID).Scan(&foreignStatus, &foreignSecret))
	require.Equal(t, "active", foreignStatus)
	require.Equal(t, "foreign-secret", foreignSecret)
}

func assertLegacyConnectionStates(t *testing.T, pool *pgxpool.Pool, channelID int64, legacyState, canonicalState string) {
	t.Helper()
	var actualLegacy, actualCanonical string
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT channel.status,connection.admin_state
  FROM channels channel JOIN provider_connections connection ON connection.legacy_channel_id=channel.id
 WHERE channel.id=$1`, channelID).Scan(&actualLegacy, &actualCanonical))
	require.Equal(t, legacyState, actualLegacy)
	require.Equal(t, canonicalState, actualCanonical)
}

func loadLegacySafetyFixture(t *testing.T, pool *pgxpool.Pool, orgID int64) legacySafetyFixture {
	t.Helper()
	fixture := legacySafetyFixture{orgID: orgID}
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT users.id,channel.id,legacy_key.id,connection.id,connection.config_revision,
       binding.id,binding.state_revision
  FROM users
  JOIN model_deployments deployment ON deployment.customer_org_id=users.org_id
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN channels channel ON channel.id=connection.legacy_channel_id
  JOIN credential_binding_deployments link ON link.deployment_id=deployment.id
  JOIN provider_credential_bindings binding ON binding.id=link.binding_id
  JOIN channel_keys legacy_key ON legacy_key.id=binding.legacy_channel_key_id
 WHERE users.org_id=$1 AND users.organization_role='org_admin'
 ORDER BY deployment.id,binding.id LIMIT 1`, orgID).Scan(
		&fixture.userID, &fixture.channelID, &fixture.credentialID,
		&fixture.connectionID, &fixture.connectionRevision,
		&fixture.bindingID, &fixture.bindingRevision,
	))
	return fixture
}

func addSharedSiblingProjection(t *testing.T, pool *pgxpool.Pool, fixture legacySafetyFixture) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
UPDATE channels
   SET models='authority-model,authority-sibling',
       model_mapping='{"authority-model":"authority-upstream","authority-sibling":"authority-sibling-upstream"}'::jsonb,
       updated_at=clock_timestamp()
 WHERE id=$1`, fixture.channelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO channel_model_probes (
    channel_id,public_model,upstream_model,reported_model,routing_epoch,
    credential_id,credential_state_epoch,success,status_code,latency_ms,
    error_summary,tested_by,tested_at)
VALUES ($1,'authority-sibling','authority-sibling-upstream','authority-sibling-upstream',$2,
        $3,$4,TRUE,200,8,'',$5,clock_timestamp())`,
		fixture.channelID, fixture.connectionRevision, fixture.credentialID, fixture.bindingRevision, fixture.userID)
	require.NoError(t, err)
}

func appendFixtureSafetyAudit(orgID, userID int64) SafetyAuditAppender {
	return func(ctx context.Context, tx pgx.Tx, record SafetyAuditRecord) error {
		detail, err := json.Marshal(record.Detail)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
INSERT INTO audit_logs (org_id,user_id,action,resource,resource_id,detail)
VALUES ($1,$2,$3,$4,$5,$6::jsonb)`, orgID, userID, record.Action, record.ResourceType, record.ResourceID, string(detail))
		return err
	}
}

func seedLegacyAuthorityFixture(t *testing.T, pool *pgxpool.Pool, suffix string) int64 {
	t.Helper()
	ctx := context.Background()
	var orgID, userID, channelID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ($1,'active','customer') RETURNING id`, suffix).Scan(&orgID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_registry (
    org_id,model_name,display_name,provider_family,lifecycle,
    context_length,output_limit,approved_catalog)
VALUES ($1,'authority-model','Authority Model','custom','ga',128000,8192,TRUE)`, orgID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
VALUES ($1,$2,'fixture','org_admin','active','org_admin') RETURNING id`,
		orgID, suffix+"-owner").Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
    name,provider,base_url,models,model_mapping,status,
    routing_epoch,circuit_epoch,priority,routing_weight,max_context,default_max_output)
VALUES ($1,'custom','https://provider.invalid','authority-model',
        '{"authority-model":"authority-upstream"}'::jsonb,'active',3,2,10,100,128000,8192)
RETURNING id`, suffix+"-channel").Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status,state_epoch)
VALUES ($1,'encrypted-fixture','authority-model','active',4)
RETURNING id`, channelID).Scan(&keyID))
	probeTime := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	_, err = pool.Exec(ctx, `
INSERT INTO channel_model_probes (
    channel_id,public_model,upstream_model,reported_model,routing_epoch,
    credential_id,credential_state_epoch,success,status_code,latency_ms,
    error_summary,tested_by,tested_at)
VALUES ($1,'authority-model','authority-upstream','authority-upstream',3,
        $2,4,TRUE,200,7,'',$3,$4)`, channelID, keyID, userID, probeTime)
	require.NoError(t, err)

	candidate := runtimechange.Candidate{
		ContractVersion: runtimechange.CandidateVersion,
		ModelName:       "authority-model",
		Overrides: []routingpolicy.ChannelOverride{{
			ChannelID: channelID, ExpectedRoutingEpoch: 3, Admission: routingpolicy.AdmissionAllow,
		}},
	}
	candidateJSON, err := json.Marshal(candidate)
	require.NoError(t, err)
	digest := sha256.Sum256(candidateJSON)
	candidateHash := "sha256:" + hex.EncodeToString(digest[:])
	var releaseID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
    candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
    reason,created_by,validated_by,activated_by,validated_at,activated_at)
VALUES ($1,'authority-model','unirouter.runtime-change-candidate.v1',$2,
        'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        $3::jsonb,'[]','{}','unchanged','active',1,
        'model route authority fixture',$4,$4,$4,NOW(),NOW())
RETURNING id`, "rcr_"+suffix, candidateHash, string(candidateJSON), userID).Scan(&releaseID))
	_, err = pool.Exec(ctx, `
INSERT INTO runtime_change_slots (model_name,stable_release_id,canary_basis_points,slot_version)
VALUES ('authority-model',$1,0,1)`, releaseID)
	require.NoError(t, err)
	return orgID
}

func addLegacyCanaryFixture(t *testing.T, pool *pgxpool.Pool, modelName, releaseRef string) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	var stableID, canaryID int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT stable_release_id FROM runtime_change_slots WHERE model_name=$1`, modelName).Scan(&stableID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO runtime_change_releases (
    release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
    candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
    parent_stable_release_id,canary_basis_points,reason,created_by,
    validated_by,activated_by,validated_at,activated_at)
SELECT $2,stable.model_name,stable.candidate_version,stable.candidate_hash,
       stable.candidate_hash,stable.candidate,stable.provider_pack_pins,
       stable.simulation_report,stable.overall_risk,'canary',4,
       stable.id,250,'authority canary fixture',stable.created_by,
       stable.created_by,stable.created_by,clock_timestamp(),clock_timestamp()
  FROM runtime_change_releases stable WHERE stable.id=$1
RETURNING id`, stableID, releaseRef).Scan(&canaryID))
	_, err := pool.Exec(ctx, `
UPDATE runtime_change_slots
   SET canary_release_id=$2,canary_basis_points=250,
       slot_version=slot_version+1,updated_at=clock_timestamp()
 WHERE model_name=$1 AND stable_release_id=$3 AND canary_release_id IS NULL`, modelName, canaryID, stableID)
	require.NoError(t, err)
	return stableID, canaryID
}

func hasModelDeploymentIssue(issues []modeldeployment.Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func authoritySequenceSnapshot(t *testing.T, pool *pgxpool.Pool) map[string]sql.NullInt64 {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
SELECT sequencename,last_value
  FROM pg_sequences
 WHERE schemaname='public'
 ORDER BY sequencename`)
	require.NoError(t, err)
	defer rows.Close()
	result := make(map[string]sql.NullInt64)
	for rows.Next() {
		var name string
		var value sql.NullInt64
		require.NoError(t, rows.Scan(&name, &value))
		result[name] = value
	}
	require.NoError(t, rows.Err())
	return result
}

func authorityFactSnapshot(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var value string
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT CONCAT_WS('|',
    (SELECT COUNT(*) FROM model_registry),
    (SELECT COUNT(*) FROM provider_connections),
    (SELECT COUNT(*) FROM model_deployments),
    (SELECT COUNT(*) FROM provider_credential_bindings),
    (SELECT COUNT(*) FROM credential_binding_deployments),
    (SELECT COUNT(*) FROM model_deployment_operations),
    (SELECT COUNT(*) FROM model_deployment_probes),
    (SELECT COUNT(*) FROM runtime_route_revision_items),
    (SELECT STRING_AGG(release.id::text || ':' || release.version::text || ':' ||
        COALESCE(release.route_items_hash,''),',' ORDER BY release.id)
       FROM runtime_change_releases release),
    (SELECT STRING_AGG(slot.model_name || ':' || slot.slot_version::text,',' ORDER BY slot.model_name)
       FROM runtime_change_slots slot))`).Scan(&value))
	return value
}

func authorityIssueCodes(report modelrouting.AuthorityCheckReport) []string {
	result := make([]string, 0, len(report.BlockingIssues))
	for _, issue := range report.BlockingIssues {
		result = append(result, issue.Code)
	}
	return result
}

func hasAuthorityIssue(issues []modelrouting.AuthorityIssue, code, operationID, classification string) bool {
	for _, issue := range issues {
		if issue.Code == code && issue.OperationID == operationID && issue.Classification == classification {
			return true
		}
	}
	return false
}
