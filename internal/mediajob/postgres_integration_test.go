//go:build integration

package mediajob

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/accessgrantlifecycle"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/principallock"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestAdminScopeUsesOrganizationRoleAndLiveMembershipIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID, projectID, _ := seedMediaScope(t, pool)
	var otherProjectID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id, name, slug, status, budget_period)
		VALUES ($1, 'Other media', 'other-media-scope', 'active', 'none')
		RETURNING id`, orgID).Scan(&otherProjectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id, username, password_hash, organization_role, role, status)
		VALUES ($1, 'media-org-viewer', 'not-a-login-verifier', 'org_viewer', 'org_viewer', 'active')
		RETURNING id`, orgID).Scan(&userID))
	_, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role)
		VALUES ($1, $2, 'project_admin')`, projectID, userID)
	require.NoError(t, err)

	handler := &HTTPHandler{db: pool}
	request := func(targetProjectID int64) *http.Request {
		req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/media-jobs?project_id=%d", targetProjectID), nil)
		requestCtx := context.WithValue(req.Context(), middleware.CtxOrgID, orgID)
		requestCtx = context.WithValue(requestCtx, middleware.CtxUserID, userID)
		requestCtx = context.WithValue(requestCtx, middleware.CtxUserRole, auth.RoleOrgViewer)
		requestCtx = context.WithValue(requestCtx, middleware.CtxOrganizationRole, auth.RoleOrgViewer)
		return req.WithContext(requestCtx)
	}

	_, err = handler.resolveAdminScope(request(projectID), adminScopeRead)
	require.NoError(t, err, "explicit project_admin membership must allow media reads")
	_, err = handler.resolveAdminScope(request(projectID), adminScopeWrite)
	require.NoError(t, err, "explicit project_admin membership must allow media cancellation")
	_, err = handler.resolveAdminScope(request(otherProjectID), adminScopeRead)
	require.ErrorIs(t, err, ErrForbidden, "membership must not propagate across projects")

	_, err = pool.Exec(ctx, `UPDATE project_members SET role='project_viewer' WHERE project_id=$1 AND user_id=$2`, projectID, userID)
	require.NoError(t, err)
	_, err = handler.resolveAdminScope(request(projectID), adminScopeRead)
	require.NoError(t, err, "project_viewer membership must retain media read access")
	_, err = handler.resolveAdminScope(request(projectID), adminScopeWrite)
	require.ErrorIs(t, err, ErrForbidden, "project_viewer must not cancel media jobs")
}

func TestPostgresStoreImageLifecycleFencingAndTenantIsolation(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID, projectID, keyID := seedMediaScope(t, pool)
	scope := Scope{OrgID: orgID, ProjectID: projectID}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	record := mediaCreateRecord(scope, keyID, "1", now, 3)
	created, reused, err := store.CreateOrGet(ctx, record)
	require.NoError(t, err)
	require.False(t, reused)
	require.NotEmpty(t, created.RequestCiphertext)
	repeated, reused, err := store.CreateOrGet(ctx, record)
	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, created.JobID, repeated.JobID)
	_, err = store.Get(
		ctx,
		Scope{OrgID: orgID + 1, ProjectID: projectID},
		created.JobID,
	)
	require.ErrorIs(t, err, ErrNotFound)

	cancelled, err := store.RequestCancellation(ctx, scope, created.JobID, now.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, CancellationAccepted, cancelled.Outcome)
	require.Equal(t, OutcomeNotStarted, cancelled.Job.Lifecycle.Outcome)
	require.Empty(t, cancelled.Job.RequestCiphertext)

	successRecord := mediaCreateRecord(scope, keyID, "2", now.Add(2*time.Second), 3)
	successJob, _, err := store.CreateOrGet(ctx, successRecord)
	require.NoError(t, err)
	claimed, err := store.Claim(ctx, "worker-a", now.Add(3*time.Second), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, successJob.JobID, claimed.JobID)
	lease := leaseFromClaimed(*claimed)
	require.NoError(t, store.MarkRunning(ctx, lease, now.Add(4*time.Second)))
	require.NoError(t, store.MarkProviderSucceeded(
		ctx, lease, "gpt-image-1", now.Add(5*time.Second),
	))
	persistedAt := now.Add(6 * time.Second)
	completed, err := store.CompleteSuccess(
		ctx,
		lease,
		PersistedResult{
			ObjectKey: "private/object", ReceiptKey: "private/receipt",
			ContentSHA256: "sha256:" + strings.Repeat("d", 64),
			MediaType:     "image/png", SizeBytes: 128,
			PersistedAt: persistedAt, Outcome: integrationImageOutcome(),
			MappedModel: "gpt-image-1",
		},
		DefaultRetention,
		now.Add(7*time.Second),
	)
	require.NoError(t, err)
	require.Equal(t, OutcomeSucceeded, completed.Lifecycle.Outcome)
	require.Equal(t, ObjectPersisted, completed.Lifecycle.Object)
	require.Equal(t, SettlementChargeFactRecorded, completed.Lifecycle.Settlement)
	require.Empty(t, completed.RequestCiphertext)
	require.NotNil(t, completed.Output)
	require.Equal(t, persistedAt.Add(DefaultRetention), *completed.Output.ExpiresAt)
	require.NotNil(t, completed.ChargeFact)
	require.Equal(t, ChargeAmountPricingPending, completed.ChargeFact.AmountState)
	require.Nil(t, completed.ChargeFact.AmountMicros)
	require.Len(t, completed.ChargeFact.Units, 4)

	late, err := store.RequestCancellation(ctx, scope, completed.JobID, now.Add(8*time.Second))
	require.NoError(t, err)
	require.Equal(t, CancellationTooLate, late.Outcome)
	require.Equal(t, OutcomeSucceeded, late.Job.Lifecycle.Outcome)
	require.NotNil(t, late.Job.ChargeFact)

	purge, err := store.ClaimExpired(
		ctx, "worker-a-purge", persistedAt.Add(DefaultRetention), time.Minute,
	)
	require.NoError(t, err)
	require.NotNil(t, purge)
	stale := *purge
	stale.Fence--
	_, err = store.MarkPurged(
		ctx, stale, persistedAt.Add(DefaultRetention).Add(time.Second),
	)
	require.ErrorIs(t, err, ErrLeaseLost)
	purged, err := store.MarkPurged(
		ctx, *purge, persistedAt.Add(DefaultRetention).Add(time.Second),
	)
	require.NoError(t, err)
	require.Equal(t, ObjectPurged, purged.Lifecycle.Object)
	require.Empty(t, purged.ObjectKey)
	require.NotNil(t, purged.Output)
	require.NotNil(t, purged.Output.PurgedAt)
}

func TestPostgresStoreUnknownAndRecoveryOnlyStates(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID, projectID, keyID := seedMediaScope(t, pool)
	scope := Scope{OrgID: orgID, ProjectID: projectID}
	now := time.Date(2026, 7, 31, 13, 0, 0, 0, time.UTC)

	unknownRecord := mediaCreateRecord(scope, keyID, "3", now, 1)
	unknownJob, _, err := store.CreateOrGet(ctx, unknownRecord)
	require.NoError(t, err)
	claimed, err := store.Claim(ctx, "worker-u", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	require.Equal(t, unknownJob.JobID, claimed.JobID)
	lease := leaseFromClaimed(*claimed)
	require.NoError(t, store.MarkRunning(ctx, lease, now.Add(2*time.Second)))
	_, err = store.RequestCancellation(ctx, scope, claimed.JobID, now.Add(3*time.Second))
	require.NoError(t, err)
	unknown, err := store.CompleteUnknown(
		ctx, lease, "provider_transport_outcome_unknown", now.Add(4*time.Second),
	)
	require.NoError(t, err)
	require.Equal(t, OutcomeUnknown, unknown.Lifecycle.Outcome)
	require.Equal(t, CancellationIndeterminate, unknown.Lifecycle.Cancellation)
	require.Equal(t, SettlementNotEligible, unknown.Lifecycle.Settlement)
	require.Nil(t, unknown.ChargeFact)

	recoveryRecord := mediaCreateRecord(scope, keyID, "4", now.Add(5*time.Second), 2)
	recoveryJob, _, err := store.CreateOrGet(ctx, recoveryRecord)
	require.NoError(t, err)
	claimed, err = store.Claim(ctx, "worker-r", now.Add(6*time.Second), time.Minute)
	require.NoError(t, err)
	require.Equal(t, recoveryJob.JobID, claimed.JobID)
	lease = leaseFromClaimed(*claimed)
	require.NoError(t, store.MarkRunning(ctx, lease, now.Add(7*time.Second)))
	require.NoError(t, store.MarkProviderSucceeded(
		ctx, lease, "gpt-image-1", now.Add(8*time.Second),
	))
	requeued, err := store.RequeueRecovery(
		ctx, lease, "image_put_failed",
		now.Add(9*time.Second), now.Add(10*time.Second),
	)
	require.NoError(t, err)
	require.Equal(t, OutcomeSucceeded, requeued.Lifecycle.Outcome)
	require.Equal(t, ExecutionPending, requeued.Lifecycle.Execution)
	require.Empty(t, requeued.RequestCiphertext)

	claimed, err = store.Claim(ctx, "worker-r2", now.Add(11*time.Second), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, recoveryJob.JobID, claimed.JobID)
	require.Equal(t, OutcomeSucceeded, claimed.Lifecycle.Outcome)
	recoveryLease := leaseFromClaimed(*claimed)
	outputFailed, err := store.CompleteOutputFailure(
		ctx, recoveryLease, "persisted_object_unavailable", now.Add(12*time.Second),
	)
	require.NoError(t, err)
	require.Equal(t, OutcomeSucceeded, outputFailed.Lifecycle.Outcome)
	require.Equal(t, ObjectNotCreated, outputFailed.Lifecycle.Object)
	require.Equal(t, SettlementNotEligible, outputFailed.Lifecycle.Settlement)
	require.Nil(t, outputFailed.ChargeFact)
}

func TestPostgresStoreRetrySuccessFenceAndExpiredDispatchSafety(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID, projectID, keyID := seedMediaScope(t, pool)
	scope := Scope{OrgID: orgID, ProjectID: projectID}
	now := time.Date(2026, 7, 31, 14, 0, 0, 0, time.UTC)

	retryJob, _, err := store.CreateOrGet(
		ctx,
		mediaCreateRecord(scope, keyID, "5", now, 3),
	)
	require.NoError(t, err)
	claimed, err := store.Claim(ctx, "worker-retry", now.Add(time.Second), time.Minute)
	require.NoError(t, err)
	require.Equal(t, retryJob.JobID, claimed.JobID)
	retryLease := leaseFromClaimed(*claimed)
	require.NoError(t, store.MarkRunning(ctx, retryLease, now.Add(2*time.Second)))
	retryAt := now.Add(10 * time.Second)
	requeued, err := store.Requeue(
		ctx,
		retryLease,
		"provider_http_503_channel_failure",
		now.Add(3*time.Second),
		retryAt,
	)
	require.NoError(t, err)
	require.Equal(t, "provider_http_503_channel_failure", requeued.ErrorCode)
	require.Equal(t, retryAt, requeued.NextAttemptAt)
	require.Equal(t, OutcomePending, requeued.Lifecycle.Outcome)
	require.Equal(t, ExecutionPending, requeued.Lifecycle.Execution)
	claimed, err = store.Claim(ctx, "worker-too-early", now.Add(9*time.Second), time.Minute)
	require.NoError(t, err)
	require.Nil(t, claimed)
	claimed, err = store.Claim(ctx, "worker-retry-2", retryAt, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, retryJob.JobID, claimed.JobID)
	retryLease = leaseFromClaimed(*claimed)
	require.NoError(t, store.MarkRunning(ctx, retryLease, now.Add(11*time.Second)))
	failed, err := store.CompleteFailure(
		ctx, retryLease, "provider_http_400_invalid_request", now.Add(12*time.Second),
	)
	require.NoError(t, err)
	require.Equal(t, OutcomeFailed, failed.Lifecycle.Outcome)

	notStartedJob, _, err := store.CreateOrGet(
		ctx,
		mediaCreateRecord(scope, keyID, "9", now.Add(13*time.Second), 1),
	)
	require.NoError(t, err)
	claimed, err = store.Claim(
		ctx,
		"worker-predispatch-failure",
		now.Add(14*time.Second),
		time.Minute,
	)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, notStartedJob.JobID, claimed.JobID)
	notStarted, err := store.CompleteFailure(
		ctx,
		leaseFromClaimed(*claimed),
		"stored_request_unavailable",
		now.Add(15*time.Second),
	)
	require.NoError(t, err)
	require.Equal(t, OutcomeNotStarted, notStarted.Lifecycle.Outcome)
	require.Equal(t, ExecutionTerminal, notStarted.Lifecycle.Execution)
	require.Nil(t, notStarted.ChargeFact)

	fencedJob, _, err := store.CreateOrGet(
		ctx,
		mediaCreateRecord(scope, keyID, "6", now.Add(20*time.Second), 3),
	)
	require.NoError(t, err)
	claimed, err = store.Claim(ctx, "worker-fence", now.Add(21*time.Second), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, fencedJob.JobID, claimed.JobID)
	fencedLease := leaseFromClaimed(*claimed)
	require.NoError(t, store.MarkRunning(ctx, fencedLease, now.Add(22*time.Second)))
	result := PersistedResult{
		ObjectKey: "private/fenced-object", ReceiptKey: "private/fenced-receipt",
		ContentSHA256: "sha256:" + strings.Repeat("e", 64),
		MediaType:     "image/png", SizeBytes: 256,
		PersistedAt: now.Add(23 * time.Second), Outcome: integrationImageOutcome(),
		MappedModel: "gpt-image-1",
	}
	_, err = store.CompleteSuccess(
		ctx, fencedLease, result, DefaultRetention, now.Add(24*time.Second),
	)
	require.ErrorIs(t, err, ErrLeaseLost)
	stillPending, err := store.Get(ctx, scope, fencedJob.JobID)
	require.NoError(t, err)
	require.Equal(t, OutcomePending, stillPending.Lifecycle.Outcome)
	require.Nil(t, stillPending.ChargeFact)
	_, err = store.RequeueRecovery(
		ctx,
		fencedLease,
		"must_not_create_success",
		now.Add(24*time.Second),
		now.Add(25*time.Second),
	)
	require.ErrorIs(t, err, ErrLeaseLost)
	require.NoError(t, store.MarkProviderSucceeded(
		ctx, fencedLease, "gpt-image-1", now.Add(25*time.Second),
	))
	completed, err := store.CompleteSuccess(
		ctx, fencedLease, result, DefaultRetention, now.Add(26*time.Second),
	)
	require.NoError(t, err)
	require.Equal(t, OutcomeSucceeded, completed.Lifecycle.Outcome)
	require.NotNil(t, completed.ChargeFact)

	ambiguousJob, _, err := store.CreateOrGet(
		ctx,
		mediaCreateRecord(scope, keyID, "7", now.Add(30*time.Second), 3),
	)
	require.NoError(t, err)
	claimed, err = store.Claim(ctx, "worker-ambiguous", now.Add(31*time.Second), 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, ambiguousJob.JobID, claimed.JobID)
	ambiguousLease := leaseFromClaimed(*claimed)
	require.NoError(t, store.MarkRunning(ctx, ambiguousLease, now.Add(32*time.Second)))
	_, err = store.RequestCancellation(
		ctx, scope, ambiguousJob.JobID, now.Add(33*time.Second),
	)
	require.NoError(t, err)
	claimed, err = store.Claim(ctx, "worker-must-not-redispatch", now.Add(37*time.Second), time.Minute)
	require.NoError(t, err)
	require.Nil(t, claimed)
	ambiguous, err := store.Get(ctx, scope, ambiguousJob.JobID)
	require.NoError(t, err)
	require.Equal(t, OutcomeUnknown, ambiguous.Lifecycle.Outcome)
	require.Equal(t, CancellationIndeterminate, ambiguous.Lifecycle.Cancellation)
	require.Equal(t, ExecutionTerminal, ambiguous.Lifecycle.Execution)
	require.Equal(
		t,
		"provider_outcome_unknown_after_lease_expiry",
		ambiguous.ErrorCode,
	)
	require.Empty(t, ambiguous.RequestCiphertext)
	require.Nil(t, ambiguous.ChargeFact)

	recoveryJob, _, err := store.CreateOrGet(
		ctx,
		mediaCreateRecord(scope, keyID, "8", now.Add(40*time.Second), 3),
	)
	require.NoError(t, err)
	claimed, err = store.Claim(ctx, "worker-success-crash", now.Add(41*time.Second), 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, recoveryJob.JobID, claimed.JobID)
	recoveryLease := leaseFromClaimed(*claimed)
	require.NoError(t, store.MarkRunning(ctx, recoveryLease, now.Add(42*time.Second)))
	require.NoError(t, store.MarkProviderSucceeded(
		ctx, recoveryLease, "gpt-image-1", now.Add(43*time.Second),
	))
	claimed, err = store.Claim(ctx, "worker-recovery-only", now.Add(47*time.Second), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, recoveryJob.JobID, claimed.JobID)
	require.Equal(t, OutcomeSucceeded, claimed.Lifecycle.Outcome)
	require.Empty(t, claimed.RequestCiphertext)
}

func TestPostgresStoreProjectAdmissionCapacityPreservesIdempotentReplay(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	orgID, projectID, keyID := seedMediaScope(t, pool)
	scope := Scope{OrgID: orgID, ProjectID: projectID}
	now := time.Date(2026, 7, 31, 15, 0, 0, 0, time.UTC)

	var first Job
	for index := 0; index < maxOutstandingJobsPerProject; index++ {
		suffix := fmt.Sprintf("%x", index)
		created, reused, createErr := store.CreateOrGet(
			ctx,
			mediaCreateRecord(scope, keyID, suffix, now.Add(time.Duration(index)*time.Second), 3),
		)
		require.NoError(t, createErr)
		require.False(t, reused)
		if index == 0 {
			first = created
		}
	}

	_, _, err = store.CreateOrGet(
		ctx,
		mediaCreateRecord(scope, keyID, "a", now.Add(20*time.Second), 3),
	)
	require.ErrorIs(t, err, ErrUnavailable)
	var contractErr *Error
	require.ErrorAs(t, err, &contractErr)
	require.Equal(t, "project_outstanding_limit", contractErr.Code)

	replayed, reused, err := store.CreateOrGet(
		ctx,
		mediaCreateRecord(scope, keyID, "0", now.Add(30*time.Second), 3),
	)
	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, first.JobID, replayed.JobID)

	pausedReplay := mediaCreateRecord(scope, keyID, "0", now.Add(31*time.Second), 3)
	pausedReplay.AllowCreate = false
	replayed, reused, err = store.CreateOrGet(ctx, pausedReplay)
	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, first.JobID, replayed.JobID)

	pausedNew := mediaCreateRecord(scope, keyID, "b", now.Add(32*time.Second), 3)
	pausedNew.AllowCreate = false
	_, _, err = store.CreateOrGet(ctx, pausedNew)
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorAs(t, err, &contractErr)
	require.Equal(t, "async_media_admission_disabled", contractErr.Code)
}

func TestPostgresStoreIdempotencyUsesLogicalCredentialAcrossAtomicRotationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)

	var orgID, projectID, subjectID, adminID, requestID, grantID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('async-media-family-idempotency','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'async-media-family-admin','hash','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&adminID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'async-media-family-user','hash','member','developer','active') RETURNING id`, orgID).Scan(&subjectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status,budget_period)
		VALUES ($1,'Async Media Family','async-media-family-idempotency','active','none') RETURNING id`, orgID).Scan(&projectID))
	_, err = pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, subjectID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
		  subject_user_id,request_kind,requested_project_role,requested_scope_mode,
		  requested_models,requested_endpoints,environment,submission_mode,status,
		  decision_mode,policy_reason_codes,decision_reason_codes,policy_version,
		  submitted_by,decided_by,decided_at
		) VALUES (
		  'req_async_media_family_0001','async-media-family',repeat('a',64),$1,$2,'user',$3,
		  'human_personal','project_user','explicit','["company-image"]','["/v1/media/jobs"]',
		  'personal','direct_grant','approved','direct',ARRAY['special_endpoint'],
		  ARRAY['direct_grant_authorized_nonproduction'],'unirouter.enterprise-access-policy.v1',
		  $4,$4,$5
		) RETURNING id`, orgID, projectID, subjectID, adminID, now).Scan(&requestID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_grants (
		  grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,
		  project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,environment,
		  status,valid_from,decision_mode,policy_reason_codes,decision_reason_codes,
		  policy_version,semantic_hash,created_by,updated_by
		) VALUES (
		  'grant_async_media_family_0001',$1,$2,$3,'user',$4,'project_user','explicit',
		  '["company-image"]','["/v1/media/jobs"]','personal','active',$5,'direct',
		  ARRAY['special_endpoint'],ARRAY['direct_grant_authorized_nonproduction'],
		  'unirouter.enterprise-access-policy.v1',repeat('a',64),$6,$6
		) RETURNING id`, requestID, orgID, projectID, subjectID, now, adminID).Scan(&grantID))

	insertFamily := func(name, credentialRef, hashChar, prefix string) (int64, int64) {
		t.Helper()
		var credentialID, keyID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO access_credentials (
			  credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,
			  allowed_models,allowed_endpoints,rpm_limit,rps_limit,created_by
			) VALUES ($1,$2,$3,$4,$5,'','company-image','/v1/media/jobs',60,10,$6)
			RETURNING id`, credentialRef, grantID, orgID, projectID, name, subjectID).Scan(&credentialID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO api_keys (
			  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
			  allowed_ips,allowed_models,allowed_endpoints,rpm_limit,qps_limit,purpose,
			  access_grant_id,access_credential_id,credential_ref
			) VALUES ($1,$2,$3,'personal',$4,$5,$6,'active','','company-image',
			  '/v1/media/jobs',60,10,'',$7,$8,$9) RETURNING id`,
			orgID, projectID, subjectID, name, strings.Repeat(hashChar, 64), prefix,
			grantID, credentialID, credentialRef).Scan(&keyID))
		return credentialID, keyID
	}

	credentialOneID, predecessorID := insertFamily(
		"media family one", "cred_async_media_family_one", "1", "ur-media-family-1",
	)
	credentialTwoID, otherFamilyKeyID := insertFamily(
		"media family two", "cred_async_media_family_two", "2", "ur-media-family-2",
	)

	scope := Scope{OrgID: orgID, ProjectID: projectID}
	secret := []byte(strings.Repeat("m", 32))
	firstHash, err := AccessIdempotencyHash(secret, scope, predecessorID, credentialOneID, "media-operation")
	require.NoError(t, err)
	firstRecord := mediaCreateRecord(scope, predecessorID, "a", now, 3)
	firstRecord.Job.AccessGrantID = positiveInt64Pointer(grantID)
	firstRecord.Job.AccessCredentialID = positiveInt64Pointer(credentialOneID)
	firstRecord.Job.CredentialRef = "cred_async_media_family_one"
	firstRecord.Job.IdempotencyHash = firstHash
	first, reused, err := store.CreateOrGet(ctx, firstRecord)
	require.NoError(t, err)
	require.False(t, reused)
	require.Equal(t, predecessorID, first.APIKeyID)

	_, err = pool.Exec(ctx, `UPDATE api_keys SET status='disabled',auth_epoch=auth_epoch+1,updated_at=$2 WHERE id=$1`, predecessorID, now.Add(time.Second))
	require.NoError(t, err)
	var successorID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
		  allowed_ips,allowed_models,allowed_endpoints,rpm_limit,qps_limit,purpose,
		  access_grant_id,access_credential_id,credential_ref,rotated_from_key_id
		) VALUES ($1,$2,$3,'personal','media family one',$4,'ur-media-family-1b','active',
		  '','company-image','/v1/media/jobs',60,10,'',$5,$6,
		  'cred_async_media_family_one',$7) RETURNING id`,
		orgID, projectID, subjectID, strings.Repeat("3", 64), grantID, credentialOneID, predecessorID).Scan(&successorID))
	successorHash, err := AccessIdempotencyHash(secret, scope, successorID, credentialOneID, "media-operation")
	require.NoError(t, err)
	require.Equal(t, firstHash, successorHash)
	successorRecord := mediaCreateRecord(scope, successorID, "b", now.Add(time.Second), 3)
	successorRecord.Job.AccessGrantID = positiveInt64Pointer(grantID)
	successorRecord.Job.AccessCredentialID = positiveInt64Pointer(credentialOneID)
	successorRecord.Job.CredentialRef = "cred_async_media_family_one"
	successorRecord.Job.IdempotencyHash = successorHash
	successorRecord.Job.RequestSHA256 = firstRecord.Job.RequestSHA256
	replayed, reused, err := store.CreateOrGet(ctx, successorRecord)
	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, first.JobID, replayed.JobID)
	require.Equal(t, predecessorID, replayed.APIKeyID, "the original physical admission evidence must be retained")
	require.Equal(t, credentialOneID, *replayed.AccessCredentialID)

	otherFamilyHash, err := AccessIdempotencyHash(secret, scope, otherFamilyKeyID, credentialTwoID, "media-operation")
	require.NoError(t, err)
	require.NotEqual(t, firstHash, otherFamilyHash)
	otherFamilyRecord := mediaCreateRecord(scope, otherFamilyKeyID, "c", now.Add(2*time.Second), 3)
	otherFamilyRecord.Job.AccessGrantID = positiveInt64Pointer(grantID)
	otherFamilyRecord.Job.AccessCredentialID = positiveInt64Pointer(credentialTwoID)
	otherFamilyRecord.Job.CredentialRef = "cred_async_media_family_two"
	otherFamilyRecord.Job.IdempotencyHash = otherFamilyHash
	_, reused, err = store.CreateOrGet(ctx, otherFamilyRecord)
	require.NoError(t, err)
	require.False(t, reused)

	insertLegacyKey := func(hashChar, prefix string) int64 {
		t.Helper()
		var keyID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO api_keys (
			  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
			  allowed_models,rpm_limit,qps_limit,purpose
			) VALUES ($1,$2,$3,'personal',$4,$5,$6,'active','company-image',60,10,'')
			RETURNING id`, orgID, projectID, subjectID, "legacy "+prefix,
			strings.Repeat(hashChar, 64), prefix).Scan(&keyID))
		return keyID
	}
	legacyOneID := insertLegacyKey("4", "ur-media-legacy-1")
	legacyTwoID := insertLegacyKey("5", "ur-media-legacy-2")
	legacyOneHash, err := IdempotencyHash(secret, scope, legacyOneID, "legacy-operation")
	require.NoError(t, err)
	legacyFirstRecord := mediaCreateRecord(scope, legacyOneID, "d", now.Add(3*time.Second), 3)
	legacyFirstRecord.Job.IdempotencyHash = legacyOneHash
	legacyFirst, reused, err := store.CreateOrGet(ctx, legacyFirstRecord)
	require.NoError(t, err)
	require.False(t, reused)
	legacyReplayRecord := mediaCreateRecord(scope, legacyOneID, "e", now.Add(4*time.Second), 3)
	legacyReplayRecord.Job.IdempotencyHash = legacyOneHash
	legacyReplayRecord.Job.RequestSHA256 = legacyFirstRecord.Job.RequestSHA256
	legacyReplay, reused, err := store.CreateOrGet(ctx, legacyReplayRecord)
	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, legacyFirst.JobID, legacyReplay.JobID)

	legacyTwoHash, err := IdempotencyHash(secret, scope, legacyTwoID, "legacy-operation")
	require.NoError(t, err)
	require.NotEqual(t, legacyOneHash, legacyTwoHash)
	legacyTwoRecord := mediaCreateRecord(scope, legacyTwoID, "f", now.Add(5*time.Second), 3)
	legacyTwoRecord.Job.IdempotencyHash = legacyTwoHash
	_, reused, err = store.CreateOrGet(ctx, legacyTwoRecord)
	require.NoError(t, err)
	require.False(t, reused)

	var jobCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM async_media_jobs WHERE org_id=$1`, orgID).Scan(&jobCount))
	require.Equal(t, 4, jobCount)
}

