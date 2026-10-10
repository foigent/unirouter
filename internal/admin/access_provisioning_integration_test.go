//go:build integration

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/accessprovisioning"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/principallock"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestForeignAccessSubjectFailsBeforePrincipalMutationLanesIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var foreignOrgID, foreignUserID, actorOrgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('foreign-subject-lane-target','active','customer') RETURNING id`).Scan(&foreignOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'foreign-subject-lane-user','hash','member','developer','active') RETURNING id`, foreignOrgID).Scan(&foreignUserID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('foreign-subject-lane-actor','active','customer') RETURNING id`).Scan(&actorOrgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'Foreign Subject Lane Actor','foreign-subject-lane-actor','active') RETURNING id`, actorOrgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'foreign-subject-lane-actor','hash','member','developer','active') RETURNING id`, actorOrgID).Scan(&actorID))
	_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, actorID)
	require.NoError(t, err)

	handler := NewHandlerWithAPIKeyHashSecret(
		pool, "foreign-subject-lane-jwt", "foreign-subject-lane-key-hash",
		[]byte("0123456789abcdef01234567"), nil,
	)
	payload := fmt.Sprintf(`{
		"org_id":%d,"project_id":%d,
		"subject":{"type":"user","user_id":%d},
		"request_kind":"human_personal",
		"requested_scope":{"project_role":"project_user","scope_mode":"project_default","models":[],"endpoints":[]},
		"environment":"personal","production_use":false,"submission_mode":"self"
	}`, actorOrgID, projectID, foreignUserID)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(middleware.ClientRequestIDHeader, "foreign-subject-lane-request")
	request = withIntegrationCommittingActor(t, pool, request, actorID)

	// Holding the legitimate actor lane distinguishes scope-first rejection
	// from the former behavior, which projected both actor and foreign subject
	// into sorted deployment-global lanes before discovering the tenant mismatch.
	holdActor, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer holdActor.Rollback(ctx) //nolint:errcheck
	require.NoError(t, principallock.LockUser(ctx, holdActor, actorID))

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		middleware.OperationIdentity(http.HandlerFunc(handler.CreateAccessRequest)).ServeHTTP(response, request)
		done <- response
	}()
	select {
	case response := <-done:
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
	case <-time.After(2 * time.Second):
		t.Fatal("foreign subject request waited on a principal mutation lane before scope rejection")
	}

	var requestCount, auditCount, outboxCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_requests`).Scan(&requestCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='access_request.provision'`).Scan(&auditCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM internal_outbox_events WHERE event_type='access_request.provision'`).Scan(&outboxCount))
	require.Zero(t, requestCount)
	require.Zero(t, auditCount)
	require.Zero(t, outboxCount)
}

func TestSelfAccessProvisioningKeepsSatisfiedLiveMembershipAndCommitsEvidenceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('self-access-org','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'Self Access Project','self-access-project','active') RETURNING id`, orgID).Scan(&projectID))
	handler := NewHandlerWithAPIKeyHashSecret(pool, "self-access-jwt-secret", "self-access-key-hash-secret", []byte("0123456789abcdef01234567"), nil)

	for _, test := range []struct {
		name, membershipRole, compatibilityRole string
	}{
		{name: "project user", membershipRole: auth.ProjectRoleUser, compatibilityRole: auth.RoleDeveloper},
		{name: "project admin", membershipRole: auth.ProjectRoleAdmin, compatibilityRole: auth.RoleProjectAdmin},
	} {
		t.Run(test.name, func(t *testing.T) {
			var userID int64
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
				VALUES ($1,$2,'hash','member',$3,'active') RETURNING id`,
				orgID, "self-access-"+test.membershipRole, test.compatibilityRole).Scan(&userID))
			_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,$3)`,
				projectID, userID, test.membershipRole)
			require.NoError(t, err)

			payload := fmt.Sprintf(`{
				"org_id":%d,"project_id":%d,
				"subject":{"type":"user","user_id":%d},
				"request_kind":"human_personal",
				"requested_scope":{"project_role":%q,"scope_mode":"project_default","models":[],"endpoints":[]},
				"environment":"personal","production_use":false,"submission_mode":"self"
			}`, orgID, projectID, userID, test.membershipRole)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests", bytes.NewBufferString(payload))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(middleware.ClientRequestIDHeader, "self-access-"+test.membershipRole)
			request = withIntegrationCommittingActor(t, pool, request, userID)
			response := httptest.NewRecorder()
			middleware.OperationIdentity(http.HandlerFunc(handler.CreateAccessRequest)).ServeHTTP(response, request)
			require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
			var result accessprovisioning.ProvisionResult
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.NotNil(t, result.Grant)
			require.Equal(t, accessprovisioning.RequestApproved, result.Request.Status)

			var membershipRole string
			require.NoError(t, pool.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`,
				projectID, userID).Scan(&membershipRole))
			require.Equal(t, test.membershipRole, membershipRole)
			var auditCount, outboxCount int
			require.NoError(t, pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM audit_logs
				WHERE org_id=$1 AND user_id=$2 AND action='access_request.provision' AND resource_id=$3`,
				orgID, userID, result.Request.RequestRef).Scan(&auditCount))
			require.Equal(t, 1, auditCount)
			require.NoError(t, pool.QueryRow(ctx, `
				SELECT COUNT(*)
				FROM internal_outbox_events e
				JOIN audit_logs a ON a.outbox_event_key=e.event_key
				WHERE a.org_id=$1 AND a.user_id=$2
				  AND a.action='access_request.provision' AND a.resource_id=$3`,
				orgID, userID, result.Request.RequestRef).Scan(&outboxCount))
			require.GreaterOrEqual(t, outboxCount, 1)
		})
	}
}

