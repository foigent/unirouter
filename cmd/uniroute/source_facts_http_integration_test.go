//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/capbilling"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/policybilling"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestSourceFactsChatAndEmbeddingsAuthHistoryAndReplayIntegration(t *testing.T) {
	sourceChatAndEmbeddingsAuthHistoryAndReplay(t, false)
}

func TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration(t *testing.T) {
	sourceChatAndEmbeddingsAuthHistoryAndReplay(t, true)
}

// J1 is separate from the frozen P3 selector and never runs without its exact-run approval.
func TestJ1ContentAuthenticatedPullIntegration(t *testing.T) {
	if os.Getenv("UR_J1_INTEROP") != "1" {
		t.Skip("explicit J1 opt-in required")
	}
	j1RequireApproval(t)
	sourceChatAndEmbeddingsAuthHistoryAndReplay(t, true, true)
}

func sourceChatAndEmbeddingsAuthHistoryAndReplay(t *testing.T, observations bool, contentEnabled ...bool) {
	content := len(contentEnabled) == 1 && contentEnabled[0]
	scope, scopeErr := p3ProducerScopeFromEnv(os.Getenv)
	p3Require(t, scopeErr == nil, "approved portable producer scope")
	p3Require(t, !scope.portable || p3ProducerInvocationMatches(t.Name(), observations, content), "portable P3 exact observation entry")
	operations := []string{"chat", "chat_stream", "chat_stream_interrupted", "chat_stream_cancel", "embeddings", "chat_cache"}
	if observations {
		operations = append(operations, "chat_stream_tools", "chat_stream_write_failure", "chat_stream_flush_failure", "chat_fallback", "embeddings_fallback")
	}
	if os.Getenv("UR_J1_INTEROP") != "" {
		require.Equal(t, "TestJ1ContentAuthenticatedPullIntegration", t.Name())
		require.Equal(t, "", os.Getenv("UR_P3_INTEROP"))
		j1RequireApproval(t)
		operations = []string{"chat"}
	}
	for _, operation := range operations {
		identities := []string{"legacy_shared", "legacy_personal", "legacy_application", "grant_personal", "grant_application", "agent"}
		if operation == "chat_cache" || strings.HasPrefix(operation, "chat_stream_") || strings.HasSuffix(operation, "_fallback") {
			identities = []string{"legacy_shared"}
		}
		if content && len(identities) > 1 {
			identities = []string{"legacy_shared", "agent"}
		}
		if os.Getenv("UR_J1_INTEROP") == "1" {
			identities = []string{"legacy_shared"}
		}
		for _, identity := range identities {
			t.Run(operation+"/"+identity, func(t *testing.T) {
				var interop *p3SourceFixture
				if os.Getenv("UR_P3_INTEROP") != "" {
					require.Equal(t, "1", os.Getenv("UR_P3_INTEROP"))
					require.Equal(t, "TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration/chat/legacy_shared", t.Name())
					require.True(t, observations && !content)
					interop = newP3SourceFixture(t)
				}
				if os.Getenv("UR_J1_INTEROP") == "1" {
					require.Equal(t, "TestJ1ContentAuthenticatedPullIntegration/chat/legacy_shared", t.Name())
					require.True(t, observations && content)
					interop = newP3SourceFixture(t)
				}
				pool := testutil.SetupMigratedTestDB(t)
				sourceHTTPIsolateRedisIdentities(t, pool)
				role := "developer"
				if operation == "chat_cache" {
					role = auth.RoleOrgAdmin
				}
				org, owner, project := seedPRD2P0OrgProjectUser(t, pool, role)
				testutil.NewTestWalletCents(t, pool, org, 10000)
				testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)
				seedPRD2P0ModelGovernance(t, pool, org, project)
				var calls atomic.Int64
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					call := calls.Add(1)
					if strings.HasSuffix(operation, "_fallback") && call == 1 {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusServiceUnavailable)
						fmt.Fprint(w, `{"error":{"message":"synthetic retryable rejection","type":"server_error"}}`)
						return
					}
					if strings.HasPrefix(operation, "chat_stream") {
						w.Header().Set("Content-Type", "text/event-stream")
						if operation == "chat_stream_tools" {
							fmt.Fprint(w, "data: {\"id\":\"synthetic-source-chat\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_synthetic\",\"type\":\"function\",\"function\":{\"name\":\"synthetic_echo\",\"arguments\":\"{}\"}}]},\"finish_reason\":null}]}\n\n")
						} else {
							fmt.Fprint(w, "data: {\"id\":\"synthetic-source-chat\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"synthetic answer\"},\"finish_reason\":null}]}\n\n")
						}
						if operation == "chat_stream_interrupted" {
							return
						}
						fmt.Fprint(w, "data: {\"id\":\"synthetic-source-chat\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
						fmt.Fprint(w, "data: {\"id\":\"synthetic-source-chat\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\ndata: [DONE]\n\n")
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if strings.HasPrefix(operation, "embeddings") {
						fmt.Fprint(w, `{"object":"list","model":"gpt-4o","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":10,"total_tokens":10}}`)
					} else {
						fmt.Fprint(w, `{"id":"synthetic-source-chat","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"synthetic answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
					}
				}))
				t.Cleanup(provider.Close)
				seedPRD2P0Channel(t, pool, provider.URL, "gpt-4o")
				if strings.HasSuffix(operation, "_fallback") {
					seedPRD2P0Channel(t, pool, provider.URL, "gpt-4o")
				}
				raw := "ur-synthetic-source-" + strings.Repeat("a", 32)
				var key, app int64
				if identity == "grant_application" {
					app, key = sourceHTTPWorkloadGrant(t, pool, org, project, owner, raw)
				} else if identity == "grant_personal" {
					h := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
					body := fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"synthetic-source","allowed_models":"company-chat"}`, org, project)
					r := httptest.NewRequest(http.MethodPost, "/api/v1/keys", strings.NewReader(body))
					r.Header.Set("Content-Type", "application/json")
					r.Header.Set("Authorization", "Bearer "+mustJWT(t, pool, owner, org, auth.RoleDeveloper))
					w := httptest.NewRecorder()
					middleware.AdminAuth(pool, prd2E2EJWTSecret)(http.HandlerFunc(h.CreateAPIKey)).ServeHTTP(w, r)
					require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
					var response struct {
						ID  int64  `json:"id"`
						Key string `json:"key"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
					key, raw = response.ID, response.Key
				} else {
					kind := "shared"
					if identity == "legacy_personal" || identity == "agent" {
						kind = "personal"
					}
					require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO api_keys(org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,allowed_models,status) VALUES($1,$2,$3,$4,'synthetic-source',$5,'ur-synthetic','company-chat','active') RETURNING id`, org, project, owner, kind, auth.HashAPIKey(raw, prd2E2EJWTSecret)).Scan(&key))
					if identity == "legacy_application" {
						require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO workload_applications(org_id,project_id,application_ref,display_name,kind,environment,identity_mode,client_profile,created_by,updated_by) VALUES($1,$2,'app_source_test','Synthetic','service','test','service_credential','synthetic',$3,$3) RETURNING id`, org, project, owner).Scan(&app))
						_, err := pool.Exec(t.Context(), `UPDATE api_keys SET workload_application_id=$1,key_type='service' WHERE id=$2`, app, key)
						require.NoError(t, err)
					}
				}
				runtime := newSourceHTTPRuntime(t, pool, org, project, key, observations, content, interop)
				source, v1, journal, relay, capabilities := runtime.source, runtime.feed, runtime.journal, runtime.relay, runtime.capabilities
				contentHarness, sourceToken := runtime.content, runtime.sourceToken
				b, rdb := runtime.billing, runtime.redis
				const sourceSecret = "synthetic-source-http-cursor-secret-only"
				var err error
				if interop == nil {
					defer runtime.observe()
				}
				path, body := "/v1/chat/completions", `{"model":"company-chat","messages":[{"role":"user","content":"source-private-prompt"}],"stream":false}`
				final := newGatewayChatHandler(pool, relay)
				if strings.HasPrefix(operation, "chat_stream") {
					body = strings.Replace(body, `"stream":false`, `"stream":true,"stream_options":{"include_usage":true}`, 1)
				}
				if strings.HasPrefix(operation, "embeddings") {
					path, body = "/v1/embeddings", `{"model":"company-chat","input":"source-private-prompt"}`
					final = newGatewayEmbeddingsHandler(pool, relay)
				}
				var workflow string
				if identity == "agent" {
					rootLimits := agentcap.Limits{MaxDepth: 1, MaxChildren: 1, MaxDescendants: 1, CostBudgetCents: 1000, TokenBudget: 100000, RPMLimit: 60, RPSLimit: 10, ConcurrencyLimit: 2, AllowedModels: []string{"company-chat"}, AllowedEndpoints: []string{path}}
					issued, err := capabilities.Exchange(t.Context(), &models.ApiKey{ID: key, OrgID: org}, agentcap.ExchangeRequest{RootAgentID: "agent:source-root", WorkflowTTLSeconds: 3600, Limits: rootLimits})
					require.NoError(t, err)
					parent, err := capabilities.Authenticate(t.Context(), issued.CapabilityToken)
					require.NoError(t, err)
					childLimits := rootLimits
					childLimits.MaxDepth = 1 // Absolute workflow depth, not remaining depth.
					childLimits.MaxChildren = 0
					childLimits.MaxDescendants = 0
					child, err := capabilities.Delegate(t.Context(), parent.Claims, agentcap.DelegateRequest{ChildAgentID: "agent:source-child", Limits: childLimits})
					require.NoError(t, err)
					raw, workflow = child.CapabilityToken, issued.WorkflowID
				}
				handler := runtime.handler(t, pool, final)
				if interop != nil {
					interop.serve(t, pool, source, v1, journal, handler, sourceSecret, sourceToken, org, project, &calls, contentHarness)
					return
				}
				if operation == "chat_cache" {
					sourceHTTPCacheCase(t, pool, relay, handler, journal, org, owner, project, key, raw, &calls, observations, contentHarness)
					return
				}
				invoke := func(document, token, idem string) *httptest.ResponseRecorder {
					r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(document))
					r.Header.Set("Content-Type", "application/json")
					r.Header.Set("Authorization", "Bearer "+token)
					r.Header.Set(middleware.IdempotencyKeyHeader, idem)
					w := httptest.NewRecorder()
					if operation == "chat_stream_cancel" {
						ctx, cancel := context.WithCancel(r.Context())
						defer cancel()
						handler.ServeHTTP(&sourceHTTPStreamCancelWriter{ResponseRecorder: w, cancel: cancel}, r.WithContext(ctx))
					} else if operation == "chat_stream_write_failure" || operation == "chat_stream_flush_failure" {
						handler.ServeHTTP(&sourceHTTPStreamFailureWriter{ResponseRecorder: w, flush: operation == "chat_stream_flush_failure"}, r)
					} else {
						handler.ServeHTTP(w, r)
					}
					return w
				}
				first := invoke(body, raw, "source-same-business-op")
				require.Equal(t, http.StatusOK, first.Code, first.Body.String())
				if operation == "chat_stream" {
					require.Contains(t, first.Body.String(), "data: [DONE]")
					require.Contains(t, first.Body.String(), "synthetic answer")
				}
				request := first.Header().Get(middleware.OperationIDHeader)
				if contentHarness != nil {
					contentHarness.assertRequest(t, request, body, first.Body.Bytes(), operation, int(calls.Load()))
				}
				if operation == "chat_stream_write_failure" || operation == "chat_stream_flush_failure" {
					_, err = journal.DrainOnce(t.Context())
					require.NoError(t, err)
					for _, kind := range []string{"first_output_measurement", "stream_measurement"} {
						var wire []byte
						require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage=$3`, request, sourcefacts.RuntimeProfile, kind).Scan(&wire))
						d, err := sourcefacts.DecodeObservation(wire)
						require.NoError(t, err)
						require.Equal(t, "not_observed", d.Measurement.State)
						require.Nil(t, d.Measurement.Value)
					}
					var outcome, failure string
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT outcome,error_code FROM request_delivery_receipts WHERE request_id=$1 ORDER BY sequence DESC LIMIT 1`, request).Scan(&outcome, &failure))
					require.Contains(t, []string{"interrupted", "write_failed"}, outcome)
					require.Equal(t, "downstream_stream_write_failed", failure)
					require.EqualValues(t, 1, calls.Load())
					return
				}
				if operation == "chat_stream_interrupted" || operation == "chat_stream_cancel" {
					_, err = journal.DrainOnce(t.Context())
					require.NoError(t, err)
					var state string
					var facts, economics int
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT status,(SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND profile=ANY($2)),(SELECT count(*) FROM completed_execution_feed_items WHERE request_id=$1) FROM freeze_idempotency WHERE request_id=$1`, request, sourcefacts.Profiles()).Scan(&state, &facts, &economics))
					// Legacy Chat settles the durable delivered counter on a
					// partial stream. This differs from the strict public V2
					// executor; preserve its estimated usage and exact I-01.
					require.Equal(t, "settled", state)
					require.Equal(t, 7, facts)
					require.Equal(t, 1, economics)
					if observations {
						sourceHTTPAssertObservations(t, pool, request, operation, 1)
					}
					var snapshotWire []byte
					var snapshotHash, sourceHash string
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT snapshot,snapshot_sha256 FROM completed_execution_feed_items WHERE request_id=$1`, request).Scan(&snapshotWire, &snapshotHash))
					var snapshot completedfeed.Snapshot
					require.NoError(t, json.Unmarshal(snapshotWire, &snapshot))
					require.Equal(t, "gateway_counter", snapshot.Usage.Authority)
					require.Equal(t, "customer_charge", snapshot.Usage.Purpose)
					require.Equal(t, "estimated", snapshot.Usage.Finality)
					require.NotEmpty(t, snapshot.Usage.EstimateVersion)
					require.Equal(t, "0", snapshot.Money.ChargedCostMicros)
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT convert_from(document,'UTF8')::jsonb->'cost'->'economic_reference'->>'snapshot_sha256' FROM execution_source_facts WHERE request_id=$1 AND profile=$2`, request, sourcefacts.CostProfile).Scan(&sourceHash))
					require.Equal(t, snapshotHash, sourceHash)
					require.EqualValues(t, 1, calls.Load())
					require.NotContains(t, first.Body.String(), "data: [DONE]")
					return
				}
				// Mutate live legacy ownership after admission but before journal.
				if identity == "legacy_shared" || identity == "legacy_application" {
					_, err = pool.Exec(t.Context(), `UPDATE api_keys SET owner_user_id=NULL,workload_application_id=NULL WHERE id=$1`, key)
					require.NoError(t, err)
				}
				_, err = journal.DrainOnce(t.Context())
				require.NoError(t, err)
				rows, err := pool.Query(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=ANY($2) ORDER BY profile,stage`, request, sourcefacts.Profiles())
				require.NoError(t, err)
				count := 0
				for rows.Next() {
					var wire []byte
					require.NoError(t, rows.Scan(&wire))
					require.False(t, bytes.Contains(wire, []byte("source-private-prompt")))
					require.False(t, bytes.Contains(wire, []byte("synthetic answer")))
					d, err := sourcefacts.Decode(wire)
					require.NoError(t, err)
					count++
					if d.Identity != nil {
						if identity == "grant_application" {
							require.Empty(t, d.Identity.OwnerUserID, "Sponsor is not a personal usage owner")
							require.Equal(t, jsonNumber(owner), d.Identity.SponsorUserID)
							require.NotEmpty(t, d.Identity.GrantID)
						} else {
							require.Equal(t, jsonNumber(owner), d.Identity.OwnerUserID)
						}
						require.Equal(t, "unknown", d.Identity.ActualHuman)
						if identity == "grant_personal" {
							require.NotEmpty(t, d.Identity.GrantID)
						}
						if identity == "agent" {
							require.Equal(t, workflow, d.Identity.WorkflowID)
							require.Equal(t, []string{"agent:source-root", "agent:source-child"}, d.Identity.Lineage)
							require.Equal(t, "agent:source-child", d.Identity.AgentID)
							require.Equal(t, "agent:source-root", d.Identity.ParentAgentID)
							require.NotEmpty(t, d.Identity.AuthorityVersion)
						}
					}
					if d.Core != nil && app > 0 {
						require.Equal(t, jsonNumber(app), d.Core.WorkloadApplicationID)
					}
				}
				require.NoError(t, rows.Err())
				rows.Close()
				require.Equal(t, 7, count)
				if observations {
					attempts := 1
					if strings.HasSuffix(operation, "_fallback") {
						attempts = 2
					}
					sourceHTTPAssertObservations(t, pool, request, operation, attempts)
					if attempts == 2 {
						require.EqualValues(t, 2, calls.Load())
						var actualAttempts, terminal int
						require.NoError(t, pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM billing_provider_attempts WHERE request_id=$1), (SELECT count(*) FROM completed_execution_feed_items WHERE request_id=$1)`, request).Scan(&actualAttempts, &terminal))
						require.Equal(t, 2, actualAttempts)
						require.Equal(t, 1, terminal)
						return
					}
				}
				replay := invoke(body, raw, "source-same-business-op")
				require.Equal(t, http.StatusConflict, replay.Code, "original_operation=%q operation=%q response=%s", replay.Header().Get(middleware.OriginalOperationHeader), replay.Header().Get(middleware.OperationIDHeader), replay.Body.String())
				require.Equal(t, request, replay.Header().Get(middleware.OriginalOperationHeader))
				conflict := invoke(strings.Replace(body, "source-private-prompt", "different-private-prompt", 1), raw, "source-same-business-op")
				require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
				denied := invoke(strings.Replace(body, "company-chat", "company-other", 1), raw, "")
				require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
				unauthenticated := invoke(body, "invalid-synthetic", "")
				require.Equal(t, http.StatusUnauthorized, unauthenticated.Code)
				require.EqualValues(t, 1, calls.Load())
				var freezes, economics, noScope int
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM freeze_idempotency),(SELECT count(*) FROM completed_execution_feed_items),(SELECT count(*) FROM execution_source_facts WHERE request_id=$1)`, unauthenticated.Header().Get(middleware.OperationIDHeader)).Scan(&freezes, &economics, &noScope))
				require.Equal(t, 1, freezes)
				require.Equal(t, 1, economics)
				require.Zero(t, noScope)
				b.WithMode(billing.BillingModePrepaidEnforced)
				_, err = pool.Exec(t.Context(), `UPDATE wallets SET balance_micros=0 WHERE org_id=$1`, org)
				require.NoError(t, err)
				admissionDenied := invoke(body, raw, "")
				require.Equal(t, http.StatusPaymentRequired, admissionDenied.Code, admissionDenied.Body.String())
				deniedRequest := admissionDenied.Header().Get(middleware.OperationIDHeader)
				var deniedFacts, deniedFreezes int
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND profile=ANY($2)),(SELECT count(*) FROM freeze_idempotency WHERE request_id=$1)`, deniedRequest, sourcefacts.Profiles()).Scan(&deniedFacts, &deniedFreezes))
				require.Equal(t, 2, deniedFacts, "authenticated identity exists without an admitted execution")
				require.Zero(t, deniedFreezes)
				require.EqualValues(t, 1, calls.Load())
				if observations {
					var rejectedWire []byte
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='admission_rejected'`, deniedRequest, sourcefacts.RuntimeProfile).Scan(&rejectedWire))
					rejected, err := sourcefacts.DecodeObservation(rejectedWire)
					require.NoError(t, err)
					require.Equal(t, "rejected", rejected.Runtime.Admission)
					require.Equal(t, "not_started", rejected.Runtime.Provider)
					require.Equal(t, "not_created", rejected.Runtime.Financial)
					if identity == "legacy_shared" {
						_, err = pool.Exec(t.Context(), `CREATE FUNCTION source_test_refuse_rejection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.stage='admission_rejected' THEN RAISE EXCEPTION 'synthetic rejection source unavailable'; END IF; RETURN NEW; END $$;
						CREATE TRIGGER source_test_refuse_rejection BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION source_test_refuse_rejection()`)
						require.NoError(t, err)
						failedRejection := invoke(body, raw, "")
						require.Equal(t, http.StatusServiceUnavailable, failedRejection.Code, failedRejection.Body.String())
						require.Contains(t, failedRejection.Body.String(), "source_facts.capture_unavailable")
						var admitted int
						require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM freeze_idempotency WHERE request_id=$1`, failedRejection.Header().Get(middleware.OperationIDHeader)).Scan(&admitted))
						require.Zero(t, admitted)
						require.EqualValues(t, 1, calls.Load())
						_, err = pool.Exec(t.Context(), `DROP TRIGGER source_test_refuse_rejection ON execution_source_facts; DROP FUNCTION source_test_refuse_rejection()`)
						require.NoError(t, err)
					}
				}
				if identity == "grant_application" {
					var before, after []byte
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='terminal'`, request, sourcefacts.IdentityProfile).Scan(&before))
					_, err = pool.Exec(t.Context(), `UPDATE users SET status='disabled' WHERE id=$1`, owner)
					require.NoError(t, err)
					revokedSponsor := invoke(body, raw, "")
					require.Equal(t, http.StatusUnauthorized, revokedSponsor.Code, revokedSponsor.Body.String())
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='terminal'`, request, sourcefacts.IdentityProfile).Scan(&after))
					require.Equal(t, before, after)
					require.EqualValues(t, 1, calls.Load())
				}
				if identity == "agent" {
					var historical []byte
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='terminal'`, request, sourcefacts.IdentityProfile).Scan(&historical))
					// Expire only this test's newly generated workflow state; no
					// wildcard cleanup or shared Redis namespace reset is used.
					workflowKey := "ur:agentcap:v2:{" + workflow + "}:workflow"
					require.NoError(t, rdb.PExpire(t.Context(), workflowKey, time.Millisecond).Err())
					require.Eventually(t, func() bool { return rdb.Exists(t.Context(), workflowKey).Val() == 0 }, time.Second, time.Millisecond)
					expired := invoke(body, raw, "")
					require.Equal(t, http.StatusUnauthorized, expired.Code, expired.Body.String())
					var retained []byte
					require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='terminal'`, request, sourcefacts.IdentityProfile).Scan(&retained))
					require.Equal(t, historical, retained)
					require.EqualValues(t, 1, calls.Load())
				}
			})
		}
	}
}

type sourceHTTPStreamCancelWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
	once   sync.Once
}

type sourceHTTPStreamFailureWriter struct {
	*httptest.ResponseRecorder
	flush bool
}

func (w *sourceHTTPStreamFailureWriter) Write(p []byte) (int, error) {
	if !w.flush {
		return 0, errors.New("synthetic first write failure")
	}
	return w.ResponseRecorder.Write(p)
}
func (w *sourceHTTPStreamFailureWriter) FlushError() error {
	return errors.New("synthetic first flush failure")
}

func (w *sourceHTTPStreamCancelWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	w.once.Do(w.cancel)
	return n, err
}

func sourceHTTPWorkloadGrant(t *testing.T, pool *pgxpool.Pool, org, project, sponsor int64, raw string) (app, key int64) {
	return sourceHTTPWorkloadGrantWithScope(t, pool, org, project, sponsor, raw, prd2E2EJWTSecret, "/v1/chat/completions,/v1/embeddings,/v1/models")
}

func sourceHTTPWorkloadGrantWithScope(t *testing.T, pool *pgxpool.Pool, org, project, sponsor int64, raw, hashSecret, endpoints string) (app, key int64) {
	t.Helper()
	mode, modelsJSON, endpointsJSON := "project_default", "[]", "[]"
	if endpoints != "/v1/chat/completions,/v1/embeddings,/v1/models" {
		mode = "explicit"
		modelsJSON = `["company-chat"]`
		wire, err := json.Marshal(strings.Split(endpoints, ","))
		require.NoError(t, err)
		endpointsJSON = string(wire)
	}
	ctx := t.Context()
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO workload_applications(contract_version,identity_contract_version,org_id,project_id,application_ref,logical_ref,display_name,kind,environment,identity_mode,owner_team,status,environment_locked_at,created_by,updated_by)
	 VALUES('unirouter.workload-application.v2','unirouter.workload-identity.v2',$1,$2,'app_source_grant','workload_source_grant','Synthetic source grant','service','development','service_credential','test','active',NOW(),$3,$3) RETURNING id`, org, project, sponsor).Scan(&app))
	var request, grant, credential int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO access_requests(request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,subject_workload_application_id,request_kind,requested_project_role,requested_scope_mode,requested_models,requested_endpoints,environment,requested_valid_until,submission_mode,sponsor_user_id,status,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,submitted_by,decided_by,decided_at)
	 VALUES('req_source_app_000001','source-app',repeat('a',64),$1,$2,'workload_application',$3,'workload_service','project_user',$5,$6::jsonb,$7::jsonb,'development',NOW()+INTERVAL '1 hour','direct_grant',$4,'approved','direct',ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],'unirouter.enterprise-access-policy.v1',$4,$4,NOW()) RETURNING id`, org, project, app, sponsor, mode, modelsJSON, endpointsJSON).Scan(&request))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO access_grants(grant_ref,access_request_id,org_id,project_id,subject_type,subject_workload_application_id,sponsor_user_id,project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,environment,status,valid_from,valid_until,decision_mode,policy_reason_codes,decision_reason_codes,policy_version,semantic_hash,created_by,updated_by)
	 VALUES('grant_source_app_0001',$1,$2,$3,'workload_application',$4,$5,'project_user',$6,$7::jsonb,$8::jsonb,'development','active',NOW()-INTERVAL '1 hour',(SELECT requested_valid_until FROM access_requests WHERE id=$1),'direct',ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],'unirouter.enterprise-access-policy.v1',repeat('a',64),$5,$5) RETURNING id`, request, org, project, app, sponsor, mode, modelsJSON, endpointsJSON).Scan(&grant))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO access_credentials(credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,allowed_models,allowed_endpoints,policy_labels,rpm_limit,rps_limit,created_by)
	 VALUES('cred_source_app_0001',$1,$2,$3,'synthetic-source','','company-chat',$5,'',60,10,$4) RETURNING id`, grant, org, project, sponsor, endpoints).Scan(&credential))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,allowed_ips,allowed_models,allowed_endpoints,purpose,workload_application_id,access_grant_id,access_credential_id,credential_ref)
	 VALUES($1,$2,'service','synthetic-source',$3,'ur-source-app','active','','company-chat',$7,'',$4,$5,$6,'cred_source_app_0001') RETURNING id`, org, project, auth.HashAPIKey(raw, hashSecret), app, grant, credential, endpoints).Scan(&key))
	return app, key
}

// Shared initialization keeps the existing identity matrix and CC14 on one wiring path.
type sourceHTTPRuntime struct {
	billing        *billing.Billing
	redis          *redis.Client
	source         *sourcefacts.Service
	feed           *completedfeed.Service
	journal        *outbox.Service
	relay          *gateway.Relay
	capabilities   *agentcap.Service
	content        *sourceHTTPContentHarness
	contentStarter contentcapture.Starter
	sourceToken    string
	observe        func()
}

func newSourceHTTPRuntime(t *testing.T, pool *pgxpool.Pool, org, project, key int64, observations, content bool, interop *p3SourceFixture) *sourceHTTPRuntime {
	t.Helper()
	return newSourceHTTPRuntimeWithReceiver(t, pool, org, project, key, observations, content, interop, true)
}

// The diagnostic stream fixture needs the original runtime without issuing an
// unrelated receiver credential. Existing callers retain their receiver setup.
func newSourceHTTPRuntimeWithReceiver(t *testing.T, pool *pgxpool.Pool, org, project, key int64, observations, content bool, interop *p3SourceFixture, receiver bool) *sourceHTTPRuntime {
	t.Helper()
	rawRedisDB, redisDBSet := os.LookupEnv("REDIS_DB")
	redisDB, err := sourceHTTPRedisDB(rawRedisDB, redisDBSet, interop != nil)
	require.NoError(t, err)
	const sourceSecret = "synthetic-source-http-cursor-secret-only"
	source, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ContentEnabled: content, ObservationsEnabled: observations, SourceInstance: "synthetic-http", CursorSecret: sourceSecret, I01Enabled: true})
	require.NoError(t, err)
	var sourceToken string
	observe := func() {}
	if receiver {
		observe = sourceHTTPAuthenticatedPull(t, pool, source, sourceSecret, org, project, func(token string) { sourceToken = token })
	}
	v1, err := completedfeed.NewService(pool, sourceSecret, source.SourceInstance(), true)
	require.NoError(t, err)
	b := billing.NewBilling(pool, nil).WithMode(billing.BillingModeCostObservation)
	b.SetReservationMaterializer(source)
	b.SetTerminalMaterializer(billing.TerminalMaterializers{v1, source})
	journal, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	b.SetOutbox(journal)
	require.NoError(t, journal.RegisterHandler(outbox.EventSourceDispatch, source.ProcessDispatchEvent))
	for _, kind := range []string{outbox.EventBillingUsagePrepared, outbox.EventBillingSettled, outbox.EventBillingCancelled} {
		require.NoError(t, journal.RegisterHandler(kind, b.ProcessUsageJournalEvent))
	}
	require.NotEmpty(t, os.Getenv("REDIS_ADDR"), "select the isolated integration Redis explicitly")
	redisOptions := &redis.Options{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD"), DB: redisDB}
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = rdb.Close() })
	capabilities, err := agentcap.NewService(pool, rdb, prd2E2EJWTSecret)
	require.NoError(t, err)
	limits, err := limitpolicy.NewService(pool, rdb, limitpolicy.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, limits.Refresh(t.Context()))
	if interop != nil {
		interop.startPolicyRefresh(t, limits)
		interop.checkPolicyRefresh(t, limits, limitpolicy.Subject{OrgID: org, ProjectID: project, APIKeyID: key, Model: "company-chat"})
	}
	hooks, err := newRuntimeTerminalHooks(pool, journal, capabilities, limits)
	require.NoError(t, err)
	require.NoError(t, journal.RegisterHandler(outbox.EventRuntimeTerminal, hooks.ProcessEvent))
	require.NoError(t, journal.RegisterHandler(outbox.EventRuntimeTerminalV1, hooks.ProcessEvent))
	audit := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
	require.NoError(t, journal.RegisterHandler(outbox.EventAudit, audit.ProcessAuditOutbox))
	capBilling := capbilling.New(b, capabilities)
	capBilling.SetTerminalHooks(hooks)
	sharedBilling := policybilling.New(capBilling, limits)
	sharedBilling.SetTerminalHooks(hooks)
	relay := gateway.NewRelay(pool, rdb, gateway.NewRouter(pool), sharedBilling, filter.NewDFAFilter(nil), prd2E2EEncryptionKey)
	var contentHarness *sourceHTTPContentHarness
	var contentStarter contentcapture.Starter
	if content {
		contentHarness = newSourceHTTPContentHarness(t, pool, source, org, project, interop)
		contentStarter = contentHarness
		relay.SetContentCapture(contentStarter)
	}
	policy := routingpolicy.Default()
	planner, err := modelrouting.NewPlanner(pool, policy)
	require.NoError(t, err)
	executionConfig, err := modelexecution.ConfigFromRoutingPolicy(policy)
	require.NoError(t, err)
	coordinator, err := modelexecution.New(pool, b, executionConfig)
	require.NoError(t, err)
	if observations {
		b.SetCommittedObservationSink(source)
		coordinator.SetDispatchMaterializer(source)
	}
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeLegacy, planner, coordinator, policy))
	return &sourceHTTPRuntime{billing: b, redis: rdb, source: source, feed: v1, journal: journal, relay: relay, capabilities: capabilities, content: contentHarness, contentStarter: contentStarter, sourceToken: sourceToken, observe: observe}
}
func (runtime *sourceHTTPRuntime) handler(t *testing.T, pool *pgxpool.Pool, final http.Handler) http.Handler {
	source, capabilities, contentStarter := runtime.source, runtime.capabilities, runtime.contentStarter
	guarded := middleware.IdempotencyGuard(middleware.NewPostgresIdempotencyStore(pool), prd2E2EJWTSecret, final)
	retained := payloadpolicy.RequestMiddlewareWithMax(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), 8*payloadpolicy.MiB, middleware.GetOrgIDFromContext)
	handler := middleware.OperationIdentityWithSecret(prd2E2EJWTSecret)(middleware.GatewayAuthAcrossBufferedBody(middleware.GatewayAuthWithAgentCapabilities(pool, prd2E2EJWTSecret, capabilities), retained)(middleware.CaptureExecutionContext(sourceCaptureDiagnostics{Service: source, t: t})(middleware.CaptureContent(contentStarter)(middleware.StrictJSONIngress(guarded)))))
	return handler
}

// Unset preserves the existing fixture defaults. Explicit configuration must
// be valid and may not override the admitted interop database.
func sourceHTTPRedisDB(raw string, configured, interop bool) (int, error) {
	database := 0
	if interop {
		database = 15
	}
	if !configured {
		return database, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return 0, errors.New("REDIS_DB must be a nonnegative integer")
	}
	if interop && parsed != 15 {
		return 0, errors.New("interop requires REDIS_DB=15")
	}
	return parsed, nil
}

func TestSourceHTTPRedisDBOffline(t *testing.T) {
	for _, test := range []struct {
		name, raw           string
		configured, interop bool
		want                int
		invalid             bool
	}{
		{name: "unset_default", want: 0},
		{name: "unset_interop", interop: true, want: 15},
		{name: "explicit_zero", raw: "0", configured: true, want: 0},
		{name: "explicit_fifteen", raw: "15", configured: true, want: 15},
		{name: "explicit_other", raw: "4", configured: true, want: 4},
		{name: "interop_match", raw: "15", configured: true, interop: true, want: 15},
		{name: "interop_conflict", raw: "0", configured: true, interop: true, invalid: true},
		{name: "empty", configured: true, invalid: true},
		{name: "malformed", raw: "db15", configured: true, invalid: true},
		{name: "negative", raw: "-1", configured: true, invalid: true},
		{name: "whitespace", raw: " 15 ", configured: true, invalid: true},
		{name: "overflow", raw: "999999999999999999999999", configured: true, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sourceHTTPRedisDB(test.raw, test.configured, test.interop)
			if test.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}
