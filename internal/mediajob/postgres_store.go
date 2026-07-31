package mediajob

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

type PostgresStore struct {
	db db.DBQuerier
}

const (
	maxOutstandingJobsPerProject = 10
	maxOutstandingJobsPerOrg     = 100
)

func NewPostgresStore(database db.DBQuerier) (*PostgresStore, error) {
	if database == nil {
		return nil, domainError(ErrUnavailable, "database_unavailable")
	}
	return &PostgresStore{db: database}, nil
}

const jobColumns = `
    j.contract_version, j.job_id, j.org_id, j.project_id, j.api_key_id,
    j.request_id, j.client_request_id, j.operation, j.profile_id,
    j.requested_model, j.routing_model, j.mapped_model, j.request_sha256,
    j.idempotency_hash, COALESCE(j.request_ciphertext, ''),
    j.admission_state, j.queue_state, j.lease_state, j.execution_state,
    j.provider_outcome, j.cancellation_state, j.object_state, j.settlement_state,
    j.attempt, j.max_attempts, j.lease_owner, j.lease_fence,
    j.lease_expires_at, j.next_attempt_at,
    j.object_key, j.receipt_key, j.output_sha256, j.output_media_type,
    j.output_size_bytes, j.output_persisted_at, j.output_expires_at,
    j.output_purged_at, j.purge_owner, j.purge_fence, j.purge_expires_at,
    j.purge_attempt, j.error_code, j.started_at, j.terminal_at,
    j.created_at, j.updated_at,
    COALESCE(cf.fact_id, ''), COALESCE(cf.trigger, ''),
    COALESCE(cf.amount_state, ''), cf.amount_micros,
    COALESCE(cf.units, '[]'::jsonb), cf.recorded_at`

type rowScanner interface {
	Scan(...any) error
}

