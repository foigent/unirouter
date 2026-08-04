//go:build integration

package admin

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/invocationopt"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestInvocationOptimizationLifecycleEvidenceGateAndTenantScope(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fixture := seedP6F16Fixture(t, pool)
	manager, err := invocationopt.NewManager(pool)
	require.NoError(t, err)
	handler := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	handler.SetInvocationOptimization(manager, false)

	mux := http.NewServeMux()
	guard := func(next http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(pool, p6f16JWTSecret)(
			middleware.RequireRoles(auth.RolePlatformAdmin, auth.RoleOrgAdmin)(next),
		)
	}
	mux.Handle("POST /api/v1/invocation-optimization/policies", guard(handler.CreateInvocationOptimizationPolicy))
	mux.Handle("GET /api/v1/invocation-optimization/policies/{ref}", guard(handler.GetInvocationOptimizationPolicy))
	mux.Handle("GET /api/v1/invocation-optimization/policies/{ref}/readiness", guard(handler.GetInvocationOptimizationReadiness))
	mux.Handle("POST /api/v1/invocation-optimization/policies/{ref}/validate", guard(handler.ValidateInvocationOptimizationPolicy))
	mux.Handle("POST /api/v1/invocation-optimization/policies/{ref}/shadow", guard(handler.ShadowInvocationOptimizationPolicy))
	mux.Handle("POST /api/v1/invocation-optimization/policies/{ref}/canary", guard(handler.CanaryInvocationOptimizationPolicy))

	serve := func(method, target, body string, userID, orgID int64, role string) *httptest.ResponseRecorder {
		t.Helper()
		token := testutil.MustAdminSessionJWT(t, ctx, pool, userID, orgID, role, p6f16JWTSecret)
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	transition := func(action string, revision int64, orgID, userID int64) *httptest.ResponseRecorder {
		return serve(http.MethodPost,
			"/api/v1/invocation-optimization/policies/opt_company_chat/"+action,
			fmt.Sprintf(`{"expected_revision":%d,"reason":"integration %s"}`, revision, action),
			userID, orgID, auth.RoleOrgAdmin)
	}

	created := serve(http.MethodPost, "/api/v1/invocation-optimization/policies", fmt.Sprintf(`{
		"org_id":%d,"project_id":%d,"policy_ref":"opt_company_chat",
		"display_name":"Company Chat 优化","model_selector":"company-chat",
		"response_cache_target":"canary","response_cache_canary_basis_points":100,
		"response_cache_ttl_seconds":300,"response_cache_persistence_allowed":true,
		"response_cache_require_request_opt_in":false,
		"compression_target":"canary","compression_canary_basis_points":100,
		"compression_require_request_opt_in":false,
		"minimum_shadow_samples":10
	}`, fixture.orgA, fixture.projectA), fixture.orgAdminA, fixture.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	require.Contains(t, created.Body.String(), `"response_cache_require_request_opt_in":true`)
	require.Contains(t, created.Body.String(), `"compression_require_request_opt_in":true`)

	foreign := serve(http.MethodGet, "/api/v1/invocation-optimization/policies/opt_company_chat", "", fixture.orgAdminB, fixture.orgB, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())

	disabledDraft := serve(http.MethodPost, "/api/v1/invocation-optimization/policies", fmt.Sprintf(`{
		"org_id":%d,"policy_ref":"opt_disabled_drivers",
		"display_name":"Disabled drivers"
	}`, fixture.orgA), fixture.orgAdminA, fixture.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusCreated, disabledDraft.Code, disabledDraft.Body.String())
	disabledValidate := serve(http.MethodPost,
		"/api/v1/invocation-optimization/policies/opt_disabled_drivers/validate",
		`{"expected_revision":1,"reason":"must select a driver"}`,
		fixture.orgAdminA, fixture.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusConflict, disabledValidate.Code, disabledValidate.Body.String())
	var disabledRevision int64
	var disabledStage string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT revision,stage FROM invocation_optimization_policies
		WHERE org_id=$1 AND policy_ref='opt_disabled_drivers'`, fixture.orgA,
	).Scan(&disabledRevision, &disabledStage))
	require.EqualValues(t, 1, disabledRevision)
	require.Equal(t, invocationopt.StageDraft, disabledStage)

	missingReason := serve(http.MethodPost,
		"/api/v1/invocation-optimization/policies/opt_company_chat/validate",
		`{"expected_revision":1,"reason":"   "}`,
		fixture.orgAdminA, fixture.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusBadRequest, missingReason.Code, missingReason.Body.String())
	var revisionAfterRejectedTransition int64
	var stageAfterRejectedTransition string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT revision,stage FROM invocation_optimization_policies
		WHERE org_id=$1 AND policy_ref='opt_company_chat'`, fixture.orgA,
	).Scan(&revisionAfterRejectedTransition, &stageAfterRejectedTransition))
	require.EqualValues(t, 1, revisionAfterRejectedTransition)
	require.Equal(t, invocationopt.StageDraft, stageAfterRejectedTransition)

	validated := transition("validate", 1, fixture.orgA, fixture.orgAdminA)
	require.Equal(t, http.StatusOK, validated.Code, validated.Body.String())
	require.Contains(t, validated.Body.String(), `"stage":"validated"`)
	shadow := transition("shadow", 2, fixture.orgA, fixture.orgAdminA)
	require.Equal(t, http.StatusOK, shadow.Code, shadow.Body.String())
	require.Contains(t, shadow.Body.String(), `"stage":"shadow"`)

	runtimeBlockedReadiness := serve(http.MethodGet,
		"/api/v1/invocation-optimization/policies/opt_company_chat/readiness", "",
		fixture.orgAdminA, fixture.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusOK, runtimeBlockedReadiness.Code, runtimeBlockedReadiness.Body.String())
	require.Contains(t, runtimeBlockedReadiness.Body.String(), `"contract_version":"`+invocationopt.ReadinessContractVersion+`"`)
	require.Contains(t, runtimeBlockedReadiness.Body.String(), `"canary_admission_ready":false`)
	require.Contains(t, runtimeBlockedReadiness.Body.String(), `"code":"response_cache_runtime_unavailable"`)
	require.Contains(t, runtimeBlockedReadiness.Body.String(), `"grants_activation_authority":false`)

	foreignReadiness := serve(http.MethodGet,
		"/api/v1/invocation-optimization/policies/opt_company_chat/readiness", "",
		fixture.orgAdminB, fixture.orgB, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusNotFound, foreignReadiness.Code, foreignReadiness.Body.String())

	cacheUnavailable := transition("canary", 3, fixture.orgA, fixture.orgAdminA)
	require.Equal(t, http.StatusServiceUnavailable, cacheUnavailable.Code, cacheUnavailable.Body.String())
	require.Contains(t, cacheUnavailable.Body.String(), "response-cache key backend is unavailable")

	handler.SetInvocationOptimization(manager, true)
	insufficientEvidence := transition("canary", 3, fixture.orgA, fixture.orgAdminA)
	require.Equal(t, http.StatusConflict, insufficientEvidence.Code, insufficientEvidence.Body.String())
	insufficientReadiness := serve(http.MethodGet,
		"/api/v1/invocation-optimization/policies/opt_company_chat/readiness", "",
		fixture.orgAdminA, fixture.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusOK, insufficientReadiness.Code, insufficientReadiness.Body.String())
	require.Contains(t, insufficientReadiness.Body.String(), `"code":"shadow_samples_insufficient"`)

	for index := 1; index <= 10; index++ {
		requestID := fmt.Sprintf("invopt-shadow-%02d", index)
		_, err = pool.Exec(ctx, `
			INSERT INTO freeze_idempotency (
			  request_id,org_id,project_id,frozen_amount_micros,reserved_balance_micros,status)
			VALUES ($1,$2,$3,1,1,'frozen')`, requestID, fixture.orgA, fixture.projectA)
		require.NoError(t, err)
		for _, driver := range []string{invocationopt.DriverResponseCache, invocationopt.DriverCompression} {
			_, err = pool.Exec(ctx, `
				INSERT INTO invocation_optimization_receipts (
				  contract_version,org_id,project_id,request_id,requested_model,resolved_model,
				  driver,policy_ref,policy_revision,stage,effective_mode,decision,reason,
				  assigned,eligible,mutation_applied,safety_passed,quality_passed)
				VALUES ($1,$2,$3,$4,'company-chat','company-chat',$5,'opt_company_chat',3,
				  'shadow','shadow','shadow_observed','eligible',false,true,false,true,true)`,
				invocationopt.ReceiptContractVersion, fixture.orgA, fixture.projectA, requestID, driver)
			require.NoError(t, err)
		}
	}
	ready := serve(http.MethodGet,
		"/api/v1/invocation-optimization/policies/opt_company_chat/readiness", "",
		fixture.orgAdminA, fixture.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusOK, ready.Code, ready.Body.String())
	require.Contains(t, ready.Body.String(), `"overall":"ready"`)
	require.Contains(t, ready.Body.String(), `"canary_admission_ready":true`)
	require.Contains(t, ready.Body.String(), `"blockers":[]`)

	canary := transition("canary", 3, fixture.orgA, fixture.orgAdminA)
	require.Equal(t, http.StatusOK, canary.Code, canary.Body.String())
	require.Contains(t, canary.Body.String(), `"stage":"canary"`)
	require.Contains(t, canary.Body.String(), `"revision":4`)
	activeReadiness := serve(http.MethodGet,
		"/api/v1/invocation-optimization/policies/opt_company_chat/readiness", "",
		fixture.orgAdminA, fixture.orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusOK, activeReadiness.Code, activeReadiness.Body.String())
	require.Contains(t, activeReadiness.Body.String(), `"overall":"not_applicable"`)

	var revisions, audits int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM invocation_optimization_policy_revisions
		WHERE org_id=$1 AND policy_ref='opt_company_chat'`, fixture.orgA).Scan(&revisions))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_logs
		WHERE org_id=$1 AND resource='invocation_optimization_policy'
		  AND resource_id='opt_company_chat'`, fixture.orgA).Scan(&audits))
	require.Equal(t, 4, revisions)
	require.Equal(t, 4, audits)
}
