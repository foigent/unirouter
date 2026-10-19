//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type normalSetupFixture struct {
	sourceControlFixture
	invitation xinsightconnector.FrozenInvitationV1
	consent    xInsightSetupXIConsent
	journal    *xinsightconnector.PostgresPhaseJournal
	peer       *httptest.Server
	resolved   *xInsightSetupResolved
}

func newNormalSetupFixture(t *testing.T) normalSetupFixture {
	t.Helper()
	f := normalSetupFixture{sourceControlFixture: newContentControlIntegrationFixture(t)}
	enableEnrollmentObservations(t, &f.sourceControlFixture)
	contentPolicyIntegrationStore(t, f.sourceControlFixture, contentcapture.ModeMetadataOnly)
	service, e := completedfeed.NewService(f.pool, syntheticSourceControlSecret, f.source.SourceInstance(), true)
	require.NoError(t, e)
	f.handler.SetCompletedFeedController(service)
	manager, e := xinsightconnector.NewManager(f.pool, xinsightconnector.CommitAuthorizerFunc(f.handler.AuthorizeXInsightConnectorControl), service)
	require.NoError(t, e)
	f.handler.SetXInsightConnectorManager(manager)
	f.journal, e = xinsightconnector.NewPostgresPhaseJournal(f.pool, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, e)
	f.resolved = &xInsightSetupResolved{}
	expires := time.Now().UTC().Add(15 * time.Minute).Truncate(time.Second)
	f.peer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/connector-setups/resolve", r.URL.Path)
		require.Equal(t, "POST", r.Method)
		require.Empty(t, r.Header.Get("Cookie"))
		require.Empty(t, r.Header.Get("Authorization"))
		var input struct {
			Token string `json:"invitation_token"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
		require.Equal(t, cp2bInvitation, input.Token)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.resolved)
	}))
	t.Cleanup(f.peer.Close)
	fragment := url.Values{"invitation": {cp2bInvitation}, "xinsight_origin": {f.peer.URL}}
	f.invitation = xinsightconnector.FrozenInvitationV1{SchemaVersion: xinsightconnector.FrozenInvitationSchemaVersion, InvitationID: "xii_AAAAAAAAAAAAAAAAAAAAAA", InstallationID: cp2bInstallationID, DeploymentID: cp2bDeploymentID, Audience: "unirouter", XInsightOrigin: f.peer.URL, RequestedEventScope: []string{xinsightconnector.EventScopeModelCompleted}, ExpiresAt: expires.Format(time.RFC3339), InstallURL: "https://ur.setup.example/connectors/x-insight/install#" + fragment.Encode()}
	f.consent = xInsightSetupXIConsent{SchemaVersion: "xinsight.connector-setup-consent-view.v1", InstallationID: cp2bInstallationID, DeploymentID: cp2bDeploymentID, XIOrigin: f.peer.URL, UROrigin: "https://ur.setup.example", ConsentRevision: 1, ConsentSHA256: cp2bSHA("synthetic-XI-consent"), ExpiresAt: expires, Custody: xInsightSetupCustody{Mode: "disabled", RetentionSeconds: 2592000, ProjectQuotaBytes: 5368709120, MaxItemBytes: 33554432}, ExpectedCustodyRevision: 0}
	f.consent.ConsentSHA256 = ""
	f.consent.ConsentSHA256 = xinsightconnector.PublicCanonicalSHA256(f.consent)
	*f.resolved = xInsightSetupResolved{Invitation: f.invitation, Consent: f.consent}
	intake, e := xinsightconnector.NewInvitationIntake(manager, f.journal, f.peer.URL, "stable")
	require.NoError(t, e)
	f.handler.SetXInsightConnectorInvitationIntake(intake)
	digest, e := config.XInsightManagedFeedNetworkDigest(f.consent.UROrigin)
	require.NoError(t, e)
	peer := config.XInsightConnectorStaticPeer{XInsightOrigin: f.peer.URL, XDeploymentID: cp2bDeploymentID, UROrigin: f.consent.UROrigin, AllowedIP: "127.0.0.1/32", NetworkBindingSHA256: digest}
	require.NoError(t, f.handler.SetXInsightSetupPeer(&peer, f.peer.Client(), nil))
	return f
}
func (f normalSetupFixture) human(t *testing.T, action string, body any, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := f.request(t, "normal-setup-"+action, body)
	r.SetPathValue("org_id", stringInt64(f.orgID))
	r.SetPathValue("installation_id", cp2bInstallationID)
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}
func (f normalSetupFixture) preview(t *testing.T) xInsightSetupPreview {
	w := f.human(t, "preview", map[string]any{"project_id": stringInt64(f.projectID), "invitation_token": cp2bInvitation}, f.handler.PreviewXInsightSetup)
	require.Equal(t, 200, w.Code, w.Body.String())
	var out xInsightSetupPreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}
func (f normalSetupFixture) put(preview xInsightSetupPreview) xInsightSetupConsentPut {
	return xInsightSetupConsentPut{SchemaVersion: setupConsentSchema, RequestID: "e7c21f56-456c-48e1-9357-c6d161b4d225", ProjectID: stringInt64(f.projectID), Invitation: f.invitation, ExpectedPreviewSHA256: preview.PreviewSHA256, ExpectedXIConsentSHA256: preview.XIConsent.ConsentSHA256, ReceiverMode: preview.ReceiverMode, ExistingBindingSHA256: preview.ExistingBindingSHA256, Coverage: "all_current_future", ExcludedProjectIDs: []string{}, Profiles: preview.DefaultProfiles, PolicyAccess: []xInsightSetupPolicyAccess{{TargetKind: "enterprise_default", Coverage: "exact", Read: true, Write: true}, {TargetKind: "project_policy", Coverage: "all_current_future", Read: true, Write: true}}}
}
func TestXInsightNormalSetupAtomicConsentReplayAndFreshAuthorityIntegration(t *testing.T) {
	f := newNormalSetupFixture(t)
	ctx := context.Background()
	preview := f.preview(t)
	require.Equal(t, sourceEnrollmentEightProfiles(), preview.DefaultProfiles)
	require.Contains(t, preview.AllowedCoverage, "all_current_future")
	require.Len(t, preview.AllowedPolicyAccess, 5)
	put := f.put(preview)
	w := f.human(t, "create", put, f.handler.CreateXInsightSetup)
	require.Equal(t, 202, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), cp2bInvitation)
	require.NotContains(t, w.Body.String(), "verifier_sha256")
	var raw []byte
	var encrypted string
	var operationCount, consentAudits int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT i.setup_consent_canonical,b.invitation_token_enc FROM xinsight_connector_installations i JOIN xinsight_connector_control_bindings b USING(installation_id) WHERE i.installation_id=$1`, cp2bInstallationID).Scan(&raw, &encrypted))
	require.NotEmpty(t, encrypted)
	require.NotContains(t, string(raw), cp2bInvitation)
	require.NotContains(t, string(raw), "install_url")
	f.peer.Close() // Exact replay survives lost resolve response/closed browser.
	w = f.human(t, "replay", put, f.handler.CreateXInsightSetup)
	require.Equal(t, 202, w.Code, w.Body.String())
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM xinsight_connector_operations`).Scan(&operationCount))
	require.Equal(t, 1, operationCount)
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='data_exchange.xinsight.setup.consent'`).Scan(&consentAudits))
	require.Equal(t, 1, consentAudits)
	changed := put
	changed.IncludeContent = true
	w = f.human(t, "changed-replay", changed, f.handler.CreateXInsightSetup)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.NoError(t, f.handler.AdvanceXInsightSetups(ctx, 32))
	require.NoError(t, f.handler.CheckXInsightSetupControlAuthority(ctx, cp2bInstallationID))
	var grants int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM xinsight_policy_delegations)+(SELECT count(*) FROM xinsight_source_enrollments)`).Scan(&grants))
	require.Zero(t, grants, "unredeemed invitation has no grant authority")
	_, e := f.pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.actorID)
	require.NoError(t, e)
	require.Error(t, f.handler.CheckXInsightSetupControlAuthority(ctx, cp2bInstallationID))
	require.NoError(t, f.handler.AdvanceXInsightSetups(ctx, 32))
	var state string
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT setup_state FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&state))
	require.Equal(t, "needs_action", state)
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM xinsight_policy_delegations)+(SELECT count(*) FROM xinsight_source_enrollments)`).Scan(&grants))
	require.Zero(t, grants)
}
func TestXInsightNormalSetupRejectsPreviewDriftAndRollsBackIntakeIntegration(t *testing.T) {
	f := newNormalSetupFixture(t)
	preview := f.preview(t)
	put := f.put(preview)
	changed := put
	changed.ExpectedPreviewSHA256 = cp2bSHA("stale")
	w := f.human(t, "stale", changed, f.handler.CreateXInsightSetup)
	require.Equal(t, 409, w.Code, w.Body.String())
	_, e := f.pool.Exec(t.Context(), `CREATE FUNCTION reject_normal_setup_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='data_exchange.xinsight.setup.consent' THEN RAISE EXCEPTION 'synthetic audit failure';END IF;RETURN NEW;END $$;CREATE TRIGGER reject_normal_setup_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_normal_setup_audit()`)
	require.NoError(t, e)
	w = f.human(t, "atomic-rejection", put, f.handler.CreateXInsightSetup)
	require.Equal(t, 503, w.Code, w.Body.String())
	for _, table := range []string{"xinsight_connector_installations", "xinsight_connector_control_bindings", "xinsight_connector_operations"} {
		var count int
		require.NoError(t, f.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table)
	}
}
func TestXInsightSetupClosedNestedDTOAndNoImplicitGrantIntegration(t *testing.T) {
	for _, raw := range []string{`{"schema_version":"x","Request_ID":"a"}`, `{"schema_version":"x","policy_access":[{"Target_Kind":"enterprise_default"}]}`, `{"project_id":null}`, `{"project_id":"1","project_id":"2"}`} {
		var put xInsightSetupConsentPut
		require.Error(t, decodeXInsightSetupWire([]byte(raw), &put), raw)
	}
	saved := xInsightSavedSetup{PolicyAccess: []xInsightSetupPolicyAccess{}, Profiles: []string{"p"}}
	status := setupStatusProjection("x", saved, xInsightSetupProgress{PolicyEffective: true, SourceGrantID: 1, SourceBindingsDone: true}, "waiting_data")
	require.Equal(t, "not_requested", status.PurposeStatus.PolicyManage)
	require.True(t, status.ConfigurationReady)
	require.Contains(t, status.Issues, xInsightSetupIssue{"enterprise_joint_management_not_granted", false})
	require.False(t, strings.Contains(status.State, "read"))
}

