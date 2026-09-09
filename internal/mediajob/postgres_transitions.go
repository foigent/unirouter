package mediajob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func getJobTx(
	ctx context.Context,
	tx pgx.Tx,
	scope Scope,
	jobID string,
	lock bool,
) (Job, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE OF j"
	}
	job, err := scanJobExact(tx.QueryRow(ctx, `
		SELECT `+jobColumns+`
		  FROM async_media_jobs j
		  LEFT JOIN async_media_charge_facts cf ON cf.job_id = j.job_id
		 WHERE j.org_id=$1 AND j.project_id=$2 AND j.job_id=$3`+suffix,
		scope.OrgID, scope.ProjectID, jobID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return job, err
}

func (s *PostgresStore) RequestCancellation(
	ctx context.Context,
	scope Scope,
	jobID string,
	now time.Time,
) (CancellationResult, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CancellationResult{}, fmt.Errorf("mediajob: begin cancellation: %w", err)
	}
	defer tx.Rollback(ctx)
	job, err := getJobTx(ctx, tx, scope, jobID, true)
	if err != nil {
		return CancellationResult{}, err
	}
	outcome := "requested"
	wasTerminal := job.Terminal()
	switch {
	case job.Lifecycle.Outcome == OutcomeSucceeded:
		outcome = CancellationTooLate
		_, err = tx.Exec(ctx, `
			UPDATE async_media_jobs
			   SET cancellation_state='too_late', updated_at=$1
			 WHERE job_id=$2`,
			now.UTC(), jobID,
		)
	case job.Lifecycle.Outcome == OutcomeUnknown:
		outcome = CancellationIndeterminate
		_, err = tx.Exec(ctx, `
			UPDATE async_media_jobs
			   SET cancellation_state='indeterminate', updated_at=$1
			 WHERE job_id=$2`,
			now.UTC(), jobID,
		)
	case job.Terminal():
		switch job.Lifecycle.Cancellation {
		case CancellationAccepted:
			outcome = CancellationAccepted
		case CancellationIndeterminate:
			outcome = CancellationIndeterminate
		default:
			outcome = CancellationTooLate
			_, err = tx.Exec(ctx, `
				UPDATE async_media_jobs
				   SET cancellation_state='too_late', updated_at=$1
				 WHERE job_id=$2`,
				now.UTC(), jobID,
			)
		}
	case job.Lifecycle.Queue == QueueQueued:
		outcome = CancellationAccepted
		_, err = tx.Exec(ctx, `
			UPDATE async_media_jobs
			   SET queue_state='terminal', lease_state='released',
			       execution_state='terminal', provider_outcome='not_started',
			       cancellation_state='accepted', object_state='not_created',
			       settlement_state='not_eligible', request_ciphertext=NULL,
			       lease_owner='', lease_expires_at=NULL,
			       terminal_at=$1, updated_at=$1, error_code='cancelled_before_dispatch'
			 WHERE job_id=$2`,
			now.UTC(), jobID,
		)
	default:
		_, err = tx.Exec(ctx, `
			UPDATE async_media_jobs
			   SET cancellation_state='requested', updated_at=$1
			 WHERE job_id=$2`,
			now.UTC(), jobID,
		)
	}
	if err != nil {
		return CancellationResult{}, fmt.Errorf("mediajob: cancel: %w", err)
	}
	job, err = getJobTx(ctx, tx, scope, jobID, false)
	if err != nil {
		return CancellationResult{}, err
	}
	if !wasTerminal {
		if err := s.sourceStageTx(ctx, tx, job, "terminal", nil); err != nil {
			return CancellationResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CancellationResult{}, fmt.Errorf("mediajob: commit cancellation: %w", err)
	}
	return CancellationResult{Job: job, Outcome: outcome}, nil
}

func (s *PostgresStore) Claim(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseDuration time.Duration,
) (*Job, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 128 || leaseDuration <= 0 {
		return nil, domainError(ErrInvalid, "lease_request_invalid")
	}
	now = now.UTC()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("mediajob: begin claim: %w", err)
	}
	defer tx.Rollback(ctx)

	// Once provider dispatch has begun, an expired lease has an ambiguous
	// provider outcome. It must never be reclaimed for another provider call.
	var terminalJobs []sourceTerminalJob
	err = s.updateTerminalJobs(ctx, tx, &terminalJobs, `
		UPDATE async_media_jobs
		   SET queue_state='terminal', lease_state='released',
		       execution_state='terminal', provider_outcome='unknown',
		       cancellation_state=CASE
		         WHEN cancellation_state='requested' THEN 'indeterminate'
		         ELSE cancellation_state
		       END,
		       object_state='not_created', settlement_state='not_eligible',
		       request_ciphertext=NULL, lease_owner='', lease_expires_at=NULL,
		       terminal_at=$1, updated_at=$1,
		       error_code='provider_outcome_unknown_after_lease_expiry'
		 WHERE queue_state='leased' AND lease_expires_at <= $1
		   AND execution_state='running' AND provider_outcome='pending'`,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("mediajob: expire ambiguous provider lease: %w", err)
	}
	// A pending execution has not dispatched. Its cancellation can therefore
	// be accepted without creating provider-side ambiguity.
	err = s.updateTerminalJobs(ctx, tx, &terminalJobs, `
		UPDATE async_media_jobs
		   SET queue_state='terminal', lease_state='released',
		       execution_state='terminal', provider_outcome='not_started',
		       cancellation_state='accepted', object_state='not_created',
		       settlement_state='not_eligible', request_ciphertext=NULL,
		       lease_owner='', lease_expires_at=NULL,
		       terminal_at=$1, updated_at=$1, error_code='cancelled_before_redispatch'
		 WHERE queue_state='leased' AND lease_expires_at <= $1
		   AND cancellation_state='requested' AND execution_state='pending'
		   AND provider_outcome='pending'`,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("mediajob: expire cancelled lease: %w", err)
	}
	err = s.updateTerminalJobs(ctx, tx, &terminalJobs, `
		UPDATE async_media_jobs
		   SET queue_state='terminal', lease_state='released',
		       execution_state='terminal',
		       provider_outcome=CASE
		         WHEN execution_state='running' THEN 'failed'
		         ELSE 'not_started'
		       END,
		       object_state='not_created', settlement_state='not_eligible',
		       request_ciphertext=NULL, lease_owner='', lease_expires_at=NULL,
		       terminal_at=$1, updated_at=$1, error_code='attempts_exhausted'
		 WHERE queue_state='leased' AND lease_expires_at <= $1
		   AND cancellation_state='not_requested'
		   AND provider_outcome='pending'
		   AND attempt >= max_attempts AND execution_state <> 'terminal'`,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("mediajob: expire exhausted lease: %w", err)
	}
	err = s.updateTerminalJobs(ctx, tx, &terminalJobs, `
		UPDATE async_media_jobs
		   SET queue_state='terminal', lease_state='released',
		       execution_state='terminal', object_state='not_created',
		       settlement_state='not_eligible', request_ciphertext=NULL,
		       lease_owner='', lease_expires_at=NULL,
		       terminal_at=$1, updated_at=$1,
		       error_code='output_recovery_exhausted'
		 WHERE queue_state='leased' AND lease_expires_at <= $1
		   AND provider_outcome='succeeded'
		   AND attempt >= max_attempts AND execution_state <> 'terminal'`,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("mediajob: expire exhausted output recovery: %w", err)
	}

	var jobID string
	var scope Scope
	err = tx.QueryRow(ctx, `
		SELECT job_id, org_id, project_id
		  FROM async_media_jobs
		 WHERE (
		     cancellation_state='not_requested'
		     OR (
		       provider_outcome='succeeded'
		       AND cancellation_state='too_late'
		     )
		   )
		   AND attempt < max_attempts
		   AND (
		     (provider_outcome='pending' AND execution_state='pending')
		     OR
		     (
		       provider_outcome='succeeded'
		       AND execution_state IN ('pending','running')
		     )
		   )
		   AND (
		     (queue_state='queued' AND next_attempt_at <= $1)
		     OR
		     (queue_state='leased' AND lease_expires_at <= $1)
		   )
		 ORDER BY next_attempt_at, created_at, job_id
		 FOR UPDATE SKIP LOCKED
		 LIMIT 1`,
		now,
	).Scan(&jobID, &scope.OrgID, &scope.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.publishTerminalJobs(ctx, tx, terminalJobs); err != nil {
			return nil, err
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("mediajob: commit empty claim: %w", commitErr)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mediajob: select claim: %w", err)
	}
	expiresAt := now.Add(leaseDuration)
	tag, err := tx.Exec(ctx, `
		UPDATE async_media_jobs
		   SET queue_state='leased', lease_state='leased',
		       execution_state='pending', lease_owner=$1,
		       lease_fence=lease_fence+1, lease_expires_at=$2,
		       updated_at=$3
		 WHERE job_id=$4 AND execution_state <> 'terminal'`,
		owner, expiresAt, now, jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("mediajob: update claim: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrLeaseLost
	}
	job, err := getJobTx(ctx, tx, scope, jobID, false)
	if err != nil {
		return nil, err
	}
	if err := s.publishTerminalJobs(ctx, tx, terminalJobs); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("mediajob: commit claim: %w", err)
	}
	return &job, nil
}

func (s *PostgresStore) MarkRunning(
	ctx context.Context,
	lease Lease,
	now time.Time,
) error {
	now = now.UTC()
	tag, err := s.db.Exec(ctx, `
		UPDATE async_media_jobs
		   SET execution_state='running', started_at=COALESCE(started_at,$1),
		       attempt=attempt+1,updated_at=$1
		 WHERE job_id=$2 AND org_id=$3 AND project_id=$4
		   AND lease_owner=$5 AND lease_fence=$6
		   AND lease_expires_at > $1 AND queue_state='leased'
		   AND execution_state='pending'
		   AND attempt<max_attempts
		   AND provider_outcome='pending'
		   AND cancellation_state='not_requested'`,
		now, lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID,
		lease.Owner, lease.Fence,
	)
	if err != nil {
		return fmt.Errorf("mediajob: mark running: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	tag, err = s.db.Exec(ctx, `
		UPDATE async_media_jobs
		   SET queue_state='terminal', lease_state='released',
		       execution_state='terminal', provider_outcome='not_started',
		       cancellation_state='accepted', object_state='not_created',
		       settlement_state='not_eligible', request_ciphertext=NULL,
		       lease_owner='', lease_expires_at=NULL, terminal_at=$1,
		       updated_at=$1, error_code='cancelled_before_dispatch'
		 WHERE job_id=$2 AND org_id=$3 AND project_id=$4
		   AND lease_owner=$5 AND lease_fence=$6
		   AND lease_expires_at > $1 AND queue_state='leased'
		   AND execution_state='pending'
		   AND provider_outcome='pending'
		   AND cancellation_state='requested'`,
		now, lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID,
		lease.Owner, lease.Fence,
	)
	if err != nil {
		return fmt.Errorf("mediajob: accept cancellation: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return ErrCancelled
	}
	return ErrLeaseLost
}

func (s *PostgresStore) MarkProviderSucceeded(
	ctx context.Context,
	lease Lease,
	mappedModel string,
	now time.Time,
) error {
	mappedModel = strings.TrimSpace(mappedModel)
	now = now.UTC()
	if mappedModel == "" || len(mappedModel) > 200 || now.IsZero() {
		return domainError(ErrInvalid, "provider_success_invalid")
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE async_media_jobs
		   SET provider_outcome='succeeded', mapped_model=$1,
		       cancellation_state=CASE
		         WHEN cancellation_state='requested' THEN 'too_late'
		         ELSE cancellation_state
		       END,
		       request_ciphertext=NULL, updated_at=$2
		 WHERE job_id=$3 AND org_id=$4 AND project_id=$5
		   AND lease_owner=$6 AND lease_fence=$7
		   AND lease_expires_at > $2 AND queue_state='leased'
		   AND execution_state='running'
		   AND (
		     provider_outcome='pending'
		     OR (provider_outcome='succeeded' AND mapped_model=$1)
		   )`,
		mappedModel, now, lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID,
		lease.Owner, lease.Fence,
	)
	if err != nil {
		return fmt.Errorf("mediajob: mark provider success: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func validatePersistedResult(job Job, result PersistedResult) error {
	if result.ObjectKey == "" || result.ReceiptKey == "" ||
		!validSHA256(result.ContentSHA256) ||
		result.SizeBytes <= 0 || result.SizeBytes > MaxOutputBytes ||
		result.PersistedAt.IsZero() ||
		result.MappedModel == "" ||
		validateImageOutcome(job.ProfileID, result.MappedModel, result.Outcome) != nil {
		return domainError(ErrInvalid, "persisted_result_invalid")
	}
	switch result.MediaType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return domainError(ErrInvalid, "persisted_media_type_invalid")
	}
	return nil
}

func (s *PostgresStore) CompleteSuccess(
	ctx context.Context,
	lease Lease,
	result PersistedResult,
	retention time.Duration,
	now time.Time,
) (Job, error) {
	if retention != DefaultRetention {
		return Job{}, domainError(ErrInvalid, "retention_must_be_seven_days")
	}
	if err := validatePersistedResult(lease.Job, result); err != nil {
		return Job{}, err
	}
	result.PersistedAt = result.PersistedAt.UTC()
	now = now.UTC()
	if now.IsZero() || result.PersistedAt.After(now) {
		return Job{}, domainError(ErrInvalid, "completion_time_invalid")
	}
	expiresAt := result.PersistedAt.Add(retention)
	units, err := json.Marshal(result.Outcome.Units)
	if err != nil {
		return Job{}, domainError(ErrInvalid, "usage_units_invalid")
	}
	factID := strings.Replace(lease.JobID, "mediajob_", "mediacharge_", 1)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: begin success: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE async_media_jobs
		   SET queue_state='terminal', lease_state='released',
		       execution_state='terminal', provider_outcome='succeeded',
		       cancellation_state=CASE
		         WHEN cancellation_state='requested' THEN 'too_late'
		         ELSE cancellation_state
		       END,
		       object_state='persisted', settlement_state='charge_fact_recorded',
		       mapped_model=$1, object_key=$2, receipt_key=$3,
		       output_sha256=$4, output_media_type=$5, output_size_bytes=$6,
		       output_persisted_at=$7, output_expires_at=$8,
		       request_ciphertext=NULL, lease_owner='', lease_expires_at=NULL,
		       error_code='', terminal_at=$9, updated_at=$9
		 WHERE job_id=$10 AND org_id=$11 AND project_id=$12
		   AND lease_owner=$13 AND lease_fence=$14
		   AND lease_expires_at > $9 AND queue_state='leased'
		   AND execution_state IN ('pending','running')
		   AND provider_outcome='succeeded'
		   AND mapped_model=$1`,
		result.MappedModel, result.ObjectKey, result.ReceiptKey,
		result.ContentSHA256, result.MediaType, result.SizeBytes,
		result.PersistedAt, expiresAt, now, lease.JobID,
		lease.Scope.OrgID, lease.Scope.ProjectID, lease.Owner, lease.Fence,
	)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: complete success: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Job{}, ErrLeaseLost
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO async_media_charge_facts (
		    fact_id, job_id, org_id, project_id, trigger,
		    amount_state, amount_micros, units, recorded_at
		) VALUES (
		    $1,$2,$3,$4,'provider_terminal_success_and_output_persisted',
		    'pricing_pending_media_rate_card',NULL,$5::jsonb,$6
		)`,
		factID, lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID,
		string(units), now,
	)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: record charge fact: %w", err)
	}
	job, err := getJobTx(ctx, tx, lease.Scope, lease.JobID, false)
	if err != nil {
		return Job{}, err
	}
	if err := s.sourceStageTx(ctx, tx, job, "terminal", nil); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("mediajob: commit success: %w", err)
	}
	return job, nil
}

