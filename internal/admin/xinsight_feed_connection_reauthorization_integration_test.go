//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestXInsightFeedControlReauthorizesAfterCommittedActorAndProjectChangesIntegration(t *testing.T) {
	tests := []struct {
		name           string
		action         string
		mutate         func(context.Context, *testing.T, xInsightFeedAuthorizationFixture)
		wantFeedKeys   int
		wantConnection string
	}{
		{
			name:   "actor disabled before staged create transaction",
			action: "create",
			mutate: func(ctx context.Context, t *testing.T, fixture xInsightFeedAuthorizationFixture) {
				tx, err := fixture.pool.Begin(ctx)
				require.NoError(t, err)
				require.NoError(t, lockProjectMembershipMutation(ctx, tx, fixture.actorID))
				_, err = tx.Exec(ctx, `UPDATE users SET status = 'disabled' WHERE id = $1`, fixture.actorID)
				require.NoError(t, err)
				require.NoError(t, tx.Commit(ctx))
			},
		},
		{
			name:   "actor role changed before staged create transaction",
			action: "create",
			mutate: func(ctx context.Context, t *testing.T, fixture xInsightFeedAuthorizationFixture) {
				tx, err := fixture.pool.Begin(ctx)
				require.NoError(t, err)
				require.NoError(t, lockProjectMembershipMutation(ctx, tx, fixture.actorID))
				_, err = tx.Exec(ctx, `UPDATE users SET role = 'org_admin' WHERE id = $1`, fixture.actorID)
				require.NoError(t, err)
				require.NoError(t, tx.Commit(ctx))
			},
		},
		{
			name:   "project disabled before staged create transaction",
			action: "create",
			mutate: func(ctx context.Context, t *testing.T, fixture xInsightFeedAuthorizationFixture) {
				_, err := fixture.pool.Exec(ctx, `UPDATE projects SET status = 'disabled' WHERE id = $1`, fixture.projectID)
				require.NoError(t, err)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newXInsightFeedAuthorizationFixture(t, test.action == "pause")
			beginReached := make(chan struct{})
			allowBegin := make(chan struct{})
			handler := NewHandler(
				&apiKeyInterleavingDB{pool: fixture.pool, beginReached: beginReached, allowBegin: allowBegin},
				"xinsight-feed-reauthorization-secret",
				[]byte("0123456789abcdef01234567"),
				nil,
			)
			handler.SetCompletedFeedController(completedFeedControllerFixture{})
			requestID := "xinsight-feed-reauthorization-" + strings.ReplaceAll(test.name, " ", "-")
			var request *http.Request
			invoke := func(response *httptest.ResponseRecorder) {
				switch test.action {
				case "create":
					request = apiKeyIntegrationRequest(
						http.MethodPost,
						fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/credentials", fixture.projectID),
						`{"allowed_ip":"127.0.0.1/32"}`,
						fixture.actorID,
						fixture.orgID,
						auth.RolePlatformAdmin,
						requestID,
					)
					request.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
					handler.CreateXInsightFeedCredential(response, request)
				case "pause":
					request = apiKeyIntegrationRequest(
						http.MethodPost,
						fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/pause", fixture.projectID),
						"",
						fixture.actorID,
						fixture.orgID,
						auth.RolePlatformAdmin,
						requestID,
					)
					request.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
					handler.PauseXInsightFeedConnection(response, request)
				default:
					t.Fatalf("unknown action %q", test.action)
				}
			}

			responseCh := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				response := httptest.NewRecorder()
				invoke(response)
				responseCh <- response
			}()
			select {
			case <-beginReached:
			case <-time.After(5 * time.Second):
				t.Fatal("X-Insight control did not reach its committing transaction after preauthorization")
			}

			test.mutate(context.Background(), t, fixture)
			close(allowBegin)

			var response *httptest.ResponseRecorder
			select {
			case response = <-responseCh:
			case <-time.After(5 * time.Second):
				t.Fatal("X-Insight control did not finish after authorization change committed")
			}
			require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), `"key"`)

			var feedKeyCount, connectionCount, auditCount int
			require.NoError(t, fixture.pool.QueryRow(context.Background(),
				`SELECT COUNT(*) FROM api_keys WHERE org_id = $1 AND project_id = $2 AND purpose = $3`,
				fixture.orgID, fixture.projectID, middleware.APIKeyPurposeXInsightFeedRead,
			).Scan(&feedKeyCount))
			require.Equal(t, test.wantFeedKeys, feedKeyCount)
			require.NoError(t, fixture.pool.QueryRow(context.Background(),
				`SELECT COUNT(*) FROM completed_execution_feed_connections WHERE org_id = $1 AND project_id = $2`,
				fixture.orgID, fixture.projectID,
			).Scan(&connectionCount))
			if test.wantConnection == "" {
				require.Zero(t, connectionCount)
			} else {
				require.Equal(t, 1, connectionCount)
				var connectionStatus, keyStatus string
				require.NoError(t, fixture.pool.QueryRow(context.Background(),
					`SELECT status FROM completed_execution_feed_connections WHERE org_id = $1 AND project_id = $2`,
					fixture.orgID, fixture.projectID,
				).Scan(&connectionStatus))
				require.Equal(t, test.wantConnection, connectionStatus)
				require.NoError(t, fixture.pool.QueryRow(context.Background(),
					`SELECT status FROM api_keys WHERE org_id = $1 AND project_id = $2 AND purpose = $3`,
					fixture.orgID, fixture.projectID, middleware.APIKeyPurposeXInsightFeedRead,
				).Scan(&keyStatus))
				require.Equal(t, middleware.APIKeyStatusFeedActive, keyStatus)
			}
			require.NoError(t, fixture.pool.QueryRow(context.Background(),
				`SELECT COUNT(*) FROM audit_logs WHERE request_id = $1`,
				requestID,
			).Scan(&auditCount))
			require.Zero(t, auditCount)
		})
	}
}

