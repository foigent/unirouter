//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

const cc14RunID = "cc14-openclaw-20260924-r9"
const cc14Actions = "synthetic-db-redis,http,credentials,guardrail-shadow-policy,content-binding,full-content-policy,human-review-grant,secret-cleanup,openclaw-read-two-rounds"

// Keep the synthetic ID stable under the client's strict alphanumeric replay normalization.
const cc14CallID = "callcc14readsample"
const cc14URL = "http://127.0.0.1:19185"
const cc14Upstream = "127.0.0.1:19186"
const cc14MaxRequest = 65536

type cc14Scenario struct {
	runID, actions, url, upstream, binaryKey string
	rounds                                   int
	fileActions                              bool
}

func cc14SelectedScenario(get func(string) string) cc14Scenario {
	if get("UR_CC15_INTEROP") == "1" {
		return cc14Scenario{cc15RunID, cc15Actions, cc15URL, cc15Upstream, "CC15_APPROVED_BINARY_SHA256", 5, true}
	}
	return cc14Scenario{cc14RunID, cc14Actions, cc14URL, cc14Upstream, "CC14_APPROVED_BINARY_SHA256", 2, false}
}

// Pure admission: no connections, filesystem access or secret-bearing errors.
// These values record approval; setting them never creates approval.
func cc14ApprovalError(get func(string) string) error {
	scenario := cc14SelectedScenario(get)
	if get("UR_CC15_INTEROP") != "" && get("UR_CC15_INTEROP") != "1" {
		return errors.New("CC14/15 exclusive mode guard")
	}
	if scenario.fileActions && get("UR_CC14_INTEROP") != "" {
		return errors.New("CC14/15 exclusive mode guard")
	}
	if !scenario.fileActions && get("UR_CC15_INTEROP") != "" {
		return errors.New("CC14/15 exclusive mode guard")
	}
	if scenario.fileActions && get("CC14_APPROVED_BINARY_SHA256") != "" {
		return errors.New("CC14/15 exclusive binary approval guard")
	}
	exact := map[string]string{
		"UR_CC14_INTEROP": map[bool]string{true: "", false: "1"}[scenario.fileActions], "UR_J1_INTEROP": "", "UR_P3_INTEROP": "",
		"CC14_APPROVED_RUN": map[bool]string{true: "", false: cc14RunID}[scenario.fileActions], "CC14_APPROVED_ACTIONS": map[bool]string{true: "", false: cc14Actions}[scenario.fileActions],
		"CC15_APPROVED_RUN": map[bool]string{true: cc15RunID, false: ""}[scenario.fileActions], "CC15_APPROVED_ACTIONS": map[bool]string{true: cc15Actions, false: ""}[scenario.fileActions],
		"P3_CREDENTIAL_DIR":     filepath.Join(p3CredentialParent, scenario.runID),
		"UR_P3_OUTPUT_DIR":      filepath.Join(p3EvidenceParent, scenario.runID, "ur"),
		"REQUIRE_TEST_DATABASE": "1", "REDIS_ADDR": "127.0.0.1:56379", "REDIS_DB": "15",
	}
	for key, want := range exact {
		if get(key) != want {
			return errors.New("CC14 exact approval/path/mode guard")
		}
	}
	digest, err := hex.DecodeString(get(scenario.binaryKey))
	if err != nil || len(digest) != 32 || get(scenario.binaryKey) != strings.ToLower(get(scenario.binaryKey)) {
		return errors.New("CC14 exact binary identity required")
	}
	cfg, err := pgxpool.ParseConfig(get("TEST_DATABASE_URL"))
	if err != nil {
		return errors.New("CC14 database configuration")
	}
	c := cfg.ConnConfig
	base := get("UR_P3_BASE_DATABASE")
	if c.Host != "127.0.0.1" || c.Port != 56132 || len(c.Fallbacks) != 0 || base == "" || base == "postgres" || strings.HasPrefix(base, "template") || c.Database != base {
		return errors.New("CC14 exact base database required")
	}
	for _, key := range []string{"UR_P3_DATABASE_OID", "UR_P3_SYSTEM_ID"} {
		if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(get(key)) {
			return errors.New("CC14 database identity required")
		}
	}
	return nil
}
func cc14RequireApproval(t *testing.T) {
	t.Helper()
	p3Require(t, cc14ApprovalError(os.Getenv) == nil, "CC14 explicit exact runtime approval")
}

type cc14Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name       string `json:"name"`
		Parameters struct {
			Type       string `json:"type"`
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
			Required []string `json:"required"`
		} `json:"parameters"`
	} `json:"function"`
}
type cc14Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}
type cc14Request struct {
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
	Model     string        `json:"model"`
	Stream    bool          `json:"stream"`
	MaxTokens int           `json:"max_tokens"`
	Tools     []cc14Tool    `json:"tools"`
	Messages  []cc14Message `json:"messages"`
}
type cc14Protocol struct {
	round     int
	pathField string
}

