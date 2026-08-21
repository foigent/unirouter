//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/integrationfixture"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCanonicalDeploymentLifecycleHTTPApplicationIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	_, err := pool.Exec(ctx, `
UPDATE orgs
   SET status='active',org_type='customer',parent_id=NULL,updated_at=NOW()
 WHERE id=$1`, orgID)
	require.NoError(t, err)
	var actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'m76-http-admin','fixture','org_admin','org_admin','active')
RETURNING id`, orgID).Scan(&actorID))

	encryptionKey := []byte("0123456789abcdef01234567")
	fixture := insertCanonicalOperationProbeFixture(t, pool, orgID, encryptionKey, "m76-http")
	publicModelID, modelName, route := makeCanonicalLifecycleRouteReady(t, pool, fixture)
	policy := routingpolicy.Default()
	router, err := gateway.NewRouterWithPolicy(pool, policy)
	require.NoError(t, err)
	manager, err := runtimechange.NewManager(pool, router, policy)
	require.NoError(t, err)
	router.SetRuntimeResolver(manager)
	handler := NewHandlerWithAPIKeyHashSecret(pool, "m76-http-jwt", "m76-http-hash-secret", encryptionKey, nil)
	handler.SetRuntimeChangeManager(manager)
	authorized := canonicalApplicationAuthorizedRequest(orgID, actorID)

	initialOptions := serveCanonicalDeploymentReleaseCollection(t, handler, authorized, http.StatusOK)
	initialModel := canonicalDeploymentReleaseCollectionModel(t, initialOptions, publicModelID)
	require.True(t, initialModel.Authoring.StructurallyAvailable)
	require.NotNil(t, initialModel.Authoring.Baseline)
	require.Equal(t, runtimechange.DeploymentReleaseBaselineInitial, initialModel.Authoring.Baseline.Kind)
	require.NotEmpty(t, initialModel.SelectionOptions)
	initialAuthoring, initialAuthoringResponse := serveCanonicalDeploymentReleaseAuthoring(
		t, handler, authorized,
		canonicalDeploymentReleaseAuthoringRequest{
			PublicModelID: publicModelID, Baseline: *initialModel.Authoring.Baseline,
			Selections: []runtimechange.DeploymentReleaseSelection{{
				DeploymentID: route.DeploymentID, OperationID: route.ModelOperationID,
			}},
			Reason: "M78 initial canonical HTTP authoring",
		},
		"m78-http-author-initial", http.StatusCreated,
	)
	require.False(t, initialAuthoring.IdempotencyReplay)
	require.Equal(t, initialAuthoring.OriginalLocation, initialAuthoringResponse.Header().Get("Location"))
	initialReplay, initialReplayResponse := serveCanonicalDeploymentReleaseAuthoring(
		t, handler, authorized,
		canonicalDeploymentReleaseAuthoringRequest{
			PublicModelID: publicModelID, Baseline: *initialModel.Authoring.Baseline,
			Selections: []runtimechange.DeploymentReleaseSelection{{
				DeploymentID: route.DeploymentID, OperationID: route.ModelOperationID,
			}},
			Reason: "M78 initial canonical HTTP authoring",
		},
		"m78-http-author-initial", http.StatusOK,
	)
	require.True(t, initialReplay.IdempotencyReplay)
	require.Equal(t, "true", initialReplayResponse.Header().Get(middleware.IdempotencyReplayHeader))
	require.Equal(t, initialAuthoring.ServerOperationID, initialReplayResponse.Header().Get(middleware.OriginalOperationHeader))
	initialReplay.IdempotencyReplay = false
	require.Equal(t, initialAuthoring, initialReplay, "authoring replay must freeze exact receipt facts")
	initial := initialAuthoring.Release
	readBefore := serveCanonicalLifecycleRead(t, handler, authorized, initial.ReleaseRef, http.StatusOK)
	require.Equal(t, initial.ReleaseRef, readBefore.Release.ReleaseRef)
	require.False(t, readBefore.Slot.Exists)
	require.True(t, lifecycleActionDescriptor(t, readBefore.Actions, runtimechange.DeploymentLifecycleActivateInitial).StructurallyAvailable)

	activateBody := fmt.Sprintf(`{"expected_release_version":%d,"reason":"initial HTTP activation"}`, initial.Version)
	activated, activateResponse := serveCanonicalLifecycleMutation(
		t, handler, authorized, initial.ReleaseRef, "activate-initial", activateBody,
		"m76-http-activate", handler.ActivateInitialCanonicalDeploymentLifecycle, http.StatusOK,
	)
	require.False(t, activated.IdempotencyReplay)
	require.Equal(t, initial.ID, *activated.Slot.AfterStableReleaseID)
	require.Equal(t, int64(1), activated.Slot.AfterVersion)
	require.Equal(t, activateResponse.Header().Get(middleware.OperationIDHeader), activated.ServerOperationID)

	replayed, replayResponse := serveCanonicalLifecycleMutation(
		t, handler, authorized, initial.ReleaseRef, "activate-initial", activateBody,
		"m76-http-activate", handler.ActivateInitialCanonicalDeploymentLifecycle, http.StatusOK,
	)
	require.True(t, replayed.IdempotencyReplay)
	require.Equal(t, "true", replayResponse.Header().Get(middleware.IdempotencyReplayHeader))
	require.Equal(t, activated.ServerOperationID, replayResponse.Header().Get(middleware.OriginalOperationHeader))
	require.NotEqual(t, replayResponse.Header().Get(middleware.OperationIDHeader), activated.ServerOperationID)
	replayed.IdempotencyReplay = false
	require.Equal(t, activated, replayed, "replay must freeze every transition-time fact except the replay marker")

	_, conflictResponse := serveCanonicalLifecycleMutation(
		t, handler, authorized, initial.ReleaseRef, "activate-initial",
		fmt.Sprintf(`{"expected_release_version":%d,"reason":"different payload"}`, initial.Version),
		"m76-http-activate", handler.ActivateInitialCanonicalDeploymentLifecycle, http.StatusConflict,
	)
	require.Contains(t, conflictResponse.Body.String(), "canonical_deployment_lifecycle_idempotency_conflict")

	stableOptions := serveCanonicalDeploymentReleaseCollection(t, handler, authorized, http.StatusOK)
	stableModel := canonicalDeploymentReleaseCollectionModel(t, stableOptions, publicModelID)
	require.True(t, stableModel.Authoring.StructurallyAvailable)
	require.NotNil(t, stableModel.Authoring.Baseline)
	require.Equal(t, runtimechange.DeploymentReleaseBaselineStable, stableModel.Authoring.Baseline.Kind)
	require.Equal(t, activated.Slot.AfterVersion, stableModel.Authoring.Baseline.ExpectedSlotVersion)
	firstCanaryAuthoring, _ := serveCanonicalDeploymentReleaseAuthoring(
		t, handler, authorized,
		canonicalDeploymentReleaseAuthoringRequest{
			PublicModelID: publicModelID, Baseline: *stableModel.Authoring.Baseline,
			Selections: []runtimechange.DeploymentReleaseSelection{{
				DeploymentID: route.DeploymentID, OperationID: route.ModelOperationID,
			}},
			Reason: "M78 stable child canonical HTTP authoring",
		},
		"m78-http-author-stable-child", http.StatusCreated,
	)
	firstCanary := firstCanaryAuthoring.Release
	startedForCancel, _ := serveCanonicalLifecycleMutation(
		t, handler, authorized, firstCanary.ReleaseRef, "start-canary",
		fmt.Sprintf(`{"expected_release_version":%d,"expected_slot_version":%d,"canary_basis_points":500}`,
			firstCanary.Version, activated.Slot.AfterVersion),
		"m76-http-start-cancel", handler.StartCanonicalDeploymentCanary, http.StatusOK,
	)
	firstCanary = mustAdminDeploymentRelease(t, manager, firstCanary.ReleaseRef)
	cancelled, _ := serveCanonicalLifecycleMutation(
		t, handler, authorized, firstCanary.ReleaseRef, "cancel-canary",
		fmt.Sprintf(`{"expected_release_version":%d,"expected_slot_version":%d,"reason":"operator contraction"}`,
			firstCanary.Version, startedForCancel.Slot.AfterVersion),
		"m76-http-cancel", handler.CancelCanonicalDeploymentCanary, http.StatusOK,
	)
	require.Equal(t, initial.ID, *cancelled.Slot.AfterStableReleaseID)
	require.Nil(t, cancelled.Slot.AfterCanaryReleaseID)

	secondCanary := prepareCanonicalLifecycleRelease(
		t, pool, manager, publicModelID, route, "rcr_m76_http_promote", actorID, orgID,
	)
	startedForPromote, _ := serveCanonicalLifecycleMutation(
		t, handler, authorized, secondCanary.ReleaseRef, "start-canary",
		fmt.Sprintf(`{"expected_release_version":%d,"expected_slot_version":%d,"canary_basis_points":750}`,
			secondCanary.Version, cancelled.Slot.AfterVersion),
		"m76-http-start-promote", handler.StartCanonicalDeploymentCanary, http.StatusOK,
	)
	secondCanary = mustAdminDeploymentRelease(t, manager, secondCanary.ReleaseRef)
	promoted, _ := serveCanonicalLifecycleMutation(
		t, handler, authorized, secondCanary.ReleaseRef, "promote",
		fmt.Sprintf(`{"expected_release_version":%d,"expected_slot_version":%d}`,
			secondCanary.Version, startedForPromote.Slot.AfterVersion),
		"m76-http-promote", handler.PromoteCanonicalDeploymentCanary, http.StatusOK,
	)
	require.Equal(t, secondCanary.ID, *promoted.Slot.AfterStableReleaseID)

	secondCanary = mustAdminDeploymentRelease(t, manager, secondCanary.ReleaseRef)
	rolledBack, _ := serveCanonicalLifecycleMutation(
		t, handler, authorized, secondCanary.ReleaseRef, "rollback-stable",
		fmt.Sprintf(`{"expected_release_version":%d,"expected_slot_version":%d}`,
			secondCanary.Version, promoted.Slot.AfterVersion),
		"m76-http-rollback", handler.RollbackCanonicalDeploymentStable, http.StatusOK,
	)
	require.Equal(t, initial.ID, *rolledBack.Slot.AfterStableReleaseID)
	require.Nil(t, rolledBack.Slot.AfterCanaryReleaseID)

	after := serveCanonicalLifecycleRead(t, handler, authorized, secondCanary.ReleaseRef, http.StatusOK)
	require.Equal(t, "rolled_back", after.Release.Stage)
	require.Equal(t, initial.ID, *after.Slot.StableReleaseID)
	require.Equal(t, modelName, after.Slot.ModelName)

	var foreignOrgID, foreignUserID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type) VALUES ('m76-http-foreign','active','customer') RETURNING id`).Scan(&foreignOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'m76-http-foreign-admin','fixture','org_admin','org_admin','active') RETURNING id`, foreignOrgID).Scan(&foreignUserID))
	serveCanonicalLifecycleRead(
		t, handler, canonicalApplicationAuthorizedRequest(foreignOrgID, foreignUserID),
		initial.ReleaseRef, http.StatusNotFound,
	)

	var receiptCount, auditCount, expectedEventCount, linkedEventCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND status='completed'`, orgID).Scan(&receiptCount))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM audit_logs
 WHERE org_id=$1 AND action LIKE 'model_deployment.lifecycle.%'`, orgID).Scan(&auditCount))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COALESCE(SUM(CASE WHEN action IN ('promote','rollback_stable') THEN 2 ELSE 1 END),0)
  FROM model_deployment_lifecycle_receipts
 WHERE customer_org_id=$1 AND status='completed'`, orgID).Scan(&expectedEventCount))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM runtime_change_events event
  JOIN model_deployment_lifecycle_receipts receipt
    ON event.detail->>'lifecycle_receipt_id'=receipt.receipt_id::TEXT
 WHERE receipt.customer_org_id=$1 AND receipt.status='completed'`, orgID).Scan(&linkedEventCount))
	require.Equal(t, 6, receiptCount)
	require.Equal(t, receiptCount, auditCount)
	require.Equal(t, 8, expectedEventCount)
	require.Equal(t, expectedEventCount, linkedEventCount)
}

