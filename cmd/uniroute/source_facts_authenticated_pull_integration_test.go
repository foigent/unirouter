//go:build integration

package main

import (
	"context"
	"crypto/rand"
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
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/testutil/sourcereceiver"
)

func sourceHTTPAuthenticatedPull(t *testing.T, pool *pgxpool.Pool, source *sourcefacts.Service, secret string, org, project int64, credentialChecks ...func(string)) func() {
	observe := sourcereceiver.Observe
	if source.ObservationsEnabled() {
		observe = sourcereceiver.ObserveWithObservations
	}
	return observe(t, pool, source, secret, prd2E2EJWTSecret, org, project,
		func(mux *http.ServeMux, h *admin.Handler, guard func(http.HandlerFunc) http.Handler) {
			registerXInsightSourceControlRoutes(mux, h, xInsightFeedControlRouteGuards{safety: guard, expansion: guard})
		}, credentialChecks...)
}

// P3 is an opt-in component fixture, never an installed-service/license proof.
// Only these two random receiver tokens leave memory, in an explicitly admitted
// fresh root-owned directory. Output contains no credentials.
const p3CredentialParent = "/data/x-insight/validation/ur-xi-20260920/credentials/xi-product-test"
const p3EvidenceParent = "/data/x-insight/validation/ur-xi-20260920/evidence"

type p3SourceFixture struct {
	cc14DatabasesBefore           map[string]string
	denyLegacyContent             func(string)
	cc14                          bool
	j1                            bool
	content                       *j1ContentReady
	contentAction                 func(string)
	contentPaused, contentRevoked bool
	policyCancel                  context.CancelFunc
	policyDone                    chan struct{}
	credentials, output           string
	ctx                           context.Context
	root                          *os.Root
	files                         map[string]os.FileInfo
}

type p3SourceReady struct {
	URL            string          `json:"url"`
	SourceInstance string          `json:"source_instance"`
	OrgID          string          `json:"org_id"`
	ProjectID      string          `json:"project_id"`
	CredentialDir  string          `json:"credential_dir"`
	Content        *j1ContentReady `json:"content,omitempty"`
}

func p3Require(t *testing.T, ok bool, stage string) {
	t.Helper()
	if !ok {
		t.Fatal("P3 fixture: " + stage)
	}
}

func p3CheckDirectory(t *testing.T, path string, private bool) {
	t.Helper()
	p3Require(t, filepath.IsAbs(path) && filepath.Clean(path) == path, "unclean directory")
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		p3Require(t, err == nil, "directory unavailable")
		stat, ok := info.Sys().(*syscall.Stat_t)
		p3Require(t, ok && stat.Uid == 0 && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0022 == 0, "directory owner/mode/link")
		if current == path && private {
			p3Require(t, info.Mode().Perm() == 0700, "private directory must be 0700")
		}
		if current == "/" {
			break
		}
	}
}

func newP3SourceFixture(t *testing.T) *p3SourceFixture {
	return newApprovedSourceFixture(t, false)
}

func newApprovedSourceFixture(t *testing.T, cc14 bool) *p3SourceFixture {
	t.Helper()
	scope, scopeErr := p3ProducerScopeFromEnv(os.Getenv)
	p3Require(t, scopeErr == nil && !(scope.portable && cc14), "approved portable producer scope")
	if cc14 {
		cc14RequireApproval(t)
	}
	j1 := os.Getenv("UR_J1_INTEROP") == "1"
	if j1 {
		j1RequireApproval(t)
	}
	runCtx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	t.Cleanup(stop)
	p3Require(t, os.Geteuid() == 0 && os.Getenv("REQUIRE_TEST_DATABASE") == "1", "root and required database guard")
	p3Require(t, os.Getenv("REDIS_ADDR") == fmt.Sprintf("127.0.0.1:%d", scope.redisPort) && os.Getenv("REDIS_DB") == "15", "exact dedicated Redis/DB15")
	if scope.portable {
		p3CheckDirectory(t, scope.root, true)
		p3CheckDirectory(t, scope.credentialParent, true)
		p3CheckDirectory(t, filepath.Dir(os.Getenv("UR_P3_OUTPUT_DIR")), true)
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	p3Require(t, err == nil, "database configuration")
	c := cfg.ConnConfig
	p3Require(t, c.Host == "127.0.0.1" && c.Port == scope.urPort && len(c.Fallbacks) == 0 && c.Database == os.Getenv("UR_P3_BASE_DATABASE") && c.Database != "" && c.Database != "postgres" && !strings.HasPrefix(c.Database, "template"), "exact synthetic UR database")
	if scope.portable {
		p3Require(t, p3ProducerDatabaseMatches(c, scope.urPort, os.Getenv("UR_P3_BASE_DATABASE")), "portable UR database without fallbacks or session overrides")
	}
	// Read-only identity proof precedes fixture configuration writes.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	p3Require(t, err == nil, "identity connection")
	defer pool.Close()
	var oid, system string
	var tables int
	err = pool.QueryRow(ctx, `SELECT (SELECT oid::text FROM pg_database WHERE datname=current_database()),system_identifier::text,(SELECT count(*) FROM pg_tables WHERE schemaname='public') FROM pg_control_system()`).Scan(&oid, &system, &tables)
	p3Require(t, err == nil && oid == os.Getenv("UR_P3_DATABASE_OID") && system == os.Getenv("UR_P3_SYSTEM_ID") && tables == 0, "UR identity or empty base changed")
	var databasesBefore map[string]string
	if cc14 {
		databasesBefore = cc14DatabaseInventory(t, ctx, pool)
	}
	credentials := os.Getenv("P3_CREDENTIAL_DIR")
	output := os.Getenv("UR_P3_OUTPUT_DIR")
	if cc14 {
		cc14RequireApproval(t)
	} else {
		p3Require(t, filepath.Dir(credentials) == scope.credentialParent && strings.HasPrefix(filepath.Base(credentials), "p3-") && filepath.Clean(credentials) == credentials, "credential directory scope")
	}
	p3Require(t, filepath.Dir(filepath.Dir(output)) == scope.evidenceParent && filepath.Base(output) == "ur" && filepath.Base(filepath.Dir(output)) == filepath.Base(credentials), "evidence directory scope")
	p3CheckDirectory(t, scope.credentialParent, true)
	p3CheckDirectory(t, filepath.Dir(output), true)
	p3Require(t, os.Mkdir(credentials, 0700) == nil, "credential directory must not exist")
	p3CheckDirectory(t, credentials, true)
	root, err := os.OpenRoot(credentials)
	p3Require(t, err == nil, "open credential root")
	f := &p3SourceFixture{cc14DatabasesBefore: databasesBefore, cc14: cc14, j1: j1, ctx: runCtx, credentials: credentials, output: output, root: root, files: map[string]os.FileInfo{}}
	// Never remove a replacement or any unknown file. Keep the empty directory
	// as a non-reusable identity, and retain evidence even on failure.
	t.Cleanup(func() {
		for name, original := range f.files {
			now, e := root.Lstat(name)
			if e != nil || !os.SameFile(original, now) || !now.Mode().IsRegular() {
				t.Errorf("P3 secret cleanup refused changed file: %s", name)
				continue
			}
			if e = root.Remove(name); e != nil {
				t.Errorf("P3 secret cleanup failed: %s", name)
			}
		}
		_ = root.Close()
	})
	p3Require(t, os.Mkdir(output, 0700) == nil, "producer output must not exist")
	return f
}

func (f *p3SourceFixture) secret(t *testing.T, name, value string) {
	t.Helper()
	p3Require(t, name == "completed-feed.key" || name == "source-facts.key" || (f.j1 || f.cc14) && (name == "source-content.key" || name == "content-store.binding") || f.cc14 && name == "ur-gateway.key", "unknown secret filename")
	file, err := f.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	p3Require(t, err == nil, "exclusive secret creation")
	info, err := file.Stat()
	p3Require(t, err == nil, "secret identity")
	f.files[name] = info // cleanup owns the file even after a partial write.
	_, err = io.WriteString(file, value+"\n")
	closeErr := file.Close()
	stat, ok := info.Sys().(*syscall.Stat_t)
	p3Require(t, err == nil && closeErr == nil && ok && stat.Uid == 0 && stat.Nlink == 1 && info.Mode().Perm() == 0600, "secret write/owner/mode")
}

func p3WriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	p3Require(t, err == nil, "encode evidence")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	p3Require(t, err == nil, "exclusive evidence creation")
	_, err = file.Write(raw)
	closeErr := file.Close()
	p3Require(t, err == nil && closeErr == nil, "write evidence")
}

