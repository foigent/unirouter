//go:build integration

package accessprovisioning

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/accesssponsor"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/principallock"
	"github.com/uniroute/unirouter/internal/testutil"
)

type accessServiceFixture struct {
	orgID, projectID                          int64
	subjectID, orgAdminID, secondOrgAdminID   int64
	sponsorID, otherProjectUserID, workloadID int64
	actor                                     *Actor
	service                                   *Service
}

func newAccessServiceFixture(t *testing.T) accessServiceFixture {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var fixture accessServiceFixture
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('access-service-org','active','customer') RETURNING id`).Scan(&fixture.orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'Access Service Project','access-service-project','active') RETURNING id`, fixture.orgID).Scan(&fixture.projectID))
	seedUser := func(username, organizationRole, compatibilityRole, projectRole string) int64 {
		t.Helper()
		var userID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash',$3,$4,'active') RETURNING id`,
			fixture.orgID, username, organizationRole, compatibilityRole).Scan(&userID))
		if projectRole != "" {
			_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,$3)`,
				fixture.projectID, userID, projectRole)
			require.NoError(t, err)
		}
		return userID
	}
	fixture.subjectID = seedUser("access-subject", auth.RoleMember, auth.RoleDeveloper, auth.ProjectRoleUser)
	fixture.orgAdminID = seedUser("access-org-admin", auth.RoleOrgAdmin, auth.RoleOrgAdmin, "")
	fixture.secondOrgAdminID = seedUser("access-second-org-admin", auth.RoleOrgAdmin, auth.RoleOrgAdmin, "")
	fixture.sponsorID = seedUser("access-workload-sponsor", auth.RoleMember, auth.RoleDeveloper, auth.ProjectRoleUser)
	fixture.otherProjectUserID = seedUser("access-other-project-user", auth.RoleMember, auth.RoleDeveloper, auth.ProjectRoleUser)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO workload_applications (
		  contract_version,identity_contract_version,org_id,project_id,application_ref,
		  logical_ref,display_name,kind,environment,identity_mode,owner_team,status,
		  environment_locked_at,created_by,updated_by
		) VALUES (
		  'unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,
		  'app_access_service_fixture','workload_access_service_fixture','Access service fixture',
		  'agent','development','service_credential','platform','active',NOW(),$3,$3
		) RETURNING id`, fixture.orgID, fixture.projectID, fixture.orgAdminID).Scan(&fixture.workloadID))

	fixture.actor = &Actor{}
	authorizer := CommitAuthorizerFunc(func(ctx context.Context, tx pgx.Tx, requirement AuthorizationRequirement) (Actor, error) {
		actor := *fixture.actor
		if actor.UserID <= 0 {
			return Actor{}, ErrAuthorizationChanged
		}
		var status, organizationRole string
		if err := tx.QueryRow(ctx, `SELECT status,organization_role FROM users WHERE id=$1 FOR UPDATE`, actor.UserID).Scan(&status, &organizationRole); err != nil {
			return Actor{}, err
		}
		if status != "active" || organizationRole != actor.OrganizationRole {
			return Actor{}, ErrAuthorizationChanged
		}
		if requirement.OrgID != fixture.orgID && organizationRole != auth.RolePlatformAdmin {
			return Actor{}, ErrAuthorizationChanged
		}
		if requirement.ProjectID > 0 {
			var projectOrgID int64
			var projectStatus string
			if err := tx.QueryRow(ctx, `SELECT org_id,status FROM projects WHERE id=$1 FOR SHARE`, requirement.ProjectID).Scan(
				&projectOrgID, &projectStatus,
			); err != nil {
				return Actor{}, err
			}
			if projectOrgID != requirement.OrgID || (projectStatus != "active" && !requirement.AllowInactiveProject) {
				return Actor{}, ErrAuthorizationChanged
			}
		}
		return actor, nil
	})
	memberships := MembershipProvisionerFunc(func(context.Context, pgx.Tx, Actor, int64, int64, int64, string) (MembershipChange, error) {
		return MembershipChange{}, nil
	})
	service, err := NewService(pool, "access-service-integration-key-hash-secret", authorizer, memberships)
	require.NoError(t, err)
	fixture.service = service
	return fixture
}

func (fixture *accessServiceFixture) useActor(userID int64, organizationRole, projectRole string) ActorScope {
	*fixture.actor = Actor{UserID: userID, OrgID: fixture.orgID, OrganizationRole: organizationRole, ProjectRole: projectRole}
	return ActorScope{UserID: userID, OrgID: fixture.orgID, OrganizationRole: organizationRole}
}

func (fixture accessServiceFixture) personalInput(clientRequestID, mode string) ProvisionInput {
	return ProvisionInput{
		ClientRequestID: clientRequestID,
		RequestID:       "req-" + clientRequestID,
		OrgID:           fixture.orgID,
		ProjectID:       fixture.projectID,
		Subject:         Subject{Type: SubjectUser, UserID: fixture.subjectID},
		RequestKind:     RequestKindHumanPersonal,
		Envelope:        Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: "project_default"},
		Environment:     PersonalEnvironment,
		SubmissionMode:  mode,
	}
}

func TestDecideAndSponsorTransitionUsePrincipalThenRequestGrantLockOrderIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	adminScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	workloadInput := func(clientRequestID string, rpm *int) ProvisionInput {
		return ProvisionInput{
			ClientRequestID: clientRequestID, RequestID: "req-" + clientRequestID,
			OrgID: fixture.orgID, ProjectID: fixture.projectID,
			Subject: Subject{Type: SubjectWorkloadApplication,
				WorkloadApplicationID: fixture.workloadID},
			RequestKind: RequestKindWorkloadService,
			Envelope: Envelope{ProjectRole: auth.ProjectRoleUser,
				ScopeMode: "project_default", RPMLimit: rpm},
			Environment: "development", SubmissionMode: SubmissionDelegated,
			SponsorUserID: fixture.sponsorID,
		}
	}
	initialInput := workloadInput("sponsor-lock-initial", nil)
	initialInput.SubmissionMode = SubmissionDirectGrant
	initial, err := fixture.service.Provision(ctx, initialInput)
	require.NoError(t, err)
	require.NotNil(t, initial.Grant)

	newRaceService := func(beforePrincipalLock, afterPrincipalLock func()) *Service {
		authorizer := CommitAuthorizerFunc(func(ctx context.Context, tx pgx.Tx, requirement AuthorizationRequirement) (Actor, error) {
			if beforePrincipalLock != nil {
				beforePrincipalLock()
			}
			userIDs := append([]int64{fixture.orgAdminID}, requirement.AdditionalUserIDs...)
			if err := principallock.LockUsers(ctx, tx, userIDs...); err != nil {
				return Actor{}, err
			}
			if err := principallock.LockWorkloads(ctx, tx, requirement.AdditionalWorkloadIDs...); err != nil {
				return Actor{}, err
			}
			if afterPrincipalLock != nil {
				afterPrincipalLock()
			}
			return Actor{UserID: fixture.orgAdminID, OrgID: fixture.orgID,
				OrganizationRole: auth.RoleOrgAdmin}, nil
		})
		service, serviceErr := NewService(fixture.service.db, "access-service-integration-key-hash-secret", authorizer,
			MembershipProvisionerFunc(func(context.Context, pgx.Tx, Actor, int64, int64, int64, string) (MembershipChange, error) {
				return MembershipChange{}, nil
			}))
		require.NoError(t, serviceErr)
		return service
	}
	assertNotDeadlock := func(t *testing.T, err error) {
		t.Helper()
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			require.NotEqual(t, "40P01", pgErr.Code, "principal/request/Grant ordering must not rely on PostgreSQL deadlock abort")
		}
	}
	transition := func(tx pgx.Tx) error {
		return accesssponsor.ValidatePostTransition(ctx, tx, accesssponsor.PostTransitionState{
			UserID: fixture.sponsorID, OrgID: fixture.orgID, Status: "active",
			OrganizationRole: auth.RoleMember,
			MembershipOverride: &accesssponsor.MembershipOverride{
				ProjectID: fixture.projectID, Present: false,
			},
		})
	}
	decideInput := func(request Request, suffix string) DecisionInput {
		return DecisionInput{
			RequestRef: request.RequestRef, ExpectedRevision: request.Revision,
			Decision: "approve", ReasonCodes: []string{ReasonManualApproved},
			ClientRequestID: "sponsor-lock-decide-" + suffix,
			RequestID:       "req-sponsor-lock-decide-" + suffix,
		}
	}

	t.Run("decision owns principal lane first", func(t *testing.T) {
		rpm := 61
		pending, provisionErr := fixture.service.Provision(ctx, workloadInput("sponsor-lock-pending-decision-first", &rpm))
		require.NoError(t, provisionErr)
		require.Equal(t, RequestNeedsReview, pending.Request.Status)

		principalLocked := make(chan struct{})
		releaseDecision := make(chan struct{})
		service := newRaceService(nil, func() {
			close(principalLocked)
			<-releaseDecision
		})
		decisionResult := make(chan error, 1)
		go func() {
			_, decideErr := service.Decide(ctx, adminScope, decideInput(pending.Request, "decision-first"))
			decisionResult <- decideErr
		}()
		<-principalLocked

		transitionStarted := make(chan struct{})
		transitionResult := make(chan error, 1)
		go func() {
			tx, beginErr := fixture.service.db.Begin(ctx)
			if beginErr != nil {
				transitionResult <- beginErr
				return
			}
			defer tx.Rollback(ctx) //nolint:errcheck
			close(transitionStarted)
			transitionResult <- transition(tx)
		}()
		<-transitionStarted
		close(releaseDecision)
		decideErr := <-decisionResult
		transitionErr := <-transitionResult
		assertNotDeadlock(t, decideErr)
		assertNotDeadlock(t, transitionErr)
		require.NoError(t, decideErr)
		require.ErrorIs(t, transitionErr, accesssponsor.ErrPostTransitionOrphaned)
	})

	t.Run("transition owns principal lane first", func(t *testing.T) {
		rpm := 62
		pending, provisionErr := fixture.service.Provision(ctx, workloadInput("sponsor-lock-pending-transition-first", &rpm))
		require.NoError(t, provisionErr)
		require.Equal(t, RequestNeedsReview, pending.Request.Status)

		transitionTx, beginErr := fixture.service.db.Begin(ctx)
		require.NoError(t, beginErr)
		defer transitionTx.Rollback(ctx) //nolint:errcheck
		require.NoError(t, principallock.LockUser(ctx, transitionTx, fixture.sponsorID))

		decisionAttempted := make(chan struct{})
		service := newRaceService(func() { close(decisionAttempted) }, nil)
		decisionResult := make(chan error, 1)
		go func() {
			_, decideErr := service.Decide(ctx, adminScope, decideInput(pending.Request, "transition-first"))
			decisionResult <- decideErr
		}()
		<-decisionAttempted
		transitionErr := transition(transitionTx)
		assertNotDeadlock(t, transitionErr)
		require.ErrorIs(t, transitionErr, accesssponsor.ErrPostTransitionOrphaned)
		require.NoError(t, transitionTx.Rollback(ctx))
		decideErr := <-decisionResult
		assertNotDeadlock(t, decideErr)
		require.NoError(t, decideErr)
	})
}