func TestPostgresStoreCommitReauthorizationSerializesGrantAndSponsorInvalidationIntegration(t *testing.T) {
	tests := []struct {
		name       string
		transition func(context.Context, *testing.T, *pgxpool.Pool, mediaCommitFixture)
	}{
		{
			name: "grant revoke",
			transition: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture mediaCommitFixture) {
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer tx.Rollback(ctx) //nolint:errcheck
				require.NoError(t, principallock.LockUsers(ctx, tx, fixture.adminID, fixture.sponsorID))
				require.NoError(t, principallock.LockWorkloads(ctx, tx, fixture.workloadID))
				_, err = accessgrantlifecycle.Change(ctx, tx, fixture.grantID, 1, fixture.adminID,
					accessgrantlifecycle.ActionRevoke, accessgrantlifecycle.ReasonManualRevoke, fixture.now)
				require.NoError(t, err)
				runBlockedMediaAdmission(t, ctx, pool, fixture, func() { require.NoError(t, tx.Commit(ctx)) })
			},
		},
		{
			name: "sponsor disabled corruption backstop",
			transition: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture mediaCommitFixture) {
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer tx.Rollback(ctx) //nolint:errcheck
				require.NoError(t, principallock.LockUser(ctx, tx, fixture.sponsorID))
				_, err = tx.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, fixture.sponsorID)
				require.NoError(t, err)
				runBlockedMediaAdmission(t, ctx, pool, fixture, func() { require.NoError(t, tx.Commit(ctx)) })
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			fixture := seedMediaCommitFixture(t, pool)
			test.transition(context.Background(), t, pool, fixture)
		})
	}
}