func (s *PostgresStore) CompleteFailure(
	ctx context.Context,
	lease Lease,
	code string,
	now time.Time,
) (Job, error) {
	code = sanitizeErrorCode(code)
	now = now.UTC()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE async_media_jobs
		   SET queue_state='terminal', lease_state='released',
		       provider_outcome=CASE
		         WHEN execution_state='running' THEN 'failed'
		         ELSE 'not_started'
		       END,
		       execution_state='terminal',
		       cancellation_state=CASE
		         WHEN cancellation_state='requested' THEN 'accepted'
		         ELSE cancellation_state
		       END,
		       object_state='not_created', settlement_state='not_eligible',
		       request_ciphertext=NULL, lease_owner='', lease_expires_at=NULL,
		       terminal_at=$1, updated_at=$1, error_code=$2
		 WHERE job_id=$3 AND org_id=$4 AND project_id=$5
		   AND lease_owner=$6 AND lease_fence=$7
		   AND lease_expires_at > $1 AND queue_state='leased'
		   AND execution_state IN ('pending','running')
		   AND provider_outcome='pending'`,
		now, code, lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID,
		lease.Owner, lease.Fence,
	)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: complete failure: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Job{}, ErrLeaseLost
	}
	return s.commitSourceTerminal(ctx, tx, lease.Scope, lease.JobID)
}

func (s *PostgresStore) CompleteUnknown(
	ctx context.Context,
	lease Lease,
	code string,
	now time.Time,
) (Job, error) {
	code = sanitizeErrorCode(code)
	now = now.UTC()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE async_media_jobs
		   SET queue_state='terminal', lease_state='released',
		       execution_state='terminal', provider_outcome='unknown',
		       cancellation_state=CASE
		         WHEN cancellation_state='requested' THEN 'indeterminate'
		         ELSE cancellation_state
		       END,
		       object_state='not_created', settlement_state='not_eligible',
		       request_ciphertext=NULL, lease_owner='', lease_expires_at=NULL,
		       terminal_at=$1, updated_at=$1, error_code=$2
		 WHERE job_id=$3 AND org_id=$4 AND project_id=$5
		   AND lease_owner=$6 AND lease_fence=$7
		   AND lease_expires_at > $1 AND queue_state='leased'
		   AND execution_state IN ('pending','running')
		   AND provider_outcome='pending'`,
		now, code, lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID,
		lease.Owner, lease.Fence,
	)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: complete unknown outcome: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Job{}, ErrLeaseLost
	}
	return s.commitSourceTerminal(ctx, tx, lease.Scope, lease.JobID)
}