func TestProvisionReplayUsesImmutableGrantRevisionAndRevokedGrantStartsNewLifecycleIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	fixedNow := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return fixedNow }
	userScope := fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	revisionOneInput := fixture.personalInput("personal-revision-one", SubmissionSelf)
	revisionOneExpiry := fixedNow.Add(time.Hour)
	revisionOneInput.ValidUntil = &revisionOneExpiry
	revisionOne, err := fixture.service.Provision(ctx, revisionOneInput)
	require.NoError(t, err)
	require.NotNil(t, revisionOne.Grant)
	require.EqualValues(t, 1, revisionOne.Grant.Revision)
	require.Equal(t, &revisionOneExpiry, revisionOne.Grant.ValidUntil)

	orgAdminScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	revisionTwo, err := fixture.service.Provision(ctx, fixture.personalInput("personal-revision-two", SubmissionDirectGrant))
	require.NoError(t, err)
	require.NotNil(t, revisionTwo.Grant)
	require.Equal(t, revisionOne.Grant.ID, revisionTwo.Grant.ID)
	require.Equal(t, revisionOne.Grant.GrantRef, revisionTwo.Grant.GrantRef)
	require.EqualValues(t, 2, revisionTwo.Grant.Revision)

	fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	replay, err := fixture.service.Provision(ctx, revisionOneInput)
	require.NoError(t, err)
	require.True(t, replay.Idempotent)
	require.NotNil(t, replay.Grant)
	require.Equal(t, revisionOne.Grant.ID, replay.Grant.ID)
	require.Equal(t, revisionOne.Grant.GrantRef, replay.Grant.GrantRef)
	require.Equal(t, revisionOne.Request.ID, replay.Grant.AccessRequestID)
	require.EqualValues(t, 1, replay.Grant.Revision)
	require.Equal(t, GrantActive, replay.Grant.Status)
	require.NotNil(t, replay.Grant.ValidUntil)
	require.True(t, replay.Grant.ValidUntil.Equal(revisionOneExpiry))

	mismatch := revisionOneInput
	mismatchExpiry := fixedNow.Add(2 * time.Hour)
	mismatch.ValidUntil = &mismatchExpiry
	_, err = fixture.service.Provision(ctx, mismatch)
	require.ErrorIs(t, err, ErrConflict)

	credential, err := fixture.service.IssueCredential(ctx, userScope, CredentialInput{
		GrantRef: revisionTwo.Grant.GrantRef, Name: "personal pre-revoke",
		ClientRequestID: "personal-pre-revoke-key", RequestID: "req-personal-pre-revoke-key",
	})
	require.NoError(t, err)
	require.True(t, credential.SecretAvailable)

	fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	revoked, err := fixture.service.ChangeGrantLifecycle(ctx, orgAdminScope, GrantLifecycleInput{
		GrantRef: revisionTwo.Grant.GrantRef, ExpectedRevision: revisionTwo.Grant.Revision,
		Action: "revoke", Reason: "authorization lifecycle ended",
		RequestID: "req-revoke-first-grant", ClientRequestID: "revoke-first-grant",
	})
	require.NoError(t, err)
	require.Equal(t, GrantRevoked, revoked.Status)

	secondLifecycleInput := fixture.personalInput("personal-second-lifecycle", SubmissionDirectGrant)
	secondLifecycle, err := fixture.service.Provision(ctx, secondLifecycleInput)
	require.NoError(t, err)
	require.NotNil(t, secondLifecycle.Grant)
	require.NotEqual(t, revisionOne.Grant.ID, secondLifecycle.Grant.ID)
	require.NotEqual(t, revisionOne.Grant.GrantRef, secondLifecycle.Grant.GrantRef)
	require.EqualValues(t, 1, secondLifecycle.Grant.Revision)
	require.Equal(t, GrantActive, secondLifecycle.Grant.Status)

	secondReplay, err := fixture.service.Provision(ctx, secondLifecycleInput)
	require.NoError(t, err)
	require.True(t, secondReplay.Idempotent)
	require.NotNil(t, secondReplay.Grant)
	require.Equal(t, secondLifecycle.Grant.ID, secondReplay.Grant.ID)
	require.Equal(t, secondLifecycle.Grant.GrantRef, secondReplay.Grant.GrantRef)

	var grantCount int
	var storedGrantID int64
	var firstGrantStatus string
	require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT COUNT(*) FROM access_grants WHERE org_id=$1 AND project_id=$2 AND subject_user_id=$3`,
		fixture.orgID, fixture.projectID, fixture.subjectID).Scan(&grantCount))
	require.Equal(t, 2, grantCount)
	require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT access_grant_id FROM api_keys WHERE id=$1`, credential.ID).Scan(&storedGrantID))
	require.Equal(t, revisionOne.Grant.ID, storedGrantID)
	require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT status FROM access_grants WHERE id=$1`, storedGrantID).Scan(&firstGrantStatus))
	require.Equal(t, GrantRevoked, firstGrantStatus)
}

func TestPlatformSubmitterCanCancelCrossOrganizationMigrationRequestIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	var platformOrgID, platformUserID, unrelatedPlatformUserID int64
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('access-platform-cancel-org','active','partner') RETURNING id`).Scan(&platformOrgID))
	for username, target := range map[string]*int64{
		"access-platform-cancel-submitter": &platformUserID,
		"access-platform-cancel-other":     &unrelatedPlatformUserID,
	} {
		require.NoError(t, fixture.service.db.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash','platform_admin','platform_admin','active') RETURNING id`,
			platformOrgID, username).Scan(target))
	}
	*fixture.actor = Actor{
		UserID: platformUserID, OrgID: platformOrgID,
		OrganizationRole: auth.RolePlatformAdmin,
	}
	identityScope := ActorScope{
		UserID: platformUserID, OrgID: platformOrgID,
		OrganizationRole: auth.RolePlatformAdmin, EnforceOrgBoundary: true,
	}
	platformScope := ActorScope{
		UserID: platformUserID, OrgID: fixture.orgID,
		OrganizationRole: auth.RolePlatformAdmin, EnforceOrgBoundary: true,
	}
	pending, err := fixture.service.Provision(ctx, fixture.personalInput(
		"platform-cross-org-migration", SubmissionMigration,
	))
	require.NoError(t, err)
	require.Equal(t, RequestNeedsReview, pending.Request.Status)
	require.Nil(t, pending.Grant)
	secondPending, err := fixture.service.Provision(ctx, fixture.personalInput(
		"platform-cross-org-migration-second", SubmissionMigration,
	))
	require.NoError(t, err)
	require.Equal(t, RequestNeedsReview, secondPending.Request.Status)

	identityPage, err := fixture.service.ListMyRequests(ctx, identityScope, PageInput{})
	require.NoError(t, err)
	require.Empty(t, identityPage.Data)
	_, _, err = fixture.service.GetRequest(ctx, identityScope, pending.Request.RequestRef)
	require.ErrorIs(t, err, ErrNotFound)

	firstPage, err := fixture.service.ListMyRequests(ctx, platformScope, PageInput{Limit: 1})
	require.NoError(t, err)
	require.Len(t, firstPage.Data, 1)
	require.Equal(t, secondPending.Request.ID, firstPage.Data[0].ID)
	require.True(t, firstPage.Pagination.HasMore)
	require.NotNil(t, firstPage.Pagination.NextAfterID)
	secondPage, err := fixture.service.ListMyRequests(ctx, platformScope, PageInput{
		Limit: 1, AfterID: *firstPage.Pagination.NextAfterID,
	})
	require.NoError(t, err)
	require.Len(t, secondPage.Data, 1)
	require.Equal(t, pending.Request.ID, secondPage.Data[0].ID)
	require.False(t, secondPage.Pagination.HasMore)

	unrelatedPage, err := fixture.service.ListMyRequests(ctx, ActorScope{
		UserID: unrelatedPlatformUserID, OrgID: fixture.orgID, OrganizationRole: auth.RolePlatformAdmin, EnforceOrgBoundary: true,
	}, PageInput{})
	require.NoError(t, err)
	require.Empty(t, unrelatedPage.Data)
	ordinaryPage, err := fixture.service.ListMyRequests(ctx, ActorScope{
		UserID: fixture.otherProjectUserID, OrgID: fixture.orgID, OrganizationRole: auth.RoleMember,
	}, PageInput{})
	require.NoError(t, err)
	require.Empty(t, ordinaryPage.Data)

	*fixture.actor = Actor{
		UserID: unrelatedPlatformUserID, OrgID: platformOrgID,
		OrganizationRole: auth.RolePlatformAdmin,
	}
	_, err = fixture.service.CancelRequest(ctx, ActorScope{
		UserID: unrelatedPlatformUserID, OrgID: fixture.orgID,
		OrganizationRole: auth.RolePlatformAdmin, EnforceOrgBoundary: true,
	}, CancelRequestInput{
		RequestRef: pending.Request.RequestRef, ExpectedRevision: pending.Request.Revision,
		ClientRequestID: "unrelated-platform-cancel",
	})
	require.ErrorIs(t, err, ErrNotFound)

	*fixture.actor = Actor{
		UserID: platformUserID, OrgID: platformOrgID,
		OrganizationRole: auth.RolePlatformAdmin,
	}
	cancelled, err := fixture.service.CancelRequest(ctx, platformScope, CancelRequestInput{
		RequestRef: pending.Request.RequestRef, ExpectedRevision: pending.Request.Revision,
		ClientRequestID: "platform-cross-org-cancel",
	})
	require.NoError(t, err)
	require.Equal(t, RequestCancelled, cancelled.Request.Status)
	require.Equal(t, platformUserID, cancelled.Request.CancelledBy)
	require.Equal(t, []string{ReasonRequesterCancelled}, cancelled.Request.DecisionReasonCodes)

	var storedCanceller int64
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		SELECT cancelled_by FROM access_requests WHERE id=$1`, pending.Request.ID).Scan(&storedCanceller))
	require.Equal(t, platformUserID, storedCanceller)
}

func TestGrantEnvelopeExpansionRequiresReviewAndNarrowingRemainsAutomaticIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	_, err := fixture.service.db.Exec(ctx, `
		INSERT INTO model_registry (org_id,model_name,display_name,lifecycle,approved_catalog)
		VALUES ($1,'model-a','Model A','ga',TRUE),($1,'model-b','Model B','ga',TRUE)`, fixture.orgID)
	require.NoError(t, err)

	initial := fixture.personalInput("envelope-baseline", SubmissionDirectGrant)
	initial.Envelope.ScopeMode = "explicit"
	initial.Envelope.Models = []string{"model-a", "model-b"}
	initial.Envelope.Endpoints = []string{"/v1/chat/completions", "/v1/embeddings"}
	fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	baseline, err := fixture.service.Provision(ctx, initial)
	require.NoError(t, err)
	require.NotNil(t, baseline.Grant)
	require.EqualValues(t, 1, baseline.Grant.Revision)

	selfScope := fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	_ = selfScope
	request := func(clientID string) ProvisionInput {
		input := fixture.personalInput(clientID, SubmissionSelf)
		input.Envelope.ScopeMode = "explicit"
		input.Envelope.Models = []string{"model-a", "model-b"}
		input.Envelope.Endpoints = []string{"/v1/chat/completions", "/v1/embeddings"}
		return input
	}
	assertPending := func(input ProvisionInput, reason string) Request {
		t.Helper()
		result, provisionErr := fixture.service.Provision(ctx, input)
		require.NoError(t, provisionErr)
		require.Nil(t, result.Grant)
		require.Equal(t, RequestNeedsReview, result.Request.Status)
		require.Contains(t, result.Request.PolicyReasonCodes, reason)
		return result.Request
	}

	toDefault := request("envelope-explicit-to-default")
	toDefault.Envelope.ScopeMode = "project_default"
	toDefault.Envelope.Models = nil
	toDefault.Envelope.Endpoints = nil
	assertPending(toDefault, ReasonScopeExpansion)

	addModel := request("envelope-add-model")
	addModel.Envelope.Models = append(addModel.Envelope.Models, "model-c")
	assertPending(addModel, ReasonScopeExpansion)

	addEndpoint := request("envelope-add-endpoint")
	// Start from a deliberately narrower baseline view so the additional
	// supported endpoint is baseline-relative expansion, not a new vocabulary.
	addEndpoint.Envelope.Endpoints = []string{"/v1/chat/completions", "/v1/embeddings", "/v1/models"}
	assertPending(addEndpoint, ReasonScopeExpansion)

	narrow := request("envelope-narrow")
	narrow.Envelope.Models = []string{"model-a"}
	narrow.Envelope.Endpoints = []string{"/v1/chat/completions"}
	narrowed, err := fixture.service.Provision(ctx, narrow)
	require.NoError(t, err)
	require.NotNil(t, narrowed.Grant)
	require.Equal(t, RequestApproved, narrowed.Request.Status)
	require.Equal(t, DecisionAutoPolicy, narrowed.Request.DecisionMode)
	require.EqualValues(t, 2, narrowed.Grant.Revision)
	require.Equal(t, []string{"model-a"}, narrowed.Grant.Envelope.Models)
	require.Equal(t, []string{"/v1/chat/completions"}, narrowed.Grant.Envelope.Endpoints)

	fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	directExpansion := fixture.personalInput("envelope-direct-expansion", SubmissionDirectGrant)
	direct, err := fixture.service.Provision(ctx, directExpansion)
	require.NoError(t, err)
	require.NotNil(t, direct.Grant)
	require.Equal(t, RequestApproved, direct.Request.Status)
	require.Equal(t, DecisionDirect, direct.Request.DecisionMode)
	require.Contains(t, direct.Request.PolicyReasonCodes, ReasonScopeExpansion)
	require.Equal(t, []string{ReasonDirectGrantAuthorizedNonProduction}, direct.Request.DecisionReasonCodes)
	require.EqualValues(t, 3, direct.Grant.Revision)
}

func TestMutableCatalogClassificationSerializesProvisionAndDecisionAuthorityIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := fixture.service.db.Exec(ctx, `
		INSERT INTO model_registry (org_id,model_name,display_name,lifecycle,approved_catalog)
		VALUES ($1,'catalog-race-model','Catalog Race Model','ga',TRUE)`, fixture.orgID)
	require.NoError(t, err)

	makeInput := func(clientID string, userID int64) ProvisionInput {
		return ProvisionInput{
			ClientRequestID: clientID, RequestID: "req-" + clientID,
			OrgID: fixture.orgID, ProjectID: fixture.projectID,
			Subject:     Subject{Type: SubjectUser, UserID: userID},
			RequestKind: RequestKindHumanPersonal,
			Envelope: Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: "explicit",
				Models: []string{"catalog-race-model"}, Endpoints: []string{"/v1/chat/completions"}},
			Environment: PersonalEnvironment, SubmissionMode: SubmissionSelf,
		}
	}

	t.Run("catalog de-list committed first prevents automatic approval", func(t *testing.T) {
		fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
		delist, beginErr := fixture.service.db.Begin(ctx)
		require.NoError(t, beginErr)
		defer delist.Rollback(ctx) //nolint:errcheck
		_, err = delist.Exec(ctx, `UPDATE model_registry SET approved_catalog=FALSE WHERE org_id=$1 AND model_name='catalog-race-model'`, fixture.orgID)
		require.NoError(t, err)

		type provisionOutcome struct {
			result ProvisionResult
			err    error
		}
		outcome := make(chan provisionOutcome, 1)
		go func() {
			result, provisionErr := fixture.service.Provision(ctx, makeInput("catalog-race-auto", fixture.subjectID))
			outcome <- provisionOutcome{result: result, err: provisionErr}
		}()
		select {
		case result := <-outcome:
			t.Fatalf("provision escaped uncommitted catalog transition: result=%#v err=%v", result.result, result.err)
		case <-time.After(150 * time.Millisecond):
		}
		require.NoError(t, delist.Commit(ctx))
		result := <-outcome
		require.NoError(t, result.err)
		require.Nil(t, result.result.Grant)
		require.Equal(t, RequestNeedsReview, result.result.Request.Status)
		require.Contains(t, result.result.Request.PolicyReasonCodes, ReasonSpecialModel)
	})

	t.Run("stale request cannot retain weaker project-admin authority", func(t *testing.T) {
		_, err = fixture.service.db.Exec(ctx, `UPDATE model_registry SET approved_catalog=TRUE WHERE org_id=$1 AND model_name='catalog-race-model'`, fixture.orgID)
		require.NoError(t, err)
		var applicantID, projectAdminID int64
		require.NoError(t, fixture.service.db.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,'catalog-race-applicant','hash','member','member','active') RETURNING id`, fixture.orgID).Scan(&applicantID))
		require.NoError(t, fixture.service.db.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,'catalog-race-project-admin','hash','member','project_admin','active') RETURNING id`, fixture.orgID).Scan(&projectAdminID))
		_, err = fixture.service.db.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_admin')`, fixture.projectID, projectAdminID)
		require.NoError(t, err)

		fixture.useActor(applicantID, auth.RoleMember, "")
		stale, err := fixture.service.Provision(ctx, makeInput("catalog-stale-request", applicantID))
		require.NoError(t, err)
		require.Equal(t, RequestNeedsReview, stale.Request.Status)
		require.Contains(t, stale.Request.PolicyReasonCodes, ReasonFirstCrossProject)
		require.NotContains(t, stale.Request.PolicyReasonCodes, ReasonSpecialModel)
		_, err = fixture.service.db.Exec(ctx, `UPDATE model_registry SET approved_catalog=FALSE WHERE org_id=$1 AND model_name='catalog-race-model'`, fixture.orgID)
		require.NoError(t, err)

		projectAdminScope := fixture.useActor(projectAdminID, auth.RoleMember, auth.ProjectRoleAdmin)
		_, err = fixture.service.Decide(ctx, projectAdminScope, DecisionInput{
			RequestRef: stale.Request.RequestRef, ExpectedRevision: stale.Request.Revision,
			Decision: "approve", ReasonCodes: []string{ReasonManualApproved},
			RequestID: "req-catalog-stale-approve", ClientRequestID: "catalog-stale-approve",
		})
		require.ErrorIs(t, err, ErrConflict)
		stored, _, err := fixture.service.GetRequest(ctx, ActorScope{
			UserID: applicantID, OrgID: fixture.orgID, OrganizationRole: auth.RoleMember,
		}, stale.Request.RequestRef)
		require.NoError(t, err)
		require.Equal(t, RequestNeedsReview, stored.Status)

		fixture.useActor(applicantID, auth.RoleMember, "")
		fresh, err := fixture.service.Provision(ctx, makeInput("catalog-fresh-request", applicantID))
		require.NoError(t, err)
		require.Contains(t, fresh.Request.PolicyReasonCodes, ReasonSpecialModel)
		projectAdminScope = fixture.useActor(projectAdminID, auth.RoleMember, auth.ProjectRoleAdmin)
		_, err = fixture.service.Decide(ctx, projectAdminScope, DecisionInput{
			RequestRef: fresh.Request.RequestRef, ExpectedRevision: fresh.Request.Revision,
			Decision: "approve", ReasonCodes: []string{ReasonManualApproved},
			RequestID: "req-catalog-fresh-project-approve", ClientRequestID: "catalog-fresh-project-approve",
		})
		require.ErrorIs(t, err, ErrForbidden)

		orgAdminScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
		approved, err := fixture.service.Decide(ctx, orgAdminScope, DecisionInput{
			RequestRef: fresh.Request.RequestRef, ExpectedRevision: fresh.Request.Revision,
			Decision: "approve", ReasonCodes: []string{ReasonManualApproved},
			RequestID: "req-catalog-fresh-org-approve", ClientRequestID: "catalog-fresh-org-approve",
		})
		require.NoError(t, err)
		require.NotNil(t, approved.Grant)
		require.Contains(t, approved.Request.PolicyReasonCodes, ReasonSpecialModel)
	})
}

