package accessprovisioning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type objectAccessMode string

const (
	objectRead             objectAccessMode = "read"
	objectAdminMutation    objectAccessMode = "admin_mutation"
	objectDecision         objectAccessMode = "decision"
	objectCancel           objectAccessMode = "cancel"
	objectCredentialRead   objectAccessMode = "credential_read"
	objectCredentialMutate objectAccessMode = "credential_mutation"
)

type operationRecord struct {
	ID               int64
	OperationRef     string
	Kind             string
	SemanticHash     string
	ResultPayload    []byte
	PredecessorKeyID int64
	SuccessorKeyID   int64
}

func lockAccessOperationTx(ctx context.Context, tx pgx.Tx, orgID, actorUserID int64, clientRequestID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		fmt.Sprintf("access-operation:%d:%d:%s", orgID, actorUserID, clientRequestID))
	return err
}

func readAccessOperationTx(ctx context.Context, tx pgx.Tx, orgID, actorUserID int64, clientRequestID string) (operationRecord, bool, error) {
	var record operationRecord
	err := tx.QueryRow(ctx, `
		SELECT id,operation_ref,operation_kind,semantic_hash,result_payload,
		       COALESCE(predecessor_key_id,0),COALESCE(successor_key_id,0)
		FROM access_operations
		WHERE org_id=$1 AND actor_user_id=$2 AND client_request_id=$3
		FOR UPDATE`, orgID, actorUserID, clientRequestID).Scan(
		&record.ID, &record.OperationRef, &record.Kind, &record.SemanticHash,
		&record.ResultPayload, &record.PredecessorKeyID, &record.SuccessorKeyID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return operationRecord{}, false, nil
	}
	if err != nil {
		return operationRecord{}, false, fmt.Errorf("read access operation: %w", err)
	}
	return record, true, nil
}

func insertAccessOperationTx(
	ctx context.Context,
	tx pgx.Tx,
	orgID, projectID, grantID, actorUserID int64,
	clientRequestID, kind, semanticHash string,
	predecessorKeyID, successorKeyID int64,
	result any,
) (string, error) {
	payload, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal access operation result: %w", err)
	}
	operationRef := stableRef("op_", fmt.Sprint(orgID), fmt.Sprint(actorUserID), clientRequestID)
	_, err = tx.Exec(ctx, `
		INSERT INTO access_operations (
		  operation_ref,org_id,project_id,access_grant_id,actor_user_id,
		  client_request_id,operation_kind,semantic_hash,predecessor_key_id,
		  successor_key_id,result_payload
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,0),NULLIF($10,0),$11)`,
		operationRef, orgID, projectID, grantID, actorUserID, clientRequestID,
		kind, semanticHash, predecessorKeyID, successorKeyID, payload)
	if err != nil {
		return "", classifyDatabaseError("insert access operation", err)
	}
	return operationRef, nil
}

type rowScanner interface {
	Scan(...any) error
}

const requestSelect = `
	SELECT ar.id,ar.request_ref,ar.client_request_id,ar.semantic_hash,ar.semantic_revision,
	       ar.org_id,ar.project_id,ar.subject_type,COALESCE(ar.subject_user_id,0),
	       COALESCE(ar.subject_workload_application_id,0),ar.request_kind,
	       ar.requested_project_role,ar.requested_scope_mode,ar.requested_models,ar.requested_endpoints,
	       ar.requested_rpm_limit,ar.requested_rps_limit,ar.requested_concurrency_limit,
	       ar.environment,ar.production_use,ar.requested_valid_until,ar.submission_mode,
	       COALESCE(ar.sponsor_user_id,0),COALESCE(ar.target_access_grant_id,0),
	       COALESCE(ar.expected_grant_revision,0),ar.status,ar.decision_mode,
	       ar.policy_reason_codes,ar.decision_reason_codes,ar.policy_version,ar.submitted_by,
	       COALESCE(ar.decided_by,0),COALESCE(ar.cancelled_by,0),ar.submitted_at,
	       ar.decided_at,ar.cancelled_at,ar.revision
	FROM access_requests ar`

