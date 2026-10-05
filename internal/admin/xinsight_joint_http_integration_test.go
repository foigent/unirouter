//go:build integration

package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
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
	registrationOnce  *sync.Once
}

// This mode is confined to the disposable Linux qualification VM. It changes
// only the joint test transport and child driver, never UR production routes.
type installedXIJointInput struct {
	SchemaVersion        string `json:"schema_version"`
	Mode                 string `json:"mode"`
	FutureProject        bool   `json:"future_project"`
	XIOrigin             string `json:"xi_origin"`
	XIDeploymentID       string `json:"xi_deployment_id"`
	URListen             string `json:"ur_listen"`
	ReceiverAllowedIP    string `json:"receiver_allowed_ip"`
	TLSCertFile          string `json:"tls_cert_file"`
	TLSKeyFile           string `json:"tls_key_file"`
	TLSCAFile            string `json:"tls_ca_file"`
	XICAFile             string `json:"xi_ca_file"`
	XIAdminUsername      string `json:"xi_admin_username"`
	XIAdminPasswordFile  string `json:"xi_admin_password_file"`
	XIReaderUsername     string `json:"xi_reader_username"`
	XIReaderPasswordFile string `json:"xi_reader_password_file"`
	XIReaderSubjectID    string `json:"xi_reader_subject_id"`
	XIAdminDSNFile       string `json:"xi_admin_dsn_file"`
	TimeoutSeconds       int    `json:"timeout_seconds"`
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

// jointProfileBootstrapBoundary only coordinates this fixture's existing
// publisher. It observes successful original HTTP responses without changing
// their cursor, event body, authentication or production forward-only rules.
func jointProfileBootstrapBoundary(profiles []string, ready func()) func(http.Handler) http.Handler {
	allowed := map[string]struct{}{}
	observed := map[string]bool{}
	for _, profile := range profiles {
		allowed[profile] = struct{}{}
		observed[profile] = false
	}
	var mu sync.Mutex
	var once sync.Once
	return func(actual http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			profile := r.URL.Query().Get("profile")
			if _, ok := allowed[profile]; !ok || r.URL.Query().Get("cursor") != "" {
				actual.ServeHTTP(w, r)
				return
			}
			response := httptest.NewRecorder()
			actual.ServeHTTP(response, r)
			for name, values := range response.Header() {
				w.Header()[name] = append([]string(nil), values...)
			}
			w.WriteHeader(response.Code)
			_, err := w.Write(response.Body.Bytes())
			var page struct {
				NextCursor string `json:"next_cursor"`
			}
			if err != nil || response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.NextCursor == "" {
				return
			}
			mu.Lock()
			observed[profile] = true
			complete := true
			for _, done := range observed {
				complete = complete && done
			}
			mu.Unlock()
			if complete {
				once.Do(ready)
			}
		})
	}
}

func TestJointProfileBootstrapBoundaryIntegration(t *testing.T) {
	profiles := sourceEnrollmentEightProfiles()
	ready := make(chan struct{}, 1)
	wrap := jointProfileBootstrapBoundary(profiles, func() { ready <- struct{}{} })
	responseBody := `{"next_cursor":"original-watermark","events":[]}`
	call := func(profile, cursor, body string, status int) {
		t.Helper()
		actual := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Original", "unchanged")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		})
		r := httptest.NewRequest(http.MethodGet, "/?"+url.Values{"profile": {profile}, "cursor": {cursor}}.Encode(), nil)
		w := httptest.NewRecorder()
		wrap(actual).ServeHTTP(w, r)
		require.Equal(t, status, w.Code)
		require.Equal(t, body, w.Body.String())
		require.Equal(t, "unchanged", w.Header().Get("X-Original"))
	}
	// Failed, malformed, noninitial and unknown requests cannot open the
	// publication boundary, even when all other profiles have bootstrapped.
	for _, profile := range profiles[:len(profiles)-1] {
		call(profile, "", responseBody, http.StatusOK)
	}
	last := profiles[len(profiles)-1]
	call(last, "", responseBody, http.StatusServiceUnavailable)
	call(last, "", `{}`, http.StatusOK)
	call(last, "", `invalid-json`, http.StatusOK)
	call(last, "original-watermark", responseBody, http.StatusOK)
	call("unknown-profile", "", responseBody, http.StatusOK)
	require.Empty(t, ready)
	call(last, "", responseBody, http.StatusOK)
	require.Len(t, ready, 1)
	<-ready
	var requests sync.WaitGroup
	for _, profile := range profiles {
		requests.Add(1)
		go func(profile string) {
			defer requests.Done()
			call(profile, "", responseBody, http.StatusOK)
		}(profile)
	}
	requests.Wait()
	require.Empty(t, ready, "publication must remain one-shot under concurrent retries")
}

func newJointHTTPFixture(t *testing.T, holdContentRegistration bool) jointHTTPFixture {
	return newJointHTTPFixtureWithManaged(t, holdContentRegistration, nil)
}

