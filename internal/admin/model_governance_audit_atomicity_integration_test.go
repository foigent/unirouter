//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelGovernanceMutationAuditAtomicityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('model-governance-audit-org', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'model-governance-audit-admin', 'test-hash', 'org_admin', 'active') RETURNING id`, orgID,
	).Scan(&userID))

	var registryID, aliasID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_registry
		 (org_id, model_name, display_name, lifecycle, pricing_model_name, approved_catalog)
		 VALUES ($1, 'model-audit-existing', 'Existing model', 'ga', 'model-audit-existing', FALSE)
		 RETURNING id`, orgID,
	).Scan(&registryID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO model_aliases (org_id, alias, target_model, display_name, status)
		 VALUES ($1, 'model-audit-alias-existing', 'model-audit-existing', 'Existing alias', 'active')
		 RETURNING id`, orgID,
	).Scan(&aliasID))
	require.NoError(t, installRejectModelGovernanceAuditTrigger(ctx, pool))

	h := NewHandler(pool, "test-secret", []byte("0123456789abcdef01234567"), nil)
	serve := func(handler http.HandlerFunc, method, target, body, requestID, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if id != "" {
			req.SetPathValue("id", id)
		}
		reqCtx := context.WithValue(req.Context(), middleware.CtxUserID, userID)
		reqCtx = context.WithValue(reqCtx, middleware.CtxOrgID, orgID)
		reqCtx = context.WithValue(reqCtx, middleware.CtxUserRole, auth.RoleOrgAdmin)
		reqCtx = context.WithValue(reqCtx, middleware.CtxRequestID, requestID)
		recorder := httptest.NewRecorder()
		handler(recorder, req.WithContext(reqCtx))
		return recorder
	}

	t.Run("audit failure rolls back every protected mutation", func(t *testing.T) {
		registryCreate := serve(h.CreateModelRegistry, http.MethodPost, "/api/v1/model-registry",
			`{"model_name":"model-audit-registry-rollback","lifecycle":"ga","approved_catalog":true}`,
			"model-governance-reject-registry-create", "")
		assertModelGovernanceAuditFailure(t, registryCreate)
		assertModelGovernanceNamedRowCount(t, pool, "model_registry", "model-audit-registry-rollback", 0)

		registryUpdate := serve(h.UpdateModelRegistry, http.MethodPut, fmt.Sprintf("/api/v1/model-registry/%d", registryID),
			`{"display_name":"Rollback model","provider_family":"rollback-provider","lifecycle":"deprecated","context_length":32768,"output_limit":4096,"capability_tags":"chat,tools","data_residency":"eu","pricing_model_name":"rollback-pricing","approved_catalog":true,"migration_recommendation":"rollback recommendation"}`,
			"model-governance-reject-registry-update", fmt.Sprintf("%d", registryID))
		assertModelGovernanceAuditFailure(t, registryUpdate)
		var displayName, providerFamily, lifecycle, capabilityTags, dataResidency, pricingModelName, recommendation string
		var contextLength, outputLimit int
		var approved bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT display_name, provider_family, lifecycle, context_length, output_limit,
			        capability_tags, data_residency, pricing_model_name, approved_catalog, migration_recommendation
			 FROM model_registry WHERE id = $1`, registryID,
		).Scan(&displayName, &providerFamily, &lifecycle, &contextLength, &outputLimit,
			&capabilityTags, &dataResidency, &pricingModelName, &approved, &recommendation))
		require.Equal(t, "Existing model", displayName)
		require.Empty(t, providerFamily)
		require.Equal(t, "ga", lifecycle)
		require.Zero(t, contextLength)
		require.Zero(t, outputLimit)
		require.Empty(t, capabilityTags)
		require.Empty(t, dataResidency)
		require.Equal(t, "model-audit-existing", pricingModelName)
		require.False(t, approved)
		require.Empty(t, recommendation)

		aliasCreate := serve(h.CreateModelAlias, http.MethodPost, "/api/v1/model-aliases",
			`{"alias":"model-audit-alias-rollback","target_model":"model-audit-existing"}`,
			"model-governance-reject-alias-create", "")
		assertModelGovernanceAuditFailure(t, aliasCreate)
		assertModelGovernanceNamedRowCount(t, pool, "model_aliases", "model-audit-alias-rollback", 0)

		aliasUpdate := serve(h.UpdateModelAlias, http.MethodPut, fmt.Sprintf("/api/v1/model-aliases/%d", aliasID),
			`{"target_model":"model-audit-other","status":"disabled"}`,
			"model-governance-reject-alias-update", fmt.Sprintf("%d", aliasID))
		assertModelGovernanceAuditFailure(t, aliasUpdate)
		var targetModel, status string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT target_model, status FROM model_aliases WHERE id = $1`, aliasID,
		).Scan(&targetModel, &status))
		require.Equal(t, "model-audit-existing", targetModel)
		require.Equal(t, "active", status)

		var auditCount int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs WHERE request_id LIKE 'model-governance-reject-%'`,
		).Scan(&auditCount))
		require.Zero(t, auditCount)
	})

	t.Run("successful controls commit mutation and scoped audit", func(t *testing.T) {
		registryCreate := serve(h.CreateModelRegistry, http.MethodPost, "/api/v1/model-registry",
			`{"model_name":"model-audit-registry-success","lifecycle":"ga","approved_catalog":true}`,
			"model-governance-success-registry-create", "")
		require.Equal(t, http.StatusCreated, registryCreate.Code, registryCreate.Body.String())
		assertModelGovernanceNamedRowCount(t, pool, "model_registry", "model-audit-registry-success", 1)
		assertModelGovernanceAudit(t, pool, "model-governance-success-registry-create", "model_registry.create", orgID, userID)

		registryUpdate := serve(h.UpdateModelRegistry, http.MethodPut, fmt.Sprintf("/api/v1/model-registry/%d", registryID),
			`{"display_name":"Updated model","provider_family":"updated-provider","lifecycle":"deprecated","context_length":32768,"output_limit":4096,"capability_tags":"chat,tools","data_residency":"eu","pricing_model_name":"updated-pricing","approved_catalog":true,"migration_recommendation":"use model-b"}`,
			"model-governance-success-registry-update", fmt.Sprintf("%d", registryID))
		require.Equal(t, http.StatusOK, registryUpdate.Code, registryUpdate.Body.String())
		var displayName, providerFamily, lifecycle, capabilityTags, dataResidency, pricingModelName, recommendation string
		var contextLength, outputLimit int
		var approved bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT display_name, provider_family, lifecycle, context_length, output_limit,
			        capability_tags, data_residency, pricing_model_name, approved_catalog, migration_recommendation
			 FROM model_registry WHERE id = $1`, registryID,
		).Scan(&displayName, &providerFamily, &lifecycle, &contextLength, &outputLimit,
			&capabilityTags, &dataResidency, &pricingModelName, &approved, &recommendation))
		require.Equal(t, "Updated model", displayName)
		require.Equal(t, "updated-provider", providerFamily)
		require.Equal(t, "deprecated", lifecycle)
		require.Equal(t, 32768, contextLength)
		require.Equal(t, 4096, outputLimit)
		require.Equal(t, "chat,tools", capabilityTags)
		require.Equal(t, "eu", dataResidency)
		require.Equal(t, "updated-pricing", pricingModelName)
		require.True(t, approved)
		require.Equal(t, "use model-b", recommendation)
		assertModelGovernanceAudit(t, pool, "model-governance-success-registry-update", "model_registry.update", orgID, userID)
		assertModelRegistryAuditState(t, pool, "model-governance-success-registry-update", map[string]any{
			"display_name": "Existing model", "provider_family": "", "context_length": float64(0),
			"output_limit": float64(0), "capability_tags": "", "data_residency": "",
			"pricing_model_name": "model-audit-existing",
		}, map[string]any{
			"display_name": "Updated model", "provider_family": "updated-provider", "context_length": float64(32768),
			"output_limit": float64(4096), "capability_tags": "chat,tools", "data_residency": "eu",
			"pricing_model_name": "updated-pricing",
		})

		aliasCreate := serve(h.CreateModelAlias, http.MethodPost, "/api/v1/model-aliases",
			`{"alias":"model-audit-alias-success","target_model":"model-audit-existing"}`,
			"model-governance-success-alias-create", "")
		require.Equal(t, http.StatusCreated, aliasCreate.Code, aliasCreate.Body.String())
		assertModelGovernanceNamedRowCount(t, pool, "model_aliases", "model-audit-alias-success", 1)
		assertModelGovernanceAudit(t, pool, "model-governance-success-alias-create", "model_alias.create", orgID, userID)

		aliasUpdate := serve(h.UpdateModelAlias, http.MethodPut, fmt.Sprintf("/api/v1/model-aliases/%d", aliasID),
			`{"target_model":"model-audit-other","status":"disabled"}`,
			"model-governance-success-alias-update", fmt.Sprintf("%d", aliasID))
		require.Equal(t, http.StatusOK, aliasUpdate.Code, aliasUpdate.Body.String())
		var targetModel, status string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT target_model, status FROM model_aliases WHERE id = $1`, aliasID,
		).Scan(&targetModel, &status))
		require.Equal(t, "model-audit-other", targetModel)
		require.Equal(t, "disabled", status)
		assertModelGovernanceAudit(t, pool, "model-governance-success-alias-update", "model_alias.update", orgID, userID)
	})
}

func installRejectModelGovernanceAuditTrigger(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE FUNCTION reject_model_governance_audit_for_test() RETURNS trigger AS $$
		BEGIN
			IF NEW.request_id LIKE 'model-governance-reject-%'
			   AND NEW.action IN (
				'model_registry.create', 'model_registry.update',
				'model_alias.create', 'model_alias.update'
			   ) THEN
				RAISE EXCEPTION 'forced model-governance audit failure';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_model_governance_audit_for_test
		BEFORE INSERT ON audit_logs
		FOR EACH ROW EXECUTE FUNCTION reject_model_governance_audit_for_test();`)
	return err
}