func scanJobExact(row rowScanner) (Job, error) {
	var job Job
	var outputSHA, outputMediaType string
	var outputSize int64
	var outputPersisted, outputExpires, outputPurged *time.Time
	var factID, factTrigger, amountState string
	var amountMicros *int64
	var unitsBody []byte
	var factRecordedAt *time.Time
	if err := row.Scan(
		&job.ContractVersion, &job.JobID, &job.Scope.OrgID, &job.Scope.ProjectID,
		&job.APIKeyID, &job.RequestID, &job.ClientRequestID, &job.Operation,
		&job.ProfileID, &job.RequestedModel, &job.RoutingModel, &job.MappedModel,
		&job.RequestSHA256, &job.IdempotencyHash, &job.RequestCiphertext,
		&job.Lifecycle.Admission, &job.Lifecycle.Queue, &job.Lifecycle.Lease,
		&job.Lifecycle.Execution, &job.Lifecycle.Outcome,
		&job.Lifecycle.Cancellation, &job.Lifecycle.Object,
		&job.Lifecycle.Settlement, &job.Attempt, &job.MaxAttempts,
		&job.LeaseOwner, &job.LeaseFence, &job.LeaseExpiresAt,
		&job.NextAttemptAt, &job.ObjectKey, &job.ReceiptKey,
		&outputSHA, &outputMediaType, &outputSize,
		&outputPersisted, &outputExpires, &outputPurged,
		&job.PurgeOwner, &job.PurgeFence, &job.PurgeExpiresAt,
		&job.PurgeAttempt, &job.ErrorCode,
		&job.StartedAt, &job.TerminalAt, &job.CreatedAt, &job.UpdatedAt,
		&factID, &factTrigger, &amountState, &amountMicros, &unitsBody,
		&factRecordedAt,
	); err != nil {
		return Job{}, err
	}
	job.LeaseExpiresAt = utcTimePointer(job.LeaseExpiresAt)
	job.PurgeExpiresAt = utcTimePointer(job.PurgeExpiresAt)
	job.StartedAt = utcTimePointer(job.StartedAt)
	job.TerminalAt = utcTimePointer(job.TerminalAt)
	job.NextAttemptAt = job.NextAttemptAt.UTC()
	job.CreatedAt = job.CreatedAt.UTC()
	job.UpdatedAt = job.UpdatedAt.UTC()
	outputPersisted = utcTimePointer(outputPersisted)
	outputExpires = utcTimePointer(outputExpires)
	outputPurged = utcTimePointer(outputPurged)
	factRecordedAt = utcTimePointer(factRecordedAt)
	if outputSHA != "" {
		job.Output = &Output{
			ContentSHA256: outputSHA, MediaType: outputMediaType,
			SizeBytes: outputSize, PersistedAt: outputPersisted,
			ExpiresAt: outputExpires, PurgedAt: outputPurged,
		}
	}
	if factID != "" {
		if factRecordedAt == nil {
			return Job{}, fmt.Errorf("mediajob: charge fact timestamp missing")
		}
		var units []endpointprotocol.UnitObservation
		if err := json.Unmarshal(unitsBody, &units); err != nil {
			return Job{}, fmt.Errorf("mediajob: decode charge units: %w", err)
		}
		job.ChargeFact = &ChargeFact{
			FactID: factID, Trigger: factTrigger, AmountState: amountState,
			AmountMicros: amountMicros, Units: units, RecordedAt: *factRecordedAt,
		}
	}
	return job, nil
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func (s *PostgresStore) CreateOrGet(
	ctx context.Context,
	record CreateRecord,
) (Job, bool, error) {
	job := record.Job
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Job{}, false, fmt.Errorf("mediajob: begin create: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(
		    hashtextextended(
		        'async-media:org:' || ($1::bigint)::text,
		        0
		    )
		)`,
		job.Scope.OrgID,
	); err != nil {
		return Job{}, false, fmt.Errorf("mediajob: acquire organization admission lock: %w", err)
	}
	existing, existingErr := scanJobExact(tx.QueryRow(ctx, `
		SELECT `+jobColumns+`
		  FROM async_media_jobs j
		  LEFT JOIN async_media_charge_facts cf ON cf.job_id = j.job_id
		 WHERE j.org_id=$1 AND j.project_id=$2 AND j.api_key_id=$3
		   AND j.idempotency_hash=$4`,
		job.Scope.OrgID, job.Scope.ProjectID, job.APIKeyID, job.IdempotencyHash,
	))
	if existingErr == nil {
		if err := tx.Commit(ctx); err != nil {
			return Job{}, false, fmt.Errorf("mediajob: commit idempotent read: %w", err)
		}
		return existing, true, nil
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return Job{}, false, fmt.Errorf("mediajob: read idempotency state: %w", existingErr)
	}
	if !record.AllowCreate {
		return Job{}, false, domainError(ErrUnavailable, "async_media_admission_disabled")
	}
	var projectOutstanding, orgOutstanding int
	if err := tx.QueryRow(ctx, `
		SELECT
		    COUNT(*) FILTER (WHERE project_id=$2),
		    COUNT(*)
		  FROM async_media_jobs
		 WHERE org_id=$1 AND execution_state <> 'terminal'`,
		job.Scope.OrgID, job.Scope.ProjectID,
	).Scan(&projectOutstanding, &orgOutstanding); err != nil {
		return Job{}, false, fmt.Errorf("mediajob: read admission capacity: %w", err)
	}
	if projectOutstanding >= maxOutstandingJobsPerProject {
		return Job{}, false, domainError(ErrUnavailable, "project_outstanding_limit")
	}
	if orgOutstanding >= maxOutstandingJobsPerOrg {
		return Job{}, false, domainError(ErrUnavailable, "organization_outstanding_limit")
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO async_media_jobs (
		    job_id, contract_version, org_id, project_id, api_key_id,
		    request_id, client_request_id, operation, profile_id,
		    requested_model, routing_model, request_sha256, idempotency_hash,
		    request_ciphertext, admission_state, queue_state, lease_state,
		    execution_state, provider_outcome, cancellation_state, object_state,
		    settlement_state, attempt, max_attempts, lease_fence,
		    next_attempt_at, created_at, updated_at
		) VALUES (
		    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
		    $18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28
		)
		ON CONFLICT (org_id, project_id, api_key_id, idempotency_hash)
		DO NOTHING`,
		job.JobID, job.ContractVersion, job.Scope.OrgID, job.Scope.ProjectID,
		job.APIKeyID, job.RequestID, job.ClientRequestID, job.Operation,
		job.ProfileID, job.RequestedModel, job.RoutingModel, job.RequestSHA256,
		job.IdempotencyHash, job.RequestCiphertext, job.Lifecycle.Admission,
		job.Lifecycle.Queue, job.Lifecycle.Lease, job.Lifecycle.Execution,
		job.Lifecycle.Outcome, job.Lifecycle.Cancellation, job.Lifecycle.Object,
		job.Lifecycle.Settlement, job.Attempt, job.MaxAttempts, job.LeaseFence,
		job.NextAttemptAt, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return Job{}, false, fmt.Errorf("mediajob: insert: %w", err)
	}
	reused := tag.RowsAffected() == 0
	row := tx.QueryRow(ctx, `
		SELECT `+jobColumns+`
		  FROM async_media_jobs j
		  LEFT JOIN async_media_charge_facts cf ON cf.job_id = j.job_id
		 WHERE j.org_id=$1 AND j.project_id=$2 AND j.api_key_id=$3
		   AND j.idempotency_hash=$4`,
		job.Scope.OrgID, job.Scope.ProjectID, job.APIKeyID, job.IdempotencyHash,
	)
	stored, err := scanJobExact(row)
	if err != nil {
		return Job{}, false, fmt.Errorf("mediajob: read after create: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, fmt.Errorf("mediajob: commit create: %w", err)
	}
	return stored, reused, nil
}

func (s *PostgresStore) Get(ctx context.Context, scope Scope, jobID string) (Job, error) {
	job, err := scanJobExact(s.db.QueryRow(ctx, `
		SELECT `+jobColumns+`
		  FROM async_media_jobs j
		  LEFT JOIN async_media_charge_facts cf ON cf.job_id = j.job_id
		 WHERE j.org_id=$1 AND j.project_id=$2 AND j.job_id=$3`,
		scope.OrgID, scope.ProjectID, jobID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: get: %w", err)
	}
	return job, nil
}

func (s *PostgresStore) List(
	ctx context.Context,
	filter ListFilter,
) (ListResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args := []any{filter.Scope.OrgID, filter.Scope.ProjectID}
	whereCursor := ""
	if !filter.Before.IsZero() {
		args = append(args, filter.Before.UTC(), filter.BeforeJobID)
		whereCursor = " AND (j.created_at, j.job_id) < ($3, $4)"
	}
	args = append(args, limit+1)
	rows, err := s.db.Query(ctx, `
		SELECT `+jobColumns+`
		  FROM async_media_jobs j
		  LEFT JOIN async_media_charge_facts cf ON cf.job_id = j.job_id
		 WHERE j.org_id=$1 AND j.project_id=$2`+whereCursor+`
		 ORDER BY j.created_at DESC, j.job_id DESC
		 LIMIT $`+strconv.Itoa(len(args)),
		args...,
	)
	if err != nil {
		return ListResult{}, fmt.Errorf("mediajob: list: %w", err)
	}
	defer rows.Close()
	jobs := make([]Job, 0, limit+1)
	for rows.Next() {
		job, scanErr := scanJobExact(rows)
		if scanErr != nil {
			return ListResult{}, fmt.Errorf("mediajob: scan list: %w", scanErr)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("mediajob: list rows: %w", err)
	}
	result := ListResult{Data: jobs}
	if len(jobs) > limit {
		last := jobs[limit-1]
		result.Data = jobs[:limit]
		result.NextCursor = encodeListCursor(last.CreatedAt, last.JobID)
	}
	return result, nil
}

func encodeListCursor(createdAt time.Time, jobID string) string {
	value := createdAt.UTC().Format(time.RFC3339Nano) + "\n" + jobID
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func DecodeListCursor(value string) (time.Time, string, error) {
	if value == "" {
		return time.Time{}, "", nil
	}
	body, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(body) > 256 {
		return time.Time{}, "", domainError(ErrInvalid, "cursor_invalid")
	}
	parts := strings.Split(string(body), "\n")
	if len(parts) != 2 || !ValidJobID(parts[1]) {
		return time.Time{}, "", domainError(ErrInvalid, "cursor_invalid")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", domainError(ErrInvalid, "cursor_invalid")
	}
	return createdAt.UTC(), parts[1], nil
}
