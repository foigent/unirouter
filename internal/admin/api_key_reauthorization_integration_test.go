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
	testAPIKeyMutationReauthorizesAfterProjectMembershipRevocationIntegration(
		t,
		http.MethodPut,
		`{"status":"disabled"}`,
		"update",
		func(handler *Handler, response http.ResponseWriter, request *http.Request) {
			handler.UpdateAPIKey(response, request)
		},
	)
}

func TestDisableAPIKeyReauthorizesAfterProjectMembershipRevocationIntegration(t *testing.T) {
	testAPIKeyMutationReauthorizesAfterProjectMembershipRevocationIntegration(
		t,
		http.MethodPost,
		"",
		"disable",
		func(handler *Handler, response http.ResponseWriter, request *http.Request) {
			handler.DisableAPIKey(response, request)
		},
	)
}

func TestAPIKeyMutationReauthorizesAfterCommittedPrincipalChangeIntegration(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		mutate func(*testing.T, *pgxpool.Pool, *Handler, string, int64)
	}{
		{
			name: "exact admitted session logout",
			code: "sid",
			mutate: func(t *testing.T, _ *pgxpool.Pool, liveHandler *Handler, token string, _ int64) {
				logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
				logoutRequest.Header.Set("Authorization", "Bearer "+token)
				logoutResponse := httptest.NewRecorder()
				liveHandler.Logout(logoutResponse, logoutRequest)
				require.Equal(t, http.StatusOK, logoutResponse.Code, logoutResponse.Body.String())
			},
		},
		{
			name: "authorization epoch advanced",
			code: "epoch",
			mutate: func(t *testing.T, pool *pgxpool.Pool, _ *Handler, _ string, actorID int64) {
				_, err := pool.Exec(t.Context(), `UPDATE users SET auth_epoch=auth_epoch+1 WHERE id=$1`, actorID)
				require.NoError(t, err)
			},
		},
		{
			name: "actor disabled",
			code: "disabled",
			mutate: func(t *testing.T, pool *pgxpool.Pool, _ *Handler, _ string, actorID int64) {
				_, err := pool.Exec(t.Context(), `UPDATE users SET status='disabled' WHERE id=$1`, actorID)
				require.NoError(t, err)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := context.Background()
			var orgID, projectID, actorID, keyID int64
			suffix := test.code
			require.NoError(t, pool.QueryRow(ctx,
				`INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','standard') RETURNING id`,
				"key-principal-change-"+suffix,
			).Scan(&orgID))
			require.NoError(t, pool.QueryRow(ctx,
				`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,$2,$3,'active') RETURNING id`,
				orgID, "Principal change "+test.name, "key-principal-change-"+suffix,
			).Scan(&projectID))
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
				VALUES ($1,$2,'hash','member','project_admin','active') RETURNING id`,
				orgID, "key-principal-change-"+suffix,
			).Scan(&actorID))
			_, err := pool.Exec(ctx,
				`INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_admin')`,
				projectID, actorID,
			)
			require.NoError(t, err)
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO api_keys
				(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit)
				VALUES ($1,$2,'shared',$3,'hash',$4,'active',60,10) RETURNING id`,
				orgID, projectID, "principal-change-"+suffix, "pc-"+suffix,
			).Scan(&keyID))

			const jwtSecret = "api-key-principal-change-secret"
			token := testutil.MustAdminSessionJWT(t, ctx, pool, actorID, orgID, auth.RoleProjectAdmin, jwtSecret)
			beginReached := make(chan struct{})
			allowBegin := make(chan struct{})
			interleavingDB := &apiKeyInterleavingDB{pool: pool, beginReached: beginReached, allowBegin: allowBegin}
			mutationHandler := NewHandler(interleavingDB, jwtSecret, []byte("0123456789abcdef01234567"), nil)
			liveHandler := NewHandler(pool, jwtSecret, []byte("0123456789abcdef01234567"), nil)
			requestID := "key-principal-change-" + suffix
			request := httptest.NewRequest(http.MethodPut, "/api/v1/keys/target", strings.NewReader(`{"status":"disabled"}`))
			request.Header.Set("Authorization", "Bearer "+token)
			request.SetPathValue("id", stringInt64(keyID))
			request = request.WithContext(context.WithValue(request.Context(), middleware.CtxRequestID, requestID))
			protected := middleware.AdminAuth(pool, jwtSecret)(http.HandlerFunc(mutationHandler.UpdateAPIKey))
			responseCh := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				response := httptest.NewRecorder()
				protected.ServeHTTP(response, request)
				responseCh <- response
			}()

			select {
			case <-beginReached:
			case <-time.After(5 * time.Second):
				t.Fatal("API-key update did not reach its committing transaction after authenticated admission")
			}
			test.mutate(t, pool, liveHandler, token, actorID)
			close(allowBegin)

			select {
			case response := <-responseCh:
				require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
			case <-time.After(5 * time.Second):
				t.Fatal("API-key update did not finish after the principal change committed")
			}
			var keyStatus string
			require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id=$1`, keyID).Scan(&keyStatus))
			require.Equal(t, "active", keyStatus)
			var auditCount int
			require.NoError(t, pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM audit_logs WHERE request_id=$1`, requestID,
			).Scan(&auditCount))
			require.Zero(t, auditCount)
		})
	}
}

func testAPIKeyMutationReauthorizesAfterProjectMembershipRevocationIntegration(
	t *testing.T,
	method, body, operation string,
	invoke func(*Handler, http.ResponseWriter, *http.Request),
) {
	t.Helper()
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
	req := apiKeyIntegrationRequest(t, pool, method, "/api/v1/keys/target", body,
		actorID, orgID, auth.RoleProjectAdmin, "api-key-reauthorization-"+operation+"-revoked")
	req.SetPathValue("id", stringInt64(keyID))
	recorderCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		invoke(h, recorder, req)
		recorderCh <- recorder
	}()

	select {
	case <-beginReached:
	case <-time.After(5 * time.Second):
		t.Fatalf("API-key %s did not reach its committing transaction after preauthorization", operation)
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
		t.Fatalf("API-key %s did not finish after revocation committed", operation)
	}
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, keyID).Scan(&status))
	require.Equal(t, "active", status)
	var auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE request_id = $1`, "api-key-reauthorization-"+operation+"-revoked",
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
	h.CreateAPIKey(missing, apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/keys",
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
	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_admin')`, defaultProjectID, actorID,
	)
	require.NoError(t, err)
	success := httptest.NewRecorder()
	h.CreateAPIKey(success, apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/keys",
		`{"name":"existing-default-project"}`, actorID, orgID, auth.RoleOrgAdmin, "api-key-existing-default"))
	require.Equal(t, http.StatusCreated, success.Code, success.Body.String())
	var storedProjectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT project_id FROM api_keys WHERE org_id = $1 AND name = 'existing-default-project'`, orgID,
	).Scan(&storedProjectID))
	require.Equal(t, defaultProjectID, storedProjectID)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_logs
		WHERE request_id = 'api-key-existing-default'
		  AND action IN ('access_request.provision','access_grant.credential.issue')`,
	).Scan(&auditCount))
	require.Equal(t, 2, auditCount)
}

