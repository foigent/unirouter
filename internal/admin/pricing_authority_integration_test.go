package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F34PriceAuthorityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)

	var baseID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
		 VALUES ('p6f34-main', 10, 50) RETURNING id`,
	).Scan(&baseID))
	_, err := pool.Exec(ctx,
		`INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
		 VALUES ('p6f34-org-create', 4, 8)`,
	)
	require.NoError(t, err)
	var multiplierAID, multiplierBID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO org_model_prices (org_id, model_name, multiplier)
		 VALUES ($1, 'p6f34-main', 1.5) RETURNING id`, fx.orgA,
	).Scan(&multiplierAID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO org_model_prices (org_id, model_name, multiplier)
		 VALUES ($1, 'p6f34-main', 2.0) RETURNING id`, fx.orgB,
	).Scan(&multiplierBID))

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	mux := p6f34PricingMux(h)
	serve := func(tb testing.TB, method, target, body string, userID, orgID int64, role, requestID string) *httptest.ResponseRecorder {
		tb.Helper()
		token, tokenErr := auth.GenerateJWT(userID, orgID, role, p6f16JWTSecret, 1)
		require.NoError(tb, tokenErr)
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if requestID != "" {
			req = req.WithContext(context.WithValue(req.Context(), middleware.CtxRequestID, requestID))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	assertStatus := func(tb testing.TB, rec *httptest.ResponseRecorder, want int) {
		tb.Helper()
		if rec.Code != want {
			tb.Fatalf("expected status %d, got %d: %s", want, rec.Code, rec.Body.String())
		}
	}

	t.Run("organization reads default to JWT organization and reject foreign scope", func(t *testing.T) {
		global := serve(t, http.MethodGet, "/api/v1/model-prices", "", fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin, "")
		assertStatus(t, global, http.StatusOK)
		require.Contains(t, global.Body.String(), "p6f34-main")
		require.Contains(t, global.Body.String(), `"can_mutate":false`)

		for _, target := range []string{
			"/api/v1/org-model-prices",
			fmt.Sprintf("/api/v1/org-model-prices?org_id=%d", fx.orgB),
		} {
			rec := serve(t, http.MethodGet, target, "", fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin, "")
			assertStatus(t, rec, http.StatusOK)
			assertP6F34OrgPriceResponse(t, rec, fx.orgB, multiplierBID)
		}
		assertStatus(t, serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/org-model-prices?org_id=%d", fx.orgA), "",
			fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin, ""), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodGet, "/api/v1/org-model-prices?org_id=invalid", "",
			fx.orgAdminB, fx.orgB, auth.RoleOrgAdmin, ""), http.StatusBadRequest)
		assertStatus(t, serve(t, http.MethodGet, "/api/v1/org-model-prices", "",
			fx.orgAdminB, 0, auth.RoleOrgAdmin, ""), http.StatusForbidden)

		own := serve(t, http.MethodGet, "/api/v1/org-model-prices", "", fx.platformA, fx.orgA, auth.RolePlatformAdmin, "")
		assertStatus(t, own, http.StatusOK)
		assertP6F34OrgPriceResponse(t, own, fx.orgA, multiplierAID)
		foreign := serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/org-model-prices?org_id=%d", fx.orgB), "",
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "")
		assertStatus(t, foreign, http.StatusOK)
		assertP6F34OrgPriceResponse(t, foreign, fx.orgB, multiplierBID)
		require.Contains(t, foreign.Body.String(), `"can_mutate":true`)
	})

	t.Run("all tenant roles are denied before parsing or side effects", func(t *testing.T) {
		bill := billing.NewBilling(pool, nil)
		before := readP6F34Snapshot(t, pool, bill, fx.orgA, fx.orgB)
		paths := []struct {
			method, target, body string
		}{
			{http.MethodPost, "/api/v1/model-prices", `{"model_name":"p6f34-main","input_price_cents":999,"output_price_cents":999}`},
			{http.MethodPut, fmt.Sprintf("/api/v1/model-prices/%d", baseID), `{"input_price_cents":999}`},
			{http.MethodDelete, fmt.Sprintf("/api/v1/model-prices/%d", baseID), ""},
			{http.MethodPost, "/api/v1/org-model-prices", fmt.Sprintf(`{"org_id":%d,"model_name":"p6f34-main","multiplier":9}`, fx.orgA)},
			{http.MethodPost, "/api/v1/org-model-prices", fmt.Sprintf(`{"org_id":%d,"model_name":"p6f34-main","multiplier":9}`, fx.orgB)},
			{http.MethodPut, fmt.Sprintf("/api/v1/org-model-prices/%d", multiplierBID), `{"multiplier":9}`},
			{http.MethodDelete, fmt.Sprintf("/api/v1/org-model-prices/%d", multiplierBID), ""},
		}
		roles := []string{auth.RoleOrgAdmin, auth.RoleBillingAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer}
		for _, role := range roles {
			for _, path := range paths {
				assertStatus(t, serve(t, path.method, path.target, path.body, fx.orgAdminA, fx.orgA, role, "p6f34-denied"), http.StatusForbidden)
			}
		}
		assertStatus(t, serve(t, http.MethodPost, "/api/v1/model-prices", "not-json",
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f34-denied-invalid"), http.StatusForbidden)
		require.Equal(t, before, readP6F34Snapshot(t, pool, bill, fx.orgA, fx.orgB))
	})

	t.Run("platform mutations audit atomically and preserve billing math", func(t *testing.T) {
		requestIDs := []string{
			"p6f34-model-upsert", "p6f34-model-update", "p6f34-model-delete",
			"p6f34-org-upsert", "p6f34-org-update", "p6f34-org-delete",
		}
		createdModel := serve(t, http.MethodPost, "/api/v1/model-prices",
			`{"model_name":"p6f34-created","input_price_cents":3,"output_price_cents":4}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[0])
		assertStatus(t, createdModel, http.StatusCreated)
		createdModelID := p6f34ResponseID(t, createdModel)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/model-prices/%d", baseID),
			`{"input_price_cents":12,"output_price_cents":60}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[1]), http.StatusOK)
		assertStatus(t, serve(t, http.MethodDelete, fmt.Sprintf("/api/v1/model-prices/%d", createdModelID), "",
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[2]), http.StatusOK)

		createdOrg := serve(t, http.MethodPost, "/api/v1/org-model-prices",
			fmt.Sprintf(`{"org_id":%d,"model_name":"p6f34-org-create","multiplier":1.25}`, fx.orgB),
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[3])
		assertStatus(t, createdOrg, http.StatusCreated)
		createdOrgID := p6f34ResponseID(t, createdOrg)
		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/org-model-prices/%d", multiplierBID),
			`{"multiplier":2.5}`, fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[4]), http.StatusOK)
		assertStatus(t, serve(t, http.MethodDelete, fmt.Sprintf("/api/v1/org-model-prices/%d", createdOrgID), "",
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[5]), http.StatusOK)

		bill := billing.NewBilling(pool, nil)
		inputA, outputA, err := bill.QueryModelPriceForOrg(ctx, fx.orgA, "p6f34-main")
		require.NoError(t, err)
		require.InDelta(t, 18, inputA, 0.000001)
		require.InDelta(t, 90, outputA, 0.000001)
		inputB, outputB, err := bill.QueryModelPriceForOrg(ctx, fx.orgB, "p6f34-main")
		require.NoError(t, err)
		require.InDelta(t, 30, inputB, 0.000001)
		require.InDelta(t, 150, outputB, 0.000001)

		var auditCount int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs WHERE request_id = ANY($1::text[])`, requestIDs,
		).Scan(&auditCount))
		require.Equal(t, 6, auditCount)
		var globalOrgID, orgTargetID, actorID int64
		var globalBefore, globalAfter string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT org_id, user_id, before_state::text, after_state::text
			 FROM audit_logs WHERE request_id = 'p6f34-model-update'`,
		).Scan(&globalOrgID, &actorID, &globalBefore, &globalAfter))
		require.Equal(t, fx.orgA, globalOrgID)
		require.Equal(t, fx.platformA, actorID)
		require.NotEqual(t, `{}`, globalBefore)
		require.NotEqual(t, `{}`, globalAfter)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT org_id FROM audit_logs WHERE request_id = 'p6f34-org-update'`,
		).Scan(&orgTargetID))
		require.Equal(t, fx.orgB, orgTargetID)
	})

	t.Run("concurrent first upserts serialize before-state capture", func(t *testing.T) {
		type concurrentWrite struct {
			requestID string
			body      string
		}
		runPair := func(target string, writes []concurrentWrite) {
			t.Helper()
			start := make(chan struct{})
			responses := make(chan *httptest.ResponseRecorder, len(writes))
			for _, write := range writes {
				write := write
				go func() {
					<-start
					responses <- serve(t, http.MethodPost, target, write.body,
						fx.platformA, fx.orgA, auth.RolePlatformAdmin, write.requestID)
				}()
			}
			close(start)
			for range writes {
				assertStatus(t, <-responses, http.StatusCreated)
			}
			requestIDs := []string{writes[0].requestID, writes[1].requestID}
			var auditCount, auditsWithBefore int
			require.NoError(t, pool.QueryRow(ctx,
				`SELECT COUNT(*), COUNT(*) FILTER (WHERE before_state <> '{}'::jsonb)
				 FROM audit_logs WHERE request_id = ANY($1::text[])`, requestIDs,
			).Scan(&auditCount, &auditsWithBefore))
			require.Equal(t, 2, auditCount)
			require.Equal(t, 1, auditsWithBefore)
		}

		runPair("/api/v1/model-prices", []concurrentWrite{
			{requestID: "p6f34-concurrent-model-a", body: `{"model_name":"p6f34-concurrent","input_price_cents":7,"output_price_cents":14}`},
			{requestID: "p6f34-concurrent-model-b", body: `{"model_name":"p6f34-concurrent","input_price_cents":8,"output_price_cents":16}`},
		})
		runPair("/api/v1/org-model-prices", []concurrentWrite{
			{requestID: "p6f34-concurrent-org-a", body: fmt.Sprintf(`{"org_id":%d,"model_name":"p6f34-concurrent-org","multiplier":1.1}`, fx.orgB)},
			{requestID: "p6f34-concurrent-org-b", body: fmt.Sprintf(`{"org_id":%d,"model_name":"p6f34-concurrent-org","multiplier":1.2}`, fx.orgB)},
		})
	})

	t.Run("missing and invalid authentication are rejected", func(t *testing.T) {
		for _, authorization := range []string{"", "Bearer invalid"} {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/model-prices", bytes.NewBufferString("not-json"))
			if authorization != "" {
				req.Header.Set("Authorization", authorization)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
		}
	})
}

