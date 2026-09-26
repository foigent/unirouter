//go:build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

type streamPreDataHeader struct {
	Marker, Operation, ContentType string
	Status                         int
}

// Observe the real writer's header boundary without flushing early, injecting
// data, blocking the handler or changing its cancellation/deadline semantics.
type streamPreDataWriter struct {
	http.ResponseWriter
	marker        string
	headers       chan<- streamPreDataHeader
	written       *atomic.Int64
	headerWritten bool
}

func (w *streamPreDataWriter) WriteHeader(status int) {
	w.ResponseWriter.WriteHeader(status)
	if status < 200 || w.headerWritten {
		return
	}
	w.headerWritten = true
	w.headers <- streamPreDataHeader{Marker: w.marker, Operation: w.Header().Get(middleware.OperationIDHeader), ContentType: w.Header().Get("Content-Type"), Status: status}
}
func (w *streamPreDataWriter) Write(p []byte) (int, error) {
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.written.Add(int64(n))
	return n, err
}
func (w *streamPreDataWriter) Flush() { _ = w.FlushError() }
func (w *streamPreDataWriter) FlushError() error {
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *streamPreDataWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestStreamPreDataWriterOffline(t *testing.T) {
	recorder := httptest.NewRecorder()
	headers := make(chan streamPreDataHeader, 1)
	var written atomic.Int64
	writer := &streamPreDataWriter{ResponseWriter: recorder, marker: "synthetic-marker", headers: headers, written: &written}
	writer.Header().Set(middleware.OperationIDHeader, "op_synthetic")
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)
	require.Equal(t, streamPreDataHeader{"synthetic-marker", "op_synthetic", "text/event-stream", 200}, <-headers)
	require.Zero(t, written.Load())
	require.Empty(t, recorder.Body.String())
	require.False(t, recorder.Flushed, "observing headers must not add a transport flush")
	writer.WriteHeader(http.StatusOK)
	require.Empty(t, headers, "only the first final header is reported")
	_, err := io.WriteString(writer, "data: synthetic\n\n")
	require.NoError(t, err)
	require.NoError(t, http.NewResponseController(writer).Flush())
	require.True(t, recorder.Flushed)
	require.Equal(t, int64(recorder.Body.Len()), written.Load())
	require.Same(t, recorder, writer.Unwrap(), "ResponseController retains the original deadline-capable writer")
}

