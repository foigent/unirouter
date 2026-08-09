//go:build integration

package admin

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestTerminalSubjectLifecycleEvidenceFailureKeepsContainmentAtomicIntegration(t *testing.T) {
	t.Run("user offboard", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		ctx := context.Background()
		var orgID, projectID, actorID, targetID, keyID, sessionID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO orgs (name,status,org_type)
			VALUES ('lifecycle-user-atomicity','active','customer') RETURNING id`).Scan(&orgID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO projects (org_id,name,slug,status)
			VALUES ($1,'Lifecycle User Atomicity','lifecycle-user-atomicity','active') RETURNING id`, orgID).Scan(&projectID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,'lifecycle-user-admin','hash','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,'lifecycle-user-target','hash','member','developer','active') RETURNING id`, orgID).Scan(&targetID))
		_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, targetID)
		require.NoError(t, err)
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO admin_sessions (user_id,sid_hash,issued_at,expires_at)
			VALUES ($1,$2,NOW()-INTERVAL '1 minute',NOW()+INTERVAL '1 hour') RETURNING id`,
			targetID, bytes.Repeat([]byte{0x41}, 32)).Scan(&sessionID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO api_keys (
			  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,purpose
			) VALUES ($1,$2,$3,'personal','lifecycle rollback key',repeat('a',64),
				  'ur-life-roll','active','') RETURNING id`, orgID, projectID, targetID).Scan(&keyID))
		_, err = pool.Exec(ctx, `
			INSERT INTO access_requests (
			  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,subject_user_id,
			  request_kind,requested_project_role,requested_scope_mode,environment,production_use,
			  submission_mode,status,decision_mode,policy_reason_codes,decision_reason_codes,
			  policy_version,submitted_by
			) VALUES (
			  'req_lifecycle_user_atomicity','lifecycle-user-pending',repeat('b',64),$1,$2,'user',$3,
			  'human_personal','project_user','project_default','personal',FALSE,'self','needs_review',
			  'pending',ARRAY['first_cross_project'],ARRAY[]::TEXT[],
			  'unirouter.enterprise-access-policy.v1',$3
				)`, orgID, projectID, targetID)
		require.NoError(t, err)
		var initialAuthEpoch int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT auth_epoch FROM users WHERE id=$1`, targetID).Scan(&initialAuthEpoch))
		installLifecycleEvidenceFailure(t, ctx, pool)

		handler := NewHandler(pool, "lifecycle-user-jwt", []byte("0123456789abcdef01234567"), nil)
		request := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", targetID), nil)
		request.SetPathValue("id", fmt.Sprint(targetID))
		request.Header.Set(middleware.ClientRequestIDHeader, "lifecycle-user-offboard-rollback")
		request = withIntegrationCommittingActor(t, pool, request, actorID)
		response := httptest.NewRecorder()
		middleware.OperationIdentity(http.HandlerFunc(handler.DeleteUser)).ServeHTTP(response, request)
		require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())

		var userStatus, membershipRole, keyStatus, requestStatus string
		var authEpoch int64
		var sessionRevoked *time.Time
		require.NoError(t, pool.QueryRow(ctx, `SELECT status,auth_epoch FROM users WHERE id=$1`, targetID).Scan(&userStatus, &authEpoch))
		require.Equal(t, "active", userStatus)
		require.Equal(t, initialAuthEpoch, authEpoch)
		require.NoError(t, pool.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, targetID).Scan(&membershipRole))
		require.Equal(t, "project_user", membershipRole)
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id=$1`, keyID).Scan(&keyStatus))
		require.Equal(t, "active", keyStatus)
		require.NoError(t, pool.QueryRow(ctx, `SELECT revoked_at FROM admin_sessions WHERE id=$1`, sessionID).Scan(&sessionRevoked))
		require.Nil(t, sessionRevoked)
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM access_requests WHERE request_ref='req_lifecycle_user_atomicity'`).Scan(&requestStatus))
		require.Equal(t, "needs_review", requestStatus)
		assertNoLifecycleEvidence(t, ctx, pool)
	})

	t.Run("workload archive", func(t *testing.T) {
		pool := testutil.SetupMigratedTestDB(t)
		ctx := context.Background()
		var orgID, projectID, actorID, workloadID, keyID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO orgs (name,status,org_type)
			VALUES ('lifecycle-workload-atomicity','active','customer') RETURNING id`).Scan(&orgID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO projects (org_id,name,slug,status)
			VALUES ($1,'Lifecycle Workload Atomicity','lifecycle-workload-atomicity','active') RETURNING id`, orgID).Scan(&projectID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,'lifecycle-workload-admin','hash','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO workload_applications (
			  contract_version,identity_contract_version,org_id,project_id,application_ref,logical_ref,
			  display_name,kind,environment,production_use,identity_mode,owner_team,status,
			  environment_locked_at,created_by,updated_by
			) VALUES (
			  'unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,
			  'app_lifecycle_workload_atomicity','workload_lifecycle_workload_atomicity',
			  'Lifecycle workload atomicity','service','development',FALSE,'service_credential',
			  'platform','active',NOW(),$3,$3
			) RETURNING id`, orgID, projectID, actorID).Scan(&workloadID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO api_keys (
			  org_id,project_id,workload_application_id,key_type,name,key_hash,key_prefix,status,purpose
			) VALUES ($1,$2,$3,'service','workload lifecycle rollback key',repeat('c',64),
				  'ur-work-roll','active','') RETURNING id`, orgID, projectID, workloadID).Scan(&keyID))
		_, err := pool.Exec(ctx, `
			INSERT INTO access_requests (
			  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
			  subject_workload_application_id,request_kind,requested_project_role,requested_scope_mode,
			  environment,production_use,submission_mode,sponsor_user_id,status,decision_mode,
			  policy_reason_codes,decision_reason_codes,policy_version,submitted_by
			) VALUES (
			  'req_lifecycle_workload_atomicity','lifecycle-workload-pending',repeat('d',64),$1,$2,
			  'workload_application',$3,'workload_service','project_user','project_default',
			  'development',FALSE,'delegated',$4,'needs_review','pending',
			  ARRAY['workload_requires_review'],ARRAY[]::TEXT[],
			  'unirouter.enterprise-access-policy.v1',$4
			)`, orgID, projectID, workloadID, actorID)
		require.NoError(t, err)
		installLifecycleEvidenceFailure(t, ctx, pool)

		handler := NewHandler(pool, "lifecycle-workload-jwt", []byte("0123456789abcdef01234567"), nil)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/workload-applications/app_lifecycle_workload_atomicity/archive",
			bytes.NewBufferString(fmt.Sprintf(`{"org_id":%d,"expected_revision":1}`, orgID)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(middleware.ClientRequestIDHeader, "lifecycle-workload-archive-rollback")
		request.SetPathValue("ref", "app_lifecycle_workload_atomicity")
		request = withIntegrationCommittingActor(t, pool, request, actorID)
		response := httptest.NewRecorder()
		middleware.OperationIdentity(http.HandlerFunc(handler.ArchiveWorkloadApplication)).ServeHTTP(response, request)
		require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())

		var workloadStatus, keyStatus, requestStatus string
		var workloadRevision int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT status,revision FROM workload_applications WHERE id=$1`, workloadID).Scan(&workloadStatus, &workloadRevision))
		require.Equal(t, "active", workloadStatus)
		require.EqualValues(t, 1, workloadRevision)
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id=$1`, keyID).Scan(&keyStatus))
		require.Equal(t, "active", keyStatus)
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM access_requests WHERE request_ref='req_lifecycle_workload_atomicity'`).Scan(&requestStatus))
		require.Equal(t, "needs_review", requestStatus)
		assertNoLifecycleEvidence(t, ctx, pool)
	})
}

func installLifecycleEvidenceFailure(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_subject_lifecycle_evidence_for_test() RETURNS trigger AS $$
		BEGIN
		  IF NEW.payload->>'action'='access_request.subject_lifecycle_cancel' THEN
		    RAISE EXCEPTION 'forced subject lifecycle evidence failure';
		  END IF;
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_subject_lifecycle_evidence_for_test
		BEFORE INSERT ON internal_outbox_events
		FOR EACH ROW EXECUTE FUNCTION reject_subject_lifecycle_evidence_for_test()`)
	require.NoError(t, err)
}

func assertNoLifecycleEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var auditCount, outboxCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_logs WHERE action='access_request.subject_lifecycle_cancel'`).Scan(&auditCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM internal_outbox_events
		WHERE payload->>'action'='access_request.subject_lifecycle_cancel'`).Scan(&outboxCount))
	require.Zero(t, auditCount)
	require.Zero(t, outboxCount)
}