func (s *PostgresStore) CompleteOutputFailure(
	ctx context.Context,
	lease Lease,
	code string,
	now time.Time,
) (Job, error) {
	code = sanitizeErrorCode(code)
	now = now.UTC()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE async_media_jobs
		   SET queue_state='terminal', lease_state='released',
		       execution_state='terminal', provider_outcome='succeeded',
		       cancellation_state=CASE
		         WHEN cancellation_state IN ('requested','too_late') THEN 'too_late'
		         ELSE cancellation_state
		       END,
		       object_state='not_created', settlement_state='not_eligible',
		       request_ciphertext=NULL, lease_owner='', lease_expires_at=NULL,
		       terminal_at=$1, updated_at=$1, error_code=$2
		 WHERE job_id=$3 AND org_id=$4 AND project_id=$5
		   AND lease_owner=$6 AND lease_fence=$7
		   AND lease_expires_at > $1 AND queue_state='leased'
		   AND execution_state IN ('pending','running')
		   AND provider_outcome='succeeded'`,
		now, code, lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID,
		lease.Owner, lease.Fence,
	)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: complete output failure: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Job{}, ErrLeaseLost
	}
	return s.commitSourceTerminal(ctx, tx, lease.Scope, lease.JobID)
}

func (s *PostgresStore) Requeue(
	ctx context.Context,
	lease Lease,
	code string,
	now time.Time,
	nextAttemptAt time.Time,
) (Job, error) {
	code = sanitizeErrorCode(code)
	now = now.UTC()
	nextAttemptAt = nextAttemptAt.UTC()
	if now.IsZero() || nextAttemptAt.Before(now) {
		return Job{}, domainError(ErrInvalid, "retry_time_invalid")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: begin requeue: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE async_media_jobs
		   SET queue_state=CASE
		         WHEN cancellation_state='requested' OR attempt >= max_attempts
		         THEN 'terminal' ELSE 'queued'
		       END,
		       lease_state='released',
		       execution_state=CASE
		         WHEN cancellation_state='requested' OR attempt >= max_attempts
		         THEN 'terminal' ELSE 'pending'
			       END,
			       provider_outcome=CASE
			         WHEN cancellation_state='requested' AND execution_state='pending'
			         THEN 'not_started'
			         WHEN cancellation_state='requested' OR attempt >= max_attempts
			         THEN 'failed'
			         ELSE 'pending'
			       END,
		       cancellation_state=CASE
		         WHEN cancellation_state='requested' THEN 'accepted'
		         ELSE cancellation_state
		       END,
		       object_state=CASE
		         WHEN cancellation_state='requested' OR attempt >= max_attempts
		         THEN 'not_created' ELSE 'pending'
		       END,
		       request_ciphertext=CASE
		         WHEN cancellation_state='requested' OR attempt >= max_attempts
		         THEN NULL ELSE request_ciphertext
			       END,
			       lease_owner='', lease_expires_at=NULL,
			       next_attempt_at=$1, error_code=$3,
		       terminal_at=CASE
		         WHEN cancellation_state='requested' OR attempt >= max_attempts
		         THEN $2 ELSE NULL
		       END,
		       updated_at=$2
		 WHERE job_id=$4 AND org_id=$5 AND project_id=$6
		   AND lease_owner=$7 AND lease_fence=$8
			   AND lease_expires_at > $2
			   AND queue_state='leased'
			   AND execution_state IN ('pending','running')
			   AND provider_outcome='pending'`,
		nextAttemptAt, now, code, lease.JobID, lease.Scope.OrgID,
		lease.Scope.ProjectID, lease.Owner, lease.Fence,
	)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: requeue: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Job{}, ErrLeaseLost
	}
	job, err := getJobTx(ctx, tx, lease.Scope, lease.JobID, false)
	if err != nil {
		return Job{}, err
	}
	if err := s.sourceStageTx(ctx, tx, job, "terminal", nil); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("mediajob: commit requeue: %w", err)
	}
	return job, nil
}

