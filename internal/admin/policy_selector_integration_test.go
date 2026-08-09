//go:build integration

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/accessprovisioning"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPolicyMutationsReauthorizeActorAndLockCanonicalFamilyAtCommit(t *testing.T) {
	for _, operation := range []string{"limit", "guardrail", "model_authorization"} {
		for _, change := range []string{"actor_epoch", "family_revoked"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				pool := testutil.SetupMigratedTestDB(t)
				ctx := context.Background()
				fx := seedP6F16Fixture(t, pool)
				seedHandler := NewHandlerWithAPIKeyHashSecret(pool, p6f16JWTSecret, "policy-commit-key-hash", []byte("0123456789abcdef01234567"), nil)
				keyID, credentialID, _ := seedPolicySelectorCredential(t, pool, seedHandler, fx.orgA, fx.projectA, fx.orgAdminA, "commit-"+operation+"-"+change)

				beginReached := make(chan struct{})
				allowBegin := make(chan struct{})
				database := &apiKeyInterleavingDB{pool: pool, beginReached: beginReached, allowBegin: allowBegin}
				handler := NewHandlerWithAPIKeyHashSecret(database, p6f16JWTSecret, "policy-commit-key-hash", []byte("0123456789abcdef01234567"), nil)
				guardrailService, err := guardrail.NewService(pool, nil, []byte("0123456789abcdef0123456789abcdef"), guardrail.DefaultOptions())
				require.NoError(t, err)
				require.NoError(t, guardrailService.Refresh(ctx))
				handler.SetGuardrailController(guardrailService)

				requestID := "policy-commit-" + operation + "-" + change
				var request *http.Request
				var invoke func(http.ResponseWriter, *http.Request)
				var table, lookupColumn, name string
				switch operation {
				case "limit":
					name, table, lookupColumn = requestID, "distributed_limit_policies", "name"
					request = apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/limit-policies",
						fmt.Sprintf(`{"org_id":%d,"project_id":%d,"api_key_id":%d,"name":%q,"mode":"enforce","rpm_limit":30}`,
							fx.orgA, fx.projectA, keyID, name), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, requestID)
					invoke = handler.CreateLimitPolicy
				case "guardrail":
					name, table, lookupColumn = requestID, "guardrail_policies", "name"
					request = apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/guardrail-policies",
						fmt.Sprintf(`{"org_id":%d,"project_id":%d,"api_key_id":%d,"name":%q,"stage":"input","detector_ref":"builtin:pii_secrets","action":"audit","mode":"shadow","failure_mode":"degraded_allow","config":{}}`,
							fx.orgA, fx.projectA, keyID, name), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, requestID)
					invoke = handler.CreateGuardrailPolicy
				case "model_authorization":
					name, table, lookupColumn = "mca_"+change, "model_call_authorizations", "authorization_ref"
					request = apiKeyIntegrationRequest(t, pool, http.MethodPost, "/api/v1/model-call-authorizations",
						fmt.Sprintf(`{"org_id":%d,"authorization_ref":%q,"subject_type":"api_key","subject_ref":%q,"environment":"production","effect":"deny","allowed_models":["gpt-4o"],"allowed_endpoints":["/v1/chat/completions"],"reason":"commit-time family policy"}`,
							fx.orgA, name, fmt.Sprintf("%d", keyID)), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, requestID)
					invoke = handler.CreateModelAuthorization
				default:
					t.Fatalf("unknown operation %q", operation)
				}

				responseCh := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					response := httptest.NewRecorder()
					invoke(response, request)
					responseCh <- response
				}()
				select {
				case <-beginReached:
				case <-time.After(5 * time.Second):
					t.Fatal("policy mutation did not reach its committing transaction")
				}

				switch change {
				case "actor_epoch":
					_, err = pool.Exec(ctx, `UPDATE users SET auth_epoch=auth_epoch+1 WHERE id=$1`, fx.orgAdminA)
				case "family_revoked":
					_, err = pool.Exec(ctx, `UPDATE access_credentials SET status='revoked',revoked_at=NOW(),revoked_by=$2,updated_at=NOW() WHERE id=$1`, credentialID, fx.orgAdminA)
				}
				require.NoError(t, err)
				close(allowBegin)

				var response *httptest.ResponseRecorder
				select {
				case response = <-responseCh:
				case <-time.After(5 * time.Second):
					t.Fatal("policy mutation did not finish after the committed change")
				}
				if change == "actor_epoch" {
					require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
				} else {
					require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, response.Code, response.Body.String())
				}
				var rows, audits int
				require.NoError(t, pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s=$1`, table, lookupColumn), name).Scan(&rows))
				require.Zero(t, rows)
				require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE request_id=$1`, requestID).Scan(&audits))
				require.Zero(t, audits)
			})
		}
	}
}