func makeCanonicalLifecycleRouteReady(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture canonicalOperationProbeFixture,
) (int64, string, runtimechange.DeploymentRoute) {
	t.Helper()
	ctx := context.Background()
	var publicModelID int64
	var modelName, upstreamModel string
	var connectionRevision, deploymentRevision, operationRevision, credentialRevision int64
	require.NoError(t, pool.QueryRow(ctx, `
SELECT deployment.public_model_id,registry.model_name,deployment.upstream_model,
       connection.config_revision,deployment.config_revision,operation.revision,binding.state_revision
  FROM model_deployments deployment
  JOIN model_registry registry ON registry.id=deployment.public_model_id
  JOIN provider_connections connection ON connection.id=deployment.connection_id
  JOIN model_deployment_operations operation
    ON operation.deployment_id=deployment.id AND operation.operation_id=$2
  JOIN provider_credential_bindings binding ON binding.id=$3
 WHERE deployment.id=$1`, fixture.DeploymentID, modeloperation.IDChatCompletions, fixture.BindingID).Scan(
		&publicModelID, &modelName, &upstreamModel, &connectionRevision,
		&deploymentRevision, &operationRevision, &credentialRevision,
	))
	require.NoError(t, pool.QueryRow(ctx, `
UPDATE model_deployment_operations
   SET evidence_state='live_probed',evidence_ref='integration:m76-http',verified_at=NOW(),
       revision=revision+1,updated_at=NOW()
 WHERE deployment_id=$1 AND operation_id=$2
 RETURNING revision`, fixture.DeploymentID, modeloperation.IDChatCompletions).Scan(&operationRevision))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,reported_model,connection_revision,deployment_revision,
  credential_binding_id,credential_state_revision,success,status_code,latency_ms,
  capabilities_verified,request_summary,response_summary,source_kind,source_ref,
  source_fingerprint,tested_by,tested_at)
