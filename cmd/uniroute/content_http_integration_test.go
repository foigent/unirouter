//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type sourceHTTPContentHarness struct {
	pool         *pgxpool.Pool
	runtime      *contentRuntime
	mu           sync.Mutex
	captures     []*contentcapture.Capture
	j1CloseOnce  sync.Once
	j1CloseError error
}

func (h *sourceHTTPContentHarness) Begin(ctx context.Context, id contentcapture.Observation) (*contentcapture.Capture, error) {
	c, err := h.runtime.service.Begin(ctx, id)
	if c != nil {
		h.mu.Lock()
		h.captures = append(h.captures, c)
		h.mu.Unlock()
	}
	return c, err
}
func (h *sourceHTTPContentHarness) CachePersistenceAllowed(ctx context.Context, principal contentcapture.CapturePrincipal, orgID, projectID int64) (bool, error) {
	return h.runtime.service.CachePersistenceAllowed(ctx, principal, orgID, projectID)
}
func newSourceHTTPContentHarness(t *testing.T, pool *pgxpool.Pool, source *sourcefacts.Service, org, project int64, fixtures ...*p3SourceFixture) *sourceHTTPContentHarness {
	t.Helper()
	var interop *p3SourceFixture
	if len(fixtures) == 1 && fixtures[0] != nil && (fixtures[0].j1 || fixtures[0].cc14) {
		interop = fixtures[0]
	}
	var root, tokenFile string
	var err error
	if interop != nil {
		interop.requireContentApproval(t)
		root = filepath.Join(interop.output, "content-store")
		p3Require(t, os.Mkdir(root, 0700) == nil, "J1 fresh content store")
	} else {
		root, err = filepath.EvalSymlinks(t.TempDir())
	}
	require.NoError(t, err)
	require.NoError(t, os.Chmod(root, 0700))
	binding, err := contentcapture.InitializeFileStore(root, "synthetic-http-pg")
	require.NoError(t, err)
	if interop != nil {
		interop.secret(t, "content-store.binding", binding.Token)
		tokenFile = filepath.Join(interop.credentials, "content-store.binding")
	} else {
		tokenRoot, e := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, e)
		require.NoError(t, os.Chmod(tokenRoot, 0700))
		tokenFile = filepath.Join(tokenRoot, "binding")
		require.NoError(t, os.WriteFile(tokenFile, []byte(binding.Token), 0600))
	}
	cfg := &config.Config{SourceContentEnabled: true, SourceContentFullEnabled: true, SourceContentStorageRoot: root, SourceContentStorageIdentity: binding.Identity, SourceContentStorageTokenFile: tokenFile, CompletedExecutionFeedSourceInstance: source.SourceInstance(), PayloadCaptureMode: contentcapture.ModeMetadataOnly, StreamMaxDurationSecs: 1800}
	runtime, err := newContentRuntime(cfg, source)
	require.NoError(t, err)
	harness := &sourceHTTPContentHarness{pool: pool, runtime: runtime}
	t.Cleanup(func() {
		if interop != nil {
			// Join the capture owner even after cancellation, before pool/DB cleanup.
			// The existing outer process supervisor owns a non-terminating child.
			err := harness.closeJ1()
			interop.recordContentStore(t)
			require.NoError(t, err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, runtime.close(ctx, true))
	})
	resolver, err := middleware.NewClientIPResolver(nil)
	require.NoError(t, err)
	metadataConfig := *cfg
	metadataConfig.SourceContentFullEnabled = false
	metadataConfig.SourceContentStorageRoot = ""
	metadataConfig.SourceContentStorageIdentity = ""
	metadataConfig.SourceContentStorageTokenFile = ""
	metadataRuntime, err := newContentRuntime(&metadataConfig, source)
	require.NoError(t, err)
	// Close this temporary owner even if an assertion below calls Goexit.
	var metadataCloseOnce sync.Once
	var metadataCloseErr error
	closeMetadata := func() error {
		metadataCloseOnce.Do(func() { metadataCloseErr = metadataRuntime.close(context.Background(), true) })
		return metadataCloseErr
	}
	defer func() { require.NoError(t, closeMetadata()) }()
	for _, candidate := range []*contentRuntime{runtime, metadataRuntime} {
		mux := http.NewServeMux()
		require.NoError(t, candidate.registerReads(mux, source, resolver))
		for _, path := range []string{sourcefacts.ContentCapabilitiesPath, sourcefacts.ContentManifestsPath + "?purpose=human_review&limit=1", sourcefacts.ContentBodyPathPrefix + "uc1_" + strings.Repeat("a", 64) + "?purpose=human_review&request_id=synthetic&revision=2&source_fact_id=uf1_" + strings.Repeat("b", 64)} {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		}
	}
	require.NoError(t, closeMetadata())

	if interop != nil {
		// J1 activates policy through the authenticated management handler in serve.
		return harness
	}
	limit, quota, retention := int64(32<<20), int64(512<<20), int64(3600)
	policy := contentcapture.PolicyConfig{Mode: contentcapture.ModeFullContent, SupportedProtocols: runtime.options.Policy.SupportedProtocols, ObservationPoints: runtime.options.Policy.ObservationPoints, AllowedPurposes: []string{"human_review"}, RetentionSeconds: &retention, QuotaBytes: &quota, MaxSavedBytes: &limit, OnLimit: "record_capture_gap"}
	wire, err := json.Marshal(policy)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason)VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, org, project, wire)
	require.NoError(t, err)
	return harness
}
func (h *sourceHTTPContentHarness) assertRequest(t *testing.T, request, clientBody string, clientResponse []byte, operation string, attempts int) {
	t.Helper()
	h.mu.Lock()
	captures := append([]*contentcapture.Capture(nil), h.captures...)
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, c := range captures {
		result, err := c.Wait(ctx)
		require.NoError(t, err)
		require.NoError(t, result.FinalizeError)
	}
	rows, err := h.pool.Query(ctx, `SELECT DISTINCT ON(source_fact_id) document FROM execution_content_manifests WHERE request_id=$1 ORDER BY source_fact_id,revision DESC`, request)
	require.NoError(t, err)
	var manifests []sourcefacts.ContentManifest
	for rows.Next() {
		var wire []byte
		require.NoError(t, rows.Scan(&wire))
		m, err := sourcefacts.DecodeContentManifest(wire)
		require.NoError(t, err)
		manifests = append(manifests, m)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Len(t, manifests, 2+2*attempts, "all actual observation points must exist")
	points := map[string]int{}
	providerAttempts := map[string]bool{}
	for _, m := range manifests {
		points[m.ObservationPoint]++
		require.Equal(t, "2", m.Revision)
		require.NotEqual(t, "pending", m.Capture.Completeness)
		if strings.HasPrefix(m.ObservationPoint, "provider_") {
			var exists bool
			require.NoError(t, h.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM billing_provider_attempts WHERE request_id=$1 AND attempt_id=$2)`, request, m.AttemptID).Scan(&exists))
			require.True(t, exists, "C must bind original committed attempt")
			providerAttempts[m.AttemptID] = true
		} else {
			require.Empty(t, m.AttemptID)
		}
		if m.ContentRef == "" {
			require.NotEqual(t, "complete", m.Capture.Completeness)
			require.Contains(t, []string{"chat_stream_cancel", "chat_stream_write_failure", "chat_stream_flush_failure"}, operation)
			continue
		}
		var receipt contentcapture.StoredReceipt
		require.NoError(t, h.pool.QueryRow(ctx, `SELECT storage_identity,storage_binding_token,relative_path,saved_bytes,digest FROM execution_content_objects WHERE content_ref=$1 AND state='ready'`, m.ContentRef).Scan(&receipt.Binding.Identity, &receipt.Binding.Token, &receipt.ObjectName, &receipt.SavedBytes, &receipt.SHA256))
		f, err := h.runtime.files.OpenVerified(ctx, receipt, contentcapture.StoreLimits{MaxSavedBytes: 32 << 20, MaxReadBytes: 32 << 20, MaxBufferBytes: 32 << 10})
		require.NoError(t, err)
		body, err := io.ReadAll(f)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		switch m.ObservationPoint {
		case "client_request":
			require.Equal(t, clientBody, string(body))
			require.Equal(t, "complete", m.Capture.Completeness)
		case "client_response":
			require.Equal(t, clientResponse, body)
		case "provider_request":
			require.Contains(t, string(body), "source-private-prompt")
			require.NotContains(t, string(body), "company-chat")
			require.Contains(t, string(body), "gpt-4o")
			require.Equal(t, "complete", m.Capture.Completeness)
		case "provider_response":
			require.NotEmpty(t, body)
		}
	}
	require.Equal(t, 1, points["client_request"])
	require.Equal(t, 1, points["client_response"])
	require.Len(t, providerAttempts, attempts)
}

// Reuses original HTTP/auth, accounting, cache, fallback and stream assertions.
// The original A/B tests keep their complete identity matrices unchanged.
func TestContentHTTPChatEmbeddingsOriginalSemanticsPG(t *testing.T) {
	sourceChatAndEmbeddingsAuthHistoryAndReplay(t, true, true)
}

func (h *sourceHTTPContentHarness) closeJ1() error {
	h.j1CloseOnce.Do(func() { h.j1CloseError = h.runtime.close(context.Background(), true) })
	return h.j1CloseError
}