func TestQueuedExecutionFollowsLogicalCredentialAndReauthorizesBeforeDispatchIntegration(t *testing.T) {
	t.Run("atomic secret replacement keeps the accepted family executable", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		ctx := context.Background()
		fixture := seedMediaCommitFixture(t, pool)
		store, err := NewPostgresStore(pool, ExecutionModelAuthorizerFunc(
			func(context.Context, db.DBQuerier, ExecutionAuthorization) error { return nil },
		))
		require.NoError(t, err)
		job, lease, expected := createClaimedMediaExecution(t, ctx, store, fixture, "a")

		_, err = pool.Exec(ctx, `
			UPDATE api_keys
			   SET status='disabled',auth_epoch=auth_epoch+1,updated_at=$2
			 WHERE id=$1 AND status='active'`, fixture.apiKeyID, fixture.now.Add(time.Second))
		require.NoError(t, err)
		var successorID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO api_keys (
			  org_id,project_id,workload_application_id,key_type,name,key_hash,key_prefix,status,
			  allowed_models,allowed_endpoints,rpm_limit,qps_limit,concurrency_limit,purpose,
			  access_grant_id,access_credential_id,credential_ref,rotated_from_key_id
			) VALUES ($1,$2,$3,'service','media commit credential',repeat('d',64),'ur-media-rotated','active',
			  'company-image','/v1/media/jobs',60,10,1,'',$4,$5,$6,$7)
			RETURNING id`,
			fixture.orgID, fixture.projectID, fixture.workloadID, fixture.grantID,
			fixture.credentialID, fixture.credentialRef, fixture.apiKeyID,
		).Scan(&successorID))

		operationID := "media-execution-family-rotation"
		attached, reused, err := store.AttachExecutionReservation(
			ctx, lease, expected, &limitpolicy.DurableReservation{OperationID: operationID},
			operationID, fixture.now.Add(time.Hour), fixture.now.Add(2*time.Second),
		)
		require.NoError(t, err)
		require.False(t, reused)
		require.Equal(t, 1, attached.ProviderAttempt)
		require.NotEqual(t, fixture.apiKeyID, successorID)
		persisted, err := store.Get(ctx, job.Scope, job.JobID)
		require.NoError(t, err)
		require.Equal(t, fixture.apiKeyID, persisted.APIKeyID, "physical admission evidence remains frozen")
		require.Equal(t, 1, persisted.Attempt)
	})

	transitions := []struct {
		name       string
		transition func(context.Context, *testing.T, *pgxpool.Pool, mediaCommitFixture) func()
	}{
		{
			name: "grant revoke",
			transition: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture mediaCommitFixture) func() {
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				require.NoError(t, principallock.LockUsers(ctx, tx, fixture.adminID, fixture.sponsorID))
				require.NoError(t, principallock.LockWorkloads(ctx, tx, fixture.workloadID))
				_, err = accessgrantlifecycle.Change(ctx, tx, fixture.grantID, 1, fixture.adminID,
					accessgrantlifecycle.ActionRevoke, accessgrantlifecycle.ReasonManualRevoke, fixture.now)
				require.NoError(t, err)
				return func() { require.NoError(t, tx.Commit(ctx)) }
			},
		},
		{
			name: "sponsor disabled",
			transition: func(ctx context.Context, t *testing.T, pool *pgxpool.Pool, fixture mediaCommitFixture) func() {
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				require.NoError(t, principallock.LockUser(ctx, tx, fixture.sponsorID))
				_, err = tx.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, fixture.sponsorID)
				require.NoError(t, err)
				return func() { require.NoError(t, tx.Commit(ctx)) }
			},
		},
	}
	for _, transition := range transitions {
		t.Run(transition.name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := context.Background()
			fixture := seedMediaCommitFixture(t, pool)
			store, err := NewPostgresStore(pool, ExecutionModelAuthorizerFunc(
				func(context.Context, db.DBQuerier, ExecutionAuthorization) error { return nil },
			))
			require.NoError(t, err)
			job, lease, expected := createClaimedMediaExecution(t, ctx, store, fixture, "b")
			commit := transition.transition(ctx, t, pool, fixture)

			result := make(chan error, 1)
			go func() {
				operationID := "media-execution-blocked-" + strings.ReplaceAll(transition.name, " ", "-")
				_, _, attachErr := store.AttachExecutionReservation(
					ctx, lease, expected, &limitpolicy.DurableReservation{OperationID: operationID},
					operationID, fixture.now.Add(time.Hour), fixture.now.Add(time.Second),
				)
				result <- attachErr
			}()
			select {
			case attachErr := <-result:
				t.Fatalf("execution dispatch escaped the transition lock before commit: %v", attachErr)
			case <-time.After(150 * time.Millisecond):
			}
			commit()
			select {
			case attachErr := <-result:
				require.ErrorIs(t, attachErr, ErrForbidden)
			case <-time.After(5 * time.Second):
				t.Fatal("execution dispatch remained blocked after transition commit")
			}
			var reservations int
			require.NoError(t, pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM async_media_limit_reservations WHERE job_id=$1`, job.JobID,
			).Scan(&reservations))
			require.Zero(t, reservations)
			persisted, err := store.Get(ctx, job.Scope, job.JobID)
			require.NoError(t, err)
			require.Zero(t, persisted.Attempt)
			require.Equal(t, ExecutionPending, persisted.Lifecycle.Execution)
		})
	}

	t.Run("live model policy denial prevents the dispatch fence", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		ctx := context.Background()
		fixture := seedMediaCommitFixture(t, pool)
		deny := false
		store, err := NewPostgresStore(pool, ExecutionModelAuthorizerFunc(
			func(context.Context, db.DBQuerier, ExecutionAuthorization) error {
				if deny {
					return ErrForbidden
				}
				return nil
			},
		))
		require.NoError(t, err)
		job, lease, expected := createClaimedMediaExecution(t, ctx, store, fixture, "c")
		deny = true
		operationID := "media-execution-model-denied"
		_, _, err = store.AttachExecutionReservation(
			ctx, lease, expected, &limitpolicy.DurableReservation{OperationID: operationID},
			operationID, fixture.now.Add(time.Hour), fixture.now.Add(time.Second),
		)
		require.ErrorIs(t, err, ErrForbidden)
		var reservations int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM async_media_limit_reservations WHERE job_id=$1`, job.JobID,
		).Scan(&reservations))
		require.Zero(t, reservations)
		persisted, err := store.Get(ctx, job.Scope, job.JobID)
		require.NoError(t, err)
		require.Zero(t, persisted.Attempt)
	})
}

func createClaimedMediaExecution(
	t *testing.T,
	ctx context.Context,
	store *PostgresStore,
	fixture mediaCommitFixture,
	suffix string,
) (Job, Lease, limitpolicy.Subject) {
	t.Helper()
	record := mediaCreateRecord(
		Scope{OrgID: fixture.orgID, ProjectID: fixture.projectID}, fixture.apiKeyID,
		suffix[:1], fixture.now, 3,
	)
	record.Job.AccessGrantID = positiveInt64Pointer(fixture.grantID)
	record.Job.AccessCredentialID = positiveInt64Pointer(fixture.credentialID)
	record.Job.CredentialRef = fixture.credentialRef
	job, reused, err := store.CreateOrGet(ctx, record)
	require.NoError(t, err)
	require.False(t, reused)
	claimed, err := store.Claim(ctx, "execution-"+suffix, fixture.now.Add(time.Second), time.Hour)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, job.JobID, claimed.JobID)
	lease := leaseFromClaimed(*claimed)
	expected, err := store.ExecutionLimitSubject(ctx, lease)
	require.NoError(t, err)
	return job, lease, expected
}

func TestProviderAttemptStartsOnlyAfterExecutionAdmissionIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := seedMediaCommitFixture(t, pool)
	store, err := NewPostgresStore(pool, ExecutionModelAuthorizerFunc(
		func(context.Context, db.DBQuerier, ExecutionAuthorization) error { return nil },
	))
	require.NoError(t, err)
	scope := Scope{OrgID: fixture.orgID, ProjectID: fixture.projectID}
	now := fixture.now.Add(time.Minute)

	createBound := func(suffix string, createdAt time.Time) Job {
		t.Helper()
		record := mediaCreateRecord(scope, fixture.apiKeyID, suffix, createdAt, 3)
		record.Job.AccessGrantID = positiveInt64Pointer(fixture.grantID)
		record.Job.AccessCredentialID = positiveInt64Pointer(fixture.credentialID)
		record.Job.CredentialRef = fixture.credentialRef
		job, reused, createErr := store.CreateOrGet(ctx, record)
		require.NoError(t, createErr)
		require.False(t, reused)
		return job
	}
	attach := func(job Job, at time.Time) (Lease, ExecutionReservation) {
		t.Helper()
		lease := leaseFromClaimed(job)
		subject, subjectErr := store.ExecutionLimitSubject(ctx, lease)
		require.NoError(t, subjectErr)
		operationID := fmt.Sprintf("media-execution-%s-%d", job.JobID[len(job.JobID)-4:], job.LeaseFence)
		attached, reused, attachErr := store.AttachExecutionReservation(
			ctx, lease, subject,
			&limitpolicy.DurableReservation{OperationID: operationID},
			operationID, at.Add(30*time.Minute), at,
		)
		require.NoError(t, attachErr)
		require.False(t, reused)
		return lease, attached
	}

	first := createBound("a", now)
	second := createBound("b", now.Add(time.Second))
	claimedFirst, err := store.Claim(ctx, "attempt-worker-1", now.Add(2*time.Second), time.Hour)
	require.NoError(t, err)
	require.Equal(t, first.JobID, claimedFirst.JobID)
	require.Zero(t, claimedFirst.Attempt, "queue claim must not consume a Provider attempt")
	firstLease, firstExecution := attach(*claimedFirst, now.Add(3*time.Second))
	require.Equal(t, 1, firstExecution.ProviderAttempt)

	// The first execution holds the configured concurrency=1 lane. Model the
	// second job's pre-dispatch capacity waits by repeatedly returning its lease
	// to the queue. More waits than max_attempts must remain legal because none
	// reached the Provider dispatch fence.
	for wait := 0; wait < second.MaxAttempts+2; wait++ {
		at := now.Add(time.Duration(4+wait*2) * time.Second)
		claimed, claimErr := store.Claim(ctx, fmt.Sprintf("attempt-wait-%d", wait), at, time.Hour)
		require.NoError(t, claimErr)
		require.NotNil(t, claimed)
		require.Equal(t, second.JobID, claimed.JobID)
		require.Zero(t, claimed.Attempt)
		requeued, requeueErr := store.Requeue(
			ctx, leaseFromClaimed(*claimed), "limit_policy.concurrency_exceeded", at.Add(time.Second), at.Add(2*time.Second),
		)
		require.NoError(t, requeueErr)
		require.Zero(t, requeued.Attempt)
		require.Equal(t, ExecutionPending, requeued.Lifecycle.Execution)
	}

	_, err = store.CompleteFailure(ctx, firstLease, "fixture_execution_complete", now.Add(30*time.Second))
	require.NoError(t, err)
	claimedSecond, err := store.Claim(ctx, "attempt-worker-2", now.Add(31*time.Second), time.Hour)
	require.NoError(t, err)
	require.NotNil(t, claimedSecond)
	require.Equal(t, second.JobID, claimedSecond.JobID)
	require.Zero(t, claimedSecond.Attempt)
	secondLease, secondExecution := attach(*claimedSecond, now.Add(32*time.Second))
	require.Equal(t, 1, secondExecution.ProviderAttempt, "first real dispatch after capacity release must be attempt 1")

	// Only a retryable failure after the dispatch fence consumes the attempt.
	retried, err := store.Requeue(
		ctx, Lease{
			JobID: secondLease.JobID, Scope: secondLease.Scope, Owner: secondLease.Owner,
			Fence: secondLease.Fence, Attempt: secondExecution.ProviderAttempt,
			Job: secondLease.Job,
		},
		"provider_http_503_channel_failure", now.Add(33*time.Second), now.Add(34*time.Second),
	)
	require.NoError(t, err)
	require.Equal(t, 1, retried.Attempt)
	claimedRetry, err := store.Claim(ctx, "attempt-worker-3", now.Add(35*time.Second), time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, claimedRetry.Attempt)
	_, retryExecution := attach(*claimedRetry, now.Add(36*time.Second))
	require.Equal(t, 2, retryExecution.ProviderAttempt)

	// Cancellation and live-authority loss before Attach never create a
	// Provider attempt.
	_, err = store.CompleteFailure(ctx, leaseFromClaimed(*claimedRetry), "fixture_done", now.Add(37*time.Second))
	require.NoError(t, err)
	cancelJob := createBound("c", now.Add(38*time.Second))
	claimedCancel, err := store.Claim(ctx, "attempt-worker-cancel", now.Add(39*time.Second), time.Hour)
	require.NoError(t, err)
	require.Equal(t, cancelJob.JobID, claimedCancel.JobID)
	cancelSubject, err := store.ExecutionLimitSubject(ctx, leaseFromClaimed(*claimedCancel))
	require.NoError(t, err)
	_, err = store.RequestCancellation(ctx, scope, cancelJob.JobID, now.Add(40*time.Second))
	require.NoError(t, err)
	cancelOperationID := "media-execution-cancel-before-dispatch"
	_, _, err = store.AttachExecutionReservation(
		ctx, leaseFromClaimed(*claimedCancel), cancelSubject,
		&limitpolicy.DurableReservation{OperationID: cancelOperationID},
		cancelOperationID, now.Add(time.Hour), now.Add(41*time.Second),
	)
	require.ErrorIs(t, err, ErrCancelled)
	cancelled, err := store.Get(ctx, scope, cancelJob.JobID)
	require.NoError(t, err)
	require.Zero(t, cancelled.Attempt)

	authorityJob := createBound("d", now.Add(42*time.Second))
	claimedAuthority, err := store.Claim(ctx, "attempt-worker-authority", now.Add(43*time.Second), time.Hour)
	require.NoError(t, err)
	require.Equal(t, authorityJob.JobID, claimedAuthority.JobID)
	_, err = pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, fixture.sponsorID)
	require.NoError(t, err)
	_, err = store.ExecutionLimitSubject(ctx, leaseFromClaimed(*claimedAuthority))
	require.ErrorIs(t, err, ErrForbidden)
	authorityChanged, err := store.Get(ctx, scope, authorityJob.JobID)
	require.NoError(t, err)
	require.Zero(t, authorityChanged.Attempt)
}

func TestExecutionTerminalClaimIsFencedAndExpiresDeterministicallyIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := seedMediaCommitFixture(t, pool)
	store, err := NewPostgresStore(pool, ExecutionModelAuthorizerFunc(
		func(context.Context, db.DBQuerier, ExecutionAuthorization) error { return nil },
	))
	require.NoError(t, err)
	scope := Scope{OrgID: fixture.orgID, ProjectID: fixture.projectID}
	now := fixture.now.Add(2 * time.Minute)

	createAndTerminalize := func(suffix, operationID string, expiresAt time.Time) (Job, int64) {
		t.Helper()
		record := mediaCreateRecord(scope, fixture.apiKeyID, suffix, now, 3)
		record.Job.AccessGrantID = positiveInt64Pointer(fixture.grantID)
		record.Job.AccessCredentialID = positiveInt64Pointer(fixture.credentialID)
		record.Job.CredentialRef = fixture.credentialRef
		job, reused, createErr := store.CreateOrGet(ctx, record)
		require.NoError(t, createErr)
		require.False(t, reused)
		claimed, claimErr := store.Claim(ctx, "terminal-claim-"+suffix, now.Add(time.Second), time.Hour)
		require.NoError(t, claimErr)
		require.Equal(t, job.JobID, claimed.JobID)
		lease := leaseFromClaimed(*claimed)
		subject, subjectErr := store.ExecutionLimitSubject(ctx, lease)
		require.NoError(t, subjectErr)
		attached, _, attachErr := store.AttachExecutionReservation(
			ctx, lease, subject, &limitpolicy.DurableReservation{OperationID: operationID},
			operationID, expiresAt, now.Add(2*time.Second),
		)
		require.NoError(t, attachErr)
		lease.Attempt = attached.ProviderAttempt
		lease.Job.Attempt = attached.ProviderAttempt
		_, completeErr := store.CompleteFailure(ctx, lease, "fixture_terminal", now.Add(3*time.Second))
		require.NoError(t, completeErr)
		return job, lease.Fence
	}

	job, fence := createAndTerminalize("e", "media-terminal-fenced", now.Add(time.Hour))
	firstClaim, err := store.PendingExecutionTerminal(
		ctx, job.JobID, fence, "terminal-owner-one", now.Add(4*time.Second), 15*time.Second,
	)
	require.NoError(t, err)
	require.NotNil(t, firstClaim)
	secondClaim, err := store.PendingExecutionTerminal(
		ctx, job.JobID, fence, "terminal-owner-two", now.Add(5*time.Second), 15*time.Second,
	)
	require.NoError(t, err)
	require.Nil(t, secondClaim, "an unexpired terminal claim must have one consumer")
	stale := *firstClaim
	stale.TerminalFence--
	require.ErrorIs(t, store.MarkExecutionTerminalized(ctx, stale, now.Add(6*time.Second), "cancelled"), ErrLeaseLost)
	require.NoError(t, store.ReleaseExecutionTerminalClaim(ctx, *firstClaim, now.Add(7*time.Second), "redis unavailable"))
	secondClaim, err = store.PendingExecutionTerminal(
		ctx, job.JobID, fence, "terminal-owner-two", now.Add(8*time.Second), 15*time.Second,
	)
	require.NoError(t, err)
	require.NotNil(t, secondClaim)
	require.Greater(t, secondClaim.TerminalFence, firstClaim.TerminalFence)
	require.NoError(t, store.MarkExecutionTerminalized(ctx, *secondClaim, now.Add(9*time.Second), "cancelled"))
	require.ErrorIs(t, store.MarkExecutionTerminalized(ctx, *secondClaim, now.Add(10*time.Second), "cancelled"), ErrLeaseLost)

	expiredJob, expiredFence := createAndTerminalize("f", "media-terminal-expired", now.Add(5*time.Second))
	worker := &Worker{
		store: store, limits: &mediaLimitRuntimeFixture{}, owner: "terminal-expiry-worker",
		now: func() time.Time { return now.Add(6 * time.Second) },
	}
	reconciled, err := worker.reconcileExecutionTerminal(ctx, expiredJob.JobID, expiredFence)
	require.NoError(t, err)
	require.True(t, reconciled)
	var outcome, lastError string
	var terminalizedAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT terminal_outcome,last_error,terminalized_at
		FROM async_media_limit_reservations WHERE job_id=$1 AND lease_fence=$2`,
		expiredJob.JobID, expiredFence,
	).Scan(&outcome, &lastError, &terminalizedAt))
	require.Equal(t, "operation_state_expired", outcome)
	require.Equal(t, "operation_state_expired", lastError)
	require.NotNil(t, terminalizedAt)
}

