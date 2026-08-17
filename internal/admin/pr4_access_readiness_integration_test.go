//go:build integration

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/accessprovisioning"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelreadiness"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type pr4RoutingPreview struct {
	mu       sync.Mutex
	previews map[string]routingpolicy.Preview
	calls    int
}

func (p *pr4RoutingPreview) Preview(_ context.Context, model string) (routingpolicy.Preview, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	preview := p.previews[model]
	preview.Model = model
	return preview, nil
}

func (*pr4RoutingPreview) Invalidate() {}

func (p *pr4RoutingPreview) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func pr4InvokePool(
	t *testing.T,
	pool *pgxpool.Pool,
	actorID int64,
	method, target, body, clientRequestID string,
	pathValues map[string]string,
	handler http.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if clientRequestID != "" {
		request.Header.Set(middleware.ClientRequestIDHeader, clientRequestID)
	}
	for name, value := range pathValues {
		request.SetPathValue(name, value)
	}
	request = withIntegrationCommittingActor(t, pool, request, actorID)
	response := httptest.NewRecorder()
	middleware.OperationIdentity(handler).ServeHTTP(response, request)
	return response
}

func pr4InvokeRead(
	t *testing.T,
	pool *pgxpool.Pool,
	actorID int64,
	target string,
	handler http.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	var orgID, authEpoch int64
	var organizationRole, compatibilityRole string
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT org_id,organization_role,role,auth_epoch FROM users WHERE id=$1`, actorID).
		Scan(&orgID, &organizationRole, &compatibilityRole, &authEpoch))
	request := httptest.NewRequest(http.MethodGet, target, nil)
	requestCtx := context.WithValue(request.Context(), middleware.CtxUserID, actorID)
	requestCtx = context.WithValue(requestCtx, middleware.CtxOrgID, orgID)
	requestCtx = context.WithValue(requestCtx, middleware.CtxUserRole, compatibilityRole)
	requestCtx = context.WithValue(requestCtx, middleware.CtxOrganizationRole, organizationRole)
	requestCtx = context.WithValue(requestCtx, middleware.CtxAuthEpoch, authEpoch)
	response := httptest.NewRecorder()
	handler(response, request.WithContext(requestCtx))
	return response
}

func TestPR4AccessListsActionsAndIdentityPartitionsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, foreignOrgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('pr4-access','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('pr4-access-foreign','active','customer') RETURNING id`).Scan(&foreignOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'PR4 Access','pr4-access','active') RETURNING id`, orgID).Scan(&projectID))

	seedUser := func(username, organizationRole, compatibilityRole, provider string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status,auth_provider)
			VALUES ($1,$2,'hash',$3,$4,'active',$5) RETURNING id`,
			orgID, username, organizationRole, compatibilityRole, provider).Scan(&id))
		return id
	}
	orgAdminID := seedUser("pr4-org-admin", auth.RoleOrgAdmin, auth.RoleOrgAdmin, "local")
	projectAdminID := seedUser("pr4-project-admin", auth.RoleMember, auth.RoleProjectAdmin, "local")
	viewerID := seedUser("pr4-project-viewer", auth.RoleMember, auth.RoleViewer, "local")
	noMembershipID := seedUser("pr4-no-membership", auth.RoleMember, auth.RoleDeveloper, "local")
	oidcID := seedUser("pr4-oidc", auth.RoleMember, auth.RoleDeveloper, "oidc")
	_ = oidcID
	for _, membership := range []struct {
		userID int64
		role   string
	}{{projectAdminID, auth.ProjectRoleAdmin}, {viewerID, auth.ProjectRoleViewer}} {
		_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,$3)`,
			projectID, membership.userID, membership.role)
		require.NoError(t, err)
	}
	var foreignID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status,auth_provider)
		VALUES ($1,'pr4-foreign','hash','org_admin','org_admin','active','oidc') RETURNING id`, foreignOrgID).Scan(&foreignID))
	_ = foreignID

	handler := NewHandlerWithAPIKeyHashSecret(pool, "pr4-access-jwt", "pr4-access-key-hash", []byte("0123456789abcdef01234567"), nil)
	emptyMy := pr4InvokePool(t, pool, oidcID, http.MethodGet,
		"/api/v1/access-requests?scope=my&limit=20", "", "", nil, handler.ListAccessRequests)
	require.Equal(t, http.StatusOK, emptyMy.Code, emptyMy.Body.String())
	require.JSONEq(t, `{"data":[],"pagination":{"limit":20,"has_more":false},"scope":"my"}`, emptyMy.Body.String())
	emptyProjectGrants := pr4InvokePool(t, pool, orgAdminID, http.MethodGet,
		fmt.Sprintf("/api/v1/access-grants?scope=project&project_id=%d&workload_application_id=9223372036854775806&limit=20", projectID),
		"", "", nil, handler.ListAccessGrants)
	require.Equal(t, http.StatusOK, emptyProjectGrants.Code, emptyProjectGrants.Body.String())
	require.JSONEq(t, `{"data":[],"pagination":{"limit":20,"has_more":false},"scope":"project"}`, emptyProjectGrants.Body.String())
	createSelf := func(userID int64, requestedRole, clientID string) accessprovisioning.ProvisionResult {
		t.Helper()
		body := fmt.Sprintf(`{
			"org_id":%d,"project_id":%d,"subject":{"type":"user","user_id":%d},
			"request_kind":"human_personal",
			"requested_scope":{"project_role":%q,"scope_mode":"project_default","models":[],"endpoints":[]},
			"environment":"personal","production_use":false,"submission_mode":"self"
		}`, orgID, projectID, userID, requestedRole)
		response := pr4InvokePool(t, pool, userID, http.MethodPost, "/api/v1/access-requests", body, clientID, nil, handler.CreateAccessRequest)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
		var result accessprovisioning.ProvisionResult
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		// Action projection is deliberately a read-model concern. Mutation
		// responses must not manufacture an authority result outside that path.
		require.Nil(t, result.Request.Actions)
		return result
	}

	users := make([]int64, 0, 3)
	for index := 0; index < 3; index++ {
		userID := seedUser(fmt.Sprintf("pr4-caller-%d", index), auth.RoleMember, auth.RoleDeveloper, "local")
		users = append(users, userID)
		_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, userID)
		require.NoError(t, err)
		approved := createSelf(userID, auth.ProjectRoleUser, fmt.Sprintf("pr4-auto-%d", index))
		require.Equal(t, accessprovisioning.RequestApproved, approved.Request.Status)
		require.NotNil(t, approved.Grant)
		elevation := createSelf(userID, auth.ProjectRoleAdmin, fmt.Sprintf("pr4-elevation-%d", index))
		require.Equal(t, accessprovisioning.RequestNeedsReview, elevation.Request.Status)
	}
	firstCross := createSelf(noMembershipID, auth.ProjectRoleUser, "pr4-first-cross")
	require.Equal(t, accessprovisioning.RequestNeedsReview, firstCross.Request.Status)
	require.Nil(t, firstCross.Grant)

	myResponse := pr4InvokePool(t, pool, noMembershipID, http.MethodGet,
		"/api/v1/access-requests?scope=my&limit=20", "", "", nil, handler.ListAccessRequests)
	require.Equal(t, http.StatusOK, myResponse.Code, myResponse.Body.String())
	var myPage accessprovisioning.RequestPage
	require.NoError(t, json.Unmarshal(myResponse.Body.Bytes(), &myPage))
	require.Equal(t, "my", myPage.Scope)
	require.Len(t, myPage.Data, 1)
	require.Equal(t, firstCross.Request.RequestRef, myPage.Data[0].RequestRef)
	require.NotNil(t, myPage.Data[0].Actions)
	require.True(t, myPage.Data[0].Actions.CanCancel)
	require.False(t, myPage.Data[0].Actions.CanApprove)

	queueResponse := pr4InvokePool(t, pool, orgAdminID, http.MethodGet,
		fmt.Sprintf("/api/v1/access-requests?scope=project&project_id=%d&status=needs_review&limit=2", projectID),
		"", "", nil, handler.ListAccessRequests)
	require.Equal(t, http.StatusOK, queueResponse.Code, queueResponse.Body.String())
	var queuePage accessprovisioning.RequestPage
	require.NoError(t, json.Unmarshal(queueResponse.Body.Bytes(), &queuePage))
	require.Len(t, queuePage.Data, 2)
	require.True(t, queuePage.Pagination.HasMore)
	require.NotNil(t, queuePage.Pagination.NextAfterID)
	seen := map[int64]bool{}
	for _, item := range queuePage.Data {
		require.Equal(t, accessprovisioning.RequestNeedsReview, item.Status)
		require.NotNil(t, item.Actions)
		require.True(t, item.Actions.CanApprove)
		require.True(t, item.Actions.CanReject)
		seen[item.ID] = true
	}
	nextResponse := pr4InvokePool(t, pool, orgAdminID, http.MethodGet,
		fmt.Sprintf("/api/v1/access-requests?scope=project&project_id=%d&status=needs_review&limit=2&after_id=%d",
			projectID, *queuePage.Pagination.NextAfterID), "", "", nil, handler.ListAccessRequests)
	require.Equal(t, http.StatusOK, nextResponse.Code, nextResponse.Body.String())
	var nextPage accessprovisioning.RequestPage
	require.NoError(t, json.Unmarshal(nextResponse.Body.Bytes(), &nextPage))
	require.NotEmpty(t, nextPage.Data)
	for _, item := range nextPage.Data {
		require.False(t, seen[item.ID], "keyset page repeated request %d", item.ID)
		require.Equal(t, accessprovisioning.RequestNeedsReview, item.Status)
	}

	detailResponse := pr4InvokePool(t, pool, projectAdminID, http.MethodGet,
		"/api/v1/access-requests/"+firstCross.Request.RequestRef, "", "",
		map[string]string{"ref": firstCross.Request.RequestRef}, handler.GetAccessRequest)
	require.Equal(t, http.StatusOK, detailResponse.Code, detailResponse.Body.String())
	var detail struct {
		Request accessprovisioning.Request `json:"request"`
	}
	require.NoError(t, json.Unmarshal(detailResponse.Body.Bytes(), &detail))
	require.NotNil(t, detail.Request.Actions)
	require.True(t, detail.Request.Actions.CanApprove)
	require.True(t, detail.Request.Actions.CanReject)

	viewerResponse := pr4InvokePool(t, pool, viewerID, http.MethodGet,
		fmt.Sprintf("/api/v1/access-requests?scope=project&project_id=%d&status=needs_review&limit=20", projectID),
		"", "", nil, handler.ListAccessRequests)
	require.Equal(t, http.StatusOK, viewerResponse.Code, viewerResponse.Body.String())
	var viewerPage accessprovisioning.RequestPage
	require.NoError(t, json.Unmarshal(viewerResponse.Body.Bytes(), &viewerPage))
	require.Empty(t, viewerPage.Data, "project viewer must not gain project-wide access inventory")

	grantResponse := pr4InvokePool(t, pool, users[0], http.MethodGet,
		"/api/v1/access-grants?scope=my&limit=20", "", "", nil, handler.ListAccessGrants)
	require.Equal(t, http.StatusOK, grantResponse.Code, grantResponse.Body.String())
	var grantPage accessprovisioning.GrantPage
	require.NoError(t, json.Unmarshal(grantResponse.Body.Bytes(), &grantPage))
	require.Equal(t, "my", grantPage.Scope)
	require.NotEmpty(t, grantPage.Data)
	for _, grant := range grantPage.Data {
		require.Equal(t, accessprovisioning.SubjectUser, grant.Subject.Type)
		require.Equal(t, users[0], grant.Subject.UserID)
	}

	listUsers := func(provider string) (int, []map[string]any) {
		t.Helper()
		response := pr4InvokePool(t, pool, orgAdminID, http.MethodGet,
			"/api/v1/users?page=1&page_size=1&auth_provider="+provider, "", "", nil, handler.ListUsers)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var payload struct {
			Data  []map[string]any `json:"data"`
			Total int              `json:"total"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
		return payload.Total, payload.Data
	}
	localTotal, localPage := listUsers("local")
	enterpriseTotal, enterprisePage := listUsers("enterprise")
	require.Equal(t, 7, localTotal)
	require.Equal(t, 1, enterpriseTotal)
	require.Len(t, enterprisePage, 1)
	require.Equal(t, "oidc", enterprisePage[0]["auth_provider"])
	require.NotEmpty(t, localPage)
	require.Equal(t, "local", localPage[0]["auth_provider"])

	invalidProvider := pr4InvokePool(t, pool, orgAdminID, http.MethodGet,
		"/api/v1/users?auth_provider=saml", "", "", nil, handler.ListUsers)
	require.Equal(t, http.StatusBadRequest, invalidProvider.Code, invalidProvider.Body.String())

	assertNavigation := func(userID int64, live, projectAdmin, review, workloads bool) {
		t.Helper()
		response := pr4InvokeRead(t, pool, userID, "/api/v1/me", handler.GetMe)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var profile struct {
			Navigation struct {
				HasLiveProjectMembership  bool `json:"has_live_project_membership"`
				HasProjectAdminMembership bool `json:"has_project_admin_membership"`
				CanDiscoverAccessReview   bool `json:"can_discover_access_review"`
				CanDiscoverWorkloads      bool `json:"can_discover_workloads"`
			} `json:"navigation_capabilities"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &profile))
		require.Equal(t, live, profile.Navigation.HasLiveProjectMembership)
		require.Equal(t, projectAdmin, profile.Navigation.HasProjectAdminMembership)
		require.Equal(t, review, profile.Navigation.CanDiscoverAccessReview)
		require.Equal(t, workloads, profile.Navigation.CanDiscoverWorkloads)
	}
	assertNavigation(projectAdminID, true, true, true, true)
	assertNavigation(viewerID, true, false, false, false)
	assertNavigation(noMembershipID, false, false, false, false)
	assertNavigation(orgAdminID, false, false, true, true)
}