func TestRejectClosesStaleNeedsReviewRequestsWhileApproveFailsClosedIntegration(t *testing.T) {
	tests := []struct {
		name       string
		workload   bool
		invalidate func(context.Context, *accessServiceFixture, time.Time)
	}{
		{
			name: "disabled human subject",
			invalidate: func(ctx context.Context, fixture *accessServiceFixture, _ time.Time) {
				_, err := fixture.service.db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, fixture.subjectID)
				require.NoError(t, err)
			},
		},
		{
			name:     "archived workload subject",
			workload: true,
			invalidate: func(ctx context.Context, fixture *accessServiceFixture, now time.Time) {
				_, err := fixture.service.db.Exec(ctx, `UPDATE workload_applications SET status='archived',archived_at=$2 WHERE id=$1`, fixture.workloadID, now)
				require.NoError(t, err)
			},
		},
		{
			name:     "inactive workload sponsor",
			workload: true,
			invalidate: func(ctx context.Context, fixture *accessServiceFixture, _ time.Time) {
				_, err := fixture.service.db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, fixture.sponsorID)
				require.NoError(t, err)
			},
		},
		{
			name: "inactive project",
			invalidate: func(ctx context.Context, fixture *accessServiceFixture, _ time.Time) {
				_, err := fixture.service.db.Exec(ctx, `UPDATE projects SET status='disabled' WHERE id=$1`, fixture.projectID)
				require.NoError(t, err)
			},
		},
		{
			name: "expired requested validity",
			invalidate: func(_ context.Context, fixture *accessServiceFixture, now time.Time) {
				fixture.service.now = func() time.Time { return now.Add(2 * time.Hour) }
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAccessServiceFixture(t)
			ctx := context.Background()
			fixedNow := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
			fixture.service.now = func() time.Time { return fixedNow }
			rpm := 61
			var input ProvisionInput
			if test.workload {
				input = ProvisionInput{
					ClientRequestID: "stale-workload-" + strings.ReplaceAll(test.name, " ", "-"),
					RequestID:       "req-stale-workload-" + strings.ReplaceAll(test.name, " ", "-"),
					OrgID:           fixture.orgID,
					ProjectID:       fixture.projectID,
					Subject: Subject{
						Type: SubjectWorkloadApplication, WorkloadApplicationID: fixture.workloadID,
					},
					RequestKind: RequestKindWorkloadService,
					Envelope:    Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: "project_default", RPMLimit: &rpm},
					Environment: "development", SubmissionMode: SubmissionDelegated,
					SponsorUserID: fixture.sponsorID,
				}
				fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
			} else {
				input = fixture.personalInput("stale-human-"+strings.ReplaceAll(test.name, " ", "-"), SubmissionSelf)
				input.Envelope.RPMLimit = &rpm
				fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
			}
			if test.name == "expired requested validity" {
				expires := fixedNow.Add(time.Hour)
				input.ValidUntil = &expires
			}
			pending, err := fixture.service.Provision(ctx, input)
			require.NoError(t, err)
			require.Equal(t, RequestNeedsReview, pending.Request.Status)
			require.Nil(t, pending.Grant)

			var membershipCountBefore int
			require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE project_id=$1`, fixture.projectID).Scan(&membershipCountBefore))
			test.invalidate(ctx, &fixture, fixedNow)
			deciderScope := fixture.useActor(fixture.secondOrgAdminID, auth.RoleOrgAdmin, "")
			_, err = fixture.service.Decide(ctx, deciderScope, DecisionInput{
				RequestRef: pending.Request.RequestRef, ExpectedRevision: pending.Request.Revision,
				Decision: "approve", ReasonCodes: []string{ReasonManualApproved},
				ClientRequestID: "approve-stale-" + strings.ReplaceAll(test.name, " ", "-"),
				RequestID:       "req-approve-stale-" + strings.ReplaceAll(test.name, " ", "-"),
			})
			require.ErrorIs(t, err, ErrConflict)

			rejected, err := fixture.service.Decide(ctx, deciderScope, DecisionInput{
				RequestRef: pending.Request.RequestRef, ExpectedRevision: pending.Request.Revision,
				Decision: "reject", ReasonCodes: []string{ReasonManualRejected},
				ClientRequestID: "reject-stale-" + strings.ReplaceAll(test.name, " ", "-"),
				RequestID:       "req-reject-stale-" + strings.ReplaceAll(test.name, " ", "-"),
			})
			require.NoError(t, err)
			require.Equal(t, RequestRejected, rejected.Request.Status)
			require.Nil(t, rejected.Grant)

			var grantCount, approvalOperationCount, membershipCountAfter int
			require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT COUNT(*) FROM access_grants WHERE access_request_id=$1`, pending.Request.ID).Scan(&grantCount))
			require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT COUNT(*) FROM access_operations WHERE client_request_id=$1`, "approve-stale-"+strings.ReplaceAll(test.name, " ", "-")).Scan(&approvalOperationCount))
			require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE project_id=$1`, fixture.projectID).Scan(&membershipCountAfter))
			require.Zero(t, grantCount)
			require.Zero(t, approvalOperationCount)
			require.Equal(t, membershipCountBefore, membershipCountAfter)
		})
	}
}

