package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestSecurityObjectLookupsAreAuthorizationScopedIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgA, orgB, projectA, hiddenProjectA, projectB int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('security-scope-a','active','standard') RETURNING id`,
	).Scan(&orgA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('security-scope-b','active','standard') RETURNING id`,
	).Scan(&orgB))
	insertProject := func(orgID int64, name, slug string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,$2,$3,'active') RETURNING id`,
			orgID, name, slug,
		).Scan(&id))
		return id
	}
	projectA = insertProject(orgA, "Authorized Project", "security-authorized")
	hiddenProjectA = insertProject(orgA, "Hidden Project", "security-hidden")
	projectB = insertProject(orgB, "Foreign Project", "security-foreign")

	insertUser := func(orgID int64, username, organizationRole, compatibilityRole string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash',$3,$4,'active') RETURNING id`,
			orgID, username, organizationRole, compatibilityRole,
		).Scan(&id))
		return id
	}
	projectAdmin := insertUser(orgA, "security-project-admin", auth.RoleMember, auth.RoleProjectAdmin)
	orgAdmin := insertUser(orgA, "security-org-admin", auth.RoleOrgAdmin, auth.RoleOrgAdmin)
	targetUser := insertUser(orgA, "security-target", auth.RoleMember, auth.RoleMember)
	foreignUser := insertUser(orgB, "security-foreign-target", auth.RoleMember, auth.RoleDeveloper)
	_, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id,user_id,role) VALUES
		($1,$2,'project_admin'),($3,$4,'project_user')`,
		projectA, projectAdmin, projectB, foreignUser,
	)
	require.NoError(t, err)

	handler := NewHandler(pool, "security-scope-secret", []byte("0123456789abcdef01234567"), nil)
	requestSequence := 0
	serveProjectActor := func(method, target, body string, userID int64, pathValues map[string]string, invoke func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		requestSequence++
		request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		request = request.WithContext(context.WithValue(request.Context(), middleware.CtxRequestID,
			fmt.Sprintf("security-object-lookup-%d", requestSequence)))
		request = withIntegrationCommittingActor(t, pool, request, userID)
		for key, value := range pathValues {
			request.SetPathValue(key, value)
		}
		response := httptest.NewRecorder()
		invoke(response, request)
		return response
	}
	assertEquivalent := func(t *testing.T, absent, inaccessible *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusNotFound, absent.Code, absent.Body.String())
		require.Equal(t, absent.Code, inaccessible.Code, inaccessible.Body.String())
		require.JSONEq(t, absent.Body.String(), inaccessible.Body.String())
	}

	t.Run("membership project and member-pair existence", func(t *testing.T) {
		absentProject := serveProjectActor(http.MethodPut, "/members", `{"role":"project_user"}`, projectAdmin,
			map[string]string{"id": "9223372036854775000", "user_id": fmt.Sprint(targetUser)}, handler.UpsertProjectMember)
		hiddenProject := serveProjectActor(http.MethodPut, "/members", `{"role":"project_user"}`, projectAdmin,
			map[string]string{"id": fmt.Sprint(hiddenProjectA), "user_id": fmt.Sprint(targetUser)}, handler.UpsertProjectMember)
		assertEquivalent(t, absentProject, hiddenProject)

		missingUserID := targetUser + 1_000_000
		absentPair := serveProjectActor(http.MethodDelete, "/members", "", projectAdmin,
			map[string]string{"id": "9223372036854775000", "user_id": fmt.Sprint(missingUserID)}, handler.DeleteProjectMember)
		foreignPair := serveProjectActor(http.MethodDelete, "/members", "", projectAdmin,
			map[string]string{"id": fmt.Sprint(projectB), "user_id": fmt.Sprint(foreignUser)}, handler.DeleteProjectMember)
		assertEquivalent(t, absentPair, foreignPair)
	})

	t.Run("budget project existence", func(t *testing.T) {
		absent := serveProjectActor(http.MethodPut, "/budget", `{"budget_cents":1}`, projectAdmin,
			map[string]string{"id": "9223372036854775000"}, handler.UpdateProjectBudget)
		inaccessible := serveProjectActor(http.MethodPut, "/budget", `{"budget_cents":1}`, projectAdmin,
			map[string]string{"id": fmt.Sprint(hiddenProjectA)}, handler.UpdateProjectBudget)
		assertEquivalent(t, absent, inaccessible)
	})

	_, err = pool.Exec(ctx, `
		INSERT INTO api_keys
		(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit)
		VALUES ($1,$2,'shared','scoped-duplicate-name','hash','scope-hidden','active',60,1)
		`, orgA, hiddenProjectA)
	require.NoError(t, err)
	t.Run("API key name is checked only inside an authorized project scope", func(t *testing.T) {
		usedName := serveProjectActor(http.MethodPost, "/keys",
			fmt.Sprintf(`{"project_id":%d,"name":"scoped-duplicate-name"}`, hiddenProjectA), projectAdmin,
			nil, handler.CreateAPIKey)
		unusedName := serveProjectActor(http.MethodPost, "/keys",
			fmt.Sprintf(`{"project_id":%d,"name":"scoped-unused-name"}`, hiddenProjectA), projectAdmin,
			nil, handler.CreateAPIKey)
		require.Equal(t, http.StatusForbidden, usedName.Code, usedName.Body.String())
		require.Equal(t, usedName.Code, unusedName.Code, unusedName.Body.String())
		require.JSONEq(t, usedName.Body.String(), unusedName.Body.String())

		local := serveProjectActor(http.MethodPost, "/keys",
			fmt.Sprintf(`{"project_id":%d,"name":"scoped-duplicate-name"}`, projectA), projectAdmin,
			nil, handler.CreateAPIKey)
		require.Equal(t, http.StatusCreated, local.Code, local.Body.String())
		duplicateLocal := serveProjectActor(http.MethodPost, "/keys",
			fmt.Sprintf(`{"project_id":%d,"name":"scoped-duplicate-name"}`, projectA), projectAdmin,
			nil, handler.CreateAPIKey)
		require.Equal(t, http.StatusConflict, duplicateLocal.Code, duplicateLocal.Body.String())
	})

	var hiddenWebhookID, visibleWebhookID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO notification_webhooks
		(org_id,project_id,name,url,event_types,enabled,scope_verified,scope_write_generation)
		VALUES ($1,$2,'hidden','https://example.invalid/hidden','[]',false,true,1) RETURNING id`,
		orgA, hiddenProjectA,
	).Scan(&hiddenWebhookID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO notification_webhooks
		(org_id,project_id,name,url,event_types,enabled,scope_verified,scope_write_generation)
		VALUES ($1,$2,'visible','https://example.invalid/visible','[]',false,true,1) RETURNING id`,
		orgA, projectA,
	).Scan(&visibleWebhookID))
	t.Run("notification webhook existence", func(t *testing.T) {
		absent := serveProjectActor(http.MethodPut, "/webhooks", `{}`, projectAdmin,
			map[string]string{"id": "9223372036854775000"}, handler.UpdateNotificationWebhook)
		inaccessible := serveProjectActor(http.MethodPut, "/webhooks", `{}`, projectAdmin,
			map[string]string{"id": fmt.Sprint(hiddenWebhookID)}, handler.UpdateNotificationWebhook)
		assertEquivalent(t, absent, inaccessible)

		authorized := serveProjectActor(http.MethodPut, "/webhooks", `{"name":"visible-updated"}`, projectAdmin,
			map[string]string{"id": fmt.Sprint(visibleWebhookID)}, handler.UpdateNotificationWebhook)
		require.Equal(t, http.StatusOK, authorized.Code, authorized.Body.String())
	})

	var applicationID, foreignKeyID, localKeyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO workload_applications
		(org_id,project_id,application_ref,display_name,kind,environment,identity_mode,client_profile,created_by,updated_by)
		VALUES ($1,$2,'app_security_scope','Security Scope','agent','test','service_credential','openai',$3,$3)
		RETURNING id`, orgA, projectA, orgAdmin,
	).Scan(&applicationID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys
		(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit,purpose)
		VALUES ($1,$2,'service','foreign-key','hash','foreign-prefix','active',1,1,'') RETURNING id`,
		orgB, projectB,
	).Scan(&foreignKeyID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys
		(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit,purpose)
		VALUES ($1,$2,'service','local-key','hash','local-prefix','active',1,1,'') RETURNING id`,
		orgA, projectA,
	).Scan(&localKeyID))
	t.Run("workload credential existence", func(t *testing.T) {
		path := map[string]string{"ref": "app_security_scope", "key_id": "9223372036854775000"}
		absent := serveProjectActor(http.MethodPost, "/workloads/bind", `{"expected_revision":1}`, orgAdmin,
			path, handler.BindWorkloadApplicationCredential)
		path = map[string]string{"ref": "app_security_scope", "key_id": fmt.Sprint(foreignKeyID)}
		inaccessible := serveProjectActor(http.MethodPost, "/workloads/bind", `{"expected_revision":1}`, orgAdmin,
			path, handler.BindWorkloadApplicationCredential)
		assertEquivalent(t, absent, inaccessible)

		path = map[string]string{"ref": "app_security_scope", "key_id": fmt.Sprint(localKeyID)}
		authorized := serveProjectActor(http.MethodPost, "/workloads/bind", `{"expected_revision":1}`, orgAdmin,
			path, handler.BindWorkloadApplicationCredential)
		require.Equal(t, http.StatusOK, authorized.Code, authorized.Body.String())
		var boundApplicationID int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT workload_application_id FROM api_keys WHERE id=$1`, localKeyID).Scan(&boundApplicationID))
		require.Equal(t, applicationID, boundApplicationID)
	})
}

func TestUsageMyScopeRequiresLiveMembershipIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, memberID, orgViewerID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ('usage-live-membership','active','standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Usage Project','usage-live-membership','active') RETURNING id`, orgID,
	).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'usage-member','hash','member','developer','active') RETURNING id`, orgID,
	).Scan(&memberID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'usage-org-viewer','hash','org_viewer','org_viewer','active') RETURNING id`, orgID,
	).Scan(&orgViewerID))
	_, err := pool.Exec(ctx, `
		INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, memberID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO call_logs
		(request_id,org_id,project_id,owner_user_id,model,provider,channel,prompt_tokens,completion_tokens,total_tokens,cost_micros,status_code,usage_source)
		VALUES
		('usage-member-row',$1,$2,$3,'model','provider','channel',1,1,2,10,200,'upstream'),
		('usage-org-viewer-row',$1,$2,$4,'model','provider','channel',1,1,2,10,200,'upstream')`,
		orgID, projectID, memberID, orgViewerID,
	)
	require.NoError(t, err)

	handler := NewHandler(pool, "usage-live-secret", []byte("0123456789abcdef01234567"), nil)
	readTotal := func(userID int64, organizationRole, compatibilityRole string) int64 {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/usage/analytics?scope=my", nil)
		request = withTestActor(request, userID, orgID, compatibilityRole)
		request = request.WithContext(context.WithValue(request.Context(), middleware.CtxOrganizationRole, organizationRole))
		response := httptest.NewRecorder()
		handler.UsageAnalytics(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var payload struct {
			Summary struct {
				TotalRequests int64 `json:"total_requests"`
			} `json:"summary"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
		return payload.Summary.TotalRequests
	}
	require.Equal(t, int64(1), readTotal(memberID, auth.RoleMember, auth.RoleDeveloper))
	_, err = pool.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, memberID)
	require.NoError(t, err)
	require.Zero(t, readTotal(memberID, auth.RoleMember, auth.RoleDeveloper), "final membership removal must immediately hide former-project usage")
	require.Equal(t, int64(1), readTotal(orgViewerID, auth.RoleOrgViewer, auth.RoleOrgViewer), "explicit organization-wide reader retains its own usage view")
}