func TestCredentialCreatingHandlersIgnoreRetiredCommercialLicenseIntegration(t *testing.T) {
	for _, test := range []struct {
		name      string
		expiresAt time.Time
	}{
		{
			name:      "active key capacity",
			expiresAt: time.Now().UTC().Add(24 * time.Hour),
		},
		{
			name:      "expired license",
			expiresAt: time.Now().UTC().Add(-time.Hour),
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			caseID := strings.ReplaceAll(test.name, " ", "-")
			pool := testutil.SetupMigratedTestDB(t)
			ctx := context.Background()
			var orgID, projectID, actorID int64
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO orgs (name,status,org_type)
				VALUES ($1,'active','customer') RETURNING id`,
				"credential-license-"+caseID,
			).Scan(&orgID))
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO projects (org_id,name,slug,status)
				VALUES ($1,'Credential License Project',$2,'active') RETURNING id`,
				orgID, "credential-license-"+caseID,
			).Scan(&projectID))
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
				VALUES ($1,$2,'hash','org_admin','org_admin','active') RETURNING id`,
				orgID, "credential-license-admin-"+caseID,
			).Scan(&actorID))

			seedSubject := func(suffix string) int64 {
				t.Helper()
				var userID int64
				require.NoError(t, pool.QueryRow(ctx, `
					INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
					VALUES ($1,$2,'hash','member','developer','active') RETURNING id`,
					orgID, "credential-license-"+suffix+"-"+caseID,
				).Scan(&userID))
				_, err := pool.Exec(ctx, `
					INSERT INTO project_members (project_id,user_id,role)
					VALUES ($1,$2,'project_user')`, projectID, userID)
				require.NoError(t, err)
				return userID
			}
			issueSubjectID := seedSubject("issue")
			rotateSubjectID := seedSubject("rotate")
			legacySubjectID := seedSubject("legacy")
			handler := NewHandlerWithAPIKeyHashSecret(
				pool, "credential-license-jwt", "credential-license-key-hash",
				[]byte("0123456789abcdef01234567"), nil,
			)

			invoke := func(actorUserID int64, method, target, body, clientRequestID string, pathValues map[string]string, handlerFunc http.HandlerFunc) *httptest.ResponseRecorder {
				t.Helper()
				request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set(middleware.ClientRequestIDHeader, clientRequestID)
				for name, value := range pathValues {
					request.SetPathValue(name, value)
				}
				request = withIntegrationCommittingActor(t, pool, request, actorUserID)
				response := httptest.NewRecorder()
				middleware.OperationIdentity(handlerFunc).ServeHTTP(response, request)
				return response
			}
			createGrant := func(subjectID int64, clientRequestID string) accessprovisioning.Grant {
				t.Helper()
				payload := fmt.Sprintf(`{
					"org_id":%d,"project_id":%d,
					"subject":{"type":"user","user_id":%d},
					"request_kind":"human_personal",
					"requested_scope":{"project_role":"project_user","scope_mode":"project_default","models":[],"endpoints":[]},
					"environment":"personal","production_use":false,"submission_mode":"direct_grant"
				}`, orgID, projectID, subjectID)
				response := invoke(actorID, http.MethodPost, "/api/v1/access-requests", payload, clientRequestID, nil, handler.CreateAccessRequest)
				require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
				var result accessprovisioning.ProvisionResult
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.NotNil(t, result.Grant)
				return *result.Grant
			}

			issueGrant := createGrant(issueSubjectID, "license-grant-issue-"+caseID)
			rotateGrant := createGrant(rotateSubjectID, "license-grant-rotate-"+caseID)
			predecessorResponse := invoke(rotateSubjectID,
				http.MethodPost, "/api/v1/access-grants/"+rotateGrant.GrantRef+"/credentials",
				`{"name":"rotation predecessor"}`, "license-predecessor-"+caseID,
				map[string]string{"ref": rotateGrant.GrantRef}, handler.IssueAccessGrantCredential,
			)
			require.Equal(t, http.StatusCreated, predecessorResponse.Code, predecessorResponse.Body.String())
			var predecessor accessprovisioning.Credential
			require.NoError(t, json.Unmarshal(predecessorResponse.Body.Bytes(), &predecessor))
			require.Positive(t, predecessor.ID)

			_, err := pool.Exec(ctx, `
				INSERT INTO commercial_license_entitlements (
					singleton,license_id,deployment_id,key_id,expires_at,features,active_api_keys
				) VALUES (TRUE,$1,'credential-license-deployment','credential-license-key',$2,'["*"]'::jsonb,1)`,
				"credential-license-"+caseID, test.expiresAt,
			)
			require.NoError(t, err)

			assertCreated := func(label string, response *httptest.ResponseRecorder) {
				t.Helper()
				require.Equal(t, http.StatusCreated, response.Code, "%s: %s", label, response.Body.String())
				require.NotContains(t, response.Body.String(), "license_error.")
			}

			issueResponse := invoke(issueSubjectID,
				http.MethodPost, "/api/v1/access-grants/"+issueGrant.GrantRef+"/credentials",
				`{"name":"license-retired canonical issue"}`, "license-retired-issue-"+caseID,
				map[string]string{"ref": issueGrant.GrantRef}, handler.IssueAccessGrantCredential,
			)
			assertCreated("canonical issue", issueResponse)

			rotateResponse := invoke(rotateSubjectID,
				http.MethodPost, fmt.Sprintf("/api/v1/access-grants/%s/credentials/%d/rotate", rotateGrant.GrantRef, predecessor.ID),
				`{}`,
				"license-retired-rotate-"+caseID,
				map[string]string{"ref": rotateGrant.GrantRef, "key_id": fmt.Sprint(predecessor.ID)}, handler.RotateAccessGrantCredential,
			)
			assertCreated("canonical rotate", rotateResponse)

			legacyResponse := invoke(legacySubjectID,
				http.MethodPost, "/api/v1/keys",
				fmt.Sprintf(`{"org_id":%d,"project_id":%d,"owner_user_id":%d,"key_type":"personal","name":"license-retired legacy create"}`,
					orgID, projectID, legacySubjectID),
				"license-retired-legacy-"+caseID, nil, handler.CreateAPIKey,
			)
			assertCreated("legacy create adapter", legacyResponse)
		})
	}
}

func TestLegacyPutGrantBoundStatusUsesCredentialRevokeLifecycleIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, actorID, subjectID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('legacy-put-grant-revoke','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'Legacy Put Grant Revoke','legacy-put-grant-revoke','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'legacy-put-grant-admin','hash','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'legacy-put-grant-subject','hash','member','developer','active') RETURNING id`, orgID).Scan(&subjectID))
	_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, subjectID)
	require.NoError(t, err)
	handler := NewHandlerWithAPIKeyHashSecret(
		pool, "legacy-put-grant-jwt", "legacy-put-grant-key-hash",
		[]byte("0123456789abcdef01234567"), nil,
	)

	invoke := func(actorUserID int64, method, target, body, clientRequestID string, pathValues map[string]string, handlerFunc http.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(middleware.ClientRequestIDHeader, clientRequestID)
		for name, value := range pathValues {
			request.SetPathValue(name, value)
		}
		request = withIntegrationCommittingActor(t, pool, request, actorUserID)
		response := httptest.NewRecorder()
		middleware.OperationIdentity(handlerFunc).ServeHTTP(response, request)
		return response
	}
	grantPayload := fmt.Sprintf(`{
		"org_id":%d,"project_id":%d,
		"subject":{"type":"user","user_id":%d},
		"request_kind":"human_personal",
		"requested_scope":{"project_role":"project_user","scope_mode":"project_default","models":[],"endpoints":[]},
		"environment":"personal","production_use":false,"submission_mode":"direct_grant"
	}`, orgID, projectID, subjectID)
	grantResponse := invoke(actorID, http.MethodPost, "/api/v1/access-requests", grantPayload,
		"legacy-put-grant-create", nil, handler.CreateAccessRequest)
	require.Equal(t, http.StatusCreated, grantResponse.Code, grantResponse.Body.String())
	var grantResult accessprovisioning.ProvisionResult
	require.NoError(t, json.Unmarshal(grantResponse.Body.Bytes(), &grantResult))
	require.NotNil(t, grantResult.Grant)

	credentialResponse := invoke(subjectID,
		http.MethodPost, "/api/v1/access-grants/"+grantResult.Grant.GrantRef+"/credentials",
		`{"name":"legacy put grant credential"}`, "legacy-put-grant-issue",
		map[string]string{"ref": grantResult.Grant.GrantRef}, handler.IssueAccessGrantCredential,
	)
	require.Equal(t, http.StatusCreated, credentialResponse.Code, credentialResponse.Body.String())
	var credential accessprovisioning.Credential
	require.NoError(t, json.Unmarshal(credentialResponse.Body.Bytes(), &credential))
	require.Positive(t, credential.ID)
	rotationResponse := invoke(subjectID,
		http.MethodPost, fmt.Sprintf("/api/v1/access-grants/%s/credentials/%d/rotate", grantResult.Grant.GrantRef, credential.ID),
		`{}`, "legacy-put-grant-rotate",
		map[string]string{"ref": grantResult.Grant.GrantRef, "key_id": fmt.Sprint(credential.ID)}, handler.RotateAccessGrantCredential,
	)
	require.Equal(t, http.StatusCreated, rotationResponse.Code, rotationResponse.Body.String())
	var rotation accessprovisioning.RotationResult
	require.NoError(t, json.Unmarshal(rotationResponse.Body.Bytes(), &rotation))
	require.Positive(t, rotation.Credential.ID)
	require.Equal(t, credential.CredentialRef, rotation.Credential.CredentialRef)

	keyPathValues := map[string]string{"id": fmt.Sprint(credential.ID)}
	reactivate := invoke(actorID, http.MethodPut, "/api/v1/keys/"+fmt.Sprint(credential.ID),
		`{"status":"active"}`, "legacy-put-grant-reactivate", keyPathValues, handler.UpdateAPIKey)
	require.Equal(t, http.StatusConflict, reactivate.Code, reactivate.Body.String())
	require.Contains(t, reactivate.Body.String(), "grant_credential_reactivation_forbidden")

	var initialEpoch int64
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,auth_epoch FROM api_keys WHERE id=$1`, credential.ID).Scan(&status, &initialEpoch))
	require.Equal(t, "disabled", status)
	var successorInitialEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,auth_epoch FROM api_keys WHERE id=$1`, rotation.Credential.ID).Scan(&status, &successorInitialEpoch))
	require.Equal(t, "active", status)

	revoke := invoke(actorID, http.MethodPut, "/api/v1/keys/"+fmt.Sprint(credential.ID),
		`{"status":"disabled"}`, "legacy-put-grant-revoke", keyPathValues, handler.UpdateAPIKey)
	require.Equal(t, http.StatusOK, revoke.Code, revoke.Body.String())
	var revokedEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,auth_epoch FROM api_keys WHERE id=$1`, credential.ID).Scan(&status, &revokedEpoch))
	require.Equal(t, "disabled", status)
	require.Equal(t, initialEpoch, revokedEpoch)
	var successorStatus, familyStatus string
	var successorEpoch int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,auth_epoch FROM api_keys WHERE id=$1`, rotation.Credential.ID).Scan(&successorStatus, &successorEpoch))
	require.Equal(t, "disabled", successorStatus)
	require.Equal(t, successorInitialEpoch+1, successorEpoch)
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM access_credentials WHERE credential_ref=$1`, credential.CredentialRef).Scan(&familyStatus))
	require.Equal(t, "revoked", familyStatus)

	assertEvidenceCounts := func(expected int) {
		t.Helper()
		var operationCount, auditCount, outboxCount int
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM access_operations
			WHERE access_grant_id=$1 AND operation_kind='credential_revoke'`, grantResult.Grant.ID).Scan(&operationCount))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM audit_logs
			WHERE resource='access_credential' AND resource_id=$1
			  AND action='access_grant.credential.revoke'`, credential.CredentialRef).Scan(&auditCount))
		require.NoError(t, pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM internal_outbox_events e
			JOIN audit_logs a ON a.outbox_event_key=e.event_key
			WHERE a.resource='access_credential' AND a.resource_id=$1
			  AND a.action='access_grant.credential.revoke'`, credential.CredentialRef).Scan(&outboxCount))
		require.Equal(t, expected, operationCount)
		require.Equal(t, expected, auditCount)
		require.Equal(t, expected, outboxCount)
	}
	assertEvidenceCounts(1)

	putReplay := invoke(actorID, http.MethodPut, "/api/v1/keys/"+fmt.Sprint(credential.ID),
		`{"status":"disabled"}`, "legacy-put-grant-revoke", keyPathValues, handler.UpdateAPIKey)
	require.Equal(t, http.StatusOK, putReplay.Code, putReplay.Body.String())
	disableReplay := invoke(actorID, http.MethodPost, "/api/v1/keys/"+fmt.Sprint(credential.ID)+"/disable",
		"", "legacy-disable-grant-replay", keyPathValues, handler.DisableAPIKey)
	require.Equal(t, http.StatusConflict, disableReplay.Code, disableReplay.Body.String())
	assertEvidenceCounts(1)
	require.NoError(t, pool.QueryRow(ctx, `SELECT auth_epoch FROM api_keys WHERE id=$1`, credential.ID).Scan(&revokedEpoch))
	require.Equal(t, initialEpoch, revokedEpoch)
}