// One real HTTP request per branch, both against synthetic loopback Provider
// and gateway listeners. Existing tests cover after-write cancellation; this
// one holds all Provider body bytes until durable response acceptance is read.
func TestStreamAcceptedBeforeFirstDataPG(t *testing.T) {
	// Require explicit valid configuration before creating resources. The run
	// owner binds the precise admitted host and database in its environment.
	require.NotEmpty(t, os.Getenv("REDIS_ADDR"))
	rawRedisDB, redisDBSet := os.LookupEnv("REDIS_DB")
	require.True(t, redisDBSet, "select the isolated integration Redis DB explicitly")
	redisDB, err := sourceHTTPRedisDB(rawRedisDB, redisDBSet, false)
	require.NoError(t, err)
	for _, action := range []string{"release", "downstream_cancel"} {
		t.Run(action, func(t *testing.T) {
			pool := contentPerformanceRuntimePool(t, testutil.SetupMigratedTestDB(t))
			sourceHTTPIsolateRedisIdentities(t, pool)
			org, owner, project := seedPRD2P0OrgProjectUser(t, pool, "developer")
			testutil.NewTestWalletCents(t, pool, org, 10000)
			testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)
			seedPRD2P0ModelGovernance(t, pool, org, project)
			marker := "synthetic-predata-" + action
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var calls, providerBytes, downstreamBytes atomic.Int64
			providerDone := make(chan struct{})
			var providerDoneOnce sync.Once
			providerErrors := make(chan error, 1)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer providerDoneOnce.Do(func() { close(providerDone) })
				calls.Add(1)
				var body struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) != 1 || body.Messages[0].Content != marker {
					select {
					case providerErrors <- errors.New("synthetic Provider marker mismatch"):
					default:
					}
					http.Error(w, "synthetic marker mismatch", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("X-Request-ID", marker+"-provider")
				w.WriteHeader(http.StatusOK)
				if err := http.NewResponseController(w).Flush(); err != nil {
					select {
					case providerErrors <- err:
					default:
					}
					return
				}
				select {
				case <-r.Context().Done():
					return
				case <-release:
				}
				n, err := io.WriteString(w, "data: {\"id\":\"synthetic-predata\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"synthetic answer\"},\"finish_reason\":null}]}\n\n"+
					"data: {\"id\":\"synthetic-predata\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
					"data: {\"id\":\"synthetic-predata\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\ndata: [DONE]\n\n")
				providerBytes.Add(int64(n))
				if err != nil {
					select {
					case providerErrors <- err:
					default:
					}
				}
			}))
			t.Cleanup(provider.Close)
			// Failure cleanup releases the synthetic gate before native Close joins.
			t.Cleanup(unblock)
			seedPRD2P0Channel(t, pool, provider.URL, "gpt-4o")
			// Reuse the existing public synthetic fixture key; issue no receiver,
			// content-store, provider or external credential for this diagnosis.
			raw := "ur-synthetic-source-" + strings.Repeat("a", 32)
			var key int64
			require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO api_keys(org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,allowed_models,status) VALUES($1,$2,$3,'shared','synthetic-predata',$4,'ur-synthetic','company-chat','active') RETURNING id`, org, project, owner, auth.HashAPIKey(raw, prd2E2EJWTSecret)).Scan(&key))
			runtime := newSourceHTTPRuntimeWithReceiver(t, pool, org, project, key, true, false, nil, false)
			require.Equal(t, redisDB, runtime.redis.Options().DB)
			t.Logf("stream_predata_redis db=%d", runtime.redis.Options().DB)
			handler := runtime.handler(t, pool, newGatewayChatHandler(pool, runtime.relay))
			headers := make(chan streamPreDataHeader, 1)
			handlerDone, downstreamCancelled := make(chan struct{}), make(chan struct{})
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(handlerDone)
				stop := context.AfterFunc(r.Context(), func() { close(downstreamCancelled) })
				defer stop()
				handler.ServeHTTP(&streamPreDataWriter{ResponseWriter: w, marker: r.Header.Get("X-Synthetic-Marker"), headers: headers, written: &downstreamBytes}, r)
			}))
			requestCtx, cancelRequest := context.WithCancel(t.Context())
			t.Cleanup(func() {
				cancelRequest()
				unblock()
				gateway.CloseClientConnections()
				gateway.Close()
			})
			// This bounds test coordination only; runtime/client timeouts stay unchanged.
			watch, cancelWatch := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancelWatch()
			body := fmt.Sprintf(`{"model":"company-chat","messages":[{"role":"user","content":%q}],"stream":true,"stream_options":{"include_usage":true}}`, marker)
			request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+raw)
			request.Header.Set("X-Synthetic-Marker", marker)
			type clientResult struct {
				status int
				body   []byte
				err    error
			}
			clientDone := make(chan clientResult, 1)
			clientExited := make(chan struct{})
			client := gateway.Client()
			t.Cleanup(client.CloseIdleConnections)
			go func() {
				defer close(clientExited)
				response, err := client.Do(request)
				if err != nil {
					clientDone <- clientResult{err: err}
					return
				}
				defer response.Body.Close()
				wire, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
				clientDone <- clientResult{response.StatusCode, wire, err}
			}()
			t.Cleanup(func() { cancelRequest(); <-clientExited })
			var header streamPreDataHeader
			select {
			case header = <-headers:
			case <-watch.Done():
				t.Fatal("gateway did not reach the accepted header boundary")
			}
			require.Equal(t, marker, header.Marker)
			require.Equal(t, http.StatusOK, header.Status)
			require.Equal(t, "text/event-stream", header.ContentType)
			require.NotEmpty(t, header.Operation)
			require.Zero(t, providerBytes.Load())
			require.Zero(t, downstreamBytes.Load(), "no downstream body before the gate decision")
			before := readStreamPreDataFacts(t, watch, pool, org, project, header.Operation)
			require.Len(t, before.Attempts, 1)
			attempt := before.Attempts[0].ID
			logStreamPreDataFacts(t, "accepted_before_data", marker, header.Operation, attempt, before)
			require.True(t, before.Freeze.ScopeMatches)
			require.Equal(t, "frozen", before.Freeze.Status)
			require.Equal(t, "response_accepted", before.Attempts[0].State)
			require.Equal(t, marker+"-provider", before.Attempts[0].UpstreamRequestID)
			require.True(t, before.Attempts[0].ScopeMatches)
			require.Empty(t, before.Deliveries)
			require.Empty(t, before.Leases)
			require.Empty(t, before.Checkpoints)
			if action == "release" {
				unblock()
			} else {
				cancelRequest()
				select {
				case <-downstreamCancelled:
				case <-watch.Done():
					t.Fatal("real gateway request did not observe downstream cancellation")
				}
			}
			select {
			case <-handlerDone:
			case <-watch.Done():
				t.Fatal("gateway handler did not exit")
			}
			select {
			case <-providerDone:
			case <-watch.Done():
				t.Fatal("synthetic Provider did not exit")
			}
			var result clientResult
			select {
			case result = <-clientDone:
			case <-watch.Done():
				t.Fatal("real HTTP client did not exit")
			}
			require.Empty(t, providerErrors)
			require.EqualValues(t, 1, calls.Load(), "no retry or second attempt")
			// Drain the existing journal through its native handler, without sleeps,
			// retries or rewriting any finance/hold state to make the test pass.
			for {
				n, err := runtime.journal.DrainOnce(watch)
				require.NoError(t, err)
				if n == 0 {
					break
				}
			}
			after := readStreamPreDataFacts(t, watch, pool, org, project, header.Operation)
			logStreamPreDataFacts(t, "handler_and_journal_finished", marker, header.Operation, attempt, after)
			require.Len(t, after.Attempts, 1)
			require.Equal(t, attempt, after.Attempts[0].ID)
			require.Equal(t, marker+"-provider", after.Attempts[0].UpstreamRequestID)
			require.True(t, after.Attempts[0].ScopeMatches && after.Freeze.ScopeMatches)
			require.Equal(t, "cost_observation", after.Freeze.BillingMode)
			require.Zero(t, after.Freeze.Charged)
			require.Zero(t, after.WalletTransactions)
			require.Zero(t, after.OutboxRemaining, "native journal drain completed")
			if action == "release" {
				require.NoError(t, result.err)
				require.Equal(t, http.StatusOK, result.status)
				require.Contains(t, string(result.body), "synthetic answer")
				require.Contains(t, string(result.body), "data: [DONE]")
				require.Equal(t, "settled", after.Freeze.Status)
				require.Equal(t, "settled", after.Attempts[0].State)
				require.True(t, after.Attempts[0].FinancialResolved)
				require.True(t, after.Freeze.OutputStarted && after.Freeze.UsageReady)
				require.Equal(t, "provider_reported", after.Freeze.Authority)
				require.Equal(t, "customer_charge", after.Freeze.Purpose)
				require.Equal(t, "final", after.Freeze.Finality)
				require.Equal(t, 10, after.Freeze.Prompt)
				require.Equal(t, 5, after.Freeze.Completion)
				require.Positive(t, after.Freeze.Calculated)
				require.NotEmpty(t, after.Deliveries)
				last := after.Deliveries[len(after.Deliveries)-1]
				require.Equal(t, "completed", last.Outcome)
				require.Positive(t, last.Bytes)
				require.Positive(t, last.Chunks)
				for _, d := range after.Deliveries {
					require.Equal(t, attempt, d.Attempt)
				}
				require.Len(t, after.Leases, 1)
				require.Equal(t, "completed", after.Leases[0].State)
				require.Equal(t, attempt, after.Leases[0].Attempt)
				require.NotEmpty(t, after.Checkpoints)
				for _, c := range after.Checkpoints {
					require.Equal(t, attempt, c.Attempt)
					require.Positive(t, c.Bytes)
					require.Positive(t, c.Chunks)
				}
				require.Len(t, after.Economic, 1)
				require.NotNil(t, after.Freeze.ObservationReceiptID)
				require.Equal(t, *after.Freeze.ObservationReceiptID, after.Economic[0].ID)
				require.Equal(t, after.Freeze.Authority, after.Economic[0].Authority)
				require.Equal(t, after.Freeze.Purpose, after.Economic[0].Purpose)
				require.Equal(t, after.Freeze.Finality, after.Economic[0].Finality)
				require.Equal(t, "settled", after.Economic[0].Status)
				require.Equal(t, after.Freeze.Calculated, after.Economic[0].Calculated)
				require.Zero(t, after.Economic[0].Charged)
				require.Equal(t, "settled", after.FeedStatus)
				require.True(t, after.FeedScopeMatches)
				require.Contains(t, after.JournalEvents, "settled")
			} else {
				require.ErrorIs(t, result.err, context.Canceled)
				require.Zero(t, providerBytes.Load(), "the cancelled branch never released a Provider data frame")
				require.Equal(t, "settlement_pending", after.Freeze.Status)
				require.Equal(t, "provider_attempt_unresolved", after.Freeze.Reason)
				require.True(t, after.Freeze.ProviderCostPending)
				require.Equal(t, "response_accepted", after.Attempts[0].State)
				require.Equal(t, "pending", after.Attempts[0].Certainty)
				require.False(t, after.Attempts[0].FinancialResolved)
				require.False(t, after.Freeze.OutputStarted || after.Freeze.UsageReady)
				require.Equal(t, before.Freeze.Authority, after.Freeze.Authority)
				require.Equal(t, before.Freeze.Finality, after.Freeze.Finality)
				require.Equal(t, before.Freeze.Calculated, after.Freeze.Calculated)
				require.Empty(t, after.Deliveries)
				require.Empty(t, after.Leases)
				require.Empty(t, after.Checkpoints)
				require.Empty(t, after.Economic)
				require.Nil(t, after.Freeze.ObservationReceiptID)
				require.Equal(t, "absent", after.FeedStatus)
				require.NotContains(t, after.JournalEvents, "settled")
				require.NotContains(t, after.JournalEvents, "cancelled")
			}
		})
	}
}

type streamPreDataFacts struct {
	Freeze *struct {
		Status, Reason, BillingMode, Authority, Purpose, Finality    string
		ScopeMatches, OutputStarted, UsageReady, ProviderCostPending bool
		Prompt, Completion                                           int
		Calculated, Charged                                          int64
		ObservationReceiptID                                         *int64
	}
	Attempts []struct {
		ID, State, Certainty, UpstreamRequestID string
		ScopeMatches, FinancialResolved         bool
		Calculated, Charged                     *int64
	}
	Deliveries []struct {
		Sequence                int
		Attempt, Outcome, Error string
		Bytes                   int64
		Chunks                  int
	}
	Leases []struct {
		Attempt, State, Reason string
		Generation             int64
	}
	Checkpoints []struct {
		Attempt                              string
		Sequence, Prompt, Completion, Chunks int
		Bytes, Generation                    int64
	}
	Economic []struct {
		ID                                   int64
		Status, Authority, Purpose, Finality string
		Calculated, Charged                  int64
	}
	FeedStatus                          string
	FeedScopeMatches                    bool
	JournalEvents                       []string
	WalletTransactions, OutboxRemaining int
}

// Whitelisted metadata in one statement snapshot. No source_context, payload,
// request/response body, DSN, credential, raw provider error or lease owner.
func readStreamPreDataFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, org, project int64, operation string) streamPreDataFacts {
	t.Helper()
	var wire []byte
	err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
 'Freeze',(SELECT jsonb_build_object('Status',f.status,'Reason',f.settlement_pending_reason,
 'BillingMode',f.billing_mode,'ScopeMatches',f.project_id=$2,
 'OutputStarted',f.output_started_at IS NOT NULL,'UsageReady',f.settlement_payload_ready_at IS NOT NULL,
 'ProviderCostPending',f.provider_cost_pending_at IS NOT NULL,
 'Prompt',f.prompt_tokens,'Completion',f.completion_tokens,'Authority',f.usage_authority,
 'Purpose',f.usage_purpose,'Finality',f.usage_finality,'Calculated',f.calculated_cost_micros,
 'Charged',f.charged_cost_micros,'ObservationReceiptID',f.terminal_observation_receipt_id)
 FROM freeze_idempotency f WHERE f.org_id=$1 AND f.request_id=$3),
 'Attempts',COALESCE((SELECT jsonb_agg(jsonb_build_object('ID',a.attempt_id,'State',a.state,
 'Certainty',a.outcome_certainty,'UpstreamRequestID',a.upstream_request_id,
 'ScopeMatches',a.project_id=$2,'FinancialResolved',a.financial_resolved_at IS NOT NULL,
 'Calculated',a.calculated_cost_micros,'Charged',a.charged_cost_micros) ORDER BY a.attempt_id)
 FROM billing_provider_attempts a WHERE a.org_id=$1 AND a.request_id=$3),'[]'::jsonb),
 'Deliveries',COALESCE((SELECT jsonb_agg(jsonb_build_object('Sequence',d.sequence,'Attempt',d.attempt_id,
 'Outcome',d.outcome,'Error',d.error_code,'Bytes',d.delivered_bytes,'Chunks',d.delivered_chunks) ORDER BY d.sequence)
 FROM request_delivery_receipts d WHERE d.org_id=$1 AND d.request_id=$3),'[]'::jsonb),
 'Leases',COALESCE((SELECT jsonb_agg(jsonb_build_object('Attempt',l.attempt_id,'State',l.state,
 'Reason',l.terminal_reason,'Generation',l.generation))
 FROM stream_delivery_leases l WHERE l.org_id=$1 AND l.request_id=$3),'[]'::jsonb),
 'Checkpoints',COALESCE((SELECT jsonb_agg(jsonb_build_object('Attempt',c.attempt_id,'Sequence',c.sequence,
 'Bytes',c.delivered_bytes,'Chunks',c.delivered_chunks,'Prompt',c.prompt_tokens,
 'Completion',c.completion_tokens,'Generation',c.lease_generation) ORDER BY c.sequence)
 FROM stream_delivery_checkpoints c WHERE c.org_id=$1 AND c.request_id=$3),'[]'::jsonb),
 'Economic',COALESCE((SELECT jsonb_agg(jsonb_build_object('ID',e.id,'Status',e.terminal_status,
 'Authority',e.usage_authority,'Purpose',e.usage_purpose,'Finality',e.usage_finality,
 'Calculated',e.calculated_cost_micros,'Charged',e.charged_cost_micros))
 FROM billing_observation_receipts e WHERE e.org_id=$1 AND e.request_id=$3),'[]'::jsonb),
 'FeedStatus',COALESCE((SELECT terminal_status FROM completed_execution_feed_items WHERE org_id=$1 AND request_id=$3),'absent'),
 'FeedScopeMatches',COALESCE((SELECT project_id=$2 FROM completed_execution_feed_items WHERE org_id=$1 AND request_id=$3),false),
 'JournalEvents',COALESCE((SELECT jsonb_agg(event_type ORDER BY event_type) FROM usage_finalization_journal WHERE org_id=$1 AND request_id=$3),'[]'::jsonb),
 'WalletTransactions',(SELECT count(*) FROM transactions WHERE org_id=$1 AND request_id=$3),
 'OutboxRemaining',(SELECT count(*) FROM internal_outbox_events WHERE org_id=$1 AND request_id=$3))`, org, project, operation).Scan(&wire)
	require.NoError(t, err)
	var result streamPreDataFacts
	require.NoError(t, json.Unmarshal(wire, &result))
	require.NotNil(t, result.Freeze, "original reservation must exist")
	return result
}

func logStreamPreDataFacts(t *testing.T, phase, marker, operation, attempt string, facts streamPreDataFacts) {
	t.Helper()
	wire, err := json.Marshal(struct {
		Phase, Marker, Operation, Attempt string
		Facts                             streamPreDataFacts
	}{phase, marker, operation, attempt, facts})
	require.NoError(t, err)
	t.Logf("stream_predata_evidence=%s", wire)
}
