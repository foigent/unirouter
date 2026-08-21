//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPRD8GatewayModelAuthorizationPEPDeniesBeforeProviderDispatch(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, userID, projectID, keyID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('prd8-model-authz-pep','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status)
		VALUES ($1,'prd8-pep-owner','test-hash','org_admin','active') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'PRD8 PEP','prd8-pep','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,owner_user_id,name,key_prefix,key_hash,status,key_type)
		VALUES ($1,$2,$3,'PRD8 PEP key','ur_prd8_pep','hash-prd8-pep','active','personal')
		RETURNING id`, orgID, projectID, userID).Scan(&keyID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO model_call_authorizations (
		  org_id,authorization_ref,subject_type,subject_ref,environment,effect,
		  allowed_models,allowed_endpoints,status,revision,reason,created_by,
		  activated_by,activated_at)
		VALUES ($1,'mca_prd8_pep_deny','api_key',$2,'production','deny',
		  '["gpt-4o"]','["/v1/chat/completions"]','active',1,'deny before dispatch',$3,$3,NOW())
		RETURNING id`, orgID, strconv.FormatInt(keyID, 10), userID).Scan(new(int64)))

	evaluator, err := modelauthz.NewEvaluator(pool, "production")
	require.NoError(t, err)
	relay := gateway.NewRelay(pool, nil, nil, nil, nil, nil)
	relay.SetModelAuthorization(evaluator)

	apiKey := &models.ApiKey{
		ID: keyID, OrgID: orgID, ProjectID: &projectID, OwnerUserID: &userID,
		KeyType: "personal", Status: "active",
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"blocked"}]}`))
	requestContext := context.WithValue(request.Context(), middleware.CtxAPIKey, apiKey)
	requestContext = context.WithValue(requestContext, middleware.CtxOrgID, orgID)
	requestContext = context.WithValue(requestContext, middleware.CtxProjectID, projectID)
	requestContext = context.WithValue(requestContext, middleware.CtxKeyID, keyID)
	requestContext = context.WithValue(requestContext, middleware.CtxRequestID, "prd8-model-authz-pep-deny")
	request = request.WithContext(requestContext)
	response := httptest.NewRecorder()

	governed, allowed := prepareGatewayModelRequest(response, request, pool, relay, nil)
	require.False(t, allowed)
	require.Nil(t, governed)
	require.Equal(t, http.StatusForbidden, response.Code)
	require.Contains(t, response.Body.String(), "auth_error.model_call_not_authorized")
	require.Contains(t, response.Body.String(), "mca_prd8_pep_deny")

	var auditCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_logs
		WHERE org_id=$1 AND action='model_authorization.deny'
		  AND request_id='prd8-model-authz-pep-deny'`, orgID).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
}

func TestGatewayDirectCanonicalModelPublishesTrustedNormalizedIdentity(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('gateway-direct-canonical','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'Direct Canonical','direct-canonical','active') RETURNING id`, orgID).Scan(&projectID))

	apiKey := &models.ApiKey{ID: 91, OrgID: orgID, ProjectID: &projectID, AllowedModels: "direct-model"}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"  direct-model  ","messages":[]}`))
	requestContext := context.WithValue(request.Context(), middleware.CtxAPIKey, apiKey)
	requestContext = context.WithValue(requestContext, middleware.CtxOrgID, orgID)
	requestContext = context.WithValue(requestContext, middleware.CtxProjectID, projectID)
	requestContext = context.WithValue(requestContext, middleware.CtxKeyID, apiKey.ID)
	request = request.WithContext(requestContext)
	response := httptest.NewRecorder()

	governed, allowed := prepareGatewayModelRequest(response, request, pool, nil, nil)
	require.True(t, allowed, response.Body.String())
	require.NotNil(t, governed)
	require.Equal(t, "direct-model", middleware.GetModelAliasFromContext(governed.Context()))
	var body struct {
		Model string `json:"model"`
	}
	require.NoError(t, json.NewDecoder(governed.Body).Decode(&body))
	require.Equal(t, "direct-model", body.Model)
}
