//go:build integration

package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

// jointHTTPFixture reuses the real UR handler and isolated migrated database
// without starting the production daemon or replacing machine authentication.
// All bearer values remain in the test process and its controlled child.
type jointHTTPFixture struct {
	sourceControlFixture
	server            *httptest.Server
	content           *sourcefacts.ContentCaptureService
	policyBearer      string
	enrollmentBearer  string
	installation      string
	policyGrant       string
	sourceGrant       string
	captureKeyID      int64
	foreignProjectID  int64
	revokedBearer     string
	contentRegistered chan struct{}
	aBootstrapped     chan struct{}
	contentPublished  chan struct{}
	publishOnce       *sync.Once
	bootstrapOnce     *sync.Once
}

type jointContentObject struct {
	RequestID    string
	SourceFactID string
	ContentRef   string
	Revision     string
	SHA256       string
}

func jointRandomHex(t *testing.T, bytesCount int) string {
	t.Helper()
	raw := make([]byte, bytesCount)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	return hex.EncodeToString(raw)
}

func (f jointHTTPFixture) sourceRegistration(t *testing.T, profiles []string) (sourcefacts.Registration, string) {
	t.Helper()
	bearer := auth.ReceiverFeedTokenPrefix + jointRandomHex(t, 32)
	verifier, err := auth.ReceiverFeedVerifierHex(bearer)
	require.NoError(t, err)
	return sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion,
		TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme,
		KeyPrefix: bearer[:auth.ReceiverFeedCredentialPrefixLength], VerifierSHA256: verifier,
		OperationID: jointRandomHex(t, 16), SourceInstance: f.source.SourceInstance(),
		OrgID: stringInt64(f.orgID), ProjectID: stringInt64(f.projectID), Profiles: profiles}, bearer
}