func (f *p3SourceFixture) serve(t *testing.T, pool *pgxpool.Pool, source *sourcefacts.Service, feed *completedfeed.Service, journal *outbox.Service, public http.Handler, secret, sourceToken string, org, project int64, calls *atomic.Int64, contentHarness *sourceHTTPContentHarness) {
	mux := f.prepareSourceEndpoints(t, pool, source, feed, secret, sourceToken, org, project, contentHarness)
	var dispatched [2]atomic.Bool
	var canonical [2]atomic.Value
	var lastObserved [2]p3Settlement
	var dispatchMu sync.Mutex
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// The parent XI fixture writes this receipt only after durable bootstrap.
		// A request with an unknown response is never forwarded a second time.
		raw, e := os.ReadFile(filepath.Join(filepath.Dir(f.output), "bootstrap.json"))
		var ready p3SourceReady
		if e != nil || json.Unmarshal(raw, &ready) != nil || ready.OrgID != fmt.Sprint(org) || ready.ProjectID != fmt.Sprint(project) || ready.SourceInstance != source.SourceInstance() || ready.CredentialDir != f.credentials || f.j1 && (ready.Content == nil || *ready.Content != *f.content) {
			http.Error(w, "fixture bootstrap required", http.StatusPreconditionFailed)
			return
		}
		slot := 0
		requestName := "request.json"
		if r.Header.Get("X-UniRouter-Client-Request-ID") == filepath.Base(filepath.Dir(f.output))+"-catchup" {
			slot, requestName = 1, "request-catchup.json"
			_, blocked := os.Stat(filepath.Join(f.output, "feed-disconnect"))
			_, resumed := os.Stat(filepath.Join(f.output, "feed-resume"))
			if blocked != nil || !os.IsNotExist(resumed) || canonical[0].Load() == nil {
				http.Error(w, "catchup requires an established blocked source", http.StatusPreconditionFailed)
				return
			}
		} else if r.Header.Get("X-UniRouter-Client-Request-ID") != filepath.Base(filepath.Dir(f.output)) {
			http.Error(w, "unknown fixture request", http.StatusBadRequest)
			return
		}
		dispatchMu.Lock()
		defer dispatchMu.Unlock()
		if !dispatched[slot].CompareAndSwap(false, true) {
			http.Error(w, "fixture request already dispatched; inspect receipt, do not retry", http.StatusConflict)
			return
		}
		beforeCalls := calls.Load()
		observed := &p3ResponseWriter{ResponseWriter: w, status: http.StatusOK}
		decisionCtx, collector := limitpolicy.ContextWithDecisionCollector(r.Context())
		public.ServeHTTP(observed, r.WithContext(decisionCtx))
		decisions, _ := collector.Snapshot()
		snapshotStale := false
		for _, decision := range decisions {
			snapshotStale = snapshotStale || decision.Reason == limitpolicy.ReasonSnapshotStale
		}
		canonical[slot].Store(observed.Header().Get(middleware.OperationIDHeader))
		// Safe producer receipt survives a client response loss; never repeat the request.
		raw, e = json.Marshal(map[string]any{"request_id": observed.Header().Get(middleware.OperationIDHeader), "status": observed.status, "provider_calls": calls.Load(), "request_provider_calls": calls.Load() - beforeCalls, "policy_snapshot_stale": snapshotStale})
		if e != nil {
			t.Error("P3 request receipt encoding failed")
			return
		}
		file, e := os.OpenFile(filepath.Join(f.output, requestName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			t.Error("P3 request receipt creation failed")
			return
		}
		_, e = file.Write(raw)
		ce := file.Close()
		if e != nil || ce != nil {
			t.Error("P3 request receipt write failed")
		}
	})
	ctx := f.ctx
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); journal.Run(workerCtx) }()
	// Test-only transport failure: public dispatch and database/outbox stay alive.
	var gateMu sync.Mutex
	blockedPaths := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		feedPaths := map[string]string{
			"/api/v1/integrations/xinsight/completed-executions":      "completed-executions",
			"/api/v1/integrations/xinsight/source-facts":              "source-facts",
			"/api/v1/integrations/xinsight/source-facts/capabilities": "source-facts-capabilities",
			"/api/v1/integrations/xinsight/source/v2/events":          "source-v2-events",
			"/api/v1/integrations/xinsight/source/v2/capabilities":    "source-v2-capabilities",
			"/api/v1/integrations/xinsight/source/v2/runtime/current": "source-v2-runtime-current",
		}
		if name, ok := feedPaths[r.URL.Path]; r.Method == http.MethodGet && ok {
			_, blocked := os.Stat(filepath.Join(f.output, "feed-disconnect"))
			_, resumed := os.Stat(filepath.Join(f.output, "feed-resume"))
			if blocked == nil && os.IsNotExist(resumed) {
				gateMu.Lock()
				// One safe receipt per fixed path, without query, headers or tokens.
				first := !blockedPaths[name]
				blockedPaths[name] = true
				gateMu.Unlock()
				if first {
					p3WriteJSON(t, filepath.Join(f.output, "feed-failed-"+name+".json"), map[string]any{"status": 503, "path": r.URL.Path, "at": time.Now().UTC()})
				}
				http.Error(w, "synthetic feed temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		mux.ServeHTTP(w, r)
	}))
	// Reuse observations from the running loop; diagnostics never query before
	// the original drain and financial verification have had their full budget.
	recordSettlements := func(ctx context.Context, name string, fresh bool) {
		states := []p3Settlement{}
		for slot := range canonical {
			if id, ok := canonical[slot].Load().(string); ok && id != "" {
				state := lastObserved[slot]
				if state.RequestID == "" {
					state.RequestID = id
				}
				if fresh {
					state = p3Settlement{RequestID: id}
					if ctx.Err() == nil {
						state = p3ReadSettlement(ctx, pool, id)
					}
				}
				states = append(states, state)
			}
		}
		origin := "last-observed-before-drain"
		if fresh {
			origin = "after-core-verification-with-remaining-budget"
		}
		p3WriteJSON(t, filepath.Join(f.output, name), map[string]any{"observation_source": origin, "states": states})
	}
	defer f.finishProducer(t, calls, &dispatched, func() {
		server.Close()
		if f.j1 {
			if contentHarness.closeJ1() != nil {
				t.Error("J1 capture owner close failed")
			}
		}
		cancelWorker()
		<-workerDone
	}, func() {
		want := int64(0)
		for i := range dispatched {
			if dispatched[i].Load() {
				want++
			}
		}
		if calls.Load() != want {
			t.Error("P3 expected exactly one provider call per dispatched request")
		}
		drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p3VerifyBeforeDiagnostics(drainCtx, func() {
			for {
				n, e := journal.DrainOnce(drainCtx)
				if e != nil {
					t.Error("P3 final journal drain failed")
					break
				}
				if n == 0 {
					break
				}
			}
			for slot := range canonical {
				if id, ok := canonical[slot].Load().(string); ok && id != "" {
					var snapshot []byte
					var snapshotHash, costHash, state string
					e := pool.QueryRow(drainCtx, `SELECT i.snapshot,i.snapshot_sha256,f.status FROM completed_execution_feed_items i JOIN freeze_idempotency f ON f.request_id=i.request_id WHERE i.request_id=$1`, id).Scan(&snapshot, &snapshotHash, &state)
					p3Require(t, e == nil && state == "settled", "producer financial terminal")
					e = pool.QueryRow(drainCtx, `SELECT convert_from(document,'UTF8')::jsonb->'cost'->'economic_reference'->>'snapshot_sha256' FROM execution_source_facts WHERE request_id=$1 AND profile=$2`, id, sourcefacts.CostProfile).Scan(&costHash)
					p3Require(t, e == nil && snapshotHash == costHash, "producer economic/source binding")
					name := "economic.json"
					if slot == 1 {
						name = "economic-catchup.json"
					}
					p3WriteJSON(t, filepath.Join(f.output, name), map[string]any{"snapshot": json.RawMessage(snapshot), "snapshot_sha256": snapshotHash, "cost_source_snapshot_sha256": costHash})
				}
			}
		}, func(ctx context.Context) {
			recordSettlements(ctx, "settlement-before-drain.json", false)
			recordSettlements(ctx, "settlement-after-drain.json", true)
		})
	})
	p3WriteJSON(t, filepath.Join(f.output, "ready.json"), p3SourceReady{URL: server.URL, SourceInstance: source.SourceInstance(), OrgID: fmt.Sprint(org), ProjectID: fmt.Sprint(project), CredentialDir: f.credentials, Content: f.content})
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var terminalRecorded [2]bool
	for {
		select {
		case <-ctx.Done():

			return
		case <-ticker.C:
			if f.j1 {
				f.contentSafetyStages(t)
			}
			for slot := range canonical {
				if id, ok := canonical[slot].Load().(string); ok && id != "" && !terminalRecorded[slot] {
					status := p3ReadSettlement(ctx, pool, id)
					lastObserved[slot] = status
					if ctx.Err() != nil {
						return
					}
					p3Require(t, status.QueryOK, "terminal observation; inspect shutdown settlement receipts")
					if status.ready() {
						if f.j1 {
							f.verifyContentPoints(t, pool, source, contentHarness, id, org, project, slot)
						}
						name := "request-ready.json"
						if slot == 1 {
							name = "backlog-ready.json"
						}
						p3WriteJSON(t, filepath.Join(f.output, name), map[string]any{"request_id": id, "settled": status.Counts.Settled, "economic_records": status.Counts.Economic, "source_profiles": 3, "provider_calls": calls.Load(), "observation": status})
						terminalRecorded[slot] = true
					}
				}
			}
		}
	}
}

