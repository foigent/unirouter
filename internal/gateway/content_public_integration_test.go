//go:build integration

package gateway_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type publicCaptureTracker struct {
	service  *sourcefacts.ContentCaptureService
	mu       sync.Mutex
	captures []*contentcapture.Capture
}

func (h *publicCaptureTracker) Begin(ctx context.Context, id contentcapture.Observation) (*contentcapture.Capture, error) {
	c, err := h.service.Begin(ctx, id)
	if c != nil {
		h.mu.Lock()
		h.captures = append(h.captures, c)
		h.mu.Unlock()
	}
	return c, err
}

func TestContentPublicResponsesMessagesOriginalSemanticsPG(t *testing.T) {
	for _, operation := range []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages} {
		for _, mode := range []string{"nonstream", "complete", "cancel", "output_held"} {
			t.Run(string(operation)+"/"+mode, func(t *testing.T) {
				var f gateway.SourceFactsGatewayFixture
				if mode == "complete" || mode == "cancel" {
					f = gateway.NewSourceFactsGatewayStreamFixture(t, operation, "1.3.0", mode)
				} else {
					f = gateway.NewSourceFactsGatewayFixture(t, operation)
				}
				original, journal, raw := sourceGatewayAttachSource(t, f)
				source, err := sourcefacts.New(f.Pool, sourcefacts.Options{Enabled: true, ContentEnabled: true, SourceInstance: original.SourceInstance(), CursorSecret: sourceGatewaySyntheticSecret, I01Enabled: true})
				require.NoError(t, err)
				var project int64
				require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT project_id FROM api_keys WHERE id=$1`, f.KeyID).Scan(&project))
				root, err := filepath.EvalSymlinks(t.TempDir())
				require.NoError(t, err)
				binding, err := contentcapture.InitializeFileStore(root, "synthetic-public-content")
				require.NoError(t, err)
				files, err := contentcapture.OpenFileStore(root, binding)
				require.NoError(t, err)
				defer files.Close()
				limit, quota, retention := int64(1<<20), int64(16<<20), int64(3600)
				protocols := []string{string(operation)}
				points := []string{"client_request", "provider_request", "provider_response", "client_response"}
				policy := contentcapture.PolicyConfig{Mode: contentcapture.ModeFullContent, SupportedProtocols: protocols, ObservationPoints: points, RetentionSeconds: &retention, QuotaBytes: &quota, MaxSavedBytes: &limit, AllowedPurposes: []string{"human_review"}, OnLimit: "record_capture_gap"}
				wire, err := json.Marshal(policy)
				require.NoError(t, err)
				_, err = f.Pool.Exec(t.Context(), `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason)VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, f.OrgID, project, wire)
				require.NoError(t, err)
				service, err := sourcefacts.NewContentCaptureService(source, files, sourcefacts.ContentCaptureServiceConfig{Policy: contentcapture.PolicyStoreOptions{SourceInstance: source.SourceInstance(), DefaultMode: contentcapture.ModeMetadataOnly, FullContentEnabled: true, SupportedProtocols: protocols, ObservationPoints: points, Limits: contentcapture.PolicyLimits{MaxSavedBytes: limit, MaxReadBytes: limit, MaxBufferBytes: 32 << 10, MaxManifestBytes: 64 << 10, MaxManifestItems: 100}}, Engine: contentcapture.EngineOptions{Queue: contentcapture.QueueLimits{MaxItems: 1024, MaxBytes: 4 << 20, MaxProjectBytes: 4 << 20, MaxChunkBytes: 32 << 10}, MaxActive: 8, MaxProjectActive: 8, PrepareTimeout: time.Second, FinalizeTimeout: 5 * time.Second, MaxLifetime: time.Minute}, LeaseOwner: "synthetic-public", LeaseTTL: time.Minute, StartTimeout: time.Second, HealthTimeout: time.Second})
				require.NoError(t, err)
				defer func() {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					require.NoError(t, service.Close(ctx))
				}()
				tracker := &publicCaptureTracker{service: service}
				f.Relay.SetContentCapture(tracker)
				if mode == "output_held" {
					guard, err := guardrail.NewService(f.Pool, nil, []byte(strings.Repeat("s", 32)), guardrail.DefaultOptions())
					require.NoError(t, err)
					require.NoError(t, guard.RegisterDetector("external:source-synthetic", sourceSyntheticDetector{}))
					_, err = f.Pool.Exec(t.Context(), `INSERT INTO guardrail_policies(org_id,name,stage,detector_ref,action,mode,failure_mode,config)VALUES($1,'synthetic-content-hold','nonstream_output','external:source-synthetic','block','enforce','fail_closed','{}')`, f.OrgID)
					require.NoError(t, err)
					require.NoError(t, guard.Refresh(t.Context()))
					f.Relay.SetGuardrail(guard)
				}
				handler := sourceGatewayHandlerWithAuth(t, f, f.Relay, original, middleware.GatewayAuthWithAPIKeyHashSecret(f.Pool, sourceGatewaySyntheticSecret), tracker)
				body := " \n" + string(f.Body) + "\n "
				r := httptest.NewRequest(http.MethodPost, f.Path, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				if operation == modeloperation.IDMessages {
					r.Header.Set("x-api-key", raw)
					r.Header.Set("anthropic-version", "2023-06-01")
				} else {
					r.Header.Set("Authorization", "Bearer "+raw)
				}
				w := httptest.NewRecorder()
				if mode == "cancel" {
					ctx, cancel := context.WithCancel(r.Context())
					defer cancel()
					handler.ServeHTTP(&sourceCancelWriter{ResponseRecorder: w, cancel: cancel}, r.WithContext(ctx))
				} else {
					handler.ServeHTTP(w, r)
				}
				wantStatus := 200
				if mode == "output_held" {
					wantStatus = 403
					require.NotContains(t, w.Body.String(), "synthetic answer")
				}
				require.Equal(t, wantStatus, w.Code, w.Body.String())
				require.EqualValues(t, 1, f.ProviderCalls())
				request := w.Header().Get(middleware.OperationIDHeader)
				require.NotEmpty(t, request)
				_, err = journal.DrainOnce(t.Context())
				require.NoError(t, err)
				var state string
				require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT status FROM freeze_idempotency WHERE request_id=$1`, request).Scan(&state))
				if mode == "cancel" || mode == "output_held" {
					require.Equal(t, "settlement_pending", state)
				} else {
					require.Equal(t, "settled", state)
				}
				tracker.mu.Lock()
				captures := append([]*contentcapture.Capture(nil), tracker.captures...)
				tracker.mu.Unlock()
				require.Len(t, captures, 4)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				for _, capture := range captures {
					result, err := capture.Wait(ctx)
					require.NoError(t, err)
					require.NoError(t, result.FinalizeError)
				}
				rows, err := f.Pool.Query(ctx, `SELECT document FROM execution_content_manifests WHERE request_id=$1 AND revision=2`, request)
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
				require.Len(t, manifests, 4)
				for _, m := range manifests {
					require.Equal(t, string(operation), m.Protocol)
					if strings.HasPrefix(m.ObservationPoint, "provider_") {
						var exists bool
						require.NoError(t, f.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM billing_provider_attempts WHERE request_id=$1 AND attempt_id=$2)`, request, m.AttemptID).Scan(&exists))
						require.True(t, exists)
					} else {
						require.Empty(t, m.AttemptID)
					}
					if m.ContentRef == "" {
						require.Equal(t, "cancel", mode)
						require.NotEqual(t, "complete", m.Capture.Completeness)
						continue
					}
					var receipt contentcapture.StoredReceipt
					require.NoError(t, f.Pool.QueryRow(ctx, `SELECT storage_identity,storage_binding_token,relative_path,saved_bytes,digest FROM execution_content_objects WHERE content_ref=$1 AND state='ready'`, m.ContentRef).Scan(&receipt.Binding.Identity, &receipt.Binding.Token, &receipt.ObjectName, &receipt.SavedBytes, &receipt.SHA256))
					file, err := files.OpenVerified(ctx, receipt, contentcapture.StoreLimits{MaxSavedBytes: limit, MaxReadBytes: limit, MaxBufferBytes: 32 << 10})
					require.NoError(t, err)
					saved, err := io.ReadAll(file)
					require.NoError(t, err)
					require.NoError(t, file.Close())
					switch m.ObservationPoint {
					case "client_request":
						require.Equal(t, body, string(saved))
					case "client_response":
						require.Equal(t, w.Body.Bytes(), saved)
					case "provider_request":
						require.Equal(t, f.ProviderRequests()[0], string(saved))
					case "provider_response":
						require.NotEmpty(t, saved)
						if mode == "output_held" {
							require.Contains(t, string(saved), "synthetic answer")
						}
					}
				}
			})
		}
	}
}