func (f jointHTTPFixture) machineHTTP(t *testing.T, method, path, bearer string, body any) *http.Response {
	t.Helper()
	var requestBody io.Reader
	if body != nil {
		wire, err := json.Marshal(body)
		require.NoError(t, err)
		requestBody = strings.NewReader(string(wire))
	}
	r, err := http.NewRequestWithContext(t.Context(), method, f.server.URL+path, requestBody)
	require.NoError(t, err)
	r.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := f.server.Client().Do(r)
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func (f jointHTTPFixture) humanCall(t *testing.T, action, method, path string, body any, handler http.HandlerFunc) map[string]any {
	t.Helper()
	r := f.request(t, action, body)
	r.Method = method
	r.URL.Path = path
	r.SetPathValue("org_id", stringInt64(f.orgID))
	w := httptest.NewRecorder()
	handler(w, r)
	require.True(t, w.Code == http.StatusCreated || w.Code == http.StatusOK, "human setup %s: status=%d body=%s", action, w.Code, w.Body.String())
	var result map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	return result
}

func (f jointHTTPFixture) activateMachineBearer(t *testing.T, purpose, installation string) string {
	t.Helper()
	prefix, domain := "ur-xpm1-", auth.XInsightPolicyVerifierDomain
	if purpose == sourceEnrollPurpose {
		prefix, domain = "ur-xse1-", auth.XInsightEnrollmentVerifierDomain
	}
	bearer := prefix + jointRandomHex(t, 32)
	verifier := sha256.Sum256([]byte(domain + "\x00" + bearer))
	registration := machineCredentialRegistration{InstallationID: installation, Purpose: purpose,
		KeyPrefix: bearer[:20], VerifierSHA256: hex.EncodeToString(verifier[:]),
		OperationID: jointRandomHex(t, 16), AllowedIP: "127.0.0.1/32"}
	registered := f.humanCall(t, "joint-register-machine", http.MethodPost,
		"/api/v2/organizations/"+stringInt64(f.orgID)+"/xinsight-policy-credentials", registration,
		f.handler.RegisterXInsightMachineCredential)
	keyID, ok := registered["key_id"].(string)
	require.True(t, ok)
	r := f.request(t, "joint-activate-machine", map[string]any{})
	r.SetPathValue("org_id", stringInt64(f.orgID))
	r.SetPathValue("key_id", keyID)
	w := httptest.NewRecorder()
	f.handler.ActivateXInsightMachineCredential(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return bearer
}

func newJointHTTPFixture(t *testing.T, holdContentRegistration bool) jointHTTPFixture {
	t.Helper()
	f := jointHTTPFixture{sourceControlFixture: newContentControlIntegrationFixture(t),
		contentRegistered: make(chan struct{}), aBootstrapped: make(chan struct{}),
		contentPublished: make(chan struct{}), publishOnce: &sync.Once{}, bootstrapOnce: &sync.Once{}}
	f.installation = "xci_" + jointRandomHex(t, 11)
	_, err := f.pool.Exec(t.Context(), `INSERT INTO xinsight_connector_installations
		(installation_id,org_id,project_id,x_deployment_id,invitation_sha256,event_scope,source_instance,source_binding_sha256,
		desired_state,observed_state,consented_by)
		VALUES($1,$2,$3,'xdep_joint',$4,'model.request.completed',$5,$6,'active','connected',$7)`,
		f.installation, f.orgID, f.projectID, "sha256:"+strings.Repeat("a", 64), f.source.SourceInstance(),
		"sha256:"+strings.Repeat("b", 64), f.actorID)
	require.NoError(t, err)

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	binding, err := contentcapture.InitializeFileStore(root, "synthetic-joint-http")
	require.NoError(t, err)
	files, err := contentcapture.OpenFileStore(root, binding)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, files.Close()) })
	protocols := []string{string(modeloperation.IDChatCompletions), string(modeloperation.IDEmbeddings)}
	points := []string{"client_request", "provider_request", "provider_response", "client_response"}
	const limit int64 = 4096
	options := sourcefacts.ContentCaptureServiceConfig{
		Policy: contentcapture.PolicyStoreOptions{SourceInstance: f.source.SourceInstance(), DefaultMode: contentcapture.ModeMetadataOnly,
			FullContentEnabled: true, SupportedProtocols: protocols, ObservationPoints: points,
			Limits: contentcapture.PolicyLimits{MaxSavedBytes: limit, MaxReadBytes: limit, MaxBufferBytes: limit, MaxManifestBytes: 65536, MaxManifestItems: 100}},
		Engine: contentcapture.EngineOptions{Queue: contentcapture.QueueLimits{MaxItems: 16, MaxBytes: 16 * limit,
			MaxProjectBytes: 16 * limit, MaxChunkBytes: limit}, MaxActive: 2, MaxProjectActive: 2,
			PrepareTimeout: time.Second, FinalizeTimeout: 5 * time.Second, MaxLifetime: time.Minute},
		LeaseOwner: "synthetic-joint-http", LeaseTTL: time.Minute, StartTimeout: time.Second, HealthTimeout: time.Second,
	}
	f.content, err = sourcefacts.NewContentCaptureService(f.source, files, options)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, f.content.Close(ctx))
	})
	f.handler.SetContentPolicyController(f.content.PolicyStore(), f.content.Readiness)
	retention, quota, maxSaved := int64(3600), int64(16*limit), limit
	policy := contentcapture.PolicyConfig{Mode: contentcapture.ModeFullContent, SupportedProtocols: protocols,
		ObservationPoints: points, RetentionSeconds: &retention, QuotaBytes: &quota, MaxSavedBytes: &maxSaved,
		AllowedPurposes: []string{sourcefacts.ContentHumanReview}, OnLimit: "record_capture_gap"}
	wire, err := json.Marshal(policy)
	require.NoError(t, err)
	_, err = f.pool.Exec(t.Context(), `INSERT INTO execution_content_policies
		(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason)
		VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, f.orgID, f.projectID, wire)
	require.NoError(t, err)
	require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status)
		VALUES($1,$2,'joint-capture',repeat('e',64),'ur-joint-capture','active') RETURNING id`,
		f.orgID, f.projectID).Scan(&f.captureKeyID))
	var foreignOrgID int64
	require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO orgs(name,status,org_type)
		VALUES('joint-foreign','active','standard') RETURNING id`).Scan(&foreignOrgID))
	require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO projects(org_id,name,slug,status,budget_period)
		VALUES($1,'Joint foreign','joint-foreign','active','none') RETURNING id`, foreignOrgID).Scan(&f.foreignProjectID))

	f.revokedBearer = f.activateMachineBearer(t, policyManagePurpose, f.installation)
	f.policyBearer = f.activateMachineBearer(t, policyManagePurpose, f.installation)
	f.enrollmentBearer = f.activateMachineBearer(t, sourceEnrollPurpose, f.installation)
	delegation := f.humanCall(t, "joint-policy-grant", http.MethodPost,
		"/api/v2/organizations/"+stringInt64(f.orgID)+"/xinsight-policy-delegations",
		policyDelegationInput{InstallationID: f.installation, TargetKind: "project_policy", Coverage: "exact",
			ProjectID: stringInt64(f.projectID), Read: true, Write: true}, f.handler.CreateXInsightPolicyDelegation)
	f.policyGrant, _ = delegation["delegation_id"].(string)
	require.NotEmpty(t, f.policyGrant)
	enrollment := f.humanCall(t, "joint-source-grant", http.MethodPost,
		"/api/v2/organizations/"+stringInt64(f.orgID)+"/xinsight-source-enrollments",
		sourceEnrollmentInput{InstallationID: f.installation, Coverage: "exact", ProjectID: stringInt64(f.projectID),
			Profiles: []string{sourcefacts.ContextProfile}, IncludeContent: true}, f.handler.CreateXInsightSourceEnrollment)
	f.sourceGrant, _ = enrollment["enrollment_id"].(string)
	require.NotEmpty(t, f.sourceGrant)

	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	policyGate := middleware.XInsightMachineAuth(f.pool, syntheticSourceControlSecret, policyManagePurpose, resolver)
	sourceGate := middleware.XInsightMachineAuth(f.pool, syntheticSourceControlSecret, sourceEnrollPurpose, resolver)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/integrations/xinsight/payload-policy/scopes", policyGate(http.HandlerFunc(f.handler.GetXInsightMachinePolicyScopes)))
	mux.Handle("GET /api/v1/integrations/xinsight/payload-policy/organizations/{org_id}", policyGate(f.handler.GetXInsightMachinePolicy(contentcapture.PolicyScopeEnterprise)))
	mux.Handle("PUT /api/v1/integrations/xinsight/payload-policy/organizations/{org_id}", policyGate(f.handler.PutXInsightMachinePolicy(contentcapture.PolicyScopeEnterprise)))
	mux.Handle("GET /api/v1/integrations/xinsight/payload-policy/projects/{project_id}", policyGate(f.handler.GetXInsightMachinePolicy(contentcapture.PolicyScopeProject)))
	mux.Handle("PUT /api/v1/integrations/xinsight/payload-policy/projects/{project_id}", policyGate(f.handler.PutXInsightMachinePolicy(contentcapture.PolicyScopeProject)))
	mux.Handle("GET /api/v1/integrations/xinsight/payload-policy/projects/{project_id}/users", policyGate(http.HandlerFunc(f.handler.GetXInsightMachinePolicyUsers)))
	mux.Handle("GET /api/v1/integrations/xinsight/payload-policy/projects/{project_id}/users/{user_id}", policyGate(f.handler.GetXInsightMachinePolicy(contentcapture.PolicyScopeUser)))
	mux.Handle("PUT /api/v1/integrations/xinsight/payload-policy/projects/{project_id}/users/{user_id}", policyGate(f.handler.PutXInsightMachinePolicy(contentcapture.PolicyScopeUser)))
	mux.Handle("GET /api/v1/integrations/xinsight/payload-policy/operations/{operation_ref}", policyGate(http.HandlerFunc(f.handler.GetXInsightMachinePolicyOperation)))
	mux.Handle("GET /api/v1/integrations/xinsight/source-enrollments/projects", sourceGate(http.HandlerFunc(f.handler.ListXInsightMachineEnrollmentProjects)))
	mux.Handle("GET /api/v1/integrations/xinsight/source-enrollments/projects/{project_id}", sourceGate(http.HandlerFunc(f.handler.GetXInsightMachineEnrollmentProject)))
	register := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !holdContentRegistration {
			f.handler.RegisterXInsightSourceEnrollmentProject(w, r)
			return
		}
		// A successful C registration creates the coverage boundary. The test
		// publishes after XI's first A bootstrap page has been produced below.
		recorded := httptest.NewRecorder()
		f.handler.RegisterXInsightSourceEnrollmentProject(recorded, r)
		if recorded.Code == http.StatusCreated {
			var active bool
			grantID, ok := sourceEnrollmentPathID(f.sourceGrant)
			if !ok {
				t.Error("joint enrollment ID invalid")
				http.Error(w, "test fixture unavailable", http.StatusServiceUnavailable)
				return
			}
			err := f.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM xinsight_source_enrollment_bindings
				WHERE enrollment_id=$1 AND org_id=$2 AND project_id=$3 AND kind='content' AND state='active')`,
				grantID, f.orgID, f.projectID).Scan(&active)
			if err != nil {
				t.Errorf("joint content registration signal: %v", err)
				http.Error(w, "test fixture unavailable", http.StatusServiceUnavailable)
				return
			}
			if active {
				close(f.contentRegistered)
			}
		}
		for name, values := range recorded.Header() {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.WriteHeader(recorded.Code)
		_, _ = w.Write(recorded.Body.Bytes())
	})
	mux.Handle("POST /api/v1/integrations/xinsight/source-enrollments/projects/{project_id}/registrations", sourceGate(register))
	factsHandler := f.source.Handler(resolver)
	mux.Handle("GET /api/v1/integrations/xinsight/source-facts", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !holdContentRegistration || r.URL.Query().Get("cursor") != "" ||
			r.URL.Query().Get("profile") != sourcefacts.ContextProfile {
			factsHandler.ServeHTTP(w, r)
			return
		}
		// Capture the forward-only bootstrap response before publishing. XI
		// commits its cursor after release and sees the new fact on its next
		// real HTTP pull; C registration has already established its coverage.
		recorded := httptest.NewRecorder()
		factsHandler.ServeHTTP(recorded, r)
		if recorded.Code == http.StatusOK {
			f.bootstrapOnce.Do(func() { close(f.aBootstrapped) })
			select {
			case <-f.contentPublished:
			case <-r.Context().Done():
				return
			}
		}
		for name, values := range recorded.Header() {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.WriteHeader(recorded.Code)
		_, _ = w.Write(recorded.Body.Bytes())
	}))
	mux.Handle("GET /api/v1/integrations/xinsight/source-facts/capabilities", f.source.Handler(resolver))
	authority, err := f.source.NewContentBodyAuthority(resolver)
	require.NoError(t, err)
	reads, err := sourcefacts.NewContentReadHandlers(sourcefacts.ContentReadHandlerConfig{Authority: authority, Store: files,
		SupportedProtocols: protocols, ObservationPoints: points, CaptureModes: []string{contentcapture.ModeDisabled,
			contentcapture.ModeMetadataOnly, contentcapture.ModeFullContent},
		Limits: sourcefacts.ContentLimits{MaxSavedBytes: limit, MaxReadBytes: limit, MaxBufferBytes: limit,
			MaxManifestBytes: 65536, MaxManifestItems: 100}, MaxPageBytes: sourcefacts.MaxPageBytes,
		MaxVerifyBytesPerPage: 16 * limit, MaxConcurrentRequests: 2,
		RequestTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
	require.NoError(t, err)
	body, err := sourcefacts.NewContentBodyHandler(sourcefacts.ContentBodyHandlerConfig{Authority: authority, Store: files,
		MaxReadBytes: limit, MaxBufferBytes: limit, MaxConcurrentReads: 2,
		RequestTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
	require.NoError(t, err)
	mux.Handle("GET "+sourcefacts.ContentCapabilitiesPath, reads.Capabilities)
	mux.Handle("GET "+sourcefacts.ContentManifestsPath, reads.Manifests)
	mux.Handle("GET "+sourcefacts.ContentEndpointTemplate, body)
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// emitSourceContent is called after XI has registered A/C and produced its
// forward-only A bootstrap page. It creates a real Source A fact and a real
// encrypted C object in the same synthetic project.
func (f jointHTTPFixture) emitSourceContent(t *testing.T, requestID string, payload []byte) jointContentObject {
	t.Helper()
	f.capture(t, requestID)
	observation := contentcapture.Observation{OrgID: f.orgID, ProjectID: f.projectID,
		RequestID: requestID, Protocol: string(modeloperation.IDChatCompletions),
		ObservationPoint: "client_response", MediaType: "application/json", OccurredAt: time.Now().UTC(),
		Principal: contentcapture.CapturePrincipal{APIKeyID: f.captureKeyID, KeyType: "shared"}}
	capture, err := f.content.Begin(t.Context(), observation)
	require.NoError(t, err)
	require.NotNil(t, capture)
	require.NoError(t, capture.Offer(t.Context(), payload))
	require.NoError(t, capture.Finish("none"))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := capture.Wait(ctx)
	require.NoError(t, err)
	require.NoError(t, result.FinalizeError)
	var wire []byte
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT document FROM execution_content_manifests
		WHERE org_id=$1 AND project_id=$2 AND request_id=$3 AND revision=2`,
		f.orgID, f.projectID, requestID).Scan(&wire))
	manifest, err := sourcefacts.DecodeContentManifest(wire)
	require.NoError(t, err)
	require.Equal(t, "complete", manifest.Capture.Completeness)
	require.NotEmpty(t, manifest.ContentRef)
	digest := sha256.Sum256(payload)
	return jointContentObject{RequestID: requestID, SourceFactID: manifest.SourceFactID,
		ContentRef: manifest.ContentRef, Revision: manifest.Revision, SHA256: hex.EncodeToString(digest[:])}
}