type mediaCommitFixture struct {
	now                                         time.Time
	orgID, projectID, adminID, sponsorID        int64
	workloadID, grantID, credentialID, apiKeyID int64
	credentialRef                               string
}

func seedMediaCommitFixture(t *testing.T, pool *pgxpool.Pool) mediaCommitFixture {
	t.Helper()
	ctx := context.Background()
	fixture := mediaCommitFixture{now: time.Now().UTC().Truncate(time.Microsecond), credentialRef: "cred_media_commit_boundary"}
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('media-commit-boundary','active','customer') RETURNING id`).Scan(&fixture.orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status,budget_period)
		VALUES ($1,'Media Commit Boundary','media-commit-boundary','active','none') RETURNING id`, fixture.orgID).Scan(&fixture.projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'media-commit-admin','hash','org_admin','org_admin','active') RETURNING id`, fixture.orgID).Scan(&fixture.adminID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'media-commit-sponsor','hash','member','developer','active') RETURNING id`, fixture.orgID).Scan(&fixture.sponsorID))
	_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, fixture.projectID, fixture.sponsorID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO workload_applications (
		  contract_version,identity_contract_version,org_id,project_id,application_ref,logical_ref,
		  display_name,kind,environment,production_use,identity_mode,owner_team,status,
		  environment_locked_at,created_by,updated_by
		) VALUES (
		  'unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,
		  'app_media_commit_boundary','workload_media_commit_boundary','Media commit workload',
		  'service','development',FALSE,'service_credential','platform','active',NOW(),$3,$3
		) RETURNING id`, fixture.orgID, fixture.projectID, fixture.adminID).Scan(&fixture.workloadID))
	var requestID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
		  subject_workload_application_id,request_kind,requested_project_role,requested_scope_mode,
		  requested_models,requested_endpoints,requested_concurrency_limit,environment,submission_mode,sponsor_user_id,status,
		  decision_mode,policy_reason_codes,decision_reason_codes,policy_version,submitted_by,decided_by,decided_at
		) VALUES (
		  'req_media_commit_boundary','media-commit-boundary',repeat('b',64),$1,$2,'workload_application',$3,
		  'workload_service','project_user','explicit','["company-image"]','["/v1/media/jobs"]',1,
		  'development','direct_grant',$4,'approved','direct',ARRAY['special_endpoint'],
		  ARRAY['direct_grant_authorized_nonproduction'],'unirouter.enterprise-access-policy.v1',$5,$5,$6
		) RETURNING id`, fixture.orgID, fixture.projectID, fixture.workloadID, fixture.sponsorID, fixture.adminID, fixture.now).Scan(&requestID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_grants (
		  grant_ref,access_request_id,org_id,project_id,subject_type,subject_workload_application_id,
		  sponsor_user_id,project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,
		  concurrency_limit,environment,production_use,status,valid_from,decision_mode,policy_reason_codes,
		  decision_reason_codes,policy_version,semantic_hash,created_by,updated_by
		) VALUES (
		  'grant_media_commit_boundary',$1,$2,$3,'workload_application',$4,$5,'project_user','explicit',
		  '["company-image"]','["/v1/media/jobs"]',1,'development',FALSE,'active',$6,'direct',
		  ARRAY['special_endpoint'],ARRAY['direct_grant_authorized_nonproduction'],
		  'unirouter.enterprise-access-policy.v1',repeat('b',64),$7,$7
		) RETURNING id`, requestID, fixture.orgID, fixture.projectID, fixture.workloadID,
		fixture.sponsorID, fixture.now, fixture.adminID).Scan(&fixture.grantID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_credentials (
		  credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,allowed_models,
		  allowed_endpoints,rpm_limit,rps_limit,concurrency_limit,created_by
		) VALUES ($1,$2,$3,$4,'media commit credential','','company-image','/v1/media/jobs',60,10,1,$5)
		RETURNING id`, fixture.credentialRef, fixture.grantID, fixture.orgID, fixture.projectID, fixture.adminID).Scan(&fixture.credentialID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,workload_application_id,key_type,name,key_hash,key_prefix,status,
		  allowed_models,allowed_endpoints,rpm_limit,qps_limit,concurrency_limit,purpose,access_grant_id,
		  access_credential_id,credential_ref
		) VALUES ($1,$2,$3,'service','media commit credential',repeat('c',64),'ur-media-commit','active',
		  'company-image','/v1/media/jobs',60,10,1,'',$4,$5,$6) RETURNING id`,
		fixture.orgID, fixture.projectID, fixture.workloadID, fixture.grantID,
		fixture.credentialID, fixture.credentialRef).Scan(&fixture.apiKeyID))
	return fixture
}

func runBlockedMediaAdmission(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture mediaCommitFixture, commit func()) {
	t.Helper()
	store, err := NewPostgresStore(pool)
	require.NoError(t, err)
	record := mediaCreateRecord(Scope{OrgID: fixture.orgID, ProjectID: fixture.projectID}, fixture.apiKeyID, "commit", fixture.now, 3)
	record.Job.AccessGrantID = positiveInt64Pointer(fixture.grantID)
	record.Job.AccessCredentialID = positiveInt64Pointer(fixture.credentialID)
	record.Job.CredentialRef = fixture.credentialRef
	result := make(chan error, 1)
	go func() {
		_, _, createErr := store.CreateOrGet(ctx, record)
		result <- createErr
	}()
	select {
	case err := <-result:
		t.Fatalf("media admission escaped transition lock before commit: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	commit()
	select {
	case err := <-result:
		require.ErrorIs(t, err, ErrForbidden)
	case <-time.After(5 * time.Second):
		t.Fatal("media admission remained blocked after transition commit")
	}
	var jobs int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM async_media_jobs WHERE org_id=$1`, fixture.orgID).Scan(&jobs))
	require.Zero(t, jobs)
}