func newJointHTTPFixtureWithManaged(t *testing.T, holdContentRegistration bool, managed *jointManagedControl) jointHTTPFixture {
	t.Helper()
	f := jointHTTPFixture{sourceControlFixture: newContentControlIntegrationFixture(t),
		contentRegistered: make(chan struct{}), aBootstrapped: make(chan struct{}),
		contentPublished: make(chan struct{}), publishOnce: &sync.Once{}, bootstrapOnce: &sync.Once{}, registrationOnce: &sync.Once{}}
	var err error
	if managed != nil {
		f.handler = NewHandler(f.pool, syntheticSourceControlSecret, []byte("0123456789abcdef0123456789abcdef"), nil)
		f.source, err = sourcefacts.New(f.pool, sourcefacts.Options{Enabled: true, ContentEnabled: true, ObservationsEnabled: true, SourceInstance: "synthetic-source-control", CursorSecret: syntheticSourceControlSecret})
		require.NoError(t, err)
		require.NoError(t, f.source.CheckReady(t.Context()))
		f.handler.SetSourceFactsController(f.source)
	}
	if managed == nil {
		f.installation = "xci_" + jointRandomHex(t, 11)
		_, err = f.pool.Exec(t.Context(), `INSERT INTO xinsight_connector_installations
		(installation_id,org_id,project_id,x_deployment_id,invitation_sha256,event_scope,source_instance,source_binding_sha256,
		desired_state,observed_state,consented_by)
		VALUES($1,$2,$3,'xdep_joint',$4,'model.request.completed',$5,$6,'active','connected',$7)`,
			f.installation, f.orgID, f.projectID, "sha256:"+strings.Repeat("a", 64), f.source.SourceInstance(),
			"sha256:"+strings.Repeat("b", 64), f.actorID)
		require.NoError(t, err)
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	binding, err := contentcapture.InitializeFileStore(root, "synthetic-joint-http")
	require.NoError(t, err)
	files, err := contentcapture.OpenFileStore(root, binding)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, files.Close()) })
	protocols := []string{string(modeloperation.IDChatCompletions), string(modeloperation.IDEmbeddings)}
	points := []string{"client_request", "provider_request", "provider_response", "client_response"}
	limit := int64(4096)
	if managed != nil && managed.normalSetup {
		limit = 32 << 20
	}
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
	if managed == nil || !managed.normalSetup {
		_, err = f.pool.Exec(t.Context(), `INSERT INTO execution_content_policies
		(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason)
		VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, f.orgID, f.projectID, wire)
		require.NoError(t, err)
	}
	require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status)
		VALUES($1,$2,'joint-capture',repeat('e',64),'ur-joint-capture','active') RETURNING id`,
		f.orgID, f.projectID).Scan(&f.captureKeyID))
	var foreignOrgID int64
	require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO orgs(name,status,org_type)
		VALUES('joint-foreign','active','standard') RETURNING id`).Scan(&foreignOrgID))
	require.NoError(t, f.pool.QueryRow(t.Context(), `INSERT INTO projects(org_id,name,slug,status,budget_period)
		VALUES($1,'Joint foreign','joint-foreign','active','none') RETURNING id`, foreignOrgID).Scan(&f.foreignProjectID))

	if managed == nil {
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
	}

	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	policyGate := middleware.XInsightMachineAuth(f.pool, syntheticSourceControlSecret, policyManagePurpose, resolver)
	sourceGate := middleware.XInsightMachineAuth(f.pool, syntheticSourceControlSecret, sourceEnrollPurpose, resolver)
	mux := http.NewServeMux()
	if managed != nil {
		managed.registerRoutes(t, &f, mux, resolver)
	}
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
			err := f.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM xinsight_source_enrollment_bindings
                WHERE org_id=$1 AND project_id=$2 AND kind='content' AND state='active')`,
				f.orgID, f.projectID).Scan(&active)
			if err != nil {
				t.Errorf("joint content registration signal: %v", err)
				http.Error(w, "test fixture unavailable", http.StatusServiceUnavailable)
				return
			}
			if active {
				f.registrationOnce.Do(func() { close(f.contentRegistered) })
			}
		}
		for name, values := range recorded.Header() {
			w.Header()[name] = append([]string(nil), values...)
		}
		w.WriteHeader(recorded.Code)
		_, _ = w.Write(recorded.Body.Bytes())
	})
	mux.Handle("POST /api/v1/integrations/xinsight/source-enrollments/projects/{project_id}/registrations", sourceGate(register))
	var normalBootstrap func(http.Handler) http.Handler
	if managed != nil && managed.normalSetup {
		normalBootstrap = jointProfileBootstrapBoundary(sourceEnrollmentEightProfiles(), func() {
			f.bootstrapOnce.Do(func() { close(f.aBootstrapped) })
		})
	}
	if managed != nil {
		observations := f.source.ObservationHandler(resolver)
		if normalBootstrap != nil {
			observations = normalBootstrap(observations)
		}
		mux.Handle("GET /api/v1/integrations/xinsight/source/v2/events", observations)
		mux.Handle("GET /api/v1/integrations/xinsight/source/v2/capabilities", f.source.ObservationHandler(resolver))
		mux.Handle("GET /api/v1/integrations/xinsight/source/v2/runtime/current", f.source.RuntimeCurrentHandler(resolver))
	}
	factsHandler := f.source.Handler(resolver)
	mux.Handle("GET /api/v1/integrations/xinsight/source-facts", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if normalBootstrap != nil {
			// Every independent forward-only cursor must capture its original
			// watermark before publication. Return each response immediately;
			// the sequential worker must be able to reach the remaining jobs.
			normalBootstrap(factsHandler).ServeHTTP(w, r)
			return
		}
		if !holdContentRegistration || (managed == nil && r.URL.Query().Get("cursor") != "") ||
			r.URL.Query().Get("profile") != sourcefacts.ContextProfile {
			factsHandler.ServeHTTP(w, r)
			return
		}
		if managed != nil {
			// Adoption may warm the original worker before pairing/grants.
			// Hold only the page after C is genuinely enrolled or bound.
			select {
			case <-f.contentRegistered:
			default:
				factsHandler.ServeHTTP(w, r)
				return
			}
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
	if managed != nil && managed.installed != nil {
		installed := managed.installed
		certificate, err := tls.LoadX509KeyPair(installed.TLSCertFile, installed.TLSKeyFile)
		require.NoError(t, err)
		f.server = httptest.NewUnstartedServer(mux)
		require.NoError(t, f.server.Listener.Close())
		f.server.Listener, err = net.Listen("tcp4", installed.URListen)
		require.NoError(t, err, "bind exact qualification receiver address")
		f.server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
		f.server.StartTLS()
	} else {
		f.server = httptest.NewTLSServer(mux)
	}
	if managed != nil {
		managed.pinOrigin(f.server.URL)
		if managed.normalSetup {
			deployment, allowedIP := "xid_AAAAAAAAAAAAAAAAAAAAAA", "127.0.0.1/32"
			if managed.installed != nil {
				deployment, allowedIP = managed.installed.XIDeploymentID, managed.installed.ReceiverAllowedIP
			}
			peer := &config.XInsightConnectorStaticPeer{XInsightOrigin: managed.origin, XDeploymentID: deployment, UROrigin: f.server.URL, AllowedIP: allowedIP, NetworkBindingSHA256: managed.network.BindingSHA256}
			managed.peer = peer
			client := f.server.Client()
			if managed.installed != nil {
				ca, readErr := os.ReadFile(managed.installed.XICAFile)
				require.NoError(t, readErr)
				roots := x509.NewCertPool()
				require.True(t, roots.AppendCertsFromPEM(ca), "XI HTTPS CA must contain a certificate")
				transport := client.Transport.(*http.Transport).Clone()
				transport.TLSClientConfig = transport.TLSClientConfig.Clone()
				transport.TLSClientConfig.RootCAs = roots
				client = &http.Client{Transport: transport, Timeout: 20 * time.Second}
			}
			managed.counterpartClient = client
			require.NoError(t, f.handler.SetXInsightSetupPeer(peer, client, func(ctx context.Context, id, deployment string) ([]byte, error) {
				counterpart, err := xinsightconnector.NewHTTPControlCounterpart(managed.origin, xinsightconnector.RuntimeInstallationDescriptor{InstallationID: id, XDeploymentID: deployment}, managed.journal, client, 10*time.Second)
				if err != nil {
					return nil, err
				}
				return counterpart.ReadSetupStatus(ctx)
			}))
		}
	}
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
	if mode := os.Getenv("XI_JOINT_MANAGED_MODE"); mode != "" {
		runManagedJointConsumer(t, mode)
		return
	}
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

// TestXInsightInstalledXIIntegration is a stdin-only CI entry point. The
// fixture owns UR's migrated test database; XI is an independently installed
// process on the same disposable Linux host.
func TestXInsightInstalledXIIntegration(t *testing.T) {
	if os.Getenv("XINSIGHT_CI_INSTALLED_JOINT") != "1" {
		t.Skip("installed XI qualification requires explicit opt-in")
	}
	if runtime.GOOS != "linux" || os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("XINSIGHT_CI_EPHEMERAL_HOST") != "1" {
		t.Fatal("installed XI joint qualification requires an explicit disposable Linux CI host")
	}
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 16<<10))
	decoder.DisallowUnknownFields()
	var input installedXIJointInput
	require.NoError(t, decoder.Decode(&input), "decode installed XI qualification input")
	var trailing any
	require.ErrorIs(t, decoder.Decode(&trailing), io.EOF, "installed XI qualification accepts exactly one JSON document")
	require.NoError(t, input.validate())
	runManagedJointConsumerWithInstalled(t, input.Mode, &input)
}