type xInsightFeedAuthorizationFixture struct {
	pool        *pgxpool.Pool
	orgID       int64
	projectID   int64
	actorID     int64
	activeKeyID int64
}

func newXInsightFeedAuthorizationFixture(t *testing.T, activeConnection bool) xInsightFeedAuthorizationFixture {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var fixture xInsightFeedAuthorizationFixture
	fixture.pool = pool
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type)
		 VALUES ('xinsight-feed-reauthorization', 'active', 'standard') RETURNING id`,
	).Scan(&fixture.orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id, name, slug, status, budget_period)
		 VALUES ($1, 'X-Insight feed reauthorization', 'xinsight-feed-reauthorization', 'active', 'none') RETURNING id`,
		fixture.orgID,
	).Scan(&fixture.projectID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (org_id, username, password_hash, role, status)
		 VALUES ($1, 'xinsight-feed-platform-admin', 'test-hash', 'platform_admin', 'active') RETURNING id`,
		fixture.orgID,
	).Scan(&fixture.actorID))
	if activeConnection {
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO api_keys (
			    org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix,
			    status, rpm_limit, qps_limit, allowed_ips, allowed_models, policy_labels, purpose
			 ) VALUES (
			    $1, $2, NULL, 'service', 'X-Insight feed active', 'test-hash', 'ur-feed-reauth',
			    'feed_active', 60, 1, '127.0.0.1/32', '', '', 'xinsight.feed.read'
		 ) RETURNING id`,
			fixture.orgID, fixture.projectID,
		).Scan(&fixture.activeKeyID))
		_, err := pool.Exec(ctx,
			`INSERT INTO completed_execution_feed_connections (org_id, project_id, status)
			 VALUES ($1, $2, 'active')`,
			fixture.orgID, fixture.projectID,
		)
		require.NoError(t, err)
	}
	return fixture
}

