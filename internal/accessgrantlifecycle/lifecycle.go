// Package accessgrantlifecycle owns the transaction-scoped AccessGrant state
// machine. Every lifecycle revision is appended before commit and permanent
// revocation uses the single logical-credential containment implementation.
package accessgrantlifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accesscredential"
)

const (
	ActionSuspend = "suspend"
	ActionResume  = "resume"
	ActionRevoke  = "revoke"

	ReasonManualSuspend                 = "explicit_suspend"
	ReasonManualResume                  = "explicit_resume"
	ReasonManualRevoke                  = "explicit_revoke"
	ReasonMembershipRemoved             = "membership_removed"
	ReasonMembershipNonCallingDowngrade = "membership_noncalling_downgrade"
	ReasonUserOffboard                  = "user_offboard"
	ReasonWorkloadArchive               = "workload_archive"
)

var (
	ErrNotFound          = errors.New("access grant not found")
	ErrRevisionConflict  = errors.New("access grant revision conflict")
	ErrInvalidTransition = errors.New("invalid access grant lifecycle transition")
)

type Result struct {
	GrantID            int64
	GrantRef           string
	PreviousStatus     string
	Status             string
	Revision           int64
	AlreadyApplied     bool
	CredentialFamilies int64
	SecretVersions     int64
	LegacyKeys         int64
}

type BatchResult struct {
	Grants             []Result
	CredentialFamilies int64
	SecretVersions     int64
	LegacyKeys         int64
}

// Change locks one exact Grant, applies one state transition, appends the
// immutable revision snapshot, and permanently contains credentials when the
// terminal state is revoked. expectedRevision=0 is reserved for internal
// containment paths that already hold the subject/member lock.
func Change(ctx context.Context, tx pgx.Tx, grantID, expectedRevision, actorUserID int64, action, reason string, at time.Time) (Result, error) {
	if grantID <= 0 || actorUserID <= 0 || !validActionReason(action, reason) {
		return Result{}, ErrInvalidTransition
	}
	var result Result
	if err := tx.QueryRow(ctx, `
		SELECT id,grant_ref,status,revision
		FROM access_grants WHERE id=$1 FOR UPDATE`, grantID).Scan(
		&result.GrantID, &result.GrantRef, &result.PreviousStatus, &result.Revision,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Result{}, ErrNotFound
		}
		return Result{}, fmt.Errorf("lock access grant: %w", err)
	}
	if expectedRevision > 0 && result.Revision != expectedRevision {
		return Result{}, ErrRevisionConflict
	}
	next, already, err := nextStatus(result.PreviousStatus, action)
	if err != nil {
		return Result{}, err
	}
	if already {
		result.Status = result.PreviousStatus
		result.AlreadyApplied = true
		return result, nil
	}
	previousRevision := result.Revision
	tag, err := tx.Exec(ctx, `
		UPDATE access_grants
		SET status=$2::VARCHAR,revision=revision+1,updated_by=$3,updated_at=$4::TIMESTAMPTZ,
		    suspended_at=CASE WHEN $2::VARCHAR='suspended' THEN $4::TIMESTAMPTZ ELSE NULL::TIMESTAMPTZ END,
		    revoked_at=CASE WHEN $2::VARCHAR='revoked' THEN $4::TIMESTAMPTZ ELSE NULL::TIMESTAMPTZ END
		WHERE id=$1 AND revision=$5 AND status=$6`, grantID, next, actorUserID, at, previousRevision, result.PreviousStatus)
	if err != nil {
		return Result{}, fmt.Errorf("update access grant lifecycle: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return Result{}, ErrRevisionConflict
	}
	result.Status = next
	result.Revision = previousRevision + 1
	if _, err := tx.Exec(ctx, `
		INSERT INTO access_grant_revisions (
		  access_grant_id,grant_ref,revision,access_request_id,org_id,project_id,
		  subject_type,subject_user_id,subject_workload_application_id,sponsor_user_id,
		  project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,
		  rpm_limit,rps_limit,concurrency_limit,environment,production_use,status,
		  valid_from,valid_until,decision_mode,policy_reason_codes,decision_reason_codes,
		  policy_version,semantic_hash,revision_kind,lifecycle_action,lifecycle_reason_code,
		  changed_by,created_at
		)
		SELECT id,grant_ref,revision,NULL,org_id,project_id,
		       subject_type,subject_user_id,subject_workload_application_id,sponsor_user_id,
		       project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,
		       rpm_limit,rps_limit,concurrency_limit,environment,production_use,status,
		       valid_from,valid_until,decision_mode,policy_reason_codes,decision_reason_codes,
		       policy_version,semantic_hash,'lifecycle',$2,$3,$4,$5
		FROM access_grants WHERE id=$1`, grantID, action, reason, actorUserID, at); err != nil {
		return Result{}, fmt.Errorf("append access grant lifecycle revision: %w", err)
	}
	if next == "revoked" {
		containment, err := accesscredential.RevokeForGrant(ctx, tx, grantID, actorUserID, at)
		if err != nil {
			return Result{}, err
		}
		result.CredentialFamilies = containment.CredentialFamilies
		result.SecretVersions = containment.SecretVersions
		result.LegacyKeys = containment.LegacyKeys
	}
	return result, nil
}