// The only generated call is chosen from the request's actual read schema.
// No filesystem read, client invocation, or fabricated second request occurs here.
func (p *cc14Protocol) next(raw []byte) (map[string]any, string, error) {
	fail := func(code string) (map[string]any, string, error) { return nil, "", errors.New(code) }
	p.round++
	if p.round > 2 {
		return fail("extra_model_request")
	}
	if len(raw) > cc14MaxRequest {
		return fail("request_too_large")
	}
	var req cc14Request
	if json.Unmarshal(raw, &req) != nil || req.Model != "gpt-4o" || !req.Stream || !req.StreamOptions.IncludeUsage || req.MaxTokens != 1024 || len(req.Messages) == 0 {
		return fail("synthetic_request_contract")
	}
	var read *cc14Tool
	for i := range req.Tools {
		if req.Tools[i].Function.Name == "read" {
			if read != nil {
				return fail("duplicate_read_schema")
			}
			read = &req.Tools[i]
		}
	}
	if read == nil || read.Type != "function" || read.Function.Parameters.Type != "object" {
		return fail("actual_read_schema_required")
	}
	schema := read.Function.Parameters
	path := ""
	for _, name := range []string{"path", "file_path"} {
		if schema.Properties[name].Type == "string" {
			path = name
			break
		}
	}
	if path == "" {
		return fail("unsupported_read_path_schema")
	}
	for _, name := range schema.Required {
		if schema.Properties[name].Type != "string" || name != path {
			return fail("unsupported_required_read_argument")
		}
	}
	if p.round == 1 {
		for _, m := range req.Messages {
			if m.Role == "tool" || len(m.ToolCalls) != 0 || bytes.Contains(m.Content, []byte("cc14-r1-cobalt-7319")) {
				return fail("unexpected_initial_tool_history")
			}
		}
		p.pathField = path
		args, _ := json.Marshal(map[string]string{path: "sample.txt"})
		return map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": cc14CallID, "type": "function", "function": map[string]any{"name": "read", "arguments": string(args)}}}}, "tool_calls", nil
	}
	if path != p.pathField {
		return fail("read_schema_changed")
	}
	calls, results := 0, 0
	var result string
	for _, m := range req.Messages {
		for _, call := range m.ToolCalls {
			calls++
			var args map[string]string
			if m.Role != "assistant" || call.ID != cc14CallID || call.Type != "function" || call.Function.Name != "read" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || len(args) != 1 || args[path] != "sample.txt" {
				return fail("read_call_history_mismatch")
			}
		}
		if m.Role == "tool" {
			results++
			if calls != 1 || m.ToolCallID != cc14CallID {
				return fail("read_result_identity_mismatch")
			}
			if json.Unmarshal(m.Content, &result) != nil {
				var blocks []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if json.Unmarshal(m.Content, &blocks) != nil {
					return fail("unsupported_tool_result")
				}
				for _, b := range blocks {
					if b.Type != "text" {
						return fail("unsupported_tool_result")
					}
					result += b.Text + "\n"
				}
			}
		}
	}
	if calls != 1 || results != 1 {
		return fail("exactly_one_read_roundtrip_required")
	}
	match := regexp.MustCompile(`(?m)marker:\s*(cc14-r1-cobalt-7319)\s*\nalpha:\s*(7)\s*\nbeta:\s*(11)(?:\s|$)`).FindStringSubmatch(result)
	if len(match) != 4 {
		return fail("actual_sample_result_required")
	}
	return map[string]any{"role": "assistant", "content": fmt.Sprintf("marker: %s; alpha: %s; beta: %s", match[1], match[2], match[3])}, "stop", nil
}

type cc14UpstreamState struct {
	mu           sync.Mutex
	protocol     cc14Protocol
	fileProtocol *cc15Protocol
	responseIDs  []string
	calls        atomic.Int64
	failure      string
}

func (s *cc14UpstreamState) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls.Add(1)
	raw, err := io.ReadAll(io.LimitReader(r.Body, cc14MaxRequest+1))
	var delta map[string]any
	var finish string
	if err == nil && r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions" {
		if s.fileProtocol != nil {
			delta, finish, err = s.fileProtocol.next(raw)
		} else {
			delta, finish, err = s.protocol.next(raw)
		}
	} else {
		err = errors.New("upstream_transport_contract")
	}
	if err != nil {
		if s.failure == "" {
			s.failure = err.Error()
		}
		// 400 is intentionally non-retryable; no third successful model response.
		http.Error(w, "CC14 synthetic request rejected", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	responseID := fmt.Sprintf("cc14-%d", s.protocol.round)
	if s.fileProtocol != nil {
		responseID = fmt.Sprintf("cc15-%s-%d", cc15RunID, s.fileProtocol.round)
	}
	s.responseIDs = append(s.responseIDs, responseID)
	emit := func(choices []any, usage any) {
		payload := map[string]any{"id": responseID, "object": "chat.completion.chunk", "model": "gpt-4o", "choices": choices}
		if usage != nil {
			payload["usage"] = usage
		}
		b, _ := json.Marshal(payload)
		if _, e := fmt.Fprintf(w, "data: %s\n\n", b); e != nil {
			s.failure = "upstream_write_failed"
		}
	}
	emit([]any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}, nil)
	emit([]any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}, nil)
	emit([]any{}, map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15})
	if _, e := io.WriteString(w, "data: [DONE]\n\n"); e != nil {
		s.failure = "upstream_write_failed"
	}
	// Returning closes the finite HTTP body: DONE is followed by actual EOF.
}
func (s *cc14UpstreamState) failed() string { s.mu.Lock(); defer s.mu.Unlock(); return s.failure }
func (s *cc14UpstreamState) responseID(index int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.responseIDs) {
		return ""
	}
	return s.responseIDs[index]
}