func TestXInsightJointServerHalfIntegration(t *testing.T) {
	f := newJointHTTPFixture(t, false)
	for _, tc := range []struct {
		path   string
		bearer string
	}{
		{"/api/v1/integrations/xinsight/payload-policy/scopes", f.policyBearer},
		{"/api/v1/integrations/xinsight/source-enrollments/projects", f.enrollmentBearer},
	} {
		response := f.machineHTTP(t, http.MethodGet, tc.path, tc.bearer, nil)
		require.Equal(t, http.StatusOK, response.StatusCode)
	}
	crossScope := f.machineHTTP(t, http.MethodGet,
		"/api/v1/integrations/xinsight/payload-policy/projects/"+stringInt64(f.foreignProjectID)+
			"?delegation_id="+url.QueryEscape(f.policyGrant), f.policyBearer, nil)
	require.True(t, crossScope.StatusCode == http.StatusForbidden || crossScope.StatusCode == http.StatusNotFound)
	revoked := f.machineHTTP(t, http.MethodGet,
		"/api/v1/integrations/xinsight/payload-policy/scopes", f.revokedBearer, nil)
	require.Equal(t, http.StatusUnauthorized, revoked.StatusCode)

	registerPath := "/api/v1/integrations/xinsight/source-enrollments/projects/" + stringInt64(f.projectID) + "/registrations"
	assertReplay := func(registration machineEnrollmentRegistration) {
		t.Helper()
		first := f.machineHTTP(t, http.MethodPost, registerPath, f.enrollmentBearer, registration)
		require.Equal(t, http.StatusCreated, first.StatusCode)
		var initial struct {
			CredentialID     string `json:"credential_id"`
			RegistrationHash string `json:"registration_hash"`
			Replayed         bool   `json:"replayed"`
		}
		require.NoError(t, json.NewDecoder(first.Body).Decode(&initial))
		require.NotEmpty(t, initial.CredentialID)
		require.Regexp(t, `^sha256:[0-9a-f]{64}$`, initial.RegistrationHash)
		require.False(t, initial.Replayed)
		replay := f.machineHTTP(t, http.MethodPost, registerPath, f.enrollmentBearer, registration)
		require.Equal(t, http.StatusOK, replay.StatusCode)
		var repeated struct {
			CredentialID     string `json:"credential_id"`
			RegistrationHash string `json:"registration_hash"`
			Replayed         bool   `json:"replayed"`
		}
		require.NoError(t, json.NewDecoder(replay.Body).Decode(&repeated))
		require.True(t, repeated.Replayed)
		require.Equal(t, initial.CredentialID, repeated.CredentialID)
		require.Equal(t, initial.RegistrationHash, repeated.RegistrationHash)
	}
	factsRegistration, factsBearer := f.sourceRegistration(t, []string{sourcefacts.ContextProfile})
	assertReplay(machineEnrollmentRegistration{EnrollmentID: f.sourceGrant, Kind: "facts", Registration: factsRegistration})
	contentRegistration, contentBearer := f.sourceRegistration(t, []string{sourcefacts.ContentProfile})
	assertReplay(machineEnrollmentRegistration{EnrollmentID: f.sourceGrant, Kind: "content", Registration: contentRegistration})

	bootstrapFacts := f.machineHTTP(t, http.MethodGet,
		"/api/v1/integrations/xinsight/source-facts?profile="+url.QueryEscape(sourcefacts.ContextProfile), factsBearer, nil)
	require.Equal(t, http.StatusOK, bootstrapFacts.StatusCode)
	var factsBoot struct {
		NextCursor string `json:"next_cursor"`
	}
	require.NoError(t, json.NewDecoder(bootstrapFacts.Body).Decode(&factsBoot))
	require.NotEmpty(t, factsBoot.NextCursor)
	bootstrapContent := f.machineHTTP(t, http.MethodGet,
		sourcefacts.ContentManifestsPath+"?purpose=human_review&limit=10", contentBearer, nil)
	require.Equal(t, http.StatusOK, bootstrapContent.StatusCode)
	var contentBoot struct {
		NextCursor string `json:"next_cursor"`
	}
	require.NoError(t, json.NewDecoder(bootstrapContent.Body).Decode(&contentBoot))
	require.NotEmpty(t, contentBoot.NextCursor)

	requestID := "joint-" + jointRandomHex(t, 12)
	payload := []byte(`{"role":"assistant","content":"joint synthetic answer"}`)
	object := f.emitSourceContent(t, requestID, payload)
	facts := f.machineHTTP(t, http.MethodGet,
		"/api/v1/integrations/xinsight/source-facts?profile="+url.QueryEscape(sourcefacts.ContextProfile)+
			"&cursor="+url.QueryEscape(factsBoot.NextCursor), factsBearer, nil)
	require.Equal(t, http.StatusOK, facts.StatusCode)
	var factsPage struct {
		Events []json.RawMessage `json:"events"`
	}
	require.NoError(t, json.NewDecoder(facts.Body).Decode(&factsPage))
	require.NotEmpty(t, factsPage.Events, "Source A must deliver a real post-registration fact")
	contentPage := f.machineHTTP(t, http.MethodGet,
		sourcefacts.ContentManifestsPath+"?purpose=human_review&limit=10&cursor="+url.QueryEscape(contentBoot.NextCursor),
		contentBearer, nil)
	require.Equal(t, http.StatusOK, contentPage.StatusCode)
	var manifests sourcefacts.ContentManifestPage
	require.NoError(t, json.NewDecoder(contentPage.Body).Decode(&manifests))
	require.NotEmpty(t, manifests.Items, "Source C must deliver a real post-registration manifest")
	bodyPath := sourcefacts.ContentBodyPathPrefix + object.ContentRef + "?purpose=human_review&request_id=" +
		url.QueryEscape(object.RequestID) + "&revision=" + url.QueryEscape(object.Revision) +
		"&source_fact_id=" + url.QueryEscape(object.SourceFactID)
	body := f.machineHTTP(t, http.MethodGet, bodyPath, contentBearer, nil)
	require.Equal(t, http.StatusOK, body.StatusCode)
	actual, err := io.ReadAll(body.Body)
	require.NoError(t, err)
	require.Equal(t, payload, actual, "Source C must return the actual encrypted-store bytes")
}

