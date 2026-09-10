package mediajob

import (
	"cmp"
	"context"
	"log"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/executioncontext"
)

type SourceMaterializer interface {
	MaterializeMediaStageTx(context.Context, pgx.Tx, Job, string, *executioncontext.Snapshot) error
}

func (s *PostgresStore) SetSourceMaterializer(materializer SourceMaterializer) {
	s.sourceMaterializer = materializer
}

func mediaSourceSnapshot(job Job, live accessgrantboundary.Snapshot, now time.Time) executioncontext.Snapshot {
	key := live.APIKey
	value := executioncontext.Snapshot{SchemaVersion: executioncontext.Version, Core: executioncontext.Core{
		RequestID: job.RequestID, OrgID: executioncontext.ID(job.Scope.OrgID), ProjectID: executioncontext.ID(job.Scope.ProjectID), APIKeyID: executioncontext.ID(key.ID), KeyType: key.KeyType,
		CredentialRef: key.CredentialRef, AuthEpoch: strconv.FormatInt(key.AuthEpoch, 10), AuthKind: "api_key", Operation: job.Operation, Protocol: "unirouter.async-media.v1", CapturedAt: executioncontext.Time(now)}, Identity: live.ExecutionIdentity()}
	value.Identity.AuthorityRead = "live_commit_boundary"
	if key.AccessCredentialID != nil {
		value.Core.CredentialID = executioncontext.ID(*key.AccessCredentialID)
		value.Core.AuthKind = "access_grant"
	}
	if key.WorkloadApplicationID != nil {
		value.Core.WorkloadApplicationID = executioncontext.ID(*key.WorkloadApplicationID)
	}
	return value
}

func (s *PostgresStore) sourceStageTx(ctx context.Context, tx pgx.Tx, job Job, stage string, snapshot *executioncontext.Snapshot) error {
	if s.sourceMaterializer == nil || stage == "terminal" && !job.Terminal() {
		return nil
	}
	return s.sourceMaterializer.MaterializeMediaStageTx(ctx, tx, job, stage, snapshot)
}

func (s *PostgresStore) sourceTerminalTx(ctx context.Context, tx pgx.Tx, scope Scope, jobID string) error {
	if s.sourceMaterializer == nil {
		return nil
	}
	job, e := getJobTx(ctx, tx, scope, jobID, false)
	if e != nil {
		return e
	}
	return s.sourceStageTx(ctx, tx, job, "terminal", nil)
}

type sourceTerminalJob struct {
	scope Scope
	id    string
}

// Capture exactly the rows changed by the existing expiry writer. This is not
// an updated_at poll, and publication waits until all business locks are held.
func (s *PostgresStore) updateTerminalJobs(ctx context.Context, tx pgx.Tx, jobs *[]sourceTerminalJob, query string, args ...any) error {
	if s.sourceMaterializer == nil {
		_, e := tx.Exec(ctx, query, args...)
		return e
	}
	rows, e := tx.Query(ctx, query+" RETURNING job_id,org_id,project_id", args...)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var job sourceTerminalJob
		if e := rows.Scan(&job.id, &job.scope.OrgID, &job.scope.ProjectID); e != nil {
			return e
		}
		*jobs = append(*jobs, job)
	}
	return rows.Err()
}

func (s *PostgresStore) publishTerminalJobs(ctx context.Context, tx pgx.Tx, jobs []sourceTerminalJob) error {
	slices.SortFunc(jobs, func(a, b sourceTerminalJob) int {
		if n := cmp.Compare(a.scope.OrgID, b.scope.OrgID); n != 0 {
			return n
		}
		if n := cmp.Compare(a.scope.ProjectID, b.scope.ProjectID); n != 0 {
			return n
		}
		return cmp.Compare(a.id, b.id)
	})
	for _, job := range jobs {
		if e := s.sourceTerminalTx(ctx, tx, job.scope, job.id); e != nil {
			return e
		}
	}
	return nil
}

func (s *PostgresStore) commitSourceTerminal(ctx context.Context, tx pgx.Tx, scope Scope, id string) (Job, error) {
	job, e := getJobTx(ctx, tx, scope, id, false)
	if e != nil {
		return Job{}, e
	}
	if e = s.sourceStageTx(ctx, tx, job, "terminal", nil); e != nil {
		return Job{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Job{}, e
	}
	s.observeSourceCommitted(ctx, job, "terminal")
	return job, nil
}

// Source-only observations cannot turn a successful original commit into a
// business failure. No new worker, outbox or retry is installed here.
func (s *PostgresStore) observeSourceCommitted(ctx context.Context, job Job, stage string) {
	observer, ok := s.sourceMaterializer.(interface {
		CaptureMediaCommitted(context.Context, Job, string) error
		ObservationsEnabled() bool
	})
	if !ok || !observer.ObservationsEnabled() {
		return
	}
	if stage == "terminal" && !job.Terminal() {
		return
	}
	observed, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	if err := observer.CaptureMediaCommitted(observed, job, stage); err != nil {
		log.Printf("[source-observations] optional media observation unavailable stage=%s", stage)
	}
}
func (s *PostgresStore) observeSourceTerminalJobs(ctx context.Context, jobs []sourceTerminalJob) {
	if observer, ok := s.sourceMaterializer.(interface {
		CaptureMediaCommitted(context.Context, Job, string) error
		ObservationsEnabled() bool
	}); !ok || !observer.ObservationsEnabled() {
		return
	}
	for _, ref := range jobs {
		observed, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
		job, err := s.Get(observed, ref.scope, ref.id)
		if err == nil {
			s.observeSourceCommitted(observed, job, "terminal")
		} else {
			log.Printf("[source-observations] committed media read unavailable")
		}
		cancel()
	}
}

func (s *PostgresStore) observeSourceProviderSuccess(ctx context.Context, scope Scope, id string, at time.Time) {
	observer, ok := s.sourceMaterializer.(interface {
		CaptureMediaCommitted(context.Context, Job, string) error
		ObservationsEnabled() bool
	})
	if !ok || !observer.ObservationsEnabled() {
		return
	}
	observed, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	job, err := s.Get(observed, scope, id)
	if err != nil || !job.UpdatedAt.Equal(at.Truncate(time.Microsecond)) || job.Lifecycle.Outcome != OutcomeSucceeded {
		log.Printf("[source-observations] committed media success observation unavailable")
		return
	}
	s.observeSourceCommitted(observed, job, "provider_succeeded")
}
