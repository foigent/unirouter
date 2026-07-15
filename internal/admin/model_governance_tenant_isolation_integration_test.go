package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F36ModelGovernanceTenantIsolationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	fx := seedP6F16Fixture(t, pool)

	var registryA, registryB int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_registry
		 (org_id, model_name, display_name, provider_family, lifecycle, pricing_model_name, approved_catalog)
		 VALUES ($1, 'p6f36-model-a', 'Model A', 'test', 'ga', 'p6f36-model-a', TRUE)
		 RETURNING id`, fx.orgA,
	).Scan(&registryA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_registry
		 (org_id, model_name, display_name, provider_family, lifecycle, pricing_model_name, approved_catalog)
		 VALUES ($1, 'p6f36-model-b', 'Model B', 'test', 'ga', 'p6f36-model-b', TRUE)
		 RETURNING id`, fx.orgB,
	).Scan(&registryB))
	_, err := pool.Exec(ctx,
		`INSERT INTO model_registry
		 (org_id, model_name, display_name, provider_family, lifecycle, pricing_model_name, approved_catalog)
		 VALUES
		 ($1, 'p6f36-runtime-model', 'Runtime Model', 'test', 'ga', 'p6f36-runtime-model', TRUE),
		 ($1, 'foreign-model', 'Malformed Cross-Org Candidate', 'test', 'ga', 'foreign-model', TRUE)`,
		fx.orgA,
	)
	require.NoError(t, err)

	var aliasA, aliasB int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_aliases (org_id, alias, target_model, display_name, status)
		 VALUES ($1, 'p6f36-alias-a', 'p6f36-model-a', 'Alias A', 'active') RETURNING id`, fx.orgA,
	).Scan(&aliasA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_aliases (org_id, alias, target_model, display_name, status)
		 VALUES ($1, 'p6f36-alias-b', 'p6f36-model-b', 'Alias B', 'active') RETURNING id`, fx.orgB,
	).Scan(&aliasB))
	_, err = pool.Exec(ctx,
		`INSERT INTO model_aliases (org_id, alias, target_model, display_name, status)
		 VALUES ($1, 'p6f36-runtime-alias', 'p6f36-runtime-model', 'Runtime Alias', 'active')`,
		fx.orgA,
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx,
		`INSERT INTO model_set_items (model_set_id, item_type, item_name) VALUES
		 ($1, 'model', 'p6f36-model-a'),
		 ($1, 'alias', 'p6f36-runtime-alias'),
		 ($2, 'model', 'p6f36-model-b'),
		 ($2, 'alias', 'p6f36-alias-b')
		 ON CONFLICT DO NOTHING`,
		fx.modelSetA, fx.modelSetB,
	)
	require.NoError(t, err)

	// This deliberately malformed historical row must not affect either catalog
	// filtering or live model-governance decisions for Project A.
	_, err = pool.Exec(ctx,
		`INSERT INTO project_model_sets (project_id, model_set_id)
		 VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		fx.projectA, fx.modelSetB,
	)
	require.NoError(t, err)

	var projectAdminA int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'p6f36-project-admin-a', 'test-hash', $2, 'active') RETURNING id`,
		fx.orgA, auth.RoleProjectAdmin,
	).Scan(&projectAdminA))
	_, err = pool.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role)
		 VALUES ($1, $2, 'project_admin')`, fx.projectA, projectAdminA,
	)
	require.NoError(t, err)

	h := NewHandler(pool, p6f16JWTSecret, []byte("0123456789abcdef01234567"), nil)
	h.SetEventService(billing.NewEventService(pool))
	mux := p6f36ModelGovernanceMux(h)
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

	t.Run("registry alias and set lists are organization scoped", func(t *testing.T) {
		paths := []string{
			"/api/v1/model-registry",
			"/api/v1/model-aliases",
			"/api/v1/model-sets",
		}
		for _, path := range paths {
			t.Run(path, func(t *testing.T) {
				own := serve(t, http.MethodGet, path, "", fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "")
				assertStatus(t, own, http.StatusOK)
				assertResponseOrgIDs(t, own.Body.Bytes(), fx.orgA)

				assertStatus(t, serve(t, http.MethodGet,
					fmt.Sprintf("%s?org_id=%d", path, fx.orgB), "",
					fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, ""), http.StatusForbidden)
				assertStatus(t, serve(t, http.MethodGet, path+"?org_id=0", "",
					fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, ""), http.StatusBadRequest)
				assertStatus(t, serve(t, http.MethodGet, path, "",
					fx.orgAdminA, 0, auth.RoleOrgAdmin, ""), http.StatusUnauthorized)

				platform := serve(t, http.MethodGet,
					fmt.Sprintf("%s?org_id=%d", path, fx.orgB), "",
					fx.platformA, fx.orgA, auth.RolePlatformAdmin, "")
				assertStatus(t, platform, http.StatusOK)
				assertResponseOrgIDs(t, platform.Body.Bytes(), fx.orgB)
			})
		}
	})

	t.Run("registry alias and set creates bind the authorized organization", func(t *testing.T) {
		cases := []struct {
			name          string
			path          string
			action        string
			ownBody       string
			foreignBody   string
			zeroBody      string
			platformBody  string
			rowOrg        func(*testing.T, string) int64
			ownLookup     string
			foreignLookup string
			zeroLookup    string
			platLookup    string
		}{
			{
				name:         "registry",
				path:         "/api/v1/model-registry",
				action:       "model_registry.create",
				ownBody:      `{"model_name":"p6f36-created-reg-own","lifecycle":"ga"}`,
				foreignBody:  fmt.Sprintf(`{"org_id":%d,"model_name":"p6f36-created-reg-denied","lifecycle":"ga"}`, fx.orgB),
				zeroBody:     `{"org_id":0,"model_name":"p6f36-created-reg-zero","lifecycle":"ga"}`,
				platformBody: fmt.Sprintf(`{"org_id":%d,"model_name":"p6f36-created-reg-platform","lifecycle":"ga"}`, fx.orgB),
				rowOrg: func(t *testing.T, lookup string) int64 {
					var orgID int64
					require.NoError(t, pool.QueryRow(ctx, `SELECT org_id FROM model_registry WHERE model_name = $1`, lookup).Scan(&orgID))
					return orgID
				},
				ownLookup:     "p6f36-created-reg-own",
				foreignLookup: "p6f36-created-reg-denied",
				zeroLookup:    "p6f36-created-reg-zero",
				platLookup:    "p6f36-created-reg-platform",
			},
			{
				name:         "alias",
				path:         "/api/v1/model-aliases",
				action:       "model_alias.create",
				ownBody:      `{"alias":"p6f36-created-alias-own","target_model":"p6f36-model-a"}`,
				foreignBody:  fmt.Sprintf(`{"org_id":%d,"alias":"p6f36-created-alias-denied","target_model":"p6f36-model-b"}`, fx.orgB),
				zeroBody:     `{"org_id":0,"alias":"p6f36-created-alias-zero","target_model":"p6f36-model-a"}`,
				platformBody: fmt.Sprintf(`{"org_id":%d,"alias":"p6f36-created-alias-platform","target_model":"p6f36-model-b"}`, fx.orgB),
				rowOrg: func(t *testing.T, lookup string) int64 {
					var orgID int64
					require.NoError(t, pool.QueryRow(ctx, `SELECT org_id FROM model_aliases WHERE alias = $1`, lookup).Scan(&orgID))
					return orgID
				},
				ownLookup:     "p6f36-created-alias-own",
				foreignLookup: "p6f36-created-alias-denied",
				zeroLookup:    "p6f36-created-alias-zero",
				platLookup:    "p6f36-created-alias-platform",
			},
			{
				name:         "set",
				path:         "/api/v1/model-sets",
				action:       "model_set.create",
				ownBody:      `{"name":"P6F36 Created Set Own","slug":"p6f36-created-set-own","items":[{"item_type":"model","item_name":"p6f36-model-a"}]}`,
				foreignBody:  fmt.Sprintf(`{"org_id":%d,"name":"P6F36 Created Set Denied","slug":"p6f36-created-set-denied","items":[{"item_type":"model","item_name":"p6f36-model-b"}]}`, fx.orgB),
				zeroBody:     `{"org_id":0,"name":"P6F36 Created Set Zero","slug":"p6f36-created-set-zero","items":[{"item_type":"model","item_name":"p6f36-model-a"}]}`,
				platformBody: fmt.Sprintf(`{"org_id":%d,"name":"P6F36 Created Set Platform","slug":"p6f36-created-set-platform","items":[{"item_type":"model","item_name":"p6f36-model-b"}]}`, fx.orgB),
				rowOrg: func(t *testing.T, lookup string) int64 {
					var orgID int64
					require.NoError(t, pool.QueryRow(ctx, `SELECT org_id FROM model_sets WHERE slug = $1`, lookup).Scan(&orgID))
					return orgID
				},
				ownLookup:     "p6f36-created-set-own",
				foreignLookup: "p6f36-created-set-denied",
				zeroLookup:    "p6f36-created-set-zero",
				platLookup:    "p6f36-created-set-platform",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ownRequestID := "p6f36-create-" + tc.name + "-own"
				assertStatus(t, serve(t, http.MethodPost, tc.path, tc.ownBody,
					fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, ownRequestID), http.StatusCreated)
				require.Equal(t, fx.orgA, tc.rowOrg(t, tc.ownLookup))
				p6f36AssertAuditOrg(t, pool, ownRequestID, tc.action, fx.orgA)

				deniedRequestID := "p6f36-create-" + tc.name + "-denied"
				assertStatus(t, serve(t, http.MethodPost, tc.path, tc.foreignBody,
					fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, deniedRequestID), http.StatusForbidden)
				p6f36AssertNoRow(t, pool, tc.name, tc.foreignLookup)
				p6f36AssertNoAudit(t, pool, deniedRequestID)

				zeroRequestID := "p6f36-create-" + tc.name + "-zero"
				assertStatus(t, serve(t, http.MethodPost, tc.path, tc.zeroBody,
					fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, zeroRequestID), http.StatusBadRequest)
				p6f36AssertNoRow(t, pool, tc.name, tc.zeroLookup)
				p6f36AssertNoAudit(t, pool, zeroRequestID)

				platformRequestID := "p6f36-create-" + tc.name + "-platform"
				assertStatus(t, serve(t, http.MethodPost, tc.path, tc.platformBody,
					fx.platformA, fx.orgA, auth.RolePlatformAdmin, platformRequestID), http.StatusCreated)
				require.Equal(t, fx.orgB, tc.rowOrg(t, tc.platLookup))
				p6f36AssertAuditOrg(t, pool, platformRequestID, tc.action, fx.orgB)
			})
		}
	})

	t.Run("approved catalog derives canonical organization from project", func(t *testing.T) {
		own := serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-catalog?project_id=%d", fx.projectA), "",
			projectAdminA, fx.orgA, auth.RoleProjectAdmin, "")
		assertStatus(t, own, http.StatusOK)
		require.Equal(t,
			[]string{"p6f36-model-a", "p6f36-runtime-model"},
			p6f36SortedDataStrings(t, own.Body.Bytes(), "model_name"),
			"the malformed Project A to Org B set assignment must be ignored",
		)
		assertResponseOrgIDs(t, own.Body.Bytes(), fx.orgA)

		canonicalForeign := serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-catalog?project_id=%d", fx.projectB), "",
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "")
		assertStatus(t, canonicalForeign, http.StatusOK)
		require.Equal(t, []string{"p6f36-model-b"}, p6f36SortedDataStrings(t, canonicalForeign.Body.Bytes(), "model_name"))
		assertResponseOrgIDs(t, canonicalForeign.Body.Bytes(), fx.orgB)

		assertStatus(t, serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-catalog?project_id=%d&org_id=%d", fx.projectB, fx.orgA), "",
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, ""), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-catalog?project_id=%d&org_id=0", fx.projectA), "",
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, ""), http.StatusBadRequest)
		assertStatus(t, serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-catalog?project_id=%d", fx.projectB), "",
			projectAdminA, fx.orgA, auth.RoleProjectAdmin, ""), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodGet, "/api/v1/model-catalog", "",
			projectAdminA, fx.orgA, auth.RoleProjectAdmin, ""), http.StatusForbidden)
	})

	t.Run("live access enforcement ignores malformed cross organization assignments", func(t *testing.T) {
		result, accessErr := modelgov.CheckAccess(ctx, pool, fx.orgA, fx.projectA, "p6f36-runtime-alias")
		require.NoError(t, accessErr)
		require.True(t, result.Allowed)
		require.Equal(t, "p6f36-runtime-model", result.ResolvedModel)
		require.Equal(t, "p6f36-runtime-alias", result.Alias)

		result, accessErr = modelgov.CheckAccess(ctx, pool, fx.orgA, fx.projectA, "foreign-model")
		require.ErrorIs(t, accessErr, modelgov.ErrModelDenied)
		require.False(t, result.Allowed)
		require.Equal(t, "project_model_not_allowed", result.Reason)
	})

	t.Run("registry global IDs are scoped and lifecycle side effects follow target organization", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-registry/%d", registryA), `{"display_name":"Model A Updated"}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-registry-own"), http.StatusOK)
		var displayName string
		require.NoError(t, pool.QueryRow(ctx, `SELECT display_name FROM model_registry WHERE id = $1`, registryA).Scan(&displayName))
		require.Equal(t, "Model A Updated", displayName)
		p6f36AssertAuditOrg(t, pool, "p6f36-registry-own", "model_registry.update", fx.orgA)

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-registry/%d", registryB), `{"lifecycle":"deprecated"}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-registry-denied"), http.StatusNotFound)
		var lifecycle string
		require.NoError(t, pool.QueryRow(ctx, `SELECT lifecycle FROM model_registry WHERE id = $1`, registryB).Scan(&lifecycle))
		require.Equal(t, modelgov.LifecycleGA, lifecycle)
		p6f36AssertNoAudit(t, pool, "p6f36-registry-denied")
		p6f36AssertNoEvent(t, pool, "model_registry", registryB, modelgov.LifecycleDeprecated)

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-registry/%d?org_id=%d", registryB, fx.orgB), `{"lifecycle":"deprecated"}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-registry-explicit-denied"), http.StatusForbidden)
		p6f36AssertNoAudit(t, pool, "p6f36-registry-explicit-denied")
		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-registry/%d?org_id=0", registryA), `{"display_name":"Invalid Zero"}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-registry-zero"), http.StatusBadRequest)
		p6f36AssertNoAudit(t, pool, "p6f36-registry-zero")

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-registry/%d?org_id=%d", registryB, fx.orgB), `{"lifecycle":"deprecated"}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f36-registry-platform"), http.StatusOK)
		require.NoError(t, pool.QueryRow(ctx, `SELECT lifecycle FROM model_registry WHERE id = $1`, registryB).Scan(&lifecycle))
		require.Equal(t, modelgov.LifecycleDeprecated, lifecycle)
		p6f36AssertAuditOrg(t, pool, "p6f36-registry-platform", "model_registry.update", fx.orgB)
		p6f36AssertEventOrg(t, pool, "model_registry", registryB, modelgov.LifecycleDeprecated, fx.orgB)

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-registry/%d", registryB), `{"display_name":"Model B Platform ID"}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f36-registry-platform-id"), http.StatusOK)
		require.NoError(t, pool.QueryRow(ctx, `SELECT display_name FROM model_registry WHERE id = $1`, registryB).Scan(&displayName))
		require.Equal(t, "Model B Platform ID", displayName)
		p6f36AssertAuditOrg(t, pool, "p6f36-registry-platform-id", "model_registry.update", fx.orgB)
	})

	t.Run("alias global IDs are scoped without enumeration", func(t *testing.T) {
		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-aliases/%d", aliasA), `{"display_name":"Alias A Updated"}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-alias-own"), http.StatusOK)
		var displayName string
		require.NoError(t, pool.QueryRow(ctx, `SELECT display_name FROM model_aliases WHERE id = $1`, aliasA).Scan(&displayName))
		require.Equal(t, "Alias A Updated", displayName)
		p6f36AssertAuditOrg(t, pool, "p6f36-alias-own", "model_alias.update", fx.orgA)

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-aliases/%d", aliasB), `{"status":"disabled"}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-alias-denied"), http.StatusNotFound)
		var status string
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM model_aliases WHERE id = $1`, aliasB).Scan(&status))
		require.Equal(t, modelgov.AliasStatusActive, status)
		p6f36AssertNoAudit(t, pool, "p6f36-alias-denied")

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-aliases/%d?org_id=%d", aliasB, fx.orgB), `{"status":"disabled"}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-alias-explicit-denied"), http.StatusForbidden)
		p6f36AssertNoAudit(t, pool, "p6f36-alias-explicit-denied")
		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-aliases/%d?org_id=0", aliasA), `{"display_name":"Invalid Zero"}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-alias-zero"), http.StatusBadRequest)
		p6f36AssertNoAudit(t, pool, "p6f36-alias-zero")

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-aliases/%d?org_id=%d", aliasB, fx.orgB), `{"status":"disabled"}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f36-alias-platform"), http.StatusOK)
		require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM model_aliases WHERE id = $1`, aliasB).Scan(&status))
		require.Equal(t, modelgov.AliasStatusDisabled, status)
		p6f36AssertAuditOrg(t, pool, "p6f36-alias-platform", "model_alias.update", fx.orgB)

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-aliases/%d", aliasB), `{"display_name":"Alias B Platform ID"}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f36-alias-platform-id"), http.StatusOK)
		require.NoError(t, pool.QueryRow(ctx, `SELECT display_name FROM model_aliases WHERE id = $1`, aliasB).Scan(&displayName))
		require.Equal(t, "Alias B Platform ID", displayName)
		p6f36AssertAuditOrg(t, pool, "p6f36-alias-platform-id", "model_alias.update", fx.orgB)
	})

	t.Run("model set item reads and replacements are scoped and audited", func(t *testing.T) {
		own := serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-sets/%d/items", fx.modelSetA), "",
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "")
		assertStatus(t, own, http.StatusOK)
		require.Contains(t, p6f36SortedDataStrings(t, own.Body.Bytes(), "item_name"), "p6f36-model-a")

		assertStatus(t, serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-sets/%d/items", fx.modelSetB), "",
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, ""), http.StatusNotFound)
		assertStatus(t, serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-sets/%d/items?org_id=%d", fx.modelSetB, fx.orgB), "",
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, ""), http.StatusForbidden)
		assertStatus(t, serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-sets/%d/items?org_id=0", fx.modelSetA), "",
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, ""), http.StatusBadRequest)
		platformList := serve(t, http.MethodGet,
			fmt.Sprintf("/api/v1/model-sets/%d/items?org_id=%d", fx.modelSetB, fx.orgB), "",
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "")
		assertStatus(t, platformList, http.StatusOK)
		require.Contains(t, p6f36SortedDataStrings(t, platformList.Body.Bytes(), "item_name"), "p6f36-model-b")

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-sets/%d/items", fx.modelSetA),
			`{"items":[{"item_type":"model","item_name":"p6f36-runtime-model"}]}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-set-items-own"), http.StatusOK)
		require.Equal(t, []string{"model:p6f36-runtime-model"}, p6f36ReadSetItems(t, pool, fx.modelSetA))
		p6f36AssertAuditOrg(t, pool, "p6f36-set-items-own", "model_set.items.replace", fx.orgA)

		beforeForeign := p6f36ReadSetItems(t, pool, fx.modelSetB)
		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-sets/%d/items", fx.modelSetB),
			`{"items":[{"item_type":"model","item_name":"p6f36-compromised"}]}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-set-items-denied"), http.StatusNotFound)
		require.Equal(t, beforeForeign, p6f36ReadSetItems(t, pool, fx.modelSetB))
		p6f36AssertNoAudit(t, pool, "p6f36-set-items-denied")
		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-sets/%d/items?org_id=%d", fx.modelSetB, fx.orgB),
			`{"items":[{"item_type":"model","item_name":"p6f36-explicit-compromised"}]}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-set-items-explicit-denied"), http.StatusForbidden)
		require.Equal(t, beforeForeign, p6f36ReadSetItems(t, pool, fx.modelSetB))
		p6f36AssertNoAudit(t, pool, "p6f36-set-items-explicit-denied")
		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-sets/%d/items?org_id=0", fx.modelSetA),
			`{"items":[{"item_type":"model","item_name":"p6f36-invalid-zero"}]}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-set-items-zero"), http.StatusBadRequest)
		require.Equal(t, []string{"model:p6f36-runtime-model"}, p6f36ReadSetItems(t, pool, fx.modelSetA))
		p6f36AssertNoAudit(t, pool, "p6f36-set-items-zero")

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-sets/%d/items?org_id=%d", fx.modelSetB, fx.orgB),
			`{"items":[{"item_type":"model","item_name":"p6f36-model-b"},{"item_type":"alias","item_name":"p6f36-alias-b"}]}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f36-set-items-platform"), http.StatusOK)
		require.Equal(t,
			[]string{"alias:p6f36-alias-b", "model:p6f36-model-b"},
			p6f36ReadSetItems(t, pool, fx.modelSetB),
		)
		p6f36AssertAuditOrg(t, pool, "p6f36-set-items-platform", "model_set.items.replace", fx.orgB)

		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-sets/%d/items", fx.modelSetB),
			`{"items":[{"item_type":"model","item_name":"p6f36-model-b"}]}`,
			fx.platformA, fx.orgA, auth.RolePlatformAdmin, "p6f36-set-items-platform-id"), http.StatusOK)
		require.Equal(t, []string{"model:p6f36-model-b"}, p6f36ReadSetItems(t, pool, fx.modelSetB))
		p6f36AssertAuditOrg(t, pool, "p6f36-set-items-platform-id", "model_set.items.replace", fx.orgB)
	})

	t.Run("model set audit failures roll back parents items and policy", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
			CREATE OR REPLACE FUNCTION p6f36_reject_selected_audit()
			RETURNS trigger AS $$
			BEGIN
				IF NEW.request_id IN ('p6f36-force-audit-set-create', 'p6f36-force-audit-set-replace') THEN
					RAISE EXCEPTION 'p6f36 forced audit failure';
				END IF;
				RETURN NEW;
			END;
			$$ LANGUAGE plpgsql;

			CREATE TRIGGER p6f36_reject_selected_audit_trigger
			BEFORE INSERT ON audit_logs
			FOR EACH ROW EXECUTE FUNCTION p6f36_reject_selected_audit();
		`)
		require.NoError(t, err)

		assertStatus(t, serve(t, http.MethodPost, "/api/v1/model-sets",
			`{"name":"P6F36 Rollback Create","slug":"p6f36-rollback-create","items":[{"item_type":"model","item_name":"p6f36-rollback-create-item"}]}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-force-audit-set-create"), http.StatusInternalServerError)
		p6f36AssertNoRow(t, pool, "set", "p6f36-rollback-create")
		var createdItemCount int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM model_set_items WHERE item_name = 'p6f36-rollback-create-item'`,
		).Scan(&createdItemCount))
		require.Zero(t, createdItemCount, "failed create left orphan model-set items")
		p6f36AssertNoAudit(t, pool, "p6f36-force-audit-set-create")

		beforePolicy := p6f36ReadSetItems(t, pool, fx.modelSetA)
		assertStatus(t, serve(t, http.MethodPut,
			fmt.Sprintf("/api/v1/model-sets/%d/items", fx.modelSetA),
			`{"items":[{"item_type":"model","item_name":"p6f36-rollback-replace-item"}]}`,
			fx.orgAdminA, fx.orgA, auth.RoleOrgAdmin, "p6f36-force-audit-set-replace"), http.StatusInternalServerError)
		require.Equal(t, beforePolicy, p6f36ReadSetItems(t, pool, fx.modelSetA), "failed audit changed the active model-set policy")
		p6f36AssertNoAudit(t, pool, "p6f36-force-audit-set-replace")
	})
}