type cc14RoundReceipt struct {
	Round               int       `json:"round"`
	RequestID           string    `json:"request_id"`
	ProviderResponseID  string    `json:"provider_response_id,omitempty"`
	RequestSHA256       string    `json:"request_sha256"`
	Status              int       `json:"status"`
	ProviderCalls       int64     `json:"provider_calls"`
	PolicySnapshotStale bool      `json:"policy_snapshot_stale"`
	StartedAt           time.Time `json:"started_at"`
	EndedAt             time.Time `json:"ended_at"`
}

func cc14WriteJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	ce := f.Close()
	if err != nil {
		return err
	}
	return ce
}
func cc14BootstrapMatches(raw []byte, want p3SourceReady) bool {
	var got p3SourceReady
	return json.Unmarshal(raw, &got) == nil && got.URL == want.URL && got.SourceInstance == want.SourceInstance && got.OrgID == want.OrgID && got.ProjectID == want.ProjectID && got.CredentialDir == want.CredentialDir && got.Content != nil && want.Content != nil && *got.Content == *want.Content
}
func cc14Server(t *testing.T, address string, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", address)
	p3Require(t, err == nil, "CC14 exact loopback listener unavailable")
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close) // ownership exists before any later assertion.
	return server
}

// Use existing synthetic test material for evidence fingerprints. NewService
// receives the bytes directly, as in startGuardrailRuntime; no credential is created.
func cc14NewGuardrailService(pool *pgxpool.Pool) (*guardrail.Service, error) {
	return guardrail.NewService(pool, filter.NewDFAFilter(nil), prd2E2EEncryptionKey, guardrail.DefaultOptions())
}

// CC14 owns the production refresh loop. Cancellation alone is insufficient:
// stop must join before the fixture pool and synthetic database are cleaned up.
func cc14StartGuardrailRefresh(parent context.Context, run func(context.Context)) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() { defer close(done); run(ctx) }()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done }) }
}