func TestXInsightNormalSetupRejectsResolvedHandoffMismatchIntegration(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*xInsightSetupResolved)
	}{
		{"wrong token", func(r *xInsightSetupResolved) {
			r.Invitation.InstallURL = strings.Replace(r.Invitation.InstallURL, cp2bInvitation, "xiiv1_"+strings.Repeat("B", 43), 1)
		}},
		{"foreign origin", func(r *xInsightSetupResolved) { r.Invitation.XInsightOrigin = "https://foreign.example" }},
		{"foreign deployment", func(r *xInsightSetupResolved) { r.Invitation.DeploymentID = "xid_BBBBBBBBBBBBBBBBBBBBBB" }},
		{"changed consent hash", func(r *xInsightSetupResolved) { r.Consent.Custody.RetentionSeconds++ }},
		{"mismatched expiry", func(r *xInsightSetupResolved) {
			r.Invitation.ExpiresAt = time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano)
		}},
		{"expired", func(r *xInsightSetupResolved) {
			r.Invitation.ExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newNormalSetupFixture(t)
			tc.mutate(f.resolved)
			w := f.human(t, "bad-resolve", map[string]any{"project_id": stringInt64(f.projectID), "invitation_token": cp2bInvitation}, f.handler.PreviewXInsightSetup)
			require.Equal(t, 409, w.Code)
			require.NotContains(t, w.Body.String(), cp2bInvitation)
			var rows int
			require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_connector_installations`).Scan(&rows))
			require.Zero(t, rows)
		})
	}
	f := newNormalSetupFixture(t)
	w := f.human(t, "old-preview-shape", map[string]any{"project_id": stringInt64(f.projectID), "invitation": f.invitation}, f.handler.PreviewXInsightSetup)
	require.Equal(t, 400, w.Code, "new normal entry accepts only token from original link")
	p := f.preview(t)
	changed := f.put(p)
	changed.Invitation.InvitationID = "xii_BBBBBBBBBBBBBBBBBBBBBB"
	w = f.human(t, "changed-final-package", changed, f.handler.CreateXInsightSetup)
	require.Equal(t, 409, w.Code)
}

func TestXInsightNormalSetupPreservesOriginalSourcePreviewIntegration(t *testing.T) {
	f := newNormalSetupFixture(t)
	registration, _ := f.registration(t, "normal-original-source", sourceEnrollmentEightProfiles())
	key := f.control(t, "prepare", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": registration}, 200)
	f.control(t, "activate", map[string]any{"credential_id": key["credential_id"]}, 200)
	f.control(t, "resume", map[string]any{}, 200)
	preview := f.preview(t)
	require.Equal(t, sourceEnrollmentEightProfiles(), preview.ExistingProfiles)
	require.Equal(t, preview.ExistingProfiles, preview.DefaultProfiles)
	require.NotNil(t, preview.ExistingBindingSHA256)
	changed := f.put(preview)
	changed.Profiles = changed.Profiles[:1]
	w := f.human(t, "changed-original-profile", changed, f.handler.CreateXInsightSetup)
	require.Equal(t, 409, w.Code)
	w = f.human(t, "same-original-profile", f.put(preview), f.handler.CreateXInsightSetup)
	require.Equal(t, 202, w.Code, w.Body.String())
	var raw []byte
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT setup_consent_canonical FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&raw))
	var saved xInsightSavedSetup
	require.NoError(t, decodeXInsightSetupWire(raw, &saved))
	require.Len(t, saved.ExistingSourceStates, 1)
	require.Equal(t, "facts", saved.ExistingSourceStates[0].Kind)
	require.Equal(t, key["credential_id"], saved.ExistingSourceStates[0].Keys[0].CredentialID)
	require.NoError(t, f.handler.AdvanceXInsightSetups(t.Context(), 32))
	var bindings int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_source_enrollment_bindings`).Scan(&bindings))
	require.Zero(t, bindings, "reviewed original sources cannot bind before actual redeem")
}