func TestXInsightFeedControlRequiresActiveProjectForExpansionIntegration(t *testing.T) {
	t.Run("initial create", func(t *testing.T) {
		fixture := newXInsightFeedAuthorizationFixture(t, false)
		disableXInsightFeedProject(t, fixture, "disabled")
		handler := newXInsightFeedAuthorizationHandler(fixture)
		request := xInsightFeedControlRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/credentials", fixture.projectID),
			`{"allowed_ip":"127.0.0.1/32"}`,
			fixture,
			"inactive-project-create",
		)
		request.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
		response := httptest.NewRecorder()

		handler.CreateXInsightFeedCredential(response, request)

		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		requireXInsightFeedControlState(t, fixture, 0, 0, "")
		requireXInsightFeedAuditCount(t, fixture, "inactive-project-create", 0)
	})

	t.Run("initial activation", func(t *testing.T) {
		fixture := newXInsightFeedAuthorizationFixture(t, false)
		stagedKeyID := insertXInsightFeedAuthorizationKey(t, fixture, middleware.APIKeyStatusFeedStaged, "ur-inact-act")
		insertXInsightFeedAuthorizationConnection(t, fixture, xInsightFeedConnectionPaused)
		disableXInsightFeedProject(t, fixture, "disabled")
		handler := newXInsightFeedAuthorizationHandler(fixture)
		request := xInsightFeedControlRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/credentials/activate", fixture.projectID),
			fmt.Sprintf(`{"staged_credential_id":%d}`, stagedKeyID),
			fixture,
			"inactive-project-activate",
		)
		request.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
		response := httptest.NewRecorder()

		handler.ActivateInitialXInsightFeedCredential(response, request)

		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		requireXInsightFeedKeyStatus(t, fixture, stagedKeyID, middleware.APIKeyStatusFeedStaged)
		requireXInsightFeedControlState(t, fixture, 1, 1, xInsightFeedConnectionPaused)
		requireXInsightFeedAuditCount(t, fixture, "inactive-project-activate", 0)
	})

	t.Run("resume", func(t *testing.T) {
		fixture := newXInsightFeedAuthorizationFixture(t, true)
		_, err := fixture.pool.Exec(context.Background(),
			`UPDATE completed_execution_feed_connections SET status = 'paused'
			 WHERE org_id = $1 AND project_id = $2`,
			fixture.orgID, fixture.projectID,
		)
		require.NoError(t, err)
		disableXInsightFeedProject(t, fixture, "archived")
		handler := newXInsightFeedAuthorizationHandler(fixture)
		request := xInsightFeedControlRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/resume", fixture.projectID),
			"",
			fixture,
			"inactive-project-resume",
		)
		request.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
		response := httptest.NewRecorder()

		handler.ResumeXInsightFeedConnection(response, request)

		require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		requireXInsightFeedKeyStatus(t, fixture, fixture.activeKeyID, middleware.APIKeyStatusFeedActive)
		requireXInsightFeedControlState(t, fixture, 1, 1, xInsightFeedConnectionPaused)
		requireXInsightFeedAuditCount(t, fixture, "inactive-project-resume", 0)
	})
}