func TestGrantValidityExtensionNeedsReviewRejectsExpiredResumeAndReplaysOriginalRevisionIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	serviceNow := time.Now().UTC().Truncate(time.Microsecond)
	fixture.service.now = func() time.Time { return serviceNow }
	userScope := fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	firstExpiry := serviceNow.Add(time.Hour)
	initialInput := fixture.personalInput("validity-initial", SubmissionSelf)
	initialInput.ValidUntil = &firstExpiry
	initial, err := fixture.service.Provision(ctx, initialInput)
	require.NoError(t, err)
	require.NotNil(t, initial.Grant)
	require.Equal(t, &firstExpiry, initial.Grant.ValidUntil)

	laterExpiry := serviceNow.Add(2 * time.Hour)
	extensionInput := fixture.personalInput("validity-extension", SubmissionSelf)
	extensionInput.ValidUntil = &laterExpiry
	pending, err := fixture.service.Provision(ctx, extensionInput)
	require.NoError(t, err)
	require.Nil(t, pending.Grant)
	require.Equal(t, RequestNeedsReview, pending.Request.Status)
	require.Equal(t, []string{ReasonValidityExtension}, pending.Request.PolicyReasonCodes)

	orgAdminScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	approved, err := fixture.service.Decide(ctx, orgAdminScope, DecisionInput{
		RequestRef: pending.Request.RequestRef, ExpectedRevision: pending.Request.Revision,
		Decision: "approve", ReasonCodes: []string{ReasonManualApproved},
		RequestID: "req-validity-extension-approve", ClientRequestID: "validity-extension-approve",
	})
	require.NoError(t, err)
	require.NotNil(t, approved.Grant)
	require.EqualValues(t, 2, approved.Grant.Revision)
	require.NotNil(t, approved.Grant.ValidUntil)
	require.True(t, approved.Grant.ValidUntil.Equal(laterExpiry))

	fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	replay, err := fixture.service.Provision(ctx, initialInput)
	require.NoError(t, err)
	require.True(t, replay.Idempotent)
	require.NotNil(t, replay.Grant)
	require.EqualValues(t, 1, replay.Grant.Revision)
	require.NotNil(t, replay.Grant.ValidUntil)
	require.True(t, replay.Grant.ValidUntil.Equal(firstExpiry))

	credential, err := fixture.service.IssueCredential(ctx, userScope, CredentialInput{
		GrantRef: approved.Grant.GrantRef, Name: "validity credential",
		ClientRequestID: "validity-credential", RequestID: "req-validity-credential",
	})
	require.NoError(t, err)
	_, err = accessgrantboundary.Resolve(ctx, fixture.service.db, credential.ID)
	require.NoError(t, err)

	fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	suspended, err := fixture.service.ChangeGrantLifecycle(ctx, orgAdminScope, GrantLifecycleInput{
		GrantRef: approved.Grant.GrantRef, ExpectedRevision: approved.Grant.Revision,
		Action: "suspend", Reason: "pause temporary grant", ClientRequestID: "expired-grant-suspend",
		RequestID: "req-expired-grant-suspend",
	})
	require.NoError(t, err)
	fixture.service.now = func() time.Time { return laterExpiry.Add(time.Minute) }
	_, err = fixture.service.ChangeGrantLifecycle(ctx, orgAdminScope, GrantLifecycleInput{
		GrantRef: approved.Grant.GrantRef, ExpectedRevision: suspended.Revision,
		Action: "resume", Reason: "must not resume expired grant", ClientRequestID: "expired-grant-resume",
		RequestID: "req-expired-grant-resume",
	})
	require.ErrorIs(t, err, ErrConflict)

	past := serviceNow.Add(-time.Minute)
	pastInput := fixture.personalInput("validity-past", SubmissionSelf)
	pastInput.ValidUntil = &past
	_, err = fixture.service.Provision(ctx, pastInput)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestWorkloadSponsorRetainsReadVisibilityAcrossGrantRevisionIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	workloadInput := func(clientRequestID string, rpm *int) ProvisionInput {
		return ProvisionInput{
			ClientRequestID: clientRequestID, RequestID: "req-" + clientRequestID,
			OrgID: fixture.orgID, ProjectID: fixture.projectID,
			Subject:     Subject{Type: SubjectWorkloadApplication, WorkloadApplicationID: fixture.workloadID},
			RequestKind: RequestKindWorkloadService,
			Envelope:    Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: "project_default", RPMLimit: rpm},
			Environment: "development", SubmissionMode: SubmissionDirectGrant, SponsorUserID: fixture.sponsorID,
		}
	}
	fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	revisionOne, err := fixture.service.Provision(ctx, workloadInput("workload-grant-revision-one", nil))
	require.NoError(t, err)
	require.NotNil(t, revisionOne.Grant)

	rpm := 30
	fixture.useActor(fixture.secondOrgAdminID, auth.RoleOrgAdmin, "")
	revisionTwo, err := fixture.service.Provision(ctx, workloadInput("workload-grant-revision-two", &rpm))
	require.NoError(t, err)
	require.NotNil(t, revisionTwo.Grant)
	require.EqualValues(t, 2, revisionTwo.Grant.Revision)
	require.Equal(t, fixture.sponsorID, revisionTwo.Grant.SponsorUserID)

	sponsorScope := fixture.useActor(fixture.sponsorID, auth.RoleMember, auth.ProjectRoleUser)
	visible, err := fixture.service.GetGrant(ctx, sponsorScope, revisionTwo.Grant.GrantRef)
	require.NoError(t, err)
	require.Equal(t, revisionTwo.Grant.ID, visible.ID)

	otherScope := fixture.useActor(fixture.otherProjectUserID, auth.RoleMember, auth.ProjectRoleUser)
	_, err = fixture.service.GetGrant(ctx, otherScope, revisionTwo.Grant.GrantRef)
	require.True(t, errors.Is(err, ErrNotFound))
}

func TestGatewayGrantBoundaryRequiresLiveWorkloadSponsorAndProductionClassificationIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	adminScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	provisioned, err := fixture.service.Provision(ctx, ProvisionInput{
		ClientRequestID: "gateway-live-sponsor-grant", RequestID: "req-gateway-live-sponsor-grant",
		OrgID: fixture.orgID, ProjectID: fixture.projectID,
		Subject:     Subject{Type: SubjectWorkloadApplication, WorkloadApplicationID: fixture.workloadID},
		RequestKind: RequestKindWorkloadService,
		Envelope:    Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: "project_default"},
		Environment: "development", SubmissionMode: SubmissionDirectGrant, SponsorUserID: fixture.sponsorID,
	})
	require.NoError(t, err)
	require.NotNil(t, provisioned.Grant)
	credential, err := fixture.service.IssueCredential(ctx, adminScope, CredentialInput{
		GrantRef: provisioned.Grant.GrantRef, Name: "gateway-live-sponsor-key",
		ClientRequestID: "gateway-live-sponsor-key", RequestID: "req-gateway-live-sponsor-key",
	})
	require.NoError(t, err)
	require.True(t, credential.SecretAvailable)

	legacyRaw, legacyPrefix, err := auth.GenerateAPIKey(fixture.orgID)
	require.NoError(t, err)
	_, err = fixture.service.db.Exec(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,workload_application_id,key_type,name,key_hash,key_prefix,status,
		  rpm_limit,qps_limit,allowed_ips,allowed_models
		) VALUES ($1,$2,$3,'service','legacy-workload-boundary',$4,$5,'active',60,10,'','')`,
		fixture.orgID, fixture.projectID, fixture.workloadID,
		auth.HashAPIKey(legacyRaw, "access-service-integration-key-hash-secret"), legacyPrefix)
	require.NoError(t, err)

	handler := middleware.GatewayAuth(fixture.service.db, "access-service-integration-key-hash-secret")(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	serve := func(secret string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Code
	}
	require.Equal(t, http.StatusOK, serve(credential.Key))
	require.Equal(t, http.StatusOK, serve(legacyRaw))

	_, err = fixture.service.db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, fixture.sponsorID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve(credential.Key))
	require.Equal(t, http.StatusOK, serve(legacyRaw), "legacy-unbound workload keys do not acquire Grant sponsor semantics")

	_, err = fixture.service.db.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, fixture.sponsorID)
	require.NoError(t, err)
	_, err = fixture.service.db.Exec(ctx, `UPDATE project_members SET role='project_viewer' WHERE project_id=$1 AND user_id=$2`, fixture.projectID, fixture.sponsorID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve(credential.Key))

	for _, role := range []string{auth.ProjectRoleUser, auth.ProjectRoleAdmin} {
		_, err = fixture.service.db.Exec(ctx, `UPDATE project_members SET role=$3 WHERE project_id=$1 AND user_id=$2`, fixture.projectID, fixture.sponsorID, role)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, serve(credential.Key), "live %s sponsor must remain valid", role)
	}

	_, err = fixture.service.db.Exec(ctx, `UPDATE users SET organization_role='org_admin',role='org_admin' WHERE id=$1`, fixture.sponsorID)
	require.NoError(t, err)
	_, err = fixture.service.db.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, fixture.projectID, fixture.sponsorID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve(credential.Key), "same-organization org_admin is a valid sponsor")

	var otherProjectID int64
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'Sponsor other project','sponsor-other-project','active') RETURNING id`, fixture.orgID).Scan(&otherProjectID))
	_, err = fixture.service.db.Exec(ctx, `UPDATE users SET organization_role='member',role='member' WHERE id=$1`, fixture.sponsorID)
	require.NoError(t, err)
	_, err = fixture.service.db.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, otherProjectID, fixture.sponsorID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, serve(credential.Key), "calling membership in another project is not sponsor authority")
	_, err = fixture.service.db.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, fixture.projectID, fixture.sponsorID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve(credential.Key))

	_, err = fixture.service.db.Exec(ctx, `ALTER TABLE workload_applications DISABLE TRIGGER workload_applications_identity_tuple_immutable`)
	require.NoError(t, err)
	_, updateErr := fixture.service.db.Exec(ctx, `UPDATE workload_applications SET production_use=TRUE WHERE id=$1`, fixture.workloadID)
	_, enableErr := fixture.service.db.Exec(ctx, `ALTER TABLE workload_applications ENABLE TRIGGER workload_applications_identity_tuple_immutable`)
	require.NoError(t, updateErr)
	require.NoError(t, enableErr)
	require.Equal(t, http.StatusUnauthorized, serve(credential.Key), "workload/grant production classification mismatch must fail closed")
	require.Equal(t, http.StatusOK, serve(legacyRaw), "legacy-unbound runtime remains outside Grant classification checks")
}

