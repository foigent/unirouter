//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/workloadapp"
)

type workloadRoutingPreviewStub struct {
	preview routingpolicy.Preview
}

func (stub workloadRoutingPreviewStub) Preview(context.Context, string) (routingpolicy.Preview, error) {
	return stub.preview, nil
}

func (workloadRoutingPreviewStub) Invalidate() {}

func TestWorkloadApplicationLifecycleCredentialBindingReadinessAndTenantIsolation(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	type tenant struct {
		orgID, userID, projectID int64
	}
	seed := func(suffix string) tenant {
		t.Helper()
		var value tenant
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO orgs (name,status,org_type)
			VALUES ($1,'active','standard') RETURNING id`, "workload-app-org-"+suffix).Scan(&value.orgID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,role,status)
			VALUES ($1,$2,'test-hash','org_admin','active') RETURNING id`, value.orgID, "workload-admin-"+suffix).Scan(&value.userID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO projects (org_id,name,slug,status)
			VALUES ($1,$2,$3,'active') RETURNING id`, value.orgID, "Workload Project "+suffix, "workload-project-"+suffix).Scan(&value.projectID))
		return value
	}
	a := seed("a")
	b := seed("b")

	evaluator, err := modelauthz.NewEvaluator(pool, "poc_2")
	require.NoError(t, err)
	const keySecret = "workload-application-api-key-secret"
	handler := NewHandlerWithAPIKeyHashSecret(pool, "workload-jwt-secret", keySecret, []byte("0123456789abcdef01234567"), nil)
	handler.SetModelAuthorizationEvaluator(evaluator)
	handler.SetRoutingController(workloadRoutingPreviewStub{preview: routingpolicy.Preview{
		PolicyVersion: "routing-policy.v1", Model: "company-chat",
		Candidates: []routingpolicy.PreviewCandidate{{ChannelID: 1, ChannelName: "poc", EligibleKeyCount: 1}},
	}})

	requestFor := func(tenant tenant, method, target, body string) *http.Request {
		t.Helper()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		requestContext := context.WithValue(request.Context(), middleware.CtxUserID, tenant.userID)
		requestContext = context.WithValue(requestContext, middleware.CtxOrgID, tenant.orgID)
		requestContext = context.WithValue(requestContext, middleware.CtxUserRole, auth.RoleOrgAdmin)
		requestContext = context.WithValue(requestContext, middleware.CtxRequestID, "req-workload-integration")
		return request.WithContext(requestContext)
	}
	createBody := fmt.Sprintf(`{
		"project_id":%d,"application_ref":"app_claims_agent","display_name":"Claims Agent",
		"kind":"agent","environment":"poc_2","identity_mode":"service_credential",
		"client_profile":"client.openai-sdk.chat.v1","owner_team":"AI Platform",
		"description":"POC agent"
	}`, a.projectID)
	createResponse := httptest.NewRecorder()
	handler.CreateWorkloadApplication(createResponse, requestFor(a, http.MethodPost, "/api/v1/workload-applications", createBody))
	require.Equal(t, http.StatusCreated, createResponse.Code, createResponse.Body.String())
	var application workloadApplicationRecord
	require.NoError(t, json.Unmarshal(createResponse.Body.Bytes(), &application))
	require.Equal(t, workloadapp.StatusActive, application.Status)
	require.EqualValues(t, 1, application.Revision)

	crossBody := strings.Replace(createBody, "app_claims_agent", "app_cross_tenant", 1)
	crossBody = strings.Replace(crossBody, fmt.Sprintf(`"project_id":%d`, a.projectID), fmt.Sprintf(`"org_id":%d,"project_id":%d`, b.orgID, b.projectID), 1)
	crossResponse := httptest.NewRecorder()
	handler.CreateWorkloadApplication(crossResponse, requestFor(a, http.MethodPost, "/api/v1/workload-applications", crossBody))
	require.Equal(t, http.StatusForbidden, crossResponse.Code, crossResponse.Body.String())

	keyBody := fmt.Sprintf(`{
		"project_id":%d,"name":"Claims Agent primary","key_type":"service",
		"allowed_models":"company-chat","workload_application_ref":"app_claims_agent"
	}`, a.projectID)
	keyResponse := httptest.NewRecorder()
	handler.CreateAPIKey(keyResponse, requestFor(a, http.MethodPost, "/api/v1/keys", keyBody))
	require.Equal(t, http.StatusCreated, keyResponse.Code, keyResponse.Body.String())
	var keyPayload struct {
		ID                    int64  `json:"id"`
		Key                   string `json:"key"`
		WorkloadApplicationID int64  `json:"workload_application_id"`
	}
	require.NoError(t, json.Unmarshal(keyResponse.Body.Bytes(), &keyPayload))
	require.NotEmpty(t, keyPayload.Key)
	require.Equal(t, application.ID, keyPayload.WorkloadApplicationID)

	var storedApplicationID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT workload_application_id FROM api_keys WHERE id=$1`, keyPayload.ID).Scan(&storedApplicationID))
	require.Equal(t, application.ID, storedApplicationID)
	keyListResponse := httptest.NewRecorder()
	handler.ListAPIKeys(keyListResponse, requestFor(a, http.MethodGet, "/api/v1/keys?page=1&page_size=100", ""))
	require.Equal(t, http.StatusOK, keyListResponse.Code, keyListResponse.Body.String())
	var keyListPayload struct {
		Data []struct {
			ID                      int64  `json:"id"`
			WorkloadApplicationID   int64  `json:"workload_application_id"`
			WorkloadApplicationRef  string `json:"workload_application_ref"`
			WorkloadApplicationName string `json:"workload_application_name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(keyListResponse.Body.Bytes(), &keyListPayload))
	require.Len(t, keyListPayload.Data, 1)
	require.Equal(t, keyPayload.ID, keyListPayload.Data[0].ID)
	require.Equal(t, application.ID, keyListPayload.Data[0].WorkloadApplicationID)
	require.Equal(t, "app_claims_agent", keyListPayload.Data[0].WorkloadApplicationRef)
	require.Equal(t, "Claims Agent", keyListPayload.Data[0].WorkloadApplicationName)

	readinessRequest := requestFor(a, http.MethodGet,
		"/api/v1/workload-applications/app_claims_agent/readiness?model=company-chat&endpoint=%2Fv1%2Fchat%2Fcompletions", "")
	readinessRequest.SetPathValue("ref", "app_claims_agent")
	readinessResponse := httptest.NewRecorder()
	handler.GetWorkloadApplicationReadiness(readinessResponse, readinessRequest)
	require.Equal(t, http.StatusOK, readinessResponse.Code, readinessResponse.Body.String())
	var readiness workloadapp.ReadinessResult
	require.NoError(t, json.Unmarshal(readinessResponse.Body.Bytes(), &readiness))
	require.Equal(t, workloadapp.OverallReady, readiness.Overall, readinessResponse.Body.String())

	updateRequest := requestFor(a, http.MethodPut, "/api/v1/workload-applications/app_claims_agent", fmt.Sprintf(`{
		"expected_revision":%d,"display_name":"Claims Agent","environment":"poc_2",
		"client_profile":"client.openai-sdk.chat.v1","owner_team":"AI Platform",
		"description":"temporarily disabled","status":"disabled"
	}`, application.Revision))
	updateRequest.SetPathValue("ref", "app_claims_agent")
	updateResponse := httptest.NewRecorder()
	handler.UpdateWorkloadApplication(updateResponse, updateRequest)
	require.Equal(t, http.StatusOK, updateResponse.Code, updateResponse.Body.String())
	require.NoError(t, json.Unmarshal(updateResponse.Body.Bytes(), &application))
	require.Equal(t, workloadapp.StatusDisabled, application.Status)

	downstreamCalled := false
	protected := middleware.GatewayAuthWithAPIKeyHashSecret(pool, keySecret)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		downstreamCalled = true
	}))
	authRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	authRequest.Header.Set("Authorization", "Bearer "+keyPayload.Key)
	authResponse := httptest.NewRecorder()
	protected.ServeHTTP(authResponse, authRequest)
	require.Equal(t, http.StatusUnauthorized, authResponse.Code, authResponse.Body.String())
	require.False(t, downstreamCalled)

	updateRequest = requestFor(a, http.MethodPut, "/api/v1/workload-applications/app_claims_agent", fmt.Sprintf(`{
		"expected_revision":%d,"display_name":"Claims Agent","environment":"poc_2",
		"client_profile":"client.openai-sdk.chat.v1","owner_team":"AI Platform",
		"description":"ready","status":"active"
	}`, application.Revision))
	updateRequest.SetPathValue("ref", "app_claims_agent")
	updateResponse = httptest.NewRecorder()
	handler.UpdateWorkloadApplication(updateResponse, updateRequest)
	require.Equal(t, http.StatusOK, updateResponse.Code, updateResponse.Body.String())
	require.NoError(t, json.Unmarshal(updateResponse.Body.Bytes(), &application))

	archiveRequest := requestFor(a, http.MethodPost, "/api/v1/workload-applications/app_claims_agent/archive",
		fmt.Sprintf(`{"expected_revision":%d}`, application.Revision))
	archiveRequest.SetPathValue("ref", "app_claims_agent")
	archiveResponse := httptest.NewRecorder()
	handler.ArchiveWorkloadApplication(archiveResponse, archiveRequest)
	require.Equal(t, http.StatusOK, archiveResponse.Code, archiveResponse.Body.String())

	var applicationStatus, keyStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM workload_applications WHERE id=$1`, application.ID).Scan(&applicationStatus))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id=$1`, keyPayload.ID).Scan(&keyStatus))
	require.Equal(t, workloadapp.StatusArchived, applicationStatus)
	require.Equal(t, "disabled", keyStatus)

	var receiptRows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE resource='workload_application' AND resource_id='app_claims_agent'`).Scan(&receiptRows))
	require.GreaterOrEqual(t, receiptRows, 4)
}
