package mediajob

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/limitpolicy"
)

type PostgresStore struct {
	db                  db.DBQuerier
	executionAuthorizer ExecutionModelAuthorizer
	sourceMaterializer  SourceMaterializer
}

const (
	maxOutstandingJobsPerProject = 10
	maxOutstandingJobsPerOrg     = 100
)

func NewPostgresStore(database db.DBQuerier, authorizers ...ExecutionModelAuthorizer) (*PostgresStore, error) {
	if database == nil {
		return nil, domainError(ErrUnavailable, "database_unavailable")
	}
	var authorizer ExecutionModelAuthorizer
	if len(authorizers) > 1 {
		return nil, domainError(ErrInvalid, "execution_authorizer_invalid")
	}
	if len(authorizers) == 1 {
		authorizer = authorizers[0]
	}
	return &PostgresStore{db: database, executionAuthorizer: authorizer}, nil
}

const jobColumns = `
    j.contract_version, j.job_id, j.org_id, j.project_id, j.api_key_id,
    j.access_grant_id, j.access_credential_id, COALESCE(j.credential_ref, ''),
    j.idempotency_subject_type, j.idempotency_subject_id,
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
		&job.APIKeyID, &job.AccessGrantID, &job.AccessCredentialID, &job.CredentialRef,
		&job.IdempotencySubjectType, &job.IdempotencySubjectID,
		&job.RequestID, &job.ClientRequestID, &job.Operation,
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

func resolveJobAccessBinding(ctx context.Context, tx pgx.Tx, job Job) (Job, accessgrantboundary.Snapshot, error) {
	snapshot, err := accessgrantboundary.ResolveForCommit(
		ctx, tx, job.APIKeyID, job.Scope.OrgID, job.Scope.ProjectID,
	)
	if err != nil {
		if errors.Is(err, accessgrantboundary.ErrInactive) {
			return Job{}, accessgrantboundary.Snapshot{}, domainError(ErrForbidden, "submission_authority_changed")
		}
		return Job{}, accessgrantboundary.Snapshot{}, fmt.Errorf("mediajob: resolve live API key access binding: %w", err)
	}
	grantID := snapshot.APIKey.AccessGrantID
	credentialID := snapshot.APIKey.AccessCredentialID
	credentialRef := snapshot.APIKey.CredentialRef
	if (job.AccessGrantID != nil && !sameNullableID(job.AccessGrantID, grantID)) ||
		(job.AccessCredentialID != nil && !sameNullableID(job.AccessCredentialID, credentialID)) ||
		(job.CredentialRef != "" && job.CredentialRef != credentialRef) {
		return Job{}, accessgrantboundary.Snapshot{}, fmt.Errorf("mediajob: supplied access binding does not match API key")
	}
	if (grantID == nil) != (credentialID == nil) ||
		(credentialID == nil) != (credentialRef == "") {
		return Job{}, accessgrantboundary.Snapshot{}, fmt.Errorf("mediajob: incomplete API key access binding")
	}
	if snapshot.APIKey.GrantBound && (snapshot.APIKey.EndpointDenied || snapshot.APIKey.ModelScopeDenied ||
		!mediaScopeContains(snapshot.APIKey.AllowedEndpoints, "/v1/media/jobs") ||
		!modelAllowed(snapshot.APIKey.AllowedModels, job.RequestedModel, job.RoutingModel)) {
		return Job{}, accessgrantboundary.Snapshot{}, domainError(ErrForbidden, "submission_authority_changed")
	}
	job.AccessGrantID = grantID
	job.AccessCredentialID = credentialID
	job.CredentialRef = credentialRef
	if credentialID == nil {
		job.IdempotencySubjectType = "api_key"
		job.IdempotencySubjectID = job.APIKeyID
	} else {
		job.IdempotencySubjectType = "access_credential"
		job.IdempotencySubjectID = *credentialID
	}
	return job, snapshot, nil
}

func mediaScopeContains(raw, required string) bool {
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "*" || value == required {
			return true
		}
	}
	return false
}

func sameNullableID(left, right *int64) bool {
	return left != nil && right != nil && *left == *right
}

func nullMediaCredentialRef(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *PostgresStore) FindByIdempotency(ctx context.Context, lookup IdempotencyLookup) (Job, error) {
	if !lookup.Scope.Valid() || lookup.SubjectID <= 0 || lookup.Hash == "" ||
		(lookup.SubjectType != "api_key" && lookup.SubjectType != "access_credential") {
		return Job{}, domainError(ErrInvalid, "idempotency_lookup_invalid")
	}
	job, err := scanJobExact(s.db.QueryRow(ctx, `
		SELECT `+jobColumns+`
		  FROM async_media_jobs j
		  LEFT JOIN async_media_charge_facts cf ON cf.job_id=j.job_id
		 WHERE j.org_id=$1 AND j.project_id=$2
		   AND j.idempotency_subject_type=$3 AND j.idempotency_subject_id=$4
		   AND j.idempotency_hash=$5`,
		lookup.Scope.OrgID, lookup.Scope.ProjectID, lookup.SubjectType, lookup.SubjectID, lookup.Hash,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("mediajob: read idempotency state: %w", err)
	}
	return job, nil
}

func executionBinding(job Job) accessgrantboundary.ExecutionBinding {
	return accessgrantboundary.ExecutionBinding{
		OrgID: job.Scope.OrgID, ProjectID: job.Scope.ProjectID,
		AdmissionAPIKeyID: job.APIKeyID,
		AccessGrantID:     nullableMediaID(job.AccessGrantID), AccessCredentialID: nullableMediaID(job.AccessCredentialID),
		CredentialRef: job.CredentialRef, RequestedModel: job.RequestedModel,
		RoutingModel: job.RoutingModel, Endpoint: "/v1/media/jobs",
	}
}

func nullableMediaID(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func executionSubject(snapshot accessgrantboundary.Snapshot) limitpolicy.Subject {
	return mediaLimitSubject(
		snapshot.APIKey,
		snapshot.APIKey.OrgID,
		nullableMediaID(snapshot.APIKey.ProjectID),
		snapshot.APIKey.AllowedModels,
	)
}

func mediaInt64Pointer(value *int) *int64 {
	if value == nil {
		return nil
	}
	result := int64(*value)
	return &result
}

func (s *PostgresStore) ExecutionLimitSubject(ctx context.Context, lease Lease) (limitpolicy.Subject, error) {
	snapshot, err := accessgrantboundary.ResolveExecution(ctx, s.db, executionBinding(lease.Job))
	if err != nil {
		if errors.Is(err, accessgrantboundary.ErrInactive) {
			return limitpolicy.Subject{}, domainError(ErrForbidden, "execution_authority_changed")
		}
		return limitpolicy.Subject{}, domainError(ErrUnavailable, "execution_authority_unavailable")
	}
	if err := s.authorizeExecution(ctx, s.db, snapshot, lease.Job); err != nil {
		return limitpolicy.Subject{}, err
	}
	subject := executionSubject(snapshot)
	subject.Model = lease.Job.RoutingModel
	return subject, nil
}

func (s *PostgresStore) AttachExecutionReservation(
	ctx context.Context,
	lease Lease,
	expected limitpolicy.Subject,
	snapshot *limitpolicy.DurableReservation,
	operationID string,
	expiresAt time.Time,
	now time.Time,
) (ExecutionReservation, bool, error) {
	if snapshot == nil || snapshot.OperationID != operationID || operationID == "" || len(operationID) > 64 ||
		expiresAt.IsZero() || !expiresAt.After(now) {
		return ExecutionReservation{}, false, domainError(ErrInvalid, "execution_reservation_invalid")
	}
	body, err := json.Marshal(snapshot)
	if err != nil || len(body) == 0 || len(body) > 65536 || !json.Valid(body) {
		return ExecutionReservation{}, false, domainError(ErrInvalid, "execution_reservation_invalid")
	}
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ExecutionReservation{}, false, fmt.Errorf("mediajob: begin execution admission: %w", err)
	}
	defer tx.Rollback(ctx)
	live, err := accessgrantboundary.ResolveExecutionForCommit(ctx, tx, executionBinding(lease.Job))
	if err != nil {
		if errors.Is(err, accessgrantboundary.ErrInactive) {
			return ExecutionReservation{}, false, domainError(ErrForbidden, "execution_authority_changed")
		}
		return ExecutionReservation{}, false, domainError(ErrUnavailable, "execution_authority_unavailable")
	}
	if err := s.authorizeExecution(ctx, tx, live, lease.Job); err != nil {
		return ExecutionReservation{}, false, err
	}
	current := executionSubject(live)
	current.Model = lease.Job.RoutingModel
	if !sameExecutionSubject(expected, current) {
		return ExecutionReservation{}, false, domainError(ErrConflict, "execution_authority_revision_changed")
	}
	var cancellation string
	if err := tx.QueryRow(ctx, `
		SELECT cancellation_state
		  FROM async_media_jobs
		 WHERE job_id=$1 AND org_id=$2 AND project_id=$3
		   AND lease_owner=$4 AND lease_fence=$5 AND lease_expires_at>$6
		   AND queue_state='leased' AND execution_state='pending'
		   AND provider_outcome='pending'
		 FOR UPDATE`,
		lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID, lease.Owner, lease.Fence, now.UTC(),
	).Scan(&cancellation); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ExecutionReservation{}, false, ErrLeaseLost
		}
		return ExecutionReservation{}, false, fmt.Errorf("mediajob: lock dispatch lease: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO async_media_limit_reservations (
		    job_id,org_id,project_id,lease_fence,operation_id,reservation,reservation_hash,
		    expires_at,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$9)
		ON CONFLICT (job_id,lease_fence) DO NOTHING`,
		lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID, lease.Fence,
		operationID, string(body), hash, expiresAt.UTC(), now.UTC(),
	)
	if err != nil {
		return ExecutionReservation{}, false, fmt.Errorf("mediajob: persist execution reservation: %w", err)
	}
	reused := tag.RowsAffected() == 0
	var stored ExecutionReservation
	var storedBody []byte
	var storedHash string
	if err := tx.QueryRow(ctx, `
		SELECT job_id,org_id,project_id,lease_fence,operation_id,reservation,reservation_hash,
		       expires_at,terminal_kind,terminalized_at
		  FROM async_media_limit_reservations
		 WHERE job_id=$1 AND lease_fence=$2 FOR UPDATE`, lease.JobID, lease.Fence,
	).Scan(&stored.JobID, &stored.Scope.OrgID, &stored.Scope.ProjectID, &stored.LeaseFence,
		&stored.OperationID, &storedBody, &storedHash, &stored.ExpiresAt,
		&stored.TerminalKind, &stored.TerminalizedAt); err != nil {
		return ExecutionReservation{}, false, fmt.Errorf("mediajob: read execution reservation: %w", err)
	}
	if stored.OperationID != operationID || storedHash != hash || json.Unmarshal(storedBody, &stored.Reservation) != nil {
		return ExecutionReservation{}, false, domainError(ErrConflict, "execution_reservation_conflict")
	}
	if cancellation == CancellationRequested {
		if _, err := tx.Exec(ctx, `
			UPDATE async_media_jobs
			   SET queue_state='terminal',lease_state='released',execution_state='terminal',
			       provider_outcome='not_started',cancellation_state='accepted',object_state='not_created',
			       settlement_state='not_eligible',request_ciphertext=NULL,lease_owner='',lease_expires_at=NULL,
			       terminal_at=$1,updated_at=$1,error_code='cancelled_before_dispatch'
			 WHERE job_id=$2 AND lease_fence=$3`, now.UTC(), lease.JobID, lease.Fence); err != nil {
			return ExecutionReservation{}, false, fmt.Errorf("mediajob: accept pre-dispatch cancellation: %w", err)
		}
		if err := s.sourceTerminalTx(ctx, tx, lease.Scope, lease.JobID); err != nil {
			return ExecutionReservation{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ExecutionReservation{}, false, fmt.Errorf("mediajob: commit cancelled dispatch: %w", err)
		}
		s.observeSourceTerminalJobs(ctx, []sourceTerminalJob{{scope: lease.Scope, id: lease.JobID}})
		return stored, reused, ErrCancelled
	}
	if cancellation != CancellationNotRequested {
		return ExecutionReservation{}, false, ErrLeaseLost
	}
	err = tx.QueryRow(ctx, `
		UPDATE async_media_jobs
		   SET execution_state='running',started_at=COALESCE(started_at,$1),
		       attempt=attempt+1,updated_at=$1
		 WHERE job_id=$2 AND org_id=$3 AND project_id=$4
		   AND lease_owner=$5 AND lease_fence=$6 AND lease_expires_at>$1
		   AND queue_state='leased' AND execution_state='pending'
		   AND provider_outcome='pending' AND cancellation_state='not_requested'
		   AND attempt<max_attempts
		RETURNING attempt`,
		now.UTC(), lease.JobID, lease.Scope.OrgID, lease.Scope.ProjectID, lease.Owner, lease.Fence,
	).Scan(&stored.ProviderAttempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExecutionReservation{}, false, ErrLeaseLost
	}
	if err != nil {
		return ExecutionReservation{}, false, fmt.Errorf("mediajob: mark admitted execution running: %w", err)
	}
	if s.sourceMaterializer != nil {
		captured := mediaSourceSnapshot(lease.Job, live, now)
		if err := s.sourceStageTx(ctx, tx, lease.Job, "dispatch", &captured); err != nil {
			return ExecutionReservation{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ExecutionReservation{}, false, fmt.Errorf("mediajob: commit execution admission: %w", err)
	}
	return stored, reused, nil
}

func (s *PostgresStore) authorizeExecution(
	ctx context.Context,
	q db.DBQuerier,
	snapshot accessgrantboundary.Snapshot,
	job Job,
) error {
	if s == nil || s.executionAuthorizer == nil {
		return domainError(ErrUnavailable, "execution_model_authority_unavailable")
	}
	if err := s.executionAuthorizer.AuthorizeExecution(ctx, q, ExecutionAuthorization{
		Scope: job.Scope, APIKeyID: snapshot.APIKey.ID,
		AccessCredentialID: nullableMediaID(snapshot.APIKey.AccessCredentialID),
		OwnerUserID:        nullableMediaID(snapshot.APIKey.OwnerUserID),
		RequestedModel:     job.RequestedModel, RoutingModel: job.RoutingModel,
	}); err != nil {
		if errors.Is(err, ErrForbidden) {
			return domainError(ErrForbidden, "execution_model_authority_changed")
		}
		return domainError(ErrUnavailable, "execution_model_authority_unavailable")
	}
	return nil
}

func sameExecutionSubject(left, right limitpolicy.Subject) bool {
	stableIdentity := left.APIKeyID == right.APIKeyID
	if left.AccessCredentialID > 0 || right.AccessCredentialID > 0 {
		stableIdentity = left.AccessCredentialID > 0 && left.AccessCredentialID == right.AccessCredentialID &&
			left.CredentialRef != "" && left.CredentialRef == right.CredentialRef
	}
	return stableIdentity && left.OrgID == right.OrgID && left.ProjectID == right.ProjectID &&
		left.AccessCredentialID == right.AccessCredentialID && left.CredentialRef == right.CredentialRef &&
		left.AccessGrantID == right.AccessGrantID && left.Model == right.Model &&
		left.BaseRPSLimit == right.BaseRPSLimit && left.BaseRPMLimit == right.BaseRPMLimit &&
		strings.Join(left.PolicyLabels, ",") == strings.Join(right.PolicyLabels, ",") &&
		sameMediaLimit(left.CredentialConcurrencyLimit, right.CredentialConcurrencyLimit) &&
		sameMediaLimit(left.GrantRPSLimit, right.GrantRPSLimit) &&
		sameMediaLimit(left.GrantRPMLimit, right.GrantRPMLimit) &&
		sameMediaLimit(left.GrantConcurrencyLimit, right.GrantConcurrencyLimit)
}

func sameMediaLimit(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func (s *PostgresStore) PendingExecutionTerminal(
	ctx context.Context,
	jobID string,
	leaseFence int64,
	owner string,
	now time.Time,
	claimTTL time.Duration,
) (*ExecutionReservation, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 128 || claimTTL < time.Second || claimTTL > time.Minute ||
		(jobID == "") != (leaseFence == 0) || leaseFence < 0 {
		return nil, domainError(ErrInvalid, "execution_terminal_claim_invalid")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("mediajob: begin execution terminal claim: %w", err)
	}
	defer tx.Rollback(ctx)
	var item ExecutionReservation
	var body []byte
	err = tx.QueryRow(ctx, `
		WITH candidate AS (
		    SELECT job_id,lease_fence
		      FROM async_media_limit_reservations
		     WHERE terminal_kind<>'' AND terminalized_at IS NULL
		       AND (terminal_owner='' OR terminal_claim_expires_at<=$1)
		       AND ($2='' OR (job_id=$2 AND lease_fence=$3))
		     ORDER BY updated_at,job_id,lease_fence
		     FOR UPDATE SKIP LOCKED
		     LIMIT 1
		)
		UPDATE async_media_limit_reservations r
		   SET terminal_owner=$4,terminal_fence=terminal_fence+1,
		       terminal_claim_expires_at=$5,updated_at=GREATEST(updated_at,$1)
		  FROM candidate c
		 WHERE r.job_id=c.job_id AND r.lease_fence=c.lease_fence
		RETURNING r.job_id,r.org_id,r.project_id,r.lease_fence,r.operation_id,r.reservation,
		          r.expires_at,r.terminal_kind,r.terminalized_at,r.terminal_owner,r.terminal_fence`,
		now.UTC(), jobID, leaseFence, owner, now.UTC().Add(claimTTL),
	).Scan(&item.JobID, &item.Scope.OrgID, &item.Scope.ProjectID, &item.LeaseFence,
		&item.OperationID, &body, &item.ExpiresAt, &item.TerminalKind, &item.TerminalizedAt,
		&item.TerminalOwner, &item.TerminalFence)
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return nil, fmt.Errorf("mediajob: commit empty execution terminal claim: %w", commitErr)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mediajob: claim pending execution terminal: %w", err)
	}
	if err := json.Unmarshal(body, &item.Reservation); err != nil {
		return nil, fmt.Errorf("mediajob: decode execution reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("mediajob: commit execution terminal claim: %w", err)
	}
	return &item, nil
}

func (s *PostgresStore) MarkExecutionTerminalized(
	ctx context.Context,
	item ExecutionReservation,
	now time.Time,
	outcome string,
) error {
	if outcome != item.TerminalKind && outcome != "operation_state_expired" {
		return domainError(ErrInvalid, "execution_terminal_outcome_invalid")
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE async_media_limit_reservations
		   SET terminalized_at=$1,terminal_outcome=$2::varchar(40),last_error=CASE WHEN $2::text='operation_state_expired'
		       THEN 'operation_state_expired' ELSE '' END,
		       terminal_owner='',terminal_claim_expires_at=NULL,updated_at=$1
		 WHERE job_id=$3 AND lease_fence=$4 AND operation_id=$5
		   AND terminalized_at IS NULL AND terminal_owner=$6 AND terminal_fence=$7`,
		now.UTC(), outcome, item.JobID, item.LeaseFence, item.OperationID,
		item.TerminalOwner, item.TerminalFence)
	if err != nil {
		return fmt.Errorf("mediajob: complete execution terminal: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (s *PostgresStore) ReleaseExecutionTerminalClaim(
	ctx context.Context,
	item ExecutionReservation,
	now time.Time,
	lastError string,
) error {
	lastError = strings.TrimSpace(lastError)
	if lastError == "" {
		lastError = "execution_terminal_reconcile_failed"
	}
	if len(lastError) > 512 {
		lastError = lastError[:512]
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE async_media_limit_reservations
		   SET last_error=$1,terminal_owner='',terminal_claim_expires_at=NULL,updated_at=$2
		 WHERE job_id=$3 AND lease_fence=$4 AND operation_id=$5
		   AND terminalized_at IS NULL AND terminal_owner=$6 AND terminal_fence=$7`,
		lastError, now.UTC(), item.JobID, item.LeaseFence, item.OperationID,
		item.TerminalOwner, item.TerminalFence)
	if err != nil {
		return fmt.Errorf("mediajob: release execution terminal claim: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
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
	var live accessgrantboundary.Snapshot
	job, live, err = resolveJobAccessBinding(ctx, tx, job)
	if err != nil {
		return Job{}, false, err
	}
	currentLimitSubject := executionSubject(live)
	currentLimitSubject.Model = job.RoutingModel
	if !sameExecutionSubject(record.ExpectedLimitSubject, currentLimitSubject) {
		return Job{}, false, domainError(ErrConflict, "submission_authority_revision_changed")
	}
	existing, existingErr := scanJobExact(tx.QueryRow(ctx, `
		SELECT `+jobColumns+`
		  FROM async_media_jobs j
		  LEFT JOIN async_media_charge_facts cf ON cf.job_id = j.job_id
		 WHERE j.org_id=$1 AND j.project_id=$2
		   AND j.idempotency_subject_type=$3 AND j.idempotency_subject_id=$4
		   AND j.idempotency_hash=$5`,
		job.Scope.OrgID, job.Scope.ProjectID, job.IdempotencySubjectType,
		job.IdempotencySubjectID, job.IdempotencyHash,
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
		    access_grant_id, access_credential_id, credential_ref,
		    idempotency_subject_type, idempotency_subject_id,
		    request_id, client_request_id, operation, profile_id,
		    requested_model, routing_model, request_sha256, idempotency_hash,
		    request_ciphertext, admission_state, queue_state, lease_state,
		    execution_state, provider_outcome, cancellation_state, object_state,
		    settlement_state, attempt, max_attempts, lease_fence,
		    next_attempt_at, created_at, updated_at
		) VALUES (
		    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
		    $18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33
		)
		ON CONFLICT (org_id, project_id, idempotency_subject_type,
		             idempotency_subject_id, idempotency_hash)
		DO NOTHING`,
		job.JobID, job.ContractVersion, job.Scope.OrgID, job.Scope.ProjectID,
		job.APIKeyID, job.AccessGrantID, job.AccessCredentialID, nullMediaCredentialRef(job.CredentialRef),
		job.IdempotencySubjectType, job.IdempotencySubjectID,
		job.RequestID, job.ClientRequestID, job.Operation,
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
		 WHERE j.org_id=$1 AND j.project_id=$2
		   AND j.idempotency_subject_type=$3 AND j.idempotency_subject_id=$4
		   AND j.idempotency_hash=$5`,
		job.Scope.OrgID, job.Scope.ProjectID, job.IdempotencySubjectType,
		job.IdempotencySubjectID, job.IdempotencyHash,
	)
	stored, err := scanJobExact(row)
	if err != nil {
		return Job{}, false, fmt.Errorf("mediajob: read after create: %w", err)
	}
	if !reused && s.sourceMaterializer != nil {
		captured := mediaSourceSnapshot(stored, live, stored.CreatedAt)
		if err := s.sourceStageTx(ctx, tx, stored, "admission", &captured); err != nil {
			return Job{}, false, err
		}
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
