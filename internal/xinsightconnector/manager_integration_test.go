//go:build integration

package xinsightconnector

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

type recordingAuthorizer struct {
	actions []string
	err     error
}

func (authorizer *recordingAuthorizer) AuthorizeConnectorControl(
	_ context.Context,
	_ pgx.Tx,
	_, _, _ int64,
	action string,
) error {
	authorizer.actions = append(authorizer.actions, action)
	return authorizer.err
}

type sourceAuthorityFixture struct {
	enabled  bool
	instance string
}

func (fixture sourceAuthorityFixture) Enabled() bool          { return fixture.enabled }
func (fixture sourceAuthorityFixture) SourceInstance() string { return fixture.instance }

func TestConnectorManagerDurableLifecycleFencingAndBoundedHealthIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('connector-cp1','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,role,status) VALUES ($1,'connector-owner','hash','platform_admin','active') RETURNING id`,
		orgID,
	).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status,created_by) VALUES ($1,'Connector CP1','connector-cp1','active',$2) RETURNING id`,
		orgID, actorID,
	).Scan(&projectID))

	authorizer := &recordingAuthorizer{}
	sourceAuthority := &sourceAuthorityFixture{enabled: true, instance: "source.cp1"}
	manager, err := NewManager(pool, authorizer, sourceAuthority)
	require.NoError(t, err)
	clock := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return clock }
	manager.token = func() (string, error) { return "11111111111111111111111111111111", nil }
	consent := Consent{
		InstallationID: "xci_AbCdEfGhIjKlMnOpQrStUv", XDeploymentID: "xid_ZyXwVuTsRqPoNmLkJiHgFe",
		InvitationSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		OrgID:            orgID, ProjectID: projectID, EventScope: EventScopeModelCompleted,
		IdempotencyKey: "connect-cp1", ConsentedByUserID: actorID,
	}
	installation, connect, err := manager.CreateInstallation(ctx, consent)
	require.NoError(t, err)
	require.Equal(t, ObservedProvisioning, installation.ObservedState)
	require.Equal(t, DesiredActive, installation.DesiredState)
	require.Equal(t, OperationQueued, connect.State)
	require.Equal(t, int64(1), connect.ControlFence)
	require.Equal(t, []string{ActionConnect}, authorizer.actions)

	replayedInstallation, replayedConnect, err := manager.CreateInstallation(ctx, consent)
	require.NoError(t, err)
	require.Equal(t, installation.InstallationID, replayedInstallation.InstallationID)
	require.Equal(t, connect.OperationRef, replayedConnect.OperationRef)
	var installations, operations int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM xinsight_connector_installations`).Scan(&installations))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM xinsight_connector_operations`).Scan(&operations))
	require.Equal(t, 1, installations)
	require.Equal(t, 1, operations)

	var secondProjectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status,created_by) VALUES ($1,'Connector CP1 replay','connector-cp1-replay','active',$2) RETURNING id`,
		orgID, actorID,
	).Scan(&secondProjectID))
	_, _, err = manager.CreateInstallation(ctx, Consent{
		InstallationID: "xci_0123456789AbCdEfGhIjKl", XDeploymentID: "xid_0123456789AbCdEfGhIjKl",
		InvitationSHA256: consent.InvitationSHA256, OrgID: orgID, ProjectID: secondProjectID,
		EventScope: EventScopeModelCompleted, IdempotencyKey: "connect-replayed-invitation",
		ConsentedByUserID: actorID,
	})
	require.ErrorIs(t, err, ErrConflict, "an invitation digest is globally single-use")

	var otherOrgID, otherProjectID, otherActorID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('connector-cp1-other','active','standard') RETURNING id`,
	).Scan(&otherOrgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,role,status) VALUES ($1,'connector-other-owner','hash','platform_admin','active') RETURNING id`,
		otherOrgID,
	).Scan(&otherActorID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status,created_by) VALUES ($1,'Connector other','connector-other','active',$2) RETURNING id`,
		otherOrgID, otherActorID,
	).Scan(&otherProjectID))
	_, _, err = manager.CreateInstallation(ctx, Consent{
		InstallationID: "xci_QrStUvWxYz0123456789Ab", XDeploymentID: consent.XDeploymentID,
		InvitationSHA256: "sha256:abababababababababababababababababababababababababababababababab",
		OrgID:            otherOrgID, ProjectID: otherProjectID, EventScope: EventScopeModelCompleted,
		IdempotencyKey: "connect-cross-org", ConsentedByUserID: otherActorID,
	})
	require.ErrorIs(t, err, ErrConflict, "one X deployment cannot have live installations in multiple organizations")

	claimedConnect, ok, err := manager.ClaimNext(ctx, "fake-x-control", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, connect.OperationRef, claimedConnect.OperationRef)
	require.Equal(t, 1, claimedConnect.AttemptCount)
	clock = clock.Add(time.Second)
	syncAt := clock
	installation, connect, err = manager.CompleteOperation(ctx,
		claimedConnect.OperationRef, claimedConnect.LeaseOwner, claimedConnect.LeaseToken,
		OperationOutcome{
			ObservedState: ObservedConnected, ResultCode: "bootstrap_complete",
			ResultSHA256:           "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			BootstrapReceiptSHA256: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			SuccessfulSyncAt:       &syncAt,
		},
	)
	require.NoError(t, err)
	require.Equal(t, OperationSucceeded, connect.State)
	require.Equal(t, ObservedConnected, installation.ObservedState)
	require.Equal(t, int64(2), installation.StateRevision)

	pauseRequest := OperationRequest{
		InstallationID: installation.InstallationID, OrgID: orgID, ProjectID: projectID,
		Action: ActionPause, IdempotencyKey: "pause-cp1", RequestedByUserID: actorID,
	}
	pause, err := manager.RequestOperation(ctx, pauseRequest)
	require.NoError(t, err)
	replayedPause, err := manager.RequestOperation(ctx, pauseRequest)
	require.NoError(t, err)
	require.Equal(t, pause.OperationRef, replayedPause.OperationRef)
	require.Equal(t, pause.ControlFence, replayedPause.ControlFence)

	claimedPause, ok, err := manager.ClaimNext(ctx, "fake-x-control", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, pause.OperationRef, claimedPause.OperationRef)
	resume, err := manager.RequestOperation(ctx, OperationRequest{
		InstallationID: installation.InstallationID, OrgID: orgID, ProjectID: projectID,
		Action: ActionResume, IdempotencyKey: "resume-cp1", RequestedByUserID: actorID,
	})
	require.NoError(t, err)
	require.Greater(t, resume.ControlFence, claimedPause.ControlFence)
	require.Equal(t, OperationCancelled, mustReadOperation(t, pool, claimedPause.OperationRef).State)
	_, _, err = manager.CompleteOperation(ctx,
		claimedPause.OperationRef, claimedPause.LeaseOwner, claimedPause.LeaseToken,
		OperationOutcome{ObservedState: ObservedPaused, ResultCode: "paused"},
	)
	require.ErrorIs(t, err, ErrConflict, "a newer desired-state fence must reject the stale result")

	type claimResult struct {
		operation Operation
		claimed   bool
		err       error
	}
	claimResults := make(chan claimResult, 2)
	startClaims := make(chan struct{})
	var claimers sync.WaitGroup
	for _, owner := range []string{"fake-x-control-a", "fake-x-control-b"} {
		claimer := owner
		claimers.Add(1)
		go func() {
			defer claimers.Done()
			<-startClaims
			operation, claimed, claimErr := manager.ClaimNext(ctx, claimer, time.Minute)
			claimResults <- claimResult{operation: operation, claimed: claimed, err: claimErr}
		}()
	}
	close(startClaims)
	claimers.Wait()
	close(claimResults)
	var resumeClaim Operation
	var claimedCount int
	for result := range claimResults {
		require.NoError(t, result.err)
		if result.claimed {
			claimedCount++
			resumeClaim = result.operation
		}
	}
	require.Equal(t, 1, claimedCount, "concurrent fake agents must claim one fenced operation once")
	require.Equal(t, resume.OperationRef, resumeClaim.OperationRef)
	clock = clock.Add(time.Second)
	installation, resume, err = manager.CompleteOperation(ctx,
		resumeClaim.OperationRef, resumeClaim.LeaseOwner, resumeClaim.LeaseToken,
		OperationOutcome{ObservedState: ObservedConnected, ResultCode: "resumed"},
	)
	require.NoError(t, err)
	require.Equal(t, OperationSucceeded, resume.State)

	// Make the Source-side feed truth consistent with the completed fake-X
	// connect result, then create more than the read-model cap without any
	// network activity.
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO api_keys (
    org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix,
    status, rpm_limit, qps_limit, budget_limit, allowed_ips, allowed_models,
    policy_labels, purpose, verifier_scheme, registration_operation_id,
    created_at, updated_at)
VALUES ($1,$2,NULL,'service','Connector CP1 feed',$3,'ur-xf1-1111111111111',
        'feed_active',60,1,0,'127.0.0.1/32','','','xinsight.feed.read',
        'receiver_sha256_v1','11111111111111111111111111111111',NOW(),NOW())
RETURNING id`, orgID, projectID, fmt.Sprintf("%064d", 1)).Scan(new(int64)))
	_, err = pool.Exec(ctx, `UPDATE completed_execution_feed_connections
SET status='active', acknowledged_sequence=0, last_authenticated_pull_at=$3
WHERE org_id=$1 AND project_id=$2`, orgID, projectID, clock)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO completed_execution_feed_states (org_id,project_id,retained_after_id)
VALUES ($1,$2,0) ON CONFLICT (org_id,project_id) DO NOTHING`, orgID, projectID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO completed_execution_feed_items (
    event_key,org_id,project_id,request_id,terminal_status,terminal_available_at,
    snapshot, snapshot_sha256, created_at)
SELECT 'connector-event-'||value, $1, $2, 'connector-request-'||value,
	       'settled', $3::timestamptz, '{}'::bytea,
       'sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
	       $3::timestamptz - INTERVAL '2 minutes'
FROM generate_series(1,$4) AS value`, orgID, projectID, clock, HealthBacklogCap+1)
	require.NoError(t, err)

	state, err := manager.ReadProjectState(ctx, orgID, projectID)
	require.NoError(t, err)
	require.NotNil(t, state.Installation)
	require.Equal(t, ProductConnected, state.Health.ProductStatus)
	require.Equal(t, HealthBacklogCap, state.Health.PendingItemCount)
	require.True(t, state.Health.PendingItemCountCapped)
	require.Equal(t, int64(120), state.Health.OldestPendingAgeSeconds)
	require.Equal(t, 1, state.Health.ActiveCredentialCount)
	require.Equal(t, 0, state.Health.StagedCredentialCount)
	sourceAuthority.enabled = false
	state, err = manager.ReadProjectState(ctx, orgID, projectID)
	require.NoError(t, err)
	require.Equal(t, ProductAbnormal, state.Health.ProductStatus, "disabled Source authority must not report connected")
	sourceAuthority.enabled = true
	sourceAuthority.instance = "source.drifted"
	state, err = manager.ReadProjectState(ctx, orgID, projectID)
	require.NoError(t, err)
	require.Equal(t, ProductAbnormal, state.Health.ProductStatus, "Source identity drift must fail closed")
	sourceAuthority.instance = "source.cp1"
	_, err = pool.Exec(ctx, `UPDATE projects SET status='disabled' WHERE id=$1 AND org_id=$2`, projectID, orgID)
	require.NoError(t, err)
	state, err = manager.ReadProjectState(ctx, orgID, projectID)
	require.NoError(t, err)
	require.Equal(t, ProductAbnormal, state.Health.ProductStatus, "inactive project authority must not report connected")
	_, err = pool.Exec(ctx, `UPDATE projects SET status='active' WHERE id=$1 AND org_id=$2`, projectID, orgID)
	require.NoError(t, err)

	var forbiddenColumns int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