func (input installedXIJointInput) validate() error {
	if input.SchemaVersion != "xinsight.ci-installed-joint.v1" ||
		input.Mode != "new_install" && input.Mode != "adopt_existing" ||
		input.FutureProject && input.Mode != "new_install" ||
		input.TimeoutSeconds < 120 || input.TimeoutSeconds > 1200 {
		return errors.New("invalid installed joint schema, mode or timeout")
	}
	origin, err := url.Parse(input.XIOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return errors.New("invalid XI HTTPS origin")
	}
	if !regexp.MustCompile(`^xid_[A-Za-z0-9_-]{22}$`).MatchString(input.XIDeploymentID) {
		return errors.New("invalid installed XI deployment identity")
	}
	bindIP, port, err := net.SplitHostPort(input.URListen)
	if err != nil || port == "" {
		return errors.New("invalid UR listen address")
	}
	ip := net.ParseIP(bindIP)
	if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
		return errors.New("UR test receiver must bind an explicit nonbaseline IPv4 address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1024 || portNumber > 65535 {
		return errors.New("invalid UR test receiver port")
	}
	allowedIP, allowedCIDR, err := net.ParseCIDR(input.ReceiverAllowedIP)
	if err != nil || allowedIP.To4() == nil || allowedCIDR.String() != allowedIP.String()+"/32" ||
		allowedIP.IsLoopback() || allowedIP.IsPrivate() || allowedIP.IsLinkLocalUnicast() {
		return errors.New("receiver allowed IP must be an exact nonbaseline IPv4 /32")
	}
	// A local route probe gives an independently observed source address for
	// connections to this receiver; never infer it from the listener address.
	route, err := net.DialTimeout("udp4", input.URListen, 2*time.Second)
	if err != nil {
		return errors.New("receiver route source unavailable")
	}
	routeSource := route.LocalAddr().(*net.UDPAddr).IP
	_ = route.Close()
	if !routeSource.Equal(allowedIP) {
		return errors.New("receiver allowed IP differs from observed route source")
	}
	for _, path := range []string{input.TLSCertFile, input.TLSKeyFile, input.TLSCAFile, input.XICAFile, input.XIAdminPasswordFile, input.XIReaderPasswordFile, input.XIAdminDSNFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("installed joint credential and trust paths must be absolute and normalized")
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("installed joint credential or trust file unavailable")
		}
	}
	if input.XIAdminUsername == "" || input.XIReaderUsername == "" || input.XIAdminUsername == input.XIReaderUsername ||
		!regexp.MustCompile(`^xs_[a-z0-9]{16,64}$`).MatchString(input.XIReaderSubjectID) {
		return errors.New("installed joint requires separate admin and reader identities")
	}
	cert, err := tls.LoadX509KeyPair(input.TLSCertFile, input.TLSKeyFile)
	if err != nil || len(cert.Certificate) == 0 {
		return errors.New("UR test receiver certificate/key unavailable")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || leaf.VerifyHostname(ip.String()) != nil {
		return errors.New("UR test receiver certificate does not cover the bound IP")
	}
	caPEM, err := os.ReadFile(input.TLSCAFile)
	if err != nil {
		return errors.New("UR test receiver CA unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return errors.New("UR test receiver CA invalid")
	}
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: ip.String()}); err != nil {
		return errors.New("UR test receiver certificate is not signed by configured CA")
	}
	return nil
}

// jointManagedControl is fixture wiring around the real durable control journal,
// HTTP counterpart and transactional Agent. It never creates a phase/result or
// feed/machine bearer. Only the synthetic human's accepted installation can be
// admitted to the fixture's independently fixed receiver network scope.
type jointManagedControl struct {
	origin            string
	installed         *installedXIJointInput
	journal           *xinsightconnector.PostgresPhaseJournal
	executor          *XInsightConnectorActivationExecutor
	mu                sync.RWMutex
	identities        map[string]string
	projects          map[int64]bool
	futureProject     bool
	normalSetup       bool
	contentReady      func(context.Context) error
	handler           *Handler
	peer              *config.XInsightConnectorStaticPeer
	counterpartClient *http.Client
	network           xinsightconnector.ReceiverNetworkBindingV2
}

func (m *jointManagedControl) pinOrigin(origin string) {
	// Match XI's managed-feed config authority using its frozen field order.
	authority := struct {
		SchemaVersion string `json:"schema_version"`
		TrustedOrigin string `json:"trusted_origin"`
		Transport     string `json:"transport"`
		Route         string `json:"route"`
	}{"xinsight.managed-feed-network-binding.v1", origin, "https", xinsightconnector.ControlFixedReceiverRoute}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.network = xinsightconnector.ReceiverNetworkBindingV2{Transport: "https", Route: xinsightconnector.ControlFixedReceiverRoute,
		BindingSHA256: xinsightconnector.PublicCanonicalSHA256(authority)}
}

func (m *jointManagedControl) ResolveXInsightReceiverBinding(_ context.Context, installation, deployment, digest string) (XInsightReceiverBinding, error) {
	if m.normalSetup {
		return XInsightReceiverBinding{}, xinsightconnector.ErrConflict
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.identities[installation] != deployment || deployment == "" || digest != m.network.BindingSHA256 {
		return XInsightReceiverBinding{}, xinsightconnector.ErrConflict
	}
	return XInsightReceiverBinding{BindingSHA256: m.network.BindingSHA256, AllowedIP: "127.0.0.1/32"}, nil
}

func (m *jointManagedControl) ResolveXInsightReceiverBindingTx(ctx context.Context, tx xinsightconnector.PhaseProductTx, installation, deployment, digest string) (XInsightReceiverBinding, error) {
	if m.normalSetup {
		return ResolveStaticXInsightReceiverBindingTx(ctx, tx, m.peer, installation, deployment, digest)
	}
	return m.ResolveXInsightReceiverBinding(ctx, installation, deployment, digest)
}

func (m *jointManagedControl) admits(d xinsightconnector.RuntimeInstallationDescriptor) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.normalSetup {
		return m.peer != nil && d.XDeploymentID == m.peer.XDeploymentID
	}
	return d.XDeploymentID != "" && m.identities[d.InstallationID] == d.XDeploymentID
}