func TestPR4ModelReadinessUsesLiveAuthoritiesWithoutMutationOrExternalTrafficIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, foreignOrgID, projectID, foreignProjectID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('pr4-readiness','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type) VALUES ('pr4-readiness-foreign','active','customer') RETURNING id`).Scan(&foreignOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'PR4 Readiness','pr4-readiness','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'PR4 Readiness Foreign','pr4-readiness-foreign','active') RETURNING id`, foreignOrgID).Scan(&foreignProjectID))

	seedUser := func(targetOrgID int64, username, organizationRole, compatibilityRole string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash',$3,$4,'active') RETURNING id`,
			targetOrgID, username, organizationRole, compatibilityRole).Scan(&id))
		return id
	}
	adminID := seedUser(orgID, "pr4-readiness-admin", auth.RoleOrgAdmin, auth.RoleOrgAdmin)
	actorID := seedUser(orgID, "pr4-readiness-actor", auth.RoleMember, auth.RoleDeveloper)
	sponsorID := seedUser(orgID, "pr4-readiness-sponsor", auth.RoleMember, auth.RoleDeveloper)
	viewerID := seedUser(orgID, "pr4-readiness-viewer", auth.RoleMember, auth.RoleViewer)
	billingID := seedUser(orgID, "pr4-readiness-billing", auth.RoleBillingAdmin, auth.RoleBillingAdmin)
	foreignActorID := seedUser(foreignOrgID, "pr4-readiness-foreign-actor", auth.RoleMember, auth.RoleDeveloper)
	for _, membership := range []struct {
		projectID int64
		userID    int64
		role      string
	}{
		{projectID, actorID, auth.ProjectRoleUser},
		{projectID, sponsorID, auth.ProjectRoleUser},
		{projectID, viewerID, auth.ProjectRoleViewer},
		{foreignProjectID, foreignActorID, auth.ProjectRoleUser},
	} {
		_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,$3)`,
			membership.projectID, membership.userID, membership.role)
		require.NoError(t, err)
	}

	for _, model := range []struct {
		name      string
		lifecycle string
		catalog   bool
	}{
		{"pr4-ready-model", "ga", true},
		{"pr4-hidden-model", "staging", false},
		{"pr4-alias-target", "ga", true},
		{"pr4-retired-model", "retired", false},
		{"pr4-route-missing", "ga", true},
		{"pr4-key-missing", "ga", true},
	} {
		_, err := pool.Exec(ctx, `
			INSERT INTO model_registry
			(org_id,model_name,display_name,provider_family,lifecycle,pricing_model_name,approved_catalog)
			VALUES ($1,$2,$2,'test',$3,$2,$4)`, orgID, model.name, model.lifecycle, model.catalog)
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO model_aliases (org_id,alias,target_model,display_name,status)
		VALUES ($1,'pr4-active-alias','pr4-alias-target','PR4 active alias','active')`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO model_aliases (org_id,alias,target_model,display_name,status)
		VALUES ($1,'pr4-disabled-alias','pr4-alias-target','PR4 disabled alias','disabled')`, orgID)
	require.NoError(t, err)
	for _, model := range []string{
		"pr4-ready-model", "pr4-hidden-model", "pr4-alias-target", "pr4-retired-model", "pr4-route-missing", "pr4-key-missing",
	} {
		_, err = pool.Exec(ctx, `INSERT INTO model_prices (model_name,input_price_cents,output_price_cents) VALUES ($1,1,2)`, model)
		require.NoError(t, err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO wallets (org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros)
		VALUES ($1,1000000,0,0,0)`, orgID)
	require.NoError(t, err)

	routing := &pr4RoutingPreview{previews: map[string]routingpolicy.Preview{}}
	for _, model := range []string{"pr4-ready-model", "pr4-runtime-only", "pr4-hidden-model", "pr4-alias-target", "pr4-retired-model"} {
		routing.previews[model] = routingpolicy.Preview{
			PolicyVersion: "unirouter.routing-policy.v1",
			Candidates: []routingpolicy.PreviewCandidate{{
				ChannelID: 101, ChannelName: "safe-aggregate", EligibleKeyCount: 1,
			}},
		}
	}
	routing.previews["pr4-key-missing"] = routingpolicy.Preview{
		PolicyVersion: "unirouter.routing-policy.v1",
		Candidates: []routingpolicy.PreviewCandidate{{
			ChannelID: 102, ChannelName: "missing-provider-key", EligibleKeyCount: 0,
		}},
	}
	evaluator, err := modelauthz.NewEvaluator(pool, "development")
	require.NoError(t, err)
	redisAddress := os.Getenv("REDIS_ADDR")
	if redisAddress == "" {
		redisAddress = "127.0.0.1:6379"
	}
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddress})
	t.Cleanup(func() { _ = redisClient.Close() })
	require.NoError(t, redisClient.Ping(ctx).Err(), "PR4 integration requires Redis 7")
	policyService, err := limitpolicy.NewService(pool, redisClient, limitpolicy.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, policyService.Refresh(ctx))

	handler := NewHandlerWithAPIKeyHashSecret(pool, "pr4-readiness-jwt", "pr4-readiness-key-hash", []byte("0123456789abcdef01234567"), nil)
	handler.SetRoutingController(routing)
	handler.SetModelAuthorizationEvaluator(evaluator)
	handler.SetLimitPolicyReloader(policyService)
	handler.SetBillingReadinessProvider(billing.NewBilling(pool, nil))

	createHumanGrant := func(userID int64, clientID string, validUntil *time.Time) accessprovisioning.ProvisionResult {
		t.Helper()
		validity := ""
		if validUntil != nil {
			validity = fmt.Sprintf(`,"valid_until":%q`, validUntil.UTC().Format(time.RFC3339Nano))
		}
		body := fmt.Sprintf(`{
			"org_id":%d,"project_id":%d,"subject":{"type":"user","user_id":%d},
			"request_kind":"human_personal",
			"requested_scope":{"project_role":"project_user","scope_mode":"project_default","models":[],"endpoints":[]},
			"environment":"personal","production_use":false,"submission_mode":"self"%s
		}`, orgID, projectID, userID, validity)
		response := pr4InvokePool(t, pool, userID, http.MethodPost, "/api/v1/access-requests", body, clientID, nil, handler.CreateAccessRequest)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
		var result accessprovisioning.ProvisionResult
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.NotNil(t, result.Grant)
		return result
	}
	issueCredential := func(userID int64, grantRef, name, clientID string) accessprovisioning.Credential {
		t.Helper()
		response := pr4InvokePool(t, pool, userID, http.MethodPost,
			"/api/v1/access-grants/"+grantRef+"/credentials", fmt.Sprintf(`{"name":%q}`, name), clientID,
			map[string]string{"ref": grantRef}, handler.IssueAccessGrantCredential)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
		var credential accessprovisioning.Credential
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &credential))
		require.True(t, credential.SecretAvailable)
		require.NotEmpty(t, credential.Key)
		return credential
	}

	human := createHumanGrant(actorID, "pr4-readiness-human-grant", nil)
	humanCredential := issueCredential(actorID, human.Grant.GrantRef, "PR4 human readiness", "pr4-readiness-human-key")

	readinessFor := func(actor int64, project int64, model, endpoint, caller, applicationRef string) (*httptest.ResponseRecorder, modelreadiness.Response) {
		t.Helper()
		query := url.Values{
			"project_id": {strconv.FormatInt(project, 10)},
			"model":      {model},
			"endpoint":   {endpoint},
			"caller":     {caller},
		}
		if applicationRef != "" {
			query.Set("application_ref", applicationRef)
		}
		response := pr4InvokeRead(t, pool, actor, "/api/v1/model-readiness?"+query.Encode(), handler.GetModelReadiness)
		var result modelreadiness.Response
		if response.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result), response.Body.String())
		}
		return response, result
	}
	readiness := func(actor int64, project int64, model, caller, applicationRef string) (*httptest.ResponseRecorder, modelreadiness.Response) {
		t.Helper()
		return readinessFor(actor, project, model, "/v1/chat/completions", caller, applicationRef)
	}

	readyHTTP, ready := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, readyHTTP.Code, readyHTTP.Body.String())
	require.Equal(t, modelreadiness.ContractVersion, ready.ContractVersion)
	require.NotNil(t, ready.Summary.Registered.Value)
	require.True(t, *ready.Summary.Registered.Value)
	require.True(t, *ready.Summary.CatalogVisible.Value)
	require.True(t, *ready.Summary.Routable.Value)
	require.True(t, *ready.Summary.CurrentProjectCallerCallable.Value, readyHTTP.Body.String())
	require.Equal(t, modelreadiness.StateNotApplicable, ready.ProjectCaller.Alias.State)
	require.Equal(t, "direct_model_optional", ready.ProjectCaller.Alias.ReasonCode)
	require.Equal(t, modelreadiness.StateNotApplicable, ready.ProjectCaller.ModelSet.State)
	require.Equal(t, "unrestricted_optional", ready.ProjectCaller.ModelSet.ReasonCode)
	require.Equal(t, modelreadiness.StateNotApplicable, ready.PlatformModel.RuntimeSlot.State)
	require.Equal(t, "none", ready.RuntimeMutation)
	require.Equal(t, "none", ready.ExternalRequests)
	require.NotContains(t, readyHTTP.Body.String(), humanCredential.Key)
	require.NotContains(t, readyHTTP.Body.String(), "key_hash")
	require.NotContains(t, readyHTTP.Body.String(), "base_url")
	mediaHTTP, mediaScope := readinessFor(actorID, projectID, "pr4-ready-model", "/v1/media/jobs", "self", "")
	require.Equal(t, http.StatusOK, mediaHTTP.Code, mediaHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, mediaScope.ProjectCaller.Grant.State)
	require.Equal(t, "access_grant_scope_excludes_request", mediaScope.ProjectCaller.Grant.ReasonCode)

	runtimeOnlyHTTP, runtimeOnly := readiness(actorID, projectID, "pr4-runtime-only", "self", "")
	require.Equal(t, http.StatusOK, runtimeOnlyHTTP.Code, runtimeOnlyHTTP.Body.String())
	require.False(t, *runtimeOnly.Summary.Registered.Value)
	require.False(t, *runtimeOnly.Summary.CatalogVisible.Value)
	require.True(t, *runtimeOnly.Summary.Routable.Value)
	require.False(t, *runtimeOnly.Summary.CurrentProjectCallerCallable.Value)
	require.Equal(t, "model_price_missing", runtimeOnly.Summary.CurrentProjectCallerCallable.ReasonCode)

	hiddenHTTP, hidden := readiness(actorID, projectID, "pr4-hidden-model", "self", "")
	require.Equal(t, http.StatusOK, hiddenHTTP.Code, hiddenHTTP.Body.String())
	require.True(t, *hidden.Summary.Registered.Value)
	require.False(t, *hidden.Summary.CatalogVisible.Value)
	require.True(t, *hidden.Summary.Routable.Value)
	require.True(t, *hidden.Summary.CurrentProjectCallerCallable.Value, hiddenHTTP.Body.String())

	aliasHTTP, alias := readiness(actorID, projectID, "pr4-active-alias", "self", "")
	require.Equal(t, http.StatusOK, aliasHTTP.Code, aliasHTTP.Body.String())
	require.Equal(t, "pr4-alias-target", alias.Scope.ResolvedModel)
	require.Equal(t, modelreadiness.StateReady, alias.ProjectCaller.Alias.State)
	disabledAliasHTTP, disabledAlias := readiness(actorID, projectID, "pr4-disabled-alias", "self", "")
	require.Equal(t, http.StatusOK, disabledAliasHTTP.Code, disabledAliasHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, disabledAlias.ProjectCaller.Alias.State)
	require.Equal(t, "alias_inactive", disabledAlias.ProjectCaller.Alias.ReasonCode)

	retiredHTTP, retired := readiness(actorID, projectID, "pr4-retired-model", "self", "")
	require.Equal(t, http.StatusOK, retiredHTTP.Code, retiredHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, retired.PlatformModel.Lifecycle.State)
	require.True(t, *retired.Summary.Registered.Value)
	require.True(t, *retired.Summary.Routable.Value)
	require.False(t, *retired.Summary.CurrentProjectCallerCallable.Value)

	routeMissingHTTP, routeMissing := readiness(actorID, projectID, "pr4-route-missing", "self", "")
	require.Equal(t, http.StatusOK, routeMissingHTTP.Code, routeMissingHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, routeMissing.PlatformModel.Channel.State)
	require.Equal(t, modelreadiness.StateBlocked, routeMissing.PlatformModel.ProviderCredential.State)
	require.Equal(t, modelreadiness.StateBlocked, routeMissing.PlatformModel.BaseRoute.State)
	keyMissingHTTP, keyMissing := readiness(actorID, projectID, "pr4-key-missing", "self", "")
	require.Equal(t, http.StatusOK, keyMissingHTTP.Code, keyMissingHTTP.Body.String())
	require.Equal(t, modelreadiness.StateReady, keyMissing.PlatformModel.Channel.State)
	require.Equal(t, modelreadiness.StateBlocked, keyMissing.PlatformModel.ProviderCredential.State)
	require.Equal(t, modelreadiness.StateBlocked, keyMissing.PlatformModel.BaseRoute.State)

	_, err = pool.Exec(ctx, `DELETE FROM wallets WHERE org_id=$1`, orgID)
	require.NoError(t, err)
	walletMissingHTTP, walletMissing := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, walletMissingHTTP.Code, walletMissingHTTP.Body.String())
	require.Equal(t, "wallet_missing", walletMissing.ProjectCaller.Wallet.ReasonCode)
	require.False(t, *walletMissing.Summary.CurrentProjectCallerCallable.Value)
	_, err = pool.Exec(ctx, `
		INSERT INTO wallets (org_id,balance_micros,granted_balance_micros,frozen_micros,total_spent_micros)
		VALUES ($1,0,0,0,0)`, orgID)
	require.NoError(t, err)
	walletEmptyHTTP, walletEmpty := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, walletEmptyHTTP.Code, walletEmptyHTTP.Body.String())
	require.Equal(t, "wallet_no_spendable_balance", walletEmpty.ProjectCaller.Wallet.ReasonCode)
	require.False(t, *walletEmpty.Summary.CurrentProjectCallerCallable.Value)
	_, err = pool.Exec(ctx, `UPDATE wallets SET balance_micros=1000000 WHERE org_id=$1`, orgID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE projects SET status='archived' WHERE id=$1`, projectID)
	require.NoError(t, err)
	inactiveProjectHTTP, inactiveProject := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, inactiveProjectHTTP.Code, inactiveProjectHTTP.Body.String())
	require.Equal(t, "project_inactive", inactiveProject.ProjectCaller.Project.ReasonCode)
	require.False(t, *inactiveProject.Summary.CurrentProjectCallerCallable.Value)
	_, err = pool.Exec(ctx, `UPDATE projects SET status='active' WHERE id=$1`, projectID)
	require.NoError(t, err)

	unavailableHandler := NewHandlerWithAPIKeyHashSecret(pool, "pr4-readiness-jwt", "pr4-readiness-key-hash", []byte("0123456789abcdef01234567"), nil)
	unavailableHandler.SetModelAuthorizationEvaluator(evaluator)
	unavailableHandler.SetLimitPolicyReloader(policyService)
	unavailableResponse := pr4InvokeRead(t, pool, actorID,
		fmt.Sprintf("/api/v1/model-readiness?project_id=%d&model=pr4-ready-model&endpoint=%%2Fv1%%2Fchat%%2Fcompletions&caller=self", projectID),
		unavailableHandler.GetModelReadiness)
	require.Equal(t, http.StatusOK, unavailableResponse.Code, unavailableResponse.Body.String())
	var unavailable modelreadiness.Response
	require.NoError(t, json.Unmarshal(unavailableResponse.Body.Bytes(), &unavailable))
	require.Equal(t, modelreadiness.StateUnavailable, unavailable.PlatformModel.BaseRoute.State)
	require.Equal(t, modelreadiness.StateUnavailable, unavailable.Summary.Routable.State)
	require.Nil(t, unavailable.Summary.Routable.Value)
	require.Equal(t, modelreadiness.StateUnavailable, unavailable.Summary.CurrentProjectCallerCallable.State)

	scopedActorID := seedUser(orgID, "pr4-readiness-scoped-actor", auth.RoleMember, auth.RoleDeveloper)
	_, err = pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, scopedActorID)
	require.NoError(t, err)
	scopedGrant := createHumanGrant(scopedActorID, "pr4-readiness-scoped-grant", nil)
	issueScoped := func(name, model, clientID string) accessprovisioning.Credential {
		t.Helper()
		response := pr4InvokePool(t, pool, scopedActorID, http.MethodPost,
			"/api/v1/access-grants/"+scopedGrant.Grant.GrantRef+"/credentials",
			fmt.Sprintf(`{"name":%q,"allowed_models":[%q],"allowed_endpoints":["/v1/chat/completions"]}`, name, model),
			clientID, map[string]string{"ref": scopedGrant.Grant.GrantRef}, handler.IssueAccessGrantCredential)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
		var credential accessprovisioning.Credential
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &credential))
		return credential
	}
	_ = issueScoped("PR4 scoped compatible", "pr4-ready-model", "pr4-readiness-scoped-compatible")
	deniedFamilyCredential := issueScoped("PR4 scoped family deny", "pr4-ready-model", "pr4-readiness-scoped-family-deny")
	var deniedFamilyID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT id FROM access_credentials WHERE credential_ref=$1`, deniedFamilyCredential.CredentialRef).Scan(&deniedFamilyID))
	familyDenyBody := fmt.Sprintf(`{
		"authorization_ref":"mca_pr4_readiness_family_deny","subject_type":"access_credential","subject_ref":%q,
		"environment":"development","effect":"deny","allowed_models":["pr4-ready-model"],
		"allowed_endpoints":["/v1/chat/completions"],"reason":"PR4 family-level deny"
	}`, strconv.FormatInt(deniedFamilyID, 10))
	familyDenyCreate := pr4InvokePool(t, pool, adminID, http.MethodPost, "/api/v1/model-call-authorizations",
		familyDenyBody, "", nil, handler.CreateModelAuthorization)
	require.Equal(t, http.StatusCreated, familyDenyCreate.Code, familyDenyCreate.Body.String())
	var familyDenyPolicy modelauthz.Policy
	require.NoError(t, json.Unmarshal(familyDenyCreate.Body.Bytes(), &familyDenyPolicy))
	familyDenyActivate := pr4InvokePool(t, pool, adminID, http.MethodPost,
		"/api/v1/model-call-authorizations/"+familyDenyPolicy.AuthorizationRef+"/activate",
		fmt.Sprintf(`{"expected_revision":%d}`, familyDenyPolicy.Revision), "",
		map[string]string{"ref": familyDenyPolicy.AuthorizationRef}, handler.ActivateModelAuthorization)
	require.Equal(t, http.StatusOK, familyDenyActivate.Code, familyDenyActivate.Body.String())
	_ = issueScoped("PR4 scoped newer incompatible", "pr4-alias-target", "pr4-readiness-scoped-incompatible")
	scopedReadyHTTP, scopedReady := readiness(scopedActorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, scopedReadyHTTP.Code, scopedReadyHTTP.Body.String())
	require.Equal(t, modelreadiness.StateReady, scopedReady.ProjectCaller.Credential.State,
		"readiness must find the older compatible family instead of trusting only the newest family")
	require.Equal(t, modelreadiness.StateReady, scopedReady.ProjectCaller.ModelAuthorization.State,
		"one family-specific deny must not hide another independently callable family")
	scopedExcludedHTTP, scopedExcluded := readiness(scopedActorID, projectID, "pr4-hidden-model", "self", "")
	require.Equal(t, http.StatusOK, scopedExcludedHTTP.Code, scopedExcludedHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, scopedExcluded.ProjectCaller.Credential.State)
	require.Equal(t, "access_credential_scope_excludes_request", scopedExcluded.ProjectCaller.Credential.ReasonCode)
	require.False(t, *scopedExcluded.Summary.CurrentProjectCallerCallable.Value)

	var modelSetID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO model_sets (org_id,name,slug,description,status)
		VALUES ($1,'PR4 only ready','pr4-only-ready','readiness test','active') RETURNING id`, orgID).Scan(&modelSetID))
	_, err = pool.Exec(ctx, `INSERT INTO model_set_items (model_set_id,item_type,item_name) VALUES ($1,'model','pr4-ready-model')`, modelSetID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO project_model_sets (project_id,model_set_id) VALUES ($1,$2)`, projectID, modelSetID)
	require.NoError(t, err)
	modelSetReadyHTTP, modelSetReady := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, modelSetReadyHTTP.Code, modelSetReadyHTTP.Body.String())
	require.Equal(t, modelreadiness.StateReady, modelSetReady.ProjectCaller.ModelSet.State)
	modelSetDeniedHTTP, modelSetDenied := readiness(actorID, projectID, "pr4-runtime-only", "self", "")
	require.Equal(t, http.StatusOK, modelSetDeniedHTTP.Code, modelSetDeniedHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, modelSetDenied.ProjectCaller.ModelSet.State)
	require.True(t, *modelSetDenied.Summary.Routable.Value)
	require.False(t, *modelSetDenied.Summary.CurrentProjectCallerCallable.Value)

	policyBody := fmt.Sprintf(`{
		"authorization_ref":"mca_pr4_readiness_deny","subject_type":"user","subject_ref":%q,
		"environment":"development","effect":"deny","allowed_models":["pr4-ready-model"],
		"allowed_endpoints":["/v1/chat/completions"],"reason":"PR4 user-level deny"
	}`, strconv.FormatInt(actorID, 10))
	createPolicy := pr4InvokePool(t, pool, adminID, http.MethodPost, "/api/v1/model-call-authorizations",
		policyBody, "", nil, handler.CreateModelAuthorization)
	require.Equal(t, http.StatusCreated, createPolicy.Code, createPolicy.Body.String())
	var policy modelauthz.Policy
	require.NoError(t, json.Unmarshal(createPolicy.Body.Bytes(), &policy))
	activatePolicy := pr4InvokePool(t, pool, adminID, http.MethodPost,
		"/api/v1/model-call-authorizations/"+policy.AuthorizationRef+"/activate",
		fmt.Sprintf(`{"expected_revision":%d}`, policy.Revision), "",
		map[string]string{"ref": policy.AuthorizationRef}, handler.ActivateModelAuthorization)
	require.Equal(t, http.StatusOK, activatePolicy.Code, activatePolicy.Body.String())
	require.NoError(t, policyService.Refresh(ctx))
	deniedHTTP, denied := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, deniedHTTP.Code, deniedHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, denied.ProjectCaller.ModelAuthorization.State)
	require.False(t, *denied.Summary.CurrentProjectCallerCallable.Value)

	var activePolicy modelauthz.Policy
	require.NoError(t, json.Unmarshal(activatePolicy.Body.Bytes(), &activePolicy))
	revokePolicy := pr4InvokePool(t, pool, adminID, http.MethodPost,
		"/api/v1/model-call-authorizations/"+policy.AuthorizationRef+"/revoke",
		fmt.Sprintf(`{"expected_revision":%d}`, activePolicy.Revision), "",
		map[string]string{"ref": policy.AuthorizationRef}, handler.RevokeModelAuthorization)
	require.Equal(t, http.StatusOK, revokePolicy.Code, revokePolicy.Body.String())

	rotateResponse := pr4InvokePool(t, pool, actorID, http.MethodPost,
		fmt.Sprintf("/api/v1/access-grants/%s/credentials/%d/rotate", human.Grant.GrantRef, humanCredential.ID),
		`{}`, "pr4-readiness-rotate", map[string]string{"ref": human.Grant.GrantRef, "key_id": strconv.FormatInt(humanCredential.ID, 10)},
		handler.RotateAccessGrantCredential)
	require.Equal(t, http.StatusCreated, rotateResponse.Code, rotateResponse.Body.String())
	var rotation accessprovisioning.RotationResult
	require.NoError(t, json.Unmarshal(rotateResponse.Body.Bytes(), &rotation))
	require.NotEqual(t, humanCredential.ID, rotation.Credential.ID)
	rotatedHTTP, rotated := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, rotatedHTTP.Code, rotatedHTTP.Body.String())
	require.True(t, *rotated.Summary.CurrentProjectCallerCallable.Value, rotatedHTTP.Body.String())

	suspendResponse := pr4InvokePool(t, pool, adminID, http.MethodPost,
		"/api/v1/access-grants/"+human.Grant.GrantRef+"/suspend",
		fmt.Sprintf(`{"expected_revision":%d,"reason":"PR4 readiness suspension"}`, human.Grant.Revision),
		"pr4-readiness-suspend", map[string]string{"ref": human.Grant.GrantRef}, handler.SuspendAccessGrant)
	require.Equal(t, http.StatusOK, suspendResponse.Code, suspendResponse.Body.String())
	var suspended accessprovisioning.Grant
	require.NoError(t, json.Unmarshal(suspendResponse.Body.Bytes(), &suspended))
	suspendedHTTP, suspendedReadiness := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, suspendedHTTP.Code, suspendedHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, suspendedReadiness.ProjectCaller.Grant.State)
	require.False(t, *suspendedReadiness.Summary.CurrentProjectCallerCallable.Value)

	resumeResponse := pr4InvokePool(t, pool, adminID, http.MethodPost,
		"/api/v1/access-grants/"+human.Grant.GrantRef+"/resume",
		fmt.Sprintf(`{"expected_revision":%d,"reason":"PR4 readiness resume"}`, suspended.Revision),
		"pr4-readiness-resume", map[string]string{"ref": human.Grant.GrantRef}, handler.ResumeAccessGrant)
	require.Equal(t, http.StatusOK, resumeResponse.Code, resumeResponse.Body.String())
	var resumed accessprovisioning.Grant
	require.NoError(t, json.Unmarshal(resumeResponse.Body.Bytes(), &resumed))
	_, err = pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, actorID)
	require.NoError(t, err)
	inactiveUserHTTP, inactiveUser := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, inactiveUserHTTP.Code, inactiveUserHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, inactiveUser.ProjectCaller.Credential.State)
	require.False(t, *inactiveUser.Summary.CurrentProjectCallerCallable.Value)
	_, err = pool.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, actorID)
	require.NoError(t, err)

	expiringID := seedUser(orgID, "pr4-readiness-expiring", auth.RoleMember, auth.RoleDeveloper)
	_, err = pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, expiringID)
	require.NoError(t, err)
	expiresAt := time.Now().UTC().Add(1200 * time.Millisecond)
	expiring := createHumanGrant(expiringID, "pr4-readiness-expiring-grant", &expiresAt)
	_ = issueCredential(expiringID, expiring.Grant.GrantRef, "PR4 expiring readiness", "pr4-readiness-expiring-key")
	if wait := time.Until(expiresAt.Add(75 * time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	expiredHTTP, expired := readiness(expiringID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, expiredHTTP.Code, expiredHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, expired.ProjectCaller.Grant.State)
	require.Equal(t, "access_grant_expired", expired.ProjectCaller.Grant.ReasonCode)

	// Native v2 Workload readiness uses the same live Grant/credential boundary,
	// including sponsor authority, rather than a compatibility UI approximation.
	applicationBody := fmt.Sprintf(`{
		"org_id":%d,"project_id":%d,"application_ref":"app_pr4_readiness",
		"logical_ref":"workload_pr4_readiness","display_name":"PR4 Readiness Workload",
		"kind":"service","environment":"development","production_use":false,
		"identity_mode":"service_credential","client_profiles":[],"owner_team":"AI Platform",
		"description":"PR4 readiness fixture","status":"active"
	}`, orgID, projectID)
	applicationResponse := pr4InvokePool(t, pool, adminID, http.MethodPost, "/api/v1/workload-applications",
		applicationBody, "", nil, handler.CreateWorkloadApplication)
	require.Equal(t, http.StatusCreated, applicationResponse.Code, applicationResponse.Body.String())
	var application workloadApplicationRecord
	require.NoError(t, json.Unmarshal(applicationResponse.Body.Bytes(), &application))
	require.True(t, application.CanManage)
	require.True(t, application.CanRequestAccess)
	require.True(t, application.CanReadCredentials)
	workloadRequestBody := fmt.Sprintf(`{
		"org_id":%d,"project_id":%d,"subject":{"type":"workload_application","workload_application_id":%d},
		"request_kind":"workload_service",
		"requested_scope":{"project_role":"project_user","scope_mode":"project_default","models":[],"endpoints":[]},
		"environment":"development","production_use":false,"submission_mode":"direct_grant","sponsor_user_id":%d
	}`, orgID, projectID, application.ID, sponsorID)
	workloadGrantResponse := pr4InvokePool(t, pool, adminID, http.MethodPost, "/api/v1/access-requests",
		workloadRequestBody, "pr4-readiness-workload-grant", nil, handler.CreateAccessRequest)
	require.Equal(t, http.StatusCreated, workloadGrantResponse.Code, workloadGrantResponse.Body.String())
	var workloadGrant accessprovisioning.ProvisionResult
	require.NoError(t, json.Unmarshal(workloadGrantResponse.Body.Bytes(), &workloadGrant))
	require.NotNil(t, workloadGrant.Grant)
	workloadRequestList := pr4InvokeRead(t, pool, adminID,
		fmt.Sprintf("/api/v1/access-requests?scope=project&project_id=%d&workload_application_id=%d&limit=20", projectID, application.ID),
		handler.ListAccessRequests)
	require.Equal(t, http.StatusOK, workloadRequestList.Code, workloadRequestList.Body.String())
	var workloadRequestPage accessprovisioning.RequestPage
	require.NoError(t, json.Unmarshal(workloadRequestList.Body.Bytes(), &workloadRequestPage))
	require.Len(t, workloadRequestPage.Data, 1)
	require.Equal(t, application.ID, workloadRequestPage.Data[0].Subject.WorkloadApplicationID)
	workloadGrantList := pr4InvokeRead(t, pool, adminID,
		fmt.Sprintf("/api/v1/access-grants?scope=project&project_id=%d&workload_application_id=%d&limit=20", projectID, application.ID),
		handler.ListAccessGrants)
	require.Equal(t, http.StatusOK, workloadGrantList.Code, workloadGrantList.Body.String())
	var workloadGrantPage accessprovisioning.GrantPage
	require.NoError(t, json.Unmarshal(workloadGrantList.Body.Bytes(), &workloadGrantPage))
	require.Len(t, workloadGrantPage.Data, 1)
	require.Equal(t, application.ID, workloadGrantPage.Data[0].Subject.WorkloadApplicationID)
	workloadCredential := issueCredential(adminID, workloadGrant.Grant.GrantRef, "PR4 workload readiness", "pr4-readiness-workload-key")
	require.Positive(t, workloadCredential.ID)
	workloadHTTP, workload := readiness(adminID, projectID, "pr4-ready-model", "workload_application", application.ApplicationRef)
	require.Equal(t, http.StatusOK, workloadHTTP.Code, workloadHTTP.Body.String())
	require.Equal(t, modelreadiness.StateReady, workload.ProjectCaller.Application.State)
	require.True(t, *workload.Summary.CurrentProjectCallerCallable.Value, workloadHTTP.Body.String())

	_, err = pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, sponsorID)
	require.NoError(t, err)
	orphanedHTTP, orphaned := readiness(adminID, projectID, "pr4-ready-model", "workload_application", application.ApplicationRef)
	require.Equal(t, http.StatusOK, orphanedHTTP.Code, orphanedHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, orphaned.ProjectCaller.Credential.State)
	require.False(t, *orphaned.Summary.CurrentProjectCallerCallable.Value)
	_, err = pool.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, sponsorID)
	require.NoError(t, err)

	foreignVisible := pr4InvokeRead(t, pool, foreignActorID,
		fmt.Sprintf("/api/v1/model-readiness?project_id=%d&model=pr4-ready-model&endpoint=%%2Fv1%%2Fchat%%2Fcompletions&caller=self", projectID),
		handler.GetModelReadiness)
	absentVisible := pr4InvokeRead(t, pool, foreignActorID,
		"/api/v1/model-readiness?project_id=9223372036854770000&model=pr4-ready-model&endpoint=%2Fv1%2Fchat%2Fcompletions&caller=self",
		handler.GetModelReadiness)
	require.Equal(t, http.StatusNotFound, foreignVisible.Code, foreignVisible.Body.String())
	require.Equal(t, foreignVisible.Code, absentVisible.Code)
	require.JSONEq(t, foreignVisible.Body.String(), absentVisible.Body.String())
	billingVisible := pr4InvokeRead(t, pool, billingID,
		fmt.Sprintf("/api/v1/model-readiness?project_id=%d&model=pr4-ready-model&endpoint=%%2Fv1%%2Fchat%%2Fcompletions&caller=self", projectID),
		handler.GetModelReadiness)
	require.Equal(t, http.StatusNotFound, billingVisible.Code, billingVisible.Body.String())
	viewerHTTP, viewer := readiness(viewerID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, viewerHTTP.Code, viewerHTTP.Body.String())
	require.Empty(t, viewer.PlatformModel.Registration.FixRoute, "viewer must not receive an inaccessible mutation route")

	var auditBefore, outboxBefore int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditBefore))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM internal_outbox_events`).Scan(&outboxBefore))
	for index := 0; index < 3; index++ {
		response, projection := readiness(actorID, projectID, "pr4-ready-model", "self", "")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, "none", projection.RuntimeMutation)
		require.Equal(t, "none", projection.ExternalRequests)
	}
	var auditAfter, outboxAfter int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&auditAfter))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM internal_outbox_events`).Scan(&outboxAfter))
	require.Equal(t, auditBefore, auditAfter)
	require.Equal(t, outboxBefore, outboxAfter)
	require.Greater(t, routing.callCount(), 0)

	revokeResponse := pr4InvokePool(t, pool, adminID, http.MethodPost,
		"/api/v1/access-grants/"+human.Grant.GrantRef+"/revoke",
		fmt.Sprintf(`{"expected_revision":%d,"reason":"PR4 readiness revoke"}`, resumed.Revision),
		"pr4-readiness-revoke", map[string]string{"ref": human.Grant.GrantRef}, handler.RevokeAccessGrant)
	require.Equal(t, http.StatusOK, revokeResponse.Code, revokeResponse.Body.String())
	revokedHTTP, revoked := readiness(actorID, projectID, "pr4-ready-model", "self", "")
	require.Equal(t, http.StatusOK, revokedHTTP.Code, revokedHTTP.Body.String())
	require.Equal(t, modelreadiness.StateBlocked, revoked.ProjectCaller.Grant.State)
	require.False(t, *revoked.Summary.CurrentProjectCallerCallable.Value)
}
