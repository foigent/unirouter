package mediajob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

type providerTaskStore interface {
	ScheduleProviderStatus(context.Context, Lease, string, time.Time) error
	AuthorizeProviderStatus(context.Context, Lease, time.Time) (*mediaDispatchRoute, error)
}

type providerTaskReader interface {
	CheckProviderTask(context.Context, Job) (ProviderResult, error)
}

// ScheduleProviderStatus binds the exact paid submission and releases the
// lease for a later read. It neither increments attempt nor calls the vendor.
func (s *PostgresStore) ScheduleProviderStatus(ctx context.Context, lease Lease, taskID string, now time.Time) error {
	if !endpointprotocol.ValidMediaTaskID(taskID) || lease.Attempt != 1 {
		return domainError(ErrInvalid, "provider_task_invalid")
	}
	readUntil := now.UTC().Add(endpointprotocol.MediaTaskReadWindow)
	if lease.Job.providerTaskReadUntil != nil {
		readUntil = *lease.Job.providerTaskReadUntil
	}
	nextAt := endpointprotocol.NextMediaTaskStatusRead(now.UTC(), readUntil)
	tag, err := s.db.Exec(ctx, `
		UPDATE async_media_jobs
		   SET provider_task_id=$1, provider_task_read_until=COALESCE(provider_task_read_until,$2),
		       queue_state='queued', lease_state='released',lease_owner='',lease_expires_at=NULL,
		       cancellation_state=CASE WHEN cancellation_state='requested' THEN 'too_late' ELSE cancellation_state END,
		       next_attempt_at=$3,updated_at=$4
		 WHERE job_id=$5 AND org_id=$6 AND project_id=$7
		   AND lease_owner=$8 AND lease_fence=$9 AND lease_expires_at>$4
		   AND queue_state='leased' AND execution_state='running' AND provider_outcome='pending'
		   AND attempt=1 AND single_upstream_attempt AND canonical_dispatch_route IS NOT NULL
		   AND (provider_task_id IS NULL OR provider_task_id=$1)`,
		taskID, readUntil, nextAt, now.UTC(), lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID, lease.Owner, lease.Fence,
	)
	if err != nil {
		return fmt.Errorf("mediajob: persist supplier task acknowledgement: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

// Revalidation does not authorize a new submission. A changed route or a
// revoked project/provider credential cannot forward the old task identity.
func (s *PostgresStore) AuthorizeProviderStatus(ctx context.Context, lease Lease, now time.Time) (*mediaDispatchRoute, error) {
	if s.canonicalRoutes == nil || lease.Job.CanonicalDispatch == nil || !endpointprotocol.ValidMediaTaskID(lease.Job.providerTaskID) {
		return nil, domainError(ErrForbidden, "provider_task_route_missing")
	}
	plan, route, err := s.canonicalRoutes.plan(ctx, lease.Job, lease.Request)
	if err != nil {
		return nil, err
	}
	if route.PublicEvidence == nil || !sameCanonicalMediaRoute(*lease.Job.CanonicalDispatch, *route.PublicEvidence) {
		return nil, domainError(ErrForbidden, "provider_task_route_changed")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	live, err := accessgrantboundary.ResolveExecutionForCommit(ctx, tx, executionBinding(lease.Job))
	if errors.Is(err, accessgrantboundary.ErrInactive) {
		return nil, domainError(ErrForbidden, "execution_authority_changed")
	}
	if err != nil {
		return nil, err
	}
	if err := s.authorizeExecution(ctx, tx, live, lease.Job); err != nil {
		return nil, err
	}
	var accepted string
	err = tx.QueryRow(ctx, `UPDATE async_media_jobs SET provider_status_reads=provider_status_reads+1
	WHERE job_id=$1 AND org_id=$2 AND project_id=$3 AND lease_owner=$4 AND lease_fence=$5 AND lease_expires_at>$6
	AND provider_task_read_until>$6 AND queue_state='leased' AND execution_state='running' AND provider_outcome='pending'
	AND attempt=1 AND provider_task_id=$7 AND provider_status_reads<$8 RETURNING provider_task_id`,
		lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID, lease.Owner, lease.Fence, now.UTC(), lease.Job.providerTaskID, endpointprotocol.MediaTaskMaxStatusReads).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLeaseLost
	}
	if err != nil {
		return nil, err
	}
	if err = s.canonicalRoutes.revalidate(ctx, tx, lease.Job, plan, route); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &mediaDispatchRoute{Route: route, profileID: plan.ExecutionProfileID, profileVersion: plan.ExecutionProfileVersion}, nil
}