func TestCC14OpenClawContentProducerIntegration(t *testing.T) {
	if os.Getenv("UR_CC14_INTEROP") != "1" && os.Getenv("UR_CC15_INTEROP") != "1" {
		t.Skip("explicit CC14 runtime approval required")
	}
	cc14RequireApproval(t)
	scenario := cc14SelectedScenario(os.Getenv)
	executable, err := os.Executable()
	p3Require(t, err == nil, "CC14 executable identity")
	binary, err := os.ReadFile(executable)
	p3Require(t, err == nil && fmt.Sprintf("%x", sha256.Sum256(binary)) == os.Getenv(scenario.binaryKey), "CC14 approved binary changed")
	f := newApprovedSourceFixture(t, true) // verifies exact live base OID/system before writes.
	pool := testutil.SetupMigratedTestDB(t)
	cc14RecordDatabases(t, f, pool, scenario.runID)
	sourceHTTPIsolateRedisIdentities(t, pool)
	org, owner, project := seedPRD2P0OrgProjectUser(t, pool, auth.RoleDeveloper)
	testutil.NewTestWalletCents(t, pool, org, 10000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)
	seedPRD2P0ModelGovernance(t, pool, org, project)
	upstream := &cc14UpstreamState{}
	if scenario.fileActions {
		upstream.fileProtocol = &cc15Protocol{}
	}
	upstreamServer := cc14Server(t, scenario.upstream, http.HandlerFunc(upstream.serve))
	seedPRD2P0Channel(t, pool, upstreamServer.URL, "gpt-4o")
	control := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
	body := fmt.Sprintf(`{"org_id":%d,"project_id":%d,"name":"cc14-openclaw","allowed_models":"company-chat"}`, org, project)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+mustJWT(t, pool, owner, org, auth.RoleDeveloper))
	response := httptest.NewRecorder()
	middleware.AdminAuth(pool, prd2E2EJWTSecret)(http.HandlerFunc(control.CreateAPIKey)).ServeHTTP(response, req)
	// Never pass this response or decoded key to an assertion/log/evidence writer.
	p3Require(t, response.Code == http.StatusCreated, "CC14 gateway key creation")
	var issued struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	p3Require(t, json.Unmarshal(response.Body.Bytes(), &issued) == nil && issued.ID > 0 && issued.Key != "", "CC14 gateway key response")
	f.secret(t, "ur-gateway.key", issued.Key)
	runtime := newSourceHTTPRuntime(t, pool, org, project, issued.ID, true, true, f)
	// One real, local-only shadow policy in this run's synthetic org/project.
	// Its receipt is produced by Evaluate and the existing CapturePolicies path.
	var guardrailPolicyID int64
	err = pool.QueryRow(f.ctx, `INSERT INTO guardrail_policies
		(org_id,project_id,name,stage,detector_ref,action,mode,failure_mode,config,created_by,updated_by)
		VALUES($1,$2,$3,'input','builtin:pii_secrets','audit','shadow','degraded_allow','{}',$4,$4)
		RETURNING id`, org, project, scenario.runID+"-input-audit", owner).Scan(&guardrailPolicyID)
	p3Require(t, err == nil && guardrailPolicyID > 0, "CC14 scoped guardrail policy seed")
	guardrailService, err := cc14NewGuardrailService(pool)
	if err != nil {
		// NewService reports only length/options validation, never parameter values.
		t.Fatalf("CC14 guardrail service: %v", err)
	}
	refreshCtx, cancelRefresh := context.WithTimeout(f.ctx, 10*time.Second)
	err = guardrailService.Refresh(refreshCtx)
	cancelRefresh()
	p3Require(t, err == nil, "CC14 initial guardrail refresh")
	runtime.relay.SetGuardrail(guardrailService)
	stopGuardrailRefresh := cc14StartGuardrailRefresh(f.ctx, guardrailService.RunRefreshLoop)
	t.Cleanup(stopGuardrailRefresh) // Covers every later setup failure, before pool cleanup.
	mux := f.prepareSourceEndpoints(t, pool, runtime.source, runtime.feed, "synthetic-source-http-cursor-secret-only", runtime.sourceToken, org, project, runtime.content)
	ready := p3SourceReady{URL: scenario.url, SourceInstance: runtime.source.SourceInstance(), OrgID: fmt.Sprint(org), ProjectID: fmt.Sprint(project), CredentialDir: f.credentials, Content: f.content}
	public := runtime.handler(t, pool, newGatewayChatHandler(pool, runtime.relay))
	var mu sync.Mutex
	rounds := []cc14RoundReceipt{}
	failure := ""
	reject := func(code string) {
		if failure == "" {
			failure = code
		}
	}
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock() // one real request at a time; no replay or assembly.
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(f.output), "bootstrap.json"))
		if err != nil || !cc14BootstrapMatches(raw, ready) {
			http.Error(w, "CC14 bootstrap required", http.StatusPreconditionFailed)
			return
		}
		if len(rounds) >= scenario.rounds {
			reject("extra_client_request")
			if err := cc14WriteJSON(filepath.Join(f.output, "extra-request.json"), map[string]any{"run": scenario.runID, "status": 400, "successful": false, "at": time.Now().UTC()}); err != nil {
				reject("extra_request_receipt_failed")
			}
			http.Error(w, "CC14/15 bounded requests; do not retry", http.StatusBadRequest)
			return
		}
		if r.Header.Get(middleware.IdempotencyKeyHeader) != "" || r.Header.Get("X-UniRouter-Client-Request-ID") != "" {
			reject("unexpected_shared_client_identity")
			http.Error(w, "CC14 independent request identities required", 400)
			return
		}
		raw, err = io.ReadAll(io.LimitReader(r.Body, cc14MaxRequest+1))
		if err != nil || len(raw) > cc14MaxRequest {
			reject("client_request_body_contract")
			http.Error(w, "CC14 request rejected", 400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		receipt := cc14RoundReceipt{Round: len(rounds) + 1, RequestSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), StartedAt: time.Now().UTC()}
		before := upstream.calls.Load()
		writer, observed := cc14ObserveResponse(w)
		decisionCtx, collector := limitpolicy.ContextWithDecisionCollector(r.Context())
		public.ServeHTTP(writer, r.WithContext(decisionCtx))
		receipt.RequestID = observed.Header().Get(middleware.OperationIDHeader)
		if scenario.fileActions {
			receipt.ProviderResponseID = upstream.responseID(receipt.Round - 1)
		}
		receipt.Status = observed.status
		receipt.ProviderCalls = upstream.calls.Load() - before
		receipt.EndedAt = time.Now().UTC()
		decisions, _ := collector.Snapshot()
		for _, d := range decisions {
			receipt.PolicySnapshotStale = receipt.PolicySnapshotStale || d.Reason == limitpolicy.ReasonSnapshotStale
		}
		if receipt.RequestID == "" || receipt.Status != 200 || receipt.ProviderCalls != 1 || receipt.PolicySnapshotStale || len(rounds) > 0 && receipt.RequestID == rounds[len(rounds)-1].RequestID || scenario.fileActions && receipt.ProviderResponseID == "" {
			reject("client_round_contract")
		}
		rounds = append(rounds, receipt)
		if cc14WriteJSON(filepath.Join(f.output, fmt.Sprintf("request-%d.json", receipt.Round)), receipt) != nil {
			reject("request_receipt_failed")
		}
	})
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); runtime.journal.Run(workerCtx) }()
	// Register cancellation before listener creation, whose failure must also join.
	var stopOnce sync.Once
	stopWorker := func() { stopOnce.Do(func() { cancelWorker(); <-workerDone }) }
	t.Cleanup(stopWorker)
	server := cc14Server(t, strings.TrimPrefix(scenario.url, "http://"), mux)
	terminals := make([]bool, scenario.rounds)
	published := false
	defer func() {
		// All owners join BEFORE assertions/receipts; Fatal cannot short-circuit joins.
		server.Close()
		closeErr := runtime.content.closeJ1()
		stopWorker()
		f.stopPolicyRefresh()
		stopGuardrailRefresh()
		upstreamServer.Close()
		mu.Lock()
		saved := append([]cc14RoundReceipt(nil), rounds...)
		failed := failure
		mu.Unlock()
		if closeErr != nil {
			t.Error("CC14 capture owner close failed")
		}
		if failed != "" || upstream.failed() != "" || len(saved) != scenario.rounds || upstream.calls.Load() != int64(scenario.rounds) || !published {
			t.Error("CC14 two-round completion failed")
		}
		defer func() {
			p3WriteJSON(t, filepath.Join(f.output, "closed.json"), map[string]any{"run": scenario.runID, "requests": saved, "provider_calls": upstream.calls.Load(), "failure": failed, "upstream_failure": upstream.failed(), "workers_stopped": true, "policy_refresh_stopped": true, "guardrail_refresh_stopped": true, "http_stopped": true, "capture_stopped": closeErr == nil, "successful": !t.Failed()})
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for {
			n, e := runtime.journal.DrainOnce(ctx)
			if e != nil {
				t.Error("CC14 final journal drain failed")
				break
			}
			if n == 0 {
				break
			}
		}
		for _, r := range saved {
			var snapshot []byte
			var hash, cost, state string
			err := pool.QueryRow(ctx, `SELECT i.snapshot,i.snapshot_sha256,f.status FROM completed_execution_feed_items i JOIN freeze_idempotency f ON f.request_id=i.request_id WHERE i.request_id=$1`, r.RequestID).Scan(&snapshot, &hash, &state)
			p3Require(t, err == nil && state == "settled", "CC14 financial terminal")
			err = pool.QueryRow(ctx, `SELECT convert_from(document,'UTF8')::jsonb->'cost'->'economic_reference'->>'snapshot_sha256' FROM execution_source_facts WHERE request_id=$1 AND profile=$2`, r.RequestID, sourcefacts.CostProfile).Scan(&cost)
			p3Require(t, err == nil && hash == cost, "CC14 economic/source binding")
			p3WriteJSON(t, filepath.Join(f.output, fmt.Sprintf("economic-%d.json", r.Round)), map[string]any{"request_id": r.RequestID, "snapshot": json.RawMessage(snapshot), "snapshot_sha256": hash, "cost_source_snapshot_sha256": cost})
		}
	}()
	var db, oid, system string
	err = pool.QueryRow(t.Context(), `SELECT current_database(),(SELECT oid::text FROM pg_database WHERE datname=current_database()),system_identifier::text FROM pg_control_system()`).Scan(&db, &oid, &system)
	p3Require(t, err == nil, "CC14 clone identity")
	p3WriteJSON(t, filepath.Join(f.output, "guardrail-policy.json"), map[string]any{"run": scenario.runID, "guardrail_policy_id": guardrailPolicyID, "guardrail_policy_ref": fmt.Sprintf("guardrail:%d", guardrailPolicyID)})
	p3WriteJSON(t, filepath.Join(f.output, "identity.json"), map[string]any{"run": scenario.runID, "binary_sha256": os.Getenv(scenario.binaryKey), "database": db, "database_oid": oid, "system_id": system, "base_database": os.Getenv("UR_P3_BASE_DATABASE"), "base_oid": os.Getenv("UR_P3_DATABASE_OID"), "gateway_key_id": issued.ID, "upstream": upstreamServer.URL})
	p3WriteJSON(t, filepath.Join(f.output, "ready.json"), ready)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			return
		case <-ticker.C:
			mu.Lock()
			saved := append([]cc14RoundReceipt(nil), rounds...)
			failed := failure
			mu.Unlock()
			if failed != "" || upstream.failed() != "" {
				t.Error("CC14 protocol failure; inspect closed receipt")
				return
			}
			for i, r := range saved {
				if terminals[i] {
					continue
				}
				state := p3ReadSettlement(f.ctx, pool, r.RequestID)
				if f.ctx.Err() != nil {
					return
				}
				p3Require(t, state.QueryOK, "CC14 terminal observation")
				if state.ready() {
					var observedProfiles int
					err := pool.QueryRow(f.ctx, `SELECT count(DISTINCT profile) FROM execution_source_facts WHERE request_id=$1 AND profile=ANY($2)`, r.RequestID, sourcefacts.ObservationProfiles()).Scan(&observedProfiles)
					if f.ctx.Err() != nil {
						return
					}
					p3Require(t, err == nil, "CC14 runtime observation query")
					if observedProfiles != len(sourcefacts.ObservationProfiles()) {
						continue
					}
					f.verifyContentPoints(t, pool, runtime.source, runtime.content, r.RequestID, org, project, i)
					p3WriteJSON(t, filepath.Join(f.output, fmt.Sprintf("request-%d-ready.json", i+1)), map[string]any{"round": i + 1, "request_id": r.RequestID, "observation": state, "content_points": 4, "runtime_profiles": sourcefacts.ObservationProfiles()})
					terminals[i] = true
				}
			}
			allTerminal := true
			for _, terminal := range terminals {
				allTerminal = allTerminal && terminal
			}
			if allTerminal && !published {
				uniqueRequests, uniqueResponses := map[string]bool{}, map[string]bool{}
				for _, receipt := range saved {
					p3Require(t, receipt.RequestID != "" && !uniqueRequests[receipt.RequestID], "CC14/15 unique UR request IDs")
					uniqueRequests[receipt.RequestID] = true
					if scenario.fileActions {
						p3Require(t, receipt.ProviderResponseID != "" && !uniqueResponses[receipt.ProviderResponseID], "CC15 unique model response IDs")
						uniqueResponses[receipt.ProviderResponseID] = true
					}
				}
				p3Require(t, len(saved) == scenario.rounds && upstream.calls.Load() == int64(scenario.rounds), "CC14/15 independent completed requests")
				p3WriteJSON(t, filepath.Join(f.output, "requests-ready.json"), map[string]any{"run": scenario.runID, "source": ready, "requests": saved, "request_count": scenario.rounds, "economic_records": scenario.rounds, "content_points": 4 * scenario.rounds, "provider_calls": scenario.rounds})
				published = true
			}
		}
	}
}

