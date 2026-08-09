//go:build integration

package accessrequestlifecycle

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

type lifecycleFixture struct {
	pool                              *pgxpool.Pool
	orgID, projectID, actorID, userID int64
	sponsorID, workloadApplicationID  int64
}

func newLifecycleFixture(t *testing.T) lifecycleFixture {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := lifecycleFixture{pool: pool}
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('access-request-lifecycle','active','customer') RETURNING id`).Scan(&fixture.orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status,budget_period)
		VALUES ($1,'Access Request Lifecycle','access-request-lifecycle','active','none') RETURNING id`, fixture.orgID).Scan(&fixture.projectID))
	seedUser := func(name, organizationRole, role string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash',$3,$4,'active') RETURNING id`, fixture.orgID, name, organizationRole, role).Scan(&id))
		return id
	}
	fixture.actorID = seedUser("lifecycle-actor", "org_admin", "org_admin")
	fixture.userID = seedUser("lifecycle-user", "member", "member")
	fixture.sponsorID = seedUser("lifecycle-sponsor", "member", "developer")
	_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, fixture.projectID, fixture.sponsorID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO workload_applications (
		  contract_version,identity_contract_version,org_id,project_id,application_ref,logical_ref,
		  display_name,kind,environment,production_use,identity_mode,owner_team,status,
		  environment_locked_at,created_by,updated_by
		) VALUES (
		  'unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,
		  'app_lifecycle_fixture','workload_lifecycle_fixture','Lifecycle fixture','service',
		  'development',FALSE,'service_credential','platform','active',NOW(),$3,$3
		) RETURNING id`, fixture.orgID, fixture.projectID, fixture.actorID).Scan(&fixture.workloadApplicationID))
	return fixture
}

func (fixture lifecycleFixture) insertPendingUserRequests(t *testing.T, count int) {
	t.Helper()
	_, err := fixture.pool.Exec(context.Background(), `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,subject_user_id,
		  request_kind,requested_project_role,requested_scope_mode,requested_models,requested_endpoints,
		  environment,production_use,submission_mode,status,decision_mode,policy_reason_codes,
		  decision_reason_codes,policy_version,submitted_by
		)
		SELECT 'req_lifecycle_user_' || lpad(item::text,8,'0'),
		       'lifecycle-user-' || item::text,repeat('a',64),$1,$2,'user',$3,
		       'human_personal','project_user','project_default','[]'::jsonb,'[]'::jsonb,
		       'personal',FALSE,'self','needs_review','pending',ARRAY['first_cross_project'],
		       ARRAY[]::text[],'unirouter.enterprise-access-policy.v1',$3
		  FROM generate_series(1,$4) AS item`, fixture.orgID, fixture.projectID, fixture.userID, count)
	require.NoError(t, err)
}

func (fixture lifecycleFixture) insertPendingWorkloadRequests(t *testing.T, count int) {
	t.Helper()
	_, err := fixture.pool.Exec(context.Background(), `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
		  subject_workload_application_id,request_kind,requested_project_role,requested_scope_mode,
		  requested_models,requested_endpoints,environment,production_use,submission_mode,sponsor_user_id,
		  status,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,submitted_by
		)
		SELECT 'req_lifecycle_workload_' || lpad(item::text,8,'0'),
		       'lifecycle-workload-' || item::text,repeat('b',64),$1,$2,'workload_application',$3,
		       'workload_service','project_user','project_default','[]'::jsonb,'[]'::jsonb,
		       'development',FALSE,'delegated',$4,'needs_review','pending',ARRAY['workload_requires_review'],
		       ARRAY[]::text[],'unirouter.enterprise-access-policy.v1',$5
		  FROM generate_series(1,$6) AS item`, fixture.orgID, fixture.projectID,
		fixture.workloadApplicationID, fixture.sponsorID, fixture.actorID, count)
	require.NoError(t, err)
}

func TestTerminalSubjectLifecycleEvidenceUsesNullableProjectAndBoundedAggregateIntegration(t *testing.T) {
	const thresholdCount = 26_000
	t.Run("one user request uses null organization-wide project evidence", func(t *testing.T) {
		fixture := newLifecycleFixture(t)
		fixture.insertPendingUserRequests(t, 1)
		tx, err := fixture.pool.Begin(context.Background())
		require.NoError(t, err)
		defer tx.Rollback(context.Background()) //nolint:errcheck
		result, err := CancelForUser(context.Background(), tx, fixture.orgID, fixture.userID, fixture.actorID,
			"lifecycle-one-user", "lifecycle-one-user-client", time.Now().UTC())
		require.NoError(t, err)
		require.EqualValues(t, 1, result.RequestsCancelled)
		require.NoError(t, tx.Commit(context.Background()))

		var auditProjectID *int64
		require.NoError(t, fixture.pool.QueryRow(context.Background(), `
			SELECT project_id FROM audit_logs WHERE request_id='lifecycle-one-user'`).Scan(&auditProjectID))
		require.Nil(t, auditProjectID)
		var payload []byte
		require.NoError(t, fixture.pool.QueryRow(context.Background(), `
			SELECT payload FROM internal_outbox_events WHERE request_id='lifecycle-one-user'`).Scan(&payload))
		var evidence map[string]any
		require.NoError(t, json.Unmarshal(payload, &evidence))
		require.Nil(t, evidence["project_id"])
		detail := evidence["detail"].(map[string]any)
		require.EqualValues(t, 1, detail["requests_cancelled"])
		require.NotContains(t, detail, "request_ids")
		require.Len(t, detail["request_ids_sha256"], 64)
	})

	for _, test := range []struct {
		name      string
		seed      func(lifecycleFixture, *testing.T, int)
		cancel    func(context.Context, lifecycleFixture, time.Time) (Result, error)
		requestID string
	}{
		{
			name:      "user aggregate",
			seed:      func(f lifecycleFixture, t *testing.T, count int) { f.insertPendingUserRequests(t, count) },
			requestID: "lifecycle-user-threshold",
			cancel: func(ctx context.Context, f lifecycleFixture, now time.Time) (Result, error) {
				tx, err := f.pool.Begin(ctx)
				if err != nil {
					return Result{}, err
				}
				defer tx.Rollback(ctx) //nolint:errcheck
				result, err := CancelForUser(ctx, tx, f.orgID, f.userID, f.actorID,
					"lifecycle-user-threshold", "lifecycle-user-threshold-client", now)
				if err != nil {
					return Result{}, err
				}
				return result, tx.Commit(ctx)
			},
		},
		{
			name:      "workload aggregate",
			seed:      func(f lifecycleFixture, t *testing.T, count int) { f.insertPendingWorkloadRequests(t, count) },
			requestID: "lifecycle-workload-threshold",
			cancel: func(ctx context.Context, f lifecycleFixture, now time.Time) (Result, error) {
				tx, err := f.pool.Begin(ctx)
				if err != nil {
					return Result{}, err
				}
				defer tx.Rollback(ctx) //nolint:errcheck
				result, err := CancelForWorkload(ctx, tx, f.orgID, f.projectID, f.workloadApplicationID,
					f.actorID, "lifecycle-workload-threshold", "lifecycle-workload-threshold-client", now)
				if err != nil {
					return Result{}, err
				}
				return result, tx.Commit(ctx)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			test.seed(fixture, t, thresholdCount)
			result, err := test.cancel(context.Background(), fixture, time.Now().UTC())
			require.NoError(t, err)
			require.EqualValues(t, thresholdCount, result.RequestsCancelled)
			var pending, payloadBytes int
			require.NoError(t, fixture.pool.QueryRow(context.Background(), `
				SELECT COUNT(*) FROM access_requests WHERE org_id=$1 AND status='needs_review'`, fixture.orgID).Scan(&pending))
			require.Zero(t, pending)
			require.NoError(t, fixture.pool.QueryRow(context.Background(), `
				SELECT octet_length(payload::text) FROM internal_outbox_events WHERE request_id=$1`, test.requestID).Scan(&payloadBytes))
			require.Less(t, payloadBytes, 256*1024)
		})
	}
}

func TestTerminalSubjectLifecycleEvidenceFailureRollsBackRequestStateIntegration(t *testing.T) {
	fixture := newLifecycleFixture(t)
	fixture.insertPendingUserRequests(t, 1)
	_, err := fixture.pool.Exec(context.Background(), `
		CREATE FUNCTION reject_lifecycle_outbox_for_test() RETURNS trigger AS $$
		BEGIN
		  IF NEW.request_id='lifecycle-rollback' THEN
		    RAISE EXCEPTION 'forced lifecycle outbox failure';
		  END IF;
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_lifecycle_outbox_for_test
		BEFORE INSERT ON internal_outbox_events
		FOR EACH ROW EXECUTE FUNCTION reject_lifecycle_outbox_for_test()`)
	require.NoError(t, err)
	tx, err := fixture.pool.Begin(context.Background())
	require.NoError(t, err)
	_, err = CancelForUser(context.Background(), tx, fixture.orgID, fixture.userID, fixture.actorID,
		"lifecycle-rollback", "lifecycle-rollback-client", time.Now().UTC())
	require.Error(t, err)
	require.NoError(t, tx.Rollback(context.Background()))
	var status string
	require.NoError(t, fixture.pool.QueryRow(context.Background(), `
		SELECT status FROM access_requests WHERE org_id=$1 AND subject_user_id=$2`, fixture.orgID, fixture.userID).Scan(&status))
	require.Equal(t, "needs_review", status)
	var auditCount, outboxCount int
	require.NoError(t, fixture.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE request_id='lifecycle-rollback'`).Scan(&auditCount))
	require.NoError(t, fixture.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM internal_outbox_events WHERE request_id='lifecycle-rollback'`).Scan(&outboxCount))
	require.Zero(t, auditCount)
	require.Zero(t, outboxCount)
}