func TestWorkloadProductionClassificationAndCallingRoleAreServerBoundIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	var productionWorkloadID int64
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		INSERT INTO workload_applications (
		  contract_version,identity_contract_version,org_id,project_id,application_ref,
		  logical_ref,display_name,kind,environment,production_use,identity_mode,owner_team,status,
		  environment_locked_at,created_by,updated_by
		) VALUES (
		  'unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,
		  'app_access_prod_cn','workload_access_prod_cn','Production workload',
		  'agent','prod-cn',TRUE,'service_credential','platform','active',NOW(),$3,$3
		) RETURNING id`, fixture.orgID, fixture.projectID, fixture.orgAdminID).Scan(&productionWorkloadID))

	input := ProvisionInput{
		ClientRequestID: "workload-production-bound", RequestID: "req-workload-production-bound",
		OrgID: fixture.orgID, ProjectID: fixture.projectID,
		Subject:     Subject{Type: SubjectWorkloadApplication, WorkloadApplicationID: productionWorkloadID},
		RequestKind: RequestKindWorkloadService,
		Envelope:    Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: "project_default"},
		Environment: "prod-cn", SubmissionMode: SubmissionDirectGrant, SponsorUserID: fixture.sponsorID,
	}
	fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	before := accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID)
	_, err := fixture.service.Provision(ctx, input)
	require.ErrorIs(t, err, ErrConflict, "client cannot relabel a production workload as non-production")
	require.Equal(t, before, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))

	input.ProductionUse = true
	pending, err := fixture.service.Provision(ctx, input)
	require.NoError(t, err)
	require.Nil(t, pending.Grant)
	require.Equal(t, RequestNeedsReview, pending.Request.Status)
	require.Contains(t, pending.Request.PolicyReasonCodes, ReasonProductionWorkload)

	invalidRole := input
	invalidRole.ClientRequestID = "workload-production-admin-role"
	invalidRole.Envelope.ProjectRole = auth.ProjectRoleAdmin
	_, err = fixture.service.Provision(ctx, invalidRole)
	require.ErrorIs(t, err, ErrInvalid)

	deciderScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	approved, err := fixture.service.Decide(ctx, deciderScope, DecisionInput{
		RequestRef: pending.Request.RequestRef, ExpectedRevision: pending.Request.Revision,
		Decision: "approve", ReasonCodes: []string{ReasonManualApproved},
		ClientRequestID: "workload-production-approve", RequestID: "req-workload-production-approve",
	})
	require.NoError(t, err)
	require.NotNil(t, approved.Grant)
	require.Equal(t, pending.Request.SubmittedBy, approved.Request.DecidedBy)
	require.True(t, approved.Grant.ProductionUse)
	require.Equal(t, "prod-cn", approved.Grant.Environment)
	require.Equal(t, auth.ProjectRoleUser, approved.Grant.Envelope.ProjectRole)

	var storedProductionUse bool
	var storedRole string
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		SELECT production_use,project_role_snapshot FROM access_grants WHERE id=$1`,
		approved.Grant.ID).Scan(&storedProductionUse, &storedRole))
	require.True(t, storedProductionUse)
	require.Equal(t, auth.ProjectRoleUser, storedRole)
}

func TestGrantCredentialAndResumeRequireLiveSubjectIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	fixedNow := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return fixedNow }

	fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	humanResult, err := fixture.service.Provision(ctx, fixture.personalInput("live-human-grant", SubmissionSelf))
	require.NoError(t, err)
	require.NotNil(t, humanResult.Grant)
	adminScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	adminBeforeSecretProbe := accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID)
	_, err = fixture.service.IssueCredential(ctx, adminScope, CredentialInput{
		GrantRef: humanResult.Grant.GrantRef, ClientRequestID: "admin-must-not-issue-human-key",
		RequestID: "req-admin-must-not-issue-human-key", Name: "admin-must-not-issue-human-key",
	})
	require.ErrorIs(t, err, ErrForbidden)
	require.Equal(t, adminBeforeSecretProbe, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))

	humanScope := fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	humanCredential, err := fixture.service.IssueCredential(ctx, humanScope, CredentialInput{
		GrantRef: humanResult.Grant.GrantRef, ClientRequestID: "live-human-key",
		RequestID: "req-live-human-key", Name: "live-human-key",
	})
	require.NoError(t, err)
	require.NotZero(t, humanCredential.ID)
	require.Equal(t, fixture.subjectID, fixture.actor.UserID)

	adminScope = fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	adminBeforeRotateProbe := accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID)
	_, err = fixture.service.RotateCredential(ctx, adminScope, RotateCredentialInput{
		GrantRef: humanResult.Grant.GrantRef, ClientRequestID: "admin-must-not-rotate-human-key",
		RequestID:        "req-admin-must-not-rotate-human-key",
		PredecessorKeyID: humanCredential.ID,
	})
	require.ErrorIs(t, err, ErrForbidden)
	require.Equal(t, adminBeforeRotateProbe, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))

	suspended, err := fixture.service.ChangeGrantLifecycle(ctx, adminScope, GrantLifecycleInput{
		GrantRef: humanResult.Grant.GrantRef, ExpectedRevision: humanResult.Grant.Revision,
		Action: "suspend", Reason: "subject review", ClientRequestID: "live-human-suspend", RequestID: "req-live-human-suspend",
	})
	require.NoError(t, err)
	require.Equal(t, GrantSuspended, suspended.Status)
	_, err = fixture.service.db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, fixture.subjectID)
	require.NoError(t, err)
	before := accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID)
	_, err = fixture.service.ChangeGrantLifecycle(ctx, adminScope, GrantLifecycleInput{
		GrantRef: humanResult.Grant.GrantRef, ExpectedRevision: suspended.Revision,
		Action: "resume", Reason: "invalid resume", ClientRequestID: "live-human-resume-disabled", RequestID: "req-live-human-resume-disabled",
	})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, before, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))

	_, err = fixture.service.IssueCredential(ctx, adminScope, CredentialInput{
		GrantRef: humanResult.Grant.GrantRef, ClientRequestID: "live-human-issue-disabled",
		RequestID: "req-live-human-issue-disabled", Name: "must-not-exist",
	})
	require.ErrorIs(t, err, ErrForbidden)
	require.Equal(t, before, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))
	_, err = fixture.service.RotateCredential(ctx, adminScope, RotateCredentialInput{
		GrantRef: humanResult.Grant.GrantRef, ClientRequestID: "live-human-rotate-disabled",
		RequestID:        "req-live-human-rotate-disabled",
		PredecessorKeyID: humanCredential.ID,
	})
	require.ErrorIs(t, err, ErrForbidden)
	require.Equal(t, before, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))

	_, err = fixture.service.db.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, fixture.subjectID)
	require.NoError(t, err)
	resumed, err := fixture.service.ChangeGrantLifecycle(ctx, adminScope, GrantLifecycleInput{
		GrantRef: humanResult.Grant.GrantRef, ExpectedRevision: suspended.Revision,
		Action: "resume", Reason: "subject restored", ClientRequestID: "live-human-resume-valid", RequestID: "req-live-human-resume-valid",
	})
	require.NoError(t, err)
	require.Equal(t, GrantActive, resumed.Status)
	_, err = fixture.service.db.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, fixture.projectID, fixture.subjectID)
	require.NoError(t, err)
	noMembershipBefore := accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID)
	_, err = fixture.service.IssueCredential(ctx, adminScope, CredentialInput{
		GrantRef: humanResult.Grant.GrantRef, ClientRequestID: "live-human-issue-no-membership",
		RequestID: "req-live-human-issue-no-membership", Name: "must-not-exist-no-membership",
	})
	require.ErrorIs(t, err, ErrForbidden)
	_, err = fixture.service.RotateCredential(ctx, adminScope, RotateCredentialInput{
		GrantRef: humanResult.Grant.GrantRef, ClientRequestID: "live-human-rotate-no-membership",
		RequestID:        "req-live-human-rotate-no-membership",
		PredecessorKeyID: humanCredential.ID,
	})
	require.ErrorIs(t, err, ErrForbidden)
	require.Equal(t, noMembershipBefore, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))

	workloadInput := ProvisionInput{
		ClientRequestID: "live-workload-grant", RequestID: "req-live-workload-grant",
		OrgID: fixture.orgID, ProjectID: fixture.projectID,
		Subject:     Subject{Type: SubjectWorkloadApplication, WorkloadApplicationID: fixture.workloadID},
		RequestKind: RequestKindWorkloadService,
		Envelope:    Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: "project_default"},
		Environment: "development", SubmissionMode: SubmissionDirectGrant, SponsorUserID: fixture.sponsorID,
	}
	workloadResult, err := fixture.service.Provision(ctx, workloadInput)
	require.NoError(t, err)
	require.NotNil(t, workloadResult.Grant)
	workloadCredential, err := fixture.service.IssueCredential(ctx, adminScope, CredentialInput{
		GrantRef: workloadResult.Grant.GrantRef, ClientRequestID: "live-workload-key",
		RequestID: "req-live-workload-key", Name: "live-workload-key",
	})
	require.NoError(t, err)
	_, err = fixture.service.db.Exec(ctx, `UPDATE workload_applications SET status='disabled' WHERE id=$1`, fixture.workloadID)
	require.NoError(t, err)
	workloadBefore := accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID)
	_, err = fixture.service.IssueCredential(ctx, adminScope, CredentialInput{
		GrantRef: workloadResult.Grant.GrantRef, ClientRequestID: "live-workload-issue-disabled",
		RequestID: "req-live-workload-issue-disabled", Name: "must-not-exist",
	})
	require.ErrorIs(t, err, ErrConflict)
	_, err = fixture.service.RotateCredential(ctx, adminScope, RotateCredentialInput{
		GrantRef: workloadResult.Grant.GrantRef, ClientRequestID: "live-workload-rotate-disabled",
		RequestID:        "req-live-workload-rotate-disabled",
		PredecessorKeyID: workloadCredential.ID,
	})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, workloadBefore, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))
	workloadSuspended, err := fixture.service.ChangeGrantLifecycle(ctx, adminScope, GrantLifecycleInput{
		GrantRef: workloadResult.Grant.GrantRef, ExpectedRevision: workloadResult.Grant.Revision,
		Action: "suspend", Reason: "workload review", ClientRequestID: "live-workload-suspend", RequestID: "req-live-workload-suspend",
	})
	require.NoError(t, err, "containment must remain available for a disabled subject")
	resumeBefore := accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID)
	_, err = fixture.service.ChangeGrantLifecycle(ctx, adminScope, GrantLifecycleInput{
		GrantRef: workloadResult.Grant.GrantRef, ExpectedRevision: workloadSuspended.Revision,
		Action: "resume", Reason: "disabled workload", ClientRequestID: "live-workload-resume-disabled", RequestID: "req-live-workload-resume-disabled",
	})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, resumeBefore, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))
}

