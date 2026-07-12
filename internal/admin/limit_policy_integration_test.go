//go:build integration

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F07LimitPolicyControlPlaneIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)
	_, err := pool.Exec(ctx, `UPDATE api_keys SET policy_labels='production' WHERE id=$1`, fx.keyA)
	require.NoError(t, err)

	redisAddress := os.Getenv("REDIS_ADDR")
	if redisAddress == "" {
		redisAddress = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddress, Password: os.Getenv("REDIS_PASSWORD")})
	t.Cleanup(func() { _ = rdb.Close() })
	require.NoError(t, rdb.Ping(ctx).Err())
	require.NoError(t, rdb.FlushDB(ctx).Err())
	policyService, err := limitpolicy.NewService(pool, rdb, limitpolicy.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, policyService.Refresh(ctx))

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	h.SetLimitPolicyReloader(policyService)
	mux := p6f07LimitPolicyMux(h)
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

	createOwn := serve(t, http.MethodPost, "/api/v1/limit-policies", fmt.Sprintf(`{
		"org_id":%d,"project_id":%d,"api_key_id":%d,"name":"production-chat",
		"model":"company-chat","policy_label":"production","mode":"shadow",
		"failure_mode":"last_known_safe","tpm_limit":1000,"concurrency_limit":3
	}`, fx.orgA, fx.projectA, fx.keyA), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, createOwn, http.StatusCreated)
	var created map[string]interface{}
	require.NoError(t, json.Unmarshal(createOwn.Body.Bytes(), &created))
	policyID := int64(created["id"].(float64))
	require.Equal(t, float64(1), created["version"])
	require.Equal(t, true, created["runtime_snapshot_applied"])
	require.Len(t, policyService.Policies(), 1)

	foreignCreate := serve(t, http.MethodPost, "/api/v1/limit-policies", fmt.Sprintf(`{
		"org_id":%d,"name":"foreign","mode":"enforce","rpm_limit":10
	}`, fx.orgB), fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, foreignCreate, http.StatusForbidden)
	orgBCreate := serve(t, http.MethodPost, "/api/v1/limit-policies", fmt.Sprintf(`{
		"org_id":%d,"name":"org-b-private","mode":"shadow","rpm_limit":10
	}`, fx.orgB), fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin)
	assertStatus(t, orgBCreate, http.StatusCreated)
	var orgBPolicy map[string]interface{}
	require.NoError(t, json.Unmarshal(orgBCreate.Body.Bytes(), &orgBPolicy))
	foreignUpdate := serve(t, http.MethodPut, fmt.Sprintf("/api/v1/limit-policies/%d", int64(orgBPolicy["id"].(float64))), `{
		"version":1,"rpm_limit":20
	}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, foreignUpdate, http.StatusNotFound)
	require.NotContains(t, foreignUpdate.Body.String(), "org-b-private")

	crossKey := serve(t, http.MethodPost, "/api/v1/limit-policies", fmt.Sprintf(`{
		"org_id":%d,"api_key_id":%d,"name":"cross-key","mode":"enforce","rpm_limit":10
	}`, fx.orgA, fx.keyB), fx.platformA, fx.orgA, auth.RolePlatformAdmin)
	assertStatus(t, crossKey, http.StatusBadRequest)

	globalCreate := serve(t, http.MethodPost, "/api/v1/limit-policies", `{
		"name":"platform-global-rps","mode":"enforce","failure_mode":"fail_closed","rps_limit":500
	}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin)
	assertStatus(t, globalCreate, http.StatusCreated)
	var global map[string]interface{}
	require.NoError(t, json.Unmarshal(globalCreate.Body.Bytes(), &global))
	globalID := int64(global["id"].(float64))

	orgAList := serve(t, http.MethodGet, "/api/v1/limit-policies", "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, orgAList, http.StatusOK)
	require.Contains(t, orgAList.Body.String(), "production-chat")
	require.Contains(t, orgAList.Body.String(), "platform-global-rps")
	require.Contains(t, orgAList.Body.String(), `"global_concurrency":100`)
	require.Contains(t, orgAList.Body.String(), `"organization_concurrency":10`)
	require.Contains(t, orgAList.Body.String(), `"workload_stream_weight":4`)
	require.Contains(t, orgAList.Body.String(), `"workload_stream_max_wait_ms":250`)
	require.Contains(t, orgAList.Body.String(), `"workload_organization_short_reserve":2`)
	orgBList := serve(t, http.MethodGet, "/api/v1/limit-policies", "", fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin)
	assertStatus(t, orgBList, http.StatusOK)
	require.NotContains(t, orgBList.Body.String(), "production-chat")
	require.Contains(t, orgBList.Body.String(), "platform-global-rps")

	updateOwn := serve(t, http.MethodPut, fmt.Sprintf("/api/v1/limit-policies/%d", policyID), `{
		"version":1,"mode":"enforce","tpm_limit":2000
	}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, updateOwn, http.StatusOK)
	var updated map[string]interface{}
	require.NoError(t, json.Unmarshal(updateOwn.Body.Bytes(), &updated))
	require.Equal(t, float64(2), updated["version"])
	require.Equal(t, "enforce", updated["mode"])
	require.Equal(t, true, updated["runtime_snapshot_applied"])
	staleUpdate := serve(t, http.MethodPut, fmt.Sprintf("/api/v1/limit-policies/%d", policyID), `{
		"version":1,"tpm_limit":3000
	}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, staleUpdate, http.StatusConflict)
	require.Contains(t, staleUpdate.Body.String(), `"current_version":2`)

	orgCannotMutateGlobal := serve(t, http.MethodPut, fmt.Sprintf("/api/v1/limit-policies/%d", globalID), `{"version":1,"mode":"disabled"}`, fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin)
	assertStatus(t, orgCannotMutateGlobal, http.StatusForbidden)

	var version, tpmLimit int64
	var mode string
	require.NoError(t, pool.QueryRow(ctx, `SELECT version, mode, tpm_limit FROM distributed_limit_policies WHERE id=$1`, policyID).Scan(&version, &mode, &tpmLimit))
	require.Equal(t, int64(2), version)
	require.Equal(t, "enforce", mode)
	require.Equal(t, int64(2000), tpmLimit)
	var auditCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE resource='distributed_limit_policy' AND resource_id=$1`, fmt.Sprintf("%d", policyID)).Scan(&auditCount))
	require.GreaterOrEqual(t, auditCount, 2)
}

func p6f07LimitPolicyMux(handler *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	roleRoute := func(h http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(p6f16JWTSecret)(middleware.RequireRoles(roles...)(h))
	}
	mux.Handle("GET /api/v1/limit-policies", roleRoute(handler.ListLimitPolicies, auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleBillingAdmin))
	mux.Handle("POST /api/v1/limit-policies", roleRoute(handler.CreateLimitPolicy, auth.RolePlatformAdmin, auth.RoleOrgAdmin))
	mux.Handle("PUT /api/v1/limit-policies/{id}", roleRoute(handler.UpdateLimitPolicy, auth.RolePlatformAdmin, auth.RoleOrgAdmin))
	return mux
}
