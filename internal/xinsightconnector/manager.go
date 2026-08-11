package xinsightconnector

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/db"
)

type CommitAuthorizer interface {
	AuthorizeConnectorControl(context.Context, pgx.Tx, int64, int64, int64, string) error
}

type CommitAuthorizerFunc func(context.Context, pgx.Tx, int64, int64, int64, string) error

func (fn CommitAuthorizerFunc) AuthorizeConnectorControl(
	ctx context.Context,
	tx pgx.Tx,
	actorUserID, orgID, projectID int64,
	action string,
) error {
	return fn(ctx, tx, actorUserID, orgID, projectID, action)
}

type SourceAuthority interface {
	Enabled() bool
	SourceInstance() string
}

type Manager struct {
	db         db.DBQuerier
	authorizer CommitAuthorizer
	source     SourceAuthority
	now        func() time.Time
	token      func() (string, error)
}

func NewManager(database db.DBQuerier, authorizer CommitAuthorizer, source SourceAuthority) (*Manager, error) {
	if database == nil || authorizer == nil || source == nil {
		return nil, fmt.Errorf("%w: connector dependencies are required", ErrInvalid)
	}
	return &Manager{
		db:         database,
		authorizer: authorizer,
		source:     source,
		now:        time.Now,
		token:      randomLeaseToken,
	}, nil
}

// CreateInstallation consumes only a consent already validated by the caller's
// invitation boundary. It never parses an X-Insight package or contacts X.
// The commit authorizer rechecks the live local actor/project authority before
// the project and Source scope become durable.
func (m *Manager) CreateInstallation(ctx context.Context, input Consent) (Installation, Operation, error) {
	sourceInstance := m.source.SourceInstance()
	if !m.source.Enabled() || !validSourceInstance(sourceInstance) {
		return Installation{}, Operation{}, fmt.Errorf("%w: completed feed Source authority is unavailable", ErrUnavailable)
	}
	input, sourceBinding, sourceBindingSHA, err := normalizeConsent(input, sourceInstance)
	if err != nil {
		return Installation{}, Operation{}, err
	}
	now := m.now().UTC().Truncate(time.Microsecond)
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return Installation{}, Operation{}, fmt.Errorf("%w: begin installation: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	if err := m.authorizer.AuthorizeConnectorControl(
		ctx, tx, input.ConsentedByUserID, input.OrgID, input.ProjectID, ActionConnect,
	); err != nil {
		return Installation{}, Operation{}, err
	}

	existing, err := readInstallation(ctx, tx, input.InstallationID, input.OrgID, input.ProjectID, true)
	if err == nil {
		if existing.XDeploymentID != input.XDeploymentID ||
			existing.InvitationSHA256 != input.InvitationSHA256 ||
			existing.EventScope != input.EventScope ||
			existing.SourceInstance != sourceBinding.SourceInstance ||
			existing.SourceBindingSHA256 != sourceBindingSHA {
			return Installation{}, Operation{}, ErrConflict
		}
		operation, operationErr := readOperationByInstallationAction(ctx, tx, input.InstallationID, ActionConnect)
		if operationErr != nil {
			return Installation{}, Operation{}, operationErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return Installation{}, Operation{}, fmt.Errorf("%w: commit installation replay: %v", ErrUnavailable, commitErr)
		}
		return existing, operation, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Installation{}, Operation{}, err
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO completed_execution_feed_connections (org_id, project_id, status)
VALUES ($1,$2,'paused')
ON CONFLICT (org_id, project_id) DO NOTHING`, input.OrgID, input.ProjectID); err != nil {
		return Installation{}, Operation{}, fmt.Errorf("%w: ensure paused Source: %v", ErrUnavailable, err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO xinsight_connector_installations (
    installation_id, org_id, project_id, x_deployment_id, invitation_sha256,
    event_scope, source_instance, source_binding_sha256, desired_state,
    observed_state, state_revision, control_fence, consented_by,
    consented_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'active','provisioning',1,1,$9,$10,$10,$10)`,
		input.InstallationID, input.OrgID, input.ProjectID, input.XDeploymentID,
		input.InvitationSHA256, input.EventScope, sourceBinding.SourceInstance,
		sourceBindingSHA, input.ConsentedByUserID, now,
	)
	if err != nil {
		return Installation{}, Operation{}, classifyWriteError(err)
	}
	operationRef := operationRef(input.InstallationID, input.IdempotencyKey)
	_, err = tx.Exec(ctx, `
INSERT INTO xinsight_connector_operations (
    operation_ref, installation_id, org_id, project_id, action,
    idempotency_key, desired_state, state, requested_revision, control_fence,
    attempt_count, next_attempt_at, requested_by, created_at, updated_at)
VALUES ($1,$2,$3,$4,'connect',$5,'active','queued',1,1,0,$6,$7,$6,$6)`,
		operationRef, input.InstallationID, input.OrgID, input.ProjectID,
		input.IdempotencyKey, now, input.ConsentedByUserID,
	)
	if err != nil {
		return Installation{}, Operation{}, classifyWriteError(err)
	}
	installation, err := readInstallation(ctx, tx, input.InstallationID, input.OrgID, input.ProjectID, false)
	if err != nil {
		return Installation{}, Operation{}, err
	}
	operation, err := readOperation(ctx, tx, operationRef, false)
	if err != nil {
		return Installation{}, Operation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Installation{}, Operation{}, fmt.Errorf("%w: commit installation: %v", ErrUnavailable, err)
	}
	return installation, operation, nil
}