func TestLegacyCreateAPIKeyDelegatedPersonalProvisionRequiresOwnerSelfServiceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('legacy-key-name-race','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects (org_id,name,slug,status)
		VALUES ($1,'Legacy Key Name Race','legacy-key-name-race','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,'legacy-key-name-admin','hash','org_admin','org_admin','active') RETURNING id`, orgID).Scan(&actorID))
	seedSubject := func(username string) int64 {
		t.Helper()
		var userID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash','member','developer','active') RETURNING id`, orgID, username).Scan(&userID))
		_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, userID)
		require.NoError(t, err)
		return userID
	}
	subjectIDs := []int64{seedSubject("legacy-key-name-a"), seedSubject("legacy-key-name-b")}
	handler := NewHandlerWithAPIKeyHashSecret(
		pool, "legacy-key-name-jwt", "legacy-key-name-hash",
		[]byte("0123456789abcdef01234567"), nil,
	)
	newRequest := func(index int, subjectID int64) *http.Request {
		body := fmt.Sprintf(`{
			"org_id":%d,"project_id":%d,"owner_user_id":%d,
			"key_type":"personal","name":"same project credential name"
		}`, orgID, projectID, subjectID)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/keys", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(middleware.ClientRequestIDHeader, fmt.Sprintf("legacy-key-name-race-%d", index))
		return withIntegrationCommittingActor(t, pool, request, actorID)
	}

	start := make(chan struct{})
	responses := make([]*httptest.ResponseRecorder, len(subjectIDs))
	var wait sync.WaitGroup
	for index, subjectID := range subjectIDs {
		index, request := index, newRequest(index, subjectID)
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			response := httptest.NewRecorder()
			middleware.OperationIdentity(http.HandlerFunc(handler.CreateAPIKey)).ServeHTTP(response, request)
			responses[index] = response
		}()
	}
	close(start)
	wait.Wait()
	type selfServiceResponse struct {
		Status          string `json:"status"`
		Reason          string `json:"reason"`
		RequestRef      string `json:"request_ref"`
		RequestStatus   string `json:"request_status"`
		GrantRef        string `json:"grant_ref"`
		OwnerUserID     int64  `json:"owner_user_id"`
		SecretAvailable bool   `json:"secret_available"`
		Idempotent      bool   `json:"idempotent"`
	}
	initial := make([]selfServiceResponse, len(responses))
	for index, response := range responses {
		require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &initial[index]))
		require.Equal(t, "credential_self_service_required", initial[index].Status)
		require.Equal(t, "credential_self_service_required", initial[index].Reason)
		require.Equal(t, accessprovisioning.RequestApproved, initial[index].RequestStatus)
		require.Equal(t, subjectIDs[index], initial[index].OwnerUserID)
		require.NotEmpty(t, initial[index].RequestRef)
		require.NotEmpty(t, initial[index].GrantRef)
		require.False(t, initial[index].SecretAvailable)
		require.False(t, initial[index].Idempotent)
		require.NotContains(t, response.Body.String(), `"key":`)
	}
	require.NotEqual(t, initial[0].RequestRef, initial[1].RequestRef)
	require.NotEqual(t, initial[0].GrantRef, initial[1].GrantRef)

	var keyCount, familyCount, requestCount, grantCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM api_keys
		WHERE org_id=$1 AND project_id=$2 AND name='same project credential name'`, orgID, projectID).Scan(&keyCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM access_credentials WHERE org_id=$1 AND project_id=$2`, orgID, projectID).Scan(&familyCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM access_requests WHERE org_id=$1 AND project_id=$2`, orgID, projectID).Scan(&requestCount))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM access_grants WHERE org_id=$1 AND project_id=$2`, orgID, projectID).Scan(&grantCount))
	require.Zero(t, keyCount, "administrator must not receive another human subject's secret")
	require.Zero(t, familyCount, "delegated access grant must not partially create a credential family")
	require.Equal(t, 2, requestCount)
	require.Equal(t, 2, grantCount)

	for index, subjectID := range subjectIDs {
		replayRecorder := httptest.NewRecorder()
		middleware.OperationIdentity(http.HandlerFunc(handler.CreateAPIKey)).ServeHTTP(
			replayRecorder, newRequest(index, subjectID),
		)
		require.Equal(t, http.StatusAccepted, replayRecorder.Code, replayRecorder.Body.String())
		var replay selfServiceResponse
		require.NoError(t, json.Unmarshal(replayRecorder.Body.Bytes(), &replay))
		require.True(t, replay.Idempotent)
		require.Equal(t, initial[index].RequestRef, replay.RequestRef)
		require.Equal(t, initial[index].GrantRef, replay.GrantRef)
		require.False(t, replay.SecretAvailable)
	}
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_requests WHERE org_id=$1`, orgID).Scan(&requestCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_grants WHERE org_id=$1`, orgID).Scan(&grantCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE org_id=$1`, orgID).Scan(&keyCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_credentials WHERE org_id=$1`, orgID).Scan(&familyCount))
	require.Equal(t, 2, requestCount, "replay must not duplicate access requests")
	require.Equal(t, 2, grantCount, "replay must not duplicate Grants")
	require.Zero(t, keyCount)
	require.Zero(t, familyCount)
}

