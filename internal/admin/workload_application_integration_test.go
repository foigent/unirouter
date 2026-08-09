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
		request = request.WithContext(context.WithValue(request.Context(), middleware.CtxRequestID, "req-workload-integration"))
		return withIntegrationCommittingActor(t, pool, request, tenant.userID)
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

func TestWorkloadReadinessCountsOnlyRuntimeEffectiveCredentials(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, actorID, sponsorID, projectID, applicationID, requestID, grantID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('workload-readiness-grant','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'workload-readiness-admin','hash','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'workload-readiness-sponsor','hash','member','developer','active') RETURNING id`, orgID).Scan(&sponsorID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'Workload Readiness Grant','workload-readiness-grant','active') RETURNING id`, orgID).Scan(&projectID))
	_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, sponsorID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO workload_applications (
		  contract_version,identity_contract_version,org_id,project_id,application_ref,logical_ref,
		  display_name,kind,environment,identity_mode,owner_team,status,environment_locked_at,created_by,updated_by
		) VALUES (
		  'unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,
		  'app_readiness_grant','workload_readiness_grant','Readiness Grant','service',
		  'development','service_credential','platform','active',NOW(),$3,$3
		) RETURNING id`, orgID, projectID, actorID).Scan(&applicationID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
		  subject_workload_application_id,request_kind,requested_project_role,requested_scope_mode,
		  requested_models,requested_endpoints,environment,requested_valid_until,submission_mode,sponsor_user_id,
		  status,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,
		  submitted_by,decided_by,decided_at
		) VALUES (
		  'req_readiness_grant_000001','readiness-grant',repeat('a',64),$1,$2,'workload_application',
		  $3,'workload_service','project_user','project_default','[]','[]','development',
		  NOW()+INTERVAL '1 hour','direct_grant',$4,
		  'approved','direct',ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
		  'unirouter.enterprise-access-policy.v1',$5,$5,NOW()
		) RETURNING id`, orgID, projectID, applicationID, sponsorID, actorID).Scan(&requestID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_grants (
		  grant_ref,access_request_id,org_id,project_id,subject_type,subject_workload_application_id,
		  sponsor_user_id,project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,
		  environment,status,valid_from,valid_until,decision_mode,policy_reason_codes,
		  decision_reason_codes,policy_version,semantic_hash,created_by,updated_by
		) VALUES (
		  'grant_readiness_grant_00001',$1,$2,$3,'workload_application',$4,$5,'project_user',
		  'project_default','[]','[]','development','active',NOW()-INTERVAL '1 hour',
		  (SELECT requested_valid_until FROM access_requests WHERE id=$1),
		  'direct',ARRAY['project_default_scope'],
		  ARRAY['direct_grant_authorized_nonproduction'],'unirouter.enterprise-access-policy.v1',
		  repeat('a',64),$6,$6
		) RETURNING id`, requestID, orgID, projectID, applicationID, sponsorID, actorID).Scan(&grantID))
	handler := NewHandler(pool, "workload-read-model-jwt", []byte("0123456789abcdef01234567"), nil)

	var logicalCredentialID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_credentials (
		  credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,allowed_models,
		  allowed_endpoints,policy_labels,rpm_limit,rps_limit,created_by
		) VALUES (
		  'cred_readiness_grant_0001',$1,$2,$3,'readiness grant family','','',
		  '/v1/chat/completions,/v1/embeddings,/v1/models','',60,10,$4
		) RETURNING id`, grantID, orgID, projectID, actorID).Scan(&logicalCredentialID))
	insertKey := func(t *testing.T, prefix string, accessGrantID, accessCredentialID, rotatedFromKeyID int64, status string) int64 {
		t.Helper()
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO api_keys (
			  org_id,project_id,key_type,name,key_hash,key_prefix,status,allowed_ips,
			  allowed_models,allowed_endpoints,purpose,workload_application_id,access_grant_id,
			  access_credential_id,credential_ref,rotated_from_key_id
			) VALUES ($1,$2,'service',CASE WHEN $6=0 THEN $3 ELSE 'readiness grant family' END,
			  repeat('c',64),$3,$8,'','',
			  CASE WHEN $6=0 THEN '/v1/chat/completions' ELSE '/v1/chat/completions,/v1/embeddings,/v1/models' END,
			  '',$4,NULLIF($5,0),NULLIF($6,0),CASE WHEN $6=0 THEN NULL ELSE 'cred_readiness_grant_0001' END,
			  NULLIF($7,0)) RETURNING id`, orgID, projectID, prefix, applicationID, accessGrantID,
			accessCredentialID, rotatedFromKeyID, status).Scan(&id))
		return id
	}
	legacyKeyID := insertKey(t, "ur-readiness-legacy", 0, 0, 0, "active")
	disabledPredecessorID := insertKey(t, "ur-readiness-old", grantID, logicalCredentialID, 0, "disabled")
	grantKeyID := insertKey(t, "ur-readiness-grant", grantID, logicalCredentialID, disabledPredecessorID, "active")
	const runtimeKeyHashSecret = "workload-readiness-runtime-secret"
	rawGrantKey, grantKeyPrefix, err := auth.GenerateAPIKey(orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE api_keys SET key_hash=$2,key_prefix=$3 WHERE id=$1`,
		grantKeyID, auth.HashAPIKey(rawGrantKey, runtimeKeyHashSecret), grantKeyPrefix)
	require.NoError(t, err)

	readIDs := func(t *testing.T) []int64 {
		t.Helper()
		credentials, err := readActiveWorkloadCredentials(ctx, pool, orgID, applicationID)
		require.NoError(t, err)
		ids := make([]int64, 0, len(credentials))
		for _, credential := range credentials {
			identityID := credential.ID
			if credential.AccessCredentialID > 0 {
				identityID = credential.AccessCredentialID
			}
			ids = append(ids, identityID)
		}
		return ids
	}
	assertReadModelCount := func(t *testing.T, expected int) {
		t.Helper()
		getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/workload-applications/app_readiness_grant", nil)
		getRequest.SetPathValue("ref", "app_readiness_grant")
		getRequest = withIntegrationCommittingActor(t, pool, getRequest, actorID)
		_, directReadErr := readWorkloadApplication(getRequest.Context(), pool, orgID, "app_readiness_grant", false)
		require.NoError(t, directReadErr)
		getResponse := httptest.NewRecorder()
		handler.GetWorkloadApplication(getResponse, getRequest)
		require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
		var detail workloadApplicationRecord
		require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &detail))
		require.Equal(t, 2, detail.CredentialCount, "rotation versions must count as one logical credential family")
		require.Equal(t, expected, detail.ActiveCredentialCount, "detail must expose runtime-effective active credential families")

		listRequest := httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/api/v1/workload-applications?project_id=%d", projectID), nil)
		listRequest = withIntegrationCommittingActor(t, pool, listRequest, actorID)
		listResponse := httptest.NewRecorder()
		handler.ListWorkloadApplications(listResponse, listRequest)
		require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())
		var list struct {
			Data []workloadApplicationRecord `json:"data"`
		}
		require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &list))
		require.Len(t, list.Data, 1)
		require.Equal(t, expected, list.Data[0].ActiveCredentialCount, "list and detail must share one effective-active definition")
	}
	assertReadinessCredential := func(t *testing.T, runtimeEffective bool) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet,
			"/api/v1/workload-applications/app_readiness_grant/readiness", nil)
		request.SetPathValue("ref", "app_readiness_grant")
		request = withIntegrationCommittingActor(t, pool, request, actorID)
		response := httptest.NewRecorder()
		handler.GetWorkloadApplicationReadiness(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var readiness workloadapp.ReadinessResult
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &readiness))
		var credentialAxis workloadapp.Axis
		for _, axis := range readiness.Axes {
			if axis.Key == "application_credential" {
				credentialAxis = axis
				break
			}
		}
		if runtimeEffective {
			require.NotEqual(t, "no_active_application_credential", credentialAxis.ReasonCode)
			return
		}
		require.Equal(t, workloadapp.AxisBlocked, credentialAxis.State)
		require.Equal(t, "no_active_application_credential", credentialAxis.ReasonCode)
	}
	runtimeProtected := middleware.GatewayAuthWithAPIKeyHashSecret(pool, runtimeKeyHashSecret)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	assertRuntimeAuth := func(t *testing.T, runtimeEffective bool) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		request.Header.Set("Authorization", "Bearer "+rawGrantKey)
		response := httptest.NewRecorder()
		runtimeProtected.ServeHTTP(response, request)
		if runtimeEffective {
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			return
		}
		require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	}
	require.Len(t, readIDs(t), 2, "legacy key plus one logical family must be ready")
	require.Contains(t, readIDs(t), legacyKeyID)
	require.NotContains(t, readIDs(t), disabledPredecessorID, "disabled predecessor versions must never become separate readiness seats")
	require.NotEqual(t, grantKeyID, logicalCredentialID, "fixture must distinguish physical version and logical family IDs")
	assertReadModelCount(t, 2)
	credentialListRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/workload-applications/app_readiness_grant/credentials", nil)
	credentialListRequest.SetPathValue("ref", "app_readiness_grant")
	credentialListRequest = withIntegrationCommittingActor(t, pool, credentialListRequest, actorID)
	credentialListResponse := httptest.NewRecorder()
	handler.ListWorkloadApplicationCredentials(credentialListResponse, credentialListRequest)
	require.Equal(t, http.StatusOK, credentialListResponse.Code, credentialListResponse.Body.String())
	var credentialList struct {
		Data []workloadCredentialRecord `json:"data"`
	}
	require.NoError(t, json.Unmarshal(credentialListResponse.Body.Bytes(), &credentialList))
	require.Len(t, credentialList.Data, 2, "one logical family plus one legacy seat")
	var family, legacy *workloadCredentialRecord
	for index := range credentialList.Data {
		switch credentialList.Data[index].Classification {
		case "grant_bound":
			family = &credentialList.Data[index]
		case "legacy_unbound":
			legacy = &credentialList.Data[index]
		}
	}
	require.NotNil(t, family)
	require.Equal(t, "cred_readiness_grant_0001", family.CredentialRef)
	require.Equal(t, 2, family.VersionCount, "atomic replacement history stays nested under one family")
	require.Len(t, family.Versions, 2)
	require.Equal(t, "active", family.EffectiveStatus)
	require.NotNil(t, legacy)
	require.Equal(t, 1, legacy.VersionCount)
	require.Len(t, legacy.Versions, 1)
	updateGrantBindingForCorruptionTest := func(t *testing.T, statement string, args ...any) {
		t.Helper()
		_, disableErr := pool.Exec(ctx, `ALTER TABLE access_grants DISABLE TRIGGER access_grants_request_binding_exact`)
		require.NoError(t, disableErr)
		_, updateErr := pool.Exec(ctx, statement, args...)
		_, enableErr := pool.Exec(ctx, `ALTER TABLE access_grants ENABLE TRIGGER access_grants_request_binding_exact`)
		require.NoError(t, enableErr)
		require.NoError(t, updateErr)
	}

	_, err = pool.Exec(ctx, `UPDATE access_grants SET status='suspended',suspended_at=NOW() WHERE id=$1`, grantID)
	require.NoError(t, err)
	require.Equal(t, []int64{legacyKeyID}, readIDs(t))
	assertReadModelCount(t, 1)
	_, err = pool.Exec(ctx, `UPDATE access_grants SET status='active',suspended_at=NULL WHERE id=$1`, grantID)
	require.NoError(t, err)
	updateGrantBindingForCorruptionTest(t,
		`UPDATE access_grants SET valid_until=NOW()-INTERVAL '1 second' WHERE id=$1`, grantID)
	require.Equal(t, []int64{legacyKeyID}, readIDs(t))
	assertReadModelCount(t, 1)
	updateGrantBindingForCorruptionTest(t,
		`UPDATE access_grants SET valid_until=NOW()+INTERVAL '1 hour' WHERE id=$1`, grantID)
	require.ElementsMatch(t, []int64{legacyKeyID, logicalCredentialID}, readIDs(t))
	assertReadModelCount(t, 2)

	// From here on the legacy key is disabled so every read-model and readiness
	// assertion is solely about the Grant-bound family.
	_, err = pool.Exec(ctx, `UPDATE api_keys SET status='disabled' WHERE id=$1`, legacyKeyID)
	require.NoError(t, err)
	assertReadModelCount(t, 1)
	assertReadinessCredential(t, true)
	assertRuntimeAuth(t, true)

	_, err = pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, sponsorID)
	require.NoError(t, err)
	require.Empty(t, readIDs(t))
	assertReadModelCount(t, 0)
	assertReadinessCredential(t, false)
	assertRuntimeAuth(t, false)
	_, err = pool.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, sponsorID)
	require.NoError(t, err)
	assertRuntimeAuth(t, true)

	_, err = pool.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, sponsorID)
	require.NoError(t, err)
	require.Empty(t, readIDs(t))
	assertReadModelCount(t, 0)
	assertReadinessCredential(t, false)
	assertRuntimeAuth(t, false)
	_, err = pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, sponsorID)
	require.NoError(t, err)
	assertRuntimeAuth(t, true)

	updateGrantBindingForCorruptionTest(t, `UPDATE access_grants SET environment=$1 WHERE id=$2`, "staging", grantID)
	require.Empty(t, readIDs(t))
	assertReadModelCount(t, 0)
	assertReadinessCredential(t, false)
	assertRuntimeAuth(t, false)
	updateGrantBindingForCorruptionTest(t, `UPDATE access_grants SET environment=$1 WHERE id=$2`, "development", grantID)
	assertRuntimeAuth(t, true)

	updateGrantBindingForCorruptionTest(t, `UPDATE access_grants SET production_use=$1 WHERE id=$2`, true, grantID)
	require.Empty(t, readIDs(t))
	assertReadModelCount(t, 0)
	assertReadinessCredential(t, false)
	assertRuntimeAuth(t, false)
	updateGrantBindingForCorruptionTest(t, `UPDATE access_grants SET production_use=$1 WHERE id=$2`, false, grantID)
	assertRuntimeAuth(t, true)

	_, err = pool.Exec(ctx, `UPDATE access_grants SET status='revoked',revoked_at=NOW() WHERE id=$1`, grantID)
	require.NoError(t, err)
	require.Empty(t, readIDs(t))
	assertReadModelCount(t, 0)
}