func p6f36ModelGovernanceMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	roleRoute := func(handler http.HandlerFunc, roles ...string) http.Handler {
		return middleware.AdminAuth(h.db, p6f16JWTSecret)(middleware.RequireRoles(roles...)(handler))
	}
	orgAdmin := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin}
	projectRead := []string{auth.RolePlatformAdmin, auth.RoleOrgAdmin, auth.RoleProjectAdmin, auth.RoleDeveloper, auth.RoleViewer}

	mux.Handle("GET /api/v1/model-registry", roleRoute(h.ListModelRegistry, orgAdmin...))
	mux.Handle("POST /api/v1/model-registry", roleRoute(h.CreateModelRegistry, orgAdmin...))
	mux.Handle("PUT /api/v1/model-registry/{id}", roleRoute(h.UpdateModelRegistry, orgAdmin...))
	mux.Handle("GET /api/v1/model-catalog", roleRoute(h.ListApprovedModelCatalog, projectRead...))
	mux.Handle("GET /api/v1/model-aliases", roleRoute(h.ListModelAliases, orgAdmin...))
	mux.Handle("POST /api/v1/model-aliases", roleRoute(h.CreateModelAlias, orgAdmin...))
	mux.Handle("PUT /api/v1/model-aliases/{id}", roleRoute(h.UpdateModelAlias, orgAdmin...))
	mux.Handle("GET /api/v1/model-sets", roleRoute(h.ListModelSets, orgAdmin...))
	mux.Handle("POST /api/v1/model-sets", roleRoute(h.CreateModelSet, orgAdmin...))
	mux.Handle("GET /api/v1/model-sets/{id}/items", roleRoute(h.ListModelSetItems, orgAdmin...))
	mux.Handle("PUT /api/v1/model-sets/{id}/items", roleRoute(h.ReplaceModelSetItems, orgAdmin...))
	return mux
}

