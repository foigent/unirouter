//go:build integration

package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestUpdateAPIKeyReauthorizesAfterProjectMembershipRevocationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var orgID, projectID, actorID, keyID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('api-key-reauthorization-org', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status)
		 VALUES ($1, 'Authorization project', 'authorization-project', '', 'active') RETURNING id`, orgID,
	).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'api-key-reauthorization-admin', 'test-hash', 'project_admin', 'active') RETURNING id`, orgID,
	).Scan(&actorID))
	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'project_admin')`,
		projectID, actorID,
	)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO api_keys
		 (org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix, status, rpm_limit, qps_limit)
		 VALUES ($1, $2, NULL, 'shared', 'reauthorization-target', 'test-hash', 'ur-reauth', 'active', 60, 10)
		 RETURNING id`, orgID, projectID,
	).Scan(&keyID))

	beginReached := make(chan struct{})
	allowBegin := make(chan struct{})
	db := &apiKeyInterleavingDB{pool: pool, beginReached: beginReached, allowBegin: allowBegin}
	h := NewHandler(db, "test-secret", []byte("0123456789abcdef01234567"), nil)
	req := apiKeyIntegrationRequest(http.MethodPut, "/api/v1/keys/target", `{"status":"disabled"}`,
		actorID, orgID, auth.RoleProjectAdmin, "api-key-reauthorization-revoked")
	req.SetPathValue("id", stringInt64(keyID))
	recorderCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		h.UpdateAPIKey(recorder, req)
		recorderCh <- recorder
	}()

	select {
	case <-beginReached:
	case <-time.After(5 * time.Second):
		t.Fatal("API-key update did not reach its committing transaction after preauthorization")
	}

	revocationTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, lockProjectMembershipMutation(ctx, revocationTx, actorID))
	_, err = revocationTx.Exec(ctx,
		`DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, actorID,
	)
	require.NoError(t, err)
	require.NoError(t, revocationTx.Commit(ctx))
	close(allowBegin)

	var recorder *httptest.ResponseRecorder
	select {
	case recorder = <-recorderCh:
	case <-time.After(5 * time.Second):
		t.Fatal("API-key update did not finish after revocation committed")
	}
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, keyID).Scan(&status))
	require.Equal(t, "active", status)
	var auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE request_id = 'api-key-reauthorization-revoked'`,
	).Scan(&auditCount))
	require.Zero(t, auditCount)
}