WHERE table_schema='public'
  AND table_name IN ('xinsight_connector_installations','xinsight_connector_operations')
  AND column_name ~ '(cursor|checkpoint|epoch|url|origin|endpoint)'`).Scan(&forbiddenColumns))
	require.Zero(t, forbiddenColumns, "UniRouter CP1 must not persist X Cursor/Checkpoint/epoch or package-supplied network targets")
}

func TestConnectorManagerCommitAuthorizationDenialHasNoMutationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('connector-denied','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,role,status) VALUES ($1,'connector-denied-owner','hash','platform_admin','active') RETURNING id`, orgID,
	).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status,created_by) VALUES ($1,'Connector Denied','connector-denied','active',$2) RETURNING id`, orgID, actorID,
	).Scan(&projectID))

	manager, err := NewManager(pool, &recordingAuthorizer{err: ErrUnauthorized}, sourceAuthorityFixture{enabled: true, instance: "source.denied"})
	require.NoError(t, err)
	_, _, err = manager.CreateInstallation(ctx, Consent{
		InstallationID: "xci_DeniedAbCdEfGhIjKlMnOp", XDeploymentID: "xid_DeniedAbCdEfGhIjKlMnOp",
		InvitationSHA256: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		OrgID:            orgID, ProjectID: projectID, EventScope: EventScopeModelCompleted,
		IdempotencyKey: "connect-denied", ConsentedByUserID: actorID,
	})
	require.ErrorIs(t, err, ErrUnauthorized)
	var installations, operations, connections int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM xinsight_connector_installations`).Scan(&installations))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM xinsight_connector_operations`).Scan(&operations))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM completed_execution_feed_connections`).Scan(&connections))
	require.Zero(t, installations)
	require.Zero(t, operations)
	require.Zero(t, connections)
}

func mustReadOperation(t *testing.T, pool interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, operationRef string) Operation {
	t.Helper()
	operation, err := readOperation(context.Background(), pool, operationRef, false)
	require.NoError(t, err)
	return operation
}
