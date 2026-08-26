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
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestApprovedCatalogCanonicalStableManifestIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	_, err := pool.Exec(ctx, `UPDATE orgs SET org_type='customer',parent_id=NULL,status='active' WHERE id=$1`, orgID)
	require.NoError(t, err)
	var actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'catalog-canonical-admin','fixture','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))
	encryptionKey := []byte("0123456789abcdef01234567")
	probeFixture := insertCanonicalOperationProbeFixture(t, pool, orgID, encryptionKey, "catalog-canonical")
	modelID, modelName, route := makeCanonicalLifecycleRouteReady(t, pool, probeFixture)
	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	router.SetRuntimeResolver(manager)
	handler := NewHandlerWithAPIKeyHashSecret(pool, "fixture-jwt", "fixture-hash", encryptionKey, nil)
	handler.SetRuntimeChangeManager(manager)
	fixture := integrationfixture.AtomicRelease{
		PublicModelID: modelID, CustomerOrgID: orgID, ActorUserID: actorID, ActorOrgID: orgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{DeploymentID: route.DeploymentID, OperationID: route.ModelOperationID}},
		Reason:     "canonical project catalog regression",
	}
	release := integrationfixture.AuthorSealedDeploymentRelease(t, pool, manager, fixture)
	readCatalog := func(scopeOrgID int64, want int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/model-catalog?org_id=%d&routable_only=true", scopeOrgID), nil)
		request = platformModelGovernanceRequest(request)
		response := httptest.NewRecorder()
		handler.ListApprovedModelCatalog(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var result struct {
			Total int `json:"total"`
			Data  []struct {
				ID int64 `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.Equal(t, want, result.Total)
		require.Len(t, result.Data, want)
		if want > 0 {
			require.Equal(t, modelID, result.Data[0].ID)
		}
	}
	readCatalog(orgID, 0) // Sealed/validated does not mean assigned.
	integrationfixture.ActivateInitialDeploymentRelease(t, manager, fixture, release)
	readCatalog(orgID, 1)

	// A same-name catalog row in another tenant must not inherit a v2 slot.
	otherOrgID := testutil.NewTestOrg(t, pool)
	_, err = pool.Exec(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog)
VALUES ($1,$2,'same name, different identity','openai','ga',TRUE)`, otherOrgID, modelName)
	require.NoError(t, err)
	readCatalog(otherOrgID, 0)

	// Runtime membership never bypasses independent catalog approval/lifecycle.
	_, err = pool.Exec(ctx, `UPDATE model_registry SET approved_catalog=FALSE WHERE id=$1`, modelID)
	require.NoError(t, err)
	readCatalog(orgID, 0)
	_, err = pool.Exec(ctx, `UPDATE model_registry SET approved_catalog=TRUE,lifecycle='staging' WHERE id=$1`, modelID)
	require.NoError(t, err)
	readCatalog(orgID, 0)
}