func TestCreateAPIKeyRequiresExistingDefaultProjectIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, actorID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('api-key-default-project-org', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'api-key-default-project-admin', 'test-hash', 'org_admin', 'active') RETURNING id`, orgID,
	).Scan(&actorID))
	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)

	missing := httptest.NewRecorder()
	h.CreateAPIKey(missing, apiKeyIntegrationRequest(http.MethodPost, "/api/v1/keys",
		`{"name":"missing-default-project"}`, actorID, orgID, auth.RoleOrgAdmin, "api-key-missing-default"))
	require.Equal(t, http.StatusInternalServerError, missing.Code, missing.Body.String())
	var projectCount, keyCount, auditCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM projects WHERE org_id = $1`, orgID).Scan(&projectCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE org_id = $1`, orgID).Scan(&keyCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE request_id = 'api-key-missing-default'`).Scan(&auditCount))
	require.Zero(t, projectCount)
	require.Zero(t, keyCount)
	require.Zero(t, auditCount)

	var defaultProjectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, description, status)
		 VALUES ($1, 'Default Project', 'default', 'Default project', 'active') RETURNING id`, orgID,
	).Scan(&defaultProjectID))
	success := httptest.NewRecorder()
	h.CreateAPIKey(success, apiKeyIntegrationRequest(http.MethodPost, "/api/v1/keys",
		`{"name":"existing-default-project"}`, actorID, orgID, auth.RoleOrgAdmin, "api-key-existing-default"))
	require.Equal(t, http.StatusCreated, success.Code, success.Body.String())
	var storedProjectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT project_id FROM api_keys WHERE org_id = $1 AND name = 'existing-default-project'`, orgID,
	).Scan(&storedProjectID))
	require.Equal(t, defaultProjectID, storedProjectID)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE request_id = 'api-key-existing-default' AND action = 'apikey.create'`,
	).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestOrganizationRoleAccountsUseLiveMembershipForPersonalCredentialsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectA, projectB, orgViewerID, billingID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('orthogonal-key-org','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Orthogonal A','orthogonal-a','active') RETURNING id`, orgID,
	).Scan(&projectA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Orthogonal B','orthogonal-b','active') RETURNING id`, orgID,
	).Scan(&projectB))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		 VALUES ($1,'orthogonal-org-viewer','hash','org_viewer','org_viewer','active') RETURNING id`, orgID,
	).Scan(&orgViewerID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		 VALUES ($1,'orthogonal-billing','hash','billing_admin','billing_admin','active') RETURNING id`, orgID,
	).Scan(&billingID))
	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id,user_id,role) VALUES
		 ($1,$2,'project_user'),($1,$3,'project_viewer')`, projectA, orgViewerID, billingID)
	require.NoError(t, err)

	h := NewHandler(pool, "orthogonal-key-secret", []byte("0123456789abcdef01234567"), nil)
	create := httptest.NewRecorder()
	h.CreateAPIKey(create, apiKeyIntegrationRequest(http.MethodPost, "/api/v1/keys",
		fmt.Sprintf(`{"name":"orthogonal-personal","project_id":%d}`, projectA),
		orgViewerID, orgID, auth.RoleOrgViewer, "orthogonal-key-create"))
	require.Equal(t, http.StatusCreated, create.Code, create.Body.String())

	var keyID, ownerUserID, storedProjectID int64
	var keyType, status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id,owner_user_id,project_id,key_type,status FROM api_keys WHERE org_id=$1 AND name='orthogonal-personal'`, orgID,
	).Scan(&keyID, &ownerUserID, &storedProjectID, &keyType, &status))
	require.Equal(t, orgViewerID, ownerUserID)
	require.Equal(t, projectA, storedProjectID)
	require.Equal(t, "personal", keyType)
	require.Equal(t, "active", status)

	update := httptest.NewRecorder()
	updateRequest := apiKeyIntegrationRequest(http.MethodPut, "/api/v1/keys/target", `{"name":"orthogonal-personal-renamed"}`,
		orgViewerID, orgID, auth.RoleOrgViewer, "orthogonal-key-update")
	updateRequest.SetPathValue("id", stringInt64(keyID))
	h.UpdateAPIKey(update, updateRequest)
	require.Equal(t, http.StatusOK, update.Code, update.Body.String())

	disable := httptest.NewRecorder()
	disableRequest := apiKeyIntegrationRequest(http.MethodPost, "/api/v1/keys/target/disable", "",
		orgViewerID, orgID, auth.RoleOrgViewer, "orthogonal-key-disable")
	disableRequest.SetPathValue("id", stringInt64(keyID))
	h.DisableAPIKey(disable, disableRequest)
	require.Equal(t, http.StatusOK, disable.Code, disable.Body.String())
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id=$1`, keyID).Scan(&status))
	require.Equal(t, "disabled", status)

	outsideMembership := httptest.NewRecorder()
	h.CreateAPIKey(outsideMembership, apiKeyIntegrationRequest(http.MethodPost, "/api/v1/keys",
		fmt.Sprintf(`{"name":"orthogonal-forbidden","project_id":%d}`, projectB),
		orgViewerID, orgID, auth.RoleOrgViewer, "orthogonal-key-forbidden"))
	require.Equal(t, http.StatusForbidden, outsideMembership.Code, outsideMembership.Body.String())

	viewerCannotCall := httptest.NewRecorder()
	h.CreateAPIKey(viewerCannotCall, apiKeyIntegrationRequest(http.MethodPost, "/api/v1/keys",
		fmt.Sprintf(`{"name":"billing-viewer-forbidden","project_id":%d}`, projectA),
		billingID, orgID, auth.RoleBillingAdmin, "orthogonal-billing-forbidden"))
	require.Equal(t, http.StatusForbidden, viewerCannotCall.Code, viewerCannotCall.Body.String())
}

type apiKeyInterleavingDB struct {
	pool         *pgxpool.Pool
	beginReached chan struct{}
	allowBegin   chan struct{}
}

func (db *apiKeyInterleavingDB) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return db.pool.Query(ctx, sql, args...)
}

func (db *apiKeyInterleavingDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return db.pool.QueryRow(ctx, sql, args...)
}

func (db *apiKeyInterleavingDB) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return db.pool.Exec(ctx, sql, args...)
}

func (db *apiKeyInterleavingDB) Begin(ctx context.Context) (pgx.Tx, error) {
	close(db.beginReached)
	select {
	case <-db.allowBegin:
		return db.pool.Begin(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func apiKeyIntegrationRequest(method, target, body string, userID, orgID int64, role, requestID string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, role)
	ctx = context.WithValue(ctx, middleware.CtxRequestID, requestID)
	return req.WithContext(ctx)
}

func stringInt64(value int64) string {
	return fmt.Sprintf("%d", value)
}