SELECT $1,$2,$3,'openai.chat_completions','1.0.0',$4,$4,$5,$6,$7,$8,
	       TRUE,200,9,'[]','{}','{}','server_provider_probe','m76-http-ready',
       'sha256:7676767676767676767676767676767676767676767676767676767676767676',
       users.id,NOW()
  FROM users
 WHERE users.org_id=(SELECT customer_org_id FROM model_deployments WHERE id=$1)
 ORDER BY users.id
 LIMIT 1`, fixture.DeploymentID, fixture.ConnectionID, modeloperation.IDChatCompletions,
		upstreamModel, connectionRevision, deploymentRevision, fixture.BindingID, credentialRevision)
	require.NoError(t, err)
	return publicModelID, modelName, runtimechange.DeploymentRoute{
		DeploymentID: fixture.DeploymentID, ModelOperationID: string(modeloperation.IDChatCompletions),
		ExpectedConnectionRevision: connectionRevision,
		ExpectedDeploymentRevision: deploymentRevision,
		ExpectedOperationRevision:  operationRevision,
		ProtocolProfileID:          "openai.chat_completions",
		ProtocolProfileVersion:     "1.0.0",
		Admission:                  "allow",
	}
}

func prepareCanonicalLifecycleRelease(
	t *testing.T,
	pool *pgxpool.Pool,
	manager *runtimechange.Manager,
	publicModelID int64,
	route runtimechange.DeploymentRoute,
	releaseRef string,
	actorID, orgID int64,
) runtimechange.DeploymentRelease {
	t.Helper()
	return integrationfixture.AuthorSealedDeploymentRelease(t, pool, manager, integrationfixture.AtomicRelease{
		PublicModelID: publicModelID,
		CustomerOrgID: orgID,
		ActorUserID:   actorID,
		ActorOrgID:    orgID,
		Selections: []runtimechange.DeploymentReleaseSelection{{
			DeploymentID: route.DeploymentID,
			OperationID:  route.ModelOperationID,
		}},
		Reason: "M76 canonical HTTP lifecycle integration: " + releaseRef,
	})
}

func serveCanonicalDeploymentReleaseCollection(
	t *testing.T,
	handler *Handler,
	authorized func(string, string, string) *http.Request,
	wantStatus int,
) canonicalDeploymentReleaseCollectionView {
	t.Helper()
	request := authorized(http.MethodGet, "/api/v1/model-deployment-releases", "")
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(
		http.HandlerFunc(handler.ListCanonicalDeploymentReleases),
	).ServeHTTP(response, request)
	require.Equal(t, wantStatus, response.Code, response.Body.String())
	var view canonicalDeploymentReleaseCollectionView
	if wantStatus == http.StatusOK {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	}
	return view
}

func canonicalDeploymentReleaseCollectionModel(
	t *testing.T,
	view canonicalDeploymentReleaseCollectionView,
	publicModelID int64,
) canonicalDeploymentReleaseAuthoringModelView {
	t.Helper()
	for _, model := range view.Models {
		if model.PublicModel.ID == publicModelID {
			return model
		}
	}
	t.Fatalf("missing deployment release authoring model %d", publicModelID)
	return canonicalDeploymentReleaseAuthoringModelView{}
}

func serveCanonicalDeploymentReleaseAuthoring(
	t *testing.T,
	handler *Handler,
	authorized func(string, string, string) *http.Request,
	body canonicalDeploymentReleaseAuthoringRequest,
	idempotencyKey string,
	wantStatus int,
) (runtimechange.DeploymentReleaseAuthoringReceipt, *httptest.ResponseRecorder) {
	t.Helper()
	wire, err := json.Marshal(body)
	require.NoError(t, err)
	request := authorized(http.MethodPost, "/api/v1/model-deployment-releases", string(wire))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.IdempotencyKeyHeader, idempotencyKey)
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(
		http.HandlerFunc(handler.AuthorCanonicalDeploymentRelease),
	).ServeHTTP(response, request)
	require.Equal(t, wantStatus, response.Code, response.Body.String())
	var receipt runtimechange.DeploymentReleaseAuthoringReceipt
	if wantStatus == http.StatusCreated || wantStatus == http.StatusOK {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
	}
	return receipt, response
}

func serveCanonicalLifecycleMutation(
	t *testing.T,
	handler *Handler,
	authorized func(string, string, string) *http.Request,
	releaseRef, actionPath, body, idempotencyKey string,
	serve http.HandlerFunc,
	wantStatus int,
) (runtimechange.DeploymentLifecycleReceipt, *httptest.ResponseRecorder) {
	t.Helper()
	target := "/api/v1/model-deployment-releases/" + releaseRef + "/" + actionPath
	request := authorized(http.MethodPost, target, body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.IdempotencyKeyHeader, idempotencyKey)
	request.SetPathValue("releaseRef", releaseRef)
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(serve).ServeHTTP(response, request)
	require.Equal(t, wantStatus, response.Code, response.Body.String())
	var receipt runtimechange.DeploymentLifecycleReceipt
	if wantStatus == http.StatusOK {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &receipt))
	}
	return receipt, response
}

func serveCanonicalLifecycleRead(
	t *testing.T,
	handler *Handler,
	authorized func(string, string, string) *http.Request,
	releaseRef string,
	wantStatus int,
) canonicalDeploymentLifecycleView {
	t.Helper()
	request := authorized(http.MethodGet, "/api/v1/model-deployment-releases/"+releaseRef, "")
	request.SetPathValue("releaseRef", releaseRef)
	response := httptest.NewRecorder()
	middleware.OperationIdentityWithSecret(handler.apiKeyHashSecret)(
		http.HandlerFunc(handler.GetCanonicalDeploymentLifecycle),
	).ServeHTTP(response, request)
	require.Equal(t, wantStatus, response.Code, response.Body.String())
	var view canonicalDeploymentLifecycleView
	if wantStatus == http.StatusOK {
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	}
	return view
}

func mustAdminDeploymentRelease(
	t *testing.T,
	manager *runtimechange.Manager,
	releaseRef string,
) runtimechange.DeploymentRelease {
	t.Helper()
	release, err := manager.GetDeploymentRelease(context.Background(), releaseRef)
	require.NoError(t, err)
	return release
}