func TestGrantCredentialReadModelUsesLogicalFamilyAndPureSecretRotationIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	fixedNow := time.Now().UTC().Truncate(time.Microsecond)
	fixture.service.now = func() time.Time { return fixedNow }

	selfScope := fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	provisioned, err := fixture.service.Provision(ctx, fixture.personalInput("family-read-model-grant", SubmissionSelf))
	require.NoError(t, err)
	require.NotNil(t, provisioned.Grant)

	concurrency := 2
	rpm, rps := 30, 5
	first, err := fixture.service.IssueCredential(ctx, selfScope, CredentialInput{
		GrantRef: provisioned.Grant.GrantRef, Name: "family-read-model",
		RPMLimit: &rpm, RPSLimit: &rps, ConcurrencyLimit: &concurrency,
		AllowedIPs: "192.0.2.10/32", ClientRequestID: "family-read-model-issue", RequestID: "req-family-read-model-issue",
	})
	require.NoError(t, err)
	require.NotEmpty(t, first.CredentialRef)

	_, err = fixture.service.db.Exec(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
		  rpm_limit,qps_limit,concurrency_limit,budget_limit,allowed_ips,allowed_models,allowed_endpoints,policy_labels,
		  purpose,access_grant_id,access_credential_id,credential_ref,created_at,updated_at
		)
		SELECT org_id,project_id,owner_user_id,key_type,name,$1,$2,'active',
		       rpm_limit,qps_limit,concurrency_limit,budget_limit,allowed_ips,allowed_models,allowed_endpoints,policy_labels,
		       purpose,access_grant_id,access_credential_id,credential_ref,NOW(),NOW()
		FROM api_keys WHERE id=$3`, strings.Repeat("a", 64), "duplicate-active-v1", first.ID)
	require.Error(t, err)
	require.ErrorContains(t, err, "api_keys_access_credential_one_active_version")

	rotated, err := fixture.service.RotateCredential(ctx, selfScope, RotateCredentialInput{
		GrantRef: provisioned.Grant.GrantRef, PredecessorKeyID: first.ID,
		ClientRequestID: "family-read-model-rotate", RequestID: "req-family-read-model-rotate",
	})
	require.NoError(t, err)
	require.Equal(t, first.CredentialRef, rotated.Credential.CredentialRef)
	require.Equal(t, first.Name, rotated.Credential.Name)
	require.Equal(t, first.AllowedIPs, rotated.Credential.AllowedIPs)
	require.Equal(t, first.RPMLimit, rotated.Credential.RPMLimit)
	require.Equal(t, first.RPSLimit, rotated.Credential.RPSLimit)
	require.Equal(t, first.ConcurrencyLimit, rotated.Credential.ConcurrencyLimit)
	var activeVersions int
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM api_keys
		WHERE access_credential_id=(SELECT access_credential_id FROM api_keys WHERE id=$1)
		  AND status='active'`, rotated.Credential.ID).Scan(&activeVersions))
	require.Equal(t, 1, activeVersions)

	page, err := fixture.service.ListGrantCredentials(ctx, selfScope, provisioned.Grant.GrantRef, PageInput{})
	require.NoError(t, err)
	items := page.Data
	require.Len(t, items, 1)
	family := items[0]
	require.NotZero(t, family.ID)
	require.Equal(t, first.CredentialRef, family.CredentialRef)
	require.Equal(t, concurrency, *family.ConcurrencyLimit)
	require.Equal(t, 2, family.VersionCount)
	require.Len(t, family.Versions, 2)
	require.Equal(t, first.ID, family.Versions[0].ID)
	require.Equal(t, "disabled", family.Versions[0].EffectiveStatus)
	require.Equal(t, rotated.Credential.ID, family.Versions[1].ID)
	require.Equal(t, "active", family.Versions[1].EffectiveStatus)

	adminScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	suspended, err := fixture.service.ChangeGrantLifecycle(ctx, adminScope, GrantLifecycleInput{
		GrantRef: provisioned.Grant.GrantRef, ExpectedRevision: provisioned.Grant.Revision,
		Action: "suspend", Reason: "credential read-model containment check",
		ClientRequestID: "family-read-model-suspend", RequestID: "req-family-read-model-suspend",
	})
	require.NoError(t, err)

	selfScope = fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	page, err = fixture.service.ListGrantCredentials(ctx, selfScope, provisioned.Grant.GrantRef, PageInput{})
	require.NoError(t, err)
	items = page.Data
	require.Equal(t, "disabled_by_grant", items[0].EffectiveStatus)
	for _, version := range items[0].Versions {
		if version.ID == first.ID {
			require.Equal(t, "disabled", version.EffectiveStatus)
		} else {
			require.Equal(t, "disabled_by_grant", version.EffectiveStatus)
		}
	}

	adminScope = fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	require.NoError(t, fixture.service.RevokeCredential(ctx, adminScope, provisioned.Grant.GrantRef,
		rotated.Credential.ID, "req-family-read-model-revoke", "family-read-model-revoke"))
	selfScope = fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	page, err = fixture.service.ListGrantCredentials(ctx, selfScope, provisioned.Grant.GrantRef, PageInput{})
	require.NoError(t, err)
	items = page.Data
	require.Equal(t, "revoked", items[0].Status)
	require.Equal(t, "revoked", items[0].EffectiveStatus)
	for _, version := range items[0].Versions {
		require.Equal(t, "disabled", version.Status)
		require.Equal(t, "disabled", version.EffectiveStatus)
	}
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM api_keys
		WHERE access_credential_id=(SELECT id FROM access_credentials WHERE credential_ref=$1)
		  AND status='active'`, first.CredentialRef).Scan(&activeVersions))
	require.Zero(t, activeVersions)

	var revisionKinds, lifecycleReasons []string
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		SELECT ARRAY_AGG(revision_kind ORDER BY revision),
		       ARRAY_AGG(COALESCE(lifecycle_reason_code,'') ORDER BY revision)
		FROM access_grant_revisions WHERE access_grant_id=$1`, provisioned.Grant.ID).Scan(&revisionKinds, &lifecycleReasons))
	require.Equal(t, []string{"scope_decision", "lifecycle"}, revisionKinds)
	require.Equal(t, []string{"", "explicit_suspend"}, lifecycleReasons)
	require.EqualValues(t, 2, suspended.Revision)
}

func TestCredentialAtomicRotationSerializesAndRollsBackInsertFailureIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	selfScope := fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	provisioned, err := fixture.service.Provision(ctx, fixture.personalInput("atomic-rotation-grant", SubmissionSelf))
	require.NoError(t, err)
	require.NotNil(t, provisioned.Grant)
	predecessor, err := fixture.service.IssueCredential(ctx, selfScope, CredentialInput{
		GrantRef: provisioned.Grant.GrantRef, Name: "atomic-rotation-family",
		ClientRequestID: "atomic-rotation-issue", RequestID: "req-atomic-rotation-issue",
	})
	require.NoError(t, err)

	type rotationOutcome struct {
		result RotationResult
		err    error
	}
	start := make(chan struct{})
	outcomes := make(chan rotationOutcome, 2)
	var wg sync.WaitGroup
	for _, suffix := range []string{"a", "b"} {
		suffix := suffix
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, rotateErr := fixture.service.RotateCredential(ctx, selfScope, RotateCredentialInput{
				GrantRef: provisioned.Grant.GrantRef, PredecessorKeyID: predecessor.ID,
				ClientRequestID: "atomic-rotation-concurrent-" + suffix,
				RequestID:       "req-atomic-rotation-concurrent-" + suffix,
			})
			outcomes <- rotationOutcome{result: result, err: rotateErr}
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)
	var winner RotationResult
	successes, conflicts := 0, 0
	for outcome := range outcomes {
		if outcome.err == nil {
			successes++
			winner = outcome.result
			continue
		}
		if errors.Is(outcome.err, ErrConflict) {
			conflicts++
			continue
		}
		require.NoError(t, outcome.err)
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	require.NotZero(t, winner.Credential.ID)
	var activeVersions, successorVersions, rotationOperations int
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status='active'),
		       COUNT(*) FILTER (WHERE rotated_from_key_id=$2)
		FROM api_keys WHERE credential_ref=$1`, predecessor.CredentialRef, predecessor.ID).Scan(
		&activeVersions, &successorVersions))
	require.Equal(t, 1, activeVersions)
	require.Equal(t, 1, successorVersions)
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM access_operations
		WHERE access_grant_id=$1 AND operation_kind='credential_rotate'`, provisioned.Grant.ID).Scan(&rotationOperations))
	require.Equal(t, 1, rotationOperations)

	secondFamily, err := fixture.service.IssueCredential(ctx, selfScope, CredentialInput{
		GrantRef: provisioned.Grant.GrantRef, Name: "atomic-rotation-collision-family",
		ClientRequestID: "atomic-rotation-collision-issue", RequestID: "req-atomic-rotation-collision-issue",
	})
	require.NoError(t, err)
	require.NoError(t, func() error {
		_, createErr := fixture.service.db.Exec(ctx, `
			CREATE FUNCTION test_force_access_rotation_prefix_collision()
			RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
			  IF NEW.rotated_from_key_id IS NOT NULL THEN
			    SELECT key_prefix INTO NEW.key_prefix FROM api_keys WHERE id=NEW.rotated_from_key_id;
			  END IF;
			  RETURN NEW;
			END;
			$$;
			CREATE TRIGGER test_force_access_rotation_prefix_collision
			BEFORE INSERT ON api_keys FOR EACH ROW
			EXECUTE FUNCTION test_force_access_rotation_prefix_collision()`)
		return createErr
	}())
	t.Cleanup(func() {
		_, _ = fixture.service.db.Exec(context.Background(), `DROP TRIGGER test_force_access_rotation_prefix_collision ON api_keys`)
		_, _ = fixture.service.db.Exec(context.Background(), `DROP FUNCTION test_force_access_rotation_prefix_collision()`)
	})
	beforeCollision := accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID)
	_, err = fixture.service.RotateCredential(ctx, selfScope, RotateCredentialInput{
		GrantRef: provisioned.Grant.GrantRef, PredecessorKeyID: secondFamily.ID,
		ClientRequestID: "atomic-rotation-forced-collision", RequestID: "req-atomic-rotation-forced-collision",
	})
	require.ErrorContains(t, err, "prefix collision limit reached")
	require.Equal(t, beforeCollision, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))
	var predecessorStatus string
	require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT status FROM api_keys WHERE id=$1`, secondFamily.ID).Scan(&predecessorStatus))
	require.Equal(t, "active", predecessorStatus)
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM api_keys WHERE credential_ref=$1 AND status='active'`, secondFamily.CredentialRef).Scan(&activeVersions))
	require.Equal(t, 1, activeVersions)
}