func (m *jointManagedControl) registerRoutes(t *testing.T, f *jointHTTPFixture, mux *http.ServeMux, resolver middleware.ClientIPResolver) {
	t.Helper()
	service, err := completedfeed.NewService(f.pool, syntheticSourceControlSecret, f.source.SourceInstance(), true)
	require.NoError(t, err)
	f.handler.SetCompletedFeedController(service)
	manager, err := xinsightconnector.NewManager(f.pool, xinsightconnector.CommitAuthorizerFunc(f.handler.AuthorizeXInsightConnectorControl), service)
	require.NoError(t, err)
	f.handler.SetXInsightConnectorManager(manager)
	m.handler = f.handler
	if m.normalSetup {
		// Normal adoption binds through the shared transaction, so the old
		// human bind POST cannot be its publication signal. Observe only the
		// actual committed setup-owned C enrollment after the original loop.
		m.contentReady = func(ctx context.Context) error {
			var active bool
			err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1
 FROM xinsight_source_enrollment_bindings b
 JOIN xinsight_source_enrollments e ON e.id=b.enrollment_id AND e.org_id=b.org_id
 JOIN xinsight_connector_installations i ON i.installation_id=e.installation_id AND i.org_id=e.org_id
 JOIN api_keys k ON k.id=b.api_key_id AND k.org_id=b.org_id AND k.project_id=b.project_id
 WHERE b.org_id=$1 AND b.project_id=$2 AND b.kind='content' AND b.state='active'
 AND e.state='active' AND i.project_id=$2 AND i.setup_request_id IS NOT NULL
 AND i.desired_state='active' AND k.status='feed_active')`, f.orgID, f.projectID).Scan(&active)
			if err != nil {
				return err
			}
			if active {
				f.registrationOnce.Do(func() { close(f.contentRegistered) })
			}
			return nil
		}
	}
	m.journal, err = xinsightconnector.NewPostgresPhaseJournal(f.pool, f.handler.encKey)
	require.NoError(t, err)
	intake, err := xinsightconnector.NewInvitationIntake(manager, m.journal, m.origin, "stable")
	require.NoError(t, err)
	f.handler.SetXInsightConnectorInvitationIntake(intake)
	m.executor, err = NewXInsightConnectorActivationExecutor(f.handler, m)
	require.NoError(t, err)
	m.identities = make(map[string]string)
	m.projects = map[int64]bool{f.projectID: true}
	gate := middleware.CompletedExecutionFeedAuth(f.pool, syntheticSourceControlSecret, resolver)
	mux.Handle("GET /api/v1/integrations/xinsight/completed-executions", gate(service.Handler()))
	mux.Handle("GET /api/v1/integrations/xinsight/connector-installations/{installation_id}/source-binding", gate(f.handler.XInsightSourceDescriptorHandler(m)))

	// This seam models the explicitly authorized synthetic human, not a human
	// JWT or elevated authority inside XI's controller. Preserve HTTP request
	// cancellation and production saved-actor reauthorization in each handler.
	template := f.request(t, "joint-managed-human", map[string]any{}).Context()
	human := func(handler http.HandlerFunc, installed, bound bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if raw := r.PathValue("project_id"); raw != "" {
				id, err := strconv.ParseInt(raw, 10, 64)
				m.mu.RLock()
				allowed := m.projects[id]
				m.mu.RUnlock()
				if err != nil || !allowed || raw != stringInt64(id) {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
			if id := r.PathValue("org_id"); id != "" && id != stringInt64(f.orgID) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			ctx := r.Context()
			for _, key := range []any{middleware.CtxUserID, middleware.CtxOrgID, middleware.CtxUserRole,
				middleware.CtxOrganizationRole, middleware.CtxAuthEpoch, middleware.CtxAdminSessionHash} {
				if value := template.Value(key); value != nil {
					ctx = context.WithValue(ctx, key, value)
				}
			}
			requestIdentity := sha256.Sum256([]byte(r.Method + " " + r.URL.Path))
			ctx = context.WithValue(ctx, middleware.CtxRequestID, "joint-human-"+hex.EncodeToString(requestIdentity[:16]))
			recorded := httptest.NewRecorder()
			handler(recorded, r.WithContext(ctx))
			if installed && recorded.Code == http.StatusOK {
				var id, deployment string
				err := f.pool.QueryRow(r.Context(), `SELECT installation_id,x_deployment_id FROM xinsight_connector_installations
					WHERE org_id=$1 AND project_id=$2 AND desired_state='active'`, f.orgID, f.projectID).Scan(&id, &deployment)
				if err != nil {
					t.Errorf("joint accepted installation readback: %v", err)
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				m.mu.Lock()
				m.identities[id] = deployment
				m.mu.Unlock()
			}
			if bound && recorded.Code == http.StatusOK {
				var active bool
				err := f.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM xinsight_source_enrollment_bindings
					WHERE org_id=$1 AND project_id=$2 AND kind='content' AND state='active')`, f.orgID, f.projectID).Scan(&active)
				if err != nil {
					t.Errorf("joint existing C binding readback: %v", err)
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if active {
					f.registrationOnce.Do(func() { close(f.contentRegistered) })
				}
			}
			for name, values := range recorded.Header() {
				w.Header()[name] = append([]string(nil), values...)
			}
			w.WriteHeader(recorded.Code)
			_, _ = w.Write(recorded.Body.Bytes())
		})
	}
	if m.futureProject {
		create := human(f.handler.CreateProject, false, false)
		mux.Handle("POST /api/v1/projects", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The approved synthetic human may create only in this exact org.
			// Formal CreateProject still owns saved-actor, quota and audit checks.
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
			var scope struct {
				OrgID int64 `json:"org_id"`
			}
			if err != nil || json.Unmarshal(raw, &scope) != nil || scope.OrgID != f.orgID {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			response := httptest.NewRecorder()
			create.ServeHTTP(response, r)
			if response.Code == http.StatusCreated {
				var project struct {
					ID    int64 `json:"id"`
					OrgID int64 `json:"org_id"`
				}
				if json.Unmarshal(response.Body.Bytes(), &project) != nil || project.ID <= 0 || project.OrgID != f.orgID {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				var owned bool
				if err := f.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND org_id=$2 AND status='active')`, project.ID, f.orgID).Scan(&owned); err != nil || !owned {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				m.mu.Lock()
				m.projects[project.ID] = true
				m.mu.Unlock()
			}
			for name, values := range response.Header() {
				w.Header()[name] = append([]string(nil), values...)
			}
			w.WriteHeader(response.Code)
			_, _ = w.Write(response.Body.Bytes())
		}))
	}
	const base = "/api/v1/data-exchange/xinsight/connections/{project_id}"
	mux.Handle("GET "+base, human(f.handler.GetXInsightFeedConnection, false, false))
	mux.Handle("POST "+base+"/install", human(f.handler.InstallXInsightConnector, true, false))
	mux.Handle("POST "+base+"/credentials", human(f.handler.CreateXInsightFeedCredential, false, false))
	mux.Handle("POST "+base+"/credentials/activate", human(f.handler.ActivateInitialXInsightFeedCredential, false, false))
	mux.Handle("POST "+base+"/resume", human(f.handler.ResumeXInsightFeedConnection, false, false))
	const org = "/api/v2/organizations/{org_id}"
	if m.normalSetup {
		mux.Handle("GET "+org+"/xinsight/setup-peer", human(f.handler.GetXInsightSetupPeer, false, false))
		mux.Handle("POST "+org+"/xinsight/setup-previews", human(f.handler.PreviewXInsightSetup, false, false))
		mux.Handle("POST "+org+"/xinsight/setups", human(f.handler.CreateXInsightSetup, false, false))
		mux.Handle("GET "+org+"/xinsight/setups/{installation_id}", human(f.handler.GetXInsightSetupStatus, false, false))
	}
	mux.Handle("POST "+org+"/xinsight-policy-credentials", human(f.handler.RegisterXInsightMachineCredential, false, false))
	mux.Handle("GET "+org+"/xinsight-policy-credentials", human(f.handler.ListXInsightMachineCredentials, false, false))
	mux.Handle("POST "+org+"/xinsight-policy-credentials/{key_id}/activate", human(f.handler.ActivateXInsightMachineCredential, false, false))
	mux.Handle("POST "+org+"/xinsight-policy-delegations", human(f.handler.CreateXInsightPolicyDelegation, false, false))
	mux.Handle("GET "+org+"/xinsight-policy-delegations", human(f.handler.ListXInsightPolicyDelegations, false, false))
	mux.Handle("POST "+org+"/xinsight-source-enrollments", human(f.handler.CreateXInsightSourceEnrollment, false, false))
	mux.Handle("GET "+org+"/xinsight-source-enrollments", human(f.handler.ListXInsightSourceEnrollments, false, false))
	const existing = org + "/xinsight-source-enrollments/{enrollment_id}/projects/{project_id}/bind-existing"
	mux.Handle("GET "+existing, human(f.handler.GetXInsightBindExistingCandidates, false, false))
	mux.Handle("POST "+existing, human(f.handler.BindExistingXInsightSourceEnrollment, false, true))
	const sources = "/api/v1/data-exchange/xinsight/source-connections/{project_id}"
	mux.Handle("GET "+sources, human(f.handler.GetXInsightSourceConnection, false, false))
	mux.Handle("GET "+sources+"/content", human(f.handler.GetXInsightContentConnection, false, false))
	for _, action := range []string{"prepare", "activate", "resume", "profiles"} {
		mux.Handle("POST "+sources+"/"+action, human(f.handler.SourceControl(action), false, false))
	}
	for _, action := range []string{"prepare", "activate", "resume"} {
		mux.Handle("POST "+sources+"/content/"+action, human(f.handler.ContentControl(action), false, false))
	}
}

// jointControlTransport observes only the last actual HTTP outcome. The production
// supervisor lives in package main and cannot be imported here. Keep this existing
// fixture loop's next-pass recovery limited to HTTP 502/503/504 and typed transient network
// failures; decoding, authentication, scope and journal errors still stop the test.
// No URL, headers, body, credential or installation identity is recorded.
type jointControlTransport struct {
	base     http.RoundTripper
	recovery string
	ackPhase string
}

func (r *jointControlTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	r.recovery = ""
	r.ackPhase = ""
	if request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/api/v1/connector-control/operations/") && request.GetBody != nil {
		copy, err := request.GetBody()
		if err == nil {
			var projection struct {
				SchemaVersion string `json:"schema_version"`
				Phase         string `json:"phase"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(copy, 1<<20)).Decode(&projection)
			_ = copy.Close()
			if decodeErr == nil && (projection.SchemaVersion == xinsightconnector.ControlResultV2Schema || projection.SchemaVersion == xinsightconnector.ControlResultV3Schema) {
				switch projection.Phase {
				case xinsightconnector.ControlPhasePreparePaused, xinsightconnector.ControlPhaseRegisterReceiver, xinsightconnector.ControlPhaseResumeSource,
					xinsightconnector.ControlPhaseBindExisting, xinsightconnector.ControlPhaseConfirmExisting, xinsightconnector.ControlPhaseDeliverMachine:
					r.ackPhase = projection.SchemaVersion + "/" + projection.Phase
				}
			}
		}
	}
	response, err := r.base.RoundTrip(request)
	if err != nil {
		var networkError net.Error
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
			errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
			(errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())) {
			r.recovery = "transient transport failure"
		}
	} else if response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusGatewayTimeout {
		// A real TLS proxy returns 502 while the installed XI process restarts.
		// Production already retries failed cycles; do not turn that supported
		// lifecycle into a fixture-only terminal failure.
		r.recovery = fmt.Sprintf("HTTP %d", response.StatusCode)
	}
	return response, err
}