func p6f34PricingMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	roleRoute := func(handler http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(p6f16JWTSecret)(middleware.RequireRoles(roles...)(handler))
	}
	readRoles := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}
	mux.Handle("GET /api/v1/model-prices", roleRoute(h.ListModelPrices, readRoles...))
	mux.Handle("POST /api/v1/model-prices", roleRoute(h.CreateModelPrice, auth.RolePlatformAdmin))
	mux.Handle("PUT /api/v1/model-prices/{id}", roleRoute(h.UpdateModelPrice, auth.RolePlatformAdmin))
	mux.Handle("DELETE /api/v1/model-prices/{id}", roleRoute(h.DeleteModelPrice, auth.RolePlatformAdmin))
	mux.Handle("GET /api/v1/org-model-prices", roleRoute(h.ListOrgModelPrices, readRoles...))
	mux.Handle("POST /api/v1/org-model-prices", roleRoute(h.CreateOrgModelPrice, auth.RolePlatformAdmin))
	mux.Handle("PUT /api/v1/org-model-prices/{id}", roleRoute(h.UpdateOrgModelPrice, auth.RolePlatformAdmin))
	mux.Handle("DELETE /api/v1/org-model-prices/{id}", roleRoute(h.DeleteOrgModelPrice, auth.RolePlatformAdmin))
	return mux
}