func (m *Manager) RequestOperation(ctx context.Context, input OperationRequest) (Operation, error) {
	input, desired, err := validateOperationRequest(input)
	if err != nil {
		return Operation{}, err
	}
	now := m.now().UTC().Truncate(time.Microsecond)
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return Operation{}, fmt.Errorf("%w: begin operation: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	if err := m.authorizer.AuthorizeConnectorControl(
		ctx, tx, input.RequestedByUserID, input.OrgID, input.ProjectID, input.Action,
	); err != nil {
		return Operation{}, err
	}
	installation, err := readInstallation(ctx, tx, input.InstallationID, input.OrgID, input.ProjectID, true)
	if err != nil {
		return Operation{}, err
	}
	if installation.ObservedState == ObservedDisconnected || installation.DesiredState == DesiredDisconnected {
		return Operation{}, ErrConflict
	}
	if existing, readErr := readOperationByIdempotency(ctx, tx, input.InstallationID, input.IdempotencyKey); readErr == nil {
		if existing.Action != input.Action {
			return Operation{}, ErrConflict
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return Operation{}, fmt.Errorf("%w: commit operation replay: %v", ErrUnavailable, commitErr)
		}
		return existing, nil
	} else if !errors.Is(readErr, ErrNotFound) {
		return Operation{}, readErr
	}
	if desired == "" {
		desired = installation.DesiredState
		if desired != DesiredActive && desired != DesiredPaused {
			return Operation{}, ErrConflict
		}
	}
	revision := installation.StateRevision + 1
	fence := installation.ControlFence + 1
	// A newer desired state supersedes every older not-yet-terminal control
	// operation in the same transaction that advances the installation fence.
	// The Connector agent can therefore never reclaim an operation whose result
	// the installation is already required to reject.
	if _, err := tx.Exec(ctx, `
UPDATE xinsight_connector_operations
SET state='cancelled', lease_owner=NULL, lease_token=NULL, lease_expires_at=NULL,
    result_code='superseded', result_sha256=NULL, error_code=NULL,
    completed_at=$2, updated_at=$2
WHERE installation_id=$1 AND state IN ('queued','claimed','retry_wait')`,
		input.InstallationID, now,
	); err != nil {
		return Operation{}, fmt.Errorf("%w: supersede prior operations: %v", ErrUnavailable, err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE xinsight_connector_installations
SET desired_state=$4, state_revision=$5, control_fence=$6, updated_at=$7
WHERE installation_id=$1 AND org_id=$2 AND project_id=$3`,
		input.InstallationID, input.OrgID, input.ProjectID, desired, revision, fence, now,
	); err != nil {
		return Operation{}, fmt.Errorf("%w: update desired state: %v", ErrUnavailable, err)
	}
	ref := operationRef(input.InstallationID, input.IdempotencyKey)
	_, err = tx.Exec(ctx, `
INSERT INTO xinsight_connector_operations (
    operation_ref, installation_id, org_id, project_id, action,
    idempotency_key, desired_state, state, requested_revision, control_fence,
    attempt_count, next_attempt_at, requested_by, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,'queued',$8,$9,0,$10,$11,$10,$10)`,
		ref, input.InstallationID, input.OrgID, input.ProjectID, input.Action,
		input.IdempotencyKey, desired, revision, fence, now, input.RequestedByUserID,
	)
	if err != nil {
		return Operation{}, classifyWriteError(err)
	}
	operation, err := readOperation(ctx, tx, ref, false)
	if err != nil {
		return Operation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Operation{}, fmt.Errorf("%w: commit operation: %v", ErrUnavailable, err)
	}
	return operation, nil
}

// ClaimNext is an inert repository seam until a later batch explicitly starts
// the Connector agent. Tests may call it with a fake counterpart.
func (m *Manager) ClaimNext(ctx context.Context, owner string, leaseDuration time.Duration) (Operation, bool, error) {
	owner = strings.TrimSpace(strings.ToLower(owner))
	if !identityPattern.MatchString(owner) || leaseDuration < time.Second || leaseDuration > 10*time.Minute {
		return Operation{}, false, ErrInvalid
	}
	now := m.now().UTC().Truncate(time.Microsecond)
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return Operation{}, false, fmt.Errorf("%w: begin claim: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	var ref string
	err = tx.QueryRow(ctx, `
SELECT candidate.operation_ref
FROM xinsight_connector_operations AS candidate
JOIN xinsight_connector_installations AS installation
  ON installation.installation_id=candidate.installation_id
 AND installation.org_id=candidate.org_id
 AND installation.project_id=candidate.project_id
WHERE (
        (candidate.state IN ('queued','retry_wait') AND candidate.next_attempt_at <= $1)
        OR (candidate.state='claimed' AND candidate.lease_expires_at <= $1)
      )
  AND candidate.control_fence=installation.control_fence
  AND installation.observed_state<>'disconnected'
  AND NOT EXISTS (
        SELECT 1 FROM xinsight_connector_operations AS active
        WHERE active.installation_id=candidate.installation_id
          AND active.operation_ref<>candidate.operation_ref
          AND active.state='claimed' AND active.lease_expires_at > $1
      )
ORDER BY candidate.next_attempt_at, candidate.created_at, candidate.operation_ref
FOR UPDATE OF installation SKIP LOCKED
LIMIT 1`, now).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return Operation{}, false, fmt.Errorf("%w: commit empty claim: %v", ErrUnavailable, commitErr)
		}
		return Operation{}, false, nil
	}
	if err != nil {
		return Operation{}, false, fmt.Errorf("%w: select claim: %v", ErrUnavailable, err)
	}
	// The installation row is the shared control lane for request, completion,
	// retry and claim paths. Locking it before the operation preserves one total
	// order and lets the retry-exhausted path update observed state without an
	// installation<->operation deadlock.
	operation, err := readOperation(ctx, tx, ref, true)
	if err != nil {
		return Operation{}, false, err
	}
	if operation.AttemptCount >= MaxAttempts {
		if err := terminalizeExhausted(ctx, tx, operation, now); err != nil {
			return Operation{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Operation{}, false, fmt.Errorf("%w: commit exhausted operation: %v", ErrUnavailable, err)
		}
		return Operation{}, false, nil
	}
	leaseToken, err := m.token()
	if err != nil {
		return Operation{}, false, fmt.Errorf("%w: lease token: %v", ErrUnavailable, err)
	}
	leaseExpires := now.Add(leaseDuration)
	if _, err := tx.Exec(ctx, `
UPDATE xinsight_connector_operations
SET state='claimed', attempt_count=attempt_count+1, lease_owner=$2,
    lease_token=$3, lease_expires_at=$4, updated_at=$5
WHERE operation_ref=$1`, ref, owner, leaseToken, leaseExpires, now); err != nil {
		return Operation{}, false, fmt.Errorf("%w: claim operation: %v", ErrUnavailable, err)
	}
	operation, err = readOperation(ctx, tx, ref, false)
	if err != nil {
		return Operation{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Operation{}, false, fmt.Errorf("%w: commit claim: %v", ErrUnavailable, err)
	}
	return operation, true, nil
}

func (m *Manager) CompleteOperation(
	ctx context.Context,
	operationRefValue, leaseOwner, leaseToken string,
	outcome OperationOutcome,
) (Installation, Operation, error) {
	operationRefValue = strings.TrimSpace(strings.ToLower(operationRefValue))
	leaseOwner = strings.TrimSpace(strings.ToLower(leaseOwner))
	leaseToken = strings.TrimSpace(strings.ToLower(leaseToken))
	if !regexpOperationRef(operationRefValue) || !identityPattern.MatchString(leaseOwner) || len(leaseToken) != 32 {
		return Installation{}, Operation{}, ErrInvalid
	}
	now := m.now().UTC().Truncate(time.Microsecond)
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return Installation{}, Operation{}, fmt.Errorf("%w: begin completion: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	identity, err := readOperation(ctx, tx, operationRefValue, false)
	if err != nil {
		return Installation{}, Operation{}, err
	}
	installation, err := readInstallation(ctx, tx, identity.InstallationID, identity.OrgID, identity.ProjectID, true)
	if err != nil {
		return Installation{}, Operation{}, err
	}
	operation, err := readOperation(ctx, tx, operationRefValue, true)
	if err != nil {
		return Installation{}, Operation{}, err
	}
	outcome, err = validateOutcome(operation.Action, outcome)
	if err != nil {
		return Installation{}, Operation{}, err
	}
	if operation.State != OperationClaimed || operation.LeaseOwner != leaseOwner || operation.LeaseToken != leaseToken ||
		operation.LeaseExpiresAt == nil || !operation.LeaseExpiresAt.After(now) ||
		installation.ControlFence != operation.ControlFence {
		return Installation{}, Operation{}, ErrConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE xinsight_connector_operations
SET state='succeeded', lease_owner=NULL, lease_token=NULL, lease_expires_at=NULL,
    result_code=$2, result_sha256=NULLIF($3,''), error_code=NULL,
    completed_at=$4, updated_at=$4
WHERE operation_ref=$1`, operationRefValue, outcome.ResultCode, outcome.ResultSHA256, now); err != nil {
		return Installation{}, Operation{}, fmt.Errorf("%w: complete operation: %v", ErrUnavailable, err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE xinsight_connector_installations
SET observed_state=$4,
    bootstrap_receipt_sha256=COALESCE(NULLIF($5,''), bootstrap_receipt_sha256),
    last_successful_sync_at=COALESCE($6, last_successful_sync_at),
    last_error_code=NULL, last_error_at=NULL,
    state_revision=state_revision+1, updated_at=$7
WHERE installation_id=$1 AND org_id=$2 AND project_id=$3`,
		installation.InstallationID, installation.OrgID, installation.ProjectID,
		outcome.ObservedState, outcome.BootstrapReceiptSHA256, outcome.SuccessfulSyncAt, now,
	); err != nil {
		return Installation{}, Operation{}, fmt.Errorf("%w: update observed state: %v", ErrUnavailable, err)
	}
	installation, err = readInstallation(ctx, tx, identity.InstallationID, identity.OrgID, identity.ProjectID, false)
	if err != nil {
		return Installation{}, Operation{}, err
	}
	operation, err = readOperation(ctx, tx, operationRefValue, false)
	if err != nil {
		return Installation{}, Operation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Installation{}, Operation{}, fmt.Errorf("%w: commit completion: %v", ErrUnavailable, err)
	}
	return installation, operation, nil
}

func (m *Manager) RetryOperation(
	ctx context.Context,
	operationRefValue, leaseOwner, leaseToken, errorCode string,
	delay time.Duration,
) (Operation, error) {
	operationRefValue = strings.TrimSpace(strings.ToLower(operationRefValue))
	leaseOwner = strings.TrimSpace(strings.ToLower(leaseOwner))
	leaseToken = strings.TrimSpace(strings.ToLower(leaseToken))
	errorCode = strings.TrimSpace(strings.ToLower(errorCode))
	if !regexpOperationRef(operationRefValue) || !identityPattern.MatchString(leaseOwner) || len(leaseToken) != 32 ||
		!validMachineToken(errorCode) || delay < time.Second || delay > time.Hour {
		return Operation{}, ErrInvalid
	}
	now := m.now().UTC().Truncate(time.Microsecond)
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return Operation{}, fmt.Errorf("%w: begin retry: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	identity, err := readOperation(ctx, tx, operationRefValue, false)
	if err != nil {
		return Operation{}, err
	}
	installation, err := readInstallation(ctx, tx, identity.InstallationID, identity.OrgID, identity.ProjectID, true)
	if err != nil {
		return Operation{}, err
	}
	operation, err := readOperation(ctx, tx, operationRefValue, true)
	if err != nil {
		return Operation{}, err
	}
	if operation.State != OperationClaimed || operation.LeaseOwner != leaseOwner || operation.LeaseToken != leaseToken ||
		operation.LeaseExpiresAt == nil || !operation.LeaseExpiresAt.After(now) ||
		installation.ControlFence != operation.ControlFence {
		return Operation{}, ErrConflict
	}
	if operation.AttemptCount >= MaxAttempts {
		if err := terminalizeFailed(ctx, tx, operation, errorCode, now); err != nil {
			return Operation{}, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
UPDATE xinsight_connector_operations
SET state='retry_wait', lease_owner=NULL, lease_token=NULL, lease_expires_at=NULL,
    next_attempt_at=$2, error_code=$3, updated_at=$4
WHERE operation_ref=$1`, operationRefValue, now.Add(delay), errorCode, now); err != nil {
			return Operation{}, fmt.Errorf("%w: schedule retry: %v", ErrUnavailable, err)
		}
		if _, err := tx.Exec(ctx, `
UPDATE xinsight_connector_installations
SET last_error_code=$4, last_error_at=$5, updated_at=$5
WHERE installation_id=$1 AND org_id=$2 AND project_id=$3`,
			installation.InstallationID, installation.OrgID, installation.ProjectID, errorCode, now); err != nil {
			return Operation{}, fmt.Errorf("%w: record retry error: %v", ErrUnavailable, err)
		}
	}
	operation, err = readOperation(ctx, tx, operationRefValue, false)
	if err != nil {
		return Operation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Operation{}, fmt.Errorf("%w: commit retry: %v", ErrUnavailable, err)
	}
	return operation, nil
}

func (m *Manager) ReadProjectState(ctx context.Context, orgID, projectID int64) (ProjectState, error) {
	if orgID <= 0 || projectID <= 0 {
		return ProjectState{}, ErrInvalid
	}
	state := ProjectState{Health: ProjectHealth{ProductStatus: ProductNotConnected, FeedConnectionStatus: "not_configured"}}
	installation, err := readLiveProjectInstallation(ctx, m.db, orgID, projectID)
	if err == nil {
		state.Installation = &installation
		state.Health.LastSuccessfulSyncAt = installation.LastSuccessfulSyncAt
		if operation, operationErr := readLatestOperation(ctx, m.db, installation.InstallationID); operationErr == nil {
			state.LatestOperation = &operation
		} else if !errors.Is(operationErr, ErrNotFound) {
			return ProjectState{}, operationErr
		}
	} else if !errors.Is(err, ErrNotFound) {
		return ProjectState{}, err
	}

	var connectionFound bool
	var lastPull sql.NullTime
	var projectStatus, orgStatus string
	if err := m.db.QueryRow(ctx, `
SELECT project.status, organization.status,
       COALESCE(connection.status,'not_configured'),
       COALESCE(connection.acknowledged_sequence,0), connection.last_authenticated_pull_at,
       connection.org_id IS NOT NULL,
       COUNT(key.id) FILTER (WHERE key.status='feed_active'),
       COUNT(key.id) FILTER (WHERE key.status='feed_staged')
FROM projects AS project
JOIN orgs AS organization ON organization.id=project.org_id
LEFT JOIN completed_execution_feed_connections AS connection
  ON connection.org_id=project.org_id AND connection.project_id=project.id
LEFT JOIN api_keys AS key
  ON key.org_id=project.org_id AND key.project_id=project.id
 AND key.purpose='xinsight.feed.read'
WHERE project.org_id=$1 AND project.id=$2
GROUP BY project.status, organization.status,
         connection.status, connection.acknowledged_sequence,
         connection.last_authenticated_pull_at, connection.org_id`, orgID, projectID).Scan(
		&projectStatus, &orgStatus, &state.Health.FeedConnectionStatus, &state.AcknowledgedSequence, &lastPull,
		&connectionFound, &state.Health.ActiveCredentialCount, &state.Health.StagedCredentialCount,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectState{}, ErrNotFound
		}
		return ProjectState{}, fmt.Errorf("%w: read project feed state: %v", ErrUnavailable, err)
	}
	if lastPull.Valid {
		value := lastPull.Time.UTC()
		state.Health.LastAuthenticatedPullAt = &value
	}
	if err := m.db.QueryRow(ctx, `
SELECT COALESCE(MAX(item.id),0),
       COALESCE((SELECT retained_after_id FROM completed_execution_feed_states
                 WHERE org_id=$1 AND project_id=$2),0)
FROM completed_execution_feed_items AS item
WHERE item.org_id=$1 AND item.project_id=$2`, orgID, projectID).Scan(
		&state.HighWatermark, &state.RetainedAfter,
	); err != nil {
		return ProjectState{}, fmt.Errorf("%w: read project watermark: %v", ErrUnavailable, err)
	}
	if state.HighWatermark < state.RetainedAfter {
		state.HighWatermark = state.RetainedAfter
	}
	var boundedCount int64
	var oldest sql.NullTime
	if err := m.db.QueryRow(ctx, `
WITH bounded AS (
    SELECT created_at
    FROM completed_execution_feed_items
    WHERE org_id=$1 AND project_id=$2 AND id>$3
    ORDER BY id ASC
    LIMIT $4
)
SELECT COUNT(*), MIN(created_at) FROM bounded`,
		orgID, projectID, state.AcknowledgedSequence, HealthBacklogCap+1,
	).Scan(&boundedCount, &oldest); err != nil {
		return ProjectState{}, fmt.Errorf("%w: read bounded backlog: %v", ErrUnavailable, err)
	}
	state.Health.PendingItemCountCapped = boundedCount > HealthBacklogCap
	state.Health.PendingItemCount = boundedCount
	if state.Health.PendingItemCountCapped {
		state.Health.PendingItemCount = HealthBacklogCap
	}
	if oldest.Valid {
		value := oldest.Time.UTC()
		state.Health.OldestPendingAt = &value
		age := m.now().UTC().Sub(value)
		if age > 0 {
			state.Health.OldestPendingAgeSeconds = int64(age / time.Second)
		}
	}
	state.Health.CursorOutsideRetention = connectionFound && state.AcknowledgedSequence < state.RetainedAfter
	sourceAuthorityCurrent := projectStatus == "active" && orgStatus == "active" && m.source.Enabled()
	if sourceAuthorityCurrent {
		currentSource := m.source.SourceInstance()
		sourceAuthorityCurrent = validSourceInstance(currentSource) &&
			(state.Installation == nil || currentSource == state.Installation.SourceInstance)
	}
	state.Health.ProductStatus = productStatus(state.Installation, state.Health, sourceAuthorityCurrent)
	return state, nil
}