func (s *PostgresStore) RequeueRecovery(
	ctx context.Context,
	lease Lease,
	code string,
	now time.Time,
	nextAttemptAt time.Time,
) (Job, error) {
	code = sanitizeErrorCode(code)
	now = now.UTC()
	nextAttemptAt = nextAttemptAt.UTC()
	if now.IsZero() || nextAttemptAt.Before(now) {
		return Job{}, domainError(ErrInvalid, "recovery_retry_time_invalid")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: begin recovery requeue: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE async_media_jobs
		   SET queue_state=CASE
		         WHEN attempt >= max_attempts THEN 'terminal' ELSE 'queued'
		       END,
		       lease_state='released',
		       execution_state=CASE
		         WHEN attempt >= max_attempts THEN 'terminal' ELSE 'pending'
		       END,
		       provider_outcome='succeeded',
		       cancellation_state=CASE
		         WHEN cancellation_state='requested' THEN 'too_late'
		         ELSE cancellation_state
		       END,
		       object_state='not_created',
		       settlement_state='not_eligible',
		       request_ciphertext=NULL,
		       lease_owner='', lease_expires_at=NULL,
		       next_attempt_at=$1, error_code=$3,
		       terminal_at=CASE WHEN attempt >= max_attempts THEN $2 ELSE NULL END,
		       updated_at=$2
		 WHERE job_id=$4 AND org_id=$5 AND project_id=$6
		   AND lease_owner=$7 AND lease_fence=$8
			   AND lease_expires_at > $2
			   AND queue_state='leased'
			   AND execution_state IN ('pending','running')
			   AND provider_outcome='succeeded'`,
		nextAttemptAt, now, code, lease.JobID, lease.Scope.OrgID,
		lease.Scope.ProjectID, lease.Owner, lease.Fence,
	)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: requeue output recovery: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Job{}, ErrLeaseLost
	}
	job, err := getJobTx(ctx, tx, lease.Scope, lease.JobID, false)
	if err != nil {
		return Job{}, err
	}
	if err := s.sourceStageTx(ctx, tx, job, "terminal", nil); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("mediajob: commit output recovery requeue: %w", err)
	}
	return job, nil
}

func (s *PostgresStore) ClaimExpired(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseDuration time.Duration,
) (*PurgeLease, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 128 || leaseDuration <= 0 {
		return nil, domainError(ErrInvalid, "purge_lease_invalid")
	}
	now = now.UTC()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("mediajob: begin purge claim: %w", err)
	}
	defer tx.Rollback(ctx)
	var jobID string
	var scope Scope
	err = tx.QueryRow(ctx, `
		SELECT job_id, org_id, project_id
		  FROM async_media_jobs
		 WHERE output_expires_at <= $1
		   AND (
		     object_state='persisted'
		     OR (
		       object_state='purge_pending'
		       AND (purge_expires_at IS NULL OR purge_expires_at <= $1)
		     )
		   )
		 ORDER BY output_expires_at, job_id
		 FOR UPDATE SKIP LOCKED
		 LIMIT 1`,
		now,
	).Scan(&jobID, &scope.OrgID, &scope.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("mediajob: commit empty purge claim: %w", commitErr)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mediajob: select purge claim: %w", err)
	}
	expiresAt := now.Add(leaseDuration)
	_, err = tx.Exec(ctx, `
		UPDATE async_media_jobs
		   SET object_state='purge_pending', purge_owner=$1,
		       purge_fence=purge_fence+1, purge_expires_at=$2,
		       purge_attempt=purge_attempt+1, updated_at=$3
		 WHERE job_id=$4`,
		owner, expiresAt, now, jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("mediajob: update purge claim: %w", err)
	}
	job, err := getJobTx(ctx, tx, scope, jobID, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("mediajob: commit purge claim: %w", err)
	}
	return &PurgeLease{
		Job: job, Owner: owner, Fence: job.PurgeFence,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *PostgresStore) MarkPurgeRetry(
	ctx context.Context,
	lease PurgeLease,
	code string,
	now time.Time,
	nextAttemptAt time.Time,
) error {
	now = now.UTC()
	nextAttemptAt = nextAttemptAt.UTC()
	if now.IsZero() || nextAttemptAt.Before(now) {
		return domainError(ErrInvalid, "purge_retry_time_invalid")
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE async_media_jobs
		   SET purge_owner='', purge_expires_at=$1, error_code=$2, updated_at=$3
		 WHERE job_id=$4 AND org_id=$5 AND project_id=$6
		   AND purge_owner=$7 AND purge_fence=$8
		   AND purge_expires_at > $3
		   AND object_state='purge_pending'`,
		nextAttemptAt, sanitizeErrorCode(code), now, lease.Job.JobID,
		lease.Job.Scope.OrgID, lease.Job.Scope.ProjectID, lease.Owner, lease.Fence,
	)
	if err != nil {
		return fmt.Errorf("mediajob: mark purge retry: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (s *PostgresStore) MarkPurged(
	ctx context.Context,
	lease PurgeLease,
	now time.Time,
) (Job, error) {
	now = now.UTC()
	tag, err := s.db.Exec(ctx, `
		UPDATE async_media_jobs
		   SET object_state='purged', object_key='', receipt_key='',
		       output_purged_at=$1, purge_owner='', purge_expires_at=NULL,
		       error_code='', updated_at=$1
		 WHERE job_id=$2 AND org_id=$3 AND project_id=$4
		   AND purge_owner=$5 AND purge_fence=$6
		   AND purge_expires_at > $1 AND object_state='purge_pending'`,
		now, lease.Job.JobID, lease.Job.Scope.OrgID, lease.Job.Scope.ProjectID,
		lease.Owner, lease.Fence,
	)
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: mark purged: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Job{}, ErrLeaseLost
	}
	return s.Get(ctx, lease.Job.Scope, lease.Job.JobID)
}

func sanitizeErrorCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown_failure"
	}
	var out strings.Builder
	for _, r := range value {
		if out.Len() >= 96 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			out.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r + ('a' - 'A'))
		default:
			out.WriteByte('_')
		}
	}
	sanitized := strings.Trim(out.String(), "_")
	if sanitized == "" {
		return "unknown_failure"
	}
	return sanitized
}