// The admitted P3 request is non-streaming; preserve ResponseController access.
type p3ResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *p3ResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *p3ResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// This is the existing production refresh loop, owned only by the P3 fixture.
func (f *p3SourceFixture) startPolicyRefresh(t *testing.T, limits *limitpolicy.Service) {
	ctx, cancel := context.WithCancel(context.Background())
	f.policyCancel, f.policyDone = cancel, make(chan struct{})
	go func() { defer close(f.policyDone); limits.RunRefreshLoop(ctx) }()
	// Covers setup failures before serve installs its full shutdown sequence.
	t.Cleanup(f.stopPolicyRefresh)
}
func (f *p3SourceFixture) stopPolicyRefresh() {
	if f.policyCancel != nil {
		f.policyCancel()
		<-f.policyDone
	}
}
func (f *p3SourceFixture) finishProducer(t *testing.T, calls *atomic.Int64, dispatched *[2]atomic.Bool, stopTransportAndOutbox, verifyTerminals func()) {
	stopTransportAndOutbox()
	f.stopPolicyRefresh()
	// Fatal/Goexit during financial verification still writes a truthful close
	// receipt. A stopped worker is not proof of a successful financial terminal.
	defer func() {
		p3WriteJSON(t, filepath.Join(f.output, "closed.json"), map[string]any{
			"provider_calls": calls.Load(), "dispatched": dispatched[0].Load(), "catchup_dispatched": dispatched[1].Load(),
			"workers_stopped": true, "policy_refresh_stopped": f.policyDone != nil, "successful": !t.Failed(),
		})
	}()
	verifyTerminals()
}