func TestCredentialFamilyRevokeUsesExplicitContainmentAuthorityIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	adminScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")
	workloadResult, err := fixture.service.Provision(ctx, ProvisionInput{
		ClientRequestID: "revoke-authority-workload", RequestID: "req-revoke-authority-workload",
		OrgID: fixture.orgID, ProjectID: fixture.projectID,
		Subject:     Subject{Type: SubjectWorkloadApplication, WorkloadApplicationID: fixture.workloadID},
		RequestKind: RequestKindWorkloadService,
		Envelope:    Envelope{ProjectRole: auth.ProjectRoleUser, ScopeMode: "project_default"},
		Environment: "development", SubmissionMode: SubmissionDirectGrant, SponsorUserID: fixture.sponsorID,
	})
	require.NoError(t, err)
	require.NotNil(t, workloadResult.Grant)

	issue := func(name string) Credential {
		t.Helper()
		credential, issueErr := fixture.service.IssueCredential(ctx, adminScope, CredentialInput{
			GrantRef: workloadResult.Grant.GrantRef, Name: name,
			ClientRequestID: name + "-issue", RequestID: "req-" + name + "-issue",
		})
		require.NoError(t, issueErr)
		return credential
	}
	orgAdminFamily := issue("revoke-org-admin-combination")
	rotated, err := fixture.service.RotateCredential(ctx, adminScope, RotateCredentialInput{
		GrantRef: workloadResult.Grant.GrantRef, PredecessorKeyID: orgAdminFamily.ID,
		ClientRequestID: "revoke-org-admin-rotate", RequestID: "req-revoke-org-admin-rotate",
	})
	require.NoError(t, err)
	projectAdminFamily := issue("revoke-project-admin")
	projectUserFamily := issue("revoke-project-user-denied")

	_, err = fixture.service.db.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, fixture.projectID, fixture.orgAdminID)
	require.NoError(t, err)
	adminScope = fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, auth.ProjectRoleUser)
	require.NoError(t, fixture.service.RevokeCredential(ctx, adminScope, workloadResult.Grant.GrantRef,
		rotated.Credential.ID, "req-revoke-org-admin-combination", "revoke-org-admin-combination"))
	var familyStatus string
	var disabledVersions int
	require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT status FROM access_credentials WHERE credential_ref=$1`, orgAdminFamily.CredentialRef).Scan(&familyStatus))
	require.NoError(t, fixture.service.db.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE credential_ref=$1 AND status='disabled'`, orgAdminFamily.CredentialRef).Scan(&disabledVersions))
	require.Equal(t, "revoked", familyStatus)
	require.Equal(t, 2, disabledVersions)

	var projectAdminID int64
	require.NoError(t, fixture.service.db.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'revoke-project-admin-user','hash','member','project_admin','active') RETURNING id`, fixture.orgID).Scan(&projectAdminID))
	_, err = fixture.service.db.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_admin')`, fixture.projectID, projectAdminID)
	require.NoError(t, err)
	projectAdminScope := fixture.useActor(projectAdminID, auth.RoleMember, auth.ProjectRoleAdmin)
	require.NoError(t, fixture.service.RevokeCredential(ctx, projectAdminScope, workloadResult.Grant.GrantRef,
		projectAdminFamily.ID, "req-revoke-project-admin", "revoke-project-admin"))

	projectUserScope := fixture.useActor(fixture.sponsorID, auth.RoleMember, auth.ProjectRoleUser)
	beforeDenied := accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID)
	err = fixture.service.RevokeCredential(ctx, projectUserScope, workloadResult.Grant.GrantRef,
		projectUserFamily.ID, "req-revoke-project-user", "revoke-project-user")
	require.ErrorIs(t, err, ErrNotFound, "workload credential inventory is hidden from ordinary project users")
	require.Equal(t, beforeDenied, accessMutationCounts(t, ctx, fixture.service.db, fixture.orgID))

	ownerScope := fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	humanResult, err := fixture.service.Provision(ctx, fixture.personalInput("revoke-human-owner-grant", SubmissionSelf))
	require.NoError(t, err)
	humanFamily, err := fixture.service.IssueCredential(ctx, ownerScope, CredentialInput{
		GrantRef: humanResult.Grant.GrantRef, Name: "revoke-human-owner",
		ClientRequestID: "revoke-human-owner-issue", RequestID: "req-revoke-human-owner-issue",
	})
	require.NoError(t, err)
	require.NoError(t, fixture.service.RevokeCredential(ctx, ownerScope, humanResult.Grant.GrantRef,
		humanFamily.ID, "req-revoke-human-owner", "revoke-human-owner"))
}

