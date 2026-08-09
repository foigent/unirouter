// Package accessrequestlifecycle closes pending AccessRequests when their
// terminal subject is permanently retired. It never changes immutable policy
// reasons and emits one bounded aggregate audit/outbox pair per lifecycle.
package accessrequestlifecycle

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessevidence"
)

const (
	ReasonSubjectOffboarded  = "subject_offboarded"
	ReasonWorkloadArchived   = "workload_archived"
	DecisionSubjectLifecycle = "subject_lifecycle"
)

type Result struct {
	RequestsCancelled int64
}

func CancelForUser(ctx context.Context, tx pgx.Tx, orgID, userID, actorUserID int64,
	requestID, clientRequestID string, at time.Time,
) (Result, error) {
	return cancel(ctx, tx, orgID, 0, "user", userID, actorUserID,
		ReasonSubjectOffboarded, requestID, clientRequestID, at)
}

func CancelForWorkload(ctx context.Context, tx pgx.Tx, orgID, projectID, workloadID, actorUserID int64,
	requestID, clientRequestID string, at time.Time,
) (Result, error) {
	return cancel(ctx, tx, orgID, projectID, "workload_application", workloadID, actorUserID,
		ReasonWorkloadArchived, requestID, clientRequestID, at)
}

func cancel(ctx context.Context, tx pgx.Tx, orgID, projectID int64, subjectType string,
	subjectID, actorUserID int64, reason, requestID, clientRequestID string, at time.Time,
) (Result, error) {
	if orgID <= 0 || subjectID <= 0 || actorUserID <= 0 {
		return Result{}, fmt.Errorf("invalid terminal access-request subject")
	}
	query := `SELECT id FROM access_requests
		WHERE org_id=$1 AND subject_type=$2 AND status='needs_review'`
	args := []any{orgID, subjectType}
	if subjectType == "user" {
		query += ` AND subject_user_id=$3`
		args = append(args, subjectID)
	} else {
		query += ` AND project_id=$3 AND subject_workload_application_id=$4`
		args = append(args, projectID, subjectID)
	}
	rows, err := tx.Query(ctx, query+` ORDER BY id FOR UPDATE`, args...)
	if err != nil {
		return Result{}, fmt.Errorf("lock terminal subject access requests: %w", err)
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return Result{}, fmt.Errorf("scan terminal subject access request: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, fmt.Errorf("iterate terminal subject access requests: %w", err)
	}
	rows.Close()
	if len(ids) == 0 {
		return Result{}, nil
	}

	update := `UPDATE access_requests
		SET status='cancelled',decision_mode=$1,decision_reason_codes=ARRAY[$2]::TEXT[],
		    cancelled_by=$3,cancelled_at=$4,revision=revision+1,updated_at=$4
		WHERE org_id=$5 AND subject_type=$6 AND status='needs_review'`
	updateArgs := []any{DecisionSubjectLifecycle, reason, actorUserID, at, orgID, subjectType}
	if subjectType == "user" {
		update += ` AND subject_user_id=$7`
		updateArgs = append(updateArgs, subjectID)
	} else {
		update += ` AND project_id=$7 AND subject_workload_application_id=$8`
		updateArgs = append(updateArgs, projectID, subjectID)
	}
	tag, err := tx.Exec(ctx, update, updateArgs...)
	if err != nil {
		return Result{}, fmt.Errorf("cancel terminal subject access requests: %w", err)
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return Result{}, fmt.Errorf("terminal subject access-request state changed")
	}
	resource := "user"
	if subjectType == "workload_application" {
		resource = "workload_application"
	}
	if err := accessevidence.WriteAuditAndOutbox(ctx, tx, orgID, projectID, actorUserID,
		"access_request.subject_lifecycle_cancel", resource, strconv.FormatInt(subjectID, 10),
		requestID, clientRequestID, map[string]any{
			"reason_code": reason, "requests_cancelled": len(ids), "request_ids": ids,
		}, at); err != nil {
		return Result{}, err
	}
	return Result{RequestsCancelled: int64(len(ids))}, nil
}