func TestXInsightManagedJointRecoveryBoundaryIntegration(t *testing.T) {
	status := http.StatusServiceUnavailable
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	defer server.Close()
	transport := &jointControlTransport{base: server.Client().Transport}
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	for _, candidate := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusInternalServerError, http.StatusBadRequest,
		http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusGone, http.StatusOK} {
		status = candidate
		response, err := transport.RoundTrip(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		if candidate == http.StatusBadGateway || candidate == http.StatusServiceUnavailable || candidate == http.StatusGatewayTimeout {
			require.Equal(t, fmt.Sprintf("HTTP %d", candidate), transport.recovery)
		} else {
			require.Empty(t, transport.recovery, "authentication/scope/contract outcomes must not inherit a prior 503")
		}
	}
	for _, phase := range []string{xinsightconnector.ControlPhaseDeliverMachine, "unrecognized-private-value"} {
		wire, err := json.Marshal(map[string]string{"schema_version": xinsightconnector.ControlResultV3Schema, "phase": phase})
		require.NoError(t, err)
		ack, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/connector-control/operations/xco_AAAAAAAAAAAAAAAAAAAAAA", bytes.NewReader(wire))
		require.NoError(t, err)
		response, err := transport.RoundTrip(ack)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		if phase == xinsightconnector.ControlPhaseDeliverMachine {
			require.Equal(t, xinsightconnector.ControlResultV3Schema+"/"+phase, transport.ackPhase)
		} else {
			require.Empty(t, transport.ackPhase)
		}
	}
	server.Close()
	_, err = transport.RoundTrip(request)
	require.Error(t, err)
	require.Equal(t, "transient transport failure", transport.recovery)
}

func (m *jointManagedControl) run(ctx context.Context, client *http.Client, logf func(string, ...any)) error {
	transport := &jointControlTransport{base: client.Transport}
	observedClient := *client
	observedClient.Transport = transport
	client = &observedClient
	ticker := time.NewTicker(time.Duration(config.XInsightConnectorDefaultPollMilliseconds) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		// Match the production supervisor's frozen runnable discovery: a redeem
		// completed in this pass cannot be polled until a later discovery pass.
		runnable, err := m.journal.ListRunnableInstallations(ctx, 10)
		if err != nil {
			return err
		}
		pending, err := m.journal.ListRedeemPendingInstallations(ctx, 10)
		if err != nil {
			return err
		}
		for _, d := range pending {
			if !m.admits(d) {
				continue
			}
			if m.normalSetup {
				if err := m.handler.CheckXInsightSetupControlAuthority(ctx, d.InstallationID); err != nil {
					return fmt.Errorf("normal setup outgoing authority: %w", err)
				}
			}
			counterpart, err := xinsightconnector.NewHTTPControlCounterpart(m.origin, d, m.journal, client, 10*time.Second)
			if err != nil {
				return err
			}
			transport.recovery = ""
			if err = counterpart.Redeem(ctx); err != nil {
				if ctx.Err() == nil && transport.recovery != "" {
					logf("joint UR redeem pending after %s; existing next pass retains durable request", transport.recovery)
					continue
				}
				return fmt.Errorf("joint UR redeem: %w", err)
			}
		}
		for _, d := range runnable {
			if !m.admits(d) {
				continue
			}
			if m.normalSetup {
				if err := m.handler.CheckXInsightSetupControlAuthority(ctx, d.InstallationID); err != nil {
					return fmt.Errorf("normal setup outgoing authority: %w", err)
				}
			}
			counterpart, err := xinsightconnector.NewHTTPControlCounterpart(m.origin, d, m.journal, client, 10*time.Second)
			if err != nil {
				return err
			}
			agent, err := xinsightconnector.NewTransactionalAgent(d.InstallationID, counterpart, m.journal, m.executor)
			if err != nil {
				return err
			}
			transport.recovery = ""
			if _, err = agent.RunOnce(ctx); err != nil {
				if ctx.Err() == nil && transport.recovery != "" {
					logf("joint UR poll/execute/ack pending after %s; existing next pass retains durable terminal/ACK replay", transport.recovery)
					continue
				}
				if transport.ackPhase != "" {
					return fmt.Errorf("joint UR ACK %s: %w", transport.ackPhase, err)
				}
				return fmt.Errorf("joint UR poll/execute/ack: %w", err)
			}
		}
		if m.normalSetup {
			if err := m.handler.AdvanceXInsightSetups(ctx, 32); err != nil {
				return fmt.Errorf("normal setup advance: %w", err)
			}
			if err := m.contentReady(ctx); err != nil {
				return fmt.Errorf("normal setup actual C binding readback: %w", err)
			}
		}
	}
}

