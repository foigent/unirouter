//go:build integration

package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		 VALUES ($1, 'completed-feed-platform-admin', 'test-hash', 'platform_admin', 'active')
		 RETURNING id`,
		orgID,
	).Scan(&actorID))

	handler := NewHandler(pool, "completed-feed-admin-secret", []byte("0123456789abcdef01234567"), nil)
	handler.SetCompletedFeedController(completedFeedControllerFixture{})
	genericCreateResponse := httptest.NewRecorder()
	handler.CreateAPIKey(
		genericCreateResponse,
		apiKeyIntegrationRequest(t, pool,
			http.MethodPost,
			"/api/v1/keys",
			fmt.Sprintf(
				`{"org_id":%d,"project_id":%d,"name":"forbidden generic feed","key_type":"service","allowed_ips":"127.0.0.1/32","purpose":"%s"}`,
				orgID, projectID, middleware.APIKeyPurposeXInsightFeedRead,
			),
			actorID,
			orgID,
			auth.RoleOrgAdmin,
			"completed-feed-generic-create-denied",
		),
	)
	require.Equal(t, http.StatusBadRequest, genericCreateResponse.Code, genericCreateResponse.Body.String())

	createRequest := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/credentials", projectID),
		xInsightFeedCreateBody(t, "lifecycle-initial", "127.0.0.1/32"),
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-key-create",
	)
	createRequest.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	createResponse := httptest.NewRecorder()
	handler.CreateXInsightFeedCredential(createResponse, createRequest)
	require.Equal(t, http.StatusCreated, createResponse.Code, createResponse.Body.String())

	var created map[string]any
	require.NoError(t, json.Unmarshal(createResponse.Body.Bytes(), &created))
	keyID := int64(created["id"].(float64))
	require.Equal(t, middleware.APIKeyPurposeXInsightFeedRead, created["purpose"])
	require.NotContains(t, created, "key")
	require.NotContains(t, createResponse.Body.String(), "verifier_sha256")

	var storedStatus, storedPurpose, storedModels, storedScheme, storedOperation, storedHash string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status, purpose, allowed_models, verifier_scheme,
		        registration_operation_id, key_hash
		 FROM api_keys WHERE id = $1`,
		keyID,
	).Scan(
		&storedStatus,
		&storedPurpose,
		&storedModels,
		&storedScheme,
		&storedOperation,
		&storedHash,
	))
	require.Equal(t, middleware.APIKeyStatusFeedStaged, storedStatus)
	require.Equal(t, middleware.APIKeyPurposeXInsightFeedRead, storedPurpose)
	require.Empty(t, storedModels)
	require.Equal(t, auth.ReceiverFeedVerifierScheme, storedScheme)
	initialRegistration, _ := testXInsightFeedRegistration(t, "lifecycle-initial")
	require.Equal(t, initialRegistration.OperationID, storedOperation)
	decodedVerifier, err := auth.DecodeReceiverFeedVerifier(initialRegistration.VerifierSHA256)
	require.NoError(t, err)
	require.Equal(t, auth.HashReceiverFeedVerifier(decodedVerifier, "completed-feed-admin-secret"), storedHash)

	genericUpdate := apiKeyIntegrationRequest(t, pool,
		http.MethodPut,
		fmt.Sprintf("/api/v1/keys/%d", keyID),
		`{"status":"active"}`,
		actorID,
		orgID,
		auth.RoleOrgAdmin,
		"completed-feed-generic-update",
	)
	genericUpdate.SetPathValue("id", strconv.FormatInt(keyID, 10))
	genericUpdateResponse := httptest.NewRecorder()
	handler.UpdateAPIKey(genericUpdateResponse, genericUpdate)
	require.Equal(t, http.StatusConflict, genericUpdateResponse.Code, genericUpdateResponse.Body.String())

	genericDisable := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/keys/%d/disable", keyID),
		"",
		actorID,
		orgID,
		auth.RoleOrgAdmin,
		"completed-feed-generic-disable",
	)
	genericDisable.SetPathValue("id", strconv.FormatInt(keyID, 10))
	genericDisableResponse := httptest.NewRecorder()
	handler.DisableAPIKey(genericDisableResponse, genericDisable)
	require.Equal(t, http.StatusConflict, genericDisableResponse.Code, genericDisableResponse.Body.String())

	activateRequest := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/credentials/activate", projectID),
		fmt.Sprintf(`{"staged_credential_id":%d}`, keyID),
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-key-activate",
	)
	activateRequest.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	activateResponse := httptest.NewRecorder()
	handler.ActivateInitialXInsightFeedCredential(activateResponse, activateRequest)
	require.Equal(t, http.StatusOK, activateResponse.Code, activateResponse.Body.String())
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM api_keys WHERE id = $1`,
		keyID,
	).Scan(&storedStatus))
	require.Equal(t, middleware.APIKeyStatusFeedActive, storedStatus)

	resumeRequest := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/resume", projectID),
		"",
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-resume",
	)
	resumeRequest.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	resumeResponse := httptest.NewRecorder()
	handler.ResumeXInsightFeedConnection(resumeResponse, resumeRequest)
	require.Equal(t, http.StatusOK, resumeResponse.Code, resumeResponse.Body.String())

	pauseRequest := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/pause", projectID),
		"",
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-pause",
	)
	pauseRequest.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	pauseResponse := httptest.NewRecorder()
	handler.PauseXInsightFeedConnection(pauseResponse, pauseRequest)
	require.Equal(t, http.StatusOK, pauseResponse.Code, pauseResponse.Body.String())

	prepareRequest := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/rotation/prepare", projectID),
		xInsightFeedRotationBody(t, "lifecycle-rotation-one"),
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-rotation-prepare",
	)
	prepareRequest.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	prepareResponse := httptest.NewRecorder()
	handler.PrepareXInsightFeedRotation(prepareResponse, prepareRequest)
	require.Equal(t, http.StatusCreated, prepareResponse.Code, prepareResponse.Body.String())
	var replacement map[string]any
	require.NoError(t, json.Unmarshal(prepareResponse.Body.Bytes(), &replacement))
	replacementID := int64(replacement["id"].(float64))

	var activeCount, stagedCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE status = 'feed_active'),
		        COUNT(*) FILTER (WHERE status = 'feed_staged')
		 FROM api_keys
		 WHERE org_id = $1 AND project_id = $2 AND purpose = 'xinsight.feed.read'`,
		orgID, projectID,
	).Scan(&activeCount, &stagedCount))
	require.Equal(t, 1, activeCount, "old credential must remain active during prepare")
	require.Equal(t, 1, stagedCount, "one staged overlap is required during installation")

	revokeOldDuringRotation := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/credentials/%d/revoke", projectID, keyID),
		"",
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-revoke-old-during-rotation",
	)
	revokeOldDuringRotation.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	revokeOldDuringRotation.SetPathValue("credential_id", strconv.FormatInt(keyID, 10))
	revokeOldDuringRotationResponse := httptest.NewRecorder()
	handler.RevokeXInsightFeedCredential(revokeOldDuringRotationResponse, revokeOldDuringRotation)
	require.Equal(t, http.StatusConflict, revokeOldDuringRotationResponse.Code, revokeOldDuringRotationResponse.Body.String())
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, keyID).Scan(&storedStatus))
	require.Equal(t, middleware.APIKeyStatusFeedActive, storedStatus, "rotation overlap must not expose an old-key 401 window")

	rotationActivate := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/rotation/activate", projectID),
		fmt.Sprintf(`{"staged_credential_id":%d}`, replacementID),
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-rotation-activate",
	)
	rotationActivate.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	rotationResponse := httptest.NewRecorder()
	handler.ActivateXInsightFeedRotation(rotationResponse, rotationActivate)
	require.Equal(t, http.StatusOK, rotationResponse.Code, rotationResponse.Body.String())

	var oldStatus, replacementStatus, connectionStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, keyID).Scan(&oldStatus))
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, replacementID).Scan(&replacementStatus))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM completed_execution_feed_connections WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID,
	).Scan(&connectionStatus))
	require.Equal(t, middleware.APIKeyStatusFeedRevoked, oldStatus)
	require.Equal(t, middleware.APIKeyStatusFeedActive, replacementStatus)
	require.Equal(t, xInsightFeedConnectionPaused, connectionStatus, "rotation never auto-resumes")

	statusRequest := apiKeyIntegrationRequest(t, pool,
		http.MethodGet,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d", projectID),
		"",
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-status",
	)
	statusRequest.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	statusResponse := httptest.NewRecorder()
	handler.GetXInsightFeedConnection(statusResponse, statusRequest)
	require.Equal(t, http.StatusOK, statusResponse.Code, statusResponse.Body.String())
	var controlStatus map[string]any
	require.NoError(t, json.Unmarshal(statusResponse.Body.Bytes(), &controlStatus))
	require.Equal(t, "active", controlStatus["project_status"])
	require.Equal(t, "paused", controlStatus["connection_status"])
	require.Equal(t, "next_requested_cursor_after_receiver_persistence", controlStatus["receiver_acknowledgement_semantics"])
	require.Equal(t, float64(0), controlStatus["pending_item_count"])
	activeCredential := controlStatus["active_credential"].(map[string]any)
	require.Equal(t, float64(replacementID), activeCredential["id"])
	require.Equal(t, auth.ReceiverFeedVerifierScheme, activeCredential["verifier_scheme"])
	rotationRegistration, _ := testXInsightFeedRegistration(t, "lifecycle-rotation-one")
	require.Equal(t, rotationRegistration.OperationID, activeCredential["operation_id"])
	require.Nil(t, controlStatus["staged_credential"])

	oldReactivate := apiKeyIntegrationRequest(t, pool,
		http.MethodPut,
		fmt.Sprintf("/api/v1/keys/%d", keyID),
		`{"status":"active"}`,
		actorID,
		orgID,
		auth.RoleOrgAdmin,
		"completed-feed-old-reactivate",
	)
	oldReactivate.SetPathValue("id", strconv.FormatInt(keyID, 10))
	oldReactivateResponse := httptest.NewRecorder()
	handler.UpdateAPIKey(oldReactivateResponse, oldReactivate)
	require.Equal(t, http.StatusConflict, oldReactivateResponse.Code)
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, keyID).Scan(&oldStatus))
	require.Equal(t, middleware.APIKeyStatusFeedRevoked, oldStatus, "terminal credential cannot be reactivated")

	secondPrepareRequest := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/rotation/prepare", projectID),
		xInsightFeedRotationBody(t, "lifecycle-rotation-two"),
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-second-rotation-prepare",
	)
	secondPrepareRequest.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	secondPrepareResponse := httptest.NewRecorder()
	handler.PrepareXInsightFeedRotation(secondPrepareResponse, secondPrepareRequest)
	require.Equal(t, http.StatusCreated, secondPrepareResponse.Code, secondPrepareResponse.Body.String())
	var cancelledReplacement map[string]any
	require.NoError(t, json.Unmarshal(secondPrepareResponse.Body.Bytes(), &cancelledReplacement))
	cancelledID := int64(cancelledReplacement["id"].(float64))

	cancelRequest := apiKeyIntegrationRequest(t, pool,
		http.MethodPost,
		fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/rotation/cancel", projectID),
		fmt.Sprintf(`{"staged_credential_id":%d}`, cancelledID),
		actorID,
		orgID,
		auth.RolePlatformAdmin,
		"completed-feed-rotation-cancel",
	)
	cancelRequest.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
	cancelResponse := httptest.NewRecorder()
	handler.CancelXInsightFeedRotation(cancelResponse, cancelRequest)
	require.Equal(t, http.StatusOK, cancelResponse.Code, cancelResponse.Body.String())
	var cancelledStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, cancelledID).Scan(&cancelledStatus))
	require.Equal(t, middleware.APIKeyStatusFeedRevoked, cancelledStatus)
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM api_keys WHERE id = $1`, replacementID).Scan(&replacementStatus))
	require.Equal(t, middleware.APIKeyStatusFeedActive, replacementStatus, "cancelling staged rotation must preserve old active credential")

	var auditedActions int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT action)
		 FROM audit_logs
		 WHERE project_id = $1
		   AND action IN (
		     'data_exchange.xinsight.credential.create',
		     'data_exchange.xinsight.credential.activate',
		     'data_exchange.xinsight.connection.resume',
		     'data_exchange.xinsight.connection.pause',
		     'data_exchange.xinsight.rotation.prepare',
		     'data_exchange.xinsight.rotation.activate',
		     'data_exchange.xinsight.rotation.cancel'
		   )`,
		projectID,
	).Scan(&auditedActions))
	require.Equal(t, 7, auditedActions)

	var createAuditDetails string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT detail::text
		 FROM audit_logs
		 WHERE project_id = $1
		   AND action = 'data_exchange.xinsight.credential.create'
		 ORDER BY id DESC
		 LIMIT 1`,
		projectID,
	).Scan(&createAuditDetails))
	_, initialRawToken := testXInsightFeedRegistration(t, "lifecycle-initial")
	require.Contains(t, createAuditDetails, auth.ReceiverFeedVerifierScheme)
	require.Contains(t, createAuditDetails, initialRegistration.OperationID)
	require.NotContains(t, createAuditDetails, initialRegistration.VerifierSHA256)
	require.NotContains(t, createAuditDetails, initialRawToken)
	require.NotContains(t, createAuditDetails, storedHash)
}

type completedFeedControllerFixture struct{}

func (completedFeedControllerFixture) Enabled() bool          { return true }
func (completedFeedControllerFixture) SourceInstance() string { return "integration-source" }
func (completedFeedControllerFixture) EncodeSequence(_ int64, sequence int64) (string, error) {
	return fmt.Sprintf("cursor-%d", sequence), nil
}

func testXInsightFeedRegistration(
	t *testing.T,
	label string,
) (xInsightFeedCredentialRegistrationV1, string) {
	t.Helper()
	tokenDigest := sha256.Sum256([]byte("receiver-token:" + label))
	rawToken := auth.ReceiverFeedTokenPrefix + fmt.Sprintf("%x", tokenDigest[:])
	verifier, err := auth.ReceiverFeedVerifierHex(rawToken)
	require.NoError(t, err)
	operation := sha256.Sum256([]byte("receiver-operation:" + label))
	return xInsightFeedCredentialRegistrationV1{
		SchemaVersion:  auth.ReceiverFeedRegistrationSchema,
		TokenFormat:    auth.ReceiverFeedTokenFormat,
		VerifierScheme: auth.ReceiverFeedVerifierScheme,
		KeyPrefix:      rawToken[:auth.ReceiverFeedCredentialPrefixLength],
		VerifierSHA256: verifier,
		OperationID:    fmt.Sprintf("%x", operation[:16]),
	}, rawToken
}

func xInsightFeedCreateBody(t *testing.T, label, allowedIP string) string {
	t.Helper()
	registration, _ := testXInsightFeedRegistration(t, label)
	body, err := json.Marshal(map[string]any{
		"allowed_ip":   allowedIP,
		"registration": registration,
	})
	require.NoError(t, err)
	return string(body)
}

func xInsightFeedRotationBody(t *testing.T, label string) string {
	t.Helper()
	registration, _ := testXInsightFeedRegistration(t, label)
	body, err := json.Marshal(map[string]any{"registration": registration})
	require.NoError(t, err)
	return string(body)
}