func TestXInsightNormalSetupOrganizationAdminScopeIntegration(t *testing.T) {
	f := newNormalSetupFixture(t)
	_, err := f.pool.Exec(t.Context(), `UPDATE users SET role='org_admin',organization_role='org_admin' WHERE id=$1`, f.actorID)
	require.NoError(t, err)
	call := func(org, project int64, body any, handler http.HandlerFunc) *httptest.ResponseRecorder {
		raw, e := json.Marshal(body)
		require.NoError(t, e)
		r := apiKeyIntegrationRequest(t, f.pool, "POST", "/api/v2/organizations/"+stringInt64(org)+"/xinsight/setup-previews", string(raw), f.actorID, f.orgID, "org_admin", "normal-org-admin")
		r.SetPathValue("org_id", stringInt64(org))
		r.SetPathValue("installation_id", cp2bInstallationID)
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	w := call(f.orgID, f.projectID, map[string]any{"project_id": stringInt64(f.projectID), "invitation_token": cp2bInvitation}, f.handler.PreviewXInsightSetup)
	require.Equal(t, 200, w.Code, w.Body.String())
	var p xInsightSetupPreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
	require.Contains(t, p.AllowedCoverage, "all_current_future")
	var foreignOrg, foreignProject int64
	require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO orgs(name,status,org_type) VALUES('normal-foreign-org','active','standard') RETURNING id`).Scan(&foreignOrg))
	require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'normal-foreign-project','normal-foreign-project','active','none') RETURNING id`, foreignOrg).Scan(&foreignProject))
	w = call(foreignOrg, foreignProject, map[string]any{"project_id": stringInt64(foreignProject), "invitation_token": cp2bInvitation}, f.handler.PreviewXInsightSetup)
	require.Equal(t, 403, w.Code)
	w = call(f.orgID, f.projectID, f.put(p), f.handler.CreateXInsightSetup)
	require.Equal(t, 202, w.Code, w.Body.String())
	require.NoError(t, f.handler.CheckXInsightSetupControlAuthority(t.Context(), cp2bInstallationID))
}