// TestXInsightJointConsumerIntegration runs the independently compiled XI
// consumer against this fixture's production UR handlers and isolated PG.
// The source bearers are deliberately not pre-registered: XI must complete
// the first-time A/C enrollment through UR's public machine API.
func TestXInsightJointConsumerIntegration(t *testing.T) {
	checkout := os.Getenv("XI_JOINT_TEST_CHECKOUT")
	if checkout == "" {
		t.Skip("set XI_JOINT_TEST_CHECKOUT to the XI worktree for the local joint test")
	}
	xiDSN := os.Getenv("XI_JOINT_POSTGRES_DSN")
	require.NotEmpty(t, xiDSN, "joint test requires isolated XI_JOINT_POSTGRES_DSN")
	require.FileExists(t, filepath.Join(checkout, "go.mod"))

	binary := filepath.Join(t.TempDir(), "xi-joint.test")
	build := exec.CommandContext(t.Context(), "go", "test", "-tags", "integration", "-c", "-o", binary, "./internal/db")
	build.Dir = checkout
	buildOutput, err := build.CombinedOutput()
	require.NoError(t, err, "compile XI joint consumer: %s", string(buildOutput))

	f := newJointHTTPFixture(t, true)
	defer f.publishOnce.Do(func() { close(f.contentPublished) })
	_, factsBearer := f.sourceRegistration(t, []string{sourcefacts.ContextProfile})
	_, contentBearer := f.sourceRegistration(t, []string{sourcefacts.ContentProfile})
	requestID := "joint-" + jointRandomHex(t, 12)
	payload := []byte(`{"role":"assistant","content":"joint synthetic answer"}`)
	digest := sha256.Sum256(payload)
	input := struct {
		BaseURL             string `json:"base_url"`
		TLSCAPEM            string `json:"tls_ca_pem"`
		InstallationID      string `json:"installation_id"`
		SourceInstance      string `json:"source_instance"`
		OrgID               string `json:"org_id"`
		ProjectID           string `json:"project_id"`
		PolicyBearer        string `json:"policy_bearer"`
		EnrollmentBearer    string `json:"enrollment_bearer"`
		SourceFactsBearer   string `json:"source_facts_bearer"`
		SourceContentBearer string `json:"source_content_bearer"`
		ForeignProjectID    string `json:"foreign_project_id"`
		RevokedPolicyBearer string `json:"revoked_policy_bearer"`
		PostgresDSN         string `json:"postgres_dsn"`
		ExpectedContent     struct {
			RequestID string `json:"request_id"`
			SHA256    string `json:"sha256"`
		} `json:"expected_content"`
	}{
		BaseURL:        f.server.URL,
		TLSCAPEM:       string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.server.Certificate().Raw})),
		InstallationID: f.installation,
		SourceInstance: f.source.SourceInstance(),
		OrgID:          stringInt64(f.orgID), ProjectID: stringInt64(f.projectID),
		PolicyBearer: f.policyBearer, EnrollmentBearer: f.enrollmentBearer,
		SourceFactsBearer: factsBearer, SourceContentBearer: contentBearer,
		ForeignProjectID: stringInt64(f.foreignProjectID), RevokedPolicyBearer: f.revokedBearer,
		PostgresDSN: xiDSN,
	}
	input.ExpectedContent.RequestID = requestID
	input.ExpectedContent.SHA256 = hex.EncodeToString(digest[:])
	wire, err := json.Marshal(input)
	require.NoError(t, err)
	childCtx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	child := exec.CommandContext(childCtx, binary, "-test.run=^TestXIJointURConsumer$", "-test.v")
	// go test normally launches a package binary from its package directory.
	// XI's migration fixture resolves ../../migrations from internal/db.
	child.Dir = filepath.Join(checkout, "internal", "db")
	child.Env = append(os.Environ(), "XINSIGHT_JOINT_UR_CONSUMER=1", "XINSIGHT_POSTGRES_DSN="+xiDSN)
	child.Stdin = bytes.NewReader(wire)
	var childOutput bytes.Buffer
	child.Stdout, child.Stderr = &childOutput, &childOutput
	require.NoError(t, child.Start())
	finished := make(chan error, 1)
	go func() { finished <- child.Wait() }()
	completed := false
	defer func() {
		cancel()
		if !completed {
			<-finished
		}
	}()
	redact := func(raw string) string {
		for _, secret := range []string{f.policyBearer, f.enrollmentBearer, factsBearer, contentBearer,
			f.revokedBearer, xiDSN} {
			raw = strings.ReplaceAll(raw, secret, "[redacted]")
		}
		return raw
	}
	select {
	case <-f.contentRegistered:
	case err = <-finished:
		completed = true
		t.Fatalf("XI consumer exited before Source C registration: %v; output: %s", err, redact(childOutput.String()))
	case <-childCtx.Done():
		t.Fatalf("XI consumer did not complete Source C registration: %v", childCtx.Err())
	}
	select {
	case <-f.aBootstrapped:
		object := f.emitSourceContent(t, requestID, payload)
		require.Equal(t, input.ExpectedContent.SHA256, object.SHA256)
		f.publishOnce.Do(func() { close(f.contentPublished) })
	case err = <-finished:
		completed = true
		t.Fatalf("XI consumer exited before Source A bootstrap: %v; output: %s", err, redact(childOutput.String()))
	case <-childCtx.Done():
		t.Fatalf("XI consumer did not complete Source A bootstrap: %v", childCtx.Err())
	}
	err = <-finished
	completed = true
	require.NoError(t, err, "XI consumer failed: %s", redact(childOutput.String()))
}
