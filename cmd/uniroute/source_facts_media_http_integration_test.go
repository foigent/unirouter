//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/mediajob"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/testutil/sourcereceiver"
)

// Only Provider and object storage are synthetic. Authorization, limits, HTTP,
// the worker, PG writers, source control and authenticated reader are real.
func TestSourceFactsMediaHTTPIntegration(t *testing.T) {
	const mediaHashSecret = "synthetic-media-hash-and-cursor-secret-only"
	require.NotEmpty(t, os.Getenv("TEST_DATABASE_URL"), "select the isolated PG explicitly")
	for _, identity := range []string{"legacy", "grant"} {
		for _, scenario := range []string{"success_replay", "revoke_before_dispatch", "queued_cancel", "inflight_cancel", "success_observations", "observations_required_failure", "observations_optional_failure"} {
			t.Run(identity+"/"+scenario, func(t *testing.T) {
				ctx := t.Context()
				nonce := make([]byte, 16)
				_, nonceErr := rand.Read(nonce)
				require.NoError(t, nonceErr)
				idempotencyKey := "same-media-" + hex.EncodeToString(nonce)
				pool := testutil.SetupMigratedTestDB(t)
				sourceHTTPIsolateRedisIdentities(t, pool)
				org, owner, project := seedPRD2P0OrgProjectUser(t, pool, auth.RoleDeveloper)
				seedPRD2P0ModelGovernance(t, pool, org, project)
				_, err := pool.Exec(ctx, `INSERT INTO model_registry(org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,pricing_model_name) VALUES($1,'gpt-image-1','Synthetic Image','mock','ga',TRUE,'gpt-image-1');`, org)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `UPDATE model_aliases SET target_model='gpt-image-1' WHERE org_id=$1 AND alias='company-chat'`, org)
				require.NoError(t, err)
				raw := "ur-synthetic-media-" + strings.Repeat("a", 32)
				var key, app int64
				if identity == "grant" {
					app, key = sourceHTTPWorkloadGrantWithScope(t, pool, org, project, owner, raw, mediaHashSecret, "/v1/media/jobs")
				} else {
					require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,allowed_models,status) VALUES($1,$2,$3,'personal','Synthetic media',$4,'ur-media','company-chat','active') RETURNING id`, org, project, owner, auth.HashAPIKey(raw, mediaHashSecret)).Scan(&key))
				}
				require.NotEmpty(t, os.Getenv("REDIS_ADDR"))
				rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD")})
				t.Cleanup(func() { _ = rdb.Close() })
				limits, err := limitpolicy.NewService(pool, rdb, limitpolicy.DefaultOptions())
				require.NoError(t, err)
				require.NoError(t, limits.Refresh(ctx))
				evaluator, err := modelauthz.NewEvaluator(pool, "development")
				require.NoError(t, err)
				observations := strings.Contains(scenario, "observations")
				source, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ObservationsEnabled: observations, SourceInstance: "synthetic-media-http", CursorSecret: mediaHashSecret, I01Enabled: true})
				require.NoError(t, err)
				store, err := mediajob.NewPostgresStore(pool, newMediaExecutionAuthorizer(evaluator))
				require.NoError(t, err)
				store.SetSourceMaterializer(source)
				cipher, err := mediajob.NewApplicationCipher([]byte("0123456789abcdef0123456789abcdef"))
				require.NoError(t, err)
				objects := &sourceMediaObjects{}
				service, err := mediajob.NewService(store, objects, cipher, newMediaSubmissionAuthorizer(pool, evaluator), []byte(mediaHashSecret), mediajob.ServiceOptions{RuntimeEnabled: true, AdmissionEnabled: true, Retention: mediajob.DefaultRetention, MaxAttempts: 1, LimitPolicy: limits})
				require.NoError(t, err)
				media, err := mediajob.NewHTTPHandler(service, pool)
				require.NoError(t, err)
				provider := &sourceMediaProvider{}
				var originalProvider mediajob.Provider = provider
				if observations {
					var imageBytes bytes.Buffer
					require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))))
					server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						provider.calls.Add(1)
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(map[string]any{"created": 1, "data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(imageBytes.Bytes())}}, "usage": map[string]any{"input_tokens": 3, "input_tokens_details": map[string]int{"text_tokens": 3, "image_tokens": 0}, "output_tokens": 5, "total_tokens": 8}})
					}))
					defer server.Close()
					seedPRD2P0Channel(t, pool, server.URL, "gpt-image-1")
					imageKey := []byte("0123456789abcdef0123456789abcdef")
					keyEnc, err := auth.EncryptKey([]byte("upstream-test-key"), imageKey)
					require.NoError(t, err)
					_, err = pool.Exec(ctx, `UPDATE channel_keys SET key_enc=$1 WHERE channel_id IN (SELECT id FROM channels WHERE base_url=$2)`, keyEnc, server.URL)
					require.NoError(t, err)
					actual, err := mediajob.NewGPTImageProvider(gateway.NewRouter(pool), imageKey, mediajob.GPTImageProviderOptions{HTTPClient: server.Client()})
					require.NoError(t, err)
					actual.SetSourceObserver(source)
					originalProvider = actual
				}

				worker, err := mediajob.NewWorker(store, objects, originalProvider, cipher, mediajob.WorkerOptions{Owner: "source-media-http", LimitPolicy: limits})
				require.NoError(t, err)
				resolver, err := middleware.NewClientIPResolver(nil)
				require.NoError(t, err)
				mediaAuth := middleware.GatewayAuthWithAgentCapabilitiesAndClientIP(pool, mediaHashSecret, nil, resolver)
				gate := sourceMediaLicense(t).Gate(licensing.FeatureGateway)
				budget := payloadpolicy.RequestMiddlewareWithMax(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), mediajob.MaxRequestBytes, middleware.GetOrgIDFromContext)
				mux := http.NewServeMux()
				mux.Handle("POST /v1/media/jobs", middleware.GatewayAuthAcrossBufferedBody(mediaAuth, budget)(middleware.CaptureExecutionContext(source)(gate(http.HandlerFunc(media.Submit)))))
				mux.Handle("GET /v1/media/jobs/{id}", mediaAuth(gate(http.HandlerFunc(media.Get))))
				mux.Handle("POST /v1/media/jobs/{id}/cancel", mediaAuth(gate(http.HandlerFunc(media.Cancel))))
				server := httptest.NewServer(middleware.OperationIdentityWithSecret(mediaHashSecret)(mux))
				t.Cleanup(server.Close)
				invoke := func(path, token, idem string) (int, []byte) {
					request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, strings.NewReader(`{"model":"company-chat","prompt":"synthetic media prompt"}`))
					require.NoError(t, err)
					request.Header.Set("Authorization", "Bearer "+token)
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Idempotency-Key", idem)
					response, err := server.Client().Do(request)
					require.NoError(t, err)
					defer response.Body.Close()
					wire, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					return response.StatusCode, wire
				}
				observeSource := sourcereceiver.Observe
				if observations {
					observeSource = sourcereceiver.ObserveWithObservations
				}
				observe := observeSource(t, pool, source, mediaHashSecret, mediaHashSecret, org, project, func(m *http.ServeMux, h *admin.Handler, g func(http.HandlerFunc) http.Handler) {
					registerXInsightSourceControlRoutes(m, h, xInsightFeedControlRouteGuards{safety: g, expansion: g})
				}, func(token string) {
					code, _ := invoke("/v1/media/jobs", token, "source-purpose-denied")
					require.Equal(t, http.StatusUnauthorized, code)
				})
				// A real capability token is denied because media's production auth has no
				// Agent runtime. Use its root API key's actual allowed endpoint contract.
				capabilities, err := agentcap.NewService(pool, rdb, mediaHashSecret)
				require.NoError(t, err)
				var agentKey int64
				require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,allowed_models,status) VALUES($1,$2,$3,'personal','Synthetic Agent root',$4,'ur-media-agent','company-chat','active') RETURNING id`, org, project, owner, auth.HashAPIKey("synthetic-agent-root", mediaHashSecret)).Scan(&agentKey))
				issued, err := capabilities.Exchange(ctx, &models.ApiKey{ID: agentKey, OrgID: org}, agentcap.ExchangeRequest{RootAgentID: "agent:synthetic-media", WorkflowTTLSeconds: 3600, Limits: agentcap.Limits{MaxDepth: 1, MaxChildren: 1, MaxDescendants: 1, CostBudgetCents: 100, TokenBudget: 10000, RPMLimit: 60, RPSLimit: 10, ConcurrencyLimit: 1, AllowedModels: []string{"company-chat"}, AllowedEndpoints: []string{"/v1/chat/completions"}}})
				require.NoError(t, err)
				code, _ := invoke("/v1/media/jobs", issued.CapabilityToken, "agent-denied")
				require.Equal(t, http.StatusUnauthorized, code)
				var count int
				require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM async_media_jobs WHERE project_id=$1`, project).Scan(&count))
				require.Zero(t, count)
				require.Zero(t, provider.calls.Load())
				code, wire := invoke("/v1/media/jobs", raw, idempotencyKey)
				require.Equal(t, http.StatusAccepted, code, string(wire))
				var created mediajob.CreateResult
				require.NoError(t, json.Unmarshal(wire, &created))
				job := created.Job
				var admission []byte
				require.NoError(t, pool.QueryRow(ctx, `SELECT source_context FROM async_media_jobs WHERE job_id=$1`, job.JobID).Scan(&admission))
				var captured executioncontext.Snapshot
				require.NoError(t, json.Unmarshal(admission, &captured))
				if identity == "legacy" {
					require.Equal(t, strconv.FormatInt(owner, 10), captured.Identity.OwnerUserID)
				} else {
					require.Equal(t, strconv.FormatInt(app, 10), captured.Core.WorkloadApplicationID)
					require.Equal(t, strconv.FormatInt(app, 10), captured.Identity.GrantSubjectWorkloadID)
					require.Equal(t, strconv.FormatInt(owner, 10), captured.Identity.SponsorUserID)
					require.Empty(t, captured.Identity.OwnerUserID)
				}
				require.Equal(t, "unknown", captured.Identity.ActualHuman)
				code, wire = invoke("/v1/media/jobs", raw, idempotencyKey)
				require.Equal(t, http.StatusOK, code, string(wire))
				var replay mediajob.CreateResult
				require.NoError(t, json.Unmarshal(wire, &replay))
				require.True(t, replay.Reused)
				require.Equal(t, job.JobID, replay.Job.JobID)
				cancelJob := func() {
					code, wire := invoke("/v1/media/jobs/"+job.JobID+"/cancel", raw, "cancel")
					require.Equal(t, http.StatusOK, code, string(wire))
				}
				if scenario == "observations_required_failure" || scenario == "observations_optional_failure" {
					stages := "'attempt_registered'"
					if scenario == "observations_optional_failure" {
						stages = "'provider_response_received','round_trip_measurement','dispatch_result','media_provider_succeeded','media_terminal','media_result'"
					}
					_, err = pool.Exec(ctx, `CREATE FUNCTION reject_media_http_observation() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.stage IN (`+stages+`) THEN RAISE EXCEPTION 'synthetic media HTTP observation failure'; END IF; RETURN NEW; END$$; CREATE TRIGGER reject_media_http_observation BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION reject_media_http_observation()`)
					require.NoError(t, err)
				}
				switch scenario {
				case "revoke_before_dispatch":
					if identity == "legacy" {
						_, err = pool.Exec(ctx, `UPDATE api_keys SET status='disabled',auth_epoch=auth_epoch+1 WHERE id=$1`, key)
					} else {
						_, err = pool.Exec(ctx, `UPDATE access_grants SET status='revoked',revoked_at=NOW() WHERE id=(SELECT access_grant_id FROM api_keys WHERE id=$1)`, key)
					}
					require.NoError(t, err)
					worked, runErr := worker.RunOnce(ctx)
					require.True(t, worked)
					require.ErrorIs(t, runErr, mediajob.ErrForbidden)
				case "queued_cancel":
					cancelJob()
					cancelJob()
				case "inflight_cancel":
					provider.entered = make(chan struct{})
					provider.release = make(chan struct{})
					result := make(chan error, 1)
					go func() { _, err := worker.RunOnce(ctx); result <- err }()
					select {
					case <-provider.entered:
					case <-time.After(10 * time.Second):
						t.Fatal("synthetic Provider not reached")
					}
					cancelJob()
					close(provider.release)
					select {
					case err := <-result:
						require.NoError(t, err)
					case <-time.After(10 * time.Second):
						t.Fatal("worker did not finish")
					}
				default:
					if identity == "legacy" {
						_, err = pool.Exec(ctx, `UPDATE api_keys SET name='Renamed after admission' WHERE id=$1`, key)
					} else {
						_, err = pool.Exec(ctx, `UPDATE workload_applications SET display_name='Renamed after admission' WHERE id=$1`, app)
					}
					require.NoError(t, err)
					worked, runErr := worker.RunOnce(ctx)
					require.True(t, worked)
					require.NoError(t, runErr)
				}
				job, err = store.Get(ctx, mediajob.Scope{OrgID: org, ProjectID: project}, job.JobID)
				require.NoError(t, err)
				require.True(t, job.Terminal())
				var currentAdmission, dispatch []byte
				require.NoError(t, pool.QueryRow(ctx, `SELECT source_context,source_dispatch_context FROM async_media_jobs WHERE job_id=$1`, job.JobID).Scan(&currentAdmission, &dispatch))
				require.JSONEq(t, string(admission), string(currentAdmission))
				charged := scenario == "success_replay" || scenario == "inflight_cancel" || scenario == "success_observations" || scenario == "observations_optional_failure"
				if charged {
					require.EqualValues(t, 1, provider.calls.Load())
					require.Equal(t, 1, job.Attempt)
					require.NotNil(t, job.ChargeFact)
					require.Nil(t, job.ChargeFact.AmountMicros)
					require.NotEmpty(t, job.ChargeFact.FactID)
					require.Equal(t, mediajob.OutcomeSucceeded, job.Lifecycle.Outcome)
					var dispatchSnapshot executioncontext.Snapshot
					require.NoError(t, json.Unmarshal(dispatch, &dispatchSnapshot))
					require.Equal(t, captured.Identity, dispatchSnapshot.Identity)
					if scenario == "inflight_cancel" {
						require.Equal(t, mediajob.CancellationTooLate, job.Lifecycle.Cancellation)
					}
				} else if scenario == "observations_required_failure" {
					require.Zero(t, provider.calls.Load(), "mandatory route registration fails before actual Do")
					require.Equal(t, 1, job.Attempt, "original job dispatch fence was already committed")
					require.Nil(t, job.ChargeFact)
					require.NotEmpty(t, dispatch)
					require.Equal(t, "source_dispatch_unavailable", job.ErrorCode)
				} else {
					require.Zero(t, provider.calls.Load())
					require.Zero(t, job.Attempt)
					require.Nil(t, job.ChargeFact)
					require.Empty(t, dispatch)
					if scenario == "revoke_before_dispatch" {
						require.Equal(t, "execution_authority_changed", job.ErrorCode)
					} else {
						require.Equal(t, mediajob.CancellationAccepted, job.Lifecycle.Cancellation)
					}
				}
				require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM completed_execution_feed_items WHERE project_id=$1`, project).Scan(&count))
				require.Zero(t, count)
				var costWire []byte
				require.NoError(t, pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='terminal'`, job.RequestID, sourcefacts.CostProfile).Scan(&costWire))
				document, err := sourcefacts.Decode(costWire)
				require.NoError(t, err)
				require.Equal(t, "async_media_charge_fact", document.Cost.Reference.Source)
				require.Equal(t, "not_applicable", document.Cost.Reference.PublicationStatus)
				require.Empty(t, document.Cost.Reference.SnapshotSHA256)
				require.Empty(t, document.Cost.Reference.CalculatedAmountPath)
				if charged {
					require.Equal(t, job.ChargeFact.FactID, document.Cost.Reference.ChargeFactRef)
				} else {
					require.Empty(t, document.Cost.Reference.ChargeFactRef)
				}
				if observations {
					rows, err := pool.Query(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=ANY($2) ORDER BY profile,sequence`, job.RequestID, sourcefacts.ObservationProfiles())
					require.NoError(t, err)
					observedKinds := map[string]int{}
					for rows.Next() {
						var wire []byte
						require.NoError(t, rows.Scan(&wire))
						doc, err := sourcefacts.DecodeObservation(wire)
						require.NoError(t, err)
						_, err = sourcefacts.ObservationEvent(doc, wire)
						require.NoError(t, err)
						observedKinds[doc.Kind]++
						if doc.Destination != nil {
							require.Equal(t, "unknown", doc.Destination.InputBindingState)
							require.NotContains(t, doc.Destination.Origin, "/v1")
						}
						if doc.Measurement != nil && doc.Kind == "round_trip_measurement" {
							require.Equal(t, "observed", doc.Measurement.State)
							require.NotNil(t, doc.Measurement.Value)
						}
					}
					require.NoError(t, rows.Err())
					rows.Close()
					for _, kind := range []string{"media_running", "media_attempt_registered", "attempt_registered", "selection", "dispatch_registered", "provider_response_received", "round_trip_measurement", "dispatch_result", "media_provider_succeeded", "media_terminal", "media_result"} {
						expected := 1
						if scenario == "observations_required_failure" && kind != "media_running" && kind != "media_attempt_registered" && kind != "media_terminal" && kind != "media_result" {
							expected = 0
						}
						if scenario == "observations_optional_failure" && kind != "media_running" && kind != "media_attempt_registered" && kind != "attempt_registered" && kind != "selection" && kind != "dispatch_registered" {
							expected = 0
						}
						require.Equal(t, expected, observedKinds[kind], kind)
					}
				}
				observe()
				if root := os.Getenv("SOURCE_FACTS_PAGE_EVIDENCE_DIR"); root != "" {
					path := filepath.Join(root, strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name())+".json")
					wire, err := os.ReadFile(path)
					require.NoError(t, err)
					var fixture map[string]any
					require.NoError(t, json.Unmarshal(wire, &fixture))
					fixture["media_jobs"] = []mediajob.Job{job}
					wire, err = json.MarshalIndent(fixture, "", "  ")
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(path, wire, 0600))
				}
			})
		}
	}
}

