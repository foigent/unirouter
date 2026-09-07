//go:build integration

package admin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelDiscoveryOwnedBindingReadOnlyIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('discovery-customer','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,organization_role,role,status) VALUES($1,'discovery-admin','fixture','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))
	h := NewHandlerWithAPIKeyHashSecret(pool, "discovery-jwt", "discovery-hmac", []byte("0123456789abcdef01234567"), nil)
	authorized := canonicalApplicationAuthorizedRequest(orgID, actorID)
	input := canonicalIdempotencyCreateRequest("discovery", "fixture-discovery-secret")
	input.Connection.Provider = "dashscope"
	input.Connection.BaseURL = "https://llm-test123.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
	created := createCanonicalApplicationForTest(t, h, authorized, input, http.StatusCreated)
	require.Positive(t, created.SelectedCredentialBindingID)
	application := created.Application
	binding := created.SelectedCredentialBindingID
	path := fmt.Sprintf("/api/v1/model-deployments/%d/model-discovery?credential_binding_id=%d&connection_revision=%d&credential_revision=1&page=1", application.Deployment.ID, binding, application.Connection.ConfigRevision)
	calls := 0
	revoke := false
	h.SetModelDeploymentProbeHTTPDoer(canonicalOperationProbeDoerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/v1/models", r.URL.Path)
		require.Equal(t, "Bearer fixture-discovery-secret", r.Header.Get("Authorization"))
		if revoke {
			_, err := pool.Exec(ctx, `UPDATE provider_credential_bindings SET status='disabled',state_revision=state_revision+1 WHERE id=$1`, binding)
			require.NoError(t, err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(discoveryFixture)), Header: make(http.Header)}, nil
	}))
	invoke := func(request *http.Request) *httptest.ResponseRecorder {
		request.SetPathValue("deploymentID", fmt.Sprint(application.Deployment.ID))
		w := httptest.NewRecorder()
		h.DiscoverCanonicalModels(w, request)
		return w
	}
	counts := func() string {
		var result string
		require.NoError(t, pool.QueryRow(ctx, `SELECT json_build_array((SELECT count(*) FROM model_deployment_create_receipts),(SELECT count(*) FROM model_deployment_probes),(SELECT count(*) FROM model_capability_probes),(SELECT count(*) FROM provider_credential_bindings),(SELECT count(*) FROM audit_logs))::text`).Scan(&result))
		return result
	}
	before := counts()
	w := invoke(authorized(http.MethodGet, path, ""))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, before, counts())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.NotContains(t, w.Body.String(), "fixture-discovery-secret")
	foreign := canonicalApplicationAuthorizedRequest(orgID+100000, actorID)
	w = invoke(foreign(http.MethodGet, path, ""))
	require.Equal(t, 404, w.Code, w.Body.String())
	require.Equal(t, 1, calls)
	w = invoke(authorized(http.MethodGet, strings.Replace(path, "credential_revision=1", "credential_revision=2", 1), ""))
	require.Equal(t, 409, w.Code)
	require.Equal(t, 1, calls)
	revoke = true
	w = invoke(authorized(http.MethodGet, path, ""))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Equal(t, 2, calls)
	w = invoke(authorized(http.MethodGet, path, ""))
	require.Equal(t, 409, w.Code)
	require.Equal(t, 2, calls)
}