func p3VerifyBeforeDiagnostics(ctx context.Context, verify func(), diagnose func(context.Context)) {
	defer diagnose(ctx)
	verify()
}

func TestP3ProducerCloseReceipt(t *testing.T) {
	executable, err := os.Executable()
	p3Require(t, err == nil, "test executable")
	for _, mode := range []string{"success", "terminal-failure", "blocked-diagnostic", "terminal-failure-blocked-diagnostic"} {
		t.Run(mode, func(t *testing.T) {
			output := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestP3ProducerCloseReceiptHelper$", "-test.timeout=5s")
			cmd.Env = []string{"P3_CLOSE_TEST_OUTPUT=" + output, "P3_CLOSE_TEST_MODE=" + mode}
			raw, err := cmd.CombinedOutput()
			want := 0
			if strings.HasPrefix(mode, "terminal-failure") {
				want = 1
			}
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != want || ctx.Err() != nil {
				t.Fatalf("close helper exit: %v %s", err, raw)
			}
			// Proves both joins occurred before the deliberate terminal Fatal, rather
			// than accepting an unrelated child failure as this regression's evidence.
			if _, err := os.Stat(filepath.Join(output, "terminal-entered")); err != nil {
				t.Fatal("worker joins not verified", err)
			}
			if strings.Contains(mode, "blocked-diagnostic") {
				if _, err := os.Stat(filepath.Join(output, "diagnostic-finished")); err != nil {
					t.Fatal("diagnostic did not unwind within the original budget")
				}
			}
			var receipt struct {
				Stopped    bool `json:"workers_stopped"`
				Policy     bool `json:"policy_refresh_stopped"`
				Successful bool `json:"successful"`
			}
			data, err := os.ReadFile(filepath.Join(output, "closed.json"))
			if err != nil || json.Unmarshal(data, &receipt) != nil || !receipt.Stopped || !receipt.Policy || receipt.Successful != (want == 0) {
				t.Fatalf("incorrect close receipt: %s %v", data, err)
			}
		})
	}
}
func TestP3ProducerCloseReceiptHelper(t *testing.T) {
	output, mode := os.Getenv("P3_CLOSE_TEST_OUTPUT"), os.Getenv("P3_CLOSE_TEST_MODE")
	if output == "" {
		t.Skip("isolated close-receipt subprocess only")
	}
	p3Require(t, filepath.IsAbs(output) && (mode == "success" || mode == "terminal-failure" || mode == "blocked-diagnostic" || mode == "terminal-failure-blocked-diagnostic"), "close helper inputs")
	var dials atomic.Int64
	rdb := redis.NewClient(&redis.Options{Dialer: func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, fmt.Errorf("network prohibited in close test")
	}})
	defer rdb.Close()
	limits, err := limitpolicy.NewService(nil, rdb, limitpolicy.DefaultOptions())
	p3Require(t, err == nil, "offline policy service")
	f := &p3SourceFixture{output: output}
	f.startPolicyRefresh(t, limits)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { <-ctx.Done(); close(done) }()
	var calls atomic.Int64
	var dispatched [2]atomic.Bool
	f.finishProducer(t, &calls, &dispatched, func() {
		_, err := os.Stat(filepath.Join(output, "closed.json"))
		p3Require(t, os.IsNotExist(err), "no early close receipt")
		cancel()
		<-done
	}, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		p3VerifyBeforeDiagnostics(ctx, func() {
			select {
			case <-done:
			default:
				t.Fatal("outbox join missing")
			}
			select {
			case <-f.policyDone:
			default:
				t.Fatal("policy refresh join missing")
			}
			p3Require(t, dials.Load() == 0, "no external I/O")
			p3Require(t, os.WriteFile(filepath.Join(output, "terminal-entered"), []byte("joined\n"), 0600) == nil, "terminal marker")
			if strings.HasPrefix(mode, "terminal-failure") {
				t.Fatal("intentional missing financial terminal")
			}
		}, func(ctx context.Context) {
			if !strings.Contains(mode, "blocked-diagnostic") {
				return
			}
			// A diagnostic that blocks must begin only after the original financial
			// callback ran, and is bounded by that callback's existing context.
			_, err := os.Stat(filepath.Join(output, "terminal-entered"))
			p3Require(t, err == nil, "financial verification must precede diagnostic")
			<-ctx.Done()
			p3Require(t, os.WriteFile(filepath.Join(output, "diagnostic-finished"), []byte("bounded\n"), 0600) == nil, "diagnostic completion marker")
		})
	})
}

// One statement gives a consistent read; unknown query results stay null, never zero.
type p3SettlementCounts struct {
	Settled     int `json:"settled"`
	Economic    int `json:"economic_records"`
	Context     int `json:"context_terminal_records"`
	Identity    int `json:"identity_terminal_records"`
	Cost        int `json:"cost_terminal_records"`
	AllProfiles int `json:"all_profiles"`
}
type p3Settlement struct {
	RequestID  string              `json:"request_id"`
	ObservedAt time.Time           `json:"observed_at"`
	QueryOK    bool                `json:"query_ok"`
	Counts     *p3SettlementCounts `json:"counts"`
}

func (s p3Settlement) ready() bool {
	c := s.Counts
	// Presence in each fixed profile is the prior restricted DISTINCT==3 contract.
	return s.QueryOK && c != nil && c.Settled == 1 && c.Economic == 1 && c.Context > 0 && c.Identity > 0 && c.Cost > 0
}

const p3SettlementSQL = `SELECT
 (SELECT count(*) FROM freeze_idempotency WHERE request_id=$1 AND status='settled'),
 (SELECT count(*) FROM completed_execution_feed_items WHERE request_id=$1),
 count(*) FILTER (WHERE stage='terminal' AND profile=$2),
 count(*) FILTER (WHERE stage='terminal' AND profile=$3),
 count(*) FILTER (WHERE stage='terminal' AND profile=$4),
 count(DISTINCT profile) FROM execution_source_facts WHERE request_id=$1`