func productStatus(installation *Installation, health ProjectHealth, sourceAuthorityCurrent bool) string {
	if installation == nil {
		return ProductNotConnected
	}
	if !sourceAuthorityCurrent || health.CursorOutsideRetention || installation.ObservedState == ObservedAbnormal || installation.LastErrorCode != "" {
		return ProductAbnormal
	}
	if installation.ObservedState == ObservedConnected && health.FeedConnectionStatus == "active" &&
		health.ActiveCredentialCount == 1 && health.StagedCredentialCount == 0 {
		return ProductConnected
	}
	if installation.ObservedState == ObservedPaused || installation.DesiredState == DesiredPaused || health.FeedConnectionStatus == "paused" {
		return ProductPaused
	}
	return ProductConnecting
}

func terminalizeExhausted(ctx context.Context, tx pgx.Tx, operation Operation, now time.Time) error {
	return terminalizeFailed(ctx, tx, operation, "retry_exhausted", now)
}

func terminalizeFailed(ctx context.Context, tx pgx.Tx, operation Operation, errorCode string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
UPDATE xinsight_connector_operations
SET state='failed', lease_owner=NULL, lease_token=NULL, lease_expires_at=NULL,
    error_code=$2, completed_at=$3, updated_at=$3
WHERE operation_ref=$1`, operation.OperationRef, errorCode, now); err != nil {
		return fmt.Errorf("%w: fail operation: %v", ErrUnavailable, err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE xinsight_connector_installations
SET observed_state='abnormal', last_error_code=$4, last_error_at=$5,
    state_revision=state_revision+1, updated_at=$5
WHERE installation_id=$1 AND org_id=$2 AND project_id=$3`,
		operation.InstallationID, operation.OrgID, operation.ProjectID, errorCode, now); err != nil {
		return fmt.Errorf("%w: fail installation: %v", ErrUnavailable, err)
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func readInstallation(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, installationID string, orgID, projectID int64, forUpdate bool) (Installation, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	return scanInstallation(q.QueryRow(ctx, `
SELECT installation_id, x_deployment_id, invitation_sha256, org_id, project_id,
       event_scope, source_instance, source_binding_sha256, desired_state,
       observed_state, state_revision, control_fence, bootstrap_receipt_sha256,
       last_successful_sync_at, last_error_code, last_error_at,
       consented_at, created_at, updated_at
FROM xinsight_connector_installations
WHERE installation_id=$1 AND org_id=$2 AND project_id=$3`+lock,
		installationID, orgID, projectID))
}

func readLiveProjectInstallation(ctx context.Context, q db.DBQuerier, orgID, projectID int64) (Installation, error) {
	return scanInstallation(q.QueryRow(ctx, `
SELECT installation_id, x_deployment_id, invitation_sha256, org_id, project_id,
       event_scope, source_instance, source_binding_sha256, desired_state,
       observed_state, state_revision, control_fence, bootstrap_receipt_sha256,
       last_successful_sync_at, last_error_code, last_error_at,
       consented_at, created_at, updated_at
FROM xinsight_connector_installations
WHERE org_id=$1 AND project_id=$2 AND observed_state<>'disconnected'
ORDER BY updated_at DESC, installation_id
LIMIT 1`, orgID, projectID))
}

func scanInstallation(row rowScanner) (Installation, error) {
	var value Installation
	var bootstrap, lastError sql.NullString
	var lastSync, lastErrorAt sql.NullTime
	err := row.Scan(
		&value.InstallationID, &value.XDeploymentID, &value.InvitationSHA256,
		&value.OrgID, &value.ProjectID, &value.EventScope, &value.SourceInstance,
		&value.SourceBindingSHA256, &value.DesiredState, &value.ObservedState,
		&value.StateRevision, &value.ControlFence, &bootstrap, &lastSync,
		&lastError, &lastErrorAt, &value.ConsentedAt, &value.CreatedAt, &value.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Installation{}, ErrNotFound
	}
	if err != nil {
		return Installation{}, fmt.Errorf("%w: read installation: %v", ErrUnavailable, err)
	}
	value.SchemaVersion = InstallationSchemaVersion
	value.BootstrapReceiptSHA256 = bootstrap.String
	value.LastErrorCode = lastError.String
	if lastSync.Valid {
		value.LastSuccessfulSyncAt = timePointer(lastSync.Time)
	}
	if lastErrorAt.Valid {
		value.LastErrorAt = timePointer(lastErrorAt.Time)
	}
	value.ConsentedAt = value.ConsentedAt.UTC()
	value.CreatedAt = value.CreatedAt.UTC()
	value.UpdatedAt = value.UpdatedAt.UTC()
	return value, nil
}

func readOperation(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, ref string, forUpdate bool) (Operation, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	return scanOperation(q.QueryRow(ctx, operationSelect+` WHERE operation_ref=$1`+lock, ref))
}

func readOperationByIdempotency(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, installationID, key string) (Operation, error) {
	return scanOperation(q.QueryRow(ctx, operationSelect+`
WHERE installation_id=$1 AND idempotency_key=$2`, installationID, key))
}

func readOperationByInstallationAction(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, installationID, action string) (Operation, error) {
	return scanOperation(q.QueryRow(ctx, operationSelect+`
WHERE installation_id=$1 AND action=$2
ORDER BY created_at, operation_ref LIMIT 1`, installationID, action))
}

func readLatestOperation(ctx context.Context, q db.DBQuerier, installationID string) (Operation, error) {
	return scanOperation(q.QueryRow(ctx, operationSelect+`
WHERE installation_id=$1 ORDER BY created_at DESC, operation_ref DESC LIMIT 1`, installationID))
}

const operationSelect = `
SELECT operation_ref, installation_id, org_id, project_id, action,
       idempotency_key, desired_state, state, requested_revision, control_fence,
       attempt_count, next_attempt_at, lease_owner, lease_token, lease_expires_at,
       result_code, result_sha256, error_code, completed_at, created_at, updated_at
FROM xinsight_connector_operations`

func scanOperation(row rowScanner) (Operation, error) {
	var value Operation
	var leaseOwner, leaseToken, resultCode, resultSHA, errorCode sql.NullString
	var leaseExpires, completedAt sql.NullTime
	err := row.Scan(
		&value.OperationRef, &value.InstallationID, &value.OrgID, &value.ProjectID,
		&value.Action, &value.IdempotencyKey, &value.DesiredState, &value.State,
		&value.RequestedRevision, &value.ControlFence, &value.AttemptCount,
		&value.NextAttemptAt, &leaseOwner, &leaseToken, &leaseExpires,
		&resultCode, &resultSHA, &errorCode, &completedAt, &value.CreatedAt, &value.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Operation{}, ErrNotFound
	}
	if err != nil {
		return Operation{}, fmt.Errorf("%w: read operation: %v", ErrUnavailable, err)
	}
	value.SchemaVersion = OperationSchemaVersion
	value.LeaseOwner = leaseOwner.String
	value.LeaseToken = leaseToken.String
	value.ResultCode = resultCode.String
	value.ResultSHA256 = resultSHA.String
	value.ErrorCode = errorCode.String
	if leaseExpires.Valid {
		value.LeaseExpiresAt = timePointer(leaseExpires.Time)
	}
	if completedAt.Valid {
		value.CompletedAt = timePointer(completedAt.Time)
	}
	value.NextAttemptAt = value.NextAttemptAt.UTC()
	value.CreatedAt = value.CreatedAt.UTC()
	value.UpdatedAt = value.UpdatedAt.UTC()
	return value, nil
}

func classifyWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23514" || pgErr.Code == "23503") {
		return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
	}
	return fmt.Errorf("%w: write: %v", ErrUnavailable, err)
}

func randomLeaseToken() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func regexpOperationRef(value string) bool {
	if len(value) != 37 || !strings.HasPrefix(value, "xcop_") {
		return false
	}
	_, err := hex.DecodeString(value[5:])
	return err == nil
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