// Read only a bounded state projection before this fixture's original cleanup.
// Never select bearer/verifier/digest/body/registration or output row identities.
func (f jointHTTPFixture) logManagedEnrollmentState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
	defer cancel()
	rows, err := f.pool.Query(ctx, `SELECT e.revision,e.state,
 CASE WHEN cardinality(e.profiles)=0 THEN 'not_requested' WHEN fb.api_key_id IS NULL AND fc.project_id IS NOT NULL THEN 'requires_migration' WHEN fb.api_key_id IS NULL THEN 'pending' WHEN fb.state='active' AND fk.status='feed_active' THEN 'active' ELSE 'revoked' END,
 CASE WHEN NOT e.include_content THEN 'not_requested' WHEN cb.api_key_id IS NULL AND cc.project_id IS NOT NULL THEN 'requires_migration' WHEN cb.api_key_id IS NULL THEN 'pending' WHEN cb.state='active' AND ck.status='feed_active' THEN 'active' ELSE 'revoked' END,
 COALESCE(fb.state='active',false),COALESCE(cb.state='active',false),COALESCE(fk.status,'absent'),COALESCE(ck.status,'absent'),
 COALESCE(fc.status,'absent'),COALESCE(fc.scope_generation,0),COALESCE(cc.status,'absent'),COALESCE(cc.state_version,0)
 FROM xinsight_source_enrollments e
 LEFT JOIN xinsight_source_enrollment_bindings fb ON fb.enrollment_id=e.id AND fb.org_id=e.org_id AND fb.project_id=$2 AND fb.kind='facts'
 LEFT JOIN xinsight_source_enrollment_bindings cb ON cb.enrollment_id=e.id AND cb.org_id=e.org_id AND cb.project_id=$2 AND cb.kind='content'
 LEFT JOIN api_keys fk ON fk.id=fb.api_key_id LEFT JOIN api_keys ck ON ck.id=cb.api_key_id
 LEFT JOIN execution_source_connections fc ON fc.org_id=e.org_id AND fc.project_id=$2
 LEFT JOIN execution_source_content_scopes cc ON cc.org_id=e.org_id AND cc.project_id=$2
 WHERE e.org_id=$1 AND (e.exact_project_id=$2 OR e.coverage='all_current_future') ORDER BY e.id LIMIT 4`, f.orgID, f.projectID)
	if err != nil {
		t.Log("joint UR enrollment state projection unavailable")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var revision, factsRevision, contentRevision int64
		var state, facts, content, factsKey, contentKey, factsConfig, contentConfig string
		var factsBinding, contentBinding bool
		if err := rows.Scan(&revision, &state, &facts, &content, &factsBinding, &contentBinding, &factsKey, &contentKey, &factsConfig, &factsRevision, &contentConfig, &contentRevision); err != nil {
			t.Log("joint UR enrollment state projection scan unavailable")
			return
		}
		t.Logf("joint UR enrollment revision=%d state=%s facts=%s content=%s active_facts_binding=%t active_content_binding=%t facts_key=%s content_key=%s facts_config=%s facts_scope_generation=%d content_config=%s content_state_version=%d", revision, state, facts, content, factsBinding, contentBinding, factsKey, contentKey, factsConfig, factsRevision, contentConfig, contentRevision)
	}
	if rows.Err() != nil {
		t.Log("joint UR enrollment state projection incomplete")
	}
}

func runManagedJointConsumer(t *testing.T, mode string) {
	runManagedJointConsumerWithInstalled(t, mode, nil)
}