func TestAccessProvisioningListsUseBoundedStableKeysetPaginationIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	adminScope := fixture.useActor(fixture.orgAdminID, auth.RoleOrgAdmin, "")

	requestIDs := make([]int64, 0, 5)
	grantIDs := make([]int64, 0, 5)
	var activeGrant Grant
	for index := 0; index < 5; index++ {
		result, err := fixture.service.Provision(ctx, fixture.personalInput(
			fmt.Sprintf("keyset-lifecycle-%d", index), SubmissionDirectGrant))
		require.NoError(t, err)
		require.NotNil(t, result.Grant)
		requestIDs = append(requestIDs, result.Request.ID)
		grantIDs = append(grantIDs, result.Grant.ID)
		activeGrant = *result.Grant
		if index < 4 {
			_, err = fixture.service.ChangeGrantLifecycle(ctx, adminScope, GrantLifecycleInput{
				GrantRef: result.Grant.GrantRef, ExpectedRevision: result.Grant.Revision,
				Action: "revoke", Reason: "complete keyset fixture lifecycle",
				ClientRequestID: fmt.Sprintf("keyset-revoke-%d", index),
				RequestID:       fmt.Sprintf("req-keyset-revoke-%d", index),
			})
			require.NoError(t, err)
		}
	}

	assertRequestPages := func(t *testing.T, list func(PageInput) (RequestPage, error)) {
		t.Helper()
		first, err := list(PageInput{Limit: 2})
		require.NoError(t, err)
		require.Equal(t, []int64{requestIDs[4], requestIDs[3]}, []int64{first.Data[0].ID, first.Data[1].ID})
		require.True(t, first.Pagination.HasMore)
		require.Equal(t, requestIDs[3], *first.Pagination.NextAfterID)

		second, err := list(PageInput{Limit: 2, AfterID: *first.Pagination.NextAfterID})
		require.NoError(t, err)
		require.Equal(t, []int64{requestIDs[2], requestIDs[1]}, []int64{second.Data[0].ID, second.Data[1].ID})
		require.True(t, second.Pagination.HasMore)
		require.Equal(t, requestIDs[1], *second.Pagination.NextAfterID)

		third, err := list(PageInput{Limit: 2, AfterID: *second.Pagination.NextAfterID})
		require.NoError(t, err)
		require.Len(t, third.Data, 1)
		require.Equal(t, requestIDs[0], third.Data[0].ID)
		require.False(t, third.Pagination.HasMore)
		require.Nil(t, third.Pagination.NextAfterID)
	}

	assertRequestPages(t, func(input PageInput) (RequestPage, error) {
		return fixture.service.ListRequests(ctx, adminScope, fixture.orgID, fixture.projectID, input)
	})
	assertRequestPages(t, func(input PageInput) (RequestPage, error) {
		return fixture.service.ListMyRequests(ctx, adminScope, input)
	})

	firstGrants, err := fixture.service.ListGrants(ctx, adminScope, fixture.orgID, fixture.projectID, PageInput{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []int64{grantIDs[4], grantIDs[3]}, []int64{firstGrants.Data[0].ID, firstGrants.Data[1].ID})
	require.True(t, firstGrants.Pagination.HasMore)
	secondGrants, err := fixture.service.ListGrants(ctx, adminScope, fixture.orgID, fixture.projectID,
		PageInput{Limit: 2, AfterID: *firstGrants.Pagination.NextAfterID})
	require.NoError(t, err)
	require.Equal(t, []int64{grantIDs[2], grantIDs[1]}, []int64{secondGrants.Data[0].ID, secondGrants.Data[1].ID})
	thirdGrants, err := fixture.service.ListGrants(ctx, adminScope, fixture.orgID, fixture.projectID,
		PageInput{Limit: 2, AfterID: *secondGrants.Pagination.NextAfterID})
	require.NoError(t, err)
	require.Len(t, thirdGrants.Data, 1)
	require.Equal(t, grantIDs[0], thirdGrants.Data[0].ID)
	require.False(t, thirdGrants.Pagination.HasMore)

	selfScope := fixture.useActor(fixture.subjectID, auth.RoleMember, auth.ProjectRoleUser)
	credentialRefs := make([]string, 0, 5)
	for index := 0; index < 5; index++ {
		credential, err := fixture.service.IssueCredential(ctx, selfScope, CredentialInput{
			GrantRef: activeGrant.GrantRef, Name: fmt.Sprintf("keyset-family-%d", index),
			ClientRequestID: fmt.Sprintf("keyset-family-issue-%d", index),
			RequestID:       fmt.Sprintf("req-keyset-family-issue-%d", index),
		})
		require.NoError(t, err)
		credentialRefs = append(credentialRefs, credential.CredentialRef)
	}
	firstCredentials, err := fixture.service.ListGrantCredentials(ctx, selfScope, activeGrant.GrantRef, PageInput{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []string{credentialRefs[4], credentialRefs[3]},
		[]string{firstCredentials.Data[0].CredentialRef, firstCredentials.Data[1].CredentialRef})
	require.True(t, firstCredentials.Pagination.HasMore)
	secondCredentials, err := fixture.service.ListGrantCredentials(ctx, selfScope, activeGrant.GrantRef,
		PageInput{Limit: 2, AfterID: *firstCredentials.Pagination.NextAfterID})
	require.NoError(t, err)
	require.Equal(t, []string{credentialRefs[2], credentialRefs[1]},
		[]string{secondCredentials.Data[0].CredentialRef, secondCredentials.Data[1].CredentialRef})
	thirdCredentials, err := fixture.service.ListGrantCredentials(ctx, selfScope, activeGrant.GrantRef,
		PageInput{Limit: 2, AfterID: *secondCredentials.Pagination.NextAfterID})
	require.NoError(t, err)
	require.Len(t, thirdCredentials.Data, 1)
	require.Equal(t, credentialRefs[0], thirdCredentials.Data[0].CredentialRef)
	require.False(t, thirdCredentials.Pagination.HasMore)

	_, err = fixture.service.ListRequests(ctx, adminScope, fixture.orgID, fixture.projectID, PageInput{Limit: MaxPageLimit + 1})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = fixture.service.ListMyRequests(ctx, adminScope, PageInput{AfterID: -1})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = fixture.service.ListGrants(ctx, adminScope, fixture.orgID, fixture.projectID, PageInput{Limit: -1})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = fixture.service.ListGrantCredentials(ctx, selfScope, activeGrant.GrantRef, PageInput{Limit: MaxPageLimit + 1})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestLegacyPreviewGroupsApplicationsAndClassifiesUnattachedCredentialsIntegration(t *testing.T) {
	fixture := newAccessServiceFixture(t)
	ctx := context.Background()
	database := fixture.service.db

	var inactiveOwnerID, nonCallingOwnerID int64
	require.NoError(t, database.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'preview-inactive-owner','hash','member','member','disabled') RETURNING id`,
		fixture.orgID).Scan(&inactiveOwnerID))
	require.NoError(t, database.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'preview-noncalling-owner','hash','member','member','active') RETURNING id`,
		fixture.orgID).Scan(&nonCallingOwnerID))

	insertApplication := func(ref, kind, identityMode string) int64 {
		t.Helper()
		var applicationID int64
		require.NoError(t, database.QueryRow(ctx, `
			INSERT INTO workload_applications (
			  contract_version,identity_contract_version,org_id,project_id,application_ref,
			  display_name,kind,environment,identity_mode,client_profile,status,created_by,updated_by
			) VALUES (
			  'unirouter.workload-application.v1','unirouter.workload-identity.v1',$1,$2,$3,
			  $3,$4,'development',$5,'client.openai-sdk.chat.v1','active',$6,$6
			) RETURNING id`, fixture.orgID, fixture.projectID, ref, kind, identityMode,
			fixture.orgAdminID).Scan(&applicationID))
		return applicationID
	}
	zeroKeyServiceID := insertApplication("app_preview_zero_key", "service", "service_credential")
	personalSingleID := insertApplication("app_preview_personal_one", "service", "personal_development")
	personalMultiID := insertApplication("app_preview_personal_multi", "service", "personal_development")
	personalMissingID := insertApplication("app_preview_personal_missing", "service", "personal_development")
	interactiveID := insertApplication("app_preview_interactive", "interactive_client", "personal_development")
	serviceID := insertApplication("app_preview_service", "agent", "service_credential")
	purposeOnlyID := insertApplication("app_preview_connector_only", "integration_adapter", "service_credential")

	insertKey := func(prefix, keyType string, projectID, ownerID, applicationID any, purpose, status string) int64 {
		t.Helper()
		allowedIPs := ""
		if purpose != "" {
			allowedIPs = "192.0.2.0/24"
		}
		var keyID int64
		require.NoError(t, database.QueryRow(ctx, `
			INSERT INTO api_keys (
			  org_id,project_id,owner_user_id,workload_application_id,name,key_prefix,key_hash,
			  key_type,purpose,status,allowed_ips,allowed_models
			) VALUES ($1,$2,$3,$4,$5::text,$5::text,$10,$6,$7,$8,$9,'')
			RETURNING id`, fixture.orgID, projectID, ownerID, applicationID, prefix, keyType,
			purpose, status, allowedIPs, strings.Repeat("a", 64)).Scan(&keyID))
		return keyID
	}
	insertKey("lp-app-one", "personal", fixture.projectID, fixture.subjectID, personalSingleID, "", "active")
	insertKey("lp-app-multi-a", "personal", fixture.projectID, fixture.subjectID, personalMultiID, "", "active")
	insertKey("lp-app-multi-b", "personal", fixture.projectID, fixture.otherProjectUserID, personalMultiID, "", "active")
	insertKey("lp-app-missing", "personal", fixture.projectID, nil, personalMissingID, "", "active")
	insertKey("lp-app-service", "service", fixture.projectID, fixture.sponsorID, serviceID, "", "active")
	insertKey("lp-purpose", "service", fixture.projectID, fixture.orgAdminID, purposeOnlyID, "xinsight.feed.read", "disabled")
	insertKey("lp-personal-ok", "personal", fixture.projectID, fixture.subjectID, nil, "", "active")
	insertKey("lp-personal-noproj", "personal", nil, fixture.subjectID, nil, "", "active")
	insertKey("lp-personal-noown", "personal", fixture.projectID, nil, nil, "", "active")
	insertKey("lp-personal-off", "personal", fixture.projectID, inactiveOwnerID, nil, "", "active")
	insertKey("lp-personal-nocall", "personal", fixture.projectID, nonCallingOwnerID, nil, "", "active")
	insertKey("lp-shared", "shared", fixture.projectID, nil, nil, "", "active")
	insertKey("lp-service", "service", fixture.projectID, fixture.subjectID, nil, "", "active")

	var foreignOrgID, foreignAdminID, foreignProjectID int64
	require.NoError(t, database.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('preview-foreign','active','customer') RETURNING id`).Scan(&foreignOrgID))
	require.NoError(t, database.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'preview-foreign-admin','hash','org_admin','org_admin','active') RETURNING id`, foreignOrgID).Scan(&foreignAdminID))
	require.NoError(t, database.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'Preview Foreign','preview-foreign','active') RETURNING id`, foreignOrgID).Scan(&foreignProjectID))
	_, err := database.Exec(ctx, `
		INSERT INTO workload_applications (
		  contract_version,identity_contract_version,org_id,project_id,application_ref,
		  display_name,kind,environment,identity_mode,client_profile,status,created_by,updated_by
		) VALUES (
		  'unirouter.workload-application.v1','unirouter.workload-identity.v1',$1,$2,
		  'app_preview_foreign','Preview Foreign','service','development','service_credential',
		  'client.openai-sdk.chat.v1','active',$3,$3)`, foreignOrgID, foreignProjectID, foreignAdminID)
	require.NoError(t, err)

	items, err := fixture.service.LegacyPreview(ctx, fixture.orgID)
	require.NoError(t, err)
	byRef := make(map[string]LegacyClassification, len(items))
	for _, item := range items {
		require.False(t, item.AutomaticMigration)
		byRef[item.ObjectRef] = item
	}

	require.NotContains(t, byRef, "app_preview_foreign")
	zeroKey := byRef["app_preview_zero_key"]
	require.Equal(t, zeroKeyServiceID, zeroKey.ObjectID)
	require.Equal(t, "workload_candidate", zeroKey.Classification)
	require.Zero(t, zeroKey.LinkedCredentialCount)
	require.Empty(t, zeroKey.LinkedCredentials)

	personalSingle := byRef["app_preview_personal_one"]
	require.Equal(t, personalSingleID, personalSingle.ObjectID)
	require.Equal(t, "human_personal_candidate", personalSingle.Classification)
	require.Equal(t, "unambiguous_personal_development_owner_project", personalSingle.ReasonCode)
	require.Equal(t, fixture.subjectID, personalSingle.OwnerUserID)
	require.Len(t, personalSingle.LinkedCredentials, 1)
	require.Equal(t, "linked_to_workload_application", byRef["lp-app-one"].Classification)

	require.Equal(t, personalMultiID, byRef["app_preview_personal_multi"].ObjectID)
	require.Equal(t, "personal_development_multiple_owners", byRef["app_preview_personal_multi"].ReasonCode)
	require.Equal(t, personalMissingID, byRef["app_preview_personal_missing"].ObjectID)
	require.Equal(t, "personal_development_missing_owner", byRef["app_preview_personal_missing"].ReasonCode)
	require.Equal(t, interactiveID, byRef["app_preview_interactive"].ObjectID)
	require.Equal(t, "interactive_client_requires_review", byRef["app_preview_interactive"].ReasonCode)
	require.Equal(t, serviceID, byRef["app_preview_service"].ObjectID)
	require.Equal(t, "workload_candidate", byRef["app_preview_service"].Classification)
	require.Equal(t, purposeOnlyID, byRef["app_preview_connector_only"].ObjectID)
	require.Equal(t, "connector_separate", byRef["app_preview_connector_only"].Classification)
	require.Equal(t, "purpose_credential_excluded", byRef["app_preview_connector_only"].ReasonCode)
	require.Equal(t, "excluded", byRef["app_preview_connector_only"].MigrationStatus)
	require.Equal(t, "connector_separate", byRef["lp-purpose"].Classification)

	require.Equal(t, "human_personal_candidate", byRef["lp-personal-ok"].Classification)
	require.Equal(t, "unambiguous_single_owner_project", byRef["lp-personal-ok"].ReasonCode)
	require.Equal(t, "personal_key_missing_project", byRef["lp-personal-noproj"].ReasonCode)
	require.Equal(t, "personal_key_missing_owner", byRef["lp-personal-noown"].ReasonCode)
	require.Equal(t, "personal_key_owner_not_active", byRef["lp-personal-off"].ReasonCode)
	require.Equal(t, "personal_key_owner_not_calling", byRef["lp-personal-nocall"].ReasonCode)
	require.Equal(t, "legacy_unbound", byRef["lp-shared"].Classification)
	require.Equal(t, "review_required", byRef["lp-service"].Classification)
	require.Equal(t, "unbound_service_subject_ambiguous", byRef["lp-service"].ReasonCode)
}

type accessMutationCount struct {
	Families, Keys, Operations, Revisions, Audits, Outbox int
}

func accessMutationCounts(t *testing.T, ctx context.Context, database interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, orgID int64) accessMutationCount {
	t.Helper()
	var result accessMutationCount
	require.NoError(t, database.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM access_credentials WHERE org_id=$1),
		  (SELECT COUNT(*) FROM api_keys WHERE org_id=$1),
		  (SELECT COUNT(*) FROM access_operations WHERE org_id=$1),
		  (SELECT COUNT(*) FROM access_grant_revisions WHERE org_id=$1),
		  (SELECT COUNT(*) FROM audit_logs WHERE org_id=$1 AND action LIKE 'access_%'),
		  (SELECT COUNT(*) FROM internal_outbox_events WHERE org_id=$1 AND event_type LIKE 'access_%')`, orgID).Scan(
		&result.Families, &result.Keys, &result.Operations, &result.Revisions, &result.Audits, &result.Outbox,
	))
	return result
}