func p6f36SortedDataStrings(t *testing.T, body []byte, field string) []string {
	t.Helper()
	var response struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	values := make([]string, 0, len(response.Data))
	for _, item := range response.Data {
		value, ok := item[field].(string)
		require.True(t, ok, "missing string field %q in %#v", field, item)
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func p6f36AssertAuditOrg(t *testing.T, pool *pgxpool.Pool, requestID, action string, wantOrgID int64) {
	t.Helper()
	var count int
	var orgID int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*), COALESCE(MAX(org_id), 0)
		 FROM audit_logs WHERE request_id = $1 AND action = $2`, requestID, action,
	).Scan(&count, &orgID))
	require.Equal(t, 1, count, "expected one audit row for %s", requestID)
	require.Equal(t, wantOrgID, orgID, "audit row attributed to wrong organization for %s", requestID)
}

func p6f36AssertNoAudit(t *testing.T, pool *pgxpool.Pool, requestID string) {
	t.Helper()
	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_logs WHERE request_id = $1`, requestID,
	).Scan(&count))
	require.Zero(t, count, "denied request created an audit side effect")
}

func p6f36AssertNoRow(t *testing.T, pool *pgxpool.Pool, kind, lookup string) {
	t.Helper()
	query := map[string]string{
		"registry": `SELECT COUNT(*) FROM model_registry WHERE model_name = $1`,
		"alias":    `SELECT COUNT(*) FROM model_aliases WHERE alias = $1`,
		"set":      `SELECT COUNT(*) FROM model_sets WHERE slug = $1`,
	}[kind]
	require.NotEmpty(t, query)
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), query, lookup).Scan(&count))
	require.Zero(t, count, "denied %s create mutated the database", kind)
}

