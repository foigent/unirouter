package sourcefacts

import (
	"context"
	"net/http"

	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/mediajob"
)

func (s *ContentCaptureService) ObserveMediaProvider(ctx context.Context, job mediajob.Job, route mediajob.SourceMediaRoute, request *http.Request) (*http.Request, func(*http.Response, error)) {
	unchanged := func(*http.Response, error) {}
	if s == nil || !s.source.ContentEnabled() || validateMediaRoute(job, route) != nil || request == nil || job.LeaseFence <= 0 || route.ProtocolProfile != endpointprotocol.ProfileOpenAIImageGPT {
		return request, unchanged
	}
	checkCtx, cancel := context.WithTimeout(ctx, s.config.StartTimeout)
	defer cancel()
	// The original worker has already committed AttachExecutionReservation.
	// This read verifies its identity; it does not grant a second dispatch lease.
	// C-only must not depend on B's execution_source_facts registration.
	var matches bool
	err := s.pool.QueryRow(checkCtx, `SELECT EXISTS(SELECT 1 FROM async_media_jobs
WHERE job_id=$1 AND org_id=$2 AND project_id=$3 AND request_id=$4 AND operation=$5
AND attempt=$6 AND max_attempts=$7 AND lease_fence=$8 AND lease_owner=$9
AND lease_expires_at>clock_timestamp() AND queue_state='leased'
AND execution_state='running' AND provider_outcome='pending')`, job.JobID, job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, job.Operation, job.Attempt, job.MaxAttempts, job.LeaseFence, job.LeaseOwner).Scan(&matches)
	if err != nil || !matches {
		return request, unchanged
	}
	// The accepted job binds the key ID; these fields are hints only. Begin and
	// Reserve recheck the live key, owner and project authority transactionally.
	principal := contentcapture.CapturePrincipal{APIKeyID: job.APIKeyID}
	if err := s.pool.QueryRow(checkCtx, `SELECT COALESCE(NULLIF(key_type,''),'shared'),COALESCE(owner_user_id,0)
FROM api_keys WHERE id=$1 AND org_id=$2 AND project_id=$3`, job.APIKeyID, job.Scope.OrgID, job.Scope.ProjectID).Scan(&principal.KeyType, &principal.OwnerUserID); err != nil {
		return request, unchanged
	}
	id := contentcapture.Observation{OrgID: job.Scope.OrgID, ProjectID: job.Scope.ProjectID, RequestID: job.RequestID, Protocol: route.ProtocolProfile, AttemptID: s.source.mediaRouteID(job, route), Principal: principal}
	return contentcapture.ObserveProvider(s, id, request)
}
