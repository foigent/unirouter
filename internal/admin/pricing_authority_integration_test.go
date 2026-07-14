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
			{http.MethodPost, "/api/v1/model-prices", `{"model_name":"p6f34-main","input_price_cny_per_million_tokens":9990,"output_price_cny_per_million_tokens":9990}`},
			{http.MethodPut, fmt.Sprintf("/api/v1/model-prices/%d", baseID), `{"input_price_cny_per_million_tokens":9990}`},
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

	t.Run("model name is immutable without partial write or audit", func(t *testing.T) {
		const requestID = "v1-model-price-rename-rejected"
		rec := serve(t, http.MethodPut, fmt.Sprintf("/api/v1/model-prices/%d", baseID),
			`{"model_name":"p6f34-renamed","input_price_cny_per_million_tokens":999}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestID)
		assertStatus(t, rec, http.StatusBadRequest)
		require.Contains(t, rec.Body.String(), "model_name is immutable")

		var modelName, inputPrice string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT model_name, input_price_cents::text FROM model_prices WHERE id = $1`, baseID,
		).Scan(&modelName, &inputPrice))
		require.Equal(t, "p6f34-main", modelName)
		require.Equal(t, "10.000000", inputPrice)

		var renamedRows, originalMultipliers, auditCount int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM model_prices WHERE model_name = 'p6f34-renamed'`,
		).Scan(&renamedRows))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM org_model_prices WHERE model_name = 'p6f34-main'`,
		).Scan(&originalMultipliers))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs WHERE request_id = $1`, requestID,
		).Scan(&auditCount))
		require.Zero(t, renamedRows)
		require.Equal(t, 2, originalMultipliers)
		require.Zero(t, auditCount)

		_, _, err := billing.NewBilling(pool, nil).QueryModelPriceMicrosForOrg(ctx, fx.orgA, "p6f34-main")
		require.NoError(t, err)
	})

	t.Run("platform mutations audit atomically and preserve billing math", func(t *testing.T) {
		requestIDs := []string{
			"p6f34-model-upsert", "p6f34-model-update", "p6f34-model-delete",
			"p6f34-org-upsert", "p6f34-org-update", "p6f34-org-delete",
		}
		createdModel := serve(t, http.MethodPost, "/api/v1/model-prices",
			`{"model_name":"p6f34-created","input_price_cny_per_million_tokens":30,"output_price_cny_per_million_tokens":40}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[0])
		assertStatus(t, createdModel, http.StatusCreated)
		createdModelID := p6f34ResponseID(t, createdModel)

		assertStatus(t, serve(t, http.MethodPut, fmt.Sprintf("/api/v1/model-prices/%d", baseID),
			`{"input_price_cny_per_million_tokens":120,"output_price_cny_per_million_tokens":600}`,
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
		inputA, outputA, err := bill.QueryModelPriceMicrosForOrg(ctx, fx.orgA, "p6f34-main")
		require.NoError(t, err)
		require.EqualValues(t, 180_000_000, inputA)
		require.EqualValues(t, 900_000_000, outputA)
		inputB, outputB, err := bill.QueryModelPriceMicrosForOrg(ctx, fx.orgB, "p6f34-main")
		require.NoError(t, err)
		require.EqualValues(t, 300_000_000, inputB)
		require.EqualValues(t, 1_500_000_000, outputB)

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

	t.Run("legacy price upsert preserves omitted cache ratios", func(t *testing.T) {
		const model = "p6f09-preserve-cache-ratios"
		_, err := pool.Exec(ctx,
			`INSERT INTO model_prices (
				model_name, input_price_cents, output_price_cents,
				cache_read_ratio, cache_creation_ratio
			 ) VALUES ($1, 4, 20, 0.2, 1.4)`, model)
		require.NoError(t, err)

		legacy := serve(t, http.MethodPost, "/api/v1/model-prices",
			fmt.Sprintf(`{"model_name":%q,"input_price_cny_per_million_tokens":60,"output_price_cny_per_million_tokens":300}`, model),
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f09-legacy-price-upsert")
		assertStatus(t, legacy, http.StatusCreated)

		var inputPrice, outputPrice, readRatio, creationRatio string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT input_price_cents::text, output_price_cents::text,
			        cache_read_ratio::text, cache_creation_ratio::text
			 FROM model_prices WHERE model_name = $1`, model,
		).Scan(&inputPrice, &outputPrice, &readRatio, &creationRatio))
		require.Equal(t, "6.000000", inputPrice)
		require.Equal(t, "30.000000", outputPrice)
		require.Equal(t, "0.200000", readRatio)
		require.Equal(t, "1.400000", creationRatio)

		explicit := serve(t, http.MethodPost, "/api/v1/model-prices",
			fmt.Sprintf(`{"model_name":%q,"input_price_cny_per_million_tokens":"60.00000","output_price_cny_per_million_tokens":"300.00000","cache_read_ratio":"0.100000","cache_creation_ratio":"1.250000"}`, model),
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f09-explicit-price-upsert")
		assertStatus(t, explicit, http.StatusCreated)
		var exactResponse struct {
			InputPrice         string `json:"input_price_cny_per_million_tokens"`
			OutputPrice        string `json:"output_price_cny_per_million_tokens"`
			CacheReadRatio     string `json:"cache_read_ratio"`
			CacheCreationRatio string `json:"cache_creation_ratio"`
		}
		require.NoError(t, json.Unmarshal(explicit.Body.Bytes(), &exactResponse))
		require.Equal(t, "60", exactResponse.InputPrice)
		require.Equal(t, "300", exactResponse.OutputPrice)
		require.Equal(t, "0.1", exactResponse.CacheReadRatio)
		require.Equal(t, "1.25", exactResponse.CacheCreationRatio)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT cache_read_ratio::text, cache_creation_ratio::text
			 FROM model_prices WHERE model_name = $1`, model,
		).Scan(&readRatio, &creationRatio))
		require.Equal(t, "0.100000", readRatio)
		require.Equal(t, "1.250000", creationRatio)
	})

	t.Run("effective price overflow is rejected atomically on every mutation path", func(t *testing.T) {
		const maxPublicPrice = "9223372036854.7758"
		requestIDs := []string{
			"v1-overflow-model-create", "v1-overflow-model-update",
			"v1-overflow-org-create", "v1-overflow-org-update",
		}

		_, err := pool.Exec(ctx, `
			INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
			VALUES
				('v1-overflow-model-create', 1, 1),
				('v1-overflow-model-update', 1, 1),
				('v1-overflow-org-create', 10000000000, 1),
				('v1-overflow-org-update', 10000000000, 1)
		`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `
			INSERT INTO org_model_prices (org_id, model_name, multiplier)
			VALUES
				($1, 'v1-overflow-model-create', 2),
				($1, 'v1-overflow-model-update', 2)
		`, fx.orgA)
		require.NoError(t, err)

		var updateModelID, updateOrgID int64
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT id FROM model_prices WHERE model_name = 'v1-overflow-model-update'`,
		).Scan(&updateModelID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO org_model_prices (org_id, model_name, multiplier)
			VALUES ($1, 'v1-overflow-org-update', 1) RETURNING id
		`, fx.orgB).Scan(&updateOrgID))

		responses := []*httptest.ResponseRecorder{
			serve(t, http.MethodPost, "/api/v1/model-prices",
				fmt.Sprintf(`{"model_name":"v1-overflow-model-create","input_price_cny_per_million_tokens":%q,"output_price_cny_per_million_tokens":1}`, maxPublicPrice),
				fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[0]),
			serve(t, http.MethodPut, fmt.Sprintf("/api/v1/model-prices/%d", updateModelID),
				fmt.Sprintf(`{"input_price_cny_per_million_tokens":%q}`, maxPublicPrice),
				fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[1]),
			serve(t, http.MethodPost, "/api/v1/org-model-prices",
				fmt.Sprintf(`{"org_id":%d,"model_name":"v1-overflow-org-create","multiplier":99.999}`, fx.orgB),
				fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[2]),
			serve(t, http.MethodPut, fmt.Sprintf("/api/v1/org-model-prices/%d", updateOrgID),
				`{"multiplier":99.999}`,
				fx.platformA, fx.orgA, auth.RolePlatformAdmin, requestIDs[3]),
		}
		for _, response := range responses {
			assertStatus(t, response, http.StatusBadRequest)
			require.Contains(t, response.Body.String(), "supported micro-CNY range")
		}

		var createBase, updateBase, updateMultiplier string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT input_price_cents::text FROM model_prices WHERE model_name = 'v1-overflow-model-create'`,
		).Scan(&createBase))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT input_price_cents::text FROM model_prices WHERE model_name = 'v1-overflow-model-update'`,
		).Scan(&updateBase))
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT multiplier::text FROM org_model_prices WHERE id = $1`, updateOrgID,
		).Scan(&updateMultiplier))
		require.Equal(t, "1.000000", createBase)
		require.Equal(t, "1.000000", updateBase)
		require.Equal(t, "1.000", updateMultiplier)
		var rejectedOrgRows, auditCount int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM org_model_prices
			WHERE org_id = $1 AND model_name = 'v1-overflow-org-create'
		`, fx.orgB).Scan(&rejectedOrgRows))
		require.Zero(t, rejectedOrgRows)
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs WHERE request_id = ANY($1::text[])`, requestIDs,
		).Scan(&auditCount))
		require.Zero(t, auditCount)

		// The rejected organization paths retain a runtime-usable base quote.
		inputPrice, _, err := billing.NewBilling(pool, nil).QueryModelPriceMicrosForOrg(ctx, fx.orgB, "v1-overflow-org-update")
		require.NoError(t, err)
		require.EqualValues(t, 100_000_000_000_000_000, inputPrice)
	})

	t.Run("concurrent base and organization writes cannot commit an overflowing combination", func(t *testing.T) {
		const model = "v1-overflow-concurrent"
		_, err := pool.Exec(ctx, `
			INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
			VALUES ($1, 1, 1)
		`, model)
		require.NoError(t, err)

		start := make(chan struct{})
		responses := make(chan *httptest.ResponseRecorder, 2)
		go func() {
			<-start
			responses <- serve(t, http.MethodPost, "/api/v1/model-prices",
				fmt.Sprintf(`{"model_name":%q,"input_price_cny_per_million_tokens":"9223372036854.7758","output_price_cny_per_million_tokens":1}`, model),
				fx.platformA, fx.orgA, auth.RolePlatformAdmin, "v1-overflow-concurrent-base")
		}()
		go func() {
			<-start
			responses <- serve(t, http.MethodPost, "/api/v1/org-model-prices",
				fmt.Sprintf(`{"org_id":%d,"model_name":%q,"multiplier":2}`, fx.orgB, model),
				fx.platformA, fx.orgA, auth.RolePlatformAdmin, "v1-overflow-concurrent-org")
		}()
		close(start)
		statuses := []int{(<-responses).Code, (<-responses).Code}
		require.ElementsMatch(t, []int{http.StatusCreated, http.StatusBadRequest}, statuses)

		_, _, err = billing.NewBilling(pool, nil).QueryModelPriceMicrosForOrg(ctx, fx.orgB, model)
		require.NoError(t, err)
		var auditCount int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM audit_logs
			WHERE request_id IN ('v1-overflow-concurrent-base', 'v1-overflow-concurrent-org')
		`).Scan(&auditCount))
		require.Equal(t, 1, auditCount)
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
			{requestID: "p6f34-concurrent-model-a", body: `{"model_name":"p6f34-concurrent","input_price_cny_per_million_tokens":70,"output_price_cny_per_million_tokens":140}`},
			{requestID: "p6f34-concurrent-model-b", body: `{"model_name":"p6f34-concurrent","input_price_cny_per_million_tokens":80,"output_price_cny_per_million_tokens":160}`},
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
	EffectiveInputA, EffectiveOutputA     int64
	EffectiveInputB, EffectiveOutputB     int64
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
	got.EffectiveInputA, got.EffectiveOutputA, err = bill.QueryModelPriceMicrosForOrg(ctx, orgA, "p6f34-main")
	require.NoError(t, err)
	got.EffectiveInputB, got.EffectiveOutputB, err = bill.QueryModelPriceMicrosForOrg(ctx, orgB, "p6f34-main")
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