const grantSelect = `
	SELECT ag.id,ag.grant_ref,ag.access_request_id,ag.org_id,ag.project_id,
	       ag.subject_type,COALESCE(ag.subject_user_id,0),
	       COALESCE(ag.subject_workload_application_id,0),COALESCE(ag.sponsor_user_id,0),ag.project_role_snapshot,ag.scope_mode,
	       ag.allowed_models,ag.allowed_endpoints,ag.rpm_limit,ag.rps_limit,
	       ag.concurrency_limit,ag.environment,ag.production_use,ag.status,ag.revision,
	       ag.valid_from,ag.valid_until,ag.decision_mode,ag.policy_reason_codes,ag.decision_reason_codes,
	       ag.policy_version,ag.semantic_hash,ag.created_by,ag.updated_by,
	       ag.created_at,ag.updated_at
	FROM access_grants ag`

// grantRevisionSelect reconstructs the immutable Grant result committed for
// one approved request. Identity creation fields come from the stable grant
// row; every request-varying field comes from the append-only revision row so
// an idempotent replay cannot drift to a later grant revision or lifecycle.
const grantRevisionSelect = `
	SELECT agr.access_grant_id,agr.grant_ref,agr.access_request_id,agr.org_id,agr.project_id,
	       agr.subject_type,COALESCE(agr.subject_user_id,0),
	       COALESCE(agr.subject_workload_application_id,0),COALESCE(agr.sponsor_user_id,0),
	       agr.project_role_snapshot,agr.scope_mode,agr.allowed_models,agr.allowed_endpoints,
	       agr.rpm_limit,agr.rps_limit,agr.concurrency_limit,
	       agr.environment,agr.production_use,agr.status,agr.revision,agr.valid_from,agr.valid_until,
	       agr.decision_mode,agr.policy_reason_codes,agr.decision_reason_codes,
	       agr.policy_version,agr.semantic_hash,ag.created_by,agr.changed_by,
	       ag.created_at,agr.created_at
	FROM access_grant_revisions agr
	JOIN access_grants ag ON ag.id=agr.access_grant_id`

func readRequestByClientIDTx(ctx context.Context, tx pgx.Tx, orgID, actorUserID int64, clientRequestID string) (Request, bool, error) {
	return readRequestRow(tx.QueryRow(ctx, requestSelect+
		` WHERE ar.org_id=$1 AND ar.submitted_by=$2 AND ar.client_request_id=$3 FOR UPDATE`,
		orgID, actorUserID, clientRequestID))
}

func readRequestByRefTx(ctx context.Context, tx pgx.Tx, requestRef string) (Request, bool, error) {
	return readRequestRow(tx.QueryRow(ctx, requestSelect+` WHERE ar.request_ref=$1 FOR UPDATE`, requestRef))
}

func readRequestByRefScopedTx(ctx context.Context, tx pgx.Tx, requestRef string, scope ActorScope, mode objectAccessMode) (Request, bool, error) {
	if !scope.valid() {
		return Request{}, false, nil
	}
	predicate := requestAccessPredicate(mode, scope.EnforceOrgBoundary)
	return readRequestRow(tx.QueryRow(ctx, requestSelect+`
		WHERE ar.request_ref=$1 AND (`+predicate+`)
		FOR UPDATE`, requestRef, scope.UserID, scope.OrgID, scope.OrganizationRole))
}

func readRequestByRef(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, requestRef string) (Request, bool, error) {
	return readRequestRow(q.QueryRow(ctx, requestSelect+` WHERE ar.request_ref=$1`, requestRef))
}

func readRequestByRefScoped(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, requestRef string, scope ActorScope) (Request, bool, error) {
	return readRequestByRefScopedMode(ctx, q, requestRef, scope, objectRead)
}

func readRequestByRefScopedMode(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, requestRef string, scope ActorScope, mode objectAccessMode) (Request, bool, error) {
	if !scope.valid() {
		return Request{}, false, nil
	}
	return readRequestRow(q.QueryRow(ctx, requestSelect+`
		WHERE ar.request_ref=$1 AND (`+requestAccessPredicate(mode, scope.EnforceOrgBoundary)+`)`,
		requestRef, scope.UserID, scope.OrgID, scope.OrganizationRole))
}