func TestWorkloadApplicationHTTPReadsFailClosedWithoutPrincipalOrProjectMembershipIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectA, projectB, actorID, creatorID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('workload-read-authz','active','customer') RETURNING id`).Scan(&orgID))
	for target, slug := range map[*int64]string{&projectA: "workload-read-a", &projectB: "workload-read-b"} {
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO projects (org_id,name,slug,status)
			VALUES ($1,$2,$2,'active') RETURNING id`, orgID, slug).Scan(target))
	}
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'workload-read-actor','hash','member','developer','active') RETURNING id`, orgID).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'workload-read-creator','hash','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&creatorID))
	_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectA, actorID)
	require.NoError(t, err)
	seedApplication := func(projectID int64, ref, logicalRef string) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO workload_applications (
			  contract_version,identity_contract_version,org_id,project_id,application_ref,logical_ref,
			  display_name,kind,environment,identity_mode,owner_team,status,environment_locked_at,created_by,updated_by
			) VALUES (
			  'unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,$3,$4,
			  $3,'service','development','service_credential','platform','active',NOW(),$5,$5
			)`, orgID, projectID, ref, logicalRef, creatorID)
		require.NoError(t, err)
	}
	seedApplication(projectA, "app_workload_read_a", "workload_read_a")
	seedApplication(projectB, "app_workload_read_b", "workload_read_b")
	handler := NewHandler(pool, "workload-read-authz-jwt", []byte("0123456789abcdef01234567"), nil)

	missingPrincipal := httptest.NewRequest(http.MethodGet, "/api/v1/workload-applications/app_workload_read_a", nil)
	missingPrincipal.SetPathValue("ref", "app_workload_read_a")
	missingPrincipal = missingPrincipal.WithContext(context.WithValue(missingPrincipal.Context(), middleware.CtxOrgID, orgID))
	missingResponse := httptest.NewRecorder()
	handler.GetWorkloadApplication(missingResponse, missingPrincipal)
	require.Equal(t, http.StatusNotFound, missingResponse.Code, missingResponse.Body.String())

	readAsActor := func(ref string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/workload-applications/"+ref, nil)
		request.SetPathValue("ref", ref)
		request = withIntegrationCommittingActor(t, pool, request, actorID)
		response := httptest.NewRecorder()
		handler.GetWorkloadApplication(response, request)
		return response
	}
	allowed := readAsActor("app_workload_read_a")
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	inaccessible := readAsActor("app_workload_read_b")
	absent := readAsActor("app_workload_read_absent")
	require.Equal(t, http.StatusNotFound, inaccessible.Code, inaccessible.Body.String())
	require.Equal(t, absent.Code, inaccessible.Code)
	require.JSONEq(t, absent.Body.String(), inaccessible.Body.String())

	missingList := httptest.NewRequest(http.MethodGet, "/api/v1/workload-applications", nil)
	missingList = missingList.WithContext(context.WithValue(missingList.Context(), middleware.CtxOrgID, orgID))
	missingListResponse := httptest.NewRecorder()
	handler.ListWorkloadApplications(missingListResponse, missingList)
	require.Equal(t, http.StatusOK, missingListResponse.Code, missingListResponse.Body.String())
	require.JSONEq(t, `{"data":[],"total":0}`, missingListResponse.Body.String())

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/workload-applications", nil)
	listRequest = withIntegrationCommittingActor(t, pool, listRequest, actorID)
	listResponse := httptest.NewRecorder()
	handler.ListWorkloadApplications(listResponse, listRequest)
	require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())
	var list struct {
		Data []workloadApplicationRecord `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	require.Equal(t, projectA, list.Data[0].ProjectID)
}