func p3ReadSettlement(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) p3Settlement {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s := p3Settlement{RequestID: id, ObservedAt: time.Now().UTC()}
	var c p3SettlementCounts
	if err := q.QueryRow(ctx, p3SettlementSQL, id, sourcefacts.ContextProfile, sourcefacts.IdentityProfile, sourcefacts.CostProfile).Scan(&c.Settled, &c.Economic, &c.Context, &c.Identity, &c.Cost, &c.AllProfiles); err == nil {
		s.QueryOK, s.Counts = true, &c
	}
	return s
}

// Observe the production loop advancing its snapshot; never refresh on demand.
func (f *p3SourceFixture) checkPolicyRefresh(t *testing.T, limits *limitpolicy.Service, subject limitpolicy.Subject) {
	ctx, cancel := context.WithTimeout(f.ctx, 2*limitpolicy.DefaultOptions().RefreshInterval)
	defer cancel()
	read := func(ctx context.Context) (limitpolicy.Readiness, error) { return limits.Readiness(ctx, subject) }
	first, err := read(ctx)
	last, advanced := first, false
	if err == nil && first.State == limitpolicy.ReadinessReady {
		last, advanced = p3AwaitPolicyRefresh(ctx, first.SnapshotLoadedAt, read)
	}
	p3WriteJSON(t, filepath.Join(f.output, "policy-refresh-preflight.json"), map[string]any{"successful": advanced, "first_snapshot_at": first.SnapshotLoadedAt, "last_snapshot_at": last.SnapshotLoadedAt, "cancelled": ctx.Err() == context.Canceled, "timed_out": ctx.Err() == context.DeadlineExceeded})
	p3Require(t, advanced, "policy refresh preflight")
}
func p3AwaitPolicyRefresh(ctx context.Context, first time.Time, read func(context.Context) (limitpolicy.Readiness, error)) (limitpolicy.Readiness, bool) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var last limitpolicy.Readiness
	for {
		if ctx.Err() != nil {
			return last, false
		}
		var err error
		last, err = read(ctx)
		if err != nil || last.State != limitpolicy.ReadinessReady {
			return last, false
		}
		if last.SnapshotLoadedAt.After(first) {
			return last, true
		}
		select {
		case <-ctx.Done():
			return last, false
		case <-ticker.C:
		}
	}
}
func TestP3PolicyRefreshPreflight(t *testing.T) {
	first := time.Now()
	for _, mode := range []string{"advanced", "stalled", "unavailable", "error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			_, ok := p3AwaitPolicyRefresh(ctx, first, func(context.Context) (limitpolicy.Readiness, error) {
				v := limitpolicy.Readiness{State: limitpolicy.ReadinessReady, SnapshotLoadedAt: first}
				if mode == "advanced" {
					v.SnapshotLoadedAt = first.Add(time.Second)
				}
				if mode == "unavailable" {
					v.State = limitpolicy.ReadinessUnavailable
				}
				if mode == "error" {
					return v, fmt.Errorf("synthetic failure")
				}
				return v, nil
			})
			if ok != (mode == "advanced") {
				t.Fatalf("unexpected preflight: %t", ok)
			}
		})
	}
}
func TestP3SettlementReadiness(t *testing.T) {
	for _, mode := range []string{"ready", "observations", "missing-context", "missing-identity", "missing-cost", "missing-economic", "duplicate-economic", "not-settled", "duplicate-settled", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			c := p3SettlementCounts{Settled: 1, Economic: 1, Context: 1, Identity: 1, Cost: 1, AllProfiles: 3}
			s := p3Settlement{QueryOK: true, Counts: &c}
			switch mode {
			case "observations":
				c.AllProfiles = 8
			case "missing-context":
				c.Context = 0
			case "missing-identity":
				c.Identity = 0
			case "missing-cost":
				c.Cost = 0
			case "missing-economic":
				c.Economic = 0
			case "duplicate-economic":
				c.Economic = 2
			case "not-settled":
				c.Settled = 0
			case "duplicate-settled":
				c.Settled = 2
			case "unknown":
				s.QueryOK = false
				s.Counts = nil
			}
			if s.ready() != (mode == "ready" || mode == "observations") {
				t.Fatal("terminal readiness mismatch")
			}
		})
	}
}

type p3FailedSettlementQuery struct{}

func (p3FailedSettlementQuery) QueryRow(context.Context, string, ...any) pgx.Row {
	return p3FailedSettlementRow{}
}

type p3FailedSettlementRow struct{}

func (p3FailedSettlementRow) Scan(...any) error {
	return fmt.Errorf("synthetic untrusted driver detail")
}
func TestP3SettlementFailureRemainsUnknown(t *testing.T) {
	s := p3ReadSettlement(t.Context(), p3FailedSettlementQuery{}, "op_synthetic")
	raw, err := json.Marshal(s)
	if err != nil || s.QueryOK || s.Counts != nil || s.ready() || !strings.Contains(string(raw), `"counts":null`) || strings.Contains(string(raw), "driver detail") {
		t.Fatal("failed query must retain unknown counts without raw driver text")
	}
}

// A guard records an external approval; setting it does not itself grant authority.
const j1RunID = "p3-j1-content-20260922-r3"
const j1Actions = "synthetic-db-redis,http,credentials,content-binding,full-content-policy,human-review-grant,secret-cleanup"

func j1RequireApproval(t *testing.T) {
	t.Helper()
	p3Require(t, os.Getenv("UR_J1_INTEROP") == "1" && os.Getenv("UR_P3_INTEROP") == "", "exclusive J1 mode")
	p3Require(t, os.Getenv("J1_APPROVED_RUN") == j1RunID && os.Getenv("J1_APPROVED_ACTIONS") == j1Actions, "J1 runtime actions require explicit approval before any side effect")
	p3Require(t, os.Getenv("P3_CREDENTIAL_DIR") == filepath.Join(p3CredentialParent, j1RunID) && os.Getenv("UR_P3_OUTPUT_DIR") == filepath.Join(p3EvidenceParent, j1RunID, "ur"), "J1 exact approved paths")
}

type j1ContentReady struct {
	CredentialID       string `json:"credential_id"`
	RegistrationHash   string `json:"registration_hash"`
	CapabilitiesSHA256 string `json:"capabilities_sha256"`
}

