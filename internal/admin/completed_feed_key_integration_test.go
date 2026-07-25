//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCompletedExecutionFeedKeyControlPlaneLifecycleIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var orgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('completed-feed-key-lifecycle', 'active', 'standard')
		 RETURNING id`,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status, budget_period)
		 VALUES ($1, 'Completed feed key', 'completed-feed-key', 'active', 'none')
		 RETURNING id`,
		orgID,
	).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'completed-feed-org-admin', 'test-hash', 'org_admin', 'active')
		 RETURNING id`,
		orgID,
	).Scan(&actorID))

	handler := NewHandler(pool, "completed-feed-admin-secret", []byte("0123456789abcdef01234567"), nil)
	createBody := fmt.Sprintf(
		`{"org_id":%d,"project_id":%d,"name":"X-Insight completed feed","key_type":"service","allowed_ips":"127.0.0.1/32","allowed_models":"","purpose":"%s"}`,
		orgID,
		projectID,
		middleware.APIKeyPurposeXInsightFeedRead,
	)
	createResponse := httptest.NewRecorder()
	handler.CreateAPIKey(
		createResponse,
		apiKeyIntegrationRequest(
			http.MethodPost,
			"/api/v1/keys",
			createBody,
			actorID,
			orgID,
			auth.RoleOrgAdmin,
			"completed-feed-key-create",
		),
	)
	require.Equal(t, http.StatusCreated, createResponse.Code, createResponse.Body.String())

	var created map[string]any
	require.NoError(t, json.Unmarshal(createResponse.Body.Bytes(), &created))
	keyID := int64(created["id"].(float64))
	require.Equal(t, middleware.APIKeyPurposeXInsightFeedRead, created["purpose"])

	var storedStatus, storedPurpose, storedModels string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, purpose, allowed_models FROM api_keys WHERE id = $1`,
		keyID,
	).Scan(&storedStatus, &storedPurpose, &storedModels))
	require.Equal(t, middleware.APIKeyStatusFeedActive, storedStatus)
	require.Equal(t, middleware.APIKeyPurposeXInsightFeedRead, storedPurpose)
	require.Empty(t, storedModels)

	getRequest := apiKeyIntegrationRequest(
		http.MethodGet,
		fmt.Sprintf("/api/v1/keys/%d", keyID),
		"",
		actorID,
		orgID,
		auth.RoleOrgAdmin,
		"completed-feed-key-get",
	)
	getRequest.SetPathValue("id", fmt.Sprintf("%d", keyID))
	getResponse := httptest.NewRecorder()
	handler.GetAPIKey(getResponse, getRequest)
	require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
	var fetched map[string]any
	require.NoError(t, json.Unmarshal(getResponse.Body.Bytes(), &fetched))
	require.Equal(t, "active", fetched["status"])
	require.Equal(t, middleware.APIKeyPurposeXInsightFeedRead, fetched["purpose"])

	disableRequest := apiKeyIntegrationRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/keys/%d/disable", keyID),
		"",
		actorID,
		orgID,
		auth.RoleOrgAdmin,
		"completed-feed-key-disable",
	)
	disableRequest.SetPathValue("id", fmt.Sprintf("%d", keyID))
	disableResponse := httptest.NewRecorder()
	handler.DisableAPIKey(disableResponse, disableRequest)
	require.Equal(t, http.StatusOK, disableResponse.Code, disableResponse.Body.String())

	reactivateRequest := apiKeyIntegrationRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/keys/%d", keyID),
		`{"status":"active"}`,
		actorID,
		orgID,
		auth.RoleOrgAdmin,
		"completed-feed-key-reactivate",
	)
	reactivateRequest.SetPathValue("id", fmt.Sprintf("%d", keyID))
	reactivateResponse := httptest.NewRecorder()
	handler.UpdateAPIKey(reactivateResponse, reactivateRequest)
	require.Equal(t, http.StatusOK, reactivateResponse.Code, reactivateResponse.Body.String())
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM api_keys WHERE id = $1`,
		keyID,
	).Scan(&storedStatus))
	require.Equal(t, middleware.APIKeyStatusFeedActive, storedStatus)
}