func assertModelGovernanceAuditFailure(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "Audit failed")
}

func assertModelGovernanceNamedRowCount(t *testing.T, pool *pgxpool.Pool, table, name string, want int) {
	t.Helper()
	query := map[string]string{
		"model_registry": `SELECT COUNT(*) FROM model_registry WHERE model_name = $1`,
		"model_aliases":  `SELECT COUNT(*) FROM model_aliases WHERE alias = $1`,
	}[table]
	require.NotEmpty(t, query)
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), query, name).Scan(&count))
	require.Equal(t, want, count)
}

func assertModelGovernanceAudit(t *testing.T, pool *pgxpool.Pool, requestID, action string, orgID, userID int64) {
	t.Helper()
	var count int
	var gotOrgID, gotUserID int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COUNT(*), COALESCE(MAX(org_id), 0), COALESCE(MAX(user_id), 0)
		 FROM audit_logs WHERE request_id = $1 AND action = $2`, requestID, action,
	).Scan(&count, &gotOrgID, &gotUserID))
	require.Equal(t, 1, count)
	require.Equal(t, orgID, gotOrgID)
	require.Equal(t, userID, gotUserID)
}

func assertModelRegistryAuditState(t *testing.T, pool *pgxpool.Pool, requestID string, wantBefore, wantAfter map[string]any) {
	t.Helper()
	var beforeJSON, afterJSON []byte
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT before_state, after_state FROM audit_logs WHERE request_id = $1 AND action = 'model_registry.update'`, requestID,
	).Scan(&beforeJSON, &afterJSON))
	var before, after map[string]any
	require.NoError(t, json.Unmarshal(beforeJSON, &before))
	require.NoError(t, json.Unmarshal(afterJSON, &after))
	for field, want := range wantBefore {
		require.Equal(t, want, before[field], "before.%s", field)
	}
	for field, want := range wantAfter {
		require.Equal(t, want, after[field], "after.%s", field)
	}
}