// These selectors are pure: no SetupMigratedTestDB, listener, credential creation,
// OpenClaw execution, or connection. They may run with an explicitly disabled DSN.
func TestCC14ApprovalGuardPure(t *testing.T) {
	env := map[string]string{
		"UR_CC14_INTEROP": "1", "CC14_APPROVED_RUN": "cc14-openclaw-20260924-r9", "CC14_APPROVED_ACTIONS": cc14Actions,
		"P3_CREDENTIAL_DIR": filepath.Join(p3CredentialParent, "cc14-openclaw-20260924-r9"), "UR_P3_OUTPUT_DIR": filepath.Join(p3EvidenceParent, "cc14-openclaw-20260924-r9", "ur"),
		"REQUIRE_TEST_DATABASE": "1", "REDIS_ADDR": "127.0.0.1:56379", "REDIS_DB": "15",
		"TEST_DATABASE_URL":   "postgres://127.0.0.1:56132/cc14_synthetic_base?sslmode=disable",
		"UR_P3_BASE_DATABASE": "cc14_synthetic_base", "UR_P3_DATABASE_OID": "12345", "UR_P3_SYSTEM_ID": "67890",
		"CC14_APPROVED_BINARY_SHA256": strings.Repeat("a", 64),
	}
	get := func(k string) string { return env[k] }
	if err := cc14ApprovalError(get); err != nil {
		t.Fatal("exact inert guard inputs rejected")
	}
	for _, tc := range []struct{ key, value string }{
		{"UR_CC14_INTEROP", ""}, {"UR_J1_INTEROP", "1"}, {"UR_P3_INTEROP", "1"},
		{"CC14_APPROVED_RUN", cc14RunID + "-copy"}, {"CC14_APPROVED_ACTIONS", j1Actions},
		{"CC14_APPROVED_ACTIONS", "synthetic-db-redis,http,credentials,content-binding,full-content-policy,human-review-grant,secret-cleanup,openclaw-read-two-rounds"},
		{"CC14_APPROVED_RUN", "cc14-openclaw-20260924-r8"},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, "cc14-openclaw-20260924-r8")},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, "cc14-openclaw-20260924-r8", "ur")},
		{"CC14_APPROVED_RUN", "cc14-openclaw-20260923-r7"},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, "cc14-openclaw-20260923-r7")},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, "cc14-openclaw-20260923-r7", "ur")},
		{"CC14_APPROVED_RUN", "cc14-openclaw-20260923-r6"},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, "cc14-openclaw-20260923-r6")},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, "cc14-openclaw-20260923-r6", "ur")},
		{"CC14_APPROVED_RUN", "cc14-openclaw-20260923-r5"},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, "cc14-openclaw-20260923-r5")},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, "cc14-openclaw-20260923-r5", "ur")},
		{"CC14_APPROVED_RUN", "cc14-openclaw-20260923-r4"},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, "cc14-openclaw-20260923-r4")},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, "cc14-openclaw-20260923-r4", "ur")},
		{"CC14_APPROVED_RUN", "cc14-openclaw-20260923-r3"},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, "cc14-openclaw-20260923-r3")},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, "cc14-openclaw-20260923-r3", "ur")},
		{"CC14_APPROVED_RUN", "cc14-openclaw-20260923-r2"},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, "cc14-openclaw-20260923-r2")},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, "cc14-openclaw-20260923-r2", "ur")},
		{"CC14_APPROVED_RUN", "cc14-openclaw-20260922-r1"},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, "cc14-openclaw-20260922-r1")},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, "cc14-openclaw-20260922-r1", "ur")},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, j1RunID)},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, cc14RunID+"-copy", "ur")},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, cc14RunID, "ur") + "/../ur"},
		{"REDIS_ADDR", "127.0.0.1:6379"}, {"REDIS_DB", "0"}, {"REQUIRE_TEST_DATABASE", ""},
		{"TEST_DATABASE_URL", "postgres://secret:never-log@127.0.0.1:5432/postgres"},
		{"TEST_DATABASE_URL", "postgres://127.0.0.1:56132/wrong?sslmode=disable"},
		{"TEST_DATABASE_URL", "postgres://127.0.0.1:56132/cc14_synthetic_base?sslmode=prefer"},
		{"UR_P3_BASE_DATABASE", "postgres"}, {"UR_P3_DATABASE_OID", ""}, {"UR_P3_SYSTEM_ID", "0"},
		{"CC14_APPROVED_BINARY_SHA256", strings.Repeat("A", 64)}, {"CC14_APPROVED_BINARY_SHA256", ""},
	} {
		t.Run(tc.key+"/"+fmt.Sprint(len(tc.value)), func(t *testing.T) {
			old := env[tc.key]
			env[tc.key] = tc.value
			defer func() { env[tc.key] = old }()
			err := cc14ApprovalError(get)
			if err == nil || strings.Contains(err.Error(), "never-log") {
				t.Fatal("unsafe admission or diagnostic")
			}
		})
	}
}
func cc14PureRequest(t *testing.T, second bool) []byte {
	t.Helper()
	messages := []any{map[string]any{"role": "user", "content": "Read sample.txt exactly once."}}
	if second {
		messages = append(messages, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "callcc14readsample", "type": "function", "function": map[string]any{"name": "read", "arguments": `{"path":"sample.txt"}`}}}}, map[string]any{"role": "tool", "tool_call_id": "callcc14readsample", "content": "marker: cc14-r1-cobalt-7319\nalpha: 7\nbeta: 11\n"})
	}
	wire, err := json.Marshal(map[string]any{"model": "gpt-4o", "stream": true, "stream_options": map[string]bool{"include_usage": true}, "max_tokens": 1024, "messages": messages, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return wire
}
func TestCC14ProtocolGuardPure(t *testing.T) {
	first, second := cc14PureRequest(t, false), cc14PureRequest(t, true)
	t.Run("two-real-request-shapes-and-extra-rejection", func(t *testing.T) {
		p := &cc14Protocol{}
		delta, finish, err := p.next(first)
		raw, _ := json.Marshal(delta)
		if err != nil || finish != "tool_calls" || bytes.Contains(raw, []byte("cobalt")) || !bytes.Contains(raw, []byte("sample.txt")) || !bytes.Contains(raw, []byte(`"id":"callcc14readsample"`)) {
			t.Fatal("first round must emit only schema-matching read call")
		}
		delta, finish, err = p.next(second)
		if err != nil || finish != "stop" || delta["content"] != "marker: cc14-r1-cobalt-7319; alpha: 7; beta: 11" {
			t.Fatal("tool result not validated")
		}
		if _, _, err = p.next(second); err == nil || err.Error() != "extra_model_request" {
			t.Fatal("third request accepted")
		}
	})
	for _, tc := range []struct {
		name     string
		input    []byte
		roundTwo bool
	}{
		{"no-read", bytes.ReplaceAll(first, []byte(`"name":"read"`), []byte(`"name":"exec"`)), false},
		{"unknown-path", bytes.ReplaceAll(first, []byte(`"path"`), []byte(`"filename"`)), false},
		{"wrong-schema-type", bytes.ReplaceAll(first, []byte(`"type":"string"`), []byte(`"type":"integer"`)), false},
		{"wrong-max", bytes.ReplaceAll(first, []byte(`"max_tokens":1024`), []byte(`"max_tokens":2048`)), false},
		{"missing-stream-usage", bytes.ReplaceAll(first, []byte(`"include_usage":true`), []byte(`"include_usage":false`)), false},
		{"initial-tool-history", second, false},
		{"oversized", bytes.Repeat([]byte(" "), cc14MaxRequest+1), false},
		{"missing-tool-result", first, true},
		{"wrong-tool-id", bytes.ReplaceAll(second, []byte(`"tool_call_id":"`+cc14CallID+`"`), []byte(`"tool_call_id":"other"`)), true},
		{"wrong-assistant-id", bytes.ReplaceAll(second, []byte(`"id":"`+cc14CallID+`"`), []byte(`"id":"other"`)), true},
		{"old-punctuated-id-pair", bytes.ReplaceAll(second, []byte(cc14CallID), []byte("call_cc14_read_sample")), true},
		{"arbitrary-matching-id-pair", bytes.ReplaceAll(second, []byte(cc14CallID), []byte("callother")), true},
		{"false-sample", bytes.ReplaceAll(second, []byte("cobalt-7319"), []byte("cobalt-0000")), true},
		{"wrong-read-file", bytes.ReplaceAll(second, []byte("sample.txt"), []byte("private.txt")), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &cc14Protocol{}
			if tc.roundTwo {
				if _, _, err := p.next(first); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := p.next(tc.input); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}
func TestCC14BootstrapGuardPure(t *testing.T) {
	ready := p3SourceReady{URL: cc14URL, SourceInstance: "synthetic-http", OrgID: "1", ProjectID: "2", CredentialDir: filepath.Join(p3CredentialParent, cc14RunID), Content: &j1ContentReady{CredentialID: "3", RegistrationHash: "hash", CapabilitiesSHA256: "digest"}}
	raw, _ := json.Marshal(ready)
	if !cc14BootstrapMatches(raw, ready) {
		t.Fatal("exact bootstrap rejected")
	}
	for _, change := range []func(*p3SourceReady){
		func(r *p3SourceReady) { r.URL = "http://127.0.0.1:19187" }, func(r *p3SourceReady) { r.OrgID = "other" }, func(r *p3SourceReady) { r.ProjectID = "other" },
		func(r *p3SourceReady) { r.SourceInstance = "other" }, func(r *p3SourceReady) { r.CredentialDir += "-other" }, func(r *p3SourceReady) { r.Content = nil },
		func(r *p3SourceReady) {
			r.Content = &j1ContentReady{CredentialID: "3", RegistrationHash: "other", CapabilitiesSHA256: "digest"}
		},
	} {
		got := ready
		change(&got)
		raw, _ := json.Marshal(got)
		if cc14BootstrapMatches(raw, ready) {
			t.Fatal("mismatched bootstrap accepted")
		}
	}
}

// Inventory differences are evidence only, never a deletion or authorization
// selector. The unchanged native helper owns both exact database names.
func cc14DatabaseInventory(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT datname,oid::text FROM pg_database`)
	p3Require(t, err == nil, "CC14 database inventory")
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var name, oid string
		p3Require(t, rows.Scan(&name, &oid) == nil, "CC14 database identity row")
		result[name] = oid
	}
	p3Require(t, rows.Err() == nil, "CC14 complete database inventory")
	return result
}
func cc14RecordDatabases(t *testing.T, f *p3SourceFixture, pool *pgxpool.Pool, runID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	after := cc14DatabaseInventory(t, ctx, pool)
	var clone, system string
	p3Require(t, pool.QueryRow(ctx, `SELECT current_database(),system_identifier::text FROM pg_control_system()`).Scan(&clone, &system) == nil && system == os.Getenv("UR_P3_SYSTEM_ID"), "CC14 clone cluster identity")
	added := map[string]string{}
	unchanged := true
	for name, oid := range f.cc14DatabasesBefore {
		if after[name] != oid {
			unchanged = false
		}
	}
	for name, oid := range after {
		if _, exists := f.cc14DatabasesBefore[name]; !exists {
			added[name] = oid
		}
	}
	// Record precise observed identities even if the exclusive-window assertion fails.
	p3WriteJSON(t, filepath.Join(f.output, "databases.json"), map[string]any{"run": runID, "system_id": system, "base_database": os.Getenv("UR_P3_BASE_DATABASE"), "base_oid": os.Getenv("UR_P3_DATABASE_OID"), "clone": clone, "created_databases": added, "existing_unchanged": unchanged, "cleanup_owner": "native SetupMigratedTestDB/TestMain; exact names only"})
	p3Require(t, unchanged && len(added) == 2 && added[clone] != "", "CC14 exclusive window must create exactly template and clone")
}

func TestCC14GuardrailRefreshJoinPure(t *testing.T) {
	for _, parentCancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent_cancel_%t", parentCancel), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cancelled, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			stop := cc14StartGuardrailRefresh(ctx, func(ctx context.Context) {
				<-ctx.Done()
				close(cancelled)
				<-release // Model refresh still unwinding after cancellation.
			})
			t.Cleanup(func() { unblock(); stop() })
			if parentCancel {
				cancel()
			}
			go func() { stop(); close(returned) }()
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("refresh loop was not cancelled")
			}
			select {
			case <-returned:
				t.Fatal("stop returned before the refresh loop finished")
			case <-time.After(20 * time.Millisecond):
			}
			unblock()
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("stop did not join the finished refresh loop")
			}
			stop() // The explicit shutdown and registered cleanup may both call it.
		})
	}
}

func TestCC14GuardrailRefreshCleanupPure(t *testing.T) {
	finished := make(chan struct{})
	t.Run("setup_returns_before_listener", func(t *testing.T) {
		stop := cc14StartGuardrailRefresh(t.Context(), func(ctx context.Context) {
			<-ctx.Done()
			close(finished)
		})
		t.Cleanup(stop)
		// No full shutdown defer was installed: setup cleanup alone owns the join.
	})
	select {
	case <-finished:
	default:
		t.Fatal("setup cleanup did not join the refresh loop")
	}
}

func TestCC14GuardrailConstructorPure(t *testing.T) {
	// Same constructor and exact filter, fingerprint and options as the producer.
	// NewService does not query the pool; this test does not call Refresh or install a snapshot.
	service, err := cc14NewGuardrailService(nil)
	if err != nil {
		t.Fatalf("CC14 guardrail service: %v", err)
	}
	if service == nil {
		t.Fatal("CC14 guardrail constructor returned no service")
	}
}

func TestCC14GuardrailR4ConstructorRegressionPure(t *testing.T) {
	service, err := guardrail.NewService(nil, filter.NewDFAFilter(nil), []byte(prd2E2EJWTSecret), guardrail.DefaultOptions())
	const expected = "guardrail evidence fingerprint secret must be at least 16 bytes"
	if service != nil || err == nil || err.Error() != expected {
		t.Fatal("r4 short fingerprint input must fail with the precise non-sensitive constructor error")
	}
}