func TestFirstCrossProjectSelfRequestIsPendingActorOwnedAndApprovalCreatesAccessIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	var orgID, foreignOrgID, projectID, archivedProjectID, foreignProjectID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('first-cross-org','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('first-cross-foreign','active','customer') RETURNING id`).Scan(&foreignOrgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'First Cross','first-cross','active') RETURNING id`, orgID).Scan(&projectID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Archived Cross','archived-cross','archived') RETURNING id`, orgID).Scan(&archivedProjectID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects (org_id,name,slug,status) VALUES ($1,'Foreign Cross','foreign-cross','active') RETURNING id`, foreignOrgID).Scan(&foreignProjectID))

	seedUser := func(username string, targetOrgID int64, organizationRole, compatibilityRole string) int64 {
		t.Helper()
		var userID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			VALUES ($1,$2,'hash',$3,$4,'active') RETURNING id`,
			targetOrgID, username, organizationRole, compatibilityRole).Scan(&userID))
		return userID
	}
	applicantID := seedUser("first-cross-applicant", orgID, auth.RoleMember, auth.RoleMember)
	otherApplicantID := seedUser("first-cross-other", orgID, auth.RoleMember, auth.RoleMember)
	viewerApplicantID := seedUser("first-cross-viewer", orgID, auth.RoleMember, auth.RoleViewer)
	projectAdminID := seedUser("first-cross-admin", orgID, auth.RoleMember, auth.RoleProjectAdmin)
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_admin')`, projectID, projectAdminID)
		return err
	}())
	require.NoError(t, func() error {
		_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_viewer')`, projectID, viewerApplicantID)
		return err
	}())
	handler := NewHandlerWithAPIKeyHashSecret(pool, "first-cross-jwt-secret", "first-cross-key-hash-secret", []byte("0123456789abcdef01234567"), nil)

	createSelf := func(userID, targetOrgID, targetProjectID int64, clientID string) *httptest.ResponseRecorder {
		t.Helper()
		payload := fmt.Sprintf(`{
			"org_id":%d,"project_id":%d,
			"subject":{"type":"user","user_id":%d},
			"request_kind":"human_personal",
			"requested_scope":{"project_role":"project_user","scope_mode":"project_default","models":[],"endpoints":[]},
			"environment":"personal","production_use":false,"submission_mode":"self"
		}`, targetOrgID, targetProjectID, userID)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests", bytes.NewBufferString(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(middleware.ClientRequestIDHeader, clientID)
		request = withIntegrationCommittingActor(t, pool, request, userID)
		response := httptest.NewRecorder()
		middleware.OperationIdentity(http.HandlerFunc(handler.CreateAccessRequest)).ServeHTTP(response, request)
		return response
	}

	createdResponse := createSelf(applicantID, orgID, projectID, "first-cross-self")
	require.Equal(t, http.StatusCreated, createdResponse.Code, createdResponse.Body.String())
	var created accessprovisioning.ProvisionResult
	require.NoError(t, json.Unmarshal(createdResponse.Body.Bytes(), &created))
	require.Equal(t, accessprovisioning.RequestNeedsReview, created.Request.Status)
	require.Equal(t, []string{accessprovisioning.ReasonFirstCrossProject}, created.Request.PolicyReasonCodes)
	require.Nil(t, created.Grant)

	var membershipCount, grantCount, credentialCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, applicantID).Scan(&membershipCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_grants WHERE subject_user_id=$1`, applicantID).Scan(&grantCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE owner_user_id=$1`, applicantID).Scan(&credentialCount))
	require.Zero(t, membershipCount)
	require.Zero(t, grantCount)
	require.Zero(t, credentialCount)

	otherResponse := createSelf(otherApplicantID, orgID, projectID, "first-cross-other")
	require.Equal(t, http.StatusCreated, otherResponse.Code, otherResponse.Body.String())
	newerResponse := createSelf(applicantID, orgID, projectID, "first-cross-self-newer")
	require.Equal(t, http.StatusCreated, newerResponse.Code, newerResponse.Body.String())
	var newer accessprovisioning.ProvisionResult
	require.NoError(t, json.Unmarshal(newerResponse.Body.Bytes(), &newer))

	myRequest := httptest.NewRequest(http.MethodGet, "/api/v1/access-requests?scope=my&limit=1", nil)
	myRequest = withIntegrationCommittingActor(t, pool, myRequest, applicantID)
	myResponse := httptest.NewRecorder()
	handler.ListAccessRequests(myResponse, myRequest)
	require.Equal(t, http.StatusOK, myResponse.Code, myResponse.Body.String())
	var myList struct {
		Data       []accessprovisioning.Request `json:"data"`
		Pagination accessprovisioning.PageInfo  `json:"pagination"`
		Scope      string                       `json:"scope"`
	}
	require.NoError(t, json.Unmarshal(myResponse.Body.Bytes(), &myList))
	require.Equal(t, "my", myList.Scope)
	require.Equal(t, 1, myList.Pagination.Limit)
	require.True(t, myList.Pagination.HasMore)
	require.Equal(t, newer.Request.ID, *myList.Pagination.NextAfterID)
	require.Len(t, myList.Data, 1)
	require.Equal(t, newer.Request.RequestRef, myList.Data[0].RequestRef)

	nextMyRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf(
		"/api/v1/access-requests?scope=my&limit=1&after_id=%d", *myList.Pagination.NextAfterID), nil)
	nextMyRequest = withIntegrationCommittingActor(t, pool, nextMyRequest, applicantID)
	nextMyResponse := httptest.NewRecorder()
	handler.ListAccessRequests(nextMyResponse, nextMyRequest)
	require.Equal(t, http.StatusOK, nextMyResponse.Code, nextMyResponse.Body.String())
	var nextMyList accessprovisioning.RequestPage
	require.NoError(t, json.Unmarshal(nextMyResponse.Body.Bytes(), &nextMyList))
	require.Len(t, nextMyList.Data, 1)
	require.Equal(t, created.Request.RequestRef, nextMyList.Data[0].RequestRef)
	require.False(t, nextMyList.Pagination.HasMore)
	require.Nil(t, nextMyList.Pagination.NextAfterID)

	invalidPageRequest := httptest.NewRequest(http.MethodGet, "/api/v1/access-requests?scope=my&limit=101", nil)
	invalidPageRequest = withIntegrationCommittingActor(t, pool, invalidPageRequest, applicantID)
	invalidPageResponse := httptest.NewRecorder()
	handler.ListAccessRequests(invalidPageResponse, invalidPageRequest)
	require.Equal(t, http.StatusBadRequest, invalidPageResponse.Code, invalidPageResponse.Body.String())

	readRequest := httptest.NewRequest(http.MethodGet, "/api/v1/access-requests/"+created.Request.RequestRef, nil)
	readRequest.SetPathValue("ref", created.Request.RequestRef)
	readRequest = withIntegrationCommittingActor(t, pool, readRequest, applicantID)
	readResponse := httptest.NewRecorder()
	handler.GetAccessRequest(readResponse, readRequest)
	require.Equal(t, http.StatusOK, readResponse.Code, readResponse.Body.String())

	var beforeRequests, beforeAudits, beforeOutbox int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_requests`).Scan(&beforeRequests))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='access_request.provision'`).Scan(&beforeAudits))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM internal_outbox_events e
		JOIN audit_logs a ON a.outbox_event_key=e.event_key
		WHERE a.action='access_request.provision'`).Scan(&beforeOutbox))
	var referenceCode int
	var referenceBody string
	for index, targetProject := range []int64{foreignProjectID, archivedProjectID, projectID + foreignProjectID + 999999} {
		probe := createSelf(applicantID, orgID, targetProject, fmt.Sprintf("first-cross-probe-%d", index))
		if index == 0 {
			referenceCode, referenceBody = probe.Code, probe.Body.String()
		}
		require.Equal(t, referenceCode, probe.Code)
		require.JSONEq(t, referenceBody, probe.Body.String())
	}
	require.Equal(t, http.StatusNotFound, referenceCode)
	var afterRequests, afterAudits, afterOutbox int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_requests`).Scan(&afterRequests))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='access_request.provision'`).Scan(&afterAudits))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM internal_outbox_events e
		JOIN audit_logs a ON a.outbox_event_key=e.event_key
		WHERE a.action='access_request.provision'`).Scan(&afterOutbox))
	require.Equal(t, beforeRequests, afterRequests)
	require.Equal(t, beforeAudits, afterAudits)
	require.Equal(t, beforeOutbox, afterOutbox)

	decisionPayload := fmt.Sprintf(`{"expected_revision":%d,"decision":"approve"}`, created.Request.Revision)
	decisionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests/"+created.Request.RequestRef+"/decision", bytes.NewBufferString(decisionPayload))
	decisionRequest.SetPathValue("ref", created.Request.RequestRef)
	decisionRequest.Header.Set("Content-Type", "application/json")
	decisionRequest.Header.Set(middleware.ClientRequestIDHeader, "first-cross-decision")
	decisionRequest = withIntegrationCommittingActor(t, pool, decisionRequest, projectAdminID)
	decisionResponse := httptest.NewRecorder()
	middleware.OperationIdentity(http.HandlerFunc(handler.DecideAccessRequest)).ServeHTTP(decisionResponse, decisionRequest)
	require.Equal(t, http.StatusOK, decisionResponse.Code, decisionResponse.Body.String())
	var decided accessprovisioning.ProvisionResult
	require.NoError(t, json.Unmarshal(decisionResponse.Body.Bytes(), &decided))
	require.Equal(t, accessprovisioning.RequestApproved, decided.Request.Status)
	require.NotNil(t, decided.Grant)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE project_id=$1 AND user_id=$2 AND role='project_user'`, projectID, applicantID).Scan(&membershipCount))
	require.Equal(t, 1, membershipCount)

	viewerResponse := createSelf(viewerApplicantID, orgID, projectID, "viewer-role-elevation")
	require.Equal(t, http.StatusCreated, viewerResponse.Code, viewerResponse.Body.String())
	var viewerPending accessprovisioning.ProvisionResult
	require.NoError(t, json.Unmarshal(viewerResponse.Body.Bytes(), &viewerPending))
	require.Equal(t, accessprovisioning.RequestNeedsReview, viewerPending.Request.Status)
	require.Equal(t, []string{accessprovisioning.ReasonRoleElevation}, viewerPending.Request.PolicyReasonCodes)
	require.Nil(t, viewerPending.Grant)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM access_grants WHERE subject_user_id=$1`, viewerApplicantID).Scan(&grantCount))
	require.Zero(t, grantCount)
	var viewerRole string
	require.NoError(t, pool.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, viewerApplicantID).Scan(&viewerRole))
	require.Equal(t, auth.ProjectRoleViewer, viewerRole)

	viewerDecisionPayload := fmt.Sprintf(`{"expected_revision":%d,"decision":"approve"}`, viewerPending.Request.Revision)
	viewerDecisionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/access-requests/"+viewerPending.Request.RequestRef+"/decision", bytes.NewBufferString(viewerDecisionPayload))
	viewerDecisionRequest.SetPathValue("ref", viewerPending.Request.RequestRef)
	viewerDecisionRequest.Header.Set("Content-Type", "application/json")
	viewerDecisionRequest.Header.Set(middleware.ClientRequestIDHeader, "viewer-role-elevation-decision")
	viewerDecisionRequest = withIntegrationCommittingActor(t, pool, viewerDecisionRequest, projectAdminID)
	viewerDecisionResponse := httptest.NewRecorder()
	middleware.OperationIdentity(http.HandlerFunc(handler.DecideAccessRequest)).ServeHTTP(viewerDecisionResponse, viewerDecisionRequest)
	require.Equal(t, http.StatusOK, viewerDecisionResponse.Code, viewerDecisionResponse.Body.String())
	require.NoError(t, pool.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, viewerApplicantID).Scan(&viewerRole))
	require.Equal(t, auth.ProjectRoleUser, viewerRole)
}
