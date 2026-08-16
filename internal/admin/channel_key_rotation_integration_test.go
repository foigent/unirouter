//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRotateChannelKeyProbesBeforeAtomicSwapAndPreservesOldCredentialOnFailure(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	orgID := testutil.NewTestOrg(t, pool)
	var userID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
VALUES ($1,'credential-rotation-owner','fixture','platform_admin','platform_admin','active')
RETURNING id`, orgID).Scan(&userID))

	var requestMu sync.Mutex
	seenAuthorization := ""
	seenModel := ""
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		requestMu.Lock()
		seenAuthorization = r.Header.Get("Authorization")
		seenModel = payload.Model
		requestMu.Unlock()
		if r.Header.Get("Authorization") == "Bearer rejected-replacement" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"replacement key rejected","code":"invalid_api_key"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"probe","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer provider.Close()

	encKey := []byte("0123456789abcdef01234567")
	oldEncrypted, err := auth.EncryptKey([]byte("old-active-key"), encKey)
	require.NoError(t, err)
	var channelID, routingEpoch, oldCredentialID, oldCredentialEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channels (
  name,provider,base_url,models,model_mapping,adapter_type,adapter_config,
  priority,routing_weight,degradation_threshold,status,circuit_state)
VALUES ('Provider active','provider-under-test',$1,'public-model',
  '{"public-model":"upstream/model"}','openai_compatible','{}',
  100,100,10,'active','healthy')
RETURNING id,routing_epoch`, provider.URL+"/v1").Scan(&channelID, &routingEpoch))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO channel_keys (channel_id,key_enc,allowed_models,status)
VALUES ($1,$2,'public-model','active')
RETURNING id,state_epoch`, channelID, oldEncrypted).Scan(&oldCredentialID, &oldCredentialEpoch))
	_, err = pool.Exec(ctx, `
INSERT INTO channel_model_probes (
  channel_id,public_model,upstream_model,routing_epoch,credential_id,
  credential_state_epoch,success,status_code,latency_ms,tested_by)
VALUES ($1,'public-model','upstream/model',$2,$3,$4,TRUE,200,8,$5)`,
		channelID, routingEpoch, oldCredentialID, oldCredentialEpoch, userID)
	require.NoError(t, err)

	handler := NewHandler(pool, "credential-rotation-jwt", encKey, nil)
	authorized := func(key string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/channel-keys/%d/rotate", oldCredentialID),
			strings.NewReader(fmt.Sprintf(`{"key":%q,"model":"public-model"}`, key)))
		request.SetPathValue("id", fmt.Sprintf("%d", oldCredentialID))
		requestContext := context.WithValue(request.Context(), middleware.CtxUserID, userID)
		requestContext = context.WithValue(requestContext, middleware.CtxOrgID, orgID)
		requestContext = context.WithValue(requestContext, middleware.CtxUserRole, auth.RolePlatformAdmin)
		customer := customercontext.Snapshot{State: customercontext.StateReady, OrganizationID: orgID, OrganizationName: "Customer"}
		requestContext = context.WithValue(requestContext, middleware.CtxCustomerContext, customer)
		requestContext = context.WithValue(requestContext, middleware.CtxCustomerOrgID, orgID)
		return request.WithContext(requestContext)
	}

	successResponse := httptest.NewRecorder()
	handler.RotateChannelKey(successResponse, authorized("accepted-replacement"))
	require.Equal(t, http.StatusCreated, successResponse.Code, successResponse.Body.String())
	var successBody struct {
		ID            int64 `json:"id"`
		RotatedFromID int64 `json:"rotated_from_id"`
		Probe         struct {
			Success bool `json:"success"`
		} `json:"probe"`
	}
	require.NoError(t, json.Unmarshal(successResponse.Body.Bytes(), &successBody))
	require.NotEqual(t, oldCredentialID, successBody.ID)
	require.Equal(t, oldCredentialID, successBody.RotatedFromID)
	require.True(t, successBody.Probe.Success)
	requestMu.Lock()
	require.Equal(t, "Bearer accepted-replacement", seenAuthorization)
	require.Equal(t, "upstream/model", seenModel)
	requestMu.Unlock()

	var oldStatus, replacementStatus, replacementEncrypted string
	var retiredEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,state_epoch FROM channel_keys WHERE id=$1`, oldCredentialID).Scan(&oldStatus, &retiredEpoch))
	require.Equal(t, "disabled", oldStatus)
	require.Equal(t, oldCredentialEpoch+1, retiredEpoch)
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,key_enc FROM channel_keys WHERE id=$1`, successBody.ID).Scan(&replacementStatus, &replacementEncrypted))
	require.Equal(t, "active", replacementStatus)
	decrypted, err := auth.DecryptKey(replacementEncrypted, encKey)
	require.NoError(t, err)
	require.Equal(t, "accepted-replacement", decrypted)

	var probeCredentialID, probeCredentialEpoch, probeRoutingEpoch int64
	var probePublicModel, probeUpstreamModel string
	var probeSuccess bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT credential_id,credential_state_epoch,routing_epoch,public_model,upstream_model,success
  FROM channel_model_probes WHERE channel_id=$1 AND public_model='public-model'`, channelID).Scan(
		&probeCredentialID, &probeCredentialEpoch, &probeRoutingEpoch, &probePublicModel, &probeUpstreamModel, &probeSuccess,
	))
	require.Equal(t, successBody.ID, probeCredentialID)
	require.Equal(t, int64(1), probeCredentialEpoch)
	require.Equal(t, routingEpoch, probeRoutingEpoch)
	require.Equal(t, "public-model", probePublicModel)
	require.Equal(t, "upstream/model", probeUpstreamModel)
	require.True(t, probeSuccess)

	var activeCount, totalCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE status='active'),COUNT(*) FROM channel_keys WHERE channel_id=$1`, channelID).Scan(&activeCount, &totalCount))
	require.Equal(t, 1, activeCount)
	require.Equal(t, 2, totalCount)
	var auditCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='channelkey.rotate' AND resource_id=$1`, fmt.Sprintf("%d", successBody.ID)).Scan(&auditCount))
	require.Equal(t, 1, auditCount)

	// A rejected second replacement must not create a third credential, retire
	// the currently active one, or replace its successful probe projection.
	oldCredentialID = successBody.ID
	failureResponse := httptest.NewRecorder()
	handler.RotateChannelKey(failureResponse, authorized("rejected-replacement"))
	require.Equal(t, http.StatusUnprocessableEntity, failureResponse.Code, failureResponse.Body.String())
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE status='active'),COUNT(*) FROM channel_keys WHERE channel_id=$1`, channelID).Scan(&activeCount, &totalCount))
	require.Equal(t, 1, activeCount)
	require.Equal(t, 2, totalCount)
	var stillActiveStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM channel_keys WHERE id=$1`, successBody.ID).Scan(&stillActiveStatus))
	require.Equal(t, "active", stillActiveStatus)
	require.NoError(t, pool.QueryRow(ctx, `SELECT credential_id,success FROM channel_model_probes WHERE channel_id=$1 AND public_model='public-model'`, channelID).Scan(&probeCredentialID, &probeSuccess))
	require.Equal(t, successBody.ID, probeCredentialID)
	require.True(t, probeSuccess)
}