func TestXInsightFeedControlCleanupRemainsAvailableForInactiveProjectIntegration(t *testing.T) {
	t.Run("disabled project pause cancel and active revoke", func(t *testing.T) {
		fixture := newXInsightFeedAuthorizationFixture(t, true)
		stagedKeyID := insertXInsightFeedAuthorizationKey(t, fixture, middleware.APIKeyStatusFeedStaged, "ur-dis-cancel")
		disableXInsightFeedProject(t, fixture, "disabled")
		handler := newXInsightFeedAuthorizationHandler(fixture)

		statusRequest := xInsightFeedControlRequest(
			http.MethodGet,
			fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d", fixture.projectID),
			"",
			fixture,
			"inactive-project-status",
		)
		statusRequest.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
		statusResponse := httptest.NewRecorder()
		handler.GetXInsightFeedConnection(statusResponse, statusRequest)
		require.Equal(t, http.StatusOK, statusResponse.Code, statusResponse.Body.String())
		var status map[string]any
		require.NoError(t, json.Unmarshal(statusResponse.Body.Bytes(), &status))
		require.Equal(t, "disabled", status["project_status"])

		pauseRequest := xInsightFeedControlRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/pause", fixture.projectID),
			"",
			fixture,
			"inactive-project-pause",
		)
		pauseRequest.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
		pauseResponse := httptest.NewRecorder()
		handler.PauseXInsightFeedConnection(pauseResponse, pauseRequest)
		require.Equal(t, http.StatusOK, pauseResponse.Code, pauseResponse.Body.String())

		cancelRequest := xInsightFeedControlRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/rotation/cancel", fixture.projectID),
			fmt.Sprintf(`{"staged_credential_id":%d}`, stagedKeyID),
			fixture,
			"inactive-project-cancel",
		)
		cancelRequest.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
		cancelResponse := httptest.NewRecorder()
		handler.CancelXInsightFeedRotation(cancelResponse, cancelRequest)
		require.Equal(t, http.StatusOK, cancelResponse.Code, cancelResponse.Body.String())

		revokeRequest := xInsightFeedControlRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/credentials/%d/revoke", fixture.projectID, fixture.activeKeyID),
			"",
			fixture,
			"inactive-project-revoke-active",
		)
		revokeRequest.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
		revokeRequest.SetPathValue("credential_id", strconv.FormatInt(fixture.activeKeyID, 10))
		revokeResponse := httptest.NewRecorder()
		handler.RevokeXInsightFeedCredential(revokeResponse, revokeRequest)
		require.Equal(t, http.StatusOK, revokeResponse.Code, revokeResponse.Body.String())

		requireXInsightFeedKeyStatus(t, fixture, stagedKeyID, middleware.APIKeyStatusFeedRevoked)
		requireXInsightFeedKeyStatus(t, fixture, fixture.activeKeyID, middleware.APIKeyStatusFeedRevoked)
		requireXInsightFeedControlState(t, fixture, 2, 1, xInsightFeedConnectionPaused)
		requireXInsightFeedAuditCount(t, fixture, "inactive-project-pause", 1)
		requireXInsightFeedAuditCount(t, fixture, "inactive-project-cancel", 1)
		requireXInsightFeedAuditCount(t, fixture, "inactive-project-revoke-active", 1)

		reenableXInsightFeedProject(t, fixture)
		requireFeedCredentialCannotReactivate(t, fixture, handler, fixture.activeKeyID, "revoked-active-after-project-reenable")
		requireFeedCredentialCannotReactivate(t, fixture, handler, stagedKeyID, "cancelled-staged-after-project-reenable")
	})

	t.Run("archived project staged revoke", func(t *testing.T) {
		fixture := newXInsightFeedAuthorizationFixture(t, false)
		stagedKeyID := insertXInsightFeedAuthorizationKey(t, fixture, middleware.APIKeyStatusFeedStaged, "ur-arc-revoke")
		insertXInsightFeedAuthorizationConnection(t, fixture, xInsightFeedConnectionPaused)
		disableXInsightFeedProject(t, fixture, "archived")
		handler := newXInsightFeedAuthorizationHandler(fixture)

		statusRequest := xInsightFeedControlRequest(
			http.MethodGet,
			fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d", fixture.projectID),
			"",
			fixture,
			"archived-project-status",
		)
		statusRequest.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
		statusResponse := httptest.NewRecorder()
		handler.GetXInsightFeedConnection(statusResponse, statusRequest)
		require.Equal(t, http.StatusOK, statusResponse.Code, statusResponse.Body.String())
		var status map[string]any
		require.NoError(t, json.Unmarshal(statusResponse.Body.Bytes(), &status))
		require.Equal(t, "archived", status["project_status"])

		revokeRequest := xInsightFeedControlRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/credentials/%d/revoke", fixture.projectID, stagedKeyID),
			"",
			fixture,
			"archived-project-revoke-staged",
		)
		revokeRequest.SetPathValue("project_id", strconv.FormatInt(fixture.projectID, 10))
		revokeRequest.SetPathValue("credential_id", strconv.FormatInt(stagedKeyID, 10))
		revokeResponse := httptest.NewRecorder()
		handler.RevokeXInsightFeedCredential(revokeResponse, revokeRequest)
		require.Equal(t, http.StatusOK, revokeResponse.Code, revokeResponse.Body.String())
		requireXInsightFeedKeyStatus(t, fixture, stagedKeyID, middleware.APIKeyStatusFeedRevoked)
		requireXInsightFeedAuditCount(t, fixture, "archived-project-revoke-staged", 1)

		reenableXInsightFeedProject(t, fixture)
		requireFeedCredentialCannotReactivate(t, fixture, handler, stagedKeyID, "revoked-staged-after-project-reenable")
	})
}

func newXInsightFeedAuthorizationHandler(fixture xInsightFeedAuthorizationFixture) *Handler {
	handler := NewHandler(
		fixture.pool,
		"xinsight-feed-reauthorization-secret",
		[]byte("0123456789abcdef01234567"),
		nil,
	)
	handler.SetCompletedFeedController(completedFeedControllerFixture{})
	return handler
}

func xInsightFeedControlRequest(
	method, target, body string,
	fixture xInsightFeedAuthorizationFixture,
	requestID string,
) *http.Request {
	return apiKeyIntegrationRequest(
		method,
		target,
		body,
		fixture.actorID,
		fixture.orgID,
		auth.RolePlatformAdmin,
		requestID,
	)
}