func (f *p3SourceFixture) prepareContent(t *testing.T, controls *http.ServeMux, control *admin.Handler, guard func(http.HandlerFunc) http.Handler, jwt string, mux *http.ServeMux, source *sourcefacts.Service, resolver middleware.ClientIPResolver, h *sourceHTTPContentHarness, org, project int64, completedKey, sourceKey string) {
	f.requireContentApproval(t)
	diagnostic := j1PrepareDiagnostic{Stage: "begin"}
	defer func() { p3WriteJSON(t, filepath.Join(f.output, "content-prepare-status.json"), diagnostic) }()
	check := func(ok bool, stage string) { diagnostic.Stage = stage; p3Require(t, ok, stage) }
	control.SetSourceFactsController(source)
	control.SetContentPolicyController(h.runtime.service.PolicyStore(), h.runtime.service.Readiness)
	registerXInsightContentControlRoutes(controls, control, xInsightFeedControlRouteGuards{safety: guard, expansion: guard})
	controls.Handle("GET /api/v1/projects/{id}/payload-policy", guard(control.GetProjectPayloadPolicy))
	controls.Handle("PUT /api/v1/projects/{id}/payload-policy", guard(control.PutProjectPayloadPolicy))
	invoke := func(stage, method, path string, body any) []byte {
		wire, e := json.Marshal(body)
		check(e == nil, "J1 control encode")
		req := httptest.NewRequest(method, path, strings.NewReader(string(wire)))
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		controls.ServeHTTP(res, req)
		diagnostic.Stage = stage
		diagnostic.HTTPStatus = res.Code
		diagnostic.ExpectedStatus = http.StatusOK
		diagnostic.ErrorCode = j1SafeFailureCode(res.Body.Bytes())
		check(res.Code == http.StatusOK, stage)
		return res.Body.Bytes()
	}
	path := fmt.Sprintf("/api/v1/projects/%d/payload-policy", project)
	var view contentcapture.PolicyView
	check(json.Unmarshal(invoke("policy-get", http.MethodGet, path, nil), &view) == nil, "J1 policy read")
	limit, quota, retention := int64(32<<20), int64(512<<20), int64(3600)
	policy := contentcapture.PolicyConfig{Mode: contentcapture.ModeFullContent, SupportedProtocols: h.runtime.options.Policy.SupportedProtocols, ObservationPoints: h.runtime.options.Policy.ObservationPoints, AllowedPurposes: []string{"human_review"}, RetentionSeconds: &retention, QuotaBytes: &quota, MaxSavedBytes: &limit, OnLimit: "record_capture_gap"}
	applied := invoke("policy-put", http.MethodPut, path, contentcapture.PolicyPut{SchemaVersion: contentcapture.PolicyPutSchema, ExpectedPolicyVersion: view.Desired.PolicyVersion, Desired: policy})
	check(json.Unmarshal(applied, &view) == nil && view.ApplicationStatus == "applied" && view.Effective.Config.Mode == contentcapture.ModeFullContent, "J1 effective full policy")
	p3WriteJSON(t, filepath.Join(f.output, "content-policy.json"), json.RawMessage(applied))
	random := make([]byte, 32)
	_, err := rand.Read(random)
	check(err == nil, "J1 independent randomness")
	token := auth.ReceiverFeedTokenPrefix + hex.EncodeToString(random)
	check(token != completedKey && token != sourceKey, "J1 credential isolation")
	verifier, err := auth.ReceiverFeedVerifierHex(token)
	check(err == nil, "J1 verifier")
	registration := sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: token[:auth.ReceiverFeedCredentialPrefixLength], VerifierSHA256: verifier, OperationID: hex.EncodeToString(random[:16]), SourceInstance: source.SourceInstance(), OrgID: fmt.Sprint(org), ProjectID: fmt.Sprint(project), Profiles: []string{sourcefacts.ContentProfile}}
	check(sourcefacts.ValidateContentRegistration(registration) == nil, "J1 registration validation")
	path = fmt.Sprintf("/api/v1/data-exchange/xinsight/source-connections/%d/content/", project)
	var state struct {
		StateVersion     string `json:"state_version"`
		CredentialID     string `json:"credential_id"`
		RegistrationHash string `json:"registration_hash"`
	}
	prepared := invoke("content-prepare", http.MethodPost, path+"prepare", map[string]any{"operation_id": registration.OperationID, "expected_state_version": "0", "allowed_ip": "127.0.0.1/32", "registration": registration})
	check(json.Unmarshal(prepared, &state) == nil && state.CredentialID != "" && state.RegistrationHash != "", "J1 prepared binding")
	f.secret(t, "source-content.key", token)
	p3WriteJSON(t, filepath.Join(f.output, "content-prepare.json"), json.RawMessage(prepared))
	action := func(name string) {
		var operation [16]byte
		_, e := rand.Read(operation[:])
		check(e == nil, "J1 control operation")
		payload := map[string]any{"operation_id": hex.EncodeToString(operation[:]), "expected_state_version": state.StateVersion, "credential_id": state.CredentialID, "registration_hash": state.RegistrationHash}
		raw := invoke("content-"+name, http.MethodPost, path+name, payload)
		check(json.Unmarshal(raw, &state) == nil, "J1 control receipt")
		p3WriteJSON(t, filepath.Join(f.output, "content-"+name+".json"), json.RawMessage(raw))
	}
	action("activate")
	f.contentAction = action
	check(h.runtime.registerReads(mux, source, resolver) == nil, "J1 formal C reads")
	read := func(stage, key, path string, expected int) []byte {
		diagnostic.Stage = stage
		diagnostic.HTTPStatus = 0
		diagnostic.ExpectedStatus = expected
		diagnostic.ErrorCode = ""
		status, body, err := j1LoopbackRead(f.ctx, mux, key, path)
		diagnostic.HTTPStatus = status
		diagnostic.ErrorCode = j1SafeFailureCode(body)
		check(err == nil, stage+"-transport")
		check(status == expected, stage)
		return body
	}
	f.denyLegacyContent = func(path string) {
		for _, item := range []struct {
			kind, key string
			status    int
		}{{"completed", completedKey, http.StatusUnauthorized}, {"source", sourceKey, http.StatusForbidden}} {
			status, body, err := j1LoopbackRead(f.ctx, mux, item.key, path)
			if err != nil || status != item.status {
				p3WriteJSON(t, filepath.Join(f.output, "content-body-probe-failure.json"), j1PrepareDiagnostic{Stage: item.kind + "-body", HTTPStatus: status, ExpectedStatus: item.status, ErrorCode: j1SafeFailureCode(body)})
				p3Require(t, false, "J1 legacy key must not read actual body")
			}
		}
	}
	for _, item := range []struct {
		kind, key string
		status    int
	}{{"completed", completedKey, http.StatusUnauthorized}, {"source", sourceKey, http.StatusForbidden}} {
		read(item.kind+"-capabilities", item.key, sourcefacts.ContentCapabilitiesPath, item.status)
		read(item.kind+"-manifests", item.key, sourcefacts.ContentManifestsPath+"?purpose=human_review&limit=1", item.status)
	}
	caps := read("content-capabilities", token, sourcefacts.ContentCapabilitiesPath, http.StatusOK)
	var capability sourcefacts.ContentCapabilities
	check(json.Unmarshal(caps, &capability) == nil && capability.SourceInstance == source.SourceInstance() && capability.OrgID == fmt.Sprint(org) && capability.ProjectID == fmt.Sprint(project) && capability.Status == "active", "J1 exact C capability scope")
	p3WriteJSON(t, filepath.Join(f.output, "content-capabilities.json"), json.RawMessage(caps))
	f.content = &j1ContentReady{CredentialID: state.CredentialID, RegistrationHash: state.RegistrationHash, CapabilitiesSHA256: sourcefactsHash(caps)}
	p3WriteJSON(t, filepath.Join(f.output, "content-legacy-denied.json"), map[string]any{"completed_key": 401, "source_key": 403, "paths": []string{"capabilities", "manifests"}})
	diagnostic.Successful = true
}
func sourcefactsHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func (f *p3SourceFixture) contentSafetyStages(t *testing.T) {
	for _, item := range []struct {
		name string
		done *bool
	}{{"pause", &f.contentPaused}, {"revoke", &f.contentRevoked}} {
		if *item.done {
			continue
		}
		_, e := os.Stat(filepath.Join(f.output, "content-"+item.name+"-requested"))
		if os.IsNotExist(e) {
			continue
		}
		p3Require(t, e == nil && (item.name != "revoke" || f.contentPaused), "J1 ordered safety marker")
		f.contentAction(item.name)
		*item.done = true
	}
}
func (f *p3SourceFixture) verifyContentPoints(t *testing.T, pool *pgxpool.Pool, source *sourcefacts.Service, h *sourceHTTPContentHarness, request string, org, project int64, slot int) {
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	h.mu.Lock()
	captures := append([]*contentcapture.Capture(nil), h.captures...)
	h.mu.Unlock()
	for _, c := range captures {
		result, e := c.Wait(ctx)
		p3Require(t, e == nil && result.FinalizeError == nil, "J1 actual capture finalize")
	}
	rows, e := pool.Query(ctx, `SELECT DISTINCT ON(source_fact_id) document FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND request_id=$3 ORDER BY source_fact_id,revision DESC`, org, project, request)
	p3Require(t, e == nil, "J1 capture query")
	defer rows.Close()
	points := map[string]bool{}
	evidence := []map[string]any{}
	defer func() {
		p3WriteJSON(t, filepath.Join(f.output, fmt.Sprintf("content-points-%d.json", slot)), evidence)
		p3WriteJSON(t, filepath.Join(f.output, fmt.Sprintf("content-points-%d-status.json", slot)), map[string]any{"request_id": request, "successful": !t.Failed(), "observed_points": len(points)})
	}()
	for rows.Next() {
		var raw []byte
		p3Require(t, rows.Scan(&raw) == nil, "J1 manifest read")
		m, e := sourcefacts.DecodeContentManifest(raw)
		p3Require(t, e == nil && m.SourceInstance == source.SourceInstance() && m.RequestID == request && m.Capture.Completeness == "complete" && m.ContentRef != "" && !points[m.ObservationPoint], "J1 complete distinct capture point")
		points[m.ObservationPoint] = true
		f.denyLegacyContent(sourcefacts.ContentBodyPathPrefix + m.ContentRef + "?purpose=human_review&request_id=" + url.QueryEscape(request) + "&source_fact_id=" + url.QueryEscape(m.SourceFactID) + "&revision=" + url.QueryEscape(m.Revision))
		evidence = append(evidence, map[string]any{"manifest": json.RawMessage(raw), "source_sha256": sourcefactsHash(raw), "legacy_body_denied": true})
	}
	p3Require(t, rows.Err() == nil && len(points) == 4 && points["client_request"] && points["provider_request"] && points["provider_response"] && points["client_response"], "J1 exactly four real observations")

}

