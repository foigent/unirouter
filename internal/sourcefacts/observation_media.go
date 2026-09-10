package sourcefacts

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/mediajob"
)

func (s *Service) mediaAttemptID(job mediajob.Job, attempt int) string {
	return sourceID("unirouter.media-attempt.v1", s.options.SourceInstance, executioncontext.ID(job.Scope.OrgID), executioncontext.ID(job.Scope.ProjectID), job.JobID, strconv.Itoa(attempt))
}
func (s *Service) mediaObservationsTx(ctx context.Context, tx pgx.Tx, job mediajob.Job, stage string, at time.Time) ([]Observation, error) {
	if !s.ObservationsEnabled() || stage == "terminal" {
		return nil, nil
	}
	d := s.observation(job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, RuntimeProfile, "media_job", job.JobID, "request_started", at)
	d.ContextRef = (Document{SourceInstance: s.options.SourceInstance, OrgID: d.OrgID, ProjectID: d.ProjectID, RequestID: job.RequestID, Profile: ContextProfile, Stage: "admission"}).factID()
	d.Runtime = &RuntimeObservation{Operation: job.Operation, StreamMode: "not_observed", Admission: "admitted", Provider: "not_started", LocalDelivery: "not_applicable", Financial: "not_applicable", Capture: "observed", EvidenceRef: job.JobID}
	if stage == "admission" {
		d.CaptureExpected = []string{"request_started", "media_terminal"}
		return []Observation{d}, nil
	}
	var attempt, total int
	var fence int64
	var state string
	if err := tx.QueryRow(ctx, `SELECT attempt,max_attempts,lease_fence,execution_state,updated_at FROM async_media_jobs WHERE job_id=$1 AND org_id=$2 AND project_id=$3`, job.JobID, job.Scope.OrgID, job.Scope.ProjectID).Scan(&attempt, &total, &fence, &state, &at); err != nil {
		return nil, err
	}
	if attempt <= 0 || attempt > total || fence <= 0 || state != mediajob.ExecutionRunning {
		return nil, errors.New("media source dispatch requires committed attempt transition")
	}
	subject := s.mediaAttemptID(job, attempt)
	d.SubjectKind = "media_attempt"
	d.SubjectID = subject
	d.Kind = "media_running"
	d.OccurredAt = executioncontext.Time(at)
	d.RecordedAt = d.OccurredAt
	d.SourceFactID = d.factID()
	d.EventID = d.eventID()
	d.CaptureExpected = []string{"media_running"}
	d.Runtime.Provider = "registered"
	a := s.observation(job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, AttemptProfile, "media_attempt", subject, "media_attempt_registered", at)
	a.ContextRef = d.ContextRef
	a.CaptureExpected = []string{"media_attempt_registered", "media_provider_succeeded", "media_retry_scheduled", "media_result"}
	a.Attempt = &AttemptObservation{AttemptID: subject, Operation: job.Operation, RouteSequence: strconv.Itoa(attempt), RouteTotal: strconv.Itoa(total), Provider: "registered", LocalDelivery: "not_applicable", Financial: "not_applicable", EvidenceRef: job.JobID}
	return []Observation{d, a}, nil
}

// CaptureMediaCommitted observes the original writer's returned value after
// commit. It never changes job, lease, reservation, provider or charge state.
func (s *Service) CaptureMediaCommitted(ctx context.Context, job mediajob.Job, stage string) error {
	if !s.ObservationsEnabled() {
		return nil
	}
	if stage != "terminal" && stage != "provider_succeeded" && stage != "retry_scheduled" {
		return errors.New("invalid committed media observation")
	}
	if stage == "terminal" && !job.Terminal() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ;SET LOCAL lock_timeout='100ms';SET LOCAL statement_timeout='200ms'`); err != nil {
		return err
	}
	provider := "unknown"
	switch job.Lifecycle.Outcome {
	case mediajob.OutcomeSucceeded:
		provider = "accepted"
	case mediajob.OutcomeNotStarted:
		provider = "not_started"
	}
	at := job.UpdatedAt
	contextRef := (Document{SourceInstance: s.options.SourceInstance, OrgID: executioncontext.ID(job.Scope.OrgID), ProjectID: executioncontext.ID(job.Scope.ProjectID), RequestID: job.RequestID, Profile: ContextProfile, Stage: "admission"}).factID()
	docs := []Observation{}
	if job.Terminal() {
		d := s.observation(job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, RuntimeProfile, "media_job", job.JobID, "media_terminal", at)
		d.ContextRef = contextRef
		d.Runtime = &RuntimeObservation{Operation: job.Operation, StreamMode: "not_observed", Admission: "admitted", Provider: provider, LocalDelivery: "not_applicable", Financial: "not_applicable", Capture: "partial", EvidenceRef: job.JobID}
		d.Coverage = "partial"
		docs = append(docs, d)
	}
	if job.Attempt > 0 {
		kind := map[string]string{"terminal": "media_result", "provider_succeeded": "media_provider_succeeded", "retry_scheduled": "media_retry_scheduled"}[stage]
		subject := s.mediaAttemptID(job, job.Attempt)
		d := s.observation(job.Scope.OrgID, job.Scope.ProjectID, job.RequestID, AttemptProfile, "media_attempt", subject, kind, at)
		d.ContextRef = contextRef
		d.Coverage = "partial"
		d.Attempt = &AttemptObservation{AttemptID: subject, Operation: job.Operation, RouteSequence: strconv.Itoa(job.Attempt), RouteTotal: strconv.Itoa(job.MaxAttempts), Provider: provider, LocalDelivery: "not_applicable", Financial: "not_applicable", RetryAuthorized: stage == "retry_scheduled" && job.Lifecycle.Queue == mediajob.QueueQueued && !job.Terminal(), EvidenceRef: job.JobID}
		docs = append(docs, d)
	}
	if len(docs) == 0 {
		return nil
	}
	if err = s.publishTogetherTx(ctx, tx, nil, docs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