func runManagedJointConsumerWithInstalled(t *testing.T, mode string, installed *installedXIJointInput) {
	t.Helper()
	require.Contains(t, []string{"new_install", "adopt_existing"}, mode)
	checkout, xiDSN := os.Getenv("XI_JOINT_TEST_CHECKOUT"), os.Getenv("XI_JOINT_POSTGRES_DSN")
	if checkout == "" {
		if installed != nil {
			t.Fatal("installed joint requires XI_JOINT_TEST_CHECKOUT")
		}
		t.Skip("managed joint test requires XI_JOINT_TEST_CHECKOUT")
	}
	if installed == nil {
		require.NotEmpty(t, xiDSN, "managed joint test requires isolated XI_JOINT_POSTGRES_DSN")
	} else {
		require.Empty(t, xiDSN, "installed XI driver reads the protected DSN file directly")
		require.True(t, filepath.IsAbs(checkout) && filepath.Clean(checkout) == checkout, "installed XI checkout must be absolute and normalized")
	}
	require.FileExists(t, filepath.Join(checkout, "go.mod"))
	binary := filepath.Join(t.TempDir(), "xi-joint-driver")
	var build *exec.Cmd
	if installed == nil {
		build = exec.CommandContext(t.Context(), "go", "test", "-tags", "integration", "-c", "-o", binary, "./internal/db")
	} else {
		build = exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./scripts/ci-fpv15-installed-joint")
	}
	build.Dir = checkout
	output, err := build.CombinedOutput()
	require.NoError(t, err, "compile XI managed joint helper: %s", string(output))
	var listener net.Listener
	if installed == nil {
		listener, err = net.Listen("tcp4", "127.0.0.1:0")
		require.NoError(t, err)
		defer listener.Close()
	}
	future := os.Getenv("XI_JOINT_FUTURE_PROJECT") == "1"
	if installed != nil {
		future = installed.FutureProject
	}
	require.False(t, future && mode != "new_install", "minimal future-project variant uses new_install only")
	managed := &jointManagedControl{futureProject: future, normalSetup: os.Getenv("XI_JOINT_NORMAL_SETUP") == "1", installed: installed}
	if installed == nil {
		managed.origin = "https://" + listener.Addr().String()
	} else {
		managed.origin, managed.normalSetup = installed.XIOrigin, true
	}
	f := newJointHTTPFixtureWithManaged(t, true, managed)
	defer func() {
		if t.Failed() {
			f.logManagedEnrollmentState(t)
		}
	}()
	defer f.publishOnce.Do(func() { close(f.contentPublished) })
	requestID := "managed-joint-" + jointRandomHex(t, 12)
	payload := []byte(`{"role":"assistant","content":"managed joint synthetic answer"}`)
	digest := sha256.Sum256(payload)
	input := struct {
		ManagedMode            string                                     `json:"managed_mode"`
		FutureProject          bool                                       `json:"managed_future_projects,omitempty"`
		NormalSetup            bool                                       `json:"managed_normal_setup,omitempty"`
		XIControlOrigin        string                                     `json:"xi_control_origin"`
		ReceiverNetworkBinding xinsightconnector.ReceiverNetworkBindingV2 `json:"receiver_network_binding"`
		ReceiverAllowedIP      string                                     `json:"receiver_allowed_ip"`
		Profiles               []string                                   `json:"profiles"`
		BaseURL                string                                     `json:"base_url"`
		TLSCAPEM               string                                     `json:"tls_ca_pem"`
		SourceInstance         string                                     `json:"source_instance"`
		OrgID                  string                                     `json:"org_id"`
		ProjectID              string                                     `json:"project_id"`
		ForeignProjectID       string                                     `json:"foreign_project_id"`
		PostgresDSN            string                                     `json:"postgres_dsn"`
		ExpectedContent        struct {
			RequestID string `json:"request_id"`
			SHA256    string `json:"sha256"`
		} `json:"expected_content"`
		Installed *installedXIJointInput `json:"installed,omitempty"`
	}{ManagedMode: mode, FutureProject: future, NormalSetup: managed.normalSetup, XIControlOrigin: managed.origin, ReceiverNetworkBinding: managed.network,
		ReceiverAllowedIP: "127.0.0.1/32", Profiles: sourceEnrollmentEightProfiles(), Installed: installed,
		BaseURL: f.server.URL, TLSCAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.server.Certificate().Raw})),
		SourceInstance: f.source.SourceInstance(), OrgID: stringInt64(f.orgID), ProjectID: stringInt64(f.projectID),
		ForeignProjectID: stringInt64(f.foreignProjectID), PostgresDSN: xiDSN}
	if installed != nil {
		input.ReceiverAllowedIP = installed.ReceiverAllowedIP
	}
	input.ExpectedContent.RequestID, input.ExpectedContent.SHA256 = requestID, hex.EncodeToString(digest[:])
	wire, err := json.Marshal(input)
	require.NoError(t, err)
	timeout := 2 * time.Minute
	if installed != nil {
		timeout = time.Duration(installed.TimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	var child *exec.Cmd
	if installed == nil {
		child = exec.CommandContext(ctx, binary, "-test.run=^TestXIJointURConsumer$", "-test.v")
		child.Dir = filepath.Join(checkout, "internal", "db")
		child.Env = append(os.Environ(), "XINSIGHT_JOINT_UR_CONSUMER=1", "XINSIGHT_POSTGRES_DSN="+xiDSN)
	} else {
		child = exec.CommandContext(ctx, binary)
		child.Dir = checkout
		child.Env = []string{"PATH=" + os.Getenv("PATH"), "GITHUB_ACTIONS=true", "XINSIGHT_CI_EPHEMERAL_HOST=1", "XINSIGHT_CI_INSTALLED_JOINT=1"}
	}
	child.Stdin = bytes.NewReader(wire)
	var childOutput bytes.Buffer
	child.Stdout, child.Stderr = &childOutput, &childOutput
	// Both httptest TLS endpoints use the Go test certificate explicitly pinned
	// in stdin and this client. XI must use httptest.StartTLS, never insecure TLS.
	if listener != nil {
		require.NoError(t, listener.Close())
	}
	require.NoError(t, child.Start())
	finished, runtimeDone := make(chan error, 1), make(chan error, 1)
	go func() { finished <- child.Wait() }()
	counterpartClient := f.server.Client()
	if managed.counterpartClient != nil {
		counterpartClient = managed.counterpartClient
	}
	go func() { runtimeDone <- managed.run(ctx, counterpartClient, t.Logf) }()
	completed, runtimeCompleted := false, false
	defer func() {
		cancel()
		if !completed {
			<-finished
		}
		if !runtimeCompleted {
			<-runtimeDone
		}
	}()
	redact := func(raw string) string {
		if installed != nil {
			var safe []string
			for _, line := range strings.Split(raw, "\n") {
				if line == "" {
					continue
				}
				var receipt struct {
					SchemaVersion   string `json:"schema_version"`
					Stage           string `json:"stage"`
					State           string `json:"state"`
					ElapsedMS       int64  `json:"elapsed_ms"`
					FailureCategory string `json:"failure_category"`
				}
				if json.Unmarshal([]byte(line), &receipt) != nil ||
					!strings.HasPrefix(receipt.SchemaVersion, "xinsight.ci-installed-joint-") ||
					!regexp.MustCompile(`^[a-z_]{1,40}$`).MatchString(receipt.Stage) ||
					!regexp.MustCompile(`^(passed|failed)$`).MatchString(receipt.State) ||
					receipt.ElapsedMS < 0 || receipt.ElapsedMS > 1200000 ||
					receipt.FailureCategory != "" && !regexp.MustCompile(`^[a-z_]{1,60}$`).MatchString(receipt.FailureCategory) {
					return "installed_driver_output_unavailable"
				}
				safe = append(safe, fmt.Sprintf("%s/%s/%s/%d/%s", receipt.SchemaVersion, receipt.Stage, receipt.State, receipt.ElapsedMS, receipt.FailureCategory))
			}
			if len(safe) == 0 || len(safe) > 24 {
				return "installed_driver_output_unavailable"
			}
			return strings.Join(safe, ";")
		}
		if xiDSN != "" {
			raw = strings.ReplaceAll(raw, xiDSN, "[redacted]")
		}
		// XI owns new bearer generation; do not assume UR knows their values.
		return regexp.MustCompile(`(?:ur-x(?:f|pm|se)1-|xiiv1_|xib_)[A-Za-z0-9_-]{21,}`).ReplaceAllString(raw, "[redacted]")
	}
	childCompletion := func() error {
		// A failing XI helper closes its TLS server before its process exits.
		// Join its output before reporting the coincident transport failure.
		// Never read the shared output buffer while the child still writes.
		select {
		case err := <-finished:
			completed = true
			return err
		case <-time.After(time.Second):
			cancel()
			err := <-finished
			completed = true
			return err
		}
	}
	await := func(signal <-chan struct{}, description string) {
		select {
		case <-signal:
		case err := <-finished:
			completed = true
			if installed != nil {
				t.Fatalf("installed joint failed before %s category=xi_driver_exit receipts=%s", description, redact(childOutput.String()))
			}
			t.Fatalf("XI managed helper exited before %s: %v; output: %s", description, err, redact(childOutput.String()))
		case err := <-runtimeDone:
			runtimeCompleted = true
			childErr := childCompletion()
			if installed != nil {
				t.Fatalf("installed joint failed before %s category=ur_control_failed receipts=%s", description, redact(childOutput.String()))
			}
			t.Fatalf("UR managed control failed before %s: %v; XI child: %v; output: %s", description, err, childErr, redact(childOutput.String()))
		case <-ctx.Done():
			if installed != nil {
				_ = childCompletion()
				t.Fatalf("installed joint failed before %s category=deadline receipts=%s", description, redact(childOutput.String()))
			}
			t.Fatalf("managed joint %s: %v", description, ctx.Err())
		}
	}
	await(f.contentRegistered, "Source C enrollment or existing C binding")
	await(f.aBootstrapped, "real Source A page")
	object := f.emitSourceContent(t, requestID, payload)
	require.Equal(t, input.ExpectedContent.SHA256, object.SHA256)
	f.publishOnce.Do(func() { close(f.contentPublished) })
	select {
	case err = <-finished:
		completed = true
		if installed != nil && err != nil {
			t.Fatalf("installed joint category=xi_driver_exit receipts=%s", redact(childOutput.String()))
		}
		require.NoError(t, err, "XI managed helper failed: %s", redact(childOutput.String()))
		if installed != nil {
			t.Logf("XI installed stage receipts: %s", redact(childOutput.String()))
		}
	case err = <-runtimeDone:
		runtimeCompleted = true
		childErr := childCompletion()
		if childErr != nil {
			if installed != nil {
				t.Fatalf("installed joint category=ur_control_failed receipts=%s", redact(childOutput.String()))
			}
			t.Fatalf("UR managed control failed: %v; XI child: %v; output: %s", err, childErr, redact(childOutput.String()))
		}
	case <-ctx.Done():
		if installed != nil {
			_ = childCompletion()
			t.Fatalf("installed joint category=deadline receipts=%s", redact(childOutput.String()))
		}
		t.Fatalf("managed joint completion: %v", ctx.Err())
	}
	// Confirm real UR terminal/credential/grant records rather than treating a
	// successful XI child process as the only acceptance authority.
	var installations, candidates, purposes, activeMachines, delegations, enrollments int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_connector_installations WHERE org_id=$1 AND project_id=$2 AND receiver_mode=$3 AND observed_state='connected'`, f.orgID, f.projectID, mode).Scan(&installations))
	require.Equal(t, 1, installations)
	var activeFeed, stagedFeed, confirmedAdoptions int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FILTER(WHERE status='feed_active'),count(*) FILTER(WHERE status='feed_staged') FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read'`, f.orgID, f.projectID).Scan(&activeFeed, &stagedFeed))
	require.Equal(t, 1, activeFeed)
	require.Zero(t, stagedFeed)
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_receiver_adoptions a JOIN api_keys k ON k.id=a.feed_key_id AND k.org_id=a.org_id AND k.project_id=a.project_id WHERE a.org_id=$1 AND a.project_id=$2 AND a.status='confirmed' AND k.purpose='xinsight.feed.read' AND k.status='feed_active'`, f.orgID, f.projectID).Scan(&confirmedAdoptions))
	if mode == "adopt_existing" {
		require.Equal(t, 1, confirmedAdoptions)
	} else {
		require.Zero(t, confirmedAdoptions)
	}
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*),count(DISTINCT convert_from(request_canonical,'UTF8')::jsonb->'request'->'registration'->>'purpose') FROM xinsight_connector_phase_operations WHERE convert_from(request_canonical,'UTF8')::jsonb->>'kind'='pair_machine' AND convert_from(request_canonical,'UTF8')::jsonb->>'phase'='deliver_machine_candidate' AND convert_from(result_canonical,'UTF8')::jsonb->>'status'='completed' AND acknowledged_at IS NOT NULL AND org_id=$1 AND project_id=$2`, f.orgID, f.projectID).Scan(&candidates, &purposes))
	require.Equal(t, 2, candidates)
	require.Equal(t, 2, purposes)
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM api_keys WHERE org_id=$1 AND project_id=$2 AND status='feed_active' AND purpose IN ('xinsight.policy.manage','xinsight.source.enroll')`, f.orgID, f.projectID).Scan(&activeMachines))
	require.Equal(t, 2, activeMachines)
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_policy_delegations WHERE org_id=$1 AND (($3 AND coverage='all_current_future' AND exact_project_id IS NULL) OR (NOT $3 AND exact_project_id=$2)) AND state='active' AND can_read AND can_write`, f.orgID, f.projectID, future || managed.normalSetup).Scan(&delegations))
	require.Equal(t, 1, delegations)
	if managed.normalSetup {
		var defaults, saved int
		require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_policy_delegations WHERE org_id=$1 AND installation_id=(SELECT installation_id FROM xinsight_connector_installations WHERE org_id=$1 AND project_id=$2) AND state='active' AND target_kind='enterprise_default' AND coverage='exact' AND exact_project_id IS NULL AND can_read AND can_write`, f.orgID, f.projectID).Scan(&defaults))
		require.Equal(t, 1, defaults)
		require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_connector_installations WHERE org_id=$1 AND setup_request_id IS NOT NULL`, f.orgID).Scan(&saved))
		require.Equal(t, 1, saved)
	}
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_source_enrollments WHERE org_id=$1 AND (($4 AND coverage='all_current_future' AND exact_project_id IS NULL) OR (NOT $4 AND exact_project_id=$2)) AND state='active' AND profiles=$3::text[] AND include_content`, f.orgID, f.projectID, sourceEnrollmentEightProfiles(), future || managed.normalSetup).Scan(&enrollments))
	require.Equal(t, 1, enrollments)
	if future {
		managed.mu.RLock()
		projectCount := len(managed.projects)
		managed.mu.RUnlock()
		require.Equal(t, 2, projectCount, "one original project and one formal future project")
		var bindings, projects int
		require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*),count(DISTINCT b.project_id) FROM xinsight_source_enrollment_bindings b JOIN xinsight_source_enrollments e ON e.id=b.enrollment_id JOIN api_keys k ON k.id=b.api_key_id WHERE e.org_id=$1 AND e.coverage='all_current_future' AND e.state='active' AND b.state='active' AND k.status='feed_active'`, f.orgID).Scan(&bindings, &projects))
		require.Equal(t, 4, bindings)
		require.Equal(t, 2, projects)
	}
}

