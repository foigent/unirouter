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

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F10GuardrailControlPlaneTenantAuthorityAndVersioning(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)
	service, err := guardrail.NewService(pool, nil, []byte("0123456789abcdef0123456789abcdef"), guardrail.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, service.Refresh(ctx))
	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	h.SetGuardrailController(service)
	mux := p6f10GuardrailPolicyMux(h)

	serve := func(tb testing.TB, method, target, body string, userID, orgID int64, role string) *httptest.ResponseRecorder {
		tb.Helper()
		token, tokenErr := auth.GenerateJWT(userID, orgID, role, p6f16JWTSecret, 1)
		require.NoError(tb, tokenErr)
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	assertStatus := func(tb testing.TB, response *httptest.ResponseRecorder, want int) {
		tb.Helper()
		if response.Code != want {
			tb.Fatalf("expected %d, got %d: %s", want, response.Code, response.Body.String())
		}
	}

	createOwn := serve(t, http.MethodPost, "/api/v1/guardrail-policies", fmt.Sprintf(`{
		"org_id":%d,"project_id":%d,"api_key_id":%d,"name":"redact-email",
		"model":"company-chat","stage":"input","detector_ref":"builtin:pii_secrets",
		"action":"redact","mode":"shadow","failure_mode":"degraded_allow",
		"config":{"entities":["email"]}
	}`, fx.orgA, fx.projectA, fx.keyA), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, createOwn, http.StatusCreated)
	var created map[string]interface{}
	require.NoError(t, json.Unmarshal(createOwn.Body.Bytes(), &created))
	policyID := int64(created["id"].(float64))
	require.Equal(t, true, created["runtime_snapshot_applied"])

	foreignCreate := serve(t, http.MethodPost, "/api/v1/guardrail-policies", fmt.Sprintf(`{
		"org_id":%d,"name":"foreign","stage":"input","detector_ref":"builtin:pii_secrets",
		"action":"block","mode":"enforce","failure_mode":"fail_closed","config":{}
	}`, fx.orgB), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, foreignCreate, http.StatusForbidden)

	orgMandatory := serve(t, http.MethodPost, "/api/v1/guardrail-policies", `{
		"name":"tenant-mandatory","stage":"input","detector_ref":"builtin:pii_secrets",
		"action":"block","mode":"enforce","failure_mode":"fail_closed","mandatory":true,"config":{}
	}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, orgMandatory, http.StatusForbidden)

	unapprovedExternal := serve(t, http.MethodPost, "/api/v1/guardrail-policies", `{
		"name":"arbitrary-url","stage":"input","detector_ref":"external:https://tenant.example",
		"action":"audit","mode":"shadow","failure_mode":"degraded_allow","config":{}
	}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin)
	assertStatus(t, unapprovedExternal, http.StatusBadRequest)
	trailingJSON := serve(t, http.MethodPost, "/api/v1/guardrail-policies", `{
		"name":"ambiguous","stage":"input","detector_ref":"builtin:pii_secrets",
		"action":"audit","mode":"shadow","failure_mode":"degraded_allow","config":{}
	}{"mode":"enforce"}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin)
	assertStatus(t, trailingJSON, http.StatusBadRequest)

	globalCreate := serve(t, http.MethodPost, "/api/v1/guardrail-policies", `{
		"name":"platform-secret-block","stage":"input","detector_ref":"builtin:pii_secrets",
		"action":"block","mode":"enforce","failure_mode":"fail_closed","mandatory":true,
		"config":{"entities":["secret"]}
	}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin)
	assertStatus(t, globalCreate, http.StatusCreated)
	var global map[string]interface{}
	require.NoError(t, json.Unmarshal(globalCreate.Body.Bytes(), &global))
	globalID := int64(global["id"].(float64))

	orgBCreate := serve(t, http.MethodPost, "/api/v1/guardrail-policies", fmt.Sprintf(`{
		"org_id":%d,"name":"org-b-audit","stage":"input","detector_ref":"builtin:pii_secrets",
		"action":"audit","mode":"shadow","failure_mode":"degraded_allow","config":{"entities":["email"]}
	}`, fx.orgB), fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin)
	assertStatus(t, orgBCreate, http.StatusCreated)
	var orgB map[string]interface{}
	require.NoError(t, json.Unmarshal(orgBCreate.Body.Bytes(), &orgB))

	orgAList := serve(t, http.MethodGet, "/api/v1/guardrail-policies", "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, orgAList, http.StatusOK)
	require.Contains(t, orgAList.Body.String(), "redact-email")
	require.Contains(t, orgAList.Body.String(), "platform-secret-block")
	require.NotContains(t, orgAList.Body.String(), "org-b-audit")
	require.Contains(t, orgAList.Body.String(), `"automatic_detector_retry":false`)
	require.Contains(t, orgAList.Body.String(), `"local_detector_max_bytes":131072`)
	require.Contains(t, orgAList.Body.String(), `"external_detector_max_bytes":262144`)
	require.Contains(t, orgAList.Body.String(), `"detector_max_texts":256`)
	var orgAListPayload struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(orgAList.Body.Bytes(), &orgAListPayload))
	for _, policy := range orgAListPayload.Data {
		if policy["name"] == "platform-secret-block" {
			require.Equal(t, true, policy["config_redacted"])
			require.Empty(t, policy["config"].(map[string]interface{}))
		}
	}

	foreignUpdate := serve(t, http.MethodPut, fmt.Sprintf("/api/v1/guardrail-policies/%d", int64(orgB["id"].(float64))), `{"version":1,"mode":"disabled"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, foreignUpdate, http.StatusNotFound)
	require.NotContains(t, foreignUpdate.Body.String(), "org-b-audit")

	updateOwn := serve(t, http.MethodPut, fmt.Sprintf("/api/v1/guardrail-policies/%d", policyID), `{"version":1,"mode":"enforce","action":"redact"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, updateOwn, http.StatusOK)
	var updated map[string]interface{}
	require.NoError(t, json.Unmarshal(updateOwn.Body.Bytes(), &updated))
	require.Equal(t, float64(2), updated["version"])
	require.Equal(t, "enforce", updated["mode"])
	stale := serve(t, http.MethodPut, fmt.Sprintf("/api/v1/guardrail-policies/%d", policyID), `{"version":1,"mode":"disabled"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, stale, http.StatusConflict)
	orgCannotMutateGlobal := serve(t, http.MethodPut, fmt.Sprintf("/api/v1/guardrail-policies/%d", globalID), `{"version":1,"mode":"disabled"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, orgCannotMutateGlobal, http.StatusForbidden)

	result := service.Pin(guardrail.Subject{OrgID: fx.orgA, ProjectID: fx.projectA, APIKeyID: fx.keyA, Model: "company-chat", Endpoint: "chat"}).Evaluate(t.Context(), guardrail.StageInput, []guardrail.Text{{Content: "token_abcdefghijkl"}})
	require.Equal(t, guardrail.DecisionBlock, result.Decision, "platform mandatory rule must remain active beside tenant redaction")

	var auditCount int
	var latestAuditDetail string
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE resource='guardrail_policy' AND resource_id=$1`, fmt.Sprintf("%d", policyID)).Scan(&auditCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT detail::text FROM audit_logs WHERE resource='guardrail_policy' AND resource_id=$1 ORDER BY id DESC LIMIT 1`, fmt.Sprintf("%d", policyID)).Scan(&latestAuditDetail))
	require.GreaterOrEqual(t, auditCount, 2)
	require.NotContains(t, latestAuditDetail, `"entities"`)
	require.Contains(t, latestAuditDetail, `"config_sha256"`)
}

func p6f10GuardrailPolicyMux(handler *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	roleRoute := func(h http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(p6f16JWTSecret)(middleware.RequireRoles(roles...)(h))
	}
	mux.Handle("GET /api/v1/guardrail-policies", roleRoute(handler.ListGuardrailPolicies, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin))
	mux.Handle("POST /api/v1/guardrail-policies", roleRoute(handler.CreateGuardrailPolicy, auth.RolePlatformAdmin, auth.RoleOrgAdmin))
	mux.Handle("PUT /api/v1/guardrail-policies/{id}", roleRoute(handler.UpdateGuardrailPolicy, auth.RolePlatformAdmin, auth.RoleOrgAdmin))
	return mux
}