// Safe inventory only: body bytes and the binding marker/token are never read
// into evidence. This candidate retains the store; deletion is a separate act.
func (f *p3SourceFixture) recordContentStore(t *testing.T) {
	root := filepath.Join(f.output, "content-store")
	entries, err := os.ReadDir(root)
	files := []map[string]any{}
	for _, entry := range entries {
		info, e := entry.Info()
		if e != nil {
			err = e
			continue
		}
		files = append(files, map[string]any{"name": entry.Name(), "size": info.Size(), "mode": info.Mode().String(), "regular": info.Mode().IsRegular(), "symlink": info.Mode()&os.ModeSymlink != 0})
	}
	p3WriteJSON(t, filepath.Join(f.output, "content-store-inventory.json"), map[string]any{"root": root, "retained": true, "complete": err == nil, "entries": files})
	if err != nil {
		t.Error("J1 content store inventory incomplete")
	}
}

// Only finite stage names chosen above, numeric status and an enumerated public
// code enter receipts. No response body, header, URL, key, verifier or raw error.
type j1PrepareDiagnostic struct {
	Stage          string `json:"stage"`
	HTTPStatus     int    `json:"http_status"`
	ExpectedStatus int    `json:"expected_status"`
	ErrorCode      string `json:"error_code"`
	Successful     bool   `json:"successful"`
}

func j1SafeFailureCode(raw []byte) string {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if len(raw) > 128<<10 || json.Unmarshal(raw, &envelope) != nil {
		return "unclassified"
	}
	switch envelope.Error.Code {
	case "unauthorized", "profile_not_authorized", "source_unavailable", "source_paused", "invalid_request", "staged_credential_requires_pause", "not_found":
		return envelope.Error.Code
	case "":
		return ""
	default:
		return "unclassified"
	}
}