func p6f36AssertNoEvent(t *testing.T, pool *pgxpool.Pool, resource string, resourceID int64, lifecycle string) {
	t.Helper()
	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM notification_events
		 WHERE resource = $1 AND resource_id = $2 AND payload->>'lifecycle' = $3`,
		resource, fmt.Sprint(resourceID), lifecycle,
	).Scan(&count))
	require.Zero(t, count, "denied request emitted a notification event")
}

func p6f36AssertEventOrg(t *testing.T, pool *pgxpool.Pool, resource string, resourceID int64, lifecycle string, wantOrgID int64) {
	t.Helper()
	var count int
	var orgID int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*), COALESCE(MAX(org_id), 0) FROM notification_events
		 WHERE resource = $1 AND resource_id = $2 AND payload->>'lifecycle' = $3`,
		resource, fmt.Sprint(resourceID), lifecycle,
	).Scan(&count, &orgID))
	require.Equal(t, 1, count)
	require.Equal(t, wantOrgID, orgID)
}

func p6f36ReadSetItems(t *testing.T, pool *pgxpool.Pool, modelSetID int64) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT item_type, item_name FROM model_set_items
		 WHERE model_set_id = $1 ORDER BY item_type, item_name`, modelSetID)
	require.NoError(t, err)
	defer rows.Close()
	items := make([]string, 0)
	for rows.Next() {
		var itemType, itemName string
		require.NoError(t, rows.Scan(&itemType, &itemName))
		items = append(items, itemType+":"+itemName)
	}
	require.NoError(t, rows.Err())
	return items
}
