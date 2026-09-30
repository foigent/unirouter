//go:build integration

package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/middleware"
)

func TestXInsightMachinePolicyDelegationCASAndReplayIntegration(t *testing.T) {
	f := newSourceControlFixture(t)
	contentPolicyIntegrationStore(t, f, contentcapture.ModeMetadataOnly)
	ctx := context.Background()
	const installation = "xci_aaaaaaaaaaaaaaaaaaaaaa"
	_, err := f.pool.Exec(ctx, `INSERT INTO xinsight_connector_installations
 (installation_id,org_id,project_id,x_deployment_id,invitation_sha256,event_scope,source_instance,source_binding_sha256,
 desired_state,observed_state,consented_by) VALUES($1,$2,$3,'xdep_demo',$4,'model.request.completed',$5,$6,'active','connected',$7)`,
		installation, f.orgID, f.projectID, "sha256:"+strings.Repeat("a", 64), "synthetic-source-control", "sha256:"+strings.Repeat("b", 64), f.actorID)
	require.NoError(t, err)
	human := func(method, path string, body any, handler http.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		wire, _ := json.Marshal(body)
		r := f.request(t, "policy-machine", json.RawMessage(wire))
		actual, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, string(wire), string(actual))
		r.Body = io.NopCloser(strings.NewReader(string(actual)))
		r.Method = method
		r.URL.Path = path
		r.SetPathValue("org_id", stringInt64(f.orgID))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	credential := "ur-xpm1-" + strings.Repeat("c", 64)
	verifier := sha256.Sum256([]byte(auth.XInsightPolicyVerifierDomain + "\x00" + credential))
	registration := machineCredentialRegistration{InstallationID: installation, Purpose: policyManagePurpose,
		KeyPrefix: credential[:20], VerifierSHA256: hex.EncodeToString(verifier[:]), OperationID: strings.Repeat("d", 32), AllowedIP: "127.0.0.1/32"}
	registrationWire, err := json.Marshal(registration)
	require.NoError(t, err)
	require.True(t, exactMachineCredentialFields(registrationWire), string(registrationWire))
	require.Nil(t, eventconnector.ValidateStrictJSON(registrationWire, eventconnector.StrictJSONOptions{RejectNull: true}))
	registered := human("POST", "/api/v2/organizations/1/xinsight-policy-credentials", registration, f.handler.RegisterXInsightMachineCredential)
	require.Equal(t, 201, registered.Code, registered.Body.String())
	var registeredBody map[string]any
	require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &registeredBody))
	keyID := registeredBody["key_id"].(string)
	replayed := human("POST", "/api/v2/organizations/1/xinsight-policy-credentials", registration, f.handler.RegisterXInsightMachineCredential)
	require.Equal(t, 200, replayed.Code, replayed.Body.String())
	require.Contains(t, replayed.Body.String(), keyID)
	activateReq := f.request(t, "credential-activate", map[string]any{})
	activateReq.SetPathValue("org_id", stringInt64(f.orgID))
	activateReq.SetPathValue("key_id", keyID)
	activateResponse := httptest.NewRecorder()
	f.handler.ActivateXInsightMachineCredential(activateResponse, activateReq)
	require.Equal(t, 200, activateResponse.Code, activateResponse.Body.String())
	grant := policyDelegationInput{InstallationID: installation, TargetKind: "enterprise_default", Coverage: "exact", Read: true, Write: true}
	granted := human("POST", "/api/v2/organizations/1/xinsight-policy-delegations", grant, f.handler.CreateXInsightPolicyDelegation)
	require.Equal(t, 201, granted.Code, granted.Body.String())
	var grantBody map[string]any
	require.NoError(t, json.Unmarshal(granted.Body.Bytes(), &grantBody))
	grantID := grantBody["delegation_id"].(string)
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	gate := middleware.XInsightMachineAuth(f.pool, syntheticSourceControlSecret, policyManagePurpose, resolver)
	get := func(method, path string, body any, handler http.Handler) *httptest.ResponseRecorder {
		t.Helper()
		wire, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(wire)))
		r.Header.Set("Authorization", "Bearer "+credential)
		r.RemoteAddr = "127.0.0.1:55555"
		r.SetPathValue("org_id", stringInt64(f.orgID))
		w := httptest.NewRecorder()
		gate(handler).ServeHTTP(w, r)
		return w
	}
	viewResponse := get("GET", "/api/v1/integrations/xinsight/payload-policy/organizations/"+stringInt64(f.orgID)+"?delegation_id="+grantID, nil, f.handler.GetXInsightMachinePolicy(contentcapture.PolicyScopeEnterprise))
	require.Equal(t, 200, viewResponse.Code, viewResponse.Body.String())
	var view contentcapture.ScopedManagementView
	require.NoError(t, json.Unmarshal(viewResponse.Body.Bytes(), &view))
	require.False(t, view.Enterprise.Explicit)
	require.Equal(t, []string{"openai.chat"}, view.PolicyCapabilities.SupportedProtocols)
	require.Equal(t, []string{"client_request"}, view.PolicyCapabilities.ObservationPoints)
	put := contentcapture.ScopedManagementPut{SchemaVersion: contentcapture.ScopedPolicyManagementSchema,
		ExpectedPolicyRef: view.Enterprise.PolicyRef, ExpectedVersion: "0", ExpectedVector: view.VersionVector,
		Desired: contentcapture.PolicyConfig{Mode: contentcapture.ModeMetadataOnly, SupportedProtocols: []string{"openai.chat"},
			ObservationPoints: []string{"client_request"}, AllowedPurposes: []string{}}, ExecutionStorageMode: "temporary_allowed"}
	quota := int64(1024)
	put.ProjectQuotaBytes = &quota
	envelope := machinePolicyWrite{SchemaVersion: "unirouter.xinsight.policy-write.v1", OperationRef: "xipw_" + strings.Repeat("e", 32),
		InstallationID: installation, SourceInstance: "synthetic-source-control", DelegationID: grantID, ExpectedDelegationRevision: "1",
		XIOperator: machinePolicyOperator{Kind: "human", SubjectRef: "xs_demo", XIRequestRef: "xi_request_1"}}
	envelope.Policy, _ = json.Marshal(put)
	path := "/api/v1/integrations/xinsight/payload-policy/organizations/" + stringInt64(f.orgID)
	written := get("PUT", path, envelope, f.handler.PutXInsightMachinePolicy(contentcapture.PolicyScopeEnterprise))
	require.Equal(t, 200, written.Code, written.Body.String())
	retry := get("PUT", path, envelope, f.handler.PutXInsightMachinePolicy(contentcapture.PolicyScopeEnterprise))
	require.Equal(t, 200, retry.Code, retry.Body.String())
	changed := envelope
	changed.XIOperator.XIRequestRef = "different"
	conflict := get("PUT", path, changed, f.handler.PutXInsightMachinePolicy(contentcapture.PolicyScopeEnterprise))
	require.Equal(t, 409, conflict.Code, conflict.Body.String())
	var count int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='content.policy.machine.update' AND org_id=$1`, f.orgID).Scan(&count))
	require.Equal(t, 1, count)
	// A future project becomes discoverable under the existing explicit grant.
	futureGrant := policyDelegationInput{InstallationID: installation, TargetKind: "project_policy", Coverage: "all_current_future", Read: true}
	futureGranted := human("POST", "/api/v2/organizations/1/xinsight-policy-delegations", futureGrant, f.handler.CreateXInsightPolicyDelegation)
	require.Equal(t, 201, futureGranted.Code, futureGranted.Body.String())
	var futureProject int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period)
 VALUES($1,'Future project','future-project','active','none') RETURNING id`, f.orgID).Scan(&futureProject))
	scopes := get("GET", "/api/v1/integrations/xinsight/payload-policy/scopes", nil, http.HandlerFunc(f.handler.GetXInsightMachinePolicyScopes))
	require.Equal(t, 200, scopes.Code, scopes.Body.String())
	require.Contains(t, scopes.Body.String(), `"project_id":"`+strconv.FormatInt(futureProject, 10)+`"`)
	// A project grant never grants access to its users.
	usersReq := httptest.NewRequest("GET", "/api/v1/integrations/xinsight/payload-policy/projects/"+strconv.FormatInt(futureProject, 10)+"/users", nil)
	usersReq.Header.Set("Authorization", "Bearer "+credential)
	usersReq.RemoteAddr = "127.0.0.1:55555"
	usersReq.SetPathValue("project_id", strconv.FormatInt(futureProject, 10))
	usersResp := httptest.NewRecorder()
	gate(http.HandlerFunc(f.handler.GetXInsightMachinePolicyUsers)).ServeHTTP(usersResp, usersReq)
	require.Equal(t, 404, usersResp.Code, usersResp.Body.String())
	_, err = f.pool.Exec(ctx, `UPDATE projects SET status='disabled' WHERE id=$1`, futureProject)
	require.NoError(t, err)
	scopes = get("GET", "/api/v1/integrations/xinsight/payload-policy/scopes", nil, http.HandlerFunc(f.handler.GetXInsightMachinePolicyScopes))
	require.Equal(t, 200, scopes.Code, scopes.Body.String())
	require.NotContains(t, scopes.Body.String(), `"project_id":"`+strconv.FormatInt(futureProject, 10)+`"`)
	_, err = f.pool.Exec(ctx, `UPDATE projects SET status='active' WHERE id=$1`, futureProject)
	require.NoError(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE users SET organization_role='member',role='member' WHERE id=$1`, f.actorID)
	require.NoError(t, err)
	denied := get("PUT", path, envelope, f.handler.PutXInsightMachinePolicy(contentcapture.PolicyScopeEnterprise))
	require.Equal(t, 403, denied.Code, denied.Body.String())
	scopes = get("GET", "/api/v1/integrations/xinsight/payload-policy/scopes", nil, http.HandlerFunc(f.handler.GetXInsightMachinePolicyScopes))
	require.Equal(t, 200, scopes.Code, scopes.Body.String())
	require.NotContains(t, scopes.Body.String(), `"project_id":"`+strconv.FormatInt(futureProject, 10)+`"`)
	_, err = f.pool.Exec(ctx, `UPDATE users SET organization_role='platform_admin',role='platform_admin',status='disabled' WHERE id=$1`, f.actorID)
	require.NoError(t, err)
	scopes = get("GET", "/api/v1/integrations/xinsight/payload-policy/scopes", nil, http.HandlerFunc(f.handler.GetXInsightMachinePolicyScopes))
	require.Equal(t, 200, scopes.Code, scopes.Body.String())
	require.NotContains(t, scopes.Body.String(), `"project_id":"`+strconv.FormatInt(futureProject, 10)+`"`)
}