func insertXInsightFeedAuthorizationKey(
	t *testing.T,
	fixture xInsightFeedAuthorizationFixture,
	status, prefix string,
) int64 {
	t.Helper()
	var keyID int64
	require.NoError(t, fixture.pool.QueryRow(context.Background(),
		`INSERT INTO api_keys (
		    org_id, project_id, owner_user_id, key_type, name, key_hash, key_prefix,
		    status, rpm_limit, qps_limit, allowed_ips, allowed_models, policy_labels, purpose
		 ) VALUES (
		    $1, $2, NULL, 'service', $3, $4, $5,
		    $6, 60, 1, '127.0.0.1/32', '', '', 'xinsight.feed.read'
		 ) RETURNING id`,
		fixture.orgID, fixture.projectID, prefix, "hash-"+prefix, prefix, status,
	).Scan(&keyID))
	return keyID
}

func insertXInsightFeedAuthorizationConnection(
	t *testing.T,
	fixture xInsightFeedAuthorizationFixture,
	status string,
) {
	t.Helper()
	_, err := fixture.pool.Exec(context.Background(),
		`INSERT INTO completed_execution_feed_connections (org_id, project_id, status)
		 VALUES ($1, $2, $3)`,
		fixture.orgID, fixture.projectID, status,
	)
	require.NoError(t, err)
}

func disableXInsightFeedProject(t *testing.T, fixture xInsightFeedAuthorizationFixture, status string) {
	t.Helper()
	_, err := fixture.pool.Exec(context.Background(),
		`UPDATE projects SET status = $2 WHERE id = $1`,
		fixture.projectID, status,
	)
	require.NoError(t, err)
}

func reenableXInsightFeedProject(t *testing.T, fixture xInsightFeedAuthorizationFixture) {
	t.Helper()
	_, err := fixture.pool.Exec(context.Background(),
		`UPDATE projects SET status = 'active' WHERE id = $1`,
		fixture.projectID,
	)
	require.NoError(t, err)
}

func requireXInsightFeedKeyStatus(
	t *testing.T,
	fixture xInsightFeedAuthorizationFixture,
	keyID int64,
	want string,
) {
	t.Helper()
	var got string
	require.NoError(t, fixture.pool.QueryRow(context.Background(),
		`SELECT status FROM api_keys WHERE id = $1`,
		keyID,
	).Scan(&got))
	require.Equal(t, want, got)
}

func requireXInsightFeedControlState(
	t *testing.T,
	fixture xInsightFeedAuthorizationFixture,
	wantKeys, wantConnections int,
	wantConnectionStatus string,
) {
	t.Helper()
	var keys, connections int
	require.NoError(t, fixture.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM api_keys
		 WHERE org_id = $1 AND project_id = $2 AND purpose = 'xinsight.feed.read'`,
		fixture.orgID, fixture.projectID,
	).Scan(&keys))
	require.Equal(t, wantKeys, keys)
	require.NoError(t, fixture.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM completed_execution_feed_connections
		 WHERE org_id = $1 AND project_id = $2`,
		fixture.orgID, fixture.projectID,
	).Scan(&connections))
	require.Equal(t, wantConnections, connections)
	if wantConnectionStatus != "" {
		var status string
		require.NoError(t, fixture.pool.QueryRow(context.Background(),
			`SELECT status FROM completed_execution_feed_connections
			 WHERE org_id = $1 AND project_id = $2`,
			fixture.orgID, fixture.projectID,
		).Scan(&status))
		require.Equal(t, wantConnectionStatus, status)
	}
}

func requireXInsightFeedAuditCount(
	t *testing.T,
	fixture xInsightFeedAuthorizationFixture,
	requestID string,
	want int,
) {
	t.Helper()
	var count int
	require.NoError(t, fixture.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_logs WHERE request_id = $1`,
		requestID,
	).Scan(&count))
	require.Equal(t, want, count)
}

func requireFeedCredentialCannotReactivate(
	t *testing.T,
	fixture xInsightFeedAuthorizationFixture,
	handler *Handler,
	keyID int64,
	requestID string,
) {
	t.Helper()
	request := apiKeyIntegrationRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/keys/%d", keyID),
		`{"status":"active"}`,
		fixture.actorID,
		fixture.orgID,
		auth.RoleOrgAdmin,
		requestID,
	)
	request.SetPathValue("id", strconv.FormatInt(keyID, 10))
	response := httptest.NewRecorder()
	handler.UpdateAPIKey(response, request)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	requireXInsightFeedKeyStatus(t, fixture, keyID, middleware.APIKeyStatusFeedRevoked)
}
