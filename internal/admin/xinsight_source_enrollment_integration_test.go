//go:build integration

package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

func TestXInsightFutureSourceEnrollmentACReadAndRevokeIntegration(t *testing.T) {
	f := newContentControlIntegrationFixture(t)
	ctx := context.Background()
	const installation = "xci_bbbbbbbbbbbbbbbbbbbbbb"
	_, err := f.pool.Exec(ctx, `INSERT INTO xinsight_connector_installations
 (installation_id,org_id,project_id,x_deployment_id,invitation_sha256,event_scope,source_instance,source_binding_sha256,
 desired_state,observed_state,consented_by) VALUES($1,$2,$3,'xdep_source_demo',$4,'model.request.completed',$5,$6,'active','connected',$7)`,
		installation, f.orgID, f.projectID, "sha256:"+strings.Repeat("a", 64), "synthetic-source-control", "sha256:"+strings.Repeat("b", 64), f.actorID)
	require.NoError(t, err)
	credential := "ur-xse1-" + strings.Repeat("d", 64)
	verifier := sha256.Sum256([]byte(auth.XInsightEnrollmentVerifierDomain + "\x00" + credential))
	registration := machineCredentialRegistration{InstallationID: installation, Purpose: sourceEnrollPurpose,
		KeyPrefix: credential[:20], VerifierSHA256: hex.EncodeToString(verifier[:]), OperationID: strings.Repeat("e", 32), AllowedIP: "127.0.0.1/32"}
	wire, _ := json.Marshal(registration)
	r := f.request(t, "source-enrollment-machine", json.RawMessage(wire))
	r.SetPathValue("org_id", stringInt64(f.orgID))
	w := httptest.NewRecorder()
	f.handler.RegisterXInsightMachineCredential(w, r)
	require.Equal(t, 201, w.Code, w.Body.String())
	var machine map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &machine))
	r = f.request(t, "source-enrollment-activate", map[string]any{})
	r.SetPathValue("org_id", stringInt64(f.orgID))
	r.SetPathValue("key_id", machine["key_id"].(string))
	w = httptest.NewRecorder()
	f.handler.ActivateXInsightMachineCredential(w, r)
	require.Equal(t, 200, w.Code, w.Body.String())
	profiles := []string{sourcefacts.Profiles()[0]}
	grant := sourceEnrollmentInput{InstallationID: installation, Coverage: "all_current_future", Profiles: profiles, IncludeContent: true}
	r = f.request(t, "source-enrollment-grant", grant)
	r.SetPathValue("org_id", stringInt64(f.orgID))
	w = httptest.NewRecorder()
	f.handler.CreateXInsightSourceEnrollment(w, r)
	require.Equal(t, 201, w.Code, w.Body.String())
	var grantBody map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &grantBody))
	grantID := grantBody["enrollment_id"].(string)
	var projectID int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period)
 VALUES($1,'Auto enrollment future','auto-enrollment-future','active','none') RETURNING id`, f.orgID).Scan(&projectID))
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	gate := middleware.XInsightMachineAuth(f.pool, syntheticSourceControlSecret, sourceEnrollPurpose, resolver)
	discover := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/integrations/xinsight/source-enrollments/projects", nil)
		req.Header.Set("Authorization", "Bearer "+credential)
		req.RemoteAddr = "127.0.0.1:55555"
		response := httptest.NewRecorder()
		gate(http.HandlerFunc(f.handler.ListXInsightMachineEnrollmentProjects)).ServeHTTP(response, req)
		return response
	}
	discovered := discover()
	require.Equal(t, 200, discovered.Code, discovered.Body.String())
	require.Contains(t, discovered.Body.String(), `"project_id":"`+strconv.FormatInt(projectID, 10)+`"`)
	require.Contains(t, discovered.Body.String(), `"facts_status":"pending"`)
	registerTarget := func(targetProject int64, kind string, sourceRegistration sourcefacts.Registration) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(machineEnrollmentRegistration{EnrollmentID: grantID, Kind: kind, Registration: sourceRegistration})
		req := httptest.NewRequest("POST", "/api/v1/integrations/xinsight/source-enrollments/projects/"+strconv.FormatInt(targetProject, 10)+"/registrations", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+credential)
		req.RemoteAddr = "127.0.0.1:55555"
		req.SetPathValue("project_id", strconv.FormatInt(targetProject, 10))
		response := httptest.NewRecorder()
		gate(http.HandlerFunc(f.handler.RegisterXInsightSourceEnrollmentProject)).ServeHTTP(response, req)
		return response
	}
	register := func(kind string, sourceRegistration sourcefacts.Registration) *httptest.ResponseRecorder {
		return registerTarget(projectID, kind, sourceRegistration)
	}
	factsRegistration, factsToken := f.registration(t, "future-facts", profiles)
	factsRegistration.ProjectID = strconv.FormatInt(projectID, 10)
	var otherOrg, otherProject int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('Other enrollment org','active','standard') RETURNING id`).Scan(&otherOrg))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period)
 VALUES($1,'Other enrollment project','other-enrollment-project','active','none') RETURNING id`, otherOrg).Scan(&otherProject))
	wrongOrg := factsRegistration
	wrongOrg.OrgID = strconv.FormatInt(otherOrg, 10)
	wrongOrg.ProjectID = strconv.FormatInt(otherProject, 10)
	require.Equal(t, 400, registerTarget(otherProject, "facts", wrongOrg).Code)
	wrongOrg.OrgID = strconv.FormatInt(f.orgID, 10)
	require.Equal(t, 403, registerTarget(otherProject, "facts", wrongOrg).Code)
	require.Equal(t, 201, register("facts", factsRegistration).Code)
	replay := register("facts", factsRegistration)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	contentRegistration, contentToken := f.registration(t, "future-content", []string{sourcefacts.ContentProfile})
	contentRegistration.ProjectID = strconv.FormatInt(projectID, 10)
	contentResult := register("content", contentRegistration)
	require.Equal(t, 201, contentResult.Code, contentResult.Body.String())
	discovered = discover()
	require.Equal(t, 200, discovered.Code, discovered.Body.String())
	require.Contains(t, discovered.Body.String(), `"facts_status":"active"`)
	require.Contains(t, discovered.Body.String(), `"content_status":"active"`)
	readFacts := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/v1/integrations/xinsight/source-facts/capabilities", nil)
		req.Header.Set("Authorization", "Bearer "+factsToken)
		req.RemoteAddr = "127.0.0.1:55555"
		response := httptest.NewRecorder()
		f.source.Handler(resolver).ServeHTTP(response, req)
		return response
	}
	require.Equal(t, 200, readFacts().Code, readFacts().Body.String())
	contentAuthority, err := f.source.NewContentBodyAuthority(resolver)
	require.NoError(t, err)
	contentRequest := httptest.NewRequest("GET", "/api/v1/integrations/xinsight/source-content", nil)
	contentRequest.Header.Set("Authorization", "Bearer "+contentToken)
	contentRequest.RemoteAddr = "127.0.0.1:55555"
	_, failure := contentAuthority.Authenticate(ctx, contentRequest)
	require.Nil(t, failure)
	_, err = f.pool.Exec(ctx, `UPDATE users SET organization_role='member',role='member' WHERE id=$1`, f.actorID)
	require.NoError(t, err)
	require.Equal(t, 401, readFacts().Code)
	_, failure = contentAuthority.Authenticate(ctx, contentRequest)
	require.NotNil(t, failure)
	require.Equal(t, 401, failure.Status)
	discovered = discover()
	require.Equal(t, 200, discovered.Code, discovered.Body.String())
	require.NotContains(t, discovered.Body.String(), `"project_id":"`+strconv.FormatInt(projectID, 10)+`"`)
	_, err = f.pool.Exec(ctx, `UPDATE users SET organization_role='platform_admin',role='platform_admin' WHERE id=$1`, f.actorID)
	require.NoError(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE projects SET status='disabled' WHERE id=$1`, projectID)
	require.NoError(t, err)
	require.Equal(t, 401, readFacts().Code)
	_, failure = contentAuthority.Authenticate(ctx, contentRequest)
	require.NotNil(t, failure)
	require.Equal(t, 401, failure.Status)
	discovered = discover()
	require.Equal(t, 200, discovered.Code, discovered.Body.String())
	require.NotContains(t, discovered.Body.String(), `"project_id":"`+strconv.FormatInt(projectID, 10)+`"`)
	_, err = f.pool.Exec(ctx, `UPDATE projects SET status='active' WHERE id=$1`, projectID)
	require.NoError(t, err)
	setExclusion := func(excluded bool, expected string) *httptest.ResponseRecorder {
		t.Helper()
		req := f.request(t, "source-enrollment-exclusion", map[string]any{"excluded": excluded, "expected_revision": expected})
		req.SetPathValue("org_id", stringInt64(f.orgID))
		req.SetPathValue("enrollment_id", grantID)
		req.SetPathValue("project_id", strconv.FormatInt(projectID, 10))
		response := httptest.NewRecorder()
		f.handler.PutXInsightSourceEnrollmentExclusion(response, req)
		return response
	}
	require.Equal(t, 200, setExclusion(true, "0").Code)
	require.Equal(t, 409, setExclusion(false, "0").Code)
	require.Equal(t, 401, readFacts().Code)
	_, failure = contentAuthority.Authenticate(ctx, contentRequest)
	require.NotNil(t, failure)
	require.Equal(t, 401, failure.Status)
	discovered = discover()
	require.Equal(t, 200, discovered.Code, discovered.Body.String())
	require.NotContains(t, discovered.Body.String(), `"project_id":"`+strconv.FormatInt(projectID, 10)+`"`)
	require.Equal(t, 200, setExclusion(false, "1").Code)
	require.Equal(t, 200, readFacts().Code)
	// Revocation leaves historical rows intact while blocking both future reads.
	r = f.request(t, "source-enrollment-revoke", map[string]any{"expected_revision": "1"})
	r.SetPathValue("org_id", stringInt64(f.orgID))
	r.SetPathValue("enrollment_id", grantID)
	w = httptest.NewRecorder()
	f.handler.RevokeXInsightSourceEnrollment(w, r)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 401, readFacts().Code)
	_, failure = contentAuthority.Authenticate(ctx, contentRequest)
	require.NotNil(t, failure)
	require.Equal(t, 401, failure.Status)
	discovered = discover()
	require.Equal(t, 200, discovered.Code, discovered.Body.String())
	require.NotContains(t, discovered.Body.String(), `"project_id":"`+strconv.FormatInt(projectID, 10)+`"`)
}