func TestOrganizationRoleAccountsUseLiveMembershipForPersonalCredentialsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectA, projectB, orgViewerID, billingCallerID, billingViewerID int64
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
		 VALUES ($1,'orthogonal-billing-caller','hash','billing_admin','billing_admin','active') RETURNING id`, orgID,
	).Scan(&billingCallerID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		 VALUES ($1,'orthogonal-billing-viewer','hash','billing_admin','billing_admin','active') RETURNING id`, orgID,
	).Scan(&billingViewerID))
	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id,user_id,role) VALUES
		 ($1,$2,'project_user'),($1,$3,'project_user'),($1,$4,'project_viewer')`,
		projectA, orgViewerID, billingCallerID, billingViewerID)
	require.NoError(t, err)

	h := NewHandler(pool, "orthogonal-key-secret", []byte("0123456789abcdef01234567"), nil)
	orgViewerCannotCall := httptest.NewRecorder()
	h.CreateAPIKey(orgViewerCannotCall, apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/keys",
		fmt.Sprintf(`{"name":"org-viewer-forbidden","project_id":%d}`, projectA),
		orgViewerID, orgID, auth.RoleOrgViewer, "orthogonal-org-viewer-forbidden"))
	require.Equal(t, http.StatusForbidden, orgViewerCannotCall.Code, orgViewerCannotCall.Body.String())

	create := httptest.NewRecorder()
	h.CreateAPIKey(create, apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/keys",
		fmt.Sprintf(`{"name":"orthogonal-personal","project_id":%d}`, projectA),
		billingCallerID, orgID, auth.RoleBillingAdmin, "orthogonal-key-create"))
	require.Equal(t, http.StatusCreated, create.Code, create.Body.String())

	var keyID, ownerUserID, storedProjectID int64
	var keyType, status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id,owner_user_id,project_id,key_type,status FROM api_keys WHERE org_id=$1 AND name='orthogonal-personal'`, orgID,
	).Scan(&keyID, &ownerUserID, &storedProjectID, &keyType, &status))
	require.Equal(t, billingCallerID, ownerUserID)
	require.Equal(t, projectA, storedProjectID)
	require.Equal(t, "personal", keyType)
	require.Equal(t, "active", status)

	get := httptest.NewRecorder()
	getRequest := apiKeyIntegrationRequest(t, pool, http.MethodGet, "/api/v1/keys/target", "",
		billingCallerID, orgID, auth.RoleBillingAdmin, "orthogonal-key-get")
	getRequest.SetPathValue("id", stringInt64(keyID))
	h.GetAPIKey(get, getRequest)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())

	update := httptest.NewRecorder()
	updateRequest := apiKeyIntegrationRequest(t, pool, http.MethodPut, "/api/v1/keys/target", `{"name":"orthogonal-personal-renamed"}`,
		billingCallerID, orgID, auth.RoleBillingAdmin, "orthogonal-key-update")
	updateRequest.SetPathValue("id", stringInt64(keyID))
	h.UpdateAPIKey(update, updateRequest)
	require.Equal(t, http.StatusConflict, update.Code, update.Body.String())

	disable := httptest.NewRecorder()
	disableRequest := apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/keys/target/disable", "",
		billingCallerID, orgID, auth.RoleBillingAdmin, "orthogonal-key-disable")
	disableRequest.SetPathValue("id", stringInt64(keyID))
	h.DisableAPIKey(disable, disableRequest)
	require.Equal(t, http.StatusOK, disable.Code, disable.Body.String())
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id=$1`, keyID).Scan(&status))
	require.Equal(t, "disabled", status)

	outsideMembership := httptest.NewRecorder()
	h.CreateAPIKey(outsideMembership, apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/keys",
		fmt.Sprintf(`{"name":"orthogonal-forbidden","project_id":%d}`, projectB),
		billingCallerID, orgID, auth.RoleBillingAdmin, "orthogonal-key-forbidden"))
	require.Equal(t, http.StatusForbidden, outsideMembership.Code, outsideMembership.Body.String())

	viewerCannotCall := httptest.NewRecorder()
	h.CreateAPIKey(viewerCannotCall, apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/keys",
		fmt.Sprintf(`{"name":"billing-viewer-forbidden","project_id":%d}`, projectA),
		billingViewerID, orgID, auth.RoleBillingAdmin, "orthogonal-billing-forbidden"))
	require.Equal(t, http.StatusForbidden, viewerCannotCall.Code, viewerCannotCall.Body.String())
}

func TestAPIKeyObjectLookupResponseEquivalenceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var actorOrgID, foreignOrgID, foreignProjectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('key-oracle-actor-org','active','standard') RETURNING id`,
	).Scan(&actorOrgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('key-oracle-foreign-org','active','standard') RETURNING id`,
	).Scan(&foreignOrgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Foreign key project','key-oracle-foreign','active') RETURNING id`, foreignOrgID,
	).Scan(&foreignProjectID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		 VALUES ($1,'key-oracle-actor','hash','org_viewer','org_viewer','active') RETURNING id`, actorOrgID,
	).Scan(&actorID))

	nextPrefix := 0
	insertKey := func(name, purpose, status string) int64 {
		t.Helper()
		nextPrefix++
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO api_keys
			 (org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit,allowed_ips,allowed_models,purpose)
			 VALUES ($1,$2,'service',$3,'hash',$4,$5,60,1,'127.0.0.1/32','',$6) RETURNING id`,
			foreignOrgID, foreignProjectID, name, fmt.Sprintf("ko-%d", nextPrefix), status, purpose,
		).Scan(&id))
		return id
	}
	ordinaryID := insertKey("key-oracle-ordinary", "", "active")
	feedID := insertKey("key-oracle-feed", middleware.APIKeyPurposeXInsightFeedRead, middleware.APIKeyStatusFeedActive)
	absentID := feedID + 100000

	h := NewHandler(pool, "key-oracle-secret", []byte("0123456789abcdef01234567"), nil)
	type operation struct {
		name   string
		method string
		body   string
		invoke func(http.ResponseWriter, *http.Request)
	}
	operations := []operation{
		{name: "get", method: http.MethodGet, invoke: func(w http.ResponseWriter, r *http.Request) { h.GetAPIKey(w, r) }},
		{name: "update", method: http.MethodPut, body: `{"name":"must-not-change"}`, invoke: func(w http.ResponseWriter, r *http.Request) { h.UpdateAPIKey(w, r) }},
		{name: "disable", method: http.MethodPost, invoke: func(w http.ResponseWriter, r *http.Request) { h.DisableAPIKey(w, r) }},
	}
	serve := func(operation operation, id int64, requestID string) *httptest.ResponseRecorder {
		request := apiKeyIntegrationRequest(t, pool, operation.method, "/api/v1/keys/target", operation.body,
			actorID, actorOrgID, auth.RoleOrgViewer, requestID)
		request.SetPathValue("id", stringInt64(id))
		response := httptest.NewRecorder()
		operation.invoke(response, request)
		return response
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			absent := serve(operation, absentID, "key-oracle-"+operation.name+"-absent")
			inaccessible := serve(operation, ordinaryID, "key-oracle-"+operation.name+"-foreign")
			inaccessibleFeed := serve(operation, feedID, "key-oracle-"+operation.name+"-feed")
			for label, response := range map[string]*httptest.ResponseRecorder{
				"absent": absent, "inaccessible": inaccessible, "inaccessible_feed": inaccessibleFeed,
			} {
				require.Equal(t, http.StatusNotFound, response.Code, "%s: %s", label, response.Body.String())
				require.JSONEq(t, `{"error":"API key not found"}`, response.Body.String(), label)
			}
			require.Equal(t, absent.Body.String(), inaccessible.Body.String())
			require.Equal(t, absent.Body.String(), inaccessibleFeed.Body.String())
		})
	}

	var ordinaryName, ordinaryStatus, feedStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT name,status FROM api_keys WHERE id=$1`, ordinaryID).Scan(&ordinaryName, &ordinaryStatus))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id=$1`, feedID).Scan(&feedStatus))
	require.Equal(t, "key-oracle-ordinary", ordinaryName)
	require.Equal(t, "active", ordinaryStatus)
	require.Equal(t, middleware.APIKeyStatusFeedActive, feedStatus)
}

