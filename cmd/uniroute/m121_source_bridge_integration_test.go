//go:build integration

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

// The current binary must serve on the exact M126 ledger while the predecessor
// A/B/C producer and existing C binding remain active. A test-owned ready row
// joins a real file-store write to the authenticated HTTP body-read path.
func TestActiveSourceM126MainStartupIntegration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.CommercialLicenseRetirementHead)
	ctx := t.Context()
	var org, project, key int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('M121 bridge','active','standard') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'M121 bridge','m121-bridge','active','none') RETURNING id`, org).Scan(&project))
	producer := "m121-existing-producer"
	_, err := pool.Exec(ctx, `INSERT INTO execution_source_connections(org_id,project_id,source_instance,status,profiles) VALUES($1,$2,$3,'active',ARRAY['unirouter.execution-context.v1']::text[])`, org, project, producer)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,$3)`, org, project, producer)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO execution_source_content_scopes(org_id,project_id,status,state_version,access_generation) VALUES($1,$2,'active',1,2)`, org, project)
	require.NoError(t, err)
	secret := strings.Repeat("s", 32)
	raw := "ur-xf1-" + strings.Repeat("a", 64)
	hash, prefix, err := auth.HashReceiverFeedCredential(raw, secret)
	require.NoError(t, err)
	verifier, err := auth.ReceiverFeedVerifierHex(raw)
	require.NoError(t, err)
	operation := strings.Repeat("b", 32)
	registration := sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat,
		VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: prefix, VerifierSHA256: verifier, OperationID: operation,
		SourceInstance: producer, OrgID: strconv.FormatInt(org, 10), ProjectID: strconv.FormatInt(project, 10), Profiles: []string{sourcefacts.ContentProfile}}
	registrationHash, err := sourcefacts.HashContentRegistration(registration)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit,budget_limit,allowed_ips,allowed_models,policy_labels,purpose,verifier_scheme,registration_operation_id,source_lifecycle_scope)
VALUES($1,$2,'service','M121 existing content',$3,$4,'feed_active',60,1,0,'127.0.0.1/32','','','xinsight.source.read',$5,$6,'content') RETURNING id`, org, project, hash, prefix, auth.ReceiverFeedVerifierScheme, operation).Scan(&key))
	_, err = pool.Exec(ctx, `INSERT INTO execution_source_credentials(api_key_id,org_id,project_id,registration_sha256,profiles,source_lifecycle_scope) VALUES($1,$2,$3,$4,ARRAY[$5]::text[],'content')`, key, org, project, registrationHash, sourcefacts.ContentProfile)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO execution_source_content_bindings(api_key_id,org_id,project_id,coverage_after_sequence,coverage_time,allowed_purposes) VALUES($1,$2,$3,0,clock_timestamp(),ARRAY['human_review']::text[])`, key, org, project)
	require.NoError(t, err)
	source, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ObservationsEnabled: true, ContentEnabled: true, SourceInstance: producer, CursorSecret: secret})
	require.NoError(t, err)
	require.NoError(t, source.CheckReady(ctx))
	authority, err := source.NewContentBodyAuthority(middleware.ClientIPResolver{})
	require.NoError(t, err)
	request := httptest.NewRequest("GET", "http://example.invalid/content", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer "+raw)
	_, failure := authority.Authenticate(ctx, request)
	require.Nil(t, failure, "existing unmarked C key must read on M121 without querying M123 enrollment tables")

	storageRoot, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(storageRoot, 0700))
	binding, err := contentcapture.InitializeFileStore(storageRoot, "m121-existing-store")
	require.NoError(t, err)
	files, err := contentcapture.OpenFileStore(storageRoot, binding)
	require.NoError(t, err)
	defer files.Close()
	body := []byte("M121 retained content read")
	receipt, err := files.Save(ctx, bytes.NewReader(body), contentcapture.StoreLimits{MaxSavedBytes: 1024, MaxReadBytes: 1024, MaxBufferBytes: 1024})
	require.NoError(t, err)
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	expires := stamp.Add(time.Hour)
	format := func(value time.Time) string { return value.Format("2006-01-02T15:04:05.000000Z") }
	manifest := sourcefacts.ContentManifest{SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: producer,
		OrgID: strconv.FormatInt(org, 10), ProjectID: strconv.FormatInt(project, 10), RequestID: "m121-content-read",
		Profile: sourcefacts.ContentProfile, Revision: "1", OccurredAt: format(stamp), RecordedAt: format(stamp),
		Protocol: "openai.chat", ObservationPoint: "client_request", ContentItemID: "body-0",
		Capture:    sourcefacts.ContentCapture{Mode: contentcapture.ModeFullContent, PolicyRef: "m121-policy", PolicyVersion: "1", PolicyEffectiveAt: format(stamp), Completeness: "complete", Reason: "none"},
		ContentRef: "uc1_" + strings.Repeat("c", 64), Representation: &sourcefacts.ContentRepresentation{MediaType: "text/plain", Encoding: "identity", ByteDomain: sourcefacts.ContentByteDomain, SavedBytes: int64(len(body)), Digest: eventconnector.HashSourceBytes(body)},
		CapturedAt: format(stamp), ExpiresAt: format(expires)}
	manifest.SourceFactID = manifest.FactID()
	manifest.EventID = manifest.EventIDForRevision()
	retention, quota, maximum := int64(3600), int64(1024), int64(1024)
	policy := contentcapture.PolicyConfig{Mode: contentcapture.ModeFullContent, SupportedProtocols: []string{manifest.Protocol}, ObservationPoints: []string{manifest.ObservationPoint}, AllowedPurposes: []string{sourcefacts.ContentHumanReview}, RetentionSeconds: &retention, QuotaBytes: &quota, MaxSavedBytes: &maximum, OnLimit: "record_capture_gap"}
	require.NoError(t, policy.Validate())
	policyWire, err := json.Marshal(policy)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO execution_content_objects(content_ref,org_id,project_id,source_fact_id,revision,storage_identity,storage_binding_token,policy_version,recovery_epoch,relative_path,state,reserved_bytes,saved_bytes,digest,expires_at,lease_owner,lease_expires_at,policy_config,policy_effective_at)
VALUES($1,$2,$3,$4,1,$5,$6,1,1,$7,'ready',$8,$8,$9,$10,'m121-fixture',clock_timestamp()+interval '1 hour',$11,$12)`, manifest.ContentRef, org, project, manifest.SourceFactID, receipt.Binding.Identity, receipt.Binding.Token, receipt.ObjectName, receipt.SavedBytes, receipt.SHA256, expires, policyWire, stamp)
	require.NoError(t, err)
	_, err = source.AppendContentManifest(ctx, manifest, 1)
	require.NoError(t, err)
	tokenRoot, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(tokenRoot, 0700))
	tokenFile := filepath.Join(tokenRoot, "binding.token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(binding.Token+"\n"), 0600))
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	binary := filepath.Join(t.TempDir(), "uniroute-m121-bridge")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	build.Env = os.Environ()
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	f, err := os.Open(binary)
	require.NoError(t, err)
	h := sha256.New()
	_, err = io.Copy(h, f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	logfile, err := os.Create(filepath.Join(t.TempDir(), "main.log"))
	require.NoError(t, err)
	defer logfile.Close()
	command := exec.Command(binary)
	command.Dir = root
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"),
		"DB_DSN=" + pool.Config().ConnConfig.ConnString(), "REDIS_ADDR=" + os.Getenv("REDIS_ADDR"), "REDIS_PASSWORD=" + os.Getenv("REDIS_PASSWORD"),
		"JWT_SECRET=" + secret, "API_KEY_HASH_SECRET=" + secret, "ENCRYPTION_KEY=0123456789abcdef0123456789abcdef",
		"MODEL_ROUTE_AUTHORITY=legacy", "MODEL_ROUTE_COMPATIBILITY_ENABLED=true", "RUNTIME_ENVIRONMENT=development", "SKIP_MIGRATION=true", "MIGRATIONS_DIR=" + filepath.Join(root, "migrations"),
		"SOURCE_FACTS_ENABLED=true", "SOURCE_OBSERVATIONS_ENABLED=true", "SOURCE_BUDGET_SNAPSHOTS_ENABLED=false", "SOURCE_CONTENT_ENABLED=true", "SOURCE_CONTENT_FULL_ENABLED=true",
		"SOURCE_CONTENT_STORAGE_ROOT=" + storageRoot, "SOURCE_CONTENT_STORAGE_IDENTITY=" + binding.Identity, "SOURCE_CONTENT_STORAGE_TOKEN_FILE=" + tokenFile,
		"COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE=" + producer, "ASYNC_MEDIA_ENABLED=false", "ASYNC_MEDIA_REQUEST_TTL_HOURS=24", "LISTEN_PORT=" + strconv.Itoa(port),
		"RUNTIME_IMAGE_DIGEST=sha256:" + hex.EncodeToString(h.Sum(nil))}
	command.Stdout, command.Stderr = logfile, logfile
	require.NoError(t, command.Start())
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = command.Process.Kill()
			<-done
		}
	})
	readLog := func() string {
		wire, readErr := os.ReadFile(logfile.Name())
		require.NoError(t, readErr)
		return string(wire)
	}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health/ready", port)
	ready := false
	for !ready {
		select {
		case exit := <-done:
			reaped = true
			t.Fatalf("M121 main exited before ready: %v\n%s", exit, readLog())
		case <-deadline.C:
			t.Fatalf("M121 main did not become ready\n%s", readLog())
		case <-tick.C:
			response, getErr := client.Get(healthURL)
			if getErr == nil {
				ready = response.StatusCode == http.StatusOK
				require.NoError(t, response.Body.Close())
			}
		}
	}
	contentURL := fmt.Sprintf("http://127.0.0.1:%d%s%s?purpose=human_review&request_id=%s&source_fact_id=%s&revision=1", port,
		sourcefacts.ContentBodyPathPrefix, manifest.ContentRef, url.QueryEscape(manifest.RequestID), url.QueryEscape(manifest.SourceFactID))
	contentRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, contentURL, nil)
	require.NoError(t, err)
	contentRequest.Header.Set("Authorization", "Bearer "+raw)
	contentResponse, err := client.Do(contentRequest)
	require.NoError(t, err)
	readBody, err := io.ReadAll(contentResponse.Body)
	require.NoError(t, err)
	require.NoError(t, contentResponse.Body.Close())
	require.Equal(t, http.StatusOK, contentResponse.StatusCode, string(readBody))
	require.Equal(t, body, readBody)
	var head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.CommercialLicenseRetirementHead, head, "startup must not expand the schema")
	var activeBinding bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_source_content_bindings WHERE api_key_id=$1 AND org_id=$2 AND project_id=$3)`, key, org, project).Scan(&activeBinding))
	require.True(t, activeBinding)
	require.NoError(t, command.Process.Signal(syscall.SIGTERM))
	select {
	case exit := <-done:
		reaped = true
		require.NoError(t, exit, readLog())
	case <-time.After(15 * time.Second):
		t.Fatal("M121 main did not stop gracefully")
	}
}