type p6f34Snapshot struct {
	ModelCount, OrgPriceCount, AuditCount int
	BaseInput, BaseOutput                 float64
	MultiplierA, MultiplierB              float64
	EffectiveInputA, EffectiveOutputA     float64
	EffectiveInputB, EffectiveOutputB     float64
}

func readP6F34Snapshot(t testing.TB, pool *pgxpool.Pool, bill *billing.Billing, orgA, orgB int64) p6f34Snapshot {
	t.Helper()
	ctx := context.Background()
	var got p6f34Snapshot
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_prices`).Scan(&got.ModelCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_model_prices`).Scan(&got.OrgPriceCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&got.AuditCount))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT input_price_cents::float8, output_price_cents::float8 FROM model_prices WHERE model_name = 'p6f34-main'`,
	).Scan(&got.BaseInput, &got.BaseOutput))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT multiplier::float8 FROM org_model_prices WHERE org_id = $1 AND model_name = 'p6f34-main'`, orgA,
	).Scan(&got.MultiplierA))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT multiplier::float8 FROM org_model_prices WHERE org_id = $1 AND model_name = 'p6f34-main'`, orgB,
	).Scan(&got.MultiplierB))
	var err error
	got.EffectiveInputA, got.EffectiveOutputA, err = bill.QueryModelPriceForOrg(ctx, orgA, "p6f34-main")
	require.NoError(t, err)
	got.EffectiveInputB, got.EffectiveOutputB, err = bill.QueryModelPriceForOrg(ctx, orgB, "p6f34-main")
	require.NoError(t, err)
	return got
}

func assertP6F34OrgPriceResponse(t testing.TB, rec *httptest.ResponseRecorder, wantOrgID, wantRowID int64) {
	t.Helper()
	var response struct {
		OrgID int64 `json:"org_id"`
		Data  []struct {
			ID    int64 `json:"id"`
			OrgID int64 `json:"org_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, wantOrgID, response.OrgID)
	require.Len(t, response.Data, 1)
	require.Equal(t, wantRowID, response.Data[0].ID)
	require.Equal(t, wantOrgID, response.Data[0].OrgID)
}

func p6f34ResponseID(t testing.TB, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	var response struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Positive(t, response.ID)
	return response.ID
}