func TestAPIKeyActorScopedLookupPreservesOrganizationAdminIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, adminID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('key-scope-admin-org','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Admin key project','key-scope-admin','active') RETURNING id`, orgID,
	).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		 VALUES ($1,'key-scope-org-admin','hash','org_admin','org_admin','active') RETURNING id`, orgID,
	).Scan(&adminID))
	nextPrefix := 0
	insertKey := func(name string) int64 {
		t.Helper()
		nextPrefix++
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO api_keys (org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit)
			 VALUES ($1,$2,'shared',$3,'hash',$4,'active',60,1) RETURNING id`,
			orgID, projectID, name, fmt.Sprintf("ksa-%d", nextPrefix),
		).Scan(&id))
		return id
	}
	h := NewHandler(pool, "key-scope-admin-secret", []byte("0123456789abcdef01234567"), nil)

	getID := insertKey("key-scope-admin-get")
	getRequest := apiKeyIntegrationRequest(t, pool, http.MethodGet, "/api/v1/keys/target", "", adminID, orgID, auth.RoleOrgAdmin, "key-scope-admin-get")
	getRequest.SetPathValue("id", stringInt64(getID))
	getResponse := httptest.NewRecorder()
	h.GetAPIKey(getResponse, getRequest)
	require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())

	updateID := insertKey("key-scope-admin-update")
	updateRequest := apiKeyIntegrationRequest(t, pool, http.MethodPut, "/api/v1/keys/target", `{"name":"key-scope-admin-updated"}`,
		adminID, orgID, auth.RoleOrgAdmin, "key-scope-admin-update")
	updateRequest.SetPathValue("id", stringInt64(updateID))
	updateResponse := httptest.NewRecorder()
	h.UpdateAPIKey(updateResponse, updateRequest)
	require.Equal(t, http.StatusOK, updateResponse.Code, updateResponse.Body.String())

	disableID := insertKey("key-scope-admin-disable")
	disableRequest := apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/keys/target/disable", "",
		adminID, orgID, auth.RoleOrgAdmin, "key-scope-admin-disable")
	disableRequest.SetPathValue("id", stringInt64(disableID))
	disableResponse := httptest.NewRecorder()
	h.DisableAPIKey(disableResponse, disableRequest)
	require.Equal(t, http.StatusOK, disableResponse.Code, disableResponse.Body.String())
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

func apiKeyIntegrationRequest(t *testing.T, pool *pgxpool.Pool, method, target, body string, userID, orgID int64, role, requestID string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
	ctx = context.WithValue(ctx, middleware.CtxOrgID, orgID)
	ctx = context.WithValue(ctx, middleware.CtxUserRole, role)
	if organizationRole, ok := auth.NormalizeOrganizationRole(role); ok {
		ctx = context.WithValue(ctx, middleware.CtxOrganizationRole, organizationRole)
	}
	ctx = context.WithValue(ctx, middleware.CtxRequestID, requestID)
	return withIntegrationCommittingActor(t, pool, req.WithContext(ctx), userID)
}

func stringInt64(value int64) string {
	return fmt.Sprintf("%d", value)
}