type sourceMediaProvider struct {
	calls            atomic.Int64
	entered, release chan struct{}
}

func (p *sourceMediaProvider) Generate(ctx context.Context, job mediajob.Job, _ mediajob.NormalizedRequest) (mediajob.ProviderResult, error) {
	p.calls.Add(1)
	if p.entered != nil {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return mediajob.ProviderResult{}, ctx.Err()
		}
	}
	return mediajob.ProviderResult{MappedModel: "gpt-image-1", Bytes: []byte("synthetic-image-object"), MediaType: "image/png", Outcome: endpointprotocol.Outcome{ContractVersion: endpointprotocol.ContractVersion, ProfileID: endpointprotocol.ProfileOpenAIImageGPT, ProviderState: "completed", MappedModel: "gpt-image-1", OutputCount: 1, Units: []endpointprotocol.UnitObservation{{Class: endpointprotocol.UnitInputTokens, Quantity: "3", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}, {Class: endpointprotocol.UnitInputTextTokens, Quantity: "2", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}, {Class: endpointprotocol.UnitInputImageTokens, Quantity: "1", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}, {Class: endpointprotocol.UnitOutputImageTokens, Quantity: "4", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}}}}, nil
}

type sourceMediaObjects struct{}

func (*sourceMediaObjects) Persist(_ context.Context, job mediajob.Job, result mediajob.ProviderResult) (mediajob.PersistedResult, error) {
	digest := sha256.Sum256(result.Bytes)
	return mediajob.PersistedResult{ObjectKey: "synthetic/" + job.JobID, ReceiptKey: "synthetic/" + job.JobID + "/receipt", ContentSHA256: "sha256:" + hex.EncodeToString(digest[:]), MediaType: result.MediaType, SizeBytes: int64(len(result.Bytes)), PersistedAt: time.Now().UTC(), Outcome: result.Outcome, MappedModel: result.MappedModel}, nil
}
func (*sourceMediaObjects) Recover(context.Context, mediajob.Job) (*mediajob.PersistedResult, error) {
	return nil, nil
}
func (*sourceMediaObjects) Read(context.Context, mediajob.Job) (mediajob.OutputContent, error) {
	return mediajob.OutputContent{}, fmt.Errorf("synthetic media content read is outside this source fixture")
}
func (*sourceMediaObjects) Delete(context.Context, mediajob.Job) error { return nil }
func sourceMediaLicense(t *testing.T) *licensing.Runtime {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	runtime := licensing.NewRuntime(licensing.NewCommercialVerifier(map[string]ed25519.PublicKey{"synthetic-media": pub}, nil, 30, "synthetic-media"))
	now := time.Now().UTC()
	doc, err := licensing.SignCommercialPayload(licensing.Payload{LicenseID: "synthetic-media", Customer: "Synthetic", DeploymentID: "synthetic-media", Tier: "enterprise", IssuedAt: now.Add(-time.Hour).Format(time.RFC3339), NotBefore: now.Add(-time.Hour).Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), Features: []string{licensing.FeatureGateway}, Limits: map[string]int64{licensing.LimitActiveChannels: 10, licensing.LimitActiveAPIKeys: 10, licensing.LimitActiveProjects: 10, licensing.LimitActiveUsers: 10}}, "synthetic-media", priv)
	require.NoError(t, err)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	status := runtime.Replace(raw)
	require.True(t, status.Valid && status.Enforceable)
	return runtime
}