func seedMediaScope(
	t *testing.T,
	pool *pgxpool.Pool,
) (int64, int64, int64) {
	t.Helper()
	ctx := context.Background()
	var orgID, projectID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('async-media-store', 'active', 'standard')
		RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id, name, slug, status, budget_period)
		VALUES ($1, 'Async media', 'async-media-store', 'active', 'none')
		RETURNING id`,
		orgID,
	).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id, project_id, name, key_hash, key_prefix, status, allowed_models
		) VALUES ($1,$2,'async-media',$3,$4,'active','company-image')
		RETURNING id`,
		orgID, projectID, strings.Repeat("a", 64),
		"ur-media-"+strings.Repeat("a", 8),
	).Scan(&keyID))
	return orgID, projectID, keyID
}

func mediaCreateRecord(
	scope Scope,
	keyID int64,
	suffix string,
	now time.Time,
	maxAttempts int,
) CreateRecord {
	return CreateRecord{
		AllowCreate: true,
		Job: Job{
			ContractVersion: ContractVersion,
			JobID:           "mediajob_" + strings.Repeat(suffix, 32),
			Scope:           scope, APIKeyID: keyID,
			RequestID:      "op-" + suffix,
			Operation:      OperationTextToImage,
			ProfileID:      endpointprotocol.ProfileOpenAIImageGPT,
			RequestedModel: "company-image", RoutingModel: "company-image",
			RequestSHA256:     "sha256:" + strings.Repeat(suffix, 64),
			IdempotencyHash:   "hmac-sha256:" + strings.Repeat(suffix, 64),
			RequestCiphertext: "ciphertext-" + suffix,
			Lifecycle: Lifecycle{
				Admission: AdmissionAccepted, Queue: QueueQueued,
				Lease: LeaseUnleased, Execution: ExecutionPending,
				Outcome: OutcomePending, Cancellation: CancellationNotRequested,
				Object: ObjectPending, Settlement: SettlementNotEligible,
			},
			MaxAttempts: maxAttempts, NextAttemptAt: now,
			CreatedAt: now, UpdatedAt: now,
		},
	}
}

func leaseFromClaimed(job Job) Lease {
	return Lease{
		JobID: job.JobID, Scope: job.Scope,
		Owner: job.LeaseOwner, Fence: job.LeaseFence,
		Attempt: job.Attempt, Job: job,
	}
}

func integrationImageOutcome() endpointprotocol.Outcome {
	return endpointprotocol.Outcome{
		ContractVersion: endpointprotocol.ContractVersion,
		ProfileID:       endpointprotocol.ProfileOpenAIImageGPT,
		ProviderState:   "completed", MappedModel: "gpt-image-1", OutputCount: 1,
		Units: []endpointprotocol.UnitObservation{
			{Class: endpointprotocol.UnitInputTokens, Quantity: "3", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly},
			{Class: endpointprotocol.UnitInputTextTokens, Quantity: "2", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly},
			{Class: endpointprotocol.UnitInputImageTokens, Quantity: "1", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly},
			{Class: endpointprotocol.UnitOutputImageTokens, Quantity: "4", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly},
		},
	}
}
