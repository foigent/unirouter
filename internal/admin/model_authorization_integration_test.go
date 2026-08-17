//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPRD8ModelAuthorizationLifecycleTenantIsolationAndEvaluationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	type tenant struct {
		orgID, userID, projectID, keyID int64
	}
	seedTenant := func(suffix string) tenant {
		t.Helper()
		var value tenant
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO orgs (name,status,org_type)
			VALUES ($1,'active','standard') RETURNING id`, "prd8-model-authz-"+suffix).Scan(&value.orgID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,role,status)
			VALUES ($1,$2,'test-hash','org_admin','active') RETURNING id`, value.orgID, "authz-admin-"+suffix).Scan(&value.userID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO projects (org_id,name,slug,status)
			VALUES ($1,$2,$3,'active') RETURNING id`, value.orgID, "Authz Project "+suffix, "authz-project-"+suffix).Scan(&value.projectID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO api_keys (
			  org_id,project_id,owner_user_id,name,key_prefix,key_hash,status,key_type)
			VALUES ($1,$2,$3,$4,$5,$6,'active','personal') RETURNING id`,
			value.orgID, value.projectID, value.userID, "Authz Key "+suffix,
			"ur_authz_"+suffix, "hash-authz-"+suffix).Scan(&value.keyID))
		return value
	}
	a := seedTenant("a")
	b := seedTenant("b")

	evaluator, err := modelauthz.NewEvaluator(pool, "production")
	require.NoError(t, err)
	handler := NewHandler(pool, "prd8-model-authz-secret", []byte("0123456789abcdef01234567"), nil)
	handler.SetModelAuthorizationEvaluator(evaluator)

	requestFor := func(tenant tenant, method, target, body string) *http.Request {
		t.Helper()
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		return withIntegrationCommittingActor(t, pool, request, tenant.userID)
	}
	create := func(tenant tenant, ref, subjectType string, subjectID int64, effect string, models, endpoints []string) modelauthz.Policy {
		t.Helper()
		body, marshalErr := json.Marshal(map[string]interface{}{
			"authorization_ref": ref,
			"subject_type":      subjectType,
			"subject_ref":       strconv.FormatInt(subjectID, 10),
			"environment":       "production",
			"effect":            effect,
			"allowed_models":    models,
			"allowed_endpoints": endpoints,
			"reason":            "enterprise model execution scope",
		})
		require.NoError(t, marshalErr)
		request := requestFor(tenant, http.MethodPost, "/api/v1/model-call-authorizations", string(body))
		response := httptest.NewRecorder()
		handler.CreateModelAuthorization(response, request)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
		var policy modelauthz.Policy
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &policy))
		require.Equal(t, modelauthz.StatusDraft, policy.Status)
		return policy
	}
	transition := func(tenant tenant, policy modelauthz.Policy, activate bool) modelauthz.Policy {
		t.Helper()
		action := "activate"
		if !activate {
			action = "revoke"
		}
		request := requestFor(tenant, http.MethodPost,
			fmt.Sprintf("/api/v1/model-call-authorizations/%s/%s", policy.AuthorizationRef, action),
			fmt.Sprintf(`{"expected_revision":%d}`, policy.Revision))
		request.SetPathValue("ref", policy.AuthorizationRef)
		response := httptest.NewRecorder()
		if activate {
			handler.ActivateModelAuthorization(response, request)
		} else {
			handler.RevokeModelAuthorization(response, request)
		}
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var updated modelauthz.Policy
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &updated))
		return updated
	}

	allow := create(a, "mca_prd8_allow_a", modelauthz.SubjectAPIKey, a.keyID, modelauthz.EffectAllow,
		[]string{"gpt-4o"}, []string{"/v1/chat/completions", "/v1/models"})
	allow = transition(a, allow, true)
	require.Equal(t, modelauthz.StatusActive, allow.Status)
	require.EqualValues(t, 2, allow.Revision)

	input := modelauthz.Input{
		OrgID: a.orgID, APIKeyID: a.keyID, ProjectID: a.projectID, OwnerUserID: a.userID,
		Endpoint: "/v1/chat/completions", RequestedModel: "gpt-4o", ResolvedModel: "gpt-4o",
	}
	decision, err := evaluator.Evaluate(ctx, input)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Equal(t, "explicit_allow_intersection", decision.Reason)
	require.Equal(t, []string{allow.AuthorizationRef}, decision.PolicyRefs)

	input.RequestedModel, input.ResolvedModel = "claude-3", "claude-3"
	decision, err = evaluator.Evaluate(ctx, input)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, "allow_scope_excludes_model", decision.Reason)

	filtered, err := evaluator.FilterModels(ctx, modelauthz.Input{
		OrgID: a.orgID, APIKeyID: a.keyID, ProjectID: a.projectID, OwnerUserID: a.userID,
		Endpoint: "/v1/models",
	}, []string{"claude-3", "gpt-4o"})
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-4o"}, filtered)

	deny := create(a, "mca_prd8_deny_project", modelauthz.SubjectProject, a.projectID, modelauthz.EffectDeny,
		[]string{"gpt-4o"}, []string{"/v1/chat/completions"})
	deny = transition(a, deny, true)
	input.RequestedModel, input.ResolvedModel = "gpt-4o", "gpt-4o"
	decision, err = evaluator.Evaluate(ctx, input)
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, "explicit_deny", decision.Reason)
	require.Equal(t, []string{deny.AuthorizationRef}, decision.PolicyRefs)

	deny = transition(a, deny, false)
	require.Equal(t, modelauthz.StatusRevoked, deny.Status)
	decision, err = evaluator.Evaluate(ctx, input)
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	create(b, "mca_prd8_allow_b", modelauthz.SubjectAPIKey, b.keyID, modelauthz.EffectAllow,
		[]string{"claude-3"}, []string{"/v1/chat/completions"})
	crossTenantBody := fmt.Sprintf(`{
		"authorization_ref":"mca_prd8_cross_tenant",
		"subject_type":"api_key","subject_ref":"%d","environment":"production",
		"effect":"allow","allowed_models":["*"],
		"allowed_endpoints":["/v1/chat/completions"],"reason":"must fail"
	}`, b.keyID)
	crossTenantRequest := requestFor(a, http.MethodPost, "/api/v1/model-call-authorizations", crossTenantBody)
	crossTenantResponse := httptest.NewRecorder()
	handler.CreateModelAuthorization(crossTenantResponse, crossTenantRequest)
	require.Equal(t, http.StatusForbidden, crossTenantResponse.Code)

	listRequest := requestFor(a, http.MethodGet, "/api/v1/model-call-authorizations", "")
	listResponse := httptest.NewRecorder()
	handler.ListModelAuthorizations(listResponse, listRequest)
	require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())
	require.Contains(t, listResponse.Body.String(), "mca_prd8_allow_a")
	require.NotContains(t, listResponse.Body.String(), "mca_prd8_allow_b")

	crossListRequest := requestFor(a, http.MethodGet,
		fmt.Sprintf("/api/v1/model-call-authorizations?org_id=%d", b.orgID), "")
	crossListResponse := httptest.NewRecorder()
	handler.ListModelAuthorizations(crossListResponse, crossListRequest)
	require.Equal(t, http.StatusForbidden, crossListResponse.Code)
}