// The real handler requires ResponseController deadlines. A ResponseRecorder
// returns ErrNotSupported and turns an otherwise valid capability read into 503.
// Each probe owns and joins a temporary loopback HTTP server; it follows no
// redirects, uses no proxy, and never persists successful body bytes itself.
func j1LoopbackRead(ctx context.Context, handler http.Handler, key, path string) (int, []byte, error) {
	server := httptest.NewServer(handler)
	defer server.Close()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("J1 loopback request construction failed")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("J1 loopback request failed")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (128<<10)+1))
	if err != nil || len(raw) > 128<<10 {
		return res.StatusCode, nil, fmt.Errorf("J1 loopback response incomplete or oversized")
	}
	return res.StatusCode, raw, nil
}
func TestJ1LoopbackProbeRequiresRealDeadlineTransport(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-key" {
			w.WriteHeader(401)
			return
		}
		if http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Second)) != nil {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Authorization", "Bearer synthetic-key")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != 503 {
		t.Fatal("recorder must demonstrate the original unsupported-deadline failure")
	}
	status, body, err := j1LoopbackRead(t.Context(), handler, "synthetic-key", "/probe")
	if err != nil || status != 200 || string(body) != `{"ok":true}` {
		t.Fatal("real deadline-capable loopback probe failed")
	}
}
func TestJ1ProbeDiagnosticRedactsUnreviewedData(t *testing.T) {
	for _, tc := range []struct{ raw, code string }{
		{`{"error":{"code":"source_unavailable","message":"secret-token","verifier":"secret-verifier"}}`, "source_unavailable"},
		{`{"error":{"code":"secret-token"}}`, "unclassified"}, {`secret-body`, "unclassified"},
	} {
		diagnostic := j1PrepareDiagnostic{Stage: "content-capabilities", HTTPStatus: 503, ExpectedStatus: 200, ErrorCode: j1SafeFailureCode([]byte(tc.raw))}
		raw, err := json.Marshal(diagnostic)
		if err != nil || diagnostic.ErrorCode != tc.code || strings.Contains(string(raw), "secret") {
			t.Fatal("diagnostic leaked unreviewed response data")
		}
	}
}

func (f *p3SourceFixture) requireContentApproval(t *testing.T) {
	if f.cc14 {
		cc14RequireApproval(t)
		return
	}
	j1RequireApproval(t)
}

func (f *p3SourceFixture) prepareSourceEndpoints(t *testing.T, pool *pgxpool.Pool, source *sourcefacts.Service, feed *completedfeed.Service, secret, sourceToken string, org, project int64, contentHarness *sourceHTTPContentHarness) *http.ServeMux {
	var actor int64
	err := pool.QueryRow(t.Context(), `SELECT id FROM users WHERE org_id=$1 AND username='source-synthetic-admin'`, org).Scan(&actor)
	p3Require(t, err == nil, "existing synthetic administrator")
	jwt := testutil.MustAdminSessionJWT(t, t.Context(), pool, actor, org, auth.RolePlatformAdmin, prd2E2EJWTSecret)
	control := admin.NewHandlerWithAPIKeyHashSecret(pool, prd2E2EJWTSecret, secret, []byte("0123456789abcdef01234567"), nil)
	control.SetCompletedFeedController(feed)
	guard := func(next http.HandlerFunc) http.Handler {
		return middleware.AdminAuth(pool, prd2E2EJWTSecret)(middleware.RequireRoles(auth.RolePlatformAdmin)(next))
	}
	controls := http.NewServeMux()
	registerXInsightFeedControlRoutes(controls, control, xInsightFeedControlRouteGuards{safety: guard, expansion: guard})
	random := make([]byte, 32)
	_, err = rand.Read(random)
	p3Require(t, err == nil, "receiver randomness")
	token := auth.ReceiverFeedTokenPrefix + hex.EncodeToString(random)
	verifier, err := auth.ReceiverFeedVerifierHex(token)
	p3Require(t, err == nil, "receiver verifier")
	invoke := func(suffix string, body any, status int) map[string]any {
		wire, e := json.Marshal(body)
		p3Require(t, e == nil, "control encoding")
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/data-exchange/xinsight/connections/%d/%s", project, suffix), strings.NewReader(string(wire)))
		req.Header.Set("Authorization", "Bearer "+jwt)
		res := httptest.NewRecorder()
		controls.ServeHTTP(res, req)
		p3Require(t, res.Code == status, "completed credential control rejected: "+suffix)
		var result map[string]any
		p3Require(t, json.Unmarshal(res.Body.Bytes(), &result) == nil, "control response")
		return result
	}
	staged := invoke("credentials", map[string]any{"allowed_ip": "127.0.0.1/32", "registration": map[string]any{"schema_version": auth.ReceiverFeedRegistrationSchema, "token_format": auth.ReceiverFeedTokenFormat, "verifier_scheme": auth.ReceiverFeedVerifierScheme, "key_prefix": token[:auth.ReceiverFeedCredentialPrefixLength], "verifier_sha256": verifier, "operation_id": hex.EncodeToString(random[:16])}}, http.StatusCreated)
	invoke("credentials/activate", map[string]any{"staged_credential_id": staged["id"]}, http.StatusOK)
	invoke("resume", map[string]any{}, http.StatusOK)
	f.secret(t, "completed-feed.key", token)
	f.secret(t, "source-facts.key", sourceToken)
	resolver, err := middleware.NewClientIPResolver(nil)
	p3Require(t, err == nil, "client IP resolver")
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/integrations/xinsight/completed-executions", middleware.CompletedExecutionFeedAuth(pool, secret, resolver)(feed.Handler()))
	mux.Handle("GET /api/v1/integrations/xinsight/source-facts", source.Handler(resolver))
	mux.Handle("GET /api/v1/integrations/xinsight/source-facts/capabilities", source.Handler(resolver))
	mux.Handle("GET /api/v1/integrations/xinsight/source/v2/events", source.ObservationHandler(resolver))
	mux.Handle("GET /api/v1/integrations/xinsight/source/v2/capabilities", source.ObservationHandler(resolver))
	mux.Handle("GET /api/v1/integrations/xinsight/source/v2/runtime/current", source.RuntimeCurrentHandler(resolver))
	if f.j1 || f.cc14 {
		p3Require(t, contentHarness != nil, "J1 capture owner required")
		f.prepareContent(t, controls, control, guard, jwt, mux, source, resolver, contentHarness, org, project, token, sourceToken)
	}
	return mux
}
