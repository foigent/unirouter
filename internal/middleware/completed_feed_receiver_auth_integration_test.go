//go:build integration

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCompletedExecutionFeedReceiverCredentialResolutionIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('receiver-feed-auth', 'active', 'standard') RETURNING id`,
	).Scan(&orgID))

	newProject := func(label string) int64 {
		t.Helper()
		var projectID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO projects (org_id, name, slug, status, budget_period)
			 VALUES ($1, $2, $3, 'active', 'none') RETURNING id`,
			orgID,
			"Receiver auth "+label,
			"receiver-auth-"+label,
		).Scan(&projectID))
		return projectID
	}
	insertReceiver := func(projectID int64, status, rawToken, operationID string) int64 {
		t.Helper()
		hash, prefix, err := auth.HashReceiverFeedCredential(rawToken, "receiver-auth-secret")
		require.NoError(t, err)
		var keyID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO api_keys (
			    org_id, project_id, key_type, name, key_hash, key_prefix, status,
			    rpm_limit, qps_limit, allowed_ips, allowed_models, purpose,
			    verifier_scheme, registration_operation_id
			 ) VALUES (
			    $1, $2, 'service', $3, $4, $5, $6,
			    60, 1, '127.0.0.1/32', '', 'xinsight.feed.read',
			    'receiver_sha256_v1', $7
			 ) RETURNING id`,
			orgID, projectID, "receiver "+operationID, hash, prefix, status, operationID,
		).Scan(&keyID))
		return keyID
	}
	request := func(handler http.Handler, rawToken string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/xinsight/completed-executions", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		req.RemoteAddr = "127.0.0.1:44221"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	activeToken := auth.ReceiverFeedTokenPrefix + strings.Repeat("ab", 32)
	activeProject := newProject("active")
	activeKeyID := insertReceiver(
		activeProject,
		APIKeyStatusFeedActive,
		activeToken,
		"00112233445566778899aabbccddeeff",
	)
	feedHandler := CompletedExecutionFeedAuth(
		pool,
		"receiver-auth-secret",
		mustClientIPResolver(t),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, activeKeyID, GetKeyIDFromContext(r.Context()))
		require.Equal(t, activeProject, GetProjectIDFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))
	require.Equal(t, http.StatusOK, request(feedHandler, activeToken).Code)
	require.Equal(t, http.StatusUnauthorized, request(
		feedHandler,
		auth.ReceiverFeedTokenPrefix+strings.Repeat("ac", 32),
	).Code)

	malformedProject := newProject("malformed")
	_, err := pool.Exec(ctx,
		`INSERT INTO api_keys (
		    org_id, project_id, key_type, name, key_hash, key_prefix, status,
		    rpm_limit, qps_limit, allowed_ips, allowed_models, purpose,
		    verifier_scheme, registration_operation_id
		 ) VALUES (
		    $1, $2, 'service', 'malformed receiver sentinel', '', '',
		    'feed_active', 60, 1, '127.0.0.1/32', '', 'xinsight.feed.read',
		    'receiver_sha256_v1', '102132435465768798a9bacbdcedfe0f'
		 )`,
		orgID, malformedProject,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, request(feedHandler, "not-a-receiver-token").Code)

	gatewayHandler := GatewayAuthWithAgentCapabilitiesAndClientIP(
		pool,
		"receiver-auth-secret",
		nil,
		mustClientIPResolver(t),
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	gatewayRequest := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	gatewayRequest.Header.Set("Authorization", "Bearer "+activeToken)
	gatewayRequest.RemoteAddr = "127.0.0.1:44221"
	gatewayResponse := httptest.NewRecorder()
	gatewayHandler.ServeHTTP(gatewayResponse, gatewayRequest)
	require.Equal(t, http.StatusUnauthorized, gatewayResponse.Code)

	_, err = pool.Exec(ctx,
		`UPDATE api_keys SET key_prefix = 'ur-xf1-fedcba9876543' WHERE id = $1`,
		activeKeyID,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, request(feedHandler, activeToken).Code)

	legacyRaw := "ur-0001-0123456789abcdef0123456789abcdef"
	legacyProject := newProject("legacy")
	_, err = pool.Exec(ctx,
		`INSERT INTO api_keys (
		    org_id, project_id, key_type, name, key_hash, key_prefix, status,
		    rpm_limit, qps_limit, allowed_ips, allowed_models, purpose
		 ) VALUES (
		    $1, $2, 'service', 'legacy feed', $3, 'ur-0001-0123456789ab',
		    'feed_active', 60, 1, '127.0.0.1/32', '', 'xinsight.feed.read'
		 )`,
		orgID, legacyProject, auth.HashAPIKey(legacyRaw, "receiver-auth-secret"),
	)
	require.NoError(t, err)
	legacyHandler := CompletedExecutionFeedAuth(
		pool,
		"receiver-auth-secret",
		mustClientIPResolver(t),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, legacyProject, GetProjectIDFromContext(r.Context()))
		w.WriteHeader(http.StatusOK)
	}))
	require.Equal(t, http.StatusOK, request(legacyHandler, legacyRaw).Code)

	stagedToken := auth.ReceiverFeedTokenPrefix + strings.Repeat("cd", 32)
	stagedProject := newProject("staged")
	insertReceiver(
		stagedProject,
		APIKeyStatusFeedStaged,
		stagedToken,
		"ffeeddccbbaa99887766554433221100",
	)
	stagedHandler := CompletedExecutionFeedAuth(
		pool,
		"receiver-auth-secret",
		mustClientIPResolver(t),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, APIKeyStatusFeedStaged, GetAPIKeyFromContext(r.Context()).Status)
		http.Error(w, "paused", http.StatusServiceUnavailable)
	}))
	require.Equal(t, http.StatusServiceUnavailable, request(stagedHandler, stagedToken).Code)

	t.Logf("receiver=%d legacy_project=%d staged_project=%d", activeKeyID, legacyProject, stagedProject)
}