func readRequestRow(row rowScanner) (Request, bool, error) {
	request, err := scanRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, false, nil
	}
	if err != nil {
		return Request{}, false, fmt.Errorf("read access request: %w", err)
	}
	return request, true, nil
}

func scanRequest(row rowScanner) (Request, error) {
	var request Request
	var modelsJSON, endpointsJSON []byte
	var rpm, rps, concurrency sql.NullInt64
	var requestedValidUntil, decidedAt, cancelledAt sql.NullTime
	if err := row.Scan(
		&request.ID, &request.RequestRef, &request.ClientRequestID, &request.SemanticHash,
		&request.SemanticRevision, &request.OrgID, &request.ProjectID, &request.Subject.Type,
		&request.Subject.UserID, &request.Subject.WorkloadApplicationID, &request.RequestKind,
		&request.Envelope.ProjectRole, &request.Envelope.ScopeMode, &modelsJSON, &endpointsJSON, &rpm, &rps, &concurrency,
		&request.Environment, &request.ProductionUse, &requestedValidUntil, &request.SubmissionMode, &request.SponsorUserID,
		&request.TargetAccessGrantID, &request.ExpectedGrantRevision,
		&request.Status, &request.DecisionMode, &request.PolicyReasonCodes, &request.DecisionReasonCodes, &request.PolicyVersion,
		&request.SubmittedBy, &request.DecidedBy, &request.CancelledBy, &request.SubmittedAt,
		&decidedAt, &cancelledAt, &request.Revision,
	); err != nil {
		return Request{}, err
	}
	if err := json.Unmarshal(modelsJSON, &request.Envelope.Models); err != nil {
		return Request{}, err
	}
	if err := json.Unmarshal(endpointsJSON, &request.Envelope.Endpoints); err != nil {
		return Request{}, err
	}
	request.Envelope.RPMLimit = nullIntPointer(rpm)
	request.Envelope.RPSLimit = nullIntPointer(rps)
	request.Envelope.ConcurrencyLimit = nullIntPointer(concurrency)
	if requestedValidUntil.Valid {
		value := requestedValidUntil.Time
		request.RequestedValidUntil = &value
	}
	if decidedAt.Valid {
		value := decidedAt.Time
		request.DecidedAt = &value
	}
	if cancelledAt.Valid {
		value := cancelledAt.Time
		request.CancelledAt = &value
	}
	return request, nil
}

func readGrantByRequestIDTx(ctx context.Context, tx pgx.Tx, requestID int64) (Grant, bool, error) {
	return readGrantRow(tx.QueryRow(ctx, grantRevisionSelect+`
		WHERE agr.access_request_id=$1`, requestID))
}

func readGrantByRequestID(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, requestID int64) (Grant, bool, error) {
	return readGrantRow(q.QueryRow(ctx, grantRevisionSelect+`
		WHERE agr.access_request_id=$1`, requestID))
}

func readGrantByRefTx(ctx context.Context, tx pgx.Tx, grantRef string) (Grant, bool, error) {
	return readGrantRow(tx.QueryRow(ctx, grantSelect+` WHERE ag.grant_ref=$1 FOR UPDATE`, grantRef))
}

func readGrantByRefScopedTx(ctx context.Context, tx pgx.Tx, grantRef string, scope ActorScope, mode objectAccessMode) (Grant, bool, error) {
	if !scope.valid() {
		return Grant{}, false, nil
	}
	return readGrantRow(tx.QueryRow(ctx, grantSelect+`
		WHERE ag.grant_ref=$1 AND (`+grantAccessPredicate(mode, scope.EnforceOrgBoundary)+`)
		FOR UPDATE`, grantRef, scope.UserID, scope.OrgID, scope.OrganizationRole))
}

func readGrantByRef(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, grantRef string) (Grant, bool, error) {
	return readGrantRow(q.QueryRow(ctx, grantSelect+` WHERE ag.grant_ref=$1`, grantRef))
}

func readGrantByRefScoped(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, grantRef string, scope ActorScope) (Grant, bool, error) {
	return readGrantByRefScopedMode(ctx, q, grantRef, scope, objectRead)
}

