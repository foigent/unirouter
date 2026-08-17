//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDeploymentNetworkDesiredStateLifecycleDoesNotMutateObservedStateIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('Deployment API','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users (org_id,username,password_hash,role,status) VALUES ($1,'deployment-platform','hash','platform_admin','active') RETURNING id`, orgID).Scan(&userID))
	handler := NewHandler(pool, "deployment-jwt", []byte("0123456789abcdef01234567"), nil)

	requestFor := func(method, target, body string) *http.Request {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		requestContext := context.WithValue(req.Context(), middleware.CtxUserID, userID)
		requestContext = context.WithValue(requestContext, middleware.CtxOrgID, orgID)
		requestContext = context.WithValue(requestContext, middleware.CtxUserRole, auth.RolePlatformAdmin)
		requestContext = context.WithValue(requestContext, middleware.CtxRequestID, "req-deployment-integration")
		return req.WithContext(requestContext)
	}

	profileResponse := httptest.NewRecorder()
	handler.CreateDeploymentProfile(profileResponse, requestFor(http.MethodPost, "/api/v1/deployment/profiles", `{
		"profile_ref":"dep_ecs_poc","display_name":"ECS POC","kind":"ecs_development",
		"environment":"poc","ingress_mode":"bundled_reverse_proxy","dns_mode":"ip_address",
		"outbound_mode":"direct","notes":"second integration","status":"active"
	}`))
	require.Equal(t, http.StatusCreated, profileResponse.Code, profileResponse.Body.String())

	invalidEndpointResponse := httptest.NewRecorder()
	handler.CreateGatewayEndpoint(invalidEndpointResponse, requestFor(http.MethodPost, "/api/v1/deployment/endpoints", `{
		"deployment_profile_ref":"dep_ecs_poc","endpoint_ref":"gwe_http_public","display_name":"Bad public HTTP",
		"base_url":"http://gateway.example.com","visibility":"public","tls_mode":"disabled",
		"certificate_ref":"","endpoint_profile":"endpoint.openai-chat.v1","status":"active"
	}`))
	require.Equal(t, http.StatusBadRequest, invalidEndpointResponse.Code, invalidEndpointResponse.Body.String())

	endpointResponse := httptest.NewRecorder()
	handler.CreateGatewayEndpoint(endpointResponse, requestFor(http.MethodPost, "/api/v1/deployment/endpoints", `{
		"deployment_profile_ref":"dep_ecs_poc","endpoint_ref":"gwe_ecs_poc","display_name":"ECS POC Gateway",
		"base_url":"https://gateway.example.com","visibility":"public","tls_mode":"external_termination",
		"certificate_ref":"secret://tls/gateway","endpoint_profile":"endpoint.openai-chat.v1","status":"active"
	}`))
	require.Equal(t, http.StatusCreated, endpointResponse.Code, endpointResponse.Body.String())
	var created struct {
		ID              int64  `json:"id"`
		DesiredRevision int64  `json:"desired_revision"`
		ObservedStatus  string `json:"observed_status"`
		RuntimeMutation string `json:"runtime_mutation"`
	}
	require.NoError(t, json.Unmarshal(endpointResponse.Body.Bytes(), &created))
	require.EqualValues(t, 1, created.DesiredRevision)
	require.Equal(t, "unverified", created.ObservedStatus)
	require.Equal(t, "none", created.RuntimeMutation)

	updateRequest := requestFor(http.MethodPut, "/api/v1/deployment/endpoints/gwe_ecs_poc", `{
		"expected_revision":1,"display_name":"ECS POC Gateway","base_url":"https://gateway.example.com",
		"visibility":"public","tls_mode":"external_termination","certificate_ref":"secret://tls/gateway-v2",
		"endpoint_profile":"endpoint.openai-chat.v1","status":"active"
	}`)
	updateRequest.SetPathValue("ref", "gwe_ecs_poc")
	updateResponse := httptest.NewRecorder()
	handler.UpdateGatewayEndpoint(updateResponse, updateRequest)
	require.Equal(t, http.StatusOK, updateResponse.Code, updateResponse.Body.String())

	var revision int64
	var observedStatus, observedBaseURL string
	var observedAt any
	require.NoError(t, pool.QueryRow(ctx, `SELECT desired_revision, observed_status, observed_base_url, observed_at FROM gateway_endpoints WHERE id=$1`, created.ID).Scan(&revision, &observedStatus, &observedBaseURL, &observedAt))
	require.EqualValues(t, 2, revision)
	require.Equal(t, "unverified", observedStatus)
	require.Empty(t, observedBaseURL)
	require.Nil(t, observedAt)

	_, err := pool.Exec(ctx, `UPDATE gateway_endpoints SET observed_status='ready', observed_base_url=base_url, observed_evidence='{"probe":"operator-only"}'::jsonb, observed_at=NOW() WHERE id=$1`, created.ID)
	require.NoError(t, err)
	orgReadRequest := httptest.NewRequest(http.MethodGet, "/api/v1/deployment/endpoints", nil)
	orgReadContext := context.WithValue(orgReadRequest.Context(), middleware.CtxUserID, userID)
	orgReadContext = context.WithValue(orgReadContext, middleware.CtxOrgID, orgID)
	orgReadContext = context.WithValue(orgReadContext, middleware.CtxUserRole, auth.RoleOrgAdmin)
	orgReadRequest = orgReadRequest.WithContext(orgReadContext)
	orgReadResponse := httptest.NewRecorder()
	handler.ListGatewayEndpoints(orgReadResponse, orgReadRequest)
	require.Equal(t, http.StatusOK, orgReadResponse.Code, orgReadResponse.Body.String())
	var orgReadPayload struct {
		Data []gatewayEndpointRecord `json:"data"`
	}
	require.NoError(t, json.Unmarshal(orgReadResponse.Body.Bytes(), &orgReadPayload))
	require.Len(t, orgReadPayload.Data, 1)
	require.Equal(t, "https://gateway.example.com", orgReadPayload.Data[0].BaseURL)
	require.Empty(t, orgReadPayload.Data[0].CertificateRef)
	require.Empty(t, orgReadPayload.Data[0].ObservedEvidence)

	var auditCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE resource IN ('deployment_profile','gateway_endpoint')`).Scan(&auditCount))
	require.Equal(t, 3, auditCount)
}