func TestXInsightManagedJointFutureProjectRouteIntegration(t *testing.T) {
	managed := &jointManagedControl{origin: "https://127.0.0.1:55439", futureProject: true}
	f := newJointHTTPFixtureWithManaged(t, false, managed)
	response := f.machineHTTP(t, http.MethodPost, "/api/v1/projects", "", map[string]any{"org_id": f.orgID + 10000, "name": "unapproved joint project"})
	require.Equal(t, http.StatusForbidden, response.StatusCode)
	_ = response.Body.Close()
	response = f.machineHTTP(t, http.MethodPost, "/api/v1/projects", "", map[string]any{"org_id": f.orgID, "name": "Joint future project", "slug": "joint-future-" + jointRandomHex(t, 8)})
	require.Equal(t, http.StatusCreated, response.StatusCode)
	var project struct {
		ID    int64 `json:"id"`
		OrgID int64 `json:"org_id"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&project))
	_ = response.Body.Close()
	require.Positive(t, project.ID)
	require.Equal(t, f.orgID, project.OrgID)
	require.NotEqual(t, f.projectID, project.ID)
	managed.mu.RLock()
	allowed := managed.projects[project.ID]
	count := len(managed.projects)
	managed.mu.RUnlock()
	require.True(t, allowed)
	require.Equal(t, 2, count)
	response = f.machineHTTP(t, http.MethodGet, "/api/v1/data-exchange/xinsight/connections/"+stringInt64(project.ID), "", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	_ = response.Body.Close()
	response = f.machineHTTP(t, http.MethodGet, "/api/v1/data-exchange/xinsight/connections/"+stringInt64(f.foreignProjectID), "", nil)
	require.Equal(t, http.StatusForbidden, response.StatusCode)
	_ = response.Body.Close()
	f.logManagedEnrollmentState(t)
	var audit int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_logs WHERE action='project.create' AND resource_id=$1`, stringInt64(project.ID)).Scan(&audit))
	require.Equal(t, 1, audit)
}

func TestXInsightManagedJointServerWiringIntegration(t *testing.T) {
	managed := &jointManagedControl{origin: "https://127.0.0.1:55439"}
	f := newJointHTTPFixtureWithManaged(t, false, managed)
	var installations, machines, grants int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_connector_installations`).Scan(&installations))
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_machine_credentials`).Scan(&machines))
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM xinsight_policy_delegations)+(SELECT count(*) FROM xinsight_source_enrollments)`).Scan(&grants))
	require.Zero(t, installations)
	require.Zero(t, machines)
	require.Zero(t, grants)
	for _, path := range []string{"/api/v1/integrations/xinsight/completed-executions", "/api/v1/integrations/xinsight/connector-installations/xci_AAAAAAAAAAAAAAAAAAAAAA/source-binding"} {
		response := f.machineHTTP(t, http.MethodGet, path, "", nil)
		require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	}
	response := f.machineHTTP(t, http.MethodGet, "/api/v1/data-exchange/xinsight/connections/"+stringInt64(f.projectID), "", nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	response = f.machineHTTP(t, http.MethodGet, "/api/v1/data-exchange/xinsight/connections/"+stringInt64(f.foreignProjectID), "", nil)
	require.Equal(t, http.StatusForbidden, response.StatusCode)
	require.Equal(t, "https", managed.network.Transport)
	require.Equal(t, xinsightconnector.ControlFixedReceiverRoute, managed.network.Route)
	require.Len(t, managed.network.BindingSHA256, 71)
	// Prove the adoption prestate can be built through actual supported HTTP
	// human handlers, without adding a feed bearer/key identity to stdin.
	registration, bearer := testXInsightFeedRegistration(t, "managed-joint-original-receiver")
	path := "/api/v1/data-exchange/xinsight/connections/" + stringInt64(f.projectID)
	response = f.machineHTTP(t, http.MethodPost, path+"/credentials", "", createXInsightFeedCredentialRequest{AllowedIP: "127.0.0.1/32", Registration: registration})
	require.Equal(t, http.StatusCreated, response.StatusCode)
	var staged stagedXInsightFeedCredentialResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&staged))
	require.Positive(t, staged.ID)
	response = f.machineHTTP(t, http.MethodPost, path+"/credentials/activate", "", xInsightFeedCredentialIDRequest{StagedCredentialID: staged.ID})
	require.Equal(t, http.StatusOK, response.StatusCode)
	response = f.machineHTTP(t, http.MethodPost, path+"/resume", "", map[string]any{})
	require.Equal(t, http.StatusOK, response.StatusCode)
	response = f.machineHTTP(t, http.MethodGet, "/api/v1/integrations/xinsight/completed-executions", bearer, nil)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var page completedfeed.FeedPage
	require.NoError(t, json.NewDecoder(response.Body).Decode(&page))
	require.Empty(t, page.Items)
	var activeID int64
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT id FROM api_keys WHERE org_id=$1 AND project_id=$2 AND purpose='xinsight.feed.read' AND status='feed_active'`, f.orgID, f.projectID).Scan(&activeID))
	require.Equal(t, staged.ID, activeID)
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT count(*) FROM xinsight_connector_installations`).Scan(&installations))
	require.Zero(t, installations)
	// The accepted human identity must be pinned independently before the
	// executor will resolve any receiver network scope.
	_, err := managed.ResolveXInsightReceiverBinding(t.Context(), cp2bInstallationID, cp2bDeploymentID, managed.network.BindingSHA256)
	require.ErrorIs(t, err, xinsightconnector.ErrConflict)
}