func readGrantByRefScopedMode(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, grantRef string, scope ActorScope, mode objectAccessMode) (Grant, bool, error) {
	if !scope.valid() {
		return Grant{}, false, nil
	}
	return readGrantRow(q.QueryRow(ctx, grantSelect+`
		WHERE ag.grant_ref=$1 AND (`+grantAccessPredicate(mode, scope.EnforceOrgBoundary)+`)`,
		grantRef, scope.UserID, scope.OrgID, scope.OrganizationRole))
}

func requestAccessPredicate(mode objectAccessMode, enforceOrgBoundary bool) string {
	platformClause := "$4='platform_admin'"
	if enforceOrgBoundary {
		platformClause = "($4='platform_admin' AND ar.org_id=$3)"
	}
	projectRoles := "'project_admin'"
	organizationRoles := "'org_admin'"
	if mode == objectRead {
		projectRoles = "'project_admin'"
		organizationRoles = "'org_admin'"
		return `
		` + platformClause + `
		OR (
		  ar.org_id=$3
		  AND (
		    $4 IN (` + organizationRoles + `)
		    OR EXISTS (
		      SELECT 1 FROM project_members access_pm
		      WHERE access_pm.project_id=ar.project_id
		        AND access_pm.user_id=$2
		        AND access_pm.role IN (` + projectRoles + `)
		    )
			    OR ar.submitted_by=$2
			    OR ar.subject_user_id=$2
			    OR ar.sponsor_user_id=$2
		  )
		)`
	} else if mode == objectDecision {
		return `
		` + platformClause + `
		OR (
		  ar.org_id=$3
		  AND (
		    $4='org_admin'
		    OR EXISTS (
		      SELECT 1 FROM project_members access_pm
		      WHERE access_pm.project_id=ar.project_id
		        AND access_pm.user_id=$2
		        AND access_pm.role='project_admin'
		    )
		  )
		)`
	} else if mode == objectCancel {
		orgBoundary := ""
		if enforceOrgBoundary {
			orgBoundary = "AND ar.org_id=$3"
		}
		return `
		$4 IN ('platform_admin','org_admin','billing_admin','org_viewer','member')
		` + orgBoundary + `
		AND (
		  ar.submitted_by=$2
		  OR (ar.org_id=$3 AND ar.subject_type='workload_application' AND ar.sponsor_user_id=$2)
		)`
	}
	return `
		` + platformClause + `
		OR (
		  ar.org_id=$3
		  AND (
		    $4 IN (` + organizationRoles + `)
		    OR EXISTS (
		      SELECT 1 FROM project_members access_pm
		      WHERE access_pm.project_id=ar.project_id
		        AND access_pm.user_id=$2
		        AND access_pm.role IN (` + projectRoles + `)
		    )
		  )
		)`
}