func TestPolicySelectorsCanonicalizeVisibleBoundKeysAndHideForeignObjects(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)
	handler := NewHandlerWithAPIKeyHashSecret(pool, p6f16JWTSecret, "policy-selector-key-hash", []byte("0123456789abcdef01234567"), nil)
	guardrailService, err := guardrail.NewService(pool, nil, []byte("0123456789abcdef0123456789abcdef"), guardrail.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, guardrailService.Refresh(ctx))
	handler.SetGuardrailController(guardrailService)

	keyA, credentialA, credentialRefA := seedPolicySelectorCredential(t, pool, handler, fx.orgA, fx.projectA, fx.orgAdminA, "org-a")
	keyB, _, _ := seedPolicySelectorCredential(t, pool, handler, fx.orgB, fx.projectB, fx.orgAdminB, "org-b")
	var projectAOther, keyAOther int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status,created_by)
		VALUES ($1,'Policy Selector Other','policy-selector-other','active',$2) RETURNING id`,
		fx.orgA, fx.orgAdminA).Scan(&projectAOther))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit
		) VALUES ($1,$2,$3,'personal','policy selector other',repeat('f',64),'policy-other','active',60,10)
		RETURNING id`, fx.orgA, projectAOther, fx.orgAdminA).Scan(&keyAOther))

	limitMux := p6f07LimitPolicyMux(handler)
	guardrailMux := p6f10GuardrailPolicyMux(handler)
	serve := func(tb testing.TB, mux *http.ServeMux, method, target, body string, userID, orgID int64, role string) *httptest.ResponseRecorder {
		tb.Helper()
		token := testutil.MustAdminSessionJWT(tb, ctx, pool, userID, orgID, role, p6f16JWTSecret)
		request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}

	limitBody := func(apiKeyID int64, name string) string {
		return fmt.Sprintf(`{"org_id":%d,"project_id":%d,"api_key_id":%d,"name":%q,"mode":"enforce","rpm_limit":30}`,
			fx.orgA, fx.projectA, apiKeyID, name)
	}
	guardrailBody := func(apiKeyID int64, name string) string {
		return fmt.Sprintf(`{"org_id":%d,"project_id":%d,"api_key_id":%d,"name":%q,"stage":"input","detector_ref":"builtin:pii_secrets","action":"audit","mode":"shadow","failure_mode":"degraded_allow","config":{}}`,
			fx.orgA, fx.projectA, apiKeyID, name)
	}

	boundLimit := serve(t, limitMux, http.MethodPost, "/api/v1/limit-policies", limitBody(keyA, "bound-limit"), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusCreated, boundLimit.Code, boundLimit.Body.String())
	boundLimitPayload := decodePolicySelectorPayload(t, boundLimit)
	limitID := int64(boundLimitPayload["id"].(float64))
	require.Nil(t, boundLimitPayload["api_key_id"])
	require.Equal(t, float64(credentialA), boundLimitPayload["access_credential_id"])
	require.Equal(t, credentialRefA, boundLimitPayload["access_credential_ref"])

	legacyLimit := serve(t, limitMux, http.MethodPost, "/api/v1/limit-policies", limitBody(fx.keyA, "legacy-limit"), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusCreated, legacyLimit.Code, legacyLimit.Body.String())
	legacyLimitPayload := decodePolicySelectorPayload(t, legacyLimit)
	require.Equal(t, float64(fx.keyA), legacyLimitPayload["api_key_id"])
	require.Nil(t, legacyLimitPayload["access_credential_id"])

	boundGuardrail := serve(t, guardrailMux, http.MethodPost, "/api/v1/guardrail-policies", guardrailBody(keyA, "bound-guardrail"), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusCreated, boundGuardrail.Code, boundGuardrail.Body.String())
	boundGuardrailPayload := decodePolicySelectorPayload(t, boundGuardrail)
	guardrailID := int64(boundGuardrailPayload["id"].(float64))
	require.Nil(t, boundGuardrailPayload["api_key_id"])
	require.Equal(t, float64(credentialA), boundGuardrailPayload["access_credential_id"])
	require.Equal(t, credentialRefA, boundGuardrailPayload["access_credential_ref"])

	for name, mux := range map[string]*http.ServeMux{"limit": limitMux, "guardrail": guardrailMux} {
		t.Run(name+" create selector response equivalence", func(t *testing.T) {
			body := limitBody
			if name == "guardrail" {
				body = guardrailBody
			}
			absent := serve(t, mux, http.MethodPost, "/api/v1/"+policyRouteName(name), body(keyB+9_000_000, "absent"), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
			foreign := serve(t, mux, http.MethodPost, "/api/v1/"+policyRouteName(name), body(keyB, "foreign"), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
			mismatch := serve(t, mux, http.MethodPost, "/api/v1/"+policyRouteName(name), body(keyAOther, "mismatch"), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
			require.Equal(t, http.StatusNotFound, absent.Code, absent.Body.String())
			require.Equal(t, absent.Code, foreign.Code)
			require.Equal(t, absent.Body.String(), foreign.Body.String())
			require.Equal(t, absent.Code, mismatch.Code)
			require.Equal(t, absent.Body.String(), mismatch.Body.String())
		})
	}

	for name, test := range map[string]struct {
		mux      *http.ServeMux
		policyID int64
	}{
		"limit":     {mux: limitMux, policyID: limitID},
		"guardrail": {mux: guardrailMux, policyID: guardrailID},
	} {
		t.Run(name+" update selector response equivalence", func(t *testing.T) {
			target := fmt.Sprintf("/api/v1/%s/%d", policyRouteName(name), test.policyID)
			body := func(keyID int64) string { return fmt.Sprintf(`{"version":1,"api_key_id":%d}`, keyID) }
			absent := serve(t, test.mux, http.MethodPut, target, body(keyB+9_000_000), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
			foreign := serve(t, test.mux, http.MethodPut, target, body(keyB), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
			mismatch := serve(t, test.mux, http.MethodPut, target, body(keyAOther), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
			require.Equal(t, http.StatusNotFound, absent.Code, absent.Body.String())
			require.Equal(t, absent.Body.String(), foreign.Body.String())
			require.Equal(t, absent.Body.String(), mismatch.Body.String())
		})
	}

	for _, table := range []string{"distributed_limit_policies", "guardrail_policies"} {
		var apiKeyID, accessCredentialID *int64
		query := fmt.Sprintf(`SELECT api_key_id,access_credential_id FROM %s WHERE name=$1`, table)
		name := "bound-limit"
		if table == "guardrail_policies" {
			name = "bound-guardrail"
		}
		require.NoError(t, pool.QueryRow(ctx, query, name).Scan(&apiKeyID, &accessCredentialID))
		require.Nil(t, apiKeyID)
		require.NotNil(t, accessCredentialID)
		require.Equal(t, credentialA, *accessCredentialID)
	}
	var auditEvidence string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT detail::text FROM audit_logs
		WHERE resource='guardrail_policy' AND resource_id=$1 ORDER BY id DESC LIMIT 1`,
		fmt.Sprintf("%d", guardrailID)).Scan(&auditEvidence))
	require.Contains(t, auditEvidence, credentialRefA)
	require.Contains(t, auditEvidence, fmt.Sprintf(`"access_credential_id": %d`, credentialA))
}

func seedPolicySelectorCredential(t *testing.T, pool *pgxpool.Pool, handler *Handler, orgID, projectID, actorID int64, suffix string) (int64, int64, string) {
	t.Helper()
	provisionPayload := fmt.Sprintf(`{
		"org_id":%d,"project_id":%d,"subject":{"type":"user","user_id":%d},
		"request_kind":"human_personal","requested_scope":{"project_role":"project_user","scope_mode":"project_default","models":[],"endpoints":[]},
		"environment":"personal","production_use":false,"submission_mode":"direct_grant"
	}`, orgID, projectID, actorID)
	provisionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests", bytes.NewBufferString(provisionPayload))
	provisionRequest.Header.Set("Content-Type", "application/json")
	provisionRequest.Header.Set(middleware.ClientRequestIDHeader, "policy-selector-grant-"+suffix)
	provisionRequest = withIntegrationCommittingActor(t, pool, provisionRequest, actorID)
	provisionResponse := httptest.NewRecorder()
	middleware.OperationIdentity(http.HandlerFunc(handler.CreateAccessRequest)).ServeHTTP(provisionResponse, provisionRequest)
	require.Equal(t, http.StatusCreated, provisionResponse.Code, provisionResponse.Body.String())
	var provision accessprovisioning.ProvisionResult
	require.NoError(t, json.Unmarshal(provisionResponse.Body.Bytes(), &provision))
	require.NotNil(t, provision.Grant)

	issueRequest := httptest.NewRequest(http.MethodPost, "/api/v1/access-grants/"+provision.Grant.GrantRef+"/credentials", bytes.NewBufferString(fmt.Sprintf(`{"name":%q}`, "policy selector "+suffix)))
	issueRequest.Header.Set("Content-Type", "application/json")
	issueRequest.Header.Set(middleware.ClientRequestIDHeader, "policy-selector-key-"+suffix)
	issueRequest.SetPathValue("ref", provision.Grant.GrantRef)
	issueRequest = withIntegrationCommittingActor(t, pool, issueRequest, actorID)
	issueResponse := httptest.NewRecorder()
	middleware.OperationIdentity(http.HandlerFunc(handler.IssueAccessGrantCredential)).ServeHTTP(issueResponse, issueRequest)
	require.Equal(t, http.StatusCreated, issueResponse.Code, issueResponse.Body.String())
	var credential accessprovisioning.Credential
	require.NoError(t, json.Unmarshal(issueResponse.Body.Bytes(), &credential))
	var accessCredentialID int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT id FROM access_credentials WHERE credential_ref=$1`, credential.CredentialRef).Scan(&accessCredentialID))
	return credential.ID, accessCredentialID, credential.CredentialRef
}

func decodePolicySelectorPayload(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	return payload
}

func policyRouteName(name string) string {
	if name == "guardrail" {
		return "guardrail-policies"
	}
	return "limit-policies"
}