// This isolates read-only status projection from provisioning. The saved local
// progress is a test fixture, while policy changes use the original human API.
// Real provisioning/receiving remains covered by the independent joint test.
func TestXInsightNormalSetupCurrentRevisionReadOnlyStatusIntegration(t *testing.T) {
	f := newNormalSetupFixture(t)
	put := f.put(f.preview(t))
	require.Equal(t, 202, f.human(t, "consent", put, f.handler.CreateXInsightSetup).Code)
	progress := xInsightSetupProgress{PolicyEffective: true, SourceBindingsDone: true, SourceGrantID: 1, PolicyGrantIDs: []int64{1, 2}}
	raw, err := json.Marshal(progress)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `UPDATE xinsight_connector_installations SET setup_progress=$2::jsonb,setup_state='waiting_data' WHERE installation_id=$1`, cp2bInstallationID, raw)
	require.NoError(t, err)
	var before []byte
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT setup_progress::text FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&before))
	xi := xInsightSetupXIStatus{SchemaVersion: "xinsight.connector-setup-status.v1", InstallationID: cp2bInstallationID, DeploymentID: cp2bDeploymentID, ConsentRevision: 1, State: "waiting_data", Issues: []xInsightSetupIssue{}}
	xi.Custody.DesiredRevision = 3
	xi.Custody.EffectiveRevision = 3
	xi.Custody.Loaded = true
	xi.Custody.Settings = &xInsightSetupCustody{Mode: "disabled", RetentionSeconds: 86400, ProjectQuotaBytes: 1024, MaxItemBytes: 64}
	xi.Receiving.ProfilesReceived = []string{}
	f.handler.xInsightSetup.status = func(context.Context, string, string) ([]byte, error) { return json.Marshal(xi) }
	store := xinsightconnector.NewCommunicationStore(f.pool, 1000, "", "")
	f.handler.SetXInsightCommunication(store, nil)
	probes := 0
	f.handler.xInsightSetup.status = func(context.Context, string, string) ([]byte, error) { probes++; return json.Marshal(xi) }
	read := func() xInsightSetupStatus {
		observation, err := json.Marshal(xi)
		require.NoError(t, err)
		require.NoError(t, store.SavePeerObservation(t.Context(), cp2bInstallationID, observation))
		t.Cleanup(func() { require.Zero(t, probes, "status GET never invokes the peer") })
		w := f.human(t, "status", nil, f.handler.GetXInsightSetupStatus)
		require.Equal(t, 200, w.Code, w.Body.String())
		var value xInsightSetupStatus
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &value))
		return value
	}
	value := read()
	require.True(t, value.ConfigurationReady)
	require.Equal(t, "waiting_data", value.State)
	require.Equal(t, int64(3), value.XI.Custody.EffectiveRevision)
	xi.Custody.Loaded = false
	require.False(t, read().ConfigurationReady)
	xi.Custody.DesiredRevision = 4
	xi.Custody.EffectiveRevision = 4
	xi.Custody.Loaded = true
	xi.Custody.Settings.Mode = "full_content"
	now := time.Now().UTC()
	xi.Receiving.LastSuccessAt = &now
	xi.Receiving.ContentReadVerifiedAt = &now
	value = read()
	require.True(t, value.ConfigurationReady)
	require.Equal(t, "ready", value.State)
	require.Equal(t, int64(4), value.XI.Custody.EffectiveRevision)
	controller := f.handler.contentPolicyController.(scopedContentPolicyController)
	tx, err := f.pool.Begin(t.Context())
	require.NoError(t, err)
	policy, err := controller.ScopedManagementTx(t.Context(), tx, f.orgID, f.projectID, 0, "ready")
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	enterpriseRead := f.human(t, "enterprise-current", nil, f.handler.GetEnterprisePayloadPolicyV2)
	require.Equal(t, 200, enterpriseRead.Code, enterpriseRead.Body.String())
	var enterprise contentcapture.ScopedManagementView
	require.NoError(t, json.Unmarshal(enterpriseRead.Body.Bytes(), &enterprise))
	quota := int64(1024)
	changed := contentcapture.ScopedManagementPut{SchemaVersion: contentcapture.ScopedPolicyManagementSchema, ExpectedPolicyRef: policy.Enterprise.PolicyRef, ExpectedVersion: "0", ExpectedVector: enterprise.VersionVector, ExecutionStorageMode: contentcapture.ExecutionStorageStrict, ProjectQuotaBytes: &quota, Desired: contentcapture.PolicyConfig{Mode: contentcapture.ModeDisabled, SupportedProtocols: []string{"openai.chat"}, ObservationPoints: []string{"client_request"}, AllowedPurposes: []string{}}}
	w := f.human(t, "human-policy", changed, f.handler.PutEnterprisePayloadPolicyV2)
	require.Equal(t, 200, w.Code, w.Body.String())
	value = read()
	require.NotEqual(t, policy.VersionVector, value.URPolicyStatus.VersionVector)
	var after []byte
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT setup_progress::text FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&after))
	require.Equal(t, before, after, "GET must not rewrite admitted progress or initial consent")
	var count int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_logs WHERE action='content.policy.enterprise.update'`).Scan(&count))
	require.Equal(t, 1, count, "status reads must not reapply policy")
}

func TestXInsightNormalSetupInitialEnterprisePolicyCASIntegration(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		t.Run(map[bool]string{false: "enterprise_only_vector", true: "reject_project_vector"}[wrong], func(t *testing.T) {
			f := newNormalSetupFixture(t)
			f.handler.contentPolicyReadiness = func(context.Context) (contentcapture.PolicyApplication, error) {
				return contentcapture.PolicyApplication{StorageStatus: "ready", ApplyNow: true}, nil
			}
			preview := f.preview(t)
			put := f.put(preview)
			quota := int64(1024)
			vector := `{"enterprise":0}`
			if wrong {
				vector = preview.URPolicy.VersionVector
			}
			put.URPolicy = &xInsightSetupPolicyPut{Scope: "enterprise", Put: contentcapture.ScopedManagementPut{SchemaVersion: contentcapture.ScopedPolicyManagementSchema, ExpectedPolicyRef: preview.URPolicy.Enterprise.PolicyRef, ExpectedVersion: "0", ExpectedVector: vector, ExecutionStorageMode: contentcapture.ExecutionStorageTemporary, ProjectQuotaBytes: &quota, Desired: contentPolicyIntegrationFull()}}
			w := f.human(t, "save-enterprise", put, f.handler.CreateXInsightSetup)
			require.Equal(t, 202, w.Code, w.Body.String())
			tx, err := f.pool.Begin(t.Context())
			require.NoError(t, err)
			defer tx.Rollback(t.Context())
			saved, progress, _, err := readXInsightSavedSetupTx(t.Context(), tx, cp2bInstallationID, false)
			require.NoError(t, err)
			err = f.handler.applyXInsightSetupPolicyTx(t.Context(), tx, saved, &progress)
			if wrong {
				require.ErrorIs(t, err, contentcapture.ErrPolicyVersionConflict)
				return
			}
			require.NoError(t, err)
			require.True(t, progress.PolicyEffective)
			view, err := f.handler.contentPolicyController.(scopedContentPolicyController).EnterpriseManagementTx(t.Context(), tx, f.orgID, "ready")
			require.NoError(t, err)
			require.Equal(t, "full_content", view.Enterprise.Effective.Config.Mode)
			require.Equal(t, "1", view.Enterprise.Effective.PolicyVersion)
			require.NoError(t, tx.Commit(t.Context()))
			var audits int
			require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_logs WHERE action='content.policy.enterprise.update' AND user_id=$1`, f.actorID).Scan(&audits))
			require.Equal(t, 1, audits)
		})
	}
}