func grantAccessPredicate(mode objectAccessMode, enforceOrgBoundary bool) string {
	platformClause := "$4='platform_admin'"
	if enforceOrgBoundary {
		platformClause = "($4='platform_admin' AND ag.org_id=$3)"
	}
	projectClause := `EXISTS (
		SELECT 1 FROM project_members access_pm
		WHERE access_pm.project_id=ag.project_id
		  AND access_pm.user_id=$2
		  AND access_pm.role IN ('project_admin')
	)`
	organizationRoles := "'org_admin'"
	if mode == objectRead {
		projectClause = `EXISTS (
			SELECT 1 FROM project_members access_pm
			WHERE access_pm.project_id=ag.project_id
			  AND access_pm.user_id=$2
			  AND access_pm.role='project_admin'
		) OR (
			((ag.subject_type='user' AND ag.subject_user_id=$2)
			 OR (ag.subject_type='workload_application' AND ag.sponsor_user_id=$2)
			 OR EXISTS (SELECT 1 FROM access_requests owner_ar WHERE owner_ar.id=ag.access_request_id AND owner_ar.submitted_by=$2))
			AND EXISTS (
			  SELECT 1 FROM project_members access_pm
			  WHERE access_pm.project_id=ag.project_id
			    AND access_pm.user_id=$2
			    AND access_pm.role IN ('project_user','project_developer','developer')
			)
		)`
		organizationRoles = "'org_admin'"
	} else if mode == objectCredentialRead {
		projectClause = `EXISTS (
			SELECT 1 FROM project_members access_pm
			WHERE access_pm.project_id=ag.project_id
			  AND access_pm.user_id=$2
			  AND (
			    access_pm.role='project_admin'
			    OR (access_pm.role IN ('project_user','project_developer','developer')
			        AND ag.subject_type='user' AND ag.subject_user_id=$2)
			  )
		) OR (
			ag.subject_type='workload_application'
			AND ag.sponsor_user_id=$2
			AND EXISTS (
			  SELECT 1 FROM users sponsor_actor
			  WHERE sponsor_actor.id=$2 AND sponsor_actor.org_id=ag.org_id
			    AND sponsor_actor.status='active'
			    AND (
			      sponsor_actor.organization_role IN ('platform_admin','org_admin')
			      OR EXISTS (
			        SELECT 1 FROM project_members sponsor_pm
			        WHERE sponsor_pm.project_id=ag.project_id AND sponsor_pm.user_id=sponsor_actor.id
			          AND sponsor_pm.role IN ('project_admin','project_user','project_developer','developer')
			      )
			    )
			)
		)`
	} else if mode == objectCredentialMutate {
		projectClause = `EXISTS (
			SELECT 1 FROM project_members access_pm
			WHERE access_pm.project_id=ag.project_id
			  AND access_pm.user_id=$2
			  AND (
			    access_pm.role='project_admin'
			    OR (
			      access_pm.role IN ('project_user','project_developer','developer')
			      AND ag.subject_type='user' AND ag.subject_user_id=$2
			    )
			  )
		)`
	}
	return `
		` + platformClause + `
		OR (
		  ag.org_id=$3
		  AND ($4 IN (` + organizationRoles + `) OR ` + projectClause + `)
		)`
}

func readGrantRow(row rowScanner) (Grant, bool, error) {
	grant, err := scanGrant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, false, nil
	}
	if err != nil {
		return Grant{}, false, fmt.Errorf("read access grant: %w", err)
	}
	return grant, true, nil
}

func scanGrant(row rowScanner) (Grant, error) {
	var grant Grant
	var modelsJSON, endpointsJSON []byte
	var rpm, rps, concurrency sql.NullInt64
	var validUntil sql.NullTime
	if err := row.Scan(
		&grant.ID, &grant.GrantRef, &grant.AccessRequestID, &grant.OrgID, &grant.ProjectID,
		&grant.Subject.Type, &grant.Subject.UserID, &grant.Subject.WorkloadApplicationID,
		&grant.SponsorUserID,
		&grant.Envelope.ProjectRole, &grant.Envelope.ScopeMode, &modelsJSON, &endpointsJSON, &rpm, &rps, &concurrency,
		&grant.Environment, &grant.ProductionUse, &grant.Status, &grant.Revision, &grant.ValidFrom, &validUntil,
		&grant.DecisionMode, &grant.PolicyReasonCodes, &grant.DecisionReasonCodes, &grant.PolicyVersion, &grant.SemanticHash,
		&grant.CreatedBy, &grant.UpdatedBy, &grant.CreatedAt, &grant.UpdatedAt,
	); err != nil {
		return Grant{}, err
	}
	if err := json.Unmarshal(modelsJSON, &grant.Envelope.Models); err != nil {
		return Grant{}, err
	}
	if err := json.Unmarshal(endpointsJSON, &grant.Envelope.Endpoints); err != nil {
		return Grant{}, err
	}
	grant.Envelope.RPMLimit = nullIntPointer(rpm)
	grant.Envelope.RPSLimit = nullIntPointer(rps)
	grant.Envelope.ConcurrencyLimit = nullIntPointer(concurrency)
	if validUntil.Valid {
		value := validUntil.Time
		grant.ValidUntil = &value
	}
	return grant, nil
}

func nullIntPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

var _ rowScanner = pgx.Row(nil)
var _ = time.Time{}