func RevokeUser(ctx context.Context, tx pgx.Tx, orgID, projectID, userID, actorUserID int64, reason string, at time.Time) (BatchResult, error) {
	query := `SELECT id FROM access_grants
		WHERE org_id=$1 AND subject_type='user' AND subject_user_id=$2 AND status<>'revoked'`
	args := []any{orgID, userID}
	if projectID > 0 {
		query += ` AND project_id=$3`
		args = append(args, projectID)
	}
	results, err := revokeMatching(ctx, tx, query, args, actorUserID, reason, at)
	if err != nil {
		return BatchResult{}, err
	}
	containment, err := accesscredential.RevokeForUser(ctx, tx, orgID, projectID, userID, actorUserID, at)
	if err != nil {
		return BatchResult{}, err
	}
	return summarize(results, containment), nil
}

func RevokeWorkload(ctx context.Context, tx pgx.Tx, orgID, projectID, workloadID, actorUserID int64, reason string, at time.Time) (BatchResult, error) {
	results, err := revokeMatching(ctx, tx, `SELECT id FROM access_grants
		WHERE org_id=$1 AND project_id=$2 AND subject_type='workload_application'
		  AND subject_workload_application_id=$3 AND status<>'revoked'`,
		[]any{orgID, projectID, workloadID}, actorUserID, reason, at)
	if err != nil {
		return BatchResult{}, err
	}
	containment, err := accesscredential.RevokeForWorkload(ctx, tx, orgID, projectID, workloadID, actorUserID, at)
	if err != nil {
		return BatchResult{}, err
	}
	return summarize(results, containment), nil
}

func summarize(results []Result, supplemental accesscredential.ContainmentResult) BatchResult {
	batch := BatchResult{Grants: results, CredentialFamilies: supplemental.CredentialFamilies,
		SecretVersions: supplemental.SecretVersions, LegacyKeys: supplemental.LegacyKeys}
	for _, result := range results {
		batch.CredentialFamilies += result.CredentialFamilies
		batch.SecretVersions += result.SecretVersions
		batch.LegacyKeys += result.LegacyKeys
	}
	return batch
}

func revokeMatching(ctx context.Context, tx pgx.Tx, query string, args []any, actorUserID int64, reason string, at time.Time) ([]Result, error) {
	rows, err := tx.Query(ctx, query+` ORDER BY id FOR UPDATE`, args...)
	if err != nil {
		return nil, fmt.Errorf("lock access grants for containment: %w", err)
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan access grant for containment: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate access grants for containment: %w", err)
	}
	rows.Close()
	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		result, err := Change(ctx, tx, id, 0, actorUserID, ActionRevoke, reason, at)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func validActionReason(action, reason string) bool {
	switch action {
	case ActionSuspend:
		return reason == ReasonManualSuspend
	case ActionResume:
		return reason == ReasonManualResume
	case ActionRevoke:
		return reason == ReasonManualRevoke || reason == ReasonMembershipRemoved ||
			reason == ReasonMembershipNonCallingDowngrade || reason == ReasonUserOffboard ||
			reason == ReasonWorkloadArchive
	default:
		return false
	}
}

func nextStatus(current, action string) (string, bool, error) {
	switch action {
	case ActionSuspend:
		if current == "active" {
			return "suspended", false, nil
		}
	case ActionResume:
		if current == "suspended" {
			return "active", false, nil
		}
	case ActionRevoke:
		if current == "revoked" {
			return "revoked", true, nil
		}
		if current == "active" || current == "suspended" {
			return "revoked", false, nil
		}
	}
	return "", false, ErrInvalidTransition
}
