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
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/middleware"
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
